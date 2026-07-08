package coreclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

var (
	ErrRoutingUnavailable  = errors.New("routing unavailable")
	ErrRoutingEmpty        = errors.New("routing empty")
	ErrModelNotFound       = errors.New("model not found")
	ErrInvalidRoute        = errors.New("invalid route entry")
	ErrPlatformUnavailable = errors.New("platform config unavailable")
)

type RouteEntry struct {
	ModelID      string  `json:"modelId"`
	ExternalID   string  `json:"externalId"`
	ProviderID   string  `json:"providerId"`
	ProviderType string  `json:"providerType"`
	ProviderName string  `json:"providerName"`
	BaseURL      string  `json:"baseUrl"`
	APIKey       string  `json:"apiKey"`
	TierMin      string  `json:"tierMin"`
	InputCost1M  float64 `json:"inputCostPer1m"`
	OutputCost1M float64 `json:"outputCostPer1m"`
}

func (e RouteEntry) Valid() error {
	if e.ExternalID == "" || e.ProviderID == "" {
		return fmt.Errorf("%w: missing model or provider id", ErrInvalidRoute)
	}
	if e.ProviderType == "" || e.BaseURL == "" || e.APIKey == "" {
		return fmt.Errorf("%w: incomplete provider config for model %s", ErrInvalidRoute, e.ExternalID)
	}
	if e.TierMin == "" {
		return fmt.Errorf("%w: missing tier_min for model %s", ErrInvalidRoute, e.ExternalID)
	}
	return nil
}

type RoutingConfig struct {
	Models    []RouteEntry `json:"models"`
	UpdatedAt string       `json:"updatedAt"`
}

type PlatformConfig struct {
	ModelsManifestURL string `json:"modelsManifestUrl"`
	UpdatedAt         string `json:"updatedAt"`
}

type UsageIngestEvent struct {
	TraceID             string   `json:"traceId"`
	SpanID              string   `json:"spanId"`
	ParentSpanID        string   `json:"parentSpanId,omitempty"`
	UserID              string   `json:"userId"`
	ModelExternalID     string   `json:"modelExternalId"`
	ProviderID          string   `json:"providerId,omitempty"`
	ModelID             string   `json:"modelId,omitempty"`
	InputTokens         int      `json:"inputTokens"`
	OutputTokens        int      `json:"outputTokens"`
	LatencyMs           int      `json:"latencyMs"`
	Status              string   `json:"status"`
	ErrorCode           string   `json:"errorCode,omitempty"`
	Tier                string   `json:"tier"`
	CostUSD             float64  `json:"costUsd"`
	StartedAt           string   `json:"startedAt"`
	EndedAt             string   `json:"endedAt"`
	RedactedCount       int      `json:"redactedCount,omitempty"`
	RedactionCategories []string `json:"redactionCategories,omitempty"`
}

type UsageIngestBatch struct {
	Events []UsageIngestEvent `json:"events"`
}

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	cacheTTL   time.Duration

	mu        sync.RWMutex
	routing   RoutingConfig
	expiresAt time.Time
	updatedAt string
	lastFetch time.Time
	fetchErr  error

	platformCfg       PlatformConfig
	platformExpiresAt time.Time
	platformUpdatedAt string
	platformLastFetch time.Time
	platformFetchErr  error
}

func New(baseURL, apiKey string, cacheTTL time.Duration) *Client {
	if cacheTTL <= 0 {
		cacheTTL = 5 * time.Second
	}
	return &Client{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		cacheTTL:   cacheTTL,
	}
}

// StartBackgroundRefresh keeps routing aligned with admin changes in Core.
func (c *Client) StartBackgroundRefresh(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(c.cacheTTL)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = c.refreshRouting(ctx)
				_, _ = c.refreshPlatformConfig(ctx)
			}
		}
	}()
}

func (c *Client) RoutingUpdatedAt() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.updatedAt
}

func (c *Client) LastFetchError() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.fetchErr
}

func (c *Client) GetRouting(ctx context.Context) (RoutingConfig, error) {
	c.mu.RLock()
	stale := time.Now().After(c.expiresAt)
	cached := c.routing
	c.mu.RUnlock()

	if !stale && len(cached.Models) > 0 {
		return cached, nil
	}
	return c.refreshRouting(ctx)
}

func (c *Client) refreshRouting(ctx context.Context) (RoutingConfig, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/internal/ai/routing", nil)
	if err != nil {
		c.setFetchError(err)
		return RoutingConfig{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.setFetchError(fmt.Errorf("%w: %v", ErrRoutingUnavailable, err))
		_, err := c.getCachedOrError()
		return RoutingConfig{}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		c.setFetchError(fmt.Errorf("%w: core status %d", ErrRoutingUnavailable, resp.StatusCode))
		_, err := c.getCachedOrError()
		return RoutingConfig{}, err
	}

	var cfg RoutingConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		c.setFetchError(err)
		_, err := c.getCachedOrError()
		return RoutingConfig{}, err
	}

	valid := make([]RouteEntry, 0, len(cfg.Models))
	for _, m := range cfg.Models {
		if err := m.Valid(); err != nil {
			continue
		}
		valid = append(valid, m)
	}
	cfg.Models = valid

	if len(cfg.Models) == 0 {
		c.setFetchError(ErrRoutingEmpty)
		c.mu.Lock()
		c.routing = RoutingConfig{Models: nil, UpdatedAt: cfg.UpdatedAt}
		c.updatedAt = cfg.UpdatedAt
		c.expiresAt = time.Now().Add(c.cacheTTL)
		c.lastFetch = time.Now()
		c.fetchErr = ErrRoutingEmpty
		c.mu.Unlock()
		return RoutingConfig{}, ErrRoutingEmpty
	}

	c.mu.Lock()
	if cfg.UpdatedAt != "" && cfg.UpdatedAt != c.updatedAt {
		c.routing = cfg
	} else if len(c.routing.Models) == 0 {
		c.routing = cfg
	} else {
		c.routing = cfg
	}
	c.updatedAt = cfg.UpdatedAt
	c.expiresAt = time.Now().Add(c.cacheTTL)
	c.lastFetch = time.Now()
	c.fetchErr = nil
	c.mu.Unlock()
	return cfg, nil
}

func (c *Client) setFetchError(err error) {
	c.mu.Lock()
	c.fetchErr = err
	c.mu.Unlock()
}

func (c *Client) getCachedOrError() (RoutingConfig, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.routing.Models) > 0 && time.Since(c.lastFetch) < 2*c.cacheTTL {
		return c.routing, nil
	}
	if c.fetchErr != nil {
		return RoutingConfig{}, c.fetchErr
	}
	return RoutingConfig{}, ErrRoutingUnavailable
}

func (c *Client) ResolveRoute(ctx context.Context, model string) (RouteEntry, error) {
	cfg, err := c.GetRouting(ctx)
	if err != nil {
		if errors.Is(err, ErrRoutingEmpty) {
			return RouteEntry{}, ErrRoutingEmpty
		}
		return RouteEntry{}, ErrRoutingUnavailable
	}
	for _, m := range cfg.Models {
		if m.ExternalID == model || m.ModelID == model {
			if err := m.Valid(); err != nil {
				return RouteEntry{}, ErrModelNotFound
			}
			return m, nil
		}
	}
	return RouteEntry{}, ErrModelNotFound
}

func (c *Client) IngestUsage(ctx context.Context, batch UsageIngestBatch) error {
	body, err := json.Marshal(batch)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/internal/usage/ingest", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("usage ingest status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) PlatformConfigUpdatedAt() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.platformUpdatedAt
}

func (c *Client) GetPlatformConfig(ctx context.Context) (PlatformConfig, error) {
	c.mu.RLock()
	stale := time.Now().After(c.platformExpiresAt)
	cached := c.platformCfg
	c.mu.RUnlock()

	if !stale && cached.ModelsManifestURL != "" {
		return cached, nil
	}
	return c.refreshPlatformConfig(ctx)
}

func (c *Client) ManifestConfigured() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.platformCfg.ModelsManifestURL != ""
}

func (c *Client) refreshPlatformConfig(ctx context.Context) (PlatformConfig, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/internal/platform/config", nil)
	if err != nil {
		c.setPlatformFetchError(err)
		return PlatformConfig{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.setPlatformFetchError(fmt.Errorf("%w: %v", ErrPlatformUnavailable, err))
		return c.getCachedPlatformOrError()
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		c.setPlatformFetchError(fmt.Errorf("%w: core status %d", ErrPlatformUnavailable, resp.StatusCode))
		return c.getCachedPlatformOrError()
	}

	var cfg PlatformConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		c.setPlatformFetchError(err)
		return c.getCachedPlatformOrError()
	}
	if cfg.ModelsManifestURL == "" {
		c.setPlatformFetchError(ErrPlatformUnavailable)
		return c.getCachedPlatformOrError()
	}

	c.mu.Lock()
	c.platformCfg = cfg
	c.platformUpdatedAt = cfg.UpdatedAt
	c.platformExpiresAt = time.Now().Add(c.cacheTTL)
	c.platformLastFetch = time.Now()
	c.platformFetchErr = nil
	c.mu.Unlock()
	return cfg, nil
}

func (c *Client) setPlatformFetchError(err error) {
	c.mu.Lock()
	c.platformFetchErr = err
	c.mu.Unlock()
}

func (c *Client) getCachedPlatformOrError() (PlatformConfig, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.platformCfg.ModelsManifestURL != "" && time.Since(c.platformLastFetch) < 2*c.cacheTTL {
		return c.platformCfg, nil
	}
	if c.platformFetchErr != nil {
		return PlatformConfig{}, c.platformFetchErr
	}
	return PlatformConfig{}, ErrPlatformUnavailable
}
