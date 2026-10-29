// Lane fill view — PWA side.
//
// window.laneFillCardHtml(status) turns the decoded GET /api/autonomous/lane-fill
// body into the Automation sub-tab's lane card. The Automaton settings modal
// carries the lane preference select, which saves through set_lane_preference
// only when it changed. The config fields and every lane_fill_ and
// settings_lane_fill_ locale key are pinned here as well.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const HOSTILE = '<img src=x onerror=alert(1)>';

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
  return { id: 'gpu-1', type: 'gpu', state: 'idle', occupant: null, blocker: '', eligible_waiting: 0, ...over };
}

function decision(over) {
  return {
    at: '2026-10-24T10:00:00Z', mode: 'shadow', lane_id: 'gpu-1', lane_type: 'gpu', action: 'start_prd',
    prd_id: 'p1', title: 'Cache column', reason: '', blocker: '', executed: false, error: '', repeat_count: 1,
    ...over,
  };
}

function status(over) {
  return {
    mode: 'shadow', last_poll_at: '2026-10-24T10:00:00Z', lanes: [], queued_eligible: { gpu: 0, claude: 0 },
    evidence: { checked_at: '2026-10-24T10:00:00Z', gpu_active_sessions: 0, loaded_models: [], error: '' },
    decisions: [], alerts: [], ...over,
  };
}

function countOccurrences(haystack, needle) {
  return haystack.split(needle).length - 1;
}

// ── card rendering ──────────────────────────────────────────────────────────

test('an occupied lane shows its id, type, state, occupant title and backend', () => {
  const html = render(status({
    lanes: [lane({ id: 'gpu-1', type: 'gpu', state: 'occupied', occupant: { prd_id: 'p1', title: 'Cache column', backend: 'pi-prompt' } })],
  }));
  assert.ok(html.includes('gpu-1'), 'lane id missing: ' + html);
  assert.ok(html.includes('GPU lane'), 'lane type label missing');
  assert.ok(html.includes('occupied'), 'occupied state missing');
  assert.ok(html.includes('Cache column · pi-prompt'), 'occupant title and backend missing: ' + html);
});

test('an idle lane shows the idle state and its blocker text, and a marker when eligible work waits', () => {
  const html = render(status({
    lanes: [
      lane({ id: 'claude-1', type: 'claude', state: 'idle', blocker: 'waiting for a free Claude slot', eligible_waiting: 2 }),
    ],
  }));
  assert.ok(html.includes('Claude lane'), 'claude lane type label missing');
  assert.ok(html.includes('idle'), 'idle state missing');
  assert.ok(html.includes('Blocked: waiting for a free Claude slot'), 'blocker text missing: ' + html);
  assert.ok(html.includes('2 eligible work waiting'), 'eligible waiting marker missing: ' + html);
  assert.ok(html.includes('lane-fill-waiting'), 'waiting marker element missing');
});

test('no marker shows when no eligible work waits', () => {
  const html = render(status({ lanes: [lane({ eligible_waiting: 0 })] }));
  assert.ok(!html.includes('lane-fill-waiting'), 'marker must not show with nothing waiting');
});

test('shadow mode shows the one-line shadow explanation and the mode badge', () => {
  const html = render(status({ mode: 'shadow' }));
  assert.ok(html.includes('Shadow: computes and records every decision and starts nothing.'), 'shadow wording missing');
  assert.ok(html.includes('Mode: shadow'), 'mode badge missing');
});

test('on and off modes show their badge without the shadow explanation', () => {
  const on = render(status({ mode: 'on' }));
  assert.ok(on.includes('Mode: on'), 'on badge missing');
  assert.ok(!on.includes('starts nothing'), 'shadow wording must not show in on mode');
  const off = render(status({ mode: 'off' }));
  assert.ok(off.includes('Mode: off'), 'off badge missing');
  assert.ok(!off.includes('starts nothing'), 'shadow wording must not show in off mode');
});

test('queued eligible counts for gpu and claude are shown', () => {
  const html = render(status({ queued_eligible: { gpu: 3, claude: 1 } }));
  assert.ok(html.includes('Eligible work queued: GPU 3, Claude 1'), 'queued counts missing: ' + html);
});

test('evidence shows active GPU sessions and loaded models', () => {
  const html = render(status({
    evidence: { gpu_active_sessions: 2, loaded_models: [{ host: 'ollama-1', model: 'qwen3.8:27b' }], error: '' },
  }));
  assert.ok(html.includes('Active GPU sessions: 2'), 'active sessions missing: ' + html);
  assert.ok(html.includes('Loaded models: ollama-1 qwen3.8:27b'), 'loaded models missing');
});

test('evidence with no loaded models says none', () => {
  const html = render(status({ evidence: { gpu_active_sessions: 0, loaded_models: [], error: '' } }));
  assert.ok(html.includes('Loaded models: none'), 'empty model list must read none: ' + html);
});

test('an evidence error replaces the evidence line', () => {
  const html = render(status({ evidence: { gpu_active_sessions: 0, loaded_models: [], error: 'ollama unreachable' } }));
  assert.ok(html.includes('Evidence unavailable: ollama unreachable'), 'evidence error missing: ' + html);
  assert.ok(!html.includes('Active GPU sessions'), 'evidence counts must not show beside an error');
});

test('a would-start row says would start when not executed and started when executed', () => {
  const pending = render(status({ decisions: [decision({ executed: false })] }));
  assert.ok(pending.includes('Would start'), 'would-start wording missing: ' + pending);
  assert.ok(!pending.includes('Started'), 'started must not show for an unexecuted start');
  const done = render(status({ mode: 'on', decisions: [decision({ executed: true })] }));
  assert.ok(done.includes('Started'), 'started wording missing');
  assert.ok(!done.includes('Would start'), 'would-start must not show for an executed start');
});

test('a non-start decision shows its action name and reason, without start wording', () => {
  const html = render(status({
    decisions: [decision({ action: 'blocked', executed: false, blocker: 'no free GPU lane', title: '' })],
  }));
  assert.ok(html.includes('blocked'), 'action missing');
  assert.ok(html.includes('no free GPU lane'), 'blocker missing from decision row');
  assert.ok(!html.includes('Would start'), 'non-start decision must not say would start');
});

test('the decision list shows the newest ten rows only', () => {
  const decisions = Array.from({ length: 12 }, (_, i) => decision({ title: 'work ' + i }));
  const html = render(status({ decisions }));
  assert.equal(countOccurrences(html, 'class="lane-fill-decision"'), 10, 'expected 10 decision rows');
  assert.ok(html.includes('work 0'), 'newest decision missing');
  assert.ok(!html.includes('work 10'), 'eleventh decision must not show');
});

test('recent alerts show the lane, the work and the message', () => {
  const html = render(status({
    alerts: [{ at: '2026-10-24T10:05:00Z', lane_id: 'claude-1', work: 'Cache column', message: 'idle with work waiting' }],
  }));
  assert.ok(html.includes('claude-1'), 'alert lane missing');
  assert.ok(html.includes('Cache column'), 'alert work missing');
  assert.ok(html.includes('idle with work waiting'), 'alert message missing');
});

// ── empty and error states ─────────────────────────────────────────────────

test('a missing status renders the placeholder card', () => {
  const html = render(null);
  assert.ok(html.includes('Lane fill'), 'card title missing');
  assert.ok(html.includes('No lane status yet'), 'placeholder missing: ' + html);
});

test('empty lists render their empty-state text', () => {
  const html = render(status());
  assert.ok(html.includes('No lanes configured'), 'no-lanes text missing');
  assert.ok(html.includes('No decisions recorded yet'), 'no-decisions text missing');
  assert.ok(html.includes('No idle lane alerts'), 'no-alerts text missing');
});

test('a zero decision time renders no clock reading', () => {
  const html = render(status({ decisions: [decision({ at: '0001-01-01T00:00:00Z' })] }));
  assert.ok(!html.includes('0001'), 'zero time must not leak');
});

// ── escaping ──────────────────────────────────────────────────────────────

test('a hostile title, backend, blocker, reason, alert and lane id are escaped everywhere', () => {
  const html = render(status({
    evidence: { gpu_active_sessions: 1, loaded_models: [{ host: HOSTILE, model: HOSTILE }], error: '' },
    queued_eligible: { gpu: 0, claude: 0 },
    lanes: [lane({
      id: HOSTILE, type: 'gpu', state: 'occupied',
      occupant: { prd_id: 'p1', title: HOSTILE, backend: HOSTILE },
      blocker: HOSTILE, eligible_waiting: 1,
    })],
    decisions: [decision({ title: HOSTILE, lane_id: HOSTILE, reason: HOSTILE, blocker: '', action: HOSTILE })],
    alerts: [{ at: '2026-10-24T10:05:00Z', lane_id: HOSTILE, work: HOSTILE, message: HOSTILE }],
  }));
  assert.ok(!html.includes('<img'), 'raw markup leaked: ' + html);
  assert.ok(html.includes('&lt;img src=x onerror=alert(1)&gt;'), 'escaped form missing');
});

test('a hostile evidence error is escaped', () => {
  const html = render(status({ evidence: { gpu_active_sessions: 0, loaded_models: [], error: HOSTILE } }));
  assert.ok(!html.includes('<img'), 'raw markup leaked from evidence error');
});

// ── Automation sub-tab mount and loader ───────────────────────────────────

test('the Automation body mounts the lane card beside the capacity card', () => {
  const sandbox = loadApp();
  // Both cards live on the Overview sub-tab; select it before rendering the body.
  vm.runInContext("_automationState.subtab = 'overview'", sandbox);
  const body = vm.runInContext('_automationBodyHTML()', sandbox);
  const cap = body.indexOf('id="automationCapacityCard"');
  const lanes = body.indexOf('id="automationLaneFillCard"');
  assert.ok(cap >= 0, 'capacity card missing');
  assert.ok(lanes > cap, 'lane card must follow the capacity card');
});

test('the lane loader polls the lane fill endpoint with limit 20 and the lane card element', async () => {
  const sandbox = loadApp();
  const urls = [];
  sandbox.apiFetch = url => { urls.push(url); return Promise.resolve(status()); };
  const ctl = vm.runInContext('_automationLaneFillLoader()', sandbox);
  await ctl.load();
  ctl.stop();
  assert.deepEqual(urls, ['/api/autonomous/lane-fill?limit=20']);
  assert.equal(ctl.state.key, 'automation_lane_fill');
});

// ── settings modal: lane preference select and save call ───────────────────

// openModal runs openPRDSettingsModal against stubs and returns what it mounted
// plus a recorder of the apiFetch calls its save handler makes.
async function openModal(prd, fieldValues) {
  const sandbox = loadApp();
  const calls = [];
  const mounted = {};
  sandbox.apiFetch = (url, opts) => {
    if (opts) calls.push({ url, body: JSON.parse(opts.body) });
    return Promise.resolve(url === '/api/autonomous/prds/p1' ? prd : {});
  };
  sandbox.ensureLLMModelLists = () => Promise.resolve();
  sandbox.refreshLLMModelField = () => {};
  sandbox.showToast = () => {};
  sandbox._prdCloseModal = () => {};
  sandbox._refreshAutomataOrPRD = () => {};
  sandbox._prdMountModal = (html, onSubmit) => { mounted.html = html; mounted.onSubmit = onSubmit; };
  sandbox.document.getElementById = id => ({
    value: fieldValues[id] !== undefined ? fieldValues[id] : '',
    checked: false,
    querySelector() { return null; },
  });
  vm.runInContext('openPRDSettingsModal("p1")', sandbox);
  for (let i = 0; i < 5; i++) await new Promise(r => setImmediate(r));
  assert.ok(mounted.onSubmit, 'settings modal did not mount');
  return { mounted, calls, sandbox };
}

test('the settings modal renders a lane preference select beside Kind, selected from the Automaton', async () => {
  const { mounted } = await openModal({ id: 'p1', lane_preference: 'claude' }, {});
  assert.ok(mounted.html.includes('id="prdSettingsLanePreference"'), 'lane preference select missing');
  assert.ok(mounted.html.includes('value="claude" selected'), 'stored claude preference must be selected');
  for (const v of ['any', 'gpu', 'claude']) {
    assert.ok(mounted.html.includes('value="' + v + '"'), 'option ' + v + ' missing');
  }
  const kindAt = mounted.html.indexOf('id="prdSettingsKind"');
  const laneAt = mounted.html.indexOf('id="prdSettingsLanePreference"');
  assert.ok(kindAt > 0 && laneAt > kindAt, 'lane preference must follow the Kind select');
});

test('an Automaton with no stored preference shows any as selected', async () => {
  const { mounted } = await openModal({ id: 'p1' }, {});
  assert.ok(mounted.html.includes('value="any" selected'), 'any must be selected when none is stored');
});

test('saving a changed lane preference posts set_lane_preference with the new value', async () => {
  const { mounted, calls } = await openModal({ id: 'p1', lane_preference: 'gpu' }, { prdSettingsLanePreference: 'claude' });
  mounted.onSubmit();
  await new Promise(r => setImmediate(r));
  const lane = calls.filter(c => c.url.endsWith('/set_lane_preference'));
  assert.equal(lane.length, 1, 'expected one set_lane_preference call, got ' + JSON.stringify(calls));
  assert.equal(lane[0].url, '/api/autonomous/prds/p1/set_lane_preference');
  assert.deepEqual(lane[0].body, { lane_preference: 'claude' });
});

test('saving an unchanged lane preference sends no set_lane_preference call', async () => {
  const { mounted, calls } = await openModal({ id: 'p1', lane_preference: 'gpu' }, { prdSettingsLanePreference: 'gpu' });
  mounted.onSubmit();
  await new Promise(r => setImmediate(r));
  assert.equal(calls.filter(c => c.url.endsWith('/set_lane_preference')).length, 0, 'unchanged preference must not post');
});

// ── settings fields ───────────────────────────────────────────────────────

const LANE_FIELDS = {
  'autonomous.lane_fill.mode': { type: 'select', options: ['off', 'shadow', 'on'], labelKey: 'settings_lane_fill_mode' },
  'autonomous.lane_fill.gpu_lanes': { type: 'number', min: 0, labelKey: 'settings_lane_fill_gpu_lanes' },
  'autonomous.lane_fill.claude_lanes': { type: 'number', min: 0, labelKey: 'settings_lane_fill_claude_lanes' },
  'autonomous.lane_fill.idle_alert_minutes': { type: 'number', min: 0, labelKey: 'settings_lane_fill_idle_alert_minutes' },
  'autonomous.lane_fill.decompose_ahead_min': { type: 'number', min: 0, labelKey: 'settings_lane_fill_decompose_ahead_min' },
  'autonomous.lane_fill.poll_seconds': { type: 'number', min: 5, labelKey: 'settings_lane_fill_poll_seconds' },
  'autonomous.lane_fill.gpu_backend': { type: 'text', labelKey: 'settings_lane_fill_gpu_backend' },
  'autonomous.lane_fill.gpu_model': { type: 'text', labelKey: 'settings_lane_fill_gpu_model' },
  'autonomous.lane_fill.claude_backend': { type: 'text', labelKey: 'settings_lane_fill_claude_backend' },
};

test('the autonomous settings card declares every lane fill field with its type, bounds and label key', () => {
  const sandbox = loadApp();
  const fields = JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields)", sandbox));
  const keys = fields.map(f => f.key);
  const automationAt = keys.indexOf('autonomous.automation.idle_alert_minutes');
  assert.ok(automationAt >= 0, 'idle_alert_minutes anchor missing');
  for (const [key, want] of Object.entries(LANE_FIELDS)) {
    const f = fields.find(x => x.key === key);
    assert.ok(f, key + ' missing from the autonomous card');
    assert.equal(f.type, want.type, key + ' type');
    assert.equal(f.labelKey, want.labelKey, key + ' labelKey');
    if (want.options) assert.deepEqual(f.options, want.options, key + ' options');
    if (want.min !== undefined) assert.equal(f.min, want.min, key + ' min');
    assert.ok(keys.indexOf(key) > automationAt, key + ' must sit after the automation idle alert');
  }
  const mode = fields.find(f => f.key === 'autonomous.lane_fill.mode');
  assert.equal(mode.default, 'shadow', 'mode must default to shadow');
});

// ── locale completeness ───────────────────────────────────────────────────

const LANE_PREFIXES = ['lane_fill_', 'settings_lane_fill_'];
function isLaneKey(k) { return LANE_PREFIXES.some(p => k.startsWith(p)); }

test('every lane_fill_ and settings_lane_fill_ key exists in all five bundles with the same set', () => {
  const enKeys = Object.keys(bundle('en')).filter(isLaneKey).sort();
  assert.ok(enKeys.length >= 40, 'expected at least 40 lane keys, found ' + enKeys.length);
  for (const lang of LANGS) {
    const b = bundle(lang);
    const langKeys = Object.keys(b).filter(isLaneKey).sort();
    assert.deepEqual(langKeys, enKeys, lang + '.json lane key set differs from English');
    for (const k of enKeys) {
      assert.ok(typeof b[k] === 'string' && b[k].trim() !== '', k + ' empty in ' + lang);
    }
  }
});

test('every lane key is a real translation in de, es, fr and ja, not an English copy', () => {
  const en = bundle('en');
  const enKeys = Object.keys(en).filter(isLaneKey);
  for (const lang of ['de', 'es', 'fr', 'ja']) {
    const b = bundle(lang);
    const same = enKeys.filter(k => b[k] === en[k]);
    assert.deepEqual(same, [], lang + '.json keeps English for: ' + same.join(', '));
  }
});

test('lane keys keep the English positional placeholders in every bundle', () => {
  const ph = s => [...new Set(String(s).match(/%\d+\$[sd]/g) || [])].sort().join(',');
  const en = bundle('en');
  for (const k of Object.keys(en).filter(isLaneKey)) {
    for (const lang of ['de', 'es', 'fr', 'ja']) {
      assert.equal(ph(bundle(lang)[k]), ph(en[k]), lang + ' placeholders differ for ' + k);
    }
  }
});

test('every literal lane key the app asks for by name exists in en.json, including the key maps', async () => {
  const sandbox = loadApp();
  const en = bundle('en');
  const source = fs.readFileSync(APP_JS, 'utf8');
  const used = new Set();
  const callPattern = /\bt\(\s*(['"])((?:lane_fill_|settings_lane_fill_)[A-Za-z0-9_]*)\1/g;
  let m;
  while ((m = callPattern.exec(source)) !== null) used.add(m[2]);
  const mapped = JSON.parse(vm.runInContext(
    'JSON.stringify([LANE_FILL_MODE_KEYS, LANE_FILL_TYPE_KEYS, LANE_FILL_PREF_KEYS, LANE_FILL_ACTION_KEYS])', sandbox));
  for (const map of mapped) for (const v of Object.values(map)) used.add(v);
  const missing = [...used].filter(k => !(k in en)).sort();
  assert.deepEqual(missing, [], 'lane keys used by app.js missing from en.json: ' + missing.join(', '));
  assert.ok(used.size >= 30, 'expected to find the lane keys in app.js, found ' + used.size);
});
