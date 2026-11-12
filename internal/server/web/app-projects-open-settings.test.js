// Projects page — the Settings links open the tab and section that own the
// setting: _projectsOpenSettings stores the tab, expands a collapsed section and
// scrolls it into view; the merge link targets the merge pipeline section.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

const APP = path.join(__dirname, 'app.js');

function load() {
  const target = makeStubElement(() => {});
  target.style = { display: 'none' };
  let scrolled = 0;
  target.scrollIntoView = () => { scrolled++; };
  const sandbox = buildSandbox({ byId: { 'settings-sec-project_policy': target, 'settings-sec-gc_autonomous': target } });
  // The stub's setTimeout never fires; run callbacks on the next tick instead.
  sandbox.setTimeout = (fn) => { process.nextTick(fn); return 0; };
  vm.createContext(sandbox);
  loadScript(APP, sandbox);
  sandbox.__navigated = [];
  vm.runInContext("navigate = function (v) { __navigated.push(v); }", sandbox);
  return { sb: sandbox, target, scrolls: () => scrolled };
}

const wait = ms => new Promise(r => setTimeout(r, ms));

test('opening the policy section stores the automata tab, expands it and scrolls to it', async () => {
  const { sb, target, scrolls } = load();
  vm.runInContext("settingsCollapsed['project_policy'] = true; _settingsTab = 'about'", sb);
  vm.runInContext("_projectsOpenSettings('project_policy')", sb);
  await wait(50);
  assert.equal(vm.runInContext('_settingsTab', sb), 'automata');
  assert.equal(sb.localStorage.getItem('cs_settings_tab'), 'automata');
  assert.deepEqual(Array.from(sb.__navigated), ['settings']);
  assert.equal(vm.runInContext("settingsCollapsed['project_policy']", sb), false, 'collapsed flag cleared');
  assert.equal(target.style.display, '', 'rendered section shown');
  assert.ok(scrolls() >= 1, 'scrollIntoView called on settings-sec-project_policy');
});

test('the merge pipeline link targets the merge pipeline settings section', () => {
  const { sb } = load();
  const id = vm.runInContext('MERGE_PIPELINE_SETTINGS_SECTION', sb);
  assert.equal(id, 'gc_autonomous');
  const html = vm.runInContext("_projectsSettingsLinkHTML(MERGE_PIPELINE_SETTINGS_SECTION, 'x')", sb);
  assert.ok(html.includes('gc_autonomous'), 'link names the section');
  const mp = vm.runInContext("_projectMergePipelineHTML({ source: 'file' }, '')", sb);
  assert.ok(mp.includes('gc_autonomous'), 'merge pipeline block links to it');
  const pol = vm.runInContext("_projectPolicyEffectiveHTML({ policy: {} }, '')", sb);
  assert.ok(pol.includes('project_policy'), 'policy block links to the policy section');
});
