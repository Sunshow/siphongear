package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/sunshow/siphongear/internal/apikey"
	"github.com/sunshow/siphongear/internal/auth"
)

const (
	ctxUserID    = "uid"
	ctxUsername  = "username"
	ctxAPIKeyID  = "api_key_id"
	headerAPIKey = "X-API-Key"
	queryAPIKey  = "api_key"
)

func authMiddleware(jwt *auth.JWT) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
			return
		}
		token := strings.TrimPrefix(header, "Bearer ")
		token = strings.TrimSpace(token)
		claims, err := jwt.Parse(token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		c.Set(ctxUserID, claims.UserID)
		c.Set(ctxUsername, claims.Username)
		c.Next()
	}
}

func apiKeyMiddleware(v *apikey.Verifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader(headerAPIKey)
		if token == "" {
			h := c.GetHeader("Authorization")
			token = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		}
		if token == "" {
			token = strings.TrimSpace(c.Query(queryAPIKey))
		}
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing api key"})
			return
		}
		row, err := v.Verify(token)
		if err != nil {
			if errors.Is(err, apikey.ErrDisabled) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "api key disabled"})
				return
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid api key"})
			return
		}
		c.Set(ctxAPIKeyID, row.ID)
		c.Next()
	}
}

func recoveryMiddleware() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, err any) {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	})
}
