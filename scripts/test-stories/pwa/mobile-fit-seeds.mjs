// scripts/test-stories/pwa/mobile-fit-seeds.mjs — deterministic API seeds for
// the datawatch PWA Observer view (renderObserverView). Lets a single browser
// fake every API response the Observer view loads so the mobile-fit layout
// suite never needs a second machine (peer) or a real daemon.
//
// Pure module: no browser import here. `installSeed(page, state)` is the only
// function that touches a Playwright page; it works against any object that
// implements `route(pattern, handler)` and `unroute(pattern)` (that is all the
// node:test fake page needs).
//
// Seed shape:  { responses: { "<pathname>[?v=2]": { status, body, delayMs } } }
// Keys are request PATHNAMES (query stripped except the special `v=2` variant
// of /api/stats that renderStatsData/loadEBPFStatus use for the eBPF fields).
// Bodies are plain JSON-serializable values — no functions, no undefined.
//
// Usage (browser test):
//   import { installSeed, clearSeed, buildSeed, STATE_NAMES } from './mobile-fit-seeds.mjs';
//   await installSeed(page, 'three-peers');   // registers **/api/** handler
//   ... render observer view, take screenshots/checks ...
//   await clearSeed(page);
//
// Named states (STATE_NAMES) and the layout edge each one stresses:
//   no-peers             zero federated peers — every peers/envelopes table empty
//   one-peer             single fresh peer — one-row tables everywhere
//   three-peers          three peers — multi-row grid cards
//   ten-peers            ten peers — long list scrolling inside fixed cards
//   long-hostname        70-char local hostname, 80/40-char peer names — ellipsis
//   peer-errored         a 5xx peer, a never-pushed peer, a >60s-stale peer
//   peer-no-gpu          peers + local host with no GPU rows at all
//   peer-multi-gpu       peers with 4 and 8 GPU rows — vertical GPU overflow
//   ebpf-unavailable     eBPF off/not loaded — degraded banner, empty net table
//   ebpf-many-processes  320 per-process rows — long table inside small card
//   ebpf-long-names      60-char comm + 120-char space-free cmdline strings
//   sessions-empty       every session counter zero, every session list empty
//   sessions-large       counters >1M, several hundred envelope rows
//   stats-slow           6s network delay on stats/peer endpoints — spinner
//   stats-failed         5xx with long error text — error copy, no crash
//   numbers-large        PB-scale bytes, billions of sessions, 8x80GB GPUs
//
// Conventions: ES module, no new npm deps, no tracker ids in identifiers.
// The baseline (no-peers) is fully deterministic: no Math.random, no
// Date.now() in any body — timestamps are fixed so a given state always
// produces byte-identical JSON on every run.

const GB = 1024 ** 3;
const MB = 1024 ** 2;

// Fixed reference clock (2024-10-27T03:00:00Z + 1000s) so every run of a
// state is byte-identical. Peers therefore always render as "stale" (age
// > 60s) — that is deterministic and fine for layout checks.
const T0_S = 1730010000;
const T0_MS = T0_S * 1000;
const T0_ISO = new Date(T0_MS).toISOString();

const LONG_ERROR =
  'upstream stats probe failed: peer proxy pool exhausted after 30000ms — ' +
  'the last relay hop (edge-eu-2) reset the connection (104) mid-stream while ' +
  'its kernel socket buffer was still draining three in-flight push batches; ' +
  'retries are backoff-looping until the remote observer process restarts';

// ── reusable body builders ──────────────────────────────────────────────────

export const makeLocalStats = (overrides = {}) => ({
  hostname: 'mobile-fit-host',
  timestamp: T0_S,
  cpu_cores: 8,
  cpu_load_avg_1: 2.5,
  cpu_load_avg_5: 1.8,
  cpu_load_avg_15: 1.2,
  mem_used: 6 * GB,
  mem_total: 16 * GB,
  disk_used: 120 * GB,
  disk_total: 512 * GB,
  swap_used: 0,
  swap_total: 0,
  gpu_name: 'NVIDIA GeForce RTX 4090',
  gpu_util_pct: 37,
  gpu_temp: 58,
  gpu_mem_used_mb: 10240,
  gpu_mem_total_mb: 24576,
  net_rx_bytes: 123456789,
  net_tx_bytes: 98765432,
  uptime_seconds: 7200,
  daemon_rss_bytes: 64 * MB,
  goroutines: 12,
  open_fds: 42,
  bound_interfaces: ['127.0.0.1'],
  web_port: 8080,
  tls_port: 0,
  tls_enabled: false,
  mcp_sse_port: 0,
  mcp_sse_host: '127.0.0.1',
  daemon_version: 'v0.9.9',
  tmux_sessions: 2,
  orphaned_tmux: [],
  rtk_installed: false,
  quality_gate_runs: 0,
  memory_enabled: false,
  memory_total_count: 0,
  active_sessions: 2,
  total_sessions: 12,
  session_stats: makeLocalSessions(2),
  comm_stats: makeCommStats(),
  ebpf_enabled: true,
  ebpf_active: true,
  ebpf_message: '',
  ...overrides,
});

export const makePeer = (overrides = {}) => ({
  name: 'alpha',
  shape: 'A',
  last_push_at: T0_ISO,
  is_self: false,
  ...overrides,
});

export const makePeerSnapshot = (overrides = {}) => ({
  host: { name: 'peer-host', os: 'linux', arch: 'arm64', uptime_seconds: 3600 },
  cpu: { pct: 12.5, load1: 1.2, load5: 0.9, load15: 0.7 },
  mem: { used_bytes: 4 * GB, total_bytes: 16 * GB },
  gpu: [makeGpu()],
  envelopes: [
    makeEnvelope('session', 'session:1001', 'claude code'),
    makeEnvelope('backend', 'backend:9001', 'mcp bridge'),
  ],
  ...overrides,
});

export const makeGpu = (overrides = {}) => ({
  name: 'NVIDIA GeForce RTX 4090',
  util_pct: 37,
  temp_c: 58,
  power_w: 112,
  mem_used_mb: 10240 * 1,     // 10 GB
  mem_total_mb: 24576,       // 24 GB
  ...overrides,
});

export const makeEnvelope = (kind, id, label, overrides = {}) => ({
  kind, id, label,
  cpu_pct: 4.2,
  rss_bytes: 256 * MB,
  net_rx_bps: 1024,
  net_tx_bps: 512,
  ...overrides,
});

export const makeCrossHostEnvelope = (id, label, peerName, overrides = {}) => ({
  id, kind: 'session', label,
  listen_addrs: [{ ip: '10.0.0.5', port: 49152 }],
  outbound_edges: [{ target_ip: '10.0.1.20', target_port: 8080 }],
  callers: [
    { caller: `session:2001:${peerName}-c1`, caller_kind: 'session', conns: 1 },
    { caller: 'local:9000', caller_kind: 'backend', conns: 2 },
  ],
  cpu_pct: 8.8,
  rss_bytes: 512 * MB,
  ...overrides,
});

export const makeProcess = (pid, comm, cmdline, overrides = {}) => ({
  pid, comm, cmdline,
  cpu_pct: 3.3,
  rss_bytes: 128 * MB,
  rx_bps: 2048,
  tx_bps: 1024,
  ...overrides,
});

const makeLocalSessions = (n) =>
  Array.from({ length: n }, (_, i) => ({
    session_id: String(1001 + i),
    name: 'session ' + String(i + 1),
    backend: 'tmux',
    state: 'active',
    rss_bytes: (i + 1) * 128 * MB,
    uptime: '(2h 1' + i + 'm)',
    pane_pid: 100 + (i + 1) * 7,
    cpu_percent: 2 + i,
    net_rx_bytes: 1048576,
    net_tx_bytes: 524288,
  }));

const makeCommStats = () => [
  {
    name: 'ollama', type: 'llm', enabled: true, endpoint: 'http://127.0.0.1:11434',
    active_sessions: 1, total_sessions: 6, avg_duration_sec: 420,
    avg_prompts: 3.5, connections: 0, msg_recv: 0, msg_sent: 0,
    bytes_in: 0, bytes_out: 0, errors: 0, last_active: T0_S,
  },
  {
    name: 'matrix', type: 'messaging', enabled: true, endpoint: 'https://matrix.example.org',
    active_sessions: 0, total_sessions: 0, avg_duration_sec: 0,
    avg_prompts: 0, connections: 1, msg_recv: 3, msg_sent: 2,
    bytes_in: 2048, bytes_out: 1024, errors: 0, last_active: T0_S,
  },
  {
    name: 'mcp', type: 'infra', enabled: true, endpoint: 'stdio',
    active_sessions: 0, total_sessions: 0, avg_duration_sec: 0,
    avg_prompts: 0, connections: 2, msg_recv: 40, msg_sent: 41,
    bytes_in: 65536, bytes_out: 65536, errors: 0, last_active: T0_S,
  },
];

const makeClusterNodes = (names) =>
  names.map((name) => ({
    name,
    cpu_pct: 33,
    mem_pct: 61,
    ready: true,
    pressure: [],
    pod_count: 3,
  }));

const makeAnalyticsBuckets = (n) =>
  Array.from({ length: n }, (_, i) => ({
    date: '2024-10-' + String(14 + i).padStart(2, '0'),
    session_count: 12 + i,
    completed: 11 + i,
    failed: 1,
    killed: 0,
  }));

const makeAuditEntries = (n) =>
  Array.from({ length: n }, (_, i) => ({
    ts: new Date(T0_MS - (n - i) * 3600 * 1000).toISOString(),
    action: i % 2 === 0 ? 'session.start' : 'config.update',
    actor: i % 2 === 0 ? 'operator@example.org' : 'daemon',
    session_id: i % 2 === 0 ? 'abcdef1234567890' : '',
    details: i === 0 ? { path: '/etc/datawatch/config.toml' } : {},
  }));

const makeMemories = (n) =>
  Array.from({ length: n }, (_, i) => ({
    id: i + 1,
    role: i % 2 === 0 ? 'manual' : 'session',
    created_at: new Date(T0_MS - (n - i) * 86400 * 1000).toISOString(),
    content: 'Seeded memory ' + (i + 1) + ': note written by the operator on 2024-10-27. ' +
      'Details: verify the observer panel renders the same row widths across all viewports.',
    tags: 'observer,mobile-fit',
    similarity: null,
  }));

function responseOk(body, delayMs = 0) {
  return { status: 200, body, delayMs };
}
function responseErr(status, error, delayMs = 0) {
  return { status, body: { error }, delayMs };
}

export const peerStatsKey = (peerName) =>
  '/api/observer/peers/' + encodeURIComponent(peerName) + '/stats';

// ── base seed: one coherent dataset across every endpoint ──────────────────

function baseSeed({ peers = [], local = {}, clusterNames = null } = {}) {
  const responses = {};
  const localName = local.hostname || 'mobile-fit-host';

  // Monitor card (loadStatsPanel → renderStatsData)
  const localStats = makeLocalStats(local);
  responses['/api/stats'] = responseOk(localStats);

  // eBPF status + per-process network (loadEBPFStatus / loadEBPFNetworkTraffic)
  responses['/api/stats?v=2'] = responseOk({
    ...localStats,
    host: { name: localName, os: 'linux', arch: 'arm64', ebpf: makeEbpfiState() },
    net: { per_process: makePerProcess(6) },
  });

  // Plugins strip (loadPluginsStatus)
  responses['/api/plugins'] = responseOk({
    plugins: [
      { name: 'observer', version: '0.9.0', enabled: true,
        hooks: ['push', 'pull'], invoke_count: 42, last_error: '' },
    ],
    native: [
      { name: 'datawatch-observer', version: '0.9.9', enabled: true,
        description: 'Observer plugin', message: '' },
    ],
  });

  // ACME card (loadAcmeHealthCard)
  responses['/api/acme/status'] = responseOk({
    enabled: true,
    domains: [
      {
        domain: 'datawatch.example.org',
        issued: '2024-09-27',
        in_flight: false,
        not_after: '2024-12-26',
        last_error: '',
      },
    ],
  });

  // LLM backends card (loadBackendHealthCard)
  responses['/api/backends'] = responseOk({
    llm: [
      { name: 'ollama', available: true, version: '0.9.0', compute_nodes: [] },
      { name: 'vllm', available: false, version: '', compute_nodes: [] },
    ],
  });

  // Envelopes card (loadObserverEnvelopesCard)
  responses['/api/observer/envelopes'] = responseOk({
    envelopes: [
      makeEnvelope('session', 'session:1001', 'session 1'),
      makeEnvelope('session', 'session:1002', 'session 2'),
      makeEnvelope('backend', 'backend:9001', 'mcp bridge'),
    ],
  });

  // Channel bridge status (loadChannelBridge)
  responses['/api/channel/info'] = responseOk({
    kind: 'js',
    ready: true,
    path: '/home/ops/.datawatch/channel/channel.js',
    hint: '',
    node_path: '/usr/bin/node',
    stdio_enabled: true,
    sse_enabled: false,
    stale_mcp_json: [],
  });

  // Channel diagnostics (loadChannelDiagnostics)
  responses['/api/channel/diagnostics'] = responseOk({
    sessions: [
      {
        session_id: '1001', session_name: 'session 1', channel_port: 49152,
        bridge_alive: true, probe_error: '',
      },
      {
        session_id: '1002', session_name: 'session 2', channel_port: 49153,
        bridge_alive: false, probe_error: 'bridge exited (137): OOM kill',
      },
    ],
    hints: ['session 2 bridge not alive — restart with `datawatch channel attach 1002`'],
  });

  // Peers card (renderObserverPeersCard) — self entry + seeded peers
  responses['/api/observer/peers'] = responseOk({
    peers: [
      { name: localName, shape: 'A', last_push_at: T0_ISO, is_self: true },
      ...peers,
    ],
  });

  // Compute nodes + federation mapping (loadPeerResourceOverview)
  responses['/api/compute/nodes'] = responseOk({
    nodes: peers.map((p) => ({
      name: p.name + '-node',
      kind: 'gpu',
      observer_peer: p.name,
    })),
  });
  responses['/api/federation/meta-peers'] = responseOk({ by_node: {}, unbound: [] });

  // Per-peer snapshot (loadSystemStatsGrid / showObserverPeerSnapshot)
  for (const p of peers) {
    responses[peerStatsKey(p.name)] = responseOk(makePeerSnapshot({
      host: { name: p.name, os: 'linux', arch: 'arm64', uptime_seconds: 7200 },
      envelopes: [makeEnvelope('session', 'session:' + p.name, p.name + ' main')],
    }));
  }

  // Cross-host envelope modal (showCrossHostView)
  responses['/api/observer/envelopes/all-peers'] = responseOk({
    by_peer: Object.fromEntries(
      peers.map((p) => [p.name, [makeCrossHostEnvelope('session:2001', p.name + ' main', p.name)]]),
    ),
  });
  responses['/api/observer/envelope'] = responseOk({
    processes: [
      makeProcess(101, 'node', '/usr/bin/node /home/ops/.datawatch/channel/channel.js'),
      makeProcess(202, 'claude', '/usr/local/bin/claude --model seed-model'),
    ],
  });

  // Observer stats (loadObserverClusterNodes + peers card tiles)
  const clusterNamesResolved = clusterNames === null ? [localName, ...peers.map((p) => p.name)] : clusterNames;
  responses['/api/observer/stats'] = responseOk({
    version: 'v0.9.9',
    peers_active: peers.length,
    started_unix: T0_S,
    cluster: { nodes: makeClusterNodes(clusterNamesResolved) },
  });

  // Observer config (renderObserverPeersCard config row)
  responses['/api/observer/config'] = responseOk({
    enabled: true,
    interval_ms: 5000,
    push_url: 'https://datawatch.example.org/observer',
  });

  // Communication backends (loadCommBackendsStatus reads /api/config)
  responses['/api/config'] = responseOk({
    telegram: { enabled: false },
    discord: { enabled: false },
    slack: { enabled: false },
    matrix: { enabled: false },
    ntfy: { enabled: false },
    email: { enabled: false },
    twilio: { enabled: false },
    github_webhook: { enabled: false },
    webhook: { enabled: false },
    dns_channel: { enabled: false },
  });
  responses['/api/matrix/status'] = responseOk({ connected: false, user_id: '' });

  // Schedules / cooldown
  responses['/api/schedules'] = responseOk([]);
  responses['/api/cooldown'] = responseOk({ active: false, until_unix_ms: 0, reason: '' });

  // Analytics / audit
  responses['/api/analytics'] = responseOk({
    success_rate: 0.965,
    buckets: makeAnalyticsBuckets(14),
  });
  responses['/api/audit'] = responseOk({ entries: makeAuditEntries(5) });

  // Memory browser + scope inventory (listMemories / loadMemoryScopeInventory)
  responses['/api/memory/list'] = responseOk(makeMemories(3));
  responses['/api/memory/search'] = responseOk(makeMemories(3));
  responses['/api/memory/scopes/inventory'] = responseOk({
    scopes: [
      { role: 'manual', session_id: '', count: 2 },
      { role: 'session', session_id: '1001', count: 1 },
    ],
  });

  return { responses };
}

// eBPF state for /api/stats?v=2 (loadEBPFStatus reads host.ebpf.*)
function makeEbpfiState() {
  return {
    enabled: true,
    configured: true,
    capability: true,
    kprobes_loaded: true,
    active: true,
    message: '',
  };
}

// Per-process network table (loadEBPFNetworkTraffic sorts by rx_bps + tx_bps)
function makePerProcess(n) {
  return Array.from({ length: n }, (_, i) => makeProcess(
    1000 + i,
    'worker-' + String(i).padStart(3, '0'),
    '/usr/local/bin/worker --id ' + String(i) + ' --data /var/data/worker-' + String(i) + '.db',
    { rx_bps: (n - i) * 1024, tx_bps: (n - i) * 512 },
  ));
}

// ── named states ─────────────────────────────────────────────────────────────

const PEER_NAMES = ['alpha', 'bravo', 'charlie', 'delta', 'echo', 'foxtrot', 'golf', 'hotel', 'india', 'juliet'];
const repeat = (ch, len) => ch.repeat(len);

export const STATE_NAMES = [
  'no-peers',
  'one-peer',
  'three-peers',
  'ten-peers',
  'long-hostname',
  'peer-errored',
  'peer-no-gpu',
  'peer-multi-gpu',
  'ebpf-unavailable',
  'ebpf-many-processes',
  'ebpf-long-names',
  'sessions-empty',
  'sessions-large',
  'stats-slow',
  'stats-failed',
  'numbers-large',
];

function peersOf(count) {
  return PEER_NAMES.slice(0, count).map((name) => makePeer({ name, shape: count % 2 === 0 ? 'B' : 'A' }));
}

// stats-slow: apply the network-delay field to /api/stats and every peer stats key
function withSlowStats(seed, delayMs) {
  for (const key of Object.keys(seed.responses)) {
    if (key === '/api/stats' || key === '/api/stats?v=2' ||
        (key.startsWith('/api/observer/peers/') && key.endsWith('/stats'))) {
      seed.responses[key].delayMs = delayMs;
    }
  }
  return seed;
}

// stats-failed: error out /api/stats and every peer stats endpoint
function withFailedStats(seed) {
  seed.responses['/api/stats'] = responseErr(500, LONG_ERROR);
  seed.responses['/api/stats?v=2'] = responseErr(500, LONG_ERROR);
  for (const key of Object.keys(seed.responses)) {
    if (key.startsWith('/api/observer/peers/') && key.endsWith('/stats')) {
      seed.responses[key] = responseErr(502, LONG_ERROR);
    }
  }
  seed.responses['/api/observer/peers'] = responseErr(503, LONG_ERROR);
  return seed;
}

const STATE_BUILDERS = {
  // no peers at all: every table empty, only the local row remains
  'no-peers': () => baseSeed({ peers: [] }),

  // single peer: one-row peer table in every card
  'one-peer': () => baseSeed({ peers: peersOf(1) }),

  // three peers: multi-row peer cards
  'three-peers': () => baseSeed({ peers: peersOf(3) }),

  // ten peers: long list inside fixed-height cards (scrolling stress)
  'ten-peers': () => baseSeed({ peers: peersOf(10) }),

  // 70-char local hostname, 80-char and 40-char peer names (ellipsis/layout)
  'long-hostname': () => baseSeed({
    peers: [
      makePeer({ name: 'peer-' + repeat('h', 75), shape: 'B' }),   // 80 chars
      makePeer({ name: 'peer-' + repeat('s', 35), shape: 'A' }),   // 40 chars
    ],
    local: { hostname: 'mobilefit-' + repeat('x', 60) },          // 70 chars
  }),

  // one 502 peer, one never-pushed peer, one stale >60s peer
  'peer-errored': () => {
    const errored = makePeer({ name: 'errored', shape: 'B', last_push_at: T0_ISO });
    const never = makePeer({ name: 'never-pushed', shape: 'C', last_push_at: null });
    const stale = makePeer({ name: 'stale', shape: 'A', last_push_at: T0_ISO });
    const seed = baseSeed({ peers: [errored, never, stale] });
    seed.responses[peerStatsKey('errored')] = responseErr(502, LONG_ERROR);
    seed.responses[peerStatsKey('never-pushed')] = responseErr(504, LONG_ERROR);
    return seed;
  },

  // peers and the local host with no GPU rows at all
  'peer-no-gpu': () => {
    const peers = [
      makePeer({ name: 'no-gpu-a', shape: 'A' }),
      makePeer({ name: 'no-gpu-b', shape: 'B' }),
    ];
    const seed = baseSeed({
      peers,
      local: {
        gpu_name: '', gpu_util_pct: 0, gpu_temp: 0,
        gpu_mem_used_mb: 0, gpu_mem_total_mb: 0,
      },
    });
    for (const key of Object.keys(seed.responses)) {
      if (key.startsWith('/api/observer/peers/') && key.endsWith('/stats')) {
        seed.responses[key].body.gpu = [];
      }
    }
    return seed;
  }
};

// Remaining state builders (grouped after the initial ones for readability).


// ── remaining state builders ─────────────────────────────────────────────────

STATE_BUILDERS['peer-multi-gpu'] = () => {
  const longGpuModel = 'NVIDIA Data Center Edition Hopper H100 SXM5 80GB PCIe Gen5 10-bit HDR Accelerator Card';
  const quads = [
    'NVIDIA Data Center Edition Ada Lovelace L40S 48GB PCIe Gen4 ECC Accelerator',
    'NVIDIA Data Center Edition Ada Lovelace L40S 48GB PCIe Gen4 ECC Accelerator 2',
    'NVIDIA Data Center Edition Ada Lovelace L40S 48GB PCIe Gen4 ECC Accelerator 3',
    'NVIDIA Data Center Edition Ada Lovelace L40S 48GB PCIe Gen4 ECC Accelerator 4',
  ];
  const octos = Array.from({ length: 8 }, (_, i) =>
    'NVIDIA Data Center Edition Hopper H100 SXM5 80GB PCIe Gen5 10-bit HDR Accelerator Card ' + String(i + 1).padStart(2, '0'));
  const quad = makePeer({ name: 'quad-gpu', shape: 'B' });
  const octo = makePeer({ name: 'octo-gpu', shape: 'C' });
  const seed = baseSeed({ peers: [quad, octo] });
  seed.responses[peerStatsKey('quad-gpu')].body.gpu =
    quads.map((name) => makeGpu({ name, mem_total_mb: 49152, mem_used_mb: 30720, util_pct: 71, temp_c: 74, power_w: 284 }));
  seed.responses[peerStatsKey('octo-gpu')].body.gpu =
    octos.map((name) => makeGpu({ name, mem_total_mb: 81920, mem_used_mb: 65536, util_pct: 88, temp_c: 81, power_w: 702 }));
  return seed;
};

STATE_BUILDERS['ebpf-unavailable'] = () => {
  const seed = baseSeed({ peers: peersOf(2) });
  seed.responses['/api/stats'].body.ebpf_enabled = true;
  seed.responses['/api/stats'].body.ebpf_active = false;
  seed.responses['/api/stats'].body.ebpf_message =
    'eBPF enabled but not active: kernel refused bpf() — missing CAP_BPF under the hardened runtime sandbox';
  seed.responses['/api/stats?v=2'].body.host.ebpf = {
    enabled: true,
    configured: true,
    capability: false,
    kprobes_loaded: false,
    active: false,
    message:
      'eBPF unavailable: missing CAP_BPF on this kernel (the hardened image strips ' +
      'the BPF capability from the observer sandbox; see the /boot/config CONFIG_BPF entry)',
  };
  seed.responses['/api/stats?v=2'].body.net.per_process = [];
  return seed;
};

STATE_BUILDERS['ebpf-many-processes'] = () => {
  const seed = baseSeed({ peers: peersOf(1) });
  seed.responses['/api/stats?v=2'].body.net.per_process = makePerProcess(320);
  return seed;
};

STATE_BUILDERS['ebpf-long-names'] = () => {
  const seed = baseSeed({ peers: peersOf(1) });
  const longComm = repeat('w', 56) + '-long';                              // 60 chars
  const longPath = '/usr/local/opt/datawatch/bin/' + repeat('p', 84) + '.worker'; // 121 chars, no spaces
  seed.responses['/api/stats?v=2'].body.net.per_process = [0, 1, 2, 3, 4].map((i) =>
    makeProcess(5000 + i, longComm + i, longPath + String(i), {
      rx_bps: 40960,
      tx_bps: 20480,
    }));
  return seed;
};

STATE_BUILDERS['sessions-empty'] = () => {
  const seed = baseSeed({
    peers: [],
    local: {
      active_sessions: 0,
      total_sessions: 0,
      session_stats: [],
      comm_stats: makeCommStats().map((c) => ({ ...c, active_sessions: 0, total_sessions: 0, connections: 0, msg_recv: 0, msg_sent: 0, bytes_in: 0, bytes_out: 0, enabled: false })),
      tmux_sessions: 0,
    },
  });
  seed.responses['/api/observer/envelopes'].body.envelopes = [];
  seed.responses['/api/observer/envelopes/all-peers'].body.by_peer = {};
  seed.responses['/api/channel/diagnostics'].body.sessions = [];
  seed.responses['/api/analytics'].body.buckets = makeAnalyticsBuckets(14).map((b) => ({ ...b, session_count: 0, completed: 0, failed: 0, killed: 0 }));
  return seed;
};

STATE_BUILDERS['sessions-large'] = () => {
  const peerCount = 3;
  const envCount = 300;
  const seed = baseSeed({
    peers: peersOf(peerCount),
    local: {
      active_sessions: 1200000,
      total_sessions: 2400000,
      session_stats: makeLocalSessions(300),
      comm_stats: makeCommStats().map((c, i) => ({ ...c, active_sessions: 1000000 + i * 1000, total_sessions: 5000000 + i * 250000, connections: 50 + i, msg_recv: 840000 + i, msg_sent: 910000 + i })),
      tmux_sessions: 12,
      net_rx_bytes: 40 * GB,
      net_tx_bytes: 22 * GB,
    },
  });
  const envelopes = Array.from({ length: envCount }, (_, i) =>
    makeEnvelope(i % 3 === 0 ? 'backend' : 'session', 'session:' + String(30000 + i), 'session ' + String(30000 + i), {
      cpu_pct: (i * 7) % 100,
      rss_bytes: ((i % 20) + 1) * 64 * MB,
      net_rx_bps: (i % 50 + 1) * 1024,
      net_tx_bps: (i % 30 + 1) * 512,
    }));
  seed.responses['/api/observer/envelopes'].body.envelopes = envelopes;
  seed.responses['/api/observer/envelopes/all-peers'].body.by_peer =
    Object.fromEntries(peersOf(peerCount)
      .map((p) => [p.name, envelopes.slice(0, 6).map((e, j) => makeCrossHostEnvelope(e.id, e.label, p.name, e))]));
  seed.responses['/api/analytics'].body.buckets = makeAnalyticsBuckets(14).map((b, i) => ({ ...b, session_count: 90000 + i * 137, completed: 80000 + i * 131, failed: 9000 + i, killed: 100 + i }));
  return seed;
};

STATE_BUILDERS['stats-slow'] = () => withSlowStats(baseSeed({ peers: peersOf(2) }), 6000);

STATE_BUILDERS['stats-failed'] = () => withFailedStats(baseSeed({ peers: peersOf(2) }));

STATE_BUILDERS['numbers-large'] = () => {
  const seed = baseSeed({
    peers: [makePeer({ name: 'big-peer', shape: 'C' })],
    local: {
      mem_used: 2 ** 61,          // 2 PiB
      mem_total: 2 ** 62,        // 4 PiB
      disk_used: 8 * 10 ** 15,   // 8 PB
      disk_total: 16 * 10 ** 15, // 16 PB
      net_rx_bytes: 15 * 10 ** 17, // 1.5 EB
      net_tx_bytes: 6 * 10 ** 17,  // 0.6 EB
      uptime_seconds: 10 ** 9,
      active_sessions: 9000000000,
      total_sessions: 18000000000,
      session_stats: makeLocalSessions(3).map((s, i) => ({ ...s, rss_bytes: (i + 1) * 1024 * GB })),
      comm_stats: makeCommStats().map((c, i) => ({ ...c, active_sessions: 1200000000 + i * 100000000, total_sessions: 3400000000 + i * 200000000, connections: 900000 + i, msg_recv: 5400000000 + i, msg_sent: 6100000000 + i })),
      gpu_mem_used_mb: 655360,   // 640 GB
      gpu_mem_total_mb: 1048576, // 1 TB
    },
  });
  const eightGpu = Array.from({ length: 8 }, (_, i) =>
    makeGpu({
      name: 'NVIDIA B200 Ultra Blackwell 80GB HBM3e Accelerator ' + String(i + 1).padStart(2, '0'),
      mem_total_mb: 8388608,    // 8 TB
      mem_used_mb: 6291456,     // 6 TB
      util_pct: 91, temp_c: 79, power_w: 1248,
    }));
  seed.responses[peerStatsKey('big-peer')].body = makePeerSnapshot({
    host: { name: 'big-peer', os: 'linux', arch: 'arm64', uptime_seconds: 10 ** 8 },
    cpu: { pct: 96.2, load1: 251.7, load5: 239.4, load15: 214.9 },
    mem: { used_bytes: 2 ** 53, total_bytes: 2 ** 54 },
    gpu: eightGpu,
    envelopes: makeLocalSessions(4).map((s, i) => makeEnvelope('session', 'session:9000' + i, s.name)),
  });
  seed.responses['/api/analytics'].body = {
    success_rate: 0.999,
    buckets: makeAnalyticsBuckets(14).map((b, i) => ({ ...b, session_count: 10 ** 9 + i * 7, completed: 10 ** 9 + i * 5, failed: 10 ** 6, killed: 10 ** 4 })),
  };
  return seed;
};

// ── public API ──────────────────────────────────────────────────────────────

export function buildSeed(state) {
  const builder = STATE_BUILDERS[state];
  if (!builder) {
    throw new Error('unknown mobile-fit seed state: ' + state + ' (valid: ' + STATE_NAMES.join(', ') + ')');
  }
  return builder();
}

// ── Playwright wiring ────────────────────────────────────────────────────────

const ROUTE_PATTERN = '**/api/**';

// request URL -> seed key. Query is stripped except the v=2 stats variant.
function responseKeyFor(url) {
  let parsed;
  try {
    parsed = new URL(url);
  } catch {
    return null;
  }
  let key;
  try {
    key = decodeURIComponent(parsed.pathname);
  } catch {
    key = parsed.pathname;
  }
  const v = parsed.searchParams.get('v');
  if (v === '2') key += '?v=2';
  if (!key.startsWith('/api/')) return null;
  return key;
}

export function buildRouteHandler(seed) {
  return async (route) => {
    const key = responseKeyFor(route.request().url);
    const target = key ? seed.responses[key] : undefined;
    if (!target) {
      await route.fallback();
      return;
    }
    if (target.delayMs > 0) {
      await new Promise((r) => setTimeout(r, target.delayMs));
    }
    await route.fulfill({
      status: target.status,
      contentType: 'application/json',
      body: JSON.stringify(target.body),
    });
  };
}

export async function installSeed(page, state) {
  // replace any previous seed (route() without a handler would append)
  clearSeed(page);
  const handler = buildRouteHandler(buildSeed(state));
  if (typeof page.route === 'function') {
    await page.route(ROUTE_PATTERN, handler);
  }
  return handler;
}

export function clearSeed(page) {
  if (page && typeof page.unroute === 'function') {
    const result = page.unroute(ROUTE_PATTERN);
    if (result && typeof result.catch === 'function') result.catch(() => {});
  }
}
