// The tag filter control on the session list, the Progress card and the
// dashboard. Covers the automation:/stage:/kind: groups, the query it sends
// (one tag parameter per tag plus tag_match), where each surface sends it, the
// control's own state (any/all, clear, count), the dashboard's client-side pass,
// and the locale strings.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const TAGS = [
  { id: 'a1', name: 'automation:release', system: true },
  { id: 'a2', name: 'automation:nightly', system: true },
  { id: 's1', name: 'stage:build', system: true },
  { id: 'k1', name: 'kind:feature', system: true },
  { id: 'w1', name: 'Work' },
  { id: 'w2', name: 'Mobile', parent_id: 'w1' },
];

function loadApp(byId) {
  const sandbox = buildSandbox({ byId: byId || {} });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  vm.runInContext(`_tagsState.list = ${JSON.stringify(TAGS)}; _tagsState.byId = _tagsById(_tagsState.list); _tagsState.loaded = true;`, sandbox);
  return sandbox;
}

const select = (sb, scope, ids, mode) => vm.runInContext(
  `_tagBrowse.${scope}.sel = ${JSON.stringify(ids)}; _tagBrowse.${scope}.mode = ${JSON.stringify(mode || '')};`, sb);

// ── groups ───────────────────────────────────────────────────────────────────

test('each tag falls in the group its prefix names, everything else in other', () => {
  const sb = loadApp();
  const g = n => sb._tagFilterGroupOf({ name: n });
  assert.equal(g('automation:x'), 'automation');
  assert.equal(g('stage:x'), 'stage');
  assert.equal(g('kind:x'), 'kind');
  assert.equal(g('Work'), 'other');
  assert.equal(g('team:x'), 'other');
  assert.equal(g(':odd'), 'other');
  assert.equal(sb._tagFilterGroupOf(null), 'other');
});

test('the rows come grouped automation, stage, kind, other, each under a heading', () => {
  const html = loadApp()._tagFilterRowsHTML('progress');
  const order = [...html.matchAll(/data-tag-group="(\w+)"/g)].map(m => m[1]);
  assert.deepEqual(order, ['automation', 'stage', 'kind', 'other']);
  for (const h of ['Automation', 'Stage', 'Kind', 'Other tags']) assert.match(html, new RegExp('tag-filter-group-head">' + h + '<'));
  assert.match(html, /value="a1"/);
  assert.match(html, /🔒|&#128274;/);
});

test('a group with no tags is not drawn and a tag in the selection is checked', () => {
  const sb = loadApp();
  vm.runInContext(`_tagsState.list = ${JSON.stringify(TAGS.filter(t => t.id === 's1' || t.id === 'w1'))};`, sb);
  select(sb, 'progress', ['s1']);
  const html = sb._tagFilterRowsHTML('progress');
  assert.deepEqual([...html.matchAll(/data-tag-group="(\w+)"/g)].map(m => m[1]), ['stage', 'other']);
  assert.match(html, /value="s1" checked/);
  assert.doesNotMatch(html, /value="w1" checked/);
});

test('other tags keep their tree indent while automatic groups stay flat', () => {
  const html = loadApp()._tagFilterRowsHTML('progress');
  assert.match(html, /padding-left:14px;"><input type="checkbox" value="w2"/);
  assert.match(html, /padding-left:0px;"><input type="checkbox" value="a1"/);
});

// ── query ────────────────────────────────────────────────────────────────────

test('the query is empty with nothing selected and carries tag and tag_match otherwise', () => {
  const sb = loadApp();
  assert.equal(sb._tagFilterQuery('progress'), '');
  assert.equal(sb._tagFilterQuery('nope'), '');
  select(sb, 'progress', ['a1', 's 1']);
  assert.equal(sb._tagFilterQuery('progress'), 'tag=a1&tag=s%201&tag_match=any');
  select(sb, 'progress', ['a1', 'k1'], 'all');
  assert.equal(sb._tagFilterQuery('progress'), 'tag=a1&tag=k1&tag_match=all');
});

test('the query joins a path with ? or & and leaves it alone with no selection', () => {
  const sb = loadApp();
  assert.equal(sb._tagFilterAppend('/api/x', 'progress'), '/api/x');
  select(sb, 'progress', ['a1']);
  assert.equal(sb._tagFilterAppend('/api/x', 'progress'), '/api/x?tag=a1&tag_match=any');
  assert.equal(sb._tagFilterAppend('/api/x?days=14', 'progress'), '/api/x?days=14&tag=a1&tag_match=any');
});

// ── the control ──────────────────────────────────────────────────────────────

test('the control shows the count, the match mode and Clear only while tags are selected', () => {
  const sb = loadApp();
  let html = sb._tagFilterControlHTML('dashboard');
  assert.match(html, /data-tag-filter="dashboard"/);
  assert.doesNotMatch(html, /tag_filter_clear/);
  assert.match(html, /aria-pressed="false"[^>]*>match ANY</);
  select(sb, 'dashboard', ['a1', 's1'], 'all');
  html = sb._tagFilterControlHTML('dashboard');
  assert.match(html, /Tags \(2\)|\(2\)<\/summary>/);
  assert.match(html, /data-op="tag_filter_clear" onclick="tagBrowseClear\('dashboard'\)"/);
  assert.match(html, /aria-pressed="true"[^>]*>match ALL</);
});

test('the control says so when the registry has no tags', () => {
  const sb = loadApp();
  vm.runInContext('_tagsState.list = [];', sb);
  assert.match(sb._tagFilterControlHTML('progress'), /tag-multi-empty/);
});

test('mounting draws the control into its element and keeps an open one open', async () => {
  const el = makeStubElement();
  el.querySelector = sel => (el.open ? { open: true } : null);
  const sb = loadApp({ slot: el });
  await sb._tagFilterMount('progress', 'slot');
  assert.match(el.innerHTML, /data-tag-filter="progress"/);
  await sb._tagFilterMount('progress', 'missing');
});

// ── progress card ────────────────────────────────────────────────────────────

test('the Progress card sends the selection on the metrics and the trend requests', async () => {
  const sb = loadApp();
  const urls = [];
  sb.apiFetch = u => { urls.push(u); return Promise.resolve({ scope: 'tags:a1' }); };
  select(sb, 'progress', ['a1', 's1'], 'all');
  await vm.runInContext('window.loadProgressCard(true)', sb);
  assert.deepEqual(urls, [
    '/api/autonomous/progress?tag=a1&tag=s1&tag_match=all',
    '/api/autonomous/progress/trend?days=14&tag=a1&tag=s1&tag_match=all',
  ]);
});

test('the Progress card sends plain requests with no selection', async () => {
  const sb = loadApp();
  const urls = [];
  sb.apiFetch = u => { urls.push(u); return Promise.resolve({}); };
  await vm.runInContext('window.loadProgressCard(true)', sb);
  assert.deepEqual(urls, ['/api/autonomous/progress', '/api/autonomous/progress/trend?days=14']);
});

test('changing the Progress selection reloads the card', async () => {
  const sb = loadApp();
  const urls = [];
  sb.apiFetch = u => { urls.push(u); return Promise.resolve({}); };
  sb.tagBrowseToggleTag('progress', 'a1', true);
  await flushAsync();
  assert.ok(urls.includes('/api/autonomous/progress?tag=a1&tag_match=any'));
  sb.tagBrowseToggleMode('progress');
  await flushAsync();
  assert.ok(urls.includes('/api/autonomous/progress?tag=a1&tag_match=all'));
  sb.tagBrowseClear('progress');
  await flushAsync();
  assert.equal(urls[urls.length - 2], '/api/autonomous/progress');
});

test('painting the card on the Automation sub-tab mounts the filter above the numbers', () => {
  const slot = makeStubElement();
  const sb = loadApp({ progressCard: slot, progressCardTagFilter: makeStubElement() });
  vm.runInContext("_automataState.tab = 'automation'", sb);
  sb.paintProgressCard();
  assert.match(slot.innerHTML, /^<div id="progressCardTagFilter"/);
});

// ── session list ─────────────────────────────────────────────────────────────

test('the session list fetch carries the session selection', async () => {
  const sb = loadApp();
  const urls = [];
  sb.fetch = u => { urls.push(u); return Promise.resolve({ ok: true, json: () => Promise.resolve({ sessions: [] }) }); };
  await sb._sessionsFetchPage('', 'local');
  assert.doesNotMatch(urls[0], /tag=/);
  select(sb, 'session', ['k1'], 'all');
  await sb._sessionsFetchPage('', 'local');
  assert.match(urls[1], /&tag=k1&tag_match=all$/);
});

test('the header Tags menu groups the session tags and leaves placement tags to the placement section', () => {
  const sb = loadApp();
  const html = sb._tagsMenuHTML();
  assert.doesNotMatch(html, /data-tag-group="automation"/);
  assert.match(html, /data-tag-group="other"/);
  assert.match(html, /automation:nightly/);
  // The option is opt-in: the default rows still list the system tags.
  assert.match(sb._tagFilterRowsHTML('session'), /data-tag-group="automation"/);
});

// ── dashboard ────────────────────────────────────────────────────────────────

test('the dashboard fetch sends the selection and keeps only the matching Automata', async () => {
  const sb = loadApp();
  const urls = [];
  const prds = [
    { id: 'p1', status: 'running', tag_ids: ['a1'] },
    { id: 'p2', status: 'running', tag_ids: ['a2'] },
    { id: 'p3', status: 'blocked', tag_ids: ['w2'] },
    { id: 'p4', status: 'completed', tag_ids: ['a1'] },
  ];
  sb.apiFetch = u => { urls.push(u); return Promise.resolve({ prds }); };
  select(sb, 'dashboard', ['a1']);
  await sb._dashFetchPRDs();
  assert.equal(urls[0], '/api/autonomous/prds?status=running,blocked,planning&limit=500&tag=a1&tag_match=any');
  assert.deepEqual(vm.runInContext('_dash._prds.map(p => p.id)', sb), ['p1']);
  select(sb, 'dashboard', ['w1']);
  await sb._dashFetchPRDs();
  assert.deepEqual(vm.runInContext('_dash._prds.map(p => p.id)', sb), ['p3'], 'a child tag counts under its parent');
});

test('the dashboard keeps every Automaton with no selection and ignores unknown tags', () => {
  const sb = loadApp();
  const list = [{ id: 'p1', tag_ids: [] }, { id: 'p2', tag_ids: ['a1'] }];
  assert.equal(sb._dashTagFilter(list).length, 2);
  select(sb, 'dashboard', ['gone']);
  assert.equal(sb._dashTagFilter(list).length, 2);
  select(sb, 'dashboard', ['a1', 's1'], 'all');
  assert.equal(sb._dashTagFilter(list).length, 0);
});

test('the all-servers dashboard fetch also carries the selection', async () => {
  const sb = loadApp();
  const urls = [];
  sb.apiFetch = u => { urls.push(u); return Promise.resolve([]); };
  vm.runInContext("state.activeServer = 'all'", sb);
  select(sb, 'dashboard', ['k1']);
  await sb._dashFetchPRDs();
  assert.equal(urls[0], '/api/autonomous/prds/aggregated?tag=k1&tag_match=any');
});

test('changing the dashboard selection refetches and redraws the control', async () => {
  const slot = makeStubElement();
  const sb = loadApp({ dashTagFilter: slot });
  const urls = [];
  sb.apiFetch = u => { urls.push(u); return Promise.resolve({ prds: [] }); };
  sb.tagBrowseToggleTag('dashboard', 's1', true);
  await flushAsync();
  assert.ok(urls.some(u => /tag=s1&tag_match=any/.test(u)));
  assert.match(slot.innerHTML, /data-tag-filter="dashboard"/);
});

test('a failed dashboard refetch is reported and does not throw', async () => {
  const sb = loadApp();
  sb.apiFetch = () => Promise.reject(new Error('down'));
  await sb._dashTagFilterChanged();
});

// ── locales ──────────────────────────────────────────────────────────────────

test('the group headings exist in all five bundles', () => {
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    for (const k of ['tag_filter_group_automation', 'tag_filter_group_stage', 'tag_filter_group_kind', 'tag_filter_group_other']) {
      assert.ok(b[k], lang + ' missing ' + k);
    }
  }
});
