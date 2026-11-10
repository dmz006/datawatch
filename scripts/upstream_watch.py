#!/usr/bin/env python3
"""Upstream watch — maintainer tooling that reports on things Dependabot
cannot see. Never edits repository files, never needs the daemon.

Checks:
  1. Pinned tool versions: `^ARG NAME=value` in docker/dockerfiles/Dockerfile.*
     and Dockerfile.dev, compared against each tool's upstream release feed
     (go.dev, GitHub releases, the Kubernetes stable feed, npm/PyPI/RubyGems
     /crates.io registry JSON APIs — source chosen from how the Dockerfile
     actually downloads the tool).
  2. Scanner pin: TRIVY_VERSION in .github/workflows/*.y*ml vs the latest
     aquasecurity/trivy release.
  3. Scanner installer: a standing note for any workflow that still fetches
     the Trivy installer from the moving `main` branch (none do since the
     workflows moved to scripts/ci-install-trivy.sh, which fetches the pinned
     tag, so the note is not emitted), and a finding when the installer text
     at `main` differs (sha256) from the one at the pinned tag. That drift
     check is unchanged and no longer guards anything the workflows fetch.
     Monitoring only — the workflows are never edited by this script.
  4. Advisory-only: OSV (api.osv.dev/v1/query) for every pin whose package
     can be named; tools OSV cannot name go under "Not checked".
  5. Any feed failure (HTTP error, timeout, bad JSON) is recorded under
     "Not checked" with the URL and never aborts the other checks.

Usage:
  python3 scripts/upstream_watch.py [--repo-root DIR] [--report-md PATH]
      [--report-json PATH] [--offline-fixtures FILE]

--offline-fixtures FILE is a JSON object mapping URL -> response body text;
fetch reads only from that map (a URL missing from it counts as a feed
failure). Without it, all network access goes through the single module
function fetch(url, ...) — timeout 20 s, 2 retries, Authorization header
added only for api.github.com. GITHUB_TOKEN or GH_TOKEN is read from the
environment for GitHub API rate limits and never printed.

Exit status: 0 whether or not findings exist (the caller decides what to do
with the report); 2 on bad arguments or an internal error. Prints one
summary line to stdout.
"""
import argparse
import hashlib
import json
import os
import re
import sys
import time
import urllib.parse
import urllib.request
from datetime import datetime, timezone

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))

# ── Feed URLs ──────────────────────────────────────────────────────────
GO_DL_FEED = "https://go.dev/dl/?mode=json"
K8S_STABLE = "https://dl.k8s.io/release/stable.txt"
OSV_QUERY = "https://api.osv.dev/v1/query"
TRIVY_MAIN_INSTALLER = (
    "https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh"
)

# ── Pin -> upstream source table ───────────────────────────────────────
# Derived from how each Dockerfile actually downloads the tool (the
# RUN/curl lines and their comments), not from assumption.
#   source: ("godl",)                       go.dev download feed
#           ("github", "owner/repo")         GitHub releases/latest API
#           ("k8s",)                         Kubernetes stable text feed
#           ("npm", package)                 registry.npmjs.org JSON
#           ("pypi", package)                pypi.org JSON
#           ("rubygems", gem)                rubygems.org JSON
#           ("crates", crate)                crates.io JSON
#   osv: (ecosystem, package) for the advisory check, or None.
#   coupled: extra edits a source bump requires elsewhere.
GO_TOOLCHAIN_NOTE = (
    "Go versions have a floor in scripts/check-go-toolchain-pins.sh and the "
    "pin must keep scripts/check-dockerfile-go-versions.sh passing"
)

PIN_SOURCES = {
    # Go toolchains — go.dev download feed / golang:<ver> builder images.
    "GO_VERSION": {
        "tool": "Go toolchain",
        "source": ("godl",),
        "osv": ("Go", "stdlib"),
        "coupled": [GO_TOOLCHAIN_NOTE],
    },
    "GH_GO_VERSION": {
        "tool": "Go toolchain (gh builder)",
        "source": ("godl",),
        "osv": ("Go", "stdlib"),
        "coupled": [GO_TOOLCHAIN_NOTE,
                    "confirm the golang:<ver>-bookworm tag exists before changing it"],
    },
    # GitHub CLI built from source — cli/cli release tag must be a RELEASED tag;
    # GH_COMMIT is that tag's commit and is verified after the clone.
    "GH_VERSION": {
        "tool": "GitHub CLI (cli/cli)",
        "source": ("github", "cli/cli"),
        "osv": None,
        "coupled": [
            "GH_COMMIT in the same Dockerfile must move to the new tag's commit "
            "(the build verifies the clone HEAD equals it)",
        ],
    },
    # rtk-ai/rtk GitHub release tarball (v${RTK_VERSION}).
    "RTK_VERSION": {"tool": "rtk", "source": ("github", "rtk-ai/rtk"), "osv": None},
    # aaif-goose/goose GitHub release tarball (v${GOOSE_VERSION}).
    "GOOSE_VERSION": {"tool": "goose", "source": ("github", "aaif-goose/goose"), "osv": None},
    # golangci-lint / delve — GitHub release tags (go install lines reference
    # modules, but the upstream release the ARG tracks is the project tag).
    "GOLANGCI_LINT_VERSION": {
        "tool": "golangci-lint", "source": ("github", "golangci/golangci-lint"), "osv": None},
    "DELVE_VERSION": {"tool": "delve", "source": ("github", "go-delve/delve"), "osv": None},
    # tools-ops: kubectl from dl.k8s.io (stable feed is the version source),
    # helm from get.helm.sh (helm/helm GitHub release), yq from yq GitHub release.
    "KUBECTL_VERSION": {"tool": "kubectl", "source": ("k8s",), "osv": None},
    "HELM_VERSION": {"tool": "helm", "source": ("github", "helm/helm"), "osv": None},
    "YQ_VERSION": {"tool": "yq", "source": ("github", "mikefarah/yq"), "osv": None},
    # signal-cli GitHub release tarball (v${SIGNAL_CLI_VERSION}).
    "SIGNAL_CLI_VERSION": {
        "tool": "signal-cli", "source": ("github", "AsamK/signal-cli"), "osv": None},
    # Kotlin compiler GitHub release zip (v${KOTLIN_VERSION}).
    "KOTLIN_VERSION": {
        "tool": "Kotlin compiler", "source": ("github", "JetBrains/kotlin"), "osv": None},
    # bun via the bun.sh installer, which resolves the oven-sh/bun bun-v*
    # release asset.
    "BUN_VERSION": {
        "tool": "bun", "source": ("github", "oven-sh/bun"), "osv": None,
        "coupled": ["installed via the bun.sh installer script (moving endpoint)"]},
    # uv via https://astral.sh/uv/${UV_VERSION}/install.sh, which serves the
    # astral-sh/uv v* GitHub release asset.
    "UV_VERSION": {
        "tool": "uv", "source": ("github", "astral-sh/uv"), "osv": None,
        "coupled": ["installed via the astral.sh versioned installer"]},
    # npm tools — registry.npmjs.org JSON (base package; the Dockerfiles pull
    # the platform tarball of the same base package).
    "PNPM_VERSION": {"tool": "pnpm", "source": ("npm", "pnpm"), "osv": ("npm", "pnpm")},
    "TYPESCRIPT_VERSION": {
        "tool": "TypeScript", "source": ("npm", "typescript"), "osv": ("npm", "typescript")},
    "TSX_VERSION": {"tool": "tsx", "source": ("npm", "tsx"), "osv": ("npm", "tsx")},
    "ESLINT_VERSION": {
        "tool": "ESLint", "source": ("npm", "eslint"), "osv": ("npm", "eslint")},
    "ESLINT_SECURITY_VERSION": {
        "tool": "eslint-plugin-security", "source": ("npm", "eslint-plugin-security"),
        "osv": ("npm", "eslint-plugin-security")},
    "CLAUDE_CODE_VERSION": {
        "tool": "Claude Code", "source": ("npm", "@anthropic-ai/claude-code"),
        "osv": ("npm", "@anthropic-ai/claude-code")},
    "GEMINI_CLI_VERSION": {
        "tool": "Gemini CLI", "source": ("npm", "@google/gemini-cli"),
        "osv": ("npm", "@google/gemini-cli")},
    # opencode publishes per-platform npm packages (opencode-linux-x64 etc.); the
    # base name is not on the registry, so the version feed is the x64 package.
    "OPENCODE_VERSION": {
        "tool": "opencode", "source": ("npm", "opencode-linux-x64"),
        "osv": ("npm", "opencode")},
    # PyPI tools — pypi.org JSON (installed via pipx/pip).
    "AIDER_VERSION": {
        "tool": "aider-chat", "source": ("pypi", "aider-chat"), "osv": ("PyPI", "aider-chat")},
    "POETRY_VERSION": {
        "tool": "Poetry", "source": ("pypi", "poetry"), "osv": ("PyPI", "poetry")},
    "RUFF_VERSION": {"tool": "Ruff", "source": ("pypi", "ruff"), "osv": ("PyPI", "ruff")},
    "PYRIGHT_VERSION": {
        "tool": "Pyright", "source": ("pypi", "pyright"), "osv": ("PyPI", "pyright")},
    "BANDIT_VERSION": {
        "tool": "Bandit", "source": ("pypi", "bandit"), "osv": ("PyPI", "bandit")},
    "PIP_AUDIT_VERSION": {
        "tool": "pip-audit", "source": ("pypi", "pip-audit"), "osv": ("PyPI", "pip-audit")},
    # RubyGems tools — rubygems.org JSON (gem install lines).
    "RUBOCOP_VERSION": {
        "tool": "RuboCop", "source": ("rubygems", "rubocop"), "osv": ("RubyGems", "rubocop")},
    "RUBY_LSP_VERSION": {
        "tool": "ruby-lsp", "source": ("rubygems", "ruby-lsp"), "osv": ("RubyGems", "ruby-lsp")},
    "BUNDLER_VERSION": {
        "tool": "Bundler", "source": ("rubygems", "bundler"), "osv": ("RubyGems", "bundler")},
    "BRAKEMAN_VERSION": {
        "tool": "Brakeman", "source": ("rubygems", "brakeman"), "osv": ("RubyGems", "brakeman")},
    "BUNDLER_AUDIT_VERSION": {
        "tool": "bundler-audit", "source": ("rubygems", "bundler-audit"),
        "osv": ("RubyGems", "bundler-audit")},
    # crates.io tool (cargo install cargo-audit).
    "CARGO_AUDIT_VERSION": {
        "tool": "cargo-audit", "source": ("crates", "cargo-audit"), "osv": ("Cargo", "cargo-audit")},
}

# Version pins with no determinable upstream feed in scope: reported, never
# silent. Some of these are still advisory-checkable via OSV (osv set).
UNMAPPED_ARGS = {
    "GH_XNET_VERSION": {
        "reason": "Go module (golang.org/x/net) forced into the gh build's module "
                  "graph — advisory-only, no release feed in scope",
        "osv": ("Go", "golang.org/x/net")},
    "GOPLS_VERSION": {
        "reason": "installed via `go install` from the Go module proxy "
                  "(golang.org/x/tools) — advisory-only, no release feed in scope",
        "osv": ("Go", "golang.org/x/tools")},
    "GOVULNCHECK_VERSION": {
        "reason": "installed via `go install` from the Go module proxy "
                  "(golang.org/x/vuln) — advisory-only, no release feed in scope",
        "osv": ("Go", "golang.org/x/vuln")},
    "RUST_VERSION": {"reason": "rustup toolchain (static.rust-lang.org dist server); "
                               "no version feed in scope", "osv": None},
    "JDK_VERSION": {"reason": "apt openjdk-${JDK_VERSION} package; not a release pin",
                    "osv": None},
    "NODE_MAJOR": {"reason": "Node.js major from deb.nodesource.com; not a release pin",
                   "osv": None},
    "RUBY_PKG_VERSION": {"reason": "ruby image variant tag; not a tool release pin",
                         "osv": None},
    "GRADLE_VERSION": {
        "reason": "services.gradle.org binary download; no JSON version feed in scope",
        "osv": None},
    "ANDROID_CMDLINE_TOOLS_VERSION": {
        "reason": "dl.google.com commandlinetools archive; no version feed in scope",
        "osv": None},
    "TERRAFORM_VERSION": {
        "reason": "releases.hashicorp.com binary download; no version feed in scope",
        "osv": None},
}

# ARGs that are not tool versions and must be ignored outright.
NON_VERSION_ARGS = {
    "REGISTRY", "BASE_TAG", "MINIDEB_TAG", "TARGETARCH", "TARGETOS",
    "VERSION", "GH_COMMIT",
}

TRIVY_LATEST = ("github", "aquasecurity/trivy")
TRIVY_OSV = ("Go", "github.com/aquasecurity/trivy")


class FeedError(Exception):
    """A single feed failed; recorded under Not checked, never fatal.
    Carries the offending URL and a short reason separately so the report
    doesn't have to re-parse the message."""

    def __init__(self, url, reason):
        super().__init__(f"{url}: {reason}")
        self.url = url
        self.reason = reason


# ── Network: the ONLY network entry point ──────────────────────────────
FIXTURES = None  # set by main() from --offline-fixtures; {url: body text}


def fetch(url, method="GET", body=None):
    """Fetch `url` and return the response body as text.

    timeout 20 s, 2 retries. The Authorization token (GITHUB_TOKEN or
    GH_TOKEN) is sent only to api.github.com and is never printed. With
    FIXTURES active (offline mode) only the fixture map is consulted and a
    missing URL raises FeedError. Raises FeedError on any failure.
    """
    if FIXTURES is not None:
        if url in FIXTURES:
            return FIXTURES[url]
        raise FeedError(url, "offline fixture missing for URL")
    token = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN")
    data = body.encode("utf-8") if isinstance(body, str) else body
    last_err = None
    for attempt in range(3):  # initial try + 2 retries
        try:
            req = urllib.request.Request(url, data=data, method=method)
            req.add_header("User-Agent", "datawatch-upstream-watch")
            if url.startswith("https://api.github.com/"):
                req.add_header("Accept", "application/json")
                if token:
                    req.add_header("Authorization", f"Bearer {token}")
            elif url.startswith("https://api.osv.dev/"):
                req.add_header("Accept", "application/json")
            if method == "POST":
                req.add_header("Content-Type", "application/json")
            with urllib.request.urlopen(req, timeout=20) as resp:
                return resp.read().decode("utf-8", "replace")
        except Exception as err:  # URLError, HTTPError, timeout, ...
            last_err = err
            if attempt < 2:
                time.sleep(1 + attempt)
    raise FeedError(url, f"{type(last_err).__name__}: {last_err}")


def load_json(url, method="GET", body=None):
    """fetch + json.loads; FeedError on bad JSON or transport failure."""
    text = fetch(url, method=method, body=body)
    try:
        return json.loads(text)
    except (ValueError, TypeError) as err:
        raise FeedError(url, f"bad JSON: {err}")


# ── Version handling (pure) ────────────────────────────────────────────
_VERSION_RE = re.compile(r"(\d+(?:\.\d+)+)")
# pre-release shapes we must NOT rank as stable: 1.28.0rc1, 2.8.1b0, 1.0a3,
# 1.2.0-rc.1, 0.14.3-canary.874, 1.0beta1, nightly/canary/alpha/beta/dev tags.
_PRE_RELEASE_RE = re.compile(
    r"\d(?:\.\d+)*(?:-rc|-canary?)"
    r"|(?:^|[-.])?(?:rc|canary|nightly|alpha|beta|dev)\d"
    r"|\d+[ab](?=\d)"
    r"|\b(?:canary|nightly)\b",
    re.I,
)


def _numeric_parts(version):
    """Extract the first numeric X.Y.Z+ core from a version string ('v0.73.0'
    -> (0, 73, 0)). None if unparseable or it looks like a pre-release."""
    if not version or not str(version).strip():
        return None
    v = str(version).strip()
    if _PRE_RELEASE_RE.search(v):
        return None
    m = _VERSION_RE.search(v)
    if not m:
        return None
    parts = tuple(int(p) for p in m.group(1).split("."))
    return parts or None


def extract_version(text):
    """Public wrapper over _numeric_parts (tests target the pure function)."""
    return _numeric_parts(text)


def compare_versions(a, b):
    """-1/0/1 like a comparator; None if either side is unparseable.
    Compares the numeric X.Y.Z cores; pre-releases are not ranked."""
    pa, pb = _numeric_parts(a), _numeric_parts(b)
    if pa is None or pb is None:
        return None
    if pa < pb:
        return -1
    if pa > pb:
        return 1
    return 0


def classify_bump(pinned, latest):
    """'major' | 'minor' | 'patch' for a newer upstream version (pinned <
    latest). None if the values are not comparable."""
    pa, pb = _numeric_parts(pinned), _numeric_parts(latest)
    if pa is None or pb is None or _numeric_parts(pinned) >= _numeric_parts(latest):
        return None
    if len(pa) >= 1 and len(pb) >= 1 and pa[0] != pb[0]:
        return "major"
    if pa[1:2] != pb[1:2]:
        return "minor"
    return "patch"


# ── Dockerfile + workflow parsing (pure) ───────────────────────────────
_ARG_RE = re.compile(r"^ARG\s+([A-Z][A-Z0-9_]*)=([^\s\\]+)", re.M)


def parse_dockerfile_args(text):
    """[(name, value)] for every top-level `ARG NAME=value` that carries a
    version-shaped value. Non-versions (REGISTRY, BASE_TAG, MINIDEB_TAG,
    TARGETARCH, GH_COMMIT, values with '$', 'latest', commit ids) are
    ignored. Re-declaring `ARG NAME` without a value is not a pin."""
    pins = []
    for m in _ARG_RE.finditer(text):
        name, value = m.group(1), m.group(2)
        if name in NON_VERSION_ARGS:
            continue
        if "$" in value or value.lower() == "latest":
            continue
        if re.fullmatch(r"[0-9a-f]{40}", value, re.I):
            continue
        pins.append((name, value))
    return pins


def trivy_pins_in_workflows(workflow_dir):
    """[(relpath, trivy_version)] for every workflow that pins TRIVY_VERSION
    as `TRIVY_VERSION: vX.Y.Z`."""
    pins = []
    if not os.path.isdir(workflow_dir):
        return pins
    for fn in sorted(os.listdir(workflow_dir)):
        if not (fn.endswith(".yaml") or fn.endswith(".yml")):
            continue
        rel = os.path.join(workflow_dir, fn)
        with open(rel, encoding="utf-8") as f:
            for line in f:
                m = re.match(r"\s*TRIVY_VERSION\s*:\s*(v?[\w.]+)\s*$", line)
                if m:
                    pins.append((os.path.relpath(rel, os.path.dirname(os.path.dirname(workflow_dir))), m.group(1)))
    return pins


def find_installer_refs(workflow_dir, needle=TRIVY_MAIN_INSTALLER):
    """[(relpath, [line numbers]) for workflow lines that fetch the Trivy
    installer from the moving `main` branch."""
    refs = []
    if not os.path.isdir(workflow_dir):
        return refs
    for fn in sorted(os.listdir(workflow_dir)):
        if not (fn.endswith(".yaml") or fn.endswith(".yml")):
            continue
        rel = os.path.join(workflow_dir, fn)
        hits = []
        with open(rel, encoding="utf-8") as f:
            for i, line in enumerate(f, start=1):
                if needle in line:
                    hits.append(i)
        if hits:
            refs.append((os.path.relpath(rel, os.path.dirname(os.path.dirname(workflow_dir))), hits))
    return refs


# ── Latest-version lookups (each does at most one feed call) ───────────
def latest_from_source(source):
    """Return (latest_version_string, feed_url) for a source spec, or raise
    FeedError."""
    kind = source[0]
    if kind == "godl":
        data = load_json(GO_DL_FEED)
        versions = [v.get("version") for v in data if v.get("stable")]
        best = max((v for v in versions if _numeric_parts(v)), key=_numeric_parts, default=None)
        if best is None:
            raise FeedError(GO_DL_FEED, "no stable version parsed from feed")
        return best, GO_DL_FEED
    if kind == "github":
        owner_repo = source[1]
        url = f"https://api.github.com/repos/{owner_repo}/releases/latest"
        data = load_json(url)
        tag = data.get("tag_name")
        if not tag:
            raise FeedError(url, "no tag_name in response")
        return tag, url
    if kind == "k8s":
        text = fetch(K8S_STABLE)
        ver = text.strip().splitlines()[0].strip()
        if not _numeric_parts(ver):
            raise FeedError(K8S_STABLE, f"unparseable stable version {ver!r}")
        return ver, K8S_STABLE
    if kind == "npm":
        pkg = urllib.parse.quote(source[1], safe="@/")
        data = load_json(f"https://registry.npmjs.org/{pkg}")
        candidates = [data.get("dist-tags", {}).get("latest"), *data.get("versions", {})]
        best = max((c for c in candidates if c and _numeric_parts(c)),
                   key=_numeric_parts, default=None)
        if best is None:
            raise FeedError(f"https://registry.npmjs.org/{pkg}",
                           f"no parseable version for npm package {source[1]!r}")
        return best, data.get("npm", f"https://www.npmjs.com/package/{source[1]}")
    if kind == "pypi":
        pkg = source[1]
        url = f"https://pypi.org/pypi/{pkg}/json"
        data = load_json(url)
        versions = data.get("releases", {})
        best = max((v for v in versions if _numeric_parts(v)),
                   key=_numeric_parts, default=None)
        if best is None:
            raise FeedError(url, f"no parseable version for PyPI package {pkg!r}")
        return best, url
    if kind == "rubygems":
        gem = source[1]
        url = f"https://rubygems.org/api/v1/gems/{gem}.json"
        data = load_json(url)
        ver = data.get("version")
        if not ver or not _numeric_parts(ver):
            raise FeedError(url, f"no parseable version in response")
        return ver, url
    if kind == "crates":
        crate = source[1]
        url = f"https://crates.io/api/v1/crates/{crate}"
        data = load_json(url)
        ver = data.get("max_stable_version")
        if not ver or not _numeric_parts(ver):
            raise FeedError(url, "no stable version in response")
        return ver, url
    raise FeedError(f"<source {kind}>", "unknown source kind")


def _osv_fixed_version(vuln, name):
    """First 'fixed' event in the vuln's ranges for the checked package."""
    for affect in vuln.get("affects", []) or []:
        pkg = (affect.get("package") or {})
        if pkg.get("name") != name:
            continue
        for rng in affect.get("ranges", []) or []:
            for event in rng.get("events", []) or []:
                if "fixed" in event:
                    return event["fixed"]
    return None


def osv_advisories(ecosystem, name, version, not_checked):
    """OSV query for one pinned package. Returns a list of advisory dicts;
    a failed query lands in not_checked and returns []."""
    body = json.dumps({"package": {"name": name, "ecosystem": ecosystem},
                       "version": version})
    try:
        data = load_json(OSV_QUERY, method="POST", body=body)
    except FeedError as err:
        not_checked.append({"url": err.url, "detail": f"advisory check for "
                            f"{name} ({version}): {err.reason}"})
        return []
    advisories = []
    for vuln in data.get("vulns", []) or []:
        advisories.append({
            "kind": "advisory",
            "osv_id": vuln.get("id", "?"),
            "summary": (vuln.get("summary") or vuln.get("details", ""))[:500].strip(),
            "fixed": _osv_fixed_version(vuln, name),
            "pinned": version,
            "package": name,
        })
    return advisories


# ── Report building (pure) ─────────────────────────────────────────────
# Severity rank within the stale-pin section: patch (0), minor (1), major last (2).
_BUMP_RANK = {"patch": 0, "minor": 1, "major": 2}


def build_report(findings, standing, unmapped, not_checked, checked_pins,
                 last_checked):
    """Assemble (markdown, json) from flat lists. `findings` items carry a
    'kind' of 'advisory' or 'stale_pin'; ordering: advisories first, then
    stale pins ordered major last (patch first, major at the end)."""
    ordered = sorted(findings, key=lambda f: (
        0 if f["kind"] == "advisory" else 1,
        _BUMP_RANK.get(f.get("bump"), 3) if f["kind"] == "stale_pin" else 0,
        f.get("package", f.get("dockerfile", "")),
        f.get("pinned", ""),
    ))
    advisory_count = sum(1 for f in ordered if f["kind"] == "advisory")
    stale_count = sum(1 for f in ordered if f["kind"] == "stale_pin")

    lines = [
        "<!-- upstream-watch -->",
        "",
        f"**upstream-watch**: {len(ordered)} finding(s) ({advisory_count} "
        f"advisorie(s), {stale_count} stale pin(s)) across {checked_pins} "
        f"checked pin(s); {len(unmapped)} unmapped; {len(not_checked)} not checked.",
        "",
        "## Findings",
        "",
    ]
    if not ordered:
        lines.append("No findings.")
    advisories = [f for f in ordered if f["kind"] == "advisory"]
    stale = [f for f in ordered if f["kind"] == "stale_pin"]
    if advisories:
        lines.append("### Advisories (OSV, advisory-only — these pins may "
                     "outdate even when no newer version exists)")
        lines.append("")
        for f in advisories:
            where = f.get("dockerfile") or f.get("workflow")
            arg = f.get("arg")
            target = f"`{arg}` in {where}" if arg else (where or f["package"] or "")
            target = f"({target})" if target else ""
            fixed = f" — first fixed in {f['fixed']}" if f.get("fixed") else ""
            lines.append(f"- **{f['osv_id']}** on {f['package']} "
                         f"{f['pinned']} {target}: "
                         f"{f['summary'] or '(no summary)'}{fixed}".rstrip())
        lines.append("")
    if stale:
        lines.append("### Stale pins (newer upstream version available)")
        lines.append("")
        for f in stale:
            human = " — **human-only**" if f.get("human_only") else ""
            lines.append(
                f"- **{f['bump']}**: {f['package']} {f['pinned']} -> "
                f"{f['latest']}{human} — edit `{f['arg']}` in "
                f"`{f['dockerfile']}` (upstream: {f.get('feed', '?')})")
            for note in f.get("coupled", []):
                lines.append(f"  - coupled edit: {note}")
        lines.append("")

    lines += ["## Standing notes", ""]
    lines += ([f"- {n}" for n in standing] or ["- (none)"])
    lines += ["", "## Unmapped pins (no upstream feed in scope — reported, "
                "never silent)", ""]
    lines += ([f"- `{u['arg']}`={u['value']} in {', '.join(u['dockerfiles'])}: "
               f"{u['reason']}" for u in unmapped] or ["- (none)"])
    lines += ["", "## Not checked (feed failures / unnameable packages)", ""]
    lines += ([f"- `{n['url']}` — {n['detail']}" for n in not_checked] or ["- (none)"])
    lines += ["", "## Last checked", "", last_checked, ""]

    digest = hashlib.sha256("\n".join(lines).encode("utf-8")).hexdigest()[:16]
    md = "\n".join(lines)
    jdoc = {
        "marker": "upstream-watch",
        "last_checked": last_checked,
        "findings_count": len(ordered),
        "findings": ordered,
        "standing": standing,
        "unmapped": unmapped,
        "not_checked": not_checked,
        "checked_pins": checked_pins,
        "report_sha256_prefix": digest,
    }
    return md, jdoc


# ── Checks ─────────────────────────────────────────────────────────────
def check_pins(repo_root, not_checked):
    """Check 1 + 4 for Dockerfile pins. Returns (findings, standing, unmapped,
    checked_count)."""
    findings, unmapped, checked = [], [], 0
    candidates = []
    patterns = [
        os.path.join(repo_root, "docker", "dockerfiles"),
    ]
    for d in patterns:
        if os.path.isdir(d):
            candidates += [os.path.join(d, fn) for fn in sorted(os.listdir(d))
                           if fn.startswith("Dockerfile")]
    dev = os.path.join(repo_root, "Dockerfile.dev")
    if os.path.isfile(dev):
        candidates.append(dev)

    for path in candidates:
        rel = os.path.relpath(path, repo_root)
        try:
            with open(path, encoding="utf-8") as f:
                text = f.read()
        except OSError:
            continue
        for name, value in parse_dockerfile_args(text):
            spec = PIN_SOURCES.get(name)
            if spec is None:
                entry = {
                    "arg": name, "value": value, "dockerfiles": [rel],
                    "reason": UNMAPPED_ARGS.get(name, {}).get(
                        "reason", "no upstream source determinable from the Dockerfile"),
                }
                for existing in unmapped:
                    if existing["arg"] == name and existing["value"] == value:
                        existing["dockerfiles"].append(rel)
                        break
                else:
                    unmapped.append(entry)

            # Check 4: advisory-only OSV query (mapped + some unmapped pins).
            osv = (spec or {}).get("osv") or UNMAPPED_ARGS.get(name, {}).get("osv")
            if osv:
                eco, pkg = osv
                for adv in osv_advisories(eco, pkg, value, not_checked):
                    adv.update({"package": pkg, "pinned": value, "dockerfile": rel,
                                "arg": name, "name": name,
                                "tool": (spec or {}).get("tool")
                                or UNMAPPED_ARGS.get(name, {}).get("reason", pkg)})
                    findings.append(adv)

            if spec is None:
                continue
            # Check 1: compare against the upstream feed.
            try:
                latest, feed = latest_from_source(spec["source"])
            except FeedError as err:
                not_checked.append({"url": err.url, "detail": err.reason})
                continue
            checked += 1
            cmp = compare_versions(value, latest)
            if cmp is not None and cmp < 0:
                bump = classify_bump(value, latest)
                findings.append({
                    "kind": "stale_pin",
                    "arg": name,
                    "dockerfile": rel,
                    "package": spec["tool"],
                    "pinned": value,
                    "latest": latest,
                    "bump": bump,
                    # Major bumps and toolchain (Go) moves are human-only.
                    "human_only": bump == "major"
                    or name in ("GO_VERSION", "GH_GO_VERSION"),
                    "feed": feed,
                    "coupled": (spec or {}).get("coupled", []),
                })
    # de-duplicate coupled notes
    for f in findings:
        if f["kind"] == "stale_pin":
            seen, deduped = set(), []
            for note in f.setdefault("coupled", []):
                if note not in seen:
                    seen.add(note)
                    deduped.append(note)
            f["coupled"] = deduped
    return findings, unmapped, checked


def check_trivy(repo_root, not_checked):
    """Checks 2 + 3. Returns (findings, standing)."""
    findings, standing = [], []
    workflow_dir = os.path.join(repo_root, ".github", "workflows")
    for rel, value in trivy_pins_in_workflows(workflow_dir):
        try:
            latest, feed = latest_from_source(TRIVY_LATEST)
        except FeedError as err:
            not_checked.append({"url": err.url, "detail": err.reason})
            continue
        cmp = compare_versions(value, latest)
        if cmp is not None and cmp < 0:
            findings.append({
                "kind": "stale_pin",
                "arg": "TRIVY_VERSION",
                "dockerfile": rel,  # it's a workflow, report says so below
                "package": "Trivy",
                "pinned": value,
                "latest": latest,
                "bump": classify_bump(value, latest),
                "human_only": True,  # scanner toolchain — human-only
                "feed": feed,
                "coupled": [],
            })
        # Check 3b: installer at `main` vs installer at the pinned tag.
        main_installer = TRIVY_MAIN_INSTALLER
        tag = value if value.startswith("v") else f"v{value}"
        pin_installer = (
            f"https://raw.githubusercontent.com/aquasecurity/trivy/{tag}/contrib/install.sh"
        )
        try:
            sha_main = hashlib.sha256(fetch(main_installer).encode("utf-8")).hexdigest()
            sha_pin = hashlib.sha256(fetch(pin_installer).encode("utf-8")).hexdigest()
        except FeedError as err:
            not_checked.append({"url": err.url, "detail": err.reason})
            continue
        if sha_main != sha_pin:
            findings.append({
                "kind": "advisory",
                "osv_id": "trivy-installer-drift",
                "summary": (
                    "Trivy installer at `main` differs (sha256 "
                    f"{sha_main[:12]}...) from the one at pinned tag {tag} "
                    f"({sha_pin[:12]}...) — monitoring only"
                ),
                "fixed": None,
                "pinned": value,
                "package": "Trivy installer",
                "workflow": rel,
                "arg": None,
                "dockerfile": None,
                "name": "trivy-installer",
                "tool": "Trivy installer drift (main vs pinned tag)",
            })
    # Check 3a: standing note for every workflow pulling the installer from
    # the moving `main` branch.
    refs = find_installer_refs(workflow_dir)
    if refs:
        total = sum(len(h) for _, h in refs)
        per = ", ".join(f"{r} ({len(h)} line{'s' if len(h) != 1 else ''}, L{h[0] if len(h) == 1 else f'{h[0]}-{h[-1]}'})"
                        for r, h in refs)
        standing.append(
            f"Trivy installer fetched from the moving `main` branch in "
            f"{len(refs)} workflow file(s), {total} line(s) total: {per}. "
            "Monitoring only — not edited by this script."
        )
    return findings, standing


# ── Main ───────────────────────────────────────────────────────────────
def run_watch(repo_root):
    not_checked = []
    findings, unmapped, checked_pins = check_pins(repo_root, not_checked)
    trivy_findings, trivy_standing = check_trivy(repo_root, not_checked)
    findings.extend(trivy_findings)
    seen, deduped = set(), []
    for n in not_checked:
        key = (n["url"], n["detail"])
        if key not in seen:
            seen.add(key)
            deduped.append(n)
    not_checked = deduped
    last_checked = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    md, jdoc = build_report(findings, trivy_standing, unmapped, not_checked,
                            checked_pins, last_checked)
    return md, jdoc


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Report on upstream tool changes Dependabot cannot see.",
    )
    parser.add_argument("--repo-root", default=os.path.dirname(SCRIPT_DIR),
                        help="repository root (default: parent of scripts/)")
    parser.add_argument("--report-md", default=None,
                        help="write the Markdown report to this path")
    parser.add_argument("--report-json", default=None,
                        help="write the JSON report to this path")
    parser.add_argument("--offline-fixtures", default=None,
                        help="JSON object mapping URL -> response body text")
    try:
        args = parser.parse_args(argv)
    except SystemExit:
        raise
    repo_root = os.path.abspath(args.repo_root)
    if not os.path.isdir(repo_root):
        print(f"error: repo root not found: {repo_root}", file=sys.stderr)
        return 2

    global FIXTURES
    if args.offline_fixtures is not None:
        try:
            with open(args.offline_fixtures, encoding="utf-8") as f:
                fixtures = json.load(f)
            if not isinstance(fixtures, dict):
                raise ValueError("fixtures file must be a JSON object URL -> body")
        except (OSError, ValueError) as err:
            print(f"error: bad --offline-fixtures: {err}", file=sys.stderr)
            return 2
        FIXTURES = fixtures

    try:
        if not os.path.isdir(os.path.join(repo_root, "docker", "dockerfiles")) \
                and not os.path.isfile(os.path.join(repo_root, "Dockerfile.dev")):
            return 2  # looks like the wrong directory — bad arguments, not a run
        md, jdoc = run_watch(repo_root)
    except FeedError:
        pass  # impossible to escape the checks, but be defensive
    except Exception as err:
        print(f"internal error: {type(err).__name__}: {err}", file=sys.stderr)
        return 2

    try:
        if args.report_md:
            with open(args.report_md, "w", encoding="utf-8") as f:
                f.write(md)
        if args.report_json:
            with open(args.report_json, "w", encoding="utf-8") as f:
                f.write(json.dumps(jdoc, indent=2, sort_keys=False) + "\n")
    except OSError as err:
        print(f"error: cannot write report: {err}", file=sys.stderr)
        return 2
    if args.report_md and args.report_json:
        print(f"upstream-watch: {jdoc['findings_count']} finding(s); "
              f"reports written to {args.report_md} and {args.report_json}")
    else:
        print(f"upstream-watch: {jdoc['findings_count']} finding(s); "
              f"{len(jdoc['unmapped'])} unmapped pin(s); "
              f"{len(jdoc['not_checked'])} feed(s) not checked")
    return 0


if __name__ == "__main__":
    sys.exit(main())
