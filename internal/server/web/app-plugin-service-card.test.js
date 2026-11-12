const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const cardKeyRe = /window\.pluginServiceCardHtml\s*=\s*function/;

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

function render(name, svc) {
  const sandbox = loadApp();
  sandbox._name = name;
  sandbox._svc = svc;
  return vm.runInContext('window.pluginServiceCardHtml(_name, _svc)', sandbox);
}

test('pluginServiceCardHtml is exported', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  assert.match(src, cardKeyRe);
  const sandbox = loadApp();
  assert.equal(typeof sandbox.window.pluginServiceCardHtml, 'function');
});

test('valid managed payload renders name, state, mode, address, version, uptime and last check — no action for an up service', () => {
  const html = render('enrich', {
    mode: 'managed',
    state: 'healthy',
    address: 'http://127.0.0.1:9443',
    version: '1.4.2',
    uptime_seconds: 3725,
    last_health_check: '2026-11-01T14:00:00Z',
    last_health_status: 'up',
  });
  assert.match(html, /class="plugin-service-card"/);
  assert.match(html, /enrich/);
  assert.match(html, /\bup\b/);
  assert.match(html, /managed/);
  assert.match(html, /http:\/\/127\.0\.0\.1:9443/);
  assert.match(html, /Address/);
  assert.match(html, /Version/);
  assert.match(html, /1\.4\.2/);
  assert.match(html, /Uptime/);
  assert.match(html, /1h 2m/);
  assert.match(html, /Last health check/);
  assert.match(html, /2026-11-01 14:00:00 UTC/);
  // A service that is up has no next action to show: no start link, no
  // check-running hint.
  assert.doesNotMatch(html, /Start it from here/);
  assert.doesNotMatch(html, /Check that the service is running/);
  assert.doesNotMatch(html, /Last error/);
  assert.doesNotMatch(html, /\bdown\b/);
});

test('attach-only payload without a version omits the Version and start-action rows but keeps uptime and last check', () => {
  const html = render('enrich-attach', {
    mode: 'attach_only',
    address: 'https://enrich.example:8443',
    last_health_check: '2026-11-01T15:30:00-04:00',
    last_health_status: 'up',
    uptime_seconds: 600,
  });
  assert.match(html, /enrich-attach/);
  assert.match(html, /\bup\b/);
  assert.match(html, /attach-only/);
  assert.match(html, /https:\/\/enrich\.example:8443/);
  assert.match(html, /Last health check/);
  assert.match(html, /2026-11-01 15:30:00 -04:00/);
  assert.match(html, /Uptime/);
  assert.match(html, /10m 0s/);
  assert.doesNotMatch(html, /Version/);
  assert.doesNotMatch(html, /managed/);
  assert.doesNotMatch(html, /Start it from here/);
  assert.doesNotMatch(html, /Check that the service is running/);
});

test('attach service down renders the down state, the last error and the check-running hint', () => {
  const html = render('enrich-down', {
    mode: 'attach_only',
    last_health_check: '2026-11-01T16:00:00Z',
    last_health_status: 'down',
    last_health_error: 'connection refused',
  });
  assert.match(html, /\bdown\b/);
  assert.match(html, /Last error/);
  assert.match(html, /connection refused/);
  assert.match(html, /Check that the service is running/);
  assert.doesNotMatch(html, /Start it from here/);
  assert.doesNotMatch(html, /Address/);
  assert.doesNotMatch(html, /Version/);
  assert.doesNotMatch(html, /Uptime/);
});

test('managed service down maps the supervisor state and offers the start-from-here action', () => {
  const html = render('svc-crashed', {
    mode: 'managed',
    state: 'crashed',
    last_error: 'process exited: exit status 2',
  });
  assert.match(html, /\bdown\b/);
  assert.match(html, /managed/);
  assert.match(html, /Start it from here/);
  assert.match(html, /pluginServiceStart\('svc-crashed'/);
  assert.match(html, /Start it from here/);
  assert.match(html, /process exited: exit status 2/);
  assert.doesNotMatch(html, /Check that the service is running/);
  assert.doesNotMatch(html, /http/);
});

test('managed starting maps to unknown, not up or down', () => {
  const html = render('svc-starting', { mode: 'managed', state: 'starting' });
  assert.match(html, /\bunknown\b/);
  assert.doesNotMatch(html, />\s*up\s*</);
  assert.doesNotMatch(html, />\s*down\s*</);
});

test('a service name or label containing a <script> tag is rendered escaped', () => {
  const html = render('<img src=x onerror=alert(1)>', {
    mode: 'managed',
    state: 'crashed',
    address: 'http://<i>evil</i>:1',
    version: '<b>1.0</b>',
    last_error: '<script>alert(1)</script>',
  });
  assert.doesNotMatch(html, /<script>alert/);
  assert.doesNotMatch(html, /<img src=x/);
  assert.doesNotMatch(html, /<b>1\.0<\/b>/);
  assert.doesNotMatch(html, /<i>evil<\/i>/);
  assert.match(html, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
  assert.match(html, /&lt;img src=x onerror=alert\(1\)&gt;/);
  assert.match(html, /&lt;b&gt;1\.0&lt;\/b&gt;/);
  assert.match(html, /&lt;i&gt;evil&lt;\/i&gt;/);
});

test('empty or missing rows are omitted; a service-less plugin renders nothing', () => {
  assert.equal(render('no-svc', null), '');
  const html = render('bare-managed', { mode: 'managed', state: 'healthy' });
  assert.match(html, /\bup\b/);
  assert.doesNotMatch(html, /Address/);
  assert.doesNotMatch(html, /Version/);
  assert.doesNotMatch(html, /Uptime/);
  assert.doesNotMatch(html, /Last health check/);
  assert.doesNotMatch(html, /Last error/);
});

test('open service events render as a single link to the Events entry, not a status strip', () => {
  const html = render('enrich-ev', {
    mode: 'attach_only',
    last_health_status: 'down',
    last_health_error: 'refused',
    open_events: 3,
  });
  assert.match(html, /3 open service events — Events →/);
  assert.match(html, /onclick="navigate\('dashboard'\)"/);
  assert.doesNotMatch(html, /refused.{0,40}refused/s); // one last-error row, not repeated per event
  const links = html.split('navigate(\'dashboard\')').length - 1;
  assert.equal(links, 1);
});

test('the open-events count comes only from the backend open_events field, from 0 or absent', () => {
  // open_events = 1 wins even if an (unwired) events array is present:
  // the card counts the real backend counter, not an event list.
  const html = render('ev-source', {
    mode: 'attach_only',
    open_events: 1,
    events: [1, 2, 3],
  });
  assert.match(html, /1 open service events — Events →/);
  assert.doesNotMatch(html, /3 open service events/);

  // 0 and absent both mean the link is omitted entirely, and an
  // (unwired) events array alone renders no link.
  assert.doesNotMatch(render('ev-zero', { mode: 'attach_only', open_events: 0 }), /open service events/);
  assert.doesNotMatch(render('ev-absent', { mode: 'attach_only' }), /open service events/);
  assert.doesNotMatch(render('ev-array', { mode: 'attach_only', events: [{ id: 1 }] }), /open service events/);
});

test('every new card locale key is present in all five bundles, without tracker IDs', () => {
  const keys = [
    'plugin_service_card_title',
    'plugin_service_up',
    'plugin_service_down',
    'plugin_service_unknown',
    'plugin_service_mode_managed',
    'plugin_service_mode_attach_only',
    'plugin_service_address',
    'plugin_service_version',
    'plugin_service_uptime',
    'plugin_service_last_check',
    'plugin_service_last_error',
    'plugin_service_action_start',
    'plugin_service_action_check_running',
    'plugin_service_events_link',
  ];
  const trackerRe = /\b(BL|B|F|S)\d+\b/; // task/defect IDs may not leak into user-visible text
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', `${lang}.json`), 'utf8'));
    for (const k of keys) {
      assert.equal(typeof bundle[k], 'string', `${lang} missing locale key: ${k}`);
      assert.ok(bundle[k].length > 0, `${lang} empty card locale value: ${k}`);
      assert.doesNotMatch(bundle[k], trackerRe, `${k} in ${lang} locale contains a tracker ID`);
    }
  }
  // The four non-English bundles must not ship untranslated English for this
  // card. (Two words are the same in several languages — card title in fr,
  // "Version" in de/fr — and are exempt.)
  const exempt = new Set([
    'fr:plugin_service_card_title',
    'de:plugin_service_version',
    'fr:plugin_service_version',
  ]);
  for (const lang of ['de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', `${lang}.json`), 'utf8'));
    for (const k of keys) {
      assert.ok(
        bundle[k] !== en[k] || exempt.has(`${lang}:${k}`),
        `${lang} untranslated (identical to EN) card key: ${k}`,
      );
    }
  }
});

test('the locale-guard file pins the high-visibility card keys', () => {
  const src = fs.readFileSync(path.join(__dirname, '..', 'v5280_locales_test.go'), 'utf8');
  for (const k of [
    'plugin_service_card_title',
    'plugin_service_up',
    'plugin_service_down',
    'plugin_service_unknown',
    'plugin_service_action_start',
    'plugin_service_action_check_running',
  ]) {
    assert.match(src, new RegExp(`"${k}"`), `v5280_locales_test.go must pin ${k}`);
  }
});
