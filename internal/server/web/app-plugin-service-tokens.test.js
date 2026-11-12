// app-plugin-service-tokens.test.js
//
// Settings → Plugins: the Tokens panel rendered under a service plugin's
// card (window.pluginServiceTokens* in app.js).
//
// Pins the four behaviors that matter most:
//   - each token is listed by name, where it is used (the manifest
//     fields) and whether it exists in the vault — and a `value`, when
//     present in the payload, is never rendered;
//   - Rotate's new-value input is type="password" and is cleared after
//     its value is sent once to POST /tokens/{secret}/rotate;
//   - Remove only hits DELETE /tokens/{secret} after a confirm() step —
//     no confirm at all, or a declined one, sends nothing;
//   - a token name containing a <script> tag is rendered escaped, not
//     as markup.
//
// app.js is ~50k lines with many top-level side effects, so the tests
// run in the same jsdom-free browser sandbox as the rest of the app
// tests (testutil_browser_stub.js).

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  sandbox._i18n.bundle = en;
  sandbox._i18n.fallback = en;
  return sandbox;
}

// apiFetchRecorder replaces the app's apiFetch with a stub that records
// every call and resolves, so the tests can assert on URL + method +
// body without any network.
function recordApiFetch(sandbox) {
  const calls = [];
  sandbox.apiFetch = function (url, opts) {
    calls.push({ url, opts: opts || {} });
    return Promise.resolve({});
  };
  return calls;
}

function fakeInput(value) {
  return { value: value === undefined ? '' : value, focus() {} };
}

// fakeRotateBtn builds the minimal browser-node shape the app's handlers
// expect: the Rotate button whose nextSibling is the rotate slot (a
// type=password input row), matching the real DOM the panel renders.
function fakeRotateBtn() {
  const input = fakeInput('');
  const slot = {
    style: { display: 'none' },
    classList: { contains: (c) => c === 'plugin-token-rotate-slot' },
    querySelector(sel) { return sel === 'input[type=password]' ? input : null; },
    parentElement: null,
  };
  slot.querySelectorAll = () => [input];
  const btn = { nextSibling: slot, parentElement: slot };
  return { btn, slot, input };
}

test('tokens panel lists each token by name, where it is used, and whether it exists — no values', () => {
  const sandbox = loadApp();
  const payload = {
    plugin: 'enrich',
    secrets: [
      { name: 'enrich_token', fields: ['connection.auth.token', 'env.ENRICH_TOKEN'], exists: true },
      { name: 'enrich_api_key', fields: ['env.ENRICH_API_KEY'], exists: false },
    ],
    service_tokens: [{ name: 'enrich_srv_token', created_at: '2026-11-01T14:00:00Z' }],
  };
  const html = sandbox.window.pluginServiceTokensPanelHTML('enrich', payload);
  assert.match(html, /class="plugin-service-tokens"/);
  assert.match(html, /Tokens/);
  assert.match(html, /enrich_token/);
  assert.match(html, /enrich_api_key/);
  // where used: the manifest fields that carry the reference
  assert.match(html, /connection\.auth\.token/);
  assert.match(html, /env\.ENRICH_TOKEN/);
  assert.match(html, /env\.ENRICH_API_KEY/);
  assert.match(html, /Used by/);
  // exists / missing in the vault
  assert.match(html, /in vault/);
  assert.match(html, /missing from vault/);
  // service tokens: name + creation time, no token
  assert.match(html, /enrich_srv_token/);
  assert.match(html, /Service tokens/);
});

test('tokens panel never displays a value, even if the payload carries one', () => {
  const sandbox = loadApp();
  const html = sandbox.window.pluginServiceTokensPanelHTML('enrich', {
    secrets: [{ name: 'enrich_token', fields: ['env.ENRICH_TOKEN'], exists: true, value: 'TOPSECRET-VALUE-123' }],
    service_tokens: [{ name: 'enrich_srv_token', created_at: '2026-11-01T14:00:00Z', token: 'SRVTOKEN-SECRET-456' }],
  });
  assert.doesNotMatch(html, /TOPSECRET-VALUE-123/);
  assert.doesNotMatch(html, /SRVTOKEN-SECRET-456/);
});

test('tokens panel rotate input is type=password; its value is sent once and the input is cleared', async () => {
  const sandbox = loadApp();
  const calls = recordApiFetch(sandbox);
  sandbox.confirm = () => true;
  sandbox.loadPluginsPanel = () => {}; // keep the refresh out of the recorded calls

  // the panel renders a password (masked) input for the new value
  const html = sandbox.window.pluginServiceTokensPanelHTML('enrich', {
    secrets: [{ name: 'enrich_token', fields: [], exists: true }],
  });
  assert.match(html, /type="password"/);
  assert.doesNotMatch(html, /type="text"/);

  // rotate reveals the hidden slot and focuses the input
  const { btn, slot, input } = fakeRotateBtn();
  let focused = false;
  input.focus = () => { focused = true; };
  sandbox.window.pluginServiceTokensRotate(btn, 'enrich', 'enrich_token');
  assert.equal(slot.style.display, 'flex');
  assert.equal(focused, true);

  // save sends the value exactly once to the rotate endpoint, then
  // clears the input
  input.value = 'NEW-TOKEN-VALUE';
  sandbox.window.pluginServiceTokensRotateSubmit('enrich', 'enrich_token', input);
  await flushAsync();
  assert.equal(input.value, '');
  assert.equal(calls.length, 1, `expected one apiFetch call, got ${JSON.stringify(calls)}`);
  assert.match(calls[0].url, /\/api\/plugins\/enrich\/tokens\/enrich_token\/rotate$/);
  assert.equal((calls[0].opts || {}).method, 'POST');
  assert.deepEqual(JSON.parse(calls[0].opts.body), { value: 'NEW-TOKEN-VALUE' });

  // an empty new value is refused locally — nothing is sent
  const callsBefore = calls.length;
  sandbox.window.pluginServiceTokensRotateSubmit('enrich', 'enrich_token', fakeInput(''));
  await flushAsync();
  assert.equal(calls.length, callsBefore);
});

test('token remove requires confirmation (declined or absent → no DELETE)', async () => {
  const sandbox = loadApp();
  const calls = recordApiFetch(sandbox);
  sandbox.loadPluginsPanel = () => {};
  sandbox.pushToAlertDock = () => {};

  // no confirm provided at all → nothing is sent
  sandbox.window.pluginServiceTokensRemove('enrich', 'enrich_token');
  assert.equal(calls.length, 0);

  // confirm declined → nothing is sent
  sandbox.confirm = () => false;
  sandbox.window.pluginServiceTokensRemove('enrich', 'enrich_token');
  assert.equal(calls.length, 0);

  // confirm accepted → exactly one DELETE to the token endpoint
  let confirmMsg = '';
  sandbox.confirm = (m) => { confirmMsg = String(m); return true; };
  sandbox.window.pluginServiceTokensRemove('enrich', 'enrich_token');
  assert.equal(calls.length, 1);
  assert.match(calls[0].url, /\/api\/plugins\/enrich\/tokens\/enrich_token$/);
  assert.equal(calls[0].url.includes('/rotate'), false, 'remove must not hit the rotate endpoint');
  assert.equal((calls[0].opts || {}).method, 'DELETE');
  assert.equal((calls[0].opts || {}).body, undefined);
  // the confirmation step names the token it is about to remove
  assert.match(confirmMsg, /enrich_token/);
});

test('token name with a <script> tag is rendered escaped, not as markup', () => {
  const sandbox = loadApp();
  const evilName = '<script>alert(1)</script>';
  const html = sandbox.window.pluginServiceTokensPanelHTML('enrich', {
    secrets: [{ name: evilName, fields: ['env.X'], exists: true }],
  });
  // the raw script tag must not survive into the rendered HTML …
  assert.doesNotMatch(html, /<script>alert\(1\)<\/script>/);
  // … but the escaped form is there, as inert text.
  assert.match(html, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
  // the plugin name is escaped in the inline handlers too
  assert.doesNotMatch(html, /onclick="pluginServiceTokensRemove\('<script/);
});

test('tokens panel with no secrets shows the empty state, not a broken row', () => {
  const sandbox = loadApp();
  const html = sandbox.window.pluginServiceTokensPanelHTML('enrich', { secrets: [] });
  assert.match(html, /class="plugin-service-tokens"/);
  assert.match(html, /references no vault secrets/);
  // null payload → no row of 'undefined' either
  const nullHtml = sandbox.window.pluginServiceTokensPanelHTML('enrich', null);
  assert.match(nullHtml, /references no vault secrets/);
  assert.doesNotMatch(nullHtml, /undefined/);
});
