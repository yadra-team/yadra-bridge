# Yadra Bridge — Roadmap

Open-source (BUSL 1.1) AI routing service for Proxy Mode.

## Shipped (v0.2.x)

- [x] JWT validation via Yadra Hub JWKS (stateless RS256)
- [x] Admin-driven routing from Yadra Hub (no hardcoded providers)
- [x] Fast routing cache + background refresh (default 5s)
- [x] Unified JSON error contract
- [x] SSE streaming (OpenAI + Anthropic → OpenAI SSE)
- [x] Server-side redaction safety net
- [x] Rate limits with standard headers
- [x] `/health` + `/ready` (routing-aware)
- [x] Privacy test suite (no content in logs/ingest)
- [x] CI: test, race, lint, gosec, govulncheck, integration, Docker

## v0.3.0 (next)

- [ ] Gemini streaming adapter
- [ ] Webhook/push invalidation from Yadra Hub when admin saves routing (sub-second updates)
- [ ] OpenAPI 3.1 spec generated from docs
- [ ] Prometheus metrics (metadata only — no content labels)

## v1.0.0 (with Desktop GA)

- [ ] Production hardening at `proxy.yadra.app`
- [ ] SLO dashboards + on-call runbooks
- [ ] Formal third-party security audit of open-source tree
- [ ] BUSL → Apache 2.0 conversion date documented (Change Date 2030-07-03, see [LICENSE](LICENSE))

## Out of scope (by design)

- Storing or logging message content
- Per-request Yadra Hub auth round-trips
- User note/task storage
- Web-based note-taking client

See [ROADMAP](ROADMAP.md) issues on GitHub for tracked work items.
