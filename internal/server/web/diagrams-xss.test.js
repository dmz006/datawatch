#!/usr/bin/env node
// BL394 security review (docs/plans/2026-10-03-bl394-security-findings-review.md
// §3h) — regression test for the reflected XSS fix in diagrams.js.
//
// Same approach as app_security_test.js (no JS test framework exists for
// this PWA): loads the real, unmodified diagrams.js into a Node vm
// context with a minimally-stubbed browser environment, sets
// location.hash to a crafted malicious value BEFORE loading (diagrams.js
// calls openFromHash() at its own top level, so loading the file IS the
// trigger -- no extra step needed to reach the vulnerable code), and
// inspects what actually got written to the #main element's innerHTML.
//
// What was wrong (fixed in this same commit): openDoc(path) interpolated
// an attacker-controlled path (from decodeURIComponent(location.hash))
// into innerHTML unescaped, in two places (the "Loading..." message and,
// more reliably reachable in practice, the "Failed to load" error
// message once the fetch for a nonexistent crafted path 404s). Three
// more unescaped spots were found in renderDoc (reachable only if a real
// file existed at the crafted path -- fixed anyway, including one inside
// an href="..." attribute, where the exploitable character is a literal
// " rather than <). Run: `node diagrams_security_test.js` (exit 0 = pass).

const vm = require('vm');
const fs = require('fs');
const path = require('path');

function makeStubElement(onSetInnerHTML) {
  const el = {
    style: {},
    classList: { add() {}, remove() {}, toggle() {}, contains() { return false; } },
    dataset: {},
    addEventListener() {}, removeEventListener() {},
    appendChild() {}, removeChild() {}, remove() {},
    querySelector() { return null; }, querySelectorAll() { return []; },
    setAttribute() {}, getAttribute() { return null; },
  };
  let html = '';
  Object.defineProperty(el, 'innerHTML', {
    get() { return html; },
    set(v) { html = v; if (onSetInnerHTML) onSetInnerHTML(v); },
  });
  return el;
}

function buildSandbox(initialHash) {
  const sandbox = {};
  sandbox.window = sandbox;
  sandbox.self = sandbox;
  sandbox.globalThis = sandbox;
  sandbox.console = console;
  sandbox.setTimeout = (fn) => { /* no-op: never actually schedule */ return 0; };
  sandbox.clearTimeout = () => {};
  sandbox.addEventListener = function () {};
  sandbox.removeEventListener = function () {};
  sandbox.matchMedia = () => ({ matches: false, addEventListener() {}, addListener() {} });
  sandbox.mermaid = { initialize() {}, render() { return Promise.reject(new Error('stub: no mermaid render in this test')); } };
  // marked left undefined on purpose -- renderDoc's own `typeof marked
  // !== 'undefined'` guard exercises the plain <pre> fallback path, which
  // is what's actually relevant here (the title/path header, not the
  // markdown body rendering).
  sandbox.location = { hash: initialHash || '' };
  sandbox.history = { pushState() {}, replaceState() {} };
  sandbox.encodeURIComponent = encodeURIComponent;
  sandbox.decodeURIComponent = decodeURIComponent;

  const mainCaptures = [];
  const mainEl = makeStubElement((html) => mainCaptures.push(html));
  const byId = {
    bodyEl: makeStubElement(),
    asideToggle: makeStubElement(),
    list: makeStubElement(),
    main: mainEl,
    filter: makeStubElement(),
  };
  sandbox.document = {
    getElementById(id) { return byId[id] || makeStubElement(); },
    querySelectorAll() { return []; },
    querySelector() { return null; },
    createElement() { return makeStubElement(); },
    addEventListener() {},
  };

  // fetch: simulate "no such file" for anything -- the realistic case
  // for an attacker-crafted path, and the path that actually reaches the
  // vulnerable error-message line in practice.
  sandbox.fetch = () => Promise.resolve({ ok: false, status: 404 });

  sandbox.__mainCaptures = mainCaptures;
  return sandbox;
}

function loadAndWait(diagramsJsPath, initialHash) {
  const src = fs.readFileSync(diagramsJsPath, 'utf8');
  const sandbox = buildSandbox(initialHash);
  vm.createContext(sandbox);
  new vm.Script(src, { filename: diagramsJsPath }).runInContext(sandbox);
  // openFromHash() -> openDoc() is async (awaits fetch). Flush enough
  // microtasks/macrotasks for that chain to settle before inspecting
  // what landed in #main. A handful of setImmediate round-trips is
  // enough for a couple of chained `await`s.
  return new Promise((resolve) => {
    let n = 0;
    const tick = () => {
      if (++n >= 10) return resolve(sandbox);
      setImmediate(tick);
    };
    tick();
  });
}

async function main() {
  const diagramsJsPath = process.argv[2] || path.join(__dirname, 'diagrams.js');
  const payload = '<img src=x onerror=alert(document.cookie)>';
  // Must start with "docs/" and end with ".md" to pass openFromHash's
  // own gate -- confirms the gate doesn't actually block the attack, it
  // just requires this exact wrapping.
  const maliciousPath = 'docs/' + payload + '.md';
  const hash = '#' + encodeURIComponent(maliciousPath).replace(/%2F/g, '/'); // keep slashes literal, like a real link would

  const sandbox = await loadAndWait(diagramsJsPath, hash);
  const rendered = sandbox.__mainCaptures.join('\n---\n');

  const failures = [];
  if (rendered.includes('<img src=x onerror=')) {
    failures.push('the raw, unescaped <img onerror=...> payload reached #main.innerHTML verbatim');
  }
  if (!rendered.includes('&lt;img')) {
    failures.push('expected the escaped form (&lt;img ...) to appear instead -- got: ' + JSON.stringify(rendered).slice(0, 300));
  }

  // Regression check: a normal, legitimate doc path must still render
  // its loading state without throwing.
  const sandbox2 = await loadAndWait(diagramsJsPath, '#docs/howto/README.md');
  const rendered2 = sandbox2.__mainCaptures.join('\n---\n');
  if (!rendered2.includes('docs/howto/README.md') && !rendered2.includes('Loading')) {
    failures.push('a normal legitimate doc path did not render the expected loading/error state: ' + JSON.stringify(rendered2).slice(0, 300));
  }

  if (failures.length) {
    console.error('FAIL');
    for (const f of failures) console.error('  - ' + f);
    process.exitCode = 1;
    return;
  }
  console.log('PASS: crafted hash payload is escaped before reaching innerHTML, legitimate paths still render');
}

main();
