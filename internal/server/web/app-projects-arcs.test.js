// Projects page — header, arcs and Events.
//
// The selected project's page shows its header fields (description, owner,
// repository, default branch, target and definition of done, lane and backend
// defaults, weekly share) and the ordered arcs from GET
// /api/projects/{name}/rollup. Each arc expands to its Automata and sessions;
// rows link to the Automaton view and the session view. A section that is off
// or empty is not shown: what it would have said becomes an Events entry. The
// roll-up is read only for the selected project, never for Overview.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync, pageAllSubtabs } = require('./testutil_browser_stub');

const APP = path.join(__dirname, 'app.js');

function loadView({ projects = [], rollup = null, rollupFail = false, hash = '' } = {}) {
  const out = { view: '', rollupCalls: [] };
  const view = makeStubElement(h => { out.view = h; });
  view.insertAdjacentHTML = (pos, h) => { out.view = h + out.view; };
  const sandbox = buildSandbox({ byId: { automataPanel: view, view }, initialHash: hash });
  sandbox.fetch = (url) => {
    const u = String(url);
    const ok = body => Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(body), text: () => Promise.resolve(JSON.stringify(body)) });
    if (u.includes('/api/projects/') && u.includes('/rollup')) {
      out.rollupCalls.push(u);
      if (rollupFail) return Promise.resolve({ ok: false, status: 500, statusText: 'x', text: () => Promise.resolve('boom'), json: () => Promise.reject(new Error('boom')) });
      return ok(rollup || {});
    }
    if (u.includes('/api/projects')) return ok(projects);
    return ok([]);
  };
  vm.createContext(sandbox);
  loadScript(APP, sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  vm.runInContext("state.activeView = 'autonomous'; _automataState.tab = 'projects'", sandbox);
  return { sb: sandbox, out, view };
}

const FULL = {
  name: 'acme', dir: '/work/acme', default_backend: 'claude-code',
  description: 'Acme <site>', owner: 'platform-team', repository: 'acme/app',
  default_branch: 'trunk', target: { name: 'v2 launch', definition_of_done: 'Checkout ships' },
  lane_defaults: { lane: 'gpu', backend: 'pi-prompt', model: 'qwen3' }, weekly_share: 40,
};

const ROLLUP = {
  project: 'acme', window_days: 7, points_priced: true,
  arcs: [
    { automation_id: 'auto-b', priority: 2, state: 'done', tasks: { done: 3, total: 3 }, points: 1.25, automata: [
      { id: 'prd-9', title: 'Ship <cart>', status: 'completed', tasks: { done: 3, total: 3 }, points: 1.25, session_ids: ['sess-aaa-111'] },
    ] },
    { automation_id: 'auto-a', priority: 1, state: 'active', tasks: { done: 1, total: 4 }, points: 0, automata: [
      { id: 'prd-1', title: 'Login', status: 'running', tasks: { done: 1, total: 4 }, points: 0, session_ids: ['sess-bbb-222', 'sess-ccc-333'] },
    ] },
  ],
  unassigned: { automata: [] },
  total: {},
};

test('the header shows every set field; the description is escaped', () => {
  const { sb } = loadView();
  const html = pageAllSubtabs(sb, FULL, ROLLUP, '');
  assert.match(html, /class="project-detail-card" data-project="acme"/);
  assert.match(html, /Acme &lt;site&gt;/);
  assert.match(html, /Owner:<\/span> platform-team/);
  assert.match(html, /Repository:<\/span> acme\/app/);
  assert.match(html, /Default branch:<\/span> trunk/);
  assert.match(html, /Target:<\/span> v2 launch/);
  assert.match(html, /Definition of done:<\/span> Checkout ships/);
  assert.match(html, /Lane defaults:<\/span> gpu · pi-prompt · qwen3/);
  assert.match(html, /Weekly share:<\/span> 40%/);
  assert.match(html, /Directory:<\/span> \/work\/acme/);
  assert.match(html, /Default backend:<\/span> claude-code/);
  assert.doesNotMatch(html, /is not set\./, 'a complete header has no unset-field Events entries');
});

test('only a missing weekly share becomes an Events entry; other unset fields are just hidden', () => {
  const { sb } = loadView();
  const html = pageAllSubtabs(sb, { name: 'bare', dir: '/w/bare' }, ROLLUP, '');
  assert.doesNotMatch(html, /Owner:<\/span>/);
  assert.doesNotMatch(html, /Weekly share:<\/span>/);
  assert.match(html, /id="projectEvents"/);
  assert.doesNotMatch(html, /Owner is not set\./);
  assert.doesNotMatch(html, /Repository is not set\./);
  assert.doesNotMatch(html, /Description is not set\./);
  assert.match(html, /Weekly share is not set\./);
});

test('a missing weekly share is silent when there is no arc or unassigned Automaton', () => {
  const { sb } = loadView();
  const html = pageAllSubtabs(sb, { name: 'bare', dir: '/w/bare' }, { arcs: [], unassigned: { automata: [] }, total: {} }, '');
  assert.doesNotMatch(html, /Weekly share is not set\./);
});

test('a weekly share of zero is a set value, not an unset one', () => {
  const { sb } = loadView();
  const html = pageAllSubtabs(sb, { ...FULL, weekly_share: 0 }, ROLLUP, '');
  assert.match(html, /Weekly share:<\/span> 0%/);
});

test('arcs are listed in priority order with their state, active arcs open', () => {
  const { sb } = loadView();
  const html = pageAllSubtabs(sb, FULL, ROLLUP, '');
  const first = html.indexOf('data-automation="auto-a"');
  const second = html.indexOf('data-automation="auto-b"');
  assert.ok(first > -1 && second > first, 'priority 1 comes before priority 2');
  assert.match(html, /<details class="project-arc" data-automation="auto-a" open/, 'the active arc is open');
  assert.match(html, /<details class="project-arc" data-automation="auto-b" style/, 'a done arc is closed');
  assert.match(html, /Active<\/span>/);
  assert.match(html, /Done<\/span>/);
});

test('an arc with no state reads as planned', () => {
  const { sb } = loadView();
  const roll = { arcs: [{ automation_id: 'auto-x', priority: 0, automata: [{ id: 'p', status: 's', session_ids: ['s-1'] }] }] };
  const html = pageAllSubtabs(sb, FULL, roll, '');
  assert.match(html, /Planned<\/span>/);
});

test('an Automaton and each session link to the existing views', () => {
  const { sb } = loadView();
  const html = pageAllSubtabs(sb, FULL, ROLLUP, '');
  assert.match(html, /onclick="_statusOpenAutomaton\(&quot;prd-1&quot;\);return false;"/);
  assert.match(html, /onclick="navigate\(&#39;session-detail&#39;,&quot;sess-bbb-222&quot;\);return false;"|onclick="navigate\('session-detail',&quot;sess-bbb-222&quot;\);return false;"/);
  assert.match(html, /data-session="sess-ccc-333"/);
  assert.match(html, /Ship &lt;cart&gt;/, 'the Automaton title is escaped');
});

test('an arc with no Automata is not expanded and its line goes to Events', () => {
  const { sb } = loadView();
  const roll = { arcs: [{ automation_id: 'auto-empty', priority: 1, state: 'planned', automata: [] }] };
  const html = pageAllSubtabs(sb, FULL, roll, '');
  assert.match(html, /data-automation="auto-empty"/);
  assert.doesNotMatch(html, /project-automaton/);
  assert.match(html, /Arc auto-empty has no Automata yet\./);
});

test('an Automaton with no sessions goes to Events, not an empty list', () => {
  const { sb } = loadView();
  const roll = { arcs: [{ automation_id: 'a', priority: 1, automata: [{ id: 'p1', title: 'Solo', session_ids: [] }] }] };
  const html = pageAllSubtabs(sb, FULL, roll, '');
  assert.match(html, /data-automaton="p1"/);
  assert.match(html, /Solo has no sessions yet\./);
  assert.doesNotMatch(html, /project-session-link/);
});

test('no arcs and no unassigned Automata both say so in Events', () => {
  const { sb } = loadView();
  const html = pageAllSubtabs(sb, FULL, { arcs: [], unassigned: { automata: [] } }, '');
  assert.doesNotMatch(html, /Arcs<\/div>/);
  assert.match(html, /No arcs are planned for this project\./);
  assert.match(html, /No unassigned Automata\./);
});

test('unassigned Automata are shown when present', () => {
  const { sb } = loadView();
  const roll = { arcs: [], unassigned: { automata: [{ id: 'loose-1', title: 'Loose', session_ids: ['s-9'] }] } };
  const html = pageAllSubtabs(sb, FULL, roll, '');
  assert.match(html, /Unassigned Automata/);
  assert.match(html, /data-automaton="loose-1"/);
  assert.doesNotMatch(html, /No unassigned Automata\./);
});

test('while the roll-up loads, the page shows a loading line and no arc or Events text', () => {
  const { sb } = loadView();
  const html = pageAllSubtabs(sb, FULL, null, '');
  assert.match(html, /Loading arcs…/);
  assert.doesNotMatch(html, /No arcs are planned/);
  assert.doesNotMatch(html, /id="projectEvents"/);
});

test('a roll-up failure is an Events entry with the escaped error', () => {
  const { sb } = loadView();
  const html = pageAllSubtabs(sb, FULL, null, '<b>down</b>');
  assert.match(html, /Could not load the roll-up: &lt;b&gt;down&lt;\/b&gt;/);
  assert.doesNotMatch(html, /Loading arcs…/);
});

test('the Projects view reads the selected project roll-up and paints the arcs', async () => {
  const { sb, out } = loadView({ projects: [FULL], rollup: ROLLUP, hash: '#projects/acme/arcs' });
  sb.renderProjectsView();
  await flushAsync();
  assert.deepEqual(out.rollupCalls, ['/api/projects/acme/rollup']);
  assert.match(out.view, /data-automation="auto-a"/);
  const ov = loadView({ projects: [FULL], rollup: ROLLUP, hash: '#projects/acme' });
  ov.sb.renderProjectsView();
  await flushAsync();
  assert.match(ov.out.view, /Owner:<\/span> platform-team/, 'the header is on Overview');
});

test('Overview reads no roll-up', async () => {
  const { sb, out } = loadView({ projects: [FULL], rollup: ROLLUP });
  sb.renderProjectsView();
  await flushAsync();
  assert.deepEqual(out.rollupCalls, []);
});

test('a roll-up request that fails paints the failure as an Events entry', async () => {
  const { sb, out } = loadView({ projects: [FULL], rollupFail: true, hash: '#projects/acme/events' });
  sb.renderProjectsView();
  await flushAsync();
  assert.match(out.view, /id="projectEvents"/);
  assert.match(out.view, /Could not load the roll-up:/);
});

test('the without_project group renders after Unassigned; an empty group renders nothing extra', () => {
  const { sb } = loadView();
  const roll = { arcs: [], unassigned: { automata: [{ id: 'loose-1', title: 'Loose' }] },
    without_project: { automata: [{ id: 'orphan-1', title: 'Orphan <x>', status: 'draft' }] } };
  const html = sb._projectPageHTML(FULL, roll, '');
  assert.match(html, /Automata without a project/);
  assert.match(html, /Set a project link from the Automaton detail/);
  assert.match(html, /Orphan &lt;x&gt;/);
  assert.ok(html.indexOf('Unassigned Automata') < html.indexOf('Automata without a project'));
  const empty = sb._projectPageHTML(FULL, { arcs: [], unassigned: { automata: [] }, without_project: { automata: [] } }, '');
  assert.doesNotMatch(empty, /Automata without a project/);
  assert.doesNotMatch(empty, /project-without-project/);
  const absent = sb._projectPageHTML(FULL, { arcs: [], unassigned: { automata: [] } }, '');
  assert.doesNotMatch(absent, /Automata without a project/);
});
