# Yadra Bridge

[![CI](https://github.com/yarda-team/yadra-bridge/actions/workflows/ci.yml/badge.svg)](https://github.com/yarda-team/yadra-bridge/actions/workflows/ci.yml)

Open-source [**BUSL 1.1**](LICENSE) Go service that routes authenticated Proxy Mode AI requests to providers configured by admins in Yadra Hub (Core Platform).

**Version:** see `VERSION` and `GET /health`.

## Why open source?

Yadra Bridge is auditable by design. You can verify it never logs prompts, always validates JWTs locally, and only forwards to admin-configured providers.

## Features

- **Admin-driven routing** — models, providers, API keys, tiers from Yadra Hub (`GET /v1/internal/ai/routing`); **no hardcoded defaults**
- **Fast config sync** — 5s cache + background refresh; admin changes propagate without restart
- **Unified API errors** — consistent JSON `{ "error": { "code", "message", "details" } }` on every endpoint
- **JWT auth** — RS256 via Yadra Hub JWKS; no per-request Yadra Hub auth call
- **SSE streaming** — OpenAI format; Anthropic translated
- **Redaction safety net** — in-memory rule pass before provider forward
- **Rate limits** — tier buckets with `X-RateLimit-*` headers
- **Privacy** — metadata-only logs; CI privacy tests mandatory

## Quick start

```bash
cd services/yadra-bridge
cp .env.example .env
# Yadra Hub must be running with admin-configured AI providers
go run ./cmd/proxy
```

```bash
curl http://localhost:8090/health
curl http://localhost:8090/ready   # fails until admin routing is configured
```

## Documentation

| Doc | Purpose |
|-----|---------|
| [docs/API.md](docs/API.md) | HTTP API + error contract |
| [CONTRIBUTING.md](CONTRIBUTING.md) | How to contribute |
| [SECURITY.md](SECURITY.md) | Vulnerability reporting + privacy invariants |
| [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) | Community standards |
| [ROADMAP.md](ROADMAP.md) | Release plan |
| [CHANGELOG.md](CHANGELOG.md) | Version history |

## Environment

| Variable | Default | Description |
|----------|---------|-------------|
| `CORE_PLATFORM_URL` | — | **Required.** Yadra Hub API base |
| `INTERNAL_API_KEY` | — | **Required.** Bearer for internal routes |
| `PROXY_PORT` / `PORT` | `8090` | Listen port (`PROXY_PORT` takes precedence) |
| `ROUTING_CACHE_SEC` | `5` | Routing refresh interval (admin config sync) |
| `REDACTION_ENABLED` | `true` | Server-side redaction safety net |
| `RATE_LIMIT_FAIL_CLOSED` | `false` | `true` in production |
| `REDIS_ADDR` | `localhost:6379` | Rate limit backend |

See [.env.example](.env.example) for the full list.

## Tests

```bash
go test ./... -race
go test -tags=integration ./internal/integration/... -race
gofmt -l .   # must be empty
golangci-lint run
gosec ./...
govulncheck ./...
```

## Architecture

```
Yadra Nest (JWT + redacted messages)
        │
        ▼
   Yadra Bridge
   ├─ Validate JWT (JWKS)
   ├─ Rate limit (Redis)
   ├─ Redact (safety net)
   ├─ Resolve route ← Yadra Hub admin config (fast cache)
   ├─ Forward → Provider
   └─ Ingest usage metadata → Yadra Hub
```

## License

[Business Source License 1.1](LICENSE) — converts to Apache 2.0 on **2030-07-02** (four years from adoption).
