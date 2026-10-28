# The 10.0.0 arc as a draft Automation

**Date:** 2026-10-28
**Version:** none (planning document; plan-document commits are exempt from the version bump)
**Tracker:** BL431 / BL609 follow-on (Automation use by the supervisor)

Operator, 2026-10-28, quoted: "Yes, draft the 10.0.0 arc as an Automation. We should use automation as much as we can, in chance to debug and make it best, see if there are any suggestions to make it better". Earlier the same day the operator asked how Automations were being used; the answer was that none existed and the supervisor did the Automation's job by hand (ordering, lane fill, merge, deploy).

## What was built (drafts, never activated)

| Automation | Id | Shape |
|---|---|---|
| 10.0.0 arc | `0ff316f1` | nests the two below: open work, then the final stage |
| 10.0.0 open features and defects | `cd2204c0` | fork of BL598 (`58aa0001`) and BL622 (`db4b0001`), joined (all must succeed) |
| 10.0.0 build tail (final stage) | `aba71c4a` | BL411 `7c960001` -> BL409 `7e460001` -> BL637 `04b20001` -> BL424 `7f3e0001` -> BL496 `80660001` |

Four held placeholder PRDs were filed to give the final-stage items something to point at (BL411, BL409, BL424, BL496); each spec is the registry text, quoted, plus a HOLD line. BL424's earlier PRD `0e2ac398` was found cancelled while registry row 46 still called it "approved, held to last"; the new placeholder replaces it.

## What the model did not fit (suggestions)

1. **A member must be an existing PRD.** Ten registry rows of the arc (BL620, BL556, BL412, BL414, BL422 follow-up, BL473, BL483, upstream monitoring, BL609 steps 9 to 11, BL610 steps 2 to 5) have no PRD, so they are not in the graph, and the arc would advance to the final stage while they are still open. Suggestion: a "pending item" member (registry row id, no PRD yet) that blocks its successors until a PRD is attached and finished, or a one-click "file a draft PRD from this registry row" so every row can be a member.
2. **Hold is prose.** The only machine-visible hold is that a draft member makes the Automation `blocked` with "next member is draft" (`startGate`). That works but reads as a fault. Suggestion: an explicit operator-hold state on a member (shown as "waiting for the operator", with who and why), distinct from blocked.
3. **"Done" means tasks done, not merged and shipped.** The next member would start from a main that lacks the previous one's branch. Suggestion: a merge-and-ship step between members (this is the supervisor's merge procedure and BL609 steps 9 to 11), with the version bump and registry update as part of it.
4. **Budget policy does not read the usage calculator.** `set-budget` takes a dollar or allocation-percent ceiling per window; the calculator (v9.29.0) holds the weekly reading, the pace line and the policy. Suggestion: let the budget policy take its ceiling and pacing from the calculator reading so the arc paces to the week by itself.
5. **Lane fill and decompose-ahead stay manual.** The Automation starts the next member; it does not fill an idle lane from the wait list or decompose the member after next (BL473, BL514).
6. **A running PRD as a member is untested.** BL628 (`f3420001`) was left out because it was already running; adopting in-flight work needs a test.

## Next steps (operator decides)

Activation is not requested. Items 1 to 3 are the ones that decide whether the supervisor's manual work can move into the Automation.
