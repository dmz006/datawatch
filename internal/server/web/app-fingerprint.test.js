// Federation resource fingerprint exchange — PWA side.
//
// The daemon exposes `fingerprint_sync` on each federation peer (opt-in,
// default off; PUT /api/federation/peers/{name} updates the named fields and
// is the only path an edit goes through), plus the pending/confirmed match
// suggestion list (GET /api/federation/fingerprint-matches?state=...) and the
// per-match decision routes (POST .../{id}/confirm, POST .../{id}/reject).
// The PWA must: keep a per-peer fingerprint-sync toggle in the peer form,
// render the pending matches with local/peer resource names and kind, post
// the operator's confirm/reject decision, reload the panel afterwards, and
// surface confirmed matches (each still rejectable). The LLM form must carry
// the `billing_group` label (empty clears it). Locales: every new i18n key
// exists in all five bundles and the high-visibility ones guard via the
// mustHave list.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const APP_SRC = fs.readFileSync(APP_JS, 'utf8');

const NEW_KEYS = [
  'federation_fingerprint_sync_label',
  'federation_fingerprint_matches_title',
  'federation_fingerprint_matches_empty',
  'federation_fingerprint_confirm_btn',
  'federation_fingerprint_reject_btn',
  'federation_fingerprint_kind_node',
  'federation_fingerprint_kind_billing',
  'llm_billing_group_label',
];

const SAMPLE_PENDING = [
  {
    id: 'm-billing-1',
    kind: 'billing',
    local_name: 'shared-plan-a',
    peer: 'b',
    peer_name: 'shared-plan-a',
    state: 'pending',
    created_at: '2026-10-19T12:00:00Z',
  },
  {
    id: 'm-node-1',
    kind: 'node',
    local_name: 'node-x',
    peer: 'c',
    peer_name: 'x',
    state: 'pending',
    created_at: '2026-10-19T12:05:00Z',
  },
];

// loadApp builds a fresh sandbox, runs the REAL app.js top level, installs
// the English bundle so t() resolves the real user-visible strings, and
// wires a fetch stub that records every call and answers the fingerprint /
// peers endpoints with the given data.
function loadApp({ pending = [], confirmed = [], peers = [] } = {}) {
  const calls = [];
  const byId = {};
  const sandbox = buildSandbox({ byId });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  sandbox.__calls = calls;
  sandbox.fetch = (url, opts) => {
    calls.push({ url, opts: opts || {} });
    const body = /fingerprint-matches\?state=confirmed/.test(url)
      ? { matches: confirmed }
      : /fingerprint-matches\?state=pending/.test(url)
        ? { matches: pending }
        : /\/api\/federation\/peers\/[^/]+$/.test(String(url))
          ? {} // single-peer GET/PUT
          : /\/api\/federation\/peers($|\?)/.test(String(url))
            ? peers
            : {};
    return Promise.resolve({ ok: true, status: 200, json: async () => body });
  };
  return { sandbox, byId, calls };
}

// inputEl / chkEl are the plain stand-ins registerElement passes into
// byId — app.js reads .value / .checked straight off them.
function inputEl(v) { return { value: v, style: {} }; }
function chkEl(c) { return { checked: !!c, style: {} }; }

test('app.js exposes the fingerprint-exchange functions and the settings markup is wired', () => {
  const { sandbox } = loadApp();
  for (const fn of ['loadFingerprintMatchesPanel', 'fedMatchDecision', 'fingerprintMatchRow', 'showFedPeerForm', 'submitFedPeerForm']) {
    assert.equal(typeof sandbox[fn], 'function', `${fn} must be a function`);
  }
  // The per-peer toggle, the panel container, the label lookups, and the
  // tab-load hook all have to be present for the panel to function.
  assert.match(APP_SRC, /id="fedPeerFormFingerprintSync"/);
  assert.match(APP_SRC, /t\('federation_fingerprint_sync_label'\)/);
  assert.match(APP_SRC, /id="fedFingerprintMatchesList"/);
  assert.match(APP_SRC, /t\('federation_fingerprint_matches_title'\)/);
  assert.match(APP_SRC, /loadFingerprintMatchesPanel\(\);/);
  assert.match(APP_SRC, /fingerprint_sync:\s*fingerprintSync/); // PUT body carries the toggle explicitly
  assert.match(APP_SRC, /if \(fingerprintSync\) body.fingerprint_sync = true;/); // POST opts in only
});

test('the pending panel renders each match with kind, local + peer resource, and both actions', async () => {
  const { sandbox, byId } = loadApp({ pending: SAMPLE_PENDING });
  const cap = { style: {}, innerHTML: '' };
  byId['fedFingerprintMatchesList'] = cap;
  sandbox.loadFingerprintMatchesPanel();
  await flushAsync(20);
  const html = cap.innerHTML;
  assert.match(html, /shared-plan-a/);
  assert.match(html, /node-x/);
  assert.match(html, /billing/); // kind label (en bundle: "billing")
  assert.match(html, /node/);   // kind label (en bundle: "node")
  assert.match(html, /fedMatchDecision\(/);
  assert.match(html, /Confirm/);
  assert.match(html, /Reject/);
  assert.doesNotMatch(html, /<script/i);
});

test('the pending panel shows the localized empty state when there are no matches', async () => {
  const { sandbox, byId } = loadApp({ pending: [], confirmed: [] });
  const cap = { style: {}, innerHTML: '' };
  byId['fedFingerprintMatchesList'] = cap;
  sandbox.loadFingerprintMatchesPanel();
  await flushAsync(20);
  assert.match(cap.innerHTML, /No pending resource matches/);
  assert.doesNotMatch(cap.innerHTML, /fedMatchDecision\(/);
  assert.doesNotMatch(cap.innerHTML, /<details/);
});

test('confirmed matches render collapsed and each row keeps a reject action', async () => {
  const confirmed = [
    { id: 'm-billing-9', kind: 'billing', local_name: 'shared-plan-a', peer: 'b', peer_name: 'shared-plan-a', state: 'confirmed' },
  ];
  const { sandbox, byId } = loadApp({ pending: [], confirmed });
  const cap = { style: {}, innerHTML: '' };
  byId['fedFingerprintMatchesList'] = cap;
  sandbox.loadFingerprintMatchesPanel();
  await flushAsync(20);
  assert.match(cap.innerHTML, /<details/);
  assert.match(cap.innerHTML, /<summary/);
  assert.match(cap.innerHTML, /confirmed \(1\)/);
  assert.match(cap.innerHTML, /shared-plan-a/);
  assert.match(cap.innerHTML, /Confirm/);
  assert.match(cap.innerHTML, /Reject/);
});

test('fedMatchDecision posts to the per-match decision route and the panel reloads', async () => {
  const { sandbox, byId, calls } = loadApp({ pending: SAMPLE_PENDING });
  byId['fedFingerprintMatchesList'] = { style: {}, innerHTML: '' };
  sandbox.fedMatchDecision('m-billing-1', 'confirm');
  await flushAsync(20);
  const put = calls.find(c => c.url === '/api/federation/fingerprint-matches/m-billing-1/confirm');
  assert.ok(put, 'confirm POST was sent');
  assert.equal(put.opts.method, 'POST');

  calls.length = 0;
  sandbox.fedMatchDecision('m-node-1', 'reject');
  await flushAsync(20);
  const rej = calls.find(c => c.url === '/api/federation/fingerprint-matches/m-node-1/reject');
  assert.ok(rej, 'reject POST was sent');
  assert.equal(rej.opts.method, 'POST');
  // The panel reloaded: at least one fresh GET to the state list happened.
  assert.ok(calls.some(c => /fingerprint-matches\?state=pending/.test(c.url)), 'panel reloaded');
});

test('editing an existing peer is a PUT to the peer path with fingerprint_sync always explicit', async () => {
  // fpSyncChecked = false — the operator did not touch the toggle: the PUT
  // still carries fingerprint_sync (false) so the saved state round-trips.
  for (const [entry, checked, expectFp] of [
    [{ name: 'b', url: 'https://b.test' }, false, false],
    [{ name: 'b', url: 'https://b.test', fingerprint_sync: true }, true, true],
    [{ name: 'b', url: 'https://b.test', fingerprint_sync: true }, false, false], // operator unchecked
  ]) {
    const { sandbox, byId, calls } = loadApp();
    byId['fedPeerFormWrap'] = { style: {} };
    byId['fedPeerFormName'] = inputEl(entry.name);          // readonly in edit mode
    byId['fedPeerFormURL'] = inputEl('http://10.0.0.9:8083');
    byId['fedPeerFormToken'] = inputEl('');
    byId['fedPeerFormChannelIdentity'] = inputEl('');
    byId['fedPeerFormTLSSkipVerify'] = chkEl(false);
    byId['fedPeerFormFingerprintSync'] = chkEl(checked);
    byId['fedPeerFormError'] = { style: { display: 'none' }, textContent: '' };

    sandbox.showFedPeerForm(entry);
    await flushAsync(10);
    assert.ok(sandbox._fedPeerFormEntry, 'edit mode tracks the entry');
    // Operator edits the URL and sets the toggle to its final state AFTER the
    // form pre-filled from the record (pre-fill is overwritten by the operator).
    byId['fedPeerFormURL'].value = 'http://10.0.0.9:8083';
    byId['fedPeerFormFingerprintSync'].checked = checked;

    sandbox.submitFedPeerForm();
    await flushAsync(20);

    const put = calls.find(c => c.url === '/api/federation/peers/' + encodeURIComponent(entry.name));
    assert.ok(put, 'edit goes through PUT /api/federation/peers/' + entry.name);
    assert.equal(put.opts.method, 'PUT');
    const body = JSON.parse(put.opts.body);
    assert.ok('fingerprint_sync' in body, 'fingerprint_sync is always explicit in an edit');
    assert.equal(body.fingerprint_sync, expectFp);
    assert.equal(body.url, 'http://10.0.0.9:8083');
    assert.ok(!('name' in body), 'the name is not part of the edit body');
    assert.equal(sandbox._fedPeerFormEntry, null, 'edit context cleared after submit');
  }
});

test('adding a new peer is still a POST and fingerprint_sync rides along only when opted in', async () => {
  for (const [checked, expectKey] of [[false, false], [true, true]]) {
    const { sandbox, byId, calls } = loadApp();
    byId['fedPeerFormWrap'] = { style: {} };
    byId['fedPeerFormName'] = inputEl('new-b');
    byId['fedPeerFormURL'] = inputEl('http://10.0.0.9:8083');
    byId['fedPeerFormToken'] = inputEl('');
    byId['fedPeerFormChannelIdentity'] = inputEl('');
    byId['fedPeerFormTLSSkipVerify'] = chkEl(false);
    byId['fedPeerFormFingerprintSync'] = chkEl(checked);
    byId['fedPeerFormError'] = { style: { display: 'none' }, textContent: '' };

    sandbox.showFedPeerForm(); // add mode — no entry
    await flushAsync(10);
    assert.equal(sandbox._fedPeerFormEntry, null, 'add mode has no tracked entry');
    // Operator types the URL and opts in/out AFTER the form reset the fields.
    byId['fedPeerFormURL'].value = 'http://10.0.0.9:8083';
    byId['fedPeerFormFingerprintSync'].checked = checked;

    sandbox.submitFedPeerForm();
    await flushAsync(20);

    const post = calls.find(c => c.url === '/api/federation/peers' && c.opts.method === 'POST');
    assert.ok(post, 'new peer is a POST to /api/federation/peers');
    const body = JSON.parse(post.opts.body);
    assert.equal(body.name, 'new-b');
    assert.ok(('fingerprint_sync' in body) === expectKey, `fingerprint_sync present iff opted in (${checked})`);
    if (expectKey) assert.equal(body.fingerprint_sync, true);
  }
});

test('showFedPeerForm pre-fills the toggle from the record and defaults it off otherwise', async () => {
  for (const [entry, expected] of [
    [{ name: 'b', url: 'u', fingerprint_sync: true }, true],
    [{ name: 'b', url: 'u', fingerprint_sync: false }, false],
    [{ name: 'b', url: 'u' }, false],           // record carries no saved state
    [undefined, false],                          // new peer
  ]) {
    const { sandbox, byId } = loadApp();
    byId['fedPeerFormWrap'] = { style: {} };
    byId['fedPeerFormName'] = inputEl('b');
    byId['fedPeerFormURL'] = inputEl('u');
    byId['fedPeerFormToken'] = inputEl('');
    byId['fedPeerFormChannelIdentity'] = inputEl('');
    byId['fedPeerFormFingerprintSync'] = chkEl(!expected); // stale opposite value
    sandbox.showFedPeerForm(entry);
    await flushAsync(10);
    assert.equal(byId['fedPeerFormFingerprintSync'].checked, expected, `toggle pre-filled (${entry ? 'edit' : 'add'})`);
  }
});

test('the LLM form carries a billing-group field and the collector round-trips it', () => {
  const { sandbox } = loadApp();
  // Render: the field exists, bound to l.billing_group, labelled through t().
  const html = vm.runInContext(`buildLLMBudgetGroup({ billing_group: 'shared-plan-a' })`, sandbox);
  assert.match(html, /id="fe_llm_billing_group"/);
  assert.match(html, /value="shared-plan-a"/);
  assert.match(html, /federation_fingerprint_sync_label|llm_billing_group_label|Billing group/);

  // Collect: trimmed into out.billing_group; an empty value clears it.
  const collect = (val) => {
    sandbox.document.getElementById = (id) => (id === 'fe_llm_billing_group' ? inputEl(val) : null);
    const getBadge = sandbox.getBadgeInputValue;
    sandbox.getBadgeInputValue = (c) => '';
    const out = vm.runInContext('collectLLMForm({})', sandbox);
    sandbox.getBadgeInputValue = getBadge;
    return out;
  };
  const withGroup = collect('  shared-plan-a  ');
  assert.equal(withGroup.billing_group, 'shared-plan-a', 'trimmed round-trip');
  const cleared = collect('   ');
  assert.ok(!('billing_group' in cleared), 'blank value clears the field');
});

test('all new i18n keys exist in every locale bundle and guard the locales test', () => {
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', `${lang}.json`), 'utf8'));
    for (const k of NEW_KEYS) {
      assert.ok(bundle[k] && String(bundle[k]).trim().length > 0, `${lang} missing ${k}`);
    }
  }
  const localeTest = fs.readFileSync(path.join(__dirname, '..', 'v5280_locales_test.go'), 'utf8');
  for (const k of NEW_KEYS) {
    assert.match(localeTest, new RegExp(`"${k}"`), `v5280_locales_test.go mustHave lacks ${k}`);
  }
});
