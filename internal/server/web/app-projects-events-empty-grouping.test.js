// Projects Events sub-tab — empty/never-state lines are grouped.
//
// A line whose only content is that something is empty, absent, unset or has
// never happened ("X has no sessions yet", "Plan X has no Automata yet", "No
// plans…", "No unassigned Automata.", "X is not set.") is listed one per line
// only while "Show empty lines and sections" is on. With it off, one summary
// line carries the count. Informative events always keep their own line, and
// the summary never replaces them.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const KEY = 'projects_event_empty_summary';

function load(showEmpty) {
  const view = makeStubElement(() => {});
  view.insertAdjacentHTML = () => {};
  const sandbox = buildSandbox({ byId: { automataPanel: view, view } });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  if (showEmpty !== undefined) vm.runInContext(`_projectsShowEmpty = function () { return ${showEmpty}; }`, sandbox);
  return sandbox;
}

// eventsHTML renders the Events sub-tab body of a project page.
function eventsHTML(sb, p, roll, rollErr) {
  const orig = sb._projectsActiveSubtab;
  sb._projectsActiveSubtab = () => 'events';
  try { return sb._projectPageHTML(p, roll, rollErr || '', null); } finally { sb._projectsActiveSubtab = orig; }
}
function eventLines(html) {
  const out = [];
  const re = /<div class="(project-event-line[^"]*)"[^>]*>([^<]*)<\/div>/g;
  let m;
  while ((m = re.exec(html))) out.push({ cls: m[1], text: m[2] });
  return out;
}

const N = 12;
const PROJECT = { name: 'acme', dir: '/w/acme', weekly_share: 10 };
function manyBare() {
  const automata = [];
  for (let i = 0; i < N; i++) automata.push({ id: 'p' + i, title: 'Auto ' + i, session_ids: [] });
  return { arcs: [{ automation_id: 'a1', priority: 1, automata }], unassigned: { automata: [{ id: 'u1', title: 'Loose', session_ids: ['s-1'] }] } };
}

test('off: many Automata with no sessions give no per-line entry and one counted summary', () => {
  const sb = load(false);
  const html = eventsHTML(sb, PROJECT, manyBare());
  assert.ok(!html.includes('has no sessions yet'), 'no individual no-sessions line');
  const lines = eventLines(html);
  const summary = lines.filter(l => l.cls.includes('project-event-summary'));
  assert.equal(summary.length, 1, 'exactly one summary line');
  assert.equal(lines.length, 1, 'the summary is the only line');
  assert.match(summary[0].text, new RegExp('^' + N + ' empty or not-started items hidden\\. '));
  assert.ok(summary[0].text.includes('Show empty lines and sections'), 'names the checkbox');
});

test('off: informative events remain and the summary does not replace them', () => {
  const sb = load(false);
  const html = eventsHTML(sb, PROJECT, null, 'boom');
  const lines = eventLines(html);
  assert.ok(lines.some(l => /Could not load the roll-up: boom/.test(l.text)), 'the roll-up failure line stays');
  assert.ok(!lines.some(l => l.cls.includes('project-event-summary')), 'no summary when nothing is empty');
  // Mixed: an informative event plus empty ones.
  const roll = manyBare();
  const bare = { name: 'acme', dir: '/w/acme' };
  const mixedLines = eventLines(eventsHTML(sb, bare, roll));
  const sum = mixedLines.filter(l => l.cls.includes('project-event-summary'));
  assert.equal(sum.length, 1);
  assert.match(sum[0].text, new RegExp('^' + (N + 1) + ' empty'), 'N no-sessions lines plus the unset weekly share');
  assert.ok(!mixedLines.some(l => /not set\.|no sessions yet/.test(l.text)));
});

test('off: the summary line wraps on a phone and shares the event-line class', () => {
  const sb = load(false);
  const html = eventsHTML(sb, PROJECT, manyBare());
  const m = /<div class="project-event-line project-event-summary" style="([^"]*)"/.exec(html);
  assert.ok(m, 'summary line present with both classes');
  assert.ok(/overflow-wrap:\s*anywhere/.test(m[1]), 'overflow-wrap:anywhere on the line itself');
  const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');
  assert.ok(/\.project-event-line[^{]*\{[^}]*overflow-wrap:\s*anywhere/.test(css), 'the class rule wraps too');
});

test('off: with only empty-state events the Events sub-tab still exists', () => {
  const sb = load(false);
  const html = sb._projectPageHTML(PROJECT, { arcs: [], unassigned: { automata: [] } }, '', null);
  assert.ok(html.includes('data-projects-subtab="events"'), 'the Events tab is offered for the summary');
});

test('on: every empty-state line is listed and there is no summary line', () => {
  const sb = load(true);
  const html = eventsHTML(sb, { name: 'acme', dir: '/w/acme' }, manyBare());
  const lines = eventLines(html);
  assert.equal(lines.filter(l => /has no sessions yet\.$/.test(l.text)).length, N, 'one line per Automaton');
  for (let i = 0; i < N; i++) assert.ok(lines.some(l => l.text === 'Auto ' + i + ' has no sessions yet.'));
  assert.ok(lines.some(l => l.text === 'Weekly share is not set.'));
  assert.ok(!lines.some(l => l.cls.includes('project-event-summary')), 'no summary line');
  assert.ok(!html.includes('items hidden'), 'no summary text');
});

test('on: every empty-state kind is listed one per line', () => {
  const sb = load(true);
  const html = eventsHTML(sb, PROJECT, { arcs: [], unassigned: { automata: [] } });
  assert.ok(html.includes('No plans are planned for this project.'));
  assert.ok(html.includes('No unassigned Automata.'));
  const arcEmpty = eventsHTML(sb, PROJECT, { arcs: [{ automation_id: 'auto-e', priority: 1, automata: [] }], unassigned: { automata: [{ id: 'u', session_ids: ['s'] }] } });
  assert.ok(arcEmpty.includes('Plan auto-e has no Automata yet.'));
});

test('toggling the checkbox re-renders the Events sub-tab', () => {
  const sb = load();
  let painted = 0;
  sb._projectsPaint = () => { painted++; };
  const render = () => eventLines(eventsHTML(sb, PROJECT, manyBare()));
  assert.equal(render().length, 1, 'off by default: one summary line');
  sb.toggleProjectsShowEmpty(true);
  assert.equal(painted, 1, 'toggle repaints');
  assert.equal(render().length, N, 'on: one line per Automaton');
  sb.toggleProjectsShowEmpty(false);
  assert.equal(painted, 2);
  assert.equal(render().length, 1, 'off again: back to the summary');
});

test('the summary key exists in all five locales, translated, with the count placeholder', () => {
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'))[KEY];
  assert.ok(en && en.includes('%1$d'), 'en has the %1$d placeholder');
  for (const l of LOCALES) {
    const raw = fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8');
    const bundle = JSON.parse(raw);
    assert.equal(typeof bundle[KEY], 'string', l + ' has ' + KEY);
    assert.ok(bundle[KEY].includes('%1$d'), l + ' keeps the count placeholder');
    if (l !== 'en') assert.notEqual(bundle[KEY], en, l + ' is translated, not an English copy');
    const keys = Object.keys(bundle);
    const i = keys.indexOf(KEY);
    assert.equal(keys[i - 1], 'projects_event_automaton_no_sessions', l + ' key order next to the projects_event_* keys');
  }
});
