package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RequireRole returns a Gin middleware that checks the authenticated user's role.
func RequireRole(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if GetRole(c) != role {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		c.Next()
	}
}
