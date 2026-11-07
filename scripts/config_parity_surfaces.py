"""Config-surface key readers for the config-parity checks.

Reads the four config-surface sources as text (stdlib only, no Go toolchain,
no imports beyond the stdlib, safe under ``python3 -I``) and returns the sets
of dotted config keys each surface handles.

Surfaces
--------
1. Web Settings UI  — the ``*_CONFIG_FIELDS`` arrays in
   ``internal/server/web/app.js``; the keys are the ``key:`` values inside
   each section's ``fields: [ ... ]`` array (underscore-prefixed keys are
   UI-local and are skipped).
2. GET /api/config  — ``handleGetConfig`` in ``internal/server/api.go``.
   The keys of the emitted object literal, nested object values flattened to
   dotted paths, and helper ``*Surface`` function values expanded (their
   returned map literal is read from the defining ``.go`` file; an unknown
   or non-map-returning helper leaves the key as a leaf).
   Example: ``"webhook": webhookNotifySurface(...)`` →
   ``autonomous.notify.webhook.enabled`` etc. from that map.
3. PUT /api/config  — ``applyConfigPatch`` (called by ``handlePutConfig``)
   in ``internal/server/api.go``. The keys are every ``case "a.b", "c.d":``
   label of the key switch, plus the dynamic key sets handled before the
   switch, each reported as ``prefix.*``:
     - ``tracing.backends.*``                     (strings.HasPrefix, api.go)
     - ``autonomous.supervisor_loop.*``           (supervisorLoopPatchKey,
                                                  supervisor_loop.go)
     - ``supervisor_loop.*``                      (same function's plan-key
                                                  form)
     - ``autonomous.requirements.min_ollama.*``   (CutPrefix, api.go)
4. comm-router ``configure`` — ``handleConfigure`` in
   ``internal/router/router.go``. It is generic: it parses
   ``<key>=<value>`` and forwards it to the router's configure function,
   which ``cmd/datawatch/main.go`` wires as a PUT to the daemon's
   ``/api/config`` — the same apply-path as surface 3 — so this reader
   returns the PUT key set.
5. CLI ``config set <key> <value>`` — ``newConfigSetCmd``/``runConfigSet``
   in ``cmd/datawatch/main.go``. It is also generic: it sends
   ``PUT /api/config`` to the running daemon, where the same
   ``applyConfigPatch`` validates + persists, so this reader returns the
   PUT key set too.

``covers(keyset, key)`` is true when ``key`` is in ``keyset`` exactly or is
under any ``prefix.*`` entry of the set (so ``"a.b.*"`` covers
``"a.b.anything"``).
"""

import os
import re

# ---------------------------------------------------------------------------
# Generic text helpers
# ---------------------------------------------------------------------------


def covers(keyset, key):
    """True if *key* is in *keyset* exactly or under any ``prefix.*`` entry."""
    if key in keyset:
        return True
    parts = key.split(".")
    for depth in range(1, len(parts)):
        if ".".join(parts[:depth]) + ".*" in keyset:
            return True
    return False


def _read(path):
    with open(path, "r", encoding="utf-8", errors="replace") as f:
        return f.read()


def _skip_string(src, i):
    """Index just past the string literal opening at src[i]."""
    quote = src[i]
    i += 1
    n = len(src)
    while i < n:
        c = src[i]
        if quote != "`" and c == "\\":
            i += 2
            continue
        if c == quote:
            return i + 1
        i += 1
    return n


def _strip_comments(src):
    """Blank out line and block comments, preserving strings and newlines."""
    out = list(src)
    i, n = 0, len(src)
    while i < n:
        c = src[i]
        if c in ("'", '"', "`"):
            i = _skip_string(src, i)
        elif c == "/" and i + 1 < n and src[i + 1] == "/":
            j = src.find("\n", i)
            if j == -1:
                j = n
            for k in range(i, j):
                out[k] = " "
            i = j
        elif c == "/" and i + 1 < n and src[i + 1] == "*":
            j = src.find("*/", i + 2)
            end = n if j == -1 else j + 2
            for k in range(i, end):
                out[k] = "\n" if src[k] == "\n" else " "
            i = end
        else:
            i += 1
    return "".join(out)


def _balanced(src, start, open_ch, close_ch):
    """Index of the close_ch matching the open_ch at src[start] (-1 = none).

    Tracks nested brackets and all three string kinds.
    """
    depth = 0
    i = start
    n = len(src)
    while i < n:
        c = src[i]
        if c in ("'", '"', "`"):
            i = _skip_string(src, i)
            continue
        if c == open_ch:
            depth += 1
        elif c == close_ch:
            depth -= 1
            if depth == 0:
                return i
        i += 1
    return -1


def _bal_paren(src, open_idx):
    """Index of the ')' matching the '(' at src[open_idx] (-1 on mismatch)."""
    depth = 0
    i = open_idx
    n = len(src)
    while i < n:
        c = src[i]
        if c in ("'", '"', "`"):
            i = _skip_string(src, i)
            continue
        if c == "(":
            depth += 1
        elif c == ")":
            depth -= 1
            if depth == 0:
                return i
        i += 1
    return -1


def _find_function(src, name):
    """Return (start, end) of ``func [recv] name(...) { ... }`` (or None)."""
    for m in re.finditer(r"func\s*(\([^)]*\)\s*)?" + re.escape(name) + r"\s*\(", src):
        close_paren = _bal_paren(src, m.end() - 1)
        if close_paren == -1:
            continue
        b = src.find("{", close_paren)
        if b == -1 or b - close_paren > 300:
            continue
        e = _balanced(src, b, "{", "}")
        if e == -1:
            continue
        return m.start(), e
    return None


def _map_open_brace(src, i):
    """Index of the opening brace of the map/object literal at src[i:], or -1.

    Handles typed literals (``map[string]interface{}{...}``, skipping the
    empty ``interface{}`` type brace) and bare object bodies (``{...}``).
    """
    head = src[i : i + 60]
    if not re.match(r"\s*(map\s*\[|\{)", head):
        return -1
    n = len(src)
    j = i
    if re.match(r"\s*map\s*\[", head):
        depth = 0
        while j < n:
            c = src[j]
            if c in ("'", '"', "`"):
                j = _skip_string(src, j)
                continue
            if c == "[":
                depth += 1
            elif c == "]":
                depth -= 1
                if depth == 0:
                    j += 1
                    break
            j += 1
        else:
            return -1
    # skip whitespace / type words / empty interface{} to the literal brace
    while j < n:
        c = src[j]
        if c in ("'", '"', "`"):
            j = _skip_string(src, j)
            continue
        if c in " \t\r\n" or c.isalpha() or c in ".*,":
            j += 1
            continue
        if c == "{":
            if j + 1 < n and src[j + 1] == "}":
                j += 2  # empty interface{} type brace — not the literal
                continue
            return j
        return -1
    return -1


def _map_body_at(src, i):
    """Body text (between braces) of the map/object literal at src[i:] (or None)."""
    b = _map_open_brace(src, i)
    if b == -1:
        return None
    e = _balanced(src, b, "{", "}")
    if e == -1:
        return None
    return src[b + 1 : e]


def _first_map_body(region):
    """First well-formed map literal body inside *region* (skipping interface{})."""
    i = 0
    n = len(region)
    while i < n:
        if re.match(r"map\s*\[", region[i:]):
            body = _map_body_at(region, i)
            if body is not None and body.strip():
                return body
            k = region.find("]", i)
            i = k + 1 if k != -1 else n
            continue
        i += 1
    return None


def _map_entries(body, consts=None):
    """Split a map/object body into [(key, value_text)] at the top level."""
    entries = []
    i, n = 0, len(body)
    while i < n:
        while i < n and body[i] in " \t\r\n,":
            i += 1
        if i >= n:
            break
        m = re.match(r'"((?:[^"\\]|\\.)*)"', body[i:])
        if m:
            key = m.group(1)
            i += m.end()
        else:
            m = re.match(r"[A-Za-z_][A-Za-z0-9_]*", body[i:])
            if not m:
                i += 1
                continue
            key = m.group(0)
            if consts and key in consts:
                key = consts[key]  # named const key, e.g. mergePipelineFieldConflictModel
            i += m.end()
        while i < n and body[i] in " \t\r\n":
            i += 1
        if i >= n or body[i] != ":":
            i += 1
            continue
        i += 1
        # value: run to top-level comma or end, tracking depth and strings
        depth = 0
        start = i
        while i < n:
            c = body[i]
            if c in ("'", '"', "`"):
                i = _skip_string(body, i)
                continue
            if c in "{[(":
                depth += 1
            elif c in "}])":
                depth -= 1
            elif c == "," and depth == 0:
                break
            i += 1
        entries.append((key, body[start:i]))
    return entries


_HELPER_CALL = re.compile(r"^([A-Za-z_][A-Za-z0-9_.]*)\s*\(")
_TRAIL_SELECT = re.compile(r'\[\s*"([^"]+)"\s*\]$')

def _const_expr_value(expr, consts):
    """Value of a const expression of identifiers and string literals, or None."""
    expr = expr.strip()
    parts = []
    i, n = 0, len(expr)
    while i < n:
        c = expr[i]
        if c in " \t+":
            i += 1
            continue
        if c in '\"`':
            j = i + 1
            while j < n:
                if expr[j] == "\\" and c == '"':
                    j += 2
                    continue
                if expr[j] == c:
                    break
                j += 1
            if j >= n:
                return None
            parts.append(expr[i + 1 : j])
            i = j + 1
        elif c.isalpha() or c == "_":
            m = re.match(r"[A-Za-z_][A-Za-z0-9_]*", expr[i:])
            name = m.group(0)
            if name not in consts:
                return None
            parts.append(consts[name])
            i += m.end()
        else:
            return None
    return "".join(parts) if parts else None


_CONST_LINE = re.compile(r"^\s*([A-Za-z_][A-Za-z0-9_]*)\s*(?:=\s*)?([^\n]+?)\s*$")


def _build_consts(*sources):
    """Map Go const names to string values across *sources* (string consts only).

    Handles standalone ``const N = "..."`` lines, const blocks
    (``NAME = "..."`` / untyped ``NAME "..."``), and concatenations of
    consts with string literals.
    """
    consts = {}
    in_block = False
    for src in sources:
        for line in src.splitlines():
            s = line.strip()
            if in_block:
                if s == ")":
                    in_block = False
                m = _CONST_LINE.match(line)
                if m:
                    val = _const_expr_value(m.group(2), consts)
                    if val is not None:
                        consts[m.group(1)] = val
                continue
            if s.startswith("const ("):
                in_block = True
                continue
            m = re.match(r"^const\s+([A-Za-z_][A-Za-z0-9_]*)\s*=([^\n]+)$", s)
            if m:
                val = _const_expr_value(m.group(2), consts)
                if val is not None:
                    consts[m.group(1)] = val
    return consts



def _closure_map(v):
    """(map_body, None) for a ``func() ... { ... }`` value, or None."""
    # body brace: first '{' that is not an empty ``interface{}`` type brace
    i = -1
    n = len(v)
    while True:
        i = v.find("{", i + 1)
        if i == -1:
            return None
        if i + 1 < n and v[i + 1] != "}":
            b = i
            break
    e = _balanced(v, b, "{", "}")
    if e == -1:
        return None
    inner = v[b + 1 : e]
    rm = re.search(r"(?:^|[;\s])return\s*", inner)
    cands = [inner[rm.end() :]] if rm else []
    cands.append(inner)
    for cand in cands:
        body = _map_body_at(cand, 0) if re.match(r"\s*(\{)", cand) else None
        if body is None:
            body = _first_map_body(cand)
        if body and body.strip():
            return body, None
    return None


def _expand_value(value, search_sources, seen):
    """Return (map_body, subkey_or_None) if *value* denotes a map, else None.

    Handles: direct map literals, ``func() {...}`` closures containing a map,
    and single-call helper values (optionally followed by a ``["subkey"]``
    selection, as in ``checksShipConfigDisplay(cfg)["checks"]``).
    """
    v = value.strip()
    if not v:
        return None
    m = _TRAIL_SELECT.search(v)
    subkey = m.group(1) if m else None
    core = v[: m.start()] if m else v
    # direct map/object literal
    if re.match(r"\s*(map\s*\[|\{)", core):
        body = _map_body_at(core, 0)
        if body is not None:
            return body, None
        return None
    if v.startswith("func"):
        return _closure_map(v)
    m = _HELPER_CALL.match(core)
    if m and core.endswith(")"):
        name = m.group(1)
        base = re.sub(r"\.[A-Za-z0-9_]+$", "", name)
        for src in search_sources:
            loc = _find_function(src, name) or _find_function(src, base)
            if not loc:
                continue
            if name in seen:
                break  # avoid helper recursion loops
            body = _first_map_body(src[loc[0] : loc[1]])
            if body is not None:
                return body, subkey
        return None
    return None


def _flatten(value, prefix, out, search_sources, seen, consts, depth=0):
    if depth > 10:
        out.add(prefix.rstrip("."))
        return
    exp = _expand_value(value, search_sources, seen)
    if exp is None:
        out.add(prefix.rstrip("."))
        return
    body, subkey = exp
    entries = _map_entries(body, consts)
    if subkey is not None:
        for k, val in entries:
            if k == subkey:
                _flatten(val, prefix + ".", out, search_sources, seen, consts, depth + 1)
                return
        out.add(prefix.rstrip("."))
        return
    if not entries:
        out.add(prefix.rstrip("."))
        return
    for k, val in entries:
        _flatten(val, prefix + k + ".", out, search_sources, seen, consts, depth + 1)


# ---------------------------------------------------------------------------
# 1. Web Settings UI
# ---------------------------------------------------------------------------

_JS_KEY = re.compile(r"(?:^|[^A-Za-z0-9_.])key\s*:\s*(['\"])((?:\\.|(?!\1).)*)\1")


def web_settings_keys(repo_root):
    """Dotted config keys shown by the Web Settings UI.

    Source: ``internal/server/web/app.js`` — every ``*_CONFIG_FIELDS`` array
    (COMMS_CONFIG_FIELDS, GENERAL_CONFIG_FIELDS, LLM_CONFIG_FIELDS and any
    sibling). For each section object, the keys are the ``key:`` entries
    inside its ``fields: [ ... ]`` array; keys starting with ``_`` are
    UI-local (e.g. ``_cert_source``) and are not config keys.
    """
    src = _read(os.path.join(repo_root, "internal", "server", "web", "app.js"))
    src = _strip_comments(src)
    keys = set()
    for m in re.finditer(r"\b[A-Z][A-Z0-9_]*_CONFIG_FIELDS\s*=\s*\[", src):
        open_br = m.end() - 1
        close_br = _balanced(src, open_br, "[", "]")
        if close_br == -1:
            continue
        array_body = src[open_br + 1 : close_br]
        for fm in re.finditer(r"\bfields\s*:\s*\[", array_body):
            f_open = fm.end() - 1
            f_close = _balanced(array_body, f_open, "[", "]")
            if f_close == -1:
                continue
            fields_body = array_body[f_open + 1 : f_close]
            for km in _JS_KEY.finditer(fields_body):
                key = km.group(2)
                if key.startswith("_"):
                    continue
                keys.add(key)
    return keys


# ---------------------------------------------------------------------------
# shared Go-side search sources (helper function resolution)
# ---------------------------------------------------------------------------


def _server_go_sources(repo_root):
    d = os.path.join(repo_root, "internal", "server")
    out = []
    if os.path.isdir(d):
        for name in sorted(os.listdir(d)):
            if name.endswith(".go") and not name.endswith("_test.go"):
                try:
                    out.append(_strip_comments(_read(os.path.join(d, name))))
                except OSError:
                    pass
    return out


# ---------------------------------------------------------------------------
# 2. GET /api/config
# ---------------------------------------------------------------------------


def api_get_keys(repo_root):
    """Dotted keys returned by GET /api/config (``handleGetConfig``, api.go).

    Reads the emitted object literal in ``handleGetConfig``; nested object
    values are flattened to dotted paths and helper ``*Surface`` function
    values are expanded from their ``.go`` definitions (e.g.
    ``repoCIWatchSurface`` → the ``autonomous.automation.repo_ci_watch``
    keys). A value whose map cannot be read statically stays as its own key
    (e.g. struct-shaped helpers such as ``requestWatchConfigSurface``).
    """
    api_path = os.path.join(repo_root, "internal", "server", "api.go")
    src = _strip_comments(_read(api_path))
    loc = _find_function(src, "handleGetConfig")
    if loc is None:
        return set()
    body = _first_map_body(src[loc[0] : loc[1]])
    if body is None:
        return set()
    server_sources = _server_go_sources(repo_root)
    consts = _build_consts(src, *server_sources)
    search_sources = [src] + server_sources
    keys = set()
    for key, value in _map_entries(body, consts):
        _flatten(value, key + ".", keys, search_sources, set(), consts)
    return keys


# ---------------------------------------------------------------------------
# 3. PUT /api/config
# ---------------------------------------------------------------------------

_CASE_LABELS = re.compile(
    r'case\s+((?:"(?:[^"\\]|\\.)*"\s*,\s*)*"(?:[^"\\]|\\.)*")'
)
_QUOTED = re.compile(r'"((?:[^"\\]|\\.)*)"')
_CUT_PREFIX = re.compile(r'CutPrefix\s*\(\s*[A-Za-z0-9_\.]+\s*,\s*"([^"]+)"')

# Dynamic key sets applied before the switch in applyConfigPatch (api.go):
#   - supervisorLoopPatchKey: "autonomous.supervisor_loop." or the plan-key
#     form "supervisor_loop." (prefix constants in supervisor_loop.go:
#     autonomous.supervisor_loop. / supervisor_loop.).
# The other two (tracing.backends., autonomous.requirements.min_ollama.)
# are read from the HasPrefix/CutPrefix literals below.
_KNOWN_DOTTED_WILDCARDS = (
    "autonomous.supervisor_loop.*",
    "supervisor_loop.*",
)


def api_put_keys(repo_root):
    """Dotted keys accepted by PUT /api/config (``applyConfigPatch``/``handlePutConfig``, api.go).

    Keys are every quoted label of the key switch (``case "a.b", "c.d":``),
    plus the dynamic prefixes handled before the switch, each reported as
    ``prefix.*``: ``tracing.backends.*``,
    ``autonomous.requirements.min_ollama.*``,
    ``autonomous.supervisor_loop.*`` and ``supervisor_loop.*``.
    """
    api_path = os.path.join(repo_root, "internal", "server", "api.go")
    src = _strip_comments(_read(api_path))
    loc = _find_function(src, "applyConfigPatch")
    if loc is None:
        return set()
    body = src[loc[0] : loc[1]]
    keys = set(_KNOWN_DOTTED_WILDCARDS)
    for cm in _CASE_LABELS.finditer(body):
        for label in _QUOTED.finditer(cm.group(1)):
            key = label.group(1)
            if key.endswith("."):
                keys.add(key + "*")
            else:
                keys.add(key)
    for pm in _CUT_PREFIX.finditer(body):
        keys.add(pm.group(1) + "*")
    for pm in re.finditer(r'HasPrefix\s*\(\s*k\s*,\s*"([^"]+)"', body):
        keys.add(pm.group(1) + "*")
    return keys


# ---------------------------------------------------------------------------
# 4. comm-router `configure`
# ---------------------------------------------------------------------------


def router_configure_keys(repo_root):
    """Dotted keys accepted by the comm-router ``configure`` command.

    ``handleConfigure`` (``internal/router/router.go``) is generic: it
    parses ``<key>=<value>`` and forwards it to the router's configure
    function, which ``cmd/datawatch/main.go`` (``SetConfigureFunc``) wires
    as a PUT to the daemon's ``/api/config`` — the same apply-path as PUT
    /api/config — so the accepted key set is the PUT key set (see
    ``api_put_keys``).
    """
    return api_put_keys(repo_root)


# ---------------------------------------------------------------------------
# 5. CLI `config set`
# ---------------------------------------------------------------------------


def cli_config_set_keys(repo_root):
    """Dotted keys settable via ``datawatch config set <key> <value>``.

    ``newConfigSetCmd``/``runConfigSet`` (``cmd/datawatch/main.go``) is
    generic: it sends ``PUT /api/config`` to the running daemon (with
    ``applyConfigPatch`` doing validation + persistence in one place), so
    the accepted key set is the PUT key set (see ``api_put_keys``).
    """
    return api_put_keys(repo_root)


if __name__ == "__main__":  # pragma: no cover
    root = os.getcwd()
    for name in (
        "web_settings_keys",
        "api_get_keys",
        "api_put_keys",
        "router_configure_keys",
        "cli_config_set_keys",
    ):
        print("%s: %d keys" % (name, len(globals()[name](root))))
