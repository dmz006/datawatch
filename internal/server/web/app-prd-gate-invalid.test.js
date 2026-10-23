// Quality-gate invalid status — the PWA must surface a PRD whose
// quality-gate baseline is invalid: renderAutomataCard / renderPRDRow
// show a visible badge with the plain reason, and the Monitor's
// quality-gate stats card adds an invalid line driven by the
// quality_gate_invalid count. Invalid gates are
// never blockers, so the badge is a warning, not an error.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  // Point t() at the real EN bundle so rendered text uses the shipped
  // translations (the test sandbox never fetches locales itself).
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  sandbox._i18n.fallback = en;
  sandbox._i18n.bundle = en;
  sandbox._i18n.ready = true;
  return sandbox;
}

function prdJson(patch) {
  return JSON.stringify(Object.assign({
    id: 'p1', title: 'Demo automaton', status: 'running', stories: [],
  }, patch));
}

test('renderAutomataCard shows the invalid quality-gate badge with the plain reason', () => {
  const sandbox = loadAppJS();
  const html = vm.runInContext(
    'renderAutomataCard(' + prdJson({
      quality_gate_status: 'invalid',
      quality_gate_invalid_reason: 'gate command produced no output',
    }) + ')', sandbox);
  assert.ok(html.includes('gate-invalid-badge'), 'missing invalid-gate badge class:\n' + html);
  assert.ok(html.includes('Quality gate invalid'), 'badge must use the prd_gate_invalid label');
  assert.ok(html.includes('gate command produced no output'), 'badge must show the plain reason');
});

test('renderAutomataCard omits the invalid-gate badge for a valid gate', () => {
  const sandbox = loadAppJS();
  const html = vm.runInContext('renderAutomataCard(' + prdJson({}) + ')', sandbox);
  assert.ok(!html.includes('gate-invalid-badge'), 'no badge when quality_gate_status is absent:\n' + html);
  const ok = vm.runInContext('renderAutomataCard(' + prdJson({ quality_gate_status: 'valid' }) + ')', sandbox);
  assert.ok(!ok.includes('gate-invalid-badge'), 'no badge when the gate is explicitly valid');
});

test('renderPRDRow shows the invalid quality-gate badge in the PRD header', () => {
  const sandbox = loadAppJS();
  const html = vm.runInContext(
    'renderPRDRow(' + prdJson({
      quality_gate_status: 'invalid',
      quality_gate_invalid_reason: 'x <script>alert(1)</script>',
    }) + ')', sandbox);
  assert.ok(html.includes('gate-invalid-badge'), 'missing invalid-gate badge class:\n' + html);
  assert.ok(html.includes('Quality gate invalid'), 'badge must use the prd_gate_invalid label');
  assert.ok(!html.includes('<script>alert(1)</script>'), 'reason must be html-escaped');
  assert.ok(html.includes('&lt;script&gt;'), 'reason must appear escaped');
});

test('renderStatsData shows an invalid-gate line with the count in the quality-gates card', () => {
  const sandbox = loadAppJS();
  const el = makeStubElement();
  el.closest = () => null;
  sandbox.scrollTo = () => {};
  sandbox.__el = el;
  sandbox.__data = {
    timestamp: Date.now(), cpu_load_avg_1: 0.1, cpu_cores: 4,
    mem_used: 1, mem_total: 2, disk_used: 1, disk_total: 2,
    daemon_rss_bytes: 1, goroutines: 1, open_fds: 1, uptime_seconds: 1,
    quality_gate_runs: 3, quality_gate_pass: 1, quality_gate_fail: 1,
    quality_gate_invalid: 1,
  };
  vm.runInContext('renderStatsData(__el, __data)', sandbox);
  delete sandbox.__el;
  delete sandbox.__data;
  const html = el.innerHTML;
  assert.ok(html.includes('Quality gate invalid'), 'stats card must show the invalid gate line');
  assert.match(html, /Quality gate invalid<\/span><span[^>]*>1<\/span>/, 'invalid line must carry the count');
});

test('renderStatsData omits the invalid-gate line when the gate is valid', () => {
  const sandbox = loadAppJS();
  const el = makeStubElement();
  el.closest = () => null;
  sandbox.scrollTo = () => {};
  sandbox.__el = el;
  sandbox.__data = {
    timestamp: Date.now(), cpu_load_avg_1: 0.1, cpu_cores: 4,
    mem_used: 1, mem_total: 2, disk_used: 1, disk_total: 2,
    daemon_rss_bytes: 1, goroutines: 1, open_fds: 1, uptime_seconds: 1,
    quality_gate_runs: 2, quality_gate_pass: 2, quality_gate_fail: 0,
  };
  vm.runInContext('renderStatsData(__el, __data)', sandbox);
  delete sandbox.__el;
  delete sandbox.__data;
  assert.ok(!el.innerHTML.includes('Quality gate invalid'), 'no invalid line for a valid gate:\n' + el.innerHTML);
});
