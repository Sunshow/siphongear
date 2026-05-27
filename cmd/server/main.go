package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sunshow/siphongear/internal/api"
	"github.com/sunshow/siphongear/internal/apikey"
	"github.com/sunshow/siphongear/internal/auth"
	"github.com/sunshow/siphongear/internal/config"
	"github.com/sunshow/siphongear/internal/crypto"
	"github.com/sunshow/siphongear/internal/events"
	"github.com/sunshow/siphongear/internal/notify"
	_ "github.com/sunshow/siphongear/internal/notify/builtin"
	"github.com/sunshow/siphongear/internal/runner"
	"github.com/sunshow/siphongear/internal/scheduler"
	"github.com/sunshow/siphongear/internal/store"
	"github.com/sunshow/siphongear/internal/store/models"
	_ "github.com/sunshow/siphongear/internal/steps"
	"github.com/sunshow/siphongear/internal/templates"
	"github.com/sunshow/siphongear/pkg/logger"
	"github.com/sunshow/siphongear/web"
)

func main() {
	configPath := flag.String("config", os.Getenv("SIPHON_CONFIG"), "path to config.yaml")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config load: %v\n", err)
		os.Exit(1)
	}
	logger.Init(cfg.Log.Level, cfg.Log.Pretty)

	if cfg.Auth.MasterKey == "" {
		logger.Error().Msg("auth.master_key (or SIPHON_AUTH__MASTER_KEY) is required")
		os.Exit(1)
	}
	if cfg.Auth.JWTSecret == "" {
		logger.Error().Msg("auth.jwt_secret (or SIPHON_AUTH__JWT_SECRET) is required")
		os.Exit(1)
	}

	cipher, err := crypto.New(cfg.Auth.MasterKey)
	if err != nil {
		logger.Error().Err(err).Msg("init cipher")
		os.Exit(1)
	}

	db, err := store.Open(cfg.Database)
	if err != nil {
		logger.Error().Err(err).Msg("open db")
		os.Exit(1)
	}
	if err := store.Migrate(db); err != nil {
		logger.Error().Err(err).Msg("migrate db")
		os.Exit(1)
	}
	if err := store.PruneOrphans(db); err != nil {
		logger.Warn().Err(err).Msg("prune orphans")
	}

	// Bootstrap initial admin user if none.
	{
		var count int64
		_ = db.Model(&models.User{}).Count(&count).Error
		if count == 0 {
			username := cfg.Auth.InitUsername
			if username == "" {
				username = "admin"
			}
			password := cfg.Auth.InitPassword
			if password == "" {
				password = "admin"
				logger.Warn().Msg("init_password not set; default admin/admin created — change immediately")
			}
			hash, err := auth.HashPassword(password)
			if err == nil {
				_ = db.Create(&models.User{Username: username, PasswordHash: hash}).Error
			}
		}
	}

	bus := events.NewBus()
	jwtSvc := auth.NewJWT(cfg.Auth.JWTSecret, cfg.Auth.TokenTTLHrs)
	r := runner.New(db, cipher, bus, cfg.Runner.MaxConcurrency)
	sch := scheduler.New(db, r, bus)
	disp := notify.NewDispatcher(db, cipher, bus)

	rootCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sch.Start(rootCtx); err != nil {
		logger.Error().Err(err).Msg("start scheduler")
		os.Exit(1)
	}
	disp.Start(rootCtx)

	staticFS, err := web.Dist()
	if err != nil {
		logger.Warn().Err(err).Msg("embedded web disabled")
	}

	server := &api.Server{
		DB:               db,
		JWT:              jwtSvc,
		Cipher:           cipher,
		Runner:           r,
		Scheduler:        sch,
		TplStore:         templates.NewStore(db),
		NotifyDispatcher: disp,
		APIKeyVerifier:   apikey.NewVerifier(db),
		Static:           staticFS,
	}
	router := api.NewRouter(server)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info().Str("addr", addr).Msg("siphongear listening")
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error().Err(err).Msg("server error")
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	logger.Info().Msg("shutting down")

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	_ = httpSrv.Shutdown(shutCtx)
	sch.Stop()
}
