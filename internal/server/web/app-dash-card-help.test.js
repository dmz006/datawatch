// Dashboard cards — one-line purpose and '?' help entry.
//
// Every card in DASH_CARD_DEFS (the console cards are part of it) needs a
// purpose line under its title and a help anchor into
// docs/howto/dashboard.md. This test loads the real app.js and checks each
// card id has a purpose key, a help key and an anchor, that all five locales
// carry the keys, that the rendered card shows the purpose and the '?' link,
// and that the operator-facing text names no tracker id and never says PRD.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { buildSandbox, loadScript, vm } = require('./testutil_browser_stub');

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const bundles = {};
for (const loc of LOCALES) {
  bundles[loc] = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', loc + '.json'), 'utf8'));
}

function loadApp() {
  const sandbox = buildSandbox({ byId: {} });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  // Resolve text from the English bundle like the running page does.
  vm.runInContext('t = function (k) { return globalThis.__en[k] || k; };', Object.assign(sandbox, { __en: bundles.en }));
  return sandbox;
}

const sb = loadApp();
const ids = vm.runInContext('DASH_CARD_DEFS.map(d => d.id)', sb);
const anchors = vm.runInContext('DASH_CARD_HELP_ANCHORS', sb);

test('there are cards to check', () => {
  assert.ok(ids.length >= 25, 'all dashboard cards are listed: ' + ids.length);
});

test('every card id has a purpose key, a help key and a help anchor', () => {
  for (const id of ids) {
    const purposeKey = vm.runInContext(`_dashCardPurposeKey(${JSON.stringify(id)})`, sb);
    const helpKey = vm.runInContext(`_dashCardHelpKey(${JSON.stringify(id)})`, sb);
    assert.match(purposeKey, /^dash_(card_purpose|network_purpose)/, id);
    assert.match(helpKey, /^dash_card_help_/, id);
    assert.match(anchors[id] || '', /^[a-z0-9]+(-[a-z0-9]+)*$/, 'anchor for ' + id);
    for (const loc of LOCALES) {
      assert.ok(bundles[loc][purposeKey], `${loc} purpose ${purposeKey}`);
      assert.ok(bundles[loc][helpKey], `${loc} help ${helpKey}`);
    }
  }
  for (const loc of LOCALES) assert.ok(bundles[loc].dash_card_help_label, loc + ' help label');
});

test('anchors are unique and name only cards that exist', () => {
  const vals = Object.values(anchors);
  assert.equal(new Set(vals).size, vals.length, 'unique anchors');
  for (const id of Object.keys(anchors)) assert.ok(ids.includes(id), 'known card ' + id);
});

test('a rendered card shows its purpose and a ? link to the dashboard howto anchor', () => {
  for (const id of ids) {
    const html = vm.runInContext(`_dashCardHTML({ id: ${JSON.stringify(id)} })`, sb);
    assert.ok(html.includes(`/diagrams.html#docs/howto/dashboard.md#${anchors[id]}"`), 'help link for ' + id);
    assert.ok(/class="docs-link"[^>]*>\?<\/a>/.test(html), '? entry for ' + id);
    if (id === 'orbital') continue; // its purpose line is drawn in the card body
    const purpose = bundles.en[vm.runInContext(`_dashCardPurposeKey(${JSON.stringify(id)})`, sb)];
    const escaped = sb.escHtml(purpose);
    assert.ok(html.includes(escaped), 'purpose text for ' + id);
  }
});

test('the Network card keeps one purpose line, in its body', () => {
  const html = vm.runInContext(`_dashCardHTML({ id: 'orbital' })`, sb);
  assert.ok(!html.includes('dboard-card-purpose'), 'no second purpose line');
  const body = vm.runInContext(`DASH_CARD_DEFS.find(d => d.id === 'orbital').body()`, sb);
  assert.ok(body.includes('Work in progress: projects, Automations'), 'body purpose');
});

test('purpose and help text keep to the project wording rules', () => {
  for (const loc of LOCALES) {
    for (const [k, v] of Object.entries(bundles[loc])) {
      if (!/^dash_card_(purpose|help)_/.test(k)) continue;
      assert.ok(!/\bPRDs?\b/.test(v), `${loc} ${k} says PRD`);
      assert.ok(!/\b(BL|GH|B)\d{2,}\b/.test(v), `${loc} ${k} has a tracker id`);
      assert.ok(!/\n/.test(v), `${loc} ${k} is one line`);
    }
  }
});

test('a card that is off stays hidden and says why in one line', () => {
  const feats = vm.runInContext('DASH_CARD_FEATURES', sb);
  for (const id of ['lane-utilization', 'throughput-forecast', 'verifier-health', 'release-train', 'review-backlog', 'defects-per-day']) {
    assert.ok(feats[id] && typeof feats[id].active === 'function', 'active() for ' + id);
    assert.ok(bundles.en[feats[id].reasonKey], 'reason for ' + id);
  }
});
