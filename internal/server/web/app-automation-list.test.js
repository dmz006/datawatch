// Automation tab — list cards and status filtering.
//
// Covers the pure helpers (member progress, blocked reason, budget summary,
// list filtering and ordering) and the card markup: draft, active with a fork,
// blocked, idle, budget lines and escaping. Rendered text is the real English
// bundle.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  // t() falls back to the raw key until a bundle loads; install the English
  // one so the rendered strings are the real user-visible ones.
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

// plain strips the sandbox realm off a value so deepEqual compares by content.
const plain = v => JSON.parse(JSON.stringify(v));

const NOW = Date.parse('2026-10-22T12:00:00Z');

function member(id, prdId, status, extra) {
  return Object.assign({ id, prd_id: prdId, status }, extra || {});
}

test('a draft card shows the name, the status label and zero progress', () => {
  const sb = loadApp();
  const html = sb.buildAutomationCardHTML({
    id: 'a1b2c3d4', name: 'Nightly build', status: 'draft',
    members: [member('m1', 'p1000001', 'pending'), member('m2', 'p2000002', 'pending')],
  }, { prdTitles: {}, now: NOW });
  assert.match(html, /prd-row prd-card prd-card-status-draft/);
  assert.match(html, /data-automation-id="a1b2c3d4"/);
  assert.match(html, />Nightly build</);
  assert.match(html, />Draft</);
  assert.match(html, />0 \/ 2 members</);
  assert.ok(!html.includes('Running:'), 'a draft has no running member line');
  assert.ok(!html.includes('Idle for'));
  assert.ok(!html.includes('Ceiling'));
  assert.match(html, /openAutomationDetail\('a1b2c3d4'\)/);
  assert.match(html, /typeof openAutomationDetail === 'function'/);
});

test('an active card with a fork lists every running member by Automaton title', () => {
  const sb = loadApp();
  const html = sb.buildAutomationCardHTML({
    id: 'f0f0f0f0', name: 'Fan out', status: 'active',
    active_cursors: ['b1', 'b2'],
    members: [
      member('m0', '', 'completed', { kind: 'fork', fork_members: ['b1', 'b2'] }),
      member('b1', 'aaaa1111bbbb', 'running'),
      member('b2', 'cccc2222dddd', 'running'),
      member('b3', 'eeee3333ffff', 'pending'),
    ],
  }, { prdTitles: { aaaa1111bbbb: 'Build the api', cccc2222dddd: 'Write <docs>' }, now: NOW });
  assert.match(html, />Running: Build the api</);
  assert.match(html, />Running: Write &lt;docs&gt;</);
  assert.equal((html.match(/automation-card-member/g) || []).length, 2);
  assert.ok(!html.includes('eeee3333'), 'a pending member is not listed');
});

test('a running member without cursors is listed, falling back to the short id', () => {
  const sb = loadApp();
  const html = sb.buildAutomationCardHTML({
    id: 'a1', name: 'Linear', status: 'active',
    members: [member('m1', 'abcdef0123456789', 'running'), member('m2', 'p2', 'pending')],
  }, { prdTitles: {}, now: NOW });
  assert.match(html, />Running: abcdef01</);
});

test('progress counts completed and skipped members and ignores fork and join members', () => {
  const sb = loadApp();
  const progress = plain(sb.automationMemberProgress({
    members: [
      member('m0', '', 'completed', { kind: 'fork' }),
      member('m1', 'p1', 'completed'),
      member('m2', 'p2', 'skipped', { kind: 'prd' }),
      member('m3', 'p3', 'running'),
      member('m4', '', 'pending', { kind: 'automation', automation_id: 'child' }),
      member('m5', '', 'completed', { kind: 'join' }),
      member('m6', 'p6', 'failed'),
    ],
  }));
  assert.deepEqual(progress, { done: 2, total: 5 });
  assert.deepEqual(plain(sb.automationMemberProgress({})), { done: 0, total: 0 });
  assert.deepEqual(plain(sb.automationMemberProgress(null)), { done: 0, total: 0 });
});

test('a blocked card shows the note of the latest blocked decision', () => {
  const sb = loadApp();
  const automation = {
    id: 'b1', name: 'Stuck', status: 'blocked',
    members: [member('m1', 'p1', 'blocked')],
    decisions: [
      { at: '2026-10-22T08:00:00Z', kind: 'blocked', note: 'first reason' },
      { at: '2026-10-22T09:00:00Z', kind: 'resume', note: 'resumed' },
      { at: '2026-10-22T10:00:00Z', kind: 'blocked', note: 'tests <failed>' },
      { at: '2026-10-22T11:00:00Z', kind: 'run', note: 'not a block' },
    ],
  };
  assert.equal(sb.automationBlockedReason(automation), 'tests <failed>');
  const html = sb.buildAutomationCardHTML(automation, { now: NOW });
  assert.match(html, /prd-card-status-blocked/);
  assert.match(html, />Blocked: tests &lt;failed&gt;</);
  assert.ok(!html.includes('first reason'));
});

test('the blocked reason is empty when the Automation is not blocked or has no such decision', () => {
  const sb = loadApp();
  const decisions = [{ at: '2026-10-22T10:00:00Z', kind: 'blocked', note: 'old' }];
  assert.equal(sb.automationBlockedReason({ status: 'active', decisions }), '');
  assert.equal(sb.automationBlockedReason({ status: 'blocked', decisions: [] }), '');
  assert.equal(sb.automationBlockedReason({ status: 'blocked' }), '');
  const html = sb.buildAutomationCardHTML({ id: 'b2', name: 'x', status: 'blocked' }, { now: NOW });
  assert.ok(!html.includes('Blocked:'));
});

test('an active Automation with idle_since shows the idle duration from ctx.now', () => {
  const sb = loadApp();
  const idle = (since, status) => sb.buildAutomationCardHTML({
    id: 'i1', name: 'Idle', status: status || 'active', idle_since: since,
  }, { prdTitles: {}, now: NOW });
  assert.match(idle('2026-10-22T09:55:00Z'), />Idle for 2h 5m</);
  assert.match(idle('2026-10-22T11:40:00Z'), />Idle for 20m 00s</);
  assert.ok(!idle('2026-10-22T09:55:00Z', 'paused').includes('Idle for'), 'only active Automations show idle time');
  assert.ok(!idle(undefined).includes('Idle for'));
});

test('a dollar budget shows the ceiling and window without the estimated word', () => {
  const sb = loadApp();
  const automation = {
    id: 'd1', name: 'Money', status: 'active',
    budget_policy: { ceiling_type: 'dollar', ceiling_value: 25, window: 'week', pacing: 'even', on_breach: 'pause' },
  };
  assert.deepEqual(plain(sb.automationBudgetSummary(automation)), {
    ceilingType: 'dollar', ceilingValue: 25, window: 'week', pacing: 'even', onBreach: 'pause',
    estimated: false, spent: null,
  });
  const html = sb.buildAutomationCardHTML(automation, { now: NOW });
  assert.match(html, /Ceiling: \$25/);
  assert.match(html, /Window: Weekly/);
  assert.ok(!html.includes('estimated'));
});

test('an allocation_pct budget is marked estimated', () => {
  const sb = loadApp();
  const automation = {
    id: 'p1', name: 'Share', status: 'active',
    budget_policy: { ceiling_type: 'allocation_pct', ceiling_value: 12.5, window: 'month', pacing: 'none', on_breach: 'block' },
  };
  assert.equal(sb.automationBudgetSummary(automation).estimated, true);
  const html = sb.buildAutomationCardHTML(automation, { now: NOW });
  assert.match(html, /Ceiling: 12\.50%/);
  assert.match(html, /\(estimated\)/);
  assert.match(html, /Window: Monthly/);
});

test('without a budget policy there is no summary and no budget line', () => {
  const sb = loadApp();
  assert.equal(sb.automationBudgetSummary({ id: 'n1' }), null);
  assert.equal(sb.automationBudgetSummary(null), null);
});

test('no spend bar is drawn when the record has no spend figure', () => {
  const sb = loadApp();
  const html = sb.buildAutomationCardHTML({
    id: 'd2', name: 'No spend', status: 'active',
    budget_policy: { ceiling_type: 'dollar', ceiling_value: 10, window: 'day', pacing: 'none', on_breach: 'pause' },
    spent: 4, spend: 4, cost: 4,
  }, { now: NOW });
  assert.match(html, /Ceiling: \$10/);
  assert.ok(!html.includes('automation-card-spend-bar'));
  assert.ok(!html.includes('progressbar'));
});

test('automationListVisible hides cancelled by default and shows it with history on', () => {
  const sb = loadApp();
  const list = [
    { id: 'a', name: 'Alpha', status: 'active' },
    { id: 'b', name: 'Beta', status: 'cancelled' },
    { id: 'c', name: 'Gamma', status: 'completed' },
  ];
  const base = () => ({
    search: '', historyOn: false,
    statusFilter: new Set(['draft', 'active', 'paused', 'blocked', 'completed']),
  });
  const ids = st => sb.automationListVisible(list, st).map(a => a.id);
  assert.deepEqual(ids(base()), ['a', 'c']);
  assert.deepEqual(ids(Object.assign(base(), { historyOn: true })), ['a', 'c', 'b']);
  const onlyCancelled = Object.assign(base(), { statusFilter: new Set(['cancelled']) });
  assert.deepEqual(ids(onlyCancelled), ['b']);
  const noCompleted = Object.assign(base(), { statusFilter: new Set(['active']) });
  assert.deepEqual(ids(noCompleted), ['a']);
});

test('automationListVisible matches the search on name or id regardless of case', () => {
  const sb = loadApp();
  const list = [
    { id: 'ab12cd34', name: 'Release train', status: 'active' },
    { id: 'ff00ee11', name: 'Docs sweep', status: 'active' },
  ];
  const st = q => ({ search: q, historyOn: false, statusFilter: new Set(['active']) });
  assert.deepEqual(sb.automationListVisible(list, st('RELEASE')).map(a => a.id), ['ab12cd34']);
  assert.deepEqual(sb.automationListVisible(list, st('FF00')).map(a => a.id), ['ff00ee11']);
  assert.deepEqual(sb.automationListVisible(list, st('  sweep ')).map(a => a.id), ['ff00ee11']);
  assert.deepEqual(sb.automationListVisible(list, st('nothing')), []);
});

test('automationListVisible orders by status, then by name, without touching the input', () => {
  const sb = loadApp();
  const list = [
    { id: '1', name: 'zeta', status: 'completed' },
    { id: '2', name: 'beta', status: 'draft' },
    { id: '3', name: 'Alpha', status: 'cancelled' },
    { id: '4', name: 'omega', status: 'active' },
    { id: '5', name: 'Gamma', status: 'paused' },
    { id: '6', name: 'delta', status: 'blocked' },
    { id: '7', name: 'alpha', status: 'active' },
  ];
  const before = list.map(a => a.id).join(',');
  const out = sb.automationListVisible(list, {
    search: '', historyOn: true,
    statusFilter: new Set(['draft', 'active', 'paused', 'blocked', 'completed']),
  });
  assert.deepEqual(out.map(a => a.id), ['7', '4', '6', '5', '2', '1', '3']);
  assert.equal(list.map(a => a.id).join(','), before);
});

test('a hostile name and id are escaped everywhere on the card', () => {
  const sb = loadApp();
  const html = sb.buildAutomationCardHTML({
    id: `x');alert(1);('`,
    name: `<img src=x onerror=alert(1)> "q" 'a'`,
    status: 'blocked',
    members: [member('m1', `<b>p</b>`, 'running')],
    decisions: [{ at: '2026-10-22T10:00:00Z', kind: 'blocked', note: '<script>boom()</script>' }],
    budget_policy: { ceiling_type: 'dollar', ceiling_value: 1, window: '<i>w</i>' },
  }, { prdTitles: { '<b>p</b>': '<u>title</u>' }, now: NOW });
  assert.ok(!html.includes('<img'), 'name markup is escaped');
  assert.ok(!html.includes('<script>'));
  assert.ok(!html.includes('<u>') && !html.includes('<i>'));
  assert.ok(html.includes('&lt;img src=x onerror=alert(1)&gt;'));
  assert.ok(html.includes('&quot;q&quot;') && html.includes('&#39;a&#39;'));
  assert.ok(html.includes('&lt;u&gt;title&lt;/u&gt;'));
  // the id sits in a JS string inside an attribute: its quote must be backslash-escaped
  assert.ok(html.includes(`openAutomationDetail('x\\&#39;);alert(1);(\\&#39;')`), 'id cannot end the handler string');
});

test('loadAutomationList fetches Automaton titles once and the cards use them', async () => {
  const sb = loadApp();
  const panel = { innerHTML: '' };
  sb.document.getElementById = id => (id === 'automationPanel' ? panel : { style: {}, classList: { toggle() {} } });
  const calls = [];
  sb.fetch = url => {
    calls.push(url);
    const body = url === '/api/autonomous/prds'
      ? { prds: [{ id: 'prd00001', title: 'Ship the thing' }] }
      : [{ id: 'a1', name: 'Chain', status: 'active', active_cursors: ['m1'], members: [member('m1', 'prd00001', 'running')] }];
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) });
  };
  await sb.loadAutomationList();
  assert.equal(calls.filter(u => u === '/api/autonomous/prds').length, 1);
  assert.match(panel.innerHTML, />Running: Ship the thing</);
  assert.deepEqual(plain(vm.runInContext('_automationState', sb).prdTitles), { prd00001: 'Ship the thing' });
});

test('a failed Automaton title lookup leaves the titles empty and still renders the list', async () => {
  const sb = loadApp();
  const panel = { innerHTML: '' };
  sb.document.getElementById = id => (id === 'automationPanel' ? panel : { style: {}, classList: { toggle() {} } });
  sb.fetch = url => {
    if (url === '/api/autonomous/prds') return Promise.reject(new Error('down'));
    return Promise.resolve({
      ok: true, status: 200,
      json: () => Promise.resolve([{ id: 'a1', name: 'Chain', status: 'active', members: [member('m1', 'prd00001', 'running')] }]),
    });
  };
  await sb.loadAutomationList();
  assert.deepEqual(plain(vm.runInContext('_automationState', sb).prdTitles), {});
  assert.match(panel.innerHTML, />Running: prd00001</);
});

test('the list also accepts a bare array of Automata from the title lookup', async () => {
  const sb = loadApp();
  sb.document.getElementById = () => null;
  sb.fetch = url => Promise.resolve({
    ok: true, status: 200,
    json: () => Promise.resolve(url === '/api/autonomous/prds' ? [{ id: 'p9', title: 'Nine' }] : []),
  });
  await sb.loadAutomationList();
  assert.deepEqual(plain(vm.runInContext('_automationState', sb).prdTitles), { p9: 'Nine' });
});

test('the helpers are exposed on window', () => {
  const sb = loadApp();
  for (const name of ['automationMemberProgress', 'automationBlockedReason', 'automationBudgetSummary', 'automationListVisible', 'buildAutomationCardHTML']) {
    assert.equal(typeof vm.runInContext('window.' + name, sb), 'function', name + ' is on window');
  }
});
