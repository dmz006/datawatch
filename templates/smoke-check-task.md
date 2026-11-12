# Smoke check — task spec template (`smoke-check-task`)

Use this template as the starting point for any task whose purpose is to
**smoke-check** a datawatch build or installation: prove that it starts,
serves, and behaves against representative input, without touching the
operator's real configuration or data.

## The rule (non-negotiable)

**Never copy, `cp`, `rsync`, symlink (`ln -s`), or take a
snapshot of the operator's configuration or data into this repository —
not from `$HOME/.config`, not from `$HOME/.datawatch`, not from
`$HOME/.env*`, not from any production database file or secrets store.**

The repository is the only place a worker may write, and anything that
lands here is under version control. Operator configuration and data may
contain credentials, channel tokens, session history, and private content;
copying them into the tree defeats the purpose of a smoke test and creates
a leak that no later cleanup step can un-see.

A smoke task **synthesizes** its world instead: throwaway configuration and
data is built fresh in a temp directory or a scratch folder outside the
repository, and the daemon runs pointed at that.

- ✅ **Allowed:** a minimal synthetic config written by the task itself
  (`datawatch.yaml` fixture in the scratch folder), fake credentials and
  tokens, `mktemp -d` scratch directories, in-process fakes in Go tests.
- ⛔ **Not allowed:** the operator's real config, config directory, env
  files, secrets, key files, or database files — in the tree, on disk, or
  referenced by the spec.

The spec itself is linted at decompose time (the smoke-config lint rule
runs next to the scope lint, and honors the `scope_lint` mode): a task
whose title, spec, or kind says "smoke"/"live check" is rejected when its
spec pairs a copy/snapshot verb (`cp`, `copy`, `rsync`, `symlink`,
`ln -s`, `snapshot`) with an operator-data target (the home config
directory, `datawatch.yaml`, secrets, databases). The regression guard for
stray copies already committed into the change set is
`scripts/check-no-config-copies.sh` (optionally `scripts/check-no-config-copies.sh <base-ref>`
to check the branch's full change set).

## Setup pattern (temp directories + outside-repo scratch)

```bash
# 1. Throwaway home — configuration and data live here, never in the repo.
SCRATCH="$(mktemp -d "${TMPDIR:-/tmp}/smoke-XXXXXX")"
export HOME="$SCRATCH/home"                # placeholder: worker's throwaway home
mkdir -p "$HOME/.config" "$HOME/.datawatch"

# 2. Synthetic configuration, written by the task (placeholders, not the real values).
cat > "$HOME/.config/datawatch.yaml" <<'YAML'
host: 127.0.0.1
port: 0                     # bind to an ephemeral port, never the operator's
channels: {}                # no real channel tokens — use fakes or none
YAML

# 3. Run the build under test against the synthetic home.
datawatch start --foreground --config "$HOME/.config/datawatch.yaml" &
DAEMON_PID=$!

# 4. Verify, then tear down (the spec must own every process it starts).
kill "$DAEMON_PID" 2>/dev/null || true
rm -rf "$SCRATCH"
```

- Go test variants use the same shape in code: a synthetic home under
  `t.TempDir()` (or the `internal/testhome` helper), registered cleanup,
  in-process fakes for external services — no live daemons, no operator
  paths.
- If the smoke must inspect real operator behavior (log format, alert
  body), capture the **output** (redacted, with secrets and personal data
  removed) — never the source material.

## Task spec skeleton

Fill in the `<...>` placeholders; keep every path under the scratch space or the
project directory. Do **not** name `$HOME/.config`, the operator's
`datawatch.yaml`, or any real host/credential value in the spec.

```
Title: Smoke check: <build|installation|packaging artifact>
Kind: smoke

Given a throwaway home built with mktemp -d outside this repository (<scratch>,
a synthetic minimal datawatch.yaml with fake credentials, placeholder endpoints,
and no real channel tokens),

- <start the build under test against that synthetic home on an ephemeral port>
- <assert the smoke surface: process stays up, endpoint <placeholder endpoint>
  answers <expected>, log line <placeholder pattern> appears, no panic lines>
- <verify cleanup: the daemon process is ended and the scratch tree removed>

The spec must not copy, rsync, symlink, or snapshot the operator's
configuration, credentials, or data into the repository; all synthetic
configuration and data is written fresh in the scratch folder above.

Verification script: <path under the project directory>
```

## Definition of done

- [ ] Synthetic config and data built in a `mktemp -d` scratch directory
      (or a scratch folder outside the repository); nothing under
      `$HOME/.config` or `$HOME/.datawatch` touched or referenced.
- [ ] In-process tests (if any) use fakes and `t.TempDir()`; no shared global
      state, no network to real endpoints, no operator tokens.
- [ ] Every process the task starts is ended on all exit paths (success,
      failure, interrupt); scratch directories removed on the same paths.
- [ ] The spec text passed the decompose smoke-config lint (no
      copy-verb + operator-data-target pair in a smoke/live-check task).
- [ ] `scripts/check-no-config-copies.sh <base-ref>` (or without a ref,
      for staged and untracked changes) reports `OK` — no stray `*.bak`,
      `.env*`, `datawatch.yaml.*`, nested `.datawatch/`, or secret/key
      paths among the added/modified files.
