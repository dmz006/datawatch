'use strict';
// The About card (logo, language, theme, version, update, daemon) is the first
// section of Settings → About; the API, config viewer and other sections follow.
const test = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');

const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');

test('the About card is the first section of the About tab', () => {
  const marker = 'data-group="about"';
  const sections = [];
  let from = 0;
  for (;;) {
    const i = src.indexOf(marker, from);
    if (i < 0) break;
    sections.push(i);
    from = i + marker.length;
  }
  assert.ok(sections.length >= 2, 'expected several About-tab sections');
  const firstBody = src.slice(sections[0], sections[0] + 300);
  assert.ok(firstBody.includes("settings_about_title"), 'the first About-tab section must be the About card');
  sections.slice(1).forEach((s) => {
    assert.ok(!src.slice(s, s + 300).includes('settings_about_title'), 'the About card must not appear again later');
  });
});
