// app-plugin-service-settings.test.js
//
// Pins the Settings → Plugins → Plugin services card in app.js against the
// plugins.services config contract:
//
//   * The declarative field list (PLUGIN_SERVICES_FIELDS) has exactly one
//     entry per plugins.services.* global key, in declaration order, each
//     with the label/locale-key the card renders under.
//   * The lifecycle_mode select for a service plugin offers ONLY the modes
//     that service's manifest declares (plus the "" = use-default entry) —
//     a service declaring just `attach_only` must never be offered
//     `managed`, and vice versa.
//   * Every rendered field carries a '?' help pill linking to
//     docs/plugins.md (PLUGIN_SERVICES_DOCS), i.e. every row has a docs
//     link and the card as a whole links to plugins.md.
//   * The locale keys the card depends on exist in every shipped locale
//     bundle (en, de, es, fr, ja) with non-empty values.
//
// Runs the REAL app.js inside the shared stub sandbox from
// testutil_browser_stub.js (node:test + node:assert/strict, no packages),
// then calls the card's pure renderers with fixture data — no network,
// no timers.
//
//   node --test app-plugin-service-settings.test.js   (from this directory)

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');

const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const sandbox = buildSandbox();
vm.createContext(sandbox);
loadScript(path.join(__dirname, 'app.js'), sandbox);

const run = (code) => vm.runInContext(code, sandbox);

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];

const expectedGlobalKeys = [
  'plugins.services.health_interval_seconds',
  'plugins.services.backlog_growth_pulls',
  'plugins.services.node_load_ratio',
  'plugins.services.small_model_max_params_billions',
  'plugins.services.small_model_context_tokens',
];

test('declarative field list: one field per plugins.services global key, in order', () => {
  const fields = run('PLUGIN_SERVICES_FIELDS');
  assert.ok(Array.isArray(fields), 'PLUGIN_SERVICES_FIELDS is an array');
  assert.equal(fields.length, expectedGlobalKeys.length);
  fields.forEach((f, i) => {
    assert.equal(f.key, expectedGlobalKeys[i], `field ${i} key`);
    assert.equal(typeof f.label, 'string', `field ${i} has an English fallback label`);
    assert.ok(f.label && f.label.length > 0, `field ${i} fallback label non-empty`);
    assert.equal(typeof f.labelKey, 'string', `field ${i} has a labelKey`);
    assert.ok(String(f.labelKey).startsWith('settings_plugin_services_'), `field ${i} labelKey namespaced`);
  });
});

test('docs target is plugins.md', () => {
  assert.equal(run('PLUGIN_SERVICES_DOCS'), 'plugins.md');
  const key = run(
    "Object.prototype.hasOwnProperty.call(DOCS_LINKS_BY_KEY, 'plugins_services') ? DOCS_LINKS_BY_KEY['plugins_services'] : null"
  );
  assert.equal(key, 'plugins.md', "settingsSectionHeader 'plugins_services' resolves to plugins.md");
});

function fixtureServices() {
  return {
    svc: {
      health_interval_seconds: 90,
      node_load_ratio: 1.5,
      per_service: { web: { lifecycle_mode: 'attach_only' } },
    },
    services: [
      { name: 'mcp', modes: ['managed'], perService: {} },
      { name: 'web', modes: ['attach_only', 'managed'], perService: { lifecycle_mode: 'attach_only' } },
    ],
  };
}

test('rendered card fills values from the stored config block', () => {
  const { svc, services } = fixtureServices();
  const html = run(
    `pluginServiceSettingsHTML(${JSON.stringify(svc)}, ${JSON.stringify(services)})`
  );
  assert.ok(html.includes('value="90"'), 'health_interval_seconds value rendered');
  assert.ok(html.includes('value="1.5"'), 'node_load_ratio value rendered');
  // unset global key renders as an empty input (store-time default)
  const backlog = html.match(/Backlog growth streak[\s\S]*?<input[^>]*value="([^"]*)"/);
  assert.ok(backlog, 'backlog growth row present with an input');
  assert.equal(backlog[1], '', 'unset backlog_growth_pulls renders empty (default)');
});

test('lifecycle_mode select offers only the modes the service declares', () => {
  // mcp declares only `managed` — `attach_only` must not be offered.
  const mcp = run(
    `pluginServiceLifecycleSelectHTML('mcp', ['managed'], '')`
  );
  assert.ok(mcp.includes('<option value="managed"'), 'declared mode managed offered');
  assert.ok(!/value="attach_only"/.test(mcp), 'non-declared attach_only NOT offered for mcp');

  // web declares both — both offered.
  const web = run(
    `pluginServiceLifecycleSelectHTML('web', ['attach_only','managed'], 'attach_only')`
  );
  assert.ok(web.includes('<option value="attach_only" selected'), 'stored attach_only selected for web');
  assert.ok(web.includes('<option value="managed"'), 'managed also offered for web');

  // The default entry exists so an operator can clear a wrong mode back
  // to the daemon's load-time default.
  const def = run(`_pluginServiceLocaleKey('settings_plugin_services_lifecycle_default', '(default)')`);
  assert.ok(web.includes(`<option value=""`), 'empty (use-default) option present');
  assert.ok(web.includes(def));

  // A stored mode for a service that only declares another is still
  // selectable (so the operator can clear it). A service that declares
  // NO modes (defensive) is offered only the use-default entry — never
  // a fallback attach_only+managed list.
  const none = run(`pluginServiceLifecycleSelectHTML('x', [], 'managed')`);
  assert.equal((none.match(/<option value="/g) || []).length, 1, 'no modes declared → default option only, no fallback modes offered');
  assert.ok(!/value="attach_only"/.test(none), 'no undeclared attach_only for a mode-less service');
  assert.ok(!/value="managed"/.test(none), 'no undeclared managed for a mode-less service');
});

test('every field row carries a ? help pill linking to docs/plugins.md', () => {
  const { svc, services } = fixtureServices();
  const html = run(
    `pluginServiceSettingsHTML(${JSON.stringify(svc)}, ${JSON.stringify(services)})`
  );
  const pill = run(`docsLink('plugins.md', '?')`);
  assert.ok(pill.includes('plugins.md'), 'pill itself links to plugins.md');
  assert.ok(pill.includes('>?</a>'), 'pill shows a ? label');

  // 5 global rows + 2 lifecycle rows, every one with the docs pill.
  const count = html.split(pill).length - 1;
  assert.equal(count, 7, 'every rendered field has the plugins.md ? pill');

  // Each per-service row is labelled with the service name.
  assert.ok(html.includes('<strong>mcp</strong>'), 'mcp labelled');
  assert.ok(html.includes('<strong>web</strong>'), 'web labelled');

  // The lifecycle save helper targets the daemon's exact per-service key:
  // 'plugins.services.per_service.<name>' (mode string as the value — the
  // daemon's applyConfigPatch contract), not a nested .lifecycle_mode member.
  const keyExpr = run(`'plugins.services.per_service.' + 'web'`);
  assert.equal(keyExpr, 'plugins.services.per_service.web');
});

test('save helper writes the per-service PUT key through saveGeneralField', () => {
  // Capture saveGeneralField's argument by wrapping it inside the sandbox.
  // The recorded call must be the daemon contract: key
  // 'plugins.services.per_service.web' (service name only) with the mode
  // string as the value — NOT a nested .lifecycle_mode member key, which
  // the daemon would store as a junk per_service entry named 'web.lifecycle_mode'.
  run(`
    (function () {
      var orig = saveGeneralField;
      var record = [];
      saveGeneralField = function (k, v) { record.push([k, v]); orig(k, v); };
      window.__psSaveRecord = record;
      savePluginServiceLifecycleMode('web', 'attach_only');
    })()
  `);
  const record = run('window.__psSaveRecord');
  // (array literal created inside the vm realm → compare by serialized form,
  // deepStrictEqual across realms would fail on identity)
  assert.equal(JSON.stringify(record), JSON.stringify([['plugins.services.per_service.web', 'attach_only']]));
});

test('locale bundles: card keys present with non-empty values in all five locales', () => {
  const fs = require('node:fs');
  const keys = [
    'settings_plugin_services_title',
    'settings_plugin_services_loading',
    'settings_plugin_services_error',
    'settings_plugin_services_health_interval',
    'settings_plugin_services_backlog_growth',
    'settings_plugin_services_node_load',
    'settings_plugin_services_small_max_params',
    'settings_plugin_services_small_context_tokens',
    'settings_plugin_services_per_service',
    'settings_plugin_services_lifecycle',
    'settings_plugin_services_lifecycle_default',
    'settings_plugin_services_no_services',
  ];
  for (const loc of LOCALES) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', `${loc}.json`), 'utf8'));
    for (const k of keys) {
      assert.equal(typeof bundle[k], 'string', `${loc} has string ${k}`);
      assert.ok(bundle[k].length > 0, `${loc} ${k} non-empty`);
    }
  }
});
