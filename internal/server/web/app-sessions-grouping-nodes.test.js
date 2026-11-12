'use strict';
// Node grouping on the sessions page: sessions from the local node and from
// peer nodes land in separate node groups, one stable label per node, in list
// and tile mode. Loads the real app.js through testutil_browser_stub.js, the
// same way app-sessions-grouping.test.js does.
//
// Run: node --test internal/server/web/app-sessions-grouping-nodes.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { buildSandbox, loadScript, vm, makeStubElement }
  = require('./testutil_browser_stub.js');

const WEB_DIR = __dirname;
const APP_JS = path.join(WEB_DIR, 'app.js');
const NOW = Date.parse('2026-09-02T12:00:00Z');

function row(id, hostname, extra) {
  return Object.assign({
    id, full_id: hostname + '-' + id, name: 'work-' + id, state: 'running',
    hostname, project_dir: '/p/x', task: 't', tags: [],
    created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z',
  }, extra || {});
}

// Aggregated-read shape: local rows carry server:'local', peer rows carry the
// peer's name; a peer may report any hostname, even an empty or the local one.
function aggregated() {
  return [
    row('a1', 'dev-box', { server: 'local' }),
    row('a2', 'dev-box', { server: 'local' }),
    row('a3', 'dev-box', { server: 'local' }),
    row('b1', 'gpu-host-internal', { server: 'gpu-peer' }),
    row('b2', 'gpu-host-internal', { server: 'gpu-peer' }),
    row('c1', '', { server: 'edge-peer' }),
    row('d1', 'dev-box', { server: 'twin-peer' }), // peer reporting the local hostname
  ];
}

function freshSandbox(fetchImpl) {
  const byId = { 'view-sessions': makeStubElement(), 'sessionFilterInput': makeStubElement() };
  const sandbox = buildSandbox({ byId });
  if (fetchImpl) sandbox.fetch = fetchImpl;
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  const en = JSON.parse(fs.readFileSync(path.join(WEB_DIR, 'locales', 'en.json'), 'utf8'));
  sandbox._i18n.bundle = en;
  sandbox._i18n.fallback = en;
  return sandbox;
}

const plain = v => JSON.parse(JSON.stringify(v));
const run = (sbx, code) => vm.runInContext(code, sbx, { filename: '<nodes-test>' });
const BY = { axis: 'node', tagPrefix: '' };

function groupMap(sbx, rows) {
  const out = {};
  plain(sbx._sessionsGroupSessions(rows, BY)).forEach(g => { out[g.key] = g.count; });
  return out;
}

test('node key: local node by real hostname, peers by peer name', () => {
  const sbx = freshSandbox();
  assert.deepEqual(groupMap(sbx, aggregated()),
    { 'dev-box': 3, 'gpu-peer': 2, 'edge-peer': 1, 'twin-peer': 1 });
});

test('node key: a local row never splits on the word local', () => {
  const sbx = freshSandbox();
  // Same node whether or not the aggregated tag is present.
  const rows = [row('x', 'dev-box'), row('y', 'dev-box', { server: 'local' })];
  assert.deepEqual(groupMap(sbx, rows), { 'dev-box': 2 });
  // Local row with no hostname uses the hostname the page learnt.
  run(sbx, "_localHostname = 'dev-box'");
  assert.deepEqual(groupMap(sbx, [row('x', ''), row('y', 'dev-box', { server: 'local' })]), { 'dev-box': 2 });
});

test('list mode: separate node groups with correct counts', () => {
  const sbx = freshSandbox();
  const html = sbx.renderSessionsGrouped(aggregated(), BY, NOW, 'list');
  assert.ok(!html.includes('session-tile-grid'));
  for (const [k, n] of [['dev-box', 3], ['gpu-peer', 2], ['edge-peer', 1], ['twin-peer', 1]]) {
    const i = html.indexOf('data-sessions-group="node::' + k + '"');
    assert.ok(i >= 0, 'group ' + k);
    const seg = html.slice(i, html.indexOf('</section>', i));
    assert.ok(seg.includes('>' + n + '<'), 'count ' + n + ' on ' + k);
  }
  assert.ok(!html.includes('node::local'), 'no group named local');
  assert.ok(!html.includes('data-sessions-group="node::gpu-host-internal"'), 'peer name, not its hostname, keys the group');
});

test('tile mode: separate node groups with correct counts', () => {
  const sbx = freshSandbox();
  const html = sbx.renderSessionsGrouped(aggregated(), BY, NOW, 'tiles');
  assert.ok(html.includes('session-tile-grid'));
  const i = html.indexOf('data-sessions-group="node::gpu-peer"');
  assert.ok(i >= 0);
  const seg = html.slice(i, html.indexOf('</section>', i));
  assert.ok(seg.includes('>2<'));
  assert.ok(seg.includes('b1') && seg.includes('b2') && !seg.includes('a1'));
});

test('local mode + node axis loads peers via the aggregated read and keeps them', async () => {
  const calls = [];
  const fetchImpl = (url) => {
    calls.push(String(url));
    return Promise.resolve({ ok: true, json: () => Promise.resolve(aggregated()) });
  };
  const sbx = freshSandbox(fetchImpl);
  run(sbx, "state.activeServer = 'local'; state.activeView = 'other'");
  run(sbx, "state.sessions = [" + JSON.stringify(row('a1', 'dev-box')) + "," + JSON.stringify(row('a2', 'dev-box')) + "]");
  run(sbx, "state._restSessionIds = new Set(['dev-box-a1','dev-box-a2'])");

  sbx.setSessionGroupBy('node');
  await new Promise(r => setTimeout(r, 20));
  assert.ok(calls.some(u => u.includes('/api/sessions/aggregated')), 'aggregated read issued');
  const keys = plain(run(sbx, 'state.sessions.map(s => s.full_id)')).sort();
  assert.deepEqual(keys, ['-c1', 'dev-box-a1', 'dev-box-a2', 'dev-box-d1', 'gpu-host-internal-b1', 'gpu-host-internal-b2']);
  assert.equal(plain(run(sbx, 'state.sessions.filter(_isPeerRow).length')), 4);

  // A websocket frame covering only this daemon does not drop the peer rows.
  run(sbx, "_applySessionsFrame([" + JSON.stringify(row('a1', 'dev-box')) + "," + JSON.stringify(row('a2', 'dev-box')) + "])");
  assert.equal(plain(run(sbx, 'state.sessions.filter(_isPeerRow).length')), 4);
  const g = plain(sbx._sessionsGroupSessions(run(sbx, 'state.sessions'), BY));
  assert.deepEqual(g.map(x => x.key).sort(), ['dev-box', 'edge-peer', 'gpu-peer', 'twin-peer']);

  // Leaving the node axis drops the peer rows again.
  sbx.setSessionGroupBy('');
  assert.equal(plain(run(sbx, 'state.sessions.filter(_isPeerRow).length')), 0);
});

test('saved view and collapse key still work on the node axis', () => {
  const sbx = freshSandbox();
  assert.equal(sbx._sessionsCollapseKey(BY, 'gpu-peer'), 'node::gpu-peer');
  const v = sbx.saveSessionsView('by node');
  assert.ok(v === null || v.name === 'by node');
});
