package middleware

import (
	"net/http"
	"strings"

	"go-live-server/internal/auth"

	"github.com/gin-gonic/gin"
)

// JWTAuth returns a Gin middleware that validates JWT Bearer tokens.
func JWTAuth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code": 1005,
				"msg":  "missing authorization header",
			})
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code": 1005,
				"msg":  "invalid authorization format, expected Bearer <token>",
			})
			return
		}

		claims, err := auth.ValidateToken(parts[1], secret)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code": 1005,
				"msg":  "invalid or expired token",
			})
			return
		}

		// Store claims in context for downstream handlers
		if username, ok := claims["username"].(string); ok {
			c.Set("username", username)
		}
		if role, ok := claims["role"].(string); ok {
			c.Set("role", role)
		}

		c.Next()
	}
}
