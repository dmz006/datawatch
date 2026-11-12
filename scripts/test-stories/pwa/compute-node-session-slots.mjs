// PWA compute node form: the session_slots override.
// Structural checks on app.js and en.json, then a live check on the sandbox
// daemon: the override saves through REST, a negative value is refused, and a
// blank form field saves 0. Runs against the sandbox only (never port 8443).
import { runStory, connectToPWA, saveLog, env } from './lib.mjs';
import { readFile } from 'node:fs/promises';

const WEB = new URL('../../../internal/server/web/', import.meta.url).pathname;
const NODE = 'ts-slots-probe';

if (/:8443(\/|$)/.test(env.http)) {
  throw new Error(`refusing to run against production port 8443: ${env.http}`);
}

await runStory(async (page) => {
  const appSrc = await readFile(WEB + 'app.js', 'utf8');
  const en = JSON.parse(await readFile(WEB + 'locales/en.json', 'utf8'));

  const hasField   = appSrc.includes('fe_compute_session_slots');
  const hasLabel   = appSrc.includes("compute_field_session_slots");
  const hasCollect = appSrc.includes("out.session_slots = parseInt((document.getElementById('fe_compute_session_slots')");
  const hasKey     = typeof en.compute_field_session_slots === 'string';

  await saveLog('structural-check', JSON.stringify({ hasField, hasLabel, hasCollect, hasKey }));
  if (!hasField)   throw new Error('fe_compute_session_slots input not found in app.js');
  if (!hasLabel)   throw new Error('compute_field_session_slots label key not used in app.js');
  if (!hasCollect) throw new Error('collectComputeNodeForm does not read session_slots');
  if (!hasKey)     throw new Error('compute_field_session_slots missing from locales/en.json');

  await connectToPWA(page);

  const result = await page.evaluate(async ({ name, token }) => {
    const call = async (method, path, body) => {
      const r = await fetch(path, {
        method,
        headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + token },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      const text = await r.text();
      return { status: r.status, text };
    };
    const base = { name, kind: 'ollama', address: 'http://127.0.0.1:1/' };
    const url = `/api/compute/nodes/${encodeURIComponent(name)}?probe=skip`;

    const created = await call('POST', '/api/compute/nodes?probe=skip', { ...base, session_slots: 3 });
    const got = await call('GET', `/api/compute/nodes/${encodeURIComponent(name)}`);
    const negative = await call('PUT', url, { ...base, session_slots: -1 });
    const blank = await call('PUT', url, { ...base });
    const after = await call('GET', `/api/compute/nodes/${encodeURIComponent(name)}`);
    await call('DELETE', `/api/compute/nodes/${encodeURIComponent(name)}`);
    return {
      created: created.status,
      savedSlots: got.status === 200 ? JSON.parse(got.text).session_slots : null,
      negativeStatus: negative.status,
      blankStatus: blank.status,
      afterBlankSlots: after.status === 200 ? (JSON.parse(after.text).session_slots ?? 0) : null,
    };
  }, { name: NODE, token: env.token });

  await saveLog('live-check', JSON.stringify(result));
  if (result.created !== 200) throw new Error(`PUT with session_slots 3 returned ${result.created}`);
  if (result.savedSlots !== 3) throw new Error(`saved session_slots = ${result.savedSlots}, want 3`);
  if (result.negativeStatus !== 400) throw new Error(`negative session_slots returned ${result.negativeStatus}, want 400`);
  if (result.blankStatus !== 200) throw new Error(`PUT without session_slots returned ${result.blankStatus}`);
  if (result.afterBlankSlots !== 0) throw new Error(`session_slots after a save without it = ${result.afterBlankSlots}, want 0`);
});
