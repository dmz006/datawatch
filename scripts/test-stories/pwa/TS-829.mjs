// TS-829 — PWA Settings: the "Stuck Automaton timeout" field
//
// The Automata settings tab carries a number field for
// autonomous.stuck_running_seconds (the stuck-running watchdog bound, default
// 900). This story drives it in the browser and checks the effect on the
// daemon:
//   - the field exists with its label, the 900 placeholder and a help title,
//   - typing a value and leaving the field makes GET /api/config report it,
//   - clearing the field reads back 0 (the default applies at read time).
// Runs against the sandbox daemon only (TEST_HTTP / TEST_TOKEN). The original
// value is restored at the end.
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

const KEY = 'autonomous.stuck_running_seconds';
const headers = { 'Content-Type': 'application/json', Authorization: `Bearer ${env.token}` };

async function readBound() {
  const r = await api('/api/config', { headers });
  if (!r.ok) throw new Error(`GET /api/config -> ${r.status}`);
  const cfg = await r.json();
  return Number((cfg.autonomous && cfg.autonomous.stuck_running_seconds) || 0);
}
async function writeBound(v) {
  const r = await api('/api/config', { method: 'PUT', headers, body: JSON.stringify({ [KEY]: v }) });
  if (!r.ok) throw new Error(`PUT /api/config ${KEY}=${v} -> ${r.status}`);
}
async function waitFor(fn, what, timeout = 8000) {
  const end = Date.now() + timeout;
  while (Date.now() < end) {
    if (await fn()) return;
    await new Promise((r) => setTimeout(r, 250));
  }
  throw new Error(`timed out waiting for ${what}`);
}

await runStory(async (page) => {
  const original = await readBound();
  try {
    await writeBound(900);

    // Open Settings on the Automata tab.
    await page.goto(env.http, { waitUntil: 'domcontentloaded' });
    await page.evaluate(() => localStorage.setItem('cs_settings_tab', 'automata'));
    await connectToPWA(page);
    await navigateTo(page, 'settings');
    await page.evaluate(() => window.switchSettingsTab && window.switchSettingsTab('automata'));

    const sel = '#gcfg-input-autonomous-stuck_running_seconds';
    await page.waitForSelector(sel, { state: 'attached', timeout: 10000 });

    const row = await page.evaluate((s) => {
      const el = document.querySelector(s);
      const row = el.closest('.settings-row');
      const label = row.querySelector('.settings-label');
      return { label: label.textContent.trim(), help: label.getAttribute('title') || '', type: el.type, placeholder: el.placeholder, value: el.value };
    }, sel);
    await saveLog('field-row', JSON.stringify(row));
    await screenshot(page, '01-settings-field');
    if (!/stuck/i.test(row.label)) throw new Error(`field label does not mention stuck: "${row.label}"`);
    if (row.type !== 'number') throw new Error(`field type = ${row.type}, want number`);
    if (!/watchdog/i.test(row.help)) throw new Error(`field help title missing or wrong: "${row.help}"`);
    if (row.value !== '900') throw new Error(`field value = "${row.value}", want 900 from the config`);

    // Type a new bound; leaving the field fires the change handler.
    await page.fill(sel, '1234');
    await page.dispatchEvent(sel, 'change');
    await waitFor(async () => (await readBound()) === 1234, `${KEY} to read 1234 after the edit`);
    await screenshot(page, '02-settings-edited');

    // Clear it: 0 means "use the default (900)" at read time.
    await page.fill(sel, '0');
    await page.dispatchEvent(sel, 'change');
    await waitFor(async () => (await readBound()) === 0, `${KEY} to read 0 after clearing`);
    await screenshot(page, '03-settings-cleared');
    await saveLog('result', 'stuck_running_seconds field rendered and round-tripped through GET/PUT /api/config');
  } finally {
    await writeBound(original).catch(() => {});
  }
});
