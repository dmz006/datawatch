// Settings → Branding / Splash card: the two new splash minimum-dwell
// settings — ui.splash_min_seconds (default 6) and
// ui.splash_min_seconds_new_version (default 9). Each renders with a
// visible label and a hint line (real English bundle), the client-side
// floors mirror the daemon (whole numbers 0 to 60, 0 = keep the legacy
// timing; ui.splash_min_seconds_new_version applies only when the version
// changed), out-of-range entries are reported with an error toast and are
// not saved, and the valid values ride the SAME settings PUT the splash
// tagline already uses (one PUT /api/config body, dotted ui.* keys next to
// the dotted session.* keys). fetch is stubbed and records every request;
// rendered text is the real English bundle.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const LOCATIONS = ['en', 'de', 'es', 'fr', 'ja'];
const SPLASH_SETTING_KEYS = [
  'settings_splash_min_seconds',
  'settings_splash_min_seconds_hint',
  'settings_splash_min_seconds_new_version',
  'settings_splash_min_seconds_new_version_hint',
];

// loadApp returns a sandbox with the app loaded and a recording fetch. routes
// maps "METHOD /path" to {status, body}; an unknown request answers 404.
// Every request is recorded in __calls with its parsed JSON body; every toast
// in __toasts.
function loadApp(routes, byId) {
  const sandbox = buildSandbox({ byId: byId || {} });
  const calls = [];
  sandbox.fetch = (url, opts) => {
    const method = (opts && opts.method) || 'GET';
    calls.push({ url, method, body: opts && opts.body ? JSON.parse(opts.body) : undefined });
    const hit = routes[method + ' ' + url];
    if (!hit) return Promise.resolve({ ok: false, status: 404, statusText: 'Not Found', text: () => Promise.resolve('not found') });
    const status = hit.status || 200;
    return Promise.resolve({
      ok: status < 400,
      status,
      statusText: String(status),
      json: () => Promise.resolve(hit.body),
      text: () => Promise.resolve(typeof hit.body === 'string' ? hit.body : JSON.stringify(hit.body)),
    });
  };
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  sandbox.__calls = calls;
  sandbox.__toasts = [];
  sandbox.showToast = (message, type) => { sandbox.__toasts.push({ message, type }); };
  return sandbox;
}

const CFG = {
  session: {
    splash_tagline: 'AI-powered dev assistant',
    splash_logo_path: '/opt/splash/logo.png',
  },
  ui: {
    splash_min_seconds: 6,
    splash_min_seconds_new_version: 9,
  },
};

const SPLASH_INFO = {
  tagline: 'AI-powered dev assistant',
  logo_url: '',
  version: '0.0.0',
  hostname: 'host',
};

const PUTS = sb => sb.__calls.filter(c => c.method === 'PUT' && c.url === '/api/config').map(c => ({ url: c.url, body: c.body }));

async function renderPanel(extraRoutes, userValues) {
  const panel = { innerHTML: '', style: {}, dataset: {}, children: [], childNodes: [],
    classList: { add() {}, remove() {}, contains: () => false },
    appendChild() {}, removeChild() {}, remove() {}, setAttribute() {},
    addEventListener() {}, removeEventListener() {}, getContext: () => null };
  // The stubs stand in for the rendered inputs; loadBrandingPanel's own
  // /api/config fill lands in these same objects, so userValues is applied
  // AFTER the async flush — simulating the operator typing into the field
  // after the card loaded.
  const stubs = {
    splashTagline: { value: 'AI-powered dev assistant' },
    splashLogoPath: { value: '/opt/splash/logo.png' },
    splashMinSeconds: { value: '' },
    splashMinSecondsNewVersion: { value: '' },
  };
  const sb = loadApp(Object.assign({
    'GET /api/splash/info': { body: SPLASH_INFO },
    'GET /api/config': { body: CFG },
  }, extraRoutes || {}), Object.assign({ brandingPanel: panel }, stubs));
  sb.loadBrandingPanel();
  await flushAsync();
  if (userValues) for (const [k, v] of Object.entries(userValues)) stubs[k].value = v;
  assert.ok(panel.innerHTML.length > 0, 'the Branding / Splash card rendered something');
  return { sb, panel };
}

// theInput returns the rendered <input ...> tag that carries the given id.
function theInput(html, id) {
  const marker = 'id="' + id + '"';
  const i = html.indexOf(marker);
  assert.notEqual(i, -1, 'id ' + id + ' rendered');
  const start = html.lastIndexOf('<input', i);
  assert.notEqual(start, -1, 'no <input before ' + marker);
  return html.slice(start, html.indexOf('>', i) + 1);
}

test('card renders both dwell settings with labels, hints, floors and loaded defaults', async () => {
  const { sb, panel } = await renderPanel();
  const html = panel.innerHTML;

  // Visible labels (real English bundle).
  assert.match(html, /Minimum splash seconds/);
  assert.match(html, /New-version splash seconds/);

  // Hint lines, including the "0 keeps the legacy timing" and the
  // version-changed scoping of the new-version value.
  assert.match(html, /0 keeps the legacy timing\./);
  assert.ok(html.includes('Applies only when the version changed since the last splash. 0 keeps the legacy timing.'),
    'new-version hint says it applies only when the version changed');

  // Both inputs are whole-number 0-60 ranges per the daemon's rule.
  let tag = theInput(html, 'splashMinSeconds');
  assert.match(tag, /type="number"/);
  assert.match(tag, /min="0"/);
  assert.match(tag, /max="60"/);
  assert.match(tag, /step="1"/);
  assert.match(tag, /placeholder="6"/);
  tag = theInput(html, 'splashMinSecondsNewVersion');
  assert.match(tag, /type="number"/);
  assert.match(tag, /min="0"/);
  assert.match(tag, /max="60"/);
  assert.match(tag, /step="1"/);
  assert.match(tag, /placeholder="9"/);

  // The loaded config lands in the rendered inputs' values.
  assert.equal(String(sb.document.getElementById('splashMinSeconds').value), '6');
  assert.equal(String(sb.document.getElementById('splashMinSecondsNewVersion').value), '9');
});

test('a card loaded without ui.* config falls back to the 6 / 9 defaults', async () => {
  const { sb } = await renderPanel({ 'GET /api/config': { body: { session: CFG.session } } });
  assert.equal(String(sb.document.getElementById('splashMinSeconds').value), '6');
  assert.equal(String(sb.document.getElementById('splashMinSecondsNewVersion').value), '9');
});

test('splashSecondsEntry mirrors the daemon: 0 and 60 pass, everything else fails', async () => {
  const { sb } = await renderPanel();
  assert.equal(sb.splashSecondsEntry('0'), 0, '0 = keep the legacy timing is legal');
  assert.equal(sb.splashSecondsEntry('60'), 60, '60 = the ceiling is legal');
  assert.equal(sb.splashSecondsEntry('6'), 6);
  assert.equal(sb.splashSecondsEntry('9'), 9);
  for (const bad of ['61', '-1', '', '2.5', 'abc', null]) {
    assert.equal(sb.splashSecondsEntry(bad), null, JSON.stringify(bad) + ' is refused');
  }
});

test('Save sends both ui.* keys through the same PUT as the tagline and logo', async () => {
  const { sb } = await renderPanel(
    { 'PUT /api/config': { body: {} } },
    { splashMinSeconds: '5', splashMinSecondsNewVersion: '10' });
  sb.saveBranding();
  await flushAsync();
  assert.equal(PUTS(sb).length, 1, 'exactly one settings PUT');
  assert.deepEqual(PUTS(sb)[0].body, {
    'session.splash_tagline': 'AI-powered dev assistant',
    'session.splash_logo_path': '/opt/splash/logo.png',
    'ui.splash_min_seconds': 5,
    'ui.splash_min_seconds_new_version': 10,
  });
});

test('the boundaries save: 60 minimum dwell, 0 new-version dwell (legacy)', async () => {
  const { sb } = await renderPanel(
    { 'PUT /api/config': { body: {} } },
    { splashMinSeconds: '60', splashMinSecondsNewVersion: '0' });
  sb.saveBranding();
  await flushAsync();
  assert.deepEqual(PUTS(sb)[0].body, {
    'session.splash_tagline': 'AI-powered dev assistant',
    'session.splash_logo_path': '/opt/splash/logo.png',
    'ui.splash_min_seconds': 60,
    'ui.splash_min_seconds_new_version': 0,
  });
});

test('an out-of-range or non-whole entry is not saved and shows the error toast', async () => {
  for (const [which, bad] of [
    ['splashMinSeconds', '61'],
    ['splashMinSeconds', '-1'],
    ['splashMinSeconds', ''],
    ['splashMinSeconds', '2.5'],
    ['splashMinSecondsNewVersion', '61'],
    ['splashMinSecondsNewVersion', '-3'],
    ['splashMinSecondsNewVersion', '1.5'],
  ]) {
    const { sb } = await renderPanel(null, { [which]: bad });
    sb.saveBranding();
    await flushAsync();
    assert.equal(PUTS(sb).length, 0, JSON.stringify(bad) + ' in ' + which + ' sends nothing');
    assert.equal(sb.__toasts.length, 1, which + ' ' + JSON.stringify(bad) + ' is reported');
    assert.deepEqual(sb.__toasts[0], { message: 'Enter a whole number of seconds from 0 to 60', type: 'error' });
  }
});

test('all five locale bundles carry the four keys and non-English bundles differ from English', async () => {
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  for (const key of SPLASH_SETTING_KEYS) {
    assert.ok(en[key], 'en has ' + key);
  }
  for (const lang of LOCATIONS.filter(l => l !== 'en')) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    for (const key of SPLASH_SETTING_KEYS) {
      assert.notEqual(bundle[key], undefined, lang + ' has ' + key);
      assert.notEqual(bundle[key], en[key], lang + ' ' + key + ' is not an English copy');
    }
  }
});

test('the new settings strings carry no version or ticket identifiers', async () => {
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  const identifiers = /V\d+\.\d+[0-9.]*|[A-Z]{2,3}-?\d{3,}|\bBL\d+\b/;
  for (const key of SPLASH_SETTING_KEYS) {
    assert.ok(!identifiers.test(en[key]), key + ': no tracker ids');
  }
});
