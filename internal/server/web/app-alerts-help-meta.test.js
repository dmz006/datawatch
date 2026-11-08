// Regression test: the Alerts tab "?" help link moved out of its own
// top-right flex row and into listLoader's metaExtra option (the link now
// sits on the alerts loader's meta row and gets its coarse-pointer tap
// target from the .alerts-help-link rule in style.css).
//
// These are source-level assertions on app.js: renderAlertsView must no
// longer emit the standalone justify-content:flex-end row, the loader must
// be built with a metaExtra option (through the _alertsLoader helper so the
// per-key cached loader in window._listLoaders carries it too), and the
// link must keep its href/target/rel, aria-label and tooltip key.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');

function blockEnd(fromIdx) {
  const j = src.indexOf('\n}\n', fromIdx);
  assert.ok(j > fromIdx, 'function body terminator not found');
  return j;
}

function alertsHelpRegion() {
  const link = src.indexOf('function _alertsHelpLinkHtml');
  assert.ok(link >= 0, '_alertsHelpLinkHtml helper not found');
  const loader = src.indexOf('function _alertsLoader', link);
  assert.ok(loader > link, '_alertsLoader helper not found');
  const view = src.indexOf('function renderAlertsView', loader);
  assert.ok(view > loader, 'renderAlertsView not found');
  return src.slice(link, blockEnd(view) + 3);
}

function renderAlertsViewSource() {
  const view = src.indexOf('function renderAlertsView');
  assert.ok(view >= 0, 'renderAlertsView not found');
  return src.slice(view, blockEnd(view) + 3);
}

test('renderAlertsView no longer renders the standalone flex-end help row', () => {
  const v = renderAlertsViewSource();
  assert.ok(!v.includes('justify-content:flex-end'),
    'renderAlertsView still emits the justify-content:flex-end help row');
  assert.ok(!v.includes('display:flex;justify-content:flex-end'),
    'renderAlertsView still emits the flex-end wrapper div');
});

test('the alerts loader is built with a metaExtra option via the cached-loader helper', () => {
  const region = alertsHelpRegion();
  const loader = region.slice(region.indexOf('function _alertsLoader'));
  assert.ok(loader.includes('listLoader('), '_alertsLoader does not call listLoader');
  assert.ok(/metaExtra\s*:/.test(loader),
    '_alertsLoader does not pass the metaExtra option to listLoader');
  const v = renderAlertsViewSource();
  assert.ok(v.includes('_alertsLoader('),
    'renderAlertsView does not build the loader through _alertsLoader');
  assert.ok(loader.includes('window._listLoaders'),
    '_alertsLoader does not return the per-key cached loader from window._listLoaders');
});

test('the help link keeps its href, target, rel, aria-label and tooltip key', () => {
  const region = alertsHelpRegion();
  assert.ok(region.includes('href="/diagrams.html#docs/howto/alerts-and-notifications.md"'),
    'help link href changed');
  assert.ok(region.includes('target="_blank"'), 'help link lost target="_blank"');
  assert.ok(region.includes('rel="noopener"'), 'help link lost rel="noopener"');
  assert.ok(region.includes('aria-label="Help"'), 'help link lost aria-label="Help"');
  assert.ok(region.includes("t('alerts_help_tip')"), 'help link lost the alerts_help_tip tooltip key');
  assert.ok(region.includes('alerts-help-link'), 'help link lost the alerts-help-link class');
  assert.ok(/>\?<\/a>/.test(region), 'help link lost the "?" glyph');
});

test('style.css gives .alerts-help-link a 40px coarse-pointer tap target', () => {
  const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');
  const i = css.indexOf('a.alerts-help-link');
  assert.ok(i >= 0, '.alerts-help-link rule missing from style.css');
  const coarseBlock = css.slice(Math.max(0, i - 800), i + 200);
  assert.ok(coarseBlock.includes('pointer: coarse'),
    'a.alerts-help-link rule is not inside a (pointer: coarse) media block');
  assert.ok(/min-height:\s*40px/.test(css.slice(i, i + 300)),
    'a.alerts-help-link rule does not set a 40px min-height tap target');
});
