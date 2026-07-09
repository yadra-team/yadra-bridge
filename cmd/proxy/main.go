package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"github.com/yadra-team/yadra-bridge/config"
	"github.com/yadra-team/yadra-bridge/internal/auth"
	"github.com/yadra-team/yadra-bridge/internal/coreclient"
	"github.com/yadra-team/yadra-bridge/internal/handler"
	"github.com/yadra-team/yadra-bridge/internal/logging"
	"github.com/yadra-team/yadra-bridge/internal/ratelimit"
	"github.com/yadra-team/yadra-bridge/internal/redact"
	"github.com/yadra-team/yadra-bridge/internal/router"
	"github.com/yadra-team/yadra-bridge/internal/yadmanifest"
)

func main() {
	_ = godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	log := logging.New()

	jwks := auth.NewJWKSCache(cfg.CorePlatformURL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	jwks.StartRefresh(ctx)

	validator := auth.NewValidator(jwks, cfg.JWTIssuer, cfg.JWTAudience)
	core := coreclient.New(cfg.CorePlatformURL, cfg.InternalAPIKey, time.Duration(cfg.RoutingCacheSec)*time.Second)
	core.StartBackgroundRefresh(ctx)
	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	limiter := ratelimit.New(rdb, log, cfg.RateLimitFailClosed)
	chatRouter := router.NewWithOptions(router.Options{
		ConnectTimeout: cfg.ConnectTimeout,
		ReadTimeout:    cfg.ReadTimeout,
	})
	redactor, err := redact.New(cfg.RedactionEnabled, cfg.RedactionCategories, cfg.RedactionRulesFile)
	if err != nil {
		panic(err)
	}
	chatHandler := handler.NewChat(log, core, limiter, chatRouter, redactor)
	yadManifestSvc := yadmanifest.New(core, log, time.Duration(cfg.RoutingCacheSec)*time.Second)
	yadManifestHandler := handler.NewYadManifest(yadManifestSvc)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Get("/health", handler.Health)
	r.Get("/ready", handler.Ready(handler.ReadyDeps{Limiter: limiter, JWKS: jwks, Core: core, YadManifest: yadManifestSvc}))
	r.Get("/v1/yad/manifest", yadManifestHandler.Handle)
	r.With(auth.JWTMiddleware(validator)).Post("/v1/chat", chatHandler.Handle)

	srv := &http.Server{
		Addr:              cfg.ListenAddr(),
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Info().Str("addr", cfg.ListenAddr()).Msg("proxy starting")
		var err error
		if cfg.TLSCert != "" && cfg.TLSKey != "" {
			err = srv.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)
		} else {
			err = srv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server failed")
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
	log.Info().Msg("proxy stopped")
}
