// Sessions Filters sheet, compute axis: peers and the local node each appear
// once under the name the node grouping already uses. A peer's session rows
// (tagged s.server=<peer> by the aggregated read) never carry a compute tag,
// so the group and the filter key on the session's node resolution, not on the
// inventory's raw "compute:<value>" values. A local host shows under its real
// name (never the word 'local'); alias spellings of that name merge into one
// entry with summed counts.
//
// Results come back as foreign-realm arrays from the vm sandbox, so every
// value is pulled into a host array via Array.from(...) before asserting --
// the same idiom app-session-tag-filter.test.js relies on.
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const WEB = __dirname;
const run = (sb, code) => vm.runInContext(code, sb);

function loadApp() {
  const sandbox = buildSandbox({});
  vm.createContext(sandbox);
  loadScript(path.join(WEB, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  run(sandbox, `state.activeView='sessions'; state._sessionTagInventory = { loaded: true, entries: [] }; _tagBrowse.session.sel = [];`);
  return sandbox;
}

// Three local rows: the local host is 'datawatch', but its sessions carry three
// different compute spellings ('local', 'johnnyjohnny', 'datawatch') -- two of
// them aliases of the real name. Two rows each for two peers, tagged s.server
// and carrying NO compute tag at all.
function seed(sb) {
  const local = (id, compute) => ({
    id, full_id: 'local/' + id, state: 'running', server: 'local', hostname: 'datawatch',
    tags: (compute ? ['compute:' + compute] : []),
  });
  const peer = (id, server) => ({
    id, full_id: server + '/' + id, state: 'running', server,
    tags: [], system_tags: ['s.server:' + server],
  });
  const sessions = [
    local('l1', 'local'), local('l2', 'johnnyjohnny'), local('l3', 'datawatch'),
    peer('p1', 'peernode'), peer('p2', 'peernode'),
    peer('q1', 'otherpeer'),
  ];
  run(sb, `state.sessions = ${JSON.stringify(sessions)};`);
}

const groupNames = (sb, q) => Array.from(run(sb, `_sessionsComputeGroupEntries(${JSON.stringify(q)})`)).map(e => e.entry.value);
const group = (sb, q, name) => Array.from(run(sb, `_sessionsComputeGroupEntries(${JSON.stringify(q)})`)).find(e => e.entry.value === name);
const filteredIds = (sb) => Array.from(run(sb, `_sessionTagFilter(state.sessions)`)).map(s => s.id);

test('peers list under their peer name and keep the compute axis', () => {
  const sb = loadApp(); seed(sb);
  const p = group(sb, '', 'peernode');
  assert.ok(p, 'peernode entry present');
  assert.equal(p.count, 2);
  assert.equal(p.entry.axis, 'compute');
  assert.equal(p.entry.tag, 'compute:peernode');
  const o = group(sb, '', 'otherpeer');
  assert.ok(o, 'otherpeer entry present');
  assert.equal(o.count, 1);
});

test('the local node lists under its real name, never the word local', () => {
  const sb = loadApp(); seed(sb);
  const names = groupNames(sb, '');
  assert.ok(names.includes('datawatch'), 'real hostname is a chip');
  assert.ok(!names.includes('local'), 'the literal local is never a chip');
  assert.ok(!names.includes('johnnyjohnny'), 'an alias of the real name does not split the node');
});

test('alias spellings of the local host merge into one summed entry', () => {
  const sb = loadApp(); seed(sb);
  const d = group(sb, '', 'datawatch');
  assert.equal(d.count, 3, 'three local rows, three spellings, one entry');
  const names = groupNames(sb, '').sort();
  assert.deepEqual(names, ['datawatch', 'otherpeer', 'peernode'], 'exactly the three nodes, aliases merged');
});

test('search narrows to the node name, case-insensitively', () => {
  const sb = loadApp(); seed(sb);
  assert.deepEqual(groupNames(sb, 'peer').sort(), ['otherpeer', 'peernode']);
  assert.deepEqual(groupNames(sb, 'data'), ['datawatch']);
  assert.deepEqual(groupNames(sb, 'DATA'), ['datawatch']);
  assert.deepEqual(groupNames(sb, 'nomatch'), []);
});

test('selecting a peer entry keeps exactly that peer sessions', () => {
  const sb = loadApp(); seed(sb);
  run(sb, `_tagBrowse.session.sel = ['compute:peernode'];`);
  assert.deepEqual(filteredIds(sb).sort(), ['p1', 'p2']);
});

test('selecting a different peer keeps only those rows', () => {
  const sb = loadApp(); seed(sb);
  run(sb, `_tagBrowse.session.sel = ['compute:otherpeer'];`);
  assert.deepEqual(filteredIds(sb), ['q1']);
});

test('selecting the local node keeps the local rows under their real name', () => {
  const sb = loadApp(); seed(sb);
  run(sb, `_tagBrowse.session.sel = ['compute:datawatch'];`);
  assert.deepEqual(filteredIds(sb).sort(), ['l1', 'l2', 'l3']);
});

test('ALL mode across a peer and the local node keeps no single row', () => {
  const sb = loadApp(); seed(sb);
  run(sb, `_tagBrowse.session.sel = ['compute:datawatch', 'compute:peernode']; _tagBrowse.session.mode = 'all';`);
  assert.deepEqual(filteredIds(sb), []);
  run(sb, `_tagBrowse.session.mode = 'any';`);
  assert.deepEqual(filteredIds(sb).sort(), ['l1', 'l2', 'l3', 'p1', 'p2']);
});
