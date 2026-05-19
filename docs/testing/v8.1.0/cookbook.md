# E2E Test Cookbook — v8.1.0

**Version**: v8.1.0  
**Sprint**: T41 — Community Registry, Plugin Install, Alert Rules, Mic Overlay  
**Stories**: TS-627–TS-636 (10 tests)  
**Last Run**: —  
**Pass Rate**: — (0/10)  
**Status**: 📋 Ready to run

---

## T41 Results

| TS# | Description | Status | Notes |
|---|---|---|---|
| TS-627 | GET /api/alert-rules returns 200 and rules array | 📋 planned | — |
| TS-628 | POST /api/alert-rules creates rule; GET confirms; DELETE cleans up | 📋 planned | — |
| TS-629 | datawatch alert-rules list exits 0 | 📋 planned | — |
| TS-630 | datawatch alert-rules add exits 0; delete cleans up | 📋 planned | — |
| TS-631 | POST /api/alert-rules/{name}/enable and /disable return ok | 📋 planned | — |
| TS-632 | GET /api/alert-rules/firings returns 200 and firings array | 📋 planned | — |
| TS-633 | GET /api/plugins/browse?registry=community returns 200 | 📋 planned | skip if not connected |
| TS-634 | datawatch plugins browse-registry exits with usage if no arg | 📋 planned | — |
| TS-635 | datawatch skills registry list includes community registry | 📋 planned | — |
| TS-636 | GET /api/skills/registries returns community as first entry | 📋 planned | — |

---

## Feature Coverage

### S14b — Alert Rules

| Surface | Story | Expected |
|---|---|---|
| REST | TS-627, TS-628, TS-631, TS-632 | CRUD + enable/disable + firings |
| CLI | TS-629, TS-630 | list + add + delete |
| MCP | — | covered by MCP tool registration smoke |
| PWA | — | alert-rules card in Settings → Compute |

**Key test notes:**
- `POST /api/alert-rules` requires `name` + `condition.metric` + `condition.operator` + `condition.threshold` + `action.kind`
- Enable/disable use `POST /{name}/enable` and `POST /{name}/disable` (no body)
- Firings list is in-memory ring buffer — empty on fresh daemon, not an error
- 503 response = alert-rules store not initialized (check daemon startup logs)

### BL324/BL325 — Community Registry + Plugin Install

| Surface | Story | Expected |
|---|---|---|
| REST | TS-633, TS-636 | browse endpoint + community first in list |
| CLI | TS-634, TS-635 | usage validation + community in registry list |

**Key test notes:**
- TS-633 skips gracefully if community registry not yet connected (`datawatch skills registry connect community` required first)
- TS-635/636 check that community registry was auto-seeded at daemon start — should always pass on v8.1.0+
- `GET /api/plugins/browse` returns `{"registry":"...","plugins":[]}` (empty array) if registry not connected; NOT a 503

### BL326 — Mic Recording Overlay

No E2E stories — PWA-only CSS/JS UX feature. Manual verification: tap mic icon in a session, confirm animated waveform overlay appears, Cancel discards, Send submits.

---

## Run Commands

```bash
# Full T41 sprint
bash scripts/run-tests.sh --sprint=T41

# Alert rules slice only
bash scripts/run-tests.sh --feature=alert-rules

# Community registry slice only
bash scripts/run-tests.sh --feature=community-registry

# Single story
bash scripts/run-tests.sh --story=TS-627

# Skip LLM-gated tests
bash scripts/run-tests.sh --sprint=T41 --skip-conflict=llm
```

---

## How to Update This File

After each run, update the Status column:
- ✅ pass
- ❌ fail
- ⏭ skip
- 📋 planned (not yet run)

Update **Last Run** and **Pass Rate** at the top after each full run.

---

## New Patterns Introduced in T41

### Alert Rules CRUD pattern

```bash
# Create → verify → enable/disable → delete
rule_name="e2e-cpu-rule-$$"
create_resp=$(api POST /api/alert-rules "{
  \"name\": \"$rule_name\",
  \"condition\": {\"metric\": \"cpu_pct\", \"operator\": \">\", \"threshold\": 99},
  \"action\": {\"kind\": \"alert\"},
  \"enabled\": true
}")
# Verify
get_resp=$(api GET /api/alert-rules/$rule_name)
assert_json "$get_resp" 'd["name"] == "'"$rule_name"'"'
# Cleanup
api DELETE /api/alert-rules/$rule_name >/dev/null
```

### Community registry auto-seed check

```bash
regs=$(api GET /api/skills/registries)
first_name=$(echo "$regs" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d[0]['name'] if d else '')" 2>/dev/null)
[ "$first_name" = "community" ] && ok "community is first registry" || ko "expected community first, got: $first_name"
```

### Skip-if-not-connected pattern

```bash
browse=$(api GET "/api/plugins/browse?registry=community")
if echo "$browse" | grep -q "not connected\|not found\|404"; then
  skip "community registry not connected"
  return
fi
```
