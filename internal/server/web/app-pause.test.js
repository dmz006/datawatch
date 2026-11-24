// Pins the operator pause control: the header badge text, the state summary,
// the set body (a reason is required, a project makes it a project pause), the
// resume requests, and that every key the module uses exists in all five
// locales.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];

function load(extra) {
  const sandbox = buildSandbox();
  sandbox.t = (k) => k;
  sandbox.escHtml = (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  Object.assign(sandbox, extra || {});
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app-pause.js'), sandbox);
  return sandbox;
}

test('pauseSummary reads global and project pauses', () => {
  const sb = load();
  const sum = (v) => JSON.parse(JSON.stringify(sb.window.pauseSummary(v)));
  assert.deepEqual(sum(null), { paused: false, count: 0 });
  assert.deepEqual(sum({ paused: false }), { paused: false, count: 0 });
  assert.deepEqual(sum({ paused: true, global: { reason: 'x' }, projects: [{ project: '/a' }] }), { paused: true, count: 2 });
});

test('badge text is empty when nothing is paused and counts when several are', () => {
  const sb = load();
  assert.equal(sb.window.pauseBadgeText({ paused: false }), '');
  // t() here returns the key (an empty bundle): the badge must show the English text, not the key.
  assert.equal(sb.window.pauseBadgeText({ paused: true, global: { reason: 'x' } }), 'Paused');
  assert.equal(sb.window.pauseBadgeText({ paused: true, global: { reason: 'x' }, projects: [{}] }), 'Paused 2');
});

test('set body needs a reason and a project makes it a project pause', () => {
  const sb = load();
  assert.ok(sb.window.pauseBuildSetBody({ reason: '  ' }).error);
  assert.deepEqual(JSON.parse(JSON.stringify(sb.window.pauseBuildSetBody({ reason: 'freeze' }).body)), { scope: 'global', reason: 'freeze', actor: 'web' });
  assert.deepEqual(JSON.parse(JSON.stringify(sb.window.pauseBuildSetBody({ reason: 'r', project: ' web ' }).body)), { scope: 'project', reason: 'r', actor: 'web', project: 'web' });
});

test('resume sends the right bodies and shows the daemon refusal', async () => {
  const calls = [];
  let fail = null;
  const sb = load({ apiFetch: (p, o) => { calls.push([p, JSON.parse(o.body)]); return fail ? Promise.reject(new Error(fail)) : Promise.resolve({ paused: false }); } });
  await sb.window.pauseResume('all', '');
  await sb.window.pauseResume('global', '');
  await sb.window.pauseResume('project', '/work/web');
  assert.deepEqual(calls.map(c => c[1]), [
    { all: true, actor: 'web' }, { scope: 'global', actor: 'web' }, { scope: 'project', actor: 'web', project: '/work/web' }]);
  assert.ok(calls.every(c => c[0] === '/api/pause/resume'));
  fail = 'only the operator can resume paused work';
  await sb.window.pauseResume('global', '');
  assert.equal(sb.window._pause.error, fail);
});

test('the body lists each pause with who, why and a resume button, escaped', () => {
  const sb = load();
  sb.window._pause.state = { paused: true, global: { reason: '<b>freeze</b>', by: 'cli', at: '2026-11-23T12:00:00Z' }, projects: [{ project: '/work/web', reason: 'review', by: 'web' }] };
  const html = sb.window.pauseBodyHtml();
  assert.match(html, /&lt;b&gt;freeze/);
  assert.doesNotMatch(html, /<b>freeze/);
  assert.match(html, /\/work\/web/);
  assert.equal((html.match(/>Resume<\/button>/g) || []).length >= 2, true);
});

test('every key the module and the page use exists in all locales', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app-pause.js'), 'utf8');
  const keys = new Set([...src.matchAll(/_pauseT\('([a-z_]+)'/g)].map(m => m[1]));
  keys.add('pause_badge_paused');
  assert.ok(keys.size >= 12);
  for (const loc of LOCALES) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', loc + '.json'), 'utf8'));
    for (const k of keys) assert.ok(bundle[k] && bundle[k].length > 0, `${loc} lacks ${k}`);
  }
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  for (const loc of LOCALES.slice(1)) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', loc + '.json'), 'utf8'));
    assert.notEqual(b.pause_hint, en.pause_hint, `${loc} must be translated`);
  }
});

test('the header carries the pause button and the script is served', () => {
  const html = fs.readFileSync(path.join(__dirname, 'index.html'), 'utf8');
  assert.match(html, /id="headerPauseBtn"/);
  assert.match(html, /\/app-pause\.js\?v=/);
  const sw = fs.readFileSync(path.join(__dirname, 'sw.js'), 'utf8');
  assert.match(sw, /\/app-pause\.js/);
});
