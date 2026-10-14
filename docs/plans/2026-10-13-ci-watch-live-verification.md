# ci-watch completion guardrail — live verification

**Date:** 2026-10-13
**Version:** v9.0.62
**Scope:** docs/howto/ci-watch-guardrail.md, docs/mcp.md, docs/api-mcp-mapping.md, plus the live verification of the `ci-watch` completion guardrail (sections added by other tasks).

## Template fit

**Conclusion: templates cannot enable ci-watch today without a new mechanism, so none was built; the howto documents pairing an instantiated Automaton with a guardrail profile (or an explicit completion list) instead.** The reason: `Template` (internal/autonomous/models.go) carries only `title`/`description`/`spec`/`type`/`tags`/`vars`. `TemplateStore.Instantiate` + `Manager.InstantiateFromTemplateStore` (`autonomous_template_instantiate`) return only a substituted spec and call `CreatePRD(spec, projectDir, backend, "", effort)`, with no profile or guardrail argument. The PRD-as-template path (`Manager.InstantiateTemplate`, `autonomous_prd_instantiate`) copies spec, title, project dir, backend/model/effort and stories, but not `GuardrailProfile` or `PerPRDGuardrails`. There are no built-in release templates in internal/autonomous. The documented recipe ("Using with templates" in the howto) is therefore two calls: instantiate, then `per_automaton_guardrails_set` with `guardrail_profile` (or `per_prd_guardrails_json`) before the Automaton completes. Or list `ci-watch` in the global `autonomous.per_prd_guardrails`.

A caveat found while checking this, which also affects the profile recipe: `resolveGuardrails` (internal/autonomous/guardrail_registry.go) returns a profile's single `Guardrails` list at **every** level (task, story, prd) when the Automaton has no explicit list for that level. A profile containing `ci-watch`/`rules-review` therefore also sends each task and story to the guardrail judge under those names (one extra LLM review per unit; task/story loops in executor.go don't skip judgment-type completion-only names). `rules-review` in a profile already behaves this way. The howto states it and offers the explicit `per_prd_guardrails` list as the completion-only alternative. It is not changed here and is left for an operator decision.
