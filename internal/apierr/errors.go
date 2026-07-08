package apierr

import (
	"encoding/json"
	"net/http"
)

// Standard error codes — stable contract for Desktop and integrators.
const (
	CodeUnauthorized          = "unauthorized"
	CodeInvalidRequest        = "invalid_request"
	CodeRateLimitExceeded     = "rate_limit_exceeded"
	CodeRateLimitUnavailable  = "rate_limit_unavailable"
	CodeRoutingUnavailable    = "routing_unavailable"
	CodeModelNotAvailable     = "model_not_available"
	CodeModelTierDenied       = "model_tier_denied"
	CodeProviderUnavailable   = "provider_unavailable"
	CodeProviderTimeout       = "provider_timeout"
	CodeStreamingNotSupported = "streaming_not_supported"
	CodeServiceUnavailable    = "service_unavailable"
	CodeInternalError         = "internal_error"
	CodeManifestUnavailable   = "manifest_unavailable"
	CodeManifestInvalid       = "manifest_invalid"
)

type Response struct {
	Error Detail `json:"error"`
}

type Detail struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func Write(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Response{
		Error: Detail{Code: code, Message: message, Details: details},
	})
}

func WriteUnauthorized(w http.ResponseWriter) {
	Write(w, http.StatusUnauthorized, CodeUnauthorized, "Valid Bearer token required.", nil)
}

func WriteInvalidRequest(w http.ResponseWriter, message string, details map[string]any) {
	if message == "" {
		message = "Request body or parameters are invalid."
	}
	Write(w, http.StatusBadRequest, CodeInvalidRequest, message, details)
}

func WriteRateLimitExceeded(w http.ResponseWriter, retryAfterSec int) {
	if retryAfterSec > 0 {
		w.Header().Set("Retry-After", itoa(retryAfterSec))
	}
	Write(w, http.StatusTooManyRequests, CodeRateLimitExceeded, "Rate limit exceeded for your subscription tier.", map[string]any{
		"retry_after_seconds": retryAfterSec,
	})
}

func WriteRateLimitUnavailable(w http.ResponseWriter) {
	Write(w, http.StatusServiceUnavailable, CodeRateLimitUnavailable, "Rate limiting service is temporarily unavailable.", nil)
}

func WriteRoutingUnavailable(w http.ResponseWriter) {
	Write(w, http.StatusServiceUnavailable, CodeRoutingUnavailable, "AI routing configuration is unavailable. Admin must configure providers in Core.", nil)
}

func WriteModelNotAvailable(w http.ResponseWriter, model string) {
	Write(w, http.StatusNotFound, CodeModelNotAvailable, "Requested model is not configured or not enabled for Proxy Mode.", map[string]any{
		"model": model,
	})
}

func WriteModelTierDenied(w http.ResponseWriter, model, tier, tierMin string) {
	Write(w, http.StatusForbidden, CodeModelTierDenied, "Your subscription tier does not include this model.", map[string]any{
		"model":    model,
		"tier":     tier,
		"tier_min": tierMin,
	})
}

func WriteProviderUnavailable(w http.ResponseWriter) {
	Write(w, http.StatusBadGateway, CodeProviderUnavailable, "Upstream AI provider is unavailable.", nil)
}

func WriteProviderTimeout(w http.ResponseWriter) {
	Write(w, http.StatusGatewayTimeout, CodeProviderTimeout, "Upstream AI provider timed out.", nil)
}

func WriteStreamingNotSupported(w http.ResponseWriter, providerType string) {
	Write(w, http.StatusNotImplemented, CodeStreamingNotSupported, "Streaming is not supported for this provider.", map[string]any{
		"provider_type": providerType,
	})
}

func WriteManifestUnavailable(w http.ResponseWriter) {
	Write(w, http.StatusServiceUnavailable, CodeManifestUnavailable, "Yad model manifest is temporarily unavailable.", nil)
}

func WriteManifestInvalid(w http.ResponseWriter) {
	Write(w, http.StatusBadGateway, CodeManifestInvalid, "Upstream Yad manifest is invalid or empty.", nil)
}

func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
