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
