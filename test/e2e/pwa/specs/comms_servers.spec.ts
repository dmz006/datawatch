// Settings → Comms → Remote Servers card and Federation access controls.

import { test, expect, Page } from '@playwright/test';

async function openCommsTab(page: Page) {
  await page.goto('/');
  await page.click('[data-nav="settings"], #navSettings, button[aria-label*="Settings"]');
  // Switch to Comms tab.
  await page.click('button:has-text("Comms"), [data-tab="comms"]');
  await page.waitForTimeout(500);
}

test.describe('Settings Comms tab — Remote Servers card', () => {
  test('Remote Servers card is visible under Comms tab', async ({ page }) => {
    await openCommsTab(page);
    await expect(page.locator('text=Remote Servers')).toBeVisible();
  });

  test('Add Server form opens from Remote Servers card', async ({ page }) => {
    await openCommsTab(page);
    await page.click('button:has-text("Add"), text=Add Server');
    // The server add form or panel should appear.
    const form = page.locator('#serverAddForm, #serverAddPanel, form:has(#newServerName)');
    await expect(form).toBeVisible({ timeout: 3000 });
  });

  test('Federated toggle shows capabilities input', async ({ page }) => {
    await openCommsTab(page);
    await page.click('button:has-text("Add"), text=Add Server');
    // Federated toggle.
    const fedToggle = page.locator('#newServerFederated, input[id*="Federated"]');
    await fedToggle.check();
    await page.waitForTimeout(300);
    // Capabilities input should appear.
    const capsInput = page.locator('#newServerCapabilities, input[id*="Capabilities"], textarea[id*="capabilities"]');
    await expect(capsInput).toBeVisible();
  });
});

test.describe('Mobile: fullscreen button hidden', () => {
  test.use({ viewport: { width: 375, height: 812 } });

  test('fullscreen button not visible on narrow display', async ({ page }) => {
    await page.goto('/');
    const btn = page.locator('#headerFullscreenBtn');
    // Either not in DOM or not visible (CSS display:none).
    const count = await btn.count();
    if (count > 0) {
      await expect(btn).not.toBeVisible();
    }
    // If count === 0, it was not rendered at all — also pass.
  });
});
