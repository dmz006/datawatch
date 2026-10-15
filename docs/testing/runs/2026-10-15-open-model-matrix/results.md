# Open-model matrix eval — run 2026-10-15

Automaton: `None`  
Preflight passed: **False**

## Preflight

| role | llm | expected | used | ok |
|---|---|---|---|---|
| cpu | opencode-johnnyjohnny-cpu | qwen3:1.7b | qwen3:1.7b | yes |
| fast | opencode-johnnyjohnny-fast | granite4.2:8b | granite4.2:8b | yes |
| mid | opencode-johnnyjohnny-gpu | granite4.2:30b | granite4.2:30b | yes |
| decompose-validate | opencode-datawatch-gpu | gpt-oss:120b | None | NO |

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

## Preflight failure

The matrix was **not built or run**. The `decompose-validate` entry
(`opencode-datawatch-gpu`, expected `gpt-oss:120b` on node datawatch) errored:
`POST https://127.0.0.1:8443/api/llms/opencode-datawatch-gpu/test: The read operation timed out`
— no `used_model` was returned, so its identity could not be confirmed. The
other three entries (qwen3:1.7b, granite4.2:8b, granite4.2:30b) reported the
expected model. Likely cause: the 120B model cold-loading on the datawatch GPU
node past the driver's HTTP read timeout, or that node/Ollama being unreachable;
re-run preflight once the node answers `llm_test` for this entry.
