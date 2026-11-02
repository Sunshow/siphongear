package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/sunshow/siphongear/internal/session"
)

// listSessions returns secret-free metadata for persisted collector sessions.
func (s *Server) listSessions(c *gin.Context) {
	if s.Sessions == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "session store not configured"})
		return
	}
	filter := session.ListFilter{}
	if raw := c.Query("collector_id"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid collector_id"})
			return
		}
		filter.CollectorID = uint(id)
	}
	if raw := c.Query("site_id"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid site_id"})
			return
		}
		filter.SiteID = uint(id)
	}
	rows, err := s.Sessions.List(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rows)
}

// deleteSession removes one persisted session, forcing the next run to log in.
func (s *Server) deleteSession(c *gin.Context) {
	if s.Sessions == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "session store not configured"})
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid session id"})
		return
	}
	if err := s.Sessions.Delete(c.Request.Context(), uint(id)); err != nil {
		if err == session.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// clearCollectorSessions removes every session owned by a collector.
func (s *Server) clearCollectorSessions(c *gin.Context) {
	if s.Sessions == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "session store not configured"})
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid collector id"})
		return
	}
	deleted, err := s.Sessions.DeleteByCollector(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "deleted": deleted})
}
