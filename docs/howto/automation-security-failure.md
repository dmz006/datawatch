---
docs:
  index: true
  topics: [autonomous, automation, automaton, security, security failure, stop and report, shadow mode, accepted risks, advisory, dependency, alert]
exec_steps:
  - tool: security_failure_list
    description: Read the stored security failure reports, newest first, each beside the identity of the run it belongs to
    args: {}
    read_only: true
  - tool: security_failure_list
    description: Read only one repository's security failure reports
    args: {repo: "{{params.repo}}"}
    read_only: true
  - tool: security_failure_list
    description: Read only the reports whose run is in one watch state (green, failing, pending, hung, rerunning or superseded)
    args: {status: "{{params.status}}"}
    read_only: true
  - tool: get_config
    description: Read the daemon settings, including the autonomous.automation.security_failure block and the repo CI watch block it depends on
    args: {}
    read_only: true
  - tool: config_set
    description: Set the security failure mode (off, shadow or on); it takes effect on the next poll
    args:
      key: autonomous.automation.security_failure.mode
      value: "{{params.mode}}"
    read_only: false
---

# Security failure report

When a security check in your CI fails because of a dependency, the daemon
does two things it used to leave to a person: it **stops** the Automaton line
whose work the failing commit belongs to, and it **writes a report** that tells
you what failed and what to do about it. The report is the **security failure
report**. It is a read-only record. You decide what to do with the finding;
the daemon only makes sure it is not buried and that the line does not keep
building on top of it.

## What the stop means

The repo CI watch ([automation-repo-ci-watch.md](automation-repo-ci-watch.md))
keeps one record per CI run and marks a failed run on a security workflow as
`security`. The security failure pass looks at exactly those records: runs
that are `failing`, classified `security`, on a workflow named in
`autonomous.automation.repo_ci_watch.security_workflows` (by default `CodeQL`,
`dependency-review`, `owasp-zap`, `secret-scan` and `security-scan`).

For each such run it does this once, and never repeats it for the same run:

1. Reads the failed part of the run's log (`gh run view <run> --log-failed`).
2. Works out from the log what failed: a **dependency** (the log names an
   advisory and the package it applies to), a **CVE** (an advisory identifier
   but no package), or something that is not a dependency at all (a detected
   secret, a code-scanning finding) or that it cannot classify.
3. Looks the advisory up and checks `security/accepted-risks.yml` for an entry
   for the same advisory identifier. The match is exact; there is no fuzzy or
   prefix matching.
4. Looks for the **Automaton line** that carries the run's commit: the
   Automaton whose last completed task ended on that exact commit.
5. Stores the report on the run's watch record.

What happens to the Automaton depends on the mode and on what the log said:

| What failed | Is an Automaton carrying the commit? | Action in the report |
|---|---|---|
| A dependency or a CVE | Yes, and the mode is `on` | `stopped` — the Automaton is moved to **blocked** with the reason `security_failure_block` and the one-line report summary as the reason text; its triage record carries the report |
| A dependency or a CVE | Yes, and the mode is `shadow` | `would_stop` — nothing is stopped; the watch record gets one would-do line saying which Automaton would have been stopped |
| A dependency or a CVE | No | `report_only` — the note says no Automaton line carries this run's commit |
| Not a dependency (a secret, a code-scanning finding), or unclassified | — | `report_only` |

There is **no waiting clock**. An advisory published an hour ago gets exactly
the same action as one published a year ago. The publication date is shown in
the report when the advisory lookup knew it, and reads `unknown` when it did
not; it is never guessed.

A stopped Automaton stays blocked until you deal with it. The daemon never
resumes it, never restarts it and never works around the finding. After you
have fixed or accepted the finding, bring the Automaton back the way you bring
back any blocked Automaton.

## Base requirements

- A running daemon with the autonomous engine enabled.
- The repo CI watch running (its mode `shadow` or `on`) over the repository,
  because the watch is what records the failed security runs. With the watch
  `off` there is nothing for the security failure pass to read.
- `gh` installed and signed in on the daemon's machine, so the failed log can
  be read. If it cannot be read, the report is still stored and its note says
  the log could not be read.
- For the registry check, the repository's `security/accepted-risks.yml`
  present at the repository path you gave the watch. If it is missing or
  unreadable the report says so and the recommendation reads `unknown`.

## The modes

The mode is one config key: `autonomous.automation.security_failure.mode`.

| Mode | What the pass does |
|---|---|
| `off` | Nothing at all. No log is read, no lookup is made, nothing is stored and nothing is stopped. Reports already stored stay readable. |
| `shadow` | The default. The pass assesses each new failing security run and stores the report, exactly as `on` would, but it **never stops an Automaton and never raises an alert**. Where `on` would have stopped one, the report's action is `would_stop` and the watch record gets a would-do line. |
| `on` | The pass stores the report, stops the linked Automaton (action `stopped`), and raises one alert at once, with the report in its body. The alert does not wait for the triage alert time. |

An unset or empty mode reads `shadow`. Any other value is refused on every
write path, and nothing is written. The mode is read live, so a change takes
effect on the next poll and needs no restart.

**Run shadow first.** In shadow you see every report and every `would_stop`
without anything being stopped. Compare them with what you would have done
yourself for a few failing runs: did it name the right dependency, did the
recommendation match your judgment, would it have stopped the Automaton you
would have stopped? When the two agree, switch to `on`.

### Switching between the modes

| Surface | How |
|---|---|
| REST | `PUT /api/config` with `{"autonomous.automation.security_failure.mode": "on"}` |
| MCP | `config_set` with `key: autonomous.automation.security_failure.mode` and `value: on` |
| CLI | `datawatch config set autonomous.automation.security_failure.mode on` |
| Comm channel | `configure autonomous.automation.security_failure.mode=on` |
| PWA | Settings → General → Autonomous → **Security failure mode** |

`GET /api/config` shows the block under `autonomous.automation`, with the
effective value (an unset mode shows `shadow`).

The pass reads two other settings that are not part of this block: the set of
security workflows comes from
`autonomous.automation.repo_ci_watch.security_workflows`, and the alert time
from the existing `autonomous.automation.triage_alert_minutes`. There is no
other security failure key.

## Reading the report

The report can be read on five surfaces. All of them read the same stored
reports; none of them changes anything, stops anything or reruns anything.

| Surface | How to read it | Filter it |
|---|---|---|
| REST | `GET /api/autonomous/security-failures` — a JSON list, newest first. Each entry holds the run's identity (`repo`, `ref_kind`, `ref_name`, `workflow`, `run_id`, `run_url`, `status`) and the stored report under `report` | `?repo=<repository>` and/or `?status=<state>` (one of `green`, `failing`, `pending`, `hung`, `rerunning`, `superseded`; anything else is a 400 that names the allowed values; a filter that matches nothing is 200 with an empty list). Only GET is accepted. |
| MCP | `security_failure_list` (read-only, same list) | `repo` and `status` parameters, same vocabulary |
| CLI | `datawatch autonomous security-failure` — a table with repository, ref, workflow, run, status, dependency, advisory, recommendation and action | `--repo <repository>`, `--status <state>`, `--json` for the raw body. Without `--status` only open reports are listed: runs whose watch status is neither `green` nor `superseded`. |
| Comm channel | `autonomous security-failure` (alias `security_failure`) lists the open reports in the same words as the CLI. `autonomous security-failure <run-id>` replies with the full report of that run; an unknown run gets a not-found reply | the run identifier |
| PWA | Automata tab → Automation view → **Chains** sub-tab → the **Security failure** card, beside the triage card above the list, refreshed every few seconds. One block per report with the dependency, advisory, publication date, registry entry, recommendation, mode and action | the card shows every stored report |

The full report holds these things:

| Field | Meaning |
|---|---|
| Dependency | The package the log named, or `unknown` when it named none |
| Advisory | The advisory identifier or identifiers (a CVE or a GHSA identifier) |
| Published | The advisory's publication date, or `unknown` |
| Fixed version | Shown only when a fixed version is known |
| Registry entry | The accepted-risks entry for the advisory with its first-added, added and expires dates, or "not in the registry", or why the registry could not be read |
| Advisory lookup | Shown only when the lookup failed, with the reason |
| Recommendation | One of the four values below |
| Mode | The mode in effect when the report was made |
| Action | `stopped`, `would_stop` (shadow) or `report_only` |
| Note | Only where it adds something: why nothing was stopped, or why a log could not be read |

## What each recommendation means

| Recommendation | When it is given | What to do |
|---|---|---|
| `upgrade` | A fixed version is known, from the advisory lookup or from the log itself. Given whatever the registry says. | Upgrade the dependency to the fixed version. An existing registry entry does not change this: a fix exists, so accepting the risk is not the right answer. |
| `keep` | No fixed version is known, the registry could be read, and it holds an entry for this advisory. | Nothing is missing. The risk is already accepted in the registry; look at the entry's `expires` date and confirm it is still in date. |
| `keep_no_entry` | No fixed version is known, the registry could be read, and it holds **no** entry for this advisory. | If you decide the dependency can stay, add an entry to `security/accepted-risks.yml` (see below). The daemon will not do it for you. |
| `unknown` | No fixed version is known, and the advisory lookup or the registry read failed. | The daemon could not decide. Read the report's lookup and registry lines for the reason, fix the cause (network, a missing or malformed registry file) and judge it yourself. |

In the PWA the four values read "upgrade to the fixed version", "keep, the
registry holds an entry", "keep, but add a registry entry" and "unknown".

## Accepting a risk: the daemon never writes a suppression

The security failure pass **never writes a suppression**. It does not touch
`security/accepted-risks.yml`, the generated `.trivyignore` or the container
table in `docs/security-review.md`. A suppression is a decision that a person
owns, and it needs the dates and the reasoning the registry requires.

To accept a risk, you do it in the registry, in this order:

1. Add an entry to `security/accepted-risks.yml`. Its `id` carries a kind prefix
   and the advisory identifier exactly as the report shows it, for example
   `cve/CVE-2031-0042` or `osv/GHSA-aaaa-bbbb-cccc`. Fill in the fields the
   registry requires: `kind`, `package`, `version`, `severity`, `impact`
   (`traced`, `reachable`, `analysis`, and `method` when traced),
   `first_added`, `added`, `expires`, `validated_by` and `reason`. An
   untraced entry may run at most 30 days, a traced one at most 90.
2. Run `make security-registry-gen`. It validates the registry first and then
   regenerates `.trivyignore` and the container table in
   `docs/security-review.md` from it.
3. Commit the registry and the two regenerated files together.

Do not edit `.trivyignore` or the generated table by hand. `make lint` runs
`scripts/check-no-direct-ignore-writes.sh`, which regenerates both files in a
scratch directory and fails if either one differs from what the registry
produces, so a hand edit is caught. Given a base ref, the same script also
fails when `.trivyignore` changed without a change to the registry.

Once the entry is in the registry, the next report for the same advisory reads
`keep` instead of `keep_no_entry`. Reports already stored are not rewritten.

## What the daemon never does

- It never writes a suppression: not to the registry, not to `.trivyignore`,
  not to the security review document.
- It never reruns a security run, in any mode. A failed security run is
  evidence, and the repo CI watch already treats it as record-only. The
  security failure pass reads the failed log and stops there.
- It never cancels or deletes a run.
- It never stops anything in `shadow` or `off`.
- It never resumes or restarts the Automaton it stopped.
- It never assesses the same run twice, and never raises a second alert for it.
- It never guesses a missing publication date or dependency name.

## Operations

**Turning it off.** Set the mode to `off`. The pass stops and nothing new is
assessed. Reports already stored stay readable on every surface, and an
Automaton that was already stopped stays blocked.

**Restarting the daemon.** The reports live on the repo CI watch records and
reload with them, so the history is there after a restart. A run that already
carries a report is not assessed again.

**Where the report appears besides the read surfaces.** In `on` mode the alert
goes through the daemon's alert sink with the dependency, advisory,
publication date, registry entry, fixed version, lookup error and
recommendation in its body. The stopped Automaton's triage record carries the
same report.
