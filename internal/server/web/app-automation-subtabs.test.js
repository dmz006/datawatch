// The Automation view's second-row sub-tabs: Overview and Chains.
//
// The first render of the Automation sub-tab is on Chains with exactly two
// second-row buttons, switching moves only the body, the choice is kept while
// the page stays open (a fresh load starts on Chains, localStorage is never
// touched), every saved link lands on Chains, no Questions sub-tab exists,
// the header New Automation button is gone while the round create button and
// the empty-state Create button remain, the filter and history buttons show
// on Chains only, and the bar renders one button per registry entry.

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

const subtabOf = sb => vm.runInContext('_automationState.subtab', sb);

// subtabButtons lists the second-row buttons in render order. The outer
// sub-tab bar of the Automata view uses data-tab; only the second row uses
// data-subtab, so this cannot pick up the other bar.
function subtabButtons(html) {
  return [...html.matchAll(/class="output-tab( active)? ?" role="tab" aria-selected="(true|false)" data-subtab="([a-z]+)"/g)]
    .map(m => ({ id: m[3], active: !!m[1], selected: m[2] === 'true' }));
}

test('default: a first render of the Automation sub-tab is Chains, and the row is exactly Overview, Chains', () => {
  const { sb, out } = loadView();
  assert.equal(subtabOf(sb), 'chains', 'module state starts on Chains');
  sb.renderAutonomousView();
  const row = subtabButtons(out.view);
  assert.deepEqual(row.map(b => b.id), ['overview', 'chains'], 'the second row has exactly two buttons in order');
  assert.equal(row[0].active, false, 'Overview is not active');
  assert.equal(row[0].selected, false);
  assert.equal(row[1].active, true, 'Chains is active');
  assert.equal(row[1].selected, true, 'Chains has aria-selected');
});

test('switching changes the body only: Overview holds the cards, Chains holds the list, an unknown id changes nothing', () => {
  const { sb, out } = loadView();
  sb.switchAutomationSubtab('overview');
  assert.equal(subtabOf(sb), 'overview');
  const row = subtabButtons(out.view);
  assert.equal(row.find(b => b.id === 'overview').active, true, 'Overview is now active');
  assert.equal(row.find(b => b.id === 'chains').active, false);
  assert.match(out.view, /id="progressCard"/, 'Overview body holds the progress card');
  assert.match(out.view, /id="automationCapacityCard"/, 'Overview body holds the capacity card');
  assert.doesNotMatch(out.view, /id="automationPanel"/, 'Overview body holds no list');
  assert.doesNotMatch(out.view, /id="automationFilterBar"/, 'Overview body holds no filter bar');

  sb.switchAutomationSubtab('chains');
  assert.equal(subtabOf(sb), 'chains');
  const back = subtabButtons(out.view);
  assert.equal(back.find(b => b.id === 'chains').active, true, 'Chains is active again');
  assert.match(out.view, /id="automationPanel"/, 'Chains body holds the list');
  assert.match(out.view, /id="automationFilterBar"/, 'Chains body holds the filter bar');
  assert.doesNotMatch(out.view, /id="progressCard"/, 'Chains body holds no progress card');
  assert.doesNotMatch(out.view, /id="automationCapacityCard"/, 'Chains body holds no capacity card');

  const before = out.view;
  sb.switchAutomationSubtab('no-such-subtab');
  assert.equal(subtabOf(sb), 'chains', 'an unknown id is ignored');
  assert.equal(out.view, before, 'an unknown id neither re-renders nor touches the state');
});

test('the chosen sub-tab is kept while the page stays open; a fresh load starts on Chains and never writes localStorage', () => {
  const { sb } = loadView();
  const writes = [];
  const origSet = sb.localStorage.setItem;
  sb.localStorage.setItem = (k, v) => { writes.push([k, String(v)]); return origSet(k, v); };
  sb.switchAutomationSubtab('overview');
  assert.deepEqual(writes, [], 'switching the second-row sub-tab never writes localStorage');
  sb.switchAutomataTab('automata');
  sb.switchAutomataTab('automation');
  assert.equal(subtabOf(sb), 'overview', 'leaving to the Automata sub-tab and returning keeps Overview');
  sb.switchAutomationSubtab('overview');
  assert.deepEqual(writes, [], 'coming back to the remembered Overview writes no localStorage either');
  const fresh = loadView();
  assert.equal(subtabOf(fresh.sb), 'chains', 'a freshly loaded sandbox starts on Chains');
});

test('deep links land on Chains, even after choosing Overview: navigate, openAutomationLink, and the parsed locations', () => {
  const { sb } = loadView();
  // The detail load is not the point of this test; let it fail cleanly.
  sb.fetch = url => (String(url).startsWith('/api/automations/a1')
    ? Promise.resolve({ ok: false, status: 500, statusText: 'x', text: () => Promise.resolve('down') })
    : Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve([]), text: () => Promise.resolve('[]') }));
  assert.equal(subtabOf(sb), 'chains');
  sb.switchAutomationSubtab('overview');
  assert.equal(subtabOf(sb), 'overview');
  sb.navigate('automation');
  assert.equal(subtabOf(sb), 'chains', "navigate('automation') lands on Chains");

  // The saved-link parser hands '' for the bare list and the id for one Automation.
  assert.equal(sb._automationRouteFromLocation({ hash: '#automation', search: '' }), '', '#automation parses to the list');
  assert.equal(sb._automationRouteFromLocation({ hash: '#automation/a1', search: '' }), 'a1', '#automation/a1 parses to the Automation');
  assert.equal(sb._automationRouteFromLocation({ hash: '', search: '?automation=a1' }), 'a1', '?automation=a1 parses to the Automation');

  for (const loc of [
    { hash: '#automation', search: '' },
    { hash: '#automation/a1', search: '' },
    { hash: '', search: '?automation=a1' },
  ]) {
    sb.switchAutomationSubtab('overview');
    assert.equal(subtabOf(sb), 'overview', 'Overview chosen again before the link');
    sb.openAutomationLink(sb._automationRouteFromLocation(loc));
    assert.equal(subtabOf(sb), 'chains', JSON.stringify(loc) + ' leaves the view on Chains');
  }
});

test('there is no Questions sub-tab: not in the rendered row, not in the registry, not in en.json', () => {
  const { sb, out } = loadView();
  sb.renderAutonomousView();
  assert.doesNotMatch(out.view, /question/i, 'the rendered view names no Questions sub-tab');
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

test('the filter and history buttons show on Chains and hide on Overview; the Automata-only buttons stay hidden on both', () => {
  const { sb, out } = loadView();
  const shown = (html, id) => !new RegExp('id="' + id + '"[^>]*style="[^"]*display:none').test(html);
  const automataOnly = ['automataSelectBtn', 'automataFilterBtn', 'automataHistoryBtn', 'automataWatchFilterBtn'];
  sb.renderAutonomousView();
  assert.equal(subtabOf(sb), 'chains');
  assert.ok(shown(out.view, 'automationFilterBtn'), 'filter shown on Chains');
  assert.ok(shown(out.view, 'automationHistoryBtn'), 'history shown on Chains');
  for (const id of automataOnly) assert.ok(!shown(out.view, id), id + ' hidden on Chains');

  sb.switchAutomationSubtab('overview');
  assert.ok(!shown(out.view, 'automationFilterBtn'), 'filter hidden on Overview');
  assert.ok(!shown(out.view, 'automationHistoryBtn'), 'history hidden on Overview');
  for (const id of automataOnly) assert.ok(!shown(out.view, id), id + ' still hidden on Overview');
});

test('the second-row bar is registry-driven: one button per entry, so a third sub-tab is one new entry', () => {
  const { sb } = loadView();
  const reg = vm.runInContext('_AUTOMATION_SUBTABS', sb);
  assert.equal(reg.length, 2, 'the current registry holds Overview and Chains');
  const dummy = { id: 'third', labelKey: 'automation_subtab_third' };
  reg.push(dummy);
  try {
    const html = sb._automationSubtabsBarHTML();
    assert.equal(html.match(/data-subtab="/g).length, 3, 'the row renders one button per registry entry');
    assert.match(html, /data-subtab="third"/, 'the new entry gets its button');
    assert.deepEqual(subtabButtons(html).map(b => b.id), ['overview', 'chains', 'third'], 'the new button renders in registry order');
    assert.equal(subtabButtons(html).find(b => b.id === 'chains').active, true, 'the active button is unaffected by the appended entry');
  } finally {
    reg.pop();
    assert.equal(reg.length, 2, 'the dummy entry is not left behind');
    assert.equal(sb._automationSubtabsBarHTML().match(/data-subtab="/g).length, 2, 'the row is back to two buttons');
  }
});
