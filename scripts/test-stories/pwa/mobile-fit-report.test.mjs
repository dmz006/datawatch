// scripts/test-stories/pwa/mobile-fit-report.test.mjs — node:test coverage for
// buildReport, formatText and writeReport. No browser needed: everything is
// driven by plain in-memory run fixtures.
import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import {
  buildReport,
  formatText,
  writeReport,
  normaliseSelector,
  groupIdOf,
} from './mobile-fit-report.mjs';

function makeRun(overrides = {}) {
  return {
    scenario: 'session-list',
    view: 'sessions',
    state: 'loaded',
    theme: 'dark',
    zoom: 100,
    viewport: { width: 375, height: 667, orientation: 'portrait' },
    screenshot: 'shots/session-list-dark.png',
    findings: [],
    ...overrides,
  };
}

test('normaliseSelector collapses nth-child and nth-of-type position variants', () => {
  assert.equal(
    normaliseSelector('#list > li:nth-child(2) > .label'),
    '#list > li:nth(*) > .label');
  assert.equal(
    normaliseSelector('#list > li:nth-of-type(7)'),
    '#list > li:nth(*)');
  assert.equal(normaliseSelector('#plain[title=hi]'), '#plain[title=hi]');
  assert.equal(normaliseSelector(42), '');
});

test('two positional variants of one selector share a group with a stable id', () => {
  const runs = [
    makeRun({
      findings: [{
        check: 'tap-target',
        selector: '#list > li:nth-child(1) > button',
        detail: 'tap target 24x24 below minimum 40px (width 24 < 40)',
      }],
    }),
    makeRun({
      scenario: 'session-detail',
      findings: [{
        check: 'tap-target',
        selector: '#list > li:nth-child(4) > button',
        detail: 'tap target 30x30 below minimum 40px (width 30 < 40)',
      }],
    }),
    makeRun({
      findings: [{
        check: 'tap-target',
        selector: '#other .row:nth-of-type(3)',
        detail: 'tap target 20x20 below minimum 40px',
        screenshot: 'shots/other-variant.png',
      }],
    }),
  ];

  const report = buildReport(runs, { view: 'sessions' });
  assert.equal(report.summary.groups, 2);
  const [shared, other] = report.groups;
  assert.equal(shared.check, 'tap-target');
  assert.equal(shared.selector, '#list > li:nth(*) > button');
  assert.equal(shared.count, 2);
  assert.deepStrictEqual(shared.scenarios, ['session-list', 'session-detail']);
  assert.equal(other.selector, '#other .row:nth(*)');

  // Findings keep their own un-normalised selector.
  assert.equal(report.findings[0].selector, '#list > li:nth-child(1) > button');
  assert.equal(report.findings[1].selector, '#list > li:nth-child(4) > button');
  assert.equal(report.findings[0].group, shared.id);
  assert.equal(report.findings[1].group, shared.id);
  assert.match(shared.id, /^G-[0-9a-f]{8}$/);
  assert.notEqual(shared.id, other.id);
});

test('group ids are stable across identical inputs', () => {
  const runs = [
    makeRun({
      findings: [{
        check: 'edge-cross',
        selector: '#chart > .bar:nth-child(3)',
        detail: 'element crosses viewport edge: right 512 > viewport 375',
      }],
    }),
  ];
  const first = buildReport(runs);
  const second = buildReport(runs);
  assert.equal(first.groups.length, second.groups.length);
  for (let i = 0; i < first.groups.length; i += 1) {
    assert.equal(first.groups[i].id, second.groups[i].id);
  }
  assert.equal(first.findings[0].group, second.findings[0].group);
  assert.equal(groupIdOf('edge-cross', '#chart > .bar:nth-child(3)'),
               groupIdOf('edge-cross', '#chart > .bar:nth-child(9)'));
  assert.equal(first.findings[0].id, 'F-0001');
});

test('summary counts reflect findings, checks, scenarios and groups', () => {
  const runs = [
    makeRun({
      scenario: 'session-list',
      findings: [
        { check: 'edge-cross', selector: '#chart > .bar:nth-child(1)', detail: 'right 520 > viewport 375' },
        { check: 'tap-target', selector: '#sidebar a', detail: 'tap target 36x18 below minimum 40px' },
        { check: 'tap-target', selector: '#sidebar button', detail: 'tap target 12x12 below minimum 40px' },
      ],
    }),
    makeRun({
      scenario: 'terminal',
      error: 'page.evaluate failed: target closed',
      findings: [],
    }),
  ];

  const meta = { view: 'sessions', profile: 'mobile', report_only: true };
  const report = buildReport(runs, meta);

  assert.equal(report.summary.total, 3);
  assert.deepStrictEqual(report.summary.by_check, { 'edge-cross': 1, 'tap-target': 2 });
  assert.equal(report.summary.scenarios, 2);
  assert.equal(report.summary.scenarios_with_findings, 1);
  assert.equal(report.summary.groups, 3);
  assert.equal(report.meta, meta);
  assert.match(report.generated_at, /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/);
  assert.deepStrictEqual(report.findings.map((f) => f.id), ['F-0001', 'F-0002', 'F-0003']);
  assert.ok(report.findings.every((f) => f.viewport.width === 375
    && f.viewport.height === 667 && f.viewport.orientation === 'portrait'));
});

test('an errored run with no findings is preserved and counted', () => {
  const errored = makeRun({
    scenario: 'terminal',
    error: 'page.evaluate failed: target closed',
    screenshot: null,
    findings: [],
  });
  const report = buildReport([errored]);
  assert.ok(report.runs.some((r) => r.error === 'page.evaluate failed: target closed'));
  assert.equal(report.summary.scenarios, 1);
  assert.equal(report.summary.scenarios_with_findings, 0);
  assert.equal(report.summary.total, 0);
});

test('finding screenshot falls back to the run screenshot', () => {
  const runs = [
    makeRun({
      screenshot: 'shots/run-level.png',
      findings: [
        { check: 'overlap', selector: '#pane .header', detail: 'sibling overlap with #pane .title' },
        {
          check: 'overlap',
          selector: '#pane .footer',
          detail: 'sibling overlap with #pane .body',
          screenshot: 'shots/finding-level.png',
        },
      ],
    }),
    makeRun({
      screenshot: null,
      error: 'screenshot step timed out',
      findings: [{ check: 'clipped-text', selector: '#note .body', detail: 'clipped, no tooltip' }],
    }),
  ];
  const report = buildReport(runs);
  assert.equal(report.findings[0].screenshot, 'shots/run-level.png');
  assert.equal(report.findings[1].screenshot, 'shots/finding-level.png');
  assert.equal(report.findings[2].screenshot, null);
  assert.equal(report.groups[0].first_screenshot, 'shots/run-level.png');
});

test('an empty report has zeroed summary and says no findings in text', () => {
  const report = buildReport([], { view: 'sessions' });
  assert.equal(report.summary.total, 0);
  assert.deepStrictEqual(report.summary.by_check, {});
  assert.equal(report.summary.scenarios, 0);
  assert.equal(report.summary.scenarios_with_findings, 0);
  assert.equal(report.summary.groups, 0);
  assert.deepStrictEqual(report.groups, []);
  assert.deepStrictEqual(report.findings, []);
  assert.match(formatText(report), /no findings/);
});

test('formatText lists selector, viewport and screenshot for group examples', () => {
  const report = buildReport([
    makeRun({
      findings: Array.from({ length: 7 }, (_, i) => ({
        check: 'edge-cross',
        selector: '#chart > .bar:nth-child(' + (i + 1) + ')',
        detail: 'element crosses viewport edge: right ' + (400 + i) + ' > viewport 375',
      })),
    }),
  ]);
  const text = formatText(report);
  const lines = text.split('\n');
  const groupIdx = lines.findIndex((l) => /^G-[0-9a-f]{8} \[edge-cross\]/.test(l));
  assert.ok(groupIdx >= 0);
  const groupLine = lines[groupIdx];
  assert.match(groupLine, /^G-[0-9a-f]{8} \[edge-cross\] #chart > .bar:nth\(\*\) - 7 findings$/);
  // Cap of 5 examples per group even with 7 findings.
  const exampleLines = lines.slice(groupIdx + 1, groupIdx + 6);
  assert.equal(exampleLines.length, 5);
  for (const line of exampleLines) {
    assert.match(line, /^  viewport 375x667 portrait, dark, zoom100%, loaded: element crosses viewport edge: right \d+ > viewport 375 -> shots\/session-list-dark\.png$/);
  }
});

test('writeReport writes mobile-fit-report.json and .txt with matching contents', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'mobile-fit-report-'));
  try {
    const runs = [
      makeRun({
        findings: [
          { check: 'tap-target', selector: '#sidebar a', detail: 'tap target 36x18 below minimum 40px' },
          { check: 'tap-target', selector: '#sidebar button', detail: 'tap target 12x12 below minimum 40px' },
        ],
      }),
    ];
    const meta = { view: 'sessions', profile: 'mobile', report_only: true };
    const paths = await writeReport(buildReport(runs, meta), join(dir, 'nested', 'out'));

    assert.equal(paths.json, join(dir, 'nested', 'out', 'mobile-fit-report.json'));
    assert.equal(paths.text, join(dir, 'nested', 'out', 'mobile-fit-report.txt'));

    const parsed = JSON.parse(await readFile(paths.json, 'utf8'));
    assert.equal(parsed.summary.total, 2);
    assert.equal(parsed.summary.groups, 2);
    assert.equal(parsed.findings[0].id, 'F-0001');
    assert.equal(parsed.meta.report_only, true);
    // 2-space indent: the second line is indented, not packed.
    assert.match(await readFile(paths.json, 'utf8'), /^\{\n  "generated_at": /);

    const text = await readFile(paths.text, 'utf8');
    assert.match(text, /\[tap-target\] #sidebar a - 1 finding/);
    assert.match(text, /viewport 375x667 portrait, dark, zoom100%, loaded: tap target 36x18 below minimum 40px -> shots\/session-list-dark\.png/);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});
