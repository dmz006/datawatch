// A session tile drops its system placement chip when a later summary row carries
// an empty system_tags (the last placement tag was removed), keeps its own and
// inherited chips, and keeps the chip when a later row omits the system_tags key.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const vm = require('vm');
const { buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const WEB = __dirname;
const TAGS = [
  { id: 'own1', name: 'Work', color: '#3366ff' },
  { id: 'inh1', name: 'Release' },
];

function run(sb, code) { return vm.runInContext(code, sb); }

function loadApp() {
  const view = makeStubElement();
  const sandbox = buildSandbox({ byId: { view } });
  vm.createContext(sandbox);
  loadScript(path.join(WEB, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  run(sandbox, `state.activeView = 'sessions'; state.activeServer = 'local';`);
  sandbox.apiFetch = url => {
    if (String(url).startsWith('/api/autonomous/tags')) return Promise.resolve({ tags: TAGS });
    return Promise.resolve({});
  };
  return { sb: sandbox, view };
}

function row(id, extra) {
  return Object.assign({
    full_id: 'host-' + id, id, state: 'running', task: 'task ' + id,
    updated_at: new Date().toISOString(),
    tags: ['own1'], inherited_tags: ['inh1'],
  }, extra || {});
}

function tile(html, fullId) {
  const parts = html.split('data-full-id="');
  const hit = parts.slice(1).find(p => p.startsWith(fullId + '"'));
  assert.ok(hit, `a tile for ${fullId} was drawn`);
  return hit;
}

async function loadSessions() {
  const { sb, view } = loadApp();
  sb.handleMessage({ type: 'sessions', data: { sessions: [row('aaaa', { system_tags: ['compute:gpu-1'] })] } });
  await flushAsync(20);
  assert.match(tile(view.innerHTML, 'host-aaaa'), /tag-chip-placement[^>]*>[^<]*compute:gpu-1/, 'the tile shows the placement chip');
  return { sb, view };
}

test('a later summary row with an empty system_tags removes the placement chip from the tile', async () => {
  const { sb, view } = await loadSessions();
  sb.handleMessage({ type: 'sessions', data: { sessions: [row('aaaa', { system_tags: [] })] } });
  await flushAsync(20);
  const t = tile(view.innerHTML, 'host-aaaa');
  assert.doesNotMatch(t, /compute:gpu-1/, 'the placement chip is gone');
  assert.doesNotMatch(t, /tag-chip-placement/, 'no placement chip is left');
  assert.match(t, /data-tag-id="own1"/, 'the own chip stays');
  assert.match(t, /tag-chip-inherited" data-tag-id="inh1"/, 'the inherited chip stays');
});

test('a later summary row that omits system_tags keeps the placement chip', async () => {
  const { sb, view } = await loadSessions();
  const later = row('aaaa', { state: 'waiting_input' });
  assert.equal('system_tags' in later, false);
  sb.handleMessage({ type: 'sessions', data: { sessions: [later] } });
  await flushAsync(20);
  const t = tile(view.innerHTML, 'host-aaaa');
  assert.match(t, /tag-chip-placement[^>]*>[^<]*compute:gpu-1/, 'the placement chip stays');
  assert.match(t, /data-tag-id="own1"/);
});
