// Per-Automaton + guardrail-profile notification events: the closed
// NOTIFY_EVENT_KINDS set, the detail-view line, the PRD settings modal
// (inherit checkbox, kind checkboxes, checkbox disabling, POST on save),
// and the guardrail profile panel — all exercised against the REAL app.js
// (loaded unmodified into a vm sandbox) and the real locale JSON files.
'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

// Wall-clock waits: under node --test the testutil flushAsync()
// (setImmediate phases) does not reliably interleave with the
// setTimeout(0) fetch-stub microtask chain inside app.js — the PRD
// settings modal mount + save toasts land only after a real-time wait.
const sleep = ms => new Promise(r => setTimeout(r, ms));

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const EXPECTED_KINDS = ['prd.active', 'prd.approved', 'prd.archived', 'prd.blocked', 'prd.cancelled', 'prd.completed', 'prd.draft', 'prd.failed', 'prd.needs_review', 'prd.paused', 'prd.planning', 'prd.rejected', 'prd.revisions_asked', 'prd.running', 'task.awaiting_approval', 'task.blocked', 'task.cancelled', 'task.completed', 'task.failed', 'task.in_progress', 'task.pending', 'task.queued', 'task.review_needed', 'task.running_tests', 'task.session_cleared', 'task.session_started', 'task.verifying', 'task.waiting_capacity'];
// 15 keys the notify-events surfaces reference (PRD settings modal, detail
// line, guardrail profile panel). Must exist in every locale bundle.
const NOTIFY_KEYS = [
  'notify_events_label',
  'notify_events_help',
  'notify_events_inherit',
  'notify_events_effective',
  'notify_events_none',
  'notify_events_default_label',
  'notify_events_source_automaton',
  'notify_events_source_profile',
  'notify_events_source_global',
  'notify_events_saved',
  'notify_events_save_failed',
  'notify_events_default_marker',
  'notify_events_profile_label',
  'notify_events_profile_edit',
  'notify_events_profile_prompt',
];

const json = (obj, status = 200) => ({
  ok: status >= 200 && status < 300,
  status,
  statusText: '',
  json: async () => obj,
  text: async () => JSON.stringify(obj),
});
const fail = (msg, status = 400) => ({
  ok: false,
  status,
  statusText: '',
  json: async () => { throw new Error('no json on error body'); },
  text: async () => msg,
});

// getElementById stub that records addEventListener (form submit path).
function makeFormStub() {
  const el = makeStubElement();
  el.__listeners = {};
  el.addEventListener = (type, fn) => { el.__listeners[type] = fn; };
  return el;
}

function setupApp({ routes = {}, byId = {}, checkedKinds = [] } = {}) {
  const calls = [];
  const toasts = [];
  const appended = [];
  const sandbox = buildSandbox({ byId });
  sandbox.fetch = (url, opts) => {
    const method = (opts && opts.method || 'GET').toUpperCase();
    const bodyRaw = opts && opts.body;
    let body = null;
    try { if (bodyRaw) body = JSON.parse(bodyRaw); } catch { body = bodyRaw; }
    calls.push({ url: String(url), method, body });
    // Routes may be a static response, a function (method, body) => response,
    // or undefined. apiFetch requires .status/.ok/.json()/.text().
    return new Promise(resolve => {
      setTimeout(() => {
        let r = routes[url];
        if (typeof r === 'function') r = r(method, body);
        if (!r) r = fail('Not Found', 404);
        resolve(r);
      }, 0);
    });
  };
  sandbox.document.querySelectorAll = sel =>
    (sel && String(sel).includes('data-notify-kind'))
      ? checkedKinds.map(k => ({ value: k }))
      : [];
  sandbox.document.body.appendChild = child => appended.push(child);
  vm.createContext(sandbox); // testutil contract: caller contextifies before loadScript.
  loadScript(APP_JS, sandbox);
  // Real English bundle (loaded from the same files the coverage test
  // checks) so t() resolves via the PWA's own i18n path rather than the
  // inline fallbacks.
  const en = JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, 'en.json'), 'utf8'));
  sandbox._i18n.bundle = en;
  sandbox._i18n.fallback = en;
  sandbox.showToast = (msg, type) => toasts.push({ msg: String(msg), type: String(type || 'info') });
  sandbox.confirm = () => true;
  sandbox.prompt = () => null;
  const modal = () => {
    for (let i = appended.length - 1; i >= 0; i--) if (appended[i].id === 'prdModal') return appended[i];
    return null;
  };
  return {
    sandbox,
    calls,
    toasts,
    en,
    checkedKinds,
    run: code => vm.runInContext(code, sandbox),
    modal,
  };
}

test('locales: every notify_events_* key present in all five bundles', () => {
  for (const loc of ['en', 'de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, loc + '.json'), 'utf8'));
    for (const key of NOTIFY_KEYS) {
      assert.equal(typeof bundle[key], 'string', `${loc}.json missing key ${key}`);
      assert.ok(bundle[key].trim().length > 0, `${loc}.json empty value for ${key}`);
    }
  }
});

test('locales: every notify_events key referenced in app.js exists in en', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const refs = new Set();
  const re = /t\(\s*'(notify_events_[a-zA-Z0-9_]+)'/g;
  let m;
  while ((m = re.exec(src)) !== null) refs.add(m[1]);
  assert.ok(refs.size >= 8, `expected several notify_events references in app.js, got ${refs.size}`);
  const en = JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, 'en.json'), 'utf8'));
  for (const key of refs) {
    assert.ok(key in en, `app.js references t('${key}') but en.json has no such key`);
    assert.equal(typeof en[key], 'string');
  }
});

test('NOTIFY_EVENT_KINDS / NOTIFY_EVENT_DEFAULTS: closed set + defaults subset', t => {
  const { sandbox, run } = setupApp();
  const kinds = run('NOTIFY_EVENT_KINDS');
  const defaults = run('NOTIFY_EVENT_DEFAULTS');
  assert.ok(Array.isArray(kinds) && kinds.length === 28, `expected 28 kinds, got ${kinds && kinds.length}`);
  assert.deepEqual(new Set(kinds), new Set(EXPECTED_KINDS), 'kind set drifted from the lifecycle event kinds');
  assert.equal(new Set(kinds).size, kinds.length, 'duplicate kinds in NOTIFY_EVENT_KINDS');
  assert.ok(Array.isArray(defaults) && defaults.length === 5, `expected 5 defaults, got ${defaults && defaults.length}`);
  assert.equal(new Set(defaults).size, defaults.length, 'duplicate defaults');
  const kindSet = new Set(kinds);
  for (const d of defaults) assert.ok(kindSet.has(d), `default ${d} not in kind set`);
});

test('_renderNotifyKindOptions: grouped checkboxes, default markers, inherit disables', t => {
  const { run } = setupApp();
  const html = run(`_renderNotifyKindOptions(['prd.blocked'], false)`);

  // All 28 kinds present as checkboxes, grouped into three sections.
  const boxes = html.match(/data-notify-kind value="/g) || [];
  assert.equal(boxes.length, 28);
  assert.match(html, />Automaton</);
  assert.match(html, />Task</);
  assert.match(html, />Session</);
  // Group ordering: every prd.* row before the Task header, session rows last.
  const prdLast = html.lastIndexOf('value="prd.running"');
  const taskHeader = html.indexOf('>', html.indexOf('>Task<'));
  const sessionHeader = html.indexOf('>Session<');
  assert.ok(prdLast > 0 && taskHeader > prdLast, 'prd.* section must precede Task section');
  assert.ok(html.indexOf('value="task.session_started"') > sessionHeader, 'session rows must be in the Session section');

  // Checked state mirrors the stored list; default marker on default kinds.
  assert.match(html, /value="prd\.blocked" checked/);
  assert.doesNotMatch(html, /value="prd\.active" checked/);
  const markers = (html.match(/\(default\)/g) || []).length;
  assert.equal(markers, 5, 'one (default) marker per default kind');

  // Inherit on → every row disabled.
  const inh = run(`_renderNotifyKindOptions(['prd.blocked'], true)`);
  const disabled = (inh.match(/disabled>/g) || []).length;
  assert.equal(disabled, 28, 'inherit mode disables every kind checkbox');
  const free = run(`_renderNotifyKindOptions([], false)`);
  assert.equal((free.match(/disabled>/g) || []).length, 0, 'no inherited rows when inherit is off');
});

test('_prdNotifyInheritToggle disables kind checkboxes and shows the effective line', t => {
  const inputs = [{ value: 'prd.blocked', disabled: false }, { value: 'task.completed', disabled: false }];
  const effEl = { style: {} };
  const byId = { prdSettingsNotifyEffective: effEl };
  const { sandbox, run } = setupApp({ byId });
  sandbox.document.querySelectorAll = sel =>
    (sel && String(sel).includes('data-notify-kind')) ? inputs : [];

  run(`_prdNotifyInheritToggle(true)`);
  assert.equal(effEl.style.display, 'block', 'effective line visible while inheriting');
  assert.ok(inputs.every(i => i.disabled === true), 'all kind checkboxes disabled while inheriting');

  run(`_prdNotifyInheritToggle(false)`);
  assert.equal(effEl.style.display, 'none', 'effective line hidden when explicit');
  assert.ok(inputs.every(i => i.disabled === false), 'all kind checkboxes re-enabled');
});

test('detail line: label, kinds and source per source (renderDetailHeader)', t => {
  const cases = [
    { source: 'automaton', text: 'this Automaton' },
    { source: 'profile', text: 'guardrail profile' },
    { source: 'global', text: 'global default' },
  ];
  for (const c of cases) {
    const { run } = setupApp();
    const html = run(`_renderDetailHeader({ title: 'T', notify_events_effective: { source: "${c.source}", events: ["prd.blocked","task.completed"] } }, '', '')`);
    assert.match(html, /Notification events/, `line missing for source ${c.source}`);
    assert.match(html, /prd\.blocked, task\.completed/);
    assert.match(html, new RegExp(c.text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')));
  }
});

test('detail line: empty effective events renders "none"; missing field omits the line', t => {
  const a = setupApp();
  const htmlA = a.run(`_renderDetailHeader({ title: 'T', notify_events_effective: { source: 'global', events: [] } }, '', '')`);
  assert.match(htmlA, /Notification events/);
  assert.match(htmlA, /none/);

  const b = setupApp();
  const htmlB = b.run(`_renderDetailHeader({ title: 'T' }, '', '')`);
  assert.doesNotMatch(htmlB, /Notification events/);
});

test('detail line: effective event kinds are HTML-escaped', t => {
  const { run } = setupApp();
  const evil = '<img src=x onerror=alert(1)>';
  const html = run(`_renderDetailHeader({ title: 'T', notify_events_effective: { source: 'automaton', events: [${JSON.stringify(evil)}] } }, '', '')`);
  assert.match(html, /Notification events/);
  assert.doesNotMatch(html, /<img /, 'raw HTML in effective events must be escaped');
  assert.match(html, /&lt;img /);
});

test('settings modal: explicit list renders non-inherit with checked rows only', async t => {
  const byId = {
    prdSettingsNotifyInherit: { checked: false },
    prdSettingsNotifyEffective: { style: {} },
    prdModalForm: makeFormStub(),
  };
  const s = setupApp({
    byId,
    routes: {
      '/api/autonomous/prds/p1': json({
        id: 'p1',
        title: 'T',
        type: '', backend: '', effort: '', model: '',
        notify_events: ['prd.blocked'],
        notify_events_effective: { source: 'automaton', events: ['prd.blocked'] },
      }),
      '/api/skills': json([]),
      '/api/autonomous/prds': json([]),
    },
  });
  try { vm.runInContext(`openPRDSettingsModal('p1')`, s.sandbox); } catch (e) {
    diag(s); throw e;
  }
  await sleep(25);
  // Mount path: apiFetch(prd) → ensureLLMModelLists → _prdMountModal → body.appendChild → form submit listener.
  assert.ok(!!s.modal(), 'modal element should be appended');
  const html = s.modal().innerHTML;
  assert.match(html, /id="prdSettingsNotifyInherit"/, 'inherit checkbox present');
  assert.doesNotMatch(html, /id="prdSettingsNotifyInherit" checked/, 'inherit not selected for an explicit list');
  assert.match(html, /value="prd\.blocked" checked/);
  assert.equal((html.match(/data-notify-kind value="[^"]+/g) || []).length, 28, 'all 28 kind checkboxes rendered');
  assert.equal((html.match(/data-notify-kind[^>]*disabled>/g) || []).length, 0, 'no rows disabled while explicit list is stored');
  const eff = html.indexOf('id="prdSettingsNotifyEffective"');
  assert.match(html.slice(eff, eff + 200), /display:none/, 'effective line hidden while explicit');
});

function bodySafe(b) { return b && typeof b === 'object' ? b : (typeof b === 'string' ? b.slice(0, 120) : b); }
function diag(s) {
  console.error('DIAG calls:', JSON.stringify(s.calls.map(c => [c.method, c.url, bodySafe(c.body)])));
  console.error('DIAG toasts:', JSON.stringify(s.toasts));
}

// assert.ok that dumps the harness's recorded fetch calls + toasts on
// failure — the app.js promise chain swallows rejections into toasts.
function ea(s, cond, msg) {
  if (!cond) diag(s);
  assert.ok(cond, msg);
}

test('settings modal: stored [] renders inherit ON with all rows disabled', async t => {
  const s = setupApp({
    byId: {
      prdSettingsNotifyInherit: { checked: true },
      prdSettingsNotifyEffective: { style: {} },
      prdModalForm: makeFormStub(),
    },
    routes: {
      '/api/autonomous/prds/p1': json({ id: 'p1', notify_events: [], notify_events_effective: { source: 'profile', events: ['prd.blocked', 'prd.completed'] } }),
      '/api/skills': json([]),
      '/api/autonomous/prds': json([]),
    },
  });
  try { vm.runInContext(`openPRDSettingsModal('p1')`, s.sandbox); } catch (e) { diag(s); throw e; }
  await sleep(25);
  const modalEl = s.modal();
  ea(s, !!modalEl, 'modal element should be appended');
  const html = modalEl.innerHTML;
  assert.match(html, /id="prdSettingsNotifyInherit" checked/, 'inherit checkbox checked when the PRD stores an empty list');
  assert.equal((html.match(/data-notify-kind[^>]*disabled>/g) || []).length, 28, 'every kind row disabled in inherit mode');
  const eff = html.indexOf('id="prdSettingsNotifyEffective"');
  assert.match(html.slice(eff, eff + 200), /display:block/, 'effective line visible while inheriting');
  assert.match(html, /prd\.blocked, prd\.completed/);
});

test('settings modal: saving with inherit ON POSTs an empty list to set_notify_events', async t => {
  const s = setupApp({
    byId: {
      prdSettingsNotifyInherit: { checked: true },
      prdSettingsNotifyEffective: { style: {} },
      prdModalForm: makeFormStub(),
    },
    checkedKinds: [],
    routes: {
      '/api/autonomous/prds/p1': json({ id: 'p1', notify_events: ['prd.blocked', 'task.completed'] }),
      '/api/autonomous/prds/p1/set_notify_events': json({}),
      '/api/skills': json([]),
      '/api/autonomous/prds': json([]),
    },
  });
  try { vm.runInContext(`openPRDSettingsModal('p1')`, s.sandbox); } catch (e) { diag(s); throw e; }
  await sleep(25);
  // Invoke the mounted form's submit handler (same path as the Save button).
  const form = s.sandbox.document.getElementById('prdModalForm');
  const handler = form.__listeners && form.__listeners.submit;
  ea(s, !!(form.__listeners && form.__listeners.submit), 'form submit listener registered by _prdMountModal (save-inherit-ON)');
  handler({ preventDefault() {} });
  await sleep(25);
  const put = s.calls.find(c => c.url === '/api/autonomous/prds/p1/set_notify_events' && c.method === 'POST');
  assert.ok(put, 'set_notify_events called');
  assert.deepEqual(put.body, { notify_events: [] });
  assert.ok(s.toasts.some(x => /notification events saved/i.test(x.msg) && x.type === 'success'), 'success toast; toasts=' + JSON.stringify(s.toasts) + ' calls=' + JSON.stringify(s.calls.map(c=>[c.method,c.url])));
});

test('settings modal: saving with inherit OFF POSTs the checked kinds; no call when unchanged', async t => {
  // Changed: stored [task.completed], operator keeps only prd.blocked.
  const s = setupApp({
    byId: {
      prdSettingsNotifyInherit: { checked: false },
      prdSettingsNotifyEffective: { style: {} },
      prdModalForm: makeFormStub(),
    },
    checkedKinds: ['prd.blocked'],
    routes: {
      '/api/autonomous/prds/p1': json({ id: 'p1', notify_events: ['task.completed'] }),
      '/api/autonomous/prds/p1/set_notify_events': json({}),
      '/api/skills': json([]),
      '/api/autonomous/prds': json([]),
    },
  });
  try { vm.runInContext(`openPRDSettingsModal('p1')`, s.sandbox); } catch (e) { diag(s); throw e; }
  await sleep(25);
  {
    const form = s.sandbox.document.getElementById('prdModalForm');
    const handler = form.__listeners && form.__listeners.submit;
    ea(s, !!handler, 'form submit listener registered by _prdMountModal (save-OFF)');
    handler({ preventDefault() {} });
  }
  await sleep(25);
  const put = s.calls.find(c => c.url.endsWith('/set_notify_events'));
  ea(s, !!put, 'set_notify_events called (save-OFF)');
  assert.deepEqual(put.body, { notify_events: ['prd.blocked'] });

  // Unchanged: stored and checked agree → zero calls (no no-op save).
  const s2 = setupApp({
    byId: {
      prdSettingsNotifyInherit: { checked: false },
      prdSettingsNotifyEffective: { style: {} },
      prdModalForm: makeFormStub(),
    },
    checkedKinds: ['prd.blocked'],
    routes: {
      '/api/autonomous/prds/p1': json({ id: 'p1', notify_events: ['prd.blocked'] }),
      '/api/autonomous/prds/p1/set_notify_events': json({}),
      '/api/skills': json([]),
      '/api/autonomous/prds': json([]),
    },
  });
  try { vm.runInContext(`openPRDSettingsModal('p1')`, s2.sandbox); } catch (e) { diag(s2); throw e; }
  await sleep(25);
  s2.sandbox.document.getElementById('prdModalForm').__listeners.submit({ preventDefault() {} });
  await sleep(25);
  assert.ok(!s2.calls.some(c => c.url.endsWith('/set_notify_events')), 'no-op save must not call the API');
});

test('settings modal: failed save surfaces the localized failure toast', async t => {
  const s = setupApp({
    byId: {
      prdSettingsNotifyInherit: { checked: false },
      prdSettingsNotifyEffective: { style: {} },
      prdModalForm: makeFormStub(),
    },
    checkedKinds: ['prd.blocked'],
    routes: {
      '/api/autonomous/prds/p1': json({ id: 'p1', notify_events: ['task.completed'] }),
      '/api/autonomous/prds/p1/set_notify_events': (m, b) => fail('boom', 400),
      '/api/skills': json([]),
      '/api/autonomous/prds': json([]),
    },
  });
  try { vm.runInContext(`openPRDSettingsModal('p1')`, s.sandbox); } catch (e) { diag(s); throw e; }
  await sleep(25);
  s.sandbox.document.getElementById('prdModalForm').__listeners.submit({ preventDefault() {} });
  await sleep(25);
  assert.ok(s.calls.some(c => c.url.endsWith('/set_notify_events')), 'save attempted');
  const bad = s.toasts.find(x => x.type === 'error');
  assert.ok(bad, 'error toast shown');
  assert.match(bad.msg, /Could not save notification events/);
});

test('guardrail profile panel: renders the profile notify line and edit action', async t => {
  const panelEl = makeStubElement();
  const s = setupApp({
    byId: {
      automataGuardrailProfilesPanel: panelEl,
      automataSettingsScanPanel: makeStubElement(),
    },
    routes: {
      '/api/autonomous/guardrails': json([]),
      '/api/autonomous/guardrail_profiles': json([
        { id: 'g1', name: 'Standard', description: 'core guardrails', guardrails: ['quality'], notify_events: ['prd.blocked', 'prd.failed'] },
        { id: 'g2', name: 'Quiet', guardrails: [], notify_events: [] },
      ]),
    },
  });
  vm.runInContext(`loadGuardrailProfilesPanel()`, s.sandbox);
  await sleep(25);
  const html = panelEl.innerHTML;
  assert.match(html, /Standard/);
  assert.match(html, /Notification events: prd\.blocked, prd\.failed/, 'profile with explicit kinds lists them');
  assert.match(html, /Notification events: Inherit/, 'profile with no kinds shows inherit wording');
  assert.match(html, /Edit notification events/);
});

test('guardrail profile edit: PUTs name/description/guardrails along with notify_events', async t => {
  const s = setupApp({
    byId: {
      automataGuardrailProfilesPanel: makeStubElement(),
      automataSettingsScanPanel: makeStubElement(),
    },
    routes: {
      '/api/autonomous/guardrails': json([]),
      '/api/autonomous/guardrail_profiles': json([
        { id: 'g1', name: 'Standard', description: 'core', guardrails: ['quality'], notify_events: ['prd.blocked'] },
      ]),
      '/api/autonomous/guardrail_profiles/g1': (m, b) => m === 'PUT' ? json({}) : fail('nf', 404),
    },
  });
  s.sandbox.prompt = (msg) => msg && /Notification events/i.test(msg) ? 'prd.blocked, prd.failed' : (msg && /Profile name/i.test(msg) ? 'Standard' : '');
  vm.runInContext(`loadGuardrailProfilesPanel()`, s.sandbox);
  await sleep(25);
  vm.runInContext(`guardrailProfileEditNotify('g1')`, s.sandbox);
  await sleep(25);
  const put = s.calls.find(c => c.url === '/api/autonomous/guardrail_profiles/g1' && c.method === 'PUT');
  ea(s, !!put, 'PUT issued (guardrail profile edit)');
  assert.deepEqual(put.body, { name: 'Standard', description: 'core', guardrails: ['quality'], notify_events: ['prd.blocked', 'prd.failed'] });
  assert.ok(s.toasts.some(x => /notification events saved/i.test(x.msg) && x.type === 'success'));
});

test('guardrail profile create: third prompt is the notify-events CSV and travels as notify_events', async t => {
  const prompts = [];
  const s = setupApp({
    routes: {
      '/api/autonomous/guardrail_profiles': (m, b) => m === 'POST' ? json({}) : json([]),
    },
  });
  s.sandbox.prompt = (msg) => {
    prompts.push(msg);
    if (/Profile name/i.test(msg || '')) return 'Fresh';
    if (/Notification events/i.test(msg || '')) return 'prd.blocked, prd.completed';
    return ''; // guardrail list prompt
  };
  vm.runInContext(`guardrailProfileOpenCreate()`, s.sandbox);
  await sleep(25);
  assert.ok(prompts.some(p => /Notification events/i.test(p)), 'a third prompt asks for notification events (blank = inherit)');
  const post = s.calls.find(c => c.url === '/api/autonomous/guardrail_profiles' && c.method === 'POST');
  assert.ok(post, 'profile create POST');
  assert.deepEqual(post.body, { name: 'Fresh', guardrails: [], notify_events: ['prd.blocked', 'prd.completed'] });
});

test('guardrail profile delete: confirmed DELETE hits the profile endpoint', async t => {
  let confirmed = false;
  const s = setupApp({
    routes: {
      '/api/autonomous/guardrail_profiles/g1': (m) => m === 'DELETE' ? json(null, 204) : fail('nf', 404),
      '/api/autonomous/guardrail_profiles': json([]),
      '/api/autonomous/guardrails': json([]),
    },
  });
  s.sandbox.confirm = () => { confirmed = true; return true; };
  vm.runInContext(`guardrailProfileDelete('g1')`, s.sandbox);
  await sleep(25);
  assert.ok(confirmed, 'confirm() dialog asked');
  assert.ok(s.calls.some(c => c.url === '/api/autonomous/guardrail_profiles/g1' && c.method === 'DELETE'), 'DELETE issued');
});
