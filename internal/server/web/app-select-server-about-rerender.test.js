'use strict';
// selectServer() re-renders the About view with the chosen peer's picker chip
// highlighted, and the About version read goes through the host-aware
// /api/proxy/<peer>/ path.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-select-server-rerender.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const PEER = 'pi-node';

// Same harness as app-select-server-controls.test.js: real app.js in a vm
// context, the real selectServer(), and the side effects that touch the
// network or the socket neutralised. Here the DOM captures what
// renderSettingsView() and the picker-bar injection write.
function loadApp() {
  const sandbox = buildSandbox();
  const written = [];
  const makeEl = () => {
    let html = '';
    const el = {
      style: {}, dataset: {}, children: [], childNodes: [],
      classList: { add() {}, remove() {}, toggle() {}, contains() { return false; } },
      appendChild() {}, removeChild() {}, remove() {}, setAttribute() {},
      getAttribute() { return null; }, removeAttribute() {},
      addEventListener() {}, removeEventListener() {}, querySelector() { return null; },
      querySelectorAll() { return []; }, focus() {}, blur() {}, click() {}, scrollIntoView() {},
      getBoundingClientRect() { return { width: 0, height: 0, top: 0, left: 0, right: 0, bottom: 0 }; },
      insertBefore(node) { written.push(node.outerHTML); return node; },
      insertAdjacentHTML(pos, h) { written.push(h); },
      get firstChild() { return { outerHTML: html }; },
      set innerHTML(v) { html = v; written.push(v); },
      get innerHTML() { return html; },
      set textContent(v) { html = String(v); },
      get textContent() { return html; },
      set value(v) {}, get value() { return ''; },
    };
    return el;
  };
  const els = {};
  sandbox.document.createElement = () => makeEl();
  sandbox.document.getElementById = (id) => els[id] || (els[id] = makeEl());
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox.selectServer, 'function', 'selectServer is not defined -- renamed?');

  // Neutralise the side effects selectServer fans out to.
  sandbox.__toasts = [];
  sandbox.__urls = [];
  sandbox.showToast = (msg) => { sandbox.__toasts.push(String(msg)); };
  sandbox.connect = () => {};
  sandbox.loadServers = () => {};
  sandbox.loadServersList = () => {};
  sandbox._ensureLocalHostname = () => {};
  sandbox._checkFederatedConnection = () => {};
  sandbox._loadAllServersSessions = () => {};
  sandbox._mintAuthNonce = () => Promise.resolve('');
  sandbox.fetch = (url) => {
    sandbox.__urls.push(String(url));
    const body = String(url).includes('/api/health') ? { version: '1.0.0' } : [];
    return Promise.resolve({ ok: true, json: () => Promise.resolve(body) });
  };
  sandbox.__written = written;

  vm.runInContext(
    `state.servers = { servers: [{ name: ${JSON.stringify(PEER)}, label: ${JSON.stringify(PEER)}, enabled: true, url: 'https://x' }] };` +
    `state.activeServer = null; state.sessions = []; state.ws = null;` +
    `state.activeView = 'settings'; _settingsTab = 'about';`,
    sandbox,
  );
  return sandbox;
}

// Returns the bar/button markup for the peer's chip, as written into the view.
function peerChipMarkup(sandbox) {
  const all = sandbox.__written.join('\n');
  const re = new RegExp(`<button[^>]*data-server-name="${PEER}"[^>]*>`, 'g');
  const tags = all.match(re) || [];
  return tags;
}

test('selectServer re-renders the About view with the peer chip active and proxies the version read', async () => {
  const sandbox = loadApp();

  vm.runInContext(`selectServer(${JSON.stringify(PEER)})`, sandbox);
  await flushAsync(30);

  // The About view was re-rendered: the Settings render wrote the About tab
  // and the picker bar was injected again.
  assert.ok(sandbox.__written.some(h => typeof h === 'string' && h.includes('settings-section')),
    'About view (Settings render) must be re-rendered after the switch');

  // The peer's chip carries the active highlight (accent background, bold).
  const tags = peerChipMarkup(sandbox);
  assert.ok(tags.length > 0, `peer chip for ${PEER} must be rendered`);
  const chipHtml = sandbox.__written.join('\n');
  const idx = chipHtml.lastIndexOf(`data-server-name="${PEER}"`);
  const chip = chipHtml.slice(idx, chipHtml.indexOf('</button>', idx));
  assert.ok(chip.includes('background:var(--accent2,#4f8)'), `peer chip must be highlighted as active: ${chip}`);
  assert.ok(chip.includes('font-weight:600'), 'active peer chip must be bold');

  // The About version read goes through the host-aware path for the peer.
  assert.equal(vm.runInContext('state.activeServer', sandbox), PEER);
  assert.ok(sandbox.__urls.some(u => u.startsWith(`/api/proxy/${PEER}/`)),
    `About version read must go through /api/proxy/${PEER}/: ${JSON.stringify(sandbox.__urls)}`);
});
