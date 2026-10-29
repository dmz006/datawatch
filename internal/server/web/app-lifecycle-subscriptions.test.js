// Automaton detail "Subscriptions" tab: lists the lifecycle event
// subscriptions that apply to one Automaton (scoped to its id, plus
// unscoped "all automata" ones) with per-row Delete and Test. REST side
// is covered in internal/server (autonomous_subscriptions tests).

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, makeStubElement, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const EN = require('./locales/en.json');

function fakeResponse(status, body) {
  const text = typeof body === 'string' ? body : JSON.stringify(body === undefined ? {} : body);
  return { ok: status >= 200 && status < 300, status, statusText: 'x', text: () => Promise.resolve(text), json: () => Promise.resolve(JSON.parse(text)) };
}

// The /api/config shape the form reads for the live webhook and peer state.
function cfgWith({ webhook = false, hosts = [], forwarding = false } = {}) {
  return { autonomous: { notify: { webhook: { enabled: webhook, allow_hosts: hosts }, forwarding: { enabled: forwarding } } } };
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
  const set = { level: 'lcSubLevel', scope: 'lcSubScope', taskId: 'lcSubTaskId', sinkType: 'lcSubSinkType', target: 'lcSubTarget', actionText: 'lcSubActionText', expiry: 'lcSubExpiry', secretRef: 'lcSubSecretRef', peer: 'lcSubPeer' };
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

test('lifecycle subscriptions form: the sink picker offers webhook and peer after the existing sinks, default still comm', () => {
  const s = formSetup([]);
  openForm(s.sandbox, null);
  const html = s.html();
  const order = ['comm', 'session', 'topic', 'alert', 'mcp_queue', 'webhook', 'peer'];
  const offered = [...html.matchAll(/<option value="([a-z_]+)"/g)].map(m => m[1]).filter(v => order.indexOf(v) >= 0);
  assert.deepEqual(offered, order, 'existing sinks keep their order; webhook and peer come last');
  assert.deepEqual([...run(s.sandbox, `_LIFECYCLE_SUB_SINKS`)], order);
  assert.match(html, /<option value="comm" selected>/);
  const kinds = run(s.sandbox, `_LIFECYCLE_SUB_KINDS`);
  assert.equal(kinds.length, 33);
  for (const k of ['prd.draft', 'prd.paused', 'task.awaiting_approval', 'task.review_needed', 'task.session_started', 'task.session_cleared', 'scope_warning', 'guardrail_warn', 'prd.updated', 'question.answered', 'question.withdrawn']) {
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

test('lifecycle subscriptions form: the webhook and peer fields belong to their own sink only', () => {
  const s = formSetup([]);
  openForm(s.sandbox, null);
  const target = { style: {} }, peer = { style: {} }, box = { style: {} }, label = { textContent: '' };
  Object.assign(s.byId, { lcSubTarget: target, lcSubPeer: peer, lcSubWebhookBox: box, lcSubTargetLabel: label, lcSubSinkHelp: makeStubElement() });
  const type = { value: 'webhook' };
  s.byId.lcSubSinkType = type;
  run(s.sandbox, `_lifecycleSubSinkChanged()`);
  assert.equal(label.textContent, 'Webhook URL');
  assert.equal(box.style.display, '', 'secret reference shown for webhook');
  assert.equal(target.style.display, '', 'URL box shown for webhook');
  assert.equal(peer.style.display, 'none', 'peer picker hidden for webhook');

  type.value = 'peer';
  run(s.sandbox, `_lifecycleSubSinkChanged()`);
  assert.equal(label.textContent, 'Federation peer');
  assert.equal(box.style.display, 'none', 'secret reference hidden for peer');
  assert.equal(target.style.display, 'none', 'text target hidden for peer');
  assert.equal(peer.style.display, '', 'peer picker shown for peer');

  type.value = 'comm';
  run(s.sandbox, `_lifecycleSubSinkChanged()`);
  assert.equal(box.style.display, 'none');
  assert.equal(target.style.display, '');
  assert.equal(peer.style.display, 'none');
});

test('lifecycle subscriptions form: a webhook subscription sends the URL and the secret reference only', async () => {
  const s = formSetup([
    ['POST', /subscriptions$/, () => fakeResponse(201, { id: 'w' })],
    ['GET', /subscriptions$/, () => fakeResponse(200, [])],
  ]);
  openForm(s.sandbox, null);
  fill(s, { kinds: ['prd.failed'], scope: 'this', sinkType: 'webhook', target: 'https://hooks.example.net/automaton', secretRef: '${secret:hook-token}' });
  await submit(s.sandbox);
  const post = s.requests.find(r => r.method === 'POST');
  assert.deepEqual(JSON.parse(JSON.stringify(post.body)), {
    filter: { kinds: ['prd.failed'], prd_id: 'p1' },
    sink: { type: 'webhook', target: 'https://hooks.example.net/automaton', secret_ref: '${secret:hook-token}' },
  });
});

test('lifecycle subscriptions form: a peer subscription sends the chosen peer name as the target and no secret', async () => {
  const s = formSetup([
    ['POST', /subscriptions$/, () => fakeResponse(201, { id: 'p' })],
    ['GET', /subscriptions$/, () => fakeResponse(200, [])],
  ]);
  openForm(s.sandbox, null);
  // A stray URL and secret typed before switching to peer must not be sent.
  fill(s, { kinds: ['prd.completed'], scope: 'this', sinkType: 'peer', target: 'https://ignored.example.net', peer: 'peer-b', secretRef: '${secret:stray}' });
  await submit(s.sandbox);
  const post = s.requests.find(r => r.method === 'POST');
  assert.deepEqual(JSON.parse(JSON.stringify(post.body)), {
    filter: { kinds: ['prd.completed'], prd_id: 'p1' },
    sink: { type: 'peer', target: 'peer-b' },
  });
  assert.ok(!('secret_ref' in post.body.sink));
});

test('lifecycle subscriptions form: the webhook secret reference must be exactly one ${secret:...} reference', async () => {
  const cases = ['', 'sk-live-123456', 'Bearer ${secret:tok}', '${secret:}', '${secret:a}${secret:b}', '${secret:a} extra'];
  for (const secretRef of cases) {
    const errBox = makeStubElement();
    const s = formSetup([], { lcSubSecretError: errBox, lcSubError: makeStubElement() });
    openForm(s.sandbox, null);
    fill(s, { kinds: ['prd.failed'], scope: 'this', sinkType: 'webhook', target: 'https://hooks.example.net/x', secretRef });
    await submit(s.sandbox);
    assert.match(errBox.innerHTML, /must be one vault reference/, JSON.stringify(secretRef));
    assert.deepEqual(s.requests.filter(r => r.method !== 'GET'), [], 'no write for ' + JSON.stringify(secretRef));
  }
  // A literal value that looks like a secret is refused even with a valid-looking URL.
  const errBox = makeStubElement();
  const s = formSetup([], { lcSubSecretError: errBox });
  openForm(s.sandbox, null);
  fill(s, { kinds: ['prd.failed'], scope: 'this', sinkType: 'webhook', target: 'https://hooks.example.net/x', secretRef: 'hunter2' });
  await submit(s.sandbox);
  assert.match(errBox.innerHTML, /must be one vault reference/);
  assert.ok(!errBox.innerHTML.includes('hunter2'), 'the typed value is not echoed back');
});

test('lifecycle subscriptions form: the webhook URL must be http or https, with the server deciding the host rules', async () => {
  const errBox = makeStubElement();
  const s = formSetup([], { lcSubError: errBox });
  openForm(s.sandbox, null);
  fill(s, { kinds: ['prd.failed'], scope: 'this', sinkType: 'webhook', target: 'ftp://hooks.example.net/x', secretRef: '${secret:tok}' });
  await submit(s.sandbox);
  assert.match(errBox.innerHTML, /must start with https:\/\//);
  assert.deepEqual(s.requests.filter(r => r.method !== 'GET'), []);
});

test('lifecycle subscriptions form: the peer picker lists the federation peers and selects the current one on edit', async () => {
  const sel = makeStubElement();
  const status = makeStubElement();
  const s = formSetup([
    ['GET', /\/api\/federation\/peers$/, () => fakeResponse(200, [{ name: 'peer-a', enabled: true }, { name: 'peer-b', enabled: false }])],
  ], { lcSubPeer: sel, lcSubPeerStatus: status });
  openForm(s.sandbox, null);
  await flushAsync();
  assert.match(sel.innerHTML, /<option value="" selected>Choose a federation peer<\/option>/);
  assert.match(sel.innerHTML, /<option value="peer-a">peer-a<\/option>/);
  assert.match(sel.innerHTML, /<option value="peer-b">peer-b<\/option>/);
  assert.equal(status.innerHTML, '', 'no status line when peers exist');

  const edit = { id: 's-peer', filter: { prd_id: 'p1', level: 'prd' }, sink: { type: 'peer', target: 'peer-z' } };
  const sel2 = makeStubElement();
  const s2 = formSetup([
    ['GET', /\/api\/federation\/peers$/, () => fakeResponse(200, [{ name: 'peer-a' }])],
  ], { lcSubPeer: sel2, lcSubPeerStatus: makeStubElement() });
  openForm(s2.sandbox, edit);
  await flushAsync();
  assert.match(sel2.innerHTML, /<option value="peer-z" selected>peer-z<\/option>/, 'a peer no longer listed stays selectable so the edit does not drop it');
});

test('lifecycle subscriptions form: the peer picker says so when no peers are registered or the list cannot be read', async () => {
  const none = makeStubElement();
  const s = formSetup([['GET', /\/api\/federation\/peers$/, () => fakeResponse(200, [])]], { lcSubPeer: makeStubElement(), lcSubPeerStatus: none });
  openForm(s.sandbox, null);
  await flushAsync();
  assert.match(none.innerHTML, /No federation peers are registered on this server/);

  const failed = makeStubElement();
  const s2 = formSetup([['GET', /\/api\/federation\/peers$/, () => fakeResponse(403, 'forbidden')]], { lcSubPeer: makeStubElement(), lcSubPeerStatus: failed });
  openForm(s2.sandbox, null);
  await flushAsync();
  assert.match(failed.innerHTML, /Could not load the federation peers/);
});

test('lifecycle subscriptions form: a peer submit with no peer chosen is refused without a request', async () => {
  const errBox = makeStubElement();
  const s = formSetup([], { lcSubError: errBox });
  openForm(s.sandbox, null);
  fill(s, { kinds: ['prd.failed'], scope: 'this', sinkType: 'peer', peer: '' });
  await submit(s.sandbox);
  assert.match(errBox.innerHTML, /Choose a federation peer/);
  assert.deepEqual(s.requests.filter(r => r.method !== 'GET'), []);
});

test('lifecycle subscriptions form: webhook help says delivery is off and names the setting to turn on', async () => {
  const help = makeStubElement();
  const s = formSetup([['GET', /\/api\/config$/, () => fakeResponse(200, cfgWith({ webhook: false }))]], { lcSubSinkHelp: help });
  openForm(s.sandbox, null);
  await flushAsync();
  s.byId.lcSubSinkType = { value: 'webhook' };
  run(s.sandbox, `_lifecycleSubSinkChanged()`);
  assert.match(help.innerHTML, /Webhook delivery is off on this server/);
  assert.match(help.innerHTML, /Turn on &quot;Outbound webhook sink: enabled&quot; in Settings/);
});

test('lifecycle subscriptions form: webhook help lists the allow-listed hosts, or says the empty list blocks everything', async () => {
  const help = makeStubElement();
  const s = formSetup([['GET', /\/api\/config$/, () => fakeResponse(200, cfgWith({ webhook: true, hosts: ['hooks.example.net', 'hooks.example.net:8443'] }))]], { lcSubSinkHelp: help });
  openForm(s.sandbox, null);
  await flushAsync();
  s.byId.lcSubSinkType = { value: 'webhook' };
  run(s.sandbox, `_lifecycleSubSinkChanged()`);
  assert.match(help.innerHTML, /Hosts allowed to receive webhook events: hooks\.example\.net, hooks\.example\.net:8443\./);

  const empty = makeStubElement();
  const s2 = formSetup([['GET', /\/api\/config$/, () => fakeResponse(200, cfgWith({ webhook: true, hosts: [] }))]], { lcSubSinkHelp: empty });
  openForm(s2.sandbox, null);
  await flushAsync();
  s2.byId.lcSubSinkType = { value: 'webhook' };
  run(s2.sandbox, `_lifecycleSubSinkChanged()`);
  assert.match(empty.innerHTML, /allow-list is empty, so nothing can receive events/);
  assert.match(empty.innerHTML, /Webhook allow-list \(host or host:port\)/);
});

test('lifecycle subscriptions form: peer help says forwarding is off, names the setting, and states the grant rule when on', async () => {
  const help = makeStubElement();
  const off = formSetup([['GET', /\/api\/config$/, () => fakeResponse(200, cfgWith({ forwarding: false }))]], { lcSubSinkHelp: help });
  openForm(off.sandbox, null);
  await flushAsync();
  off.byId.lcSubSinkType = { value: 'peer' };
  run(off.sandbox, `_lifecycleSubSinkChanged()`);
  assert.match(help.innerHTML, /Peer forwarding is off on this server/);
  assert.match(help.innerHTML, /Peer forwarding of lifecycle events: enabled/);

  const on = makeStubElement();
  const s = formSetup([['GET', /\/api\/config$/, () => fakeResponse(200, cfgWith({ forwarding: true }))]], { lcSubSinkHelp: on });
  openForm(s.sandbox, null);
  await flushAsync();
  s.byId.lcSubSinkType = { value: 'peer' };
  run(s.sandbox, `_lifecycleSubSinkChanged()`);
  assert.match(on.innerHTML, /grant autonomous:write/);
  assert.ok(!/is off/.test(on.innerHTML));
});

test('lifecycle subscriptions form: when the settings cannot be read the help says delivery state is unknown', async () => {
  const help = makeStubElement();
  const s = formSetup([['GET', /\/api\/config$/, () => fakeResponse(500, 'boom')]], { lcSubSinkHelp: help });
  openForm(s.sandbox, null);
  await flushAsync();
  s.byId.lcSubSinkType = { value: 'webhook' };
  run(s.sandbox, `_lifecycleSubSinkChanged()`);
  assert.match(help.innerHTML, /delivery state is unknown/);
});

test('lifecycle subscriptions form: editing a webhook subscription prefills the reference text, never a value', () => {
  const hook = { id: 'w1', filter: { prd_id: 'p1', kinds: ['prd.failed'] }, sink: { type: 'webhook', target: 'https://hooks.example.net/x', secret_ref: '${secret:hook-token}' } };
  const s = formSetup([]);
  openForm(s.sandbox, hook);
  assert.match(s.html(), /id="lcSubSecretRef" type="text" class="form-input" placeholder="\$\{secret:name\}" value="\$\{secret:hook-token\}"/);
  assert.match(s.html(), /<option value="webhook" selected>/);
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
    // Opening the form reads the settings and peers (GET); only a write would be a failure.
    assert.deepEqual(s.requests.filter(r => r.method !== 'GET'), [], 'no write for ' + JSON.stringify(vals));
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
  const before = s.requests.length;
  await submit(s.sandbox);
  assert.match(errBox.innerHTML, /filter kind &quot;prd\.bogus&quot; is not a known event kind/);
  assert.ok(!s.requests.slice(before).some(r => r.method === 'GET'), 'the list is not reloaded on failure');
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

// ── Webhook and peer sinks: rows, test outcome, locales ─────────────────────

test('lifecycle subscriptions: a webhook row shows the secret reference text, never a resolved value', async () => {
  const hook = [{ id: 'w1', filter: { prd_id: 'p1', kinds: ['prd.failed'] },
    sink: { type: 'webhook', target: 'https://hooks.example.net/x', secret_ref: '${secret:hook-token}' } }];
  const { sandbox, body } = setup([['GET', /subscriptions$/, () => fakeResponse(200, hook)]]);
  await run(sandbox, `_loadPRDSubscriptionsTab({id:'p1'})`);
  assert.match(body.innerHTML, /webhook → https:\/\/hooks\.example\.net\/x/);
  assert.match(body.innerHTML, /secret: \$\{secret:hook-token\}/);
});

test('lifecycle subscriptions: a peer row shows the peer name as the target', async () => {
  const peer = [{ id: 'p1s', filter: { prd_id: 'p1', kinds: ['task.failed'] }, sink: { type: 'peer', target: 'peer-b' } }];
  const { sandbox, body } = setup([['GET', /subscriptions$/, () => fakeResponse(200, peer)]]);
  await run(sandbox, `_loadPRDSubscriptionsTab({id:'p1'})`);
  assert.match(body.innerHTML, /peer → peer-b/);
  assert.ok(!/secret:/.test(body.innerHTML));
});

test('lifecycle subscriptions: a webhook test shows the HTTP status and attempts on failure', async () => {
  const { sandbox, result, toasts } = setup([['POST', /s-mine\/test$/, () => fakeResponse(200, { ok: false, sink: 'webhook', error: 'receiver returned 500', status: 500, attempts: 3 })]]);
  await run(sandbox, `_lifecycleSubTest('p1', 's-mine')`);
  assert.match(result.innerHTML, /Test failed: receiver returned 500/);
  assert.match(result.innerHTML, /HTTP status: 500/);
  assert.match(result.innerHTML, /Attempts: 3/);
  assert.equal(toasts[0][1], 'error');
});

test('lifecycle subscriptions: a peer test shows attempts, and no HTTP status when no response arrived', async () => {
  const { sandbox, result, toasts } = setup([['POST', /s-mine\/test$/, () => fakeResponse(200, { ok: true, sink: 'peer', attempts: 1 })]]);
  await run(sandbox, `_lifecycleSubTest('p1', 's-mine')`);
  assert.match(result.innerHTML, /Delivered via peer/);
  assert.match(result.innerHTML, /Attempts: 1/);
  assert.ok(!/HTTP status/.test(result.innerHTML), 'no status when the test got no HTTP response');
  assert.equal(toasts[0][1], 'success');
});

test('lifecycle sink locale keys: present with real translations in all five bundles', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  for (const locale of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
    for (const k of LIFECYCLE_SINK_LOCALE_KEYS) {
      assert.ok(typeof b[k] === 'string' && b[k].trim().length > 0, k + ' missing or empty in ' + locale);
      assert.ok(!/\bAutomat(a|on)s?\b/.test(b[k]), k + ' carries an untranslated Automaton/Automata word in ' + locale);
      assert.ok(!/\bPRD\b/.test(b[k]), k + ' uses the internal term PRD in ' + locale);
      assert.ok(!/\bBL\d+\b/.test(b[k]), k + ' carries a tracker id in ' + locale);
      if (locale !== 'en' && k !== 'lifecycle_sub_form_sink_webhook') {
        // "Webhook" is the same word in these languages; every other key needs its own translation.
        assert.notEqual(b[k], EN[k], k + ' is an English copy in ' + locale);
      }
    }
  }
  for (const k of LIFECYCLE_SINK_LOCALE_KEYS) {
    assert.ok(src.includes(`'${k}'`), k + ' is not used by app.js');
  }
});

test('lifecycle sink locale keys: the same key set as en.json in every bundle', () => {
  const enKeys = LIFECYCLE_SINK_LOCALE_KEYS.filter(k => k in EN).sort();
  assert.equal(enKeys.length, LIFECYCLE_SINK_LOCALE_KEYS.length);
  for (const locale of ['de', 'es', 'fr', 'ja']) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
    assert.deepEqual(LIFECYCLE_SINK_LOCALE_KEYS.filter(k => k in b).sort(), enKeys, locale);
  }
});

// The block added for the webhook and peer sinks. It is grouped in each bundle
// right after lifecycle_sub_form_err_save.
const LIFECYCLE_SINK_LOCALE_KEYS = [
  'lifecycle_sub_form_sink_webhook',
  'lifecycle_sub_form_sink_peer',
  'lifecycle_sub_form_target_webhook',
  'lifecycle_sub_form_target_peer',
  'lifecycle_sub_form_peer_select',
  'lifecycle_sub_form_peer_none',
  'lifecycle_sub_form_peer_load_failed',
  'lifecycle_sub_form_secret_ref',
  'lifecycle_sub_form_secret_help',
  'lifecycle_sub_form_err_webhook_url',
  'lifecycle_sub_form_err_secret_ref',
  'lifecycle_sub_form_err_peer',
  'lifecycle_sub_help_settings_failed',
  'lifecycle_sub_help_webhook_disabled',
  'lifecycle_sub_help_webhook_no_hosts',
  'lifecycle_sub_help_webhook_hosts',
  'lifecycle_sub_help_peer_disabled',
  'lifecycle_sub_help_peer',
  'lifecycle_sub_secret_ref',
  'lifecycle_sub_test_status',
  'lifecycle_sub_test_attempts',
];
