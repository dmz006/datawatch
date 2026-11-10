// The Automation view's second-row sub-tabs: Inbox, Automations, Resources,
// Progress, Setup and Housekeeping.
//
// The first render of the Automation sub-tab is on Inbox with exactly six
// second-row buttons in that order, switching moves only the body, the choice
// is kept while the page stays open (a fresh load starts on Inbox,
// localStorage is never touched), a bare saved link lands on Inbox while a
// link to one Automation lands on Automations, no Questions sub-tab exists
// (the question queue is a card on Inbox), the header New Automation button is
// gone while the round create button and the empty-state Create button remain,
// the filter and history buttons show on Automations only, and the bar renders
// one button per registry entry.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

function loadView() {
  const els = {};
  const out = { view: '' };
  els.view = makeStubElement(h => { out.view = h; });
  const sandbox = buildSandbox({ byId: els });
  sandbox.URLSearchParams = URLSearchParams; // app.js parses ?automation=<id> with it
  sandbox.fetch = () => Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve([]), text: () => Promise.resolve('[]') });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  // t() falls back to the raw key until a bundle loads; install the English
  // one so the rendered strings are the real user-visible ones.
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return { sb: sandbox, out, els };
}

const SUBTABS = ['inbox', 'automations', 'resources', 'progress', 'setup', 'housekeeping'];
const subtabOf = sb => vm.runInContext('_automationState.subtab', sb);

// subtabButtons lists the second-row buttons in render order. The outer
// sub-tab bar of the Automata view uses data-tab; only the second row uses
// data-subtab, so this cannot pick up the other bar.
function subtabButtons(html) {
  return [...html.matchAll(/class="output-tab( active)? ?" role="tab" aria-selected="(true|false)" data-subtab="([a-z]+)"/g)]
    .map(m => ({ id: m[3], active: !!m[1], selected: m[2] === 'true' }));
}

test('default: a first render of the Automation sub-tab is Inbox, and the row is exactly the six sub-tabs in order', () => {
  const { sb, out } = loadView();
  assert.equal(subtabOf(sb), 'inbox', 'module state starts on Inbox');
  sb.renderAutonomousView();
  const row = subtabButtons(out.view);
  assert.deepEqual(row.map(b => b.id), SUBTABS, 'the second row has exactly six buttons in order');
  row.forEach((b, i) => {
    assert.equal(b.active, i === 0, b.id + (i === 0 ? ' is active' : ' is not active'));
    assert.equal(b.selected, i === 0, b.id + ' aria-selected matches');
  });
});

// Cards each sub-tab body owns, by element id.
const OWNED = {
  inbox: ['automationTriageCard', 'automationQuestionsCard', 'automationHeldQuestionsCard', 'automationSecurityFailureCard'],
  automations: ['automationFilterBar', 'automationPanel', 'automationReleaseArcCard', 'automationGanttCard'],
  resources: ['automationCapacityCard', 'automationLaneFillCard', 'automationUsageCard'],
  progress: ['progressCard'],
  setup: ['automationOnboardingReportCard', 'automationMergePipelineCard', 'automationRequestWatchCard'],
  housekeeping: ['automationUncommittedWorkCard'],
};

test('switching changes the body only: each sub-tab holds its own cards and none of the others, an unknown id changes nothing', () => {
  const { sb, out } = loadView();
  for (const id of SUBTABS) {
    sb.switchAutomationSubtab(id);
    assert.equal(subtabOf(sb), id);
    const row = subtabButtons(out.view);
    for (const b of row) assert.equal(b.active, b.id === id, b.id + ' active state while ' + id + ' is selected');
    for (const [owner, ids] of Object.entries(OWNED)) {
      for (const el of ids) {
        const re = new RegExp('id="' + el + '"');
        if (owner === id) assert.match(out.view, re, id + ' body holds ' + el);
        else assert.doesNotMatch(out.view, re, id + ' body holds no ' + el);
      }
    }
  }

  sb.switchAutomationSubtab('automations');
  const before = out.view;
  sb.switchAutomationSubtab('no-such-subtab');
  assert.equal(subtabOf(sb), 'automations', 'an unknown id is ignored');
  assert.equal(out.view, before, 'an unknown id neither re-renders nor touches the state');
  // The retired ids are unknown now.
  sb.switchAutomationSubtab('overview');
  sb.switchAutomationSubtab('chains');
  assert.equal(subtabOf(sb), 'automations', 'the old overview and chains ids are ignored');
});

test('the chosen sub-tab is kept while the page stays open; a fresh load starts on Inbox and never writes localStorage', () => {
  const { sb } = loadView();
  const writes = [];
  const origSet = sb.localStorage.setItem;
  sb.localStorage.setItem = (k, v) => { writes.push([k, String(v)]); return origSet(k, v); };
  sb.switchAutomationSubtab('resources');
  assert.deepEqual(writes, [], 'switching the second-row sub-tab never writes localStorage');
  sb.switchAutomataTab('automata');
  sb.switchAutomataTab('automation');
  assert.equal(subtabOf(sb), 'resources', 'leaving to the Automata sub-tab and returning keeps Resources');
  sb.switchAutomationSubtab('resources');
  assert.deepEqual(writes, [], 'coming back to the remembered Resources writes no localStorage either');
  const fresh = loadView();
  assert.equal(subtabOf(fresh.sb), 'inbox', 'a freshly loaded sandbox starts on Inbox');
});

test('deep links: a bare link lands on Inbox, a link to one Automation lands on Automations, even after choosing another sub-tab', () => {
  const { sb } = loadView();
  // The detail load is not the point of this test; let it fail cleanly.
  sb.fetch = url => (String(url).startsWith('/api/automations/a1')
    ? Promise.resolve({ ok: false, status: 500, statusText: 'x', text: () => Promise.resolve('down') })
    : Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve([]), text: () => Promise.resolve('[]') }));
  assert.equal(subtabOf(sb), 'inbox');
  sb.switchAutomationSubtab('resources');
  assert.equal(subtabOf(sb), 'resources');
  sb.navigate('automation');
  assert.equal(subtabOf(sb), 'inbox', "navigate('automation') lands on Inbox");

  // The saved-link parser hands '' for the bare list and the id for one Automation.
  assert.equal(sb._automationRouteFromLocation({ hash: '#automation', search: '' }), '', '#automation parses to the list');
  assert.equal(sb._automationRouteFromLocation({ hash: '#automation/a1', search: '' }), 'a1', '#automation/a1 parses to the Automation');
  assert.equal(sb._automationRouteFromLocation({ hash: '', search: '?automation=a1' }), 'a1', '?automation=a1 parses to the Automation');

  for (const [loc, want] of [
    [{ hash: '#automation', search: '' }, 'inbox'],
    [{ hash: '#automation/a1', search: '' }, 'automations'],
    [{ hash: '', search: '?automation=a1' }, 'automations'],
  ]) {
    sb.switchAutomationSubtab('resources');
    assert.equal(subtabOf(sb), 'resources', 'Resources chosen again before the link');
    sb.openAutomationLink(sb._automationRouteFromLocation(loc));
    assert.equal(subtabOf(sb), want, JSON.stringify(loc) + ' leaves the view on ' + want);
  }
});

test('there is no Questions sub-tab: not in the rendered row, not in the registry, not in en.json', () => {
  const { sb, out } = loadView();
  sb.renderAutonomousView();
  // The question queue and the held questions are cards on Inbox, not
  // sub-tabs: drop those cards' markup (each ends at the next card) before
  // checking the view.
  let viewNoCard = out.view;
  viewNoCard = viewNoCard.replace(/<div id="automationQuestionsCard"[\s\S]*?(?=<div id="automationSecurityFailureCard")/, '');
  assert.doesNotMatch(viewNoCard, /automationQuestionsCard|automationHeldQuestionsCard/, 'the question cards were removed from the copy');
  assert.doesNotMatch(viewNoCard, /question/i, 'the rendered view names no Questions sub-tab');
  const bar = sb._automationSubtabsBarHTML();
  assert.doesNotMatch(bar, /question/i, 'the second-row bar names no Questions button');
  const reg = vm.runInContext('_AUTOMATION_SUBTABS', sb);
  assert.ok(reg.length >= 1, 'the registry has entries');
  for (const entry of reg) assert.doesNotMatch(String(entry.id) + ' ' + String(entry.labelKey), /question/i, 'registry entry ' + entry.id);
  assert.doesNotMatch(reg.map(e => e.labelKey).join(' '), /question/i);
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  assert.ok(!('automation_subtab_questions' in en), 'en.json has no automation_subtab_questions key');
  assert.ok(!Object.keys(en).some(k => /^automation_subtab/.test(k) && /question/i.test(k)), 'no automation_subtab_* key in en.json names Questions');
});

test('the header New Automation button is gone: no automationNewBtn on any sub-tab', () => {
  const { sb, out } = loadView();
  sb.renderAutonomousView();
  assert.doesNotMatch(out.view, /automationNewBtn/, 'no header button on the Automation sub-tab');
  sb.switchAutomataTab('automata');
  assert.doesNotMatch(out.view, /automationNewBtn/, 'no header button on the Automata sub-tab');
  sb.switchAutomataTab('templates');
  assert.doesNotMatch(out.view, /automationNewBtn/, 'no header button on the Templates sub-tab');
});

test('creating stays available: the round button opens the Automation form and the empty state carries its create button', () => {
  const { sb, els } = loadView();
  const fab = els.newSessionFab = makeStubElement();
  sb.switchAutomataTab('automation');
  assert.equal(fab.onclick.toString().includes('openAutomationForm'), true, 'the floating button opens the Automation form on the Automation tab');
  assert.equal(fab.title, 'New Automation');
  const html = sb.automationEmptyStateHtml();
  assert.match(html, /id="automationEmptyCreateBtn"/, 'the empty state carries its create button');
  assert.match(html, /openAutomationForm/, 'the empty state button opens the same form');
});

test('the filter and history buttons show on Automations only; the Automata-only buttons stay hidden everywhere', () => {
  const { sb, out } = loadView();
  const shown = (html, id) => !new RegExp('id="' + id + '"[^>]*style="[^"]*display:none').test(html);
  const automataOnly = ['automataSelectBtn', 'automataFilterBtn', 'automataHistoryBtn', 'automataWatchFilterBtn'];
  sb.renderAutonomousView();
  assert.equal(subtabOf(sb), 'inbox');
  assert.ok(!shown(out.view, 'automationFilterBtn'), 'filter hidden on Inbox');
  assert.ok(!shown(out.view, 'automationHistoryBtn'), 'history hidden on Inbox');

  sb.switchAutomationSubtab('automations');
  assert.ok(shown(out.view, 'automationFilterBtn'), 'filter shown on Automations');
  assert.ok(shown(out.view, 'automationHistoryBtn'), 'history shown on Automations');
  for (const id of automataOnly) assert.ok(!shown(out.view, id), id + ' hidden on Automations');

  for (const tab of SUBTABS.filter(x => x !== 'automations')) {
    sb.switchAutomationSubtab(tab);
    assert.ok(!shown(out.view, 'automationFilterBtn'), 'filter hidden on ' + tab);
    assert.ok(!shown(out.view, 'automationHistoryBtn'), 'history hidden on ' + tab);
    for (const id of automataOnly) assert.ok(!shown(out.view, id), id + ' still hidden on ' + tab);
  }
});

test('the second-row bar is registry-driven: one button per entry, so a seventh sub-tab is one new entry', () => {
  const { sb } = loadView();
  const reg = vm.runInContext('_AUTOMATION_SUBTABS', sb);
  assert.deepEqual(Array.from(reg, e => e.id), SUBTABS, 'the current registry holds the six sub-tabs in order');
  const dummy = { id: 'seventh', labelKey: 'automation_subtab_seventh' };
  reg.push(dummy);
  try {
    const html = sb._automationSubtabsBarHTML();
    assert.equal(html.match(/data-subtab="/g).length, 7, 'the row renders one button per registry entry');
    assert.match(html, /data-subtab="seventh"/, 'the new entry gets its button');
    assert.deepEqual(subtabButtons(html).map(b => b.id), [...SUBTABS, 'seventh'], 'the new button renders in registry order');
    assert.equal(subtabButtons(html).find(b => b.id === 'inbox').active, true, 'the active button is unaffected by the appended entry');
  } finally {
    reg.pop();
    assert.equal(reg.length, 6, 'the dummy entry is not left behind');
    assert.equal(sb._automationSubtabsBarHTML().match(/data-subtab="/g).length, 6, 'the row is back to six buttons');
  }
});
