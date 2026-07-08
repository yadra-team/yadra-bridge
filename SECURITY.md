# Security Policy

The Yadra Bridge (Proxy Service) is **open source (BUSL 1.1)** and privacy-critical.

## Supported versions

| Version | Supported |
| ------- | --------- |
| 0.2.x   | Yes       |
| 0.1.x   | Best effort |

## Reporting a vulnerability

Email **security@yadra.app** — do not file public issues for security bugs.

Include:

- Version (`GET /health` → `version`)
- Steps to reproduce
- Whether message content could leak via logs, storage, or third-party forwarding

We aim to acknowledge within **3 business days**.

## Privacy invariants (must never break)

1. **No content logging** — prompts and responses must not appear in logs
2. **No content persistence** — no database tables for message bodies
3. **Ephemeral processing** — discard request/response data after the HTTP response completes
4. **JWKS-only auth** — no synchronous Yadra Hub API call per chat request for authentication
5. **Admin routing only** — no hardcoded provider keys or model fallbacks
6. **Unified errors** — error responses must not echo user content

## CI enforcement

Every PR runs:

```bash
go test ./... -race
go test ./internal/handler/... -run 'Privacy|Redaction|Stream'
gosec ./...
govulncheck ./...
```

## Safe harbor

Good-faith security research following this policy will not be pursued legally, provided you do not access other users' data or disclose issues publicly before we publish a fix.

## Audit

Source is public for review: routing, redaction, logging, and ingest paths are the highest-risk areas.
