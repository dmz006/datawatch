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
  `council.backend` in `~/.datawatch/datawatch.yaml`; defaults to the
  daemon's primary backend.
- Disk space for persona YAMLs + run history (negligible).

## Setup

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

# 2. Quick check — 1 round, 3 personas, immediate read.
datawatch council run \
  --proposal "Should we switch the autonomous executor to fan-out workers per story?" \
  --personas security-skeptic,perf-hawk,simplicity-advocate \
  --mode quick
#  → started run abc123; returns summary on completion (~30s)

# 3. Fetch the result.
datawatch council get-run abc123
#  → consensus: ...
#    dissent: ...
#    rounds:
#      - persona: security-skeptic
#        response: "Fan-out workers introduce a new IPC surface ..."
#      ...

# 4. Debate — 3 rounds, full council, longer wait but richer output.
datawatch council run --proposal "Same proposal" --mode debate
#  → started run def456; ~3-5 min for full council × 3 rounds

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

# Run.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"proposal":"...","personas":["security-skeptic","perf-hawk"],"mode":"quick"}' \
  $BASE/api/council/run

# Past runs.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/council/runs?limit=20
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/council/runs/<run-id>
```

### 5c. MCP

Tools: `council_personas`, `council_personas_add`,
`council_personas_delete`, `council_personas_restore`, `council_run`,
`council_runs`, `council_get_run`.

`council_run` args: `{ "proposal": "...", "personas": [...], "mode": "quick"|"debate" }`.
Returns a run object once complete (synchronous from the MCP host's
perspective; the daemon parallelizes per-persona inference internally).

### 5d. Comm channel

| Verb | Example |
|---|---|
| `council quick "<proposal>"` | 1-round run, default personas. |
| `council debate "<proposal>"` | 3-round run, default personas. |
| `council quick personas=security-skeptic,perf-hawk "<proposal>"` | scoped run |
| `council personas` | List currently loaded. |

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
