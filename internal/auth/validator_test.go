package auth_test

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/yadra-team/yadra-bridge/internal/auth"
)

const testIssuer = "https://api.yadra.app"
const testAudience = "proxy.yadra.app"

func testValidator(t *testing.T, pub *rsa.PublicKey, kid string) *auth.Validator {
	t.Helper()
	cache := auth.NewJWKSCache("http://unused")
	cache.SetPublicKey(kid, pub)
	return auth.NewValidator(cache, testIssuer, testAudience)
}

func signToken(t *testing.T, priv *rsa.PrivateKey, kid string, claims auth.Claims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	s, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

func validClaims(now time.Time) auth.Claims {
	return auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   uuid.NewString(),
			Issuer:    testIssuer,
			Audience:  jwt.ClaimStrings{testAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
			ID:        uuid.NewString(),
		},
		SubscriptionStatus: "active",
		SubscriptionTier:   "pro",
		RateLimitBucket:    "pro_standard",
	}
}

func TestValidatorAcceptsValidToken(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kid := "test-kid"
	v := testValidator(t, &priv.PublicKey, kid)
	now := time.Now()
	token := signToken(t, priv, kid, validClaims(now))

	got, err := v.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got.SubscriptionTier != "pro" {
		t.Fatalf("tier=%q", got.SubscriptionTier)
	}
}

func TestValidatorRejectsExpiredToken(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	kid := "test-kid"
	v := testValidator(t, &priv.PublicKey, kid)
	claims := validClaims(time.Now().Add(-2 * time.Hour))
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour))
	token := signToken(t, priv, kid, claims)

	if _, err := v.Validate(token); err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestValidatorRejectsWrongAudience(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	kid := "test-kid"
	v := testValidator(t, &priv.PublicKey, kid)
	claims := validClaims(time.Now())
	claims.Audience = jwt.ClaimStrings{"wrong.audience"}
	token := signToken(t, priv, kid, claims)

	if _, err := v.Validate(token); err == nil {
		t.Fatal("expected error for wrong audience")
	}
}

func TestValidatorRejectsInactiveSubscription(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	kid := "test-kid"
	v := testValidator(t, &priv.PublicKey, kid)
	claims := validClaims(time.Now())
	claims.SubscriptionStatus = "cancelled"
	token := signToken(t, priv, kid, claims)

	if _, err := v.Validate(token); err == nil {
		t.Fatal("expected error for inactive subscription")
	}
}

func TestValidatorRejectsWrongIssuer(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	kid := "test-kid"
	v := testValidator(t, &priv.PublicKey, kid)
	claims := validClaims(time.Now())
	claims.Issuer = "https://evil.example.com"
	token := signToken(t, priv, kid, claims)

	if _, err := v.Validate(token); err == nil {
		t.Fatal("expected error for wrong issuer")
	}
}

func TestValidatorAcceptsTrialSubscription(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	kid := "test-kid"
	v := testValidator(t, &priv.PublicKey, kid)
	claims := validClaims(time.Now())
	claims.SubscriptionStatus = "trial"
	claims.SubscriptionTier = "trial"
	token := signToken(t, priv, kid, claims)

	if _, err := v.Validate(token); err != nil {
		t.Fatalf("trial should be valid: %v", err)
	}
}
