'use strict';
// app-review-state.test.js
//
// 2026-11-03 — Review state (timed-out hold): badge + retry/reroute actions +
// strong-backend / verdict-cache settings fields + locale coverage. A PRD
// whose judge hit its time limit is held in the blocked state (review_state
// {state:'timed_out', cause, action}); the card and detail views must show a
// distinct "Review timed out" badge — NOT the block badge — with the cause
// and the next actions from the hold: always Retry, plus Reroute when the
// action is "reroute" (re-judged pinned to the PRD's stronger guardrail
// backend, prd.guardrail_backend). Both actions call the existing rejudge
// endpoint POST /api/autonomous/prds/{id}/rejudge. A real block
// (state:'blocked') must not get the timeout badge; it keeps the block
// treatment.
//
// Run: node --test internal/server/web/app-review-state.test.js
// (or node internal/server/web/app-review-state.test.js)

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub.js');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES = path.join(__dirname, 'locales') + path.sep;

const CAUSE = 'judge timed out on 2 backend(s): ollama, openwebui, limit 30m0s';

function timedOutPRD(overrides) {
  return Object.assign({
    id: 'id1',
    title: 'Hold PRD',
    status: 'blocked',
    review_state: { state: 'timed_out', cause: CAUSE, action: 'reroute' },
    guardrail_backend: 'claude-opus',
  }, overrides || {});
}

async function loadApp(routes, extra) {
  const byId = {};
  const calls = [];
  const sandbox = buildSandbox({ byId, ...extra });
  vm.createContext(sandbox);
  sandbox.fetch = async (url, init) => {
    const u = String(url).split('?')[0];
    const routeName = init && init.routeName;
    const match = routes[u];
    calls.push({ url: u, method: (init && init.method) || 'GET', body: init && init.body, route: u, routeName: routeName || '', status: match ? match.status : 404 });
    if (match) {
      return {
        ok: match.status < 400,
        status: match.status,
        json() { return Promise.resolve(match.body); },
        text() {
          if (typeof match.body === 'string') return Promise.resolve(match.body);
          try { return Promise.resolve(JSON.stringify(match.body)); } catch (e) { return Promise.resolve(''); }
        },
      };
    }
    return {
      ok: false, status: 404,
      json() { return Promise.resolve({}); },
      text() { return Promise.resolve('not found'); },
    };
  };
  sandbox._refreshAutomataOrPRD = () => {};
  loadScript(APP_JS, sandbox);
  sandbox.calls = calls;
  sandbox.GENERAL_CONFIG_FIELDS = vm.runInContext('GENERAL_CONFIG_FIELDS', sandbox);
  // initI18n() is fire-and-forget at load; drive it explicitly so locale
  // bundles (en fallback) are resolved before t() assertions.
  if (typeof sandbox.initI18n === 'function' || typeof sandbox.window.initI18n === 'function') {
    const fn = sandbox.initI18n || sandbox.window.initI18n;
    try { await fn(); } catch (e) { /* i18n is best-effort in the stub */ }
  }
  await flushAsync();
  return sandbox;
}

async function sandboxWithRoutes() {
  const routes = {
    '/locales/en.json': { status: 200, body: JSON.parse(fs.readFileSync(LOCALES + 'en.json', 'utf8')) },
    '/api/autonomous/prds/id1/rejudge': { status: 200, body: { status: 'rejudging' } },
  };
  return loadApp(routes);
}

test('badge: Review timed out with cause + Retry + Reroute when action is reroute', async () => {
  const sb = await sandboxWithRoutes();
  const html = sb.reviewTimedOutBadgeHtml(timedOutPRD());
  assert.ok(html.length > 0, 'badge must render for a timed_out hold');
  assert.ok(html.includes('Review timed out'), 'shows the localized title, got: ' + html.slice(0, 200));
  assert.ok(html.includes(CAUSE), 'shows the cause from review_state.cause');
  assert.ok(html.includes('Cause') || html.includes('review_timed_out_cause_prefix'), 'shows the cause prefix');
  assert.ok(html.includes('reviewTimedOutBadge') || html.includes('review-retry-btn'), 'shows a Retry button');
  assert.ok(html.includes('reviewRetry(&quot;id1&quot;)') || html.includes('reviewRetry(\'id1\')'), 'Retry calls reviewRetry with the PRD id');
  assert.ok(html.includes('review-reroute-btn'), 'shows a Reroute button when action is reroute');
  assert.ok(html.includes('reviewReroute(&quot;id1&quot;,&quot;claude-opus&quot;)') || html.includes("reviewReroute('id1','claude-opus')"), 'Reroute pins the PRD guardrail backend');
  // A timeout hold must NOT be dressed as a block.
  assert.ok(!html.includes('prd-card-status-blocked'), 'no block status badge on the timeout hold');
  assert.ok(!html.includes('⛔'), 'no block glyph in the timeout badge');
});

test('badge: Retry only when action is retry', async () => {
  const sb = await sandboxWithRoutes();
  const prd = timedOutPRD({ review_state: { state: 'timed_out', cause: CAUSE, action: 'retry' }, guardrail_backend: '' });
  const html = sb.reviewTimedOutBadgeHtml(prd);
  assert.ok(html.includes('Review timed out'), 'title still shown');
  assert.ok(html.includes('review-retry-btn'), 'Retry shown');
  assert.ok(!html.includes('review-reroute-btn'), 'no Reroute when action is not reroute');
});

test('badge: absent for real blocks, passed/pending, or no review_state', async () => {
  const sb = await sandboxWithRoutes();
  assert.equal(sb.reviewTimedOutBadgeHtml(timedOutPRD({ review_state: { state: 'blocked', cause: 'guardrail g1 returned block: unsafe' } })), '');
  assert.equal(sb.reviewTimedOutBadgeHtml(timedOutPRD({ review_state: { state: 'passed' } })), '');
  assert.equal(sb.reviewTimedOutBadgeHtml(timedOutPRD({ review_state: { state: 'pending' } })), '');
  assert.equal(sb.reviewTimedOutBadgeHtml(timedOutPRD({ review_state: undefined })), '');
  assert.equal(sb.reviewTimedOutBadgeHtml({ id: 'id1', status: 'blocked' }), '');
  assert.equal(sb.reviewTimedOutBadgeHtml(timedOutPRD({ id: '' })), '');
  assert.equal(sb.reviewTimedOutBadgeHtml(null), '');
});

test('reviewRetry POSTs the rejudge endpoint without a backend, then refreshes', async () => {
  const sb = await loadApp({
    '/locales/en.json': { status: 200, body: { review_timed_out_retry: 'Retry' } },
    '/api/autonomous/prds/id1/rejudge': { status: 200, body: { status: 'rejudging' } },
  });
  let refreshed = false;
  sb._refreshAutomataOrPRD = () => { refreshed = true; };
  await sb.reviewRetry('id1');
  assert.equal(refreshed, true, 'refreshes the automata view after success');
  const post = sb.calls ? sb.calls : [];
  assert.ok(post.some(c => c.url === '/api/autonomous/prds/id1/rejudge' && c.method === 'POST'), 'POST /rejudge seen: ' + JSON.stringify(post));
  const c = post.find(c => c.url === '/api/autonomous/prds/id1/rejudge' && c.method === 'POST');
  const body = JSON.parse(c.body);
  assert.equal(body.backend, undefined, 'Retry must not pin a backend');
  assert.equal(body.actor, 'operator');
});

test('reviewReroute POSTs the rejudge endpoint pinned to the strong backend', async () => {
  const sb = await loadApp({
    '/locales/en.json': { status: 200, body: { review_timed_out_retry: 'Retry' } },
    '/api/autonomous/prds/id1/rejudge': { status: 200, body: { status: 'rejudging' } },
  });
  await sb.reviewReroute('id1', 'claude-opus');
  const c = sb.calls.find(c => c.url === '/api/autonomous/prds/id1/rejudge' && c.method === 'POST');
  assert.ok(c, 'POST /rejudge seen');
  const body = JSON.parse(c.body);
  assert.equal(body.backend, 'claude-opus', 'Reroute pins the strong backend');
  assert.equal(body.actor, 'operator');
});

test('rejudge reply message is shown in the success toast; no message keeps the existing toast', async () => {
  const msg = 'rejudge complete; the stored verdict reused because the head commit and the rules text are unchanged; no new review ran';
  const withMsg = await loadApp({
    '/locales/en.json': { status: 200, body: { review_timed_out_retry: 'Retry done' } },
    '/api/autonomous/prds/id1/rejudge': { status: 200, body: { id: 'id1', status: 'blocked', message: msg } },
  });
  const toasts = [];
  withMsg.showToast = (m, type, ms) => { toasts.push({ m, type, ms }); };
  await withMsg.reviewRetry('id1');
  assert.equal(toasts.length, 1, 'one toast, got: ' + JSON.stringify(toasts));
  assert.ok(toasts[0].m.includes('stored verdict reused'), 'toast shows the reply message, got: ' + toasts[0].m);
  assert.equal(toasts[0].type, 'success');
  assert.equal(toasts[0].ms, 6000, 'message toast stays about six seconds');
  const post = withMsg.calls.find(c => c.method === 'POST' && c.url === '/api/autonomous/prds/id1/rejudge');
  assert.ok(post, 'POST /rejudge seen');

  const noMsg = await loadApp({
    '/locales/en.json': { status: 200, body: { review_timed_out_retry: 'Retry done' } },
    '/api/autonomous/prds/id1/rejudge': { status: 200, body: { id: 'id1', status: 'blocked' } },
  });
  const plain = [];
  noMsg.showToast = (m, type, ms) => { plain.push({ m, type, ms }); };
  await noMsg.reviewReroute('id1', 'claude-opus');
  assert.equal(plain.length, 1);
  assert.equal(plain[0].m, 'Retry done', 'no message keeps the existing toast text');
  assert.equal(plain[0].ms, 2500, 'existing toast duration kept');
});

test('failed re-judgment rejects and surfaces an error toast', async () => {
  const sb = await loadApp({
    '/locales/en.json': { status: 200, body: {} },
    '/api/autonomous/prds/id1/rejudge': { status: 500, body: 'judge backend down' },
  });
  const dock = sb._alertDock || sb.window._alertDock;
  assert.ok(dock, 'alert dock reachable');
  await assert.rejects(sb.reviewRetry('id1'), /judge backend down|Re-judgment failed/);
  const msgs = (dock.alerts || []).map(a => a.message);
  assert.ok(msgs.some(m => /Re-judgment failed/.test(m)), 'error toast recorded, got: ' + JSON.stringify(msgs));
});

test('general config fields: strong backend + verdict cache after the judge size threshold', async () => {
  const sb = await sandboxWithRoutes();
  const fields = JSON.parse(vm.runInContext(
    "JSON.stringify(((GENERAL_CONFIG_FIELDS || []).find(s => s.id === 'autonomous') || {fields: []}).fields)",
    sb));
  assert.ok(Array.isArray(fields) && fields.length > 0, 'autonomous section fields reachable from the sandbox');
  const idx = fields.findIndex(f => f.key === 'autonomous.judge_size_threshold_lines');
  assert.ok(idx >= 0, 'judge_size_threshold field present');
  const strong = fields[idx + 1];
  const cache = fields[idx + 2];
  assert.equal(strong.key, 'autonomous.judge_strong_backend');
  assert.equal(strong.labelKey, 'settings_judge_strong_backend');
  assert.equal(strong.helpKey, 'settings_judge_strong_backend_help');
  assert.equal(cache.key, 'autonomous.judge_verdict_cache_enabled');
  assert.equal(cache.labelKey, 'settings_judge_verdict_cache');
  assert.equal(cache.helpKey, 'settings_judge_verdict_cache_help');
});

test('badges wired into the card and detail views', async () => {
  const sb = await sandboxWithRoutes();
  const src = fs.readFileSync(AppPath(), 'utf8');
  const cardFn = src.slice(src.indexOf('function renderAutomataCard('), src.indexOf('// GH#182 — hand-rolled SVG DAG'));
  assert.ok(cardFn.includes('reviewTimedOutBadgeHtml(prd)'), 'card renders the timeout badge');
  const detailFn = src.slice(src.indexOf('function _renderDetailHeader('), src.indexOf('// v6.13.1 (C4 + C10 + C12)'));
  assert.ok(detailFn.includes('reviewTimedOutBadgeHtml(prd)'), 'detail header renders the timeout badge');
});

function AppPath() { return APP_JS; }

test('locales: all five carry the review_timed_out_* and settings_judge_* keys', () => {
  const need = [
    'review_timed_out_title',
    'review_timed_out_cause_prefix',
    'review_timed_out_retry',
    'review_timed_out_reroute',
    'settings_judge_strong_backend',
    'settings_judge_strong_backend_help',
    'settings_judge_verdict_cache',
    'settings_judge_verdict_cache_help',
  ];
  let baseKeys = null;
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const d = JSON.parse(fs.readFileSync(LOCALES + lang + '.json', 'utf8'));
    for (const k of need) {
      assert.ok(typeof d[k] === 'string' && d[k].trim().length > 0, `${lang}: ${k} set`);
    }
  }
});
