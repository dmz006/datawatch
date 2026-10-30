# Datawatch App — gate-scope parity note (mobile)

The PWA now lets operators steer the quality-gate test scope: a global
default (full | touched-plus-smoke | touched) with a smoke-package list for
the `touched-plus-smoke` mode, plus a per-Automaton override. The mobile app
does not render any of this yet, and it must not break on the new fields.
This note is the parity brief for the next app pass.

## What shipped on the PWA

- Settings (Admin → Autonomous card):
  - `autonomous.default_quality_gates.scope` — select over
    `full | touched-plus-smoke | touched`; an unmade/empty selection is
    saved as `""` and the gate inherits the built-in default (`full`).
  - `autonomous.default_quality_gates.smoke_packages` — comma-separated
    Go package patterns, saved to the server as an array
    (e.g. `["internal/pipeline","internal/server"]`).
- Per-Automaton override: each PRD (Automaton) object carries an optional
  `quality_gates` object (`GET /api/autonomous/prds/{id}`) with the same
  `scope` + `smoke_packages` keys; set via
  `POST /api/autonomous/prds/{id}/set_quality_gates`. An empty scope
  inherits the global default; empty smoke inherits global, then the
  built-in list.
- Per-task display: a task's `quality_gate_result.scope_plan` records the
  decision (see below); the PWA shows it under each task. The PRD itself
  also carries `quality_gate_scope` (the scope actually in force).

## Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/api/config` | read `autonomous.default_quality_gates.{scope,smoke_packages}` (scope `string`, smoke `[]string`) |
| PUT | `/api/config` | save both; `scope` is a string (empty = inherit default), `smoke_packages` accepts a list **or** a comma-separated string |
| GET | `/api/autonomous/prds/{id}` | PRD object with optional `quality_gates` override |
| POST | `/api/autonomous/prds/{id}/set_quality_gates` | per-Automaton override: `{enabled, test_command, timeout, block_on_regression, scope, smoke_packages}`; invalid scope → 400 |

## Fields to parse

### Global defaults
```jsonc
"autonomous": {
  "default_quality_gates": {
    "enabled": true,
    "test_command": "go test ./...",
    "timeout": 600,
    "block_on_regression": true,
    "scope": "",                    // "" | "full" | "touched-plus-smoke" | "touched"
    "smoke_packages": ["internal/pipeline"]  // list, may be absent
  }
}
```

### Per-Automaton override (`{id}` = PRD id)
```jsonc
"quality_gates": {                 // absent = no override; inherits global
  "enabled": true,
  "test_command": "go test ./...",
  "timeout": 0,
  "block_on_regression": true,
  "scope": "",                     // "" | "full" | "touched-plus-smoke" | "touched";
                                   // empty inherits the global value
  "smoke_packages": ["internal/pipeline"]  // empty inherits global, then built-in
}
```

### Per-PRD / per-task
```jsonc
"quality_gate_scope": "touched-plus-smoke",   // the scope actually in force
"quality_gate_result": {
  "regression": false,
  "invalid": false,
  "scope_plan": {                    // ABSENT for full-suite runs / older tasks — tolerate missing
    "scope": "touched-plus-smoke",   // "full" | "touched-plus-smoke" | "touched"
    "scope_source": "global",        // "default" | "automaton" | "global"
    "full": false,
    "packages": ["internal/pipeline", "internal/server"],
    "reasons": [
      {"package": "internal/pipeline", "reason": "touched by changed files"},
      {"package": "internal/server", "reason": "smoke"}
    ],
    "full_reason": "",               // non-empty only when full=true
    "fallback_reason": "",           // set when a scoped run fell back to full
    "warnings": [],
    "nothing_to_run": false
  }
}
```

## Error shapes

- `PUT /api/config` with a bad scope → **400**:
  `unknown gate scope "x" (valid values: "full", "touched-plus-smoke", "touched")` —
  the whole PUT is rejected, nothing stored.
- `POST /api/autonomous/prds/{id}/set_quality_gates` with a bad scope → **400**
  naming the valid values (`full`, `touched-plus-smoke`, `touched`).
- Scoped gate whose scoped command fails to run (e.g. no `go`) → the gate
  itself still runs; the PWA records it in
  `scope_plan.warnings` / `fallback_reason` and runs the full suite. The
  app only needs to **display** those strings.

## Parity checklist (next app pass)

- [ ] Autonomous settings screen: scope picker (3 choices + "default (full)") and a smoke-package list field; send empty scope as `""`.
- [ ] Automaton detail (`GET /api/autonomous/prds/{id}`): show `quality_gate_scope` and
      the `quality_gates` override (scope + smoke packages); set/clear the
      per-Automaton scope via `set_quality_gates` (empty = inherit).
- [ ] Task view: render `scope_plan` (scope, source, packages, per-package reasons, fallback/warnings) when present; hide the section when absent.
- [ ] No regressions: both scope settings are optional — absence must mean "built-in default (full)".
