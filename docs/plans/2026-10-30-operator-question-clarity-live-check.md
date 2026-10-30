# Operator question clarity — live check

Date: 2026-10-30

Sandbox note: every check below ran against a throwaway sandbox daemon built from this
branch, with its own config and data directory under `.datawatch/qc-live` (removed at the
end). It listened on loopback only, with `autonomous.questions.enabled` on and `quality_mode`
set to `warn` at start. No session was started, so no repository auto-commit could occur.
Nothing else was touched.

port 47931

Sandbox daemon PID 2144102, started with `env -u TMUX -u TMUX_TMPDIR` and stopped by that
exact PID. The web dialog check used the headless Chrome that was already installed; its PID
was 2170343 on debugging port 47933, stopped by that exact PID. This is the third run of
this check. Earlier runs left gaps in the evidence, so this run was repeated from scratch and
every request and response is recorded verbatim, in order, in the evidence log at the end of
this file (nothing is shortened or re-typed; the only made-up value is the tracker-style id
`ZQ8841` in check 3, which is not a real identifier in any registry).

## Checks

1. Options with no tradeoff text, mode warn: HTTP 201, question stored, one warning `no_tradeoffs`: "These options have no trade-offs written: keep, split. Say what the operator gains and gives up with each choice." — PASS
2. Options raised with tradeoff text (REST `tradeoffs` map and CLI `question raise --tradeoff id=text`): both raised (HTTP 201 and "question raised") with no warning, and `GET /api/questions/{id}` returned the stored text on each option ("Nothing to change, but the folder keeps growing." and "Easier browsing, but old links to the shared file break.") — PASS
3. Short text and tracker-style id, mode warn: a 17-character question returned HTTP 201 with `text_too_short`: "The question text is 17 characters; at least 60 are needed to explain what is being asked and why. Add the context the operator needs to decide."; a question holding the made-up id `ZQ8841` returned HTTP 201 with `tracker_id`: "The question contains \"ZQ8841\", which looks like an internal tracker identifier. The operator reads these questions and may not know it; describe the item in plain words instead." — PASS
4. Mode switched to refuse through `PUT /api/config` (HTTP 200, `GET /api/questions/quality` shows `refuse`): a question with no tradeoffs and a question with short text each returned HTTP 422 "question refused: it does not meet the question quality policy; fix the findings and send it again" with the findings listed; `GET /api/questions` returned `"count":5` before and `"count":5` after, so nothing was stored; a well-formed question still returned HTTP 201 — PASS
5. Mode off through `PUT /api/config` (HTTP 200, quality shows `off`): a question with short text and no tradeoffs returned HTTP 201 with no `warnings` and no `findings` key, so it raised silently — PASS
6. Question with `context` `proposal` and `proposal_path` `docs/plans/README.md`: both `GET /api/questions/{id}` and the `GET /api/questions?state=open` list row returned the `context_ref` `{"kind":"proposal","title":"README.md","id":"","doc_path":"docs/plans/README.md","viewer_url":"/diagrams.html#docs/plans/README.md"}` (it appears twice in the log, once in each response); `/diagrams.html` answered HTTP 200 (11942 bytes) and `/docs/plans/README.md` answered HTTP 200 (203275 bytes), so the link resolves — PASS
7. Same quality values on every surface (non-default values set: mode refuse, minimum text length 75, queue preview 4 lines): `GET /api/questions/quality`, CLI `question quality` (text and `--json`), the chat read `question quality` (`POST /api/test/message`) and the MCP tool `question_quality` (`datawatch mcp` over stdio) all returned mode `refuse`, `min_text_chars` 75, the same `tracker_id_pattern` and `queue_preview_lines` 4 — PASS
8. Answer and verbatim quote: `POST /api/questions/{id}/answer` returned HTTP 201 for an 89-byte quote with leading and trailing spaces, a newline, a tab, a non-breaking space, curly quotes and an accented letter; the log records `cmp` exit code 0 for the sent bytes against the quote read back from `GET /api/questions/{id}` and exit code 0 against the quote from `GET /api/questions/{id}/answers`, both 89 bytes with sha256 `c8729171701b056d57ab4b38db15bbafaf6c098871c5910b1105ca757469476f`, the same value as the API's `quote_sha256` — PASS
9. Web dialog (extra): headless Chrome was already installed, so the real answer dialog was opened with `window.openQuestion` on the proposal question and showed the question text, both tradeoff lines under their options, the context line "This question is about: README.md" with its Open control, the default line, the label "Your answer in your own words" and the helper line "Optional when you pick an option. Whatever you type is saved exactly as written, with your choice, as the record of your decision, and the asker reads it as your answer." (the dialog text is in the evidence log) — PASS

## Cleanup

- Daemon stopped by its exact PID (2144102): it exited within 2 seconds and port 47931 is closed.
- Headless Chrome stopped by its exact PID (2170343): it exited and port 47933 is closed; no process mentioning `qc-live` remained. No process was ended by name.
- Questions created through the API: 8 stored (the two refused raises in check 4 stored nothing). 7 were withdrawn (HTTP 200 each) and `GET /api/questions?state=open` then returned `"count":0`. **Deviation from the spec wording "delete every question":** the 8th, the question answered in check 8, could not be removed through the API. `POST /api/questions/{id}/withdraw` returned HTTP 409 ("question is not open") because a decided question is kept by design, and the API has no delete route (only POST and GET are accepted on the question paths). Its record sat in the sandbox data directory (`questions.jsonl`, one line, confirmed before removal), which was deleted with the rest of `.datawatch/qc-live`, so no question created by this check survives anywhere. This is a gap in the task wording, not in the feature under test: the API offers no way to delete a decided question, so the only possible removal is the sandbox data directory.
- `.datawatch/qc-live` removed whole (binary, config, data, logs, Chrome profile): `ls .datawatch/qc-live` reports no such file or directory, `ss` shows nothing listening on 47931 or 47933, and `git status` shows no stray files.
- No tmux command was run and the version variables were not touched.

## Overall result

Checks: PASS — all eight checks and the web dialog check passed; nothing failed and nothing was fixed in this task.
Cleanup: PARTIAL against the literal wording only — everything was stopped, removed and confirmed closed, and seven questions were withdrawn through the API, but the one answered question cannot be deleted through the API (no delete route exists), so it was removed with the sandbox data directory instead.
Overall: PASS for the feature under test; the single cleanup deviation is a limit of the API, recorded above.

## Evidence log

Recorded requests and responses from the sandbox, in order and verbatim. Section headings name
the check each block belongs to. In check 8 the request body was printed as one line.

```text
## check 1: options without tradeoffs, mode warn
[17:12:17] GET /api/questions/quality 
  -> HTTP 200 {"mode":"warn","min_text_chars":60,"tracker_id_pattern":"\\b[A-Z]{1,3}[0-9]{2,4}\\b","queue_preview_lines":3}

[17:12:17] POST /api/questions {"label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder"},{"id":"split","label":"Split per team"}],"default":"hold"}
  -> HTTP 201 {"question":{"id":"q-18e36b0329b1d2677e65","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder"},{"id":"split","label":"Split per team"}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:17.952182887-04:00","state":"open","return_path":false,"source":"rest"},"warnings":[{"code":"no_tradeoffs","message":"These options have no trade-offs written: keep, split. Say what the operator gains and gives up with each choice."}]}


## check 2: tradeoffs via REST map
[17:12:17] POST /api/questions {"label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder"},{"id":"split","label":"Split per team"}],"tradeoffs":{"keep":"Nothing to change, but the folder keeps growing.","split":"Easier browsing, but old links to the shared file break."},"default":"hold"}
  -> HTTP 201 {"question":{"id":"q-18e36b032a76ccb950dd","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder","tradeoffs":"Nothing to change, but the folder keeps growing."},{"id":"split","label":"Split per team","tradeoffs":"Easier browsing, but old links to the shared file break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:17.965092025-04:00","state":"open","return_path":false,"source":"rest"}}

[17:12:17] GET /api/questions/q-18e36b032a76ccb950dd 
  -> HTTP 200 {"answer":null,"answer_count":0,"current_answer_id":"","question":{"id":"q-18e36b032a76ccb950dd","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder","tradeoffs":"Nothing to change, but the folder keeps growing."},{"id":"split","label":"Split per team","tradeoffs":"Easier browsing, but old links to the shared file break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:17.965092025-04:00","state":"open","return_path":false,"source":"rest","context_ref":{"kind":"none","title":"","id":"","doc_path":"","viewer_url":""}}}


## check 2: tradeoffs via CLI --tradeoff
question raised: q-18e36b032bc86fc2dae5
  label: decision
  options: 2
  default: hold
  state: open
[17:12:17] GET /api/questions/q-18e36b032bc86fc2dae5 
  -> HTTP 200 {"answer":null,"answer_count":0,"current_answer_id":"","question":{"id":"q-18e36b032bc86fc2dae5","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder","tradeoffs":"Nothing to change, but the folder keeps growing."},{"id":"split","label":"Split per team","tradeoffs":"Easier browsing, but old links to the shared file break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:17.987219394-04:00","state":"open","return_path":false,"source":"rest","context_ref":{"kind":"none","title":"","id":"","doc_path":"","viewer_url":""}}}


## check 3a: short text
[17:12:18] POST /api/questions {"label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Split the folder?","options":[{"id":"keep","label":"Keep","tradeoffs":"Nothing changes."},{"id":"split","label":"Split","tradeoffs":"Links break."}],"default":"hold"}
  -> HTTP 201 {"question":{"id":"q-18e36b032cd5b71a84ec","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Split the folder?","options":[{"id":"keep","label":"Keep","tradeoffs":"Nothing changes."},{"id":"split","label":"Split","tradeoffs":"Links break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:18.004866842-04:00","state":"open","return_path":false,"source":"rest"},"warnings":[{"code":"text_too_short","message":"The question text is 17 characters; at least 60 are needed to explain what is being asked and why. Add the context the operator needs to decide."}]}


## check 3b: tracker-style id (made-up, not in any registry)
[17:12:18] POST /api/questions {"label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should we keep the shared notes folder as it is while ZQ8841 is open, or split it into one folder per team now?","options":[{"id":"keep","label":"Keep","tradeoffs":"Nothing changes."},{"id":"split","label":"Split","tradeoffs":"Links break."}],"default":"hold"}
  -> HTTP 201 {"question":{"id":"q-18e36b032d95c984bdcd","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should we keep the shared notes folder as it is while ZQ8841 is open, or split it into one folder per team now?","options":[{"id":"keep","label":"Keep","tradeoffs":"Nothing changes."},{"id":"split","label":"Split","tradeoffs":"Links break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:18.017454468-04:00","state":"open","return_path":false,"source":"rest"},"warnings":[{"code":"tracker_id","message":"The question contains \"ZQ8841\", which looks like an internal tracker identifier. The operator reads these questions and may not know it; describe the item in plain words instead."}]}


## check 4: refuse mode
[17:12:18] GET /api/questions 
  -> HTTP 200 {"count":5,"questions":[{"id":"q-18e36b032d95c984bdcd","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should we keep the shared notes folder as it is while ZQ8841 is open, or split it into one folder per team now?","options":[{"id":"keep","label":"Keep","tradeoffs":"Nothing changes."},{"id":"split","label":"Split","tradeoffs":"Links break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:18.017454468-04:00","state":"open","return_path":false,"source":"rest","context_ref":{"kind":"none","title":"","id":"","doc_path":"","viewer_url":""}},{"id":"q-18e36b032cd5b71a84ec","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Split the folder?","options":[{"id":"keep","label":"Keep","tradeoffs":"Nothing changes."},{"id":"split","label":"Split","tradeoffs":"Links break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:18.004866842-04:00","state":"open","return_path":false,"source":"rest","context_ref":{"kind":"none","title":"","id":"","doc_path":"","viewer_url":""}},{"id":"q-18e36b032bc86fc2dae5","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder","tradeoffs":"Nothing to change, but the folder keeps growing."},{"id":"split","label":"Split per team","tradeoffs":"Easier browsing, but old links to the shared file break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:17.987219394-04:00","state":"open","return_path":false,"source":"rest","context_ref":{"kind":"none","title":"","id":"","doc_path":"","viewer_url":""}},{"id":"q-18e36b032a76ccb950dd","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder","tradeoffs":"Nothing to change, but the folder keeps growing."},{"id":"split","label":"Split per team","tradeoffs":"Easier browsing, but old links to the shared file break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:17.965092025-04:00","state":"open","return_path":false,"source":"rest","context_ref":{"kind":"none","title":"","id":"","doc_path":"","viewer_url":""}},{"id":"q-18e36b0329b1d2677e65","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder"},{"id":"split","label":"Split per team"}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:17.952182887-04:00","state":"open","return_path":false,"source":"rest","context_ref":{"kind":"none","title":"","id":"","doc_path":"","viewer_url":""}}]}

stored before: 5
[17:12:18] PUT /api/config {"autonomous.questions.quality_mode":"refuse"}
  -> HTTP 200 {"status":"ok"}

[17:12:18] GET /api/questions/quality 
  -> HTTP 200 {"mode":"refuse","min_text_chars":60,"tracker_id_pattern":"\\b[A-Z]{1,3}[0-9]{2,4}\\b","queue_preview_lines":3}

[17:12:18] POST /api/questions {"label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder"},{"id":"split","label":"Split per team"}],"default":"hold"}
  -> HTTP 422 {"error":"question refused: it does not meet the question quality policy; fix the findings and send it again","findings":[{"code":"no_tradeoffs","message":"These options have no trade-offs written: keep, split. Say what the operator gains and gives up with each choice."}]}

[17:12:18] POST /api/questions {"label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Split the folder?","options":[{"id":"keep","label":"Keep","tradeoffs":"Nothing changes."},{"id":"split","label":"Split","tradeoffs":"Links break."}],"default":"hold"}
  -> HTTP 422 {"error":"question refused: it does not meet the question quality policy; fix the findings and send it again","findings":[{"code":"text_too_short","message":"The question text is 17 characters; at least 60 are needed to explain what is being asked and why. Add the context the operator needs to decide."}]}

[17:12:18] GET /api/questions 
  -> HTTP 200 {"count":5,"questions":[{"id":"q-18e36b032d95c984bdcd","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should we keep the shared notes folder as it is while ZQ8841 is open, or split it into one folder per team now?","options":[{"id":"keep","label":"Keep","tradeoffs":"Nothing changes."},{"id":"split","label":"Split","tradeoffs":"Links break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:18.017454468-04:00","state":"open","return_path":false,"source":"rest","context_ref":{"kind":"none","title":"","id":"","doc_path":"","viewer_url":""}},{"id":"q-18e36b032cd5b71a84ec","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Split the folder?","options":[{"id":"keep","label":"Keep","tradeoffs":"Nothing changes."},{"id":"split","label":"Split","tradeoffs":"Links break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:18.004866842-04:00","state":"open","return_path":false,"source":"rest","context_ref":{"kind":"none","title":"","id":"","doc_path":"","viewer_url":""}},{"id":"q-18e36b032bc86fc2dae5","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder","tradeoffs":"Nothing to change, but the folder keeps growing."},{"id":"split","label":"Split per team","tradeoffs":"Easier browsing, but old links to the shared file break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:17.987219394-04:00","state":"open","return_path":false,"source":"rest","context_ref":{"kind":"none","title":"","id":"","doc_path":"","viewer_url":""}},{"id":"q-18e36b032a76ccb950dd","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder","tradeoffs":"Nothing to change, but the folder keeps growing."},{"id":"split","label":"Split per team","tradeoffs":"Easier browsing, but old links to the shared file break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:17.965092025-04:00","state":"open","return_path":false,"source":"rest","context_ref":{"kind":"none","title":"","id":"","doc_path":"","viewer_url":""}},{"id":"q-18e36b0329b1d2677e65","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder"},{"id":"split","label":"Split per team"}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:17.952182887-04:00","state":"open","return_path":false,"source":"rest","context_ref":{"kind":"none","title":"","id":"","doc_path":"","viewer_url":""}}]}

stored after: 5

## check 4 control: well-formed question in refuse mode
[17:12:18] POST /api/questions {"label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder"},{"id":"split","label":"Split per team"}],"tradeoffs":{"keep":"Nothing to change, but the folder keeps growing.","split":"Easier browsing, but old links to the shared file break."},"default":"hold"}
  -> HTTP 201 {"question":{"id":"q-18e36b0330eb21669aa9","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder","tradeoffs":"Nothing to change, but the folder keeps growing."},{"id":"split","label":"Split per team","tradeoffs":"Easier browsing, but old links to the shared file break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:18.073379174-04:00","state":"open","return_path":false,"source":"rest"}}


## check 5: mode off
[17:12:18] PUT /api/config {"autonomous.questions.quality_mode":"off"}
  -> HTTP 200 {"status":"ok"}

[17:12:18] GET /api/questions/quality 
  -> HTTP 200 {"mode":"off","min_text_chars":60,"tracker_id_pattern":"\\b[A-Z]{1,3}[0-9]{2,4}\\b","queue_preview_lines":3}

[17:12:18] POST /api/questions {"label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Split the folder?","options":[{"id":"keep","label":"Keep"},{"id":"split","label":"Split"}],"default":"hold"}
  -> HTTP 201 {"question":{"id":"q-18e36b03328efd341203","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Split the folder?","options":[{"id":"keep","label":"Keep"},{"id":"split","label":"Split"}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:18.100895028-04:00","state":"open","return_path":false,"source":"rest"}}

[17:12:18] PUT /api/config {"autonomous.questions.quality_mode":"warn"}
  -> HTTP 200 {"status":"ok"}


## check 6: proposal_path context_ref
[17:12:18] POST /api/questions {"label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"proposal","proposal_path":"docs/plans/README.md","text":"Should the plans index list finished plans separately from the ones still open? Separating them shortens the main list.","options":[{"id":"keep","label":"Keep one list","tradeoffs":"Nothing to change, but the list keeps growing."},{"id":"split","label":"Separate finished plans","tradeoffs":"A shorter main list, but readers need a second click for history."}],"default":"hold"}
  -> HTTP 201 {"question":{"id":"q-18e36b0333d99c113335","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"proposal","proposal_path":"docs/plans/README.md","text":"Should the plans index list finished plans separately from the ones still open? Separating them shortens the main list.","options":[{"id":"keep","label":"Keep one list","tradeoffs":"Nothing to change, but the list keeps growing."},{"id":"split","label":"Separate finished plans","tradeoffs":"A shorter main list, but readers need a second click for history."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:18.122562577-04:00","state":"open","return_path":false,"source":"rest"}}

[17:12:18] GET /api/questions/q-18e36b0333d99c113335 
  -> HTTP 200 {"answer":null,"answer_count":0,"current_answer_id":"","question":{"id":"q-18e36b0333d99c113335","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"proposal","proposal_path":"docs/plans/README.md","text":"Should the plans index list finished plans separately from the ones still open? Separating them shortens the main list.","options":[{"id":"keep","label":"Keep one list","tradeoffs":"Nothing to change, but the list keeps growing."},{"id":"split","label":"Separate finished plans","tradeoffs":"A shorter main list, but readers need a second click for history."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:18.122562577-04:00","state":"open","return_path":false,"source":"rest","context_ref":{"kind":"proposal","title":"README.md","id":"","doc_path":"docs/plans/README.md","viewer_url":"/diagrams.html#docs/plans/README.md"}}}

[17:12:18] GET /api/questions?state=open 
  -> HTTP 200 {"count":8,"questions":[{"id":"q-18e36b0333d99c113335","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"proposal","proposal_path":"docs/plans/README.md","text":"Should the plans index list finished plans separately from the ones still open? Separating them shortens the main list.","options":[{"id":"keep","label":"Keep one list","tradeoffs":"Nothing to change, but the list keeps growing."},{"id":"split","label":"Separate finished plans","tradeoffs":"A shorter main list, but readers need a second click for history."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:18.122562577-04:00","state":"open","return_path":false,"source":"rest","context_ref":{"kind":"proposal","title":"README.md","id":"","doc_path":"docs/plans/README.md","viewer_url":"/diagrams.html#docs/plans/README.md"}},{"id":"q-18e36b03328efd341203","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Split the folder?","options":[{"id":"keep","label":"Keep"},{"id":"split","label":"Split"}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:18.100895028-04:00","state":"open","return_path":false,"source":"rest","context_ref":{"kind":"none","title":"","id":"","doc_path":"","viewer_url":""}},{"id":"q-18e36b0330eb21669aa9","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder","tradeoffs":"Nothing to change, but the folder keeps growing."},{"id":"split","label":"Split per team","tradeoffs":"Easier browsing, but old links to the shared file break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:18.073379174-04:00","state":"open","return_path":false,"source":"rest","context_ref":{"kind":"none","title":"","id":"","doc_path":"","viewer_url":""}},{"id":"q-18e36b032d95c984bdcd","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should we keep the shared notes folder as it is while ZQ8841 is open, or split it into one folder per team now?","options":[{"id":"keep","label":"Keep","tradeoffs":"Nothing changes."},{"id":"split","label":"Split","tradeoffs":"Links break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:18.017454468-04:00","state":"open","return_path":false,"source":"rest","context_ref":{"kind":"none","title":"","id":"","doc_path":"","viewer_url":""}},{"id":"q-18e36b032cd5b71a84ec","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Split the folder?","options":[{"id":"keep","label":"Keep","tradeoffs":"Nothing changes."},{"id":"split","label":"Split","tradeoffs":"Links break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:18.004866842-04:00","state":"open","return_path":false,"source":"rest","context_ref":{"kind":"none","title":"","id":"","doc_path":"","viewer_url":""}},{"id":"q-18e36b032bc86fc2dae5","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder","tradeoffs":"Nothing to change, but the folder keeps growing."},{"id":"split","label":"Split per team","tradeoffs":"Easier browsing, but old links to the shared file break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:17.987219394-04:00","state":"open","return_path":false,"source":"rest","context_ref":{"kind":"none","title":"","id":"","doc_path":"","viewer_url":""}},{"id":"q-18e36b032a76ccb950dd","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder","tradeoffs":"Nothing to change, but the folder keeps growing."},{"id":"split","label":"Split per team","tradeoffs":"Easier browsing, but old links to the shared file break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:17.965092025-04:00","state":"open","return_path":false,"source":"rest","context_ref":{"kind":"none","title":"","id":"","doc_path":"","viewer_url":""}},{"id":"q-18e36b0329b1d2677e65","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder"},{"id":"split","label":"Split per team"}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:17.952182887-04:00","state":"open","return_path":false,"source":"rest","context_ref":{"kind":"none","title":"","id":"","doc_path":"","viewer_url":""}}]}

GET /diagrams.html -> HTTP 200 11942 bytes
GET /docs/plans/README.md -> HTTP 200 203275 bytes

## check 7: same values on every surface
[17:12:18] PUT /api/config {"autonomous.questions.quality_mode":"refuse","autonomous.questions.quality_min_text_chars":75,"autonomous.questions.queue_preview_lines":4}
  -> HTTP 200 {"status":"ok"}

[17:12:18] GET /api/questions/quality 
  -> HTTP 200 {"mode":"refuse","min_text_chars":75,"tracker_id_pattern":"\\b[A-Z]{1,3}[0-9]{2,4}\\b","queue_preview_lines":4}

$ datawatch question quality
question quality policy:
  mode: refuse (a weak question is refused)
  minimum text length: 75 characters
  internal id pattern: \b[A-Z]{1,3}[0-9]{2,4}\b
  queue preview: 4 lines
$ datawatch question quality --json
{
  "min_text_chars": 75,
  "mode": "refuse",
  "queue_preview_lines": 4,
  "tracker_id_pattern": "\\b[A-Z]{1,3}[0-9]{2,4}\\b"
}
[17:12:18] POST /api/test/message {"text":"question quality"}
  -> HTTP 200 {"count":1,"input":"question quality","responses":["[qc-live-sandbox] question quality\nquestion quality policy:\nmode: refuse\nminimum text length: 75\ntracker-id pattern: \\b[A-Z]{1,3}[0-9]{2,4}\\b\nqueue preview lines: 4"]}

$ MCP question_quality over stdio
2026/10/30 17:12:18 [warn] [mcp] mcp.token and server.token are both empty — MCP SSE's full tool catalog is open to any caller that can reach it
2026/10/30 17:12:18 mcp/stdio: listener daemon started
{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{"prompts":{},"resources":{},"sampling":{},"tools":{"listChanged":true}},"serverInfo":{"name":"datawatch","version":"1.0.0"}}}
{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"{\"mode\":\"refuse\",\"min_text_chars\":75,\"tracker_id_pattern\":\"\\\\b[A-Z]{1,3}[0-9]{2,4}\\\\b\",\"queue_preview_lines\":4}\n"}]}}
2026/10/30 17:12:22 mcp/stdio: listener daemon stopped — stdin closed/EOF (client disconnected or shutting down)

## check 8: answer with verbatim quote
[17:12:22] POST /api/questions/q-18e36b032a76ccb950dd/answer {"chosen_option_id": "keep", "quote": "  Keep it for now.\n\tNon-breaking space, “curly quotes” and café, trailing spaces   ", "surface": "rest", "actor": "qc-live"}
  -> HTTP 201 {"answer":{"id":"a-18e36b043567416b9ff9","question_id":"q-18e36b032a76ccb950dd","actor":"qc-live","surface":"rest","chosen_option_id":"keep","quote":"  Keep it for now.\n\tNon-breaking space, “curly quotes” and café, trailing spaces   ","quote_sha256":"c8729171701b056d57ab4b38db15bbafaf6c098871c5910b1105ca757469476f","created_at":"2026-10-30T17:12:22.443589995-04:00"},"question":{"id":"q-18e36b032a76ccb950dd","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder","tradeoffs":"Nothing to change, but the folder keeps growing."},{"id":"split","label":"Split per team","tradeoffs":"Easier browsing, but old links to the shared file break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:17.965092025-04:00","state":"answered","return_path":false,"source":"rest"}}

sent: 89 bytes sha256 c8729171701b056d57ab4b38db15bbafaf6c098871c5910b1105ca757469476f
got : 89 bytes sha256 c8729171701b056d57ab4b38db15bbafaf6c098871c5910b1105ca757469476f
cmp (sent vs GET /api/questions/{id}) exit code: 0
[17:12:22] GET /api/questions/q-18e36b032a76ccb950dd 
  -> HTTP 200 {"answer":{"id":"a-18e36b043567416b9ff9","chosen_option_id":"keep","quote":"  Keep it for now.\n\tNon-breaking space, “curly quotes” and café, trailing spaces   ","actor":"qc-live","surface":"rest","created_at":"2026-10-30T17:12:22.443589995-04:00"},"answer_count":1,"current_answer_id":"a-18e36b043567416b9ff9","question":{"id":"q-18e36b032a76ccb950dd","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder","tradeoffs":"Nothing to change, but the folder keeps growing."},{"id":"split","label":"Split per team","tradeoffs":"Easier browsing, but old links to the shared file break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:17.965092025-04:00","state":"answered","return_path":false,"source":"rest","context_ref":{"kind":"none","title":"","id":"","doc_path":"","viewer_url":""}}}

[17:12:22] GET /api/questions/q-18e36b032a76ccb950dd/answers 
  -> HTTP 200 {"answers":[{"id":"a-18e36b043567416b9ff9","question_id":"q-18e36b032a76ccb950dd","actor":"qc-live","surface":"rest","chosen_option_id":"keep","quote":"  Keep it for now.\n\tNon-breaking space, “curly quotes” and café, trailing spaces   ","quote_sha256":"c8729171701b056d57ab4b38db15bbafaf6c098871c5910b1105ca757469476f","created_at":"2026-10-30T17:12:22.443589995-04:00","superseded":false}],"current_answer_id":"a-18e36b043567416b9ff9"}

ids file:
q-18e36b0329b1d2677e65
q-18e36b032a76ccb950dd
q-18e36b032bc86fc2dae5
q-18e36b032cd5b71a84ec
q-18e36b032d95c984bdcd
q-18e36b0330eb21669aa9
q-18e36b03328efd341203
q-18e36b0333d99c113335

## check 9: web dialog (headless Chrome, PID 2170343, debugging port 47933)
openQuestion defined: function
---- dialog text ----
Answer question decision
Question q-18e36b0333d99c113335
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

## cleanup: withdraw every open question created
[17:13:01] POST /api/questions/q-18e36b0329b1d2677e65/withdraw {"actor":"qc-live","reason":"qc-live cleanup"}
  -> HTTP 200 {"question":{"id":"q-18e36b0329b1d2677e65","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder"},{"id":"split","label":"Split per team"}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:17.952182887-04:00","state":"withdrawn","return_path":false,"source":"rest"}}

[17:13:01] POST /api/questions/q-18e36b032a76ccb950dd/withdraw {"actor":"qc-live","reason":"qc-live cleanup"}
  -> HTTP 409 {"error":"question q-18e36b032a76ccb950dd: question is not open"}

[17:13:01] POST /api/questions/q-18e36b032bc86fc2dae5/withdraw {"actor":"qc-live","reason":"qc-live cleanup"}
  -> HTTP 200 {"question":{"id":"q-18e36b032bc86fc2dae5","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder","tradeoffs":"Nothing to change, but the folder keeps growing."},{"id":"split","label":"Split per team","tradeoffs":"Easier browsing, but old links to the shared file break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:17.987219394-04:00","state":"withdrawn","return_path":false,"source":"rest"}}

[17:13:01] POST /api/questions/q-18e36b032cd5b71a84ec/withdraw {"actor":"qc-live","reason":"qc-live cleanup"}
  -> HTTP 200 {"question":{"id":"q-18e36b032cd5b71a84ec","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Split the folder?","options":[{"id":"keep","label":"Keep","tradeoffs":"Nothing changes."},{"id":"split","label":"Split","tradeoffs":"Links break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:18.004866842-04:00","state":"withdrawn","return_path":false,"source":"rest"}}

[17:13:01] POST /api/questions/q-18e36b032d95c984bdcd/withdraw {"actor":"qc-live","reason":"qc-live cleanup"}
  -> HTTP 200 {"question":{"id":"q-18e36b032d95c984bdcd","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should we keep the shared notes folder as it is while ZQ8841 is open, or split it into one folder per team now?","options":[{"id":"keep","label":"Keep","tradeoffs":"Nothing changes."},{"id":"split","label":"Split","tradeoffs":"Links break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:18.017454468-04:00","state":"withdrawn","return_path":false,"source":"rest"}}

[17:13:01] POST /api/questions/q-18e36b0330eb21669aa9/withdraw {"actor":"qc-live","reason":"qc-live cleanup"}
  -> HTTP 200 {"question":{"id":"q-18e36b0330eb21669aa9","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Should the shared notes folder be split into one folder per team, or kept as a single folder? Splitting changes where people look for files.","options":[{"id":"keep","label":"Keep one folder","tradeoffs":"Nothing to change, but the folder keeps growing."},{"id":"split","label":"Split per team","tradeoffs":"Easier browsing, but old links to the shared file break."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:18.073379174-04:00","state":"withdrawn","return_path":false,"source":"rest"}}

[17:13:01] POST /api/questions/q-18e36b03328efd341203/withdraw {"actor":"qc-live","reason":"qc-live cleanup"}
  -> HTTP 200 {"question":{"id":"q-18e36b03328efd341203","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"none","text":"Split the folder?","options":[{"id":"keep","label":"Keep"},{"id":"split","label":"Split"}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:18.100895028-04:00","state":"withdrawn","return_path":false,"source":"rest"}}

[17:13:01] POST /api/questions/q-18e36b0333d99c113335/withdraw {"actor":"qc-live","reason":"qc-live cleanup"}
  -> HTTP 200 {"question":{"id":"q-18e36b0333d99c113335","label":"decision","asked_by":"qc-live","asker_kind":"operator","context":"proposal","proposal_path":"docs/plans/README.md","text":"Should the plans index list finished plans separately from the ones still open? Separating them shortens the main list.","options":[{"id":"keep","label":"Keep one list","tradeoffs":"Nothing to change, but the list keeps growing."},{"id":"split","label":"Separate finished plans","tradeoffs":"A shorter main list, but readers need a second click for history."}],"default":"hold","risk":"normal","created_at":"2026-10-30T17:12:18.122562577-04:00","state":"withdrawn","return_path":false,"source":"rest"}}

[17:13:01] GET /api/questions?state=open
  -> HTTP 200 {"count":0,"questions":[]}


## check 8 addendum: answers log vs sent bytes
cmp (sent vs GET /api/questions/{id}/answers) exit code: 0

## cleanup: stop and remove
stopped by exact PID: chrome 2170343 exited, daemon 2144102 exited; ss shows nothing listening on 47931 or 47933
answered question q-18e36b032a76ccb950dd (withdraw refused with 409, no delete route exists) present in the store before removal: 3 line(s) in data/questions.jsonl
after removal: ls .datawatch/qc-live -> No such file or directory; ss -ltn on 47931 and 47933 -> nothing listening; no process mentioning qc-live
```
