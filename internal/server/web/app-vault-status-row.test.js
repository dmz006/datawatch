// ef0a0003 — Settings Vault status row: `last_success` and `last_error`
// rendered as clearly labelled lines from GET /api/secrets/vault/status.
//
//   - 'Last success: <ISO time>' (the 'never' string when the backend has
//     never reported a successful read)
//   - 'Last error: <message>'    (the 'none' string when there is no error)
//   - every interpolated value is escaped via escHtml
//
// Loads the REAL, unmodified app.js into a stub browser, points its
// apiFetch at a fake status payload, drives loadVaultStatusRow(), and inspects
// what lands in the #vaultStatusRow element's innerHTML. The English bundle
// is active so the labelled lines resolve to their real "Last success"/
// "Last error" text. Run: `node --test internal/server/web/app-vault-status-row.test.js`

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');

// Load the real app.js into a fresh sandbox with an active English bundle and
// a #vaultStatusRow element stub that captures whatever innerHTML the row
// renderer writes. `apiFetchImpl` (optional) stands in for the network read
// of /api/secrets/vault/status.
function loadApp({ apiFetchImpl = null } = {}) {
  const el = makeStubElement();
  const sandbox = buildSandbox({ byId: { vaultStatusRow: el } });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  const en = JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, 'en.json'), 'utf8'));
  sandbox.window._i18n.fallback = en;
  sandbox.window._i18n.bundle = en;
  if (apiFetchImpl) sandbox.apiFetch = apiFetchImpl;
  return { sandbox, el };
}

// Render the row by driving loadVaultStatusRow() with a fake status payload
// and waiting for the (single) microtask of the .then() chain to settle.
async function renderRow(sandbox, payload) {
  sandbox.apiFetch = () => Promise.resolve(payload);
  sandbox.loadVaultStatusRow();
  await flushAsync();
}

// A status payload with a malicious last_error (a classic img-onerror XSS
// payload plus entities) and a concrete last_success timestamp.
const MALICIOUS_ERROR = '<img src=x onerror=alert(1)> & "quoted" \'single\'';
function activePayload(overrides = {}) {
  return {
    backend_active: true,
    status: Object.assign({
      reachable: true,
      last_success: '2025-01-01T00:10:00Z',
      last_error: MALICIOUS_ERROR,
      last_request_id: 'req-123',
      address: 'https://vault.example:8200',
      kv_mount: 'secret',
      path_prefix: 'datawatch',
      path_layout: 'flat',
    }, overrides),
  };
}

test('last_error + last_success are both defined as labelled lines', async () => {
  const { sandbox, el } = loadApp();
  await renderRow(sandbox, activePayload());
  const html = el.innerHTML;
  assert.ok(html.includes('Last success:'), `expected a 'Last success:' label, got: ${html}`);
  assert.ok(html.includes('Last error:'), `expected a 'Last error:' label, got: ${html}`);
});

test('last_success renders as an ISO timestamp', async () => {
  const { sandbox, el } = loadApp();
  await renderRow(sandbox, activePayload());
  const html = el.innerHTML;
  // '2025-01-01T00:10:00Z' -> toISOString() -> '2025-01-01T00:10:00.000Z'
  assert.ok(
    html.includes('2025-01-01T00:10:00.000Z'),
    `expected the ISO success timestamp on the 'Last success:' line, got: ${html}`);
});

test('last_error is escaped and never rendered as raw HTML', async () => {
  const { sandbox, el } = loadApp();
  await renderRow(sandbox, activePayload());
  const html = el.innerHTML;
  // escHtml turns the payload into entities; the raw tag must be absent.
  assert.ok(
    html.includes('&lt;img src=x onerror=alert(1)&gt;'),
    `expected the escaped error payload, got: ${html}`);
  assert.ok(
    !html.includes('<img src=x onerror='),
    `the raw, unescaped error payload must not reach innerHTML, got: ${html}`);
  // The ampersand and quotes are entity-encoded too.
  assert.ok(html.includes('&amp;'), `expected an escaped '&', got: ${html}`);
  assert.ok(html.includes('&quot;quoted&quot;'), `expected an escaped '"', got: ${html}`);
  assert.ok(html.includes('&#39;single&#39;'), `expected an escaped "'", got: ${html}`);
});

test('a never-successful backend shows the "never" string', async () => {
  const { sandbox, el } = loadApp();
  await renderRow(sandbox, activePayload({ last_success: null }));
  const html = el.innerHTML;
  assert.ok(html.includes('Last success:'), `expected a 'Last success:' label, got: ${html}`);
  // English bundle's vault_last_success_never = 'never'.
  assert.ok(html.includes('never'), `expected the 'never' fallback, got: ${html}`);
});

test('a backend with no error shows the "none" string', async () => {
  const { sandbox, el } = loadApp();
  await renderRow(sandbox, activePayload({ last_error: '' }));
  const html = el.innerHTML;
  assert.ok(html.includes('Last error:'), `expected a 'Last error:' label, got: ${html}`);
  // English bundle's vault_last_error_none = 'none'; and no error styling.
  assert.ok(html.includes('none'), `expected the 'none' fallback, got: ${html}`);
  assert.ok(
    !html.includes('color:var(--error);'),
    `an empty error must not carry the error colour, got: ${html}`);
});

test('a backend that is not active hides the whole row', async () => {
  const { sandbox, el } = loadApp();
  // Start with a stale-looking value to prove the renderer hides it.
  el.innerHTML = 'stale';
  el.style.display = '';
  await renderRow(sandbox, { backend_active: false, status: {} });
  assert.equal(el.style.display, 'none');
});

test('a backend that is unreachable still renders both labelled lines', async () => {
  const { sandbox, el } = loadApp();
  await renderRow(sandbox, activePayload({ reachable: false }));
  const html = el.innerHTML;
  assert.ok(html.includes('Last success:'), `expected a 'Last success:' label, got: ${html}`);
  assert.ok(html.includes('Last error:'), `expected a 'Last error:' label, got: ${html}`);
});

test('all five locales define the four new vault status keys', () => {
  const keys = [
    'vault_last_success_label',
    'vault_last_success_never',
    'vault_last_error_label',
    'vault_last_error_none',
  ];
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(
      fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
    for (const k of keys) {
      assert.ok(
        typeof bundle[k] === 'string' && bundle[k].length > 0,
        `${lang}.json must define a non-empty string for ${k}`);
    }
  }
});
