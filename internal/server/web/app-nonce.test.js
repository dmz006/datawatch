// SEC-006 — regression tests for the ?token=-in-URL removal: the real
// token must never again appear in a URL this file builds, and the new
// nonce-minting helpers must behave correctly (including the two-
// separate-nonces fix for the file viewer, since nonces are single-use).
// Run: `node --test internal/server/web/*.test.js`.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS(opts) {
  const sandbox = buildSandbox(opts);
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox._mintAuthNonce, 'function', '_mintAuthNonce is not defined -- did it get renamed?');
  return sandbox;
}

test('_mintAuthNonce returns "" when no token is stored (SEC-001 empty-token posture)', async () => {
  const sandbox = loadAppJS();
  const result = await vm.runInContext('_mintAuthNonce()', sandbox);
  assert.equal(result, '');
});

test('_mintAuthNonce calls POST /api/auth/nonce with the Authorization header, returns the nonce', async () => {
  const sandbox = loadAppJS();
  sandbox.localStorage.setItem('cs_token', 'the-real-token');
  let capturedUrl, capturedOpts;
  sandbox.fetch = (url, opts) => {
    capturedUrl = url; capturedOpts = opts;
    return Promise.resolve({ ok: true, json: () => Promise.resolve({ nonce: 'abc123', expires_at: '2026-01-01T00:00:00Z' }) });
  };
  const result = await vm.runInContext('_mintAuthNonce()', sandbox);
  assert.equal(result, 'abc123');
  assert.equal(capturedUrl, '/api/auth/nonce');
  assert.equal(capturedOpts.method, 'POST');
  assert.equal(capturedOpts.headers.Authorization, 'Bearer the-real-token');
  // The real token must never appear anywhere BUT this one header.
  assert.ok(!JSON.stringify(capturedUrl).includes('the-real-token'));
});

test('_mintAuthNonce returns "" (not a throw) when the mint call fails', async () => {
  const sandbox = loadAppJS();
  sandbox.localStorage.setItem('cs_token', 'tok');
  sandbox.fetch = () => Promise.resolve({ ok: false, status: 401 });
  const result = await vm.runInContext('_mintAuthNonce()', sandbox);
  assert.equal(result, '');
});

test('buildWsUrl never includes ?token= or the raw token (SEC-006)', () => {
  const sandbox = loadAppJS();
  sandbox.localStorage.setItem('cs_token', 'super-secret-token');
  const url = vm.runInContext('buildWsUrl()', sandbox);
  assert.ok(!url.includes('token='), `buildWsUrl() must not put the token in the URL, got: ${url}`);
  assert.ok(!url.includes('super-secret-token'), `the real token leaked into the URL: ${url}`);
  assert.ok(url.endsWith('/ws'), `expected a bare /ws URL, got: ${url}`);
});

test('_downloadFile mints a nonce and sets href with ?nonce=, never ?token=', async () => {
  let createdHref = null;
  const link = { set href(v) { createdHref = v; }, get href() { return createdHref; }, set download(_v) {}, click() {}, remove() {} };
  const sandbox = loadAppJS({ byId: {} });
  sandbox.localStorage.setItem('cs_token', 'the-real-token');
  sandbox.fetch = () => Promise.resolve({ ok: true, json: () => Promise.resolve({ nonce: 'dl-nonce-1' }) });
  sandbox.document.createElement = (tag) => (tag === 'a' ? link : require('./testutil_browser_stub').makeStubElement());
  let appended = false;
  sandbox.document.body.appendChild = () => { appended = true; };

  await vm.runInContext("_downloadFile('/workspace/report.md')", sandbox);
  await flushAsync();

  assert.ok(appended, 'the throwaway <a> should be appended to the DOM before .click()');
  assert.ok(createdHref.includes('nonce=dl-nonce-1'), `expected ?nonce= in href, got: ${createdHref}`);
  assert.ok(!createdHref.includes('token='), `_downloadFile must never put the real token in the URL, got: ${createdHref}`);
});

// Regression test for a bug caught and fixed in review (2026-10-05): the
// file viewer originally minted ONE nonce and used it for both the
// inline-view fetch AND the download button's href -- since nonces are
// single-use, whichever one fired first would silently break the other.
test('_showFileViewer mints two SEPARATE nonces for the view fetch and the download link', async () => {
  const mintedTokens = [];
  const sandbox = loadAppJS();
  sandbox.localStorage.setItem('cs_token', 'the-real-token');
  let nonceCounter = 0;
  sandbox.fetch = (url) => {
    if (url === '/api/auth/nonce') {
      nonceCounter += 1;
      return Promise.resolve({ ok: true, json: () => Promise.resolve({ nonce: 'nonce-' + nonceCounter }) });
    }
    // The inline-view GET itself.
    mintedTokens.push(url);
    return Promise.resolve({ ok: true, text: () => Promise.resolve('file contents') });
  };

  await vm.runInContext("_showFileViewer('/workspace/notes.txt')", sandbox);
  await flushAsync();

  assert.equal(nonceCounter, 2, 'expected exactly two separate nonce mints (view + download), not one shared');
  const viewFetchUrl = mintedTokens.find(u => String(u).includes('/api/files/download'));
  assert.ok(viewFetchUrl, 'expected the inline-view fetch to hit /api/files/download');
  assert.ok(viewFetchUrl.includes('nonce=nonce-1'), `view fetch should use the first-minted nonce, got: ${viewFetchUrl}`);
});
