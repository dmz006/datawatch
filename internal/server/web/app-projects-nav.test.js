// Projects top-level tab — navigation, picker and remembered choice.
//
// The Projects tab is a top-level nav button. Its picker lists Overview first
// (the cross-project in-flight view) and then each project from GET
// /api/projects. The last choice is remembered in localStorage inside
// try/catch, and mirrored in the #projects hash. With no projects the tab
// points at Settings. This test covers the nav markup, the route, the hash
// round trip, the storage guards, the picker order, a project card, the
// no-projects message and the escaped load failure.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP = path.join(__dirname, 'app.js');

function bindBundle(sb) {
  // t() falls back to the raw key until a bundle loads; install English so
  // the rendered strings are the real user-visible ones.
  sb._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
}

// loadView builds the sandbox with a #view element that records what the
// tab draws. apiFetch answers /api/projects with `projects`, and the rest of
// the page (the Overview body) with an empty list.
function loadView({ projects = [], fail = false, hash = '' } = {}) {
  const out = { view: '' };
  const view = makeStubElement(h => { out.view = h; });
  view.insertAdjacentHTML = (pos, h) => { out.view = h + out.view; };
  const sandbox = buildSandbox({ byId: { automataPanel: view, view }, initialHash: hash });
  sandbox.fetch = (url) => {
    if (String(url).includes('/api/projects')) {
      if (fail) return Promise.resolve({ ok: false, status: 500, statusText: 'x', text: () => Promise.resolve('<b>down</b>'), json: () => Promise.reject(new Error('x')) });
      return Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(projects), text: () => Promise.resolve(JSON.stringify(projects)) });
    }
    return Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve([]), text: () => Promise.resolve('[]') });
  };
  vm.createContext(sandbox);
  loadScript(APP, sandbox);
  bindBundle(sandbox);
  // The Projects tab is the active view: a load only repaints while it is.
  vm.runInContext("state.activeView = 'autonomous'; _automataState.tab = 'projects'", sandbox);
  return { sb: sandbox, out, view };
}

const PROJECTS = [
  { name: 'acme', dir: '/work/acme', default_backend: 'claude-code', description: 'Acme <site>' },
  { name: 'zeta', dir: '/work/zeta' },
];

test('index.html has no top-level Projects nav button; Projects is the first Automata sub-tab', () => {
  const html = fs.readFileSync(path.join(__dirname, 'index.html'), 'utf8');
  assert.ok(html.indexOf('id="navBtnProjects"') < 0);
  const js = fs.readFileSync(APP, 'utf8');
  assert.match(js, /const _AUTOMATA_TABS = \['projects', 'automation', 'automata', 'events', 'templates'\];/);
  const { sb } = loadView();
  assert.equal(typeof sb.renderProjectsView, 'function');
  assert.equal(typeof sb.selectProjectsView, 'function');
  assert.equal(sb._projectsActive(), true);
});

test('the #projects hash round-trips the Overview and a project name', () => {
  const { sb } = loadView();
  assert.equal(sb._projectsHashBuild(''), '#projects');
  assert.equal(sb._projectsHashBuild('acme'), '#projects/acme');
  assert.equal(sb._projectsHashBuild('a b/c'), '#projects/a%20b%2Fc');
  assert.equal(sb._projectsHashParse('#projects'), '');
  assert.equal(sb._projectsHashParse('#projects/acme'), 'acme');
  assert.equal(sb._projectsHashParse('#projects/a%20b%2Fc'), 'a b/c');
  assert.equal(sb._projectsHashParse('#automata'), null, 'another route is not a projects route');
  assert.equal(sb._projectsHashParse('#projects/%E0%A4%A'), null, 'a bad escape is not a route');
});

test('the remembered choice is read and written inside try/catch', () => {
  const { sb } = loadView();
  sb._projectsRememberLast('acme');
  assert.equal(sb.localStorage.getItem('cs_projects_last'), 'acme');
  assert.equal(sb._projectsRecallLast(), 'acme');
  // Blocked storage (private window): neither call throws, and the choice reads as Overview.
  const blocked = () => { throw new Error('denied'); };
  sb.localStorage.setItem = blocked;
  sb.localStorage.getItem = blocked;
  assert.doesNotThrow(() => sb._projectsRememberLast('zeta'));
  assert.equal(sb._projectsRecallLast(), '');
});

test('the picker lists Overview first, then the projects, and selects the remembered one', async () => {
  const { sb, out } = loadView({ projects: PROJECTS });
  sb.localStorage.setItem('cs_projects_last', 'zeta');
  sb.renderProjectsView();
  await flushAsync();
  // Only the picker's own options: the Gantt window selector has options too.
  const picker = out.view.match(/<select id="projectsPicker"[\s\S]*?<\/select>/)[0];
  const options = [...picker.matchAll(/<option value="([^"]*)"\s*(selected)?\s*>/g)].map(m => [m[1], !!m[2]]);
  assert.deepEqual(options, [['', false], ['acme', false], ['zeta', true]]);
  assert.match(out.view, /id="projectsPicker"/);
  assert.match(out.view, /data-project="zeta"/, 'the remembered project is the body');
});

test('with no remembered choice the picker opens on Overview', async () => {
  const { sb, out } = loadView({ projects: PROJECTS });
  sb.renderProjectsView();
  await flushAsync();
  assert.match(out.view, /<option value="" selected>Overview<\/option>/);
});

test('a project named in the hash wins over the remembered choice', async () => {
  const { sb, out } = loadView({ projects: PROJECTS, hash: '#projects/acme' });
  sb.localStorage.setItem('cs_projects_last', 'zeta');
  sb.renderProjectsView();
  await flushAsync();
  assert.match(out.view, /<option value="acme" selected>acme<\/option>/);
  assert.match(out.view, /class="project-detail-card" data-project="acme"/);
  assert.match(out.view, /\/work\/acme/);
  assert.match(out.view, /Acme &lt;site&gt;/, 'the description is escaped');
});

test('a project that is not listed falls back to Overview', async () => {
  const { sb, out } = loadView({ projects: PROJECTS, hash: '#projects/ghost' });
  sb.renderProjectsView();
  await flushAsync();
  assert.match(out.view, /<option value="" selected>Overview<\/option>/);
});

test('selecting a project remembers it and mirrors it in the hash without a new history entry', async () => {
  const { sb, out } = loadView({ projects: PROJECTS });
  const calls = [];
  sb.history.replaceState = (...args) => calls.push(args);
  sb.history.pushState = () => calls.push(['push']);
  sb.renderProjectsView();
  await flushAsync();
  sb.selectProjectsView('acme');
  assert.equal(sb.localStorage.getItem('cs_projects_last'), 'acme');
  assert.deepEqual(calls.map(c => c[2]), ['/#projects/acme']);
  assert.ok(!calls.some(c => c[0] === 'push'), 'no history entry is pushed');
  assert.match(out.view, /data-project="acme"/);
  // Back to Overview: the choice is cleared and the hash returns to #projects.
  sb.selectProjectsView('');
  assert.equal(sb.localStorage.getItem('cs_projects_last'), '');
  assert.equal(calls[calls.length - 1][2], '/#projects');
  assert.match(out.view, /id="automationTriageCard"|data-tab="automata"/);
});

test('a blocked history.replaceState does not stop the choice from applying', async () => {
  const { sb, out } = loadView({ projects: PROJECTS });
  sb.history.replaceState = () => { throw new Error('blocked'); };
  sb.renderProjectsView();
  await flushAsync();
  assert.doesNotThrow(() => sb.selectProjectsView('zeta'));
  assert.match(out.view, /data-project="zeta"/);
});

test('with no projects the tab points at Settings and shows no picker', async () => {
  const { sb, out } = loadView({ projects: [] });
  sb.renderProjectsView();
  await flushAsync();
  assert.match(out.view, /id="projectsNone"/);
  assert.match(out.view, /No projects yet\. Add one in Settings, then pick it here\./);
  assert.match(out.view, /onclick="navigate\('settings'\)"/);
  assert.doesNotMatch(out.view, /id="projectsPicker"/);
});

test('a failed load shows the escaped error and still offers Overview', async () => {
  const { sb, out } = loadView({ fail: true });
  sb.renderProjectsView();
  await flushAsync();
  assert.match(out.view, /Could not load projects:/);
  assert.ok(!out.view.includes('<b>down</b>'), 'the raw error body is not drawn');
  assert.match(out.view, /id="projectsPicker"/);
  assert.match(out.view, /<option value="" selected>Overview<\/option>/);
});
