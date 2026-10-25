// PWA mobile-fit provider for the Observer / System Statistics view.
//
// The runner in mobile-fit.mjs iterates the matrix for this view and, for each
// cell:
//   1. installs the state's seeded API surface (mobile-fit-seeds.mjs)
//   2. opens the view
//   3. walks the cell's phases (loading / settled / ...)
//   4. collects layout findings (mobile-fit-lib.mjs)
//
// This module owns only the view-specific bits: which states exist, and how to
// open/settle the view for each. Conventions: ES modules, no new npm deps,
// repo-relative imports only, no tracker IDs in identifiers.

import { installSeed, clearSeed, STATE_NAMES } from './mobile-fit-seeds.mjs';
import { collectFindings } from './mobile-fit-lib.mjs';

// A state whose fake network is deliberately slow: lets the checks observe the
// loading phase (placeholders / spinners) before the settled layout.
const SLOW_STATE = 'stats-slow';

// How long settle() may wait for the view to stop showing loading placeholders
// before it has to move on.
const SETTLE_CAP_MS = 12000;
const SLOW_SETTLE_CAP_MS = 15000;

// Loading placeholders the Observer view uses while panels fetch.
const LOADING_PLACEHOLDERS =
  '.spinner, .skeleton, .skeleton-line, .skeleton-card, .integ-loading, .loading-eye-block';

// The per-system stat cards live in a grid inside the stats section.
const gridSelectorList = () => ['#perSystemGrid'];

// Interaction states: every interaction (below) runs at the first matrix cell
// with this many-peer seed installed, per the runner contract.
const INTERACTION_STATE = 'ten-peers';

// ten-peers => 10 peer cards + 1 local card in #perSystemGrid.
const EXPECTED_GRID_CARDS = 11;

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// A single interaction failure (vs a layout finding — those use the standard
// shape and carry a rect; interaction findings explain what did not work).
const interactionFinding = (selector, detail) => ({ check: 'interaction', selector, detail });

// Seed the interaction state, drive the view to Observer, and settle once so
// the initial paint exists before any toggling/refreshing.
async function seedAndOpen(page) {
  await installSeed(page, INTERACTION_STATE);
  await observerProvider.open(page);
  await observerProvider.settle(page, INTERACTION_STATE, 'settled');
}

// Click the first element whose inline onclick handler contains `part`.
// The PWA wires its toggles/refresh buttons through inline handlers, so
// attribute matching avoids coupling to a specific CSS class name.
async function clickOnclickMatch(page, part) {
  return page.evaluate((frag) => {
    const el = Array.from(document.querySelectorAll('[onclick]'))
      .find((e) => (e.getAttribute('onclick') || '').includes(frag));
    if (!el) return false;
    el.click();
    return true;
  }, part);
}

// Wait (bounded) until `#id` is visible / hidden per getComputedStyle.
async function waitBodyState(page, id, wantVisible) {
  await page.waitForFunction(
    ([eid, hidden]) => {
      const el = document.getElementById(eid);
      if (!el) return false;
      const isHidden = window.getComputedStyle(el).display === 'none';
      return hidden ? isHidden : !isHidden;
    },
    [id, !wantVisible],
    { timeout: 3000, polling: 50 },
  );
}

// Run collectFindings once (defensively) and append to the accumulator.
async function collectInto(page, findings, scopeOpts = {}) {
  const found = await collectFindings(page, scopeOpts).catch(() => []);
  findings.push(...found);
}

// One collapse/expand verification: wait for the body to actually switch, then
// checkpoint. If the display never switches, record an interaction finding.
async function verifySwap(page, findings, id, wantVisible, label, scopeOpts = {}) {
  try {
    await waitBodyState(page, id, wantVisible);
    await collectInto(page, findings, scopeOpts);
  } catch {
    findings.push(interactionFinding(
      `#${id}`,
      `${label}: expected #${id} to become ${wantVisible ? 'visible' : 'hidden'} (display:none) after its header click, but it did not switch`,
    ));
  }
}

// interaction: section-collapse-expand
// Cycle the System Statistics header 3x (each toggle must visibly collapse and
// bring the section back), then expand/collapse both inner collapsibles — the
// MCP channel bridge and the channel bridge diagnostics.
async function runSectionCollapseExpand(page, scopeOpts = {}) {
  const findings = [];
  await seedAndOpen(page);

  const STATS = 'settings-sec-stats';
  const STATS_HEADER = "toggleSettingsSection('stats')";

  // Normalize to expanded before cycling.
  const hidden = await page.evaluate((id) => {
    const el = document.getElementById(id);
    return el ? window.getComputedStyle(el).display === 'none' : null;
  }, STATS).catch(() => null);
  if (hidden === true) {
    await clickOnclickMatch(page, STATS_HEADER).catch(() => false);
    await waitBodyState(page, STATS, true).catch(() => {});
  }

  for (let cycle = 1; cycle <= 3; cycle += 1) {
    const collapsedOk = await clickOnclickMatch(page, STATS_HEADER).catch(() => false);
    if (!collapsedOk) {
      findings.push(interactionFinding(`[onclick*="stats"]`, `cycle ${cycle}: System Statistics section header (toggleSettingsSection) not found in the Observer view`));
      continue;
    }
    await verifySwap(page, findings, STATS, false, `stats collapse ${cycle}/3`, scopeOpts);
    const expandedOk = await clickOnclickMatch(page, STATS_HEADER).catch(() => false);
    if (!expandedOk) {
      findings.push(interactionFinding(`[onclick*="stats"]`, `cycle ${cycle}: System Statistics section header (toggleSettingsSection) not found in the Observer view`));
      continue;
    }
    await verifySwap(page, findings, STATS, true, `stats expand ${cycle}/3`, scopeOpts);
  }

  const innerSections = [
    ['channelBridgeBody', 'MCP channel bridge section'],
    ['channelDiagBody', 'channel bridge diagnostics section'],
  ];
  for (const [id, label] of innerSections) {
    const expandedOk = await clickOnclickMatch(page, id).catch(() => false);
    if (expandedOk) await verifySwap(page, findings, id, true, `expand ${label}`, scopeOpts);
    else findings.push(interactionFinding(`[onclick*="${id}"]`, `${label} header not found in the Observer view`));

    const collapsedOk = await clickOnclickMatch(page, id).catch(() => false);
    if (collapsedOk) await verifySwap(page, findings, id, false, `collapse ${label}`, scopeOpts);
    else findings.push(interactionFinding(`[onclick*="${id}"]`, `${label} header not found in the Observer view`));
  }
  return findings;
}

// interaction: refresh
// Drive every refresh path into the stats section and, after each, verify the
// view is settled (no spinner/skeleton in #statsPanel), the per-system grid
// shows the full ten-peers card count, and layout has not regressed.
async function refreshStep(page, findings, label, act, extraSelector, scopeOpts = {}) {
  let fired = true;
  try {
    await act();
  } catch (e) {
    fired = false;
    findings.push(interactionFinding('#statsPanel', `${label}: refresh path failed to fire: ${e && e.message ? e.message : String(e)}`));
  }
  if (!fired) return;

  // Let the (seeded, fast) async loads resolve, then verify deterministically.
  let settled = true;
  try {
    await page.waitForFunction(([ph, extra]) => {
      const panel = document.getElementById('statsPanel');
      const grid = document.getElementById('perSystemGrid');
      if (!panel || !grid) return false;
      if (panel.querySelector(ph) || grid.querySelector(ph)) return false;
      if (grid.children.length === 0) return false;
      if (extra) {
        const extr = document.getElementById(extra);
        return !(extr && extr.querySelector(ph));
      }
      return true;
    }, [LOADING_PLACEHOLDERS, extraSelector || null], { timeout: 8000, polling: 100 });
  } catch { settled = false; }
  if (!settled) {
    findings.push(interactionFinding('#statsPanel', `${label}: view still showed loading placeholders or an empty grid after refresh`));
    return;
  }

  const cards = await page.evaluate(() => {
    const grid = document.getElementById('perSystemGrid');
    if (!grid) return -1;
    const inner = grid.children.length === 1 ? grid.firstElementChild : null;
    return inner ? inner.children.length : grid.children.length;
  }).catch(() => -1);
  if (cards !== EXPECTED_GRID_CARDS) {
    findings.push(interactionFinding(
      '#perSystemGrid',
      `${label}: expected ${EXPECTED_GRID_CARDS} cards (10 peers + local) after refresh, saw ${cards}`,
    ));
    return;
  }

  await collectInto(page, findings, scopeOpts);
}

async function runRefresh(page, scopeOpts = {}) {
  const findings = [];
  await seedAndOpen(page);

  const headerPart = "toggleSettingsSection('stats')";
  const headerClick = async () => {
    const ok = await clickOnclickMatch(page, headerPart);
    if (!ok) throw new Error(`System Statistics section header (toggleSettingsSection) not found in the Observer view`);
  };

  // Path 1: the section header itself (collapse/expand pass through it).
  await refreshStep(page, findings, 'header toggle', headerClick, undefined, scopeOpts);
  // Restore expanded state for the remaining paths.
  const statsHidden = await page.evaluate((id) => {
    const el = document.getElementById(id);
    return el ? window.getComputedStyle(el).display === 'none' : false;
  }, 'settings-sec-stats').catch(() => false);
  if (statsHidden) {
    await clickOnclickMatch(page, headerPart).catch(() => false);
    await waitBodyState(page, 'settings-sec-stats', true).catch(() => {});
  }

  // Path 2: the stats-grid refresh (window.loadSystemStatsGrid).
  await refreshStep(page, findings, 'stats grid refresh', () => page.evaluate(() => {
    if (typeof window.loadSystemStatsGrid !== 'function') throw new Error('window.loadSystemStatsGrid is not a function');
    window.loadSystemStatsGrid();
  }), undefined, scopeOpts);

  // Path 3: the stats panel refresh (window.loadStatsPanel).
  await refreshStep(page, findings, 'stats panel refresh', () => page.evaluate(() => {
    if (typeof window.loadStatsPanel !== 'function') throw new Error('window.loadStatsPanel is not a function');
    window.loadStatsPanel();
  }), undefined, scopeOpts);

  // Path 4: the peers card refresh (window.renderObserverPeersCard).
  await refreshStep(page, findings, 'peers card refresh', () => page.evaluate(() => {
    if (typeof window.renderObserverPeersCard !== 'function') throw new Error('window.renderObserverPeersCard is not a function');
    window.renderObserverPeersCard();
  }), 'observerPeersPanel', scopeOpts);

  // Path 5: the small refresh button in the channel bridge diagnostics block.
  const diagPart = 'loadChannelDiagnostics()';
  await refreshStep(page, findings, 'channel diagnostics refresh button', async () => {
    const ok = await clickOnclickMatch(page, diagPart);
    if (!ok) throw new Error(`channel bridge diagnostics refresh button (loadChannelDiagnostics) not found`);
    // The diagnostics body renders a placeholder while it loads; wait for it to clear.
    await page.waitForFunction((ph) => {
      const el = document.getElementById('channelDiagBody');
      return !el || !el.querySelector(ph);
    }, LOADING_PLACEHOLDERS, { timeout: 8000, polling: 100 });
  }, undefined, scopeOpts);

  // Path 6: one 8-second auto tick (the live peers / stats grid pollers
  // refresh on an 8s cycle while Observer is active). Wait just past two
  // tick boundaries, then verify the settled layout held.
  await sleep(17000);
  await refreshStep(page, findings, '8s auto tick', async () => {
    const panel = await page.evaluate(() => document.getElementById('statsPanel') !== null);
    if (!panel) throw new Error('#statsPanel missing after auto tick');
  }, undefined, scopeOpts);

  return findings;
}

// interaction: rerender-timers
// The app's 8s pollers should survive a view re-render by being reused or
// replaced, never added on top of each other. Re-render Observer 10 times and
// compare (a) the live setInterval count before/after, and (b) the /api/stats
// request rate in two 17s windows bracketing the re-renders. Uses a fresh
// page with an addInitScript setInterval wrapper — the runner's context is
// out of scope by the time the first matrix cell has already painted.
async function runRerenderTimers(page, scopeOpts = {}) {
  const findings = [];
  const base = process.env.TEST_HTTP || 'http://127.0.0.1:18080';
  const token = process.env.TEST_TOKEN || 'dw-test-token-12345';

  const browser = page.context() && page.context().browser();
  if (!browser) throw new Error('no browser handle available to open the fresh tracking page');
  const size = page.viewportSize() || { width: 390, height: 844 };

  const fresh = await browser.newContext({
    ignoreHTTPSErrors: true,
    viewport: { width: size.width, height: size.height },
    deviceScaleFactor: 1,
  });
  try {
    await fresh.addInitScript(([theme, tok]) => {
      try {
        localStorage.setItem('cs_theme', theme);
        localStorage.setItem('cs_token', tok);
      } catch { /* init may run before a document exists; page script re-injects */ }
    }, ['dark', token]);

    await fresh.addInitScript(() => {
      const live = new Set();
      window.__liveIntervals = live;
      const origSet = window.setInterval.bind(window);
      const origClear = window.clearInterval.bind(window);
      // Arrow functions have no `arguments` binding — rest args only.
      window.setInterval = (...args) => {
        const id = origSet(...args);
        if (id !== undefined && id !== null) live.add(id);
        return id;
      };
      window.clearInterval = (id) => { live.delete(id); return origClear(id); };
    });

    const p2 = await fresh.newPage();
    // Diagnostics: if the app script crashes mid-evaluation in the fresh page
    // (hoisted functions stay callable while later `const` bindings sit in
    // their TDZ — observed as `Cannot access '_dash' before initialization`
    // when `navigate` runs), these collectors record what the app saw.
    const diag = { pageErrors: [], consoleErrors: [], bootErrors: [], reloads: 0 };
    const note = (list, msg) => {
      list.push(String(msg).replace(/\s*\n\s*/g, ' | ').slice(0, 400));
      if (list.length > 12) list.shift();
    };
    let pageLoaded = false;
    p2.on('pageerror', (e) => { note(diag.pageErrors, e.message || e); });
    p2.on('console', (m) => {
      if (m.type() === 'error') note(diag.consoleErrors, m.text().slice(0, 200));
    });
    p2.on('framenavigated', (frame) => {
      try {
        if (pageLoaded && typeof p2.mainFrame === 'function' && frame === p2.mainFrame()) {
          diag.reloads += 1;
        }
      } catch { /* keep the run alive no matter what the handler sees */ }
    });
    let keepAlive = null;
    try {
      try {
        await p2.goto(base, { waitUntil: 'domcontentloaded', timeout: 20000 });
      } catch {
        // Redirect or slow-daemon hiccups can surface here; bootVerify below
        // reloads the page on any anomaly, so just note and continue.
      }
      pageLoaded = true;

      // The app auto-reloads when it believes a newer daemon build arrived and
      // the page has been idle ≥30s (app.js `_canAutoReload`); automated pages
      // emit no real input, so nudge the app's own passive activity listeners
      // to keep that path dormant. No test hooks — just synthetic events.
      keepAlive = setInterval(() => {
        if (typeof p2.isClosed === 'function' && p2.isClosed()) return;
        p2.evaluate(() => {
          window.dispatchEvent(new Event('mousemove'));
          window.dispatchEvent(new Event('wheel'));
        }).catch(() => {});
      }, 4000);

      // Boot check: `window.navigate` is a top-level function declaration, so
      // it exists even when the app crashed mid-evaluation. Actually invoking
      // it (and landing on #perSystemGrid) proves the full script, including
      // the `_dash` const, evaluated.
      const bootVerify = async (attempts) => {
        for (let attempt = 1; attempt <= attempts; attempt += 1) {
          try {
            const ok = await p2.evaluate(() => {
              try {
                if (typeof window.navigate !== 'function') return false;
                window.navigate('observer');
              } catch { return false; }
              return !!document.getElementById('perSystemGrid');
            }, { timeout: 20000 });
            if (ok) return true;
            note(diag.bootErrors, `attempt ${attempt}: navigate ok but #perSystemGrid missing`);
          } catch (e) {
            note(diag.bootErrors, `attempt ${attempt}: ` + (e && e.message ? e.message.split('\n')[0] : String(e)));
          }
          // The page may be mid-crash or already auto-reloaded; reset the seed
          // routes, reload, and try again from a clean document.
          try { clearSeed(p2); } catch { /* routes may not exist yet */ }
          await p2.reload({ waitUntil: 'domcontentloaded', timeout: 20000 }).catch(() => {});
          await sleep(800);
          try { await installSeed(p2, INTERACTION_STATE); } catch { /* retry next loop */ }
        }
        return false;
      };

      await sleep(500);
      await p2.evaluate((t) => { try { localStorage.setItem('cs_token', t); } catch { /* pre-document */ } }, token).catch(() => {});
      try { clearSeed(p2); } catch { /* routes may not exist yet */ }
      await installSeed(p2, INTERACTION_STATE);
      if (!(await bootVerify(3))) {
        throw new Error('rerender-timers: fresh page failed to boot the observer view — ' + [
          `reloads=${diag.reloads}`,
          `bootErrors=[${diag.bootErrors.join(' ; ')}]`,
          `pageErrors=[${diag.pageErrors.join(' ; ')}]`,
          `consoleErrors=[${diag.consoleErrors.join(' ; ')}]`,
        ].join(' '));
      }
      await observerProvider.settle(p2, INTERACTION_STATE, 'settled');
      await sleep(1000); // let the app finish its first paint (timer registration included)

      const N1 = await p2.evaluate(() => (window.__liveIntervals && window.__liveIntervals.size) || 0);
      const baselineLayout = await collectFindings(p2, scopeOpts).catch(() => []);

      // Window 1: 17s of normal auto-refresh traffic.
      let statsRequests = 0;
      const listener = (req) => {
        try {
          const u = new URL(req.url());
          if (u.pathname === '/api/stats' || u.pathname.endsWith('/api/stats')) statsRequests += 1;
        } catch { /* count is best-effort */ }
      };
      p2.on('request', listener);
      await sleep(17000);
      const R1 = statsRequests;

      // Interleave 10 re-renders of the Observer view.
      for (let i = 0; i < 10; i += 1) {
        await p2.evaluate(() => { window.navigate('sessions'); }).catch(() => {});
        await sleep(150);
        await p2.evaluate(() => { window.navigate('observer'); }).catch(() => {});
        await observerProvider.settle(p2, INTERACTION_STATE, 'settled');
      }

      const N2 = await p2.evaluate(() => (window.__liveIntervals && window.__liveIntervals.size) || 0);
      // The re-render loop itself fetches stats on every navigation; mark where
      // it ended so window 2 counts only the steady-state poller traffic.
      const RAfterLoop = statsRequests;

      // Window 2: another 17s; its request count vs window 1 is the
      // steady-state rate after 10 re-renders.
      await sleep(17000);
      const R2 = statsRequests - RAfterLoop;
      p2.off('request', listener);

      if (N2 > N1) {
        findings.push(interactionFinding(
          'window.setInterval',
          `live setInterval count grew from ${N1} to ${N2} after 10 Observer re-renders — auto-refresh timers stack on re-render instead of being replaced`,
        ));
      } else if (N1 === 0) {
        findings.push(interactionFinding(
          'window.setInterval',
          'could not observe any live setInterval from the tracking wrapper; timer stacking neither confirmed nor refuted',
        ));
      }

      const perTick = Math.max(1, Math.ceil(R1 / 2));
      if (R2 - R1 > perTick) {
        findings.push(interactionFinding(
          '#perSystemGrid',
          `auto-refresh stacking: /api/stats requests per 17s window grew from ${R1} (before re-renders) to ${R2} (after) — delta ${R2 - R1} exceeds tolerance ${perTick}`,
        ));
      }

      const finalLayout = await collectFindings(p2, scopeOpts).catch(() => []);
      if (JSON.stringify(finalLayout) !== JSON.stringify(baselineLayout)) {
        findings.push(interactionFinding(
          '#settings-sec-stats',
          `layout drifted after the rerender loop: ${baselineLayout.length} layout finding(s) before, ${finalLayout.length} after`,
        ));
      }
      findings.push(...finalLayout);
    } finally {
      if (keepAlive) { clearInterval(keepAlive); keepAlive = null; }
      await p2.close().catch(() => {});
    }
  } finally {
    await fresh.close().catch(() => {});
  }
  return findings;
}

export const observerProvider = {
  id: 'observer',

  // States: every seeded API state the Observer / System Statistics view has to
  // fit, in the order the matrix visits them. The slow state last so its longer
  // settle cap lands on the same view geometry as the fast ones.
  states: STATE_NAMES,
  quickStates: STATE_NAMES.filter((s) => !['stats-empty', SLOW_STATE].includes(s)),
  zoomStates: ['three-peers'],

  // Named scenarios: the ones named in this PRD's spec.
  named: [
    {
      name: 'system-stats-phone',
      state: 'three-peers',
      theme: 'dark',
      zoom: 100,
      viewport: { width: 360, height: 740 },
      scope: '#settings-sec-stats',
    },
  ],

  // Interactions the runner performs at the first matrix cell (seeded).
  interactions: [
    'section-collapse-expand',
    'refresh',
    'rerender-timers',
  ],

  // Phases the runner walks per state. The slow state has a distinct loading
  // phase (placeholders visible) worth capturing; the others load fast enough
  // that only the settled phase is meaningful.
  phases(state) {
    if (state === SLOW_STATE) return ['loading', 'settled'];
    return ['settled'];
  },

  // State-specific API seed.
  seed(page, state) { return installSeed(page, state); },

  // Drive the app to the Observer / System Statistics view.
  async open(page) {
    const view = await page.evaluate(() => {
      const el = document.querySelector('[data-view="observer"]');
      if (!el) return { exists: false };
      return { exists: true, active: el.classList.contains('active'), visible: !!(el.offsetParent !== null) };
    });

    if (view.exists && view.active) {
      // Force a re-render on (re)navigation so the seeded payloads are re-consumed.
      await page.evaluate(() => { if (typeof window.navigate !== 'function') throw new Error('navigate unavailable'); window.navigate('sessions'); });
      await page.waitForTimeout(250);
      await page.evaluate(() => { if (typeof window.navigate !== 'function') throw new Error('navigate unavailable'); window.navigate('observer'); });
    } else if (view.exists && view.visible) {
      await page.evaluate(() => {
        const el = document.querySelector('[data-view="observer"]');
        if (el) el.click(); else throw new Error('observer nav hidden');
      });
    } else {
      await page.evaluate(() => { if (typeof window.navigate !== 'function') throw new Error('navigate unavailable'); window.navigate('observer'); });
    }

    await page.waitForTimeout(250);
    // Wait for the stats section to paint (per-system grid exists, non-empty).
    await page.waitForSelector('#perSystemGrid', { timeout: 15000 }).catch(() => {});
  },

  // Wait for the view to reach the requested phase.
  async settle(page, state, phase) {
    const cap = phase === 'loading'
      ? (state === SLOW_STATE ? SLOW_SETTLE_CAP_MS : 3000)
      : (state === SLOW_STATE ? SLOW_SETTLE_CAP_MS : SETTLE_CAP_MS);

    if (phase === 'loading') {
      // loading phase: wait for at least one of the stats panels to be showing
      // a loading placeholder (the slow state does this visibly).
      const deadline = Date.now() + cap;
      while (Date.now() < deadline) {
        const found = await page.evaluate((sel) => !!document.querySelector(sel), LOADING_PLACEHOLDERS).catch(() => false);
        if (found) return;
        await page.waitForTimeout(150);
      }
      return; // timed out; let checks report what they see
    }

    // settled phase: wait until no loading placeholders remain in the stats
    // section and the per-system grid has rendered cards.
    const isSettled = () => page
      .evaluate((sel) => {
        const section = document.getElementById('settings-sec-stats');
        const grid = document.getElementById('perSystemGrid');
        if (!section || !grid) return false;
        const loading = section.querySelector(sel);
        if (loading) return false;
        return grid.children.length > 0;
      }, LOADING_PLACEHOLDERS)
      .catch(() => false);

    const deadline = Date.now() + cap;
    while (Date.now() < deadline) {
      if (await isSettled()) return;
      await page.waitForTimeout(200);
    }
  },

  // Interactions: run one named interaction at the (already at the first
  // matrix cell) live page, returning extra interaction findings.
  // opts.scope (a CSS selector list) narrows the layout checks the interaction
  // runs between its steps; the interaction's own behavioural checks always run.
  async runInteraction(page, id, opts = {}) {
    const scopeOpts = Array.isArray(opts.scope) && opts.scope.length > 0 ? { scope: opts.scope } : {};
    switch (id) {
      case 'section-collapse-expand': return runSectionCollapseExpand(page, scopeOpts);
      case 'refresh': return runRefresh(page, scopeOpts);
      case 'rerender-timers': return runRerenderTimers(page, scopeOpts);
      default: throw new Error(`unknown interaction: ${id}`);
    }
  },
};

export { gridSelectorList, SETTLE_CAP_MS, SLOW_SETTLE_CAP_MS, SLOW_STATE };
