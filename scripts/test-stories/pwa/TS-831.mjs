// TS-831 — PWA Settings: the Detection Filters card shows the built-in per-backend default
//
// With no operator rate-limit list, the daemon applies its built-in per-backend
// table. The Settings -> Detection Filters card must say so instead of listing
// a flat set of phrases as the "defaults". This story checks, in the browser:
//   - with detection.rate_limit_patterns empty, the Rate Limit Patterns section
//     shows the translated "built-in table" badge and the hint, lists no phrase
//     rows, and offers no remove button,
//   - after an operator list is saved through the API, the section shows the
//     custom entry with the "1 custom" badge and no hint (so the check is not
//     passing on a card that never changes).
// The story changes one config value and restores the original list in a
// finally block. Runs against the sandbox daemon only (TEST_HTTP / TEST_TOKEN).
import { runStory, connectToPWA, navigateTo, screenshot, saveLog, env } from './lib.mjs';

// The sandbox HTTP port redirects to TLS (307) and a redirect drops the
// Authorization header, so REST calls go straight to the self-signed TLS port.
// The relaxed certificate check lives on this dedicated Agent, not on the whole process.
import https from 'node:https';
const sandboxAgent = new https.Agent({ rejectUnauthorized: false });
const API = env.tls;

// api issues one REST call to the sandbox TLS port and answers { ok, status, json() } like fetch.
function api(path, { method = 'GET', headers: h = {}, body } = {}) {
  return new Promise((resolve, reject) => {
    if (body) h = { ...h, 'Content-Length': Buffer.byteLength(body) };
    const req = https.request(new URL(path, API), { method, headers: h, agent: sandboxAgent }, (res) => {
      const chunks = [];
      res.on('data', (c) => chunks.push(c));
      res.on('end', () => {
        const text = Buffer.concat(chunks).toString('utf8');
        resolve({ ok: res.statusCode >= 200 && res.statusCode < 300, status: res.statusCode, json: async () => JSON.parse(text || '{}'), text: async () => text });
      });
    });
    req.on('error', reject);
    if (body) req.write(body);
    req.end();
  });
}

const KEY = 'detection.rate_limit_patterns';
const CUSTOM = 'ts831 custom phrase';
const headers = { 'Content-Type': 'application/json', Authorization: `Bearer ${env.token}` };

async function readList() {
  const r = await api('/api/config', { headers });
  if (!r.ok) throw new Error(`GET /api/config -> ${r.status}`);
  const cfg = await r.json();
  return (cfg.detection && cfg.detection.rate_limit_patterns) || [];
}
async function writeList(list) {
  const r = await api('/api/config', { method: 'PUT', headers, body: JSON.stringify({ [KEY]: list }) });
  if (!r.ok) throw new Error(`PUT /api/config ${KEY} -> ${r.status}`);
}

// openDetectionCard loads the PWA fresh and returns the Rate Limit Patterns section.
async function openDetectionCard(page) {
  await page.goto(env.http, { waitUntil: 'domcontentloaded' });
  await page.evaluate(() => localStorage.setItem('cs_settings_tab', 'compute'));
  await connectToPWA(page);
  await navigateTo(page, 'settings');
  await page.evaluate(() => window.switchSettingsTab && window.switchSettingsTab('compute'));
  await page.waitForSelector('#detectionFiltersList .det-item, #detectionFiltersList .det-builtin-hint', { state: 'attached', timeout: 15000 });
  return page.evaluate(() => {
    const root = document.getElementById('detectionFiltersList');
    const block = [...root.children].find((c) => c.textContent.includes('Rate Limit Patterns'));
    if (!block) return null;
    return {
      text: block.textContent,
      hint: !!block.querySelector('.det-builtin-hint'),
      hintText: (block.querySelector('.det-builtin-hint') || {}).textContent || '',
      rows: block.querySelectorAll('.det-item').length,
      removeButtons: (block.innerHTML.match(/removeDetPattern\('rate_limit_patterns'/g) || []).length,
    };
  });
}

await runStory(async (page) => {
  const original = await readList();
  try {
    // Start from the shipped default: no operator list.
    await writeList([]);

    const def = await openDetectionCard(page);
    await saveLog('default-card', JSON.stringify(def));
    await screenshot(page, '01-detection-default');
    if (!def) throw new Error('Rate Limit Patterns section not rendered');
    if (!def.hint) throw new Error('built-in table hint missing with no operator list');
    if (/^settings_/.test(def.hintText.trim())) throw new Error(`hint is not translated: "${def.hintText}"`);
    if (!/built-in/i.test(def.text)) throw new Error('section does not say the built-in table applies');
    if (def.rows !== 0) throw new Error(`expected no phrase rows with no operator list, saw ${def.rows}`);
    if (def.removeButtons !== 0) throw new Error(`expected no remove buttons, saw ${def.removeButtons}`);
    if (/\d+ defaults/.test(def.text)) throw new Error('the "N defaults" count must not appear for the built-in table');

    // An operator list replaces the built-in table and is shown as custom.
    await writeList([CUSTOM]);
    const custom = await openDetectionCard(page);
    await saveLog('custom-card', JSON.stringify(custom));
    await screenshot(page, '02-detection-custom');
    if (!custom.text.includes(CUSTOM)) throw new Error('operator entry not shown');
    if (!custom.text.includes('1 custom')) throw new Error('"1 custom" badge missing');
    if (custom.hint) throw new Error('built-in hint must go away once an operator list is set');
    if (custom.rows !== 1) throw new Error(`expected 1 row, saw ${custom.rows}`);

    await saveLog('result', 'detection card verified for the built-in default and for an operator list');
  } finally {
    await writeList(original).catch(() => {});
  }
});
