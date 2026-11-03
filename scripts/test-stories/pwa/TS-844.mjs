// TS-844 — PWA Sessions list: the Stalled state chip, localized and filtering
//
// The Sessions list state filter carries a chip for the stalled session state
// whose label is the session_chip_stalled locale key. This story drives it in
// the browser against the sandbox daemon:
//   - a shell session is started through the API and set to the stalled state,
//   - the Stalled chip renders with the English session_chip_stalled text,
//   - after the UI language is switched to another locale the chip shows that
//     locale's session_chip_stalled text (different from English),
//   - clicking the chip filters the list to sessions in the stalled state, and
//     a second, running session is no longer listed.
// Runs against the sandbox daemon only (TEST_HTTP / TEST_TOKEN); it refuses
// ports 8080 and 8443, never starts or stops a daemon, and removes the
// sessions it created at the end. Nothing here spawns tmux itself; TMUX and
// TMUX_TMPDIR are dropped from this process so nothing inherits them.
import { runStory, connectToPWA, screenshot, saveLog, env } from './lib.mjs';

delete process.env.TMUX;
delete process.env.TMUX_TMPDIR;

for (const base of [env.http, env.tls]) {
  const port = new URL(base).port;
  if (port === '8080' || port === '8443') {
    throw new Error(`refusing to run against production port ${port}: ${base}`);
  }
}

const headers = { 'Content-Type': 'application/json', Authorization: `Bearer ${env.token}` };

async function waitFor(fn, what, timeout = 10000) {
  const end = Date.now() + timeout;
  while (Date.now() < end) {
    if (await fn()) return;
    await new Promise((r) => setTimeout(r, 250));
  }
  throw new Error(`timed out waiting for ${what}`);
}

await runStory(async (page) => {
  await connectToPWA(page);

  // REST calls go through the page so they use its origin and the sandbox cert.
  const api = (path, body) => page.evaluate(async ({ path, body, headers }) => {
    const r = await fetch(path, { method: 'POST', headers, body: JSON.stringify(body) });
    const text = await r.text();
    let json = {};
    try { json = JSON.parse(text); } catch { /* not JSON */ }
    return { ok: r.ok, status: r.status, json, text };
  }, { path, body, headers });

  const created = [];
  try {
    const start = async (name) => {
      const r = await api('/api/sessions/start', {
        task: 'bash --norc', name, backend: 'shell', project_dir: '/tmp', effort: 'quick',
      });
      const id = r.json.full_id || r.json.id;
      if (!r.ok || !id) throw new Error(`session start ${name} failed: ${r.status} ${r.text}`);
      created.push(id);
      return { id, shortId: r.json.id || id };
    };
    const stalled = await start('ts-844-stalled');
    const running = await start('ts-844-running');

    const set = await api('/api/sessions/state', { id: stalled.id, state: 'stalled' });
    if (!set.ok) throw new Error(`set stalled failed: ${set.status} ${set.text}`);

    // Both sessions reach the client, the first one as stalled.
    await page.waitForFunction(
      (id) => ((typeof state !== 'undefined' && state.sessions) || []).some((s) => (s.full_id === id || s.id === id) && s.state === 'stalled'),
      stalled.id, { timeout: 15000 });

    // Locale bundles from the daemon itself, so the expected text is the real key value.
    const bundle = async (lang) => page.evaluate(async (l) => {
      const r = await fetch(`/locales/${l}.json`);
      const j = await r.json();
      return j.session_chip_stalled;
    }, lang);
    const enText = await bundle('en');
    const deText = await bundle('de');
    if (!enText || !deText || enText === deText) {
      throw new Error(`bundles do not differ: en="${enText}" de="${deText}"`);
    }

    // chipLabel opens the state filter and reads the Stalled chip's text.
    const chipLabel = async () => {
      await page.evaluate(() => {
        state._stateFilterOpen = true;
        state.sessionStateChip = 'all';
        renderSessionsView();
      });
      await page.waitForSelector('.state-filter-chips button[onclick*="\'stalled\'"]', { timeout: 10000 });
      return page.evaluate(() => {
        const b = document.querySelector('.state-filter-chips button[onclick*="\'stalled\'"]');
        return b.textContent.replace(/\d+$/, '').replace('●', '').trim();
      });
    };

    // Open the Sessions list in English.
    await page.evaluate(() => { localStorage.setItem('datawatch.locale', 'en'); });
    await page.reload({ waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof state !== 'undefined' && state.sessions.length > 0, null, { timeout: 20000 });
    const en = await chipLabel();
    await saveLog('chip-en', en);
    await screenshot(page, '01-chip-en');
    if (en !== enText) throw new Error(`Stalled chip label = "${en}", want "${enText}"`);

    // Switch the UI language: the label must change to the German text.
    await page.evaluate(() => { localStorage.setItem('datawatch.locale', 'de'); });
    await page.reload({ waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof state !== 'undefined' && state.sessions.length > 0, null, { timeout: 20000 });
    const de = await chipLabel();
    await saveLog('chip-de', de);
    await screenshot(page, '02-chip-de');
    if (de !== deText) throw new Error(`Stalled chip label after language switch = "${de}", want "${deText}"`);
    if (de === en) throw new Error(`Stalled chip label did not change with the language: "${de}"`);

    // Back to English, then click the chip and check the list.
    await page.evaluate(() => { localStorage.setItem('datawatch.locale', 'en'); });
    await page.reload({ waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof state !== 'undefined' && state.sessions.length > 0, null, { timeout: 20000 });
    await chipLabel();
    await page.click('.state-filter-chips button[onclick*="\'stalled\'"]');
    await waitFor(() => page.evaluate(() => state.sessionStateChip === 'stalled'), 'the stalled chip to become active');

    const listed = await page.evaluate(() => {
      const stateById = new Map((state.sessions || []).map((s) => [s.full_id, s.state]));
      const cards = [...document.querySelectorAll('[data-session-id], [data-full-id], .session-card')];
      return { cards: cards.map((c) => c.textContent.replace(/\s+/g, ' ').trim().slice(0, 120)), states: [...stateById.values()] };
    });
    await saveLog('filtered-list', JSON.stringify(listed));
    await screenshot(page, '03-filtered-stalled');
    const text = listed.cards.join('\n');
    if (!/ts-844-stalled/.test(text)) throw new Error(`stalled session missing from the filtered list: ${text}`);
    if (/ts-844-running/.test(text)) throw new Error(`running session still listed under the Stalled filter: ${text}`);
    // Every listed session card belongs to a session in the stalled state.
    const nonStalled = await page.evaluate(() => {
      const shown = (state.sessions || []).filter((s) => s.state !== 'stalled');
      const body = document.body.innerText;
      return shown.filter((s) => s.name && body.includes(s.name)).map((s) => `${s.name}:${s.state}`);
    });
    if (nonStalled.length) throw new Error(`non-stalled sessions listed under the Stalled filter: ${nonStalled.join(', ')}`);

    await saveLog('result', 'Stalled chip rendered its localized label, changed with the language, and filtered the list');
  } finally {
    await page.evaluate(() => { try { localStorage.removeItem('datawatch.locale'); localStorage.removeItem('cs_session_state_chip'); } catch { /* ignore */ } }).catch(() => {});
    for (const id of created) await api('/api/sessions/delete', { id }).catch(() => {});
  }
});
