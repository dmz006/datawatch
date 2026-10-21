// New-session Git toggle: the New-Session form must render a "Git"
// checkbox (default unchecked) labelled via the new_session_git_label
// i18n key, and submitNewSession must send `git: true` only when it is
// checked (omit the field otherwise, matching the chrome:true pattern).
// New_session_git_label must be present (non-empty string) in all five
// locale bundles (de, en, es, fr, ja).

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');

const LOCALES_DIR = path.join(__dirname, 'locales');
const LOCALES = ['de', 'en', 'es', 'fr', 'ja'];

test('new_session_git_label is a non-empty string in all five locale bundles', () => {
  for (const locale of LOCALES) {
    const raw = fs.readFileSync(path.join(LOCALES_DIR, locale + '.json'), 'utf8');
    const bundle = JSON.parse(raw); // throws if the bundle is not valid JSON
    assert.equal(typeof bundle.new_session_git_label, 'string',
      `new_session_git_label missing or non-string in ${locale}.json`);
    assert.ok(bundle.new_session_git_label.length > 0,
      `new_session_git_label empty in ${locale}.json`);
  }
});

test('new_session_git_label sits at its alphabetical position in every bundle', () => {
  for (const locale of LOCALES) {
    const bundle = JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, locale + '.json'), 'utf8'));
    const keys = Object.keys(bundle);
    const i = keys.indexOf('new_session_git_label');
    assert.ok(i >= 0, `${locale}.json: key not found`);
    // keys are stored alphabetically (insertion-sorted JSON); verify neighbours.
    if (i > 0) {
      assert.ok(keys[i - 1] < 'new_session_git_label',
        `${locale}.json: not in alphabetical order (previous key ${keys[i - 1]} >= new_session_git_label)`);
    }
    if (i < keys.length - 1) {
      assert.ok(keys[i + 1] > 'new_session_git_label',
        `${locale}.json: not in alphabetical order (next key ${keys[i + 1]} <= new_session_git_label)`);
    }
  }
});

test('app.js: new-session Git checkbox exists, is default-unchecked, and uses the label key', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  const idx = src.indexOf('id="newSessionGitEnable"');
  assert.ok(idx >= 0, 'the newSessionGitEnable checkbox is missing from app.js');
  // The checkbox input must not carry a `checked` attribute (default unchecked).
  const inputTag = src.slice(src.lastIndexOf('<input', idx), idx + ' id="newSessionGitEnable"'.length);
  assert.match(inputTag, /type="checkbox"/, 'newSessionGitEnable must be a checkbox input');
  assert.doesNotMatch(inputTag, /checked/i, 'newSessionGitEnable must default to unchecked');
  // The row is the labelled form, using the bundled i18n key.
  const labelBlock = src.slice(Math.max(0, idx - 400), idx + 200);
  assert.match(labelBlock, /new_session_git_label/, 'the Git checkbox label must use the new_session_git_label i18n key');
  assert.match(labelBlock, /gitInitToggle/, 'the Git checkbox must live next to the auto git init toggle');
});

test('app.js: submitNewSession sends git:true only when the checkbox is checked', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  assert.match(src, /payload\.git\s*=\s*true/,
    'submitNewSession must assign payload.git = true when the checkbox is checked');
  const assignIdx = src.indexOf('payload.git = true');
  assert.ok(assignIdx >= 0, 'payload.git = true assignment not found');
  const guard = src.slice(Math.max(0, assignIdx - 200), assignIdx);
  assert.match(guard, /newSessionGitEnable[\s\S]*?\.checked/,
    'payload.git must be guarded by the newSessionGitEnable checkbox\'s `checked` state (only send git:true when checked)');
  // The unconditional payload literal must NOT contain a static git field (no git:true/false default).
  const buildIdx = src.indexOf('const payload = {');
  assert.ok(buildIdx >= 0, 'const payload = { not found in app.js');
  const literal = src.slice(buildIdx, src.indexOf('};', buildIdx));
  assert.doesNotMatch(literal, /(^|\n)\s*git:\s*(true|false)/,
    'the payload literal must not hard-code git: true/false — it is conditionally set only when the checkbox is checked');
});
