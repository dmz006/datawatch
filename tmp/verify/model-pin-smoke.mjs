// End-to-end smoke for node model pinning: one datawatch daemon against one
// throwaway fake Ollama. Everything lives under tmp/verify in the repo tree.
//
// Run from the repo root after `go build -o datawatch ./cmd/datawatch/`:
//   node tmp/verify/model-pin-smoke.mjs
//
// It starts and stops only the fake server and the daemon it spawned itself.
// A marker line is written to model-pin-smoke-evidence.txt only when the check
// for that step passed; a failed step is reported and no marker is written.

import { spawn } from "node:child_process";
import { mkdirSync, rmSync, writeFileSync, readFileSync, existsSync, openSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, "../..");
const runDir = path.join(here, "run");
const dataDir = path.join(runDir, "data");
const homeDir = path.join(runDir, "home");
const cfgPath = path.join(runDir, "config.yaml");
const logPath = path.join(runDir, "daemon.log");
const evidencePath = path.join(here, "model-pin-smoke-evidence.txt");

const FAKE_PORT = 18434;
const DAEMON_PORT = 18733;
const FAKE = `http://127.0.0.1:${FAKE_PORT}`;
const BASE = `http://127.0.0.1:${DAEMON_PORT}`;
// The legacy "ollama" LLM is auto-linked to the compute node named
// datawatch-ollama (cmd/datawatch/main.go autoLinkLegacyComputeNode), so the
// pinned test node takes that name.
const NODE = "datawatch-ollama";
const BASE_MODEL = "base-27b";
const PIN = "pinned-27b";
const SWAP = "other-70b";
const ZERO_TIME = "0001-01-01T00:00:00Z";
const KEEPER_WAIT_MS = 12000; // pin_check_seconds is 5; this covers at least two ticks.

const lines = [];
const passed = new Set();
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const stamp = () => new Date().toISOString().slice(11, 19);

function evidence(text) {
  const line = `[${stamp()}] ${text}`;
  lines.push(line);
  console.log(line);
}

function check(marker, ok, detail) {
  evidence(`${ok ? "PASS" : "FAIL"} ${marker}: ${detail}`);
  if (!ok) throw new Error(`${marker} check failed: ${detail}`);
  passed.add(marker);
}

async function call(method, url, body) {
  const res = await fetch(url, {
    method,
    headers: body ? { "Content-Type": "application/json" } : {},
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  let json = null;
  try {
    json = JSON.parse(text);
  } catch {
    json = null;
  }
  return { status: res.status, text, json };
}

// Fake Ollama control, via its /_ctl endpoints.
const fakeLoaded = (models) => call("POST", `${FAKE}/_ctl/loaded`, { models });
const fakeClear = () => call("POST", `${FAKE}/_ctl/requests/clear`, {});
async function fakeRequests() {
  return (await call("GET", `${FAKE}/_ctl/requests`)).json.requests;
}
const generateFor = (reqs, model) => reqs.filter((r) => r.model === model);

function startFake() {
  const child = spawn(process.execPath, [path.join(here, "fake-ollama.mjs")], {
    env: { ...process.env, PORT: String(FAKE_PORT) },
    stdio: ["ignore", "pipe", "pipe"],
  });
  child.stderr.on("data", (d) => process.stderr.write(`[fake] ${d}`));
  return child;
}

function writeConfig(pinCheck) {
  mkdirSync(dataDir, { recursive: true });
  const yaml = [
    `data_dir: ${dataDir}`,
    "server:",
    "  enabled: true",
    "  host: 127.0.0.1",
    `  port: ${DAEMON_PORT}`,
    "  tls_enabled: false",
    "ollama:",
    "  enabled: true",
    `  model: ${BASE_MODEL}`,
    `  host: ${FAKE}`,
    "pi:",
    "  enabled: false",
    `  model: ollama/${PIN}`,
    `pin_check_seconds: ${pinCheck}`,
    "compute_nodes:",
    `  - name: ${NODE}`,
    "    kind: ollama",
    `    address: ${FAKE}`,
    `    node_pinned_model: ${PIN}`,
    "    allow_model_swap: false",
    "",
  ].join("\n");
  writeFileSync(cfgPath, yaml);
}

// The daemon runs with TMUX and TMUX_TMPDIR removed and HOME pointed into
// tmp/verify, so nothing it writes lands in the real user state.
function startDaemon() {
  const env = { ...process.env, HOME: homeDir };
  delete env.TMUX;
  delete env.TMUX_TMPDIR;
  for (const k of Object.keys(env)) if (k.startsWith("XDG_")) delete env[k];
  const out = openSync(logPath, "a");
  return spawn(path.join(repo, "datawatch"), ["--config", cfgPath, "start", "--foreground"], {
    cwd: repo,
    env,
    stdio: ["ignore", out, out],
  });
}

async function waitReady(child) {
  const deadline = Date.now() + 60000;
  while (Date.now() < deadline) {
    if (child.exitCode !== null) throw new Error(`daemon exited early with code ${child.exitCode}; see ${logPath}`);
    try {
      const r = await call("GET", `${BASE}/api/config`);
      if (r.status === 200) return;
    } catch {
      // not listening yet
    }
    await sleep(500);
  }
  throw new Error("daemon did not become ready within 60s");
}

async function stopChild(child, label) {
  if (!child || child.exitCode !== null || child.signalCode !== null) return;
  child.kill("SIGTERM");
  const exited = await Promise.race([
    new Promise((r) => child.once("exit", () => r(true))),
    sleep(15000).then(() => false),
  ]);
  if (!exited) child.kill("SIGKILL");
  evidence(`${label} stopped (pid ${child.pid})`);
}

function pinLogLines() {
  if (!existsSync(logPath)) return [];
  return readFileSync(logPath, "utf8")
    .split("\n")
    .filter((l) => l.includes("[pin]"));
}

// ---- steps ----

async function stepA_collisionWarning() {
  const cfg = await call("GET", `${BASE}/api/config`);
  const cap = await call("GET", `${BASE}/api/capacity`);
  const hasWarning = (obj) =>
    Array.isArray(obj?.model_node_warnings) &&
    obj.model_node_warnings.some(
      (w) =>
        w.node === NODE &&
        w.message.includes(BASE_MODEL) &&
        w.message.includes(PIN) &&
        w.message.includes(NODE),
    );
  evidence(`GET /api/config model_node_warnings: ${JSON.stringify(cfg.json?.model_node_warnings ?? null)}`);
  // /api/capacity names the same list "warnings" (capacityResponse in capacity.go).
  const capHasWarning = (obj) =>
    Array.isArray(obj?.warnings) && hasWarning({ model_node_warnings: obj.warnings });
  evidence(`GET /api/capacity warnings: ${JSON.stringify(cap.json?.warnings ?? null)}`);
  check(
    "collision-warning",
    hasWarning(cfg.json) && capHasWarning(cap.json),
    `both /api/config and /api/capacity name ${BASE_MODEL}, ${PIN} and node ${NODE}`,
  );
}

async function stepB_pinSubstitution() {
  await fakeLoaded([{ name: PIN, model: PIN, expires_at: ZERO_TIME }]);
  await fakeClear();
  const ask = await call("POST", `${BASE}/api/ask`, { question: "ping-b" });
  evidence(`POST /api/ask (no model) -> HTTP ${ask.status}: ${ask.text.slice(0, 200)}`);
  const reqs = await fakeRequests();
  evidence(`fake /api/generate received: ${JSON.stringify(reqs.map((r) => ({ model: r.model, keep_alive: r.keep_alive, prompt: r.prompt })))}`);
  const ok =
    ask.status === 200 &&
    reqs.some((r) => r.prompt === "ping-b" && r.model === PIN) &&
    generateFor(reqs, BASE_MODEL).length === 0;
  check("pin-substitution", ok, `implicit ask ran ${PIN} (config default ${BASE_MODEL} never reached fake)`);
}

async function setSwapAllowed(allowed) {
  const got = await call("GET", `${BASE}/api/compute/nodes/${NODE}`);
  if (got.status !== 200) throw new Error(`GET node ${NODE}: HTTP ${got.status} ${got.text}`);
  const node = got.json.node ?? got.json;
  const put = await call("PUT", `${BASE}/api/compute/nodes/${NODE}?probe=skip`, { ...node, allow_model_swap: allowed });
  if (put.status !== 200) throw new Error(`PUT node ${NODE} allow_model_swap=${allowed}: HTTP ${put.status} ${put.text}`);
  evidence(`PUT /api/compute/nodes/${NODE}?probe=skip allow_model_swap=${allowed} -> HTTP ${put.status}`);
}

async function stepC_swapRefused() {
  await fakeClear();
  const refused = await call("POST", `${BASE}/api/ask`, { question: "ping-c1", model: SWAP });
  evidence(`POST /api/ask model=${SWAP} (swap off) -> HTTP ${refused.status}: ${refused.text.trim()}`);
  let reqs = await fakeRequests();
  evidence(`fake received after refused request: ${JSON.stringify(reqs.map((r) => r.model))}`);
  const refusedOk =
    refused.status === 409 &&
    refused.text.includes("refused") &&
    refused.text.includes(PIN) &&
    generateFor(reqs, SWAP).length === 0;
  evidence(`refusal honored (409 + clear message + no ${SWAP} request): ${refusedOk}`);

  await setSwapAllowed(true);
  await fakeClear();
  const honored = await call("POST", `${BASE}/api/ask`, { question: "ping-c2", model: SWAP });
  reqs = await fakeRequests();
  evidence(`POST /api/ask model=${SWAP} (swap on) -> HTTP ${honored.status}; fake received: ${JSON.stringify(reqs.map((r) => ({ model: r.model, prompt: r.prompt })))}`);
  const honoredOk = honored.status === 200 && reqs.some((r) => r.prompt === "ping-c2" && r.model === SWAP);
  await setSwapAllowed(false);
  check("swap-refused", refusedOk && honoredOk, `${SWAP} refused while swap off, honored once allow_model_swap=true; allow_model_swap restored to false`);
}

async function stepD_repinAlert() {
  await fakeLoaded([{ name: SWAP, model: SWAP, expires_at: ZERO_TIME }]); // pinned model unloaded, another resident
  await fakeClear();
  evidence(`fake loaded list set to ${SWAP} only (pinned ${PIN} unloaded); waiting ${KEEPER_WAIT_MS / 1000}s for one keeper interval (pin_check_seconds 5)`);
  await sleep(KEEPER_WAIT_MS);
  const reqs = await fakeRequests();
  const repins = reqs.filter((r) => r.model === PIN && r.keep_alive === -1 && r.prompt === "");
  evidence(`fake received ${repins.length} re-pin(s) in the window: ${JSON.stringify(repins)}`);
  const alerts = await call("GET", `${BASE}/api/alerts`);
  const alertText = `Repinned ${PIN} on ${NODE}`;
  const alertOk = alerts.status === 200 && alerts.text.includes(alertText);
  evidence(`GET /api/alerts contains "${alertText}": ${alertOk}`);
  evidence(`daemon [pin] log lines so far (log is append-only; includes a boot-time pin of the empty fake): ${JSON.stringify(pinLogLines())}`);
  check("repin-alert", repins.length > 0 && alertOk, `keep_alive -1 re-pin of ${PIN} arrived and eviction alert was raised`);
}

async function stepE_keeperOff(daemon) {
  const pinLinesBefore = pinLogLines().length;
  const put = await call("PUT", `${BASE}/api/config`, { pin_check_seconds: 0 });
  evidence(`PUT /api/config pin_check_seconds=0 -> HTTP ${put.status}`);
  if (put.status !== 200) throw new Error(`PUT pin_check_seconds=0 failed: ${put.text}`);
  const saved = readFileSync(cfgPath, "utf8");
  evidence(`saved config has pin_check_seconds: 0 -> ${/pin_check_seconds:\s*0\b/.test(saved)}`);
  // The keeper set is built once at daemon start, so restart the daemon to apply 0.
  await stopChild(daemon.child, "daemon (restart to apply pin_check_seconds=0)");
  const restarted = startDaemon();
  await waitReady(restarted);
  daemon.child = restarted;
  const cfg = await call("GET", `${BASE}/api/config`);
  evidence(`after restart GET /api/config pin_check_seconds: ${cfg.json?.pin_check_seconds}`);
  await fakeLoaded([{ name: SWAP, model: SWAP, expires_at: ZERO_TIME }]); // pinned model still unloaded
  await fakeClear();
  await sleep(KEEPER_WAIT_MS);
  const reqs = await fakeRequests();
  const repins = reqs.filter((r) => r.keep_alive === -1);
  evidence(`fake requests after ${KEEPER_WAIT_MS / 1000}s with keeper off: ${JSON.stringify(reqs.map((r) => ({ model: r.model, keep_alive: r.keep_alive })))}`);
  evidence(`daemon [pin] log lines: ${pinLinesBefore} before the restart, ${pinLogLines().length} after the keeper-off window`);
  check("keeper-off", cfg.json?.pin_check_seconds === 0 && repins.length === 0, `pin_check_seconds=0, no keep_alive -1 request arrived`);
}

// ---- main ----

async function main() {
  rmSync(runDir, { recursive: true, force: true });
  mkdirSync(homeDir, { recursive: true });
  writeConfig(5);

  const fake = startFake();
  const daemon = { child: null };
  let failure = null;
  try {
    await sleep(500);
    evidence(`fake ollama on ${FAKE} (pid ${fake.pid}); daemon data_dir ${dataDir}`);
    daemon.child = startDaemon();
    evidence(`daemon started on ${BASE} (pid ${daemon.child.pid}), config ${cfgPath}`);
    await waitReady(daemon.child);

    await stepA_collisionWarning();
    await stepB_pinSubstitution();
    await stepC_swapRefused();
    await stepD_repinAlert();
    await stepE_keeperOff(daemon);
  } catch (err) {
    failure = err;
    evidence(`STOPPED: ${err.message}`);
  } finally {
    await stopChild(daemon.child, "daemon");
    await stopChild(fake, "fake ollama");
    const markerOrder = ["collision-warning", "pin-substitution", "swap-refused", "repin-alert", "keeper-off"];
    const body = [
      "# model-pin smoke evidence (tmp/verify/model-pin-smoke.mjs)",
      `# fake ollama ${FAKE}, daemon ${BASE}, node ${NODE}, pinned ${PIN}, config default ${BASE_MODEL}, pin_check_seconds 5`,
      ...lines,
      ...markerOrder.filter((m) => passed.has(m)).map((m) => `${m}: ok`),
      "",
    ].join("\n");
    writeFileSync(evidencePath, body);
    evidence(`evidence written to ${evidencePath}; markers: ${[...passed].join(", ") || "none"}`);
  }
  if (failure) process.exitCode = 1;
}

main();
