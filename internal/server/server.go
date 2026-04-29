// Package server provides the HTTP API for pr-reviewer-ai.
package server

import (
	"fmt"
	"net/http"
	"pr-reviewer-ai/internal/agent"
	"pr-reviewer-ai/internal/auth"
	"pr-reviewer-ai/internal/git"
	"pr-reviewer-ai/internal/llm"
	"pr-reviewer-ai/internal/repository"
	"strconv"

	"github.com/gin-gonic/gin"
)

// Server holds all dependencies needed to handle HTTP requests.
type Server struct {
	authSvc    *auth.AuthService
	logRepo    repository.ReviewLogRepository
	jwtSvc     *JWTService
	gitFactory GitProviderFactory
	pipeline   *llm.Pipeline // nil when GEMINI_API_KEY is not set
	router     *gin.Engine
}

// GitProviderFactory creates a GitProvider for a given user token and base URL.
// Injected from app.go to keep server.go provider-agnostic.
type GitProviderFactory func(webUrl, token string) (git.GitProvider, error)

// New creates a Server and registers all routes.
func New(
	authSvc *auth.AuthService,
	logRepo repository.ReviewLogRepository,
	jwtSvc *JWTService,
	factory GitProviderFactory,
	pipeline *llm.Pipeline,
) *Server {
	s := &Server{
		authSvc:    authSvc,
		logRepo:    logRepo,
		jwtSvc:     jwtSvc,
		gitFactory: factory,
		pipeline:   pipeline,
		router:     gin.Default(),
	}
	s.registerRoutes()
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) registerRoutes() {
	s.router.GET("/healthz", s.handleHealthz)
	s.router.POST("/api/auth/register", s.handleRegister)
	s.router.POST("/api/auth/login", s.handleLogin)
	s.router.DELETE("/api/auth/logout", requireAuth(s.jwtSvc), s.handleLogout)
	s.router.POST("/api/review", requireAuth(s.jwtSvc), s.handleReview)
	s.router.GET("/api/reviews", requireAuth(s.jwtSvc), s.handleListReviews)
}

// --- Handlers ---

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

	if err := s.authSvc.Login(body.Username, body.Password); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	gitlabUserID, err := s.authSvc.GetGitlabUserID(body.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve user info"})
		return
	}

	jwt, err := s.jwtSvc.Sign(strconv.Itoa(gitlabUserID), body.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate session token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "login successful",
		"token":   jwt,
	})
}

func (s *Server) handleLogout(c *gin.Context) {
	userID := c.GetString("user_id")
	if err := s.authSvc.Logout(userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}

func (s *Server) handleReview(c *gin.Context) {
	userID := c.GetString("user_id")

	var body struct {
		MRID      int    `json:"mr_id"`
		ProjectID string `json:"project_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	if body.MRID == 0 || body.ProjectID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "mr_id and project_id are required"})
		return
	}

	token, err := s.authSvc.GetToken(userID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no GitLab token found — call /api/auth/login first"})
		return
	}

	webUrl, err := s.authSvc.GetWebUrl(userID)
	if err != nil {
		// Fallback to error or default? The user registered with a webUrl, so we should use it.
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve GitLab base URL"})
		return
	}

	provider, err := s.gitFactory(webUrl, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to build git provider: %v", err)})
		return
	}

	reviewer := agent.New(provider, s.logRepo, body.ProjectID, s.pipeline)
	if err := reviewer.Review(c.Request.Context(), userID, body.MRID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "review posted to MR !" + strconv.Itoa(body.MRID),
	})
}

func (s *Server) handleListReviews(c *gin.Context) {
	userID := c.GetString("user_id")

	logs, err := s.logRepo.ListReviews(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"reviews": logs})
}
