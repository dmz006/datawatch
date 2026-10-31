// Infrastructure retry view — PWA side.
//
// window.infraRetryCardHtml(status) turns the decoded GET
// /api/autonomous/infra-retries body into the Automation Overview card. The
// Autonomous settings group carries the seven autonomous.infra_retry.* fields,
// docsLinkForKey maps them to the glossary section, and the Automaton task row
// shows a Retry n of max badge and the hold cause. Every infra_retry_ and
// settings_infra_retry_ locale key is pinned in all five bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const HOSTILE = '<img src=x onerror=alert(1)>';
const GLOSSARY_LINK = 'datawatch-definitions.md#infrastructure-retry';
const VIEW_PREFIX = 'infra_retry_';
const SETTINGS_PREFIX = 'settings_infra_retry_';

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

function render(status) {
  const sandbox = loadApp();
  sandbox._status = status;
  return vm.runInContext('window.infraRetryCardHtml(_status)', sandbox);
}

function badges(task, max) {
  const sandbox = loadApp();
  sandbox._task = task;
  sandbox._max = max;
  return vm.runInContext('window.infraRetryTaskBadgesHtml(_task, _max)', sandbox);
}

function entry(over) {
  return {
    number: 1, class: 'spawn_error', reason: 'tmux server unreachable', at: '2026-10-30T10:00:00Z',
    delay_seconds: 60, next_at: '2026-10-30T10:01:00Z', ...over,
  };
}

function taskRow(over) {
  return {
    automaton_id: 'a1', automaton_title: 'Cache column', task_id: 't1', task_title: 'Add the index',
    status: 'pending', retry_count: 1, max_tries: 3, retries: [entry()], held: false, ...over,
  };
}

function status(over) {
  return {
    enabled: true, max_tries: 3, backoff_seconds: [60, 300, 540],
    categories: ['spawn_error', 'storage_write', 'session_killed_before_delivery', 'daemon_restart'],
    exhausted_action: 'hold', retry_dependents: true, raise_question: true, tasks: [], count: 0, ...over,
  };
}

function autonomousFields() {
  const sandbox = loadApp();
  return JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.filter(f => (f.key || '').startsWith('autonomous.infra_retry.')))",
    sandbox));
}

function docsLink(key) {
  const sandbox = loadApp();
  sandbox._key = key;
  return vm.runInContext('docsLinkForKey(_key)', sandbox);
}

// ── card rendering ──────────────────────────────────────────────────────────

test('a retried task shows Retry n of max, its class, reason and time', () => {
  const html = render(status({ tasks: [taskRow({ retries: [entry({ number: 1 }), entry({ number: 2, class: 'storage_write', reason: 'disk full' })], retry_count: 2 })], count: 1 }));
  assert.ok(html.includes('Infrastructure retries'), 'card title missing: ' + html);
  assert.ok(html.includes('Cache column · Add the index'), 'task name missing: ' + html);
  assert.ok(html.includes('Retry 2 of 3'), 'badge missing: ' + html);
  assert.ok(html.includes('Retry 1 of 3'), 'first retry line missing');
  assert.ok(html.includes('Class: spawn_error'), 'class missing');
  assert.ok(html.includes('Class: storage_write'), 'second class missing');
  assert.ok(html.includes('Reason: tmux server unreachable'), 'reason missing');
  assert.ok(html.includes('Reason: disk full'), 'second reason missing');
  assert.ok(/At [^<]+/.test(html), 'time missing');
  assert.ok(html.includes('Next try'), 'next try time missing');
  assert.ok(!html.includes('infra-retry-held'), 'a task that is not held must not show the held badge');
});

test('a held task shows the held label, the cause, and a button for the open question', () => {
  const html = render(status({ tasks: [taskRow({ held: true, status: 'review_needed', held_cause: 'the tmux server stayed down', question_id: 'q-42' })], count: 1 }));
  assert.ok(html.includes('Held for the operator'), 'held label missing: ' + html);
  assert.ok(html.includes('Cause: the tmux server stayed down'), 'cause missing');
  assert.ok(html.includes('Open the question'), 'question button text missing');
  assert.ok(html.includes("window.openQuestion('q-42')"), 'question button does not open the question: ' + html);
});

test('a held task without an open question shows the cause and no question button', () => {
  const html = render(status({ tasks: [taskRow({ held: true, held_cause: 'storage unavailable' })], count: 1 }));
  assert.ok(html.includes('Cause: storage unavailable'));
  assert.ok(!html.includes('Open the question'), 'no question, no button');
});

test('an empty list shows the empty state and the policy line', () => {
  const html = render(status());
  assert.ok(html.includes('No task has been retried or held because of an infrastructure failure.'), 'empty text missing: ' + html);
  assert.ok(html.includes('a task gets 3 tries, then it is held for the operator'), 'policy line missing');
});

test('a missing status shows the placeholder with the card title', () => {
  for (const html of [render(null), render(undefined)]) {
    assert.ok(html.includes('Infrastructure retries'));
    assert.ok(html.includes('No task has been retried or held'));
  }
});

test('the policy line follows enabled and the exhausted action', () => {
  assert.ok(render(status({ enabled: false })).includes('Automatic retry is off'));
  assert.ok(render(status({ exhausted_action: 'count' })).includes('the failure counts against the task'));
});

test('the card carries the help link to the glossary section', () => {
  const html = render(status());
  assert.ok(html.includes('class="docs-link"'), 'help link missing: ' + html);
  assert.ok(html.includes('/diagrams.html#docs/' + GLOSSARY_LINK), 'help link does not open the glossary section: ' + html);
});

test('every value from the API is escaped', () => {
  const html = render(status({
    tasks: [taskRow({
      automaton_title: HOSTILE, task_title: HOSTILE, held: true, held_cause: HOSTILE, question_id: HOSTILE,
      retries: [entry({ class: HOSTILE, reason: HOSTILE })],
    })], count: 1,
  }));
  assert.ok(!html.includes(HOSTILE), 'hostile markup rendered unescaped: ' + html);
  assert.ok(!html.includes('<img'), 'an image element was injected');
});

test('rows are capped and the card renders a missing tasks list as empty', () => {
  const many = Array.from({ length: 30 }, (_, i) => taskRow({ task_id: 't' + i, task_title: 'task ' + i }));
  const html = render(status({ tasks: many, count: 30 }));
  assert.equal(html.split('class="infra-retry-task"').length - 1, 20);
  assert.ok(render(status({ tasks: undefined })).includes('No task has been retried or held'));
});

// ── task row badges ─────────────────────────────────────────────────────────

test('a task row shows Retry n of max when the task carries infra_retries', () => {
  const html = badges({ status: 'pending', infra_retry_count: 2, infra_retries: [entry({ number: 1 }), entry({ number: 2 })] }, 3);
  assert.ok(html.includes('Retry 2 of 3'), html);
  assert.ok(!html.includes('infra-retry-held'));
});

test('a task row without the limit shows Retry n, and the history number when the incident is over', () => {
  assert.ok(badges({ infra_retry_count: 1, infra_retries: [entry()] }, 0).includes('>Retry 1<'));
  const over = badges({ infra_retry_count: 0, infra_retries: [entry({ number: 2 })] }, 3);
  assert.ok(over.includes('Retry 2 of 3'), over);
});

test('a task with no infrastructure record gets no badge', () => {
  assert.equal(badges({ status: 'pending' }, 3), '');
  assert.equal(badges(null, 3), '');
});

test('a task held for an infrastructure reason shows the held label with the cause as its tooltip', () => {
  const html = badges({ status: 'review_needed', infra_retry_count: 3, infra_retries: [entry({ number: 3 })], failure_reason: { source: 'infra-retry-hold', detail: 'the daemon restarted twice' } }, 3);
  assert.ok(html.includes('Held for the operator'), html);
  assert.ok(html.includes('title="Cause: the daemon restarted twice"'), html);
});

test('a task held for another reason does not show the infrastructure hold', () => {
  const html = badges({ status: 'review_needed', failure_reason: { source: 'verifier', detail: 'tests failed' } }, 3);
  assert.equal(html, '');
});

test('the expanded body names the cause of an infrastructure hold and escapes it', () => {
  const sandbox = loadApp();
  sandbox._held = { status: 'review_needed', failure_reason: { source: 'infra-retry-hold', detail: HOSTILE } };
  sandbox._other = { status: 'review_needed', failure_reason: { source: 'verifier', detail: 'x' } };
  const held = vm.runInContext('window.infraRetryHeldCauseHtml(_held)', sandbox);
  assert.ok(held.includes('Cause: '), held);
  assert.ok(!held.includes(HOSTILE));
  assert.equal(vm.runInContext('window.infraRetryHeldCauseHtml(_other)', sandbox), '');
});

// ── the tries-per-task limit ────────────────────────────────────────────────

function withFetchRecorder(reply) {
  const sandbox = loadApp();
  sandbox._calls = [];
  sandbox._reply = reply;
  vm.runInContext("apiFetch = (url) => { _calls.push(url); return Promise.resolve(_reply); };", sandbox);
  return sandbox;
}

test('rendering a task row reads nothing from the network', () => {
  const sandbox = withFetchRecorder({ max_tries: 3 });
  sandbox._task = { status: 'pending', infra_retry_count: 1, infra_retries: [entry()] };
  vm.runInContext('window.infraRetryTaskBadgesHtml(_task, 0)', sandbox);
  assert.deepEqual(sandbox._calls, []);
});

test('the Automaton detail reads the limit once, and only when a task carries retries', async () => {
  const sandbox = withFetchRecorder({ max_tries: 4 });
  sandbox._plain = { stories: [{ tasks: [{ id: 't1' }] }] };
  sandbox._retried = { stories: [{ tasks: [{ id: 't1' }, { id: 't2', infra_retry_count: 1, infra_retries: [entry()] }] }] };
  assert.equal(vm.runInContext('_infraRetryLoadMax(_plain)', sandbox), null, 'no retries, no read');
  assert.deepEqual(sandbox._calls, []);
  await vm.runInContext('_infraRetryLoadMax(_retried)', sandbox);
  assert.deepEqual(sandbox._calls, ['/api/autonomous/infra-retries?limit=1']);
  assert.equal(vm.runInContext('window._infraRetryMax', sandbox), 4);
  assert.equal(vm.runInContext('_infraRetryLoadMax(_retried)', sandbox), null, 'limit known, no second read');
  sandbox._task = { infra_retry_count: 2, infra_retries: [entry({ number: 2 })] };
  assert.ok(vm.runInContext('window.infraRetryTaskBadgesHtml(_task, _infraRetryMaxTries())', sandbox).includes('Retry 2 of 4'));
});

test('a failed limit read still lets the detail render, with the badge lacking the limit', async () => {
  const sandbox = loadApp();
  vm.runInContext("apiFetch = () => Promise.reject(new Error('down'));", sandbox);
  sandbox._retried = { stories: [{ tasks: [{ infra_retry_count: 1, infra_retries: [entry()] }] }] };
  await vm.runInContext('_infraRetryLoadMax(_retried)', sandbox);
  assert.equal(vm.runInContext('_infraRetryMaxTries()', sandbox), 0);
});

// ── settings fields ─────────────────────────────────────────────────────────

test('the Autonomous settings group has the seven infra_retry fields with the right types', () => {
  const fields = autonomousFields();
  const byKey = Object.fromEntries(fields.map(f => [f.key, f]));
  const want = {
    'autonomous.infra_retry.enabled': 'toggle',
    'autonomous.infra_retry.max_tries': 'number',
    'autonomous.infra_retry.backoff_seconds': 'text',
    'autonomous.infra_retry.categories': 'text',
    'autonomous.infra_retry.exhausted_action': 'select',
    'autonomous.infra_retry.retry_dependents': 'toggle',
    'autonomous.infra_retry.raise_question': 'toggle',
  };
  assert.equal(fields.length, 7, 'expected seven fields: ' + fields.map(f => f.key).join(','));
  for (const [key, type] of Object.entries(want)) {
    assert.ok(byKey[key], key + ' missing');
    assert.equal(byKey[key].type, type, key + ' has the wrong type');
  }
  assert.deepEqual(byKey['autonomous.infra_retry.exhausted_action'].options, ['hold', 'count']);
  assert.equal(byKey['autonomous.infra_retry.max_tries'].min, 1);
  assert.equal(byKey['autonomous.infra_retry.backoff_seconds'].csv, true);
  assert.equal(byKey['autonomous.infra_retry.categories'].csv, true);
});

test('every infra_retry field has a label key, an inline description key and a docs link', () => {
  const en = bundle('en');
  for (const f of autonomousFields()) {
    const name = f.key.slice('autonomous.infra_retry.'.length);
    assert.equal(f.labelKey, SETTINGS_PREFIX + name, f.key + ' labelKey');
    assert.equal(f.hintKey, SETTINGS_PREFIX + name + '_hint', f.key + ' hintKey');
    assert.ok(en[f.labelKey] && en[f.hintKey], f.key + ' label or description missing in en');
    assert.ok(f.docsKey, f.key + ' has no docs link');
    assert.equal(docsLink(f.docsKey), GLOSSARY_LINK, f.key + ' docs link');
  }
});

test('the settings renderer shows the description and docs link under a toggle', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const start = src.indexOf("} else if (f.type === 'toggle') {\n            const checked = !!val;");
  assert.ok(start > 0, 'general settings toggle branch not found');
  const branch = src.slice(start, start + 900);
  assert.ok(branch.includes('_fieldHintHTML(f)'), 'toggle branch does not render the description');
  assert.ok(branch.includes('defsLink(f.docsKey)'), 'toggle branch does not render the docs link');
});

// ── help link ───────────────────────────────────────────────────────────────

test('docsLinkForKey maps the infra_retry keys to the glossary section', () => {
  for (const key of ['autonomous.infra_retry.enabled', 'autonomous.infra_retry.max_tries', 'settings_infra_retry_max_tries', 'infra_retry_card_title']) {
    assert.equal(docsLink(key), GLOSSARY_LINK, key);
  }
  assert.equal(docsLink('autonomous.lane_fill.mode'), null, 'unrelated keys keep their own mapping');
});

// ── locale keys ─────────────────────────────────────────────────────────────

function ownKeys(b) {
  return Object.keys(b).filter(k => k.startsWith(VIEW_PREFIX) || k.startsWith(SETTINGS_PREFIX)).sort();
}

test('the infra_retry and settings_infra_retry keys are identical in all five bundles', () => {
  const en = ownKeys(bundle('en'));
  assert.ok(en.length >= 25, 'too few keys in en: ' + en.length);
  for (const lang of LANGS) {
    const b = bundle(lang);
    assert.deepEqual(ownKeys(b), en, lang + ' key set differs from en');
    for (const k of en) assert.ok(typeof b[k] === 'string' && b[k].length > 0, k + ' empty in ' + lang);
  }
});

test('the non-English bundles carry the English text until translations arrive', () => {
  const en = bundle('en');
  for (const lang of LANGS.filter(l => l !== 'en')) {
    const b = bundle(lang);
    for (const k of ownKeys(en)) assert.equal(b[k], en[k], k + ' differs from en in ' + lang);
  }
});

test('every infra_retry key the app references exists in all five bundles', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const referenced = new Set();
  for (const m of src.matchAll(/['"`]((?:settings_)?infra_retry_[a-z_]+)['"`]/g)) referenced.add(m[1]);
  assert.ok(referenced.size >= 20, 'too few references found: ' + referenced.size);
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of referenced) assert.ok(typeof b[k] === 'string', k + ' referenced by app.js but missing in ' + lang);
  }
});

test('placeholders agree: each %n$s in a value is one the view supplies', () => {
  const en = bundle('en');
  const counts = {
    infra_retry_count_of: 2, infra_retry_count: 1, infra_retry_class: 1, infra_retry_reason: 1, infra_retry_time: 1,
    infra_retry_next: 1, infra_retry_held_cause: 1, infra_retry_failed: 1, infra_retry_policy_on_hold: 1, infra_retry_policy_on_count: 1,
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

test('the card, the task row and the settings fields carry no PRD wording or tracker id in their markup', () => {
  const html = render(status({ tasks: [taskRow({ held: true, held_cause: 'x', question_id: 'q1' })], count: 1 })) +
    badges({ status: 'review_needed', infra_retry_count: 1, infra_retries: [entry()], failure_reason: { source: 'infra-retry-hold', detail: 'x' } }, 3);
  // Class names are not user-visible text; the shared card styles keep their prd- names.
  const visible = html.replace(/class="[^"]*"/g, '');
  assert.ok(!/\bPRD/i.test(visible), 'PRD wording in markup');
  assert.ok(!/\b(?:BL|GH#?)\d+\b/.test(visible), 'tracker id in markup');
  for (const f of autonomousFields()) {
    for (const v of [f.label, f.placeholder || '']) {
      assert.ok(!/\bPRD/i.test(v) && !/\b(?:BL|GH#?)\d+\b/.test(v), f.key + ' fallback text: ' + v);
    }
  }
});
