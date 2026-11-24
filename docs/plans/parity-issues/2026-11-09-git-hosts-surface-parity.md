# [sync] Git hosts: operator-surface parity closed (REST write, MCP, CLI, comm) — what is left is the PWA card and the mobile clients

**Not filed yet.** — this is the tracked draft; the issue is opened when the
operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI and creates
nothing on any host.

**Work this draft captures** — the four operator-surface wirings that land on
top of the git-hosts slice of *Part of finishing review and merge by the
daemon* (Phase 4 of the autonomy handover plan, operator 2026-11-07: "b
finsih now we need to be using it to debug it and monitor its right."), PRD
`7bd50001`: git host keys exposed through `GET` and `PUT /api/config`
(`internal/server/api.go`, `internal/server/api_git_hosts_config_test.go`),
the comm-channel `configure git.hosts list` and
`configure git.hosts.<name>.<field>=<value>` verbs
(`internal/router/git_hosts_comm.go`, registered in
`internal/router/router.go`), the CLI's `datawatch git-hosts` list and the
dotted-key `datawatch config set` of git host fields
(`cmd/datawatch/cli_git_hosts.go`), and the two MCP tools `git_hosts_status`
and `git_hosts_test` (`internal/mcp/git_hosts_tools.go`, registered in
`internal/mcp/server.go`).

## Affected area

The area is the set of operator surfaces that can read and write the
daemon's configured git hosts (`git.hosts` — the named list of push targets
the Phase 4 push-and-tag loop reads) and that can check one host's readiness
or run its connection test. Until this work, that set was REST-plus-file
only: [2026-11-09-git-hosts-parity.md](./2026-11-09-git-hosts-parity.md)
recorded the MCP, CLI and comm cells as "partial — tracked", and the howto
([docs/howto/git-hosts.md](../../howto/git-hosts.md)) said the same.

The **gaps this work closes** are those four cells:

- **REST write** — `PUT /api/config` now carries both write shapes: the
  whole list under the `git.hosts` key (a JSON array — the only way to
  create, rename or remove a host) and the dotted per-field keys
  `git.hosts.<name>.<field>`. The patch is validated by simulating it over
  the whole list and applied once, atomically, through
  `config.ValidateGitHostsPatch`/`SetGitHostField`
  (`internal/server/api.go`, `internal/config/git_hosts.go`): a value that
  would leave the list inconsistent is refused whole and the stored config
  is unchanged, so a single write can set `credential_source: vault` together
  with its `vault_secret` and nothing half-applied survives a rejection.
- **MCP** — `git_hosts_status` (no arguments) returns every configured host
  with its readiness, the same data as `GET /api/git/hosts`; `git_hosts_test`
  (argument `name`) runs the connection test; unknown names are a tool error.
  Both tools proxy the two REST endpoints, so they inherit the token rule: the
  tools return the account, the scope names and a vault secret **name**,
  never a secret value. MCP deliberately has no set tool — config writes
  stay operator surfaces.
- **CLI** — `datawatch git-hosts` lists every configured host with its
  readiness, read from the running daemon (the source of truth for the
  applied config); `datawatch config set git.hosts.<name>.<field> <value>`
  writes one field through the daemon and confirms with `Set <key> = <value>`
  on the operator's own terminal (it does not ride into any channel), and
  after a set of `credential_source` or `vault_secret` it prints
  that host's readiness result, so a botched rotation is visible in the same
  command. A rejected patch comes back whole. `sign_tags` is coerced to a
  JSON boolean on the wire; every other field rides as a string.
- **comm channel** — `configure git.hosts list` names every host's kind,
  credential source and (for vault hosts) the vault entry name;
  `configure git.hosts.<name>.<field>=<value>` applies the field through the
  same daemon configure surface. The reply echoes only the key — never the
  value — so a secret is never mirrored into the chat channel (unlike the
  CLI's local confirm line, which prints the value into the operator's
  terminal only).

The **gap this work leaves** is now smaller and named in one place: the
operator-facing *clients*. The PWA has no Git hosts settings card —
`internal/server/web/app.js` carries no git-hosts code at all — so on the
PWA the hosts are still a raw block under `GET /api/config` with `POST
/api/git/hosts/<name>/test` available to nobody as a button, and host writes
require hand-building a `PUT /api/config` body. The Android and iPhone
clients are in the same position one level down: no card, no readiness view,
no test, nothing to render. Two smaller seams remain on the wired surfaces:
the comm channel runs the connection test only over a channel's REST
passthrough where it has one (no dedicated test verb), and the CLI has no
test subcommand (readiness is printed after credential-source sets and by
`datawatch git-hosts`, which is the howto's stated answer, not necessarily a
gap). The daemon-side consumer gaps — the push-and-tag loop not consuming
the list, signed tags unsigned in the ship path, the repo CI watch's
host-path actions — are recorded in
[2026-11-09-daemon-merge-ship-host-parity.md](./2026-11-09-daemon-merge-ship-host-parity.md),
and the per-kind capability floor (which host kinds answer which
questions) in
[2026-11-09-git-provider-capability-parity.md](./2026-11-09-git-provider-capability-parity.md);
this draft deliberately does not duplicate either.

## Current behavior (the parity position per surface, as it stands now)

| Surface | Read the hosts | Test one host | Write the hosts |
| --- | --- | --- | --- |
| YAML/config | `git:` → `hosts:` in the config file | — | the config file, whole list (create/rename/remove) |
| REST | `GET /api/git/hosts` (live readiness), `GET /api/config` (raw block) | `POST /api/git/hosts/{name}/test` | `PUT /api/config`: `git.hosts` (whole list) or dotted `git.hosts.<name>.<field>` keys — atomic, validated, single-write vault pairing possible |
| MCP | `git_hosts_status` (readiness), or `get_config` `section: git` | `git_hosts_test` (`name`) | — by design (config writes stay operator surfaces) |
| CLI | `datawatch git-hosts` (daemon's list with readiness), `datawatch config get git` | readiness printed by `datawatch git-hosts` and after credential sets; no test subcommand | `datawatch config set git.hosts.<name>.<field> <value>` (confirm line: `Set <key> = <value>` in the local terminal only) |
| comm channel | `configure git.hosts list` (name, kind, credential source, vault entry name) | via the channel's REST passthrough where one exists | `configure git.hosts.<name>.<field>=<value>` (reply: key only, never value) |
| PWA | the raw block under `GET /api/config` | — | — (the card is the tracked gap) |
| Android, iPhone/iOS | nothing renders the block | — | — (the tracked gap) |

The token rule holds on every server-side surface: the list rows carry
kind, credential source and at most a vault secret **name**; readiness
carries the account, scope names, the problem and the fix command; the test
carries `ok`, `account`, a plain-language message and `fix_command` — never
a secret value. On the daemon side the write confirmations never print a
secret (the comm reply echoes the key only); the CLI's local `Set <key> =
<value>` line only reaches the operator's own terminal — the howto's CLI
parity row says "echoes the key, never the value", one wording to line up
with the code when the card issue is filed. The
howto's parity table and its "What is not here yet" list
([docs/howto/git-hosts.md](../../howto/git-hosts.md)) now state exactly
this table; its item 4 (the PWA card) remains the open client-side entry,
and item 1 (the loop that consumes `git.hosts`) is the daemon-merge-ship
draft's subject.

## Expected behavior

Once client-side parity is met:

- **PWA** — a Git hosts settings card lists every configured host with its
  kind, credential source (a vault host by its secret **name** only) and
  readiness state, with a Test connection button per host calling
  `POST /api/git/hosts/<name>/test` and rendering `ok`, `account`,
  `message` and `fix_command`. Host writes follow the same discipline as
  the wired surfaces: field edits through `PUT /api/config` dotted keys,
  create/rename/remove through a whole-list `git.hosts` write, a rejected
  patch surfaced whole, and no response field ever rendered as a possible
  token (a secret name shown as a name, a `[redacted]` marker shown as a
  marker). New strings go through the localization rule (five locale
  bundles) and the Mobile-Parity Rule's app-issue step.
- **Android and iPhone** — capability parity with that card: the list, the
  readiness view and the Test button, each platform idiomatic, driven by
  the same two REST endpoints; implementation parity is not required. Until
  then each client must at minimum not misrender or drop the `git.hosts`
  block it meets under `GET /api/config`.
- **The two seams** — either a dedicated comm test verb (so "is my host
  logged in, and can it reach the host?" is a chat question on every
  channel) and, if wanted, a `datawatch git-hosts test <name>` subcommand —
  or the howto keeps saying, as it says today, that the test runs from MCP,
  REST, the CLI's readiness output or the channel's REST passthrough. Both
  answers are honest; the requirement is that the table stays true.
- **The drafts stay split** — filing the surface this draft names must not
  pull in the daemon push-and-tag wiring (daemon-merge-ship draft), the
  per-kind capability floor (capability draft), the registry row, the
  broker's GitLab mint or the token-mint questions, which keep their own
  drafts.

## Suggested next step

File one issue in this repository, `feat(pwa):`-prefixed, covering the Git
hosts settings card (list with readiness, Test connection button, dotted-key
field write, whole-list create/rename/remove, the five-locale strings),
referencing this draft, the two REST endpoints and
`docs/howto/git-hosts.md` as the word-for-word source for the shapes and
the token rule; acceptance is the card rendering a seeded host list (one
host with a missing-scope readiness problem, one vault host shown by entry
name only, one plain-git host) and a pass-through of one successful and one
failed test response, with no string in the DOM that could be a secret
value. Fold the comm test verb and the optional CLI test subcommand into the
same issue or a follow-up of it, whichever is cheaper, so the "how do I test
a host" answer stays one line in the howto, and line up the howto's CLI-echo
wording ("key only, never value") with what `datawatch config set` actually
prints. When the card lands,
file/comment the datawatch-app issue per the Mobile-Parity Rule and update
its sibling draft
([2026-11-09-git-hosts-parity.md](./2026-11-09-git-hosts-parity.md)) — whose
per-surface table this draft supersedes for the operator surfaces — to close
its MCP/CLI/comm "tracked" cells. Nothing here changes a REST or MCP
contract an existing client parses: everything in the closed cells is
additive on endpoints and tools clients already receive, and the card work
is display and settings, not integration.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | included — closed | `PUT /api/config` now carries both write shapes atomically; the two reads and the test are unchanged and already landed. |
| MCP | included — closed | `git_hosts_status` and `git_hosts_test` proxy the two REST endpoints and inherit their token rule; no set tool by design. |
| CLI | included — closed | `datawatch git-hosts` (list with readiness) and the dotted-key set with readiness-after on credential changes; a test subcommand is the only optional addition. |
| comm channel | included — closed with a seam | list and dotted-key field set land; the connection test still rides a channel's REST passthrough where one exists — a dedicated verb is optional. |
| YAML/config | included | unchanged: the config file remains the whole-list write surface (create/rename/remove) alongside `git.hosts` on `PUT /api/config`. |
| PWA | included — the tracked gap | No card, readiness view or test button yet; raw block plus hand-built writes. Exactly what the draft's issue names. |
| Android | excluded (tracked) | No app code in this repo; capability parity with the PWA card is required and is tracked for the datawatch-app issue. |
| iPhone/iOS | excluded (tracked) | Same as Android: tracked for the native client, idiomatic delivery, no implementation parity. |
