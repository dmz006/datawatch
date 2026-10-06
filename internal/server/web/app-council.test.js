// GH#178 — GET /api/council/runs returns a bare array (same as
// /api/council/personas, "bare array for mobile client compat",
// internal/server/council.go), but loadCouncilPanel read rdata.runs,
// which is always undefined against a bare array, so Recent Runs was
// always empty. Confirmed the only call site hitting this exact bug
// (a near-identical pattern on /api/evals/runs is a separate, later
// follow-up, not this fix).

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript, flushAsync, makeStubElement } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

test('loadCouncilPanel renders Recent Runs from a bare-array /api/council/runs response', async () => {
  const panelEl = makeStubElement();
  const sandbox = buildSandbox({ byId: { councilPanel: panelEl } });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);

  const runs = [
    { id: 'run-aaaaaaaa-1111', mode: 'quick', personas: ['a', 'b'], rounds: [{}] },
  ];
  sandbox.fetch = (url) => {
    if (String(url).includes('/api/council/personas')) {
      return Promise.resolve({ ok: true, json: () => Promise.resolve([{ name: 'ux-advocate' }]) });
    }
    if (String(url).includes('/api/council/runs')) {
      // The real server response shape: a bare array, not {runs:[...]}.
      return Promise.resolve({ ok: true, json: () => Promise.resolve(runs) });
    }
    return Promise.resolve({ ok: false, status: 404 });
  };

  vm.runInContext('loadCouncilPanel()', sandbox);
  await flushAsync();

  assert.ok(panelEl.innerHTML.includes('run-aaa'.slice(0, 8)) || panelEl.innerHTML.includes('aaaaaaaa'),
    `expected the run's id to appear in the rendered panel, got: ${panelEl.innerHTML.slice(0, 500)}`);
});

test('loadCouncilPanel still handles an empty bare array (no runs yet)', async () => {
  const panelEl = makeStubElement();
  const sandbox = buildSandbox({ byId: { councilPanel: panelEl } });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);

  sandbox.fetch = (url) => {
    if (String(url).includes('/api/council/personas')) {
      return Promise.resolve({ ok: true, json: () => Promise.resolve([]) });
    }
    if (String(url).includes('/api/council/runs')) {
      return Promise.resolve({ ok: true, json: () => Promise.resolve([]) });
    }
    return Promise.resolve({ ok: false, status: 404 });
  };

  vm.runInContext('loadCouncilPanel()', sandbox);
  await flushAsync();

  // Must not throw and must not render a "Recent Runs" section with no data.
  assert.ok(!panelEl.innerHTML.includes('undefined'), `rendered panel should not contain "undefined", got: ${panelEl.innerHTML.slice(0, 300)}`);
});
