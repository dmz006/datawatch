# Model routing — sandbox live check

Date: 2026-11-04 · Branch: `automaton/b2220001--tasks-write-the-final-gate-report-703b0015` (tree at commit `d2221e2e5`)

**Sandbox daemon: `datawatch v9.51.0`** (`GET /api/health` read `"version":"9.51.0"`), built from this worktree with
`go build -o .datawatch/live-check/bin/datawatch ./cmd/datawatch/`. Host: `johnnyjohnny`, Linux 7.0.0-34-generic. It ran from its own config and
data directory under `.datawatch/live-check/` (git-ignored, deleted at the end). The production daemon, its
service unit and its ports were never touched.

**Sandbox ports:** HTTP 59795, TLS 54271, MCP 49365, DNS 47099, webhook 40063, notify 54699, planner stand-in 50317, model stand-in A 56571, model stand-in B 59193.

Script: `scripts/model-routing-live-check.sh`, run as `env -u TMUX -u TMUX_TMPDIR bash scripts/model-routing-live-check.sh`. It
regenerates this report on every run. Nothing was ended by name, only by the PID the lifecycle helper recorded.

## Results

One line per step. A step passes only when every check under it in the evidence is met.

- (a) Config round trip — Result: PASS — the six keys read back refuse, 300, true, assume_serves, 8, true; each was set and read back; four invalid requests were refused with 400 naming the key and the valid key beside each was not applied
- (b) Served models read on REST, MCP, CLI and comm — Result: PASS — both nodes read the same models on REST, MCP and CLI --json, and the CLI text and comm reply carried both node names with every model
- (c) Pin-time refusal — Result: PASS — a model neither node serves was refused with 400 naming the model and both nodes, nothing was stored and an error alert was raised; a model node A serves was accepted and stored
- (d) Warn mode — Result: PASS — with refusal_mode warn the unserved model was applied, a model_not_served_warning decision naming the model and both nodes was recorded and a warning alert existed on the alerts API; with alert_on_refusal false the pin was applied and recorded and no alert was raised
- (e) Node pin — Result: PASS — a pin to the node not serving the model was refused naming node and model, a pin to the serving node was accepted and showed as pinned_node, clearing removed it, and an unregistered node was refused
- (f) Cache age — Result: PASS — inside the age a plain read kept the old list, refresh=true showed the new one, and with the age at 0 the next plain read was fresh
- (g) The verifier follows the worker's node — Result: NOT EXERCISED — not exercised live: it needs a running worker, which the sandbox cannot drive; covered by the Go test `TestModelRoute_VerifierFollowsPinnedNode`
- (cleanup) Cleanup — Result: PASS — the daemon and all three stand-ins were gone, no Automaton, node or LLM entry was left, and the leftover guard found no test daemon

**Steps:** 7 passed, 0 failed, 1 not exercised live (g).

**Cleanup result:** daemon and stand-ins ended by recorded pid, TLS port 54271 closed, created Automaton, LLM entry and nodes deleted through the API, no leftover test daemon from this check, scratch area removed.

## Evidence

Scope: the `autonomous.model_routing` config keys on `GET`/`PUT /api/config`, `GET /api/compute/served-models` and its MCP, CLI and comm channel reads, the pin-time refusal and warning on `set_task_llm`, the node pin on `set_task_node`, the served-models cache age, and the alert and decision records, all against two stand-in model servers registered as compute nodes.

### Setup

- **Sandbox only.** Config generated from `testdata/datawatch.yaml` by the substitutions the test runner uses (data folder, loopback ports, test token), plus `server.allowed_project_roots` (one folder). No `autonomous.model_routing` key is set in the file, so step (a) reads the built-in defaults.
- **Ports** (probed free just before use): HTTP 59795, TLS 54271, MCP 49365, DNS 47099, webhook 40063, notify 54699, planner stand-in 50317, model stand-in A 56571, model stand-in B 59193. Port 8080 and the production TLS port were not used.
- **Model stand-ins.** Two inline `python3 -I` servers on loopback, each recorded by PID like the daemon. `GET /api/tags` returns the names held in a list file the check rewrites at runtime. Node A starts serving `live-a:1b` and `live-shared:1b`; node B starts serving `live-b:1b` and `live-shared:1b`.
- **Registered through the API.** Two compute nodes of kind `ollama` (`live-node-a` at stand-in A, `live-node-b` at stand-in B) and one throwaway LLM entry `aa-live-check-engine` of kind `ollama` listing both as `compute_nodes`, so a local-engine model is checked against exactly these two nodes. All three are deleted through the API at the end.
- **Planner stand-in.** A small HTTP server (`planner.py`) answers the daemon's planning call with a fixed one-task plan so an Automaton can be planned without a model; the check replaces that plan with its own story and task. The sandbox daemon also links its own model node to that address; the LLM entry's node list keeps it out of every check.
- **Throwaway project.** A git repository (`.datawatch/live-check/run/projects/throwaway`, default branch `trunk`) with one commit. No worker is started.

Sandbox daemon pid `700933` (recorded by the lifecycle helper) was healthy at 01:09:12; planner stand-in pid `700898`, model stand-ins pid `700908` and `700920`.

Registration answers: node A `200`, node B `200`, LLM entry `200`.

### Steps

#### (a) Config round trip

`GET /api/config` → `autonomous.model_routing` before anything was set:
```
{"alert_on_refusal":true,"probe_timeout_seconds":8,"refusal_mode":"refuse","served_models_cache_seconds":300,"unknown_node_policy":"assume_serves","verifier_follows_worker_node":true}

```
- ok — the defaults read back: refusal_mode refuse, served_models_cache_seconds 300, verifier_follows_worker_node true, unknown_node_policy assume_serves, probe_timeout_seconds 8, alert_on_refusal true
  - read: `{"alert_on_refusal":true,"probe_timeout_seconds":8,"refusal_mode":"refuse","served_models_cache_seconds":300,"unknown_node_policy":"assume_serves","verifier_follows_worker_node":true}`

Each key set alone with `PUT /api/config`, then read back (HTTP status of the PUT, value read):
```
refusal_mode: PUT 200, read back warn
served_models_cache_seconds: PUT 200, read back 17
verifier_follows_worker_node: PUT 200, read back false
unknown_node_policy: PUT 200, read back assume_not
probe_timeout_seconds: PUT 200, read back 3
alert_on_refusal: PUT 200, read back false


```
- ok — each of the six keys was accepted (200) and reads back the value set
  - after all six: `{"alert_on_refusal":false,"probe_timeout_seconds":3,"refusal_mode":"warn","served_models_cache_seconds":17,"unknown_node_policy":"assume_not","verifier_follows_worker_node":false}`
- ok — all six set back to the defaults in one request, and the block reads as at the start
  - PUT 200

Invalid values, each sent to `PUT /api/config` beside a valid key (HTTP status, then the refusal text):
```
refusal_mode sometimes + cache 99: 400 autonomous.model_routing.refusal_mode: sometimes is invalid; use "refuse" or "warn" (default "refuse") 
cache -1 + alert_on_refusal false: 400 autonomous.model_routing.served_models_cache_seconds: -1 is invalid; use a whole number from 0 to 604800 (0 = read from the node every time, default 300) 
probe_timeout 0 + refusal_mode warn: 400 autonomous.model_routing.probe_timeout_seconds: 0 is invalid; use a whole number from 1 to 604800 (default 8) 
unknown_node_policy maybe + probe_timeout 20: 400 autonomous.model_routing.unknown_node_policy: maybe is invalid; use "assume_serves" or "assume_not" (default "assume_serves") 

```
- ok — a bad mode, a negative cache age, a zero probe timeout and a bad policy are each refused with 400 and the refusal names the key
- ok — nothing was applied by a refused request, including the valid key sent beside the bad one (cache 99, alert_on_refusal false, refusal_mode warn, probe 20 not saved)
  - config after the four refusals: `{"alert_on_refusal":true,"probe_timeout_seconds":8,"refusal_mode":"refuse","served_models_cache_seconds":300,"unknown_node_policy":"assume_serves","verifier_follows_worker_node":true}`

#### (b) Served models: REST, MCP, CLI and comm channel agree

REST: `GET /api/compute/served-models`; MCP: `compute_served_models {}` over `datawatch mcp` (stdio); CLI: `datawatch compute node served-models` (text) and `--json`; comm: `POST /api/test/message {"text":"compute node models"}`.

REST, the two sandbox nodes:
```
{
  "cache_seconds": 300,
  "refusal_mode": "refuse",
  "nodes": [
    {
      "node": "live-node-a",
      "kind": "ollama",
      "models": [
        "live-a:1b",
        "live-shared:1b"
      ],
      "known": true,
      "cached": false,
      "age_seconds": 0
    },
    {
      "node": "live-node-b",
      "kind": "ollama",
      "models": [
        "live-b:1b",
        "live-shared:1b"
      ],
      "known": true,
      "cached": false,
      "age_seconds": 0
    }
  ]
}

```

CLI text:
```
datawatch-ollama (ollama): 0 models, cached, 0s old: none
datawatch-ollama-b (ollama): 0 models, cached, 0s old: none
live-node-a (ollama): 2 models, cached, 0s old: live-a:1b, live-shared:1b
live-node-b (ollama): 2 models, cached, 0s old: live-b:1b, live-shared:1b

```

Comm channel reply:
```
[dw-e2e-test] compute node models
datawatch-ollama (ollama)
  0 models, kept copy, 0s old
datawatch-ollama-b (ollama)
  0 models, kept copy, 0s old
live-node-a (ollama)
  2 models, kept copy, 0s old
  live-a:1b, live-shared:1b
live-node-b (ollama)
  2 models, kept copy, 0s old
  live-b:1b, live-shared:1b

```

MCP result (first 500 bytes):
```
{"cache_seconds":300,"nodes":[{"node":"datawatch-ollama","kind":"ollama","models":[],"known":true,"cached":true,"age_seconds":0,"fetched_at":"2026-11-04T06:09:13Z","error":""},{"node":"datawatch-ollama-b","kind":"ollama","models":[],"known":true,"cached":true,"age_seconds":0,"fetched_at":"2026-11-04T06:09:13Z","error":""},{"node":"live-node-a","kind":"ollama","models":["live-a:1b","live-shared:1b"],"known":true,"cached":true,"age_seconds":0,"fetched_at":"2026-11-04T06:09:13Z","error":""},{"node"

```
- ok — REST reads node A as live-a:1b and live-shared:1b, and node B as live-b:1b and live-shared:1b, both known
  - A: live-a:1b,live-shared:1b; B: live-b:1b,live-shared:1b
- ok — MCP and CLI `--json` return the same kind, known flag and models for both nodes as REST, and the CLI text and the comm reply name both nodes with every model
  - comparison: `OK`; CLI stderr: 

Throwaway Automaton `46a10001`, one story, one task `4d700001`, created and planned through the API and left in `needs_review`, the status in which a model or node pin is accepted. It is never approved or run.

#### (c) Pin-time refusal

`POST /api/autonomous/prds/46a10001/set_task_llm` with model `ollama/live-none:1b` (served by neither node), HTTP status then body:
```
400 no registered node serves model "ollama/live-none:1b"; nodes checked: live-node-a (serves 2 models, none is ollama/live-none:1b), live-node-b (serves 2 models, none is ollama/live-none:1b) 

```

The same call with model `ollama/live-a:1b` (served by node A only):
```
200 {"id":"46a10001","spec":"Model routing live check: model-routing","title":"Model routing live check","project_dir":".datawatch/live-check/run/projects/throwaway","git":{},"backend":"ollama","status":"needs_review","created_at":"2026-11-04T01:09:13.825810714-05:00","updated_at":"2026-11-04T01:09:14.031898926-05:00","rev":6,"stories":[{"id":"4a190001","prd_id":"46a10001","title":"Live check story","description":"The task of the live check.","status":"pending","tasks":[{"id":"4d700001","story_id":"4a190001","prd_id":"46a10001","title":"Live check task","spec":"Placeholder task for the model routing live check.","status":"","created_at":"2026-11-04T01:09:13.939987167-05:00","updated_at":"2026-11-04T01:09:14.031896332-05:00","backend":"ollama","model":"ollama/live-a:1b"}],"created_at":"2026-11-04T01:09:13.883952422-05:00","updated_at":"2026-11-04T01:09:13.939987968-05:00"}],"decisions":[{"at":"2026-11-04T01:09:13.864732337-05:00","kind":"decompose","backend":"ollama","prompt_chars":4809,"response_chars":389,"actor":"autonomous"},{"at":"2026-11-04T01:09:13.889590994-05:00","kind":"add_story","actor":"operator","note":"story=4a190001 title=\"Live check story\""},{"at":"2026-11-04T01:09:13.9187602-05:00","kind":"remove_story","actor":"operator","note":"story=48a30001"},{"at":"2026-11-04T01:09:13.947294404-05:00","kind":"add_task","actor":"operator","note":"story=4a190001 task=4d700001 title=\"Live check task\""},{"at":"2026-11-04T01:09:14.031896422-05:00","kind":"set_task_llm","actor":"operator","note":"task=4d700001 backend=ollama effort= model=ollama/live-a:1b"}],"memory_seed":{},"memory_harvest":{},"plan_coverage":{"mode":"block","min_ratio":1,"report":{"Covered":0,"Missing":1,"Weakened":0,"Unmatched":[{"requirement":"model-routing","verdict":"missing"}]},"findings":[{"Kind":"missing_verification","Detail":"task 48a30002 has neither verification_script nor verification_script_reason; per B124 a task without a script must state an explicit exemption reason","StoryID":"48a30001","TaskID":"48a30002"}]}} 

```
- ok — the unserved model is refused with 400
  - answer: 400
- ok — the refusal names the model and both nodes
  - text: `no registered node serves model "ollama/live-none:1b"; nodes checked: live-node-a (serves 2 models, none is ollama/live-none:1b), live-node-b (serves 2 models, none is ollama/live-none:1b) `
- ok — nothing was stored by the refusal: the task's model is still empty
  - task model after the refusal: ``
- ok — the refusal raised one error-level alert titled `Automaton model not served by any node` (alert_on_refusal is on by default)
  - alerts with that title: before 0, after 1; the refusal alert is at level `error`
- ok — a model one node serves is accepted (200) and stored on the task
  - answer 200; task model now `ollama/live-a:1b`

#### (d) Warn mode

With `refusal_mode` warn, `set_task_llm` with model `ollama/live-none:1b`, HTTP status then body:
```
200 {"id":"46a10001","spec":"Model routing live check: model-routing","title":"Model routing live check","project_dir":".datawatch/live-check/run/projects/throwaway","git":{},"backend":"ollama","status":"needs_review","created_at":"2026-11-04T01:09:13.825810714-05:00","updated_at":"2026-11-04T01:09:14.149184004-05:00","rev":7,"stories":[{"id":"4a190001","prd_id":"46a10001","title":"Live check story","description":"The task of the live check.","status":"pending","tasks":[{"id":"4d700001","story_id":"4a190001","prd_id":"46a10001","title":"Live check task","spec":"Placeholder task for the model routing live check.","status":"","created_at":"2026-11-04T01:09:13.939987167-05:00","updated_at":"2026-11-04T01:09:14.149182211-05:00","backend":"ollama","model":"ollama/live-none:1b"}],"created_at":"2026-11-04T01:09:13.883952422-05:00","updated_at":"2026-11-04T01:09:13.939987968-05:00"}],"decisions":[{"at":"2026-11-04T01:09:13.864732337-05:00","kind":"decompose","backend":"ollama","prompt_chars":4809,"response_chars":389,"actor":"autonomous"},{"at":"2026-11-04T01:09:13.889590994-05:00","kind":"add_story","actor":"operator","note":"story=4a190001 title=\"Live check story\""},{"at":"2026-11-04T01:09:13.9187602-05:00","kind":"remove_story","actor":"operator","note":"story=48a30001"},{"at":"2026-11-04T01:09:13.947294404-05:00","kind":"add_task","actor":"operator","note":"story=4a190001 task=4d700001 title=\"Live check task\""},{"at":"2026-11-04T01:09:14.031896422-05:00","kind":"set_task_llm","actor":"operator","note":"task=4d700001 backend=ollama effort= model=ollama/live-a:1b"},{"at":"2026-11-04T01:09:14.149182311-05:00","kind":"set_task_llm","actor":"operator","note":"task=4d700001 backend=ollama effort= model=ollama/live-none:1b"},{"at":"2026-11-04T01:09:14.149183333-05:00","kind":"model_not_served_warning","actor":"operator","note":"no registered node serves model \"ollama/live-none:1b\"; nodes checked: live-node-a (serves 2 models, none is ollama/live-none:1b), live-node-b (serves 2 models, none is ollama/live-none:1b)"}],"memory_seed":{},"memory_harvest":{},"plan_coverage":{"mode":"block","min_ratio":1,"report":{"Covered":0,"Missing":1,"Weakened":0,"Unmatched":[{"requirement":"model-routing","verdict":"missing"}]},"findings":[{"Kind":"missing_verification","Detail":"task 48a30002 has neither verification_script nor verification_script_reason; per B124 a task without a script must state an explicit exemption reason","StoryID":"48a30001","TaskID":"48a30002"}]}} 

```

The newest `model_not_served_warning` decision note, and the alert the warning raised:
```
no registered node serves model "ollama/live-none:1b"; nodes checked: live-node-a (serves 2 models, none is ollama/live-none:1b), live-node-b (serves 2 models, none is ollama/live-none:1b)
{"title":"Automaton model not served by any node","level":"warn","body":"Automaton: 46a10001\nPinned: task 4d700001\nno registered node serves model \"ollama/live-none:1b\"; nodes checked: live-node-a (serves 2 models, none is ollama/live-none:1b), live-node-b (serves 2 models, none is ollama/live-none:1b)\nThe pin was applied anyway because model routing is set to warn."}

```
- ok — the unserved model is applied: the answer is 200 and the task's model is now `ollama/live-none:1b`
  - answer 200; task model `ollama/live-none:1b`
- ok — a `model_not_served_warning` decision was recorded on the Automaton and names the model and both nodes
  - decisions of that kind: before 0, after 1
- ok — an alert exists on the alerts API, at warning level (the alerts API spells it `warn`)
  - alerts with that title: before 1, after 2; the warn-mode alert is at level `warn`

With `alert_on_refusal` false, `set_task_llm` with model `ollama/live-none-two:1b`, HTTP status then body:
```
200 {"id":"46a10001","spec":"Model routing live check: model-routing","title":"Model routing live check","project_dir":".datawatch/live-check/run/projects/throwaway","git":{},"backend":"ollama","status":"needs_review","created_at":"2026-11-04T01:09:13.825810714-05:00","updated_at":"2026-11-04T01:09:14.284821545-05:00","rev":8,"stories":[{"id":"4a190001","prd_id":"46a10001","title":"Live check story","description":"The task of the live check.","status":"pending","tasks":[{"id":"4d700001","story_id":"4a190001","prd_id":"46a10001","title":"Live check task","spec":"Placeholder task for the model routing live check.","status":"","created_at":"2026-11-04T01:09:13.939987167-05:00","updated_at":"2026-11-04T01:09:14.284819111-05:00","backend":"ollama","model":"ollama/live-none-two:1b"}],"created_at":"2026-11-04T01:09:13.883952422-05:00","updated_at":"2026-11-04T01:09:13.939987968-05:00"}],"decisions":[{"at":"2026-11-04T01:09:13.864732337-05:00","kind":"decompose","backend":"ollama","prompt_chars":4809,"response_chars":389,"actor":"autonomous"},{"at":"2026-11-04T01:09:13.889590994-05:00","kind":"add_story","actor":"operator","note":"story=4a190001 title=\"Live check story\""},{"at":"2026-11-04T01:09:13.9187602-05:00","kind":"remove_story","actor":"operator","note":"story=48a30001"},{"at":"2026-11-04T01:09:13.947294404-05:00","kind":"add_task","actor":"operator","note":"story=4a190001 task=4d700001 title=\"Live check task\""},{"at":"2026-11-04T01:09:14.031896422-05:00","kind":"set_task_llm","actor":"operator","note":"task=4d700001 backend=ollama effort= model=ollama/live-a:1b"},{"at":"2026-11-04T01:09:14.149182311-05:00","kind":"set_task_llm","actor":"operator","note":"task=4d700001 backend=ollama effort= model=ollama/live-none:1b"},{"at":"2026-11-04T01:09:14.149183333-05:00","kind":"model_not_served_warning","actor":"operator","note":"no registered node serves model \"ollama/live-none:1b\"; nodes checked: live-node-a (serves 2 models, none is ollama/live-none:1b), live-node-b (serves 2 models, none is ollama/live-none:1b)"},{"at":"2026-11-04T01:09:14.284819211-05:00","kind":"set_task_llm","actor":"operator","note":"task=4d700001 backend=ollama effort= model=ollama/live-none-two:1b"},{"at":"2026-11-04T01:09:14.284820303-05:00","kind":"model_not_served_warning","actor":"operator","note":"no registered node serves model \"ollama/live-none-two:1b\"; nodes checked: live-node-a (serves 2 models, none is ollama/live-none-two:1b), live-node-b (serves 2 models, none is ollama/live-none-two:1b)"}],"memory_seed":{},"memory_harvest":{},"plan_coverage":{"mode":"block","min_ratio":1,"report":{"Covered":0,"Missing":1,"Weakened":0,"Unmatched":[{"requirement":"model-routing","verdict":"missing"}]},"findings":[{"Kind":"missing_verification","Detail":"task 48a30002 has neither verification_script nor verification_script_reason; per B124 a task without a script must state an explicit exemption reason","StoryID":"48a30001","TaskID":"48a30002"}]}} 

```
- ok — with alert_on_refusal false the pin is still applied and recorded (200, a second warning decision)
  - decisions: 1 -> 2
- ok — and no alert is raised: the count of alerts with that title did not change
  - alerts with that title: 2 -> 2
- ok — refuse mode and alert_on_refusal are back at their defaults and the task's model is back on `ollama/live-a:1b`

#### (e) Node pin

Pin the task (model `ollama/live-a:1b`, served by node A only) to node B, HTTP status then body:
```
400 node "live-node-b" does not serve model "ollama/live-a:1b" (serves 2 models, none is ollama/live-a:1b) 

```

Pin it to node A, then the task as the Automaton JSON shows it:
```
200 {"id":"46a10001","spec":"Model routing live check: model-routing","title":"Model routing live check","project_dir":".datawatch/live-check/run/projects/throwaway","git":{},"backend":"ollama","status":"needs_review","created_at":"2026-11-04T01:09:13.825810714-05:00","updated_at":"2026-11-04T01:09:14.509245695-05:00","rev":10,"stories":[{"id":"4a190001","prd_id":"46a10001","title":"Live check story","description":"The task of the live check.","status":"pending","tasks":[{"id":"4d700001","story_id":"4a190001","prd_id":"46a10001","title":"Live check task","spec":"Placeholder task for the model routing live check.","status":"","pinned_node":"live-node-a","created_at":"2026-11-04T01:09:13.939987167-05:00","updated_at":"2026-11-04T01:09:14.509244342-05:00","backend":"ollama","model":"ollama/live-a:1b"}],"created_at":"2026-11-04T01:09:13.883952422-05:00","updated_at":"2026-11-04T01:09:13.939987968-05:00"}],"decisions":[{"at":"2026-11-04T01:09:13.864732337-05:00","kind":"decompose","backend":"ollama","prompt_chars":4809,"response_chars":389,"actor":"autonomous"},{"at":"2026-11-04T01:09:13.889590994-05:00","kind":"add_story","actor":"operator","note":"story=4a190001 title=\"Live check story\""},{"at":"2026-11-04T01:09:13.9187602-05:00","kind":"remove_story","actor":"operator","note":"story=48a30001"},{"at":"2026-11-04T01:09:13.947294404-05:00","kind":"add_task","actor":"operator","note":"story=4a190001 task=4d700001 title=\"Live check task\""},{"at":"2026-11-04T01:09:14.031896422-05:00","kind":"set_task_llm","actor":"operator","note":"task=4d700001 backend=ollama effort= model=ollama/live-a:1b"},{"at":"2026-11-04T01:09:14.149182311-05:00","kind":"set_task_llm","actor":"operator","note":"task=4d700001 backend=ollama effort= model=ollama/live-none:1b"},{"at":"2026-11-04T01:09:14.149183333-05:00","kind":"model_not_served_warning","actor":"operator","note":"no registered node serves model \"ollama/live-none:1b\"; nodes checked: live-node-a (serves 2 models, none is ollama/live-none:1b), live-node-b (serves 2 models, none is ollama/live-none:1b)"},{"at":"2026-11-04T01:09:14.284819211-05:00","kind":"set_task_llm","actor":"operator","note":"task=4d700001 backend=ollama effort= model=ollama/live-none-two:1b"},{"at":"2026-11-04T01:09:14.284820303-05:00","kind":"model_not_served_warning","actor":"operator","note":"no registered node serves model \"ollama/live-none-two:1b\"; nodes checked: live-node-a (serves 2 models, none is ollama/live-none-two:1b), live-node-b (serves 2 models, none is ollama/live-none-two:1b)"},{"at":"2026-11-04T01:09:14.380393627-05:00","kind":"set_task_llm","actor":"operator","note":"task=4d700001 backend=ollama effort= model=ollama/live-a:1b"},{"at":"2026-11-04T01:09:14.509244433-05:00","kind":"set_task_node","actor":"operator","note":"task=4d700001 node=live-node-a"}],"memory_seed":{},"memory_harvest":{},"plan_coverage":{"mode":"block","min_ratio":1,"report":{"Covered":0,"Missing":1,"Weakened":0,"Unmatched":[{"requirement":"model-routing","verdict":"missing"}]},"findings":[{"Kind":"missing_verification","Detail":"task 48a30002 has neither verification_script nor verification_script_reason; per B124 a task without a script must state an explicit exemption reason","StoryID":"48a30001","TaskID":"48a30002"}]}} 
{"id":"4d700001","model":"ollama/live-a:1b","pinned_node":"live-node-a"}

```

Clear the pin (empty node), then pin to a node name that is not registered:
```
clear: 200 {"id":"46a10001","spec":"Model routing live check: model-routing","title":"Model routing live check","project_dir":".datawatch/live-check/run/projects/throwaway","git":{},"backend":"ollama","status":"needs_review","created_at":"2026-11-04T01:09:13.825810714-05:00","updated_at":"2026-11-04T01:09:14.564514861-05:00","rev":11,"stories":[{"id":"4a190001","prd_id":"46a10001","title":"Live check story","description":"The task of the live check.","status":"pending","tasks":[{"id":"4d700001","story_id":"4a190001","prd_id":"46a10001","title":"Live check task","spec":"Placeholder task for the model routing live check.","status":"","created_at":"2026-11-04T01:09:13.939987167-05:00","updated_at":"2026-11-04T01:09:14.564510955-05:00","backend":"ollama","model":"ollama/live-a:1b"}],"created_at":"2026-11-04T01:09:13.883952422-05:00","updated_at":"2026-11-04T01:09:13.939987968-05:00"}],"decisions":[{"at":"2026-11-04T01:09:13.864732337-05:00","kind":"decompose","backend":"ollama","prompt_chars":4809,"response_chars":389,"actor":"autonomous"},{"at":"2026-11-04T01:09:13.889590994-05:00","kind":"add_story","actor":"operator","note":"story=4a190001 title=\"Live check story\""},{"at":"2026-11-04T01:09:13.9187602-05:00","kind":"remove_story","actor":"operator","note":"story=48a30001"},{"at":"2026-11-04T01:09:13.947294404-05:00","kind":"add_task","actor":"operator","note":"story=4a190001 task=4d700001 title=\"Live check task\""},{"at":"2026-11-04T01:09:14.031896422-05:00","kind":"set_task_llm","actor":"operator","note":"task=4d700001 backend=ollama effort= model=ollama/live-a:1b"},{"at":"2026-11-04T01:09:14.149182311-05:00","kind":"set_task_llm","actor":"operator","note":"task=4d700001 backend=ollama effort= model=ollama/live-none:1b"},{"at":"2026-11-04T01:09:14.149183333-05:00","kind":"model_not_served_warning","actor":"operator","note":"no registered node serves model \"ollama/live-none:1b\"; nodes checked: live-node-a (serves 2 models, none is ollama/live-none:1b), live-node-b (serves 2 models, none is ollama/live-none:1b)"},{"at":"2026-11-04T01:09:14.284819211-05:00","kind":"set_task_llm","actor":"operator","note":"task=4d700001 backend=ollama effort= model=ollama/live-none-two:1b"},{"at":"2026-11-04T01:09:14.284820303-05:00","kind":"model_not_served_warning","actor":"operator","note":"no registered node serves model \"ollama/live-none-two:1b\"; nodes checked: live-node-a (serves 2 models, none is ollama/live-none-two:1b), live-node-b (serves 2 models, none is ollama/live-none-two:1b)"},{"at":"2026-11-04T01:09:14.380393627-05:00","kind":"set_task_llm","actor":"operator","note":"task=4d700001 backend=ollama effort= model=ollama/live-a:1b"},{"at":"2026-11-04T01:09:14.509244433-05:00","kind":"set_task_node","actor":"operator","note":"task=4d700001 node=live-node-a"},{"at":"2026-11-04T01:09:14.564511065-05:00","kind":"set_task_node","actor":"operator","note":"task=4d700001 node="}],"memory_seed":{},"memory_harvest":{},"plan_coverage":{"mode":"block","min_ratio":1,"report":{"Covered":0,"Missing":1,"Weakened":0,"Unmatched":[{"requirement":"model-routing","verdict":"missing"}]},"findings":[{"Kind":"missing_verification","Detail":"task 48a30002 has neither verification_script nor verification_script_reason; per B124 a task without a script must state an explicit exemption reason","StoryID":"48a30001","TaskID":"48a30002"}]}} 
unregistered: 400 cannot pin task 4d700001 
```
- ok — pinning to the node that does not serve the model is refused (400) and names the node and the model
  - text: `node "live-node-b" does not serve model "ollama/live-a:1b" (serves 2 models, none is ollama/live-a:1b) `
- ok — the refused pin stored nothing (no pinned_node on the task)
- ok — pinning to the serving node is accepted (200) and the Automaton JSON shows pinned_node `live-node-a` on the task
  - task JSON: `{"id":"4d700001","model":"ollama/live-a:1b","pinned_node":"live-node-a"}`
- ok — clearing the pin (empty node) is accepted and pinned_node is gone from the task
  - clear answer 200; pinned_node key present: false
- ok — a node name that is not registered is refused (400) and the answer names it
  - text: `cannot pin task 4d700001 to node "live-node-unregistered": compute node is not registered `

#### (f) Cache age

The stand-in for node A was changed to also serve `live-late-one:1b` while the cache age was 300 seconds.

Plain read of node A inside the age, then with `refresh=true`, then plain again:
```
plain:   {"models":["live-a:1b","live-shared:1b"],"cached":true,"age_seconds":1}
refresh: {"models":["live-a:1b","live-shared:1b","live-late-one:1b"],"cached":false,"age_seconds":0}
after:   {"models":["live-a:1b","live-shared:1b","live-late-one:1b"],"cached":true,"age_seconds":0}

```
- ok — inside the age a plain read still shows the old list (no `live-late-one:1b`) and says it is cached
  - models: live-a:1b,live-shared:1b; cached: true
- ok — refresh=true shows the new list (with `live-late-one:1b`), not cached
  - models: live-a:1b,live-late-one:1b,live-shared:1b
- ok — the refresh replaced the kept copy: the next plain read shows the new list

With `served_models_cache_seconds` set to 0 (PUT 200) and the stand-in changed to `live-late-two:1b`, one plain read:
```
{"cache_seconds":0,"node":{"models":["live-a:1b","live-shared:1b","live-late-two:1b"],"cached":false,"age_seconds":0}}

```
- ok — with the age at 0 the next plain read is fresh: it shows `live-late-two:1b`, not cached, and the answer reports cache_seconds 0
  - models: live-a:1b,live-late-two:1b,live-shared:1b
- ok — the cache age is back at 300 and node A serves its original list again

#### (g) The verifier follows the worker's node

Not exercised live. Placing a verification job on the node its worker ran on needs a running worker, and the sandbox has no model to run one. This behavior is covered by the Go test `TestModelRoute_VerifierFollowsPinnedNode` (and by the `TestVerifierNode_` tests), which use fakes; this run did not drive it.

### Cleanup

- Created through the API and removed through the API: 1 Automaton (cancelled, hard-deleted), the LLM entry `aa-live-check-engine` and the two compute nodes. After the deletes `GET /api/autonomous/prds` listed 0 Automata, `GET /api/compute/nodes` listed 0 of this check's nodes and `GET /api/llms` listed 0 of this check's entries.
- **Sandbox daemon ended** by `td_stop_all` (recorded pid `700933`, marker = its config path); planner stand-in and both model stand-ins ended the same way (pids `700898`, `700908`, `700920`, marker = their script or list file). Daemon gone: yes. Planner gone: yes. Stand-in A gone: yes. Stand-in B gone: yes. TLS port 54271 closed: yes. Stand-in port 56571 closed: yes.
- **Leftover check** against the snapshot taken before this run: `scripts/check-no-leftover-test-daemons.sh check` exit 0 — OK: no leftover test daemons. New daemons whose config lies in this check's scratch area: 0. New daemons of other jobs on the host: 0.
- **Scratch area** (`.datawatch/live-check/`: project, config, data, log, list files, shims, daemon binary) is removed by the exit handler right after this report is written.
- ok — the sandbox daemon and both stand-ins are gone, every created Automaton, LLM entry and node was removed, and no test daemon was left behind
  - daemon gone: yes; planner gone: yes; stand-ins gone: yes/yes; port closed: yes; leftover check exit 0 (new daemons from this check: 0, from other jobs: 0); Automata left: 0; nodes left: 0; LLM entries left: 0

## What is not shown live

- **The verifier following the worker's node (g)** needs a running worker and is not drivable in the sandbox without one. It is covered by the Go test `TestModelRoute_VerifierFollowsPinnedNode` and was not exercised live.
- **The nodes are stand-ins.** They answer `GET /api/tags` only; no model is loaded or run. A real engine's model list (tags, digests) and a real node that is down were not exercised; the unreachable-node policy (`unknown_node_policy`) and `probe_timeout_seconds` were set and read back in step (a) but no node was made unreachable here.
- **Only the task model setter and the task node pin were driven.** The story and Automaton level setters, the verifier model setters and the story node pin share the same check and are covered by Go tests, not driven here.
- **Web app and mobile app** were not driven; their tracker rows are not changed by this check.
- **The comm read** went through `POST /api/test/message` on the sandbox daemon with `server.token` set, so it is the router's own verb that ran. A real chat channel was not connected.


Result: PASS
