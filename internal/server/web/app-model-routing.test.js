// Model routing — PWA side.
//
// The compute node view shows a Served models block per node
// (window.servedModelsNodeHtml), the task and story editors carry a Pin to node
// select, and a pinned row shows a badge. The Autonomous settings group carries
// the six autonomous.model_routing.* fields, DOCS_LINKS_BY_KEY maps the card and
// each config key to its glossary anchor, and every model_routing_ and
// settings_model_routing_ locale key is pinned in all five bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const HOSTILE = '<img src=x onerror=alert(1)>';
const DEFS = 'datawatch-definitions.md';
const VIEW_PREFIX = 'model_routing_';
const SETTINGS_PREFIX = 'settings_model_routing_';
const CONFIG_PREFIX = 'autonomous.model_routing.';

const ANCHORS = {
  model_routing_served_models_title: DEFS + '#model-routing',
  'autonomous.model_routing.refusal_mode': DEFS + '#model-routing-refusal-mode',
  'autonomous.model_routing.served_models_cache_seconds': DEFS + '#served-models-cache-age',
  'autonomous.model_routing.verifier_follows_worker_node': DEFS + '#verifier-follows-the-workers-node',
  'autonomous.model_routing.unknown_node_policy': DEFS + '#unreachable-node-policy',
  'autonomous.model_routing.probe_timeout_seconds': DEFS + '#node-probe-timeout',
  'autonomous.model_routing.alert_on_refusal': DEFS + '#model-routing-refusal-alert',
};

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
}

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  return sandbox;
}

function run(code, vars) {
  const sandbox = loadApp();
  Object.assign(sandbox, vars || {});
  return vm.runInContext(code, sandbox);
}

function nodeHtml(entry) {
  return run('window.servedModelsNodeHtml(_entry)', { _entry: entry });
}

function entry(over) {
  return {
    node: 'gpu-1', kind: 'ollama', models: ['qwen3:27b', 'llama3:8b'], known: true, cached: true,
    age_seconds: 42, fetched_at: '2026-10-31T10:00:00Z', error: '', ...over,
  };
}

function modelRoutingFields() {
  const sandbox = loadApp();
  return JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.filter(f => (f.key || '').startsWith('autonomous.model_routing.')))",
    sandbox));
}

function docsLink(key) {
  return run('docsLinkForKey(_key)', { _key: key });
}

// ── served models block ─────────────────────────────────────────────────────

test('the block shows the model names, the age, the title and the Refresh button', () => {
  const html = nodeHtml(entry());
  assert.ok(html.includes('Served models'), 'title missing: ' + html);
  assert.ok(html.includes('qwen3:27b') && html.includes('llama3:8b'), 'model names missing: ' + html);
  assert.ok(html.includes('Read 42 seconds ago'), 'age missing: ' + html);
  assert.ok(html.includes('>Refresh<'), 'Refresh button missing: ' + html);
  assert.ok(html.includes('loadServedModels(true)'), 'Refresh does not read with refresh=true: ' + html);
});

test('a reading that was not cached says it was read just now', () => {
  const html = nodeHtml(entry({ cached: false, age_seconds: 0 }));
  assert.ok(html.includes('Read just now'), html);
  assert.ok(!html.includes('seconds ago'));
});

test('a node whose models cannot be read shows the muted note and no model list', () => {
  const html = nodeHtml(entry({ known: false, models: [], error: 'connection refused' }));
  assert.ok(html.includes('The models on this node cannot be read right now'), 'unknown note missing: ' + html);
  assert.ok(html.includes('served-models-note'), 'the note is not muted: ' + html);
  assert.ok(!html.includes('served-models-list'), 'an unknown node must not show a model list');
  assert.ok(!html.includes('This node serves no models.'), 'unknown must differ from empty');
  assert.ok(html.includes('>Refresh<'), 'an unknown node can still be refreshed');
});

test('a node that is read and serves nothing shows the empty state', () => {
  for (const models of [[], null, undefined]) {
    const html = nodeHtml(entry({ models }));
    assert.ok(html.includes('This node serves no models.'), 'empty state missing: ' + html);
    assert.ok(!html.includes('served-models-list'));
    assert.ok(!html.includes('cannot be read'), 'empty must differ from unknown');
  }
});

test('a disabled node says so instead of listing models', () => {
  const html = nodeHtml(entry({ disabled: true, known: false, models: [] }));
  assert.ok(html.includes('This node is disabled'), html);
});

test('a missing entry renders as an unknown node', () => {
  for (const e of [null, undefined, {}]) {
    assert.ok(nodeHtml(e).includes('cannot be read right now'));
  }
});

test('the card title carries the help link to the model routing section', () => {
  const html = run('window.servedModelsTitleHtml()');
  assert.ok(html.includes('Served models'), html);
  assert.ok(html.includes('class="docs-link"'), 'help link missing: ' + html);
  assert.ok(html.includes('/diagrams.html#docs/' + DEFS + '#model-routing"'), 'help link does not open the section: ' + html);
});

test('hostile model, node and error text renders as text', () => {
  const html = nodeHtml(entry({ node: HOSTILE, models: [HOSTILE, 'ok'], error: HOSTILE }))
    + nodeHtml(entry({ known: false, models: [], error: HOSTILE }))
    + run('window.servedModelsBlockHtml(_n)', { _n: HOSTILE });
  assert.ok(!html.includes(HOSTILE), 'hostile markup rendered unescaped: ' + html);
  assert.ok(!html.includes('<img'), 'an image element was injected');
  assert.ok(html.includes('&lt;img'), 'hostile text should be shown as text');
});

test('the per-node placeholder carries the escaped node name', () => {
  const html = run('window.servedModelsBlockHtml(_n)', { _n: 'gpu-1' });
  assert.ok(html.includes('class="served-models-block"') && html.includes('data-node="gpu-1"'), html);
});

test('the compute node view puts a block in every node row and fills it from one read', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const start = src.indexOf('function loadComputeNodesPanel()');
  const end = src.indexOf('window.loadComputeNodesPanel = loadComputeNodesPanel;');
  assert.ok(start > 0 && end > start, 'compute nodes panel not found');
  const panel = src.slice(start, end);
  assert.ok(panel.includes('window.servedModelsBlockHtml(n.name)'), 'node row has no served models block');
  assert.ok(panel.includes('window.loadServedModels(false)'), 'the panel does not fill the blocks');
});

test('loadServedModels reads once, fills each block, and Refresh reads with refresh=true', async () => {
  const sandbox = loadApp();
  sandbox._calls = [];
  sandbox._reply = { nodes: [entry(), entry({ node: 'gpu-2', known: false, models: [] })] };
  sandbox._blocks = ['gpu-1', 'gpu-2', 'gone'].map(n => ({ dataset: { node: n }, innerHTML: '' }));
  vm.runInContext(`
    apiFetch = (url) => { _calls.push(url); return Promise.resolve(_reply); };
    document.querySelectorAll = (sel) => sel === '.served-models-block' ? _blocks : [];
  `, sandbox);
  await vm.runInContext('window.loadServedModels(false)', sandbox);
  assert.deepEqual(sandbox._calls, ['/api/compute/served-models']);
  assert.ok(sandbox._blocks[0].innerHTML.includes('qwen3:27b'));
  assert.ok(sandbox._blocks[1].innerHTML.includes('cannot be read right now'));
  assert.ok(sandbox._blocks[2].innerHTML.includes('cannot be read right now'), 'a node missing from the read counts as unknown');
  await vm.runInContext('window.loadServedModels(true)', sandbox);
  assert.equal(sandbox._calls[1], '/api/compute/served-models?refresh=true');
});

test('a failed read shows the reason in every block, escaped', async () => {
  const sandbox = loadApp();
  sandbox._blocks = [{ dataset: { node: 'gpu-1' }, innerHTML: '' }];
  vm.runInContext(`
    apiFetch = () => Promise.reject(new Error('<img src=x onerror=alert(1)>'));
    document.querySelectorAll = () => _blocks;
  `, sandbox);
  await vm.runInContext('window.loadServedModels(false)', sandbox);
  const html = sandbox._blocks[0].innerHTML;
  assert.ok(html.includes('Could not read the served models'), html);
  assert.ok(!html.includes('<img'), html);
});

// ── node pin ────────────────────────────────────────────────────────────────

test('the Pin to node select lists none first, then every registered node, and marks the pinned one', () => {
  const sandbox = loadApp();
  sandbox._nodes = [{ name: 'gpu-1' }, { name: 'gpu-2' }];
  vm.runInContext('window._computeNodesCache = _nodes', sandbox);
  const html = vm.runInContext("window.modelRoutingPinFieldHtml('pin1', 'gpu-2')", sandbox);
  assert.ok(html.includes('Pin to node'), html);
  assert.ok(html.indexOf('No pin (any node)') < html.indexOf('gpu-1'), 'none choice must come first');
  assert.ok(html.includes('<option value="gpu-2" selected>'), 'the pinned node is not selected: ' + html);
  assert.ok(html.includes('<option value="gpu-1">'));
  const none = vm.runInContext("window.modelRoutingPinFieldHtml('pin1', '')", sandbox);
  assert.ok(none.includes('<option value="" selected>'), none);
});

test('a pinned node that is no longer registered stays in the list, and hostile names are escaped', () => {
  const sandbox = loadApp();
  sandbox._nodes = [{ name: HOSTILE }, { name: 'gpu-1' }];
  vm.runInContext('window._computeNodesCache = _nodes', sandbox);
  const html = vm.runInContext("window.modelRoutingPinFieldHtml('pin1', 'retired-node')", sandbox);
  assert.ok(html.includes('<option value="retired-node" selected>'), html);
  assert.ok(!html.includes(HOSTILE) && !html.includes('<img'), 'hostile node name rendered unescaped: ' + html);
});

test('saving a pin posts the node to the task or story route, and an empty node clears it', async () => {
  const sandbox = loadApp();
  sandbox._calls = [];
  vm.runInContext("apiFetch = (url, opts) => { _calls.push([url, JSON.parse(opts.body)]); return Promise.resolve({}); };", sandbox);
  await vm.runInContext("window.modelRoutingSavePin('p/1', 'task', 't1', 'gpu-1')", sandbox);
  await vm.runInContext("window.modelRoutingSavePin('p1', 'story', 's1', '')", sandbox);
  // The calls were built inside the vm context, so compare their JSON form.
  assert.equal(JSON.stringify(sandbox._calls[0]), JSON.stringify(['/api/autonomous/prds/p%2F1/set_task_node', { node: 'gpu-1', actor: 'operator', task_id: 't1' }]));
  assert.equal(JSON.stringify(sandbox._calls[1]), JSON.stringify(['/api/autonomous/prds/p1/set_story_node', { node: '', actor: 'operator', story_id: 's1' }]));
});

test('a refusal from the server rejects the save so the editor shows it in its error toast', async () => {
  const sandbox = loadApp();
  vm.runInContext("apiFetch = () => Promise.reject(new Error('node gpu-1 does not serve qwen3:27b'));", sandbox);
  await assert.rejects(vm.runInContext("window.modelRoutingSavePin('p1', 'task', 't1', 'gpu-1')", sandbox), /does not serve qwen3:27b/);
});

test('both editors add the pin select, save it after the LLM change, and show a refusal like the LLM editor', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  for (const [fn, selectId, unit] of [['function openPRDEditTaskModal(', 'prdEditPinNode', 'task'], ['function openPRDSetStoryLLMModal(', 'prdStoryPinNode', 'story']]) {
    const start = src.indexOf(fn);
    assert.ok(start > 0, fn + ' not found');
    const body = src.slice(start, start + 9000);
    assert.ok(body.includes(`modelRoutingPinFieldHtml('${selectId}'`), unit + ' editor has no pin select');
    assert.ok(body.includes(`modelRoutingPinValue('${selectId}')`), unit + ' editor does not read the pin select');
    assert.ok(body.includes(`modelRoutingSavePin(prdID, '${unit}'`), unit + ' editor does not save the pin');
    assert.ok(body.includes("showToast('Save failed: ' + String(err), 'error'"), unit + ' editor lost its error toast');
    assert.ok(body.indexOf('Promise.all(calls)') < body.indexOf('modelRoutingSavePin('), unit + ' pin must be saved after the LLM calls');
  }
});

test('the task and story rows pass their pinned node to the editors', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.ok(src.includes("JSON.stringify(task.pinned_node || '')"), 'task row does not pass pinned_node');
  assert.ok(src.includes("JSON.stringify(story.pinned_node || '')"), 'story row does not pass pinned_node');
  assert.ok(src.includes('window.modelRoutingPinBadgeHtml(task)'), 'task row has no pinned badge');
  assert.ok(src.includes('window.modelRoutingPinBadgeHtml(story)'), 'story row has no pinned badge');
});

test('a pinned row shows a badge with the node, an unpinned row shows none, and the node is escaped', () => {
  const html = run('window.modelRoutingPinBadgeHtml(_item)', { _item: { pinned_node: 'gpu-1' } });
  assert.ok(html.includes('Node: gpu-1'), html);
  assert.ok(html.includes('title="Pinned to compute node gpu-1"'), html);
  for (const item of [{}, { pinned_node: '' }, null, undefined]) {
    assert.equal(run('window.modelRoutingPinBadgeHtml(_item)', { _item: item }), '');
  }
  const hostile = run('window.modelRoutingPinBadgeHtml(_item)', { _item: { pinned_node: HOSTILE } });
  assert.ok(!hostile.includes('<img') && !hostile.includes(HOSTILE), hostile);
});

// ── settings fields ─────────────────────────────────────────────────────────

test('the Autonomous settings group has the six model_routing fields with the right types and defaults', () => {
  const fields = modelRoutingFields();
  const byKey = Object.fromEntries(fields.map(f => [f.key, f]));
  const want = {
    'autonomous.model_routing.refusal_mode': 'select',
    'autonomous.model_routing.served_models_cache_seconds': 'number',
    'autonomous.model_routing.verifier_follows_worker_node': 'toggle',
    'autonomous.model_routing.unknown_node_policy': 'select',
    'autonomous.model_routing.probe_timeout_seconds': 'number',
    'autonomous.model_routing.alert_on_refusal': 'toggle',
  };
  assert.equal(fields.length, 6, 'expected six fields: ' + fields.map(f => f.key).join(','));
  for (const [key, type] of Object.entries(want)) {
    assert.ok(byKey[key], key + ' missing');
    assert.equal(byKey[key].type, type, key + ' has the wrong type');
  }
  assert.deepEqual(byKey['autonomous.model_routing.refusal_mode'].options, ['refuse', 'warn']);
  assert.equal(byKey['autonomous.model_routing.refusal_mode'].default, 'refuse');
  assert.deepEqual(byKey['autonomous.model_routing.unknown_node_policy'].options, ['assume_serves', 'assume_not']);
  assert.equal(byKey['autonomous.model_routing.unknown_node_policy'].default, 'assume_serves');
  const cache = byKey['autonomous.model_routing.served_models_cache_seconds'];
  assert.equal(cache.min, 0);
  assert.equal(cache.step, 1);
  assert.equal(cache.default, '300');
  const probe = byKey['autonomous.model_routing.probe_timeout_seconds'];
  assert.equal(probe.min, 1);
  assert.equal(probe.step, 1);
  assert.equal(probe.default, '8');
});

test('every model_routing field has a label key, a description key, a docs link and select option labels', () => {
  const en = bundle('en');
  for (const f of modelRoutingFields()) {
    assert.ok(f.labelKey && f.labelKey.startsWith(SETTINGS_PREFIX), f.key + ' labelKey');
    assert.equal(f.hintKey, f.labelKey + '_hint', f.key + ' hintKey');
    assert.ok(en[f.labelKey] && en[f.hintKey], f.key + ' label or description missing in en');
    assert.equal(f.docsKey, f.key, f.key + ' docsKey');
    assert.equal(docsLink(f.docsKey), ANCHORS[f.key], f.key + ' docs link');
    if (f.type === 'select') {
      for (const o of f.options) {
        assert.ok(en[f.optionLabelKeys[o]], f.key + ' option ' + o + ' has no label');
      }
    }
  }
});

// ── help links ──────────────────────────────────────────────────────────────

test('DOCS_LINKS_BY_KEY and docsLinkForKey map the card key and each config key to the exact anchor', () => {
  const sandbox = loadApp();
  for (const [key, link] of Object.entries(ANCHORS)) {
    sandbox._key = key;
    assert.equal(vm.runInContext('DOCS_LINKS_BY_KEY[_key]', sandbox), link, 'DOCS_LINKS_BY_KEY ' + key);
    assert.equal(vm.runInContext('docsLinkForKey(_key)', sandbox), link, 'docsLinkForKey ' + key);
  }
  assert.equal(docsLink('autonomous.lane_fill.mode'), null, 'unrelated keys keep their own mapping');
});

// ── locale keys ─────────────────────────────────────────────────────────────

function ownKeys(b) {
  return Object.keys(b).filter(k => k.startsWith(VIEW_PREFIX) || k.startsWith(SETTINGS_PREFIX)).sort();
}

test('the model_routing and settings_model_routing keys are identical in all five bundles', () => {
  const en = ownKeys(bundle('en'));
  assert.ok(en.length >= 25, 'too few keys in en: ' + en.length);
  for (const lang of LANGS) {
    const b = bundle(lang);
    assert.deepEqual(ownKeys(b), en, lang + ' key set differs from en');
    for (const k of en) assert.ok(typeof b[k] === 'string' && b[k].trim().length > 0, k + ' empty in ' + lang);
  }
});

test('the non-English bundles carry the English text until translations arrive', () => {
  const en = bundle('en');
  for (const lang of LANGS.filter(l => l !== 'en')) {
    const b = bundle(lang);
    for (const k of ownKeys(en)) assert.equal(b[k], en[k], k + ' differs from en in ' + lang);
  }
});

test('every model_routing key the app references exists in all five bundles with text', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const referenced = new Set();
  for (const m of src.matchAll(/['"`]((?:settings_)?model_routing_[a-z_]+)['"`]/g)) referenced.add(m[1]);
  assert.ok(referenced.size >= 25, 'too few references found: ' + referenced.size);
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of referenced) {
      assert.ok(typeof b[k] === 'string' && b[k].trim().length > 0, k + ' referenced by app.js but missing or empty in ' + lang);
    }
  }
  // The other direction: no key sits in a bundle without a user.
  for (const k of ownKeys(bundle('en'))) assert.ok(referenced.has(k), k + ' is in the bundles but app.js never uses it');
});

test('placeholders agree: each %n$s in a value is one the view supplies', () => {
  const en = bundle('en');
  const counts = {
    model_routing_served_models_failed: 1, model_routing_age: 1, model_routing_pinned_badge: 1, model_routing_pinned_title: 1,
  };
  for (const [k, n] of Object.entries(counts)) {
    const found = new Set([...en[k].matchAll(/%(\d+)\$s/g)].map(m => m[1]));
    assert.equal(found.size, n, k + ' placeholder count');
  }
});

test('no new string says PRD, and none carries a tracker id', () => {
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of ownKeys(b)) {
      assert.ok(!/\bPRDs?\b/i.test(b[k]), k + ' says PRD in ' + lang + ': ' + b[k]);
      assert.ok(!/\b(?:BL|B|GH#?)\d+\b|#\d+\b/.test(b[k]), k + ' carries a tracker id in ' + lang + ': ' + b[k]);
    }
  }
});

test('the markup and the settings fields carry no PRD wording or tracker id', () => {
  const html = nodeHtml(entry()) + nodeHtml(entry({ known: false, models: [] }))
    + run('window.modelRoutingPinBadgeHtml(_item)', { _item: { pinned_node: 'gpu-1' } })
    + run("window.modelRoutingPinFieldHtml('pin1', 'gpu-1')");
  const visible = html.replace(/class="[^"]*"/g, '').replace(/id="[^"]*"/g, '');
  assert.ok(!/\bPRD/i.test(visible), 'PRD wording in markup');
  assert.ok(!/\b(?:BL|GH#?)\d+\b/.test(visible), 'tracker id in markup');
  for (const f of modelRoutingFields()) {
    for (const v of [f.label, f.placeholder || '']) {
      assert.ok(!/\bPRD/i.test(v) && !/\b(?:BL|GH#?)\d+\b/.test(v), f.key + ' fallback text: ' + v);
    }
  }
});
