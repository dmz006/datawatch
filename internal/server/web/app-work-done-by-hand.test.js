// Work finished by hand reads done in the web app: the status chip, the
// Completed filter and its count. A record with a shipped record, or closed
// with outcome delivered, reads Completed. A held or open record never does.
//
//   node --test app-work-done-by-hand.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function loadApp() {
  const sandbox = buildSandbox({ byId: {}, localStorage: new Map() });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  sandbox.window._i18n.fallback = en;
  sandbox.window._i18n.bundle = en;
  return sandbox;
}

// Full records (the detail page) carry no effective_status; list rows do.
const delivered = { id: 'c1', status: 'cancelled', closed_outcome: 'delivered' };
const deliveredRow = { id: 'c2', status: 'archived', closed_outcome: 'delivered', effective_status: 'completed' };
const shippedRow = { id: 'c3', status: 'cancelled', merge_commit: 'abc123', effective_status: 'completed' };
const draftShipped = { id: 'c4', status: 'draft', merge_commit: 'abc123' };
const heldShipped = { id: 'h1', status: 'held', merge_commit: 'abc123' };
const heldRow = { id: 'h2', status: 'held', merge_commit: 'abc123', effective_status: 'held' };
const rerunRow = { id: 'r1', status: 'running', merge_commit: 'abc123', effective_status: 'running' };
const openDelivered = { id: 'o1', status: 'draft', closed_outcome: 'delivered' };
const dropped = { id: 'x1', status: 'cancelled', closed_outcome: 'dropped' };
const superseded = { id: 'x2', status: 'cancelled', closed_outcome: 'superseded' };

const DONE = [delivered, deliveredRow, shippedRow, draftShipped];
const NOT_DONE = [heldShipped, heldRow, rerunRow, openDelivered, dropped, superseded];

test('status chip: finished work says Completed', () => {
  const s = loadApp();
  for (const rec of DONE) {
    assert.match(s.shippedChipHtml(rec), />Completed/, rec.id);
  }
});

test('status chip: held and open records never say Completed', () => {
  const s = loadApp();
  for (const rec of NOT_DONE) {
    assert.equal(s.shippedChipHtml(rec), '', rec.id);
  }
});

test('status chip: a delivered outcome says Completed with the shipped chip switched off', () => {
  const s = loadApp();
  s._shippedState.show_chip = false;
  assert.match(s.shippedChipHtml(delivered), />Completed/);
  s._shippedState.enabled = false;
  assert.match(s.shippedChipHtml(delivered), />Completed/);
  assert.equal(s.shippedChipHtml(draftShipped), '');
});

function ids(list) { return Array.from(list, r => r.id).sort(); }

function selectStatus(s, status) {
  s.__all = [...DONE, ...NOT_DONE];
  vm.runInContext(`_automataState.allPrds = __all; _automataState.statusFilter = new Set(${JSON.stringify(status ? [status] : [])});`, s);
}

test('list: the Completed filter keeps the finished records and only those', () => {
  const s = loadApp();
  selectStatus(s, 'completed');
  assert.deepEqual(ids(s._tagBrowseItems('automata', s.__all)), ids(DONE));
});

test('list: the Cancelled filter no longer keeps finished records', () => {
  const s = loadApp();
  selectStatus(s, 'cancelled');
  assert.deepEqual(ids(s._tagBrowseItems('automata', s.__all)), ids([dropped, superseded]));
});

test('list: the Completed badge counts the finished records', () => {
  const s = loadApp();
  selectStatus(s, '');
  s.__base = s.__all;
  vm.runInContext('_tagFacetBase = () => __base;', s);
  const html = s._tagFacetsHTML('automata', false);
  const count = st => {
    const m = html.match(new RegExp(`automata-filter-badge status-${st}[^>]*>[^<]*<span class="tag-facet-count">(\\d+)<`));
    assert.ok(m, `no ${st} badge`);
    return Number(m[1]);
  };
  assert.equal(count('completed'), DONE.length);
  assert.equal(count('cancelled'), 2);
  assert.equal(count('held'), 2);
});
