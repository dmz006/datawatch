# Local-environment leak check (whole tree)

A **local-environment leak** is a value tied to one operator's machine that
has landed in a tracked file: a private or internal IP address, an absolute
home-directory path, a personal email address, or a machine-specific token
(a hostname, an internal registry host, a kubectl context, a Wi-Fi SSID).
This check is the rule in `AGENT.md` ("No local-environment leaks in git")
enforced automatically, across the **whole tracked tree at once** — docs,
plan files, config samples, Helm charts, deployment manifests, test fixtures
and Go source comments alike.

It is a development tool for the repository, not a datawatch daemon feature:
the daemon does not run it, it has no setting, and nothing in the web app,
the API or a chat channel shows its result. It runs at commit time (a
pre-commit hook), inside `make lint`, and before a release is cut.

The check only reads files. It never prints the matched value.

## What the check covers

Four classes, each with its own configuration under `scripts/`:

| Class   | What it flags | Configuration |
|---------|---------------|---------------|
| `address` | private / internal / link-local IPv4, unique-local (ULA) and tailnet IPv6, and MagicDNS hostnames | `scripts/leak-check.patterns` (generic patterns) and `scripts/leak-check.allowed-addresses` (reserved documentation ranges and loopback pass) |
| `home` | absolute home-directory paths for **any** user, not just yours | `scripts/leak-check.patterns`; placeholder paths pass |
| `email` | every address not covered by the allowed-email glob list | `scripts/leak-check.allowed-emails` (e.g. `@example.com`) |
| `local` | any fixed string from your own machine-specific token list | `.leak-patterns.local` at the repository root (gitignored, never committed) |

The generic patterns are configuration, not code. They are written
**self-clean**: dots inside patterns are bracketed (`10[.]0[.]…`) so the
pattern file itself never matches its own patterns. Keep it that way when you
edit it.

## Run it

```bash
# from anywhere in the repository
make check-leaks
# or directly:
bash scripts/check-no-local-leaks.sh            # scans the repository root
bash scripts/check-no-local-leaks.sh some/dir   # scan another directory
```

In a git work tree the **tracked** files are scanned (`git ls-files`), so
untracked scratch does not block you; elsewhere the directory is walked with
`find`, skipping `.git`. Binary files and paths matching a glob in
`scripts/leak-check.exclude` are skipped.

### Output and exit codes

```text
docs/plans/notes.md:42: address
Makefile:118: home
local patterns: skipped        # stderr, only when the local file is absent/empty
address: 1
home: 1
```

- One line per finding: `path:line: class` (path relative to the scanned
  root), then a per-class count summary — **and nothing else**. The report
  never contains the matched text; candidate matches are extracted in memory
  purely to test them and discarded. This is deliberate: the report is
  CI-safe and safe to paste into a commit message or CI log. Keep the same
  discipline manually — name the file and the line, never the value.
- Exit `0` clean, `1` findings reported, `2` usage or configuration error.

### Environment overrides

Each is resolved relative to the script's location, never relative to the
scanned root:

| Variable | Meaning | Default |
|----------|---------|---------|
| `LEAK_CHECK_CLASSES` | comma-separated subset of `address,home,email,local` (empty/unset = all) | all |
| `LEAK_CHECK_PATTERNS` | class patterns file | `scripts/leak-check.patterns` |
| `LEAK_CHECK_ALLOWED_EMAILS` | allowed-email glob list | `scripts/leak-check.allowed-emails` |
| `LEAK_CHECK_EXCLUDE` | exclude glob list | `scripts/leak-check.exclude` |
| `LEAK_CHECK_LOCAL` | local fixed-string list | `<root>/.leak-patterns.local` |

## The generated reports folder scan

The whole-tree check scans **tracked** files, and the generated reports
under `docs/plans/reports` are largely *not* tracked: they are session
artefacts — plan-coverage gate outputs, final-gate reports and similar —
written by sessions that may have picked up values from the operator's own
environment (a hostname inside a quoted path, a private address in a log
excerpt, a home-directory path pasted from real output). Untracked files are
exactly the surface the tracked-tree scan cannot see, and they are where a
generated report leaks from, so the reports folder gets its own scan over
the same four classes.

`make check-generated-reports-leaks` runs
`scripts/check-generated-reports-leaks.sh [folder]` (the folder defaults to
`docs/plans/reports`):

- It collects **every file under the folder** — tracked (`git ls-files`) and
  untracked non-ignored (`git ls-files --others --exclude-standard`) — and
  copies them into a throwaway directory (`mktemp -d`, removed on exit),
  keeping the repository-relative structure.
- It hands that directory to the same `check-no-local-leaks.sh` machinery,
  with `LEAK_CHECK_LOCAL` pointing at **this repository's**
  `.leak-patterns.local`, so the `local` class runs against your fixed
  strings even though the scan root is the throwaway copy. If the file is
  absent the class is skipped with a stderr notice — never an error.
- A tracked file that was deleted from the working tree has no content left
  to leak: removing a leak is never blocked.

```bash
make check-generated-reports-leaks
# or directly, for another folder:
bash scripts/check-generated-reports-leaks.sh docs/plans/reports
```

It is a step of `make check-leaks` (after the whole-tree check), so a
failing lint means a leak finding until you act on the report. The
pre-commit chain and `scripts/release-smoke.sh` run the whole-tree check
only, not this scan — the reports folder is generated session output, not
part of the tracked tree they guard. The scan can also be run on its own
as above, and `make lint` is the gate that covers it.

### Reading the report

Same shape as the whole-tree check, with the paths being the real ones
(because the throwaway copy keeps the repository-relative structure):

```text
docs/plans/reports/18970001-gate.md:42: address
local patterns: skipped        # stderr, only when .leak-patterns.local is absent
address: 1
```

One line per finding — `path:line: class`, path relative to the repository
root — then a per-class count summary, and nothing else. The matched text is
never printed, so the output is CI-safe and safe to paste, exactly like the
tree check. Exit `0` clean, `1` findings reported, `2` usage or
configuration error.

Fix a finding by the same routes as the tree check (placeholder, operator
config, environment variable, or `scripts/scrub-peer-names.sh`) — or simply
discard the stale report. The report is generated output, so cleaning the
value at the source (the generator or the session that wrote it) is the
durable fix.

### Host names in session ids are already stripped

A second line of defence is built into the report generators themselves:
session ids in generated reports no longer carry a host prefix. An id of
the form `<host>-<id>` or `<host>:<id>` is reduced to its final segment
after the last `-` or `:`, and a test renders every report kind with a fake
host name and asserts the host does not appear anywhere in the output. The
scan above still runs because a report can carry values the generator does
not scrub (log excerpts, quoted paths, free-text notes) — the two layers
cover different material.

## The local patterns file

`.leak-patterns.local` is where you keep tokens that are too private to put
in the checked-in `scripts/leak-check.patterns` — a personal hostname, an
internal registry host, a kubectl context name, a private Wi-Fi SSID.

- It lives at the **repository root**, is gitignored and must never be
  committed.
- Format: one fixed-string pattern per line; matching is **case-insensitive
  substring** matching against every scanned file; blank lines and `#`
  comments are ignored. A finding from this class is reported as
  `path:line: local` — the matched token is never printed.
- If the file is absent or holds only comments and blank lines the check
  prints `local patterns: skipped` (on stderr) and carries on. **That is
  never an error** — the absence only skips that one class.

### Seeding it from the local daemon

`make leak-patterns-init` runs `scripts/seed-leak-patterns.sh`:

- When the file is absent it is created from
  `scripts/leak-patterns.local.template`. An existing file is **never
  overwritten**; only names missing from it are appended, one per line,
  beneath a `# seeded peer names` comment marker.
- The seeded names are the ones your local datawatch daemon knows:
  federation peers, compute nodes, observer peers and pool hosts.
- A candidate name is **never seeded** when it is shorter than 5 characters
  or matches a whole word in the tracked generic-name stoplist
  `scripts/leak-patterns.skip` (such names would match thousands of ordinary
  lines). Only a count is printed — `seeded N names`, `skipped N generic
  names` — never a name.
- When the daemon is not reachable nothing is seeded, a notice is printed and
  the script exits `0`.

You can also create the file by hand: copy `scripts/leak-patterns.local.template`
to `.leak-patterns.local` at the repository root and add your patterns. Do
not commit the result.

## When the check finds something

A finding is `path:line: class`. Go to the line and choose one of the
prescribed routes:

1. **Documentation example** — replace the value with a placeholder:
   reserved documentation ranges for addresses (`192.0.2.0/24`,
   `198.51.100.0/24`, `203.0.113.0/24`), `example.com` / `*.example.com` for
   hosts, `noreply@example.com` for emails, `/home/<user>` for home paths,
   neutral role names (*local node*, *remote node*) for nodes and GPU models.
2. **Operator config** — the value lives in `~/.datawatch/config.yaml`
   (gitignored) or another config file the operator names; the repository
   ships defaults and placeholders, and a document that must quote a real
   setting says so in the text.
3. **Environment variable** — for build-time values, the `Makefile` reads the
   env var; ship a `.env.<thing>.example` template, never the real file.
4. **Cluster Secret / SealedSecret / CSI** — a Helm chart reads from a Secret
   the operator pre-creates; the chart itself never carries the values.

### Scrubbing peer and node names

`scripts/scrub-peer-names.sh` rewrites the peer/node names your daemon knows
into the same neutral placeholders (remote-node, peer-a, peer-b, gpu-node, …)
across the tree, case-insensitively, replacing longer patterns first. It
never prints the patterns or the matched lines; a dry run emits only
`path: N` counts. Use it after the daemon learns a new peer or node name and
you want the tree cleaned in one pass; then run `make check-leaks` to
confirm.

### A path that genuinely cannot be made clean

Add a glob to `scripts/leak-check.exclude`, one glob per line, with a
comment saying why. Keep this list minimal — prefer self-clean patterns and
cleaning the value, and exclude only as a last resort.

## Where the check is wired

```text
git commit
   └── hooks/pre-commit-docs-sync          (your pre-commit chain)
         ├── scripts/check-docs-sync.sh
         ├── hooks/pre-commit-leak-check  ──► scripts/check-no-local-leaks.sh .
         └── scripts/check-no-unscoped-tmux.sh

make lint ─────────────────────────► make check-leaks
                                   ├── scripts/check-no-local-leaks.sh .
                                   └── scripts/check-generated-reports-leaks.sh
scripts/release-smoke.sh (before a release) ─► scripts/check-no-local-leaks.sh .
```

- **Pre-commit**: `hooks/pre-commit-leak-check` runs the whole-tree check
  from the worktree root and lets the checker's report and exit code be the
  hook's: the commit is refused on any finding in any class. There is no
  escape hatch. The sibling hook `hooks/pre-commit-docs-sync` already calls
  it as the middle step of the chain, so a commit that syncs docs is leak
  checked before it is allowed. Install (per the sibling `hooks/` pattern):

  ```sh
  # direct hook:
  ln -sf ../../hooks/pre-commit-leak-check .git/hooks/pre-commit
  # or chain it:
  ln -s -f ../../hooks/pre-commit-leak-check .git/hooks/pre-commit-leak-check
  ```

- **`make lint`**: `check-leaks` is one of its steps (it runs the whole-tree
  check and the generated reports folder scan together), so a failing lint
  means a leak finding until you act on the report.
- **Release**: `scripts/release-smoke.sh` runs the whole-tree check
  before a release is cut; it can be skipped only with
  `DW_SKIP_LOCAL_LEAKS_CHECK=1` (which the operator owns, deliberately).
  The release smoke does not run the generated reports folder scan either. If a
  generated report must be cited in a release note, run
  `make check-generated-reports-leaks` (or `make lint`) against it first.

## What the tree check cannot see

Commit messages, and issue and PR comment text, are not part of the tree and
are not scanned by `check-leaks`. They must still stay free of local
identifiers; the narrower last-commit diff check (hostname, private IP
ranges, personal email in `git diff HEAD~1`) remains as the complementary
check for exactly that surface.

## Operator review checklist

- [ ] `.gitignore` still covers `.env`, `.env.build`, `config.yaml`,
      `.leak-patterns.local`, `*.kubeconfig` and any Secret-bearing file.
- [ ] `.leak-patterns.local` exists on every machine that builds or commits
      this repository, seeded (`make leak-patterns-init`) plus the operator's
      own private tokens — until it exists, the `local` class is silently
      inactive there (`local patterns: skipped` on stderr).
- [ ] The pre-commit chain is installed (see above); until it is, only
      `make lint` and the release smoke catch a leak.
- [ ] New generic patterns added to `scripts/leak-check.patterns` stay
      self-clean (bracketed dots) and the file still passes `make check-leaks`.
- [ ] Stale generated reports under `docs/plans/reports` are cleaned up or
      pass the scan: the pre-commit gate and the release smoke run the
      whole-tree check only, so `make lint` (via
      `make check-generated-reports-leaks`) is the only standing gate over
      that untracked surface.
- [ ] `scripts/leak-check.exclude` stayed minimal: every entry has a comment
      saying why the path cannot be made clean.
