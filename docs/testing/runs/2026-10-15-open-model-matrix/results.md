# Open-model matrix eval — run 20261015-r5

Automaton: `None`  
Preflight passed: **False**

## Preflight

| role | llm | expected | used | ok |
|---|---|---|---|---|
| cpu | opencode-johnnyjohnny-cpu | qwen3:1.7b | None | NO |
| fast | opencode-johnnyjohnny-fast | granite4.2:8b | None | NO |
| mid | opencode-johnnyjohnny-gpu | granite4.2:30b | None | NO |
| decompose-validate | opencode-datawatch-gpu | gpt-oss:120b | None | NO |

## Preflight failure — run stopped (no matrix built)

All 4 entries errored, so per the run rules no cells were seeded, no scratch
Automaton was created, and `cells` is `[]`. Every `llm test` call returned
HTTP 502 from the daemon wrapping an Ollama 404:

| llm | error |
|---|---|
| opencode-johnnyjohnny-cpu | compute node "johnnyjohnny-cpu": ollama HTTP 404: `model 'ollama/qwen3:1.7b' not found` |
| opencode-johnnyjohnny-fast | compute node "johnnyjohnny": ollama HTTP 404: `model 'ollama/granite4.2:8b' not found` |
| opencode-johnnyjohnny-gpu | compute node "johnnyjohnny": ollama HTTP 404: `model 'ollama/granite4.2:30b' not found` |
| opencode-datawatch-gpu | compute node "datawatch": ollama HTTP 404: `model 'ollama/gpt-oss:120b' not found` |

**Cause (observed, not fixed here):** the top-level `model` field of all four
registry entries now carries opencode's `provider/model` form
(`ollama/qwen3:1.7b`, …; `updated_at` 2026-10-15T05:28–05:29Z, minutes
before this run, apparently in response to the r4 opencode worker launch
failure). The per-node `models[]` still list the bare Ollama names
(`qwen3:1.7b` etc.). The `/api/llms/<name>/test` path forwards the top-level
string to Ollama verbatim, and Ollama has no model named `ollama/…`. The models
themselves are not shown to be missing. The two places that need the model
name disagree on its format: opencode wants the `ollama/` prefix, while the
direct Ollama test probe needs it stripped. Until one side normalises the
prefix, preflight cannot pass with these entries. Daemon version at run:
9.1.2.

## Wrong model cells

None.

## Suspect integrity cells

None.

## Model results

Trustworthy cells only (identity ok AND integrity ok); `—` = untrusted/not run.

| role | analysis | scripting | docs | review |
|---|---|---|---|---|
| cpu | — | — | — | — |
| fast | — | — | — | — |
| mid | — | — | — | — |
| decompose-validate | — | — | — | — |
