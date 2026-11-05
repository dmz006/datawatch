// The Automaton detail view's tab strip (.prd-detail-tabs).
//
// _renderDetailTabStrip emits eight tabs when the Scan and Rules tabs are
// enabled and six otherwise (Trace is always shown); style.css lets the strip scroll horizontally
// (overflow-x:auto, nowrap, 40px touch-target min-height); the tab swap in
// switchAutomataDetailTab re-renders the strip and then calls
// _automataDetailTabsReveal, which scrolls the strip (never the page) to the
// active tab. Regression: replacing the strip via outerHTML resets its
// scrollLeft to 0, so without the reveal call a tab past the right edge
// (Scan, Rules) would be selected but invisible, and without nowrap/overflow
// and the min-width:0 wrappers an eight-tab strip widens the whole page.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

const APP = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
const CSS = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');

function load(docOverrides = {}) {
  const sandbox = buildSandbox();
  Object.assign(sandbox.document, docOverrides);
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

const tabKeys = html => [...html.matchAll(/switchAutomataDetailTab\('([a-z]+)'\)/g)].map(m => m[1]);

// ruleBody returns the declarations of every CSS rule whose selector list
// mentions sel, concatenated.
function ruleBody(sel) {
  let out = '';
  const re = /([^{}]+)\{([^{}]*)\}/g;
  let m;
  while ((m = re.exec(CSS))) {
    if (m[1].split(',').some(s => s.trim().endsWith(sel))) out += m[2] + '\n';
  }
  return out;
}

test('strip emits all eight tabs in order when scan and rules are enabled', () => {
  const sb = load();
  const html = sb._renderDetailTabStrip({ scan_enabled: true, rules_enabled: true }, 'overview');
  assert.deepEqual(tabKeys(html),
    ['overview', 'stories', 'decisions', 'memory', 'subscriptions', 'scan', 'rules', 'trace']);
  assert.match(html, /class="output-tabs prd-detail-tabs"/);
});

test('strip omits Scan and Rules when neither is enabled nor has results', () => {
  const sb = load();
  const html = sb._renderDetailTabStrip({}, 'overview');
  assert.deepEqual(tabKeys(html), ['overview', 'stories', 'decisions', 'memory', 'subscriptions', 'trace']);
});

test('strip shows Scan or Rules independently, including from past results', () => {
  const sb = load();
  assert.ok(tabKeys(sb._renderDetailTabStrip({ last_scan_at: 'x' }, 'overview')).includes('scan'));
  assert.ok(!tabKeys(sb._renderDetailTabStrip({ last_scan_at: 'x' }, 'overview')).includes('rules'));
  assert.ok(tabKeys(sb._renderDetailTabStrip({ rules_enabled: true }, 'overview')).includes('rules'));
});

test('only the current tab carries the active class', () => {
  const sb = load();
  const html = sb._renderDetailTabStrip({ scan_enabled: true, rules_enabled: true }, 'rules');
  const active = [...html.matchAll(/class="output-tab active"[^>]*switchAutomataDetailTab\('([a-z]+)'\)/g)].map(m => m[1]);
  assert.deepEqual(active, ['rules']);
});

test('style.css: the strip scrolls horizontally without wrapping', () => {
  const body = ruleBody('.prd-detail-tabs.output-tabs');
  assert.match(body, /overflow-x:\s*auto/);
  assert.match(body, /flex-wrap:\s*nowrap/);
  assert.match(body, /min-width:\s*0/);
  assert.match(body, /max-width:\s*100%/);
  const tab = ruleBody('.prd-detail-tabs .output-tab');
  assert.match(tab, /white-space:\s*nowrap/);
  assert.match(tab, /flex:\s*0 0 auto/);
});

test('style.css: touch/narrow tabs have a 40px min-height', () => {
  assert.match(CSS,
    /@media \(max-width: 600px\), \(pointer: coarse\) \{\s*\.prd-detail-tabs \.output-tab \{[^}]*min-height:\s*40px/);
});

test('regression: wrappers cannot let the strip widen the page', () => {
  const body = ruleBody('#automataDetailBody');
  assert.match(body, /min-width:\s*0/);
  assert.match(body, /max-width:\s*100%/);
});

test('_automataDetailTabsReveal exists, is exported, and switchAutomataDetailTab calls it after the strip swap', () => {
  const sb = load();
  assert.equal(typeof sb._automataDetailTabsReveal, 'function');
  const fn = APP.slice(APP.indexOf('function switchAutomataDetailTab('));
  const body = fn.slice(0, fn.indexOf('window.switchAutomataDetailTab'));
  const swap = body.indexOf('tabsEl.outerHTML = _renderDetailTabStrip(prd, tab)');
  const reveal = body.indexOf('_automataDetailTabsReveal()');
  assert.ok(swap >= 0 && reveal > swap, 'reveal runs after the strip is replaced');
});

test('_automataDetailTabsReveal centres the active tab by moving only the strip scrollLeft', () => {
  const strip = { clientWidth: 300, scrollLeft: 0 };
  const btn = { parentElement: strip, offsetLeft: 700, offsetWidth: 100 };
  const sb = load({ querySelector: sel => (sel === '.prd-detail-tabs .output-tab.active' ? btn : null) });
  sb._automataDetailTabsReveal();
  assert.equal(strip.scrollLeft, 700 - (300 - 100) / 2);
  // No active tab: no throw, no change.
  const sb2 = load();
  assert.doesNotThrow(() => sb2._automataDetailTabsReveal());
});

test('switchAutomataDetailTab re-renders the strip with the chosen tab active, changes the body and reveals', () => {
  const written = { strip: null, body: null };
  const stripEl = { set outerHTML(v) { written.strip = v; } };
  const bodyEl = makeStubElement(h => { written.body = h; });
  let revealed = 0;
  const sb = load({
    querySelector(sel) {
      if (sel === '#automataDetailBody .prd-detail-tabs') return stripEl;
      if (sel === '#automataDetailBody .prd-detail-tab-body') return bodyEl;
      if (sel === '.prd-detail-tabs .output-tab.active') { revealed++; return null; }
      return null;
    },
  });
  const prd = { id: 'p1', title: 'T', status: 'draft', scan_enabled: true, rules_enabled: true, stories: [] };
  vm.runInContext('_automataDetailId = "p1"', sb);
  sb._automataDetailPRD = prd;
  vm.runInContext('_automataDetailPRD = ' + JSON.stringify(prd), sb);

  sb.switchAutomataDetailTab('stories');
  assert.equal(vm.runInContext('_automataDetailTab', sb), 'stories');
  assert.deepEqual(tabKeys(written.strip).length, 8);
  assert.match(written.strip, /class="output-tab active"[^>]*switchAutomataDetailTab\('stories'\)/);
  assert.doesNotMatch(written.strip, /class="output-tab active"[^>]*switchAutomataDetailTab\('overview'\)/);
  const storiesBody = written.body;
  assert.ok(storiesBody && storiesBody.length > 0, 'body rendered for stories');
  assert.ok(revealed >= 1, 'reveal ran after the swap');

  sb.switchAutomataDetailTab('rules');
  assert.match(written.strip, /class="output-tab active"[^>]*switchAutomataDetailTab\('rules'\)/);
  assert.notEqual(written.body, storiesBody, 'body changes with the tab');
});
