---
docs:
  index: true
  topics: [attempt cost, first attempt, failed attempt, retry, resume, escalation, automata]
---

# Failed-attempt cost

A failing task can cost several full starts. Attempt cost sets three daemon-wide
choices so that the cheap work is tried first and the strong worker only starts
once a task has actually failed. You set them in **Settings > General >
Autonomous**, or with the matching keys in `datawatch config set`, the REST API,
the MCP `autonomous_config_set` tool or the YAML file.

## The settings

| Setting (config key) | Default | What it does |
|---|---|---|
| `autonomous.first_attempt.<kind>.backend` | empty | Backend the first attempt of each task kind runs on. Empty keeps the normal choice. |
| `autonomous.first_attempt.<kind>.model` | empty | Model for that backend. `pi`, `pi-prompt` and `ollama` need a model. |
| `autonomous.first_attempt_escalate_after_failures` | `1` | Failed attempts before the strong worker takes over. Range 0 to 20. |
| `autonomous.retry_resume_max_context_tokens` | `150000` | Largest prior context a retry may resume. Range 0 to 2000000. `0` means every retry starts a fresh session. |

`<kind>` is one of `implementation`, `docs`, `note` or `final_gate`. A kind is
chosen from the task's own files: a read-only or file-less task is a `note`, a
task whose files are all documentation is `docs`, the Automaton's final gate
task is `final_gate`, and everything else is `implementation`.

## How the first-attempt pair is applied

- **First attempts only.** Once a task has failed as many times as
  `first_attempt_escalate_after_failures`, the pair stops applying and the normal
  resolution takes over, including the failure-escalation ladder.
- **A pin beats the pair.** A worker pinned on the task, its story, its Automaton
  or by a burn-guard route is used instead.
- **Empty means unchanged.** A kind with no backend keeps today's choice.

## Resuming a retry

When a task attempt fails verification and its session is still open, the next
attempt can resume that session. It receives one retry line with the failure
summary (capped at 4,000 characters) rather than a fresh session with the full
prompt. Only backends that can resume a session do this; `claude-code` is the
one that can today. A retry resumes only while the prior context is within
`retry_resume_max_context_tokens`; above that limit, or with the limit at `0`,
the retry starts fresh.

## Reading the attempt rows

Each attempt in an Automaton's task review panel shows:

- **Why this worker** — the backend reason: `first attempt for <kind> tasks`,
  `escalated after <n> failures`, `operator pinned` or `default choice`.
- **Resumed the previous session** or **Fresh session**, with the resume reason
  when the retry decision gave one.
- **No retry** — present only when the failure held the task for review without
  another attempt, with the reason.

## Errors

Each field saves when you leave it. If the daemon refuses a value, the message
appears in a red line under that field and the value stays in the box so you can
correct it. Nothing is saved on a refusal. Values outside the ranges above are
refused before they are sent.

Related: [failure-escalation](failure-escalation.md) covers the retry ladder, the
review hold and the rest of the failure policy.
