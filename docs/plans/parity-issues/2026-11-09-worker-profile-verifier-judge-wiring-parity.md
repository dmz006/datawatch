# [docs] Worker session profiles: verifier and judge launches now apply their profile, docs corrected across the how-to, glossary, config reference and implementation notes

Date: 2026-11-09
Status: Draft for operator review. Not filed on any tracker. This file is the only output.

## Affected area

Worker session profiles: the named set of MCP servers, tools, skills, starting context and model/effort that a worker, verifier or judge session starts with. A launch resolves a profile by name (task, Automaton, project, daemon-wide, task-kind default, role default) and applies it at session start.

Documentation touched by the docs set for "Make an Automaton's own worker, verifier and judge launches resolve and apply a worker session profile, so the small start":

- `docs/howto/worker-session-profiles.md`
- `docs/datawatch-definitions.md` (glossary entry "Worker session profile")
- `docs/config-reference.yaml` (`autonomous.worker_profile_defaults` and its wiring note)
- `docs/implementation.md` (launch behaviour and wiring status)

## Current behavior

Before the docs set, the four documents said that worker launches apply their resolved profile, but verifier and judge launches did not. They said the resolver `ResolveSessionProfile` had no production caller, so verifier and judge sessions always started with the full harness whatever was configured. The rollout step that moves the `verifier` role to `minimal` was described as having no effect yet. The "Verifier and judge wiring" open item asked the operator to either wire it or keep the note.

After the docs set (commits 19f0e3bd8, 605089dae, 3cf64a376), the four documents say that worker, verifier and judge launches each resolve and apply the profile. They name the call path: `withSessionProfile` in `cmd/datawatch/session_judgment.go` on the verifier and judge paths, which sends the resolved profile with the one-shot session's start request. The `verifier` rollout step is described as effective. The field-level "applied" outcomes for verifier and judge sessions are explicitly marked as not re-checked in the docs pass.

## Expected behavior

- Every verifier and judge session's start record names the profile that the profile-in-force read (`GET /api/config` `autonomous.in_force`, MCP `worker_profile_status`, `datawatch config get`, comm `configure`, web settings group) shows for its role and task kind.
- A verifier or judge launch that cannot apply its profile falls back to the full harness, and the attempt records the fallback and its reason. The session is never silently smaller than the operator expects.
- The four documents agree with each other and with the code on which launches apply a profile, and none of them carries a stale "not yet wired" sentence.
- Clients that show the profile in force for the verifier and judge rows read the same values the launch uses.

## Parity surface

Every surface named in the Mobile-Parity Rule and the Configuration Accessibility Rule is listed below. "Unchanged" means only the existing read path reaches it.

| Surface | Status | Note |
|---|---|---|
| REST | Affected (docs) | `GET /api/config` `autonomous.in_force` and `autonomous.worker_profile_defaults` are the read-back. The verifier and judge rows now describe the real launch. No endpoint or field changed in this docs set. |
| MCP | Affected (docs) | `worker_profile_status` read-back; same wording issue. No tool change. |
| CLI | Affected (docs) | `datawatch config get` / `set` for the profile keys. No command change. |
| comm channel | Affected (docs) | `configure` keys for the profile defaults. No verb change. |
| YAML/config | Affected (docs) | The wiring note under `autonomous.worker_profile_defaults` in `docs/config-reference.yaml` is updated. No key added or removed. |
| PWA | Affected (reads the same data) | The settings group shows the profile in force. Its verifier and judge rows now match the launch. No layout or key change in this docs set. If the PWA quotes the old "not yet wired" note, its wording needs the same correction; not checked from this draft. |
| Android | Unchanged, to confirm | Android reads the same in-force values. If it shows a caption copied from the older docs, it needs the same correction. Not checked from this draft. |
| iPhone/iOS | Unchanged, to confirm | Same as Android. |

Excluded with reason: the docs set added no config key, endpoint, command or wire field, so no client needs a new control. The affected clients only read existing data. The open question is whether any client copied the old caveat text.

## Suggested next step

1. Confirm in code and on a live daemon that a verifier launch and a judge launch each record the profile in force for their role in the start record. Use a throwaway daemon with a test profile on the `verifier` role, then check the start record and the attempt record. Do not end any operator daemon or session to do this.
2. Check that the field-level "applied" outcome for verifier and judge sessions matches the docs text. The docs pass left this open. If it does not match, fix the code or the wording, not both at once.
3. Search the PWA locale bundles and the Android client copy for any caveat saying verifier or judge profiles are not applied yet, and file a parity note for any hit. Per the Localization Rule, a new string needs a datawatch-app issue, and a changed caption does too.
4. Only after step 2 passes, follow the rollout step in the how-to that moves the `verifier` role to `minimal`, one step at a time, watching the failed-attempt rate the cost report shows.
5. Leave the "Verifier and judge wiring" item in the implementation notes open until steps 1 and 2 are confirmed, then close it in the same change that adjusts the wording.

## Notes for the filer

- These are documentation changes only. Under the project rules a docs-only change takes no version bump, but the release that ships the wiring should record it in the changelog.
- Operator-facing text must not carry internal tracker IDs. The four documents are clean on this point at the time of the docs set.
- This draft creates no issue, PR or tracker entry, and it runs no hosting CLI.
