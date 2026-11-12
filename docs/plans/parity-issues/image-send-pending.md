# [sync] feat(sessions): image-send pending indicator, `@path` fallback and describe timeout (datawatch-app follow-up)

Not filed yet. File on `dmz006/datawatch-app` with `gh issue create --body-file` and record the issue number and link in the Filing record at the end.

Parent release: datawatch next release. The daemon side is done (PWA pending indicator, the bounded describe call with the `@path` fallback, and the `vision.describe_timeout_seconds` setting on every daemon config surface); this issue covers what the Android and iPhone clients must do to keep up.

## What changed in datawatch

Three things around an operator's image send, from the operator's point of view:

1. **An image send now shows a pending state.** In the PWA, while a session input carrying an `[image:<path>]` tag waits for the daemon to accept it (upload, then the describe call), the composer status banner — the same surface that already reads "Uploading image…" and "Transcribing voice message…" — reads "Sending image…" (locale key `image_send_pending`, translated in all five bundles: en "Sending image…", de "Bild wird gesendet…", es "Enviando la imagen…", fr "Envoi de l'image en cours…", ja "画像を送信中…"). The indicator is purely visual: the input box and the terminal stay fully interactive, nothing is disabled or blocked. It is cleared as soon as the server accepts the input (the send succeeds or the next output frame arrives), again on socket close, and by a hard 15-second client cap so it can never stick.
2. **The server degrades the tag to `@<path>` when describe fails, instead of stalling or dropping the send.** Each describe call is bounded by a per-request deadline. On timeout, on any describe error, on an empty description, or when no vision service is configured, the `[image:<path>]` tag is delivered as a bare `@<path>` reference — the same notation a session already uses to read an image file itself. The message is still delivered and the session can still see the image by reading the file. An unreadable image file never degrades: the tag passes through unchanged. Over a WebSocket the expansion runs on the connection's input worker in send order, so the socket's read loop stays responsive; a later plain send to the same session on the same connection waits its turn behind the still-expanding image send rather than overtaking it.
3. **A new setting, `vision.describe_timeout_seconds`, default 5.** It bounds a single image description request in whole seconds. Unset (0) or a negative value means "keep the 5-second default"; a value of 1 or more is honored as-is. Written through the same surfaces as every other config key: YAML (`vision.describe_timeout_seconds`), CLI (`datawatch config set vision.describe_timeout_seconds 15`), REST (`PUT /api/config` with `{"vision.describe_timeout_seconds": 15}`; `GET /api/config` reports the effective value, 5 when unset), MCP (`config_set` / `get_config`), and the comm channel (`configure vision.describe_timeout_seconds=15`). The PWA Settings → Vision card gained a "Describe timeout (seconds, 0 = default 5)" field localized in all five locales, next to the rest of the `vision` block (`enabled`, `backend`, `endpoint`, `api_key`, `model`, `default_prompt`, `max_image_bytes`). A sub-1, fractional, or non-numeric write is refused on the write surfaces and the stored value is left alone; the YAML path is forgiving (unset, 0, or negative all mean the default).

The operator reference for the record is `docs/howto/vision-image-descriptions.md` (timeout, fallback table, per-connection ordering, valid-value rules, surface matrix) and the mobile API note is `docs/api/mobile-surface.md`'s image-attachment section, which already records both delivered forms — the folded description and the bare `@<path>` reference.

## What the datawatch-app clients must do

Capability parity is required; implementation parity is not — Android and iOS each use idiomatic delivery.

### 1. Show a pending state while an image send is in flight (Android and iPhone)

When the operator attaches an image and hits send, the message round-trip now has two client-visible stages — upload, then the daemon's bounded describe — and the app should say so while it happens:

- Show a status above/at the composer (the same place the app already reports an in-progress upload or voice transcription) with a "Sending image…" string while the image is attached to an outgoing send and the daemon has not accepted it yet.
- Keep the composer fully interactive. The pending state is informational, not a lock: the operator can type, attach more, or send another message while the image send waits. Do not disable the input or queue user input behind it.
- Clear the indicator when the send is accepted (the session input reaches the daemon and the next output frame arrives), again on socket close, and add a client-side cap of about 15 seconds so the indicator can never stick after a transport failure. The daemon's own describe bound (`vision.describe_timeout_seconds`, default 5 s) means the wait has an upper end even when the vision backend hangs.
- Do not assume the send failed if it takes a few seconds: up to the configured describe timeout is normal, and a later message to the same connection may be held in order behind the still-expanding image send (the daemon's per-connection ordering guarantee — sends arrive, just not out of order).

### 2. Treat `@<path>` as a delivered image, not a lost one (Android and iPhone)

A sent message that carried an image can arrive in the transcript in one of two forms:

- `[image: <description> | path: <path>]` — the folded description (the common case), or
- a bare `@<path>` — the daemon's expected degraded form when the describe timed out, errored, came back empty, or vision is not configured. The image was **not** lost: the session reads the file itself using that reference.

The app must:

- Render the bare `@<path>` form as a tappable file reference (the same affordance it already gets from tapping a file path in a message), not as plain prose and not as an error state.
- Where the app shows its own send status for that message, mark it as delivered — the fallback is a successful delivery in the daemon's contract, never a dropped or failed one.
- If the operator keeps seeing `@<path>` fallbacks, point them at the `vision.describe_timeout_seconds` setting (below) and the daemon's vision docs; the daemon's how-to (`docs/howto/vision-image-descriptions.md`) is the canonical reference for the fallback table.

### 3. Offer `vision.describe_timeout_seconds` in app settings (Android and iPhone; or a recorded, reasoned exclusion)

| Item | Value |
|---|---|
| Config key | `vision.describe_timeout_seconds` |
| REST field | `describe_timeout_seconds` under `vision` in `GET` / `PUT /api/config` (round-trips as `{"vision.describe_timeout_seconds": 15}`) |
| Label | Describe timeout (seconds) |
| Type | whole number, seconds |
| Default | 5 (effective value reported by `GET /api/config` when unset; 0 or empty = use the default) |
| Range | 1 or more to honor as-is; 0 or negative = 5-second default; sub-1, fractional or non-numeric writes are refused by the daemon — show the daemon's message |
| Placement | Vision / image-description settings, next to the rest of the `vision` block the app may already show (`enabled`, `model`, `max_image_bytes`) |

If the app team agrees the timeout (and the other `vision.*` keys) belong in the daemon's own surfaces only, record that decision as an intentional exclusion on the issue with the reasoning, and the parity gap is closed by decision instead of by the field.

### 4. Locale keys

If the app adds the pending string and the settings label above for the first time (rather than reusing an existing "upload in progress"-style string), send translations for them through the Compose Multiplatform pipeline and let the PWA bundles mirror them. The PWA already carries `image_send_pending` in all five locales with the values listed in "What changed in datawatch"; use it as the source wording for EN and ask the app for native DE, ES, FR and JA.

## Acceptance

- Sending an image shows a pending status until the daemon accepts the input or the ~15 s cap elapses; the composer stays interactive the whole time and nothing in the UI reads as an error.
- A message delivered in the bare `@<path>` fallback form renders as a tappable file reference marked delivered, on both platforms.
- The app can read and write `vision.describe_timeout_seconds` through the daemon's config endpoint (or the issue records the reasoned exclusion), and a refused write surfaces the daemon's message.
- Any new strings are localized in EN, DE, ES, FR and JA through the Compose Multiplatform pipeline, with the PWA bundles mirroring the app's translations.

## Filing record

Not yet filed. File as a datawatch-app issue with `gh issue create --repo dmz006/datawatch-app --body-file <this file, from "Parent release" onward>` and record the URL and date here.
