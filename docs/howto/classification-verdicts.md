---
docs:
  index: true
  topics: [classification, verification, llm, guardrails]
exec_steps:
  - tool: llm_classifier
    description: Read the verification mode of every LLM, the capability probe status, the calibration status and the publisher allowlist
    args: {action: status}
    read_only: true
  - tool: llm_classifier
    description: List the publisher allowlist for classify-mode judge models
    args: {action: publishers}
    read_only: true
---
# How-to: classification verdicts

A judge can reach a verdict two ways. In **generate** mode (the default) it writes
a verdict and datawatch parses it. In **classify** mode datawatch asks the judge to
pick one lettered option and reads the probability of each option instead. The
verdict is then a number you can reproduce, not a sentence that has to be parsed.
The explanation shown next to a verdict is written separately and never changes it.

## Before you start

- A running daemon and a judge model served by a node that can return option
  probabilities (an Ollama node that supports it, or a server that returns log
  probabilities).
- Classify mode applies to verification and guardrail judging. Everything else is
  unchanged.

## 1. Allow the model's publisher

No publisher is trusted until you add it. The allowlist
(`classifier.publisher_allowlist`) is empty by default, and a model from a
publisher that is not on it is not used as a classify-mode judge. Adding a
publisher is a deliberate step and is recorded in the audit log.

| Channel | How |
|---|---|
| CLI | `datawatch llm classifier add-publisher <name> --confirm` |
| MCP | `llm_classifier` with `action: add_publisher`, `publisher`, `confirm: true` |
| REST | `POST /api/llm/classifier/publishers` with `{"publisher": "<name>", "confirm": true}` |
| Chat | `llm classifier add-publisher <name> confirm=true` |

Each is refused without the confirmation. You cannot change the list through
`PUT /api/config`. To read it: `datawatch llm classifier publishers`.

## 2. Turn classify mode on for an LLM

| Channel | How |
|---|---|
| Web app | Settings, LLM, edit an entry, **Verification mode** selector: empty (default, generate), `generate` or `classify` |
| CLI | `datawatch llm update <name> --verification-mode classify` |
| MCP | `llm_update` with `verification_mode: classify` |
| REST | update the LLM entry with `verification_mode` |

Leave it empty to follow the default. Changing it takes effect on the next verdict.

## 3. Probe and calibrate

Run `datawatch llm classifier status` (MCP `llm_classifier` `action: status`,
REST `GET /api/llm/classifier`, or chat `llm classifier status`). It shows three
things:

- **Verification mode** of every LLM.
- **Probe status** for each node, model and read path (`native` or `logprobs`).
  A probe checks that the node really returns usable option probabilities. A
  failed probe means classify mode will not work on that combination.
- **Calibration status** for each model fingerprint (model, quantization, node):
  the pass threshold, the held-out test result and when it was made. A threshold
  fits one model on one node. Change the model, its quantization or the node and
  the calibration is stale; until it is redone, verdicts from that combination
  block with an infra category instead of guessing.

Probe results are cached for a short time and refreshed on their own.

## 4. Read a verdict

In the web app the verdict card shows the **Route** (node, model, read path) and the
**Score vector** (pass probability against the threshold, plus the severity
scores), with the mode and category. A block marked **Low confidence** means the
classifier was unsure, not that the work was wrong; those are the ones to review by
hand.

| Channel | How |
|---|---|
| CLI | `datawatch llm classifier verdicts <automaton> [--task <id>] [--low-confidence]` |
| MCP | `llm_classifier` with `action: verdicts`, `automaton_id`, optional `task_id`, `low_confidence` |
| REST | `GET /api/llm/classifier/verdicts` for the Automaton |
| Chat | `llm classifier verdicts <automaton> [task=<id>] [low]` |

To decide a low-confidence block: `datawatch llm classifier review <automaton>
<task> retry|cancel`, or `autonomous_prd_resolve_task_review` over MCP.

Keeping the score vector on results is on by default; turn it off with
`autonomous.verification_record_scores: false` to keep only the mode.

## Failure behaviour

Classify mode fails closed.

- Missing scores, a timeout, a transport error, an abstain or a stale calibration
  becomes a **block** with an infra category. It is retried like other
  infrastructure faults and is not counted against the task's own work.
- A block is never turned into a pass later in the pipeline.
- datawatch never falls back to generate-and-parse by itself. If you want that,
  set the LLM's `verification_mode` back yourself.
- Two reads that disagree (pass, but severity high or critical) block with an
  `inconsistent-verdict` category.

## Tuning

Every tunable is a `classifier.*` key in the config file, read on each call (no
restart). Set one with `datawatch config set classifier.<key>=<value>`, chat
`configure classifier.<key>=<value>`, `PUT /api/config`, or Settings, Classifier
in the web app. A zero reads as the default.

| Key | Default | Effect |
|---|---|---|
| `timeout_seconds` | 30 | Bound on one classify call; a timeout is an infra block |
| `top_logprobs` | 10 | Probabilities asked for on the generic path |
| `abstain_margin` | 0.10 | Winner must lead the runner-up by this much |
| `min_probability` | 0.50 | Winner must reach this probability |
| `min_option_mass` | 0.50 | Options must hold this share of first-token probability |
| `rotate_options` | false | Rotate option order and average (generic path) |
| `thinking_off_models` | `["qwen3"]` | Model-name parts that get thinking switched off |
| `probe_ttl_seconds` | 600 | How long a probe result is cached |
| `probe_timeout_seconds` | 5 | Bound on one probe |
| `calibration.test_fraction` | 0.30 | Held-out share |
| `calibration.split_seed` | `classifier-calibration` | Salt for the split |
| `calibration.min_fit_samples` | 20 | Least samples to fit a threshold |
| `calibration.false_pass_weight` | 5 | False blocks one false pass costs |
| `calibration.rotation_bias_threshold` | 0.10 | Bias at which rotation is recommended |
| `calibration.rotation_flip_threshold` | 0.10 | Flip share at which rotation is recommended |

## Optional: scope-breadth check

`autonomous.scope_breadth_enabled: true` makes a task that failed on content get
one more question: does it bundle several separable units? If so, the failure is
marked too broad so it can be split. Off by default; it needs classify mode.

See also: [`docs/config-reference.yaml`](../config-reference.yaml) and the
Classification verdicts section of [`docs/operations.md`](../operations.md).
