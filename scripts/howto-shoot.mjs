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
