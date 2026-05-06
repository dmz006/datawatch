# How-to: Evals — rubric-based grading

Replace binary pass/fail with explicit rubrics. Each suite has a `mode`
(capability or regression) with a corresponding pass-threshold, and a
list of graders that score the output against the rubric.

## What it is

Suites of grader rules at `~/.datawatch/evals/<name>.yaml`. A suite has:

- A **name** (filename without extension).
- A **mode**:
  - `capability` (default 70% threshold) — exploring whether a backend
    or model can handle a task class.
  - `regression` (default 99% threshold) — gating CI / pre-deploy.
- An optional **threshold_pct** override.
- A list of **graders**, each with `type`, `weight`, and type-specific
  parameters.

Four grader types:

| Type | Does | Typical use |
|---|---|---|
| `string_match` | Output contains/equals a target string. | Confirm a specific token. |
| `regex_match` | Output matches a regex. | Pattern checks. |
| `binary_test` | Pipes output into a shell command; exit 0 = pass. | Real validators (`jq empty`, `pytest`). |
| `llm_rubric` | Second-pass LLM grades against a written rubric. | Subjective qualities. |

Score = `Σ(weight × pass) / Σ(weight) × 100`. Pass if score ≥ threshold.

## Base requirements

- `datawatch start` — daemon up.
- For `binary_test` graders: the test command available on PATH.
- For `llm_rubric` graders: a configured LLM backend (any will do; a
  smaller/faster model is usually fine).

## Setup

```sh
mkdir -p ~/.datawatch/evals
```

That's it — suites live as YAML files in that directory; the daemon
discovers them on every `evals run`.

## Two happy paths

### 4a. Happy path — CLI

```sh
# 1. Author a suite.
cat > ~/.datawatch/evals/json-output.yaml <<'EOF'
name: json-output
mode: capability
threshold_pct: 70
graders:
  - type: regex_match
    pattern: '^\s*\{'
    weight: 1
    description: starts with an opening brace
  - type: binary_test
    command: jq empty
    weight: 2
    description: parses as JSON
  - type: llm_rubric
    rubric: |
      Does the output answer the prompt without trailing commentary,
      apologies, or markdown fences? Score 0-1.
    weight: 1
EOF

# 2. List configured suites.
datawatch evals list
#  → json-output  capability  70%  3 graders

# 3. Run the suite against an ad-hoc input.
datawatch evals run json-output --input "Give me a JSON object describing yourself"
#  → suite=json-output mode=capability threshold=70 score=83 PASS
#      ✓ regex_match (weight 1) — starts with an opening brace
#      ✓ binary_test (weight 2) — parses as JSON
#      ✗ llm_rubric (weight 1) — trailing apology detected

# 4. List + inspect runs (persisted at ~/.datawatch/evals/runs/).
datawatch evals runs --suite json-output --limit 10
datawatch evals get-run <run-id>

# 5. Compare across backends.
datawatch evals compare json-output --backends claude-code,opencode-acp,ollama
#  → claude-code      score=92  PASS
#    opencode-acp     score=83  PASS
#    ollama:llama3    score=58  FAIL (capability threshold 70)
```

### 4b. Happy path — PWA

1. PWA → Settings → Automate → **Evals** card.
2. Click **+ New Suite**. The editor opens with a starter YAML
   template. Fill in name, mode, threshold, graders. **Save**.
3. Back on the card, your suite appears in the list with mode + grader
   count + last-run summary.
4. Click **Run** next to the suite. A modal asks for an input string
   (or a file). Type the prompt; click **Run**.
5. Result modal renders the per-grader pass/fail with explanations and
   the aggregate score in a colored badge (green PASS / red FAIL).
6. The **Recent Runs** section below the suite list shows the last
   ~20 runs across all suites; click any row to re-render the result
   modal.

## Other channels

### 5a. Mobile (Compose Multiplatform)

Same surface — Settings → Automate → Evals card. Suite editor is a
multi-line text input rendered as YAML. Run + view-results parity.

### 5b. REST

```sh
# Run a suite.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"suite":"json-output","input":"Give me JSON about yourself"}' \
  $BASE/api/evals/run

# List configured suites.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/evals/list

# List recent runs.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/evals/runs?suite=json-output&limit=10

# Fetch a run by ID.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/evals/runs/<run-id>
```

### 5c. MCP

Tools: `evals_list`, `evals_run`, `evals_runs`, `evals_get_run`.

`evals_run` accepts `{ "suite": "<name>", "input": "<text>" }` and
returns the result object (including per-grader verdicts). Useful when
the LLM in a session wants to self-grade its own output before
declaring done.

### 5d. Comm channel

| Verb | Example |
|---|---|
| `evals list` | List configured suites. |
| `evals run <suite> "<input>"` | Run + return summary. |
| `evals runs <suite>` | List recent runs. |

### 5e. YAML

Suites at `~/.datawatch/evals/<name>.yaml`. Schema documented above.
Runs persist to `~/.datawatch/evals/runs/<run-id>.json` with full input,
output, per-grader verdict, aggregate score.

## Wire into Algorithm Mode + Automata

- **Algorithm Mode Measure phase** — `datawatch algorithm measure
  <session-id> --evals <suite>` runs the suite against the captured
  Act-phase output. See `algorithm-mode.md`.
- **Automaton story** — declare `evals_suite: <name>` on a story in
  the PRD spec; the autonomous executor runs the suite when the story
  finishes. Regression failures put the story into `blocked`.

## Diagram

```
  ┌──────────────────────┐
  │ ~/.datawatch/evals/  │ ← author suites here
  │   <name>.yaml         │
  └──────────┬───────────┘
             │
             ▼
  ┌──────────────────────┐    ┌───────────────────┐
  │ datawatch evals run  │───►│ runs/<id>.json     │ ← persisted result
  │  + per-grader verdict│    └───────────────────┘
  └──────────┬───────────┘
             │ optional
             ▼
  ┌──────────────────────┐
  │ Algorithm Measure /  │
  │ Automaton story gate │
  └──────────────────────┘
```

## Common pitfalls

- **Mixed-mode suites.** A capability suite that includes a brittle
  `regex_match` grader will keep tipping below threshold for cosmetic
  reasons. Either loosen the regex or split into a separate regression
  suite.
- **LLM rubric self-grading.** Using the same model for `llm_rubric`
  that produced the output biases the grade upward. Use a smaller /
  different backend for the grader where possible.
- **No description on graders.** When a suite fails 6 months later you
  won't remember why grader #3 exists. Always set `description:`.
- **Threshold gymnastics.** If you find yourself adjusting
  `threshold_pct` on a regression suite to make it pass, the
  regression already happened. Roll back.
- **Slow `binary_test` commands.** Each grader runs serially per
  invocation. `pytest -k some_slow_thing` makes the suite minutes-long.
  Move expensive validators to a separate suite you only run on demand.

## Linked references

- See also: `algorithm-mode.md` (Measure-phase auto-grading)
- See also: `autonomous-planning.md` (per-story evals)
- Architecture: `../architecture-overview.md` § Verification + grading
- Plan attribution: PAI Evals in `../plan-attribution.md`

## Screenshots needed (operator weekend pass)

- [ ] PWA Evals card with multiple suites listed
- [ ] Suite editor modal with starter YAML template
- [ ] Run result modal — PASS example with green badge
- [ ] Run result modal — FAIL example with per-grader explanations
- [ ] Compare-backends table view
- [ ] CLI `datawatch evals run` output (PASS + FAIL examples)
