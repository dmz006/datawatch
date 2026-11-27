// Requests card (Settings → Automata) and the six request settings.
//
// The card draws GET /api/requests and GET /api/requests/roles with the
// contract's plain readings in five locales. Only a request datawatch's check
// proved reads "done". Lists hold no request content.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const HOSTILE = `x'"><img src=x onerror=alert(1)>`;

// The contract's readings, with the state and proof each belongs to.
const LABELS = [
  ['waiting', 'waiting', ''], ['typed, unconfirmed', 'waiting', ''], ['fetched, unconfirmed', 'waiting', ''],
  ['delivered to the holder', 'delivered', ''], ['in a completed model call', 'delivered', ''],
  ['accepted by the holder', 'accepted', ''], ['reported: finished, not accepted', 'reported', ''],
  ['done', 'done', 'done'], ['delivered (not the same as done)', 'done', 'delivered'],
  ['done, accepted without proof', 'done', 'accepted_unproven'],
  ['failed', 'failed', ''], ['expired', 'expired', ''], ['cancelled', 'cancelled', ''],
];

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
}

function view(over) {
  return Object.assign({
    id: 'r-1a2b', kind: 'ask', class: 'ask', role: 'supervisor', scope: { kind: 'project', id: 'web' },
    state: 'waiting', label: 'waiting',
  }, over || {});
}

function loadApp(routes, lang) {
  const panel = makeStubElement();
  const sandbox = buildSandbox({ byId: { requestsPanel: panel } });
  const calls = [];
  const confirms = [];
  sandbox.fetch = (url, opts) => {
    const method = (opts && opts.method) || 'GET';
    const key = method + ' ' + url;
    if (String(url).startsWith('/api/requests')) calls.push({ key, body: opts && opts.body ? JSON.parse(opts.body) : null });
    const r = Object.prototype.hasOwnProperty.call(routes || {}, key) ? routes[key] : { status: 404, text: 'no such request' };
    if (r && r.status && r.text !== undefined) {
      return Promise.resolve({ ok: false, status: r.status, statusText: '', text: () => Promise.resolve(r.text) });
    }
    return Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(r), text: () => Promise.resolve(JSON.stringify(r)) });
  };
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle(lang || 'en');
  sandbox.localStorage = { getItem: () => null };
  sandbox.showConfirmModal = (msg, yes) => confirms.push({ msg, yes });
  return { sandbox, panel, calls, confirms };
}

const run = (sb, code) => vm.runInContext(code, sb);
const plain = (v) => JSON.parse(JSON.stringify(v));

const lists = (requests, roles) => ({
  'GET /api/requests': { requests: requests || [] },
  'GET /api/requests/roles': { roles: roles || [] },
});

async function loaded(routes, lang) {
  const app = loadApp(routes, lang);
  await app.sandbox.loadRequestsPanel();
  await flushAsync();
  return app;
}

test('every contract reading is shown from its own locale entry, in five locales', () => {
  for (const lang of LOCALES) {
    const { sandbox } = loadApp({}, lang);
    const b = bundle(lang);
    const shown = new Set();
    for (const [label, state, proof] of LABELS) {
      const got = sandbox.requestLabelText(view({ label, state, proof_as: proof }));
      assert.ok(got, `${lang} ${label}`);
      if (lang === 'en') assert.equal(got, label);
      assert.ok(Object.values(b).includes(got), `${lang} ${label}: ${got} is not a locale entry`);
      shown.add(got);
    }
    assert.equal(shown.size, LABELS.length, `${lang}: two readings look the same`);
  }
});

test('only proven done reads done', () => {
  for (const lang of LOCALES) {
    const { sandbox } = loadApp({}, lang);
    const done = sandbox.requestLabelText(view({ label: 'done', state: 'done', proof_as: 'done' }));
    for (const v of [
      view({ label: 'done', state: 'reported' }),
      view({ label: 'done', state: 'accepted', proof_as: 'done' }),
      view({ label: 'done', state: 'done', proof_as: 'delivered' }),
      view({ label: 'done', state: 'done', proof_as: 'accepted_unproven' }),
      view({ label: 'done', state: 'done' }),
      view({ label: '', state: 'reported' }),
      view({ label: 'all done', state: 'reported' }),
    ]) {
      const got = sandbox.requestLabelText(v);
      assert.notEqual(got, done, `${lang} ${v.state}/${v.proof_as}`);
      assert.notEqual(got, 'all done');
      assert.ok(got);
    }
  }
});

test('the list shows readings, codes and holders, escaped, and the roles', async () => {
  const { panel, calls } = await loaded(lists([
    view({ id: 'r-aa', label: 'fetched, unconfirmed', reason_code: 'take_not_proven', overdue: true, may_run_twice: true, holder: { session_id: 'host-1a2b' } }),
    view({ id: HOSTILE, label: 'waiting', scope: { kind: 'project', id: HOSTILE }, reason_code: HOSTILE }),
  ], [{ role: 'supervisor', scope: { kind: 'project', id: 'web' }, session_id: 'host-1a2b', self_given: true }]));
  assert.deepEqual(calls.map(c => c.key).sort(), ['GET /api/requests', 'GET /api/requests/roles']);
  for (const want of ['r-aa', 'fetched, unconfirmed', 'take_not_proven', 'host-1a2b', 'supervisor@project:web', bundle('en').requests_row_overdue, bundle('en').requests_role_unproven]) {
    assert.ok(panel.innerHTML.includes(want), 'lacks ' + want);
  }
  assert.ok(!panel.innerHTML.includes('<img src=x'));
});

test('an empty list and a refusal each say so', async () => {
  const empty = await loaded(lists());
  assert.ok(empty.panel.innerHTML.includes(bundle('en').requests_empty));
  assert.ok(empty.panel.innerHTML.includes(bundle('en').requests_roles_empty));
  const why = 'tracked requests to or from another datawatch computer are not supported yet; nothing was saved';
  const refused = await loaded({ 'GET /api/requests': { status: 422, text: why } });
  assert.ok(refused.panel.innerHTML.includes(why));
});

test('the state filter asks the daemon', async () => {
  const routes = lists();
  routes['GET /api/requests?state=waiting'] = { requests: [view({ id: 'r-wait' })] };
  const { sandbox, panel, calls } = await loaded(routes);
  await sandbox.requestsFilter('waiting');
  await flushAsync();
  assert.ok(calls.some(c => c.key === 'GET /api/requests?state=waiting'));
  assert.ok(panel.innerHTML.includes('r-wait'));
});

test('operator actions need a choice and a reason code; accept unproven needs words', async () => {
  const routes = lists([view({ id: 'r-1a2b' })]);
  for (const verb of ['reissue', 'drop', 'force', 'accept-unproven']) routes[`POST /api/requests/r-1a2b/${verb}`] = view({ id: 'r-1a2b', state: 'cancelled', label: 'cancelled' });
  const { sandbox, calls, confirms, panel } = await loaded(routes);
  const posts = () => calls.filter(c => c.key.startsWith('POST'));
  await sandbox.requestsAction('reissue');
  assert.ok(panel.innerHTML.includes(bundle('en').requests_need_selection));
  sandbox.requestsSelect('r-1a2b');
  await sandbox.requestsAction('reissue');
  assert.ok(panel.innerHTML.includes(bundle('en').requests_need_reason));
  sandbox.requestsField('reason', 'obsolete');
  await sandbox.requestsAction('accept-unproven');
  assert.ok(panel.innerHTML.includes(bundle('en').requests_need_words));
  assert.equal(posts().length, 0);
  sandbox.requestsField('words', 'I looked');
  await sandbox.requestsAction('accept-unproven');
  await flushAsync();
  assert.deepEqual(plain(posts().pop().body), { reason_code: 'obsolete', words: 'I looked' });

  for (const [verb, field, value, body] of [
    ['reissue', 'repeatOK', true, { reason_code: 'obsolete', repeat_ok: true }],
    ['drop', null, null, { reason_code: 'obsolete' }],
    ['force', 'toSession', 'host-2', { reason_code: 'obsolete', to_session: 'host-2' }],
  ]) {
    sandbox.requestsSelect('r-1a2b');
    sandbox.requestsField('reason', 'obsolete');
    if (field) sandbox.requestsField(field, value);
    const before = posts().length;
    await sandbox.requestsAction(verb);
    if (verb === 'drop' || verb === 'force') {
      assert.equal(posts().length, before, verb + ' asks first');
      await confirms.pop().yes();
    }
    await flushAsync();
    assert.equal(posts().pop().key, `POST /api/requests/r-1a2b/${verb}`);
    assert.deepEqual(plain(posts().pop().body), body);
  }
});

test('give and release a role', async () => {
  const routes = lists([], [{ role: 'supervisor', scope: { kind: 'project', id: 'web' }, session_id: 'host-1' }]);
  routes['POST /api/requests/roles/give'] = { role: 'supervisor' };
  routes['POST /api/requests/roles/release'] = { ok: true };
  const { sandbox, calls, panel } = await loaded(routes);
  await sandbox.requestsRoleGive();
  assert.ok(panel.innerHTML.includes(bundle('en').requests_role_need_fields));
  sandbox.requestsField('roleScope', 'web');
  sandbox.requestsField('roleSession', 'host-2');
  sandbox.requestsField('roleForce', true);
  sandbox.requestsField('roleReason', 'stuck');
  await sandbox.requestsRoleGive();
  await flushAsync();
  assert.deepEqual(plain(calls.find(c => c.key === 'POST /api/requests/roles/give').body),
    { role: 'supervisor', scope: { kind: 'project', id: 'web' }, session_id: 'host-2', force: true, reason_code: 'stuck' });
  await sandbox.requestsRoleRelease(0);
  await flushAsync();
  assert.deepEqual(plain(calls.find(c => c.key === 'POST /api/requests/roles/release').body),
    { role: 'supervisor', scope: { kind: 'project', id: 'web' }, session_id: 'host-1' });
});

test('the settings page has the card and the six settings under Automata', () => {
  const { sandbox } = loadApp({});
  const sec = plain(run(sandbox, "GENERAL_CONFIG_FIELDS.find(s => s.id === 'request_settings')"));
  assert.deepEqual(sec.fields.map(f => [f.key, f.type]), [
    ['delivery.request_take_by_seconds', 'number'], ['delivery.request_due_seconds', 'number'],
    ['delivery.request_due_seconds_by_kind', 'json_map'], ['delivery.reminder_interval_seconds', 'number'],
    ['delivery.reminders_per_session_hour', 'number'], ['delivery.reminders_per_host_hour', 'number']]);
  for (const f of sec.fields.filter(f => f.type === 'number')) {
    assert.equal(f.min, 0);
    assert.ok(f.onchange.includes(`savePromptNumberSetting('${f.key}', this.value, 0)`), f.onchange);
  }
  assert.deepEqual(sec.fields.filter(f => f.type === 'number').map(f => f.placeholder), ['3600', '86400', '900', '2', '20']);
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.match(src, /_automataIds = \[[^\]]*'request_settings'/);
  assert.match(src, /id="requestsPanel"/);
  assert.match(src, /loadRequestsPanel\(\{ fresh: true \}\)/);
});

test('every request key the page uses is in all five locales', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const keys = new Set([...src.matchAll(/'((?:requests?_|settings_delivery_re)[a-z0-9_]+)'/g)].map(m => m[1]));
  keys.delete('request_settings'); // the settings section id
  keys.delete('requests_list'); // the card's collapse id
  const en = bundle('en');
  const mine = [...keys].filter(k => k.startsWith('requests_') || k.startsWith('request_label_') || k.startsWith('request_settings_') || k.startsWith('settings_delivery_re'));
  assert.ok(mine.length >= 50, 'found ' + mine.length);
  for (const lang of LOCALES) {
    const b = bundle(lang);
    for (const k of mine) {
      assert.ok(b[k], `${lang} lacks ${k}`);
      assert.ok(!/\bPRD\b|\bBL\d|\bF\d{2}\b/.test(b[k]), `${lang} ${k}: ${b[k]}`);
      const holes = (s) => (s.match(/%\d\$s/g) || []).sort().join();
      assert.equal(holes(b[k]), holes(en[k]), `${lang} ${k} placeholders`);
    }
  }
});
