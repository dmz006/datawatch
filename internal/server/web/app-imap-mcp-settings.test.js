// BL404 — the imap-mcp channel gained a PWA Settings → Comms surface.
// GET /api/config returns imap_mcp.{enabled,url,account,subject_prefix,
// token,to} (token masked as "***") and PUT /api/config accepts
// imap_mcp.<field>. This pins the declarative BACKEND_FIELDS entry that
// drives showBackendConfigPopup/saveBackendConfig, that the bearer token
// stays a password field (never rendered as plaintext), and that both
// backend lists (Settings config status + Observer comm-backends status)
// include imap_mcp so the row actually appears.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  return sandbox;
}

// BACKEND_FIELDS is a top-level const (not a global property), so read it
// via runInContext and round-trip through JSON to avoid cross-realm
// comparisons.
function imapFields(sandbox) {
  return JSON.parse(vm.runInContext('JSON.stringify(BACKEND_FIELDS.imap_mcp || null)', sandbox));
}

test('BACKEND_FIELDS.imap_mcp exposes url, account, to, subject_prefix, token', () => {
  const fields = imapFields(loadAppJS());
  assert.ok(Array.isArray(fields), 'BACKEND_FIELDS.imap_mcp is missing');
  const keys = fields.map(f => f.key).sort();
  assert.deepEqual(keys, ['account', 'subject_prefix', 'to', 'token', 'url']);
});

test('imap_mcp token is a password field', () => {
  const fields = imapFields(loadAppJS());
  const token = fields.find(f => f.key === 'token');
  assert.equal(token.type, 'password');
});

test('both comm-backend service lists include imap_mcp', () => {
  const sandbox = loadAppJS();
  for (const fn of ['loadConfigStatus', 'loadCommBackendsStatus']) {
    const src = vm.runInContext(`${fn}.toString()`, sandbox);
    const m = src.match(/const services = \[([^\]]*)\]/);
    assert.ok(m, `${fn}: services array not found`);
    assert.match(m[1], /'imap_mcp'/, `${fn}: services list lacks imap_mcp`);
  }
});

test('isBackendConfigured treats imap_mcp as configured only once url is set', () => {
  const sandbox = loadAppJS();
  assert.equal(vm.runInContext(`isBackendConfigured('imap_mcp', {})`, sandbox), false);
  assert.equal(vm.runInContext(`isBackendConfigured('imap_mcp', { token: '***' })`, sandbox), false);
  assert.equal(vm.runInContext(`isBackendConfigured('imap_mcp', { url: 'http://localhost:8765' })`, sandbox), true);
});
