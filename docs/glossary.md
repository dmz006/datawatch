# Glossary

Short definitions of terms used across the docs. The full reference is
[datawatch-definitions.md](datawatch-definitions.md).

## Pool fall-through

The setting `autonomous.capacity_fallthrough` (default off). When on, a worker
task whose requested capacity pool cannot be granted is admitted on an
alternate backend that has room instead of waiting. Verifiers never fall
through. See [howto/automata-capacity.md](howto/automata-capacity.md#pool-fall-through).

## Allowed fall-through target

An LLM entry setting (`fallthrough_target`, default off). With pool
fall-through on, a marked entry may receive a worker whose requested pool is
full. The other lane fill backend is tried first and needs no mark. See
[howto/automata-capacity.md](howto/automata-capacity.md#pool-fall-through).

## Per-LLM usage hold

A usage back-off flag scoped to one LLM entry (and, optionally, a named list
of its models — **held models**) rather than a whole backend. While it holds,
new autonomous work on those models waits with a `backoff:` reason naming them;
work on the other models of the entry and on other backends keeps running. It
never blocks an interactive session and survives a restart. The burn guard's
`hold_entry` rung sets it scoped to the entry it guards. See
[howto/usage-backoff.md](howto/usage-backoff.md#holding-only-certain-models).

## Cheaper-model rung

The burn guard's `route_cheaper_model` ladder rung: it keeps the entry's
backend and sends the entry's new work to the entry's cheaper model — Haiku
for a Claude entry, otherwise its own cheapest other priced model by its
per-model cost rates (or the model the entry's `route_cheaper_model` field
names). It is a no-op for an entry that resolves no cheaper model, and it is
**not in the default ladder**; you opt in by naming it where it sits, before
the hold rung. Work already running continues. See
[howto/burn-guard.md](howto/burn-guard.md#the-ladder).

## Per-model cost rate

A USD-per-1K-token rate set on one model of an LLM entry (`in_per_k`,
`out_per_k`, plus `cache_read_per_k` / `cache_write_per_k`, any subset). It
wins for that model over the entry's per-backend rate, which is the
field-by-field fallback. Per-model rates price the cost report's by-model rows,
the burn guard's per-entry projection, and the comparison the cheaper-model
rung uses to pick an entry's cheapest other model. See
[datawatch-definitions.md](datawatch-definitions.md#cost-rates-usd--1k-tokens).

## Metered vs flat guard plan

How a burn guard entry reads its budget: `flat` — a weekly percentage against
the allowance readings (the default `claude-code` entry), priced per model;
`metered` — a dollar spend figure against the rate-based projection, using the
per-model rates. An entry with `usage_source: none` is never held. See
[howto/burn-guard.md](howto/burn-guard.md#guarding-several-paid-llm-entries-entries).
