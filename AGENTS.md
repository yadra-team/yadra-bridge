# yadra-bridge AI Rules

This repository is part of Yadra.

## Responsibility

Yadra Bridge owns the redacted external AI proxy and privacy boundary.

## Source Of Truth

- This file is the canonical project-local AI instruction file for `yadra-bridge`.
- Cross-project rules live in the root workspace `AGENTS.md` and `ai-context/`.
- Do not copy private user data, secrets, personal memory, or local-only paths into committed files.

## Workflow

- Work on feature branches; do not push directly to `main`, `preprod`, or `production`.
- Use squash merge through pull requests.
- Run this repository's validation before commit or push.
- For AI-assisted changes, include verification commands in the pull request.

## Validation

Run:

```bash
bash scripts/verify-ai-governance.sh
go test ./...
```
