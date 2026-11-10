// Merge pipeline card — PWA side.
//
// window.mergePipelineCardHtml(view, opts) turns the decoded GET
// /api/autonomous/merge-pipeline body into the Automation Overview card: the
// source in effect with its reason, the steps in order, the conflict policy and
// the conflict model, and the release approval mode in effect for each size.
// The Autonomous settings group carries the six autonomous.merge_pipeline.*
// fields, the three release approval fields and the Post-ship hooks card (the
// autonomous.merge_pipeline.post_ship.* fields). docsLinkForKey maps the
// merge pipeline keys and card to the four glossary sections, the release
// approval keys to the Release approval modes howto section, and the post-ship
// keys and card to the Post-ship hooks howto. Every merge_pipeline_,
// settings_merge_pipeline_, release_approval, post_ship_ and settings_post_ship_
// locale key is present in all five bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const HOSTILE = '<img src=x onerror=alert(1)>';
const DEFS = 'datawatch-definitions.md#';
const PREFIXES = ['merge_pipeline_', 'settings_merge_pipeline_'];

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

function render(view, opts) {
  const sandbox = loadApp();
  sandbox._view = view;
  sandbox._opts = opts;
  return vm.runInContext('window.mergePipelineCardHtml(_view, _opts)', sandbox);
}

function docsLink(key) {
  const sandbox = loadApp();
  sandbox._key = key;
  return vm.runInContext('docsLinkForKey(_key)', sandbox);
}

function steps() {
  return [
    { name: 'merge', kind: 'ship', action: 'merge_branches' },
    { name: 'test', kind: 'test', command: 'make test', on_failure: 'continue' },
  ];
}

function view(over) {
  return {
    project: 'demo', definition: { steps: steps() }, source: 'file', reason: 'the project file was found',
    settings: { file_name: '.datawatch/merge-pipeline.yaml', source_order: ['file', 'record'], fallback_on_bad_file: 'record', version_files: [] },
    conflict_policy: 'mechanical_and_model', conflict_policy_in_effect: 'mechanical_and_model',
    conflict_model: 'claude-opus-5-5', conflict_model_in_effect: 'claude-opus-5-5',
    release_approval_in_effect: { patch: 'auto', minor: 'ask', major: 'manual' }, ...over,
  };
}

function mergeFields() {
  const sandbox = loadApp();
  return JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.filter(f => { const k = f.key || ''; return k.startsWith('autonomous.merge_pipeline.') && !k.startsWith('autonomous.merge_pipeline.post_ship.'); }))",
    sandbox));
}

function postShipFields() {
  const sandbox = loadApp();
  return JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.filter(f => (f.key || '').startsWith('autonomous.merge_pipeline.post_ship.')))",
    sandbox));
}

// ── source badge ────────────────────────────────────────────────────────────

test('the source badge names the file, with the reason', () => {
  const html = render(view());
  assert.ok(html.includes('merge-pipeline-source-file'), html);
  assert.ok(html.includes('>Project file<'), html);
  assert.ok(html.includes('Why: the project file was found'), html);
});

test('the source badge names the daemon record', () => {
  const html = render(view({ source: 'record', reason: 'no project file' }));
  assert.ok(html.includes('merge-pipeline-source-record'), html);
  assert.ok(html.includes('>Daemon record<'), html);
});

test('the source badge names the built-in default', () => {
  const html = render(view({ source: 'default', reason: 'nothing else was set' }));
  assert.ok(html.includes('merge-pipeline-source-default'), html);
  assert.ok(html.includes('>Built-in default<'), html);
});

test('an unknown source reads as none chosen, and a bad file shows its problem', () => {
  const html = render(view({ source: 'constructor', file_error: 'line 3: bad step' }));
  assert.ok(html.includes('merge-pipeline-source-none'), html);
  assert.ok(html.includes('None chosen'), html);
  assert.ok(html.includes('Problem with the project file: line 3: bad step'), html);
});

// ── steps, empty state, placeholder ─────────────────────────────────────────

test('the steps are listed in order with their action or command', () => {
  const html = render(view());
  assert.ok(html.indexOf('>merge<') > 0 && html.indexOf('>merge<') < html.indexOf('>test<'), 'steps out of order: ' + html);
  assert.ok(html.includes('Built-in action: merge_branches'));
  assert.ok(html.includes('Command: make test'));
  assert.ok(html.includes('continues on failure'));
  assert.equal(html.split('class="merge-pipeline-step"').length - 1, 2);
});

test('no definition shows the empty state', () => {
  for (const v of [view({ definition: null, source: '' }), view({ definition: { steps: [] } })]) {
    const html = render(v);
    assert.ok(html.includes('No pipeline could be chosen, so a merge run would not start.'), html);
    assert.ok(!html.includes('class="merge-pipeline-step"'));
  }
});

test('a missing view shows the placeholder with the card title', () => {
  for (const html of [render(null), render(undefined)]) {
    assert.ok(html.includes('Merge pipeline'));
    assert.ok(html.includes('The merge pipeline has not loaded yet.'));
  }
});

test('every value from the API is escaped', () => {
  const html = render(view({
    project: HOSTILE, reason: HOSTILE, file_error: HOSTILE, conflict_model: HOSTILE,
    definition: { steps: [{ name: HOSTILE, kind: HOSTILE, command: HOSTILE, action: '' }] },
  }), { models: [HOSTILE], projects: [HOSTILE] });
  assert.ok(!html.includes(HOSTILE), 'hostile markup rendered unescaped: ' + html);
  assert.ok(!html.includes('<img'), 'an image element was injected');
});

// ── policy and model ────────────────────────────────────────────────────────

test('policy selection offers the three policies and selects the configured one', () => {
  const html = render(view({ conflict_policy: 'mechanical', conflict_policy_in_effect: 'mechanical' }));
  const sel = html.slice(html.indexOf('id="mergePipelineConflictPolicy"'), html.indexOf('</select>', html.indexOf('id="mergePipelineConflictPolicy"')));
  for (const v of ['stop_and_ask', 'mechanical', 'mechanical_and_model']) assert.ok(sel.includes('value="' + v + '"'), v + ' missing: ' + sel);
  assert.ok(sel.includes('value="mechanical" selected'), 'configured policy not selected: ' + sel);
  assert.equal((sel.match(/ selected/g) || []).length, 1);
  assert.ok(sel.includes('window.mergePipelineSetPolicy(this.value)'));
});

test('a degraded policy shows what is in effect and why', () => {
  const html = render(view({ conflict_policy_in_effect: 'stop_and_ask', conflict_degraded_reason: 'the conflict model "x" is not available' }));
  assert.ok(html.includes('In effect: stop_and_ask.'), html);
  assert.ok(html.includes('is not available'));
  assert.ok(!render(view()).includes('merge-pipeline-degraded'));
});

test('the model list comes from the registry, with a no-model choice and the current model kept', () => {
  const html = render(view({ conflict_model: 'old-model' }), { models: ['claude-opus-5-5', 'qwen'] });
  const sel = html.slice(html.indexOf('id="mergePipelineConflictModel"'), html.indexOf('</select>', html.indexOf('id="mergePipelineConflictModel"')));
  assert.ok(sel.includes('value="" '.trim()) && sel.includes('>No model<'), sel);
  for (const n of ['claude-opus-5-5', 'qwen', 'old-model']) assert.ok(sel.includes('value="' + n + '"'), n + ' missing: ' + sel);
  assert.ok(sel.includes('value="old-model" selected'), sel);
});

test('choosing a policy or a model writes the per-project setting', async () => {
  const sandbox = loadApp();
  sandbox._calls = [];
  vm.runInContext("apiFetch = (url, o) => { _calls.push([url, o && o.method, o && o.body]); return Promise.resolve({}); };", sandbox);
  vm.runInContext("window._mergePipelineProject = 'demo';", sandbox);
  await vm.runInContext("window.mergePipelineSetPolicy('stop_and_ask')", sandbox);
  await vm.runInContext("window.mergePipelineSetModel('')", sandbox);
  assert.deepEqual(Array.from(sandbox._calls[0]), ['/api/config', 'PUT', JSON.stringify({ 'autonomous.merge_pipeline.projects.demo.conflict_policy': 'stop_and_ask' })]);
  const model = sandbox._calls.find(c => c[0] === '/api/config' && c[2].includes('conflict_model'));
  assert.equal(model[2], JSON.stringify({ 'autonomous.merge_pipeline.projects.demo.conflict_model': '' }));
});

// ── release approval ────────────────────────────────────────────────────────

function releaseSelect(html, size) {
  const at = html.indexOf('id="mergePipelineReleaseApproval-' + size + '"');
  assert.ok(at > 0, size + ' select missing: ' + html);
  return html.slice(at, html.indexOf('</select>', at));
}

test('the card shows the release approval mode in effect for each size, always', () => {
  const html = render(view());
  assert.ok(html.includes('class="merge-pipeline-release-approval"'), html);
  assert.ok(html.includes('Release approval'), html);
  for (const [size, mode] of [['patch', 'auto'], ['minor', 'ask'], ['major', 'manual']]) {
    const sel = releaseSelect(html, size);
    assert.ok(sel.includes('value="' + mode + '" selected'), size + ' not showing ' + mode + ': ' + sel);
    assert.equal((sel.match(/ selected/g) || []).length, 1, size + ' has more than one selected mode');
    for (const v of ['auto', 'ask', 'manual']) assert.ok(sel.includes('value="' + v + '"'), size + ' lacks ' + v);
  }
  assert.ok(html.includes('Patch releases') && html.includes('Minor releases') && html.includes('Major releases'), html);
  assert.ok(html.includes('>Ship without waiting<') && html.includes('>Ask the operator first<') && html.includes('>Left for the operator to ship<'), html);
});

test('the card always shows a mode, even when the view omits the in-effect block', () => {
  const html = render(view({ release_approval_in_effect: undefined }));
  assert.ok(html.includes('class="merge-pipeline-release-approval"'), html);
  assert.ok(!html.includes(' selected>Ship without waiting'), 'a mode was invented: ' + html);
});

test('the release approval rows link their labels to the how-to section', () => {
  const html = render(view());
  assert.ok(html.includes('/diagrams.html#docs/' + RELEASE_HOWTO + '"'), 'release approval help link missing: ' + html);
});

test('choosing a release approval mode writes that project and size only', async () => {
  const sandbox = loadApp();
  sandbox._calls = [];
  vm.runInContext("apiFetch = (url, o) => { _calls.push([url, o && o.method, o && o.body]); return Promise.resolve({}); };", sandbox);
  vm.runInContext("window._mergePipelineProject = 'demo';", sandbox);
  await vm.runInContext("window.mergePipelineSetReleaseApproval('minor', 'manual')", sandbox);
  assert.deepEqual(Array.from(sandbox._calls[0]), ['/api/config', 'PUT', JSON.stringify({ 'autonomous.merge_pipeline.projects.demo.release_approval.minor': 'manual' })]);
  // An unknown size or mode is refused before anything is sent.
  await vm.runInContext("window.mergePipelineSetReleaseApproval('hotfix', 'manual'); window.mergePipelineSetReleaseApproval('major', 'never')", sandbox);
  assert.equal(sandbox._calls.length, 1, 'a bad size or mode was sent: ' + JSON.stringify(sandbox._calls));
});

test('the record editor rejects invalid JSON without calling the daemon', async () => {
  const sandbox = loadApp();
  sandbox._calls = [];
  vm.runInContext("apiFetch = (url) => { _calls.push(url); return Promise.resolve({}); };", sandbox);
  const html = render(view({ source: 'record' }));
  assert.ok(html.includes('id="mergePipelineRecord"') && html.includes('window.mergePipelineSaveRecord()') && html.includes('window.mergePipelineClearRecord()'), html);
  await vm.runInContext("document.getElementById = (id) => id === 'mergePipelineRecord' ? { value: '{nope' } : null; window.mergePipelineSaveRecord()", sandbox);
  assert.deepEqual(sandbox._calls, []);
});

// ── help links ──────────────────────────────────────────────────────────────

test('the card and its headings carry help links to the glossary sections', () => {
  const html = render(view());
  assert.ok(html.includes('class="docs-link"'), 'help link missing: ' + html);
  for (const a of ['merge-pipeline', 'merge-pipeline-source', 'merge-conflict-policy', 'merge-conflict-model']) {
    assert.ok(html.includes('/diagrams.html#docs/' + DEFS + a + '"'), a + ' link missing: ' + html);
  }
});

test('docsLinkForKey maps the merge pipeline keys to the four sections', () => {
  const want = {
    'merge_pipeline_card_title': 'merge-pipeline',
    'autonomous.merge_pipeline.version_files': 'merge-pipeline',
    'autonomous.merge_pipeline.file_name': 'merge-pipeline-source',
    'autonomous.merge_pipeline.source_order': 'merge-pipeline-source',
    'autonomous.merge_pipeline.fallback_on_bad_file': 'merge-pipeline-source',
    'merge_pipeline_source_label': 'merge-pipeline-source',
    'autonomous.merge_pipeline.conflict_policy': 'merge-conflict-policy',
    'merge_pipeline_conflict_policy_label': 'merge-conflict-policy',
    'autonomous.merge_pipeline.conflict_model': 'merge-conflict-model',
    'merge_pipeline_conflict_model_label': 'merge-conflict-model',
  };
  for (const [key, anchor] of Object.entries(want)) assert.equal(docsLink(key), DEFS + anchor, key);
  assert.equal(docsLink('autonomous.infra_retry.enabled'), DEFS + 'infrastructure-retry', 'infra retry keeps its own link');
  assert.equal(docsLink('merge_pipeline_no_project_set_base'), 'howto/merge-pipeline.md#no-project-yet',
    'the empty-state setting line points at the how-to, before the generic merge_pipeline_ fallback');
  assert.equal(docsLink('merge_pipeline_no_project_title'), DEFS + 'merge-pipeline', 'the other no-project keys keep the glossary link');
});

const RELEASE_HOWTO = 'howto/merge-pipeline.md#release-approval-modes';

test('the release approval keys link to the Release approval modes howto section', () => {
  const keys = [
    'autonomous.merge_pipeline.release_approval.patch',
    'autonomous.merge_pipeline.release_approval.minor',
    'autonomous.merge_pipeline.release_approval.major',
    'settings_merge_pipeline_release_approval_patch',
    'settings_merge_pipeline_release_approval_major_hint',
    'merge_pipeline_release_approval_label',
    'merge_pipeline_release_approval_minor',
    'merge_pipeline_release_approval_mode_manual',
  ];
  for (const k of keys) assert.equal(docsLink(k), RELEASE_HOWTO, k);
  assert.ok(fs.existsSync(path.join(__dirname, '..', '..', '..', 'docs', 'howto', 'merge-pipeline.md')), 'the how-to is missing');
  const md = fs.readFileSync(path.join(__dirname, '..', '..', '..', 'docs', 'howto', 'merge-pipeline.md'), 'utf8');
  assert.ok(/^## Release approval modes$/m.test(md), 'the how-to has no "Release approval modes" section');
});

// ── settings fields ─────────────────────────────────────────────────────────

test('the Autonomous settings group has the six merge_pipeline fields and the three release approval fields with the right types', () => {
  const fields = mergeFields();
  const byKey = Object.fromEntries(fields.map(f => [f.key, f]));
  const want = {
    'autonomous.merge_pipeline.file_name': 'text',
    'autonomous.merge_pipeline.source_order': 'text',
    'autonomous.merge_pipeline.fallback_on_bad_file': 'select',
    'autonomous.merge_pipeline.conflict_policy': 'select',
    'autonomous.merge_pipeline.conflict_model': 'text',
    'autonomous.merge_pipeline.conflict_mode': 'select',
    'autonomous.merge_pipeline.conflict_confirm_first_n': 'number',
    'autonomous.merge_pipeline.version_files': 'text',
    // Release settings: their own help link, checked in the next test.
    'autonomous.merge_pipeline.release_size_map': 'release_size_map',
    'autonomous.merge_pipeline.major_trigger': 'select',
    'autonomous.merge_pipeline.release_cadence': 'select',
    'autonomous.merge_pipeline.version_scheme': 'select',
    'autonomous.merge_pipeline.release_approval.patch': 'select',
    'autonomous.merge_pipeline.release_approval.minor': 'select',
    'autonomous.merge_pipeline.release_approval.major': 'select',
  };
  assert.equal(fields.length, 15, 'expected fifteen fields: ' + fields.map(f => f.key).join(','));
  for (const [key, type] of Object.entries(want)) {
    assert.ok(byKey[key], key + ' missing');
    assert.equal(byKey[key].type, type, key + ' has the wrong type');
  }
  assert.deepEqual(byKey['autonomous.merge_pipeline.conflict_policy'].options, ['stop_and_ask', 'mechanical', 'mechanical_and_model']);
  assert.deepEqual(byKey['autonomous.merge_pipeline.conflict_mode'].options, ['shadow', 'on']);
  assert.equal(byKey['autonomous.merge_pipeline.conflict_mode'].default, 'shadow');
  assert.equal(byKey['autonomous.merge_pipeline.conflict_confirm_first_n'].min, 0);
  assert.deepEqual(byKey['autonomous.merge_pipeline.fallback_on_bad_file'].options, ['record', 'default', 'none']);
  assert.equal(byKey['autonomous.merge_pipeline.source_order'].csv, true);
  assert.equal(byKey['autonomous.merge_pipeline.version_files'].csv, true);
});

test('each release approval field offers auto, ask and manual, with its default', () => {
  const byKey = Object.fromEntries(mergeFields().map(f => [f.key, f]));
  const defaults = { patch: 'auto', minor: 'auto', major: 'ask' };
  for (const size of ['patch', 'minor', 'major']) {
    const f = byKey['autonomous.merge_pipeline.release_approval.' + size];
    assert.deepEqual(f.options, ['auto', 'ask', 'manual'], size + ' options');
    assert.equal(f.default, defaults[size], size + ' default');
    assert.deepEqual(Object.keys(f.optionLabelKeys).sort(), ['ask', 'auto', 'manual'], size + ' option labels');
    assert.equal(f.optionLabelKeys.auto, 'merge_pipeline_release_approval_mode_auto');
    assert.equal(f.optionLabelKeys.ask, 'merge_pipeline_release_approval_mode_ask');
    assert.equal(f.optionLabelKeys.manual, 'merge_pipeline_release_approval_mode_manual');
  }
});

test('every merge_pipeline field has a label key, an inline description key and a docs link', () => {
  const en = bundle('en');
  for (const f of mergeFields()) {
    // A dotted key maps to underscores: release_approval.patch -> release_approval_patch.
    const name = f.key.slice('autonomous.merge_pipeline.'.length).replace(/\./g, '_');
    assert.equal(f.labelKey, 'settings_merge_pipeline_' + name, f.key + ' labelKey');
    if (f.helpDocs) {
      // Release fields carry a '?' help link (helpDocs) instead of the docs pill.
      assert.ok(en[f.helpKey], f.key + ' help text missing in en');
      assert.equal(docsLink(f.key), f.helpDocs, f.key + ' help link does not map to its howto');
      continue;
    }
    assert.equal(f.hintKey, 'settings_merge_pipeline_' + name + '_hint', f.key + ' hintKey');
    assert.ok(en[f.labelKey] && en[f.hintKey], f.key + ' label or description missing in en');
    assert.ok(f.docsKey, f.key + ' has no docs link');
    assert.ok(docsLink(f.docsKey), f.key + ' docs link does not resolve');
  }
});

// ── post-ship hooks ─────────────────────────────────────────────────────────

const HOWTO = 'howto/post-ship-hooks.md';

test('the Autonomous settings group has the Post-ship hooks card with the seven post-ship keys and the right types', () => {
  const fields = postShipFields();
  const byKey = Object.fromEntries(fields.map(f => [f.key, f]));
  const want = {
    'autonomous.merge_pipeline.post_ship.enabled': 'toggle',
    'autonomous.merge_pipeline.post_ship.hooks': 'post_ship_hooks',
    'autonomous.merge_pipeline.post_ship.registry_file': 'text',
    'autonomous.merge_pipeline.post_ship.registry_row_pattern': 'text',
    'autonomous.merge_pipeline.post_ship.companion_repository': 'text',
    'autonomous.merge_pipeline.post_ship.issue_title_template': 'text',
    'autonomous.merge_pipeline.post_ship.notify_template': 'textarea',
  };
  assert.equal(fields.length, 7, 'expected seven post-ship fields: ' + fields.map(f => f.key).join(','));
  for (const [key, type] of Object.entries(want)) {
    assert.ok(byKey[key], key + ' missing');
    assert.equal(byKey[key].type, type, key + ' has the wrong type');
  }
});

test('the Post-ship hooks group entry carries the card title and a docs link', () => {
  const sandbox = loadApp();
  const group = JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.find(f => f.type === 'group' && f.labelKey === 'post_ship_card_title'))",
    sandbox));
  assert.ok(group, 'the Post-ship hooks group entry is missing');
  assert.equal(group.labelKey, 'post_ship_card_title');
  assert.ok(group.docsKey, 'the group has no docs link');
  assert.equal(docsLink(group.docsKey), HOWTO);
  assert.ok(bundle('en').post_ship_card_title, 'card title key missing in en');
});

test('every post-ship field has a label key, an inline description key and a docs link', () => {
  const en = bundle('en');
  for (const f of postShipFields()) {
    const name = f.key.slice('autonomous.merge_pipeline.post_ship.'.length);
    assert.equal(f.labelKey, 'settings_post_ship_' + name, f.key + ' labelKey');
    assert.equal(f.hintKey, 'settings_post_ship_' + name + '_hint', f.key + ' hintKey');
    assert.ok(en[f.labelKey] && en[f.hintKey], f.key + ' label or description missing in en');
    assert.ok(f.docsKey, f.key + ' has no docs link');
    assert.ok(docsLink(f.docsKey), f.key + ' docs link does not resolve');
  }
});

// ── release readiness card ──────────────────────────────────────────────────

function readinessBody(size, over) {
  return {
    size, records: 0, conflicts: 0, pwa_changes: 0, blocking_defects: 0,
    required: { records: 10, conflicts: 2, pwa_changes: 2 }, missing: [], ready: false, ...over,
  };
}

function readinessCard(results) {
  const sandbox = loadApp();
  sandbox._results = results;
  return vm.runInContext('window.releaseReadinessCardHtml(_results)', sandbox);
}

test('a size with no comparison records is not rendered, and no size with records renders nothing', () => {
  assert.equal(readinessCard([
    { size: 'patch', readiness: readinessBody('patch') },
    { size: 'minor', readiness: readinessBody('minor') },
    { size: 'major', readiness: readinessBody('major') },
  ]), '');
  assert.equal(readinessCard([]), '');
  assert.equal(readinessCard(undefined), '');
  const html = readinessCard([
    { size: 'patch', readiness: readinessBody('patch') },
    { size: 'minor', readiness: readinessBody('minor', { records: 3, conflicts: 1, pwa_changes: 0, missing: ['short'] }) },
    { size: 'major', readiness: readinessBody('major') },
  ]);
  assert.ok(html.includes('data-size="minor"'), html);
  assert.ok(!html.includes('data-size="patch"') && !html.includes('data-size="major"'), 'an empty size was rendered: ' + html);
});

test('a size whose only record is a blocking defect is still rendered, so the defect is never hidden', () => {
  const html = readinessCard([{ size: 'major', readiness: readinessBody('major', { blocking_defects: 1, missing: ['blocked'] }) }]);
  assert.ok(html.includes('data-size="major"'), html);
  assert.ok(html.includes('Comparisons with a blocking defect: 1'), html);
});

test('the card shows the counts against the bar, the missing items and the ready flag', () => {
  const html = readinessCard([{ size: 'minor', readiness: readinessBody('minor', {
    records: 4, conflicts: 2, pwa_changes: 1, missing: ['The minor release needs 10 records, but has 4.', 'PWA short'],
  }) }]);
  assert.ok(html.includes('Plans equal or better: 4 of 10'), html);
  assert.ok(html.includes('Counted comparisons with conflicts: 2 of 2'), html);
  assert.ok(html.includes('Counted comparisons with PWA changes: 1 of 2'), html);
  assert.ok(html.includes('<li>The minor release needs 10 records, but has 4.</li>'), html);
  assert.ok(html.includes('<li>PWA short</li>'), html);
  assert.ok(html.includes('>Not ready<') && !html.includes('>Ready<'), html);
});

test('a size that meets the bar shows Ready and no missing items', () => {
  const html = readinessCard([{ size: 'patch', readiness: readinessBody('patch', {
    records: 10, conflicts: 2, pwa_changes: 2, ready: true,
  }) }]);
  assert.ok(html.includes('>Ready<') && !html.includes('>Not ready<'), html);
  assert.ok(html.includes('Nothing is missing against the bar.'), html);
  assert.ok(!html.includes('release-readiness-missing"'), 'a missing list was drawn for a ready size: ' + html);
});

test('the ready flag comes from the API and is not recomputed from the counts', () => {
  const html = readinessCard([{ size: 'patch', readiness: readinessBody('patch', {
    records: 10, conflicts: 2, pwa_changes: 2, ready: false, missing: ['held by the operator'],
  }) }]);
  assert.ok(html.includes('>Not ready<'), html);
  assert.ok(html.includes('<li>held by the operator</li>'), html);
});

test('the readiness values and the missing items are escaped', () => {
  const html = readinessCard([{ size: 'patch', readiness: readinessBody('patch', { records: 1, missing: [HOSTILE] }) }]);
  assert.ok(!html.includes(HOSTILE), 'hostile markup rendered unescaped: ' + html);
  assert.ok(!html.includes('<img'), 'an image element was injected');
});

test('the readiness card help control links to the Move from shadow to on section', () => {
  const html = readinessCard([{ size: 'patch', readiness: readinessBody('patch', { records: 1 }) }]);
  assert.ok(html.includes('class="release-readiness-help"'), html);
  assert.ok(html.includes('/diagrams.html#docs/howto/merge-pipeline.md#move-from-shadow-to-on"'), html);
  assert.equal(docsLink('release_readiness_card_title'), 'howto/merge-pipeline.md#move-from-shadow-to-on');
  const md = fs.readFileSync(path.join(__dirname, '..', '..', '..', 'docs', 'howto', 'merge-pipeline.md'), 'utf8');
  assert.ok(/^## Move from shadow to on$/m.test(md), 'the how-to has no "Move from shadow to on" section');
});

test('every release_readiness key is present in all five bundles with identical key sets', () => {
  const sets = LANGS.map(lang => Object.keys(bundle(lang)).sort());
  for (let i = 1; i < LANGS.length; i++) assert.deepEqual(sets[i], sets[0], LANGS[i] + ' key set differs from en');
  const keys = sets[0].filter(k => k.startsWith('release_readiness_'));
  assert.equal(keys.length, 13, keys.join(','));
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of keys) assert.ok(typeof b[k] === 'string' && b[k].length > 0, lang + ' lacks ' + k);
  }
});

test('docsLinkForKey maps the post-ship keys to the howto sections, before the merge pipeline map', () => {
  assert.equal(docsLink('autonomous.merge_pipeline.post_ship.enabled'), HOWTO + '#enabling');
  assert.equal(docsLink('autonomous.merge_pipeline.post_ship.hooks'), HOWTO + '#the-hook-list');
  for (const name of ['registry_file', 'registry_row_pattern', 'companion_repository', 'issue_title_template', 'notify_template']) {
    assert.equal(docsLink('autonomous.merge_pipeline.post_ship.' + name), HOWTO + '#the-built-in-targets', name);
    assert.equal(docsLink('settings_post_ship_' + name), HOWTO + '#the-built-in-targets', name);
  }
  assert.equal(docsLink('post_ship_card_title'), HOWTO);
  assert.equal(docsLink('settings_post_ship_hooks_hint'), HOWTO + '#the-hook-list');
  assert.equal(docsLink('post_ship_hook_max'), HOWTO + '#the-hook-list');
  // The merge pipeline keys keep their own definitions links.
  assert.equal(docsLink('autonomous.merge_pipeline.file_name'), DEFS + 'merge-pipeline-source');
  assert.equal(docsLink('autonomous.merge_pipeline.conflict_policy'), DEFS + 'merge-conflict-policy');
  assert.equal(docsLink('autonomous.merge_pipeline.conflict_model'), DEFS + 'merge-conflict-model');
});

test('the hook list adds, orders and removes hooks client-side', () => {
  const sandbox = loadApp();
  sandbox._postShipHooks = [{ name: 'one', kind: 'builtin', builtin: 'registry_row', command: '', enabled: true }];
  vm.runInContext("addPostShipHook()", sandbox);
  assert.equal(sandbox._postShipHooks.length, 2);
  assert.equal(sandbox._postShipHooks[1].enabled, false, 'a new hook starts off');
  vm.runInContext("_postShipSetCommand(1, 'make ship-smoke')", sandbox);
  vm.runInContext("_postShipSetKind(1, 'command')", sandbox);
  assert.equal(sandbox._postShipHooks[1].kind, 'command');
  vm.runInContext("_postShipMove(1, -1)", sandbox);
  assert.equal(sandbox._postShipHooks[0].kind, 'command', 'the move did not reorder');
  vm.runInContext("_postShipRemove(0)", sandbox);
  assert.equal(sandbox._postShipHooks.length, 1);
  assert.equal(sandbox._postShipHooks[0].name, 'one');
  assert.equal(sandbox._postShipHooks[0].enabled, true);
});

test('the hook list refuses the hook beyond the maximum', () => {
  const sandbox = loadApp();
  sandbox._postShipHooks = Array.from({ length: 20 }, (_, i) => ({ name: 'h' + i, kind: 'command', builtin: '', command: 'x', enabled: false }));
  vm.runInContext("addPostShipHook()", sandbox);
  assert.equal(sandbox._postShipHooks.length, 20, 'the hook beyond the maximum must not be added');
});

test('saving sends the whole hook list as one PUT, with exactly one of builtin or command per hook', async () => {
  const sandbox = loadApp();
  sandbox._calls = [];
  vm.runInContext("fetch = (url, o) => { _calls.push([url, o && o.method, o && o.body]); return Promise.resolve({ ok: true }); };", sandbox);
  sandbox._postShipHooks = [
    { name: 'registry', kind: 'builtin', builtin: 'registry_row', command: 'stray', enabled: true },
    { name: 'smoke', kind: 'command', builtin: 'notify', command: 'make ship-smoke', enabled: false },
  ];
  await vm.runInContext('savePostShipHooks()', sandbox);
  await new Promise(r => setImmediate(r));
  assert.equal(sandbox._calls.length, 1, 'exactly one PUT: ' + JSON.stringify(sandbox._calls));
  const [url, method, body] = sandbox._calls[0];
  assert.equal(url, '/api/config');
  assert.equal(method, 'PUT');
  assert.deepEqual(JSON.parse(body), {
    'autonomous.merge_pipeline.post_ship.hooks': [
      { name: 'registry', builtin: 'registry_row', command: '', enabled: true },
      { name: 'smoke', builtin: '', command: 'make ship-smoke', enabled: false },
    ],
  });
});

test('a rejected save surfaces the daemon message instead of guessing', async () => {
  const sandbox = loadApp();
  sandbox._toasts = [];
  vm.runInContext("showToast = (m) => { _toasts.push(m); };", sandbox);
  sandbox._postShipHooks = [{ name: 'x', kind: 'builtin', builtin: 'tag', command: '', enabled: true }];
  vm.runInContext("fetch = () => Promise.resolve({ ok: false, status: 400, json: () => Promise.resolve({ error: 'hook 1: a target is missing' }) });", sandbox);
  await vm.runInContext('savePostShipHooks()', sandbox);
  await new Promise(r => setImmediate(r));
  assert.ok(sandbox._toasts.includes('hook 1: a target is missing'), JSON.stringify(sandbox._toasts));
});

// ── locale keys ─────────────────────────────────────────────────────────────

function ownKeys(b) {
  return Object.keys(b).filter(k => PREFIXES.some(p => k.startsWith(p))).sort();
}

test('the merge_pipeline and settings_merge_pipeline keys are identical in all five bundles', () => {
  const en = ownKeys(bundle('en'));
  assert.ok(en.length >= 40, 'too few keys in en: ' + en.length);
  for (const lang of LANGS) {
    const b = bundle(lang);
    assert.deepEqual(ownKeys(b), en, lang + ' key set differs from en');
    for (const k of en) assert.ok(typeof b[k] === 'string' && b[k].length > 0, k + ' empty in ' + lang);
  }
});

test('the non-English bundles carry real translations', () => {
  const en = bundle('en');
  for (const lang of LANGS.filter(l => l !== 'en')) {
    const b = bundle(lang);
    const same = ownKeys(en).filter(k => b[k] === en[k]);
    assert.ok(same.length <= 3, lang + ' leaves English text in: ' + same.join(','));
  }
});

test('placeholders match the English text in every bundle', () => {
  const en = bundle('en');
  const ph = s => (s.match(/%\d\$s/g) || []).sort().join();
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of ownKeys(en)) assert.equal(ph(b[k]), ph(en[k]), k + ' placeholders differ in ' + lang);
  }
});

test('no merge pipeline string uses the word PRD', () => {
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of ownKeys(b)) assert.ok(!/\bPRD/i.test(b[k]), k + ' uses PRD in ' + lang);
  }
});

test('every merge_pipeline key the app references exists in all five bundles', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const referenced = new Set();
  for (const m of src.matchAll(/['"`]((?:settings_)?merge_pipeline_[a-z_]+)['"`]/g)) referenced.add(m[1]);
  assert.ok(referenced.size >= 30, 'too few references found: ' + referenced.size);
  // A string that is only the start of other keys is a help-link prefix, not a key.
  const enKeys = Object.keys(bundle('en'));
  for (const k of [...referenced]) if (enKeys.some(e => e !== k && e.startsWith(k))) referenced.delete(k);
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of referenced) assert.ok(typeof b[k] === 'string', k + ' missing in ' + lang);
  }
});

// ── the project_required sentinel ──────────────────────────────────────────

test('the project_required sentinel renders the four no-project strings and the project choice', () => {
  const en = bundle('en');
  const html = render({ project_required: true }, { projects: ['alpha', 'beta'] });
  for (const k of ['merge_pipeline_no_project_title', 'merge_pipeline_no_project_body', 'merge_pipeline_no_project_choose', 'merge_pipeline_no_project_set_base']) {
    assert.ok(html.includes(en[k]), k + ' text missing: ' + html);
  }
  assert.ok(html.includes('id="mergePipelineProject"'), 'project input missing: ' + html);
  assert.ok(html.includes('window.mergePipelineSelectProject(this.value)'), 'project change handler missing: ' + html);
  assert.ok(html.includes('<option value="alpha"></option><option value="beta"></option>'), 'project choices missing: ' + html);
  // The worktree_base_repo line is clickable: a help link to the how-to's
  // "No project yet" section, not just named text.
  assert.ok(html.includes('href="/diagrams.html#docs/howto/merge-pipeline.md#no-project-yet"'), 'worktree_base_repo docs link missing: ' + html);
  assert.ok(html.includes('class="docs-link"'), 'help link element missing: ' + html);
  // The failed state the loader owns is the banner text, not this empty state.
  const failed = String(en.merge_pipeline_failed).replace(/%1\$s/g, '').trim();
  assert.ok(!html.includes(failed.split(':')[0].trim()), 'failure text leaked into the empty state: ' + html);
});

test('the project_required sentinel escapes a hostile project choice', () => {
  const html = render({ project_required: true }, { projects: [HOSTILE] });
  assert.ok(html.includes('<option value="&lt;img src=x onerror=alert(1)&gt;"'), 'hostile project not escaped: ' + html);
  assert.ok(!html.includes(HOSTILE), 'hostile markup rendered unescaped: ' + html);
  assert.ok(!html.includes('<img'), 'an image element was injected');
});

// ── the card URL preselect ──────────────────────────────────────────────────

test('_mergePipelineUrl preselects the first project when none is chosen', () => {
  const sandbox = loadApp();
  sandbox._mergePipelineProjects = ['alpha', 'beta'];
  sandbox._mergePipelineProject = '';
  assert.equal(vm.runInContext('_mergePipelineUrl()', sandbox), '/api/autonomous/merge-pipeline?project=alpha');
});

test('_mergePipelineUrl prefers the chosen project over the preselect', () => {
  const sandbox = loadApp();
  sandbox._mergePipelineProjects = ['alpha', 'beta'];
  sandbox._mergePipelineProject = 'beta';
  assert.equal(vm.runInContext('_mergePipelineUrl()', sandbox), '/api/autonomous/merge-pipeline?project=beta');
});

test('_mergePipelineUrl is the bare endpoint when the project list is empty', () => {
  const sandbox = loadApp();
  sandbox._mergePipelineProjects = [];
  sandbox._mergePipelineProject = '';
  assert.equal(vm.runInContext('_mergePipelineUrl()', sandbox), '/api/autonomous/merge-pipeline');
});

// ── the loader fetch wrapper ────────────────────────────────────────────────

// mergePipelineFetchOf instantiates the real _automationMergePipelineLoader in
// a fresh sandbox and returns the fetch the loader passed to listLoader, which
// is _mergePipelineFetch: the apiFetch wrapper that turns the daemon's
// project_required refusal into the sentinel view and rethrows everything else.
async function mergePipelineFetchOf(stubSrc) {
  const sandbox = loadApp();
  // The constructor kicks off the one-time list reads; make them settle to
  // nothing so no state the test does not set leaks in.
  vm.runInContext('apiFetch = () => Promise.resolve({ llms: [], projects: [] }); ' + stubSrc + ';', sandbox);
  vm.runInContext('_automationMergePipelineLoader();', sandbox);
  const ctl = sandbox._listLoaders.automation_merge_pipeline;
  const doFetch = ctl && ctl.state && ctl.state.opts && ctl.state.opts.fetch;
  assert.ok(typeof doFetch === 'function', 'the loader did not pass a fetch');
  return doFetch('/api/autonomous/merge-pipeline', {});
}

test('the loader resolves the project_required refusal to the sentinel view', async () => {
  const view = await mergePipelineFetchOf(
    'fetchStubRej = new Error(JSON.stringify({ code: "project_required" })); apiFetch = () => Promise.reject(fetchStubRej)');
  assert.equal(view.project_required, true);
  assert.equal((Object.keys(view) || []).join(','), 'project_required', JSON.stringify(view));
});

test('the loader rethrows any other rejection as the same error', async () => {
  const sandbox = loadApp();
  vm.runInContext('boom = new Error("500 Internal Server Error"); apiFetch = () => Promise.reject(boom);', sandbox);
  vm.runInContext('_automationMergePipelineLoader();', sandbox);
  const doFetch = sandbox._listLoaders.automation_merge_pipeline.state.opts.fetch;
  await assert.rejects(
    Promise.resolve(doFetch('/api/autonomous/merge-pipeline', {})),
    e => e === sandbox.boom, 'the rejection was not rethrown unchanged');
});

// ── post-ship locale keys ───────────────────────────────────────────────────

const POST_SHIP_PREFIXES = ['settings_post_ship_', 'post_ship_'];

function postShipKeys(b) {
  return Object.keys(b).filter(k => POST_SHIP_PREFIXES.some(p => k.startsWith(p))).sort();
}

test('the post_ship and settings_post_ship keys are identical in all five bundles', () => {
  const en = postShipKeys(bundle('en'));
  assert.ok(en.length >= 25, 'too few keys in en: ' + en.length);
  for (const lang of LANGS) {
    const b = bundle(lang);
    assert.deepEqual(postShipKeys(b), en, lang + ' key set differs from en');
    for (const k of en) assert.ok(typeof b[k] === 'string' && b[k].length > 0, k + ' empty in ' + lang);
  }
});

test('the post-ship strings are translated in the non-English bundles', () => {
  const en = bundle('en');
  for (const lang of LANGS.filter(l => l !== 'en')) {
    const b = bundle(lang);
    const same = postShipKeys(en).filter(k => b[k] === en[k]);
    assert.ok(same.length <= 3, lang + ' leaves English text in: ' + same.join(','));
  }
});

test('no post-ship string uses the word PRD', () => {
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of postShipKeys(b)) assert.ok(!/\bPRD/i.test(b[k]), k + ' uses PRD in ' + lang);
  }
});

test('every post_ship key the app references exists in all five bundles', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const referenced = new Set();
  for (const m of src.matchAll(/t\(\s*['"`]((?:settings_)?post_ship_[a-z_]+)['"`]/g)) referenced.add(m[1]);
  for (const m of src.matchAll(/(?:labelKey|hintKey|docsKey):\s*['"`]((?:settings_)?post_ship_[a-z_]+)['"`]/g)) referenced.add(m[1]);
  assert.ok(referenced.size >= 20, 'too few references found: ' + referenced.size);
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of referenced) assert.ok(typeof b[k] === 'string', k + ' missing in ' + lang);
  }
});

// ── release approval locale keys ────────────────────────────────────────────

function releaseKeys(b) {
  return Object.keys(b).filter(k => k.includes('release_approval')).sort();
}

test('the release_approval keys are identical in all five bundles, with a value in each', () => {
  const en = releaseKeys(bundle('en'));
  assert.ok(en.length >= 13, 'too few release_approval keys in en: ' + en.length);
  for (const lang of LANGS) {
    const b = bundle(lang);
    assert.deepEqual(releaseKeys(b), en, lang + ' release_approval key set differs from en');
    for (const k of en) assert.ok(typeof b[k] === 'string' && b[k].length > 0, k + ' empty in ' + lang);
  }
});

test('the release_approval strings are translated in the non-English bundles', () => {
  const en = bundle('en');
  for (const lang of LANGS.filter(l => l !== 'en')) {
    const b = bundle(lang);
    const same = releaseKeys(en).filter(k => b[k] === en[k]);
    assert.deepEqual(same, [], lang + ' leaves English text in');
  }
});

test('no release_approval string uses the word PRD or a tracker identifier', () => {
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of releaseKeys(b)) {
      assert.ok(!/\bPRD/i.test(b[k]), k + ' uses PRD in ' + lang);
      assert.ok(!/\b(BL|GH|SEC|HLLM)-?\d+\b/.test(b[k]), k + ' carries a tracker ID in ' + lang);
    }
  }
});

test('every release_approval key the app references exists in all five bundles', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const referenced = new Set();
  for (const m of src.matchAll(/['"`]((?:settings_|autonomous\.merge_pipeline\.)?(?:merge_pipeline_)?release_approval[a-z_.]*)['"`]/g)) referenced.add(m[1]);
  // The field keys are settings keys, not locale keys: keep only the locale ones.
  // A string that is only the start of other keys is a help-link prefix, not a key.
  const enKeys = Object.keys(bundle('en'));
  for (const k of [...referenced]) {
    if (k.startsWith('autonomous.') || k.endsWith('.')) referenced.delete(k);
    else if (enKeys.some(e => e !== k && e.startsWith(k))) referenced.delete(k);
  }
  assert.ok(referenced.size >= 10, 'too few references found: ' + referenced.size);
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of referenced) assert.ok(typeof b[k] === 'string', k + ' missing in ' + lang);
  }
});

// ── the stats-poll rule ─────────────────────────────────────────────────────

test('the card stores no setInterval on a DOM element', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const start = src.indexOf('// ── Merge pipeline card');
  const end = src.indexOf('// ── Triage card');
  assert.ok(start > 0 && end > start);
  assert.ok(!/setInterval/.test(src.slice(start, end)), 'the card must not start a timer of its own');
});

// ── merge lock and last validator lines ─────────────────────────────────────

const LOCK = { repo: '/repo', run_id: 'run-1', automaton_id: 'auto-1', branches: ['feat/x', 'feat/y'], since: '2026-11-08T10:00:00Z' };
const PASS_V = { pass: true, version: '9.1.0', checked_at: '2026-11-08T11:00:00Z', checks: [
  { name: 'version_variables_equal', pass: true, detail: '' },
  { name: 'tag_not_taken', pass: true, detail: '' },
] };
const FAIL_V = { pass: false, version: '9.1.0', checked_at: '2026-11-08T11:00:00Z', checks: [
  { name: 'version_variables_equal', pass: true, detail: '' },
  { name: 'tag_not_taken', pass: false, detail: 'tag v9.1.0 already exists' },
  { name: 'readme_line_matches', pass: false, detail: '' },
] };

test('no merge lock and no validator result renders no status block', () => {
  const bare = render(view());
  assert.ok(!bare.includes('merge-status'), bare);
  assert.equal(render(view(), { mergeStatus: {} }), bare);
  assert.equal(render(view(), { mergeStatus: null }), bare);
  assert.equal(render(view(), { mergeStatus: { merge_lock: null, last_validator: null } }), bare);
});

test('a held merge lock renders one line naming the Automaton, branches and since', () => {
  const html = render(view(), { mergeStatus: { merge_lock: LOCK } });
  assert.ok(html.includes('class="merge-status-lock"'), html);
  assert.ok(html.includes('Merge lock held: Automaton auto-1, branches feat/x, feat/y, since '), html);
  assert.ok(!html.includes('merge-status-validator'), html);
  assert.ok(html.includes(DEFS + 'merge-lock'), 'the lock line links to the merge lock entry');
  assert.equal(html.split('class="merge-status-lock"').length - 1, 1);
});

test('a failing validator renders its version and each failing check', () => {
  const html = render(view(), { mergeStatus: { last_validator: FAIL_V } });
  assert.ok(html.includes('class="merge-status-validator"'), html);
  assert.ok(html.includes('Last release check failed: version 9.1.0, '), html);
  assert.ok(html.includes('Failed check tag_not_taken: tag v9.1.0 already exists'), html);
  assert.ok(html.includes('Failed check readme_line_matches'), html);
  assert.ok(!html.includes('Failed check version_variables_equal'), 'passing checks are not listed');
  assert.equal(html.split('class="merge-validator-check"').length - 1, 2);
  assert.ok(html.includes(DEFS + 'release-bump-validator'), 'the validator line links to the validator entry');
});

test('a passing validator renders one line with no failing checks', () => {
  const html = render(view(), { mergeStatus: { last_validator: PASS_V } });
  assert.ok(html.includes('Last release check passed: version 9.1.0, '), html);
  assert.ok(!html.includes('merge-validator-check'), html);
  assert.ok(!html.includes('merge-status-lock'), html);
});

test('the lock and the validator render together, lock first', () => {
  const html = render(view(), { mergeStatus: { merge_lock: LOCK, last_validator: PASS_V } });
  assert.ok(html.indexOf('merge-status-lock') > 0 && html.indexOf('merge-status-lock') < html.indexOf('merge-status-validator'), html);
});

test('merge lock and validator values from the API are escaped', () => {
  const html = render(view(), { mergeStatus: {
    merge_lock: { ...LOCK, automaton_id: HOSTILE, branches: [HOSTILE] },
    last_validator: { ...FAIL_V, version: HOSTILE, checks: [{ name: HOSTILE, pass: false, detail: HOSTILE }] },
  } });
  assert.ok(!html.includes(HOSTILE), html);
  assert.ok(html.includes('&lt;img src=x'), html);
});

test('the merge lock and validator keys link to their definitions entries', () => {
  assert.ok(docsLink('merge_lock_line').endsWith(DEFS + 'merge-lock'), docsLink('merge_lock_line'));
  assert.ok(docsLink('merge_validator_check').endsWith(DEFS + 'release-bump-validator'), docsLink('merge_validator_check'));
});

test('every merge_lock_ and merge_validator_ key the app references exists in all five bundles', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const referenced = new Set();
  for (const m of src.matchAll(/['"`](merge_(?:lock|validator)_[a-z_]+)['"`]/g)) referenced.add(m[1]);
  assert.ok(referenced.size >= 8, 'too few references found: ' + referenced.size);
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of referenced) assert.ok(typeof b[k] === 'string' && b[k] !== '', k + ' missing in ' + lang);
  }
});

test('no merge lock or validator string uses the word PRD', () => {
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of Object.keys(b).filter(k => k.startsWith('merge_lock_') || k.startsWith('merge_validator_'))) {
      assert.ok(!/\bPRD/i.test(b[k]), k + ' uses PRD in ' + lang);
    }
  }
});

// ── conflict confirmation ───────────────────────────────────────────────────

function confirmation(over) {
  return Object.assign({ mode: 'on', confirm_first_n: 5, counted: 2, remaining: 3, enabled: true, error: '' }, over || {});
}

test('conflict confirmation line renders the count and the remaining when enabled and counted is a number', () => {
  const html = render(view({ conflict_confirmation: confirmation() }));
  assert.ok(html.includes('class="merge-pipeline-confirmation"'), 'confirmation line missing: ' + html);
  assert.ok(html.includes('2 conflicts confirmed'), 'count missing: ' + html);
  assert.ok(html.includes('3 confirmations remaining'), 'remaining missing: ' + html);
  assert.ok(!html.includes('{count}') && !html.includes('{remaining}'), 'placeholder left unfilled: ' + html);
  assert.ok(html.includes('Conflict confirmations'), 'label missing: ' + html);
});

test('conflict confirmation line shows a zero count when enabled', () => {
  const html = render(view({ conflict_confirmation: confirmation({ counted: 0, remaining: 5 }) }));
  assert.ok(html.includes('0 conflicts confirmed'), html);
});

test('conflict confirmation line omits the remaining part when it is not a number', () => {
  const html = render(view({ conflict_confirmation: confirmation({ remaining: null }) }));
  assert.ok(html.includes('2 conflicts confirmed'), html);
  assert.ok(!html.includes('confirmations remaining'), 'remaining shown without a number: ' + html);
});

test('conflict confirmation renders nothing when confirmation is off', () => {
  const html = render(view({ conflict_confirmation: confirmation({ enabled: false, mode: 'shadow', confirm_first_n: 5 }) }));
  assert.ok(!html.includes('merge-pipeline-confirmation'), 'line rendered while off: ' + html);
  assert.ok(!html.includes('conflicts confirmed'), html);
});

test('conflict confirmation renders nothing when the count is empty or never a number', () => {
  for (const counted of [null, undefined, 'never', '2']) {
    const html = render(view({ conflict_confirmation: confirmation({ counted }) }));
    assert.ok(!html.includes('merge-pipeline-confirmation'), 'line rendered for counted=' + String(counted) + ': ' + html);
  }
});

test('conflict confirmation renders nothing when the block is missing', () => {
  const html = render(view({}));
  assert.ok(!html.includes('merge-pipeline-confirmation'), html);
});

test('the conflict mode and confirm-first-N settings link to their how-to sections', () => {
  assert.equal(docsLink('autonomous.merge_pipeline.conflict_mode'), 'howto/merge-pipeline.md#conflict-mode');
  assert.equal(docsLink('autonomous.merge_pipeline.conflict_confirm_first_n'), 'howto/merge-pipeline.md#confirm-the-first-n-model-conflicts');
  assert.equal(docsLink('settings_merge_pipeline_conflict_mode'), 'howto/merge-pipeline.md#conflict-mode');
});
