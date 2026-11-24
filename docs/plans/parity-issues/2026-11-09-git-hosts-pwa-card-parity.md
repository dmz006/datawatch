# [sync] Git hosts: the PWA card lands — the parity gap now is the native clients and the card's missing signing fields

**Not filed yet.** — this is the tracked draft; the issue is opened when the
operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI (no `gh`, no
issue creation on a tracker) and creates nothing on any host.

**Work this draft captures** — the PWA slice of *Part of finishing review
and merge by the daemon* (Phase 4 of the autonomy handover plan, operator
2026-11-07: "b finsih now we need to be using it to debug it and monitor
its right."), PRD `7bd50001`. In this slice the web UI gained the Git hosts
card: the pure render-and-validate module `internal/server/web/app-git-hosts.js`
(`shouldShowGitHostsCard`, `renderHostRow`, `hostsFromConfig`,
`readinessRowsFromEntries`, `buildHostsPatch`, `hostTestPath`), its wiring
into the Settings → General tab of `internal/server/web/app.js`
(`loadGitHostsCard`, `renderGitHostsCard`, `addGitHost`, `removeGitHost`,
`testGitHost`, `saveGitHosts`) with the `gitHostsCard` container in
`internal/server/web/index.html`, the 47 `git_hosts_*` strings in all five
locale bundles (`internal/server/web/locales/{en,de,es,fr,ja}.json`) plus
the high-visibility guard keys added to
`internal/server/v5280_locales_test.go`, the behavior tests
`internal/server/web/app-git-hosts.test.js` and
`internal/server/web/app-git-hosts-settings.test.js`, and the
`docs/howto/connect-git-host.md` howto behind each row's **?** link.

## Affected area

The area is the last in-repo operator surface of the git-hosts parity set —
the PWA — for `git.hosts`, the daemon's named list of push targets that
Phase 4's push-and-tag loop reads. The config model, the readiness and
connection-test REST reads, and the REST/MCP/CLI/comm wirings already landed
and are recorded in
[2026-11-09-git-hosts-parity.md](./2026-11-09-git-hosts-parity.md) and
[2026-11-09-git-hosts-surface-parity.md](./2026-11-09-git-hosts-surface-parity.md).
That surface draft named the PWA "the tracked gap": no card, no readiness
view, no Test button, writes by hand-building a `PUT /api/config` body.

The **gap this work closes** is exactly that PWA cell. The card in
**Settings → General** now loads the list from `GET /api/config` and each
host's live readiness from `GET /api/git/hosts`, and shows per host: the
state (Ready / Not logged in / Status unknown), the account, the
repo-scope status, the daemon's plain-language problem and fix command when
there is one, and the editable fields — name, kind, base URL (shown only
where the kind has no public default), credential source, and the vault
secret **name** (shown only for `vault`). **Add host** appends a row,
**Remove** drops one, and **Save git hosts** validates the whole list
locally first — a failure names the host number and the reason (missing
name, duplicate name, missing base URL for that kind, missing vault secret
name) — and, when valid, writes the **entire list** in one
`PUT /api/config` under the `git.hosts` key, so a card save is a
whole-list write exactly like the YAML `hosts:` block; the dotted
`git.hosts.<name>.<field>` keys remain the field-edit write keys on the
same endpoint. **Test connection** per host calls
`POST /api/git/hosts/<name>/test` by the host's **saved** name and renders
`Connection OK` / `Connection failed` with the daemon's message — the same
answer every other channel gets. The card is safe by construction: if the
config never loads, the editor stays off, so a save can never overwrite
`git.hosts` with a list the operator never saw; and no token or secret value
is ever read into it — a vault host shows its secret name only, and any
problem text is redacted of token-shaped values and `user:password@`
credentials before it is shown. The strings run through the Localization
Rule: 47 `git_hosts_*` keys in each of the five bundles, with the
high-visibility ones (card title, row title, Test connection, the three
readiness states) pinned by `TestLocales_CommonNavKeysPresent` in
`internal/server/v5280_locales_test.go`. The howto behind the card's **?**
link is [docs/howto/connect-git-host.md](../../howto/connect-git-host.md);
the master table in [docs/howto/git-hosts.md](../../howto/git-hosts.md) now
lists the Web / PWA row as live.

The **gaps this work leaves** are outside this repo's web UI, plus two small
named ones on the card itself:

1. **The native clients.** Android and iPhone/iOS still have no model for
   `git.hosts`: a client that lists its configuration meets a block it does
   not render, and the card's capabilities — the list, the readiness view,
   the Test connection answer — have no native counterpart. This is the
   Mobile-Parity Rule's open item for this feature: capability parity is
   required, implementation parity is not.
2. **The card's signing fields.** The card has no `sign_tags`/`signing_key`
   inputs: both are preserved from the loaded config when an existing host
   is saved, and a host added from the card starts with signing off. They
   can still be set on the config file or a dotted-key write — the howto's
   "What is not here yet" item 4 states this — but a settings card that
   claims to edit the host and silently carries the two signing fields
   through is the seam an operator can trip over.
3. **The two channel seams the surface draft left.** The comm channel's
   connection test still rides a channel's REST passthrough where one exists
   (no dedicated test verb), and the CLI has no test subcommand (its
   readiness printing is the howto's stated answer, so optional). Neither
   is changed by the card; the card makes the test one click away on the
   PWA, which is what the table needs next.
4. **The capability gate.** `GET /api/git/hosts` and
   `POST /api/git/hosts/<name>/test` authenticate as config-read; the card,
   like every other surface, inherits that. Whether the connection test
   wants its own gate is the howto's review item — unchanged.

The daemon-side consumer gaps — the merge loop never reaching
`Runner.Hosts`, the unsigned `git tag -a` in the ship path, the uncalled
release publish, the GitLab mint — stay with
[2026-11-09-daemon-merge-ship-host-parity.md](./2026-11-09-daemon-merge-ship-host-parity.md),
and the per-kind capability floor stays with
[2026-11-09-git-provider-capability-parity.md](./2026-11-09-git-provider-capability-parity.md).
This draft deliberately does not duplicate either.

## Current behavior (the parity position per surface, as it stands now)

| Surface | Read the hosts | Test one host | Write the hosts |
| --- | --- | --- | --- |
| YAML/config | `git:` → `hosts:` in the config file | — | the config file, whole list (create/rename/remove) |
| REST | `GET /api/git/hosts` (live readiness), `GET /api/config` (raw block) | `POST /api/git/hosts/{name}/test` | `PUT /api/config`: `git.hosts` (whole list) or dotted `git.hosts.<name>.<field>` keys — atomic, validated |
| MCP | `git_hosts_status` (readiness), or `get_config` `section: git` | `git_hosts_test` (`name`) | — by design (config writes stay operator surfaces) |
| CLI | `datawatch git-hosts` (daemon's list with readiness), `datawatch config get git` | readiness printed by `datawatch git-hosts` and after credential sets; no test subcommand | `datawatch config set git.hosts.<name>.<field> <value>` (echoes the key, never the value) |
| comm channel | `configure git.hosts list` (name, kind, credential source, vault entry name) | via the channel's REST passthrough where one exists | `configure git.hosts.<name>.<field>=<value>` (reply: key only, never value) |
| **PWA — closed by this work** | the **Git hosts card** in Settings → General: `GET /api/config` with defaults applied plus the live readiness of every host from `GET /api/git/hosts`; per host: state, account, repo-scope status, problem and fix command | the card's **Test connection** button — `POST /api/git/hosts/<name>/test` for the host's **saved** name (a new or renamed host must be saved first); renders `ok` plus the daemon's plain-language message | the card's **Add host** / **Remove** / **Save git hosts** — local whole-list validation, then a whole-list write via `PUT /api/config` (`git.hosts`); the editor stays off if the config never loads, so a save can never overwrite a list the operator never saw |
| Android, iPhone/iOS | nothing renders the `git.hosts` block | — | — (the tracked gap this draft names) |

The token rule now holds on the card the same way it holds on the daemon
side: a vault host is shown by its secret **name** only; readiness and test
results carry the account, scope names, the problem and the fix command —
never a secret value; and any token-shaped value or `user:password@`
credential in a base URL is redacted before it reaches a row. Every label
the card prints is a `git_hosts_*` key present in all five bundles.

## Expected behavior

Once the remaining parity is met:

- **Android and iPhone** — capability parity with the PWA card: the host
  list with its readiness (state, account, scope status, problem and fix
  command), the Test connection answer per host, and the host editing the
  card offers (name, kind, base URL, credential source, vault secret name;
  save as a whole-list write; new or renamed host saved before it can be
  tested), each platform idiomatic, driven by the same two reads and the
  same `PUT /api/config` write; implementation parity is not required per
  the Mobile-Parity Rule. Until it lands, each client must at minimum not
  misrender or drop the `git.hosts` block it meets under `GET /api/config`,
  and never surface a vault secret name as a value.
- **The card's signing fields** — either `sign_tags`/`signing_key` become
  visible inputs on the card (a boolean and a key id; validation already
  exists on the daemon, `sign_tags` coerced to a JSON boolean), or the card
  and the howto both say plainly what they say today: signing fields are
  preserved, a new host starts with signing off, set them on the config
  file or a field write. Both answers are honest; the requirement is that
  the card never edits a host while silently dropping a field's value.
- **The two seams** — either a dedicated comm test verb (so "is my host
  logged in, and can it reach the host?" is a chat question on every
  channel) and, if wanted, a `datawatch git-hosts test <name>` subcommand —
  or the howto keeps naming the test's live routes (MCP, REST, the PWA
  card, the CLI's readiness output, the channel's REST passthrough). The
  requirement is that the table stays true.
- **The drafts stay split** — filing the issue this draft names must not
  pull in the daemon push-and-tag wiring (daemon-merge-ship draft), the
  per-kind capability floor (capability draft), the GitLab mint, or the
  capability-gate review item, which keep their own drafts.

## Suggested next step

Per the Mobile-Parity Rule, file (or comment on an existing) `datawatch-app`
issue for the native clients covering capability parity with the Git hosts
card — the list with readiness, the Test connection answer, and the card's
editing set — referencing this draft, the two REST reads
(`GET /api/git/hosts`, `POST /api/git/hosts/<name>/test`), the
`PUT /api/config` `git.hosts` whole-list write, and
[docs/howto/connect-git-host.md](../../howto/connect-git-host.md) as the
word-for-word source for the shapes and the token rule; acceptance is a
native screen rendering a seeded host list (one host with a missing-scope
readiness problem, one vault host shown by entry name only, one plain-git
host) and a pass-through of one successful and one failed test response,
with no string that could be a secret value. Then update the two sibling
drafts to close the PWA cell:
[2026-11-09-git-hosts-surface-parity.md](./2026-11-09-git-hosts-surface-parity.md)'s
"the PWA has no Git hosts settings card" gap paragraph and its
`feat(pwa):` card issue scope are superseded for the PWA itself by what
landed — that issue is now the native clients plus the two optional seams;
and
[2026-11-09-git-hosts-parity.md](./2026-11-09-git-hosts-parity.md)'s PWA
row ("raw block under `GET /api/config` only") is replaced by the live card
row. Fold the card's `sign_tags`/`signing_key` question into whichever is
cheaper — a small in-repo `feat(pwa):` issue or a stated howto position —
so the card and the docs agree on what the card edits, and line the
howto's CLI-echo wording ("echoes the key, never the value") up with what
`datawatch config set` actually prints. Nothing here changes a REST or MCP
contract an existing client parses: the card reads and writes endpoints and
keys the other surfaces already use, and the strings ride the locale
bundles.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| PWA | included — closed | The card in Settings → General carries the list, the live readiness, the Test connection button and the whole-list save, in all five locales, with the token rule and the load-before-edit guard. |
| REST | included | unchanged — already landed; the card's two reads and its write are the same endpoints the REST row lists. |
| MCP | included | unchanged — `git_hosts_status` / `git_hosts_test` as landed; no set tool by design. |
| CLI | included | unchanged — `datawatch git-hosts` and the dotted-key set as landed; the test subcommand remains optional. |
| comm channel | included | unchanged — list and dotted-key set as landed; the dedicated test verb remains the optional seam. |
| YAML/config | included | unchanged: the config file remains the whole-list write surface alongside the card and the `git.hosts` key on `PUT /api/config`. |
| Android | excluded (tracked) | No app code in this repo; capability parity with the card is the Mobile-Parity Rule item this draft's issue names. |
| iPhone/iOS | excluded (tracked) | Same as Android: tracked for the native client, idiomatic delivery, no implementation parity. |
