# [parity] vision describe timeout: bound, fallback, and every config surface (datawatch-app follow-up)

**Not filed yet.** The supervisor files this on the `dmz006/datawatch-app` repository with `gh issue create` at merge. The issue title is the heading above and the issue body is the text below, from "Parent release" through the end of the body. Whoever files it records the issue number in the Filing record at the end. Draft updated 2026-11-08 to reflect the work that has since landed on the datawatch side (PWA Settings field in all five locales, the vision how-to, and the updated changelog, definitions, config reference and MCP docs).

Parent release: datawatch next patch release (fix currently under `[Unreleased]` in `CHANGELOG.md`; daemon version at planning is v9.109.0). The fix needs no app-side action; this is the follow-up issue for the gap it leaves open.

## Affected area

Session image sends and the vision describe pipeline, from the operator's point of view. When an operator attaches an image (PWA file attach, app attach flow, or an `[image:<path>]` tag in the sent text), the daemon expands the tag through the vision service before the session sees the message. Everything downstream of the daemon — how an expanded description reads and how a fallback reference reads — is unchanged. What changed is how long an image send may spend waiting for a description, what the operator sees when the vision backend is slow, hung, or down, and where the new `vision.describe_timeout_seconds` knob is visible and settable.

## What this work closes

Before, the vision describe call in the websocket input path had no deadline. A slow or hung vision backend blocked that connection's read loop for as long as the model took; while one image send waited on the model, every other input on the same terminal (a keystroke, another send, a command) was stuck, and a backend that ignored cancellation could hold it indefinitely. A PWA image send could visibly freeze the terminal, and the only way out was patience.

Now, on the datawatch daemon:

1. Every describe request runs under a per-request deadline — `vision.describe_timeout_seconds`, 5 seconds by default, with 0 or a negative value meaning "keep the 5-second default". The deadline is enforced by the caller: the model call runs in its own worker, so a backend that ignores its context still cannot hold the wait past the deadline.
2. On timeout, on any describe error, on an empty description, or when no vision service is configured at all, the tag degrades to the bare `@<path>` reference — the same notation Claude Code sessions already use to read an image file themselves. The message is still delivered and the session can still see the image by reading the file. An unreadable image file never degrades: the `[image:<path>]` tag passes through unchanged.
3. Over a WebSocket the expansion runs on the connection's input worker in send order instead of the socket's read loop: later messages on the same connection are processed while an image send still waits, and a later plain send to the same session on the same connection waits its turn behind the still-expanding image send rather than overtaking it. Ordering into the session is unchanged — the wait is per connection, not global per session.
4. The knob is on every operator-interface surface in the datawatch repo, and the operator can see and set it without a terminal: the YAML config (`vision.describe_timeout_seconds`), the CLI (`datawatch config set vision.describe_timeout_seconds 15`), REST (`PUT /api/config` with `{"vision.describe_timeout_seconds": 15}`; `GET /api/config` reports the effective value, 5 when unset), MCP (`config_set` / `get_config`), the comm channel (`configure vision.describe_timeout_seconds=15`) — and the PWA, where Settings → Vision (Image Descriptions) gained a **Describe timeout (seconds, 0 = default 5)** field with a help tooltip explaining the `@path` fallback, localized in all five locales (en, de, es, fr, ja) and linked to the new how-to `docs/howto/vision-image-descriptions.md`, which documents the timeout, the fallback table, the per-connection ordering guarantee, valid-value rules, and the full surface matrix. A sub-1, fractional, or non-numeric write is refused on every write surface and the stored value is left alone; the YAML path is forgiving instead (unset, 0, or negative all mean the default).

So the "PWA has no way to see or set the timeout" half of the gap is closed: the PWA already had the rest of the `vision` section (`enabled`, `backend`, `endpoint`, `api_key`, `model`, `default_prompt`, `max_image_bytes`), and it now has this knob with the same read/write semantics as every other surface.

## The parity gap this leaves open

The mobile clients of `datawatch-app` — Android and iPhone — still have no settings surface for any `vision.*` key, and none of the datawatch documentation or daemon behavior tells a mobile operator why an image send came back as a bare `@<path>` reference instead of a folded description. Concretely:

- An operator on Android or iPhone whose vision backend routinely needs more than five seconds now gets every image send degrading to `@<path>`, but the app has no field for `vision.describe_timeout_seconds` (or for the rest of the `vision` block), no explanation that the daemon bounded the describe call and that the fallback is the expected, content-preserving degraded form — and no hint of where the timeout is configured. They must discover the cause by reading the daemon's docs and raise the timeout from a terminal, the REST API, or the PWA on another device.
- Where the app presents a sent image that arrived as the `@<path>` fallback, there is no affordance marking it as "the model reads this file itself" — it looks like the image was lost, when in fact the session can still see it.

This is a client-side discovery and presentation gap, not a daemon defect: the daemon's contract for app clients (an image send resolves within the describe timeout, and the delivered text is either the folded description or the `@<path>` reference, never a dropped or hung message) already holds, and `docs/api/mobile-surface.md` records both forms.

## Expected behavior

A mobile operator who sends an image and starts seeing `@<path>` fallbacks instead of descriptions should be able to learn, from the app, that the daemon bounded the describe call, that the fallback is the expected degraded form (not an error), and where the timeout is configured — and ideally be able to see or adjust `vision.describe_timeout_seconds` from the app itself.

## Suggested next step

Treat this as a low-priority follow-up in `datawatch-app`, not a blocker: the datawatch-side fix is a strict improvement for all clients and needs no app change. Optionally add the `vision` block to the settings surface on Android and iPhone — at minimum `describe_timeout_seconds`, alongside `enabled`, `model`, and `max_image_bytes` — each reading and writing the same `vision.*` keys the daemon's REST config endpoint already round-trips (`PUT /api/config` with `{"vision.describe_timeout_seconds": 15}`, `GET /api/config` reporting the effective value), with a help link to the daemon's `datawatch-definitions.md#vision-system` / the vision how-to. And wherever the app renders a sent message that arrived in the `@<path>` fallback form, leave a small affordance (even just the existing tap-to-open-file path, plus a one-line note that the session reads the image itself) so the operator knows the image still reaches the session. If the app team agrees the timeout is an advanced setting that belongs in the daemon's own surfaces only, record that decision in this issue and close the gap as intentional exclusion.

## Filing record

Filed 2026-11-08 as https://github.com/dmz006/datawatch-app/issues/397 with the shipped release v9.112.2.
