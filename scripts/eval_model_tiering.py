#!/usr/bin/env python3
"""Model/effort tiering eval driver: JSON, transcript, scoring and report logic.

Invoked by scripts/eval-model-tiering.sh as `python3 -I scripts/eval_model_tiering.py
<subcommand> ...`. Stdlib only. Fixtures: testdata/eval/model-tiering/.
State: .datawatch/eval/model-tiering/<run-id>/state.json (gitignored).

Three touchpoints:
  execution    - setup / create-prd / run / collect (one scratch Automaton, one
                 task per cell, per-task LLM pinned via set_task_llm)
  decompose    - decompose-run (one scratch Automaton per tier, decompose only,
                 scored against decompose/ground-truth-structure.json)
  verification - vt-* internals driven by the bash verify-tier-run wrapper; the
                 ONLY code path allowed to write daemon global config, and only
                 through verify_tier_config_put() under the bash capture/restore
                 trap.

Every other subcommand is read-only with respect to daemon global config; the
HTTP helper refuses any non-GET request to the autonomous config route unless a
verify-tier subcommand has armed the write gate.
"""

import argparse
import datetime as _dt
import glob
import hashlib
import json
import os
import re
import shutil
import ssl
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(HERE)
FIX_REL = "testdata/eval/model-tiering"
FIX = os.path.join(REPO, FIX_REL)
STATE_ROOT_REL = ".datawatch/eval/model-tiering"
STATE_ROOT = os.path.join(REPO, STATE_ROOT_REL)
RUNS_REL = "docs/testing/runs"

DEFAULT_BASE = "https://127.0.0.1:8443"
EXEC_TITLE_PREFIX = "[eval] model-tiering"
ACTOR = "eval-model-tiering"
TERMINAL_TASK = {"completed", "failed", "blocked", "cancelled"}
TERMINAL_PRD = {"completed", "failed", "cancelled", "rejected", "archived"}
VERIFY_KEYS = ("verification_backend", "verification_model", "verification_effort")

REQUIRED_FIXTURES = [
    "matrix.json",
    "rates.json",
    "transcript-sample.jsonl",
    "transcript-sample.expected.json",
    "decompose/ground-truth-spec.md",
    "decompose/ground-truth-structure.json",
    "decompose/rubric.md",
    "review/candidate-pass.diff",
    "review/candidate-fail.diff",
    "review/cell-names.json",
]


class EvalError(Exception):
    """Fatal, operator-facing error; message is printed and exit code is 1."""


def log(msg):
    print(f"[{_dt.datetime.now().strftime('%H:%M:%S')}] {msg}", flush=True)


def now_iso():
    return _dt.datetime.now(_dt.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


# --------------------------------------------------------------------------
# fixtures
# --------------------------------------------------------------------------

def check_fixtures(fix=FIX):
    if not os.path.isdir(fix):
        raise EvalError(f"fixtures missing: {fix} does not exist (do not invent them; add the fixture commit first)")
    missing = [p for p in REQUIRED_FIXTURES if not os.path.isfile(os.path.join(fix, p))]
    matrix_path = os.path.join(fix, "matrix.json")
    if os.path.isfile(matrix_path):
        m = load_json(matrix_path)
        for key in ("task_types", "execution_tiers", "decompose_tiers", "valid_models", "valid_efforts"):
            if key not in m:
                missing.append(f"matrix.json:{key}")
        for tt in m.get("task_types", []):
            for k in ("spec", "verify"):
                if tt.get(k) and not os.path.isfile(os.path.join(fix, tt[k])):
                    missing.append(tt[k])
    if missing:
        raise EvalError("fixtures missing: " + ", ".join(missing) + " (stop: do not invent fixtures)")


def load_json(path):
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def write_json_atomic(path, data):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    fd, tmp = tempfile.mkstemp(dir=os.path.dirname(path), prefix=".tmp-", suffix=".json")
    with os.fdopen(fd, "w", encoding="utf-8") as f:
        json.dump(data, f, indent=2, sort_keys=False)
        f.write("\n")
    os.replace(tmp, path)


def matrix():
    return load_json(os.path.join(FIX, "matrix.json"))


def rates():
    return load_json(os.path.join(FIX, "rates.json"))


def tier_by_id(tiers, tid):
    for t in tiers:
        if t["id"] == tid:
            return t
    raise EvalError(f"unknown tier {tid!r} (known: {', '.join(t['id'] for t in tiers)})")


# --------------------------------------------------------------------------
# state
# --------------------------------------------------------------------------

def valid_run_id(run_id):
    if not run_id or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]{0,63}", run_id):
        raise EvalError(f"invalid --run-id {run_id!r} (allowed: [A-Za-z0-9._-], max 64)")
    return run_id


def run_dir(run_id):
    return os.path.join(STATE_ROOT, valid_run_id(run_id))


def state_path(run_id):
    return os.path.join(run_dir(run_id), "state.json")


def load_state(run_id, required=False):
    p = state_path(run_id)
    if not os.path.isfile(p):
        if required:
            raise EvalError(f"no state for run-id {run_id!r} at {os.path.relpath(p, REPO)} (run preflight/setup first)")
        return {"run_id": run_id}
    return load_json(p)


def save_state(run_id, st):
    st["updated_at"] = now_iso()
    write_json_atomic(state_path(run_id), st)


# --------------------------------------------------------------------------
# HTTP (stdlib) — with the global-config write gate
# --------------------------------------------------------------------------

_CONFIG_PATH = "/api/autonomous/config"
_CONFIG_WRITE_ARMED = False  # only verify-tier subcommands arm this


class Client:
    def __init__(self, base=None, token=None, insecure=None, timeout=60):
        self.base = (base or os.environ.get("DW_BASE") or DEFAULT_BASE).rstrip("/")
        self.token = token if token is not None else os.environ.get("DW_BASE_TOKEN", "")
        if insecure is None:
            insecure = os.environ.get("DW_BASE_INSECURE", "1") != "0"
        self.ctx = None
        if self.base.startswith("https"):
            self.ctx = ssl.create_default_context()
            if insecure:  # local daemon uses a self-signed cert, same default as release-smoke.sh
                self.ctx.check_hostname = False
                self.ctx.verify_mode = ssl.CERT_NONE
        self.timeout = timeout

    def request(self, method, path, body=None, ok_statuses=(200, 201, 202)):
        if path.split("?")[0].rstrip("/") == _CONFIG_PATH and method != "GET" and not _CONFIG_WRITE_ARMED:
            raise EvalError(f"refusing {method} {_CONFIG_PATH}: global config writes are only allowed inside verify-tier-* subcommands")
        url = self.base + path
        data = None
        headers = {"Accept": "application/json"}
        if body is not None:
            data = json.dumps(body).encode()
            headers["Content-Type"] = "application/json"
        if self.token:
            headers["Authorization"] = "Bearer " + self.token
        req = urllib.request.Request(url, data=data, method=method, headers=headers)
        try:
            with urllib.request.urlopen(req, timeout=self.timeout, context=self.ctx) as resp:
                raw = resp.read()
                status = resp.status
        except urllib.error.HTTPError as e:
            raw = e.read()
            status = e.code
        except (urllib.error.URLError, OSError) as e:
            raise EvalError(f"{method} {url}: {e}")
        if status not in ok_statuses:
            raise EvalError(f"{method} {path}: HTTP {status}: {raw[:400].decode(errors='replace').strip()}")
        if not raw:
            return {}
        try:
            return json.loads(raw)
        except json.JSONDecodeError:
            return {"_raw": raw.decode(errors="replace")}

    def get(self, path):
        return self.request("GET", path)

    def post(self, path, body=None):
        return self.request("POST", path, body if body is not None else {})


# --- autonomous PRD helpers (never touch global config) ---------------------

def prd_get(c, prd_id):
    return c.get(f"/api/autonomous/prds/{prd_id}")


def prd_action(c, prd_id, action, body=None):
    return c.post(f"/api/autonomous/prds/{prd_id}/{action}", body)


def prd_create(c, spec, project_dir):
    prd = c.post("/api/autonomous/prds", {"spec": spec, "project_dir": project_dir})
    if not prd.get("id"):
        raise EvalError(f"create PRD: no id in response: {str(prd)[:300]}")
    return prd


def prd_set_title(c, prd_id, title):
    return c.request("PATCH", f"/api/autonomous/prds/{prd_id}", {"title": title, "actor": ACTOR})


def prd_wait_planned(c, prd_id, timeout_s, poll_s=10):
    """Poll until the PRD leaves draft/planning after a decompose kick."""
    deadline = time.time() + timeout_s
    seen_planning = False
    while True:
        prd = prd_get(c, prd_id)
        st = prd.get("status")
        if st == "planning":
            seen_planning = True
        elif st != "draft" or seen_planning:
            # draft after planning = decompose failed and rolled back
            return prd
        if st == "draft":
            try:
                ds = c.get(f"/api/autonomous/prds/{prd_id}/decompose/status")
                if ds.get("status") == "error":
                    return prd
            except EvalError:
                pass
        if time.time() > deadline:
            raise EvalError(f"PRD {prd_id} still {st!r} after {timeout_s}s")
        time.sleep(poll_s)


def prd_cancel_quiet(c, prd_id):
    try:
        prd_action(c, prd_id, "cancel")
        return "cancelled"
    except EvalError as e1:
        try:
            c.request("DELETE", f"/api/autonomous/prds/{prd_id}")
            return "archived"
        except EvalError as e2:
            return f"cancel failed: {e1}; delete failed: {e2}"


def all_tasks(prd):
    out = []
    for si, s in enumerate(prd.get("stories") or []):
        for ti, t in enumerate(s.get("tasks") or []):
            out.append((si, ti, s, t))
    return out


def sessions_index(c):
    data = c.get("/api/sessions")
    lst = data if isinstance(data, list) else data.get("sessions", [])
    idx = {}
    for s in lst or []:
        for k in ("id", "full_id"):
            if s.get(k):
                idx[s[k]] = s
    return idx, (lst or [])


# --------------------------------------------------------------------------
# transcripts + cost (mirrors internal/session/claude_usage.go path logic)
# --------------------------------------------------------------------------

def claude_session_uuid(full_id):
    """claudecode.deriveSessionUUID / session.claudeSessionUUID, byte for byte."""
    ns = bytes.fromhex("6ba7b8109dad11d180b400c04fd430c8")
    s = bytearray(hashlib.sha1(ns + full_id.encode()).digest())  # nosec - UUIDv5 derivation, not security
    s[6] = (s[6] & 0x0F) | 0x50
    s[8] = (s[8] & 0x3F) | 0x80
    h = s.hex()
    return f"{h[0:8]}-{h[8:12]}-{h[12:16]}-{h[16:20]}-{h[20:32]}"


def claude_transcript_path(project_dir, full_id, home=None):
    if not project_dir or not full_id:
        return ""
    escaped = project_dir.replace("/", "-").replace(".", "-")
    home = home or os.path.expanduser("~")
    return os.path.join(home, ".claude", "projects", escaped, claude_session_uuid(full_id) + ".jsonl")


def model_family(model, rates_doc):
    for fam, prefix in rates_doc.get("model_match", {}).items():
        if model and model.startswith(prefix):
            return fam
    if model in rates_doc.get("models", {}):
        return model
    return None


TOKEN_KEYS = ("input_tokens", "output_tokens", "cache_creation_input_tokens", "cache_read_input_tokens")


def parse_transcripts(paths, rates_doc):
    """Sum message.usage over assistant lines of one or more JSONL files.

    Lines sharing a message.id are counted once (Claude Code can repeat a
    message's usage on every content-block line). Sidechain lines are
    included. Unknown models are reported but not priced.
    """
    per_model = {}
    seen = set()
    assistant = non_assistant = dupes = 0
    for path in paths:
        with open(path, encoding="utf-8", errors="replace") as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    e = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if e.get("type") != "assistant":
                    non_assistant += 1
                    continue
                msg = e.get("message") or {}
                usage = msg.get("usage")
                if not usage:
                    continue
                mid = msg.get("id")
                if mid:
                    if mid in seen:
                        dupes += 1
                        continue
                    seen.add(mid)
                assistant += 1
                model = msg.get("model") or "unknown"
                pm = per_model.setdefault(model, {k: 0 for k in TOKEN_KEYS})
                for k in TOKEN_KEYS:
                    pm[k] += int(usage.get(k) or 0)
    by_family = {}
    unpriced = []
    for model, toks in per_model.items():
        fam = model_family(model, rates_doc)
        if fam is None:
            unpriced.append(model)
            fam = model
        bf = by_family.setdefault(fam, {k: 0 for k in TOKEN_KEYS})
        for k in TOKEN_KEYS:
            bf[k] += toks[k]
    total = {k: 0 for k in TOKEN_KEYS}
    total_cost = 0.0
    for fam, toks in by_family.items():
        r = rates_doc["models"].get(fam)
        cost = None
        if r:
            cost = (toks["input_tokens"] * r["input"] + toks["output_tokens"] * r["output"]
                    + toks["cache_creation_input_tokens"] * r["cache_write"]
                    + toks["cache_read_input_tokens"] * r["cache_read"]) / 1e6
            cost = round(cost, 8)
            total_cost += cost
        toks["cost_usd"] = cost
        for k in TOKEN_KEYS:
            total[k] += toks[k]
    total["cost_usd"] = round(total_cost, 8)
    return {
        "assistant_entries": assistant,
        "non_assistant_entries": non_assistant,
        "duplicate_message_lines_skipped": dupes,
        "per_model": by_family,
        "observed_models": sorted(per_model),
        "unpriced_models": sorted(unpriced),
        "total": total,
    }


def session_cost(sess, rates_doc, home=None):
    """Locate a session's transcript(s) the way claude_usage.go does and price them."""
    out = {"transcript": None, "tokens": None, "observed_models": [], "cost_usd_model_rates": None,
           "datawatch_est_cost_usd": None, "datawatch_tokens": None, "cost_method": "none"}
    if not sess:
        out["cost_method"] = "no-session"
        return out
    out["datawatch_est_cost_usd"] = sess.get("est_cost_usd", 0.0)
    out["datawatch_tokens"] = {"tokens_in": sess.get("tokens_in", 0), "tokens_out": sess.get("tokens_out", 0)}
    path = claude_transcript_path(sess.get("project_dir", ""), sess.get("full_id", ""), home=home)
    if not path or not os.path.isfile(path):
        out["transcript"] = path or None
        out["cost_method"] = "transcript-missing"
        return out
    paths = [path]
    sub = os.path.join(path[:-len(".jsonl")], "subagents")
    paths += sorted(glob.glob(os.path.join(sub, "*.jsonl")))
    t = parse_transcripts(paths, rates_doc)
    out.update({
        "transcript": path,
        "transcript_files": len(paths),
        "tokens": {k: t["total"][k] for k in TOKEN_KEYS},
        "observed_models": t["observed_models"],
        "cost_usd_model_rates": t["total"]["cost_usd"],
        "cost_method": "session-transcript",
    })
    return out


# --------------------------------------------------------------------------
# decompose scoring (decompose/rubric.md, C1..C15, max 100)
# --------------------------------------------------------------------------

RUBRIC_POINTS = {"C1": 10, "C2": 10, "C3": 5, "C4": 10, "C5": 10, "C6": 10, "C7": 5, "C8": 5,
                 "C9": 5, "C10": 5, "C11": 5, "C12": 10, "C13": 5, "C14": 3, "C15": 2}
RELEASE_FILES = ("cmd/datawatch/main.go", "internal/server/api.go", "CHANGELOG.md")


def normalise_structure(prd):
    """autonomous_prd_get response -> positional stories/tasks (ids mapped to keys)."""
    stories, tasks = [], []
    sid_key, tid_key = {}, {}
    for si, s in enumerate(prd.get("stories") or []):
        skey = f"S{si + 1}"
        sid_key[s.get("id") or skey] = skey
        for ti, t in enumerate(s.get("tasks") or []):
            tid_key[t.get("id") or f"{skey}.T{ti + 1}"] = f"{skey}.T{ti + 1}"
    for si, s in enumerate(prd.get("stories") or []):
        skey = f"S{si + 1}"
        stories.append({"key": skey, "title": s.get("title", ""),
                        "depends_on": [sid_key.get(d, "?" + str(d)) for d in (s.get("depends_on") or [])]})
        for ti, t in enumerate(s.get("tasks") or []):
            tasks.append({"key": f"{skey}.T{ti + 1}", "story": skey, "title": t.get("title", ""),
                          "spec": t.get("spec", "") or "", "files": list(t.get("files") or []),
                          "depends_on": [tid_key.get(d, "?" + str(d)) for d in (t.get("depends_on") or [])]})
    return {"stories": stories, "tasks": tasks}


def _has_cycle(nodes, edges):
    color = {n: 0 for n in nodes}

    def visit(n):
        color[n] = 1
        for m in edges.get(n, ()):
            if m not in color:
                continue
            if color[m] == 1 or (color[m] == 0 and visit(m)):
                return True
        color[n] = 2
        return False

    return any(color[n] == 0 and visit(n) for n in list(nodes))


def _task_edges(norm):
    """task key -> set(task keys it depends on): task edges + expanded story edges."""
    by_story = {}
    for t in norm["tasks"]:
        by_story.setdefault(t["story"], []).append(t["key"])
    sdeps = {s["key"]: s["depends_on"] for s in norm["stories"]}
    edges = {}
    for t in norm["tasks"]:
        e = set(d for d in t["depends_on"] if not d.startswith("?"))
        for ds in sdeps.get(t["story"], []):
            e.update(by_story.get(ds, []))
        edges[t["key"]] = e
    return edges


def _reachable(start, edges):
    seen, stack = set(), list(edges.get(start, ()))
    while stack:
        n = stack.pop()
        if n in seen:
            continue
        seen.add(n)
        stack.extend(edges.get(n, ()))
    return seen


def score_structure(norm, gt):
    stories, tasks = norm["stories"], norm["tasks"]
    skeys = {s["key"] for s in stories}
    tkeys = {t["key"] for t in tasks}
    passed = {}
    passed["C1"] = abs(len(stories) - gt["story_count"]) <= 1
    passed["C2"] = abs(len(tasks) - gt["task_count"]) <= 1
    per_story = {s["key"]: 0 for s in stories}
    for t in tasks:
        per_story[t["story"]] = per_story.get(t["story"], 0) + 1
    passed["C3"] = bool(stories) and all(n >= 1 for n in per_story.values())
    passed["C4"] = (all(d in skeys for s in stories for d in s["depends_on"])
                    and all(d in tkeys for t in tasks for d in t["depends_on"]))
    sedges = {s["key"]: set(d for d in s["depends_on"] if d in skeys) for s in stories}
    tedges = _task_edges(norm)
    passed["C5"] = not _has_cycle(skeys, sedges) and not _has_cycle(tkeys, tedges)
    c6 = [t["key"] for t in tasks if "internal/session/tracker.go" in t["files"]]
    passed["C6"] = bool(c6)
    passed["C7"] = any(re.match(r"^internal/session/.*_test\.go$", f) for t in tasks for f in t["files"])
    ts = lambda t: t["title"] + "\n" + t["spec"]  # noqa: E731
    passed["C8"] = any(re.search(r"repro|reproduc", ts(t), re.I) for t in tasks)
    passed["C9"] = any(re.search(r"remediat|shrink", ts(t), re.I) and re.search(r"bloated|ab22|9245|ddeb", ts(t), re.I)
                       for t in tasks)
    c10 = [t["key"] for t in tasks if all(f in t["files"] for f in RELEASE_FILES)]
    passed["C10"] = bool(c10)
    passed["C11"] = any("docs/plans/README.md" in t["files"] for t in tasks)
    # Deterministic reading of "every C10 task transitively depends on a C6
    # task": requires at least one C10 and one C6 task (no vacuous pass), and
    # a task never satisfies this by depending on itself.
    passed["C12"] = bool(c10) and bool(c6) and all(bool(_reachable(k, tedges) & (set(c6) - {k})) for k in c10)
    c13 = True
    for t in tasks:
        for ln in t["spec"].splitlines():
            if re.search(r"max_parallel_tasks", ln) and not re.search(r"do not|don't|leave|unchanged|not change", ln, re.I):
                c13 = False
        if any(re.search(r"datawatch\.ya?ml$|config\.ya?ml$", f) for f in t["files"]):
            c13 = False
    passed["C13"] = c13
    passed["C14"] = not any(f.startswith("internal/session/") and f != "internal/session/tracker.go"
                            and not f.endswith("_test.go") for t in tasks for f in t["files"])
    allf = set(gt["all_files"])
    passed["C15"] = all(f in allf or re.match(r"^(scripts|docs)/", f) or re.match(r"^\.datawatch/verify/", f)
                        for t in tasks for f in t["files"])
    score = sum(RUBRIC_POINTS[k] for k, v in passed.items() if v)
    n_edges = sum(len(s["depends_on"]) for s in stories) + sum(len(t["depends_on"]) for t in tasks)
    return {
        "score": score,
        "max": sum(RUBRIC_POINTS.values()),
        "passed": sorted((k for k, v in passed.items() if v), key=lambda k: int(k[1:])),
        "failed": sorted((k for k, v in passed.items() if not v), key=lambda k: int(k[1:])),
        "counts": {"stories": len(stories), "tasks": len(tasks), "edges": n_edges},
    }


def gt_as_prd(gt):
    """ground-truth-structure.json -> an autonomous_prd_get-shaped dict."""
    return {"stories": [{"id": s["id"], "title": s["title"],
                         "depends_on": [next(x["id"] for x in gt["stories"] if x["key"] == d) for d in s["depends_on"]],
                         "tasks": [{"id": t["id"], "title": t["title"], "spec": "", "files": t["files"],
                                    "depends_on": [next(x["id"] for x in s["tasks"] if x["key"] == d) for d in t["depends_on"]]}
                                   for t in s["tasks"]]}
                        for s in gt["stories"]]}


# --------------------------------------------------------------------------
# results.json + report
# --------------------------------------------------------------------------

def results_path(st):
    day = st.get("run_date") or _dt.date.today().isoformat()
    return os.path.join(REPO, RUNS_REL, f"{day}-model-tiering", "results.json")


def row_key(r):
    return (r.get("touchpoint"), r.get("task_type"), r.get("tier"), r.get("rep"))


def merge_results(path, st, rows, extra_top=None):
    doc = load_json(path) if os.path.isfile(path) else {}
    doc.setdefault("run_id", st.get("run_id"))
    doc.setdefault("started_at", st.get("started_at") or now_iso())
    for k in ("eval_prd_id", "repo_head_at_run"):
        if st.get(k):
            doc[k] = st[k]
    doc.setdefault("eval_prd_id", None)
    doc.setdefault("repo_head_at_run", st.get("repo_head_at_run"))
    doc["rates_as_of"] = rates().get("as_of")
    doc["observed_default"] = st.get("observed_default_live") or matrix().get("observed_default")
    if extra_top:
        doc.update(extra_top)
    new_keys = {row_key(r) for r in rows}
    cells = [r for r in doc.get("cells", []) if row_key(r) not in new_keys]
    cells.extend(rows)
    doc["cells"] = cells
    doc["finished_at"] = now_iso()
    write_json_atomic(path, doc)
    return doc


def _fmt_cost(v):
    return "—" if v is None else f"${v:.4f}"


def _fmt_tokens(t):
    if not t:
        return "—"
    return f"in {t.get('input_tokens', 0)} / out {t.get('output_tokens', 0)} / cw {t.get('cache_creation_input_tokens', 0)} / cr {t.get('cache_read_input_tokens', 0)}"


def _fmt_secs(s):
    if s is None:
        return "—"
    s = int(s)
    return f"{s // 60}m{s % 60:02d}s"


def render_report(doc):
    cells = doc.get("cells", [])
    ex = [c for c in cells if c.get("touchpoint") == "execution"]
    de = [c for c in cells if c.get("touchpoint") == "decompose"]
    ve = [c for c in cells if c.get("touchpoint") == "verification"]
    L = [f"# Model-tiering eval results — run `{doc.get('run_id')}`", "",
         f"- Repo HEAD at run: `{doc.get('repo_head_at_run')}`",
         f"- Execution scratch Automaton: `{doc.get('eval_prd_id')}`",
         f"- Started: {doc.get('started_at')} · Finished: {doc.get('finished_at')}",
         f"- Model rates as of: {doc.get('rates_as_of')} (`{FIX_REL}/rates.json`)", ""]
    od = doc.get("observed_default") or {}
    if od:
        L += [f"- Observed default: backend `{od.get('backend')}`, model `{od.get('model')}`, effort `{od.get('effort')}`", ""]

    L += ["## Execution", ""]
    if ex:
        L += ["| Task type | Tier | Pass | Wall time | Tokens | Cost (model rates) | Cost (datawatch flat) |",
              "|---|---|---|---|---|---|---|"]
        for c in sorted(ex, key=lambda c: (c.get("task_type") or "", c.get("tier") or "", c.get("rep") or 0)):
            L.append(f"| {c.get('task_type')} | {c.get('tier')} | {'yes' if c.get('passed') else 'no'} | "
                     f"{_fmt_secs(c.get('wall_seconds'))} | {_fmt_tokens(c.get('tokens'))} | "
                     f"{_fmt_cost(c.get('cost_usd_model_rates'))} | {_fmt_cost(c.get('datawatch_est_cost_usd'))} |")
        L += ["", "**Aggregates by tier**", "", "| Tier | Cells | Pass rate | Total cost (model rates) | Mean wall time |",
              "|---|---|---|---|---|"]
        for tier in sorted({c.get("tier") for c in ex}):
            cs = [c for c in ex if c.get("tier") == tier]
            p = sum(1 for c in cs if c.get("passed"))
            cost = sum(c.get("cost_usd_model_rates") or 0 for c in cs)
            walls = [c["wall_seconds"] for c in cs if c.get("wall_seconds") is not None]
            L.append(f"| {tier} | {len(cs)} | {p}/{len(cs)} | {_fmt_cost(cost)} | "
                     f"{_fmt_secs(sum(walls) / len(walls)) if walls else '—'} |")
        L += ["", "**Aggregates by task type**", "", "| Task type | Pass rate | Cheapest passing tier |", "|---|---|---|"]
        for tt in sorted({c.get("task_type") for c in ex}):
            cs = [c for c in ex if c.get("task_type") == tt]
            ok = [c for c in cs if c.get("passed") and c.get("cost_usd_model_rates") is not None]
            cheapest = min(ok, key=lambda c: c["cost_usd_model_rates"])["tier"] if ok else "—"
            L.append(f"| {tt} | {sum(1 for c in cs if c.get('passed'))}/{len(cs)} | {cheapest} |")
    else:
        L.append("_No execution cells collected._")
    L.append("")

    L += ["## Decompose", ""]
    if de:
        L += ["| Tier | Structure score | Rubric detail | Cost |", "|---|---|---|---|"]
        for c in sorted(de, key=lambda c: c.get("tier") or ""):
            rd = c.get("rubric_detail") or {}
            detail = f"failed: {', '.join(rd.get('failed') or []) or 'none'}; counts {rd.get('counts')}"
            if c.get("error"):
                detail = "error: " + str(c["error"])[:120]
            score = c.get("structure_score")
            L.append(f"| {c.get('tier')} | {'—' if score is None else f'{score}/100'} | {detail} | {_fmt_cost(c.get('cost_usd_model_rates'))} |")
        scored = [c for c in de if c.get("structure_score") is not None]
        if scored:
            best = max(scored, key=lambda c: (c["structure_score"], -(c.get("cost_usd_model_rates") or 0)))
            L += ["", f"Mean score {sum(c['structure_score'] for c in scored) / len(scored):.1f}/100 over {len(scored)} tiers; "
                  f"best {best['tier']} ({best['structure_score']}/100); total cost "
                  f"{_fmt_cost(sum(c.get('cost_usd_model_rates') or 0 for c in de))}."]
    else:
        L.append("_No decompose cells collected._")
    L.append("")

    L += ["## Verification", ""]
    if ve:
        L += ["| Tier | Daemon verdict | Eval verdict | Agreement y/n | Cost |", "|---|---|---|---|---|"]
        agree = 0
        for c in sorted(ve, key=lambda c: c.get("tier") or ""):
            dv, ev = c.get("daemon_verdict"), ("pass" if c.get("verify_exit") == 0 else "fail") if c.get("verify_exit") is not None else None
            a = (dv is not None and ev is not None and dv == ev)
            agree += 1 if a else 0
            L.append(f"| {c.get('tier')} | {dv or '—'} | {ev or '—'} | {'y' if a else 'n'} | {_fmt_cost(c.get('cost_usd_model_rates'))} |")
        L += ["", f"Agreement {agree}/{len(ve)}. Execution tier fixed at "
              f"`{ve[0].get('execution_tier')}`, task type `{ve[0].get('task_type')}`."]
        not_applied = [c["tier"] for c in ve if c.get("tier_applied") is False]
        if not_applied:
            L += ["", f"**Caveat:** the verifier transcript did not show the requested model for: {', '.join(not_applied)}. "
                  "The daemon's verifier reads a startup-time copy of the autonomous config, so a live config change "
                  "may not reach it without a daemon restart; treat those rows as measuring the unchanged verifier."]
    else:
        L.append("_No verification cells collected._")
    L.append("")
    return "\n".join(L)


# --------------------------------------------------------------------------
# subcommands: common
# --------------------------------------------------------------------------

def git(*args, cwd=REPO, check=True, capture=True):
    r = subprocess.run(["git", *args], cwd=cwd, check=check, text=True,
                       stdout=subprocess.PIPE if capture else None, stderr=subprocess.PIPE if capture else None)
    return (r.stdout or "").strip()


def read_verification_config(c):
    cfg = c.get(_CONFIG_PATH)
    return {k: cfg.get(k, "") or "" for k in VERIFY_KEYS}, cfg


def cmd_preflight(a):
    check_fixtures()
    m = matrix()
    c = Client()
    log(f"daemon: {c.base}")
    try:
        c.get("/api/health")
    except EvalError as e:
        raise EvalError(f"daemon not reachable: {e}")
    llms = c.get("/api/llms").get("llms", [])
    cc = next((l for l in llms if l.get("name") == "claude-code"), None)
    if not cc:
        raise EvalError("claude-code LLM not in /api/llms registry")
    if cc.get("disabled"):
        raise EvalError("claude-code LLM is disabled in the registry")
    models = c.get("/api/llm/claude/models")
    efforts = c.get("/api/llm/claude/efforts")
    valid_m = {x["value"] for x in models.get("aliases", [])} | {x["value"] for x in models.get("full_names", [])}
    valid_e = {x["value"] for x in efforts.get("levels", [])}
    bad = []
    for grid in ("execution_tiers", "decompose_tiers"):
        for t in m[grid]:
            if t.get("backend") != "claude-code":
                bad.append(f"{grid}:{t['id']} backend {t.get('backend')!r} is not claude-code")
            if t["model"] not in valid_m:
                bad.append(f"{grid}:{t['id']} model {t['model']!r} not in daemon claude models")
            if t.get("effort") is not None and t["effort"] not in valid_e:
                bad.append(f"{grid}:{t['id']} effort {t['effort']!r} not in daemon claude efforts")
    if bad:
        raise EvalError("tier validation failed:\n  " + "\n  ".join(bad))
    vcfg, full = read_verification_config(c)
    st = load_state(a.run_id)
    st.setdefault("started_at", now_iso())
    st.setdefault("run_date", _dt.date.today().isoformat())
    st["dw_base"] = c.base
    st["observed_default_live"] = {"backend": "claude-code", "model": cc.get("model"), "effort": None,
                                   "datawatch_effort_hint": cc.get("default_effort"),
                                   "planning_backend": full.get("planning_backend"),
                                   "execution_backend": full.get("execution_backend"),
                                   "verification_backend": full.get("verification_backend"),
                                   "verification_model": full.get("verification_model"),
                                   "source": f"GET /api/llms + GET {_CONFIG_PATH} at {now_iso()}"}
    if "verification_backend_at_start" in st and any(st.get(f"{k}_at_start") != vcfg[k] for k in VERIFY_KEYS):
        raise EvalError(f"live verification config {vcfg} differs from this run's recorded start values "
                        f"{ {k: st.get(k + '_at_start') for k in VERIFY_KEYS} } — a verify-tier run may have been "
                        f"interrupted. Run: scripts/eval-model-tiering.sh verify-tier-restore --run-id {a.run_id}")
    for k in VERIFY_KEYS:
        st[f"{k}_at_start"] = vcfg[k]
    st["verification_backends_at_start"] = full.get("verification_backends") or []
    save_state(a.run_id, st)
    log(f"claude-code registry model={cc.get('model')} default_effort={cc.get('default_effort')}")
    log(f"verification at start: {vcfg} backends_list={st['verification_backends_at_start']}")
    if st["verification_backends_at_start"]:
        log("WARN: autonomous.verification_backends is non-empty; the verifier tries that list before "
            "verification_backend, so verify-tier-run results may not reflect the requested tier")
    log(f"PREFLIGHT OK — state: {os.path.relpath(state_path(a.run_id), REPO)}")


# --------------------------------------------------------------------------
# subcommands: execution touchpoint
# --------------------------------------------------------------------------

def planned_cells(m, tiers=None):
    reps = int(m.get("reps", 1))
    tlist = [t for t in m["execution_tiers"] if tiers is None or t["id"] in tiers]
    for tt in m["task_types"]:
        for t in tlist:
            for r in range(1, reps + 1):
                yield {"name": f"{tt['id']}__{t['id']}__r{r}", "task_type": tt["id"], "tier": t["id"], "rep": r,
                       "model": t["model"], "effort": t.get("effort"), "backend": t["backend"]}


def parse_tiers(arg):
    return None if not arg else [x.strip() for x in arg.split(",") if x.strip()]


def seed_cell(cell_dir, baseline_sha, tt):
    os.makedirs(cell_dir, exist_ok=False)
    arch = subprocess.Popen(["git", "archive", baseline_sha], cwd=REPO, stdout=subprocess.PIPE)
    subprocess.run(["tar", "-x", "-C", cell_dir], stdin=arch.stdout, check=True)
    arch.stdout.close()
    if arch.wait() != 0:
        raise EvalError(f"git archive {baseline_sha} failed")
    for src, dst in (tt.get("cell_inputs") or {}).items():
        shutil.copyfile(os.path.join(FIX, src), os.path.join(cell_dir, dst))
    git("init", "-q", "-b", "main", cwd=cell_dir)
    git("add", "-A", cwd=cell_dir)
    git("-c", "user.email=eval@datawatch.local", "-c", "user.name=datawatch eval", "commit", "-q",
        "--no-verify", "-m", f"baseline {baseline_sha}", cwd=cell_dir)
    # gitignored generated embed needed by `go build` (see fixture README)
    os.makedirs(os.path.join(cell_dir, "internal/docsindex/assets"), exist_ok=True)
    idx = os.path.join(cell_dir, "internal/docsindex/assets/docs-bm25-index.json")
    if not os.path.exists(idx):
        with open(idx, "w") as f:
            f.write("{}\n")


def cmd_setup(a):
    check_fixtures()
    m = matrix()
    st = load_state(a.run_id)
    st.setdefault("started_at", now_iso())
    st.setdefault("run_date", _dt.date.today().isoformat())
    st["repo_head_at_run"] = git("rev-parse", "HEAD")
    types = {tt["id"]: tt for tt in m["task_types"]}
    cells_root = os.path.join(run_dir(a.run_id), "cells")
    st.setdefault("cells", {})
    for cell in planned_cells(m, parse_tiers(a.tiers)):
        d = os.path.join(cells_root, cell["name"])
        rel = os.path.relpath(d, REPO)
        if os.path.exists(d):
            log(f"skip (exists) {rel}")
        else:
            seed_cell(d, types[cell["task_type"]]["baseline_sha"], types[cell["task_type"]])
            log(f"seeded {rel}")
        st["cells"].setdefault(cell["name"], {}).update({**cell, "dir": rel})
    save_state(a.run_id, st)
    log(f"SETUP OK — {len(st['cells'])} cells, repo_head_at_run={st['repo_head_at_run']}")


PLACEHOLDER_SPEC = (
    "Eval scaffold placeholder for the model-tiering eval driver. Plan exactly one story with one task "
    "titled 'placeholder' that changes no files. The eval driver removes this generated plan and replaces "
    "it with its own cell tasks before review; nothing in this plan will ever run."
)


def cell_task_spec(cell, cell_dir_abs, spec_text):
    return (f"Cell directory: {cell_dir_abs}\n"
            f"(repo-relative: {cell['dir']}). This is your ONE working directory for this task: cd into it first; "
            f"every path in the spec below is relative to it, and its own git repo is the only one you may commit to.\n\n"
            + spec_text)


def cmd_create_prd(a):
    check_fixtures()
    m = matrix()
    st = load_state(a.run_id, required=not a.dry_run)
    cells = list(planned_cells(m, parse_tiers(a.tiers)))
    if a.dry_run:
        for cell in cells:
            print(f"CELL {cell['name']} task_type={cell['task_type']} tier={cell['tier']} backend={cell['backend']} "
                  f"model={cell['model']} effort={cell['effort'] or '(none)'}")
        log(f"DRY RUN — {len(cells)} cells, no network calls made")
        return
    if st.get("eval_prd_id"):
        raise EvalError(f"run {a.run_id} already has eval_prd_id {st['eval_prd_id']}; use a new --run-id")
    missing = [c["name"] for c in cells if c["name"] not in st.get("cells", {})]
    if missing:
        raise EvalError(f"cells not set up: {', '.join(missing[:5])}… (run setup first)")
    types = {tt["id"]: tt for tt in m["task_types"]}
    c = Client()
    title = f"{EXEC_TITLE_PREFIX} {a.run_id}"
    prd = prd_create(c, PLACEHOLDER_SPEC, REPO)
    pid = prd["id"]
    st["eval_prd_id"] = pid
    save_state(a.run_id, st)  # record immediately so cleanup can find it
    log(f"created scratch PRD {pid}; decomposing placeholder spec")
    prd_action(c, pid, "decompose")
    prd = prd_wait_planned(c, pid, a.decompose_timeout)
    if prd.get("status") != "needs_review":
        raise EvalError(f"placeholder decompose ended in status {prd.get('status')!r}, expected needs_review")
    for s in prd.get("stories") or []:
        prd_action(c, pid, "remove_story", {"story_id": s["id"], "actor": ACTOR})
    prd_action(c, pid, "set_memory_seed", {"enabled": False, "actor": ACTOR})
    prd_action(c, pid, "set_memory_harvest", {"enabled": False, "actor": ACTOR})
    prd_action(c, pid, "set_continue_on_story_failure", {"continue_on_story_failure": True, "actor": ACTOR})
    by_type = {}
    for cell in cells:
        by_type.setdefault(cell["task_type"], []).append(cell)
    for tt_id, tcells in by_type.items():
        stitle = f"[eval] {tt_id}"
        before = {s["id"] for s in prd_get(c, pid).get("stories") or []}
        prd = prd_action(c, pid, "add_story", {"title": stitle, "description": types[tt_id]["title"], "actor": ACTOR})
        new = [s for s in prd.get("stories") or [] if s["id"] not in before]
        if len(new) != 1:
            raise EvalError(f"add_story {stitle}: could not identify new story")
        sid = new[0]["id"]
        spec_text = open(os.path.join(FIX, types[tt_id]["spec"]), encoding="utf-8").read()
        for cell in tcells:
            before_t = {t["id"] for (_, _, _, t) in all_tasks(prd_get(c, pid))}
            cdir_abs = os.path.join(REPO, st["cells"][cell["name"]]["dir"])
            prd = prd_action(c, pid, "add_task", {"story_id": sid, "title": f"[eval] {cell['name']}",
                                                  "spec": cell_task_spec(st["cells"][cell["name"]], cdir_abs, spec_text),
                                                  "actor": ACTOR})
            newt = [t for (_, _, _, t) in all_tasks(prd) if t["id"] not in before_t]
            if len(newt) != 1:
                raise EvalError(f"add_task {cell['name']}: could not identify new task")
            tid = newt[0]["id"]
            prd_action(c, pid, "set_task_llm", {"task_id": tid, "backend": "claude-code", "model": cell["model"],
                                                "effort": cell["effort"] or "", "actor": ACTOR})
            st["cells"][cell["name"]].update({"task_id": tid, "story_id": sid})
            save_state(a.run_id, st)
            log(f"task {tid} <- {cell['name']}")
    prd_set_title(c, pid, title)
    # re-read and assert
    prd = prd_get(c, pid)
    tasks = {t["id"]: t for (_, _, _, t) in all_tasks(prd)}
    errs = []
    for cell in cells:
        t = tasks.get(st["cells"][cell["name"]]["task_id"])
        if not t:
            errs.append(f"{cell['name']}: task missing")
            continue
        got = (t.get("backend"), t.get("model"), t.get("effort") or "")
        want = ("claude-code", cell["model"], cell["effort"] or "")
        if got != want:
            errs.append(f"{cell['name']}: got {got} want {want}")
    if len(tasks) != len(cells):
        errs.append(f"PRD has {len(tasks)} tasks, expected exactly {len(cells)}")
    if prd.get("status") != "needs_review":
        errs.append(f"PRD status {prd.get('status')!r}, expected needs_review")
    if not str(prd.get("title", "")).startswith(EXEC_TITLE_PREFIX):
        errs.append(f"PRD title {prd.get('title')!r} does not start with {EXEC_TITLE_PREFIX!r}")
    if errs:
        raise EvalError("scratch PRD verification failed:\n  " + "\n  ".join(errs))
    save_state(a.run_id, st)
    log(f"CREATE-PRD OK — {pid} '{title}' with {len(cells)} tasks, left at needs_review (operator approves via `run`)")


def assert_scratch_prd(c, st, prd_id, prefix=EXEC_TITLE_PREFIX):
    if st.get("eval_prd_id") != prd_id:
        raise EvalError(f"refusing: --prd {prd_id} != recorded eval_prd_id {st.get('eval_prd_id')!r} for run {st.get('run_id')}")
    prd = prd_get(c, prd_id)
    if not str(prd.get("title", "")).startswith(prefix):
        raise EvalError(f"refusing: PRD {prd_id} title {prd.get('title')!r} does not start with {prefix!r}")
    return prd


def wait_tasks_terminal(c, prd_id, timeout_s, poll_s, task_ids=None):
    deadline = time.time() + timeout_s
    while True:
        prd = prd_get(c, prd_id)
        ts = [t for (_, _, _, t) in all_tasks(prd) if task_ids is None or t["id"] in task_ids]
        # auto-approve gates on THIS scratch PRD only (per_story_approval / guided mode)
        for s in prd.get("stories") or []:
            if s.get("status") == "awaiting_approval":
                prd_action(c, prd_id, "approve_story", {"story_id": s["id"], "actor": ACTOR})
            for t in s.get("tasks") or []:
                if t.get("status") == "awaiting_approval":
                    prd_action(c, prd_id, "approve_task", {"story_id": s["id"], "task_id": t["id"], "actor": ACTOR})
        done = sum(1 for t in ts if t.get("status") in TERMINAL_TASK)
        log(f"prd {prd_id} status={prd.get('status')} tasks terminal {done}/{len(ts)}")
        if ts and done == len(ts):
            return prd
        if prd.get("status") in TERMINAL_PRD:
            log(f"WARN: PRD reached terminal status {prd.get('status')} with {len(ts) - done} non-terminal tasks")
            return prd
        if time.time() > deadline:
            raise EvalError(f"timeout after {timeout_s}s: {len(ts) - done} tasks not terminal (PRD {prd_id} left as-is)")
        time.sleep(poll_s)


def cmd_run(a):
    st = load_state(a.run_id, required=True)
    c = Client()
    prd = assert_scratch_prd(c, st, a.prd)
    if prd.get("status") == "needs_review":
        prd_action(c, a.prd, "approve", {"actor": ACTOR, "note": f"model-tiering eval run {a.run_id}"})
        log(f"approved {a.prd}")
    if prd.get("status") in ("needs_review", "approved"):
        prd_action(c, a.prd, "run")
        log(f"run started for {a.prd}")
    else:
        log(f"PRD already {prd.get('status')}; polling only")
    st["exec_run_started_at"] = st.get("exec_run_started_at") or now_iso()
    save_state(a.run_id, st)
    wait_tasks_terminal(c, a.prd, a.timeout, a.poll)
    st["exec_run_finished_at"] = now_iso()
    save_state(a.run_id, st)
    log("RUN OK — all tasks terminal")


def parse_ts(s):
    if not s:
        return None
    s = re.sub(r"\.(\d{6})\d+", r".\1", s.replace("Z", "+00:00"))
    try:
        return _dt.datetime.fromisoformat(s)
    except ValueError:
        return None


def wall_seconds(t):
    a, b = parse_ts(t.get("started_at")), parse_ts(t.get("completed_at"))
    return round((b - a).total_seconds(), 1) if a and b else None


def daemon_verdict(t):
    v = t.get("verification")
    if not v:
        return None
    return "pass" if v.get("ok") else "fail"


def run_verify(type_id, cell_dir_abs, log_path):
    m = matrix()
    tt = next(x for x in m["task_types"] if x["id"] == type_id)
    os.makedirs(os.path.dirname(log_path), exist_ok=True)
    with open(log_path, "w") as lf:
        r = subprocess.run(["bash", os.path.join(FIX, tt["verify"]), cell_dir_abs], cwd=REPO,
                           stdout=lf, stderr=subprocess.STDOUT)
    return r.returncode


def cmd_collect(a):
    check_fixtures()
    st = load_state(a.run_id, required=True)
    if not st.get("eval_prd_id"):
        raise EvalError("no eval_prd_id in state (run create-prd first)")
    c = Client()
    prd = assert_scratch_prd(c, st, st["eval_prd_id"])
    tasks = {t["id"]: t for (_, _, _, t) in all_tasks(prd)}
    sidx, _ = sessions_index(c)
    rd = rates()
    rows = []
    for name, cell in sorted(st.get("cells", {}).items()):
        if not cell.get("task_id"):
            continue
        t = tasks.get(cell["task_id"], {})
        sess = sidx.get(t.get("session_id")) if t.get("session_id") else None
        cdir = os.path.join(REPO, cell["dir"])
        vexit = run_verify(cell["task_type"], cdir, os.path.join(run_dir(a.run_id), "verify-logs", name + ".log"))
        cost = session_cost(sess, rd)
        rows.append({
            "touchpoint": "execution", "task_type": cell["task_type"], "tier": cell["tier"], "rep": cell["rep"],
            "requested_model": cell["model"], "requested_effort": cell["effort"], "task_id": cell["task_id"],
            "session_id": t.get("session_id"), "task_status": t.get("status"), "daemon_verdict": daemon_verdict(t),
            "verify_exit": vexit, "passed": vexit == 0, "wall_seconds": wall_seconds(t),
            "tokens": cost["tokens"], "observed_models": cost["observed_models"],
            "cost_usd_model_rates": cost["cost_usd_model_rates"],
            "datawatch_est_cost_usd": cost["datawatch_est_cost_usd"], "datawatch_tokens": cost["datawatch_tokens"],
            "cost_method": cost["cost_method"],
        })
        log(f"{name}: status={t.get('status')} verify_exit={vexit} cost={cost['cost_usd_model_rates']}")
    path = results_path(st)
    merge_results(path, st, rows)
    log(f"COLLECT OK — {len(rows)} execution rows -> {os.path.relpath(path, REPO)}")


# --------------------------------------------------------------------------
# subcommands: decompose touchpoint
# --------------------------------------------------------------------------

DECOMPOSE_EFFORT_NOTE = (
    "claude-code decompose runs as a one-shot session (cmd/datawatch/main.go decomposeFnSession -> "
    "runOneShotJSONSession), which passes the model but no effort; requested effort is recorded but is not "
    "known to reach the claude CLI. Cost comes from that session's transcript (cost_method=session-transcript); "
    "headless (ollama/openwebui) decompose has no transcript and would be cost_method=headless-unpriced."
)


def find_decompose_session(sessions, prd_id, project_dir, t0):
    cands = []
    for s in sessions:
        if s.get("name") != "autonomous:decompose" or s.get("project_dir") != project_dir:
            continue
        ct = parse_ts(s.get("created_at"))
        if ct and ct.timestamp() >= t0 - 5:
            cands.append((ct, s))
    if s_ := [s for _, s in cands if prd_id in (s.get("task") or "")]:
        return s_[-1]
    return sorted(cands, key=lambda x: x[0])[0][1] if cands else None


def cmd_decompose_run(a):
    check_fixtures()
    m = matrix()
    st = load_state(a.run_id)
    st.setdefault("started_at", now_iso())
    st.setdefault("run_date", _dt.date.today().isoformat())
    st.setdefault("repo_head_at_run", git("rev-parse", "HEAD"))
    tiers = m["decompose_tiers"]
    wanted = parse_tiers(a.tiers)
    if wanted:
        tiers = [tier_by_id(m["decompose_tiers"], t) for t in wanted]
    spec = open(os.path.join(FIX, "decompose/ground-truth-spec.md"), encoding="utf-8").read()
    gt = load_json(os.path.join(FIX, "decompose/ground-truth-structure.json"))
    rd = rates()
    c = Client()
    path = results_path(st)
    st.setdefault("decompose_prds", {})
    for tier in tiers:
        leftover = glob.glob(os.path.join(REPO, ".decompose-output-*.json"))
        if leftover:
            # the daemon reuses a same-hash output file left by an interrupted decompose; identical spec
            # across tiers means a stale file would silently be scored as this tier's output
            raise EvalError(f"refusing tier {tier['id']}: stale decompose output in repo root: "
                            f"{', '.join(os.path.basename(p) for p in leftover)} (inspect and remove it first)")
        title = f"{EXEC_TITLE_PREFIX} decompose {a.run_id} {tier['id']}"
        row = {"touchpoint": "decompose", "task_type": None, "tier": tier["id"], "rep": 1,
               "requested_model": tier["model"], "requested_effort": tier.get("effort"),
               "effort_note": DECOMPOSE_EFFORT_NOTE}
        t0 = time.time()
        pid = None
        try:
            prd = prd_create(c, spec, REPO)
            pid = prd["id"]
            st["decompose_prds"][tier["id"]] = {"prd_id": pid, "state": "created"}
            save_state(a.run_id, st)
            prd_set_title(c, pid, title)
            prd_action(c, pid, "set_memory_seed", {"enabled": False, "actor": ACTOR})
            prd_action(c, pid, "set_memory_harvest", {"enabled": False, "actor": ACTOR})
            prd_action(c, pid, "set_llm", {"backend": "claude-code", "model": tier["model"], "effort": tier.get("effort") or "",
                                           "decomposition_profile": "claude-code", "decomposition_model": tier["model"],
                                           "actor": ACTOR})
            log(f"[{tier['id']}] decomposing PRD {pid}")
            prd_action(c, pid, "decompose")
            prd = prd_wait_planned(c, pid, a.timeout)
            row["wall_seconds"] = round(time.time() - t0, 1)
            row["prd_id"] = pid
            row["prd_status"] = prd.get("status")
            if prd.get("status") != "needs_review":
                row["error"] = f"decompose ended in status {prd.get('status')}: {prd.get('last_decompose_failure')}"
                row["structure_score"] = None
            else:
                detail = score_structure(normalise_structure(prd), gt)
                row["structure_score"] = detail["score"]
                row["rubric_detail"] = detail
            _, sessions = sessions_index(c)
            sess = find_decompose_session(sessions, pid, REPO, t0)
            cost = session_cost(sess, rd)
            if not sess:
                cost["cost_method"] = "no-session-found (headless backend or session already purged)"
            row.update({"session_id": (sess or {}).get("full_id"), "tokens": cost["tokens"],
                        "observed_models": cost["observed_models"], "cost_usd_model_rates": cost["cost_usd_model_rates"],
                        "datawatch_est_cost_usd": cost["datawatch_est_cost_usd"], "cost_method": cost["cost_method"]})
        except EvalError as e:
            row.setdefault("wall_seconds", round(time.time() - t0, 1))
            row["error"] = str(e)
            row.setdefault("structure_score", None)
            log(f"[{tier['id']}] ERROR {e}")
        finally:
            if pid:
                res = prd_cancel_quiet(c, pid)
                st["decompose_prds"][tier["id"]]["state"] = res
                save_state(a.run_id, st)
                log(f"[{tier['id']}] scratch PRD {pid}: {res}")
        merge_results(path, st, [row])
        log(f"[{tier['id']}] score={row.get('structure_score')} cost={row.get('cost_usd_model_rates')}")
    log(f"DECOMPOSE-RUN OK — {len(tiers)} tiers -> {os.path.relpath(path, REPO)}")


def cmd_decompose_self_test(_a):
    check_fixtures()
    gt = load_json(os.path.join(FIX, "decompose/ground-truth-structure.json"))
    # 1. ground truth scored against itself: everything passes
    d = score_structure(normalise_structure(gt_as_prd(gt)), gt)
    assert d["score"] == 100 and d["failed"] == [], d
    assert d["counts"] == {"stories": 2, "tasks": 5, "edges": 3}, d["counts"]
    # 2. hand-built bad structure with known failures
    bad = {"stories": [
        {"id": "a", "title": "Only story", "depends_on": ["zz"], "tasks": [
            {"id": "t1", "title": "Ship it", "spec": "Set max_parallel_tasks to 8.\n", "depends_on": ["t2"],
             "files": ["cmd/datawatch/main.go", "internal/server/api.go", "CHANGELOG.md", "datawatch.yaml"]},
            {"id": "t2", "title": "Fix tracker", "spec": "reproduce first", "depends_on": ["t1"],
             "files": ["internal/session/tracker.go", "internal/session/manager.go", "web/app.js"]},
        ]},
        {"id": "b", "title": "Empty", "depends_on": [], "tasks": []},
    ]}
    d = score_structure(normalise_structure(bad), gt)
    # passes: C1 (2 stories), C6, C8, C10, C12 (t1 -> t2 cyclic but reachable) ; fails the rest
    expect_failed = ["C2", "C3", "C4", "C5", "C7", "C9", "C11", "C13", "C14", "C15"]
    assert d["failed"] == expect_failed, d
    assert d["score"] == 10 + 10 + 5 + 5 + 10, d
    # 3. C12 has no vacuous pass, and a lone code+release task cannot satisfy it alone
    solo = {"stories": [{"id": "s", "title": "x", "depends_on": [], "tasks": [
        {"id": "t", "title": "all", "spec": "", "depends_on": [],
         "files": ["internal/session/tracker.go", *RELEASE_FILES]}]}]}
    d = score_structure(normalise_structure(solo), gt)
    assert "C12" in d["failed"] and "C10" in d["passed"] and "C6" in d["passed"], d
    # 4. C13 allows a negated mention; repository-relative ids never compared
    neg = json.loads(json.dumps(gt_as_prd(gt)))
    neg["stories"][0]["tasks"][0]["spec"] = "Do not change max_parallel_tasks.\nreproduction"
    d = score_structure(normalise_structure(neg), gt)
    assert d["score"] == 100, d
    log("DECOMPOSE-SELF-TEST OK")


# --------------------------------------------------------------------------
# subcommands: verification touchpoint (the ONLY global-config writer)
# --------------------------------------------------------------------------

def _vt_mock_path():
    return os.environ.get("EVAL_MOCK_CONFIG") or None


def verify_tier_config_get(c):
    """Read verification_* from the daemon (or the self-test mock file)."""
    mp = _vt_mock_path()
    if mp:
        return {k: (load_json(mp).get(k) or "") for k in VERIFY_KEYS}
    v, _ = read_verification_config(c)
    return v


def verify_tier_config_put(c, values):
    """The single global-config write in this driver: partial PUT of verification_* keys
    (REST equivalent of MCP autonomous_config_set; the handler merges onto current config)."""
    global _CONFIG_WRITE_ARMED
    body = {k: values[k] for k in VERIFY_KEYS if k in values}
    mp = _vt_mock_path()
    if mp:
        if os.environ.get("EVAL_MOCK_PUT_DROP") == "1":
            return  # self-test: simulate a write that silently does not stick
        cur = load_json(mp)
        cur.update(body)
        write_json_atomic(mp, cur)
        return
    _CONFIG_WRITE_ARMED = True
    try:
        c.request("PUT", _CONFIG_PATH, body)
    finally:
        _CONFIG_WRITE_ARMED = False


def verify_tier_gate_self_check():
    """Self-test: an un-armed write to the global config route is refused before any network I/O."""
    try:
        Client(base="http://127.0.0.1:9").request("PUT", _CONFIG_PATH, {})
        raise AssertionError("config write gate did not trip")
    except EvalError as e:
        assert "only allowed inside verify-tier" in str(e), e


def verify_tier_manual_fix(run_id, restore_to):
    body = json.dumps({k: restore_to[k] for k in VERIFY_KEYS})
    return (f"scripts/eval-model-tiering.sh verify-tier-restore --run-id {run_id}\n"
            f"  or: curl -sk -X PUT -H 'Content-Type: application/json' "
            f"-H \"Authorization: Bearer $DW_BASE_TOKEN\" -d '{body}' \"${{DW_BASE:-{DEFAULT_BASE}}}{_CONFIG_PATH}\"")


def verify_tier_values_for(tier):
    return {"verification_backend": tier["backend"], "verification_model": tier["model"],
            "verification_effort": tier.get("effort") or ""}


def cmd_vt_capture(a):
    st = load_state(a.run_id, required=True)
    if "verification_backend_at_start" not in st:
        raise EvalError("no verification_*_at_start in state: run preflight for this run-id first")
    at_start = {k: st[f"{k}_at_start"] for k in VERIFY_KEYS}
    live = verify_tier_config_get(Client())
    if live != at_start:
        raise EvalError(f"refusing to change anything: live verification config {live} != recorded start "
                        f"{at_start}. A previous verify-tier run may have been interrupted. Fix with:\n  "
                        + verify_tier_manual_fix(a.run_id, at_start))
    st["restore_to"] = at_start
    st["verify_tier_in_flight"] = {"tier": a.tier, "since": now_iso()}
    save_state(a.run_id, st)
    log(f"captured restore_to={at_start} (tier {a.tier} in flight)")


def cmd_vt_set(a):
    st = load_state(a.run_id, required=True)
    if not st.get("restore_to") or (st.get("verify_tier_in_flight") or {}).get("tier") != a.tier:
        raise EvalError("refusing vt-set: restore_to not captured for this tier (vt-capture must run first)")
    tier = tier_by_id(matrix()["execution_tiers"], a.tier)
    vals = verify_tier_values_for(tier)
    verify_tier_config_put(Client(), vals)
    log(f"global verification config set for tier {a.tier}: {vals}")


def cmd_vt_restore(a):
    st = load_state(a.run_id, required=True)
    restore_to = st.get("restore_to") or ({k: st[f"{k}_at_start"] for k in VERIFY_KEYS}
                                          if "verification_backend_at_start" in st else None)
    if not restore_to:
        raise EvalError("nothing to restore: no restore_to / *_at_start in state")
    c = Client()
    err = None
    try:
        verify_tier_config_put(c, restore_to)
        live = verify_tier_config_get(c)
    except EvalError as e:
        err, live = str(e), None
    if live != restore_to:
        bar = "!" * 78
        sys.stderr.write(f"\n{bar}\nGLOBAL CONFIG RESTORE FAILED — daemon verification config is NOT back to its "
                         f"original value.\n  expected: {restore_to}\n  live:     {live}\n  error:    {err}\n"
                         f"Run this to fix it:\n  {verify_tier_manual_fix(a.run_id, restore_to)}\n{bar}\n\n")
        sys.exit(3)
    st.pop("verify_tier_in_flight", None)
    save_state(a.run_id, st)
    log(f"restore verified: {live}")


def cmd_vt_final_check(a):
    st = load_state(a.run_id, required=True)
    if "verification_backend_at_start" not in st:
        raise EvalError("no verification_*_at_start in state: run preflight for this run-id first")
    at_start = {k: st[f"{k}_at_start"] for k in VERIFY_KEYS}
    live = verify_tier_config_get(Client())
    if live != at_start or st.get("verify_tier_in_flight"):
        bar = "!" * 78
        sys.stderr.write(f"\n{bar}\nFINAL GLOBAL CONFIG CHECK FAILED\n  preflight start: {at_start}\n  live now:        "
                         f"{live}\n  in-flight marker: {st.get('verify_tier_in_flight')}\nRun this to fix it:\n  "
                         f"{verify_tier_manual_fix(a.run_id, at_start)}\n{bar}\n\n")
        sys.exit(3)
    log(f"final check OK: live verification config equals preflight start {at_start}")


def cmd_vt_task(a):
    """Run ONE fixture task with a fixed execution tier through a small scratch PRD."""
    check_fixtures()
    m = matrix()
    st = load_state(a.run_id, required=True)
    exec_tier = tier_by_id(m["execution_tiers"], a.exec_tier)
    tt = next((x for x in m["task_types"] if x["id"] == a.task_type), None)
    if not tt:
        raise EvalError(f"unknown --task-type {a.task_type}")
    mock = os.environ.get("EVAL_MOCK_TASK")
    if mock:  # offline self-test hook: simulate the task outcome without any network call
        log(f"mock task ({mock}) for tier {a.tier}")
        if mock == "fail":
            raise EvalError("simulated task failure partway through (self-test)")
        if mock == "sleep":
            pidfile = os.environ.get("EVAL_MOCK_PIDFILE")
            if pidfile:
                with open(pidfile, "w") as f:
                    f.write(str(os.getpid()))
            time.sleep(30)
        return
    cell_name = f"verify__{a.tier}__{a.task_type}"
    cdir = os.path.join(run_dir(a.run_id), "cells", cell_name)
    if os.path.exists(cdir):
        shutil.rmtree(cdir)
    seed_cell(cdir, tt["baseline_sha"], tt)
    cell = {"dir": os.path.relpath(cdir, REPO)}
    c = Client()
    title = f"{EXEC_TITLE_PREFIX} verify {a.run_id} {a.tier}"
    t0 = time.time()
    prd = prd_create(c, PLACEHOLDER_SPEC, REPO)
    pid = prd["id"]
    st.setdefault("verify_prds", {})[a.tier] = pid
    save_state(a.run_id, st)
    row = {"touchpoint": "verification", "task_type": a.task_type, "tier": a.tier, "rep": 1,
           "execution_tier": a.exec_tier, "prd_id": pid,
           "requested_verifier": verify_tier_values_for(tier_by_id(m["execution_tiers"], a.tier))
           if a.tier != "default" else "unchanged (default)"}
    try:
        prd_action(c, pid, "decompose")
        prd = prd_wait_planned(c, pid, 900)
        if prd.get("status") != "needs_review":
            raise EvalError(f"placeholder decompose ended in {prd.get('status')}")
        for s in prd.get("stories") or []:
            prd_action(c, pid, "remove_story", {"story_id": s["id"], "actor": ACTOR})
        prd_action(c, pid, "set_memory_seed", {"enabled": False, "actor": ACTOR})
        prd_action(c, pid, "set_memory_harvest", {"enabled": False, "actor": ACTOR})
        prd = prd_action(c, pid, "add_story", {"title": f"[eval] verify {a.tier}", "description": tt["title"], "actor": ACTOR})
        sid = (prd.get("stories") or [{}])[-1].get("id")
        spec_text = open(os.path.join(FIX, tt["spec"]), encoding="utf-8").read()
        prd = prd_action(c, pid, "add_task", {"story_id": sid, "title": f"[eval] {cell_name}",
                                              "spec": cell_task_spec(cell, cdir, spec_text), "actor": ACTOR})
        tid = all_tasks(prd)[-1][3]["id"]
        prd_action(c, pid, "set_task_llm", {"task_id": tid, "backend": "claude-code", "model": exec_tier["model"],
                                            "effort": exec_tier.get("effort") or "", "actor": ACTOR})
        prd_set_title(c, pid, title)
        prd_action(c, pid, "approve", {"actor": ACTOR, "note": f"verify-tier {a.tier}"})
        prd_action(c, pid, "run")
        prd = wait_tasks_terminal(c, pid, a.timeout, a.poll, task_ids={tid})
        t = next(t for (_, _, _, t) in all_tasks(prd) if t["id"] == tid)
        row.update({"task_id": tid, "session_id": t.get("session_id"), "task_status": t.get("status"),
                    "daemon_verdict": daemon_verdict(t), "wall_seconds": wall_seconds(t) or round(time.time() - t0, 1)})
        sidx, _ = sessions_index(c)
        rd = rates()
        cost = session_cost(sidx.get(t.get("session_id")), rd)
        vid = (t.get("verification") or {}).get("verifier_id")
        vcost = session_cost(sidx.get(vid), rd) if vid else {"observed_models": [], "cost_usd_model_rates": None,
                                                             "cost_method": "verifier-not-session"}
        want_fam = None if a.tier == "default" else tier_by_id(m["execution_tiers"], a.tier)["model"]
        applied = None
        if want_fam and vcost["observed_models"]:
            applied = any(model_family(x, rd) == want_fam for x in vcost["observed_models"])
        row.update({"tokens": cost["tokens"], "observed_models": cost["observed_models"],
                    "cost_usd_model_rates": cost["cost_usd_model_rates"],
                    "datawatch_est_cost_usd": cost["datawatch_est_cost_usd"],
                    "verifier_session_id": vid, "verifier_observed_models": vcost["observed_models"],
                    "verifier_cost_usd_model_rates": vcost["cost_usd_model_rates"], "tier_applied": applied})
    except EvalError as e:
        row["error"] = str(e)
        raise
    finally:
        # eval's own independent check, then persist the row whatever happened
        try:
            vexit = run_verify(a.task_type, cdir, os.path.join(run_dir(a.run_id), "verify-logs", cell_name + ".log"))
            row.update({"verify_exit": vexit, "passed": vexit == 0})
        except Exception as e:  # noqa: BLE001 - never mask the original error
            row["verify_error"] = str(e)
        merge_results(results_path(st), st, [row])
        log(f"verification row recorded for tier {a.tier}: daemon={row.get('daemon_verdict')} eval_exit={row.get('verify_exit')}")


# --------------------------------------------------------------------------
# subcommands: report / cleanup / self-test
# --------------------------------------------------------------------------

def cmd_report(a):
    if a.results:
        path = os.path.abspath(a.results)
    else:
        path = results_path(load_state(a.run_id, required=True))
    if not os.path.isfile(path):
        raise EvalError(f"no results.json at {path}")
    md = render_report(load_json(path))
    out = os.path.join(os.path.dirname(path), "results.md")
    with open(out, "w", encoding="utf-8") as f:
        f.write(md + "\n")
    log(f"REPORT OK -> {os.path.relpath(out, REPO) if out.startswith(REPO) else out}")


def cmd_cleanup(a):
    d = run_dir(a.run_id)
    cells = os.path.join(d, "cells")
    real = os.path.realpath(cells)
    if not real.startswith(os.path.realpath(STATE_ROOT) + os.sep):
        raise EvalError(f"refusing to remove {cells}: not under {STATE_ROOT_REL}")
    if os.path.isdir(cells):
        shutil.rmtree(cells)
        log(f"removed {os.path.relpath(cells, REPO)}")
    else:
        log("no cells/ to remove")
    st = load_state(a.run_id)
    problems = []
    if a.offline:
        log("offline: skipping daemon checks")
    else:
        c = Client()
        for label, ids in (("decompose", [v.get("prd_id") for v in (st.get("decompose_prds") or {}).values()]),
                           ("verify", list((st.get("verify_prds") or {}).values()))):
            for pid in filter(None, ids):
                try:
                    stt = prd_get(c, pid).get("status")
                except EvalError as e:
                    stt = f"unreadable ({e})"
                if stt not in TERMINAL_PRD and not str(stt).startswith("unreadable"):
                    problems.append(f"{label} scratch PRD {pid} still {stt}")
                else:
                    log(f"{label} scratch PRD {pid}: {stt}")
        if "verification_backend_at_start" in st:
            at_start = {k: st[f"{k}_at_start"] for k in VERIFY_KEYS}
            live = verify_tier_config_get(c)
            if live != at_start or st.get("verify_tier_in_flight"):
                problems.append(f"global verification config drift: live {live} != start {at_start}; fix: "
                                + verify_tier_manual_fix(a.run_id, at_start))
            else:
                log("global verification config matches preflight start")
    if problems:
        raise EvalError("cleanup found leftovers:\n  " + "\n  ".join(problems))
    log("CLEANUP OK")


def synthetic_results():
    tok = {"input_tokens": 10, "output_tokens": 20, "cache_creation_input_tokens": 30, "cache_read_input_tokens": 40}
    return {"run_id": "selftest", "eval_prd_id": "deadbeef", "repo_head_at_run": "0" * 40,
            "started_at": "2026-10-14T00:00:00Z", "finished_at": "2026-10-14T01:00:00Z", "rates_as_of": "2026-10-06",
            "observed_default": {"backend": "claude-code", "model": "opus", "effort": None},
            "cells": [
                {"touchpoint": "execution", "task_type": "docs", "tier": "haiku-low", "rep": 1, "passed": True,
                 "wall_seconds": 61, "tokens": tok, "cost_usd_model_rates": 0.0012, "datawatch_est_cost_usd": 0.01},
                {"touchpoint": "execution", "task_type": "docs", "tier": "opus-high", "rep": 1, "passed": False,
                 "wall_seconds": 300, "tokens": tok, "cost_usd_model_rates": 0.5, "datawatch_est_cost_usd": None},
                {"touchpoint": "decompose", "tier": "sonnet-medium", "rep": 1, "structure_score": 93,
                 "rubric_detail": {"failed": ["C9"], "counts": {"stories": 2, "tasks": 5, "edges": 3}},
                 "cost_usd_model_rates": 0.05},
                {"touchpoint": "decompose", "tier": "haiku-low", "rep": 1, "structure_score": None, "error": "timeout"},
                {"touchpoint": "verification", "tier": "opus-high", "rep": 1, "task_type": "analysis",
                 "execution_tier": "sonnet-medium", "daemon_verdict": "pass", "verify_exit": 0, "cost_usd_model_rates": 0.3,
                 "tier_applied": False},
                {"touchpoint": "verification", "tier": "haiku-low", "rep": 1, "task_type": "analysis",
                 "execution_tier": "sonnet-medium", "daemon_verdict": "pass", "verify_exit": 1, "cost_usd_model_rates": 0.2},
            ]}


def cmd_self_test(a):
    check_fixtures()
    rd = rates()
    exp = load_json(os.path.join(FIX, "transcript-sample.expected.json"))
    t = parse_transcripts([os.path.join(FIX, "transcript-sample.jsonl")], rd)
    assert t["assistant_entries"] == exp["assistant_entries"], t
    assert t["non_assistant_entries"] == exp["non_assistant_entries"], t
    for fam, want in exp["per_model"].items():
        got = t["per_model"][fam]
        for k in TOKEN_KEYS:
            assert got[k] == want[k], (fam, k, got[k], want[k])
        assert abs(got["cost_usd"] - want["cost_usd"]) < 1e-9, (fam, got["cost_usd"], want["cost_usd"])
    for k in TOKEN_KEYS:
        assert t["total"][k] == exp["total"][k], (k, t["total"][k])
    assert abs(t["total"]["cost_usd"] - exp["total"]["cost_usd"]) < 1e-9, t["total"]
    assert t["observed_models"] == ["claude-haiku-5-5", "claude-opus-5-5"], t["observed_models"]
    log(f"transcript totals OK: {t['total']}")
    # duplicate message.id lines are counted once
    with tempfile.TemporaryDirectory() as td:
        p = os.path.join(td, "dup.jsonl")
        lines = open(os.path.join(FIX, "transcript-sample.jsonl")).read().splitlines()
        with open(p, "w") as f:
            f.write("\n".join(lines + [lines[1], "{not json"]) + "\n")
        t2 = parse_transcripts([p], rd)
        assert t2["total"]["cost_usd"] == t["total"]["cost_usd"] and t2["duplicate_message_lines_skipped"] == 1, t2
        # transcript location mirrors claude_usage.go
        uid = claude_session_uuid("ralfthewise-f7ec")
        assert re.fullmatch(r"[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}", uid), uid
        import uuid as _uuid
        assert uid == str(_uuid.uuid5(_uuid.UUID("6ba7b810-9dad-11d1-80b4-00c04fd430c8"), "ralfthewise-f7ec")), uid
        tp = claude_transcript_path("/w/data.watch/x", "host-ab12", home=td)
        assert tp == os.path.join(td, ".claude", "projects", "-w-data-watch-x", claude_session_uuid("host-ab12") + ".jsonl"), tp
        os.makedirs(os.path.dirname(tp))
        shutil.copyfile(os.path.join(FIX, "transcript-sample.jsonl"), tp)
        sc = session_cost({"project_dir": "/w/data.watch/x", "full_id": "host-ab12", "est_cost_usd": 0.42}, rd, home=td)
        assert sc["cost_method"] == "session-transcript" and sc["cost_usd_model_rates"] == t["total"]["cost_usd"], sc
        assert sc["datawatch_est_cost_usd"] == 0.42, sc
        log("transcript location + session cost OK")
        # report rendering on synthetic results covering all three touchpoints
        rp = os.path.join(td, "results.json")
        write_json_atomic(rp, synthetic_results())
        md = render_report(load_json(rp))
        for needle in ("## Execution", "| Task type | Tier | Pass | Wall time | Tokens | Cost (model rates) | Cost (datawatch flat) |",
                       "## Decompose", "| Tier | Structure score | Rubric detail | Cost |", "93/100", "error: timeout",
                       "## Verification", "| Tier | Daemon verdict | Eval verdict | Agreement y/n | Cost |",
                       "| opus-high | pass | pass | y |", "| haiku-low | pass | fail | n |", "Agreement 1/2",
                       "Caveat:", "| docs | 1/2 | haiku-low |"):
            assert needle in md, f"report missing {needle!r}\n{md}"
        # merge keeps other touchpoints and upserts by key
        st = {"run_id": "selftest", "run_date": "2026-10-14"}
        doc = merge_results(rp, st, [{"touchpoint": "decompose", "tier": "haiku-low", "rep": 1, "task_type": None,
                                      "structure_score": 80}])
        assert len(doc["cells"]) == 6 and any(c.get("structure_score") == 80 for c in doc["cells"]), doc["cells"]
        log("report rendering + results merge OK")
    # dry-run planning covers every task type x execution tier
    m = matrix()
    n = len(list(planned_cells(m)))
    assert n == len(m["task_types"]) * len(m["execution_tiers"]) * int(m.get("reps", 1)), n
    verify_tier_gate_self_check()
    log("planning + config write gate OK")
    cmd_decompose_self_test(a)
    log("SELF-TEST (python) OK")


# --------------------------------------------------------------------------

def main(argv=None):
    p = argparse.ArgumentParser(prog="eval_model_tiering.py", description=__doc__.split("\n")[0])
    sub = p.add_subparsers(dest="cmd", required=True)

    def add(name, fn, run_id=True, **kw):
        sp = sub.add_parser(name, **kw)
        if run_id:
            sp.add_argument("--run-id", required=True)
        sp.set_defaults(fn=fn)
        return sp

    add("check-fixtures", lambda a: (check_fixtures(), log("fixtures OK")), run_id=False)
    add("preflight", cmd_preflight)
    sp = add("setup", cmd_setup)
    sp.add_argument("--tiers", default="")
    sp = add("create-prd", cmd_create_prd)
    sp.add_argument("--dry-run", action="store_true")
    sp.add_argument("--tiers", default="")
    sp.add_argument("--decompose-timeout", type=int, default=900)
    sp = add("run", cmd_run)
    sp.add_argument("--prd", required=True)
    sp.add_argument("--timeout", type=int, default=4 * 3600)
    sp.add_argument("--poll", type=int, default=30)
    add("collect", cmd_collect)
    sp = add("decompose-run", cmd_decompose_run)
    sp.add_argument("--tiers", default="")
    sp.add_argument("--timeout", type=int, default=600)
    add("decompose-self-test", cmd_decompose_self_test, run_id=False)
    sp = add("vt-capture", cmd_vt_capture)
    sp.add_argument("--tier", required=True)
    sp = add("vt-set", cmd_vt_set)
    sp.add_argument("--tier", required=True)
    add("vt-restore", cmd_vt_restore)
    add("vt-final-check", cmd_vt_final_check)
    sp = add("vt-task", cmd_vt_task)
    sp.add_argument("--tier", required=True)
    sp.add_argument("--task-type", default="analysis")
    sp.add_argument("--exec-tier", default="sonnet-medium")
    sp.add_argument("--timeout", type=int, default=2 * 3600)
    sp.add_argument("--poll", type=int, default=30)
    sp = add("report", cmd_report, run_id=False)
    sp.add_argument("--run-id", default="")
    sp.add_argument("--results", default="")
    sp = add("cleanup", cmd_cleanup)
    sp.add_argument("--offline", action="store_true")
    add("self-test", cmd_self_test, run_id=False)
    sp = add("tier-ids", lambda a: print(",".join(t["id"] for t in matrix()[a.grid])), run_id=False)
    sp.add_argument("--grid", default="execution_tiers")

    a = p.parse_args(argv)
    try:
        a.fn(a)
    except EvalError as e:
        sys.stderr.write(f"[{_dt.datetime.now().strftime('%H:%M:%S')}] ERROR: {e}\n")
        return 1
    except AssertionError as e:
        sys.stderr.write(f"[{_dt.datetime.now().strftime('%H:%M:%S')}] SELF-TEST FAILED: {e}\n")
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
