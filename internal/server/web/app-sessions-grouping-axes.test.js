'use strict';
// One test per Group-by option the sessions picker offers, in list and tile
// rendering, over a fixture with a peer-node session and Automaton task
// sessions (worker, verifier, planner; the verifier runs on a peer). Also
// checks the web grouping agrees with the server rules: one group per
// session, ungrouped last, root story first.
//
// Run: node --test internal/server/web/app-sessions-grouping-axes.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { buildSandbox, loadScript, vm, makeStubElement }
  = require('./testutil_browser_stub.js');

const WEB_DIR = __dirname;
const NOW = Date.parse('2026-09-02T12:00:00Z');

function row(id, extra) {
  return Object.assign({
    id, full_id: 'dev-box-' + id, name: 'work-' + id, state: 'running',
    hostname: 'dev-box', server: 'local', project_dir: '/p/x', task: 't', tags: [],
    created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z',
  }, extra || {});
}

function fixture() {
  return [
    row('a1', { tags: ['stage:build'] }),
    row('a2', { state: 'complete', project_dir: '/p/x/', inherited_tags: ['automation:worker', 'lane:l1'] }),
    row('b1', { hostname: 'gpu-internal', server: 'gpu-peer', project_dir: '/p/y' }),
    row('w1', { project_dir: '/wt/x', prd_id: 'prd1', prd_title: 'Ship it', story_id: 'st1', task_id: 't1', tags: ['automation:worker'] }),
    row('v1', { state: 'complete', hostname: 'gpu-internal', server: 'gpu-peer', project_dir: '/wt/x', prd_id: 'prd1', story_id: 'st1', task_id: 't1', tags: ['automation:verifier'] }),
    row('p1', { state: 'failed', project_dir: '', prd_id: 'prd1', tags: ['automation:planner'] }),
    row('n1', { state: 'waiting_input', hostname: '', server: '', project_dir: '' }),
  ];
}

function freshSandbox() {
  const byId = { 'view-sessions': makeStubElement(), 'sessionFilterInput': makeStubElement() };
  const sandbox = buildSandbox({ byId });
  vm.createContext(sandbox);
  loadScript(path.join(WEB_DIR, 'app.js'), sandbox);
  const en = JSON.parse(fs.readFileSync(path.join(WEB_DIR, 'locales', 'en.json'), 'utf8'));
  sandbox._i18n.bundle = en;
  sandbox._i18n.fallback = en;
  return sandbox;
}

const plain = v => JSON.parse(JSON.stringify(v));

// groups returns { key: [session ids] } for the axis via the real grouping.
function groups(sbx, by) {
  const out = {};
  plain(sbx._sessionsGroupSessions(fixture(), by)).forEach(g => {
    out[g.key] = g.sessions.map(s => s.id).sort();
  });
  return out;
}

// sectionOf slices one group's section out of the rendered page.
function sectionOf(html, attr, ck) {
  const i = html.indexOf(attr + '="' + ck + '"');
  assert.ok(i >= 0, 'section ' + ck);
  return html.slice(i, html.indexOf('</section>', i));
}

function eachMode(sbx, by, check) {
  for (const mode of ['list', 'tiles']) {
    const html = (by.axis === 'automaton')
      ? sbx.renderSessionsAutomaton(fixture(), NOW, mode)
      : sbx.renderSessionsGrouped(fixture(), by, NOW, mode);
    assert.equal(html.includes('session-tile-grid'), mode === 'tiles', mode + ' rendering');
    check(html, mode);
  }
}

function assertOnceEach(sbx, by) {
  const g = groups(sbx, by);
  const all = [].concat(...Object.values(g)).sort();
  assert.deepEqual(all, fixture().map(s => s.id).sort(), 'each session in exactly one group');
  return g;
}

test('state option', () => {
  const sbx = freshSandbox();
  const by = { axis: 'state', tagPrefix: '' };
  const g = assertOnceEach(sbx, by);
  assert.deepEqual(g.running, ['a1', 'b1', 'w1']);
  assert.deepEqual(g.complete, ['a2', 'v1']);
  assert.deepEqual(g.failed, ['p1']);
  eachMode(sbx, by, html => {
    const seg = sectionOf(html, 'data-sessions-group', 'state::running');
    assert.ok(seg.includes('a1') && seg.includes('b1') && seg.includes('w1') && !seg.includes('a2'));
  });
});

test('node option: peer sessions under their own node', () => {
  const sbx = freshSandbox();
  const by = { axis: 'node', tagPrefix: '' };
  const g = assertOnceEach(sbx, by);
  assert.deepEqual(g['gpu-peer'], ['b1', 'v1']);
  assert.deepEqual(g['dev-box'], ['a1', 'a2', 'p1', 'w1']);
  assert.ok(!g.local && !g['gpu-internal']);
  assert.deepEqual(Object.keys(g).pop(), 'ungrouped');
  eachMode(sbx, by, html => {
    const seg = sectionOf(html, 'data-sessions-group', 'node::gpu-peer');
    assert.ok(seg.includes('b1') && seg.includes('v1') && !seg.includes('w1'));
  });
});

test('project option: trailing slash is one folder, Automaton sessions group under the Automaton, no dir is ungrouped', () => {
  const sbx = freshSandbox();
  const by = { axis: 'project', tagPrefix: '' };
  const g = assertOnceEach(sbx, by);
  assert.deepEqual(g['/p/x'], ['a1', 'a2']);
  // An Automaton's sessions group under the Automaton, never under its worktree folder.
  assert.deepEqual(g.prd1, ['p1', 'v1', 'w1']);
  assert.deepEqual(g.ungrouped, ['n1']);
  eachMode(sbx, by, html => {
    const seg = sectionOf(html, 'data-sessions-group', 'project::/p/x');
    assert.ok(seg.includes('a1') && seg.includes('a2') && !seg.includes('b1'));
  });
});

test('tag option: sorted first match over own and inherited tags', () => {
  const sbx = freshSandbox();
  const by = { axis: 'tag', tagPrefix: '' };
  const g = assertOnceEach(sbx, by);
  assert.deepEqual(g['automation:worker'], ['a2', 'w1']);
  assert.deepEqual(g['automation:verifier'], ['v1']);
  assert.deepEqual(g['stage:build'], ['a1']);
  eachMode(sbx, by, html => {
    const seg = sectionOf(html, 'data-sessions-group', 'tag|all::automation:worker');
    assert.ok(seg.includes('a2') && seg.includes('w1') && !seg.includes('v1'));
  });
});

test('tag prefix option: only matching tags group, the rest are ungrouped', () => {
  const sbx = freshSandbox();
  const by = { axis: 'tag', tagPrefix: 'automation:' };
  const g = assertOnceEach(sbx, by);
  assert.deepEqual(g['automation:planner'], ['p1']);
  assert.deepEqual(g.ungrouped, ['a1', 'b1', 'n1']);
  eachMode(sbx, by, html => {
    const seg = sectionOf(html, 'data-sessions-group', 'tag|automation:::automation:planner');
    assert.ok(seg.includes('p1') && !seg.includes('w1'));
  });
});

test('Automaton option: story nesting, peer verifier, root first, readable label', () => {
  const sbx = freshSandbox();
  const by = { axis: 'automaton', tagPrefix: '' };
  const g = assertOnceEach(sbx, by);
  assert.deepEqual(g.prd1, ['p1', 'v1', 'w1']);
  eachMode(sbx, by, html => {
    const start = html.indexOf('data-automaton-block="automaton|::prd1"');
    assert.ok(start >= 0);
    const next = html.indexOf('data-automaton-block=', start + 10);
    const blk = html.slice(start, next < 0 ? html.length : next);
    assert.ok(blk.includes('Ship it'), 'block label is the Automaton title');
    const root = blk.indexOf('data-story-block="automaton|::prd1::root"');
    const st1 = blk.indexOf('data-story-block="automaton|::prd1::st1"');
    assert.ok(root >= 0 && st1 > root, 'root story first');
    const storySeg = blk.slice(st1);
    assert.ok(storySeg.includes('w1') && storySeg.includes('v1'), 'worker and peer-node verifier under the story');
    assert.ok(blk.slice(root, st1).includes('p1'), 'planner in the root block');
    // ungrouped bucket is last
    assert.ok(html.lastIndexOf('automaton|::ungrouped') > html.indexOf('automaton|::prd1'));
  });
});

test('local mode pulls peer rows for the Automaton axis too', () => {
  const sbx = freshSandbox();
  vm.runInContext("state.activeServer = 'local'", sbx);
  for (const axis of ['node', 'automaton']) {
    vm.runInContext("state.sessionGroupBy = { axis: '" + axis + "', tagPrefix: '' }", sbx);
    assert.equal(vm.runInContext('_sessionsNodeAxisNeedsPeers()', sbx), true, axis);
  }
  vm.runInContext("state.sessionGroupBy = { axis: 'state', tagPrefix: '' }", sbx);
  assert.equal(vm.runInContext('_sessionsNodeAxisNeedsPeers()', sbx), false);
});
