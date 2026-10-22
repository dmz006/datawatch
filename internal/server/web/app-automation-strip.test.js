// Automation tab — pipeline strip.
//
// Covers the display-status helper and the strip markup: a linear chain, the
// active ring, branch points with their labelled sub-rows, which connector was
// taken, inserted members, unreachable members and a malformed cycle. Rendered
// text is the real English bundle.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

function member(id, status, extra) {
  return Object.assign({ id, prd_id: id + 'aaaaaaaaaaaa', status, added_by: 'operator' }, extra || {});
}

// cards returns the opening tag of every full member card, in document order.
function cards(html) {
  return html.match(/<div class="automation-member [^>]*data-member-id="[^"]*"[^>]*>/g) || [];
}

function connectors(html, edge) {
  const all = html.match(/<span class="automation-connector[^>]*>/g) || [];
  return edge ? all.filter(c => c.includes(`data-edge="${edge}"`)) : all;
}

// row returns the markup of the strip row whose data-row is key.
function row(html, key) {
  const start = html.indexOf(`<div class="automation-strip-row" data-row="${key}">`);
  assert.notEqual(start, -1, 'row ' + key + ' is present');
  const next = html.indexOf('<div class="automation-strip-row"', start + 1);
  return html.slice(start, next === -1 ? html.length : next);
}

function linear() {
  return {
    id: 'a1b2c3d4', name: 'Linear', status: 'active', entry_member_id: 'm1',
    members: [
      member('m1', 'completed', { on_complete: 'm2', edge_taken: 'on_complete' }),
      member('m2', 'running', { on_complete: 'm3' }),
      member('m3', 'pending'),
    ],
  };
}

test('a three-member chain renders three cards in order with two on_complete connectors', () => {
  const sb = loadApp();
  const html = sb.buildAutomationStripHTML(linear(), { prdTitles: { m1aaaaaaaaaaaa: 'First' }, prdStatuses: {} });
  assert.match(html, /^<div class="automation-strip" data-automation-id="a1b2c3d4">/);
  assert.deepEqual(cards(html).map(c => c.match(/data-member-id="([^"]*)"/)[1]), ['m1', 'm2', 'm3']);
  assert.match(html, /data-row="main"/);
  assert.equal(connectors(html).length, 2);
  assert.equal(connectors(html, 'on_complete').length, 2);
  assert.match(html, /<span class="automation-connector" data-edge="on_complete" data-from="m1" data-to="m2" data-taken="true">/);
  assert.match(html, /data-from="m2" data-to="m3" data-taken="unknown"/);
  assert.match(html, /<span class="automation-dot"><\/span>/);
  assert.match(html, /<span class="automation-member-title">First<\/span>/);
  assert.match(html, /<span class="automation-chip">m1aaaaaa<\/span>/);
  assert.ok(!html.includes('automation-fork-glyph'), 'a straight chain has no fork glyph');
  assert.ok(!html.includes('data-row="unlinked"'));
});

test('the entry member and the active_cursors member get the active ring, the others do not', () => {
  const sb = loadApp();
  const draft = linear();
  draft.status = 'draft';
  draft.members = [member('m1', 'pending', { on_complete: 'm2' }), member('m2', 'pending', { on_complete: 'm3' }), member('m3', 'pending')];
  let html = sb.buildAutomationStripHTML(draft, {});
  let tags = cards(html);
  assert.match(tags[0], /automation-member-active/);
  assert.match(tags[0], /data-active="true"/);
  assert.ok(!/automation-member-active/.test(tags[1]) && /data-active="false"/.test(tags[1]));
  assert.ok(!/automation-member-active/.test(tags[2]) && /data-active="false"/.test(tags[2]));

  const running = linear();
  running.active_cursors = ['m2'];
  html = sb.buildAutomationStripHTML(running, {});
  tags = cards(html);
  assert.ok(!/automation-member-active/.test(tags[0]) && /data-active="false"/.test(tags[0]));
  assert.match(tags[1], /automation-member-active/);
  assert.match(tags[1], /data-active="true"/);
  assert.ok(!/automation-member-active/.test(tags[2]) && /data-active="false"/.test(tags[2]));
});

test('a pending member with an approved Automaton displays as approved', () => {
  const sb = loadApp();
  const m = { id: 'm1', prd_id: 'p1', status: 'pending' };
  assert.equal(sb.automationMemberDisplayStatus(m, { p1: 'approved' }), 'approved');
  assert.equal(sb.automationMemberDisplayStatus({ id: 'm1', prd_id: 'p1', status: '' }, { p1: 'approved' }), 'approved');
  assert.equal(sb.automationMemberDisplayStatus(m, { p1: 'draft' }), 'pending');
  assert.equal(sb.automationMemberDisplayStatus(m, {}), 'pending');
  assert.equal(sb.automationMemberDisplayStatus(m), 'pending');
  assert.equal(sb.automationMemberDisplayStatus({ id: 'm1', prd_id: 'p1', status: 'running' }, { p1: 'approved' }), 'running');

  const a = { id: 'a1', entry_member_id: 'm1', members: [{ id: 'm1', prd_id: 'p1', status: 'pending', added_by: 'operator' }] };
  const html = sb.buildAutomationStripHTML(a, { prdStatuses: { p1: 'approved' } });
  assert.match(html, /class="automation-member automation-member-approved/);
  assert.match(html, /data-status="approved"/);
  assert.match(html, /<span class="automation-member-status">Approved<\/span>/);
});

test('a member with on_blocked and on_failed gets the fork glyph and two labelled sub-rows', () => {
  const sb = loadApp();
  const a = {
    id: 'f1', status: 'active', entry_member_id: 'm1',
    members: [
      member('m1', 'running', { on_complete: 'm2', on_blocked: 'm3', on_failed: 'm4' }),
      member('m2', 'pending'),
      member('m3', 'pending', { on_complete: 'm5' }),
      member('m4', 'pending'),
      member('m5', 'pending'),
    ],
  };
  const html = sb.buildAutomationStripHTML(a, {});
  assert.equal((html.match(/class="automation-fork-glyph"/g) || []).length, 1);
  const main = row(html, 'main');
  assert.ok(main.includes('⑂'));
  assert.deepEqual(cards(main).map(c => c.match(/data-member-id="([^"]*)"/)[1]), ['m1', 'm2']);

  const blocked = row(html, 'on_blocked:m1');
  assert.match(blocked, />If blocked →</);
  assert.deepEqual(cards(blocked).map(c => c.match(/data-member-id="([^"]*)"/)[1]), ['m3', 'm5']);
  assert.match(blocked, /data-edge="on_blocked" data-from="m1" data-to="m3"/);
  assert.match(blocked, /data-edge="on_complete" data-from="m3" data-to="m5"/);

  const failed = row(html, 'on_failed:m1');
  assert.match(failed, />If failed →</);
  assert.deepEqual(cards(failed).map(c => c.match(/data-member-id="([^"]*)"/)[1]), ['m4']);
  assert.equal(html.match(/data-row="/g).length, 3);
  assert.ok(!html.includes('data-row="unlinked"'));
});

test('a branch target already drawn in an earlier row is drawn as a short reference', () => {
  const sb = loadApp();
  const a = {
    id: 'f2', status: 'active', entry_member_id: 'm1',
    members: [
      member('m1', 'running', { on_complete: 'm2', on_failed: 'm2' }),
      member('m2', 'pending'),
    ],
  };
  const html = sb.buildAutomationStripHTML(a, { prdTitles: { m2aaaaaaaaaaaa: 'Shared step' } });
  assert.equal(cards(html).length, 2, 'm2 is drawn in full once');
  const failed = row(html, 'on_failed:m1');
  assert.equal(cards(failed).length, 0);
  assert.match(failed, /<div class="automation-member-ref" data-ref-to="m2">/);
  assert.match(failed, />Shared step</);
});

test('an edge_taken of on_blocked marks the on_complete connector untaken and the on_blocked one taken', () => {
  const sb = loadApp();
  const a = {
    id: 'f3', status: 'active', entry_member_id: 'm1',
    members: [
      member('m1', 'blocked', { on_complete: 'm2', on_blocked: 'm3', edge_taken: 'on_blocked' }),
      member('m2', 'pending'),
      member('m3', 'running'),
    ],
  };
  const html = sb.buildAutomationStripHTML(a, {});
  const [complete] = connectors(html, 'on_complete');
  const [blocked] = connectors(html, 'on_blocked');
  assert.match(complete, /data-taken="false"/);
  assert.match(complete, /automation-connector-untaken/);
  assert.match(blocked, /data-taken="true"/);
  assert.ok(!/automation-connector-untaken/.test(blocked));
});

test('a finished member that took another edge leaves the unused edge untaken, a running one leaves it unknown', () => {
  const sb = loadApp();
  const a = {
    id: 'f4', status: 'active', entry_member_id: 'm1',
    members: [
      member('m1', 'completed', { on_complete: 'm2', on_failed: 'm3', edge_taken: 'on_complete' }),
      member('m2', 'running', { on_complete: 'm4' }),
      member('m3', 'pending'),
      member('m4', 'pending'),
    ],
  };
  const html = sb.buildAutomationStripHTML(a, {});
  assert.match(connectors(html, 'on_failed')[0], /data-taken="false"/);
  assert.match(connectors(html).find(c => c.includes('data-from="m2"')), /data-taken="unknown"/);
});

test('an inserted member shows the marker and a tooltip with who, why and when', () => {
  const sb = loadApp();
  const a = {
    id: 'i1', status: 'active', entry_member_id: 'm1',
    members: [
      member('m1', 'completed', { on_complete: 'm2' }),
      member('m2', 'pending', {
        added_by: 'agent:x1', added_reason: 'blocking <bug>', added_at: '2026-10-22T10:00:00Z',
      }),
    ],
  };
  const html = sb.buildAutomationStripHTML(a, {});
  const [first, second] = cards(html);
  assert.match(first, /data-inserted="false"/);
  assert.match(second, /data-inserted="true"/);
  assert.equal((html.match(/class="automation-inserted-marker"/g) || []).length, 1);
  assert.match(html, /class="automation-inserted-marker" title="Inserted by agent:x1 · blocking &lt;bug&gt; · 2026-10-22T10:00:00Z">inserted</);
});

test('a reason alone marks a member inserted, and an import author does not', () => {
  const sb = loadApp();
  const a = {
    id: 'i2', entry_member_id: 'm1',
    members: [
      member('m1', 'pending', { on_complete: 'm2', added_by: 'import:legacy:g1' }),
      member('m2', 'pending', { added_reason: 'needed first' }),
    ],
  };
  const [first, second] = cards(sb.buildAutomationStripHTML(a, {}));
  assert.match(first, /data-inserted="false"/);
  assert.match(second, /data-inserted="true"/);
});

test('completed, failed and skipped members are read-only, the others are not', () => {
  const sb = loadApp();
  const a = {
    id: 'r1', status: 'active', entry_member_id: 'm1',
    members: [
      member('m1', 'completed', { on_complete: 'm2' }),
      member('m2', 'failed', { on_complete: 'm3' }),
      member('m3', 'skipped', { on_complete: 'm4' }),
      member('m4', 'running'),
    ],
  };
  const tags = cards(sb.buildAutomationStripHTML(a, {}));
  assert.ok(tags.slice(0, 3).every(c => c.includes('automation-member-readonly')));
  assert.ok(!tags[3].includes('automation-member-readonly'));
});

test('an unreachable member lands in the unlinked row', () => {
  const sb = loadApp();
  const a = linear();
  a.members.push(member('m9', 'pending'));
  const html = sb.buildAutomationStripHTML(a, {});
  const unlinked = row(html, 'unlinked');
  assert.deepEqual(cards(unlinked).map(c => c.match(/data-member-id="([^"]*)"/)[1]), ['m9']);
  assert.equal(cards(row(html, 'main')).length, 3);
  assert.equal(connectors(unlinked).length, 0);
});

test('a cyclic malformed input ends without looping and draws each member once', () => {
  const sb = loadApp();
  const a = {
    id: 'c1', status: 'active', entry_member_id: 'm1',
    members: [
      member('m1', 'running', { on_complete: 'm2', on_blocked: 'm1' }),
      member('m2', 'pending', { on_complete: 'm1', on_failed: 'm2' }),
    ],
  };
  const html = sb.buildAutomationStripHTML(a, {});
  assert.deepEqual(cards(html).map(c => c.match(/data-member-id="([^"]*)"/)[1]), ['m1', 'm2']);
  assert.match(html, /data-ref-to="m1"/);
  assert.match(html, /data-ref-to="m2"/);
});

test('an empty or missing Automation renders an empty strip', () => {
  const sb = loadApp();
  assert.equal(sb.buildAutomationStripHTML({ id: 'e1', members: [] }, {}), '<div class="automation-strip" data-automation-id="e1"></div>');
  assert.match(sb.buildAutomationStripHTML(null, null), /^<div class="automation-strip"/);
});

test('a card opens its Automaton, and every dynamic value is escaped', () => {
  const sb = loadApp();
  const evil = "x'\"<b>";
  const a = {
    id: 'e"1', entry_member_id: 'm1',
    members: [{ id: 'm1', prd_id: evil, status: 'pending', added_by: 'operator' }],
  };
  const html = sb.buildAutomationStripHTML(a, { prdTitles: { [evil]: '<img src=x onerror=alert(1)>' } });
  assert.ok(!html.includes('<img'));
  assert.ok(!html.includes('<b>'));
  assert.match(html, /data-automation-id="e&quot;1"/);
  assert.match(html, /typeof openAutomationMemberPRD === 'function'\) openAutomationMemberPRD\('x\\&#39;&quot;&lt;b&gt;'\)/);
  const plainHtml = sb.buildAutomationStripHTML({ id: 'a', entry_member_id: 'm1', members: [{ id: 'm1', prd_id: 'p1', status: 'pending' }] }, {});
  assert.match(plainHtml, /openAutomationMemberPRD\('p1'\)/);
});

test('a fork card has no click-through and shows its short member id', () => {
  const sb = loadApp();
  const a = {
    id: 'k1', entry_member_id: 'f1234567890',
    members: [{ id: 'f1234567890', kind: 'fork', status: 'pending', fork_members: ['b1', 'b2'] }],
  };
  const html = sb.buildAutomationStripHTML(a, {});
  assert.match(html, /data-kind="fork"/);
  assert.match(html, /<span class="automation-chip">f1234567<\/span>/);
  assert.ok(!html.includes('openAutomationMemberPRD'));
});
