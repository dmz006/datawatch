# Projects Overview sub-tabs do not respond to clicks — investigation

Date: 2026-11-13. Investigation only; no code changed.

## Symptom
On Projects with Overview selected, the second-row sub-tab bar (Inbox, Automations,
Resources, Progress, Housekeeping) is drawn but clicking a button changes nothing.

## Root cause
`_projectsPaint` (internal/server/web/app.js, Overview branch) draws
`_automationBodyHTML()`, which includes `_automationSubtabsBarHTML()`. Each button has
`onclick="switchAutomationSubtab('<id>')"`.

`switchAutomationSubtab` (app.js, near the `_tabStripReveal` helpers) starts with:

    if (_automataNormalizeTab() !== 'automation') return;

On Projects the Automata tab is `projects`, so the handler returns before it stores the
sub-tab or redraws. Nothing else runs: no state change, no repaint, no error.

It is not a missing handler, a wrong container lookup, a refresh re-render, a shared id
or class, or an overlay. The handler exists and is reached; the guard stops it.

## Reproduction (existing harness: testutil_browser_stub.js, same loader as app-projects-subtabs.test.js)
Rendered Projects Overview (`_automataState.tab = 'projects'`, selected `''`), then called
the handler each button's onclick names:

- Bar present, buttons: inbox, automations, resources, progress, housekeeping.
- For each id: `_automationState.subtab` stays `inbox`, `renderAutonomousView` calls = 0.
- Bypassing the guard (set `_automationState.subtab = 'progress'`, call
  `renderAutonomousView()`) works: the Progress body is drawn, because
  `renderAutonomousView` already routes `st.tab === 'projects'` to `renderProjectsView()`
  (app.js ~42999), which repaints Overview from `_automationState.subtab`.
- Projects detail strip (`.projects-subtabs`, `selectProjectsSubtab`) is fine:
  clicking Plans switches the body to `plans`. It uses `_projectsState.subtab` and
  `_projectsPaint()` with no tab guard.

## Other defects on the same path
- Even without the guard, `switchAutomationSubtab` ends with `_automataSyncActions('automation')`,
  which shows the filter/history buttons for the Automation tab, not Projects.
- `prev.stop()` on the left sub-tab is correct and should be kept.
- Existing tests (app-automation-subtabs.test.js) run with tab `automation`, and
  app-projects-subtabs.test.js only covers the project detail strip, so no test clicks the
  Overview bar.

## Phone-width rules
- `.automation-subtabs .output-tab`: `@media (max-width: 600px), (pointer: coarse)` sets
  `min-height: 40px`; the strip scrolls sideways (style.css ~848-880), with edge fades.
- `.projects-subtabs`: no media query; `overflow-x: auto; white-space: nowrap`, buttons
  `flex-shrink: 0` (style.css ~7586). Fine on phone width; no 40 px tap rule.
- Neither rule affects click handling.

## Proposed fix
1. In `switchAutomationSubtab`, accept both tabs that host the bar:

       const tab = _automataNormalizeTab();
       if (tab !== 'automation' && !_projectsActive()) return;
       ...
       _automationState.subtab = id;
       renderAutonomousView();
       if (tab === 'automation') _automataSyncActions('automation');

   `_projectsActive()` is already true only on Projects, so the Overview-only condition can
   be tightened to `_projectsActive() && _projectsState.selected === _PROJECTS_OVERVIEW`.
2. Guard `_projectsActive()` against a project (non-Overview) page: the bar is only drawn on
   Overview, so no other change is needed.
3. Add a test in app-projects-subtabs.test.js: open Projects Overview, click each of the five
   bar buttons through the stub, assert `_automationState.subtab` changes and the painted
   body matches, and that `_automataSyncActions` is not called with `automation`.
4. Optional: give `.projects-subtabs button` the same 40 px phone tap height.
