package yadmanifest

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/yadra-team/yadra-bridge/internal/coreclient"
)

var (
	ErrManifestUnavailable = errors.New("manifest unavailable")
	ErrManifestInvalid     = errors.New("manifest invalid")
)

type Entry struct {
	Version       string  `json:"version"`
	URL           string  `json:"url"`
	SHA256        string  `json:"sha256"`
	SizeBytes     uint64  `json:"sizeBytes"`
	MinAppVersion string  `json:"minAppVersion"`
	EvalReportURL *string `json:"evalReportUrl,omitempty"`
}

type Manifest struct {
	Models []Entry `json:"models"`
}

type Service struct {
	core       *coreclient.Client
	log        zerolog.Logger
	httpClient *http.Client
	cacheTTL   time.Duration

	mu        sync.RWMutex
	manifest  Manifest
	sourceURL string
	configKey string
	expiresAt time.Time
	lastFetch time.Time
}

func New(core *coreclient.Client, log zerolog.Logger, cacheTTL time.Duration) *Service {
	if cacheTTL <= 0 {
		cacheTTL = 60 * time.Second
	}
	return &Service{
		core:       core,
		log:        log,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		cacheTTL:   cacheTTL,
	}
}

func (s *Service) Configured() bool {
	return s.core != nil && s.core.ManifestConfigured()
}

func (s *Service) Get(ctx context.Context) (Manifest, error) {
	if s.core == nil {
		return s.cachedOrError()
	}
	platformCfg, err := s.core.GetPlatformConfig(ctx)
	if err != nil {
		return s.cachedOrError()
	}
	configKey := platformCfg.ModelsManifestURL + "|" + platformCfg.UpdatedAt

	s.mu.RLock()
	if time.Now().Before(s.expiresAt) && s.configKey == configKey && len(s.manifest.Models) > 0 {
		m := s.manifest
		s.mu.RUnlock()
		return m, nil
	}
	s.mu.RUnlock()

	manifest, err := s.fetchAndValidate(ctx, platformCfg.ModelsManifestURL)
	if err != nil {
		s.log.Warn().Err(err).Str("source_url", platformCfg.ModelsManifestURL).Msg("yad manifest fetch failed")
		if cached, cerr := s.cachedOrError(); cerr == nil {
			return cached, nil
		}
		return Manifest{}, err
	}

	s.mu.Lock()
	s.manifest = manifest
	s.sourceURL = platformCfg.ModelsManifestURL
	s.configKey = configKey
	s.expiresAt = time.Now().Add(s.cacheTTL)
	s.lastFetch = time.Now()
	s.mu.Unlock()

	s.log.Info().
		Str("source_url", platformCfg.ModelsManifestURL).
		Int("model_count", len(manifest.Models)).
		Msg("yad manifest refreshed")

	return manifest, nil
}

func (s *Service) cachedOrError() (Manifest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.manifest.Models) > 0 && time.Since(s.lastFetch) < 2*s.cacheTTL {
		return s.manifest, nil
	}
	return Manifest{}, ErrManifestUnavailable
}

func (s *Service) fetchAndValidate(ctx context.Context, sourceURL string) (Manifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrManifestUnavailable, err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrManifestUnavailable, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: read body: %v", ErrManifestUnavailable, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Manifest{}, fmt.Errorf("%w: upstream status %d", ErrManifestUnavailable, resp.StatusCode)
	}

	var manifest Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrManifestInvalid, err)
	}
	if err := validateManifest(manifest); err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrManifestInvalid, err)
	}
	return manifest, nil
}

func validateManifest(m Manifest) error {
	if len(m.Models) == 0 {
		return fmt.Errorf("models array is empty")
	}
	for i, e := range m.Models {
		if strings.TrimSpace(e.Version) == "" || strings.TrimSpace(e.URL) == "" {
			return fmt.Errorf("model[%d] missing version or url", i)
		}
		if e.MinAppVersion == "" {
			return fmt.Errorf("model[%d] missing minAppVersion", i)
		}
		if e.SHA256 != "" {
			if len(e.SHA256) != 64 {
				return fmt.Errorf("model[%d] sha256 must be 64 hex chars", i)
			}
			if _, err := hex.DecodeString(e.SHA256); err != nil {
				return fmt.Errorf("model[%d] sha256 is not valid hex", i)
			}
		}
	}
	return nil
}
