// vault-nav-label-clear — at a 390 px viewport with the Vault status forced
// unreachable, nothing but the Settings nav button and its label sits over the
// label centre, and the retired #vaultUnreachableBadge element does not exist.
// Runs against the sandbox daemon (TLS, never the production port).
import { env, runStory, connectToPWA, screenshot } from './lib.mjs';

await runStory(async (page) => {
  await page.setViewportSize({ width: 390, height: 844 });

  // Force the Vault status unreachable before the app loads.
  const unreachable = JSON.stringify({
    backend_active: true,
    status: { reachable: false, last_error: 'mocked unreachable', last_success: '' },
  });
  await page.route('**/api/secrets/vault/status', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: unreachable }));

  await connectToPWA(page, { base: env.tls });
  // Let any status poll / badge update run.
  await page.waitForTimeout(1500);
  await screenshot(page, '01-390px-vault-unreachable');

  const res = await page.evaluate(() => {
    const btn = document.querySelector('.nav-btn[data-view="settings"]');
    const label = btn && btn.querySelector('.nav-label');
    if (!btn || !label) return { error: 'settings nav button or label missing' };
    const r = label.getBoundingClientRect();
    const x = r.left + r.width / 2;
    const y = r.top + r.height / 2;
    const stack = document.elementsFromPoint(x, y);
    const describe = (e) => `${e.tagName.toLowerCase()}${e.id ? '#' + e.id : ''}${e.className && typeof e.className === 'string' ? '.' + e.className.trim().split(/\s+/).join('.') : ''}`;
    // Anything above the label that is neither the label nor the button.
    const above = [];
    for (const e of stack) {
      if (e === label) break;
      if (e !== btn) above.push(describe(e));
    }
    return {
      vw: window.innerWidth,
      point: [x, y],
      stack: stack.map(describe),
      above,
      labelInStack: stack.includes(label),
      badgeCount: document.querySelectorAll('#vaultUnreachableBadge').length,
    };
  });

  if (res.error) throw new Error(res.error);
  if (res.vw !== 390) throw new Error(`expected 390 px viewport, got ${res.vw}`);
  if (!res.labelInStack) throw new Error(`Settings nav label not hit-testable at ${res.point}: ${res.stack.join(' > ')}`);
  if (res.above.length) throw new Error(`elements over the Settings nav label: ${res.above.join(', ')}`);
  if (res.badgeCount !== 0) throw new Error(`#vaultUnreachableBadge exists (${res.badgeCount})`);
  console.log(`PASS: Settings label clear at ${res.point.map(Math.round)}; stack ${res.stack.join(' > ')}`);
});
