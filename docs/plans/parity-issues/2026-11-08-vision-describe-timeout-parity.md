# [sync] vision describe: bounded image tags and @path fallback (datawatch next patch release)

**Not filed yet.** The supervisor files this on the `dmz006/datawatch-app` repository with `gh issue create` at merge. The issue title is the heading above and the issue body is the text below, from "Parent release" through the end of the body. Whoever files it records the issue number in the Filing record at the end.

Parent release: datawatch next patch release (fix currently under `[Unreleased]` in `CHANGELOG.md`; daemon version at planning is v9.109.0).

## Affected area

Session image sends from the terminal and the mobile clients: an image the operator attaches (PWA file attach, or the app's `POST /api/files` attach flow) is delivered to a session as an `[image:<path>]` tag in the message text, which the daemon expands through the vision service before the session sees it. Everything downstream of the daemon — how the expanded text reads, how the fallback reference reads — is unchanged; what changed is how long the operator may wait for the expansion and what the operator sees when the vision backend is slow, hung, or down. The new configuration knob the fix introduces is `vision.describe_timeout_seconds`. There is no REST, WebSocket, or MCP contract change, and no new locale string is involved.

## Current behavior, after this change

Before this change, the vision describe call in the websocket input path had no deadline. A slow or hung vision backend blocked that connection's read loop for as long as the model took; while one image send waited on the model, every other input on the same terminal (a keystroke, another send, a command) was stuck, and a backend that ignored cancellation could hold it indefinitely — a PWA image send could visibly freeze the terminal.

Now two things are true, and they close that gap for both the PWA terminal and any client, including Android and iPhone, that sends `[image:<path>]` tags over the same websocket or REST paths:

1. Each describe request runs under a per-request deadline — `vision.describe_timeout_seconds`, default 5 seconds, with `0` or a negative value keeping the 5-second default. The deadline is applied by the daemon, not the backend, so a vision server that ignores its context still cannot hold the wait.
2. On timeout, on any describe error, on an empty description, or when no vision service is configured at all, the tag degrades to the bare `@<path>` reference — the same notation Claude Code sessions already use to read an image file themselves. The message is still delivered; the session can still see the image by reading the file. An unreadable image file never degrades: the `[image:<path>]` tag passes through unchanged.
3. The expansion of an image-bearing send runs on a per-connection input worker goroutine (FIFO) instead of the websocket read loop, so later messages on the same connection are processed while an image send is still waiting, and a plain send never overtakes an earlier still-expanding image send for the same session. Ordering into the session is unchanged.

For the app clients the observable contract is: an image send resolves within the daemon's describe timeout instead of hanging; the delivered text is either the usual `[image: <description> | path: <path>]` form (fast backend) or the bare `@<path>` reference (slow, hung, or absent backend) — never a dropped message, and never an indefinitely waiting input path. `docs/api/mobile-surface.md` already records the bounded behavior and the `@<path>` fallback, so the app's existing assumption that an image send eventually completes is now guaranteed.

## The parity gap this leaves open

The new `vision.describe_timeout_seconds` setting is reachable from every operator-interface surface — the YAML config (`vision.describe_timeout_seconds`), the CLI (`datawatch config set vision.describe_timeout_seconds 15`), the REST API (`PUT /api/config` with `{"vision.describe_timeout_seconds": 15}`), and the comm channel (`configure vision.describe_timeout_seconds=15`) — but not from the PWA. The PWA settings have no vision section at all: no field for the timeout, and none for the rest of the `vision` block (`enabled`, `backend`, `endpoint`, `api_key`, `model`, `default_prompt`, `max_image_bytes`). An operator whose vision backend routinely needs more than five seconds — and who therefore gets every image send degrading to `@<path>` — has to find out by watching the fallbacks, and has to raise the timeout from a terminal or API call; the same is true on Android and iPhone, where there is no settings surface for any `vision.*` key. The 5-second default is a reasonable choice, but it is the one knob in the whole `vision` block that directly degrades the mobile/PWA image-send experience, and it is the least discoverable.

## Expected behavior

A mobile or PWA operator who sends an image and starts seeing `@<path>` fallbacks instead of descriptions should be able to learn, from the client, that the daemon bounded the describe call, that the fallback is the expected degraded form (not an error), and where the timeout is configured — and ideally be able to see or adjust `vision.describe_timeout_seconds` without reaching for a terminal.

## Suggested next step

Treat this as a low-priority follow-up in `datawatch-app`, not a blocker for this fix: the behavior change needs no app-side action, and the fix is a strict improvement for all clients. Optionally add the `vision` block — at minimum `describe_timeout_seconds`, alongside `enabled`, `model` and `max_image_bytes` — to the settings surface on Android, iPhone and the PWA, each reading and writing the same `vision.*` keys the REST config endpoint already round-trips, with a help link to `datawatch-definitions.md#vision-system`; and wherever the app presents a sent message that arrived as the `@<path>` fallback, leave an affordance (even just the existing tap-to-open-file path) so the operator knows the image is still referenceable by the session. If the app team agrees the timeout is an advanced setting, record that decision in this issue and close the gap as intentional exclusion instead.

## Filing record

Not filed yet. When the supervisor files it, record the issue number and link here in the form used by `2026-10-30-node-tls-verification-parity.md`.
