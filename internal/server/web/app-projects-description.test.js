// Projects page — description: saving a description must survive the round-trip.
//
// Add posts name/dir/description/default_backend to POST /api/projects;
// edit keeps the saved description in the form draft and posts it back with
// the untouched policy and arcs; the manage list and the project page both
// display the description.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP = path.join(__dirname, 'app.js');

const ACME = {
  name: 'acme', dir: '/work/acme', description: 'the billing service of the Automata',
  default_backend: 'claude-code',
  policy: { max_concurrent_tasks: 3 }, arcs: [{ id: 'arc-a', title: 'First arc' }],
};

function load(projects, options = {}) {
  const view = makeStubElement(() => {});
  view.__ins = [];
  view.insertAdjacentHTML = (_pos, h) => { view.__ins.push(h); };
  Object.defineProperty(view, 'page', { get() { return (view.__ins.length ? view.__ins[view.__ins.length - 1] : '') + view.innerHTML; } });
  const sandbox = buildSandbox({ byId: { automataPanel: view, view } });
  sandbox.__calls = [];
  sandbox.__projects = projects.map(p => Object.assign({}, p));
  sandbox.confirm = () => options.confirm !== false;
  sandbox.fetch = (url, opts = {}) => {
    const u = String(url);
    const method = (opts.method || 'GET').toUpperCase();
    sandbox.__calls.push({ url: u, method, body: opts.body ? JSON.parse(opts.body) : undefined });
    const ok = body => Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(body), text: () => Promise.resolve(JSON.stringify(body)) });
    if (method === 'POST' && u === '/api/projects') {
      const b = JSON.parse(opts.body);
      sandbox.__projects = sandbox.__projects.filter(p => p.name !== b.name).concat([b]);
      return ok({ ok: true });
    }
    if (method === 'DELETE' && u.startsWith('/api/projects/')) {
      const n = decodeURIComponent(u.slice('/api/projects/'.length));
      sandbox.__projects = sandbox.__projects.filter(p => p.name !== n);
      return ok({ ok: true });
    }
    if (u === '/api/projects') return ok(sandbox.__projects);
    if (u.includes('/history')) return ok({ items: [], gantt: [] });
    return ok({});
  };
  vm.createContext(sandbox);
  loadScript(APP, sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  vm.runInContext("state.activeView = 'autonomous'; _automataState.tab = 'projects'", sandbox);
  return { sb: sandbox, view };
}

async function open(sb) {
  await sb._projectsLoad();
  sb._projectsPaint();
  await flushAsync();
}

const writes = sb => sb.__calls.filter(c => c.method !== 'GET');

test('adding a project posts the description and the list shows it', async () => {
  const { sb, view } = load([]);
  await open(sb);
  sb.projectsManageInput('name', 'acme');
  sb.projectsManageInput('dir', '/work/acme');
  sb.projectsManageInput('description', 'the billing service of the Automata');
  sb.projectsManageInput('default_backend', 'claude-code');
  await sb.projectsManageSave();
  await flushAsync();

  const posts = writes(sb).filter(c => c.url === '/api/projects' && c.body && c.body.name === 'acme');
  assert.equal(posts.length, 1, 'one POST /api/projects for the new project');
  assert.equal(posts[0].body.description, 'the billing service of the Automata', 'description is posted, not dropped');
  assert.equal(posts[0].body.default_backend, 'claude-code');

  const html = view.page;
  assert.ok(html.includes('the billing service of the Automata'), 'saved description is displayed in the manage list');
});

test('editing another field keeps the saved description in the posted body', async () => {
  const { sb, view } = load([ACME]);
  await open(sb);

  sb.projectsManageStartEdit('acme');
  const draft = vm.runInContext('_projectsManage.draft', sb);
  assert.equal(draft.description, 'the billing service of the Automata', 'edit form pre-fills the saved description');

  sb.projectsManageInput('dir', '/work/acme-v2');
  await sb.projectsManageSave();
  await flushAsync();

  const posts = writes(sb).filter(c => c.url === '/api/projects' && c.body && c.body.name === 'acme');
  assert.equal(posts.length, 1, 'one POST for the edit');
  const body = posts[0].body;
  assert.equal(body.dir, '/work/acme-v2', 'the edited field is posted');
  assert.equal(body.description, 'the billing service of the Automata', 'the saved description is posted back unchanged, not blanked');
  assert.equal(body.policy.max_concurrent_tasks, 3, 'policy is carried over');
  assert.equal(body.arcs.length, 1, 'arcs are carried over');
  assert.equal(body.default_backend, 'claude-code', 'the saved default_backend is posted back unchanged, not blanked');
});

test('editing the backend keeps the saved description in the posted body', async () => {
  const { sb } = load([ACME]);
  await open(sb);

  sb.projectsManageStartEdit('acme');
  const draft = vm.runInContext('_projectsManage.draft', sb);
  assert.equal(draft.default_backend, 'claude-code', 'edit form pre-fills the saved backend');

  sb.projectsManageInput('default_backend', 'pi');
  await sb.projectsManageSave();
  await flushAsync();

  const posts = writes(sb).filter(c => c.url === '/api/projects' && c.body && c.body.name === 'acme');
  assert.equal(posts.length, 1, 'one POST for the edit');
  const body = posts[0].body;
  assert.equal(body.default_backend, 'pi', 'the edited backend is posted');
  assert.equal(body.description, 'the billing service of the Automata', 'the saved description is posted back unchanged, not blanked by a backend edit');
});

test('the project page displays the saved description', async () => {
  const { sb, view } = load([ACME]);
  await open(sb);
  vm.runInContext("_projectsState.selected = 'acme'", sb);
  sb._projectsPaint();
  await flushAsync();
  const html = view.page;
  assert.ok(html.includes('the billing service of the Automata'), 'project page shows the description');
});
