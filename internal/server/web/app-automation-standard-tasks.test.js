// Templates tab, Standard tasks sub-tab: a read-only table loaded from the
// REST read, with the fixed-mode note and how-to link.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

function load(respond) {
  const panel = makeStubElement();
  const sandbox = buildSandbox({ byId: { automataPanel: panel } });
  sandbox.URLSearchParams = URLSearchParams;
  const urls = [];
  sandbox.fetch = url => { urls.push(String(url)); return respond(); };
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return { sb: sandbox, panel, urls };
}
const okBody = body => () => Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(body), text: () => Promise.resolve(JSON.stringify(body)) });

// Runs the registry entry's loader and renderer inside the sandbox.
async function run(sb) {
  await vm.runInContext(`_AUTOMATA_TEMPLATE_SUBTABS.find(s => s.key === 'standard').load()`, sb);
  await flushAsync();
  vm.runInContext(`_automataTmplSubtab = 'standard'; _AUTOMATA_TEMPLATE_SUBTABS.find(s => s.key === 'standard').render()`, sb);
}
const html = p => String(p.innerHTML !== undefined ? p.innerHTML : p._html || '');

test('one row per task, loaded from the REST read', async () => {
  const tasks = [
    { id: 'a', title: 'A', kind: 'docs', summary: 's1', when: 'always', enabled: true, mode: 'model session' },
    { id: 'b', title: 'B', kind: 'note', summary: 's2', when: 'never', enabled: false, mode: 'model session' },
  ];
  const { sb, panel, urls } = load(okBody({ enabled: true, tasks }));
  await run(sb);
  const out = html(panel);
  assert.ok(urls.some(u => u.includes('/api/autonomous/standard-tasks')));
  assert.equal((out.match(/<tbody>[\s\S]*<\/tbody>/)[0].match(/<tr>/g) || []).length, 2);
  for (const col of ['Title', 'Kind', 'Summary', 'When it is offered', 'On or off', 'Mode']) assert.ok(out.includes('>' + col + '<'), col);
  assert.ok(out.includes('>Off<') && out.includes('>On<'));
});

test('every value is escaped', async () => {
  const bad = '<img src=x onerror=alert(1)>';
  const { sb, panel } = load(okBody({ tasks: [{ title: bad, kind: bad, summary: bad, when: bad, enabled: true, mode: bad }] }));
  await run(sb);
  const out = html(panel);
  assert.ok(!out.includes(bad));
  assert.ok(out.includes('&lt;img'));
});

test('the fixed-mode note and the how-to link render', async () => {
  const { sb, panel } = load(okBody({ tasks: [] }));
  await run(sb);
  const out = html(panel);
  assert.ok(out.includes('fixed to model session'));
  assert.ok(out.includes('autonomous.task_templates'));
  assert.ok(out.includes(encodeURIComponent('docs/howto/automata-standard-tasks.md')));
});

test('a load failure shows the error text', async () => {
  const { sb, panel } = load(() => Promise.reject(new Error('boom-stubbed')));
  await run(sb);
  const out = html(panel);
  assert.ok(out.includes('Load failed:') && out.includes('boom-stubbed'));
});
