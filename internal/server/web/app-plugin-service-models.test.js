// app-plugin-service-models.test.js
//
// Settings → Plugins: the Model use panel rendered under a service
// plugin's card (window.pluginServiceModels* in app.js).
//
// Pins the behaviors that matter most:
//   - the per-model table renders each model with its Ollama node,
//     the memory it holds and the call count over the window — field
//     names as served by GET /api/plugins/{name}/models (model, node,
//     held_bytes, calls, calls_documented);
//   - the findings area is omitted entirely when the report carries no
//     findings — the panel never shows "none";
//   - a finding is shown as its sentence with its next action, and a
//     sentence containing markup is rendered escaped, not as markup;
//   - node-unreachable (a node that did not answer) is an Events entry,
//     not a panel row: it never appears in the table;
//   - pluginServiceModelsLoad GETs /api/plugins/{name}/models and
//     renders the panel into the anchor div.
//
// app.js is ~50k lines with many top-level side effects, so the tests
// run in the same jsdom-free browser sandbox as the rest of the app
// tests (testutil_browser_stub.js).

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  sandbox._i18n.bundle = en;
  sandbox._i18n.fallback = en;
  return sandbox;
}

function report() {
  return {
    plugin: 'enrich',
    nodes: [
      { name: 'gpu-a', served: ['qwen3:1.7b'], loaded: [{ name: 'qwen3:1.7b', ram_bytes: 64 * 1024 * 1024, vram_bytes: 1300 * 1024 * 1024, context_tokens: 32000 }] },
      { name: 'cpu-b', served: ['qwen3:1.7b'], loaded: [{ name: 'qwen3:1.7b', ram_bytes: 100 * 1024 * 1024, vram_bytes: 800 * 1024 * 1024, context_tokens: 4096 }] },
      { name: 'gpu-c', error: 'connection refused' },
    ],
    models: [
      { model: 'qwen3:1.7b', node: 'gpu-a', ram_bytes: 64 * 1024 * 1024, vram_bytes: 1300 * 1024 * 1024, held_bytes: 1364 * 1024 * 1024, context_tokens: 32000, calls: 41, calls_documented: true, used_by_service: true },
      { model: 'llama3.2:1b', node: 'cpu-b', ram_bytes: 0, vram_bytes: 600 * 1024 * 1024, held_bytes: 600 * 1024 * 1024, context_tokens: 4096, calls: 0, calls_documented: false, used_by_service: true },
    ],
    findings: [
      { id: 'small_model_large_context', model: 'qwen3:1.7b', node: 'gpu-a',
        sentence: 'qwen3:1.7b holds only 1.3 GB on gpu-a yet is loaded with a context of 32000 tokens — above the 8192-token context threshold for a model this small',
        next_action: 'give qwen3:1.7b a context the small model can hold, or route this workload to a larger model on another node' },
      { id: 'node_overloaded', model: 'qwen3:1.7b', node: 'gpu-a',
        sentence: 'gpu-a holds 1.3 GB of resident models — more than 2x the 900.0 MB on cpu-b, which also serves qwen3:1.7b',
        next_action: 'move qwen3:1.7b from gpu-a to cpu-b to relieve the busier node' },
    ],
    notes: ['node gpu-c did not answer: connection refused'],
  };
}

test('models panel renders the per-model table: model, node, memory held, calls', () => {
  const sandbox = loadApp();
  const html = sandbox.window.pluginServiceModelsPanelHTML('enrich', report());
  assert.match(html, /class="plugin-service-models"/);
  assert.match(html, /Model use/);
  // column headers
  assert.match(html, /Model/);
  assert.match(html, /Node/);
  assert.match(html, /Memory held/);
  assert.match(html, /Calls \(window\)/);
  // both models are listed with their node
  assert.match(html, /qwen3:1\.7b/);
  assert.match(html, /gpu-a/);
  assert.match(html, /llama3\.2:1b/);
  assert.match(html, /cpu-b/);
  // memory held is the report's held_bytes, rendered compactly
  assert.match(html, /1\.3 GB/);
  assert.match(html, /600\.0 MB/);
  // the call count over the window
  assert.match(html, /41/);
});

test('calls_documented=false renders "not documented", not a fake 0', () => {
  const sandbox = loadApp();
  const rep = report();
  rep.models = [
    { model: 'llama3.2:1b', node: 'cpu-b', held_bytes: 600 * 1024 * 1024, calls: 0, calls_documented: false, used_by_service: true },
    { model: 'qwen3:1.7b', node: 'gpu-a', held_bytes: 0, calls: 41, calls_documented: true, used_by_service: true },
  ];
  const html = sandbox.window.pluginServiceModelsPanelHTML('enrich', rep);
  assert.match(html, /not documented/);
  assert.doesNotMatch(html, /undefined/);
});

test('findings area is omitted entirely when there are no findings — never "none"', () => {
  const sandbox = loadApp();
  const rep = report();
  rep.findings = [];
  const html = sandbox.window.pluginServiceModelsPanelHTML('enrich', rep);
  // no findings area at all
  assert.doesNotMatch(html, /Findings/);
  assert.doesNotMatch(html, /Next action/);
  // a report with the findings field absent behaves the same
  delete rep.findings;
  const html2 = sandbox.window.pluginServiceModelsPanelHTML('enrich', rep);
  assert.doesNotMatch(html2, /Findings/);
  // null payload: still no findings area, no broken row
  const html3 = sandbox.window.pluginServiceModelsPanelHTML('enrich', null);
  assert.doesNotMatch(html3, /Findings/);
  assert.doesNotMatch(html3, /undefined/);
});

test('each finding renders as its sentence with its next action', () => {
  const sandbox = loadApp();
  const html = sandbox.window.pluginServiceModelsPanelHTML('enrich', report());
  assert.match(html, /Findings/);
  // both finding sentences are present …
  assert.match(html, /holds only 1\.3 GB on gpu-a yet is loaded with a context of 32000 tokens/);
  assert.match(html, /more than 2x the 900\.0 MB on cpu-b, which also serves qwen3:1\.7b/);
  // … each with its next action
  assert.match(html, /Next action: give qwen3:1\.7b a context the small model can hold/);
  assert.match(html, /Next action: move qwen3:1\.7b from gpu-a to cpu-b to relieve the busier node/);
});

test('a finding sentence containing markup is rendered escaped, not as markup', () => {
  const sandbox = loadApp();
  const rep = {
    plugin: 'enrich',
    models: [],
    findings: [{
      id: 'injected',
      sentence: '<script>window.__poked=1</script> qwen3:1.7b holds memory',
      next_action: '<img src=x onerror="window.__poked=1"> move the model',
    }],
  };
  const html = sandbox.window.pluginServiceModelsPanelHTML('enrich', rep);
  // the raw tags must not survive into the rendered HTML …
  assert.doesNotMatch(html, /<script>window\.__poked=1<\/script>/);
  assert.doesNotMatch(html, /<img src=x/);
  // … they are inert escaped text instead
  assert.match(html, /&lt;script&gt;window\.__poked=1&lt;\/script&gt;/);
  assert.match(html, /&lt;img src=x onerror=&quot;window\.__poked=1&quot;&gt;/);
  // the plain parts of the same finding still render
  assert.match(html, /Next action: /);
  assert.match(html, /move the model/);
});

test('node-unreachable is an Events entry, not a panel row', () => {
  const sandbox = loadApp();
  const rep = report();
  const html = sandbox.window.pluginServiceModelsPanelHTML('enrich', rep);
  // the unreachable node never appears as a table row …
  assert.doesNotMatch(html, /gpu-c/);
  // … and its "did not answer" note from the report is not rendered either
  assert.doesNotMatch(html, /did not answer/);
  // a model the service calls but no answering node serves gets a row
  // with an empty node cell (its reachability goes to Events, not here)
  const rep2 = {
    plugin: 'enrich',
    models: [{ model: 'mistral:7b', node: '', held_bytes: 0, calls: 0, calls_documented: true, used_by_service: true }],
    findings: [],
  };
  const html2 = sandbox.window.pluginServiceModelsPanelHTML('enrich', rep2);
  assert.match(html2, /mistral:7b/);
  assert.doesNotMatch(html2, /Node>undefined/);
});

test('pluginServiceModelsLoad GETs /api/plugins/{name}/models and renders into the anchor', async () => {
  const sandbox = loadApp();
  const calls = [];
  sandbox.apiFetch = function (url, opts) {
    calls.push({ url, opts: opts || {} });
    return Promise.resolve(report());
  };
  // document in the sandbox: getElementById returns a sink object
  const sink = { innerHTML: '' };
  sandbox.document.getElementById = (id) => (id === 'panelAnchor' ? sink : null);

  sandbox.window.pluginServiceModelsLoad('enrich', 'panelAnchor');
  await flushAsync();
  assert.equal(calls.length, 1);
  assert.match(calls[0].url, /\/api\/plugins\/enrich\/models$/);
  assert.match(sink.innerHTML, /class="plugin-service-models"/);
  assert.match(sink.innerHTML, /41/);
});
