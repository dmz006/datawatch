#!/usr/bin/env bash
# Shell tests for the skills coverage check. Builds a minimal fixture tree in a
# temporary directory, points the check and the extractor at it through ROOT,
# and prints PASS or FAIL per case. Exits non-zero when any case fails.
# Override the check under test with SKILLS_COVERAGE_CHECK=<path>.
set -uo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHECK="${SKILLS_COVERAGE_CHECK:-$REPO/scripts/skills-coverage-check.sh}"
EXTRACT="$REPO/scripts/skills-coverage-extract.sh"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT INT TERM

# Never let a nested tool reach the operator's tmux server.
run_check() { env -u TMUX -u TMUX_TMPDIR ROOT="$1" SKILLS_COVERAGE_EXTRACT="$EXTRACT" bash "$CHECK" >"$1/.out" 2>&1; }

# build_fixture <dir>: a tree where inventory, matrix, staging, registry and code agree.
build_fixture() {
  local r="$1"
  mkdir -p "$r/docs/skills/staging/alpha-skill" "$r/docs/howto" "$r/internal/mcp" \
           "$r/internal/server" "$r/cmd/datawatch"
  cat >"$r/internal/mcp/tools.go" <<'GO'
package mcp

func register() {
	NewTool("list_widgets", "list widgets")
	NewTool("get_widget", "get one widget")
}
GO
  printf '# Widgets\n' >"$r/docs/howto/widgets-basics.md"
  printf '# Index\n' >"$r/docs/howto/README.md"
  printf 'package server\n\nfunc routes(m *Mux) { m.HandleFunc("/api/widgets", h) }\n' >"$r/internal/server/routes.go"
  printf 'package main\n\nfunc main() {}\n' >"$r/cmd/datawatch/main.go"
  printf '# Commands\n\n### `widgets list`\n' >"$r/docs/commands.md"
  printf 'widgets:\n  enabled: true\n' >"$r/docs/config-reference.yaml"
  cat >"$r/docs/skills/inventory.yaml" <<'YAML'
entries:
- id: widgets
  area: Widgets
  summary: Widget handling.
  tools:
  - get_widget
  - list_widgets
  commands:
  - widgets
  routes:
  - /api/widgets
  settings:
  - widgets
  howtos:
  - widgets-basics
YAML
  cat >"$r/docs/skills/coverage-matrix.yaml" <<'YAML'
rows:
- entry: widgets
  skills: [alpha-skill]
  status: covered
  note: alpha-skill covers widget handling.
YAML
  printf '# Published community skill names, one per line.\nbeta-skill\n' >"$r/docs/skills/registry-skills.txt"
  printf '# Alpha\n' >"$r/docs/skills/staging/alpha-skill/SKILL.md"
}

failures=0
# expect <name> <pass|fail> <fixture dir>
expect() {
  local name="$1" want="$2" dir="$3" rc=0
  run_check "$dir" || rc=$?
  if { [ "$want" = pass ] && [ "$rc" -eq 0 ]; } || { [ "$want" = fail ] && [ "$rc" -ne 0 ]; }; then
    echo "PASS: $name"
  else
    echo "FAIL: $name (expected $want, exit $rc)"; sed 's/^/    /' "$dir/.out"
    failures=$((failures + 1))
  fi
}

new_case() { local d; d="$(mktemp -d "$WORK/case.XXXXXX")"; build_fixture "$d"; echo "$d"; }

if [ ! -f "$CHECK" ]; then echo "FAIL: check script not found: ${CHECK#"$REPO"/}"; exit 1; fi
if ! ROOT="$WORK" true || [ ! -f "$EXTRACT" ]; then echo "FAIL: extractor not found"; exit 1; fi

d="$(new_case)"
expect "clean fixture passes" pass "$d"

d="$(new_case)"
printf 'rows:\n' >"$d/docs/skills/coverage-matrix.yaml"
expect "deleting a matrix row fails" fail "$d"

d="$(new_case)"
sed -i 's/\[alpha-skill\]/[ghost-skill]/' "$d/docs/skills/coverage-matrix.yaml"
expect "matrix skill absent from staging and registry fails" fail "$d"

d="$(new_case)"
sed -i 's/get one widget")/get one widget")\n\tNewTool("delete_widget", "delete a widget")/' "$d/internal/mcp/tools.go"
expect "new MCP tool missing from the inventory fails" fail "$d"

d="$(new_case)"
printf '# Gadgets\n' >"$d/docs/howto/gadgets-basics.md"
expect "new how-to missing from the inventory fails" fail "$d"

if [ "$failures" -ne 0 ]; then echo "$failures case(s) failed"; exit 1; fi
echo "all cases passed"
