#!/usr/bin/env bash
#
# test-release-ci-summary.sh
#
# Runs the logic of .github/workflows/release-ci-summary.yaml against made-up
# GitHub API replies: the job's `if` gate, the "Resolve tag" shell step (run
# under `bash -e`, as the runner does) and the table step. `gh` is replaced by
# a fake that records every API request, so no network access happens and no
# token is needed.
#
# What it asserts:
#   - the automatic path does work only for a completed tag-push run of a
#     release tag (vX.Y.Z) in this repository; anything else makes 0 requests;
#   - the tag comes from the event, and finding its commit takes at most two
#     requests, never a tag or release listing;
#   - a tag that no longer points at the run's commit writes nothing;
#   - a failed lookup fails the step instead of carrying on without a commit;
#   - a manual run takes an existing version tag and rejects any other input;
#   - the table is written only to the release of that tag.
#
# Usage: bash tests/test-release-ci-summary.sh [workflow-file]
# Needs python3 with PyYAML, and jq; fails (does not skip) without them.

set -uo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
wf="${1:-$root/.github/workflows/release-ci-summary.yaml}"

for tool in python3 jq bash; do
  command -v "$tool" >/dev/null 2>&1 || { echo "FAIL environment - $tool is not on PATH"; exit 1; }
done

work="$(mktemp -d)" || { echo "FAIL environment - mktemp failed"; exit 1; }
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"

# The fake gh. Replies come from $FAKE_REPLIES (endpoint -> JSON). An endpoint
# without a reply answers like GitHub does: error JSON on stdout, exit 1.
cat > "$work/bin/gh" <<'FAKE'
#!/usr/bin/env python3
import json, os, subprocess, sys

def log(rec):
    with open(os.environ["FAKE_LOG"], "a") as fh:
        fh.write(json.dumps(rec) + "\n")

args = sys.argv[1:]
if args[:2] == ["release", "edit"]:
    rest, opts, i = [], {}, 2
    while i < len(args):
        if args[i].startswith("--"):
            opts[args[i]] = args[i + 1]; i += 2
        else:
            rest.append(args[i]); i += 1
    log({"kind": "edit", "tag": rest[0], "notes": opts.get("--notes", "")})
    sys.exit(0)

if not args or args[0] != "api":
    sys.stderr.write("fake gh: unsupported command %r\n" % args); sys.exit(2)

positional, jqexpr, i = [], None, 1
while i < len(args):
    if args[i] == "--jq":
        jqexpr = args[i + 1]; i += 2
    else:
        positional.append(args[i]); i += 1
if len(positional) != 1:
    # Real gh rejects this before sending any request.
    sys.stderr.write("accepts 1 arg(s), received %d\n" % len(positional)); sys.exit(1)

endpoint = positional[0]
log({"kind": "api", "endpoint": endpoint})
replies = json.load(open(os.environ["FAKE_REPLIES"]))
if endpoint not in replies:
    sys.stdout.write('{"message":"Not Found","status":"404"}')
    sys.stderr.write("gh: Not Found (HTTP 404)\n"); sys.exit(1)
body = json.dumps(replies[endpoint])
if jqexpr is None:
    sys.stdout.write(body)
else:
    r = subprocess.run(["jq", "-r", jqexpr], input=body, capture_output=True, text=True)
    sys.stdout.write(r.stdout); sys.stderr.write(r.stderr); sys.exit(r.returncode)
FAKE
chmod +x "$work/bin/gh"

python3 - "$wf" "$work" "$root" <<'PY'
import glob, json, os, re, subprocess, sys

wf_path, work, root = sys.argv[1:4]
try:
    import yaml
except ImportError as exc:
    print("FAIL environment - PyYAML cannot be imported: %s" % exc); sys.exit(1)

doc = yaml.safe_load(open(wf_path))
on = doc.get("on", doc.get(True))
job = doc["jobs"]["update-release-status"]
steps = job["steps"]
resolve = next(s for s in steps if s.get("id") == "resolve")
table = next(s for s in steps if s.get("name", "").startswith("Build CI status table"))

REPO = "dmz006/datawatch"
C1 = "1" * 40   # the tagged commit
C2 = "2" * 40   # some other commit
TOBJ = "a" * 40 # an annotated tag object

failed = []
def check(name, ok, detail=""):
    if ok:
        print("ok " + name)
    else:
        failed.append(name)
        print("FAIL %s - %s" % (name, detail))

def context(event="workflow_run", branch="v9.1.0", run_event="push", sha=C1,
            head_repo=REPO, input_tag=""):
    c = {"github.event_name": event, "github.repository": REPO,
         "secrets.GITHUB_TOKEN": "fake", "github.event.inputs.tag": input_tag,
         "inputs.tag": input_tag}
    run = event == "workflow_run"
    c["github.event.workflow_run.head_branch"] = branch if run else ""
    c["github.event.workflow_run.event"] = run_event if run else ""
    c["github.event.workflow_run.head_sha"] = sha if run else ""
    c["github.event.workflow_run.head_repository.full_name"] = head_repo if run else ""
    return c

def job_runs(ctx):
    """Evaluate the job-level `if` (only the operators this workflow needs)."""
    expr = job.get("if")
    if expr is None:
        return True
    expr = str(expr).strip()
    if expr.startswith("${{"):
        expr = expr[3:-2]
    tokens = re.findall(r"'[^']*'|&&|\|\||==|!=|[(),]|[A-Za-z_][A-Za-z0-9_.]*|\S", expr)
    out = []
    for t in tokens:
        if t.startswith("'") or t in ("==", "!=", "(", ")", ",", "startsWith"):
            out.append(t)
        elif t in ("&&", "||"):
            out.append("and" if t == "&&" else "or")
        elif t in ctx:
            out.append(repr(ctx[t]))
        else:
            raise SystemExit("FAIL gate - the test cannot evaluate token %r" % t)
    return bool(eval(" ".join(out), {"__builtins__": {}},
                     {"startsWith": lambda a, b: a.lower().startswith(b.lower())}))

def subst(text, ctx):
    return re.sub(r"\$\{\{\s*([^}]*?)\s*\}\}", lambda m: ctx.get(m.group(1), ""), str(text))

def run_step(step, ctx, replies, outputs=None):
    log, rep, out = [os.path.join(work, n) for n in ("log", "replies.json", "output")]
    for p in (log, out):
        open(p, "w").close()
    json.dump(replies, open(rep, "w"))
    env = {"PATH": os.path.join(work, "bin") + os.pathsep + os.environ["PATH"],
           "FAKE_LOG": log, "FAKE_REPLIES": rep, "GITHUB_OUTPUT": out}
    for k, v in (step.get("env") or {}).items():
        v = str(v)
        m = re.fullmatch(r"\$\{\{\s*steps\.resolve\.outputs\.(\w+)\s*\}\}", v)
        env[k] = (outputs or {}).get(m.group(1), "") if m else subst(v, ctx)
    r = subprocess.run(["bash", "-e", "-c", subst(step["run"], ctx)], env=env, cwd=work,
                       capture_output=True, text=True, timeout=120)
    recs = [json.loads(l) for l in open(log)]
    return {"rc": r.returncode, "stdout": r.stdout, "stderr": r.stderr,
            "api": [x["endpoint"] for x in recs if x["kind"] == "api"],
            "edits": [x for x in recs if x["kind"] == "edit"],
            "out": dict(l.rstrip("\n").split("=", 1) for l in open(out) if "=" in l)}

def automatic(ctx, replies):
    """The whole resolve stage as GitHub would run it: gate first, then step."""
    if not job_runs(ctx):
        return {"rc": 0, "api": [], "edits": [], "out": {}, "gated": True, "stdout": "", "stderr": ""}
    r = run_step(resolve, ctx, replies)
    r["gated"] = False
    return r

def ref(tag, kind, sha):
    return {"repos/%s/git/ref/tags/%s" % (REPO, tag):
            {"ref": "refs/tags/" + tag, "object": {"type": kind, "sha": sha}}}

def tagobj(sha, kind, target):
    return {"repos/%s/git/tags/%s" % (REPO, sha): {"object": {"type": kind, "sha": target}}}

def merged(*ds):
    out = {}
    for d in ds:
        out.update(d)
    return out

# A repository with many older tags, so that any walk over them is visible.
MANY = ["v1.0.%d" % i for i in range(40)]
listing = {"repos/%s/git/refs/tags?per_page=30" % REPO:
           [{"ref": "refs/tags/" + t} for t in MANY]}
many = merged(listing, *[ref(t, "commit", C2) for t in MANY])

def proceeds(r):
    return r["rc"] == 0 and r["out"].get("skip") == "false"

def writes_nothing(r):
    return r["gated"] or (r["rc"] == 0 and r["out"].get("skip") == "true")

def brief(r):
    return "rc=%s requests=%d out=%s stderr=%r" % (r["rc"], len(r["api"]), r["out"], r["stderr"][-200:])

# 1. Runs that must cost nothing.
for name, ctx in [
    ("main push run", context(branch="main")),
    ("run started by another workflow run (head_branch main)", context(branch="main", run_event="workflow_run")),
    ("fork pull request from a branch named like a tag", context(run_event="pull_request", head_repo="someone/datawatch")),
    ("own pull request from a branch named like a tag", context(run_event="pull_request")),
    ("push run from another repository", context(head_repo="someone/datawatch")),
    ("scheduled run", context(branch="main", run_event="schedule")),
    ("branch beginning with v", context(branch="vendor-update")),
    ("pre-release tag", context(branch="v9.1.0-rc1")),
    ("partial version", context(branch="v9.1")),
]:
    r = automatic(ctx, many)
    check("no work: " + name, writes_nothing(r) and not r["api"], brief(r))

# 2. Release tag runs.
r = automatic(context(), merged(many, ref("v9.1.0", "commit", C1)))
check("lightweight release tag proceeds", proceeds(r) and r["out"].get("tag") == "v9.1.0" and r["out"].get("sha") == C1, brief(r))
check("lightweight release tag takes 1 request", len(r["api"]) == 1, brief(r))

r = automatic(context(), merged(many, ref("v9.1.0", "tag", TOBJ), tagobj(TOBJ, "commit", C1)))
check("annotated release tag proceeds", proceeds(r) and r["out"].get("tag") == "v9.1.0" and r["out"].get("sha") == C1, brief(r))
check("annotated release tag takes 2 requests", len(r["api"]) == 2, brief(r))

# 3. A commit with no matching tag must not walk the tags.
r = automatic(context(branch="main"), many)
check("untagged commit never lists tags or releases",
      not any("/git/refs/tags" in e or "/releases" in e for e in r["api"]) and len(r["api"]) <= 2, brief(r))

# 4. The tag was moved after the run started.
r = automatic(context(), ref("v9.1.0", "commit", C2))
check("moved tag writes nothing", writes_nothing(r), brief(r))

# 5. Failed or unusable lookups fail the step and never report "go ahead".
for name, replies in [
    ("tag lookup fails", {}),
    ("annotated tag object lookup fails", ref("v9.1.0", "tag", TOBJ)),
    ("tag points at a tree", ref("v9.1.0", "tree", C1)),
    ("reply has no usable id", ref("v9.1.0", "commit", "nope")),
]:
    r = automatic(context(), replies)
    check("fails loudly: " + name, r["rc"] != 0 and r["out"].get("skip") != "false", brief(r))

# 6. Manual runs.
def manual(tag, replies):
    return automatic(context(event="workflow_dispatch", input_tag=tag), replies)

r = manual("v9.1.0", ref("v9.1.0", "tag", TOBJ) | tagobj(TOBJ, "commit", C1))
check("manual run on an annotated tag", proceeds(r) and r["out"].get("sha") == C1 and len(r["api"]) <= 2, brief(r))
r = manual("v7.0.0-alpha.3", ref("v7.0.0-alpha.3", "commit", C1))
check("manual run accepts a pre-release tag", proceeds(r) and r["out"].get("tag") == "v7.0.0-alpha.3", brief(r))
r = manual("v0.0.99", {})
check("manual run on a missing tag fails", r["rc"] != 0 and r["out"].get("skip") != "false", brief(r))

marker = os.path.join(work, "injected")
for bad in ['v1.0.0"; touch %s; "' % marker, "v1.0.0$(touch %s)" % marker,
            "v1.0.0?per_page=100", "../../releases", "v1.0.0\nskip=false", ""]:
    r = manual(bad, many)
    check("manual run rejects %r" % bad[:24],
          r["rc"] != 0 and not r["api"] and r["out"].get("skip") != "false" and not os.path.exists(marker), brief(r))

# 7. Table step: writes only the release of the resolved tag.
runs = {"workflow_runs": [{"name": "Release", "status": "completed", "conclusion": "success",
        "html_url": "https://example.invalid/1", "run_started_at": "2026-01-01T00:00:00Z",
        "updated_at": "2026-01-01T00:05:00Z"}]}
res = automatic(context(), ref("v9.1.0", "commit", C1))
replies = {"repos/%s/actions/runs?head_sha=%s&per_page=100" % (REPO, C1): runs,
           "repos/%s/releases/tags/v9.1.0" % REPO: {"body": "Notes\n\n## CI Status\n\nold table"}}
r = run_step(table, context(), replies, res["out"])
check("table goes to the tag's release only",
      r["rc"] == 0 and [e["tag"] for e in r["edits"]] == ["v9.1.0"]
      and r["edits"][0]["notes"].startswith("Notes\n\n## CI Status")
      and "old table" not in r["edits"][0]["notes"] and "| 5m 0s |" in r["edits"][0]["notes"], brief(r))
del replies["repos/%s/releases/tags/v9.1.0" % REPO]
r = run_step(table, context(), replies, res["out"])
check("no release for the tag: nothing edited", r["rc"] == 0 and not r["edits"], brief(r))

# 8. The trigger list holds only workflows that can pass the gate: those that
#    start on a tag push.
names = {}
for p in glob.glob(os.path.join(root, ".github", "workflows", "*.y*ml")):
    d = yaml.safe_load(open(p))
    names[d.get("name")] = d.get("on", d.get(True))
listed = on["workflow_run"]["workflows"]
idle = [n for n in listed
        if not (isinstance(names.get(n), dict) and (names[n].get("push") or {}).get("tags"))]
check("every listed workflow starts on a tag push", listed and not idle, "no tag trigger: %s" % idle)
check("manual dispatch is still offered", "tag" in on["workflow_dispatch"]["inputs"], "missing input")

if failed:
    print("FAIL (%d check(s) failed)" % len(failed)); sys.exit(1)
print("PASS")
PY
