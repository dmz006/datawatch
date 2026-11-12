// 2026-10-17 — plan coverage filters (PWA): the suppressed-findings line
// and the "?" definition link in the plan coverage panel, the four new
// autonomous settings fields, and the DOCS_LINKS_BY_KEY registration for
// the plan-coverage-filters definition anchor. Loads the real app.js
// into the stub browser (testutil_browser_stub) and exercises the real
// functions.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALE_DIR = path.join(__dirname, 'locales');
const LOCALE_LIST = ['en', 'de', 'es', 'fr', 'ja'];

function loadAppJS() {
  const sandbox = buildSandbox({});
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  return sandbox;
}

function call(sandbox, fnName, ...args) {
  sandbox.__args = args;
  const result = vm.runInContext(`${fnName}(...__args)`, sandbox);
  delete sandbox.__args;
  return result;
}

test('panel shows the suppressed counts with both labels when suppressed is present', () => {
  const sandbox = loadAppJS();
  const prd = {
    id: 'p-sup',
    status: 'draft',
    plan_coverage: {
      mode: 'block',
      min_ratio: 1,
      report: { covered: 5, missing: 0, weakened: 0, unmatched: [] },
      findings: [],
      suppressed: { created_files: 3, requirements: 2 },
    },
  };
  const html = call(sandbox, '_prdPlanCoverageBlock', prd);
  // t() returns the key itself when the bundle is empty (test env).
  assert.match(html, /prd_plan_coverage_suppressed_title/);
  assert.match(html, /prd_plan_coverage_suppressed_files/);
  assert.match(html, /prd_plan_coverage_suppressed_requirements/);
  // The line reads "<title>: 3 <files>, 2 <requirements>".
  const m = html.match(/prd_plan_coverage_suppressed_title: (\d+) [^<]*?prd_plan_coverage_suppressed_files, (\d+) [^<]*?prd_plan_coverage_suppressed_requirements/);
  assert.ok(m, 'suppressed line with both counts present');
  assert.equal(m[1], '3', 'created_files count rendered');
  assert.equal(m[2], '2', 'requirements count rendered');
});

test('suppressed line carries the definitions help link', () => {
  const sandbox = loadAppJS();
  const prd = {
    id: 'p-help',
    status: 'draft',
    plan_coverage: {
      mode: 'off',
      report: { covered: 1, missing: 0, weakened: 0 },
      suppressed: { created_files: 0, requirements: 0 },
    },
  };
  const html = call(sandbox, '_prdPlanCoverageBlock', prd);
  assert.match(html, /href="\/diagrams\.html#docs\/datawatch-definitions\.md#plan-coverage-filters"/);
  assert.match(html, /prd_plan_coverage_filters_help/);
});

test('panel shows the suppressed line even when both counts are zero', () => {
  const sandbox = loadAppJS();
  const prd = {
    id: 'p-zero',
    status: 'draft',
    plan_coverage: {
      mode: 'block',
      report: { covered: 2, missing: 0, weakened: 0 },
      suppressed: { created_files: 0, requirements: 0 },
    },
  };
  const html = call(sandbox, '_prdPlanCoverageBlock', prd);
  assert.match(html, /prd_plan_coverage_suppressed_title/);
  assert.match(html, /: 0 [^<]*?prd_plan_coverage_suppressed_files, 0 [^<]*?prd_plan_coverage_suppressed_requirements/);
});

test('panel does not throw and shows no suppressed line when suppressed is absent', () => {
  const sandbox = loadAppJS();
  const prd = {
    id: 'p-nosupp',
    status: 'draft',
    plan_coverage: {
      mode: 'warn',
      report: { covered: 1, missing: 1, weakened: 0, unmatched: [{ requirement: 'must log' }] },
      findings: [{ rule: 'unresolved-file', detail: 'nothing/src/x.go' }],
    },
  };
  const html = call(sandbox, '_prdPlanCoverageBlock', prd);
  assert.match(html, /prd-plan-coverage/);
  assert.doesNotMatch(html, /prd_plan_coverage_suppressed_title/);
  // null plan_coverage still renders nothing.
  assert.equal(call(sandbox, '_prdPlanCoverageBlock', { id: 'p-none' }), '');
});

test('the four plan coverage filter settings fields exist with the right keys, types and defaults', () => {
  const sandbox = loadAppJS();
  const run = (key) => {
    const field = vm.runInContext(
      "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.find(f => f.key === '" + key + "') || null)",
      sandbox);
    return JSON.parse(field);
  };
  const suppress = run('autonomous.plan_coverage_suppress_created_files');
  assert.ok(suppress, 'autonomous.plan_coverage_suppress_created_files missing');
  assert.equal(suppress.type, 'toggle');
  assert.equal(suppress.default, true);
  assert.equal(suppress.labelKey, 'settings_plan_coverage_suppress_created_files');
  assert.equal(suppress.helpKey, 'settings_plan_coverage_suppress_created_files_help');

  const scoped = run('autonomous.plan_coverage_scoped_requirements');
  assert.ok(scoped, 'autonomous.plan_coverage_scoped_requirements missing');
  assert.equal(scoped.type, 'toggle');
  assert.equal(scoped.default, true);
  assert.equal(scoped.labelKey, 'settings_plan_coverage_scoped_requirements');
  assert.equal(scoped.helpKey, 'settings_plan_coverage_scoped_requirements_help');

  const sections = run('autonomous.plan_coverage_requirement_sections');
  assert.ok(sections, 'autonomous.plan_coverage_requirement_sections missing');
  assert.equal(sections.type, 'text');
  assert.equal(sections.placeholder, 'scope,constraints');
  assert.equal(sections.labelKey, 'settings_plan_coverage_requirement_sections');
  assert.equal(sections.helpKey, 'settings_plan_coverage_requirement_sections_help');

  const minChars = run('autonomous.plan_coverage_min_requirement_chars');
  assert.ok(minChars, 'autonomous.plan_coverage_min_requirement_chars missing');
  assert.equal(minChars.type, 'number');
  assert.equal(minChars.min, 0);
  assert.equal(minChars.placeholder, '30');
  assert.equal(minChars.labelKey, 'settings_plan_coverage_min_requirement_chars');
  assert.equal(minChars.helpKey, 'settings_plan_coverage_min_requirement_chars_help');

  // No fallback label names the tracker or says PRD; they name Automaton or
  // stay neutral.
  for (const f of [suppress, scoped, sections, minChars]) {
    assert.doesNotMatch(f.label, /\bPRD\b/i, f.key + ' fallback label must not mention PRD');
  }
});

test('DOCS_LINKS_BY_KEY registers plan_coverage_filters', () => {
  const sandbox = loadAppJS();
  const target = vm.runInContext('DOCS_LINKS_BY_KEY.plan_coverage_filters', sandbox);
  assert.equal(target, 'datawatch-definitions.md#plan-coverage-filters');
});

test('plan coverage filter labels and help exist in all five locale bundles', () => {
  const keys = [
    'settings_plan_coverage_suppress_created_files',
    'settings_plan_coverage_suppress_created_files_help',
    'settings_plan_coverage_scoped_requirements',
    'settings_plan_coverage_scoped_requirements_help',
    'settings_plan_coverage_requirement_sections',
    'settings_plan_coverage_requirement_sections_help',
    'settings_plan_coverage_min_requirement_chars',
    'settings_plan_coverage_min_requirement_chars_help',
    'prd_plan_coverage_suppressed_title',
    'prd_plan_coverage_suppressed_files',
    'prd_plan_coverage_suppressed_requirements',
    'prd_plan_coverage_filters_help',
  ];
  for (const locale of LOCALE_LIST) {
    const bundle = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, locale + '.json'), 'utf8'));
    for (const k of keys) {
      assert.ok(typeof bundle[k] === 'string' && bundle[k].length > 0,
        k + ' missing or empty in ' + locale + ' locale');
    }
  }
});
