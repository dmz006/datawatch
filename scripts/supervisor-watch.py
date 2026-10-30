#!/usr/bin/env python3
"""Cheap supervisor watch: one command, one verdict, no judgement calls.

Prints "OK <summary>" when nothing needs attention, otherwise "ATTENTION" followed
by one line per finding. Meant for the 20 minute supervisor wake-up; the full tick
(merges, lane fill, registry audit, CI deep read, v10 table) stays hourly.

Checks: tmux restarts, daemon up, the working model resident on both nodes, PRDs that
newly failed, blocked or need review, PRDs finished but not merged, idle lanes with
work waiting, running PRDs whose worktree is broken or silent, a dirty main checkout
(it makes a ship abort), and red or stuck CI on main and the tags.

With --record <path> the findings are additionally written to <path> as
{"at": <unix seconds>, "findings": [{"kind": <stable finding kind>,
"subject": <id or empty>, "text": <line>}]} (the daemon-side supervisor watch
compares its own findings against that record); without the option stdout and
exit code are unchanged.
"""
import glob
import json
import os
import re
import ssl
import subprocess
import sys
import time
import urllib.request

BASE = os.path.expanduser("~/workspace/datawatch")
WT = os.path.expanduser("~/.datawatch/prd-worktrees")
STATE = "/tmp/sv/watch-state.json"
MODEL = "qwen3.8-64k"
TMUX_RESTARTS, TMUX_SINCE = "3", "Tue 2026-10-20 18:46:38 EDT"
SILENT_MIN = 60
# Deliberately held PRDs (operator or build manager decision, with the reason in the registry).
ACKED_RUNS = {40316827946, 40293597613, 40320408404, 40366394552, 40366382818, 40425238678}  # owasp-zap: only the "attach reports to release" step failed on a GitHub API rate limit; the scan passed (registry row 209); 40320408404 failed the version bump check on a script-only commit, fixed by exempting this script in 9.19.3; 40366394552 and 40366382818 are CodeQL Analyze (actions) runs that end red at the result upload with no error shown, tracked in BL625 (row 212); 40425238678 is CodeQL rejecting the workflow upload because GitHub default setup is also enabled (operator decision pending)
# While the operator has not chosen between GitHub default CodeQL setup and our own workflow, every CodeQL run on main ends
# red at the SARIF upload ("advanced configurations cannot be processed when the default setup is enabled"); set to False once decided.
CODEQL_CONFLICT_KNOWN = True
IGNORE = {"58aa0001", "99c20001", "fdd80001", "6bded76a", "04b20001", "7c960001", "7e460001", "7f3e0001", "80660001", "900d0001", "95950001", "fdba0001"}  # 04b20001 BL637 parity backfill held until the final stage before 10.0 (row 227); 6bded76a voice PRD cancelled, it ran in the main checkout (replaced by a worktree PRD, row 222); 58aa0001 BL598 held (row 180); db4b0001 BL622 draft, not decomposed while Claude use is over the pace line (row 209); 99c20001 BL623 draft, decompose at the next hourly tick if the budget allows (row 210); 7c96/7e46/7f3e/8066 final-stage placeholders (BL411, BL409, BL424, BL496) and 900d0001 BL644 design-gated, all held


def sh(cmd, timeout=40):
    try:
        return subprocess.run(cmd, shell=True, capture_output=True, text=True, timeout=timeout).stdout
    except Exception:
        return ""


def get_json(url, timeout=6, insecure=False):
    ctx = ssl._create_unverified_context() if insecure else None
    try:
        with urllib.request.urlopen(url, timeout=timeout, context=ctx) as r:
            return json.load(r)
    except Exception:
        return None


def models(url):
    """Names of the resident models; the working model also counts when it is loaded under its base tag with the 64k context."""
    d = get_json(url + "/api/ps")
    if d is None:
        return None
    out = []
    for m in d.get("models", []):
        name = m["name"]
        if name.startswith("qwen3.8:") and (m.get("context_length") or 0) >= 65536:
            name = MODEL + ":" + name.split(":", 1)[1]
        out.append(name)
    return out


def recent(ts, hours=72):
    """True when an ISO timestamp is within the last `hours` (old probes and drafts do not count)."""
    try:
        t = time.mktime(time.strptime(ts[:19], "%Y-%m-%dT%H:%M:%S"))
        return time.time() - t < hours * 3600
    except Exception:
        return False


def newest_activity(prd_id):
    """Seconds since the newest file change in the worktree, the newest pi log or the newest Claude transcript."""
    best = 0
    d = os.path.join(WT, prd_id)
    for root, dirs, files in os.walk(d):
        dirs[:] = [x for x in dirs if x != ".git"]
        for f in files:
            try:
                best = max(best, os.path.getmtime(os.path.join(root, f)))
            except OSError:
                pass
    for f in glob.glob(os.path.expanduser("~/.pi/agent/sessions/--home-dmz-.datawatch-prd-worktrees-%s--/*.jsonl" % prd_id)):
        try:
            best = max(best, os.path.getmtime(f))
        except OSError:
            pass
    for f in glob.glob(os.path.expanduser("~/.claude/projects/-home-dmz--datawatch-prd-worktrees-%s/*.jsonl" % prd_id)):
        try:
            best = max(best, os.path.getmtime(f))
        except OSError:
            pass
    return None if best == 0 else time.time() - best


def liveness(prd_id):
    """One-line sign of life for a silent worker: processes whose cwd is in its worktree, with state and wait channel.

    A worker that is only waiting shows pipe_read or do_wait with no CPU time; a live one is running, or its children are.
    """
    root = os.path.join(WT, prd_id)
    out = []
    for d in glob.glob("/proc/[0-9]*"):
        try:
            if not os.path.realpath(os.readlink(d + "/cwd")).startswith(root):
                continue
            pid = os.path.basename(d)
            st = open(d + "/stat").read().rsplit(")", 1)[1].split()
            cpu = (int(st[11]) + int(st[12])) / os.sysconf("SC_CLK_TCK")
            wchan = open(d + "/wchan").read().strip() or "-"
            out.append("%s:%s/%s/%.0fs" % (pid, st[0], wchan, cpu))
        except (OSError, IndexError, ValueError):
            continue
    return ("processes in worktree (pid:state/wait/cpu) " + " ".join(out[:6])) if out else "no process has its cwd in the worktree"


def usage_snapshot():
    """Append cumulative Claude token counters per session to /tmp/sv/usage-ledger.jsonl.

    Session counters are lifetime totals, so only the difference between two snapshots says how many tokens a window used.
    """
    d = get_json("https://localhost:8443/api/sessions", timeout=15, insecure=True)
    ss = d if isinstance(d, list) else (d or {}).get("sessions", [])
    rec = {"ts": int(time.time()), "s": {x["id"]: [x.get("model") or "default", x.get("tokens_in") or 0, x.get("tokens_out") or 0, x.get("name", "")[:30]]
                                         for x in ss if x.get("backend_family") == "claude-code"}}
    try:
        with open("/tmp/sv/usage-ledger.jsonl", "a") as f:
            f.write(json.dumps(rec) + "\n")
    except OSError:
        pass


def map_finding(text):
    """(stable finding kind, subject id) for one printed finding line, or None when the line is outside the stable vocabulary the daemon watch understands.

    The kinds are the daemon-side supervisor watch's stable vocabulary; subject is the id the line is about (the PRD id, the CI run id, the model host or lane name) or the empty string when the line carries no id.
    """
    m = re.match(r"^NEW (failed|review_needed) task \S+ of (\S+)", text)
    if m:
        return ("task_failed" if m.group(1) == "failed" else "task_review_needed"), m.group(2)
    m = re.match(r"^MERGE READY (\S+)", text)
    if m:
        return "automaton_not_merged", m.group(1)
    if text.startswith("TMUX"):
        return "tmux_restart", ""
    if text.startswith("DAEMON"):
        return "daemon_inactive", ""
    m = re.search(r"not resident on (\S+)", text)
    if text.startswith("MODEL") and m:
        # "MODEL host X unreachable" is not a resident-model finding: the daemon
        # watch reports nothing when a node cannot be reached, so leave it out
        # of the record rather than key it as model_not_resident with no node.
        return "model_not_resident", m.group(1)
    m = re.match(r"^IDLE (\S+) lane", text)
    if m:
        return "idle_lane_eligible_work", m.group(1).lower()
    m = re.match(r"^WORKTREE (\S+) has no go\.mod", text)
    if m:
        return "worktree_no_gomod", m.group(1)
    m = re.match(r"^SILENT (\S+)", text)
    if m:
        # The printed line is "SILENT <id>: ...", so \S+ catches the trailing
        # colon. The daemon-side watch keys this finding by the bare Automaton
        # id, so strip the colon — keeping it would make every silent
        # worktree a false one-sided difference in the compare.
        return "worktree_silent", m.group(1).rstrip(":")
    if text.startswith("MAIN checkout"):
        return "main_checkout_dirty", ""
    m = re.search(r"run (\d+)", text)
    sub = m.group(1) if m else ""
    if text.startswith("CI RED"):
        return "ci_red", sub
    if text.startswith("CI STUCK"):
        return "ci_stuck", sub
    return None


def main(argv=None):
    argv = sys.argv[1:] if argv is None else list(argv)
    record_path = None
    i = 0
    while i < len(argv):
        if argv[i] == "--record" and i + 1 < len(argv):
            record_path = argv[i + 1]
            i += 2
        else:
            i += 1

    findings, state = [], {}
    usage_snapshot()
    try:
        state = json.load(open(STATE))
    except Exception:
        pass
    seen = set(tuple(x) for x in state.get("held", []))

    # tmux and daemon
    t = sh("systemctl --user show datawatch-tmux -p NRestarts -p ActiveEnterTimestamp")
    nr = re.search(r"NRestarts=(\d+)", t)
    ae = re.search(r"ActiveEnterTimestamp=(.*)", t)
    if not nr or nr.group(1) != TMUX_RESTARTS or not ae or ae.group(1).strip() != TMUX_SINCE:
        findings.append("TMUX server restarted or unreadable (%s): audit every non-terminal PRD" % t.replace("\n", " ").strip())
    if sh("systemctl --user is-active datawatch").strip() != "active":
        findings.append("DAEMON is not active")

    # models
    for name, url in (("local", "http://127.0.0.1:11434"), ("remote", "http://100.64.0.20:11434")):
        m = models(url)
        if m is None:
            findings.append("MODEL host %s unreachable" % name)
        elif not any(MODEL in x for x in m):
            findings.append("MODEL %s not resident on %s (reload with keep_alive -1)" % (MODEL, name))

    # PRDs
    out = sh("datawatch autonomous prd-list", 60)
    prds = []
    try:
        prds = json.loads(out[out.index("{"):]).get("prds", [])
    except Exception:
        findings.append("PRD list unreadable")
    held_now, running, queued_runnable = [], [], 0
    for p in prds:
        st = p.get("status")
        tasks = [k for s in p.get("stories", []) for k in s.get("tasks", [])]
        if p["id"] in IGNORE:
            continue
        if st in ("needs_review", "approved", "draft") and recent(p.get("updated_at")) and (tasks or st == "draft"):
            queued_runnable += 1
        if st not in ("running", "blocked"):
            continue
        done = sum(1 for k in tasks if k.get("status") == "completed")
        for k in tasks:
            if k.get("status") in ("failed", "review_needed"):
                key = (p["id"], k["id"], k["status"])
                held_now.append(key)
                if key not in seen and p["id"] not in ("4f9da380", "67915a24", "6d3c36ca"):
                    findings.append("NEW %s task %s of %s (%s)" % (k["status"], k["id"], p["id"], (p.get("title") or "")[:40]))
        if tasks and done == len(tasks):
            findings.append("MERGE READY %s all %d tasks done: %s" % (p["id"], len(tasks), (p.get("title") or "")[:40]))
        if st == "running":
            running.append(p["id"])
            d = os.path.join(WT, p["id"])
            if not os.path.isfile(os.path.join(d, "go.mod")):
                findings.append("WORKTREE %s has no go.mod (broken project directory)" % p["id"])
            live = [k for k in tasks if k.get("status") in ("in_progress", "verifying")]
            if live:
                age = newest_activity(p["id"])
                if age is not None and age > SILENT_MIN * 60:
                    findings.append("SILENT %s: no file or log activity for %d min while %s is running; %s" % (p["id"], age // 60, live[0]["id"], liveness(p["id"])))

    # capacity and lanes
    cap = get_json("https://localhost:8443/api/capacity", insecure=True) or {}
    pools = {p["name"]: p for p in cap.get("pools", [])}
    waiting = cap.get("waiting") or []
    cl, pi = pools.get("llm:claude-code", {}), pools.get("llm:pi-prompt", {})
    summary = "claude %s/%s pi %s/%s waiting %d prds-running %d" % (
        cl.get("held", "?"), cl.get("limit", "?"), pi.get("held", "?"), pi.get("limit", "?"), len(waiting), len(running))
    if cl.get("limit") and cl.get("held", 0) == 0 and queued_runnable:
        findings.append("IDLE Claude lane while %d PRD(s) are needs_review, approved or draft" % queued_runnable)
    if pi.get("limit") and pi.get("held", 0) < pi.get("limit", 0) and (waiting or queued_runnable):
        findings.append("IDLE pi lane (%s/%s held) with %d waiting and %d queued PRDs" % (pi.get("held"), pi.get("limit"), len(waiting), queued_runnable))

    # node balancing invariants (one engine per node; every running pi worker holds a node lease; no idle node with waiters)
    for name, pool in pools.items():
        if name.startswith("node:") and pool.get("limit") and pool.get("held", 0) > pool["limit"]:
            findings.append("NODE %s holds %s sessions against a limit of %s (balancing broken)" % (name, pool["held"], pool["limit"]))
    for l in cap.get("leases") or []:
        lp = l.get("pools") or []
        if "llm:pi-prompt" in lp and not any(x.startswith("node:") for x in lp):
            why = "node unknown after adoption" if l.get("node_unknown") else "no node lease"
            findings.append("LEASE %s holds a pi-prompt slot with %s" % (l.get("holder"), why))
    idle_nodes = [n for n, pool in pools.items() if n.startswith("node:") and pool.get("limit") and pool.get("held", 0) == 0 and n not in ("node:datawatch-openai", "node:verdict")]
    node_waiters = [w for w in waiting if "waiting for node" in str(w.get("reason", ""))]
    if idle_nodes and node_waiters:
        findings.append("PLACEMENT idle %s while %d request(s) wait for a busy node" % (",".join(idle_nodes), len(node_waiters)))

    # main checkout
    dirty = [l for l in sh("git -C %s status --short" % BASE).splitlines() if not l.startswith("??")]
    if dirty:
        findings.append("MAIN checkout has %d uncommitted tracked file(s): a ship will abort until committed" % len(dirty))
    ab = sh("git -C %s rev-list --left-right --count origin/main...main" % BASE).split()
    if len(ab) == 2 and ab[1] != "0":
        findings.append("MAIN is %s commit(s) ahead of origin: push pending" % ab[1])

    # CI on main and tags
    ci = sh("gh run list --limit 20 --json workflowName,conclusion,status,databaseId,headBranch,displayTitle,createdAt", 30)
    try:
        for r in json.loads(ci):
            if r["workflowName"] == "Release CI Summary":
                continue
            if not (r["headBranch"] == "main" or r["headBranch"].startswith("v")):
                continue
            age = time.time() - time.mktime(time.strptime(r["createdAt"], "%Y-%m-%dT%H:%M:%SZ")) + time.timezone
            if r["status"] == "completed" and r["conclusion"] not in ("success", "skipped", "cancelled"):
                if age < 6 * 3600 and r["databaseId"] not in ACKED_RUNS and not (CODEQL_CONFLICT_KNOWN and r["workflowName"] == "CodeQL"):
                    findings.append("CI RED %s run %s (%s)" % (r["workflowName"], r["databaseId"], r["displayTitle"][:40]))
            elif r["status"] == "in_progress" and age > 150 * 60 and r["workflowName"] == "Release":
                findings.append("CI STUCK? Release run %s in progress for %d min" % (r["databaseId"], age // 60))
    except Exception:
        findings.append("CI unreadable (gh rate limit or auth): check manually in the hourly tick")

    try:
        os.makedirs(os.path.dirname(STATE), exist_ok=True)
        json.dump({"held": held_now, "at": time.time()}, open(STATE, "w"))
    except OSError:
        pass

    if record_path is not None:
        recs = []
        for f in findings:
            mapped = map_finding(f)
            if mapped is None:
                continue  # outside the stable vocabulary; the daemon watch cannot key it
            recs.append({"kind": mapped[0], "subject": mapped[1], "text": f})
        try:
            d = os.path.dirname(record_path)
            if d:
                os.makedirs(d, exist_ok=True)
            with open(record_path, "w") as fh:
                json.dump({"at": int(time.time()), "findings": recs}, fh)
        except OSError as e:
            print("record write failed: %s" % e, file=sys.stderr)

    if findings:
        print("ATTENTION " + summary)
        for f in findings:
            print(" - " + f)
        return 1
    print("OK " + summary)
    return 0


if __name__ == "__main__":
    sys.exit(main())
