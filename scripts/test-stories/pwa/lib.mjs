// scripts/test-stories/pwa/lib.mjs — shared Playwright helpers for datawatch PWA tests.
//
// Each TS-NNN.mjs story imports from this file. Stories exit 0 (pass) or 1 (fail).
// The calling TS-NNN.sh bash story maps the exit code to RESULT=pass|fail.
//
// Env vars (set by lib.sh / run_pwa_story):
//   TEST_HTTP      — daemon HTTP base URL (e.g. http://127.0.0.1:18080)
//   TEST_TLS       — daemon HTTPS base URL (e.g. https://127.0.0.1:18443)
//   TEST_TOKEN     — auth token
//   EVIDENCE_DIR   — where to write screenshots + logs
//   CURRENT_STORY  — story ID (e.g. TS-130), used for evidence subdirectory

import { chromium } from 'playwright';
import { mkdir } from 'node:fs/promises';
import path from 'node:path';

const CHROME_EXECUTABLE = '/usr/bin/google-chrome';

// --- env helpers ------------------------------------------------------------

export const env = {
  http:    process.env.TEST_HTTP    || 'http://127.0.0.1:18080',
  tls:     process.env.TEST_TLS    || 'https://127.0.0.1:18443',
  token:   process.env.TEST_TOKEN  || 'dw-test-token-12345',
  evidenceDir: process.env.EVIDENCE_DIR || '/tmp/dw-pwa-evidence',
  story:   process.env.CURRENT_STORY || 'TS-000',
};

export function storyEvidenceDir() {
  return path.join(env.evidenceDir, env.story);
}

// --- browser ----------------------------------------------------------------

export async function launchBrowser({ headless = true } = {}) {
  return chromium.launch({
    executablePath: CHROME_EXECUTABLE,
    headless,
    args: [
      '--no-sandbox',
      '--disable-dev-shm-usage',
      '--disable-gpu',
      '--ignore-certificate-errors',   // test daemon uses self-signed TLS
    ],
  });
}

// --- PWA setup --------------------------------------------------------------

// Navigate to the PWA, inject auth token into localStorage, reload so the app
// picks it up, then wait for the splash to resolve (WS connected).
export async function connectToPWA(page, { base = env.tls, token = env.token, timeout = 20000 } = {}) {
  // First load — may show splash, 401, or unstyled page. Just need the origin.
  await page.goto(base, { waitUntil: 'domcontentloaded', timeout });

  // Inject token — the PWA reads from localStorage key 'dw_token'.
  await page.evaluate((t) => localStorage.setItem('dw_token', t), token);

  // Reload with token in place.
  await page.reload({ waitUntil: 'domcontentloaded', timeout });

  // Wait for splash overlay to hide — indicates WebSocket connected + sessions loaded.
  await waitForSplash(page, timeout);
}

export async function waitForSplash(page, timeout = 20000) {
  await page.waitForFunction(
    () => {
      const el = document.getElementById('splash');
      return !el || el.style.display === 'none' || el.classList.contains('hidden') ||
             window.getComputedStyle(el).display === 'none' ||
             window.getComputedStyle(el).opacity === '0';
    },
    { timeout },
  );
}

// --- navigation -------------------------------------------------------------

// Click the bottom-nav button for a view and wait for it to become active.
// view: 'sessions' | 'autonomous' | 'alerts' | 'observer' | 'settings'
export async function navigateTo(page, view, timeout = 5000) {
  await page.click(`[data-view="${view}"]`, { timeout });
  await page.waitForFunction(
    (v) => document.querySelector(`[data-view="${v}"]`)?.classList.contains('active'),
    view,
    { timeout },
  );
}

// --- assertions -------------------------------------------------------------

export async function assertVisible(page, selector, description, timeout = 5000) {
  try {
    await page.waitForSelector(selector, { state: 'visible', timeout });
  } catch {
    throw new Error(`assertVisible failed: ${description} — selector not visible: ${selector}`);
  }
}

export async function assertText(page, selector, expected, timeout = 5000) {
  await assertVisible(page, selector, `text check for "${expected}"`, timeout);
  const actual = await page.textContent(selector);
  if (!actual?.includes(expected)) {
    throw new Error(`assertText failed: expected "${expected}" in "${actual}"`);
  }
}

export async function assertCount(page, selector, min = 1, timeout = 5000) {
  await page.waitForFunction(
    ({ sel, n }) => document.querySelectorAll(sel).length >= n,
    { sel: selector, n: min },
    { timeout },
  );
}

// --- evidence ---------------------------------------------------------------

export async function screenshot(page, name) {
  const dir = storyEvidenceDir();
  await mkdir(dir, { recursive: true });
  await page.screenshot({ path: path.join(dir, `${name}.png`), fullPage: false });
}

export async function saveLog(name, content) {
  const dir = storyEvidenceDir();
  await mkdir(dir, { recursive: true });
  const { writeFile } = await import('node:fs/promises');
  await writeFile(path.join(dir, `${name}.txt`), content);
}

// --- story runner -----------------------------------------------------------

// Wrap a story function: launches browser, runs fn(page), tears down.
// Exits 0 on pass, 1 on fail. Call this from every TS-NNN.mjs.
export async function runStory(fn) {
  let browser;
  try {
    browser = await launchBrowser();
    const context = await browser.newContext({
      ignoreHTTPSErrors: true,
      viewport: { width: 1280, height: 800 },
    });
    const page = await context.newPage();

    // Capture console errors as evidence.
    const consoleErrors = [];
    page.on('console', (msg) => { if (msg.type() === 'error') consoleErrors.push(msg.text()); });
    page.on('pageerror', (err) => consoleErrors.push(`pageerror: ${err.message}`));

    await fn(page);

    if (consoleErrors.length > 0) {
      await saveLog('console-errors', consoleErrors.join('\n'));
    }

    await browser.close();
    process.exit(0);
  } catch (err) {
    if (browser) {
      try {
        const pages = browser.contexts()[0]?.pages() || [];
        if (pages[0]) await screenshot(pages[0], 'failure').catch(() => {});
      } catch { /* best-effort */ }
      await browser.close().catch(() => {});
    }
    await saveLog('error', err.stack || err.message).catch(() => {});
    console.error('FAIL:', err.message);
    process.exit(1);
  }
}
