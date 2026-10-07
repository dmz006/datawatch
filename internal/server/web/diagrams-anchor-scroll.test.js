// Regression test for an iOS-reported bug (datawatch-app, 2026-10-07):
// "?" help links open the right doc page but never scroll to the
// section. Cause: iOS (Foundation URL / SFSafariViewController)
// percent-encodes the SECOND '#' in a link like
// #docs/memory.md#configuration, so location.hash arrives as
// "#docs/memory.md%23configuration". openFromHash() already decodes
// before splitting (why the page opens), but the scroll-to-anchor step
// in rewriteRelativeMdLinks() searched the RAW (still-encoded) hash for
// a second '#', found none, and silently skipped the scroll. Fixed by
// decoding first, matching openFromHash's own decode-then-split order.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, makeStubElement, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const DIAGRAMS_JS = path.join(__dirname, 'diagrams.js');

// Drives the real openDoc -> renderDoc -> rewriteRelativeMdLinks chain
// with a stubbed successful fetch, then reports which element id(s)
// document.getElementById was asked to resolve (the scroll-anchor step
// calls it with the decoded anchor id).
function loadAndCaptureGetElementByIdCalls(initialHash) {
  const mainEl = makeStubElement();
  // The prose root rewrite pass needs mainEl.querySelector('.prose') to
  // return something non-null, else it bails before reaching the
  // anchor-scroll step. Headings/anchors/imgs can stay empty -- this
  // test only cares whether the anchor id computed from the hash is
  // correct, not real heading-slug matching (covered separately by the
  // existing heading-id-assignment code path in normal use).
  const proseRoot = makeStubElement();
  mainEl.querySelector = (sel) => (sel === '.prose' ? proseRoot : null);

  const sandbox = buildSandbox({ initialHash, byId: { main: mainEl } });
  sandbox.fetch = () => Promise.resolve({ ok: true, text: () => Promise.resolve('# Test doc\n\nbody text') });
  // The real code defers the getElementById(anchorId) call inside
  // requestAnimationFrame; buildSandbox's stub never fires it (these
  // tests only need synchronous top-level code by default). Run it
  // immediately so the scroll step actually executes within this test.
  sandbox.requestAnimationFrame = (cb) => { cb(); return 0; };
  const idCalls = [];
  sandbox.document.getElementById = (id) => { idCalls.push(id); return byIdReturn(id); };
  function byIdReturn(id) { return id === 'main' ? mainEl : makeStubElement(); }

  vm.createContext(sandbox);
  loadScript(DIAGRAMS_JS, sandbox);
  return flushAsync().then(() => idCalls);
}

test('iOS-style double-percent-encoded hash (#docs/x.md%23anchor) still resolves the in-page anchor', async () => {
  const idCalls = await loadAndCaptureGetElementByIdCalls('#docs/test.md%23configuration');
  assert.ok(idCalls.includes('configuration'),
    `expected getElementById('configuration') to be called once the hash is decoded correctly; calls were: ${JSON.stringify(idCalls)}`);
});

test('a normal, non-encoded in-page anchor hash (#docs/x.md#anchor) still works (regression check)', async () => {
  const idCalls = await loadAndCaptureGetElementByIdCalls('#docs/test.md#configuration');
  assert.ok(idCalls.includes('configuration'),
    `expected getElementById('configuration') to be called for the already-literal '#' case too; calls were: ${JSON.stringify(idCalls)}`);
});

test('a hash with no in-page anchor does not attempt a spurious scroll', async () => {
  const idCalls = await loadAndCaptureGetElementByIdCalls('#docs/test.md');
  assert.ok(!idCalls.includes('configuration'),
    `expected no lookup for an anchor id that was never in the hash; calls were: ${JSON.stringify(idCalls)}`);
});
