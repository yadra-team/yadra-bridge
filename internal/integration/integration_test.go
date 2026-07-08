//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/yarda-team/yadra-bridge/internal/auth"
	"github.com/yarda-team/yadra-bridge/internal/coreclient"
	"github.com/yarda-team/yadra-bridge/internal/handler"
	"github.com/yarda-team/yadra-bridge/internal/ratelimit"
	"github.com/yarda-team/yadra-bridge/internal/redact"
	"github.com/yarda-team/yadra-bridge/internal/router"
)

func TestIntegrationChatFlow(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kid := "integration-kid"
	jwks := auth.NewJWKSCache("http://unused")
	jwks.SetPublicKey(kid, &priv.PublicKey)
	validator := auth.NewValidator(jwks, "https://api.yadra.app", "proxy.yadra.app")

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "ok", "object": "chat.completion",
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": "ok"}}},
			"usage":   map[string]int{"prompt_tokens": 1, "completion_tokens": 1},
		})
	}))
	defer provider.Close()

	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/internal/ai/routing":
			_ = json.NewEncoder(w).Encode(coreclient.RoutingConfig{
				UpdatedAt: time.Now().UTC().Format(time.RFC3339),
				Models: []coreclient.RouteEntry{{
					ModelID: "m1", ExternalID: "gpt-4o-mini", ProviderID: "p1",
					ProviderType: "openai", ProviderName: "OpenAI",
					BaseURL: provider.URL, APIKey: "sk-test", TierMin: "trial",
				}},
			})
		case "/v1/internal/usage/ingest":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer coreSrv.Close()

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	log := zerolog.Nop()
	coreClient := coreclient.New(coreSrv.URL, "key", time.Second)
	redactor, _ := redact.New(true, nil, "")
	chat := handler.NewChat(log, coreClient, ratelimit.New(rdb, log, false), router.New(), redactor)

	mux := chi.NewRouter()
	mux.With(auth.JWTMiddleware(validator)).Post("/v1/chat", chat.Handle)
	mux.Get("/ready", handler.Ready(handler.ReadyDeps{Limiter: ratelimit.New(rdb, log, false), JWKS: jwks, Core: coreClient, YadManifest: nil}))

	srv := httptest.NewServer(mux)
	defer srv.Close()

	readyResp, err := http.Get(srv.URL + "/ready")
	if err != nil {
		t.Fatal(err)
	}
	if readyResp.StatusCode != http.StatusOK {
		t.Fatalf("ready status=%d", readyResp.StatusCode)
	}

	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: uuid.NewString(), Issuer: "https://api.yadra.app",
			Audience: jwt.ClaimStrings{"proxy.yadra.app"},
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
		},
		SubscriptionStatus: "active", SubscriptionTier: "pro", RateLimitBucket: "pro_standard",
	})
	token.Header["kid"] = kid
	signed, err := token.SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{
		"model":    "gpt-4o-mini",
		"messages": []map[string]string{{"role": "user", "content": "hello"}},
	})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/v1/chat", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+signed)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("chat status=%d", resp.StatusCode)
	}
}
