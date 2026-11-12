# [sync] Automation merged-member policy and decision view

Issue number: #310
Issue URL: https://github.com/dmz006/datawatch-app/issues/310
Filed: 2026-10-30, via `gh issue create -R dmz006/datawatch-app`.

The issue title is the heading of this file. The text below, from "Parent release" through the end of "Acceptance (mobile)", is the full body that was filed.

---

Parent release: datawatch next release

## What changed in datawatch

When a member of an Automation ends cancelled or completed, but its Automaton work branch (`automaton/<id>`) is already merged into the project's own default branch, the Automation used to stay blocked although the work was on the default branch. The daemon now proves the merge with read-only git queries and applies an operator-chosen policy. Every such decision is recorded in the Automation's decision log, and a new read route lists the decisions. A branch that is not merged, or whose state cannot be read, always keeps the blocked outcome, whatever the policy.

## New config key

The key sits under `autonomous.automation` in the daemon config. It is returned by `GET /api/config` (the effective value) and accepted by `PUT /api/config` as `autonomous.automation.merged_member_policy`, and by the Automata config object as `merged_member_policy`. A change applies to the next member that ends, with no restart.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `merged_member_policy` | string: `completed`, `manual`, `block` | `completed` | `completed` treats the member as completed, so the Automation follows its on-complete edge. `manual` keeps the blocked outcome and adds the merge proof to the block reason, so the operator resolves it with resume or skip. `block` keeps the blocked outcome and runs no git check at all. |

- A key that is absent from the config reads as `completed`.
- Over the wire an empty string is not accepted: `PUT /api/config` and the Automata config object refuse an empty value, and any value outside the three words, with a 400, and nothing is written. The app must send one of the three words.

## Changed REST contract

Two new read-only routes. Only `GET` is served (any other method is a 405), a federated peer needs the autonomous read capability (403 without it), and while Automata are disabled both return 503.

- `GET /api/automations/merged-members` lists the decisions of every Automaton.
- `GET /api/automations/{id}/merged-members` lists the decisions of one Automaton. An unknown id is a 404; an Automaton with no such decision returns an empty list.

Both return the same object, oldest decision first:

```json
{
  "automation_id": "string (the requested id, empty for the all-Automata route)",
  "count": 0,
  "merged_members": [
    {
      "automation_id": "string",
      "automation_name": "string (omitted when empty)",
      "member": "string (the member id within the Automaton)",
      "prd_id": "string (the member's work record id, omitted when empty)",
      "branch": "string (the member work branch)",
      "default_branch": "string (the project default branch the member was proved merged into)",
      "proving_commit": "string (the commit that proves the merge)",
      "policy": "completed | manual | block (the policy in force)",
      "outcome": "advanced | blocked",
      "at": "RFC 3339 time",
      "note": "string (omitted when empty)"
    }
  ]
}
```

- `outcome` is `advanced` when the Automation moved on past the member, and `blocked` when it stayed blocked for the operator. A client must show an unknown `outcome` value as written instead of failing.
- `merged_members` is always an array (never `null`), and `count` is its length.

## Automaton JSON field changes

The Automaton object gains no new field. Its existing `decisions` array can now hold an entry with `kind` set to `merged_member`. That entry carries the usual `at`, `actor` and `note`, plus one optional object `merged_member` with `member`, `prd_id` (omitted when empty), `branch`, `default_branch`, `commit`, `policy` and `outcome` (the same values as the route; the proving commit is `commit` here and `proving_commit` on the route). A client that lists decisions must tolerate an entry of this kind.

## PWA behaviour to match

### Settings

The Automaton settings screen (Settings, General, Automate) has one new field, placed after the two merge-pending fields: **Automaton merged-member policy**, a select of `completed`, `manual` and `block`, default `completed`. It reads the effective value from the daemon config and writes it back through the config endpoint. The PWA also registers a new docs-link entry, key `merged_member_policy`, pointing at the definitions page anchor `#merged-member-policy`; add the same entry to the app's `DocsLinks` map (the PWA's `docsLinkForKey` mirrors it).

### Merged-member decisions on the Automaton detail

- The Automation detail view has a new block, titled **Automaton merged-member decisions**, below the progress strip and above the pending inserts and the decision log. It is filled from `GET /api/automations/{id}/merged-members` each time the detail view is drawn.
- The block lists the decisions newest first (the route returns oldest first, so the PWA reverses them). Each row shows the member, the branch, the first 12 characters of the proving commit, the policy, the outcome (**Automaton advanced** or **Automaton blocked**) and the time.
- No rows draws nothing at all, not even the title. A failed read leaves the block empty and shows no error.
- In the decision log of the same view, an entry of kind `merged_member` that carries the `merged_member` object is drawn as one line: "Automaton member `<member>` on `<default_branch>`, proved merged at `<12-char commit>`: `<outcome text>`". Other decision kinds are unchanged.

## New locale keys

| Key | EN |
|---|---|
| `settings_automation_merged_member_policy` | Automaton merged-member policy (completed, manual or block; default completed) |
| `automation_merged_members_title` | Automaton merged-member decisions |
| `automation_merged_member_member` | Automaton member |
| `automation_merged_member_branch` | Automaton branch |
| `automation_merged_member_commit` | Automaton proving commit |
| `automation_merged_member_policy` | Automaton policy |
| `automation_merged_member_outcome` | Automaton outcome |
| `automation_merged_member_outcome_advanced` | Automaton advanced |
| `automation_merged_member_outcome_blocked` | Automaton blocked |
| `automation_decision_merged_member` | Automaton member %1$s on %2$s, proved merged at %3$s: %4$s |

Every string says Automaton or Automata. The DE, ES, FR and JA bundles carry the English text as a placeholder until translations arrive; please send translations of these 10 keys for DE, ES, FR and JA through the Compose Multiplatform pipeline, using the app's own word for "Automaton", and the PWA bundles will then mirror them.

## Other surfaces (for context, no app work)

The same decisions are on MCP (`automation_merged_members`, optional `id`), the CLI (`datawatch autonomous merged-members [automation-id]`) and the chat channels (`autonomous merged-members [automation-id]`, newest first, at most ten lines). When the Automation advances on a merged member, one info alert is raised named "Automaton advanced on a merged member". The policy key is settable on PUT `/api/config`, CLI `datawatch config set`, chat `configure`, and the MCP `autonomous_config_set` tool.

## Where to look in datawatch

- `docs/api/openapi.yaml`: paths `/api/automations/merged-members` and `/api/automations/{id}/merged-members`, schemas `MergedMemberList` and `MergedMemberRow`.
- `docs/config-reference.yaml`: `merged_member_policy` under `autonomous.automation`.
- `docs/datawatch-definitions.md`: *Merged-member policy* under Settings, Automate, and *Merged member* in the glossary.
- `internal/server/web/app.js`: `automationMergedMembersHTML`, `loadAutomationMergedMembers`, and the `autonomous.automation.merged_member_policy` entry of `GENERAL_CONFIG_FIELDS`.
- `internal/server/web/locales/en.json`: the 10 keys above.

## Acceptance (mobile)

- The Automaton settings screen shows the merged-member policy select with the three values, reads the current value from the daemon config, writes it back through the config endpoint, and shows the default `completed` when the key is absent.
- The Automaton detail view lists the merged-member decisions from `GET /api/automations/{id}/merged-members`, newest first, with member, branch, shortened proving commit, policy, outcome and time, and draws nothing when there are none.
- An unknown `outcome` value, or an unknown decision `kind` in the decision log, is shown as written and never breaks the view.
- A `merged_member` entry in the decision log reads as the one-line text above.
- All 10 locale keys exist in EN, DE, ES, FR and JA, with the English text as the placeholder in DE, ES, FR and JA until translations land.
