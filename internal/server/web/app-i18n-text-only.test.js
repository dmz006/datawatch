'use strict';
// applyI18nDOM writes a translation as text or as one named attribute, never as
// markup. The key it looks up is read back from the page, and t() returns the
// key itself when no bundle holds it, so a markup path would turn page text into
// HTML (code scanning rule js/xss-through-dom).
//
//   node --test internal/server/web/app-i18n-text-only.test.js

const { test } = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const vm = require('vm');
const { buildSandbox, loadScript } = require('./testutil_browser_stub.js');

function load() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

// fakeEl records every write applyI18nDOM can make.
function fakeEl(attrs) {
  const el = {
    set: {},
    getAttribute: n => (n in attrs ? attrs[n] : null),
    hasAttribute: n => n in attrs,
    setAttribute: (n, v) => { el.set['@' + n] = v; },
    set innerHTML(v) { el.set.innerHTML = v; },
    set textContent(v) { el.set.textContent = v; },
  };
  return el;
}

const PAYLOAD = '<img src=x onerror=alert(1)>';

test('a key read from the page is never written as markup', () => {
  const sb = load();
  const el = fakeEl({ 'data-i18n': PAYLOAD, 'data-i18n-html': '' });
  sb.applyI18nDOM({ querySelectorAll: () => [el] });
  assert.deepEqual(el.set, { textContent: PAYLOAD });
});

test('text and named-attribute translation still work', () => {
  const sb = load();
  sb.window._i18n.bundle = { hello: 'Hallo <b>' };
  const text = fakeEl({ 'data-i18n': 'hello' });
  const title = fakeEl({ 'data-i18n': 'hello', 'data-i18n-attr': 'title' });
  sb.applyI18nDOM({ querySelectorAll: () => [text, title] });
  assert.deepEqual(text.set, { textContent: 'Hallo <b>' });
  assert.deepEqual(title.set, { '@title': 'Hallo <b>' });
});
