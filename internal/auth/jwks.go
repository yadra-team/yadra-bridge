package auth

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	jwt.RegisteredClaims
	SubscriptionStatus string `json:"subscription_status"`
	SubscriptionTier   string `json:"subscription_tier"`
	RateLimitBucket    string `json:"rate_limit_bucket"`
}

type JWKSCache struct {
	coreURL    string
	httpClient *http.Client
	mu         sync.RWMutex
	keys       map[string]*rsa.PublicKey
	fetchedAt  time.Time
	ttl        time.Duration
}

func NewJWKSCache(coreURL string) *JWKSCache {
	return &JWKSCache{
		coreURL:    strings.TrimRight(coreURL, "/"),
		httpClient: &http.Client{Timeout: 15 * time.Second},
		keys:       make(map[string]*rsa.PublicKey),
		ttl:        60 * time.Minute,
	}
}

func (c *JWKSCache) StartRefresh(ctx context.Context) {
	_ = c.refresh(ctx)
	ticker := time.NewTicker(c.ttl)
	go func() {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				_ = c.refresh(ctx)
			}
		}
	}()
}

func (c *JWKSCache) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.coreURL+"/.well-known/jwks.json", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Guard against bad responses (5xx/4xx). A non-200 with an empty or
	// unexpected body must never replace a previously-good key set.
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks fetch: unexpected status %d", resp.StatusCode)
	}

	var jwks struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return err
	}
	keys := make(map[string]*rsa.PublicKey)
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		key, err := rsaPublicKeyFromJWK(k.N, k.E)
		if err != nil {
			continue
		}
		keys[k.Kid] = key
	}

	// Never clobber a working cache with an empty result (e.g. a valid 200
	// that momentarily contains no usable keys during rotation).
	if len(keys) == 0 {
		return fmt.Errorf("jwks fetch: no usable keys in response")
	}

	c.mu.Lock()
	c.keys = keys
	c.fetchedAt = time.Now()
	c.mu.Unlock()
	return nil
}

func (c *JWKSCache) HasKeys() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.keys) > 0
}

func (c *JWKSCache) GetKey(kid string) (*rsa.PublicKey, error) {
	c.mu.RLock()
	key, ok := c.keys[kid]
	c.mu.RUnlock()
	if ok {
		return key, nil
	}
	return nil, fmt.Errorf("unknown key id")
}

// SetPublicKey installs a verification key (used in tests).
func (c *JWKSCache) SetPublicKey(kid string, pub *rsa.PublicKey) {
	c.mu.Lock()
	c.keys[kid] = pub
	c.fetchedAt = time.Now()
	c.mu.Unlock()
}

type Validator struct {
	cache    *JWKSCache
	issuer   string
	audience string
}

func NewValidator(cache *JWKSCache, issuer, audience string) *Validator {
	return &Validator{cache: cache, issuer: issuer, audience: audience}
}

func (v *Validator) Validate(tokenString string) (*Claims, error) {
	parser := jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}))
	token, err := parser.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		return v.cache.GetKey(kid)
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	if claims.Issuer != v.issuer {
		return nil, fmt.Errorf("invalid issuer")
	}
	audOK := false
	for _, aud := range claims.Audience {
		if aud == v.audience {
			audOK = true
			break
		}
	}
	if !audOK {
		return nil, fmt.Errorf("invalid audience")
	}
	if claims.SubscriptionStatus != "active" && claims.SubscriptionStatus != "trial" {
		return nil, fmt.Errorf("subscription inactive")
	}
	return claims, nil
}
