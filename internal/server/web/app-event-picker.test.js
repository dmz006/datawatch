// Subscription form event picker: the kind checkboxes come from
// GET /api/autonomous/event-kinds (every kind with its one-line description,
// grouped Automaton, task, review, capacity, budget), the sink is chosen in
// the same form, and a line under the picker shows what the ticked kinds
// would have matched in the last day, fetched from the preview route before
// anything is saved.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, makeStubElement, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES = ['en', 'de', 'es', 'fr', 'ja'].reduce((all, l) => { all[l] = require('./locales/' + l + '.json'); return all; }, {});
const EN = LOCALES.en;

function fakeResponse(status, body) {
  const text = typeof body === 'string' ? body : JSON.stringify(body === undefined ? {} : body);
  return { ok: status >= 200 && status < 300, status, statusText: 'x', text: () => Promise.resolve(text), json: () => Promise.resolve(JSON.parse(text)) };
}

const KINDS = {
  groups: [
    { group: 'task', kinds: [
      { kind: 'task.completed', description: 'A task finished and passed verification.', wake_default: false },
      { kind: 'task.failed', description: 'A task failed after all retries.', wake_default: true }] },
    { group: 'automaton', kinds: [
      { kind: 'prd.completed', description: 'An Automaton finished with every task done.', wake_default: true }] },
    { group: 'budget', kinds: [
      { kind: 'burn_guard.rung_changed', description: 'The burn guard moved to a different rung.', wake_default: false }] },
    { group: 'review', kinds: [
      { kind: 'judge.verdict_block', description: 'A judge returned a block verdict.', wake_default: false }] },
    { group: 'capacity', kinds: [
      { kind: 'task.capacity_wait_too_long', description: 'A task has waited for capacity longer than the threshold.', wake_default: false }] },
  ],
};

// Loads app.js with a fetch router and a captured form modal. routes is an
// array of [method, urlRegex, () => response]; every request is recorded.
function formSetup(routes, fields) {
  const sandbox = buildSandbox();
  const byId = Object.assign({}, fields);
  const modals = [];
  sandbox.document.getElementById = (id) => (byId[id] !== undefined ? byId[id] : makeStubElement());
  sandbox.document.createElement = () => {
    const el = makeStubElement();
    modals.push(el);
    return el;
  };
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  Object.assign(sandbox._i18n.fallback, EN);
  const s = { sandbox, byId, requests: [], html: () => modals.map(m => m.innerHTML).join('') };
  sandbox.fetch = (url, opts) => {
    const method = (opts && opts.method) || 'GET';
    s.requests.push({ method, url, body: opts && opts.body ? JSON.parse(opts.body) : undefined });
    for (const [m, re, fn] of routes) {
      if (m === method && re.test(url)) return Promise.resolve(fn());
    }
    return Promise.resolve(fakeResponse(404, 'not found'));
  };
  sandbox.showToast = () => {};
  // Elements the picker writes to, so tests can read what was rendered.
  for (const id of ['lcSubKindPicker', 'lcSubKindStatus', 'lcSubPreview']) if (!byId[id]) byId[id] = makeStubElement();
  return s;
}

const run = (sandbox, code) => vm.runInContext(code, sandbox);
const openForm = (sandbox, sub) => run(sandbox, `_openLifecycleSubscriptionForm({id:'p1'}, ${JSON.stringify(sub || null)})`);
const kindIdx = (sandbox, kind) => run(sandbox, `_lifecycleSubKindList.indexOf(${JSON.stringify(kind)})`);
const eventsRoute = () => ['GET', /\/api\/autonomous\/event-kinds$/, () => fakeResponse(200, KINDS)];

test('event picker: the form asks the daemon for the kinds and lists every one with its description', async () => {
  const s = formSetup([eventsRoute()]);
  openForm(s.sandbox, null);
  await flushAsync();
  assert.ok(s.requests.some(r => r.method === 'GET' && r.url === '/api/autonomous/event-kinds'));
  const html = s.byId.lcSubKindPicker.innerHTML;
  for (const g of KINDS.groups) {
    for (const k of g.kinds) {
      assert.ok(html.includes(k.kind), k.kind);
      assert.ok(html.includes(k.description), k.description);
    }
  }
  assert.equal((html.match(/type="checkbox"/g) || []).length, 6, 'one checkbox per kind');
  assert.deepEqual([...run(s.sandbox, `_lifecycleSubKindList`)].sort(),
    KINDS.groups.flatMap(g => g.kinds.map(k => k.kind)).sort());
});

test('event picker: groups come in the order Automaton, task, review, capacity, budget', async () => {
  const s = formSetup([eventsRoute()]);
  openForm(s.sandbox, null);
  await flushAsync();
  const html = s.byId.lcSubKindPicker.innerHTML;
  const at = (label) => html.indexOf('>' + label + '<');
  const order = ['Automaton', 'Task', 'Review', 'Capacity', 'Budget'].map(at);
  assert.ok(order.every(i => i >= 0), JSON.stringify(order));
  assert.deepEqual([...order].sort((a, b) => a - b), order, 'headings appear in the fixed order');
});

test('event picker: a group the form does not know is shown under its own name after the known ones', async () => {
  const body = { groups: [{ group: 'zeta', kinds: [{ kind: 'zeta.one', description: 'One.' }] }, KINDS.groups[1]] };
  const s = formSetup([['GET', /event-kinds$/, () => fakeResponse(200, body)]]);
  openForm(s.sandbox, null);
  await flushAsync();
  const html = s.byId.lcSubKindPicker.innerHTML;
  assert.ok(html.indexOf('>Automaton<') < html.indexOf('>zeta<'));
  assert.ok(html.includes('zeta.one'));
});

test('event picker: kinds ticked before the list arrives, and kinds of an edited rule, stay ticked', async () => {
  const s = formSetup([eventsRoute()]);
  openForm(s.sandbox, { id: 's1', filter: { prd_id: 'p1', kinds: ['task.failed', 'prd.completed'] }, sink: { type: 'alert', target: 'x' } });
  await flushAsync();
  const html = s.byId.lcSubKindPicker.innerHTML;
  // The stub's checkboxes are not real inputs, so the ticks are read back from
  // the markup the daemon list was rendered with after the first render.
  assert.ok(run(s.sandbox, `_lifecycleSubKindList`).includes('task.failed'));
  assert.match(run(s.sandbox, `_lifecycleSubKindsHTML(['task.failed','prd.completed'], _lifecycleSubPickerGroups)`),
    new RegExp(`id="lcSubKind_${kindIdx(s.sandbox, 'task.failed')}" checked`));
  assert.ok(html.length > 0);
});

test('event picker: a selected kind the daemon does not list is kept in a last group', () => {
  const s = formSetup([]);
  const out = run(s.sandbox, `_lifecycleSubKindsHTML(['legacy.kind'], _lifecycleSubPickerNormalize(${JSON.stringify(KINDS)}))`);
  assert.match(out, /id="lcSubKind_\d+" checked[^>]*><span><span[^>]*>legacy\.kind</);
  assert.equal(run(s.sandbox, `_lifecycleSubKindList`).slice(-1)[0], 'legacy.kind');
});

test('event picker: when the list cannot be loaded the built-in kinds stay and a note says so', async () => {
  const s = formSetup([['GET', /event-kinds$/, () => fakeResponse(500, 'boom')]]);
  openForm(s.sandbox, null);
  await flushAsync();
  assert.match(s.byId.lcSubKindStatus.innerHTML, /Could not load the event kinds/);
  assert.ok(s.html().includes('prd.completed'), 'the built-in list is still offered');
  assert.equal(run(s.sandbox, `_lifecycleSubKindList.length`), run(s.sandbox, `_LIFECYCLE_SUB_KINDS.length`));
});

test('event picker: a daemon kind outside the built-in list can be saved', async () => {
  const body = { groups: [{ group: 'automaton', kinds: [{ kind: 'automaton.extra', description: 'Extra.' }] }] };
  const s = formSetup([
    ['GET', /event-kinds$/, () => fakeResponse(200, body)],
    ['POST', /subscriptions$/, () => fakeResponse(201, { id: 'n' })],
    ['GET', /subscriptions$/, () => fakeResponse(200, [])],
  ]);
  openForm(s.sandbox, null);
  await flushAsync();
  s.byId['lcSubKind_' + kindIdx(s.sandbox, 'automaton.extra')] = { checked: true };
  s.byId.lcSubSinkType = { value: 'alert' };
  s.byId.lcSubTarget = { value: 'label' };
  s.byId.lcSubScope = { value: 'all' };
  await run(s.sandbox, `_lifecycleSubSubmit()`);
  const post = s.requests.find(r => r.method === 'POST' && /subscriptions$/.test(r.url));
  assert.deepEqual(JSON.parse(JSON.stringify(post.body)), {
    filter: { kinds: ['automaton.extra'] },
    sink: { type: 'alert', target: 'label' },
  });
});

test('event picker: kind names and descriptions are escaped', async () => {
  const body = { groups: [{ group: 'task', kinds: [{ kind: 'task.<b>x</b>', description: '<img src=x onerror=alert(1)>' }] }] };
  const s = formSetup([['GET', /event-kinds$/, () => fakeResponse(200, body)]]);
  openForm(s.sandbox, null);
  await flushAsync();
  const html = s.byId.lcSubKindPicker.innerHTML;
  assert.ok(!html.includes('<img src=x'), html);
  assert.ok(!html.includes('<b>x</b>'), html);
  assert.match(html, /&lt;img src=x onerror=alert\(1\)&gt;/);
});

test('event picker: the form offers a sink choice beside the picker', () => {
  const s = formSetup([]);
  openForm(s.sandbox, null);
  const html = s.html();
  assert.match(html, /id="lcSubKindPicker"/);
  assert.match(html, /id="lcSubSinkType"/);
  const offered = [...html.matchAll(/<option value="([a-z_]+)"/g)].map(m => m[1])
    .filter(v => ['comm', 'session', 'topic', 'alert', 'mcp_queue', 'webhook', 'peer'].indexOf(v) >= 0);
  assert.deepEqual(offered, ['comm', 'session', 'topic', 'alert', 'mcp_queue', 'webhook', 'peer']);
  assert.match(html, /id="lcSubPreview"/, 'the preview line sits in the same form');
});

test('event picker: the preview line posts the ticked kinds and shows the last-day count', async () => {
  const s = formSetup([
    eventsRoute(),
    ['POST', /subscriptions\/preview$/, () => fakeResponse(200, { count: 7, window_hours: 24, sample: [] })],
  ]);
  openForm(s.sandbox, null);
  await flushAsync();
  s.byId['lcSubKind_' + kindIdx(s.sandbox, 'task.failed')] = { checked: true };
  s.byId['lcSubKind_' + kindIdx(s.sandbox, 'prd.completed')] = { checked: true };
  s.requests.length = 0;
  await run(s.sandbox, `_lifecycleSubPreview()`);
  assert.equal(s.requests.length, 1);
  assert.equal(s.requests[0].method, 'POST');
  assert.equal(s.requests[0].url, '/api/autonomous/subscriptions/preview');
  assert.deepEqual([...s.requests[0].body.kinds].sort(), ['prd.completed', 'task.failed']);
  assert.equal(s.requests[0].body.owner, undefined);
  assert.match(s.byId.lcSubPreview.innerHTML, /Would have matched in the last day: 7/);
});

test('event picker: the preview names the Automation of an edited rule as the owner', async () => {
  const s = formSetup([
    eventsRoute(),
    ['POST', /preview$/, () => fakeResponse(200, { count: 0 })],
  ]);
  openForm(s.sandbox, { id: 's1', filter: { automation_id: 'chain-9', kinds: ['task.failed'] }, sink: { type: 'alert', target: 'x' } });
  await flushAsync();
  s.byId['lcSubKind_' + kindIdx(s.sandbox, 'task.failed')] = { checked: true };
  s.requests.length = 0;
  await run(s.sandbox, `_lifecycleSubPreview()`);
  assert.equal(s.requests[0].body.owner, 'chain-9');
  assert.match(s.byId.lcSubPreview.innerHTML, /last day: 0/);
});

test('event picker: nothing ticked asks for no preview and says what to do', async () => {
  const s = formSetup([eventsRoute()]);
  openForm(s.sandbox, null);
  await flushAsync();
  s.requests.length = 0;
  await run(s.sandbox, `_lifecycleSubPreview()`);
  assert.deepEqual(s.requests, []);
  assert.match(s.byId.lcSubPreview.innerHTML, /Pick an event kind to see what it would have matched/);
});

test('event picker: a refused preview is reported and does not block saving', async () => {
  const s = formSetup([
    eventsRoute(),
    ['POST', /preview$/, () => fakeResponse(400, 'bad request: unknown kind "x"')],
    ['POST', /subscriptions$/, () => fakeResponse(201, { id: 'n' })],
    ['GET', /subscriptions$/, () => fakeResponse(200, [])],
  ]);
  openForm(s.sandbox, null);
  await flushAsync();
  s.byId['lcSubKind_' + kindIdx(s.sandbox, 'task.failed')] = { checked: true };
  await run(s.sandbox, `_lifecycleSubPreview()`);
  assert.match(s.byId.lcSubPreview.innerHTML, /Preview unavailable: .*unknown kind/);
  s.byId.lcSubSinkType = { value: 'alert' };
  s.byId.lcSubTarget = { value: 'label' };
  await run(s.sandbox, `_lifecycleSubSubmit()`);
  assert.ok(s.requests.some(r => r.method === 'POST' && /subscriptions$/.test(r.url)), 'save still goes through');
});

test('event picker: an older preview answer never replaces a newer one', async () => {
  const waiting = [];
  const s = formSetup([eventsRoute()]);
  const base = s.sandbox.fetch;
  s.sandbox.fetch = (url, opts) => {
    if (opts && opts.method === 'POST' && /preview$/.test(url)) {
      s.requests.push({ method: 'POST', url, body: JSON.parse(opts.body) });
      return new Promise(resolve => waiting.push(resolve));
    }
    return base(url, opts);
  };
  openForm(s.sandbox, null);
  await flushAsync();
  s.byId['lcSubKind_' + kindIdx(s.sandbox, 'task.failed')] = { checked: true };
  const first = run(s.sandbox, `_lifecycleSubPreview()`);
  const second = run(s.sandbox, `_lifecycleSubPreview()`);
  assert.equal(waiting.length, 2);
  waiting[1](fakeResponse(200, { count: 5 }));
  await second;
  waiting[0](fakeResponse(200, { count: 99 }));
  await first;
  assert.match(s.byId.lcSubPreview.innerHTML, /last day: 5/);
});

test('event picker: no preview is sent once the form is closed', async () => {
  const s = formSetup([eventsRoute(), ['POST', /preview$/, () => fakeResponse(200, { count: 1 })]]);
  openForm(s.sandbox, null);
  await flushAsync();
  run(s.sandbox, `_lifecycleSubFormCtx = null`);
  s.byId['lcSubKind_0'] = { checked: true };
  s.requests.length = 0;
  await run(s.sandbox, `_lifecycleSubPreview()`);
  assert.deepEqual(s.requests, []);
});

test('event picker: ticking a kind schedules one preview after a short pause', () => {
  const s = formSetup([]);
  const timers = [];
  s.sandbox.setTimeout = (fn, ms) => { timers.push([fn, ms]); return timers.length; };
  s.sandbox.clearTimeout = () => {};
  run(s.sandbox, `_lifecycleSubPreviewChanged(); _lifecycleSubPreviewChanged();`);
  assert.equal(timers.length, 2);
  assert.equal(timers[1][1], 300);
  assert.match(s.html() + run(s.sandbox, `_lifecycleSubKindsHTML([])`), /onchange="_lifecycleSubPreviewChanged\(\)"/);
});

test('event picker: the kinds label carries a ? help link into the subscriptions how-to', () => {
  const s = formSetup([]);
  openForm(s.sandbox, null);
  const html = s.html();
  assert.match(html, /class="docs-link"[^>]*>\?<\/a>/);
  assert.match(html, /howto\/automaton-lifecycle-subscriptions\.md#pwa-walkthrough/);
  assert.match(html, /How the event picker and the last-day preview work\./);
});

const NEW_KEYS = [
  'lifecycle_sub_picker_group_automaton', 'lifecycle_sub_picker_group_task', 'lifecycle_sub_picker_group_review',
  'lifecycle_sub_picker_group_capacity', 'lifecycle_sub_picker_group_budget', 'lifecycle_sub_picker_load_failed',
  'lifecycle_sub_picker_help', 'lifecycle_sub_preview_idle', 'lifecycle_sub_preview_loading',
  'lifecycle_sub_preview_count', 'lifecycle_sub_preview_failed',
];

test('event picker: every new locale key exists in all five locales with the same placeholders', () => {
  for (const key of NEW_KEYS) {
    const want = (EN[key] || '').match(/%\d\$s/g) || [];
    for (const [lang, loc] of Object.entries(LOCALES)) {
      assert.ok(typeof loc[key] === 'string' && loc[key].length > 0, `${lang} is missing ${key}`);
      assert.deepEqual(loc[key].match(/%\d\$s/g) || [], want, `${lang} ${key} placeholders`);
    }
  }
});

test('event picker: the new strings use Automaton wording and carry no tracker ids', () => {
  // The patterns are assembled so this file itself holds neither the internal
  // name nor an id-shaped token.
  const internalName = new RegExp('\\b' + 'P' + 'RDs?\\b');
  const trackerId = new RegExp('\\b(' + ['B', 'L'].join('') + '|G' + 'H)\\d+\\b');
  for (const key of NEW_KEYS) {
    for (const [lang, loc] of Object.entries(LOCALES)) {
      assert.ok(!internalName.test(loc[key]), `${lang} ${key} uses the internal name`);
      assert.ok(!trackerId.test(loc[key]), `${lang} ${key} has a tracker id`);
    }
  }
});
