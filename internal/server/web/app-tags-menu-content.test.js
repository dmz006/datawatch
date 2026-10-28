// Header Tags menu content — the watched switch, the tag list, the match mode and Clear, and the open-session actions.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function loadApp() {
  const sandbox = buildSandbox({});
  sandbox.URLSearchParams = URLSearchParams;
  sandbox.fetch = () => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ tags: [] }), text: () => Promise.resolve('{"tags":[]}') });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return { sb: sandbox };
}

const run = (h, code) => vm.runInContext(code, h.sb);

function setTags(h, tags) {
  h.sb.__tags = tags;
  run(h, '_tagsState.list = __tags; _tagsState.byId = _tagsById(__tags); _tagsState.loaded = true;');
}

function setup(h, { tags = [], sel = [], mode = '', watchFilter = false, view = 'sessions', active = '', watched = [] } = {}) {
  setTags(h, tags);
  h.sb.__cfg = { sel, mode, watchFilter, view, active, watched };
  run(h, `
    _tagBrowse.session.sel = __cfg.sel.slice();
    _tagBrowse.session.mode = __cfg.mode;
    state.sessionWatchFilter = __cfg.watchFilter;
    state.activeView = __cfg.view;
    state.activeSession = __cfg.active;
    state.watchedSessions = new Set(__cfg.watched);
  `);
  return run(h, '_tagsMenuHTML()');
}

// manyTags builds n tags; tags ending in 1 or 6 are children of the tag before, those ending in 2 are grandchildren.
function manyTags(n) {
  const tags = [];
  for (let i = 0; i < n; i++) {
    const id = 'tag' + String(i).padStart(4, '0');
    let parent = '';
    if (i % 10 === 2) parent = tags[i - 1].id;
    else if (i % 5 === 1) parent = tags[i - 1].id;
    tags.push({ id, name: 'Tag ' + String(i).padStart(2, '0'), parent_id: parent });
  }
  return tags;
}

const sections = html => (html.match(/class="tags-menu-head"/g) || []).length;
const checkboxes = html => [...html.matchAll(/<input type="checkbox"([^>]*)>/g)].map(m => m[1]);

test('with no tags the menu shows the empty line, no list and no Clear', () => {
  const h = loadApp();
  const html = setup(h);
  assert.ok(html.includes(h.sb.t('tags_menu_no_tags')));
  assert.equal(html.includes('tags-menu-list'), false);
  assert.equal(html.includes('tagBrowseClear'), false);
  assert.ok(html.includes("tagBrowseToggleMode('session')"));
  assert.ok(html.includes(h.sb.t('tag_browse_match_any')));
});

test('the watched row is first and its checked state follows the watch filter', () => {
  const h = loadApp();
  const off = setup(h, { watchFilter: false });
  const on = setup(h, { watchFilter: true });
  const [offBox] = checkboxes(off);
  const [onBox] = checkboxes(on);
  assert.ok(offBox.includes('onchange="toggleSessionWatchFilter()"'));
  assert.equal(/\bchecked\b/.test(offBox), false);
  assert.ok(onBox.includes('onchange="toggleSessionWatchFilter()"'));
  assert.equal(/\bchecked\b/.test(onBox), true);
  assert.ok(off.indexOf(h.sb.t('tags_menu_watched_only')) < off.indexOf(h.sb.t('tags_menu_button')));
});

test('thirty tags, some nested, list in tree order inside a scrolling container', () => {
  const h = loadApp();
  const tags = manyTags(30);
  const sel = [tags[3].id, tags[12].id];
  const html = setup(h, { tags, sel });
  const boxes = checkboxes(html).slice(1);
  assert.equal(boxes.length, 30);
  // The list lives in the scrolling container, and so do all the tag rows.
  const list = html.slice(html.indexOf('class="tags-menu-list"'));
  assert.equal((list.match(/class="tag-multi-row"/g) || []).length, 30);
  // Same order as the tree walk, with indentation by depth.
  const want = Array.from(h.sb._tagTreeOrder(tags)); // copy out of the vm realm for deepEqual
  const ids = [...html.matchAll(/class="tag-multi-row" style="padding-left:(\d+)px;"><input type="checkbox" value="([^"]+)"/g)];
  assert.deepEqual(ids.map(m => m[2]), want.map(w => w.tag.id));
  assert.deepEqual(ids.map(m => Number(m[1])), want.map(w => w.depth * 14));
  assert.ok(want.some(w => w.depth >= 2), 'fixture has grandchildren');
  // Checked exactly for the selected tags, each wired to tagBrowseToggleTag.
  for (const m of html.matchAll(/<input type="checkbox" value="([^"]+)" ([^>]*)onchange="tagBrowseToggleTag\('session','([^']+)',this\.checked\)"/g)) {
    assert.equal(m[1], m[3]);
    assert.equal(/\bchecked\b/.test(m[2]), sel.includes(m[1]), m[1]);
  }
  assert.equal((html.match(/tagBrowseToggleTag\('session'/g) || []).length, 30);
  assert.ok(html.includes("tagBrowseClear('session')"), 'Clear is shown with a selection');
  assert.ok(html.includes(h.sb.t('tags_menu_clear')));
});

test('Clear is hidden without a selection and the mode button follows the match mode', () => {
  const h = loadApp();
  const tags = manyTags(4);
  const any = setup(h, { tags });
  assert.equal(any.includes('tagBrowseClear'), false);
  assert.ok(any.includes(h.sb.t('tag_browse_match_any')));
  assert.ok(any.includes('aria-pressed="false"'));
  const all = setup(h, { tags, sel: [tags[0].id], mode: 'all' });
  assert.ok(all.includes(h.sb.t('tag_browse_match_all')));
  assert.ok(all.includes('aria-pressed="true"'));
  assert.ok(all.includes("tagBrowseToggleMode('session')"));
  assert.ok(all.includes("tagBrowseClear('session')"));
});

test('with no open session the This session section is absent', () => {
  const h = loadApp();
  const cases = [
    { view: 'sessions', active: '' },
    { view: 'session-detail', active: '' },
    { view: 'sessions', active: 'host-abcd' },
  ];
  for (const c of cases) {
    const html = setup(h, { tags: manyTags(3), ...c });
    assert.equal(html.includes(h.sb.t('tags_menu_this_session')), false, JSON.stringify(c));
    assert.equal(html.includes('toggleSessionWatch('), false);
    assert.equal(html.includes('openSessionTagsModal'), false);
    assert.equal(sections(html), 1, 'only the Tags heading');
  }
});

test('with an open session the section offers Watch and Edit tags with the full session id', () => {
  const h = loadApp();
  const id = "host-ab'cd";
  const off = setup(h, { view: 'session-detail', active: id });
  assert.ok(off.includes(h.sb.t('tags_menu_this_session')));
  assert.ok(off.includes(h.sb.t('tags_menu_watch_this')));
  assert.equal(off.includes(h.sb.t('tags_menu_unwatch_this')), false);
  assert.equal(sections(off), 2);
  const watchCall = off.match(/onclick="toggleSessionWatch\('([^"]*)'\)"/);
  assert.ok(watchCall, 'watch button calls toggleSessionWatch');
  assert.equal(watchCall[1], h.sb.escJsAttr(id));
  const editCall = off.match(/onclick="closeTagsMenu\(\);openSessionTagsModal\('([^"]*)'\)"/);
  assert.ok(editCall, 'edit button closes the menu then opens the modal');
  assert.equal(editCall[1], h.sb.escJsAttr(id));
  assert.ok(off.includes(h.sb.t('tags_menu_edit_this')));
  assert.ok(off.indexOf('toggleSessionWatch(') < off.indexOf('openSessionTagsModal'));

  const on = setup(h, { view: 'session-detail', active: id, watched: [id] });
  assert.ok(on.includes(h.sb.t('tags_menu_unwatch_this')));
  assert.equal(on.includes(h.sb.t('tags_menu_watch_this')), false);
  assert.ok(on.includes("toggleSessionWatch('"));
});

test('the Tags menu rows match the rows the Automata facets draw', () => {
  const h = loadApp();
  const tags = manyTags(6);
  setTags(h, tags);
  run(h, "_tagBrowse.session.sel = []; _tagBrowse.automata.sel = [];");
  const row = html => html.match(/<label class="tag-multi-row"[\s\S]*?<\/label>/)[0].replace(/'session'|'automata'/g, "'S'");
  const menu = h.sb._tagMultiRowsHTML('session');
  const facets = h.sb._tagFacetsHTML('automata', false);
  assert.equal(row(menu).replace(/<span class="tag-facet-count">\d+<\/span>/, ''), row(facets).replace(/<span class="tag-facet-count">\d+<\/span>/, ''));
  assert.ok(facets.includes('class="tag-facet-count"'), 'facets keep their counts');
});

test('the new strings exist in every locale with real translations', () => {
  const keys = ['tags_menu_watched_only', 'tags_menu_no_tags', 'tags_menu_clear', 'tags_menu_this_session', 'tags_menu_watch_this', 'tags_menu_unwatch_this', 'tags_menu_edit_this'];
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  for (const l of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));
    for (const k of keys) {
      assert.ok(b[k], l + ' ' + k);
      assert.doesNotMatch(b[k], /PRD/);
      if (l !== 'en') assert.notEqual(b[k], en[k], l + ' ' + k + ' is translated');
    }
  }
});
