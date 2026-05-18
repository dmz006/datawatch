// TS-148 — PWA: Autonomous nav button visible when autonomous enabled
import { runStory, connectToPWA, screenshot, saveLog } from './lib.mjs';

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
