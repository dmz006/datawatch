// Automaton detail "Subscriptions" tab: lists the lifecycle event
// subscriptions that apply to one Automaton (scoped to its id, plus
// unscoped "all automata" ones) with per-row Delete and Test. REST side
// is covered in internal/server (autonomous_subscriptions tests).

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, makeStubElement, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const EN = require('./locales/en.json');

function fakeResponse(status, body) {
  const text = typeof body === 'string' ? body : JSON.stringify(body === undefined ? {} : body);
  return { ok: status >= 200 && status < 300, status, statusText: 'x', text: () => Promise.resolve(text), json: () => Promise.resolve(JSON.parse(text)) };
}

// Loads app.js with a body element for PRD "p1" and a fetch router.
// routes: array of [method, urlRegex, () => response]; calls are recorded.
function setup(routes) {
  const body = makeStubElement();
  const result = makeStubElement();
  const sandbox = buildSandbox();
  sandbox.document.getElementById = (id) => {
    if (id === 'prdSubsBody_p1') return body;
    if (id === 'prdSubsResult_p1') return result;
    return makeStubElement();
  };
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  Object.assign(sandbox._i18n.fallback, EN);
  const calls = [];
  sandbox.fetch = (url, opts) => {
    const method = (opts && opts.method) || 'GET';
    calls.push(method + ' ' + url);
    for (const [m, re, fn] of routes) {
      if (m === method && re.test(url)) return Promise.resolve(fn());
    }
    return Promise.resolve(fakeResponse(404, 'not found'));
  };
  const toasts = [];
  sandbox.showToast = (msg, type) => toasts.push([msg, type]);
  return { sandbox, body, result, calls, toasts };
}

const run = (sandbox, code) => vm.runInContext(code, sandbox);

const SUBS = [
  { id: 's-mine', owner: 'alice', filter: { prd_id: 'p1', kinds: ['prd.completed', 'prd.failed'] }, sink: { type: 'session', target: 'abcd' },
    action: { type: 'send_input', text: 'go' }, one_shot: true, coalesce: true, expires_at: '2030-01-02T03:04:05Z' },
  { id: 's-all', filter: { level: 'prd' }, sink: { type: 'alert' } },
  { id: 's-other', filter: { prd_id: 'other', kinds: ['prd.completed'] }, sink: { type: 'alert' } },
];

test('lifecycle subscriptions: tab strip includes the Subscriptions tab and the renderer is wired', () => {
  const { sandbox } = setup([]);
  const strip = run(sandbox, `_renderDetailTabStrip({id:'p1'}, 'subscriptions')`);
  assert.match(strip, /switchAutomataDetailTab\('subscriptions'\)/);
  assert.match(strip, /Subscriptions/);
  const html = run(sandbox, `_renderDetailSubscriptionsTab({id:'p1'})`);
  assert.match(html, /prdSubsBody_p1/);
  assert.match(html, /_lifecycleSubOpenForm/);
});

test('lifecycle subscriptions: rows show only relevant subscriptions with kinds, sink, action, flags and owner', async () => {
  const { sandbox, body, calls } = setup([['GET', /\/api\/autonomous\/subscriptions$/, () => fakeResponse(200, SUBS)]]);
  await run(sandbox, `_loadPRDSubscriptionsTab({id:'p1'})`);
  const html = body.innerHTML;
  assert.deepEqual(calls, ['GET /api/autonomous/subscriptions']);
  assert.match(html, /prd\.completed/);
  assert.match(html, /prd\.failed/);
  assert.match(html, /session → abcd/);
  assert.match(html, /send_input/);
  assert.match(html, /one-shot/);
  assert.match(html, /coalesce/);
  assert.match(html, /expires /);
  assert.match(html, /owner: alice/);
  assert.match(html, /all automata/);       // the unscoped level subscription
  assert.match(html, /Level: prd/);
  assert.equal((html.match(/prd-sub-row/g) || []).length, 2, 'the subscription scoped to another Automaton must be hidden');
});

test('lifecycle subscriptions: an XSS payload in a target or owner is escaped', async () => {
  const evil = [{ id: 'x1', owner: '<img src=x onerror=alert(2)>', filter: { prd_id: 'p1', kinds: ['prd.completed'] },
    sink: { type: 'comm', target: '<script>alert(1)</script>' } }];
  const { sandbox, body } = setup([['GET', /subscriptions$/, () => fakeResponse(200, evil)]]);
  await run(sandbox, `_loadPRDSubscriptionsTab({id:'p1'})`);
  assert.ok(!body.innerHTML.includes('<script>'), body.innerHTML);
  assert.ok(!body.innerHTML.includes('<img src=x'), body.innerHTML);
  assert.match(body.innerHTML, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
});

test('lifecycle subscriptions: empty state when nothing applies', async () => {
  const { sandbox, body } = setup([['GET', /subscriptions$/, () => fakeResponse(200, [SUBS[2]])]]);
  await run(sandbox, `_loadPRDSubscriptionsTab({id:'p1'})`);
  assert.match(body.innerHTML, /No subscriptions apply to this Automaton/);
  assert.ok(!body.innerHTML.includes('prd-sub-row'));
});

test('lifecycle subscriptions: delete flow confirms, calls DELETE, then reloads the list', async () => {
  let deleted = false;
  const { sandbox, body, calls } = setup([
    ['GET', /subscriptions$/, () => fakeResponse(200, deleted ? [] : [SUBS[0]])],
    ['DELETE', /subscriptions\/s-mine$/, () => { deleted = true; return fakeResponse(204, ''); }],
  ]);
  let confirmMsg = '';
  sandbox.showConfirmModal = (msg, onYes) => { confirmMsg = msg; onYes(); };
  await run(sandbox, `_loadPRDSubscriptionsTab({id:'p1'})`);
  assert.match(body.innerHTML, /prd-sub-row/);
  await run(sandbox, `_lifecycleSubDelete('p1', 's-mine')`);
  assert.match(confirmMsg, /Delete this subscription/);
  assert.deepEqual(calls, [
    'GET /api/autonomous/subscriptions',
    'DELETE /api/autonomous/subscriptions/s-mine',
    'GET /api/autonomous/subscriptions',
  ]);
  assert.match(body.innerHTML, /No subscriptions apply/);
});

test('lifecycle subscriptions: declining the confirm does not call DELETE', async () => {
  const { sandbox, calls } = setup([]);
  sandbox.showConfirmModal = () => {};
  run(sandbox, `_lifecycleSubDelete('p1', 's-mine')`);
  await flushAsync();
  assert.deepEqual(calls, []);
});

test('lifecycle subscriptions: delete failure is reported and the row is kept', async () => {
  const { sandbox, result, toasts } = setup([['DELETE', /s-mine$/, () => fakeResponse(404, 'subscription not found')]]);
  sandbox.showConfirmModal = (m, onYes) => onYes();
  await run(sandbox, `_lifecycleSubDelete('p1', 's-mine')`);
  assert.match(result.innerHTML, /Delete failed: subscription not found/);
  assert.equal(toasts[0][1], 'error');
});

test('lifecycle subscriptions: test flow shows the delivery sink on success', async () => {
  const { sandbox, result, calls, toasts } = setup([['POST', /s-mine\/test$/, () => fakeResponse(200, { ok: true, sink: 'session' })]]);
  await run(sandbox, `_lifecycleSubTest('p1', 's-mine')`);
  assert.deepEqual(calls, ['POST /api/autonomous/subscriptions/s-mine/test']);
  assert.match(result.innerHTML, /Delivered via session/);
  assert.equal(toasts[0][1], 'success');
});

test('lifecycle subscriptions: test flow shows the error text, including "sink not available"', async () => {
  const unavailable = setup([['POST', /test$/, () => fakeResponse(503, 'sink not available')]]);
  await run(unavailable.sandbox, `_lifecycleSubTest('p1', 's-mine')`);
  assert.match(unavailable.result.innerHTML, /Test failed: sink not available/);
  assert.equal(unavailable.toasts[0][1], 'error');

  const failed = setup([['POST', /test$/, () => fakeResponse(200, { ok: false, sink: 'comm', error: 'channel down' })]]);
  await run(failed.sandbox, `_lifecycleSubTest('p1', 's-mine')`);
  assert.match(failed.result.innerHTML, /Test failed: channel down/);
});

test('lifecycle subscriptions: a 503 shows the disabled message', async () => {
  const { sandbox, body } = setup([['GET', /subscriptions$/, () => fakeResponse(503, 'lifecycle subscriptions disabled\n')]]);
  await run(sandbox, `_loadPRDSubscriptionsTab({id:'p1'})`);
  assert.match(body.innerHTML, /Lifecycle subscriptions are disabled on this server/);
});

test('lifecycle subscriptions: a federated capability error surfaces the real server text', async () => {
  const { sandbox, body } = setup([['GET', /subscriptions$/, () => fakeResponse(403, 'federation peer lacks capability: autonomous:read')]]);
  run(sandbox, `state.activeServer = 'host-a';`);
  await run(sandbox, `_loadPRDSubscriptionsTab({id:'p1'})`);
  assert.match(body.innerHTML, /autonomous:read/);
});

test('lifecycle subscriptions: Add and Edit go through the form hook with the right arguments', () => {
  const { sandbox } = setup([]);
  run(sandbox, `_prdSubsCache['p1'] = [{id:'s1', filter:{}, sink:{type:'alert'}}];`);
  const seen = [];
  sandbox._openLifecycleSubscriptionForm = (prd, sub) => seen.push([prd.id, sub && sub.id]);
  run(sandbox, `_lifecycleSubOpenForm('p1', '')`);
  run(sandbox, `_lifecycleSubOpenForm('p1', 's1')`);
  assert.deepEqual(seen, [['p1', null], ['p1', 's1']]);
});
