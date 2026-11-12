#!/usr/bin/env python3
"""Check that SARIF uploads never block a release or a publishing job.

SARIF uploads to GitHub Code Scanning are reporting only. A rate limit or an
outage on the upload endpoint must never fail a release or stop a publishing
job, so every upload step must tolerate failure, be retried, and never sit in
the dependency chain of a publishing job as a blocking step. The real gate is
the gosec baseline diff, which must stay blocking.

Usage: python3 scripts/check-sarif-upload-nonblocking.py [PATH ...]
Each PATH is a workflow file or a directory whose *.yaml and *.yml files are
scanned. With no PATH the repository's .github/workflows is scanned.
Exit 0 when all rules pass, 1 when any rule fails, 2 on a usage error.
Rules: R1 blocking-upload, R2 retry-attempts, R3 publish-dependency,
R4 gate-stays-blocking. The script only reads files.
"""
import os
import sys

UPLOAD = "github/codeql-action/upload-sarif"
MIN_ATTEMPTS = 3


def die(msg):
    print("FAIL " + msg)
    sys.exit(2)


def steps_of(job):
    s = job.get("steps") if isinstance(job, dict) else None
    return [x for x in s if isinstance(x, dict)] if isinstance(s, list) else []


def is_true(v):
    return v is True or (isinstance(v, str) and v.strip().lower() == "true")


def is_upload(step):
    return str(step.get("uses") or "").startswith(UPLOAD)


def nonblocking(step):
    return is_true(step.get("continue-on-error"))


def is_publishing(job):
    for st in steps_of(job):
        uses = str(st.get("uses") or "")
        if uses.startswith("goreleaser/goreleaser-action"):
            return True
        if uses.startswith("docker/build-push-action"):
            w = st.get("with")
            if isinstance(w, dict) and is_true(w.get("push")):
                return True
        if "gh release upload" in str(st.get("run") or ""):
            return True
    return False


def needs_of(job):
    n = job.get("needs") if isinstance(job, dict) else None
    if isinstance(n, str):
        return [n]
    return [x for x in n if isinstance(x, str)] if isinstance(n, list) else []


def reachable(jobs, name):
    seen, todo = set(), needs_of(jobs.get(name, {}))
    while todo:
        j = todo.pop()
        if j in seen or j not in jobs:
            continue
        seen.add(j)
        todo.extend(needs_of(jobs[j]))
    return seen


def check(path, yaml):
    out = []
    with open(path, encoding="utf-8") as fh:
        doc = yaml.safe_load(fh)
    jobs = doc.get("jobs") if isinstance(doc, dict) else None
    if not isinstance(jobs, dict):
        return out
    # R1 blocking-upload: every upload step sets continue-on-error true.
    for jn, job in jobs.items():
        for st in steps_of(job):
            if is_upload(st) and not nonblocking(st):
                out.append(("R1", "job %s step %r does not set continue-on-error: true" % (jn, st.get("name", UPLOAD))))
    # R2 retry-attempts: a job with any upload has at least three of them.
    for jn, job in jobs.items():
        n = sum(1 for st in steps_of(job) if is_upload(st))
        if 0 < n < MIN_ATTEMPTS:
            out.append(("R2", "job %s has %d upload-sarif step(s), want at least %d" % (jn, n, MIN_ATTEMPTS)))
    # R3 publish-dependency: no blocking upload upstream of a publishing job.
    for jn, job in jobs.items():
        if not is_publishing(job):
            continue
        for up in sorted(reachable(jobs, jn)):
            if any(is_upload(st) and not nonblocking(st) for st in steps_of(jobs[up])):
                out.append(("R3", "publishing job %s depends on job %s, which has a blocking upload-sarif step" % (jn, up)))
    # R4 gate-stays-blocking: release.yaml keeps its blocking baseline check.
    if os.path.basename(path) == "release.yaml" and "gosec" in jobs:
        gates = [st for st in steps_of(jobs["gosec"])
                 if "baseline" in str(st.get("name") or "") and "sys.exit(1)" in str(st.get("run") or "")]
        if not gates:
            out.append(("R4", "gosec job has no baseline step that runs sys.exit(1)"))
        elif any("continue-on-error" in g for g in gates):
            out.append(("R4", "gosec baseline gate must not set continue-on-error"))
    return out


def collect(paths):
    files = []
    for p in paths:
        if os.path.isdir(p):
            files += [os.path.join(p, f) for f in sorted(os.listdir(p)) if f.endswith((".yaml", ".yml"))]
        elif os.path.isfile(p):
            files.append(p)
        else:
            die("path does not exist: " + p)
    return files


def main(argv):
    if any(a in ("-h", "--help") for a in argv):
        print(__doc__.split("\n\n")[2])
        return 0
    try:
        import yaml
    except ImportError as e:
        die("PyYAML import failed: %s" % e)
    root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    files = collect(argv or [os.path.join(root, ".github", "workflows")])
    bad = 0
    for f in files:
        try:
            res = check(f, yaml)
        except (OSError, yaml.YAMLError) as e:
            die("cannot read %s: %s" % (f, e))
        for rule, msg in res:
            print("FAIL %s %s: %s" % (rule, f, msg))
        bad += len(res)
        if not res:
            print("ok " + f)
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
