# Projects Overview: the Automation sub-tab buttons do nothing

Date: 2026-11-13. Scope: investigation only; no code changed.

## Verdict

The sub-tab buttons on Projects Overview (Inbox, Automations, Resources, Progress, Housekeeping) are the
Automation page's buttons, drawn inside the Projects page. Clicking one runs `switchAutomationSubtab(id)`
(`internal/server/web/app.js`, near line 44115), which returns without doing anything.

### Root cause (two stacked defects, the first one is the visible one)

1. **Early-return guard.** `switchAutomationSubtab` begins with
   `if (_automataNormalizeTab() !== 'automation') return;`. On Projects Overview `_automataState.tab` is
   `'projects'`, so the function exits before it changes `_automationState.subtab`. Nothing is stored,
   nothing is repainted.
2. **Wrong repaint target (hidden behind the guard).** Past the guard the function calls
   `renderAutonomousView()`, which rewrites `#view` and builds the Automata tab bar. The Projects page paints
   into `#automataPanel` (`_projectsPaint`, app.js line 3188, `document.getElementById('automataPanel')`).
   So removing only the guard would draw a second Automata page in the wrong container and leave the Projects
   Overview unchanged. The handler never calls `_projectsPaint()`.

Not the cause (checked): there is no missing handler (the inline `onclick="switchAutomationSubtab('<id>')"`
resolves); no id or class clash (`.automation-subtabs` is used only by `_automationSubtabsBarHTML`;
`.projects-subtabs` / `data-projects-subtab` / `selectProjectsSubtab` belong to the project detail strip and are
a separate set); no overlay (the bar is `position: sticky; z-index: 10`, and the same bar works on the
Automation tab); no refresh re-render (the click handler itself is a no-op, and a repaint would keep
`_automationState.subtab` at its old value anyway).

### Code path

- `_projectsPaint`, Overview branch (app.js ~3205-3210): `view.innerHTML = '<div id="projectsOverviewBody">' +
  _automationBodyHTML() + '</div>'; _automationMount(); view.insertAdjacentHTML('afterbegin', picker + '</div>')`.
  The extra `'</div>'` passed to `insertAdjacentHTML` is also stray (it closes nothing). Cosmetic, same fix pass.
- `_automationBodyHTML` (~44034) = `_automationSubtabsBarHTML()` + the selected sub-tab's `bodyHTML()`.
- `_automationSubtabsBarHTML` (~44023): `<div class="output-tabs automation-subtabs">` with one
  `<button data-subtab=ID onclick="switchAutomationSubtab('ID')">` per registry entry.
- `_automationMount` (~44047) starts only the loaders of the current sub-tab (`def.mount()`); `switchAutomationSubtab`
  calls `prev.stop()` then `def.mount()` happens via the repaint. Container lookups: the sub-tab bodies find their
  cards by element id (`automationTriageCard` etc.), and `_automationUpdateInboxBadge` / `_automationSubtabsReveal`
  use `document.querySelector('.automation-subtabs ...')`. Those work in any container, so they are not a blocker.
- The Projects detail strip is separate: `_projectsSubtabStripHTML` (~3545) emits `.projects-subtabs` buttons that
  call `selectProjectsSubtab(k)` (~3550), which sets `_projectsState.subtab` and calls `_projectsPaint()`. It is
  not drawn on Overview, so it is not involved there.

## Reproduction

Scratch script (outside the repo) using `testutil_browser_stub.js`: set `_automataState.tab='projects'`,
`state.activeView='autonomous'`, a project list, `_projectsState.selected = _PROJECTS_OVERVIEW`, call
`_projectsPaint()`, then call `switchAutomationSubtab(id)` for each button (the same call the `onclick` makes).

```
painted into automataPanel: 3504 chars; has subtab bar: true
automations subtab before/after: inbox inbox   tab: projects  view repainted: false
resources   subtab before/after: inbox inbox   tab: projects  view repainted: false
progress    subtab before/after: inbox inbox   tab: projects  view repainted: false
housekeeping subtab before/after: inbox inbox  tab: projects  view repainted: false
```

Every click leaves `_automationState.subtab` on `inbox`, leaves the tab on `projects`, and paints nothing.
Handler that runs for all four: `switchAutomationSubtab`, exiting at the `!== 'automation'` guard.

## Phone-width rules (style.css)

- `.automation-subtabs` (~848): flex, `nowrap`, `overflow-x: auto`, scrollbar hidden, sticky top, `max-width: 100%`;
  `@media (max-width: 600px), (pointer: coarse)` (~876) gives its `.output-tab` a 40 px min-height. Tabs keep natural
  width; the strip scrolls sideways.
- `.projects-subtabs` (~7586-7588): flex, `overflow-x: auto`, `white-space: nowrap`, buttons `flex-shrink: 0`. No media
  query of its own; it relies on the horizontal scroll at every width.
- Neither rule hides or blocks taps on phone, so the failure is the same at every width.

## Proposed fix

In `switchAutomationSubtab`, handle the Projects Overview host before the guard:

```js
function switchAutomationSubtab(id) {
  _automationNormalizeSubtab();
  const def = _AUTOMATION_SUBTABS.find(s => s.id === id);
  if (!def) return;
  const onProjectsOverview = typeof _projectsActive === 'function' && _projectsActive()
    && _projectsState.selected === _PROJECTS_OVERVIEW;
  if (!onProjectsOverview && _automataNormalizeTab() !== 'automation') return;
  const prev = _AUTOMATION_SUBTABS.find(s => s.id === _automationState.subtab);
  if (prev && prev !== def) prev.stop();
  _automationState.subtab = id;
  if (onProjectsOverview) { _projectsPaint(); return; }   // repaint #automataPanel, not #view
  renderAutonomousView();
  _automataSyncActions('automation');
}
```

Also drop the stray `'</div>'` in the Overview `insertAdjacentHTML` call. Add a node test next to
`app-projects-subtabs.test.js`: paint Overview, click each `data-subtab` button, assert
`_automationState.subtab` changes, the painted body switches, `_automataState.tab` stays `projects`, and the
`.automation-subtabs` bar is present exactly once. Keep `app-automation-subtabs.test.js` unchanged; it must
still pass. A PWA change needs the usual datawatch-app parity issue.
