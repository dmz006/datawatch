const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

// ── pi progress block (Automata task card; session strip removed) ──────
// Loads the REAL app.js into a stub browser context (testutil_browser_stub)
// and exercises the pure rendering helper `renderPiProgressBlock` plus the
// poll-stacking guard on window._piProgressInterval.

const PROG_KEYS = [
  'pi_progress_loading',
  'pi_progress_last_event',
  'pi_progress_turns',
  'pi_progress_tools',
  'pi_progress_last_tool',
  'pi_progress_summary',
  'pi_progress_tokens',
  'pi_progress_looping',
];

function newSandbox() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

const SAMPLE_PROGRESS = (extra = '') =>
  `({
    last_event_at: new Date(Date.now() - 90000).toISOString(),
    assistant_turns: 3,
    tool_calls: { bash: 1, read: 4, edit: 2 },
    last_tool_name: 'edit',
    last_summary: 'fix the flaky test <img src=x onerror=alert(1)> & friends',
    tokens: 12345
  })`;

test('renderPiProgressBlock renders age, turns, sorted tool counts, last tool, intent, tokens, looping badge', () => {
  const sandbox = newSandbox();
  const html = vm.runInContext(
    `renderPiProgressBlock(${SAMPLE_PROGRESS()}, 'suspected repeating tool-call sequence')`,
    sandbox,
  );
  assert.match(html, /pi-progress-inline/);
  // relative age of the last event (90s → "1m ago" bucket)
  assert.match(html, /1m ago/);
  assert.match(html, /3/); // 3 assistant turns
  // tool-call counts by name, sorted (bash < edit < read)
  assert.match(html, /bash 1/);
  assert.match(html, /edit 2/);
  assert.match(html, /read 4/);
  assert.ok(html.indexOf('bash 1') < html.indexOf('edit 2'), 'tool counts sorted by name');
  assert.ok(html.indexOf('edit 2') < html.indexOf('read 4'), 'tool counts sorted by name');
  assert.match(html, /edit/); // last tool name
  assert.match(html, /12345/); // tokens
  // looping badge with the warning
  assert.match(html, /pi-progress-loop-badge/);
  assert.match(html, /suspected repeating tool-call sequence/);
  // raw <img> from the summary must not appear — only its escaped form
  assert.doesNotMatch(html, /<img /);
  assert.doesNotMatch(html, /<script/);
  assert.match(html, /&lt;img src=x onerror=alert\(1\)&gt;/);
  assert.match(html, /&amp; friends/);
});

test('renderPiProgressBlock escapes summary text in both content and title attribute', () => {
  const sandbox = newSandbox();
  const html = vm.runInContext(
    `renderPiProgressBlock({ last_summary: 'a"b<c>d&#39;e' }, null)`,
    sandbox,
  );
  // content: quotes/angles/entities all entity-encoded
  assert.match(html, /a&quot;b&lt;c&gt;d&amp;#39;e/);
  // the raw substring must not sneak through anywhere in the markup
  assert.doesNotMatch(html, /a"b<c>/);
  assert.doesNotMatch(html, /<c>/);
  // with no warning there is no looping badge
  assert.doesNotMatch(html, /pi-progress-loop-badge/);
});

test('renderPiProgressBlock truncates the summary to 200 chars', () => {
  const sandbox = newSandbox();
  const html = vm.runInContext(
    `renderPiProgressBlock({ last_summary: 'x'.repeat(500) }, null)`,
    sandbox,
  );
  assert.ok(!/x{201}/.test(html.replace(/title="[^"]*"/g, '')), 'visible summary text is capped at 200 chars');
});

test('renderPiProgressBlock shows the loading placeholder when there is no data yet', () => {
  const sandbox = newSandbox();
  const html = vm.runInContext(`renderPiProgressBlock(null, null)`, sandbox);
  assert.match(html, /pi-progress-loading/);
  assert.doesNotMatch(html, /pi-progress-loop-badge/);
});

test('startPiProgressPolling reuses the single window._piProgressInterval handle (PWA poll-stacking rule)', () => {
  const sandbox = newSandbox();
  let intervalCalls = 0;
  sandbox.setInterval = (fn) => { intervalCalls++; return 42; };
  vm.runInContext(`startPiProgressPolling(); startPiProgressPolling(); startPiProgressPolling();`, sandbox);
  assert.equal(intervalCalls, 1, 'setInterval must fire exactly once across repeated renders');
  assert.equal(sandbox._piProgressInterval, 42, 'handle stored on window._piProgressInterval');
});

test('piProgressTick self-stops when no progress block is mounted', () => {
  const sandbox = newSandbox();
  let cleared = 0;
  sandbox.setInterval = () => { sandbox._piProgressInterval = 42; return 42; };
  sandbox.clearInterval = () => { cleared++; };
  vm.runInContext(`startPiProgressPolling();`, sandbox);
  assert.equal(sandbox._piProgressInterval, 42);
  // no .pi-progress-live elements in the stub DOM → tick must clear the handle
  vm.runInContext(`piProgressTick();`, sandbox);
  assert.ok(cleared >= 1, 'clearInterval called');
  assert.equal(sandbox._piProgressInterval, null, 'handle reset so the next surface starts fresh');
});

test('all five locale bundles carry the pi_progress_* keys', () => {
  for (const lang of ['de', 'en', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    const missing = PROG_KEYS.filter(k => bundle[k] === undefined);
    assert.deepEqual(missing, [], `bundle ${lang} missing: ${missing.join(', ')}`);
  }
});

test('session page no longer mounts the piProgressPanel live strip (task card only)', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  assert.doesNotMatch(src, /piProgressPanel/);
  assert.doesNotMatch(src, /session-pi-progress/);
  // task card block stays:
  assert.match(src, /prd-task-progress/);
  assert.match(src, /renderPiProgressBlock/);
});
