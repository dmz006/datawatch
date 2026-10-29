// TS-833 — PWA Settings: the operator-question notification toggles
//
// The Automata settings tab carries three toggles under the "Operator
// questions for Automata" switch, all on by default:
//   autonomous.questions.notify_messaging, .notify_alert and .return_path.
// This story drives each one in the browser and checks the effect on the
// daemon:
//   - every toggle row exists with a translated label (not the raw locale
//     key) and renders checked when the key is at its default,
//   - a click turns the toggle off and GET /api/config reports false, and a
//     reload shows it unchecked,
//   - a second click turns it back on and GET /api/config reports true.
// Each toggle is changed alone, so the other two keep their value while one
// is off. The story creates nothing; the three original values are restored
// in a finally block. Runs against the sandbox daemon only
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

const KEYS = [
  'autonomous.questions.notify_messaging',
  'autonomous.questions.notify_alert',
  'autonomous.questions.return_path',
];
const headers = { 'Content-Type': 'application/json', Authorization: `Bearer ${env.token}` };

// readAll answers the effective value of each toggle; an absent key is the on default.
async function readAll() {
  const r = await api('/api/config', { headers });
  if (!r.ok) throw new Error(`GET /api/config -> ${r.status}`);
  const q = ((await r.json()).autonomous || {}).questions || {};
  const out = {};
  for (const k of KEYS) {
    const v = q[k.split('.').pop()];
    out[k] = v === undefined || v === null ? true : !!v;
  }
  return out;
}
async function writeAll(values) {
  const r = await api('/api/config', { method: 'PUT', headers, body: JSON.stringify(values) });
  if (!r.ok) throw new Error(`PUT /api/config ${Object.keys(values).join(',')} -> ${r.status}`);
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
  await page.waitForSelector(`input[type="checkbox"][onchange*="${KEYS[0]}"]`, { state: 'attached', timeout: 15000 });
}

const selFor = (key) => `input[type="checkbox"][onchange*="${key}"]`;
const rowOf = (page, key) => page.evaluate((s) => {
  const el = document.querySelector(s);
  if (!el) return null;
  const row = el.closest('.settings-row');
  return { label: row.querySelector('.settings-label').textContent.trim(), checked: el.checked };
}, selFor(key));

await runStory(async (page) => {
  const original = await readAll();
  try {
    // Start from the defaults (all on) so each click sequence is deterministic.
    await writeAll(Object.fromEntries(KEYS.map((k) => [k, true])));
    await openAutomataSettings(page);

    const rows = {};
    for (const key of KEYS) {
      const row = await rowOf(page, key);
      if (!row) throw new Error(`no settings row for ${key}`);
      if (!row.label || /^settings_/.test(row.label)) throw new Error(`label for ${key} is not translated: "${row.label}"`);
      if (!row.checked) throw new Error(`${key} renders unchecked while it is at its on default`);
      rows[key] = row;
    }
    await saveLog('toggle-rows', JSON.stringify(rows));
    await screenshot(page, '01-settings-defaults-on');

    for (const [i, key] of KEYS.entries()) {
      const n = String(i + 2).padStart(2, '0');
      // Click off: this key reads false, the other two are untouched.
      await page.evaluate((s) => document.querySelector(s).click(), selFor(key));
      await waitFor(async () => (await readAll())[key] === false, `${key} to read false after the click`);
      const afterOff = await readAll();
      for (const other of KEYS.filter((k) => k !== key)) {
        if (!afterOff[other]) throw new Error(`turning ${key} off also turned ${other} off`);
      }
      await openAutomataSettings(page);
      const off = await rowOf(page, key);
      if (off.checked) throw new Error(`${key} still renders checked after a reload while the daemon reads false`);
      await screenshot(page, `${n}-${key.split('.').pop()}-off`);

      // Click on: back to the default.
      await page.evaluate((s) => document.querySelector(s).click(), selFor(key));
      await waitFor(async () => (await readAll())[key] === true, `${key} to read true after the second click`);
    }
    await saveLog('result', 'each notification toggle verified off and on against GET /api/config and a reload');
  } finally {
    await writeAll(original).catch(() => {});
  }
});
