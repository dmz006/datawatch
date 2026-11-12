// The Vault unreachable nav badge on the Settings nav button was removed:
// `_setVaultBadge()` (which created/updated the `vaultUnreachableBadge`
// element) and its call sites in `loadVaultStatusRow` are gone. The status
// row itself (`loadVaultStatusRow`, also exposed as
// `window.loadVaultStatusRow`) must remain.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');

function readFile(name) {
  return fs.readFileSync(path.join(__dirname, name), 'utf8');
}

const APP_JS = 'app.js';
const INDEX_HTML = 'index.html';
const STYLE_CSS = 'style.css';

test('vaultUnreachableBadge is absent from app.js, index.html and style.css', () => {
  for (const name of [APP_JS, INDEX_HTML, STYLE_CSS]) {
    assert.ok(
      !readFile(name).includes('vaultUnreachableBadge'),
      `${name} still references vaultUnreachableBadge`,
    );
  }
});

test('_setVaultBadge is absent from app.js, index.html and style.css', () => {
  for (const name of [APP_JS, INDEX_HTML, STYLE_CSS]) {
    assert.ok(
      !readFile(name).includes('_setVaultBadge'),
      `${name} still references _setVaultBadge`,
    );
  }
});

test('loadVaultStatusRow still exists in app.js', () => {
  const src = readFile(APP_JS);
  assert.ok(src.includes('function loadVaultStatusRow'), 'loadVaultStatusRow function definition is missing');
  assert.ok(src.includes('window.loadVaultStatusRow = loadVaultStatusRow'), 'window.loadVaultStatusRow export is missing');
});
