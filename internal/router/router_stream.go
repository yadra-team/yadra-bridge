package router

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yarda-team/yadra-bridge/internal/coreclient"
)

type StreamForwardResult struct {
	InputTokens  int
	OutputTokens int
	LatencyMs    int
	TraceID      string
	SpanID       string
	StartedAt    time.Time
	EndedAt      time.Time
	CostUSD      float64
}

type Options struct {
	ConnectTimeout time.Duration
	ReadTimeout    time.Duration
}

func NewWithOptions(opts Options) *Router {
	if opts.ConnectTimeout == 0 {
		opts.ConnectTimeout = 10 * time.Second
	}
	if opts.ReadTimeout == 0 {
		opts.ReadTimeout = 120 * time.Second
	}
	return &Router{
		httpClient: &http.Client{
			Timeout: opts.ReadTimeout + opts.ConnectTimeout,
		},
		connectTimeout: opts.ConnectTimeout,
		readTimeout:    opts.ReadTimeout,
	}
}

type Router struct {
	httpClient     *http.Client
	connectTimeout time.Duration
	readTimeout    time.Duration
}

func New() *Router {
	return NewWithOptions(Options{})
}

func (r *Router) ForwardStream(ctx context.Context, route coreclient.RouteEntry, req ChatRequest, w http.ResponseWriter) (StreamForwardResult, error) {
	started := time.Now()
	traceID := uuid.NewString()
	spanID := uuid.NewString()

	switch route.ProviderType {
	case "gemini":
		return StreamForwardResult{
			TraceID: traceID, SpanID: spanID, StartedAt: started, EndedAt: time.Now(),
		}, fmt.Errorf("streaming not supported for gemini")
	case "anthropic":
		return r.forwardAnthropicStream(ctx, route, req, w, traceID, spanID, started)
	default:
		return r.forwardOpenAIStream(ctx, route, req, w, traceID, spanID, started)
	}
}

func (r *Router) forwardOpenAIStream(ctx context.Context, route coreclient.RouteEntry, req ChatRequest, w http.ResponseWriter, traceID, spanID string, started time.Time) (StreamForwardResult, error) {
	payload := map[string]any{
		"model":    route.ExternalID,
		"messages": req.Messages,
		"stream":   true,
	}
	if req.Temperature != nil {
		payload["temperature"] = *req.Temperature
	}
	if req.MaxTokens != nil {
		payload["max_tokens"] = *req.MaxTokens
	}
	body, _ := json.Marshal(payload)
	url := strings.TrimRight(route.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return r.streamErr(traceID, spanID, started, err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+route.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := r.httpClient.Do(httpReq)
	if err != nil {
		return r.streamErr(traceID, spanID, started, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return r.streamErr(traceID, spanID, started, providerHTTPError(resp.StatusCode))
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		return r.streamErr(traceID, spanID, started, fmt.Errorf("streaming unsupported"))
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	outTok := 0
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if _, err := fmt.Fprintf(w, "%s\n", line); err != nil {
			break
		}
		flusher.Flush()
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				continue
			}
			var chunk map[string]any
			if json.Unmarshal([]byte(data), &chunk) == nil {
				outTok += extractStreamDelta(chunk)
			}
		}
	}
	ended := time.Now()
	inTok := estimateTokens(req.Messages)
	if outTok == 0 {
		outTok = 1
	}
	cost := (float64(inTok)/1_000_000)*route.InputCost1M + (float64(outTok)/1_000_000)*route.OutputCost1M
	return StreamForwardResult{
		InputTokens:  inTok,
		OutputTokens: outTok,
		LatencyMs:    int(ended.Sub(started).Milliseconds()),
		TraceID:      traceID,
		SpanID:       spanID,
		StartedAt:    started,
		EndedAt:      ended,
		CostUSD:      cost,
	}, nil
}

func (r *Router) forwardAnthropicStream(ctx context.Context, route coreclient.RouteEntry, req ChatRequest, w http.ResponseWriter, traceID, spanID string, started time.Time) (StreamForwardResult, error) {
	system, messages := splitAnthropicMessages(req.Messages)
	payload := map[string]any{
		"model":      route.ExternalID,
		"max_tokens": 4096,
		"messages":   messages,
		"stream":     true,
	}
	if system != "" {
		payload["system"] = system
	}
	if req.MaxTokens != nil {
		payload["max_tokens"] = *req.MaxTokens
	}
	body, _ := json.Marshal(payload)
	url := strings.TrimRight(route.BaseURL, "/") + "/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return r.streamErr(traceID, spanID, started, err)
	}
	httpReq.Header.Set("x-api-key", route.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := r.httpClient.Do(httpReq)
	if err != nil {
		return r.streamErr(traceID, spanID, started, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return r.streamErr(traceID, spanID, started, providerHTTPError(resp.StatusCode))
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		return r.streamErr(traceID, spanID, started, fmt.Errorf("streaming unsupported"))
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	outTok := 0
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "event:") {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		var evt struct {
			Type  string `json:"type"`
			Delta struct {
				Text string `json:"text"`
			} `json:"delta"`
		}
		if json.Unmarshal([]byte(data), &evt) != nil {
			continue
		}
		if evt.Type != "content_block_delta" || evt.Delta.Text == "" {
			continue
		}
		outTok += len(evt.Delta.Text) / 4
		openAIChunk := map[string]any{
			"id":      traceID,
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   route.ExternalID,
			"choices": []map[string]any{{
				"index": 0,
				"delta": map[string]string{"content": evt.Delta.Text},
			}},
		}
		b, _ := json.Marshal(openAIChunk)
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			break
		}
		flusher.Flush()
	}
	if _, err := fmt.Fprintf(w, "data: [DONE]\n\n"); err == nil {
		flusher.Flush()
	}

	ended := time.Now()
	inTok := estimateTokens(req.Messages)
	if outTok == 0 {
		outTok = 1
	}
	cost := (float64(inTok)/1_000_000)*route.InputCost1M + (float64(outTok)/1_000_000)*route.OutputCost1M
	return StreamForwardResult{
		InputTokens:  inTok,
		OutputTokens: outTok,
		LatencyMs:    int(ended.Sub(started).Milliseconds()),
		TraceID:      traceID,
		SpanID:       spanID,
		StartedAt:    started,
		EndedAt:      ended,
		CostUSD:      cost,
	}, nil
}

func extractStreamDelta(chunk map[string]any) int {
	choices, ok := chunk["choices"].([]any)
	if !ok || len(choices) == 0 {
		return 0
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return 0
	}
	delta, ok := choice["delta"].(map[string]any)
	if !ok {
		return 0
	}
	if content, ok := delta["content"].(string); ok {
		return len(content) / 4
	}
	return 0
}

func (r *Router) streamErr(traceID, spanID string, started time.Time, err error) (StreamForwardResult, error) {
	ended := time.Now()
	return StreamForwardResult{
		TraceID:   traceID,
		SpanID:    spanID,
		StartedAt: started,
		EndedAt:   ended,
		LatencyMs: int(ended.Sub(started).Milliseconds()),
	}, err
}

func providerHTTPError(code int) error {
	if code == 429 || code == 503 {
		return fmt.Errorf("provider retryable %d", code)
	}
	return fmt.Errorf("provider error %d", code)
}

// doWithRetry executes a provider request with optional single retry on 429/503.
func (r *Router) doRequest(req *http.Request) (*http.Response, error) {
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 429 && resp.StatusCode != 503 {
		return resp, nil
	}
	_ = resp.Body.Close()
	retryBody, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, providerHTTPError(resp.StatusCode)
	}
	retryReq := req.Clone(req.Context())
	retryReq.Body = io.NopCloser(bytes.NewReader(retryBody))
	return r.httpClient.Do(retryReq)
}
