// Requirements report card (web setup view) — PWA side.
//
// window.requirementsReportCardHtml(report) turns the decoded
// GET /api/automation/requirements-report body into the setup view card:
// each capability with its minimum (and override) and its nodes (a node
// whose version is below the minimum is flagged), the Automation backend
// minimums with their below-minimum flags, and the missing list.
// _requirementsReportLoader reads it through the shared list loader into
// #requirementsReportPanel (no polling — the refresh button re-reads).
// Every value is escaped, the card is a real settings section of the
// Automata tab, and every requirements_report_ locale key is pinned in
// all five bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const HOSTILE = '<img src=x onerror=alert(1)>';
const VIEW_PREFIX = 'requirements_report_';

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
  return vm.runInContext('window.requirementsReportCardHtml(_report)', sandbox);
}

function docsLink(key) {
  const sandbox = loadApp();
  sandbox._key = key;
  return vm.runInContext('docsLinkForKey(_key)', sandbox);
}

function node(over) { return { name: 'node-a', found: '0.9.6', required: '0.9.7', flagged: false, ...over }; }
function cap(over) {
  return { name: 'memory', minimum_ollama: '0.9.7', nodes: [node()], satisfied: true, ...over };
}
function backend(over) {
  return { name: 'judge', backends: [{ name: 'ollama', minimum: '0.9.7', flagged: false }], ...over };
}
function report(over) {
  return { capabilities: [cap()], automation_backends: [backend()], missing: [], ...over };
}

// ── rendering ───────────────────────────────────────────────────────────────

test('a satisfied report shows the three parts, the minimum and nothing missing', () => {
  const html = render(report());
  assert.ok(html.includes('Requirements report'), 'card title missing: ' + html);
  assert.ok(html.includes('Capabilities'), 'capabilities part missing');
  assert.ok(html.includes('Requirement backends'), 'backends part missing');
  assert.ok(html.includes('Missing'), 'missing part missing');
  assert.ok(html.includes('memory'), 'capability name missing');
  assert.ok(html.includes('minimum 0.9.7'), 'minimum missing');
  assert.ok(html.includes('node-a'), 'node missing');
  assert.ok(html.includes('>0.9.6</code>'), 'node found version missing: ' + html);
  assert.ok(html.includes('satisfied'), 'satisfied state missing');
  assert.ok(html.includes('Nothing is missing.'), 'all-ok line missing');
  assert.ok(!html.includes('requirements-report-flag'), 'a clean report must not carry flags');
});

test('a flagged node below its minimum is marked, and an unsatisfied capability says so', () => {
  const html = render(report({
    capabilities: [cap({ satisfied: false, override: '0.10.0', nodes: [node({ flagged: true }), node({ name: 'node-b', found: null })] })],
  }));
  assert.ok(html.includes('not satisfied'), 'unsatisfied state missing: ' + html);
  assert.ok(html.includes('below minimum'), 'flag missing');
  assert.ok(html.includes('override 0.10.0'), 'override missing');
  assert.ok(html.includes('not installed'), 'a node without a found version reads not installed');
});

test('a backend minimum below its floor carries the flag', () => {
  const html = render(report({ automation_backends: [backend({ backends: [{ name: 'ollama', minimum: '0.9.7', flagged: true }] })] }));
  assert.ok(html.includes('judge'), 'backend role missing');
  assert.ok(html.includes('minimum 0.9.7'), 'backend minimum missing');
  assert.ok(html.includes('below minimum'), 'flag missing');
});

test('missing items are listed; an empty report still renders its parts', () => {
  const html = render(report({ missing: [{ title: 'A judge backend is configured' }, 'raw-item'] }));
  assert.ok(html.includes('A judge backend is configured'), 'missing item missing: ' + html);
  assert.ok(html.includes('raw-item'), 'raw missing entry missing');
  const empty = render(report({ capabilities: [], automation_backends: [], missing: [] }));
  assert.ok(empty.includes('No capabilities in the report yet.'), empty);
  assert.ok(empty.includes('No requirement backend minimums in the report yet.'), empty);
  assert.ok(empty.includes('Nothing is missing.'), empty);
});

test('a missing report shows the placeholder with the card title', () => {
  for (const html of [render(null), render(undefined)]) {
    assert.ok(html.includes('Requirements report'));
    assert.ok(html.includes('has not loaded yet'), html);
  }
});

test('every value from the API is escaped', () => {
  const html = render(report({
    capabilities: [cap({ name: HOSTILE, minimum_ollama: HOSTILE, override: HOSTILE, nodes: [node({ name: HOSTILE, found: HOSTILE })] })],
    automation_backends: [backend({ name: HOSTILE, backends: [{ name: HOSTILE, minimum: HOSTILE }] })],
    missing: [{ title: HOSTILE }, HOSTILE],
  }));
  assert.ok(!html.includes(HOSTILE), 'hostile markup rendered unescaped: ' + html);
  assert.ok(!html.includes('<img'), 'an image element was injected');
});

test('the card carries a refresh button that re-reads the report through the list loader', () => {
  const html = render(report());
  assert.ok(html.includes('requirements-report-refresh'), html);
  assert.ok(html.includes('window._listLoaders.automation_requirements_report.load()'), 'refresh does not reload: ' + html);
  assert.ok(html.includes('>Refresh<'), html);
});

test('the card and its parts carry help links to the glossary sections', () => {
  const html = render(report());
  assert.ok(html.includes('class="docs-link"'), 'help link missing: ' + html);
  assert.ok(html.includes('/diagrams.html#docs/datawatch-definitions.md#requirements-report"'), 'report link missing: ' + html);
});

// ── loader wiring ───────────────────────────────────────────────────────────

test('the setup view wires the requirements report panel and the refresh reads the same endpoint', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.ok(src.includes("id=\"requirementsReportPanel\""), 'the settings panel is not in the setup view: ' + src.slice(src.indexOf('requirementsReportPanel') - 200, src.indexOf('requirementsReportPanel') + 200));
  assert.ok(src.includes("/api/automation/requirements-report"), 'the report endpoint is not wired');
  const start = src.indexOf('REQUIREMENTS_REPORT_LOADER =');
  const end = src.indexOf('Model pull consent', start);
  const block = src.slice(start, end);
  assert.ok(block.includes("listLoader({"), 'the report must read through the shared list loader');
  assert.ok(!/setInterval/.test(block), 'the report must not start its own interval');
});

test('docsLinkForKey maps the card and its parts to the glossary section', () => {
  const base = 'datawatch-definitions.md';
  for (const k of ['requirements_report', 'requirements_report_card_title', 'requirements_report_capabilities', 'requirements_report_backends', 'requirements_report_missing']) {
    assert.equal(docsLink(k), base + '#requirements-report', k);
  }
});

// ── locale bundles ──────────────────────────────────────────────────────────

test('every requirements_report_ key is in all five bundles with a real translation', () => {
  const en = bundle('en');
  const keys = Object.keys(en).filter(k => k.startsWith(VIEW_PREFIX));
  assert.ok(keys.length >= 15, 'expected the full key set, got ' + keys.length);
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of keys) {
      assert.equal(typeof b[k], 'string', `${lang} lacks ${k}`);
      assert.ok(b[k].trim() !== '', `${lang} has an empty ${k}`);
      if (lang !== 'en') assert.notEqual(b[k], en[k], `${lang} ${k} is an untranslated copy of English`);
      const ph = s => (s.match(/%\d\$s/g) || []).sort().join(',');
      assert.equal(ph(b[k]), ph(en[k]), `${lang} ${k} changes the placeholders`);
    }
  }
});
