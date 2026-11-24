// Pins where a pause goes. Default: this daemon only. When the header server
// selector is on All, the pause window offers each server (the same list the
// selector shows) or all servers. The wording says the pause is for automation
// work, which is what the pause service holds.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const bundle = (l) => JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));
const plain = (v) => JSON.parse(JSON.stringify(v));

const SERVERS = [
  { name: 'pi-box', label: 'Pi box', enabled: true },
  { name: 'gpu-1', enabled: true },
  { name: 'old', label: 'Old host', enabled: false },
];

function load(active, extra) {
  const sandbox = buildSandbox();
  sandbox.t = (k) => k;
  sandbox.escHtml = (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  sandbox.state = { activeServer: active, servers: { servers: SERVERS } };
  // The header selector's own list (app.js _pickerServers): enabled servers only.
  sandbox._pickerServers = () => SERVERS.filter(s => s && s.enabled !== false);
  Object.assign(sandbox, extra || {});
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app-pause.js'), sandbox);
  return sandbox;
}

test('the picker is offered only where the pause targets all servers', () => {
  for (const active of [null, 'local', 'pi-box']) {
    assert.deepEqual(plain(load(active).window.pauseTargetOptions()), [], `no picker when the selector is ${active}`);
  }
  const opts = plain(load('all').window.pauseTargetOptions());
  assert.deepEqual(opts, [
    { value: '', label: 'This server only' },
    { value: 'pi-box', label: 'Pi box' },
    { value: 'gpu-1', label: 'gpu-1' },
    { value: 'all', label: 'All servers' },
  ]);
});

test('the default target is this server only', () => {
  const sb = load('all');
  assert.equal(sb.window._pause.target || '', '');
  const html = sb.window.pauseBodyHtml();
  assert.match(html, /<select id="pauseTarget"/);
  assert.match(html, /<option value="" selected>/);
  assert.doesNotMatch(html, /value="old"/, 'a disabled server is not offered, as in the header selector');
  assert.doesNotMatch(load(null).window.pauseBodyHtml(), /pauseTarget/);
});

test('a pause aimed at this server sends nothing extra', () => {
  const sb = load('all');
  const built = sb.window.pauseBuildSetBody({ reason: 'freeze' });
  assert.deepEqual(plain(built.body), { scope: 'global', reason: 'freeze', actor: 'web' });
  assert.equal(built.path, '/api/pause');
});

test('a pause aimed at all servers asks the daemon to send it to every peer too', () => {
  const sb = load('all');
  sb.window._pause.target = 'all';
  const built = sb.window.pauseBuildSetBody({ reason: 'freeze' });
  assert.deepEqual(plain(built.body), { scope: 'global', reason: 'freeze', actor: 'web', all_peers: true });
  assert.equal(built.path, '/api/pause');
});

test('a pause aimed at one server goes to that server only, through the proxy', async () => {
  const calls = [];
  const sb = load('all', { apiFetch: (p, o) => { calls.push([p, o && o.body ? JSON.parse(o.body) : null]); return Promise.resolve({ paused: true }); } });
  sb.window._pause.target = 'pi-box';
  sb.document.getElementById = (id) => ({ pauseReason: { value: 'r' }, pauseProject: { value: '' } }[id] || null);
  await sb.window.pauseSet();
  assert.equal(calls[0][0], '/api/proxy/pi-box/api/pause');
  assert.deepEqual(plain(calls[0][1]), { scope: 'global', reason: 'r', actor: 'web' });
  await sb.window.pauseResume('all', '');
  assert.equal(calls[1][0], '/api/proxy/pi-box/api/pause/resume');
  assert.deepEqual(plain(calls[1][1]), { all: true, actor: 'web' });
});

test('a resume aimed at all servers goes to every peer too', async () => {
  const calls = [];
  const sb = load('all', { apiFetch: (p, o) => { calls.push([p, JSON.parse(o.body)]); return Promise.resolve({ paused: false }); } });
  sb.window._pause.target = 'all';
  await sb.window.pauseResume('global', '');
  assert.equal(calls[0][0], '/api/pause/resume');
  assert.deepEqual(plain(calls[0][1]), { scope: 'global', actor: 'web', all_peers: true });
});

test('a target chosen in one view is ignored once the selector leaves All', async () => {
  const calls = [];
  const sb = load('all', { apiFetch: (p, o) => { calls.push([p, JSON.parse(o.body)]); return Promise.resolve({ paused: false }); } });
  sb.window._pause.target = 'pi-box';
  sb.state.activeServer = null;
  await sb.window.pauseResume('global', '');
  assert.equal(calls[0][0], '/api/pause/resume');
  assert.deepEqual(plain(calls[0][1]), { scope: 'global', actor: 'web' });
});

test('looking at all servers asks for every peer state and lists each one', async () => {
  const calls = [];
  const state = { paused: true, global: { reason: 'freeze', by: 'web' }, peers: {
    'pi-box': { ok: true, state: { paused: true } },
    'gpu-1': { ok: true, state: { paused: false } },
    'down-1': { ok: false, error: 'connection refused' },
  } };
  const sb = load('all', { apiFetch: (p) => { calls.push(p); return Promise.resolve(state); } });
  sb.window._pause.target = 'all';
  await sb.window.pauseRefresh();
  assert.equal(calls[0], '/api/pause?peers=1');
  sb.window._pause.state = state;
  const html = sb.window.pauseBodyHtml();
  assert.match(html, /down-1[^<]*connection refused/);
  assert.match(html, /pi-box[^<]*: paused/);
  assert.match(html, /gpu-1[^<]*: not paused/);
});

test('the window and the badge say the pause is for automation work, in every language', () => {
  const en = bundle('en');
  for (const k of ['pause_modal_title', 'pause_badge_title_paused', 'pause_badge_title_idle', 'pause_hint']) {
    assert.match(en[k], /automation/i, `${k} must say automation work: ${en[k]}`);
  }
  assert.match(en.pause_hint, /operator sessions/i, 'the hint must say what is not paused');
  const html = fs.readFileSync(path.join(__dirname, 'index.html'), 'utf8');
  assert.match(html, /id="headerPauseBtn"[^>]*title="Pause automation work"[^>]*aria-label="Pause automation work"/);
  for (const l of LOCALES.slice(1)) {
    const b = bundle(l);
    for (const k of ['pause_modal_title', 'pause_badge_title_paused', 'pause_badge_title_idle', 'pause_hint']) {
      assert.notEqual(b[k], en[k], `${l} ${k} must be translated`);
    }
  }
});

test('the new strings exist in all five locales as real translations', () => {
  const keys = ['pause_target_label', 'pause_target_local', 'pause_target_all', 'pause_peer_paused', 'pause_peer_clear'];
  for (const l of LOCALES) for (const k of keys) assert.ok(bundle(l)[k], `${l} lacks ${k}`);
  // "Server" is the same word in German and French, so the one-word label is not compared.
  for (const l of LOCALES.slice(1)) for (const k of keys.filter(x => x !== 'pause_target_label')) assert.notEqual(bundle(l)[k], bundle('en')[k], `${l} ${k} is an English copy`);
});
