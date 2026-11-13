// Projects tab locale guard.
//
// Every string the Projects tab shows lives under a projects_* key (plus the
// settings_projects_defaults_* keys for the project-defaults settings card).
// This test keeps those keys in step across the five locale bundles: same
// projects_ key set, real translations, matching positional placeholders, and
// every projects_ key the app asks for by literal name present in English.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');

const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const TRANSLATED = LANGS.filter(l => l !== 'en');

// Values that are the same word in the target language, so identical to
// English on purpose. Anything else identical to English is a missing
// translation.
const SAME_IN_LANGUAGE = {
  de: ['projects_subtab_gantt', 'projects_page_repository', 'projects_manage_col_name', 'projects_manage_field_name'],
  es: ['projects_subtab_gantt'],
  fr: ['projects_subtab_gantt', 'projects_subtab_plans', 'projects_subtab_arcs', 'projects_page_plans', 'projects_page_arcs', 'projects_field_description', 'projects_page_points', 'projects_manage_col_description', 'projects_manage_field_description'],
  ja: [],
};

// Quoted names in app.js that are element or settings-section ids, not
// translation keys. Listed here so the reference check does not flag them.
const NOT_TRANSLATION_KEYS = ['projects_defaults'];

const bundles = {};
for (const lang of LANGS) {
  bundles[lang] = JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
}

function projectKeys(lang) {
  return Object.keys(bundles[lang]).filter(k => k.startsWith('projects_')).sort();
}

function positionals(value) {
  return [...value.matchAll(/%(\d+)\$[sd]/g)].map(m => m[1]).sort().join(',');
}

test('every locale holds the same projects_ key set as English', () => {
  const en = projectKeys('en');
  assert.ok(en.length > 0, 'English has projects_ keys');
  for (const lang of TRANSLATED) {
    assert.deepEqual(projectKeys(lang), en, `${lang}.json projects_ keys differ from en.json`);
  }
});

test('every projects_ key has a non-empty string value in every locale', () => {
  for (const lang of LANGS) {
    for (const key of projectKeys(lang)) {
      const value = bundles[lang][key];
      assert.equal(typeof value, 'string', `${lang}:${key} is not a string`);
      assert.ok(value.trim().length > 0, `${lang}:${key} is empty`);
    }
  }
});

test('positional placeholders match English in every translation', () => {
  for (const key of projectKeys('en')) {
    const want = positionals(bundles.en[key]);
    for (const lang of TRANSLATED) {
      assert.equal(positionals(bundles[lang][key]), want,
        `${lang}:${key} placeholders differ from English (want %${want.split(',').join(', %')})`);
    }
  }
});

test('no English-only placeholder syntax survives in projects_ values', () => {
  for (const lang of LANGS) {
    for (const key of projectKeys(lang)) {
      assert.doesNotMatch(bundles[lang][key], /\{\d+\}/, `${lang}:${key} uses {n} placeholder; t() needs %n$s`);
    }
  }
});

test('projects_ values are translated, not copied from English', () => {
  for (const lang of TRANSLATED) {
    const allowed = new Set(SAME_IN_LANGUAGE[lang]);
    for (const key of projectKeys(lang)) {
      if (allowed.has(key)) continue;
      if (bundles[lang][key] === bundles.en[key] && !/^[^A-Za-z]*$/.test(bundles.en[key])) {
        assert.fail(`${lang}:${key} is still the English text: "${bundles.en[key]}"`);
      }
    }
  }
});

test('app.js references only projects_ keys that exist in en.json', () => {
  const app = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  const en = bundles.en;
  const referenced = new Set();
  // Quoted literals only: t('projects_x'), t("projects_x"), `projects_x` and
  // map entries such as ['projects_x', ...]. Dotted config paths such as
  // 'projects_defaults.branch_retention_days' are not translation keys.
  for (const m of app.matchAll(/(['"`])(projects_[a-z0-9_]+)\1/g)) referenced.add(m[2]);
  assert.ok(referenced.size > 0, 'app.js references projects_ keys');
  const missing = [...referenced].filter(k => !(k in en) && !NOT_TRANSLATION_KEYS.includes(k)).sort();
  assert.deepEqual(missing, [], `app.js uses projects_ keys missing from en.json: ${missing.join(', ')}`);
});

// The Projects sub-tab strip: seven tab labels plus the strip's aria-label.
const SUBTAB_KEYS = [
  'projects_subtab_overview', 'projects_subtab_plans', 'projects_subtab_gantt',
  'projects_subtab_unassigned', 'projects_subtab_history', 'projects_subtab_policy',
  'projects_subtab_events', 'projects_subtabs_label',
];

test('the eight Projects sub-tab keys exist and are non-empty in all five locales', () => {
  assert.equal(SUBTAB_KEYS.length, 8);
  for (const lang of LANGS) {
    for (const key of SUBTAB_KEYS) {
      assert.ok(Object.prototype.hasOwnProperty.call(bundles[lang], key), `${lang}.json is missing ${key}`);
      assert.equal(typeof bundles[lang][key], 'string', `${lang}.json ${key} is not a string`);
      assert.ok(bundles[lang][key].trim().length > 0, `${lang}.json ${key} is empty`);
    }
  }
});
