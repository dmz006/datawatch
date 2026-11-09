#!/usr/bin/env python3
"""Offline unit tests for the rate-limit paths of scripts/codeql-alert-gate.sh.

Run:
    python3 -I scripts/tests/test_codeql_alert_gate.py

The test drives the gate script through a subprocess, substituting both
test seams the script exposes:

  * CODEQL_GATE_API_COMMAND points at a stub executable written into a
    temporary directory (created and removed per test). The stub selects
    its behaviour by the endpoint name in its arguments and is
    configured purely through STUB_* environment variables:
      - every call to the analyses or alerts endpoint is counted in a
        file; calls whose endpoint has failed fewer times than its
        configured fail count print the configured stderr text (default:
        "API rate limit exceeded for installation (HTTP 403)") and exit
        1, and after that the configured rows are printed one per line
        and the call exits 0. The stub ignores --jq.
      - a call whose arguments contain "rate_limit" by default fails
        with exit 1 (so the no-reset interval fallback is exercised);
        a case can flip STUB_RATE_LIMIT_FAIL to make it print the reset
        epoch, the current time plus STUB_RATE_LIMIT_RESET_OFFSET
        seconds, and exit 0.
  * CODEQL_GATE_SLEEP_COMMAND points at a stub that appends its seconds
    argument to a log file in the temp directory and returns at once.

There are no real API calls and no real sleeps anywhere.

Covered (wait-analyses unless stated):
  1. Three rate-limit responses then success: the recorded sleeps are
     exactly 60, 120 and 240 and the output never contains
     "No analysis for".
  2. Reset honoured: one rate-limit response, the rate_limit call
     returns now+90, the first recorded sleep is about 90 (within 5s).
  3. A missing category never appears: exit 1 with "No analysis for",
     no single sleep exceeds 300, and the total slept stays within
     WAIT_MAX_SECONDS (set to 700 here).
  4. Rate limited with no reset reported until the budget runs out
     (WAIT_MAX_SECONDS=500, endless rate limits): exit 1, output
     contains "Rate limited", does not contain "No analysis for", and
     the recorded sleeps are exactly 60, 120, 240, 80 (the last wait
     clamped to the remaining budget).
  5. A reset beyond the budget (WAIT_MAX_SECONDS=500, reset 10**6 seconds
     away): exit 1, output contains "Rate limited" and "not a code problem",
     does not contain "No analysis for", no sleep is called, and the
     analyses endpoint is called once.
  6. A reset inside the budget (WAIT_MAX_SECONDS=500, reset 90 seconds
     away, one analyses failure): exit 0, exactly one sleep of about 90
     seconds (within 5s), the success message is printed, and the
     analyses endpoint is called twice.
  7. open-alerts: three rate-limit responses then no rows exits 0 with
     the clean no-alerts message.
  8. open-alerts: one high row exits 1 and the output contains
     "0 critical and 1 high".
  9. open-alerts: a failed read
     ("Resource not accessible by integration (HTTP 403)") exits 1 and
     the clean message is never printed.
 10. open-alerts: a rate-limit reset beyond the budget (WAIT_MAX_SECONDS=500,
     reset 10**6 seconds away) stops at once: exit 1, output contains
     "Rate limited" and "not a code problem", does not contain the clean
     no-alerts message, no sleep is called, and the alerts endpoint is
     called exactly once.
 11. open-alerts: a rate-limit reset inside the budget (WAIT_MAX_SECONDS=500,
     reset 90 seconds away, one alerts failure): exit 0, exactly one sleep of
     about 90 seconds (within 5s), the clean no-alerts message is printed, and
     the alerts endpoint is called twice.
 12. An unknown subcommand exits 2.
 13. .github/workflows/codeql.yaml checks out the repository (actions/checkout)
     before it invokes bash scripts/codeql-alert-gate.sh (a missing checkout
     made every CI run fail with the script absent from the runner).
"""

import os
import stat
import subprocess
import sys
import tempfile
import unittest

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
GATE = os.path.join(REPO_ROOT, "scripts", "codeql-alert-gate.sh")

EXPECTED_CATEGORIES = "/language:go /language:python"
SHA = "a1" * 20

SLEEP_STUB = r"""#!/usr/bin/env bash
# Test stub for the sleep command: append the requested seconds to the
# log file and return immediately (no real sleeping).
printf '%s\n' "$1" >> "${STUB_SLEEP_LOG:?STUB_SLEEP_LOG not set}"
exit 0
"""

API_STUB = r"""#!/usr/bin/env bash
# Test stub for the API command (substituted for `gh'). Selects its
# behaviour by the endpoint name in the arguments. Configuration comes
# from STUB_* environment variables; --jq and other flags are ignored.
endpoint=""
for arg in "$@"; do
  case "$arg" in
    rate_limit*) endpoint="rate_limit" ;;
    *code-scanning/alerts*) endpoint="alerts" ;;
    *code-scanning/analyses*) endpoint="analyses" ;;
  esac
done

case "$endpoint" in
  rate_limit)
    if [ "${STUB_RATE_LIMIT_FAIL:-1}" = "1" ]; then
      printf 'stub: rate_limit lookup failed\n' >&2
      exit 1
    fi
    printf '%s\n' "$(( $(date +%s) + ${STUB_RATE_LIMIT_RESET_OFFSET:-0} ))"
    exit 0
    ;;
  analyses|alerts)
    counter="${STUB_COUNT_FILE:?STUB_COUNT_FILE not set}"
    n=0
    [ -f "$counter" ] && n="$(<"$counter")"
    n=$((n + 1))
    printf '%s\n' "$n" > "$counter"
    up="$(printf '%s' "$endpoint" | tr a-z A-Z)"
    var="STUB_${up}_FAIL_COUNT"
    fail_n="${!var:-0}"
    if [ "$n" -le "$fail_n" ]; then
      printf '%s\n' "${STUB_FAIL_STDERR:-API rate limit exceeded for installation (HTTP 403)}" >&2
      exit 1
    fi
    var="STUB_OK_${up}_FILE"
    ok_file="${!var:-}"
    if [ -n "$ok_file" ] && [ -f "$ok_file" ]; then
      cat "$ok_file"
    fi
    exit 0
    ;;
  *)
    printf 'stub: no recognised endpoint in arguments: %s\n' "$*" >&2
    exit 1
    ;;
esac
"""


def write_stub(path, content):
    with open(path, "w") as fh:
        fh.write(content)
    os.chmod(path, os.stat(path).st_mode | stat.S_IEXEC | stat.S_IXGRP | stat.S_IXOTH)


class CodeqlAlertGateRateLimitTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="codeql-gate-test-")
        self.addCleanup(self.tmp.cleanup)
        self.stub_dir = os.path.join(self.tmp.name, "stubs")
        os.makedirs(self.stub_dir)
        self.api_stub = os.path.join(self.stub_dir, "api-stub")
        self.sleep_stub = os.path.join(self.stub_dir, "sleep-stub")
        write_stub(self.api_stub, API_STUB)
        write_stub(self.sleep_stub, SLEEP_STUB)
        self.count_file = os.path.join(self.tmp.name, "api-calls.count")
        with open(self.count_file, "w") as fh:
            fh.write("0\n")
        self.sleep_log = os.path.join(self.tmp.name, "sleeps.log")

    def base_env(self, **overrides):
        env = dict(os.environ)
        env.pop("TMUX", None)
        env.pop("TMUX_TMPDIR", None)
        env.update({
            "REPO": "testorg/testrepo",
            "REF": "refs/heads/main",
            "SHA": SHA,
            "EXPECTED_CATEGORIES": EXPECTED_CATEGORIES,
            "CODEQL_GATE_API_COMMAND": self.api_stub,
            "CODEQL_GATE_SLEEP_COMMAND": self.sleep_stub,
            "STUB_COUNT_FILE": self.count_file,
            "STUB_SLEEP_LOG": self.sleep_log,
            # Defaults: the rate_limit lookup fails (interval fallback),
            # the analyses endpoint succeeds on the first call, and the
            # stub failure text is the GitHub installation rate-limit 403.
            "STUB_RATE_LIMIT_FAIL": "1",
            "STUB_RATE_LIMIT_RESET_OFFSET": "0",
            "STUB_ANALYSES_FAIL_COUNT": "0",
            "STUB_ALERTS_FAIL_COUNT": "0",
        })
        # The default analyses rows file is only created when the test
        # does not override it (it names a different content set).
        if "STUB_OK_ANALYSES_FILE" not in overrides:
            env["STUB_OK_ANALYSES_FILE"] = self.ok_analyses_file()
        env.update(overrides)
        return env

    def ok_analyses_file(self, categories=EXPECTED_CATEGORIES):
        path = os.path.join(self.tmp.name, "analyses-ok.txt")
        with open(path, "w") as fh:
            fh.write("\n".join(categories.split()) + "\n")
        return path

    def run_gate(self, subcommand, **overrides):
        env = self.base_env(**overrides)
        return subprocess.run(
            ["bash", GATE, subcommand],
            env=env,
            capture_output=True,
            text=True,
            timeout=120,
        )

    def recorded_sleeps(self):
        if not os.path.exists(self.sleep_log):
            return []
        with open(self.sleep_log) as fh:
            return [int(line) for line in fh if line.strip()]

    # 1. Interval fallback: three rate-limit responses, then success.
    def test_wait_analyses_retries_past_rate_limit_then_succeeds(self):
        proc = self.run_gate(
            "wait-analyses",
            STUB_ANALYSES_FAIL_COUNT="3",
            STUB_RATE_LIMIT_FAIL="1",  # no reset: interval fallback path
        )
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertEqual(self.recorded_sleeps(), [60, 120, 240])
        self.assertNotIn("No analysis for", proc.stdout + proc.stderr)
        self.assertIn("All default setup analyses are present", proc.stdout)

    # 2. A reported core reset epoch is honoured.
    def test_reset_epoch_honoured(self):
        proc = self.run_gate(
            "wait-analyses",
            STUB_ANALYSES_FAIL_COUNT="1",
            STUB_RATE_LIMIT_FAIL="0",
            STUB_RATE_LIMIT_RESET_OFFSET="90",
        )
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        sleeps = self.recorded_sleeps()
        self.assertTrue(sleeps, "no sleeps were recorded")
        self.assertAlmostEqual(
            sleeps[0], 90, delta=5,
            msg="first recorded sleep should be about the 90s reset: {!r}".format(sleeps))

    # 3. A category that never appears: budget caps the waits.
    def test_missing_category_bounded_by_budget(self):
        proc = self.run_gate(
            "wait-analyses",
            WAIT_MAX_SECONDS="700",
            STUB_ANALYSES_FAIL_COUNT="0",
            STUB_OK_ANALYSES_FILE=self.ok_analyses_file("/language:go"),
        )
        self.assertEqual(proc.returncode, 1, proc.stdout + proc.stderr)
        self.assertIn("No analysis for", proc.stdout + proc.stderr)
        sleeps = self.recorded_sleeps()
        self.assertTrue(sleeps, "no sleeps were recorded")
        for s in sleeps:
            self.assertLessEqual(s, 300, "single sleep exceeds cap: {!r}".format(sleeps))
        self.assertLessEqual(sum(sleeps), 700,
                             "total slept {!r} exceeds WAIT_MAX_SECONDS=700".format(sleeps))

    # 4. Rate limited with no reset reported until the total budget runs out.
    def test_wait_analyses_rate_limit_without_reset_waits_out_budget(self):
        proc = self.run_gate(
            "wait-analyses",
            STUB_RATE_LIMIT_FAIL="1",  # no reset is reported: interval fallback
            WAIT_MAX_SECONDS="500",
            STUB_ANALYSES_FAIL_COUNT=str(10 ** 9),
        )
        self.assertEqual(proc.returncode, 1, proc.stdout + proc.stderr)
        out = proc.stdout + proc.stderr
        self.assertIn("Rate limited", out)
        self.assertNotIn("No analysis for", out)
        self.assertEqual(self.recorded_sleeps(), [60, 120, 240, 80])

    # 5. A reset far beyond the budget stops at once, without sleeping.
    def test_wait_analyses_reset_beyond_budget_stops_at_once(self):
        proc = self.run_gate(
            "wait-analyses",
            WAIT_MAX_SECONDS="500",
            STUB_ANALYSES_FAIL_COUNT=str(10 ** 9),
            STUB_RATE_LIMIT_FAIL="0",
            STUB_RATE_LIMIT_RESET_OFFSET=str(10 ** 6),
        )
        out = proc.stdout + proc.stderr
        self.assertEqual(proc.returncode, 1, out)
        self.assertIn("Rate limited", out)
        self.assertIn("not a code problem", out)
        self.assertNotIn("No analysis for", out)
        self.assertEqual(self.recorded_sleeps(), [],
                         "sleep must not be called when the reset is beyond the budget")
        with open(self.count_file) as fh:
            self.assertEqual(int(fh.read().strip()), 1,
                             "the analyses endpoint should be called exactly once")

    # 6. A reset inside the budget sleeps once, then the retry succeeds.
    def test_wait_analyses_reset_inside_budget_sleeps_once_then_succeeds(self):
        proc = self.run_gate(
            "wait-analyses",
            WAIT_MAX_SECONDS="500",
            STUB_ANALYSES_FAIL_COUNT="1",
            STUB_RATE_LIMIT_FAIL="0",
            STUB_RATE_LIMIT_RESET_OFFSET="90",
        )
        out = proc.stdout + proc.stderr
        self.assertEqual(proc.returncode, 0, out)
        sleeps = self.recorded_sleeps()
        self.assertEqual(len(sleeps), 1,
                         "expected exactly one sleep: {!r}".format(sleeps))
        self.assertAlmostEqual(
            sleeps[0], 90, delta=5,
            msg="the sleep should be about the 90s reset: {!r}".format(sleeps))
        self.assertIn("All default setup analyses are present", proc.stdout)
        with open(self.count_file) as fh:
            self.assertEqual(int(fh.read().strip()), 2,
                             "the analyses endpoint should be called twice")

    # 7. open-alerts: rate-limit retries, then a clean no-alerts result.
    def test_open_alerts_retries_then_no_rows(self):
        proc = self.run_gate(
            "open-alerts",
            STUB_ALERTS_FAIL_COUNT="3",
            STUB_RATE_LIMIT_FAIL="1",  # no reset: interval fallback path
        )
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn(
            "No open high or critical code-scanning alerts", proc.stdout)

    # Open-alerts reset beyond the budget: stop at once, no sleep.
    def test_open_alerts_reset_beyond_budget_stops_at_once(self):
        proc = self.run_gate(
            "open-alerts",
            WAIT_MAX_SECONDS="500",
            STUB_ALERTS_FAIL_COUNT=str(10 ** 9),
            STUB_RATE_LIMIT_FAIL="0",
            STUB_RATE_LIMIT_RESET_OFFSET=str(10 ** 6),
        )
        out = proc.stdout + proc.stderr
        self.assertEqual(proc.returncode, 1, out)
        self.assertIn("Rate limited", out)
        self.assertIn("not a code problem", out)
        self.assertNotIn("No open high or critical code-scanning alerts", out)
        self.assertEqual(self.recorded_sleeps(), [],
                         "sleep must not be called when the reset is beyond the budget")
        with open(self.count_file) as fh:
            self.assertEqual(int(fh.read().strip()), 1,
                             "the alerts endpoint should be called exactly once")

    # Open-alerts reset inside the budget: one sleep, then the read succeeds.
    def test_open_alerts_reset_inside_budget_sleeps_once_then_reads(self):
        proc = self.run_gate(
            "open-alerts",
            WAIT_MAX_SECONDS="500",
            STUB_ALERTS_FAIL_COUNT="1",
            STUB_RATE_LIMIT_FAIL="0",
            STUB_RATE_LIMIT_RESET_OFFSET="90",
        )
        out = proc.stdout + proc.stderr
        self.assertEqual(proc.returncode, 0, out)
        sleeps = self.recorded_sleeps()
        self.assertEqual(len(sleeps), 1,
                         "expected exactly one sleep: {!r}".format(sleeps))
        self.assertAlmostEqual(
            sleeps[0], 90, delta=5,
            msg="the sleep should be about the 90s reset: {!r}".format(sleeps))
        self.assertIn("No open high or critical code-scanning alerts", proc.stdout)
        with open(self.count_file) as fh:
            self.assertEqual(int(fh.read().strip()), 2,
                             "the alerts endpoint should be called twice")

    # 8. open-alerts: one high alert fails with its count.
    def test_open_alerts_one_high_row(self):
        rows = os.path.join(self.tmp.name, "alerts-ok.txt")
        with open(rows, "w") as fh:
            fh.write("high\t1234\tjs/useless-assignment\t/app/src/lib.js\n")
        proc = self.run_gate(
            "open-alerts",
            STUB_ALERTS_FAIL_COUNT="0",
            STUB_OK_ALERTS_FILE=rows,
        )
        self.assertEqual(proc.returncode, 1, proc.stdout + proc.stderr)
        self.assertIn("0 critical and 1 high", proc.stdout)
        self.assertIn("high\t1234\tjs/useless-assignment\t/app/src/lib.js", proc.stdout)

    # 9. open-alerts: a read failure is never reported as zero alerts.
    def test_open_alerts_read_failure_never_zero(self):
        proc = self.run_gate(
            "open-alerts",
            STUB_ALERTS_FAIL_COUNT=str(10 ** 9),
            STUB_FAIL_STDERR="Resource not accessible by integration (HTTP 403)",
        )
        self.assertEqual(proc.returncode, 1, proc.stdout + proc.stderr)
        self.assertNotIn("No open high or critical code-scanning alerts",
                         proc.stdout + proc.stderr)
        self.assertIn("Failed to read open code-scanning alerts",
                      proc.stdout + proc.stderr)
        self.assertIn("Resource not accessible by integration (HTTP 403)",
                      proc.stdout + proc.stderr)

    # 10. Unknown subcommand is a usage error.
    def test_unknown_subcommand_exits_2(self):
        proc = self.run_gate("frobnicate")
        self.assertEqual(proc.returncode, 2, proc.stdout + proc.stderr)
        self.assertIn("unknown subcommand", proc.stderr)


# 9. The CI workflow must check out the repository before it invokes the
# gate script: without it, the step runs `bash scripts/codeql-alert-gate.sh`
# on an empty runner workspace and fails on every run. This test was added
# after exactly that defect shipped once (the job invoked the script with no
# actions/checkout step in the file). The tests above always call the script
# from a working checkout, so none of them can catch it.
class WorkflowInvocationTest(unittest.TestCase):
    def test_workflow_checks_out_before_invoking_gate_script(self):
        path = os.path.join(
            REPO_ROOT, ".github", "workflows", "codeql.yaml")
        with open(path) as fh:
            lines = fh.readlines()
        checkout = [i for i, l in enumerate(lines)
                    if "uses:" in l and "actions/checkout" in l]
        invoke = [i for i, l in enumerate(lines)
                  if "bash scripts/codeql-alert-gate.sh" in l]
        self.assertTrue(invoke,
                        "workflow no longer invokes the gate script")
        self.assertTrue(checkout, "workflow invokes the script but has no "
                                  "actions/checkout step")
        self.assertLess(min(checkout), min(invoke),
                        "actions/checkout must precede the first invocation "
                        "of scripts/codeql-alert-gate.sh")


if __name__ == "__main__":
    unittest.main(verbosity=2)
