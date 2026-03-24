package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	contextKeyUserID = "user_id"
	contextKeyRole   = "role"
)

// Middleware returns a Gin middleware that validates JWT access tokens
// from the Authorization header.
func Middleware(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			return
		}

		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization header format"})
			return
		}

		tokenString := strings.TrimPrefix(header, "Bearer ")

		claims, err := ParseAccessToken(tokenString, secret)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}

		c.Set(contextKeyUserID, claims.Subject)
		c.Set(contextKeyRole, claims.Role)
		c.Next()
	}
}

// GetUserID returns the authenticated user's ID from the Gin context.
func GetUserID(c *gin.Context) string {
	return c.GetString(contextKeyUserID)
}

// GetRole returns the authenticated user's role from the Gin context.
func GetRole(c *gin.Context) string {
	return c.GetString(contextKeyRole)
}
