# "Done when": acceptance criteria for every kind of request (revised design)

Revised 2026-11-26. This replaces the 2026-11-25 draft and takes in its review (16 rules against a false "done"). Code paths are under `internal/`.

Decided: **both builds come before the first unpause** (A, the core; B, drafting and the independent reviewer). Rules about **who** may change or approve something move to **11.0**, with the authentication work. Keeping the frozen list, running the saved copy of its checks and showing every change is validation, and stays.

**What 10.0 does not give.** A worker can change its own list. 10.0 runs the saved copy and shows every change datawatch records. It does not stop the edit, and a hostile local process can change the records too.

## 1. The model

One list per **ask** (an Automaton, or a lone request). Tasks add evidence; they have no list of their own. datawatch's own drafting and review runs are not requests.

The list keeps: the ask **word for word** with a hash, the kind (set by code from the source; an unknown source is `other`), the items, every revision, when it was frozen and the list hash.

| Item field | Meaning |
|---|---|
| statement | one plain "done when" sentence |
| from | the words of the ask it covers |
| method, check | `command`, `state:<what>`, `receipt`, `confirm`, `review`. A project check is a saved, named command. `wait` and `occurrence` modify command or state. Any other name reads "cannot tell" |
| change, negative, report only | proves work was done; proves something did not happen; only reports a failure |
| effect | where it counts, free text. Branch, build and docs defaults live in data packs, not in the core |
| result | unchecked, met, not met, cannot tell (with a reason, or "waiting" with a due time) |
| evidence | what was run or read, output tail, exit code, when, subject version, origin |

**Roll-up.** Done only when every item is explicitly met on the current subject version. Zero items is never done. An accepted item reads "accepted without proof", never met. Receipt-only requests read "delivered" or "filed", never done. No list reads "no criteria". Anything that closes a bug or a request, or deletes its work, reads the list and never "completed".

**Defaults (four, as data, plus a scheduled modifier):** written deliverable; change to something live; delivery only; other. A project can add or replace them in a data file. A software pack (on the main branch, docs, settings survive a restart) is opt-in data.

**Methods that run commands** are a safety boundary: the command text comes only from an operator-registered project check in the config file, or from a script the daemon froze through the plan's approval path. No REST, MCP, CLI, chat or web route accepts command text.

## 2. The 16 rules against a false "done"

Each rule has a test with fakes and an injected clock. "Build" says which job carries it.

| # | Rule | Test | Build |
|---|---|---|---|
| 1 | Failed-before needs a start run that **finished** and failed at a named test or output line that later passes. A timeout, crash, missing tool or kill is no baseline: cannot tell | start = timeout, exit 127 or killed, after = 0: not met | A |
| 2 | Exit 0 is not proof. A change item needs "before". The item names what must have run; zero run or skipped is cannot tell. A check that already passed never alone meets a change item | script `true`; a filter matching no test | A |
| 3 | Run the saved check on a clean copy of the exact version. Freeze hashes of the check's listed inputs; a changed input is shown and reads cannot tell until the item is revised after review | Makefile target changed to `echo`; planted untracked helper | A |
| 4 | No result is taken from verifier, guardrail, final gate, landing or shipped records. An item runs only its own saved check; no fallback | each shortcut leaves the item unchecked | A |
| 5 | A run is keyed by request, item, revision, check hash, subject version and occurrence | unread exit 0, subject changes: it runs again | A |
| 6 | An inherited "done when" is a draft. A branch that only reports the failure is flagged and never closes a change: "failure now reported, still broken" | only the alert branch true: not done | A |
| 7 | Silence is not success. A negative item needs fresh positive answers over the whole window (bounded gaps, or a complete event log). A gap or restart is cannot tell | empty plus error; stale source; load then unload | A |
| 8 | The start value is always saved; "already true" is shown and never proves work. Command items run on the **effect** version. A deploy compares the build commit | `-s ours` merge; revert; same version, old commit | A |
| 9 | What the holder can write is a claim and proves no outside effect. The holder's own confirmation or acceptance is shown as its claim | forged confirmation file: cannot tell | A |
| 10 | Every reviewer "met" carries a quote datawatch finds at the cited place or in its saved copy. Cut or missing input is cannot tell. The reviewer must be able to open the subject. A true but irrelevant quote, and instructions planted in the work, are **not** settled by this | invented quote; cut diff | B |
| 11 | One stored verdict per item text, subject version and reviewer settings. Rerun only after an infrastructure fault. An earlier "not met" on the same subject makes a later "met" read "reviewers disagree". No route submits a verdict or sets met | a second run on the same key starts none | A (store), B (runs) |
| 12 | Zero items, or only default or receipt items on a real ask, is never done. Unmapped ask sentences become unchecked items. With no reviewer (or a person who confirms the list covers the ask), a free-text list stays "not reviewed" | each case | A; the reviewer's uncovered-ask question is B |
| 13 | A changed ask is not done until the list is revised. Words beyond a chosen answer become an open item | follow-up words; "A, and also move X" | A |
| 14 | Code at the source sets the kind. Receipt-only is never done. The list belongs to the ask: reset, remove, cancel, split or clone cannot drop or weaken it; open items pass to a named successor or to the operator's list | each case | A (core); sources call it in job 5 |
| 15 | Changes are also computed against revision 1. Changed or accepted items show the claimed name beside the observed caller. Approval and freeze name the same list hash. This shows; it blocks no call | draft changed after approval; weakened item is not plain done | A |
| 16 | Empty is unchecked. Only an explicit result counts. Error, empty reply, removed method, mode off and no reviewer are never met. No list is "no criteria" | table over every method and consumer | A |

## 3. No stalls, storms or runaway cost

- A judged result binds to the **content judged**. Only deterministic checks run again at the effect. A done request closes with its versions; unrelated commits start no reviewer.
- "Not yet due" and "usage hold" read **waiting** (reason, due time): no retry spent, not the operator's action until overdue.
- Rework: at most **2 rounds per list** (a proposed number, a setting), counted across new holders and resets. No round without a changed subject. Then it is the operator's.
- Open items are grouped by source and reason with a count. A list may carry a needed-by time with an early warning.
- A scheduled occurrence gets its own list at its due time, so one that never fires still shows.
- A judged item on a 15-minute schedule is 96 runs a day. Defaults for messages, operations, intake, handoffs and questions have no judged item.

## 4. Who may do what (recorded in 10.0, enforced in 11.0)

Four actions: **view, edit, confirm, accept**. There is no route that submits a verdict or evidence, or sets met. Every change records the words, the **claimed** name and the **observed** caller (from the credential). 10.0 refuses no caller on these grounds beyond the capability the route family already needs.

## 5. Jobs

**A, core.** Package `internal/acceptance` (it does not import the Automaton engine, which keeps rule 4 true by construction): list, ask, freeze, revisions, command and state checks, run keys, roll-up, verdict store, weak-list flags, waiting, needed-by, grouping, counters, defaults as data, report-only checks I18 to I20, the four actions on REST, MCP, CLI, chat and web, config, docs. **Wiring that creates, freezes and evaluates lists from real requests** waits for the request store (job 3) and lands with the request sources (job 5).

**B, drafting and reviewer.** Planner and drafter items, list check inside plan review, a reviewer datawatch starts (never the holder, its child or an editor), every item answered, quote and cut-input checks (rule 10), the uncovered-ask question, stored verdicts, usage waiting, rework cap. Done includes a replay of known past false "done" cases: none may read met.

**Standing checks (report only).** I18: every request started after the start mark had a frozen list before its first notice or spawn. I19: every done has, per item, met with evidence for the current version, or an acceptance; also against revision 1. I20: every later change is recorded; one without words is shown. They block nothing until the operator decides after the first real report.

**After unpause:** plugin check methods, eval suites and guardrail profiles as methods, peer item results, template and type default editing, cross-item verdict reuse, bulk drafting, a polished editor, app screens.

## 6. Open choices (operator)

In every option, who is allowed is not enforced until 11.0: any session can release, change or accept, and datawatch records it.

**Choice 2. Which lists wait for approval before work starts?** It does not approve the spending or deleting itself.
- A: only lists with a serious gap (uncovered ask, no usable check, list not reviewed), or work that deletes, deploys, changes a live system, spends or sends outside. Where a plan is already approved today, the list is shown in that step. A vague-word warning or a planner-written command alone adds no wait. A direct operator instruction with only defaults counts as approval. Risk: a plausible wrong item or a mislabelled risk starts unseen.
- B: every list waits. Fewest wrong starts; the queue stops on the operator even for small requests.
- C: none wait; flags are only shown. Work can be built to a poor list and redone.
- **Recommend A.** Holds back: the start gate.

**Choice 3. Do open items also stop "completed" or merging?** The request stays open in every option.
- A: no; show "finished, not accepted". Merge rules unchanged. Unmet work can merge and "completed" can mislead. Needs rule 16.
- B: no completed or merge until all before-merge items are met. Safest; a new checker can stall the queue, above all before B lands.
- C: stop only merging, only on "not met". Unchecked work can still merge.
- **Recommend A**, and look again after the first real report. Holds back: status and merge wiring.

**Choice 4. The unfinished records** (224 on 2026-11-25; 59 have some "done when" text, 165 have none; presence was counted, not quality).
- A: make the list when each is released, from the whole record; old text is only a draft (rule 6).
- B: draft all now: up to 224 drafting runs plus reading, and lists go stale.
- C: run them marked "no criteria", never "accepted". Most current work stays unchecked.
- **Recommend A.** Holds back: releasing old work. Nothing to build waits.

**Choice 5. A list changed after work began, and the new version passes. What does it read?**
- A: "met on a changed list", not plain done, grouped on the operator's list until seen.
- B: done, with a "list changed" label. No wait; a weakened list is easy to miss.
- C: as A only when an item was removed or altered, or something was accepted unproven; added items alone read done. An added "or" hidden in a new item must count as altering.
- **Recommend C.** Holds back: the final result for a changed list. Until answered, build A reports "met on a changed list", which is **not** done, and says the choice is open.

## 7. To confirm (readings built in the strict direction; no call is refused)

1. A confirmation or acceptance from the holder's own session is shown as its claim and does not count.
2. The rework cap is 2 rounds per list.
3. Job 4 must record the observed caller, or receipt items read "cannot tell". Whether job 4 **refuses** a non-holder is for the supervisor to settle.

## Parity surface

| Surface | Build A |
|---|---|
| REST | list, view, edit, confirm, accept, report, catalog |
| MCP | matching tools, registered only when the setting is not `off` |
| CLI | `datawatch acceptance …`, through the daemon only |
| comm channel | `acceptance` show, confirm, accept, report |
| YAML/config | `acceptance.*`. Project checks and the defaults file are **file-only**, because no route may accept command text or swap in weaker defaults |
| PWA | "Done when" table on the Automaton page and a list view, five locales |
| Android | parity draft filed; screens after unpause |
| iPhone/iOS | parity draft filed; screens after unpause |

Excluded: federation fan-out (peer item results are after unpause; work sent to another host is refused clearly until then).
