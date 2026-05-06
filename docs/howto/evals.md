# How-to: Evals — rubric-based grading

Replace binary pass/fail with explicit rubrics. Each suite has a
`mode` (capability or regression) with a corresponding pass-threshold,
and a list of graders that score the output against the rubric.

## What it is

Suites of grader rules at `~/.datawatch/evals/<name>.yaml`. A suite has:

- A **name** (filename without extension).
- A **mode**:
  - `capability` (default 70% threshold) — exploring whether a backend /
    model can handle a task class. Good enough most of the time is good
    enough.
  - `regression` (default 99% threshold) — gating CI / pre-deploy. Used
    when a regression below the threshold should fail the run.
- A **threshold_pct** override (optional).
- A list of **graders**, each with a `type`, `weight`, and type-specific
  parameters.

Four grader types:

| Type | What it does | Use case |
|---|---|---|
| `string_match` | Output contains / equals a target string. Cheap. | Confirm a specific token is present. |
| `regex_match` | Output matches a regex. | Pattern checks ("starts with `{`"). |
| `binary_test` | Invokes a shell command with the output piped in; exit 0 = pass. | Real validators (`jq empty`, `pytest`, etc.). |
| `llm_rubric` | Second-pass LLM grades against a written rubric. Slow + token cost. | Subjective qualities (clarity, tone, structure). |

The suite's score is `Σ(weight × pass) / Σ(weight)` × 100. Pass if
score ≥ threshold_pct.

## When to use it

- **Capability suites** — exploring whether a backend or model is up
  to a task class before committing to it. e.g. *"Can ollama llama3.1
  reliably produce valid JSON?"*
- **Regression suites** — gating before a release / merge / deploy.
  e.g. *"Does my new prompt template still produce parseable output?"*
- **Algorithm Mode Measure phase** — see [`algorithm-mode.md`](algorithm-mode.md).

## 1. Write a suite

```sh
$EDITOR ~/.datawatch/evals/json-output.yaml
```

```yaml
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
    backend: ollama       # optional; default: same backend as the session
    model: llama3.1:8b    # optional; default: backend's default
```

Run it manually to test:

```sh
datawatch evals run json-output --input "Give me a JSON object describing yourself"
```

Output:

```
suite=json-output mode=capability threshold=70 score=83 PASS
  ✓ regex_match (weight 1) — starts with an opening brace
  ✓ binary_test (weight 2) — parses as JSON
  ✗ llm_rubric (weight 1) — trailing apology detected
```

## 2. Inspect runs

Every run lands at `~/.datawatch/evals/runs/<run-id>.json` with the full
input, output, per-grader verdict, and aggregate score.

```sh
datawatch evals runs                   # list recent
datawatch evals runs --suite json-output --limit 20
datawatch evals get-run <run-id>       # full detail
```

PWA: Settings → Automate → **Evals** card lists configured suites +
recent runs. Click any run to see per-grader pass/fail with
explanations.

## 3. Wire into Algorithm Mode

In a session running Algorithm Mode (see [`algorithm-mode.md`](algorithm-mode.md)),
the Measure phase can auto-run a suite against the captured Act-phase
output:

```sh
datawatch algorithm measure <session-id> --evals json-output
```

If `mode: regression` and the score is below threshold, the daemon
warns before letting Algorithm Mode advance to Learn — surfacing the
regression at the gate where a human can decide whether to fix-and-retry
or accept-with-rationale.

## 4. Wire into autonomous PRDs

In an Automaton spec, declare the suite under the story:

```yaml
stories:
  - title: Add JSON endpoint
    evals_suite: json-output
    tasks:
      - title: Implement handler
      - title: Write tests
```

When the autonomous executor finishes the story, it runs the suite
against the produced output. A regression-mode failure puts the story
into `blocked` state for operator review.

## 5. Compare backends across a suite

```sh
datawatch evals compare json-output --backends claude-code,opencode-acp,ollama
```

Runs the same suite against each backend and prints a per-backend
score table. Useful when picking a backend for a workflow.

## CLI reference

```sh
datawatch evals list                              # configured suites
datawatch evals run <suite> --input "..."         # ad-hoc run
datawatch evals run <suite> --file ./input.txt    # piped input
datawatch evals runs [--suite ...] [--limit N]    # past runs
datawatch evals get-run <run-id>                  # full run detail
datawatch evals compare <suite> --backends ...    # cross-backend table
datawatch evals delete-suite <name>               # remove
```

## REST / MCP

- `POST /api/evals/run {suite, input}` → run
- `GET /api/evals/list` → suites
- `GET /api/evals/runs?suite=&limit=` → past runs
- `GET /api/evals/runs/{id}` → run detail
- MCP: `evals_run`, `evals_list`, `evals_runs`, `evals_get_run`.

## Common pitfalls

- **Mixed-mode suites.** A capability suite that includes a brittle
  regex_match grader will keep tipping below threshold for cosmetic
  reasons. Either loosen the regex or split it into a separate
  regression suite.
- **LLM rubric self-grading.** Using the same model for `llm_rubric`
  that produced the output biases the grade upward. Use a different
  backend for the grader where possible.
- **No description on graders.** When a suite fails 6 months later
  you won't remember why grader #3 exists. Always set `description:`.
- **Threshold gymnastics.** If you find yourself adjusting `threshold_pct`
  on a regression suite to make it pass, the regression already
  happened. Roll back instead.

## Linked references

- Plan attribution: PAI Evals in `plan-attribution.md`
- See also: `algorithm-mode.md` for Measure-phase auto-grading
- See also: `autonomous-planning.md` for per-story evals in Automata
- API: `/api/evals/*` (Swagger UI under `/api/docs`)

## All channels reference

| Channel | How |
|---|---|
| **PWA** | Settings → Automate → Evals card → suite list, run, view runs. |
| **Mobile** | Settings → Automate → Evals; same surface. |
| **REST** | `POST /api/evals/run`, `GET /api/evals/{list,runs,runs/<id>}`. |
| **MCP** | `evals_run`, `evals_list`, `evals_runs`, `evals_get_run`. |
| **CLI** | `datawatch evals {list,run,runs,get-run,compare,delete-suite}`. |
| **Comm** | `evals run <suite> "<input>"`, `evals runs` from any chat channel. |
| **YAML** | Suites authored as `~/.datawatch/evals/<name>.yaml`. Runs persisted to `~/.datawatch/evals/runs/<id>.json`. |

## Screenshots needed (operator weekend pass)

- [ ] PWA Evals card with suite list
- [ ] Run modal showing per-grader pass/fail with explanations
- [ ] Suite YAML in editor
- [ ] CLI `datawatch evals run` output (pass + fail examples)
- [ ] Compare-backends table
