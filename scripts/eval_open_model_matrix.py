"""Open-model matrix eval driver (roles x task types = cells; the tracked matrix
is 5 pi-prompt roles — baseline plus execution-role candidates — x 4 task types
= 20 cells).

Run as `python3 -I scripts/eval_open_model_matrix.py <subcommand>`; normally
via scripts/eval-open-model-matrix.sh. Stdlib only.

Fixture:  testdata/eval/open-model-matrix/matrix.json (task specs/verifiers
          reused from testdata/eval/model-tiering — fixtures_from). The tracked
          file carries placeholder LLM/node names only; the operator's real
          registry names live in a gitignored local matrix (see load_matrix).
State:    <cell_root>/<run-id>/state.json (gitignored, under .datawatch/)
Results:  docs/testing/runs/<YYYY-MM-DD>-open-model-matrix/results.{json,md}

Trust model — three verdicts are kept separate and never collapsed:
  model_identity          did the cell actually run on the expected model?
  verification_integrity  did the task/verifier operate on the cell dir (not a parent)?
  model_result            per-type verifier pass/fail — ONLY when both above are 'ok'.

HTTP client, PRD helpers and cell seeding are imported from
eval_model_tiering.py (not modified).
"""

import argparse
import datetime as _dt
import getpass
import json
import os
import re
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import eval_model_tiering as emt  # noqa: E402

from eval_model_tiering import (  # noqa: E402
    Client, EvalError, all_tasks, log, now_iso, prd_action, prd_cancel_quiet, prd_create, prd_get,
    prd_set_title, prd_wait_planned, sessions_index, wait_tasks_terminal, write_json_atomic,
)

REPO = emt.REPO
MATRIX_REL = "testdata/eval/open-model-matrix/matrix.json"
# Operator-local matrix with the real LLM registry / node names. Gitignored
# (lives under .datawatch/), so private hostnames never land in tracked files.
LOCAL_MATRIX_REL = ".datawatch/eval/open-model-matrix/matrix.local.json"
MATRIX_ENV = "DATAWATCH_EVAL_OPEN_MATRIX"
RUNS_REL = "docs/testing/runs"
TITLE_PREFIX = "[eval] open-model-matrix"
ACTOR = "eval-open-model-matrix"
IDENTITY_FILE = "MODEL-IDENTITY.txt"
PREFLIGHT_PROMPT = "Reply with the single word OK so we can verify reachability."


# --------------------------------------------------------------------------
# fixtures + state
# --------------------------------------------------------------------------

def matrix_path():
    """$DATAWATCH_EVAL_OPEN_MATRIX, else the gitignored local matrix, else the
    tracked placeholder matrix (whose names will not resolve on a real daemon)."""
    env = os.environ.get(MATRIX_ENV)
    if env:
        return env
    local = os.path.join(REPO, LOCAL_MATRIX_REL)
    return local if os.path.exists(local) else os.path.join(REPO, MATRIX_REL)


# The baseline role: its executor cells run like any other role, and it also
# holds the scratch Automaton's decompose / validate / guardrail roles.
BASELINE_ROLE = "base"
# Allowed pi thinking levels — mirrors internal/config/pi_thinking_level.go.
# Per-role `thinking_level` (executor) and top-level `prd_thinking_level`
# (non-executor duties) may each be null (= leave pi on its own default).
THINKING_LEVELS = ("off", "minimal", "low", "medium", "high")


def _check_matrix(m):
    roles = m.get("roles") or []
    if not roles:
        raise EvalError("matrix.json must define at least one role")
    names = [r.get("role") for r in roles]
    if BASELINE_ROLE not in names:
        raise EvalError(f"matrix.json must define the {BASELINE_ROLE!r} role (it holds the decompose/validate/"
                        "guardrail roles in addition to executing cells)")
    if len(names) != len(set(names)):
        raise EvalError("matrix.json role names must be unique")
    if not m.get("task_types"):
        raise EvalError("matrix.json must define task_types")
    allowed = "null or: " + "|".join(THINKING_LEVELS)
    if m.get("prd_thinking_level") is not None and m["prd_thinking_level"] not in THINKING_LEVELS:
        raise EvalError(f"prd_thinking_level {m['prd_thinking_level']!r} not allowed ({allowed})")
    for r in roles:
        lvl = r.get("thinking_level")
        if lvl is not None and lvl not in THINKING_LEVELS:
            raise EvalError(f"role {r.get('role')!r}: thinking_level {lvl!r} not allowed ({allowed})")
    return m


def load_matrix(path=None):
    return _check_matrix(emt.load_json(path or matrix_path()))


def load_public_matrix():
    return _check_matrix(emt.load_json(os.path.join(REPO, MATRIX_REL)))


def redact(obj, m, public):
    """Rewrite a results doc for a tracked file: absolute in-repo paths become
    repo-relative, the local matrix's LLM names become the tracked placeholder
    names wherever they appear (whole names, per role), and `node` fields are
    mapped likewise. Node names are only swapped in `node` fields because a
    host can share a name with ordinary words (e.g. the product name).
    Any remaining home-dir path becomes `~`, and the local username, standing
    alone or as a prefix like `<user>_model` in model output, becomes `<user>`."""
    pub = {r["role"]: r for r in public["roles"]}
    llms, nodes = [], {}
    for r in m["roles"]:
        p = pub.get(r["role"]) or {}
        if r.get("llm") and p.get("llm") and r["llm"] != p["llm"]:
            llms.append((r["llm"], p["llm"]))
        if r.get("node") and p.get("node"):
            nodes[r["node"]] = p["node"]
    llms.sort(key=lambda x: -len(x[0]))
    root = os.path.realpath(REPO) + os.sep
    home = os.path.expanduser("~").rstrip(os.sep)
    try:
        user = getpass.getuser()
    except Exception:  # no passwd entry / env in some sandboxes
        user = ""

    def fix(v, key=None):
        if isinstance(v, dict):
            return {k: fix(x, k) for k, x in v.items()}
        if isinstance(v, list):
            return [fix(x) for x in v]
        if not isinstance(v, str):
            return v
        if key == "node" and v in nodes:
            return nodes[v]
        v = v.replace(root, "").replace(REPO.rstrip(os.sep) + os.sep, "")
        for a, b in llms:
            v = re.sub(r"(?<![\w.-])" + re.escape(a) + r"(?![\w.-])", b, v)
        if home and home != os.sep:
            v = v.replace(home + os.sep, "~" + os.sep)
        if len(user) >= 3:
            v = re.sub(r"(?<![A-Za-z0-9])" + re.escape(user) + r"(?![A-Za-z0-9])", "<user>", v)
        return v
    return fix(obj)


def fix_dir(m):
    return os.path.join(REPO, m["fixtures_from"])


def type_defs(m):
    """task-type id -> model-tiering task_type entry (baseline_sha, spec, verify, cell_inputs)."""
    src = emt.load_json(os.path.join(fix_dir(m), "matrix.json"))
    by = {t["id"]: t for t in src["task_types"]}
    missing = [t for t in m["task_types"] if t not in by]
    if missing:
        raise EvalError(f"task types not in {m['fixtures_from']}/matrix.json: {missing}")
    return by


def run_dir(m, run_id):
    if not emt.valid_run_id(run_id):
        raise EvalError(f"invalid --run-id {run_id!r}")
    return os.path.join(REPO, m["cell_root"], run_id)


def state_file(m, run_id):
    return os.path.join(run_dir(m, run_id), "state.json")


def load_state(m, run_id, required=False):
    p = state_file(m, run_id)
    if os.path.exists(p):
        return emt.load_json(p)
    if required:
        raise EvalError(f"no state for run {run_id} ({os.path.relpath(p, REPO)}); run preflight first")
    return {"run_id": run_id}


def save_state(m, run_id, st):
    st["updated_at"] = now_iso()
    write_json_atomic(state_file(m, run_id), st)


def cells(m):
    for r in m["roles"]:
        for tt in m["task_types"]:
            yield {"name": f"{r['role']}-{tt}", "role": r["role"], "llm": r["llm"],
                   "expected_model": r["model"], "task_type": tt,
                   "thinking_level": r.get("thinking_level")}


# --------------------------------------------------------------------------
# pure verdict logic (exercised offline by self-test)
# --------------------------------------------------------------------------

def preflight_row(role, resp=None, err=None):
    used = (resp or {}).get("used_model")
    ok = err is None and used == role["model"]
    return {"role": role["role"], "llm": role["llm"], "expected_model": role["model"],
            "used_model": used, "ok": ok, "error": err}


def preflight_passed(st):
    pf = st.get("preflight") or {}
    rows = pf.get("rows") or []
    return bool(pf.get("passed")) and len(rows) >= 1 and all(r.get("ok") for r in rows)


def require_preflight(st):
    if not preflight_passed(st):
        raise EvalError("refusing: preflight has not passed for this run (every role's used_model must equal "
                        "its expected model). Run `preflight` and fix the mismatches first.")


def classify_identity(expected, used):
    if used is None or used == "":
        return "unknown"
    return "ok" if used == expected else "wrong_model"


def _real(p):
    return os.path.realpath(p) if p else None


def classify_integrity(cell_dir, task_project_dir, resolved_dir):
    cd = _real(cell_dir)
    if not task_project_dir or not resolved_dir:
        return "suspect"
    if _real(task_project_dir) != cd or _real(resolved_dir) != cd:
        return "suspect"
    return "ok"


def classify_result(identity, integrity, verify_exit):
    if identity != "ok" or integrity != "ok":
        return None
    return "pass" if verify_exit == 0 else "fail"


# --------------------------------------------------------------------------
# preflight
# --------------------------------------------------------------------------

# Cold-load on the shared GPU node can outlast the Client's default 60s read
# timeout (observed 3 consecutive preflight runs 2026-10-15, a different role
# timing out each time, not a broken model) — give this specific call more
# headroom than other, already-warm API calls get.
PREFLIGHT_TEST_TIMEOUT_S = 240


def llm_test(c, name):
    return c.post(f"/api/llms/{name}/test", {"prompt": PREFLIGHT_PROMPT}, timeout=PREFLIGHT_TEST_TIMEOUT_S)


def render_preflight_table(rows):
    lines = [f"{'role':<20} {'llm':<28} {'expected':<16} {'used':<16} ok   error"]
    for r in rows:
        lines.append(f"{r['role']:<20} {r['llm']:<28} {r['expected_model']:<16} {str(r['used_model']):<16} "
                     f"{'yes' if r['ok'] else 'NO ':<4} {(r.get('error') or '')[:160]}")
    return "\n".join(lines)


def cmd_preflight(a):
    m = load_matrix()
    st = load_state(m, a.run_id)
    st.setdefault("started_at", now_iso())
    st.setdefault("run_date", _dt.date.today().isoformat())
    c = Client()
    rows = []
    for role in m["roles"]:
        try:
            rows.append(preflight_row(role, resp=llm_test(c, role["llm"])))
        except EvalError as e:
            rows.append(preflight_row(role, err=str(e)))
    passed = all(r["ok"] for r in rows)
    st["preflight"] = {"at": now_iso(), "rows": rows, "passed": passed}
    save_state(m, a.run_id, st)
    print(render_preflight_table(rows))
    if not passed:
        raise EvalError("PREFLIGHT FAILED — at least one role did not report used_model == expected model; "
                        "nothing further will run for this run-id")
    log(f"PREFLIGHT OK — all {len(rows)} roles answered on their expected model")


# --------------------------------------------------------------------------
# setup / create-prd / run
# --------------------------------------------------------------------------

def cmd_setup(a):
    m = load_matrix()
    tds = type_defs(m)
    st = load_state(m, a.run_id)
    st.setdefault("run_date", _dt.date.today().isoformat())
    st["repo_head_at_run"] = emt.git("rev-parse", "HEAD")
    st.setdefault("cells", {})
    root = run_dir(m, a.run_id)
    for cell in cells(m):
        d = os.path.join(root, cell["name"])
        rel = os.path.relpath(d, REPO)
        if os.path.exists(d):
            log(f"skip (exists) {rel}")
        else:
            emt.seed_cell(d, tds[cell["task_type"]]["baseline_sha"], tds[cell["task_type"]])
            log(f"seeded {rel}")
        base = emt.git("rev-parse", "HEAD", cwd=d)
        st["cells"].setdefault(cell["name"], {}).update({**cell, "dir": rel, "baseline_commit": base})
    save_state(m, a.run_id, st)
    log(f"SETUP OK — {len(st['cells'])} cells")


PLACEHOLDER_SPEC = (
    "Eval scaffold placeholder for the open-model matrix eval driver. Plan exactly one story with one task "
    "titled 'placeholder' that changes no files. The eval driver removes this generated plan and replaces "
    "it with its own cell tasks before review; nothing in this plan will ever run."
)


def identity_preamble(cell_dir_abs):
    return (f"FIRST ACTION (before anything else): write the file {cell_dir_abs}/{IDENTITY_FILE} containing "
            f"one line with your own self-reported model name (the model you are running as). Do not commit "
            f"it unless the spec below asks for commits; it is read by the eval harness.\n\n")


def task_spec(cell, cell_dir_abs, spec_text):
    return identity_preamble(cell_dir_abs) + emt.cell_task_spec(cell, cell_dir_abs, spec_text)


def cmd_create_prd(a):
    m = load_matrix()
    tds = type_defs(m)
    st = load_state(m, a.run_id, required=True)
    require_preflight(st)
    if st.get("eval_prd_id"):
        raise EvalError(f"run {a.run_id} already has eval_prd_id {st['eval_prd_id']}; use a new --run-id")
    want = list(cells(m))
    missing = [x["name"] for x in want if x["name"] not in st.get("cells", {})]
    if missing:
        raise EvalError(f"cells not set up: {', '.join(missing)} (run setup first)")
    c = Client()
    prd = prd_create(c, PLACEHOLDER_SPEC, REPO)
    pid = prd["id"]
    st["eval_prd_id"] = pid
    save_state(m, a.run_id, st)
    # Non-executor duties (decompose/validate/guardrail) ride the PRD-level pi
    # thinking level. Applied only when the matrix sets one; null (the tracked
    # matrix's choice) leaves pi on its own default for those roles.
    if m.get("prd_thinking_level"):
        prd_action(c, pid, "set_thinking_level", {"thinking_level": m["prd_thinking_level"], "actor": ACTOR})
        log(f"PRD-level thinking level set to {m['prd_thinking_level']}")
    # Operator-reported 2026-10-15: this whole matrix is meant to be
    # Claude-free, but decompose (planning) and the per-task guardrail
    # verifier both silently defaulted to the global claude-code setting
    # — only the cells' own workers were ever actually local. Pin both
    # to a local model before decomposing. In the tracked matrix the
    # baseline role holds the decompose/validate/guardrail roles: decompose
    # needs real codebase tool access (headless ollama/openwebui can't
    # explore the repo), so it uses the matrix's own largest,
    # most-exercised model; the guardrail verifier is a cheap/quick
    # confirmation only (the real pass/fail signal is the deterministic
    # verify script) and rides the same local model here.
    def role_by_name(n):
        for r in m["roles"]:
            if r["role"] == n:
                return r
        raise EvalError(f"matrix.json has no role {n!r}; it is required for this step")

    base = role_by_name(BASELINE_ROLE)
    decompose_llm = base["llm"]
    guardrail_llm = base["llm"]
    guardrail_model = base["model"]
    # set_llm (decomposition_profile) works pre-decompose (draft), but
    # set_verification_llm requires needs_review/revisions_asked/cancelled
    # — it has to wait until after decompose lands.
    prd_action(c, pid, "set_llm", {"decomposition_profile": decompose_llm, "actor": ACTOR})
    log(f"created scratch Automaton {pid}; decomposing placeholder (planning={decompose_llm})")
    prd_action(c, pid, "decompose")
    prd = prd_wait_planned(c, pid, a.decompose_timeout)
    if prd.get("status") != "needs_review":
        raise EvalError(f"placeholder decompose ended in {prd.get('status')!r}, expected needs_review")
    prd_action(c, pid, "set_verification_llm", {"verification_backend": guardrail_llm,
                                                "verification_model": guardrail_model, "actor": ACTOR})
    log(f"guardrail pinned to {guardrail_llm}/{guardrail_model}")
    for s in prd.get("stories") or []:
        prd_action(c, pid, "remove_story", {"story_id": s["id"], "actor": ACTOR})
    prd_action(c, pid, "set_memory_seed", {"enabled": False, "actor": ACTOR})
    prd_action(c, pid, "set_memory_harvest", {"enabled": False, "actor": ACTOR})
    prd = prd_action(c, pid, "add_story", {"title": "[eval] open-model-matrix cells",
                                          "description": f"{len(want)} cells: {len(m['roles'])} roles x "
                                                         f"{len(m['task_types'])} task types", "actor": ACTOR})
    stories = prd.get("stories") or []
    if len(stories) != 1:
        raise EvalError(f"expected exactly 1 story after add_story, got {len(stories)}")
    sid = stories[0]["id"]
    for cell in want:
        sc = st["cells"][cell["name"]]
        cdir = os.path.join(REPO, sc["dir"])
        spec_text = open(os.path.join(fix_dir(m), tds[cell["task_type"]]["spec"]), encoding="utf-8").read()
        before = {t["id"] for (_, _, _, t) in all_tasks(prd_get(c, pid))}
        prd = prd_action(c, pid, "add_task", {"story_id": sid, "title": f"[eval] {cell['name']}",
                                              "spec": task_spec(sc, cdir, spec_text), "actor": ACTOR})
        new = [t for (_, _, _, t) in all_tasks(prd) if t["id"] not in before]
        if len(new) != 1:
            raise EvalError(f"add_task {cell['name']}: could not identify new task")
        tid = new[0]["id"]
        prd_action(c, pid, "set_task_project_dir", {"task_id": tid, "project_dir": cdir, "actor": ACTOR})
        # set_task_llm's `backend` is validated against the LLM registry, so the
        # registry entry name (a pi-prompt entry) goes there; model pins it.
        prd_action(c, pid, "set_task_llm", {"task_id": tid, "backend": cell["llm"],
                                            "model": cell["expected_model"], "actor": ACTOR})
        # Per-task thinking-level override: applied only when the role pins a
        # level (null = leave pi on its own default).
        if sc.get("thinking_level"):
            prd_action(c, pid, "set_task_thinking_level",
                       {"task_id": tid, "thinking_level": sc["thinking_level"], "actor": ACTOR})
        sc.update({"task_id": tid, "story_id": sid})
        save_state(m, a.run_id, st)
        log(f"task {tid} <- {cell['name']}")
    prd_set_title(c, pid, f"{TITLE_PREFIX} {a.run_id}")
    prd = prd_get(c, pid)
    tasks = {t["id"]: t for (_, _, _, t) in all_tasks(prd)}
    errs = []
    for cell in want:
        sc = st["cells"][cell["name"]]
        t = tasks.get(sc["task_id"]) or {}
        if t.get("backend") != cell["llm"] or t.get("model") != cell["expected_model"]:
            errs.append(f"{cell['name']}: llm {t.get('backend')}/{t.get('model')} != {cell['llm']}/{cell['expected_model']}")
        if _real(t.get("project_dir")) != _real(os.path.join(REPO, sc["dir"])):
            errs.append(f"{cell['name']}: project_dir {t.get('project_dir')!r} != cell dir")
        if (t.get("thinking_level") or "") != (sc.get("thinking_level") or ""):
            errs.append(f"{cell['name']}: thinking_level {t.get('thinking_level')!r} != {sc.get('thinking_level')!r}")
    if len(tasks) != len(want):
        errs.append(f"Automaton has {len(tasks)} tasks, expected {len(want)}")
    if errs:
        raise EvalError("scratch Automaton verification failed:\n  " + "\n  ".join(errs))
    log(f"CREATE-PRD OK — {pid} with {len(want)} tasks, left at needs_review")


def assert_scratch(c, st):
    pid = st.get("eval_prd_id")
    if not pid:
        raise EvalError("no eval_prd_id in state (run create-prd first)")
    prd = prd_get(c, pid)
    if not str(prd.get("title", "")).startswith(TITLE_PREFIX):
        raise EvalError(f"refusing: Automaton {pid} title {prd.get('title')!r} lacks prefix {TITLE_PREFIX!r}")
    return pid, prd


def cmd_run(a):
    m = load_matrix()
    st = load_state(m, a.run_id, required=True)
    require_preflight(st)
    c = Client()
    pid, prd = assert_scratch(c, st)
    if prd.get("status") == "needs_review":
        prd_action(c, pid, "approve", {"actor": ACTOR, "note": f"open-model-matrix eval {a.run_id}"})
        log(f"approved {pid}")
    if prd.get("status") in ("needs_review", "approved"):
        prd_action(c, pid, "run")
        log(f"run started for {pid}")
    else:
        log(f"Automaton already {prd.get('status')}; polling only")
    st["run_started_at"] = st.get("run_started_at") or now_iso()
    save_state(m, a.run_id, st)
    wait_tasks_terminal(c, pid, a.timeout, a.poll)
    st["run_finished_at"] = now_iso()
    save_state(m, a.run_id, st)
    log("RUN OK — all tasks terminal")


# --------------------------------------------------------------------------
# collect
# --------------------------------------------------------------------------

def session_model(sess):
    if not sess:
        return None
    return sess.get("model") or None


def artifact_check(cell_dir, baseline_commit, verify_script, log_path):
    """Independent of the daemon: new commit(s) in the cell's own repo + per-type verifier exit."""
    out = {"cell_repo_head": None, "new_commits": None, "verify_exit": None}
    if not os.path.isdir(os.path.join(cell_dir, ".git")):
        out["error"] = "cell has no own git repo"
        return out
    try:
        head = emt.git("rev-parse", "HEAD", cwd=cell_dir)
        out["cell_repo_head"] = head
        if baseline_commit:
            out["new_commits"] = int(emt.git("rev-list", "--count", f"{baseline_commit}..HEAD", cwd=cell_dir))
    except Exception as e:  # noqa: BLE001 - report, never crash collect
        out["error"] = f"git: {e}"
    os.makedirs(os.path.dirname(log_path), exist_ok=True)
    with open(log_path, "w") as lf:
        out["verify_exit"] = subprocess.run(["bash", verify_script, cell_dir], cwd=REPO,
                                            stdout=lf, stderr=subprocess.STDOUT).returncode
    out["verify_log"] = os.path.relpath(log_path, REPO)
    return out


def thinking_disp(lvl):
    """Report display: a null/absent level means pi kept its own default."""
    return lvl if lvl else "default"


def build_cell_row(cell, cell_dir, task, worker_sess, verifier_sess, used_model, model_source, art):
    notes = [f"used_model source: {model_source}"]
    identity = classify_identity(cell["expected_model"], used_model)
    tpd = (task or {}).get("project_dir") or None
    # Verifier/diff path: verifier session cwd when one ran, else the worker session's cwd.
    if verifier_sess and verifier_sess.get("project_dir"):
        resolved, src = verifier_sess["project_dir"], "verifier session project_dir"
    elif worker_sess and worker_sess.get("project_dir"):
        resolved, src = worker_sess["project_dir"], "worker session project_dir"
    else:
        resolved, src = None, "none"
    notes.append(f"resolved_dir source: {src}")
    integrity = classify_integrity(cell_dir, tpd, resolved)
    self_rep = None
    p = os.path.join(cell_dir, IDENTITY_FILE)
    if os.path.exists(p):
        with open(p, encoding="utf-8", errors="replace") as f:
            self_rep = f.read().strip()[:200] or None
    if art.get("new_commits") == 0:
        notes.append("no new commit beyond baseline in cell repo")
    return {
        "role": cell["role"], "llm": cell["llm"], "task_type": cell["task_type"],
        "expected_model": cell["expected_model"], "used_model": used_model, "model_identity": identity,
        "self_reported_model": self_rep, "verification_integrity": integrity,
        "resolved_dir": _real(resolved), "task_project_dir": tpd, "artifact_check": art,
        "thinking_level": cell.get("thinking_level"),
        "model_result": classify_result(identity, integrity, art.get("verify_exit")),
        "task_status": (task or {}).get("status"), "session_id": (task or {}).get("session_id"),
        "notes": "; ".join(notes),
    }


def cmd_collect(a):
    m = load_matrix()
    tds = type_defs(m)
    st = load_state(m, a.run_id, required=True)
    c = Client()
    pid, prd = assert_scratch(c, st)
    tasks = {t["id"]: t for (_, _, _, t) in all_tasks(prd)}
    sidx, _ = sessions_index(c)
    fallback = {}
    rows = []
    for cell in cells(m):
        sc = st["cells"].get(cell["name"]) or {}
        t = tasks.get(sc.get("task_id")) or {}
        ws = sidx.get(t.get("session_id")) if t.get("session_id") else None
        vid = (t.get("verification") or {}).get("verifier_id")
        vs = sidx.get(vid) if vid else None
        used, source = session_model(ws), "session_record.model"
        if used is None:
            if cell["llm"] not in fallback:
                try:
                    fallback[cell["llm"]] = llm_test(c, cell["llm"]).get("used_model")
                except EvalError as e:
                    fallback[cell["llm"]] = None
                    log(f"post-run llm_test {cell['llm']} failed: {e}")
            used, source = fallback[cell["llm"]], "post-run llm_test (no per-session model exposed)"
        cdir = os.path.join(REPO, sc.get("dir", ""))
        art = artifact_check(cdir, sc.get("baseline_commit"),
                             os.path.join(fix_dir(m), tds[cell["task_type"]]["verify"]),
                             os.path.join(run_dir(m, a.run_id), "verify-logs", cell["name"] + ".log"))
        row = build_cell_row(cell, cdir, t, ws, vs, used, source, art)
        rows.append(row)
        log(f"{cell['name']}: identity={row['model_identity']} integrity={row['verification_integrity']} "
            f"verify_exit={art.get('verify_exit')} result={row['model_result']}")
    st["collected"] = {"at": now_iso(), "cells": rows}
    save_state(m, a.run_id, st)
    log(f"COLLECT OK — {len(rows)} cells (run `report` to write results)")


# --------------------------------------------------------------------------
# report
# --------------------------------------------------------------------------

def build_results(st):
    pf = st.get("preflight") or {}
    return {"run_id": st.get("run_id"), "preflight": pf.get("rows") or [], "preflight_passed": preflight_passed(st),
            "prd_id": st.get("eval_prd_id"), "cells": (st.get("collected") or {}).get("cells") or []}


def render_md(doc, m):
    out = [f"# Open-model matrix eval — run {doc.get('run_id')}", "",
           f"Automaton: `{doc.get('prd_id')}`  ", f"Preflight passed: **{doc['preflight_passed']}**", "",
           "## Preflight", "", "| role | llm | expected | used | ok |", "|---|---|---|---|---|"]
    for r in doc["preflight"]:
        out.append(f"| {r['role']} | {r['llm']} | {r['expected_model']} | {r['used_model']} | {'yes' if r['ok'] else 'NO'} |")
    wrong = [x for x in doc["cells"] if x["model_identity"] != "ok"]
    out += ["", "## Wrong model cells", ""]
    out += [f"- {x['role']}-{x['task_type']}: expected `{x['expected_model']}`, used `{x['used_model']}` "
            f"({x['model_identity']}; self-report `{x['self_reported_model']}`)" for x in wrong] or ["None."]
    sus = [x for x in doc["cells"] if x["verification_integrity"] != "ok"]
    out += ["", "## Suspect integrity cells", ""]
    out += [f"- {x['role']}-{x['task_type']}: task_project_dir `{x['task_project_dir']}`, resolved `{x['resolved_dir']}`"
            for x in sus] or ["None."]
    out += ["", "## Model results", "", "Trustworthy cells only (identity ok AND integrity ok); `—` = untrusted/not run.", ""]
    types = m["task_types"]
    out.append("| role | " + " | ".join(types) + " |")
    out.append("|---|" + "---|" * len(types))
    idx = {(x["role"], x["task_type"]): x for x in doc["cells"]}
    for r in m["roles"]:
        vals = [(idx.get((r["role"], t)) or {}).get("model_result") or "—" for t in types]
        out.append(f"| {r['role']} | " + " | ".join(vals) + " |")
    out += ["", "## Thinking level actually used", "" ,
           "`default` = the level was left at pi's own default (null in the matrix).", ""]
    out.append("| role | " + " | ".join(types) + " |")
    out.append("|---|" + "---|" * len(types))
    for r in m["roles"]:
        vals = [thinking_disp((idx.get((r["role"], t)) or {}).get("thinking_level")) for t in types]
        out.append(f"| {r['role']} | " + " | ".join(vals) + " |")
    return "\n".join(out) + "\n"


def cmd_report(a):
    m = load_matrix()
    st = load_state(m, a.run_id, required=True)
    doc = redact(build_results(st), m, load_public_matrix())
    d = os.path.join(REPO, RUNS_REL, f"{st.get('run_date') or _dt.date.today().isoformat()}-open-model-matrix")
    os.makedirs(d, exist_ok=True)
    write_json_atomic(os.path.join(d, "results.json"), doc)
    with open(os.path.join(d, "results.md"), "w", encoding="utf-8") as f:
        f.write(render_md(doc, m))
    log(f"REPORT OK — {os.path.relpath(d, REPO)}/results.{{json,md}}")


# --------------------------------------------------------------------------
# cleanup
# --------------------------------------------------------------------------

def cmd_cleanup(a):
    m = load_matrix()
    st = load_state(m, a.run_id, required=True)
    if st.get("eval_prd_id") and not a.offline:
        c = Client()
        pid, _ = assert_scratch(c, st)
        log(f"Automaton {pid}: {prd_cancel_quiet(c, pid)}")
    root = run_dir(m, a.run_id)
    for name in list((st.get("cells") or {}).keys()):
        d = os.path.join(root, name)
        if os.path.isdir(d) and os.path.realpath(d).startswith(os.path.realpath(root) + os.sep):
            emt.shutil.rmtree(d)
            log(f"removed {os.path.relpath(d, REPO)}")
    st["cleaned_at"] = now_iso()
    save_state(m, a.run_id, st)
    log("CLEANUP OK")


# --------------------------------------------------------------------------
# self-test (offline)
# --------------------------------------------------------------------------

def cmd_self_test(_a):
    import tempfile
    m = load_matrix()
    fails = []

    def check(cond, msg):
        if not cond:
            fails.append(msg)
        log(("PASS " if cond else "FAIL ") + msg)

    with tempfile.TemporaryDirectory() as tmp:
        parent = os.path.join(tmp, "run")
        cdir = os.path.join(parent, "cpu-docs")
        os.makedirs(cdir)
        cell = {"role": "cpu", "llm": "pi-x", "expected_model": "qwen3:1.7b", "task_type": "docs"}
        good_art = {"verify_exit": 0, "new_commits": 1}
        task = {"project_dir": cdir, "status": "completed"}
        ws = {"project_dir": cdir, "model": "qwen3:1.7b"}

        r = build_cell_row(cell, cdir, task, ws, None, "qwen3:1.7b", "session_record.model", good_art)
        check(r["model_identity"] == "ok" and r["verification_integrity"] == "ok" and r["model_result"] == "pass",
              "correct cell -> pass")
        check(r["thinking_level"] is None, "cell row records thinking_level (null when the role leaves pi's default)")
        r = build_cell_row(cell, cdir, task, ws, None, "granite4.2:8b", "session_record.model", good_art)
        check(r["model_identity"] == "wrong_model" and r["model_result"] is None,
              "wrong used_model -> wrong_model, model_result null")
        r = build_cell_row(cell, cdir, task, ws, None, None, "none", good_art)
        check(r["model_identity"] == "unknown" and r["model_result"] is None, "unknown model -> never a pass")
        r = build_cell_row(cell, cdir, {"project_dir": parent}, {"project_dir": parent}, None, "qwen3:1.7b",
                           "session_record.model", good_art)
        check(r["verification_integrity"] == "suspect" and r["model_result"] is None,
              "ProjectDir points to parent -> suspect, model_result null")
        r = build_cell_row(cell, cdir, task, ws, {"project_dir": parent}, "qwen3:1.7b", "session_record.model", good_art)
        check(r["verification_integrity"] == "suspect", "verifier ran in parent dir -> suspect")
        r = build_cell_row(cell, cdir, task, ws, None, "qwen3:1.7b", "session_record.model",
                           {"verify_exit": 1, "new_commits": 0})
        check(r["model_result"] == "fail", "missing artifact -> fail")

        rows = [preflight_row(x, resp={"used_model": x["model"]}) for x in m["roles"]]
        check(preflight_passed({"preflight": {"rows": rows, "passed": True}}), "matching preflight passes")
        bad = rows[:3] + [preflight_row(m["roles"][3], resp={"used_model": "gpt-oss:20b"})]
        st_bad = {"preflight": {"rows": bad, "passed": all(x["ok"] for x in bad)}}
        try:
            require_preflight(st_bad)
            blocked = False
        except EvalError:
            blocked = True
        check(blocked, "preflight mismatch blocks create-prd")
        err_row = preflight_row(m["roles"][0], err="HTTP 400: session-backend kind")
        check(not err_row["ok"], "preflight error row is not ok")
        try:
            require_preflight({})
            blocked = False
        except EvalError:
            blocked = True
        check(blocked, "missing preflight blocks create-prd")

        doc = {"run_id": "t", "prd_id": "p", "preflight": rows, "preflight_passed": True,
               "cells": [build_cell_row({**cell, "role": ro["role"], "task_type": tt}, cdir, task, ws, None,
                                        "qwen3:1.7b", "s", good_art) for ro in m["roles"] for tt in m["task_types"]]}
        md = render_md(doc, m)
        check(all(s in md for s in ("## Preflight", "## Wrong model cells", "## Suspect integrity cells",
                                    "## Model results", "## Thinking level actually used")), "report has all sections")
        check("default" in md.split("## Thinking level actually used")[1], "unpinned thinking level renders as 'default'")

        badm = {**m, "roles": m["roles"][:-1] + [dict(m["roles"][-1], thinking_level="xhigh")],
                "prd_thinking_level": None}
        try:
            _check_matrix(badm)
            ok = False
        except EvalError:
            ok = True
        check(ok, "invalid thinking_level rejected by _check_matrix")
        nobase = {**m, "roles": [dict(r, role=r["role"] + "-x") for r in m["roles"]]}
        try:
            _check_matrix(nobase)
            ok = False
        except EvalError:
            ok = True
        check(ok, "missing baseline role rejected by _check_matrix")
        empty = {**m, "roles": []}
        try:
            _check_matrix(empty)
            ok = False
        except EvalError:
            ok = True
        check(ok, "empty roles list rejected by _check_matrix")

        local = {**m, "roles": [{**r, "llm": f"pi-host{i}-x", "node": f"host{i}"}
                                for i, r in enumerate(m["roles"])]}
        public = load_public_matrix()
        raw = {"cells": [{"llm": "pi-host0-x", "node": "host0",
                          "notes": "pi-host0-x on host0, not pi-host0-xl",
                          "task_project_dir": os.path.join(REPO, ".datawatch", "eval", "c")}]}
        red = redact(raw, local, public)["cells"][0]
        pl = public["roles"][0]["llm"]
        check(red["llm"] == pl and red["node"] == public["roles"][0]["node"],
              "redact maps local llm/node names to tracked placeholders")
        check(red["notes"] == f"{pl} on host0, not pi-host0-xl",
              "redact replaces whole llm names only; node names only in node fields")
        check(red["task_project_dir"] == os.path.join(".datawatch", "eval", "c"),
              "redact makes in-repo paths repo-relative")
        u = getpass.getuser()
        if len(u) >= 3:
            out = redact({"m": f"{u}_model", "p": os.path.join(os.path.expanduser("~"), "x")}, local, public)
            check(out["m"] == "<user>_model" and u not in out["p"],
                  "redact scrubs the local username and home dir")
    if fails:
        log(f"SELF-TEST FAILED ({len(fails)})")
        return 1
    log("SELF-TEST OK")
    return 0


def main(argv=None):
    p = argparse.ArgumentParser(prog="eval_open_model_matrix.py")
    sp = p.add_subparsers(dest="cmd", required=True)
    for name in ("preflight", "setup", "create-prd", "run", "collect", "report", "cleanup"):
        s = sp.add_parser(name)
        s.add_argument("--run-id", required=True)
        if name == "create-prd":
            s.add_argument("--decompose-timeout", type=int, default=900)
        if name == "run":
            s.add_argument("--timeout", type=int, default=6 * 3600)
            s.add_argument("--poll", type=int, default=30)
        if name == "cleanup":
            s.add_argument("--offline", action="store_true")
    sp.add_parser("self-test")
    a = p.parse_args(argv)
    fn = {"preflight": cmd_preflight, "setup": cmd_setup, "create-prd": cmd_create_prd, "run": cmd_run,
          "collect": cmd_collect, "report": cmd_report, "cleanup": cmd_cleanup, "self-test": cmd_self_test}[a.cmd]
    try:
        return fn(a) or 0
    except EvalError as e:
        print(f"ERROR: {e}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
