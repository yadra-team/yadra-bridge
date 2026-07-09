package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/yadra-team/yadra-bridge/internal/yadmanifest"
)

func TestYadManifestHandleUnavailable(t *testing.T) {
	svc := yadmanifest.New(nil, zerolog.Nop(), 0)
	h := NewYadManifest(svc)

	req := httptest.NewRequest(http.MethodGet, "/v1/yad/manifest", nil)
	rec := httptest.NewRecorder()
	h.Handle(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	errObj := body["error"].(map[string]any)
	if errObj["code"] != "manifest_unavailable" {
		t.Fatalf("code = %v", errObj["code"])
	}
}
