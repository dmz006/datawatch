// Automation milestone marker (app.js). An Automation with milestone set shows a
// badge on its list row and in its detail header; an ordinary arc shows nothing.
// The detail header toggle marks or unmarks it through the Automation update
// path (PUT /api/automations/{id}), echoing the loaded settings so the update
// does not clear them, and re-applying the planning LLM pair through set_llm
// because the update path does not carry it. Every new key exists in all five
// locale bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const APP_JS = path.join(__dirname, 'app.js');
const MILESTONE_KEYS = [
  'automation_milestone_badge',
  'automation_milestone_hint',
  'automation_milestone_mark',
  'automation_milestone_unmark',
];

// loadApp returns a sandbox with app.js loaded, the real English bundle, and a
// recording fetch. routes maps "METHOD path" to a response body; an unknown
// request answers 200 with an empty object.
function loadApp(routes) {
  const sandbox = buildSandbox();
  const calls = [];
  sandbox.fetch = (url, opts) => {
    const method = (opts && opts.method) || 'GET';
    const body = opts && opts.body ? JSON.parse(opts.body) : undefined;
    calls.push({ url, method, body });
    const key = method + ' ' + url;
    const hit = routes && routes[key];
    const payload = hit !== undefined ? hit : {};
    return Promise.resolve({
      ok: true,
      status: 200,
      statusText: 'OK',
      json: () => Promise.resolve(payload),
      text: () => Promise.resolve(JSON.stringify(payload)),
    });
  };
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, 'en.json'), 'utf8'));
  sandbox.__calls = calls;
  return sandbox;
}

// automation returns a loaded Automation with the settings the update path
// overwrites, plus optional extras.
function automation(extra) {
  return Object.assign({
    id: 'a1b2c3d4', name: 'Release train', description: 'Builds then ships', status: 'active', version: 7,
    entry_member_id: 'm1', active_cursors: [],
    members: [
      { id: 'm1', prd_id: 'p1aaaaaaaaaaaa', status: 'completed', added_by: 'operator', on_complete: 'm2', edge_taken: 'on_complete' },
      { id: 'm2', prd_id: 'p2aaaaaaaaaaaa', status: 'pending', added_by: 'operator' },
    ],
    budget_policy: { ceiling_type: 'dollar', ceiling_value: 25, window: 'week', pacing: 'none', on_breach: 'pause' },
    project_dir: '/work/tree', backend: 'claude-code', model: 'opus', effort: 'high',
    verification_backend: 'ollama', verification_model: 'qwen', verification_effort: 'low',
  }, extra || {});
}

function setDetail(sb, a) {
  sb.__a = a;
  vm.runInContext('_automationState.detail = __a; _automationState.detailId = __a.id;', sb);
}

function detailHtml(sb, a) {
  sb.__a = a;
  return vm.runInContext('buildAutomationDetailHTML(__a, { prdTitles: {}, mergeById: {}, now: Date.now() })', sb);
}

function listBadges(sb, a) {
  sb.__a = a;
  return vm.runInContext('automationMilestoneBadgeHtml(__a)', sb);
}

// ---- shown -----------------------------------------------------------------

test('a milestone shows the badge in the list row helper', () => {
  const sb = loadApp();
  const html = listBadges(sb, automation({ milestone: true }));
  assert.match(html, /class="automation-milestone-badge"/);
  assert.match(html, /data-milestone="true"/);
  assert.match(html, />Milestone</);
});

test('a milestone shows the badge in the detail header with the unmark toggle', () => {
  const sb = loadApp();
  const html = detailHtml(sb, automation({ milestone: true }));
  assert.match(html, /class="automation-milestone-badge"/);
  assert.match(html, /class="btn-secondary automation-milestone-toggle"[^>]*data-milestone="true"/);
  assert.match(html, /aria-pressed="true"/);
  assert.match(html, /Unmark milestone/);
});

// ---- hidden when off -------------------------------------------------------

test('an ordinary arc shows no milestone badge in the list row', () => {
  const sb = loadApp();
  assert.equal(listBadges(sb, automation({ milestone: false })), '');
  assert.equal(listBadges(sb, automation()), '');
});

test('an ordinary arc shows no badge in the detail header but keeps the mark toggle', () => {
  const sb = loadApp();
  const html = detailHtml(sb, automation({ milestone: false }));
  assert.doesNotMatch(html, /automation-milestone-badge/);
  assert.match(html, /class="btn-secondary automation-milestone-toggle"[^>]*data-milestone="false"/);
  assert.match(html, /aria-pressed="false"/);
  assert.match(html, /Mark as milestone/);
});

// ---- toggle ----------------------------------------------------------------

test('marking an arc sends a full update through PUT and redraws it as a milestone', async () => {
  const sb = loadApp({ 'PUT /api/automations/a1b2c3d4': automation({ milestone: true, version: 8 }) });
  setDetail(sb, automation());
  const result = vm.runInContext('automationToggleMilestone("a1b2c3d4")', sb);
  await flushAsync();
  const put = sb.__calls.find(c => c.method === 'PUT');
  assert.ok(put, 'a PUT request was sent');
  assert.equal(put.url, '/api/automations/a1b2c3d4');
  assert.equal(put.body.milestone, true);
  assert.equal(put.body.expected_version, 7);
  assert.equal(put.body.actor, 'operator');
  // The loaded settings are echoed so the overwrite keeps them.
  assert.equal(put.body.name, 'Release train');
  assert.equal(put.body.description, 'Builds then ships');
  assert.equal(put.body.entry_member_id, 'm1');
  assert.deepEqual(put.body.budget_policy, { ceiling_type: 'dollar', ceiling_value: 25, window: 'week', pacing: 'none', on_breach: 'pause' });
  assert.equal(put.body.project_dir, '/work/tree');
  assert.equal(put.body.backend, 'claude-code');
  assert.equal(put.body.model, 'opus');
  assert.equal(put.body.effort, 'high');
  assert.equal(put.body.verification_backend, 'ollama');
  assert.equal(put.body.verification_model, 'qwen');
  assert.equal(put.body.verification_effort, 'low');
  // Members, decision assistant and auto tags are left out so the server keeps them.
  assert.equal('members' in put.body, false);
  assert.equal('decision_assistant' in put.body, false);
  assert.equal('auto_tags' in put.body, false);
  // No planning pair was stored, so no set_llm follow-up.
  assert.equal(sb.__calls.filter(c => c.url.endsWith('/set_llm')).length, 0);
  const updated = await result;
  assert.equal(updated.milestone, true);
  assert.equal(vm.runInContext('_automationState.detail.milestone', sb), true);
});

test('unmarking a milestone sends milestone false', async () => {
  const sb = loadApp({ 'PUT /api/automations/a1b2c3d4': automation({ milestone: false, version: 8 }) });
  setDetail(sb, automation({ milestone: true }));
  vm.runInContext('automationToggleMilestone("a1b2c3d4")', sb);
  await flushAsync();
  const put = sb.__calls.find(c => c.method === 'PUT');
  assert.ok(put);
  assert.equal(put.body.milestone, false);
});

test('a planning pair is re-applied through set_llm after the update', async () => {
  const sb = loadApp({
    'PUT /api/automations/a1b2c3d4': automation({ milestone: true, version: 8 }),
    'POST /api/automations/a1b2c3d4/set_llm': automation({ milestone: true, version: 9, planning_backend: 'claude-code', planning_model: 'sonnet' }),
  });
  setDetail(sb, automation({ planning_backend: 'claude-code', planning_model: 'sonnet' }));
  vm.runInContext('automationToggleMilestone("a1b2c3d4")', sb);
  await flushAsync();
  const llm = sb.__calls.find(c => c.url.endsWith('/set_llm'));
  assert.ok(llm, 'set_llm was called');
  assert.equal(llm.method, 'POST');
  assert.deepEqual(
    { planning_backend: llm.body.planning_backend, planning_model: llm.body.planning_model, expected_version: llm.body.expected_version },
    { planning_backend: 'claude-code', planning_model: 'sonnet', expected_version: 8 },
  );
});

test('a toggle for an Automation that is not open sends nothing', async () => {
  const sb = loadApp();
  setDetail(sb, automation());
  const result = vm.runInContext('automationToggleMilestone("other-id")', sb);
  assert.equal(await result, null);
  assert.equal(sb.__calls.filter(c => c.method === 'PUT').length, 0);
});

// ---- locale parity ---------------------------------------------------------

test('every milestone key exists in all five locale bundles with a non-empty value', () => {
  for (const lang of LANGS) {
    const bundle = JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
    for (const key of MILESTONE_KEYS) {
      assert.equal(typeof bundle[key], 'string', lang + ' is missing ' + key);
      assert.ok(bundle[key].trim().length > 0, lang + ' has an empty ' + key);
    }
  }
});

test('the milestone keys match across locales (same key set as English)', () => {
  const en = JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, 'en.json'), 'utf8'));
  for (const lang of LANGS.slice(1)) {
    const bundle = JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
    for (const key of MILESTONE_KEYS) {
      assert.ok(key in bundle, lang + ' lacks ' + key);
      assert.ok(key in en, 'en lacks ' + key);
    }
  }
});
