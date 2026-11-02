// Settings → Sessions card: the four UI prompt settings — the
// dismiss_ui_prompts toggle (default on) plus the dismiss delay, the clear
// bound and the max attempts, each with a help link into the definitions
// manual, the client-side whole-number floors that mirror the daemon
// (delay ≥ 0 where 0 = send at once, clear bound ≥ 1, max attempts ≥ 1),
// and saving through the existing settings PUT. fetch is stubbed and records
// every request; rendered text is the real English bundle.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const LOCATIONS = ['en', 'de', 'es', 'fr', 'ja'];
const UI_SETTING_KEYS = [
  'settings_dismiss_ui_prompts',
  'settings_ui_prompt_dismiss_delay_seconds',
  'settings_ui_prompt_clear_bound_seconds',
  'settings_ui_prompt_max_attempts',
];

// loadApp returns a sandbox with the app loaded and a recording fetch. routes
// maps "METHOD /path" to {status, body}; an unknown request answers 404.
// Every request is recorded in __calls with its parsed JSON body; every toast
// in __toasts.
function loadApp(routes, byId) {
  const sandbox = buildSandbox({ byId: byId || {} });
  const calls = [];
  sandbox.fetch = (url, opts) => {
    const method = (opts && opts.method) || 'GET';
    calls.push({ url, method, body: opts && opts.body ? JSON.parse(opts.body) : undefined });
    const hit = routes[method + ' ' + url];
    if (!hit) return Promise.resolve({ ok: false, status: 404, statusText: 'Not Found', text: () => Promise.resolve('not found') });
    const status = hit.status || 200;
    return Promise.resolve({
      ok: status < 400,
      status,
      statusText: String(status),
      json: () => Promise.resolve(hit.body),
      text: () => Promise.resolve(typeof hit.body === 'string' ? hit.body : JSON.stringify(hit.body)),
    });
  };
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  sandbox.__calls = calls;
  sandbox.__toasts = [];
  sandbox.showToast = (message, type) => { sandbox.__toasts.push({ message, type }); };
  return sandbox;
}

const CFG = {
  session: {
    max_sessions: 4,
    dismiss_ui_prompts: true,
    ui_prompt_dismiss_delay_seconds: 5,
    ui_prompt_clear_bound_seconds: 30,
    ui_prompt_max_attempts: 2,
  },
};

const PUTS = sb => sb.__calls.filter(c => c.method === 'PUT' && c.url === '/api/config').map(c => ({ url: c.url, body: c.body }));

// theInput returns the rendered <input ...> tag whose attributes precede the
// onchange marker on the card html.
function theInput(html, marker) {
  const i = html.indexOf(marker);
  assert.notEqual(i, -1, 'onchange marker ' + marker);
  const start = html.lastIndexOf('<input', i);
  assert.notEqual(start, -1, 'no <input before the marker');
  return html.slice(start, html.indexOf('>', i));
}

async function renderSessionsCard(extraRoutes) {
  const panel = { innerHTML: '', style: {}, dataset: {} , children: [], childNodes: [],
    classList: { add() {}, remove() {}, contains: () => false },
    appendChild() {}, removeChild() {}, remove() {}, setAttribute() {},
    addEventListener() {}, removeEventListener() {}, getContext: () => null };
  const sb = loadApp(Object.assign({ 'GET /api/config': { body: CFG } }, extraRoutes || {}), { gcfg_sess: panel });
  sb.loadGeneralConfig();
  await flushAsync();
  assert.ok(panel.innerHTML.length > 0, 'the Sessions card rendered something');
  return { sb, panel };
}

test('Sessions card renders the four UI prompt settings with values, help and docs links', async () => {
  const { panel } = await renderSessionsCard();
  const html = panel.innerHTML;
  const docsHref = 'href="/diagrams.html#docs/datawatch-definitions.md#claude-code-interstitial-prompts"';
  const hits = html.split(docsHref).length - 1;
  // One docs link per row plus the card level docs link every settings card now carries.
  assert.equal(hits, 5, 'each of the four rows and the card carry the docs help link');

  // The toggle: English label, help tooltip, saved boolean, value on.
  assert.match(html, /Dismiss Claude Code interstitial prompts/);
  const toggleSeg = html.slice(html.indexOf("onchange=\"saveGeneralField('session.dismiss_ui_prompts', this.checked)\"") - 1400);
  assert.match(toggleSeg, /title="datawatch detects known Claude Code interstitial prompts/);
  assert.match(toggleSeg, /type="checkbox" checked/);

  // Dismiss delay: floor 0 (0 = send at once), loaded value 5.
  let tag = theInput(html, "onchange=\"savePromptNumberSetting('session.ui_prompt_dismiss_delay_seconds', this.value, 0)\"");
  assert.match(tag, /type="number"/);
  assert.match(tag, /value="5"/);
  assert.match(tag, /placeholder="5"/);
  assert.match(tag, /min="0"/);

  // Clear bound: floor 1, loaded value 30.
  tag = theInput(html, "onchange=\"savePromptNumberSetting('session.ui_prompt_clear_bound_seconds', this.value, 1)\"");
  assert.match(tag, /value="30"/);
  assert.match(tag, /placeholder="30"/);
  assert.match(tag, /min="1"/);

  // Max attempts: floor 1, loaded value 2.
  tag = theInput(html, "onchange=\"savePromptNumberSetting('session.ui_prompt_max_attempts', this.value, 1)\"");
  assert.match(tag, /value="2"/);
  assert.match(tag, /placeholder="2"/);
  assert.match(tag, /min="1"/);
});

test('dismiss delay accepts 0 (send at once) and saves it through PUT /api/config', async () => {
  const { sb } = await renderSessionsCard({ 'PUT /api/config': { body: {} } });
  sb.savePromptNumberSetting('session.ui_prompt_dismiss_delay_seconds', '0', 0);
  await flushAsync();
  assert.deepEqual(PUTS(sb), [{ url: '/api/config', body: { 'session.ui_prompt_dismiss_delay_seconds': 0 } }]);
  assert.deepEqual(sb.__toasts.pop(), { message: 'Saved', type: 'success' });
});

test('dismiss delay rejects a negative entry, an empty entry and a fraction without sending', async () => {
  const { sb } = await renderSessionsCard();
  for (const bad of ['-1', '', '2.5']) {
    sb.savePromptNumberSetting('session.ui_prompt_dismiss_delay_seconds', bad, 0);
    await flushAsync();
  }
  assert.equal(PUTS(sb).length, 0, 'no request for an out-of-range value');
  assert.equal(sb.__toasts.length, 3, 'each bad entry is reported');
  for (const toast of sb.__toasts) {
    assert.equal(toast.type, 'error');
    assert.equal(toast.message, 'Enter a whole number of at least 0');
  }
});

test('clear bound accepts 5 and rejects 0', async () => {
  const { sb } = await renderSessionsCard({ 'PUT /api/config': { body: {} } });
  sb.savePromptNumberSetting('session.ui_prompt_clear_bound_seconds', '5', 1);
  await flushAsync();
  assert.deepEqual(PUTS(sb), [{ url: '/api/config', body: { 'session.ui_prompt_clear_bound_seconds': 5 } }]);
  sb.savePromptNumberSetting('session.ui_prompt_clear_bound_seconds', '0', 1);
  await flushAsync();
  assert.equal(PUTS(sb).length, 1, '0 is below the clear-bound floor and is not sent');
  assert.deepEqual(sb.__toasts.pop(), { message: 'Enter a whole number of at least 1', type: 'error' });
});

test('max attempts accepts 3 and rejects 0', async () => {
  const { sb } = await renderSessionsCard({ 'PUT /api/config': { body: {} } });
  sb.savePromptNumberSetting('session.ui_prompt_max_attempts', '3', 1);
  await flushAsync();
  assert.deepEqual(PUTS(sb), [{ url: '/api/config', body: { 'session.ui_prompt_max_attempts': 3 } }]);
  sb.savePromptNumberSetting('session.ui_prompt_max_attempts', '0', 1);
  await flushAsync();
  assert.equal(PUTS(sb).length, 1, '0 is below the max-attempts floor and is not sent');
  assert.equal(sb.__toasts.pop().type, 'error');
});

test('the toggle saves a boolean through the same PUT', async () => {
  const { sb } = await renderSessionsCard({ 'PUT /api/config': { body: {} } });
  sb.saveGeneralField('session.dismiss_ui_prompts', false);
  await flushAsync();
  assert.deepEqual(PUTS(sb), [{ url: '/api/config', body: { 'session.dismiss_ui_prompts': false } }]);
  assert.deepEqual(sb.__toasts.pop(), { message: 'Saved', type: 'success' });
});

test('DOCS_LINKS_BY_KEY maps all four settings to the definitions section', async () => {
  const { sb } = await renderSessionsCard();
  const table = vm.runInContext('DOCS_LINKS_BY_KEY', sb);
  for (const key of UI_SETTING_KEYS) {
    assert.equal(table[key], 'datawatch-definitions.md#claude-code-interstitial-prompts', key);
  }
});

test('the Sessions section definition carries the four settings with the right types', async () => {
  const { sb } = await renderSessionsCard();
  const fields = vm.runInContext('GENERAL_CONFIG_FIELDS', sb);
  const sess = fields.find(s => s.id === 'sess');
  const byKey = Object.fromEntries(sess.fields.map(f => [f.key, f]));
  assert.equal(byKey['session.dismiss_ui_prompts'].type, 'toggle');
  for (const [key, min] of [
    ['session.ui_prompt_dismiss_delay_seconds', 0],
    ['session.ui_prompt_clear_bound_seconds', 1],
    ['session.ui_prompt_max_attempts', 1],
  ]) {
    assert.equal(byKey[key].type, 'number', key);
    assert.equal(byKey[key].min, min, key);
    assert.match(byKey[key].onchange, new RegExp("savePromptNumberSetting\\('" + key + "', this\\.value, " + min + "\\)"));
  }
});

test('all five locale bundles carry the eight keys and non-English bundles differ from English', async () => {
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  for (const key of UI_SETTING_KEYS) {
    assert.ok(en[key], 'en has ' + key);
    assert.ok(en[key + '_help'], 'en has ' + key + '_help');
  }
  for (const lang of LOCATIONS.filter(l => l !== 'en')) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    for (const key of UI_SETTING_KEYS) {
      assert.notEqual(bundle[key], undefined, lang + ' has ' + key);
      assert.notEqual(bundle[key], en[key], lang + ' ' + key + ' is not an English copy');
      assert.notEqual(bundle[key + '_help'], undefined, lang + ' has ' + key + '_help');
      assert.notEqual(bundle[key + '_help'], en[key + '_help'], lang + ' ' + key + '_help is not an English copy');
    }
  }
});

test('the new settings strings carry no version or ticket identifiers', async () => {
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  const identifiers = /V\d+\.\d+[0-9.]*|[A-Z]{2,3}-?\d{3,}|\bBL\d+\b/;
  for (const key of UI_SETTING_KEYS) {
    assert.ok(!identifiers.test(en[key]), key + ': no tracker ids');
    assert.ok(!identifiers.test(en[key + '_help']), key + '_help: no tracker ids');
  }
});
