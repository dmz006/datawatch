// Resume button on blocked automata parked by the boot-resume gate.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function loadAppJS() {
  const sandbox = buildSandbox({});
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

function header(sandbox, prd) {
  sandbox.__prd = prd;
  const out = vm.runInContext('_renderDetailHeader(__prd, "", "")', sandbox);
  delete sandbox.__prd;
  return out;
}

const PARK = { kind: 'boot_resume_parked', note: 'stale after restart' };

test('Resume button rendered for parked blocked PRD with the reason', () => {
  const html = header(loadAppJS(), { id: 'p1', status: 'blocked', blocked_reason: 'older than the limit', decisions: [PARK] });
  assert.match(html, /prd-resume-btn/);
  assert.match(html, /'resume','POST'/);
  assert.match(html, /older than the limit/);
});

test('Resume button not rendered for other PRDs', () => {
  const sb = loadAppJS();
  assert.doesNotMatch(header(sb, { id: 'p2', status: 'blocked', decisions: [] }), /prd-resume-btn/);
  assert.doesNotMatch(header(sb, { id: 'p3', status: 'running', decisions: [PARK] }), /prd-resume-btn/);
  assert.doesNotMatch(header(sb, { id: 'p4', status: 'blocked', decisions: [PARK, { kind: 'resume' }] }), /prd-resume-btn/);
});
