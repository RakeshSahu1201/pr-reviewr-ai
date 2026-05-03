package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pr-reviewer-ai/internal/ratelimit"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// ────────────────────────────────────────────────────────────────────────────
// JWTService
// ────────────────────────────────────────────────────────────────────────────

// JWTService signs and validates JWT session tokens.
type JWTService struct {
	secret      []byte
	expiryHours int
}

// NewJWTService creates a JWTService.
// secret should be at least 32 random bytes. expiryHours is JWT lifetime (default 24).
func NewJWTService(secret []byte, expiryHours int) *JWTService {
	if expiryHours <= 0 {
		expiryHours = 24
	}
	return &JWTService{secret: secret, expiryHours: expiryHours}
}

// Sign creates a signed JWT embedding the userID and username.
func (j *JWTService) Sign(userID int64, username string) (string, error) {
	claims := jwt.MapClaims{
		"sub":      strconv.FormatInt(userID, 10),
		"username": username,
		"exp":      time.Now().Add(time.Duration(j.expiryHours) * time.Hour).Unix(),
		"iat":      time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(j.secret)
	if err != nil {
		return "", fmt.Errorf("jwt: sign failed: %w", err)
	}
	return signed, nil
}

// ExpiryDuration returns the configured token lifetime.
func (j *JWTService) ExpiryDuration() time.Duration {
	return time.Duration(j.expiryHours) * time.Hour
}

// Validate parses and verifies a JWT string. Returns the embedded userID and username.
func (j *JWTService) Validate(tokenStr string) (int64, string, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("jwt: unexpected signing method: %v", t.Header["alg"])
		}
		return j.secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}))

	if err != nil || !token.Valid {
		return 0, "", fmt.Errorf("jwt: invalid token: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return 0, "", fmt.Errorf("jwt: could not parse claims")
	}

	sub, ok := claims["sub"].(string)
	if !ok || sub == "" {
		return 0, "", fmt.Errorf("jwt: missing sub claim")
	}

	userID, err := strconv.ParseInt(sub, 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("jwt: invalid sub claim: %w", err)
	}

	username, _ := claims["username"].(string)

	return userID, username, nil
}

// ────────────────────────────────────────────────────────────────────────────
// requireAuth middleware
// ────────────────────────────────────────────────────────────────────────────

// requireAuth extracts the Bearer JWT, validates it, and injects the userID
// and username into the request context.
func requireAuth(jwtSvc *JWTService) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing or malformed Authorization header"})
			c.Abort()
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		userID, username, err := jwtSvc.Validate(tokenStr)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			c.Abort()
			return
		}

		c.Set("userID", userID)
		c.Set("username", username)
		c.Next()
	}
}

// ────────────────────────────────────────────────────────────────────────────
// rateLimitAuth middleware  (fail-CLOSED, IP-based)
// ────────────────────────────────────────────────────────────────────────────

// rateLimitAuth enforces strict IP-based rate limiting on auth routes
// (/login, /register).  5 requests per 15 minutes.  Fails CLOSED: if Redis is
// unreachable the request is rejected to prevent brute-force storms.
func rateLimitAuth(limiter *ratelimit.Limiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := clientIP(c)
		res, err := limiter.AllowAuthIP(c.Request.Context(), ip)
		if err != nil {
			// Redis error on auth route → fail-closed.
			c.Header("Retry-After", "900") // 15 min
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "rate limiter temporarily unavailable, please try again later",
			})
			c.Abort()
			return
		}
		setRateLimitHeaders(c, res)
		if !res.Allowed {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":       "too many requests — try again later",
				"retry_after": int(res.RetryAfter.Seconds()),
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

// ────────────────────────────────────────────────────────────────────────────
// rateLimitAPI middleware  (fail-OPEN, UserID or IP-based)
// ────────────────────────────────────────────────────────────────────────────

// rateLimitAPI enforces general API rate limiting.  100 requests per minute.
// Uses UserID for authenticated routes and IP for unauthenticated ones.
// Fails OPEN: if Redis is unreachable, requests are allowed through.
func rateLimitAPI(limiter *ratelimit.Limiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Prefer userID set by requireAuth; fall back to IP.
		var identifier string
		if val, exists := c.Get("userID"); exists {
			if id, ok := val.(int64); ok {
				identifier = strconv.FormatInt(id, 10)
			}
		}

		if identifier == "" {
			identifier = clientIP(c)
		}

		res, err := limiter.AllowAPI(c.Request.Context(), identifier)
		if err != nil {
			// Redis error on API route → fail-open (log but continue).
			c.Next()
			return
		}
		setRateLimitHeaders(c, res)
		if !res.Allowed {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":       "rate limit exceeded",
				"retry_after": int(res.RetryAfter.Seconds()),
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Helpers
// ────────────────────────────────────────────────────────────────────────────

// clientIP extracts the real client IP, respecting X-Forwarded-For when
// running behind a trusted reverse proxy.
func clientIP(c *gin.Context) string {
	if xff := c.GetHeader("X-Forwarded-For"); xff != "" {
		// Grab the first (leftmost) IP — the originating client.
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	return c.ClientIP()
}

// setRateLimitHeaders writes standard X-RateLimit-* response headers.
func setRateLimitHeaders(c *gin.Context, res ratelimit.Result) {
	remaining := res.Limit - res.Count
	if remaining < 0 {
		remaining = 0
	}
	c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", res.Limit))
	c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
	if !res.Allowed {
		c.Header("Retry-After", fmt.Sprintf("%d", int(res.RetryAfter.Seconds())))
	}
}
