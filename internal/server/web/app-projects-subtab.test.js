// Projects as the first Automata sub-tab — placement, order, routes, timers, label.
//
// Projects used to be a top-level view with its own nav button. It is now the
// first sub-tab of Automata (Projects, Automation, Automata, Events,
// Templates); a viewer with no choice still opens on Automation. This test
// covers: no Projects nav button, the sub-tab order (list and rendered
// buttons), every old route landing on the sub-tab, timers and late replies
// after leaving the sub-tab, and the nav_projects label in all five locales.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP = path.join(__dirname, 'app.js');
const ORDER = ['projects', 'automation', 'automata', 'events', 'templates'];
const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];

function locale(code) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', code + '.json'), 'utf8'));
}

// load builds the app with English strings. `hold` makes /api/projects wait
// until release() so a reply can arrive after the viewer has left the sub-tab.
function load({ hash = '', projects = [{ name: 'acme', dir: '/work/acme' }], hold = false } = {}) {
  const out = { view: '' };
  const view = makeStubElement(h => { out.view = h; });
  view.insertAdjacentHTML = (pos, h) => { out.view = h + out.view; };
  const sandbox = buildSandbox({ byId: { automataPanel: view, view }, initialHash: hash });
  const held = [];
  sandbox.fetch = (url) => {
    const body = String(url).includes('/api/projects') ? projects : [];
    const res = { ok: true, status: 200, statusText: '', json: () => Promise.resolve(body), text: () => Promise.resolve(JSON.stringify(body)) };
    if (hold && String(url).includes('/api/projects')) return new Promise(r => held.push(() => r(res)));
    return Promise.resolve(res);
  };
  vm.createContext(sandbox);
  loadScript(APP, sandbox);
  sandbox._i18n.bundle = locale('en');
  return { sb: sandbox, out, view, release: () => held.splice(0).forEach(f => f()) };
}

const run = (sb, code) => vm.runInContext(code, sb);

test('the main navigation markup has no Projects button', () => {
  const html = fs.readFileSync(path.join(__dirname, 'index.html'), 'utf8');
  assert.ok(html.indexOf('id="navBtnProjects"') < 0);
  assert.ok(!/data-view="projects"/.test(html), 'no nav button targets the projects view');
  assert.ok(!/navigate\('projects'\)/.test(html), 'no nav button opens the projects view');
  assert.ok(!/data-i18n="nav_projects"/.test(html), 'the nav carries no Projects label');
});

test('the sub-tab list and the rendered buttons are Projects, Automation, Automata, Events, Templates; the default is Automation', () => {
  const { sb } = load();
  assert.deepEqual(run(sb, 'JSON.stringify(_AUTOMATA_TABS)'), JSON.stringify(ORDER));
  assert.equal(run(sb, '_AUTOMATA_DEFAULT_TAB'), 'automation');
  assert.equal(run(sb, '_automataState.tab'), 'automation', 'a viewer with no choice opens on Automation');
  assert.equal(run(sb, '_automataInitialTab()'), 'automation');

  // The rendered strip: the buttons' data-tab values in document order.
  const src = fs.readFileSync(APP, 'utf8');
  const strip = src.match(/<div class="output-tabs automata-tabs-bar">([\s\S]*?)<div style="flex:1;"><\/div>/);
  assert.ok(strip, 'the Automata tab strip template exists');
  const tabs = [...strip[1].matchAll(/data-tab="([a-z]+)"/g)].map(m => m[1]);
  assert.deepEqual(tabs, ORDER);
});

test('navigate("projects") lands on the Projects sub-tab of the Automata view', () => {
  const { sb } = load();
  sb.navigate('projects');
  assert.equal(run(sb, 'state.activeView'), 'autonomous');
  assert.equal(run(sb, '_automataState.tab'), 'projects');
  assert.equal(sb._projectsActive(), true);
});

test('a #projects hash restores into the Projects sub-tab', () => {
  const { sb } = load({ hash: '#projects' });
  assert.equal(sb._projectsHashParse(sb._projectsCurrentHash()), '');
  const src = fs.readFileSync(APP, 'utf8');
  assert.match(src, /_projectsHashParse\(_projectsCurrentHash\(\)\) !== null\) _initView = 'projects';/);
  sb.navigate('projects');
  assert.equal(run(sb, '_automataState.tab'), 'projects');
  assert.equal(run(sb, 'state.activeView'), 'autonomous');
});

test('a #projects/<name> hash keeps the project name on the sub-tab', async () => {
  const projects = [{ name: 'acme', dir: '/work/acme' }, { name: 'zeta', dir: '/work/zeta' }];
  const { sb, out } = load({ hash: '#projects/zeta', projects });
  assert.equal(sb._projectsHashParse('#projects/zeta'), 'zeta');
  sb.navigate('projects');
  await flushAsync();
  assert.equal(run(sb, '_automataState.tab'), 'projects');
  assert.match(out.view, /<option value="zeta" selected>zeta<\/option>/);
  assert.match(out.view, /data-project="zeta"/);
});

test('the Projects sub-tab button and a link to it switch the sub-tab, not the view', () => {
  const { sb } = load();
  run(sb, "state.activeView = 'autonomous'; _automataState.tab = 'automata'");
  sb.switchAutomataTab('projects');
  assert.equal(run(sb, '_automataState.tab'), 'projects');
  assert.equal(run(sb, 'state.activeView'), 'autonomous');
  // The project picker and chips only ever select a project inside the sub-tab.
  sb.selectProjectsView('acme');
  assert.equal(run(sb, 'state.activeView'), 'autonomous');
  assert.equal(run(sb, '_automataState.tab'), 'projects');
});

test('leaving the sub-tab clears every timer the Projects code started', async () => {
  const { sb, out } = load();
  const live = new Set();
  let next = 1;
  sb.setInterval = () => { const id = next++; live.add(id); return id; };
  sb.clearInterval = (id) => { live.delete(id); };
  sb.setTimeout = () => 0;
  sb.clearTimeout = () => {};
  run(sb, "state.activeView = 'autonomous'; _automataState.tab = 'automata'");
  sb.switchAutomataTab('projects');
  await flushAsync();
  assert.equal(sb._projectsActive(), true);
  // Whatever the Projects code and its polling helpers started is now running.
  // A stand-in poller started while Projects is open must also be ended.
  sb.window._prdCapacityInterval = sb.setInterval();
  const started = new Set(live);
  sb.switchAutomataTab('templates');
  await flushAsync();
  assert.equal(sb._projectsActive(), false);
  for (const id of started) assert.ok(!live.has(id), 'timer ' + id + ' was cleared on leaving Projects');
  assert.equal(live.size, 0, 'no interval is left running');
  assert.ok(!sb._prdCapacityInterval, 'no capacity timer is left');
});

test('a reply that arrives after leaving the sub-tab does not repaint', async () => {
  const { sb, release } = load({ hold: true });
  run(sb, "state.activeView = 'autonomous'; _automataState.tab = 'automata'");
  sb.switchAutomataTab('projects');
  await flushAsync();
  run(sb, 'globalThis.__paints = 0; const __orig = _projectsPaint; _projectsPaint = function () { globalThis.__paints++; return __orig.apply(this, arguments); }');
  sb.switchAutomataTab('templates');
  await flushAsync();
  const before = run(sb, 'globalThis.__paints');
  const listBefore = run(sb, '_projectsState.list');
  release();
  await flushAsync(30);
  assert.equal(run(sb, 'globalThis.__paints'), before, 'no repaint after the late reply');
  assert.equal(run(sb, '_projectsState.list'), listBefore, 'the late reply is dropped');
});

test('the sub-tab label is the existing nav_projects string in all five locales', () => {
  const { sb } = load();
  const expected = { en: 'Projects', de: 'Projekte', es: 'Proyectos', fr: 'Projets', ja: 'プロジェクト' };
  for (const code of LOCALES) {
    const bundle = locale(code);
    assert.equal(bundle.nav_projects, expected[code], code + ' keeps its nav_projects string');
    sb._i18n.bundle = bundle;
    run(sb, "_automataState.tab = 'projects'");
    assert.equal(sb._automataHeaderTitle(), expected[code], code + ' header uses nav_projects');
  }
  const src = fs.readFileSync(APP, 'utf8');
  assert.match(src, /data-tab="projects"[^>]*>\$\{escHtml\(t\('nav_projects'\)\)\}<\/button>/);
});
