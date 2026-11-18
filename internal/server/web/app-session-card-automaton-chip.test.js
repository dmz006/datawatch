// Session cards — bare-id placement chips.
//
// Placement chips whose value is a bare id (automaton:1d440001,
// stage:ab12cd34, task:3) are meaningless on their own: the card must show
// only the axis word ("Automaton") as the chip text and keep the full tag in
// the chip's tooltip. Chips whose value is an already-readable name
// (lane:gpu, kind:research, role:reviewer) render unchanged, chip text and
// tooltip alike. The rule applies to every session-card chip surface: the
// list card's machine-derived placement chips, its own/inherited registry
// chips, and the tile / compact grid chips.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const WEB = __dirname;
const NOW = Date.parse('2026-10-25T12:00:00Z');
const plain = v => JSON.parse(JSON.stringify(v));

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(WEB, 'app.js'), sandbox);
  // t() returns the raw key until a bundle loads; install the English one so
  // the rendered strings are the real user-visible ones.
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

function baseSession(over) {
  return Object.assign({
    id: 'a1',
    full_id: 'local/a1',
    name: 'Automation run',
    state: 'running',
    hostname: 'dev-1',
    updated_at: new Date(NOW - 60e3).toISOString(),
  }, over);
}

// ── the label rule itself ─────────────────────────────────────────────────

test('the helper folds bare-id system axes to the axis word and leaves readable values alone', () => {
  const sb = loadApp();
  const p = n => sb._placementChipParts(n);
  assert.deepEqual(plain(p('automaton:1d440001')), { label: 'Automaton', idLike: true });
  assert.deepEqual(plain(p('stage:ab12cd34')), { label: 'Stage', idLike: true });
  assert.deepEqual(plain(p('task:3')), { label: 'Task', idLike: true });
  assert.deepEqual(plain(p('story:s-9')), { label: 'Story', idLike: true });
  // Readable values keep their full text.
  for (const n of ['lane:gpu', 'kind:research', 'role:reviewer', 'compute:dev-1', 'project:webapp']) {
    assert.deepEqual(plain(p(n)), { label: n, idLike: false }, `${n} is a readable value, rendered unchanged`);
  }
  // A named (not bare-id) value on a system axis stays readable.
  assert.deepEqual(plain(p('automaton:My Robot')), { label: 'automaton:My Robot', idLike: false });
  // Names without an axis are untouched.
  assert.deepEqual(plain(p('alpha')), { label: 'alpha', idLike: false });
  assert.deepEqual(plain(p('')), { label: '', idLike: false });
});

// ── list card: machine-derived placement chips ─────────────────────────────

test('a list card with an automaton tag reads the word "Automaton", keeps the full tag in the tooltip', () => {
  const sb = loadApp();
  const s = baseSession({ system_tags: ['automaton:1d440001', 'stage:ab12cd34', 'task:3'] });
  const html = sb.sessionCard(s, 0, 1);
  for (const [tag, word] of [['automaton:1d440001', 'Automaton'], ['stage:ab12cd34', 'Stage'], ['task:3', 'Task']]) {
    assert.match(html, new RegExp(`title="${tag}"[^>]*>⚙️ ${word}</span>`), `chip for ${tag} shows the axis word and carries the full tag as tooltip`);
    assert.ok(!html.includes('\u2699\ufe0f ' + tag + '<'), 'no chip shows the raw ' + tag + ' as text');
  }
  assert.ok(!new RegExp('>\u2699\ufe0f [^<]*1d440001<').test(html), 'the bare id never appears as chip text');
});

test('readable-value placement chips on the list card are unchanged, text and tooltip', () => {
  const sb = loadApp();
  const s = baseSession({ system_tags: ['lane:gpu', 'kind:research', 'role:reviewer'] });
  const html = sb.sessionCard(s, 0, 1);
  const tip = 'Placement tag — set by the machine';
  for (const n of ['lane:gpu', 'kind:research', 'role:reviewer']) {
    assert.match(html, new RegExp(`title="${tip}"[^>]*>⚙️ ${n}</span>`), `${n} keeps its full text and the generic placement tooltip`);
  }
});

// ── list card: registry chips (own and inherited) with a bare-id name ─────

function withSystemTagRegistry(sb) {
  const tg = { id: 'sys_auto', name: 'automaton:1d440001', system: true };
  vm.runInContext(`_tagsState.list = ${JSON.stringify([tg])}; _tagsState.byId = ${JSON.stringify({ sys_auto: tg })}; _tagsState.loaded = true;`, sb);
}

test('an own registry chip named "automaton:<id>" shows the axis word with the full tag in the tooltip', () => {
  const sb = loadApp();
  withSystemTagRegistry(sb);
  const s = baseSession({ tags: ['sys_auto'] });
  const html = sb.sessionCard(s, 0, 1);
  assert.match(html, /class="tag-chip" data-tag-id="sys_auto" title="automaton:1d440001"[^>]*>Automaton<\/span>/);
});

test('an inherited registry chip named "automaton:<id>" keeps the word, the inherited mark, and the full tag as tooltip', () => {
  const sb = loadApp();
  withSystemTagRegistry(sb);
  const s = baseSession({ inherited_tags: ['sys_auto'] });
  const html = sb.sessionCard(s, 0, 1);
  assert.match(html, /class="tag-chip tag-chip-inherited" data-tag-id="sys_auto" title="automaton:1d440001"[^>]*>Automaton <span class="tag-chip-inherited-label"[^>]*>inherited<\/span><\/span>/);
});

// ── tile and compact: the same rule in the session tile ───────────────────

test('a tile with an automaton tag chip shows the word "Automaton" and keeps the full tag in the tooltip', () => {
  const sb = loadApp();
  withSystemTagRegistry(sb);
  const s = baseSession({ tags: ['sys_auto'] });
  const tile = sb.renderSessionTile(s, NOW);
  assert.match(tile, /class="tag-chip" data-tag-id="sys_auto" title="automaton:1d440001"[^>]*>Automaton<\/span>/);
  assert.doesNotMatch(tile, />Automaton:1d440001</, 'no chip text carries the raw id');
});

test('the compact grid keeps the same word-only chip with the full tag in the tooltip', () => {
  const sb = loadApp();
  withSystemTagRegistry(sb);
  const s = baseSession({ tags: ['sys_auto'] });
  const grid = sb.renderSessionTileGrid(sb.renderSessionTile(s, NOW), 'compact');
  assert.match(grid, /session-tile-grid-compact/);
  assert.match(grid, /class="tag-chip" data-tag-id="sys_auto" title="automaton:1d440001"[^>]*>Automaton<\/span>/);
});

// ── the Filters sheet's Automaton picker is left as-is ─────────────────────

test('the Filters sheet still lists the Automaton by title with its id kept visible', () => {
  const sb = loadApp();
  vm.runInContext('state.sessions = [{ id: "x1", prd_id: "1d440001", prd_title: "Nightly build" }, { id: "x2", prd_id: "2e550002", prd_title: "Morning build" }];', sb);
  vm.runInContext(`state._sessionTagInventory = {
    loaded: true, at: Date.now(),
    entries: [
      { tag: 'automaton:1d440001', value: '1d440001', count: 3 },
      { tag: 'automaton:2e550002', value: '2e550002', count: 1 },
      { tag: 'lane:gpu', value: 'gpu', count: 5 }
    ]
  };`, sb);
  const rows = plain(sb._sessionTagAutomatonRows());
  assert.equal(rows.length, 2, 'two distinct Automata, not one collapsed "Automaton"');
  assert.ok(rows.map(r => r.id).includes('1d440001'));
  assert.ok(rows.map(r => r.id).includes('2e550002'));
  // Two Automata sharing a title stay disambiguated: the picker keeps the
  // raw id as secondary text on every row.
  const rows0 = plain(sb._sessionTagAutomatonRows());
  assert.deepEqual(rows0.map(r => r.title).sort(), ['Morning build', 'Nightly build'], 'one title per Automaton');
  const html = sb._sessionTagAutomatonResultsHTML();
  // The searchable picker keeps the title with the raw id alongside, so the
  // two never collapse into a single "Automaton".
  for (const [title, id] of [['Nightly build', '1d440001'], ['Morning build', '2e550002']]) {
    assert.match(html, new RegExp('sessions-tag-pick-title">' + title + '</span><span class="sessions-tag-pick-id">' + id + '</span>'), title + ' row keeps its id ' + id);
  }
});
