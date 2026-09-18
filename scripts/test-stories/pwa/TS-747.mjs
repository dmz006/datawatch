// TS-747 — apiFetch returns null for 204/205 responses (no JSON parse error)
import { runStory, connectToPWA, screenshot, saveLog } from './lib.mjs';

await runStory(async (page) => {
  await connectToPWA(page);
  await screenshot(page, '01-pwa-loaded');

  const result = await page.evaluate(async () => {
    if (typeof apiFetch !== 'function') return { error: 'apiFetch not found' };

    try {
      // GET /api/sessions returns 200 with body — should NOT return null
      const got = await apiFetch('/api/sessions');
      const notNull = got !== null;

      // Verify the 204/205 guard is in the source
      const src = apiFetch.toString();
      const has204guard = src.includes('204') || src.includes('205');

      return { notNull, has204guard, srcSnippet: src.slice(0, 300) };
    } catch (e) {
      return { error: e.message };
    }
  });

  await saveLog('result', JSON.stringify(result, null, 2));
  await screenshot(page, '02-result');

  if (result.error) {
    throw new Error(`apiFetch test failed: ${result.error}`);
  }
  if (!result.notNull) {
    throw new Error('apiFetch returned null for a 200 response — guard is too broad');
  }
  if (!result.has204guard) {
    throw new Error('apiFetch source does not contain 204/205 guard — line 471 guard missing');
  }

  await saveLog('pass', `apiFetch 204/205 guard present; 200 response not null`);
});
