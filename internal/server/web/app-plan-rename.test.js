// Rename and edit goal in the Plans browser — the detail of a plan on the
// Projects tab.
//
// Each plan shows its status, as the daemon last rolled it up from the plan's
// linked Automata, with a "set by hand" badge when the status was written by
// hand (the roll-up keeps it). The detail of a plan has a Rename and an Edit
// goal control. Each opens one inline editor that checks the value as it is
// typed (a title is required, one line and at most 100 characters; a goal is at
// most 2000), shows a character counter, and calls PATCH /api/plans/{ref}. A
// refusal from the daemon, such as a taken title that names the other plan, is
// shown in place and the editor keeps what was typed. Every value is escaped
// and every string goes through the i18n helper.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-plan-rename.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const HOSTILE = '<img src=x onerror=alert(1)>';
const ESCAPED = '&lt;img src=x onerror=alert(1)&gt;';

const PLAN_A = { title: 'Ship the login flow', id: 'login-flow', uid: 'u1', goal: 'Users sign in with a passkey', state: 'in_progress', set_by_hand: false, order: 1,
  progress: { done: 1, total: 3, percent: 33 }, points: 2.5, automation_ids: [], automaton_ids: ['a1'], finding_count: 0 };
const PLAN_B = { title: 'Polish the dashboard', id: 'polish', uid: 'u2', goal: '', state: 'done', set_by_hand: true, order: 2,
  progress: { done: 1, total: 1, percent: 100 }, points: 0, automation_ids: [], automaton_ids: ['b1'], finding_count: 0 };

function bundle(lang) { return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8')); }

// field is a stand-in for a DOM element the editor updates in place.
function field() {
  const f = { textContent: '', style: {}, disabled: false, attrs: {}, focused: 0 };
  f.setAttribute = (k, v) => { f.attrs[k] = v; };
  f.focus = () => { f.focused++; };
  return f;
}

// load builds the app with the Projects tab active. opts.patch(call) answers a
// PATCH: it returns a body, or throws {status, text}.
function load(opts = {}) {
  const view = makeStubElement(() => {});
  view.insertAdjacentHTML = () => {};
  const list = { innerHTML: '' };
  const fields = { plansEditCounter: field(), plansEditError: field(), plansEditSave: field(), plansEditInput: field() };
  const sandbox = buildSandbox({ byId: { automataPanel: view, view, plansBrowserList: list } });
  const realGet = sandbox.document.getElementById.bind(sandbox.document);
  sandbox.document.getElementById = id => (fields[id] || realGet(id));
  sandbox.__calls = [];
  const plans = [PLAN_A, PLAN_B].map(p => Object.assign({}, p));
  sandbox.fetch = (url, o) => {
    const u = String(url);
    o = o || {};
    const call = { url: u, method: o.method || 'GET', body: o.body ? JSON.parse(o.body) : null };
    const ok = b => Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(b), text: () => Promise.resolve(JSON.stringify(b)) });
    if (u.startsWith('/api/plans') || u.includes('/plans?') || u.startsWith('/api/plan-records/browse')) {
      sandbox.__calls.push(call);
      if (call.method === 'PATCH') {
        try {
          const r = opts.patch ? opts.patch(call) : null;
          const target = plans.find(p => u === '/api/plans/' + encodeURIComponent(p.id));
          if (target && call.body.title !== undefined) target.title = call.body.title;
          if (target && call.body.goal !== undefined) target.goal = call.body.goal;
          return ok(r || { plan: { title: target && target.title, id: target && target.id }, goal: target && target.goal });
        } catch (e) {
          return Promise.resolve({ ok: false, status: e.status || 500, statusText: 'err', json: () => Promise.resolve({}), text: () => Promise.resolve(e.text || '') });
        }
      }
      if (u.includes('/plans?')) return ok({ project: 'acme', plans, page: { total: plans.length }, backlog: { plans: [], page: { total: 0 } } });
      if (u.includes('/automata?')) return ok({ automata: [], page: { total: 0 } });
      if (u.startsWith('/api/plan-records/browse')) return ok({ records: [], page: { total: 0 } });
      return ok({});
    }
    if (u.includes('/api/projects') && !u.includes('/rollup') && !u.includes('/history') && !u.includes('/overview-summary')) return ok([{ name: 'acme', dir: '/work/acme' }]);
    if (u.includes('/history')) return ok({ items: [], gantt: [] });
    return ok({});
  };
  sandbox.__timers = [];
  sandbox.setTimeout = fn => { sandbox.__timers.push(fn); return sandbox.__timers.length; };
  sandbox.clearTimeout = id => { if (id) sandbox.__timers[id - 1] = null; };
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  vm.runInContext("state.activeView = 'autonomous'; _automataState.tab = 'projects'", sandbox);
  return { sb: sandbox, list, fields, plans };
}

async function openPlans(sb) {
  await sb._projectsLoad();
  vm.runInContext("_projectsState.selected = 'acme'", sb);
  await sb._plansBrowserLoadSelected();
  await flushAsync();
}
const html = sb => sb.plansBrowserListHTML(vm.runInContext('_plansBrowser', sb));
const patches = sb => Array.from(sb.__calls).filter(c => c.method === 'PATCH');

async function expanded(opts) {
  const h = load(opts);
  await openPlans(h.sb);
  await h.sb.plansBrowserToggle('login-flow');
  await flushAsync();
  return h;
}

test('a plan shows its rolled-up status, and a status set by hand carries a badge in the list and in the detail', async () => {
  const { sb } = load();
  await openPlans(sb);
  await sb.plansBrowserToggle('polish');
  await flushAsync();
  const h = html(sb);
  const a = h.slice(h.indexOf('data-plan="login-flow"'), h.indexOf('data-plan="polish"'));
  const b = h.slice(h.indexOf('data-plan="polish"'));
  assert.match(a, /In progress/);
  assert.doesNotMatch(a, /plans-browser-by-hand/, 'a derived status has no badge');
  assert.match(b, /Done/);
  assert.equal((b.match(/plans-browser-by-hand/g) || []).length, 2, 'badge in the list row and in the detail');
  assert.match(b, /set by hand/);
  assert.match(b, /title="This status was set by hand/);
  assert.match(b, /plans-browser-detail-state/);
  assert.match(b, /Status:/);
});

test('the detail of a plan has Rename and Edit goal controls; a collapsed plan has none', async () => {
  const { sb } = load();
  await openPlans(sb);
  assert.doesNotMatch(html(sb), /plans-edit-rename/);
  await sb.plansBrowserToggle('login-flow');
  await flushAsync();
  const h = html(sb);
  assert.match(h, /class="btn-secondary plans-edit-rename"[^>]*>Rename</);
  assert.match(h, /class="btn-secondary plans-edit-goal"[^>]*>Edit goal</);
});

test('Rename opens an editor holding the current title and a character counter', async () => {
  const { sb, fields } = await expanded();
  await sb.plansBrowserEditOpen('login-flow', 'rename');
  const h = html(sb);
  assert.match(h, /data-edit="rename"/);
  assert.match(h, /<input type="text" id="plansEditInput"[^>]*value="Ship the login flow"/);
  assert.match(h, /aria-label="New title for this plan"/);
  assert.match(h, /id="plansEditCounter"[^>]*>19 of 100 characters</);
  assert.doesNotMatch(h, /plans-edit-rename"/, 'the buttons give way to the editor');
  assert.ok(fields.plansEditInput.focused > 0, 'the field takes focus');
});

test('typing checks the title as the daemon would: required, one line, at most 100 characters', async () => {
  const { sb, fields } = await expanded();
  await sb.plansBrowserEditOpen('login-flow', 'rename');
  sb.plansBrowserEditInput('   ');
  assert.equal(fields.plansEditError.textContent, 'A plan needs a title.');
  assert.equal(fields.plansEditSave.disabled, true);
  sb.plansBrowserEditInput('two\nlines');
  assert.equal(fields.plansEditError.textContent, 'A plan title is one line.');
  assert.equal(fields.plansEditSave.disabled, true);
  sb.plansBrowserEditInput('x'.repeat(100));
  assert.equal(fields.plansEditError.textContent, '');
  assert.equal(fields.plansEditCounter.textContent, '100 of 100 characters');
  assert.equal(fields.plansEditSave.disabled, false, '100 characters is allowed');
  sb.plansBrowserEditInput('x'.repeat(101));
  assert.equal(fields.plansEditError.textContent, 'A plan title is at most 100 characters.');
  assert.equal(fields.plansEditCounter.textContent, '101 of 100 characters');
  assert.equal(fields.plansEditCounter.style.color, 'var(--error)');
  assert.equal(fields.plansEditSave.disabled, true);
  assert.equal(fields.plansEditInput.attrs['aria-invalid'], 'true');
  // Characters, not bytes: 100 accented letters fit.
  sb.plansBrowserEditInput('é'.repeat(100));
  assert.equal(fields.plansEditError.textContent, '');
  assert.equal(fields.plansEditInput.attrs['aria-invalid'], 'false');
  await sb.plansBrowserEditSave();
  assert.equal(patches(sb).length, 1, 'valid value is sent');
});

test('Save sends PATCH /api/plans/{ref} with the trimmed title, closes the editor and shows the new title', async () => {
  const { sb, list } = await expanded();
  await sb.plansBrowserEditOpen('login-flow', 'rename');
  sb.plansBrowserEditInput('  Ship login v2  ');
  await sb.plansBrowserEditSave();
  await flushAsync();
  const p = patches(sb);
  assert.equal(p.length, 1);
  assert.equal(p[0].url, '/api/plans/login-flow');
  assert.deepEqual(p[0].body, { title: 'Ship login v2' });
  assert.equal(vm.runInContext('_plansBrowser.edit', sb), null, 'editor closed');
  assert.match(list.innerHTML, /Ship login v2/);
  assert.doesNotMatch(list.innerHTML, /data-edit=/);
  assert.ok(Array.from(sb.__calls).some(c => c.method === 'GET' && c.url.includes('/plans?')), 'the list is read again');
});

test('a taken title shows the daemon\'s message naming the other plan, in place, and keeps what was typed', async () => {
  const msg = 'the title "Polish the dashboard" is already used in project acme by "Polish the dashboard" (polish)';
  const { sb, list } = await expanded({ patch: () => { throw { status: 409, text: JSON.stringify({ error: msg }) }; } });
  await sb.plansBrowserEditOpen('login-flow', 'rename');
  sb.plansBrowserEditInput('polish the dashboard');
  await sb.plansBrowserEditSave();
  await flushAsync();
  const h = list.innerHTML;
  assert.match(h, /id="plansEditError"[^>]*role="alert"|role="alert"[^>]*id="plansEditError"/);
  assert.ok(h.includes('by &quot;Polish the dashboard&quot; (polish)'), 'the other plan is named: ' + h.slice(h.indexOf('plansEditError')));
  assert.match(h, /value="polish the dashboard"/, 'the typed value stays');
  assert.equal(vm.runInContext('_plansBrowser.edit.busy', sb), false, 'Save is available again');
  assert.equal(vm.runInContext('_plansBrowser.edit.mode', sb), 'rename');
  // Typing again clears the daemon's message.
  sb.plansBrowserEditInput('polish the dashboard 2');
  assert.equal(sb.document.getElementById('plansEditError').textContent, '');
});

test('Cancel and an unchanged value close the editor without calling the daemon', async () => {
  const { sb } = await expanded();
  await sb.plansBrowserEditOpen('login-flow', 'rename');
  sb.plansBrowserEditInput('Something else');
  sb.plansBrowserEditCancel();
  assert.equal(vm.runInContext('_plansBrowser.edit', sb), null);
  await sb.plansBrowserEditOpen('login-flow', 'goal');
  await sb.plansBrowserEditSave();
  assert.equal(vm.runInContext('_plansBrowser.edit', sb), null, 'nothing changed');
  assert.equal(patches(sb).length, 0);
  // Escape cancels; Enter saves a title.
  await sb.plansBrowserEditOpen('login-flow', 'rename');
  sb.plansBrowserEditKey({ key: 'Escape' });
  assert.equal(vm.runInContext('_plansBrowser.edit', sb), null);
  await sb.plansBrowserEditOpen('login-flow', 'rename');
  sb.plansBrowserEditInput('Entered title');
  let prevented = false;
  sb.plansBrowserEditKey({ key: 'Enter', preventDefault() { prevented = true; } });
  await flushAsync();
  assert.ok(prevented);
  assert.deepEqual(patches(sb)[0].body, { title: 'Entered title' });
});

test('Edit goal opens a text area with the goal, counts to 2000 and sends the goal; an empty goal clears it', async () => {
  const { sb, fields, plans } = await expanded();
  await sb.plansBrowserEditOpen('login-flow', 'goal');
  let h = html(sb);
  assert.match(h, /data-edit="goal"/);
  assert.match(h, /<textarea id="plansEditInput"[^>]*>Users sign in with a passkey<\/textarea>/);
  assert.match(h, /aria-label="Goal of this plan"/);
  assert.match(h, /28 of 2000 characters/);
  sb.plansBrowserEditInput('x'.repeat(2001));
  assert.equal(fields.plansEditError.textContent, 'A goal is at most 2000 characters.');
  assert.equal(fields.plansEditSave.disabled, true);
  await sb.plansBrowserEditSave();
  assert.equal(patches(sb).length, 0, 'an over-long goal is not sent');
  sb.plansBrowserEditInput('  Sign in without a password ');
  await sb.plansBrowserEditSave();
  await flushAsync();
  assert.deepEqual(patches(sb)[0].body, { goal: 'Sign in without a password' });
  assert.equal(plans[0].goal, 'Sign in without a password');
  assert.match(html(sb), /Sign in without a password/);
  // An empty goal is allowed and clears it.
  await sb.plansBrowserEditOpen('login-flow', 'goal');
  sb.plansBrowserEditInput('');
  assert.equal(fields.plansEditError.textContent, '');
  assert.equal(fields.plansEditSave.disabled, false);
  await sb.plansBrowserEditSave();
  await flushAsync();
  assert.deepEqual(patches(sb)[1].body, { goal: '' });
});

test('a failure that is not a conflict is shown in place too, and Save is available to retry', async () => {
  const { sb, list } = await expanded({ patch: () => { throw { status: 400, text: JSON.stringify({ error: 'a plan title is at most 100 characters' }) }; } });
  await sb.plansBrowserEditOpen('login-flow', 'rename');
  sb.plansBrowserEditInput('Valid here');
  await sb.plansBrowserEditSave();
  await flushAsync();
  assert.match(list.innerHTML, /a plan title is at most 100 characters/);
  assert.equal(vm.runInContext('_plansBrowser.edit.busy', sb), false);
});

test('every plan value is escaped in the editor, the error and the badge', async () => {
  const { sb, list } = await expanded({ patch: () => { throw { status: 409, text: JSON.stringify({ error: 'used by ' + HOSTILE }) }; } });
  await sb.plansBrowserEditOpen('login-flow', 'rename');
  sb.plansBrowserEditInput(HOSTILE);
  await sb.plansBrowserEditSave();
  await flushAsync();
  assert.ok(!list.innerHTML.includes(HOSTILE), 'no raw markup');
  assert.ok(list.innerHTML.includes(ESCAPED));
});

test('the editor strings exist in all five locales, are translated, and keep their placeholders', () => {
  const keys = ['plans_browser_by_hand', 'plans_browser_by_hand_hint', 'plans_edit_status_label', 'plans_edit_rename', 'plans_edit_goal',
    'plans_edit_rename_label', 'plans_edit_goal_label', 'plans_edit_save', 'plans_edit_saving', 'plans_edit_cancel', 'plans_edit_counter',
    'plans_edit_title_required', 'plans_edit_title_one_line', 'plans_edit_title_too_long', 'plans_edit_goal_too_long'];
  const en = bundle('en');
  const placeholders = { plans_edit_counter: ['%1$s', '%2$s'], plans_edit_title_too_long: ['%1$s'], plans_edit_goal_too_long: ['%1$s'] };
  // "Status" is the same word in English and German.
  const sameAsEnglish = { de: ['plans_edit_status_label'] };
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of keys) {
      assert.ok(typeof b[k] === 'string' && b[k].trim() !== '', `${lang}: ${k} missing`);
      for (const ph of placeholders[k] || []) assert.ok(b[k].includes(ph), `${lang}: ${k} lost ${ph}`);
      assert.ok(!/PRD/.test(b[k]), `${lang}: ${k} names a PRD`);
      if (lang !== 'en' && !(sameAsEnglish[lang] || []).includes(k)) assert.notEqual(b[k], en[k], `${lang}: ${k} is not translated`);
    }
  }
});

test('the editor draws in every language without leaking a raw key', async () => {
  for (const lang of LANGS) {
    const { sb } = await expanded();
    sb._i18n.bundle = bundle(lang);
    await sb.plansBrowserEditOpen('login-flow', 'rename');
    sb.plansBrowserEditInput('x'.repeat(101));
    const h = html(sb);
    assert.doesNotMatch(h, /plans_edit_|plans_browser_by_hand/, `${lang}: raw key in the markup`);
    assert.notEqual(sb.document.getElementById('plansEditError').textContent, 'plans_edit_title_too_long');
    assert.ok(sb.document.getElementById('plansEditError').textContent.includes('100'), `${lang}: limit in the message`);
  }
});
