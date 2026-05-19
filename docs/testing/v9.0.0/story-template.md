# v9.0.0 Story Template

**Use this template when planning E2E stories for the v9.0 release sprint.**  
Next available TS#: **TS-637** (v8.1.0 closed at TS-636).

---

## Story file template

Copy to `scripts/test-stories/TS-NNN.sh`:

```bash
#!/usr/bin/env bash
# TS-NNN — <one-line description>
# tags: surface:<api|cli|mcp|pwa|comm|locale> feature:<feature-name> [group:<group>] [conflict:<tag>]
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-NNN"
story_preflight "surface:<tag> feature:<feature>" || return 0

_story_ts_NNN() {
  local resp

  # --- setup (create any resources needed) ---

  # --- act ---
  resp=$(api GET /api/<endpoint>)
  save_evidence TS-NNN "resp.json" "$resp"

  # --- assert ---
  if echo "$resp" | grep -q "503\|not available"; then
    skip "<feature> not enabled in this build"
    return
  fi

  if assert_json "$resp" 'd.get("key") is not None'; then
    ok "<what succeeded>"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi

  # --- cleanup ---
  api DELETE /api/<endpoint>/<id> >/dev/null 2>&1 || true
}

RESULT=fail
_story_ts_NNN
: "${RESULT:=fail}"
unset -f _story_ts_NNN
```

---

## Master cookbook row template

Add to `docs/testing/master-cookbook.md` table (replace T42 with actual sprint number):

```
| T42 | TS-NNN | <description> | surface:<tag> feature:<feature> group:<group> parallel:ok [conflict:<tag>] | ✅ ready | — | — |
```

---

## Cookbook file template

Create `docs/testing/v9.0.0/cookbook.md` when the sprint is planned:

```markdown
# E2E Test Cookbook — v9.0.0

**Version**: v9.0.0  
**Sprint**: T42 — <Sprint Name>  
**Stories**: TS-637–TS-NNN (N tests)  
**Last Run**: —  
**Pass Rate**: — (0/N)  
**Status**: 📋 Ready to run

---

## T42 Results

| TS# | Description | Status | Notes |
|---|---|---|---|
| TS-637 | <description> | 📋 planned | — |

---

## Feature Coverage

### <Feature Name>

| Surface | Story | Expected |
|---|---|---|
| REST | TS-NNN | <what> |
| CLI | TS-NNN | <what> |
| MCP | TS-NNN | <what> |

---

## Run Commands

```bash
bash scripts/run-tests.sh --sprint=T42
bash scripts/run-tests.sh --feature=<feature>
```
```

---

## Surface tag reference

| Tag | When to use |
|-----|-------------|
| `surface:api` | REST endpoint tests |
| `surface:cli` | `datawatch` binary subcommand tests |
| `surface:mcp` | MCP tool tests |
| `surface:pwa` | Browser/CDP tests against the web UI |
| `surface:comm` | Chat channel (Signal/Telegram/etc.) command tests |
| `surface:locale` | i18n key presence tests |

## Common conflict tags

| Tag | Meaning |
|-----|---------|
| `conflict:docker` | Requires Docker socket access |
| `conflict:llm` | Requires a live LLM backend (cost-gated: `DW_MAJOR=1`) |
| `conflict:signal` | Requires Signal account configured |
| `conflict:tailscale` | Requires Tailscale daemon |
| `conflict:k8s` | Requires `kubectl --context=testing` |
| `conflict:github` | Requires `gh` CLI authenticated |

## lib.sh helpers quick reference

```bash
api <METHOD> <path> [body]     # call daemon REST API; returns body
save_evidence TS-NNN file.ext "$data"  # persist to evidence dir
assert_json "$resp" 'python_expr'      # evaluate expr with d=parsed JSON
ok "message"    # pass
ko "message"    # fail
skip "reason"   # skip (counts as pass for non-blocking)
story_preflight "tags" || return 0  # check skip-conflict + feature flags
```

---

## Lessons from v7–v8 sprints (apply to v9.0)

1. **Always skip on 503** — new features return 503 when the backing store is nil. Don't ko, skip.
2. **Cleanup in test, not teardown** — each story manages its own cleanup inside the function. Deferred cleanup breaks with `--story=` single-run mode.
3. **Use `$$` in resource names** — e.g. `e2e-rule-$$` prevents name collisions in parallel runs.
4. **assert_json for structural checks** — use Python inline (`d["key"] == "value"`) not `grep`; grep misses nested fields and false-matches substrings.
5. **Skip if feature not configured** — gates (`DW_MAJOR=1`, `TEST_NTFY_TOPIC`, etc.) must be checked with `story_preflight`; don't require operators to set env vars to avoid test failures.
6. **One blocking story per feature group** — the health/smoke story in each group should be `⛔ blocking`; all others non-blocking. This prevents a broken feature from halting unrelated suites.
7. **E2E count check** — `TS-555` (howtos count ≥ 30) must be updated when new howtos are added. Update the threshold in TS-555.sh when crossing a multiple of 5.
