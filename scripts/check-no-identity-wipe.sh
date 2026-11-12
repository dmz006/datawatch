#!/usr/bin/env bash
# scripts/check-no-identity-wipe.sh — blocks shell scripts from writing an empty
# identity to the identity route. A script that clears state "so the next run
# starts clean" with an empty body wipes whatever the target daemon held, and
# against a live daemon that is the operator's real identity.
#
# A violation is a curl PUT, PATCH or POST (an explicit -X/--request method, or
# a data flag with no method, which curl sends as POST) to a URL containing
# /api/identity whose data is:
#
#   - `{}`
#   - an empty string
#   - a JSON object whose every value is empty ("", null, [] or {}, nested)
#
# POST /api/identity/reset is the explicit, backed-up clear and is allowed.
# A body that is a shell variable, a file (@file) or any non-empty JSON is not
# a violation. Clearing on purpose goes through the reset route.
#
# Shell scripts are scanned under scripts/, tests/, testdata/ and
# .datawatch/verify/ (*.sh, *.bash and any file with a sh/bash shebang).
# Backslash line continuations are joined before matching. Comment-only lines
# are skipped. A line whose matching text is fixture data, or an assertion that
# the daemon refuses an empty write, may carry an `identity-wipe-ok` marker
# comment and is skipped; on a continued command the marker goes on any of its
# physical lines. CHECK_ROOT overrides the scanned root (tests point it at a
# fixture tree). The checker only reads files; it never calls a daemon.
# Run independently: bash scripts/check-no-identity-wipe.sh

set -euo pipefail

ROOT="${CHECK_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"
cd "$ROOT"

ERRORS=0
report() {
    echo "  ✗ $1"
    ERRORS=$((ERRORS + 1))
}

echo "==> Identity wipe audit (empty write to /api/identity)"

SELF="check-no-identity-wipe.sh"

targets=()
for d in scripts tests testdata .datawatch/verify; do
    [[ -d "$d" ]] && targets+=("$d")
done

if ((${#targets[@]} > 0)); then
    while IFS= read -r hit; do
        [[ -z "$hit" ]] && continue
        report "$hit"
    done < <(
        find "${targets[@]}" -type f -not -name "$SELF" -print 2>/dev/null | sort |
            python3 -I -c '
import json, re, sys

URL = re.compile(r"/api/identity[^\s\"\x27?#]*")
METHOD = re.compile(r"(?:-X|--request)[\s=]*[\"\x27]?([A-Za-z]+)")
DATA = re.compile(
    r"(?:^|\s)(?:-d|--data|--data-raw|--data-binary|--data-ascii)(?:\s+|=)"
    r"(\x27[^\x27]*\x27|\"(?:\\.|[^\"\\])*\"|[^\s]+)"
)


def is_shell(path):
    if path.endswith((".sh", ".bash")):
        return True
    try:
        with open(path, "rb") as f:
            first = f.readline(200)
    except OSError:
        return False
    return first.startswith(b"#!") and (b"sh" in first)


def empty(v):
    if v is None or v == "":
        return True
    if isinstance(v, (list, tuple)):
        return all(empty(x) for x in v)
    if isinstance(v, dict):
        return all(empty(x) for x in v.values())
    return False


def wipe_body(raw):
    """True when the data argument is an empty identity body."""
    if len(raw) >= 2 and raw[0] == raw[-1] and raw[0] in "\x27\"":
        quote, raw = raw[0], raw[1:-1]
        if quote == "\"":
            raw = raw.replace("\\\"", "\"").replace("\\\\", "\\")
    if raw.strip() == "":
        return True
    if raw.startswith(("@", "$")) or "$" in raw or "`" in raw:
        return False
    try:
        obj = json.loads(raw)
    except ValueError:
        return False
    return isinstance(obj, dict) and empty(obj)


def logical_lines(path):
    with open(path, encoding="utf-8", errors="replace") as f:
        pending, start = "", 0
        for n, line in enumerate(f, 1):
            line = line.rstrip("\n")
            if not pending:
                start = n
            if line.endswith("\\"):
                pending += line[:-1] + " "
                continue
            yield start, pending + line
            pending = ""
        if pending:
            yield start, pending


for path in (p.strip() for p in sys.stdin if p.strip()):
    if not is_shell(path):
        continue
    for num, line in logical_lines(path):
        if line.lstrip().startswith("#") or "identity-wipe-ok" in line:
            continue
        urls = URL.findall(line)
        if not urls or all(u.rstrip("/\"\x27") == "/api/identity/reset" for u in urls):
            continue
        datas = DATA.findall(line)
        if not datas:
            continue
        m = METHOD.search(line)
        method = m.group(1).upper() if m else "POST"
        if method not in ("PUT", "PATCH", "POST"):
            continue
        if any(wipe_body(d) for d in datas):
            print("%s:%d: %s" % (path, num, line.strip()))
'
    )
fi

if ((ERRORS > 0)); then
    echo
    echo "  hint: a script must never send an empty body to /api/identity; it"
    echo "  replaces the saved identity. To clear it on purpose use"
    echo "  POST /api/identity/reset, and to leave a daemon as you found it,"
    echo "  read the identity first and put the saved copy back. If the text is"
    echo "  fixture data or asserts the daemon refuses an empty write, add an"
    echo "  'identity-wipe-ok' marker comment."
    echo "FAIL: $ERRORS identity-wipe hit(s)"
    exit 1
fi
echo "OK: no empty writes to /api/identity"
