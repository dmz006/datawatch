// Header Tags menu — each system tag is listed once: placement tags (axis:value) only in the
// Placement tags (system) section, axis-less system tags and operator tags only in the Tags section,
// and the ticking, match-mode and Clear controls still work with placement tags selected.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
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

// The fixture: two placement tags (system, axis:value), one axis-less system tag and two operator tags.
const SYS_AUTO = { id: 'sys-auto', name: 'automation:X', system: true, parent_id: '' };
const SYS_KIND = { id: 'sys-kind', name: 'kind:bug', system: true, parent_id: '' };
const SYS_INTERNAL = { id: 'sys-internal', name: 'internal', system: true, parent_id: '' };
const OP_ALPHA = { id: 'op-alpha', name: 'ops-alpha', system: false, parent_id: '' };
const OP_BETA = { id: 'op-beta', name: 'ops-beta', system: false, parent_id: '' };
const FIXTURE = [SYS_AUTO, SYS_KIND, SYS_INTERNAL, OP_ALPHA, OP_BETA];

// Split the menu into its sections. The placement section sits between the watch row and the Tags heading.
function sectionsOf(h, html) {
  const pStart = html.indexOf('class="tags-menu-head">Placement tags (system)</div>');
  const tStart = html.indexOf(`class="tags-menu-head">${h.sb.t('tags_menu_button')}</div>`);
  assert.ok(pStart >= 0, 'placement section present');
  assert.ok(tStart > pStart, 'Tags section after placement section');
  return { placement: html.slice(pStart, tStart), tags: html.slice(tStart) };
}

// The checkbox rows of the menu, one string per row (each row is a <label class="tag-multi-row">).
const rowsOf = html => html.split('</label>').filter(s => s.includes('class="tag-multi-row"'));

// Rows whose tag-multi-name span ends in the given name (placement rows carry a gear prefix).
function rowsNamed(html, name) {
  const re = new RegExp('class="tag-multi-name">[^<]*' + name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '</span>');
  return rowsOf(html).filter(r => re.test(r));
}

test('a placement tag is listed once, only in the placement section under its axis heading', () => {
  const h = loadApp();
  const html = setup(h, { tags: FIXTURE });
  const { placement, tags } = sectionsOf(h, html);
  for (const name of ['automation:X', 'kind:bug']) {
    assert.equal(rowsNamed(html, name).length, 1, name + ' appears once in the whole menu');
    assert.equal(rowsNamed(placement, name).length, 1, name + ' is in the placement section');
    assert.equal(rowsNamed(tags, name).length, 0, name + ' is not in the Tags section');
  }
  // Each placement row sits under the heading of its own axis.
  const autoHead = placement.indexOf('>automation</div>');
  const kindHead = placement.indexOf('>kind</div>');
  assert.ok(autoHead >= 0 && kindHead >= 0, 'both axis headings drawn');
  assert.ok(autoHead < placement.indexOf('automation:X</span>'), 'automation:X under its axis heading');
  assert.ok(kindHead < placement.indexOf('kind:bug</span>'), 'kind:bug under its axis heading');
});

test('an operator tag is listed once, in the Tags section after the placement section', () => {
  const h = loadApp();
  const html = setup(h, { tags: FIXTURE });
  const { placement, tags } = sectionsOf(h, html);
  for (const name of ['ops-alpha', 'ops-beta']) {
    assert.equal(rowsNamed(html, name).length, 1, name + ' appears once in the whole menu');
    assert.equal(rowsNamed(placement, name).length, 0, name + ' is outside the placement section');
    assert.equal(rowsNamed(tags, name).length, 1, name + ' is in the Tags section');
    assert.equal(rowsNamed(tags, name)[0].includes('tag-system-mark'), false, name + ' has no padlock');
  }
  assert.ok(html.indexOf('ops-alpha</span>') > html.indexOf('class="tags-menu-head">Placement tags (system)</div>'), 'operator tag after the placement section');
});

test('an axis-less system tag is listed once, in the Tags section, with the padlock', () => {
  const h = loadApp();
  const html = setup(h, { tags: FIXTURE });
  const { placement, tags } = sectionsOf(h, html);
  assert.equal(rowsNamed(html, 'internal').length, 1, 'internal appears once in the whole menu');
  assert.equal(rowsNamed(placement, 'internal').length, 0, 'internal is not in the placement section');
  const [row] = rowsNamed(tags, 'internal');
  assert.ok(row, 'internal is in the Tags section');
  assert.ok(row.includes('<span class="tag-system-mark" aria-hidden="true">'), 'internal carries the padlock');
});

test('a selected placement tag is ticked and wired to tagBrowseToggleTag; match mode and Clear still render', () => {
  const h = loadApp();
  const html = setup(h, { tags: FIXTURE, sel: [SYS_AUTO.id] });
  const { placement } = sectionsOf(h, html);
  // The selected placement row is checked and calls the same handler as the operator rows.
  const [auto] = rowsNamed(placement, 'automation:X');
  assert.ok(auto.includes('<input type="checkbox" checked onchange="tagBrowseToggleTag(\'session\',\'sys-auto\',this.checked)">'), auto);
  // The unselected placement row is not ticked.
  const [kind] = rowsNamed(placement, 'kind:bug');
  assert.equal(kind.includes('type="checkbox" checked'), false, kind);
  assert.ok(kind.includes("tagBrowseToggleTag('session','sys-kind',this.checked)"));
  // Only the selected tag is ticked in the whole menu (the watch row is not ticked either).
  assert.equal((html.match(/type="checkbox" checked/g) || []).length, 1);
  // Match mode button and Clear render as before.
  assert.ok(html.includes("tagBrowseToggleMode('session')"));
  assert.ok(html.includes('aria-pressed="false"'));
  assert.ok(html.includes(h.sb.t('tag_browse_match_any')));
  assert.equal((html.match(/tagBrowseClear\('session'\)/g) || []).length, 1, 'Clear is shown once');
  assert.ok(html.includes(h.sb.t('tags_menu_clear')));
  // With the placement tag in ALL mode the button flips as before.
  const all = setup(h, { tags: FIXTURE, sel: [SYS_AUTO.id], mode: 'all' });
  assert.ok(all.includes(h.sb.t('tag_browse_match_all')));
  assert.ok(all.includes('aria-pressed="true"'));
});

test('the tag filter rows without the option, and the Automata facets, still list system tags with the padlock', () => {
  const h = loadApp();
  setup(h, { tags: FIXTURE });
  for (const scope of ['session', 'automata']) {
    const rows = run(h, `_tagFilterRowsHTML(${JSON.stringify(scope)})`);
    for (const name of ['automation:X', 'kind:bug', 'internal']) {
      const [row] = rowsNamed(rows, name);
      assert.ok(row, scope + ' lists ' + name);
      assert.ok(row.includes('tag-system-mark'), scope + ' ' + name + ' carries the padlock');
    }
    const [op] = rowsNamed(rows, 'ops-alpha');
    assert.ok(op, scope + ' lists operator tags');
    assert.equal(op.includes('tag-system-mark'), false, scope + ' operator tag has no padlock');
  }
  const control = run(h, "_tagFilterControlHTML('automata')");
  for (const name of ['automation:X', 'kind:bug', 'internal']) {
    assert.ok(rowsNamed(control, name).length === 1 && rowsNamed(control, name)[0].includes('tag-system-mark'), 'Automata filter control lists ' + name + ' with the padlock');
  }
});
