'use strict';
// CodeQL alert regression tests -- internal/server/web/app.js
//
//   alert 849  js/incomplete-multi-character-sanitization
//     _projectsSectionCard derived its plain-text title with a single
//     pass of the multi-character pattern /<[^>]*>/g. CodeQL flags
//     a single pass of a multi-character sanitizer as an incomplete
//     sanitizer; the fixed-point form (strip until no tag-shaped span
//     remains) is the rule's accepted shape. The two forms are
//     behavior-identical for this pattern (one left-to-right pass already
//     leaves no surviving '<' with a surviving '>' after it; verified
//     exhaustively, zero differences), and the result is additionally
//     escHtml()-escaped at every insertion point, so operator-visible
//     output is unchanged. Fixed by _stripTagsStable.
//
//   alert 850  js/xss-through-dom
//     `fBody.innerHTML = html;` in formEditToggleYaml -- the html built by
//     buildComputeNodeForm / buildLLMForm interpolated several record
//     fields (parsed from the operator YAML textarea) into value="..."
//     attributes with no escaping, so a `"` in the value broke out of the
//     attribute and injected live markup/handlers. Fixed by wrapping those
//     interpolations in the existing escHtml() helper. The inline
//     JS-literal case (openOllamaMarketplace('${n.name}') in an onclick
//     attribute) needs escJsAttr: escHtml alone is defeated by the HTML
//     attribute decode step (BL394 §3h), while escJsAttr's backslash
//     escaping survives the decode-then-compile cycle.
//
// The security tests below fail on the pre-fix code and pass on the
// fixed code (verified: 9 of 12 fail on the pre-fix tree; the three that
// pass on both sides are the operator-visible behavior-parity pins).
//
// Run: node --test internal/server/web/app-codeql-dom.test.js

const { test } = require('node:test');
const assert = require('node:assert');
const vm = require('vm');
const { buildSandbox, loadScript } = require('./testutil_browser_stub.js');

const APP_JS = require('path').join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  return sandbox;
}
function callFn(sandbox, name, ...args) {
  if (typeof sandbox[name] !== 'function')
    throw new Error(`app.js does not expose top-level function ${name}`);
  return sandbox[name](...args);
}
function callStr(sandbox, name, ...args) {
  const r = callFn(sandbox, name, ...args);
  if (typeof r !== 'string')
    throw new Error(`${name}() returned ${typeof r}, expected string`);
  return r;
}
// htmlAttrDecode approximates a browser's HTML entity decode of an attribute
// value: each of the five entities escHtml emits is decoded by one plain
// split/join sweep, in the same fixed order (a sweep only ever turns an
// entity into plain text, so it can never re-form a new one).
function htmlAttrDecode(s) {
  let out = String(s);
  for (const [ent, ch] of [['&amp;', '&'], ['&lt;', '<'], ['&gt;', '>'], ['&quot;', '"'], ['&#39;', "'"]]) {
    out = out.split(ent).join(ch);
  }
  return out;
}
// attrRawValue returns the RAW (pre-decode) attribute body between
// value=" and the next raw quote for input#id. If the value had not been
// escaped, the body would end early and the rest would appear as live
// attributes/markup in the raw HTML -- which the tests assert against
// separately with a / onmouseover="/ check.
function attrRawValue(rendered, id) {
  const m = new RegExp(`id="${id}"[^>]*?value="([^"]*)"`, 's').exec(rendered);
  if (!m) throw new Error(`no input#... ${id} value="..." found in: ${rendered}`);
  return m[1];
}

// ---------------------------------------------------------------------------
// alert 849 -- single-pass tag stripping (incomplete-multi-character-sanitization)
// ---------------------------------------------------------------------------

test('849: _stripTagsStable leaves no live tag after "<scr<script>ipt>"', () => {
  const sandbox = loadAppJS();
  const out = callStr(sandbox, '_stripTagsStable', '<scr<script>ipt>alert(1)</scr</script>ipt>');
  assert.ok(!/<[^>]*>/.test(out), `a live tag-shaped span survived: ${JSON.stringify(out)}`);
  assert.ok(!out.includes('<script'), `live <script leaked: ${JSON.stringify(out)}`);
});

test('849: _stripTagsStable leaves no live tag after "<img src=x onerror=...>"', () => {
  const sandbox = loadAppJS();
  const out = callStr(sandbox, '_stripTagsStable', '<img src=x onerror=alert(1)>');
  assert.ok(!/<[^>]*>/.test(out), `a live tag survived: ${JSON.stringify(out)}`);
});

test('849: _stripTagsStable keeps the old behavior on ordinary escaped input', () => {
  const sandbox = loadAppJS();
  assert.equal(callStr(sandbox, '_stripTagsStable', 'hello <b>world</b>'), 'hello world');
  assert.equal(callStr(sandbox, '_stripTagsStable', 'plain text'), 'plain text');
  assert.equal(callStr(sandbox, '_stripTagsStable', ''), '');
  assert.equal(callStr(sandbox, '_stripTagsStable', null), '');
  // A lone "<" with no ">" must terminate (not loop forever) and stay intact.
  assert.equal(callStr(sandbox, '_stripTagsStable', 'a < b'), 'a < b');
});

test('849: _projectsSectionCard output contains no live tag for a hostile title', () => {
  const sandbox = loadAppJS();
  const card = callStr(sandbox, '_projectsSectionCard', 'sec1',
    '<scr<script>ipt>alert(1)</scr</script>ipt>', '<div>body</div>');
  // The data-title text carries no tag-shaped span, escaped or not; the
  // escHtml() at the insertion point makes the raw card HTML inert either
  // way.
  const title = /data-title="([^"]*)"/.exec(card);
  assert.ok(title, 'card must carry a data-title attribute');
  assert.ok(!/<\/?script/.test(title[1]), `data-title carries a live script tag: ${JSON.stringify(title[1])}`);
  assert.ok(!/<[^>]*>/.test(htmlAttrDecode(title[1])),
    `title text still tag-shaped after HTML decode: ${JSON.stringify(htmlAttrDecode(title[1]))}`);
});

// ---------------------------------------------------------------------------
// alert 850 -- unescaped values into the form-edit innerHTML (xss-through-dom)
// ---------------------------------------------------------------------------

const XSS_IN_ATTR = '1" onmouseover="alert(1)';

test('850: buildComputeNodeForm escapes a quote-breakout value', () => {
  const sandbox = loadAppJS();
  const html = callStr(sandbox, 'buildComputeNodeForm', {
    name: 'gpu-1', kind: 'ollama', address: 'https://gpu-1:11434',
    max_concurrent_sessions: XSS_IN_ATTR,
    session_slots: XSS_IN_ATTR,
    verifier_slots: XSS_IN_ATTR,
    verifier_min_free_mem_mb: XSS_IN_ATTR,
    verifier_context_mb: XSS_IN_ATTR,
    scheduling_priority: XSS_IN_ATTR,
  });
  assert.ok(!/ onmouseover="/.test(html), 'raw injected onmouseover=" attribute present in rendered form');
  assert.ok(html.includes('onmouseover=&quot;'), 'payload should be entity-escaped, not stripped');
  for (const id of ['fe_compute_max_sessions', 'fe_compute_session_slots', 'fe_verifier_slots',
    'fe_verifier_min_free_mem_mb', 'fe_verifier_context_mb', 'fe_compute_priority']) {
    const raw = attrRawValue(html, id);
    assert.equal(htmlAttrDecode(raw), XSS_IN_ATTR, `input#${id} value did not round-trip its (escaped) payload: ${JSON.stringify(raw)}`);
  }
});

test('850: buildComputeNodeForm escapes an <img> payload in a numeric field', () => {
  const sandbox = loadAppJS();
  const html = callStr(sandbox, 'buildComputeNodeForm', {
    name: 'gpu-1', kind: 'ollama', address: 'https://gpu-1:11434',
    hardware: { gpu_count: '<img src=x onerror=alert(1)>' },
    max_concurrent_sessions: '<img src=x onerror=alert(1)>',
  });
  assert.ok(!/<img /.test(html), 'live <img tag escaped into the form HTML');
  assert.ok(html.includes('&lt;img'), 'payload should be entity-escaped, not stripped');
});

test('850: buildComputeNodeForm keeps normal values visible unchanged', () => {
  const sandbox = loadAppJS();
  const html = callStr(sandbox, 'buildComputeNodeForm', {
    name: 'gpu-1', kind: 'ollama', address: 'https://gpu-1:11434',
    hardware: { gpu_count: 2, memory_gb: 64, cpu_cores: 8 },
    declared_capacity: { max_concurrent_models: 4 },
    max_concurrent_sessions: 16,
    session_slots: 8,
    verifier_slots: 2,
    verifier_min_free_mem_mb: null,
    verifier_context_mb: null,
    scheduling_priority: 75,
  });
  assert.ok(html.includes('value="2"'), 'gpu_count round-trip lost');
  assert.ok(html.includes('value="64"'), 'memory_gb round-trip lost');
  assert.ok(html.includes('value="8"'), 'cpu_cores round-trip lost');
  assert.ok(html.includes('value="4"'), 'max_concurrent_models round-trip lost');
  assert.ok(html.includes('value="16"'), 'max_concurrent_sessions round-trip lost');
  assert.ok(html.includes('value="8"'), 'session_slots round-trip lost');
  assert.ok(html.includes('value="2"'), 'verifier_slots round-trip lost');
  assert.ok(html.includes('value="75"'), 'scheduling_priority round-trip lost');
});

test('850: openOllamaMarketplace onclick is escJsAttr-escaped (decode-then-compile safe)', () => {
  const sandbox = loadAppJS();
  // A name that breaks out of a single-quoted JS literal: if only
  // escHtml() had been applied, the browser's HTML attribute decode turns
  // the &#39; back into a raw quote before the handler compiles.
  const hostileName = "');alert(1);//";
  const html = callStr(sandbox, 'buildComputeNodeForm', {
    name: hostileName, kind: 'ollama', address: 'https://gpu-1:11434',
  });
  const m = /onclick="openOllamaMarketplace\('([^"]*)'\)"/.exec(html);
  assert.ok(m, 'marketplace button must be rendered for an ollama node');
  const decoded = htmlAttrDecode(m[1]);
  // The expected escape is a backslash immediately followed by a single
  // quote; build that two-character needle from char codes so this test
  // source itself carries no escape sequences.
  const backslash = String.fromCharCode(92);
  const quote = String.fromCharCode(39);
  const escapedQuote = backslash + quote;
  assert.ok(decoded.includes(escapedQuote),
    `the quote in the JS literal must be backslash-escaped (escJsAttr): ${JSON.stringify(decoded)}`);
  assert.ok(!decoded.split(escapedQuote).join('').includes(quote),
    `decoded JS string is still breakable (escHtml-level escaping only): ${JSON.stringify(decoded)}`);
});

test('850: buildLLMForm escapes a quote-breakout value', () => {
  const sandbox = loadAppJS();
  const html = callStr(sandbox, 'buildLLMForm', {
    kind: 'openai', model: 'm',
    timeout_seconds: XSS_IN_ATTR,
    max_inflight: XSS_IN_ATTR,
  });
  assert.ok(!/ onmouseover="/.test(html), 'raw injected onmouseover=" attribute present in rendered form');
  assert.ok(html.includes('onmouseover=&quot;'), 'payload should be entity-escaped, not stripped');
  for (const id of ['fe_llm_timeout', 'fe_llm_max_inflight']) {
    assert.equal(htmlAttrDecode(attrRawValue(html, id)), XSS_IN_ATTR, `input#${id} value did not round-trip its (escaped) payload`);
  }
});

test('850: buildLLMForm keeps normal values visible unchanged', () => {
  const sandbox = loadAppJS();
  const html = callStr(sandbox, 'buildLLMForm', {
    kind: 'openai', model: 'm', timeout_seconds: 60, max_inflight: 3,
  });
  assert.ok(html.includes('value="60"'), 'timeout_seconds round-trip lost');
  assert.ok(html.includes('value="3"'), 'max_inflight round-trip lost');
});

// The exact alert-850 sink: formEditToggleYaml writes the rendered form
// into fBody.innerHTML from JSON parsed out of the operator YAML textarea.
// This end-to-end test pins the whole path, not just the builder function.
function runFormEditToggle(kindTitle, record) {
  const formBody = { innerHTML: '', style: {} };
  const textarea = { value: JSON.stringify(record) };
  const byId = {
    formEditFormBody: formBody,
    formEditYamlBody: { style: {} },
    formEditYamlTextarea: textarea,
  };
  const { buildSandbox, loadScript } = require('./testutil_browser_stub.js');
  const sandbox = buildSandbox({ byId });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._formEditKindCfg = { title: kindTitle };
  callFn(sandbox, 'formEditToggleYaml');
  return formBody.innerHTML;
}

test('850: formEditToggleYaml sink renders a hostile Compute Node record safely', () => {
  const html = runFormEditToggle('Compute Node', {
    name: 'gpu-1', kind: 'ollama', address: 'https://gpu-1:11434',
    max_concurrent_sessions: XSS_IN_ATTR,
    session_slots: XSS_IN_ATTR,
    verifier_slots: XSS_IN_ATTR,
  });
  assert.ok(html.length > 0, 'form body was not rendered');
  assert.ok(html.includes('fe_compute_max_sessions'), 'expected the compute form in the sink output');
  assert.ok(!/ onmouseover="/.test(html), 'raw injected onmouseover=" attribute reached the innerHTML sink');
  assert.ok(!/<script/.test(html), 'live <script reached the innerHTML sink');
});

test('850: formEditToggleYaml sink renders a hostile LLM record safely', () => {
  const html = runFormEditToggle('LLM', {
    kind: 'openai', model: 'm',
    timeout_seconds: XSS_IN_ATTR,
    max_inflight: XSS_IN_ATTR,
  });
  assert.ok(html.length > 0, 'form body was not rendered');
  assert.ok(html.includes('fe_llm_timeout'), 'expected the LLM form in the sink output');
  assert.ok(!/ onmouseover="/.test(html), 'raw injected onmouseover=" attribute reached the innerHTML sink');
});
