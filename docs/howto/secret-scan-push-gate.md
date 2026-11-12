---
docs:
  index: true
  topics: [git, push, secret scan, pre-push gate, detectors, allow-list, shadow mode, enforce, false positive, fingerprint]
exec_steps:
  - tool: secret_scan_config_get
    description: Read the current pre-push secret scan config: effective mode, detector list, and the allow-list (paths + fingerprints)
    args: {}
    read_only: true
  - tool: get_alerts
    description: Find recorded shadow findings and enforce refusals — warn alerts titled "…pre-push secret finding(s) (shadow mode)" or "…not pushed", body is the redacted findings summary
    args: {}
    read_only: true
  - tool: secret_scan_config_set
    description: Allow-list a false positive by path or fingerprint (clears that finding), and/or set the mode (off/shadow/enforce). Takes effect on the next push attempt, no restart needed
    args: {mode: "{{params.mode}}", allow_list_paths: "{{params.allow_list_paths}}", allow_list_fingerprints: "{{params.allow_list_fingerprints}}"}
    read_only: false
---

# Secret scan push gate

The **secret scan push gate** is the daemon's pre-push check for secret
material. Before the daemon pushes anything — PRD push-on-complete, a
one-shot session's completion push, or the pre-teardown save push — it scans
the commits that are about to be pushed and, per the configured **mode**,
either records the findings (shadow) or refuses the push (enforce).

It is on by default, in **shadow** mode: a fresh install scans, records and
alerts on findings, and never refuses a push.

## What is scanned

For each push attempt to a remote (default `origin`):

- **The commits** on the current branch (`HEAD`) that are present on **no
  ref of that remote** — the exact set the push would deliver. Commits
  already on the remote are never re-scanned, and neither is a commit that
  was pushed on an earlier attempt.
- **The lines those commits *add***. A finding is a line added by one of
  those commits that matches a detector — with the file path and the
  line number in that commit's version of the file.
- **The built-in detectors**, unless a shorter list is configured:

| Detector | Matches |
|---|---|
| `private_key` | PEM private key blocks (`-----BEGIN … PRIVATE KEY…-----`) |
| `aws_access_key` | AWS access key IDs (`AKIA`/`ASIA`/`ABIA`/`ACCA`/`A3T…` + 16 chars) |
| `github_token` | GitHub tokens (`ghp_`, `gho_`, `ghu_`, `ghs_`, `ghr_` + 20–255 chars) |
| `generic_token` | High-entropy values assigned to key-ish names (`api_key`, `token`, `secret`, `password`, …); an entropy gate keeps obvious placeholders like `aaaaaaaaaaaaaaaa` from matching |
| `slack_token` | Slack tokens (`xoxb-`, `xoxp-`, …) |

An **unknown detector name is a hard error at scan time**, not a silent
no-op: the scan fails, is logged, and the push proceeds. Because of that,
check the daemon log if you set a detector list and expected findings are
missing — a typo in the list disables the scan rather than failing the
push.

## Modes, and the shadow-first rollout

`git.secret_scan.mode` decides what a push does with a finding:

| Mode | Behaviour |
|---|---|
| `off` | No scan at all; the push proceeds exactly as the gate did not exist. |
| `shadow` **(default; an unset mode reads shadow)** | The scan runs. Findings are logged, recorded on the PRD/session and one warn alert is raised — then the push proceeds. |
| `enforce` | Any finding that is not allow-listed **refuses the push before it runs**. The remote is left untouched and the local commit is kept exactly where it is. |

A value other than `off`, `shadow` or `enforce` is **refused on every write
surface** (REST, MCP, comm/CLI) with an error naming the allowed values —
it can never be stored.

**Shadow-first rollout.** The safe order to bring the gate into a new
repository:

1. **Start in `shadow`** (the default). Nothing is blocked; you only learn
   what the gate would have blocked.
2. **Read the findings** — the alert dock, the PRD's push warning and
   decisions list (see *Where findings are recorded*).
3. **Clear the false positives** into the allow-list (paths or
   fingerprints, see *Clearing a false positive*).
4. **Switch to `enforce`** once every recorded finding is either real (the
   secret is rotated and the line removed) or allow-listed. A config change
   is read live on every push attempt — it applies on the **next push, no
   restart needed**.

## Configuring

In the daemon config (e.g. `~/.datawatch/config.yaml`):

```yaml
git:
  secret_scan:
    mode: shadow                       # off | shadow | enforce (unset = shadow)
    detectors:                         # empty/omitted = all built-ins
      - private_key
      - aws_access_key
      - github_token
      - generic_token
      - slack_token
    allow_list:
      paths:                           # path globs; matches are skipped entirely
        - vendor                       # a bare directory name matches any path inside it
        - testdata
        - "fixtures/*"                 # or a glob matched against the repository-relative path
      fingerprints:                    # 16-hex short or 64-hex full sha256 of the matched material
        - "3f9c2ab041de77a5"
```

Key behaviour:

- `mode` unset or empty reads as `shadow` (the first-release default).
- `detectors` empty or omitted reads as **all built-ins**; an empty value
  written at runtime resets it to all built-ins.
- `allow_list.paths` are matched with `path.Match` against the
  repository-relative path, and a **bare directory name** (e.g. `vendor`)
  also matches any path *inside* that directory — the usual intent.
- `allow_list.fingerprints` are the fingerprints a finding carries: the
  **short 16-hex** form (shown on every finding) or the **full 64-hex**
  sha256 digest of the matched material. Each entry must be 16- or
  64-char hex — the write paths validate and reject anything else.
  Only the hash is stored; the secret itself is never written to the
  allow-list.

Every write surface works and takes effect on the next push attempt:

| Surface | How |
|---|---|
| config file | the `git.secret_scan` block above |
| REST | `PUT /api/config` with key `git.secret_scan.mode`, `git.secret_scan.detectors`, `git.secret_scan.allow_list.paths`, `git.secret_scan.allow_list.fingerprints` |
| MCP | `secret_scan_config_get` (read) and `secret_scan_config_set` (write; requires `mcp.allow_self_config: true`); the generic `config_set` with the same keys too |
| comm / CLI | `datawatch configure git.secret_scan.mode=enforce` |

## What a refusal looks like (enforce)

In `enforce` mode, when a finding is not allow-listed the push is **not
attempted at all**:

- the remote is untouched, and the local commit stays on the branch —
  nothing is rolled back or rewritten;
- the recorded reason has the shape

  ```
  secret scan (enforce mode): 2 secret finding(s): private_key keys/prod.example.pem:1 (fingerprint 3f9c2ab041de77a5); github_token .env.example:12 (fingerprint 9d41be07aa12c8e3) refuses the push to origin/feature/example
  ```

- on **PRD work** the PRD record shows `push_state: not_pushed`, the same
  redacted text in `push_warning`, a `git_not_pushed` decision row with it
  as the note, and one operator alert at **warn** level
  ("Automaton \<id\> not pushed", reason as body);
- on a **one-shot session** the session record shows `push_state:
  not_pushed` plus the `push_warning`, and one warn alert as well.

In `shadow` mode the same finding **does not block**: the push proceeds,
the PRD gains a `git_secret_scan_shadow` decision row with the redacted
summary as its note, the session record's `push_warning` reads "pushed;
pre-push secret scan (shadow mode) reported: …", and one warn alert is
raised ("… N pre-push secret finding(s) (shadow mode)").

## Where recorded findings are

A finding carries exactly five fields — **detector, commit SHA, file path,
line number, and the short 16-hex fingerprint** — and those are the only
fields that appear anywhere:

1. **The alert dock** (and REST `GET /api/alerts`): one warn alert per push
   attempt with findings; the body is the redacted summary (count, then per
   finding detector, `path:line`, fingerprint).
2. **The PRD record**: `git.push_warning` holds the last attempt's summary
   and the decisions list carries the `git_secret_scan_shadow` /
   `git_not_pushed` rows, newest last.
3. **The session record**: the `push_warning` field (one-shot and teardown
   pushes).
4. **The daemon log**: the same redacted summary on the scan, refusal and
   recording lines.

## Clearing a false positive

Two allow-list entries, two different scopes:

- **A path** (`allow_list.paths`) — the whole file or directory is never
  scanned. Right for vendored code and test fixture trees, e.g. `vendor`
  or `testdata`. Anything added under that path later stays exempt.
- **A fingerprint** (`allow_list.fingerprints`) — only *that one match* is
  suppressed. Right for a single known-benign value (a public demo key, a
  committed test token). The fingerprint is the hash of the exact matched
  material: if the material changes, the fingerprint changes and the
  finding comes back.

Steps:

1. Read the finding (alert, PRD, or session record) and copy its
   **16-hex fingerprint** — or note the file path.
2. Add it, e.g. via any write surface:
   `datawatch configure git.secret_scan.allow_list.fingerprints=3f9c2ab041de77a5`
   or `datawatch configure git.secret_scan.allow_list.paths=testdata`
   (comma-separated lists append the standard way the config patch splits
   CSV values; the MCP `secret_scan_config_set` tool takes the same fields).
3. Push again. The commits are still unpushed, so they are scanned again —
   and the allow-listed finding is now dropped, while every other finding
   still surfaces. Note that writing one of these list keys **replaces**
   the current list: when you add an entry, pass the existing ones along
   with it (the MCP `secret_scan_config_set` tool, by contrast, leaves a
   field you don't provide untouched).

Prefer the narrowest entry that works: a fingerprint exempts one line, a
path exempts a tree.

## What the gate never does

- **Never holds or prints the matched secret.** The finding struct has no
  field for it, and the summary rendered into logs, alerts, PRD and
  session records only reuses the redacted fields (detector, commit,
  `path:line`, fingerprint). The allow-list stores hashes only.
- **Never touches the commit.** A refusal is a refusal: no rollback, no
  amend, no force-push; the local branch is exactly where it was.
- **Never wedges a legitimate push on a scanner fault.** If the scan
  itself errors (for example, an unknown detector name), the error is
  logged and the push proceeds — no finding is ever invented from an error.
  The trade-off to remember: with a broken detector list the gate
  silently stops finding things, so after changing `detectors`, confirm
  the expected findings still appear (or the log is quiet *and* the list
  is right).
- **Never scans pushed history.** Only lines added by commits not yet on
  the remote; once a commit is pushed it is out of scope.

See also: [`secrets-manager.md`](secrets-manager.md) (where secrets should
live) and
[`alerts-and-notifications.md`](alerts-and-notifications.md) (the alert
dock that carries the findings).
