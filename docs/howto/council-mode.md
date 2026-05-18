---
docs:
  index: true
  topics: [council, debate, decision, pai]
exec_params:
  - name: proposal
    description: The question or design to debate
    required: true
  - name: personas
    description: Comma-separated persona names (empty = all registered)
    required: false
    default: ""
  - name: mode
    description: "'quick' (1 round) or 'debate' (3 rounds); default quick"
    required: false
    default: "quick"
exec_steps:
  - tool: council_personas
    description: List currently registered personas
    args: {}
    read_only: true
  - tool: council_run
    description: Run the debate against the proposal
    args:
      proposal: "{{params.proposal}}"
      personas: "{{params.personas}}"
      mode: "{{params.mode}}"
    read_only: false
  - tool: council_list_runs
    description: Confirm the run is recorded
    args: {limit: "5"}
    read_only: true
---
# How-to: Council Mode — multi-persona structured debate

Surface a non-trivial decision to a council of personas, get a
synthesized consensus + dissent list back. Each persona has a distinct
perspective and is asked to push back on the proposal from that angle.

## What it is

The daemon ships with 12 default personas. Each is a YAML file at
`~/.datawatch/council/personas/<name>.yaml` with `name`, `role`, and
`system_prompt`:

| Persona | Lens |
|---|---|
| `security-skeptic` | Attack vectors, supply-chain, weakest auth |
| `ux-advocate` | Operator experience, mobile parity, accessibility |
| `perf-hawk` | Latency, throughput, memory, IO |
| `simplicity-advocate` | Push back on premature abstractions |
| `ops-realist` | Deployment, observability, on-call burden |
| `contrarian` | Steel-man the alternative; argue against momentum |
| `platform-engineer` | Host/runtime, capacity, blast radius |
| `network-engineer` | N-S + E-W traffic, mTLS, mesh policies |
| `data-architect` | Schema migrations, retention, GDPR, query plans |
| `privacy` | PII / consent / data-subject rights |
| `hacker` | Adversarial security tester; exploit-chain reasoning |
| `app-hacker` | Application security; injection / IDOR / CSRF / SSRF |

Two modes:

- **Quick (1 round)** — fast perspective check.
- **Debate (3 rounds)** — personas see each other's prior responses
  and push back across rounds. The 3rd round usually shows the
  position changes worth reading.

A synthesizer combines all responses into `consensus` + `dissent`
blocks. The `dissent` is usually the high-signal part.

## Base requirements

- `datawatch start` — daemon up.
- An LLM backend the council inference path can call. Configure under
  `council.llm_ref` in `~/.datawatch/datawatch.yaml`; defaults to
  `ollama-default`. Set `council.max_parallel` (default 2) for
  concurrent per-persona inference.
  - If no LLM is registered yet, see [llm-registry.md](llm-registry.md) to add one and [chat-and-llm-quickstart.md](chat-and-llm-quickstart.md) for the quickest path from zero to a working LLM backend.
- Disk space for persona YAMLs + run history (negligible).

> **Pre-conditions**: Council Mode requires a registered, enabled LLM referenced by `council.llm_ref`. See [llm-registry.md](llm-registry.md) for how to add and enable an LLM, and [chat-and-llm-quickstart.md](chat-and-llm-quickstart.md) for end-to-end setup.

> **API field name**: The run payload uses `"proposal"` (not `"question"`). REST: `{"proposal": "...", "personas": [...], "mode": "quick"}`. MCP: `council_run` arg `proposal="..."`. See the REST and MCP sections below.

## Setup

Default config works out of the box if you have an LLM configured.
To point Council at a specific LLM registry entry:

```yaml
council:
  llm_ref: claude-sonnet          # any name from Settings → Compute → LLMs
  max_parallel: 2                 # personas that run concurrently per round
  comm_firehose: false            # true = per-persona responses also pushed to comm channels
```

Run `datawatch reload` after editing.

The daemon seeds the 12 default persona YAMLs into
`~/.datawatch/council/personas/` on first start. Confirm:

```sh
ls ~/.datawatch/council/personas/
#  → security-skeptic.yaml ux-advocate.yaml perf-hawk.yaml ...
#    plus a hidden `.seeded` marker file (don't delete it).
```

Edit any persona's YAML directly to customize its system_prompt;
changes are picked up at next daemon start (or `datawatch reload`).

## Two happy paths

### 4a. Happy path — CLI

```sh
# 1. List currently registered personas.
datawatch council personas
#  → contrarian             Devil's advocate
#    data-architect         Data / DBA
#    hacker                 Adversarial security tester
#    ...

# 2. Quick check — 1 round, 3 personas. Run returns immediately with ID.
datawatch council run \
  --proposal "Should we switch the autonomous executor to fan-out workers per story?" \
  --personas security-skeptic,perf-hawk,simplicity-advocate \
  --mode quick
#  → run started: abc123 (watching SSE for completion...)
#    consensus: Fan-out workers are useful but add complexity...
#    dissent:   [security-skeptic] New IPC surface needs auth...

# 3. Cancel an in-flight run.
datawatch council cancel abc123

# 4. Debate — 3 rounds, full council, richer output.
datawatch council run --proposal "Same proposal" --mode debate
#  → run started: def456

# 5. Inspect past runs.
datawatch council runs --limit 20
datawatch council get-run def456 --format md > /tmp/council.md
```

Add a custom persona:

```sh
cat > ~/.datawatch/council/personas/cost-watcher.yaml <<'EOF'
name: cost-watcher
role: Cost / budget impact
system_prompt: |
  You are a cost-watcher. For the proposal, identify infrastructure
  cost impact (compute, storage, network egress, third-party API spend)
  and operator time-cost. Flag anything that increases either by >20%.
  Suggest cheaper variants where possible.
EOF
datawatch reload
datawatch council personas | grep cost-watcher
```

Remove a persona (durable across daemon restarts):

```sh
datawatch council personas-delete contrarian
# To restore later:
datawatch council personas-restore-default contrarian
```

### 4b. Happy path — PWA

1. PWA → Settings → Automate → **Council Mode** card.
2. Tick the personas you want at the table (default: all 12; tap
   chips to deselect).
3. Type the proposal in the textarea — the more specific, the better.
   Vague proposals get vague responses.
4. Pick mode (**Quick** / **Debate**) and click **Run Council**.
5. Result modal opens with each persona's response per round + the
   synthesizer's consensus + dissent.
6. To manage personas: click **View / edit personas** in the card.
   Modal lists every loaded persona with name, role, system_prompt,
   and on-disk path. Each row has × Remove. The **+ Add Persona**
   collapsible at the bottom takes a name + role + system_prompt.
7. **Recent Runs** below the card lists past runs; click any row to
   re-open the result modal.

## Other channels

### 5a. Mobile (Compose Multiplatform)

Same surface in Settings → Automate → Council Mode. Persona modal +
Add/Remove parity. Result modal renders as a stacked card list.

### 5b. REST

```sh
TOKEN=$(cat ~/.datawatch/token); BASE=https://localhost:8443

# List personas.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/council/personas

# Add a persona.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"cost-watcher","role":"Cost / budget impact","system_prompt":"You are a cost-watcher..."}' \
  $BASE/api/council/personas

# Remove a persona.
curl -sk -X DELETE -H "Authorization: Bearer $TOKEN" \
  $BASE/api/council/personas/contrarian

# Restore a default.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  $BASE/api/council/personas/contrarian/restore

# Run (async by default — returns immediately with a run ID).
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"proposal":"...","personas":["security-skeptic","perf-hawk"],"mode":"quick"}' \
  $BASE/api/council/run
# → {"id":"abc123","status":"running","events_path":"/api/council/runs/abc123/events","cancel_path":"/api/council/runs/abc123/cancel"}

# Watch live SSE stream (streams events as each persona responds).
curl -sk -N -H "Authorization: Bearer $TOKEN" \
  $BASE/api/council/runs/abc123/events

# Cancel an in-flight run.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  $BASE/api/council/runs/abc123/cancel

# Get final result (poll until status=completed or watch SSE).
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/council/runs/abc123

# Past runs.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/council/runs?limit=20
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/council/runs/<run-id>
```

### 5c. MCP

Tools: `council_personas`, `council_personas_get`, `council_personas_set`,
`council_run`, `council_list_runs`, `council_get_run`,
`council_run_cancel`, `council_config_get`, `council_config_set`,
`council_persona_oneshot`, `council_persona_draft_start`,
`council_persona_draft_answer`, `council_persona_draft_refine`,
`council_persona_draft_save`, `council_persona_draft_list`,
`council_persona_draft_purge`.

`council_run` args: `{ "proposal": "...", "personas": [...], "mode": "quick"|"debate" }`.
Returns `{id, status:"running", events_path}` immediately (async by default). Poll
`council_get_run` for completion or watch the SSE stream at `events_path`.
Use `council_run_cancel` to abort an in-flight run.

### 5d. Comm channel

| Verb | Example |
|---|---|
| `council quick "<proposal>"` | 1-round run, default personas. |
| `council debate "<proposal>"` | 3-round run, default personas. |
| `council quick personas=security-skeptic,perf-hawk "<proposal>"` | scoped run |
| `council personas` | List currently loaded. |
| `council cancel <run-id>` | Cancel an in-flight run. |

Milestone messages (run started / round complete / consensus reached) are
pushed to all configured comm channels automatically. Set
`council.comm_firehose: true` in YAML to also receive per-persona
response previews.

### 5e. YAML

Personas live as `~/.datawatch/council/personas/<name>.yaml`:

```yaml
name: security-skeptic
role: Security review
system_prompt: |
  You are a paranoid security reviewer. For the proposal, identify
  attack vectors, privacy risks, supply-chain concerns, and weakest
  authentication / authorization assumptions. Cite specific scenarios.
```

The hidden `.seeded` marker file tracks which default names have ever
been written to disk, so deletes survive daemon restarts and
new defaults from a future release land cleanly. Don't delete the
marker file; if you do, the daemon will re-create every default on
next start.

## Built-in personas (the 12 ship-with defaults)

| Name | Stance |
|---|---|
| `app-hacker` | Edge cases + adversarial input + abuse vectors |
| `contrarian` | Devil's advocate; challenges the proposal's premises |
| `data-architect` | Schema, query patterns, scaling, large/connected/enterprise data |
| `hacker` | Offensive security; how this gets exploited |
| `network-engineer` | Networking, load-balancing, network boundaries, network load |
| `ops-realist` | Production realities, observability, on-call burden |
| `perf-hawk` | Latency, throughput, resource budget |
| `platform-engineer` | Systems / operations of the running tech environment |
| `privacy` | PII, retention, consent, data minimization |
| `security-skeptic` | Defensive security; threat modeling |
| `simplicity-advocate` | Smallest thing that works; complexity push-back |
| `ux-advocate` | Operator + end-user experience |

The four personas an operator commonly asks for — `platform-engineer`, `network-engineer`, `data-architect` (covers the "data" responsibility), and `privacy` — all ship by default. No add step required.

## View / edit / add personas

| Surface | How |
|---|---|
| **PWA** | Settings → Automate → Council Mode card → click ⚙ **View / edit / add personas** button. Opens a modal with one expandable row per persona; inline-edit the YAML and Save writes to `~/.datawatch/council/personas/<name>.yaml`. |
| **YAML** | Edit `~/.datawatch/council/personas/<name>.yaml` directly (any text editor); changes load on next council run. |
| **REST** | `GET /api/council/personas` lists; `POST /api/council/personas` creates; `PUT /api/council/personas/<name>` updates; `DELETE /api/council/personas/<name>` removes. |
| **CLI** | `datawatch council personas {list,get <name>,create,update,delete}`. |
| **Comm** | `council personas list` / `council personas get <name>`. |
| **MCP** | `council_personas_list`, `council_personas_get`, `council_personas_create`, `council_personas_update`, `council_personas_delete`. |

To add a fresh persona via YAML:

```sh
cat > ~/.datawatch/council/personas/security-architect.yaml <<'EOF'
name: security-architect
role: Security Architect — threat models the proposal end-to-end
system_prompt: |
  You are a security architect on the council. For each proposal, evaluate:
  * Authentication / authorization gaps
  * Trust boundaries crossed
  * Least-privilege violations
  * Audit-log coverage
  Be specific; reference the proposal's actual surfaces.
EOF
```

Next council run picks it up automatically.

## How personas are distributed + installed

The 12 defaults are defined in Go (`internal/council/council.go`
`DefaultPersonas()`). On daemon start, `loadOrSeed`:

1. Reads `~/.datawatch/council/personas/`.
2. **Empty?** Writes all 12 default YAMLs + creates the `.seeded`
   marker.
3. **Not empty?** Loads existing YAMLs AND additively writes any
   default missing from disk that isn't recorded in `.seeded`.

So: defaults on disk are operator-editable copies; the Go-side
defaults are templates. After first run, the on-disk version is
authoritative. Removing a default via API/CLI/PWA writes its name
into `.seeded` so it stays removed across restarts; restore re-adds
the YAML AND drops the name from `.seeded`.

## Diagram

```
        ┌─────────────────┐
        │ defaults (Go)   │ ← built-in templates
        └────┬────────────┘
             │ first start (additive seed)
             ▼
  ┌──────────────────────────────┐
  │ ~/.datawatch/council/personas/│ ← authoritative on-disk copies
  │   <name>.yaml × 12            │
  │   .seeded (marker file)       │
  └────┬─────────────────────────┘
       │ loaded at daemon start + reload
       ▼
  ┌──────────────────────┐    ┌──────────────────┐
  │ council run          │───►│ runs/<id>.json    │
  │  (parallel inference │    │ rounds + synth    │
  │   per persona)       │    └──────────────────┘
  └──────────────────────┘
```

## Creating personas with the AI wizard

Instead of writing a `system_prompt` by hand, the wizard interviews you
with 5 questions and drafts the YAML via LLM. Available since v6.22.3.

### PWA

1. Settings → Automate → Council Mode → **View / edit personas**.
2. Click **+ Add Persona** → choose the **🤖 AI wizard** option.
3. Select a backend LLM (defaults to configured `council.llm_ref`).
4. Answer the 5 interview questions; each question has a **Refine**
   button that lets you iterate the LLM's draft.
5. **Save** writes the final YAML to
   `~/.datawatch/council/wizard-sessions.db` then to disk.
6. To re-interview an existing persona: click **🤖 Re-interview** on
   its row.

### CLI (one-shot)

```sh
datawatch council persona-wizard one-shot \
  --name security-architect \
  --role "Security Architect" \
  --focus "threat-modeling, trust-boundaries, least-privilege"
# → Drafts + saves the persona in a single LLM call.
```

### MCP

```json
{ "tool": "council_persona_oneshot", "args": { "name": "security-architect", "role": "Security Architect", "focus": "threat-modeling" } }
```

For multi-step refinement: `council_persona_draft_start` → `council_persona_draft_answer` × 5 → `council_persona_draft_refine` → `council_persona_draft_save`. List drafts with `council_persona_draft_list`; clean up with `council_persona_draft_purge`.

### Comm channel

```
council persona-wizard start name=cost-watcher role="Cost analyst"
# → question 1 of 5: ...
council persona-wizard answer <draft-id> "Focus on infra and third-party API spend"
council persona-wizard save <draft-id>
```

Drafts are stored in SQLite (`~/.datawatch/council/wizard-sessions.db`)
and auto-deleted after 7 days (configurable via `council.draft_retention_days`).

## Common pitfalls

- **Too many personas in Quick mode.** 12 personas × 1 round produces
  a long output to skim for "should we ship today?" — pick 3-5.
- **Vague proposal.** Personas can only push back on what's specific.
  *"Should we improve the API?"* gets vague responses. *"Should we
  switch /api/sessions/start from sync to async, returning 202 + a
  status URL?"* gets specific ones.
- **Confirmation bias when picking personas.** It's tempting to pick
  ones that'll agree with you. Always include `contrarian` for
  non-trivial decisions; `simplicity-advocate` if you suspect yourself
  of feature-creeping.
- **Skipping the dissent block.** The interesting bit isn't where
  personas agree — it's where they don't. Read dissent first.
- **Treating output as verdict.** Council augments your thinking; it
  doesn't replace the decision. The synthesizer's framing isn't the
  decision — you are.

## Linked references

- See also: `algorithm-mode.md` (Council can be invoked from Decide phase)
- API: `/api/council/*` (Swagger UI under `/api/docs`)
- Plan attribution: PAI Council in `../plan-attribution.md`

## Screenshots needed (operator weekend pass)

- [ ] PWA Council card (persona checkboxes, proposal textarea, mode dropdown)
- [ ] View / edit personas modal with the 12 defaults
- [ ] + Add Persona collapsible expanded
- [ ] Run result modal — quick mode (1 round + synthesizer)
- [ ] Run result modal — debate mode (3 rounds with cross-persona pushback)
- [ ] Recent Runs list
- [ ] CLI `datawatch council run` output

---

## See also

- [datawatch-definitions](../datawatch-definitions.md)
- [howto/identity-and-telos](identity-and-telos.md)
- [howto/algorithm-mode](algorithm-mode.md)
- [howto/evals](evals.md)
- [howto/autonomous-planning](autonomous-planning.md)
