// scripts/test-stories/pwa/mobile-fit-seeds.test.mjs — node:test coverage for
// mobile-fit-seeds.mjs. No browser needed: the seed module is pure data plus
// a route-handler factory, and installSeed is exercised against a fake page
// object that only records route()/unroute() calls.
import test from 'node:test';
import assert from 'node:assert/strict';

import {
  STATE_NAMES,
  buildSeed,
  buildRouteHandler,
  clearSeed,
  installSeed,
  makeLocalStats,
  makePeer,
  makePeerSnapshot,
  peerStatsKey,
} from './mobile-fit-seeds.mjs';

const PEER_STAT_PREFIX = '/api/observer/peers/';
const PEER_STAT_SUFFIX = '/stats';

function peerStatsKeys(seed) {
  return Object.keys(seed.responses)
    .filter((k) => k.startsWith(PEER_STAT_PREFIX) && k.endsWith(PEER_STAT_SUFFIX));
}

// Peers list only exists on a 200 response (stats-failed errors it out).
function peersList(seed) {
  const entry = seed.responses['/api/observer/peers'];
  if (entry.status !== 200) return [];
  return entry.body.peers.filter((p) => !p.is_self);
}

// ── state inventory ─────────────────────────────────────────────────────────

test('STATE_NAMES lists exactly the 16 required states', () => {
  assert.deepEqual(STATE_NAMES, [
    'no-peers', 'one-peer', 'three-peers', 'ten-peers', 'long-hostname',
    'peer-errored', 'peer-no-gpu', 'peer-multi-gpu',
    'ebpf-unavailable', 'ebpf-many-processes', 'ebpf-long-names',
    'sessions-empty', 'sessions-large', 'stats-slow', 'stats-failed',
    'numbers-large',
  ]);
});

test('every state builds a fully serializable seed with well-shaped responses', () => {
  for (const name of STATE_NAMES) {
    const seed = buildSeed(name);
    assert.ok(seed.responses && typeof seed.responses === 'object', name + ': has responses map');
    assert.ok(Object.keys(seed.responses).length >= 15, name + ': seeds the core endpoints');
    const roundTripped = JSON.parse(JSON.stringify(seed)); // throws on non-serializable
    assert.deepEqual(Object.keys(roundTripped.responses).sort(), Object.keys(seed.responses).sort());
    for (const [key, target] of Object.entries(seed.responses)) {
      assert.ok(Number.isInteger(target.status) && target.status >= 200 && target.status < 600,
        name + ' ' + key + ': status code');
      assert.ok(typeof target.delayMs === 'number' && target.delayMs >= 0,
        name + ' ' + key + ': delayMs >= 0');
      assert.ok(target.body !== undefined, name + ' ' + key + ': body present');
    }
    // core endpoints always seeded
    for (const core of ['/api/stats', '/api/stats?v=2', '/api/observer/peers',
      '/api/observer/envelopes', '/api/observer/stats', '/api/config']) {
      assert.ok(seed.responses[core], name + ': has ' + core);
    }
    // every non-self peer has a snapshot endpoint
    for (const p of peersList(seed)) {
      assert.ok(seed.responses[peerStatsKey(p.name)], name + ': snapshot for ' + p.name);
    }
  }
});

test('buildSeed throws for unknown states', () => {
  assert.throws(() => buildSeed('nope'), /unknown mobile-fit seed state/);
});

// ── peer counts ──────────────────────────────────────────────────────────────

test('peer counts: no-peers=0, one-peer=1, three-peers=3, ten-peers=10', () => {
  assert.equal(peersList(buildSeed('no-peers')).length, 0);
  assert.equal(peersList(buildSeed('one-peer')).length, 1);
  assert.equal(peersList(buildSeed('three-peers')).length, 3);
  assert.equal(peersList(buildSeed('ten-peers')).length, 10);
});

// ── long-hostname ────────────────────────────────────────────────────────────

test('long-hostname: 70-char local, one 80-char and one 40-char peer', () => {
  const seed = buildSeed('long-hostname');
  assert.equal(seed.responses['/api/stats'].body.hostname.length, 70);
  const names = peersList(seed).map((p) => p.name).map((n) => n.length).sort((a, b) => a - b);
  assert.deepEqual(names, [40, 80]);
});

// ── peer-errored ─────────────────────────────────────────────────────────────

test('peer-errored: errored 502 + never-pushed (no last_push_at) + stale', () => {
  const seed = buildSeed('peer-errored');
  const byName = Object.fromEntries(peersList(seed).map((p) => [p.name, p]));
  // errored peer: stats endpoint is 502 with a long message
  const errResp = seed.responses[peerStatsKey('errored')];
  assert.ok(errResp.status >= 500, 'errored peer stats is 5xx');
  assert.ok(errResp.body.error.length > 100, 'errored peer error is long');
  // never-pushed peer: last_push_at absent/null, stats endpoint 5xx
  assert.equal(byName['never-pushed'].last_push_at, null);
  assert.ok(seed.responses[peerStatsKey('never-pushed')].status >= 500);
  // stale peer: has a last_push_at (hours old vs the fixed clock)
  assert.ok(byName.stale.last_push_at);
});

// ── GPU variants ─────────────────────────────────────────────────────────────

test('peer-no-gpu: local host and all peers have zero GPUs', () => {
  const seed = buildSeed('peer-no-gpu');
  assert.equal(seed.responses['/api/stats'].body.gpu_name, '');
  assert.equal(seed.responses['/api/stats'].body.gpu_mem_total_mb, 0);
  for (const key of peerStatsKeys(seed)) {
    assert.equal(seed.responses[key].body.gpu.length, 0, key);
  }
});

test('peer-multi-gpu: one peer with 4 GPUs, one with 8, long model names', () => {
  const seed = buildSeed('peer-multi-gpu');
  const quads = seed.responses[peerStatsKey('quad-gpu')].body.gpu;
  const octos = seed.responses[peerStatsKey('octo-gpu')].body.gpu;
  assert.equal(quads.length, 4);
  assert.equal(octos.length, 8);
  assert.ok(quads.every((g) => g.name.length >= 40), 'long GPU model names');
});

// ── eBPF states ──────────────────────────────────────────────────────────────

test('ebpf-unavailable: kprobes not loaded, empty per-process list', () => {
  const seed = buildSeed('ebpf-unavailable');
  const ebpf = seed.responses['/api/stats?v=2'].body.host.ebpf;
  assert.equal(ebpf.kprobes_loaded, false);
  assert.equal(ebpf.capability, false);
  assert.ok(ebpf.message.length > 60);
  assert.deepEqual(seed.responses['/api/stats?v=2'].body.net.per_process, []);
  assert.equal(seed.responses['/api/stats'].body.ebpf_active, false);
});

test('ebpf-many-processes: 300+ rows and a large serialized seed', () => {
  const seed = buildSeed('ebpf-many-processes');
  const procs = seed.responses['/api/stats?v=2'].body.net.per_process;
  assert.ok(procs.length >= 300, 'has ' + procs.length + ' rows');
  assert.ok(procs.every((p) => p.comm && p.pid > 0 && p.rx_bps >= 0 && p.tx_bps >= 0));
  assert.ok(JSON.stringify(seed).length >= 20000, 'seed JSON is large');
});

test('ebpf-long-names: 60-char comm, 120-char space-free cmdline', () => {
  const seed = buildSeed('ebpf-long-names');
  const procs = seed.responses['/api/stats?v=2'].body.net.per_process;
  assert.ok(procs.length >= 3);
  for (const p of procs) {
    assert.ok(p.comm.length >= 60, 'comm length ' + p.comm.length);
    assert.ok(p.cmdline.length >= 120, 'cmdline length ' + p.cmdline.length);
    assert.equal(p.cmdline.includes(' '), false, 'cmdline has no spaces');
  }
});

// ── session counts ───────────────────────────────────────────────────────────

test('sessions-empty: zero counters and empty session/envelope lists', () => {
  const seed = buildSeed('sessions-empty');
  const stats = seed.responses['/api/stats'].body;
  assert.equal(stats.active_sessions, 0);
  assert.equal(stats.total_sessions, 0);
  assert.deepEqual(stats.session_stats, []);
  assert.ok(stats.comm_stats.every((c) => c.active_sessions === 0 && c.total_sessions === 0));
  assert.deepEqual(seed.responses['/api/observer/envelopes'].body.envelopes, []);
  assert.deepEqual(seed.responses['/api/observer/envelopes/all-peers'].body.by_peer, {});
  assert.deepEqual(seed.responses['/api/channel/diagnostics'].body.sessions, []);
});

test('sessions-large: counters >1M and several hundred envelopes', () => {
  const seed = buildSeed('sessions-large');
  const stats = seed.responses['/api/stats'].body;
  assert.ok(stats.active_sessions > 1_000_000);
  assert.ok(stats.total_sessions > stats.active_sessions, 'total > active');
  assert.ok(stats.session_stats.length >= 100, 'hundreds of session rows');
  assert.ok(seed.responses['/api/observer/envelopes'].body.envelopes.length >= 100);
});

// ── stats-slow / stats-failed ────────────────────────────────────────────────

test('stats-slow: delayMs >= 1000 on all stats and peer endpoints, 0 elsewhere', () => {
  const seed = buildSeed('stats-slow');
  const delayed = ['/api/stats', '/api/stats?v=2', ...peerStatsKeys(seed)];
  assert.ok(delayed.length >= 3, 'has stats + peer keys');
  for (const key of delayed) {
    assert.ok(seed.responses[key].delayMs >= 1000, key + ' delays >= 1000ms');
  }
  const noDelay = Object.entries(seed.responses)
    .filter(([key]) => !delayed.includes(key));
  assert.ok(noDelay.every(([, t]) => t.delayMs === 0), 'other endpoints are instant');
});

test('stats-failed: stats and peer endpoints 5xx with long error text', () => {
  const seed = buildSeed('stats-failed');
  for (const key of ['/api/stats', '/api/stats?v=2', '/api/observer/peers', ...peerStatsKeys(seed)]) {
    const target = seed.responses[key];
    assert.ok(target.status >= 500, key + ' is 5xx');
    assert.ok(target.body.error.length > 100, key + ' has long error text');
  }
});

// ── numbers-large ────────────────────────────────────────────────────────────

test('numbers-large: PB-scale bytes, billions of counters, 8x80GB GPUs', () => {
  const seed = buildSeed('numbers-large');
  const stats = seed.responses['/api/stats'].body;
  assert.ok(stats.mem_total >= 10 ** 15, 'mem_total >= 1 PB');
  assert.ok(stats.disk_total >= 10 ** 15, 'disk_total >= 1 PB');
  assert.ok(stats.active_sessions >= 10 ** 9, 'sessions in the billions');
  const gpuKey = peerStatsKey('big-peer');
  const gpu = seed.responses[gpuKey].body.gpu;
  assert.equal(gpu.length, 8);
  assert.ok(gpu.every((g) => g.mem_total_mb >= 80 * 1024), 'each GPU has 80GB+ memory');
});

// ── reusable builders ────────────────────────────────────────────────────────

test('makeLocalStats / makePeer / makePeerSnapshot merge overrides', () => {
  const local = makeLocalStats({ hostname: 'h', mem_total: 42 });
  assert.equal(local.hostname, 'h');
  assert.equal(local.mem_total, 42);
  assert.ok(Array.isArray(local.session_stats));

  const peer = makePeer({ name: 'zed', shape: 'C' });
  assert.equal(peer.name, 'zed');
  assert.equal(peer.is_self, false);

  const snap = makePeerSnapshot({ cpu: { pct: 99 } });
  assert.equal(snap.cpu.pct, 99);
  assert.ok(Array.isArray(snap.gpu) && snap.gpu.length === 1);
  assert.ok(Array.isArray(snap.envelopes) && snap.envelopes.length >= 1);
});

test('baselined seeds are deterministic (no Math.random / Date.now drift)', () => {
  for (const name of ['no-peers', 'three-peers', 'peer-errored']) {
    assert.equal(JSON.stringify(buildSeed(name)), JSON.stringify(buildSeed(name)), name);
  }
});

// ── installSeed / clearSeed against a fake page ─────────────────────────────

function makeFakePage() {
  const calls = { routes: [], unroutes: [] };
  return {
    calls,
    route: (pattern, handler) => {
      calls.routes.push({ pattern, handler });
      return Promise.resolve();
    },
    unroute: (pattern) => {
      calls.unroutes.push(pattern);
      return Promise.resolve();
    },
  };
}

test('installSeed registers a **/api/** route handler', async () => {
  const page = makeFakePage();
  const handler = await installSeed(page, 'one-peer');
  assert.equal(page.calls.routes.length, 1);
  assert.equal(page.calls.routes[0].pattern, '**/api/**');
  assert.equal(typeof handler, 'function');
});

test('installSeed replaces any previously registered handler', async () => {
  const page = makeFakePage();
  await installSeed(page, 'one-peer');
  await installSeed(page, 'ten-peers');
  assert.equal(page.calls.routes.length, 2, 'second state re-registers');
  assert.ok(page.calls.unroutes.length >= 1, 'previous registration is unroute-d first');
});

test('clearSeed unroutes the seed pattern', async () => {
  const page = makeFakePage();
  await installSeed(page, 'no-peers');
  const before = page.calls.unroutes.length;
  clearSeed(page);
  assert.equal(page.calls.unroutes.length, before + 1);
  assert.equal(page.calls.unroutes[before], '**/api/**');
});

// ── route handler behavior (fake route objects, no timers) ──────────────────

function makeFakeRoute(url) {
  const out = { fulfilled: null, fellBack: false };
  const route = {
    request: () => ({ url }),
    fallback: async () => { out.fellBack = true; },
    fulfill: async (opts) => {
      out.fulfilled = {
        status: opts.status,
        contentType: opts.contentType,
        body: JSON.parse(opts.body),
      };
      return Promise.resolve();
    },
  };
  return { route, out };
}

test('handler fulfills the exact seeded key and falls back otherwise', async () => {
  const seed = buildSeed('one-peer');
  const handler = buildRouteHandler(seed);

  // plain /api/stats
  let { route, out } = makeFakeRoute('http://127.0.0.1:8080/api/stats');
  await handler(route);
  assert.ok(!out.fellBack);
  assert.equal(out.fulfilled.status, 200);
  assert.equal(out.fulfilled.contentType, 'application/json');
  assert.ok(out.fulfilled.body.hostname);

  // v=2 variant resolves to its own key
  ({ route, out } = makeFakeRoute('http://127.0.0.1:8080/api/stats?v=2'));
  await handler(route);
  assert.ok(!out.fellBack);
  assert.ok(out.fulfilled.body.net.per_process.length >= 1);

  // per-peer snapshot key
  ({ route, out } = makeFakeRoute('http://127.0.0.1:8080/api/observer/peers/alpha/stats'));
  await handler(route);
  assert.ok(!out.fellBack);
  assert.ok(out.fulfilled.body.cpu);

  // unknown endpoint falls back to the real network/stack
  ({ route, out } = makeFakeRoute('http://127.0.0.1:8080/api/unseeded/path'));
  await handler(route);
  assert.ok(out.fellBack);

  // non-api URL falls back
  ({ route, out } = makeFakeRoute('http://127.0.0.1:8080/static/app.js'));
  await handler(route);
  assert.ok(out.fellBack);
});

test('error states fulfill with 5xx status and error body', async () => {
  const seed = buildSeed('stats-failed');
  const handler = buildRouteHandler(seed);
  const { route, out } = makeFakeRoute('http://127.0.0.1:8080/api/stats');
  await handler(route);
  assert.equal(out.fulfilled.status, 500);
  assert.ok(out.fulfilled.body.error.length > 100);
});

// ── /api/observer/peers self entry (identity is the hostname) ──────────────

test('every seed builds /api/observer/peers/free with a peers array', () => {
  for (const name of STATE_NAMES) {
    const seed = buildSeed(name);
    const entry = seed.responses['/api/observer/peers/free'];
    assert.ok(entry, name + ': has /api/observer/peers/free');
    assert.ok(Array.isArray(entry.body.peers), name + ': free body has a peers array');
    // the self entry never counts as a free observer
    assert.ok(entry.body.peers.every((p) => !p.is_self), name + ': no self entry in free list');
  }
});

test('peers list always has exactly one is_self entry named after the host', () => {
  for (const name of STATE_NAMES) {
    const seed = buildSeed(name);
    const entry = seed.responses['/api/observer/peers'];
    if (entry.status !== 200) continue; // stats-failed legitimately 5xx's it
    const peers = entry.body.peers;
    const self = peers.filter((p) => p.is_self);
    const localName = seed.responses['/api/stats'].body.hostname;
    assert.equal(self.length, 1, name + ': exactly one self');
    assert.equal(self[0].name, localName, name + ': self uses the hostname');
  }
});
