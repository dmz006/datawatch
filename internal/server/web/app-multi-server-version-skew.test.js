// app-multi-server-version-skew.test.js
//
// Multi-server version skew: the operator can point the UI at a federated
// peer (state.activeServer = a peer name) whose daemon runs a DIFFERENT
// build than the daemon serving this page. Three things must hold, and
// none of them did before the per-server version ledger and proxy-aware
// Settings fetches landed (regressions this file pins):
//
//   1. A peer's reported version is recorded under the peer's own key —
//      never as the page's own daemon version — and never triggers the
//      update banner or location.reload(). Switching back to local and
//      re-reporting the local version must not read as an update either.
//   2. A REAL build change of the daemon serving THIS page takes the
//      update path (reload or banner) exactly once; re-reporting the same
//      version (e.g. the first frame after a reconnect) never re-fires.
//   3. While a peer is selected, every Settings tab fetches the PEER's
//      data through /api/proxy/<peer>/ (About version, config viewer,
//      general, plugins, comms, compute, automata), and the informational
//      version-skew note appears only when the two builds differ.
//
// Regression check on the original code: run this file with
//   APP_JS_UNDER_TEST=$(git show main:internal/server/web/app.js > /tmp/appjs-main.js && echo /tmp/appjs-main.js)
// — the pre-ledger code (a single state._daemonVersion + a raw,
// non-proxy-aware About fetch) fails it, as expected.
//
// Plain node:test, no package.json, matching app-reload-guard.test.js and
// app-fed-conn-status.test.js: buildSandbox + loadScript + vm.runInContext.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const {
  makeStubElement, buildSandbox, loadScript, flushAsync, vm,
} = require('./testutil_browser_stub');

const APP_JS = process.env.APP_JS_UNDER_TEST || path.join(__dirname, 'app.js');

// Two daemons, two different builds — descriptive names, no release numbers.
const PEER = 'peer-gpu-box';
const LOCAL_VERSION = 'local-build-a';   // the daemon serving this page
const PEER_VERSION = 'peer-build-b';     // the federated peer's own daemon
const NEW_LOCAL_VERSION = 'local-build-c'; // a real update of the page's own daemon
const PEER_PROXY_PREFIX = '/api/proxy/' + encodeURIComponent(PEER) + '/';

// Serve the real English bundle for /locales/en.json so t() substitutes
// placeholders the way a real browser does ('Showing: %1$s' etc.).
const EN_BUNDLE = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));

// ── Fake fetch: local routes + a distinct peer dataset behind the proxy ──
function makeFetch() {
  const log = [];
  let peerHealthVersion = PEER_VERSION;

  const localRoutes = {
    '/api/health': () => ({ version: LOCAL_VERSION, hostname: 'local-host' }),
    '/api/config': () => ({ marker: 'local-config' }),
    '/api/sessions': () => [{ id: 'local-s1', name: 'local-session-a' }],
    '/api/servers': () => ({ servers: [{ name: PEER, url: 'https://peer.example:8443', enabled: true }] }),
  };
  // The peer's data is distinct from the local daemon's everywhere, so a
  // fetch that accidentally hits local renders the wrong rows and the
  // assertions below catch it.
  const peerRoutes = {
    '/api/health': () => ({ version: peerHealthVersion, hostname: 'peer-host' }),
    '/api/config': () => ({ marker: 'peer-config' }),
    '/api/sessions': () => [{ id: 'peer-s1', name: 'peer-session-b' }],
    '/api/plugins': () => ({ native: [], plugins: [{ name: 'peer-plugin', enabled: true, version: '9', description: 'exposed by the peer daemon' }] }),
    '/api/compute/nodes': () => ({ nodes: [{ name: 'peer-node' }] }),
    '/api/llms': () => ({ llms: [] }),
    '/api/interfaces': () => [],
    '/api/autonomous/scan/config': () => ({ enabled: true, sast_enabled: true, fail_on_severity: 'error', max_findings: 3 }),
    '/api/autonomous/config': () => ({ enabled: true, poll_interval_seconds: 30, max_concurrent_tasks: 2 }),
    '/api/mcp/docs': () => ({ tools: [{ name: 'peer-tool', description: 'exposed by the peer daemon' }] }),
    '/api/skills/registries': () => [],
  };

  function ok(body) {
    return Promise.resolve({
      ok: true, status: 200,
      json: () => Promise.resolve(body),
      text: () => Promise.resolve(JSON.stringify(body)),
    });
  }
  function notFound() {
    return Promise.resolve({
      ok: false, status: 404,
      json: () => Promise.resolve({}),
      text: () => Promise.resolve('404 (not a route in the test fixture)'),
    });
  }

  const fetch = function (url) {
    const u = String(url);
    log.push(u);
    if (u.indexOf('/locales/') === 0) return ok(EN_BUNDLE);
    let target = u;
    let routes = localRoutes;
    if (u.indexOf(PEER_PROXY_PREFIX) === 0) {
      target = u.slice(PEER_PROXY_PREFIX.length); // '/api/proxy/<peer>/api/…' → 'api/…'
      if (target[0] !== '/') target = '/' + target;
      routes = peerRoutes;
    }
    const handler = routes[target.split('?')[0]];
    return handler ? ok(handler()) : notFound();
  };
  fetch.__log = log;
  fetch.__setPeerHealthVersion = (v) => { peerHealthVersion = v; };
  return fetch;
}

// ── Sandbox harness (same shape as app-reload-guard.test.js) ───────────────
function run(env, code) { vm.runInContext(code, env.sandbox); }
function get(env, expr) { return vm.runInContext(expr, env.sandbox); }

async function loadEnv() {
  // Elements the assertions read; everything else resolves to the stub's
  // generic makeStubElement() via buildSandbox's getElementById fallback.
  const els = {
    aboutVersion: makeStubElement(),
    aboutVersionSkewNote: makeStubElement(),
    aboutShowingServer: { style: {}, textContent: '' },
    configViewerBody: { style: {}, textContent: '' },
    pluginsPanelBody: makeStubElement(),
    computeNodesPanel: makeStubElement(),
    automataSettingsScanPanel: makeStubElement(),
    automataSettingsAutonomousPanel: makeStubElement(),
    aboutMcpToolsSummary: makeStubElement(),
  };
  const fetchStub = makeFetch();
  const sandbox = buildSandbox({ byId: els });
  const env = { sandbox, els, reloads: [], log: fetchStub.__log, setPeerHealthVersion: fetchStub.__setPeerHealthVersion };

  sandbox.fetch = fetchStub;
  sandbox.location.reload = () => { env.reloads.push(1); };

  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);

  // The stub DOM never fires DOMContentLoaded (its addEventListener is a
  // no-op), so run the two boot steps app.js normally registers there:
  // record the page's own daemon version (plain non-proxied /api/health)
  // and load the i18n bundle.
  run(env, '_bootDaemonVersion(); initI18n().catch(() => {});');
  await flushAsync();
  assert.equal(get(env, 'window._i18n.ready'), true, 'i18n bundle should load');
  assert.equal(get(env, 'state._daemonVersion'), LOCAL_VERSION, 'boot should record the local daemon version');
  assert.equal(get(env, `state._daemonVersionByServer['local']`), LOCAL_VERSION, 'boot should record it under the local key');
  return env;
}

function setPeer(env, on) {
  run(env, `state.activeServer = ${on ? JSON.stringify(PEER) : 'null'};`);
}

// Count the config-cache side effect but let the REAL update path run:
// _onDaemonVersionChange with the user idle long enough auto-reloads via
// the stubbed location.reload, which the assertions count.
function spyLoadUiConfig(env) {
  run(env, `__cfgs = 0; _loadUiConfig = () => { __cfgs++; };`);
}

// ── 1. Selecting the peer records its version — no banner, no reload ──────
test('selecting the peer records its version under the peer key — no update banner, no reload', async () => {
  const env = await loadEnv();
  spyLoadUiConfig(env);
  run(env, 'window.__updates = 0; _onDaemonVersionChange = (v) => { window.__updates++; };');
  setPeer(env, true);
  run(env, `_daemonVersionNoteFrame('${PEER_VERSION}');`);

  assert.equal(get(env, `state._daemonVersionByServer['${PEER}']`), PEER_VERSION, 'peer version recorded under the peer key');
  assert.equal(get(env, `state._daemonVersionByServer['local']`), LOCAL_VERSION, 'local entry untouched');
  assert.equal(get(env, 'state._daemonVersion'), LOCAL_VERSION, "the page's own daemon version must not be replaced by the peer's");
  assert.equal(get(env, 'window.__updates'), 0, 'peer version must never take the update path');
  assert.equal(get(env, '__cfgs'), 0);
  assert.ok(!get(env, 'state._updateBannerShown'), 'no update banner');
  assert.ok(!get(env, 'state._updateAvailable'));
  assert.deepEqual(env.reloads, [], 'no location.reload');

  // The peer's own session list arrives over the local proxy — distinct
  // session names prove the peer data (not local's) was fetched.
  run(env, `_checkFederatedConnection('${PEER}');`);
  await flushAsync();
  assert.ok(env.log.some(u => u.indexOf('/api/proxy/' + PEER + '/api/sessions') === 0),
    'peer session probe must go through the local proxy, got: ' + JSON.stringify(env.log));
  assert.deepEqual(JSON.parse(get(env, 'JSON.stringify(state.sessions)')),
    [{ id: 'peer-s1', name: 'peer-session-b' }]);
});

// ── 2. Switching back to local: no prompt, no reload ──────────────────────
test('switching back to local re-reports the local version without a prompt or reload', async () => {
  const env = await loadEnv();
  spyLoadUiConfig(env);
  run(env, 'window.__updates = 0; _onDaemonVersionChange = (v) => { window.__updates++; };');
  setPeer(env, true);
  run(env, `_daemonVersionNoteFrame('${PEER_VERSION}');`);
  assert.equal(get(env, 'window.__updates'), 0);
  setPeer(env, false); // picker back on the local server
  run(env, `_daemonVersionNoteFrame('${LOCAL_VERSION}');`);

  assert.equal(get(env, 'window.__updates'), 0, 'local version already recorded for local — switching back must not read as an update');
  assert.ok(!get(env, 'state._updateBannerShown'));
  assert.deepEqual(env.reloads, []);
});

// ── 3. A real local update takes the update path exactly once ─────────────
test('a real update of the local daemon takes the update path exactly once', async () => {
  const env = await loadEnv();
  spyLoadUiConfig(env);
  run(env, 'state._lastUserActivityAt = 0;'); // long-idle: auto-reload branch
  run(env, `_daemonVersionNoteFrame('${NEW_LOCAL_VERSION}');`);

  assert.equal(env.reloads.length, 1, 'idle real change → auto location.reload()');
  assert.equal(get(env, '__cfgs'), 1, 'config cache reloaded once');
  assert.ok(!get(env, 'state._updateBannerShown'), 'auto-reload branch — banner never shown');
  assert.equal(get(env, `state._daemonVersionByServer['local']`), NEW_LOCAL_VERSION);
});

// ── 4. The same new version re-reported does not fire twice ───────────────
test('re-reporting the same new version (e.g. first frame after reconnect) does not fire again', async () => {
  const env = await loadEnv();
  spyLoadUiConfig(env);
  run(env, 'state._lastUserActivityAt = 0;');
  run(env, `_daemonVersionNoteFrame('${NEW_LOCAL_VERSION}');`);
  assert.equal(env.reloads.length, 1, 'first real change: update path exactly once');

  run(env, `_daemonVersionNoteFrame('${NEW_LOCAL_VERSION}');`); // e.g. reconnect
  run(env, `_daemonVersionNoteFrame('${NEW_LOCAL_VERSION}');`);
  assert.equal(env.reloads.length, 1, 'once per version, ever');
  assert.ok(!get(env, 'state._updateBannerShown'));
});

// ── 5. Peer selected: About version / config viewer / MCP hit the proxy ───
test('with a peer selected, About version, config viewer and MCP summary request the peer and render peer data', async () => {
  const env = await loadEnv();
  setPeer(env, true);

  const at = env.log.length;
  run(env, 'loadVersionInfo();');
  await flushAsync();
  const health = env.log.slice(at).filter(u => u.indexOf('/api/health') !== -1);
  assert.deepEqual(health, [PEER_PROXY_PREFIX + 'api/health'], 'About version must read the peer daemon through the proxy');
  assert.ok(env.els.aboutVersion.innerHTML.indexOf(PEER_VERSION) !== -1, 'About must render the PEER version');
  assert.equal(env.els.aboutShowingServer.textContent, 'Showing: ' + PEER, 'About must name the peer server');

  const at2 = env.log.length;
  run(env, 'loadConfigViewer();');
  await flushAsync();
  const cfg = env.log.slice(at2).filter(u => u.indexOf('/api/config') !== -1);
  assert.deepEqual(cfg, [PEER_PROXY_PREFIX + 'api/config'], 'config viewer must request the peer config');
  assert.deepEqual(JSON.parse(get(env, 'JSON.stringify(window._configViewerData)')), { marker: 'peer-config' });

  const at3 = env.log.length;
  run(env, 'loadAboutMcpToolsSummary();');
  await flushAsync();
  const mcp = env.log.slice(at3).filter(u => u.indexOf('/api/mcp/docs') !== -1);
  assert.deepEqual(mcp, [PEER_PROXY_PREFIX + 'api/mcp/docs'], 'MCP tools summary must request the peer docs');
  assert.ok(env.els.aboutMcpToolsSummary.innerHTML.indexOf('peer-tool') !== -1, 'MCP summary must render the peer tool list');
});

// ── 6. Skew note: visible iff the peer reports a different build ──────────
test('the version-skew note appears only while the peer reports a different build', async () => {
  const env = await loadEnv();
  const note = env.els.aboutVersionSkewNote;

  setPeer(env, true);
  run(env, 'loadVersionInfo();');
  await flushAsync();
  assert.equal(note.style.display, '', 'note visible while the peer build differs');
  assert.ok(note.innerHTML.length > 0, 'note carries the explanation');

  env.setPeerHealthVersion(LOCAL_VERSION); // peer daemon now reports the same build
  run(env, 'loadVersionInfo();');
  await flushAsync();
  assert.equal(note.style.display, 'none', 'note hidden when the builds match');
  assert.equal(note.innerHTML, '');

  // Not looking at a peer at all → no note either (local vs local).
  setPeer(env, false);
  run(env, 'loadVersionInfo();');
  await flushAsync();
  assert.equal(note.style.display, 'none');
});

// ── 7. Peer selected: every Settings tab requests the peer's data ─────────
test('with a peer selected, all Settings tabs request the peer’s data', async () => {
  const env = await loadEnv();
  setPeer(env, true);
  const at = env.log.length;

  const tabs = [
    'loadGeneralConfig',      // general
    'loadPluginsPanel',       // plugins
    'loadCommsConfig',        // comms
    'loadComputeNodesPanel',  // compute
    'loadAutomataSettingsPanel', // automata
    'loadVersionInfo',        // about
  ];
  for (const fn of tabs) {
    run(env, fn + '();');
    await flushAsync();
  }

  const api = env.log.slice(at).filter(u => u.indexOf('/api/') === 0);
  assert.ok(api.length >= tabs.length, 'each tab should have fetched at least once, got: ' + JSON.stringify(api));
  const bare = api.filter(u => u.indexOf(PEER_PROXY_PREFIX) !== 0);
  assert.deepEqual(bare, [], 'no Settings-tab request may bypass the proxy while a peer is selected: ' + JSON.stringify(bare));

  // Distinct peer data reached the panels (not the local fixture's).
  assert.ok(env.els.pluginsPanelBody.innerHTML.indexOf('peer-plugin') !== -1, 'plugins panel shows the peer plugin');
  assert.ok(env.els.computeNodesPanel.innerHTML.indexOf('peer-node') !== -1, 'compute panel shows the peer node');
  assert.ok(env.els.automataSettingsScanPanel.innerHTML.length > 0, 'automata scan panel rendered');
  assert.ok(env.els.automataSettingsAutonomousPanel.innerHTML.length > 0, 'automata autonomous panel rendered');
});
