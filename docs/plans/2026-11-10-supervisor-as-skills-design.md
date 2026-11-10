# Supervisor as skills, delivered as packs (design proposal, open)

Status: proposal. Nothing here is decided except what is quoted. No Automaton is drafted until the open
questions below are answered, one at a time.

## Operator direction (quoted)

- 2026-11-09: "We need to modulator even the supervisor, skills and eventually other models seem essential"
  (read as: make the supervisor modular; skills, and eventually other models, are essential).
- 2026-11-09, on how to take it forward: "A, but it should be designed to fit into the packs" (option A: a
  separate design proposal first; it must fit the packs design in `72bc0001`).

- 2026-11-09, on question 2 (what the first cut covers): "A, even though we need to make sure we have the full
  feature set planned out of when we're going to get the rest of it" (option A: read-only units first, state-
  changing units second in the same release train; a full roadmap is required so the rest has a date).
- 2026-11-09, on scratch and notes: "For scratch and notes datawatch has extensive memory that should be used"
  (as heard; the supervisor's working notes go in datawatch memory, not scratch files).

## Goal

Today the supervisor is one long rules document (`docs/plans/2026-10-17-supervisor-rules-and-lessons.md`),
two short cron prompts, and scripts in `scripts/` and in a scratch folder outside the repository. Only a
strong model in one long session can follow it. The goal is that each routine procedure is a unit that
datawatch can install, version, trust and run, and that a smaller or different model can run the units that
do not need judgment.

## Fit with packs

The packs design already decides: a pack is one folder with a manifest, versioned, synced through the skills
registry, trusted per version, with the community registry as the first source and datawatch's own material
as the first packs. This proposal adds no second mechanism. It adds one pack kind, called `supervisor` here
(name open), whose manifest lists procedures instead of Automaton tasks.

A procedure unit holds:
- a name, a version and a short description;
- the trigger (a schedule, an event such as a merge-ready Automaton, or a manual run);
- the steps, each a script file in the verify-style folder (never a command string), with a declared read-only
  mark and a report path where the step writes only a report;
- for each step, the lowest model tier that may run it: `script` (no model), `small` (a pi-prompt model),
  `strong` (a Claude model), or `operator` (stop and ask);
- the tools and files the unit may touch, shown at install like any third-party pack, and bounded by the
  worker session profile allow-list.

## Candidate units (from the current procedures)

| Unit | Today | Likely tier |
|---|---|---|
| Watch | `scripts/supervisor-watch.py`, prints OK or ATTENTION | script, then small to read findings |
| Budget position | `datawatch usage advice`, `scripts/claude-burn.py` | script |
| Lane fill | manual, per section 11a step 5 | small for mechanical routing, strong for validating a plan |
| Decompose validation | manual read of a plan and plan coverage | strong |
| Merge | trial merge, finish, ship scripts plus helper agents | script for clean merges, strong for semantic conflicts |
| Registry audit | `docs/plans/README.md` against reality, `scripts/branch-audit.py` | script plus small |
| CI read | `gh run list` and log reading | small |
| Failed task triage | manual recipes in the lessons file | strong |
| Deploy after ship | manual | script, gated by the restart-safety checks |

The scripts the units call move into the repository and into the pack folder, so the unit is complete without
the scratch folder.

## What stays with the operator

Scope, configuration, design, release 10.0 content, and any step marked `operator`. A unit may never change
settings or choose lane counts by itself unless a setting delegates that, and that setting would be named and
off by default.

## Open questions (one at a time)

1. Does the first cut cover only the read-only units (watch, budget, registry audit, CI read), or also the
   units that change state (lane fill, merge, deploy)? Recommendation: read-only units first, state-changing
   units second, in the same release train with no phasing of the format itself.
2. Who runs a unit: the daemon on a schedule, a session started for it, or either by setting? This ties to the
   scheduler question (the daemon cannot yet run a script on a schedule and alert on failure).
3. Where does the lessons file go: split into per-unit lessons carried in each unit, or kept as one shared
   reference every unit links to?
4. How is a model tier mapped to a real backend and model: by the existing LLM entries and the planning routing
   setting, or by a new setting? Recommendation: reuse the existing ones.
5. What does a unit do when a small model is unsure: stop and raise a question, or hand to the next tier?
   Recommendation: hand to the next tier once, then stop and raise a question.
6. Should the supervisor units be published in the community registry from the first release, as the packs
   decision says for the standard tasks, or kept local until the format is proven? The packs decision says no
   phasing; this asks whether supervisor units count.
7. After 10.0, the matrix test decides which models take which tiers. Nothing here depends on it, but the tier
   names must not be tied to a single vendor.

## Not in this proposal

No implementation, no change to the current cron jobs, and no change to who holds scope decisions. The current
procedures keep running as they are until a unit replaces one and a full day of agreement is shown (section 11a,
item 9).

## Roadmap (required by the operator's answer; dates are for the operator to set)

| Step | Content | Depends on | Target |
|---|---|---|---|
| 1 | Pack format and supervisor unit kind agreed in the packs design | packs Automaton decomposed and reviewed | open |
| 2 | Read-only units as a pack: watch, budget position, registry audit, CI read | step 1 | open |
| 3 | Scheduler: the daemon runs a unit on a schedule and alerts on failure | scheduler question | open |
| 4 | State-changing units: lane fill, merge, deploy-after-ship, with the restart-safety checks | steps 2 and 3, restart-safe verifier work shipped | open |
| 5 | Failure triage and decompose validation units (strong tier, operator stop points) | step 4 | open |
| 6 | Tier-to-model mapping run on the post-10.0 matrix test; smaller models take tiers they pass | matrix test | after 10.0 |
| 7 | Retire each manual check after a full day of agreement with its unit (section 11a, item 9) | each unit | rolling |

Targets stay open until the operator sets them; the supervisor lists, recommends and waits.
