// Settings → Automata — Planning routing card.
//
// Covers the card markup, the dry-run test form and result, the counts-only
// scorecard and the GENERAL_CONFIG_FIELDS entries. Rendered text is the real
// English bundle.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

function view(extra) {
  return Object.assign({
    enabled: true, shadow: false,
    rules: [
      { name: 'risky-bugs', match: { kind: ['bug'], risk_class: ['security'] }, planners: [{ backend: 'claude-code', model: 'opus' }, { backend: 'pi-prompt', model: 'qwen' }], ladder: 'strict' },
      { name: 'catch-all', match: {}, planners: [{ backend: 'pi-prompt', model: 'qwen' }] },
    ],
    ladders: { default: { touch_up_max: 1, replan_max: 1 }, strict: { touch_up_max: 2, replan_max: 3 } },
    classes: { security: { match_tags: ['security'] } },
    validation: { valid: true },
  }, extra || {});
}

test('card shows both toggles, ordered rules, ladders, classes, JSON editor and test form', () => {
  const sb = loadApp();
  const html = sb.planningRoutingCardHtml(view());
  assert.match(html, /id="prEnabled" checked/);
  assert.doesNotMatch(html, /id="prShadow" checked/);
  const rules = html.match(/<tr class="planning-routing-rule" data-rule="([^"]*)"/g).map(r => r.match(/data-rule="([^"]*)"/)[1]);
  assert.deepEqual(rules, ['risky-bugs', 'catch-all']);
  assert.match(html, /claude-code:opus → pi-prompt:qwen/);
  assert.match(html, /kind=bug; risk_class=security/);
  assert.match(html, /touch-ups up to 2, replans up to 3/);
  assert.match(html, /class="planning-routing-class"/);
  assert.match(html, /<textarea id="prJson"/);
  assert.match(html, /planningRoutingSaveJson\(\)/);
  assert.match(html, /id="prtKind"/);
  assert.match(html, /Automata/);
  assert.doesNotMatch(html, /\bPRDs?\b/);
});

test('card shows the validation error and the empty rules state', () => {
  const sb = loadApp();
  const html = sb.planningRoutingCardHtml(view({ rules: [], validation: { valid: false, error: 'bad ladder' } }));
  assert.match(html, /class="planning-routing-invalid"[^>]*>[^<]*bad ladder/);
  assert.match(html, /planning-routing-rules-empty/);
});

test('JSON editor holds the whole block so a PUT round-trips it', () => {
  const sb = loadApp();
  const html = sb.planningRoutingCardHtml(view());
  const raw = html.match(/<textarea id="prJson"[^>]*>([\s\S]*?)<\/textarea>/)[1]
    .replace(/&quot;/g, '"').replace(/&#39;/g, "'").replace(/&lt;/g, '<').replace(/&gt;/g, '>')
    .replace(/&amp;/g, '&'); // decoded last so an escaped ampersand is never unescaped twice
  const block = JSON.parse(raw);
  assert.deepEqual(Object.keys(block).sort(), ['classes', 'enabled', 'ladders', 'rules', 'shadow']);
  assert.equal(block.rules.length, 2);
  assert.equal(block.validation, undefined);
});

test('dry-run form has every input and the decision renders rule, planners, ladder and flags', () => {
  const sb = loadApp();
  const form = sb.planningRoutingTestFormHtml();
  for (const id of ['prtKind', 'prtTags', 'prtProject', 'prtSpecBytes', 'prtOverride']) assert.match(form, new RegExp('id="' + id + '"'));
  assert.match(form, /planningRoutingRunTest\(\)/);
  assert.match(form, /id="prtResult"/);
  const out = sb.planningRoutingDecisionHtml({ rule: 'risky-bugs', planners: [{ backend: 'claude-code', model: 'opus' }], ladder: 'strict', classes: ['security'], shadow: true, fallback: false, reason: 'matched' });
  assert.match(out, /risky-bugs/);
  assert.match(out, /claude-code:opus/);
  assert.match(out, /strict/);
  assert.match(out, /security/);
  assert.match(out, /matched/);
});

test('dry-run posts the typed input to the test endpoint and renders the answer', async () => {
  const sb = loadApp();
  const calls = [];
  const inputs = { prtKind: 'bug', prtTags: 'a, b', prtProject: 'p', prtSpecBytes: '1200', prtOverride: '' };
  const result = { innerHTML: '' };
  sb.document.getElementById = id => (id === 'prtResult' ? result : (id in inputs ? { value: inputs[id] } : null));
  sb.apiFetch = (url, opts) => { calls.push([url, opts]); return Promise.resolve({ rule: 'r1', planners: [], classes: [] }); };
  vm.runInContext('apiFetch = globalThis.apiFetch', sb);
  await sb.planningRoutingRunTest();
  assert.equal(calls.length, 1);
  assert.equal(calls[0][0], '/api/autonomous/planning-routing/test');
  assert.equal(calls[0][1].method, 'POST');
  assert.deepEqual(JSON.parse(calls[0][1].body), { kind: 'bug', project: 'p', override: '', tags: ['a', 'b'], spec_bytes: 1200 });
  assert.match(result.innerHTML, /r1/);
});

test('scorecard renders counts only and ignores any free text', () => {
  const sb = loadApp();
  const html = sb.planningRoutingScorecardHtml({
    window: '168h0m0s',
    scorecard: { rows: [{ backend: 'pi-prompt', model: 'qwen', class: 'security', decomposes: 4, passed_first_attempt: 2, passed_after_touch_up: 1, passed_after_replan: 0, escalations: 1, errors: 0, secret_note: 'LEAK-SPEC-TEXT' }] },
    gate: { decomposes: 4, passed_first_attempt: 2, passed_after_touch_up: 1, passed_after_replan: 0, escalations: 1, errors: 0, would_route_total: 3,
      would_route: [{ backend: 'pi-prompt', model: 'qwen', rule: 'catch-all', would_route: 3 }] },
    report: 'LEAK-REPORT-TEXT',
  });
  assert.match(html, /Automata decomposes 4; passed first try 2; after touch-up 1; after replan 0; escalations 1; errors 0; would route 3/);
  assert.equal((html.match(/class="planning-routing-score-row"/g) || []).length, 1);
  assert.match(html, /pi-prompt:qwen via catch-all: 3/);
  assert.doesNotMatch(html, /LEAK/);
  assert.match(html, /window 168h0m0s/);
});

test('scorecard shows the empty state when no rows exist', () => {
  const sb = loadApp();
  const html = sb.planningRoutingScorecardHtml({ scorecard: { rows: [] }, gate: {} });
  assert.match(html, /planning-routing-score-empty/);
  assert.doesNotMatch(html, /<table/);
});

test('GENERAL_CONFIG_FIELDS exposes enabled and shadow as toggles with planning_routing_ labels', () => {
  const sb = loadApp();
  const sections = vm.runInContext('GENERAL_CONFIG_FIELDS', sb);
  const fields = sections.find(s => s.id === 'autonomous').fields;
  for (const [key, labelKey] of [['autonomous.planning_routing.enabled', 'planning_routing_field_enabled'], ['autonomous.planning_routing.shadow', 'planning_routing_field_shadow']]) {
    const f = fields.find(x => x.key === key);
    assert.ok(f, key + ' is present');
    assert.equal(f.type, 'toggle');
    assert.equal(f.labelKey, labelKey);
    assert.ok(sb._i18n.bundle[labelKey], labelKey + ' has an English value');
  }
});

test('every planning_routing_ key the app uses exists in all five bundles', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  const keys = [...new Set(src.match(/planning_routing_[a-z_]+/g))];
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    const missing = keys.filter(k => !(k in b));
    assert.deepEqual(missing, [], lang + ' is missing keys');
  }
});
