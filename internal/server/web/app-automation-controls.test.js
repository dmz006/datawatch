// Automation tab — detail page control operations.
//
// Covers the button set for each status, the request every control sends
// (expected_version from the loaded Automation, actor), the cancel dialog being
// the only way to cancel and refusing to send without a choice, the explicit
// cancel_in_flight key, the stale prompt on a 409 (no retry) versus a toast on
// any other failure, and the branch selector for several active branches.
// fetch is stubbed and records every request; rendered text is the real
// English bundle.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

// loadApp returns a sandbox with the app loaded and a recording fetch. routes
// maps "METHOD /path" to {status, body}; an unknown request answers 404. Every
// request is recorded in __calls with its parsed JSON body. The modal the app
// opens is captured in __modals; the elements by id are the byId map.
function loadApp(routes, byId) {
  const ids = byId || {};
  const sandbox = buildSandbox({ byId: ids });
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
  // Record the modal showModal builds; its confirm button and the cancel
  // dialog's message line are registered by id so the test can click and read.
  sandbox.document.createElement = () => {
    const el = makeStubElement();
    el.removed = false;
    el.remove = () => { el.removed = true; };
    modals.push(el);
    return el;
  };
  ids.genericModalConfirmBtn = makeStubElement();
  ids.automationCancelRequired = makeStubElement();
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

function automation(extra) {
  return Object.assign({
    id: 'a1b2c3d4', name: 'Release train', description: '', status: 'active', version: 7,
    entry_member_id: 'm1', active_cursors: ['m2'],
    members: [
      member('m1', 'completed', { on_complete: 'm2', edge_taken: 'on_complete' }),
      member('m2', 'running', { on_complete: 'm3' }),
      member('m3', 'pending'),
    ],
  }, extra || {});
}

// twoBranches is an Automation with a fork whose two branches both run.
function twoBranches(extra) {
  return automation(Object.assign({
    active_cursors: ['b1', 'b2'],
    members: [
      member('b1', 'running'),
      member('b2', 'running'),
    ],
  }, extra || {}));
}

// openDetail loads one Automation through the page and resolves the env.
// extraRoutes adds POST answers.
async function openDetail(a, extraRoutes) {
  const view = makeStubElement();
  const controls = makeStubElement();
  const routes = Object.assign({
    ['GET /api/automations/' + encodeURIComponent(a.id)]: { body: a },
    'GET /api/autonomous/prds': { body: { prds: [] } },
  }, extraRoutes || {});
  const byId = { view, automationControls: controls, automationStrip: makeStubElement() };
  const sb = loadApp(routes, byId);
  sb.openAutomationDetail(a.id);
  await flushAsync();
  return { sb, view, controls, byId };
}

// controlsFor renders only the controls for one Automation.
function controlsFor(a, titles) {
  const controls = makeStubElement();
  const sb = loadApp({}, { automationControls: controls });
  if (titles) stateOf(sb).prdTitles = titles;
  sb.renderAutomationControls(a);
  return { sb, html: controls.innerHTML };
}

const opsIn = html => (html.match(/data-op="([^"]*)"/g) || []).map(s => s.match(/data-op="([^"]*)"/)[1]);
const posts = sb => sb.__calls.filter(c => c.method === 'POST');
const lastModal = sb => sb.__modals.filter(m => m.innerHTML).pop();

// A pause the server accepts: version 7 in, version 8 out.
const PAUSE_OK = {
  'POST /api/automations/a1b2c3d4/pause': { body: automation({ status: 'paused', version: 8 }) },
};

test('each status offers the right operations and never more', () => {
  const expected = {
    draft: ['activate', 'set_verification_llm', 'set_budget', 'cancel'],
    active: ['pause', 'set_verification_llm', 'set_budget', 'cancel'],
    paused: ['resume', 'set_verification_llm', 'set_budget', 'cancel'],
    blocked: ['resume', 'set_verification_llm', 'set_budget', 'cancel'],
    completed: [],
    cancelled: [],
  };
  for (const [status, ops] of Object.entries(expected)) {
    const { html } = controlsFor(automation({ status }));
    assert.deepEqual(opsIn(html), ops, 'status ' + status);
  }
});

test('the buttons carry the translated labels', () => {
  const { html } = controlsFor(automation({ status: 'active' }));
  assert.match(html, />Pause<\/button>/);
  assert.match(html, />Cancel Automation<\/button>/);
  const paused = controlsFor(automation({ status: 'paused' })).html;
  assert.match(paused, />Resume<\/button>/);
});

test('a finished Automation leaves the control area empty', () => {
  assert.equal(controlsFor(automation({ status: 'completed' })).html, '');
  assert.equal(controlsFor(automation({ status: 'cancelled' })).html, '');
});

test('the detail page fills the control area for the loaded status', async () => {
  const { controls } = await openDetail(automation({ status: 'paused' }));
  assert.deepEqual(opsIn(controls.innerHTML), ['resume', 'set_verification_llm', 'set_budget', 'cancel']);
});

test('pause posts to the pause path with expected_version from the loaded version and actor operator', async () => {
  const { sb, view, controls } = await openDetail(automation({ version: 7 }), PAUSE_OK);
  await sb.automationControl('a1b2c3d4', 'pause');
  const sent = posts(sb);
  assert.equal(sent.length, 1);
  assert.equal(sent[0].url, '/api/automations/a1b2c3d4/pause');
  assert.equal(sent[0].body.expected_version, 7);
  assert.equal(sent[0].body.actor, 'operator');
  // The returned Automation becomes the detail and the page and controls redraw.
  assert.equal(stateOf(sb).detail.version, 8);
  assert.match(view.innerHTML, />Version 8<\/span>/);
  assert.deepEqual(opsIn(controls.innerHTML), ['resume', 'set_verification_llm', 'set_budget', 'cancel']);
});

test('a caller cannot override the version or actor', async () => {
  const { sb } = await openDetail(automation({ version: 7 }), PAUSE_OK);
  await sb.automationControl('a1b2c3d4', 'pause', { expected_version: 1, actor: 'someone' });
  const body = posts(sb)[0].body;
  assert.equal(body.expected_version, 7);
  assert.equal(body.actor, 'operator');
});

test('the Cancel Automation button opens the dialog and sends nothing', async () => {
  const { sb, controls } = await openDetail(automation());
  assert.match(controls.innerHTML, /onclick="openAutomationCancelDialog\('a1b2c3d4'\)"/);
  assert.ok(!/automationControl\([^)]*'cancel'/.test(controls.innerHTML), 'no control path posts a cancel directly');
  sb.openAutomationCancelDialog('a1b2c3d4');
  assert.equal(posts(sb).length, 0);
  const html = lastModal(sb).innerHTML;
  assert.match(html, /Cancel this Automation\?/);
  assert.match(html, /Choose what happens to the member that is running now\. You must pick one\./);
  assert.match(html, /Stop advancing; let the running member finish/);
  assert.match(html, /Also cancel the member that is running now/);
  assert.match(html, />Cancel Automation<\/button>/);
  assert.equal((html.match(/type="radio"/g) || []).length, 2);
  assert.ok(!/checked/.test(html), 'neither choice is preselected');
  assert.ok(!html.includes('Choose one option to continue'), 'the required message is not shown up front');
});

test('confirming the cancel dialog with no choice sends no request, shows the message and stays open', async () => {
  const { sb, byId } = await openDetail(automation());
  sb.openAutomationCancelDialog('a1b2c3d4');
  const modal = lastModal(sb);
  sb.document.querySelector = () => null;
  byId.genericModalConfirmBtn.onclick();
  await flushAsync();
  assert.equal(posts(sb).length, 0);
  assert.match(byId.automationCancelRequired.innerHTML, /Choose one option to continue/);
  assert.equal(modal.removed, false, 'the dialog stays open');
});

test('choosing stop advancing sends cancel_in_flight false as an explicit key', async () => {
  const { sb, byId } = await openDetail(automation({ version: 7 }), {
    'POST /api/automations/a1b2c3d4/cancel': { body: automation({ status: 'cancelled', version: 8 }) },
  });
  sb.openAutomationCancelDialog('a1b2c3d4');
  const modal = lastModal(sb);
  sb.document.querySelector = () => ({ value: 'stop_advance' });
  byId.genericModalConfirmBtn.onclick();
  await flushAsync();
  const sent = posts(sb);
  assert.equal(sent.length, 1);
  assert.equal(sent[0].url, '/api/automations/a1b2c3d4/cancel');
  assert.ok(Object.prototype.hasOwnProperty.call(sent[0].body, 'cancel_in_flight'), 'the key is present');
  assert.equal(sent[0].body.cancel_in_flight, false);
  assert.equal(sent[0].body.expected_version, 7);
  assert.equal(sent[0].body.actor, 'operator');
  assert.equal(modal.removed, true, 'the dialog closes once a choice is sent');
  assert.equal(stateOf(sb).detail.status, 'cancelled');
});

test('choosing the in-flight option sends cancel_in_flight true as an explicit key', async () => {
  const { sb, byId } = await openDetail(automation(), {
    'POST /api/automations/a1b2c3d4/cancel': { body: automation({ status: 'cancelled', version: 8 }) },
  });
  sb.openAutomationCancelDialog('a1b2c3d4');
  sb.document.querySelector = () => ({ value: 'in_flight' });
  byId.genericModalConfirmBtn.onclick();
  await flushAsync();
  const sent = posts(sb);
  assert.equal(sent.length, 1);
  assert.ok(Object.prototype.hasOwnProperty.call(sent[0].body, 'cancel_in_flight'), 'the key is present');
  assert.equal(sent[0].body.cancel_in_flight, true);
});

test('a 409 shows the stale prompt with the server message and a reload button, and does not retry', async () => {
  const { sb, byId } = await openDetail(automation({ version: 7 }), {
    'POST /api/automations/a1b2c3d4/pause': { status: 409, body: 'version 7 is stale, current is 9 <b>x</b>' },
  });
  await sb.automationControl('a1b2c3d4', 'pause');
  assert.equal(posts(sb).length, 1, 'no retry');
  const html = lastModal(sb).innerHTML;
  assert.match(html, /Out of date/);
  assert.match(html, /This Automation was changed elsewhere\. Reload it before making changes\./);
  assert.ok(html.includes('version 7 is stale, current is 9 &lt;b&gt;x&lt;/b&gt;'), 'the server message is shown, escaped');
  assert.ok(!html.includes('<b>x</b>'));
  assert.match(html, />Reload<\/button>/);
  assert.equal(sb.__toasts.length, 0);
  // Reload re-fetches the Automation and sends no further change.
  const gets = () => sb.__calls.filter(c => c.method === 'GET' && c.url === '/api/automations/a1b2c3d4').length;
  const before = gets();
  byId.genericModalConfirmBtn.onclick();
  await flushAsync();
  assert.equal(gets(), before + 1);
  assert.equal(posts(sb).length, 1);
});

test('a state conflict 409 on cancel also shows the stale prompt', async () => {
  const { sb, byId } = await openDetail(automation(), {
    'POST /api/automations/a1b2c3d4/cancel': { status: 409, body: 'automation is already cancelled' },
  });
  sb.openAutomationCancelDialog('a1b2c3d4');
  sb.document.querySelector = () => ({ value: 'in_flight' });
  byId.genericModalConfirmBtn.onclick();
  await flushAsync();
  assert.equal(posts(sb).length, 1);
  assert.match(lastModal(sb).innerHTML, /automation is already cancelled/);
  assert.match(lastModal(sb).innerHTML, /Out of date/);
});

test('a non-409 error shows a toast and not the stale prompt', async () => {
  const { sb } = await openDetail(automation(), {
    'POST /api/automations/a1b2c3d4/pause': { status: 500, body: 'store unavailable' },
  });
  const modalsBefore = sb.__modals.length;
  await sb.automationControl('a1b2c3d4', 'pause');
  assert.equal(sb.__toasts.length, 1);
  assert.equal(sb.__toasts[0].message, 'store unavailable');
  assert.equal(sb.__toasts[0].type, 'error');
  assert.equal(sb.__modals.length, modalsBefore, 'no modal opened');
  assert.equal(posts(sb).length, 1, 'no retry');
  assert.equal(stateOf(sb).detail.version, 7, 'the loaded Automation is unchanged');
});

test('with two active branches the selector and the whole-Automation scope text appear', () => {
  const { html } = controlsFor(twoBranches(), { b1aaaaaaaaaaaa: 'Build', b2aaaaaaaaaaaa: 'Test' });
  assert.match(html, /<select class="automation-branch-select"/);
  assert.match(html, /Acting on branch/);
  assert.match(html, /<option value="b1">Build \(b1\)<\/option>/);
  assert.match(html, /<option value="b2">Test \(b2\)<\/option>/);
  assert.match(html, /Applies to the whole Automation/);
  assert.deepEqual(opsIn(html), ['pause', 'set_verification_llm', 'set_budget', 'cancel']);
});

test('the branch selector starts with no branch chosen and remembers a choice', () => {
  const controls = makeStubElement();
  const sb = loadApp({}, { automationControls: controls });
  const a = twoBranches();
  stateOf(sb).detail = a;
  sb.renderAutomationControls(a);
  assert.equal(stateOf(sb).actingBranch, '');
  assert.match(controls.innerHTML, /<option value="" selected>/);
  sb.automationSelectBranch('b2');
  assert.equal(stateOf(sb).actingBranch, 'b2');
  assert.match(controls.innerHTML, /<option value="b2" selected>/);
});

test('with one active branch it is named in the text and there is no selector', () => {
  const { html } = controlsFor(automation(), { m2aaaaaaaaaaaa: 'Ship it' });
  assert.ok(!html.includes('<select'));
  assert.ok(!html.includes('Applies to the whole Automation'));
  assert.match(html, /Acting on branch/);
  assert.match(html, /<strong class="automation-branch-name">Ship it \(m2\)<\/strong>/);
});

test('automationActiveBranches returns the live members with their ids and titles', () => {
  const sb = loadApp({}, {});
  stateOf(sb).prdTitles = { b1aaaaaaaaaaaa: 'Build' };
  const branches = sb.automationActiveBranches(twoBranches({
    members: [
      member('b1', 'running'),
      { id: 'b2', kind: 'automation', automation_id: 'child01', status: 'running' },
      member('b3', 'pending'),
    ],
  }));
  assert.equal(branches.length, 2);
  assert.equal(branches[0].id, 'b1');
  assert.equal(branches[0].prd_id, 'b1aaaaaaaaaaaa');
  assert.equal(branches[0].title, 'Build');
  assert.equal(branches[1].id, 'b2');
  assert.equal(branches[1].automation_id, 'child01');
  assert.equal(sb.automationActiveBranches(automation({ active_cursors: [] })).length, 0);
});
