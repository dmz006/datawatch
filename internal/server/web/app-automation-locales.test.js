// Automation locale guard.
//
// Every Automation string the PWA shows lives under nav_automation or an
// automation_* key. This test keeps those keys in step across the five locale
// bundles: same key set, real translations, matching positional placeholders,
// no internal vocabulary in user-facing text, and every key the app asks for
// by literal name present in English.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');

const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const TRANSLATED = LANGS.filter(l => l !== 'en');

// Keys allowed to stay English in the non-English bundles. Remove each entry
// from this list once its real translation has landed in de, es, fr and ja.
const PENDING_TRANSLATION = ['automation_subtab_overview', 'automation_subtab_chains'];

const bundles = {};
for (const lang of LANGS) {
  bundles[lang] = JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
}

function isAutomationKey(key) {
  return key === 'nav_automation' || key.startsWith('automation_');
}

function automationEntries(lang) {
  const out = {};
  for (const [key, value] of Object.entries(bundles[lang])) {
    if (isAutomationKey(key)) out[key] = value;
  }
  return out;
}

const entries = {};
for (const lang of LANGS) entries[lang] = automationEntries(lang);

function placeholderSet(value) {
  return [...new Set(String(value).match(/%\d+\$[sd]/g) || [])].sort();
}

test('English bundle carries at least 40 Automation keys', () => {
  assert.ok(
    Object.keys(entries.en).length >= 40,
    'expected >= 40 Automation keys in en.json, found ' + Object.keys(entries.en).length
  );
});

for (const lang of TRANSLATED) {
  test(lang + ' has the same Automation key set as English', () => {
    const enKeys = Object.keys(entries.en);
    const langKeys = Object.keys(entries[lang]);
    const missing = enKeys.filter(k => !(k in entries[lang]));
    const extra = langKeys.filter(k => !(k in entries.en));
    assert.deepEqual(missing, [], lang + '.json is missing Automation keys: ' + missing.join(', '));
    assert.deepEqual(extra, [], lang + '.json has extra Automation keys: ' + extra.join(', '));
  });
}

for (const lang of LANGS) {
  test(lang + ' Automation values are non-empty strings', () => {
    const bad = Object.entries(entries[lang])
      .filter(([, v]) => typeof v !== 'string' || v.trim() === '')
      .map(([k]) => k);
    assert.deepEqual(bad, [], lang + '.json has empty or non-string Automation values: ' + bad.join(', '));
  });
}

for (const lang of TRANSLATED) {
  test(lang + ' Automation values keep the English positional placeholders', () => {
    const mismatched = [];
    for (const [key, enValue] of Object.entries(entries.en)) {
      if (!(key in entries[lang])) continue;
      const want = placeholderSet(enValue);
      const got = placeholderSet(entries[lang][key]);
      if (want.join(',') !== got.join(',')) {
        mismatched.push(key + ' (want [' + want.join(' ') + '], got [' + got.join(' ') + '])');
      }
    }
    assert.deepEqual(mismatched, [], lang + '.json placeholder mismatch: ' + mismatched.join('; '));
  });

  test(lang + ' has translated at least 80 percent of the Automation values', () => {
    const keys = Object.keys(entries.en).filter(k => k in entries[lang] && !PENDING_TRANSLATION.includes(k));
    const differing = keys.filter(k => entries[lang][k] !== entries.en[k]);
    const share = keys.length ? differing.length / keys.length : 0;
    const same = keys.filter(k => entries[lang][k] === entries.en[k]);
    assert.ok(
      share >= 0.8,
      lang + '.json translates ' + Math.round(share * 100) + '% of the Automation values; still English: ' + same.join(', ')
    );
  });
}

test('the pending-translation sub-tab keys exist in every bundle and are correct in English', () => {
  const want = { automation_subtab_overview: 'Overview', automation_subtab_chains: 'Chains' };
  for (const lang of LANGS) {
    for (const key of Object.keys(want)) {
      const value = bundles[lang][key];
      assert.equal(typeof value, 'string', lang + '.json is missing string key ' + key);
      assert.ok(value.trim() !== '', lang + '.json has an empty value for ' + key);
    }
  }
  for (const [key, value] of Object.entries(want)) {
    assert.equal(bundles.en[key], value, 'en.json ' + key + ' must be exactly ' + JSON.stringify(value));
  }
});

const INTERNAL_PRD_WORD = /\bPRDs?\b/;
const TRACKER_ID = /\b(?:BL|GH)\d+/;

for (const lang of LANGS) {
  test(lang + ' Automation values avoid internal vocabulary and tracker identifiers', () => {
    const prd = [];
    const tracker = [];
    for (const [key, value] of Object.entries(entries[lang])) {
      if (INTERNAL_PRD_WORD.test(value)) prd.push(key);
      if (TRACKER_ID.test(value)) tracker.push(key);
    }
    assert.deepEqual(prd, [], lang + '.json uses the word PRD in: ' + prd.join(', '));
    assert.deepEqual(tracker, [], lang + '.json contains a tracker identifier in: ' + tracker.join(', '));
  });
}

test('every literal Automation key used with t() in app.js exists in en.json', () => {
  const source = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  const callPattern = /\bt\(\s*(['"])((?:nav_automation|automation_)[A-Za-z0-9_]*)\1(?!\s*\+)/g;
  const used = new Set();
  let match;
  while ((match = callPattern.exec(source)) !== null) used.add(match[2]);
  const missing = [...used].filter(k => !(k in bundles.en)).sort();
  assert.deepEqual(missing, [], 'app.js uses Automation keys absent from en.json: ' + missing.join(', '));
});

test('every Automation and member status has a status key', () => {
  const automationStatuses = ['draft', 'active', 'paused', 'completed', 'blocked', 'cancelled'];
  const memberStatuses = ['pending', 'approved', 'running', 'blocked', 'completed', 'failed', 'skipped', 'cancelled'];
  const wanted = [
    ...automationStatuses.map(s => 'automation_status_' + s),
    ...memberStatuses.map(s => 'automation_member_status_' + s),
  ];
  for (const lang of LANGS) {
    const missing = wanted.filter(k => !(k in bundles[lang]));
    assert.deepEqual(missing, [], lang + '.json is missing status keys: ' + missing.join(', '));
  }
});
