// Usage page: one verdict sentence above three small cards. The page draws only
// what the advice body gives; a card that is off, empty or never set is not drawn.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');
const { workedExampleBody, emptyBody } = require('./usage-calculator-fixture');

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}
function frame(sandbox, body) {
  sandbox._body = body;
  return vm.runInContext('window.usageCalcFrameHtml(_body, {})', sandbox);
}
const count = (html, re) => (html.match(re) || []).length;

test('one verdict sentence and exactly three cards for the worked example', () => {
  const html = frame(loadApp(), workedExampleBody());
  assert.equal(count(html, /class="usage-verdict-sentence"/g), 1);
  assert.equal(count(html, /class="usage-card usage-card-/g), 3);
  for (const id of ['allowance', 'pace', 'burn']) assert.ok(html.includes(`data-card="${id}"`), id);
  assert.ok(html.indexOf('usage-verdict-sentence') < html.indexOf('usage-cards'), 'sentence is above the cards');
  assert.match(html, /Over budget: at the recent burn/);
});

test('each card states its purpose, state, why and next action', () => {
  const html = frame(loadApp(), workedExampleBody());
  for (const cls of ['usage-card-title', 'usage-card-state', 'usage-card-why', 'usage-card-next']) {
    assert.equal(count(html, new RegExp(`class="${cls}"`, 'g')), 3, cls);
  }
});

test('no variant switcher, reading form, extra credit block or counter toggle', () => {
  const html = frame(loadApp(), workedExampleBody());
  for (const gone of ['usage-calc-switcher', 'usageCalcSetVariant', 'usageCalcPercent', 'usageCalcSubmitReading',
    'usageCalcExtra', 'usageCalcToggleCounter', 'usage_calc_counter']) {
    assert.ok(!html.includes(gone), gone + ' must not be on the page');
  }
  assert.ok(html.includes('navigate(\'settings\')'), 'link to the Settings block');
});

test('explanations sit behind ? popovers that link to the usage calculator howto', () => {
  const html = frame(loadApp(), workedExampleBody());
  const buttons = count(html, /class="usage-help"/g);
  assert.equal(buttons, 4, 'one for the page and one for each card');
  assert.equal(count(html, /class="usage-help-pop"[^>]*hidden/g), buttons, 'popovers start closed');
  assert.equal(count(html, /href="\/diagrams\.html#docs\/howto\/usage-calculator\.md"/g), buttons);
  assert.ok(!html.includes('usage-calc-intro'), 'no loose intro paragraph');
});

test('the popover toggle opens and closes', () => {
  const s = loadApp();
  const pop = { hasAttribute: () => pop.h, removeAttribute: () => { pop.h = false; }, setAttribute: () => { pop.h = true; }, h: true };
  const btn = { nextElementSibling: pop, setAttribute(k, v) { btn[k] = v; } };
  s._btn = btn;
  vm.runInContext('window.usageCalcHelpToggle(_btn)', s);
  assert.equal(pop.h, false);
  assert.equal(btn['aria-expanded'], 'true');
  vm.runInContext('window.usageCalcHelpToggle(_btn)', s);
  assert.equal(pop.h, true);
});

test('card sizes are bounded in style.css with fixed width, height and max-width', () => {
  const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');
  const card = /^\.usage-card\s*\{([^}]*)\}/m.exec(css);
  assert.ok(card, '.usage-card rule');
  assert.match(card[1], /(^|[\s;])width:\s*\d+px/);
  assert.match(card[1], /(^|[\s;])height:\s*\d+px/);
  assert.match(card[1], /max-width:\s*100%/);
  assert.match(css, /\.usage-cards\s*\{[^}]*display:\s*grid[^}]*grid-template-columns:\s*repeat\(auto-fit,\s*minmax\(220px,\s*1fr\)\)/);
  assert.match(css, /@media\s*\(max-width:\s*600px\)\s*\{[^{]*\.usage-cards\s*\{[^}]*grid-template-columns:\s*1fr/);
  assert.match(/\.usage-card-chart\s*\{([^}]*)\}/.exec(css)[1], /max-width:\s*\d+px/);
});

test('a card that is off or empty is not shown', () => {
  const s = loadApp();
  const noDaily = frame(s, workedExampleBody({ advice: { daily: [] } }));
  assert.ok(!noDaily.includes('data-card="burn"'), 'burn card hidden without day totals');
  assert.equal(count(noDaily, /class="usage-card usage-card-/g), 2);
  const noPace = frame(s, workedExampleBody({ advice: { pace_line: null, daily: [] } }));
  assert.ok(!noPace.includes('data-card="pace"'));
  const zeroBurn = frame(s, workedExampleBody({ advice: { recent_burn_per_24h: 0, week_avg_burn_per_24h: 0 } }));
  assert.ok(!zeroBurn.includes('data-card="burn"'));
});

test('with no reading there is no sentence and no cards, only the Settings link', () => {
  const html = frame(loadApp(), emptyBody());
  assert.ok(!html.includes('usage-verdict-sentence'));
  assert.ok(!html.includes('usage-card'));
  assert.ok(html.includes('No readings yet'));
  assert.ok(html.includes('usage-settings-link'));
});

test('the within verdict reads as a within-budget sentence', () => {
  const html = frame(loadApp(), workedExampleBody({ advice: { verdict: 'within', projected_end_recent: 80 } }));
  assert.match(html, /Within budget: you have used 72%/);
});
