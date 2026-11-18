// Sessions Filters sheet label rule: every list and the compact summary row
// show a human-readable name as the primary text and the id only as a small
// secondary piece; when no name is known the primary text is the localized
// "unnamed" word plus the id's first 8 characters. What a chip selects is
// untouched.
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const WEB = __dirname;

function loadApp(byId) {
  const sandbox = buildSandbox({ byId: byId || {} });
  vm.createContext(sandbox);
  loadScript(path.join(WEB, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  vm.runInContext(`state.activeView = 'other'; _tagsState.list = [{ id: 'u1', name: 'Nightly batch' }]; _tagsState.byId = _tagsById(_tagsState.list); _tagsState.loaded = true;`, sandbox);
  return sandbox;
}
const run = (sb, code) => vm.runInContext(code, sb);
const count = (html, needle) => html.split(needle).length - 1;

function setup(byId) {
  const sb = loadApp(byId);
  run(sb, `state.sessions = [
    { id: 'x0', full_id: 'n/x0', state: 'running', prd_id: 'ab', tags: [] },
    { id: 'x1', full_id: 'n/x1', state: 'running', prd_id: 'ab', tags: [] }
  ];
  state._sessionsGroupTitles = { automaton: { ab: 'Ship the parser' }, project: {}, node: {}, loaded: { automaton: true } };
  _tagBrowse.session.sel = [];`);
  return sb;
}

test('_sessionsChipLabel keeps the name primary and the id secondary', () => {
  const sb = setup();
  // copy into a host object so deepEqual sees plain-realm prototypes
  const parts = o => ({ primary: o.primary, secondary: o.secondary });
  assert.deepEqual(parts(run(sb, `_sessionsChipLabel('Ship the parser', 'ab')`)), { primary: 'Ship the parser', secondary: 'ab' });
  assert.deepEqual(parts(run(sb, `_sessionsChipLabel('', 'cd2204c0deadbeef')`)), { primary: 'unnamed', secondary: 'cd2204c0' });
  assert.deepEqual(parts(run(sb, `_sessionsChipLabel(null, 'cd2204c0deadbeef')`)), { primary: 'unnamed', secondary: 'cd2204c0' });
  // a "name" that is just the id is not a name
  assert.deepEqual(parts(run(sb, `_sessionsChipLabel('ab', 'ab')`)), { primary: 'unnamed', secondary: 'ab' });
  // nothing known at all still renders
  assert.deepEqual(parts(run(sb, `_sessionsChipLabel('')`)), { primary: 'unnamed', secondary: '' });
});

test('a bare operator tag id is never the primary text of a chip', () => {
  const sb = setup();
  const parts = o => ({ primary: o.primary, secondary: o.secondary });
  // tag "zz" has no registry entry: primary is "unnamed", secondary is its id
  assert.deepEqual(parts(run(sb, `_sessionTagEntryLabel({ tag: 'zz', axis: '', value: '' })`)), { primary: 'unnamed', secondary: 'zz' });
  // a tag with a registry name: name primary, id secondary
  assert.deepEqual(parts(run(sb, `_sessionTagEntryLabel({ tag: 'u1', axis: '', value: '' })`)), { primary: 'Nightly batch', secondary: 'u1' });

  run(sb, `state._sessionTagInventory = { loaded: true, entries: [
    { tag: 'zz', axis: '', value: '', count: 1 },
    { tag: 'u1', axis: '', value: '', count: 2 }
  ] };`);
  run(sb, `state.sessions.push({ id: 'x2', full_id: 'n/x2', state: 'done', prd_id: '', tags: ['zz', 'u1'] });`);
  const html = run(sb, '_sessionTagResultsHTML()');
  // the chip shows the unknown tag as "unnamed zz", never a bare "zz" lead
  assert.match(html, /sessionTagChipToggle\('zz'\)">unnamed<span class="badge-count">1<\/span><span class="sessions-chip-id">zz<\/span>/);
  assert.match(html, /sessionTagChipToggle\('u1'\)">Nightly batch<span class="badge-count">1<\/span><span class="sessions-chip-id">u1<\/span>/);
  // the id still rides in the select and the removal affordance
  assert.match(html, /onclick="sessionTagChipToggle\('zz'\)"/);
});

test('project entries show the registered project name or the folder, the directory as secondary', () => {
  const sb = setup();
  const parts = o => ({ primary: o.primary, secondary: o.secondary });
  // no registered name: the folder, not the directory, is primary
  assert.deepEqual(parts(run(sb, `_sessionTagProjectParts('path/to/repo')`)), { primary: 'repo', secondary: 'path/to/repo' });
  // with a registered name: the name is primary, the directory secondary
  run(sb, `state._sessionsGroupTitles.project['path/to/repo'] = 'Acme Platform';`);
  assert.deepEqual(parts(run(sb, `_sessionTagProjectParts('path/to/repo')`)), { primary: 'Acme Platform', secondary: 'path/to/repo' });
  // a flat value has no folder to distinguish
  assert.deepEqual(parts(run(sb, `_sessionTagProjectParts('solo')`)), { primary: 'solo', secondary: '' });
});

test('the automaton picker lists the title with the id as small secondary text', () => {
  const sb = setup();
  run(sb, `state._sessionTagInventory = { loaded: true, entries: [
    { tag: 'automaton:ab', axis: 'automaton', value: 'ab', count: 2 },
    { tag: 'automaton:cd2204c0deadbeef', axis: 'automaton', value: 'cd2204c0deadbeef', count: 1 }
  ] };`);
  const list = run(sb, '_sessionTagAutomatonResultsHTML()');
  assert.match(list, /<span class="sessions-tag-pick-title">Ship the parser<\/span><span class="sessions-tag-pick-id">ab<\/span>/);
  // an automaton with an unknown title renders "unnamed" + first 8 chars
  const row = (list.match(/class="sessions-tag-pick "[^>]*>[\s\S]*?<\/button>/g) || []);
  assert.ok(row.some(r => r.indexOf('>unnamed</span>') >= 0 && r.indexOf('>cd2204c0</span>') >= 0), 'unknown automaton row is unnamed + first 8 chars');
});

test('selected-filter summary shows name first and the id as small secondary text', () => {
  const sb = setup();
  assert.equal(run(sb, '_sessionTagSelectedSummaryHTML()'), '');
  run(sb, `_tagBrowse.session.sel = ['automaton:ab', 'zz'];`);
  const html = run(sb, '_sessionTagSelectedSummaryHTML()');
  assert.equal(count(html, 'class="sessions-tag-sel-chip"'), 2);
  // Automaton: name primary, id secondary
  assert.match(html, /<span class="sessions-tag-sel-text">Ship the parser<\/span><span class="sessions-chip-id">ab<\/span>/);
  // unknown tag: "unnamed" primary, id secondary — never a bare id lead
  assert.match(html, /<span class="sessions-tag-sel-text">unnamed<\/span><span class="sessions-chip-id">zz<\/span>/);
  // what a chip toggles is untouched: same tag values as the selection
  assert.match(html, /onclick="sessionTagChipToggle\('automaton:ab'\)"/);
  assert.match(html, /onclick="sessionTagChipToggle\('zz'\)"/);
  run(sb, `sessionTagChipToggle('zz');`);
  assert.deepEqual(Array.from(run(sb, '_tagBrowse.session.sel')), ['automaton:ab']);
});

test('tooltips are name-led and never lead with a bare id', () => {
  const sb = setup();
  run(sb, `state._sessionTagInventory = { loaded: true, entries: [
    { tag: 'zz', axis: '', value: '', count: 1 },
    { tag: 'u1', axis: '', value: '', count: 2 }
  ] };`);
  run(sb, `state.sessions.push({ id: 'x2', full_id: 'n/x2', state: 'done', prd_id: '', tags: ['zz', 'u1'] });`);
  const html = run(sb, '_sessionTagResultsHTML()');
  const zz = html.match(/title="([^"]*)"[^>]*sessionTagChipToggle\('zz'\)/);
  const u1 = html.match(/title="([^"]*)"[^>]*sessionTagChipToggle\('u1'\)/);
  assert.ok(zz, 'zz chip has a tooltip');
  assert.ok(u1, 'u1 chip has a tooltip');
  assert.ok(zz[1].indexOf('zz') !== 0, 'unknown tag tooltip does not lead with its id: ' + zz[1]);
  assert.match(u1[1], /^Nightly batch/);
});
