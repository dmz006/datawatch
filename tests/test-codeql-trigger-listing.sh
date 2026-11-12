#!/usr/bin/env bash
#
# test-codeql-trigger-listing.sh
#
# Asserts the trigger listing and gating of the CodeQL alert-gate workflow
# (.github/workflows/codeql.yaml), which the workflow's own header documents:
#
#   1. on.push.paths-ignore contains docs/**, CHANGELOG.md and **/*.md (a
#      doc-only push starts no run), and on.push has no paths list.
#   2. on.push.branches is exactly [main].
#   3. on.pull_request.paths contains each of **/*.go, go.mod, go.sum,
#      channel/**, internal/server/web/**, **/*.js, **/*.ts, **/*.mjs,
#      **/*.py, .github/workflows/** and scripts/**, and on.pull_request
#      has no paths-ignore.
#   4. on.pull_request.branches is exactly [main].
#   5. on.schedule is exactly one entry with cron '17 4 * * 1'.
#   6. on.workflow_dispatch is present.
#   7. The top-level permissions are exactly {contents: read}, and the
#      alert-gate job's permissions are exactly {security-events: read,
#      contents: read}.
#   8. The top-level concurrency group is 'codeql-${{ github.ref }}' with
#      cancel-in-progress true.
#
# The workflow file is only read, never written; nothing is written into
# the repository.
#
# Usage: bash tests/test-codeql-trigger-listing.sh [workflow-file]
#        The workflow file defaults to .github/workflows/codeql.yaml,
#        resolved from the script's own directory, so the script runs
#        from any working directory.
# Output: one 'ok <check>' or 'FAIL <check> - <reason>' line per check,
#         then a final PASS or FAIL line; exits non-zero if any check
#         failed. python3 with the PyYAML module is required: if PyYAML
#         cannot be imported the script fails loudly instead of skipping.

set -uo pipefail

fail_env() {
  printf 'FAIL %s - %s\n' "$1" "$2"
  echo "FAIL (1 check(s) failed)"
  exit 1
}

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
wf="${1:-$root/.github/workflows/codeql.yaml}"

py="$(command -v python3)" || fail_env environment "python3 is not on PATH but is required to load Yaml"
[ -r "$wf" ] || fail_env "load" "workflow file is not readable: $wf"

# The workflow file is passed as a here-document argument list so a path
# containing whitespace or quotes cannot be mangled on its way into the
# interpreter. PyYAML reads the key `on:` as the boolean True, so the
# trigger map is looked up as doc.get('on', doc.get(True)).
"$py" - "$wf" <<'PY'
import sys

args = sys.argv[1:]
try:
    import yaml
except ImportError as exc:
    print("FAIL yaml - PyYAML is required (%s)" % exc)
    print("FAIL (1 check(s) failed)")
    sys.exit(1)

try:
    with open(args[0], encoding="utf-8") as fh:
        d = yaml.safe_load(fh)
except Exception as exc:
    print("FAIL load - cannot parse %s: %s" % (args[0], exc))
    print("FAIL (1 check(s) failed)")
    sys.exit(1)

if not isinstance(d, dict):
    print("FAIL load - workflow file is not a YAML mapping")
    print("FAIL (1 check(s) failed)")
    sys.exit(1)

fails = 0

def ok(name):
    print("ok   %s" % name)

def fail(name, reason):
    global fails
    fails += 1
    print("FAIL %s - %s" % (name, reason))

on = d.get("on", d.get(True))
if not isinstance(on, dict):
    on = {}
    for name in ("push-ignore", "push-branches", "pr-paths", "pr-branches",
                 "schedule", "dispatch", "permissions", "concurrency"):
        fail(name, "no trigger mapping found under on:")

push = on.get("push")
if not isinstance(push, dict):
    push = {}
pr = on.get("pull_request")
if not isinstance(pr, dict):
    pr = {}

# 1. push: docs/**, CHANGELOG.md and **/*.md are ignored; no paths list.
pi = push.get("paths-ignore")
missing = [p for p in ("docs/**", "CHANGELOG.md", "**/*.md")
           if not (isinstance(pi, list) and p in pi)]
if missing and "paths" not in push:
    fail("push-ignore", "paths-ignore is missing %s" % ", ".join(missing))
elif not missing and "paths" in push:
    fail("push-ignore", "on.push must have no paths list, got: %r" % (push.get("paths"),))
elif missing:
    fail("push-ignore", "paths-ignore is missing %s and a paths list is present" % ", ".join(missing))
else:
    ok("push: paths-ignore has the doc patterns and no paths list")

# 2. push.branches is exactly [main].
if push.get("branches") == ["main"]:
    ok("push: branches is exactly [main]")
else:
    fail("push-branches", "expected exactly ['main'], got %r" % (push.get("branches"),))

# 3. pull_request.paths has every tracked-code pattern; no paths-ignore.
want = ["**/*.go", "go.mod", "go.sum", "channel/**", "internal/server/web/**",
        "**/*.js", "**/*.ts", "**/*.mjs", "**/*.py",
        ".github/workflows/**", "scripts/**"]
paths = pr.get("paths")
missing = [p for p in want if not (isinstance(paths, list) and p in paths)]
extra_gate = "paths-ignore" in pr
if missing and extra_gate:
    fail("pr-paths", "paths is missing %s and a paths-ignore is present" % ", ".join(missing))
elif missing:
    fail("pr-paths", "paths is missing %s" % ", ".join(missing))
elif extra_gate:
    fail("pr-paths", "on.pull_request must have no paths-ignore, got: %r" % (pr.get("paths-ignore"),))
else:
    ok("pull_request: paths has every tracked-code pattern and no paths-ignore")

# 4. pull_request.branches is exactly [main].
if pr.get("branches") == ["main"]:
    ok("pull_request: branches is exactly [main]")
else:
    fail("pr-branches", "expected exactly ['main'], got %r" % (pr.get("branches"),))

# 5. on.schedule: exactly one entry, cron '17 4 * * 1'.
sched = on.get("schedule")
if (isinstance(sched, list) and len(sched) == 1
        and isinstance(sched[0], dict) and sched[0].get("cron") == "17 4 * * 1"):
    ok("schedule: exactly one entry with cron '17 4 * * 1'")
else:
    fail("schedule", "expected exactly one entry with cron '17 4 * * 1', got %r" % (sched,))

# 6. workflow_dispatch is present.
if "workflow_dispatch" in on:
    ok("workflow_dispatch: present")
else:
    fail("dispatch", "on.workflow_dispatch is missing")

# 7. Least privilege, exactly.
top_perm = {"contents": "read"}
gate_perm = {"security-events": "read", "contents": "read"}
job = (d.get("jobs") or {}).get("alert-gate") or {}
if d.get("permissions") == top_perm and job.get("permissions") == gate_perm:
    ok("permissions: top-level contents: read and alert-gate security-events: read, contents: read")
else:
    fail("permissions", "top-level %r (want %r), alert-gate %r (want %r)" % (
        d.get("permissions"), top_perm, job.get("permissions"), gate_perm))

# 8. Concurrency is keyed on the ref and cancels the older run.
conc = d.get("concurrency") or {}
if (conc.get("group") == "codeql-${{ github.ref }}"
        and conc.get("cancel-in-progress") is True):
    ok("concurrency: group is codeql-${{ github.ref }} with cancel-in-progress true")
else:
    fail("concurrency", "got %r (want group 'codeql-${{ github.ref }}', cancel-in-progress True)" % (conc,))

if fails:
    print("FAIL (%d check(s) failed)" % fails)
    sys.exit(1)
print("PASS")
PY

exit "$?"
