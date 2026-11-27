// Delivery check card (Settings → Automata) and its three settings.
//
// The card draws GET /api/delivery: the result, why it is red, the new,
// backlog and accepted items, and the actions (check now, acknowledge, accept
// with a reason code and date, drop, test message). Rendering, request bodies,
// refusals, the settings fields and the five locale bundles are covered here
// with fetch stubbed and the real English bundle.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const HOSTILE = `x'"><img src=x onerror=alert(1)>`;

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
}

// routes maps "METHOD url" to a body, or to { status, text } for a refusal.
function loadApp(routes) {
  const panel = makeStubElement();
  const sandbox = buildSandbox({ byId: { deliveryPanel: panel } });
  const calls = [];
  const confirms = [];
  sandbox.fetch = (url, opts) => {
    const method = (opts && opts.method) || 'GET';
    const key = method + ' ' + url;
    if (String(url).startsWith('/api/delivery')) {
      calls.push({ key, body: opts && opts.body ? JSON.parse(opts.body) : null });
    }
    const r = Object.prototype.hasOwnProperty.call(routes || {}, key) ? routes[key] : { status: 404, text: 'not found' };
    if (r && r.status && r.text !== undefined) {
      return Promise.resolve({ ok: false, status: r.status, statusText: '', text: () => Promise.resolve(r.text) });
    }
    return Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(r), text: () => Promise.resolve(JSON.stringify(r)) });
  };
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  sandbox.localStorage = { getItem: () => null };
  sandbox.showConfirmModal = (msg, yes) => confirms.push({ msg, yes });
  return { sandbox, panel, calls, confirms };
}

const run = (sb, code) => vm.runInContext(code, sb);
const plain = (v) => JSON.parse(JSON.stringify(v));

function entry(over) {
  return Object.assign({
    key: 'automaton:a1b2:blocked', kind: 'automaton', id: 'a1b2', state: 'blocked', invariant: 'I2',
    reason: 'awaiting_you', waits_on: 'operator', since: '2026-11-20T10:00:00Z', review_by: '2026-11-21T10:00:00Z',
  }, over || {});
}

function report(over) {
  return Object.assign({
    report_only: true, result: 'red', red: ['listed_items'], at: '2026-11-26T12:00:00Z', pass: 7,
    items: [entry()],
    backlog: [entry({ key: 'schedule:s9:failed', kind: 'schedule', id: 's9', state: 'failed', backlog: true })],
    accepted: [entry({ key: 'bug:b3:parked', kind: 'bug', id: 'b3', state: 'parked', accepted: { reason: 'waiting-upstream', until: '2026-12-01T00:00:00Z', by: 'cli' } })],
    by_rule: { I2: 1 }, by_kind: { automaton: 1 }, sources: [{ name: 'automata', ok: true, candidates: 3 }],
    channels: ['ntfy'], heartbeat: 'red', start_mark: '2026-11-26T09:00:00Z', non_operator_actions: 0,
    uncovered: ['other-hosts'],
  }, over || {});
}

async function loaded(routes) {
  const app = loadApp(routes);
  await app.sandbox.loadDeliveryPanel();
  await flushAsync();
  return app;
}

test('the card shows the result, that it blocks nothing, why it is red and the three groups', async () => {
  const { panel, calls } = await loaded({ 'GET /api/delivery': report({ red: ['listed_items', 'all_sends_failed', 'some_new_code'] }) });
  assert.deepEqual(calls.map(c => c.key), ['GET /api/delivery']);
  const html = panel.innerHTML;
  const en = bundle('en');
  for (const k of ['delivery_result_red', 'delivery_report_only', 'delivery_red_listed_items', 'delivery_red_all_sends_failed',
    'delivery_group_new', 'delivery_group_backlog', 'delivery_group_accepted']) {
    assert.ok(en[k], 'en lacks ' + k);
    assert.ok(html.includes(run(loadApp({}).sandbox, `escHtml(${JSON.stringify(en[k].split('%')[0])})`)), 'missing ' + k + ' in ' + html);
  }
  assert.ok(html.includes('some_new_code'), 'a code with no label is shown as it is');
  for (const text of ['a1b2', 's9', 'b3', 'awaiting_you', 'waiting-upstream', 'other-hosts', 'ntfy']) {
    assert.ok(html.includes(text), 'missing ' + text);
  }
  assert.ok(!/\bPRD\b/.test(html));
});

test('a green report says green and an empty one with nulls does not throw', async () => {
  const { panel } = await loaded({ 'GET /api/delivery': report({ result: 'green', red: null, items: null, backlog: null, accepted: null, sources: null, channels: null, uncovered: null, by_rule: null, at: '0001-01-01T00:00:00Z' }) });
  assert.ok(panel.innerHTML.includes(bundle('en').delivery_result_green));
  assert.ok(panel.innerHTML.includes(bundle('en').delivery_no_channel));
  assert.ok(panel.innerHTML.includes(bundle('en').delivery_never));
  assert.ok(!panel.innerHTML.includes('0001-01-01'));
});

test('a source that could not be read is named', async () => {
  const { panel } = await loaded({ 'GET /api/delivery': report({ red: ['source_unreadable'], sources: [{ name: 'schedules', ok: false, candidates: 0 }, { name: 'automata', ok: true, candidates: 1 }] }) });
  const line = panel.innerHTML.match(/<div class="delivery-unreadable">.*?<\/div>/);
  assert.ok(line, panel.innerHTML);
  assert.ok(line[0].includes('schedules') && !line[0].includes('automata'));
});

test('everything from the daemon is escaped', async () => {
  const bad = entry({ key: HOSTILE, id: HOSTILE, state: HOSTILE, kind: HOSTILE, reason: HOSTILE, waits_on: HOSTILE, invariant: HOSTILE, seen_at: HOSTILE, seen_by: HOSTILE, review_by: HOSTILE, since: HOSTILE, accepted: { reason: HOSTILE, until: HOSTILE, by: HOSTILE } });
  const { panel } = await loaded({ 'GET /api/delivery': report({ red: [HOSTILE], items: [bad], backlog: [bad], accepted: [bad], channels: [HOSTILE], uncovered: [HOSTILE], heartbeat: HOSTILE, sources: [{ name: HOSTILE, ok: false }] }) });
  assert.ok(!panel.innerHTML.includes('<img src=x'), panel.innerHTML);
});

test('the daemon refusal is shown, and a disabled check is not an empty green list', async () => {
  const { panel } = await loaded({ 'GET /api/delivery': { status: 503, text: 'the delivery check is not available' } });
  assert.ok(panel.innerHTML.includes('the delivery check is not available'));
  assert.ok(!panel.innerHTML.includes(bundle('en').delivery_result_green));
});

test('a long group shows 100 rows and says how many more there are', async () => {
  const many = Array.from({ length: 1000 }, (_, i) => entry({ key: 'schedule:s' + i + ':failed', id: 's' + i, backlog: true }));
  const { panel } = await loaded({ 'GET /api/delivery': report({ backlog: many }) });
  assert.equal((panel.innerHTML.match(/class="delivery-row"/g) || []).length, 102);
  assert.ok(panel.innerHTML.includes('900'));
});

test('check now runs a pass and draws its report', async () => {
  const { sandbox, panel, calls } = await loaded({
    'GET /api/delivery': report(),
    'POST /api/delivery/check': report({ result: 'green', red: [], items: [], pass: 8 }),
  });
  await sandbox.deliveryCheckNow();
  await flushAsync();
  assert.equal(calls[calls.length - 1].key, 'POST /api/delivery/check');
  assert.ok(panel.innerHTML.includes(bundle('en').delivery_result_green));
});

test('acknowledge sends the chosen keys as the web, then reads the list again', async () => {
  const { sandbox, panel, calls } = await loaded({ 'GET /api/delivery': report(), 'POST /api/delivery/ack': { changed: 2 } });
  sandbox.deliveryToggle('automaton:a1b2:blocked', true);
  sandbox.deliveryToggle('bug:b3:parked', true);
  sandbox.deliveryToggle('schedule:s9:failed', true);
  sandbox.deliveryToggle('schedule:s9:failed', false);
  await sandbox.deliveryAction('ack');
  await flushAsync();
  assert.deepEqual(plain(calls.slice(1)), [
    { key: 'POST /api/delivery/ack', body: { keys: ['automaton:a1b2:blocked', 'bug:b3:parked'], actor: 'web' } },
    { key: 'GET /api/delivery', body: null },
  ]);
  assert.ok(panel.innerHTML.includes('2'));
  assert.deepEqual(plain(run(sandbox, 'Array.from(window._delivery.selected)')), []);
});

test('accept needs a reason code and a date; the whole backlog is one selector', async () => {
  const { sandbox, panel, calls } = await loaded({ 'GET /api/delivery': report(), 'POST /api/delivery/accept': { changed: 1 } });
  const en = bundle('en');
  const refused = async (key) => {
    await sandbox.deliveryAction('accept');
    await flushAsync();
    assert.equal(calls.length, 1, 'nothing is sent');
    assert.ok(panel.innerHTML.includes(run(sandbox, `escHtml(${JSON.stringify(en[key])})`)), 'missing ' + key);
  };
  await refused('delivery_need_selection');
  sandbox.deliveryToggle('automaton:a1b2:blocked', true);
  await refused('delivery_need_reason');
  sandbox.deliveryField('reason', 'Has Spaces');
  await refused('delivery_need_reason');
  sandbox.deliveryField('reason', ' waiting-upstream ');
  await refused('delivery_need_date');
  sandbox.deliveryField('until', '2026-12-10');
  assert.ok(sandbox.deliveryPanelHtml().includes('value="2026-12-10"'), 'a redraw keeps what was typed');
  sandbox.deliveryWholeBacklog(true);
  await sandbox.deliveryAction('accept');
  await flushAsync();
  assert.deepEqual(plain(calls[1]), { key: 'POST /api/delivery/accept', body: { backlog: true, reason: 'waiting-upstream', until: '2026-12-10', actor: 'web' } });
});

test('drop asks first and sends only after yes', async () => {
  const { sandbox, panel, calls, confirms } = await loaded({
    'GET /api/delivery': report(),
    'POST /api/delivery/drop': { status: 409, text: 'only failed or backlog entries can be dropped; accept the others with a review date' },
  });
  sandbox.deliveryToggle('automaton:a1b2:blocked', true);
  sandbox.deliveryField('reason', 'not-wanted');
  await sandbox.deliveryAction('drop');
  assert.equal(confirms.length, 1);
  assert.ok(confirms[0].msg.includes('1'));
  assert.equal(calls.length, 1, 'nothing is sent before yes');
  await confirms[0].yes();
  await flushAsync();
  assert.deepEqual(plain(calls[1]), { key: 'POST /api/delivery/drop', body: { keys: ['automaton:a1b2:blocked'], reason: 'not-wanted', actor: 'web' } });
  assert.ok(panel.innerHTML.includes('only failed or backlog entries can be dropped'));
  // A refusal keeps the choice so it can be corrected.
  assert.deepEqual(plain(run(sandbox, 'Array.from(window._delivery.selected)')), ['automaton:a1b2:blocked']);
});

test('items that left the list are no longer selected', async () => {
  const routes = { 'GET /api/delivery': report(), 'POST /api/delivery/check': report({ items: [] }) };
  const { sandbox } = await loaded(routes);
  sandbox.deliveryToggle('automaton:a1b2:blocked', true);
  await sandbox.deliveryCheckNow();
  await flushAsync();
  assert.deepEqual(plain(run(sandbox, 'Array.from(window._delivery.selected)')), []);
});

test('the test message shows what each channel answered', async () => {
  const { sandbox, panel } = await loaded({
    'GET /api/delivery': report(),
    'POST /api/delivery/test-message': { results: [{ channel: 'ntfy', ok: true }, { channel: 'email', ok: false, code: 'timeout' }] },
  });
  await sandbox.deliveryTestMessage();
  await flushAsync();
  const lines = panel.innerHTML.match(/<div class="delivery-send">.*?<\/div>/g);
  assert.equal(lines.length, 2);
  assert.ok(lines[0].includes('ntfy') && lines[0].includes(bundle('en').delivery_send_ok));
  assert.ok(lines[1].includes('email') && lines[1].includes('timeout'));
});

test('a test message with no channel says nothing was sent', async () => {
  const { sandbox, panel } = await loaded({ 'GET /api/delivery': report(), 'POST /api/delivery/test-message': { results: null } });
  await sandbox.deliveryTestMessage();
  await flushAsync();
  assert.ok(panel.innerHTML.includes(bundle('en').delivery_test_none));
});

test('the settings page has the card and the three settings under Automata', () => {
  const { sandbox } = loadApp({});
  const sec = plain(run(sandbox, "GENERAL_CONFIG_FIELDS.find(s => s.id === 'delivery_check')"));
  assert.equal(sec.docs, 'howto/delivery-check.md');
  assert.deepEqual(sec.fields.map(f => [f.key, f.type, f.default]), [
    ['delivery.enabled', 'toggle', true], ['delivery.notify', 'toggle', true], ['delivery.interval_seconds', 'number', undefined]]);
  const interval = sec.fields[2];
  assert.equal(interval.min, 60);
  assert.equal(interval.placeholder, '300');
  assert.match(interval.onchange, /savePromptNumberSetting\('delivery\.interval_seconds', this\.value, 60\)/);
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.match(src, /_automataIds = \[[^\]]*'delivery_check'/);
  assert.match(src, /id="deliveryPanel"/);
  assert.match(src, /loadDeliveryPanel\(\{ fresh: true \}\)/);
});

test('every delivery key the page uses is in all five locales', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const keys = new Set([...src.matchAll(/'((?:settings_)?delivery_[a-z0-9_]+)'/g)].map(m => m[1]));
  keys.delete('delivery_check'); // the settings section id
  keys.delete('delivery_list'); // the card's collapse id
  // Keys built from a red code.
  for (const code of ['listed_items', 'source_unreadable', 'start_mark_missing', 'record_unreadable', 'cannot_record', 'no_channel',
    'all_sends_failed', 'not_told', 'check_stale', 'check_never_ran', 'mass_expiry_held', 'flood_not_recorded']) {
    keys.add('delivery_red_' + code);
  }
  keys.delete('delivery_red_');
  assert.ok(keys.size >= 40, 'found ' + keys.size);
  const en = bundle('en');
  for (const lang of LOCALES) {
    const b = bundle(lang);
    for (const k of keys) {
      assert.ok(b[k], `${lang} lacks ${k}`);
      assert.ok(!/\bPRD\b|\bBL\d|\bF\d{2}\b/.test(b[k]), `${lang} ${k}: ${b[k]}`);
      const holes = (s) => (s.match(/%\d\$s/g) || []).sort().join();
      assert.equal(holes(b[k]), holes(en[k]), `${lang} ${k} placeholders`);
    }
  }
});
