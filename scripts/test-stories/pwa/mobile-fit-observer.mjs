// scripts/test-stories/pwa/mobile-fit-observer.mjs — provider module for the
// mobile-fit scenario harness (mobile-fit.mjs) for the Observer view of the
// datawatch PWA. Implements the provider contract documented in the header
// of mobile-fit.mjs: states / quickStates / zoomStates / named / interactions
// / phases / seed / open / settle.
//
// The view under test is renderObserverView() — the "System Statistics"
// section (#settings-sec-stats) with the per-system grid (#perSystemGrid)
// and the local stats panel (#statsPanel), plus the eBPF, plugins, backend
// health, ACME, envelopes and peers cards below it.
//
// Data comes from mobile-fit-seeds.mjs: installSeed(page, state) route-fakes
// every /api/** response the Observer view loads, so a single browser can
// reproduce every state deterministically against any (isolated) daemon.
//
// Conventions: ES module, no new npm deps, repo-relative imports only,
// no tracker ids in identifiers.

import { installSeed, STATE_NAMES } from './mobile-fit-seeds.mjs';

// The loading placeholder the Observer view renders in every card while a
// card's API call is in flight (loadingEyeBlock() in the app).
const LOADING_PLACEHOLDERS =
  '.spinner, .skeleton, .skeleton-line, .skeleton-card, .integ-loading, .loading-eye-block';

const STATS_PANEL = '#statsPanel';
const SYSTEM_GRID = '#perSystemGrid';

// settled caps: the normal matrix cap, and the relaxed cap for stats-slow
// whose seeded stats responses take 6 s to answer.
const SETTLE_CAP_MS = 12000;
const SLOW_SETTLE_CAP_MS = 15000;
const SLOW_STATE = 'stats-slow';

function gridSelectorList() {
  return SYSTEM_GRID;
}

export const observerProvider = {
  id: 'observer',

  // All 16 seeded states are part of the full sweep.
  states: [...STATE_NAMES],

  // quick sweep: the states that most often break the phone layout.
  quickStates: [
    'no-peers',
    'three-peers',
    'long-hostname',
    'ebpf-many-processes',
    'numbers-large',
    'stats-failed',
  ],

  // zoom sweep: states where horizontal overflow at 130%/200% is the risk.
  zoomStates: ['three-peers', 'long-hostname', 'ebpf-many-processes', 'numbers-large'],

  // Named scenarios: the exact reproduction the operator reported —
  // System Statistics on a phone (360 x 740 portrait, dark, zoom 100),
  // measured inside the System Statistics section only.
  named: [
    {
      name: 'system-stats-phone',
      state: 'three-peers',
      viewport: { width: 360, height: 740, orientation: 'portrait' },
      theme: 'dark',
      zoom: 100,
      scope: '#settings-sec-stats',
    },
  ],

  // Interactions are intentionally empty for now; a later task fills them in.
  interactions: [],

  // stats-slow is checked BOTH while the cards are still loading
  // (placeholders visible) and after the responses resolve.
  phases(state) {
    return state === SLOW_STATE ? ['loading', 'settled'] : ['settled'];
  },

  // Seed the Observer view into its state by faking every API it loads.
  async seed(page, state) {
    await installSeed(page, state);
  },

  // Open the Observer view, forcing a fresh renderObserverView() when the
  // view is already active (navigate to another view and back), then wait
  // for the section markup to be in the DOM.
  async open(page) {
    const isObserverActive = () =>
      document.querySelector('[data-view="observer"]')?.classList.contains('active');

    if (await page.evaluate(isObserverActive).catch(() => false)) {
      // Already on the Observer view: round-trip through another view so
      // renderObserverView() runs again and the seed is re-consumed.
      await page.evaluate(() => window.navigate('sessions'));
      await page.waitForTimeout(250);
      await page.evaluate(() => { window.navigate('observer'); });
    } else if (await page.locator('[data-view="observer"]').first().isVisible().catch(() => false)) {
      await page.locator('[data-view="observer"]').first().click();
    } else {
      await page.evaluate(() => { window.navigate('observer'); });
    }

    await page.waitForTimeout(250); // let the first render pass finish
    await page
      .waitForSelector(gridSelectorList(), { timeout: 15000 })
      .catch(() => { throw new Error('Observer view did not render: #perSystemGrid never appeared'); });
  },

  // Wait for a phase to be stable.
  //   'loading'  — let the first paint with cards in flight happen; the
  //                ~600 ms wait is long enough for the loading placeholders
  //                to be on screen (the stats-slow delay is 6 s, so at 600 ms
  //                the placeholders are still guaranteed to be visible).
  //   'settled'  — wait until #statsPanel has no loading placeholder left
  //                and #perSystemGrid has rendered its cards (cap 12 s;
  //                15 s for stats-slow), then 400 ms of quiet. Collection
  //                runs immediately after settle so the 8 s auto-refresh
  //                interval cannot rewrite the DOM mid-measurement.
  async settle(page, state, phase) {
    if (phase === 'loading') {
      await page.waitForTimeout(600);
      return;
    }
    const cap = state === SLOW_STATE ? SLOW_SETTLE_CAP_MS : SETTLE_CAP_MS;
    try {
      await page.waitForFunction(
        ([panelSel, gridSel, placeholders]) => {
          const panel = document.querySelector(panelSel);
          if (!panel) return false;
          if (panel.querySelector(placeholders)) return false;
          const grid = document.querySelector(gridSel);
          return !!grid && grid.children.length > 0;
        },
        [STATS_PANEL, SYSTEM_GRID, LOADING_PLACEHOLDERS],
        { timeout: cap, polling: 200 },
      );
    } catch { /* cap expired — treat the view as settled and collect anyway */ }
    await page.waitForTimeout(400); // 400 ms of quiet before collection
  },
};
