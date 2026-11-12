// TS-823 — session tags: a session's own tags are set from the session detail,
// a session spawned by an Automaton shows the Automaton's tags as inherited and
// read-only, and deleting a tag that a session still carries is refused with a
// count. The REST shapes the PWA reads are asserted along the way: `tags` (own),
// `inherited_tags` on every session, and the 409 refusal body with `count`.
//
// Runs against the sandbox daemon that e2e-pwa.sh starts (TEST_HTTP / TEST_TLS
// from the environment), never the production daemon. Everything the story needs
// is created inline through the API: three tags, one Automaton carrying one of
// them, and two shell sessions. Session B is linked to the Automaton through its
// prd_id, the same link the executor sets on the sessions it spawns, so its
// inherited tags come from the real owner lookup. Everything created is removed
// in the finally block, in dependency order.
import { runStory, connectToPWA, navigateTo, screenshot, saveLog, env } from './lib.mjs';

// clickDom fires the click on the element itself. Toasts from earlier config
// migrations sit over the detail row and would intercept a pointer click.
async function clickDom(page, selector) {
  await page.evaluate((sel) => document.querySelector(sel).click(), selector);
}

const TAG = (n) => `TS823 ${n} ${Date.now().toString(36)}`;

// api(method, path, body) calls the sandbox over plain HTTP with the bearer
// token, which is how the sibling stories reach the daemon's REST surface.
async function api(method, p, body) {
  const r = await fetch(env.http + p, {
    method,
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${env.token}` },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await r.text();
  let parsed = text;
  try { parsed = text ? JSON.parse(text) : null; } catch { /* plain-text error body */ }
  return { status: r.status, body: parsed };
}

function mustOK(res, what) {
  if (res.status < 200 || res.status > 299) {
    throw new Error(`${what}: HTTP ${res.status} ${typeof res.body === 'string' ? res.body.slice(0, 200) : JSON.stringify(res.body).slice(0, 200)}`);
  }
  return res.body;
}

// sessionById finds one session in the list, by full or short ID.
async function sessionById(id) {
  const list = mustOK(await api('GET', '/api/sessions'), 'list sessions');
  const items = Array.isArray(list) ? list : (list.sessions || []);
  const hit = items.find((s) => s.full_id === id || s.id === id);
  if (!hit) throw new Error(`session ${id} not in the sessions list`);
  return hit;
}

// waitFor polls a REST condition; the PWA and the daemon apply writes
// asynchronously, so a single read right after a write is not enough.
async function waitFor(label, fn, timeoutMs = 10000) {
  const deadline = Date.now() + timeoutMs;
  let last;
  while (Date.now() < deadline) {
    last = await fn();
    if (last) return last;
    await new Promise((r) => setTimeout(r, 250));
  }
  throw new Error(`timed out waiting for ${label} (last: ${JSON.stringify(last)})`);
}

const created = { sessions: [], automaton: null, tags: [] };

async function cleanup() {
  for (const id of created.sessions.reverse()) {
    await api('POST', '/api/sessions/delete', { id }).catch(() => {});
  }
  if (created.automaton) {
    await api('DELETE', `/api/autonomous/prds/${encodeURIComponent(created.automaton)}`).catch(() => {});
  }
  for (const id of created.tags.reverse()) {
    await api('DELETE', `/api/autonomous/tags/${encodeURIComponent(id)}`).catch(() => {});
  }
}

await runStory(async (page) => {
  const log = [];
  try {
    // --- inline fixtures: three tags, one Automaton carrying the Automaton tag ---
    const own = mustOK(await api('POST', '/api/autonomous/tags', { name: TAG('own') }), 'create own tag');
    created.tags.push(own.id);
    const extra = mustOK(await api('POST', '/api/autonomous/tags', { name: TAG('extra') }), 'create extra tag');
    created.tags.push(extra.id);
    const auto = mustOK(await api('POST', '/api/autonomous/tags', { name: TAG('automaton') }), 'create automaton tag');
    created.tags.push(auto.id);
    const doomed = mustOK(await api('POST', '/api/autonomous/tags', { name: TAG('doomed') }), 'create doomed tag');
    created.tags.push(doomed.id);

    const prd = mustOK(await api('POST', '/api/autonomous/prds', {
      spec: 'ts823-session-tags-fixture: echo hello world',
      project_dir: process.env.TMPDIR || '/tmp',
      effort: 'low',
    }), 'create automaton');
    created.automaton = prd.id;
    mustOK(await api('POST', `/api/autonomous/prds/${encodeURIComponent(prd.id)}/set_tags`, { tag_ids: [auto.id] }), 'set automaton tags');

    // --- two shell sessions: A runs outside any Automaton, B is Automaton-linked ---
    const startBody = (extraFields) => ({
      task: 'ts823-session-tags-fixture', name: 'ts823-session', backend: 'shell',
      project_dir: process.env.TMPDIR || '/tmp', effort: 'quick', ...extraFields,
    });
    const sessA = mustOK(await api('POST', '/api/sessions/start', startBody({})), 'start session A');
    const idA = sessA.full_id || sessA.id;
    created.sessions.push(idA);
    const sessB = mustOK(await api('POST', '/api/sessions/start', startBody({ prd_id: prd.id })), 'start session B');
    const idB = sessB.full_id || sessB.id;
    created.sessions.push(idB);

    // --- REST shapes before any edit ---
    const aRest0 = await sessionById(idA);
    const bRest0 = await sessionById(idB);
    if (!Array.isArray(aRest0.inherited_tags) || aRest0.inherited_tags.length !== 0) {
      throw new Error(`session A inherited_tags should be an empty array, got ${JSON.stringify(aRest0.inherited_tags)}`);
    }
    if (!Array.isArray(bRest0.inherited_tags) || !bRest0.inherited_tags.includes(auto.id)) {
      throw new Error(`session B inherited_tags should carry the Automaton tag ${auto.id}, got ${JSON.stringify(bRest0.inherited_tags)}`);
    }
    if ((bRest0.tags || []).length !== 0) {
      throw new Error(`session B own tags should be empty before any edit, got ${JSON.stringify(bRest0.tags)}`);
    }
    log.push('REST: A inherited_tags=[], B inherited_tags carries the Automaton tag, B own tags=[]');

    // A refused tag ID is 404 and nothing is written.
    const refused = await api('POST', '/api/sessions/tags', { id: idA, tag_ids: ['no-such-tag-ts823'] });
    if (refused.status !== 404) throw new Error(`unknown tag on a session should be 404, got ${refused.status}`);

    // --- the PWA: set session A's own tags from the session detail -----------
    await connectToPWA(page);
    await navigateTo(page, 'sessions');
    await page.evaluate((id) => window.navigate('session-detail', id), idA);
    await page.waitForSelector('#sessionDetailTagRow button', { state: 'visible', timeout: 20000 });
    await clickDom(page, '#sessionDetailTagRow button');
    await page.waitForSelector('#sessionTagsModalPicker', { state: 'visible', timeout: 10000 });
    await page.evaluate((v) => { const cb = document.querySelector(`#sessionTagsModalPicker .tag-picker-cb[value="${v}"]`); cb.checked = true; cb.dispatchEvent(new Event('change', { bubbles: true })); }, extra.id);
    await clickDom(page, '#genericModalConfirmBtn');
    await waitFor('session A to carry the extra tag', async () => {
      const s = await sessionById(idA);
      return (s.tags || []).includes(extra.id);
    });
    await page.waitForSelector(`#sessionDetailTagRow .tag-chip[data-tag-id="${extra.id}"]`, { state: 'visible', timeout: 10000 });
    await screenshot(page, '01-session-a-own-tags');
    log.push('PWA: session A own tags saved from the detail modal, chip shown, REST tags carries the extra tag');

    // --- the PWA: B shows the Automaton tag as inherited and read-only -------
    await page.evaluate((id) => window.navigate('session-detail', id), idB);
    await page.waitForSelector(`#sessionDetailTagRow .tag-chip-inherited[data-tag-id="${auto.id}"]`, { state: 'visible', timeout: 20000 });
    const ownOnB = await page.locator('#sessionDetailTagRow .tag-chip[data-tag-id]:not(.tag-chip-inherited)').count();
    if (ownOnB !== 0) throw new Error(`session B shows ${ownOnB} own tag chip(s), expected none`);
    const inheritedLabel = await page.innerText(`#sessionDetailTagRow .tag-chip-inherited[data-tag-id="${auto.id}"]`);
    if (!/inherited/i.test(inheritedLabel)) throw new Error(`inherited chip has no inherited label: ${inheritedLabel}`);
    await screenshot(page, '02-session-b-inherited');

    await clickDom(page, '#sessionDetailTagRow button');
    await page.waitForSelector('#sessionTagsModalPicker', { state: 'visible', timeout: 10000 });
    const autoChecked = await page.isChecked(`#sessionTagsModalPicker .tag-picker-cb[value="${auto.id}"]`);
    if (autoChecked) throw new Error('the picker on session B pre-checks the inherited Automaton tag; inherited tags must not be editable on the session');
    const note = await page.innerText('#genericModal');
    if (!/Inherited from the Automaton/i.test(note)) throw new Error('the session tag modal does not state the inherited tags are read-only');
    await clickDom(page, '#genericModal .btn-secondary');
    await page.waitForSelector('#genericModal', { state: 'detached', timeout: 5000 }).catch(() => {});
    const bRest1 = await sessionById(idB);
    if ((bRest1.tags || []).length !== 0 || !bRest1.inherited_tags.includes(auto.id)) {
      throw new Error(`session B changed after cancelling the modal: tags=${JSON.stringify(bRest1.tags)} inherited=${JSON.stringify(bRest1.inherited_tags)}`);
    }
    log.push('PWA: session B shows the Automaton tag as an inherited chip, no own chips, picker does not pre-check it; cancel leaves REST unchanged');

    // --- deleting a tag a session still carries is refused with a count -----
    mustOK(await api('POST', '/api/sessions/tags', { id: idA, tag_ids: [own.id, extra.id, doomed.id] }), 'assign doomed tag to session A');
    const inUse = await api('DELETE', `/api/autonomous/tags/${encodeURIComponent(doomed.id)}`);
    if (inUse.status !== 409) throw new Error(`deleting an in-use tag should be 409, got ${inUse.status}: ${JSON.stringify(inUse.body)}`);
    if (!inUse.body || inUse.body.count !== 1) throw new Error(`in-use refusal should carry count 1 (session A only), got ${JSON.stringify(inUse.body)}`);
    log.push(`REST: DELETE of an in-use tag -> 409 with count=${inUse.body.count}: ${inUse.body.error}`);

    // Once the session drops the tag, the same delete goes through.
    mustOK(await api('POST', '/api/sessions/tags', { id: idA, tag_ids: [own.id, extra.id] }), 'remove doomed tag from session A');
    const freed = await api('DELETE', `/api/autonomous/tags/${encodeURIComponent(doomed.id)}`);
    if (freed.status < 200 || freed.status > 299) throw new Error(`delete after unassigning should succeed, got ${freed.status}`);
    created.tags = created.tags.filter((t) => t !== doomed.id);
    log.push('REST: after the session drops the tag, the delete succeeds');

    await saveLog('result', log.join('\n'));
    console.log(log.map((l) => `  ${l}`).join('\n'));
  } catch (err) {
    await saveLog('result', `${log.join('\n')}\nFAILED: ${err.message}`).catch(() => {});
    throw err;
  } finally {
    await cleanup();
  }
});
