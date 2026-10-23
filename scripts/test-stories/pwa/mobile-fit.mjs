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
//     runInteraction(page, id) -> Promise,     // replay one interaction and
//                                             //   return extra findings
//   }
// Every missing key falls back to the default; a provider that fails to
// load or to export the expected shape is skipped WITHOUT an error and the
// view falls back to the defaults (this is how views work before their
// provider module exists).
//
// Browser execution lands in the next task, in runBrowser() below.

import path from 'node:path';
import { existsSync } from 'node:fs';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { buildMatrix } from './mobile-fit-matrix.mjs';

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
// Browser execution — STUB
//
// The real driver (per-cell open/seed/settle phases, interaction replay,
// finding collection via mobile-fit-lib.mjs, screenshots, and report
// assembly via mobile-fit-report.mjs) is added by the next task. It replaces
// this stub; the CLI in main() already hands it the resolved configuration.
// ---------------------------------------------------------------------------
export async function runBrowser(/* config */) {
  throw new Error('not implemented');
}

// CLI entry: parse + resolve, then either print the plan (--list) or run the
// browser. Returns the process exit code (main() itself never throws on the
// --list path; on the run path the runBrowser stub throws until the next
// task implements it).
export async function runCli(argv = process.argv.slice(2), { stdout = process.stdout } = {}) {
  const config = resolveConfig(parseArgs(argv));
  if (config.list) {
    const plan = await buildPlan(config);
    stdout.write(JSON.stringify(plan, null, 2) + '\n');
    return 0;
  }
  if (!config.out) config.out = await defaultOutDir();
  await runBrowser(config);
  return 0;
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
      if (msg === 'not implemented') {
        process.stderr.write('mobile-fit: browser execution is not implemented yet (use --list for the plan)\n');
      } else {
        process.stderr.write(`mobile-fit: ${msg}\n`);
      }
      process.exit(msg === 'not implemented' ? 1 : 2);
    });
}
