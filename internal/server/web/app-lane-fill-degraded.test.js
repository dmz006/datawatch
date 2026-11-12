// Lane fill card — the degraded hosts block is separate from lane blockers,
// shown only when evidence.degraded is non-empty, and escaped.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const KEYS = ['settings_lane_fill_host_down_after_minutes', 'lane_fill_degraded_title', 'lane_fill_degraded_host', 'lane_fill_degraded_help'];

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
}

function render(status) {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  sandbox._status = status;
  return vm.runInContext('window.laneFillCardHtml(_status)', sandbox);
}

function status(degraded) {
  return {
    mode: 'shadow', lanes: [{ id: 'gpu-1', type: 'gpu', state: 'idle', occupant: null, blocker: '', eligible_waiting: 0 }],
    queued_eligible: { gpu: 0, claude: 0 },
    evidence: { checked_at: '2026-10-24T10:00:00Z', gpu_active_sessions: 0, loaded_models: [], error: '', degraded },
    decisions: [], alerts: [],
  };
}

test('a degraded host shows the block with host and error, escaped', () => {
  const since = new Date(Date.now() - 7 * 60000).toISOString();
  const html = render(status([{ host: 'node-a', error: '<img src=x onerror=alert(1)>', since }]));
  assert.ok(html.includes('lane-fill-degraded'), html);
  assert.ok(html.includes('Degraded hosts (not blocking the lanes)'), html);
  assert.ok(html.includes('node-a'), html);
  assert.ok(!html.includes('<img src=x'), 'error text must be escaped: ' + html);
  assert.ok(html.includes('&lt;img'), html);
});

test('no degraded hosts shows no block', () => {
  for (const d of [[], undefined, null]) {
    const html = render(status(d));
    assert.ok(!html.includes('lane-fill-degraded'), html);
  }
});

test('every locale carries the new keys', () => {
  for (const lang of ['en', 'es', 'de', 'ja', 'fr']) {
    const b = bundle(lang);
    for (const k of KEYS) assert.ok(typeof b[k] === 'string' && b[k], lang + ' missing ' + k);
    if (lang !== 'en') assert.notEqual(b.lane_fill_degraded_title, bundle('en').lane_fill_degraded_title, lang);
  }
});
