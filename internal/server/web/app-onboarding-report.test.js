// Onboarding report view — PWA side.
//
// window.onboardingReportCardHtml(report) turns the decoded
// GET /api/automation/onboarding-report body into the Automation Overview
// card: what is configured, what is missing (each check with its reason) and
// what to try next (each suggestion with its how-to link), with a refresh
// button. The Autonomous settings group carries the three onboarding_report.*
// fields, docsLinkForKey maps the card and the fields to the glossary, and
// every onboarding_report_ and settings_onboarding_report_ locale key is
// pinned in all five bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const HOSTILE = '<img src=x onerror=alert(1)>';
const VIEW_PREFIX = 'onboarding_report_';
const SETTINGS_PREFIX = 'settings_onboarding_report_';

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

function render(report) {
  const sandbox = loadApp();
  sandbox._report = report;
  return vm.runInContext('window.onboardingReportCardHtml(_report)', sandbox);
}

function docsLink(key) {
  const sandbox = loadApp();
  sandbox._key = key;
  return vm.runInContext('docsLinkForKey(_key)', sandbox);
}

function configured(over) {
  return {
    backends: ['ollama'], models: ['qwen3'], projects: [{ name: 'app', dir: '/srv/app' }],
    channels: ['ntfy'], memory: true, merge_pipeline: [{ project: 'app', source: 'file' }], ...over,
  };
}

function check(over) {
  return { id: 'llm_backend', title: 'An LLM backend is enabled', status: 'ok', reason: '1 backend(s) are enabled.', ...over };
}

function step(over) {
  return {
    check_id: 'channels', title: 'Connect a comm channel', howto: 'docs/howto/comm-channels.md',
    anchor: 'docs/datawatch-definitions.md#channel-routing-v830', command: 'datawatch channel add',
    mcp_tool: 'channel_add', pwa_place: 'Settings > Comms', ...over,
  };
}

function report(over) {
  return { enabled: true, configured: configured(), checks: [check()], next_steps: [], ...over };
}

function groupFields() {
  const sandbox = loadApp();
  return JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.filter(f => (f.key || '').startsWith('onboarding_report.')))",
    sandbox));
}

// ── all ok ──────────────────────────────────────────────────────────────────

test('a report with every check passing shows the three parts and says nothing is missing', () => {
  const html = render(report());
  assert.ok(html.includes('Onboarding report'), 'card title missing: ' + html);
  assert.ok(html.includes('Configured'), 'configured part missing');
  assert.ok(html.includes('Missing'), 'missing part missing');
  assert.ok(html.includes('Try next'), 'try next part missing');
  assert.ok(html.includes('LLM backends: ollama'), 'backends missing: ' + html);
  assert.ok(html.includes('Models: qwen3'));
  assert.ok(html.includes('Projects: app'));
  assert.ok(html.includes('Channels: ntfy'));
  assert.ok(html.includes('Memory: on'));
  assert.ok(html.includes('Merge pipeline: app: file'));
  assert.ok(html.includes('Every check passes. Nothing is missing.'), 'all-ok line missing');
  assert.ok(html.includes('Nothing to try next.'), 'empty next steps line missing');
  assert.ok(!html.includes('onboarding-report-check"'), 'a passing check must not be listed as missing');
});

test('an empty setup reads none for each list and memory off', () => {
  const html = render(report({ configured: configured({ backends: [], models: [], projects: [], channels: [], memory: false, merge_pipeline: [] }) }));
  assert.ok(html.includes('LLM backends: none'), html);
  assert.ok(html.includes('Memory: off'));
});

// ── missing items ───────────────────────────────────────────────────────────

test('each missing check shows its title, status and reason, and passing checks are left out', () => {
  const html = render(report({
    checks: [
      check(),
      check({ id: 'channels', title: 'A comm channel is connected', status: 'missing', reason: 'No comm channel is enabled.' }),
      check({ id: 'memory', title: 'Memory is on', status: 'optional', reason: 'Memory is off; Automation works without it.' }),
    ],
  }));
  assert.ok(html.includes('A comm channel is connected'), html);
  assert.ok(html.includes('No comm channel is enabled.'), 'reason missing');
  assert.ok(html.includes('Memory is off; Automation works without it.'), 'second reason missing');
  assert.ok(html.includes('>Optional<'), 'optional label missing');
  assert.ok(!html.includes('An LLM backend is enabled'), 'a passing check must not be listed');
  assert.ok(!html.includes('Every check passes'));
});

test('each suggestion shows its title, how-to link, definitions link and where to do it', () => {
  const html = render(report({
    checks: [check({ id: 'channels', status: 'missing', title: 'A comm channel is connected', reason: 'No comm channel is enabled.' })],
    next_steps: [step()],
  }));
  assert.ok(html.includes('Connect a comm channel'), html);
  assert.ok(html.includes('href="/diagrams.html#docs/howto/comm-channels.md"'), 'how-to link missing: ' + html);
  assert.ok(html.includes('href="/diagrams.html#docs/datawatch-definitions.md#channel-routing-v830"'), 'definitions link missing');
  assert.ok(html.includes('>How-to<'));
  assert.ok(html.includes('Command: datawatch channel add'));
  assert.ok(html.includes('MCP tool: channel_add'));
  assert.ok(html.includes('In the app: Settings &gt; Comms'));
});

test('a missing check with no suggestions says suggestions are off', () => {
  const html = render(report({ checks: [check({ status: 'missing', reason: 'x' })], next_steps: [] }));
  assert.ok(html.includes('Suggestions are switched off in the settings.'), html);
});

test('the card carries a refresh button that reads the report again', () => {
  const html = render(report());
  assert.ok(html.includes('onboarding-report-refresh'), html);
  assert.ok(html.includes('window._listLoaders.automation_onboarding_report.load()'), 'refresh does not reload the report: ' + html);
  assert.ok(html.includes('>Refresh<'));
});

test('every value from the API is escaped', () => {
  const html = render(report({
    configured: configured({ backends: [HOSTILE], projects: [{ name: HOSTILE }], channels: [HOSTILE], merge_pipeline: [{ project: HOSTILE, source: HOSTILE }] }),
    checks: [check({ title: HOSTILE, status: 'missing', reason: HOSTILE })],
    next_steps: [step({ title: HOSTILE, howto: HOSTILE, anchor: HOSTILE, command: HOSTILE, mcp_tool: HOSTILE, pwa_place: HOSTILE })],
  }));
  assert.ok(!html.includes(HOSTILE), 'hostile markup rendered unescaped: ' + html);
  assert.ok(!html.includes('<img'), 'an image element was injected');
});

// ── disabled report ─────────────────────────────────────────────────────────

test('a report that is switched off shows one line and none of the parts', () => {
  const html = render({ enabled: false, configured: {}, checks: [], next_steps: [] });
  assert.ok(html.includes('Onboarding report'), 'card title missing');
  assert.ok(html.includes('The onboarding report is switched off.'), html);
  assert.ok(!html.includes('Try next'), 'a disabled report must not show its parts');
  assert.ok(!html.includes('Configured'));
});

test('a missing report shows the placeholder with the card title', () => {
  for (const html of [render(null), render(undefined)]) {
    assert.ok(html.includes('Onboarding report'));
    assert.ok(html.includes('has not loaded yet'));
  }
});

// ── help links ──────────────────────────────────────────────────────────────

test('the card and its parts carry help links to the glossary sections', () => {
  const html = render(report());
  assert.ok(html.includes('class="docs-link"'), 'help link missing: ' + html);
  assert.ok(html.includes('/diagrams.html#docs/datawatch-definitions.md#onboarding-report"'), 'report link missing: ' + html);
  assert.ok(html.includes('/diagrams.html#docs/datawatch-definitions.md#onboarding-report-checks"'), 'checks link missing');
  assert.ok(html.includes('/diagrams.html#docs/datawatch-definitions.md#onboarding-report-next-steps"'), 'next steps link missing');
});

test('docsLinkForKey maps the card, the settings and their labels to the three sections', () => {
  const base = 'datawatch-definitions.md';
  assert.equal(docsLink('onboarding_report_card_title'), base + '#onboarding-report');
  assert.equal(docsLink('onboarding_report.enabled'), base + '#onboarding-report');
  assert.equal(docsLink('settings_onboarding_report_enabled'), base + '#onboarding-report');
  assert.equal(docsLink('onboarding_report.disabled_checks'), base + '#onboarding-report-checks');
  assert.equal(docsLink('settings_onboarding_report_disabled_checks'), base + '#onboarding-report-checks');
  assert.equal(docsLink('onboarding_report_checks'), base + '#onboarding-report-checks');
  assert.equal(docsLink('onboarding_report.include_next_steps'), base + '#onboarding-report-next-steps');
  assert.equal(docsLink('settings_onboarding_report_include_next_steps'), base + '#onboarding-report-next-steps');
  assert.equal(docsLink('onboarding_report_next_steps'), base + '#onboarding-report-next-steps');
});

// ── settings fields ─────────────────────────────────────────────────────────

test('the Autonomous settings carry the three onboarding_report fields with help links', () => {
  const fields = groupFields();
  const byKey = Object.fromEntries(fields.map(f => [f.key, f]));
  assert.deepEqual(Object.keys(byKey).sort(), ['onboarding_report.disabled_checks', 'onboarding_report.enabled', 'onboarding_report.include_next_steps']);
  assert.equal(byKey['onboarding_report.enabled'].type, 'toggle');
  assert.equal(byKey['onboarding_report.include_next_steps'].type, 'toggle');
  assert.equal(byKey['onboarding_report.disabled_checks'].type, 'text');
  assert.equal(byKey['onboarding_report.disabled_checks'].csv, true);
  for (const f of fields) {
    assert.ok(f.labelKey && f.hintKey && f.docsKey, f.key + ' lacks a label, hint or docs key');
    assert.ok(docsLink(f.docsKey), f.key + ' docs key maps to no section');
  }
});

// ── locale bundles ──────────────────────────────────────────────────────────

test('the polling loader is window scoped and the card source never stores an interval on an element', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const start = src.indexOf('onboardingReportCardHtml = function');
  const end = src.indexOf('function _automationMergePipelineLoader', start);
  const block = src.slice(start, end);
  assert.ok(!/setInterval/.test(block), 'the onboarding report must not start its own interval');
  assert.ok(block.includes('listLoader('), 'the report reads through the shared list loader');
});

test('every onboarding_report_ and settings_onboarding_report_ key is in all five bundles with a real translation', () => {
  const en = bundle('en');
  const keys = Object.keys(en).filter(k => k.startsWith(VIEW_PREFIX) || k.startsWith(SETTINGS_PREFIX));
  assert.ok(keys.length >= 30, 'expected the full key set, got ' + keys.length);
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of keys) {
      assert.equal(typeof b[k], 'string', `${lang} lacks ${k}`);
      assert.ok(b[k].trim() !== '', `${lang} has an empty ${k}`);
      if (lang !== 'en' && !['onboarding_report_status_optional', 'onboarding_report_status_missing'].includes(k)) {
        assert.notEqual(b[k], en[k], `${lang} ${k} is an untranslated copy of English`);
      }
      const ph = s => (s.match(/%\d\$s/g) || []).sort().join(',');
      assert.equal(ph(b[k]), ph(en[k]), `${lang} ${k} changes the placeholders`);
    }
  }
});
