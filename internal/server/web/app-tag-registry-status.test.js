// Tag registry status in the PWA: the role and keeper line, the stale banner and
// the disabled edits while the keeper is unreachable, the pending sync badge, the
// conflict toast, merge notes with their Dismiss request, and escaping of tag names.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const flush = () => new Promise(r => setImmediate(r));

function loadApp(opts) {
  const sandbox = buildSandbox(opts);
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

// setRegistry replaces the registry copy the tag helpers read.
function setRegistry(sb, reg) {
  vm.runInContext('_tagsState.registry = ' + JSON.stringify(reg), sb);
}

// stubApi routes apiFetch to respond(path, opts) and records each call.
function stubApi(sb, respond) {
  const calls = [];
  sb.apiFetch = (p, opts) => {
    calls.push({ path: p, method: (opts && opts.method) || 'GET' });
    return Promise.resolve(respond(p, opts));
  };
  return calls;
}

const TAGS = [
  { id: 'aaaa0001', name: 'Work', count: 2, subtree_count: 2, direct_count: 2 },
  { id: 'aaaa0002', name: 'Mobile', parent_id: 'aaaa0001', count: 1, subtree_count: 1, direct_count: 1 },
];

const peer = (over) => Object.assign({
  role: 'peer', keeper: 'node-a', stale: false, last_synced_at: '2026-10-25T10:00:00Z',
  revision: 4, pending_sync_count: 0, notes: [], last_error: '',
}, over || {});

test('standalone role renders nothing: no status line, banner or notes', () => {
  const sb = loadApp();
  setRegistry(sb, { role: 'standalone', keeper: '', stale: false, notes: [] });
  assert.equal(sb._tagRegistryBlockHTML({ role: 'standalone', keeper: '', stale: false, notes: [] }), '');
  assert.equal(sb._tagRegistryBlockHTML({ role: 'standalone', keeper: '', stale: false, notes: [{ id: 'n', kind: 'merged', name: 'X' }] }), '');
  assert.equal(sb._tagRegistryBlockHTML(null), '');
});

test('keeper role shows the role and the keeper, with no banner', () => {
  const sb = loadApp();
  const html = sb._tagRegistryBlockHTML({ role: 'keeper', keeper: 'node-a', stale: false, notes: [] });
  assert.match(html, /Shared registry: keeper · keeper: node-a/);
  assert.doesNotMatch(html, /tag-registry-banner/);
});

test('peer role shows the role and the keeper it follows', () => {
  const sb = loadApp();
  const html = sb._tagRegistryBlockHTML(peer());
  assert.match(html, /Shared registry: peer · keeper: node-a/);
  assert.doesNotMatch(html, /tag-registry-banner/);
});

test('stale peer shows the banner with the time of the last copy', () => {
  const sb = loadApp();
  const html = sb._tagRegistryBlockHTML(peer({ stale: true }));
  assert.match(html, /class="tag-registry-banner"/);
  assert.match(html, /Showing the last copy from .+; the keeper is unreachable/);
});

test('stale banner says an unknown time when there is no sync time', () => {
  const sb = loadApp();
  const html = sb._tagRegistryBannerHTML(peer({ stale: true, last_synced_at: null }));
  assert.match(html, /Showing the last copy from an unknown time; the keeper is unreachable/);
});

test('stale marks a count and the marker is absent while the copy is current', () => {
  const sb = loadApp();
  setRegistry(sb, peer({ stale: true }));
  assert.match(sb._tagStaleMarkHTML(), /class="tag-stale-mark"/);
  setRegistry(sb, peer({ stale: false }));
  assert.equal(sb._tagStaleMarkHTML(), '');
});

test('stale state disables rename, re-parent, recolor and delete in the panel, not create', async () => {
  const panel = { innerHTML: '' };
  const sb = loadApp({ byId: { automataTagRegistryPanel: panel } });
  stubApi(sb, () => ({ tags: TAGS, untagged: null, registry: peer({ stale: true }) }));
  sb.loadAutomataTagRegistryPanel();
  await flush(); await flush();
  const html = panel.innerHTML;
  const controls = html.match(/<(input|select|button)[^>]*tag-registry-control[^>]*>/g) || [];
  assert.ok(controls.length >= 7, 'expected controls on both tag rows, found ' + controls.length);
  for (const c of controls) assert.match(c, / disabled/, 'control not disabled: ' + c);
  assert.match(html, /paused until the keeper is reachable again/);
  const create = html.match(/<button class="btn-primary"[^>]*>/);
  assert.ok(create, 'create button present');
  assert.doesNotMatch(create[0], /disabled/);
});

test('current copy leaves the panel controls enabled', async () => {
  const panel = { innerHTML: '' };
  const sb = loadApp({ byId: { automataTagRegistryPanel: panel } });
  stubApi(sb, () => ({ tags: TAGS, untagged: null, registry: peer({ stale: false }) }));
  sb.loadAutomataTagRegistryPanel();
  await flush(); await flush();
  assert.doesNotMatch(panel.innerHTML, / disabled/);
  assert.doesNotMatch(panel.innerHTML, /tag-registry-banner/);
});

test('a blocked delete shows the reason and sends no request', () => {
  const sb = loadApp();
  setRegistry(sb, peer({ stale: true }));
  const toasts = [];
  sb.showToast = (msg, type) => toasts.push({ msg, type });
  const calls = stubApi(sb, () => ({}));
  sb.automataTagDelete('aaaa0001');
  assert.equal(calls.length, 0);
  assert.equal(toasts.length, 1);
  assert.match(toasts[0].msg, /paused until the keeper is reachable again/);
});

test('a created tag that is pending sync shows the pending badge', async () => {
  const panel = { innerHTML: '' };
  const sb = loadApp({ byId: { automataTagRegistryPanel: panel } });
  const pendingTags = [{ id: 'aaaa0009', name: 'Fresh', pending_sync: true, count: 0 }];
  stubApi(sb, () => ({ tags: pendingTags, untagged: null, registry: peer({ stale: false }) }));
  sb.loadAutomataTagRegistryPanel();
  await flush(); await flush();
  assert.match(panel.innerHTML, /<span class="tag-pending-badge">pending sync<\/span>/);
});

test('conflict toast names the field and the keeper value', async () => {
  const sb = loadApp();
  setRegistry(sb, peer({ stale: false }));
  const toasts = [];
  sb.showToast = (msg, type) => toasts.push({ msg, type });
  stubApi(sb, (p, opts) => {
    if (opts && opts.method === 'PATCH') return { id: 'aaaa0001', name: 'Keeper name', conflict: true, conflict_field: 'name' };
    return { tags: TAGS, untagged: null, registry: peer({ stale: false }) };
  });
  sb.automataTagUpdate('aaaa0001', { name: 'Mine' });
  await flush(); await flush();
  assert.equal(toasts.length, 1);
  assert.equal(toasts[0].msg, "The keeper's version won: Name is now Keeper name");
});

test('a clean write shows no conflict toast', async () => {
  const sb = loadApp();
  setRegistry(sb, peer({ stale: false }));
  const toasts = [];
  sb.showToast = (msg) => toasts.push(msg);
  stubApi(sb, (p, opts) => (opts && opts.method === 'PATCH')
    ? { id: 'aaaa0001', name: 'Mine', conflict: false }
    : { tags: TAGS, untagged: null, registry: peer({ stale: false }) });
  sb.automataTagUpdate('aaaa0001', { name: 'Mine' });
  await flush(); await flush();
  assert.equal(toasts.length, 0);
});

test('a merge note renders its sentence with a Dismiss button', () => {
  const sb = loadApp();
  const html = sb._tagRegistryBlockHTML(peer({
    notes: [{ id: 'n1', kind: 'merged', name: 'Work', merged_at: '2026-10-25T10:00:00Z' }],
  }));
  assert.match(html, /Tag Work was created on two nodes while they were apart and has been merged/);
  assert.match(html, /onclick="tagRegistryNoteDismiss\('n1'\)"/);
  assert.match(html, />Dismiss</);
});

test('a removal note renders its own sentence', () => {
  const sb = loadApp();
  const html = sb._tagRegistryNotesHTML(peer({
    notes: [{ id: 'n2', kind: 'removed_on_keeper', name: 'Old' }],
  }));
  assert.match(html, /Tag Old was deleted on the keeper while this node still had Automata under it/);
});

test('dismiss sends a POST to the note route and reloads the tags', async () => {
  const sb = loadApp();
  const calls = stubApi(sb, (p) => (p.endsWith('/dismiss') ? { dismissed: 'n1' } : { tags: TAGS, untagged: null, registry: peer() }));
  sb.tagRegistryNoteDismiss('n1');
  await flush(); await flush();
  assert.equal(calls[0].path, '/api/autonomous/tags/registry/notes/n1/dismiss');
  assert.equal(calls[0].method, 'POST');
});

test('tag names in notes and the status line are escaped', () => {
  const sb = loadApp();
  const evil = '<img src=x onerror=alert(1)>';
  const html = sb._tagRegistryBlockHTML(peer({
    keeper: evil,
    notes: [{ id: 'n3', kind: 'merged', name: evil }],
  }));
  assert.doesNotMatch(html, /<img/);
  assert.match(html, /&lt;img src=x onerror=alert\(1\)&gt;/);
});

test('the fallback fills its placeholders when the bundle has no key', () => {
  const sb = loadApp();
  assert.equal(sb._tagT('tag_no_such_key_xyz', 'Hello %1$s, %2$d', ['A', 3]), 'Hello A, 3');
});
