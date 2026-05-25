package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sunshow/siphongear/internal/templates"
)

func (s *Server) handleListTemplates(c *gin.Context) {
	if s.TplStore == nil {
		c.JSON(http.StatusOK, templates.BuiltinList())
		return
	}
	all, err := s.TplStore.ListAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, all)
}

func (s *Server) handleGetTemplate(c *gin.Context) {
	name := c.Param("name")
	if s.TplStore == nil {
		t, ok := templates.BuiltinGet(name)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "template not found"})
			return
		}
		c.JSON(http.StatusOK, t)
		return
	}
	t, ok, err := s.TplStore.GetAny(name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "template not found"})
		return
	}
	c.JSON(http.StatusOK, t)
}

func (s *Server) handleCreateTemplate(c *gin.Context) {
	if s.TplStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "template store not configured"})
		return
	}
	var t templates.Template
	if err := c.ShouldBindJSON(&t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := s.TplStore.Upsert(t, true); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	out, _, _ := s.TplStore.GetAny(t.Name)
	c.JSON(http.StatusOK, out)
}

func (s *Server) handleUpdateTemplate(c *gin.Context) {
	if s.TplStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "template store not configured"})
		return
	}
	name := c.Param("name")
	if _, ok := templates.BuiltinGet(name); ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot modify a builtin template"})
		return
	}
	var t templates.Template
	if err := c.ShouldBindJSON(&t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	t.Name = name
	if err := s.TplStore.Upsert(t, false); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	out, _, _ := s.TplStore.GetAny(name)
	c.JSON(http.StatusOK, out)
}

func (s *Server) handleDeleteTemplate(c *gin.Context) {
	if s.TplStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "template store not configured"})
		return
	}
	if err := s.TplStore.Delete(c.Param("name")); err != nil {
		if err == templates.ErrConflictBuiltin {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		if err == templates.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type importTemplatesReq struct {
	Templates  []templates.Template `json:"templates"`
	OnConflict string               `json:"on_conflict"` // skip|overwrite
}

type importTemplatesResp struct {
	Imported       []string `json:"imported"`
	Skipped        []string `json:"skipped"`
	SkippedBuiltin []string `json:"skipped_builtin"`
	Errors         []string `json:"errors"`
}

func (s *Server) handleImportTemplates(c *gin.Context) {
	if s.TplStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "template store not configured"})
		return
	}
	var req importTemplatesReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	resp := importTemplatesResp{}
	overwrite := req.OnConflict == "overwrite"
	for _, t := range req.Templates {
		if t.Name == "" {
			resp.Errors = append(resp.Errors, "template with empty name skipped")
			continue
		}
		if _, ok := templates.BuiltinGet(t.Name); ok {
			resp.SkippedBuiltin = append(resp.SkippedBuiltin, t.Name)
			continue
		}
		_, exists, _ := s.TplStore.GetUser(t.Name)
		if exists && !overwrite {
			resp.Skipped = append(resp.Skipped, t.Name)
			continue
		}
		if err := s.TplStore.Upsert(t, false); err != nil {
			resp.Errors = append(resp.Errors, fmt.Sprintf("%s: %v", t.Name, err))
			continue
		}
		resp.Imported = append(resp.Imported, t.Name)
	}
	c.JSON(http.StatusOK, resp)
}

type exportTemplatesResp struct {
	Version    int                  `json:"version"`
	ExportedAt time.Time            `json:"exported_at"`
	Templates  []templates.Template `json:"templates"`
}

func (s *Server) handleExportTemplates(c *gin.Context) {
	var picked []templates.Template
	names := strings.TrimSpace(c.Query("names"))
	if names != "" {
		for _, n := range strings.Split(names, ",") {
			n = strings.TrimSpace(n)
			if n == "" {
				continue
			}
			t, ok, err := s.TplStore.GetAny(n)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if ok {
				picked = append(picked, t)
			}
		}
	} else {
		users, err := s.TplStore.ListUser()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		picked = users
	}
	out := exportTemplatesResp{
		Version:    1,
		ExportedAt: time.Now().UTC(),
		Templates:  picked,
	}
	c.JSON(http.StatusOK, out)
}
