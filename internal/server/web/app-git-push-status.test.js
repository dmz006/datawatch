// Push-on-complete outcome chip (renderPrdPushState in app.js):
//   - a recorded push (git.push_state === 'pushed') renders a
//     success-styled "Pushed" chip whose tooltip carries the pushed ref
//     and a short (7-char) SHA
//   - a refused push (git.push_state === 'not_pushed') renders a
//     warning-styled "Not pushed" chip whose tooltip is the recorded
//     push_warning -- free text that may contain HTML, so it must come
//     through escHtml escaped (this file injects a <script> payload to
//     prove the escaping)
//   - an empty/absent push state renders nothing at all
//   - the prd_push_* localisation keys exist (non-empty) in all five
//     locale bundles (en / de / es / fr / ja)
//
// Loads the REAL app.js into the browser stub (same approach as
// app-git-push-override.test.js) and calls the top-level helper
// directly, so no DOM mocking around the render sites is needed.
//
// Run: node --test internal/server/web/*.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const LOCALE_CODES = ['en', 'de', 'es', 'fr', 'ja'];
const PUSH_KEYS = [
  'prd_push_pushed',
  'prd_push_not_pushed',
  'prd_push_ref_label',
  'prd_push_settings_label',
];

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox.renderPrdPushState, 'function',
    'renderPrdPushState is not defined in app.js -- did it get renamed?');
  // Deterministic per-key markers prove the chip labels come from
  // t('prd_push_*') at render time, not from hard-coded strings.
  vm.runInContext(`window._i18n.bundle = {
    prd_push_pushed: 'MARKPUSHED',
    prd_push_not_pushed: 'MARKNOTPUSHED',
    prd_push_ref_label: 'MARKREF',
    prd_push_settings_label: 'MARKSTATE',
  };`, sandbox);
  return sandbox;
}

function render(sandbox, prd) {
  sandbox.__prd = prd;
  const out = vm.runInContext('renderPrdPushState(__prd)', sandbox);
  delete sandbox.__prd;
  return out;
}

test('pushed renders a success-styled chip whose tooltip carries ref + short sha', () => {
  const sandbox = loadApp();
  const out = render(sandbox, {
    id: 'p1',
    git: {
      push_state: 'pushed',
      pushed_ref: 'refs/heads/automaton/p1',
      pushed_sha: '0123456789abcdef',
    },
  });
  assert.match(out, /prd-push-chip prd-push-chip-pushed/);
  assert.match(out, /MARKPUSHED/, 'chip label must come from t(prd_push_pushed)');
  assert.match(out, /var\(--success,/, 'pushed chip must use the success CSS variable');
  // Tooltip: full ref, and a short sha (first 7 chars, not the full 16).
  assert.match(out, /refs\/heads\/automaton\/p1/);
  assert.match(out, /MARKREF/, 'tooltip ref prefix must come from t(prd_push_ref_label)');
  assert.match(out, /0123456(?![0-9a-f])/);
  assert.doesNotMatch(out, /0123456789abcdef/);
});

test('not_pushed renders the warning state with the reason escaped in the tooltip', () => {
  const sandbox = loadApp();
  // The push reason is free text from the push tooling -- inject a
  // script tag to prove escHtml escapes it in the title attribute.
  const warn = 'no origin remote <script>alert(1)</script>';
  const out = render(sandbox, {
    id: 'p1',
    git: { push_state: 'not_pushed', push_warning: warn },
  });
  assert.match(out, /prd-push-chip prd-push-chip-notpushed/);
  assert.match(out, /MARKNOTPUSHED/, 'chip label must come from t(prd_push_not_pushed)');
  assert.match(out, /var\(--warning,/, 'not-pushed chip must use the warning CSS variable');
  assert.match(out, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
  assert.doesNotMatch(out, /<script>alert(1)<\/script>/,
    'push_warning must be HTML-escaped before it reaches the title attribute');
});

test('summary-shaped payload (top-level push_state) also renders', () => {
  const sandbox = loadApp();
  const warn = 'no origin remote';
  const pushed = render(sandbox, { push_state: 'pushed', pushed_ref: '' , pushed_sha: '' });
  assert.match(pushed, /prd-push-chip-pushed/);
  const refused = render(sandbox, { push_state: 'not_pushed', push_warning: warn });
  assert.match(refused, /prd-push-chip-notpushed/);
  assert.match(refused, /no origin remote/);
});

test('empty or absent push state renders nothing', () => {
  const sandbox = loadApp();
  assert.equal(render(sandbox, { id: 'p1', git: {} }), '');
  assert.equal(render(sandbox, { id: 'p1' }), '');
  assert.equal(render(sandbox, null), '');
});

test('prd_push keys exist (non-empty) in all five locale bundles', () => {
  const bundles = {};
  for (const code of LOCALE_CODES) {
    const raw = fs.readFileSync(path.join(LOCALES_DIR, `${code}.json`), 'utf8');
    bundles[code] = JSON.parse(raw); // throws if the bundle is invalid JSON
  }
  // Identical key sets for the push-state family across bundles.
  for (const code of LOCALE_CODES) {
    const own = PUSH_KEYS.filter(k => Object.prototype.hasOwnProperty.call(bundles[code], k));
    assert.deepEqual(own, PUSH_KEYS, `${code}.json must define every prd_push key`);
    for (const k of PUSH_KEYS) {
      assert.equal(typeof bundles[code][k], 'string', `${code}.json ${k} must be a string`);
      assert.ok(bundles[code][k].length > 0, `${code}.json ${k} must be non-empty`);
    }
  }
});
