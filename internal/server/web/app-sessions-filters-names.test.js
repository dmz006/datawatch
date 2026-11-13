'use strict';
// Filters sheet, naming rule: every list in the sheet (operator tags,
// projects, the Automaton picker) shows a human-readable name as the
// primary text and the id only as small secondary text; when no name is
// known the primary is the word "unnamed" plus the id's first 8 characters.
// A bare id — e.g. a 6+-digit hex record id — never leads any chip or row.
//
// Loads the real app.js through testutil_browser_stub.js, the same way
// app-sessions-filters-sheet.test.js does.
//
// Run: node --test internal/server/web/app-sessions-filters-names.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub.js');
const vm = require('vm');

const WEB_DIR = __dirname;
const APP_JS = path.join(WEB_DIR, 'app.js');

// Record data -------------------------------------------------------------
const TAG_NAMED = 'a1b2c3d4';               // operator tag, registry knows it: "nightowl"
const TAG_UNNAMED = 'e5f6a7b8';             // operator tag, registry has nothing for it
const AUTO_NAMED = '1d440001';              // Automaton with a known title
const AUTO_NAMED_TITLE = 'Lane refill runner';
const AUTO_UNNAMED = '9f3c0007';            // Automaton with no known title
const PROJ_NAMED = '/srv/work/alpha';  // registered project name: "Alpha Research"
const PROJ_NAMED_LABEL = 'Alpha Research';
const PROJ_UNNAMED = '/srv/beta';           // no registered name → folder name "beta"

function row(id, extra) {
  return Object.assign({
    id, full_id: 'host-' + id, name: 'work-' + id, state: 'running',
    server: 'local', hostname: 'host',
    tags: [], system_tags: [], inherited_tags: [],
    backend_family: 'gpt',
    created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z',
  }, extra || {});
}

// Two sessions spread over the whole inventory: one row per named/unnamed
// pair, both backends present so the sheet's backend row has two chips.
function rows() {
  return [
    row('s1', {
      tags: [TAG_NAMED],
      system_tags: ['automaton:' + AUTO_NAMED, 'project:' + PROJ_NAMED],
      prd_id: AUTO_NAMED, prd_title: AUTO_NAMED_TITLE,
      backend_family: 'gpt',
    }),
    row('s2', {
      tags: [TAG_UNNAMED],
      system_tags: ['automaton:' + AUTO_UNNAMED, 'project:' + PROJ_UNNAMED],
      prd_id: AUTO_UNNAMED,
      backend_family: 'pi',
    }),
  ];
}

const INVENTORY = [
  { tag: TAG_NAMED, value: TAG_NAMED },
  { tag: TAG_UNNAMED, value: TAG_UNNAMED },
  { tag: 'project:' + PROJ_NAMED, axis: 'project', value: PROJ_NAMED },
  { tag: 'project:' + PROJ_UNNAMED, axis: 'project', value: PROJ_UNNAMED },
  { tag: 'automaton:' + AUTO_NAMED, axis: 'automaton' },
  { tag: 'automaton:' + AUTO_UNNAMED, axis: 'automaton' },
];

function freshSandbox() {
  const byId = { 'view-sessions': makeStubElement(), 'sessionFilterInput': makeStubElement() };
  const sandbox = buildSandbox({ byId });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  const en = JSON.parse(fs.readFileSync(path.join(WEB_DIR, 'locales', 'en.json'), 'utf8'));
  sandbox._i18n.bundle = en;
  sandbox._i18n.fallback = en;

  const r = (code) => vm.runInContext(code, sandbox, { filename: '<filters-names-test>' });
  r('state.sessions = ' + JSON.stringify(rows()) + ';');
  r('state._sessionsSheetOpen = true;');
  r('state._sessionTagInventory = { loaded: true, entries: ' + JSON.stringify(INVENTORY) + ' };');
  // The registry knows ONE of the two operator tags.
  r('_tagsState.byId = { ' + JSON.stringify(TAG_NAMED) + ': { id: "' + TAG_NAMED + '", name: "nightowl", system: false, color: "" } };');
  r('_tagsState.list = Object.values(_tagsState.byId); _tagsState.loaded = true;');
  // Group-title cache: the named project and the named Automaton — nothing
  // for the unnamed ones.
  r('state._sessionsGroupTitles = { ' +
    "automaton: { '" + AUTO_NAMED + "': '" + AUTO_NAMED_TITLE + "' }, " +
    "project: { '" + PROJ_NAMED + "': '" + PROJ_NAMED_LABEL + "' }, " +
    'node: {}, loaded: {} };');
  // One saved view for the Views section.
  r("state._savedViews = [{ id: 'sv-night', name: 'Night triage', mode: 'list', groupBy: null, filter: '' }];");
  return sandbox;
}

// The sheet HTML with the same fragments renderSessionsView passes in.
function sheetHTML(sb) {
  const r = (code) => vm.runInContext(code, sb, { filename: '<filters-names-test>' });
  return r(
    '_sessionsSheetHTML({' +
    ' groupBy: _sessionsGroupCurrent(),' +
    ' stateBadges: _sessionsSheetStateChips().html,' +
    ' llmBadges: _sessionsSheetLlmChips().html,' +
    ' tagPanel: _sessionTagFilterPanelHTML(),' +
    ' tagHelp: \'\'' +
    '})'
  ) || '';
}

// Visible text: strip every double-quoted attribute (onclick, title, ... —
// ids legitimately travel in those), then the tags.
function visibleText(html) {
  return String(html)
    .replace(/\s\w[\w:-]*="[^"]*"/g, '')
    .replace(/<[^>]+>/g, ' ')
    .replace(/\s+/g, ' ');
}

// Ids shown as the sheet's small secondary element.
function secondaryIds(html) {
  const out = [];
  let m;
  const reChip = /<span class="sessions-chip-id">([^<]*)<\/span>/g;
  const rePick = /<span class="sessions-tag-pick-id">([^<]*)<\/span>/g;
  while ((m = reChip.exec(html)) !== null) out.push(m[1]);
  while ((m = rePick.exec(html)) !== null) out.push(m[1]);
  return out;
}

// Primary texts: the tag chips and the Automaton picker rows.
function primaries(html) {
  const out = [];
  let m;
  const reChip = /sessions-tag-chip[^>]*>([^<]*)</g;
  const rePick = /<span class="sessions-tag-pick-title">([^<]*)<\/span>/g;
  while ((m = reChip.exec(html)) !== null) out.push(m[1].trim());
  while ((m = rePick.exec(html)) !== null) out.push(m[1].trim());
  return out;
}

const HEX_ID = /^[0-9a-f]{6,}$/i;

test('known names lead: registered tag, project name and Automaton title are the primary text', (t) => {
  const sb = freshSandbox();
  const html = sheetHTML(sb);
  const prim = primaries(html);

  t.test('registry name for the operator tag', () => assert.ok(prim.includes('nightowl'), prim.join(' | ')));
  t.test('registered project name', () => assert.ok(prim.includes(PROJ_NAMED_LABEL), prim.join(' | ')));
  t.test('folder name for the unregistered project', () => assert.ok(prim.includes('beta'), prim.join(' | ')));
  t.test('known Automaton title', () => assert.ok(prim.includes(AUTO_NAMED_TITLE), prim.join(' | ')));
});

test('no chip primary text is a bare id when a name is (or is not) known', (t) => {
  const sb = freshSandbox();
  const prim = primaries(sheetHTML(sb));
  assert.ok(prim.length >= 5, 'chips and picker rows are present: ' + prim.join(' | '));
  const naked = prim.filter(p => HEX_ID.test(p));
  assert.deepEqual(naked, [], 'no bare-hex primary, got: ' + prim.join(' | '));
});

test('the unnamed item shows "unnamed" plus the id as small secondary text, for a tag and for an Automaton', (t) => {
  const sb = freshSandbox();
  const html = sheetHTML(sb);
  const sec = secondaryIds(html);
  const prim = primaries(html);

  t.test('"unnamed" word appears as a primary (tag + Automaton)', () => {
    const unnamed = prim.filter(p => p.toLowerCase() === 'unnamed');
    assert.equal(unnamed.length, 2, 'two unnamed items, primaries: ' + prim.join(' | '));
  });
  t.test('operator tag id in the small secondary element', () => assert.ok(sec.includes(TAG_UNNAMED), 'secondary: ' + sec.join(', ')));
  t.test('unknown Automaton id in the small secondary element', () => assert.ok(sec.includes(AUTO_UNNAMED), 'secondary: ' + sec.join(', ')));
  t.test('named entries also keep their id as secondary text', () => {
    assert.ok(sec.includes(TAG_NAMED), 'tag id secondary: ' + sec.join(', '));
    assert.ok(sec.includes(PROJ_NAMED), 'project dir secondary: ' + sec.join(', '));
    assert.ok(sec.includes(AUTO_NAMED), 'Automaton id secondary: ' + sec.join(', '));
  });
});

test('an id appears in the visible text only inside the small secondary element', (t) => {
  const sb = freshSandbox();
  const html = sheetHTML(sb);

  // Order matters: the secondary spans (whose class carries the very
  // attribute pattern the next step removes) come out first.
  const withoutSecondary = String(html)
    .replace(/<span class="sessions-chip-id">[^<]*<\/span>/g, ' ')
    .replace(/<span class="sessions-tag-pick-id">[^<]*<\/span>/g, ' ')
    .replace(/\s\w[\w:-]*="[^"]*"/g, '');

  for (const id of [TAG_NAMED, TAG_UNNAMED, AUTO_NAMED, AUTO_UNNAMED]) {
    t.test(id, () => assert.ok(withoutSecondary.indexOf(id) < 0,
      'id ' + id + ' must not appear outside the secondary element'));
  }

  // ...while in the visible text it must ride behind the name/word, never lead it.
  const vis = visibleText(html);
  assert.ok(new RegExp('nightowl[^|]*' + TAG_NAMED).test(vis), 'tag id follows its name: ' + vis);
  assert.ok(new RegExp('unnamed[^|]*' + TAG_UNNAMED).test(vis), 'unnamed tag id: ' + vis);
  assert.ok(new RegExp(AUTO_NAMED_TITLE + '[^|]*' + AUTO_NAMED).test(vis), 'Automaton id follows its title: ' + vis);
});

test('counts, saved view and backends render in the same sheet', (t) => {
  const sb = freshSandbox();
  const html = sheetHTML(sb);
  const r = (code) => vm.runInContext(code, sb);

  t.test('chip counts track the current session list', () => {
    const seg = name => name + '<span class="badge-count">1</span>';
    assert.ok(html.includes(seg('nightowl')), 'nightowl count 1');
    assert.ok(html.includes(seg(PROJ_NAMED_LABEL)), 'project count 1');
    assert.ok(html.includes(seg('beta')), 'folder count 1');
  });
  t.test('saved view chip', () =>
    assert.ok(html.includes('data-saved-view="sv-night"') && html.includes('>Night triage<'), 'saved view rendered'));
  t.test('both backend families', () => {
    const types = JSON.parse(r('JSON.stringify(_sessionsSheetLlmChips().types)'));
    assert.deepEqual(types, ['gpt', 'pi']);
    assert.ok(/>gpt</.test(html) && />pi</.test(html), 'backend chips in the sheet');
  });
});
