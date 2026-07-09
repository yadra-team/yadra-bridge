package handler_test

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/yadra-team/yadra-bridge/internal/auth"
	"github.com/yadra-team/yadra-bridge/internal/coreclient"
	"github.com/yadra-team/yadra-bridge/internal/handler"
	"github.com/yadra-team/yadra-bridge/internal/ratelimit"
	"github.com/yadra-team/yadra-bridge/internal/redact"
	"github.com/yadra-team/yadra-bridge/internal/router"
)

const (
	testIssuer   = "https://api.yadra.app"
	testAudience = "proxy.yadra.app"
	secretPrompt = "YADRA_PRIVACY_TEST_PROMPT_DO_NOT_LOG"
	secretReply  = "YADRA_PRIVACY_TEST_RESPONSE_DO_NOT_LOG"
)

type chatFixture struct {
	mux          *chi.Mux
	logBuf       *bytes.Buffer
	logMu        sync.Mutex
	token        string
	ingestMu     sync.Mutex
	ingest       []byte
	providerMu   sync.Mutex
	provider     []byte
	priv         *rsa.PrivateKey
	kid          string
	routeTierMin string
	routingEmpty bool
}

// mintToken signs a JWT with the fixture key for the given tier and bucket.
func (fx *chatFixture) mintToken(t *testing.T, tier, bucket string) string {
	t.Helper()
	now := time.Now()
	claims := auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: uuid.NewString(), Issuer: testIssuer,
			Audience: jwt.ClaimStrings{testAudience},
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
			ID: uuid.NewString(),
		},
		SubscriptionStatus: "active", SubscriptionTier: tier, RateLimitBucket: bucket,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = fx.kid
	s, err := token.SignedString(fx.priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func setupChatTest(t *testing.T) *chatFixture {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kid := "test-kid"
	cache := auth.NewJWKSCache("http://unused")
	cache.SetPublicKey(kid, &priv.PublicKey)
	validator := auth.NewValidator(cache, testIssuer, testAudience)

	fx := &chatFixture{logBuf: &bytes.Buffer{}, priv: priv, kid: kid, routeTierMin: "trial"}

	providerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fx.providerMu.Lock()
		fx.provider = body
		fx.providerMu.Unlock()

		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if stream, _ := req["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			chunk, _ := json.Marshal(map[string]any{
				"id": "chatcmpl-test", "object": "chat.completion.chunk",
				"choices": []map[string]any{{"index": 0, "delta": map[string]string{"content": secretReply}}},
			})
			_, _ = w.Write([]byte("data: " + string(chunk) + "\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-test", "object": "chat.completion", "created": time.Now().Unix(),
			"model": "gpt-4o-mini",
			"choices": []map[string]any{{
				"index": 0, "message": map[string]string{"role": "assistant", "content": secretReply},
				"finish_reason": "stop",
			}},
			"usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
		})
	}))
	t.Cleanup(providerSrv.Close)

	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/internal/ai/routing":
			models := []coreclient.RouteEntry{{
				ModelID: "model-1", ExternalID: "gpt-4o-mini", ProviderID: "prov-1",
				ProviderType: "openai", ProviderName: "OpenAI", BaseURL: providerSrv.URL,
				APIKey: "sk-test", TierMin: fx.routeTierMin, InputCost1M: 0.15, OutputCost1M: 0.6,
			}}
			if fx.routingEmpty {
				models = nil
			}
			_ = json.NewEncoder(w).Encode(coreclient.RoutingConfig{
				UpdatedAt: time.Now().UTC().Format(time.RFC3339),
				Models:    models,
			})
		case "/v1/internal/usage/ingest":
			body, _ := io.ReadAll(r.Body)
			fx.ingestMu.Lock()
			fx.ingest = body
			fx.ingestMu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(coreSrv.Close)

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	log := zerolog.New(&syncWriter{buf: fx.logBuf, mu: &fx.logMu}).With().Timestamp().Str("service", "proxy").Logger()
	core := coreclient.New(coreSrv.URL, "internal-test-key", time.Minute)
	redactor, err := redact.New(true, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	chatHandler := handler.NewChat(log, core, ratelimit.New(rdb, log, false), router.New(), redactor)

	fx.mux = chi.NewRouter()
	fx.mux.With(auth.JWTMiddleware(validator)).Post("/v1/chat", chatHandler.Handle)

	fx.token = fx.mintToken(t, "pro", "pro_standard")
	return fx
}

type syncWriter struct {
	buf *bytes.Buffer
	mu  *sync.Mutex
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (fx *chatFixture) logs() string {
	fx.logMu.Lock()
	defer fx.logMu.Unlock()
	return fx.logBuf.String()
}

func TestChatHandleSuccess(t *testing.T) {
	fx := setupChatTest(t)
	body, _ := json.Marshal(map[string]any{
		"model":    "gpt-4o-mini",
		"messages": []map[string]string{{"role": "user", "content": secretPrompt}},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+fx.token)
	rec := httptest.NewRecorder()
	fx.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	logs := fx.logs()
	if strings.Contains(logs, secretPrompt) || strings.Contains(logs, secretReply) {
		t.Fatalf("content leaked into logs")
	}
}

func TestChatHandleUnauthorized(t *testing.T) {
	fx := setupChatTest(t)
	body := []byte(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	fx.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"unauthorized"`) {
		t.Fatalf("expected unified error body: %s", rec.Body.String())
	}
}

func TestChatHandleUnknownModel(t *testing.T) {
	fx := setupChatTest(t)
	body := []byte(`{"model":"unknown-model","messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+fx.token)
	rec := httptest.NewRecorder()
	fx.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"model_not_available"`) {
		t.Fatalf("expected model_not_available error: %s", rec.Body.String())
	}
}

func TestUsageIngestHasNoMessageContent(t *testing.T) {
	fx := setupChatTest(t)
	body, _ := json.Marshal(map[string]any{
		"model":    "gpt-4o-mini",
		"messages": []map[string]string{{"role": "user", "content": secretPrompt}},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+fx.token)
	rec := httptest.NewRecorder()
	fx.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		fx.ingestMu.Lock()
		got := len(fx.ingest) > 0
		payload := append([]byte(nil), fx.ingest...)
		fx.ingestMu.Unlock()
		if got {
			if strings.Contains(string(payload), secretPrompt) || strings.Contains(string(payload), secretReply) {
				t.Fatalf("usage ingest must not contain message content: %s", payload)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("usage ingest was not called")
}

func TestChatRedactionPrivacy(t *testing.T) {
	fx := setupChatTest(t)
	pii := "Reach me at alice@example.com or 4111111111111111"
	body, _ := json.Marshal(map[string]any{
		"model":    "gpt-4o-mini",
		"messages": []map[string]string{{"role": "user", "content": pii}},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+fx.token)
	rec := httptest.NewRecorder()
	fx.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if rec.Header().Get("X-Redaction-Count") == "" {
		t.Fatal("expected X-Redaction-Count header")
	}
	fx.providerMu.Lock()
	payload := string(fx.provider)
	fx.providerMu.Unlock()
	if strings.Contains(payload, "alice@example.com") || strings.Contains(payload, "4111111111111111") {
		t.Fatalf("provider received unredacted PII: %s", payload)
	}
	if !strings.Contains(payload, "[EMAIL]") {
		t.Fatalf("expected [EMAIL] placeholder in provider payload")
	}
	logs := fx.logs()
	if strings.Contains(logs, "alice@example.com") {
		t.Fatalf("PII leaked into logs")
	}
}

func TestChatStreamPrivacy(t *testing.T) {
	fx := setupChatTest(t)
	body, _ := json.Marshal(map[string]any{
		"model": "gpt-4o-mini", "stream": true,
		"messages": []map[string]string{{"role": "user", "content": secretPrompt}},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+fx.token)
	rec := httptest.NewRecorder()
	fx.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "data:") {
		t.Fatalf("expected SSE body")
	}
	if strings.Contains(fx.logs(), secretPrompt) {
		t.Fatalf("stream prompt leaked into logs")
	}
}

func TestChatTierDenied(t *testing.T) {
	fx := setupChatTest(t)
	fx.routeTierMin = "pro"
	token := fx.mintToken(t, "free", "free_standard")
	body := []byte(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	fx.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"model_tier_denied"`) {
		t.Fatalf("expected model_tier_denied error: %s", rec.Body.String())
	}
}

func TestChatRoutingUnavailable(t *testing.T) {
	fx := setupChatTest(t)
	fx.routingEmpty = true
	body := []byte(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+fx.token)
	rec := httptest.NewRecorder()
	fx.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestChatRateLimitExceeded(t *testing.T) {
	fx := setupChatTest(t)
	// free_standard allows 5/minute; use a pro tier so model access passes,
	// but a free bucket so the rate limiter trips on the 6th request.
	token := fx.mintToken(t, "pro", "free_standard")
	body := []byte(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`)
	var lastCode int
	for i := 0; i < 6; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		fx.mux.ServeHTTP(rec, req)
		lastCode = rec.Code
		if i == 5 {
			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("6th request status=%d body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"code":"rate_limit_exceeded"`) {
				t.Fatalf("expected rate_limit_exceeded: %s", rec.Body.String())
			}
			if rec.Header().Get("Retry-After") == "" {
				t.Fatalf("expected Retry-After header on 429")
			}
		}
	}
	if lastCode != http.StatusTooManyRequests {
		t.Fatalf("expected final 429, got %d", lastCode)
	}
}

func TestChatRateLimitFailClosed(t *testing.T) {
	// A dedicated handler wired to a fail-closed limiter over a dead Redis:
	// when Redis is unavailable, the request must be rejected (503), not
	// allowed through.
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kid := "test-kid"
	cache := auth.NewJWKSCache("http://unused")
	cache.SetPublicKey(kid, &priv.PublicKey)
	validator := auth.NewValidator(cache, testIssuer, testAudience)

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	mr.Close() // kill Redis so every command errors

	log := zerolog.New(io.Discard)
	core := coreclient.New("http://unused", "k", time.Minute)
	redactor, _ := redact.New(false, nil, "")
	h := handler.NewChat(log, core, ratelimit.New(rdb, log, true), router.New(), redactor)

	mux := chi.NewRouter()
	mux.With(auth.JWTMiddleware(validator)).Post("/v1/chat", h.Handle)

	now := time.Now()
	claims := auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: uuid.NewString(), Issuer: testIssuer,
			Audience: jwt.ClaimStrings{testAudience},
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
			ID: uuid.NewString(),
		},
		SubscriptionStatus: "active", SubscriptionTier: "pro", RateLimitBucket: "pro_standard",
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	signed, _ := tok.SignedString(priv)

	body := []byte(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+signed)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("fail-closed expected 503, got %d body=%s", rec.Code, rec.Body.String())
	}
}
