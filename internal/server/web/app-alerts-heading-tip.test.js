'use strict';
// The Alerts heading's hover tip is a translated string, not literal English.
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');

test('the Alerts heading tooltip reads the alerts_heading_tip locale key', () => {
  assert.ok(src.includes("t('alerts_heading_tip')"), 'heading title uses the locale key');
  assert.ok(!src.includes("title=\"Alerts in this tab (matches the 'all' chip)\""), 'no hard-coded English title');
});

test('alerts_heading_tip exists in all five locales and is translated', () => {
  const seen = new Set();
  for (const l of ['en', 'de', 'es', 'fr', 'ja']) {
    const j = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));
    assert.ok(typeof j.alerts_heading_tip === 'string' && j.alerts_heading_tip.length > 0, l + ' has the key');
    seen.add(j.alerts_heading_tip);
  }
  assert.equal(seen.size, 5, 'each locale carries its own wording');
});
