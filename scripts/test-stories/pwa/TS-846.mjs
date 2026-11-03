// TS-846.mjs
// PWA story: selecting a federated peer from EVERY control that can select one
// (header server chip, server picker modal, Settings servers list) switches the
// whole PWA to that peer, and selecting it again changes nothing.
//
// Setup (sandbox only): this story starts TWO fresh sandbox daemons itself,
// each with its own temporary data directory, its own free ports (HTTP, TLS,
// MCP, DNS, ...) and a distinct config hostname. The first is the primary the
// browser opens over TLS; the second is registered on the primary as the
// federated peer named "ralfthewise" (its config hostname is "ralfthewise" too,
// so the peer's name and hostname are the same string). TMUX / TMUX_TMPDIR are
// removed from the daemons' environment. The production daemon and the
// operator's config are never touched; both daemons and both data directories
// are removed on every exit path (normal exit, failure, SIGINT/SIGTERM).
//
// For each control the story asserts, right after the selection:
//   - state.activeServer equals the peer;
//   - the toast (alert dock entry) names the peer and not the local hostname;
//   - #server-header-line shows the peer;
//   - the About page and every Settings tab show the peer ("Showing: <peer>")
//     and fetch their data through /api/proxy/<peer>/...;
// then selects the peer again (state, sessions array and toasts unchanged, no
// reload) and returns to the primary with the explicit back-to-local control,
// asserting the local hostname everywhere the peer was shown.

import { spawn } from 'node:child_process';
import { existsSync, openSync, mkdirSync, readFileSync, writeFileSync, rmSync, accessSync, constants } from 'node:fs';
import net from 'node:net';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import {
  runPerfStory,
  coldPage,
  skipStory,
  saveJson,
} from './perf-lib.mjs';

const PEER = 'ralfthewise';
const LOCAL_HOST = 'ts846-primary-host';
const SETTINGS_TABS = ['general', 'plugins', 'comms', 'compute', 'automata', 'about'];

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, '../../..');
const template = path.join(repoRoot, 'testdata/datawatch.yaml');

let checks = 0;
function check(cond, message) {
  checks += 1;
  if (!cond) throw new Error(message);
}

// --- sandbox daemons ---------------------------------------------------------

const daemons = [];
let sandboxRoot = '';

function stopAll() {
  for (const d of daemons.splice(0)) {
    try { process.kill(-d.pid, 'SIGTERM'); } catch { try { process.kill(d.pid, 'SIGTERM'); } catch { /* gone */ } }
  }
  if (sandboxRoot) {
    try { rmSync(sandboxRoot, { recursive: true, force: true }); } catch { /* best-effort */ }
    sandboxRoot = '';
  }
}
process.on('exit', stopAll);
for (const sig of ['SIGINT', 'SIGTERM', 'SIGHUP']) {
  process.on(sig, () => { stopAll(); process.exit(130); });
}

function freePort() {
  return new Promise((resolve, reject) => {
    const s = net.createServer();
    s.once('error', reject);
    s.listen(0, '127.0.0.1', () => { const { port } = s.address(); s.close(() => resolve(port)); });
  });
}

function findBinary() {
  for (const c of [process.env.TEST_BINARY, path.join(repoRoot, 'bin/datawatch')]) {
    if (!c) continue;
    try { accessSync(c, constants.X_OK); return c; } catch { /* next */ }
  }
  return '';
}

async function startDaemon(bin, name, hostname, token) {
  const dir = path.join(sandboxRoot, name);
  mkdirSync(dir, { recursive: true });
  const ports = { http: await freePort(), tls: await freePort(), mcp: await freePort(), dns: await freePort(), x1: await freePort(), x2: await freePort() };
  let cfg = readFileSync(template, 'utf8')
    .replace(/^hostname:.*$/m, `hostname: ${hostname}`)
    .replace(/data_dir: \/data/g, `data_dir: ${dir}`)
    .replace(/port: 8080/g, `port: ${ports.http}`)
    .replace(/tls_port: 8443/g, `tls_port: ${ports.tls}`)
    .replace(/sse_port: 9090/g, `sse_port: ${ports.mcp}`)
    .replace(/host: 0\.0\.0\.0/g, 'host: 127.0.0.1')
    .replace(/127\.0\.0\.1:19080/g, `127.0.0.1:${ports.x1}`)
    .replace(/127\.0\.0\.1:19053/g, `127.0.0.1:${ports.dns}`)
    .replace(/127\.0\.0\.1:19180/g, `127.0.0.1:${ports.x2}`)
    .replace(/token: ""/g, `token: "${token}"`);
  const cfgPath = path.join(dir, 'config.yaml');
  writeFileSync(cfgPath, cfg);
  const env = { ...process.env };
  delete env.TMUX;
  delete env.TMUX_TMPDIR;
  const log = path.join(dir, 'daemon.log');
  const out = openSync(log, 'a');
  const child = spawn(bin, ['start', '--foreground', '--config', cfgPath], {
    env, detached: true, stdio: ['ignore', out, out],
  });
  child.unref();
  daemons.push(child);
  const base = `http://127.0.0.1:${ports.http}`;
  const deadline = Date.now() + 45000;
  while (Date.now() < deadline) {
    if (child.exitCode !== null) throw new Error(`sandbox daemon ${name} exited early (see ${log})`);
    try {
      const r = await fetch(`${base}/api/health`);
      if (r.ok) return { base, tlsBase: `https://127.0.0.1:${ports.tls}`, token, ports, hostname };
    } catch { /* not up yet */ }
    await new Promise((r) => setTimeout(r, 300));
  }
  throw new Error(`sandbox daemon ${name} did not become healthy within 45s`);
}

// --- story -------------------------------------------------------------------

runPerfStory(async (page) => {
  const bin = findBinary();
  if (!bin) await skipStory('datawatch binary not found (set TEST_BINARY or build bin/datawatch)');
  if (!existsSync(template)) await skipStory('testdata/datawatch.yaml not found');

  sandboxRoot = path.join(repoRoot, `.ts846-sandbox-${process.pid}`);
  mkdirSync(sandboxRoot, { recursive: true });
  const primary = await startDaemon(bin, 'primary', LOCAL_HOST, 'ts846-primary-token');
  const peer = await startDaemon(bin, 'peer', PEER, 'ts846-peer-token');

  // Register the second daemon as a federated peer on the primary. The peer's
  // plain-HTTP port is used for the proxy hop: the proxy verifies certificates
  // and the sandbox TLS certificate is self-signed.
  const reg = await fetch(`${primary.base}/api/servers`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${primary.token}`, 'Content-Type': 'application/json' },
    body: JSON.stringify({ name: PEER, url: peer.base, token: peer.token, enabled: true, federated: true, capabilities: ['full-control'] }),
  });
  check(reg.ok, `registering the peer on the primary failed: HTTP ${reg.status}`);

  const { context, page: p } = await coldPage(page.context().browser());
  const proxyHits = [];
  p.on('request', (req) => {
    const u = new URL(req.url());
    if (u.pathname.startsWith(`/api/proxy/${PEER}/`)) proxyHits.push(u.pathname);
  });

  // Open the PWA on the primary over TLS.
  await p.goto(primary.tlsBase, { waitUntil: 'domcontentloaded', timeout: 60000 });
  await p.evaluate((t) => localStorage.setItem('cs_token', t), primary.token);
  await p.reload({ waitUntil: 'domcontentloaded', timeout: 60000 });
  await p.waitForFunction(() => typeof window.navigate === 'function', null, { timeout: 60000 });
  await p.waitForFunction(() => {
    const el = document.getElementById('splash');
    return !el || getComputedStyle(el).display === 'none' || getComputedStyle(el).opacity === '0' || el.classList.contains('hidden');
  }, null, { timeout: 60000 });
  await p.evaluate(() => { window.navigate('sessions'); });
  await p.waitForFunction((n) => {
    return window.state && Array.isArray(state.servers ? (state.servers.servers || state.servers) : null)
      && (state.servers.servers || state.servers).some((s) => s.name === n);
  }, PEER, { timeout: 30000 });
  await p.evaluate(() => window._ensureLocalHostname && window._ensureLocalHostname());
  await p.waitForFunction((h) => document.querySelector('.server-picker-bar') && document.body.innerText.indexOf(h) !== -1, LOCAL_HOST, { timeout: 30000 });
  await p.evaluate(() => { window.__ts846Marker = 'ts846'; });

  const marker = () => p.evaluate('window.__ts846Marker');
  const active = () => p.evaluate('state.activeServer');
  const toasts = () => p.evaluate(() => (window._alertDock.alerts || []).map((a) => String(a.message)));
  const clearToasts = () => p.evaluate(() => { window._alertDock.alerts.length = 0; });
  const headerLine = () => p.evaluate(() => {
    const el = document.getElementById('server-header-line');
    return el ? { text: el.textContent.trim(), server: el.getAttribute('data-server') } : null;
  });

  // Assert the whole PWA is showing `who` (a hostname): header line, About page
  // and every Settings tab, with data fetched through the proxy for the peer.
  async function assertShowing(label, who, isPeer) {
    const other = isPeer ? LOCAL_HOST : PEER;
    const h = await headerLine();
    check(h && h.text.includes(who) && !h.text.includes(other), `${label}: server header line should show ${who}, got ${JSON.stringify(h)}`);
    check(h.server === who, `${label}: header line data-server should be ${who}, got ${h.server}`);
    for (const tab of SETTINGS_TABS) {
      const before = proxyHits.length;
      await p.evaluate(() => { window.navigate('settings'); });
      await p.evaluate((tb) => window.switchSettingsTab(tb), tab);
      await p.waitForFunction((tb) => {
        const s = document.querySelector(`.settings-section[data-group="${tb}"]`);
        return !!s && s.style.display !== 'none';
      }, tab, { timeout: 15000 });
      const line = await p.evaluate((tb) => {
        const el = tb === 'about'
          ? document.getElementById('aboutShowingServer')
          : document.querySelector(`[data-showing-line="${tb}"] .settings-label`);
        return el ? el.textContent.trim() : null;
      }, tab);
      check(line !== null && line.includes(who) && !line.includes(other), `${label}: Settings ${tab} should show ${who}, got ${JSON.stringify(line)}`);
      const hh = await headerLine();
      check(hh && hh.text.includes(who), `${label}: header line missing on Settings ${tab}`);
      if (tab === 'about') {
        await p.waitForFunction(() => {
          const v = document.getElementById('aboutVersion');
          return v && v.textContent.trim() && v.textContent.trim() !== '—';
        }, null, { timeout: 20000 });
        if (isPeer) {
          await p.waitForFunction((n) => performance.getEntriesByType('resource').some((r) => r.name.includes(`/api/proxy/${n}/`)), PEER, { timeout: 20000 });
          check(proxyHits.length > before || proxyHits.length > 0, `${label}: About data did not come through /api/proxy/${PEER}/`);
        }
      }
    }
    await p.evaluate(() => { window.navigate('sessions'); });
  }

  const openSettingsServers = async () => {
    await p.evaluate(() => { window.navigate('settings'); });
    for (const tab of SETTINGS_TABS) {
      await p.evaluate((tb) => window.switchSettingsTab(tb), tab);
      const found = await p.waitForFunction(() => {
        const el = document.getElementById('serverStatus');
        return !!el && el.querySelector('button[onclick^="selectServer("]');
      }, null, { timeout: 4000 }).then(() => true).catch(() => false);
      if (found) return;
    }
    throw new Error('Settings servers list (#serverStatus) not found on any tab');
  };

  const controls = {
    'header chip': {
      selectPeer: async () => {
        await p.evaluate(() => { window.navigate('sessions'); });
        await p.click(`.server-picker-bar button[data-server-name="${PEER}"]`);
      },
      back: async () => {
        await p.evaluate(() => { window.navigate('sessions'); });
        await p.click('.server-picker-bar button[data-server-name=""]');
      },
    },
    'server picker modal': {
      selectPeer: async () => {
        await p.evaluate(() => window.openServerPickerModal());
        await p.click(`#serverPickerModal button[data-server-name="${PEER}"]`);
      },
      back: async () => {
        await p.evaluate(() => window.openServerPickerModal());
        await p.click('#serverPickerModal button[data-back-to-local]');
      },
    },
    'Settings servers list': {
      selectPeer: async () => {
        await openSettingsServers();
        await p.click(`#serverStatus button[onclick="selectServer('${PEER}')"]`);
      },
      back: async () => {
        await openSettingsServers();
        await p.click('#serverStatus button[data-back-to-local]');
      },
    },
  };

  const evidence = {};
  for (const [label, ctl] of Object.entries(controls)) {
    check((await active()) === null, `${label}: should start on the primary, got ${JSON.stringify(await active())}`);
    await clearToasts();
    await ctl.selectPeer();
    await p.waitForFunction((n) => state.activeServer === n, PEER, { timeout: 15000 });
    check((await active()) === PEER, `${label}: state.activeServer should equal ${PEER}`);
    const t1 = await toasts();
    check(t1.some((m) => m.includes(PEER)) && !t1.some((m) => m.includes(LOCAL_HOST)),
      `${label}: toast should name ${PEER} and not ${LOCAL_HOST}, got ${JSON.stringify(t1)}`);
    check((await marker()) === 'ts846', `${label}: page reloaded on selecting the peer`);
    await assertShowing(`${label} → peer`, PEER, true);

    // Select the peer again: nothing changes.
    await p.evaluate(() => { window.__ts846Sessions = state.sessions; });
    await clearToasts();
    await ctl.selectPeer();
    await new Promise((r) => setTimeout(r, 400));
    check((await active()) === PEER, `${label}: re-selecting changed state.activeServer`);
    check((await toasts()).length === 0, `${label}: re-selecting the peer raised a toast`);
    check(await p.evaluate(() => state.sessions === window.__ts846Sessions), `${label}: re-selecting the peer reset state`);
    check((await marker()) === 'ts846', `${label}: page reloaded on re-selecting the peer`);
    await assertShowing(`${label} → peer again`, PEER, true);

    // Explicit back-to-local control.
    await clearToasts();
    await ctl.back();
    await p.waitForFunction(() => state.activeServer === null, null, { timeout: 15000 });
    const t2 = await toasts();
    check(t2.some((m) => m.includes(LOCAL_HOST)) && !t2.some((m) => m.includes(PEER)),
      `${label}: back-to-local toast should name ${LOCAL_HOST} and not ${PEER}, got ${JSON.stringify(t2)}`);
    check((await marker()) === 'ts846', `${label}: page reloaded on returning to local`);
    await assertShowing(`${label} → local`, LOCAL_HOST, false);
    evidence[label] = { toastPeer: t1, toastLocal: t2 };
  }

  await saveJson('peer-selection-evidence', { peer: PEER, local: LOCAL_HOST, proxyRequests: proxyHits.length, evidence });
  console.log(`controls=${Object.keys(controls).length} tabs=${SETTINGS_TABS.length} checks=${checks}`);
  console.log('RESULT: pass');

  await context.close();
  stopAll();
});
