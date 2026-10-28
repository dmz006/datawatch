// Session tags in the PWA: own versus inherited chips on the session card and tree
// nodes, a session with neither, escaping, the detail-view editor and the request
// body it sends, the picker that lists every tag but never sends an inherited ID,
// the new-session form sending `tags`, the batch Tag action, the repaint on a
// tag_update frame, and locale parity for the new strings.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const vm = require('vm');
const { buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const WEB = __dirname;
const LOCALES = ['de', 'en', 'es', 'fr', 'ja'];

const TAGS = [
  { id: 'own1', name: 'Work', color: '#3366ff' },
  { id: 'own2', name: 'Mobile' },
  { id: 'inh1', name: 'Release' },
  { id: 'xss1', name: '<img src=x onerror=alert(1)>' },
];

function loadApp(opts) {
  const o = opts || {};
  const sandbox = buildSandbox(o);
  const timers = { queue: [], seq: 0 };
  sandbox.setTimeout = (fn, ms) => { const id = ++timers.seq; timers.queue.push({ id, fn, ms }); return id; };
  sandbox.clearTimeout = id => {
    const k = timers.queue.findIndex(x => x.id === id);
    if (k >= 0) timers.queue.splice(k, 1);
  };
  vm.createContext(sandbox);
  loadScript(path.join(WEB, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  vm.runInContext(`_tagsState.list = ${JSON.stringify(TAGS)}; _tagsState.byId = _tagsById(_tagsState.list); _tagsState.loaded = true;`, sandbox);
  timers.queue.length = 0;
  sandbox.__timers = timers;
  return sandbox;
}

function run(sb, code) { return vm.runInContext(code, sb); }

function sess(extra) {
  return Object.assign({ full_id: 'host-aaaa', id: 'aaaa', state: 'running', task: 'a task', updated_at: new Date().toISOString() }, extra || {});
}

// A picker element built from the HTML the app drew: its checkboxes are read back
// from the markup, so the checked state under test is the one the app rendered.
function pickerFrom(html, pickerId) {
  const start = html.indexOf(`id="${pickerId}"`);
  assert.ok(start >= 0, `picker ${pickerId} was drawn`);
  const boxes = [];
  const re = /<input type="checkbox" class="tag-picker-cb" value="([^"]*)" (checked)?/g;
  let m;
  const slice = html.slice(start);
  while ((m = re.exec(slice))) boxes.push({ value: m[1], checked: !!m[2] });
  return { querySelectorAll: () => boxes };
}

// Capture the modal showModal is asked to draw.
function captureModal(sb) {
  const seen = { calls: [] };
  sb.showModal = spec => { seen.calls.push(spec); };
  return seen;
}

function captureApi(sb, reply) {
  const calls = [];
  sb.apiFetch = (url, init) => {
    calls.push({ url, init, body: init && init.body ? JSON.parse(init.body) : undefined });
    return Promise.resolve(typeof reply === 'function' ? reply(url, init) : reply);
  };
  return calls;
}

// ── Chips ────────────────────────────────────────────────────────────────────

test('own tags render as normal chips and inherited tags as marked chips', () => {
  const sb = loadApp();
  const html = sb._sessionTagChipsHTML(sess({ tags: ['own1', 'own2'], inherited_tags: ['inh1'] }));
  assert.match(html, /data-tag-id="own1"/);
  assert.match(html, /data-tag-id="own2"/);
  const own = html.match(/<span class="tag-chip" data-tag-id="own1"/);
  assert.ok(own, 'an own tag is a plain tag-chip');
  assert.doesNotMatch(html, /class="tag-chip" data-tag-id="inh1"/, 'the inherited tag is not a plain chip');
  assert.match(html, /class="tag-chip tag-chip-inherited" data-tag-id="inh1"/);
  assert.match(html, /tag-chip-inherited-label[^>]*>inherited</, 'the small label comes from t()');
  assert.match(html, /title="Inherited from the Automaton; change it there"/, 'the tooltip comes from t()');
  assert.equal((html.match(/tag-chip-inherited-label/g) || []).length, 1, 'only the inherited chip is labelled');
});

test('the inherited label and tooltip follow the locale bundle', () => {
  const sb = loadApp();
  sb._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'de.json'), 'utf8'));
  const html = sb._sessionTagChipsHTML(sess({ inherited_tags: ['inh1'] }));
  assert.match(html, /geerbt/);
  assert.match(html, /Vom Automaten geerbt; dort ändern/);
});

test('a tag that is both own and inherited is shown once, as an own chip', () => {
  const sb = loadApp();
  const html = sb._sessionTagChipsHTML(sess({ tags: ['own1'], inherited_tags: ['own1', 'inh1'] }));
  assert.equal((html.match(/data-tag-id="own1"/g) || []).length, 1);
  assert.match(html, /data-tag-id="inh1"/);
});

test('a session with neither own nor inherited tags draws no chips and the card still renders', () => {
  const sb = loadApp();
  assert.equal(sb._sessionTagChipsHTML(sess({})), '');
  assert.equal(sb._sessionTagChipsHTML(sess({ tags: [], inherited_tags: [] })), '');
  assert.equal(sb._sessionTagChipsHTML(null), '');
  const card = sb.sessionCard(sess({}), 0, 1);
  assert.match(card, /session-card/);
  assert.doesNotMatch(card, /tag-chip/);
});

test('IDs the registry does not know are skipped', () => {
  const sb = loadApp();
  assert.equal(sb._sessionTagChipsHTML(sess({ tags: ['gone'], inherited_tags: ['gone2'] })), '');
});

test('tag names are escaped in the chips', () => {
  const sb = loadApp();
  const html = sb._sessionTagChipsHTML(sess({ tags: ['xss1'], inherited_tags: [] }));
  assert.doesNotMatch(html, /<img/);
  assert.match(html, /&lt;img src=x onerror=alert\(1\)&gt;/);
  const inh = sb._sessionTagChipsHTML(sess({ inherited_tags: ['xss1'] }));
  assert.doesNotMatch(inh, /<img/);
  assert.match(inh, /&lt;img/);
});

test('the session card shows both kinds of chip', () => {
  const sb = loadApp();
  const card = sb.sessionCard(sess({ tags: ['own1'], inherited_tags: ['inh1'] }), 0, 1);
  assert.match(card, /data-tag-id="own1"/);
  assert.match(card, /tag-chip-inherited/);
});

test('tree nodes show the chips of each session, parent and child', () => {
  const sb = loadApp();
  const parent = sess({ full_id: 'host-p', id: 'p', tags: ['own1'], inherited_tags: [] });
  const child = sess({ full_id: 'host-c', id: 'c', parent_id: 'host-p', tags: [], inherited_tags: ['inh1'] });
  const html = sb.renderSessionsAsTree([parent, child]);
  assert.match(html, /data-tag-id="own1"/);
  assert.match(html, /class="tag-chip tag-chip-inherited" data-tag-id="inh1"/);
  assert.equal((html.match(/session-card /g) || []).length, 2);
});

// ── Detail view ──────────────────────────────────────────────────────────────

test('the detail Tags row renders only the chips, with no edit button, none note or label', () => {
  const row = makeStubElement();
  const sb = loadApp({ byId: { sessionDetailTagRow: row } });
  sb._renderSessionDetailTagRow(sess({ tags: ['own1'], inherited_tags: ['inh1'] }));
  assert.match(row.innerHTML, /data-tag-id="own1"/);
  assert.match(row.innerHTML, /data-tag-id="inh1"/);
  assert.match(row.innerHTML, /tag-chip-inherited/);
  assert.doesNotMatch(row.innerHTML, /openSessionTagsModal/);
  assert.doesNotMatch(row.innerHTML, /Edit tags/);
  assert.doesNotMatch(row.innerHTML, /No tags/);
  assert.doesNotMatch(row.innerHTML, /Tags/);
});

test('the detail Tags row is hidden and empty when the session has no tags', () => {
  const row = makeStubElement();
  const sb = loadApp({ byId: { sessionDetailTagRow: row } });
  sb._renderSessionDetailTagRow(sess({}));
  assert.equal(row.innerHTML, '');
  assert.equal(row.style.display, 'none');
});

test('the detail Tags row is shown when the session has tags', () => {
  const row = makeStubElement();
  const sb = loadApp({ byId: { sessionDetailTagRow: row } });
  sb._renderSessionDetailTagRow(sess({}));
  sb._renderSessionDetailTagRow(sess({ tags: ['own1'], inherited_tags: [] }));
  assert.notEqual(row.style.display, 'none');
});

test('the detail view template carries the Tags row slot', () => {
  const src = fs.readFileSync(path.join(WEB, 'app.js'), 'utf8');
  assert.match(src, /id="sessionDetailTagRow"/);
  assert.match(src, /_renderSessionDetailTagRow\(sess\);/);
});

// ── Editor ───────────────────────────────────────────────────────────────────

function openEditor(sb, s) {
  run(sb, `state.sessions = ${JSON.stringify([s])};`);
  const modal = captureModal(sb);
  sb.openSessionTagsModal(s.full_id);
  return modal;
}

test('the editor lists every tag, ticks only the own tags and names the inherited ones', async () => {
  const sb = loadApp();
  const modal = openEditor(sb, sess({ tags: ['own1'], inherited_tags: ['inh1'] }));
  await flushAsync(5);
  assert.equal(modal.calls.length, 1);
  const spec = modal.calls[0];
  assert.equal(spec.title, 'Session tags');
  const boxes = pickerFrom(spec.body, 'sessionTagsModalPicker').querySelectorAll();
  assert.deepEqual(boxes.map(b => b.value).sort(), TAGS.map(tg => tg.id).sort(), 'nothing is excluded');
  assert.deepEqual(boxes.filter(b => b.checked).map(b => b.value), ['own1'], 'only the own tag is ticked');
  assert.match(spec.body, /Inherited from the Automaton \(read-only\):[^<]*Release/);
});

test('saving posts the session ID and the own tag IDs, and never the inherited ones', async () => {
  const sb = loadApp();
  const s = sess({ tags: ['own1'], inherited_tags: ['inh1'] });
  const modal = openEditor(sb, s);
  await flushAsync(5);
  const spec = modal.calls[0];
  const picker = pickerFrom(spec.body, 'sessionTagsModalPicker');
  const byId = sb.document.getElementById;
  sb.document.getElementById = id => (id === 'sessionTagsModalPicker' ? picker : byId(id));
  const calls = captureApi(sb, Object.assign({}, s, { tags: ['own1'], inherited_tags: ['inh1'] }));
  sb.showToast = () => {};
  spec.onConfirm();
  await flushAsync(5);
  const saves = calls.filter(c => c.url === '/api/sessions/tags');
  assert.equal(saves.length, 1, 'one save request');
  assert.equal(calls[0].url, '/api/sessions/tags');
  assert.equal(saves[0].init.method, 'POST');
  assert.deepEqual(JSON.parse(JSON.stringify(saves[0].body)), { id: 'host-aaaa', tag_ids: ['own1'] });
  assert.ok(!saves[0].body.tag_ids.includes('inh1'));
});

test('ticking another tag adds it to the own list; unticking clears it', async () => {
  const sb = loadApp();
  const s = sess({ tags: ['own1'], inherited_tags: ['inh1'] });
  const modal = openEditor(sb, s);
  await flushAsync(5);
  const spec = modal.calls[0];
  const picker = pickerFrom(spec.body, 'sessionTagsModalPicker');
  picker.querySelectorAll().forEach(b => { b.checked = b.value === 'own2'; });
  const byId = sb.document.getElementById;
  sb.document.getElementById = id => (id === 'sessionTagsModalPicker' ? picker : byId(id));
  const calls = captureApi(sb, s);
  sb.showToast = () => {};
  spec.onConfirm();
  await flushAsync(5);
  assert.deepEqual(JSON.parse(JSON.stringify(calls[0].body.tag_ids)), ['own2']);
});

test('the server refusal text is shown through the tag error text', async () => {
  const sb = loadApp();
  const s = sess({ tags: [] });
  const modal = openEditor(sb, s);
  await flushAsync(5);
  const spec = modal.calls[0];
  const picker = pickerFrom(spec.body, 'sessionTagsModalPicker');
  const byId = sb.document.getElementById;
  sb.document.getElementById = id => (id === 'sessionTagsModalPicker' ? picker : byId(id));
  sb.apiFetch = () => Promise.reject(new Error(JSON.stringify({ error: 'tag not found: nope' })));
  const toasts = [];
  sb.showToast = (msg, kind) => toasts.push([msg, kind]);
  spec.onConfirm();
  await flushAsync(5);
  assert.deepEqual(toasts, [['tag not found: nope', 'error']]);
});

// ── New-session form ─────────────────────────────────────────────────────────

test('the new-session form draws a tag picker and hides it for a project-profile spawn', () => {
  const src = fs.readFileSync(path.join(WEB, 'app.js'), 'utf8');
  assert.match(src, /id="sessTagsRow"/);
  assert.match(src, /id="newSessionTagsWrap"/);
  assert.match(src, /_newSessionTagsMount\(\);/);
});

test('the new-session picker lists every tag once the registry has loaded', async () => {
  const wrap = makeStubElement();
  const sb = loadApp({ byId: { newSessionTagsWrap: wrap } });
  sb._newSessionTagsMount();
  await flushAsync(5);
  assert.match(wrap.innerHTML, /id="newSessionTags"/);
  const boxes = pickerFrom(wrap.innerHTML, 'newSessionTags').querySelectorAll();
  assert.deepEqual(boxes.map(b => b.value).sort(), TAGS.map(tg => tg.id).sort());
  assert.ok(boxes.every(b => !b.checked), 'a new session starts with nothing ticked');
});

function submitWith(sb, ticked) {
  const picker = { querySelectorAll: () => TAGS.map(tg => ({ value: tg.id, checked: ticked.includes(tg.id) })) };
  const els = {
    taskInput: { value: 'do the thing', focus() {} },
    newSessionTags: picker,
  };
  sb.document.getElementById = id => els[id] || null;
  sb.document.querySelector = () => null;
  const calls = captureApi(sb, { full_id: 'host-new' });
  sb.updateSession = () => {};
  sb.closeNewSessionModal = () => {};
  sb.navigate = () => {};
  sb.submitNewSession();
  return calls;
}

test('the start request carries the ticked tags as `tags`', async () => {
  const sb = loadApp();
  const calls = submitWith(sb, ['own1', 'inh1']);
  await flushAsync(5);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, '/api/sessions/start');
  assert.deepEqual(JSON.parse(JSON.stringify(calls[0].body.tags)), ['own1', 'inh1']);
  assert.equal(calls[0].body.task, 'do the thing');
});

test('the start request omits `tags` when nothing is ticked', async () => {
  const sb = loadApp();
  const calls = submitWith(sb, []);
  await flushAsync(5);
  assert.equal(calls.length, 1);
  assert.equal('tags' in calls[0].body, false);
});

// ── Batch action ─────────────────────────────────────────────────────────────

test('the select bar offers a Tag action for the selected sessions', () => {
  const src = fs.readFileSync(path.join(WEB, 'app.js'), 'utf8');
  assert.match(src, /onclick="batchTagSelectedSessions\(\)"/);
});

test('batch Tag adds the picked tags to each selected session and keeps the tags it has', async () => {
  const sb = loadApp();
  const a = sess({ full_id: 'host-a', id: 'a', tags: ['own1'], inherited_tags: ['inh1'] });
  const b = sess({ full_id: 'host-b', id: 'b', tags: [], inherited_tags: [] });
  run(sb, `state.sessions = ${JSON.stringify([a, b])}; state.selectedSessions = new Set(['host-a', 'host-b']); state.selectMode = true; state.activeView = 'other';`);
  const modal = captureModal(sb);
  sb.batchTagSelectedSessions();
  await flushAsync(5);
  const spec = modal.calls[0];
  assert.equal(spec.title, 'Add tags to 2 sessions');
  const picker = pickerFrom(spec.body, 'sessionBatchTagPicker');
  picker.querySelectorAll().forEach(bx => { bx.checked = bx.value === 'own2'; });
  const byId = sb.document.getElementById;
  sb.document.getElementById = id => (id === 'sessionBatchTagPicker' ? picker : byId(id));
  const calls = captureApi(sb, (url, init) => {
    const body = JSON.parse(init.body);
    return Object.assign({}, body.id === 'host-a' ? a : b, { tags: body.tag_ids });
  });
  sb.showToast = () => {};
  spec.onConfirm();
  await flushAsync(5);
  const bodies = calls.map(c => JSON.parse(JSON.stringify(c.body)));
  assert.deepEqual(bodies, [
    { id: 'host-a', tag_ids: ['own1', 'own2'] },
    { id: 'host-b', tag_ids: ['own2'] },
  ]);
  assert.ok(bodies.every(x => !x.tag_ids.includes('inh1')), 'an inherited ID is never sent');
  assert.equal(run(sb, 'state.selectedSessions.size'), 0);
  assert.equal(run(sb, 'state.selectMode'), false);
});

test('batch Tag with nothing ticked keeps the modal open and sends nothing', async () => {
  const sb = loadApp();
  run(sb, `state.sessions = ${JSON.stringify([sess({})])}; state.selectedSessions = new Set(['host-aaaa']);`);
  const modal = captureModal(sb);
  sb.batchTagSelectedSessions();
  await flushAsync(5);
  const picker = pickerFrom(modal.calls[0].body, 'sessionBatchTagPicker');
  const byId = sb.document.getElementById;
  sb.document.getElementById = id => (id === 'sessionBatchTagPicker' ? picker : byId(id));
  const calls = captureApi(sb, {});
  assert.equal(modal.calls[0].onConfirm(), false);
  assert.equal(calls.length, 0);
});

// ── Live refresh ─────────────────────────────────────────────────────────────

test('a tag_update frame refetches the registry and the rows, then repaints the sessions list', async () => {
  const sb = loadApp();
  run(sb, `state.activeView = 'sessions';`);
  const urls = [];
  sb.apiFetch = u => { urls.push(u); return Promise.resolve({ tags: [{ id: 'own1', name: 'Renamed' }] }); };
  let listLoads = 0;
  let paints = 0;
  sb.loadSessionsList = () => { listLoads++; return Promise.resolve([]); };
  sb.renderSessionsView = () => { paints++; };
  sb.handleMessage({ type: 'tag_update', data: {} });
  assert.equal(sb.__timers.queue.length, 1, 'one debounced refresh is pending');
  sb.__timers.queue.shift().fn();
  await flushAsync(20);
  assert.ok(urls.includes('/api/autonomous/tags?counts=true'));
  assert.equal(listLoads, 1);
  assert.equal(paints, 1);
  assert.equal(run(sb, '_tagsState.byId.own1.name'), 'Renamed');
});

test('a tag_update frame on the session detail refreshes the Tags row', async () => {
  const row = makeStubElement();
  const sb = loadApp({ byId: { sessionDetailTagRow: row } });
  const s = sess({ tags: ['own1'], inherited_tags: [] });
  run(sb, `state.activeView = 'session-detail'; state.activeSession = 'host-aaaa'; state.sessions = ${JSON.stringify([s])};`);
  const urls = [];
  sb.apiFetch = u => {
    urls.push(u);
    if (u.startsWith('/api/sessions/')) return Promise.resolve(Object.assign({}, s, { tags: ['own1'], inherited_tags: ['inh1'] }));
    return Promise.resolve({ tags: TAGS });
  };
  sb.handleMessage({ type: 'tag_update', data: {} });
  sb.__timers.queue.shift().fn();
  await flushAsync(20);
  assert.ok(urls.includes('/api/sessions/host-aaaa'));
  assert.match(row.innerHTML, /tag-chip-inherited/, 'the inherited tag the refetch brought is drawn');
});

test('the sessions list loads the registry once when it is not loaded yet', async () => {
  const sb = loadApp();
  run(sb, `_tagsState.loaded = false;`);
  let loads = 0;
  sb.apiFetch = () => { loads++; return Promise.resolve({ tags: TAGS }); };
  let paints = 0;
  sb._sessionTagsEnsure(() => { paints++; }, [sess({ tags: ['own1'] })]);
  await flushAsync(10);
  assert.equal(loads, 1);
  assert.equal(paints, 1);
  sb._sessionTagsEnsure(() => { paints++; }, [sess({ tags: ['own1'] })]);
  await flushAsync(10);
  assert.equal(loads, 1, 'a loaded registry is not fetched again');
  assert.equal(paints, 1);
});

test('a view of untagged sessions fetches no registry', async () => {
  const sb = loadApp();
  run(sb, `_tagsState.loaded = false;`);
  let loads = 0;
  sb.apiFetch = () => { loads++; return Promise.resolve({ tags: TAGS }); };
  sb._sessionTagsEnsure(() => {}, [sess({}), sess({ tags: [], inherited_tags: [] })]);
  sb._sessionTagsEnsure(() => {}, []);
  await flushAsync(10);
  assert.equal(loads, 0);
});

test('a failed registry load does not repaint', async () => {
  const sb = loadApp();
  run(sb, `_tagsState.loaded = false;`);
  sb.apiFetch = () => Promise.reject(new Error('down'));
  let paints = 0;
  sb._sessionTagsEnsure(() => { paints++; }, [sess({ tags: ['own1'] })]);
  await flushAsync(10);
  assert.equal(paints, 0);
});

// ── Locales ──────────────────────────────────────────────────────────────────

const NEW_KEYS = [
  'tag_inherited_label', 'tag_inherited_tip', 'tag_inherited_note',
  'tag_session_edit_title', 'tag_session_batch_title',
];

test('the new strings exist in all five bundles, keep their placeholder and carry no tracker ID', () => {
  for (const l of LOCALES) {
    const bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', l + '.json'), 'utf8'));
    for (const k of NEW_KEYS) {
      assert.equal(typeof bundle[k], 'string', `${l}.json is missing ${k}`);
      assert.ok(bundle[k].length > 0, `${l}.json has an empty ${k}`);
      assert.doesNotMatch(bundle[k], /\bBL\d+\b/, `${l}.json ${k} carries a tracker ID`);
    }
    assert.match(bundle.tag_session_batch_title, /%1\$d/, `${l}.json lost the count placeholder`);
  }
});

test('every t() key the session tag code reads exists in the English bundle', () => {
  const en = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  const src = fs.readFileSync(path.join(WEB, 'app.js'), 'utf8');
  const start = src.indexOf('// ── Session tags ──');
  const end = src.indexOf('// ── Tag browser (Automata and Automation lists)');
  assert.ok(start > 0 && end > start);
  const keys = new Set();
  const re = /_tagT\('([a-z_]+)'/g;
  let m;
  const block = src.slice(start, end);
  while ((m = re.exec(block))) keys.add(m[1]);
  assert.ok(keys.size >= 8);
  for (const k of keys) assert.ok(k in en, `en.json has no ${k}`);
});
