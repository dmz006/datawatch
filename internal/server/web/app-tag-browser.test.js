// Shared tag browser for the Automata and Automation lists. Covers ANY and ALL
// with the hierarchy, subtree counts (an item under two children counted once),
// the "also in:" line, primary-tag ordering, faceted counts, the empty-state
// text, the drill-down hash and breadcrumb, the default match mode from config,
// the sections, the layout breakpoints, and that both tabs mount one function.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const vm = require('vm');
const { buildSandbox, loadScript } = require('./testutil_browser_stub');

const WEB = __dirname;

// The registry: Work > Mobile > iOS > Beta, Work > Shared, and Home on its own.
const TAGS = [
  { id: 'w', name: 'Work' },
  { id: 'm', name: 'Mobile', parent_id: 'w' },
  { id: 'i', name: 'iOS', parent_id: 'm' },
  { id: 'x', name: 'Beta', parent_id: 'i' },
  { id: 's', name: 'Shared', parent_id: 'w' },
  { id: 'h', name: 'Home' },
];

// Six Automata. B sits under two children of Work (Mobile and Shared).
const ITEMS = [
  { id: 'A', tags: ['i'], backend: 'claude', status: 'running' },
  { id: 'B', tags: ['m', 's'], backend: 'pi', status: 'paused' },
  { id: 'C', tags: ['h'], backend: 'pi', status: 'running' },
  { id: 'D', tags: [], backend: 'pi', status: 'draft' },
  { id: 'E', tags: ['s'], backend: 'claude', status: 'running' },
  { id: 'F', tags: ['i', 'h'], backend: 'claude', status: 'completed', primary_tag_id: 'h' },
];

const LABELS = { root: 'Tags', untagged: 'Untagged' };

function loadApp(opts) {
  const sandbox = buildSandbox(opts || {});
  vm.createContext(sandbox);
  loadScript(path.join(WEB, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  vm.runInContext(`_tagsState.list = ${JSON.stringify(TAGS)}; _tagsState.byId = _tagsById(_tagsState.list); _tagsState.loaded = true;`, sandbox);
  return sandbox;
}

function ids(list) {
  return plain(list).map(x => x.id).sort();
}

// A tile renderer that records what it was asked to draw.
function recorder() {
  const seen = [];
  const fn = (item, also) => { seen.push({ id: item.id, also }); return `<tile id="${item.id}">${also}</tile>`; };
  return { fn, seen };
}

// ── ANY and ALL with the hierarchy ───────────────────────────────────────────

test('ANY matches an item carrying any selected tag or one of its descendants', () => {
  const sb = loadApp();
  const tree = sb._tagBrowseTree(TAGS);
  const got = sb._tagBrowseFilter(ITEMS, { tagIds: ['m', 'h'], mode: 'any' }, tree);
  assert.deepEqual(ids(got), ['A', 'B', 'C', 'F']);
});

test('ALL needs every selected tag, each matched through its subtree', () => {
  const sb = loadApp();
  const tree = sb._tagBrowseTree(TAGS);
  assert.deepEqual(ids(sb._tagBrowseFilter(ITEMS, { tagIds: ['m', 'h'], mode: 'all' }, tree)), ['F']);
  assert.deepEqual(ids(sb._tagBrowseFilter(ITEMS, { tagIds: ['w', 's'], mode: 'all' }, tree)), ['B', 'E']);
});

test('a selected tag matches itself and every descendant in both modes', () => {
  const sb = loadApp();
  const tree = sb._tagBrowseTree(TAGS);
  assert.deepEqual(ids(sb._tagBrowseFilter(ITEMS, { tagIds: ['i'], mode: 'any' }, tree)), ['A', 'F']);
  assert.deepEqual(ids(sb._tagBrowseFilter(ITEMS, { tagIds: ['i'], mode: 'all' }, tree)), ['A', 'F']);
  assert.deepEqual(ids(sb._tagBrowseFilter(ITEMS, { tagIds: ['w'], mode: 'any' }, tree)), ['A', 'B', 'E', 'F']);
});

test('facets are OR within one and AND across: backend and status narrow the tags', () => {
  const sb = loadApp();
  const tree = sb._tagBrowseTree(TAGS);
  const pi = sb._tagBrowseFilter(ITEMS, { backends: ['pi'] }, tree);
  assert.deepEqual(ids(pi), ['B', 'C', 'D']);
  assert.deepEqual(ids(sb._tagBrowseFilter(ITEMS, { backends: ['pi'], statuses: ['running', 'paused'] }, tree)), ['B', 'C']);
  assert.deepEqual(ids(sb._tagBrowseFilter(ITEMS, { backends: ['pi', 'claude'], statuses: ['running'] }, tree)), ['A', 'C', 'E']);
  assert.deepEqual(ids(sb._tagBrowseFilter(ITEMS, { tagIds: ['w'], backends: ['pi'] }, tree)), ['B']);
});

test('no tag selection matches every item, and an unknown tag is ignored', () => {
  const sb = loadApp();
  const tree = sb._tagBrowseTree(TAGS);
  assert.equal(sb._tagBrowseFilter(ITEMS, { tagIds: [] }, tree).length, ITEMS.length);
  assert.equal(sb._tagBrowseFilter(ITEMS, { tagIds: ['gone'] }, tree).length, ITEMS.length);
});

// ── Subtree counts ───────────────────────────────────────────────────────────

test('a subtree count is the distinct items under the tag, an item under two children once', () => {
  const sb = loadApp();
  const tree = sb._tagBrowseTree(TAGS);
  const counts = sb._tagBrowseCounts(ITEMS, tree);
  assert.equal(counts.w, 4, 'B sits under Mobile and Shared but counts once in Work');
  assert.equal(counts.m, 3);
  assert.equal(counts.i, 2);
  assert.equal(counts.s, 2);
  assert.equal(counts._untagged, 1);
});

test('the sections place each item once per branch and their counts are the subtree counts', () => {
  const sb = loadApp();
  const tree = sb._tagBrowseTree(TAGS);
  const sec = sb._tagBrowseSections(ITEMS, tree);
  const work = sec.tags.find(n => n.id === 'w');
  assert.equal(work.count, 4);
  assert.deepEqual(plain(work.items), [], 'nothing sits at Work itself: every item is in a child branch');
  const mobile = work.children.find(n => n.id === 'm');
  assert.deepEqual(ids(mobile.items), ['B'], 'Mobile holds B directly; A and F sit under iOS');
  const ios = mobile.children.find(n => n.id === 'i');
  assert.deepEqual(ids(ios.items), ['A', 'F']);
  const shared = work.children.find(n => n.id === 's');
  assert.deepEqual(ids(shared.items), ['B', 'E']);
  assert.deepEqual(ids(sec.untagged.items), ['D']);
  const home = sec.tags.find(n => n.id === 'h');
  assert.equal(home.direct, 2);
});

test('the untagged section holds only the items with no tag', () => {
  const sb = loadApp();
  const tree = sb._tagBrowseTree(TAGS);
  assert.deepEqual(ids(sb._tagBrowseSections(ITEMS, tree).untagged.items), ['D']);
  assert.deepEqual(ids(sb._tagBrowseDrillItems(ITEMS, ['_untagged'], tree)), ['D']);
});

// ── "also in:" ───────────────────────────────────────────────────────────────

test('also in names the other tags, leaving out the section and its ancestors', () => {
  const sb = loadApp();
  const tree = sb._tagBrowseTree(TAGS);
  assert.deepEqual(sb._tagBrowseAlsoIn(ITEMS[5], 'm', tree), ['i', 'h'], 'F in Mobile: iOS and Home');
  assert.deepEqual(sb._tagBrowseAlsoIn(ITEMS[0], 'i', tree), [], 'A in iOS: nothing else');
  assert.deepEqual(sb._tagBrowseAlsoIn(ITEMS[1], 'm', tree), ['s'], 'B in Mobile: Shared');
});

test('the also-in line names each other tag and each name taps through to its tag', () => {
  const sb = loadApp();
  const tree = sb._tagBrowseTree(TAGS);
  const html = sb._tagAlsoInHTML('automata', ITEMS[5], 'm', tree);
  assert.ok(html.includes('also in:'), 'the label is there');
  assert.ok(html.includes('>Home<'), 'Home is named');
  assert.ok(html.includes(">iOS<"), 'iOS is named');
  assert.ok(!html.includes('>Mobile<'), 'the section tag is not repeated');
  assert.ok(html.includes("tagBrowseGo('automata','h')"), 'a tap opens the tag');
  assert.equal(sb._tagAlsoInHTML('automata', ITEMS[0], 'i', tree), '', 'no line when there is nothing else');
});

// ── Primary tag ──────────────────────────────────────────────────────────────

test('the primary tag sorts items first inside a section without hiding any', () => {
  const sb = loadApp();
  const tree = sb._tagBrowseTree(TAGS);
  const items = [
    { id: 'p', tags: ['m'] },
    { id: 'q', tags: ['m'], primary_tag_id: 'm' },
    { id: 'r', tags: ['i'], primary_tag_id: 'i' },
    { id: 'u', tags: ['m'], primary_tag_id: 'h' },
  ];
  const ordered = sb._tagBrowseOrderForTag(items, 'm', tree).map(x => x.id);
  assert.deepEqual(ordered, ['q', 'r', 'p', 'u'], 'primary in the subtree first, the rest keep their order');
  assert.equal(ordered.length, items.length, 'nothing is dropped');
});

test('the primary tag select lists only the ticked tags', () => {
  const sb = loadApp();
  const html = sb._tagPrimaryOptions('m', ['m', 'h']);
  assert.ok(html.includes('value="m" selected'));
  assert.ok(html.includes('value="h"'));
  assert.ok(!html.includes('value="i"'));
  assert.ok(html.includes('value=""'), 'a no-primary choice is there');
});

// ── Faceted counts ───────────────────────────────────────────────────────────

test('facet counts come from the loaded list with the other facets applied', () => {
  const sb = loadApp();
  const tree = sb._tagBrowseTree(TAGS);
  // Backend chips: counted over the items the tag selection leaves in.
  const underWork = sb._tagBrowseFilter(ITEMS, { tagIds: ['w'], mode: 'any' }, tree);
  assert.deepEqual(plain(sb._tagBrowseValueCounts(underWork, 'backend')), { claude: 3, pi: 1 });
  // Status chips: counted over the items the backend leaves in.
  const pi = sb._tagBrowseFilter(ITEMS, { backends: ['pi'] }, tree);
  assert.deepEqual(plain(sb._tagBrowseValueCounts(pi, 'status')), { paused: 1, running: 1, draft: 1 });
  // A missing status counts as draft.
  assert.equal(sb._tagBrowseValueCounts([{}], 'status').draft, 1);
});

// ── Empty state ──────────────────────────────────────────────────────────────

test('an empty result reads No matches with the filter count and Showing X of Y', () => {
  const sb = loadApp();
  vm.runInContext(`_tagBrowse.automata.sel = ['h', 's'];`, sb);
  const html = sb._tagNoMatchesHTML('automata', 12);
  assert.ok(html.includes('No matches. Clear 2 filters'));
  assert.ok(html.includes('Showing 0 of 12'));
  assert.ok(html.includes("tagBrowseClear('automata')"));
  vm.runInContext(`_tagBrowse.automata.sel = [];`, sb);
  const none = sb._tagNoMatchesHTML('automata', 12);
  assert.ok(none.includes('No matches.') && !none.includes('Clear 0'), 'no filter count with no filter');
});

test('an active filter with no hits draws the empty result instead of a grid', () => {
  const sb = loadApp();
  vm.runInContext(`_tagBrowse.automata.sel = ['h']; _tagBrowse.automata.view = 'tags';`, sb);
  const rec = recorder();
  const html = sb._tagBrowseMountHTML('automata', [], 6, rec.fn, '<empty/>');
  assert.ok(html.includes('No matches. Clear 1 filters'));
  assert.ok(html.includes('Showing 0 of 6'));
  assert.equal(rec.seen.length, 0);
});

// ── Hash and breadcrumb ──────────────────────────────────────────────────────

test('a drill-down hash builds and parses both sub-tabs', () => {
  const sb = loadApp();
  assert.equal(sb._tagHashBuild('automata', ['w', 'm']), '#automata/tags/w/m');
  assert.equal(sb._tagHashBuild('automata', []), '#automata/tags');
  assert.equal(sb._tagHashBuild('automation', ['w']), '#automata/automations/tags/w');
  assert.deepEqual(plain(sb._tagHashParse('#automata/tags/w/m')), { scope: 'automata', path: ['w', 'm'] });
  assert.deepEqual(plain(sb._tagHashParse('#automata/tags')), { scope: 'automata', path: [] });
  assert.deepEqual(plain(sb._tagHashParse('#automata/automations/tags/w')), { scope: 'automation', path: ['w'] });
  assert.equal(sb._tagHashParse('#automation/abc123'), null, 'the Automation route is not a tag path');
  assert.equal(sb._tagHashParse('#sessions'), null);
});

test('a tag id in a hash survives encoding both ways', () => {
  const sb = loadApp();
  const built = sb._tagHashBuild('automata', ['a b']);
  assert.equal(built, '#automata/tags/a%20b');
  assert.deepEqual(plain(sb._tagHashParse(built)).path, ['a b']);
});

test('a drill-down path is kept only while it is a chain of known tags', () => {
  const sb = loadApp();
  const tree = sb._tagBrowseTree(TAGS);
  assert.deepEqual(plain(sb._tagBrowseValidPath(['w', 'm', 'i'], tree)), ['w', 'm', 'i']);
  assert.deepEqual(plain(sb._tagBrowseValidPath(['w', 'm', 'x'], tree)), ['w', 'm'], 'Beta is not a child of Mobile');
  assert.deepEqual(plain(sb._tagBrowseValidPath(['zzz'], tree)), []);
  assert.deepEqual(plain(sb._tagBrowseValidPath(['_untagged'], tree)), ['_untagged']);
  assert.deepEqual(plain(sb._tagBrowseValidPath(['w', '_untagged'], tree)), ['w'], 'the untagged token only stands alone');
});

test('the breadcrumb reads Tags then the path, and collapses the middle beyond three levels', () => {
  const sb = loadApp();
  const tree = sb._tagBrowseTree(TAGS);
  const short = plain(sb._tagBrowseCrumbs(['w', 'm', 'i'], tree, LABELS)).map(c => c.label);
  assert.deepEqual(short, ['Tags', 'Work', 'Mobile', 'iOS']);
  const long = plain(sb._tagBrowseCrumbs(['w', 'm', 'i', 'x'], tree, LABELS));
  assert.deepEqual(long.map(c => c.label || '…'), ['Tags', 'Work', '…', 'iOS', 'Beta']);
  assert.equal(long[3].depth, 3, 'a crumb returns to its depth');
});

// ── Default mode and settings ────────────────────────────────────────────────

test('the initial match mode comes from autonomous.tags.default_match_mode, default any', () => {
  const sb = loadApp();
  assert.equal(sb._tagBrowseDefaultMode({ default_match_mode: 'all' }), 'all');
  assert.equal(sb._tagBrowseDefaultMode({ default_match_mode: 'any' }), 'any');
  assert.equal(sb._tagBrowseDefaultMode({}), 'any');
  assert.equal(sb._tagBrowseDefaultMode(undefined), 'any');
  assert.equal(sb._tagBrowseModeOf('', 'all'), 'all', 'no stored choice: the config default');
  assert.equal(sb._tagBrowseModeOf('any', 'all'), 'any', 'a stored choice wins');
});

test('the config load reads the default mode and the direct-count switch', async () => {
  const sb = loadApp();
  sb.apiFetch = () => Promise.resolve({ autonomous: { tags: { default_match_mode: 'all', direct_counts_visible: true } } });
  const cfg = await vm.runInContext('_tagBrowseConfigLoad()', sb);
  assert.equal(cfg.defaultMode, 'all');
  assert.equal(cfg.directCounts, true);
  assert.equal(vm.runInContext('_tagBrowseMode("automata")', sb), 'all', 'the browser starts in ALL');
});

test('the direct count is a tooltip only when the setting is on', () => {
  const sb = loadApp();
  const tree = sb._tagBrowseTree(TAGS);
  const sec = sb._tagBrowseSections(ITEMS, tree);
  vm.runInContext('_tagBrowse.cfg.directCounts = false;', sb);
  const off = sb._tagSectionHTML({ scope: 'automata', tree, renderTile: () => '', stored: {}, direct: false }, sec.tags.find(n => n.id === 'h'), 0, true);
  assert.ok(!off.includes('directly tagged'));
  const on = sb._tagSectionHTML({ scope: 'automata', tree, renderTile: () => '', stored: {}, direct: true }, sec.tags.find(n => n.id === 'h'), 0, true);
  assert.ok(on.includes('title="2 directly tagged"') || on.includes('directly tagged'));
});

test('top-level sections start collapsed only above eight top-level tags', () => {
  const sb = loadApp();
  assert.equal(sb._tagBrowseRootOpen(8), true);
  assert.equal(sb._tagBrowseRootOpen(9), false);
});

test('the layout is the drill-down under 720px and the sections from 720px up', () => {
  const sb = loadApp();
  assert.equal(sb._tagBrowseLayout(400), 'drill');
  assert.equal(sb._tagBrowseLayout(719), 'drill');
  assert.equal(sb._tagBrowseLayout(720), 'sections');
  assert.equal(sb._tagBrowseLayout(1280), 'sections');
  assert.equal(sb._tagBrowseLayout(0), 'sections', 'an unknown width reads as wide');
});

test('a section open state persists per viewer and survives a broken store', () => {
  const sb = loadApp();
  sb._tagSectionSave('automata:w', false);
  assert.equal(sb._tagSectionsStored()['automata:w'], false);
  sb.localStorage.setItem('cs_tag_sections', '{not json');
  assert.deepEqual(plain(sb._tagSectionsStored()), {}, 'a bad value reads as empty');
});

// ── Drawing ──────────────────────────────────────────────────────────────────

test('on a phone the Tags view draws the drill-down with Untagged last at the top', () => {
  const sb = loadApp({ initialHash: '#automata/tags' });
  sb.innerWidth = 400;
  vm.runInContext(`_tagBrowse.automata.view = 'tags';`, sb);
  const rec = recorder();
  const html = sb._tagBrowseMountHTML('automata', ITEMS, ITEMS.length, rec.fn, '<empty/>');
  assert.ok(html.includes('tag-drill-rows'));
  assert.ok(!html.includes('tag-sections'), 'no sections on a phone');
  assert.ok(html.indexOf('Work') < html.indexOf('Untagged'), 'Untagged comes last');
  assert.ok(html.indexOf('Home') < html.indexOf('Untagged'));
});

test('under a tag the drill-down lists its subtree once, with a back row', () => {
  const sb = loadApp({ initialHash: '#automata/tags/w/m' });
  sb.innerWidth = 400;
  vm.runInContext(`_tagBrowse.automata.view = 'tags';`, sb);
  const rec = recorder();
  const html = sb._tagBrowseMountHTML('automata', ITEMS, ITEMS.length, rec.fn, '<empty/>');
  assert.ok(html.includes('tag-drill-back'), 'back row');
  assert.deepEqual(rec.seen.map(x => x.id).sort(), ['A', 'B', 'F'], 'Mobile subtree, each item once');
  assert.ok(html.includes('Mobile') && html.includes('iOS'), 'the sub-tag row is there');
});

test('on web and tablet the Tags view draws sections, with Untagged last', () => {
  const sb = loadApp({ initialHash: '' });
  sb.innerWidth = 1280;
  vm.runInContext(`_tagBrowse.automata.view = 'tags';`, sb);
  const rec = recorder();
  const html = sb._tagBrowseMountHTML('automata', ITEMS, ITEMS.length, rec.fn, '<empty/>');
  assert.ok(html.includes('tag-sections') && html.includes('tag-section'));
  assert.ok(!html.includes('tag-drill-back'));
  assert.ok(html.lastIndexOf('data-tag-section="automata:_untagged"') > html.indexOf('data-tag-section="automata:h"'));
  assert.ok(html.includes('tag-view-switch'), 'the List and Tags switch is drawn');
});

test('an active filter gives way to one flat tile grid even in the Tags view', () => {
  const sb = loadApp({ initialHash: '' });
  sb.innerWidth = 1280;
  vm.runInContext(`_tagBrowse.automata.view = 'tags'; _tagBrowse.automata.sel = ['w'];`, sb);
  const rec = recorder();
  const items = sb._tagBrowseFilter(ITEMS, { tagIds: ['w'], mode: 'any' }, sb._tagBrowseTree(TAGS));
  const html = sb._tagBrowseMountHTML('automata', items, ITEMS.length, rec.fn, '<empty/>');
  assert.ok(!html.includes('tag-sections'));
  assert.ok(html.includes('Showing 4 of 6'));
  assert.deepEqual(rec.seen.map(x => x.id).sort(), ['A', 'B', 'E', 'F']);
});

test('the filter count on the phone button tracks the active facets', () => {
  const sb = loadApp();
  vm.runInContext(`_tagBrowse.automata.sel = ['w']; _tagBrowse.automata.backends.add('pi');`, sb);
  assert.ok(sb._tagFilterPhoneHTML('automata').includes('Filters (2)'));
});

test('the tag dropdown carries the ANY and ALL toggle and a count on each option', () => {
  const sb = loadApp();
  vm.runInContext(`_automataState.allPrds = ${JSON.stringify(ITEMS)};`, sb);
  const html = sb._tagFacetsHTML('automata', false);
  assert.ok(html.includes('match ANY'));
  assert.ok(html.includes("tagBrowseToggleMode('automata')"));
  vm.runInContext(`_tagBrowse.automata.mode = 'all';`, sb);
  assert.ok(sb._tagFacetsHTML('automata', false).includes('match ALL'));
  assert.ok(html.includes('tagBrowseToggleTag(\'automata\',\'w\',this.checked)'));
});

// ── Both tabs mount the same function ────────────────────────────────────────

test('both tabs draw their list through the one shared mount function', () => {
  const src = fs.readFileSync(path.join(WEB, 'app.js'), 'utf8');
  const cards = src.slice(src.indexOf('function _automataCardsHtml()'), src.indexOf('function _automataAfterPaint'));
  const tiles = src.slice(src.indexOf('function _automationTilesHTML('), src.indexOf('// _automationActiveMembers'));
  assert.ok(cards.includes("_tagBrowseMountHTML('automata'"), 'Automata mounts it');
  assert.ok(tiles.includes("_tagBrowseMountHTML('automation'"), 'Automation mounts it');
  assert.ok(!cards.includes('_tileGridHTML(filtered'), 'no private flat grid is left');
});

test('the two sub-tabs draw the same switch and the same empty-state path', () => {
  const sb = loadApp();
  const rec = recorder();
  const a = sb._tagBrowseMountHTML('automata', [], 3, rec.fn, '<empty/>');
  const b = sb._tagBrowseMountHTML('automation', [], 3, rec.fn, '<empty/>');
  assert.ok(a.includes('tag-view-switch') && b.includes('tag-view-switch'));
  assert.ok(a.includes('No matches.') && b.includes('No matches.'));
  assert.equal(sb._tagBrowseMountHTML('automata', [], 0, rec.fn, '<empty/>'), '<empty/>', 'an empty tab shows its message');
});

function plain(v) {
  return JSON.parse(JSON.stringify(v));
}
