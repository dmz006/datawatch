# Operator question clarity — live check

Date: 2026-10-30

Sandbox note: every check below ran against a throwaway sandbox daemon built from this
branch, with its own config and data directory under `.datawatch/qc-live` (removed at the
end). It listened on loopback only, with `autonomous.questions.enabled` on and `quality_mode`
set to `warn` at start. No session was started, so no repository auto-commit could occur.
Nothing else was touched.

port 47921

Sandbox daemon PID 2025857, started with `env -u TMUX -u TMUX_TMPDIR` and stopped by that
exact PID. The web dialog check used the headless Chrome that was already installed; its PID
was 2051981 on debugging port 47923, stopped by that exact PID. This is the second run of
this check: the first run's output was not kept, so everything below was repeated and every
request and response is recorded in the evidence log at the end of this file.

## Checks

1. Options with no tradeoff text, mode warn: HTTP 201, question stored, one warning `no_tradeoffs`: "These options have no trade-offs written: keep, split. Say what the operator gains and gives up with each choice." — PASS
2. Options raised with tradeoff text (REST `tradeoffs` map and CLI `question raise --tradeoff id=text`): both HTTP 201 / raised with no warning, and `GET /api/questions/{id}` returned the stored text on each option ("Nothing to change, but the folder keeps growing." and "Easier browsing, but old links to the shared file break.") — PASS
3. Short text and tracker-style id, mode warn: a 17-character question returned HTTP 201 with `text_too_short`: "The question text is 17 characters; at least 60 are needed to explain what is being asked and why. Add the context the operator needs to decide."; a question holding a made-up letters-and-digits id returned HTTP 201 with `tracker_id`: "The question contains \"<the id>\", which looks like an internal tracker identifier. The operator reads these questions and may not know it; describe the item in plain words instead." — PASS
4. Mode switched to refuse through `PUT /api/config` (HTTP 200, `GET /api/questions/quality` shows `refuse`): a question with no tradeoffs and a question with short text each returned HTTP 422 "question refused: it does not meet the question quality policy; fix the findings and send it again" with the findings listed; the stored question count was 5 before and 5 after, so nothing was stored; a well-formed question still returned HTTP 201 — PASS
5. Mode off through `PUT /api/config`: a question with no tradeoffs and short text returned HTTP 201 with no `warnings` and no `findings` key, so it raised silently — PASS
6. Question with `context` proposal and `proposal_path` `docs/plans/README.md`: `GET /api/questions/{id}` and the `GET /api/questions` row returned `context_ref` `{"kind":"proposal","title":"README.md","doc_path":"docs/plans/README.md","viewer_url":"/diagrams.html#docs/plans/README.md"}`; `/diagrams.html` answered HTTP 200 (11942 bytes) and `/docs/plans/README.md` answered HTTP 200 (203275 bytes) — PASS
7. Same quality values on every surface (non-default values set: mode refuse, minimum text length 75, queue preview 4 lines): `GET /api/questions/quality`, CLI `question quality` (text and `--json`), the chat read `question quality` (`POST /api/test/message`) and the MCP tool `question_quality` (`datawatch mcp` over stdio) all returned mode `refuse`, `min_text_chars` 75, the same `tracker_id_pattern` and `queue_preview_lines` 4 — PASS
8. Answer and verbatim quote: `POST /api/questions/{id}/answer` returned HTTP 201 for an 89-byte quote with leading and trailing spaces, a newline, a tab, a non-breaking space, curly quotes and an accented letter; the quote read back from `GET /api/questions/{id}` and from the answers log was byte-for-byte identical to the bytes sent (`cmp` clean on both) — PASS
9. Web dialog (extra): headless Chrome was already installed, so the real answer dialog was opened with `window.openQuestion` on the proposal question and showed the question text, both tradeoff lines under their options, the context line "This question is about: README.md" with its Open control, the default line, the label "Your answer in your own words" and the helper line "Optional when you pick an option. Whatever you type is saved exactly as written, with your choice, as the record of your decision, and the asker reads it as your answer." (dialog text is in the evidence log) — PASS

## Cleanup

- Daemon stopped by its exact PID (2025857): it exited within 1 second and port 47921 is closed.
- Headless Chrome stopped by its exact PID (2051981): it exited and port 47923 is closed; no process mentioning `qc-live` remained. No process was ended by name.
- Questions created through the API: 8 stored (the two refused raises in check 4 stored nothing). 7 were withdrawn (HTTP 200 each) and `GET /api/questions?state=open` then returned 0. **Deviation from the spec:** the 8th, the question answered in check 8, could not be removed through the API. `POST /api/questions/{id}/withdraw` returned HTTP 409 ("question is not open") because a decided question is kept by design, and the API has no delete route. It existed only in the sandbox data directory, which was removed with the rest of `.datawatch/qc-live`. So "delete every question through the API" was met for 7 of 8; the last one was removed with its data directory.
- `.datawatch/qc-live` removed whole (binary, config, data, logs, Chrome profile); `git status` shows no stray files.
- No tmux command was run and the version variables were not touched.

## Overall result

Checks: PASS — all eight checks and the web dialog check passed; nothing failed and nothing was fixed in this task.
Cleanup: PARTIAL — everything was stopped, removed and confirmed closed, but one answered question could not be deleted through the API (no delete route exists), so it was removed with the sandbox data directory instead. This looks like a gap in the spec or the API rather than in the feature under test.

## Evidence log

Recorded requests and responses from the sandbox, in order (`<the id>` stands in for the made-up
tracker-style id used in check 3). Section headings name the check each block belongs to.

```text

## check 1: options without tradeoffs, mode warn
[17:05:30] POST /api/questions {"label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder"},{"id":"split","label":"Split per team"}],"default":"hold"}
  -> HTTP 201 {"question":{"id":"q-18e36aa4369907e576a9","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder"},{"id":"split","label":"Split per team"}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:05:30.146768869-04:00","state":"open","return_path":false,"source":"rest"},"warnings":[{"code":"no_tradeoffs","message":"These options have no trade-offs written: keep, split. Say what the operator gains and gives up with each choice."}]}

## check 2: tradeoffs via REST map
[17:05:35] POST /api/questions {"label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder"},{"id":"split","label":"Split per team"}],"tradeoffs":{"keep":"Nothing to change, but the folder keeps growing.","split":"Easier browsing, but old links to the shared file break."},"default":"hold"}
  -> HTTP 201 {"question":{"id":"q-18e36aa58311a34431e9","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder","tradeoffs":"Nothing to change, but the folder keeps growing."},{"id":"split","label":"Split per team","tradeoffs":"Easier browsing, but old links to the shared file break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:05:35.724708676-04:00","state":"open","return_path":false,"source":"rest"}}
[17:05:35] GET /api/questions/q-18e36aa58311a34431e9 
  -> HTTP 200 {"answer":null,"answer_count":0,"current_answer_id":"","question":{"id":"q-18e36aa58311a34431e9","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder","tradeoffs":"Nothing to change, but the folder keeps growing."},{"id":"split","label":"Split per team","tradeoffs":"Easier browsing, but old links to the shared file break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:05:35.724708676-04:00","state":"open","return_path":false,"source":"rest","context_ref":{"kind":"none","title":"","id":"","doc_path":"","viewer_url":""}}}

## check 2: tradeoffs via CLI --tradeoff
question raised: q-18e36aa58565419569a1
  label: decision
  options: 2
  default: hold
  state: open
[17:05:42] GET /api/questions/q-18e36aa58565419569a1 
  -> HTTP 200 {"answer":null,"answer_count":0,"current_answer_id":"","question":{"id":"q-18e36aa58565419569a1","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder","tradeoffs":"Nothing to change, but the folder keeps growing."},{"id":"split","label":"Split per team","tradeoffs":"Easier browsing, but old links to the shared file break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:05:35.763743125-04:00","state":"open","return_path":false,"source":"rest","context_ref":{"kind":"none","title":"","id":"","doc_path":"","viewer_url":""}}}

## check 3a: short text
[17:05:42] POST /api/questions {"label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Split the folder?","options":[{"id":"keep","label":"Keep","tradeoffs":"Nothing changes."},{"id":"split","label":"Split","tradeoffs":"Links break."}],"default":"hold"}
  -> HTTP 201 {"question":{"id":"q-18e36aa6fd25c8721cc5","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Split the folder?","options":[{"id":"keep","label":"Keep","tradeoffs":"Nothing changes."},{"id":"split","label":"Split","tradeoffs":"Links break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:05:42.067816562-04:00","state":"open","return_path":false,"source":"rest"},"warnings":[{"code":"text_too_short","message":"The question text is 17 characters; at least 60 are needed to explain what is being asked and why. Add the context the operator needs to decide."}]}

## check 3b: tracker-style id
[17:05:42] POST /api/questions {"label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should we keep the shared notes folder as it is while <the id> is open, or split it into one folder per team now?","options":[{"id":"keep","label":"Keep","tradeoffs":"Nothing changes."},{"id":"split","label":"Split","tradeoffs":"Links break."}],"default":"hold"}
  -> HTTP 201 {"question":{"id":"q-18e36aa6fe9ca21a4794","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should we keep the shared notes folder as it is while <the id> is open, or split it into one folder per team now?","options":[{"id":"keep","label":"Keep","tradeoffs":"Nothing changes."},{"id":"split","label":"Split","tradeoffs":"Links break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:05:42.092382746-04:00","state":"open","return_path":false,"source":"rest"},"warnings":[{"code":"tracker_id","message":"The question contains \"<the id>\", which looks like an internal tracker identifier. The operator reads these questions and may not know it; describe the item in plain words instead."}]}

## check 4: refuse mode
before=5
[17:05:50] PUT /api/config {"autonomous.questions.quality_mode":"refuse"}
  -> HTTP 200 {"status":"ok"}
[17:05:50] GET /api/questions/quality 
  -> HTTP 200 {"mode":"refuse","min_text_chars":60,"tracker_id_pattern":"\\b[A-Z]{1,3}[0-9]{2,4}\\b","queue_preview_lines":3}
[17:05:50] POST /api/questions {"label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder"},{"id":"split","label":"Split per team"}],"default":"hold"}
  -> HTTP 422 {"error":"question refused: it does not meet the question quality policy; fix the findings and send it again","findings":[{"code":"no_tradeoffs","message":"These options have no trade-offs written: keep, split. Say what the operator gains and gives up with each choice."}]}
[17:05:50] POST /api/questions {"label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Split the folder?","options":[{"id":"keep","label":"Keep","tradeoffs":"Nothing changes."},{"id":"split","label":"Split","tradeoffs":"Links break."}],"default":"hold"}
  -> HTTP 422 {"error":"question refused: it does not meet the question quality policy; fix the findings and send it again","findings":[{"code":"text_too_short","message":"The question text is 17 characters; at least 60 are needed to explain what is being asked and why. Add the context the operator needs to decide."}]}
after=5

## check 4 control: well-formed question in refuse mode
[17:05:50] POST /api/questions {"label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder","tradeoffs":"Nothing to change, but the folder keeps growing."},{"id":"split","label":"Split per team","tradeoffs":"Easier browsing, but old links to the shared file break."}],"default":"hold"}
  -> HTTP 201 {"question":{"id":"q-18e36aa8fdcab8461bc1","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder","tradeoffs":"Nothing to change, but the folder keeps growing."},{"id":"split","label":"Split per team","tradeoffs":"Easier browsing, but old links to the shared file break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:05:50.668560454-04:00","state":"open","return_path":false,"source":"rest"}}
final=6

## check 5: mode off
[17:06:02] PUT /api/config {"autonomous.questions.quality_mode":"off"}
  -> HTTP 200 {"status":"ok"}
[17:06:02] GET /api/questions/quality 
  -> HTTP 200 {"mode":"off","min_text_chars":60,"tracker_id_pattern":"\\b[A-Z]{1,3}[0-9]{2,4}\\b","queue_preview_lines":3}
[17:06:02] POST /api/questions {"label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Split the folder?","options":[{"id":"keep","label":"Keep"},{"id":"split","label":"Split"}],"default":"hold"}
  -> HTTP 201 {"question":{"id":"q-18e36aabb01a781ccec8","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Split the folder?","options":[{"id":"keep","label":"Keep"},{"id":"split","label":"Split"}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:06:02.250065948-04:00","state":"open","return_path":false,"source":"rest"}}
[17:06:02] PUT /api/config {"autonomous.questions.quality_mode":"warn"}
  -> HTTP 200 {"status":"ok"}

## check 6: proposal_path context_ref
[17:06:02] POST /api/questions {"label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"proposal","proposal_path":"docs/plans/README.md","text":"Should the plans index list finished plans separately from the ones still open? Separating them shortens the main list.","options":[{"id":"keep","label":"Keep one list","tradeoffs":"Nothing to change, but the list keeps growing."},{"id":"split","label":"Separate finished plans","tradeoffs":"A shorter main list, but readers need a second click for history."}],"default":"hold"}
  -> HTTP 201 {"question":{"id":"q-18e36aabb208bf7cd81c","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"proposal","proposal_path":"docs/plans/README.md","text":"Should the plans index list finished plans separately from the ones still open? Separating them shortens the main list.","options":[{"id":"keep","label":"Keep one list","tradeoffs":"Nothing to change, but the list keeps growing."},{"id":"split","label":"Separate finished plans","tradeoffs":"A shorter main list, but readers need a second click for history."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:06:02.282459004-04:00","state":"open","return_path":false,"source":"rest"}}
[17:06:02] GET /api/questions/q-18e36aabb208bf7cd81c 
  -> HTTP 200 {"answer":null,"answer_count":0,"current_answer_id":"","question":{"id":"q-18e36aabb208bf7cd81c","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"proposal","proposal_path":"docs/plans/README.md","text":"Should the plans index list finished plans separately from the ones still open? Separating them shortens the main list.","options":[{"id":"keep","label":"Keep one list","tradeoffs":"Nothing to change, but the list keeps growing."},{"id":"split","label":"Separate finished plans","tradeoffs":"A shorter main list, but readers need a second click for history."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:06:02.282459004-04:00","state":"open","return_path":false,"source":"rest","context_ref":{"kind":"proposal","title":"README.md","id":"","doc_path":"docs/plans/README.md","viewer_url":"/diagrams.html#docs/plans/README.md"}}}
{"kind":"proposal","title":"README.md","id":"","doc_path":"docs/plans/README.md","viewer_url":"/diagrams.html#docs/plans/README.md"}
/diagrams.html -> HTTP 200 11942 bytes
/docs/plans/README.md -> HTTP 200 203275 bytes

## check 7: same values on every surface
[17:06:10] PUT /api/config {"autonomous.questions.quality_mode":"refuse","autonomous.questions.quality_min_text_chars":75,"autonomous.questions.queue_preview_lines":4}
  -> HTTP 200 {"status":"ok"}
[17:06:10] GET /api/questions/quality 
  -> HTTP 200 {"mode":"refuse","min_text_chars":75,"tracker_id_pattern":"\\b[A-Z]{1,3}[0-9]{2,4}\\b","queue_preview_lines":4}
question quality policy:
  mode: refuse (a weak question is refused)
  minimum text length: 75 characters
  internal id pattern: \b[A-Z]{1,3}[0-9]{2,4}\b
  queue preview: 4 lines
{
  "min_text_chars": 75,
  "mode": "refuse",
  "queue_preview_lines": 4,
  "tracker_id_pattern": "\\b[A-Z]{1,3}[0-9]{2,4}\\b"
}
[17:06:11] POST /api/test/message {"text":"question quality"}
  -> HTTP 200 {"count":1,"input":"question quality","responses":["[qc-live-sandbox] question quality\nquestion quality policy:\nmode: refuse\nminimum text length: 75\ntracker-id pattern: \\b[A-Z]{1,3}[0-9]{2,4}\\b\nqueue preview lines: 4"]}
{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{"prompts":{},"resources":{},"sampling":{},"tools":{"listChanged":true}},"serverInfo":{"name":"datawatch","version":"1.0.0"}}}
{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"{\"mode\":\"refuse\",\"min_text_chars\":75,\"tracker_id_pattern\":\"\\\\b[A-Z]{1,3}[0-9]{2,4}\\\\b\",\"queue_preview_lines\":4}\n"}]}}

## check 8: answer with verbatim quote
[17:07:37] POST /api/questions/q-18e36aa58311a34431e9/answer {
  "chosen_option_id": "keep",
  "quote": "  Keep it for now.\n\tNon-breaking space, “curly quotes” and café, trailing spaces   ",
  "surface": "rest",
  "actor": "qc-live"
}
  -> HTTP 201 {"answer":{"id":"a-18e36ac1c18a1033d994","question_id":"q-18e36aa58311a34431e9","actor":"qc-live","surface":"rest","chosen_option_id":"keep","quote":"  Keep it for now.\n\tNon-breaking space, “curly quotes” and café, trailing spaces   ","quote_sha256":"c8729171701b056d57ab4b38db15bbafaf6c098871c5910b1105ca757469476f","created_at":"2026-10-30T17:07:37.031872563-04:00"},"question":{"id":"q-18e36aa58311a34431e9","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder","tradeoffs":"Nothing to change, but the folder keeps growing."},{"id":"split","label":"Split per team","tradeoffs":"Easier browsing, but old links to the shared file break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:05:35.724708676-04:00","state":"answered","return_path":false,"source":"rest"}}
GET answer quote identical (89 bytes)
answers log quote identical
[17:07:37] GET /api/questions/q-18e36aa58311a34431e9 
  -> HTTP 200 {"answer":{"id":"a-18e36ac1c18a1033d994","chosen_option_id":"keep","quote":"  Keep it for now.\n\tNon-breaking space, “curly quotes” and café, trailing spaces   ","actor":"qc-live","surface":"rest","created_at":"2026-10-30T17:07:37.031872563-04:00"},"answer_count":1,"current_answer_id":"a-18e36ac1c18a1033d994","question":{"id":"q-18e36aa58311a34431e9","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder","tradeoffs":"Nothing to change, but the folder keeps growing."},{"id":"split","label":"Split per team","tradeoffs":"Easier browsing, but old links to the shared file break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:05:35.724708676-04:00","state":"answered","return_path":false,"source":"rest","context_ref":{"kind":"none","title":"","id":"","doc_path":"","viewer_url":""}}}
[17:07:37] POST /api/questions/q-18e36aa58311a34431e9/withdraw {"reason":"qc-live cleanup"}
  -> HTTP 409 {"error":"question q-18e36aa58311a34431e9: question is not open"}

## cleanup: withdraw every open question created
[17:07:45] POST /api/questions/q-18e36aa4369907e576a9/withdraw {"reason":"qc-live cleanup"}
  -> HTTP 200 {"question":{"id":"q-18e36aa4369907e576a9","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder"},{"id":"split","label":"Split per team"}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:05:30.146768869-04:00","state":"withdrawn","return_path":false,"source":"rest"}}
[17:07:45] POST /api/questions/q-18e36aa58311a34431e9/withdraw {"reason":"qc-live cleanup"}
  -> HTTP 409 {"error":"question q-18e36aa58311a34431e9: question is not open"}
[17:07:45] POST /api/questions/q-18e36aa58565419569a1/withdraw {"reason":"qc-live cleanup"}
  -> HTTP 200 {"question":{"id":"q-18e36aa58565419569a1","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder","tradeoffs":"Nothing to change, but the folder keeps growing."},{"id":"split","label":"Split per team","tradeoffs":"Easier browsing, but old links to the shared file break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:05:35.763743125-04:00","state":"withdrawn","return_path":false,"source":"rest"}}
[17:07:45] POST /api/questions/q-18e36aa6fd25c8721cc5/withdraw {"reason":"qc-live cleanup"}
  -> HTTP 200 {"question":{"id":"q-18e36aa6fd25c8721cc5","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Split the folder?","options":[{"id":"keep","label":"Keep","tradeoffs":"Nothing changes."},{"id":"split","label":"Split","tradeoffs":"Links break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:05:42.067816562-04:00","state":"withdrawn","return_path":false,"source":"rest"}}
[17:07:45] POST /api/questions/q-18e36aa6fe9ca21a4794/withdraw {"reason":"qc-live cleanup"}
  -> HTTP 200 {"question":{"id":"q-18e36aa6fe9ca21a4794","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should we keep the shared notes folder as it is while <the id> is open, or split it into one folder per team now?","options":[{"id":"keep","label":"Keep","tradeoffs":"Nothing changes."},{"id":"split","label":"Split","tradeoffs":"Links break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:05:42.092382746-04:00","state":"withdrawn","return_path":false,"source":"rest"}}
[17:07:45] POST /api/questions/q-18e36aa8fdcab8461bc1/withdraw {"reason":"qc-live cleanup"}
  -> HTTP 200 {"question":{"id":"q-18e36aa8fdcab8461bc1","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder","tradeoffs":"Nothing to change, but the folder keeps growing."},{"id":"split","label":"Split per team","tradeoffs":"Easier browsing, but old links to the shared file break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:05:50.668560454-04:00","state":"withdrawn","return_path":false,"source":"rest"}}
[17:07:45] POST /api/questions/q-18e36aabb01a781ccec8/withdraw {"reason":"qc-live cleanup"}
  -> HTTP 200 {"question":{"id":"q-18e36aabb01a781ccec8","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Split the folder?","options":[{"id":"keep","label":"Keep"},{"id":"split","label":"Split"}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:06:02.250065948-04:00","state":"withdrawn","return_path":false,"source":"rest"}}
[17:07:45] POST /api/questions/q-18e36aabb208bf7cd81c/withdraw {"reason":"qc-live cleanup"}
  -> HTTP 200 {"question":{"id":"q-18e36aabb208bf7cd81c","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"proposal","proposal_path":"docs/plans/README.md","text":"Should the plans index list finished plans separately from the ones still open? Separating them shortens the main list.","options":[{"id":"keep","label":"Keep one list","tradeoffs":"Nothing to change, but the list keeps growing."},{"id":"split","label":"Separate finished plans","tradeoffs":"A shorter main list, but readers need a second click for history."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:06:02.282459004-04:00","state":"withdrawn","return_path":false,"source":"rest"}}
```

Web dialog text captured from headless Chrome for check 9:

```text
Answer question decision
Question q-18e36aabb208bf7cd81c
Should the plans index list finished plans separately from the ones still open? Separating them shortens the main list.
This question is about: README.mdOpen
 Keep one list
Nothing to change, but the list keeps growing.
 Separate finished plans
A shorter main list, but readers need a second click for history.
Default: hold
If nobody answers, the question stays on hold.
Your answer in your own words
Optional when you pick an option. Whatever you type is saved exactly as written, with your choice, as the record of your decision, and the asker reads it as your answer.
Cancel
Answer
```
