// Session detail full-height fill layout guard.
//
// The session detail view must fill the whole viewport between the fixed
// header and the keyboard-aware bottom edge with a pure-flex chain — no
// page scroll, no blank band under the input row, nothing after the
// input bar, no fixed pixel heights. If a rule drifts, the terminal gets
// clipped or the input bar floats above a gap with no error anywhere.

const { test } = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');

const CSS = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');
const APP = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');

// ruleBody returns the declaration block of the base rule for `sel`,
// anchored at a line start so compound selectors (`.input-bar.needs-input`,
// `body:has(x) .new-session-fab`, ...) are never mistaken for the base rule.
function ruleBody(sel) {
  const esc = sel.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const m = new RegExp('(?:^|\\n)\\s*' + esc + '\\s*\\{([\\s\\S]*?)\\}\\n', 'm').exec(CSS);
  assert.ok(m, 'style.css has a base rule for ' + sel);
  return m[1];
}

function noDecl(body, re, label) {
  assert.ok(!re.test(body), label + ' must not be in the rule');
}

// A "fixed" height is one carrying a length (px/vh/dvh/%) or a calc().
// Unitless `min-height: 0` is the flex-shrink idiom, not a fixed height.
const FIXED_HEIGHT_RE = /(?:min-|max-)?height:\s*[^;]*(?:px|%|vh|vw|dvh|dvw|calc\()/;

test('.view.view-full drops the FAB-clearance padding and becomes a full-height flex column', () => {
  const body = ruleBody('.view.view-full');
  assert.match(body, /padding-bottom:\s*0/, 'bottom padding reset to zero');
  assert.match(body, /display:\s*flex/, 'a flex container');
  assert.match(body, /flex-direction:\s*column/, 'column direction');
  assert.match(body, /overflow:\s*hidden/, 'no page scroll in full views');
  assert.match(body, /bottom:\s*var\(--keyboard-h/, 'keyboard-aware bottom offset stays');
});

test('base .view keeps the FAB-clearance padding for scrolling list pages', () => {
  const body = ruleBody('.view');
  assert.match(body, /padding-bottom:\s*calc\(\s*56px/, 'list pages need FAB clearance padding');
  assert.match(body, /overflow-y:\s*auto/, 'list pages must stay scrollable');
});

test('.session-detail is a full-height flex column that never scrolls itself', () => {
  const body = ruleBody('.session-detail');
  assert.match(body, /display:\s*flex/, 'display:flex');
  assert.match(body, /flex-direction:\s*column/, 'flex-direction:column');
  assert.match(body, /flex:\s*1/, 'flex:1 — stretches into .view.view-full');
  assert.match(body, /min-height:\s*0(?!px)/, 'min-height:0 so the flex child can shrink');
  assert.match(body, /overflow:\s*hidden/, 'overflow hidden — xterm scrolls inside itself');
  // percent height resolves against the content box (excludes .view
  // padding) — the original blank-band bug, so it must not come back:
  noDecl(body, /height:\s*100%/, 'no percentage height');
  noDecl(body, FIXED_HEIGHT_RE, 'no fixed pixel height');
});

test('.output-area is flexible: flex:1 + min-height:0, no fixed pixel height', () => {
  const body = ruleBody('.output-area');
  assert.match(body, /flex:\s*1/, 'flex:1 — takes all remaining height');
  assert.match(body, /min-height:\s*0(?!px)/, 'min-height:0 — critical for the flex chain');
  noDecl(body, FIXED_HEIGHT_RE, 'no fixed pixel height');
});

test('.input-bar is pinned to the bottom: flex-shrink:0 and no bottom spacer', () => {
  const body = ruleBody('.input-bar');
  assert.match(body, /flex-shrink:\s*0/, 'flex-shrink:0 — the bar never squashes');
  noDecl(body, /margin-bottom/, 'no margin-bottom spacer');
});

test('renderSessionDetail draws the input bar as the last child of .session-detail', () => {
  // The whole chain in one structural match: the session-detail container
  // is opened, the input bar is drawn, and the very next markup is the
  // input bar's closing </div>, the end of its conditional, and the
  // session-detail closing </div> right at the end of the template —
  // i.e. nothing is rendered after the input bar.
  assert.match(APP, /<div class="session-detail">[\s\S]*?<div class="input-bar[^`]*?<\/div>` : ''\}\n    <\/div>`;/);
  // The input bar element itself still carries its id and the waiting /
  // disabled modifier classes the CSS and JS depend on.
  assert.match(APP, /<div class="input-bar\$\{isWaiting \? ' needs-input' : ''\}\$\{!connReady \? ' input-disabled' : ''\}" id="inputBar">/);
});

test('phone layout keeps dvh sizing plus the keyboard and safe-area insets', () => {
  // dvh is the base viewport unit (JS may swap it for --app-h from
  // visualViewport, but the CSS must still fall back to dvh).
  assert.match(ruleBody('.app'), /dvh/, 'dvh as the viewport unit');
  assert.match(ruleBody('.view'), /env\(safe-area-inset-bottom, 0px\)/, 'env(safe-area-inset-bottom) still applied on .view');
  assert.match(ruleBody('.input-bar'), /env\(safe-area-inset-bottom, 0\)/, 'input bar keeps its safe-area padding');
  // The FAB stays out of the layout flow, so nothing below the input bar
  // reserves a row either.
  assert.match(ruleBody('.new-session-fab'), /position:\s*fixed/, 'FAB is fixed-positioned');
  // And app.js still measures the keyboard from visualViewport into
  // --keyboard-h.
  assert.match(APP, /setProperty\('--keyboard-h', keyboardH \+ 'px'\)/);
  assert.match(APP, /visualViewport\.addEventListener\('resize', _setAppH\)/, 'viewport resize re-measures app height');
});

test('navigate() applies view-full only to the session-detail branch', () => {
  const i = APP.indexOf("view === 'session-detail'");
  const branch = APP.slice(i, APP.indexOf('state.activeSession = null;'));
  assert.ok(i >= 0 && branch.length > 0, 'session-detail navigate branch found in app.js');
  assert.match(branch, /classList\.add\('view-full'\)/, 'session detail adds the view-full class');
  assert.strictEqual(APP.match(/classList\.add\('view-full'\)/g).length, 1, 'view-full is applied in exactly one place');
  assert.match(branch, /nav\.style\.display\s*=\s*'none'/, 'bottom nav hides on session detail');
});

test('the resize handler still re-fits the terminal', () => {
  // The resize path: _vvResize pins the container to the keyboard-aware
  // height, then _fitAndScroll calls _termSafeFit — a page resize or
  // keyboard pop keeps the terminal fitted to the new height.
  assert.match(APP, /const _vvResize = \(\) => \{[\s\S]*?_setContainerH\(\)/, 'resize pins the container height from visualViewport');
  assert.match(APP, /const _vvResize = \(\) => \{[\s\S]*?_fitAndScroll\(\)/, 'resize re-fits the terminal');
  assert.match(APP, /window\.visualViewport\.addEventListener\('resize', _vvResize\)/, 'visualViewport resize listener attached');
  assert.match(APP, /state\._viewportResizeHandler = _vvResize/, 'handler tracked for removal on teardown');
  assert.match(APP, /const _fitAndScroll = \(\) => \{[\s\S]*?_termSafeFit\(/, 'the re-fit goes through the guarded fit()');
  // Container resizes without a keyboard (desktop window drag, zoom) go
  // through the ResizeObserver, torn down when leaving the view:
  assert.match(APP, /new ResizeObserver/, 'ResizeObserver drives the xterm re-fit');
  assert.match(APP, /state\._termResizeObserver\.disconnect/, 'observer torn down on leave');
});
