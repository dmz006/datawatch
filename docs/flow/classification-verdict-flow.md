# Classification verdict flow

How a verification or guardrail judge reaches a verdict in **classify** mode, how the
publisher allowlist gates which models may judge, and where a low-confidence block
goes. Operator walkthrough: [howto/classification-verdicts.md](../howto/classification-verdicts.md).
Definitions: [datawatch-definitions.md](../datawatch-definitions.md#classification-verdicts-and-the-publisher-allowlist).

```mermaid
flowchart TD
    J[Judge call for a task or guardrail] --> M{LLM verification_mode}
    M -- generate / empty --> G[Judge writes a verdict, datawatch parses it]
    M -- classify --> P{Model publisher on<br/>classifier.publisher_allowlist?}
    P -- no --> B0[Block, setup category]
    P -- yes --> C{Fresh calibration for<br/>model + quantization + node?}
    C -- no --> B1[Block, infra category: stale or missing calibration]
    C -- yes --> R[Capability probe picks read path:<br/>native /v1/systemone or generic logprobs]
    R --> Q[Closed-option prompt, options lettered A, B, C]
    Q --> S{Scores usable?}
    S -- missing / timeout / transport error --> B2[Block, infra category]
    S -- yes --> T{Winner leads by abstain_margin<br/>and reaches min_probability?}
    T -- no --> BL[Block flagged low confidence]
    T -- yes --> V[Verdict record: mode, score vector, route, outcome]
    BL --> H[Held for a manual decision:<br/>llm classifier review / resolve_task_review]
```

## Rules the flow enforces

- **Fail closed.** Every infra-category block is retried as infrastructure and is not
  counted against the task. A block is never turned into a pass later, and there is
  no silent fall-back to generate mode.
- **The allowlist is operator-only.** It starts empty. A publisher is added only with
  an explicit confirmation (REST, MCP, CLI or chat), the addition is audited, and
  `PUT /api/config` refuses `classifier.publisher_allowlist`.
- **Tunables** (`classifier.timeout_seconds`, `abstain_margin`, `min_probability`,
  `min_option_mass`, `rotate_options`, the probe and calibration keys) are read on
  every call; see [config-reference.yaml](../config-reference.yaml).
- **The explanation is separate.** The prose shown next to a verdict never changes it.
