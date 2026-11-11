// Settings -> Automata -> Merge pipeline rows.
//
// Every editable autonomous.merge_pipeline.* key has a Settings row with a
// label, a docs link and locale keys in all five bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');

const appSrc = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
const P = 'autonomous.merge_pipeline.';
const KEYS = [
  'file_name', 'source_order', 'fallback_on_bad_file', 'conflict_policy', 'conflict_model',
  'conflict_mode', 'conflict_confirm_first_n', 'version_files', 'release_size_map',
  'major_trigger', 'release_cadence', 'version_scheme',
  'release_approval.patch', 'release_approval.minor', 'release_approval.major',
  'post_ship.enabled', 'post_ship.hooks', 'post_ship.registry_file', 'post_ship.registry_row_pattern',
  'post_ship.companion_repository', 'post_ship.issue_title_template', 'post_ship.notify_template',
].map((k) => P + k);
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const bundles = Object.fromEntries(LANGS.map((l) =>
  [l, JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'))]));

function rowFor(key) {
  const line = appSrc.split('\n').find((l) => l.includes(`{ key: '${key}'`));
  assert.ok(line, `no Settings row for ${key}`);
  return line;
}

test('every merge pipeline key has a Settings row with a label and a docs link', () => {
  for (const key of KEYS) {
    const row = rowFor(key);
    assert.match(row, /labelKey: '[a-z0-9_]+'/, `${key} lacks a labelKey`);
    assert.match(row, /label: /, `${key} lacks a label`);
    assert.match(row, /type: '[a-z_]+'/, `${key} lacks a type`);
    assert.match(row, /docsKey: '[^']+'|helpDocs: '[^']+'/, `${key} lacks a docs link`);
  }
});

test('every merge pipeline row sits under the Automata group', () => {
  const groupAt = appSrc.indexOf("labelKey: 'settings_merge_pipeline_group'");
  assert.ok(groupAt > 0, 'merge pipeline group missing');
  for (const key of KEYS) {
    assert.ok(appSrc.indexOf(`{ key: '${key}'`) > groupAt, `${key} is above the merge pipeline group`);
  }
});

test('every merge pipeline row has its locale keys in all five bundles', () => {
  const refs = new Set(['settings_merge_pipeline_group']);
  for (const key of KEYS) {
    const row = rowFor(key);
    for (const m of row.matchAll(/(?:labelKey|hintKey|helpKey): '([^']+)'/g)) refs.add(m[1]);
  }
  for (const lang of LANGS) {
    for (const k of refs) {
      assert.ok(typeof bundles[lang][k] === 'string' && bundles[lang][k].length > 0, `${lang} missing ${k}`);
    }
  }
});
