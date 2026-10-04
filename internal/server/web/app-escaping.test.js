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
  // Minimal, just the 5 entities escHtml ever produces. &amp; must decode
  // LAST (mirroring escHtml's own &-first encode order) -- decoding it
  // first would double-unescape a payload containing literal "&amp;lt;"
  // text, turning it into "<" here even though a real browser's parser
  // decodes entities in one pass and would stop at "&lt;" (CodeQL
  // js/double-escaping, alert #629, caught on this exact line).
  return s.replace(/&lt;/g, '<').replace(/&gt;/g, '>')
    .replace(/&quot;/g, '"').replace(/&#39;/g, "'").replace(/&amp;/g, '&');
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

test('htmlAttrDecode does not double-unescape literal "&amp;lt;"-shaped text (CodeQL #629 regression)', () => {
  // Unit test of the decoder itself, not the full escJsAttr round trip --
  // escJsAttr always runs its result through escHtml too, so it can never
  // hand htmlAttrDecode a *single*-layer-encoded "&amp;lt;" string to
  // decode (any literal '&' in the input always comes out double-wrapped,
  // e.g. "&amp;amp;lt;x"); that still round-trips correctly (see the
  // "round-trips a normal value" test below with an '&' in the value),
  // but it never exercises this decoder's own single-pass-vs-sequential
  // correctness directly, which is what CodeQL's finding was actually
  // about. A real single-pass HTML decoder, given "&amp;lt;x", decodes
  // only the leading &amp; -> & (consuming exactly those 5 characters,
  // never re-scanning its own output), leaving the result as "&lt;x" --
  // not further decoding that into "<x". Decoding &amp; before the other
  // entities (the original, buggy order) produced "<x" instead, exactly
  // the double-unescape CodeQL flagged.
  assert.equal(htmlAttrDecode('&amp;lt;x'), '&lt;x');
});

test('escJsAttr + htmlAttrDecode round-trips a value containing a literal "&" unchanged', () => {
  const sandbox = loadAppJS();
  const payload = '&amp;lt;x';
  const r = simulateInlineOnclick(sandbox, payload);
  assert.equal(r.captured, payload,
    `value did not round-trip correctly -- decoded: ${r.decodedAttrValue}`);
});

// SEC-021 (docs/plans/historical-plans/2026-08-28-security-assessment-
// core.md) -- the PWA's file viewer (_showFileViewer -> _renderMarkdownFileInto)
// has the exact same unsanitized marked.parse()-straight-into-innerHTML
// pattern diagrams.js's renderDoc had (see diagrams-xss.test.js's own
// SEC-021 tests for the full writeup). A file opened through the viewer
// containing e.g. <img onerror=...> would execute with the operator's
// own session. Fixed the same way: DOMPurify.sanitize() runs on marked's
// output before it's assigned to innerHTML. Fake marked/DOMPurify stubs
// here for the same reason as diagrams-xss.test.js's: this tests THIS
// file's integration logic, not DOMPurify's own sanitization, which
// isn't this codebase's to verify.
test('_renderMarkdownFileInto runs marked output through DOMPurify.sanitize before using it', () => {
  const sandbox = loadAppJS();
  vm.runInContext(`
    marked = { parse: () => '<img src=x onerror=alert(1)>RAW_MARKED_OUTPUT' };
    DOMPurify = { sanitize: (html) => { globalThis.__sanitizeCalledWith = html; return '<p>SANITIZED_OUTPUT</p>'; } };
    globalThis.__el = { innerHTML: '', querySelector: () => null };
  `, sandbox);
  vm.runInContext("_renderMarkdownFileInto(__el, '# hello')", sandbox);

  const calledWith = vm.runInContext('globalThis.__sanitizeCalledWith', sandbox);
  const innerHTML = vm.runInContext('__el.innerHTML', sandbox);
  assert.equal(calledWith, '<img src=x onerror=alert(1)>RAW_MARKED_OUTPUT',
    "DOMPurify.sanitize must be called with marked.parse()'s raw output");
  assert.ok(innerHTML.includes('SANITIZED_OUTPUT'),
    `expected the SANITIZED result in innerHTML, got: ${innerHTML}`);
  assert.ok(!innerHTML.includes('onerror=alert(1)'),
    `the raw, unsanitized marked output must never reach innerHTML, got: ${innerHTML}`);
});

test('_renderMarkdownFileInto falls back to escaped text when DOMPurify is unavailable', () => {
  const sandbox = loadAppJS();
  vm.runInContext(`
    marked = { parse: () => '<img src=x onerror=alert(1)>' };
    globalThis.__el = { innerHTML: '', querySelector: () => null };
  `, sandbox);
  vm.runInContext("_renderMarkdownFileInto(__el, '# hello')", sandbox);

  const innerHTML = vm.runInContext('__el.innerHTML', sandbox);
  assert.ok(!innerHTML.includes('<img src=x onerror='),
    `must not render marked's raw output just because the sanitizer failed to load, got: ${innerHTML}`);
});
