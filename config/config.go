package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/yadra-team/yadra-bridge/internal/redact"
)

type Config struct {
	Addr                string
	CorePlatformURL     string
	InternalAPIKey      string
	JWTIssuer           string
	JWTAudience         string
	RedisAddr           string
	RoutingCacheSec     int
	TLSCert             string
	TLSKey              string
	RateLimitFailClosed bool
	ConnectTimeout      time.Duration
	ReadTimeout         time.Duration
	RedactionEnabled    bool
	RedactionCategories []redact.Category
	RedactionRulesFile  string
}

func Load() (Config, error) {
	categories := parseCategories(getenv("REDACTION_CATEGORIES", ""))
	cfg := Config{
		Addr:                getenv("PROXY_PORT", getenv("PORT", "8090")),
		CorePlatformURL:     getenv("CORE_PLATFORM_URL", "http://localhost:8080"),
		InternalAPIKey:      os.Getenv("INTERNAL_API_KEY"),
		JWTIssuer:           getenv("JWT_ISSUER", "https://api.yadra.app"),
		JWTAudience:         getenv("JWT_AUDIENCE", "proxy.yadra.app"),
		RedisAddr:           getenv("REDIS_ADDR", "localhost:6379"),
		RoutingCacheSec:     getenvInt("ROUTING_CACHE_SEC", 5),
		TLSCert:             os.Getenv("TLS_CERT"),
		TLSKey:              os.Getenv("TLS_KEY"),
		RateLimitFailClosed: getenvBool("RATE_LIMIT_FAIL_CLOSED", false),
		ConnectTimeout:      time.Duration(getenvInt("PROXY_CONNECT_TIMEOUT_SEC", 10)) * time.Second,
		ReadTimeout:         time.Duration(getenvInt("PROXY_READ_TIMEOUT_SEC", 120)) * time.Second,
		RedactionEnabled:    getenvBool("REDACTION_ENABLED", true),
		RedactionCategories: categories,
		RedactionRulesFile:  os.Getenv("REDACTION_RULES_FILE"),
	}
	if cfg.InternalAPIKey == "" {
		return cfg, fmt.Errorf("INTERNAL_API_KEY is required")
	}
	return cfg, nil
}

func parseCategories(raw string) []redact.Category {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]redact.Category, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, redact.Category(p))
		}
	}
	return out
}

func (c Config) ListenAddr() string {
	port, _ := strconv.Atoi(c.Addr)
	if port == 0 {
		return ":8090"
	}
	return fmt.Sprintf(":%d", port)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getenvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return fallback
}
