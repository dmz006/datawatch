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
