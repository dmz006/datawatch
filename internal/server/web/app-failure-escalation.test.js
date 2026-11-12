// Failure escalation in the PWA: the Settings fields, the held-task review
// surface on an Automaton task, the override editors, the Automation strip
// badge and the locale keys these surfaces read. The server side (REST, MCP,
// CLI, comm) has its own Go tests. This file loads the real app.js into a vm
// sandbox and drives its functions with a recording fetch.

const nodeTest = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

// Every test title in this file starts with TestFailureEscalation, so the
// feature's tests are found together in the node output.
const test = (title, fn) => nodeTest('TestFailureEscalation: ' + title, fn);

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const BUNDLES = ['en', 'de', 'es', 'fr', 'ja'];
const bundle = lang => JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
const EN = bundle('en');

// The escalation keys: every review_* key, the settings keys and the held status key.
const ESCALATION_KEY = /^(review_|settings_failure_escalation|task_status_review_needed$)/;
const ESCALATION_KEYS = Object.keys(EN).filter(k => ESCALATION_KEY.test(k));

const HELD_TASK = {
  id: 'T2',
  title: 'Wire the audit log',
  status: 'review_needed',
  spec: 'Add audit entries to every write',
  backend: 'claude-code',
  effort: 'high',
  model: '',
  files: ['internal/audit/log.go', 'internal/audit/log_test.go'],
  attempts: [
    {
      number: 1, backend: 'ollama', model: 'qwen3.8:27b', duration_seconds: 75, outcome: 'failed',
      failure_category: 'verification', failure_summary: 'tests red',
      verifier_summary: 'missing coverage', verifier_issues: ['no test for empty input'],
      changed_files: ['internal/audit/log.go'],
    },
    {
      number: 2, backend: 'claude-code', duration_seconds: 30, outcome: 'stalled', escalated: true,
      changed_files: [],
    },
  ],
  review_summary: { failure_count: 2, total_seconds: 105, text: 'Attempt 1: failed\nAttempt 2: stalled' },
  escalation_failures: 2,
  failure_escalation: {
    enabled: false, max_failures: 4,
    steps: [{ backend: 'ollama', model: 'qwen3.8:27b', attempts: 2 }, { attempts: 1 }],
  },
};

const PRD = {
  id: 'PRD-1',
  title: 'Audit trail',
  status: 'running',
  stories: [{ id: 'S1', title: 'Logging', status: 'running', tasks: [HELD_TASK] }],
};

// boot loads app.js into a fresh sandbox with the English bundle in place, so
// t() returns English text. Toasts and the post-action refresh are recorded or
// stubbed, and confirm() accepts.
function boot(byId = {}) {
  const sandbox = buildSandbox({ byId });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  vm.runInContext('Object.assign(window._i18n.bundle, ' + JSON.stringify(EN) + ')', sandbox);
  sandbox.confirm = () => true;
  sandbox.__toasts = [];
  sandbox.showToast = (message, type) => { sandbox.__toasts.push({ message, type }); };
  sandbox._refreshAutomataOrPRD = () => {};
  return sandbox;
}

// recordFetch replaces the sandbox fetch. Each call is recorded with its URL,
// method and parsed JSON body; reply(url) gives the JSON answer.
function recordFetch(sandbox, reply = () => ({})) {
  const calls = [];
  sandbox.fetch = (url, opts = {}) => {
    calls.push({ url: String(url), method: opts.method || 'GET', body: opts.body ? JSON.parse(opts.body) : null });
    const data = reply(String(url));
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(data), text: () => Promise.resolve(JSON.stringify(data)) });
  };
  return calls;
}

// onclickCodes returns the decoded onclick handlers in a rendered fragment, the
// way the browser reads them out of the attribute.
function onclickCodes(html) {
  const decode = s => s.replace(/&quot;/g, '"').replace(/&#39;/g, "'").replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&');
  return [...html.matchAll(/onclick="([^"]*)"/g)].map(m => decode(m[1]));
}

// runHandler runs one onclick handler against the sandbox, as the button would.
function runHandler(sandbox, code) {
  sandbox.event = { stopPropagation() {} };
  return vm.runInContext(code, sandbox);
}

// plain copies a value made inside the sandbox into this realm, so strict deep
// equality compares contents and not prototypes.
const plain = v => JSON.parse(JSON.stringify(v));

test('settings: the autonomous fields expose enabled, max_failures and steps with their control types', () => {
  const sandbox = boot();
  const sections = vm.runInContext('GENERAL_CONFIG_FIELDS', sandbox);
  const fields = sections.find(s => s.id === 'autonomous').fields;
  const byKey = k => fields.find(f => f.key === k);

  const enabled = byKey('autonomous.failure_escalation.enabled');
  assert.ok(enabled, 'enabled field present');
  assert.equal(enabled.type, 'toggle');
  assert.equal(enabled.labelKey, 'settings_failure_escalation_enabled');

  const max = byKey('autonomous.failure_escalation.max_failures');
  assert.ok(max, 'max_failures field present');
  assert.equal(max.type, 'number');
  assert.equal(max.min, 1);
  assert.equal(max.placeholder, '3');
  assert.equal(max.labelKey, 'settings_failure_escalation_max_failures');

  const steps = byKey('autonomous.failure_escalation.steps');
  assert.ok(steps, 'steps field present');
  assert.equal(steps.type, 'failure_escalation_steps');
  assert.equal(steps.placeholder, 'ollama:qwen3.8:27b@2,claude-code:claude-sonnet-5-5');
  assert.equal(steps.hintKey, 'settings_failure_escalation_steps_hint');
  assert.equal(steps.labelKey, 'settings_failure_escalation_steps');
});

test('steps text: the compact form round-trips through the parser and bad items are refused', () => {
  const sandbox = boot();
  const steps = [
    { backend: 'ollama', model: 'qwen3.8:27b', attempts: 2 },
    { attempts: 1 },
    { backend: 'claude-code', model: 'claude-sonnet-5-5' },
  ];
  const text = vm.runInContext('_failureEscalationStepsText', sandbox)(steps);
  assert.equal(text, 'ollama:qwen3.8:27b@2,-@1,claude-code:claude-sonnet-5-5');
  assert.deepEqual(plain(vm.runInContext('_parseFailureEscalationSteps', sandbox)(text)), steps);
  assert.equal(vm.runInContext('_failureEscalationStepsText', sandbox)(undefined), '');
  assert.deepEqual(plain(vm.runInContext('_parseFailureEscalationSteps', sandbox)('   ')), []);

  const parse = vm.runInContext('_parseFailureEscalationSteps', sandbox);
  assert.throws(() => parse('ollama'), /backend:model/);
  assert.throws(() => parse('ollama:x@0'), /@attempts/);
  assert.throws(() => parse(':model'), /no backend/);
  assert.throws(() => parse('a:b,,c:d'), /empty/);
});

test('task row: a held task shows the badge, the attempt summary and the five review actions', () => {
  const sandbox = boot();
  const html = sandbox.renderTask(PRD, PRD.stories[0], HELD_TASK, true);

  assert.match(html, /prd-task-review-badge/, 'distinct held badge on the row');
  assert.match(html, /Held for review/, 'held status label in English');
  assert.match(html, /prd-task-review-actions/, 'review action group rendered');
  assert.match(html, /Attempts \(2\)/, 'attempt summary heading');
  assert.match(html, /Time spent: 1m 45s/, 'total time from the review summary');
  assert.match(html, /qwen3\.8:27b/, 'attempt worker shown');
  assert.match(html, /tests red/, 'attempt failure summary shown');
  assert.match(html, /internal\/audit\/log\.go/, 'changed files shown');
  assert.match(html, /missing coverage/, 'verifier report shown');
  assert.match(html, /no test for empty input/, 'verifier issue shown');
  assert.match(html, /Attempt 1: failed/, 'review report text shown');

  const actions = onclickCodes(html).filter(c => /prdReview|openPRDTaskReviewModal/.test(c));
  assert.equal(actions.length, 5, 'five actions: retry, spec, worker, scope, cancel');
  for (const label of ['Retry', 'Edit spec and retry', 'Change worker', 'Narrow scope', 'Cancel task']) {
    assert.match(html, new RegExp('>[^<]*' + label + '<'), 'button labelled ' + label);
  }
});

test('task row: a task that is not held shows no review actions', () => {
  const sandbox = boot();
  const running = Object.assign({}, HELD_TASK, { status: 'running', attempts: [] , review_summary: null});
  const html = sandbox.renderTask(PRD, PRD.stories[0], running, true);
  assert.ok(!/prd-task-review-actions/.test(html), 'no review action group');
  assert.ok(!/prd-task-review-badge/.test(html), 'no held badge');
});

test('actions: Retry posts action retry to resolve_task_review for the task', async () => {
  const sandbox = boot();
  const calls = recordFetch(sandbox, () => ({ id: 'PRD-1' }));
  const html = sandbox.renderTask(PRD, PRD.stories[0], HELD_TASK, true);
  const retry = onclickCodes(html).find(c => c.includes('prdReviewRetry'));
  await runHandler(sandbox, retry);

  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, '/api/autonomous/prds/PRD-1/resolve_task_review');
  assert.equal(calls[0].method, 'POST');
  assert.deepEqual(calls[0].body, { task_id: 'T2', actor: 'operator', action: 'retry' });
  assert.equal(sandbox.__toasts.at(-1).type, 'success');
});

test('actions: Cancel posts action cancel after confirmation, and posts nothing when declined', async () => {
  const sandbox = boot();
  const calls = recordFetch(sandbox, () => ({ id: 'PRD-1' }));
  const html = sandbox.renderTask(PRD, PRD.stories[0], HELD_TASK, true);
  const cancel = onclickCodes(html).find(c => c.includes('prdReviewCancel'));

  sandbox.confirm = () => false;
  await runHandler(sandbox, cancel);
  assert.equal(calls.length, 0, 'declined cancel posts nothing');

  sandbox.confirm = () => true;
  await runHandler(sandbox, cancel);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, '/api/autonomous/prds/PRD-1/resolve_task_review');
  assert.deepEqual(calls[0].body, { task_id: 'T2', actor: 'operator', action: 'cancel' });
});

test('retry bodies: each form sends only the fields it changes', () => {
  const sandbox = boot();
  const body = vm.runInContext('_prdReviewBody', sandbox);
  assert.deepEqual(plain(body('spec', { spec: 'Tighter spec', note: '' })), { action: 'retry', spec: 'Tighter spec' });
  assert.deepEqual(
    plain(body('worker', { backend: 'ollama', model: 'qwen3.8:27b', effort: 'low', note: 'cheaper worker' })),
    { action: 'retry', backend: 'ollama', model: 'qwen3.8:27b', effort: 'low', note: 'cheaper worker' });
  assert.deepEqual(plain(body('worker', { backend: '', model: '', effort: '' })), { action: 'retry' });
  assert.deepEqual(plain(body('scope', { files: 'a.go\n\n b.go ,c.go', note: '' })), { action: 'retry', files: ['a.go', 'b.go', 'c.go'] });
});

test('spec and scope forms: submitting posts the changed spec or narrowed files', async () => {
  const modal = {};
  const sandbox = boot({
    prdReviewSpec: { value: 'Tighter spec' },
    prdReviewFiles: { value: 'internal/audit/log.go' },
    prdReviewNote: { value: 'narrowed on purpose' },
  });
  sandbox._prdMountModal = (html, onSubmit) => { modal.html = html; modal.onSubmit = onSubmit; };
  vm.runInContext('_automataDetailPRD = __prd', Object.assign(sandbox, { __prd: PRD }));
  const calls = recordFetch(sandbox, () => ({ id: 'PRD-1' }));

  sandbox.openPRDTaskReviewModal('PRD-1', 'T2', 'spec');
  assert.match(modal.html, /prdReviewSpec/, 'spec form mounted');
  await modal.onSubmit();
  assert.deepEqual(calls[0].body, { task_id: 'T2', actor: 'operator', action: 'retry', spec: 'Tighter spec', note: 'narrowed on purpose' });

  sandbox.openPRDTaskReviewModal('PRD-1', 'T2', 'scope');
  assert.match(modal.html, /prdReviewFiles/, 'scope form mounted');
  await modal.onSubmit();
  assert.deepEqual(calls[1].body, { task_id: 'T2', actor: 'operator', action: 'retry', files: ['internal/audit/log.go'], note: 'narrowed on purpose' });
});

test('spec and scope forms: an empty spec or an empty file list is refused without a request', async () => {
  const modal = {};
  const sandbox = boot({ prdReviewSpec: { value: '  ' }, prdReviewFiles: { value: '\n' }, prdReviewNote: { value: '' } });
  sandbox._prdMountModal = (html, onSubmit) => { modal.onSubmit = onSubmit; };
  vm.runInContext('_automataDetailPRD = __prd', Object.assign(sandbox, { __prd: PRD }));
  const calls = recordFetch(sandbox);

  sandbox.openPRDTaskReviewModal('PRD-1', 'T2', 'spec');
  await modal.onSubmit();
  sandbox.openPRDTaskReviewModal('PRD-1', 'T2', 'scope');
  await modal.onSubmit();
  assert.equal(calls.length, 0, 'nothing posted');
  assert.equal(sandbox.__toasts.filter(t => t.type === 'error').length, 2);
});

test('override editor: saving writes the policy with the server field names, and clearing sends null', async () => {
  const fields = {
    'feEnabled-task-T2': { value: 'off' },
    'feMax-task-T2': { value: '4' },
    'feSteps-task-T2': { value: 'ollama:qwen3.8:27b@2,-@1' },
  };
  const sandbox = boot(fields);
  const calls = recordFetch(sandbox, () => ({ id: 'PRD-1' }));

  await runHandler(sandbox, 'prdSaveFailureEscalation("PRD-1","T2","task-T2",false)');
  assert.equal(calls[0].url, '/api/autonomous/prds/PRD-1/set_failure_escalation');
  assert.equal(calls[0].method, 'POST');
  assert.deepEqual(calls[0].body, {
    task_id: 'T2',
    actor: 'operator',
    policy: {
      enabled: false,
      max_failures: 4,
      steps: [{ backend: 'ollama', model: 'qwen3.8:27b', attempts: 2 }, { attempts: 1 }],
    },
  });

  await runHandler(sandbox, 'prdSaveFailureEscalation("PRD-1","","prd",true)');
  assert.equal(calls[1].url, '/api/autonomous/prds/PRD-1/set_failure_escalation');
  assert.deepEqual(calls[1].body, { task_id: '', actor: 'operator', policy: null });
});

test('override editor: an editor with every field inherited saves as a clear', async () => {
  const sandbox = boot({
    'feEnabled-prd': { value: '' },
    'feMax-prd': { value: '' },
    'feSteps-prd': { value: '' },
  });
  const calls = recordFetch(sandbox, () => ({ id: 'PRD-1' }));
  await runHandler(sandbox, 'prdSaveFailureEscalation("PRD-1","","prd",false)');
  assert.deepEqual(calls[0].body, { task_id: '', actor: 'operator', policy: null });
});

test('override editor: invalid steps are refused client-side and nothing is posted', async () => {
  const sandbox = boot({
    'feEnabled-prd': { value: 'on' },
    'feMax-prd': { value: '' },
    'feSteps-prd': { value: 'ollama' },
  });
  const calls = recordFetch(sandbox);
  await runHandler(sandbox, 'prdSaveFailureEscalation("PRD-1","","prd",false)');
  assert.equal(calls.length, 0);
  assert.equal(sandbox.__toasts.at(-1).type, 'error');
  assert.equal(sandbox.__toasts.at(-1).message, EN.review_override_invalid);
});

test('override editor: the Automaton overview and the task body render the editor with the stored override', () => {
  const sandbox = boot();
  const overview = sandbox._renderDetailOverview(PRD);
  assert.match(overview, /id="feEnabled-prd"/, 'Automaton-level editor');

  const html = sandbox.renderTask(PRD, PRD.stories[0], HELD_TASK, true);
  assert.match(html, /id="feEnabled-task-T2"/, 'task-level editor');
  assert.match(html, /value="4"/, 'stored max_failures shown');
  assert.match(html, /ollama:qwen3\.8:27b@2,-@1/, 'stored steps shown in compact form');
  assert.match(html, /<option value="off" selected>/, 'stored enabled shown');
});

test('held count: the Automaton header badge and the list progress line count review_needed tasks', () => {
  const sandbox = boot();
  assert.match(sandbox._prdHeldBadgeHTML(PRD), /prd-held-badge[\s\S]*1 held for review/);
  const summary = { id: 'PRD-2', task_count: 3, task_status_counts: { completed: 2, review_needed: 1 } };
  assert.equal(sandbox._prdHeldTaskCount(summary), 1);
  assert.match(sandbox.renderProgressBar(summary), /prd-held-badge/);
  assert.equal(sandbox._prdHeldBadgeHTML({ id: 'PRD-3', stories: [] }), '');
});

test('Automation strip: the member shows its held count and the strip header counts every member', () => {
  const sandbox = boot();
  const automation = {
    id: 'A1',
    name: 'Chain',
    entry_member_id: 'M1',
    members: [{ id: 'M1', kind: 'prd', prd_id: 'PRD-1', status: 'running' }],
  };
  const ctx = {
    prdTitles: { 'PRD-1': 'Audit trail' },
    prdStatuses: { 'PRD-1': 'running' },
    reviewByPrd: { 'PRD-1': 2 },
    expanded: new Set(),
    automationsById: {},
  };
  const html = sandbox.buildAutomationStripHTML(automation, ctx);
  assert.match(html, /automation-held-strip[^>]*>[^<]*2 held for review/, 'strip header count');
  assert.match(html, /automation-held-badge[^>]*>[^<]*2 held for review/, 'member badge count');

  const quiet = sandbox.buildAutomationStripHTML(automation, Object.assign({}, ctx, { reviewByPrd: {} }));
  assert.ok(!/automation-held/.test(quiet), 'no badge when nothing is held');
});

test('Automation status: loadAutomationPrdInfo reads review_needed_count for the current Automaton', async () => {
  const sandbox = boot();
  recordFetch(sandbox, url => {
    if (url === '/api/autonomous/prds') return [{ id: 'PRD-1', title: 'Audit trail', status: 'running' }];
    if (url === '/api/autonomous/status') return {
      automations: [
        { id: 'A1', current_prd_id: 'PRD-1', review_needed_count: 2 },
        { id: 'A2', current_prd_id: 'PRD-9', review_needed_count: 0 },
      ],
    };
    return {};
  });
  const info = await vm.runInContext('loadAutomationPrdInfo()', sandbox);
  assert.deepEqual(plain(info.review), { 'PRD-1': 2 });
  assert.equal(info.titles['PRD-1'], 'Audit trail');
});

test('locales: every escalation key is present and non-empty in all five bundles, with no PRD wording', () => {
  assert.ok(ESCALATION_KEYS.length >= 40, 'the escalation key set is complete: ' + ESCALATION_KEYS.length);
  for (const lang of BUNDLES) {
    const b = bundle(lang);
    for (const k of ESCALATION_KEYS) {
      assert.ok(typeof b[k] === 'string' && b[k].trim().length > 0, k + ' missing or empty in ' + lang);
      assert.ok(!/\bPRD\b/.test(b[k]), k + ' uses PRD wording in ' + lang);
    }
  }
});

test('locales: every escalation key app.js reads exists in en.json', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const used = new Set();
  for (const m of src.matchAll(/t\(\s*'((?:review_|settings_failure_escalation|task_status_review_needed)[A-Za-z0-9_]*)'/g)) used.add(m[1]);
  for (const m of src.matchAll(/(?:labelKey|hintKey):\s*'((?:settings_failure_escalation)[A-Za-z0-9_]*)'/g)) used.add(m[1]);
  assert.ok(used.size >= 30, 'app.js reads the escalation keys: ' + used.size);
  for (const k of used) {
    assert.ok(typeof EN[k] === 'string' && EN[k].length > 0, k + ' read by app.js but missing from en.json');
  }
});
