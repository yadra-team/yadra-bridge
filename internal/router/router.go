package router

import (
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

var tierRank = map[string]int{
	"free":  0,
	"trial": 1,
	"pro":   2,
	"power": 3,
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
	Stream      bool          `json:"stream"`
}

type ChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type ForwardResult struct {
	Response     ChatResponse
	InputTokens  int
	OutputTokens int
	LatencyMs    int
	TraceID      string
	SpanID       string
	StartedAt    time.Time
	EndedAt      time.Time
	CostUSD      float64
}

func TierAllows(userTier, minTier string) bool {
	return tierRank[userTier] >= tierRank[minTier]
}

func (r *Router) Forward(ctx context.Context, route coreclient.RouteEntry, req ChatRequest) (ForwardResult, error) {
	started := time.Now()
	traceID := uuid.NewString()
	spanID := uuid.NewString()

	var resp ChatResponse
	var err error
	switch route.ProviderType {
	case "anthropic":
		resp, err = r.forwardAnthropic(ctx, route, req)
	case "gemini":
		resp, err = r.forwardGemini(ctx, route, req)
	default:
		resp, err = r.forwardOpenAICompatible(ctx, route, req)
	}
	ended := time.Now()
	latency := int(ended.Sub(started).Milliseconds())

	if err != nil {
		return ForwardResult{
			TraceID:   traceID,
			SpanID:    spanID,
			StartedAt: started,
			EndedAt:   ended,
			LatencyMs: latency,
		}, err
	}

	inTok := resp.Usage.PromptTokens
	outTok := resp.Usage.CompletionTokens
	if inTok == 0 {
		inTok = estimateTokens(req.Messages)
	}
	if outTok == 0 && len(resp.Choices) > 0 {
		outTok = len(resp.Choices[0].Message.Content) / 4
	}
	cost := (float64(inTok)/1_000_000)*route.InputCost1M + (float64(outTok)/1_000_000)*route.OutputCost1M

	return ForwardResult{
		Response:     resp,
		InputTokens:  inTok,
		OutputTokens: outTok,
		LatencyMs:    latency,
		TraceID:      traceID,
		SpanID:       spanID,
		StartedAt:    started,
		EndedAt:      ended,
		CostUSD:      cost,
	}, nil
}

func (r *Router) forwardOpenAICompatible(ctx context.Context, route coreclient.RouteEntry, req ChatRequest) (ChatResponse, error) {
	payload := map[string]any{
		"model":    route.ExternalID,
		"messages": req.Messages,
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
		return ChatResponse{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+route.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := r.httpClient.Do(httpReq)
	if err != nil {
		return ChatResponse{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return ChatResponse{}, fmt.Errorf("provider error %d", resp.StatusCode)
	}
	var out ChatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return ChatResponse{}, err
	}
	out.Model = route.ExternalID
	return out, nil
}

func (r *Router) forwardAnthropic(ctx context.Context, route coreclient.RouteEntry, req ChatRequest) (ChatResponse, error) {
	system, messages := splitAnthropicMessages(req.Messages)
	payload := map[string]any{
		"model":      route.ExternalID,
		"max_tokens": 4096,
		"messages":   messages,
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
		return ChatResponse{}, err
	}
	httpReq.Header.Set("x-api-key", route.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := r.httpClient.Do(httpReq)
	if err != nil {
		return ChatResponse{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return ChatResponse{}, fmt.Errorf("provider error %d", resp.StatusCode)
	}
	var anthropic struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &anthropic); err != nil {
		return ChatResponse{}, err
	}
	text := ""
	for _, c := range anthropic.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	out := ChatResponse{
		ID:      anthropic.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   route.ExternalID,
	}
	out.Choices = []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	}{
		{
			Index: 0,
			Message: struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}{Role: "assistant", Content: text},
			FinishReason: "stop",
		},
	}
	out.Usage.PromptTokens = anthropic.Usage.InputTokens
	out.Usage.CompletionTokens = anthropic.Usage.OutputTokens
	out.Usage.TotalTokens = anthropic.Usage.InputTokens + anthropic.Usage.OutputTokens
	return out, nil
}

func (r *Router) forwardGemini(ctx context.Context, route coreclient.RouteEntry, req ChatRequest) (ChatResponse, error) {
	contents := make([]map[string]any, 0, len(req.Messages))
	for _, m := range req.Messages {
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		text := messageText(m.Content)
		contents = append(contents, map[string]any{
			"role":  role,
			"parts": []map[string]string{{"text": text}},
		})
	}
	payload := map[string]any{"contents": contents}
	body, _ := json.Marshal(payload)
	modelPath := route.ExternalID
	if !strings.HasPrefix(modelPath, "models/") {
		modelPath = "models/" + modelPath
	}
	url := fmt.Sprintf("%s/%s:generateContent?key=%s",
		strings.TrimRight(route.BaseURL, "/"), modelPath, route.APIKey)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := r.httpClient.Do(httpReq)
	if err != nil {
		return ChatResponse{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return ChatResponse{}, fmt.Errorf("provider error %d", resp.StatusCode)
	}
	var gemini struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(raw, &gemini); err != nil {
		return ChatResponse{}, err
	}
	text := ""
	if len(gemini.Candidates) > 0 {
		for _, p := range gemini.Candidates[0].Content.Parts {
			text += p.Text
		}
	}
	out := ChatResponse{
		ID:      uuid.NewString(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   route.ExternalID,
	}
	out.Choices = []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	}{
		{
			Index: 0,
			Message: struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}{Role: "assistant", Content: text},
			FinishReason: "stop",
		},
	}
	out.Usage.PromptTokens = gemini.UsageMetadata.PromptTokenCount
	out.Usage.CompletionTokens = gemini.UsageMetadata.CandidatesTokenCount
	out.Usage.TotalTokens = out.Usage.PromptTokens + out.Usage.CompletionTokens
	return out, nil
}

func splitAnthropicMessages(msgs []ChatMessage) (string, []map[string]string) {
	system := ""
	out := make([]map[string]string, 0)
	for _, m := range msgs {
		if m.Role == "system" {
			system += messageText(m.Content) + "\n"
			continue
		}
		role := m.Role
		if role == "assistant" {
			role = "assistant"
		} else {
			role = "user"
		}
		out = append(out, map[string]string{"role": role, "content": messageText(m.Content)})
	}
	return strings.TrimSpace(system), out
}

func MessageText(content any) string {
	return messageText(content)
}

func messageText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var b strings.Builder
		for _, part := range v {
			if m, ok := part.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					b.WriteString(t)
				}
			}
		}
		return b.String()
	default:
		b, _ := json.Marshal(content)
		return string(b)
	}
}

func estimateTokens(msgs []ChatMessage) int {
	n := 0
	for _, m := range msgs {
		n += len(messageText(m.Content)) / 4
	}
	if n < 1 {
		return 1
	}
	return n
}
