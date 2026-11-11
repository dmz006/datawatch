#!/usr/bin/env bash
# Prints one surface name per line, sorted, derived from code and docs.
# Usage: scripts/skills-coverage-extract.sh <mcp-tools|howtos|routes|commands|settings>
# ROOT redirects every source path (default: the repository root).
set -euo pipefail

ROOT="${ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"

die() { echo "skills-coverage-extract: $*" >&2; exit 1; }
need_dir()  { [ -d "$1" ] || die "missing source directory: $1"; }
need_file() { [ -f "$1" ] || die "missing source file: $1"; }

# Files of a directory (non-recursive) matching *.go, excluding tests.
go_files() { find "$1" -maxdepth 1 -name '*.go' ! -name '*_test.go' | sort; }

mcp_tools() {
  local dir="$ROOT/internal/mcp" golden="$ROOT/internal/mcp/testdata/registered_tool_names.golden"
  need_dir "$dir"
  {
    # Tool declarations: NewTool("name", ...) possibly with the name on the next line.
    go_files "$dir" | xargs -r cat | perl -0ne 'while (/NewTool\(\s*"([A-Za-z0-9_]+)"/g) { print "$1\n" }'
    # Golden list of registered names used by tests.
    [ -f "$golden" ] && grep -E '^[A-Za-z0-9_]+$' "$golden" || true
  } | LC_ALL=C sort -u
}

howtos() {
  local dir="$ROOT/docs/howto"
  need_dir "$dir"
  find "$dir" -maxdepth 1 -name '*.md' -printf '%f\n' \
    | sed 's/\.md$//' | grep -vix 'readme' | LC_ALL=C sort -u
}

routes() {
  local dir="$ROOT/internal/server"
  need_dir "$dir"
  go_files "$dir" | xargs -r cat \
    | perl -0ne 'while (/\.Handle(?:Func)?\(\s*"([^"]+)"/g) { print "$1\n" }' \
    | LC_ALL=C sort -u
}

commands() {
  local main="$ROOT/cmd/datawatch/main.go" doc="$ROOT/docs/commands.md"
  need_file "$main"; need_file "$doc"
  {
    # Top-level CLI commands: constructors listed in root.AddCommand( ... ), then their Use: value.
    # The block comes from main.go; the Use: values from every non-test file.
    perl -0e '
      my ($main, @rest) = @ARGV; my $src = "";
      for my $f ($main, @rest) { open(my $h, "<", $f) or next; local $/; $src .= <$h> . "\n"; }
      open(my $m, "<", $main) or exit 0; local $/; my $ms = <$m>;
      if ($ms =~ /root\.AddCommand\((.*?)\n\t\)/s) {
        for my $fn ($1 =~ /\b(new\w+Cmd)\(/g) {
          if ($src =~ /func \Q$fn\E\([^)]*\)[^{]*\{.*?Use:\s*"([^"\s]+)/s) { print "$1\n" }
        }
      }' "$main" $(find "$ROOT/cmd/datawatch" -maxdepth 1 -name '*.go' ! -name '*_test.go' ! -path "$main" | sort)
    # Messaging commands documented as `### `verb ...``.
    sed -nE 's/^### `([a-z][a-z-]*)[ :`].*/\1/p' "$doc"
  } | grep -vx 'datawatch' | LC_ALL=C sort -u
}

settings() {
  local f="$ROOT/docs/config-reference.yaml"
  need_file "$f"
  sed -nE 's/^([A-Za-z_][A-Za-z0-9_-]*):.*/\1/p' "$f" | LC_ALL=C sort -u
}

case "${1:-}" in
  mcp-tools) mcp_tools ;;
  howtos)    howtos ;;
  routes)    routes ;;
  commands)  commands ;;
  settings)  settings ;;
  *) echo "usage: $0 {mcp-tools|howtos|routes|commands|settings}" >&2; exit 2 ;;
esac
