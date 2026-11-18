// e73d0006 — narrow-screen layout of the Alerts view.
//
// On a phone-width viewport (~320–390 CSS px) the Alerts cards must WRAP,
// never overflow the card horizontally. The app does this with three
// cooperating mechanisms that this test pins down:
//   1. a CSS class contract in style.css: the session header row wraps
//      (flex-wrap: wrap) and every text child is allowed to shrink and
//      break (min-width: 0; max-width: 100%; overflow-wrap: anywhere;
//      word-break: break-word); message bodies additionally get
//      white-space: pre-wrap so long words break too;
//   2. inline overflow-wrap styles on the card rows themselves (so the
//      contract survives even where the classes don't apply — the same
//      way the app styles other long identifiers);
//   3. width-agnostic markup: the rendered HTML is identical at 390 px
//      and 360 px — wrapping is never done by JS measuring the viewport.
//
// The stub document has no layout engine, so the assertions are on the
// markup (classes + inline wrapping styles + full text preserved +
// escaped) and on the style.css rules; the 360 px run proves the app
// does not branch the HTML on width.
// A session id of 128 no-space chars, a long multi-word session name,
// and a 300-char no-space alert message are the fixtures — the cases
// that overflow on narrow screens without the wrap rules.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const {
  vm, buildSandbox, loadScript, makeStubElement,
} = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const STYLE_CSS = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');
const LOCALE = (code) => JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', code + '.json'), 'utf8'));

const NOBREAK_ID = 's' + 'a'.repeat(127);            // 128 chars, zero spaces
const LONG_NAME = 'build-checker' + ' very-long-name'.repeat(10);  // long, with spaces
const LONG_MSG = 'm'.repeat(300);                    // 300 chars, zero spaces
const EVIL = '<img src=x onerror=alert(1)>';

const SESSIONS = [
  { id: NOBREAK_ID, state: 'running' },                          // no name → label IS the 128-char id
  { id: 'sess-long', name: LONG_NAME, state: 'running' },        // long session name
];
const DATA = [
  { id: 'n-1', level: 'error', title: 'step failed', body: LONG_MSG, session_id: NOBREAK_ID, created_at: '2026-01-01T00:00:09Z' },
  { id: 'n-2', level: 'warn', title: EVIL, body: 'needs a reply — ' + 'x'.repeat(80), session_id: 'sess-long', created_at: '2026-01-01T00:00:08Z' },
];

function renderAt(width) {
  const view = makeStubElement();
  const list = makeStubElement();
  const sandbox = buildSandbox({ byId: { view, alertsList: list } });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);   // real top-level code runs to completion
  sandbox._i18n.bundle = LOCALE('en');
  sandbox.innerWidth = width;    // the stub honors viewport hints; the app must ignore them in the markup
  sandbox.__ses = SESSIONS;
  sandbox.__data = DATA;
  vm.runInContext(`state.sessions = __ses; window._alertsTabSel = 'active';`, sandbox);
  return vm.runInContext(`_alertsBuildHtml({ data: __data, cmds: [], hasMore: false, total: 2 })`, sandbox);
}

function renderFR() {
  const sandbox = buildSandbox({ byId: { alertsList: makeStubElement() } });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = LOCALE('fr');
  sandbox.__ses = SESSIONS;
  sandbox.__data = DATA;
  vm.runInContext(`state.sessions = __ses; window._alertsTabSel = 'active';`, sandbox);
  return vm.runInContext(`_alertsBuildHtml({ data: __data, cmds: [], hasMore: false, total: 2 })`, sandbox);
}

const ESC = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

// The CSS rule for a bare selector; the look-behind keeps '.card.alert-card'
// (or similar compound selectors) from shadowing the standalone rule.
function cssRule(selector) {
  const re = new RegExp('(?<![\\w.#\\-])' + ESC(selector) + '\\s*\\{');
  const m = re.exec(STYLE_CSS);
  assert.ok(m, `style.css declares a ${selector} rule`);
  const end = STYLE_CSS.indexOf('}', m.index + m[0].length);
  return STYLE_CSS.slice(m.index, end + 1);
}

// The CSS may split one concept across several same-selector rules (later
// rules extend earlier ones — e.g. .alert-card is styled twice: once for
// card chrome, once more for the narrow-wrap contract). Join all matches.
function cssRules(selector) {
  const re = new RegExp('(?<![\\w.#\\-])' + ESC(selector) + '\\s*\\{', 'g');
  const out = [];
  let m;
  while ((m = re.exec(STYLE_CSS))) {
    const end = STYLE_CSS.indexOf('}', m.index + m[0].length);
    out.push(STYLE_CSS.slice(m.index, end + 1));
  }
  assert.ok(out.length >= 1, `style.css declares a ${selector} rule`);
  return out.join('\n');
}

test('style.css wraps alert text instead of letting it overflow the narrow card', () => {
  const head = cssRule('.alert-session-head');
  assert.ok(/flex-wrap:\s*wrap/.test(head), '.alert-session-head: header row flex-wraps');
  assert.ok(/min-width:\s*0/.test(head), '.alert-session-head: can shrink (min-width: 0)');

  const headChild = cssRule('.alert-session-head > *');
  assert.ok(/min-width:\s*0/.test(headChild), '.alert-session-head > *: children allowed to shrink');
  assert.ok(/max-width:\s*100%/.test(headChild), '.alert-session-head > *: children capped to the row width');

  // Combined selector .alert-session-label, .alert-row-title — find the
  // block that STARTS at that selector list.
  const m = /\.alert-session-label[^{}]*\{/.exec(STYLE_CSS);
  assert.ok(m, 'style.css styles .alert-session-label (with .alert-row-title)');
  const end = STYLE_CSS.indexOf('}', m.index + m[0].length);
  const labelTitle = STYLE_CSS.slice(m.index, end + 1);
  assert.ok(labelTitle.includes('.alert-row-title'), 'the same rule covers .alert-row-title');
  assert.ok(/overflow-wrap:\s*anywhere/.test(labelTitle), 'labels/titles: overflow-wrap: anywhere (long no-space ids break)');
  assert.ok(/word-break:\s*break-word/.test(labelTitle), 'labels/titles: word-break: break-word');
  assert.ok(/min-width:\s*0/.test(labelTitle), 'labels/titles: can shrink');
  assert.ok(/max-width:\s*100%/.test(labelTitle), 'labels/titles: capped to the row width');

  const body = cssRule('.alert-row-body');
  assert.ok(/white-space:\s*pre-wrap/.test(body), '.alert-row-body: message wraps (pre-wrap)');
  assert.ok(/overflow-wrap:\s*anywhere/.test(body), '.alert-row-body: overflow-wrap: anywhere (300-char no-space string breaks)');
  assert.ok(/word-break:\s*break-word/.test(body), '.alert-row-body: word-break: break-word');
  assert.ok(/min-width:\s*0/.test(body), '.alert-row-body: can shrink');

  const card = cssRules('.alert-card');
  assert.ok(/min-width:\s*0/.test(card), '.alert-card: the card can shrink in a flex/grid container');
  const cardOv = cssRule('.card.alert-card');
  assert.ok(/overflow:\s*hidden/.test(cardOv), '.card.alert-card: horizontal spill cannot poke out of the card');
});

test('rendered card markup: inline wrapping styles on the 128-char no-break id, long name and 300-char message (EN)', () => {
  const html = renderAt(390);
  assert.ok(html.includes(NOBREAK_ID), 'the 128-char no-space id is rendered in full');
  assert.ok(html.includes(LONG_NAME), 'the long session name is rendered in full');
  assert.ok(html.includes(LONG_MSG), 'the 300-char no-space message is rendered in full');

  assert.ok(/<a class="alert-session-label"[^>]*overflow-wrap:anywhere;word-break:break-word/.test(html),
    'session label anchor is inline-wrapped (no-space ids break anywhere)');
  assert.ok(/class="alert-session-head"[^>]*flex-wrap:wrap/.test(html),
    'session header row is inline flex-wrap:wrap');
  const i = html.indexOf('<div class="alert-row-body"');
  assert.ok(i >= 0, 'the message body div is present');
  assert.ok(html.slice(i).includes(LONG_MSG), 'the whole 300-char message is inside .alert-row-body (not truncated)');

  const FR_LAST = LOCALE('fr').alert_session_last;
  const FR = renderFR();
  assert.ok(FR.includes(FR_LAST + ' <span style="overflow-wrap:anywhere;word-break:break-word;">'),
    'localized "last" label (FR: ' + FR_LAST + ') is present and time value inline-wrapped');
  assert.ok(html.includes('last <span style="overflow-wrap:anywhere;word-break:break-word;">'),
    'localized "last" label (EN) is present and time value inline-wrapped');
});

test('hostile content in alerts is escaped, never injected', () => {
  const html = renderAt(390);
  assert.ok(html.includes(EVIL.replace(/</g, '&lt;').replace(/>/g, '&gt;')), 'evil text is HTML-escaped in the card');
  assert.ok(!html.includes(EVIL), 'and the raw <img onerror> payload is not present as markup');
});

test('the markup is width-agnostic: 390 px and 360 px render identically', () => {
  // No layout in the stub → the ONLY way 360 px could differ is if the
  // app branches the HTML on the viewport, which would defeat the CSS
  // wrap contract. Identical HTML at both widths is the requirement.
  const wide = renderAt(390);
  const narrow = renderAt(360);
  assert.equal(narrow, wide, 'rendered Alerts HTML is identical at 360 px and 390 px — narrow screens wrap via CSS, not JS');
});
