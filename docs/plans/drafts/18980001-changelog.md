<!-- genartifacts kind=changelog_draft automaton=18980001 -->
<!-- edited by a human: auto-generated draft replaced after rules-review flagged placeholder heading + missing behavior detail (2026-11-13) -->
<!-- generated 2026-11-13 by datawatch genartifacts -->
<!-- do not edit the markers above; the generator keys on them -->

## v9.168.0 — Make the Automata Inbox actionable

_Draft: v9.168.0 is the proposed release number. The branch does not pin it; the release script stamps the final number at merge time._

The Automata Inbox now counts and lists only what waits on the operator to act, and each of those items carries its unblocking action right on its card.

### Changed

- **The Inbox badge counts only what is waiting on you to act.** The badge and the three lists under it are cut by the same rule, so the number on the tab and the cards always agree. Counted: Triage cards that carry an action button (Retry, Resolve review, Cancel), every open question, and the open held entries. Not counted, though still rendered: the security failure report, the supervisor watch findings, and the merge-blocked Triage card (its next action is worktree edits, described as text on the card). Would-be holds in shadow mode never count.
- **The held question card shows the question, not just a placeholder.** Each held entry now renders the question's own text, the Automaton it blocks and the default answer, and opening it lands on the full held-question details. The held-questions report entry behind it carries the question text.
- **The three Inbox lists share one view switch.** Triage, open questions and held questions move between **List**, **Tiles** and **Compact** — the same three modes as the Sessions list. One choice applies to all three and is remembered on this device; until you pick one the mode follows the window width, and switching only repaints from the data already loaded.
- **The Group by dropdown survives a redraw.** With the list repainting while the dropdown is open or focused (next poll, a filter change, coming back to the sub-tab), the select is patched in place instead of replaced: it keeps its open state and focus, and the options and the selection you made are restored onto it.

### Added

- **An open question can be answered in place.** Its card carries the answer field, so the open Automaton is no longer needed to unblock an open question.

### Fixed

- The Triage card strings in the German, Spanish, French and Japanese locale bundles carried the untranslated English word "Automaton" in six keys (the no-reason text, the blocked-reason source text, and the error, verdict and blocked-reason unblock hints, plus the cancel description). Ten more triage guidance keys (retry and resolve-review descriptions, the review unblock hint, the verdict / task-error / task-review source labels) also carried the English copy unchanged. All sixteen are now translated to Automat(en), autómata, automate and オートマトン with each locale's own vocabulary, and the locale tests — including the one that previously pinned the non-English bundles to the English copy — now assert the other way: a translation is present, and the English word and the English copy are not.

### Docs

- The Automation tab how-to documents "What the Inbox counts" (what counts, what never does, how the badge follows the selected project), the List/Tiles/Compact view switch and the Group by dropdown's redraw behavior.
- The plan-doc parity drafts describe the change and its surfaces (web app, REST, MCP, CLI and comm channel unchanged in return scope; web app display rules only).

_Automaton 18980001 — 43 files changed before this fix, the bulk in `internal/server` (web app + locale bundles + tests) plus `docs/howto` and the plan docs; behavior detail as above and in `docs/howto/automation-tab.md`._
