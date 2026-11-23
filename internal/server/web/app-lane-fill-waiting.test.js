// Lane fill waiting list and stalled-merge setting — PWA side.
//
// window.laneFillCardHtml(status) draws each entry of the status' waiting list
// with its title and a plain-language reason, and leaves the freed lane
// visibly idle. The autonomous settings card carries
// autonomous.lane_fill.stalled_merge_minutes next to the other lane fill rows.
// Every new string exists in all five locale bundles with the same keys.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const HOSTILE = '<img src=x onerror=alert(1)>';

const NEW_KEYS = [
  'settings_lane_fill_stalled_merge_minutes',
  'settings_lane_fill_stalled_merge_minutes_help',
  'lane_fill_waiting_heading',
  'lane_fill_waiting_help',
  'lane_fill_waiting_lane_idle',
  'lane_fill_waiting_reason_waiting_review_merge',
  'lane_fill_waiting_reason_merge_stalled',
  'lane_fill_waiting_reason_no_task_startable',
  'lane_fill_waiting_reason_other',
];

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
}

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  return sandbox;
}

function render(status) {
  const sandbox = loadApp();
  sandbox._status = status;
  return vm.runInContext('window.laneFillCardHtml(_status)', sandbox);
}

function lane(over) {
  return { id: 'claude-1', type: 'claude', state: 'idle', occupant: null, blocker: '', eligible_waiting: 0, ...over };
}

function status(over) {
  return {
    mode: 'on', last_poll_at: '2026-11-14T10:00:00Z', lanes: [lane()], queued_eligible: { gpu: 0, claude: 0 },
    evidence: { checked_at: '2026-11-14T10:00:00Z', gpu_active_sessions: 0, loaded_models: [], error: '' },
    decisions: [], alerts: [], waiting: [], ...over,
  };
}

function waitingOcc(over) {
  return { prd_id: 'p1', title: 'Cache column', backend: 'claude-code', wait_reason: 'server text', reason_code: 'merge_stalled', ...over };
}

function countOccurrences(haystack, needle) {
  return haystack.split(needle).length - 1;
}

// ── the setting field ────────────────────────────────────────────────────────

function autonomousFields() {
  const sandbox = loadApp();
  return JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields)", sandbox));
}

test('the autonomous settings card declares stalled_merge_minutes with its type, bounds, default and help', () => {
  const fields = autonomousFields();
  const keys = fields.map(f => f.key);
  const f = fields.find(x => x.key === 'autonomous.lane_fill.stalled_merge_minutes');
  assert.ok(f, 'stalled_merge_minutes missing from the autonomous card');
  assert.equal(f.type, 'number');
  assert.equal(f.min, 0);
  assert.equal(f.step, 1);
  assert.equal(String(f.default), '30');
  assert.equal(f.labelKey, 'settings_lane_fill_stalled_merge_minutes');
  assert.equal(f.helpKey, 'settings_lane_fill_stalled_merge_minutes_help');
  assert.equal(f.helpDocs, 'howto/automation-lane-fill.md', 'docs link must point at the lane fill how-to');
  const gpu = keys.indexOf('autonomous.lane_fill.gpu_lanes');
  assert.ok(gpu >= 0, 'gpu_lanes anchor missing');
  assert.ok(keys.indexOf(f.key) > gpu, 'the field must sit after the other lane fill entries');
});

test('the lane fill how-to the settings link names exists', () => {
  const docs = path.join(__dirname, '..', '..', '..', 'docs', 'howto', 'automation-lane-fill.md');
  assert.ok(fs.existsSync(docs), 'docs/howto/automation-lane-fill.md missing');
});

test('the stalled-merge label and help read in plain words, with no tracker id and no PRD', () => {
  const en = bundle('en');
  for (const k of NEW_KEYS) {
    assert.ok(typeof en[k] === 'string' && en[k].trim() !== '', k + ' missing in en.json');
    assert.ok(!/\bPRD\b/.test(en[k]), k + ' must say Automaton, not PRD');
    assert.ok(!/\b(BL|GH|TS)-?\d+\b|#\d{2,}/.test(en[k]), k + ' must not carry a tracker id');
  }
});

// ── waiting rendering ────────────────────────────────────────────────────────

test('each waiting Automaton shows its title and a plain-language reason', () => {
  const html = render(status({ waiting: [
    waitingOcc({ prd_id: 'a', title: 'Cache column', reason_code: 'merge_stalled' }),
    waitingOcc({ prd_id: 'b', title: 'Fix login', reason_code: 'waiting_review_merge' }),
    waitingOcc({ prd_id: 'c', title: 'Tidy docs', reason_code: 'no_task_startable' }),
  ] }));
  const en = bundle('en');
  assert.ok(html.includes('Waiting with no live work'), 'waiting heading missing: ' + html);
  assert.equal(countOccurrences(html, 'lane-fill-waiting-row'), 3, 'one row per waiting Automaton');
  for (const t of ['Cache column', 'Fix login', 'Tidy docs']) assert.ok(html.includes(t), t + ' title missing');
  assert.ok(html.includes(en.lane_fill_waiting_reason_merge_stalled), 'merge_stalled reason missing');
  assert.ok(html.includes(en.lane_fill_waiting_reason_waiting_review_merge), 'waiting_review_merge reason missing');
  assert.ok(html.includes(en.lane_fill_waiting_reason_no_task_startable), 'no_task_startable reason missing');
  assert.ok(!html.includes('server text'), 'a known code must show the translated reason, not the server text');
});

test('the freed lane stays visibly idle: the lane row is idle and each waiting row carries an empty bar and an idle marker', () => {
  const html = render(status({ waiting: [waitingOcc()] }));
  assert.ok(html.includes('data-lane-state="idle"'), 'the lane must still be drawn idle: ' + html);
  assert.ok(!html.includes('data-lane-state="occupied"'), 'a waiting Automaton must not make its lane look occupied');
  assert.ok(html.includes('lane-fill-waiting-idle'), 'idle marker missing');
  assert.ok(html.includes('Lane idle'), 'idle marker text missing');
  assert.ok(html.includes('class="lane-bar-fill" style="width:0%;"'), 'idle bar must be empty');
});

test('the backend shows beside the title when the server gives one, and is left out otherwise', () => {
  const withBackend = render(status({ waiting: [waitingOcc({ backend: 'pi-prompt' })] }));
  assert.ok(withBackend.includes('lane-fill-waiting-backend') && withBackend.includes('pi-prompt'), 'backend missing');
  const without = render(status({ waiting: [waitingOcc({ backend: '' })] }));
  assert.equal(countOccurrences(without, 'lane-fill-waiting-backend'), 0, 'no backend element without a backend');
});

test('a reason code the app does not know falls back to the server text, then to a generic line', () => {
  const server = render(status({ waiting: [waitingOcc({ reason_code: 'something_new', wait_reason: 'Held for a manual check' })] }));
  assert.ok(server.includes('Held for a manual check'), 'server reason fallback missing: ' + server);
  const generic = render(status({ waiting: [waitingOcc({ reason_code: '', wait_reason: '' })] }));
  assert.ok(generic.includes(bundle('en').lane_fill_waiting_reason_other), 'generic reason missing: ' + generic);
});

test('a row with no title falls back to the Automaton id', () => {
  const html = render(status({ waiting: [waitingOcc({ title: '', prd_id: 'd3680001' })] }));
  assert.ok(html.includes('d3680001'), 'id fallback missing: ' + html);
});

test('nothing renders for an empty, missing or malformed waiting list', () => {
  for (const waiting of [[], undefined, null, 'x', [null, 7]]) {
    const html = render(status({ waiting }));
    assert.equal(countOccurrences(html, 'lane-fill-waiting-list'), 0, 'no waiting block for ' + JSON.stringify(waiting));
    assert.equal(countOccurrences(html, 'lane-fill-waiting-row'), 0);
  }
});

test('hostile titles, backends and server reasons are escaped', () => {
  const html = render(status({ waiting: [
    waitingOcc({ title: HOSTILE, backend: HOSTILE, reason_code: 'other', wait_reason: HOSTILE }),
  ] }));
  assert.ok(!html.includes('<img'), 'raw markup leaked: ' + html);
  assert.ok(html.includes('&lt;img src=x onerror=alert(1)&gt;'), 'escaped text missing');
});

test('the waiting list does not replace the per-lane eligible-work marker', () => {
  const html = render(status({ lanes: [lane({ eligible_waiting: 2 })], waiting: [waitingOcc()] }));
  assert.ok(html.includes('2 eligible work waiting'), 'eligible waiting marker lost: ' + html);
  assert.equal(countOccurrences(html, 'lane-fill-waiting-list'), 1);
});

// ── locale completeness ──────────────────────────────────────────────────────

test('every new key exists in all five bundles and is a real translation outside English', () => {
  const en = bundle('en');
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of NEW_KEYS) {
      assert.ok(typeof b[k] === 'string' && b[k].trim() !== '', k + ' missing or empty in ' + lang);
      if (lang !== 'en') assert.notEqual(b[k], en[k], k + ' is an English copy in ' + lang);
    }
  }
});

test('the new keys keep the English positional placeholders in every bundle', () => {
  const ph = s => [...new Set(String(s).match(/%\d+\$[sd]/g) || [])].sort().join(',');
  const en = bundle('en');
  for (const k of NEW_KEYS) for (const lang of LANGS) {
    assert.equal(ph(bundle(lang)[k]), ph(en[k]), lang + ' placeholders differ for ' + k);
  }
});

test('every reason code in the key map has its key in en.json', () => {
  const sandbox = loadApp();
  const en = bundle('en');
  const map = JSON.parse(vm.runInContext('JSON.stringify(LANE_FILL_WAITING_REASON_KEYS)', sandbox));
  assert.deepEqual(Object.keys(map).sort(), ['merge_stalled', 'no_task_startable', 'waiting_review_merge']);
  for (const v of Object.values(map)) assert.ok(v in en, v + ' missing from en.json');
});

// ── OpenAPI ──────────────────────────────────────────────────────────────────

test('both OpenAPI copies describe the stalled-merge setting and the waiting list', () => {
  for (const p of [path.join(__dirname, 'openapi.yaml'), path.join(__dirname, '..', '..', '..', 'docs', 'api', 'openapi.yaml')]) {
    const y = fs.readFileSync(p, 'utf8');
    assert.ok(y.includes('autonomous.lane_fill.stalled_merge_minutes'), p + ': config field missing');
    assert.ok(/LaneFillWaiting:\n/.test(y), p + ': LaneFillWaiting schema missing');
    assert.ok(/waiting:\n\s+type: array/.test(y), p + ': waiting list missing from the status');
    for (const code of ['waiting_review_merge', 'merge_stalled', 'no_task_startable']) {
      assert.ok(y.includes(code), p + ': reason code ' + code + ' missing');
    }
  }
});
