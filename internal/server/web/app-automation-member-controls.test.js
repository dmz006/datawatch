// Automation tab — per-member control operations on the detail page.
//
// Covers the skip, insert, remove and edge-editing dialogs, the held-insert
// approve button, which members show action buttons, the insert picker marking
// unavailable Automata with a reason, and the stale prompt on a 409. fetch is
// stubbed and records every request; rendered text is the real English bundle.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

// loadApp returns a sandbox with the app loaded and a recording fetch. routes
// maps "METHOD /path" to {status, body}; an unknown request answers 404. Every
// request is recorded in __calls with its parsed JSON body and every element
// the app creates (the modals) in __modals.
function loadApp(routes, byId) {
  const sandbox = buildSandbox({ byId });
  const calls = [];
  const modals = [];
  sandbox.fetch = (url, opts) => {
    const method = (opts && opts.method) || 'GET';
    calls.push({ url, method, body: opts && opts.body ? JSON.parse(opts.body) : undefined });
    const hit = routes[method + ' ' + url];
    if (!hit) return Promise.resolve({ ok: false, status: 404, statusText: 'Not Found', text: () => Promise.resolve('not found') });
    const status = hit.status || 200;
    return Promise.resolve({
      ok: status < 400,
      status,
      statusText: String(status),
      json: () => Promise.resolve(hit.body),
      text: () => Promise.resolve(typeof hit.body === 'string' ? hit.body : JSON.stringify(hit.body)),
    });
  };
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  sandbox.document.createElement = () => {
    const el = makeStubElement();
    el.removed = false;
    el.remove = () => { el.removed = true; };
    modals.push(el);
    return el;
  };
  sandbox.__calls = calls;
  sandbox.__modals = modals;
  sandbox.__toasts = [];
  sandbox.showToast = (message, type) => { sandbox.__toasts.push({ message, type }); };
  return sandbox;
}

const stateOf = sb => vm.runInContext('_automationState', sb);

function member(id, status, extra) {
  return Object.assign({ id, prd_id: id + 'aaaaaaaaaaaa', status, added_by: 'operator' }, extra || {});
}

// A chain m1 (done) -> m2 (running) -> m3 (pending) -> m4 (pending), version 7.
function automation(extra) {
  return Object.assign({
    id: 'a1b2c3d4', name: 'Release train', description: '', status: 'active', version: 7,
    entry_member_id: 'm1', active_cursors: ['m2'],
    members: [
      member('m1', 'completed', { on_complete: 'm2', edge_taken: 'on_complete' }),
      member('m2', 'running', { on_complete: 'm3' }),
      member('m3', 'pending', { on_complete: 'm4', on_failed: 'm4' }),
      member('m4', 'pending'),
    ],
  }, extra || {});
}

const PRDS = {
  prds: [
    { id: 'plainaaaaaaaa', title: 'Plain work', status: 'approved' },
    { id: 'tmplaaaaaaaa', title: 'Nightly template', status: 'draft', is_template: true },
    { id: 'childaaaaaaaa', title: 'Child work', status: 'approved', parent_prd_id: 'plainaaaaaaaa' },
    { id: 'ownedaaaaaaaa', title: 'Owned work', status: 'approved' },
    { id: 'm3aaaaaaaaaaaa', title: 'Deploy', status: 'approved' },
  ],
};

const OTHER_AUTOMATION = {
  id: 'zz990011', name: 'Other train', status: 'active', version: 2,
  members: [member('o1', 'pending', { prd_id: 'ownedaaaaaaaa' })],
};

// openDetail loads one Automation through the page and resolves the env. The
// form fields the dialogs read are plain objects registered by id.
async function openDetail(a, extraRoutes) {
  const ids = {
    view: makeStubElement(),
    automationControls: makeStubElement(),
    automationStrip: makeStubElement(),
    genericModalConfirmBtn: makeStubElement(),
    automationSkipEdge: { value: 'on_complete' },
    automationSkipReason: { value: '' },
    automationInsertSearch: { value: '' },
    automationInsertPicker: makeStubElement(),
    automationInsertUrgent: { checked: false },
    automationInsertReason: { value: '' },
    automationInsertError: makeStubElement(),
    automationReorderComplete: { value: '' },
    automationReorderBlocked: { value: '' },
    automationReorderFailed: { value: '' },
  };
  const routes = Object.assign({
    ['GET /api/automations/' + encodeURIComponent(a.id)]: { body: a },
    'GET /api/autonomous/prds': { body: PRDS },
    'GET /api/automations': { body: [a, OTHER_AUTOMATION] },
  }, extraRoutes || {});
  const sb = loadApp(routes, ids);
  sb.openAutomationDetail(a.id);
  await flushAsync();
  return { sb, ids };
}

const posts = sb => sb.__calls.filter(c => c.method === 'POST');
const lastModal = sb => sb.__modals.filter(m => m.innerHTML).pop();
const confirm = async env => { env.ids.genericModalConfirmBtn.onclick(); await flushAsync(); };
const CONTROL = op => 'POST /api/automations/a1b2c3d4/' + op;

// actionsOf returns the member-op names on one member's card in a strip.
function actionsOf(html, memberId) {
  const start = html.indexOf(`data-member-id="${memberId}"`);
  assert.ok(start >= 0, 'card ' + memberId + ' is drawn');
  const next = html.indexOf('<div class="automation-member ', start);
  const card = html.slice(start, next < 0 ? html.length : next);
  return (card.match(/data-member-op="([^"]*)"/g) || []).map(s => s.match(/data-member-op="([^"]*)"/)[1]);
}

test('skip sends member_id, the chosen edge and the reason', async () => {
  const env = await openDetail(automation(), { [CONTROL('skip_member')]: { body: automation({ version: 8 }) } });
  const { sb, ids } = env;
  sb.openAutomationSkipDialog('m3');
  const html = lastModal(sb).innerHTML;
  assert.match(html, /Skip this member/);
  assert.match(html, /Acting on branch: Deploy \(m3\)/);
  assert.match(html, />On complete<\/option>/);
  assert.match(html, />On blocked<\/option>/);
  assert.match(html, />On failed<\/option>/);
  assert.match(html, /Take this outcome/);
  assert.equal(posts(sb).length, 0, 'opening the dialog sends nothing');
  ids.automationSkipEdge.value = 'on_failed';
  ids.automationSkipReason.value = ' flaky check ';
  await confirm(env);
  const sent = posts(sb);
  assert.equal(sent.length, 1);
  assert.equal(sent[0].url, '/api/automations/a1b2c3d4/skip_member');
  assert.equal(sent[0].body.member_id, 'm3');
  assert.equal(sent[0].body.edge, 'on_failed');
  assert.equal(sent[0].body.reason, 'flaky check');
  assert.equal(sent[0].body.expected_version, 7);
  assert.equal(stateOf(sb).detail.version, 8);
});

test('insert with an empty reason sends nothing, shows the required text and stays open', async () => {
  const env = await openDetail(automation());
  const { sb, ids } = env;
  await sb.openAutomationInsertDialog('m3', 'after');
  const modal = lastModal(sb);
  sb.automationPickInsertAutomaton('plainaaaaaaaa');
  ids.automationInsertReason.value = '   ';
  await confirm(env);
  assert.equal(posts(sb).length, 0);
  assert.match(ids.automationInsertError.innerHTML, /A reason is required to insert a member/);
  assert.equal(modal.removed, false, 'the dialog stays open');
  // No reason and no pick is still the reason message.
  const again = await openDetail(automation());
  await again.sb.openAutomationInsertDialog('m3', 'after');
  await confirm(again);
  assert.equal(posts(again.sb).length, 0);
  assert.match(again.ids.automationInsertError.innerHTML, /A reason is required to insert a member/);
});

test('insert with a reason sends member.prd_id, after, urgent and reason', async () => {
  const env = await openDetail(automation(), { [CONTROL('insert_member')]: { body: automation({ version: 8 }) } });
  const { sb, ids } = env;
  await sb.openAutomationInsertDialog('m3', 'after');
  const html = lastModal(sb).innerHTML;
  assert.match(html, /Insert after — Acting on branch: Deploy \(m3\)/);
  assert.match(html, /Search Automata by name or ID/);
  assert.match(html, /Insert urgently/);
  assert.match(html, /Reason for the insert/);
  sb.automationPickInsertAutomaton('plainaaaaaaaa');
  ids.automationInsertReason.value = 'Needed before deploy';
  await confirm(env);
  const sent = posts(sb);
  assert.equal(sent.length, 1);
  assert.equal(sent[0].url, '/api/automations/a1b2c3d4/insert_member');
  assert.deepEqual(sent[0].body.member, { prd_id: 'plainaaaaaaaa' });
  assert.equal(sent[0].body.after, 'm3');
  assert.equal(sent[0].body.urgent, false);
  assert.equal(sent[0].body.reason, 'Needed before deploy');
  assert.equal(sent[0].body.expected_version, 7);
});

test('an urgent insert sends urgent true with an empty after', async () => {
  const env = await openDetail(automation(), { [CONTROL('insert_member')]: { body: automation({ version: 8 }) } });
  await env.sb.openAutomationInsertDialog('m3', 'after');
  env.sb.automationPickInsertAutomaton('plainaaaaaaaa');
  env.ids.automationInsertUrgent.checked = true;
  env.ids.automationInsertReason.value = 'Blocker';
  await confirm(env);
  const body = posts(env.sb)[0].body;
  assert.equal(body.urgent, true);
  assert.equal(body.after, '');
});

test('insert before passes the predecessor of the chosen member as after', async () => {
  const env = await openDetail(automation(), { [CONTROL('insert_member')]: { body: automation({ version: 8 }) } });
  await env.sb.openAutomationInsertDialog('m4', 'before');
  assert.match(lastModal(env.sb).innerHTML, /Insert before — Acting on branch: .*\(m4\)/);
  env.sb.automationPickInsertAutomaton('plainaaaaaaaa');
  env.ids.automationInsertReason.value = 'Fix-up first';
  await confirm(env);
  assert.equal(posts(env.sb)[0].body.after, 'm3');
  assert.equal(posts(env.sb)[0].body.urgent, false);
});

test('insert before the entry member of a draft is an urgent insert; on an active Automation there is no slot', async () => {
  const draft = automation({
    status: 'draft', active_cursors: [], entry_member_id: 'm3',
    members: [member('m3', 'pending', { on_complete: 'm4' }), member('m4', 'pending')],
  });
  const env = await openDetail(draft, { [CONTROL('insert_member')]: { body: draft } });
  await env.sb.openAutomationInsertDialog('m3', 'before');
  env.sb.automationPickInsertAutomaton('plainaaaaaaaa');
  await confirm(env); // empty reason is refused first
  env.ids.automationInsertReason.value = 'New first step';
  await confirm(env);
  assert.equal(posts(env.sb)[0].body.urgent, true);
  assert.equal(posts(env.sb)[0].body.after, '');

  const live = await openDetail(automation({
    entry_member_id: 'm3', members: [member('m3', 'pending', { on_complete: 'm4' }), member('m4', 'pending')],
  }));
  const before = live.sb.__modals.length;
  await live.sb.openAutomationInsertDialog('m3', 'before');
  assert.equal(live.sb.__modals.length, before, 'no dialog for a slot that does not exist');
});

test('the picker marks a template and an Automaton owned by another Automation as unavailable with a reason', async () => {
  const env = await openDetail(automation());
  await env.sb.openAutomationInsertDialog('m3', 'after');
  const html = lastModal(env.sb).innerHTML;
  const item = prdId => {
    const m = html.match(new RegExp(`<label class="automation-picker-item" data-prd-id="${prdId}" data-unavailable="(true|false)"[\\s\\S]*?</label>`));
    assert.ok(m, prdId + ' is listed');
    return m[0];
  };
  const tmpl = item('tmplaaaaaaaa');
  assert.match(tmpl, /data-unavailable="true"/);
  assert.match(tmpl, /disabled/);
  assert.match(tmpl, /Not available: it is a template/);
  const owned = item('ownedaaaaaaaa');
  assert.match(owned, /data-unavailable="true"/);
  assert.match(owned, /Not available: it already belongs to Automation Other train/);
  assert.match(item('childaaaaaaaa'), /Not available: it is a child Automaton/);
  assert.match(item('m3aaaaaaaaaaaa'), /Not available: it is already in this Automation/);
  const plain = item('plainaaaaaaaa');
  assert.match(plain, /data-unavailable="false"/);
  assert.ok(!/disabled/.test(plain));
  assert.ok(!/PRD/.test(html), 'no internal vocabulary in the dialog');
  // An unavailable row cannot be picked.
  env.sb.automationPickInsertAutomaton('tmplaaaaaaaa');
  env.ids.automationInsertReason.value = 'x';
  await confirm(env);
  assert.equal(posts(env.sb).length, 0);
  assert.match(env.ids.automationInsertError.innerHTML, /Choose an Automaton to insert/);
});

test('the picker search filters by name or id', async () => {
  const env = await openDetail(automation());
  await env.sb.openAutomationInsertDialog('m3', 'after');
  env.ids.automationInsertSearch.value = 'nightly';
  env.sb.automationFilterInsertPicker();
  assert.match(env.ids.automationInsertPicker.innerHTML, /Nightly template/);
  assert.ok(!env.ids.automationInsertPicker.innerHTML.includes('Plain work'));
  env.ids.automationInsertSearch.value = 'plainaaa';
  env.sb.automationFilterInsertPicker();
  assert.match(env.ids.automationInsertPicker.innerHTML, /Plain work/);
  env.ids.automationInsertSearch.value = 'zzz-none';
  env.sb.automationFilterInsertPicker();
  assert.match(env.ids.automationInsertPicker.innerHTML, /No Automata match/);
});

test('remove sends member_id only after the confirmation', async () => {
  const env = await openDetail(automation(), { [CONTROL('remove_member')]: { body: automation({ version: 8 }) } });
  env.sb.automationRemoveMember('m4');
  assert.equal(posts(env.sb).length, 0, 'nothing is sent before the confirmation');
  const html = lastModal(env.sb).innerHTML;
  assert.match(html, /Remove this member from the Automation\?/);
  assert.match(html, /\(m4\)/);
  await confirm(env);
  const sent = posts(env.sb);
  assert.equal(sent.length, 1);
  assert.equal(sent[0].url, '/api/automations/a1b2c3d4/remove_member');
  assert.equal(sent[0].body.member_id, 'm4');
});

test('reorder offers every other member plus stop and sends the three edges, empty string for stop', async () => {
  const env = await openDetail(automation(), { [CONTROL('reorder_edges')]: { body: automation({ version: 8 }) } });
  env.sb.openAutomationReorderDialog('m3');
  const html = lastModal(env.sb).innerHTML;
  assert.match(html, /Edit outgoing edges/);
  assert.equal((html.match(/<select /g) || []).length, 3);
  assert.equal((html.match(/>Stop and alert<\/option>/g) || []).length, 3);
  ['m1', 'm2', 'm4'].forEach(id => assert.equal((html.match(new RegExp(`<option value="${id}"`, 'g')) || []).length, 3, id));
  assert.ok(!html.includes('<option value="m3"'), 'a member does not point at itself');
  assert.match(html, /<option value="m4" selected>/, 'current edges are preselected');
  env.ids.automationReorderComplete.value = 'm1';
  env.ids.automationReorderBlocked.value = '';
  env.ids.automationReorderFailed.value = 'm2';
  await confirm(env);
  const sent = posts(env.sb);
  assert.equal(sent.length, 1);
  assert.equal(sent[0].url, '/api/automations/a1b2c3d4/reorder_edges');
  assert.deepEqual(
    { member_id: sent[0].body.member_id, on_complete: sent[0].body.on_complete, on_blocked: sent[0].body.on_blocked, on_failed: sent[0].body.on_failed },
    { member_id: 'm3', on_complete: 'm1', on_blocked: '', on_failed: 'm2' });
});

test('a held insert renders an Approve button that sends approve_insert with the member id', async () => {
  const a = automation({
    pending_inserts: [{ id: 'p9', prd_id: 'plainaaaaaaaa', name: 'Plain work', actor: 'agent:x', reason: 'needed', status: 'pending' }],
  });
  const env = await openDetail(a, { [CONTROL('approve_insert')]: { body: automation({ version: 8 }) } });
  const view = env.ids.view.innerHTML;
  assert.match(view, /<button type="button" class="btn-secondary automation-approve-insert" data-insert-id="p9" onclick="automationApproveInsert\('p9'\)">Approve insert<\/button>/);
  await env.sb.automationApproveInsert('p9');
  const sent = posts(env.sb);
  assert.equal(sent.length, 1);
  assert.equal(sent[0].url, '/api/automations/a1b2c3d4/approve_insert');
  assert.equal(sent[0].body.member_id, 'p9');
  assert.equal(sent[0].body.expected_version, 7);
});

test('only pending members offer every action; a running member offers edges and skip; finished members none', () => {
  const sb = loadApp({}, {});
  const a = automation({
    members: [
      member('m1', 'completed', { on_complete: 'm2' }),
      member('m2', 'running', { on_complete: 'm3' }),
      member('m3', 'pending', { on_complete: 'm4' }),
      member('m4', 'pending', { on_complete: 'm5' }),
      member('m5', 'failed', { on_complete: 'm6' }),
      member('m6', 'skipped'),
    ],
  });
  const html = sb.buildAutomationStripHTML(a, {});
  assert.deepEqual(actionsOf(html, 'm1'), [], 'completed member is read-only');
  assert.deepEqual(actionsOf(html, 'm5'), [], 'failed member is read-only');
  assert.deepEqual(actionsOf(html, 'm6'), [], 'skipped member is read-only');
  assert.deepEqual(actionsOf(html, 'm2'), ['reorder_edges', 'skip_member']);
  const all = ['insert_before', 'insert_after', 'reorder_edges', 'skip_member', 'remove_member'];
  assert.deepEqual(actionsOf(html, 'm3'), all);
  assert.deepEqual(actionsOf(html, 'm4'), all);
  assert.match(html, /aria-label="Member actions"/);
});

test('a completed member card has no action buttons at all', () => {
  const sb = loadApp({}, {});
  const html = sb.buildAutomationStripHTML(automation(), {});
  const card = html.slice(html.indexOf('data-member-id="m1"'), html.indexOf('data-member-id="m2"'));
  assert.ok(!card.includes('automation-member-action-btn'));
  assert.ok(!card.includes('automation-member-actions'));
});

test('no member actions on a finished Automation, a read-only strip, or a nested child strip', () => {
  const sb = loadApp({}, {});
  assert.ok(!sb.buildAutomationStripHTML(automation({ status: 'completed' }), {}).includes('automation-member-action-btn'));
  assert.ok(!sb.buildAutomationStripHTML(automation({ status: 'cancelled' }), {}).includes('automation-member-action-btn'));
  assert.ok(!sb.buildAutomationStripHTML(automation(), { readOnly: true }).includes('automation-member-action-btn'));
  const child = automation({ id: 'child001', members: [member('c1', 'pending')], entry_member_id: 'c1', active_cursors: [] });
  const parent = automation({
    status: 'completed',
    members: [{ id: 'n1', kind: 'automation', automation_id: 'child001', status: 'running' }], entry_member_id: 'n1',
  });
  const html = sb.buildAutomationStripHTML(parent, { expanded: new Set(['n1']), automationsById: { child001: child } });
  assert.match(html, /data-member-id="c1"/);
  assert.ok(!html.includes('automation-member-action-btn'), 'child members are edited from their own page');
});

test('the card buttons keep the existing attributes and name the member they act on', () => {
  const sb = loadApp({}, {});
  const html = sb.buildAutomationStripHTML(automation(), {});
  assert.match(html, /<div class="automation-member automation-member-pending[^"]*" data-member-id="m4" data-kind="prd" data-status="pending" data-active="false" data-inserted="false"/);
  assert.match(html, /data-member-op="skip_member" data-action-for="m4" onclick="event\.stopPropagation\(\); if \(typeof openAutomationSkipDialog === 'function'\) openAutomationSkipDialog\('m4'\)">Skip member<\/button>/);
  assert.match(html, /openAutomationInsertDialog\('m4','before'\)">Insert before<\/button>/);
  assert.match(html, /openAutomationInsertDialog\('m4','after'\)">Insert after<\/button>/);
  assert.match(html, /openAutomationReorderDialog\('m4'\)">Edit edges<\/button>/);
  assert.match(html, /automationRemoveMember\('m4'\)">Remove<\/button>/);
});

test('a 409 on skip, insert, remove, reorder and approve shows the stale prompt and does not retry', async () => {
  const stale = { status: 409, body: 'version 7 is stale' };
  const cases = [
    ['skip_member', env => { env.sb.openAutomationSkipDialog('m3'); return confirm(env); }],
    ['insert_member', async env => {
      await env.sb.openAutomationInsertDialog('m3', 'after');
      env.sb.automationPickInsertAutomaton('plainaaaaaaaa');
      env.ids.automationInsertReason.value = 'why';
      return confirm(env);
    }],
    ['remove_member', env => { env.sb.automationRemoveMember('m4'); return confirm(env); }],
    ['reorder_edges', env => { env.sb.openAutomationReorderDialog('m3'); return confirm(env); }],
    ['approve_insert', env => env.sb.automationApproveInsert('p9')],
  ];
  for (const [op, run] of cases) {
    const env = await openDetail(automation(), { [CONTROL(op)]: stale });
    await run(env);
    await flushAsync();
    assert.equal(posts(env.sb).length, 1, op + ': one request, no retry');
    const html = lastModal(env.sb).innerHTML;
    assert.match(html, /Out of date/, op);
    assert.match(html, /version 7 is stale/, op);
    assert.match(html, />Reload<\/button>/, op);
    assert.equal(env.sb.__toasts.length, 0, op + ': no toast for a 409');
  }
});
