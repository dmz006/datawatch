#!/usr/bin/env python3
"""Reference automation-kind plugin entry point.

Reads one JSON Request line from stdin, writes one JSON Response line
to stdout. See examples/plugins/hello-automation/manifest.yaml.
"""
import json
import sys


def main() -> int:
    line = sys.stdin.readline()
    try:
        req = json.loads(line) if line.strip() else {}
    except json.JSONDecodeError:
        req = {}

    status = req.get("status", "unknown")
    resp = {"ok": True, "fields": {"greeting": f"hello-automation saw status={status}"}}
    print(json.dumps(resp))
    return 0


if __name__ == "__main__":
    sys.exit(main())
