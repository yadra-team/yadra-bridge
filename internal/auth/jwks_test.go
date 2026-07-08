package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A non-200 JWKS response must not clobber a previously-good key cache.
func TestJWKSRefreshBadStatusKeepsCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	c := NewJWKSCache(srv.URL)
	c.SetPublicKey("kid-1", &priv.PublicKey)

	if err := c.refresh(context.Background()); err == nil {
		t.Fatal("expected error on 500 response")
	}
	if !c.HasKeys() {
		t.Fatal("cache was wiped by a bad response")
	}
	if _, err := c.GetKey("kid-1"); err != nil {
		t.Fatalf("existing key lost: %v", err)
	}
}

// A 200 response with no usable keys must not clobber a good cache either.
func TestJWKSRefreshEmptyKeysKeepsCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"keys":[]}`))
	}))
	defer srv.Close()

	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	c := NewJWKSCache(srv.URL)
	c.SetPublicKey("kid-1", &priv.PublicKey)

	if err := c.refresh(context.Background()); err == nil {
		t.Fatal("expected error on empty key set")
	}
	if !c.HasKeys() {
		t.Fatal("cache was wiped by an empty response")
	}
}
