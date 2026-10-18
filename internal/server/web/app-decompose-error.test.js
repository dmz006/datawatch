// Decompose failure visibility on the PRD card — the persistent
// last_decompose_failure line must be escaped and must not render when
// there is no failure to show.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox({});
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  return sandbox;
}

function call(sandbox, fnName, ...args) {
  sandbox.__args = args;
  const result = vm.runInContext(`${fnName}(...__args)`, sandbox);
  delete sandbox.__args;
  return result;
}

test('decompose failure line escapes a </script> payload in the detail', () => {
  const sandbox = loadAppJS();
  const prd = {
    id: 'p1',
    status: 'draft',
    last_decompose_failure: { category: 'infra', detail: 'bad </script><img src=x onerror=alert(1)>' },
  };
  const html = call(sandbox, 'renderPRDDecomposeFailure', prd);
  assert.match(html, /prd-decompose-failure/);
  assert.match(html, /&lt;\/script&gt;&lt;img src=x onerror=alert\(1\)&gt;/);
  assert.doesNotMatch(html, /<\/script>/);
  assert.doesNotMatch(html, /<img/);
});

test('decompose failure line is omitted when last_decompose_failure is absent', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, 'renderPRDDecomposeFailure', { id: 'p2', status: 'draft' });
  assert.equal(html, '');
});

test('decompose failure line is omitted when the failure detail is empty', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, 'renderPRDDecomposeFailure', {
    id: 'p3',
    status: 'draft',
    last_decompose_failure: { category: 'scope', detail: '' },
  });
  assert.equal(html, '');
});
