// Automation tab — create and edit form.
//
// Covers the default linear wiring, the Automaton picker's unavailable rows,
// the request body the form builds, saving a new Automation (POST) and an edit
// (PUT with the loaded version), the stale prompt on a 409, the client checks
// that send nothing, the edge selects and the escaping of hostile titles.
// fetch is stubbed and records every request; rendered text is the real
// English bundle.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const HEX8 = /^[0-9a-f]{8}$/;

const HOSTILE = '<img src=x onerror=alert(1)>';

const PRDS = {
  prds: [
    { id: 'aaaaaaaa1111', title: 'Build the base', status: 'approved' },
    { id: 'bbbbbbbb2222', title: 'Run the checks', status: 'approved' },
    { id: 'cccccccc3333', title: 'Ship it', status: 'draft' },
    { id: 'dddddddd4444', title: 'Nightly template', status: 'draft', is_template: true },
    { id: 'eeeeeeee5555', title: 'Child work', status: 'approved', parent_prd_id: 'aaaaaaaa1111' },
    { id: 'ffffffff6666', title: 'Owned work', status: 'approved' },
    { id: 'abababab7777', title: HOSTILE, status: 'approved' },
  ],
};

const OTHER_AUTOMATION = {
  id: 'oth00001', name: 'Other chain', status: 'active', version: 2, entry_member_id: 'o1',
  members: [{ id: 'o1', prd_id: 'ffffffff6666', status: 'running' }],
};

// loadApp returns a sandbox with the app loaded, a recording fetch and the
// form's elements registered. routes maps "METHOD /path" to {status, body}; an
// unknown request answers 404. fields sets the text fields' values.
function loadApp(routes, fields) {
  const els = {
    view: makeStubElement(),
    name: { value: '' },
    description: { value: '' },
    reason: { value: '' },
    error: { innerHTML: '' },
    members: { innerHTML: '' },
    picker: { innerHTML: '' },
    save: { disabled: false },
  };
  Object.assign(els.name, { value: (fields && fields.name) || '' });
  Object.assign(els.description, { value: (fields && fields.description) || '' });
  Object.assign(els.reason, { value: (fields && fields.reason) || '' });
  const byId = {
    view: els.view,
    automationFormName: els.name,
    automationFormDescription: els.description,
    automationFormReason: els.reason,
    automationFormError: els.error,
    automationFormMembers: els.members,
    automationFormPicker: els.picker,
    automationFormSaveBtn: els.save,
  };
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
    modals.push(el);
    return el;
  };
  sandbox.__calls = calls;
  sandbox.__modals = modals;
  sandbox.__els = els;
  return sandbox;
}

const formState = sb => vm.runInContext('_automationForm', sb);
// plain copies a value out of the sandbox realm so deepEqual compares it.
const plain = value => JSON.parse(JSON.stringify(value));
const writes = sb => sb.__calls.filter(c => c.method !== 'GET');

const BASE_ROUTES = { 'GET /api/autonomous/prds': { body: PRDS }, 'GET /api/automations': { body: [OTHER_AUTOMATION] } };

// openNew opens the form for a new Automation and waits for the picker data.
async function openNew(extraRoutes, fields) {
  const sb = loadApp(Object.assign({}, BASE_ROUTES, extraRoutes), fields);
  sb.openAutomationForm(null);
  await flushAsync();
  return sb;
}

function pickAndAdd(sb, prdId) {
  sb.automationFormPick(prdId);
  sb.automationFormAdd();
}

// ── automationLinearWiring ───────────────────────────────────────────────────

test('linear wiring of one id has an empty on_complete', () => {
  const sb = loadApp({});
  assert.deepEqual(JSON.parse(JSON.stringify(sb.automationLinearWiring(['m1']))), [{ id: 'm1', on_complete: '' }]);
});

test('linear wiring of two ids points the first at the second', () => {
  const sb = loadApp({});
  assert.deepEqual(JSON.parse(JSON.stringify(sb.automationLinearWiring(['m1', 'm2']))), [
    { id: 'm1', on_complete: 'm2' },
    { id: 'm2', on_complete: '' },
  ]);
});

test('linear wiring of three ids chains them and the last one stops', () => {
  const sb = loadApp({});
  const wired = JSON.parse(JSON.stringify(sb.automationLinearWiring(['m1', 'm2', 'm3'])));
  assert.deepEqual(wired, [
    { id: 'm1', on_complete: 'm2' },
    { id: 'm2', on_complete: 'm3' },
    { id: 'm3', on_complete: '' },
  ]);
  assert.equal(wired[0].id, 'm1', 'the first id is the entry member');
});

test('linear wiring of nothing is empty', () => {
  const sb = loadApp({});
  assert.deepEqual(JSON.parse(JSON.stringify(sb.automationLinearWiring([]))), []);
});

// ── automationPickerCandidates ───────────────────────────────────────────────

test('picker marks templates, child Automata and Automata owned elsewhere as unavailable', () => {
  const sb = loadApp({});
  const rows = JSON.parse(JSON.stringify(sb.automationPickerCandidates(PRDS.prds, [OTHER_AUTOMATION], 'mine0001')));
  const byId = Object.fromEntries(rows.map(r => [r.id, r]));
  assert.equal(byId.aaaaaaaa1111.available, true);
  assert.equal(byId.aaaaaaaa1111.reason, '');
  assert.equal(byId.aaaaaaaa1111.title, 'Build the base');
  assert.equal(byId.aaaaaaaa1111.status, 'approved');
  assert.equal(byId.dddddddd4444.available, false);
  assert.equal(byId.dddddddd4444.reason, 'automation_picker_unavailable_template');
  assert.equal(byId.eeeeeeee5555.available, false);
  assert.equal(byId.eeeeeeee5555.reason, 'automation_picker_unavailable_child');
  assert.equal(byId.ffffffff6666.available, false);
  assert.equal(byId.ffffffff6666.reason, 'automation_picker_unavailable_owned');
});

test('picker keeps the members of the Automation being edited available', () => {
  const sb = loadApp({});
  const mine = { id: 'mine0001', name: 'Mine', status: 'active', members: [{ id: 'x1', prd_id: 'aaaaaaaa1111' }] };
  const rows = JSON.parse(JSON.stringify(sb.automationPickerCandidates(PRDS.prds, [mine, OTHER_AUTOMATION], 'mine0001')));
  assert.equal(rows.find(r => r.id === 'aaaaaaaa1111').available, true);
});

test('picker releases the members of a completed or cancelled Automation', () => {
  const sb = loadApp({});
  const done = Object.assign({}, OTHER_AUTOMATION, { status: 'completed' });
  const gone = Object.assign({}, OTHER_AUTOMATION, { id: 'oth00002', status: 'cancelled' });
  const rows = JSON.parse(JSON.stringify(sb.automationPickerCandidates(PRDS.prds, [done, gone], '')));
  assert.equal(rows.find(r => r.id === 'ffffffff6666').available, true);
});

test('picker rows show the reason text and are searchable by name or id', async () => {
  const sb = await openNew();
  const html = sb.__els.view.innerHTML;
  assert.ok(html.includes('This is a template'));
  assert.ok(html.includes('This is a child Automaton'));
  assert.ok(html.includes('Already in another Automation'));
  sb.automationFormSearch('checks');
  assert.ok(sb.__els.picker.innerHTML.includes('Run the checks'));
  assert.ok(!sb.__els.picker.innerHTML.includes('Build the base'));
  sb.automationFormSearch('CCCCCCCC');
  assert.ok(sb.__els.picker.innerHTML.includes('Ship it'));
  assert.ok(!sb.__els.picker.innerHTML.includes('Run the checks'));
  sb.automationFormSearch('no such thing');
  assert.ok(sb.__els.picker.innerHTML.includes('No Automata match'));
});

test('a hostile Automaton title is escaped in the picker and in the member rows', async () => {
  const sb = await openNew();
  assert.ok(!sb.__els.view.innerHTML.includes(HOSTILE), 'raw markup must not reach the picker');
  assert.ok(sb.__els.view.innerHTML.includes('&lt;img src=x onerror=alert(1)&gt;'));
  pickAndAdd(sb, 'abababab7777');
  assert.ok(!sb.__els.members.innerHTML.includes(HOSTILE));
  assert.ok(sb.__els.members.innerHTML.includes('&lt;img src=x onerror=alert(1)&gt;'));
});

// ── buildAutomationFormPayload ───────────────────────────────────────────────

test('payload for a new Automation has no expected_version and drops empty edges', () => {
  const sb = loadApp({});
  const body = JSON.parse(JSON.stringify(sb.buildAutomationFormPayload({
    name: '  Release  ',
    description: 'ship it',
    members: [
      { id: 'aaaa0001', prd_id: 'p1', on_complete: 'aaaa0002', on_blocked: '', on_failed: '' },
      { id: 'aaaa0002', prd_id: 'p2', on_complete: '', on_blocked: '', on_failed: 'aaaa0001' },
    ],
    entryMemberId: 'aaaa0001',
    reason: '',
  })));
  assert.deepEqual(body, {
    name: 'Release',
    description: 'ship it',
    entry_member_id: 'aaaa0001',
    members: [
      { id: 'aaaa0001', prd_id: 'p1', on_complete: 'aaaa0002' },
      { id: 'aaaa0002', prd_id: 'p2', on_failed: 'aaaa0001' },
    ],
    actor: 'operator',
  });
  assert.ok(!('expected_version' in body));
  assert.ok(!('reason' in body));
});

test('payload for an edit carries expected_version and the reason', () => {
  const sb = loadApp({});
  const body = JSON.parse(JSON.stringify(sb.buildAutomationFormPayload({
    name: 'Release',
    description: '',
    members: [{ id: 'aaaa0001', prd_id: 'p1' }],
    entryMemberId: 'aaaa0001',
    expectedVersion: 4,
    reason: ' scope changed ',
  })));
  assert.equal(body.expected_version, 4);
  assert.equal(body.reason, 'scope changed');
  assert.equal(body.actor, 'operator');
});

test('payload defaults the entry member to the first member', () => {
  const sb = loadApp({});
  const body = sb.buildAutomationFormPayload({ name: 'n', members: [{ id: 'aaaa0009', prd_id: 'p1' }, { id: 'aaaa0008', prd_id: 'p2' }] });
  assert.equal(body.entry_member_id, 'aaaa0009');
});

test('payload with locked members leaves the member list out and carries the base fields over', () => {
  const sb = loadApp({});
  const body = JSON.parse(JSON.stringify(sb.buildAutomationFormPayload({
    name: 'n',
    members: [{ id: 'aaaa0001', prd_id: 'p1' }],
    membersLocked: true,
    expectedVersion: 2,
    base: { backend: 'claude-code', model: 'sonnet', project_dir: '/work', budget_policy: { ceiling: 5 }, status: 'active' },
  })));
  assert.ok(!('members' in body));
  assert.ok(!('entry_member_id' in body));
  assert.equal(body.backend, 'claude-code');
  assert.equal(body.model, 'sonnet');
  assert.equal(body.project_dir, '/work');
  assert.deepEqual(body.budget_policy, { ceiling: 5 });
  assert.ok(!('status' in body));
});

// ── the form ─────────────────────────────────────────────────────────────────

test('the new form shows the new heading, the name and description fields and the buttons', async () => {
  const sb = await openNew();
  const html = sb.__els.view.innerHTML;
  assert.ok(html.includes('New Automation'));
  assert.ok(html.includes('>Name<'));
  assert.ok(html.includes('>Description<'));
  assert.ok(html.includes('>Members<'));
  assert.ok(html.includes('>Add Automaton<'));
  assert.ok(html.includes('>Save<'));
  assert.ok(html.includes('>Cancel<'));
  assert.ok(!html.includes('automationFormReason'), 'no reason field for a new Automation');
});

test('picking several Automata in order wires them linearly with 8-hex member ids', async () => {
  const sb = await openNew();
  pickAndAdd(sb, 'aaaaaaaa1111');
  pickAndAdd(sb, 'bbbbbbbb2222');
  pickAndAdd(sb, 'cccccccc3333');
  const members = formState(sb).members;
  assert.equal(members.length, 3);
  members.forEach(m => assert.match(m.id, HEX8));
  assert.equal(new Set(members.map(m => m.id)).size, 3);
  assert.equal(members[0].on_complete, members[1].id);
  assert.equal(members[1].on_complete, members[2].id);
  assert.equal(members[2].on_complete, '');
  assert.equal(formState(sb).entryMemberId, members[0].id);
});

test('adding without a picked Automaton adds nothing and says so', async () => {
  const sb = await openNew();
  sb.automationFormAdd();
  assert.equal(formState(sb).members.length, 0);
  assert.ok(sb.__els.error.innerHTML.includes('Choose an Automaton to insert'));
});

test('an unavailable Automaton cannot be picked or added', async () => {
  const sb = await openNew();
  pickAndAdd(sb, 'dddddddd4444');
  pickAndAdd(sb, 'ffffffff6666');
  assert.equal(formState(sb).members.length, 0);
});

test('a member already in the list leaves the picker', async () => {
  const sb = await openNew();
  pickAndAdd(sb, 'aaaaaaaa1111');
  assert.ok(!sb.__els.picker.innerHTML.includes('Build the base'));
  assert.ok(sb.__els.picker.innerHTML.includes('Run the checks'));
});

test('each member row has three edge selects offering the other members and stop and alert', async () => {
  const sb = await openNew();
  pickAndAdd(sb, 'aaaaaaaa1111');
  pickAndAdd(sb, 'bbbbbbbb2222');
  const [first, second] = formState(sb).members;
  const html = sb.__els.members.innerHTML;
  const rowOf = id => html.split('data-member-id="').find(r => r.startsWith(id + '"'));
  const row = rowOf(first.id);
  assert.equal((row.match(/<select /g) || []).length, 3);
  assert.ok(row.includes('On complete'));
  assert.ok(row.includes('On blocked'));
  assert.ok(row.includes('On failed'));
  assert.ok(row.includes('Stop and alert'));
  assert.ok(row.includes('Run the checks'), 'the other member is offered by title');
  assert.ok(row.includes(`value="${second.id}" selected`), 'on complete defaults to the next member');
  const secondRow = rowOf(second.id);
  assert.ok(secondRow.includes('Build the base'));
  assert.ok(!secondRow.includes(`value="${second.id}"`), 'a member is not offered as its own target');
});

test('changing an edge select sets that edge', async () => {
  const sb = await openNew();
  pickAndAdd(sb, 'aaaaaaaa1111');
  pickAndAdd(sb, 'bbbbbbbb2222');
  const [first, second] = formState(sb).members;
  sb.automationFormSetEdge(second.id, 'on_failed', first.id);
  assert.equal(formState(sb).members[1].on_failed, first.id);
  sb.automationFormSetEdge(first.id, 'on_complete', '');
  assert.equal(formState(sb).members[0].on_complete, '');
  sb.automationFormSetEdge(first.id, 'on_blocked', 'zzzzzzzz');
  assert.equal(formState(sb).members[0].on_blocked, '', 'an unknown target is ignored');
});

test('insert before and insert after put the picked Automaton at that position and rewire', async () => {
  const sb = await openNew();
  pickAndAdd(sb, 'aaaaaaaa1111');
  pickAndAdd(sb, 'bbbbbbbb2222');
  const [first, second] = formState(sb).members.map(m => m.id);
  sb.automationFormPick('cccccccc3333');
  sb.automationFormInsert(second, 'before');
  let members = formState(sb).members;
  assert.deepEqual(plain(members.map(m => m.prd_id)), ['aaaaaaaa1111', 'cccccccc3333', 'bbbbbbbb2222']);
  assert.deepEqual(plain(members.map(m => m.on_complete)), [members[1].id, second, '']);
  assert.equal(members[0].id, first);

  sb.automationFormPick('abababab7777');
  sb.automationFormInsert(second, 'after');
  members = formState(sb).members;
  assert.deepEqual(plain(members.map(m => m.prd_id)), ['aaaaaaaa1111', 'cccccccc3333', 'bbbbbbbb2222', 'abababab7777']);
  assert.deepEqual(plain(members.map(m => m.on_complete)), [members[1].id, second, members[3].id, '']);
});

test('inserting before the first member makes the new member the entry', async () => {
  const sb = await openNew();
  pickAndAdd(sb, 'aaaaaaaa1111');
  const first = formState(sb).members[0].id;
  sb.automationFormPick('bbbbbbbb2222');
  sb.automationFormInsert(first, 'before');
  const members = formState(sb).members;
  assert.equal(members[0].prd_id, 'bbbbbbbb2222');
  assert.equal(members[0].on_complete, first);
  assert.equal(formState(sb).entryMemberId, members[0].id);
});

test('an edge the operator changed survives a later insert', async () => {
  const sb = await openNew();
  pickAndAdd(sb, 'aaaaaaaa1111');
  pickAndAdd(sb, 'bbbbbbbb2222');
  const [first, second] = formState(sb).members.map(m => m.id);
  sb.automationFormSetEdge(first, 'on_complete', '');
  pickAndAdd(sb, 'cccccccc3333');
  const members = formState(sb).members;
  assert.equal(members[0].on_complete, '', 'the stop the operator chose stays');
  assert.equal(members[1].on_complete, members[2].id);
  assert.equal(second, members[1].id);
});

test('remove drops a member and bridges the chain over it', async () => {
  const sb = await openNew();
  pickAndAdd(sb, 'aaaaaaaa1111');
  pickAndAdd(sb, 'bbbbbbbb2222');
  pickAndAdd(sb, 'cccccccc3333');
  const [first, second, third] = formState(sb).members.map(m => m.id);
  sb.automationFormSetEdge(third, 'on_failed', second);
  sb.automationFormRemove(second);
  const members = formState(sb).members;
  assert.deepEqual(plain(members.map(m => m.id)), [first, third]);
  assert.equal(members[0].on_complete, third);
  assert.equal(members[1].on_failed, '');
  sb.automationFormRemove(first);
  assert.equal(formState(sb).entryMemberId, third);
});

// ── saving ───────────────────────────────────────────────────────────────────

test('saving a new Automation posts the linear wiring and opens the returned Automation', async () => {
  const created = { id: 'new00001', name: 'Release', status: 'draft', version: 1, entry_member_id: 'x', members: [] };
  const sb = await openNew({
    'POST /api/automations': { status: 201, body: created },
    'GET /api/automations/new00001': { body: created },
  }, { name: 'Release', description: 'the train' });
  pickAndAdd(sb, 'aaaaaaaa1111');
  pickAndAdd(sb, 'bbbbbbbb2222');
  pickAndAdd(sb, 'cccccccc3333');
  await sb.saveAutomationForm();
  await flushAsync();
  const posts = writes(sb);
  assert.equal(posts.length, 1);
  assert.equal(posts[0].method, 'POST');
  assert.equal(posts[0].url, '/api/automations');
  const body = posts[0].body;
  assert.equal(body.name, 'Release');
  assert.equal(body.description, 'the train');
  assert.equal(body.actor, 'operator');
  assert.ok(!('expected_version' in body));
  assert.deepEqual(body.members.map(m => m.prd_id), ['aaaaaaaa1111', 'bbbbbbbb2222', 'cccccccc3333']);
  body.members.forEach(m => assert.match(m.id, HEX8));
  assert.equal(body.entry_member_id, body.members[0].id);
  assert.equal(body.members[0].on_complete, body.members[1].id);
  assert.equal(body.members[1].on_complete, body.members[2].id);
  assert.ok(!('on_complete' in body.members[2]), 'the last member stops');
  assert.ok(sb.__calls.some(c => c.method === 'GET' && c.url === '/api/automations/new00001'), 'the detail view of the new Automation loads');
});

test('saving an edit of a draft PUTs the loaded version', async () => {
  const draft = {
    id: 'dr000001', name: 'Draft chain', description: '', status: 'draft', version: 3, entry_member_id: 'm1',
    backend: 'claude-code', project_dir: '/work',
    members: [
      { id: 'm1', prd_id: 'aaaaaaaa1111', status: 'pending', on_complete: 'm2' },
      { id: 'm2', prd_id: 'bbbbbbbb2222', status: 'pending' },
    ],
  };
  const sb = loadApp(Object.assign({}, BASE_ROUTES, {
    'PUT /api/automations/dr000001': { body: Object.assign({}, draft, { version: 4 }) },
    'GET /api/automations/dr000001': { body: draft },
  }), { name: 'Renamed chain' });
  sb.openAutomationForm(draft);
  await flushAsync();
  assert.ok(sb.__els.view.innerHTML.includes('Edit Automation'));
  assert.ok(!sb.__els.view.innerHTML.includes('automationFormReason'), 'a draft needs no reason');
  await sb.saveAutomationForm();
  await flushAsync();
  const puts = writes(sb);
  assert.equal(puts.length, 1);
  assert.equal(puts[0].method, 'PUT');
  assert.equal(puts[0].url, '/api/automations/dr000001');
  assert.equal(puts[0].body.expected_version, 3);
  assert.equal(puts[0].body.name, 'Renamed chain');
  assert.equal(puts[0].body.backend, 'claude-code', 'fields the form does not show are carried over');
  assert.equal(puts[0].body.project_dir, '/work');
  assert.deepEqual(puts[0].body.members.map(m => m.id), ['m1', 'm2']);
  assert.equal(puts[0].body.members[0].on_complete, 'm2');
});

test('editing a started Automation asks for a reason, shows finished members read-only and sends no members', async () => {
  const running = {
    id: 'ru000001', name: 'Running chain', description: 'd', status: 'active', version: 7, entry_member_id: 'm1',
    members: [
      { id: 'm1', prd_id: 'aaaaaaaa1111', status: 'completed', on_complete: 'm2' },
      { id: 'm2', prd_id: 'bbbbbbbb2222', status: 'failed', on_complete: 'm3', on_failed: 'm3' },
      { id: 'm3', prd_id: 'cccccccc3333', status: 'skipped', on_complete: 'm4' },
      { id: 'm4', prd_id: 'abababab7777', status: 'pending' },
    ],
  };
  const sb = loadApp(Object.assign({}, BASE_ROUTES, {
    'PUT /api/automations/ru000001': { body: Object.assign({}, running, { version: 8 }) },
    'GET /api/automations/ru000001': { body: running },
  }), { name: 'Running chain, renamed', reason: '' });
  sb.openAutomationForm(running);
  await flushAsync();
  const html = sb.__els.view.innerHTML;
  assert.ok(html.includes('Reason for this change'));
  assert.ok(html.includes('automationFormReason'));
  const readonlyRows = html.split('data-member-id="').slice(1, 4);
  readonlyRows.forEach(r => {
    assert.ok(r.includes('data-readonly="true"'));
    assert.ok(!r.split('automation-form-member')[0].includes('<select'));
  });
  assert.ok(html.includes('Completed'));
  assert.ok(html.includes('Failed'));
  assert.ok(html.includes('Skipped'));

  await sb.saveAutomationForm();
  assert.equal(writes(sb).length, 0, 'no reason, nothing sent');
  assert.ok(sb.__els.error.innerHTML.includes('A reason is required for this change'));

  sb.__els.reason.value = 'rename only';
  await sb.saveAutomationForm();
  await flushAsync();
  const puts = writes(sb);
  assert.equal(puts.length, 1);
  assert.equal(puts[0].body.expected_version, 7);
  assert.equal(puts[0].body.reason, 'rename only');
  assert.ok(!('members' in puts[0].body));
});

test('on a started Automation the insert buttons use the detail insert dialog', async () => {
  const running = {
    id: 'ru000002', name: 'Running chain', status: 'active', version: 1, entry_member_id: 'm1',
    members: [
      { id: 'm1', prd_id: 'aaaaaaaa1111', status: 'completed', on_complete: 'm2' },
      { id: 'm2', prd_id: 'bbbbbbbb2222', status: 'pending' },
    ],
  };
  const sb = loadApp(BASE_ROUTES);
  const opened = [];
  sb.openAutomationInsertDialog = (anchor, position) => { opened.push([anchor, position]); };
  sb.openAutomationForm(running);
  await flushAsync();
  sb.automationFormInsert('m2', 'after');
  sb.automationFormAdd();
  assert.deepEqual(plain(opened), [['m2', 'after'], ['m2', 'after']]);
  assert.equal(formState(sb).members.length, 2, 'the form does not edit the members locally');
});

test('a 409 on save shows the stale prompt with the server message', async () => {
  const draft = {
    id: 'dr000002', name: 'Draft chain', status: 'draft', version: 3, entry_member_id: 'm1',
    members: [{ id: 'm1', prd_id: 'aaaaaaaa1111', status: 'pending' }],
  };
  const sb = loadApp(Object.assign({}, BASE_ROUTES, {
    'PUT /api/automations/dr000002': { status: 409, body: 'version 2 is stale' },
  }), { name: 'Draft chain' });
  sb.openAutomationForm(draft);
  await flushAsync();
  const result = await sb.saveAutomationForm();
  await flushAsync();
  assert.equal(result, null);
  const modal = sb.__modals.find(m => m.id === 'genericModal');
  assert.ok(modal, 'the stale prompt opened');
  assert.ok(modal.innerHTML.includes('Out of date'));
  assert.ok(modal.innerHTML.includes('version 2 is stale'));
  assert.equal(sb.__els.error.innerHTML, '', 'a stale version is not an error line');
});

test('a 400 on save shows the server message above the form and stays on the form', async () => {
  const sb = await openNew({
    'POST /api/automations': { status: 400, body: { error: 'automation invalid: cycle detected' } },
  }, { name: 'Loop' });
  pickAndAdd(sb, 'aaaaaaaa1111');
  const result = await sb.saveAutomationForm();
  await flushAsync();
  assert.equal(result, null);
  assert.ok(sb.__els.error.innerHTML.includes('automation invalid: cycle detected'));
  assert.equal(sb.__modals.filter(m => m.id === 'genericModal').length, 0);
  assert.equal(sb.__els.save.disabled, false, 'the save button is usable again');
  assert.ok(!sb.__calls.some(c => c.method === 'GET' && c.url.startsWith('/api/automations/')), 'still on the form');
});

test('a hostile server message is escaped in the error line', async () => {
  const sb = await openNew({
    'POST /api/automations': { status: 400, body: HOSTILE },
  }, { name: 'Loop' });
  pickAndAdd(sb, 'aaaaaaaa1111');
  await sb.saveAutomationForm();
  assert.ok(!sb.__els.error.innerHTML.includes(HOSTILE));
  assert.ok(sb.__els.error.innerHTML.includes('&lt;img'));
});

test('an empty name sends nothing and says a name is required', async () => {
  const sb = await openNew({}, { name: '   ' });
  pickAndAdd(sb, 'aaaaaaaa1111');
  const result = await sb.saveAutomationForm();
  assert.equal(result, null);
  assert.equal(writes(sb).length, 0);
  assert.ok(sb.__els.error.innerHTML.includes('A name is required'));
});

test('no members sends nothing and says to add at least one member', async () => {
  const sb = await openNew({}, { name: 'Release' });
  const result = await sb.saveAutomationForm();
  assert.equal(result, null);
  assert.equal(writes(sb).length, 0);
  assert.ok(sb.__els.error.innerHTML.includes('Add at least one member'));
});

test('cancel on a new form returns to the list', async () => {
  const sb = await openNew();
  let drew = 0;
  sb.renderAutomationView = () => { drew++; };
  await sb.automationFormCancel();
  assert.equal(drew, 1);
});

test('cancel on an edit opened from the detail page returns to the detail page', async () => {
  const draft = {
    id: 'dr000003', name: 'Draft chain', status: 'draft', version: 1, entry_member_id: 'm1',
    members: [{ id: 'm1', prd_id: 'aaaaaaaa1111', status: 'pending' }],
  };
  const sb = loadApp(Object.assign({}, BASE_ROUTES, { 'GET /api/automations/dr000003': { body: draft } }));
  vm.runInContext("_automationState.detailId = 'dr000003'", sb);
  sb.openAutomationForm(draft);
  await flushAsync();
  await sb.automationFormCancel();
  await flushAsync();
  assert.ok(sb.__calls.some(c => c.method === 'GET' && c.url === '/api/automations/dr000003'));
});
