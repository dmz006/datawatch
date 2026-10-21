// 2026-10-17 — plan review gate: PRD-card verdict panel, the re-run
// button, and the blocked-approve error surfacing. Loads the real
// app.js into the stub browser (testutil_browser_stub) and exercises
// the real functions.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

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

function captureToasts(sandbox) {
  const toasts = [];
  sandbox.showToast = (msg, type, ms) => { toasts.push({ msg: String(msg), type, ms }); };
  return toasts;
}

test('renderPRDPlanReview renders a green pass panel with the re-run button', () => {
  const sandbox = loadAppJS();
  const prd = {
    id: 'p-pass',
    status: 'draft',
    plan_review: { ok: true, severity: 'info', summary: 'looks solid', backend: 'anthropic', model: 'claude-x', mode: 'warn', at: '2026-10-16T00:00:00Z' },
  };
  const html = call(sandbox, 'renderPRDPlanReview', prd);
  assert.match(html, /prd-plan-review/);
  assert.match(html, /#10b981/);
  // t() returns the key itself when the bundle is empty (test env), so
  // the locale key is what renders here.
  assert.match(html, /prd_plan_review_ok/);
  assert.match(html, /anthropic \/ claude-x/);
  assert.match(html, /warn/);
  assert.match(html, /looks solid/);
  assert.match(html, /prdrPlanReviewReRun\("p-pass", this\)/);
});

test('renderPRDPlanReview renders findings and escapes hostile detail text on a failure', () => {
  const sandbox = loadAppJS();
  const prd = {
    id: 'p-fail',
    status: 'draft',
    plan_review: { ok: false, summary: 'bad plan', mode: 'block', findings: [{ task_id: 't1', task_title: 'Task & Co', category: 'scope', detail: '</script><img src=x onerror=alert(1)>' }] },
  };
  const html = call(sandbox, 'renderPRDPlanReview', prd);
  assert.match(html, /#ef4444/);
  assert.match(html, /prd_plan_review_fail/);
  assert.match(html, /&lt;\/script&gt;&lt;img src=x onerror=alert\(1\)&gt;/);
  assert.doesNotMatch(html, /<img/);
  assert.match(html, /Task &amp; Co/);
  assert.match(html, /block/);
});

test('renderPRDPlanReview is empty when the PRD has no verdict', () => {
  const sandbox = loadAppJS();
  assert.equal(call(sandbox, 'renderPRDPlanReview', { id: 'p-none' }), '');
  assert.equal(call(sandbox, 'renderPRDPlanReview', {}), '');
});

test('prdrPlanReviewReRun POSTs the plan-review endpoint and toasts success', async () => {
  const sandbox = loadAppJS();
  const toasts = captureToasts(sandbox);
  const reqs = [];
  sandbox.apiFetch = (url, opts) => {
    reqs.push({ url, opts });
    if (url === '/api/autonomous/prds') return Promise.resolve([]);
    return Promise.resolve({ ok: true });
  };
  const btn = { disabled: false, style: {} };
  sandbox.__btn = btn;
  vm.runInContext('prdrPlanReviewReRun("p-re", __btn)', sandbox);
  delete sandbox.__btn;
  await flushAsync();
  const planReviewReqs = reqs.filter(r => r.url.endsWith('/plan-review'));
  assert.equal(planReviewReqs.length, 1, 'apiFetch called once for plan-review: ' + JSON.stringify(reqs));
  assert.ok(planReviewReqs[0].url.startsWith('/api/autonomous/prds/p-re/plan-review'), planReviewReqs[0].url);
  assert.equal(planReviewReqs[0].opts.method, 'POST');
  assert.match(planReviewReqs[0].opts.body, /"actor":"operator"/);
  // On success the panel refresh replaces the button in place, so the
  // stale button is intentionally left disabled (no double-click).
  assert.equal(btn.disabled, true, 'stale button stays disabled while the panel refreshes');
  await flushAsync();
  assert.ok(toasts.some(t => t.type === 'success' && /prd_plan_review_done/.test(t.msg)), JSON.stringify(toasts));
});

test('prdrPlanReviewReRun toasts the error and re-enables the button on failure', async () => {
  const sandbox = loadAppJS();
  const toasts = captureToasts(sandbox);
  sandbox.apiFetch = () => Promise.reject(new Error('reviewer offline'));
  const btn = { disabled: false, style: Object.create(null) };
  vm.runInContext('prdrPlanReviewReRun("p-re2", __btn)', Object.assign(sandbox, { __btn: btn }));
  delete sandbox.__btn;
  assert.equal(btn.disabled, true, 'button disabled while in flight');
  await flushAsync();
  assert.equal(btn.disabled, false, 'button re-enabled after failure');
  assert.equal(btn.style.opacity, '', 'opacity restored after failure');
  assert.ok(toasts.some(t => t.type === 'error' && /reviewer offline/.test(t.msg)), JSON.stringify(toasts));
  assert.ok(toasts.every(t => !(t.type === 'success')), 'no success toast on failure: ' + JSON.stringify(toasts));
});

test('prdAction approve rejection containing "plan review blocks" gets the dedicated long toast', async () => {
  const sandbox = loadAppJS();
  const toasts = captureToasts(sandbox);
  const reqs = [];
  sandbox.apiFetch = (url, opts) => {
    reqs.push({ url, opts });
    return Promise.reject(new Error('plan review blocks approval of prd "p9" (mode block): two gaps found'));
  };
  call(sandbox, 'prdAction', 'p9', 'approve', 'POST', null);
  await flushAsync();
  assert.equal(reqs.length, 1);
  assert.equal(reqs[0].url, '/api/autonomous/prds/p9/approve');
  assert.equal(reqs[0].opts.method, 'POST');
  const blocked = toasts.find(t => /plan review blocks/.test(t.msg));
  assert.ok(blocked, 'blocked toast present: ' + JSON.stringify(toasts));
  assert.ok(blocked.msg.startsWith('plan_review_approve_blocked_tip'), 'locale prefix: ' + blocked.msg);
  assert.equal(blocked.ms, 8000, 'dedicated toast uses the longer 8000ms duration');
});

test('prdAction generic failures keep the original short toast', async () => {
  const sandbox = loadAppJS();
  const toasts = captureToasts(sandbox);
  sandbox.apiFetch = () => Promise.reject(new Error('connection refused'));
  call(sandbox, 'prdAction', 'p10', 'approve', 'POST', null);
  await flushAsync();
  const err = toasts.find(t => /connection refused/.test(t.msg));
  assert.ok(err, JSON.stringify(toasts));
  assert.match(err.msg, /^Automaton action failed:/);
  assert.equal(err.ms, 3000);
});

test('renderTask surfaces the smoke kind chip and the verification LLM badge', () => {
  const sandbox = loadAppJS();
  const task = {
    id: 't-smoke',
    title: 'Smoke: verify build',
    kind: 'smoke',
    status: 'done',
    verification_backend: 'anthropic',
    verification_model: 'claude-x',
  };
  const html = call(sandbox, 'renderTask', { id: 'p1', status: 'draft', stories: [] }, {}, task, true);
  assert.match(html, /prd-task-kind-badge/);
  assert.match(html, /vfy: anthropic \/ claude-x/);
  const gate = { id: 't-gate', title: 'final gate', kind: 'final_gate', status: 'running' };
  const gateHtml = call(sandbox, 'renderTask', { id: 'p1', status: 'draft', stories: [] }, {}, gate, true);
  assert.match(gateHtml, /final-gate/);
  const plain = { id: 't-plain', title: 'normal task', status: 'running' };
  const plainHtml = call(sandbox, 'renderTask', { id: 'p1', status: 'draft', stories: [] }, {}, plain, true);
  assert.doesNotMatch(plainHtml, /prd-task-kind-badge/);
  assert.doesNotMatch(plainHtml, /vfy:/);
});
