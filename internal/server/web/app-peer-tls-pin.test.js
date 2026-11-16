// TLS certificate pin + skip-verification pair — PWA side (server form +
// federation peer form).
//
// The daemon stores `tls_cert_fingerprint` (a SHA-256 pin) per server/peer
// alongside `tls_skip_verify`, and refuses the combination of the two; when
// the stored posture is insecure it appends a `warnings` array to the
// create/update/test responses. The PWA must: expose a pin field under each
// skip-verify checkbox (i18n label + tip), pre-fill it from
// entry.tls_cert_fingerprint on edit and clear it in add mode, send the
// trimmed value only when non-empty, show the inline skip-verify warning
// while the checkbox is on, keep pin and skip-verify mutually exclusive,
// and surface response `warnings` through the existing toast helper.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const APP_SRC = fs.readFileSync(APP_JS, 'utf8');

const NEW_KEYS = [
  'server_tls_cert_fingerprint_label',
  'server_tls_cert_fingerprint_tip',
  'server_tls_skip_verify_warning',
];
const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const PIN = 'aa'.repeat(32); // 64 hex chars — one valid SHA-256 fingerprint
const SKIP_WARNING = 'TLS certificate verification is disabled for this server — use only on a trusted network.';

// loadApp runs the REAL app.js top level in a fresh sandbox, installs the
// English bundle so t() resolves the real user-visible strings, and wires a
// fetch stub that records every call and answers with `response` (default
// {} — the panel/list loaders all degrade gracefully on a non-array body).
function loadApp(response = {}) {
  const calls = [];
  const byId = {};
  const sandbox = buildSandbox({ byId });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  sandbox.fetch = (url, opts) => {
    calls.push({ url: String(url), opts: opts || {} });
    return Promise.resolve({
      ok: true, status: 200,
      json: async () => response,
      text: async () => JSON.stringify(response),
    });
  };
  return { sandbox, byId, calls };
}

// inputEl / chkEl / warnEl are the plain stand-ins registered into byId —
// app.js reads .value / .checked / .style straight off them.
function inputEl(v) { return { value: String(v == null ? '' : v), style: {} }; }
function chkEl(c) { return { checked: !!c, style: {} }; }
function warnEl() { return { style: { display: 'none' }, textContent: '' }; }

// Register every element the two forms touch so saveServer/submitFedPeerForm
// and the show* pre-fill paths all see a real stand-in (the generic stub
// would swallow the values under test).
function registerServerForm(byId) {
  byId['serverFormWrap'] = { style: {} };
  byId['serverFormName'] = inputEl('alpha');
  byId['serverFormURL'] = inputEl('https://alpha.test:8443');
  byId['serverFormToken'] = inputEl('');
  byId['serverFormEnabled'] = chkEl(true);
  byId['serverFormEditName'] = inputEl('');
  byId['serverFormError'] = { style: {}, textContent: '' };
  byId['serverFormFederated'] = chkEl(false);
  byId['serverFormCapsWrap'] = { style: {} };
  byId['serverFormCaps'] = { querySelectorAll: () => [] };
  byId['serverFormCaps_input'] = { insertAdjacentHTML: () => {}, value: '', addEventListener: () => {} };
  byId['serverFormTLSSkipVerify'] = chkEl(false);
  byId['serverFormTLSCertFingerprint'] = inputEl('');
  byId['serverFormTLSWarn'] = warnEl();
}

function registerFedForm(byId) {
  byId['fedPeerFormWrap'] = { style: {} };
  byId['fedPeerFormName'] = inputEl('b');
  byId['fedPeerFormURL'] = inputEl('https://b.test:8083');
  byId['fedPeerFormToken'] = inputEl('');
  byId['fedPeerFormChannelIdentity'] = inputEl('');
  byId['fedPeerFormTLSSkipVerify'] = chkEl(false);
  byId['fedPeerFormTLSCertFingerprint'] = inputEl('');
  byId['fedPeerFormTLSWarn'] = warnEl();
  byId['fedPeerFormFingerprintSync'] = chkEl(false);
  byId['fedPeerFormObserverPull'] = chkEl(false);
  byId['fedPeerFormObserverPullInterval'] = inputEl('');
  byId['fedPeerFormObserverPullTimeout'] = inputEl('');
  byId['fedPeerFormError'] = { style: {}, textContent: '' };
}

// dockHas reports whether the alert dock (the toast pipeline's sink as of
// alpha.29 #271) carries an entry whose message contains `needle`.
function dockHas(sandbox, needle) {
  const alerts = (sandbox._alertDock && sandbox._alertDock.alerts) || [];
  return alerts.some(a => String(a.message || '').includes(needle));
}

test('both forms expose the pin field, the skip-verify warning, and the pair sync', () => {
  const { sandbox } = loadApp();
  assert.equal(typeof sandbox.tlsPinPairSync, 'function', 'tlsPinPairSync helper exists');
  assert.equal(typeof sandbox._surfaceResponseWarnings, 'function', 'warnings surfacer exists');

  // Pin input under each skip checkbox, both with the i18n label + tip.
  assert.match(APP_SRC, /id="serverFormTLSCertFingerprint"/);
  assert.match(APP_SRC, /id="fedPeerFormTLSCertFingerprint"/);
  assert.match(APP_SRC, /t\('server_tls_cert_fingerprint_label'\)/);
  assert.match(APP_SRC, /t\('server_tls_cert_fingerprint_tip'\)/);
  // The inline warning element + key for both forms.
  assert.match(APP_SRC, /id="serverFormTLSWarn"/);
  assert.match(APP_SRC, /id="fedPeerFormTLSWarn"/);
  assert.match(APP_SRC, /t\('server_tls_skip_verify_warning'\)/);
  // The checkbox and the pin input both drive the pair sync on change/typing.
  assert.match(APP_SRC, /onchange="tlsPinPairSync\('serverForm'\)"/);
  assert.match(APP_SRC, /oninput="tlsPinPairSync\('serverForm'\)"/);
  assert.match(APP_SRC, /onchange="tlsPinPairSync\('fedPeerForm'\)"/);
  assert.match(APP_SRC, /oninput="tlsPinPairSync\('fedPeerForm'\)"/);
});

test('server form sends the trimmed pin and omits the field when blank', async () => {
  for (const [val, expectKey] of [[`  ${PIN}  `, true], ['', false], ['   ', false]]) {
    const { sandbox, byId, calls } = loadApp();
    registerServerForm(byId);
    byId['serverFormTLSCertFingerprint'].value = val;
    byId['serverFormTLSSkipVerify'].checked = false;

    sandbox.saveServer();
    await flushAsync(20);

    const post = calls.find(c => c.url === '/api/servers' && c.opts.method === 'POST');
    assert.ok(post, 'POST /api/servers was sent');
    const body = JSON.parse(post.opts.body);
    assert.ok(('tls_cert_fingerprint' in body) === expectKey, `pin present iff non-blank (${JSON.stringify(val)})`);
    if (expectKey) assert.equal(body.tls_cert_fingerprint, PIN, 'pin is trimmed');
    assert.equal(body.tls_skip_verify, false);
  }
});

test('editing a server is a PUT that carries the pin', async () => {
  const { sandbox, byId, calls } = loadApp();
  registerServerForm(byId);
  byId['serverFormEditName'].value = 'alpha';
  byId['serverFormTLSCertFingerprint'].value = PIN;

  sandbox.saveServer();
  await flushAsync(20);

  const put = calls.find(c => c.url === '/api/servers/alpha' && c.opts.method === 'PUT');
  assert.ok(put, 'edit goes through PUT /api/servers/alpha');
  const body = JSON.parse(put.opts.body);
  assert.equal(body.tls_cert_fingerprint, PIN);
});

test('showServerForm pre-fills the saved pin, clears it in add mode, and keeps the pair consistent', async () => {
  // Edit with a saved pin: the field fills, the stale checkbox reverts,
  // and the warning stays hidden while the pin is set.
  const { sandbox, byId } = loadApp();
  registerServerForm(byId);
  byId['serverFormTLSSkipVerify'].checked = true;   // stale DOM state
  byId['serverFormTLSCertFingerprint'].value = 'stale';
  sandbox.showServerForm({ name: 'a', url: 'u', tls_cert_fingerprint: PIN });
  await flushAsync(10);
  assert.equal(byId['serverFormTLSCertFingerprint'].value, PIN, 'pin pre-filled from the record');
  assert.equal(byId['serverFormTLSSkipVerify'].checked, false, 'checkbox reverts against the pin');
  assert.equal(byId['serverFormTLSWarn'].style.display, 'none', 'no warning while the pin is the posture');

  // Add mode: any stale value is cleared.
  const { sandbox: sb2, byId: b2 } = loadApp();
  registerServerForm(b2);
  b2['serverFormTLSCertFingerprint'].value = 'stale';
  sb2.showServerForm(undefined);
  await flushAsync(10);
  assert.equal(b2['serverFormTLSCertFingerprint'].value, '', 'add mode clears the pin field');
});

test('showFedPeerForm pre-fills the saved pin and add mode clears it', async () => {
  const { sandbox, byId } = loadApp();
  registerFedForm(byId);
  byId['fedPeerFormTLSSkipVerify'].checked = true;  // stale DOM state
  byId['fedPeerFormTLSCertFingerprint'].value = 'stale';
  sandbox.showFedPeerForm({ name: 'b', url: 'u', tls_cert_fingerprint: PIN });
  await flushAsync(10);
  assert.equal(byId['fedPeerFormTLSCertFingerprint'].value, PIN, 'pin pre-filled from the record');
  assert.equal(byId['fedPeerFormTLSSkipVerify'].checked, false, 'checkbox reverts against the pin');
  assert.equal(byId['fedPeerFormTLSWarn'].style.display, 'none');

  const { sandbox: sb2, byId: b2 } = loadApp();
  registerFedForm(b2);
  b2['fedPeerFormTLSCertFingerprint'].value = 'stale';
  sb2.showFedPeerForm(); // add mode
  await flushAsync(10);
  assert.equal(b2['fedPeerFormTLSCertFingerprint'].value, '', 'add mode clears the pin field');
});

test('the skip-verify checkbox toggles the inline warning and the pair stays mutually exclusive', () => {
  for (const prefix of ['serverForm', 'fedPeerForm']) {
    const { sandbox, byId } = loadApp();
    const skip = chkEl(false);
    const pin = inputEl('');
    const warn = warnEl();
    byId[prefix + 'TLSSkipVerify'] = skip;
    byId[prefix + 'TLSCertFingerprint'] = pin;
    byId[prefix + 'TLSWarn'] = warn;

    // Checkbox on → warning shown; off → hidden.
    skip.checked = true; sandbox.tlsPinPairSync(prefix);
    assert.equal(warn.style.display, '', `${prefix}: warning shown while checked`);
    skip.checked = false; sandbox.tlsPinPairSync(prefix);
    assert.equal(warn.style.display, 'none', `${prefix}: warning hidden while unchecked`);

    // Typing a pin while the checkbox is on reverts the checkbox (and the
    // warning), and checking the box while a pin is typed reverts the box.
    skip.checked = true; pin.value = PIN; sandbox.tlsPinPairSync(prefix);
    assert.equal(skip.checked, false, `${prefix}: pin reverts the checkbox`);
    assert.equal(warn.style.display, 'none', `${prefix}: warning follows the reverted checkbox`);

    pin.value = PIN; skip.checked = true; sandbox.tlsPinPairSync(prefix);
    assert.equal(skip.checked, false, `${prefix}: checkbox reverts when a pin is present`);
  }
});

test('editing a federation peer PUTs the pin and adding one POSTs it; blank pins are omitted', async () => {
  // Edit (PUT): pin present in the body.
  {
    const { sandbox, byId, calls } = loadApp();
    registerFedForm(byId);
    sandbox.showFedPeerForm({ name: 'b', url: 'u' });
    await flushAsync(10);
    byId['fedPeerFormTLSCertFingerprint'].value = PIN;
    sandbox.submitFedPeerForm();
    await flushAsync(20);
    const put = calls.find(c => c.url === '/api/federation/peers/b' && c.opts.method === 'PUT');
    assert.ok(put, 'edit goes through PUT /api/federation/peers/b');
    assert.equal(JSON.parse(put.opts.body).tls_cert_fingerprint, PIN);
  }
  // Add (POST): pin present, and omitted when blank.
  for (const [val, expectKey] of [[PIN, true], ['', false]]) {
    const { sandbox, byId, calls } = loadApp();
    registerFedForm(byId);
    byId['fedPeerFormName'].value = 'new-b';
    byId['fedPeerFormTLSCertFingerprint'].value = val;
    sandbox.showFedPeerForm(); // add mode resets from nothing
    await flushAsync(10);
    byId['fedPeerFormTLSCertFingerprint'].value = val;
    sandbox.submitFedPeerForm();
    await flushAsync(20);
    const post = calls.find(c => c.url === '/api/federation/peers' && c.opts.method === 'POST');
    assert.ok(post, 'new peer is a POST to /api/federation/peers');
    const body = JSON.parse(post.opts.body);
    assert.equal(body.name, 'new-b');
    assert.ok(('tls_cert_fingerprint' in body) === expectKey, `pin present iff non-blank (${JSON.stringify(val)})`);
    if (expectKey) assert.equal(body.tls_cert_fingerprint, PIN);
  }
});

test('response warnings from create/update/test surface through the toast helper', async () => {
  // Server save (POST /api/servers) — the warnings array rides the response.
  {
    const W = 'warning: tls_skip_verify is set; connections are not verified';
    const { sandbox, byId } = loadApp({ warnings: [W] });
    registerServerForm(byId);
    sandbox.saveServer();
    await flushAsync(20);
    assert.ok(dockHas(sandbox, W), 'server save warning reached the toast pipeline');
  }
  // Server test (POST /api/servers/{name}/test).
  {
    const W = 'warning: tls_skip_verify is set; connections are not verified';
    const { sandbox, byId } = loadApp({ warnings: [W] });
    registerServerForm(byId);
    sandbox.testServerEntry('alpha', null);
    await flushAsync(20);
    assert.ok(dockHas(sandbox, W), 'server test warning reached the toast pipeline');
  }
  // Federation peer add (POST) and edit (PUT) and test.
  for (const mode of ['add', 'edit']) {
    const W = 'warning: tls_skip_verify is set; connections are not verified';
    const { sandbox, byId } = loadApp({ warnings: [W] });
    registerFedForm(byId);
    if (mode === 'edit') {
      sandbox.showFedPeerForm({ name: 'b', url: 'u' });
    } else {
      byId['fedPeerFormName'].value = 'new-b';
    }
    await flushAsync(10);
    sandbox.submitFedPeerForm();
    await flushAsync(20);
    assert.ok(dockHas(sandbox, W), `federation peer ${mode} warning reached the toast pipeline`);
  }
  {
    const W = 'warning: tls_skip_verify is set; connections are not verified';
    const { sandbox } = loadApp({ warnings: [W] });
    sandbox.testFedPeer('b');
    await flushAsync(20);
    assert.ok(dockHas(sandbox, W), 'federation peer test warning reached the toast pipeline');
  }
  // A response without warnings must not add anything.
  {
    const { sandbox, byId } = loadApp({ ok: true });
    registerServerForm(byId);
    sandbox.saveServer();
    await flushAsync(20);
    const alerts = (sandbox._alertDock && sandbox._alertDock.alerts) || [];
    assert.ok(!alerts.some(a => /warning:/.test(String(a.message || ''))), 'no warning entry without a warnings array');
  }
});

test('the inline warning renders the localized string from the bundle', () => {
  const { sandbox, byId } = loadApp();
  registerServerForm(byId);
  byId['serverFormTLSSkipVerify'].checked = true;
  sandbox.tlsPinPairSync('serverForm');
  assert.equal(byId['serverFormTLSWarn'].style.display, '');
  // The element's content comes from t('server_tls_skip_verify_warning') —
  // the English bundle must carry that exact sentence.
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  assert.equal(en['server_tls_skip_verify_warning'], SKIP_WARNING);
});

test('all new i18n keys exist in every locale bundle and guard the locales test', () => {
  for (const lang of LOCALES) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', `${lang}.json`), 'utf8'));
    for (const k of NEW_KEYS) {
      assert.ok(bundle[k] && String(bundle[k]).trim().length > 0, `${lang} missing ${k}`);
      if (lang !== 'en') {
        const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
        assert.notEqual(bundle[k], en[k], `${lang} must carry a real translation for ${k}`);
      }
    }
  }
  const localeTest = fs.readFileSync(path.join(__dirname, '..', 'v5280_locales_test.go'), 'utf8');
  for (const k of NEW_KEYS) {
    assert.match(localeTest, new RegExp(`"${k}"`), `v5280_locales_test.go mustHave lacks ${k}`);
  }
});
