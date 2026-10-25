// Observer phone-width fit regression tests — long hostnames / peer names
// must neither overflow the 320–600px viewports nor clip their content:
//   * #perSystemGrid cards: flexible tracks (minmax(min(200px,100%),1fr))
//     so the grid fits 375px, min-width:0 so a card can shrink, and the
//     name clipped with an ellipsis while the FULL name stays in the DOM
//     (title attribute) — the previous inline-styled cards fixed 200px+
//     minimums and let long peer names spill past the viewport edge.
//   * #statsPanel: long hostnames and values wrap (overflow-wrap: anywhere)
//     inside the panel instead of widening the page; session/channel rows
//     keep ≥40px tap targets at phone widths without clipping their
//     badges (rows wrap instead of overflowing at 200% zoom).
//
// The DOM side is verified in a vm sandbox (real app.js renderer output),
// the layout side is verified against the style.css rules that implement
// the fix — the browser harness (scripts/test-stories/pwa/mobile-fit.mjs)
// re-checks the same contracts for real at widths 320/360/375/390/412/600/768.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, makeStubElement, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

// The Observer stats panel renders through the shared list loader: the loader
// owns #statsPanel and the stats land in its #statsBody child. This stub gives
// the panel that child, and reads of innerHTML see both, so assertions about
// what the operator sees keep working.
function makeStatsPanel() {
  const body = makeStubElement();
  const panel = makeStubElement();
  return new Proxy(panel, {
    get(target, prop) {
      if (prop === 'innerHTML') return target.innerHTML + body.innerHTML;
      if (prop === 'querySelector') return (sel) => (sel === '#statsBody' ? body : null);
      const v = Reflect.get(target, prop, target);
      return typeof v === 'function' ? v.bind(target) : v;
    },
    set(target, prop, value) { target[prop] = value; return true; },
  });
}

const APP_JS = path.join(__dirname, 'app.js');
const STYLE_CSS = path.join(__dirname, 'style.css');

const LONG_HOST = 'host-abcdefghij-klmnopqrst-uvwxyz-long-0123456789';
const PEER_LONG = 'peer-very-long-hostname-abcdefghijklmnopqrstuvwxyz-01234567';

const STATS_BODY = {
  timestamp: Math.floor(Date.now() / 1000),
  hostname: LONG_HOST,
  cpu_cores: 8,
  cpu_load_avg_1: 2.2, cpu_load_avg_5: 2.0, cpu_load_avg_15: 1.9,
  mem_used: 4294967296, mem_total: 17179869184,
  uptime_seconds: 7200,
  total_sessions: 1,
  session_stats: [
    { session_id: 'sid-1', name: 'alpha', backend: 'anthropic', state: 'active',
      rss_bytes: 20971520, uptime: '10m', pane_pid: 4242, cpu_percent: 5.5 },
  ],
};

const PEERS_BODY = {
  peers: [
    { name: PEER_LONG, shape: 'flat', last_push_at: new Date().toISOString(), is_self: false },
  ],
};

const PEER_STATS = {
  cpu: { pct: 55, load1: 3.1, load5: 2.8, load15: 2.5 },
  mem: { used_bytes: 12884901888, total_bytes: 34359738368 },
  gpu: [{ name: '0', util_pct: 70, temp_c: 65, power_w: 60.5,
    mem_used_bytes: 8589934592, mem_total_bytes: 25769803776 }],
};

function fakeResponse(status, body) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(body),
    text: () => Promise.resolve(typeof body === 'string' ? body : JSON.stringify(body)),
  };
}

function makeEnv() {
  const grid = makeStubElement();
  const panel = makeStatsPanel();
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox.document.getElementById = (id) => {
    if (id === 'perSystemGrid') return grid;
    if (id === 'statsPanel') return panel;
    return makeStubElement();
  };
  sandbox.fetch = (url) => {
    const u = String(url);
    if (u.startsWith('/api/stats')) return Promise.resolve(fakeResponse(200, STATS_BODY));
    if (u === '/api/observer/peers') return Promise.resolve(fakeResponse(200, PEERS_BODY));
    if (u.startsWith('/api/observer/peers/')) return Promise.resolve(fakeResponse(200, PEER_STATS));
    return Promise.resolve(fakeResponse(200, {}));
  };
  return { grid, panel, sandbox };
}

// Extract a full `selector { ... }` block (flat, no nesting) from CSS text.
function cssBlock(css, selector) {
  const re = new RegExp(selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\s*\\{([^{}]*?)\\}');
  const m = css.match(re);
  return m ? m[1] : null;
}

// Find the @media (max-width: <widthPx>px) block that contains
// `mustContain` (style.css has several media blocks at these widths; the
// mustContain anchor pins us to the MOBILE-FIT one rather than an
// unrelated block that happens to share the breakpoint).
function mediaBlock(css, widthPx, mustContain) {
  // The touch block also covers short landscape screens, so the query may
  // carry a trailing ", (max-height: …)" alternative.
  const marker = new RegExp(`@media \\(max-width: ${widthPx}px\\)(?:, \\(max-height: \\d+px\\))? \\{`, 'g');
  let searchFrom = 0;
  for (;;) {
    marker.lastIndex = searchFrom;
    const hit = marker.exec(css);
    if (!hit) return null;
    const start = hit.index;
    let depth = 0;
    for (let i = css.indexOf('{', start); i < css.length; i += 1) {
      if (css[i] === '{') depth += 1;
      else if (css[i] === '}') {
        depth -= 1;
        if (depth === 0) {
          const block = css.slice(start, i + 1);
          if (!mustContain || block.includes(mustContain)) return block;
          break;
        }
      }
    }
    searchFrom = start + 1;
  }
}

test('grid card at 375px: shrinkable card, name clipped with ellipsis (full name in title), no inline pixel width', async () => {
  const env = makeEnv();
  vm.runInContext('loadSystemStatsGrid()', env.sandbox);
  await flushAsync();
  const html = env.grid.innerHTML;
  assert.match(html, /obs-sys-grid/, 'grid container rendered');
  // Cards lay out via the .obs-sys-* CSS classes — the track shrinkability
  // (minmax + the <=600px single-column collapse) lives in style.css and is
  // asserted by the "style.css pins the phone-width grid layout" test below.
  assert.equal((html.match(/class="obs-sys-card"/g) || []).length, 2,
    'one card per system — local + peer');
  // Every long name is clipped INSIDE the card with an ellipsis, while the
  // full name remains reachable (title=): no name spilling past the edge.
  assert.match(html, new RegExp('class="obs-sys-name"[^>]*title="' + PEER_LONG + '"'),
    'long peer name must stay in the DOM (title attribute) for the ellipsis clip');
  // The name span itself must have NO inline fixed width — clipping comes
  // from the .obs-sys-name CSS rule (overflow:hidden + text-overflow:ellipsis).
  const nameSpan = html.split('class="obs-sys-name"')[1].split('>')[0];
  assert.doesNotMatch(' class="' + nameSpan, /width:/, 'name span must size via CSS, not inline width');
  assert.equal((html.match(/class="obs-sys-name"/g) || []).length, 2,
    'one clipped-name span per card (local + peer)');
  // No inline pixel width anywhere in the card markup (the old inline-
  // styled cards carried fixed 200px+ floors that overflowed 375px).
  assert.doesNotMatch(html, /width:\s*\d+px/, 'cards must not carry inline pixel widths');
});

test('style.css pins the phone-width grid layout: ellipsis names, min-width:0 cards, 1-column at <=600px', () => {
  const css = fs.readFileSync(STYLE_CSS, 'utf8');

  const name = cssBlock(css, '.obs-sys-name');
  assert.ok(name, '.obs-sys-name rule must exist');
  assert.match(name, /overflow:\s*hidden/, 'name must clip (overflow:hidden)');
  assert.match(name, /text-overflow:\s*ellipsis/, 'clipped name must show an ellipsis');
  assert.match(name, /white-space:\s*nowrap/, 'name must stay on one line before clipping');

  const card = cssBlock(css, '.obs-sys-card');
  assert.ok(card, '.obs-sys-card rule must exist');
  assert.match(card, /min-width:\s*0/, 'card must be shrinkable (min-width:0)');

  // The per-system grid track must be shrinkable (min(200px,100%)) so a card
  // can fit an 180px effective viewport (360 @ z200) instead of hard-forcing
  // 200px and overflowing past the right edge. The shrinkable track lives on
  // the base .obs-sys-grid rule (distinct from the ≤600 single-column one).
  assert.match(css, /\.obs-sys-grid \{[^}]*minmax\(min\(200px,\s*100%\),\s*1fr\)[^}]*\}/,
    'base .obs-sys-grid track must be able to shrink below 200px to fit narrow viewports');

  const media = mediaBlock(css, 600, '.obs-sys-grid');
  assert.ok(media, '@media (max-width: 600px) block with the grid rule must exist');
  assert.match(media, /\.obs-sys-grid \{ grid-template-columns: 1fr; \}/,
    'phone widths collapse the grid to one full-width column');
});

test('style.css pins the #statsPanel no-clip contract: wrapping text, ≥40px phone tap targets', () => {
  const css = fs.readFileSync(STYLE_CSS, 'utf8');

  const panel = cssBlock(css, '#statsPanel');
  assert.ok(panel, '#statsPanel rule must exist');
  assert.match(panel, /overflow-wrap:\s*anywhere/,
    'long hostnames/values must wrap inside the panel, not widen the page');
  assert.match(panel, /min-width:\s*0/,
    'panel must be allowed to shrink with the viewport');

  // Desktop baseline: the .stats-taprow base rule exists and stays a compact,
  // cursor-pointer row — the ≥40px tap floors live in the ≤768px touch block
  // only, so the true desktop (1280/1920) heights stay in the TS-818 baseline.
  const tap = cssBlock(css, '.stats-taprow');
  assert.ok(tap, '.stats-taprow rule must exist');
  assert.match(tap, /cursor:\s*pointer/, 'tap rows stay pointer rows on desktop');
  assert.doesNotMatch(tap, /min-height:\s*40px/, 'the 40px tap floor must not leak into the desktop baseline');

  // The mobile-fit harness flags tap targets at every quick-profile touch
  // viewport — 320/360/390 and the 768 tablet portrait, plus the 740
  // (360-landscape) width — so the ≥40px floors must apply at ≤768px, not
  // just ≤600px. 740 and 768 both satisfy max-width:768px; 1280/1920 do not.
  const media = mediaBlock(css, 768, '.stats-taprow');
  assert.ok(media, '@media (max-width: 768px) block with the tap-row rules must exist');
  assert.match(media, /\.stats-taprow \{ min-height: 40px; flex-wrap: wrap; \}/,
    'touch rows need a 40px tap floor and must WRAP their badges rather than clip them');
  assert.match(media, /#statsPanel a\[href\] \{ display: inline-block; min-height: 40px;/,
    'the "sessions in store" link needs a 40px tap floor on touch viewports');
  assert.match(media, /#statsPanel button \{ min-height: 40px; min-width: 40px; \}/,
    'panel buttons (e.g. History) need 40×40px tap floors on touch viewports');
});

test('rendered stats panel: session rows use the stats-taprow hook and no inline pixel width anywhere in the panel', async () => {
  const env = makeEnv();
  vm.runInContext('loadStatsPanel()', env.sandbox);
  await flushAsync();
  const html = env.panel.innerHTML;
  assert.match(html, /class="stats-taprow"/, 'session rows must carry the stats-taprow hook so the CSS tap floors apply');
  assert.match(html, /#sid-1/, 'session row renders');
  // The overflow findings came from inline fixed pixel widths on panel
  // containers — none may appear on the panel root or on its row wrappers.
  assert.doesNotMatch(String(env.panel.style.width), /^\d+px$/, '#statsPanel must not carry an inline pixel width');
  // Row wrappers (the flex tap rows) must size their text columns with flex,
  // not fixed widths, so long names wrap/shrink on phones.
  // Row names size with flex (not inline fixed widths) so long names
  // shrink/wrap on phones; and no inline pixel width >=100px anywhere in
  // the rendered panel (the overflow findings came from inline fixed
  // widths). The chevron marker is the only inline px width and it is 10px.
  assert.match(html, /flex:1/, 'the name column in tap rows is flex-sized, not fixed-width');
  assert.doesNotMatch(html, /width:\s*(\d{3,})px/, 'no inline fixed width >=100px in the rendered panel');
});
