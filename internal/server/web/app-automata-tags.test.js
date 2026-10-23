// Automaton tags — pure tree, matching and suggestion helpers.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
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
