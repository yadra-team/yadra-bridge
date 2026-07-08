# API Reference — Yadra Bridge (Proxy Service)

All public endpoints return JSON. Errors use a **single unified shape** (never plain text).

## Error contract

Every non-2xx response:

```json
{
  "error": {
    "code": "model_not_available",
    "message": "Human-readable explanation.",
    "details": { "model": "gpt-4o-mini" }
  }
}
```

### Error codes

| HTTP | `code` | When |
|------|--------|------|
| 401 | `unauthorized` | Missing or invalid JWT |
| 400 | `invalid_request` | Malformed body or missing required fields |
| 404 | `model_not_available` | Model not in admin routing config |
| 403 | `model_tier_denied` | Subscription tier too low for model |
| 429 | `rate_limit_exceeded` | Tier bucket limit hit (`Retry-After` header set) |
| 503 | `rate_limit_unavailable` | Redis unavailable (fail-closed) |
| 503 | `routing_unavailable` | Yadra Hub routing fetch failed; no admin config |
| 503 | `manifest_unavailable` | Yad manifest unavailable (Yadra Hub config or upstream CDN) |
| 502 | `manifest_invalid` | Upstream Yad manifest JSON invalid or empty |
| 502 | `provider_unavailable` | Upstream AI provider error |
| 504 | `provider_timeout` | Upstream timeout |
| 501 | `streaming_not_supported` | Provider lacks stream adapter (e.g. Gemini) |

**Privacy:** Error messages never include prompt/response content.

---

## `GET /health`

Liveness probe.

**Response 200:**

```json
{ "status": "ok", "version": "0.2.0" }
```

---

## `GET /ready`

Readiness probe — Redis, JWKS, and **admin routing config** must be healthy.

**Response 200:**

```json
{
  "status": "ready",
  "routing_updated_at": "2026-07-02T12:00:00Z",
  "routing_configured": true
}
```

**Response 503** when routing not configured (admin must enable models in Yadra Hub):

```json
{
  "status": "routing_not_configured",
  "routing_configured": false
}
```

---

## `POST /v1/chat`

OpenAI-compatible chat completion (buffered or SSE stream).

**Headers:**

| Header | Required |
|--------|----------|
| `Authorization: Bearer <jwt>` | Yes |
| `Content-Type: application/json` | Yes |

**Request body:**

```json
{
  "model": "gpt-4o-mini",
  "messages": [{ "role": "user", "content": "Hello" }],
  "stream": false,
  "temperature": 0.7,
  "max_tokens": 1024
}
```

**Routing:** Model must exist in Yadra Hub admin config (`GET /v1/internal/ai/routing`). There are **no hardcoded provider defaults** in the Proxy.

**Success 200 (non-stream):** OpenAI `chat.completion` object.

**Success 200 (stream):** `text/event-stream` — OpenAI SSE chunks.

**Response headers (success):**

| Header | Description |
|--------|-------------|
| `X-RateLimit-Limit` | Minute window limit |
| `X-RateLimit-Remaining` | Remaining requests |
| `X-RateLimit-Reset` | Unix timestamp |
| `X-Redaction-Count` | Server-side safety-net redactions (if any) |

---

## Routing freshness

The Proxy fetches provider/model config from Yadra Hub on every cache miss and via background refresh (`ROUTING_CACHE_SEC`, default **5 seconds**). Admin changes in the Yadra Hub panel propagate within one cache interval — no Proxy restart required.

Invalid or incomplete admin entries (missing API key, base URL, tier) are **skipped**, never used as silent defaults.

---

## `GET /v1/yad/manifest`

Public endpoint — **no JWT required**. Returns the Yad GGUF manifest JSON for Desktop fallback when the primary CDN is unreachable.

Proxy fetches the manifest from the URL configured in **Yadra Hub Admin → Settings → Yad models manifest** (default `https://models.yadra.app/manifest.json`), validates it, and caches the result.

**Response:** same shape as `models.yadra.app/manifest.json`:

```json
{
  "models": [{
    "version": "y0.5-base-qwen2.5-1.5b-q4",
    "url": "https://…",
    "sha256": "…",
    "sizeBytes": 1117320736,
    "minAppVersion": "0.1.0",
    "evalReportUrl": "https://models.yadra.app/reports/yad-y05-base.json"
  }]
}
```

**Privacy:** Only public model metadata is served. No user content is logged or stored.
