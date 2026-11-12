// Priority source in the web UI.
//
// priorityBadgeHtml renders the capacity-queue priority together with the
// source that set it (explicit | kind | default) when the object carries
// one: "Priority 80 · kind". Without a source the badge renders exactly as
// before (legacy "Priority N", hidden at 0), so sessions and bodies
// without a priority record are unchanged. The Automaton row and the
// capacity card's waiting rows use the same helper. The autonomous
// settings panel exposes autonomous.priority_by_kind (the kind → priority
// table, edited as kind=priority text pairs) and
// autonomous.defect_intake_automation. Output is escaped, and all five
// locale bundles carry the new keys.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');
const LOCALE_LIST = ['en', 'de', 'es', 'fr', 'ja'];
const LOCALE_DIR = path.join(__dirname, 'locales');

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, 'en.json'), 'utf8'));
  return sandbox;
}

function sess(extra) {
  return Object.assign({
    id: 'ab12', full_id: 'host-ab12', state: 'running', task: 'build it',
    updated_at: '2026-11-01T12:00:00Z',
  }, extra || {});
}

function callBadge(priority, source) {
  const sb = loadApp();
  sb._p = priority;
  sb._s = source;
  return vm.runInContext('priorityBadgeHtml(_p, _s)', sb);
}

function renderCapacity(cap, opts) {
  const sb = loadApp();
  sb._cap = cap;
  sb._opts = opts || {};
  return vm.runInContext('window.capacityCardHtml(_cap, _opts)', sb);
}

test('no priority and no source render no badge', () => {
  assert.equal(callBadge(undefined, undefined), '');
  assert.equal(callBadge(0, ''), '');
  assert.equal(callBadge('<img src=x onerror=alert(1)>', ''), '');
});

test('a non-zero priority without a source renders exactly as before', () => {
  const html = callBadge(7, undefined);
  assert.match(html, /class="priority-badge"/);
  assert.match(html, />Priority 7</);
  assert.doesNotMatch(html, /explicit|kind|default/);
});

test('a negative priority without a source is shown', () => {
  assert.match(callBadge(-3, ''), />Priority -3</);
});

test('source explicit shows the value and the source label', () => {
  const html = callBadge(1, 'explicit');
  assert.match(html, /class="priority-badge"/);
  assert.match(html, />Priority 1 · explicit</);
});

test('source kind on a table value shows the value and the source label', () => {
  assert.match(callBadge(80, 'kind'), />Priority 80 · kind</);
});

test('an explicit 0-pin still renders with its source', () => {
  // A priority pinned to 0 reads back without the priority field; the
  // source alone marks the record.
  assert.match(callBadge(0, 'explicit'), />Priority 0 · explicit</);
  assert.match(callBadge(null, 'explicit'), />Priority 0 · explicit</);
});

test('the value and the source label are escaped', () => {
  const sb = loadApp();
  sb._i18n.bundle.priority_source_badge = '<b>Q %1$d</b> · %2$s';
  sb._i18n.bundle.priority_source_kind = '<i>K</i>';
  sb._p = 80;
  sb._s = 'kind';
  const html = vm.runInContext('priorityBadgeHtml(_p, _s)', sb);
  assert.doesNotMatch(html, /<b>Q 80<\/b>/);
  assert.doesNotMatch(html, /<i>K<\/i>/);
  assert.match(html, /&lt;b&gt;Q 80&lt;\/b&gt;/);
});

test('a session badge renders the source when the session carries one', () => {
  const sb = loadApp();
  const withSource = sb.sessionCard(sess({ priority: 7, priority_source: 'explicit' }), 0, 1);
  assert.match(withSource, /class="priority-badge"/);
  assert.match(withSource, />Priority 7 · explicit</);
  // A session without a source is unchanged.
  const legacy = sb.sessionCard(sess({ priority: 7 }), 0, 1);
  assert.match(legacy, />Priority 7</);
  assert.doesNotMatch(legacy, /·/);
});

test('the Automaton row carries the priority badge with its source', () => {
  const sb = loadApp();
  const html = vm.runInContext(
    "renderPRDRow({id: 'e3db', title: 'T', status: 'draft', priority: 80, priority_source: 'kind'}, {})",
    sb);
  assert.match(html, /class="priority-badge"/);
  assert.match(html, />Priority 80 · kind</);
});

test('an Automaton row without a priority record shows no badge', () => {
  const sb = loadApp();
  const html = vm.runInContext(
    "renderPRDRow({id: 'e3db', title: 'T', status: 'draft'}, {})",
    sb);
  assert.doesNotMatch(html, /priority-badge/);
});

test('a capacity-waiting row shows the badge with the source', () => {
  const html = renderCapacity({
    waiting: [{ holder: 'feedbeef9999', since: '2026-11-01T10:00:00Z',
                priority: 80, priority_source: 'kind' }],
  });
  assert.match(html, /class="priority-badge"/);
  assert.match(html, />Priority 80 · kind</);
});

test('a capacity-waiting row without a priority record is unchanged', () => {
  const html = renderCapacity({
    waiting: [{ holder: 'feedbeef9999', since: '2026-11-01T10:00:00Z' }],
  });
  assert.doesNotMatch(html, /priority-badge/);
});

test('autonomous settings expose priority_by_kind and defect_intake_automation', () => {
  const sb = loadApp();
  const json = vm.runInContext(
    "JSON.stringify(((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields)" +
    ".filter(f => f.key === 'autonomous.priority_by_kind' || f.key === 'autonomous.defect_intake_automation'))",
    sb);
  const fields = JSON.parse(json);
  const pbk = fields.find(f => f.key === 'autonomous.priority_by_kind');
  const dia = fields.find(f => f.key === 'autonomous.defect_intake_automation');
  assert.ok(pbk, 'autonomous.priority_by_kind missing from autonomous fields');
  assert.equal(pbk.type, 'priority_by_kind');
  assert.equal(pbk.labelKey, 'settings_priority_by_kind');
  assert.ok(pbk.hintKey, 'priority_by_kind hint key missing');
  assert.ok(dia, 'autonomous.defect_intake_automation missing from autonomous fields');
  assert.equal(dia.type, 'text');
  assert.equal(dia.labelKey, 'settings_defect_intake_automation');
  assert.ok(dia.hintKey, 'defect intake hint key missing');
});

test('_priorityByKindTableText renders the table as kind=priority pairs', () => {
  const sb = loadApp();
  sb._v = { feature: 20, bug: 80, maintenance: 70 };
  assert.equal(vm.runInContext('_priorityByKindTableText(_v)', sb),
    'bug=80, feature=20, maintenance=70');
  sb._v = 'bug=80, other=0';
  assert.equal(vm.runInContext('_priorityByKindTableText(_v)', sb),
    'bug=80, other=0');
  sb._v = null;
  assert.equal(vm.runInContext('_priorityByKindTableText(_v)', sb), '');
});

test('every locale bundle carries the priority-source and settings keys', () => {
  const keys = [
    'priority_source_explicit',
    'priority_source_kind',
    'priority_source_default',
    'priority_source_badge',
    'priority_source_tip',
    'settings_priority_by_kind',
    'settings_priority_by_kind_hint',
    'settings_defect_intake_automation',
    'settings_defect_intake_automation_hint',
  ];
  for (const lang of LOCALE_LIST) {
    const b = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, lang + '.json'), 'utf8'));
    for (const k of keys) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, k + ' missing or empty in ' + lang);
    }
    assert.match(b.priority_source_badge, /%1\$[sd]/, lang + ': value placeholder');
  }
});

test('the english badge template carries both placeholders', () => {
  const en = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, 'en.json'), 'utf8'));
  assert.match(en.priority_source_badge, /%1\$[sd]/);
  assert.match(en.priority_source_badge, /%2\$[sd]/);
});
