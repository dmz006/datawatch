'use strict';
// The review card labels the judge outcome; every locale carries the same keys.
const test = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');

const P = 'automation.review.judgeOutcome.';
const KEYS = [
  'label', 'couldNotStart', 'timedOut', 'foundProblem',
  'next.couldNotStart', 'next.timedOut', 'next.foundProblem',
].map((k) => P + k);

for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
  test(`${lang}.json has the judge outcome keys`, () => {
    const loc = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    for (const k of KEYS) {
      assert.ok(typeof loc[k] === 'string' && loc[k].trim() !== '', `${lang}.json missing ${k}`);
    }
  });
}

test('the review card uses the keys', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  assert.ok(src.includes('judge_outcome_text'));
  assert.ok(src.includes(P + 'label'));
  assert.ok(src.includes(P + 'next.'));
});
