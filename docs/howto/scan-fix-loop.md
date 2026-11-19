---
docs:
  index: true
  topics: [scan, fix loop, autonomous, automata, hold, retry, guardrails, quality gate]
exec_params:
  - {name: automaton_id, required: false, description: "Automaton ID to run the scan-fix loop on; leave empty to only read/update the loop settings"}
exec_steps:
  - tool: autonomous_scan_config_get
    description: Read the current scan config, fix_loop_enabled and fix_loop_max_retries included
    args: {}
    read_only: true
  - tool: autonomous_scan_config_set
    description: Enable the loop and set its retry budget
    args: {fix_loop_enabled: "true", fix_loop_max_retries: "{{params.fix_loop_max_retries}}"}
    read_only: false
  - tool: autonomous_prd_scan_fixloop
    description: Run the full scan-fix loop on the Automaton: scan, fix, re-scan, up to fix_loop_max_retries, then hold if it still fails
    args: {id: "{{params.automaton_id}}"}
    read_only: false
  - tool: autonomous_prd_scan_results
    description: Read the latest scan result (findings, per-scanner breakdown, verdict) the loop worked from
    args: {id: "{{params.automaton_id}}"}
    read_only: true
---

# How-to: Scan fix loop

A scan that fails is only half the answer: it tells you what is wrong, not
what happens next. The scan fix loop closes that loop on the Automaton's own
work. When it is enabled and a scan has findings at or above
`autonomous.scan.fail_on_severity`, the loop opens a corrective task for the
findings, executes it, re-scans, and goes again until the scan passes or the
retry budget is spent. If the budget runs out with the scan still failing, the
Automaton is put on hold with the remaining findings named in the hold reason —
so the work stops in a state where you can see exactly what it could not fix.

It is off by default (`autonomous.scan.fix_loop_enabled` is `false`), and when
off everything behaves as before: a scan is a scan, nothing else happens.

## The two settings

| Key | Default | Meaning |
|---|---|---|
| `autonomous.scan.fix_loop_enabled` | `false` | Master switch. When `true`, a failing scan can drive the fix loop. When `false`, a `scan/fixloop` call degrades to a plain scan — no fix task is opened and nothing is held. |
| `autonomous.scan.fix_loop_max_retries` | `0` | The retry budget: the maximum number of fix-and-rescan cycles. This is not the total number of scans — there is also the initial one. `0` means no fix attempts: a failing scan is held immediately. A negative value is treated as `0`. |

Both keys affect new loop runs; they do not change how a plain
`scan` / `scan/fix` call behaves. The threshold at which a scan counts as
failing is the separate `autonomous.scan.fail_on_severity` key (default
`error`); `max_findings`, `rules_grader_enabled` and the scanner toggles all
apply to the scans the loop performs too, because the loop uses the exact same
scan the surface uses.

## What one loop run does

1. **Scan.** The Automaton's project is scanned with the registered scanners
   (SAST, secrets, dependencies, and your project rules when they are
   configured). Findings at or above `fail_on_severity` make the scan fail.
2. **Pass?** If the scan passes, the loop ends right there with outcome `clean`.
   Nothing is opened, executed or held.
3. **Fix.** While the scan fails and attempts remain, the loop opens one
   corrective task: a child Automaton whose spec is built from the findings,
   linked to the scanned Automaton as its parent. The child starts from the
   parent's setup (backend, model, effort, project directory), is decomposed if
   it has no story yet, and its approval gates are opened with the loop's
   actor (`scan_fix_loop`) so the step can execute it.
4. **Execute.** The corrective task is run to a terminal outcome — the same
   executor pair operator-started runs use.
5. **Re-scan.** The scanned Automaton's project is scanned again. A passing
   re-scan ends the loop with outcome `fixed`; a failing one spends the attempt
   and the loop goes back to step 3 if budget remains.
6. **Hold.** When the budget is spent and the scan still fails, the scanned
   Automaton is put on hold. The hold reason is plain English and names the
   remaining findings — each one labelled with its rule ID, severity, location
   and message, for example:

   > the scan-fix loop spent all 3 of its retries and its scan left 2 findings: SAST-102 (error) in cmd/datawatch/main.go:88: connection string not over TLS; SECRET-01 (critical) in deploy/vars.env: AWS access key id.

   If there is too much to name in a hold reason, as many finding labels fit as
   fit and the rest is pointed at the scan report. Two hold edge cases: an
   Automaton that is already on hold keeps its existing hold — the loop does
   not replace that reason — and a completed Automaton cannot be held at all,
   so such a run ends with outcome `error` (release or re-open it first, then
   run the loop again). The loop's actor is
   `scan_fix_loop` in the decisions, and its trail — every opened fix Automaton
   and the final hold — is recorded on the scanned Automaton's decision log
   (`scan_fix_loop_open`, `scan_fix_loop_hold`).

The report for a run always comes back, regardless of outcome, with an `outcome`
of `clean` (scan passed), `fixed` (a re-scan passed after fix attempts), `held`
(budget spent, Automaton held with the reason), or `error` (a step could not even
be performed, e.g. the scan itself could not run, or the final hold could not be
set). Each attempt records the fix Automaton it opened and any error it hit.

### What the loop never does

- **It never overrides an operator gate.** The operator's plan gates (coverage,
  plan review, hold) stay in force on the corrective task: if one refuses, the
  refusal is recorded on the attempt and you see both the corrective task and
  the gate that stopped it.
- **It never touches unrelated Automata.** Only the scanned Automaton and the
  fix children it opens are affected.
- **It never invents work.** Every corrective task is built from the scan's own
  findings; a scan with no failing findings produces no task at all.
- **It never hides findings.** A failing scan that can or cannot be fixed ends
   in either a re-scan or a hold whose reason carries the findings — not in a
   completed-looking state.

## How to enable it

All the surfaces below write the same two keys; they apply to scan runs from
that moment on.

### YAML

```yaml
autonomous:
  scan:
    fail_on_severity: error          # the threshold a finding must reach to fail the scan (default)
    fix_loop_enabled: true           # enable the fix loop
    fix_loop_max_retries: 3          # fix-and-rescan cycles; 0 = hold immediately on a failing scan
```

Restart the daemon to pick up config-file changes (or set the keys live through
one of the runtime surfaces below, which apply without a restart).

### PWA

Settings → **Automata** → **Guardrail Library** card: flip the **Auto-fix loop**
toggle and set **Fix loop max retries** — each row saves the moment it
changes, there is no separate Save button to press.

To run it, open the Automaton's detail view: when the recorded scan result is
failing with one or more findings, the Scan tab's result panel carries
**Run Fix Loop** (the loop), **Create Fix Automaton** (one corrective
task), and **Propose AGENT.md Rules** (rule-edit proposals to prevent
recurrence) buttons. The loop's outcome arrives as a toast and the tab
re-renders the fresh scan result.

### REST

```sh
# Enable with a retry budget (partial body is fine — only the keys you send change)
curl -sk -X PUT https://127.0.0.1:302/api/autonomous/scan/config \
  -H 'Content-Type: application/json' \
  -d '{"fix_loop_enabled": true, "fix_loop_max_retries": 3}'

# Read the current scan config back
curl -sk https://127.0.0.1:302/api/autonomous/scan/config
```

The loop itself runs on the Automaton:

```sh
# Full loop: scan, fix, re-scan, hold if still failing
curl -sk -X POST https://127.0.0.1:302/api/autonomous/prds/<id>/scan/fixloop

# Plain scan only (never opens a fix task, never holds)
curl -sk -X POST https://127.0.0.1:302/api/autonomous/prds/<id>/scan

# One corrective task for the current findings, no loop
curl -sk -X POST https://127.0.0.1:302/api/autonomous/prds/<id>/scan/fix
```

When the loop is disabled, `scan/fixloop` behaves like a plain `scan` and the
daemon records a `scan_fixloop_disabled` audit event; a real loop run records a
`scan_fixloop` event.

### CLI

```sh
datawatch autonomous scan-config-set fix_loop_enabled=true fix_loop_max_retries=3
datawatch autonomous scan-config-get

datawatch autonomous prd-scan-fixloop <id>     # the loop
datawatch autonomous prd-scan <id>             # scan only
datawatch autonomous prd-scan-fix <id>         # one corrective task
datawatch autonomous prd-scan-rules <id>       # propose AGENT.md rule edits to prevent recurrence
```

### MCP

- `autonomous_scan_config_set` — sets the scan config; it takes
  `fix_loop_enabled` (boolean) and `fix_loop_max_retries` (integer) among the
  other scan keys.
- `autonomous_scan_config_get` — read the config back.
- `autonomous_prd_scan_fixloop` — run the loop on an Automaton.
- `autonomous_prd_scan_fix` — open one corrective task for the current findings.
- `autonomous_prd_scan`, `autonomous_prd_scan_results` — the underlying scan
  and its cached result.

Federation maps the three verbs to the standard autonomous capabilities: the
scan and its result are read, the single fix task is a write, the loop is a run.

### Comm channel

On a channel where your operator has the autonomous verbs, talk to it directly:

```
autonomous scan-fixloop <prd-id>      (aliases: scan_fixloop, scan_fix_loop)
autonomous scan-fix <prd-id>          (alias: scan_fix)
autonomous scan <prd-id>
```

Each reply carries the same JSON the REST endpoint returns.

## The single fix task (without the loop)

If you only want the corrective task and nothing else — say you'd rather watch
it run yourself — use `scan/fix` (REST), `prd-scan-fix` (CLI),
`autonomous_prd_scan_fix` (MCP), or `autonomous scan-fix <prd-id>` (comm). It
opens one child Automaton whose spec is built from the findings and links it to
the scanned Automaton; what it then does is up to you. It does not execute the
child on its own, re-scan, or hold, and it records a `scan_fix` audit event.

## Choosing the retry budget

- **`0`** — you still get the loop's discipline (a failing scan ends in a hold
  whose reason names the findings) without spending any automation attempts on
  the fix. Good for a first pass: you review the findings, then decide.
- **`1–3`** — the common range. Each attempt is a real child Automaton that
  runs to a terminal outcome, so budget one per fix-and-rescan cycle you expect
  to need; the scan is re-run after every attempt and a passing re-scan stops
  the loop early.
- **Higher** — only for findings that need several rounds (dependency bumps,
  multi-file cleanups). Remember the budget is a ceiling, not a forecast: a run
  that passes on the first re-scan used one attempt and stops.

The budget is per loop run, not shared across Automata or over time, and a
disabled loop spends none of it.

## Reading the outcome

- **The hold.** When the loop holds the Automaton, the hold reason is the
  first place to look: it names the remaining findings. Release or re-open the
  Automaton when you're ready and run another loop — or fix it by hand and
  re-scan.
- **The decision log.** `datawatch autonomous prd-get <id>` (or the Automaton
  detail view) shows the loop's trail: each `scan_fix_loop_open` entry names
  the findings the corrective task was opened for, and the final
  `scan_fix_loop_hold` entry carries the hold reason.
- **The fix Automata.** Each attempt's child is a normal Automaton, linked as a
  child of the scanned one; its own task history shows what the fix tried and
  what it produced.
- **The audit trail.** `scan_fixloop`, `scan_fixloop_disabled`, and `scan_fix`
  events on each path, so you can tell later which surface started what and
  what it returned.

## What it is not

- **Not `autonomous.infra_retry`.** Infrastructure retry is about the
  *machinery* failing (quota full, worker killed, daemon restart) and gives a
  task free re-attempts. The scan fix loop is about the *work* failing its own
  quality scan, and pays for each round with the fix budget, not with the
  task's failure budget.
- **Not the rules grader on its own.** `rules_grader_enabled` lets an LLM pass
  or fail findings; the loop reacts to the scan's verdict either way.
- **Not an approval override.** Approval gates on the corrective task belong to
  the operator, and the loop goes around them by recording the refusal, not by
  bypassing them.
