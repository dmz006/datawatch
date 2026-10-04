// BL394 security review (docs/plans/2026-10-03-bl394-security-findings-review.md
// §3h) — regression test for the escHtml/escJsAttr fixes in app.js. Uses
// Node's built-in test runner (node:test). Run:
// `node --test internal/server/web/*.test.js`.
//
// Loads the REAL, unmodified app.js and tests its real escHtml/escJsAttr
// functions directly (both are top-level `function` declarations, hoisted
// -- callable as sandbox.escHtml/sandbox.escJsAttr once the file loads).
//
// What was wrong, two separate bugs fixed in this same commit:
//
// 1. The status-board renderer interpolated board.tests.{pass,fail,skip}
//    into innerHTML completely unescaped (two call sites). These values
//    come from a hook-event payload (POST /api/sessions/{sid}/hook-event,
//    gated by CapConfigWrite) and are normally numbers, but nothing
//    enforces that.
//
// 2. A subtler bug: escHtml() alone is NOT sufficient for a value
//    embedded as a single-quoted JS string literal INSIDE an inline
//    event-handler attribute, e.g. onclick="fn('${x}')" -- confirmed two
//    real call sites doing exactly this (a channel-stats row's expand/
//    collapse toggle, and the schedule-entry edit/delete buttons). The
//    browser HTML-decodes an attribute's value (undoing escHtml's own
//    '->&#39; back to a literal ') BEFORE compiling it as JS, so an
//    HTML-entity-encoded quote still terminates the nested JS string
//    early. Only a real backslash-escaped quote (\') survives that
//    decode step -- which only works correctly if backslashes already in
//    the data are doubled FIRST (the pre-fix code escaped the quote
//    without doing this; a trailing backslash in the data would combine
//    with the one it added to produce an unescaped quote). New
//    escJsAttr() fixes both call sites.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox.escHtml, 'function', 'escHtml is not defined -- did it get renamed?');
  assert.equal(typeof sandbox.escJsAttr, 'function', 'escJsAttr is not defined -- did it get renamed?');
  return sandbox;
}

function callStr(sandbox, fnName, arg) {
  sandbox.__arg = arg;
  const result = vm.runInContext(`${fnName}(__arg)`, sandbox);
  delete sandbox.__arg;
  return result;
}

function htmlAttrDecode(s) {
  // Minimal, just the 5 entities escHtml ever produces.
  return s.replace(/&amp;/g, '&').replace(/&lt;/g, '<').replace(/&gt;/g, '>')
    .replace(/&quot;/g, '"').replace(/&#39;/g, "'");
}

// Simulates the FULL round trip an inline onclick="fn('${escJsAttr(x)}')"
// attribute goes through: (1) the string app.js produces, (2) an HTML
// parser decoding that as an attribute value (the browser's actual
// behavior for event-handler attributes), (3) a JS parser then compiling
// the decoded string as the literal contents of a single-quoted string.
function simulateInlineOnclick(sandbox, rawValue) {
  const escaped = callStr(sandbox, 'escJsAttr', rawValue);
  const decodedAttrValue = htmlAttrDecode(`fn('${escaped}')`);
  let captured;
  const fn = (x) => { captured = x; };
  new Function('fn', decodedAttrValue)(fn); // eslint-disable-line no-new-func
  return { escaped, decodedAttrValue, captured };
}

test('escHtml escapes a basic payload', () => {
  const sandbox = loadAppJS();
  const out = callStr(sandbox, 'escHtml', '<script>&"\'</script>');
  assert.ok(!out.includes('<script>'), `got: ${out}`);
});

test('escHtml stringifies non-string values unchanged (regression)', () => {
  const sandbox = loadAppJS();
  assert.equal(callStr(sandbox, 'escHtml', 42), '42');
  assert.equal(callStr(sandbox, 'escHtml', null), '');
});

test('escJsAttr contains a classic quote-breakout payload', () => {
  const sandbox = loadAppJS();
  const payload = "x'); globalThis.__pwned = true; //";
  const r = simulateInlineOnclick(sandbox, payload);
  assert.equal(globalThis.__pwned, undefined,
    `escJsAttr failed to contain the payload -- injected code ran. decoded: ${r.decodedAttrValue}`);
  assert.equal(r.captured, payload, 'value did not round-trip correctly');
});

test('escJsAttr contains the backslash-collision payload (the actual bug)', () => {
  const sandbox = loadAppJS();
  // A trailing backslash immediately before a quote -- the OLD code
  // (escape quote without doubling backslashes first) turned this into
  // an unescaped quote once combined with its own added backslash.
  const payload = "x\\'); globalThis.__pwned2 = true; //";
  const r = simulateInlineOnclick(sandbox, payload);
  assert.equal(globalThis.__pwned2, undefined,
    `escJsAttr failed to contain the backslash-collision payload -- injected code ran. decoded: ${r.decodedAttrValue}`);
  assert.equal(r.captured, payload, 'value did not round-trip correctly');
});

test('escJsAttr round-trips a normal value unchanged (regression)', () => {
  const sandbox = loadAppJS();
  const normal = 'telegram-ops-channel';
  const r = simulateInlineOnclick(sandbox, normal);
  assert.equal(r.captured, normal);
});
