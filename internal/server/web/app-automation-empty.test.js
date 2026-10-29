// Automation tab — empty state and capacity card.
//
// Drives the real renderAutomationView against a stubbed fetch: the empty state
// (heading, Create Automation button, Automation-versus-Automaton line) shows
// only after a successful load with zero Automations, never while loading and
// never after a failed load; the capacity and queue card loads in parallel with
// its own loading and failed states on the Automation tab's Overview sub-tab
// (the card lives there, the list and empty state on the Chains sub-tab);
// the Automata and Templates sub-tab bodies carry no status card.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

// load builds a sandbox whose fetch answers by URL. routes maps a path prefix
// to {status, body} or a function returning a promise; unmatched URLs answer [].
function load(routes) {
  const els = {};
  const out = {};
  for (const id of ['view', 'automationPanel', 'automationCapacityCard']) {
    out[id] = '';
    els[id] = makeStubElement(h => { out[id] = h; });
  }
  const sandbox = buildSandbox({ byId: els });
  sandbox.fetch = url => {
    const key = Object.keys(routes).find(k => String(url).startsWith(k));
    const r = key ? routes[key] : { status: 200, body: [] };
    if (typeof r === 'function') return r();
    return Promise.resolve({
      ok: r.status >= 200 && r.status < 300, status: r.status,
      statusText: 'err', json: () => Promise.resolve(r.body), text: () => Promise.resolve(String(r.body)),
    });
  };
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return { sb: sandbox, out };
}

const CAP = { pools: [{ name: 'host', limit: 4, held: 1, external: 0 }] };

test('after a successful load with zero Automations the empty state shows', async () => {
  const { sb, out } = load({ '/api/automations': { status: 200, body: [] }, '/api/capacity': { status: 200, body: CAP } });
  sb.renderAutomationView();
  await flushAsync(30);
  assert.match(out.automationPanel, /No Automations yet/);
  assert.match(out.automationPanel, /id="automationEmptyCreateBtn"/);
  assert.match(out.automationPanel, /Create Automation/);
  assert.match(out.automationPanel, /An Automation chains Automata in order; an Automaton is a single planned unit of work\./);
  assert.ok(!/\bPRDs?\b/.test(out.automationPanel), 'user-facing text never says PRD');
});

test('the Create Automation button runs the existing create flow', async () => {
  const { sb, out } = load({ '/api/automations': { status: 200, body: [] }, '/api/capacity': { status: 200, body: CAP } });
  sb.renderAutomationView();
  await flushAsync(30);
  const m = out.automationPanel.match(/id="automationEmptyCreateBtn"[^>]*onclick="([^"]*)"/);
  assert.ok(m, 'button has an onclick');
  let opened = 0;
  sb.openAutomationForm = () => { opened++; };
  vm.runInContext(m[1].replace(/&gt;/g, '>').replace(/&lt;/g, '<').replace(/&amp;/g, '&').replace(/&quot;/g, '"').replace(/&#39;/g, "'"), sb);
  assert.equal(opened, 1);
});

test('no empty state while the first load is in flight', async () => {
  const { sb, out } = load({
    '/api/automations': () => new Promise(() => {}),
    '/api/capacity': { status: 200, body: CAP },
  });
  sb.renderAutomationView();
  await flushAsync(30);
  assert.ok(!out.automationPanel.includes('No Automations yet'), out.automationPanel);
  assert.ok(!out.automationPanel.includes('automationEmptyCreateBtn'));
  assert.match(out.automationPanel, /list-state-skeleton|Loading/);
});

test('no empty state after a failed load, which shows the failed banner instead', async () => {
  const { sb, out } = load({ '/api/automations': { status: 500, body: 'boom' }, '/api/capacity': { status: 200, body: CAP } });
  sb.renderAutomationView();
  await flushAsync(30);
  assert.ok(!out.automationPanel.includes('automationEmptyCreateBtn'), out.automationPanel);
  assert.ok(!out.automationPanel.includes('No Automations yet'));
  assert.match(out.automationPanel, /list-state-failed/);
  assert.match(out.automationPanel, /Could not load Automations/);
});

test('a non-empty list shows cards and no empty state', async () => {
  const { sb, out } = load({
    '/api/automations': { status: 200, body: [{ id: 'a1b2c3d4', name: 'Nightly', status: 'draft', members: [] }] },
    '/api/capacity': { status: 200, body: CAP },
  });
  sb.renderAutomationView();
  await flushAsync(30);
  assert.match(out.automationPanel, /data-automation-id="a1b2c3d4"/);
  assert.ok(!out.automationPanel.includes('automationEmptyCreateBtn'));
});

test('the Automation tab renders the capacity card from the shared component', async () => {
  const { sb, out } = load({ '/api/automations': { status: 200, body: [] }, '/api/capacity': { status: 200, body: CAP } });
  sb.renderAutomationView();
  // The capacity card lives on the Overview sub-tab; select it before reading.
  sb.switchAutomationSubtab('overview');
  assert.match(out.view, /id="automationCapacityCard"/);
  await flushAsync(30);
  assert.match(out.automationCapacityCard, /prd-capacity-card/);
  assert.match(out.automationCapacityCard, /1 \/ 4/);
});

test('the capacity card has its own loading and failed handling', async () => {
  const slow = load({ '/api/automations': { status: 200, body: [] }, '/api/capacity': () => new Promise(() => {}) });
  slow.sb.renderAutomationView();
  await flushAsync(30);
  assert.match(slow.out.automationPanel, /No Automations yet/, 'the Chains list is not held up by the card');
  // The card slot lives on the Overview sub-tab; selecting it starts the load.
  slow.sb.switchAutomationSubtab('overview');
  await flushAsync(30);
  assert.match(slow.out.automationCapacityCard, /list-state-skeleton/);

  const bad = load({ '/api/automations': { status: 200, body: [] }, '/api/capacity': { status: 500, body: 'nope' } });
  bad.sb.renderAutomationView();
  // Let the first Chains load settle first: switching the sub-tab aborts
  // in-flight loads, which would leave the panel at the initial skeleton.
  await flushAsync(30);
  // The card slot lives on the Overview sub-tab; selecting it starts the load.
  bad.sb.switchAutomationSubtab('overview');
  await flushAsync(30);
  assert.match(bad.out.automationCapacityCard, /Could not load capacity and queue/);
  assert.match(bad.out.automationCapacityCard, /Retry/);
  assert.match(bad.out.automationPanel, /No Automations yet/, 'a failed card does not blank the list');
});

test('the capacity card is present on the Automation sub-tab only', async () => {
  const { sb, out } = load({ '/api/automations': { status: 200, body: [] }, '/api/capacity': { status: 200, body: CAP } });
  sb.renderAutomationView();
  sb.switchAutomationSubtab('overview');
  assert.equal((out.view.match(/id="automationCapacityCard"/g) || []).length, 1, 'exactly one capacity slot on the view');
  assert.equal((out.view.match(/id="progressCard"/g) || []).length, 1, 'exactly one progress slot on the view');
  assert.doesNotMatch(out.view, /id="prdCapacityCard"/, 'no machine-wide capacity slot on the Automata list');
  await flushAsync(30);
  assert.match(out.automationCapacityCard, /prd-capacity-card/);
  sb.switchAutomataTab('automata');
  assert.doesNotMatch(out.view, /id="automationCapacityCard"/, 'no capacity slot on the Automata body');
  assert.doesNotMatch(out.view, /id="progressCard"/, 'no progress slot on the Automata body');
  assert.doesNotMatch(out.view, /id="prdCapacityCard"/, 'no capacity slot anywhere on the Automata body');
  sb.switchAutomataTab('templates');
  assert.doesNotMatch(out.view, /id="automationCapacityCard"/, 'no capacity slot on the Templates body');
  assert.doesNotMatch(out.view, /id="progressCard"/, 'no progress slot on the Templates body');
  assert.doesNotMatch(out.view, /id="prdCapacityCard"/, 'no capacity slot anywhere on the Templates body');
});

test('every locale bundle has the new keys and keeps the Automata wording', () => {
  const keys = ['automation_empty_title', 'automation_empty_create', 'automation_empty_explain', 'automation_capacity_failed'];
  const dir = path.join(__dirname, 'locales');
  for (const l of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = JSON.parse(fs.readFileSync(path.join(dir, l + '.json'), 'utf8'));
    for (const k of keys) assert.ok(b[k] && b[k].length > 0, `${l} missing ${k}`);
    assert.ok(!/\bPRDs?\b/.test(b.automation_empty_explain), `${l} explain mentions PRD`);
  }
});
