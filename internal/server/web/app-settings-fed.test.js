// Settings-tab federation pass (2026-10-08, item 3 of the operator's
// 3-item queue: GH#194 -> v8.73.10, Dashboard/Observer denial-coverage
// extension -> v8.73.11, this round). The original directive's literal
// subject: Settings had NO server picker at all, and ~24 of its ~45
// sub-card loaders used a raw, non-proxy-aware fetch() that never
// reflected a selected remote peer's own config -- several with no
// error handling whatsoever (an unhandled rejection, stuck on the
// loading placeholder forever).
//
// Not every one of the ~24 migrated functions gets its own test here --
// most share one of a small number of mechanical patterns. This file
// covers each distinct pattern once:
//   - the picker itself being added to the view
//   - simple raw-fetch -> apiFetch + real-error-on-catch (loadConfigStatus)
//   - the multi-section per-field-group pattern that fills every
//     section's element with the error (loadCommsConfig)
//   - the "already apiFetch, catch just needed the real message"
//     pattern (loadGuardrailProfilesPanel)
//   - the already-proxy-aware, already-showing-the-real-error functions
//     that needed NO change (loadComputeNodesPanel, loadLLMsPanel,
//     loadSecretsPanel) -- confirmed, not just assumed
//
// See docs/plans/historical-plans/2026-10-08-settings-tab-federation.md for the full
// per-function inventory.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, makeStubElement, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox.renderSettingsView, 'function', 'renderSettingsView is not defined -- did it get renamed?');
  assert.equal(typeof sandbox._fedMsg, 'function', '_fedMsg is not defined -- did it get renamed?');
  return sandbox;
}

function fakeResponse(status, body) {
  return { ok: status >= 200 && status < 300, status, text: () => Promise.resolve(body || ''), json: () => Promise.resolve(JSON.parse(body || '{}')) };
}

test('renderSettingsView: injects the server picker bar (the view had none before this pass)', () => {
  const sandbox = loadAppJS();
  const viewEl = makeStubElement();
  viewEl.querySelector = () => null; // no existing bar yet
  sandbox.document.getElementById = (id) => (id === 'view' ? viewEl : makeStubElement());
  vm.runInContext(`state.servers = { servers: [{ name: 'host-a', enabled: true }] };`, sandbox);
  let injected = false;
  const origInject = sandbox._injectServerPickerBar;
  sandbox._injectServerPickerBar = function (el, fn, opts) {
    injected = true;
    assert.equal(opts && opts.hideAll, true, 'Settings should use hideAll, like Observer -- an aggregated "All servers" view of ~45 independent config sections has no coherent meaning');
    return origInject.apply(this, arguments);
  };
  vm.runInContext('renderSettingsView()', sandbox);
  assert.equal(injected, true, '_injectServerPickerBar should be called from renderSettingsView');
});

test('loadConfigStatus: a federated capability error reaches the card, not a generic "Config unavailable"', async () => {
  const sandbox = loadAppJS();
  // Real code sets .textContent (correctly -- plain text, no markup to
  // escape) but makeStubElement()'s textContent setter is a no-op (it
  // only captures .innerHTML); use a minimal custom stub with a real
  // mutable textContent property instead of fighting the shared stub's
  // read-only property descriptor.
  let captured = '';
  const el = { set textContent(v) { captured = v; }, get textContent() { return captured; } };
  sandbox.document.getElementById = (id) => (id === 'configStatus' ? el : makeStubElement());
  vm.runInContext(`state.activeServer = 'host-a';`, sandbox);
  sandbox.fetch = (url) => url.includes('/api/config')
    ? Promise.resolve(fakeResponse(403, 'federation peer lacks capability: config:read'))
    : Promise.resolve(fakeResponse(200, '{}'));
  vm.runInContext('loadConfigStatus()', sandbox);
  await flushAsync();
  assert.match(captured, /config:read/, `expected the real capability error; got: ${captured}`);
});

test('loadConfigStatus: proxies through apiFetch -- a selected remote server\'s URL is actually hit', async () => {
  const sandbox = loadAppJS();
  sandbox.document.getElementById = () => makeStubElement();
  vm.runInContext(`state.activeServer = 'host-a';`, sandbox);
  let calledUrl = null;
  sandbox.fetch = (url) => { calledUrl = url; return Promise.resolve(fakeResponse(200, '{}')); };
  vm.runInContext('loadConfigStatus()', sandbox);
  await flushAsync();
  assert.equal(calledUrl, '/api/proxy/host-a/api/config', 'loadConfigStatus was a raw, non-proxy-aware fetch before this pass -- it must now proxy like every other apiFetch-based loader');
});

test('loadCommsConfig: a federated denial fills every COMMS_CONFIG_FIELDS section with the real error', async () => {
  const sandbox = loadAppJS();
  const sectionIds = vm.runInContext('COMMS_CONFIG_FIELDS.map(s => s.id)', sandbox);
  assert.ok(sectionIds.length > 0, 'COMMS_CONFIG_FIELDS should be non-empty');
  const els = {};
  sandbox.document.getElementById = (id) => {
    if (id.startsWith('ccfg_')) { els[id] = els[id] || makeStubElement(); return els[id]; }
    return makeStubElement();
  };
  vm.runInContext(`state.activeServer = 'host-a';`, sandbox);
  sandbox.fetch = (url) => url.includes('/api/config')
    ? Promise.resolve(fakeResponse(403, 'federation peer lacks capability: config:read'))
    : Promise.resolve(fakeResponse(200, '[]'));
  vm.runInContext('loadCommsConfig()', sandbox);
  await flushAsync();
  const firstId = 'ccfg_' + sectionIds[0];
  assert.match(els[firstId].innerHTML, /config:read/, `expected the real error in the first comms section; got: ${els[firstId] && els[firstId].innerHTML}`);
});

test('loadGuardrailProfilesPanel: a federated capability error reaches the panel (was already apiFetch-based, just needed the real message)', async () => {
  const sandbox = loadAppJS();
  const panel = makeStubElement();
  sandbox.document.getElementById = (id) => (id === 'automataGuardrailProfilesPanel' ? panel : makeStubElement());
  vm.runInContext(`state.activeServer = 'host-a';`, sandbox);
  sandbox.fetch = (url) => url.includes('/api/autonomous/guardrail_profiles')
    ? Promise.resolve(fakeResponse(403, 'federation peer lacks capability: guardrails:read'))
    : Promise.resolve(fakeResponse(200, '[]'));
  vm.runInContext('loadGuardrailProfilesPanel()', sandbox);
  await flushAsync();
  assert.match(panel.innerHTML, /guardrails:read/, `expected the real capability error; got: ${panel.innerHTML}`);
});

// ── Already-correct loaders: confirmed, not assumed ─────────────────────

test('loadComputeNodesPanel / loadLLMsPanel / loadSecretsPanel: already proxy-aware AND already show the real error (no change needed, confirmed by inspection)', async () => {
  const sandbox = loadAppJS();
  for (const [fn, elId, url] of [
    ['loadComputeNodesPanel', 'computeNodesPanel', '/api/compute/nodes'],
    ['loadSecretsPanel', 'secretsListPanel', '/api/secrets'],
  ]) {
    const el = makeStubElement();
    sandbox.document.getElementById = (id) => (id === elId ? el : makeStubElement());
    vm.runInContext(`state.activeServer = 'host-a';`, sandbox);
    sandbox.fetch = (u) => u.includes(url)
      ? Promise.resolve(fakeResponse(403, `federation peer lacks capability: test:${fn}`))
      : Promise.resolve(fakeResponse(200, '{}'));
    vm.runInContext(`${fn}()`, sandbox);
    await flushAsync();
    assert.match(el.innerHTML, new RegExp(`test:${fn}`), `${fn} should already show the real capability error; got: ${el.innerHTML}`);
  }
});
