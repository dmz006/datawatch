// TS-822 — PWA Automation tab and attention group: the shared tag browser draws in
// the Automation tab, Automata that carry an attention status land in the pinned
// "Needs attention" group, the landing rule picks attention when the group is not
// empty and tags otherwise, the viewer's own choice overrides the landing rule, a
// status change and a tag rename update the page through the event stream without a
// reload, and with the stream blocked the poll brings the same change in.
//
// Scope note: the default attention statuses are blocked, needs_review and paused.
// Reaching those states for a real Automaton needs an LLM decompose or a running
// session, which the sandbox does not provide, so the group is filled through the
// operator's own `autonomous.tags.attention_statuses` setting with draft Automata.
// The default list is still asserted on GET /api/autonomous/attention.
//
// Runs against the sandbox daemon that e2e-pwa.sh starts (TEST_HTTP / TEST_TLS from
// the environment), never the production daemon. Everything it needs is created
// through the API and removed at the end; nothing is skipped for missing state.
import https from 'node:https';
import { runStory, connectToPWA, navigateTo, screenshot, saveLog, env, waitForSplash } from './lib.mjs';

const TLS = new URL(env.tls);
const SFX = Date.now().toString(36);
const ROOT_NAME = `attn-root-${SFX}`;
const CHILD_NAME = `attn-child-${SFX}`;
const RENAMED = `attn-renamed-${SFX}`;
const DEFAULT_STATUSES = ['blocked', 'needs_review', 'paused'];
const STREAM_WAIT_MS = 8000;   // a stream update lands well inside this; the poll is 30 s
const POLL_WAIT_MS = 45000;    // the Automation loader polls every 30 s

// api(method, path, body) — the sandbox serves TLS with a self-signed cert.
function api(method, path, body) {
  return new Promise((resolve, reject) => {
    const data = body === undefined ? null : JSON.stringify(body);
    const headers = { Authorization: `Bearer ${env.token}` };
    if (data !== null) {
      headers['Content-Type'] = 'application/json';
      headers['Content-Length'] = Buffer.byteLength(data);
    }
    const req = https.request({
      host: TLS.hostname, port: TLS.port, path, method, headers, rejectUnauthorized: false,
    }, (res) => {
      let buf = '';
      res.on('data', (c) => { buf += c; });
      res.on('end', () => {
        if (res.statusCode >= 300) {
          reject(new Error(`${method} ${path} -> ${res.statusCode}: ${buf.slice(0, 200)}`));
          return;
        }
        try { resolve(buf ? JSON.parse(buf) : null); } catch { resolve(buf); }
      });
    });
    req.on('error', reject);
    if (data !== null) req.write(data);
    req.end();
  });
}

function assert(cond, msg) {
  if (!cond) throw new Error(msg);
}

function assertEqualJSON(actual, expected, msg) {
  const a = JSON.stringify(actual);
  const e = JSON.stringify(expected);
  if (a !== e) throw new Error(`${msg}: expected ${e}, got ${a}`);
}

// Read the tag config keys the test changes, so the story restores them.
async function readTagConfig() {
  const cfg = await api('GET', '/api/config');
  const tg = (cfg && cfg.autonomous && cfg.autonomous.tags) || {};
  return {
    attention_statuses: Array.isArray(tg.attention_statuses) ? tg.attention_statuses : DEFAULT_STATUSES,
    default_view: tg.default_view || 'auto',
  };
}

async function setTagConfig(values) {
  const patch = {};
  for (const [k, v] of Object.entries(values)) patch[`autonomous.tags.${k}`] = v;
  await api('PUT', '/api/config', patch);
}

// Open a fresh browser context: its own localStorage, so the landing decision and
// the viewer's override start from nothing. Every context uses the same viewport.
async function freshPage(browser, { initScript, blockStream } = {}) {
  const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport: { width: 1280, height: 900 } });
  const page = await context.newPage();
  if (initScript) await page.addInitScript(initScript);
  if (blockStream) {
    // The event stream is the WebSocket at /ws. Accept no connection on it, so no
    // frame ever reaches the page and only the poll can refresh the view.
    await page.routeWebSocket(/\/ws(\?|$)/, () => {});
  }
  await connectToPWA(page);
  return { context, page };
}

async function openAutomationTab(page) {
  await navigateTo(page, 'autonomous');
  await page.evaluate(() => switchAutomataTab('automation'));
  await page.waitForFunction(
    () => document.querySelector('#automationPanel .tag-view-switch .tag-view-btn'),
    null,
    { timeout: 20000 },
  );
}

// The landing view on screen: the switch's buttons are attention, list, tags in that
// order, and the pressed one names the view.
async function currentView(page) {
  return page.evaluate(() => {
    const buttons = [...document.querySelectorAll('#automationPanel .tag-view-switch .tag-view-btn')];
    const names = ['attention', 'list', 'tags'];
    const i = buttons.findIndex((b) => b.getAttribute('aria-pressed') === 'true');
    return i < 0 ? '' : names[i];
  });
}

async function pinnedRowIds(page) {
  return page.evaluate(() =>
    [...document.querySelectorAll('#automationPanel details.tag-pinned-attention .tag-pinned-row')]
      .map((r) => (r.querySelector('.tag-pinned-title')?.innerText || '').trim()));
}

// The tag filter lists every tag by its name in the Automation tab's facets.
async function facetNames(page) {
  return page.evaluate(() =>
    [...document.querySelectorAll('#tagFacets-automation .tag-multi-row .tag-multi-name')]
      .map((n) => (n.textContent || '').trim()));
}

async function clickViewButton(page, index) {
  await page.evaluate((i) => {
    const b = [...document.querySelectorAll('#automationPanel .tag-view-switch .tag-view-btn')][i];
    if (!b) throw new Error(`no view button at index ${i}`);
    b.click();
  }, index);
}

const created = { tagIds: [], prdIds: [], automationIds: [] };
let original = null;

await runStory(async (page) => {
  const browser = page.context().browser();
  original = await readTagConfig();

  // --- REST shapes the PWA depends on ---------------------------------------
  const attention0 = await api('GET', '/api/autonomous/attention');
  assert(Array.isArray(attention0.statuses) && Array.isArray(attention0.items), 'GET /api/autonomous/attention is not {statuses, items}');
  if (JSON.stringify(original.attention_statuses) === JSON.stringify(DEFAULT_STATUSES)) {
    assertEqualJSON(attention0.statuses, DEFAULT_STATUSES, 'default attention statuses');
  }

  // --- fixtures: a two-level tag, two draft Automata, one Automation -------
  const root = await api('POST', '/api/autonomous/tags', { name: ROOT_NAME });
  created.tagIds.push(root.id);
  const child = await api('POST', '/api/autonomous/tags', { name: CHILD_NAME, parent_id: root.id });
  created.tagIds.push(child.id);

  const p1 = await api('POST', '/api/autonomous/prds', { spec: `attention fixture one ${SFX}`, project_dir: '/tmp', effort: 'low' });
  created.prdIds.push(p1.id);
  const p2 = await api('POST', '/api/autonomous/prds', { spec: `attention fixture two ${SFX}`, project_dir: '/tmp', effort: 'low' });
  created.prdIds.push(p2.id);
  await api('POST', `/api/autonomous/prds/${p1.id}/set_tags`, { tag_ids: [child.id], primary_tag_id: child.id });

  const auto = await api('POST', '/api/automations', { name: `attn-auto-${SFX}`, members: [] });
  created.automationIds.push(auto.id);

  // Draft Automata land in the attention group only through the operator's setting.
  await setTagConfig({ attention_statuses: ['draft'], default_view: 'auto' });

  // REST: the attention list carries the tags the tile reads, and the tag list counts.
  const attention1 = await api('GET', '/api/autonomous/attention');
  assertEqualJSON(attention1.statuses, ['draft'], 'attention statuses after the setting');
  const attentionIds = attention1.items.map((i) => i.id).sort();
  assertEqualJSON(attentionIds, [p1.id, p2.id].sort(), 'attention items');
  const p1Item = attention1.items.find((i) => i.id === p1.id);
  assert(p1Item && Array.isArray(p1Item.tags) && p1Item.tags.includes(child.id), 'attention item carries no tags array');

  const tagList = await api('GET', '/api/autonomous/tags?counts=true');
  for (const t of tagList.tags) {
    for (const k of ['subtree_count', 'direct_count', 'session_subtree_count', 'session_direct_count']) {
      assert(typeof t[k] === 'number', `counts=true tag ${t.name} lacks numeric ${k}`);
    }
  }
  assert(tagList.untagged && typeof tagList.untagged.count === 'number' && typeof tagList.untagged.session_count === 'number',
    'counts=true reply lacks the untagged object');
  assert(tagList.registry && typeof tagList.registry === 'object' && typeof tagList.registry.stale === 'boolean'
    && Array.isArray(tagList.registry.notes), 'counts=true reply lacks the registry object');

  // --- landing rule and the viewer's override -------------------------------
  // A. attention is not empty, default view auto: the Attention view opens.
  let ctx = await freshPage(browser);
  await openAutomationTab(ctx.page);
  const landA = await currentView(ctx.page);
  assertEqualJSON(landA, 'attention', 'landing with a non-empty attention group');
  const rowsA = await pinnedRowIds(ctx.page);
  assertEqualJSON(rowsA.slice().sort(), [p1.id, p2.id].sort(), 'attention group rows');
  await screenshot(ctx.page, '01-attention-landing');

  // B. the viewer picks Tags: that choice is kept, so the next load opens Tags even
  // though the group is still not empty.
  await clickViewButton(ctx.page, 2);
  await ctx.page.waitForFunction(() => document.querySelector('#automationPanel .tag-sections'), null, { timeout: 10000 });
  await ctx.page.reload({ waitUntil: 'domcontentloaded' });
  await waitForSplash(ctx.page);
  await openAutomationTab(ctx.page);
  const landB = await currentView(ctx.page);
  assertEqualJSON(landB, 'tags', 'per-viewer override after the viewer picked Tags');
  await ctx.context.close();

  // C. the same override cleared: the landing rule again picks attention.
  ctx = await freshPage(browser, { initScript: () => { try { localStorage.removeItem('cs_tag_landing'); } catch (_) { /* storage blocked */ } } });
  await openAutomationTab(ctx.page);
  assertEqualJSON(await currentView(ctx.page), 'attention', 'landing after the override is cleared');
  await ctx.context.close();

  // D. the group is empty: the landing rule picks tags.
  // The landing rule's "tags" leaves the attention view off; the browser then opens
  // on the viewer's stored sub-view (List by default), so the check is "not attention".
  await setTagConfig({ attention_statuses: ['paused'] });
  ctx = await freshPage(browser);
  await openAutomationTab(ctx.page);
  const landD = await currentView(ctx.page);
  assert(landD !== 'attention', `landing with an empty attention group opened ${landD}, expected the tag browser`);
  assertEqualJSON(await pinnedRowIds(ctx.page), [], 'empty attention group rows');
  // E. the viewer picks Attention on an empty group: the view opens and says so.
  await clickViewButton(ctx.page, 0);
  await ctx.page.waitForFunction(() => document.querySelector('#automationPanel .tag-attention-none'), null, { timeout: 10000 });
  await ctx.context.close();
  await setTagConfig({ attention_statuses: ['draft'] });

  // --- live: a status change and a tag rename, no reload -------------------
  ctx = await freshPage(browser);
  await openAutomationTab(ctx.page);
  assertEqualJSON(await currentView(ctx.page), 'attention', 'live page landing');
  const rowsLive = await pinnedRowIds(ctx.page);
  assertEqual(rowsLive.length, 2, 'live page: rows before the status change');

  // p2 is cancelled, so it leaves the draft setting's attention group.
  let t0 = Date.now();
  await api('POST', `/api/autonomous/prds/${p2.id}/cancel`, {});
  await ctx.page.waitForFunction(
    () => document.querySelectorAll('#automationPanel details.tag-pinned-attention .tag-pinned-row').length === 1,
    null, { timeout: STREAM_WAIT_MS },
  ).catch(() => { throw new Error(`status change did not reach the attention group within ${STREAM_WAIT_MS} ms (stream)`); });
  const statusMs = Date.now() - t0;

  t0 = Date.now();
  await api('PATCH', `/api/autonomous/tags/${child.id}`, { name: RENAMED });
  await ctx.page.waitForFunction(
    (name) => [...document.querySelectorAll('#tagFacets-automation .tag-multi-row .tag-multi-name')].some((n) => (n.textContent || '').trim() === name),
    RENAMED, { timeout: STREAM_WAIT_MS },
  ).catch(() => { throw new Error(`tag rename did not reach the tag browser within ${STREAM_WAIT_MS} ms (stream)`); });
  const renameMs = Date.now() - t0;
  await screenshot(ctx.page, '02-live-update');
  await ctx.context.close();

  // --- stream blocked: the poll catches up -----------------------------------
  // p1 (the child-tagged draft) is still in the group; renaming the tag again must
  // not reach the page through the stream, only through the 30 s poll.
  const POLLED = `${RENAMED}-poll`;
  ctx = await freshPage(browser, { blockStream: true });
  await openAutomationTab(ctx.page);
  await api('PATCH', `/api/autonomous/tags/${child.id}`, { name: POLLED });
  await ctx.page.waitForTimeout(5000);
  const stillOld = await facetNames(ctx.page);
  assert(!stillOld.includes(POLLED), 'with the stream blocked the rename reached the page before the poll');
  t0 = Date.now();
  await ctx.page.waitForFunction(
    (name) => [...document.querySelectorAll('#tagFacets-automation .tag-multi-row .tag-multi-name')].some((n) => (n.textContent || '').trim() === name),
    POLLED, { timeout: POLL_WAIT_MS },
  ).catch(() => { throw new Error(`poll did not bring the rename within ${POLL_WAIT_MS} ms with the stream blocked`); });
  const pollMs = Date.now() - t0;
  await screenshot(ctx.page, '03-poll-catch-up');
  await ctx.context.close();

  // --- cleanup: restore the operator's settings and remove what was created --
  await setTagConfig({ attention_statuses: original.attention_statuses, default_view: original.default_view });
  for (const id of created.automationIds) await api('DELETE', `/api/automations/${id}`).catch(() => null);
  for (const id of created.prdIds) {
    await api('POST', `/api/autonomous/prds/${id}/set_tags`, { tag_ids: [] }).catch(() => null);
  }
  for (const id of created.tagIds.slice().reverse()) await api('DELETE', `/api/autonomous/tags/${id}`).catch(() => null);

  const result = [
    'REST: attention statuses default, items carry tags; counts=true numeric counts, untagged, registry object',
    'landing: attention with a non-empty group; per-viewer override to tags kept across reload; override cleared back to attention; empty group lands off attention; attention view on an empty group shows the none note',
    `live: status change reached the group in ${statusMs} ms; rename reached the tag browser in ${renameMs} ms`,
    `stream blocked: rename reached the page after ${pollMs} ms (poll), not within 5 s`,
  ].join('\n');
  await saveLog('result', result);
  console.log(result);
});

function assertEqual(actual, expected, msg) {
  if (actual !== expected) throw new Error(`${msg}: expected ${expected}, got ${actual}`);
}
