// internal/middleware/auth.go
package middleware

import (
	"cashback-tracker/internal/auth"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

type AuthMiddleware struct {
	tokenService *auth.TokenService
}

func NewAuthMiddleware(ts *auth.TokenService) *AuthMiddleware {
	return &AuthMiddleware{tokenService: ts}
}

func (m *AuthMiddleware) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		slog.Debug("Auth header", "header", authHeader)

		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
			return
		}

		var tokenStr string
		if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
			tokenStr = authHeader[7:]
		} else {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid Authorization header format"})
			return
		}

		userID, err := m.tokenService.ParseToken(tokenStr) // → int64
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
			return
		}

		c.Set("user_id", userID) // ← сохраняем как int64
		c.Next()
	}
}	