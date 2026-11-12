// Protected-paths settings, card, task chip, Automaton banner and authorised
// paths editor — PWA side.
//
// window.protectedPathsCardHtml(status) draws GET /api/autonomous/protected-paths:
// the action and rules source in effect, the effective protected list, one row
// per held task and the Automata that authorise paths in advance. The chip and
// paths on a held task, the banner on an Automaton with held tasks, the three
// settings fields and the Authorised paths editor are covered here with fetch
// stubbed and the real English bundle.

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
// maps a request path to a JSON body (200) or to {status, body} for any other
// answer; an unknown path answers 404.
function loadApp(routes, byId) {
  const sandbox = buildSandbox({ byId: byId || {} });
  const calls = [];
  sandbox.fetch = (url, opts) => {
    calls.push({ url: String(url), opts: opts || {} });
    if (!Object.prototype.hasOwnProperty.call(routes || {}, url)) {
      return Promise.resolve({ ok: false, status: 404, statusText: 'Not Found', text: () => Promise.resolve('not found') });
    }
    const route = routes[url];
    if (route && typeof route === 'object' && typeof route.status === 'number' && 'body' in route) {
      const ok = route.status >= 200 && route.status < 300;
      return Promise.resolve({
        ok, status: route.status, statusText: '',
        json: () => Promise.resolve(route.body), text: () => Promise.resolve(typeof route.body === 'string' ? route.body : JSON.stringify(route.body)),
      });
    }
    return Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(route), text: () => Promise.resolve(JSON.stringify(route)) });
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
  return call(loadApp({}), 'window.protectedPathsCardHtml(_v)', status);
}

function heldEntry(over) {
  return {
    automaton_id: 'a1b2c3d4', automaton_title: 'Release train', task_id: 'T3', task_title: 'Tidy the rules',
    paths: ['AGENT.md', '.github/workflows/ci.yml'],
    action: 'hold',
    reason: 'The commit changed 2 protected paths',
    question_id: 'q-42',
    since: '2026-11-01T08:30:00Z',
    awaiting_spec_change: false,
    ...over,
  };
}

function status(over) {
  return {
    action: 'hold', rules_source: 'base_commit', paths: ['AGENT.md', '.github/workflows/'],
    held: [heldEntry()], authorized: [],
    ...over,
  };
}

// ── the card ─────────────────────────────────────────────────────────────────

test('a held task shows its title, Automaton, paths, reason, since and the open-question button', () => {
  const html = card(status());
  assert.ok(html.includes('Automata protected paths'), 'card title missing');
  assert.ok(html.includes('Task Tidy the rules of Automaton Release train is held on protected paths'), 'task heading missing: ' + html);
  assert.ok(html.includes('Changed protected paths (2)'), 'path count missing');
  assert.ok(html.includes('<code>AGENT.md</code>') && html.includes('<code>.github/workflows/ci.yml</code>'), 'path names missing');
  assert.ok(html.includes('Reason: The commit changed 2 protected paths'), 'reason missing');
  assert.ok(html.includes('Held since '), 'held time missing');
  assert.ok(html.includes('data-question="q-42"'), 'question id missing');
  assert.ok(html.includes('>Open the question<'), 'open-question label missing');
  assert.ok(html.includes('onclick="window.uncommittedWorkOpenQuestion(this)"'), 'the open-question helper is reused');
  assert.ok(html.includes('<details'), 'path list should expand');
  assert.ok(!/<form|<input|<select/i.test(html), 'card must carry no form controls');
  assert.ok(html.includes('Read-only status'), 'read-only note missing');
});

test('the card shows the action, the rules source and the effective list in effect', () => {
  const html = card(status({ action: 'block', rules_source: 'worktree' }));
  assert.ok(html.includes('Protected-paths action: block (refuse the commit)'), 'action line missing: ' + html);
  assert.ok(html.includes('Rules review source: worktree (the rules file in the task working tree)'), 'source line missing');
  assert.ok(html.includes('Protected paths in effect (2)'), 'effective list heading missing');
  assert.ok(html.includes('protected-paths-effective'), 'effective list missing');
  const unknown = card(status({ action: 'future_mode', rules_source: '', paths: [] }));
  assert.ok(unknown.includes('Protected-paths action: future_mode'), 'an action this version does not know shows as it came');
  assert.ok(unknown.includes('No path is protected.'), 'an empty effective list says so');
});

test('an awaiting-spec-change hold says the task waits for its instructions', () => {
  const html = card(status({ held: [heldEntry({ awaiting_spec_change: true })] }));
  assert.ok(html.includes('Waiting for the task instructions to change'));
  assert.ok(!card(status()).includes('Waiting for the task instructions to change'));
});

test('an Automaton that authorises paths in advance is listed', () => {
  const html = card(status({ held: [], authorized: [{ automaton_id: 'a9', automaton_title: 'Docs sweep', paths: ['docs/a.md', 'docs/b.md'] }, { automaton_id: 'a8', automaton_title: 'None', paths: [] }] }));
  assert.ok(html.includes('Automaton Docs sweep authorises: docs/a.md, docs/b.md'), 'authorised line missing: ' + html);
  assert.ok(!html.includes('Automaton None authorises'), 'an Automaton with no paths is not listed');
});

test('an empty hold list shows the empty sentence and no rows', () => {
  for (const body of [status({ held: [] }), status({ held: null }), {}, null, undefined, 'nope', [], { held: 'x' }]) {
    const html = card(body);
    assert.ok(html.includes('Automata protected paths'), 'title missing for ' + JSON.stringify(body));
    assert.ok(html.includes('No Automaton task is held on protected paths.'), 'empty sentence missing for ' + JSON.stringify(body));
    assert.ok(!html.includes('protected-paths-row'), 'no rows when nothing is held');
  }
});

test('a hold with no question has no open button', () => {
  const html = card(status({ held: [heldEntry({ question_id: '' })] }));
  assert.ok(!html.includes('uncommitted-work-open'), 'no button without a question: ' + html);
});

test('hostile titles, paths, reasons, actions and question ids are escaped', () => {
  const html = card(status({
    action: HOSTILE, rules_source: HOSTILE, paths: [HOSTILE],
    held: [heldEntry({ automaton_title: HOSTILE, task_title: HOSTILE, paths: [HOSTILE, 'ok.go'], reason: HOSTILE, question_id: '"><script>x</script>' })],
    authorized: [{ automaton_id: 'a', automaton_title: HOSTILE, paths: [HOSTILE] }],
  }));
  assert.ok(html.includes(ENTITY_IMG), 'hostile text should survive as an escaped entity');
  assert.ok(!html.includes('<img'), 'live tag must not survive: ' + html);
  assert.ok(!html.includes('<script'), 'live script must not survive: ' + html);
  assert.ok(!/onclick="[^"]*x<\/script>/.test(html), 'question id must not reach script text');
});

test('a long path list is capped with a count of the rest', () => {
  const paths = Array.from({ length: 120 }, (_, i) => 'p' + i + '.go');
  const html = card(status({ held: [heldEntry({ paths })] }));
  assert.ok(html.includes('Changed protected paths (120)'), 'full count in the summary');
  assert.ok(html.includes('and 70 more'), 'rest count missing');
  assert.ok(!html.includes('<code>p119.go</code>'), 'paths past the cap are not drawn');
});

test('the card title carries the help link to the protected-paths heading', () => {
  const html = card(status());
  assert.ok(html.includes('href="/diagrams.html#docs/datawatch-definitions.md#protected-paths"'), 'help link missing: ' + html);
  assert.ok(html.includes('About protected paths'), 'help link text missing');
});

test('the card never says PRD', () => {
  assert.ok(!/\bPRDs?\b/.test(card(status())));
  assert.ok(!/\bPRDs?\b/.test(card(null)));
});

// ── the loader ───────────────────────────────────────────────────────────────

test('the loader reads the route and draws the card into the Overview slot', async () => {
  const slot = makeStubElement();
  const sb = loadApp({ '/api/autonomous/protected-paths': status() }, { automationProtectedPathsCard: slot });
  const ctl = vm.runInContext('_automationProtectedPathsLoader()', sb);
  ctl.load ? ctl.load() : ctl.refresh && ctl.refresh();
  await flushAsync(30);
  try {
    assert.ok(sb.__calls.some(c => c.url === '/api/autonomous/protected-paths'), 'loader must read GET /api/autonomous/protected-paths');
    assert.ok(slot.innerHTML.includes('Task Tidy the rules of Automaton Release train'), 'held task missing from the slot: ' + slot.innerHTML);
  } finally {
    ctl.stop();
  }
});

test('the Housekeeping sub-tab carries the slot beside the uncommitted-work card and the stop lists end the loader', () => {
  const sb = loadApp({});
  const body = vm.runInContext("_AUTOMATION_SUBTABS.find(s => s.id === 'housekeeping').bodyHTML()", sb);
  const a = body.indexOf('id="automationUncommittedWorkCard"');
  const b = body.indexOf('id="automationProtectedPathsCard"');
  assert.ok(a >= 0 && b > a, 'protected-paths slot should follow the uncommitted-work slot');
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.ok(src.includes("url: '/api/autonomous/protected-paths'"), 'loader must read the route');
  assert.ok(src.includes('_automationUncommittedWorkLoader(), _automationProtectedPathsLoader()'), 'mount list must start the loader');
  assert.equal((src.match(/'automation_protected_paths'/g) || []).length >= 3, true, 'loader key in the loader, the sub-tab stop list and the stop-all list');
});

// ── the Automaton detail ─────────────────────────────────────────────────────

function hold(over) {
  return { paths: ['AGENT.md', HOSTILE], action: 'hold', reason: 'two paths changed', question_id: 'q-42', since: '2026-11-01T08:30:00Z', ...over };
}

function prdWith(task, over) {
  return {
    id: 'a1b2c3d4', title: 'Release train', status: 'running', project_dir: '/repo',
    stories: [{ id: 'S1', title: 'Story', tasks: [task] }],
    ...over,
  };
}

function taskHtml(task) {
  const sb = loadApp({});
  sb._p = prdWith(task);
  return vm.runInContext('renderTask(_p, _p.stories[0], _p.stories[0].tasks[0], false)', sb);
}

test('a task whose JSON carries protected_path_hold shows a chip and the paths', () => {
  const html = taskHtml({ id: 'T3', title: 'Tidy the rules', status: 'waiting_decision', protected_path_hold: hold() });
  assert.ok(html.includes('protected-paths-chip'), 'chip missing: ' + html);
  assert.ok(html.includes('Protected paths (2)'), 'chip count missing');
  assert.ok(html.includes('protected-paths-task'), 'task body missing');
  assert.ok(html.includes('<code>AGENT.md</code>'), 'path missing');
  assert.ok(html.includes(ENTITY_IMG) && !html.includes('<img'), 'paths must be escaped');
  assert.ok(html.includes('data-question="q-42"'), 'open-question button missing');
  assert.ok(html.includes('Reason: two paths changed'), 'reason missing');
});

test('the chip tooltip carries the reason and the paths, escaped', () => {
  const sb = loadApp({});
  sb._t = { protected_path_hold: hold() };
  const chip = vm.runInContext('window.protectedPathsChipHtml(_t)', sb);
  assert.ok(chip.includes('title="two paths changed\nAGENT.md\n' + ENTITY_IMG + '"'), 'tooltip: ' + chip);
  sb._t = { protected_path_hold: hold({ reason: '' }) };
  assert.ok(vm.runInContext('window.protectedPathsChipHtml(_t)', sb).includes('This task is held: its commit changed protected paths'), 'default tooltip');
});

test('a task without protected_path_hold has no chip or body', () => {
  const html = taskHtml({ id: 'T4', title: 'Plain', status: 'completed' });
  assert.ok(!html.includes('protected-paths'), 'no protected-paths markup on a clean task');
  const sb = loadApp({});
  for (const v of [null, undefined, {}, { protected_path_hold: null }, { protected_path_hold: 'x' }]) {
    sb._t = v;
    assert.equal(vm.runInContext('window.protectedPathsChipHtml(_t)', sb), '');
    assert.equal(vm.runInContext('window.protectedPathsTaskHtml(_t)', sb), '');
  }
});

test('an Automaton with held tasks shows the banner, and a clean one does not', () => {
  const sb = loadApp({});
  sb._a = prdWith({ id: 'T3', title: 'Tidy the rules', status: 'waiting_decision', protected_path_hold: hold() });
  const banner = vm.runInContext('window.protectedPathsBannerHtml(_a)', sb);
  assert.ok(banner.includes('This Automaton is held: 1 of its tasks changed protected paths'), 'banner text missing: ' + banner);
  assert.ok(banner.includes('Task Tidy the rules of Automaton Release train is held on protected paths'), 'task line missing');
  assert.ok(banner.includes('<code>AGENT.md</code>'), 'paths missing');
  assert.ok(banner.includes('data-question="q-42"'), 'open-question button missing');
  assert.ok(banner.includes('role="alert"'));
  assert.ok(!banner.includes('<img'), 'banner must escape paths');
  assert.ok(!/\bPRDs?\b/.test(banner), 'banner must say Automaton, never PRD');
  sb._a = prdWith({ id: 'T4', title: 'Plain', status: 'completed' });
  assert.equal(vm.runInContext('window.protectedPathsBannerHtml(_a)', sb), '');
  for (const v of ['null', 'undefined', '{}', '{stories: "x"}', '{stories: [null, {tasks: null}]}']) {
    assert.equal(vm.runInContext('window.protectedPathsBannerHtml(' + v + ')', sb), '', v);
  }
});

test('the Automaton detail page draws the banner and the authorised editor', () => {
  const body = makeStubElement();
  const sb = loadApp({}, { automataDetailBody: body });
  sb._p = prdWith({ id: 'T1', title: 'One', status: 'waiting_decision', protected_path_hold: hold() }, { authorized_paths: ['docs/a.md'] });
  vm.runInContext('_renderDetailContent(_p)', sb);
  assert.ok(body.innerHTML.includes('protected-paths-banner'), 'banner missing from the detail body');
  assert.ok(body.innerHTML.includes('protected-paths-authorised'), 'authorised editor missing from the detail body');
  sb._p = prdWith({ id: 'T1', title: 'One', status: 'running' });
  vm.runInContext('_renderDetailContent(_p)', sb);
  assert.ok(!body.innerHTML.includes('protected-paths-banner'), 'a clean Automaton has no banner');
  assert.ok(body.innerHTML.includes('protected-paths-authorised'), 'the editor is always there');
});

// ── the authorised paths editor ──────────────────────────────────────────────

function editorHtml(prd) {
  const sb = loadApp({});
  sb.localStorage = { getItem: () => null };
  return call(sb, 'window.protectedPathsAuthorisedHtml(_v)', prd);
}

test('the editor shows the title, the help link, the current list and a field with the list', () => {
  const html = editorHtml({ id: 'a1b2c3d4', authorized_paths: ['docs/a.md', HOSTILE] });
  assert.ok(html.includes('Authorised paths'), 'title missing');
  assert.ok(html.includes('/diagrams.html#docs/datawatch-definitions.md#authorised-paths'), 'help link missing: ' + html);
  assert.ok(html.includes('<code>docs/a.md</code>'), 'current list missing');
  assert.ok(html.includes('value="docs/a.md, ' + ENTITY_IMG + '"'), 'field should hold the current list, escaped');
  assert.ok(!html.includes('<img'), 'hostile path must be escaped');
  assert.ok(html.includes('data-prd="a1b2c3d4"'), 'id travels in a data attribute');
  assert.ok(html.includes('>Save authorised paths<'), 'save label missing');
  assert.ok(!/\bPRDs?\b/.test(html.replace(/protectedPaths\w+/g, '')), 'editor must say Automaton, never PRD');
});

test('an Automaton with no authorised paths says so, and a missing Automaton draws nothing', () => {
  assert.ok(editorHtml({ id: 'a1' }).includes('No path is authorised for this Automaton.'));
  assert.equal(editorHtml(null), '');
  assert.equal(editorHtml({}), '');
});

function editorDom(value) {
  const input = { value };
  const list = { innerHTML: '' };
  const msg = { innerHTML: '' };
  const btn = { disabled: false, getAttribute: k => (k === 'data-prd' ? 'a1b2c3d4' : null) };
  return { input, list, msg, btn, byId: { protectedPathsAuthInput: input, protectedPathsAuthList: list, protectedPathsAuthMsg: msg } };
}

test('saving posts the comma separated entries to set_authorized_paths and shows the saved list', async () => {
  const dom = editorDom(' docs/a.md , ,AGENT.md,');
  const url = '/api/autonomous/prds/a1b2c3d4/set_authorized_paths';
  const sb = loadApp({ [url]: { id: 'a1b2c3d4', authorized_paths: ['AGENT.md', 'docs/a.md'] } }, dom.byId);
  sb._btn = dom.btn;
  const saved = await vm.runInContext('window.protectedPathsSaveAuthorised(_btn)', sb);
  const post = sb.__calls.find(c => c.url === url);
  assert.ok(post, 'must POST to set_authorized_paths: ' + JSON.stringify(sb.__calls.map(c => c.url)));
  assert.equal(post.opts.method, 'POST');
  assert.deepEqual(JSON.parse(post.opts.body), { paths: ['docs/a.md', 'AGENT.md'] });
  assert.deepEqual(Array.from(saved), ['AGENT.md', 'docs/a.md']);
  assert.ok(dom.list.innerHTML.includes('<code>AGENT.md</code>') && dom.list.innerHTML.includes('<code>docs/a.md</code>'), 'list should show the saved paths');
  assert.equal(dom.input.value, 'AGENT.md, docs/a.md');
  assert.ok(dom.msg.innerHTML.includes('Authorised paths saved'), 'saved note missing: ' + dom.msg.innerHTML);
  assert.equal(dom.btn.disabled, false, 'button is enabled again');
});

test('an empty field posts an empty list to clear', async () => {
  const dom = editorDom('  ');
  const url = '/api/autonomous/prds/a1b2c3d4/set_authorized_paths';
  const sb = loadApp({ [url]: { id: 'a1b2c3d4' } }, dom.byId);
  sb._btn = dom.btn;
  const saved = await vm.runInContext('window.protectedPathsSaveAuthorised(_btn)', sb);
  assert.deepEqual(JSON.parse(sb.__calls.find(c => c.url === url).opts.body), { paths: [] });
  assert.deepEqual(Array.from(saved), []);
  assert.ok(dom.list.innerHTML.includes('No path is authorised for this Automaton.'));
});

test('a 400 shows the server message, keeps the list and re-enables the button', async () => {
  const dom = editorDom('../outside');
  dom.list.innerHTML = 'unchanged';
  const url = '/api/autonomous/prds/a1b2c3d4/set_authorized_paths';
  const sb = loadApp({ [url]: { status: 400, body: 'path "../outside" must stay inside the repository\n' } }, dom.byId);
  sb._btn = dom.btn;
  const saved = await vm.runInContext('window.protectedPathsSaveAuthorised(_btn)', sb);
  assert.equal(saved, null);
  assert.ok(dom.msg.innerHTML.includes('Could not save the authorised paths: path &quot;../outside&quot; must stay inside the repository')
    || dom.msg.innerHTML.includes('Could not save the authorised paths: path "../outside" must stay inside the repository'), 'server message missing: ' + dom.msg.innerHTML);
  assert.ok(dom.msg.innerHTML.includes('protected-paths-error'));
  assert.equal(dom.list.innerHTML, 'unchanged', 'a failed save leaves the list alone');
  assert.equal(dom.btn.disabled, false, 'button is enabled again');
});

test('a hostile server message is escaped', async () => {
  const dom = editorDom('x');
  const url = '/api/autonomous/prds/a1b2c3d4/set_authorized_paths';
  const sb = loadApp({ [url]: { status: 400, body: HOSTILE } }, dom.byId);
  sb._btn = dom.btn;
  await vm.runInContext('window.protectedPathsSaveAuthorised(_btn)', sb);
  assert.ok(dom.msg.innerHTML.includes(ENTITY_IMG) && !dom.msg.innerHTML.includes('<img'), dom.msg.innerHTML);
});

test('a button with no Automaton id or no field posts nothing', async () => {
  const sb = loadApp({});
  sb._btn = { disabled: false, getAttribute: () => '' };
  assert.equal(await vm.runInContext('window.protectedPathsSaveAuthorised(_btn)', sb), null);
  assert.equal(await vm.runInContext('window.protectedPathsSaveAuthorised(null)', sb), null);
  assert.equal(sb.__calls.length, 0);
});

// ── the settings fields ──────────────────────────────────────────────────────

function section() {
  const sb = loadApp({});
  return { sb, sec: vm.runInContext('GENERAL_CONFIG_FIELDS', sb, { filename: 'app.js' }).find(s => s.id === 'autonomous') };
}

function field(key) {
  return section().sec.fields.find(f => f.key === key);
}

test('the list field is a comma separated text field with a label key and a docs key', () => {
  const f = field('autonomous.automation.protected_paths');
  assert.ok(f, 'protected paths field is declared');
  assert.equal(f.type, 'text');
  assert.equal(f.csv, true, 'reuses the comma separated text field');
  assert.equal(f.labelKey, 'settings_automation_protected_paths');
  assert.equal(f.docsKey, 'settings_protected_paths');
  assert.equal(f.label, bundle('en')[f.labelKey]);
});

test('the action select offers hold, warn, block and off and defaults to hold', () => {
  const f = field('autonomous.automation.protected_paths_action');
  assert.ok(f, 'action field is declared');
  assert.equal(f.type, 'select');
  assert.deepEqual(Array.from(f.options), ['hold', 'warn', 'block', 'off']);
  assert.equal(f.default, 'hold');
  assert.equal(f.labelKey, 'settings_automation_protected_paths_action');
  assert.equal(f.docsKey, 'settings_protected_paths_action');
  assert.equal(f.label, bundle('en')[f.labelKey]);
});

test('the rules review source select offers base_commit and worktree and defaults to base_commit', () => {
  const f = field('autonomous.automation.rules_review_source');
  assert.ok(f, 'rules review source field is declared');
  assert.equal(f.type, 'select');
  assert.deepEqual(Array.from(f.options), ['base_commit', 'worktree']);
  assert.equal(f.default, 'base_commit');
  assert.equal(f.labelKey, 'settings_automation_rules_review_source');
  assert.equal(f.docsKey, 'settings_rules_review_source');
  assert.equal(f.label, bundle('en')[f.labelKey]);
});

test('every option has a label key in the bundle, every field has a docs link, and the group links to the help heading', () => {
  const en = bundle('en');
  const { sb, sec } = section();
  sb.localStorage = { getItem: () => null };
  const keys = ['autonomous.automation.protected_paths', 'autonomous.automation.protected_paths_action', 'autonomous.automation.rules_review_source'];
  const expected = {
    settings_protected_paths: '#protected-paths',
    settings_protected_paths_action: '#protected-paths-action',
    settings_rules_review_source: '#rules-review-source',
  };
  for (const key of keys) {
    const f = sec.fields.find(x => x.key === key);
    for (const o of Array.from(f.options || [])) {
      assert.ok(f.optionLabelKeys[o] && en[f.optionLabelKeys[o]], key + ' option ' + o + ' needs a label key in the bundle');
    }
    assert.ok(en[f.labelKey], key + ' label key missing from the bundle');
    const link = call(sb, 'defsLink(_v)', f.docsKey);
    assert.ok(link.includes('/diagrams.html#docs/datawatch-definitions.md' + expected[f.docsKey]), key + ' help link target: ' + link);
  }
  const group = sec.fields.find(f => f.type === 'group' && f.labelKey === 'settings_automation_protected_paths_group');
  assert.ok(group, 'settings group is declared');
  assert.equal(group.docsKey, 'settings_protected_paths_group');
  assert.ok(call(sb, 'defsLink(_v)', group.docsKey).includes('datawatch-definitions.md#protected-paths"'), 'group help link target');
  assert.ok(call(sb, 'defsLink(_v)', 'automaton_authorised_paths').includes('datawatch-definitions.md#authorised-paths"'), 'authorised editor help link target');
  const order = sec.fields.map(f => f.key || f.labelKey);
  assert.ok(order.indexOf('settings_automation_protected_paths_group') < order.indexOf('autonomous.automation.protected_paths'), 'group precedes its fields');
});

// ── locale keys ──────────────────────────────────────────────────────────────

const REQUIRED_KEYS = [
  'protected_paths_card_title', 'protected_paths_read_only', 'protected_paths_empty', 'protected_paths_held_task',
  'protected_paths_held_automaton', 'protected_paths_chip_tip', 'protected_paths_banner', 'protected_paths_help_link',
  'protected_paths_open_question', 'protected_paths_authorised_title', 'protected_paths_authorised_save',
  'settings_automation_protected_paths_group', 'settings_automation_protected_paths',
  'settings_automation_protected_paths_action', 'settings_automation_rules_review_source',
  'protected_paths_action_hold', 'protected_paths_action_warn', 'protected_paths_action_block', 'protected_paths_action_off',
  'rules_review_source_base_commit', 'rules_review_source_worktree',
];

test('the five bundles carry every required key and every key the PWA uses, with no PRD word or tracker id', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const start = src.indexOf('// ── Automata protected-paths card');
  const end = src.indexOf('// ── Automata uncommitted-work card');
  const used = new Set((src.slice(start, end) + src.slice(src.indexOf('function _automationProtectedPathsLoader'), src.indexOf('function _automationProtectedPathsLoader') + 700))
    .match(/'protected_paths_[a-z_]*[a-z]'/g).map(s => s.slice(1, -1)));
  for (const k of ['protected_paths_action_hold', 'protected_paths_action_warn', 'protected_paths_action_block', 'protected_paths_action_off',
    'rules_review_source_base_commit', 'rules_review_source_worktree']) used.add(k);
  for (const k of REQUIRED_KEYS) used.add(k);
  assert.ok(used.size >= 25, 'expected at least 25 keys, saw ' + used.size);
  const en = bundle('en');
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = bundle(lang);
    for (const k of used) {
      const v = b[k];
      assert.ok(typeof v === 'string' && v.length > 0, lang + ' missing or empty ' + k);
      assert.ok(!/\bPRDs?\b/.test(v), lang + ' ' + k + ' must say Automaton or Automata, never PRD');
      assert.ok(!/\b(?:BL|F|B|S)\d{1,6}\b/.test(v), lang + ' ' + k + ' carries a tracker id');
      assert.equal(v, en[k], lang + ' ' + k + ' carries the English text until translations arrive');
    }
  }
});

test('every protected_paths key the PWA uses is a key of the English bundle', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const start = src.indexOf('// ── Automata protected-paths card');
  const end = src.indexOf('// ── Automata uncommitted-work card');
  const en = bundle('en');
  for (const m of src.slice(start, end).matchAll(/\bt\('([a-z_]+)'/g)) {
    assert.ok(en[m[1]], 'app.js asks for ' + m[1] + ' which the bundle lacks');
  }
});
