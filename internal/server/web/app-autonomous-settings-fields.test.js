// Pins the Settings > General > Autonomous field list to the real
// per-Automaton concurrency key. autonomous.max_parallel_tasks never had
// any effect and is deprecated; the PWA must expose
// autonomous.max_concurrent_tasks instead and offer no editable control
// for the dead key.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');
const LOCALE_LIST = ['en', 'de', 'es', 'fr', 'ja'];
const LOCALE_DIR = path.join(__dirname, 'locales');

function findField(sectionId, key) {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const json = vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === '" + sectionId + "') || {fields: []}).fields.find(f => f.key === '" + key + "') || null)",
    sandbox);
  return JSON.parse(json);
}

function autonomousKeys() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const json = vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.map(f => f.key))",
    sandbox);
  return JSON.parse(json);
}

test('autonomous settings expose max_concurrent_tasks, not max_parallel_tasks', () => {
  const keys = autonomousKeys();
  assert.ok(keys.includes('autonomous.max_concurrent_tasks'), 'autonomous.max_concurrent_tasks missing: ' + keys.join(','));
  assert.ok(!keys.includes('autonomous.max_parallel_tasks'), 'deprecated autonomous.max_parallel_tasks still editable');
});

test('autonomous settings expose gate_disagreement as a select with warn/block options and a label key', () => {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const field = vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.find(f => f.key === 'autonomous.gate_disagreement') || null)",
    sandbox);
  const parsed = JSON.parse(field);
  assert.ok(parsed, 'autonomous.gate_disagreement missing from autonomous card fields');
  assert.equal(parsed.type, 'select');
  assert.deepEqual(parsed.options, ['warn', 'block']);
  assert.equal(parsed.labelKey, 'settings_gate_disagreement');
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  assert.ok(typeof en.settings_gate_disagreement === 'string' && en.settings_gate_disagreement.length > 0,
    'settings_gate_disagreement missing from en locale');
});

test('gate_disagreement settings and PRD invalid-gate keys exist in all five locale bundles', () => {
  const keys = [
    'settings_gate_disagreement',
    'settings_gate_disagreement_hint',
    'settings_gate_disagreement_block',
    'settings_gate_disagreement_warn',
    'prd_gate_invalid',
    'prd_gate_invalid_reason',
  ];
  for (const locale of ['en', 'de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
    for (const k of keys) {
      assert.ok(typeof bundle[k] === 'string' && bundle[k].length > 0,
        k + ' missing or empty in ' + locale + ' locale');
    }
  }
});

test('autonomous settings expose verification_timeout_seconds with an en label key', () => {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const field = vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.find(f => f.key === 'autonomous.verification_timeout_seconds') || null)",
    sandbox);
  const parsed = JSON.parse(field);
  assert.ok(parsed, 'autonomous.verification_timeout_seconds missing from autonomous card fields');
  assert.equal(parsed.labelKey, 'settings_verification_timeout');
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  assert.ok(typeof en.settings_verification_timeout === 'string' && en.settings_verification_timeout.length > 0,
    'settings_verification_timeout missing from en locale');
});

test('autonomous settings expose task_override_statuses as a csv text field with a label key', () => {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const field = vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.find(f => f.key === 'autonomous.task_override_statuses') || null)",
    sandbox);
  const parsed = JSON.parse(field);
  assert.ok(parsed, 'autonomous.task_override_statuses missing from autonomous card fields');
  assert.equal(parsed.type, 'text');
  assert.ok(parsed.csv, 'autonomous.task_override_statuses must set csv: true');
  assert.equal(parsed.labelKey, 'settings_task_override_statuses');
});

test('settings_task_override_statuses is a non-empty string in all five locale bundles', () => {
  for (const locale of ['en', 'de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
    assert.ok(typeof bundle.settings_task_override_statuses === 'string' && bundle.settings_task_override_statuses.length > 0,
      'settings_task_override_statuses missing or empty in ' + locale + ' locale');
  }
});

test('autonomous settings expose session_idle_timeout_seconds as a number with an en label key', () => {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const field = vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.find(f => f.key === 'autonomous.session_idle_timeout_seconds') || null)",
    sandbox);
  const parsed = JSON.parse(field);
  assert.ok(parsed, 'autonomous.session_idle_timeout_seconds missing from autonomous card fields');
  assert.equal(parsed.type, 'number');
  assert.equal(parsed.labelKey, 'settings_session_idle_timeout');
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  assert.ok(typeof en.settings_session_idle_timeout === 'string' && en.settings_session_idle_timeout.length > 0,
    'settings_session_idle_timeout missing from en locale');
});

test('autonomous settings expose the worker idle nudge fields as numbers with locale labels in all 5 locales', () => {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const wanted = {
    'autonomous.worker_idle_nudge_seconds': 'settings_worker_idle_nudge',
    'autonomous.worker_idle_nudge_stall_seconds': 'settings_worker_idle_nudge_stall',
  };
  for (const [key, labelKey] of Object.entries(wanted)) {
    const parsed = JSON.parse(vm.runInContext(
      "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.find(f => f.key === '" + key + "') || null)",
      sandbox));
    assert.ok(parsed, key + ' missing from autonomous card fields');
    assert.equal(parsed.type, 'number');
    assert.equal(parsed.labelKey, labelKey);
    for (const locale of ['en', 'de', 'es', 'fr', 'ja']) {
      const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
      assert.ok(typeof bundle[labelKey] === 'string' && bundle[labelKey].length > 0,
        labelKey + ' missing or empty in ' + locale + ' locale');
    }
  }
});

// ── T2: plan-coverage gate — new Autonomous settings fields ─────────────────

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const LOCALES = { en: 'en.json', de: 'de.json', es: 'es.json', fr: 'fr.json', ja: 'ja.json' };

// boot loads the real app.js into a fresh vm context and returns it.
function boot() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  return sandbox;
}

test('GENERAL_CONFIG_FIELDS: autonomous.plan_coverage_check is the off/warn/block select', async (t) => {
  const sandbox = boot();
  const fields = vm.runInContext('GENERAL_CONFIG_FIELDS', sandbox, { filename: 'app.js' });
  const autonomous = fields.find((sec) => sec.id === 'autonomous');
  assert.ok(autonomous, 'autonomous section exists');
  const field = autonomous.fields.find((f) => f.key === 'autonomous.plan_coverage_check');
  assert.ok(field, 'autonomous.plan_coverage_check field is declared');
  assert.equal(field.type, 'select');
  assert.equal(JSON.stringify(field.options), JSON.stringify(['off', 'warn', 'block']), 'options are exactly off/warn/block');
  assert.equal(field.default, 'block');
  assert.ok(field.labelKey, 'labelKey set for the locale bundle');
  assert.ok(field.label && field.label.length > 0, 'hardcoded fallback label non-empty');
});

test('GENERAL_CONFIG_FIELDS: autonomous.plan_coverage_min_ratio is a 0..1 number', async (t) => {
  const sandbox = boot();
  const fields = vm.runInContext('GENERAL_CONFIG_FIELDS', sandbox, { filename: 'app.js' });
  const autonomous = fields.find((sec) => sec.id === 'autonomous');
  const field = autonomous.fields.find((f) => f.key === 'autonomous.plan_coverage_min_ratio');
  assert.ok(field, 'autonomous.plan_coverage_min_ratio field is declared');
  assert.equal(field.type, 'number');
  assert.ok(field.labelKey, 'labelKey set for the locale bundle');
  assert.ok(field.label && field.label.length > 0, 'hardcoded fallback label non-empty');
  assert.equal(field.max, 1);
  assert.ok(field.min > 0 && field.min <= 1, 'min ratio bounded (0,1]');
});

test('GENERAL_CONFIG_FIELDS: autonomous.automation.idle_alert_minutes is a non-negative number', async (t) => {
  const sandbox = boot();
  const fields = vm.runInContext('GENERAL_CONFIG_FIELDS', sandbox, { filename: 'app.js' });
  const autonomous = fields.find((sec) => sec.id === 'autonomous');
  const field = autonomous.fields.find((f) => f.key === 'autonomous.automation.idle_alert_minutes');
  assert.ok(field, 'autonomous.automation.idle_alert_minutes field is declared');
  assert.equal(field.type, 'number');
  assert.equal(field.min, 0);
  assert.equal(field.labelKey, 'settings_automation_idle_alert_minutes');
  assert.ok(field.label && field.label.length > 0, 'hardcoded fallback label non-empty');
});

test('GENERAL_CONFIG_FIELDS: autonomous.automation.triage_alert_minutes is a non-negative number with default 30', async (t) => {
  const sandbox = boot();
  const fields = vm.runInContext('GENERAL_CONFIG_FIELDS', sandbox, { filename: 'app.js' });
  const autonomous = fields.find((sec) => sec.id === 'autonomous');
  const field = autonomous.fields.find((f) => f.key === 'autonomous.automation.triage_alert_minutes');
  assert.ok(field, 'autonomous.automation.triage_alert_minutes field is declared');
  assert.equal(field.type, 'number');
  assert.equal(field.min, 0, '0 is a valid value (triage alert off)');
  assert.equal(field.default, '30', 'unset reads the 30-minute default');
  assert.equal(field.labelKey, 'settings_automation_triage_alert_minutes');
  assert.ok(field.label && field.label.length > 0, 'hardcoded fallback label non-empty');
});

for (const [lang, file] of Object.entries(LOCALES)) {
  test(`locales: ${lang} defines the automation triage-alert label`, (t) => {
    const bundle = JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, file), 'utf8'));
    const v = bundle.settings_automation_triage_alert_minutes;
    assert.ok(typeof v === 'string' && v.trim().length > 0, `${lang} key present and non-empty`);
    assert.ok(!/\b((?:BL|F|B|S)\d{1,6})\b/.test(v), 'no tracker IDs in the string');
  });
}

for (const [lang, file] of Object.entries(LOCALES)) {
  test(`locales: ${lang} defines the automation idle-alert label`, (t) => {
    const bundle = JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, file), 'utf8'));
    const v = bundle.settings_automation_idle_alert_minutes;
    assert.ok(typeof v === 'string' && v.trim().length > 0, `${lang} key present and non-empty`);
    assert.ok(!/\bBL\d+\b/.test(v), 'no tracker IDs in the string');
  });
}

const PLAN_COVERAGE_KEYS = [
  'settings_plan_coverage',
  'settings_plan_coverage_min_ratio',
  'prd_plan_coverage_title',
  'prd_plan_coverage_pass',
  'prd_plan_coverage_warn',
  'prd_plan_coverage_fail',
  'prd_plan_coverage_ack',
  'prd_plan_coverage_ack_hint',
  'prd_plan_coverage_ack_empty',
  'prd_plan_coverage_ack_done',
];

for (const [lang, file] of Object.entries(LOCALES)) {
  test(`locales: ${lang} defines non-empty plan-coverage keys`, (t) => {
    const bundle = JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, file), 'utf8'));
    for (const k of PLAN_COVERAGE_KEYS) {
      const v = bundle[k];
      assert.ok(
        typeof v === 'string' && v.trim().length > 0,
        `${lang} key "${k}" present and non-empty`
      );
    }
  });
}

// ── T3: plan-coverage gate — PRD view surface & acknowledge flow ────────────

test('plan coverage block renders counts, unmatched list, warn/block state and ack UI', async (t) => {
  const sandbox = boot();

  // No plan_coverage → nothing rendered (gate not in effect).
  assert.equal(vm.runInContext(`_prdPlanCoverageBlock({id:'P1',status:'needs_review'})`, sandbox), '');

  // Passing report → pass status line, no ack UI.
  const passing = vm.runInContext(`_prdPlanCoverageBlock({
    id: 'P1', status: 'needs_review',
    plan_coverage: {
      mode: 'block', min_ratio: 1,
      report: { covered: 3, missing: 0, weakened: 0, passing: true, unmatched: [] },
      findings: []
    }
  })`, sandbox);
  assert.match(passing, /covered 3\/3/);
  assert.match(passing, /covered the spec|prd_plan_coverage_pass/);
  assert.ok(!passing.includes('prdPcAck-'), 'no ack form for passing reports');

  // Failing report in block mode (no ack) → error state + ack input+button.
  const failing = vm.runInContext(`_prdPlanCoverageBlock({
    id: 'P1', status: 'needs_review',
    plan_coverage: {
      mode: 'block', min_ratio: 1,
      report: { covered: 1, missing: 1, weakened: 1, passing: false, unmatched: [
        { requirement: 'REQ-2 — audit logging', verdict: 'unmatched' }
      ] },
      findings: [{ rule: 'pc.missing_requirement', detail: 'REQ-2 not covered by any story' }],
      ack: null
    }
  })`, sandbox);
  assert.match(failing, /covered 1\/3/);
  assert.match(failing, /missing 1/);
  assert.match(failing, /weakened 1/);
  assert.match(failing, /blocked mode|prd_plan_coverage_fail/);
  assert.ok(failing.includes('REQ-2 — audit logging') || failing.includes('REQ-2 — audit logging'), 'unmatched requirement listed');
  assert.match(failing, /REQ-2 not covered by any story/);
  assert.match(failing, /prdPcAck-P1/);
  assert.match(failing, /window\._prdPlanCoverageAck/);

  // Failing in warn mode → warning state, NO ack form (warn never gates).
  const warn = vm.runInContext(`_prdPlanCoverageBlock({
    id: 'P1', status: 'needs_review',
    plan_coverage: {
      mode: 'warn', min_ratio: 1,
      report: { covered: 1, missing: 1, weakened: 0, unmatched: [] },
      findings: [{ rule: 'pc.missing_requirement', detail: 'REQ-3 not covered' }]
    }
  })`, sandbox);
  assert.match(warn, /warning mode|prd_plan_coverage_warn/);
  assert.ok(!warn.includes('prdPcAck-'), 'warn mode never renders the ack form');

  // Failing in block mode with an ack on file → ack shown, form hidden.
  const acked = vm.runInContext(`_prdPlanCoverageBlock({
    id: 'P1', status: 'needs_review',
    plan_coverage: {
      mode: 'block', min_ratio: 1,
      report: { covered: 1, missing: 1, weakened: 0, unmatched: [] },
      findings: [{ rule: 'pc.missing_requirement', detail: 'REQ-3 not covered' }],
      ack: { by: 'op-1', note: 'accepted risk', acknowledged_at: '2026-11-01T00:00:00Z' }
    }
  })`, sandbox);
  assert.match(acked, /accepted risk/);
  assert.match(acked, /op-1/);
  assert.ok(!acked.includes('type="text" id="prdPcAck'), 'no ack form once acknowledged');
});

test('plan coverage ack: payload is null for a blank note, trimmed for a real one', async (t) => {
  const sandbox = boot();
  assert.equal(vm.runInContext(`_prdPlanCoverageAckPayload("")`, sandbox), null);
  assert.equal(vm.runInContext(`_prdPlanCoverageAckPayload("   ")`, sandbox), null);
  assert.equal(vm.runInContext(`_prdPlanCoverageAckPayload(null)`, sandbox), null);
  const p = vm.runInContext(`_prdPlanCoverageAckPayload("  ship it  ")`, sandbox);
  assert.equal(p.actor, 'operator', 'payload actor');
  assert.equal(p.note, 'ship it', 'non-empty note trimmed into payload');
});

test('plan coverage ack: POSTs {actor,note} to the acknowledge endpoint; blank note never posts', async (t) => {
  const sandbox = boot();

  const calls = [];
  sandbox.fetch = (url, opts) => {
    const u = String(url);
    calls.push({ url: u, opts: opts || {} });
    const data = u.includes('plan_coverage/acknowledge') ? { id: 'PRD-PC 1' } : [];
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(data), text: () => Promise.resolve('[]') });
  };

  // Blank note → rejected client-side, no fetch at all.
  sandbox.__ackInput = { value: '   ' };
  const blank = await vm.runInContext(`window._prdPlanCoverageAck('PRD-PC 1', __ackInput, null)`, sandbox);
  assert.equal(blank, null, 'blank note resolves null');
  assert.equal(calls.length, 0, 'blank note must not reach the server');

  // Real note → POST to /api/autonomous/prds/{id}/plan_coverage/acknowledge.
  sandbox.__ackInput = { value: 'accepted risk: 80% is fine for this slice' };
  sandbox.__ackBtn = { disabled: false };
  await vm.runInContext(`window._prdPlanCoverageAck('PRD-PC 1', __ackInput, __ackBtn)`, sandbox);
  const ackCall = calls.find((c) => c.url === '/api/autonomous/prds/PRD-PC%201/plan_coverage/acknowledge');
  assert.ok(ackCall, 'acknowledge endpoint called');
  assert.equal(ackCall.opts.method, 'POST');
  const body = JSON.parse(ackCall.opts.body);
  assert.equal(body.actor, 'operator');
  assert.equal(body.note, 'accepted risk: 80% is fine for this slice');
  assert.equal(sandbox.__ackBtn.disabled, true, 'button disabled while in flight');
});

test('autonomous settings expose resume_max_age_seconds as a min-less number with a label key', () => {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const field = vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.find(f => f.key === 'autonomous.resume_max_age_seconds') || null)",
    sandbox);
  const parsed = JSON.parse(field);
  assert.ok(parsed, 'autonomous.resume_max_age_seconds missing from autonomous card fields');
  assert.equal(parsed.type, 'number');
  assert.equal(parsed.labelKey, 'settings_resume_max_age');
  // 0 (never resume) and -1 (no age limit) are both meaningful sentinels,
  // so the field must not impose a min bound that would reject them.
  assert.ok(parsed.min === undefined, 'autonomous.resume_max_age_seconds must not set a min bound');
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  assert.ok(typeof en.settings_resume_max_age === 'string' && en.settings_resume_max_age.length > 0,
    'settings_resume_max_age missing from en locale');
});

test('settings_resume_max_age and settings_resume_max_age_hint are non-empty strings in all five locale bundles', () => {
  for (const locale of ['en', 'de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
    assert.ok(typeof bundle.settings_resume_max_age === 'string' && bundle.settings_resume_max_age.length > 0,
      'settings_resume_max_age missing or empty in ' + locale + ' locale');
    assert.ok(typeof bundle.settings_resume_max_age_hint === 'string' && bundle.settings_resume_max_age_hint.length > 0,
      'settings_resume_max_age_hint missing or empty in ' + locale + ' locale');
  }
});

// 2026-10-17 — plan review gate + smoke verification settings card fields.

const FIVE = ['en','de','es','fr','ja'];

test('autonomous settings expose plan review + smoke verification fields', () => {
  const keys = autonomousKeys();
  const want = ['autonomous.plan_review_backend','autonomous.plan_review_model','autonomous.plan_review_mode','autonomous.smoke_verification_backend','autonomous.smoke_verification_model'];
  for (const k of want) assert.ok(keys.includes(k), k + ' missing: ' + keys.join(','));
});

test('plan review backend/model pairs follow the paired-model pattern; mode is a select over off/warn/block', () => {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const fields = JSON.parse(vm.runInContext(
    'JSON.stringify(GENERAL_CONFIG_FIELDS.find(s => s.id === "autonomous").fields)', sandbox));
  const get = k => fields.find(f => f.key === k);
  const pb = get('autonomous.plan_review_backend');
  assert.equal(pb.type, 'llm_backend');
  assert.equal(pb.pairedModelKey, 'autonomous.plan_review_model');
  const pm = get('autonomous.plan_review_model');
  assert.equal(pm.type, 'llm_model');
  assert.equal(pm.backendKey, 'autonomous.plan_review_backend');
  const mode = get('autonomous.plan_review_mode');
  assert.equal(mode.type, 'select');
  for (const m of ['off','warn','block']) assert.ok(mode.options.includes(m), 'mode options missing ' + m);
  const sb = get('autonomous.smoke_verification_backend');
  assert.equal(sb.type, 'llm_backend');
  assert.equal(sb.pairedModelKey, 'autonomous.smoke_verification_model');
  const sm = get('autonomous.smoke_verification_model');
  assert.equal(sm.type, 'llm_model');
  assert.equal(sm.backendKey, 'autonomous.smoke_verification_backend');
});

test('all five label keys are non-empty strings in all five locale bundles', () => {
  for (const locale of FIVE) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
    for (const k of ['settings_plan_review_backend','settings_plan_review_model','settings_plan_review_mode','settings_smoke_verification_backend','settings_smoke_verification_model']) {
      assert.ok(typeof bundle[k] === 'string' && bundle[k].length > 0, k + ' missing or empty in ' + locale + ' locale');
    }
  }
});

test('autonomous settings expose session_start_confirm_seconds right after session_idle_timeout_seconds, as a number with the label key', () => {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const fields = JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields)",
    sandbox));
  const idx = fields.findIndex(f => f.key === 'autonomous.session_idle_timeout_seconds');
  assert.ok(idx >= 0, 'autonomous.session_idle_timeout_seconds missing from autonomous card fields');
  const field = fields[idx + 1];
  assert.ok(field, 'field directly after session_idle_timeout_seconds missing');
  assert.equal(field.key, 'autonomous.session_start_confirm_seconds',
    'session_start_confirm_seconds must sit directly after session_idle_timeout_seconds');
  assert.equal(field.type, 'number');
  assert.equal(field.labelKey, 'settings_session_start_confirm');
});

test('settings_session_start_confirm label + help keys are non-empty strings in all five locale bundles', () => {
  for (const locale of ['en', 'de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
    for (const key of ['settings_session_start_confirm', 'settings_session_start_confirm_help']) {
      assert.ok(typeof bundle[key] === 'string' && bundle[key].length > 0,
        key + ' missing or empty in ' + locale + ' locale');
    }
  }
});

test('autonomous settings expose stuck_running_seconds as a number with label and help keys in all five locale bundles', () => {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const fields = JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields)",
    sandbox));
  const field = fields.find(f => f.key === 'autonomous.stuck_running_seconds');
  assert.ok(field, 'autonomous.stuck_running_seconds missing from autonomous card fields');
  assert.equal(field.type, 'number');
  assert.equal(field.labelKey, 'settings_stuck_running_seconds');
  assert.equal(field.helpKey, 'settings_stuck_running_seconds_help');
  for (const locale of ['en', 'de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
    for (const key of ['settings_stuck_running_seconds', 'settings_stuck_running_seconds_help']) {
      assert.ok(typeof bundle[key] === 'string' && bundle[key].length > 0,
        key + ' missing or empty in ' + locale + ' locale');
      assert.ok(!/\bPRDs?\b/.test(bundle[key]), key + ' must not say PRD in ' + locale + ' locale');
    }
  }
});

test('autonomous settings expose judge_policy and judge_timeout_seconds with label and help keys', () => {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const fields = JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields)",
    sandbox));
  const policy = fields.find(f => f.key === 'autonomous.judge_policy');
  assert.ok(policy, 'autonomous.judge_policy missing from autonomous card fields');
  assert.equal(policy.type, 'text');
  assert.ok(policy.csv, 'autonomous.judge_policy must set csv: true');
  assert.equal(policy.labelKey, 'settings_judge_policy');
  assert.equal(policy.helpKey, 'settings_judge_policy_help');
  const timeout = fields.find(f => f.key === 'autonomous.judge_timeout_seconds');
  assert.ok(timeout, 'autonomous.judge_timeout_seconds missing from autonomous card fields');
  assert.equal(timeout.type, 'number');
  assert.equal(timeout.placeholder, '1800');
  assert.equal(timeout.labelKey, 'settings_judge_timeout');
  assert.equal(timeout.helpKey, 'settings_judge_timeout_help');
  const sizeThreshold = fields.find(f => f.key === 'autonomous.judge_size_threshold_lines');
  assert.ok(sizeThreshold, 'autonomous.judge_size_threshold_lines missing from autonomous card fields');
  assert.equal(sizeThreshold.type, 'number');
  assert.equal(sizeThreshold.min, 0);
  assert.equal(sizeThreshold.placeholder, '400');
  assert.equal(sizeThreshold.labelKey, 'settings_judge_size_threshold');
  assert.equal(sizeThreshold.helpKey, 'settings_judge_size_threshold_help');
  const keys = fields.map(f => f.key);
  assert.equal(keys.indexOf('autonomous.judge_size_threshold_lines'),
    keys.indexOf('autonomous.judge_timeout_seconds') + 1,
    'size threshold must sit right after the time-limit field');
});

test('judge_policy and judge_timeout_seconds label and help keys are non-empty in all five locale bundles', () => {
  const keys = [
    'settings_judge_policy', 'settings_judge_policy_help',
    'settings_judge_timeout', 'settings_judge_timeout_help',
    'settings_judge_size_threshold', 'settings_judge_size_threshold_help',
  ];
  for (const locale of ['en', 'de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
    for (const key of keys) {
      assert.ok(typeof bundle[key] === 'string' && bundle[key].trim().length > 0,
        key + ' missing or empty in ' + locale + ' locale');
    }
  }
});

// ── Tag browser settings card (autonomous.tags.*) ────────────────────────────

test('autonomous settings group the six tag-browser fields under a Tag browser sub-heading', () => {
  const sandbox = boot();
  const fields = JSON.parse(vm.runInContext(
    'JSON.stringify(GENERAL_CONFIG_FIELDS.find(s => s.id === "autonomous").fields)', sandbox));
  const groupIdx = fields.findIndex(f => f.type === 'group' && f.labelKey === 'settings_tags_group');
  assert.ok(groupIdx >= 0, 'Tag browser group header missing from autonomous fields');
  assert.ok(fields[groupIdx].key === undefined, 'group header must not carry a config key');
  const tagKeys = [
    'autonomous.tags.default_view',
    'autonomous.tags.attention_statuses',
    'autonomous.tags.default_match_mode',
    'autonomous.tags.direct_counts_visible',
    'autonomous.tags.registry_keeper',
    'autonomous.tags.registry_sync_seconds',
  ];
  const idxs = tagKeys.map(k => fields.findIndex(f => f.key === k));
  idxs.forEach((i, n) => assert.ok(i > groupIdx, tagKeys[n] + ' must sit under the Tag browser header'));
  // Six fields contiguous directly after the header.
  assert.deepEqual(idxs, [1, 2, 3, 4, 5, 6].map(n => groupIdx + n), 'tag-browser fields must be contiguous under the header');
});

test('autonomous.tags.* fields have the declared types, options, defaults and label keys', () => {
  const sandbox = boot();
  const fields = JSON.parse(vm.runInContext(
    'JSON.stringify(GENERAL_CONFIG_FIELDS.find(s => s.id === "autonomous").fields)', sandbox));
  const get = k => fields.find(f => f.key === k);

  const view = get('autonomous.tags.default_view');
  assert.equal(view.type, 'select');
  assert.deepEqual(view.options, ['auto', 'attention', 'tags']);
  assert.equal(view.default, 'auto');
  assert.equal(view.labelKey, 'settings_tags_default_view');

  const statuses = get('autonomous.tags.attention_statuses');
  assert.equal(statuses.type, 'text');
  assert.equal(statuses.csv, true);
  assert.equal(statuses.default, 'blocked, needs_review, paused');
  assert.equal(statuses.labelKey, 'settings_tags_attention_statuses');

  const match = get('autonomous.tags.default_match_mode');
  assert.equal(match.type, 'select');
  assert.deepEqual(match.options, ['any', 'all']);
  assert.equal(match.default, 'any');
  assert.equal(match.labelKey, 'settings_tags_default_match_mode');

  const direct = get('autonomous.tags.direct_counts_visible');
  assert.equal(direct.type, 'toggle');
  assert.equal(direct.labelKey, 'settings_tags_direct_counts_visible');

  const keeper = get('autonomous.tags.registry_keeper');
  assert.equal(keeper.type, 'text');
  assert.ok(keeper.default === undefined || keeper.default === '', 'registry_keeper defaults to empty');
  assert.equal(keeper.labelKey, 'settings_tags_registry_keeper');

  const sync = get('autonomous.tags.registry_sync_seconds');
  assert.equal(sync.type, 'number');
  assert.equal(sync.min, 5);
  assert.equal(sync.step, 1);
  assert.equal(sync.default, '30');
  assert.equal(sync.labelKey, 'settings_tags_registry_sync_seconds');

  for (const f of [view, statuses, match, direct, keeper, sync]) {
    assert.ok(f.label && f.label.length > 0, f.key + ' has a hardcoded fallback label');
    assert.ok(!/\bPRD\b/.test(f.label), f.key + ' label must not say PRD');
  }
});

test('every tag-browser labelKey and the group hint exist in all five locale bundles, with no tracker IDs or PRD wording', () => {
  const keys = [
    'settings_tags_group',
    'settings_tags_group_help',
    'settings_tags_default_view',
    'settings_tags_attention_statuses',
    'settings_tags_default_match_mode',
    'settings_tags_direct_counts_visible',
    'settings_tags_registry_keeper',
    'settings_tags_registry_sync_seconds',
  ];
  for (const locale of ['en', 'de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
    for (const k of keys) {
      assert.ok(typeof bundle[k] === 'string' && bundle[k].length > 0,
        k + ' missing or empty in ' + locale + ' locale');
      assert.ok(!/\bBL\d+\b|\bB\d+\b|\bGH\d+\b/.test(bundle[k]), k + ' must not contain tracker IDs (' + locale + ')');
      assert.ok(!/\bPRD\b/.test(bundle[k]), k + ' must not say PRD (' + locale + ')');
    }
  }
});

// Pi worker observability settings (docs/operations.md "Pi worker
// observability"): the loop-detection window on the autonomous card and
// the global pi thinking level on its own card, rendered the same way as
// session_idle_timeout_seconds (label + labelKey + localized label).

test('autonomous settings expose loop_detect_turns as a number with a label key', () => {
  const parsed = findField('autonomous', 'autonomous.loop_detect_turns');
  assert.ok(parsed, 'autonomous.loop_detect_turns missing from autonomous card fields');
  assert.equal(parsed.type, 'number');
  assert.equal(parsed.labelKey, 'settings_loop_detect_turns');
  const en = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, 'en.json'), 'utf8'));
  assert.ok(typeof en.settings_loop_detect_turns === 'string' && en.settings_loop_detect_turns.length > 0,
    'settings_loop_detect_turns missing from en locale');
});

test('pi worker card exposes pi.thinking_level as a select with the valid levels', () => {
  const parsed = findField('piworker', 'pi.thinking_level');
  assert.ok(parsed, 'pi.thinking_level missing from the pi worker card fields');
  assert.equal(parsed.type, 'select');
  // Empty = don't pass --thinking (pi's model default); then the five
  // levels config.ValidatePiThinkingLevel accepts.
  for (const opt of ['', 'off', 'minimal', 'low', 'medium', 'high']) {
    assert.ok(parsed.options.includes(opt), 'pi.thinking_level select missing option: ' + JSON.stringify(opt));
  }
  assert.equal(parsed.labelKey, 'settings_pi_thinking_level');
});

test('pi worker observability labels are non-empty strings in all five locale bundles', () => {
  for (const locale of LOCALE_LIST) {
    const bundle = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, locale + '.json'), 'utf8'));
    assert.ok(typeof bundle.settings_loop_detect_turns === 'string' && bundle.settings_loop_detect_turns.length > 0,
      'settings_loop_detect_turns missing or empty in ' + locale + ' locale');
    assert.ok(typeof bundle.settings_pi_thinking_level === 'string' && bundle.settings_pi_thinking_level.length > 0,
      'settings_pi_thinking_level missing or empty in ' + locale + ' locale');
  }
});
