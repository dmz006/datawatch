// Pins the attempt-cost fields in Settings > General > Autonomous: the first
// attempt backend and model of each task kind, the escalate-after-failures
// count and the resume context limit. Each field carries a "?" help entry
// that links to howto/failed-attempt-cost.md, saves through a handler that
// shows the daemon's refusal inline, and every label, help and error string
// exists in all five locale bundles. The Automaton attempt rows show the
// backend reason, whether the attempt resumed or started fresh, and the
// no-retry reason.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const LOCALE_DIR = path.join(__dirname, 'locales');
const HELP_DOCS = 'howto/failed-attempt-cost.md';
const KINDS = ['implementation', 'docs', 'note', 'final_gate'];

// loadApp loads app.js with the English bundle as the active locale, so t()
// returns the real strings the assertions check.
function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const en = bundle('en');
  vm.runInContext('window._i18n = ' + JSON.stringify({ bundle: en, fallback: en }) + ';', sandbox);
  return sandbox;
}

function autonomousField(sandbox, key) {
  const json = vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.find(f => f.key === '" + key + "') || null)",
    sandbox);
  return JSON.parse(json);
}

function bundle(locale) {
  return JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, locale + '.json'), 'utf8'));
}

// Every locale key the attempt-cost fields and attempt rows use.
function attemptCostKeys() {
  const keys = [];
  for (const kind of KINDS) {
    keys.push('settings_attempt_cost_' + kind + '_backend');
    keys.push('settings_attempt_cost_' + kind + '_model');
  }
  keys.push(
    'settings_attempt_cost_help',
    'settings_attempt_cost_escalate_after',
    'settings_attempt_cost_resume_limit',
    'settings_attempt_cost_error_refused',
    'settings_attempt_cost_error_network',
    'settings_attempt_cost_error_range',
    'review_attempt_why',
    'review_attempt_resumed',
    'review_attempt_fresh',
    'review_attempt_resume_reason',
    'review_attempt_no_retry',
  );
  return keys;
}

test('each task kind exposes a first-attempt backend and model text field that saves inline', () => {
  const sandbox = loadApp();
  for (const kind of KINDS) {
    for (const part of ['backend', 'model']) {
      const key = 'autonomous.first_attempt.' + kind + '.' + part;
      const f = autonomousField(sandbox, key);
      assert.ok(f, key + ' missing from autonomous card fields');
      assert.equal(f.type, 'text', key + ' must be a text field');
      assert.equal(f.labelKey, 'settings_attempt_cost_' + kind + '_' + part);
      assert.equal(f.helpKey, 'settings_attempt_cost_help', key + ' must carry the attempt-cost help text');
      assert.equal(f.helpDocs, HELP_DOCS, key + ' help link must point at ' + HELP_DOCS);
      assert.equal(f.onchange, "saveAttemptCostField('" + key + "', this)", key + ' must save through saveAttemptCostField');
    }
  }
});

test('escalate-after-failures and resume context limit are whole-number fields with their ranges and inline save', () => {
  const sandbox = loadApp();
  const escalate = autonomousField(sandbox, 'autonomous.first_attempt_escalate_after_failures');
  assert.ok(escalate, 'autonomous.first_attempt_escalate_after_failures missing');
  assert.equal(escalate.type, 'number');
  assert.equal(escalate.min, 0);
  assert.equal(escalate.max, 20);
  assert.equal(escalate.labelKey, 'settings_attempt_cost_escalate_after');
  assert.equal(escalate.helpDocs, HELP_DOCS);
  assert.equal(escalate.onchange, "saveAttemptCostNumber('autonomous.first_attempt_escalate_after_failures', this, 0, 20)");

  const resume = autonomousField(sandbox, 'autonomous.retry_resume_max_context_tokens');
  assert.ok(resume, 'autonomous.retry_resume_max_context_tokens missing');
  assert.equal(resume.type, 'number');
  assert.equal(resume.min, 0);
  assert.equal(resume.max, 2000000);
  assert.equal(resume.labelKey, 'settings_attempt_cost_resume_limit');
  assert.equal(resume.helpDocs, HELP_DOCS);
  assert.equal(resume.onchange, "saveAttemptCostNumber('autonomous.retry_resume_max_context_tokens', this, 0, 2000000)");
});

test('the attempt-cost help entry resolves to howto/failed-attempt-cost.md', () => {
  const sandbox = loadApp();
  const f = autonomousField(sandbox, 'autonomous.first_attempt.docs.backend');
  const link = vm.runInContext(
    "settingsHelpLink(" + JSON.stringify(f) + ")", sandbox);
  assert.match(link, /href="\/diagrams\.html#docs\/howto\/failed-attempt-cost\.md"/);
  assert.match(link, />\?</, 'help entry must render as a ? link');
});

test('the howto page the help links point at exists', () => {
  const page = path.join(__dirname, '..', '..', '..', 'docs', HELP_DOCS);
  assert.ok(fs.existsSync(page), 'docs/' + HELP_DOCS + ' is missing');
});

test('every attempt-cost label, help and error key exists in all five locale bundles', () => {
  for (const locale of LOCALES) {
    const b = bundle(locale);
    for (const k of attemptCostKeys()) {
      assert.ok(typeof b[k] === 'string' && b[k].trim().length > 0,
        k + ' missing or empty in ' + locale + ' locale');
      assert.ok(!/PRD/.test(b[k]), k + ' must not use the word PRD in ' + locale);
    }
  }
});

test('the Automaton attempt rows show backend reason, resumed or fresh, and the no-retry reason', () => {
  const sandbox = loadApp();
  const html = vm.runInContext(
    "_taskAttemptsHTML(" + JSON.stringify({
      attempts: [
        { number: 1, backend: 'pi', model: 'qwen3.8:27b', outcome: 'failed',
          backend_reason: 'first attempt for docs tasks', resumed: false },
        { number: 2, backend: 'claude-code', outcome: 'failed',
          backend_reason: 'escalated after 1 failures', resumed: true,
          resume_reason: 'retry marker with failure summary',
          no_retry_reason: 'the same failure summary failed twice' },
      ],
    }) + ")", sandbox);
  assert.match(html, /first attempt for docs tasks/);
  assert.match(html, /escalated after 1 failures/);
  assert.match(html, /retry marker with failure summary/);
  assert.match(html, /the same failure summary failed twice/);
  assert.match(html, /Fresh session/);
  assert.match(html, /Resumed the previous session/);
});

test('a refused write shows the daemon message in an inline alert, not only a toast', () => {
  const sandbox = loadApp();
  const refusal = vm.runInContext(
    "_attemptCostRefusalText(JSON.stringify({error: 'autonomous.first_attempt.docs.model: backend \"pi\" needs a model'}))",
    sandbox);
  assert.equal(refusal, 'autonomous.first_attempt.docs.model: backend "pi" needs a model');
  const plain = vm.runInContext("_attemptCostRefusalText('plain refusal')", sandbox);
  assert.equal(plain, 'plain refusal');
  const empty = vm.runInContext("_attemptCostRefusalText('')", sandbox);
  assert.equal(empty, 'The daemon did not save this value. Nothing changed.');
});
