---
docs:
  index: true
  topics: [autonomous, decisions, decision-records, operator-questions, folder-watch, follow-ups]
exec_params: []
exec_steps:
  - tool: get_config
    description: Read the decisions settings (folder, clarity rules, document status mode and the configured follow-ups)
    args: {section: "autonomous"}
    read_only: true
  - tool: autonomous_decision_record_list
    description: List the decision records, newest first, with their state and question id
    args: {}
    read_only: true
---
# How-to: decision records, the structured way to put a decision in front of you

A **decision record** is a choice written down once, in a fixed shape, so the
daemon can turn it into a clear question for you. You describe the situation,
what is being decided and the options; the daemon builds the queue question
from that record. Nobody types the question text by hand, so every question
you are asked looks the same and says the same things: what is going on, what
is being decided, what each option costs, and what is recommended.

This guide covers what a record holds, five ways to create one, the folder
file format and its configurable layout, the clarity rules, how an edit or a
withdrawal reaches the open question, making a task wait for the answer, what
happens when you answer, the document status line rewrite, follow-ups,
reading the list, and every setting.

## Base requirements

- The question channel on (`autonomous.questions.enabled` is `true`). The
  record raises its question through the channel. See
  [getting notified when an Automaton asks you a question](operator-questions-notifications.md).
- `autonomous.decisions.enabled` on. It is on by default.
- For the folder route only: `autonomous.decisions.folder` set to a folder
  the daemon can read.

In the web app, `autonomous.decisions.folder` and the other decision settings are in
**Settings → General → Automata**; each has a help entry. The **Decision records**
card on the Automata pages is drawn only once a record exists.

## What a decision record is

| Field | What it holds |
|---|---|
| `id` | `dr-` followed by 20 lowercase hex characters |
| `label` | A short human label. Default `decision` |
| `title` | The plain-language title |
| `situation` | What is going on, in plain words |
| `deciding` | What exactly is being decided |
| `options` | A list of `{id, label, tradeoff}`: a short label and one line of cost and risk |
| `recommendation` | `{option_id, reason}` |
| `default` | What happens if nobody answers: an option id, or the word `hold` |
| `links` | Optional: `doc` (a repo-relative document path), `automaton` (an Automaton id), `registry_row` (a registry row key) |
| `state` | `open`, `answered` or `withdrawn` |
| `question_id` | The queue question the daemon raised from this record |
| `source`, `source_ref` | `rest`, `mcp`, `cli`, `comm` or `file`; for a file, its name inside the folder |
| `answer` | Filled when answered: the answer id, the option chosen, your exact words, who, which surface, when |
| `doc_change` | What the document status line rewrite did, see below |
| `follow_up` | The follow-up you asked for with the answer, and how it went |

### Why the daemon builds the question

The question is built from the record and never from free text sent with it.
The question text is the title, a blank line, the situation, a blank line,
then `Deciding: ` and the deciding line. The options, the recommendation, the
default and the label come straight from the record. The question is asked by
`datawatch` (a system asker) unless the creator is known. Its context follows
the links: an Automaton link gives an Automaton context, otherwise a registry
row link gives a registry row context, otherwise a document link gives a
proposal context that opens the document. Links that do not fit stay on the
record.

## Creating a record

The option line is the same everywhere: three parts split by the field
separator (`|` by default): `id | label | tradeoff`. Repeat the option line
once per option. The recommendation line is `option_id | reason`.

### REST

```bash
curl -sk -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  https://localhost:8443/api/autonomous/decision-records -d '{
  "label": "design choice",
  "title": "Which storage layout for the report archive",
  "situation": "The report archive is growing and the current single folder is slow to list. Two layouts are on the table and both can be built this week.",
  "deciding": "Pick the layout the archive uses from now on.",
  "options": [
    {"id": "by-month", "label": "One folder per month", "tradeoff": "Simple to browse; a very busy month is still one large folder."},
    {"id": "by-hash", "label": "Folders by id prefix", "tradeoff": "Even spread; harder to browse by hand."}
  ],
  "recommendation": {"option_id": "by-month", "reason": "People browse the archive by date."},
  "default": "hold",
  "links": {"doc": "docs/plans/report-archive.md"}
}'
```

The answer is `201` with `record`, `question_id` and, when the clarity rules
warn, `warnings`. A `422` means the rules refused it, with the findings.
`GET /api/autonomous/decision-records/{id}` reads one,
`PUT /api/autonomous/decision-records/{id}` replaces the content of an open
record, and `POST /api/autonomous/decision-records/{id}/withdraw` withdraws it
(optional body `{"reason": "..."}`). The body can only carry the content
fields; state, source, question link and answer cannot be set by a caller.

### MCP

`autonomous_decision_record_create` takes the content fields. `options`,
`recommendation` and `links` are JSON text:

```json
{"tool": "autonomous_decision_record_create", "arguments": {
  "label": "design choice",
  "title": "Which storage layout for the report archive",
  "situation": "The report archive is growing and the current single folder is slow to list. Two layouts are on the table.",
  "deciding": "Pick the layout the archive uses from now on.",
  "options": "[{\"id\":\"by-month\",\"label\":\"One folder per month\",\"tradeoff\":\"Simple to browse; a busy month is still large.\"},{\"id\":\"by-hash\",\"label\":\"Folders by id prefix\",\"tradeoff\":\"Even spread; harder to browse.\"}]",
  "recommendation": "{\"option_id\":\"by-month\",\"reason\":\"People browse by date.\"}",
  "default": "hold"
}}
```

The other tools are `autonomous_decision_record_update` (`id` plus content),
`autonomous_decision_record_withdraw` (`id`, optional `reason`),
`autonomous_decision_record_get` and `autonomous_decision_record_list`.

### The CLI

```bash
datawatch autonomous decision-records create \
  --title "Which storage layout for the report archive" \
  --situation "The report archive is growing and the current single folder is slow to list." \
  --deciding "Pick the layout the archive uses from now on." \
  --option by-month="One folder per month" --option by-hash="Folders by id prefix" \
  --tradeoff by-month="Simple to browse; a busy month is still large" \
  --tradeoff by-hash="Even spread; harder to browse" \
  --recommend by-month --rationale "People browse by date" \
  --default hold --doc docs/plans/report-archive.md
```

`--option` and `--tradeoff` repeat, once per option, in the form `id=label` and `id=text` respectively.
`--file <path>` (or `--file -`) takes the whole record as JSON instead. The
other verbs are `list`, `show <id>`, `update <id>` and `withdraw <id>
[--reason ...]`.

### The chat channel

```text
autonomous decision-record create {"title":"Which storage layout for the report archive","situation":"The report archive is growing and the current folder is slow to list.","deciding":"Pick the layout the archive uses from now on.","options":[{"id":"by-month","label":"One folder per month","tradeoff":"Simple to browse"},{"id":"by-hash","label":"Folders by id prefix","tradeoff":"Even spread"}],"default":"hold"}
```

The reply names the record id, the question id and any warnings. Other verbs:
`autonomous decision-records [automaton-id]`, `autonomous decision-record <id>`
and `autonomous decision-record withdraw <id> [reason]`.

### A file in the configured folder

Drop a file in the folder named by `autonomous.decisions.folder`. The daemon
reads the folder on its poll, creates one record per file and keeps the record
in step with the file.

```markdown
---
label: design choice
title: Which storage layout for the report archive
situation: The report archive is growing and the current single folder is slow to list.
  Two layouts are on the table and both can be built this week.
deciding: Pick the layout the archive uses from now on.
option: by-month | One folder per month | Simple to browse; a very busy month is still one large folder.
option: by-hash | Folders by id prefix | Even spread; harder to browse by hand.
recommendation: by-month | People browse the archive by date.
default: hold
doc: docs/plans/report-archive.md
---
Anything after the closing line is ignored. Keep your own notes here.
```

## The folder file format

The file starts with a block between two delimiter lines. Each line in the
block is `key`, the key separator, then the value. Rules:

- Blank lines and lines starting with `#` are ignored.
- A value continues on the following lines that start with two spaces; the
  pieces are joined with one space.
- `option` is repeatable. The parts of an `option` or `recommendation` line
  are split on the field separator.
- Unknown keys are ignored, and so is everything after the closing delimiter.
- Files over 64 KB are skipped, and one scan reads at most 200 files.

The canonical keys are `label`, `title`, `situation`, `deciding`, `option`,
`recommendation`, `default`, `doc`, `automaton` and `registry_row`.

### Making the layout your own

Four settings reshape the block so your existing documents can serve as
decision files without edits:

| Setting | What it changes |
|---|---|
| `front_matter_delimiter` | The line that opens and closes the block (default `---`) |
| `front_matter_separator` | The separator between a key and its value (default `:`) |
| `front_matter_field_separator` | The separator between the parts of an option or recommendation line (default `\|`) |
| `front_matter_keys` | A map from canonical key name to the name used in files |

A second example, with a different delimiter, separator and names. The
settings are:

```json
{
  "autonomous.decisions.front_matter_delimiter": "+++",
  "autonomous.decisions.front_matter_separator": "=",
  "autonomous.decisions.front_matter_field_separator": ";",
  "autonomous.decisions.front_matter_keys": {
    "title": "Topic", "situation": "Background", "deciding": "Question",
    "option": "Choice", "recommendation": "Advice", "doc": "Document"
  }
}
```

and a file in that layout:

```text
+++
Topic = Retention period for the audit export
Background = Exports are kept forever today and the disk is filling.
  Legal has not set a minimum.
Question = Choose how long exports are kept.
Choice = 90d ; Ninety days ; Small disk use; older data is gone.
Choice = 1y ; One year ; Covers a yearly review; uses about four times the disk.
Advice = 1y ; A yearly review needs last year's data.
default = hold
Document = docs/operations.md
+++
```

A key you did not rename (here `default`) keeps its canonical name.

### What the folder watch does

- A new file creates a record with the source `file`.
- A changed file updates its open record, and through it the same open
  question. An unchanged file does nothing.
- A deleted file withdraws its open record when `folder_withdraw_on_delete`
  is on.
- A file whose record is answered or withdrawn is never reopened. An edit to
  it shows up as a note on the folder status.
- A file that cannot be read or does not make a valid record is skipped with a
  plain message. It never stops the scan. Read the messages on
  `GET /api/autonomous/decision-records` under `folder.errors` and
  `folder.notes`.

## The clarity rules

Before a record raises its question, the daemon checks that you will be able
to decide from what it says. `clarity_mode` sets what happens when a rule
fails:

| Mode | Effect |
|---|---|
| `off` | No checks |
| `warn` (default) | The record is created and the findings come back as warnings |
| `refuse` | The record is not created; the answer is `422` with the findings |

The rules and their defaults:

- The title, situation and deciding text must each be at least
  `clarity_min_text_chars` characters (default 60).
- Every option needs a trade-off line (`clarity_require_tradeoffs`, default on).
- A label is required (`clarity_require_label`, default on).
- The text must not contain a tracker identifier. `clarity_tracker_id_pattern`
  sets the pattern; empty uses the built-in one.

A file that fails under `warn` still creates its record; under `refuse` it is
skipped and the message names the rule.

## One question per record, edits and withdrawals

A record raises exactly one question. When you edit an open record, from REST, MCP,
the CLI, chat or by changing its file, the same open question is updated
in place, with the same id. Nothing new is raised, so a task that names the
question keeps waiting on the right one. When you withdraw a record, or its
file is deleted with withdrawal on delete, its open question is withdrawn too.
An answered record cannot be edited: record a new decision instead.

## Making a task wait for the answer

Put the question id (the `question_id` on the record, `q-` and lowercase hex)
in the task's spec. The [decision gate](automaton-operator-decisions.md) holds
the task as **Waiting for a decision** until the question is answered. The
record's `links.automaton` is only context; the spec is what holds a task.

## What happens when you answer

You answer the question as always (see
[answering an open operator question](operator-questions-answering.md)). The
daemon then:

1. Stores your exact words and the option you chose on the record's `answer`,
   and marks the record `answered`.
2. Releases every task whose spec names the question.
3. Runs the document status line rewrite (next section) and the follow-up you
   asked for, if any.

## The document status line rewrite

When a record links a document (`links.doc`), the answer can update a status
line in that document. The path resolves against `doc_root` (empty is the
daemon's working directory) and must stay inside it. `doc_status_mode`:

| Mode | Effect |
|---|---|
| `off` | The document is not looked at |
| `report` (default) | The change is worked out and shown on the record, and no file is touched |
| `on` | The change is written to the file |

The line to replace is the first line matching `doc_status_pattern` (default
`^Confirmed:.*$`). The new line comes from `doc_status_template`, default
`Confirmed: {choice} - "{quote}" ({actor}, {date})`, with the placeholders
`{choice}`, `{choice_label}`, `{quote}`, `{actor}`, `{date}` and
`{decision_id}`.

In `report` mode the record shows what would change, in `doc_change`:

```json
"doc_change": {
  "mode": "report",
  "path": "docs/plans/report-archive.md",
  "line": 12,
  "before": "Confirmed: pending",
  "after": "Confirmed: by-month - \"Go with by-month\" (dmz, 2026-11-06)",
  "status": "reported"
}
```

`status` is one of `reported`, `applied`, `unchanged`, `no_match` (no line
matched the pattern), `ambiguous` (more than one line matched, so nothing is
changed) or `error`. `diff` holds the change as a diff.

To review an applied change in `on` mode, use git, since the document is in
your repository:

```bash
git diff -- docs/plans/report-archive.md
```

Leave the mode on `report` until you have read a few of these and trust the
pattern.

## Follow-ups

A follow-up is something the daemon does after an answer. **Nothing runs
unless you configure it.** `autonomous.decisions.follow_ups` is a list of
`{name, kind, target}` and is empty by default. `kind` is `start_template`
(target is a template id) or `open_automaton` (target is an Automaton id).

```json
{"autonomous.decisions.follow_ups": [
  {"name": "start-archive-work", "kind": "start_template", "target": "tmpl-archive-migration"},
  {"name": "open-release", "kind": "open_automaton", "target": "a1b2c3d4"}
]}
```

An answer names one by its `name`:

- REST: `follow_up` in the body of `POST /api/questions/{id}/answer`.
- CLI: `datawatch question answer <id> --option <id> --follow-up <name>`.
- MCP: the `follow_up` argument of `question_answer`.

A name that is not in the list is refused. The record's `follow_up` shows what
was requested, its status and its result.

## Reading the list

| Surface | How |
|---|---|
| REST | `GET /api/autonomous/decision-records` (filters `state`, `prd_id`, `limit`), or `GET /api/autonomous/decision-records/{id}` |
| MCP | `autonomous_decision_record_list` and `autonomous_decision_record_get` (the decision line is `autonomous_decisions`) |
| CLI | `datawatch autonomous decision-records list [--state ...] [--automaton ...] [--limit n] [--json]` and `show <id>` |
| Chat | `autonomous decision-records [automaton-id]` and `autonomous decision-record <id>` |
| PWA | the **Decision records** card in the Automata area. It shows only when at least one record exists, and the folder-watch-off note shows in Settings only; the folder and the other decision settings (clarity rules, document status mode and follow-ups) are in **Settings → General → Automata**, each with a help entry that opens this guide or the decision record settings in the definitions |

The decision line (`GET /api/autonomous/decisions`) carries the record id on
each decision raised from a record.

## Every setting

All under `autonomous.decisions.`:

| Key | Default |
|---|---|
| `enabled` | `true` |
| `folder` | empty (folder watch off) |
| `folder_glob` | `*.md` |
| `front_matter_delimiter` | `---` |
| `front_matter_separator` | `:` |
| `front_matter_field_separator` | `\|` |
| `front_matter_keys` | empty (canonical names) |
| `folder_withdraw_on_delete` | `true` |
| `clarity_mode` | `warn` |
| `clarity_min_text_chars` | `60` |
| `clarity_require_tradeoffs` | `true` |
| `clarity_require_label` | `true` |
| `clarity_tracker_id_pattern` | empty (built-in pattern) |
| `doc_root` | empty (the daemon's working directory) |
| `doc_status_mode` | `report` |
| `doc_status_pattern` | `^Confirmed:.*$` |
| `doc_status_template` | `Confirmed: {choice} - "{quote}" ({actor}, {date})` |
| `follow_ups` | empty (nothing runs) |

Change them with `PUT /api/config`, `datawatch config set`, the chat
`configure` verb or the MCP `config_set` tool. They are read at the moment
they are used, so no restart is needed.
