'use strict';
// MOBILE-FIT tests for the shared compact number formatters (fmtBytesCompact /
// fmtCountCompact) that loadSystemStatsGrid, loadPeerResourceOverview and
// renderStatsData use in the Observer "#perSystemGrid" and "#statsPanel"
// blocks — and for the fact that loadSystemStatsGrid now emits the shared
// .obs-sys-* / .obs-bar* CSS classes (style.css) instead of inline styles,
// so narrow screens can reflow them without the grid spilling off-screen.
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, makeStubElement, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  return sandbox;
}

// ── 1. The helpers exist, are functions, and are exposed on window ───────────
test('fmtBytesCompact / fmtCountCompact exist and are exposed on window', () => {
  const sb = loadAppJS();
  assert.equal(typeof sb.fmtBytesCompact, 'function', 'fmtBytesCompact is a function');
  assert.equal(typeof sb.fmtCountCompact, 'function', 'fmtCountCompact is a function');
  assert.equal(vm.runInContext('typeof window.fmtBytesCompact', sb), 'function');
  assert.equal(vm.runInContext('typeof window.fmtCountCompact', sb), 'function');
});

// ── 2. fmtBytesCompact: compact base-1024 output across the full range ──────
test('fmtBytesCompact matches the shared helper contract (B to EB)', () => {
  const sb = loadAppJS();
  const c = sb.fmtBytesCompact;
  // Zero / missing / non-finite render as the '—' missing-value sentinel
  assert.equal(c(0), '—');
  // Byte range
  assert.equal(c(512), '512 B');
  assert.equal(c(1023), '1023 B');
  // KB (base-1024, one decimal)
  assert.equal(c(1024), '1.0 KB');
  assert.equal(c(1536), '1.5 KB');
  assert.equal(c(10 * 1024), '10.0 KB');
  // MB
  assert.equal(c(5 * 1024 * 1024), '5.0 MB');
  assert.equal(c(512 * 1024 * 1024), '512.0 MB');
  // GB
  assert.equal(c(1073741824), '1.0 GB');
  assert.equal(c(1.5 * 1024 ** 3), '1.5 GB');
  assert.equal(c(15 * 1024 ** 3), '15.0 GB');
  // TB (1 TiB and up)
  assert.equal(c(1024 ** 4), '1.0 TB');
  assert.equal(c(2 * 1024 ** 4), '2.0 TB');
  assert.equal(c(5 * 1024 ** 4), '5.0 TB');
  // PB (1 PiB and up)
  // PB (1 PiB and up) — 1024 TiB = 1 PiB, so 1023.0 TB is the TB ceiling
  assert.equal(c(1023 * 1024 ** 4), '1023.0 TB');
  assert.equal(c(1024 ** 5), '1.0 PB');
  assert.equal(c(1.5 * 1024 ** 5), '1.5 PB');
  assert.equal(c(5 * 1024 ** 5), '5.0 PB');
  // EB (1 EiB and up) — big-end scale stays a short "x.x EB" (numbers-large seed class)
  assert.equal(c(1023 * 1024 ** 5), '1023.0 PB');
  assert.equal(c(1024 ** 6), '1.0 EB');
  assert.equal(c(2 ** 61), '2.0 EB');
  assert.equal(c(8 * 10 ** 15), '7.1 PB');
  assert.equal(c(15 * 10 ** 17), '1.3 EB');
  // Defensive: non-finite / negative input must not leak NaN or '-' — same '—' sentinel
  assert.equal(c(Infinity), c(0));
  assert.equal(c(-9), '—');
});

// ── 3. fmtCountCompact: exact counts under 1K, compact above ─────────────────
test('fmtCountCompact: exact under 1000, K/M/B/T at and above', () => {
  const sb = loadAppJS();
  const c = sb.fmtCountCompact;
  assert.equal(c(0), '—');
  assert.equal(c(1), '1');
  assert.equal(c(42), '42');
  assert.equal(c(999), '999');
  assert.equal(c(1000), '1.0K');
  assert.equal(c(1536), '1.5K');
  assert.equal(c(12345), '12.3K');
  assert.equal(c(2000e3), '2.0M');
  assert.equal(c(3e9), '3.0B');
  // T (1 trillion and up)
  assert.equal(c(999 * 10 ** 9), '999.0B');
  assert.equal(c(10 ** 12), '1.0T');
  assert.equal(c(3 * 10 ** 12), '3.0T');
  assert.equal(c(-7), '—');
  assert.equal(c(9.5), '10'); // rounds to an exact small count
});

// ── 4. loadSystemStatsGrid emits the shared classes, not inline styles ───────
test('loadSystemStatsGrid renders .obs-sys-*/.obs-bar* classes and compact values', async () => {
  const grid = makeStubElement();
  const sb = buildSandbox({ byId: { perSystemGrid: grid } });
  vm.createContext(sb);

  const GiB = 1024 ** 3;
  // Stub the HTTP layer with the REAL response shapes:
  //   /api/stats (flat local stats)
  //   /api/observer/peers -> {peers:[...]} incl. synthesized self entry
  //   /api/observer/peers/<name>/stats -> {cpu, mem, gpu[]}
  // Byte values are chosen as clean base-1024 multiples (the shared
  // helper is base-1024, like the module-level fmtBytes it replaces).
  sb.fetch = (url) => {
    const u = String(url);
    if (/\/api\/observer\/peers\/[^/]+\/stats/.test(u)) {
      return Promise.resolve({ status: 200, ok: true,
        json: () => Promise.resolve({
          cpu: { pct: 40, load1: 2.1, load5: 2.2, load15: 2.3 },
          mem: { used_bytes: 1 * GiB, total_bytes: 16 * GiB },
          gpu: [{ name: 'g0', util_pct: 20, temp_c: 55, power_w: 4.4,
                  mem_used_bytes: 2 * GiB, mem_total_bytes: 8 * GiB }],
        }),
      });
    }
    if (/\/api\/observer\/peers/.test(u)) {
      return Promise.resolve({ status: 200, ok: true,
        json: () => Promise.resolve({ peers: [
          { name: 'host-a', is_self: true, last_push_at: new Date().toISOString() },
          { name: 'host-b', is_self: false, last_push_at: new Date().toISOString() },
        ] }),
      });
    }
    if (/\/api\/stats/.test(u)) {
      return Promise.resolve({ status: 200, ok: true,
        json: () => Promise.resolve({
          timestamp: Date.now(),
          hostname: 'host-a',
          cpu_cores: 8, cpu_load_avg_1: 4.0, cpu_load_avg_5: 4.2, cpu_load_avg_15: 4.5,
          mem_used: 1.5 * GiB, mem_total: 32 * GiB,
          gpu_name: 'nvidia', gpu_util_pct: 30, gpu_temp: 61,
          gpu_mem_used_mb: (2 * GiB) / 1048576, gpu_mem_total_mb: (8 * GiB) / 1048576,
        }),
      });
    }
    return Promise.resolve({ status: 404, ok: false, text: () => Promise.resolve('nf') });
  };

  loadScript(APP_JS, sb);
  vm.runInContext('loadSystemStatsGrid()', sb);
  await flushAsync();

  const html = grid.innerHTML;
  assert.ok(html, 'innerHTML was written to #perSystemGrid');

  // Two cards: local host-a + peer host-b (synthesized self entry filtered out).
  assert.match(html, /class="obs-sys-name" title="System name">host-a</);
  assert.match(html, /class="obs-sys-name" title="System name">host-b</);

  // Shared CSS classes (deliverable 2: card layout comes from style.css).
  assert.match(html, /class="obs-sys-grid"/);
  assert.equal((html.match(/class="obs-sys-card"/g) || []).length, 2);
  assert.match(html, /class="obs-bar"/);
  assert.match(html, /class="obs-bar-val" title="/);
  assert.match(html, /class="obs-kv"/); // gpu power (peer)

  // Compact shared-formatter output (deliverable 1).
  assert.match(html, /1\.5 GB/);   // local mem used (1.5 GiB)
  assert.match(html, /32\.0 GB/);  // local mem total (32 GiB)
  assert.match(html, /2\.0 GB/);   // gpu mem used (2 GiB, local + peer)
  assert.match(html, /8\.0 GB/);   // gpu mem total (8 GiB, local + peer)
  assert.match(html, /16\.0 GB/);  // peer mem total (16 GiB)
  assert.match(html, /4\.4 W/);    // peer gpu power

  // The old inline declarations must be gone from the rendered markup.
  assert.doesNotMatch(html, /grid-template-columns:repeat\(auto-fit,minmax\(200px/);
  assert.doesNotMatch(html, /style="background:var\(--surface1/);
  assert.doesNotMatch(html, /style="display:flex;justify-content:space-between;font-size:10px"/);
});
