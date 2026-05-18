// TS-148 — PWA: Autonomous nav button visible when autonomous enabled
import { runStory, connectToPWA, screenshot, saveLog } from './lib.mjs';

// Pre-check: verify autonomous is enabled via API before testing the PWA button
const apiBase = process.env.TEST_HTTP || 'http://127.0.0.1:18080';
const token = process.env.TEST_TOKEN || 'dw-test-token-12345';
let autonomousEnabled = false;
try {
  const resp = await fetch(`${apiBase}/api/autonomous/config`, {
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  });
  const data = await resp.json();
  autonomousEnabled = data.enabled === true;
} catch { /* ignore fetch errors */ }

if (!autonomousEnabled) {
  await saveLog('result', 'autonomous disabled in daemon — skipping nav button test');
  process.exit(2);
}

await runStory(async (page) => {
  await connectToPWA(page);

  // JS fetches /api/autonomous/config on boot and unhides the button when enabled:true.
  // Wait up to 10s for the button to become visible.
  try {
    await page.waitForFunction(
      () => {
        const btn = document.getElementById('navBtnAutonomous');
        if (!btn) return false;
        return btn.style.display !== 'none' && window.getComputedStyle(btn).display !== 'none';
      },
      null,
      { timeout: 10000 },
    );
  } catch {
    await screenshot(page, 'autonomous-nav-hidden');
    const display = await page.evaluate(() => {
      const btn = document.getElementById('navBtnAutonomous');
      return btn ? btn.style.display + ' / computed:' + window.getComputedStyle(btn).display : 'not found';
    });
    throw new Error(`Autonomous nav button not visible after 10s (display: ${display})`);
  }

  await screenshot(page, '01-autonomous-nav-visible');
  await saveLog('result', 'navBtnAutonomous visible — PASS');
});
