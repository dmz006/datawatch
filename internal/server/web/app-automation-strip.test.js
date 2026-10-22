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

// ── Fork, join and nested members ───────────────────────────────────────────

function ids(tags) {
  return tags.map(c => c.match(/data-member-id="([^"]*)"/)[1]);
}

// lane returns the markup of the lane whose data-lane is id.
function lane(html, id) {
  const start = html.indexOf(`<div class="automation-lane" data-lane="${id}"`);
  assert.notEqual(start, -1, 'lane ' + id + ' is present');
  const next = html.indexOf('<div class="automation-lane"', start + 1);
  return html.slice(start, next === -1 ? html.length : next);
}

function forked(rule, extraJoin) {
  return {
    id: 'fj1', status: 'active', entry_member_id: 'm1',
    members: [
      member('m1', 'completed', { on_complete: 'fk', edge_taken: 'on_complete' }),
      { id: 'fk', kind: 'fork', status: 'running', fork_members: ['b1', 'b2', 'b3'] },
      member('b1', 'running'),
      member('b2', 'completed'),
      member('b3', 'pending'),
      Object.assign({ id: 'jn', kind: 'join', status: 'pending', join_members: ['b3', 'b1', 'b2'], on_complete: 'm9' }, rule ? { on_branch_failure: rule } : {}, extraJoin || {}),
      member('m9', 'pending'),
    ],
  };
}

test('fork with three branches renders three lanes in one group in fork_members order, then the join and the rest of the chain', () => {
  const sb = loadApp();
  const html = sb.buildAutomationStripHTML(forked(), {});
  assert.equal((html.match(/class="automation-fork-group"/g) || []).length, 1);
  assert.match(html, /<div class="automation-fork-group" data-fork-id="fk" data-join-id="jn">/);
  const lanes = (html.match(/<div class="automation-lane" data-lane="([^"]*)"/g) || []).map(l => l.match(/data-lane="([^"]*)"/)[1]);
  assert.deepEqual(lanes, ['b1', 'b2', 'b3']);
  const group = html.slice(html.indexOf('automation-fork-group'), html.indexOf('data-kind="join"'));
  assert.equal((group.match(/class="automation-lane"/g) || []).length, 3);
  assert.deepEqual(ids(cards(html)), ['m1', 'fk', 'b1', 'b2', 'b3', 'jn', 'm9']);
  assert.ok(html.indexOf('data-kind="fork"') < html.indexOf('automation-fork-group'));
  assert.ok(html.indexOf('automation-fork-group') < html.indexOf('data-kind="join"'));
  assert.ok(html.indexOf('data-kind="join"') < html.indexOf('data-member-id="m9"'));
  assert.match(html, /data-edge="on_complete" data-from="jn" data-to="m9"/);
  assert.match(html, /data-kind="fork"[^>]*>(?:(?!<\/div>).)*<span class="automation-member-title">Fork<\/span>/);
});

test('branch members never appear outside their lane', () => {
  const sb = loadApp();
  const a = forked();
  a.members.push(member('u1', 'pending'));
  const html = sb.buildAutomationStripHTML(a, {});
  ['b1', 'b2', 'b3'].forEach(id => {
    assert.equal((html.match(new RegExp(`data-member-id="${id}"`, 'g')) || []).length, 1, id + ' drawn once');
    assert.ok(lane(html, id).includes(`data-member-id="${id}"`));
  });
  assert.ok(!cards(row(html, 'main')).some(c => c.includes('data-member-id="u1"')));
  const unlinked = row(html, 'unlinked');
  assert.deepEqual(ids(cards(unlinked)), ['u1']);
});

test('two running branch lanes are both highlighted and carry the branch title', () => {
  const sb = loadApp();
  const a = forked();
  a.active_cursors = ['b1', 'b3'];
  a.members.find(m => m.id === 'b3').status = 'running';
  const html = sb.buildAutomationStripHTML(a, {});
  const l1 = lane(html, 'b1');
  const l2 = lane(html, 'b2');
  const l3 = lane(html, 'b3');
  assert.match(l1, /automation-member-active/);
  assert.match(l1, /data-active="true"/);
  assert.match(l3, /automation-member-active/);
  assert.ok(!/automation-member-active/.test(l2) && /data-active="false"/.test(l2));
  assert.match(l1, /^<div class="automation-lane" data-lane="b1" title="Branch b1">/);
  assert.match(l3, /title="Branch b3"/);
  assert.equal(cards(html).filter(c => c.includes('automation-member-active')).length, 2);
});

test('each join rule produces the right data-rule and label, and an empty rule is all_must_succeed', () => {
  const sb = loadApp();
  const cases = [
    ['', 'all_must_succeed', 'All branches must succeed'],
    ['all_must_succeed', 'all_must_succeed', 'All branches must succeed'],
    ['any_one_is_enough', 'any_one_is_enough', 'One branch is enough'],
    ['continue_anyway', 'continue_anyway', 'Continue anyway'],
    ['bogus', 'all_must_succeed', 'All branches must succeed'],
  ];
  cases.forEach(([rule, want, text]) => {
    const html = sb.buildAutomationStripHTML(forked(rule), {});
    const join = html.match(/<div class="automation-member [^>]*data-kind="join"[^>]*>.*?<\/div>/)[0];
    assert.match(join, new RegExp(`data-rule="${want}"`));
    assert.match(join, /<span class="automation-member-title">Join<\/span>/);
    assert.match(join, new RegExp(`<span class="automation-join-rule">${text}</span>`));
  });
});

test('a join whose members do not match the fork is not paired and the walk ends after the group', () => {
  const sb = loadApp();
  const a = forked('', { join_members: ['b1', 'b2'] });
  const html = sb.buildAutomationStripHTML(a, {});
  assert.match(html, /data-join-id=""/);
  assert.deepEqual(ids(cards(row(html, 'unlinked'))), ['jn', 'm9']);
  assert.equal(ids(cards(row(html, 'main'))).includes('jn'), false);
});

function nestedTree() {
  const child = {
    id: 'child001', name: 'Child chain', status: 'running', entry_member_id: 'c1',
    members: [member('c1', 'running', { on_complete: 'c2' }), member('c2', 'pending')],
  };
  const parent = {
    id: 'parent01', status: 'active', entry_member_id: 'n1',
    members: [
      { id: 'n1', kind: 'automation', automation_id: 'child001', status: 'running', on_complete: 'm2' },
      member('m2', 'pending'),
    ],
  };
  return { child, parent, byId: { child001: child, parent01: parent } };
}

test('a nested member renders collapsed by default with the child name, status and expand control', () => {
  const sb = loadApp();
  const { parent, byId } = nestedTree();
  const html = sb.buildAutomationStripHTML(parent, { automationsById: byId, expanded: new Set() });
  assert.match(html, /data-kind="automation"[^>]*data-expanded="false"/);
  assert.match(html, /<span class="automation-member-title">Child chain<\/span>/);
  assert.match(html, /<span class="automation-member-status">Running<\/span>/);
  assert.match(html, /<button type="button" class="automation-nested-toggle" data-member-id="n1" aria-expanded="false" onclick="toggleAutomationNested\('n1'\)">Show nested Automation<\/button>/);
  assert.ok(!html.includes('class="automation-nested"'));
  assert.ok(!html.includes('data-member-id="c1"'));
  assert.deepEqual(ids(cards(html)), ['n1', 'm2']);
});

test('a nested member falls back to the first eight characters of the child id when the child is not loaded', () => {
  const sb = loadApp();
  const { parent } = nestedTree();
  const html = sb.buildAutomationStripHTML(parent, { automationsById: {}, expanded: new Set() });
  assert.match(html, /<span class="automation-member-title">child001<\/span>/);
});

test('an expanded nested member renders the child members inside an automation-nested element', () => {
  const sb = loadApp();
  const { parent, byId } = nestedTree();
  const html = sb.buildAutomationStripHTML(parent, { automationsById: byId, expanded: new Set(['n1']) });
  assert.match(html, /data-kind="automation"[^>]*data-expanded="true"/);
  assert.match(html, /aria-expanded="true"[^>]*>Hide nested Automation<\/button>/);
  const start = html.indexOf('<div class="automation-nested" data-automation-id="child001" data-expanded="true">');
  assert.notEqual(start, -1);
  const nested = html.slice(start);
  assert.match(nested, /<div class="automation-strip" data-automation-id="child001">/);
  assert.ok(nested.includes('data-member-id="c1"') && nested.includes('data-member-id="c2"'));
  assert.deepEqual(ids(cards(html)), ['n1', 'c1', 'c2', 'm2']);
  assert.ok(html.indexOf('data-member-id="c2"') < html.indexOf('data-member-id="m2"'));
});

test('a two-deep nesting renders and a fork inside a child draws its lanes', () => {
  const sb = loadApp();
  const { parent, child, byId } = nestedTree();
  const grand = {
    id: 'grand001', name: 'Grand', status: 'active', entry_member_id: 'g1',
    members: [
      { id: 'g1', kind: 'fork', status: 'pending', fork_members: ['gb1', 'gb2'] },
      member('gb1', 'pending'), member('gb2', 'pending'),
      { id: 'gj', kind: 'join', status: 'pending', join_members: ['gb1', 'gb2'] },
    ],
  };
  child.members[1] = { id: 'c2', kind: 'automation', automation_id: 'grand001', status: 'pending' };
  byId.grand001 = grand;
  const html = sb.buildAutomationStripHTML(parent, { automationsById: byId, expanded: new Set(['n1', 'c2']) });
  assert.equal((html.match(/class="automation-nested"/g) || []).length, 2);
  assert.match(html, /data-automation-id="grand001" data-expanded="true"/);
  assert.match(html, /<span class="automation-member-title">Grand<\/span>/);
  assert.equal((html.match(/class="automation-lane"/g) || []).length, 2);
  assert.ok(html.includes('data-member-id="gb2"'));
});

test('a self-referencing nested Automation ends without looping', () => {
  const sb = loadApp();
  const a = {
    id: 'self0001', name: 'Self', status: 'active', entry_member_id: 'n1',
    members: [{ id: 'n1', kind: 'automation', automation_id: 'self0001', status: 'running' }],
  };
  const html = sb.buildAutomationStripHTML(a, { automationsById: { self0001: a }, expanded: new Set(['n1']) });
  assert.equal(cards(html).length, 1);
  assert.equal((html.match(/class="automation-nested"/g) || []).length, 1);

  const x = { id: 'xx', entry_member_id: 'nx', members: [{ id: 'nx', kind: 'automation', automation_id: 'yy', status: 'running' }] };
  const y = { id: 'yy', entry_member_id: 'ny', members: [{ id: 'ny', kind: 'automation', automation_id: 'xx', status: 'running' }] };
  const mutual = sb.buildAutomationStripHTML(x, { automationsById: { xx: x, yy: y }, expanded: new Set(['nx', 'ny']) });
  assert.deepEqual(ids(cards(mutual)), ['nx', 'ny']);
});

test('nesting stops at the maximum depth', () => {
  const sb = loadApp();
  const byId = {};
  for (let i = 0; i < 9; i++) {
    byId['lvl' + i] = {
      id: 'lvl' + i, entry_member_id: 'n' + i,
      members: [{ id: 'n' + i, kind: 'automation', automation_id: 'lvl' + (i + 1), status: 'running' }],
    };
  }
  byId.lvl9 = { id: 'lvl9', entry_member_id: 'leaf', members: [member('leaf', 'pending')] };
  const expanded = new Set(Object.keys(byId).map((k, i) => 'n' + i));
  const html = sb.buildAutomationStripHTML(byId.lvl0, { automationsById: byId, expanded });
  assert.equal((html.match(/class="automation-nested"/g) || []).length, 6);
  assert.ok(!html.includes('data-member-id="leaf"'));
});

test('a missing child shows the not-loaded text when expanded', () => {
  const sb = loadApp();
  const { parent } = nestedTree();
  const html = sb.buildAutomationStripHTML(parent, { automationsById: {}, expanded: new Set(['n1']) });
  assert.match(html, /<div class="automation-nested" data-automation-id="child001" data-expanded="true"><span class="automation-nested-missing">Nested Automation not loaded<\/span><\/div>/);
});

test('toggleAutomationNested flips the id in the expanded set and redraws only when the detail task defined the renderer', () => {
  const sb = loadApp();
  assert.equal(typeof sb.toggleAutomationNested, 'function');
  sb.toggleAutomationNested('n1');
  const html = (set) => sb.buildAutomationStripHTML(nestedTree().parent, { automationsById: nestedTree().byId, expanded: set });
  assert.ok(html(vm.runInContext('_automationState.expanded', sb)).includes('class="automation-nested"'));
  sb.toggleAutomationNested('n1');
  assert.equal(vm.runInContext('_automationState.expanded.has("n1")', sb), false);

  let calls = 0;
  sb.renderAutomationStrip = () => { calls++; };
  sb.toggleAutomationNested('n1');
  assert.equal(calls, 0, 'no detail view is open');
  vm.runInContext('_automationState.detailId = "parent01"', sb);
  sb.toggleAutomationNested('n1');
  assert.equal(calls, 1);
});
