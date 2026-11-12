// scripts/test-stories/pwa/mobile-fit-report.mjs — turn raw mobile-fit runs
// into the report that drives the fix work: a JSON document plus a readable
// text listing with the CSS selector, viewport and screenshot path for every
// finding.
//
// Pure data module: no browser, no Playwright import. The harness (see
// run-all.mjs) collects the raw `runs` and hands them to buildReport;
// writeReport files the result under an output directory.
//
// A run: { scenario, view, state, theme, zoom,
//          viewport: { width, height, orientation },
//          screenshot?, error?,
//          findings: [{ check, selector, detail, rect?, screenshot? }] }
// A run carrying an `error` string and no findings is preserved verbatim in
// report.runs and counted in summary.scenarios, so harness failures stay
// visible. A finding's own screenshot wins; otherwise it falls back to the
// run's screenshot. `meta` is carried through unchanged (callers put view,
// profile and report_only there).
//
// Grouping: the group key is check + '|' + selector, with every
// ':nth-child(N)' and ':nth-of-type(N)' replaced by ':nth(*)'; the group id
// is 'G-' + the first 8 hex characters of the sha1 (node:crypto) of that key,
// so ids are stable across runs of the same app. A finding keeps its own
// un-normalised selector; the group records the normalised one.

import { createHash } from 'node:crypto';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

// Cap on unique scenario names listed per group and example lines per group
// in the text report.
const MAX_SCENARIOS_PER_GROUP = 20;
const MAX_EXAMPLES_PER_GROUP = 5;

// Collapse positional pseudo-classes so the N sibling of the same element
// family lands in one fix group.
export function normaliseSelector(selector) {
  if (typeof selector !== 'string') return '';
  return selector
    .replace(/:nth-child\(\d+\)/g, ':nth(*)')
    .replace(/:nth-of-type\(\d+\)/g, ':nth(*)');
}

export function groupKeyOf(check, selector) {
  return (typeof check === 'string' ? check : '') + '|' + normaliseSelector(selector);
}

export function groupIdOf(check, selector) {
  const hash = createHash('sha1').update(groupKeyOf(check, selector)).digest('hex');
  return 'G-' + hash.slice(0, 8);
}

function viewportOf(run) {
  const v = run.viewport && typeof run.viewport === 'object' ? run.viewport : {};
  return {
    width: v.width == null ? null : v.width,
    height: v.height == null ? null : v.height,
    orientation: v.orientation == null ? null : v.orientation,
  };
}

// Build the report object. `runs` is the raw harness output; `meta` is
// carried through unchanged.
export function buildReport(runs, meta = {}) {
  const allRuns = (Array.isArray(runs) ? runs : [])
    .map((r) => (r && typeof r === 'object' ? r : {}));

  const findings = [];
  const byCheck = {};
  const groups = [];
  const groupsByKey = new Map();
  let scenariosWithFindings = 0;

  for (const run of allRuns) {
    const runFindings = Array.isArray(run.findings) ? run.findings : [];
    if (runFindings.length > 0) scenariosWithFindings += 1;

    for (const f of runFindings) {
      if (!f || typeof f !== 'object') continue;
      const check = typeof f.check === 'string' && f.check.trim() ? f.check.trim() : 'unknown';
      const selector = typeof f.selector === 'string' && f.selector.trim() ? f.selector.trim() : '';
      const detail = typeof f.detail === 'string' ? f.detail.trim() : '';
      const screenshot = (typeof f.screenshot === 'string' && f.screenshot)
        || (typeof run.screenshot === 'string' && run.screenshot)
        || null;

      const key = groupKeyOf(check, selector);
      let group = groupsByKey.get(key);
      if (!group) {
        group = {
          id: groupIdOf(check, selector),
          check,
          selector: normaliseSelector(selector),
          count: 0,
          scenarios: [],
          first_screenshot: null,
        };
        groupsByKey.set(key, group);
        groups.push(group);
      }
      if (group.first_screenshot == null && screenshot) group.first_screenshot = screenshot;
      const scenarioName = typeof run.scenario === 'string' && run.scenario
        ? run.scenario : 'unnamed';
      if (!group.scenarios.includes(scenarioName)
          && group.scenarios.length < MAX_SCENARIOS_PER_GROUP) {
        group.scenarios.push(scenarioName);
      }

      byCheck[check] = (byCheck[check] || 0) + 1;
      group.count += 1;
      const id = 'F-' + String(findings.length + 1).padStart(4, '0');
      findings.push({
        id,
        group: group.id,
        check,
        selector,
        detail,
        scenario: scenarioName,
        view: run.view == null ? null : run.view,
        state: run.state == null ? null : run.state,
        theme: run.theme == null ? null : run.theme,
        zoom: run.zoom == null ? null : run.zoom,
        viewport: viewportOf(run),
        screenshot,
      });
    }
  }

  return {
    generated_at: new Date().toISOString(),
    meta,
    summary: {
      total: findings.length,
      by_check: byCheck,
      scenarios: allRuns.length,
      scenarios_with_findings: scenariosWithFindings,
      groups: groups.length,
    },
    groups,
    findings,
    runs: allRuns,
  };
}

function zoomPct(zoom) {
  if (zoom == null) return 'unknown';
  const s = String(zoom).trim();
  if (s.endsWith('%')) return s;
  return s + '%';
}

// Human-readable listing: header (generated_at, meta values, totals by
// check), then one block per group with up to 5 example lines. Every listed
// example carries the CSS selector (in the group heading), the viewport and
// the screenshot path.
export function formatText(report) {
  const lines = [];
  const summary = report && report.summary && typeof report.summary === 'object'
    ? report.summary : {};
  const groups = report && Array.isArray(report.groups) ? report.groups : [];
  const findings = report && Array.isArray(report.findings) ? report.findings : [];

  lines.push('mobile-fit report');
  if (report && report.generated_at) lines.push('generated_at: ' + report.generated_at);
  const meta = report && report.meta && typeof report.meta === 'object' ? report.meta : {};
  for (const [key, value] of Object.entries(meta)) {
    const shown = (typeof value === 'string' || typeof value === 'number')
      ? String(value) : JSON.stringify(value);
    lines.push('meta.' + key + ': ' + shown);
  }
  lines.push('');
  lines.push('totals: '
    + (summary.total == null ? 0 : summary.total) + ' finding(s) in '
    + (summary.scenarios == null ? 0 : summary.scenarios) + ' scenario run(s), '
    + (summary.groups == null ? 0 : summary.groups) + ' group(s)');
  const byCheck = summary.by_check && typeof summary.by_check === 'object'
    ? summary.by_check : {};
  for (const check of Object.keys(byCheck).sort()) {
    lines.push('  ' + check + ': ' + byCheck[check]);
  }

  if (groups.length === 0) {
    lines.push('');
    lines.push('no findings');
    return lines.join('\n') + '\n';
  }

  for (const group of groups) {
    lines.push('');
    lines.push(group.id + ' [' + group.check + '] ' + group.selector
      + ' - ' + group.count + (group.count === 1 ? ' finding' : ' findings'));
    const examples = findings
      .filter((f) => f && f.group === group.id)
      .slice(0, MAX_EXAMPLES_PER_GROUP);
    for (const f of examples) {
      const vp = f.viewport || {};
      lines.push('  viewport ' + vp.width + 'x' + vp.height + ' ' + (vp.orientation || 'unknown')
        + ', ' + (f.theme == null ? 'unknown' : f.theme)
        + ', zoom' + zoomPct(f.zoom)
        + ', ' + (f.state == null ? 'unknown' : f.state)
        + ': ' + f.detail
        + ' -> ' + (f.screenshot || '(no screenshot)'));
    }
  }
  return lines.join('\n') + '\n';
}

// File the report under outDir (created recursively) and return the two paths.
export async function writeReport(report, outDir) {
  await mkdir(outDir, { recursive: true });
  const jsonPath = join(outDir, 'mobile-fit-report.json');
  const textPath = join(outDir, 'mobile-fit-report.txt');
  await writeFile(jsonPath, JSON.stringify(report, null, 2) + '\n', 'utf8');
  await writeFile(textPath, formatText(report), 'utf8');
  return { json: jsonPath, text: textPath };
}
