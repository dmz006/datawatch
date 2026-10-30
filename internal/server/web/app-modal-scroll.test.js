'use strict';
// Regression: showModal's dialog must scroll its body, not the page, so the
// title and the Cancel / confirm buttons stay reachable however long the
// caller's body is and however little of the screen is visible.
//
// Before the fix the dialog grew with its body: on a phone a long body pushed
// the buttons below the bottom edge with no way to scroll to them, and the
// on-screen keyboard made it worse. What this file protects:
//   * markup: the caller's body sits in one .generic-modal-body; the title
//     comes before it and both buttons come after its closing tag, so they are
//     outside the scrolling region;
//   * style.css: .generic-modal is a flex column capped at the visible height
//     (vh, and dvh / --generic-modal-vvh under @supports) with overflow hidden,
//     and only .generic-modal-body scrolls (overflow-y:auto, min-height:0);
//   * geometry: with the real CSS numbers at 360x640 and at 360x400 (keyboard
//     open) the body's scroll viewport is positive, smaller than a long body,
//     and the button row ends inside the visible area;
//   * keyboard: showModal follows window.visualViewport (resize + scroll),
//     pins the overlay to it, and unhooks both listeners once the dialog it
//     created is gone; it must not throw without visualViewport;
//   * short dialogs still shrink to their content and keep their old markup,
//     buttons and click behaviour;
//   * the other confirm-modal dialogs gained no height cap or scrolling.
//
// Layout is checked against the real style.css text, the same way
// app-observer-stats-scroll.test.js does it. There is no layout engine here.
//
// Run: node --test app-modal-scroll.test.js

const fs = require('fs');
const path = require('path');
const test = require('node:test');
const assert = require('node:assert/strict');

const vm = require('vm');
const { buildSandbox, loadScript, makeStubElement } = require(path.join(__dirname, 'testutil_browser_stub.js'));

const APP_JS = path.join(__dirname, 'app.js');
const STYLE_CSS = path.join(__dirname, 'style.css');
const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
const CSS = fs.readFileSync(STYLE_CSS, 'utf8');

// ── CSS helpers ─────────────────────────────────────────────────────────────

// parseCss flattens a stylesheet into [{ selectors, decls, at }]. `at` is the
// text of the enclosing at-rule prelude ('' for top level, e.g. '@supports
// (height: 100dvh)'). Comments are stripped first. Declarations come back as
// an ordered list of [prop, value] pairs so duplicates stay visible.
function parseCss(text, at = '') {
  const src = text.replace(/\/\*[\s\S]*?\*\//g, '');
  const rules = [];
  let i = 0;
  while (i < src.length) {
    const open = src.indexOf('{', i);
    if (open < 0) break;
    const prelude = src.slice(i, open).trim();
    let depth = 1;
    let j = open + 1;
    while (j < src.length && depth > 0) {
      if (src[j] === '{') depth += 1;
      else if (src[j] === '}') depth -= 1;
      j += 1;
    }
    const inner = src.slice(open + 1, j - 1);
    if (prelude.startsWith('@')) {
      // @media / @supports hold nested rules; @keyframes / @font-face never
      // match the selectors asked for here, so recursing into them is harmless.
      rules.push(...parseCss(inner, prelude));
    } else {
      const decls = inner.split(';').map(d => d.trim()).filter(Boolean).map(d => {
        const k = d.indexOf(':');
        return [d.slice(0, k).trim().toLowerCase(), d.slice(k + 1).trim()];
      });
      rules.push({
        selectors: prelude.split(',').map(s => s.replace(/\s+/g, ' ').trim()),
        decls,
        at,
      });
    }
    i = j;
  }
  return rules;
}

const ALL_RULES = parseCss(CSS);

// decls returns the [prop, value] pairs of every rule whose selector list
// contains exactly `selector`, filtered by at-rule: 'top' = outside any
// at-rule, 'supports' = inside an @supports block, 'any' = everywhere.
function decls(selector, where = 'top') {
  return ALL_RULES
    .filter(r => r.selectors.includes(selector))
    .filter(r => where === 'any'
      || (where === 'top' && r.at === '')
      || (where === 'supports' && r.at.startsWith('@supports')))
    .flatMap(r => r.decls);
}

// declMap collapses declarations to prop -> last value (CSS cascade order).
function declMap(selector, where = 'top') {
  const m = {};
  for (const [k, v] of decls(selector, where)) m[k] = v;
  return m;
}

// px reads the first plain "<n>px" out of a value.
const px = v => Number(/(-?\d+(?:\.\d+)?)px/.exec(v)[1]);

// ── Sandbox ─────────────────────────────────────────────────────────────────

// makeEnv loads the real app.js into a sandbox whose document keeps track of
// the one #genericModal overlay showModal creates, so tests can read its
// markup, style and handlers, and see it disappear when remove() is called.
// `visualViewport` is put on window only when given.
function makeEnv({ visualViewport } = {}) {
  const env = { current: null, created: [], removed: 0, confirmBtn: null };
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = en;

  sandbox.document.createElement = () => {
    const el = makeStubElement();
    const style = {};
    style.setProperty = (k, v) => { style[k] = v; };
    el.style = style;
    el.handlers = {};
    el.addEventListener = (type, fn) => { el.handlers[type] = fn; };
    el.remove = () => {
      if (env.current === el) env.current = null;
      env.removed += 1;
    };
    env.created.push(el);
    return el;
  };
  sandbox.document.body.appendChild = (el) => {
    env.current = el;
    env.confirmBtn = makeStubElement();
  };
  sandbox.document.getElementById = (id) => {
    if (id === 'genericModal') return env.current;
    if (id === 'genericModalConfirmBtn') return env.current ? env.confirmBtn : null;
    return makeStubElement();
  };
  if (visualViewport) sandbox.visualViewport = visualViewport;

  env.sandbox = sandbox;
  env.show = opts => sandbox.showModal(opts);
  return env;
}

// makeVisualViewport records add/remove calls and exposes the handler per type.
function makeVisualViewport(height, offsetTop = 0) {
  const vv = { height, offsetTop, added: [], removedL: [], handlers: {} };
  vv.addEventListener = (type, fn) => { vv.added.push(type); vv.handlers[type] = fn; };
  vv.removeEventListener = (type, fn) => { vv.removedL.push({ type, fn }); };
  return vv;
}

const LONG_LINES = Array.from({ length: 300 }, (_, i) => `<p class="line">line ${i + 1}: some body text</p>`);
const LONG_BODY = LONG_LINES.join('\n');

// ── Geometry model ──────────────────────────────────────────────────────────

// This is a layout MODEL built from the real style.css numbers and the real
// showModal markup, not a browser: it has no text layout, so the title block
// is its font size plus margin, and the button row is a fixed 30px (12px font,
// 4px padding, line and border). The point is the arithmetic that decides
// whether the buttons stay on screen: dialog height is capped by max-height,
// the body takes what is left after padding, title and buttons, and the dialog
// sits inside the overlay's padding.
const BUTTON_ROW_PX = 30;
const LINE_PX = 18; // one body line

// resolveMaxHeight evaluates a "calc(<100vh|var(--x, 100dvh)> - Npx)" value
// for a viewport of `vh` px, where the custom property is `vvh` px when set.
function resolveMaxHeight(value, { vh, vvh }) {
  const m = /^calc\(\s*(100vh|var\(\s*--generic-modal-vvh\s*,\s*100dvh\s*\))\s*-\s*(\d+)px\s*\)$/.exec(value);
  assert.ok(m, `max-height must be calc(<viewport height> - Npx), got ${value}`);
  const base = m[1] === '100vh' ? vh : (vvh !== undefined ? vvh : vh);
  return { height: base - Number(m[2]), reserve: Number(m[2]) };
}

// bodyViewport models the dialog at a visible height and returns its parts.
function bodyViewport({ visible, usesVar, vvh }) {
  const dlg = declMap('.generic-modal');
  const sup = declMap('.generic-modal', 'supports');
  const rule = usesVar ? sup['max-height'] : dlg['max-height'];
  const { height: maxH, reserve } = resolveMaxHeight(rule, { vh: visible, vvh });

  const overlayPad = px(declMap('.confirm-modal-overlay').padding);
  const dialogPad = /^(\d+)px\b/.exec(declMap('.confirm-modal').padding);
  assert.ok(dialogPad, '.confirm-modal must have a px padding');
  const padVert = Number(dialogPad[1]) * 2; // "16px 20px": top and bottom are the first value

  // Title block and body margin come from the markup showModal really emits.
  const env = makeEnv();
  env.show({ title: 'T', body: 'x' });
  const html = env.current.innerHTML;
  const titleStyle = /class="generic-modal-title" style="([^"]*)"/.exec(html)[1];
  const bodyStyle = /class="generic-modal-body" style="([^"]*)"/.exec(html)[1];
  const titleBlock = px(/font-size:\s*([^;]+)/.exec(titleStyle)[1]) + px(/margin-bottom:\s*([^;]+)/.exec(titleStyle)[1]);
  const bodyMargin = px(/margin-bottom:\s*([^;]+)/.exec(bodyStyle)[1]);

  const bodyViewportPx = maxH - padVert - titleBlock - BUTTON_ROW_PX - bodyMargin;
  // A long body fills the dialog to its cap, so the dialog is exactly maxH tall.
  const dialogH = maxH;
  return { maxH, reserve, overlayPad, padVert, titleBlock, bodyMargin, bodyViewportPx, dialogH,
    buttonsBottom: overlayPad + dialogH };
}

// ── 1. A very long body ─────────────────────────────────────────────────────

test('very long body: one scrolling body holds it all; title before, both buttons after (outside the scroll area)', () => {
  const env = makeEnv();
  env.show({ title: 'Long dialog', body: LONG_BODY, confirmLabel: 'Do it' });
  assert.equal(env.created.length, 1);
  const html = env.current.innerHTML;

  const openings = html.match(/class="generic-modal-body"/g) || [];
  assert.equal(openings.length, 1, 'exactly one .generic-modal-body');

  // The body div: from its opening tag's end to the closing </div> that must
  // directly follow the caller's HTML.
  const openIdx = html.indexOf('class="generic-modal-body"');
  const bodyStart = html.indexOf('>', openIdx) + 1;
  const bodyAt = html.indexOf(LONG_BODY);
  assert.equal(bodyAt, bodyStart, 'the whole 300-line body starts right at the body div content');
  const bodyEnd = bodyAt + LONG_BODY.length;
  assert.equal(html.slice(bodyEnd, bodyEnd + '</div>'.length), '</div>', 'body div closes right after the caller HTML');
  assert.equal((LONG_BODY.match(/<p class="line">/g) || []).length, 300);
  assert.equal((html.match(/<p class="line">/g) || []).length, 300, 'no line is lost or duplicated');

  const titleIdx = html.indexOf('class="generic-modal-title"');
  assert.ok(titleIdx >= 0 && titleIdx < openIdx, 'title comes before the body');
  assert.match(html.slice(titleIdx), /^class="generic-modal-title"[^>]*>Long dialog</);

  const afterBody = html.slice(bodyEnd + '</div>'.length);
  const actionsIdx = afterBody.indexOf('class="generic-modal-actions"');
  assert.ok(actionsIdx >= 0, 'actions row follows the body');
  const cancelIdx = afterBody.indexOf('Cancel</button>');
  const confirmIdx = afterBody.indexOf('id="genericModalConfirmBtn"');
  assert.ok(cancelIdx > actionsIdx, 'Cancel button is after the body closing tag, in the actions row');
  assert.ok(confirmIdx > actionsIdx, 'confirm button is after the body closing tag, in the actions row');
  assert.ok(afterBody.indexOf('Do it</button>') > confirmIdx, 'confirm button carries its label');
  // Neither button is anywhere before the end of the body.
  const beforeBodyEnd = html.slice(0, bodyEnd);
  assert.ok(!beforeBodyEnd.includes('genericModalConfirmBtn'), 'confirm button is not inside or before the body');
  assert.ok(!beforeBodyEnd.includes('<button'), 'no button inside or before the scrolling body');
});

// ── 2. CSS contract ─────────────────────────────────────────────────────────

test('style.css: .generic-modal is a capped flex column; only .generic-modal-body scrolls', () => {
  const dlg = declMap('.generic-modal');
  assert.equal(dlg.display, 'flex');
  assert.equal(dlg['flex-direction'], 'column');
  assert.equal(dlg.overflow, 'hidden', 'the dialog itself must not scroll or spill');
  assert.match(dlg['max-height'], /^calc\(\s*100vh\s*-\s*\d+px\s*\)$/, 'vh max-height fallback');

  const sup = declMap('.generic-modal', 'supports');
  assert.ok(ALL_RULES.some(r => r.at.replace(/\s+/g, ' ') === '@supports (height: 100dvh)' && r.selectors.includes('.generic-modal')),
    'a .generic-modal rule inside @supports (height: 100dvh)');
  assert.match(sup['max-height'], /^calc\(\s*var\(\s*--generic-modal-vvh\s*,\s*100dvh\s*\)\s*-\s*\d+px\s*\)$/,
    'dvh max-height driven by --generic-modal-vvh');

  const body = declMap('.generic-modal-body');
  assert.equal(body['overflow-y'], 'auto');
  assert.equal(body['min-height'], '0', 'min-height:0 lets the flex child shrink below its content');
  assert.equal(body.flex, '1 1 auto');

  const title = declMap('.generic-modal-title');
  const actions = declMap('.generic-modal-actions');
  assert.equal(title.flex, '0 0 auto', 'title never shrinks');
  assert.equal(actions.flex, '0 0 auto', 'buttons never shrink');

  const overlay = declMap('.confirm-modal-overlay');
  assert.equal(overlay.padding, '12px', 'overlay still reserves 12px on each side');
});

// ── 3. Geometry at phone sizes ──────────────────────────────────────────────

test('geometry model at 360x640: buttons stay inside the viewport, body scrolls (model of the real CSS numbers, not a browser)', () => {
  const g = bodyViewport({ visible: 640, usesVar: false });
  assert.equal(g.overlayPad * 2, g.reserve, 'the 24px reserve is the overlay padding top plus bottom');
  assert.equal(g.reserve, 24);
  assert.equal(g.maxH, 640 - 24, 'dialog max height is 616');
  assert.equal(g.padVert, 32, 'dialog padding is 16px top and 16px bottom');
  assert.equal(g.titleBlock, 14 + 12, 'title: 14px font plus 12px margin');

  const longBodyContent = 300 * LINE_PX;
  assert.ok(g.bodyViewportPx > 0, `body viewport ${g.bodyViewportPx} must be positive`);
  assert.ok(g.bodyViewportPx < longBodyContent, 'a 300-line body is taller than its viewport, so it scrolls');
  assert.ok(g.buttonsBottom <= 640, `button row bottom ${g.buttonsBottom} must be inside the 640 viewport`);
  assert.equal(g.buttonsBottom, 12 + 616);
});

test('geometry model at 360x400 (keyboard open, via visualViewport): buttons stay inside the visible height', () => {
  // The visible height is whatever showModal writes to --generic-modal-vvh when
  // the visual viewport shrinks to 400.
  const vv = makeVisualViewport(640);
  const env = makeEnv({ visualViewport: vv });
  env.show({ title: 'T', body: LONG_BODY });
  vv.height = 400;
  vv.handlers.resize();
  const vvh = px(env.current.style['--generic-modal-vvh']);
  assert.equal(vvh, 400);
  assert.equal(px(env.current.style.height), 400, 'overlay is pinned to the visible height');

  const g = bodyViewport({ visible: 640, usesVar: true, vvh });
  assert.equal(g.maxH, 400 - 24, 'dialog max height follows the visual viewport');
  assert.ok(g.bodyViewportPx > 0, `body viewport ${g.bodyViewportPx} must be positive at 400`);
  assert.ok(g.bodyViewportPx < 300 * LINE_PX, 'the long body still scrolls');
  assert.ok(g.buttonsBottom <= 400, `button row bottom ${g.buttonsBottom} must be inside the 400 visible height`);

  // And the vh fallback (no dvh support) at 400 holds the same bound.
  const fb = bodyViewport({ visible: 400, usesVar: false });
  assert.ok(fb.bodyViewportPx > 0 && fb.buttonsBottom <= 400);
});

// ── 4. Keyboard: visualViewport tracking ────────────────────────────────────

test('visualViewport: listeners registered, overlay pinned on resize, listeners removed once the dialog is gone', () => {
  const vv = makeVisualViewport(600, 0);
  const env = makeEnv({ visualViewport: vv });
  env.show({ title: 'T', body: '<p>b</p>' });
  const overlay = env.current;

  assert.deepEqual([...vv.added].sort(), ['resize', 'scroll'], 'resize and scroll listeners registered');
  assert.equal(vv.handlers.resize, vv.handlers.scroll, 'one handler serves both events');
  assert.equal(overlay.style['--generic-modal-vvh'], '600px', 'initial fit runs immediately');
  assert.equal(overlay.style.top, '0px');
  assert.equal(overlay.style.height, '600px');
  assert.equal(overlay.style.bottom, 'auto');

  // Keyboard opens: visible height shrinks, browser scrolls the visual viewport.
  vv.height = 300;
  vv.offsetTop = 20;
  vv.handlers.resize();
  assert.equal(overlay.style['--generic-modal-vvh'], '300px');
  assert.equal(overlay.style.top, '20px');
  assert.equal(overlay.style.height, '300px');
  assert.equal(overlay.style.bottom, 'auto');
  assert.equal(vv.removedL.length, 0, 'listeners stay while the dialog is open');

  // The scroll event drives the same fit.
  vv.height = 280;
  vv.offsetTop = 40;
  vv.handlers.scroll();
  assert.equal(overlay.style['--generic-modal-vvh'], '280px');
  assert.equal(overlay.style.top, '40px');

  // Dialog removed (getElementById no longer returns it): next event unhooks.
  env.current = null;
  vv.height = 100;
  vv.handlers.resize();
  assert.deepEqual(vv.removedL.map(r => r.type).sort(), ['resize', 'scroll'], 'both listeners removed');
  assert.ok(vv.removedL.every(r => r.fn === vv.handlers.resize), 'the very same handler is unhooked');
  assert.equal(overlay.style['--generic-modal-vvh'], '280px', 'a removed dialog is no longer touched');
  assert.equal(overlay.style.height, '280px', 'height untouched after removal');
  assert.equal(overlay.style.top, '40px', 'top untouched after removal');
});

test('visualViewport: a handler from a replaced dialog unhooks itself instead of fitting the new one', () => {
  const vv = makeVisualViewport(500, 0);
  const env = makeEnv({ visualViewport: vv });
  env.show({ title: 'first', body: 'a' });
  const first = env.current;
  const firstFit = vv.handlers.resize;
  env.show({ title: 'second', body: 'b' });
  const second = env.current;
  assert.notEqual(first, second);
  assert.equal(env.removed, 1, 'opening a second dialog removes the first');

  vv.height = 250;
  firstFit();
  assert.ok(vv.removedL.some(r => r.fn === firstFit && r.type === 'resize'), 'first dialog handler unhooked');
  assert.equal(first.style['--generic-modal-vvh'], '500px', 'first overlay not fitted again');
  assert.equal(second.style['--generic-modal-vvh'], '500px', 'second overlay untouched by the first handler');
});

test('no visualViewport: showModal does not throw and sets no viewport var', () => {
  const env = makeEnv();
  assert.equal(env.sandbox.visualViewport, undefined);
  assert.doesNotThrow(() => env.show({ title: 'T', body: LONG_BODY }));
  assert.ok(env.current, 'dialog still opens');
  assert.equal(env.current.style['--generic-modal-vvh'], undefined);
  assert.equal(env.current.style.height, undefined, 'overlay left to its CSS size');
});

// ── 5. Short dialog regression ──────────────────────────────────────────────

test('short dialog: same overlay, dialog classes, inline width, escaped title, body and buttons as before', () => {
  const env = makeEnv();
  env.show({ title: 'Hi', body: '<p>x</p>', confirmLabel: 'Yes' });
  const overlay = env.current;
  assert.equal(overlay.id, 'genericModal');
  assert.equal(overlay.className, 'confirm-modal-overlay');
  const html = overlay.innerHTML;

  const dialog = /<div class="([^"]*)" style="([^"]*)">/.exec(html);
  assert.ok(dialog, 'inner dialog present');
  assert.ok(dialog[1].split(/\s+/).includes('confirm-modal'), 'dialog still carries confirm-modal');
  assert.ok(dialog[1].split(/\s+/).includes('generic-modal'));
  assert.equal(dialog[2], 'max-width:500px;width:95%;');
  assert.match(html, /class="generic-modal-title"[^>]*>Hi<\/div>/);
  assert.match(html, /class="generic-modal-body"[^>]*><p>x<\/p><\/div>/);
  assert.match(html, /<button class="btn-secondary"[^>]*onclick="document\.getElementById\('genericModal'\)\.remove\(\)"[^>]*>Cancel<\/button>/);
  assert.match(html, /<button class="btn-primary"[^>]*id="genericModalConfirmBtn"[^>]*>Yes<\/button>/);

  // Defaults and escaping.
  const env2 = makeEnv();
  env2.show({ title: '<b>"A" & \'B\'</b>', body: 'x', cancelLabel: 'Nope' });
  assert.match(env2.current.innerHTML, /class="generic-modal-title"[^>]*>&lt;b&gt;&quot;A&quot; &amp; &#39;B&#39;&lt;\/b&gt;<\/div>/);
  assert.match(env2.current.innerHTML, />Nope<\/button>/);
  assert.match(env2.current.innerHTML, />OK<\/button>/, 'confirm label defaults to the translated OK');
});

test('short dialog: confirm closes unless onConfirm returns false; overlay click closes only on the overlay itself', () => {
  // No onConfirm: closes.
  let env = makeEnv();
  env.show({ title: 'T', body: 'b' });
  env.confirmBtn.onclick();
  assert.equal(env.current, null, 'no onConfirm closes the dialog');

  // onConfirm returning undefined / true / 0 closes (only false keeps it open).
  for (const ret of [undefined, true, 0, null, 'x']) {
    env = makeEnv();
    let calls = 0;
    env.show({ title: 'T', body: 'b', onConfirm: () => { calls += 1; return ret; } });
    env.confirmBtn.onclick();
    assert.equal(calls, 1);
    assert.equal(env.current, null, `onConfirm returning ${String(ret)} closes the dialog`);
  }

  // onConfirm returning false keeps it open, and a later true closes it.
  env = makeEnv();
  let answer = false;
  env.show({ title: 'T', body: 'b', onConfirm: () => answer });
  env.confirmBtn.onclick();
  assert.ok(env.current, 'onConfirm returning false keeps the dialog open');
  assert.equal(env.removed, 0);
  answer = true;
  env.confirmBtn.onclick();
  assert.equal(env.current, null);

  // Overlay click: only a click whose target is the overlay closes.
  env = makeEnv();
  env.show({ title: 'T', body: 'b' });
  const overlay = env.current;
  assert.equal(typeof overlay.handlers.click, 'function', 'overlay click handler registered');
  overlay.handlers.click({ target: makeStubElement() });
  assert.equal(env.current, overlay, 'a click inside the dialog does not close it');
  overlay.handlers.click({ target: overlay });
  assert.equal(env.current, null, 'a click on the overlay closes it');
});

test('short dialog shrinks to its content: .generic-modal rules set no fixed height, min-height or max-height beyond the two caps', () => {
  const dialogRules = ALL_RULES.filter(r => r.selectors.includes('.generic-modal'));
  assert.equal(dialogRules.length, 2, 'the plain rule and the @supports rule');
  const sizing = dialogRules.flatMap(r => r.decls.filter(([k]) => /^(height|min-height|max-height)$/.test(k)));
  assert.deepEqual(sizing.map(([k]) => k), ['max-height', 'max-height'], 'only the two max-height caps');
  assert.equal(decls('.generic-modal', 'any').filter(([k]) => k === 'height' || k === 'min-height').length, 0);
  assert.ok(sizing.every(([, v]) => v.startsWith('calc(')), 'both caps are calc() of the viewport, not fixed lengths');

  // Neither the title nor the actions row, nor the body, sets a height.
  for (const sel of ['.generic-modal-title', '.generic-modal-actions', '.generic-modal-body']) {
    const m = declMap(sel, 'any');
    assert.equal(m.height, undefined, `${sel} has no fixed height`);
    assert.equal(m['max-height'], undefined, `${sel} has no max-height`);
  }
  // The body's only min-height is the zero that lets it shrink.
  assert.equal(declMap('.generic-modal-body')['min-height'], '0');
  // The dialog flex-sizes its body (grow/shrink from auto), never a fixed basis.
  assert.equal(declMap('.generic-modal-body')['flex-basis'], undefined);

  // Short content is smaller than the cap, so the model gives it its natural
  // size: 1 line of body is far below the 640-high cap.
  const g = bodyViewport({ visible: 640, usesVar: false });
  assert.ok(LINE_PX < g.bodyViewportPx, 'a one-line body fits in the body viewport without scrolling');
});

// ── 6. Other dialogs untouched ──────────────────────────────────────────────

test('other confirm-modal dialogs: .confirm-modal and the overlay gained no height cap or scrolling; width cap unchanged', () => {
  for (const sel of ['.confirm-modal', '.confirm-modal-overlay']) {
    const all = decls(sel, 'any');
    assert.ok(all.length > 0, `${sel} rule must exist`);
    for (const [k] of all) {
      assert.notEqual(k, 'max-height', `${sel} must not gain max-height`);
      assert.notEqual(k, 'overflow-y', `${sel} must not gain overflow-y`);
    }
  }
  // Same for the shared pair selector: only max-width and box-sizing there.
  const pair = declMap('.confirm-modal-overlay > .confirm-modal');
  assert.equal(pair['max-width'], 'min(560px, calc(100% - 24px)) !important');
  assert.equal(pair['max-height'], undefined);
  assert.equal(pair['overflow-y'], undefined);

  // The existing .confirm-modal look is intact.
  const cm = declMap('.confirm-modal');
  assert.equal(cm.padding, '16px 20px');
  assert.equal(cm['min-width'], '220px');
  assert.equal(declMap('.confirm-modal-overlay').overflow, 'hidden');
});
