// Pending item card, hold badge and attach control — PWA side.
//
// window.pendingItemsCardHtml(status) draws GET /api/autonomous/pending-items:
// the policy and file registry lines, the last registry read, every item with its
// state and ref, the Automations held on a waiting item with the reason, the add
// form and (only when a file registry is configured) the refresh button. A pending
// item member of an Automation shows its title and a "Waiting for an Automaton"
// badge with the attach control, and the insert dialog can pick a waiting item.
// Covered here with fetch stubbed and the real English bundle.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const HOSTILE = '<img src=x onerror=alert(1)>';
const ENTITY_IMG = '&lt;img src=x onerror=alert(1)&gt;';
const LIST = '/api/autonomous/pending-items';

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
}

// loadApp returns a sandbox with the app loaded, a recording fetch and a toast
// recorder. routes maps "METHOD /path" to {status, body, onHit}; an unknown
// request answers 404. Every request is recorded in __calls with its parsed JSON
// body (undefined when it sent none).
function loadApp(routes, byId) {
  const sandbox = buildSandbox({ byId: byId || {} });
  const calls = [];
  sandbox.fetch = (url, opts) => {
    const method = (opts && opts.method) || 'GET';
    calls.push({ url: String(url), method, body: opts && opts.body ? JSON.parse(opts.body) : undefined });
    const hit = (routes || {})[method + ' ' + url];
    if (!hit) return Promise.resolve({ ok: false, status: 404, statusText: 'Not Found', text: () => Promise.resolve('not found') });
    if (hit.onHit) hit.onHit();
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
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  sandbox.__calls = calls;
  sandbox.__toasts = [];
  sandbox.showToast = (message, type) => { sandbox.__toasts.push({ message, type }); };
  return sandbox;
}

function call(sandbox, expr, value) {
  sandbox._v = value;
  return vm.runInContext(expr, sandbox);
}

function card(status) {
  return call(loadApp({}), 'window.pendingItemsCardHtml(_v)', status);
}

const posts = sb => sb.__calls.filter(c => c.method === 'POST');

function item(over) {
  return {
    id: '11223344', ref: 'T-17', title: 'Write the migration guide', state: 'waiting', source: 'internal',
    automaton_id: '', created_at: '2026-11-01T08:00:00Z', updated_at: '2026-11-01T08:00:00Z',
    automations: [], ...over,
  };
}

function heldEntry(over) {
  return {
    automation_id: 'a1b2c3d4', name: 'Release train', member_id: 'm2', item_id: '11223344',
    title: 'Write the migration guide', ref: 'T-17', since: '2026-11-02T09:30:00Z',
    reason: 'member "Write the migration guide" is a pending item waiting for an Automaton to be attached (item 11223344); attach one to continue',
    ...over,
  };
}

function status(over) {
  return {
    policy: 'hold', auto_resume: true,
    source: { configured: false, path: '', id_pattern: '^\\S+$', id_column: '1', title_column: '2', state_column: '', done_values: ['done'], on_missing_row: 'keep' },
    last_refresh: { configured: false, at: null, rows: 0, added: 0, updated: 0, done: 0, dropped: 0, kept: 0, skipped: 0, error: '' },
    items: [item()], held: [], ...over,
  };
}

function fileStatus(over) {
  return status({
    source: { configured: true, path: 'docs/work-registry.md', id_pattern: '^\\S+$', id_column: '1', title_column: '2', state_column: '', done_values: ['done'], on_missing_row: 'keep' },
    last_refresh: { configured: true, at: '2026-11-03T10:00:00Z', rows: 12, added: 3, updated: 2, done: 1, dropped: 0, kept: 0, skipped: 4, error: '' },
    ...over,
  });
}

// attrOf reads one attribute of the first element whose opening tag matches re.
function attrOf(html, tagRe, attr) {
  const tag = (html.match(tagRe) || [])[0];
  assert.ok(tag, 'no element matches ' + tagRe + ' in ' + html);
  const m = tag.match(new RegExp(attr + '="([^"]*)"'));
  assert.ok(m, attr + ' missing from ' + tag);
  return m[1];
}

// ── the card ─────────────────────────────────────────────────────────────────

test('the card shows the policy and file registry lines and every item with its state and ref', () => {
  const html = card(fileStatus({
    items: [
      item(),
      item({ id: 'aaaa0001', title: 'Hand the cache over', ref: '', state: 'attached', source: 'file', automaton_id: '9f8e7d6c', automations: [{ automation_id: 'a1b2c3d4', name: 'Release train', member_id: 'm3', held: false }] }),
      item({ id: 'aaaa0002', title: 'Retire the old path', ref: 'T-3', state: 'done', source: 'file' }),
    ],
  }));
  assert.ok(html.includes('>Pending items'), 'card title missing');
  assert.ok(html.includes('Pending item policy: hold (the Automation waits at the pending item until an Automaton is attached)'), 'policy line missing: ' + html);
  assert.ok(html.includes('Auto-resume when an Automaton is attached: on'), 'auto-resume line missing');
  assert.ok(html.includes('File registry: docs/work-registry.md'), 'registry line missing');
  assert.ok(html.includes('Last read of the file registry: '), 'last refresh line missing');
  assert.ok(html.includes('Rows: 12, added 3, updated 2, done 1, dropped 0, skipped 4.'), 'refresh counts missing: ' + html);
  assert.ok(html.includes('Pending items (3)'), 'item count heading missing');
  assert.ok(html.includes('Write the migration guide') && html.includes('Hand the cache over') && html.includes('Retire the old path'), 'titles missing');
  assert.ok(html.includes('pending-item-state-waiting') && html.includes('pending-item-state-attached') && html.includes('pending-item-state-done'), 'state chips missing');
  assert.ok(html.includes('Ref T-17') && html.includes('Ref T-3'), 'refs missing');
  assert.ok(!html.includes('Ref </') && (html.match(/class="pending-item-ref"/g) || []).length === 2, 'no ref chip for an item without a ref');
  assert.ok(html.includes('Automaton 9f8e7d6c is attached'), 'attached automaton missing');
  assert.ok(html.includes('In Automation: Release train'), 'automation membership missing');
  assert.ok(html.includes('>file registry<') && html.includes('>added here<'), 'source chips missing');
  assert.ok(!/\bPRDs?\b/.test(html), 'card must say Automaton, never PRD');
});

test('only a waiting item has an attach control, and the card is read with the policy skip wording', () => {
  const html = card(status({ policy: 'skip', auto_resume: false, items: [item(), item({ id: 'aaaa0001', state: 'attached', automaton_id: 'x' }), item({ id: 'aaaa0002', state: 'done' })] }));
  assert.equal((html.match(/class="btn-secondary pending-item-attach-btn"/g) || []).length, 1, 'one attach button, on the waiting item');
  assert.ok(html.includes('data-item-id="11223344" data-input="pendingItemAttachInput-item-11223344"'), 'button names its item and field: ' + html);
  assert.ok(html.includes('Attach Automaton'), 'attach label missing');
  assert.ok(html.includes('Pending item policy: skip (the Automation moves past the pending item)'), 'skip wording missing');
  assert.ok(html.includes('Auto-resume when an Automaton is attached: off'), 'auto-resume off missing');
  const unknown = card(status({ policy: 'future_mode' }));
  assert.ok(unknown.includes('Pending item policy: future_mode'), 'a policy this version does not know shows as it came');
});

test('held Automations list the item, the since time, the reason and an attach control', () => {
  const html = card(status({ held: [heldEntry()] }));
  assert.ok(html.includes('Automations held on a pending item (1)'), 'held heading missing');
  assert.ok(html.includes('Automation Release train is waiting for an Automaton on Write the migration guide'), 'held row missing: ' + html);
  assert.ok(html.includes('Waiting since '), 'since missing');
  assert.ok(html.includes('Reason: member &quot;Write the migration guide&quot; is a pending item waiting for an Automaton'), 'reason missing');
  assert.ok(html.includes('class="pending-item-held-row"') && html.includes('data-member-id="m2"'), 'held row data missing');
  assert.ok(html.includes('data-input="pendingItemAttachInput-held-m2-11223344"'), 'held attach control missing: ' + html);
  assert.ok(html.includes('data-input="pendingItemAttachInput-item-11223344"'), 'the item row keeps its own control');
  const none = card(status({ held: [] }));
  assert.ok(!none.includes('pending-item-held-row') && !none.includes('held on a pending item'), 'no held section when nothing is held');
});

test('an empty or malformed registry shows the empty sentence, the add form and no rows', () => {
  for (const body of [status({ items: [] }), status({ items: null }), {}, null, undefined, 'nope', [], { items: 'x', held: 'y' }]) {
    const html = card(body);
    assert.ok(html.includes('>Pending items'), 'title missing for ' + JSON.stringify(body));
    assert.ok(html.includes('No pending items yet.'), 'empty sentence missing for ' + JSON.stringify(body));
    assert.ok(html.includes('Pending items (0)'), 'zero count missing');
    assert.ok(!html.includes('class="pending-item-row"') && !html.includes('pending-item-held-row'), 'no rows');
    assert.ok(html.includes('id="pendingItemAddTitle"'), 'the add form stays when empty');
    assert.ok(html.includes('No file registry is configured.'), 'no-registry line missing');
  }
});

test('the refresh button shows only when a file registry is configured', () => {
  const without = card(status());
  assert.ok(!without.includes('pending-item-refresh-btn'), 'no refresh button without a file registry: ' + without);
  assert.ok(without.includes('No file registry is configured.'));
  const withReg = card(fileStatus());
  assert.ok(withReg.includes('pending-item-refresh-btn'), 'refresh button missing with a file registry');
  assert.ok(withReg.includes('Read the file registry now'), 'refresh label missing');
  const never = card(fileStatus({ last_refresh: { configured: true, at: null, rows: 0, added: 0, updated: 0, done: 0, dropped: 0, kept: 0, skipped: 0, error: '' } }));
  assert.ok(never.includes('The file registry has not been read yet.'), 'never-read line missing');
  const failed = card(fileStatus({ last_refresh: { configured: true, at: '2026-11-03T10:00:00Z', rows: 0, added: 0, updated: 0, done: 0, dropped: 0, kept: 0, skipped: 0, error: 'open docs/work-registry.md: no such file' } }));
  assert.ok(failed.includes('The last read of the file registry failed: open docs/work-registry.md: no such file'), 'refresh error missing');
});

test('the card title carries the help link and follows the inline docs link setting', () => {
  const html = card(status());
  assert.ok(html.includes('href="/diagrams.html#docs/datawatch-definitions.md#pending-items"'), 'help link missing: ' + html);
  assert.ok(html.includes('About pending items'), 'help link text missing');
  const sb = loadApp({});
  sb.localStorage.getItem = k => (k === 'cs_show_docs_links' ? '0' : null);
  sb._v = status();
  const hidden = vm.runInContext('window.pendingItemsCardHtml(_v)', sb);
  assert.ok(!hidden.includes('pending-item-help'), 'the link hides when the setting is off');
});

test('hostile titles, refs, names, reasons and ids are escaped and ids never reach script text', () => {
  const html = card(fileStatus({
    policy: HOSTILE,
    source: { configured: true, path: HOSTILE, id_pattern: '', id_column: '1', title_column: '2', state_column: '', done_values: [], on_missing_row: 'keep' },
    last_refresh: { configured: true, at: '2026-11-03T10:00:00Z', rows: 1, added: 0, updated: 0, done: 0, dropped: 0, kept: 0, skipped: 0, error: HOSTILE },
    items: [item({ id: '"><script>x</script>', title: HOSTILE, ref: HOSTILE, source: HOSTILE, state: HOSTILE, automaton_id: HOSTILE, automations: [{ automation_id: HOSTILE, name: HOSTILE, member_id: 'm', held: false }] })],
    held: [heldEntry({ name: HOSTILE, title: HOSTILE, reason: HOSTILE, item_id: '"><script>x</script>', member_id: '"><b>m</b>', automation_id: HOSTILE })],
  }));
  assert.ok(html.includes(ENTITY_IMG), 'hostile text should survive as an escaped entity');
  assert.ok(!html.includes('<img'), 'live tag must not survive: ' + html);
  assert.ok(!html.includes('<script'), 'live script must not survive: ' + html);
  assert.ok(!html.includes('<b>m</b>'), 'live tag in an id must not survive');
  assert.ok(!/onclick="[^"]*(script|<b>)/.test(html), 'ids must not reach script text');
});

test('a malformed item or held entry is skipped, not drawn', () => {
  const html = card(status({ items: [null, 'x', 7, [], item()], held: [null, 'y'] }));
  assert.equal((html.match(/class="pending-item-row"/g) || []).length, 1, 'only the real item is drawn');
  assert.ok(!html.includes('pending-item-held-row'), 'no held row for junk');
});

// ── the loader and the overview slot ─────────────────────────────────────────

test('the loader reads the route and draws the card into the Overview slot', async () => {
  const slot = makeStubElement();
  const sb = loadApp({ ['GET ' + LIST]: { body: status({ held: [heldEntry()] }) } }, { automationPendingItemsCard: slot });
  const ctl = vm.runInContext('_automationPendingItemsLoader()', sb);
  ctl.load();
  await flushAsync(30);
  try {
    assert.ok(sb.__calls.some(c => c.url === LIST && c.method === 'GET'), 'loader must read GET ' + LIST);
    assert.ok(slot.innerHTML.includes('Write the migration guide'), 'item missing from the slot: ' + slot.innerHTML);
    assert.ok(slot.innerHTML.includes('Automations held on a pending item (1)'), 'held list missing from the slot');
    const data = sb.window._pendingItemsData;
    assert.ok(data && data.items['11223344'] && data.held.m2, 'the lookups the strip reads are kept');
  } finally {
    ctl.stop();
  }
});

test('the Overview sub-tab carries the slot and the start and stop lists include the loader', () => {
  const sb = loadApp({});
  const body = vm.runInContext("_AUTOMATION_SUBTABS.find(s => s.id === 'overview').bodyHTML()", sb);
  assert.ok(body.includes('id="automationPendingItemsCard"'), 'slot missing from the Overview body');
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.ok(src.includes("url: '/api/autonomous/pending-items'"), 'loader must read the route');
  assert.ok(/listLoaderAll\(\[_automationPendingItemsLoader\(\), /.test(src), 'the start list must include the loader');
  assert.ok(/_automationStopLoaders\(\['automation_pending_items', [^\]]*\]\); \}/.test(src), 'the Overview stop list must include the loader');
  assert.ok(/const all = \['automation', 'automation_pending_items', [^\]]*\];/.test(src), 'the stop-all list must include the loader');
});

test('a repaint keeps what the operator typed in the add form', () => {
  const sb = loadApp({});
  const typed = { id: 'pendingItemAddTitle', value: 'half written', classList: { contains: c => c === 'pending-item-field' } };
  const fresh = { id: 'pendingItemAddTitle', value: '', focus() { this.focused = true; } };
  sb.document.activeElement = typed;
  sb._el = { querySelectorAll: () => [typed] };
  const keep = vm.runInContext('_pendingItemFieldKeep(_el)', sb);
  assert.deepEqual(JSON.parse(JSON.stringify(keep)), { values: { pendingItemAddTitle: 'half written' }, focus: 'pendingItemAddTitle' });
  sb.document.getElementById = id => (id === 'pendingItemAddTitle' ? fresh : null);
  sb._keep = keep;
  vm.runInContext('_pendingItemFieldRestore(null, _keep)', sb);
  assert.equal(fresh.value, 'half written');
  assert.equal(fresh.focused, true);
});

// ── the attach control ───────────────────────────────────────────────────────

// attachButton builds the click target from the card's own markup, so the
// handler is checked against what the card draws.
function attachButton(html, itemId) {
  const tag = (html.match(new RegExp('<button[^>]*pending-item-attach-btn[^>]*data-item-id="' + itemId + '"[^>]*>')) || [])[0];
  assert.ok(tag, 'no attach button for ' + itemId + ' in ' + html);
  const attrs = {};
  tag.replace(/([a-z-]+)="([^"]*)"/g, (_, k, v) => { attrs[k] = v; return ''; });
  return { getAttribute: k => (k in attrs ? attrs[k] : null), attrs };
}

test('clicking attach sends the POST body {automaton_id} to the item route and reloads', async () => {
  const html = card(status());
  const btn = attachButton(html, '11223344');
  const input = { value: '  9f8e7d6c  ' };
  let listBody = status();
  const slot = makeStubElement();
  const sb = loadApp({
    ['POST ' + LIST + '/11223344/attach']: {
      body: { converted: [], resumed: [] },
      onHit: () => { listBody = status({ items: [item({ state: 'attached', automaton_id: '9f8e7d6c' })] }); },
    },
  }, { [btn.attrs['data-input']]: input, automationPendingItemsCard: slot });
  // The list route answers with whatever the registry holds right now.
  sb.fetch = (orig => (url, opts) => ((opts && opts.method) === 'POST' || url !== LIST ? orig(url, opts) : (sb.__calls.push({ url, method: 'GET', body: undefined }), Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(listBody), text: () => Promise.resolve('') }))))(sb.fetch);
  const ctl = vm.runInContext('_automationPendingItemsLoader()', sb);
  try {
    const res = await sb.window.pendingItemAttach(btn);
    await flushAsync(30);
    const sent = posts(sb);
    assert.equal(sent.length, 1, 'one POST');
    assert.equal(sent[0].url, LIST + '/11223344/attach');
    assert.deepEqual(sent[0].body, { automaton_id: '9f8e7d6c' }, 'body is exactly {automaton_id}, trimmed');
    assert.ok(res && Array.isArray(res.converted), 'the reply is returned');
    const postAt = sb.__calls.findIndex(c => c.method === 'POST');
    assert.ok(sb.__calls.slice(postAt + 1).some(c => c.method === 'GET' && c.url === LIST), 'the card reloads after the POST: ' + JSON.stringify(sb.__calls));
    assert.ok(slot.innerHTML.includes('Automaton 9f8e7d6c is attached'), 'the reloaded card shows the attach: ' + slot.innerHTML);
    assert.ok(sb.__toasts.some(x => x.message === 'Automaton 9f8e7d6c attached' && x.type !== 'error'), 'success toast: ' + JSON.stringify(sb.__toasts));
  } finally {
    ctl.stop();
  }
});

test('attach with an empty field sends nothing and says so', async () => {
  const btn = attachButton(card(status()), '11223344');
  const sb = loadApp({}, { [btn.attrs['data-input']]: { value: '   ' } });
  const res = await sb.window.pendingItemAttach(btn);
  assert.equal(res, null);
  assert.equal(posts(sb).length, 0, 'nothing is sent');
  assert.deepEqual(sb.__toasts.map(x => [x.message, x.type]), [['Enter the id of the Automaton to attach.', 'error']]);
  // A control with no item id does nothing at all.
  const none = await sb.window.pendingItemAttach({ getAttribute: () => '' });
  assert.equal(none, null);
  assert.equal(sb.__toasts.length, 1);
});

test('a refused attach shows the server text and does not reload', async () => {
  const btn = attachButton(card(status()), '11223344');
  const sb = loadApp({ ['POST ' + LIST + '/11223344/attach']: { status: 409, body: '409: Automaton 9f8e7d6c already belongs to another item' } },
    { [btn.attrs['data-input']]: { value: '9f8e7d6c' } });
  const res = await sb.window.pendingItemAttach(btn);
  assert.equal(res, null);
  assert.equal(sb.__toasts.length, 1);
  assert.equal(sb.__toasts[0].type, 'error');
  assert.ok(sb.__toasts[0].message.startsWith('Could not attach the Automaton: 409:'), sb.__toasts[0].message);
  assert.ok(!sb.__calls.some(c => c.method === 'GET'), 'no reload after a refusal');
});

// ── the add form and refresh ─────────────────────────────────────────────────

test('the add form has a title field, an optional ref field and an Add item button', () => {
  const html = card(status());
  assert.ok(html.includes('Add a pending item'), 'form heading missing');
  assert.ok(html.includes('id="pendingItemAddTitle"') && html.includes('placeholder="Title"') && /id="pendingItemAddTitle"[^>]*required/.test(html), 'title field missing');
  assert.ok(html.includes('id="pendingItemAddRef"') && html.includes('placeholder="Ref (optional)"') && !/id="pendingItemAddRef"[^>]*required/.test(html), 'ref field missing or required');
  assert.ok(html.includes('>Add item<'), 'button label missing');
  assert.ok(/<form class="pending-item-add" onsubmit="event\.preventDefault\(\); window\.pendingItemAdd\(\)"/.test(html), 'form must call the handler');
});

test('adding sends {title, ref}, clears the fields and reloads', async () => {
  const title = { value: '  Write the docs  ' };
  const ref = { value: ' T-9 ' };
  const sb = loadApp({ ['POST ' + LIST]: { status: 201, body: item({ id: 'bbbb0001', title: 'Write the docs', ref: 'T-9' }) } },
    { pendingItemAddTitle: title, pendingItemAddRef: ref });
  const res = await sb.window.pendingItemAdd();
  assert.deepEqual(posts(sb).map(c => [c.url, c.body]), [[LIST, { title: 'Write the docs', ref: 'T-9' }]]);
  assert.equal(res.id, 'bbbb0001');
  assert.equal(title.value, '');
  assert.equal(ref.value, '');
  assert.ok(sb.__toasts.some(x => x.message === 'Pending item added: Write the docs'), JSON.stringify(sb.__toasts));
});

test('adding without a ref sends no ref, and an empty title sends nothing', async () => {
  const sb = loadApp({ ['POST ' + LIST]: { status: 201, body: item() } }, { pendingItemAddTitle: { value: 'Only a title' }, pendingItemAddRef: { value: '' } });
  await sb.window.pendingItemAdd();
  assert.deepEqual(posts(sb)[0].body, { title: 'Only a title' });
  const empty = loadApp({}, { pendingItemAddTitle: { value: '  ' }, pendingItemAddRef: { value: 'T-1' } });
  assert.equal(await empty.window.pendingItemAdd(), null);
  assert.equal(posts(empty).length, 0);
  assert.deepEqual(empty.__toasts.map(x => [x.message, x.type]), [['Enter a title for the pending item.', 'error']]);
});

test('a refused add shows the server text and keeps the fields', async () => {
  const title = { value: 'Dup' };
  const sb = loadApp({ ['POST ' + LIST]: { status: 400, body: '400: a pending item needs a title' } }, { pendingItemAddTitle: title, pendingItemAddRef: { value: '' } });
  assert.equal(await sb.window.pendingItemAdd(), null);
  assert.equal(title.value, 'Dup', 'a failed add keeps what was typed');
  assert.ok(sb.__toasts[0].message.startsWith('Could not add the pending item: 400:'), sb.__toasts[0].message);
});

test('refresh posts to the refresh route and reports what the read changed', async () => {
  const sb = loadApp({ ['POST ' + LIST + '/refresh']: { body: { configured: true, added: 2, updated: 1, error: '' } } });
  await sb.window.pendingItemRefresh();
  const sent = posts(sb);
  assert.deepEqual(sent.map(c => [c.url, c.body]), [[LIST + '/refresh', undefined]]);
  assert.equal(sb.__toasts[0].message, 'File registry read: 2 added, 1 updated');
  const bad = loadApp({ ['POST ' + LIST + '/refresh']: { body: { configured: true, error: 'open x: no such file' } } });
  await bad.window.pendingItemRefresh();
  assert.deepEqual([bad.__toasts[0].message, bad.__toasts[0].type], ['Could not read the file registry: open x: no such file', 'error']);
});

// ── the hold badge on a strip member and in the member detail ────────────────

function pendingAutomation(over) {
  return {
    id: 'a1b2c3d4', name: 'Release train', status: 'blocked', version: 3,
    entry_member_id: 'm1', active_cursors: ['m1'],
    members: [
      { id: 'm1', kind: 'pending_item', pending_item_id: '11223344', status: 'running', on_complete: 'm2', added_by: 'operator' },
      { id: 'm2', prd_id: 'p2aaaaaaaaaa', status: 'pending', added_by: 'operator' },
    ],
    ...over,
  };
}

function strip(a, ctx) {
  const sb = loadApp({});
  sb._a = a;
  sb._c = ctx;
  return vm.runInContext('buildAutomationStripHTML(_a, _c)', sb);
}

const CTX = {
  prdTitles: { p2aaaaaaaaaa: 'Deploy' },
  pendingItems: { 11223344: item() },
  pendingHeld: { m1: heldEntry() },
};

function cardOf(html, memberId) {
  const start = html.indexOf(`data-member-id="${memberId}"`);
  assert.ok(start >= 0, 'card ' + memberId + ' is drawn');
  const next = html.indexOf('<div class="automation-member ', start + 1);
  return html.slice(start, next < 0 ? html.length : next);
}

test('a pending item member shows its title, the Waiting for an Automaton badge and the attach control', () => {
  const html = strip(pendingAutomation(), CTX);
  const m1 = cardOf(html, 'm1');
  assert.ok(m1.includes('data-kind="pending_item"'), 'kind attribute missing');
  assert.ok(m1.includes('<span class="automation-member-title">Write the migration guide</span>'), 'title missing: ' + m1);
  assert.ok(m1.includes('class="pending-item-badge"') && m1.includes('Waiting for an Automaton'), 'hold badge missing: ' + m1);
  assert.ok(m1.includes('data-held="true"'), 'badge should say the member is held');
  assert.ok(m1.includes('title="member &quot;Write the migration guide&quot; is a pending item waiting'), 'the hold reason shows as the badge tip');
  assert.ok(m1.includes('data-input="pendingItemAttachInput-member-m1-11223344"') && m1.includes('Attach Automaton'), 'attach control missing: ' + m1);
  assert.ok(m1.includes('<span class="automation-chip">11223344</span>'), 'the chip carries the item id');
  const m2 = cardOf(html, 'm2');
  assert.ok(!m2.includes('pending-item-badge') && !m2.includes('Waiting for an Automaton'), 'an Automaton member has no hold badge');
});

test('a pending item member that is not held still shows the badge, with the generic tip', () => {
  const html = strip(pendingAutomation({ status: 'active' }), { ...CTX, pendingHeld: {} });
  const m1 = cardOf(html, 'm1');
  assert.ok(m1.includes('data-held="false"') && m1.includes('Waiting for an Automaton'), m1);
  assert.ok(m1.includes('The Automation waits here until an Automaton is attached.'), 'generic tip missing');
});

test('an item the page has not loaded yet shows a fallback title and still offers the control', () => {
  const html = strip(pendingAutomation(), { prdTitles: {} });
  const m1 = cardOf(html, 'm1');
  assert.ok(m1.includes('Pending item 11223344'), 'fallback title missing: ' + m1);
  assert.ok(m1.includes('Waiting for an Automaton') && m1.includes('pending-item-attach-btn'));
});

test('a finished, done or attached pending item member shows no badge and no control', () => {
  const finished = strip(pendingAutomation({ status: 'active', members: [{ id: 'm1', kind: 'pending_item', pending_item_id: '11223344', status: 'skipped' }] }), CTX);
  assert.ok(!finished.includes('pending-item-badge') && !finished.includes('pending-item-attach'), 'a skipped member has neither');
  for (const state of ['done', 'attached']) {
    const html = strip(pendingAutomation(), { ...CTX, pendingItems: { 11223344: item({ state }) } });
    assert.ok(!html.includes('pending-item-badge') && !html.includes('pending-item-attach'), state + ' item has neither');
  }
});

test('hostile item titles and ids in a strip member are escaped', () => {
  const a = pendingAutomation({ members: [{ id: 'm1', kind: 'pending_item', pending_item_id: '"><script>x</script>', status: 'running' }] });
  const html = strip(a, { pendingItems: { '"><script>x</script>': item({ title: HOSTILE }) }, pendingHeld: { m1: heldEntry({ reason: HOSTILE }) } });
  assert.ok(html.includes(ENTITY_IMG), 'hostile text escaped');
  assert.ok(!html.includes('<img') && !html.includes('<script'), 'no live markup: ' + html);
  assert.ok(!/onclick="[^"]*script/.test(html), 'ids must not reach script text');
});

test('an inherited name such as constructor is not read as a pending item', () => {
  const a = pendingAutomation({ members: [{ id: 'm1', kind: 'pending_item', pending_item_id: 'constructor', status: 'running' }] });
  const html = strip(a, { pendingItems: {}, pendingHeld: {} });
  assert.ok(html.includes('Pending item constructor'), 'falls back to the id: ' + html);
});

test('the member detail shows the item, the badge and the attach control for a pending item member', () => {
  const sb = loadApp({});
  sb._m = pendingAutomation().members[0];
  sb._c = { pendingItems: CTX.pendingItems, pendingHeld: CTX.pendingHeld };
  const html = vm.runInContext('buildAutomationMemberDrawerHTML(_m, _c)', sb);
  assert.ok(html.includes('<dd>Pending item</dd>'), 'kind label missing: ' + html);
  assert.ok(html.includes('<dt>Pending item title</dt><dd>Write the migration guide</dd>'), 'item title missing');
  assert.ok(html.includes('<dt>Pending item ref</dt><dd>T-17</dd>'), 'item ref missing');
  assert.ok(html.includes('<dt>Pending item state</dt><dd>waiting</dd>'), 'item state missing');
  assert.ok(html.includes('Waiting for an Automaton') && html.includes('pendingItemAttachInput-member-m1-11223344'), 'badge and control missing');
  // A member that came from a pending item keeps the item rows but has no control.
  sb._m = { id: 'm5', prd_id: 'p5aaaaaaaaaa', pending_item_id: '11223344', status: 'pending' };
  const converted = vm.runInContext('buildAutomationMemberDrawerHTML(_m, _c)', sb);
  assert.ok(converted.includes('Write the migration guide'), 'provenance shows the item');
  assert.ok(!converted.includes('pending-item-attach'), 'no attach control once converted');
});

test('the detail page loads the registry and draws the member title and badge', async () => {
  const a = pendingAutomation();
  const view = makeStubElement();
  const sb = loadApp({
    ['GET /api/automations/' + a.id]: { body: a },
    'GET /api/autonomous/prds': { body: { prds: [{ id: 'p2aaaaaaaaaa', title: 'Deploy', status: 'approved' }] } },
    ['GET ' + LIST]: { body: status({ held: [heldEntry({ member_id: 'm1' })] }) },
  }, { view, automationControls: makeStubElement(), automationStrip: makeStubElement() });
  sb.openAutomationDetail(a.id);
  await flushAsync();
  assert.ok(sb.__calls.some(c => c.url === LIST), 'the page reads the registry');
  assert.ok(view.innerHTML.includes('<span class="automation-member-title">Write the migration guide</span>'), 'item title in the strip: ' + view.innerHTML);
  assert.ok(view.innerHTML.includes('Waiting for an Automaton'), 'hold badge in the strip');
  assert.ok(view.innerHTML.includes('data-held="true"'), 'the badge knows the member is held');
});

test('a failed registry read leaves the strip drawn with the fallback title', async () => {
  const a = pendingAutomation();
  const view = makeStubElement();
  const sb = loadApp({
    ['GET /api/automations/' + a.id]: { body: a },
    'GET /api/autonomous/prds': { body: { prds: [] } },
  }, { view, automationControls: makeStubElement(), automationStrip: makeStubElement() });
  sb.openAutomationDetail(a.id);
  await flushAsync();
  assert.ok(view.innerHTML.includes('Pending item 11223344'), 'fallback title: ' + view.innerHTML);
  assert.ok(view.innerHTML.includes('Waiting for an Automaton'));
});

// ── the insert dialog ────────────────────────────────────────────────────────

function chain() {
  return {
    id: 'a1b2c3d4', name: 'Release train', status: 'active', version: 7, entry_member_id: 'm1', active_cursors: ['m1'],
    members: [
      { id: 'm1', prd_id: 'm1aaaaaaaaaa', status: 'running', on_complete: 'm2', added_by: 'operator' },
      { id: 'm2', prd_id: 'm2aaaaaaaaaa', status: 'pending', added_by: 'operator' },
    ],
  };
}

async function openInsert(items) {
  const a = chain();
  const ids = {
    view: makeStubElement(), automationControls: makeStubElement(), automationStrip: makeStubElement(),
    genericModalConfirmBtn: makeStubElement(), automationInsertSearch: { value: '' }, automationInsertPicker: makeStubElement(),
    automationInsertUrgent: { checked: false }, automationInsertReason: { value: '' }, automationInsertError: makeStubElement(),
  };
  const modals = [];
  const sb = loadApp({
    ['GET /api/automations/' + a.id]: { body: a },
    'GET /api/autonomous/prds': { body: { prds: [{ id: 'plainaaaaaaaa', title: 'Plain work', status: 'approved' }] } },
    'GET /api/automations': { body: [a] },
    ['GET ' + LIST]: { body: status({ items }) },
    ['POST /api/automations/' + a.id + '/insert_member']: { body: { ...a, version: 8 } },
  }, ids);
  sb.document.createElement = () => { const el = makeStubElement(); el.remove = () => {}; modals.push(el); return el; };
  sb.openAutomationDetail(a.id);
  await flushAsync();
  await sb.openAutomationInsertDialog('m2', 'after');
  const confirm = async () => { ids.genericModalConfirmBtn.onclick(); await flushAsync(); };
  return { sb, ids, confirm, html: () => modals.filter(m => m.innerHTML).pop().innerHTML };
}

const WAITING = [
  item({ id: 'aaaa0001', title: 'Free item', ref: 'T-1' }),
  item({ id: 'aaaa0002', title: 'Taken item', ref: '', automations: [{ automation_id: 'zz990011', name: 'Other train', member_id: 'o1', held: false }] }),
  item({ id: 'aaaa0003', title: 'Finished item', state: 'done' }),
  item({ id: 'aaaa0004', title: 'Attached item', state: 'attached', automaton_id: 'x' }),
];

test('the insert dialog lists the waiting items and marks one that belongs to another Automation', async () => {
  const env = await openInsert(WAITING);
  const html = env.html();
  assert.ok(html.includes('Or insert a pending item (work with no Automaton yet)'), 'picker heading missing: ' + html);
  assert.ok(html.includes('data-pending-item-id="aaaa0001"') && html.includes('Free item'), 'waiting item missing');
  assert.ok(html.includes('data-pending-item-id="aaaa0002" data-unavailable="true"') && html.includes('already in Automation Other train'), 'unavailable reason missing');
  assert.ok(!html.includes('Finished item') && !html.includes('Attached item'), 'only waiting items are offered');
  assert.ok(html.includes('value="item:aaaa0001"'), 'item radios share the Automaton group');
});

test('the insert dialog says so when no pending item is waiting', async () => {
  const env = await openInsert([WAITING[2]]);
  assert.ok(env.html().includes('No waiting pending items.'));
});

test('inserting a picked pending item sends member.pending_item_id', async () => {
  const env = await openInsert(WAITING);
  env.sb.automationPickInsertItem('aaaa0001');
  env.ids.automationInsertReason.value = 'Docs come first';
  await env.confirm();
  const sent = posts(env.sb);
  assert.equal(sent.length, 1);
  assert.equal(sent[0].url, '/api/automations/a1b2c3d4/insert_member');
  assert.deepEqual(sent[0].body.member, { pending_item_id: 'aaaa0001' }, 'no prd_id alongside a pending item');
  assert.equal(sent[0].body.after, 'm2');
  assert.equal(sent[0].body.reason, 'Docs come first');
});

test('choosing an Automaton drops a chosen item and the other way round; an unavailable item is ignored', async () => {
  const env = await openInsert(WAITING);
  env.sb.automationPickInsertItem('aaaa0002');
  env.ids.automationInsertReason.value = 'Nothing picked';
  await env.confirm();
  assert.equal(posts(env.sb).length, 0, 'an unavailable item is not picked');
  assert.ok(env.ids.automationInsertError.innerHTML.includes('Choose an Automaton to insert'), 'nothing picked: ' + env.ids.automationInsertError.innerHTML);
  env.sb.automationPickInsertItem('aaaa0001');
  env.sb.automationPickInsertAutomaton('plainaaaaaaaa');
  await env.confirm();
  assert.deepEqual(posts(env.sb)[0].body.member, { prd_id: 'plainaaaaaaaa' }, 'the Automaton replaced the item');
  const again = await openInsert(WAITING);
  again.sb.automationPickInsertAutomaton('plainaaaaaaaa');
  again.sb.automationPickInsertItem('aaaa0001');
  again.ids.automationInsertReason.value = 'Item wins';
  await again.confirm();
  assert.deepEqual(posts(again.sb)[0].body.member, { pending_item_id: 'aaaa0001' }, 'the item replaced the Automaton');
});

// ── locales ──────────────────────────────────────────────────────────────────

test('every locale has every pending item key the app names', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const keys = new Set([...src.matchAll(/'(pending_item_[a-z_]+)'/g)].map(m => m[1]));
  assert.ok(keys.size >= 15, 'the app should name at least 15 pending item keys, found ' + keys.size);
  for (const lang of LANGS) {
    const b = bundle(lang);
    const missing = [...keys].filter(k => typeof b[k] !== 'string' || b[k].trim() === '');
    assert.deepEqual(missing, [], lang + ' is missing keys');
    const ours = Object.keys(b).filter(k => k.startsWith('pending_item_'));
    assert.ok(ours.length >= 15, lang + ' has too few pending_item_ keys: ' + ours.length);
  }
});

test('the four non-English bundles hold the English text for the new keys and no value says PRD', () => {
  const en = bundle('en');
  const keys = Object.keys(en).filter(k => k.startsWith('pending_item_'));
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of keys) {
      if (lang !== 'en') assert.equal(b[k], en[k], lang + ' ' + k + ' should be the English text until translations arrive');
      assert.ok(!/\bPRDs?\b/.test(b[k]), lang + ' ' + k + ' says PRD');
      assert.ok(!/\b(?:BL|GH#?)\d{2,}\b/.test(b[k]), lang + ' ' + k + ' carries a tracker id');
    }
  }
});

test('the bundles keep the same key set', () => {
  const base = Object.keys(bundle('en')).sort();
  for (const lang of LANGS.slice(1)) {
    assert.deepEqual(Object.keys(bundle(lang)).sort(), base, lang + ' differs from en');
  }
});
