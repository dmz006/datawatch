// Uncommitted-work card, task chip and Automaton banner — PWA side.
//
// window.uncommittedWorkCardHtml(status) draws GET /api/autonomous/uncommitted-work:
// the policy and scope in effect and one row per held task or Automaton with its
// files, the files outside the declared files, the reason and the button that
// opens the question. The card loader, the chip and file list on a held task and
// the banner on a held Automaton are covered here with fetch stubbed and the real
// English bundle.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const HOSTILE = '<img src=x onerror=alert(1)>';
const ENTITY_IMG = '&lt;img src=x onerror=alert(1)&gt;';

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
}

// loadApp returns a sandbox with the app loaded and a recording fetch. routes
// maps a request path to a JSON body; an unknown path answers 404.
function loadApp(routes, byId) {
  const sandbox = buildSandbox({ byId: byId || {} });
  const calls = [];
  sandbox.fetch = url => {
    calls.push(String(url));
    if (!Object.prototype.hasOwnProperty.call(routes || {}, url)) {
      return Promise.resolve({ ok: false, status: 404, statusText: 'Not Found', text: () => Promise.resolve('not found') });
    }
    const body = routes[url];
    return Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(body), text: () => Promise.resolve(JSON.stringify(body)) });
  };
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  sandbox.__calls = calls;
  return sandbox;
}

function call(sandbox, expr, value) {
  sandbox._v = value;
  return vm.runInContext(expr, sandbox);
}

function card(status) {
  return call(loadApp({}), 'window.uncommittedWorkCardHtml(_v)', status);
}

function heldTask(over) {
  return {
    automaton_id: 'a1b2c3d4', name: 'Release train', task_id: 'T3', task_title: 'Add the roll-up',
    kind: 'task',
    files: ['internal/a.go', 'docs/notes.md'],
    outside_scope: ['docs/notes.md'],
    reason: 'The task finished with 2 uncommitted files',
    question_id: 'q-77',
    held_at: '2026-11-01T08:30:00Z',
    ...over,
  };
}

function status(over) {
  return { policy: 'hold', scope: 'task_files', held: [heldTask()], ...over };
}

// ── the card ─────────────────────────────────────────────────────────────────

test('a held task shows its files, the files outside the declared files, the reason and the open-question button', () => {
  const html = card(status());
  assert.ok(html.includes('Automata uncommitted work'), 'card title missing');
  assert.ok(html.includes('Task Add the roll-up of Automaton Release train is held on uncommitted work'), 'task heading missing: ' + html);
  assert.ok(html.includes('Uncommitted files (2)'), 'file count missing');
  assert.ok(html.includes('<code>internal/a.go</code>') && html.includes('<code>docs/notes.md</code>'), 'file names missing');
  assert.ok(html.includes('Outside the declared files (1)'), 'outside-scope heading missing');
  assert.ok(html.includes('Reason: The task finished with 2 uncommitted files'), 'reason missing');
  assert.ok(html.includes('Held since '), 'held time missing');
  assert.ok(html.includes('data-question="q-77"'), 'question id missing');
  assert.ok(html.includes('>Open the question<'), 'open-question label missing');
  assert.ok(html.includes('<details'), 'file list should expand');
  assert.ok(!/<form|<input|<select/i.test(html), 'card must carry no form controls');
});

test('the card shows the policy and scope in effect with their option wording', () => {
  const html = card(status({ policy: 'warn', scope: 'worktree' }));
  assert.ok(html.includes('Uncommitted-work policy: warn (record it and continue)'), 'policy line missing: ' + html);
  assert.ok(html.includes('Uncommitted-work scope: worktree (every uncommitted file)'), 'scope line missing');
  const unknown = card(status({ policy: 'future_mode', scope: '' }));
  assert.ok(unknown.includes('Uncommitted-work policy: future_mode'), 'a policy this version does not know shows as it came');
});

test('a held Automaton row says Automaton and has no task title', () => {
  const html = card(status({ held: [heldTask({ kind: 'worktree', task_id: '', task_title: '', outside_scope: [] })] }));
  assert.ok(html.includes('Automaton Release train is held on uncommitted work in its worktree'), 'worktree heading missing: ' + html);
  assert.ok(!html.includes('Outside the declared files'), 'no outside list when every file is in scope');
  assert.ok(!/\bPRDs?\b/.test(html), 'card must say Automaton, never PRD');
});

test('an empty hold list shows the empty sentence and no rows', () => {
  for (const body of [status({ held: [] }), status({ held: null }), {}, null, undefined, 'nope', [], { held: 'x' }]) {
    const html = card(body);
    assert.ok(html.includes('Automata uncommitted work'), 'title missing for ' + JSON.stringify(body));
    assert.ok(html.includes('No Automaton is held on uncommitted work.'), 'empty sentence missing for ' + JSON.stringify(body));
    assert.ok(!html.includes('uncommitted-work-row'), 'no rows when nothing is held');
  }
});

test('a hold with no question has no open button', () => {
  const html = card(status({ held: [heldTask({ question_id: '' })] }));
  assert.ok(!html.includes('uncommitted-work-open'), 'no button without a question: ' + html);
});

test('hostile file names, names, reasons and question ids are escaped', () => {
  const html = card(status({
    policy: HOSTILE,
    held: [heldTask({ name: HOSTILE, task_title: HOSTILE, files: [HOSTILE, 'ok.go'], outside_scope: [HOSTILE], reason: HOSTILE, question_id: '"><script>x</script>' })],
  }));
  assert.ok(html.includes(ENTITY_IMG), 'hostile text should survive as an escaped entity');
  assert.ok(!html.includes('<img'), 'live tag must not survive: ' + html);
  assert.ok(!html.includes('<script'), 'live script must not survive: ' + html);
  assert.ok(!/onclick="[^"]*x<\/script>/.test(html), 'question id must not reach script text');
});

test('a long file list is capped with a count of the rest', () => {
  const files = Array.from({ length: 120 }, (_, i) => 'f' + i + '.go');
  const html = card(status({ held: [heldTask({ files, outside_scope: [] })] }));
  assert.ok(html.includes('Uncommitted files (120)'), 'full count in the summary');
  assert.ok(html.includes('and 70 more'), 'rest count missing');
  assert.ok(!html.includes('f119.go'), 'files past the cap are not drawn');
});

test('the card title carries the help link to the uncommitted-work heading', () => {
  const html = card(status());
  assert.ok(html.includes('href="/diagrams.html#docs/datawatch-definitions.md#uncommitted-work-check"'), 'help link missing: ' + html);
  assert.ok(html.includes('About the uncommitted-work check'), 'help link text missing');
});

test('the open-question button hands the id to openQuestion', () => {
  const sb = loadApp({});
  const opened = [];
  sb.window.openQuestion = id => opened.push(id);
  sb._btn = { getAttribute: k => (k === 'data-question' ? 'q-77' : null) };
  vm.runInContext('window.uncommittedWorkOpenQuestion(_btn)', sb);
  assert.deepEqual(opened, ['q-77']);
  sb._btn = { getAttribute: () => '' };
  vm.runInContext('window.uncommittedWorkOpenQuestion(_btn)', sb);
  assert.deepEqual(opened, ['q-77'], 'an empty id opens nothing');
});

// ── the loader ───────────────────────────────────────────────────────────────

test('the loader reads the route and draws the card into the Overview slot', async () => {
  const slot = makeStubElement();
  const sb = loadApp({ '/api/autonomous/uncommitted-work': status() }, { automationUncommittedWorkCard: slot });
  const ctl = vm.runInContext('_automationUncommittedWorkLoader()', sb);
  ctl.load ? ctl.load() : ctl.refresh && ctl.refresh();
  await flushAsync(30);
  try {
    assert.ok(sb.__calls.includes('/api/autonomous/uncommitted-work'), 'loader must read GET /api/autonomous/uncommitted-work: ' + sb.__calls);
    assert.ok(slot.innerHTML.includes('Task Add the roll-up of Automaton Release train'), 'held task missing from the slot: ' + slot.innerHTML);
  } finally {
    ctl.stop();
  }
});

test('the Overview sub-tab carries the slot and the stop-all list ends the loader', () => {
  const sb = loadApp({});
  const body = vm.runInContext("_AUTOMATION_SUBTABS.find(s => s.id === 'overview').bodyHTML()", sb);
  assert.ok(body.includes('id="automationUncommittedWorkCard"'), 'slot missing from the Overview body');
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.ok(src.includes("url: '/api/autonomous/uncommitted-work'"), 'loader must read the route');
  assert.ok(/'automation_cleanup', 'automation_uncommitted_work'\]/.test(src), 'stop lists must include the loader');
});

// ── the Automaton detail ─────────────────────────────────────────────────────

function prdWith(task, over) {
  return {
    id: 'a1b2c3d4', title: 'Release train', status: 'running', project_dir: '/repo',
    story: [{ id: 'S1', title: 'Story', tasks: [task] }],
    ...over,
  };
}

function taskHtml(task) {
  const sb = loadApp({});
  sb._p = prdWith(task);
  return vm.runInContext('renderTask(_p, _p.story[0], _p.story[0].tasks[0], false)', sb);
}

test('a task whose JSON carries uncommitted_work shows a chip and an expandable file list', () => {
  const html = taskHtml({
    id: 'T3', title: 'Add the roll-up', status: 'waiting_decision',
    uncommitted_work: { kind: 'task', files: ['internal/a.go', HOSTILE], outside_scope: [HOSTILE], reason: 'two files left', question_id: 'q-77', held_at: '2026-11-01T08:30:00Z' },
  });
  assert.ok(html.includes('uncommitted-work-chip'), 'chip missing: ' + html);
  assert.ok(html.includes('Uncommitted work (2)'), 'chip count missing');
  assert.ok(html.includes('uncommitted-work-files'), 'file list missing');
  assert.ok(html.includes('<code>internal/a.go</code>'), 'file name missing');
  assert.ok(html.includes(ENTITY_IMG) && !html.includes('<img'), 'file names must be escaped');
  assert.ok(html.includes('Outside the declared files (1)'), 'outside list missing');
  assert.ok(html.includes('data-question="q-77"'), 'open-question button missing');
});

test('a task without uncommitted_work has no chip', () => {
  const html = taskHtml({ id: 'T4', title: 'Plain', status: 'completed' });
  assert.ok(!html.includes('uncommitted-work'), 'no uncommitted-work markup on a clean task');
});

test('an Automaton whose own uncommitted_work is set shows the banner, and a clean one does not', () => {
  const sb = loadApp({});
  sb._a = { id: 'a1', uncommitted_work: { kind: 'worktree', files: ['x.go', 'y.go', 'z.go'], outside_scope: ['z.go'], reason: 'files no task owns', question_id: 'q-9' } };
  const banner = vm.runInContext('window.uncommittedWorkBannerHtml(_a)', sb);
  assert.ok(banner.includes('This Automaton is held: its worktree has 3 uncommitted files that no task owns'), 'banner text missing: ' + banner);
  assert.ok(banner.includes('<code>z.go</code>') && banner.includes('Outside the declared files (1)'), 'file lists missing');
  assert.ok(banner.includes('data-question="q-9"'), 'open-question button missing');
  assert.ok(!/\bPRDs?\b/.test(banner), 'banner must say Automaton, never PRD');
  assert.equal(vm.runInContext('window.uncommittedWorkBannerHtml({id: "a2"})', sb), '');
  assert.equal(vm.runInContext('window.uncommittedWorkBannerHtml(null)', sb), '');
});

test('the Automaton detail page draws the banner above its tabs', () => {
  const body = makeStubElement();
  const sb = loadApp({}, { automataDetailBody: body });
  sb._p = prdWith({ id: 'T1', title: 'One', status: 'running' }, {
    uncommitted_work: { kind: 'worktree', files: ['x.go'], outside_scope: [], reason: 'r' },
  });
  vm.runInContext('_renderDetailContent(_p)', sb);
  assert.ok(body.innerHTML.includes('uncommitted-work-banner'), 'banner missing from the detail body');
  sb._p = prdWith({ id: 'T1', title: 'One', status: 'running' });
  vm.runInContext('_renderDetailContent(_p)', sb);
  assert.ok(!body.innerHTML.includes('uncommitted-work-banner'), 'a clean Automaton has no banner');
});

// ── the settings fields ──────────────────────────────────────────────────────

function field(key) {
  const sb = loadApp({});
  const fields = vm.runInContext('GENERAL_CONFIG_FIELDS', sb, { filename: 'app.js' });
  return fields.find(sec => sec.id === 'autonomous').fields.find(f => f.key === key);
}

test('the policy select offers hold, warn and off and defaults to hold', () => {
  const f = field('autonomous.automation.uncommitted_work_policy');
  assert.ok(f, 'policy field is declared');
  assert.equal(f.type, 'select');
  assert.deepEqual(Array.from(f.options), ['hold', 'warn', 'off']);
  assert.equal(f.default, 'hold');
  assert.equal(f.labelKey, 'settings_automation_uncommitted_work_policy');
  assert.equal(f.label, bundle('en')[f.labelKey]);
});

test('the scope select offers task_files and worktree and defaults to task_files', () => {
  const f = field('autonomous.automation.uncommitted_work_scope');
  assert.ok(f, 'scope field is declared');
  assert.equal(f.type, 'select');
  assert.deepEqual(Array.from(f.options), ['task_files', 'worktree']);
  assert.equal(f.default, 'task_files');
  assert.equal(f.labelKey, 'settings_automation_uncommitted_work_scope');
  assert.equal(f.label, bundle('en')[f.labelKey]);
});

test('every option has a label key in the bundle, and the settings group links to the help heading', () => {
  const en = bundle('en');
  for (const key of ['autonomous.automation.uncommitted_work_policy', 'autonomous.automation.uncommitted_work_scope']) {
    const f = field(key);
    for (const o of Array.from(f.options)) {
      assert.ok(f.optionLabelKeys[o] && en[f.optionLabelKeys[o]], key + ' option ' + o + ' needs a label key in the bundle');
    }
  }
  const sb = loadApp({});
  const sec = vm.runInContext('GENERAL_CONFIG_FIELDS', sb).find(s => s.id === 'autonomous');
  const group = sec.fields.find(f => f.type === 'group' && f.labelKey === 'settings_automation_uncommitted_work_group');
  assert.ok(group, 'settings group is declared');
  sb.localStorage = { getItem: () => null };
  const link = call(sb, 'defsLink(_v)', group.docsKey);
  assert.ok(link.includes('/diagrams.html#docs/datawatch-definitions.md#uncommitted-work-check'), 'group help link target: ' + link);
});

// ── locale keys ──────────────────────────────────────────────────────────────

test('every uncommitted_work key the PWA uses is in every bundle with no PRD word or tracker id', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const start = src.indexOf('// ── Automata uncommitted-work card');
  const end = src.indexOf('// ── Automata progress card');
  const used = new Set((src.slice(start, end) + src.slice(src.indexOf('function _automationUncommittedWorkLoader'), src.indexOf('function _automationUncommittedWorkLoader') + 700))
    .match(/'(?:settings_automation_)?uncommitted_work_[a-z_]*[a-z]'/g).map(s => s.slice(1, -1)));
  assert.ok(used.size >= 8, 'expected at least eight keys, saw ' + used.size);
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = bundle(lang);
    for (const k of used) {
      if (k === 'uncommitted_work_help_href') continue;
      const v = b[k];
      assert.ok(typeof v === 'string' && v.length > 0, lang + ' missing or empty ' + k);
      assert.ok(!/\bPRDs?\b/.test(v), lang + ' ' + k + ' must say Automaton or Automata, never PRD');
      assert.ok(!/\b(?:BL|F|B|S)\d{1,6}\b/.test(v), lang + ' ' + k + ' carries a tracker id');
    }
  }
});
