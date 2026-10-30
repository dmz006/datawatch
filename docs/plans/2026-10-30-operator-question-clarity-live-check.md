# Operator question clarity — live check

Date: 2026-10-30

Sandbox note: every check below ran against a throwaway sandbox daemon built from this
branch, with its own config and data directory under `.datawatch/qc-live` (removed at the
end). It listened on port 47911 on loopback only, with `autonomous.questions.enabled` on and
`quality_mode` set to `warn` at start. No session was started, so no repository auto-commit
could occur. Nothing else was touched.

port 47911

Sandbox daemon PID 1868740 (stopped by that exact PID). The web dialog check used a headless
Chrome that was already installed; its PID was 1900337 on debugging port 47913 (stopped by
that exact PID).

## Checks

1. Options with no tradeoff text, mode `warn` — PASS. HTTP 201, question stored, and the
   response carried one warning, code `no_tradeoffs`: "These options have no trade-offs
   written: keep, split. Say what the operator gains and gives up with each choice."
2. Options raised with tradeoff text — PASS. Raised once through the REST `tradeoffs` map and
   once through the command line `question raise --tradeoff id=text`. Both returned
   no warning, and a read of `GET /api/questions/{id}` returned the stored tradeoff text on
   each option ("Nothing to change, but the folder keeps growing." and "Easier browsing, but
   old links to the shared file break.").
3. Short text and a tracker-style id — PASS. A 16-character question returned HTTP 201 with
   code `text_too_short`: "The question text is 16 characters; at least 60 are needed to
   explain what is being asked and why. Add the context the operator needs to decide." A
   question containing a made-up letters-and-digits id returned HTTP 201 with code
   `tracker_id`: "The question contains \"<the id>\", which looks like an internal tracker
   identifier. The operator reads these questions and may not know it; describe the item in
   plain words instead."
4. Switch to `refuse` through `PUT /api/config` — PASS. The write returned HTTP 200 and
   `GET /api/questions/quality` then showed mode `refuse`. A question with no tradeoffs and a
   question with short text each returned HTTP 422 ("question refused: it does not meet the
   question quality policy; fix the findings and send it again") with the findings listed.
   The question count read 9 before and 9 after, so nothing was stored. A well-formed question
   still returned HTTP 201 in the same mode.
5. Mode `off` — PASS. After `PUT /api/config` set the mode to `off`, a question with no
   tradeoffs and short text returned HTTP 201 with neither a `warnings` nor a `findings`
   key, so it raised silently.
6. Proposal path context reference — PASS. A question with `context` `proposal` and
   `proposal_path` `docs/plans/README.md` returned, on `GET /api/questions/{id}` and in the
   `GET /api/questions` row, `context_ref` `{"kind":"proposal","title":"README.md","doc_path":"docs/plans/README.md","viewer_url":"/diagrams.html#docs/plans/README.md"}`.
   The viewer page `/diagrams.html` answered HTTP 200 and the document `/docs/plans/README.md`
   answered HTTP 200 (203275 bytes). A path with no such document got an empty `viewer_url`.
   The context reference is computed on reads; the raise response carries the stored record
   only.
7. Same quality values on every surface — PASS. With non-default values set (mode `refuse`,
   minimum text length 75, queue preview 4 lines), `GET /api/questions/quality`, the command
   line `question quality` (text and `--json`), the chat read `question quality` (through
   `POST /api/test/message`) and the MCP tool `question_quality` (through `datawatch mcp`
   over stdio) all returned mode `refuse`, `min_text_chars` 75, the same
   `tracker_id_pattern`, and `queue_preview_lines` 4.
8. Answer and verbatim quote — PASS. `POST /api/questions/{id}/answer` returned HTTP 201 for a
   quote with leading and trailing spaces, a newline, a tab, a non-breaking space, curly
   quotes and accented characters. The quote read back from `GET /api/questions/{id}` and
   from the answers log was byte-for-byte equal to what was sent.
9. Web dialog (extra) — PASS. Headless Chrome was available, so the real answer dialog was
   opened with `window.openQuestion` on the proposal question. It showed the id line, the
   question text, both tradeoff lines under their options, the context line "This question
   is about: README.md" with its Open control, the label "Your answer in your own words",
   the helper line "Optional when you pick an option. Whatever you type is saved exactly as
   written, with your choice, as the record of your decision, and the asker reads it as your
   answer." and the default line "If nobody answers, the question stays on hold."

## Cleanup

- Daemon stopped by its exact PID (1868740); the process exited and port 47911 is closed.
- Headless Chrome stopped by its exact PID (1900337); no process with its profile remains and
  port 47913 is closed.
- 12 of the 13 questions created through the API were withdrawn (HTTP 200 each; four of the 13
  came from a first pass of checks 1 to 3 whose output was not recorded, so it was repeated).
  The 13th, the question answered in
  check 8, returned HTTP 409 on withdraw, because a decided question is kept by design and the
  API has no delete route. It lived only in the sandbox data directory, which was removed with
  the rest of `.datawatch/qc-live`. Zero questions were left open.
- `.datawatch/qc-live` removed whole (binary, config, data, logs, Chrome profile); `git status`
  shows no stray files.
- No process was ended by name, no tmux command was run, and the version variables were not
  touched.

## Overall result

PASS — all eight checks and the web dialog check passed; nothing failed and nothing was fixed
in this task.
