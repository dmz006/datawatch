// Settings → Automata — Task routing card, and the Automaton detail routing column.
//
// Covers the editor markup (one row per task type, selects fed from the LLM
// registry), the profile switch / save / delete / export / import wiring, inline
// server validation errors, the preview for a draft, the detail routing column
// and its phone layout, and locale parity. Rendered text is the real English bundle.

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

const TYPES = ['docs_set', 'ui_code', 'final_gate'];
function view(extra) {
  return Object.assign({
    enabled: true, active_profile: 'default', task_types: TYPES,
    profiles: {
      default: {
        docs_set: { worker_backend: 'pi-prompt', worker_model: 'qwen' },
        ui_code: { worker_backend: 'claude-code', worker_model: 'sonnet', verification_backend: 'pi-prompt', effort: 'high', second_choice: { backend: 'pi-prompt', model: 'qwen' } },
        final_gate: {},
      },
      thrifty: { docs_set: { worker_backend: 'pi-prompt' } },
    },
    validation: { valid: true },
  }, extra || {});
}
const CTX = {
  backends: ['claude-code', 'pi-prompt'],
  modelsByBackend: { 'claude-code': ['sonnet', 'opus'], 'pi-prompt': ['qwen'] },
  drafts: [{ id: 'd1', title: 'Add <b>x</b>' }],
};

function stubFetch(sb, handler) {
  const calls = [];
  sb.apiFetch = (url, opts) => { calls.push([url, opts]); return Promise.resolve(handler(url, opts)); };
  vm.runInContext('apiFetch = globalThis.apiFetch', sb);
  return calls;
}

test('card has the enable toggle, profile bar, one row per task type and the preview control', () => {
  const sb = loadApp();
  const html = sb.taskRoutingCardHtml(view(), CTX, null, []);
  assert.match(html, /id="trEnabled" checked/);
  assert.match(html, /id="trProfile"/);
  assert.match(html, /<option value="default" selected>default<\/option>/);
  assert.match(html, /<option value="thrifty">thrifty<\/option>/);
  assert.match(html, /running|already running/);
  const rows = html.match(/<tr class="task-routing-row[^"]*" data-type="([^"]*)"/g).map(r => r.match(/data-type="([^"]*)"/)[1]);
  assert.deepEqual(rows, TYPES);
  for (const cls of ['task-routing-save-profile', 'task-routing-delete-profile', 'task-routing-export', 'task-routing-import']) assert.match(html, new RegExp(cls));
  assert.match(html, /id="trImportFile" type="file"/);
  assert.match(html, /id="trPreviewAutomaton"/);
  assert.match(html, /taskRoutingPreview\(\)/);
  assert.match(html, /Automaton/);
  assert.doesNotMatch(html, /\bPRDs?\b/);
});

test('row selects come from the registry, keep the stored choice and show effort and second choice', () => {
  const sb = loadApp();
  const row = sb.taskRoutingRowHtml('ui_code', view().profiles.default.ui_code, CTX, []);
  assert.match(row, /data-field="worker_backend"[^>]*>(?:(?!<\/select>)[\s\S])*<option value="claude-code" selected>/);
  assert.match(row, /data-field="worker_model"[^>]*>(?:(?!<\/select>)[\s\S])*<option value="sonnet" selected>/);
  assert.match(row, /data-field="verification_backend"[^>]*>(?:(?!<\/select>)[\s\S])*<option value="pi-prompt" selected>/);
  assert.match(row, /data-field="effort"[^>]*>(?:(?!<\/select>)[\s\S])*<option value="high" selected>/);
  assert.match(row, /data-field="second_backend"[^>]*>(?:(?!<\/select>)[\s\S])*<option value="pi-prompt" selected>/);
  // a model the registry no longer lists stays visible
  const stale = sb.taskRoutingRowHtml('ui_code', { worker_backend: 'claude-code', worker_model: 'gone-model' }, CTX, []);
  assert.match(stale, /<option value="gone-model" selected>gone-model<\/option>/);
});

test('server validation errors show inline on the row they name and escape their text', () => {
  const sb = loadApp();
  const problems = [
    'task_routing: profile "default" row "ui_code": worker names model "opus", which needs allow_premium_model set on this row',
    'task_routing: profile "default" row "docs_set": worker names LLM backend "<img onerror=x>", which is not known',
    'task_routing: active profile "zzz" does not exist',
  ];
  const html = sb.taskRoutingCardHtml(view(), CTX, null, problems);
  assert.match(html, /data-type="ui_code"[\s\S]*?class="task-routing-row-error"[^>]*>[^<]*allow_premium_model/);
  assert.match(html, /task-routing-row-invalid" data-type="docs_set"/);
  assert.doesNotMatch(html, /<img onerror/);
  assert.match(html, /&lt;img onerror=x&gt;/);
  assert.match(html, /class="task-routing-invalid"[^>]*>[^<]*active profile &quot;zzz&quot;/);
  assert.doesNotMatch(html, /row "ui_code": worker/); // the general list holds only the table-wide problems
});

test('taskRoutingErrorParts reads the JSON error body and falls back to plain text', () => {
  const sb = loadApp();
  assert.deepEqual(JSON.parse(JSON.stringify(sb.taskRoutingErrorParts(new Error('{"error":"bad table","problems":["a","b"]}')))), { message: 'bad table', problems: ['a', 'b'] });
  assert.deepEqual(JSON.parse(JSON.stringify(sb.taskRoutingErrorParts(new Error('boom')))), { message: 'boom', problems: [] });
});

test('saving a table that the server refuses shows the problems inline', async () => {
  const sb = loadApp();
  const panel = { innerHTML: '' };
  sb.document.getElementById = id => (id === 'automataTaskRoutingPanel' ? panel : null);
  stubFetch(sb, url => {
    if (url === '/api/autonomous/task-routing') return view();
    if (url === '/api/llms') return { llms: [{ name: 'claude-code', kind: 'claude-code', model: 'sonnet' }, { name: 'pi-prompt', kind: 'pi-prompt' }] };
    return null;
  });
  await sb.loadAutomataTaskRoutingPanel();
  assert.match(panel.innerHTML, /task-routing-row/);
  sb.apiFetch = (url, opts) => (opts && opts.method === 'PUT'
    ? Promise.reject(new Error(JSON.stringify({ error: 'the routing table is not valid; nothing was changed', problems: ['task_routing: profile "default" row "ui_code": worker names model "opus", which needs allow_premium_model set on this row'] })))
    : Promise.resolve(null));
  vm.runInContext('apiFetch = globalThis.apiFetch', sb);
  sb.taskRoutingEditField('ui_code', 'worker_model', 'opus');
  await sb.taskRoutingSaveTable();
  assert.match(panel.innerHTML, /task-routing-row-invalid" data-type="ui_code"/);
  assert.match(panel.innerHTML, /allow_premium_model/);
});

test('editing a row changes the draft only; a backend change clears its model; save PUTs the whole table', async () => {
  const sb = loadApp();
  const panel = { innerHTML: '' };
  sb.document.getElementById = id => (id === 'automataTaskRoutingPanel' ? panel : null);
  const calls = stubFetch(sb, url => (url === '/api/autonomous/task-routing' ? view() : { llms: [] }));
  await sb.loadAutomataTaskRoutingPanel();
  sb.taskRoutingEditField('ui_code', 'worker_backend', 'pi-prompt');
  sb.taskRoutingEditField('ui_code', 'worker_model', 'qwen');
  sb.taskRoutingEditField('ui_code', 'allow_premium_model', true);
  sb.taskRoutingEditField('docs_set', 'second_backend', 'claude-code');
  calls.length = 0;
  await sb.taskRoutingSaveTable();
  const put = calls.find(c => c[1] && c[1].method === 'PUT');
  assert.equal(put[0], '/api/autonomous/task-routing');
  const body = JSON.parse(put[1].body);
  assert.equal(body.active_profile, 'default');
  assert.equal(body.enabled, true);
  assert.deepEqual(body.profiles.default.ui_code.worker_backend, 'pi-prompt');
  assert.equal(body.profiles.default.ui_code.worker_model, 'qwen');
  assert.equal(body.profiles.default.ui_code.allow_premium_model, true);
  assert.deepEqual(body.profiles.default.docs_set.second_choice, { backend: 'claude-code' });
  assert.deepEqual(Object.keys(body.profiles).sort(), ['default', 'thrifty']);
});

test('switching the profile calls the active-profile endpoint', async () => {
  const sb = loadApp();
  const calls = stubFetch(sb, url => (url === '/api/autonomous/task-routing' ? view({ active_profile: 'thrifty' }) : { active_profile: 'thrifty' }));
  await sb.taskRoutingSwitchProfile('thrifty');
  assert.equal(calls[0][0], '/api/autonomous/task-routing/active-profile');
  assert.equal(calls[0][1].method, 'PUT');
  assert.deepEqual(JSON.parse(calls[0][1].body), { name: 'thrifty' });
});

test('save as profile posts the name and the draft rows; an empty name posts nothing', async () => {
  const sb = loadApp();
  const panel = { innerHTML: '' };
  const name = { value: '  nightly ' };
  sb.document.getElementById = id => (id === 'automataTaskRoutingPanel' ? panel : id === 'trProfileName' ? name : null);
  const calls = stubFetch(sb, url => (url === '/api/autonomous/task-routing' ? view() : { llms: [] }));
  await sb.loadAutomataTaskRoutingPanel();
  calls.length = 0;
  await sb.taskRoutingSaveProfileAs();
  const post = calls.find(c => c[0] === '/api/autonomous/task-routing/profiles');
  assert.equal(post[1].method, 'POST');
  const body = JSON.parse(post[1].body);
  assert.equal(body.name, 'nightly');
  assert.equal(body.rows.docs_set.worker_backend, 'pi-prompt');
  name.value = '   ';
  calls.length = 0;
  await sb.taskRoutingSaveProfileAs();
  assert.equal(calls.length, 0);
});

test('delete profile asks first, then calls DELETE on the active profile', async () => {
  const sb = loadApp();
  const calls = stubFetch(sb, url => (url === '/api/autonomous/task-routing' ? view() : { llms: [] }));
  sb.document.getElementById = id => (id === 'automataTaskRoutingPanel' ? { innerHTML: '' } : null);
  await sb.loadAutomataTaskRoutingPanel();
  let asked = '';
  sb.showConfirmModal = (msg, ok) => { asked = msg; sb._confirmed = ok; };
  vm.runInContext('showConfirmModal = globalThis.showConfirmModal', sb);
  calls.length = 0;
  await sb.taskRoutingDeleteProfile();
  assert.equal(calls.length, 0, 'nothing is deleted before the confirmation');
  assert.match(asked, /default/);
  await sb._confirmed();
  assert.equal(calls[0][0], '/api/autonomous/task-routing/profiles/default');
  assert.equal(calls[0][1].method, 'DELETE');
});

test('export downloads the profile file and import posts the picked file text', async () => {
  const sb = loadApp();
  stubFetch(sb, url => (url === '/api/autonomous/task-routing' ? view() : { llms: [] }));
  sb.document.getElementById = id => (id === 'automataTaskRoutingPanel' ? { innerHTML: '' } : null);
  await sb.loadAutomataTaskRoutingPanel();
  const made = [];
  sb.Blob = function(parts, o) { this.parts = parts; this.type = o.type; };
  sb.URL = { createObjectURL: b => { made.push(b); return 'blob:x'; }, revokeObjectURL() {} };
  const anchor = { click() { this.clicked = true; }, remove() {} };
  sb.document.createElement = () => anchor;
  sb.document.body = { appendChild() {} };
  vm.runInContext('Blob = globalThis.Blob; URL = globalThis.URL', sb);
  const calls = stubFetch(sb, () => ({ format_version: 1, name: 'default', rows: {} }));
  await sb.taskRoutingExport();
  assert.equal(calls[0][0], '/api/autonomous/task-routing/profiles/default/export');
  assert.equal(anchor.download, 'task-routing-default.json');
  assert.ok(anchor.clicked);
  assert.equal(made[0].type, 'application/json');

  const calls2 = stubFetch(sb, url => (url === '/api/autonomous/task-routing' ? view() : { profiles: [] }));
  const input = { files: [{ text: () => Promise.resolve('{"format_version":1,"name":"x","rows":{}}') }], value: 'x' };
  await sb.taskRoutingImportFile(input);
  assert.equal(calls2[0][0], '/api/autonomous/task-routing/profiles/import');
  assert.equal(calls2[0][1].method, 'POST');
  assert.equal(calls2[0][1].body, '{"format_version":1,"name":"x","rows":{}}');
  assert.equal(input.value, '');
});

test('preview for a draft posts the active profile to the dry run and renders the assignments', async () => {
  const sb = loadApp();
  const panel = { innerHTML: '' };
  const result = { innerHTML: '' };
  sb.document.getElementById = id => (id === 'automataTaskRoutingPanel' ? panel : id === 'trPreviewAutomaton' ? { value: 'd1' } : id === 'trPreviewResult' ? result : null);
  const calls = stubFetch(sb, url => (url === '/api/autonomous/task-routing' ? view() : { llms: [] }));
  await sb.loadAutomataTaskRoutingPanel();
  calls.length = 0;
  sb.apiFetch = (url, opts) => { calls.push([url, opts]); return Promise.resolve({ automaton_id: 'd1', entries: [
    { task_id: 't1', title: 'Write <i>docs</i>', task_type: 'docs_set', row: 'docs_set', would_apply: true, result: { worker_backend: 'pi-prompt', worker_model: 'qwen' } },
    { task_id: 't2', title: 'Pinned', task_type: 'ui_code', would_apply: false, reason: 'set by operator' },
  ] }); };
  vm.runInContext('apiFetch = globalThis.apiFetch', sb);
  await sb.taskRoutingPreview();
  assert.equal(calls[0][0], '/api/autonomous/prds/d1/task-routing/dry-run');
  assert.equal(calls[0][1].method, 'POST');
  assert.deepEqual(JSON.parse(calls[0][1].body), { profile: 'default' });
  assert.match(result.innerHTML, /pi-prompt:qwen/);
  assert.match(result.innerHTML, /set by operator/);
  assert.doesNotMatch(result.innerHTML, /<i>docs/);
  assert.match(sb.taskRoutingPreviewFormHtml(CTX.drafts), /Add &lt;b&gt;x&lt;\/b&gt;/);
});

test('detail column shows the task type with the deciding row, or the reason none did', () => {
  const sb = loadApp();
  const decided = { task_id: 't1', task_type: 'ui_code', row: 'ui_code' };
  const none = { task_id: 't2', task_type: 'docs_set', reason: 'set by operator' };
  assert.equal(sb.taskRoutingDetailText(decided), 'ui_code · row ui_code');
  assert.equal(sb.taskRoutingDetailText(none), 'docs_set · no row: set by operator');
  assert.equal(sb.taskRoutingDetailText(null), '');
  assert.match(sb.taskRoutingDetailChipHtml(decided), /class="prd-task-routing-col"/);
  assert.match(sb.taskRoutingDetailLineHtml(none), /class="prd-task-routing-line">docs_set · no row: set by operator</);
  const evil = sb.taskRoutingDetailChipHtml({ task_type: '<script>', reason: '"><b>' });
  assert.doesNotMatch(evil, /<script>|<b>/);
  assert.equal(sb.taskRoutingDetailChipHtml(undefined), '');
});

test('detail read is fetched once per Automaton, cached, and re-renders only when it changed', async () => {
  const sb = loadApp();
  const calls = stubFetch(sb, () => ({ automaton_id: 'a1', tasks: [{ task_id: 't1', task_type: 'tests', row: 'tests' }] }));
  let renders = 0;
  await sb.taskRoutingEnsure('a1', () => renders++);
  await sb.taskRoutingEnsure('a1', () => renders++);
  assert.equal(calls.length, 1);
  assert.equal(calls[0][0], '/api/autonomous/prds/a1/task-routing');
  assert.equal(renders, 1);
  assert.equal(sb.taskRoutingForTask('a1', 't1').task_type, 'tests');
  assert.equal(sb.taskRoutingForTask('a1', 'nope'), undefined);
  // a failed read leaves the column empty rather than breaking the detail view
  sb.apiFetch = () => Promise.reject(new Error('boom'));
  vm.runInContext('apiFetch = globalThis.apiFetch', sb);
  await sb.taskRoutingEnsure('a2', () => renders++);
  assert.equal(sb.taskRoutingForTask('a2', 't1'), undefined);
});

test('task row renders the routing chip and the phone line from the cached read', () => {
  const sb = loadApp();
  vm.runInContext("window._taskRoutingByAutomaton['p1'] = { at: Date.now(), byTask: { t1: { task_id: 't1', task_type: 'ui_code', row: 'ui_code' } }, sig: 'x' }", sb);
  const prd = { id: 'p1', status: 'running', stories: [], story: [] };
  const story = { id: 's1', tasks: [] };
  const html = vm.runInContext('renderTask', sb)(prd, story, { id: 't1', title: 'Do it', status: 'queued', spec: '' }, false);
  assert.match(html, /class="prd-task-routing-col"/);
  assert.match(html, /class="prd-task-routing-line">ui_code · row ui_code</);
  const plain = vm.runInContext('renderTask', sb)(prd, story, { id: 't9', title: 'Other', status: 'queued', spec: '' }, false);
  assert.doesNotMatch(plain, /prd-task-routing/);
});

test('phone layout: stylesheet hides the column for the line, stacks table rows, and has no page scroll', () => {
  const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');
  assert.match(css, /@media \(max-width: 520px\) \{\s*\.prd-task-routing-col \{ display: none; \}\s*\.prd-task-routing-line \{ display: block; \}/);
  assert.match(css, /\.prd-task-routing-line \{ display: none;/);
  assert.match(css, /@media \(max-width: 600px\) \{\s*\.task-routing-table thead \{ display: none; \}/);
  assert.match(css, /\.task-routing-table-wrap \{ max-width: 100%; overflow-x: auto; \}/);
  const sb = loadApp();
  assert.match(sb.taskRoutingTableHtml(TYPES, view().profiles.default, CTX, []), /<div class="task-routing-table-wrap"><table class="task-routing-table">/);
  assert.match(sb.taskRoutingRowHtml('docs_set', {}, CTX, []), /<td class="task-routing-cell" data-label="Worker backend">/);
});

test('settings page mounts the card and loads it', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  assert.match(src, /id="automataTaskRoutingPanel"/);
  assert.match(src, /loadAutomataTaskRoutingPanel\(\);/);
});

test('every task_routing_ key the app uses exists in all five bundles', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  const keys = [...new Set((src.match(/'task_routing_[a-z_]+'/g) || []).map(k => k.slice(1, -1)))];
  assert.ok(keys.length > 20);
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    const missing = keys.filter(k => !(k in b));
    assert.deepEqual(missing, [], lang + ' is missing keys');
  }
});
