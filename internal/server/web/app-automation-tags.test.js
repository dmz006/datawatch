// Automaton tags — pure tree, matching, suggestion helpers, persistence keys and locale parity.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

const plain = v => JSON.parse(JSON.stringify(v));
const TAGS = [
  { id: 'aaaa0001', name: 'Work' },
  { id: 'aaaa0002', name: 'Mobile', parent_id: 'aaaa0001' },
  { id: 'aaaa0003', name: 'iOS', parent_id: 'aaaa0002' },
  { id: 'aaaa0004', name: 'Home' },
];

test('empty tag id matches every Automaton', () => {
  const sb = loadApp();
  assert.equal(sb._automataTagMatches({ tag_ids: [] }, '', true, {}), true);
});

test('exact match works with include-sub off', () => {
  const sb = loadApp();
  const byId = sb._tagsById(TAGS);
  assert.equal(sb._automataTagMatches({ tag_ids: ['aaaa0001'] }, 'aaaa0001', false, byId), true);
  assert.equal(sb._automataTagMatches({ tag_ids: ['aaaa0003'] }, 'aaaa0001', false, byId), false);
});

test('include-sub matches any descendant, not siblings', () => {
  const sb = loadApp();
  const byId = sb._tagsById(TAGS);
  assert.equal(sb._automataTagMatches({ tag_ids: ['aaaa0003'] }, 'aaaa0001', true, byId), true);
  assert.equal(sb._automataTagMatches({ tag_ids: ['aaaa0004'] }, 'aaaa0001', true, byId), false);
  assert.equal(sb._automataTagMatches({}, 'aaaa0001', true, byId), false);
});

test('counts roll up only when include-sub is on', () => {
  const sb = loadApp();
  const byId = sb._tagsById(TAGS);
  const prds = [{ tag_ids: ['aaaa0001'] }, { tag_ids: ['aaaa0003'] }, { tag_ids: ['aaaa0004'] }];
  assert.equal(sb._automataTagCount(prds, 'aaaa0001', true, byId), 2);
  assert.equal(sb._automataTagCount(prds, 'aaaa0001', false, byId), 1);
});

test('tree order is depth first with depths', () => {
  const sb = loadApp();
  const rows = plain(sb._tagTreeOrder(TAGS)).map(r => [r.tag.name, r.depth]);
  assert.deepEqual(rows, [['Home', 0], ['Work', 0], ['Mobile', 1], ['iOS', 2]]);
});

test('self-and-descendants covers the subtree', () => {
  const sb = loadApp();
  assert.deepEqual([...sb._tagSelfAndDescendants('aaaa0002', TAGS)].sort(), ['aaaa0002', 'aaaa0003']);
});

test('alias suggestion picks only an existing tag by name', () => {
  const sb = loadApp();
  const projects = [{ name: 'home', dir: '/srv/home/' }, { name: 'nomatch', dir: '/srv/x' }];
  assert.equal(sb._tagSuggestFromAlias('/srv/home', projects, TAGS), 'aaaa0004');
  assert.equal(sb._tagSuggestFromAlias('/srv/x', projects, TAGS), '');
  assert.equal(sb._tagSuggestFromAlias('/elsewhere', projects, TAGS), '');
});

test('a multi-tag Automaton appears under each of its tags', () => {
  const sb = loadApp();
  const byId = sb._tagsById(TAGS);
  const prd = { tag_ids: ['aaaa0003', 'aaaa0004'] };
  assert.equal(sb._automataTagMatches(prd, 'aaaa0003', false, byId), true);
  assert.equal(sb._automataTagMatches(prd, 'aaaa0004', false, byId), true);
  assert.equal(sb._automataTagMatches(prd, 'aaaa0001', true, byId), true);
  assert.equal(sb._automataTagMatches(prd, 'aaaa0002', false, byId), false);
});

test('All shows every Automaton, tagged or not', () => {
  const sb = loadApp();
  const byId = sb._tagsById(TAGS);
  const prds = [{ tag_ids: ['aaaa0003'] }, { tag_ids: [] }, {}];
  assert.equal(prds.filter(p => sb._automataTagMatches(p, '', false, byId)).length, 3);
  assert.equal(prds.filter(p => sb._automataTagMatches(p, '', true, byId)).length, 3);
});

test('tag filter state persists under its localStorage keys', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  for (const k of ['cs_automata_tag_filter', 'cs_automata_tag_include_sub']) {
    assert.ok(src.includes(`getItem('${k}')`), `${k} is read`);
    assert.ok(src.includes(`setItem('${k}'`), `${k} is written`);
  }
});

test('every tag_ key used in app.js exists in all five bundles', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  const keys = new Set();
  for (const m of src.matchAll(/_tagT\('(tag_[a-z_]+)'/g)) keys.add(m[1]);
  for (const m of src.matchAll(/\bt\('(tag_[a-z_]+)'/g)) keys.add(m[1]);
  assert.ok(keys.size >= 20, 'found the tag_ keys');
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    for (const k of keys) assert.ok(bundle[k], `${lang} missing ${k}`);
  }
});
