package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sunshow/siphongear/internal/store/models"
)

func (s *Server) handlePublicIndicators(c *gin.Context) {
	filter := dashboardFilter{
		IndicatorKey: strings.TrimSpace(c.Query("indicator_key")),
		Tag:          strings.TrimSpace(c.Query("tag")),
	}
	if v := c.Query("collector_id"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			filter.CollectorID = uint(n)
		}
	}
	if v := c.Query("site_id"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			filter.SiteID = uint(n)
		}
	}
	cards, err := s.buildDashboardCards(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, cards)
}

func (s *Server) handlePublicIndicatorHistory(c *gin.Context) {
	cidStr := c.Param("collector_id")
	cid64, err := strconv.ParseUint(cidStr, 10, 64)
	if err != nil || cid64 == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid collector_id"})
		return
	}
	key := strings.TrimSpace(c.Param("indicator_key"))
	if key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "indicator_key is required"})
		return
	}
	var ind models.Indicator
	if err := s.DB.Where("collector_id = ? AND key = ?", uint(cid64), key).First(&ind).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "indicator not found"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "500"))
	if limit <= 0 {
		limit = 500
	}
	if limit > 5000 {
		limit = 5000
	}
	q := s.DB.Where("indicator_id = ?", ind.ID)
	if from := c.Query("from"); from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			q = q.Where("ts >= ?", t)
		}
	}
	if to := c.Query("to"); to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			q = q.Where("ts <= ?", t)
		}
	}
	var rows []models.DataPoint
	if err := q.Order("ts asc").Limit(limit).Find(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{
		"indicator": ind,
		"points":    rows,
	})
}
