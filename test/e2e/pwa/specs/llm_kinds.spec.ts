// TS-392–TS-393 — PWA LLM kind options (gemini-api, opencode-api visible in ComputeNode form).

import { test, expect, Page } from '@playwright/test';

async function openAddComputeNodePanel(page: Page) {
  await page.goto('/');
  await page.click('[data-nav="settings"], #navSettings, button[aria-label*="Settings"]');
  await page.click('text=Compute Nodes');
  await page.click('text=Add ComputeNode, button:has-text("Add")');
  await page.waitForSelector('#computeAddPanel', { state: 'visible' });
}

test.describe('ComputeNode Kind dropdown — new kinds', () => {
  test('TS-392: gemini-api is available in kind select', async ({ page }) => {
    await openAddComputeNodePanel(page);
    const options = await page.locator('#computeNewKind option').allTextContents();
    expect(options).toContain('gemini-api');
  });

  test('TS-393: opencode-api is available in kind select', async ({ page }) => {
    await openAddComputeNodePanel(page);
    const options = await page.locator('#computeNewKind option').allTextContents();
    expect(options).toContain('opencode-api');
  });

  test('TS-392+393: can select gemini-api without JS error', async ({ page }) => {
    const errors: string[] = [];
    page.on('console', msg => { if (msg.type() === 'error') errors.push(msg.text()); });

    await openAddComputeNodePanel(page);
    await page.selectOption('#computeNewKind', 'gemini-api');
    await page.waitForTimeout(300);

    const jsErrors = errors.filter(e => !e.includes('favicon') && !e.includes('net::ERR'));
    expect(jsErrors).toHaveLength(0);
  });
});
