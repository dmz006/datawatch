import { chromium } from 'playwright';
import { env } from './lib.mjs';
const browser = await chromium.launch({
  executablePath: '/usr/bin/google-chrome',
  headless: true,
  args: ['--no-sandbox', '--disable-dev-shm-usage', '--disable-gpu', '--ignore-certificate-errors', '--disable-service-worker'],
});
const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport: { width: 1280, height: 800 } });
const page = await context.newPage();
page.on('console', (m) => console.log('CONSOLE[' + m.type() + ']: ' + m.text().slice(0, 150)));
await page.goto(env.http, { waitUntil: 'domcontentloaded' });
await page.evaluate((t) => localStorage.setItem('cs_token', t), env.token);
await page.reload({ waitUntil: 'domcontentloaded' });
await new Promise(r => setTimeout(r, 10000));
const st = await page.evaluate(() => ({
  swSupported: 'serviceWorker' in navigator,
  controller: navigator.serviceWorker.controller,
  banner: !!document.getElementById('updateAvailableBanner'),
  settings: !!document.querySelector('[data-view="settings"]'),
}));
console.log('STATE:', JSON.stringify(st));
await page.click('[data-view="settings"]', { timeout: 5000 });
console.log('CLICK OK');
await page.close(); await browser.close();
