# Changelog

All notable changes to the **Yadra Bridge (Proxy Service)** are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).  
Versioning follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.0] - 2026-07-02

### Added

- Unified JSON error contract (`internal/apierr`) — all endpoints return `{ "error": { "code", "message", "details" } }`
- Real-time admin routing sync: 5s default cache, background refresh, `UpdatedAt` tracking
- `/ready` reports `routing_configured` and `routing_updated_at`
- Route validation — incomplete admin entries skipped; no provider defaults
- Unknown rate-limit buckets fail explicitly (no silent `free_standard` fallback)
- `docs/API.md`, `ROADMAP.md`, `CODE_OF_CONDUCT.md`
- Expanded CI: apierr tests, coreclient routing tests, integration tests
- SSE streaming pass-through for OpenAI-compatible providers (`stream: true`)
- Anthropic streaming translated to OpenAI SSE format for Desktop clients
- Server-side redaction safety net (`proxy/internal/redact/`) — defense-in-depth rule engine
- Built-in redaction categories: API keys, credit cards (Luhn), IBAN, email, phone, IP, national ID
- Config: `REDACTION_ENABLED`, `REDACTION_CATEGORIES`, `REDACTION_RULES_FILE`
- Rate limit response headers: `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`
- Redaction metadata: `X-Redaction-Count` header + usage ingest fields
- `GET /ready` probe (Redis + JWKS cache health)
- `RATE_LIMIT_FAIL_CLOSED` config for production fail-closed rate limiting
- Configurable provider timeouts (`PROXY_CONNECT_TIMEOUT_SEC`, `PROXY_READ_TIMEOUT_SEC`)
- Redaction accuracy tests: golden corpus, false-positive suite, fuzz tests
- Stream privacy tests

### Changed

- Rate limiter returns structured check result for header emission
- Chat handler applies in-memory redaction before provider forward (never logged)
- `model_not_available` returns **404** (was 403) with structured error
- `ROUTING_CACHE_SEC` default **5** (was 60) for faster admin config propagation

### Security

- Layered redaction model: Desktop primary + Proxy in-memory safety net (Doc #11 updated)
- Gemini streaming returns `501 Not Implemented` until adapter exists

## [0.1.0] - 2026-07-02

### Added

- JWT validation via Yadra Hub JWKS (RS256, subscription claims, no per-request Yadra Hub auth call)
- `POST /v1/chat` with OpenAI-compatible, Anthropic, and Gemini adapters
- Model routing whitelist from Yadra Hub `GET /v1/internal/ai/routing` cache
- Async usage ingest to Yadra Hub `POST /v1/internal/usage/ingest`
- Redis sliding-window rate limits (daily + per-minute by tier bucket)
- Metadata-only structured logging (zerolog)
- Privacy tests: assert prompts/responses never appear in logs or usage payloads
- CI: unit tests, race detector, golangci-lint, govulncheck, Docker build
- BUSL 1.1 license, SECURITY.md, CONTRIBUTING.md

### Security

- Rejects inactive subscriptions at JWT validation layer
- No message content in logs or usage events

[0.2.0]: https://github.com/yarda-team/yadra-bridge/releases/tag/v0.2.0
[0.1.0]: https://github.com/yarda-team/yadra-bridge/releases/tag/v0.1.0
