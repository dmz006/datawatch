# Ancestor CLAUDE.md contamination of PRD workers: findings

Date: 2026-10-21. Claude Code 2.1.306. Research only; no source changes.

## Conclusion

Use the `claudeMdExcludes` setting, passed per worker with `--settings`:

```
claude ... --settings '{"claudeMdExcludes":["/home/user/CLAUDE.md","/home/user/workspace/CLAUDE.md"]}'
```

It removes the foreign ancestor files and keeps the worktree's own `CLAUDE.md` and the user-level `~/.claude/CLAUDE.md`. `CLAUDE_CODE_DISABLE_CLAUDE_MDS=1` also works but is too blunt (see Mechanism). `--bare` was not tested because it also disables auth, hooks and auto-memory.

## 1. Frequency

Method: `datawatch autonomous prd-list` and `prd-get <id>` (the daemon's own tooling), run with `env -u TMUX -u TMUX_TMPDIR`. The output was parsed in a scratch directory inside the project and then deleted.

Not examined: worker transcripts and session logs. They live under `~/.datawatch/`, outside the allowed directories. Only the PRD records the daemon returned were searched. The foreign-task cause (`BL551` in `docs/plans/README.md`) is therefore not confirmed from transcripts here.

Counts (226 PRDs in the store):

- 20 PRDs contain at least one task error with the stall wording `worker stopped and asked an interactive question instead of completing the one-shot task`.
- 54 tasks across those 20 PRDs carry that wording.
- Across all fields, the phrase `asked an interactive question` appears 59 times. The largest is PRD `38b69678` with 33.
- Creation dates of the 20 PRDs: 2026-10-11 (2), 10-12 (1), 10-13 (4), 10-14 (1), 10-15 (2), 10-18 (3), 10-19 (1), 10-20 (1), 10-21 (5).
- The foreign task path `/home/user/.config/imap-mcp/run-rules` and the guardrail session id `local-node-6b4d` do not appear in any PRD record. The only record containing the path is `9a400001`, and there it comes from this task's own spec. Evidence of foreign task names, such as the one in the BL551 note, would have to come from worker transcripts, which I did not read.

PRDs named in the request:

| PRD | Status | Records with stall wording |
|---|---|---|
| b2060001 | running | 2 |
| 1c4c0001 | cancelled | 3 |
| c1eb0001 | cancelled | 1 |
| 4cbc0001 | cancelled | 0 |
| 6ac20001 | cancelled | 0 |
| 84c60001 | completed | 0 |

For `4cbc0001`, `6ac20001` and `84c60001` the PRD records show no stall wording, so the claim is not verifiable from the records. Any stalls would only be in transcripts. Most of the stall wording predates 2026-10-21, so not all of it can come from this contamination.

## 2. Mechanism

Setup: a scratch parent `CLAUDE.md` with a unique marker, and a child directory with its own `CLAUDE.md`. The scratch directory sat inside the project, so the real ancestors (`/home/user/CLAUDE.md`, the worktree `CLAUDE.md`) loaded as well. Every run used `claude -p` with `env -u TMUX -u TMUX_TMPDIR`.

| Run | Parent marker | Child marker |
|---|---|---|
| baseline | quoted | quoted |
| `--settings '{"claudeMdExcludes":["<abs parent CLAUDE.md>"]}'` | gone | still quoted |
| `CLAUDE_CODE_DISABLE_CLAUDE_MDS=1` | gone | gone |

Second test, on heading presence in the loaded memory:

| Run | `/home/user/CLAUDE.md` (Session Guardrails, Cross-Session) | `~/.claude/CLAUDE.md` (Home Assistant) | worktree `CLAUDE.md` (Command Timing) |
|---|---|---|---|
| baseline | loaded | loaded | loaded |
| `claudeMdExcludes: ["/home/user/CLAUDE.md"]` | gone | loaded | loaded |
| adding `/home/user/workspace/CLAUDE.md` to the list | gone | loaded | loaded |
| `CLAUDE_CODE_DISABLE_CLAUDE_MDS=1` | gone | gone | gone |

Findings:

- `claudeMdExcludes` works. The syntax is `--settings '{"claudeMdExcludes":["<absolute path>", ...]}'` with absolute paths.
- The project's own `CLAUDE.md` still loads, and so does the user-level `~/.claude/CLAUDE.md`. The user-level file is not affected.
- The env var disables all CLAUDE.md files, including the project's own and the user-level one. It is not suitable, because workers rely on the worktree `CLAUDE.md` (timing rule, completion protocol).
- `/home/user/.datawatch/CLAUDE.md` and `/home/user/.datawatch/prd-worktrees/CLAUDE.md` do not exist, so only `/home/user/CLAUDE.md` (worktree workers) and `/home/user/workspace/CLAUDE.md` (main-checkout workers) need excluding.
- I did not test glob patterns. Use explicit absolute paths.
- I did not check that this survives however the daemon launches workers, or that `--settings` merges with the project's own settings files.

## Chosen fix

Pass `--settings` with `claudeMdExcludes` listing the foreign ancestor `CLAUDE.md` paths on every claude-code worker launch, computed from the worker's working directory (all ancestor `CLAUDE.md` files except the worktree's own). Do not use the env var or `--bare`.
