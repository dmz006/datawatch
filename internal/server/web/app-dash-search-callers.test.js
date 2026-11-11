// Dashboard "Search Usage" card (id 'websearch-usage'): the counts-by-caller
// list, its empty case, escaping, the card gate, and locale coverage.

'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { buildSandbox, loadScript, vm } = require('./testutil_browser_stub.js');

const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const bundle = lang => JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));

function load() {
  const sandbox = vm.createContext(buildSandbox());
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox.__en = bundle('en');
  vm.runInContext('window._i18n = { bundle: __en, fallback: __en };', sandbox);
  return sandbox;
}
const evalIn = (sb, code) => vm.runInContext(code, sb);
const textOf = html => String(html).replace(/<[^>]*>/g, ' ').replace(/\s+/g, ' ');

test('callers list shows a label and count per caller', () => {
  const sb = load();
  sb.__c = [{ caller: 'session', total: 4 }, { caller: 'automaton', total: 2 }, { caller: 'cli', total: 1 }];
  const txt = textOf(evalIn(sb, '_dashWebSearchCallersHtml(__c)'));
  assert.match(txt, /by caller/);
  assert.match(txt, /session 4/);
  assert.match(txt, /Automaton task 2/);
  assert.match(txt, /CLI 1/);
});

test('callers list is empty when nothing has a caller', () => {
  const sb = load();
  assert.equal(evalIn(sb, '_dashWebSearchCallersHtml(undefined)'), '');
  assert.equal(evalIn(sb, '_dashWebSearchCallersHtml([])'), '');
  assert.equal(evalIn(sb, '_dashWebSearchCallersHtml([{caller:"rest",total:0}])'), '');
});

test('unknown caller names are escaped', () => {
  const sb = load();
  sb.__c = [{ caller: '<img src=x onerror=alert(1)>', total: 1 }];
  const html = evalIn(sb, '_dashWebSearchCallersHtml(__c)');
  assert.ok(!html.includes('<img'));
});

test('card stays off without providers', () => {
  const sb = load();
  evalIn(sb, '_dash._webSearchStats = {enabled:true, summary:{providers:[]}, callers:[{caller:"cli",total:3}]}');
  assert.equal(evalIn(sb, "DASH_CARD_FEATURES['websearch-usage'].active()"), false);
  evalIn(sb, '_dash._webSearchStats = {enabled:true, summary:{providers:[{name:"a",total:1}]}}');
  assert.equal(evalIn(sb, "DASH_CARD_FEATURES['websearch-usage'].active()"), true);
});

test('every locale has the caller keys', () => {
  for (const l of LANGS) {
    const b = bundle(l);
    for (const k of ['websearch_callers', 'websearch_caller_session', 'websearch_caller_automaton',
      'websearch_caller_mcp', 'websearch_caller_rest', 'websearch_caller_cli']) {
      assert.ok(b[k], `${l} missing ${k}`);
    }
  }
});
