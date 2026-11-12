// First load of the Sessions page: a session tile shows its system placement chip
// from the first frame, before the tag registry has arrived; the registry then adds
// the own and inherited operator chips; and a later session_state push that omits
// system_tags keeps the chip while an explicit empty system_tags removes it.

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

// The app is loaded with the registry NOT loaded, the sessions view mounted, and
// the registry request held open so the test decides when it arrives.
function loadFirstLoadApp() {
  const view = makeStubElement();
  const sandbox = buildSandbox({ byId: { view } });
  vm.createContext(sandbox);
  loadScript(path.join(WEB, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  run(sandbox, `state.activeView = 'sessions'; state.activeServer = 'local';`);
  const pending = { resolve: null, calls: 0 };
  sandbox.apiFetch = url => {
    if (String(url).startsWith('/api/autonomous/tags')) {
      pending.calls++;
      return new Promise(res => { pending.resolve = () => res({ tags: TAGS }); });
    }
    return Promise.resolve({});
  };
  return { sb: sandbox, view, pending };
}

function row(id, extra) {
  return Object.assign({
    full_id: 'host-' + id, id, state: 'running', task: 'task ' + id,
    updated_at: new Date().toISOString(),
  }, extra || {});
}

function rows() {
  return [
    row('aaaa', { system_tags: ['compute:gpu-1'], tags: ['own1'], inherited_tags: ['inh1'] }),
    row('bbbb', { system_tags: ['compute:gpu-2'], tags: ['own1'], inherited_tags: ['inh1'] }),
  ];
}

// The markup of one session tile, cut from the sessions view.
function tile(html, fullId) {
  const parts = html.split('data-full-id="');
  const hit = parts.slice(1).find(p => p.startsWith(fullId + '"'));
  assert.ok(hit, `a tile for ${fullId} was drawn`);
  return hit;
}

function placementChips(html) { return html.match(/tag-chip-placement[^>]*>[^<]*/g) || []; }

function sendFrame(sb) {
  sb.handleMessage({ type: 'sessions', data: { sessions: rows() } });
}

test('first load: every session tile has its system placement chip before the registry arrives', () => {
  const { sb, view } = loadFirstLoadApp();
  assert.equal(run(sb, '_tagsState.loaded'), false, 'the registry is not loaded yet');
  sendFrame(sb);
  const html = view.innerHTML;
  assert.match(tile(html, 'host-aaaa'), /tag-chip-placement[^>]*>[^<]*compute:gpu-1/);
  assert.match(tile(html, 'host-bbbb'), /tag-chip-placement[^>]*>[^<]*compute:gpu-2/);
});

test('first load: the sessions frame asks for the registry exactly once', () => {
  const { sb, pending } = loadFirstLoadApp();
  sendFrame(sb);
  assert.equal(pending.calls, 1);
});

test('registry arrives: each tile keeps the system chip and gains the own and inherited chips', async () => {
  const { sb, view, pending } = loadFirstLoadApp();
  sendFrame(sb);
  assert.ok(pending.resolve, 'the registry request is in flight');
  pending.resolve();
  await flushAsync(20);
  assert.equal(run(sb, '_tagsState.loaded'), true);
  const html = view.innerHTML;
  for (const [id, gpu] of [['host-aaaa', 'compute:gpu-1'], ['host-bbbb', 'compute:gpu-2']]) {
    const t = tile(html, id);
    assert.match(t, new RegExp('tag-chip-placement[^>]*>[^<]*' + gpu), `${id} keeps its system chip`);
    assert.match(t, /data-tag-id="own1"/, `${id} shows the own chip`);
    assert.match(t, /tag-chip-inherited" data-tag-id="inh1"/, `${id} shows the inherited chip`);
  }
});

test('a later session_state push without system_tags keeps the system chip', async () => {
  const { sb, view, pending } = loadFirstLoadApp();
  sendFrame(sb);
  pending.resolve();
  await flushAsync(20);
  const push = row('aaaa', { tags: ['own1'], inherited_tags: ['inh1'], state: 'waiting_input' });
  assert.equal('system_tags' in push, false);
  sb.handleMessage({ type: 'session_state', data: { session: push } });
  const held = run(sb, `state.sessions.find(s => s.full_id === 'host-aaaa')`);
  assert.deepEqual(Array.from(held.system_tags), ['compute:gpu-1'], 'the held record keeps its placement tags');
  assert.match(run(sb, `_sessionTagChipsHTML(state.sessions.find(s => s.full_id === 'host-aaaa'))`), /tag-chip-placement[^>]*>[^<]*compute:gpu-1/);
  assert.match(tile(view.innerHTML, 'host-aaaa'), /compute:gpu-1/, 'the painted tile still shows it');
});

test('a session_state push with system_tags set to an empty array removes the system chip', async () => {
  const { sb, view, pending } = loadFirstLoadApp();
  sendFrame(sb);
  pending.resolve();
  await flushAsync(20);
  const push = row('aaaa', { tags: ['own1'], inherited_tags: ['inh1'], system_tags: [] });
  sb.handleMessage({ type: 'session_state', data: { session: push } });
  const chips = run(sb, `_sessionTagChipsHTML(state.sessions.find(s => s.full_id === 'host-aaaa'))`);
  assert.equal(placementChips(chips).length, 0, 'no system chip is left');
  assert.match(chips, /data-tag-id="own1"/, 'the operator chips stay');
  assert.doesNotMatch(tile(view.innerHTML, 'host-aaaa'), /compute:gpu-1/, 'the painted tile dropped it');
  assert.match(tile(view.innerHTML, 'host-bbbb'), /compute:gpu-2/, 'the other tile is untouched');
});
