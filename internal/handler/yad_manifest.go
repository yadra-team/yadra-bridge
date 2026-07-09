package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/yadra-team/yadra-bridge/internal/apierr"
	"github.com/yadra-team/yadra-bridge/internal/yadmanifest"
)

type YadManifest struct {
	svc *yadmanifest.Service
}

func NewYadManifest(svc *yadmanifest.Service) *YadManifest {
	return &YadManifest{svc: svc}
}

func (h *YadManifest) Handle(w http.ResponseWriter, r *http.Request) {
	manifest, err := h.svc.Get(r.Context())
	if err != nil {
		if errors.Is(err, yadmanifest.ErrManifestInvalid) {
			apierr.WriteManifestInvalid(w)
			return
		}
		apierr.WriteManifestUnavailable(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=60")
	_ = json.NewEncoder(w).Encode(manifest)
}
