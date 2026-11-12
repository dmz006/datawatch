# Interstitial prompt fixtures

Each `.txt` file here is a pane capture (or reconstruction of one) of a
mid-session Claude Code settings prompt, or of a pane that must NOT match
(idle prompt, quoted prompt text, truncated capture). The directory feeds
`internal/session/interstitial_prompt_test.go`; the built-in table lives in
`internal/session/interstitial_prompt.go`.

| File                              | Classifies as              | Enter on the cursor row would change a setting |
|-----------------------------------|----------------------------|------------------------------------------------|
| `autocompact-enter-would-pick.txt`      | `autocompact-threshold`    | **yes** — cursor is on "Autocompact at 400k"   |
| `autocompact-keep-auto-selected.txt`    | `autocompact-threshold`    | no — cursor is on "Keep auto (1m tokens)"      |
| `autocompact-enter-would-pick-ansi.txt` | `autocompact-threshold`    | **yes** — identical capture, raw ANSI escapes  |
| `partial-autocompact-no-footer.txt`     | `none`                     | n/a — hint line missing                        |
| `idle-prompt.txt`                       | `none`                     | n/a                                            |
| `quoted-autocompact-in-output.txt`      | `none`                     | n/a — wording appears only as prose above the window |

## Provenance

- `autocompact-enter-would-pick.txt` and
  `autocompact-keep-auto-selected.txt` were **reconstructed from the
  operator's recorded wording** of the stall incident on
  2026-10-31, documented in `docs/plans/README.md`: the question
  "Want to autocompact at 400k tokens instead?", the two option rows
  "Autocompact at 400k" and "Keep auto (1m tokens)", and the hint line's
  key names — up/down to choose, enter to confirm, **esc "not now"**, the
  key that cleared the prompt without changing the setting. Frame
  characters, `↑ ↓` arrows and the `·` separators are included so the
  fixture carries the tolerant-separator shape the matcher must survive.
  The exact pixel layout (arrow style, dot separators) was not captured
  verbatim; the words recorded in the incident are what the matcher keys
  on.
- `autocompact-enter-would-pick-ansi.txt` is the same content with raw
  ANSI SGR escapes wrapped around every line, exercising `StripANSI`.
- `partial-autocompact-no-footer.txt` is the prompt truncated before its
  hint line; it exists to pin the "footer missing → none" rule, because a
  real capture could cut anywhere when a pane is redrawn.
- `idle-prompt.txt` is a normal idle input line after a finished turn; it
  must be disjoint from the interstitial classifier.
- `quoted-autocompact-in-output.txt` is a worker's own recap that quotes
  the prompt's wording verbatim in prose, followed by more than 20 lines —
  the window rule must keep this from matching.

## Candidates checked and NOT added (no verbatim evidence)

The task brief asked that other known Claude Code interstitial prompts be
added **only when their verbatim wording is evidenced**. We checked:

- **Session feedback survey** ("How is Claude doing this session?
  (optional)"). The official Claude Code release notes name the survey
  (e.g. "Fixed the session feedback survey showing no hover highlight on
  light and ANSI themes") but neither the release notes nor anything in
  this repository records the survey's prompt wording, its option rows or
  its dismiss hint line verbatim. **No entry, no fixture.** When a live
  capture lands: add the fixture (name the capture as its source) and the
  table entry.
- **Update-available notices.** No verbatim mid-session capture exists in
  the repository; the startup-phase update dialog is already owned by the
  startup classifier (`testdata/startup/update-prompt.txt`), which this
  classifier must stay disjoint from. **Not an interstitial here; no
  entry.**
- **Other "not now" settings nudges.** No in-tree capture and no official
  release-note wording found for any other nudge shape. **No entry.**

## Recording live captures

**Replace the reconstructions with live recordings whenever one exists.**
A `tmux capture-pane` of the real prompt, saved raw (keep the ANSI
escapes in the file) or stripped, is strictly better than a
reconstruction: re-run the reconstruction against it, confirm the table
still classifies it, and record the capture date in this README.
