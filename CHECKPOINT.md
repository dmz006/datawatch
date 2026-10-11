# CHECKPOINT — merge Manifest v3 schema (PR #206) into supervisor branch

- Timestamp: 2026-10-11 18:05
- Task: bring BL403 Phase 1 (manifest v3 kind/connection/lifecycle) into automaton/a04015ac as the base for service-plugin supervision.

## Steps completed
1. `git fetch origin`; PR #206 still OPEN (mergedAt null). Merged `origin/automaton/f76713b9` (head fc3a5312) with `--no-ff` → merge commit ab5e80cc. No conflicts (PR head already merged main; Version = 9.0.31 in both api.go and main.go).
2. Schema confirmed in internal/plugins/plugins.go:
   - Kinds: `KindAutomation="automation"`, `KindService="service"`
   - Lifecycle modes: `LifecycleAttachOnly="attach_only"`, `LifecycleManaged="managed"`
   - Restart policies (line ~66): `RestartOnFailure="on-failure"`, `RestartAlways="always"`, `RestartNever="never"`
   - Default restart policy: **none applied in Phase 1** — empty `RestartPolicy` is accepted by Validate and documented as "empty = supervisor default, decided by the supervisor phase" (docs/api/plugins.md:193). Supervisor phase must choose it (suggest `RestartOnFailure`).
   - `StopTimeoutMs` 0 = supervisor default (also undecided).
   - `(*Manifest).Normalize()` (absent kind → automation), `(Manifest).Validate()` present.
3. Verification: `go build -o datawatch ./cmd/datawatch/` OK (needs gitignored stub `internal/docsindex/assets/docs-bm25-index.json` = `{}`, as CI does); `go test ./internal/plugins/... ./internal/secrets/...` 176 passed; check-plugin-manifests.sh with `PLUGIN_MANIFEST_ROOTS=<fixtures>` (script ignores positional args): valid exit 0, invalid exit 1.
4. Quality gates: gofmt clean on all merge-touched files; go vet clean; gosec 11 findings in plugins/secrets, all pre-existing (lines identical in HEAD^1).

## Current step
Done.

## Remaining
Supervisor implementation (next PRD tasks).
