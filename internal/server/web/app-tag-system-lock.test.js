// System tags in the tag registry panel: a lock badge, and rename, re-parent and
// delete disabled for them (recolor stays enabled). Ordinary tags keep every
// control; a tag name is escaped; the five bundles carry the new strings.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const flush = () => new Promise(r => setImmediate(r));

function loadApp(opts) {
  const sandbox = buildSandbox(opts);
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

const TAGS = [
  { id: 'sys00001', name: 'automation:release', system: true, count: 3 },
  { id: 'usr00001', name: 'Mobile', count: 1 },
];

async function panelFor(tags, registry) {
  const panel = { innerHTML: '' };
  const sb = loadApp({ byId: { automataTagRegistryPanel: panel } });
  sb.apiFetch = () => Promise.resolve({ tags, untagged: null, registry: registry || null });
  sb.loadAutomataTagRegistryPanel();
  await flush(); await flush();
  return panel.innerHTML;
}

// rowOf returns the markup of the registry row that holds a tag's name input.
function rowOf(html, name) {
  const start = html.indexOf(`value="${name}"`);
  assert.ok(start >= 0, name + ' row drawn');
  const rowStart = html.lastIndexOf('<div style="display:flex', start);
  const next = html.indexOf('<div style="display:flex', start);
  return html.slice(rowStart, next < 0 ? html.length : next);
}

test('a system tag shows the lock badge and an ordinary tag does not', async () => {
  const html = await panelFor(TAGS);
  assert.match(rowOf(html, 'automation:release'), /class="tag-system-badge"[^>]*>&#128274; system</);
  assert.doesNotMatch(rowOf(html, 'Mobile'), /tag-system-badge/);
});

test('rename, parent and delete are disabled for a system tag with the lock tooltip', async () => {
  const row = rowOf(await panelFor(TAGS), 'automation:release');
  const name = row.match(/<input type="text"[^>]*>/)[0];
  const parent = row.match(/<select[^>]*>/)[0];
  const del = row.match(/<button[^>]*automataTagDelete[^>]*>/)[0];
  for (const control of [name, parent, del]) {
    assert.match(control, / disabled/);
    assert.match(control, /A system tag: datawatch keeps its name and parent, and it cannot be deleted/);
  }
});

test('the color control of a system tag stays enabled', async () => {
  const row = rowOf(await panelFor(TAGS), 'automation:release');
  const color = row.match(/<input type="color"[^>]*>/)[0];
  assert.doesNotMatch(color, / disabled/);
});

test('an ordinary tag keeps all its controls enabled', async () => {
  const row = rowOf(await panelFor(TAGS), 'Mobile');
  assert.doesNotMatch(row, / disabled/);
});

test('while the keeper is unreachable a system tag still shows the lock reason', async () => {
  const stale = { role: 'peer', keeper: 'node-a', stale: true, last_synced_at: '2026-10-25T10:00:00Z', notes: [] };
  const row = rowOf(await panelFor(TAGS, stale), 'automation:release');
  assert.match(row.match(/<input type="text"[^>]*>/)[0], /A system tag/);
  const other = rowOf(await panelFor(TAGS, stale), 'Mobile');
  assert.match(other.match(/<input type="text"[^>]*>/)[0], /paused until the keeper is reachable again/);
});

test('a system tag name is escaped in the row', async () => {
  const html = await panelFor([{ id: 's2', name: '<b>x</b>', system: true }]);
  assert.doesNotMatch(html, /<b>x<\/b>/);
  assert.match(html, /&lt;b&gt;x&lt;\/b&gt;/);
});

test('_tagSystemBadgeHTML is empty for a plain tag and for nothing', () => {
  const sb = loadApp();
  assert.equal(sb._tagSystemBadgeHTML({ id: 'a', name: 'a' }), '');
  assert.equal(sb._tagSystemBadgeHTML(null), '');
  assert.match(sb._tagSystemBadgeHTML({ id: 'a', name: 'a', system: true }), /tag-system-badge/);
});

test('the lock strings exist in all five bundles', () => {
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    assert.ok(b.tag_system_locked && b.tag_system_badge, lang);
  }
});
