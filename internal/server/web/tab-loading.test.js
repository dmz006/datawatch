// Every list tab (Sessions, Automata, Automation, Alerts, Observer stats)
// shows an indicator in each of its states: loading, failed (with Retry),
// empty and loaded. Opening an Automata card fetches its stories and tasks on
// demand, and refreshes neither flash the spinner nor close open cards.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const EN = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales/en.json'), 'utf8'));

// A page with controllable network: route(url) returns {status, body} or a
// promise of it; every request is recorded in `calls`.
function harness(route, panelIds = []) {
  const panels = {};
  const byId = {};
  for (const id of panelIds) { panels[id] = makeStubElement(); byId[id] = panels[id]; }
  const sandbox = buildSandbox({ byId });
  sandbox.setInterval = () => 0;
  sandbox.clearInterval = () => {};
  sandbox.AbortController = AbortController;
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n = sandbox._i18n || {};
  sandbox._i18n.bundle = EN;
  // These tests drive the Automata sub-tab; a first visit opens on Automation.
  vm.runInContext("_automataState.tab = 'automata'", sandbox);
  const calls = [];
  sandbox.fetch = (url, init) => {
    calls.push(String(url));
    return Promise.resolve(route(String(url), init)).then(r => {
      if (r instanceof Error) throw r;
      const status = r.status || 200;
      const text = typeof r.body === 'string' ? r.body : JSON.stringify(r.body === undefined ? {} : r.body);
      return { ok: status >= 200 && status < 300, status, headers: { get() { return null; } }, text: () => Promise.resolve(text), json: () => Promise.resolve(JSON.parse(text)) };
    });
  };
  const run = code => vm.runInContext(code, sandbox);
  return { sandbox, panels, calls, run };
}
const defer = () => { let res; const p = new Promise(r => { res = r; }); return { p, res }; };

// ── Automata ────────────────────────────────────────────────────────────────

const ROW = (id, extra) => Object.assign({ id, title: 'Auto ' + id, status: 'running', rev: 1, updated_at: '2026-10-23T01:00:00Z', story_count: 2, task_count: 4, task_status_counts: { completed: 1, pending: 3 } }, extra);
const FULL = id => Object.assign(ROW(id), { stories: [{ id: 's1', title: 'Story one', status: 'pending', tasks: [{ id: 't1', title: 'Task one', status: 'pending' }] }] });

function automataRoutes(over = {}) {
  return (url) => {
    if (url.startsWith('/api/autonomous/prds/')) return over.detail ? over.detail(url) : { body: FULL(decodeURIComponent(url.split('/').pop())) };
    if (url.startsWith('/api/autonomous/prds')) return over.list ? over.list(url) : { body: { prds: [ROW('p1'), ROW('p2')], total: 2 } };
    if (url.startsWith('/api/llms')) return { body: { llms: [] } };
    if (url.startsWith('/api/autonomous/tags')) return { body: { tags: [] } };
    if (url.startsWith('/api/capacity')) return { body: {} };
    return { body: {} };
  };
}

test('automata: loading indicator shows before the first response, then the cards', async () => {
  const d = defer();
  const h = harness(automataRoutes({ list: () => d.p }), ['automataPanel']);
  h.run('loadAutomataPanel()');
  assert.match(h.panels.automataPanel.innerHTML, /list-state-skeleton/);
  assert.match(h.panels.automataPanel.innerHTML, /Loading/);
  d.res({ body: { prds: [ROW('p1'), ROW('p2')], total: 2 } });
  await flushAsync(30);
  assert.match(h.panels.automataPanel.innerHTML, /id="prd-p1"/);
  assert.match(h.panels.automataPanel.innerHTML, /id="prd-p2"/);
  assert.doesNotMatch(h.panels.automataPanel.innerHTML, /list-state-skeleton/);
});

test('automata: the list, LLMs, tags and capacity are fetched in parallel, the list as a paged summary', async () => {
  const h = harness(automataRoutes(), ['automataPanel']);
  const p = h.run('loadAutomataPanel()');
  // All four requests are issued before any response is awaited.
  assert.ok(h.calls.some(u => u.startsWith('/api/autonomous/prds?') && u.includes('limit=') && u.includes('active=1')), h.calls.join());
  assert.ok(h.calls.includes('/api/llms'));
  assert.ok(h.calls.includes('/api/autonomous/tags?counts=true'));
  assert.ok(h.calls.includes('/api/capacity'));
  assert.ok(!h.calls.some(u => u.includes('full=1')), 'never the full list');
  await p;
});

test('automata: status filter and search go to the server as status= and q=', async () => {
  const h = harness(automataRoutes(), ['automataPanel']);
  await h.run('loadAutomataPanel()');
  h.calls.length = 0;
  h.run(`_automataState.statusFilter.add('blocked'); _automataState.search = 'zed'; loadAutomataPanel()`);
  const listCall = h.calls.find(u => u.startsWith('/api/autonomous/prds?'));
  assert.match(listCall, /status=blocked/);
  assert.match(listCall, /q=zed/);
  assert.doesNotMatch(listCall, /active=1/);
});

test('automata: failed load shows the error with Retry, and Retry loads again', async () => {
  let fail = true;
  const h = harness(automataRoutes({ list: () => (fail ? { status: 500, body: 'boom' } : { body: { prds: [ROW('p1')], total: 1 } }) }), ['automataPanel']);
  await h.run('loadAutomataPanel()');
  assert.match(h.panels.automataPanel.innerHTML, /list-state-failed/);
  assert.match(h.panels.automataPanel.innerHTML, /boom/);
  assert.match(h.panels.automataPanel.innerHTML, /Retry/);
  fail = false;
  await h.run(`window._listLoaders.automata.retry()`);
  assert.match(h.panels.automataPanel.innerHTML, /id="prd-p1"/);
  assert.doesNotMatch(h.panels.automataPanel.innerHTML, /list-state-failed/);
});

test('automata: an empty list says so instead of leaving the tab blank', async () => {
  const h = harness(automataRoutes({ list: () => ({ body: { prds: [], total: 0 } }) }), ['automataPanel']);
  await h.run('loadAutomataPanel()');
  assert.match(h.panels.automataPanel.innerHTML, /list-state-empty/);
  assert.ok(h.panels.automataPanel.innerHTML.length > 50);
});

test('automata: Load more fetches the next page with the cursor and appends', async () => {
  const h = harness(automataRoutes({
    list: url => (url.includes('cursor=c1')
      ? { body: { prds: [ROW('p3')], total: 3 } }
      : { body: { prds: [ROW('p1'), ROW('p2')], total: 3, next_cursor: 'c1' } }),
  }), ['automataPanel']);
  await h.run('loadAutomataPanel()');
  assert.match(h.panels.automataPanel.innerHTML, /id="automataLoadMore"/);
  assert.match(h.panels.automataPanel.innerHTML, /Load more \(2\/3\)/);
  h.run('automataLoadMore()');
  await flushAsync(30);
  assert.ok(h.calls.some(u => u.includes('cursor=c1')));
  assert.match(h.panels.automataPanel.innerHTML, /id="prd-p3"/);
  assert.doesNotMatch(h.panels.automataPanel.innerHTML, /automataLoadMore/, 'no control once the last page is in');
});

test('automata: opening a card fetches its stories and tasks on demand, once', async () => {
  const h = harness(automataRoutes(), ['automataPanel']);
  await h.run('loadAutomataPanel()');
  assert.ok(!h.calls.some(u => /^\/api\/autonomous\/prds\/p1$/.test(u)), 'nothing per item before a card opens');
  assert.doesNotMatch(h.panels.automataPanel.innerHTML, /Story one/);
  h.run(`_automataCardToggle('p1', { open: true })`);
  await flushAsync(30);
  assert.equal(h.calls.filter(u => u === '/api/autonomous/prds/p1').length, 1);
  assert.match(h.run(`_automataCardDetailHtml(_automataState.allPrds.find(p => p.id === 'p1'))`), /Story one/);
  h.run(`_automataCardToggle('p1', { open: true })`);
  await flushAsync(10);
  assert.equal(h.calls.filter(u => u === '/api/autonomous/prds/p1').length, 1, 'cached, not refetched');
  assert.ok(!h.calls.includes('/api/autonomous/prds/p2'));
});

test('automata: a failed detail fetch shows an error with Retry on that card only', async () => {
  const h = harness(automataRoutes({ detail: () => ({ status: 500, body: 'nope' }) }), ['automataPanel']);
  await h.run('loadAutomataPanel()');
  h.run(`_automataCardToggle('p1', { open: true })`);
  await flushAsync(30);
  const html = h.run(`_automataCardDetailHtml(_automataState.allPrds.find(p => p.id === 'p1'))`);
  assert.match(html, /list-state-failed/);
  assert.match(html, /nope/);
  assert.match(html, /_automataLoadDetail\(&quot;p1&quot;,true\)/);
});

test('automata: a later refresh keeps the cards and never shows the spinner again', async () => {
  const h = harness(automataRoutes(), ['automataPanel']);
  await h.run('loadAutomataPanel()');
  const d = defer();
  h.sandbox.fetch = () => d.p.then(() => { throw new Error('unused'); });
  const refresh = h.run('loadAutomataPanel()');
  assert.match(h.panels.automataPanel.innerHTML, /id="prd-p1"/, 'cards stay while refreshing');
  assert.doesNotMatch(h.panels.automataPanel.innerHTML, /list-state-skeleton/);
  d.res({});
  await refresh;
  assert.match(h.panels.automataPanel.innerHTML, /list-state-failed/, 'a failed refresh is a banner above the cards');
  assert.match(h.panels.automataPanel.innerHTML, /id="prd-p1"/);
});

test('automata: the all-servers mode keeps the aggregated endpoint, unpaged', async () => {
  const h = harness((url) => {
    if (url === '/api/autonomous/prds/aggregated') return { body: [ROW('a1')] };
    return automataRoutes()(url);
  }, ['automataPanel']);
  h.run(`state.activeServer = 'all'`);
  await h.run('loadAutomataPanel()');
  assert.ok(h.calls.includes('/api/autonomous/prds/aggregated'));
  assert.ok(!h.calls.some(u => u.startsWith('/api/autonomous/prds?')));
  assert.match(h.panels.automataPanel.innerHTML, /id="prd-a1"/);
});

// ── Automation ─────────────────────────────────────────────────────────────

const AUTOMATION = { id: 'a1', name: 'Chain', status: 'active', version: 1, members: [] };

function automationRoutes(over = {}) {
  return (url) => {
    if (url === '/api/automations') return over.list ? over.list() : { body: [AUTOMATION] };
    if (url.startsWith('/api/autonomous/prds')) return over.titles ? over.titles() : { body: { prds: [] } };
    return { body: {} };
  };
}

test('automation: loading, then the cards; automations and titles are fetched in parallel', async () => {
  const d = defer();
  const h = harness(automationRoutes({ list: () => d.p }), ['automationPanel']);
  const p = h.run('loadAutomationList()');
  assert.match(h.panels.automationPanel.innerHTML, /list-state-skeleton/);
  assert.ok(h.calls.includes('/api/automations') && h.calls.includes('/api/autonomous/prds'), 'both requests issued up front');
  d.res({ body: [AUTOMATION] });
  await p;
  assert.match(h.panels.automationPanel.innerHTML, /data-automation-id="a1"/);
});

test('automation: failed shows the error with Retry; empty shows the empty text', async () => {
  let mode = 'fail';
  const h = harness(automationRoutes({ list: () => (mode === 'fail' ? { status: 500, body: 'down' } : { body: [] }) }), ['automationPanel']);
  await h.run('loadAutomationList()');
  assert.match(h.panels.automationPanel.innerHTML, /list-state-failed/);
  assert.match(h.panels.automationPanel.innerHTML, /down/);
  assert.match(h.panels.automationPanel.innerHTML, /Retry/);
  mode = 'empty';
  await h.run('window._listLoaders.automation.retry()');
  assert.match(h.panels.automationPanel.innerHTML, /list-state-empty/);
});

// ── Alerts ──────────────────────────────────────────────────────────────────

const ALERT = { id: 'x1', level: 'info', title: 'Hello alert', body: 'b', created_at: '2026-10-23T01:00:00Z', source: 'system' };

function alertsRoutes(over = {}) {
  return (url) => {
    if (url === '/api/alerts?limit=100') return over.alerts ? over.alerts() : { body: { alerts: [ALERT] } };
    if (url === '/api/commands') return { body: [] };
    if (url === '/api/sessions') return { body: [] };
    return { body: {} };
  };
}

test('alerts: loading, loaded, empty and failed all show something', async () => {
  const d = defer();
  let mode = 'pending';
  const h = harness(alertsRoutes({
    alerts: () => {
      if (mode === 'pending') return d.p;
      if (mode === 'empty') return { body: { alerts: [] } };
      if (mode === 'fail') return { status: 503, body: 'unavailable' };
      return { body: { alerts: [ALERT] } };
    },
  }), ['alertsList']);
  h.run(`state.activeView = 'alerts'; window._alertsTabSel = 'system'`);
  const loading = h.run('renderAlertsView()');
  void loading;
  assert.match(h.panels.alertsList.innerHTML, /list-state-skeleton/);
  d.res({ body: { alerts: [ALERT] } });
  await flushAsync(30);
  assert.match(h.panels.alertsList.innerHTML, /Hello alert/);
  assert.doesNotMatch(h.panels.alertsList.innerHTML, /list-state-skeleton/);

  mode = 'empty';
  await h.run('window._listLoaders["alerts:local"].load()');
  assert.match(h.panels.alertsList.innerHTML, /list-state-empty/);

  mode = 'fail';
  await h.run('window._listLoaders["alerts:local"].load()');
  assert.match(h.panels.alertsList.innerHTML, /list-state-failed/);
  assert.match(h.panels.alertsList.innerHTML, /HTTP 503/);
  assert.match(h.panels.alertsList.innerHTML, /Retry/);
});

test('alerts: a failed fetch is a failure, never "no alerts"', async () => {
  const h = harness(alertsRoutes({ alerts: () => ({ status: 500, body: 'err' }) }), ['alertsList']);
  h.run(`state.activeView = 'alerts'`);
  h.run('renderAlertsView()');
  await flushAsync(30);
  assert.match(h.panels.alertsList.innerHTML, /list-state-failed/);
  assert.doesNotMatch(h.panels.alertsList.innerHTML, /list-state-empty/);
});

// ── Observer stats ─────────────────────────────────────────────────────────

function statsPanel() {
  const panel = makeStubElement();
  const body = makeStubElement();
  body.closest = () => null;
  body.parentElement = null;
  panel.querySelector = sel => (sel === '#statsBody' && /statsBody/.test(panel.innerHTML) ? body : null);
  return { panel, body };
}

test('observer stats: loading, failed with Retry, and loaded', async () => {
  const { panel, body } = statsPanel();
  const d = defer();
  let mode = 'pending';
  const h = harness((url) => {
    if (url === '/api/stats') {
      if (mode === 'pending') return d.p;
      if (mode === 'fail') return { status: 500, body: 'internal error' };
      return { body: { timestamp: '2026-10-23T01:00:00Z', cpu_load_avg_1: 1 } };
    }
    return { body: {} };
  }, ['statsPanel']);
  h.sandbox.document.getElementById = id => (id === 'statsPanel' ? panel : makeStubElement());
  h.run('window.scrollTo = () => {}');
  h.run('loadStatsPanel()');
  assert.match(panel.innerHTML, /list-state-skeleton/);
  d.res({ status: 500, body: 'x' });
  await flushAsync(30);
  assert.match(panel.innerHTML, /list-state-failed/);
  assert.match(panel.innerHTML, /Retry/);
  assert.doesNotMatch(panel.innerHTML, /internal error|x\b/, 'a local failure keeps the generic text');

  mode = 'ok';
  await h.run('window._listLoaders["observer-stats:local"].retry()');
  assert.match(panel.innerHTML, /statsBody/);
  assert.doesNotMatch(panel.innerHTML, /list-state-failed/);
  assert.notEqual(body.innerHTML, '', 'stats rendered into the body');
});

// ── Sessions ────────────────────────────────────────────────────────────────

const SESS = (n, extra) => Object.assign({ id: 's' + n, full_id: 'host-s' + n, name: 'Session ' + n, state: 'running', backend: 'claude-code', task: 'do ' + n, updated_at: new Date().toISOString() }, extra);

function sessionsPage(over = {}) {
  return (url) => {
    if (url.startsWith('/api/sessions')) return over.sessions ? over.sessions(url) : { body: { sessions: [SESS(1), SESS(2)], total: 2 } };
    return { body: {} };
  };
}

function sessionsHarness(over) {
  const h = harness(sessionsPage(over), ['view']);
  h.run(`state.activeView = 'sessions'; state.connected = false;`);
  return h;
}

test('sessions: first load shows a loading indicator and NO stale banner', async () => {
  const d = defer();
  const h = sessionsHarness({ sessions: () => d.p });
  h.run('renderSessionsView()');
  const html = h.panels.view.innerHTML;
  assert.match(html, /Loading/);
  assert.doesNotMatch(html, /sessions-disconnected-banner/);
  assert.ok(h.calls.some(u => u.startsWith('/api/sessions?view=summary&limit=')), h.calls.join());
  d.res({ body: { sessions: [SESS(1)], total: 1 } });
  await flushAsync(30);
  assert.match(h.panels.view.innerHTML, /Session 1/);
});

test('sessions: the stale banner appears only once loaded and the connection is down', async () => {
  const h = sessionsHarness();
  h.run('renderSessionsView()');
  await flushAsync(30);
  assert.match(h.panels.view.innerHTML, /sessions-disconnected-banner/, 'loaded but not connected');
  h.run('state.connected = true; renderSessionsView()');
  assert.doesNotMatch(h.panels.view.innerHTML, /sessions-disconnected-banner/, 'fresh data over a live connection');
});

test('sessions: data older than the polling interval after a failed refresh is stale', () => {
  const h = sessionsHarness();
  h.run(`state.connected = true; _sl().loaded = true; _sl().at = Date.now() - SESSIONS_POLL_MS - 1000; _sl().error = '';`);
  assert.equal(h.run('_sessionsStale()'), false, 'old but no error and connected: the socket keeps it live');
  h.run(`_sl().error = 'HTTP 500'`);
  assert.equal(h.run('_sessionsStale()'), true);
});

test('sessions: failed first load shows the error with Retry, and Retry loads again', async () => {
  let fail = true;
  const h = sessionsHarness({ sessions: () => (fail ? { status: 500, body: 'dead' } : { body: { sessions: [SESS(1)], total: 1 } }) });
  h.run('renderSessionsView()');
  await flushAsync(30);
  assert.match(h.panels.view.innerHTML, /list-state-failed/);
  assert.match(h.panels.view.innerHTML, /Retry/);
  assert.doesNotMatch(h.panels.view.innerHTML, /sessions-disconnected-banner/);
  fail = false;
  h.run('loadSessionsList()');
  await flushAsync(30);
  assert.match(h.panels.view.innerHTML, /Session 1/);
});

test('sessions: an empty list says there are no sessions', async () => {
  const h = sessionsHarness({ sessions: () => ({ body: { sessions: [], total: 0 } }) });
  h.run('state.connected = true; renderSessionsView()');
  await flushAsync(30);
  assert.match(h.panels.view.innerHTML, /No active sessions/);
});

test('sessions: Load more pages with the cursor', async () => {
  const h = sessionsHarness({
    sessions: url => (url.includes('cursor=c9')
      ? { body: { sessions: [SESS(3)], total: 3 } }
      : { body: { sessions: [SESS(1), SESS(2)], total: 3, next_cursor: 'c9' } }),
  });
  h.run('state.connected = true; renderSessionsView()');
  await flushAsync(30);
  assert.match(h.panels.view.innerHTML, /id="sessionsLoadMore"/);
  h.run('sessionsLoadMore()');
  await flushAsync(30);
  assert.ok(h.calls.some(u => u.includes('cursor=c9')));
  assert.match(h.panels.view.innerHTML, /Session 3/);
  assert.doesNotMatch(h.panels.view.innerHTML, /sessionsLoadMore/);
});

test('sessions: opening a session whose summary row was cut fetches GET /api/sessions/{id}', async () => {
  const h = sessionsHarness({
    sessions: url => (url === '/api/sessions/host-s1'
      ? { body: SESS(1, { task: 'the whole task text' }) }
      : { body: { sessions: [SESS(1, { task: 'the whole…', truncated_fields: ['task'] })], total: 1 } }),
  });
  h.run('state.connected = true; renderSessionsView()');
  await flushAsync(30);
  h.run(`_ensureFullSession('host-s1', state.sessions[0])`);
  await flushAsync(30);
  assert.ok(h.calls.includes('/api/sessions/host-s1'));
  assert.equal(h.run('state.sessions[0].task'), 'the whole task text');
});

test('sessions: a summary row never overwrites full text already held', () => {
  const h = sessionsHarness();
  h.run(`state.sessions = [{ full_id: 'host-s1', id: 's1', state: 'running', task: 'the whole task text' }]`);
  h.run(`_mergeSessionRow({ full_id: 'host-s1', id: 's1', state: 'running', task: 'the whole…', truncated_fields: ['task'] })`);
  assert.equal(h.run('state.sessions[0].task'), 'the whole task text');
  assert.equal(h.run('state.sessions[0].truncated_fields'), undefined);
});
