'use strict';
// Sessions list group headers show the record's human title, never the bare
// id: an Automaton group header reads the Automaton title with the id as
// secondary small text / tooltip, and falls back to the id only when no title
// is known. Run: node --test internal/server/web/app-sessions-group-titles.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { buildSandbox, loadScript, vm, makeStubElement } = require('./testutil_browser_stub.js');

const WEB_DIR = __dirname;
const NOW = Date.parse('2026-09-02T12:00:00Z');

function freshSandbox() {
  const sandbox = buildSandbox({ byId: { 'view-sessions': makeStubElement(), 'sessionFilterInput': makeStubElement() } });
  vm.createContext(sandbox);
  loadScript(path.join(WEB_DIR, 'app.js'), sandbox);
  const en = JSON.parse(fs.readFileSync(path.join(WEB_DIR, 'locales', 'en.json'), 'utf8'));
  sandbox._i18n.bundle = en;
  sandbox._i18n.fallback = en;
  return sandbox;
}
const run = (sbx, code) => vm.runInContext(code, sbx, { filename: '<group-titles-test>' });

const SESSIONS = [
  { id: 's1', full_id: 'n/s1', name: 'one', state: 'running', prd_id: 'b8a90001', story_id: 'st1', task_id: 't1' },
  { id: 's2', full_id: 'n/s2', name: 'two', state: 'idle', prd_id: 'b8a90001', story_id: 'st1', task_id: 't2' },
];

function headerLabel(html, cls) {
  const re = new RegExp('class="' + cls + '"[\\s\\S]*?<span class="sgp-label">([^<]*)</span>');
  const m = html.match(re);
  return m ? m[1] : null;
}

test('Automaton group header shows the title, id only as secondary text', () => {
  const sbx = freshSandbox();
  run(sbx, "_sessionsGroupTitleMaps().automaton['b8a90001'] = 'Group headers show titles'");
  for (const mode of ['list', 'tiles']) {
    const html = sbx.renderSessionsAutomaton(SESSIONS, NOW, mode);
    assert.equal(headerLabel(html, 'automaton-header'), 'Group headers show titles');
    assert.match(html, /<small class="sgp-id"[^>]*>b8a90001<\/small>/);
    assert.match(html, /title="Group headers show titles \(b8a90001\)"/);
  }
  // Grouped renderer (collapsed summary uses the same header) agrees.
  const g = sbx.renderSessionsGrouped(SESSIONS, { axis: 'automaton', tagPrefix: '' }, NOW, 'list');
  assert.equal(headerLabel(g, 'sessions-group-header'), 'Group headers show titles');
});

test('Automaton group header falls back to the id when no title is known', () => {
  const sbx = freshSandbox();
  const html = sbx.renderSessionsAutomaton(SESSIONS, NOW, 'list');
  assert.equal(headerLabel(html, 'automaton-header'), 'b8a90001');
  assert.doesNotMatch(html, /class="sgp-id"/);
  const g = sbx.renderSessionsGrouped(SESSIONS, { axis: 'automaton', tagPrefix: '' }, NOW, 'tiles');
  assert.equal(headerLabel(g, 'sessions-group-header'), 'b8a90001');
});

test('project group header shows the project name, not the directory', () => {
  const sbx = freshSandbox();
  run(sbx, "_sessionsGroupTitleMaps().project['/p/alpha'] = 'Alpha'");
  const html = sbx.renderSessionsGrouped(
    [{ id: 'a', full_id: 'n/a', name: 'a', state: 'running', project_dir: '/p/alpha' }],
    { axis: 'project', tagPrefix: '' }, NOW, 'list');
  assert.equal(headerLabel(html, 'sessions-group-header'), 'Alpha');
  assert.match(html, /<small class="sgp-id"[^>]*>\/p\/alpha<\/small>/);
});
