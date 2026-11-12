// Pins the three daemon task settings on the Settings > General > Automata
// card: the per-template task mode control (one select per built-in template
// id, daemon offered only for the daemon-capable ids), the script gate time
// cap (a number, 0 = no cap, default 900) and the project gate command (a
// text line). Each has a '?' help entry linking the daemon task modes how-to,
// a refused write shows the daemon's own text, and every label, help and
// option string exists in all five locale bundles.
//
// Tests run app.js's real functions in the shared stub browser. The daemon is
// a stub fetch that answers 200 or 400 with plain text, as PUT /api/config does.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const LOCALE_DIR = path.join(__dirname, 'locales');
const APP_JS = path.join(__dirname, 'app.js');
const HOWTO = 'howto/automaton-daemon-task-modes.md';

const MODE_KEY = 'autonomous.task_templates.mode';
const TIMEOUT_KEY = 'autonomous.script_gate_timeout_seconds';
const GATE_KEY = 'autonomous.project_gate_command';

const ALL_TEMPLATE_IDS = ['changelog_entry', 'parity_issue_draft', 'docs_set', 'bounded_load_step', 'final_gate', 'version_bump_left_to_merge'];
const DAEMON_IDS = ['changelog_entry', 'parity_issue_draft', 'bounded_load_step', 'version_bump_left_to_merge'];

// A select stand-in: value, a dataset for the last saved mode, and no DOM.
function makeSelect(value, saved) {
  return { value, dataset: { saved: saved === undefined ? value : saved } };
}

// An input stand-in: value plus defaultValue (the saved value, as in the DOM).
function makeInput(value, defaultValue) {
  return { value, defaultValue: defaultValue === undefined ? value : defaultValue };
}

function load({ fetchImpl } = {}) {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox.__toasts = [];
  sandbox.showToast = function (msg, type) { sandbox.__toasts.push({ msg: String(msg), type }); };
  sandbox.__calls = [];
  sandbox.fetch = function (url, opts) {
    sandbox.__calls.push({ url, opts: opts || {} });
    if (fetchImpl) return fetchImpl(url, opts || {});
    return Promise.resolve({ ok: true, status: 200, text: async () => '' });
  };
  return sandbox;
}

function run(sandbox, expr) {
  return vm.runInContext(expr, sandbox);
}

function bundle(locale) {
  return JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, locale + '.json'), 'utf8'));
}

function autonomousFields(sandbox) {
  return JSON.parse(run(sandbox,
    'JSON.stringify(GENERAL_CONFIG_FIELDS.find(s => s.id === "autonomous").fields)'));
}

function field(sandbox, key) {
  const f = autonomousFields(sandbox).find(x => x.key === key);
  assert.ok(f, key + ' missing from the autonomous settings card');
  return f;
}

function refusal(sandbox, text) {
  sandbox.fetch = function (url, opts) {
    sandbox.__calls.push({ url, opts: opts || {} });
    return Promise.resolve({ ok: false, status: 400, text: async () => text });
  };
}

function sentBody(sandbox, index) {
  return JSON.parse(sandbox.__calls[index].opts.body);
}

// ── fields ───────────────────────────────────────────────────────────────────

test('the autonomous card carries the task mode, script gate and project gate fields', () => {
  const sandbox = load();
  const keys = autonomousFields(sandbox).map(f => f.key);
  for (const k of [MODE_KEY, TIMEOUT_KEY, GATE_KEY]) {
    assert.ok(keys.includes(k), k + ' is not on the autonomous card');
  }
  assert.equal(field(sandbox, MODE_KEY).type, 'task_template_modes');
  assert.equal(field(sandbox, TIMEOUT_KEY).type, 'number');
  assert.equal(field(sandbox, GATE_KEY).type, 'text');
});

test('the script gate time cap is a whole-number field, minimum 0, default 900', () => {
  const sandbox = load();
  const f = field(sandbox, TIMEOUT_KEY);
  assert.equal(f.min, 0);
  assert.equal(f.step, 1);
  assert.equal(f.default, '900');
  assert.equal(f.placeholder, '900');
  assert.match(f.onchange, /saveDaemonTaskNumberField\('autonomous\.script_gate_timeout_seconds', this\)/);
});

test('the project gate command saves through its own text handler', () => {
  const sandbox = load();
  const f = field(sandbox, GATE_KEY);
  assert.match(f.onchange, /saveDaemonTaskTextField\('autonomous\.project_gate_command', this\)/);
});

test('each of the three fields has a label key, a help key and the daemon task help link', () => {
  const sandbox = load();
  for (const k of [MODE_KEY, TIMEOUT_KEY, GATE_KEY]) {
    const f = field(sandbox, k);
    assert.ok(f.labelKey, k + ' needs a labelKey');
    assert.ok(f.helpKey, k + ' needs a helpKey');
    assert.equal(f.helpLabelKey, 'settings_daemon_task_help_link_label', k + ' help link label');
  }
});

// ── help entries ─────────────────────────────────────────────────────────────

test('each field shows a ? help entry that opens the daemon task modes how-to', () => {
  const sandbox = load();
  for (const k of [MODE_KEY, TIMEOUT_KEY, GATE_KEY]) {
    const f = field(sandbox, k);
    assert.equal(f.helpDocs, HOWTO, k + ' helpDocs');
    sandbox.__f = f;
    const html = run(sandbox, 'settingsHelpLink(__f)');
    assert.ok(html.includes('href="/diagrams.html#docs/' + HOWTO + '"'), 'no ? link for ' + k);
    assert.ok(html.includes('>?</a>'), 'the ? glyph is missing for ' + k);
  }
});

test('the how-to that the help links open exists, with front matter and its sections', () => {
  const doc = fs.readFileSync(path.join(__dirname, '..', '..', '..', 'docs', HOWTO), 'utf8');
  assert.match(doc, /^---\n/, 'front matter missing');
  for (const h of ['## 1. The three modes, per template', '## 4. The script gate', '## 5. The time cap', '## 6. The project gate command', '## 8. What a refused write looks like']) {
    assert.ok(doc.includes(h), 'section "' + h + '" missing from the how-to');
  }
});

// ── mode control ─────────────────────────────────────────────────────────────

function modeHTML(sandbox, stored) {
  sandbox.__f = field(sandbox, MODE_KEY);
  sandbox.__v = stored;
  return run(sandbox, 'taskTemplateModesHTML(__f, "Modes", __v)');
}

function selectsIn(html) {
  const out = {};
  const re = /<select[^>]*data-daemon-task-template="([^"]+)"[^>]*>([\s\S]*?)<\/select>/g;
  let m;
  while ((m = re.exec(html))) {
    out[m[1]] = {
      tag: m[0],
      options: [...m[2].matchAll(/<option value="([^"]*)"([^>]*)>/g)].map(o => ({ value: o[1], selected: /\bselected\b/.test(o[2]) })),
    };
  }
  return out;
}

test('the mode control renders one select per built-in template id', () => {
  const sandbox = load();
  const sel = selectsIn(modeHTML(sandbox, {}));
  assert.deepEqual(Object.keys(sel).sort(), [...ALL_TEMPLATE_IDS].sort());
});

test('daemon is offered only for the daemon-capable ids; final_gate gets llm and off', () => {
  const sandbox = load();
  const sel = selectsIn(modeHTML(sandbox, {}));
  for (const id of ALL_TEMPLATE_IDS) {
    const values = sel[id].options.map(o => o.value);
    if (DAEMON_IDS.includes(id)) {
      assert.deepEqual(values, ['llm', 'daemon', 'off'], id + ' options');
    } else {
      assert.deepEqual(values, ['llm', 'off'], id + ' options');
    }
  }
  assert.ok(!selectsIn(modeHTML(sandbox, { final_gate: 'daemon' })).final_gate.options.some(o => o.value === 'daemon'));
});

test('a stored mode is the selected option and an unstored template takes its default', () => {
  const sandbox = load();
  const sel = selectsIn(modeHTML(sandbox, { changelog_entry: 'off', docs_set: 'llm' }));
  assert.ok(sel.changelog_entry.options.find(o => o.value === 'off').selected, 'stored off not selected');
  assert.ok(sel.docs_set.options.find(o => o.value === 'llm').selected, 'stored llm not selected');
  assert.ok(sel.parity_issue_draft.options.find(o => o.value === 'daemon').selected, 'daemon default not selected');
  assert.ok(sel.final_gate.options.find(o => o.value === 'llm').selected, 'final_gate default not selected');
});

test('each mode select saves its own dotted key on change', () => {
  const sandbox = load();
  const html = modeHTML(sandbox, {});
  const sel = selectsIn(html);
  assert.match(sel.docs_set.tag, /onchange="saveDaemonTaskModeSetting\('docs_set', this\)"/);
  assert.match(sel.changelog_entry.tag, /onchange="saveDaemonTaskModeSetting\('changelog_entry', this\)"/);
});

test('a mode change PUTs autonomous.task_templates.mode.<id> with the chosen mode', async () => {
  const sandbox = load();
  sandbox.__el = makeSelect('off', 'daemon');
  run(sandbox, 'saveDaemonTaskModeSetting("changelog_entry", __el)');
  await flushAsync();
  assert.equal(sandbox.__calls.length, 1);
  assert.equal(sandbox.__calls[0].url, '/api/config');
  assert.equal(sandbox.__calls[0].opts.method, 'PUT');
  assert.deepEqual(sentBody(sandbox, 0), { [MODE_KEY + '.changelog_entry']: 'off' });
  assert.equal(sandbox.__el.dataset.saved, 'off', 'a saved mode becomes the new baseline');
});

test('a refused mode shows the daemon refusal text and puts the select back', async () => {
  const sandbox = load();
  refusal(sandbox, 'autonomous.task_templates.mode[final_gate]: daemon is not allowed for final_gate (allowed values: llm, off)');
  sandbox.__el = makeSelect('daemon', 'llm');
  run(sandbox, 'saveDaemonTaskModeSetting("final_gate", __el)');
  await flushAsync();
  assert.equal(sandbox.__toasts.length, 1);
  assert.equal(sandbox.__toasts[0].type, 'error');
  assert.equal(sandbox.__toasts[0].msg, 'autonomous.task_templates.mode[final_gate]: daemon is not allowed for final_gate (allowed values: llm, off)');
  assert.equal(sandbox.__el.value, 'llm', 'select was not restored to the saved mode');
});

// ── script gate time cap ─────────────────────────────────────────────────────

test('a whole-number script gate cap is saved as a number', async () => {
  const sandbox = load();
  sandbox.__el = makeInput('0', '900');
  run(sandbox, 'saveDaemonTaskNumberField("' + TIMEOUT_KEY + '", __el)');
  await flushAsync();
  assert.deepEqual(sentBody(sandbox, 0), { [TIMEOUT_KEY]: 0 });
  assert.equal(sandbox.__el.defaultValue, '0', 'a saved value becomes the new baseline');
});

test('a negative script gate cap is sent and the daemon refusal text is shown', async () => {
  const sandbox = load();
  refusal(sandbox, 'autonomous.script_gate_timeout_seconds: must be a whole number of seconds >= 0 (0 = no cap)');
  sandbox.__el = makeInput('-5', '900');
  run(sandbox, 'saveDaemonTaskNumberField("' + TIMEOUT_KEY + '", __el)');
  await flushAsync();
  assert.deepEqual(sentBody(sandbox, 0), { [TIMEOUT_KEY]: -5 });
  assert.equal(sandbox.__toasts.length, 1);
  assert.equal(sandbox.__toasts[0].msg, 'autonomous.script_gate_timeout_seconds: must be a whole number of seconds >= 0 (0 = no cap)');
  assert.equal(sandbox.__toasts[0].type, 'error');
  assert.equal(sandbox.__el.value, '900', 'box was not restored to the saved value');
});

test('an empty script gate box is reported locally and nothing is sent', async () => {
  const sandbox = load();
  sandbox.__el = makeInput('', '900');
  run(sandbox, 'saveDaemonTaskNumberField("' + TIMEOUT_KEY + '", __el)');
  await flushAsync();
  assert.equal(sandbox.__calls.length, 0, 'an empty box must not reach the daemon');
  assert.equal(sandbox.__toasts.length, 1);
  assert.equal(sandbox.__toasts[0].type, 'error');
  assert.equal(sandbox.__el.value, '900');
});

// ── project gate command ─────────────────────────────────────────────────────

test('a project gate command is saved as one trimmed line', async () => {
  const sandbox = load();
  sandbox.__el = makeInput('  make test  ', '');
  run(sandbox, 'saveDaemonTaskTextField("' + GATE_KEY + '", __el)');
  await flushAsync();
  assert.deepEqual(sentBody(sandbox, 0), { [GATE_KEY]: 'make test' });
});

test('an empty project gate command clears it', async () => {
  const sandbox = load();
  sandbox.__el = makeInput('', 'make test');
  run(sandbox, 'saveDaemonTaskTextField("' + GATE_KEY + '", __el)');
  await flushAsync();
  assert.deepEqual(sentBody(sandbox, 0), { [GATE_KEY]: '' });
});

test('a refused project gate command (multi-line) shows the daemon refusal text', async () => {
  const sandbox = load();
  const msg = 'autonomous.project_gate_command: a command must be one line (no line break)';
  refusal(sandbox, msg);
  sandbox.__el = makeInput('make test\nmake lint', 'make test');
  run(sandbox, 'saveDaemonTaskTextField("' + GATE_KEY + '", __el)');
  await flushAsync();
  assert.equal(sandbox.__toasts.length, 1);
  assert.equal(sandbox.__toasts[0].msg, msg);
  assert.equal(sandbox.__toasts[0].type, 'error');
  assert.equal(sandbox.__el.value, 'make test', 'text box was not restored');
});

test('a network failure shows the generic save failure and no refusal', async () => {
  const sandbox = load();
  sandbox.fetch = function () { return Promise.reject(new Error('offline')); };
  sandbox.__el = makeInput('make test', '');
  run(sandbox, 'saveDaemonTaskTextField("' + GATE_KEY + '", __el)');
  await flushAsync();
  assert.equal(sandbox.__toasts.length, 1);
  assert.equal(sandbox.__toasts[0].type, 'error');
  assert.equal(sandbox.__toasts[0].msg, 'settings_daemon_task_save_failed');
});

// ── locale parity ────────────────────────────────────────────────────────────

function daemonTaskKeysUsedInApp() {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const keys = new Set(['settings_daemon_task_help_link_label']);
  for (const m of src.matchAll(/'(settings_daemon_[a-z_]*[a-z])'/g)) keys.add(m[1]);
  for (const id of ALL_TEMPLATE_IDS) keys.add('settings_daemon_task_template_' + id);
  for (const m of ['llm', 'daemon', 'off']) keys.add('settings_daemon_task_mode_' + m);
  return [...keys];
}

test('every daemon task label, help and option key exists in all five locale bundles', () => {
  const keys = daemonTaskKeysUsedInApp();
  assert.equal(keys.length, 19, 'the daemon task key set changed size: ' + keys.join(', '));
  for (const l of LOCALES) {
    const b = bundle(l);
    for (const k of keys) {
      assert.ok(typeof b[k] === 'string' && b[k].trim().length > 0, k + ' missing or empty in ' + l);
    }
  }
});

test('the five locale bundles carry the same daemon task keys', () => {
  const en = Object.keys(bundle('en')).filter(k => k.startsWith('settings_daemon_'));
  for (const l of LOCALES) {
    const got = Object.keys(bundle(l)).filter(k => k.startsWith('settings_daemon_')).sort();
    assert.deepEqual(got, [...en].sort(), 'daemon task key set differs in ' + l);
  }
});

test('daemon task strings carry no tracker identifiers and no PRD wording', () => {
  for (const l of LOCALES) {
    const b = bundle(l);
    for (const k of Object.keys(b).filter(x => x.startsWith('settings_daemon_'))) {
      assert.ok(!/\bPRD\b/.test(b[k]), k + ' in ' + l + ' says PRD');
      assert.ok(!/\b(BL|GH|SEC|HLLM)-?\d+\b/.test(b[k]), k + ' in ' + l + ' has a tracker id');
    }
  }
});

test('no daemon task setting key or help text names a tracker identifier or PRD', () => {
  const sandbox = load();
  for (const f of autonomousFields(sandbox).filter(x => /daemon|script_gate|project_gate/.test(x.key))) {
    assert.ok(!/PRD|\b(BL|GH|SEC|HLLM)-?\d+\b/.test(f.key + ' ' + (f.label || '')), f.key + ' names a tracker id or PRD');
  }
});
