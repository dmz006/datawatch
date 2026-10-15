# Open-model matrix eval — run 20261015-r3

Automaton: `None`  
Preflight passed: **False**

## Preflight

| role | llm | expected | used | ok |
|---|---|---|---|---|
| cpu | opencode-johnnyjohnny-cpu | qwen3:1.7b | qwen3:1.7b | yes |
| fast | opencode-johnnyjohnny-fast | granite4.2:8b | granite4.2:8b | yes |
| mid | opencode-johnnyjohnny-gpu | granite4.2:30b | None | NO |
| decompose-validate | opencode-datawatch-gpu | gpt-oss:120b | gpt-oss:120b | yes |

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

The matrix was **not built or run** (no setup, no Automaton, `cells=[]`). The
`mid` entry (`opencode-johnnyjohnny-gpu`, expected `granite4.2:30b` on node
johnnyjohnny) errored:
`POST https://127.0.0.1:8443/api/llms/opencode-johnnyjohnny-gpu/test: The read operation timed out`
— no `used_model` came back, so its identity could not be confirmed. The other
three entries (qwen3:1.7b, granite4.2:8b, gpt-oss:120b) reported the expected
model.

This is the third preflight attempt today and the failures don't line up.
Run `2026-10-15` timed out on `gpt-oss:120b` while all three johnnyjohnny entries
passed. Run `20261015-r2` passed all four. This run (`20261015-r3`) timed out on
`granite4.2:30b`, and gpt-oss:120b passed. The likely cause is model
cold-load/eviction on the shared GPU node, which outlasts the driver's 60s HTTP
read timeout; the entry itself is not misconfigured. It also suggests the
pass/fail result depends on timing. Raising the preflight timeout, or pre-warming
each model before preflight, would make this gate deterministic.
