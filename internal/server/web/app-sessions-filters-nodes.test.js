'use strict';
// Filters sheet, compute-node section: the node chips are rebuilt from
// state.sessions (not the raw tag inventory), so
//   - one local host and its aliases land under ONE entry named by the host's
//     real name — never the word 'local';
//   - a federated peer's sessions (rows tagged server:<peer>, no compute tag
//     at all, no compute node record) appear as one entry by the peer name;
//   - selecting one entry filters the list to exactly that node's sessions
//     through the shared tag selection and _sessionTagFilter;
//   - each entry's count is the number of sessions that node carries.
//
// Loads the real app.js through testutil_browser_stub.js, the same way
// app-sessions-filters-sheet.test.js does.
//
// Run: node --test internal/server/web/app-sessions-filters-nodes.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub.js');
const vm = require('vm');

const WEB_DIR = __dirname;
const APP_JS = path.join(WEB_DIR, 'app.js');

const HOST = 'dev-box';           // the local host's real name
const PEER = 'ralfthewise';       // a federated peer, by the name it is registered under

function local(id, extra) {
  return Object.assign({
    id, full_id: HOST + '-' + id, name: 'work-' + id, state: 'running',
    server: 'local', hostname: HOST,
    tags: [], system_tags: [], inherited_tags: [],
    backend_family: 'gpt',
    created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z',
  }, extra || {});
}

// A peer's row from the aggregated read: tagged with the peer name in
// `server`, and carrying no compute tag and no node identity of its own.
function peer(id) {
  return {
    id, full_id: PEER + '-' + id, name: 'work-' + id, state: 'waiting_input',
    server: PEER, hostname: '',
    tags: [], system_tags: [], inherited_tags: [],
    backend_family: 'pi',
    created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z',
  };
}

// The local node: the host itself plus its aliases. l2 never reports a
// hostname of its own (the page knew dev-box from /api/health); l3 and l4
// carry compute tags from when the node went by other names.
function localSessions() {
  return [
    local('l1'),
    local('l2', { hostname: '' }),
    local('l3', { system_tags: ['compute:' + HOST] }),
    local('l4', { system_tags: ['compute:dev-box-alias'] }),
  ];
}

function peerSessions() { return [peer('p1'), peer('p2')]; }

function sessions() { return localSessions().concat(peerSessions()); }

function freshSandbox(rows) {
  const byId = { 'view-sessions': makeStubElement(), 'sessionFilterInput': makeStubElement() };
  const sandbox = buildSandbox({ byId });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  const en = JSON.parse(fs.readFileSync(path.join(WEB_DIR, 'locales', 'en.json'), 'utf8'));
  sandbox._i18n.bundle = en;
  sandbox._i18n.fallback = en;
  vm.runInContext('_localHostname = ' + JSON.stringify(HOST) + ';', sandbox);
  vm.runInContext('state.sessions = ' + JSON.stringify(rows || sessions()) + ';', sandbox);
  vm.runInContext('state._sessionTagInventory = { loaded: true, entries: [] };', sandbox);
  return sandbox;
}

const plain = v => JSON.parse(JSON.stringify(v));
const run = (sbx, code) => vm.runInContext(code, sbx, { filename: '<filters-nodes-test>' });

// The panel renders once the compute group has entries (the inventory is
// allowed to be empty — peers are invisible to it).
function panelHTML(sbx) {
  return run(sbx, '_sessionTagFilterPanelHTML()') || '';
}

function entries(sbx) {
  return plain(run(sbx, '_sessionsComputeGroupEntries("")'));
}

test('(a) local host and its aliases collapse to one entry under the real hostname, never "local"', () => {
  const sbx = freshSandbox();

  const es = entries(sbx);
  const localEntry = es.filter(e => e.entry.value !== PEER);
  assert.equal(localEntry.length, 1, 'local node is a single entry, got: ' + JSON.stringify(es));
  assert.equal(localEntry[0].entry.value, HOST, 'the entry carries the host real name');
  assert.equal(localEntry[0].entry.tag, 'compute:' + HOST);

  assert.ok(!es.some(e => e.entry.value === 'local'), 'no entry named local');
  assert.ok(!es.some(e => /alias/i.test(e.entry.value)), 'an alias of the host never becomes its own entry');

  const html = panelHTML(sbx);
  assert.ok(html.includes('sessionTagChipToggle(\'' + 'compute:' + HOST + '\')'), 'chip selects the real-name entry');
  assert.ok(!html.includes('sessionTagChipToggle(\'local\')'), 'no chip selects the word local');
  assert.ok(!html.includes('>local<'), 'the word local is not a visible chip name');
  assert.ok(!/dev-box-alias/.test(html.replace(/sessionTagChipToggle/g, '')) ||
            !html.includes('>dev-box-alias<'), 'alias name is not shown as a chip');
});

test('(b) the peer appears as an entry by its peer name, with the right count', () => {
  const sbx = freshSandbox();

  const peerEntry = entries(sbx).find(e => e.entry.value === PEER);
  assert.ok(peerEntry, 'peer entry present');
  assert.equal(peerEntry.entry.tag, 'compute:' + PEER);
  assert.equal(peerEntry.entry.axis, 'compute');
  assert.equal(peerEntry.count, 2, 'the peer entry counts exactly its sessions');

  const html = panelHTML(sbx);
  assert.ok(html.includes(PEER), 'peer name is visible in the panel');
  const m = new RegExp(PEER + '<span class="badge-count">(\\d+)<').exec(html);
  assert.ok(m, 'peer chip shows a count');
  assert.equal(m[1], '2');
});

test('(c) selecting the peer entry filters the list to only that peer\'s sessions', () => {
  const sbx = freshSandbox();
  const all = sessions().map(s => s.full_id).sort();

  // Selecting the peer chip lands in the shared session tag selection...
  sbx.sessionTagChipToggle('compute:' + PEER);
  assert.deepEqual(plain(run(sbx, '_tagBrowse.session.sel')), ['compute:' + PEER]);

  // ...and the client-side filter keeps only the peer's own rows.
  const kept = plain(run(sbx, '_sessionTagFilter(state.sessions)')).map(s => s.full_id).sort();
  assert.deepEqual(kept, [PEER + '-p1', PEER + '-p2']);
  assert.ok(kept.every(f => String(f).indexOf(PEER) === 0), 'only rows of the selected peer survive');

  // And the panel marks exactly that entry active.
  const html = panelHTML(sbx);
  assert.ok(html.includes('sessions-tag-chip active') && html.indexOf('compute:' + PEER) >= 0, 'selected peer chip is active');

  // Deselecting restores the full list.
  sbx.sessionTagChipToggle('compute:' + PEER);
  assert.deepEqual(plain(run(sbx, '_tagBrowse.session.sel')), []);
  assert.deepEqual(plain(run(sbx, '_sessionTagFilter(state.sessions)')).map(s => s.full_id).sort(), all);
});

test('(c2) selecting the local entry filters to the local node and its aliases only, never the peer', () => {
  const sbx = freshSandbox();

  sbx.sessionTagChipToggle('compute:' + HOST);
  const kept = plain(run(sbx, '_sessionTagFilter(state.sessions)')).map(s => s.full_id).sort();
  assert.deepEqual(kept, ['dev-box-l1', 'dev-box-l2', 'dev-box-l3', 'dev-box-l4']);
  assert.ok(kept.every(f => f.indexOf(PEER) < 0), 'peer rows are out');
});

test('(d) every entry count equals the number of sessions that node carries', () => {
  const sbx = freshSandbox();

  const rows = sessions();
  const expect = {};
  rows.forEach(s => {
    const names = plain(run(sbx, '_sessionComputeNodeNames(' + JSON.stringify(s) + ')'));
    const key = names[0];
    assert.ok(key, 'every session resolves to a node name');
    expect[key] = (expect[key] || 0) + 1;
  });
  assert.deepEqual(expect, { [HOST]: 4, [PEER]: 2 }, 'raw per-node counts');

  const got = {};
  entries(sbx).forEach(e => { got[e.entry.value] = e.count; });
  assert.deepEqual(got, expect, 'sheet entry count matches the sessions per node');
});

test('no session, no entry — the section stays empty rather than inventing a local chip', () => {
  const sbx = freshSandbox([]);
  assert.deepEqual(entries(sbx), []);
  assert.ok(!panelHTML(sbx).includes('sessions-tag-chip'), 'no node chips without sessions');
});
