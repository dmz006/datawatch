// Edit-task dialog — verification script and exemption reason inputs. fetch is
// stubbed and records every request.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

function loadApp(byId) {
  const sandbox = buildSandbox({ byId });
  const calls = [];
  sandbox.fetch = (url, opts) => {
    calls.push({ url, method: (opts && opts.method) || 'GET', body: opts && opts.body ? JSON.parse(opts.body) : undefined });
    return Promise.resolve({
      ok: true, status: 200, statusText: '200',
      json: () => Promise.resolve({}),
      text: () => Promise.resolve('{}'),
    });
  };
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  sandbox.__calls = calls;
  sandbox.showToast = () => {};
  return sandbox;
}

function elements() {
  return {
    prdEditSpec: { value: 'same spec' }, prdEditBackend: { value: '' }, prdEditEffort: { value: '' },
    prdEditModelInner: makeStubElement(), prdEditProjectDir: { value: '' },
    prdEditReadOnly: { checked: false },
    prdEditVerifScript: { value: 'scripts/check.sh' }, prdEditVerifScriptReason: { value: 'docs only' },
    prdEditVerifBackend: { value: '' }, prdEditVerifEffort: { value: '' }, prdEditVerifModelInner: makeStubElement(),
  };
}

async function openDialog(els) {
  const sb = loadApp(els);
  vm.runInContext('_prdMountModal = function (h, f) { __mounted = { h, f }; }; ensureLLMModelLists = function () { return Promise.resolve(); }; refreshLLMModelField = function () {};', sb);
  sb.openPRDEditTaskModal('p1', 't1', 'same spec', '', '', '', { verification_script: 'scripts/check.sh', verification_script_reason: 'docs only' }, '', false, '');
  await new Promise(r => setImmediate(r));
  return { sb, mounted: vm.runInContext('__mounted', sb) };
}

const settle = () => new Promise(r => setImmediate(r));

test('the dialog renders both inputs prefilled, with the hint under the script input', async () => {
  const { mounted } = await openDialog(elements());
  assert.match(mounted.h, /id="prdEditVerifScript"[^>]*value="scripts\/check.sh"/);
  assert.match(mounted.h, /id="prdEditVerifScriptReason"[^>]*value="docs only"/);
  assert.match(mounted.h, /repo-relative script file/);
  assert.match(mounted.h, /Automaton/);
});

test('an unchanged dialog sends no edit_task request', async () => {
  const { sb, mounted } = await openDialog(elements());
  mounted.f();
  await settle();
  assert.equal(sb.__calls.some(c => c.url.endsWith('/edit_task')), false);
});

test('only the changed key is sent', async () => {
  const els = elements();
  const { sb, mounted } = await openDialog(els);
  els.prdEditVerifScript.value = '  scripts/new.sh ';
  mounted.f();
  await settle();
  const edit = sb.__calls.find(c => c.url.endsWith('/edit_task'));
  assert.ok(edit, 'edit_task was not called');
  assert.equal(edit.body.verification_script, 'scripts/new.sh');
  assert.equal('verification_script_reason' in edit.body, false);
  assert.equal('new_spec' in edit.body, false);

  els.prdEditVerifScript.value = 'scripts/check.sh';
  els.prdEditVerifScriptReason.value = 'tests only';
  sb.__calls.length = 0;
  mounted.f();
  await settle();
  const second = sb.__calls.find(c => c.url.endsWith('/edit_task'));
  assert.equal(second.body.verification_script_reason, 'tests only');
  assert.equal('verification_script' in second.body, false);
});
