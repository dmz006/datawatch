# [parity] vision fallback: the bare `@path` reference ends in a deliberate space when the tag was last

**Filed 2026-11-08 as https://github.com/dmz006/datawatch-app/issues/399 (release v9.112.5).** Original note: File on `dmz006/datawatch-app` with `gh issue create` if/when that is wanted; nobody runs a hosting CLI from this draft and nothing is created on any host. Record the issue number and link in the Filing record at the end.

Parent release: the datawatch patch carrying the trailing-space rule (work found 2026-11-08, right after the vision describe timeout/fallback shipped; daemon version at planning is v9.109.0, change still under `[Unreleased]`).

## Affected area

The text form the daemon delivers when a session image send degrades to the bare file-reference fallback, on every surface that consumes or re-renders delivered session input: the PWA transcript, the Android and iPhone clients of `datawatch-app`, comm-channel clients that echo or diff session input, and any tooling (automation steps, MCP consumers, scripts) that matches the delivered text against the sent text. The daemon side — the expansion itself, the timeout, the ordering guarantee, the Settings field — is already on every daemon config surface; this issue is only about the one-byte-visible difference the fallback now has.

## What this work closes

Before, when the daemon degraded an `[image:<path>]` tag to the bare `@<path>` reference — no vision service, describe timeout, describe error, or empty description — the delivered text ended in the path with nothing after it *when the tag was the last thing in the message*. In sessions like Claude Code the `@<path>` reference opens a file completion as it is typed or rendered, and a completion left open spends the operator's first Enter on dismissal: the message that should have sent on one Enter needed two — one to close the completion, one to send. An operator who watched a slow describe fall back and then hit what looked like an unresponsive send was being cost an extra Enter by the daemon's own fallback text.

Now, on the datawatch daemon:

1. `expandImageTags` gives the bare `@<path>` reference exactly one trailing space when — and only when — the `[image:<path>]` tag was the last thing in the input. That space terminates the session's `@<path>` file reference, closes the completion, and a single Enter sends the message.
2. The space is strictly scoped: with the same tag twice, only the last occurrence takes it; a tag followed by any text is left as-is; the successful description form (`[image: <description> | path: <path>]`) carries no trailing space at all; and tags that pass through unchanged (unreadable files, paths the daemon will not serve) never got one. Every fallback path (no backend, timeout, error, empty description) yields the same end-of-text form.
3. The rule is pinned by a table test across tag positions and every fallback variant, including the success case asserting no trailing space, and the how-to `docs/howto/vision-image-descriptions.md` documents it in a Limits entry ("The trailing space belongs to the fallback, not to you") and in its topics.

So the operator-visible cost — the extra Enter after a slow describe — is closed on the daemon side, for every client that simply delivers the text.

## The parity gap this leaves open

- **The mobile-surface developer doc lags the delivered form.** `docs/api/mobile-surface.md`'s image-attachment section still describes the fallback as "a bare `@<path>`" with no word about the trailing space the text ends in when the tag was last. An app that detects "this message arrived in the fallback form" by exact string comparison of the sent text against the delivered text — for example, to mark a sent image as delivered, to re-link it to the attachment it sent, or to match its own optimistic transcript entry against the daemon's — will match every fallback except the last-tag one, and see the most common real-world case (operator attaches an image, app appends the tag at the end, describe times out) as unexplained drift.
- **Consumers that compare session input byte-for-byte** (automation steps that assert on delivered text, scripts that strip or re-pad it, a datawatch-app test fixture built from the pre-change form) will see one space where there used to be none. That byte is deliberate and stable, not padding to trim; nothing in the app-side documentation or test fixtures says so yet.
- **No consumer-side behavior is blocked.** The PWA renders the delivered text as-is and a trailing space is invisible; comm channels deliver it as-is. The gap is discovery and matching, not capability — the daemon's contract is unchanged in direction (a last-tag fallback ends in exactly one space; everything else ends in the path), and the how-to is the canonical record of it.

## Expected behavior

An operator or an app author who sends an image last in a message, sees the daemon deliver `@<path>` (with the extra space the text ends in), should be able to learn — from the app-side documentation — that the space is deliberate fallback formatting meant so that one Enter sends, not stray whitespace, that it appears only when the tag was the last thing in the text, and that any matching logic should be written to tolerate it (strip trailing whitespace before comparing, or match with a boundary-tolerant pattern) rather than exact-match the pre-change form.

## Suggested next step

Treat this as a low-priority follow-up in `datawatch-app`, not a blocker: no app behavior breaks, and the PWA needs no change. Concretely, (a) add one sentence to `docs/api/mobile-surface.md`'s image-attachment section — the fallback ends in exactly one trailing space of the daemon's when the tag was the last thing in the text, and any comparison of sent vs. delivered text should be whitespace-tolerant at the end — and (b) ask the app team to audit their own transcript-matching and optimistic-echo paths for exact comparisons on the delivered `@<path>` form, relaxing any to a trailing-whitespace-tolerant comparison; if their code already trims or matches by prefix, record that on the issue and close the gap as covered. If the app team can point to a concrete place an exact comparison exists and the space surfaces to the operator, promote the issue's priority for that one fix.

## Filing record

Not yet filed. No hosting CLI was run and no tracker issue was created; anyone filing this later records the URL and date here.
