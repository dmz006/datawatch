// Projects page — roll-up section and paged history.
//
// The project page shows a Progress block above the arcs: a whole-project
// progress bar, the cost in usage points (never dollars), and the throughput
// over the window from GET /api/projects/{name}/rollup. Each arc shows its own
// progress bar. Below the page, a paged History list reads
// GET /api/projects/{name}/history, one page at a time, with Load more while
// the server reports next_cursor. Overview reads neither.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP = path.join(__dirname, 'app.js');

// loadView wires the Projects view to stubbed endpoints. history is an array of
// replies, one per history request, in order; the last one repeats.
function loadView({ projects = [], rollup = null, hash = '', history = [], historyFail = false } = {}) {
  const out = { view: '', rollupCalls: [], historyCalls: [] };
  const view = makeStubElement(h => { out.view = h; });
  view.insertAdjacentHTML = (pos, h) => { out.view = h + out.view; };
  const sandbox = buildSandbox({ byId: { view }, initialHash: hash });
  let historyIdx = 0;
  sandbox.fetch = (url) => {
    const u = String(url);
    const ok = body => Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(body), text: () => Promise.resolve(JSON.stringify(body)) });
    if (u.includes('/api/projects/') && u.includes('/rollup')) {
      out.rollupCalls.push(u);
      return ok(rollup || {});
    }
    if (u.includes('/api/projects/') && u.includes('/history')) {
      out.historyCalls.push(u);
      if (historyFail) return Promise.resolve({ ok: false, status: 500, statusText: 'x', text: () => Promise.resolve('boom'), json: () => Promise.reject(new Error('boom')) });
      const body = history.length ? history[Math.min(historyIdx, history.length - 1)] : { items: [] };
      historyIdx++;
      return ok(body);
    }
    if (u.includes('/api/projects')) return ok(projects);
    return ok([]);
  };
  vm.createContext(sandbox);
  loadScript(APP, sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  vm.runInContext("state.activeView = 'projects'", sandbox);
  return { sb: sandbox, out, view };
}

const FULL = {
  name: 'acme', dir: '/work/acme', default_backend: 'claude-code',
  target: { name: 'v2 launch', definition_of_done: 'Checkout ships' },
};

const ROLLUP = {
  project: 'acme', window_days: 7, points_priced: true,
  arcs: [
    { automation_id: 'auto-b', priority: 2, state: 'done', tasks: { done: 3, total: 3 }, points: 1.25,
      throughput: { automata_finished: 1, tasks_finished: 3, automata_per_day: 0.14, tasks_per_day: 0.43 }, automata: [
        { id: 'prd-9', title: 'Ship <cart>', status: 'completed', tasks: { done: 3, total: 3 }, points: 1.25, session_ids: ['sess-aaa-111'] },
      ] },
    { automation_id: 'auto-a', priority: 1, state: 'active', tasks: { done: 1, total: 4 }, points: 0,
      throughput: { automata_finished: 0, tasks_finished: 0, automata_per_day: 0, tasks_per_day: 0 }, automata: [
        { id: 'prd-1', title: 'Login', status: 'running', tasks: { done: 1, total: 4 }, points: 0, session_ids: ['sess-bbb-222'] },
      ] },
  ],
  unassigned: { automata: [] },
  total: {
    tasks: { done: 4, total: 7 }, points: 1.25,
    throughput: { automata_finished: 1, tasks_finished: 3, automata_per_day: 0.14, tasks_per_day: 0.43 },
  },
};

test('the roll-up shows a whole-project bar with done/total and the percent', () => {
  const { sb } = loadView();
  const html = sb._projectPageHTML(FULL, ROLLUP, '', null);
  assert.match(html, /class="project-rollup"/);
  assert.match(html, /aria-label="Whole project" aria-valuemin="0" aria-valuemax="100" aria-valuenow="57"/, '4 of 7 is 57%');
  assert.match(html, /4\/7 tasks · 57%/);
});

test('each arc shows its own progress bar with its task count', () => {
  const { sb } = loadView();
  const html = sb._projectPageHTML(FULL, ROLLUP, '', null);
  assert.match(html, /aria-label="auto-a" aria-valuemin="0" aria-valuemax="100" aria-valuenow="25"/);
  assert.match(html, /1\/4 tasks · 25%/);
  assert.match(html, /aria-label="auto-b" aria-valuemin="0" aria-valuemax="100" aria-valuenow="100"/);
});

test('an empty total reads as 0%, never NaN', () => {
  const { sb } = loadView();
  const roll = { arcs: [], unassigned: { automata: [] }, total: { tasks: { done: 0, total: 0 }, points: 0, throughput: {} } };
  const html = sb._projectPageHTML(FULL, roll, '', null);
  assert.match(html, /0\/0 tasks · 0%/);
  assert.doesNotMatch(html, /NaN/);
});

test('cost is shown in points and never in dollars', () => {
  const { sb } = loadView();
  const html = sb._projectPageHTML(FULL, ROLLUP, '', null);
  assert.match(html, /Cost in points:<\/span> 1\.3 points|Cost in points:<\/span> 1\.25 points/);
  const block = html.slice(html.indexOf('class="project-rollup"'), html.indexOf('id="projectHistory"') > -1 ? html.indexOf('id="projectHistory"') : html.length);
  assert.doesNotMatch(block, /\$/, 'the roll-up block carries no dollar sign');
  assert.doesNotMatch(html, /\$\d/);
});

test('when the roll-up is not priced the points line says so', () => {
  const { sb } = loadView();
  const html = sb._projectPageHTML(FULL, { ...ROLLUP, points_priced: false }, '', null);
  assert.match(html, /Cost in points:<\/span> Not priced: the backends are not on a bundled plan/);
});

test('throughput shows the finished counts and the per-day rates over the window', () => {
  const { sb } = loadView();
  const html = sb._projectPageHTML(FULL, ROLLUP, '', null);
  assert.match(html, /Throughput, last 7 days:<\/span> 1 Automata finished \(0\.14 per day\) · 3 tasks finished \(0\.43 per day\)/);
});

test('a roll-up without a total is summed from its arcs and the unassigned group', () => {
  const { sb } = loadView();
  const roll = {
    window_days: 7, points_priced: true,
    arcs: [
      { automation_id: 'a', priority: 1, tasks: { done: 2, total: 4 }, points: 1, throughput: { automata_finished: 1, tasks_finished: 2, automata_per_day: 0.5, tasks_per_day: 1 }, automata: [] },
      { automation_id: 'b', priority: 2, tasks: { done: 1, total: 2 }, points: 0.5, throughput: { automata_finished: 0, tasks_finished: 1, automata_per_day: 0, tasks_per_day: 0.5 }, automata: [] },
    ],
    unassigned: { tasks: { done: 1, total: 2 }, points: 0.5, throughput: { automata_finished: 0, tasks_finished: 0, automata_per_day: 0, tasks_per_day: 0 }, automata: [] },
  };
  const html = sb._projectPageHTML(FULL, roll, '', null);
  assert.match(html, /4\/8 tasks · 50%/);
  assert.match(html, /Cost in points:<\/span> 2 points/);
  assert.match(html, /Throughput, last 7 days:<\/span> 1 Automata finished \(0\.5 per day\) · 3 tasks finished \(1\.5 per day\)/);
});

test('while the roll-up loads there is no roll-up block', () => {
  const { sb } = loadView();
  const html = sb._projectPageHTML(FULL, null, '', null);
  assert.doesNotMatch(html, /class="project-rollup"/);
  assert.match(html, /Loading arcs…/);
});

test('a history list shows Automata with links and snapshots as progress lines', () => {
  const { sb } = loadView();
  const hist = {
    name: 'acme', error: '', next: '',
    items: [
      { kind: 'automaton', time: '2026-11-08T10:00:00Z', id: 'prd-9', title: 'Ship <cart>', status: 'completed', tasks: { done: 3, total: 3 }, finished_at: '2026-11-08T10:00:00Z' },
      { kind: 'snapshot', time: '2026-11-07T00:00:00Z', taken_at: '2026-11-07T00:00:00Z', progress: { date: '2026-11-07', tasks: 9, tasks_done: 5 } },
      { kind: 'task', time: '2026-11-06T00:00:00Z', id: 'x' },
    ],
  };
  const html = sb._projectPageHTML(FULL, ROLLUP, '', hist);
  assert.match(html, /id="projectHistory"/);
  assert.match(html, /data-kind="automaton" data-automaton="prd-9"/);
  assert.match(html, /Ship &lt;cart&gt;/, 'the Automaton title is escaped');
  assert.match(html, /onclick="_statusOpenAutomaton\(&quot;prd-9&quot;\);return false;"/);
  assert.match(html, /Progress on [^:]+: 5 of 9 tasks done/);
  assert.doesNotMatch(html, /data-kind="task"/, 'other kinds are left out');
  assert.doesNotMatch(html, /projectHistoryMore/, 'no Load more without a next cursor');
});

test('Load more shows while a next cursor is present', () => {
  const { sb } = loadView();
  const hist = { name: 'acme', error: '', next: 'abc', items: [{ kind: 'snapshot', taken_at: '2026-11-07T00:00:00Z', progress: { tasks: 1, tasks_done: 0 } }] };
  const html = sb._projectPageHTML(FULL, ROLLUP, '', hist);
  assert.match(html, /id="projectHistoryMore"/);
  assert.match(html, /onclick="projectsHistoryMore\(\)"/);
});

test('an empty history says so, and a history error is shown escaped', () => {
  const { sb } = loadView();
  const empty = sb._projectPageHTML(FULL, ROLLUP, '', { name: 'acme', error: '', next: '', items: [] });
  assert.match(empty, /Nothing has finished yet\./);
  const failed = sb._projectPageHTML(FULL, ROLLUP, '', { name: 'acme', error: '<i>down</i>', next: '', items: [] });
  assert.match(failed, /Could not load the history: &lt;i&gt;down&lt;\/i&gt;/);
});

test('the history is loading until its first page arrives', () => {
  const { sb } = loadView();
  const html = sb._projectPageHTML(FULL, ROLLUP, '', null);
  assert.match(html, /Loading history…/);
});

test('the Projects view reads the first history page for the selected project', async () => {
  const { sb, out } = loadView({
    projects: [FULL], rollup: ROLLUP, hash: '#projects/acme',
    history: [{ items: [{ kind: 'snapshot', taken_at: '2026-11-07T00:00:00Z', progress: { tasks: 2, tasks_done: 1 } }], next_cursor: 'c1' }],
  });
  sb.renderProjectsView();
  await flushAsync();
  assert.deepEqual(out.historyCalls, ['/api/projects/acme/history?limit=20']);
  assert.match(out.view, /id="projectHistory"/);
  assert.match(out.view, /id="projectHistoryMore"/);
  assert.match(out.view, /class="project-rollup"/);
});

test('Load more requests the next cursor and appends the page', async () => {
  const { sb, out } = loadView({
    projects: [FULL], rollup: ROLLUP, hash: '#projects/acme',
    history: [
      { items: [{ kind: 'snapshot', taken_at: '2026-11-07T00:00:00Z', progress: { tasks: 2, tasks_done: 1 } }], next_cursor: 'c1' },
      { items: [{ kind: 'snapshot', taken_at: '2026-11-06T00:00:00Z', progress: { tasks: 2, tasks_done: 0 } }] },
    ],
  });
  sb.renderProjectsView();
  await flushAsync();
  await sb.projectsHistoryMore();
  await flushAsync();
  assert.equal(out.historyCalls.length, 2);
  assert.equal(out.historyCalls[1], '/api/projects/acme/history?limit=20&cursor=c1');
  const rows = out.view.match(/data-kind="snapshot"/g) || [];
  assert.equal(rows.length, 2, 'both pages are listed');
  assert.doesNotMatch(out.view, /id="projectHistoryMore"/, 'no Load more after the last page');
});

test('Load more does nothing when there is no next cursor', async () => {
  const { sb, out } = loadView({ projects: [FULL], rollup: ROLLUP, hash: '#projects/acme', history: [{ items: [] }] });
  sb.renderProjectsView();
  await flushAsync();
  await sb.projectsHistoryMore();
  assert.equal(out.historyCalls.length, 1);
});

test('a history request that fails shows the error on the page', async () => {
  const { sb, out } = loadView({ projects: [FULL], rollup: ROLLUP, hash: '#projects/acme', historyFail: true });
  sb.renderProjectsView();
  await flushAsync();
  assert.match(out.view, /Could not load the history:/);
});

test('Overview reads no history', async () => {
  const { sb, out } = loadView({ projects: [FULL], rollup: ROLLUP, history: [{ items: [] }] });
  sb.renderProjectsView();
  await flushAsync();
  assert.deepEqual(out.historyCalls, []);
});
