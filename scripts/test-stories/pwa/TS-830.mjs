// TS-830 — PWA Settings: the Automata "Descendant stall timeout" field
//
// The Automata settings tab carries a number field for
// autonomous.descendant_stall_seconds. This story edits it in the browser and
// checks the effect on the daemon:
//   - the field renders with a translated label (not the raw locale key) and
//     the 2700 placeholder,
//   - typing a value and leaving the field makes GET /api/config report it,
//   - after a reload the field shows the saved value.
// The story creates nothing; it changes one config value and restores the
// original in a finally block. Runs against the sandbox daemon only
// (TEST_HTTP / TEST_TOKEN).
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

const KEY = 'autonomous.descendant_stall_seconds';
const TYPED = 1234;
const headers = { 'Content-Type': 'application/json', Authorization: `Bearer ${env.token}` };

async function readValue() {
  const r = await api('/api/config', { headers });
  if (!r.ok) throw new Error(`GET /api/config -> ${r.status}`);
  const cfg = await r.json();
  return cfg.autonomous ? cfg.autonomous.descendant_stall_seconds : undefined;
}
async function writeValue(v) {
  const r = await api('/api/config', { method: 'PUT', headers, body: JSON.stringify({ [KEY]: v }) });
  if (!r.ok) throw new Error(`PUT /api/config ${KEY}=${v} -> ${r.status}`);
}
async function waitFor(fn, what, timeout = 8000) {
  const end = Date.now() + timeout;
  while (Date.now() < end) {
    const last = await fn();
    if (last) return last;
    await new Promise((r) => setTimeout(r, 250));
  }
  throw new Error(`timed out waiting for ${what}`);
}

async function openAutomataSettings(page) {
  await page.goto(env.http, { waitUntil: 'domcontentloaded' });
  await page.evaluate(() => localStorage.setItem('cs_settings_tab', 'automata'));
  await connectToPWA(page);
  await navigateTo(page, 'settings');
  await page.evaluate(() => window.switchSettingsTab && window.switchSettingsTab('automata'));
  const sel = `input[type="number"][onchange*="${KEY}"]`;
  await page.waitForSelector(sel, { state: 'attached', timeout: 15000 });
  return sel;
}

await runStory(async (page) => {
  const original = await readValue();
  try {
    // Start from the default so the typed value is a real change.
    await writeValue(0);

    let sel = await openAutomataSettings(page);
    const field = await page.evaluate((s) => {
      const el = document.querySelector(s);
      const row = el.closest('.settings-row');
      return { label: row.querySelector('.settings-label').textContent.trim(), placeholder: el.placeholder, value: el.value };
    }, sel);
    await saveLog('field', JSON.stringify(field));
    await screenshot(page, '01-settings-default');
    if (!field.label || /^settings_/.test(field.label)) throw new Error(`field label is not translated: "${field.label}"`);
    if (!/stall/i.test(field.label)) throw new Error(`field label does not mention the stall bound: "${field.label}"`);
    if (field.placeholder !== '2700') throw new Error(`placeholder is "${field.placeholder}", want 2700`);

    // Type a value and leave the field: the change handler saves it.
    await page.fill(sel, String(TYPED));
    await page.dispatchEvent(sel, 'change');
    await waitFor(async () => (await readValue()) === TYPED, `${KEY} to read ${TYPED} after the edit`);
    await screenshot(page, '02-settings-edited');

    // A reload shows the saved value in the field.
    sel = await openAutomataSettings(page);
    await waitFor(
      () => page.evaluate(([s, want]) => document.querySelector(s).value === String(want), [sel, TYPED]),
      `the field to show ${TYPED} after a reload`,
    );
    await screenshot(page, '03-settings-after-reload');
    await saveLog('result', `${KEY} edit verified against GET /api/config and a reload`);
  } finally {
    await writeValue(Number.isInteger(original) ? original : 0).catch(() => {});
  }
});
