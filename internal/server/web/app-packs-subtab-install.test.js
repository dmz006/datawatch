// Pins the Packs sub-tab install pieces: install payload and button, the
// summary of what a pack adds, the version change list, and the accept button.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

function fresh() {
  const sandbox = buildSandbox({});
  const calls = [];
  sandbox.apiFetch = (url, opts) => { calls.push({ url, opts }); return Promise.resolve({ ok: true }); };
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app-packs-subtab-install.js'), sandbox);
  return { sb: sandbox, calls };
}

test('install payload needs a source folder', () => {
  const { sb } = fresh();
  assert.deepEqual(JSON.parse(JSON.stringify(sb.packsInstallPayload({ source_dir: ' /r/p ' }))), { source_dir: '/r/p' });
  assert.equal(sb.packsInstallPayload({ name: 'x' }), null);
  assert.equal(sb.packsInstallButtonHTML({ name: 'x' }, 0), '');
  assert.match(sb.packsInstallButtonHTML({ source_dir: '/r/p' }, 2), /packs-install-btn[^>]*packsSubtabInstall\(2,event\)/);
});

test('summary lists tasks, scripts, tools and scopes, escaped', () => {
  const { sb } = fresh();
  const html = sb.packsSummaryHTML({ task_titles: ['A <b>'], verification_scripts: ['check.sh'], tools: ['git'], file_scopes: [] });
  assert.match(html, /This pack adds/);
  assert.match(html, /A &lt;b&gt;/);
  assert.match(html, /check\.sh/);
  assert.match(html, /git/);
  assert.match(html, /<em>none<\/em>/);
});

test('diff lists additions, flags access widening, and is empty when nothing is added', () => {
  const { sb } = fresh();
  assert.equal(sb.packsDiffHTML({}), '');
  const html = sb.packsDiffHTML({ new_tasks: ['T2'], new_tools: ['curl'], new_scripts: [], new_file_scopes: ['src/'] });
  assert.match(html, /New tasks:<\/span> T2/);
  assert.match(html, /packs-diff-widens[^>]*>.*New tools/);
  assert.match(html, /New file scopes/);
  assert.doesNotMatch(html, /New verification scripts/);
});

test('accept button shows only for an unaccepted pack', () => {
  const { sb } = fresh();
  assert.match(sb.packsAcceptButtonHTML({ name: 'p', version: '1.0.0', accepted: false }, 1), /packsSubtabAccept\(1,event\)/);
  assert.equal(sb.packsAcceptButtonHTML({ name: 'p', version: '1.0.0', accepted: true }, 1), '');
  assert.equal(sb.packsAcceptButtonHTML({ name: 'p' }, 1), '');
});

test('pending view combines summary, change list and accept button', () => {
  const { sb } = fresh();
  const html = sb.packsPendingHTML({ name: 'p', version: '2.0.0', summary: { task_titles: ['T'] }, diff: { new_tasks: ['T'] } }, 0);
  assert.match(html, /p 2\.0\.0/);
  assert.match(html, /packs-summary/);
  assert.match(html, /packs-diff/);
  assert.match(html, /packs-accept-btn/);
});

test('accept and install post to the packs routes', async () => {
  const { sb, calls } = fresh();
  sb.PACKS_INSTALL_LIST.push({ name: 'p', version: '1.0.0', source_dir: '/r/p' });
  await sb.packsSubtabAccept(0);
  await sb.packsSubtabInstall(0);
  await flushAsync();
  assert.equal(calls[0].url, '/api/packs/accept');
  assert.deepEqual(JSON.parse(calls[0].opts.body), { name: 'p', version: '1.0.0' });
  assert.equal(calls[1].url, '/api/packs/install');
  assert.deepEqual(JSON.parse(calls[1].opts.body), { source_dir: '/r/p' });
});
