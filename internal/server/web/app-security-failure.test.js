// Security failure card (Automation view):
//
//   - window.securityFailureCardHtml(entry) renders one entry of
//     GET /api/autonomous/security-failures as a read-only card: the
//     dependency, advisory identifier, publication date (or unknown), the
//     registry entry with its first_added, added and expires dates (or not
//     in the registry), the recommendation, and the action taken or the
//     would-have-taken line in shadow mode
//   - the card has no control that acts, escapes every value and wires every
//     string through t()
//   - the automation_security_failure_* keys exist in all five bundles
//
//   node --test app-security-failure.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
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

function report(over) {
  return {
    dependency: 'golang.org/x/net',
    advisory_ids: ['GHSA-aaaa-bbbb-cccc'],
    published_at: '2026-10-01',
    fixed_version: '',
    registry_found: true,
    registry_id: 'GHSA-aaaa-bbbb-cccc',
    registry_first_added: '2026-09-01',
    registry_added: '2026-09-15',
    registry_expires: '2026-12-01',
    registry_error: '',
    lookup_error: '',
    recommendation: 'keep',
    note: '',
    mode: 'on',
    action: 'stopped',
    ...over,
  };
}

function entry(over, rep) {
  return {
    repo: 'dmz006/datawatch', ref_kind: 'main', ref_name: 'main', workflow: 'security.yaml',
    run_id: '4242', run_url: 'https://github.com/dmz006/datawatch/actions/runs/4242', status: 'failing',
    report: report(rep), ...over,
  };
}

test('card shows dependency, advisory, publication date and recommendation', () => {
  const app = loadApp();
  const html = app.securityFailureCardHtml(entry());
  assert.match(html, /security-failure-card/);
  assert.match(html, /Dependency: golang\.org\/x\/net/);
  assert.match(html, /Advisory: GHSA-aaaa-bbbb-cccc/);
  assert.match(html, /Published: 2026-10-01/);
  assert.match(html, /Recommendation: keep, the registry holds an entry/);
});

test('card shows the registry entry with its three dates', () => {
  const html = loadApp().securityFailureCardHtml(entry());
  assert.match(html, /Registry entry: GHSA-aaaa-bbbb-cccc \(first added 2026-09-01, added 2026-09-15, expires 2026-12-01\)/);
});

test('missing publication date reads unknown', () => {
  const html = loadApp().securityFailureCardHtml(entry({}, { published_at: '' }));
  assert.match(html, /Published: unknown/);
});

test('an advisory missing from the registry reads not in the registry', () => {
  const html = loadApp().securityFailureCardHtml(entry({}, {
    registry_found: false, registry_id: '', registry_first_added: '', registry_added: '', registry_expires: '',
    recommendation: 'keep_no_entry',
  }));
  assert.match(html, /Registry entry: not in the registry/);
  assert.doesNotMatch(html, /first added/);
  assert.match(html, /Recommendation: keep, but add a registry entry/);
});

test('an unreadable registry reads not in the registry and says why', () => {
  const html = loadApp().securityFailureCardHtml(entry({}, { registry_found: false, registry_error: 'permission denied' }));
  assert.match(html, /not in the registry \(the registry could not be read: permission denied\)/);
});

test('registry dates the report does not hold read unknown', () => {
  const html = loadApp().securityFailureCardHtml(entry({}, { registry_expires: '' }));
  assert.match(html, /expires unknown\)/);
});

test('action taken: stopped the Automaton line', () => {
  const html = loadApp().securityFailureCardHtml(entry());
  assert.match(html, /Action taken: stopped the Automaton line/);
});

test('shadow mode shows the would-have-taken line', () => {
  const html = loadApp().securityFailureCardHtml(entry({}, { mode: 'shadow', action: 'would_stop' }));
  assert.match(html, /Would have taken \(shadow mode\): stop the Automaton line/);
  assert.match(html, /Mode: shadow/);
  assert.doesNotMatch(html, /Action taken: stopped/);
});

test('report-only and unknown actions have their own words', () => {
  const app = loadApp();
  assert.match(app.securityFailureCardHtml(entry({}, { action: 'report_only' })), /Action taken: none, report only/);
  assert.match(app.securityFailureCardHtml(entry({}, { action: 'something_new' })), /Action taken: something_new/);
});

test('fixed version, lookup error and note appear only when present', () => {
  const app = loadApp();
  const bare = app.securityFailureCardHtml(entry());
  assert.doesNotMatch(bare, /Fixed version|Advisory lookup failed|Note:/);
  const full = app.securityFailureCardHtml(entry({}, {
    recommendation: 'upgrade', fixed_version: '0.30.0', lookup_error: 'timeout', note: 'check by hand',
  }));
  assert.match(full, /Fixed version: 0\.30\.0/);
  assert.match(full, /Advisory lookup failed: timeout/);
  assert.match(full, /Note: check by hand/);
  assert.match(full, /Recommendation: upgrade to the fixed version/);
});

test('the card is read-only: no button, input, form or handler', () => {
  const html = loadApp().securityFailureCardHtml(entry());
  assert.doesNotMatch(html, /<button|<input|<select|<textarea|<form|onclick=|onchange=|onsubmit=/i);
});

test('every value is escaped and a hostile run URL is not linked', () => {
  const app = loadApp();
  const html = app.securityFailureCardHtml(entry(
    { repo: HOSTILE, run_url: 'javascript:alert(1)' },
    { dependency: HOSTILE, advisory_ids: [HOSTILE], published_at: HOSTILE, note: HOSTILE, mode: HOSTILE, action: HOSTILE },
  ));
  assert.ok(!html.includes('<img'), html);
  assert.ok(html.includes('&lt;img'), html);
  assert.doesNotMatch(html, /<a /);
});

test('a real run URL is linked safely', () => {
  const html = loadApp().securityFailureCardHtml(entry());
  assert.match(html, /<a href="https:\/\/github\.com\/dmz006\/datawatch\/actions\/runs\/4242" target="_blank" rel="noopener noreferrer">Run 4242<\/a>/);
});

test('a missing report renders the placeholder', () => {
  const app = loadApp();
  for (const bad of [null, undefined, 'x', {}, { report: null }]) {
    assert.match(app.securityFailureCardHtml(bad), /This run carries no security failure report\./);
  }
});

test('the list draws one card per entry and an empty state for none', () => {
  const app = loadApp();
  const two = app.securityFailureListHtml([entry(), entry({ run_id: '4243' })]);
  assert.equal((two.match(/security-failure-card/g) || []).length, 2);
  for (const none of [[], null, undefined, {}]) {
    const html = app.securityFailureListHtml(none);
    assert.match(html, /security-failure-list-empty/);
    assert.match(html, /No security failure reports\./);
  }
});

test('every automation_security_failure_ key exists in all five bundles with a value', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const used = new Set();
  for (const m of src.matchAll(/['"](automation_security_failure_[a-z_]+)['"]/g)) used.add(m[1]);
  assert.ok(used.size >= 20, 'found ' + used.size + ' keys in app.js');
  const en = bundle('en');
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of used) {
      assert.equal(typeof b[k], 'string', lang + ' missing ' + k);
      assert.notEqual(b[k].trim(), '', lang + ' empty ' + k);
      assert.doesNotMatch(b[k], /\bPRDs?\b/, lang + ' ' + k + ' carries PRD wording');
    }
  }
  for (const k of Object.keys(en).filter(k => k.startsWith('automation_security_failure_'))) {
    assert.ok(used.has(k), 'en key ' + k + ' is not used by app.js');
  }
});
