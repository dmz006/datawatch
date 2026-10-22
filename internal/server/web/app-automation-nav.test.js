// Automation tab — navigation and list scaffold.
//
// The Automation tab sits left of the Automata tab, stays hidden until the
// autonomous subsystem is enabled, and renders a minimal list. This test
// covers the nav markup, the functions the section exposes, the status-aware
// request helper (a 409 must surface its status; a 204 resolves null) and the
// escaping in the card builder.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  // t() falls back to the raw key until a bundle loads; install the English
  // one so the rendered strings are the real user-visible ones.
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

test('index.html places the hidden Automation button before the Automata button', () => {
  const html = fs.readFileSync(path.join(__dirname, 'index.html'), 'utf8');
  const automation = html.indexOf('id="navBtnAutomation"');
  const automata = html.indexOf('id="navBtnAutonomous"');
  assert.ok(automation > -1, 'navBtnAutomation is present');
  assert.ok(automation < automata, 'navBtnAutomation comes before navBtnAutonomous');
  const tag = /<button id="navBtnAutomation"[^>]*>/.exec(html)[0];
  assert.match(tag, /data-view="automation"/);
  assert.match(tag, /onclick="navigate\('automation'\)"/);
  assert.match(tag, /display:\s*none/);
  assert.match(html, /data-i18n="nav_automation"/);
});

test('the sandbox exposes the Automation view, request helper and card builder', () => {
  const sb = loadApp();
  assert.equal(typeof sb.renderAutomationView, 'function');
  assert.equal(typeof sb.automationRequest, 'function');
  assert.equal(typeof sb.buildAutomationCardHTML, 'function');
});

test('automationRequest rejects with the HTTP status on a 409 and resolves null on a 204', async () => {
  const sb = loadApp();
  sb.fetch = () => Promise.resolve({
    ok: false, status: 409, statusText: 'Conflict',
    text: () => Promise.resolve('version is stale'),
  });
  await assert.rejects(
    () => sb.automationRequest('PUT', '/api/automations/a1', { name: 'x' }),
    err => err.status === 409 && err.message === 'version is stale',
  );

  sb.fetch = () => Promise.resolve({ ok: true, status: 204 });
  assert.equal(await sb.automationRequest('DELETE', '/api/automations/a1'), null);
});

test('automationRequest sends the token, the method and the JSON body', async () => {
  const sb = loadApp();
  sb.localStorage.setItem('cs_token', 'tok');
  let seen;
  sb.fetch = (url, opts) => {
    seen = { url, opts };
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ id: 'a1' }) });
  };
  const out = await sb.automationRequest('POST', '/api/automations', { name: 'n' });
  assert.deepEqual(JSON.parse(JSON.stringify(out)), { id: 'a1' });
  assert.equal(seen.url, '/api/automations');
  assert.equal(seen.opts.method, 'POST');
  assert.equal(seen.opts.headers.Authorization, 'Bearer tok');
  assert.equal(seen.opts.body, JSON.stringify({ name: 'n' }));
});

test('buildAutomationCardHTML escapes the name and the id', () => {
  const sb = loadApp();
  const html = sb.buildAutomationCardHTML({
    id: 'x" onclick="boom()',
    name: `<script>alert(1)</script> it's "quoted"`,
    status: 'active',
  }, {});
  assert.ok(!html.includes('<script>'), 'raw script tag is escaped');
  assert.ok(html.includes('&lt;script&gt;'));
  assert.ok(!html.includes(`it's`), 'single quote is escaped');
  assert.ok(html.includes('&#39;') && html.includes('&quot;quoted&quot;'));
  assert.ok(!/data-automation-id="x" onclick/.test(html), 'id cannot break out of its attribute');
  assert.match(html, /prd-card-status-active/);
  assert.match(html, />Active</);
});

test('the list renders cards, the empty text and the escaped load failure', async () => {
  const sb = loadApp();
  const panel = { innerHTML: '' };
  sb.document.getElementById = id => (id === 'automationPanel' ? panel : { style: {}, classList: { toggle() {} } });

  sb.fetch = () => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ automations: [{ id: 'a1', name: 'Nightly', status: 'draft' }] }) });
  await sb.loadAutomationList();
  assert.match(panel.innerHTML, /data-automation-id="a1"/);
  assert.match(panel.innerHTML, /Nightly/);

  sb.fetch = () => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve([]) });
  await sb.loadAutomationList();
  assert.match(panel.innerHTML, /No Automations yet/);

  sb.fetch = () => Promise.resolve({ ok: false, status: 500, statusText: 'x', text: () => Promise.resolve('<b>down</b>') });
  await sb.loadAutomationList();
  assert.match(panel.innerHTML, /Could not load Automations: &lt;b&gt;down&lt;\/b&gt;/);
});
