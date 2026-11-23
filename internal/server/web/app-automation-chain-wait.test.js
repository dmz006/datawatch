// Automation tab — fork branch chains, waits, and the run-shape controls.
//
// Covers the strip drawing a fork branch as a chain of members joined at the
// end, a decision wait member shown as "waiting for you" (never blocked) with
// a link to its question, the "waiting for a free slot" and "waiting for stage"
// states, the strip header chips, the Inbox row and alert card for a decision
// wait, the timeline wait intervals, and the form controls: max parallel
// (Automation and fork), the stage gate, a decision wait member with its
// question, and growing a branch into a chain. fetch is stubbed; rendered text
// is the real English bundle, and the other four bundles carry the same keys.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const EN = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
const plain = value => JSON.parse(JSON.stringify(value));

const PRDS = {
  prds: [
    { id: 'aaaaaaaa1111', title: 'Build the base', status: 'approved' },
    { id: 'bbbbbbbb2222', title: 'Run the checks', status: 'approved' },
    { id: 'cccccccc3333', title: 'Ship it', status: 'approved' },
    { id: 'dddddddd4444', title: 'Write the notes', status: 'approved' },
    { id: 'eeeeeeee5555', title: 'Publish', status: 'approved' },
  ],
};

function loadApp(opts) {
  const o = opts || {};
  const calls = [];
  const modals = [];
  const sandbox = buildSandbox({ byId: o.byId || {} });
  const routes = o.routes || {};
  sandbox.fetch = (url, init) => {
    const method = (init && init.method) || 'GET';
    calls.push({ url, method, body: init && init.body ? JSON.parse(init.body) : undefined });
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
  sandbox._i18n.bundle = JSON.parse(JSON.stringify(EN));
  sandbox.document.createElement = () => {
    const el = makeStubElement();
    el.removed = false;
    el.remove = () => { el.removed = true; };
    modals.push(el);
    return el;
  };
  sandbox.__calls = calls;
  sandbox.__modals = modals;
  sandbox.showToast = () => {};
  return sandbox;
}

const formState = sb => vm.runInContext('_automationForm', sb);

function member(id, status, extra) {
  return Object.assign({ id, prd_id: id + 'aaaaaaaaaaaa', status, added_by: 'operator' }, extra || {});
}

// card returns the opening tag of the member card whose data-member-id is id.
function card(html, id) {
  const m = html.match(new RegExp('<div class="automation-member [^>]*data-member-id="' + id + '"[^>]*>'));
  assert.ok(m, 'card ' + id + ' is drawn');
  return m[0];
}

// cardBlock returns the markup of one member card, up to the next card.
function cardBlock(html, id) {
  const start = html.indexOf('data-member-id="' + id + '"');
  assert.ok(start >= 0, 'card ' + id + ' is drawn');
  const next = html.indexOf('<div class="automation-member ', start);
  return html.slice(start, next < 0 ? html.length : next);
}

// A fork over two branch chains: a1 -> a2 -> a3 and b1 (one member), joined at
// the end; the join names the tails a3 and b1.
function chainedFork(extra) {
  return Object.assign({
    id: 'a1b2c3d4', name: 'Chained', status: 'active', entry_member_id: 'f',
    active_cursors: ['a1', 'b1'],
    members: [
      member('f', 'completed', { kind: 'fork', prd_id: undefined, fork_members: ['a1', 'b1'] }),
      member('a1', 'running', { on_complete: 'a2' }),
      member('a2', 'pending', { on_complete: 'a3' }),
      member('a3', 'pending'),
      member('b1', 'running'),
      member('j', 'running', { kind: 'join', prd_id: undefined, join_members: ['a3', 'b1'], on_complete: 'z' }),
      member('z', 'pending'),
    ],
  }, extra || {});
}

// ── Branch chains on the strip ───────────────────────────────────────────────

test('a fork branch is drawn as a chain of members, in order, joined by connectors', () => {
  const sb = loadApp();
  const html = sb.buildAutomationStripHTML(chainedFork(), {});
  const lane = html.match(/<div class="automation-lane" data-lane="a1"[^>]*>/)[0];
  assert.match(lane, /data-chain="true"/);
  assert.match(lane, /data-chain-length="3"/);
  assert.match(lane, /data-tail="a3"/);
  const start = html.indexOf(lane);
  const end = html.indexOf('data-lane="b1"');
  const body = html.slice(start, end);
  const order = [...body.matchAll(/data-member-id="(a\d)"/g)].map(m => m[1]);
  assert.deepEqual(order, ['a1', 'a2', 'a3']);
  const links = [...body.matchAll(/<span class="automation-connector[^>]*data-edge="on_complete"[^>]*data-from="(\w+)"[^>]*data-to="(\w+)"/g)].map(m => m[1] + '>' + m[2]);
  assert.deepEqual(links, ['a1>a2', 'a2>a3']);
});

test('a branch of one member keeps the plain lane, and every member is drawn once', () => {
  const sb = loadApp();
  const html = sb.buildAutomationStripHTML(chainedFork(), {});
  const single = html.match(/<div class="automation-lane" data-lane="b1"[^>]*>/)[0];
  assert.doesNotMatch(single, /data-chain/);
  ['a1', 'a2', 'a3', 'b1'].forEach(id => {
    assert.equal((html.match(new RegExp('<div class="automation-member [^>]*data-member-id="' + id + '"', 'g')) || []).length, 1, id + ' is drawn once');
  });
  assert.doesNotMatch(html, /data-row="unlinked"/, 'no chain member is left unlinked');
});

test('the join is drawn after the group and leads on to what it led to', () => {
  const sb = loadApp();
  const html = sb.buildAutomationStripHTML(chainedFork(), {});
  assert.match(html, /data-fork-id="f" data-join-id="j"/);
  assert.ok(html.indexOf('data-fork-id="f"') < html.indexOf('data-member-id="j"'), 'the join follows the branches');
  assert.ok(html.indexOf('data-member-id="j"') < html.indexOf('data-member-id="z"'), 'the chain goes on after the join');
  assert.match(html, /data-edge="join"/);
});

test('a chain member that is a decision wait sits in its lane too', () => {
  const sb = loadApp();
  const a = chainedFork();
  a.members[1] = member('a1', 'completed', { on_complete: 'a2' });
  a.members[2] = member('a2', 'running', { kind: 'decision_wait', prd_id: undefined, question: 'Ship?', on_complete: 'a3', waiting_for_operator: true, question_id: 'q-1' });
  const html = sb.buildAutomationStripHTML(a, {});
  const lane = html.slice(html.indexOf('data-lane="a1"'), html.indexOf('data-lane="b1"'));
  assert.match(lane, /data-member-id="a2"[^>]*data-kind="decision_wait"/);
});

test('the fork card shows its own max parallel', () => {
  const sb = loadApp();
  const a = chainedFork();
  a.members[0].max_parallel = 2;
  const html = sb.buildAutomationStripHTML(a, {});
  assert.match(card(html, 'f'), /data-max-parallel="2"/);
  assert.match(cardBlock(html, 'f'), /Max parallel 2/);
});

test('a loop in a chain does not repeat a member', () => {
  const sb = loadApp();
  const a = chainedFork();
  a.members[3] = member('a3', 'pending', { on_complete: 'a1' });
  const html = sb.buildAutomationStripHTML(a, {});
  assert.equal((html.match(/<div class="automation-member [^>]*data-member-id="a1"/g) || []).length, 1);
});

test('the join wait counts a chain as done when its tail is done', () => {
  const sb = loadApp();
  const a = chainedFork();
  a.members[1] = member('a1', 'completed', { on_complete: 'a2' });
  a.members[2] = member('a2', 'running', { on_complete: 'a3' });
  a.members[4] = member('b1', 'completed');
  const wait = vm.runInContext('_automationJoinWait', sb)(a);
  assert.deepEqual(plain(wait), { done: 1, total: 2 });
  // A chain whose head is running counts as started even while its tail waits.
  const early = chainedFork();
  early.members[4] = member('b1', 'pending');
  assert.deepEqual(plain(vm.runInContext('_automationJoinWait', sb)(early)), { done: 0, total: 2 });
});

// ── Waits ────────────────────────────────────────────────────────────────────

function decisionWaitAutomation() {
  return {
    id: 'a1b2c3d4', name: 'Waiting', status: 'active', entry_member_id: 'f', active_cursors: ['a1', 'd1'],
    members: [
      member('f', 'completed', { kind: 'fork', prd_id: undefined, fork_members: ['a1', 'd1'] }),
      member('a1', 'running'),
      member('d1', 'running', { kind: 'decision_wait', prd_id: undefined, question: 'Is the build good enough?', waiting_for_operator: true, question_id: 'q-abc12' }),
      member('j', 'running', { kind: 'join', prd_id: undefined, join_members: ['a1', 'd1'] }),
    ],
  };
}

test('a decision wait member that waits for the operator shows "Waiting for you", not blocked', () => {
  const sb = loadApp();
  const html = sb.buildAutomationStripHTML(decisionWaitAutomation(), {});
  const tag = card(html, 'd1');
  assert.match(tag, /data-wait="operator"/);
  assert.match(tag, /data-status="running"/, 'the status stays running');
  assert.match(tag, /automation-member-waiting-operator/);
  assert.doesNotMatch(tag, /automation-member-blocked/);
  const block = cardBlock(html, 'd1');
  assert.match(block, /<span class="automation-member-status">Waiting for you<\/span>/);
  assert.doesNotMatch(block, />Blocked</);
  assert.match(block, /Decision wait/);
  assert.match(block, /Is the build good enough\?/);
  assert.match(block, /Only this branch waits; the Automation is not blocked\./);
});

test('a waiting decision wait links to its question, and the link opens it', () => {
  const sb = loadApp();
  const opened = [];
  sb.openQuestion = id => { opened.push(id); };
  const html = sb.buildAutomationStripHTML(decisionWaitAutomation(), {});
  const btn = html.match(/<button type="button" class="automation-wait-answer-btn" data-answer-question="q-abc12" onclick="([^"]*)">Answer the question<\/button>/);
  assert.ok(btn, 'the answer link is drawn');
  assert.match(btn[1], /window\.openQuestion\('q-abc12'\)/);
  // The inline handler hands the id to window.openQuestion.
  sb.window.openQuestion = sb.openQuestion;
  vm.runInContext(btn[1].replace(/&#39;/g, "'").replace(/event\.stopPropagation\(\);/, ''), sb);
  assert.deepEqual(opened, ['q-abc12']);
});

test('a decision wait with no question linked yet has no answer link', () => {
  const sb = loadApp();
  const a = decisionWaitAutomation();
  a.members[2].question_id = '';
  const html = sb.buildAutomationStripHTML(a, {});
  assert.match(card(html, 'd1'), /data-wait="operator"/);
  assert.doesNotMatch(html, /automation-wait-answer-btn/);
});

test('a decision wait that is not waiting any more shows its plain status', () => {
  const sb = loadApp();
  const a = decisionWaitAutomation();
  a.members[2] = member('d1', 'completed', { kind: 'decision_wait', prd_id: undefined, question: 'Q?', question_id: 'q-abc12', waiting_for_operator: true });
  const html = sb.buildAutomationStripHTML(a, {});
  assert.doesNotMatch(card(html, 'd1'), /data-wait/);
  assert.match(cardBlock(html, 'd1'), />Completed</);
});

test('a member held for a free slot shows "Waiting for a free slot"', () => {
  const sb = loadApp();
  const a = chainedFork();
  a.members[1] = member('a1', 'running', { on_complete: 'a2' });
  a.members[4] = member('b1', 'pending', { waiting_for_slot: true });
  const html = sb.buildAutomationStripHTML(a, {});
  assert.match(card(html, 'b1'), /data-wait="slot"/);
  assert.match(cardBlock(html, 'b1'), /Waiting for a free slot/);
  assert.doesNotMatch(card(html, 'b1'), /automation-member-blocked/);
});

test('a member held by the stage gate shows the stage it waits for', () => {
  const sb = loadApp();
  const a = chainedFork();
  a.members[4] = member('b1', 'pending', { stage: 'review', waiting_for_stage: 'build' });
  const html = sb.buildAutomationStripHTML(a, {});
  assert.match(card(html, 'b1'), /data-wait="stage"/);
  assert.match(cardBlock(html, 'b1'), /Waiting for stage build/);
});

test('the stage wait wins over the slot wait, and a started member is never waiting', () => {
  const sb = loadApp();
  const wait = sb.automationMemberWait;
  assert.deepEqual(plain(wait(member('x', 'pending', { waiting_for_slot: true, waiting_for_stage: 'build' }))), { kind: 'stage', stage: 'build' });
  assert.equal(wait(member('x', 'running', { waiting_for_slot: true })), null);
  assert.equal(wait(member('x', 'completed', { waiting_for_stage: 'build' })), null);
  assert.equal(wait(member('x', 'running', { waiting_for_operator: true })), null, 'only a decision wait waits for the operator');
  assert.equal(wait(member('x', 'pending', { kind: 'join', waiting_for_slot: true })), null);
  assert.equal(wait(null), null);
  assert.equal(wait(member('x', '', { waiting_for_slot: true })).kind, 'slot', 'an unset status has not started');
});

test('the strip header counts each kind of wait and shows the two run-shape settings', () => {
  const sb = loadApp();
  const a = decisionWaitAutomation();
  a.max_parallel = 3;
  a.stage_gate = true;
  a.members.push(member('s1', 'pending', { waiting_for_slot: true }));
  a.members.push(member('s2', 'pending', { waiting_for_slot: true }));
  a.members.push(member('t1', 'pending', { stage: 'x', waiting_for_stage: 'build' }));
  const html = sb.buildAutomationStripHTML(a, {});
  const strip = html.match(/<div class="automation-wait-strip"[^>]*>.*?<\/div>/)[0];
  assert.match(strip, /data-wait-count="operator">1 waiting for you</);
  assert.match(strip, /data-wait-count="slot">2 waiting for a free slot</);
  assert.match(strip, /data-wait-count="stage">1 waiting for an earlier stage</);
  assert.match(strip, /data-max-parallel="3">Max parallel 3</);
  assert.match(strip, /data-stage-gate="true">Stage gate on</);
});

test('the strip header is absent for an Automation with no wait and no setting', () => {
  const sb = loadApp();
  const html = sb.buildAutomationStripHTML(chainedFork(), {});
  assert.doesNotMatch(html, /automation-wait-strip/);
});

test('the Automation tile names a member waiting for you and links to its question', () => {
  const sb = loadApp();
  const html = sb.buildAutomationCardHTML(decisionWaitAutomation(), { prdTitles: {} });
  assert.match(html, /<div class="automation-card-wait" data-wait="operator"[^>]*>Waiting for you<button/);
  assert.match(html, /data-answer-question="q-abc12"/);
  const none = sb.buildAutomationCardHTML(chainedFork(), { prdTitles: {} });
  assert.doesNotMatch(none, /automation-card-wait/);
});

test('a decision wait does not count toward the member progress', () => {
  const sb = loadApp();
  const a = decisionWaitAutomation();
  assert.deepEqual(plain(sb.automationMemberProgress(a)), { done: 0, total: 1 });
});

test('the member drawer shows the wait, the question and a fork limit', () => {
  const sb = loadApp();
  const dw = sb.buildAutomationMemberDrawerHTML(decisionWaitAutomation().members[2], {});
  assert.match(dw, /Waiting for you/);
  assert.match(dw, /Question to ask you/);
  assert.match(dw, /Is the build good enough\?/);
  assert.match(dw, /Decision wait/);
  const fk = sb.buildAutomationMemberDrawerHTML(member('f', 'running', { kind: 'fork', prd_id: undefined, max_parallel: 4 }), {});
  assert.match(fk, /Max parallel branches \(0 = no limit\)/);
  assert.match(fk, /<dd>4<\/dd>/);
});

// ── Inbox, alert and timeline ────────────────────────────────────────────────

test('the Inbox row of a decision wait question says waiting for you and keeps the answer controls', () => {
  const sb = loadApp();
  const html = sb.questionsCardHtml({ questions: [
    { id: 'q-abc12', label: 'decision', text: 'Is the build good enough?', asked_by: 'decision-wait', options: [{ id: 'continue', label: 'Continue' }] },
    { id: 'q-other', text: 'Another question', asked_by: 'assistant' },
  ] });
  const rows = html.split('<div class="questions-queue-row"').slice(1);
  assert.match(rows[0], /data-decision-wait="true"/);
  assert.match(rows[0], /data-waiting-for-you="true"[^>]*>Waiting for you</);
  assert.match(rows[0], /An Automation member is waiting for your answer\. Only its branch is held; the Automation is not blocked\./);
  assert.match(rows[0], /questions-option-btn/);
  assert.match(rows[0], /questions-open-btn/);
  assert.doesNotMatch(rows[1], /data-decision-wait|waiting-for-you|questions-waiting-note/);
});

const WAIT_ALERT = {
  id: 'al1', level: 'warn', title: 'Automation waiting for you', created_at: new Date().toISOString(),
  body: 'Automation "Chained" (a1b2c3d4): member d1 is waiting for your answer to question q-abc12: Is the build good enough?\nOnly this branch waits; the Automation is not blocked and its other branches keep running.',
};

test('the alert of a decision wait finds its question id and gets an answer link', () => {
  const sb = loadApp();
  assert.equal(sb._alertDecisionWaitQuestionId(WAIT_ALERT), 'q-abc12');
  assert.equal(sb._alertDecisionWaitQuestionId({ title: 'Automation blocked', body: 'to question q-1: x' }), '');
  assert.equal(sb._alertDecisionWaitQuestionId({ title: 'Automation waiting for you', body: 'no id here' }), '');
  assert.equal(sb._alertDecisionWaitQuestionId(null), '');
  const html = sb.renderAlertCard(WAIT_ALERT, 'unknown', true, []);
  assert.match(html, /data-decision-wait="true"/);
  assert.match(html, /data-answer-question="q-abc12"[^>]*>Answer the question</);
  assert.match(html, /Automation waiting for you/);
  const plainAlert = sb.renderAlertCard({ id: 'al2', level: 'info', title: 'Done', body: 'ok', created_at: WAIT_ALERT.created_at }, 'unknown', true, []);
  assert.doesNotMatch(plainAlert, /automation-wait-answer-btn/);
});

test('a waiting alert is counted as needing the operator, not as an error', () => {
  const sb = loadApp();
  assert.equal(sb._alertCategory(WAIT_ALERT, 'unknown'), 'prompt');
});

test('timeline waiting intervals are classified and shown in plain words', () => {
  const sb = loadApp();
  const now = Date.now();
  const iso = ms => new Date(ms).toISOString();
  const mk = reason => ({
    id: 'a', title: 'A', status: 'running', start: iso(now - 4000), end: null, projected_finish: null,
    stages: [{ id: 's', title: 'S', status: 'running', start: iso(now - 4000), end: null, members: [
      { id: 'm', title: 'M', status: 'running', start: iso(now - 4000), end: null, intervals: [{ kind: 'waiting', start: iso(now - 3000), end: null, reason }] },
    ] }],
  });
  const draw = reason => sb.timelineGanttSvg(mk(reason), { today: iso(now) });
  const operator = draw('waiting for you');
  assert.match(operator, /data-timeline-interval="waiting" data-timeline-wait="operator"/);
  assert.match(operator, /waiting — Waiting for you/);
  const slot = draw('waiting for a free slot');
  assert.match(slot, /data-timeline-wait="slot"/);
  assert.match(slot, /waiting — Waiting for a free slot/);
  const stage = draw('waiting for stage build');
  assert.match(stage, /data-timeline-wait="stage"/);
  assert.match(stage, /waiting — Waiting for stage build/);
  const other = draw('queued');
  assert.doesNotMatch(other, /data-timeline-wait/);
  assert.match(other, /waiting — queued/);
});

// ── Form: run shape, decision wait, chains ───────────────────────────────────

function formEnv(extraIds) {
  const els = { members: { innerHTML: '' }, panel: { innerHTML: '' }, error: { innerHTML: '' } };
  const byId = Object.assign({
    view: makeStubElement(),
    automationFormMembers: els.members,
    automationFormPanel: els.panel,
    automationFormError: els.error,
    automationFormPicker: { innerHTML: '' },
    automationFormName: { value: 'Chain' },
    automationFormDescription: { value: '' },
    automationFormReason: { value: 'why' },
    automationFormSaveBtn: { disabled: false },
    automationFormMaxParallel: { value: '0' },
    automationFormStageGate: { checked: false },
  }, extraIds || {});
  const routes = { 'GET /api/autonomous/prds': { body: PRDS }, 'GET /api/automations': { body: [] } };
  const sb = loadApp({ byId, routes });
  sb.__els = els;
  sb.__ids = byId;
  return sb;
}

async function openNew(extraIds) {
  const sb = formEnv(extraIds);
  sb.openAutomationForm(null);
  await flushAsync();
  return sb;
}

function twoMembers() {
  return {
    name: 'Chain', entryMemberId: 'm1',
    members: [
      { id: 'm1', prd_id: 'aaaaaaaa1111', on_complete: 'm2', on_blocked: '', on_failed: '' },
      { id: 'm2', prd_id: 'bbbbbbbb2222', on_complete: '', on_blocked: '', on_failed: '' },
    ],
  };
}

test('the form has a Run shape section with the max parallel field and the stage gate box', async () => {
  const sb = await openNew();
  const html = sb.__ids.view.innerHTML;
  assert.match(html, /id="automationFormRunShape"/);
  assert.match(html, />Run shape</);
  assert.match(html, /id="automationFormMaxParallel"[^>]*value="0"/);
  assert.match(html, /Max parallel members/);
  assert.match(html, /The most members that run at once\. 0 means no limit\./);
  assert.match(html, /<input type="checkbox" id="automationFormStageGate">/);
  assert.match(html, /Hold a stage until the earlier stages finish/);
  assert.match(html, /Off, a stage is only a label\./);
});

test('editing an Automation with the settings on opens the section filled in', async () => {
  const sb = formEnv();
  sb.openAutomationForm({ id: 'a1b2c3d4', name: 'Chain', status: 'draft', version: 2, entry_member_id: 'm1', max_parallel: 3, stage_gate: true, members: [member('m1', 'pending')] });
  await flushAsync();
  const html = sb.__ids.view.innerHTML;
  assert.match(html, /<details id="automationFormRunShape" class="automation-form-collapsible" open/);
  assert.match(html, /id="automationFormMaxParallel"[^>]*value="3"/);
  assert.match(html, /id="automationFormStageGate" checked>/);
});

test('an untouched form sends neither max_parallel nor stage_gate', () => {
  const sb = loadApp();
  const body = plain(sb.buildAutomationFormPayload({ name: 'n', members: twoMembers().members }));
  assert.equal('max_parallel' in body, false);
  assert.equal('stage_gate' in body, false);
});

test('the payload carries max_parallel and stage_gate once they are set, even to switch them off', () => {
  const sb = loadApp();
  const on = plain(sb.buildAutomationFormPayload({ name: 'n', members: twoMembers().members, maxParallel: 3, stageGate: true }));
  assert.equal(on.max_parallel, 3);
  assert.equal(on.stage_gate, true);
  const off = plain(sb.buildAutomationFormPayload({ name: 'n', members: twoMembers().members, maxParallel: 0, stageGate: false }));
  assert.equal(off.max_parallel, 0);
  assert.equal(off.stage_gate, false);
  const locked = plain(sb.buildAutomationFormPayload({ name: 'n', membersLocked: true, maxParallel: 2, stageGate: true, expectedVersion: 4, reason: 'r' }));
  assert.equal(locked.max_parallel, 2);
  assert.equal('members' in locked, false);
});

test('saving a touched run shape posts max_parallel and stage_gate', async () => {
  const sb = await openNew();
  sb.automationFormPick('aaaaaaaa1111');
  sb.automationFormAdd();
  sb.__ids.automationFormMaxParallel.value = '2';
  sb.__ids.automationFormStageGate.checked = true;
  sb.automationFormRunShapeChanged();
  sb.saveAutomationForm();
  await flushAsync();
  const post = sb.__calls.find(c => c.method === 'POST' && c.url === '/api/automations');
  assert.ok(post, 'the create request was sent');
  assert.equal(post.body.max_parallel, 2);
  assert.equal(post.body.stage_gate, true);
});

test('saving without touching the run shape leaves both out', async () => {
  const sb = await openNew();
  sb.automationFormPick('aaaaaaaa1111');
  sb.automationFormAdd();
  sb.saveAutomationForm();
  await flushAsync();
  const post = sb.__calls.find(c => c.method === 'POST' && c.url === '/api/automations');
  assert.ok(post);
  assert.equal('max_parallel' in post.body, false);
  assert.equal('stage_gate' in post.body, false);
});

test('a max parallel that is not a whole number stops the save and says so', async () => {
  const sb = await openNew();
  sb.automationFormPick('aaaaaaaa1111');
  sb.automationFormAdd();
  sb.automationFormRunShapeChanged();
  ['-1', '1.5', 'abc'].forEach(bad => {
    sb.__ids.automationFormMaxParallel.value = bad;
    assert.deepEqual(plain(sb.automationReadRunShape()), { error: 'automation_form_max_parallel_invalid' });
  });
  sb.__ids.automationFormMaxParallel.value = '-1';
  await sb.saveAutomationForm();
  assert.match(sb.__els.error.innerHTML, /Max parallel must be a whole number, 0 or more/);
  assert.equal(sb.__calls.filter(c => c.method === 'POST').length, 0);
  sb.__ids.automationFormMaxParallel.value = '';
  assert.equal(sb.automationReadRunShape().maxParallel, 0, 'an empty field means no limit');
});

// A fork with a one-member branch and a three-member chain, built through the
// pure add function.
function forkWithChain(sb) {
  return plain(sb.automationAddForkJoin(twoMembers(), [['cccccccc3333', 'dddddddd4444', 'eeeeeeee5555'], 'aaaaaaaa1111'], 'all_must_succeed', 'm1', { maxParallel: 2 }));
}

test('a fork can be added with a chain for a branch: heads on the fork, tails on the join', () => {
  const sb = loadApp();
  const out = forkWithChain(sb);
  const fork = out.members.find(m => m.kind === 'fork');
  const join = out.members.find(m => m.kind === 'join');
  const byId = id => out.members.find(m => m.id === id);
  assert.equal(fork.fork_members.length, 2);
  const head = byId(fork.fork_members[0]);
  const second = byId(head.on_complete);
  const tail = byId(second.on_complete);
  assert.deepEqual([head, second, tail].map(m => m.prd_id), ['cccccccc3333', 'dddddddd4444', 'eeeeeeee5555']);
  assert.equal(tail.on_complete, '');
  const single = byId(fork.fork_members[1]);
  assert.deepEqual(join.join_members, [tail.id, single.id]);
  assert.equal(fork.max_parallel, 2);
  assert.equal(new Set(out.members.map(m => m.id)).size, out.members.length);
});

test('a plain list of ids is still one member per branch, and one chain is not a fork', () => {
  const sb = loadApp();
  const out = plain(sb.automationAddForkJoin(twoMembers(), ['cccccccc3333', 'dddddddd4444'], 'all_must_succeed', 'm1'));
  const fork = out.members.find(m => m.kind === 'fork');
  const join = out.members.find(m => m.kind === 'join');
  assert.deepEqual(join.join_members, fork.fork_members);
  assert.equal('max_parallel' in fork, false);
  assert.throws(() => sb.automationAddForkJoin(twoMembers(), [['cccccccc3333', 'dddddddd4444']], 'all_must_succeed', 'm1'), /at least two branches/);
  assert.throws(() => sb.automationAddForkJoin(twoMembers(), [['cccccccc3333'], ['cccccccc3333']], 'all_must_succeed', 'm1'), /at least two branches/, 'a repeated Automaton is one branch');
});

test('the payload keeps on_complete on every chain member and nothing else on a branch', () => {
  const sb = loadApp();
  const out = forkWithChain(sb);
  const body = plain(sb.buildAutomationFormPayload(Object.assign({}, out, { name: 'n' })));
  const fork = body.members.find(m => m.kind === 'fork');
  const join = body.members.find(m => m.kind === 'join');
  const byId = id => body.members.find(m => m.id === id);
  const head = byId(fork.fork_members[0]);
  assert.ok(head.on_complete, 'the head leads to the next chain member');
  assert.ok(byId(head.on_complete).on_complete, 'the second member leads to the tail');
  body.members.filter(m => m.kind !== 'join' && m.kind !== 'fork' && m.id !== 'm1' && m.id !== 'm2').forEach(m => {
    assert.equal('on_blocked' in m, false, m.id + ' has no on_blocked');
    assert.equal('on_failed' in m, false, m.id + ' has no on_failed');
  });
  ['on_complete', 'on_blocked', 'on_failed'].forEach(e => assert.equal(e in fork, false, 'the fork has no ' + e));
  assert.equal(fork.max_parallel, 2);
  assert.equal(join.on_complete, 'm2');
});

test('max_parallel is sent for a fork only, and never as 0', () => {
  const sb = loadApp();
  const members = [
    { id: 'm1', prd_id: 'aaaaaaaa1111', max_parallel: 5, on_complete: '' },
  ];
  const plainBody = plain(sb.buildAutomationFormPayload({ name: 'n', members }));
  assert.equal('max_parallel' in plainBody.members[0], false, 'a plain member never carries it');
  const out = forkWithChain(sb);
  out.members.find(m => m.kind === 'fork').max_parallel = 0;
  const body = plain(sb.buildAutomationFormPayload(Object.assign({}, out, { name: 'n' })));
  assert.equal('max_parallel' in body.members.find(m => m.kind === 'fork'), false);
});

test('the form draws a branch chain as rows in order under its fork, with the fork limit field', async () => {
  const sb = await openNew();
  sb.automationFormPick('aaaaaaaa1111');
  sb.automationFormAdd();
  sb.automationFormPick('bbbbbbbb2222');
  sb.automationFormAdd();
  sb.automationFormOpenPanel('fork');
  sb.automationFormSetForkBranches(['cccccccc3333', 'dddddddd4444']);
  assert.equal(sb.automationFormApplyFork(), true);
  const fork = formState(sb).members.find(m => m.kind === 'fork');
  const head = fork.fork_members[0];
  sb.automationFormPick('eeeeeeee5555');
  assert.equal(sb.automationFormExtendBranch(head), true);
  const html = sb.__els.members.innerHTML;
  const f = plain(formState(sb));
  const first = f.members.find(m => m.id === head);
  assert.equal(f.members.find(m => m.id === first.on_complete).prd_id, 'eeeeeeee5555');
  const group = html.slice(html.indexOf('data-fork-id="' + fork.id + '"'));
  const rows = [...group.matchAll(/automation-form-branch-row" data-member-id="(\w+)"/g)].map(m => m[1]);
  assert.deepEqual(rows.slice(0, 2), [head, first.on_complete], 'the chain is drawn in order');
  assert.match(group, new RegExp('data-max-parallel-for="' + fork.id + '"'));
  assert.match(group, /data-op="extend_branch"/);
  assert.match(group, /data-op="remove_branch_member"/);
});

test('extending a branch moves the join to the new tail and keeps the order of the rest', async () => {
  const sb = await openNew();
  sb.automationFormOpenPanel('fork');
  sb.automationFormSetForkBranches(['cccccccc3333', 'dddddddd4444']);
  sb.automationFormApplyFork();
  const before = plain(formState(sb));
  const fork = before.members.find(m => m.kind === 'fork');
  const join = before.members.find(m => m.kind === 'join');
  const head = fork.fork_members[0];
  sb.automationFormPick('eeeeeeee5555');
  assert.equal(sb.automationFormExtendBranch(head), true);
  const after = plain(formState(sb));
  const newId = after.members.find(m => m.id === head).on_complete;
  assert.ok(newId);
  const newJoin = after.members.find(m => m.kind === 'join');
  assert.deepEqual(newJoin.join_members, [newId, join.join_members[1]]);
  assert.deepEqual(after.members.find(m => m.kind === 'fork').fork_members, fork.fork_members, 'the head is unchanged');
  // Extending the new tail again moves the join once more.
  sb.automationFormPick('cccccccc3333');
  assert.equal(sb.automationFormExtendBranch(newId), false, 'an Automaton already in the form is refused');
  assert.match(sb.__els.error.innerHTML, /Choose an Automaton/);
});

test('extending a branch needs a picked, available Automaton and a branch member', async () => {
  const sb = await openNew();
  sb.automationFormOpenPanel('fork');
  sb.automationFormSetForkBranches(['cccccccc3333', 'dddddddd4444']);
  sb.automationFormApplyFork();
  const head = formState(sb).members.find(m => m.kind === 'fork').fork_members[0];
  assert.equal(sb.automationFormExtendBranch(head), false, 'nothing is picked');
  sb.automationFormPick('eeeeeeee5555');
  assert.equal(sb.automationFormExtendBranch('nope'), false);
  assert.equal(sb.automationFormExtendBranch(formState(sb).members.find(m => m.kind === 'join').id), false, 'a join is not on a branch');
});

test('removing a member from a chain relinks the head, the middle and the tail', async () => {
  const sb = await openNew();
  sb.automationFormOpenPanel('fork');
  sb.automationFormSetForkBranches(['cccccccc3333', 'dddddddd4444']);
  sb.automationFormApplyFork();
  const fork0 = plain(formState(sb)).members.find(m => m.kind === 'fork');
  const head = fork0.fork_members[0];
  sb.automationFormPick('eeeeeeee5555');
  sb.automationFormExtendBranch(head);
  const second = plain(formState(sb)).members.find(m => m.id === head).on_complete;
  sb.automationFormPick('bbbbbbbb2222');
  sb.automationFormExtendBranch(second);
  const third = plain(formState(sb)).members.find(m => m.id === second).on_complete;
  const chainOf = () => {
    const s = plain(formState(sb));
    const forkNow = s.members.find(m => m.kind === 'fork');
    const walk = h => { const out = []; for (let id = h; id; id = s.members.find(m => m.id === id).on_complete) out.push(id); return out; };
    return { s, chain: walk(forkNow.fork_members[0]), join: s.members.find(m => m.kind === 'join') };
  };
  assert.deepEqual(chainOf().chain, [head, second, third]);

  // The middle member goes: head leads to the tail.
  assert.equal(sb.automationFormRemoveBranchMember(second), true);
  assert.deepEqual(chainOf().chain, [head, third]);
  assert.equal(chainOf().join.join_members[0], third);

  // The tail goes: the head is the tail again and the join names it.
  assert.equal(sb.automationFormRemoveBranchMember(third), true);
  assert.deepEqual(chainOf().chain, [head]);
  assert.equal(chainOf().join.join_members[0], head);

  // The only member of a branch cannot be removed.
  assert.equal(sb.automationFormRemoveBranchMember(head), false);
  assert.equal(sb.automationFormRemoveBranchMember('nope'), false);
});

test('removing the head of a chain hands the head role to the next member', async () => {
  const sb = await openNew();
  sb.automationFormOpenPanel('fork');
  sb.automationFormSetForkBranches(['cccccccc3333', 'dddddddd4444']);
  sb.automationFormApplyFork();
  const head = plain(formState(sb)).members.find(m => m.kind === 'fork').fork_members[0];
  sb.automationFormPick('eeeeeeee5555');
  sb.automationFormExtendBranch(head);
  const next = plain(formState(sb)).members.find(m => m.id === head).on_complete;
  assert.equal(sb.automationFormRemoveBranchMember(head), true);
  const s = plain(formState(sb));
  assert.equal(s.members.find(m => m.kind === 'fork').fork_members[0], next);
  assert.equal(s.members.some(m => m.id === head), false);
});

test('removing the whole fork group drops every member of every chain', () => {
  const sb = loadApp();
  const out = forkWithChain(sb);
  const fork = out.members.find(m => m.kind === 'fork');
  const next = plain(sb.automationRemoveForkGroup(out, fork.id));
  assert.deepEqual(next.members.map(m => m.id), ['m1', 'm2']);
  assert.equal(next.members[0].on_complete, 'm2');
});

test('a fork limit typed on the row is recorded and a bad value clears it', async () => {
  const sb = await openNew();
  sb.automationFormOpenPanel('fork');
  sb.automationFormSetForkBranches(['cccccccc3333', 'dddddddd4444']);
  sb.automationFormApplyFork();
  const fork = formState(sb).members.find(m => m.kind === 'fork');
  sb.automationFormSetForkMaxParallel(fork.id, '3');
  assert.equal(formState(sb).members.find(m => m.kind === 'fork').max_parallel, 3);
  sb.automationFormSetForkMaxParallel(fork.id, 'abc');
  assert.equal(formState(sb).members.find(m => m.kind === 'fork').max_parallel, 0);
  sb.automationFormSetForkMaxParallel(fork.id, '2.9');
  assert.equal(formState(sb).members.find(m => m.kind === 'fork').max_parallel, 2);
  sb.automationFormSetForkMaxParallel('nope', '4');
  const head = formState(sb).members.find(m => m.id === fork.fork_members[0]);
  sb.automationFormSetForkMaxParallel(head.id, '4');
  assert.equal('max_parallel' in formState(sb).members.find(m => m.id === head.id), false, 'only a fork takes a limit');
});

// ── Form: decision wait ──────────────────────────────────────────────────────

test('the form offers an Add a decision wait button and a panel with the question box', async () => {
  const sb = await openNew();
  assert.match(sb.__ids.view.innerHTML, /data-op="add_decision_wait"[^>]*>Add a decision wait</);
  sb.automationFormOpenPanel('decision');
  const html = sb.__els.panel.innerHTML;
  assert.match(html, /data-panel="decision"/);
  assert.match(html, /<textarea id="automationDecisionQuestion"/);
  assert.match(html, /Question to ask you/);
  assert.match(html, /Holds only its own branch until you answer/);
  assert.match(html, /data-op="apply_decision"/);
});

test('a decision wait member is added at the end of the chain with its question', () => {
  const sb = loadApp();
  const out = plain(sb.automationAddDecisionWait(twoMembers(), '  Is it good?  ', 'm2'));
  const dw = out.members.find(m => m.kind === 'decision_wait');
  assert.match(dw.id, /^[0-9a-f]{8}$/);
  assert.equal(dw.question, 'Is it good?');
  assert.equal(dw.on_complete, '');
  assert.equal(out.members.find(m => m.id === 'm2').on_complete, dw.id);
});

test('a decision wait inserted in the middle takes over the edge it replaces', () => {
  const sb = loadApp();
  const out = plain(sb.automationAddDecisionWait(twoMembers(), 'Go?', 'm1'));
  const dw = out.members.find(m => m.kind === 'decision_wait');
  assert.equal(out.members.find(m => m.id === 'm1').on_complete, dw.id);
  assert.equal(dw.on_complete, 'm2');
});

test('a decision wait needs a question, and not an endless one', () => {
  const sb = loadApp();
  assert.throws(() => sb.automationAddDecisionWait(twoMembers(), '   ', 'm2'), /A decision wait needs a question/);
  assert.throws(() => sb.automationAddDecisionWait(twoMembers(), undefined, 'm2'), /needs a question/);
  assert.throws(() => sb.automationAddDecisionWait(twoMembers(), 'x'.repeat(4097), 'm2'), /The question is too long/);
  assert.doesNotThrow(() => sb.automationAddDecisionWait(twoMembers(), 'x'.repeat(4096), 'm2'));
});

test('applying the decision panel adds the member, closes the panel and shows it', async () => {
  const sb = await openNew();
  sb.automationFormPick('aaaaaaaa1111');
  sb.automationFormAdd();
  sb.automationFormOpenPanel('decision');
  sb.automationFormSetDecisionQuestion('Is the build good enough?');
  assert.equal(sb.automationFormApplyDecisionWait(), true);
  const f = plain(formState(sb));
  const dw = f.members.find(m => m.kind === 'decision_wait');
  assert.equal(dw.question, 'Is the build good enough?');
  assert.equal(f.members[0].on_complete, dw.id);
  assert.equal(f.decisionQuestion, '');
  assert.equal(sb.__els.panel.innerHTML, '');
  const row = sb.__els.members.innerHTML;
  assert.match(row, new RegExp('data-member-id="' + dw.id + '" data-kind="decision_wait"'));
  assert.match(row, /data-question-for="/);
  assert.match(row, /Is the build good enough\?/);
});

test('applying the decision panel with no question shows the message and adds nothing', async () => {
  const sb = await openNew();
  sb.automationFormOpenPanel('decision');
  assert.equal(sb.automationFormApplyDecisionWait(), false);
  assert.match(sb.__els.error.innerHTML, /A decision wait needs a question/);
  assert.equal(formState(sb).members.length, 0);
});

test('a decision wait row has only the on_complete edge and a remove button, and the question is editable', async () => {
  const sb = await openNew();
  sb.automationFormPick('aaaaaaaa1111');
  sb.automationFormAdd();
  sb.automationFormOpenPanel('decision');
  sb.automationFormSetDecisionQuestion('Go?');
  sb.automationFormApplyDecisionWait();
  const dw = formState(sb).members.find(m => m.kind === 'decision_wait');
  const html = sb.__els.members.innerHTML;
  const start = html.indexOf('data-member-id="' + dw.id + '"');
  const row = html.slice(start, html.indexOf('data-member-id=', start + 10) < 0 ? html.length : html.indexOf('data-member-id=', start + 10));
  assert.match(row, /data-edge="on_complete"/);
  assert.doesNotMatch(row, /data-edge="on_blocked"|data-edge="on_failed"/);
  assert.match(row, /data-op="remove_member"/);
  sb.automationFormSetQuestion(dw.id, 'Changed?');
  assert.equal(formState(sb).members.find(m => m.id === dw.id).question, 'Changed?');
  sb.automationFormSetQuestion('nope', 'x');
  sb.automationFormSetQuestion(formState(sb).members[0].id, 'x');
  assert.equal('question' in formState(sb).members[0], false, 'only a decision wait carries a question');
  sb.automationFormRemove(dw.id);
  assert.equal(formState(sb).members.some(m => m.kind === 'decision_wait'), false);
});

test('the payload sends a decision wait as kind, question and on_complete only', () => {
  const sb = loadApp();
  const state = plain(sb.automationAddDecisionWait(twoMembers(), 'Is it good?', 'm1'));
  state.members.find(m => m.kind === 'decision_wait').stage = ' review ';
  state.members.find(m => m.kind === 'decision_wait').on_blocked = 'm2';
  const body = plain(sb.buildAutomationFormPayload(Object.assign({}, state, { name: 'n' })));
  const dw = body.members.find(m => m.kind === 'decision_wait');
  assert.deepEqual(Object.keys(dw).sort(), ['id', 'kind', 'on_complete', 'question', 'stage']);
  assert.equal(dw.question, 'Is it good?');
  assert.equal(dw.stage, 'review');
  assert.equal(dw.on_complete, 'm2');
  assert.equal('prd_id' in dw, false);
  const first = body.members.find(m => m.id === 'm1');
  assert.equal('question' in first, false, 'a plain member never carries a question');
});

test('a decision wait inside a fork branch chain keeps its question and on_complete', () => {
  const sb = loadApp();
  let state = plain(sb.automationAddForkJoin(twoMembers(), [['cccccccc3333'], ['dddddddd4444']], 'all_must_succeed', 'm1'));
  const fork = state.members.find(m => m.kind === 'fork');
  const head = state.members.find(m => m.id === fork.fork_members[0]);
  const dw = { id: 'dw000001', kind: 'decision_wait', question: 'Go on?', on_complete: '' };
  head.on_complete = dw.id;
  state.members.push(dw);
  state.members.find(m => m.kind === 'join').join_members[0] = dw.id;
  const body = plain(sb.buildAutomationFormPayload(Object.assign({}, state, { name: 'n' })));
  const sent = body.members.find(m => m.id === 'dw000001');
  assert.equal(sent.question, 'Go on?');
  assert.equal(body.members.find(m => m.id === head.id).on_complete, 'dw000001');
});

// ── Insert dialog: a decision wait on a running Automation ───────────────────

function runningAutomation() {
  return {
    id: 'a1b2c3d4', name: 'Release train', description: '', status: 'active', version: 7,
    entry_member_id: 'm1', active_cursors: ['m2'],
    members: [
      member('m1', 'completed', { on_complete: 'm2', edge_taken: 'on_complete' }),
      member('m2', 'running', { on_complete: 'm3' }),
      member('m3', 'pending', { on_complete: 'm4' }),
      member('m4', 'pending'),
    ],
  };
}

async function openInsert() {
  const a = runningAutomation();
  const ids = {
    view: makeStubElement(),
    automationControls: makeStubElement(),
    automationStrip: makeStubElement(),
    genericModalConfirmBtn: makeStubElement(),
    automationInsertSearch: { value: '' },
    automationInsertPicker: makeStubElement(),
    automationInsertUrgent: { checked: false },
    automationInsertReason: { value: 'needed' },
    automationInsertQuestion: { value: '' },
    automationInsertError: makeStubElement(),
  };
  const routes = {
    'GET /api/automations/a1b2c3d4': { body: a },
    'GET /api/autonomous/prds': { body: PRDS },
    'GET /api/automations': { body: [a] },
    'POST /api/automations/a1b2c3d4/insert_member': { body: Object.assign({}, a, { version: 8 }) },
  };
  const sb = loadApp({ byId: ids, routes });
  sb.openAutomationDetail(a.id);
  await flushAsync();
  await sb.openAutomationInsertDialog('m3', 'after');
  return { sb, ids };
}

const confirm = async env => { env.ids.genericModalConfirmBtn.onclick(); await flushAsync(); };
const posts = sb => sb.__calls.filter(c => c.method === 'POST');

test('the insert dialog has a decision wait question box', async () => {
  const env = await openInsert();
  const html = env.sb.__modals.filter(m => m.innerHTML).pop().innerHTML;
  assert.match(html, /id="automationInsertQuestion"/);
  assert.match(html, /Or a decision wait: the question to ask you/);
});

test('a question in the insert dialog inserts a decision wait member', async () => {
  const env = await openInsert();
  env.ids.automationInsertQuestion.value = '  Is it safe to deploy?  ';
  await confirm(env);
  const sent = posts(env.sb);
  assert.equal(sent.length, 1);
  assert.equal(sent[0].url, '/api/automations/a1b2c3d4/insert_member');
  assert.deepEqual(sent[0].body.member, { kind: 'decision_wait', question: 'Is it safe to deploy?' });
  assert.equal(sent[0].body.after, 'm3');
  assert.equal(sent[0].body.reason, 'needed');
  assert.equal(sent[0].body.expected_version, 7);
});

test('an Automaton and a question together are refused; neither is refused too', async () => {
  const both = await openInsert();
  both.sb.automationPickInsertAutomaton('aaaaaaaa1111');
  both.ids.automationInsertQuestion.value = 'Go?';
  await confirm(both);
  assert.equal(posts(both.sb).length, 0);
  assert.match(both.ids.automationInsertError.innerHTML, /not both/);
  const none = await openInsert();
  await confirm(none);
  assert.equal(posts(none.sb).length, 0);
  assert.match(none.ids.automationInsertError.innerHTML, /Choose an Automaton/);
  const long = await openInsert();
  long.ids.automationInsertQuestion.value = 'x'.repeat(4097);
  await confirm(long);
  assert.equal(posts(long.sb).length, 0);
  assert.match(long.ids.automationInsertError.innerHTML, /too long/);
});

test('an Automaton picked in the insert dialog is still inserted by prd_id', async () => {
  const env = await openInsert();
  env.sb.automationPickInsertAutomaton('aaaaaaaa1111');
  await confirm(env);
  assert.deepEqual(posts(env.sb)[0].body.member, { prd_id: 'aaaaaaaa1111' });
});

// ── Locale bundles ───────────────────────────────────────────────────────────

const NEW_KEYS = [
  'automation_member_status_waiting_for_operator', 'automation_member_status_waiting_for_slot', 'automation_member_status_waiting_for_stage',
  'automation_decision_wait_label', 'automation_kind_decision_wait', 'automation_decision_wait_answer', 'automation_decision_wait_branch_note',
  'automation_decision_wait_inbox_note', 'automation_wait_strip_operator', 'automation_wait_strip_slot', 'automation_wait_strip_stage',
  'automation_max_parallel_chip', 'automation_stage_gate_chip', 'automation_form_run_shape_section', 'automation_form_max_parallel',
  'automation_form_max_parallel_hint', 'automation_form_max_parallel_invalid', 'automation_form_stage_gate', 'automation_form_stage_gate_hint',
  'automation_form_fork_max_parallel', 'automation_form_branch_add', 'automation_form_decision_wait_add', 'automation_form_decision_wait_question',
  'automation_form_decision_wait_placeholder', 'automation_form_decision_wait_hint', 'automation_form_decision_wait_need_question',
  'automation_form_decision_wait_too_long', 'automation_insert_question_label', 'automation_insert_one_only',
];

test('every new string is in all five locale bundles with the same placeholders', () => {
  const placeholders = s => (String(s).match(/%\d\$s/g) || []).sort().join(',');
  ['en', 'de', 'es', 'fr', 'ja'].forEach(lang => {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    NEW_KEYS.forEach(key => {
      assert.ok(typeof bundle[key] === 'string' && bundle[key].trim() !== '', lang + ' has ' + key);
      assert.equal(placeholders(bundle[key]), placeholders(EN[key]), lang + ' ' + key + ' keeps its placeholders');
    });
  });
});

test('the translated bundles do not just copy the English text', () => {
  ['de', 'es', 'fr', 'ja'].forEach(lang => {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    const same = NEW_KEYS.filter(k => bundle[k] === EN[k]);
    assert.deepEqual(same, [], lang + ' translates every new string');
  });
});

test('every t() key this feature uses in app.js exists in the English bundle', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  const used = new Set();
  NEW_KEYS.forEach(k => { if (src.includes("'" + k + "'")) used.add(k); });
  // The keys the strip, form and dialogs ask for by name all exist.
  const missing = [...used].filter(k => !(k in EN));
  assert.deepEqual(missing, []);
  assert.ok(used.size >= NEW_KEYS.length - 2, 'the new keys are used by the app');
});
