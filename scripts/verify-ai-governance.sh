#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$root"

fail() {
  printf 'ai governance violation: %s\n' "$1" >&2
  exit 1
}

require_file() {
  [[ -f "$1" ]] || fail "missing required file: $1"
}

require_file AGENTS.md
require_file CLAUDE.md
require_file CODEX.md
require_file .claude/rules/yadra-repo.md
require_file .cursor/rules/yadra-repo.mdc
require_file .grok/README.md
require_file .windsurfrules
require_file .clinerules
require_file .roo/rules/yadra-repo.md
require_file .github/workflows/ai-governance.yml

grep -q 'yadra-bridge' AGENTS.md || fail 'AGENTS.md must name yadra-bridge'
grep -q '@AGENTS.md' CLAUDE.md || fail 'CLAUDE.md must import AGENTS.md'

for file in CLAUDE.local.md Agents.md Agents.local.md AGENTS.local.md; do
  [[ ! -e "$file" ]] || fail "personal AI file found: $file"
done

if grep -RIn --exclude-dir=.git --exclude-dir=node_modules --exclude-dir=.svelte-kit --exclude=verify-ai-governance.sh 'status\.yarda\.app' AGENTS.md CLAUDE.md CODEX.md .claude .cursor .grok .roo .windsurfrules .clinerules README.md 2>/dev/null; then
  fail 'typo domain status.yarda.app found'
fi

printf 'ai governance ok\n'
