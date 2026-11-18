// Regression test: the Alerts tab "?" help link moved out of the alerts
// loader's metaExtra row (the content) and into the shared header help link
// (#headerHelpLink in index.html). The nav function's anchorMap carries an
// 'alerts' entry pointing at the Alerts howto, the link's title uses the
// alerts_help_tip string on the alerts view (the generic index.html title is
// restored on every other view), and _alertsLoader no longer takes a metaExtra
// option — no second "?" remains in the Alerts content.
//
// These are source-level assertions on app.js and style.css.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');

function anchorMapRegion() {
  const m = src.indexOf('const anchorMap = {');
  assert.ok(m >= 0, 'nav anchorMap not found in app.js');
  const end = src.indexOf('renderServerHeaderLine', m);
  assert.ok(end > m, 'nav help-link block not found after the anchorMap');
  return src.slice(m, end);
}

function alertsLoaderRegion() {
  const loader = src.indexOf('function _alertsLoader');
  assert.ok(loader >= 0, '_alertsLoader helper not found');
  const view = src.indexOf('function renderAlertsView', loader);
  assert.ok(view > loader, 'renderAlertsView not found');
  const j = src.indexOf('\n}\n', view);
  assert.ok(j > view, 'renderAlertsView body terminator not found');
  return src.slice(loader, j + 3);
}

test('the header anchorMap carries the alerts view pointing at the Alerts howto', () => {
  const region = anchorMapRegion();
  assert.ok(region.includes("'alerts':"), "nav anchorMap missing the 'alerts' entry");
  assert.ok(region.includes("'/diagrams.html#docs/howto/alerts-and-notifications.md'"),
    'alerts map entry does not point at the Alerts howto');
});

test('the alerts view gets the alerts_help_tip title, other views keep the generic one', () => {
  const region = anchorMapRegion();
  assert.ok(region.includes('helpLink.title'), 'nav no longer sets the help link title per view');
  assert.ok(/view === 'alerts'/.test(region) && region.includes("t('alerts_help_tip')"),
    'alerts view does not use the alerts_help_tip tooltip');
  assert.ok(region.includes("'Open documentation for this view'"),
    'the generic header help title is not restored for the other views');
});

test('the alerts loader no longer ships a help link in the content', () => {
  assert.ok(!src.includes('function _alertsHelpLinkHtml'),
    '_alertsHelpLinkHtml helper still present in app.js');
  assert.ok(!src.includes('_alertsHelpLinkHtml'),
    'app.js still references _alertsHelpLinkHtml');
  const region = alertsLoaderRegion();
  assert.ok(region.includes('listLoader('), '_alertsLoader does not call listLoader');
  assert.ok(!/metaExtra\s*:/.test(region),
    '_alertsLoader still passes a metaExtra option to listLoader');
  assert.ok(region.includes('window._listLoaders'),
    '_alertsLoader does not return the per-key cached loader from window._listLoaders');
  assert.ok(region.includes('_alertsLoader('),
    'renderAlertsView does not build the loader through _alertsLoader');
});

test('style.css no longer carries the dead .alerts-help-link rule', () => {
  const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');
  assert.ok(!css.includes('alerts-help-link'),
    'style.css still references the alerts-help-link class');
});
