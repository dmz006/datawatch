// Operator-reported (2026-10-07): connecting to a federated server in the
// PWA showed no host-picker, no spinner, and never surfaced sessions or an
// error -- stuck indefinitely with zero feedback. Two separate bugs:
//
// 1. state.servers defaulted to [] instead of undefined, so
//    _injectServerPickerBar's "haven't fetched yet" check (`state.servers
//    === undefined`) was always false -- the picker's own fetch never ran
//    at all unless some other view (Settings) happened to populate
//    state.servers as a side effect first.
// 2. Even when the fetch DID run, a single failure permanently set
//    state.servers = null with no retry, and a federated WS connection
//    failure had no visible status/error -- just silent, endless loading.
//
// This pins: the corrected default, that a loading state is now rendered
// (not nothing) while servers load, that a failed load retries instead of
// giving up forever, and that selecting a federated server drives a real,
// event-based connecting/connected/error status rather than a canned one.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, makeStubElement, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox.loadServerListEager, 'function', 'loadServerListEager is not defined -- did it get renamed?');
  assert.equal(typeof sandbox._injectServerPickerBar, 'function', '_injectServerPickerBar is not defined -- did it get renamed?');
  assert.equal(typeof sandbox._checkFederatedConnection, 'function', '_checkFederatedConnection is not defined -- did it get renamed?');
  return sandbox;
}

test('state.servers defaults to undefined, not [] -- the actual root cause of the picker never loading', () => {
  const sandbox = loadAppJS();
  assert.equal(vm.runInContext('state.servers', sandbox), undefined);
});

test('_injectServerPickerBar kicks off loadServerListEager (not nothing) while state.servers is undefined', () => {
  // The stub DOM's innerHTML setter doesn't parse into real child nodes
  // (no real layout engine), so the tmp.firstChild / insertBefore path
  // this function uses for DOM insertion isn't directly observable here
  // -- that's a harness limitation shared with the pre-existing
  // _serverPickerBar()/_injectServerPickerBar() code, not something new.
  // What IS observable and is the actual regression this pins: the loader
  // actually starts (state._serversLoading flips true) instead of the old
  // behavior of silently doing nothing because the undefined-check never
  // matched.
  const sandbox = loadAppJS();
  const container = makeStubElement();
  vm.runInContext('fetch = () => new Promise(() => {})', sandbox); // never resolves -- isolate the "still loading" state
  sandbox.__container = container;
  vm.runInContext('_injectServerPickerBar(__container, null)', sandbox);
  assert.equal(vm.runInContext('state._serversLoading', sandbox), true, 'the eager loader should have started');
});

test('loadServerListEager populates state.servers from a successful fetch', async () => {
  const sandbox = loadAppJS();
  const fakeServers = { servers: [{ name: 'host-a', url: 'https://host-a:8443', enabled: true }] };
  vm.runInContext(
    `fetch = () => Promise.resolve({ ok: true, json: () => Promise.resolve(${JSON.stringify(fakeServers)}) })`,
    sandbox
  );
  vm.runInContext('loadServerListEager()', sandbox);
  await flushAsync();
  const result = vm.runInContext('JSON.stringify(state.servers)', sandbox);
  assert.deepEqual(JSON.parse(result), fakeServers);
});

test('loadServerListEager does NOT poison state.servers to null on failure (retries instead of giving up forever)', async () => {
  const sandbox = loadAppJS();
  vm.runInContext(`fetch = () => Promise.resolve({ ok: false, status: 500 })`, sandbox);
  vm.runInContext('loadServerListEager()', sandbox);
  await flushAsync();
  // Must still be undefined ("haven't succeeded yet"), never null
  // ("permanently gave up") -- null was the old behavior that hid the
  // picker for the rest of the session after one transient failure.
  assert.equal(vm.runInContext('state.servers', sandbox), undefined);
});

test('_checkFederatedConnection sets phase=connecting immediately, then phase=connected on a successful health probe', async () => {
  const sandbox = loadAppJS();
  vm.runInContext(`fetch = () => Promise.resolve({ ok: true, json: () => Promise.resolve({}) })`, sandbox);
  vm.runInContext(`state.activeServer = 'host-a'`, sandbox);
  vm.runInContext(`_checkFederatedConnection('host-a')`, sandbox);
  const immediate = JSON.parse(vm.runInContext('JSON.stringify(state._fedConnStatus)', sandbox));
  assert.equal(immediate.phase, 'connecting', 'status must flip to connecting synchronously, before the probe resolves');
  assert.equal(immediate.server, 'host-a');
  await flushAsync();
  const after = JSON.parse(vm.runInContext('JSON.stringify(state._fedConnStatus)', sandbox));
  assert.equal(after.phase, 'connected');
});

test('_checkFederatedConnection sets phase=error with a real reason when the probe fails', async () => {
  const sandbox = loadAppJS();
  vm.runInContext(`fetch = () => Promise.resolve({ ok: false, status: 401 })`, sandbox);
  vm.runInContext(`state.activeServer = 'host-a'`, sandbox);
  vm.runInContext(`_checkFederatedConnection('host-a')`, sandbox);
  await flushAsync();
  const status = JSON.parse(vm.runInContext('JSON.stringify(state._fedConnStatus)', sandbox));
  assert.equal(status.phase, 'error');
  assert.match(status.message, /401/, 'error message should carry the real HTTP status, not a generic placeholder');
});

test('_checkFederatedConnection ignores a stale probe result after the operator already switched to a different server', async () => {
  const sandbox = loadAppJS();
  vm.runInContext(`fetch = () => Promise.resolve({ ok: true, json: () => Promise.resolve({}) })`, sandbox);
  vm.runInContext(`state.activeServer = 'host-a'`, sandbox);
  vm.runInContext(`_checkFederatedConnection('host-a')`, sandbox);
  // Operator switches away before the probe resolves.
  vm.runInContext(`state.activeServer = 'host-b'; state._fedConnStatus = null;`, sandbox);
  await flushAsync();
  assert.equal(vm.runInContext('state._fedConnStatus', sandbox), null, 'a stale host-a probe result must not overwrite the newer host-b state');
});

test('real session data arriving clears any in-progress federated connection status', () => {
  const sandbox = loadAppJS();
  vm.runInContext(`state._fedConnStatus = { server: 'host-a', phase: 'connecting' }`, sandbox);
  vm.runInContext(`handleMessage({ type: 'sessions', data: { sessions: [] } })`, sandbox);
  assert.equal(vm.runInContext('state._fedConnStatus', sandbox), null);
});

test('selectServer() kicks off a federated connection check when switching to a named remote server', () => {
  const sandbox = loadAppJS();
  vm.runInContext(`fetch = () => new Promise(() => {})`, sandbox);
  // Isolate selectServer's own branching from connect()'s real WS
  // handshake logic (covered elsewhere) -- the stub WebSocket doesn't
  // implement addEventListener.
  vm.runInContext(`connect = function(){}; loadServers = function(){};`, sandbox);
  vm.runInContext(`let __checkedServer = null; _checkFederatedConnection = (name) => { __checkedServer = name; };`, sandbox);
  vm.runInContext('selectServer("host-a")', sandbox);
  assert.equal(vm.runInContext('__checkedServer', sandbox), 'host-a');
});

test('selectServer() clears federated status when switching back to Local', () => {
  const sandbox = loadAppJS();
  vm.runInContext(`fetch = () => new Promise(() => {})`, sandbox);
  vm.runInContext(`connect = function(){}; loadServers = function(){};`, sandbox);
  vm.runInContext(`state.activeServer = 'host-a'; state._fedConnStatus = { server: 'host-a', phase: 'connecting' };`, sandbox);
  vm.runInContext('selectServer(null)', sandbox);
  assert.equal(vm.runInContext('state._fedConnStatus', sandbox), null);
});
