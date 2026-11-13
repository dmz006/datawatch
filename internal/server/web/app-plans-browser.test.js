// Plans browser — the Plans sub-tab of a project page.
//
// Plans are listed in priority order from GET /api/projects/{name}/plans, each
// with its goal, state, progress and points; reorder controls call PUT
// /api/plans/order. Expanding a plan reads its Automata in run order, shows its
// Automations and the findings and registry rows linked to it, and opens a
// Tasks section that pages with Load more. Plans with no Automaton sit in a
// Backlog group. A search box and a state filter narrow the list; which plans
// are expanded is remembered in one localStorage key. Controls create a plan,
// reorder its Automata and attach an Automaton, an Automation or a finding.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-plans-browser.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const HOSTILE = '<img src=x onerror=alert(1)>';
const ESCAPED = '&lt;img src=x onerror=alert(1)&gt;';

const PLAN_A = { title: 'Ship the login flow', id: 'login-flow', uid: 'u1', goal: 'Users sign in with a passkey', state: 'in_progress', order: 1,
  progress: { done: 1, total: 3, percent: 33 }, points: 2.5, automation_ids: ['chain-1'], automaton_ids: ['a1', 'a2', 'a3'], finding_count: 1,
  next: { automaton_id: 'a3', title: 'Third step' } };
const PLAN_B = { title: 'Polish the dashboard', id: 'polish', uid: 'u2', goal: '', state: 'planned', order: 2,
  progress: { done: 0, total: 1, percent: 0 }, points: 0, automation_ids: [], automaton_ids: ['b1'], finding_count: 0 };
const IDEA = { title: 'Idea with no Automaton', id: 'idea', uid: 'u3', goal: 'Think about it', state: 'open', order: 3,
  progress: { done: 0, total: 0, percent: 0 }, points: 0, automation_ids: [], automaton_ids: [], finding_count: 0 };

const AUTOMATA = [
  { title: 'First step', id: 'a1', state: 'completed', run: 'completed', position: 1, completed_at: '2026-11-10T12:30:00Z', points: 1 },
  { title: 'Second step', id: 'a2', state: 'blocked', run: 'blocked', position: 2, points: 0.5, skipped: true },
  { title: 'Third step', id: 'a3', state: 'active', run: 'next', position: 3, points: 1, is_next: true },
];
const BROWSE = [
  { title: 'Search misses aliases', id: 'f1', uid: 'x1', kind: 'finding', project: 'acme', state: 'open', plan_id: 'login-flow', automaton_ids: [] },
  { title: 'Loose finding', id: 'f2', uid: 'x2', kind: 'finding', project: 'acme', state: 'open', automaton_ids: [] },
  { title: 'Passkey registry row', id: 'r1', uid: 'x3', kind: 'registry_row', project: 'acme', state: 'shipped', legacy_number: 412, plan_id: 'login-flow', plan_title: 'Ship the login flow', automaton_ids: [] },
];
const TASKS = Array.from({ length: 30 }, (_, i) => ({ title: 'Task ' + (i + 1), id: 't' + (i + 1), status: 'pending', automaton_id: 'a1', automaton_title: 'First step' }));

function bundle(lang) { return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8')); }

// load builds the app with the Projects tab active. opts.plans and opts.backlog
// are the lists the plans route answers; opts.fail maps a "METHOD path-prefix"
// to an error text.
function load(opts = {}) {
  const view = makeStubElement(() => {});
  view.insertAdjacentHTML = () => {};
  const list = { innerHTML: '' };
  const fields = {};
  const sandbox = buildSandbox({ byId: { automataPanel: view, view, plansBrowserList: list } });
  const realGet = sandbox.document.getElementById.bind(sandbox.document);
  sandbox.document.getElementById = id => (fields[id] || realGet(id));
  sandbox.__calls = [];
  sandbox.fetch = (url, o) => {
    const u = String(url);
    o = o || {};
    const call = { url: u, method: o.method || 'GET', body: o.body ? JSON.parse(o.body) : null };
    const ok = b => Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(b), text: () => Promise.resolve(JSON.stringify(b)) });
    if (u.startsWith('/api/plans') || u.includes('/plans?') || u.startsWith('/api/plan-records/browse')) {
      sandbox.__calls.push(call);
      for (const k of Object.keys(opts.fail || {})) {
        const [m, p] = k.split(' ');
        if (call.method === m && u.startsWith(p)) return Promise.resolve({ ok: false, status: 409, statusText: 'x', text: () => Promise.resolve(opts.fail[k]), json: () => Promise.resolve({}) });
      }
      if (u.includes('/plans?')) {
        const off = Number((u.match(/[?&]offset=(\d+)/) || [])[1] || 0);
        const boff = Number((u.match(/backlog_offset=(\d+)/) || [])[1] || 0);
        const plans = opts.plans || [PLAN_A, PLAN_B];
        const back = opts.backlog || [IDEA];
        return ok({ project: 'acme', plans: plans.slice(off), page: { total: opts.plansTotal || plans.length }, backlog: { plans: back.slice(boff), page: { total: opts.backlogTotal || back.length } } });
      }
      if (u.includes('/automata?')) return ok({ automata: opts.automata || AUTOMATA, page: { total: (opts.automata || AUTOMATA).length } });
      if (u.includes('/tasks?')) {
        const off = Number((u.match(/offset=(\d+)/) || [])[1] || 0);
        return ok({ tasks: TASKS.slice(off, off + 25), page: { total: TASKS.length } });
      }
      if (u.startsWith('/api/plan-records/browse')) {
        const q = decodeURIComponent((u.match(/[?&]q=([^&]*)/) || [])[1] || '').toLowerCase();
        return ok({ records: BROWSE.filter(r => !q || r.title.toLowerCase().includes(q)), page: { total: BROWSE.length } });
      }
      return ok({ attached: true });
    }
    if (u.includes('/api/projects') && !u.includes('/rollup') && !u.includes('/history') && !u.includes('/overview-summary')) return ok([{ name: 'acme', dir: '/work/acme' }]);
    if (u.includes('/history')) return ok({ items: [], gantt: [] });
    return ok({});
  };
  // The harness stubs timers out; keep the pending callbacks so a test can run them.
  sandbox.__timers = [];
  sandbox.setTimeout = fn => { sandbox.__timers.push(fn); return sandbox.__timers.length; };
  sandbox.clearTimeout = id => { if (id) sandbox.__timers[id - 1] = null; };
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  vm.runInContext("state.activeView = 'autonomous'; _automataState.tab = 'projects'", sandbox);
  return { sb: sandbox, list, fields };
}

async function openPlans(sb) {
  await sb._projectsLoad();
  vm.runInContext("_projectsState.selected = 'acme'", sb);
  await sb._plansBrowserLoadSelected();
  await flushAsync();
}
const pr = (sb, expr) => vm.runInContext(expr, sb);
const html = sb => sb.plansBrowserListHTML(pr(sb, '_plansBrowser'));
const calls = sb => Array.from(sb.__calls);
const idx = (h, s) => h.indexOf(s);
// search types into the box, then lets the pause in typing pass.
async function search(sb, text) {
  sb.plansBrowserSearchInput(text);
  const fns = sb.__timers.splice(0).filter(Boolean);
  fns.forEach(fn => fn());
  await flushAsync();
}

test('plans are listed in the order the API gives, each with goal, state, progress and points', async () => {
  const { sb } = load();
  await openPlans(sb);
  const h = html(sb);
  assert.ok(idx(h, 'data-plan="login-flow"') > -1 && idx(h, 'data-plan="polish"') > idx(h, 'data-plan="login-flow"'));
  assert.match(h, /Ship the login flow/);
  assert.match(h, /Users sign in with a passkey/, 'goal');
  assert.match(h, /In progress/, 'state');
  assert.match(h, /1 of 3 Automata · 33%/, 'progress');
  assert.match(h, /aria-valuenow="33"/);
  assert.match(h, /2\.5 points/, 'points');
  assert.match(h, /Next: Third step/, 'next Automaton on the row');
  assert.ok(calls(sb)[0].url.startsWith('/api/projects/acme/plans?limit=50'), 'one paged read: ' + calls(sb)[0].url);
});

test('a plan list that is longer than a page offers Load more and appends the next page', async () => {
  const { sb } = load({ plans: [PLAN_A, PLAN_B], plansTotal: 3 });
  await openPlans(sb);
  assert.match(html(sb), /plans-browser-more/);
  await sb.plansBrowserMore();
  assert.ok(calls(sb).some(c => c.url.includes('offset=2')), 'second page asks for offset 2');
});

test('the Backlog group holds plans with no Automaton and opens on demand', async () => {
  const { sb } = load();
  await openPlans(sb);
  let h = html(sb);
  assert.match(h, /Backlog \(1\)/);
  assert.doesNotMatch(h, /Idea with no Automaton/, 'collapsed by default');
  await sb.plansBrowserToggle('__backlog');
  h = html(sb);
  assert.match(h, /Idea with no Automaton/);
  assert.match(h, /old row number order/);
  assert.ok(idx(h, 'class="plans-browser-backlog"') < idx(h, 'data-plan="idea"'));
  assert.ok(idx(h, 'data-plan="polish"') < idx(h, 'class="plans-browser-backlog"'), 'plans come before the Backlog group');
});

test('expanding a plan reads its Automata and shows Automations, run order, the next marker, findings and rows', async () => {
  const { sb } = load();
  await openPlans(sb);
  await sb.plansBrowserToggle('login-flow');
  await flushAsync();
  assert.ok(calls(sb).some(c => c.url === '/api/plans/login-flow/automata?limit=500'));
  assert.ok(calls(sb).some(c => c.url.startsWith('/api/plan-records/browse?project=acme')), 'linked records come from the summary list');
  const h = html(sb);
  assert.match(h, /Automations/);
  assert.match(h, /data-automation="chain-1"/);
  assert.ok(idx(h, 'data-automaton="a1"') < idx(h, 'data-automaton="a2"') && idx(h, 'data-automaton="a2"') < idx(h, 'data-automaton="a3"'), 'run order');
  assert.match(h, /data-run="completed"/);
  assert.match(h, /data-run="blocked"/);
  assert.match(h, /data-run="next"/);
  assert.match(h, /plans-browser-next-marker/);
  assert.match(h, /completed 2026-11-10 12:30 UTC/);
  assert.match(h, /Search misses aliases/, 'attached finding');
  assert.match(h, /Passkey registry row/, 'linked registry row');
  assert.match(h, /row 412/, 'old row number');
  assert.doesNotMatch(h, /Loose finding<\/div>/, 'an unattached finding is not listed under the plan');
  assert.match(h, /Loose finding<\/option>/, 'but it can be chosen to attach');
});

test('only one plan row carries the next marker and it is the one the API names', async () => {
  const { sb } = load();
  await openPlans(sb);
  await sb.plansBrowserToggle('login-flow');
  const h = html(sb);
  assert.equal((h.match(/plans-browser-next-marker/g) || []).length, 1);
  assert.ok(idx(h, 'plans-browser-next-marker') > idx(h, 'data-automaton="a3"'), 'the marker is on the third Automaton');
});

test('tasks are read only when the Tasks section is opened and page with Load more', async () => {
  const { sb } = load();
  await openPlans(sb);
  await sb.plansBrowserToggle('login-flow');
  assert.ok(!calls(sb).some(c => c.url.includes('/tasks')), 'no task read before the section opens');
  await sb.plansBrowserToggleTasks('login-flow');
  await flushAsync();
  let h = html(sb);
  assert.ok(calls(sb).some(c => c.url === '/api/plans/login-flow/tasks?limit=25&offset=0'));
  assert.match(h, /Task 25 </);
  assert.doesNotMatch(h, /Task 26 </);
  assert.match(h, /plans-browser-tasks-more/);
  await sb.plansBrowserTasksMore('login-flow');
  h = html(sb);
  assert.ok(calls(sb).some(c => c.url === '/api/plans/login-flow/tasks?limit=25&offset=25'));
  assert.match(h, /Task 30 </);
  assert.doesNotMatch(h, /plans-browser-tasks-more/, 'all tasks shown, no more button');
});

test('the search box matches plan titles, Automaton titles and registry row or finding titles', async () => {
  const { sb } = load();
  await openPlans(sb);
  await sb.plansBrowserToggle('login-flow');
  await search(sb, 'dashboard');
  let h = html(sb);
  assert.match(h, /data-plan="polish"/);
  assert.doesNotMatch(h, /data-plan="login-flow"/, 'title match only');
  await search(sb, 'second step');
  assert.match(html(sb), /data-plan="login-flow"/, 'a loaded Automaton title matches its plan');
  await search(sb, 'passkey registry');
  assert.ok(calls(sb).some(c => c.url.includes('/api/plan-records/browse') && c.url.includes('q=passkey%20registry')), 'row and finding titles use the browse search');
  h = html(sb);
  assert.match(h, /data-plan="login-flow"/);
  assert.doesNotMatch(h, /data-plan="polish"/);
  await search(sb, 'nothing like this');
  assert.match(html(sb), /No plan matches\./);
  await search(sb, '');
  assert.match(html(sb), /data-plan="polish"/);
  assert.notEqual(pr(sb, 'window._plansBrowserSearchTimer'), undefined, 'the debounce timer lives on window');
});

test('the state filter narrows the list and hides the reorder controls while a filter is on', async () => {
  const { sb } = load();
  await openPlans(sb);
  assert.match(html(sb), /plans-move-up/);
  sb.plansBrowserSetState('planned');
  const h = html(sb);
  assert.match(h, /data-plan="polish"/);
  assert.doesNotMatch(h, /data-plan="login-flow"/);
  assert.doesNotMatch(h, /plans-move-up/, 'reordering a filtered list would hide plans from the new order');
});

test('collapse and expand state is remembered under one localStorage key', async () => {
  const { sb } = load();
  await openPlans(sb);
  await sb.plansBrowserToggle('login-flow');
  await sb.plansBrowserToggleTasks('login-flow');
  const keys = pr(sb, `(() => { const k = []; const s = localStorage; return JSON.stringify(s.getItem('cs_plans_browser')); })()`);
  assert.match(keys, /login-flow/);
  assert.match(keys, /tasks:login-flow/);
  assert.equal(pr(sb, "_plansIsOpen('acme', 'login-flow')"), true);
  // A fresh page load reopens what was open and reads its Automata and tasks.
  const again = load();
  again.sb.localStorage.setItem('cs_plans_browser', pr(sb, "localStorage.getItem('cs_plans_browser')"));
  await openPlans(again.sb);
  assert.ok(calls(again.sb).some(c => c.url.includes('/api/plans/login-flow/automata')));
  assert.ok(calls(again.sb).some(c => c.url.includes('/api/plans/login-flow/tasks')));
  assert.match(html(again.sb), /First step/);
  await sb.plansBrowserToggle('login-flow');
  assert.equal(pr(sb, "_plansIsOpen('acme', 'login-flow')"), false);
});

test('a localStorage that throws or holds garbage does not break the browser', async () => {
  const { sb } = load();
  const get = sb.localStorage.getItem, set = sb.localStorage.setItem;
  sb.localStorage.getItem = k => { if (k === 'cs_plans_browser') throw new Error('blocked'); return get(k); };
  sb.localStorage.setItem = (k, v) => { if (k === 'cs_plans_browser') throw new Error('blocked'); return set(k, v); };
  await openPlans(sb);
  assert.doesNotThrow(() => html(sb));
  await assert.doesNotReject(() => sb.plansBrowserToggle('login-flow'));
  const g = load();
  g.sb.localStorage.setItem('cs_plans_browser', '[not json');
  await openPlans(g.sb);
  assert.match(html(g.sb), /data-plan="login-flow"/);
});

test('moving a plan sends the whole new order to PUT /api/plans/order and reloads', async () => {
  const { sb } = load();
  await openPlans(sb);
  await sb.plansBrowserMovePlan('login-flow', 1);
  const put = calls(sb).find(c => c.method === 'PUT' && c.url === '/api/plans/order');
  assert.deepEqual(put.body, { plans: ['polish', 'login-flow'] });
  assert.ok(calls(sb).filter(c => c.url.includes('/plans?')).length >= 2, 'the list is read again');
  const before = calls(sb).length;
  await sb.plansBrowserMovePlan('login-flow', -1);
  assert.equal(calls(sb).length, before, 'the first plan cannot move up');
});

test('moving an Automaton sends the plan\'s whole Automaton order', async () => {
  const { sb } = load();
  await openPlans(sb);
  await sb.plansBrowserToggle('login-flow');
  await sb.plansBrowserMoveAutomaton('login-flow', 'a3', -1);
  const put = calls(sb).find(c => c.method === 'PUT' && c.url === '/api/plans/login-flow/automata/order');
  assert.deepEqual(put.body, { automata: ['a1', 'a3', 'a2'] });
});

test('create a plan posts the project, title and goal, and needs a title', async () => {
  const { sb, fields } = load();
  fields.plansBrowserNewTitle = { value: 'New plan' };
  fields.plansBrowserNewGoal = { value: ' Reach it ' };
  await openPlans(sb);
  await sb.plansBrowserCreate('   ', '');
  assert.match(html(sb), /Enter a title for the plan\./);
  assert.ok(!calls(sb).some(c => c.method === 'POST'));
  await sb.plansBrowserCreate('New plan', ' Reach it ');
  const post = calls(sb).find(c => c.method === 'POST' && c.url === '/api/plans');
  assert.deepEqual(post.body, { project: 'acme', title: 'New plan', goal: 'Reach it' });
  assert.equal(fields.plansBrowserNewTitle.value, '', 'the form clears after a save');
});

test('attach sends the kind and id for an Automaton, an Automation and a finding', async () => {
  const { sb } = load();
  await openPlans(sb);
  await sb.plansBrowserToggle('login-flow');
  await sb.plansBrowserAttach('login-flow', 'automaton', ' a9 ');
  await sb.plansBrowserAttach('login-flow', 'automation', 'chain-2');
  await sb.plansBrowserAttach('login-flow', 'finding', 'f2');
  const posts = calls(sb).filter(c => c.method === 'POST' && c.url === '/api/plans/login-flow/attach').map(c => c.body);
  assert.deepEqual(posts, [{ kind: 'automaton', id: 'a9' }, { kind: 'automation', id: 'chain-2' }, { kind: 'finding', id: 'f2' }]);
  await sb.plansBrowserAttach('login-flow', 'finding', '');
  assert.match(html(sb), /Enter or choose what to attach\./);
});

test('a refused write shows the server message above the list and leaves the list alone', async () => {
  const { sb } = load({ fail: { 'POST /api/plans/login-flow/attach': JSON.stringify({ error: 'automaton "a9" is already linked to polish; detach it first' }) } });
  await openPlans(sb);
  await sb.plansBrowserAttach('login-flow', 'automaton', 'a9');
  const h = html(sb);
  assert.match(h, /The change was not saved: automaton &quot;a9&quot; is already linked to polish; detach it first/);
  assert.match(h, /data-plan="login-flow"/);
});

test('every value from the API is escaped', async () => {
  const evil = { ...PLAN_A, title: HOSTILE, goal: HOSTILE, id: 'evil', next: { automaton_id: 'x', title: HOSTILE } };
  const { sb } = load({ plans: [evil], automata: [{ ...AUTOMATA[0], title: HOSTILE }] });
  await openPlans(sb);
  await sb.plansBrowserToggle('evil');
  const h = html(sb);
  assert.ok(!h.includes(HOSTILE), 'no raw markup from the API');
  assert.ok(h.includes(ESCAPED));
});

test('the Plans sub-tab body is the browser and the project load reads the plans', async () => {
  const { sb } = load();
  await sb._projectsLoad();
  vm.runInContext("_projectsState.selected = 'acme'", sb);
  const page = sb._projectsActiveSubtab;
  sb._projectsActiveSubtab = () => 'plans';
  const body = sb._projectPageHTML({ name: 'acme', dir: '/w' }, { arcs: [], unassigned: { automata: [] } }, '', null);
  sb._projectsActiveSubtab = page;
  assert.match(body, /class="plans-browser" data-project="acme"/);
  assert.match(body, /id="plansBrowserSearch"/);
  assert.match(body, /id="plansBrowserState"/);
  await sb._projectsLoadSelected();
  await flushAsync();
  assert.ok(calls(sb).some(c => c.url.startsWith('/api/projects/acme/plans?')));
});

test('a server with plan records off hides the browser with one line', async () => {
  const { sb } = load();
  sb.fetch = () => Promise.resolve({ ok: false, status: 404, statusText: 'nf', text: () => Promise.resolve('plan records are turned off (autonomous.plan_records.enabled)'), json: () => Promise.resolve({}) });
  await sb._projectsLoad();
  vm.runInContext("_projectsState.list = [{name:'acme'}]; _projectsState.selected = 'acme'", sb);
  await sb._plansBrowserLoad(false);
  assert.match(html(sb), /Plans are turned off for this server\./);
});

test('the browser starts no polling timer and stores no timer on a DOM node', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const a = src.indexOf('// ── Plans browser');
  const b = src.indexOf('// The Records sub-tab: the Registry and findings card');
  const part = src.slice(a, b);
  assert.ok(a > 0 && b > a);
  assert.doesNotMatch(part, /setInterval/);
  assert.match(part, /window\._plansBrowserSearchTimer/);
  assert.equal((part.match(/localStorage\./g) || []).length, 2, 'one key: a read and a write, both in helpers');
  assert.equal((part.match(/_PLANS_BROWSER_KEY/g) || []).length, 4, 'the key constant is declared once and used by the two helpers');
  assert.doesNotMatch(part, /\bPRDs?\b/);
});

test('every key the browser uses is in all five locale bundles, translated, with the right object word', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const part = src.slice(src.indexOf('// ── Plans browser'), src.indexOf('// The Records sub-tab: the Registry and findings card'));
  const used = new Set((part.match(/'((?:automation_)?plans_browser_[a-z_]+)'/g) || []).map(s => s.slice(1, -1)));
  assert.ok(used.size > 35);
  const en = bundle('en');
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of used) {
      assert.ok(typeof b[k] === 'string' && b[k].length, `${lang} lacks ${k}`);
      assert.doesNotMatch(b[k], /\bPRDs?\b/);
      if (lang !== 'en') assert.doesNotMatch(b[k], /\bAutomat(a|on)s?\b/i, `${lang} ${k} leaves the English word`);
    }
  }
  assert.match(en.automation_plans_browser_heading, /Automations/);
  assert.match(bundle('de').automation_plans_browser_heading, /Automatisierungen/);
  assert.match(bundle('de').plans_browser_automata, /Automaten/);
  assert.match(bundle('es').plans_browser_automata, /Autómatas/);
  assert.match(bundle('fr').plans_browser_automata, /Automates/);
  assert.match(bundle('ja').plans_browser_automata, /オートマタ/);
  assert.doesNotMatch(en.plans_browser_automata + en.plans_browser_attach_automaton, /Automation/);
});
