#!/usr/bin/env node
// TS-802 — Observer mobile-fit regression guard (recorded-unfixed-baseline guard)
// tags: surface:pwa feature:pwa-observer-mobile-fit conflict:pwa-pwa
//
// The Observer / System Statistics mobile layout fixes are not in this tree yet.
// `docs/testing/runs/observer-mobile-fit/repro-report.json` (2026-10-23) recorded
// 29,968 findings in 68 groups on the UNFIXED tree. This story locks that
// unfixed baseline: it replays the recorded scenario set against a live daemon
// and FAILS (exit 1) while any of the 68 recorded finding groups (by group id)
// still produces findings.
//
// Replay set (all at the recorded viewport / theme / zoom / scope per group):
//   - every scenario name present in mobile-fit-observer-regressions.json
//     (8 matrix cells + 3 interactions + the named system-stats-phone scenario);
//   - the full Observer "quick" profile matrix (14 states x {320x568, 360x740,
//     390x844, 768x1024} dark + 740x360 landscape + 360x740 light + z200 state),
//     a superset covering the remaining recorded cells;
//   - the named scenario system-stats-phone (three-peers, 360x740, dark, z100,
//     scope #settings-sec-stats);
//   - the three interactions: section-collapse-expand, refresh, rerender-timers.
//
// Like the mobile-fit harness (mobile-fit.mjs) it: builds a fresh browser
// context per (viewport, theme, zoom), sets the cs_theme / cs_token localStorage
// keys via context.addInitScript, connects via lib.mjs connectToPWA, seeds the
// API state, drives the Observer view, settles, then runs collectFindings.
// The mobile-fit report is re-emitted into this story's evidence directory
// ($EVIDENCE_DIR/TS-802/mobile-fit-report.json + .txt).
//
// Runs through runStory (single retry on transient navigation errors).
// Exit 1 = recorded regressions still present (expected on the unfixed tree),
// or a harness error. Exit 0 = none of the 68 recorded groups reproduce.
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import { runStory, connectToPWA, env } from './lib.mjs';
import { buildMatrix, cssViewportForZoom } from './mobile-fit-matrix.mjs';
import { collectFindings } from './mobile-fit-lib.mjs';
import { buildReport, writeReport } from './mobile-fit-report.mjs';
import { observerProvider } from './mobile-fit-observer.mjs';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const REGRESSIONS_PATH = path.join(HERE, 'mobile-fit-observer-regressions.json');

const CELL_RE = /^(.+)@(\d+)x(\d+)\/(light|dark)\/z(\d+)$/;

// --- recorded regression set ------------------------------------------------

function loadRecordedGroups() {
  const doc = JSON.parse(readFileSync(REGRESSIONS_PATH, 'utf8'));
  const list = Array.isArray(doc) ? doc : doc.groups;
  if (!Array.isArray(list) || list.length === 0) {
    throw new Error(`no recorded regression groups found in ${REGRESSIONS_PATH}`);
  }
  return list;
}

function parseCell(name) {
  const m = CELL_RE.exec(name);
  if (!m) throw new Error(`unparseable cell scenario: ${name}`);
  const state = m[1];
  const vp = { width: Number(m[2]), height: Number(m[3]), orientation: Number(m[2]) > Number(m[3]) ? 'landscape' : 'portrait' };
  return { scenario: name, viewport: vp, theme: m[4], zoom: Number(m[5]), state };
}

// Replay set: quick-profile matrix cells + every recorded cell scenario that is
// not already covered (the zoom cells z130/z200) — de-duplicated by scenario name.
function buildReplayCells(recorded) {
  const quickCells = buildMatrix({
    profile: 'quick',
    states: observerProvider.states,
    quickStates: observerProvider.quickStates,
    zoomStates: observerProvider.zoomStates,
    exhaustive: false,
  });
  const byName = new Map(quickCells.map((c) => [c.scenario, c]));
  for (const entry of recorded) {
    const name = entry.scenario;
    if (!name || name.startsWith('interaction:') || !CELL_RE.test(name)) continue;
    if (!byName.has(name)) byName.set(name, parseCell(name));
  }
  return [...byName.values()];
}

// --- scenario runner (mirrors mobile-fit.mjs cell handling) -----------------

async function createContext(browser, ctxKey, vp, theme, zoom) {
  const css = cssViewportForZoom(vp, zoom);
  const context = await browser.newContext({
    ignoreHTTPSErrors: true,
    viewport: { width: css.width, height: css.height },
    deviceScaleFactor: css.scaleFactor,
  });
  await context.addInitScript(([t, token]) => {
    try {
      localStorage.setItem('cs_theme', t);
      localStorage.setItem('cs_token', token);
    } catch { /* page not usable */ }
  }, [theme, env.token]);
  const page = await context.newPage();
  await connectToPWA(page);
  return { name: ctxKey, vp, theme, zoom, context, page };
}

async function withTimeout(promise, ms, label) {
  let timer;
  try {
    return await Promise.race([
      promise,
      new Promise((_, reject) => {
        timer = setTimeout(() => reject(new Error(`${label} timed out after ${ms} ms`)), ms);
      }),
    ]);
  } finally {
    clearTimeout(timer);
  }
}

async function attachScreenshot(page, evidenceDir, scenario, fullPage = true) {
  const safe = scenario.replace(/[^a-z0-9]/gi, '-');
  const rel = path.posix.join('screenshots', `${safe}.png`);
  const abs = path.join(evidenceDir, rel);
  await page.screenshot({ path: abs, fullPage, animations: 'disabled' });
  return rel;
}

async function runCellScenario(ctx, cell, evidenceDir) {
  const run = {
    scenario: cell.scenario,
    view: 'observer',
    state: cell.state,
    theme: cell.theme,
    zoom: cell.zoom,
    viewport: { width: cell.viewport.width, height: cell.viewport.height, orientation: cell.viewport.orientation || 'portrait' },
    findings: [],
  };
  let failed = false;
  try {
    await observerProvider.seed(ctx.page, cell.state);
    await observerProvider.open(ctx.page);
    await observerProvider.settle(ctx.page, cell.state, 'settled');
    run.findings = await collectFindings(ctx.page, {});
  } catch (err) {
    run.error = (err && err.message) ? err.message : String(err);
    failed = true;
  }
  if (run.findings.length) run.screenshot = await attachScreenshot(ctx.page, evidenceDir, cell.scenario).catch(() => null);
  else if (failed) run.screenshot = await attachScreenshot(ctx.page, evidenceDir, cell.scenario, false).catch(() => null);
  return run;
}

const NAMED_TIMEOUT_MS = 60000;
const INTERACTION_TIMEOUT_MS = 90000;

// --- story ------------------------------------------------------------------

await runStory(async (page) => {
  const browser = (page.context() && page.context().browser()) || (await import('./lib.mjs')).launchBrowser();
  const evidenceDir = path.join(env.evidenceDir, env.story);

  const recorded = loadRecordedGroups();
  const cells = buildReplayCells(recorded);
  const named = observerProvider.named.filter((n) => n.name === 'system-stats-phone');
  if (named.length === 0) throw new Error('provider has no system-stats-phone named scenario');
  const interactions = observerProvider.interactions;

  console.log(`[TS-802] ${recorded.length} recorded groups; replaying ${cells.length} cells + ${named.length} named + ${interactions.length} interactions`);

  // Group cells by context key (viewport+theme+zoom); one browser context per group.
  const contextPlan = new Map();
  for (const cell of cells) {
    const key = `${cell.viewport.width}x${cell.viewport.height}/${cell.theme}/z${cell.zoom}`;
    if (!contextPlan.has(key)) contextPlan.set(key, { key, vp: cell.viewport, theme: cell.theme, zoom: cell.zoom, cells: [] });
    contextPlan.get(key).cells.push(cell);
  }
  const namedEntry = named[0];
  const namedKey = `${namedEntry.viewport.width}x${namedEntry.viewport.height}/${namedEntry.theme}/z${namedEntry.zoom}`;

  const runs = [];
  const liveContexts = [];

  try {
    for (const plan of contextPlan.values()) {
      let ctx = null;
      try {
        ctx = await createContext(browser, plan.key, plan.vp, plan.theme, plan.zoom);
      } catch (err) {
        // Context unusable: record the failure on every cell it covers and move on.
        for (const cell of plan.cells) {
          runs.push({
            scenario: cell.scenario, view: 'observer', state: cell.state, theme: cell.theme,
            zoom: cell.zoom, viewport: { width: cell.viewport.width, height: cell.viewport.height },
            findings: [], error: `context creation failed: ${err && err.message ? err.message : err}`,
          });
        }
        console.log(`[TS-802] context ${plan.key}: FAILED — ${err && err.message ? err.message : err}`);
        continue;
      }
      liveContexts.push(ctx);
      for (const cell of plan.cells) {
        const run = await runCellScenario(ctx, cell, evidenceDir);
        runs.push(run);
        console.log(`[TS-802] ${cell.scenario}: ${run.findings.length} finding(s)${run.error ? ` — ERROR: ${run.error}` : ''}`);
      }
    }

    // Named scenario on (possibly shared) context for its viewport/theme/zoom.
    let namedCtx = null;
    for (const c of liveContexts) {
      if (c.name === namedKey) { namedCtx = c; break; }
    }
    if (!namedCtx) {
      try {
        namedCtx = await createContext(browser, namedKey, namedEntry.viewport, namedEntry.theme, namedEntry.zoom);
        liveContexts.push(namedCtx);
      } catch (err) {
        runs.push({
          scenario: namedEntry.name, view: 'observer', state: namedEntry.state, theme: namedEntry.theme,
          zoom: namedEntry.zoom, viewport: { width: namedEntry.viewport.width, height: namedEntry.viewport.height },
          findings: [], error: `context creation failed: ${err && err.message ? err.message : err}`,
        });
      }
    }
    if (namedCtx) {
      const run = {
        scenario: namedEntry.name, view: 'observer', state: namedEntry.state, theme: namedEntry.theme,
        zoom: namedEntry.zoom, viewport: { width: namedEntry.viewport.width, height: namedEntry.viewport.height, orientation: 'portrait' },
        findings: [],
      };
      try {
        await observerProvider.seed(namedCtx.page, namedEntry.state);
        await observerProvider.open(namedCtx.page);
        await observerProvider.settle(namedCtx.page, namedEntry.state, 'settled');
        const scopeList = (namedEntry.scope && (Array.isArray(namedEntry.scope) ? namedEntry.scope : [namedEntry.scope])) || [];
        run.findings = await withTimeout(
          collectFindings(namedCtx.page, scopeList.length ? { scope: scopeList } : {}),
          NAMED_TIMEOUT_MS, 'named scenario',
        );
      } catch (err) {
        run.error = (err && err.message) ? err.message : String(err);
      }
      if (run.findings.length) run.screenshot = await attachScreenshot(namedCtx.page, evidenceDir, namedEntry.name, false).catch(() => null);
      runs.push(run);
      console.log(`[TS-802] ${namedEntry.name}: ${run.findings.length} finding(s)${run.error ? ` — ERROR: ${run.error}` : ''}`);
    }

    // Interactions run at the first matrix cell (already seeded by the provider contract).
    if (cells.length === 0 || liveContexts.length === 0) {
      throw new Error('no live contexts to run interactions on');
    }
    const firstCell = cells[0];
    let interCtx = null;
    for (const c of liveContexts) {
      const key = `${firstCell.viewport.width}x${firstCell.viewport.height}/${firstCell.theme}/z${firstCell.zoom}`;
      if (c.name === key) { interCtx = c; break; }
    }
    if (interCtx) {
      // Re-seed the first cell state before each interaction in case the last
      // cell in that context used another state (provider contract: interaction
      // runs at the first matrix cell).
      for (const id of interactions) {
        const run = {
          scenario: `interaction:${id}`, view: 'observer', state: firstCell.state, theme: firstCell.theme,
          zoom: firstCell.zoom, viewport: { width: firstCell.viewport.width, height: firstCell.viewport.height, orientation: 'portrait' },
          findings: [],
        };
        try {
          await observerProvider.seed(interCtx.page, firstCell.state);
          await observerProvider.open(interCtx.page);
          await observerProvider.settle(interCtx.page, firstCell.state, 'settled');
          run.findings = await withTimeout(observerProvider.runInteraction(interCtx.page, id), INTERACTION_TIMEOUT_MS, `interaction ${id}`);
        } catch (err) {
          run.error = (err && err.message) ? err.message : String(err);
        }
        if (run.findings.length) run.screenshot = await attachScreenshot(interCtx.page, evidenceDir, run.scenario).catch(() => null);
        runs.push(run);
        console.log(`[TS-802] interaction:${id}: ${run.findings.length} finding(s)${run.error ? ` — ERROR: ${run.error}` : ''}`);
      }
    }
  } finally {
    for (const c of liveContexts) {
      try { await c.context.close(); } catch { /* best-effort */ }
    }
  }

  // Re-emit the mobile-fit report into this story's evidence directory.
  const meta = {
    story: 'TS-802',
    views: ['observer'],
    profile: 'quick',
    regression_guard: true,
    recorded_groups: recorded.length,
    recorded_source: 'docs/testing/runs/observer-mobile-fit/repro-report.json',
  };
  const report = buildReport(runs, meta);
  await writeReport(report, evidenceDir);
  console.log(`[TS-802] report: ${report.summary.scenarios} scenarios, ${report.summary.total} findings in ${report.summary.groups} group(s) -> ${evidenceDir}/mobile-fit-report.json`);

  // Verdict: any recorded group id reproducing = regression guard failure.
  const newGroupIds = new Set(report.groups.map((g) => g.id));
  const stillFailing = recorded.filter((e) => newGroupIds.has(e.group_id));
  const erroredRuns = runs.filter((r) => r.error);

  if (stillFailing.length > 0) {
    const sample = stillFailing.slice(0, 10)
      .map((e) => `${e.group_id} [${e.check}] ${e.selector} (${e.scenario})`)
      .join('; ');
    const more = stillFailing.length > 10 ? `; +${stillFailing.length - 10} more` : '';
    throw new Error(
      `${stillFailing.length}/${recorded.length} recorded Observer regression group(s) still produce findings ` +
      `(expected while the layout fixes are unapplied): ${sample}${more}`
    );
  }
  if (erroredRuns.length > 0) {
    throw new Error(`${erroredRuns.length} replay scenario(s) errored: ${erroredRuns.map((r) => `${r.scenario}: ${r.error}`).slice(0, 5).join('; ')}`);
  }
  console.log(`[TS-802] PASS: none of the ${recorded.length} recorded regression groups reproduce`);
});
