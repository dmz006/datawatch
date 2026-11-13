'use strict';
/*
 * One-line toolbar rule — applying it to the Alerts, Automata, Observer and
 * Dashboard pages.
 *
 * The Sessions page toolbar introduced the one-line toolbar pattern: a
 * page's server select, its search input (taking the free width) and the
 * page's compact controls sit on one non-wrapping line under the page
 * header (see .sessions-topbar in style.css; the Sessions page builds it
 * in app.js with id="sessionsServerDropdown").
 *
 * The rule was inspected for the four other pages and its fallback applies
 * on each of them: when a page has no search/filter at the page level, or
 * the controls need more room than a phone-width non-wrapping line allows,
 * the page is left unchanged and the reason is recorded in a code comment
 * next to the page's header render:
 *
 *   Alerts    — has the shared header server select and always-visible
 *               search/filter controls, but the five labelled filter chips
 *               + search + sort + icon buttons + server select exceed one
 *               phone-width line → unchanged.
 *   Automata  — search/filter live only in the ⊞-toggled filter bar (a
 *               sheet, its logic unchanged); no always-visible
 *               page-level search/filter line → unchanged.
 *   Observer  — scrolling settings page with no page-level search/filter;
 *               the Memory Browser controls are section-scoped → unchanged.
 *   Dashboard — the stat bar is already one non-wrapping line carrying
 *               the placement filter and icon-sized buttons; no text
 *               search, and no room for a toolbar line → unchanged.
 *
 * This test pins that decision: each page header render carries the
 * reason comment (naming the rule and a page-specific reason), none of
 * the four pages adopts the Sessions topbar pattern or a new server
 * select (so they really are unchanged), each page's existing controls
 * and handlers are still present, and the Sessions one-line rule itself
 * (nowrap topbar + flexing search) is intact in style.css and app.js.
 */

const test = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');

const appSrc = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
const cssSrc = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');

// Extract a top-level function body: from `function NAME(` up to the next
// column-0 `function …(` / `window.… =` declaration (or end of file).
function fnBody(src, name) {
  const marker = `function ${name}(`;
  let start = src.indexOf(`\n${marker}`);
  if (start === -1) start = src.indexOf(marker);
  assert.notStrictEqual(start, -1, `app.js declares function ${name}(`);
  start += 1;
  const rest = src.slice(start + 1);
  const m = rest.match(/\n(?:function [A-Za-z_$][\w$]*\(|window\.[A-Za-z_$][\w$]* =)/);
  const end = m ? start + 1 + m.index : src.length;
  return src.slice(start, end);
}

// Page → which function renders its header, which existing controls/handlers
// must survive, the header render marker the comment must precede, and a
// page-specific word from the recorded reason.
const PAGES = [
  {
    page: 'Alerts',
    fn: '_alertsBuildHtml',
    headerMarker: 'const topBar',
    reasonWord: 'chips',
    keep: ['alertsSearchInput', 'setAlertsFilter', 'setAlertsSort',
           'dismissAlertsAll', 'muteAlertDock', 'alertsRefresh'],
  },
  {
    page: 'Automata',
    fn: 'renderAutonomousView',
    headerMarker: 'view.innerHTML',
    reasonWord: 'filter bar',
    keep: ['automata-tabs-bar', 'automataFilterBtn', 'toggleAutomataFilter',
           'automataFilterBar', 'automataSearch', 'toggleAutomataHistory'],
  },
  {
    page: 'Observer',
    fn: 'renderObserverView',
    headerMarker: 'view.innerHTML',
    reasonWord: 'Memory Browser',
    keep: ['perSystemGrid', 'System Statistics'],
  },
  {
    page: 'Dashboard',
    fn: 'renderDashboardView',
    headerMarker: 'viewEl.innerHTML',
    reasonWord: 'stat bar',
    keep: ['dboard-stat-bar', 'dashPlacementSelect', 'dashPlacementFilter', 'dashTagFilter',
           'dashAddCardBtn', 'dashEditBtn', 'dashStatSessions'],
  },
];

for (const p of PAGES) {
  test(`${p.page}: one-line toolbar rule decision recorded next to the header render`, () => {
    const body = fnBody(appSrc, p.fn);

    // The reason comment names the rule and sits above the header render.
    const commentIdx = body.toLowerCase().indexOf('one-line toolbar rule');
    assert.notStrictEqual(commentIdx, -1, `${p.page}: header render lacks the rule decision comment`);
    assert.match(body.replace(/\/\//g, ' '), /NOT\s+applied/, `${p.page}: decision comment records the leave-unchanged outcome`);
    assert.ok(body.slice(commentIdx, commentIdx + 700).includes(p.reasonWord),
      `${p.page}: decision comment records a page-specific reason`);
    const headerIdx = body.indexOf(p.headerMarker);
    assert.notStrictEqual(headerIdx, -1, `${p.page}: header render (${p.headerMarker}) still present`);
    assert.ok(commentIdx < headerIdx, `${p.page}: comment sits next to (above) the header render`);
  });

  test(`${p.page}: page left unchanged (no Sessions topbar pattern, no new server select)`, () => {
    const body = fnBody(appSrc, p.fn);
    for (const s of ['sessions-topbar', 'ServerDropdown', 'serverDropdown', '_sessionsServerSelectHTML']) {
      assert.ok(!body.includes(s), `${p.page}: no Sessions toolbar pattern/selector in the page render (found ${s})`);
    }
  });

  test(`${p.page}: existing controls, ids and handlers intact`, () => {
    const body = fnBody(appSrc, p.fn);
    for (const id of p.keep) {
      assert.ok(body.includes(id), `${p.page}: existing control/handler "${id}" still present`);
    }
  });
}

test('Sessions topbar rule itself is intact in style.css', () => {
  const top = cssSrc.match(/\.sessions-topbar \{[^}]*\}/);
  assert.ok(top, '.sessions-topbar rule present in style.css');
  assert.match(top[0], /display:\s*flex/, 'topbar is a flex line');
  assert.match(top[0], /flex-wrap:\s*nowrap/, 'topbar never wraps');

  const search = cssSrc.match(/\.sessions-topbar \.sessions-search-row \{[^}]*\}/);
  assert.ok(search, 'topbar search row rule present');
  assert.match(search[0], /flex:\s*1/, 'search takes the free width');
  assert.match(search[0], /min-width:\s*0/, 'search row may shrink, never wrap');

  const select = cssSrc.match(/\.sessions-topbar \.sessions-server-select \{[^}]*\}/);
  assert.ok(select, 'topbar server select rule present');
  assert.match(select[0], /flex:\s*0\s*0\s*auto/, 'server select is icon-sized (fixed)');
  assert.match(select[0], /max-width:\s*40%/, 'server select capped on phones');
});

test('Sessions page still renders the one-line toolbar', () => {
  assert.match(appSrc, /<select[^>]*id="sessionsServerDropdown"[^>]*>/, 'sessions server select present');
  const bar = fnBody(appSrc, 'renderSessionsToolbar');
  assert.match(bar, /<div class="sessions-topbar">/, 'sessions topbar line present');
  const divIdx = bar.indexOf('<div class="sessions-topbar">');
  const line = bar.slice(divIdx);
  const selIdx = line.indexOf('_sessionsServerSelectHTML()');
  const searchIdx = line.indexOf('renderSessionsSearchRow');
  const viewIdx = line.indexOf('_sessionsViewSelectHTML');
  assert.ok(selIdx !== -1 && searchIdx !== -1 && viewIdx !== -1,
    'server select, search and view select all on the line');
  assert.ok(selIdx < searchIdx && searchIdx < viewIdx,
    'control order: server select, then search (free width), then compact controls');
});
