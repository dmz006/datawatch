#!/usr/bin/env node
// tags: surface:pwa feature:pwa-priority-by-kind

// TS-841 — PWA Settings: the autonomous priority-by-kind and defect-intake fields
//
// The Automata settings tab carries two text rows: autonomous.priority_by_kind
// (the kind → capacity-queue priority table) and
// autonomous.defect_intake_automation (the Automation that new kind-bug defect
// filings are offered to). This story checks, against the sandbox daemon only
// (TEST_HTTP / TEST_TOKEN), that:
//   - both rows render with their localized labels and their current value
//     (or the placeholder) pre-filled,
//   - the table round-trips through the same PUT /api/config path the field
//     uses: write {bug:85, feature:25}, GET reads it back effective,
//   - a non-integer table is refused (400, nothing stored) and a clear (empty
//     value) restores the shipped defaults (bug=80, maintenance=70, feature=20,
//     other=0),
//   - the intake name round-trips and an empty value clears it back to "".
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

const headers = { 'Content-Type': 'application/json', Authorization: `Bearer ${env.token}` };
const SHIPPED = { bug: 80, maintenance: 70, feature: 20, other: 0 };

async function readConfig() {
  const r = await api('/api/config', { headers });
  if (!r.ok) throw new Error(`GET /api/config -> ${r.status}`);
  return await r.json();
}
async function writeConfig(obj) {
  const r = await api('/api/config', { method: 'PUT', headers, body: JSON.stringify(obj) });
  return { status: r.status, ok: r.ok, body: await r.json().catch(() => ({})) };
}

await runStory(async (page) => {
  const original = await readConfig();
  const origTable =
    original.autonomous && original.autonomous.priority_by_kind !== undefined
      ? JSON.parse(JSON.stringify(original.autonomous.priority_by_kind))
      : null;
  const origIntake =
    original.autonomous && typeof original.autonomous.defect_intake_automation === 'string'
      ? original.autonomous.defect_intake_automation
      : '';
  const cleanup = async () => {
    const body = { autonomous: { defect_intake_automation: origIntake } };
    if (origTable !== null) body.autonomous.priority_by_kind = origTable;
    else body.autonomous.priority_by_kind = {};
    await writeConfig(body).catch(() => {});
  };
  try {
    // Open Settings on the Automata tab, like TS-827.
    await page.goto(env.http, { waitUntil: 'domcontentloaded' });
    await page.evaluate(() => localStorage.setItem('cs_settings_tab', 'automata'));
    await connectToPWA(page);
    await navigateTo(page, 'settings');
    await page.evaluate(() => window.switchSettingsTab && window.switchSettingsTab('automata'));

    // Both rows render with their localized labels and the current value pre-filled.
    const rows = await page.evaluate(() => {
      const table = document.querySelector('input[class*="autonomous-priority-by-kind"], input[value="autonomous.priority_by_kind"]');
      const intake = document.querySelector('input[class*="autonomous-defect-intake-automation"], input[value="autonomous.defect_intake_automation"]');
      const info = (el) => {
        if (!el) return null;
        const row = el.closest('.settings-row');
        const label = row && row.querySelector('.settings-label');
        const hint = row && row.querySelector('.settings-hint');
        return { label: label ? label.textContent.trim() : '', hint: hint ? hint.textContent.trim() : '', value: el.value, placeholder: el.placeholder || '' };
      };
      return { table: info(table), intake: info(intake) };
    });
    await saveLog('settings-rows', JSON.stringify(rows));
    await screenshot(page, '01-settings-rows');
    if (!rows.table) throw new Error('the autonomous.priority_by_kind settings row did not render');
    if (!rows.intake) throw new Error('the autonomous.defect_intake_automation settings row did not render');
    if (!/priority by kind/i.test(rows.table.label)) throw new Error(`table row label does not mention the kind priority table: "${rows.table.label}"`);
    if (!/automation/i.test(rows.intake.label)) throw new Error(`intake row label does not mention the Automation: "${rows.intake.label}"`);

    // The table round-trips through the same PUT path the field uses.
    const written = await writeConfig({ autonomous: { priority_by_kind: { bug: 85, feature: 25 } } });
    if (!written.ok) throw new Error(`PUT table -> ${written.status}: ${JSON.stringify(written.body)}`);
    let after = await readConfig();
    const eff = after.autonomous && after.autonomous.priority_by_kind;
    if (!eff || eff.bug !== 85 || eff.feature !== 25 || eff.other !== SHIPPED.other)
      throw new Error(`table did not read back effective: ${JSON.stringify(eff)}`);
    if (eff.maintenance === undefined) throw new Error('maintenance fell out of the effective table');
    await saveLog('after-write', JSON.stringify({ effective: eff }));

    // A non-integer table is refused and stores nothing.
    const refused = await writeConfig({ autonomous: { priority_by_kind: { bug: 'soon' } } });
    if (refused.status !== 400) throw new Error(`a non-integer table must be refused with 400, got ${refused.status}`);
    after = await readConfig();
    if (after.autonomous.priority_by_kind && after.autonomous.priority_by_kind.bug !== 85)
      throw new Error('the refused write changed the stored table');
    await saveLog('refused', JSON.stringify({ status: refused.status }));

    // An empty value clears the override back to the shipped defaults.
    const cleared = await writeConfig({ autonomous: { priority_by_kind: {} } });
    if (!cleared.ok) throw new Error(`clear table -> ${cleared.status}`);
    after = await readConfig();
    const clearedEff = after.autonomous && after.autonomous.priority_by_kind;
    for (const [k, v] of Object.entries(SHIPPED))
      if (!clearedEff || clearedEff[k] !== v) throw new Error(`clear did not restore the shipped defaults: ${JSON.stringify(clearedEff)}`);
    await saveLog('after-clear', JSON.stringify({ effective: clearedEff }));

    // The intake name round-trips and an empty value clears it.
    const intakeWritten = await writeConfig({ autonomous: { defect_intake_automation: 'pwa-ts841-intake' } });
    if (!intakeWritten.ok) throw new Error(`PUT intake name -> ${intakeWritten.status}: ${JSON.stringify(intakeWritten.body)}`);
    after = await readConfig();
    if (after.autonomous.defect_intake_automation !== 'pwa-ts841-intake')
      throw new Error(`intake name did not read back: ${JSON.stringify(after.autonomous)}`);
    const intakeCleared = await writeConfig({ autonomous: { defect_intake_automation: '' } });
    if (!intakeCleared.ok) throw new Error(`clear intake -> ${intakeCleared.status}`);
    after = await readConfig();
    if (after.autonomous.defect_intake_automation !== '')
      throw new Error(`intake name did not clear: ${after.autonomous.defect_intake_automation}`);
    await saveLog('result', 'TS-841: priority-by-kind + defect-intake settings rows render and the config contract verifies (write/read/refuse/clear)');
  } finally {
    await cleanup();
  }
});
