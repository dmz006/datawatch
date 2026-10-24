// scripts/test-stories/pwa/mobile-fit.mjs — scenario-harness CLI for the
// mobile-fit view runner. Decides WHAT to render (viewport, theme, zoom,
// state, interaction) for each view of the datawatch PWA, either listing the
// plan (pure, no browser) with --list or driving the browser execution.
//
// The matrix itself lives in mobile-fit-matrix.mjs (pure, no browser); the
// browser-side layout checks live in mobile-fit-lib.mjs; report assembly
// lives in mobile-fit-report.mjs. Each view contributes its own state and
// interaction list through a provider module (see contract below); views
// without a provider fall back to the plain 'default' state.
//
// Arguments (a --flag wins over the matching environment variable):
//   --view / --view=V            MOBILE_FIT_VIEW          comma list of
//       sessions, autonomous, alerts, observer, settings
//       (default: observer)
//   --profile / --profile=P      MOBILE_FIT_PROFILE       smoke | quick | full
//       (default: quick)
//   --exhaustive                 MOBILE_FIT_EXHAUSTIVE=1  exhaustive matrix
//       (profile must be full)
//   --out / --out=D              MOBILE_FIT_OUT           output directory
//       (default: $EVIDENCE_DIR/$CURRENT_STORY/mobile-fit, per lib.mjs env)
//   --report-only                MOBILE_FIT_REPORT_ONLY=1 rebuild the report
//       from the saved runs without rendering anything
//   --scope / --scope=S          MOBILE_FIT_SCOPE         CSS selector list
//       (comma separated) restricting the checks to the given scope
//   --list                       print the run plan as ONE JSON document on
//       stdout and exit 0, without launching a browser
//
// Exit codes: 0 on success (including --list and --report-only), including
// a browser run with no findings; 1 when a browser run produced findings or
// when the harness itself failed; 2 when no browser could be launched
// (prints 'SKIP: no browser') or on bad arguments (unknown flag, unknown
// view or profile, --exhaustive without --profile full, or a matrix
// state-list error).
//
// Plan document printed by --list:
//   { profile, views, viewports (unique {width,height,orientation} used),
//     themes (unique), zooms (unique), states, interactions,
//     named_scenarios, scenario_count, scenarios (names of all scenarios that
//     will be run: one per matrix cell, plus each named scenario name) }
//
// --- Provider contract -------------------------------------------------
// A view with a provider module (for the observer view:
// scripts/test-stories/pwa/mobile-fit-observer.mjs, exporting
// `observerProvider`) provides the per-view run plan. Only `observer` has a
// provider today; the others always use the fallback defaults below.
//
//   {
//     id: 'observer',                          // view identifier
//     states: [...],                           // states for the full sweeps
//     quickStates: [...],                      // states for the quick sweep
//     zoomStates: [...],                       // states for the zoom sweep
//     named: [                                 // named scenarios
//       { name, state, viewport, theme, zoom, scope },
//     ],
//     interactions: ['...', ...],              // interaction ids to replay
//     phases(state) -> string[],               // phase labels to settle per
//                                             //   state (default ['settled'])
//     seed(page, state) -> Promise,            // seed the view into its state
//     open(page) -> Promise,                   // open/navigate to the view
//     settle(page, state, phase) -> Promise,   // wait for a phase to be stable
//     runInteraction(page, id, { scope }) -> Promise,  // replay one interaction
//                                             //   and return extra findings;
//                                             //   scope narrows its layout checks
//   }
// Every missing key falls back to the default; a provider that fails to
// load or to export the expected shape is skipped WITHOUT an error and the
// view falls back to the defaults (this is how views work before their
// provider module exists).
//
// // Browser execution lives in runMobileFit(options) below. It returns the
// report object (see mobile-fit-report.mjs for its shape), so other stories
// can import it and drive the same harness programmatically.

import path from 'node:path';
import { existsSync } from 'node:fs';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { buildMatrix, cssViewportForZoom } from './mobile-fit-matrix.mjs';
import { collectFindings } from './mobile-fit-lib.mjs';
import { buildReport, writeReport } from './mobile-fit-report.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));

export const VIEWS = ['sessions', 'autonomous', 'alerts', 'observer', 'settings'];
export const PROFILES = ['smoke', 'quick', 'full'];

// View fallback when no provider module (or an unusable one) is present.
export const DEFAULT_VIEW_PLAN = {
  states: ['default'],
  quickStates: ['default'],
  zoomStates: ['default'],
  interactions: [],
  named: [],
};

const PROVIDER_FILES = {
  observer: { file: 'mobile-fit-observer.mjs', exportName: 'observerProvider' },
};

const TRUTHY_ENV = new Set(['1', 'true', 'yes', 'on']);

function envBool(name) {
  const v = process.env[name];
  return v != null && TRUTHY_ENV.has(String(v).trim().toLowerCase());
}

// Parse a comma-separated list argument into a trimmed, non-empty array.
export function splitList(value) {
  if (value == null) return [];
  return String(value)
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s !== '');
}

// Split argv into { flags, ... }. Supports `--flag value`, `--flag=value`,
// and bare boolean flags. Unknown or valueless flags throw with a clear
// message (the CLI exits non-zero on bad arguments).
export function parseArgs(argv) {
  const out = {
    view: process.env.MOBILE_FIT_VIEW ?? null,
    profile: process.env.MOBILE_FIT_PROFILE ?? null,
    out: process.env.MOBILE_FIT_OUT ?? null,
    scope: process.env.MOBILE_FIT_SCOPE ?? null,
    exhaustive: envBool('MOBILE_FIT_EXHAUSTIVE'),
    reportOnly: envBool('MOBILE_FIT_REPORT_ONLY'),
    list: false,
  };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (typeof arg !== 'string' || !arg.startsWith('--')) {
      throw new Error(`unexpected argument: ${arg} (want --flag or --flag=value)`);
    }
    let name = arg;
    let inline = null;
    const eq = arg.indexOf('=');
    if (eq !== -1) {
      name = arg.slice(0, eq);
      inline = arg.slice(eq + 1);
    }
    const takeValue = () => {
      if (inline != null) return inline;
      i += 1;
      if (i >= argv.length) throw new Error(`missing value for ${name}`);
      return argv[i];
    };
    switch (name) {
      case '--view':
        out.view = takeValue();
        break;
      case '--profile':
        out.profile = takeValue();
        break;
      case '--out':
        out.out = takeValue();
        break;
      case '--scope':
        out.scope = takeValue();
        break;
      case '--exhaustive':
        out.exhaustive = true;
        break;
      case '--report-only':
        out.reportOnly = true;
        break;
      case '--list':
        out.list = true;
        break;
      default:
        throw new Error(`unknown argument: ${name} (known: --view --profile --out --scope --exhaustive --report-only --list)`);
    }
  }
  return out;
}

// Turn parsed args into the runner configuration, applying defaults and
// validating the view/profile lists. Flags already win over env vars in
// parseArgs; this layer only normalises.
export function resolveConfig(parsed) {
  const views = splitList(parsed.view);
  if (views.length === 0) {
    views.push('observer');
  }
  for (const v of views) {
    if (!VIEWS.includes(v)) {
      throw new Error(`unknown view "${v}" (want a comma list of: ${VIEWS.join(', ')})`);
    }
  }
  const profile = parsed.profile || 'quick';
  if (!PROFILES.includes(profile)) {
    throw new Error(`unknown profile "${profile}" (want one of: ${PROFILES.join(', ')})`);
  }
  return {
    views,
    profile,
    exhaustive: parsed.exhaustive === true,
    // out defaults to $EVIDENCE_DIR/$CURRENT_STORY/mobile-fit, resolved
    // from lib.mjs's env; resolved lazily in runCli() so --list does not
    // need the Playwright dependency chain.
    out: (typeof parsed.out === 'string' && parsed.out.trim())
      ? parsed.out.trim()
      : null,
    reportOnly: parsed.reportOnly === true,
    scope: splitList(parsed.scope),
    list: parsed.list === true,
  };
}

// Load and validate the provider for a view (if any). Returns the provider
// object, or null when the view has no (usable) provider. Never throws: a
// missing file or a malformed provider simply yields no provider and the
// view falls back to the defaults.
async function loadProvider(view) {
  const entry = PROVIDER_FILES[view];
  if (!entry) return null;
  const file = path.join(here, entry.file);
  if (!existsSync(file)) return null;
  let mod;
  try {
    mod = await import(pathToFileURL(file).href);
  } catch {
    return null;
  }
  const provider = mod && mod[entry.exportName];
  if (!provider || typeof provider !== 'object') return null;
  return provider;
}

// Default output directory comes from lib.mjs's env object (EVIDENCE_DIR /
// CURRENT_STORY / mobile-fit). Imported lazily (dynamic import) so the pure
// --list path never pays for lib.mjs's Playwright chain; if lib.mjs cannot
// load at all (e.g. dependencies not installed in this sandbox), fall back
// to reading the same two environment variables directly, mirroring
// lib.mjs's own defaults.
async function defaultOutDir() {
  let evidenceDir;
  let story;
  try {
    const { env } = await import('./lib.mjs');
    evidenceDir = env.evidenceDir;
    story = env.story;
  } catch {
    evidenceDir = process.env.EVIDENCE_DIR && process.env.EVIDENCE_DIR.trim()
      ? process.env.EVIDENCE_DIR.trim()
      : '/tmp/dw-pwa-evidence';
    story = process.env.CURRENT_STORY || 'TS-000';
  }
  return path.join(evidenceDir, story, 'mobile-fit');
}

// The run plan for one view: the provider's lists when it exists, the
// defaults otherwise. `named` entries are kept verbatim; `states` /
// `quickStates` / `zoomStates` fall back to the provider's full `states`
// list (then to the default single state).
async function viewPlan(view) {
  const provider = await loadProvider(view);
  if (!provider) return DEFAULT_VIEW_PLAN;
  const stateList = (v, fb) => (Array.isArray(v) && v.length && v.every((s) => typeof s === 'string' && s.trim()))
    ? v
    : fb;
  const states = stateList(provider.states, DEFAULT_VIEW_PLAN.states);
  return {
    provider,
    states,
    quickStates: stateList(provider.quickStates, states),
    zoomStates: stateList(provider.zoomStates, states),
    interactions: Array.isArray(provider.interactions)
      ? provider.interactions.filter((s) => typeof s === 'string' && s.trim())
      : [],
    named: Array.isArray(provider.named) ? provider.named : [],
  };
}

// Build the --list plan document for all requested views. Pure (aside from
// the provider dynamic import, which is data only).
export async function buildPlan(config) {
  const viewports = [];
  const vpSeen = new Set();
  const themes = [];
  const themeSeen = new Set();
  const zooms = [];
  const zoomSeen = new Set();
  const states = [];
  const stateSeen = new Set();
  const interactions = [];
  const interactionSeen = new Set();
  const namedScenarios = [];
  const namedSeen = new Set();
  const scenarios = [];
  const scenarioSeen = new Set();
  const pushUnique = (arr, seen, value) => {
    const k = typeof value === 'object' ? JSON.stringify(value) : String(value);
    if (!seen.has(k)) { seen.add(k); arr.push(value); }
  };

  for (const view of config.views) {
    const plan = await viewPlan(view);
    const cells = buildMatrix({
      profile: config.profile,
      states: plan.states,
      quickStates: plan.quickStates,
      zoomStates: plan.zoomStates,
      exhaustive: config.exhaustive,
    });
    for (const c of cells) {
      pushUnique(viewports, vpSeen, c.viewport);
      pushUnique(themes, themeSeen, c.theme);
      pushUnique(zooms, zoomSeen, c.zoom);
      pushUnique(states, stateSeen, c.state);
      pushUnique(scenarios, scenarioSeen, c.scenario);
    }
    for (const id of plan.interactions) pushUnique(interactions, interactionSeen, id);
    for (const n of plan.named) {
      pushUnique(namedScenarios, namedSeen, n);
      if (n && n.name) pushUnique(scenarios, scenarioSeen, n.name);
    }
  }

  return {
    profile: config.profile,
    views: config.views,
    viewports,
    themes,
    zooms,
    states,
    interactions,
    named_scenarios: namedScenarios,
    scenario_count: scenarios.length,
    scenarios,
  };
}

// ---------------------------------------------------------------------------
// Browser execution
// ---------------------------------------------------------------------------
// runMobileFit(options) drives the real run and returns the report object
// (see mobile-fit-report.mjs for its shape), so other stories can import it
// and drive the same harness programmatically. options mirror the CLI flags:
// views (id, comma list or array), profile, exhaustive, out, reportOnly,
// scope.
//
// Behaviour:
//   * one browser; one context per distinct (viewport, theme, zoom). The
//     context gets ignoreHTTPSErrors, the viewport + deviceScaleFactor from
//     cssViewportForZoom(), and an init script that pre-seeds
//     localStorage's 'cs_theme' and 'cs_token' so the very first paint is in
//     the right theme with a valid token; connectToPWA runs once per context
//     and the page is then reused for every state in that combination;
//   * per state: provider.seed(page, state) (a no-op without a provider
//     module), the view is re-opened so it re-renders (bottom nav button if
//     visible, otherwise window.navigate(id) — no full page reload), it is
//     waited on until settled, then collectFindings() runs;
//   * provider phases: each state runs provider.phases(state) (default
//     ['settled']); phases other than 'settled' append '#<phase>' to the
//     scenario name, and provider.settle() is the wait for that phase;
//   * provider interactions, when present, run ONCE per profile at the first
//     matrix cell as 'interaction:<id>' scenarios; when a scope option is set
//     (flag or MOBILE_FIT_SCOPE) they still run, with their layout checks
//     narrowed to that scope;
//   * provider named scenarios run in every profile, in addition to the
//     matrix cells, each with its own viewport/theme/zoom/state/scope;
//     an explicit scope option overrides the named scenario's own scope;
//   * a view that fails to open produces a run with an error string and one
//     finding of check 'open-failed' — the harness does not abort;
//   * a run with findings gets a full-page screenshot at
//     <out>/screenshots/<sanitised-scenario>.png, the path (relative to
//     <out>) attached to the run;
//   * always writes mobile-fit-report.json + .txt under <out>, then prints
//     one summary line and the first 20 group lines.

// Loading UI that says "not settled yet": spinners, skeleton blocks, inline
// loading placeholders. The default settle waits for these to go away, up to
// SETTLE_TIMEOUT_MS, after which the view counts as settled anyway.
const LOADING_PLACEHOLDERS = '.spinner, .skeleton, .skeleton-line, .skeleton-card, .integ-loading, .loading-eye-block';
const SETTLE_TIMEOUT_MS = 10000;

function messageOf(err) {
  return err && err.message ? err.message : String(err);
}

function toList(v) {
  if (v == null) return [];
  const arr = Array.isArray(v) ? v : splitList(v);
  return arr
    .filter((x) => typeof x === 'string' && x.trim() !== '')
    .map((x) => x.trim());
}

function sanitiseScenario(name) {
  let s = String(name).replace(/[^A-Za-z0-9._-]+/g, '-').replace(/^-+|-+$/g, '');
  if (!s) s = 'scenario';
  return s.slice(0, 80);
}

function scopeOpts(scopeList) {
  return scopeList && scopeList.length > 0 ? { scope: scopeList } : {};
}

function phasesFor(provider, state) {
  if (provider && typeof provider.phases === 'function') {
    try {
      const p = provider.phases(state);
      if (Array.isArray(p) && p.length > 0 && p.every((x) => typeof x === 'string' && x.trim() !== '')) return p;
    } catch { /* provider error - default phases */ }
  }
  return ['settled'];
}

function openFailedFinding(view, err) {
  return {
    check: 'open-failed',
    selector: `[data-view="${view}"]`,
    detail: `could not open view '${view}': ${messageOf(err)}`,
  };
}

async function defaultSettle(page) {
  try {
    await page.waitForFunction(
      (sel) => Array.from(document.querySelectorAll(sel)).every((el) => el.offsetWidth === 0 || el.offsetHeight === 0),
      LOADING_PLACEHOLDERS,
      { timeout: SETTLE_TIMEOUT_MS, polling: 250 },
    );
  } catch { /* settle cap expired - count the view as settled and go on */ }
}

async function openView(page, view, lib) {
  const btn = page.locator(`[data-view="${view}"]`).first();
  if (await btn.isVisible().catch(() => false)) {
    await lib.navigateTo(page, view); // click + wait for the view to be active
    return;
  }
  await page.evaluate((v) => {
    if (typeof window.navigate !== 'function') throw new Error('window.navigate is not available');
    window.navigate(v);
  }, view);
  await page.waitForTimeout(250); // let the first render pass finish
}

async function attachScreenshot(page, outDir, run) {
  const { mkdir } = await import('node:fs/promises');
  const rel = `screenshots/${sanitiseScenario(run.scenario)}.png`;
  const abs = path.join(outDir, rel);
  await mkdir(path.dirname(abs), { recursive: true });
  await page.screenshot({ path: abs, fullPage: true, animations: 'disabled' });
  run.screenshot = rel;
}

// One context per distinct (viewport, theme, zoom), shared across all views
// and states that land on the same combination.
function createContextFactory(browser, lib, consoleErrors) {
  const contexts = new Map();
  const keyOf = (vp, theme, zoom) =>
    `${vp.width}x${vp.height}x${vp.orientation || 'portrait'}/${theme}/z${zoom}`;
  return async function getContext(vp, theme, zoom) {
    const key = keyOf(vp, theme, zoom);
    if (contexts.has(key)) return contexts.get(key);
    const css = cssViewportForZoom(vp, zoom);
    const context = await browser.newContext({
      ignoreHTTPSErrors: true,
      viewport: { width: css.width, height: css.height },
      deviceScaleFactor: css.deviceScaleFactor,
    });
    await context.addInitScript((args) => {
      try {
        localStorage.setItem('cs_theme', args[0]);
        localStorage.setItem('cs_token', args[1]);
      } catch { /* storage not usable before the page has an origin */ }
    }, [theme, lib.env.token]);
    const page = await context.newPage();
    page.on('console', (msg) => {
      if (msg.type() === 'error') {
        try { consoleErrors.push(msg.text()); } catch { /* ignore */ }
      }
    });
    page.on('pageerror', (err) => { consoleErrors.push(`pageerror: ${err.message}`); });
    contexts.set(key, { context, page });
    try {
      await lib.connectToPWA(page);
    } catch (err) {
      contexts.delete(key);
      await context.close().catch(() => {});
      throw err;
    }
    return contexts.get(key);
  };
}

async function runCell(view, provider, cell, dep) {
  const { scenario, viewport, theme, zoom, state } = cell;
  const base = { scenario, view, state, theme, zoom, viewport };
  let page;
  try {
    page = (await dep.getContext(viewport, theme, zoom)).page;
  } catch (err) {
    base.error = `context setup failed: ${messageOf(err)}`;
    base.findings = [openFailedFinding(view, err)];
    return [base];
  }
  try {
    if (provider && typeof provider.seed === 'function') await provider.seed(page, state);
  } catch (err) {
    base.error = `seed failed: ${messageOf(err)}`;
    base.findings = [openFailedFinding(view, err)];
    return [base];
  }
  try {
    if (provider && typeof provider.open === 'function') await provider.open(page);
    else await dep.open(page, view);
  } catch (err) {
    base.error = messageOf(err);
    base.findings = [openFailedFinding(view, err)];
    return [base];
  }
  const runs = [];
  for (const phase of phasesFor(provider, state)) {
    const run = {
      ...base,
      scenario: phase === 'settled' ? scenario : `${scenario}#${phase}`,
      findings: [],
    };
    try {
      if (provider && typeof provider.settle === 'function') await provider.settle(page, state, phase);
      else await dep.settle(page);
      run.findings = await collectFindings(page, scopeOpts(dep.scopeList));
      if (run.findings.length > 0) await dep.screenshot(page, run).catch(() => {});
    } catch (err) {
      run.error = messageOf(err);
      run.findings = [];
    }
    runs.push(run);
  }
  return runs;
}

function shouldRunInteractions(config, plan) {
  return plan.interactions.length > 0
    && plan.provider
    && typeof plan.provider.runInteraction === 'function';
}

async function runInteractions(view, plan, cell, dep) {
  const provider = plan.provider;
  const out = [];
  for (const id of plan.interactions) {
    const run = {
      scenario: `interaction:${id}`,
      view,
      state: cell.state,
      theme: cell.theme,
      zoom: cell.zoom,
      viewport: cell.viewport,
      findings: [],
    };
    let page;
    try {
      page = (await dep.getContext(cell.viewport, cell.theme, cell.zoom)).page;
    } catch (err) {
      run.error = `context setup failed: ${messageOf(err)}`;
      out.push(run);
      continue;
    }
    try {
      const extra = await provider.runInteraction(page, id, { scope: dep.scopeList });
      run.findings = Array.isArray(extra) ? extra.filter((f) => f && typeof f === 'object') : [];
      if (run.findings.length > 0) await dep.screenshot(page, run).catch(() => {});
    } catch (err) {
      run.error = messageOf(err);
      run.findings = [];
    }
    out.push(run);
  }
  return out;
}

const FALLBACK_VIEWPORT = { width: 390, height: 844, orientation: 'portrait' };

async function runNamedScenario(view, provider, entry, dep) {
  if (!entry || typeof entry !== 'object') return null;
  const name = typeof entry.name === 'string' ? entry.name.trim() : '';
  if (!name) return null;
  const state = typeof entry.state === 'string' && entry.state.trim() ? entry.state.trim() : 'default';
  const vp = entry.viewport && Number.isFinite(entry.viewport.width) && Number.isFinite(entry.viewport.height) && entry.viewport.width > 0
    ? { width: entry.viewport.width, height: entry.viewport.height, orientation: entry.viewport.orientation || 'portrait' }
    : FALLBACK_VIEWPORT;
  const theme = entry.theme === 'light' || entry.theme === 'dark' ? entry.theme : 'dark';
  const zoom = Number.isFinite(Number(entry.zoom)) && Number(entry.zoom) > 0 ? Number(entry.zoom) : 100;
  const scopeList = dep.scopeList.length > 0 ? dep.scopeList : toList(entry.scope);
  const run = { scenario: name, view, state, theme, zoom, viewport: vp, findings: [] };
  let page;
  try {
    page = (await dep.getContext(vp, theme, zoom)).page;
  } catch (err) {
    run.error = `context setup failed: ${messageOf(err)}`;
    run.findings = [openFailedFinding(view, err)];
    return run;
  }
  try {
    if (provider && typeof provider.seed === 'function') await provider.seed(page, state);
    if (provider && typeof provider.open === 'function') await provider.open(page);
    else await dep.open(page, view);
    if (provider && typeof provider.settle === 'function') await provider.settle(page, state, 'settled');
    else await dep.settle(page);
    run.findings = await collectFindings(page, scopeOpts(scopeList));
    if (run.findings.length > 0) await dep.screenshot(page, run).catch(() => {});
  } catch (err) {
    run.error = messageOf(err);
    if (run.findings.length === 0) run.findings = [openFailedFinding(view, err)];
  }
  return run;
}

async function runView(config, view, dep) {
  const plan = await viewPlan(view);
  const provider = plan.provider || null;
  const cells = buildMatrix({
    profile: config.profile,
    states: plan.states,
    quickStates: plan.quickStates,
    zoomStates: plan.zoomStates,
    exhaustive: config.exhaustive,
  });
  const out = [];
  let interactionsRun = false;
  for (const cell of cells) {
    out.push(...await runCell(view, provider, cell, dep));
    if (!interactionsRun) {
      interactionsRun = true;
      if (shouldRunInteractions(config, plan)) {
        out.push(...await runInteractions(view, plan, cell, dep));
      }
    }
  }
  for (const ns of plan.named) {
    const namedRun = await runNamedScenario(view, provider, ns, dep);
    if (namedRun) out.push(namedRun);
  }
  return out;
}

async function collectBrowserRuns(config) {
  const lib = await import('./lib.mjs');
  let browser;
  try {
    browser = await lib.launchBrowser();
  } catch (err) {
    const e = new Error(`no browser could be launched: ${messageOf(err)}`);
    e.code = 'NO_BROWSER';
    throw e;
  }
  const consoleErrors = [];
  const runs = [];
  const dep = {
    getContext: createContextFactory(browser, lib, consoleErrors),
    open: (page, view) => openView(page, view, lib),
    settle: (page) => defaultSettle(page),
    screenshot: (page, run) => attachScreenshot(page, config.out, run),
    scopeList: config.scope,
  };
  try {
    for (const view of config.views) {
      runs.push(...await runView(config, view, dep));
    }
  } finally {
    if (consoleErrors.length > 0) {
      const body = consoleErrors.join('\n') + '\n';
      const { mkdir, writeFile } = await import('node:fs/promises');
      await mkdir(config.out, { recursive: true }).catch(() => {});
      await writeFile(path.join(config.out, 'console-errors.txt'), body).catch(() => {});
      await lib.saveLog('console-errors', body).catch(() => {});
    }
    await browser.close().catch(() => {});
  }
  return runs;
}

// --report-only rebuilds the report FROM SAVED RUNS WITHOUT RENDERING
// ANYTHING under <out>: the previously written mobile-fit-report.json if it
// exists, otherwise the planned scenarios (one run each, no findings).
async function collectSavedRuns(config) {
  const savedFile = path.join(config.out, 'mobile-fit-report.json');
  if (existsSync(savedFile)) {
    try {
      const { readFile } = await import('node:fs/promises');
      const saved = JSON.parse(await readFile(savedFile, 'utf8'));
      if (saved && Array.isArray(saved.runs) && saved.runs.length > 0) return saved.runs;
    } catch { /* unreadable saved report - fall back to the plan */ }
  }
  const runs = [];
  for (const view of config.views) {
    const plan = await viewPlan(view);
    const cells = buildMatrix({
      profile: config.profile,
      states: plan.states,
      quickStates: plan.quickStates,
      zoomStates: plan.zoomStates,
      exhaustive: config.exhaustive,
    });
    for (const cell of cells) {
      runs.push({ scenario: cell.scenario, view, state: cell.state, theme: cell.theme, zoom: cell.zoom, viewport: cell.viewport, findings: [] });
    }
    if (plan.interactions.length > 0) {
      const first = cells.length > 0 ? cells[0] : null;
      for (const id of plan.interactions) {
        runs.push({
          scenario: `interaction:${id}`,
          view,
          state: first ? first.state : 'default',
          theme: first ? first.theme : 'dark',
          zoom: first ? first.zoom : 100,
          viewport: first ? first.viewport : FALLBACK_VIEWPORT,
          findings: [],
        });
      }
    }
    for (const ns of plan.named) {
      const name = ns && typeof ns.name === 'string' ? ns.name.trim() : '';
      if (!name) continue;
      runs.push({
        scenario: name,
        view,
        state: ns && typeof ns.state === 'string' && ns.state ? ns.state : 'default',
        theme: ns && (ns.theme === 'light' || ns.theme === 'dark') ? ns.theme : 'dark',
        zoom: ns && Number.isFinite(Number(ns.zoom)) && Number(ns.zoom) > 0 ? Number(ns.zoom) : 100,
        viewport: ns && ns.viewport && Number.isFinite(ns.viewport.width) && Number.isFinite(ns.viewport.height) ? ns.viewport : FALLBACK_VIEWPORT,
        findings: [],
      });
    }
  }
  return runs;
}

function printSummary(report, jsonPath) {
  const s = report.summary;
  console.log(`mobile-fit: ${s.scenarios} scenarios, ${s.total} findings in ${s.groups} groups; report: ${jsonPath}`);
  for (const g of report.groups.slice(0, 20)) {
    console.log(`  ${g.id} [${g.check}] ${g.selector} - ${g.count} ${g.count === 1 ? 'finding' : 'findings'}`);
  }
}

export async function runMobileFit(options = {}) {
  let config;
  try {
    config = resolveConfig({
      view: options.views,
      profile: options.profile,
      exhaustive: options.exhaustive === true,
      out: options.out,
      reportOnly: options.reportOnly === true,
      scope: options.scope,
    });
  } catch (err) {
    err.code = 'BAD_ARGS';
    throw err;
  }
  if (!config.out) config.out = await defaultOutDir();
  const meta = {
    views: config.views,
    profile: config.profile,
    scope: config.scope,
    report_only: config.reportOnly,
    exhaustive: config.exhaustive,
  };
  const runs = config.reportOnly ? await collectSavedRuns(config) : await collectBrowserRuns(config);
  const report = buildReport(runs, meta);
  const { json } = await writeReport(report, config.out);
  printSummary(report, json);
  return report;
}

// CLI entry: parse + resolve, then either print the plan (--list), rebuild
// the report (--report-only) or run the browser. Returns the process exit
// code; main() maps thrown errors onto exit codes (2 for bad arguments and
// 'no browser', 1 for harness failures).
export async function runCli(argv = process.argv.slice(2), { stdout = process.stdout } = {}) {
  let config;
  try {
    config = resolveConfig(parseArgs(argv));
  } catch (err) {
    err.code = 'BAD_ARGS';
    throw err;
  }
  if (config.list) {
    const plan = await buildPlan(config);
    const text = JSON.stringify(plan, null, 2) + '\n';
    stdout.write(text);
    return 0;
  }
  if (!config.out) config.out = await defaultOutDir();
  const report = await runMobileFit({
    views: config.views,
    profile: config.profile,
    exhaustive: config.exhaustive,
    out: config.out,
    reportOnly: config.reportOnly,
    scope: config.scope,
  });
  // --report-only always exits 0 (it just re-renders a saved report, even
  // one with findings); otherwise 1 when findings exist, 0 when none.
  if (config.reportOnly) return 0;
  return report.summary.total > 0 ? 1 : 0;
}

function isMain() {
  if (!process.argv[1]) return false;
  try {
    return import.meta.url === pathToFileURL(process.argv[1]).href;
  } catch {
    return false;
  }
}

if (isMain()) {
  runCli()
    .then((code) => process.exit(code))
    .catch((err) => {
      const msg = err && err.message ? err.message : String(err);
      if (err && err.code === 'NO_BROWSER') {
        process.stderr.write('SKIP: no browser\n');
        process.exit(2);
      }
      if (err && err.code === 'BAD_ARGS') {
        process.stderr.write(`mobile-fit: ${msg}\n`);
        process.exit(2);
      }
      process.stderr.write(`mobile-fit: ${msg}\n`);
      process.exit(1);
    });
}
