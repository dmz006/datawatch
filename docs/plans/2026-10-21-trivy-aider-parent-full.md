# Trivy findings in containers / agent-aider and parent-full (2026-10-21)

Date checked: 2026-10-21

## Source findings

Release workflow run 39589657149 (the latest Release run whose container jobs
had completed when this was written; the two newer runs were still in
progress and had not reached their container jobs). Two jobs failed at the
blocking Trivy step, "Scan ... — HIGH/CRITICAL (Trivy SARIF, blocking)":

| Job | Job id | Image | Target |
|-----|--------|-------|--------|
| containers / agent-aider | 119194986420 | agent-aider | python-pkg `opt/pipx/venvs/aider-chat/.../certifi-2025.8.3.dist-info` |
| containers / parent-full | 119194986497 | parent-full (debian 12.15) | debian packages |

### agent-aider: certifi

Trivy table: `Total: 1 (HIGH: 0, CRITICAL: 1)`.

| Package | Advisory | Severity | Status | Installed | Fixed |
|---------|----------|----------|--------|-----------|-------|
| certifi (METADATA) | GHSA-6q48-6c7w-9gw2 | CRITICAL | fixed | 2025.8.3 | 2026.10.19 |

Title: certifi bundles emSign root CAs with compromised intermediate CA keys.
The certifi copy sits inside the aider-chat pipx venv; no other package in
the image is reported.

### parent-full: libcups2

Trivy table: `Total: 1 (HIGH: 1, CRITICAL: 0)`.

| Package | Vulnerability | Severity | Status | Installed | Fixed |
|---------|---------------|----------|--------|-----------|-------|
| libcups2 | CVE-2026-55453 | HIGH | affected | 2.4.2-3+deb12u9 | none |

Title: cups: arbitrary code execution via newline injection in printer status
messages. Trivy reports the same package version for the amd64 and arm64
builds (both pulled `libcups2 2.4.2-3+deb12u9` from bookworm/main as a
dependency of the Java runtime install in parent-full).

### Other images

In the same run the other container jobs (agent-base, stats-cluster,
validator, agent-goose, agent-gemini, agent-opencode, agent-claude) passed the
blocking scan, and none of their logs lists libcups2 or CVE-2026-55453. Only
parent-full reports libcups2.

## certifi fix

The certifi floor is a pip pin, so an upgrade clears it. `Dockerfile.agent-aider`
now adds `"certifi>=2026.10.19"` (with a GHSA-6q48-6c7w-9gw2 comment) to the
forced-minimum pip install into the aider-chat venv, matching the existing
floors for gitpython, litellm, urllib3 and pyasn1. 2026.10.19 is exactly the
fixed version Trivy lists. A guard script,
`scripts/check-dockerfile-certifi.sh`, fails if the Dockerfile loses the pin.
The fix was already committed before this note; no further Dockerfile change is
needed, and it takes effect on the next agent-aider image build. Decision for
certifi: **upgrade**, no suppression.

## CVE summary (CVE-2026-55453)

The CUPS backend's `quote_string()` in `backend/ipp.c` keeps CR/LF characters
in a printer-supplied marker message. The scheduler can read an injected
newline as a separate PPD control line and save an attacker-chosen PPD
keyword, which the reporter showed can lead to code execution as the `lp` user
through a later print job on a Foomatic-backed queue. Versions before 2.4.20
are affected (advisory GHSA-7hqf-mfhx-7r3v, fix commit d41cf06).

## Debian security tracker evidence

Checked https://security-tracker.debian.org/tracker/CVE-2026-55453 on
2026-10-21 (source package `cups`; `libcups2` is a binary from it):

| Release | Source package version | Status |
|---------|------------------------|--------|
| bookworm / bookworm (security) | 2.4.2-3+deb12u9 | vulnerable |
| trixie | 2.4.10-3+deb13u2 | vulnerable |
| trixie (security) | 2.4.10-3+deb13u1 | vulnerable |
| forky | 2.4.18-1 | vulnerable |
| sid | 2.4.20-1 | fixed |

Debian bug: 1150560. There is no bookworm fixed version; only unstable has a
fix, so even trixie would not clear it.

## Decision

- **certifi (GHSA-6q48-6c7w-9gw2): upgrade.** Fixed by the pip floor above.
- **libcups2 (CVE-2026-55453): accepted risk, no-fix suppression.** The
  installed `2.4.2-3+deb12u9` is already the newest bookworm build, so
  `apt-get upgrade` cannot clear it, and a base-image move to trixie would not
  help either. Following the existing no-fix CVE precedent (see the
  CVE-2026-53615 note, docs/plans/2026-10-21-cve-2026-53615-agent-base.md), the
  finding is to be recorded as a no-fix entry in the Trivy ignore registry
  (`.trivyignore`) and `security/accepted-risks.yml`, citing
  security-tracker.debian.org/tracker/CVE-2026-55453 (checked 2026-10-21). This
  note does not edit those files; that registry change is a separate step and
  parent-full will keep failing the blocking scan until it lands.

Re-check trigger: re-run the tracker lookup when Debian publishes a bookworm
`cups` update (watch the tracker page); on a fix, remove the suppression and
rely on the existing `apt-get upgrade` step. Suggested review date 2026-11-20,
matching the precedent.

## Exposure note

Exploitation needs a hostile or spoofed IPP printer feeding crafted marker
messages to a CUPS scheduler plus a later print job on a Foomatic queue.
parent-full pulls libcups2 only as a library dependency of the Java runtime; it
is not expected to run a CUPS scheduler or print (not verified against the
runtime configuration), so practical exposure is likely low.
