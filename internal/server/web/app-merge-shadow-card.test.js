// Merge shadow card — PWA side tests.
//
// window.mergeShadowCardHtml(view) renders the matched and different counts per
// release size, summed over projects. It renders nothing when the shadow is
// disabled or when there are zero comparisons. The title and count labels
// live in all five locale bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const CARD_JS = path.join(__dirname, 'app-merge-shadow-card.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const KEYS = [
  'merge_shadow_card_title', 'merge_shadow_matched', 'merge_shadow_different',
  'merge_shadow_size_patch', 'merge_shadow_size_minor', 'merge_shadow_size_major',
  'merge_shadow_size_none', 'merge_shadow_failed',
];

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
}

function loadCard() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  loadScript(CARD_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  return sandbox;
}

function render(view) {
  const sandbox = loadCard();
  sandbox._view = view;
  return vm.runInContext('window.mergeShadowCardHtml(_view)', sandbox);
}

function counts(over) {
  const base = {
    patch: { matched: 0, different: 0 },
    minor: { matched: 0, different: 0 },
    major: { matched: 0, different: 0 },
    none: { matched: 0, different: 0 },
  };
  return Object.assign(base, over);
}

test('hidden with zero comparisons: enabled, every count zero renders nothing', () => {
  assert.equal(render({ enabled: true, projects: [] }), '');
  assert.equal(render({ enabled: true, projects: [{ project: 'p', counts: counts({}) }] }), '');
});

test('hidden when the shadow is disabled, even with comparisons', () => {
  const projects = [{ project: 'p', counts: counts({ patch: { matched: 3, different: 1 } }) }];
  assert.equal(render({ enabled: false, projects }), '');
  assert.equal(render({ projects }), '', 'a missing enabled flag is not enabled');
  assert.equal(render(null), '');
});

test('visible: counts per release size are summed over projects', () => {
  const html = render({
    enabled: true,
    projects: [
      { project: 'alpha', counts: counts({ patch: { matched: 2, different: 1 }, minor: { matched: 0, different: 4 } }) },
      { project: 'beta', counts: counts({ patch: { matched: 5, different: 0 }, major: { matched: 1, different: 0 } }) },
    ],
  });
  assert.ok(html.includes('merge-shadow-card'), 'card rendered');
  assert.ok(html.includes(bundle('en').merge_shadow_card_title), 'title shown');
  const row = (size) => html.match(new RegExp('<tr class="merge-shadow-row merge-shadow-row-' + size + '">[\\s\\S]*?</tr>'))[0];
  const cells = (r) => Array.from(r.matchAll(/<td class="merge-shadow-(?:matched|different)"[^>]*>([^<]*)<\/td>/g)).map(m => m[1]);
  assert.deepEqual(cells(row('patch')), ['7', '1'], 'patch sums both projects');
  assert.deepEqual(cells(row('minor')), ['0', '4'], 'minor');
  assert.deepEqual(cells(row('major')), ['1', '0'], 'major');
  assert.deepEqual(cells(row('none')), ['0', '0'], 'none row shown with zeros');
});

test('visible with one comparison in one size: the other sizes still show', () => {
  const html = render({ enabled: true, projects: [{ project: 'p', counts: counts({ none: { matched: 1, different: 0 } }) }] });
  assert.ok(html.includes('merge-shadow-row-none'));
  assert.ok(html.includes(bundle('en').merge_shadow_size_patch));
});

test('every value is escaped', () => {
  const html = render({
    enabled: true,
    projects: [{ project: '<img src=x onerror=alert(1)>', counts: counts({ patch: { matched: 1, different: 0 } }) }],
  });
  assert.ok(!html.includes('<img src=x'), 'no raw markup from project names');
});

test('locale bundles carry the title and count labels in all five languages', () => {
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of KEYS) {
      assert.equal(typeof b[k], 'string', lang + ' has ' + k);
      assert.ok(b[k].length > 0, lang + ' ' + k + ' is not empty');
    }
  }
});
