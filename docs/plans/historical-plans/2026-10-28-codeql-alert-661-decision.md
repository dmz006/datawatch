# CodeQL alert 661: subprocess sessions keep running a shell (decision, 2026-10-28)

**Date:** 2026-10-28
**Version:** v9.28.4 at the time of the decision.
**Scope:** `go/command-injection` alert 661 (critical) at `internal/session/subprocess_resume.go:41`, open since 2026-10-18.
**Status:** decided; the alert is dismissed in GitHub as "won't fix" with the risk acceptance below; the argv-only rewrite built by the code-scanning clean-up Automaton is not merged.

## What the code does

A subprocess session (`--shell`, `--subprocess`, the `!cmd` style, a scheduled shell job) runs its task through `bash -c`. The task string is the command line the caller supplied. CodeQL traces that string from the request that starts a session to a shell and reports it as command injection.

## Why it is the intent

`docs/datawatch-definitions.md` and `docs/commands.md` describe the feature as "Shell command to run" and "Run via `bash -c`; exit code signals completion". Running an arbitrary command line is the feature, not an escape from it.

## The decision

Operator, 2026-10-28, quoted: "2 the shell is the intent right?" (the answer to the choice between argv-only and keeping the shell).

Keep the shell. The change that rewrote subprocess sessions to run a program with arguments and no shell, which refused pipes, redirects, `&&`, globs and variable expansion, is dropped. The other two alerts in that Automaton (reflected output in the MCP debug transport and an insecure random value in the PWA) are fixed in code and merged.

## Risk acceptance (recorded in the GitHub dismissal)

- Command execution through a subprocess session is the documented purpose of the feature.
- The trust boundary is whoever may start a session: an authenticated REST caller, an allowed messaging-channel sender, the operator in the PWA. The same caller can already start a Claude session with full tool access, so a shell adds no privilege.
- If `server.token` is empty the REST listener accepts anyone who can reach it; that exposure is covered by the startup warning added in v9.21.0 and by the per-listener `auth_required` flag in `/api/health`, not by this alert.
- Revisit if sessions can ever be started by an untrusted principal (for example a federation peer without the right capability).

## Parity surface

REST, MCP, CLI, comm channel, YAML/config, PWA, Android and iPhone: no change; behaviour is unchanged.
