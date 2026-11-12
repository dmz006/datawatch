// The Inbox counter and lists follow the Projects selection.
//
// Pins, against the REAL app.js loaded into the shared stub browser:
//   • _inboxItemInProject takes the project link (system tag) first and the
//     directory only as the fallback; an unlinked item matches no project,
//   • with a project selected the count and each filtered list agree,
//   • the Overview keeps everything and says it counts all projects,
//   • the all-projects locale key exists in all five bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const bundle = (lang) => JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = bundle('en');
  sandbox._i18n.fallback = bundle('en');
  return sandbox;
}
const run = (s, code) => vm.runInContext(code, s);

test('item matching: link first, directory fallback, unlinked matches nothing', () => {
  const s = loadApp();
  run(s, "_automataState.allPrds = [{id:'p1', project_dir:'/w/alpha'}, {id:'p2', system_tags:['project:beta'], project_dir:'/w/alpha'}]");
  const alpha = { name: 'alpha', dir: '/w/alpha/' };
  const beta = { name: 'beta', dir: '/w/beta' };
  const m = (item, p) => run(s, '_inboxItemInProject(' + JSON.stringify(item) + ',' + JSON.stringify(p) + ')');
  assert.equal(m({ system_tags: ['project:alpha'] }, alpha), true);
  assert.equal(m({ system_tags: ['project:alpha'] }, beta), false);
  assert.equal(m({ prd_id: 'p1' }, alpha), true, 'directory fallback through the Automaton');
  assert.equal(m({ prd_id: 'p2' }, alpha), false, 'the link wins over the directory');
  assert.equal(m({ prd_id: 'p2' }, beta), true);
  assert.equal(m({ repo: 'me/alpha' }, alpha), true);
  assert.equal(m({}, alpha), false);
  assert.equal(m({ prd_id: 'nope' }, alpha), false);
});

test('project scope filters count and list alike; Overview keeps all', () => {
  const s = loadApp();
  run(s, "_automataState.allPrds = [{id:'p1', system_tags:['project:alpha']}, {id:'p2', system_tags:['project:beta']}]");
  const triage = { records: [{ prd_id: 'p1' }, { prd_id: 'p2' }, { prd_id: 'zz' }] };
  const held = { open: [{ question_id: 'a', system_tags: ['project:alpha'] }, { question_id: 'b' }] };
  assert.equal(run(s, '_inboxScope().kind'), 'overview');
  assert.equal(run(s, "_automationInboxCountOf('automation_triage'," + JSON.stringify(triage) + ')'), 3);
  assert.match(run(s, '_automationBadgeLabel(3)'), /all projects/);

  run(s, "_projectsState.list = [{name:'alpha', dir:'/w/alpha'}]; _projectsState.selected = 'alpha'; state.activeView = 'autonomous'; _automataState.tab = 'projects';");
  assert.equal(run(s, '_inboxScope().kind'), 'project');
  assert.equal(run(s, "_automationInboxCountOf('automation_triage'," + JSON.stringify(triage) + ')'), 1);
  assert.equal(run(s, "_inboxScopeData('automation_triage'," + JSON.stringify(triage) + ').records.length'), 1);
  assert.equal(run(s, "_automationInboxCountOf('automation_held_questions'," + JSON.stringify(held) + ')'), 1);
  assert.equal(run(s, "_automationInboxCountOf('automation_security_failure',[{repo:'x/alpha'},{repo:'x/other'}])"), 1);
  assert.equal(run(s, "_automationInboxCountOf('supervisor',{findings:[{prd_id:'p1'},{prd_id:'p2'},{prd_id:'p1',resolved:true}]})"), 1);
  assert.equal(run(s, "_automationInboxCountOf('automation_questions',{questions:[{system_tags:['project:beta']},{system_tags:['project:alpha']}]})"), 1);
  assert.doesNotMatch(run(s, '_automationBadgeLabel(1)'), /all projects/);
});

test('all-projects badge label exists in every bundle with the count slot', () => {
  for (const l of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = bundle(l);
    assert.ok(b.automation_subtab_inbox_badge_label_all && b.automation_subtab_inbox_badge_label_all.includes('%1$s'), l);
  }
});
