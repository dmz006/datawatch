# [sync] Actionable Automata Inbox: count only what needs action, blocked-triage reasons, held question text, cards/tiles/compact Inbox lists, Housekeeping question list

Draft only (no issue filed yet).

## Body of the issue

The daemon and the PWA now make the Automata Inbox actionable. The Android
client mirrors the PWA, so it needs the same count rule, card content and
view switch, and the locale keys below (real translations in de, es, fr, ja).

### Daemon fields the client reads

| Where | Field | Notes |
|---|---|---|
| `GET /api/autonomous/triage` records | `blocked_reason` | for a blocked record, the Automaton's own block reason when it set one, else a reason derived from the Automaton's facts; never the task's error |
| same | `reason_source` | `blocked_reason`, `verdict`, `task_error`, `task_review` or `none`; blocked records only, absent on failed ones. `none` means no reason was recorded: show the honest no-reason line and the next step, not an empty card |
| `GET /api/autonomous/held-questions` open entries | `text` | the held question's own text, capped to 500 runes with a trailing ellipsis; absent when the record has none |

Unknown or absent fields stay ignored, so an un-updated client keeps working.
REST, MCP (`autonomous_triage`, `autonomous_held_questions`), the CLI
(`datawatch question held` prints an `ask:` line) and the comm channel
(`question held`) all carry the same fields.

### What the client should do

1. **Badge and count.** Count only what the operator can act on: open questions,
   open held entries (never the shadow would-be list) and Triage records whose
   card has an action button (not the read-only `merge_blocked` card). The
   security failure report and supervisor watch findings still show but never
   count. Badge label: "N waiting on you to act" (and "... across all
   projects").
2. **Blocked Triage card.** Show the whole reason (no clamp), one line naming
   its source (`reason_source`), one concrete next step, and one sentence
   under Retry, Resolve review and Cancel saying what each does.
3. **Held question card.** Show the question text (three lines), the Automaton
   title and id, the default (the forced hold line when the entry has none),
   and open the full question on a tap anywhere that is not a button or link.
4. **Inbox lists.** One List / Tiles / Compact switch covers Triage, open
   questions and held questions, remembered per device like Sessions. Tiles keep
   the same markers as the rows (waiting-for-you badge and note on a decision
   wait question).
5. **Group by dropdown.** On the Automata and Automations lists the selection
   and open state survive a refresh of the list.
6. **Housekeeping.** An Open questions card shows the same live list as the
   Inbox (search box stays in the Inbox only).

### Locale keys (en, with de/es/fr/ja translations in the PWA bundles)

New: `triage_btn_desc_retry`, `triage_btn_desc_resolve_review`,
`triage_btn_desc_cancel`, `triage_unblock_review`, `triage_unblock_error`,
`triage_unblock_verdict`, `triage_unblock_blocked_reason`,
`triage_reason_source_blocked_reason`, `triage_reason_source_verdict`,
`triage_reason_source_task_error`, `triage_reason_source_task_review`,
`held_question_default_value`, `housekeeping_questions_card_title`,
`housekeeping_questions_help_link`.

Reworded: `automation_subtab_inbox_badge_label`,
`automation_subtab_inbox_badge_label_all`, `triage_reason_none`.

See `docs/howto/automation-tab.md` (What the Inbox counts, The view switch, The
Group by dropdown) and `docs/api/autonomous.md` (Triage records).
