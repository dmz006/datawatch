// Classification verdict display and the Verification mode selector — PWA side.
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const bundle = l => JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, l + '.json'), 'utf8'));

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  return sandbox;
}
function verdict(v) {
  const sb = loadApp();
  sb._v = v;
  return vm.runInContext('classifyVerdictHTML(_v)', sb);
}

test('verdict without route or scores renders nothing', () => {
  assert.equal(verdict({ category: 'content' }), '');
  assert.equal(verdict(null), '');
});

test('verdict shows route and score vector', () => {
  const h = verdict({
    mode: 'classify',
    route: { node: 'gpu1', model: 'qwen3', path: 'native' },
    scores: { pass_probability: 0.91, threshold: 0.8, ok: { yes: 0.91, no: 0.09 } },
  });
  assert.match(h, /gpu1 \/ qwen3/);
  assert.match(h, /native/);
  assert.match(h, /0\.910/);
  assert.match(h, /no=0\.090 yes=0\.910/);
});

test('verdict escapes hostile route values', () => {
  const h = verdict({ route: { node: '<img src=x onerror=alert(1)>', model: 'm' } });
  assert.ok(!h.includes('<img'));
});

test('low-confidence category shows the warning', () => {
  const h = verdict({ category: 'low-confidence', scores: { pass_probability: 0.5, threshold: 0.8 } });
  assert.match(h, /Low confidence/);
});

test('LLM settings form has a verification mode selector', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.match(src, /llm_field_verification_mode/);
  assert.match(src, /\['','generate','classify'\]/);
  assert.match(src, /verification_mode: verificationMode/);
});

test('classif and verification_mode keys exist in every locale', () => {
  const en = Object.keys(bundle('en')).filter(k => /classif|verification_mode/.test(k));
  assert.ok(en.length >= 10);
  for (const l of LANGS) {
    const b = bundle(l);
    for (const k of en) assert.ok(typeof b[k] === 'string' && b[k].trim(), `${l} missing ${k}`);
  }
});
