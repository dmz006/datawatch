#!/usr/bin/env python3
"""Find GitHub code-scanning / Dependabot alerts dismissed in the UI with
no corresponding security/accepted-risks.yml entry (GH#197's shared
daily-watch spec). A dismissal in GitHub's UI is itself an acceptance
decision -- this catches one made with no registry record, no impact
analysis and no expiry, bypassing the whole point of the standard.

This only ever REPORTS a gap; it never creates a registry entry for one
(that needs an actual impact analysis per alert, not a mechanical sync).

Code-scanning: works with the default GITHUB_TOKEN + `security-events:
read` permission.

Dependabot: GitHub's REST API has not reliably granted dependabot/alerts
read access to the default Actions GITHUB_TOKEN even with security-events:
read: a fine-grained PAT with the "Dependabot alerts: read-only"
repository permission is required. Set it as the SCA_WATCH_TOKEN secret;
without it, this degrades to "Not checked" with a warning rather than
failing, per datawatch-app's own sca_fix_watch.py behavior.

Usage:
  python3 scripts/dismissed_alerts_watch.py <owner/repo> <accepted-risks.yml>
Reads GH_TOKEN (code-scanning) and optionally SCA_WATCH_TOKEN
(dependabot) from the environment.
"""
import json
import os
import subprocess
import sys

try:
    import yaml
except ImportError:
    sys.exit("PyYAML is required: pip install pyyaml")


def gh_api_paginated(repo, path, token, state="dismissed"):
    env = dict(os.environ)
    if token:
        env["GH_TOKEN"] = token
    try:
        out = subprocess.run(
            ["gh", "api", f"repos/{repo}/{path}", "--paginate", "-X", "GET",
             "-f", f"state={state}", "-f", "per_page=100"],
            capture_output=True, text=True, check=True, env=env,
        )
    except subprocess.CalledProcessError as e:
        return None, e.stderr.strip()
    try:
        return json.loads(out.stdout), None
    except json.JSONDecodeError:
        return None, f"non-JSON response: {out.stdout[:200]!r}"


def main(argv):
    if len(argv) != 2:
        sys.exit(__doc__)
    repo, registry_path = argv

    with open(registry_path) as f:
        entries = (yaml.safe_load(f) or {}).get("entries") or []
    registered_ids = {e["id"] for e in entries}

    lines = ["## Dismissed GitHub findings with no registry entry", ""]

    gh_token = os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN")
    cs_alerts, cs_err = gh_api_paginated(repo, "code-scanning/alerts", gh_token)
    if cs_err:
        lines.append(f"**code-scanning: NOT CHECKED** -- {cs_err}")
    else:
        unregistered = [a for a in cs_alerts if f"code-scanning/{a['number']}" not in registered_ids]
        lines.append(f"**code-scanning** ({len(unregistered)} of {len(cs_alerts)} dismissed alerts unregistered):")
        for a in sorted(unregistered, key=lambda a: a["number"]):
            rule = a.get("rule", {}).get("id", "?")
            lines.append(f"  - `code-scanning/{a['number']}` ({rule}, dismissed as {a.get('dismissed_reason', '?')!r}): https://github.com/{repo}/security/code-scanning/{a['number']}")

    lines.append("")

    sca_token = os.environ.get("SCA_WATCH_TOKEN")
    if not sca_token:
        lines.append("**dependabot: NOT CHECKED** -- SCA_WATCH_TOKEN secret not set (needs a fine-grained PAT with 'Dependabot alerts: read-only'; the default GITHUB_TOKEN cannot read this endpoint).")
    else:
        dep_alerts, dep_err = gh_api_paginated(repo, "dependabot/alerts", sca_token)
        if dep_err:
            lines.append(f"**dependabot: NOT CHECKED** -- {dep_err}")
        else:
            unregistered = [a for a in dep_alerts if f"dependabot/{a['number']}" not in registered_ids]
            lines.append(f"**dependabot** ({len(unregistered)} of {len(dep_alerts)} dismissed alerts unregistered):")
            for a in sorted(unregistered, key=lambda a: a["number"]):
                lines.append(f"  - `dependabot/{a['number']}`: https://github.com/{repo}/security/dependabot/{a['number']}")

    print("\n".join(lines))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
