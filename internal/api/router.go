package api

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/sunshow/siphongear/internal/auth"
	"github.com/sunshow/siphongear/internal/crypto"
	"github.com/sunshow/siphongear/internal/notify"
	"github.com/sunshow/siphongear/internal/runner"
	"github.com/sunshow/siphongear/internal/scheduler"
	"github.com/sunshow/siphongear/internal/templates"
)

type Server struct {
	DB               *gorm.DB
	JWT              *auth.JWT
	Cipher           *crypto.Cipher
	Runner           *runner.Runner
	Scheduler        *scheduler.Scheduler
	TplStore         *templates.Store
	NotifyDispatcher *notify.Dispatcher
	Static           fs.FS // embedded web/dist
}

func NewRouter(s *Server) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger(), recoveryMiddleware())

	api := r.Group("/api/v1")
	api.GET("/healthz", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
	api.POST("/auth/login", s.handleLogin)

	authed := api.Group("")
	authed.Use(authMiddleware(s.JWT))

	authed.GET("/auth/me", s.handleMe)
	authed.POST("/auth/password", s.handleChangePassword)

	authed.GET("/registry/steps", s.handleListStepMeta)
	authed.GET("/templates", s.handleListTemplates)
	authed.GET("/templates/export", s.handleExportTemplates)
	authed.POST("/templates/import", s.handleImportTemplates)
	authed.POST("/templates", s.handleCreateTemplate)
	authed.GET("/templates/:name", s.handleGetTemplate)
	authed.PUT("/templates/:name", s.handleUpdateTemplate)
	authed.DELETE("/templates/:name", s.handleDeleteTemplate)

	authed.GET("/sites", s.listSites)
	authed.POST("/sites", s.createSite)
	authed.GET("/sites/:id", s.getSite)
	authed.PUT("/sites/:id", s.updateSite)
	authed.DELETE("/sites/:id", s.deleteSite)

	authed.GET("/credentials", s.listCredentials)
	authed.POST("/credentials", s.createCredential)
	authed.GET("/credentials/:id", s.getCredential)
	authed.PUT("/credentials/:id", s.updateCredential)
	authed.DELETE("/credentials/:id", s.deleteCredential)

	authed.GET("/collectors", s.listCollectors)
	authed.POST("/collectors", s.createCollector)
	authed.GET("/collectors/:id", s.getCollector)
	authed.PUT("/collectors/:id", s.updateCollector)
	authed.DELETE("/collectors/:id", s.deleteCollector)
	authed.POST("/collectors/:id/run", s.runCollector)
	authed.POST("/collectors/:id/dryrun", s.dryRunCollector)
	authed.GET("/collectors/:id/runs", s.listRuns)
	authed.GET("/collectors/:id/datapoints", s.listDataPoints)

	authed.GET("/collectors/:id/indicators", s.listIndicators)
	authed.POST("/collectors/:id/indicators", s.createIndicator)
	authed.PUT("/indicators/:id", s.updateIndicator)
	authed.DELETE("/indicators/:id", s.deleteIndicator)

	authed.GET("/runs/:id", s.getRun)

	authed.GET("/rules", s.listRules)
	authed.POST("/rules", s.createRule)
	authed.POST("/rules/preview-notify", s.previewRuleNotify)
	authed.GET("/rules/:id", s.getRule)
	authed.PUT("/rules/:id", s.updateRule)
	authed.DELETE("/rules/:id", s.deleteRule)

	authed.GET("/notify/types", s.handleListNotifyTypes)
	authed.GET("/notify/channels", s.listNotifyChannels)
	authed.POST("/notify/channels", s.createNotifyChannel)
	authed.GET("/notify/channels/:id", s.getNotifyChannel)
	authed.PUT("/notify/channels/:id", s.updateNotifyChannel)
	authed.DELETE("/notify/channels/:id", s.deleteNotifyChannel)
	authed.POST("/notify/channels/:id/test", s.testNotifyChannel)
	authed.GET("/notify/logs", s.listNotifyLogs)

	authed.GET("/dashboard", s.handleDashboard)

	if s.Static != nil {
		r.NoRoute(serveStatic(s.Static))
	}
	return r
}

func serveStatic(static fs.FS) gin.HandlerFunc {
	fsys := http.FS(static)
	server := http.FileServer(fsys)
	return func(c *gin.Context) {
		path := strings.TrimPrefix(c.Request.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		f, err := static.Open(path)
		if err != nil {
			c.Request.URL.Path = "/"
			indexFile, e := static.Open("index.html")
			if e != nil {
				c.String(http.StatusNotFound, "not found")
				return
			}
			indexFile.Close()
		} else {
			f.Close()
		}
		server.ServeHTTP(c.Writer, c.Request)
	}
}
