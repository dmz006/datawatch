# Lane fill eligibility — live check on a sandbox daemon

**Date:** 2026-11-01 (run started 15:33:11, local time)
**Host:** the sandbox host
**Binary version output:** `datawatch <release number omitted>` (built from this worktree at source revision `459322a52`)
**Script:** `scripts/lane-fill-eligibility-live-check.sh` (run as `env -u TMUX -u TMUX_TMPDIR bash scripts/lane-fill-eligibility-live-check.sh`)
**Scope:** the lane fill eligibility rules (stale, held, sequenced after, proposal, base branch, worktree), their read surfaces, the lane counts, the per-lane-kind mode and the pool evidence, against a throwaway project repository whose default branch is `trunk`; the default mode stays shadow throughout except where check 5 sets it on for one lane kind.

## Result

**Overall:** PASS

Check 1: PASS Defaults on a fresh config, the round trip and a bad mode

Check 2: PASS Refusals in shadow mode, one for each rule

Check 3: PASS The same refusals on every read surface

Check 4: PASS Lane counts follow the LLM in-flight caps

Check 5: PASS The per-lane-kind switch: gpu_mode on starts GPU work only

Check 6: PASS Pool evidence with an untrusted certificate

Check 7: PASS Shadow comparison

Check 8: PASS Cleanup

| # | Check | Result | Assertions |
|---|---|---|---|
| 1 | Defaults on a fresh config, the round trip and a bad mode | PASS | 5 of 5 passed |
| 2 | Refusals in shadow mode, one for each rule | PASS | 12 of 12 passed |
| 3 | The same refusals on every read surface | PASS | 3 of 3 passed |
| 4 | Lane counts follow the LLM in-flight caps | PASS | 3 of 3 passed |
| 5 | The per-lane-kind switch: gpu_mode on starts GPU work only | PASS | 7 of 7 passed |
| 6 | Pool evidence with an untrusted certificate | PASS | 4 of 4 passed |
| 7 | Shadow comparison | PASS | 4 of 4 passed |
| 8 | Cleanup | PASS | 3 of 3 passed |

## Setup

- **Sandbox daemon only.** Started with `td_start` from `scripts/test-daemon-lib.sh` from a config generated from `testdata/datawatch.yaml` by the substitutions the test runner uses (data folder, ports, loopback host, test token). Added: `autonomous.worktree_base_repo` and `autonomous.worktree_dir` (the throwaway project and its worktree root), `autonomous.lane_fill.poll_seconds` (5), `autonomous.questions.enabled`, `autonomous.verification_backend: shell`, `server.allowed_project_roots` and `shell_backend.script_path` (the stand-in worker). Every Ollama address in the config points at a loopback stub of the script and the test config's compute nodes are removed, so nothing leaves the machine. The production daemon, its service and its data were not touched, and nothing was ended by name.
- **Ports.** HTTP 35653, TLS 54531, MCP 44641, DNS 39117, webhook 55489, notify 54031, Ollama stub 60125, https stub 59181, each probed free just before use.
- **Session sockets.** The daemon ran with its multiplexer socket directory under the throwaway folder, so no session of the operator was reachable. The script itself issues no multiplexer command.
- **Throwaway project.** A bare repository whose default branch is `trunk`, cloned to the worktree base repository; `refs/remotes/origin/HEAD` records it and `main` does not exist. It holds a proposal file with a `Confirmed: yes` line and others without, and a task commit for one Automaton on the base branch.
- **Worker.** The daemon's shell backend runs a generated script as the worker, so a started Automaton runs headlessly with no model; the daemon's verification (a one-shot session on the same backend) is answered by the same script with a fixed pass.
- **The Automata** (written into the store before the first start): the eight kinds the check asks for, plus two candidates, (i) for a Claude lane and (j) for a GPU lane, whose proposals are confirmed in check 5 so a lane decision exists to watch.

| Automaton | Kind |
|---|---|
| `1f1100a1` | (a) stale draft |
| `1f1100b1` | (b) HOLD line |
| `1f1100c1` | (c) sequenced after an unfinished Automaton |
| `1f1100d1` | (d) proposal without the line |
| `1f1100e1` | (e) no proposal named |
| `1f1100f1` | (f) task commit on the base branch |
| `1f1100a7` | (g) worktree not registered |
| `1f1100c0` | (h) control, fully eligible |
| `1f1100d2` | (i) Claude-lane candidate, proposal confirmed in check 5 |
| `1f1100a8` | (j) GPU-lane candidate, proposal confirmed in check 5 |

```
refs/remotes/origin/trunk; main exists: no
```

## 1. Defaults on a fresh config, the round trip and a bad mode

`GET /api/config`, the lane fill block on a fresh config:
```
{
  "check_main_carries": true,
  "claude_backend": "claude-code",
  "claude_lanes": 3,
  "claude_mode": "",
  "count_verifier_wait": true,
  "decompose_ahead_min": 2,
  "gpu_backend": "pi-prompt",
  "gpu_lanes": 2,
  "gpu_mode": "",
  "gpu_model": "",
  "idle_alert_minutes": 30,
  "lane_counts_from_caps": true,
  "mode": "shadow",
  "poll_seconds": 5,
  "require_confirmed_proposal": true,
  "require_worktree": true,
  "stale_after_hours": 72
}
```

- **PASS** — the seven new keys read their defaults on a fresh config
  - stale_after_hours 72, require_confirmed_proposal, check_main_carries, require_worktree and lane_counts_from_caps true, gpu_mode and claude_mode empty; read: {"stale_after_hours":72,"require_confirmed_proposal":true,"check_main_carries":true,"require_worktree":true,"lane_counts_from_caps":true,"gpu_mode":"","claude_mode":""}

- **PASS** — lane fill mode is shadow for both lane kinds on a fresh config
  - status: {"mode":"shadow","modes":{"gpu":"shadow","claude":"shadow"}}; the generated config sets only the poll interval (5 seconds), no mode

- **PASS** — a PUT then GET round trip of all seven keys (48, false x4, off, shadow)
  - PUT HTTP 200; read: {"stale_after_hours":48,"require_confirmed_proposal":false,"check_main_carries":false,"require_worktree":false,"lane_counts_from_caps":false,"gpu_mode":"off","claude_mode":"shadow"}

- **PASS** — the defaults can be written back (empty modes inherit again)
  - PUT HTTP 200

- **PASS** — a bad gpu_mode (and claude_mode) is a 400 and saves nothing
  - gpu_mode: 400 autonomous.lane_fill.gpu_mode: "sometimes" is not a valid value (must be "off", "shadow" or "on")  — claude_mode: 400 autonomous.lane_fill.claude_mode: "sometimes" is not a valid value (must be "off", "shadow" or "on")  — gpu_mode after the refused write: `empty`

## 2. Refusals in shadow mode, one for each rule

Throwaway project: a bare repository whose default branch is `trunk` cloned to the worktree base repository (`refs/remotes/origin/HEAD` records it, `main` does not exist), holding one proposal file with a `Confirmed: yes` line and others without. The Automata were written into the sandbox store before its first start.

Base branch probe:
```
refs/remotes/origin/trunk; main exists: no
```

| Automaton | What it is | Decision | eligibility_reason | Reason text |
|---|---|---|---|---|
| `1f1100a1` | (a) stale draft | blocked | `stale` | Automaton last updated 101 hours ago, older than the 72 hour stale limit |
| `1f1100b1` | (b) HOLD line | blocked | `held` | a line starts with HOLD |
| `1f1100c1` | (c) sequenced after an unfinished Automaton | blocked | `sequenced_after_unshipped` | sequenced after 1f1100a1, which is draft and not completed |
| `1f1100d1` | (d) proposal without the line | blocked | `proposal_not_confirmed` | Automaton 1f1100d1: proposal docs/plans/unconfirmed-proposal.md does not carry a Confirmed: yes line |
| `1f1100e1` | (e) no proposal named | blocked | `proposal_missing` | Automaton 1f1100e1: the spec names no readable proposal file |
| `1f1100f1` | (f) task commit on the base branch | blocked | `main_carries_work` | the base branch already carries this work: commit 0b3fd2c on trunk: task(1f1100f1): Write the sample file |
| `1f1100a7` | (g) worktree not registered | blocked | `worktree_invalid` | its recorded worktree is not usable: the directory <sandbox>/phantom-worktree is not in the registered worktree list of the repository |
| `1f1100d2` | (i) Claude-lane candidate, proposal confirmed in check 5 | blocked | `proposal_not_confirmed` | Automaton 1f1100d2: proposal docs/plans/pending-claude.md does not carry a Confirmed: yes line |
| `1f1100a8` | (j) GPU-lane candidate, proposal confirmed in check 5 | blocked | `proposal_not_confirmed` | Automaton 1f1100a8: proposal docs/plans/pending-gpu.md does not carry a Confirmed: yes line |
| `1f1100c0` | (h) control, fully eligible | start_prd | — | first eligible work for idle gpu lane gpu-1 (gpu preference, priority 0); backend would change from shell to pi-prompt |

- **PASS** — (a) stale draft: a blocked decision with eligibility_reason `stale`
  - `1f1100a1`: {"action":"blocked","eligibility_reason":"stale","mode":"shadow","executed":false}, reason "Automaton last updated 101 hours ago, older than the 72 hour stale limit"

- **PASS** — (b) HOLD line: a blocked decision with eligibility_reason `held`
  - `1f1100b1`: {"action":"blocked","eligibility_reason":"held","mode":"shadow","executed":false}, reason "a line starts with HOLD"

- **PASS** — (c) sequenced after an unfinished Automaton: a blocked decision with eligibility_reason `sequenced_after_unshipped`
  - `1f1100c1`: {"action":"blocked","eligibility_reason":"sequenced_after_unshipped","mode":"shadow","executed":false}, reason "sequenced after 1f1100a1, which is draft and not completed"

- **PASS** — (d) proposal without the line: a blocked decision with eligibility_reason `proposal_not_confirmed`
  - `1f1100d1`: {"action":"blocked","eligibility_reason":"proposal_not_confirmed","mode":"shadow","executed":false}, reason "Automaton 1f1100d1: proposal docs/plans/unconfirmed-proposal.md does not carry a Confirmed: yes line"

- **PASS** — (e) no proposal named: a blocked decision with eligibility_reason `proposal_missing`
  - `1f1100e1`: {"action":"blocked","eligibility_reason":"proposal_missing","mode":"shadow","executed":false}, reason "Automaton 1f1100e1: the spec names no readable proposal file"

- **PASS** — (f) task commit on the base branch: a blocked decision with eligibility_reason `main_carries_work`
  - `1f1100f1`: {"action":"blocked","eligibility_reason":"main_carries_work","mode":"shadow","executed":false}, reason "the base branch already carries this work: commit 0b3fd2c on trunk: task(1f1100f1): Write the sample file"

- **PASS** — (g) worktree not registered: a blocked decision with eligibility_reason `worktree_invalid`
  - `1f1100a7`: {"action":"blocked","eligibility_reason":"worktree_invalid","mode":"shadow","executed":false}, reason "its recorded worktree is not usable: the directory <sandbox>/phantom-worktree is not in the registered worktree list of the repository"

- **PASS** — (i) Claude-lane candidate, proposal confirmed in check 5: refused as proposal_not_confirmed until the line is added
  - `1f1100d2`: {"action":"blocked","eligibility_reason":"proposal_not_confirmed"}

- **PASS** — (j) GPU-lane candidate, proposal confirmed in check 5: refused as proposal_not_confirmed until the line is added
  - `1f1100a8`: {"action":"blocked","eligibility_reason":"proposal_not_confirmed"}

- **PASS** — the control is the only start_prd decision
  - start_prd decisions 1, for: `1f1100c0`; {"lane_id":"gpu-1","lane_type":"gpu","action":"start_prd","mode":"shadow","executed":false}

- **PASS** — nothing was started in shadow mode (every decision executed false, no Automaton running, the worker never ran)
  - executed decisions 0; statuses: 1f1100a1=draft 1f1100b1=needs_review 1f1100c1=needs_review 1f1100d1=needs_review 1f1100e1=needs_review 1f1100f1=needs_review 1f1100a7=needs_review 1f1100c0=needs_review 1f1100d2=needs_review 1f1100a8=needs_review ; worker runs: `none`

- **PASS** — the queued eligible count is the control alone (gpu 1, claude 0)
  - {"gpu":1,"claude":0}

Refusal decisions as served by `GET /api/autonomous/lane-fill`:
```
[
  {
    "prd_id": "1f1100a1",
    "eligibility_reason": "stale",
    "mode": "shadow",
    "executed": false
  },
  {
    "prd_id": "1f1100f1",
    "eligibility_reason": "main_carries_work",
    "mode": "shadow",
    "executed": false
  },
  {
    "prd_id": "1f1100e1",
    "eligibility_reason": "proposal_missing",
    "mode": "shadow",
    "executed": false
  },
  {
    "prd_id": "1f1100d2",
    "eligibility_reason": "proposal_not_confirmed",
    "mode": "shadow",
    "executed": false
  },
  {
    "prd_id": "1f1100d1",
    "eligibility_reason": "proposal_not_confirmed",
    "mode": "shadow",
    "executed": false
  },
  {
    "prd_id": "1f1100c1",
    "eligibility_reason": "sequenced_after_unshipped",
    "mode": "shadow",
    "executed": false
  },
  {
    "prd_id": "1f1100b1",
    "eligibility_reason": "held",
    "mode": "shadow",
    "executed": false
  },
  {
    "prd_id": "1f1100a8",
    "eligibility_reason": "proposal_not_confirmed",
    "mode": "shadow",
    "executed": false
  },
  {
    "prd_id": "1f1100a7",
    "eligibility_reason": "worktree_invalid",
    "mode": "shadow",
    "executed": false
  }
]
```

## 3. The same refusals on every read surface

CLI text (`datawatch autonomous lane-fill`), refusal lines:
```
Lane fill: mode shadow
Lane counts: gpu 2 of 2 (config), claude 1 of 3 (LLM caps)
Queued eligible: gpu 1, claude 0
  15:33:14  blocked 1f1100a1: [stale] Automaton last updated 101 hours ago, older than the 72 hour stale limit [not executed]
  15:33:14  blocked 1f1100f1: [main_carries_work] the base branch already carries this work: commit 0b3fd2c on trunk: task(1f1100f1): Write the sample file [not executed]
  15:33:14  blocked 1f1100e1: [proposal_missing] Automaton 1f1100e1: the spec names no readable proposal file [not executed]
  15:33:14  blocked 1f1100d2: [proposal_not_confirmed] Automaton 1f1100d2: proposal docs/plans/pending-claude.md does not carry a Confirmed: yes line [not executed]
  15:33:14  blocked 1f1100d1: [proposal_not_confirmed] Automaton 1f1100d1: proposal docs/plans/unconfirmed-proposal.md does not carry a Confirmed: yes line [not executed]
  15:33:14  blocked 1f1100c1: [sequenced_after_unshipped] sequenced after 1f1100a1, which is draft and not completed [not executed]
  15:33:14  blocked 1f1100b1: [held] a line starts with HOLD [not executed]
  15:33:14  blocked 1f1100a8: [proposal_not_confirmed] Automaton 1f1100a8: proposal docs/plans/pending-gpu.md does not carry a Confirmed: yes line [not executed]
  15:33:14  blocked 1f1100a7: [worktree_invalid] its recorded worktree is not usable: the directory <sandbox>/phantom-worktree is not in the registered worktree list of the repository [not executed]
```

Chat reply (`POST /api/test/message`, text `autonomous lane-fill 300`; the chat verb shows the newest five decisions by design):
```
[dw-e2e-test] autonomous lane-fill
Lane fill: mode shadow
gpu-1: idle, 1 eligible waiting
gpu-2: idle, 1 eligible waiting
claude-1: idle
Lane counts: gpu 2 of 2 (config), claude 1 of 3 (LLM caps)
Queued eligible: gpu 1, claude 0
Newest decisions:
  15:33:14  blocked 1f1100a1: [stale] Automaton last updated 101 hours ago, older than the 72 hour stale limit [not executed]
  15:33:14  blocked 1f1100f1: [main_carries_work] the base branch already carries this work: commit 0b3fd2c on trunk: task(1f1100f1): Write the sample file [not executed]
  15:33:14  blocked 1f1100e1: [proposal_missing] Automaton 1f1100e1: the spec names no readable proposal file [not executed]
  15:33:14  blocked 1f1100d2: [proposal_not_confirmed] Automaton 1f1100d2: proposal docs/plans/pending-claude.md does not carry a Confirmed: yes line [not executed]
  15:33:14  blocked 1f1100d1: [proposal_not_confirmed] Automaton 1f1100d1: proposal docs/plans/unconfirmed-proposal.md does not carry a Confirmed: yes line [not executed]
```

MCP result (`autonomous_lane_fill_status`), refusals only:
```
[{"prd_id":"1f1100b1","eligibility_reason":"held"},{"prd_id":"1f1100f1","eligibility_reason":"main_carries_work"},{"prd_id":"1f1100e1","eligibility_reason":"proposal_missing"},{"prd_id":"1f1100a8","eligibility_reason":"proposal_not_confirmed"},{"prd_id":"1f1100d1","eligibility_reason":"proposal_not_confirmed"},{"prd_id":"1f1100d2","eligibility_reason":"proposal_not_confirmed"},{"prd_id":"1f1100c1","eligibility_reason":"sequenced_after_unshipped"},{"prd_id":"1f1100a1","eligibility_reason":"stale"},{"prd_id":"1f1100a7","eligibility_reason":"worktree_invalid"}]
```

- **PASS** — REST, the MCP tool, the CLI (--json and text) and the chat verb carry the same refusals, lane counts and modes
  - comparison: `OK`; the chat reply is held to the newest five decisions it shows

- **PASS** — the served app.js maps all seven refusal codes to chip keys and carries the lane counts and per-kind mode keys
  - lane_fill_eligibility_stale lane_fill_eligibility_held lane_fill_eligibility_sequenced_after_unshipped lane_fill_eligibility_proposal_not_confirmed lane_fill_eligibility_proposal_missing lane_fill_eligibility_main_carries_work lane_fill_eligibility_worktree_invalid 

- **PASS** — the five locale bundles (en, de, es, fr, ja) carry the seven chip keys and the lane counts and per-kind mode keys
  - en: all keys present; de: all keys present; es: all keys present; fr: all keys present; ja: all keys present; 

## 4. Lane counts follow the LLM in-flight caps

Claude backend `claude-code`, claude_lanes `3`; LLM entry updated with max_inflight 1: `200 {"name":"claude-code","ok":true} `.

- **PASS** — with the Claude entry capped at 1 and claude_lanes 3, lane_counts shows effective 1, source llm_caps
  - lane_counts.claude: {"configured":3,"effective":1,"source":"llm_caps"}; Claude lanes in the status: 1

- **PASS** — with lane_counts_from_caps false it shows effective 3, source config
  - lane_counts.claude: {"configured":3,"effective":3,"source":"config","reason":"lane_counts_from_caps is off"}; reason: lane_counts_from_caps is off

- **PASS** — the CLI line says it plainly: claude 1 of 3 (LLM caps)
  - `Lane counts: gpu 2 of 2 (config), claude 1 of 3 (LLM caps)`

## 5. The per-lane-kind switch: gpu_mode on starts GPU work only

- **PASS** — gpu_backend is the daemon's shell backend; claude_mode is empty and the base mode is shadow
  - gpu_backend `shell`, claude_mode ``, mode `shadow`

Status after gpu_mode on (modes, control decision):
```
{"mode":"shadow","modes":{"gpu":"on","claude":"shadow"}}
{"at":"2026-11-01T15:35:14.461307282-05:00","mode":"on","lane_id":"gpu-1","lane_type":"gpu","action":"start_prd","prd_id":"1f1100c0","title":"control","reason":"first eligible work for idle gpu lane gpu-1 (gpu preference, priority 0)","blocker":"","executed":true,"error":"","repeat_count":1,"last_seen_at":"2026-11-01T15:35:14.461307282-05:00"}
```

- **PASS** — the status shows gpu on, claude shadow, base mode shadow
  - {"mode":"shadow","modes":{"gpu":"on","claude":"shadow"}}

- **PASS** — the control starts on a GPU lane (its decision is executed in mode on)
  - Automaton `1f1100c0` status `running`; decision {"lane_id":"gpu-1","lane_type":"gpu","mode":"on","executed":true}; worker runs: `1f1100c0 `

- **PASS** — a Claude-lane decision exists and stays unexecuted (mode shadow) over three polls
  - Automaton `1f1100d2` status `needs_review`; decision {"lane_id":"claude-1","lane_type":"claude","mode":"shadow","executed":false}

- **PASS** — none of the refused Automata started (statuses unchanged, the worker ran for the control only)
  - refused statuses: 1f1100a1=draft 1f1100b1=needs_review 1f1100c1=needs_review 1f1100d1=needs_review 1f1100e1=needs_review 1f1100f1=needs_review 1f1100a7=needs_review ; worker runs (count id): `      1 1f1100c0 `

- **PASS** — the control's run ends and frees its GPU lane (the stand-in worker ran and its verification passed)
  - Automaton `1f1100c0` status `completed`; worker runs: `1f1100c0 `; GPU lanes: [{"id":"gpu-1","state":"idle"}]

- **PASS** — with gpu_mode back to shadow an eligible GPU candidate gets a decision and is not started at the next polls
  - Automaton `1f1100a8` status `needs_review`; decision {"lane_id":"gpu-1","lane_type":"gpu","mode":"shadow","executed":false}; modes {"gpu":"shadow","claude":"shadow"}

## 6. Pool evidence with an untrusted certificate

A compute node `tls-stub-node` on a loopback https stub with a generated self-signed certificate (SAN 127.0.0.1): `200 {"name":"tls-stub-node","ok":true} `.

Evidence error in `GET /api/autonomous/lane-fill`:
```
pool host tls-stub-node unreadable (loaded models): ollama /api/ps: node tls-stub-node certificate not trusted: tls: failed to verify certificate: x509: certificate signed by unknown authority: set tls_ca_file or tls_cert_fingerprint on the node, or tls_insecure_skip_verify to turn verification off for it
```

- **PASS** — the evidence error names the node and the fix (tls_ca_file or tls_cert_fingerprint, or tls_insecure_skip_verify)
  - node named: yes

- **PASS** — the evidence error does not show the URL (no address, port or scheme)
  - checked for 127.0.0.1, :59181 and a scheme

- **PASS** — the CLI shows the same error line
  - `Pool evidence error: pool host tls-stub-node unreadable (loaded models): ollama /api/ps: node tls-stub-node certificate not trusted: tls: failed to verify certificate: x509: certificate signed by unkn`

Evidence after the node's tls_cert_fingerprint was set (`PUT` 200 {"name":"tls-stub-node","ok":true} ):
```
{"checked_at":"2026-11-01T15:36:04.518426303-05:00","gpu_active_sessions":0,"loaded_models":[{"host":"tls-stub-node","model":"tls-stub-model:1b"}],"error":""}
```

- **PASS** — after the node's tls_cert_fingerprint is set the evidence reads (no error, the node's resident model is listed)
  - fingerprint of the generated certificate (SHA-256, hex) set on the node; evidence: {"error":"","loaded_models":[{"host":"tls-stub-node","model":"tls-stub-model:1b"}]}

## 7. Shadow comparison

The dogfood step: what lane fill refused and why, next to what the daemon-side supervisor watch and the supervisor watch script report for the same sandbox state. The data was taken in check 4's window (before anything was started, the Claude entry capped at 1); the refusal-to-eligible-work link was then tried by making the control stale.

**What could be run.** `scripts/supervisor-watch.py` is written for the operator's own daemon (a fixed address, the service name and the checkout), so it cannot be pointed at a sandbox and was **not** run as a program. The check loads it as a module and applies its own `IGNORE` set, its own `recent()` (72 hours), its idle-lane rule as written in `main()` and its own `map_finding()` and record format to the sandbox's Automaton list and capacity pools; the findings it would have recorded went to the daemon's comparison file.

| Automaton | What it is | Lane fill | Supervisor watch script (its rule) |
|---|---|---|---|
| `1f1100a1` | (a) stale draft | refused: `stale` | not counted (older than the script's 72 hour window) |
| `1f1100b1` | (b) HOLD line | refused: `held` | counted as queued and runnable |
| `1f1100c1` | (c) sequenced after an unfinished Automaton | refused: `sequenced_after_unshipped` | counted as queued and runnable |
| `1f1100d1` | (d) proposal without the line | refused: `proposal_not_confirmed` | counted as queued and runnable |
| `1f1100e1` | (e) no proposal named | refused: `proposal_missing` | counted as queued and runnable |
| `1f1100f1` | (f) task commit on the base branch | refused: `main_carries_work` | counted as queued and runnable |
| `1f1100a7` | (g) worktree not registered | refused: `worktree_invalid` | counted as queued and runnable |
| `1f1100c0` | (h) control, fully eligible | eligible | counted as queued and runnable |
| `1f1100d2` | (i) Claude-lane candidate, proposal confirmed in check 5 | refused: `proposal_not_confirmed` | counted as queued and runnable |
| `1f1100a8` | (j) GPU-lane candidate, proposal confirmed in check 5 | refused: `proposal_not_confirmed` | counted as queued and runnable |

**Counts.** Lane fill: eligible work for GPU lanes 1, for Claude lanes 0. The script's own rule: 9 Automata counted as queued and runnable. Script output for the state (its findings): `["IDLE Claude lane while 9 Automata are needs_review, approved or draft"]`; Claude pool it read: `{"name":"llm:claude-code","limit":1,"external":0,"held":0,"holders":null,"effective_max_inflight":1,"inflight_source":"explicit"}`.

**Daemon-side supervisor watch**, findings of kind idle_lane_eligible_work before the control was made stale: `[{"subject":"gpu-2","resolved":false},{"subject":"gpu-1","resolved":false}]`; after: `[{"subject":"gpu-2","resolved":true},{"subject":"gpu-1","resolved":true}]`.

The watch's own comparison against the script record file (`GET /api/autonomous/supervisor-watch`, comparison):
```
{
  "last": {
    "at": "2026-11-01T15:34:09.353329471-05:00",
    "watch_kinds": [
      "idle_lane_eligible_work/gpu-1",
      "idle_lane_eligible_work/gpu-2"
    ],
    "script_kinds": [
      "idle_lane_eligible_work/claude"
    ],
    "only_watch": [
      "idle_lane_eligible_work/gpu-1",
      "idle_lane_eligible_work/gpu-2"
    ],
    "only_script": [
      "idle_lane_eligible_work/claude"
    ],
    "agreed": false
  },
  "agreed_days": 0,
  "only_watch": [
    "idle_lane_eligible_work/gpu-1",
    "idle_lane_eligible_work/gpu-2"
  ],
  "only_script": [
    "idle_lane_eligible_work/claude"
  ]
}
```

**Differences, written down:**

- The script's rule counts 8 Automata that lane fill refuses: `1f1100b1` ((b) HOLD line): lane fill refuses it as `held`; the script counts it as runnable. `1f1100c1` ((c) sequenced after an unfinished Automaton): lane fill refuses it as `sequenced_after_unshipped`; the script counts it as runnable. `1f1100d1` ((d) proposal without the line): lane fill refuses it as `proposal_not_confirmed`; the script counts it as runnable. `1f1100e1` ((e) no proposal named): lane fill refuses it as `proposal_missing`; the script counts it as runnable. `1f1100f1` ((f) task commit on the base branch): lane fill refuses it as `main_carries_work`; the script counts it as runnable. `1f1100a7` ((g) worktree not registered): lane fill refuses it as `worktree_invalid`; the script counts it as runnable. `1f1100d2` ((i) Claude-lane candidate, proposal confirmed in check 5): lane fill refuses it as `proposal_not_confirmed`; the script counts it as runnable. `1f1100a8` ((j) GPU-lane candidate, proposal confirmed in check 5): lane fill refuses it as `proposal_not_confirmed`; the script counts it as runnable. 
- The script reports one line per backend (`IDLE Claude lane`, `IDLE pi lane`) read from the capacity pools of the operator's daemon; the daemon-side watch reports one finding per idle lane id (for example `gpu-1`) read from lane fill's own queued-eligible count. The two keys differ by construction, so the daemon's comparison lists the lane ids as raised by the watch only and the backend names as raised by the script only, even when both mean the same idle lane.
- The script has no stale, proposal, base-branch or worktree rule. It filters only by status, a 72 hour window and its fixed ignore list.

- **PASS** — the script's own rule was applied to the sandbox state and its findings and counts are in the report
  - queued_runnable 9, exit 0; 

- **PASS** — the daemon-side watch raises idle_lane_eligible_work for exactly the idle lanes whose kind lane fill counts eligible work for (the GPU lanes, never a Claude lane)
  - watch findings ["gpu-1","gpu-2"]; idle GPU lanes with eligible work in lane fill: ["gpu-1","gpu-2"]; lane fill counts gpu 1, claude 0

- **PASS** — once lane fill refuses the control (stale), it stops counting as eligible work in the daemon-side watch too: queued eligible 0 and the idle lane findings clear
  - lane fill refusal: {"eligibility_reason":"stale","reason":"Automaton last updated 11 hours ago, older than the 8 hour stale limit"}; queued eligible after: {"gpu":0,"claude":0}; watch findings after: []

- The daemon recorded a comparison against the script record, shown above.

- **PASS** — every difference between lane fill and the script is listed in the report (none hidden)
  - 8 Automata counted by the script that lane fill refuses

## 8. Cleanup

- **PASS** — the sandbox daemon and both stubs are gone and the daemon's port is closed
  - daemon gone: yes; stubs gone: yes; port 54531 closed: yes

- **PASS** — no test daemon is left behind (scripts/check-no-leftover-test-daemons.sh)
  - exit 0

- The daemon and the stubs were ended by `td_stop_all` from `scripts/test-daemon-lib.sh` (the recorded pids, config or stub path as marker); nothing was ended by name.

- **PASS** — the throwaway directory (project, remote, worktrees, data, config, log, stubs, certificate, records) is removed
  - directory exists after removal: no

## Daemon log, the lane fill lines

```
{"ts":"2026-11-01T15:36:00.013674517-05:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/lane-fill","remote_ip":"127.0.0.1","status":200,"user_agent":"curl/
{"ts":"2026-11-01T15:36:00.022711489-05:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/lane-fill","remote_ip":"127.0.0.1","status":200,"user_agent":"curl/
{"ts":"2026-11-01T15:36:00.041416239-05:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/lane-fill","remote_ip":"127.0.0.1","status":200,"user_agent":"Go-ht
{"ts":"2026-11-01T15:36:00.058095033-05:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/lane-fill","remote_ip":"127.0.0.1","status":200,"user_agent":"curl/
{"ts":"2026-11-01T15:36:02.068668119-05:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/lane-fill","remote_ip":"127.0.0.1","status":200,"user_agent":"curl/
{"ts":"2026-11-01T15:36:04.079495136-05:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/lane-fill","remote_ip":"127.0.0.1","status":200,"user_agent":"curl/
{"ts":"2026-11-01T15:36:06.090391827-05:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/lane-fill","remote_ip":"127.0.0.1","status":200,"user_agent":"curl/
{"ts":"2026-11-01T15:36:06.099024614-05:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/lane-fill","remote_ip":"127.0.0.1","status":200,"user_agent":"curl/
{"ts":"2026-11-01T15:36:06.107324211-05:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/lane-fill","remote_ip":"127.0.0.1","status":200,"user_agent":"curl/
{"ts":"2026-11-01T15:36:06.117296615-05:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/lane-fill","remote_ip":"127.0.0.1","status":200,"user_agent":"curl/
```

Branches in the throwaway project at the end: `automaton/1f1100c0-control trunk `

## Order and limits

- **Run order.** The checks ran in the order 1, 2, 3, 4, 7, 5, 6, 8: check 7 needs the state before anything is started, so its data was taken in check 4's window and the report shows it under its own number.
- **The Automata were written into the sandbox store, not created through the create route.** The route only makes drafts and a plan needs a planner model, which a sandbox does not have. Every read, setting change and start goes through the REST API or the command line of the running daemon.
- **The chat verb shows the newest five decisions** by design, so check 3 compares those five with the status; the CLI shows every decision it reads.
- **The PWA card** is checked at its code path (the served script and the five locale bundles); the script does not drive a browser.
- **Check 5 confirms a proposal by editing a file in the throwaway repository**, which is how an operator confirms one; the rule reads the file on every poll.

Result: PASS
