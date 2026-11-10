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

- 2026-11-09, on question 3 (who runs a unit): "A is correct, configurable" (option A: the daemon on a schedule or a
  session started for it, chosen by a setting per unit; a session runs it until the scheduler exists). The
  setting is configurable on every surface that configures the daemon, like the packs settings.

- 2026-11-09, on question 4 (tier to model mapping): "A is the right one" (option A: reuse the existing LLM entries
  and the planning routing setting; each tier names an existing LLM entry; no new supervisor-specific setting).

- 2026-11-09, on question 5 (a small model is unsure): "The option to try a more mature tier or to stop and raise it as
  an issue should be configurable. I personally would have it turned on and have it try the next model and then
  if that was unsure to have it then raise the issue. But that should be configurable." Decision: a setting
  chooses, per unit with a global default, between escalating one tier and raising a question straight away;
  the recommended default is on (escalate once, then raise a question with what was tried), configurable on
  every surface that configures the daemon.

- 2026-11-09, on question 6 (local or community registry): "Hey, yes, publish them. We do have to make sure we don't
  get any project-specific things in there. There should be configuration options in the scripts that can
  inject things that might come from rules or definitions, which we can make sure are available in different
  repo configurations. But we need to set up something to make sure that we don't leak data watch-specific
  things or anything that is required for us of a bank module so it can work with anybody." (as heard; read
  as: publish supervisor units in the community registry; units carry no project-specific content; project
  facts are injected through configuration from rules or definitions, available per repository; a check
  must stop datawatch-specific content leaking in, so the published unit is a blank module that works for
  anybody.)

- 2026-11-09, on question 7 (where the lessons go): "Let's go with A and if you run into any issues, make sure it
  stops and raises them." (option A: split by kind; generic lessons travel inside the unit they belong to as
  part of the blank pack, datawatch-specific lessons stay in this repository and feed the injected
  configuration; the leak check keeps the two apart. Any problem found while splitting stops the work and is
  raised to the operator.)

- 2026-11-09, on the leak check patterns (pasted text, as heard): "For the leak detection, yes, the repo name, the
  pass [read as: paths], definitely any secrets, and the stuff that you had mentioned were all seemed valid
  things to be in scope. The rules, though, that are in the agent or the context files, those are local only.
  And if we have custom specific rules or something actually in the process of doing our minimal rules, uses a
  repos context to rules to make smaller ones, those are unique for the repo. Those should not be synced
  locally [read as: not synced to the registry]. ... If there's anything that would be a genericized rule that
  can be used as a way of approaching [things], so we could have a supervisor skill for doing version
  detection in a file or however the different things that we end up doing. Those can all be configurable sort
  of rules and things and procedures around it. People can take those and make those that fit their own
  stuff, or we can show how context and agent definitions can create and manage their own rules around
  things. So we just need to make sure we document this and make it as generic as possible and keep data
  watch specific stuff in our own repo. Let's go through the rest of the leak detection patterns in
  questions."
  Decisions: in scope for the leak check are the repository name, paths, secrets, and the items listed in
  requirement 3. Rules in agent or context files are local only and never published. Repo-specific rules
  derived from a repository's context (the smaller rules made from it) are unique to that repository and are
  not synced. Generic, configurable procedures (for example version detection in a file) may be published;
  the documentation must show how context and agent definitions create and manage a repository's own rules.
  The remaining pattern questions follow, one at a time.

- 2026-11-09, on leak pattern question 1 (the product name): "A" (allow the product name and its command, MCP tool
  and REST path names, because every unit needs them; block this repository's name, organization and project
  names and anything that identifies our deployment; the check holds an explicit allowlist of product terms,
  so a new term fails closed until added).

- 2026-11-09, on leak pattern question 2 (hosts and deployment identifiers): "A" (block every concrete deployment
  identifier: host names, IP addresses, node and peer names, deployment-tied ports and URLs; a unit refers to a
  node only through a configuration input whose value each repository supplies; documentation examples use
  reserved placeholder values such as example.invalid, and the check recognizes only those).

- 2026-11-09, on leak pattern question 3 (tracker ids and version numbers): "A" (block both, as in task text:
  tracker ids and Automaton ids never appear; literal release and version numbers never appear except in
  placeholder examples written as X.Y.Z; a unit that needs the current version reads it through a configuration
  input such as a version-file path and pattern, which is the version-detection skill).

- 2026-11-09, on leak pattern question 4 (lane, capacity and model names): "A" (block concrete lane and capacity
  numbers, backend names and model names; a unit refers only to a tier (script, small, strong) and to
  configuration inputs for limits such as a lane cap, set per repository; examples use placeholder values; the
  tier-to-entry mapping lives in each installation's LLM entries).

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

## Requirements that follow from the operator's answers

1. **Blank published units.** A supervisor unit in the community registry holds no datawatch-specific names,
   paths, hosts, release rules, tracker identifiers or lane rules. It works for any repository.
2. **Injected configuration.** Everything project-specific is an input the unit reads from configuration, which
   comes from the project's rules and definitions files. Each repository supplies its own values; a unit with a
   missing required value stops with a plain message naming it. datawatch's own values live in this repository,
   not in the unit.
3. **Leak check.** A check runs when a unit is authored and at publish time, and fails on datawatch-specific
   content in a unit (project name, repository paths, host names, tracker identifiers, version numbers, lane
   and capacity numbers, and any value that appears in this repository's own rules and definitions files). It
   reuses the existing local-leak and tracker-identifier checks where they fit, and it is a script file in the
   verify folder like any other gate. Its exact patterns are an open question for the operator.
4. **Same trust path.** A published supervisor unit is a pack: accepted by the operator per version, with the
   community source accepted once, as decided for packs.
