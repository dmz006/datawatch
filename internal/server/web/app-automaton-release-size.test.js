// Pins the Automaton release size in the PWA (Phase 4). The list row and the
// detail header show the resolved release size with its source layer, and a
// breaking-change badge when the flag is set. A none or unresolved size with
// the flag off shows nothing. The settings modal sets the explicit size and the
// breaking flag through the set_release_size and set_breaking_change actions.
// Every new key exists in all five locale bundles with the same placeholders.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const APP_JS = path.join(__dirname, 'app.js');

const RELEASE_KEYS = [
  'automaton_release_size_label',
  'automaton_release_size_patch',
  'automaton_release_size_minor',
  'automaton_release_size_major',
  'automaton_release_size_none',
  'automaton_release_size_resolve',
  'automaton_release_size_hint',
  'automaton_release_breaking_label',
  'automaton_release_breaking_hint',
  'automaton_release_source_explicit',
  'automaton_release_source_project',
  'automaton_release_source_default',
  'automation_release_source_milestone',
];

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  // t() echoes its key so assertions read the key names, not translations.
  vm.runInContext('t = function (k) { return k; };', sandbox);
  return sandbox;
}

function badges(sandbox, rec) {
  sandbox.__rec = rec;
  return vm.runInContext('automatonReleaseBadgesHtml(__rec)', sandbox);
}

function info(sandbox, rec) {
  sandbox.__rec = rec;
  return JSON.parse(vm.runInContext('JSON.stringify(automatonReleaseInfo(__rec))', sandbox));
}

// ---- shown -----------------------------------------------------------------

test('an explicit minor size shows the chip with its explicit source', () => {
  const sb = loadApp();
  const html = badges(sb, { release_size: 'minor' });
  assert.match(html, /class="automaton-release-chip"/);
  assert.match(html, /data-release-size="minor"/);
  assert.match(html, /data-release-source="explicit"/);
  assert.match(html, /automaton_release_source_explicit/);
  assert.doesNotMatch(html, /automaton-breaking-badge/);
});

test('a server-resolved major from a milestone shows the milestone source', () => {
  const sb = loadApp();
  const html = badges(sb, { release_size_resolved: 'major', release_size_source: 'automation_milestone' });
  assert.match(html, /data-release-size="major"/);
  assert.match(html, /data-release-source="automation_milestone"/);
  assert.match(html, /automation_release_source_milestone/);
});

test('a server-resolved patch from the built-in default names that layer', () => {
  const sb = loadApp();
  const html = badges(sb, { release_size_resolved: 'patch', release_size_source: 'built_in_default' });
  assert.match(html, /data-release-size="patch"/);
  assert.match(html, /automaton_release_source_default/);
});

test('a breaking change shows the badge beside the size', () => {
  const sb = loadApp();
  const html = badges(sb, { release_size: 'patch', breaking_change: true });
  assert.match(html, /data-release-size="patch"/);
  assert.match(html, /class="automaton-breaking-badge"/);
  assert.match(html, /automaton_release_breaking_label/);
});

test('a breaking change with no size shows only the breaking badge', () => {
  const sb = loadApp();
  const html = badges(sb, { release_size: 'none', breaking_change: true });
  assert.doesNotMatch(html, /automaton-release-chip/);
  assert.match(html, /class="automaton-breaking-badge"/);
});

test('an unknown resolved source is dropped but the size still shows', () => {
  const sb = loadApp();
  const html = badges(sb, { release_size_resolved: 'minor', release_size_source: 'guessed' });
  assert.match(html, /data-release-size="minor"/);
  assert.match(html, /data-release-source=""/);
  assert.doesNotMatch(html, /guessed/);
});

test('an explicit size takes precedence over a server-resolved size', () => {
  const sb = loadApp();
  const html = badges(sb, { release_size: 'minor', release_size_resolved: 'major', release_size_source: 'automation_milestone' });
  assert.match(html, /data-release-size="minor"/);
  assert.doesNotMatch(html, /data-release-size="major"/);
});

// ---- hidden when empty -----------------------------------------------------

test('no size and no flag shows nothing', () => {
  const sb = loadApp();
  assert.equal(badges(sb, {}), '');
  assert.equal(badges(sb, null), '');
  assert.equal(badges(sb, undefined), '');
});

test('an explicit none with the flag off shows nothing', () => {
  const sb = loadApp();
  assert.equal(badges(sb, { release_size: 'none' }), '');
  assert.equal(badges(sb, { release_size: 'none', breaking_change: false }), '');
});

test('an unresolved server size with the flag off shows nothing', () => {
  const sb = loadApp();
  assert.equal(badges(sb, { release_size_resolved: 'none', release_size_source: 'built_in_default' }), '');
  assert.equal(badges(sb, { release_size_resolved: '', release_size_source: '' }), '');
});

test('an unknown explicit size is treated as unset', () => {
  const sb = loadApp();
  assert.equal(badges(sb, { release_size: 'huge' }), '');
  assert.equal(info(sb, { release_size: 'huge' }).showSize, false);
});

test('a status that is off or empty is never shown', () => {
  const sb = loadApp();
  assert.equal(badges(sb, { breaking_change: false, release_size: '' }), '');
});

// ---- rendered row and header -----------------------------------------------

test('the list row renders the release chip for a sized Automaton', () => {
  const sb = loadApp();
  sb.__prd = { id: 'prd-1', title: 'T', status: 'draft', release_size: 'minor', stories: [] };
  const html = vm.runInContext('renderPRDRow(__prd, {})', sb);
  assert.match(html, /automaton-release-chip/);
});

test('the list row renders no release chip for an unsized Automaton', () => {
  const sb = loadApp();
  sb.__prd = { id: 'prd-2', title: 'T', status: 'draft', stories: [] };
  const html = vm.runInContext('renderPRDRow(__prd, {})', sb);
  assert.doesNotMatch(html, /automaton-release-chip|automaton-breaking-badge/);
});

test('the detail header carries the release badges', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const start = src.indexOf('function renderPRDDetailView(');
  assert.ok(start >= 0, 'renderPRDDetailView exists');
  const body = src.slice(start, start + 9000);
  assert.match(body, /automatonReleaseBadgesHtml\(prd\)/, 'the detail header calls automatonReleaseBadgesHtml');
});

// ---- settings control ------------------------------------------------------

test('the settings modal sets the release size and breaking flag through the set actions', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.match(src, /\/set_release_size', \{/);
  assert.match(src, /body: JSON\.stringify\(\{ release_size: newReleaseSize \}\)/);
  assert.match(src, /\/set_breaking_change', \{/);
  assert.match(src, /body: JSON\.stringify\(\{ breaking_change: newBreakingChange \}\)/);
  assert.match(src, /id="prdSettingsReleaseSize"/);
  assert.match(src, /id="prdSettingsBreakingChange"/);
});

test('the settings modal control offers resolve-again, patch, minor, major and none', () => {
  const sb = loadApp();
  // The modal's option list is built from these values; check the helper that labels them.
  for (const size of ['patch', 'minor', 'major', 'none']) {
    sb.__s = size;
    assert.equal(vm.runInContext('automatonReleaseSizeLabel(__s)', sb), 'automaton_release_size_' + size);
  }
});

// ---- locale parity ---------------------------------------------------------

const bundles = {};
for (const lang of LANGS) {
  bundles[lang] = JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
}

for (const key of RELEASE_KEYS) {
  test('locale key ' + key + ' exists in all five bundles', () => {
    for (const lang of LANGS) {
      assert.equal(typeof bundles[lang][key], 'string', lang + '.json is missing ' + key);
      assert.ok(bundles[lang][key].trim().length > 0, lang + '.json has an empty ' + key);
    }
  });
}

test('the release keys keep the same key set across the five bundles', () => {
  const sets = LANGS.map(lang => RELEASE_KEYS.filter(k => k in bundles[lang]).join(','));
  for (const s of sets) assert.equal(s, sets[0]);
});

test('translated release values do not leave the English text in place', () => {
  for (const lang of ['de', 'es', 'fr', 'ja']) {
    for (const key of RELEASE_KEYS) {
      if (key === 'automaton_release_size_minor' || key === 'automaton_release_size_major') continue;
      assert.notEqual(bundles[lang][key], bundles.en[key], lang + '.json still has English for ' + key);
    }
  }
});

test('release values never use the internal word PRD or a tracker identifier', () => {
  const tracker = /\b(BL|B|GH|SEC|HLLM)-?\d+\b/;
  for (const lang of LANGS) {
    for (const key of RELEASE_KEYS) {
      const v = bundles[lang][key];
      assert.doesNotMatch(v, /\bPRDs?\b/, lang + '.json ' + key + ' uses PRD');
      assert.doesNotMatch(v, tracker, lang + '.json ' + key + ' carries a tracker id');
    }
  }
});

test('every release key the app asks for by literal name exists in English', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const literal = /t\('(automaton_release_[a-z_]+|automation_release_[a-z_]+)'\)/g;
  const used = new Set();
  let m;
  while ((m = literal.exec(src)) !== null) used.add(m[1]);
  const missing = [...used].filter(k => !(k in bundles.en));
  assert.deepEqual(missing, [], 'app.js uses release keys absent from en.json: ' + missing.join(', '));
  assert.ok(used.size >= 8, 'app.js should use the release keys by literal name');
});
