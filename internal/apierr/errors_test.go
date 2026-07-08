package apierr_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yarda-team/yadra-bridge/internal/apierr"
)

func TestErrorResponseShape(t *testing.T) {
	rec := httptest.NewRecorder()
	apierr.WriteModelNotAvailable(rec, "gpt-4o-mini")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d", rec.Code)
	}
	var body struct {
		Error struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != apierr.CodeModelNotAvailable {
		t.Fatalf("code=%s", body.Error.Code)
	}
	if body.Error.Message == "" {
		t.Fatal("message required")
	}
}
