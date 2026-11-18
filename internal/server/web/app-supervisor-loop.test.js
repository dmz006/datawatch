// Supervisor loop — PWA side: the dashboard card, the Settings group and the
// locale keys. Loads the real app.js into the stub browser.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const STEPS = ['close_finished_records', 'deploy_after_ship', 'retry_failed_infra', 'cleanup_sweep',
  'apply_answers', 'lane_count', 'start_next_chain_step', 'run_merge_until_conflict'];
const MODEL_JOBS = ['draft_spec', 'classify_ci_failure', 'pick_backend'];

// Every supervisor_loop key, in the order the daemon lists them.
const EXPECTED_KEYS = ['enabled']
  .concat(STEPS.map(s => s + '.enabled'))
  .concat(MODEL_JOBS.reduce((a, j) => a.concat([j + '.style', j + '.backend', j + '.model']), []))
  .concat(['validator_retries', 'escalate_after_failures', 'run_retention', 'archive_cancelled_after_minutes', 'lane_count.min', 'lane_count.max',
    'wake.enabled', 'wake.kinds', 'wake.coalesce_seconds', 'wake.fallback_interval_minutes']);

const JOBS_BODY = {
  enabled: true,
  jobs: [
    { name: 'cleanup_sweep', kind: 'mechanical', enabled: true, last_outcome: 'ok', last_run: '2026-11-05T09:00:00Z' },
    { name: 'draft_spec', kind: 'model', style: 'session', backend: 'pi-prompt', model: 'qwen-test', enabled: true, last_outcome: 'escalated' },
    { name: 'pick_backend', kind: 'model', style: 'oneshot', backend: 'claude-code', model: 'haiku-test', enabled: true, last_outcome: '' },
  ],
};
const RUNS_BODY = {
  runs: [
    { job: 'draft_spec', style: 'session', backend: 'pi-prompt', model: 'qwen-test', outcome: 'escalated', escalations: 1, retries: 2, started: '2026-11-05T09:30:00Z', validator_failures: [] },
    { job: 'cleanup_sweep', outcome: 'ok', escalations: 0, retries: 0, started: '2026-11-05T09:00:00Z', validator_failures: [] },
    { job: 'draft_spec', outcome: 'escalated', escalations: 1, retries: 1, started: '2026-11-05T08:00:00Z', validator_failures: [] },
  ],
};

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

function readLocale(l) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));
}

test('the card renders the jobs and the recent runs from a fixture body', () => {
  const s = loadApp();
  s._j = JOBS_BODY; s._r = RUNS_BODY;
  const html = vm.runInContext('supervisorLoopCardHtml(_j, _r)', s);
  assert.ok(html.includes('Supervisor loop'), 'card title');
  for (const j of JOBS_BODY.jobs) assert.ok(html.includes('data-job="' + j.name + '"'), 'job row ' + j.name);
  for (const v of ['session', 'pi-prompt', 'qwen-test', 'claude-code', 'haiku-test']) assert.ok(html.includes(v), 'job column value ' + v);
  assert.ok(html.includes('escalated'), 'last outcome shown');
  assert.ok(html.includes('no runs yet'), 'a job with no outcome says so');
  assert.equal((html.match(/class="supervisor-loop-run"/g) || []).length, 3, 'one row per run');
  assert.ok(html.includes('2026-11-05T09:30:00Z'), 'run start time');
  // escalation count per job: draft_spec escalated twice across the runs.
  const row = html.match(/<tr class="supervisor-loop-job" data-job="draft_spec">.*?<\/tr>/)[0];
  assert.ok(/<td[^>]*>2<\/td><\/tr>$/.test(row), 'draft_spec escalation count is 2: ' + row);
});

test('the card links to the how-to and the definitions entry, with no PRD wording or tracker IDs', () => {
  const s = loadApp();
  s._j = JOBS_BODY; s._r = RUNS_BODY;
  const html = vm.runInContext('supervisorLoopCardHtml(_j, _r)', s);
  assert.ok(html.includes('howto%2Fsupervisor-loop.md'), 'how-to link');
  assert.ok(html.includes('datawatch-definitions.md#supervisor-loop'), 'definitions link');
  assert.ok(!/PRD|\bBL\d+|\bB\d+\b/.test(html), 'no PRD wording or tracker IDs');
});

test('the card handles an empty body, a disabled loop and escapes text', () => {
  const s = loadApp();
  s._j = { enabled: false, jobs: [] }; s._r = { runs: [] };
  const html = vm.runInContext('supervisorLoopCardHtml(_j, _r)', s);
  assert.ok(html.includes('switched off'));
  assert.ok(html.includes('No jobs reported.') && html.includes('No runs yet.'));
  assert.equal(vm.runInContext('supervisorLoopCardHtml(null, null)', s), null);
  s._j = { enabled: true, jobs: [{ name: 'x', model: '<img src=x>' }] }; s._r = null;
  const esc = vm.runInContext('supervisorLoopCardHtml(_j, _r)', s);
  assert.ok(!esc.includes('<img src=x>'));
});

test('the Settings group lists every supervisor_loop key, each with a help link and a locale label', () => {
  const s = loadApp();
  const fields = JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(x => x.id === 'autonomous') || {fields: []}).fields.filter(f => (f.key || '').indexOf('autonomous.supervisor_loop.') === 0))", s));
  assert.deepEqual(fields.map(f => f.key.replace('autonomous.supervisor_loop.', '')), EXPECTED_KEYS);
  const en = readLocale('en');
  for (const f of fields) {
    assert.ok(f.labelKey && f.labelKey.startsWith('supervisor_loop_'), 'labelKey for ' + f.key);
    assert.ok(en[f.labelKey], 'en label for ' + f.key);
    assert.ok(f.docsKey && f.docsKey.startsWith('supervisor_loop.'), 'docsKey for ' + f.key);
    s._k = f.docsKey;
    const link = vm.runInContext('docsLinkForKey(_k)', s);
    const anchor = 'supervisor-loop-' + f.key.replace('autonomous.supervisor_loop.', '').replace(/[._]+/g, '-');
    assert.equal(link, 'datawatch-definitions.md#' + anchor, 'help link for ' + f.key);
  }
  const group = JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(x => x.id === 'autonomous')).fields.find(f => f.type === 'group' && f.docsKey === 'supervisor_loop'))", s));
  assert.ok(group, 'group heading');
  assert.equal(vm.runInContext("docsLinkForKey('supervisor_loop')", s), 'datawatch-definitions.md#supervisor-loop');
});

test('every supervisor_loop_ key exists in all five locale files', () => {
  const en = readLocale('en');
  const enKeys = Object.keys(en).filter(k => k.startsWith('supervisor_loop_'));
  assert.ok(enKeys.length >= 40, 'en has the supervisor_loop_ keys');
  // keys the app references must exist too
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  for (const m of src.matchAll(/'(supervisor_loop_[a-z_]+)'/g)) {
    if (m[1].endsWith('_')) continue; // a prefix, completed at run time
    assert.ok(enKeys.includes(m[1]), 'app.js uses ' + m[1] + ' but en lacks it');
  }
  for (const l of LOCALES) {
    const b = readLocale(l);
    for (const k of enKeys) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, l + ' missing ' + k);
    }
    for (const k of Object.keys(b).filter(k => k.startsWith('supervisor_loop_'))) {
      assert.ok(enKeys.includes(k), l + ' has extra ' + k);
      assert.ok(!/\bPRDs?\b|\bBL\d+/.test(b[k]), l + ' ' + k + ' must not say PRD or carry a tracker ID');
    }
  }
});

test('the archive grace field renders as a number input with minimum 0', () => {
  const s = loadApp();
  const f = JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(x => x.id === 'autonomous')).fields.find(f => f.key === 'autonomous.supervisor_loop.archive_cancelled_after_minutes'))", s));
  assert.ok(f, 'the archive grace field exists');
  assert.equal(f.type, 'number', 'the field is a number input');
  assert.equal(f.min, 0, 'the field renders with minimum 0');
  const en = readLocale('en');
  assert.ok(en[f.labelKey], 'en label for the field');
  assert.ok(en[f.hintKey], 'en hint for the field');
  for (const l of LOCALES) {
    const b = readLocale(l);
    assert.ok(b[f.labelKey] && b[f.hintKey], l + ' has both the label and the hint');
  }
});

test('every help link resolves: the how-to file and each definitions anchor exist', () => {
  const s = loadApp();
  const docs = path.join(__dirname, '..', '..', '..', 'docs');
  const defs = fs.readFileSync(path.join(docs, 'datawatch-definitions.md'), 'utf8');
  const howto = vm.runInContext('SUPERVISOR_LOOP_HOWTO', s);
  assert.ok(fs.existsSync(path.join(docs, howto)), 'how-to file exists: ' + howto);
  const keys = ['supervisor_loop'].concat(EXPECTED_KEYS.map(k => 'supervisor_loop.' + k));
  for (const k of keys) {
    s._k = k;
    const link = vm.runInContext('docsLinkForKey(_k)', s);
    const anchor = link.split('#')[1];
    assert.ok(anchor, 'link has an anchor: ' + link);
    assert.ok(defs.includes('<a id="' + anchor + '"></a>'), 'definitions anchor exists: ' + anchor);
  }
});
