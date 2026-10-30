# CodeQL path-injection alerts: operator decision (2026-10-26)

Source: BL625 (PRD 5a390001), task "Triage and fix the Go path-injection alerts". Three worker attempts could not
decide how to treat an absolute project directory in a session-start request (alerts 695, 696, 658 and 681),
because the dismissal criterion (input validated by the project directory guard or from daemon config) does not
hold for a request-supplied path.

Question to the operator: should a session-start request be allowed to name an absolute project directory outside
the workspace? Options: (1) restrict to a configured list of allowed roots, (2) dismiss because the authenticated
operator API starts sessions in any directory, (3) always restrict to the workspace root with no list.

Operator discussion, 2026-10-26: the supervisor asked why the root is not known when worktrees are used. Answer
recorded: the root is known for Automaton work (the daemon creates `~/.datawatch/prd-worktrees/<id>`); it is not
known for interactive sessions and PRDs created with an explicit `project_dir`, whose repositories live under
`/home/user/workspace`. Proposed default: allowed roots are the worktree base plus the workspace root, always
enforced, more roots only by explicit configuration.

**Decision, operator, quoted: "Yes, use that default".**

Applied rule:
- Allowed project roots: `~/.datawatch/prd-worktrees` and the daemon's workspace root (`/home/user/workspace` here),
  always enforced, whether or not an API token is set.
- More roots only through an explicit setting (`server.allowed_project_roots`), on every configuration surface.
- A project directory outside the allowed roots is refused with a clear error. Symlinks and `..` are resolved
  before the check, including dangling links.
- This applies to session start, PRD creation with an explicit `project_dir`, the pipeline test runner and the git
  guard.
