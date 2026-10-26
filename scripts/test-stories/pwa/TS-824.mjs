// TS-824 — federated tag registry on a peer daemon: the peer shows the keeper's tags,
// a rename on the keeper reaches it, stopping the keeper marks the registry stale with
// the last-copy banner and disables the edits that need the keeper, an offline create
// shows pending sync, and restarting the keeper with a same-name tag produces the
// visible merge note, which the dismiss button removes. The REST shapes the PWA reads
// (the registry object and the counts=true fields) are asserted at each step.
//
// The sandbox daemon (TEST_HTTP / TEST_TLS from e2e-pwa.sh) is the peer. The keeper is a
// small stub HTTP server this story runs in-process on a free loopback port: it serves
// the two registry routes a keeper serves (GET /api/autonomous/tags/registry and
// POST /api/autonomous/tags/registry/write) and checks the bearer token the peer was
// configured with. Stopping and restarting it keeps the same port, so the peer's sync
// loop sees the keeper go away and come back.
//
// The browser checks read the Settings → Automata → Tag Registry panel, which lists
// every tag with the registry block (role line, stale banner, merge notes) and disables
// the edits that need the keeper. The Automata tab draws nothing until an Automaton
// carries a tag, so the story does not depend on one.
//
// Every change this story makes is removed afterwards: the keeper stub is closed, the
// peer is set back to standalone, the federation peer is deleted and the tags created
// on the peer are deleted. Nothing is skipped for missing state.
import http from 'node:http';
import net from 'node:net';
import { runStory, connectToPWA, navigateTo, screenshot, saveLog, env } from './lib.mjs';

// The sandbox serves HTTPS with a self-signed certificate; the REST calls below go to
// the TLS origin (HTTP redirects drop the Authorization header).
process.env.NODE_TLS_REJECT_UNAUTHORIZED = '0';

const KEEPER_NAME = 'ts824-keeper';
const KEEPER_TOKEN = 'ts824-keeper-token';
const ALPHA_ID = 'kp-ts824-alpha';
const ALPHA = 'Alpha';
const ALPHA_RENAMED = 'Alpha Renamed';
const OFFLINE = 'Offline Tag';
const STEP_MS = 45000;

// ---- REST helper -------------------------------------------------------------

async function api(method, path, body) {
  const res = await fetch(env.tls + path, {
    method,
    headers: { Authorization: `Bearer ${env.token}`, 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  let json = null;
  try { json = JSON.parse(text); } catch { /* non-JSON reply */ }
  return { status: res.status, json, text };
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function until(label, fn, timeout = STEP_MS, step = 500) {
  const deadline = Date.now() + timeout;
  let last = null;
  while (Date.now() < deadline) {
    last = await fn();
    if (last) return last;
    await sleep(step);
  }
  throw new Error(`timed out waiting for ${label}; last seen: ${JSON.stringify(last).slice(0, 600)}`);
}

function check(cond, msg) {
  if (!cond) throw new Error(msg);
}

// The registry object and the tag list, as the PWA reads them.
async function tagList() {
  const r = await api('GET', '/api/autonomous/tags?counts=true');
  check(r.status === 200, `GET /api/autonomous/tags?counts=true answered ${r.status}`);
  return { reply: r.json, reg: r.json.registry };
}

// ---- keeper stub ---------------------------------------------------------------

const keeper = { server: null, port: 0, state: null };

function freePort() {
  return new Promise((resolve, reject) => {
    const s = net.createServer();
    s.unref();
    s.on('error', reject);
    s.listen(0, '127.0.0.1', () => {
      const { port } = s.address();
      s.close(() => resolve(port));
    });
  });
}

const nowISO = () => new Date().toISOString();

function sendJSON(res, status, body) {
  res.writeHead(status, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify(body));
}

// applyKeeperWrite applies one peer write the way a keeper does for the cases this
// story reaches: a create (a pending create merges into a same-name top-level tag) and
// a rename. Other kinds are refused, so an unexpected write fails the story loudly.
function applyKeeperWrite(state, w) {
  if (w.kind === 'create') {
    const same = state.tags.find((t) => t.id === w.tag_id);
    if (same) return { status: 200, body: { tag: same, revision: state.revision } };
    if (w.pending) {
      const kept = state.tags.find((t) =>
        (t.parent_id || '') === (w.parent_id || '') && t.name.toLowerCase() === w.name.toLowerCase());
      if (kept) {
        const note = {
          id: `kp-note-${state.notes.length + 1}`,
          kind: 'merged',
          name: kept.name,
          parent_path: '',
          kept_id: kept.id,
          replaced_id: w.tag_id,
          nodes: [kept.origin_node || KEEPER_NAME, w.origin_node || 'peer'],
          merged_at: nowISO(),
        };
        state.notes.push(note);
        state.revision += 1;
        return { status: 200, body: { tag: kept, revision: state.revision, merge: note } };
      }
    }
    const tag = {
      id: w.tag_id,
      name: w.name,
      parent_id: w.parent_id || '',
      color: w.color || '',
      description: w.description || '',
      origin_node: w.origin_node || 'peer',
      created_at: nowISO(),
      updated_at: nowISO(),
    };
    state.tags.push(tag);
    state.revision += 1;
    return { status: 200, body: { tag, revision: state.revision } };
  }
  if (w.kind === 'rename') {
    const tag = state.tags.find((t) => t.id === w.tag_id);
    if (!tag) return { status: 404, body: { error: 'tag not found', kind: 'tag_not_found' } };
    tag.name = w.name;
    tag.updated_at = nowISO();
    state.revision += 1;
    return { status: 200, body: { tag, revision: state.revision } };
  }
  return { status: 400, body: { error: `keeper stub does not apply ${w.kind}` } };
}

function keeperHandler(req, res) {
  let raw = '';
  req.on('data', (c) => { raw += c; });
  req.on('end', () => {
    if (req.headers.authorization !== `Bearer ${KEEPER_TOKEN}`) {
      return sendJSON(res, 401, { error: 'unauthorized' });
    }
    const url = req.url.split('?')[0];
    if (req.method === 'GET' && url === '/api/autonomous/tags/registry') {
      return sendJSON(res, 200, {
        revision: keeper.state.revision,
        tags: keeper.state.tags,
        notes: keeper.state.notes,
      });
    }
    if (req.method === 'POST' && url === '/api/autonomous/tags/registry/write') {
      let w;
      try { w = JSON.parse(raw || '{}'); } catch { return sendJSON(res, 400, { error: 'bad json' }); }
      const out = applyKeeperWrite(keeper.state, w);
      return sendJSON(res, out.status, out.body);
    }
    return sendJSON(res, 404, { error: 'not a keeper route' });
  });
}

function startKeeper() {
  return new Promise((resolve, reject) => {
    const srv = http.createServer(keeperHandler);
    srv.on('error', reject);
    srv.listen(keeper.port, '127.0.0.1', () => resolve(srv));
  });
}

function stopKeeper() {
  return new Promise((resolve) => {
    if (!keeper.server) return resolve();
    const srv = keeper.server;
    keeper.server = null;
    srv.closeAllConnections();
    srv.close(() => resolve());
  });
}

async function bringKeeperUp(state) {
  keeper.state = state;
  keeper.server = await startKeeper();
}

// ---- Settings → Automata → Tag Registry panel ----------------------------------

async function openRegistrySettings(page) {
  await navigateTo(page, 'sessions');
  await navigateTo(page, 'settings');
  await page.evaluate(() => window.switchSettingsTab('automata'));
  await page.waitForSelector('#settings-sec-automata_tag_registry', { state: 'attached', timeout: 20000 });
  await page.evaluate(() => {
    const c = document.getElementById('settings-sec-automata_tag_registry');
    if (c && c.style.display === 'none') window.toggleSettingsSection('automata_tag_registry');
  });
}

// registryPanel reads what the panel shows now: the role line, the tag names (the
// rename inputs), the stale banner, stale marks, pending badges, merge notes and
// whether each edit control is disabled.
// showRegistry scrolls the Tag Registry panel into view, so a screenshot shows it.
async function showRegistry(page) {
  await page.evaluate(() => {
    const e = document.getElementById('automataTagRegistryPanel');
    if (e) e.scrollIntoView({ block: 'start' });
  });
}

async function registryPanel(page) {
  return page.evaluate(() => {
    const p = document.getElementById('automataTagRegistryPanel');
    if (!p) return null;
    const line = p.querySelector('.tag-registry-line');
    const banner = p.querySelector('.tag-registry-banner');
    return {
      line: line ? line.innerText : '',
      names: [...p.querySelectorAll('input[type="text"]')].map((i) => i.value),
      banner: banner ? banner.innerText : '',
      stale: p.querySelectorAll('.tag-stale-mark').length,
      pending: [...p.querySelectorAll('.tag-pending-badge')].map((e) => e.innerText),
      notes: [...p.querySelectorAll('.tag-registry-note')].map((e) => ({ id: e.dataset.noteId, text: e.innerText })),
      controls: [...p.querySelectorAll('.tag-registry-control')].map((e) => e.disabled),
    };
  });
}

// waitForRegistryPanel reopens Settings until the panel satisfies pred. The panel
// draws when Settings opens, so a sync that lands later shows on the next open.
async function waitForRegistryPanel(page, label, pred, timeout = STEP_MS) {
  const deadline = Date.now() + timeout;
  let last = null;
  while (Date.now() < deadline) {
    await openRegistrySettings(page);
    await sleep(1500);
    last = await registryPanel(page);
    if (last && pred(last)) return last;
    await sleep(1000);
  }
  throw new Error(`timed out waiting for the Tag Registry panel to show ${label}; last: ${JSON.stringify(last).slice(0, 600)}`);
}

// ---- story ---------------------------------------------------------------------

const created = { offlineID: '', peer: false, keeperSet: false };

// failures collects the checks that did not hold and the story keeps going, so one
// missing UI badge does not hide the merge and dismiss steps. soft returns null when
// the wrapped wait times out; the caller records the failure.
const failures = [];
const soft = async (fn) => {
  try { return await fn(); } catch { return null; }
};

async function cleanup() {
  await stopKeeper().catch(() => {});
  if (created.keeperSet) {
    await api('PUT', '/api/config', { 'autonomous.tags.registry_keeper': '' }).catch(() => {});
  }
  for (const id of [ALPHA_ID, created.offlineID]) {
    if (id) await api('DELETE', `/api/autonomous/tags/${encodeURIComponent(id)}`).catch(() => {});
  }
  if (created.peer) {
    await api('DELETE', `/api/federation/peers/${encodeURIComponent(KEEPER_NAME)}`).catch(() => {});
  }
}

runStory(async (page) => {
  await connectToPWA(page);
  keeper.port = await freePort();
  const keeperURL = `http://127.0.0.1:${keeper.port}`;
  await bringKeeperUp({
    revision: 1,
    tags: [{ id: ALPHA_ID, name: ALPHA, parent_id: '', color: '#6366f1', origin_node: KEEPER_NAME, created_at: nowISO(), updated_at: nowISO() }],
    notes: [],
  });

  try {
    // --- configure the peer -----------------------------------------------------
    const peer = await api('POST', '/api/federation/peers', { name: KEEPER_NAME, url: keeperURL, token: KEEPER_TOKEN, enabled: true });
    check(peer.status === 201, `POST /api/federation/peers answered ${peer.status}: ${peer.text}`);
    created.peer = true;

    const cfg = await api('PUT', '/api/config', {
      'autonomous.tags.registry_keeper': KEEPER_NAME,
      'autonomous.tags.registry_sync_seconds': 5,
    });
    check(cfg.status === 200, `PUT /api/config answered ${cfg.status}: ${cfg.text}`);
    created.keeperSet = true;

    // --- the peer shows the keeper's tags ----------------------------------------
    let seen = null;
    const synced = await until('the peer to pull the keeper tag', async () => {
      const { reply, reg } = await tagList();
      const alpha = (reply.tags || []).find((t) => t.id === ALPHA_ID);
      if (reg && reg.role === 'peer' && alpha && !reg.stale) return { reply, reg };
      seen = { registry: reg, tagIDs: (reply.tags || []).map((t) => t.id) };
      return null;
    }).catch((err) => { throw new Error(`${err.message} (last registry and tag ids: ${JSON.stringify(seen)})`); });

    // REST shape the PWA depends on: the registry object and the counts=true fields.
    const reg = synced.reg;
    for (const key of ['role', 'keeper', 'stale', 'last_synced_at', 'revision', 'pending_sync_count', 'notes', 'last_error']) {
      check(key in reg, `registry object lacks ${key}: ${JSON.stringify(reg)}`);
    }
    check(reg.keeper === KEEPER_NAME, `registry keeper is ${reg.keeper}, want ${KEEPER_NAME}`);
    check(reg.revision >= 1, `registry revision is ${reg.revision}`);
    check(Array.isArray(reg.notes), 'registry notes is not an array');
    check(synced.reply.untagged !== undefined, 'counts=true reply lacks untagged');
    const counted = (synced.reply.tags || []).find((t) => t.id === ALPHA_ID);
    check(counted && 'subtree_count' in counted && 'direct_count' in counted,
      `counts=true tag lacks subtree_count/direct_count: ${JSON.stringify(counted)}`);

    // The Settings panel shows the registry line, the keeper's tag and no banner.
    const shown = await waitForRegistryPanel(page, `the keeper tag ${ALPHA}`, (p) =>
      p.line.includes(KEEPER_NAME) && p.names.includes(ALPHA) && p.banner === '');
    check(shown.line.includes(KEEPER_NAME), `registry line does not name the keeper: ${shown.line}`);
    await showRegistry(page);
    await screenshot(page, '01-peer-shows-keeper-tags');

    // --- a rename on the keeper reaches the peer ---------------------------------
    keeper.state.tags[0].name = ALPHA_RENAMED;
    keeper.state.tags[0].updated_at = nowISO();
    keeper.state.revision += 1;
    await until('the rename to reach the peer', async () => {
      const { reply } = await tagList();
      const t = (reply.tags || []).find((x) => x.id === ALPHA_ID);
      return t && t.name === ALPHA_RENAMED ? true : null;
    });
    await waitForRegistryPanel(page, 'the renamed tag', (p) => p.names.includes(ALPHA_RENAMED));

    // --- stop the keeper: stale, last-copy banner, edits disabled ----------------
    await stopKeeper();
    await until('the peer to mark the registry stale', async () => {
      const { reg: r } = await tagList();
      return r && r.stale === true ? r : null;
    });
    const stale = await tagList();
    check(stale.reg.last_error !== '', 'a stale registry names no last_error');
    const stalePanel = await waitForRegistryPanel(page, 'the stale banner and disabled edits', (p) =>
      p.banner.includes('last copy') && p.stale > 0 && p.controls.length > 0);
    check(stalePanel.banner.includes('last copy'), `banner does not say it shows the last copy: ${stalePanel.banner}`);
    check(stalePanel.controls.every((d) => d), `edits are not all disabled while stale: ${JSON.stringify(stalePanel.controls)}`);
    await showRegistry(page);
    await screenshot(page, '02-stale-banner');

    // A rename while the keeper is unreachable is refused, and the REST refusal names
    // the unreachable kind the PWA maps.
    const refused = await api('PATCH', `/api/autonomous/tags/${encodeURIComponent(ALPHA_ID)}`, { name: 'Refused' });
    check(refused.status === 503, `rename while stale answered ${refused.status}, want 503`);
    check(refused.json && refused.json.kind === 'unreachable', `rename refusal kind is ${refused.json && refused.json.kind}`);

    // --- an offline create shows pending sync ------------------------------------
    const offline = await api('POST', '/api/autonomous/tags', { name: OFFLINE });
    check(offline.status === 200, `offline create answered ${offline.status}: ${offline.text}`);
    check(offline.json && offline.json.pending_sync === true,
      `offline create is not marked pending_sync: ${offline.text}`);
    created.offlineID = offline.json.id;

    // The PWA draws the pending badge from pending_sync on each tag of the list. A
    // failed check here is recorded and the story carries on, so the merge and dismiss
    // steps still run; the story fails at the end with every recorded message.
    const pendingList = await tagList();
    const offlineRow = (pendingList.reply.tags || []).find((t) => t.id === created.offlineID);
    if (!offlineRow || offlineRow.pending_sync !== true) {
      failures.push(`GET /api/autonomous/tags lists the offline tag without pending_sync: ${JSON.stringify(offlineRow)}`);
    }
    check(pendingList.reg.pending_sync_count === 1, `pending_sync_count is ${pendingList.reg.pending_sync_count}, want 1`);
    const pendPanel = await soft(() => waitForRegistryPanel(page, 'the pending sync badge', (p) =>
      p.pending.length > 0 && p.banner.includes('last copy')));
    if (pendPanel && !pendPanel.pending.some((t) => /pending sync/i.test(t))) {
      failures.push(`pending badge reads ${JSON.stringify(pendPanel.pending)}`);
    } else if (!pendPanel) {
      failures.push('the Tag Registry panel never showed the pending sync badge for the offline tag');
    }
    await showRegistry(page);
    await screenshot(page, '03-pending-sync');

    // --- restart the keeper with a same-name tag: merge note, then dismiss --------
    await bringKeeperUp({
      revision: 7,
      tags: [
        { id: ALPHA_ID, name: ALPHA_RENAMED, parent_id: '', color: '#6366f1', origin_node: KEEPER_NAME, created_at: nowISO(), updated_at: nowISO() },
        { id: 'kp-ts824-offline', name: OFFLINE, parent_id: '', color: '', origin_node: KEEPER_NAME, created_at: nowISO(), updated_at: nowISO() },
      ],
      notes: [],
    });

    const merged = await until('the peer to push the pending tag and record the merge', async () => {
      const { reply, reg: r } = await tagList();
      const same = (reply.tags || []).filter((t) => t.name === OFFLINE);
      return r && !r.stale && r.pending_sync_count === 0 && (r.notes || []).some((n) => n.kind === 'merged') && same.length === 1
        ? { reg: r, same }
        : null;
    });
    const note = merged.reg.notes.find((n) => n.kind === 'merged');
    check(note.name === OFFLINE, `merge note names ${note.name}, want ${OFFLINE}`);
    check(Array.isArray(note.nodes) && note.nodes.length >= 2, `merge note names no two nodes: ${JSON.stringify(note.nodes)}`);
    created.offlineID = merged.same[0].id;

    const mergePanel = await waitForRegistryPanel(page, 'the merge note', (p) =>
      p.notes.some((n) => n.id === note.id) && p.banner === '');
    const noteText = mergePanel.notes.find((n) => n.id === note.id).text;
    check(noteText.includes(OFFLINE) && noteText.includes('merged'), `merge note line reads: ${noteText}`);
    await showRegistry(page);
    await screenshot(page, '04-merge-note');

    await page.click(`#automataTagRegistryPanel .tag-registry-note[data-note-id="${note.id}"] button`);
    await page.waitForFunction((id) =>
      !document.querySelector(`#automataTagRegistryPanel .tag-registry-note[data-note-id="${id}"]`), note.id, { timeout: 15000 });
    const after = await until('the dismissal to be kept', async () => {
      const { reg: r } = await tagList();
      return (r.notes || []).some((n) => n.id === note.id) ? null : r;
    });
    check(after.notes.length === 0, `notes after dismiss: ${JSON.stringify(after.notes)}`);

    if (failures.length) throw new Error(failures.join(' | '));

    await saveLog('result', [
      'peer shows keeper tags with the registry object and counts=true fields',
      'rename on keeper reached the peer in the Tag Registry panel and over REST',
      'keeper stopped: stale true, last-copy banner, rename refused 503 kind unreachable, every edit control disabled',
      'offline create: pending_sync true, pending badge, pending_sync_count 1',
      `keeper restarted with same-name tag: merge note ${note.id} naming two nodes; dismiss removed it`,
    ].join('\n'));
  } finally {
    await cleanup();
  }
});
