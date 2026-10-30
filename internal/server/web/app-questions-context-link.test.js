'use strict';
// The PWA question answer dialog shows what the question is about.
//
// window._questionAnswerBodyHTML reads the context_ref object the question
// read carries ({kind, title, id, doc_path, viewer_url}) and, below the
// question text, shows "This question is about: <title>" with an open
// control. A proposal gives a link into the documents viewer (new tab, only
// for a same-origin relative viewer_url); an Automaton gets an open button
// on the Automata open-by-id helper; a task and a registry row show title,
// kind and id as text; kind none shows nothing. Everything is escaped, and
// both new keys are in all five bundles.
//
// Run: env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-questions-context-link.test.js

const fs = require('fs');
const path = require('path');
const test = require('node:test');
const assert = require('node:assert');
const vm = require('vm');
const { buildSandbox, loadScript, flushAsync } = require(path.join(__dirname, 'testutil_browser_stub.js'));

const APP_JS = path.join(__dirname, 'app.js');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const bundles = {};
LANGS.forEach(l => { bundles[l] = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8')); });
const en = bundles.en;
const KEYS = ['automation_questions_context_label', 'automation_questions_context_open'];
const HOSTILE = '<img src=x onerror=alert(1)>';

const Q = { id: 'q7', label: 'Scope', text: 'Which way?', options: [], state: 'open' };

function makeSandbox(question) {
  const sandbox = buildSandbox({ byId: {} });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = en;
  sandbox._modals = [];
  sandbox.showModal = (opts) => { sandbox._modals.push(opts); };
  sandbox.fetch = () => Promise.resolve({
    ok: true, status: 200,
    json: () => Promise.resolve({ question, answer: null, answer_count: 0, current_answer_id: '' }),
  });
  return sandbox;
}

const bodyFor = (ref, extra) => makeSandbox(Q)._questionAnswerBodyHTML(Object.assign({}, Q, { context_ref: ref }, extra), null);
const label = (title) => en.automation_questions_context_label.replace('%1$s', title);

test('both new keys are in all five bundles', () => {
  LANGS.forEach(l => KEYS.forEach(k => {
    assert.ok(typeof bundles[l][k] === 'string' && bundles[l][k], l + ' has ' + k);
  }));
  assert.ok(en.automation_questions_context_label.includes('%1$s'), 'label takes the title');
  LANGS.forEach(l => assert.ok(!/\bPRD\b/.test(bundles[l].automation_questions_context_label + bundles[l].automation_questions_context_open), 'no PRD wording'));
});

test('a proposal gives a viewer link that opens in a new tab', () => {
  const url = '/diagrams.html#docs/plans/2026-10-17-example.md';
  const html = bodyFor({ kind: 'proposal', title: '2026-10-17-example.md', id: '', doc_path: 'docs/plans/2026-10-17-example.md', viewer_url: url });
  assert.ok(html.includes(label('2026-10-17-example.md')), 'context line');
  assert.ok(html.includes('href="' + url + '"'), 'viewer_url exactly as given');
  assert.ok(html.includes('target="_blank"'), 'new tab');
  assert.ok(html.includes('rel="noopener noreferrer"'), 'no opener');
  assert.ok(html.includes('>' + en.automation_questions_context_open + '</a>'), 'open control text');
  assert.ok(html.indexOf('Which way?') < html.indexOf('question-answer-context'), 'below the question text');
});

test('a proposal with no viewer_url shows the title without a link', () => {
  const html = bodyFor({ kind: 'proposal', title: 'notes.md', id: '', doc_path: 'notes.md', viewer_url: '' });
  assert.ok(html.includes(label('notes.md')));
  assert.ok(!html.includes('<a '), 'no link');
});

test('an Automaton gets an open button on the Automata helper', () => {
  const s = makeSandbox(Q);
  const calls = [];
  s.openAutomationMemberPRD = (id) => { calls.push(id); };
  const html = s._questionAnswerBodyHTML(Object.assign({}, Q, { context_ref: { kind: 'automaton', title: 'Nightly build', id: 'a1b2c3d4' } }), null);
  assert.ok(html.includes(label(en.automation_kind_prd + ' Nightly build (a1b2c3d4)')), 'title with kind and id');
  assert.ok(html.includes('<button'), 'open button');
  assert.ok(html.includes('questionContextOpenAutomaton(&quot;a1b2c3d4&quot;)'), 'button calls the open helper with the id');
  assert.ok(!html.includes('<a '), 'not a link');
  s._modals.length = 0;
  vm.runInContext('window.questionContextOpenAutomaton("a1b2c3d4")', s);
  assert.deepStrictEqual(calls, ['a1b2c3d4'], 'helper called with the id');
});

test('an Automaton with no open-by-id helper shows title and id as text', () => {
  const s = makeSandbox(Q);
  vm.runInContext('window.openAutomationMemberPRD = undefined', s);
  const html = s._questionAnswerBodyHTML(Object.assign({}, Q, { context_ref: { kind: 'automaton', title: 'Nightly build', id: 'a1b2c3d4' } }), null);
  assert.ok(html.includes('Nightly build (a1b2c3d4)'));
  assert.ok(!html.includes('<button') && !html.includes('<a '));
});

test('task and registry row show title and kind as text', () => {
  const task = bodyFor({ kind: 'task', title: 'Write the tests', id: 't-9' });
  assert.ok(task.includes(label(en.status_automaton_task + ' Write the tests (t-9)')));
  assert.ok(!task.includes('<a ') && !task.includes('questionContextOpenAutomaton'));
  const row = bodyFor({ kind: 'registry_row', title: 'row-key', id: 'row-key' });
  assert.ok(row.includes(label(en.overlap_warning_registry + ' row-key')));
  assert.ok(!row.includes('(row-key)'), 'id not repeated when it equals the title');
  assert.ok(!row.includes('<a '));
});

test('kind none, a missing context_ref, and a bad shape show nothing', () => {
  [{ kind: 'none', title: '', id: '' }, undefined, null, 'x', {}].forEach(ref => {
    const html = bodyFor(ref);
    assert.ok(!html.includes('question-answer-context'), 'no context line for ' + JSON.stringify(ref));
    assert.ok(!html.includes(en.automation_questions_context_label.split('%1$s')[0]));
  });
});

test('an unsafe viewer_url is not rendered as a link', () => {
  const bad = ['https://evil.example/x', '//evil.example/x', 'javascript:alert(1)', '/ok\\evil', '/a b', '/a\nb', 'diagrams.html', ''];
  bad.forEach(url => {
    const html = bodyFor({ kind: 'proposal', title: 'p.md', id: '', doc_path: 'docs/p.md', viewer_url: url });
    assert.ok(!html.includes('<a '), 'no link for ' + JSON.stringify(url));
    assert.ok(!html.includes('href='), 'no href for ' + JSON.stringify(url));
    assert.ok(html.includes(label('p.md')), 'title still shown');
  });
});

test('every value is escaped', () => {
  const s = makeSandbox(Q);
  s.openAutomationMemberPRD = () => {};
  const out = [
    s._questionAnswerBodyHTML(Object.assign({}, Q, { context_ref: { kind: 'proposal', title: HOSTILE, id: '', viewer_url: '/diagrams.html#docs/a"onmouseover="x.md' } }), null),
    s._questionAnswerBodyHTML(Object.assign({}, Q, { context_ref: { kind: 'automaton', title: HOSTILE, id: '"><script>x</script>' } }), null),
    s._questionAnswerBodyHTML(Object.assign({}, Q, { context_ref: { kind: 'task', title: HOSTILE, id: HOSTILE } }), null),
  ];
  out.forEach(html => {
    assert.ok(!html.includes(HOSTILE), 'no raw markup');
    assert.ok(!html.includes('<script>'), 'no raw script');
    assert.ok(!html.includes('onmouseover="x'), 'no attribute break-out');
  });
  assert.ok(out[0].includes('&lt;img src=x onerror=alert(1)&gt;'), 'title escaped text');
});

test('openQuestion passes the context_ref of the read into the dialog body', async () => {
  const s = makeSandbox(Object.assign({}, Q, { context_ref: { kind: 'task', title: 'Write the tests', id: 't-9' } }));
  await vm.runInContext('window.openQuestion("q7")', s).finally(() => flushAsync());
  assert.equal(s._modals.length, 1);
  assert.ok(s._modals[0].body.includes(label(en.status_automaton_task + ' Write the tests (t-9)')));
});
