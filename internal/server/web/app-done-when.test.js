'use strict';
// env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-done-when.test.js
const test = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');
const dw = require('./app-done-when.js');

const view = {
  mode: 'report', hash: 'h1',
  list: {
    frozen_at: '2026-11-26T09:00:00Z',
    items: [
      { id: 'a', statement: '<img src=x onerror=1>', method: 'confirm' },
      { id: 'b', statement: 'checked by a command', method: 'command' },
      { id: 'c', statement: 'odd', method: 'review' },
    ],
  },
  flags: [{ text: 'flag <b>' }],
  changes: { removed: [{ statement: 'was here' }] },
  rollup: {
    status: 'met_on_changed_list',
    items: [
      { id: 'a', result: { value: 'unchecked' }, claim: 'claimed met by a session' },
      { id: 'b', result: { value: 'cannot_tell', reason: 'no reviewer' }, accepted_without_proof: true },
      { id: 'c', result: { value: 'great' } },
    ],
  },
};

test('an unknown or accepted result never reads met', () => {
  const r = dw.rows(view);
  assert.equal(r[1].shown, 'accepted without proof');
  assert.equal(r[2].shown, 'great');
  assert.notEqual(r[2].value, 'met');
  assert.equal(dw.resultText(undefined), 'unchecked');
});

test('server text is escaped and nothing uses inline script', () => {
  const html = dw.tableHtml(view);
  assert.ok(!html.includes('<img'));
  assert.ok(!html.includes('flag <b>'));
  assert.ok(!/\son[a-z]+=["']/.test(html));
  assert.ok(html.includes('was here'));
  assert.ok(html.includes('not plain done'));
});

test('only a confirm item offers confirm; a draft offers fixing, not accepting', () => {
  const html = dw.tableHtml(view);
  assert.equal((html.match(/data-dw-action="yes"/g) || []).length, 1);
  assert.ok(!html.includes('data-dw-action="freeze"'));
  const draft = dw.tableHtml({ ...view, list: { ...view.list, frozen_at: '0001-01-01T00:00:00Z' } });
  assert.ok(draft.includes('data-dw-action="freeze"'));
  assert.ok(!draft.includes('data-dw-action="accept"'));
  assert.ok(!draft.includes('data-dw-action="yes"'));
});

test('writes carry the words and the list hash, and edit cannot set a result', () => {
  assert.ok(dw.request('yes', 'r', 'a', {}, 'h').error);
  assert.ok(dw.request('accept', 'r', 'a', { words: 'ok' }, 'h').error);
  assert.deepEqual(dw.request('no', 'r', 'a', { words: 'no' }, 'h').body, { id: 'r', actor: 'web', words: 'no', item: 'a', answer: 'no', hash: 'h' });
  assert.deepEqual(dw.request('freeze', 'r', '', {}, 'h').body, { id: 'r', actor: 'web', freeze: true, hash: 'h' });
  const edit = dw.request('edit', 'r', '', { changes: '{"remove":["a"],"id":"other","actor":"operator","via_session":"x","result":"met"}' }, 'h');
  assert.deepEqual(edit.body, { id: 'r', actor: 'web', remove: ['a'], hash: 'h' });
  assert.ok(dw.request('edit', 'r', '', { changes: '[1]' }, 'h').error);
});

test('every result, status and string has a key in all five locales', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app-done-when.js'), 'utf8');
  const keys = new Set([...src.matchAll(/T\('(done_when_[a-z_]+)'/g)].map(m => m[1]).filter(k => !k.endsWith('_')));
  Object.keys(dw.RESULTS).forEach(k => keys.add('done_when_result_' + k));
  Object.keys(dw.STATUSES).forEach(k => keys.add('done_when_status_' + k));
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    for (const k of keys) assert.ok(bundle[k], `${lang} lacks ${k}`);
  }
});

test('the statuses match the daemon', () => {
  const go = fs.readFileSync(path.join(__dirname, '..', '..', 'acceptance', 'rollup.go'), 'utf8');
  const daemon = [...go.matchAll(/^\tStatus\w+\s+Status = "([a-z_]+)"/gm)].map(m => m[1]).sort();
  assert.ok(daemon.length > 10);
  assert.deepEqual(Object.keys(dw.STATUSES).sort(), daemon);
});

test('the module is loaded, cached and mounted', () => {
  const read = f => fs.readFileSync(path.join(__dirname, f), 'utf8');
  assert.ok(read('index.html').includes('/app-done-when.js?v='));
  assert.ok(read('sw.js').includes("'/app-done-when.js'"));
  assert.ok(read('app.js').includes('window.doneWhen.mount(body, id)'));
});
