package coreclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestResolveRouteUsesAdminConfig(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(RoutingConfig{
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
			Models: []RouteEntry{{
				ModelID: "mid-1", ExternalID: "gpt-4o-mini", ProviderID: "pid-1",
				ProviderType: "openai", ProviderName: "OpenAI", BaseURL: "https://api.openai.com/v1",
				APIKey: "sk-test", TierMin: "pro", InputCost1M: 0.1, OutputCost1M: 0.2,
			}},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "key", 5*time.Second)
	route, err := c.ResolveRoute(context.Background(), "gpt-4o-mini")
	if err != nil {
		t.Fatal(err)
	}
	if route.ProviderName != "OpenAI" || route.APIKey != "sk-test" {
		t.Fatalf("unexpected route: %+v", route)
	}
}

func TestResolveRouteNoDefaultWhenEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(RoutingConfig{
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
			Models:    nil,
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "key", time.Second)
	_, err := c.ResolveRoute(context.Background(), "gpt-4o-mini")
	if err != ErrRoutingEmpty {
		t.Fatalf("expected ErrRoutingEmpty, got %v", err)
	}
}

func TestResolveRouteModelNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(RoutingConfig{
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
			Models: []RouteEntry{{
				ModelID: "mid-1", ExternalID: "gpt-4o-mini", ProviderID: "pid-1",
				ProviderType: "openai", BaseURL: "https://api.openai.com/v1",
				APIKey: "sk-test", TierMin: "pro",
			}},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "key", time.Second)
	_, err := c.ResolveRoute(context.Background(), "unknown-model")
	if err != ErrModelNotFound {
		t.Fatalf("expected ErrModelNotFound, got %v", err)
	}
}

func TestRouteEntryValidRejectsIncomplete(t *testing.T) {
	e := RouteEntry{ExternalID: "m", ProviderID: "p", ProviderType: "openai", TierMin: "pro"}
	if e.Valid() == nil {
		t.Fatal("expected invalid without base url and api key")
	}
}
