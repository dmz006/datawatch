#!/usr/bin/env python3
"""Classify dependency updates by kind.

Reads a JSON list on stdin of objects:
  {name, ecosystem (gomod | docker | github-actions), old, new, security (bool, optional)}
and prints the same list on stdout with a "kind" field added:
  security | major | minor | patch | unknown

Exits non-zero with a message on stderr when the input is malformed.
Standard library only.
"""
import json
import re
import sys

ECOSYSTEMS = ("gomod", "docker", "github-actions")

_GO_VER = re.compile(r"^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?(?:-[0-9A-Za-z.\-]+)?(?:\+[0-9A-Za-z.\-]+)?$")
_NUM_PREFIX = re.compile(r"^v?(\d+(?:\.\d+)*)")
_PATH_MAJOR = re.compile(r"/v(\d+)$")


class InputError(Exception):
    pass


def _bucket(a, b, v0_minor_is_major=False):
    """a, b are (major, minor, patch) tuples."""
    if a[0] != b[0]:
        return "major"
    if a[1] != b[1]:
        return "major" if (v0_minor_is_major and a[0] == 0) else "minor"
    return "patch"


def _split_path(value):
    """Split an optional 'module/path@version' into (path, version)."""
    if "@" in value:
        path, _, ver = value.partition("@")
        return path, ver
    return None, value


def _go_tuple(ver):
    m = _GO_VER.match(ver.strip())
    if not m:
        return None
    return tuple(int(g) if g is not None else 0 for g in m.groups())


def classify_gomod(name, old, new):
    old_path, old_ver = _split_path(old)
    new_path, new_ver = _split_path(new)
    a, b = _go_tuple(old_ver), _go_tuple(new_ver)
    if a is None or b is None:
        return "unknown"
    # +incompatible is stripped by the regex; pre-release is ignored.
    old_path = old_path or name
    new_path = new_path or name
    pm_old = _PATH_MAJOR.search(old_path)
    pm_new = _PATH_MAJOR.search(new_path)
    if (pm_old.group(1) if pm_old else None) != (pm_new.group(1) if pm_new else None):
        return "major"
    return _bucket(a, b, v0_minor_is_major=True)


def _numeric_prefix(tag):
    tag = tag.split("@", 1)[0].strip()
    m = _NUM_PREFIX.match(tag)
    if not m:
        return None
    parts = [int(p) for p in m.group(1).split(".")]
    while len(parts) < 3:
        parts.append(0)
    return tuple(parts[:3])


def classify_docker(name, old, new):
    a, b = _numeric_prefix(old), _numeric_prefix(new)
    if a is None and b is None:
        return "patch"  # digest-only / non-numeric on both sides: prefix unchanged
    if a is None or b is None:
        return "unknown"
    return _bucket(a, b)


def _action_version(value):
    if "#" in value:
        value = value.split("#", 1)[1]
    value = value.strip()
    m = _NUM_PREFIX.match(value)
    if not m:
        return None  # e.g. a bare commit hash with no version comment
    parts = [int(p) for p in m.group(1).split(".")]
    while len(parts) < 3:
        parts.append(0)
    return tuple(parts[:3])


def classify_action(name, old, new):
    a, b = _action_version(old), _action_version(new)
    if a is None or b is None:
        return "unknown"
    return _bucket(a, b)


def classify(item):
    if item.get("security") is True:
        return "security"
    eco = item["ecosystem"]
    if eco == "gomod":
        return classify_gomod(item["name"], item["old"], item["new"])
    if eco == "docker":
        return classify_docker(item["name"], item["old"], item["new"])
    return classify_action(item["name"], item["old"], item["new"])


def validate(data):
    if not isinstance(data, list):
        raise InputError("input must be a JSON list")
    for i, item in enumerate(data):
        if not isinstance(item, dict):
            raise InputError("item %d is not an object" % i)
        for key in ("name", "old", "new"):
            if not isinstance(item.get(key), str) or not item[key].strip():
                raise InputError("item %d: %r must be a non-empty string" % (i, key))
        if item.get("ecosystem") not in ECOSYSTEMS:
            raise InputError("item %d: ecosystem must be one of %s" % (i, ", ".join(ECOSYSTEMS)))
        if "security" in item and not isinstance(item["security"], bool):
            raise InputError("item %d: security must be a boolean" % i)


def process(data):
    validate(data)
    out = []
    for item in data:
        rec = dict(item)
        rec["kind"] = classify(item)
        out.append(rec)
    return out


def main():
    try:
        data = json.load(sys.stdin)
        result = process(data)
    except (json.JSONDecodeError, InputError) as exc:
        sys.stderr.write("dependency-update-classify: malformed input: %s\n" % exc)
        return 1
    json.dump(result, sys.stdout, indent=2)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
