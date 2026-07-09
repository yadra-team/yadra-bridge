package handler

import (
	"encoding/json"
	"net/http"

	"github.com/yadra-team/yadra-bridge/internal/auth"
	"github.com/yadra-team/yadra-bridge/internal/coreclient"
	"github.com/yadra-team/yadra-bridge/internal/ratelimit"
	"github.com/yadra-team/yadra-bridge/internal/version"
	"github.com/yadra-team/yadra-bridge/internal/yadmanifest"
)

type ReadyDeps struct {
	Limiter     *ratelimit.Limiter
	JWKS        *auth.JWKSCache
	Core        *coreclient.Client
	YadManifest *yadmanifest.Service
}

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

type readyResponse struct {
	Status             string `json:"status"`
	RoutingUpdatedAt   string `json:"routing_updated_at,omitempty"`
	RoutingConfigured  bool   `json:"routing_configured"`
	ManifestConfigured bool   `json:"manifest_configured"`
	ManifestUpdatedAt  string `json:"manifest_updated_at,omitempty"`
}

func Health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(healthResponse{
		Status:  "ok",
		Version: version.String(),
	})
}

func Ready(deps ReadyDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if deps.Limiter != nil {
			if err := deps.Limiter.Ping(ctx); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(map[string]string{"status": "redis_unavailable"})
				return
			}
		}
		if deps.JWKS != nil && !deps.JWKS.HasKeys() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "jwks_unavailable"})
			return
		}

		routingConfigured := false
		routingUpdatedAt := ""
		manifestConfigured := false
		manifestUpdatedAt := ""
		if deps.Core != nil {
			cfg, err := deps.Core.GetRouting(ctx)
			routingUpdatedAt = deps.Core.RoutingUpdatedAt()
			if err == nil && len(cfg.Models) > 0 {
				routingConfigured = true
			}
			manifestUpdatedAt = deps.Core.PlatformConfigUpdatedAt()
			manifestConfigured = deps.Core.ManifestConfigured()
		}
		if deps.YadManifest != nil && deps.YadManifest.Configured() {
			manifestConfigured = true
		}

		w.Header().Set("Content-Type", "application/json")
		if !routingConfigured {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(readyResponse{
			Status:             ternary(routingConfigured, "ready", "routing_not_configured"),
			RoutingUpdatedAt:   routingUpdatedAt,
			RoutingConfigured:  routingConfigured,
			ManifestConfigured: manifestConfigured,
			ManifestUpdatedAt:  manifestUpdatedAt,
		})
	}
}

func ternary(ok bool, a, b string) string {
	if ok {
		return a
	}
	return b
}
