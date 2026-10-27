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

// ── Add / edit form ─────────────────────────────────────────────────────────

// Mounts the form and returns the captured modal HTML. fields maps element id
// -> fake element ({value}/{checked}); anything else is a default stub.
function formSetup(routes, fields) {
  const s = setup(routes);
  const { sandbox } = s;
  const modals = [];
  sandbox.document.createElement = () => {
    const el = makeStubElement();
    modals.push(el);
    return el;
  };
  const byId = Object.assign({}, fields);
  const baseGet = sandbox.document.getElementById;
  sandbox.document.getElementById = (id) => (byId[id] !== undefined ? byId[id] : baseGet(id));
  s.byId = byId;
  s.html = () => modals.map(m => m.innerHTML).join('');
  s.requests = [];
  const origFetch = sandbox.fetch;
  sandbox.fetch = (url, opts) => {
    s.requests.push({ method: (opts && opts.method) || 'GET', url, body: opts && opts.body ? JSON.parse(opts.body) : undefined });
    return origFetch(url, opts);
  };
  return s;
}

const kindIdx = (sandbox, kind) => run(sandbox, `_LIFECYCLE_SUB_KINDS.indexOf(${JSON.stringify(kind)})`);

function fill(s, vals) {
  const { sandbox, byId } = s;
  for (const k of (vals.kinds || [])) byId['lcSubKind_' + kindIdx(sandbox, k)] = { checked: true };
  const set = { level: 'lcSubLevel', scope: 'lcSubScope', taskId: 'lcSubTaskId', sinkType: 'lcSubSinkType', target: 'lcSubTarget', actionText: 'lcSubActionText', expiry: 'lcSubExpiry' };
  for (const [k, id] of Object.entries(set)) if (vals[k] !== undefined) byId[id] = { value: vals[k] };
  if (vals.oneShot) byId.lcSubOneShot = { checked: true };
  if (vals.coalesce) byId.lcSubCoalesce = { checked: true };
}

const openForm = (sandbox, sub) => run(sandbox, `_openLifecycleSubscriptionForm({id:'p1'}, ${JSON.stringify(sub || null)})`);
const submit = (sandbox) => run(sandbox, `_lifecycleSubSubmit()`);

test('lifecycle subscriptions form: create sends the body for a session send_input one-shot', async () => {
  const s = formSetup([
    ['POST', /subscriptions$/, () => fakeResponse(201, { id: 'new' })],
    ['GET', /subscriptions$/, () => fakeResponse(200, [])],
  ]);
  openForm(s.sandbox, null);
  fill(s, { kinds: ['prd.completed', 'task.failed'], level: '', scope: 'this', sinkType: 'session', target: 'abcd', actionText: 'continue please', oneShot: true });
  await submit(s.sandbox);
  const post = s.requests.find(r => r.method === 'POST');
  assert.equal(post.url, '/api/autonomous/subscriptions');
  assert.deepEqual(JSON.parse(JSON.stringify(post.body)), {
    filter: { kinds: ['prd.completed', 'task.failed'], prd_id: 'p1' },
    sink: { type: 'session', target: 'abcd' },
    action: { type: 'send_input', text: 'continue please' },
    one_shot: true,
  });
  assert.equal(s.requests[s.requests.length - 1].method, 'GET', 'the tab list is reloaded after save');
});

test('lifecycle subscriptions form: create sends a comm coalesce subscription for all automata', async () => {
  const s = formSetup([['POST', /subscriptions$/, () => fakeResponse(201, { id: 'n' })], ['GET', /subscriptions$/, () => fakeResponse(200, [])]]);
  openForm(s.sandbox, null);
  fill(s, { level: 'prd', scope: 'all', taskId: ' t9 ', sinkType: 'comm', target: 'signal', coalesce: true });
  await submit(s.sandbox);
  const post = s.requests.find(r => r.method === 'POST');
  assert.deepEqual(JSON.parse(JSON.stringify(post.body)), {
    filter: { level: 'prd', task_id: 't9' },
    sink: { type: 'comm', target: 'signal' },
    coalesce: true,
  });
});

test('lifecycle subscriptions form: create sends an mcp_queue subscription', async () => {
  const s = formSetup([['POST', /subscriptions$/, () => fakeResponse(201, { id: 'n' })], ['GET', /subscriptions$/, () => fakeResponse(200, [])]]);
  openForm(s.sandbox, null);
  fill(s, { kinds: ['prd.updated'], sinkType: 'mcp_queue', target: 'ci.events:main-1', actionText: 'ignored', coalesce: true });
  await submit(s.sandbox);
  const post = s.requests.find(r => r.method === 'POST');
  assert.deepEqual(JSON.parse(JSON.stringify(post.body)), {
    filter: { kinds: ['prd.updated'], prd_id: 'p1' },
    sink: { type: 'mcp_queue', target: 'ci.events:main-1' },
  }, 'action text and coalesce belong to other sink types and must be dropped');
});

test('lifecycle subscriptions form: the webhook sink is not offered and every kind is', () => {
  const s = formSetup([]);
  openForm(s.sandbox, null);
  const html = s.html();
  assert.ok(!/webhook/i.test(html), 'webhook must not appear in the form');
  for (const sink of ['comm', 'session', 'topic', 'alert', 'mcp_queue']) {
    assert.match(html, new RegExp(`<option value="${sink}"`));
  }
  const kinds = run(s.sandbox, `_LIFECYCLE_SUB_KINDS`);
  assert.equal(kinds.length, 31);
  for (const k of ['prd.draft', 'prd.paused', 'task.awaiting_approval', 'task.review_needed', 'task.session_started', 'task.session_cleared', 'scope_warning', 'guardrail_warn', 'prd.updated']) {
    assert.ok(kinds.includes(k), k);
    assert.ok(html.includes(k), k);
  }
  assert.match(html, /Add subscription/);

  // The bare warning kinds sit in their own localized "Warnings (opt-in)"
  // group, and the form carries the opt-in explainer below the kind grid.
  assert.match(html, /Warnings \(opt-in\)/, 'opt-in warning group header shown (en value via t(lifecycle_sub_form_group_warnings))');
  assert.match(html, /opt-in kind only fires when the Automaton/,
    'opt-in explainer shown under the kind grid (apostrophe HTML-escaped in render)');
});

test('lifecycle subscriptions form: edit is prefilled and saves with PUT', async () => {
  const s = formSetup([
    ['PUT', /subscriptions\/s-mine$/, () => fakeResponse(200, SUBS[0])],
    ['GET', /subscriptions$/, () => fakeResponse(200, [])],
  ]);
  openForm(s.sandbox, SUBS[0]);
  const html = s.html();
  assert.match(html, /Edit subscription/);
  assert.match(html, /value="abcd"/);
  assert.match(html, /<option value="session" selected>/);
  assert.match(html, />go<\/textarea>/);
  assert.match(html, /id="lcSubOneShot" checked/);
  assert.match(html, new RegExp(`id="lcSubKind_${kindIdx(s.sandbox, 'prd.completed')}" checked`));
  assert.match(html, new RegExp(`id="lcSubKind_${kindIdx(s.sandbox, 'prd.failed')}" checked`));
  assert.ok(!new RegExp(`id="lcSubKind_${kindIdx(s.sandbox, 'prd.draft')}" checked`).test(html));
  // Read the form back as the browser would: the prefilled values.
  fill(s, { kinds: ['prd.completed', 'prd.failed'], scope: 'this', sinkType: 'session', target: 'abcd2', actionText: 'go', oneShot: true, coalesce: true });
  await submit(s.sandbox);
  const put = s.requests.find(r => r.method === 'PUT');
  assert.equal(put.url, '/api/autonomous/subscriptions/s-mine');
  assert.equal(put.body.sink.target, 'abcd2');
  assert.equal(put.body.filter.prd_id, 'p1');
  assert.ok(!s.requests.some(r => r.method === 'POST'));
});

test('lifecycle subscriptions form: client validation blocks bad input without a request', async () => {
  const cases = [
    [{}, /Choose at least one event kind or a level/],
    [{ kinds: ['prd.failed'], sinkType: 'comm', target: '   ' }, /A target is required/],
    [{ level: 'task', sinkType: 'mcp_queue', target: 'bad name!' }, /Queue name may contain only letters, digits/],
    [{ level: 'task', sinkType: 'mcp_queue', target: 'q'.repeat(65) }, /Queue name is 65 characters; the limit is 64/],
    [{ level: 'task', sinkType: 'session', target: 'abcd', actionText: '    ' }, /send_input action needs text/],
    [{ level: 'task', sinkType: 'session', target: 'abcd', actionText: 'x'.repeat(4097) }, /Action text is 4097 characters; the limit is 4096/],
    [{ level: 'task', sinkType: 'comm', target: 'c', expiry: '0' }, /Expiry must be a number of hours greater than zero/],
    [{ level: 'task', sinkType: 'comm', target: 'c', expiry: 'soon' }, /Expiry must be a number of hours/],
  ];
  for (const [vals, want] of cases) {
    const errBox = makeStubElement();
    const s = formSetup([], { lcSubError: errBox });
    openForm(s.sandbox, null);
    fill(s, vals);
    await submit(s.sandbox);
    assert.match(errBox.innerHTML, want, JSON.stringify(vals));
    assert.deepEqual(s.requests, [], 'no request for ' + JSON.stringify(vals));
  }
});

test('lifecycle subscriptions form: a 4096-character action text is accepted', () => {
  const { sandbox } = setup([]);
  const out = run(sandbox, `_lifecycleSubBuild({kinds:['prd.failed'], sinkType:'session', target:'s', actionText:'x'.repeat(4096), scope:'all'}, 0)`);
  assert.equal(out.error, undefined);
  assert.equal(out.body.action.text.length, 4096);
});

test('lifecycle subscriptions form: server 400 text is shown inline and the form stays open', async () => {
  const errBox = makeStubElement();
  const s = formSetup([['POST', /subscriptions$/, () => fakeResponse(400, 'filter kind "prd.bogus" is not a known event kind\n')]], { lcSubError: errBox });
  openForm(s.sandbox, null);
  fill(s, { level: 'prd', sinkType: 'alert', target: 'lbl' });
  await submit(s.sandbox);
  assert.match(errBox.innerHTML, /filter kind &quot;prd\.bogus&quot; is not a known event kind/);
  assert.ok(!s.requests.some(r => r.method === 'GET'), 'the list is not reloaded on failure');
});

test('lifecycle subscriptions form: send_input and coalesce are only enabled for their sink types', () => {
  const s = formSetup([]);
  openForm(s.sandbox, null);
  assert.match(s.html(), /<textarea id="lcSubActionText"[^>]* disabled/, 'comm is the default sink: action text off');
  assert.match(s.html(), /id="lcSubCoalesce"[^>]*>/);
  assert.ok(!/id="lcSubCoalesce"[^>]* disabled/.test(s.html()), 'coalesce on for comm');

  const action = { disabled: true };
  const coalesce = { disabled: false };
  const label = { textContent: '' };
  const type = { value: 'session' };
  Object.assign(s.byId, { lcSubSinkType: type, lcSubActionText: action, lcSubCoalesce: coalesce, lcSubTargetLabel: label });
  run(s.sandbox, `_lifecycleSubSinkChanged()`);
  assert.equal(action.disabled, false);
  assert.equal(coalesce.disabled, true);
  assert.equal(label.textContent, 'Session name or id');
  for (const [sink, text] of [['comm', 'Channel'], ['topic', 'Topic name'], ['alert', 'Alert label'], ['mcp_queue', /Queue name/]]) {
    type.value = sink;
    run(s.sandbox, `_lifecycleSubSinkChanged()`);
    assert.equal(action.disabled, true, sink);
    assert.equal(coalesce.disabled, sink !== 'comm', sink);
    if (text instanceof RegExp) assert.match(label.textContent, text); else assert.equal(label.textContent, text);
  }
});

test('lifecycle subscriptions form: editing a session subscription starts with the action text enabled and counted', () => {
  const s = formSetup([]);
  openForm(s.sandbox, SUBS[0]);
  assert.ok(!/<textarea id="lcSubActionText"[^>]* disabled/.test(s.html()));
  assert.match(s.html(), /id="lcSubActionCount">2 \/ 4096</);
  const count = { textContent: '' };
  Object.assign(s.byId, { lcSubActionText: { value: 'héllo' }, lcSubActionCount: count });
  run(s.sandbox, `_lifecycleSubCountChanged()`);
  assert.equal(count.textContent, '5 / 4096');
});

test('lifecycle subscriptions form: expiry hours become an RFC3339 expires_at', () => {
  const { sandbox } = setup([]);
  const now = Date.UTC(2030, 0, 1, 12, 0, 0);
  const out = run(sandbox, `_lifecycleSubBuild({kinds:['prd.failed'], sinkType:'alert', target:'x', expiryHours:'36'}, ${now})`);
  assert.equal(out.body.expires_at, '2030-01-03T00:00:00Z');
  const frac = run(sandbox, `_lifecycleSubBuild({kinds:['prd.failed'], sinkType:'alert', target:'x', expiryHours:'0.5'}, ${now})`);
  assert.equal(frac.body.expires_at, '2030-01-01T12:30:00Z');
  const none = run(sandbox, `_lifecycleSubBuild({kinds:['prd.failed'], sinkType:'alert', target:'x', expiryHours:''}, ${now})`);
  assert.equal(none.body.expires_at, undefined);
});

test('lifecycle subscriptions form: edit shows remaining hours and keeps an untouched expiry exactly', async () => {
  const soon = new Date(Date.now() + 5 * 3600000 + 60000).toISOString();
  const sub = { id: 's-exp', filter: { prd_id: 'p1', level: 'prd' }, sink: { type: 'alert', target: 'lbl' }, expires_at: soon };
  const s = formSetup([['PUT', /s-exp$/, () => fakeResponse(200, sub)], ['GET', /subscriptions$/, () => fakeResponse(200, [])]]);
  openForm(s.sandbox, sub);
  const hours = run(s.sandbox, `_lifecycleSubFormCtx.initialExpiry`);
  assert.ok(Number(hours) > 5 && Number(hours) < 5.1, hours);
  assert.match(s.html(), new RegExp(`id="lcSubExpiry" type="text" inputmode="decimal" class="form-input" value="${hours}"`));
  fill(s, { level: 'prd', scope: 'this', sinkType: 'alert', target: 'lbl', expiry: hours });
  await submit(s.sandbox);
  assert.equal(s.requests.find(r => r.method === 'PUT').body.expires_at, soon);
  // An expired record shows an empty field.
  assert.equal(run(s.sandbox, `_lifecycleSubExpiryHours('2000-01-01T00:00:00Z', Date.now())`), '');
});

test('lifecycle subscriptions form: dynamic values in the prefilled form are escaped', () => {
  const evil = { id: 'x', filter: { prd_id: 'p1', level: 'prd', task_id: '"><script>alert(1)</script>' },
    sink: { type: 'session', target: '"><img src=x onerror=alert(2)>' }, action: { type: 'send_input', text: '</textarea><script>alert(3)</script>' } };
  const s = formSetup([]);
  openForm(s.sandbox, evil);
  const html = s.html();
  assert.ok(!html.includes('<script>'), html);
  assert.ok(!html.includes('<img src=x'), html);
  assert.match(html, /&lt;\/textarea&gt;&lt;script&gt;/);
});

test('lifecycle subscriptions form: Add button and edit keep the owning Automaton in the request', async () => {
  const s = formSetup([['POST', /subscriptions$/, () => fakeResponse(201, {})], ['GET', /subscriptions$/, () => fakeResponse(200, [])]]);
  run(s.sandbox, `_lifecycleSubOpenForm('p1', '')`);
  fill(s, { kinds: ['prd.running'], sinkType: 'topic', target: 'ops' });
  await submit(s.sandbox);
  const post = s.requests.find(r => r.method === 'POST');
  assert.equal(post.body.filter.prd_id, 'p1');
  assert.equal(post.body.sink.type, 'topic');
});
