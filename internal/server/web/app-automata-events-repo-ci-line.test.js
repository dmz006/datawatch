// Automata view — the repo CI status line is no longer rendered under the
// sub-tab bar.
//
// The repo CI watch line used to sit above the Automata sub-tab bar on every
// render. It is gone: when the watch is off nothing is shown there, and its
// failing/pending runs and never-polled state arrive as Events entries and
// alerts from the backend. These checks read the renderAutonomousView source
// in app.js and pin that the sub-tab bar carries no repo CI line element and
// that the view no longer starts the repo CI poll.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');

const appSource = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');

function renderAutonomousViewSource() {
  const start = appSource.indexOf('function renderAutonomousView()');
  const end = appSource.indexOf('window.renderAutonomousView = renderAutonomousView;', start);
  assert.ok(start >= 0 && end > start, 'renderAutonomousView is present in app.js');
  return appSource.slice(start, end);
}

function subTabBarSource() {
  const view = renderAutonomousViewSource();
  const start = view.indexOf('<div class="output-tabs automata-tabs-bar">');
  assert.ok(start >= 0, 'automata sub-tab bar is present in renderAutonomousView');
  return view.slice(start, view.indexOf('${isAutomation ? _automationBodyHTML()', start));
}

test('the sub-tab bar under Automata no longer renders the repo CI status line', () => {
  const bar = subTabBarSource();
  assert.ok(!bar.includes('repoCIWatchStatusLine'), 'no repo CI status line element in the sub-tab bar');
  assert.ok(!/repo[-_ ]?ci/i.test(bar), 'no repo CI text in the sub-tab bar');
});

test('renderAutonomousView draws no repo CI status line element at all', () => {
  const view = renderAutonomousViewSource();
  assert.ok(!view.includes('repoCIWatchStatusLine'), 'no repoCIWatchStatusLine element is written by the view');
  assert.ok(!view.includes('REPO_CI_WATCH_STATUS_ELEMENT_ID'), 'the view does not reference the status line element id');
});

test('renderAutonomousView does not start the repo CI status poll', () => {
  const view = renderAutonomousViewSource();
  assert.ok(!view.includes('repoCIWatchStartPolling('), 'the view no longer calls repoCIWatchStartPolling');
});

test('the Events sub-tab still sits in the bar after the line is removed', () => {
  const tabs = [...subTabBarSource().matchAll(/data-tab="([a-z]+)"/g)].map(m => m[1]);
  assert.deepEqual(tabs, ['automation', 'automata', 'events', 'templates']);
});
