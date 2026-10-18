# Tree-serialization live smoke — final report

**Run:** 2026-10-18 16:28–16:39 (operator local time) · **datawatch v9.5.4** (built from
HEAD) · **scratch daemon** port 18912 (isolated from the operator daemon) · planner
`pi-prompt → ollama` (local, direct) · executor `claude-code` (local).
**Capacity control:** `max_concurrent_tasks=8` so capacity is *not* the gate — only the
per-tree lock can refuse.

Two PRDs are kicked "simultaneously" (sub-second apart). The per-canonical-tree advisory
lock (`internal/autonomous/tree_lock.go`, acquired in `internal/autonomous/executor.go`,
keyed by `canonicalTreeKey` = resolved tree directory) is the thing under test.

---

## Scenarios & how the lock decides

- **Scenario 1 — same tree (direct mode).** Both PRDs point at the *same* `project_dir`
  (`s1`). Same canonical tree ⇒ same lock key ⇒ serialized. Expected: never both in-flight;
  loser refused.
- **Scenario 2 — distinct trees.** PRDs point at *different* `project_dir`s (`s2c`, `s2d`)
  — the same property worktree mode (PRD w/ empty `project_dir`) gives via distinct
  branches. Distinct trees ⇒ distinct lock keys ⇒ the per-tree lock does **not** gate
  them ⇒ both run in parallel. This is the decisive discriminator: if a *global* gate
  existed, they'd serialize too; they don't.

## Results (live)

### Scenario 1 — same tree

| time   | A (owner first)          | B (loser)            |
|--------|--------------------------|----------------------|
| 16:29:04 | kick                     | kick (205 ms later)  |
| 16:29:35–16:30:38 | running/verifying | `approved/pending` (refused, same tree) |
| 16:30:40 | **completed**            | **still `approved`** ← not auto-resumed |
| 16:33:05 |                          | **manual re-kick → running** |
| 16:34:11 |                          | **completed**        |

- **A1 — same-tree serialized (never both `in_progress`): PASS.** B never entered
  `in_progress` while A held the tree.
- **Distinct commits, own files:** `c61a833` → `A_LIST.txt`(150 lines); `db1ebdf` →
  `B_LOG.txt`(150 lines). Each in its own commit. `tree.lock` created then released.
- **A1b — loser auto-resume:** **GAP.** B was refused, reverted to `approved`, and sat
  there 2+ min after A released the lock. Only a **manual re-kick** started it. This
  reproduces the known BL428 defect ("loser stuck, needs re-kick") live on v9.5.4.

### Scenario 2 — distinct trees (worktree-equivalent)

| time   | C (s2c)            | D (s2d)            |
|--------|--------------------|--------------------|
| 16:36:36 | kick             | kick              |
| **16:36:44–16:37:27** | **in_progress** | **in_progress** ← BOTH, ~43 s |
| 16:37:30 | **completed**    | running/verifying |
| 16:37:34 |                    | **failed (infra)** |

- **A2 — cross-tree parallel (both `in_progress` at once): PASS.** Direct proof the gate
  is the **per-tree** lock, not a global/capacity limit (capacity was 8; both ran
  together anyway).
- **C** completed and committed `C_FILE.txt` in its own tree (`bfb4af0`).
- **D** failed with `category: infra` — "worker stopped and asked an interactive
  question … **worker stalled**", `retryable:false`. Re-kicked (lock free) → same stall.
  This is a **worker execution flake, unrelated to the tree lock** (a lock refusal looks
  like B's `approved/pending`, not `failed/infra`). Non-blocking for the lock claim.

---

## VERDICT on the tree-lock feature (live)

| Assertion                                              | Result |
|--------------------------------------------------------|--------|
| Same-tree → serialized (no simultaneous in-flight)      | **PASS** |
| Distinct commits, each own file                         | **PASS** |
| Cross-tree → parallel (lock is **per-tree**, not global)| **PASS** |
| Same-tree loser auto-waits + auto-resumes (BL428 §3a)   | **GAP** — reverts to `approved`; manual re-kick required (defect reproduced live) |

**Headline:** the per-canonical-tree advisory lock is real and works exactly as the core
contract states — it serializes same-tree work and permits cross-tree parallelism, and the
tree lock (not capacity) is the gate. The remaining defect is the convenience path: a
same-tree second PRD is refused and **not** auto-queued/`waiting`+resumed; it needs a
manual re-kick. That is the one part of the tree-serialization story that is **not** fully
shipped.

## Reproduce

```bash
cd datawatch
BINARY=$(pwd)/datawatch SMOK_PORT=18912 \
  PI_MODEL=ollama/qwen3.8-64k:27b \
  ./tests/live-smoke/tree-serialization-smoke.sh
```

## Evidence (this live run)
`tests/live-smoke/EVIDENCE/20261019-1637/` — `S1_EVIDENCE.txt`, `S2_EVIDENCE.txt`,
`timeline_s1.txt`, `timeline_s2.txt`, `A_snap.json`, `B_snap.json`, `D_snap.json`,
`git-logs.txt`.
