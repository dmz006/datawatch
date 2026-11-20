// Checks and ship status cards (Automation view, Overview sub-tab) — PWA side.
//
// window.checksCardHtml(status) draws GET /api/autonomous/checks and
// window.shipCardHtml(status) draws GET /api/autonomous/ship: the resolved
// modes and the recorded findings, read-only. The card render, the loaders,
// the Settings fields, the help-link lookup and the locale bundles are covered
// here with fetch stubbed and the real English bundle.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const HOSTILE = '<img src=x onerror=alert(1)>';
const ENTITY_IMG = '&lt;img src=x onerror=alert(1)&gt;';

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
}

function loadApp(routes, byId) {
  const sandbox = buildSandbox({ byId: byId || {} });
  const calls = [];
  sandbox.fetch = url => {
    calls.push(String(url));
    if (!Object.prototype.hasOwnProperty.call(routes || {}, url)) {
      return Promise.resolve({ ok: false, status: 404, statusText: 'Not Found', text: () => Promise.resolve('not found') });
    }
    const body = routes[url];
    return Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(body), text: () => Promise.resolve(JSON.stringify(body)) });
  };
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  sandbox.localStorage = { getItem: () => null };
  sandbox.__calls = calls;
  return sandbox;
}

function call(sandbox, expr, value) {
  sandbox._v = value;
  return vm.runInContext(expr, sandbox);
}

function checksStatus(over) {
  return Object.assign({
    policy: { pre_start_verifier: 'on', block_when_missing: true, plan_rules: 'shadow', after_done: 'off', registry_readme: 'on' },
    findings: [{ prd_id: 'a1b2c3d4', at: '2026-11-02T10:00:00Z', source: 'verifier_prestart', text: 'verification script "x.sh" does not exist' }],
  }, over || {});
}

function shipStatus(over) {
  return Object.assign({
    policy: { push: 'on', tag: 'shadow', ci_watch: 'off', tag_format: 'v{version}', remote: 'origin', ci_workflows: ['release.yaml'] },
    findings: [{ prd_id: 'a1b2c3d4', at: '2026-11-02T11:00:00Z', source: 'ship_step', text: 'pushed to origin' }],
  }, over || {});
}

// ── the card render ──────────────────────────────────────────────────────────

test('the checks card shows each check mode, the findings and the read-only note', () => {
  const html = call(loadApp({}), 'window.checksCardHtml(_v)', checksStatus());
  assert.ok(html.includes('Automata checks'), html);
  assert.ok(html.includes('Pre-start verifier: on'), html);
  assert.ok(html.includes('Plan rules: shadow (record only)'), html);
  assert.ok(html.includes('After-done checklist: off'), html);
  assert.ok(html.includes('Registry check: on'), html);
  assert.ok(html.includes('Block a start when the verifier is missing: yes'), html);
  assert.ok(html.includes('verification script &quot;x.sh&quot; does not exist') || html.includes('does not exist'), html);
  assert.ok(html.includes('[verifier_prestart]'), html);
  assert.ok(html.includes('Read only.'), html);
});

test('the ship card explains itself, tiles the three steps and reads findings as title and sentence', () => {
  const sb = loadApp({});
  vm.runInContext("_automataState.allPrds.push({ id: 'a1b2c3d4', title: 'Fix the parser' });", sb);
  const html = call(sb, 'window.shipCardHtml(_v)', shipStatus());
  assert.ok(html.includes('Automata ship status'), html);
  // The plain-language line under the title says what shipping is and what
  // each mode word means.
  assert.ok(html.includes('When an Automaton is finished the daemon ships it'), html);
  assert.ok(html.includes('off (does nothing), shadow (records what it would do) or on (acts)'), html);
  // Three compact step tiles: plain step name and the mode word.
  assert.ok(html.includes('ship-step-tiles'), html);
  assert.ok(/>Push</.test(html), html);
  assert.ok(/>Tag</.test(html), html);
  assert.ok(/>CI watch</.test(html), html);
  assert.ok(html.includes('shadow (record only)'), html);
  // Tag format, remote and workflow names: one short secondary line.
  assert.ok(html.includes('Tag format v{version} · Remote origin · CI release.yaml'), html);
  // Findings are rows: the Automaton's title, then the outcome sentence.
  assert.ok(html.includes('Fix the parser'), 'title shown: ' + html);
  assert.ok(html.includes('pushed to origin'), html);
  // With no title known the row still shows an id fallback, not nothing.
  const sb2 = loadApp({});
  const html2 = call(sb2, 'window.shipCardHtml(_v)', shipStatus());
  assert.ok(html2.includes('a1b2c3d4'), html2);
});

test('both cards read as empty for a missing or malformed body and say no findings', () => {
  const sb = loadApp({});
  for (const bad of [undefined, null, 'x', [], {}]) {
    const c = call(sb, 'window.checksCardHtml(_v)', bad);
    const s = call(sb, 'window.shipCardHtml(_v)', bad);
    assert.ok(c.includes('No findings recorded.'), String(bad));
    assert.ok(s.includes('No findings recorded.'), String(bad));
    assert.ok(s.includes('When an Automaton is finished the daemon ships it'), String(bad));
  }
});

test('a hostile value is escaped and an unknown mode word shows as given', () => {
  const sb = loadApp({});
  const c = call(sb, 'window.checksCardHtml(_v)', checksStatus({
    policy: { pre_start_verifier: HOSTILE },
    findings: [{ prd_id: HOSTILE, at: '2026-11-02T10:00:00Z', source: HOSTILE, text: HOSTILE }],
  }));
  assert.ok(!c.includes(HOSTILE), 'raw markup leaked: ' + c);
  assert.ok(c.includes(ENTITY_IMG));
  const s = call(sb, 'window.shipCardHtml(_v)', shipStatus({ policy: { tag_format: HOSTILE, remote: HOSTILE, ci_workflows: [HOSTILE] } }));
  assert.ok(!s.includes(HOSTILE), 'raw markup leaked: ' + s);
});

test('more than twenty findings are cut and the rest counted', () => {
  const findings = [];
  for (let i = 0; i < 23; i++) findings.push({ prd_id: 'p' + i, at: '2026-11-02T10:00:00Z', source: 'ship_step', text: 'f' + i });
  const html = call(loadApp({}), 'window.shipCardHtml(_v)', shipStatus({ findings }));
  assert.equal((html.match(/checks-ship-finding/g) || []).length, 20);
  assert.ok(html.includes('3 more findings not shown'), html);
});

// ── the help-link lookup ─────────────────────────────────────────────────────

test('docsLinkForKey resolves every checks and ship key to a definitions anchor', () => {
  const sb = loadApp({});
  const expected = {
    checks_findings_card: '#checks',
    ship_status_card: '#ship-steps',
    settings_checks_group: '#checks',
    settings_ship_group: '#ship-steps',
    settings_checks_pre_start_verifier: '#pre-start-verifier',
    settings_checks_plan_rules: '#plan-rules',
    settings_checks_after_done: '#after-done-checklist',
    settings_checks_registry_readme: '#registry-check',
    settings_ship_ci_watch: '#ci-watch',
  };
  const defs = fs.readFileSync(path.join(__dirname, '..', '..', '..', 'docs', 'datawatch-definitions.md'), 'utf8');
  for (const [key, anchor] of Object.entries(expected)) {
    const link = call(sb, 'docsLinkForKey(_v)', key);
    assert.equal(link, 'datawatch-definitions.md' + anchor, key);
    const heading = defs.split('\n').find(l => /^#{2,4} /.test(l) && l.replace(/^#+ /, '').toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '') === anchor.slice(1));
    assert.ok(heading, 'definitions.md has no heading for ' + anchor);
  }
});

test('the card titles carry a help link that follows the inline doc links preference', () => {
  const sb = loadApp({});
  const c = call(sb, 'window.checksCardHtml(_v)', checksStatus());
  const s = call(sb, 'window.shipCardHtml(_v)', shipStatus());
  assert.ok(c.includes('/diagrams.html#docs/datawatch-definitions.md#checks'), c);
  assert.ok(s.includes('/diagrams.html#docs/datawatch-definitions.md#ship-steps'), s);
  sb.localStorage = { getItem: k => (k === 'cs_show_docs_links' ? '0' : null) };
  assert.ok(!call(sb, 'window.checksCardHtml(_v)', checksStatus()).includes('checks-ship-help'));
});

// ── the loaders and the sub-tab ──────────────────────────────────────────────

test('the loaders read their routes and draw into the Overview slots', async () => {
  const cSlot = makeStubElement();
  const sSlot = makeStubElement();
  const sb = loadApp({ '/api/autonomous/checks': checksStatus(), '/api/autonomous/ship': shipStatus() },
    { automationChecksCard: cSlot, automationShipCard: sSlot });
  const c = vm.runInContext('_automationChecksLoader()', sb);
  const s = vm.runInContext('_automationShipLoader()', sb);
  c.load ? c.load() : c.refresh && c.refresh();
  s.load ? s.load() : s.refresh && s.refresh();
  await flushAsync(30);
  try {
    assert.ok(sb.__calls.includes('/api/autonomous/checks'), String(sb.__calls));
    assert.ok(sb.__calls.includes('/api/autonomous/ship'), String(sb.__calls));
    assert.ok(cSlot.innerHTML.includes('Pre-start verifier: on'), cSlot.innerHTML);
    assert.ok(sSlot.innerHTML.includes('Remote origin'), sSlot.innerHTML);
  } finally {
    c.stop();
    s.stop();
  }
});

test('the Resources sub-tab carries both slots and the stop lists end both loaders', () => {
  const sb = loadApp({});
  const body = vm.runInContext("_AUTOMATION_SUBTABS.find(s => s.id === 'resources').bodyHTML()", sb);
  assert.ok(!body.includes('id="automationChecksCard"'), 'the checks card moved to the Events stream');
  assert.ok(body.includes('id="automationShipCard"'));
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.ok(src.includes("url: '/api/autonomous/checks'") && src.includes("url: '/api/autonomous/ship'"));
  assert.ok(src.includes("'automation_usage', 'automation_ship']);"), 'the Resources stop() must end the ship loader');
  assert.ok(src.includes("'automation_onboarding_report', 'automation_checks', 'automation_ship'];"), 'the stop-all list must include both loaders');
});

// ── the Settings fields ──────────────────────────────────────────────────────

test('the Settings field list carries every checks and ship key with a label in the bundle', () => {
  const sb = loadApp({});
  const sec = vm.runInContext('GENERAL_CONFIG_FIELDS', sb).find(s => s.id === 'autonomous');
  const en = bundle('en');
  const keys = ['autonomous.checks.pre_start_verifier', 'autonomous.checks.block_when_missing', 'autonomous.checks.plan_rules',
    'autonomous.checks.after_done', 'autonomous.checks.registry_readme', 'autonomous.ship.push', 'autonomous.ship.tag',
    'autonomous.ship.ci_watch', 'autonomous.ship.tag_format', 'autonomous.ship.remote', 'autonomous.ship.ci_workflows'];
  for (const k of keys) {
    const f = sec.fields.find(x => x.key === k);
    assert.ok(f, k + ' missing from the Settings field list');
    assert.ok(en[f.labelKey], k + ' label key missing from the bundle');
    if (f.type === 'select') assert.deepEqual(Array.from(f.options), ['off', 'shadow', 'on'], k);
  }
  for (const gk of ['settings_checks_group', 'settings_ship_group']) {
    const g = sec.fields.find(x => x.type === 'group' && x.labelKey === gk);
    assert.ok(g, gk + ' group missing');
    assert.ok(call(sb, 'docsLinkForKey(_v)', g.docsKey), gk + ' docs key does not resolve');
  }
});

// ── locale keys ──────────────────────────────────────────────────────────────

test('every checks and ship key the PWA uses is in every bundle with no PRD word or tracker id', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const start = src.indexOf('// ── Automata checks and ship status cards');
  const end = src.indexOf('// ── Automata model request progress');
  const loaders = src.indexOf('function _automationChecksLoader');
  const text = src.slice(start, end) + src.slice(loaders, loaders + 900) +
    src.slice(src.indexOf("label: 'Checks', labelKey"), src.indexOf("label: 'Checks', labelKey") + 4200);
  const used = new Set((text.match(/'(?:settings_)?(?:checks|ship)_[a-z_]*[a-z]'/g) || []).map(s => s.slice(1, -1)));
  assert.ok(used.size >= 25, 'expected many keys, saw ' + used.size);
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = bundle(lang);
    for (const k of used) {
      if (k === 'checks_findings_card' || k === 'ship_status_card') continue; // docs-table keys, not strings
      const v = b[k];
      assert.ok(typeof v === 'string' && v.length > 0, lang + ' missing or empty ' + k);
      assert.ok(!/\bPRDs?\b/.test(v), lang + ' ' + k + ' must say Automaton or Automata, never PRD');
      assert.ok(!/\b(?:BL|F|B|S)\d{1,6}\b/.test(v), lang + ' ' + k + ' carries a tracker id');
    }
  }
});
