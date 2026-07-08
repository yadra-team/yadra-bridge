package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/yarda-team/yadra-bridge/internal/apierr"
	"github.com/yarda-team/yadra-bridge/internal/auth"
	"github.com/yarda-team/yadra-bridge/internal/coreclient"
	"github.com/yarda-team/yadra-bridge/internal/ratelimit"
	"github.com/yarda-team/yadra-bridge/internal/redact"
	"github.com/yarda-team/yadra-bridge/internal/router"
)

type Chat struct {
	log      zerolog.Logger
	core     *coreclient.Client
	limiter  *ratelimit.Limiter
	router   *router.Router
	redactor *redact.Engine
}

func NewChat(log zerolog.Logger, core *coreclient.Client, limiter *ratelimit.Limiter, r *router.Router, redactor *redact.Engine) *Chat {
	return &Chat{log: log, core: core, limiter: limiter, router: r, redactor: redactor}
}

func (h *Chat) Handle(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		apierr.WriteUnauthorized(w)
		return
	}

	rlResult, err := h.limiter.Check(r.Context(), claims.Subject, claims.RateLimitBucket)
	if err != nil {
		if le, ok := err.(*ratelimit.LimitError); ok {
			apierr.WriteRateLimitExceeded(w, int(le.RetryAfter.Seconds()))
			return
		}
		h.log.Warn().Err(err).Str("user_sub", claims.Subject).Str("bucket", claims.RateLimitBucket).Msg("rate limit check failed")
		apierr.WriteRateLimitUnavailable(w)
		return
	}
	if rlResult != nil {
		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(rlResult.Limit))
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(rlResult.Remaining))
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(rlResult.Reset.Unix(), 10))
	}

	var req router.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteInvalidRequest(w, "Request body must be valid JSON.", nil)
		return
	}
	if req.Model == "" || len(req.Messages) == 0 {
		apierr.WriteInvalidRequest(w, "Fields 'model' and 'messages' are required.", map[string]any{
			"required": []string{"model", "messages"},
		})
		return
	}

	redaction := h.applyRedaction(&req)
	if redaction.Count > 0 {
		w.Header().Set("X-Redaction-Count", strconv.Itoa(redaction.Count))
	}

	route, err := h.core.ResolveRoute(r.Context(), req.Model)
	if err != nil {
		switch {
		case errors.Is(err, coreclient.ErrRoutingEmpty), errors.Is(err, coreclient.ErrRoutingUnavailable):
			apierr.WriteRoutingUnavailable(w)
		default:
			apierr.WriteModelNotAvailable(w, req.Model)
		}
		return
	}
	if !router.TierAllows(claims.SubscriptionTier, route.TierMin) {
		apierr.WriteModelTierDenied(w, req.Model, claims.SubscriptionTier, route.TierMin)
		return
	}

	h.log.Info().
		Str("model", req.Model).
		Str("user_sub", claims.Subject).
		Str("tier", claims.SubscriptionTier).
		Str("provider_type", route.ProviderType).
		Str("provider_id", route.ProviderID).
		Bool("stream", req.Stream).
		Int("redacted_count", redaction.Count).
		Msg("routing request")

	if req.Stream {
		h.handleStream(w, r, claims, route, req, redaction)
		return
	}
	h.handleBuffered(w, r.Context(), claims, route, req, redaction)
}

func (h *Chat) handleBuffered(w http.ResponseWriter, ctx context.Context, claims *auth.Claims, route coreclient.RouteEntry, req router.ChatRequest, redaction redact.Result) {
	start := time.Now()
	result, err := h.router.Forward(ctx, route, req)
	latency := time.Since(start).Milliseconds()

	status := "ok"
	errCode := ""
	if err != nil {
		status = "error"
		errCode = mapProviderError(err)
		h.log.Error().
			Err(err).
			Str("model", req.Model).
			Str("user_sub", claims.Subject).
			Int64("latency_ms", latency).
			Msg("provider error")
		writeProviderError(w, err)
		go h.ingestUsage(claims, route, req.Model, result, status, errCode, redaction)
		return
	}

	go h.ingestUsage(claims, route, req.Model, result, status, errCode, redaction)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result.Response)
}

func (h *Chat) handleStream(w http.ResponseWriter, r *http.Request, claims *auth.Claims, route coreclient.RouteEntry, req router.ChatRequest, redaction redact.Result) {
	result, err := h.router.ForwardStream(r.Context(), route, req, w)
	status := "ok"
	errCode := ""
	if err != nil {
		status = "error"
		errCode = mapProviderError(err)
		h.log.Error().
			Err(err).
			Str("model", req.Model).
			Str("user_sub", claims.Subject).
			Msg("provider stream error")
		if !headersSent(w) {
			if strings.Contains(err.Error(), "not supported for gemini") {
				apierr.WriteStreamingNotSupported(w, route.ProviderType)
			} else {
				writeProviderError(w, err)
			}
		}
	}
	forward := router.ForwardResult{
		InputTokens:  result.InputTokens,
		OutputTokens: result.OutputTokens,
		LatencyMs:    result.LatencyMs,
		TraceID:      result.TraceID,
		SpanID:       result.SpanID,
		StartedAt:    result.StartedAt,
		EndedAt:      result.EndedAt,
		CostUSD:      result.CostUSD,
	}
	go h.ingestUsage(claims, route, req.Model, forward, status, errCode, redaction)
}

func writeProviderError(w http.ResponseWriter, err error) {
	msg := err.Error()
	if strings.Contains(msg, "504") || strings.Contains(msg, "timeout") {
		apierr.WriteProviderTimeout(w)
		return
	}
	apierr.WriteProviderUnavailable(w)
}

func mapProviderError(err error) string {
	if strings.Contains(err.Error(), "504") || strings.Contains(err.Error(), "timeout") {
		return apierr.CodeProviderTimeout
	}
	return apierr.CodeProviderUnavailable
}

func headersSent(w http.ResponseWriter) bool {
	rw, ok := w.(interface{ Size() int })
	if !ok {
		return false
	}
	return rw.Size() > 0
}

func (h *Chat) applyRedaction(req *router.ChatRequest) redact.Result {
	if h.redactor == nil || !h.redactor.Enabled() {
		return redact.Result{}
	}
	total := redact.Result{}
	for i := range req.Messages {
		text := contentAsString(req.Messages[i].Content)
		got := h.redactor.Redact(text)
		if got.Count > 0 {
			req.Messages[i].Content = got.Content
			total.Count += got.Count
			total.Categories = mergeCategories(total.Categories, got.Categories)
		}
	}
	return total
}

func mergeCategories(existing []redact.Category, add []redact.Category) []redact.Category {
	seen := make(map[redact.Category]bool, len(existing))
	for _, c := range existing {
		seen[c] = true
	}
	for _, c := range add {
		if !seen[c] {
			existing = append(existing, c)
			seen[c] = true
		}
	}
	return existing
}

func (h *Chat) ingestUsage(claims *auth.Claims, route coreclient.RouteEntry, model string, result router.ForwardResult, status, errCode string, redaction redact.Result) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cats := make([]string, 0, len(redaction.Categories))
	for _, c := range redaction.Categories {
		cats = append(cats, string(c))
	}
	evt := coreclient.UsageIngestEvent{
		TraceID:             result.TraceID,
		SpanID:              result.SpanID,
		UserID:              claims.Subject,
		ModelExternalID:     model,
		ProviderID:          route.ProviderID,
		ModelID:             route.ModelID,
		InputTokens:         result.InputTokens,
		OutputTokens:        result.OutputTokens,
		LatencyMs:           result.LatencyMs,
		Status:              status,
		ErrorCode:           errCode,
		Tier:                claims.SubscriptionTier,
		CostUSD:             result.CostUSD,
		StartedAt:           result.StartedAt.UTC().Format(time.RFC3339),
		EndedAt:             result.EndedAt.UTC().Format(time.RFC3339),
		RedactedCount:       redaction.Count,
		RedactionCategories: cats,
	}
	if err := h.core.IngestUsage(ctx, coreclient.UsageIngestBatch{Events: []coreclient.UsageIngestEvent{evt}}); err != nil {
		h.log.Warn().Err(err).Str("user_sub", claims.Subject).Msg("usage ingest failed")
	}
}

func contentAsString(content any) string {
	switch v := content.(type) {
	case string:
		return v
	default:
		b, _ := json.Marshal(content)
		return string(b)
	}
}
