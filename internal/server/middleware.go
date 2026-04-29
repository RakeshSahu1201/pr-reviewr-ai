package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

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
func (j *JWTService) Sign(userID, username string) (string, error) {
	claims := jwt.MapClaims{
		"sub": userID,
		"username": username,
		"exp": time.Now().Add(time.Duration(j.expiryHours) * time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(j.secret)
	if err != nil {
		return "", fmt.Errorf("jwt: sign failed: %w", err)
	}
	return signed, nil
}

// Validate parses and verifies a JWT string. Returns the embedded userID and username.
func (j *JWTService) Validate(tokenStr string) (string, string, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("jwt: unexpected signing method: %v", t.Header["alg"])
		}
		return j.secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}))

	if err != nil || !token.Valid {
		return "", "", fmt.Errorf("jwt: invalid token: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", "", fmt.Errorf("jwt: could not parse claims")
	}

	userID, ok := claims["sub"].(string)
	if !ok || userID == "" {
		return "", "", fmt.Errorf("jwt: missing sub claim")
	}
	
	username, _ := claims["username"].(string)
	
	return userID, username, nil
}

// requireAuth is middleware that extracts the Bearer JWT, validates it,
// and injects the userID into the request context.
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

		c.Set("user_id", userID)
		c.Set("username", username)
		c.Next()
	}
}
