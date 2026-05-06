# How-to: Council Mode — multi-persona structured debate

Surface a non-trivial decision to a council of personas, get a
synthesized consensus + dissent list back. Each persona has a
distinct perspective and is asked to push back on the proposal from
that angle.

## What it is

The daemon ships with default personas (security-skeptic, ux-advocate,
perf-hawk, simplicity-advocate, ops-realist, contrarian, platform-engineer,
network-engineer, data-architect, privacy, hacker, app-hacker — 12 in
v6.12.4+). Each is just a YAML file at
`~/.datawatch/council/personas/<name>.yaml` containing:

```yaml
name: security-skeptic
role: Security review
system_prompt: |
  You are a paranoid security reviewer. For the proposal, identify
  attack vectors, privacy risks, supply-chain concerns, and weakest
  authentication / authorization assumptions. Cite specific scenarios.
```

Personas are loaded at daemon start. Operator can edit, add, remove —
`.seeded` marker tracks which defaults have been written so deletions
stick across restarts and new defaults from a future release land
cleanly.

## When to use it

- **Quick mode (1 round)** — fast perspective check. *"Should we ship
  feature X this week?"* — cheap, useful for sanity-check.
- **Debate mode (3 rounds)** — serious decisions where you want
  cross-persona pushback. The synthesizer can see what each persona
  said in earlier rounds, so personas push back on each other's
  arguments. Good for design proposals, architecture changes, security
  reviews, "should we do this at all" questions.

## 1. Run a quick check

PWA: Settings → Automate → **Council Mode**.

1. Pick personas (default: all 10/12).
2. Type the proposal in the textarea.
3. Mode: **Quick (1 round)**.
4. Click **Run Council**.

Result modal shows each persona's response + the synthesizer's
consensus + dissent block.

CLI:

```sh
datawatch council run \
  --proposal "Switch the autonomous executor to fan-out workers per story" \
  --personas security-skeptic,perf-hawk,simplicity-advocate \
  --mode quick
```

Returns a run-id; fetch the detail with:

```sh
datawatch council get-run <run-id>
```

## 2. Run a debate

3-round debate:

```sh
datawatch council run --proposal "..." --mode debate
```

What happens:

- **Round 1** — every selected persona responds independently to the
  proposal. (Same as Quick mode.)
- **Round 2** — every persona sees Round 1's responses and can push
  back on what others said. Replies are explicitly labeled as
  responding to specific persona / claim.
- **Round 3** — final round, personas can soften / harden / change
  position. Often the most useful — where actual learning shows up.
- **Synthesizer** — combines all responses into a `consensus` block
  (where personas agree) and a `dissent` block (where they don't).
  The dissent block is usually more interesting than the consensus.

## 3. Pick the right personas

The default 12 cover most decisions. Some heuristics:

- **Touches user-visible behaviour** → ux-advocate, simplicity-advocate.
- **Touches network / cluster boundaries** → network-engineer,
  platform-engineer, ops-realist.
- **Touches data flows** → data-architect, privacy, security-skeptic.
- **Touches authn / authz / new external integrations** →
  security-skeptic, hacker, app-hacker.
- **Major design decision** → contrarian + simplicity-advocate to
  push back on momentum.
- **Performance-critical work** → perf-hawk + platform-engineer.

You can run with as few as 2 personas (cheap; less rich) or all of
them (expensive; long output to skim).

## 4. Edit / view personas

PWA: Council card → **View / edit personas** button. Modal lists
every loaded persona with its name, role, system_prompt, and on-disk
path. Read-only in v6.12.3; v6.12.4+ adds in-app Add / Remove.

YAML edits work too — they're picked up at next daemon start (or `datawatch reload`):

```sh
$EDITOR ~/.datawatch/council/personas/perf-hawk.yaml
datawatch reload
```

## 5. Add a new persona

PWA: Council card → **+ Persona** (v6.12.4+) → fill name, role, system_prompt → **Save**.

YAML:

```sh
cat > ~/.datawatch/council/personas/cost-watcher.yaml <<'EOF'
name: cost-watcher
role: Cost / budget impact
system_prompt: |
  You are a cost-watcher. For the proposal, identify infrastructure
  cost impact (compute, storage, network egress, third-party API
  spend) and operator time-cost. Flag anything that increases either
  by >20%. Suggest cheaper variants where possible.
EOF
datawatch reload
```

## 6. Remove a persona

PWA: Council card → **View / edit personas** → click the **× Remove**
button next to the persona. The daemon deletes the YAML and adds the
name to `.seeded` so it doesn't get auto-recreated on the next start.

CLI:

```sh
datawatch council personas-delete contrarian
```

To restore a default persona you removed:

```sh
datawatch council personas-restore-default contrarian
```

(removes the name from `.seeded` AND re-writes the YAML).

## 7. Read a past run

```sh
datawatch council runs                           # recent runs (paginated)
datawatch council get-run <run-id>               # full detail (one big JSON)
datawatch council get-run <run-id> --format md   # markdown render
```

PWA: Council card → **Recent Runs** list → click a row → modal with
the full transcript.

## CLI reference

```sh
datawatch council personas                       # list registered
datawatch council personas-add --name ... --role ... --system-prompt ...
datawatch council personas-delete <name>
datawatch council personas-restore-default <name>
datawatch council run --proposal "..." --personas ... --mode quick|debate
datawatch council runs [--limit N]
datawatch council get-run <run-id> [--format md|json]
```

## REST / MCP

- `GET /api/council/personas` → list
- `POST /api/council/personas` → add
- `DELETE /api/council/personas/<name>` → remove
- `POST /api/council/personas/<name>/restore` → restore default
- `POST /api/council/run {proposal, personas, mode}` → run
- `GET /api/council/runs/<id>` → detail
- MCP: `council_personas`, `council_run`, `council_get_run`, etc.

## How personas are distributed

The 12 default personas are defined in Go (`internal/council/council.go`
`DefaultPersonas()`). On daemon start, `loadOrSeed` checks
`~/.datawatch/council/personas/`:

- **Empty** → writes all 12 default YAMLs to disk + creates the
  `.seeded` marker file listing each name.
- **Not empty** → loads existing YAMLs AND additively writes any
  default that's missing from disk AND isn't recorded in `.seeded`
  (so a new default added in a future release lands automatically;
  ones the operator deliberately deleted stay deleted).

This means: the YAMLs you see on disk are operator-editable copies.
The Go-side defaults are templates that seed those copies. After first
run, the on-disk version is authoritative.

## Common pitfalls

- **Too many personas in Quick mode.** With 12 personas you'll get a
  long-to-skim output for a quick decision. Pick 3–5.
- **Vague proposal.** Personas can only push back on what's specific.
  *"Should we improve the API?"* gets vague responses. *"Should we
  switch /api/sessions/start from sync to async, returning 202 + a
  status URL?"* gets specific ones.
- **Confirmation bias.** It's tempting to pick personas that'll
  agree with you. Always include `contrarian` for non-trivial
  decisions; consider also `simplicity-advocate` if you suspect
  yourself of feature-creeping.
- **Skipping the dissent block.** The interesting bit isn't where
  personas agree — it's where they don't. The synthesizer's
  `dissent` block is the high-signal part.
- **One-shot decisions.** Council Mode isn't replacing a meeting; it
  augments your own thinking. Treat the output as input, not verdict.

## Linked references

- Plan attribution: PAI Council in `plan-attribution.md`
- See also: `algorithm-mode.md` (Council can be invoked from Decide
  phase to surface trade-offs)
- API: `/api/council/*` (Swagger UI under `/api/docs`)

## All channels reference

| Channel | How |
|---|---|
| **PWA** | Settings → Automate → Council Mode → pick personas, type proposal, run; View / edit personas modal for add/remove. |
| **Mobile** | Same surface in Compose Multiplatform. |
| **REST** | `POST /api/council/run`, `GET /api/council/personas`, `POST/DELETE /api/council/personas[/<name>]`, `POST /api/council/personas/<name>/restore`, `GET /api/council/runs[/{id}]`. |
| **MCP** | `council_personas`, `council_run`, `council_personas_add`, `council_personas_delete`, `council_personas_restore`, `council_get_run`. |
| **CLI** | `datawatch council {personas,personas-add,personas-delete,personas-restore-default,run,runs,get-run}`. |
| **Comm** | `council quick "<proposal>"`, `council debate "<proposal>"`, `council personas` from any chat channel. |
| **YAML** | Personas live as `~/.datawatch/council/personas/<name>.yaml`. `.seeded` marker tracks which defaults have been written. |

## Screenshots needed (operator weekend pass)

- [ ] PWA Council card (persona checkboxes, proposal textarea, mode dropdown)
- [ ] View / edit personas modal with the 12 defaults
- [ ] Add Persona form expanded
- [ ] Run result modal — quick mode (1 round + synthesizer)
- [ ] Run result modal — debate mode (3 rounds)
- [ ] Recent Runs list
