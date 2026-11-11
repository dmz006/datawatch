// Dashboard cards — how-to sections match the help anchors.
//
// Every card's '?' link opens docs/howto/dashboard.md at an anchor. This test
// loads the real app.js and checks that each anchor is the slug of a heading
// in the how-to, that the section says what the card is for, where its data
// comes from and what off means, and that the dashboard docs keep to the
// project wording rules (no tracker identifiers, never PRD).

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { buildSandbox, loadScript, vm } = require('./testutil_browser_stub');

const DOCS = path.join(__dirname, '..', '..', '..', 'docs');
const howto = fs.readFileSync(path.join(DOCS, 'howto', 'dashboard.md'), 'utf8');

function slug(h) {
  return h.toLowerCase().replace(/[^a-z0-9 -]/g, '').trim().replace(/ /g, '-');
}

// Sections keyed by heading slug (### level only).
const sections = {};
{
  const parts = howto.split(/^### /m).slice(1);
  for (const p of parts) {
    const nl = p.indexOf('\n');
    sections[slug(p.slice(0, nl))] = p.slice(nl + 1).split(/^## /m)[0];
  }
}

const sandbox = buildSandbox({ byId: {} });
vm.createContext(sandbox);
loadScript(path.join(__dirname, 'app.js'), sandbox);
const anchors = vm.runInContext('DASH_CARD_HELP_ANCHORS', sandbox);
const features = vm.runInContext('DASH_CARD_FEATURES', sandbox);

test('every help anchor is a heading in the dashboard how-to', () => {
  for (const [id, anchor] of Object.entries(anchors)) {
    assert.ok(sections[anchor], `heading for ${id} (#${anchor})`);
  }
});

test('each card section says what it is for, its data and what off means', () => {
  for (const [id, anchor] of Object.entries(anchors)) {
    const body = sections[anchor] || '';
    assert.match(body, /\*\*For:\*\*/, `${id} purpose`);
    assert.match(body, /\*\*Data:\*\*/, `${id} data source`);
    assert.match(body, /\*\*Off:\*\*/, `${id} off meaning`);
  }
});

test('a card with an active() check quotes its one-line reason', () => {
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  for (const [id, feat] of Object.entries(features)) {
    const anchor = anchors[id];
    if (!anchor) continue;
    const reason = en[feat.reasonKey] || feat.reasonDefault;
    assert.ok(sections[anchor].includes(reason), `${id} section quotes: ${reason}`);
  }
});

test('the dashboard docs name no tracker id and never say PRD', () => {
  for (const f of ['howto/dashboard.md', 'howto/dashboard-operator-console.md', 'howto/automation-tab.md', 'datawatch-definitions.md']) {
    const txt = fs.readFileSync(path.join(DOCS, f), 'utf8');
    assert.ok(!/\b(BL|GH)\d{2,}\b/.test(txt), `${f} has a tracker id`);
  }
  assert.ok(!/\bPRDs?\b/.test(howto), 'dashboard.md says PRD');
});
