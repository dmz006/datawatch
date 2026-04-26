#!/usr/bin/env node
// BL190 (v5.11.0+) — puppeteer-core driver for the howto screenshot suite.
//
// Drives /usr/bin/google-chrome through the CDP via puppeteer-core.
// Pre-seeds localStorage so the splash never blocks captures, waits on
// real selectors before shooting, and writes one PNG per shot label
// into <out-dir>/<howto>-<label>.png.
//
// Usage:
//   node scripts/howto-shoot.mjs <howto-name> [--out=<dir>] [--base=<url>]
//
// Where <howto-name> is one of:
//   sessions-landing, autonomous-landing, autonomous-create-modal,
//   settings-llm, settings-comms, settings-voice, …
// (the recipe map below grows as new shots are added)

import { mkdir } from 'node:fs/promises';
import path from 'node:path';

// puppeteer-core is installed out-of-tree at /tmp/puppet (per the
// BL190 plan: not committed; survives between runs). Import via the
// absolute path so this script can live in scripts/ without dragging
// node_modules into the repo.
const PUPPET_DIR = process.env.PUPPET_DIR || '/tmp/puppet';
const puppeteer = (await import(`${PUPPET_DIR}/node_modules/puppeteer-core/lib/esm/puppeteer/puppeteer-core.js`)).default;

const args = Object.fromEntries(
  process.argv.slice(2).filter(a => a.startsWith('--')).map(a => {
    const [k, ...v] = a.replace(/^--/, '').split('=');
    return [k, v.join('=') || true];
  })
);
const positional = process.argv.slice(2).filter(a => !a.startsWith('--'));
const recipeName = positional[0] || 'all';
const baseURL = args.base || 'https://localhost:8443/';
const outDir = path.resolve(args.out || 'docs/howto/screenshots');

await mkdir(outDir, { recursive: true });

// One recipe = one named shot. Each step runs in the page context.
// The shoot loop pre-loads the page, runs setup steps, then captures.
const RECIPES = {
  // Sessions tab landing — empty or populated state, depending on
  // whether the seed-fixtures script ran first.
  'sessions-landing': {
    setup: async (page) => {
      await page.evaluate(() => {
        localStorage.setItem('cs_active_view', 'sessions');
        localStorage.setItem('cs_splash_time', String(Date.now()));
        localStorage.setItem('cs_splash_version', 'shot');
      });
      await page.reload();
      await page.waitForSelector('.nav-btn.active[data-view="sessions"]', { timeout: 10000 });
      await sleep(500);
    },
  },
  // Autonomous tab landing — PRD list (populated by seed fixtures).
  'autonomous-landing': {
    setup: async (page) => {
      await page.evaluate(() => {
        localStorage.setItem('cs_active_view', 'autonomous');
        localStorage.setItem('cs_splash_time', String(Date.now()));
        localStorage.setItem('cs_splash_version', 'shot');
      });
      await page.reload();
      await page.waitForSelector('.nav-btn.active[data-view="autonomous"]', { timeout: 10000 });
      await sleep(500);
    },
  },
  // Settings → LLM sub-tab.
  'settings-llm': {
    setup: async (page) => {
      await page.evaluate(() => {
        localStorage.setItem('cs_active_view', 'settings');
        localStorage.setItem('cs_splash_time', String(Date.now()));
        localStorage.setItem('cs_splash_version', 'shot');
      });
      await page.reload();
      await page.waitForSelector('.settings-tab-btn', { timeout: 10000 });
      await page.evaluate(() => {
        if (typeof window.switchSettingsTab === 'function') window.switchSettingsTab('llm');
      });
      await sleep(800);
    },
  },
  // Settings → Comms sub-tab.
  'settings-comms': {
    setup: async (page) => {
      await page.evaluate(() => {
        localStorage.setItem('cs_active_view', 'settings');
        localStorage.setItem('cs_splash_time', String(Date.now()));
        localStorage.setItem('cs_splash_version', 'shot');
      });
      await page.reload();
      await page.waitForSelector('.settings-tab-btn', { timeout: 10000 });
      await page.evaluate(() => {
        if (typeof window.switchSettingsTab === 'function') window.switchSettingsTab('comms');
      });
      await sleep(800);
    },
  },
  // Settings → General → Voice Input section.
  'settings-voice': {
    setup: async (page) => {
      await page.evaluate(() => {
        localStorage.setItem('cs_active_view', 'settings');
        localStorage.setItem('cs_splash_time', String(Date.now()));
        localStorage.setItem('cs_splash_version', 'shot');
      });
      await page.reload();
      await page.waitForSelector('.settings-tab-btn', { timeout: 10000 });
      await page.evaluate(() => {
        if (typeof window.switchSettingsTab === 'function') window.switchSettingsTab('general');
      });
      await sleep(800);
    },
  },
  // /diagrams.html landing.
  'diagrams-landing': {
    url: 'diagrams.html',
    setup: async (page) => {
      await page.waitForSelector('.diagram-shell, body', { timeout: 10000 });
      await sleep(500);
    },
  },
  // Settings → Monitor — system stats card (CPU/mem/disk/GPU + sessions).
  'settings-monitor': {
    setup: async (page) => {
      await page.evaluate(() => {
        localStorage.setItem('cs_active_view', 'settings');
        localStorage.setItem('cs_splash_time', String(Date.now()));
        localStorage.setItem('cs_splash_version', 'shot');
      });
      await page.reload();
      await page.waitForSelector('.settings-tab-btn', { timeout: 10000 });
      await page.evaluate(() => {
        if (typeof window.switchSettingsTab === 'function') window.switchSettingsTab('monitor');
      });
      await sleep(800);
    },
  },
  // Settings → About — version, links, orphan-tmux maintenance.
  'settings-about': {
    setup: async (page) => {
      await page.evaluate(() => {
        localStorage.setItem('cs_active_view', 'settings');
        localStorage.setItem('cs_splash_time', String(Date.now()));
        localStorage.setItem('cs_splash_version', 'shot');
      });
      await page.reload();
      await page.waitForSelector('.settings-tab-btn', { timeout: 10000 });
      await page.evaluate(() => {
        if (typeof window.switchSettingsTab === 'function') window.switchSettingsTab('about');
      });
      await sleep(800);
    },
  },
  // Alerts tab — system alerts list.
  'alerts-tab': {
    setup: async (page) => {
      await page.evaluate(() => {
        localStorage.setItem('cs_active_view', 'alerts');
        localStorage.setItem('cs_splash_time', String(Date.now()));
        localStorage.setItem('cs_splash_version', 'shot');
      });
      await page.reload();
      await page.waitForSelector('.nav-btn.active[data-view="alerts"]', { timeout: 10000 });
      await sleep(500);
    },
  },
  // New PRD modal — captured by clicking the +New PRD button after the
  // autonomous tab loads.
  'autonomous-new-prd-modal': {
    setup: async (page) => {
      await page.evaluate(() => {
        localStorage.setItem('cs_active_view', 'autonomous');
        localStorage.setItem('cs_splash_time', String(Date.now()));
        localStorage.setItem('cs_splash_version', 'shot');
      });
      await page.reload();
      await page.waitForSelector('.nav-btn.active[data-view="autonomous"]', { timeout: 10000 });
      await sleep(400);
      // Click the New PRD button — its label varies but always contains "New PRD".
      await page.evaluate(() => {
        const btn = Array.from(document.querySelectorAll('button')).find(b => /new prd/i.test(b.textContent));
        if (btn) btn.click();
      });
      await sleep(700);
    },
  },
  // Session detail — drill in by clicking the first session card.
  'session-detail': {
    setup: async (page) => {
      await page.evaluate(() => {
        localStorage.setItem('cs_active_view', 'sessions');
        localStorage.setItem('cs_splash_time', String(Date.now()));
        localStorage.setItem('cs_splash_version', 'shot');
      });
      await page.reload();
      await page.waitForSelector('.session-card, .nav-btn.active[data-view="sessions"]', { timeout: 10000 });
      await sleep(400);
      await page.evaluate(() => {
        const card = document.querySelector('.session-card');
        if (card) card.click();
      });
      await sleep(900);
    },
  },
  // Mobile-viewport (narrow) shot of the Sessions tab — exercises the
  // PWA's responsive layout that the desktop captures don't see.
  'sessions-mobile': {
    viewport: { width: 412, height: 850, deviceScaleFactor: 2 },
    setup: async (page) => {
      await page.evaluate(() => {
        localStorage.setItem('cs_active_view', 'sessions');
        localStorage.setItem('cs_splash_time', String(Date.now()));
        localStorage.setItem('cs_splash_version', 'shot');
      });
      await page.reload();
      await page.waitForSelector('.nav-btn.active[data-view="sessions"]', { timeout: 10000 });
      await sleep(500);
    },
  },
  // Mobile-viewport Autonomous tab.
  'autonomous-mobile': {
    viewport: { width: 412, height: 850, deviceScaleFactor: 2 },
    setup: async (page) => {
      await page.evaluate(() => {
        localStorage.setItem('cs_active_view', 'autonomous');
        localStorage.setItem('cs_splash_time', String(Date.now()));
        localStorage.setItem('cs_splash_version', 'shot');
      });
      await page.reload();
      await page.waitForSelector('.nav-btn.active[data-view="autonomous"]', { timeout: 10000 });
      await sleep(500);
    },
  },
  // Autonomous PRD card expanded — clicks the "Stories & tasks"
  // toggle on the *rich* PRD (the seeded fixrich row, which has 1
  // story + 3 tasks + 3 decisions) so the inline story+task tree
  // has substance. Falls back to the first details if the rich
  // fixture isn't present.
  'autonomous-prd-expanded': {
    setup: async (page) => {
      await page.evaluate(() => {
        localStorage.setItem('cs_active_view', 'autonomous');
        localStorage.setItem('cs_splash_time', String(Date.now()));
        localStorage.setItem('cs_splash_version', 'shot');
      });
      await page.reload();
      await page.waitForSelector('.nav-btn.active[data-view="autonomous"]', { timeout: 10000 });
      await sleep(500);
      const expanded = await page.evaluate(() => {
        // Find the details element that contains "fixrich" or "rich PRD"
        // by walking up from any matching text node.
        const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
        let node;
        while ((node = walker.nextNode())) {
          if (/rich PRD with stories|fixrich/i.test(node.textContent || '')) {
            let el = node.parentElement;
            while (el && el.tagName !== 'DETAILS') el = el.parentElement;
            if (el) {
              el.open = true;
              el.scrollIntoView({ block: 'start' });
              return true;
            }
          }
        }
        // Fallback: open whichever details has the most non-empty content.
        const allDetails = Array.from(document.querySelectorAll('details'));
        let best = null, bestLen = 0;
        for (const d of allDetails) {
          const len = (d.textContent || '').length;
          if (len > bestLen) { bestLen = len; best = d; }
        }
        if (best) { best.open = true; best.scrollIntoView({ block: 'start' }); }
        return false;
      });
      // Give the layout a tick; one more scroll to keep the card visible.
      await sleep(700);
      if (expanded) {
        await page.evaluate(() => {
          const richCard = Array.from(document.querySelectorAll('details')).find(d => /rich PRD with stories|fixrich/i.test(d.textContent || ''));
          if (richCard) richCard.scrollIntoView({ block: 'start' });
        });
        await sleep(300);
      }
    },
  },
  // Settings → General scrolled to the Autonomous block — operators
  // landing here from chat-channel docs see the toggles they need.
  'settings-general-autonomous': {
    setup: async (page) => {
      await page.evaluate(() => {
        localStorage.setItem('cs_active_view', 'settings');
        localStorage.setItem('cs_splash_time', String(Date.now()));
        localStorage.setItem('cs_splash_version', 'shot');
      });
      await page.reload();
      await page.waitForSelector('.settings-tab-btn', { timeout: 10000 });
      await page.evaluate(() => {
        if (typeof window.switchSettingsTab === 'function') window.switchSettingsTab('general');
      });
      await sleep(500);
      // Scroll the Autonomous label into the upper half of the
      // viewport. Target by visible text since the section header
      // doesn't have a stable ID.
      await page.evaluate(() => {
        const all = document.querySelectorAll('.settings-section-title, h2, h3, .settings-card-title, summary');
        for (const el of all) {
          if (/autonomous/i.test(el.textContent || '')) {
            el.scrollIntoView({ block: 'start' });
            return;
          }
        }
      });
      await sleep(500);
    },
  },
  // Settings → Comms expanded to a specific backend — useful for
  // comm-channels.md where each backend has its own block.
  'settings-comms-signal': {
    setup: async (page) => {
      await page.evaluate(() => {
        localStorage.setItem('cs_active_view', 'settings');
        localStorage.setItem('cs_splash_time', String(Date.now()));
        localStorage.setItem('cs_splash_version', 'shot');
      });
      await page.reload();
      await page.waitForSelector('.settings-tab-btn', { timeout: 10000 });
      await page.evaluate(() => {
        if (typeof window.switchSettingsTab === 'function') window.switchSettingsTab('comms');
      });
      await sleep(500);
      await page.evaluate(() => {
        const all = document.querySelectorAll('.settings-section-title, h2, h3, .settings-card-title, summary');
        for (const el of all) {
          if (/signal/i.test(el.textContent || '')) {
            el.scrollIntoView({ block: 'start' });
            return;
          }
        }
      });
      await sleep(500);
    },
  },
  // Settings → LLM scrolled to the ollama / openwebui block (useful
  // for chat-and-llm-quickstart's local-first path).
  'settings-llm-ollama': {
    setup: async (page) => {
      await page.evaluate(() => {
        localStorage.setItem('cs_active_view', 'settings');
        localStorage.setItem('cs_splash_time', String(Date.now()));
        localStorage.setItem('cs_splash_version', 'shot');
      });
      await page.reload();
      await page.waitForSelector('.settings-tab-btn', { timeout: 10000 });
      await page.evaluate(() => {
        if (typeof window.switchSettingsTab === 'function') window.switchSettingsTab('llm');
      });
      await sleep(500);
      await page.evaluate(() => {
        const all = document.querySelectorAll('.settings-section-title, h2, h3, .settings-card-title, summary');
        for (const el of all) {
          if (/ollama/i.test(el.textContent || '')) {
            el.scrollIntoView({ block: 'start' });
            return;
          }
        }
      });
      await sleep(500);
    },
  },
  // Mobile-viewport Settings → Monitor — verifies responsive layout.
  'settings-monitor-mobile': {
    viewport: { width: 412, height: 850, deviceScaleFactor: 2 },
    setup: async (page) => {
      await page.evaluate(() => {
        localStorage.setItem('cs_active_view', 'settings');
        localStorage.setItem('cs_splash_time', String(Date.now()));
        localStorage.setItem('cs_splash_version', 'shot');
      });
      await page.reload();
      await page.waitForSelector('.settings-tab-btn', { timeout: 10000 });
      await page.evaluate(() => {
        if (typeof window.switchSettingsTab === 'function') window.switchSettingsTab('monitor');
      });
      await sleep(800);
    },
  },
  // Autonomous PRD with both Stories & tasks AND Decisions log
  // expanded — the full per-PRD audit trail screenshot.
  'autonomous-prd-decisions': {
    setup: async (page) => {
      await page.evaluate(() => {
        localStorage.setItem('cs_active_view', 'autonomous');
        localStorage.setItem('cs_splash_time', String(Date.now()));
        localStorage.setItem('cs_splash_version', 'shot');
      });
      await page.reload();
      await page.waitForSelector('.nav-btn.active[data-view="autonomous"]', { timeout: 10000 });
      await sleep(500);
      await page.evaluate(() => {
        const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
        let node;
        while ((node = walker.nextNode())) {
          if (/rich PRD with stories|fixrich/i.test(node.textContent || '')) {
            let el = node.parentElement;
            while (el && el.tagName !== 'DETAILS') el = el.parentElement;
            if (el) {
              el.open = true;
              // Open every nested details too — Decisions log is one.
              el.querySelectorAll('details').forEach(d => (d.open = true));
              el.scrollIntoView({ block: 'start' });
              return;
            }
          }
        }
      });
      await sleep(700);
    },
  },
  // Session detail mobile — tmux pane in portrait viewport.
  'session-detail-mobile': {
    viewport: { width: 412, height: 850, deviceScaleFactor: 2 },
    setup: async (page) => {
      await page.evaluate(() => {
        localStorage.setItem('cs_active_view', 'sessions');
        localStorage.setItem('cs_splash_time', String(Date.now()));
        localStorage.setItem('cs_splash_version', 'shot');
      });
      await page.reload();
      await page.waitForSelector('.session-card, .nav-btn.active[data-view="sessions"]', { timeout: 10000 });
      await sleep(400);
      await page.evaluate(() => {
        const card = document.querySelector('.session-card');
        if (card) card.click();
      });
      await sleep(900);
    },
  },
  // Settings → General scrolled to Auto-update section.
  'settings-general-auto-update': {
    setup: async (page) => {
      await page.evaluate(() => {
        localStorage.setItem('cs_active_view', 'settings');
        localStorage.setItem('cs_splash_time', String(Date.now()));
        localStorage.setItem('cs_splash_version', 'shot');
      });
      await page.reload();
      await page.waitForSelector('.settings-tab-btn', { timeout: 10000 });
      await page.evaluate(() => {
        if (typeof window.switchSettingsTab === 'function') window.switchSettingsTab('general');
      });
      await sleep(500);
      await page.evaluate(() => {
        const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
        let node;
        while ((node = walker.nextNode())) {
          if (/auto.?update/i.test(node.textContent || '')) {
            let el = node.parentElement;
            while (el && !el.classList.contains('settings-section') && el !== document.body) el = el.parentElement;
            (el || node.parentElement).scrollIntoView({ block: 'start' });
            return;
          }
        }
      });
      await sleep(400);
    },
  },
  // Settings → LLM scrolled to memory/embedder block.
  'settings-llm-memory': {
    setup: async (page) => {
      await page.evaluate(() => {
        localStorage.setItem('cs_active_view', 'settings');
        localStorage.setItem('cs_splash_time', String(Date.now()));
        localStorage.setItem('cs_splash_version', 'shot');
      });
      await page.reload();
      await page.waitForSelector('.settings-tab-btn', { timeout: 10000 });
      await page.evaluate(() => {
        if (typeof window.switchSettingsTab === 'function') window.switchSettingsTab('llm');
      });
      await sleep(500);
      await page.evaluate(() => {
        const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
        let node;
        while ((node = walker.nextNode())) {
          if (/episodic memory|embedder/i.test(node.textContent || '')) {
            (node.parentElement || document.body).scrollIntoView({ block: 'start' });
            return;
          }
        }
      });
      await sleep(400);
    },
  },
  // Header search affordance — toggles the search bar from the header.
  'header-search': {
    setup: async (page) => {
      await page.evaluate(() => {
        localStorage.setItem('cs_active_view', 'sessions');
        localStorage.setItem('cs_splash_time', String(Date.now()));
        localStorage.setItem('cs_splash_version', 'shot');
      });
      await page.reload();
      await page.waitForSelector('.nav-btn.active[data-view="sessions"]', { timeout: 10000 });
      await sleep(400);
      await page.evaluate(() => {
        const btn = document.querySelector('#headerSearchBtn');
        if (btn) btn.click();
      });
      await sleep(600);
    },
  },
  // Diagrams page scrolled to a flowchart so the README screenshot
  // shows actual content rather than just the header.
  'diagrams-flow': {
    url: 'diagrams.html',
    setup: async (page) => {
      await page.waitForSelector('.diagram-shell, body', { timeout: 10000 });
      await sleep(500);
      // Click the first sidebar entry that mentions a flow.
      await page.evaluate(() => {
        const link = Array.from(document.querySelectorAll('a, .diagram-toc-item, .toc-link')).find(a => /flow|architecture/i.test(a.textContent || ''));
        if (link) link.click();
      });
      await sleep(1200);
    },
  },
};

function sleep(ms) { return new Promise(r => setTimeout(r, ms)); }

const recipes = recipeName === 'all' ? Object.keys(RECIPES) : [recipeName];

const browser = await puppeteer.launch({
  executablePath: '/usr/bin/google-chrome',
  headless: 'new',
  args: ['--ignore-certificate-errors', '--no-sandbox', '--disable-dev-shm-usage'],
  defaultViewport: { width: 1280, height: 900, deviceScaleFactor: 1 },
});

try {
  for (const name of recipes) {
    const recipe = RECIPES[name];
    if (!recipe) {
      console.error(`[shoot] unknown recipe ${name} — skipping`);
      continue;
    }
    const page = await browser.newPage();
    page.on('pageerror', e => console.error(`[shoot:${name}] pageerror`, e.message));
    if (recipe.viewport) {
      await page.setViewport(recipe.viewport);
    }
    const url = new URL(recipe.url || '/', baseURL).href;
    try {
      await page.goto(url, { waitUntil: 'networkidle0', timeout: 20000 });
    } catch (e) {
      // Splash blocks networkidle0 on some daemons — keep going.
      console.error(`[shoot:${name}] goto: ${e.message} — proceeding anyway`);
    }
    if (recipe.setup) await recipe.setup(page);
    const file = path.join(outDir, `${name}.png`);
    await page.screenshot({ path: file, fullPage: false });
    console.log(`[shoot] ${name} → ${file}`);
    await page.close();
  }
} finally {
  await browser.close();
}
