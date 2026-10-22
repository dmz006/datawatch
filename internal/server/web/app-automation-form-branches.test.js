// Automation tab — fork, join and nested members in the create and edit form.
//
// Covers adding a fork with its branches and paired join, adding a nested
// Automation, the rejections (too few branches, itself, already owned), the
// request body for each join rule, the absence of outcome edges on branches
// and forks, removing a whole fork group, and the grouped rows the members
// editor draws. fetch is stubbed; rendered text is the real English bundle.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const HEX8 = /^[0-9a-f]{8}$/;

const PRDS = {
  prds: [
    { id: 'aaaaaaaa1111', title: 'Build the base', status: 'approved' },
    { id: 'bbbbbbbb2222', title: 'Run the checks', status: 'approved' },
    { id: 'cccccccc3333', title: 'Ship it', status: 'draft' },
    { id: 'dddddddd4444', title: 'Write the notes', status: 'approved' },
    { id: 'eeeeeeee5555', title: 'Publish', status: 'approved' },
  ],
};

const CHILD = { id: 'chi00001', name: 'Nightly chain', status: 'draft', version: 1, members: [{ id: 'c1', prd_id: 'zzzzzzzz9999' }] };
const OWNER = {
  id: 'own00001', name: 'Owner chain', status: 'active', version: 3, entry_member_id: 'o1',
  members: [{ id: 'o1', kind: 'automation', automation_id: 'taken001', status: 'running' }],
};
const TAKEN = { id: 'taken001', name: 'Taken chain', status: 'draft', version: 1, members: [] };
const FINISHED_OWNER = {
  id: 'fin00001', name: 'Finished chain', status: 'completed', version: 4, entry_member_id: 'f1',
  members: [{ id: 'f1', kind: 'automation', automation_id: 'reuse001', status: 'completed' }],
};
const REUSABLE = { id: 'reuse001', name: 'Reusable chain', status: 'draft', version: 1, members: [] };

const LIST = [CHILD, OWNER, TAKEN, FINISHED_OWNER, REUSABLE];

function loadApp(routes) {
  const els = {
    view: makeStubElement(),
    members: { innerHTML: '' },
    panel: { innerHTML: '' },
    error: { innerHTML: '' },
  };
  const byId = {
    view: els.view,
    automationFormMembers: els.members,
    automationFormPanel: els.panel,
    automationFormError: els.error,
    automationFormPicker: { innerHTML: '' },
    automationFormName: { value: 'Chain' },
    automationFormDescription: { value: '' },
    automationFormReason: { value: '' },
    automationFormSaveBtn: { disabled: false },
  };
  const sandbox = buildSandbox({ byId });
  sandbox.fetch = (url, opts) => {
    const method = (opts && opts.method) || 'GET';
    const hit = routes[method + ' ' + url];
    if (!hit) return Promise.resolve({ ok: false, status: 404, statusText: 'Not Found', text: () => Promise.resolve('not found') });
    return Promise.resolve({
      ok: true,
      status: 200,
      statusText: '200',
      json: () => Promise.resolve(hit.body),
      text: () => Promise.resolve(JSON.stringify(hit.body)),
    });
  };
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  sandbox.__els = els;
  vm.runInContext('_automationState.list = ' + JSON.stringify(LIST), sandbox);
  return sandbox;
}

const plain = value => JSON.parse(JSON.stringify(value));
const formState = sb => vm.runInContext('_automationForm', sb);
const EN = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));

const ROUTES = { 'GET /api/autonomous/prds': { body: PRDS }, 'GET /api/automations': { body: LIST } };

async function openNew() {
  const sb = loadApp(ROUTES);
  sb.openAutomationForm(null);
  await flushAsync();
  return sb;
}

// chain returns a plain two-member form state: m1 leads to m2.
function chain() {
  return {
    name: 'Chain',
    entryMemberId: 'm1',
    members: [
      { id: 'm1', prd_id: 'aaaaaaaa1111', on_complete: 'm2', on_blocked: '', on_failed: '' },
      { id: 'm2', prd_id: 'bbbbbbbb2222', on_complete: '', on_blocked: '', on_failed: '' },
    ],
  };
}

const byId = (state, id) => state.members.find(m => m.id === id);
const forkOf = state => state.members.find(m => m.kind === 'fork');
const joinOf = state => state.members.find(m => m.kind === 'join');

// ── automationAddForkJoin ────────────────────────────────────────────────────

test('a fork gets the given branch ids, the join lists the same ids and carries the chosen rule', () => {
  const sb = loadApp({});
  const out = plain(sb.automationAddForkJoin(chain(), ['cccccccc3333', 'dddddddd4444'], 'any_one_is_enough', 'm1'));
  const fork = forkOf(out);
  const join = joinOf(out);
  assert.equal(fork.fork_members.length, 2);
  fork.fork_members.forEach(id => assert.match(id, HEX8));
  assert.deepEqual(fork.fork_members.map(id => byId(out, id).prd_id), ['cccccccc3333', 'dddddddd4444']);
  assert.deepEqual(join.join_members, fork.fork_members);
  assert.equal(join.on_branch_failure, 'any_one_is_enough');
  assert.match(fork.id, HEX8);
  assert.match(join.id, HEX8);
  assert.equal(new Set(out.members.map(m => m.id)).size, out.members.length, 'member ids are unique');
});

test('branches and the fork carry no edges', () => {
  const sb = loadApp({});
  const out = plain(sb.automationAddForkJoin(chain(), ['cccccccc3333', 'dddddddd4444'], 'all_must_succeed', 'm1'));
  const fork = forkOf(out);
  [fork].concat(fork.fork_members.map(id => byId(out, id))).forEach(m => {
    ['on_complete', 'on_blocked', 'on_failed'].forEach(edge => assert.ok(!m[edge], m.id + ' has no ' + edge));
  });
  assert.equal(byId(out, fork.fork_members[0]).kind, undefined, 'a branch is a plain Automaton member');
});

test('the previous member leads to the fork and the join leads on to what it led to', () => {
  const sb = loadApp({});
  const out = plain(sb.automationAddForkJoin(chain(), ['cccccccc3333', 'dddddddd4444'], 'all_must_succeed', 'm1'));
  assert.equal(byId(out, 'm1').on_complete, forkOf(out).id);
  assert.equal(joinOf(out).on_complete, 'm2');
  assert.equal(byId(out, 'm2').on_complete, '');
  assert.equal(out.entryMemberId, 'm1');
  const ids = out.members.map(m => m.id);
  assert.ok(ids.indexOf('m1') < ids.indexOf(forkOf(out).id) && ids.indexOf(joinOf(out).id) < ids.indexOf('m2'), 'the group sits between m1 and m2');
});

test('inserting after the last member leaves the join as the end of the chain', () => {
  const sb = loadApp({});
  const out = plain(sb.automationAddForkJoin(chain(), ['cccccccc3333', 'dddddddd4444'], 'all_must_succeed', 'm2'));
  assert.equal(byId(out, 'm2').on_complete, forkOf(out).id);
  assert.equal(joinOf(out).on_complete, '');
});

test('with no previous member the fork becomes the entry and the join leads to the old entry', () => {
  const sb = loadApp({});
  const out = plain(sb.automationAddForkJoin(chain(), ['cccccccc3333', 'dddddddd4444'], 'all_must_succeed', ''));
  assert.equal(out.entryMemberId, forkOf(out).id);
  assert.equal(joinOf(out).on_complete, 'm1');
  const empty = plain(sb.automationAddForkJoin({ members: [] }, ['cccccccc3333', 'dddddddd4444'], 'all_must_succeed', ''));
  assert.equal(empty.entryMemberId, forkOf(empty).id);
  assert.equal(joinOf(empty).on_complete, '');
});

test('adding a fork does not change the state it was given', () => {
  const sb = loadApp({});
  const before = chain();
  sb.automationAddForkJoin(before, ['cccccccc3333', 'dddddddd4444'], 'all_must_succeed', 'm1');
  assert.deepEqual(before, chain());
});

test('the join rule defaults to all_must_succeed', () => {
  const sb = loadApp({});
  const out = plain(sb.automationAddForkJoin(chain(), ['cccccccc3333', 'dddddddd4444'], undefined, 'm1'));
  assert.equal(joinOf(out).on_branch_failure, 'all_must_succeed');
});

test('fewer than two branches throws the key text', () => {
  const sb = loadApp({});
  const want = EN.automation_fork_min_branches;
  assert.equal(want, 'A fork needs at least two branches');
  assert.throws(() => sb.automationAddForkJoin(chain(), [], 'all_must_succeed', 'm1'), err => err.message === want);
  assert.throws(() => sb.automationAddForkJoin(chain(), ['cccccccc3333'], 'all_must_succeed', 'm1'), err => err.message === want);
  assert.throws(() => sb.automationAddForkJoin(chain(), ['cccccccc3333', 'cccccccc3333'], 'all_must_succeed', 'm1'), err => err.message === want);
});

for (const rule of ['all_must_succeed', 'any_one_is_enough', 'continue_anyway']) {
  test('the ' + rule + ' rule is carried into the payload', () => {
    const sb = loadApp({});
    const state = sb.automationAddForkJoin(chain(), ['cccccccc3333', 'dddddddd4444'], rule, 'm1');
    const body = plain(sb.buildAutomationFormPayload(Object.assign({}, state, { name: 'Chain' })));
    const join = body.members.find(m => m.kind === 'join');
    assert.equal(join.on_branch_failure, rule);
    const fork = body.members.find(m => m.kind === 'fork');
    assert.deepEqual(join.join_members, fork.fork_members);
    assert.equal(join.on_complete, 'm2');
  });
}

// ── automationAddNestedMember ────────────────────────────────────────────────

test('a nested member has kind automation and automation_id, and no prd_id', () => {
  const sb = loadApp({});
  const out = plain(sb.automationAddNestedMember(chain(), 'chi00001', 'm1'));
  const nested = out.members.find(m => m.kind === 'automation');
  assert.equal(nested.automation_id, 'chi00001');
  assert.ok(!('prd_id' in nested));
  assert.match(nested.id, HEX8);
  assert.equal(byId(out, 'm1').on_complete, nested.id);
  assert.equal(nested.on_complete, 'm2');
  const body = plain(sb.buildAutomationFormPayload(Object.assign({}, out, { name: 'Chain' })));
  const sent = body.members.find(m => m.kind === 'automation');
  assert.equal(sent.automation_id, 'chi00001');
  assert.ok(!('prd_id' in sent));
});

test('an Automation cannot nest itself', () => {
  const sb = loadApp({});
  const state = Object.assign(chain(), { source: { id: 'chi00001' } });
  assert.throws(() => sb.automationAddNestedMember(state, 'chi00001', 'm1'), err => err.message === 'An Automation cannot contain itself');
  const withId = Object.assign(chain(), { automationId: 'chi00001' });
  assert.throws(() => sb.automationAddNestedMember(withId, 'chi00001', 'm1'), err => err.message === EN.automation_nested_self);
});

test('a child already inside another active Automation is rejected', () => {
  const sb = loadApp({});
  assert.throws(() => sb.automationAddNestedMember(chain(), 'taken001', 'm1'),
    err => err.message === 'That Automation is already inside another Automation');
});

test('a child held only by a finished Automation, or by the Automation being edited, is accepted', () => {
  const sb = loadApp({});
  const out = plain(sb.automationAddNestedMember(chain(), 'reuse001', 'm1'));
  assert.equal(out.members.find(m => m.kind === 'automation').automation_id, 'reuse001');
  const state = Object.assign(chain(), { source: { id: 'own00001' } });
  const own = plain(sb.automationAddNestedMember(state, 'taken001', 'm1'));
  assert.equal(own.members.find(m => m.kind === 'automation').automation_id, 'taken001');
});

test('the same child cannot be added twice', () => {
  const sb = loadApp({});
  const once = sb.automationAddNestedMember(chain(), 'chi00001', 'm1');
  assert.throws(() => sb.automationAddNestedMember(once, 'chi00001', 'm2'), err => err.message === EN.automation_nested_owned);
});

// ── payload ──────────────────────────────────────────────────────────────────

test('the payload for fork, join and nested members has no outcome edges on branches or forks', () => {
  const sb = loadApp({});
  let state = sb.automationAddForkJoin(chain(), ['cccccccc3333', 'dddddddd4444'], 'continue_anyway', 'm1');
  state = sb.automationAddNestedMember(state, 'chi00001', 'm2');
  // Stale edges on a fork or a branch must not be sent.
  const dirty = plain(state);
  const fork = forkOf(dirty);
  fork.on_complete = 'm2';
  byId(dirty, fork.fork_members[0]).on_failed = 'm2';
  const body = plain(sb.buildAutomationFormPayload(Object.assign({}, dirty, { name: 'Chain' })));
  const sentFork = body.members.find(m => m.kind === 'fork');
  const branches = sentFork.fork_members.map(id => body.members.find(m => m.id === id));
  [sentFork].concat(branches).forEach(m => {
    ['on_complete', 'on_blocked', 'on_failed'].forEach(edge => assert.ok(!(edge in m), m.id + ' must not send ' + edge));
  });
  branches.forEach(b => assert.ok(b.prd_id));
  const join = body.members.find(m => m.kind === 'join');
  assert.deepEqual(Object.keys(join).filter(k => k.startsWith('on_')).sort(), ['on_branch_failure', 'on_complete']);
  assert.ok(!('prd_id' in sentFork) && !('prd_id' in join));
  assert.ok(!('join_members' in sentFork) && !('fork_members' in join));
  assert.ok(!('automation_id' in sentFork) && !('on_branch_failure' in sentFork));
  assert.equal(body.members.find(m => m.kind === 'automation').automation_id, 'chi00001');
  assert.equal(body.entry_member_id, 'm1');
});

// ── removing a group ─────────────────────────────────────────────────────────

test('removing a fork group removes the branches and the join as well', () => {
  const sb = loadApp({});
  const added = plain(sb.automationAddForkJoin(chain(), ['cccccccc3333', 'dddddddd4444'], 'all_must_succeed', 'm1'));
  const out = plain(sb.automationRemoveForkGroup(added, forkOf(added).id));
  assert.deepEqual(out.members.map(m => m.id), ['m1', 'm2']);
  assert.ok(!joinOf(out));
  assert.equal(byId(out, 'm1').on_complete, 'm2', 'the edge that led to the fork leads on to what the join led to');
  assert.equal(out.entryMemberId, 'm1');
});

test('removing a fork that is the entry hands the entry to what the join led to', () => {
  const sb = loadApp({});
  const added = plain(sb.automationAddForkJoin(chain(), ['cccccccc3333', 'dddddddd4444'], 'all_must_succeed', ''));
  assert.equal(added.entryMemberId, forkOf(added).id);
  const out = plain(sb.automationRemoveForkGroup(added, forkOf(added).id));
  assert.equal(out.entryMemberId, 'm1');
});

// ── the members editor ───────────────────────────────────────────────────────

async function formWithFork(rule) {
  const sb = await openNew();
  sb.automationFormPick('aaaaaaaa1111');
  sb.automationFormAdd();
  sb.automationFormPick('bbbbbbbb2222');
  sb.automationFormAdd();
  sb.automationFormOpenPanel('fork');
  sb.automationFormSetForkBranches(['cccccccc3333', 'dddddddd4444']);
  sb.automationFormSetForkRule(rule || 'all_must_succeed');
  assert.equal(sb.automationFormApplyFork(), true);
  return sb;
}

test('the form offers the fork and nested buttons with translated labels', async () => {
  const sb = await openNew();
  const html = sb.__els.view.innerHTML;
  assert.match(html, /data-op="add_fork"[^>]*>Add fork and join</);
  assert.match(html, /data-op="add_nested"[^>]*>Add nested Automation</);
});

test('the fork panel offers a multi-select of Automata and the join rule select', async () => {
  const sb = await openNew();
  sb.automationFormOpenPanel('fork');
  const html = sb.__els.panel.innerHTML;
  assert.match(html, /<select multiple/);
  assert.match(html, /Build the base/);
  assert.match(html, /When a branch fails/);
  assert.match(html, /All branches must succeed/);
  assert.match(html, /One branch is enough/);
  assert.match(html, /Continue anyway/);
});

test('the nested panel offers the other Automations', async () => {
  const sb = await openNew();
  sb.automationFormOpenPanel('nested');
  const html = sb.__els.panel.innerHTML;
  assert.match(html, /Nightly chain/);
  assert.match(html, /Choose an Automation/);
  sb.automationFormClosePanel();
  assert.equal(sb.__els.panel.innerHTML, '');
});

test('applying the fork panel puts the group at the end of the chain and draws it grouped', async () => {
  const sb = await formWithFork('continue_anyway');
  const f = plain(formState(sb));
  const fork = forkOf(f);
  const join = joinOf(f);
  assert.equal(join.on_branch_failure, 'continue_anyway');
  const second = f.members[1];
  assert.equal(second.on_complete, fork.id);
  assert.equal(join.on_complete, '');
  const html = sb.__els.members.innerHTML;
  assert.ok(html.indexOf('data-fork-id="' + fork.id + '"') >= 0, 'a group wraps the fork');
  fork.fork_members.forEach(id => {
    assert.match(html, new RegExp('automation-form-branch" style="margin-left:24px;"><div class="automation-form-member automation-form-branch-row" data-member-id="' + id + '"'));
  });
  assert.ok(html.indexOf('data-member-id="' + fork.id + '"') < html.indexOf('data-member-id="' + fork.fork_members[0] + '"'), 'branches come after their fork');
  assert.ok(html.indexOf('data-member-id="' + fork.fork_members[1] + '"') < html.indexOf('data-member-id="' + join.id + '"'), 'the join comes after the branches');
  assert.equal(sb.__els.panel.innerHTML, '', 'the panel closes');
});

test('branch rows and the fork row have no edge selects; the join row has the rule select and one edge select', async () => {
  const sb = await formWithFork();
  const f = plain(formState(sb));
  const fork = forkOf(f);
  const join = joinOf(f);
  const html = sb.__els.members.innerHTML;
  const rowOf = id => {
    const start = html.indexOf('data-member-id="' + id + '"');
    const next = html.indexOf('data-member-id=', start + 10);
    return html.slice(start, next < 0 ? html.length : next);
  };
  fork.fork_members.concat([fork.id]).forEach(id => {
    assert.ok(!/<select/.test(rowOf(id)), id + ' has no select');
  });
  const joinRow = rowOf(join.id);
  assert.equal((joinRow.match(/<select/g) || []).length, 2);
  assert.match(joinRow, /data-join-rule="true"/);
  assert.match(joinRow, /data-edge="on_complete"/);
  assert.ok(!/data-edge="on_blocked"|data-edge="on_failed"/.test(joinRow));
});

test('changing the join rule select updates on_branch_failure', async () => {
  const sb = await formWithFork();
  const join = joinOf(plain(formState(sb)));
  assert.equal(join.on_branch_failure, 'all_must_succeed');
  sb.automationFormSetJoinRule(join.id, 'any_one_is_enough');
  assert.equal(joinOf(plain(formState(sb))).on_branch_failure, 'any_one_is_enough');
  sb.automationFormSetJoinRule(join.id, 'not-a-rule');
  assert.equal(joinOf(plain(formState(sb))).on_branch_failure, 'any_one_is_enough');
});

test('edge selects of ordinary members never offer a join or a branch', async () => {
  const sb = await formWithFork();
  const f = plain(formState(sb));
  const html = sb.__els.members.innerHTML;
  const start = html.indexOf('data-member-id="' + f.members[0].id + '"');
  const end = html.indexOf('automation-form-group', start);
  const firstRow = html.slice(start, end);
  assert.ok(firstRow.includes('value="' + forkOf(f).id + '"'), 'the fork is a valid target');
  assert.ok(!firstRow.includes('value="' + joinOf(f).id + '"'));
  forkOf(f).fork_members.forEach(id => assert.ok(!firstRow.includes('value="' + id + '"')));
  sb.automationFormSetEdge(f.members[0].id, 'on_failed', joinOf(f).id);
  assert.equal(plain(formState(sb)).members[0].on_failed, '');
});

test('removing a fork group from the form asks first, then removes the join too', async () => {
  const sb = await formWithFork();
  const fork = forkOf(plain(formState(sb)));
  let asked = '';
  let confirm = null;
  sb.showConfirmModal = (message, onConfirm) => { asked = message; confirm = onConfirm; };
  sb.automationFormRemoveGroup(fork.id);
  assert.equal(asked, 'Remove the fork, its branches and its join?');
  assert.equal(plain(formState(sb)).members.length, 6, 'nothing is removed before the answer');
  confirm();
  const f = plain(formState(sb));
  assert.equal(f.members.length, 2);
  assert.ok(!forkOf(f) && !joinOf(f));
  assert.equal(f.members[1].on_complete, '');
  assert.ok(!sb.__els.members.innerHTML.includes('data-fork-id'));
});

test('an Automaton added after a fork follows the join and leaves the branches alone', async () => {
  const sb = await formWithFork();
  sb.automationFormPick('eeeeeeee5555');
  sb.automationFormAdd();
  const f = plain(formState(sb));
  const added = f.members[f.members.length - 1];
  assert.equal(added.prd_id, 'eeeeeeee5555');
  assert.equal(joinOf(f).on_complete, added.id);
  forkOf(f).fork_members.forEach(id => assert.ok(!byId(f, id).on_complete));
  assert.ok(!forkOf(f).on_complete);
});

test('a nested Automation added after a fork follows the join', async () => {
  const sb = await formWithFork();
  sb.automationFormSetNestedChild('chi00001');
  assert.equal(sb.automationFormApplyNested(), true);
  const f = plain(formState(sb));
  const child = f.members.find(m => m.kind === 'automation');
  assert.equal(joinOf(f).on_complete, child.id);
  assert.equal(f.members[f.members.length - 1].id, child.id);
  assert.match(sb.__els.members.innerHTML, /Nightly chain/);
});

test('a rejected nested Automation shows the message and changes nothing', async () => {
  const sb = await openNew();
  sb.automationFormPick('aaaaaaaa1111');
  sb.automationFormAdd();
  sb.automationFormOpenPanel('nested');
  sb.automationFormSetNestedChild('taken001');
  assert.equal(sb.automationFormApplyNested(), false);
  assert.equal(sb.__els.error.innerHTML, 'That Automation is already inside another Automation');
  assert.equal(plain(formState(sb)).members.length, 1);
});

test('applying a fork with fewer than two branches shows the message', async () => {
  const sb = await openNew();
  sb.automationFormOpenPanel('fork');
  sb.automationFormSetForkBranches(['cccccccc3333']);
  assert.equal(sb.automationFormApplyFork(), false);
  assert.equal(sb.__els.error.innerHTML, 'A fork needs at least two branches');
  assert.equal(plain(formState(sb)).members.length, 0);
});

test('the new locale keys exist in all five bundles with real translations', () => {
  const keys = ['automation_add_fork', 'automation_add_nested', 'automation_join_failure_label', 'automation_fork_min_branches',
    'automation_nested_self', 'automation_nested_owned', 'automation_remove_group_confirm'];
  for (const lang of ['de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    for (const key of keys) {
      assert.ok(bundle[key], lang + ' is missing ' + key);
      assert.notEqual(bundle[key], EN[key], lang + ' still has English for ' + key);
    }
  }
});
