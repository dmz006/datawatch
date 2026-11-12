# [sync] Automation view reorganised: header New Automation button removed, Overview and Chains sub-tabs (datawatch next release)

Parent release: datawatch next release

## What changed in datawatch

The Automation sub-tab of the Automata view is reorganised into two second-row sub-tabs, and its duplicated header create button is gone:

- **Header New Automation button removed.** The **New Automation** button in the sub-tab bar header (shown only on the Automation sub-tab) no longer exists. Creating an Automation is unchanged otherwise: the round create button at the bottom right and the **＋ Create Automation** button on the empty state both open the same form.
- **Overview sub-tab.** Carries the summary cards: the Progress card, the Capacity and queue card (pools, usage back-off, budgets, Automata waiting for capacity), and the summary cards that sit with them.
- **Chains sub-tab (new and default).** Carries the needs-attention card, the status badges, the search and filter bar, and the list of Automations; opening an Automation lands here, and its pipeline strip and control buttons stay with it. **Chains** is the sub-tab that opens by default, and saved links (`#automation`, `#automation/<id>`, `?automation=<id>`) land on it.
- **Questions later.** A **Questions** sub-tab will join the second row later, when the question queue lands; no Questions sub-tab exists today.
- **No API change.** The change is PWA layout and labels only: the daemon endpoints the Automation view already uses are unchanged, so nothing is added to or removed from any request or response.

## New locale keys

| Key | EN |
|---|---|
| `automation_subtab_overview` | Overview |
| `automation_subtab_chains` | Chains |

The DE, ES, FR and JA bundles carry the English text as a placeholder until translations arrive through the localization pipeline; please send translations of both keys for DE, ES, FR and JA, and the PWA bundles will then mirror them in place of the placeholders.

## Where the keys are used

- `internal/server/web/app.js`: the `labelKey` of each entry of the `_AUTOMATION_SUBTABS` registry (`automation_subtab_overview` for the Overview entry, `automation_subtab_chains` for the Chains entry), rendered as the two buttons of the second-row sub-tab bar through `t(s.labelKey)`; `_AUTOMATION_DEFAULT_SUBTAB` is `chains`.
- `internal/server/web/locales/{en,de,es,fr,ja}.json`: one entry per key in each of the five bundles.
- `internal/server/web/app-automation-locales.test.js`: asserts both keys exist in every bundle, with the English values, and lists the two keys as pending translation.

## Acceptance

- The app's Automation screen drops the header **New Automation** button; creation stays with the round create button and the empty-state **Create Automation** button.
- The Automation screen has the two second-row sub-tabs **Overview** (the summary cards: Progress, Capacity and queue, and the summary cards that sit with them) and **Chains** (the needs-attention card, the badges, the search bar and the Automation list), and **Chains** is the one open by default.
- Saved and shared links to the Automation screen land on **Chains**.
- The two sub-tab labels are localized in EN, DE, ES, FR and JA; DE, ES, FR and JA carry the English placeholder until translations land.
- No **Questions** sub-tab yet, and no dependency on any new daemon field: the daemon contract is unchanged.

## Filing record

Filed: https://github.com/dmz006/datawatch-app/issues/298
