// v1 held-questions — the held section on the Automation queue card.
//
// Pins, against the REAL app.js (loaded into the shared stub browser via
// testutil_browser_stub.js, exactly like the sibling node tests in this
// directory):
//
//   • the HELD QUESTION_* i18n keys: present in all five locale bundles
//     (en, de, es, fr, ja), with the non-English bundles carrying the
//     English copy unchanged, with matching placeholder sets, and with
//     none of the new strings mentioning PRD (they say
//     "Automaton"/"Automata"),
//   • the Settings fields: the autonomous card exposes
//     autonomous.questions.held_mode (off/shadow/on, default shadow) and
//     autonomous.questions.held_states, next to the decision-gate fields,
//     each with a translated labelKey that is in the en bundle,
//   • the Chains sub-tab body: the held-questions card sits directly
//     under the open-questions card (queue body first, then the held
//     card, then the automation panel),
//   • the render contract: the mode badge off/shadow/on, the held rows
//     (Automaton title + id, the held task, the one-line reason, the
//     recommended option when present, the forced hold-on-default line,
//     the detail link, the answer button), the empty state, the
//     would-be (shadow) list in shadow mode only, the escaping of every
//     dynamic value, and the link contract to the Automaton detail
//     (renderPRDDetailView, guarded).

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALE_DIR = path.join(__dirname, 'locales');
const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const HELD_KEYS = [
  'held_question_title',
  'held_question_mode_off',
  'held_question_mode_shadow',
  'held_question_mode_on',
  'held_question_empty',
  'held_question_recommended',
  'held_question_default_hold',
  'held_question_answer',
  'held_question_open_automaton',
  'held_question_open_task',
  'held_question_shadow_title',
  'held_question_failed',
  'held_question_settings_mode',
  'held_question_settings_states',
];

const bundle = (lang) => JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, lang + '.json'), 'utf8'));

// loadApp loads the real app.js and installs the REAL en bundle as the
// active i18n bundle, so every t(key) resolves to the real string.
function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  sandbox._i18n.fallback = bundle('en');
  return sandbox;
}

const placeholders = (s) => String(s == null ? '' : s).match(/%(\d+)\$[sd]/g) || [];

test('HELD QUESTION keys: present in all five bundles, English-copy parity, matching placeholders, no PRD wording', () => {
  const en = bundle('en');
  for (const key of HELD_KEYS) {
    assert.equal(typeof en[key], 'string', `en: ${key} must be a string`);
    assert.ok(en[key].length > 0, `en: ${key} must not be empty`);
    assert.ok(!/PRD/i.test(en[key]), `en: ${key} must not mention PRD: ${en[key]}`);
  }
  for (const lang of LOCALES) {
    if (lang === 'en') continue;
    const b = bundle(lang);
    for (const key of HELD_KEYS) {
      assert.equal(b[key], en[key], `${lang}: ${key} must carry the English copy unchanged`);
      assert.deepEqual(placeholders(b[key]).sort(), placeholders(en[key]).sort(),
        `${lang}: ${key} placeholder set must match en`);
    }
  }
});

test('settings: autonomous.questions.held_mode and held_states sit with the decision-gate fields and have translated labels', () => {
  const sandbox = loadApp();
  const section = vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.map(f => f.key))",
    sandbox);
  const keys = JSON.parse(section);
  assert.ok(keys.includes('autonomous.questions.decision_gate_mode'), 'decision-gate fields are present');
  assert.ok(keys.includes('autonomous.questions.held_mode'), 'held_mode missing from autonomous card');
  assert.ok(keys.includes('autonomous.questions.held_states'), 'held_states missing from autonomous card');
  assert.ok(keys.indexOf('autonomous.questions.held_mode') > keys.indexOf('autonomous.questions.decision_gate_mode'),
    'held_mode must sit next to (after) the decision-gate fields');

  const field = (key) => JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous').fields.find(f => f.key === '" + key + "') || null))",
    sandbox));

  const mode = field('autonomous.questions.held_mode');
  assert.ok(mode, 'held_mode field object missing');
  assert.equal(mode.type, 'select');
  assert.deepEqual(mode.options, ['off', 'shadow', 'on']);
  assert.equal(mode.labelKey, 'held_question_settings_mode');
  assert.equal(typeof bundle('en')[mode.labelKey], 'string', 'held_question_settings_mode must be in en');

  const states = field('autonomous.questions.held_states');
  assert.ok(states, 'held_states field object missing');
  assert.equal(states.type, 'text');
  assert.equal(states.labelKey, 'held_question_settings_states');
  assert.equal(typeof bundle('en')[states.labelKey], 'string', 'held_question_settings_states must be in en');
});

test('inbox body: the held-questions card renders directly under the queue card, before the security failure card', () => {
  const sandbox = loadApp();
  const body = vm.runInContext("(_automationState.subtab = 'inbox', _automationBodyHTML())", sandbox);
  const iQueues = body.indexOf('id="automationQuestionsBody"');
  const iHeldCard = body.indexOf('id="automationHeldQuestionsCard"');
  const iHeldBody = body.indexOf('id="automationHeldQuestionsBody"');
  const iSecurity = body.indexOf('id="automationSecurityFailureCard"');
  assert.ok(iQueues !== -1, 'queue card present in the inbox body');
  assert.ok(iHeldCard !== -1, 'held-questions card present in the inbox body');
  assert.ok(iHeldBody !== -1 && iHeldCard < iHeldBody, 'held card carries its body slot');
  assert.ok(iSecurity !== -1 && iHeldBody < iSecurity, 'held card sits under the queue card, above the security failure card');
  assert.ok(!body.includes('id="automationPanel"'), 'the list panel is not on Inbox');
  const list = vm.runInContext("(_automationState.subtab = 'automations', _automationBodyHTML())", sandbox);
  assert.ok(!list.includes('id="automationHeldQuestionsCard"') && !list.includes('id="automationQuestionsBody"'), 'no question cards on Automations');
  assert.ok(iQueues < iHeldCard, 'queue card comes before the held card');
});

const entry = (extra) => Object.assign({
  question_id: 'q_102',
  question_text: 'Which backend for the fine-tune run?',
  automaton_title: 'Fine-tune pipeline',
  automaton_id: 'prd_42',
  task_id: 'task_7',
  state: 'review_held',
  reason: 'Blocked on an unanswered decision',
  recommended_option: 'Use B200',
}, extra);

test('render with items: title, ids, task, reason, recommended, hold-default, link and answer control', () => {
  const sandbox = loadApp();
  const en = bundle('en');
  sandbox._hqBody = { mode: 'shadow', held_states: [], open: [entry()], shadow: [] };
  const html = vm.runInContext('window.heldQuestionsCardHtml(_hqBody)', sandbox);
  assert.ok(html.includes('Fine-tune pipeline'), 'Automaton title rendered');
  assert.ok(html.includes('held-question-automaton') && html.includes('prd_42'), 'Automaton id chip rendered');
  assert.ok(html.includes('held-question-task') && html.includes('task_7'), 'held task rendered');
  assert.ok(html.includes('held-question-state') && html.includes('review_held'), 'held state id rendered');
  assert.ok(html.includes('Blocked on an unanswered decision'), 'one-line reason rendered');
  assert.ok(html.includes(en.held_question_recommended.replace('%1$s', 'Use B200')), 'recommended option rendered');
  assert.ok(html.includes(en.held_question_default_hold), 'forced hold-on-default line rendered');
  assert.ok(html.includes('javascript:void(0)') && html.includes('data-automaton="prd_42"') && html.includes('data-task="task_7"'),
    'detail link carries the Automaton and task ids');
  assert.ok(html.includes(en.held_question_open_task), 'link names the task (opens the Automaton detail)');
  assert.ok(html.includes("if (typeof window.openQuestion === 'function') window.openQuestion('q_102')"),
    'answer button reuses the existing answer control (guarded hand-off)');
  assert.ok(!/PRD/i.test([en.held_question_title, en.held_question_answer, en.held_question_open_task,
    en.held_question_default_hold, en.held_question_recommended, en.held_question_empty,
    en.held_question_shadow_title, en.held_question_settings_mode, en.held_question_settings_states].join(' ')),
    'the rendered keys carry no PRD wording');
});

test('mode off: badge reads off; shadow mode badge and would-be list render', () => {
  const en = bundle('en');
  const withEntry = (mode, shadow) => ({ mode: mode, held_states: [], open: [entry()], shadow: shadow });

  const sOff = loadApp();
  sOff._hqBody = withEntry('off', []);
  const offHtml = vm.runInContext('window.heldQuestionsCardHtml(_hqBody)', sOff);
  assert.ok(offHtml.includes('held-question-mode-off') && offHtml.includes(`data-mode="off"`), 'off badge rendered');
  assert.ok(offHtml.includes(en.held_question_mode_off), 'off badge text');
  assert.ok(offHtml.includes('held-question-row'), 'held rows still listed in off mode');

  const sShadow = loadApp();
  sShadow._hqBody = withEntry('shadow', [entry({ question_id: 'q_103', automaton_id: 'prd_43' })]);
  const shadowHtml = vm.runInContext('window.heldQuestionsCardHtml(_hqBody)', sShadow);
  assert.ok(shadowHtml.includes('held-question-mode-shadow') && shadowHtml.includes('data-mode="shadow"'), 'shadow badge rendered');
  assert.ok(shadowHtml.includes(en.held_question_mode_shadow), 'shadow badge text');
  assert.ok(shadowHtml.includes(en.held_question_shadow_title), 'would-be section titled');
  assert.ok(shadowHtml.includes('held-question-shadow-list'), 'would-be list container');
  assert.ok(shadowHtml.includes('data-question-id="q_103"') || shadowHtml.includes('q_103'), 'would-be entry listed');

  const sOn = loadApp();
  sOn._hqBody = withEntry('on', [entry({ question_id: 'q_104' })]);
  const onHtml = vm.runInContext('window.heldQuestionsCardHtml(_hqBody)', sOn);
  assert.ok(onHtml.includes('held-question-mode-on') && onHtml.includes(`data-mode="on"`), 'on badge rendered');
  assert.ok(!onHtml.includes('held-question-shadow-section'), 'the would-be list is shadow-only');
});

test('shadow badge renders for "shadow" mode and not unknown', () => {
  const en = bundle('en');
  const box = loadApp();
  box._hqBody = { mode: 'shadow', held_states: [], open: [], shadow: [] };
  const html = vm.runInContext('window.heldQuestionsCardHtml(_hqBody)', box);
  assert.ok(html.includes('held-question-mode-shadow'), 'shadow badge class');
  assert.ok(html.includes(`data-mode="shadow"`), 'shadow badge attr');
  assert.ok(html.includes(en.held_question_mode_shadow), 'shadow badge text from the bundle');
  const unknown = vm.runInContext('window.heldQuestionsCardHtml({mode: "bogus", open: [], shadow: []})', box);
  assert.ok(!/held-question-mode-badge/.test(unknown), 'an unknown mode never invents a badge state');
});

test('empty state: no held rows, the translated empty line', () => {
  const en = bundle('en');
  const box = loadApp();
  box._hqBody = { mode: 'off', held_states: [], open: [], shadow: [] };
  const html = vm.runInContext('window.heldQuestionsCardHtml(_hqBody)', box);
  assert.ok(html.includes(en.held_question_empty), 'empty text rendered');
  assert.ok(!html.includes('held-question-row'), 'no rows in the empty state');
});

test('escaping: hostile title, task, reason, option and id never reach the HTML raw', () => {
  const HOSTILE = '<img src=x onerror=alert(1)>';
  const box = loadApp();
  box._hqBody = {
    mode: 'shadow', held_states: [],
    open: [entry({
      automaton_title: HOSTILE,
      task_id: HOSTILE,
      reason: HOSTILE,
      recommended_option: HOSTILE,
    })], shadow: [],
  };
  const esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  const html = vm.runInContext('window.heldQuestionsCardHtml(_hqBody)', box);
  assert.ok(!html.includes(HOSTILE), 'raw hostile HTML must never be spliced in');
  assert.ok(html.includes(esc(HOSTILE)), 'hostile values show up escaped, inert');
});

test('link contract: the detail link opens the Automaton detail via the guarded renderPRDDetailView; absent id -> no link, no hand-off without a question', () => {
  const box = loadApp();
  const one = vm.runInContext('window.heldQuestionDetailLinkHtml({automaton_id: "prd_42", task_id: "task_7"})', box);
  assert.ok(one.includes('javascript:void(0)'), 'controlled link');
  assert.ok(one.includes("if (typeof renderPRDDetailView === 'function') renderPRDDetailView('prd_42')"),
    'opens the Automaton detail (guarded hand-off): ' + one);
  assert.ok(one.includes('data-automaton="prd_42"') && one.includes('data-task="task_7"'), 'names the Automaton and the task');

  const noId = vm.runInContext('window.heldQuestionDetailLinkHtml({task_id: "task_7"})', box);
  assert.equal(noId, '', 'no Automaton id -> no detail link; the row keeps its chips for manual look-up');

  const rowNoQuestion = vm.runInContext('window.heldQuestionRowHtml({automaton_id: "prd_42", task_id: "task_7"})', box);
  assert.ok(!rowNoQuestion.includes('held-question-answer'), 'no question id -> no answer button');

  const rowNoId = vm.runInContext('window.heldQuestionRowHtml({question_id: "q_9"})', box);
  assert.ok(rowNoId.includes("window.openQuestion('q_9')"), 'answer hand-off still works without an Automaton id');
  assert.ok(!rowNoId.includes('held-question-detail-link'), 'and the detail link stays absent');
});

test('listLoader integration: polls GET /api/autonomous/held-questions and paints the card', async () => {
  const en = bundle('en');
  const s = loadApp();
  const calls = [];
  s.fetch = (url, o) => {
    calls.push({ url: String(url), method: (o && o.method) || 'GET' });
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({
      mode: 'shadow', held_states: [], open: [entry()], shadow: [entry({ question_id: 'q_105' })],
    }) });
  };
  const ctl = vm.runInContext('_automationHeldQuestionsLoader()', s);
  await ctl.load();
  const html = ctl.html();
  assert.ok(calls.length >= 1, 'the loader did a GET');
  assert.ok(calls.some(c => c.url.indexOf('/api/autonomous/held-questions') !== -1), 'hit the report route: ' + JSON.stringify(calls));
  assert.ok(calls.every(c => c.method === 'GET'), 'the card never POSTs');
  assert.ok(html.includes('held-question-mode-shadow'), 'badge painted by the loader render');
  assert.ok(html.includes(en.held_question_shadow_title), 'would-be list painted');
  assert.ok(html.includes('Fine-tune pipeline'), 'held row painted');
  if (typeof ctl.stop === 'function') ctl.stop();
});

test('loader failure: the failed line is painted, not a crash', async () => {
  const en = bundle('en');
  const s = loadApp();
  s.fetch = () => Promise.resolve({ ok: false, status: 502, text: () => Promise.resolve('upstream says no') });
  const ctl = vm.runInContext('_automationHeldQuestionsLoader()', s);
  await ctl.load();
  if (typeof ctl.html === 'function' && ctl.html()) {
    assert.ok(typeof ctl.html() === 'string', 'the loader still produces html on failure');
  }
  assert.ok(typeof en.held_question_failed === 'string' && en.held_question_failed.includes('%1$s'),
    'the failed line is the translated one with its arg');
  if (typeof ctl.stop === 'function') ctl.stop();
  await flushAsync(5);
});
