// Projects page — manage controls: add, edit and remove project records.
//
// Writes go to POST /api/projects (the whole entry) and DELETE
// /api/projects/<name>. The empty list shows the add form, not an Open Settings
// button. Removing asks first and never touches a session.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP = path.join(__dirname, 'app.js');
const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];

const ACME = {
  name: 'acme', dir: '/work/acme', description: 'old', default_backend: 'claude-code',
  policy: { max_concurrent_tasks: 3 }, arcs: [{ id: 'arc-a', title: 'First arc' }],
};

// load builds the app on the Projects tab with English strings. projects is the
// live list the stub serves; options.reply overrides a write's response, and
// options.confirm sets the answer to the remove confirm.
function load(projects, options = {}) {
  const view = makeStubElement(() => {});
  // Overview paints the picker (with the manage block) via insertAdjacentHTML.
  view.__ins = [];
  view.insertAdjacentHTML = (_pos, h) => { view.__ins.push(h); };
  Object.defineProperty(view, 'page', { get() { return (view.__ins.length ? view.__ins[view.__ins.length - 1] : '') + view.innerHTML; } });
  const sandbox = buildSandbox({ byId: { automataPanel: view, view } });
  sandbox.__calls = [];
  sandbox.__projects = projects.slice();
  sandbox.__confirms = [];
  sandbox.confirm = (msg) => { sandbox.__confirms.push(msg); return options.confirm !== false; };
  sandbox.fetch = (url, opts = {}) => {
    const u = String(url);
    const method = (opts.method || 'GET').toUpperCase();
    sandbox.__calls.push({ url: u, method, body: opts.body ? JSON.parse(opts.body) : undefined });
    const ok = body => Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(body), text: () => Promise.resolve(JSON.stringify(body)) });
    if (method !== 'GET' && options.fail) {
      return Promise.resolve({ ok: false, status: 500, statusText: 'err', json: () => Promise.resolve({}), text: () => Promise.resolve(options.fail) });
    }
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

function fill(sb, fields) {
  for (const k of Object.keys(fields)) sb.projectsManageInput(k, fields[k]);
}

const writes = sb => sb.__calls.filter(c => c.method !== 'GET');
const T = (key) => JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'))[key];

test('the empty list renders the add form and no Open Settings button', async () => {
  const { sb, view } = load([]);
  await open(sb);
  const html = view.page;
  assert.ok(html.includes('id="projectsManageForm"'), 'add form shown');
  assert.ok(html.includes('data-mode="add"'), 'form is in add mode');
  assert.ok(html.includes('id="projectsManageName"') && html.includes('id="projectsManageDir"'), 'name and directory fields shown');
  assert.ok(!/_projectsOpenSettings|openSettings/i.test(html), 'no Open Settings control');
  assert.ok(!/Open settings/i.test(html), 'no Open settings label');
});

test('adding a project posts name, dir, description and default_backend, then reloads the list', async () => {
  const { sb } = load([]);
  await open(sb);
  fill(sb, { name: ' newproj ', dir: '/work/new', description: 'a thing', default_backend: 'pi' });
  await sb.projectsManageSave();
  await flushAsync();
  const w = writes(sb);
  assert.equal(w.length, 1);
  assert.equal(w[0].url, '/api/projects');
  assert.equal(w[0].method, 'POST');
  assert.equal(w[0].body.name, 'newproj');
  assert.equal(w[0].body.dir, '/work/new');
  assert.equal(w[0].body.description, 'a thing');
  assert.equal(w[0].body.default_backend, 'pi');
  const idx = sb.__calls.indexOf(w[0]);
  assert.ok(sb.__calls.slice(idx + 1).some(c => c.method === 'GET' && c.url === '/api/projects'), 'list reloaded after the write');
  const list = vm.runInContext('_projectsState.list', sb);
  assert.ok(list.some(p => p.name === 'newproj'), 'reloaded list holds the new project');
});

test('editing posts the whole entry with policy and arcs preserved and the name unchanged', async () => {
  const { sb } = load([ACME]);
  await open(sb);
  sb.projectsManageStartEdit('acme');
  fill(sb, { name: 'renamed-attempt', dir: '/work/acme2', description: 'new desc', default_backend: 'pi' });
  await sb.projectsManageSave();
  await flushAsync();
  const w = writes(sb);
  assert.equal(w.length, 1);
  assert.equal(w[0].method, 'POST');
  assert.equal(w[0].url, '/api/projects');
  assert.equal(w[0].body.name, 'acme', 'name unchanged');
  assert.equal(w[0].body.dir, '/work/acme2');
  assert.equal(w[0].body.description, 'new desc');
  assert.equal(w[0].body.default_backend, 'pi');
  assert.deepEqual(w[0].body.policy, ACME.policy, 'policy preserved');
  assert.deepEqual(w[0].body.arcs, ACME.arcs, 'arcs preserved');
});

test('the name field is read-only while editing', async () => {
  const { sb, view } = load([ACME]);
  await open(sb);
  sb.projectsManageStartEdit('acme');
  assert.ok(/id="projectsManageName"[^>]*readonly/.test(view.page), 'name input is readonly');
  assert.ok(view.page.includes('data-mode="edit"'));
});

test('removing calls DELETE with the encoded name once confirmed', async () => {
  const name = 'my proj/α';
  const { sb } = load([{ name, dir: '/x' }], { confirm: true });
  await open(sb);
  await sb.projectsManageRemove(name);
  await flushAsync();
  const w = writes(sb);
  assert.equal(w.length, 1, 'exactly one write');
  assert.equal(w[0].method, 'DELETE');
  assert.equal(w[0].url, '/api/projects/' + encodeURIComponent(name));
  assert.equal(sb.__confirms.length, 1, 'confirm asked once');
  assert.ok(sb.__confirms[0].includes(name), 'confirm names the project');
});

test('removing sends nothing when the confirm is declined', async () => {
  const { sb } = load([ACME], { confirm: false });
  await open(sb);
  await sb.projectsManageRemove('acme');
  await flushAsync();
  assert.equal(sb.__confirms.length, 1);
  assert.equal(writes(sb).length, 0, 'no request sent');
});

test('an empty name or directory sends no request and shows the error', async () => {
  const { sb, view } = load([]);
  await open(sb);
  fill(sb, { name: '   ', dir: '/work/x' });
  await sb.projectsManageSave();
  assert.equal(writes(sb).length, 0);
  assert.ok(view.page.includes('id="projectsManageError"'));
  assert.ok(view.page.includes(T('projects_manage_err_name')));
  fill(sb, { name: 'x', dir: '  ' });
  await sb.projectsManageSave();
  assert.equal(writes(sb).length, 0);
  assert.ok(view.page.includes(T('projects_manage_err_dir')));
});

test('a server error is shown escaped', async () => {
  const evil = '<img src=x onerror=alert(1)>';
  const { sb, view } = load([], { fail: evil });
  await open(sb);
  fill(sb, { name: 'p', dir: '/work/p' });
  await sb.projectsManageSave();
  await flushAsync();
  const html = view.page;
  assert.ok(html.includes('id="projectsManageError"'), 'error shown');
  assert.ok(!html.includes(evil), 'raw markup not present');
  assert.ok(html.includes('&lt;img src=x onerror=alert(1)&gt;'), 'markup escaped');
});

test('a failed remove shows its error escaped', async () => {
  const evil = '<script>alert(1)</script>';
  const { sb, view } = load([ACME], { fail: evil });
  await open(sb);
  await sb.projectsManageRemove('acme');
  await flushAsync();
  assert.ok(!view.page.includes(evil));
  assert.ok(view.page.includes('&lt;script&gt;'));
});

test('no request is ever sent to a session kill or stop route', async () => {
  const { sb } = load([ACME]);
  await open(sb);
  sb.projectsManageStartEdit('acme');
  fill(sb, { dir: '/work/acme3' });
  await sb.projectsManageSave();
  await flushAsync();
  await sb.projectsManageRemove('acme');
  await flushAsync();
  fill(sb, { name: 'n2', dir: '/d' });
  sb.projectsManageCancel();
  for (const c of sb.__calls) {
    assert.ok(!/kill|stop|signal|\/sessions/i.test(c.url), 'no session route: ' + c.url);
    if (c.method !== 'GET') assert.ok(c.url.startsWith('/api/projects'), 'writes only to project routes: ' + c.url);
  }
});

test('every projects_manage_* key exists in all five locale files', () => {
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  const keys = Object.keys(en).filter(k => k.startsWith('projects_manage_'));
  assert.ok(keys.length > 0, 'en.json has projects_manage_ keys');
  // Keys the code uses must be among them.
  const src = fs.readFileSync(APP, 'utf8');
  const used = new Set((src.match(/projects_manage_[a-z_]+/g) || []));
  for (const k of used) assert.ok(keys.includes(k), 'en.json has used key ' + k);
  for (const loc of LOCALES) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', loc + '.json'), 'utf8'));
    for (const k of keys) {
      assert.ok(Object.prototype.hasOwnProperty.call(bundle, k), loc + '.json is missing ' + k);
      assert.ok(String(bundle[k]).length > 0, loc + '.json has an empty ' + k);
    }
  }
});
