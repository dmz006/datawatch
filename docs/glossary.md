# Glossary

Short definitions of terms used across the docs. The full reference is
[datawatch-definitions.md](datawatch-definitions.md).

## Pool fall-through

The setting `autonomous.capacity_fallthrough` (default off). When on, a worker
task whose requested capacity pool cannot be granted is admitted on an
alternate backend that has room instead of waiting. Verifiers never fall
through. See [howto/automata-capacity.md](howto/automata-capacity.md#pool-fall-through).

## Allowed fall-through target

An LLM entry setting (`fallthrough_target`, default off). With pool
fall-through on, a marked entry may receive a worker whose requested pool is
full. The other lane fill backend is tried first and needs no mark. See
[howto/automata-capacity.md](howto/automata-capacity.md#pool-fall-through).

## agent-aider venv Python

The Python that runs aider inside the agent-aider container image: a uv-managed Python 3.12 in a dedicated venv, not Debian 12's system Python 3.11.2. It exists so soupsieve 4.x (needs Python 3.11.5 or newer) can be installed. See [Container image refresh](howto/container-image-refresh.md#agent-aider-python-and-the-soupsieve-floor).
## Quick reply

A one-tap reply offered on the web app — the dropdown on a prompt alert,
the session-detail Yes/No/Stop chips, or a quick command. It first goes
through an open-question gate: if the session has an open question and
the reply matches one of its options, it **answers that question**
(nothing is typed into the session); with no open question it is sent as
session input. A reply matching no option, or a session with more than one
open question, is refused with a message and nothing is sent. Messaging
channels apply the same gate to `send <id>: <text>` when the text is a
saved command (`!name` / `/name`), an approve/reject word, or an option
id or label of the session's open question; other text is typed into the
session as before. See
[howto/alerts-and-notifications.md](howto/alerts-and-notifications.md#messaging-channel-quick-replies).

## Verification-script preflight

Text-only checks on a verification or final-gate script that catch causes of a
failed first run unrelated to the task: a shell syntax error
(`verify_script_syntax`), a helper called with a different shell than its
shebang (`verify_script_shell_mismatch`), a piped `grep -q` under `pipefail`
(`verify_script_pipefail_grep_q`), and an allowed-file list that is not
cumulative over the whole plan (`verify_script_allowed_files`). Runs on every
decompose-lint pass (recorded even in mode `off`; drives the retry in
`enforce`) and when a task edit sets a verification script; every finding is
`block`. The dry run does not run it. See
[howto/automaton-verification-scripts.md](howto/automaton-verification-scripts.md#5a-preflight-checks-for-planner-written-scripts).
