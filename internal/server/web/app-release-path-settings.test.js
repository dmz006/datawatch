// Pins the Settings > Sessions release block (autonomous.push_and_tag,
// autonomous.release_path, git.release_ci_window_minutes). The block reads
// every value from the config object, always draws its switch (an off
// switch still shows), and draws the last-release status line only when a
// release record is present.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LOCALE_LIST = ['en', 'de', 'es', 'fr', 'ja'];
const LOCALE_DIR = path.join(__dirname, 'locales');

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, 'en.json'), 'utf8'));
  return sandbox;
}

function renderSection(cfg) {
  const sandbox = loadApp();
  sandbox.__cfg = cfg;
  return vm.runInContext('releaseSettingsSectionHTML(__cfg)', sandbox);
}

// Pull the checkbox tag for the push-and-tag switch out of the rendered block.
function switchTag(html) {
  const m = html.match(/<input type="checkbox" data-cfg-key="autonomous\.push_and_tag"[^>]*>/);
  return m ? m[0] : null;
}

test('the push-and-tag switch reads autonomous.push_and_tag from the config object', () => {
  const on = switchTag(renderSection({ autonomous: { push_and_tag: true } }));
  assert.ok(on, 'switch control missing');
  assert.match(on, /\schecked\b/);
  const off = switchTag(renderSection({ autonomous: { push_and_tag: false } }));
  assert.ok(off, 'an off switch must still render its control');
  assert.doesNotMatch(off, /\schecked\b/);
});

test('an unset push_and_tag renders the switch off, not missing', () => {
  const tag = switchTag(renderSection({}));
  assert.ok(tag, 'switch control missing when config has no release keys');
  assert.doesNotMatch(tag, /\schecked\b/);
});

test('the release path select reads autonomous.release_path and offers both values', () => {
  const html = renderSection({ autonomous: { release_path: 'pull_request' } });
  assert.match(html, /<select[^>]*data-cfg-key="autonomous\.release_path"/);
  assert.match(html, /<option value="pull_request" selected>/);
  assert.match(html, /<option value="direct_push" >/);
});

test('an unknown release path falls back to direct_push', () => {
  const html = renderSection({ autonomous: { release_path: 'force_push' } });
  assert.match(html, /<option value="direct_push" selected>/);
});

test('the CI window reads git.release_ci_window_minutes and defaults to 60', () => {
  const set = renderSection({ git: { release_ci_window_minutes: 90 } });
  assert.match(set, /data-cfg-key="git\.release_ci_window_minutes"[^>]*value="90"/);
  const unset = renderSection({});
  assert.match(unset, /data-cfg-key="git\.release_ci_window_minutes"[^>]*value="60"/);
});

test('every control carries a help entry that links to the release howto', () => {
  const html = renderSection({});
  const links = html.match(/href="\/diagrams\.html#docs\/howto\/release-push-and-tag\.md"/g) || [];
  assert.equal(links.length, 3, 'expected one ? help link per control');
});

test('the last-release status line is not rendered without a release record', () => {
  const html = renderSection({ autonomous: { push_and_tag: true } });
  assert.doesNotMatch(html, /Last release/);
  const noVersion = renderSection({ autonomous: { last_release: {} } });
  assert.doesNotMatch(noVersion, /Last release/);
});

test('the last-release status line renders when a release record is present', () => {
  const html = renderSection({ autonomous: { last_release: { version: 'v1.2.3' } } });
  assert.match(html, /Last release: v1\.2\.3/);
});

test('release locale keys exist with the same key set in all five bundles', () => {
  const keys = [
    'settings_release_section',
    'settings_release_push_and_tag',
    'settings_release_push_and_tag_help',
    'settings_release_path',
    'settings_release_path_help',
    'settings_release_ci_window',
    'settings_release_ci_window_help',
    'settings_release_help_link_label',
    'settings_release_last_status',
  ];
  const sets = {};
  for (const locale of LOCALE_LIST) {
    const bundle = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, locale + '.json'), 'utf8'));
    for (const k of keys) {
      assert.ok(typeof bundle[k] === 'string' && bundle[k].length > 0,
        k + ' missing or empty in ' + locale + ' locale');
      assert.doesNotMatch(bundle[k], /PRD/, k + ' in ' + locale + ' must say Automaton, not PRD');
    }
    sets[locale] = Object.keys(bundle).filter(k => k.startsWith('settings_release_')).sort().join(',');
  }
  for (const locale of LOCALE_LIST) {
    assert.equal(sets[locale], sets.en, locale + ' settings_release_* key set differs from en');
  }
});

test('the release field is registered in the Sessions card next to the git settings', () => {
  const sandbox = loadApp();
  const keys = JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'sess') || {fields: []}).fields.map(f => f.key))",
    sandbox));
  const gitIdx = keys.indexOf('git.secret_scan.allow_list.fingerprints');
  const relIdx = keys.indexOf('release.section');
  assert.ok(gitIdx >= 0 && relIdx > gitIdx, 'release block must follow the git settings in the Sessions card');
});
