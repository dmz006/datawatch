// Rules and context settings card and the planning record — PWA side.
//
// window.rulesContextCardHtml(data) draws GET /api/autonomous/rules-context;
// rulesContextSave() sends PUT with the whole config; window.rulesContextPlanningHtml(rec)
// draws the planning record on an Automaton. Run with fetch stubbed and the real
// English bundle; the locale check reads every key the code uses in all five files.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
}

// loadApp loads app.js with fetch routed through `handler(url, opts)`, which
// returns { status, body }.
function loadApp(handler, byId) {
  const sandbox = buildSandbox({ byId: byId || {} });
  const calls = [];
  sandbox.fetch = (url, opts) => {
    calls.push({ url: String(url), method: (opts && opts.method) || 'GET', body: opts && opts.body });
    const r = handler(String(url), opts || {});
    const status = r.status || 200;
    const ok = status >= 200 && status < 300;
    return Promise.resolve({
      ok, status, statusText: ok ? '' : 'Bad Request',
      json: () => Promise.resolve(r.body),
      text: () => Promise.resolve(typeof r.body === 'string' ? r.body : JSON.stringify(r.body)),
    });
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

function apiResponse() {
  return {
    config: {
      rules_file: 'AGENT.md',
      context_files: ['CONTEXT.md', 'docs/state.md'],
      digest_mode: 'all',
      digest_max_bytes: 8192,
      projects: [{ project_dir: '/work/app', rules_file: 'RULES.md', context_files: ['NOTES.md'], digest_mode: 'off', digest_max_bytes: 1024 }],
    },
    policy: { rules_file: 'AGENT.md', context_files: ['CONTEXT.md'], digest_mode: 'all', digest_max_bytes: 8192 },
  };
}

function field(value) { return { value, innerHTML: '', style: {}, classList: { add() {}, remove() {} } }; }

function formElements(over) {
  const v = Object.assign({
    rulesContext_rules: 'AGENT.md', rulesContext_files: 'CONTEXT.md\n  docs/state.md \n\n', rulesContext_mode: 'all', rulesContext_bytes: '8192',
    rulesContextProj0_dir: '/work/app', rulesContextProj0_rules: '', rulesContextProj0_files: '', rulesContextProj0_mode: '', rulesContextProj0_bytes: '',
  }, over || {});
  const byId = { rulesContextCard: makeStubElement(), rulesContextError: makeStubElement() };
  for (const k of Object.keys(v)) byId[k] = field(v[k]);
  return byId;
}

const planningRecord = () => ({
  files: [
    { name: 'AGENT.md', kind: 'rules', present: true, sha256: 'abcdef0123456789abcdef0123456789', bytes: 1234 },
    { name: 'CONTEXT.md', kind: 'context', present: false },
  ],
  missing: ['CONTEXT.md'],
  notes: [],
  digest_injected: true,
});

// ── the card render ──────────────────────────────────────────────────────────

test('the card renders the global fields, the digest bounds and the project overrides', () => {
  const html = call(loadApp(() => ({})), 'window.rulesContextCardHtml(_v)', apiResponse());
  assert.ok(html.includes('value="AGENT.md"'), html);
  assert.ok(html.includes('CONTEXT.md\ndocs/state.md'), html);
  assert.ok(/<option value="all" selected>/.test(html), html);
  assert.ok(html.includes('Only for backends without file tools'), html);
  assert.ok(html.includes('min="512"') && html.includes('max="65536"'), html);
  assert.ok(html.includes('value="8192"'), html);
  assert.ok(html.includes('value="/work/app"') && html.includes('value="RULES.md"'), html);
  assert.ok(html.includes('rulesContextAddProject()') && html.includes('rulesContextRemoveProject(0)'), html);
  assert.ok(!/PRD/.test(html), 'no PRD wording in the card');
});

test('the card loads from the API into its slot', async () => {
  const slot = makeStubElement();
  const sb = loadApp(url => (url === '/api/autonomous/rules-context' ? { body: apiResponse() } : { status: 404, body: 'nf' }), { rulesContextCard: slot });
  await call(sb, 'window.loadRulesContextCard()');
  await flushAsync(10);
  assert.ok(String(slot.innerHTML).includes('value="AGENT.md"'), slot.innerHTML);
});

test('the settings area has a Rules and context group with the card and docs links', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.ok(src.includes("labelKey: 'settings_rules_context_title'"));
  assert.ok(src.includes("type: 'rules_context_card'"));
  assert.ok(src.includes("settings_rules_context_title: DOCS_DEFINITIONS + '#rules-and-context'"));
  assert.ok(src.includes("rules_context_howto: 'howto/automata-rules-and-context.md'"));
});

test('the docs links point at a how-to file and a definitions heading that exist', () => {
  const docs = path.join(__dirname, '..', '..', '..', 'docs');
  assert.ok(fs.existsSync(path.join(docs, 'howto', 'automata-rules-and-context.md')));
  const defs = fs.readFileSync(path.join(docs, 'datawatch-definitions.md'), 'utf8');
  assert.ok(/^### Rules and context$/m.test(defs));
});

// ── saving ───────────────────────────────────────────────────────────────────

test('save sends PUT with the whole config and trims the file list', async () => {
  const byId = formElements();
  const sb = loadApp((url, o) => (o.method === 'PUT' ? { body: apiResponse() } : { body: apiResponse() }), byId);
  call(sb, 'window._rulesContextState.cfg = _v', apiResponse().config);
  await call(sb, 'window.rulesContextSave()');
  await flushAsync(10);
  const put = sb.__calls.find(c => c.method === 'PUT');
  assert.ok(put, 'a PUT was sent');
  assert.equal(put.url, '/api/autonomous/rules-context');
  assert.deepEqual(JSON.parse(put.body), {
    rules_file: 'AGENT.md',
    context_files: ['CONTEXT.md', 'docs/state.md'],
    digest_mode: 'all',
    digest_max_bytes: 8192,
    projects: [{ rules_file: '', context_files: [], digest_mode: '', digest_max_bytes: 0, project_dir: '/work/app' }],
  });
});

test('a 400 shows the error inline and keeps the form values', async () => {
  const byId = formElements({ rulesContext_bytes: '10' });
  const msg = 'bad request: autonomous.rules_context.digest_max_bytes must be between 512 and 65536';
  const sb = loadApp(() => ({ status: 400, body: msg }), byId);
  call(sb, 'window._rulesContextState.cfg = _v', apiResponse().config);
  await call(sb, 'window.rulesContextSave()');
  await flushAsync(10);
  assert.ok(String(sb._rulesContextState.error).includes('digest_max_bytes must be between 512 and 65536'), sb._rulesContextState.error);
  assert.ok(!/^bad request/i.test(sb._rulesContextState.error));
  const html = call(sb, 'window.rulesContextCardHtml({ config: window._rulesContextState.cfg })');
  assert.ok(html.includes('id="rulesContextError"') && html.includes('role="alert"'), html);
  assert.ok(html.includes('digest_max_bytes must be between 512 and 65536'), html);
  assert.ok(html.includes('value="10"'), 'the entered value stays in the form');
});

test('add and remove project rows change the override list', () => {
  const byId = formElements();
  const sb = loadApp(() => ({}), byId);
  call(sb, 'window._rulesContextState.cfg = _v', apiResponse().config);
  call(sb, 'window.rulesContextAddProject()');
  assert.equal(sb._rulesContextState.cfg.projects.length, 2);
  call(sb, 'window.rulesContextRemoveProject(0)');
  assert.equal(sb._rulesContextState.cfg.projects.length, 1);
});

// ── the planning record ──────────────────────────────────────────────────────

test('the planning record shows hash, size, a missing-file warning and the digest line', () => {
  const html = call(loadApp(() => ({})), 'window.rulesContextPlanningHtml(_v)', planningRecord());
  assert.ok(html.includes('id="rulesContextPlanning"'), html);
  assert.ok(html.includes('AGENT.md (rules) hash abcdef012345, 1234 bytes'), html);
  assert.ok(!html.includes('abcdef0123456789'), 'only the short hash is shown');
  assert.ok(html.includes('class="rules-context-missing"') && html.includes('Warning: CONTEXT.md was not found'), html);
  assert.ok(html.includes('A digest was added to the plan prompt'), html);
  const none = call(loadApp(() => ({})), 'window.rulesContextPlanningHtml(_v)', Object.assign(planningRecord(), { digest_injected: false, missing: [], files: [] }));
  assert.ok(none.includes('No digest was added to the plan prompt'), none);
  assert.equal(call(loadApp(() => ({})), 'window.rulesContextPlanningHtml(_v)', null), '');
});

test('the Automaton detail views draw the planning record', () => {
  const sb = loadApp(() => ({}));
  const auto = call(sb, 'window.buildAutomationDetailHTML(_v, {})', { id: 'a1', name: 'N', status: 'draft', version: 1, rules_context: planningRecord() });
  assert.ok(auto.includes('id="rulesContextPlanning"'), 'automation detail');
  const prd = call(sb, '_renderDetailOverview(_v)', { id: 'p1', stories: [], rules_context: planningRecord() });
  assert.ok(prd.includes('id="rulesContextPlanning"') && prd.includes('Warning: CONTEXT.md was not found'), 'automaton overview');
});

// ── locales ──────────────────────────────────────────────────────────────────

test('every rules_context key the code uses exists in all five locales with the same placeholders', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const keys = new Set();
  for (const m of src.matchAll(/t\('((?:settings_)?rules_context_[a-z_]+)'/g)) keys.add(m[1]);
  assert.ok(keys.size >= 20, 'found the keys: ' + keys.size);
  const en = bundle('en');
  const ph = s => (String(s).match(/%\d\$s/g) || []).sort().join(',');
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of keys) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, lang + ' missing ' + k);
      assert.equal(ph(b[k]), ph(en[k]), lang + ' placeholders differ for ' + k);
      assert.ok(!/\bPRD\b/.test(b[k]) && !/\b(BL|B)\d+\b/.test(b[k]), lang + ' has a banned term in ' + k);
      if (lang !== 'en' && !['rules_context_kind_context'].includes(k)) {
        assert.notEqual(b[k], en[k], lang + ' copies English for ' + k);
      }
    }
  }
});
