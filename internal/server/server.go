// Package server provides the HTTP API for pr-reviewer-ai.
package server

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"pr-reviewer-ai/internal/agent"
	"pr-reviewer-ai/internal/auth"
	"pr-reviewer-ai/internal/cache"
	"pr-reviewer-ai/internal/git"
	"pr-reviewer-ai/internal/llm"
	"pr-reviewer-ai/internal/ratelimit"
	"pr-reviewer-ai/internal/repository"

	"github.com/gin-gonic/gin"
)

// Server holds all dependencies needed to handle HTTP requests.
type Server struct {
	authSvc      *auth.AuthService
	logRepo      repository.ReviewLogRepository
	jwtSvc       *JWTService
	gitFactory   GitProviderFactory
	pipeline     *llm.Pipeline // nil when no LLM API keys are set
	sessionStore *cache.SessionStore
	limiter      *ratelimit.Limiter
	router       *gin.Engine
}

// GitProviderFactory creates a GitProvider for a given user token, base URL, and optional project ID.
// Injected from app.go to keep server.go provider-agnostic.
type GitProviderFactory func(webUrl, token string, projectID int64, gitlabUserID int) (git.GitProvider, error)

// New creates a Server and registers all routes.
func New(
	authSvc *auth.AuthService,
	logRepo repository.ReviewLogRepository,
	jwtSvc *JWTService,
	factory GitProviderFactory,
	pipeline *llm.Pipeline,
	sessionStore *cache.SessionStore,
	limiter *ratelimit.Limiter,
) *Server {
	s := &Server{
		authSvc:      authSvc,
		logRepo:      logRepo,
		jwtSvc:       jwtSvc,
		gitFactory:   factory,
		pipeline:     pipeline,
		sessionStore: sessionStore,
		limiter:      limiter,
		router:       gin.Default(),
	}
	s.registerRoutes()
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) registerRoutes() {
	// Health check — no rate limit.
	s.router.GET("/healthz", s.handleHealthz)

	// Auth routes: strict IP-based rate limiting (5 req / 15 min), fail-closed.
	authGroup := s.router.Group("/api/auth")
	authGroup.Use(rateLimitAuth(s.limiter))
	{
		authGroup.POST("/register", s.handleRegister)
		authGroup.POST("/login", s.handleLogin)
	}

	// Authenticated routes: JWT validation first, then general API rate limiting
	// (100 req / min, UserID-based), fail-open.
	protected := s.router.Group("/api")
	protected.Use(requireAuth(s.jwtSvc), rateLimitAPI(s.limiter))
	{
		protected.DELETE("/auth/logout", s.handleLogout)
		protected.POST("/review", s.handleReview)
		protected.GET("/reviews", s.handleListReviews)
		protected.GET("/projects", s.handleListProjects)
		protected.PUT("/project", s.handleUpdateProject)
	}
}

// ─── Handlers ────────────────────────────────────────────────────────────────

func (s *Server) handleHealthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) handleRegister(c *gin.Context) {
	var body struct {
		Username      string `json:"username"`
		Password      string `json:"password"`
		Token         string `json:"token"`
		GitlabBaseUrl string `json:"webUrl"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}

	if _, err := s.authSvc.Register(body.Username, body.Password, body.Token, body.GitlabBaseUrl); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "user created successfully"})
}

func (s *Server) handleLogin(c *gin.Context) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	if body.Username == "" || body.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username and password are required"})
		return
	}

	loginUser, err := s.authSvc.Login(body.Username, body.Password)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID := int64(loginUser.ID)

	jwtToken, err := s.jwtSvc.Sign(userID, loginUser.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate session token"})
		return
	}

	// Warm the session cache: fetch plaintext token + webUrl + projectID from Postgres,
	// then store them encrypted in Redis aligned to the JWT TTL.
	if s.sessionStore != nil {
		rawToken, errT := s.authSvc.GetToken(userID)
		rawWebUrl, errW := s.authSvc.GetWebUrl(userID)
		projectID, _ := s.authSvc.GetProjectID(userID)
		var guid int
		if loginUser.GitlabUserID != nil {
			guid = *loginUser.GitlabUserID
		}
		if errT == nil && errW == nil {
			ttl := s.jwtSvc.ExpiryDuration()
			if ttl <= 0 {
				ttl = 24 * time.Hour
			}
			_ = s.sessionStore.Set(c.Request.Context(), userID, loginUser.Username, projectID, guid, rawToken, rawWebUrl, ttl)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "login successful",
		"token":   jwtToken,
	})
}

func (s *Server) handleLogout(c *gin.Context) {
	userID := c.GetInt64("userID")

	// Invalidate Redis session.
	if s.sessionStore != nil {
		_ = s.sessionStore.Invalidate(c.Request.Context(), userID)
	}

	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}

// getUserGitConfig fetches the user's token, webUrl, and gitlabUserID, preferring the Redis
// cache and falling back to Postgres transparently.
func (s *Server) getUserGitConfig(c *gin.Context, userID int64) (token, webUrl string, guid int, err error) {
	// Try cache first.
	if s.sessionStore != nil {
		data, err := s.sessionStore.Get(c.Request.Context(), userID)
		if err == nil && data != nil {
			token, errT := s.sessionStore.Token(c.Request.Context(), userID)
			webUrl, errW := s.sessionStore.WebUrl(c.Request.Context(), userID)
			if errT == nil && errW == nil && token != "" && webUrl != "" {
				return token, webUrl, data.GitlabUserID, nil
			}
		}
	}
	// Cache miss or error — fall back to Postgres.
	token, err = s.authSvc.GetToken(userID)
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to retrieve GitLab token: %w", err)
	}
	webUrl, err = s.authSvc.GetWebUrl(userID)
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to retrieve GitLab base URL: %w", err)
	}
	guid, _ = s.authSvc.GetGitlabUserIDByID(userID)
	return token, webUrl, guid, nil
}

func (s *Server) handleReview(c *gin.Context) {
	userID := c.GetInt64("userID")

	var body struct {
		MRID      int   `json:"mr_id"`
		ProjectID int64 `json:"project_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}

	// Fallback to cached ProjectID if not provided in body.
	if body.ProjectID <= 0 && s.sessionStore != nil {
		if cachedID, _ := s.sessionStore.ProjectID(c.Request.Context(), userID); cachedID > 0 {
			body.ProjectID = cachedID
		}
	}
	// Final fallback to Postgres if still empty.
	if body.ProjectID <= 0 {
		if dbID, _ := s.authSvc.GetProjectID(userID); dbID > 0 {
			body.ProjectID = dbID
		}
	}

	if body.MRID == 0 || body.ProjectID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "mr_id and project_id are required"})
		return
	}

	token, webUrl, guid, err := s.getUserGitConfig(c, userID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	provider, err := s.gitFactory(webUrl, token, body.ProjectID, guid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to build git provider: %v", err)})
		return
	}

	reviewer := agent.New(provider, s.logRepo, strconv.FormatInt(body.ProjectID, 10), s.pipeline)
	if err := reviewer.Review(c.Request.Context(), userID, body.MRID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "review posted to MR !" + strconv.Itoa(body.MRID),
	})
}

func (s *Server) handleListReviews(c *gin.Context) {
	userID := c.GetInt64("userID")

	logs, err := s.logRepo.ListReviews(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"reviews": logs})
}

func (s *Server) handleListProjects(c *gin.Context) {
	userID := c.GetInt64("userID")

	token, webUrl, guid, err := s.getUserGitConfig(c, userID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	provider, err := s.gitFactory(webUrl, token, 0, guid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to build git provider: %v", err)})
		return
	}

	projects, err := provider.ListProjects()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"projects": projects})
}

func (s *Server) handleUpdateProject(c *gin.Context) {
	userID := c.GetInt64("userID")

	var body struct {
		ProjectID int64 `json:"project_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	if body.ProjectID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project_id is required and must be positive"})
		return
	}

	if err := s.authSvc.UpdateProjectID(userID, body.ProjectID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Partial session update in Redis.
	if s.sessionStore != nil {
		_ = s.sessionStore.UpdateProjectID(c.Request.Context(), userID, body.ProjectID)
	}

	c.JSON(http.StatusOK, gin.H{"message": "project_id updated successfully"})
}
