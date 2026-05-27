package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/sunshow/siphongear/internal/apikey"
	"github.com/sunshow/siphongear/internal/store/models"
)

type apiKeyIn struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Notes   string `json:"notes"`
}

type apiKeyOut struct {
	ID         uint    `json:"id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	Enabled    bool    `json:"enabled"`
	LastUsedAt *string `json:"last_used_at"`
	Notes      string  `json:"notes"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

func toAPIKeyOut(r models.APIKey) apiKeyOut {
	out := apiKeyOut{
		ID:        r.ID,
		Name:      r.Name,
		Prefix:    r.Prefix,
		Enabled:   r.Enabled,
		Notes:     r.Notes,
		CreatedAt: r.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: r.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if r.LastUsedAt != nil {
		s := r.LastUsedAt.Format("2006-01-02T15:04:05Z07:00")
		out.LastUsedAt = &s
	}
	return out
}

func (s *Server) listAPIKeys(c *gin.Context) {
	var rows []models.APIKey
	if err := s.DB.Order("id desc").Find(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]apiKeyOut, 0, len(rows))
	for _, r := range rows {
		out = append(out, toAPIKeyOut(r))
	}
	c.JSON(200, out)
}

func (s *Server) createAPIKey(c *gin.Context) {
	var in apiKeyIn
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	gen, err := apikey.Generate()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	row := models.APIKey{
		Name:       in.Name,
		Prefix:     gen.Prefix,
		SecretHash: gen.SecretHash,
		Enabled:    true,
		Notes:      strings.TrimSpace(in.Notes),
	}
	if err := s.DB.Create(&row).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{
		"api_key":   toAPIKeyOut(row),
		"plaintext": gen.Plaintext,
	})
}

func (s *Server) updateAPIKey(c *gin.Context) {
	var row models.APIKey
	if err := s.DB.First(&row, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	var in apiKeyIn
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	row.Name = in.Name
	row.Enabled = in.Enabled
	row.Notes = strings.TrimSpace(in.Notes)
	if err := s.DB.Save(&row).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, toAPIKeyOut(row))
}

func (s *Server) deleteAPIKey(c *gin.Context) {
	if err := s.DB.Delete(&models.APIKey{}, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) rotateAPIKey(c *gin.Context) {
	var row models.APIKey
	if err := s.DB.First(&row, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	gen, err := apikey.Generate()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	row.Prefix = gen.Prefix
	row.SecretHash = gen.SecretHash
	row.LastUsedAt = nil
	if err := s.DB.Save(&row).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{
		"api_key":   toAPIKeyOut(row),
		"plaintext": gen.Plaintext,
	})
}
