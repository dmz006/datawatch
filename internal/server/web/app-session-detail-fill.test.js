// Ratchet for the session-detail blank-band fix: the session page is a
// fill-the-viewport layout (.view.view-full becomes a flex column with no
// FAB-clearance padding, .session-detail stretches with flex:1), not a
// scrolling list. Percent/flex sizing only — no fixed pixel heights — so
// window resize and browser zoom keep the page filled via the existing
// ResizeObserver/visualViewport re-fit path. These asserts keep the layout
// invariants in place if a future edit reintroduces the band.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');

const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');
const appJs = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');

// Extract the first top-level rule block for a selector (brace-matched,
// comments stripped). Returns '' when the selector is not found.
function ruleBlock(sel) {
  const text = css.replace(/\/\*[\s\S]*?\*\//g, '');
  // Anchor at a line start so compound selectors (e.g.
  // "body:has(x) .new-session-fab {") are never mistaken for the base rule.
  const m = text.match(new RegExp('(?:^|\\n)\\s*' + sel.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\s*\\{'));
  if (!m) return '';
  let i = text.indexOf('{', m.index);
  if (i < 0) return '';
  let depth = 0;
  for (let j = i; j < text.length; j++) {
    if (text[j] === '{') depth++;
    if (text[j] === '}') {
      depth--;
      if (depth === 0) return text.slice(i + 1, j);
    }
  }
  return '';
}

test('.view.view-full drops FAB clearance, disables scroll, becomes a flex column', () => {
  const body = ruleBlock('.view.view-full');
  assert.ok(body, '.view.view-full rule is missing from style.css');
  assert.match(body, /padding-bottom\s*:\s*0\s*;/, 'view-full must reset padding-bottom to 0');
  assert.match(body, /overflow\s*:\s*hidden\s*;/, 'view-full must disable page scroll');
  assert.match(body, /display\s*:\s*flex\s*;/, 'view-full must be a flex container');
  assert.match(body, /flex-direction\s*:\s*column\s*;/, 'view-full must stack vertically');
  assert.match(body, /bottom\s*:\s*var\(--keyboard-h/, 'view-full bottom must stay keyboard-aware');
});

test('base .view keeps the FAB-clearance padding for scrolling list pages', () => {
  const body = ruleBlock('.view');
  assert.ok(body, 'base .view rule is missing from style.css');
  assert.match(body, /padding-bottom\s*:\s*calc\(\s*56px/, 'list pages need FAB clearance padding');
  assert.match(body, /overflow-y\s*:\s*auto\s*;/, 'list pages must stay scrollable');
});

test('.session-detail stretches with flex sizing, not a percentage height', () => {
  const body = ruleBlock('.session-detail');
  assert.ok(body, '.session-detail rule is missing from style.css');
  assert.match(body, /flex\s*:\s*1\s*;/, '.session-detail must grow to fill the view');
  assert.match(body, /min-height\s*:\s*0\s*;/, '.session-detail must allow flex shrink');
  assert.match(body, /overflow\s*:\s*hidden\s*;/, '.session-detail must not page-scroll');
  assert.ok(!/height\s*:\s*100%/.test(body), 'height:100% would resolve against the content box and re-create the band');
});

test('.output-area and .input-bar keep their flex roles', () => {
  const out = ruleBlock('.output-area');
  assert.match(out, /flex\s*:\s*1\s*;/, '.output-area must grow to fill');
  assert.match(out, /min-height\s*:\s*0\s*;/, '.output-area must allow flex shrink');
  const bar = ruleBlock('.input-bar');
  assert.match(bar, /flex-shrink\s*:\s*0\s*;/, 'input row must never shrink');
  assert.match(bar, /padding-bottom\s*:\s*calc\([^)]*env\(safe-area-inset-bottom/, 'input row must keep safe-area inset');
});

test('FAB is fixed-positioned (reserves no layout space) and dvh units are kept', () => {
  const fab = ruleBlock('.new-session-fab');
  assert.match(fab, /position\s*:\s*fixed\s*;/, 'FAB must stay out of the layout flow');
  assert.match(css, /100dvh/, 'dynamic viewport unit rules must remain for phones');
});

test('navigate() applies view-full only to the session-detail branch', () => {
  const branch = appJs.slice(appJs.indexOf("view === 'session-detail'"), appJs.indexOf("state.activeSession = null;"));
  assert.ok(branch.length > 0, 'session-detail navigate branch not found in app.js');
  assert.match(branch, /classList\.add\('view-full'\)/, 'session-detail must add view-full');
  assert.match(branch, /nav\.style\.display\s*=\s*'none'/, 'bottom nav must hide on session detail');
});

test('terminal re-fit relies on the existing ResizeObserver + visualViewport path', () => {
  assert.match(appJs, /new ResizeObserver/, 'xterm re-fit must use a ResizeObserver');
  assert.match(appJs, /window\.visualViewport\.addEventListener\('resize'/, 'xterm re-fit must listen to visualViewport resize');
  assert.match(appJs, /state\._termResizeObserver\.disconnect/, 'observer must be torn down on leave');
});
