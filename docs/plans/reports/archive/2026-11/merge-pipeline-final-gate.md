# Merge pipeline and onboarding report: final gate

Run of `bash .datawatch/verify/mp-final-gate.sh` (under `env -u TMUX -u TMUX_TMPDIR`) on branch `automaton/d1b60001`. Nothing was fixed or edited to make a check pass.

## Script checks

| Check | Result |
|---|---|
| go build ./... | pass |
| go vet (changed packages) | pass |
| gofmt on changed files | pass |
| go test ./... | **fail** (see below) |
| node tests (`internal/server/web/*.test.js`) | pass |
| check-no-internal-refs | pass |
| check-docs-sync | pass |
| check-howto-coverage | pass |
| check-no-unscoped-tmux | pass |
| check-test-daemon-cleanup | pass |
| no version line or versioned CHANGELOG heading changed on the branch | pass |
| parity record present (`docs/plans/parity-issues/merge-pipeline.md`) | pass |

Script result: `VERIFY FAILED` (exit 1), solely from `go test ./...`.

### go test ./... failures

Packages `internal/server` and `internal/session` reported FAIL; every other package passed.

Exact output, `internal/server`:

```
--- FAIL: TestUncommittedPWAHasCardAndPoll (0.01s)
    uncommitted_work_pwa_test.go:74: app.js does not contain "_automationCleanupLoader(), _automationUncommittedWorkLoader()]"
--- FAIL: TestLocales_AutomatonNeverUntranslatedOrMistranslated (0.01s)
    v8_73_33_automata_terminology_test.go:176: locale de key "settings_onboarding_report_enabled_hint" uses the wrong word for Automaton/Automata (automation, a different concept): "Zeigt für die Automatisierung, was eingerichtet ist, was fehlt und was als Nächstes zu tun ist. Er liest nur und ändert nie eine Einstellung."
    v8_73_33_automata_terminology_test.go:176: locale es key "settings_onboarding_report_enabled_hint" uses the wrong word for Automaton/Automata (automation, a different concept): "Muestra para la automatización qué está configurado, qué falta y qué probar a continuación. Solo lee; nunca cambia un ajuste."
    v8_73_33_automata_terminology_test.go:176: locale fr key "settings_onboarding_report_enabled_hint" uses the wrong word for Automaton/Automata (automation, a different concept): "Montre pour l'automatisation ce qui est configuré, ce qui manque et ce qu'il faut essayer ensuite. Il ne fait que lire ; il ne change jamais un paramètre."
FAIL	github.com/dmz006/datawatch/internal/server	67.712s
```

Exact output, `internal/session`:

```
--- FAIL: TestNoNewTmpLeakEntries (72.64s)
    tmp_leak_regression_test.go:107: temp-leak guard: 3 package(s): ./internal/mcp/ ./internal/server/ ./internal/plugins/
    (inner go test ./internal/server/ fails with the same two server failures above; ./internal/mcp/ ok)
FAIL	github.com/dmz006/datawatch/internal/session	165.416s
```

The `internal/session` failure is a knock-on: its leak guard runs `go test ./internal/server/` internally, which fails on the two server tests above. It is not a separate defect.

Causes, from the output only:

1. The German, Spanish and French `settings_onboarding_report_enabled_hint` strings say "automation" (Automatisierung / automatización / automatisation) where the terminology test requires the Automaton word.
2. The loader-list string asserted by `uncommitted_work_pwa_test.go:74` is not found in `app.js`; the PWA loader list no longer has that exact form.

Neither was fixed (the task forbids editing locale files or tests). The operator note says a direct run of the script passed; in this run it did not, so the gate fails here.

## Confirm by reading

- **Default pipeline equals current behaviour: pass.** `internal/mergepipeline/default.go` lists nine steps in the current merge order: check-branch-versions, check-internal-refs (only where the make target exists), merge-branches (`--no-ff`), set-release-version, insert-changelog-heading, commit-release-bump, tag-release, push-release, deploy-latest (continue on failure). Every other step stops on failure.
- **File wins over the record: pass.** `resolver.go` `Resolve` documents and implements file, then record, then built-in default. The default `SourceOrder` is file then record, and a bad file falls back to the record by default.
- **PWA shows the source in effect: pass.** `view.go` returns `source` and `reason`, and prints "Source: the pipeline file in the repository / the pipeline record kept by the daemon / the built-in pipeline". The PWA has the merge pipeline source rows (`app.js` ~line 9132) and `app-merge-pipeline.test.js` passes.
- **Every new setting and card has a help-link entry and a definitions entry: pass.** `app.js` help-link table (~lines 9121 to 9137) covers `onboarding_report.*` and `autonomous.merge_pipeline.{source_order,file_name,fallback_on_bad_file,conflict_policy,conflict_model}`. `docs/datawatch-definitions.md` has sections for Merge pipeline source, Onboarding report, its checks and its next steps. `check-docs-sync` and `check-howto-coverage` pass. I did not check each setting key one by one beyond these.
- **Conflict policy and model are settings with the operator's answers as defaults: pass, with a note.** `internal/config/merge_pipeline.go` defines `conflict_policy` values stop_and_ask / mechanical / mechanical_and_model, and `MergePipelineDefaultConflictModel = "claude-opus-5-5"`. The model is a `*string`, so an unset key reads the default. I did not confirm the default policy value against the operator's answer beyond the code constants.
- **REST, MCP, CLI, comm and PWA surfaces agree: pass, partially checked.** REST `/api/autonomous/merge-pipeline` (+ `/record`, `/validate`), MCP `internal/mcp/merge_pipeline_tools.go`, CLI `datawatch autonomous merge-pipeline show|set-record|clear-record|validate` and `automation onboarding-report`, comm `internal/router/merge_pipeline_comm.go`, and the PWA all exist and their package tests pass. I did not exercise them against a live daemon here; the existing live check is in `docs/plans/reports/archive/2026-11/merge-pipeline-live-check.md`.

## Outcome

Gate **not clean**: 11 of 12 script checks pass; `go test ./...` fails on two `internal/server` tests (plus the `internal/session` leak guard that re-runs them). The confirm-by-reading list shows no defect.
