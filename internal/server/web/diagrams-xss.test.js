// BL394 security review (docs/plans/2026-10-03-bl394-security-findings-review.md
// §3h) — regression test for the reflected XSS fix in diagrams.js. Uses
// Node's built-in test runner (node:test). Run:
// `node --test internal/server/web/*.test.js`.
//
// What was wrong (fixed in this same commit): openDoc(path) interpolated
// an attacker-controlled path (from decodeURIComponent(location.hash))
// into innerHTML unescaped, in two places (the "Loading..." message and,
// more reliably reachable in practice, the "Failed to load" error
// message once the fetch for a nonexistent crafted path 404s). Three
// more unescaped spots were found in renderDoc (reachable only if a real
// file existed at the crafted path -- fixed anyway, including one inside
// an href="..." attribute, where the exploitable character is a literal
// " rather than <).

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, makeStubElement, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const DIAGRAMS_JS = path.join(__dirname, 'diagrams.js');

function loadAndWait(initialHash) {
  const mainCaptures = [];
  const mainEl = makeStubElement((html) => mainCaptures.push(html));
  const sandbox = buildSandbox({ initialHash, byId: { main: mainEl } });
  vm.createContext(sandbox);
  loadScript(DIAGRAMS_JS, sandbox);
  // openFromHash() -> openDoc() is async (awaits fetch); diagrams.js
  // calls openFromHash() at its own top level, so loading the file IS
  // the trigger. Flush enough microtasks/macrotasks for that chain to
  // settle before inspecting what landed in #main.
  return flushAsync().then(() => mainCaptures.join('\n---\n'));
}

test('a crafted location.hash payload is escaped before reaching #main.innerHTML', async () => {
  const payload = '<img src=x onerror=alert(document.cookie)>';
  // Must start with "docs/" and end with ".md" to pass openFromHash's
  // own gate -- confirms the gate doesn't actually block the attack, it
  // just requires this exact wrapping.
  const maliciousPath = 'docs/' + payload + '.md';
  const hash = '#' + encodeURIComponent(maliciousPath).replace(/%2F/g, '/'); // keep slashes literal, like a real link would

  const rendered = await loadAndWait(hash);
  assert.ok(!rendered.includes('<img src=x onerror='),
    'the raw, unescaped <img onerror=...> payload reached #main.innerHTML verbatim');
  assert.ok(rendered.includes('&lt;img'),
    `expected the escaped form (&lt;img ...) to appear instead -- got: ${JSON.stringify(rendered).slice(0, 300)}`);
});

test('a normal legitimate doc path still renders (regression check)', async () => {
  const rendered = await loadAndWait('#docs/howto/README.md');
  assert.ok(rendered.includes('docs/howto/README.md') || rendered.includes('Loading'),
    `expected a normal legitimate doc path to render a loading/error state: ${JSON.stringify(rendered).slice(0, 300)}`);
});

// SEC-021 (docs/plans/historical-plans/2026-08-28-security-assessment-
// core.md) -- renderDoc's own prose rendering runs marked.parse() (which
// passes raw HTML embedded in the .md source through unchanged -- that's
// standard markdown behavior, marked does no sanitization itself)
// straight into innerHTML with no sanitization at all. A doc containing
// e.g. <img onerror=...> executed in the PWA with the operator's own
// session. Fixed by running marked's output through DOMPurify.sanitize()
// first. These tests use fake marked/DOMPurify stubs rather than the
// real libraries (no npm dependency exists for this PWA, see
// testutil_browser_stub.js's own header comment) -- the thing actually
// at risk of a coding mistake is THIS file's own integration logic
// (does it call sanitize, does it use sanitize's result rather than
// marked's raw output), not DOMPurify's own sanitization correctness,
// which is an extensively-tested third-party library's job to get right,
// not this test's.
async function loadWithFakeMarkdownLibs(markedOutput, domPurifyOutput) {
  const mainEl = makeStubElement();
  const sandbox = buildSandbox({ byId: { main: mainEl } });
  sandbox.marked = { parse: () => markedOutput };
  if (domPurifyOutput !== undefined) {
    sandbox.DOMPurify = { sanitize: (html) => { sandbox.__sanitizeCalledWith = html; return domPurifyOutput; } };
  }
  vm.createContext(sandbox);
  loadScript(DIAGRAMS_JS, sandbox);
  // The file's own top-level openFromHash() call (with no hash set)
  // kicks off an unrelated async fetch/404 chain that also writes to
  // #main -- let it finish before the test does its own direct
  // renderDoc() call, or the two can race and clobber each other.
  await flushAsync();
  return { sandbox, mainEl };
}

test('renderDoc runs marked output through DOMPurify.sanitize before using it', async () => {
  const { sandbox, mainEl } = await loadWithFakeMarkdownLibs(
    '<img src=x onerror=alert(1)>RAW_MARKED_OUTPUT',
    '<p>SANITIZED_OUTPUT</p>'
  );
  await vm.runInContext("renderDoc('docs/test.md', '# hello')", sandbox);

  assert.equal(sandbox.__sanitizeCalledWith, '<img src=x onerror=alert(1)>RAW_MARKED_OUTPUT',
    'DOMPurify.sanitize must be called with marked.parse()\'s raw output');
  assert.ok(mainEl.innerHTML.includes('SANITIZED_OUTPUT'),
    `expected the SANITIZED result in innerHTML, got: ${mainEl.innerHTML.slice(0, 300)}`);
  assert.ok(!mainEl.innerHTML.includes('onerror=alert(1)'),
    `the raw, unsanitized marked output must never reach innerHTML, got: ${mainEl.innerHTML.slice(0, 300)}`);
});

test('renderDoc falls back to escaped text, not raw marked output, when DOMPurify is unavailable', async () => {
  const { sandbox, mainEl } = await loadWithFakeMarkdownLibs(
    '<img src=x onerror=alert(1)>', undefined // DOMPurify left undefined
  );
  await vm.runInContext("renderDoc('docs/test.md', '# hello')", sandbox);

  assert.ok(!mainEl.innerHTML.includes('<img src=x onerror='),
    `must not render marked's raw output just because the sanitizer failed to load, got: ${mainEl.innerHTML.slice(0, 300)}`);
});
