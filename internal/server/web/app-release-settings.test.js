// Pins the release settings on Settings > Automation > Merge pipeline: the
// four fields render with their help link, the release size map saves as one
// object through PUT /api/config, a refused cadence or scheme shows the
// daemon's reason and puts the select back, and every label, option and help
// key exists in all five locale bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const PREFIX = 'autonomous.merge_pipeline.';

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

function field(sandbox, key) {
  const json = vm.runInContext(
    "JSON.stringify(((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.find(f => f.key === '" + key + "') || null))",
    sandbox);
  return JSON.parse(json);
}

// recordToasts swaps showToast for a recorder so tests can read the messages.
function recordToasts(sandbox) {
  const toasts = [];
  sandbox.__toasts = toasts;
  vm.runInContext("showToast = function (m, kind) { __toasts.push({ m: String(m), kind: kind }); };", sandbox);
  return toasts;
}

// stubT makes t() echo its key and arguments, since no locale bundle is loaded
// in this sandbox. The toast text then shows the reason passed to t().
function stubT(sandbox) {
  vm.runInContext("t = function (k, a) { return a && a.length ? k + ': ' + a.join(' ') : k; };", sandbox);
}

// recordPuts swaps fetch for a recorder that answers with status and body.
function recordPuts(sandbox, status, body) {
  const calls = [];
  sandbox.fetch = (url, opts) => {
    calls.push({ url, method: opts && opts.method, body: opts && opts.body });
    return Promise.resolve({ ok: status >= 200 && status < 300, status, text: () => Promise.resolve(body) });
  };
  return calls;
}

const flush = () => new Promise(resolve => setImmediate(resolve));

const RELEASE_KEYS = [
  'settings_merge_pipeline_release_size_map',
  'settings_merge_pipeline_release_size_map_help',
  'settings_merge_pipeline_release_size_map_kind',
  'settings_merge_pipeline_release_size_map_size',
  'settings_merge_pipeline_release_size_map_add',
  'settings_merge_pipeline_release_size_map_remove',
  'settings_merge_pipeline_release_size_map_save',
  'settings_merge_pipeline_release_size_map_empty_kind',
  'settings_merge_pipeline_release_size_map_duplicate',
  'merge_pipeline_release_size_patch',
  'merge_pipeline_release_size_minor',
  'merge_pipeline_release_size_major',
  'merge_pipeline_release_size_none',
  'settings_merge_pipeline_major_trigger',
  'settings_merge_pipeline_major_trigger_help',
  'merge_pipeline_major_manual',
  'merge_pipeline_major_milestone',
  'merge_pipeline_major_never',
  'settings_merge_pipeline_release_cadence',
  'settings_merge_pipeline_release_cadence_help',
  'merge_pipeline_cadence_per_merge',
  'merge_pipeline_cadence_batch',
  'merge_pipeline_cadence_manual',
  'settings_merge_pipeline_version_scheme',
  'settings_merge_pipeline_version_scheme_help',
  'merge_pipeline_scheme_semver',
];

test('the four release fields render on the autonomous card with their help link', () => {
  const sandbox = loadApp();
  const size = field(sandbox, PREFIX + 'release_size_map');
  assert.ok(size, 'release_size_map missing');
  assert.equal(size.type, 'release_size_map');
  assert.equal(size.helpDocs, 'datawatch-definitions.md#release-size');

  const trigger = field(sandbox, PREFIX + 'major_trigger');
  assert.equal(trigger.type, 'select');
  assert.deepEqual(trigger.options, ['manual', 'milestone', 'never']);
  assert.equal(trigger.default, 'manual');
  assert.equal(trigger.helpDocs, 'datawatch-definitions.md#release-size');

  const cadence = field(sandbox, PREFIX + 'release_cadence');
  assert.equal(cadence.type, 'select');
  assert.deepEqual(cadence.options, ['per_merge', 'batch', 'manual']);
  assert.equal(cadence.default, 'per_merge');
  assert.equal(cadence.helpDocs, 'datawatch-definitions.md#release-size');

  const scheme = field(sandbox, PREFIX + 'version_scheme');
  assert.equal(scheme.type, 'select');
  assert.deepEqual(scheme.options, ['semver']);
  assert.equal(scheme.default, 'semver');
  assert.equal(scheme.helpDocs, 'datawatch-definitions.md#release-size');
});

test('the help link maps to the Release size entry of the definitions through the merge pipeline table', () => {
  const sandbox = loadApp();
  for (const key of [PREFIX + 'release_size_map', PREFIX + 'major_trigger', PREFIX + 'release_cadence', PREFIX + 'version_scheme']) {
    const link = vm.runInContext("docsLinkForKey(" + JSON.stringify(key) + ")", sandbox);
    assert.equal(link, 'datawatch-definitions.md#release-size', key);
  }
  // The existing merge pipeline keys keep their own glossary anchors.
  const other = vm.runInContext("docsLinkForKey('" + PREFIX + "conflict_policy')", sandbox);
  assert.notEqual(other, 'datawatch-definitions.md#release-size');
});

test('a select save PUTs the value to /api/config under its key', async () => {
  const sandbox = loadApp();
  const calls = recordPuts(sandbox, 200, '');
  recordToasts(sandbox);
  vm.runInContext("saveMergePipelineReleaseField('" + PREFIX + "major_trigger', { value: 'milestone', tagName: 'SELECT', options: [] })", sandbox);
  await flush();
  assert.equal(calls.length, 1);
  assert.equal(calls[0].method, 'PUT');
  assert.match(calls[0].url, /\/api\/config$/);
  assert.deepEqual(JSON.parse(calls[0].body), { [PREFIX + 'major_trigger']: 'milestone' });
});

test('a refused cadence shows the daemon reason and puts the select back to the saved value', async () => {
  const sandbox = loadApp();
  const reason = 'autonomous.merge_pipeline.release_cadence: release cadence batch is not available yet';
  const calls = recordPuts(sandbox, 400, reason + '\n');
  const toasts = recordToasts(sandbox);
  stubT(sandbox);
  const el = {
    tagName: 'SELECT',
    value: 'batch',
    options: [{ value: 'per_merge', defaultSelected: true }, { value: 'batch', defaultSelected: false }],
  };
  sandbox.__el = el;
  vm.runInContext("saveMergePipelineReleaseField('" + PREFIX + "release_cadence', __el)", sandbox);
  await flush();
  assert.equal(calls.length, 1);
  assert.deepEqual(JSON.parse(calls[0].body), { [PREFIX + 'release_cadence']: 'batch' });
  assert.equal(el.value, 'per_merge', 'the select must go back to the saved value');
  assert.equal(toasts.length, 1);
  assert.equal(toasts[0].kind, 'error');
  assert.ok(toasts[0].m.includes('release cadence batch is not available yet'), toasts[0].m);
});

test('a refused version scheme is shown with the daemon reason', async () => {
  const sandbox = loadApp();
  const reason = 'autonomous.merge_pipeline.version_scheme: version scheme calver is not available yet';
  recordPuts(sandbox, 400, reason);
  const toasts = recordToasts(sandbox);
  stubT(sandbox);
  vm.runInContext("saveMergePipelineReleaseField('" + PREFIX + "version_scheme', { value: 'calver', tagName: 'SELECT', options: [] })", sandbox);
  await flush();
  assert.ok(toasts.some(t => t.kind === 'error' && t.m.includes('version scheme calver is not available yet')), JSON.stringify(toasts));
});

// fakeRow builds the element shape saveReleaseSizeMap reads from one row.
function fakeRow(kind, size) {
  return {
    querySelector(sel) {
      if (sel === '[data-release-kind]') return { value: kind };
      if (sel === '[data-release-size]') return { value: size };
      return null;
    },
  };
}

function withRows(sandbox, rows) {
  sandbox.__rows = { querySelectorAll: () => rows };
  vm.runInContext("document.getElementById = function (id) { return id === 'release_size_map_rows' ? __rows : null; };", sandbox);
}

test('the release size map saves every kind row as one object through PUT /api/config', async () => {
  const sandbox = loadApp();
  const calls = recordPuts(sandbox, 200, '');
  recordToasts(sandbox);
  withRows(sandbox, [fakeRow(' feature ', 'major'), fakeRow('chore', 'none'), fakeRow('bug', 'patch')]);
  vm.runInContext("saveReleaseSizeMap()", sandbox);
  await flush();
  assert.equal(calls.length, 1);
  assert.equal(calls[0].method, 'PUT');
  assert.deepEqual(JSON.parse(calls[0].body), {
    [PREFIX + 'release_size_map']: { feature: 'major', chore: 'none', bug: 'patch' },
  });
});

test('a blank or repeated kind is refused in the browser and nothing is sent', async () => {
  const sandbox = loadApp();
  const calls = recordPuts(sandbox, 200, '');
  const toasts = recordToasts(sandbox);
  withRows(sandbox, [fakeRow('feature', 'minor'), fakeRow('feature', 'major')]);
  vm.runInContext("saveReleaseSizeMap()", sandbox);
  await flush();
  assert.equal(calls.length, 0);
  assert.equal(toasts.length, 1);
  assert.equal(toasts[0].kind, 'error');

  withRows(sandbox, [fakeRow('  ', 'minor')]);
  vm.runInContext("saveReleaseSizeMap()", sandbox);
  await flush();
  assert.equal(calls.length, 0);
  assert.equal(toasts.length, 2);
});

test('the release size row offers patch, minor, major and none and names the kind', () => {
  const sandbox = loadApp();
  const html = vm.runInContext("_releaseSizeMapRowHTML('feature', 'minor')", sandbox);
  assert.ok(html.includes('data-release-kind'));
  assert.ok(html.includes('value="feature"'));
  for (const size of ['patch', 'minor', 'major', 'none']) {
    assert.ok(html.includes('<option value="' + size + '"'), 'missing size ' + size);
  }
  assert.ok(/<option value="minor" selected>/.test(html), 'the given size is selected');
});

test('every release label, option and help key exists in all five locale bundles', () => {
  for (const locale of LOCALES) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
    for (const k of RELEASE_KEYS) {
      assert.ok(typeof bundle[k] === 'string' && bundle[k].length > 0, k + ' missing or empty in ' + locale);
    }
    // Placeholders in the duplicate-kind message must match English.
    const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
    const ph = s => (s.match(/%\d\$[sd]/g) || []).sort().join(',');
    assert.equal(ph(bundle.settings_merge_pipeline_release_size_map_duplicate), ph(en.settings_merge_pipeline_release_size_map_duplicate), locale);
  }
});
