// Pinned groups above the tag browser (Needs attention and Running), the landing
// rule, and the live refresh. Covers attention membership by status (a custom
// setting and a task held for review included), the landing outcome for each
// default_view value and the viewer's override, that the landing never switches
// while the page is open, that an event burst becomes one refetch, that the list
// poll catches up with the stream down, and that re-renders add no timer.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const vm = require('vm');
const { buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const WEB = __dirname;
const SRC = fs.readFileSync(path.join(WEB, 'app.js'), 'utf8');

const TAGS = [
  { id: 'w', name: 'Work', subtree_count: 3, direct_count: 1 },
  { id: 'h', name: 'Home', subtree_count: 1, direct_count: 1 },
];

function prd(id, status, extra) {
  return Object.assign({ id, title: 'Title ' + id, status, tags: [], task_count: 4, tasks_done: 1 }, extra || {});
}

// A sandbox whose timers are a visible queue, so a test can see what is pending and
// fire it by hand. Intervals are counted.
function loadApp(opts) {
  const o = opts || {};
  const sandbox = buildSandbox(o);
  const timers = { queue: [], seq: 0, intervals: 0, cleared: 0 };
  sandbox.setTimeout = (fn, ms) => { const id = ++timers.seq; timers.queue.push({ id, fn, ms }); return id; };
  sandbox.clearTimeout = id => {
    const k = timers.queue.findIndex(x => x.id === id);
    if (k >= 0) { timers.queue.splice(k, 1); timers.cleared++; }
  };
  sandbox.setInterval = () => { timers.intervals++; return ++timers.seq; };
  sandbox.clearInterval = () => {};
  vm.createContext(sandbox);
  loadScript(path.join(WEB, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  vm.runInContext(`_tagsState.list = ${JSON.stringify(TAGS)}; _tagsState.byId = _tagsById(_tagsState.list); _tagsState.loaded = true;`, sandbox);
  timers.queue.length = 0; // the app's own start-up timers are not under test
  sandbox.__timers = timers;
  return sandbox;
}

function run(sb, code) { return vm.runInContext(code, sb); }
function ids(list) { return Array.from(list, x => x.id); }

// Fire the oldest pending timer; resolves once its promise chain has settled.
async function fireNext(sb) {
  const t = sb.__timers.queue.shift();
  assert.ok(t, 'a timer was pending');
  t.fn();
  await flushAsync(20);
  return t;
}
function pending(sb, ms) { return sb.__timers.queue.filter(t => ms === undefined || t.ms === ms); }

// ── Membership ───────────────────────────────────────────────────────────────

test('attention holds blocked, needs_review and paused by default, once each', () => {
  const sb = loadApp();
  const items = [
    prd('a', 'blocked'), prd('b', 'running'), prd('c', 'needs_review'), prd('d', 'paused'),
    prd('e', 'completed'), prd('a', 'blocked'), prd('t', 'blocked', { is_template: true }),
  ];
  assert.deepEqual(ids(sb._tagAttentionMembers(items, undefined)), ['a', 'c', 'd']);
  assert.deepEqual(ids(sb._tagAttentionMembers(items, [])), ['a', 'c', 'd'], 'an empty list is the default list');
});

test('a custom attention_statuses setting replaces the default list', () => {
  const sb = loadApp();
  const items = [prd('a', 'blocked'), prd('b', 'running'), prd('c', 'paused'), prd('d', 'failed')];
  assert.deepEqual(ids(sb._tagAttentionMembers(items, ' Running , failed ')), ['b', 'd']);
  assert.deepEqual(ids(sb._tagAttentionMembers(items, ['paused'])), ['c']);
  assert.deepEqual(Array.from(sb._tagAttentionStatusList('blocked, paused,blocked')), ['blocked', 'paused']);
  assert.deepEqual(Array.from(sb._tagAttentionStatusList('')), ['blocked', 'needs_review', 'paused']);
  assert.deepEqual(Array.from(sb._tagAttentionStatusList(null)), ['blocked', 'needs_review', 'paused']);
});

test('an Automaton with a task held for review is in the group whatever its status', () => {
  const sb = loadApp();
  const held = prd('h', 'running', { review_needed: [{ task_id: 't1' }] });
  const none = prd('n', 'running', { review_needed: [] });
  assert.deepEqual(ids(sb._tagAttentionMembers([held, none, prd('r', 'running')], ['blocked'])), ['h']);
});

test('the config load reads default_view and the attention statuses', async () => {
  const sb = loadApp();
  sb.apiFetch = () => Promise.resolve({ autonomous: { tags: { default_view: 'Tags', attention_statuses: 'blocked, failed' } } });
  const cfg = await run(sb, '_tagBrowseConfigLoad()');
  assert.equal(cfg.defaultView, 'tags');
  assert.deepEqual(Array.from(cfg.attentionStatuses), ['blocked', 'failed']);
});

// ── Running strip ────────────────────────────────────────────────────────────

test('the running strip shows five lines and then +N more', () => {
  const sb = loadApp();
  const items = [1, 2, 3, 4, 5, 6, 7].map(n => prd('r' + n, 'running')).concat([prd('x', 'paused')]);
  const s = sb._tagRunningStrip(items, 0, 5);
  assert.equal(s.shown.length, 5);
  assert.equal(s.more, 2);
  const server = sb._tagRunningStrip(items.slice(0, 5), 12, 5);
  assert.equal(server.shown.length, 5);
  assert.equal(server.more, 7, 'the server total counts the lines the page did not carry');
  assert.equal(sb._tagRunningStrip([prd('a', 'paused')], 0, 5).shown.length, 0);
});

// ── Drawing ──────────────────────────────────────────────────────────────────

function setPinned(sb, att, run) {
  run_(sb, `_tagPinned.attention = ${JSON.stringify(att)}; _tagPinned.running = ${JSON.stringify(run)};`);
}
function run_(sb, code) { return vm.runInContext(code, sb); }

test('Needs attention is open and names the blocked reason where one exists', () => {
  const sb = loadApp();
  setPinned(sb, {
    statuses: ['blocked', 'needs_review', 'paused'],
    items: [prd('a', 'blocked', { blocked_reason: 'waiting on a credential' }), prd('b', 'paused'), prd('c', 'running', { review_needed: [{}, {}] })],
  }, null);
  const html = sb._tagPinnedHTML();
  assert.match(html, /<details[^>]*tag-pinned-attention[^>]*\sopen/);
  assert.ok(html.includes('Needs attention'));
  assert.ok(html.includes('waiting on a credential'));
  assert.ok(html.includes('2 task(s) held for review'));
  assert.equal((html.match(/class="tag-pinned-row"/g) || []).length, 3);
  assert.ok(!html.includes('tag-pinned-running'), 'no running Automata: no strip');
});

test('an empty group takes no height and two empty groups hide the area', () => {
  const sb = loadApp();
  setPinned(sb, { statuses: [], items: [] }, { items: [], total: 0 });
  assert.equal(sb._tagPinnedHTML(), '');
  setPinned(sb, { statuses: [], items: [] }, { items: [prd('r', 'running')], total: 1 });
  const html = sb._tagPinnedHTML();
  assert.ok(html.includes('tag-pinned-running'));
  assert.ok(!html.includes('tag-pinned-attention'));
});

test('the running strip is collapsed by default on a phone and open on a wide screen', () => {
  const sb = loadApp();
  const runs = Array.from({ length: 7 }, (_, i) => prd('r' + i, 'running'));
  setPinned(sb, { statuses: [], items: [] }, { items: runs.slice(0, 5), total: 7 });
  sb.innerWidth = 390;
  const phone = sb._tagPinnedHTML();
  assert.doesNotMatch(phone, /<details[^>]*tag-pinned-running[^>]*\sopen/);
  assert.ok(phone.includes('+2 more'));
  sb.innerWidth = 1200;
  assert.match(sb._tagPinnedHTML(), /<details[^>]*tag-pinned-running[^>]*\sopen/);
  sb.localStorage.setItem('cs_tag_sections', JSON.stringify({ 'pinned:running': false }));
  assert.doesNotMatch(sb._tagPinnedHTML(), /<details[^>]*tag-pinned-running[^>]*\sopen/, 'the viewer\'s choice wins');
});

test('a hostile title and reason are escaped', () => {
  const sb = loadApp();
  setPinned(sb, { statuses: [], items: [prd('a', 'blocked', { title: '<img src=x onerror=1>', blocked_reason: '"><script>1</script>' })] }, null);
  const html = sb._tagPinnedHTML();
  assert.ok(!html.includes('<img'));
  assert.ok(!html.includes('<script>'));
});

test('the header shows Attention (N) at every count, zero included', () => {
  const sb = loadApp();
  setPinned(sb, { statuses: [], items: [] }, null);
  assert.ok(sb._tagViewSwitchHTML('automata', 'list').includes('Attention (0)'));
  setPinned(sb, { statuses: [], items: [prd('a', 'blocked'), prd('b', 'paused')] }, null);
  assert.ok(sb._tagViewSwitchHTML('automation', 'tags').includes('Attention (2)'));
});

test('a pinned item still appears in its tag section: groups are views, not moves', () => {
  const sb = loadApp();
  sb.innerWidth = 1200;
  const item = prd('a', 'blocked', { tags: ['w'] });
  setPinned(sb, { statuses: [], items: [item] }, null);
  run_(sb, `_tagLanding.view = 'tags'; _tagBrowse.automata.attn = false; _tagBrowse.automata.view = 'tags';`);
  const html = sb._tagBrowseMountHTML('automata', [item], 1, it => `<tile id="${it.id}"></tile>`, '');
  assert.ok(html.includes('tag-pinned-attention'));
  assert.ok(html.includes('data-tag-section="automata:w"'));
  assert.ok(html.includes('<tile id="a">'), 'the tile is still under its tag');
  assert.ok(html.indexOf('tag-pinned') < html.indexOf('tag-sections'), 'the pinned area sits above the browser');
});

test('the Attention view draws only the pinned groups, or a note when nothing needs attention', () => {
  const sb = loadApp();
  const tile = it => `<tile id="${it.id}"></tile>`;
  setPinned(sb, { statuses: [], items: [prd('a', 'blocked')] }, null);
  run_(sb, `_tagLanding.view = 'attention'; _tagBrowse.automata.attn = true;`);
  const withItems = sb._tagBrowseMountHTML('automata', [prd('a', 'blocked')], 1, tile, '');
  assert.ok(withItems.includes('tag-pinned-attention'));
  assert.ok(!withItems.includes('<tile'));
  setPinned(sb, { statuses: [], items: [] }, null);
  const none = sb._tagBrowseMountHTML('automata', [], 0, tile, '');
  assert.ok(none.includes('Nothing needs attention right now.'));
  assert.ok(none.includes('Attention (0)'));
});

test('without an attention read the group is built from the loaded list', () => {
  const sb = loadApp();
  setPinned(sb, null, null);
  run_(sb, `_automataState.allPrds = ${JSON.stringify([prd('a', 'paused'), prd('b', 'running'), prd('c', 'completed')])};`);
  assert.deepEqual(ids(sb._tagAttentionGroup()), ['a']);
  assert.deepEqual(ids(sb._tagRunningGroup().shown), ['b']);
});

// ── Landing rule ─────────────────────────────────────────────────────────────

test('auto opens attention when something needs it and the tag browser otherwise', () => {
  const sb = loadApp();
  assert.equal(sb._tagLandingDecide('auto', '', 1), 'attention');
  assert.equal(sb._tagLandingDecide('auto', '', 7), 'attention');
  assert.equal(sb._tagLandingDecide('auto', '', 0), 'tags');
  assert.equal(sb._tagLandingDecide('', '', 0), 'tags', 'an unset default is auto');
  assert.equal(sb._tagLandingDecide('bogus', '', 2), 'attention', 'an unknown value is auto');
});

test('attention and tags force one view whatever the count', () => {
  const sb = loadApp();
  assert.equal(sb._tagLandingDecide('attention', '', 0), 'attention');
  assert.equal(sb._tagLandingDecide('tags', '', 5), 'tags');
});

test('the viewer\'s override beats every default_view value', () => {
  const sb = loadApp();
  for (const d of ['auto', 'attention', 'tags']) {
    for (const n of [0, 3]) {
      assert.equal(sb._tagLandingDecide(d, 'tags', n), 'tags', `${d}/${n} with override tags`);
      assert.equal(sb._tagLandingDecide(d, 'attention', n), 'attention', `${d}/${n} with override attention`);
    }
  }
});

test('the override is read from storage and a broken storage is ignored', () => {
  const sb = loadApp();
  assert.equal(sb._tagLandingOverride(), '');
  sb._tagLandingSave('attention');
  assert.equal(sb._tagLandingOverride(), 'attention');
  sb._tagLandingSave('list');
  assert.equal(sb._tagLandingOverride(), 'tags', 'List and Tags both mean the tag browser');
  sb.localStorage.setItem('cs_tag_landing', 'nonsense');
  assert.equal(sb._tagLandingOverride(), '');
  sb.localStorage.getItem = () => { throw new Error('blocked'); };
  sb.localStorage.setItem = () => { throw new Error('blocked'); };
  assert.equal(sb._tagLandingOverride(), '');
  assert.doesNotThrow(() => sb._tagLandingSave('attention'));
});

test('the first paint decides the landing for both tabs and applies the config and the override', () => {
  const sb = loadApp();
  run_(sb, `_tagBrowse.cfg.defaultView = 'auto';`);
  assert.equal(sb._tagLandingEnsure(2), 'attention');
  assert.equal(run_(sb, '_tagBrowse.automata.attn && _tagBrowse.automation.attn'), true);

  const forced = loadApp();
  run_(forced, `_tagBrowse.cfg.defaultView = 'tags';`);
  assert.equal(forced._tagLandingEnsure(9), 'tags');
  assert.equal(run_(forced, '_tagBrowse.automata.attn || _tagBrowse.automation.attn'), false);

  const remembered = loadApp();
  remembered.localStorage.setItem('cs_tag_landing', 'attention');
  run_(remembered, `_tagBrowse.cfg.defaultView = 'tags';`);
  assert.equal(remembered._tagLandingEnsure(0), 'attention', 'the override beats the forced default');
});

test('the landing never switches while the page is open', () => {
  const sb = loadApp();
  assert.equal(sb._tagLandingEnsure(1), 'attention');
  // The count falls to zero, then rises again, and the config changes: no switch.
  assert.equal(sb._tagLandingEnsure(0), 'attention');
  run_(sb, `_tagBrowse.cfg.defaultView = 'tags';`);
  assert.equal(sb._tagLandingEnsure(5), 'attention');
  setPinned(sb, { statuses: [], items: [] }, null);
  const tile = it => `<tile id="${it.id}"></tile>`;
  const html = sb._tagBrowseMountHTML('automata', [], 0, tile, '');
  assert.ok(html.includes('Nothing needs attention right now.'), 'a re-render with no attention items stays in the Attention view');

  const other = loadApp();
  assert.equal(other._tagLandingEnsure(0), 'tags');
  assert.equal(other._tagLandingEnsure(4), 'tags', 'a later attention item does not pull the page over');
});

test('choosing a view remembers it for the next page load', () => {
  const sb = loadApp();
  sb.renderAutomataCard = () => '';
  sb._automataRenderCards = () => {};
  sb._tagFacetsRefresh = () => {};
  sb.tagBrowseSetView('automata', 'attention');
  assert.equal(sb.localStorage.getItem('cs_tag_landing'), 'attention');
  assert.equal(run_(sb, '_tagBrowse.automata.attn'), true);
  sb.tagBrowseSetView('automata', 'tags');
  assert.equal(sb.localStorage.getItem('cs_tag_landing'), 'tags');
  assert.equal(run_(sb, '_tagBrowse.automata.attn'), false);
  assert.equal(run_(sb, '_tagBrowse.automata.view'), 'tags');
});

// ── Live refresh ─────────────────────────────────────────────────────────────

function liveApp() {
  const sb = loadApp();
  const calls = { list: 0, automation: 0, tags: 0 };
  sb._automationTabActive = () => false;
  sb.loadAutomataTagRegistryPanel = () => {};
  sb.loadAutomataPanel = () => { calls.list++; return Promise.resolve(); };
  sb.refreshAutomationView = () => { calls.automation++; return Promise.resolve(); };
  run_(sb, `state.activeView = 'autonomous';`);
  sb.__calls = calls;
  return sb;
}

test('a burst of tag_update and prd_update frames waits one window and refetches once', async () => {
  const sb = liveApp();
  const frames = ['tag_update', 'prd_update', 'prd_update', 'tag_update', 'prd_update', 'tag_update'];
  frames.forEach(type => sb.handleMessage({ type, data: { id: 'x' } }));
  const live = pending(sb, 250);
  assert.equal(live.length, 1, 'six frames, one pending timer');
  assert.equal(sb.__calls.list, 0, 'nothing refetches before the window ends');
  await fireNext(sb);
  assert.equal(sb.__calls.list, 1, 'one refetch for the whole burst');
  assert.equal(pending(sb).length, 0);
});

test('a frame after the window starts a new refetch', async () => {
  const sb = liveApp();
  sb.handleMessage({ type: 'prd_update', data: {} });
  await fireNext(sb);
  sb.handleMessage({ type: 'tag_update', data: {} });
  await fireNext(sb);
  assert.equal(sb.__calls.list, 2);
});

test('the Automation tab refreshes through its own view', async () => {
  const sb = liveApp();
  sb._automationTabActive = () => true;
  sb.handleMessage({ type: 'prd_update', data: {} });
  await fireNext(sb);
  assert.equal(sb.__calls.automation, 1);
  assert.equal(sb.__calls.list, 0);
});

test('tag_update does nothing until the registry has loaded; prd_update off the Automata views is ignored', () => {
  const sb = liveApp();
  run_(sb, '_tagsState.loaded = false;');
  sb.handleMessage({ type: 'tag_update', data: {} });
  assert.equal(pending(sb).length, 0);
  run_(sb, `state.activeView = 'sessions';`);
  sb.handleMessage({ type: 'prd_update', data: {} });
  assert.equal(pending(sb).length, 0);
});

test('with an Automaton detail open, prd_update keeps its own detail refresh and does not refetch the list', async () => {
  const sb = liveApp();
  const urls = [];
  sb.apiFetch = u => { urls.push(u); return Promise.resolve({ id: 'p1' }); };
  sb._liveUpdateDetail = () => {};
  run_(sb, `_automataDetailId = 'p1';`);
  sb.handleMessage({ type: 'prd_update', data: {} });
  await fireNext(sb);
  assert.deepEqual(urls, ['/api/autonomous/prds/p1']);
  assert.equal(sb.__calls.list, 0);
});

// A server whose answers a test can change between fetches.
function serverStub(world) {
  const urls = [];
  const fn = u => {
    urls.push(u);
    if (u === '/api/config') return Promise.resolve({ autonomous: { tags: { default_view: 'auto' } } });
    if (u.startsWith('/api/autonomous/tags')) return Promise.resolve({ tags: world.tags, untagged: { count: 0 } });
    if (u === '/api/autonomous/attention') return Promise.resolve({ statuses: world.statuses, items: world.attention });
    if (u.startsWith('/api/autonomous/prds?status=running')) return Promise.resolve({ prds: world.running, total: world.running.length });
    if (u.startsWith('/api/autonomous/prds')) return Promise.resolve({ prds: world.prds, total: world.prds.length });
    if (u === '/api/llms') return Promise.resolve({ llms: [] });
    if (u === '/api/capacity') return Promise.resolve(null);
    return Promise.resolve(null);
  };
  return { fn, urls };
}

function panelApp(world) {
  const el = makeStubElement();
  const sb = loadApp({ byId: { automataPanel: el } });
  const srv = serverStub(world);
  sb.apiFetch = srv.fn;
  sb.renderAutomataCard = p => `<tile id="${p.id}"></tile>`;
  sb._automataRenderBatchBar = () => {};
  sb._tagFacetsRefresh = () => {};
  // Only the Automata panel is on screen: the settings registry panel is not.
  const byId = sb.document.getElementById;
  sb.document.getElementById = id => (id === 'automataTagRegistryPanel' ? null : byId(id));
  run_(sb, `_tagsState.loaded = false; state.activeView = 'autonomous'; _automataState.tab = 'automata';`);
  sb.__el = el;
  sb.__srv = srv;
  return sb;
}

test('the refetch reads the tag counts, the attention group, the running strip and the list together', async () => {
  const world = { tags: TAGS, statuses: ['blocked'], attention: [prd('a', 'blocked')], running: [prd('r', 'running')], prds: [prd('a', 'blocked'), prd('r', 'running')] };
  const sb = panelApp(world);
  await sb.loadAutomataPanel();
  const u = sb.__srv.urls;
  assert.equal(u.filter(x => x === '/api/autonomous/tags?counts=true').length, 1);
  assert.equal(u.filter(x => x === '/api/autonomous/attention').length, 1);
  assert.equal(u.filter(x => x.startsWith('/api/autonomous/prds?status=running')).length, 1);
  assert.equal(u.filter(x => x.startsWith('/api/autonomous/prds?limit=')).length, 1);
  assert.ok(sb.__el.innerHTML.includes('Attention (1)'));
  assert.ok(sb.__el.innerHTML.includes('tag-pinned-attention'));
});

test('a burst of frames through the real loader is one set of requests, and the counts move', async () => {
  const world = { tags: TAGS, statuses: ['blocked'], attention: [], running: [], prds: [prd('a', 'running')] };
  const sb = panelApp(world);
  await sb.loadAutomataPanel();
  run_(sb, `_tagsState.loaded = true;`);
  sb.__srv.urls.length = 0;
  sb.__timers.queue.length = 0;
  world.attention = [prd('a', 'blocked', { blocked_reason: 'needs a key' })];
  world.prds = [prd('a', 'blocked')];
  for (let i = 0; i < 8; i++) sb.handleMessage({ type: i % 2 ? 'tag_update' : 'prd_update', data: {} });
  await fireNext(sb);
  const tagReads = sb.__srv.urls.filter(x => x === '/api/autonomous/tags?counts=true');
  assert.equal(tagReads.length, 1, 'eight frames, one tag-count read');
  assert.equal(sb.__srv.urls.filter(x => x.startsWith('/api/autonomous/prds?limit=')).length, 1, 'and one list read');
  assert.ok(sb.__el.innerHTML.includes('needs a key'), 'the group shows the new blocked reason');
  assert.ok(sb.__el.innerHTML.includes('Attention (1)'));
});

test('a rename or re-parent updates every shown count and chip', async () => {
  const world = { tags: TAGS, statuses: [], attention: [], running: [], prds: [prd('a', 'running', { tags: ['w'] })] };
  const sb = panelApp(world);
  sb.innerWidth = 1200;
  run_(sb, `_tagBrowse.cfg.defaultView = 'tags'; _tagBrowse.automata.view = 'tags';`);
  await sb.loadAutomataPanel();
  assert.match(sb.__el.innerHTML, /tag-section-name">Work<\/span><span class="tag-section-count"[^>]*>3</);
  run_(sb, `_tagsState.loaded = true;`);
  world.tags = [
    { id: 'w', name: 'Job', subtree_count: 5, direct_count: 2 },
    { id: 'h', name: 'Home', parent_id: 'w', subtree_count: 1, direct_count: 1 },
  ];
  sb.handleMessage({ type: 'tag_update', data: { id: 'w' } });
  await fireNext(sb);
  assert.match(sb.__el.innerHTML, /tag-section-name">Job<\/span><span class="tag-section-count"[^>]*>5</);
  assert.ok(!sb.__el.innerHTML.includes('>Work<'));
});

test('with the stream down the list poll catches the screen up without any frame', async () => {
  const world = { tags: TAGS, statuses: ['blocked'], attention: [], running: [], prds: [prd('a', 'running')] };
  const sb = panelApp(world);
  run_(sb, `state.connected = false;`);
  await sb.loadAutomataPanel();
  assert.ok(sb.__el.innerHTML.includes('Attention (0)'));
  const poll = pending(sb, 30000);
  assert.equal(poll.length, 1, 'the loader scheduled its poll');
  world.attention = [prd('a', 'blocked', { blocked_reason: 'stuck' })];
  world.prds = [prd('a', 'blocked')];
  sb.__srv.urls.length = 0;
  await fireNext(sb);
  assert.ok(sb.__srv.urls.includes('/api/autonomous/tags?counts=true'));
  assert.ok(sb.__srv.urls.includes('/api/autonomous/attention'));
  assert.ok(sb.__el.innerHTML.includes('Attention (1)'));
  assert.ok(sb.__el.innerHTML.includes('stuck'));
  assert.equal(pending(sb, 30000).length, 1, 'the poll re-arms itself once');
});

// ── No timer stacking ────────────────────────────────────────────────────────

test('repeated renders and frames leave one pending live timer and start no interval', async () => {
  const world = { tags: TAGS, statuses: ['blocked'], attention: [prd('a', 'blocked')], running: [prd('r', 'running')], prds: [prd('a', 'blocked'), prd('r', 'running')] };
  const sb = panelApp(world);
  await sb.loadAutomataPanel();
  const loaderIntervals = sb.__timers.intervals;
  run_(sb, `_tagsState.loaded = true;`);
  for (let i = 0; i < 6; i++) {
    sb._tagBrowseMountHTML('automata', world.prds, 2, p => `<tile id="${p.id}"></tile>`, '');
    sb.handleMessage({ type: 'tag_update', data: {} });
    sb.handleMessage({ type: 'prd_update', data: {} });
  }
  assert.equal(pending(sb, 250).length, 1, 'six renders and twelve frames, one live timer');
  assert.ok(sb.__timers.cleared >= 11, 'each frame cleared the timer before it set a new one');
  assert.equal(sb.__timers.intervals, loaderIntervals, 'no interval was started by the tag code');
  assert.equal(sb.window._tagLiveTimer, pending(sb, 250)[0].id, 'the timer id is kept on window');
});

test('the pinned and live code starts no interval of its own', () => {
  const start = SRC.indexOf('// ── Pinned groups: Needs attention and Running');
  const end = SRC.indexOf('// ── Drawing ──');
  assert.ok(start > 0 && end > start);
  assert.ok(!/setInterval/.test(SRC.slice(start, end)));
  assert.ok(/clearTimeout\(window\._tagLiveTimer\);\s*window\._tagLiveTimer = setTimeout/.test(SRC.slice(start, end)));
});

test('both frames go through the one debounce in the message handler', () => {
  const i = SRC.indexOf("case 'prd_update':");
  const j = SRC.indexOf("case 'hook_update':");
  const block = SRC.slice(i, j);
  assert.equal((block.match(/_tagLiveSchedule\(\)/g) || []).length, 2);
  assert.ok(!block.includes('_tagUpdateTimer'), 'the old per-frame timer is gone');
});

// ── Strings ──────────────────────────────────────────────────────────────────

test('the new strings are in all five bundles and carry no tracker ID', () => {
  const keys = ['tag_view_attention', 'tag_attention_title', 'tag_attention_review_held', 'tag_attention_none', 'tag_running_title', 'tag_running_more'];
  for (const l of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', l + '.json'), 'utf8'));
    for (const k of keys) {
      assert.ok(typeof b[k] === 'string' && b[k], `${l} has ${k}`);
      assert.doesNotMatch(b[k], /\b(BL|GH|SEC)-?\d+/);
      assert.doesNotMatch(b[k], /\bPRD\b/);
    }
  }
});
