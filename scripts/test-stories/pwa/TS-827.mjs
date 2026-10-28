// TS-827 — PWA Settings: the "Operator questions for Automata" toggle
//
// The Automata settings tab carries a toggle for autonomous.questions.enabled
// (off by default). This story clicks it in the browser and checks the
// effect on the daemon:
//   - the toggle row exists with its label and starts unchecked (or at the
//     value the sandbox already holds, which is restored at the end),
//   - turning it on makes GET /api/config report autonomous.questions.enabled
//     true and GET /api/questions answer 200 instead of 503,
//   - turning it off again reports false and /api/questions answers 503.
// Runs against the sandbox daemon only (TEST_HTTP / TEST_TOKEN).
import { runStory, connectToPWA, navigateTo, screenshot, saveLog, env } from './lib.mjs';

// The sandbox HTTP port redirects to TLS (307) and a redirect drops the
// Authorization header, so REST calls go straight to the self-signed TLS port.
process.env.NODE_TLS_REJECT_UNAUTHORIZED = '0';
const API = env.tls;

const KEY = 'autonomous.questions.enabled';
const headers = { 'Content-Type': 'application/json', Authorization: `Bearer ${env.token}` };

async function readSwitch() {
  const r = await fetch(`${API}/api/config`, { headers });
  if (!r.ok) throw new Error(`GET /api/config -> ${r.status}`);
  const cfg = await r.json();
  return !!(cfg.autonomous && cfg.autonomous.questions && cfg.autonomous.questions.enabled);
}
async function writeSwitch(v) {
  const r = await fetch(`${API}/api/config`, { method: 'PUT', headers, body: JSON.stringify({ [KEY]: v }) });
  if (!r.ok) throw new Error(`PUT /api/config ${KEY}=${v} -> ${r.status}`);
}
async function questionsStatus() {
  const r = await fetch(`${API}/api/questions`, { headers });
  return r.status;
}
async function waitFor(fn, what, timeout = 8000) {
  const end = Date.now() + timeout;
  let last;
  while (Date.now() < end) {
    last = await fn();
    if (last) return last;
    await new Promise((r) => setTimeout(r, 250));
  }
  throw new Error(`timed out waiting for ${what}`);
}

await runStory(async (page) => {
  const original = await readSwitch();
  try {
    // Start from off so the click sequence is deterministic.
    await writeSwitch(false);

    // Open Settings on the Automata tab.
    await page.goto(env.http, { waitUntil: 'domcontentloaded' });
    await page.evaluate(() => localStorage.setItem('cs_settings_tab', 'automata'));
    await connectToPWA(page);
    await navigateTo(page, 'settings');
    await page.evaluate(() => window.switchSettingsTab && window.switchSettingsTab('automata'));

    const sel = `input[type="checkbox"][onchange*="${KEY}"]`;
    await page.waitForSelector(sel, { state: 'attached', timeout: 10000 });

    const row = await page.evaluate((s) => {
      const el = document.querySelector(s);
      const row = el.closest('.settings-row');
      return { label: row.querySelector('.settings-label').textContent.trim(), checked: el.checked };
    }, sel);
    await saveLog('toggle-row', JSON.stringify(row));
    await screenshot(page, '01-settings-off');
    if (!/question/i.test(row.label)) throw new Error(`toggle label does not mention questions: "${row.label}"`);
    if (row.checked) throw new Error('toggle renders checked while the switch is off');
    if ((await questionsStatus()) !== 503) throw new Error('/api/questions should answer 503 while the switch is off');

    // Click on.
    await page.evaluate((s) => document.querySelector(s).click(), sel);
    await waitFor(readSwitch, `${KEY} to read true after the click`);
    const onStatus = await waitFor(async () => ((await questionsStatus()) === 200 ? 200 : 0), '/api/questions to answer 200');
    await screenshot(page, '02-settings-on');
    await saveLog('after-on', JSON.stringify({ config: true, questions: onStatus }));

    // Click off.
    await page.evaluate((s) => document.querySelector(s).click(), sel);
    await waitFor(async () => !(await readSwitch()), `${KEY} to read false after the second click`);
    await waitFor(async () => ((await questionsStatus()) === 503 ? 503 : 0), '/api/questions to answer 503 again');
    await screenshot(page, '03-settings-off-again');
    await saveLog('result', 'toggle on/off verified against GET /api/config and /api/questions');
  } finally {
    await writeSwitch(original).catch(() => {});
  }
});
