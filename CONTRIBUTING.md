# Contributing to Yadra Bridge

Thank you for improving the open-source Yadra Bridge (Proxy Service). Privacy and API consistency are non-negotiable.

## Before you start

1. Read [SECURITY.md](SECURITY.md), [docs/API.md](docs/API.md), and [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)
2. Never log `messages`, `content`, `prompt`, or `response`
3. All HTTP errors must use `internal/apierr` — **no plain-text `http.Error`**
4. No hardcoded AI providers or models — routing comes from Yadra Hub admin config only

## Local checks (run all before PR)

```bash
gofmt -w .
go vet ./...
go test ./... -race -count=1
go test -tags=integration ./internal/integration/... -race
golangci-lint run
gosec ./...
govulncheck ./...
go build -o /dev/null ./cmd/proxy
```

## Pull requests

1. Branch from `main`
2. One focused change per PR
3. Add/update tests — handler changes require privacy tests
4. Update [CHANGELOG.md](CHANGELOG.md) under `[Unreleased]`
5. Update [docs/API.md](docs/API.md) if HTTP contract changes
6. CI must pass (see `.github/workflows/ci.yml`)

## Code review checklist

- [ ] No user message content in logs, metrics, or Yadra Hub ingest
- [ ] JWT validated locally (JWKS) — no new per-request Yadra Hub auth calls
- [ ] Model routing from Yadra Hub only — no defaults/fallbacks for providers
- [ ] Errors use unified `{ "error": { "code", "message" } }` shape
- [ ] Rate limits enforced before provider forward
- [ ] New env vars documented in `.env.example` and README

## Versioning

- Semver in `VERSION`
- Tag: `git tag v0.2.0 && git push origin v0.2.0`
- Tag triggers [release workflow](.github/workflows/release.yml)

## License

Contributions accepted under [BUSL 1.1](LICENSE).
