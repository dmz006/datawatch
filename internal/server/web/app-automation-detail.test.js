// Automation tab — detail page.
//
// Covers the request the page issues, the rendered header, blocked banner,
// budget, active members, strip, pending inserts and decisions, the member
// click-through and details drawer, the nested toggle, live refresh, the
// failed-load text and escaping. fetch is stubbed; rendered text is the real
// English bundle.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

// loadApp returns a sandbox with the app loaded and a recording fetch. routes
// maps a request path to {status, body}; an unknown path answers 404.
function loadApp(routes, byId) {
  const sandbox = buildSandbox({ byId: byId || {} });
  const calls = [];
  sandbox.fetch = (url, opts) => {
    calls.push({ url, method: (opts && opts.method) || 'GET' });
    const hit = routes[url];
    if (!hit) return Promise.resolve({ ok: false, status: 404, statusText: 'Not Found', text: () => Promise.resolve('not found') });
    const status = hit.status || 200;
    return Promise.resolve({
      ok: status < 400,
      status,
      statusText: String(status),
      json: () => Promise.resolve(hit.body),
      text: () => Promise.resolve(typeof hit.body === 'string' ? hit.body : JSON.stringify(hit.body)),
    });
  };
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  sandbox.__calls = calls;
  return sandbox;
}

const stateOf = sb => vm.runInContext('_automationState', sb);

function member(id, status, extra) {
  return Object.assign({ id, prd_id: id + 'aaaaaaaaaaaa', status, added_by: 'operator' }, extra || {});
}

function automation(extra) {
  return Object.assign({
    id: 'a1b2c3d4', name: 'Release train', description: 'Builds then ships', status: 'active', version: 7,
    entry_member_id: 'm1', active_cursors: ['m2'],
    members: [
      member('m1', 'completed', { on_complete: 'm2', edge_taken: 'on_complete' }),
      member('m2', 'running', { on_complete: 'm3' }),
      member('m3', 'pending'),
    ],
    budget_policy: { ceiling_type: 'dollar', ceiling_value: 25, window: 'week', pacing: 'none', on_breach: 'pause' },
  }, extra || {});
}

// openDetail loads one Automation into a recording #view and resolves the html.
async function openDetail(a, opts) {
  const o = opts || {};
  const view = makeStubElement();
  const strip = makeStubElement();
  const routes = {
    ['/api/automations/' + encodeURIComponent(a.id)]: { body: a },
    '/api/autonomous/prds': { body: { prds: o.prds || [] } },
  };
  const sb = loadApp(routes, { view, automationStrip: strip });
  if (o.list) stateOf(sb).list = o.list;
  sb.openAutomationDetail(a.id);
  await flushAsync();
  return { sb, view, strip, html: view.innerHTML };
}

test('renderAutomationDetail issues GET /api/automations/ID with the id URL-encoded', async () => {
  const view = makeStubElement();
  const sb = loadApp({
    '/api/automations/a%20b%2Fc': { body: automation({ id: 'a b/c' }) },
    '/api/autonomous/prds': { body: { prds: [] } },
  }, { view });
  sb.openAutomationDetail('a b/c');
  await flushAsync();
  const detail = sb.__calls.find(c => c.url.startsWith('/api/automations/'));
  assert.ok(detail, 'the detail request was made');
  assert.equal(detail.url, '/api/automations/a%20b%2Fc');
  assert.equal(detail.method, 'GET');
  assert.equal(stateOf(sb).detailId, 'a b/c');
  assert.equal(stateOf(sb).detail.name, 'Release train');
});

test('the page shows a loading block with a back control while the load is pending', () => {
  const view = makeStubElement();
  const sb = loadApp({}, { view });
  sb.renderAutomationDetail('a1b2c3d4');
  assert.match(view.innerHTML, /automation-detail/);
  assert.match(view.innerHTML, /Back to Automations/);
  assert.ok(!view.innerHTML.includes('automation-detail-title'));
});

test('the page shows name, status, version, description, budget and the strip with its member ids', async () => {
  const { html, sb } = await openDetail(automation(), {
    prds: [{ id: 'm2aaaaaaaaaaaa', title: 'Ship it', status: 'running' }],
  });
  assert.match(html, /<h2 class="automation-detail-title">Release train<\/h2>/);
  assert.match(html, />Active<\/span>/);
  assert.match(html, />Version 7<\/span>/);
  assert.match(html, /Builds then ships/);
  assert.match(html, /Ceiling: \$25/);
  assert.match(html, /Weekly/);
  assert.match(html, /<div id="automationControls"><\/div>/);
  assert.match(html, /<div id="automationStrip"><div class="automation-strip" data-automation-id="a1b2c3d4">/);
  const ids = (html.match(/<div class="automation-member [^>]*data-member-id="([^"]*)"/g) || []).map(s => s.match(/data-member-id="([^"]*)"/)[1]);
  assert.deepEqual(ids, ['m1', 'm2', 'm3']);
  assert.match(html, /Active members/);
  assert.match(html, /<li class="automation-active-member" data-member-id="m2">Ship it<\/li>/);
  assert.equal(stateOf(sb).prdTitles.m2aaaaaaaaaaaa, 'Ship it');
  assert.equal(stateOf(sb).prdStatuses.m2aaaaaaaaaaaa, 'running');
  assert.ok(!html.includes('automation-blocked-banner'));
});

test('a blocked Automation shows the blocked banner with the latest blocked reason', async () => {
  const { html } = await openDetail(automation({
    status: 'blocked',
    decisions: [
      { at: '2026-10-22T10:00:00Z', kind: 'blocked', note: 'old reason' },
      { at: '2026-10-22T11:00:00Z', kind: 'blocked', note: 'budget ceiling reached' },
    ],
  }));
  assert.match(html, /<div class="automation-blocked-banner" role="alert">Blocked: budget ceiling reached<\/div>/);
  assert.ok(!html.includes('old reason</div>'));
});

test('an approved Automaton shows its member as approved in the strip', async () => {
  const a = automation({ members: [member('m1', 'pending'), member('m2', 'pending')], entry_member_id: 'm1', active_cursors: [] });
  a.members[0].on_complete = 'm2';
  const { html } = await openDetail(a, { prds: [{ id: 'm1aaaaaaaaaaaa', title: 'One', status: 'approved' }] });
  assert.match(html, /data-member-id="m1" data-kind="prd" data-status="approved"/);
  assert.match(html, /data-member-id="m2" data-kind="prd" data-status="pending"/);
});

test('pending inserts list the reason and who asked', async () => {
  const { html } = await openDetail(automation({
    pending_inserts: [{ id: 'p1', prd_id: 'cccc3333dddd', name: 'Hotfix', actor: 'agent:7', reason: 'prod is down', status: 'pending' }],
  }));
  assert.match(html, /Pending inserts/);
  assert.match(html, /<li class="automation-pending-insert" data-insert-id="p1">/);
  assert.match(html, />Hotfix</);
  assert.match(html, /Requested by agent:7/);
  assert.match(html, /prod is down/);
});

test('decisions list the 20 most recent, newest first', async () => {
  const decisions = [];
  for (let i = 1; i <= 25; i++) decisions.push({ at: '2026-10-22T10:00:' + String(i).padStart(2, '0') + 'Z', kind: 'run', note: 'note-' + i });
  const { html } = await openDetail(automation({ decisions }));
  assert.match(html, /Recent decisions/);
  const notes = (html.match(/note-\d+/g) || []);
  assert.equal(notes.length, 20);
  assert.equal(notes[0], 'note-25');
  assert.equal(notes[19], 'note-6');
});

test('an unfinished join with finished branches shows the join-wait summary', async () => {
  const a = automation({
    entry_member_id: 'f', active_cursors: ['b2', 'j'],
    members: [
      member('f', 'completed', { kind: 'fork', fork_members: ['b1', 'b2'], on_complete: 'j' }),
      member('b1', 'completed'),
      member('b2', 'running'),
      member('j', 'pending', { kind: 'join', join_members: ['b1', 'b2'], on_complete: '' }),
    ],
  });
  const { html } = await openDetail(a);
  assert.match(html, /<div class="automation-join-wait">Join waiting: 1 of 2 branches finished<\/div>/);
});

test('renderAutomationControls runs with the loaded Automation when it exists', async () => {
  const view = makeStubElement();
  const a = automation();
  const sb = loadApp({
    ['/api/automations/' + a.id]: { body: a },
    '/api/autonomous/prds': { body: [] },
  }, { view });
  const seen = [];
  sb.renderAutomationControls = x => seen.push(x.id);
  sb.openAutomationDetail(a.id);
  await flushAsync();
  assert.deepEqual(seen, ['a1b2c3d4']);
});

test('the strip carries a details button and a click-through naming the member prd_id', async () => {
  const { html } = await openDetail(automation());
  assert.match(html, /openAutomationMemberPRD\('m1aaaaaaaaaaaa'\)/);
  assert.match(html, /<button type="button" class="automation-member-details-btn" data-details-for="m2" onclick="event\.stopPropagation\(\); if \(typeof openAutomationMemberDrawer === 'function'\) openAutomationMemberDrawer\('m2'\)">Details<\/button>/);
});

test('openAutomationMemberPRD opens the Automaton page from the Automata tab and remembers the Automation', async () => {
  const { sb } = await openDetail(automation());
  const log = [];
  sb.navigate = v => { log.push(['navigate', v]); };
  sb.renderPRDDetailView = id => { log.push(['detail', id]); };
  sb.openAutomationMemberPRD('m2aaaaaaaaaaaa');
  assert.deepEqual(log, [['navigate', 'autonomous'], ['detail', 'm2aaaaaaaaaaaa']]);
  assert.equal(sb._automationReturnId, 'a1b2c3d4');
});

test('the Automaton back control returns to the Automation it was opened from', async () => {
  const { sb } = await openDetail(automation());
  const log = [];
  sb.navigate = v => { log.push(['navigate', v]); };
  sb.renderPRDDetailView = () => {};
  sb.openAutomationMemberPRD('m2aaaaaaaaaaaa');
  sb.openAutomationDetail = id => { log.push(['open', id]); };
  log.length = 0;
  sb._backToAutomataList();
  assert.deepEqual(log, [['navigate', 'autonomous'], ['open', 'a1b2c3d4']]);
  assert.equal(vm.runInContext('_automataState.tab', sb), 'automation');
});

test('the member drawer shows id, kind, status, outcome, edge and times, and the insert fields for an inserted member', async () => {
  const a = automation({
    members: [
      member('m1', 'completed', {
        on_complete: 'm2', edge_taken: 'on_complete', outcome: 'completed',
        started_at: '2026-10-22T09:00:00Z', finished_at: '2026-10-22T09:30:00Z',
      }),
      member('m2', 'pending', { added_by: 'agent:9', added_reason: 'needs a lint pass', added_at: '2026-10-22T09:10:00Z' }),
    ],
    active_cursors: [],
  });
  const { sb } = await openDetail(a);
  const drawer = makeStubElement();
  let appended = null;
  sb.document.createElement = () => drawer;
  sb.document.body.appendChild = el => { appended = el; };

  sb.openAutomationMemberDrawer('m1');
  assert.equal(appended, drawer);
  assert.equal(drawer.id, 'automationMemberDrawer');
  let html = drawer.innerHTML;
  assert.match(html, /Member details/);
  assert.match(html, /<dt>ID<\/dt><dd>m1<\/dd>/);
  assert.match(html, /<dt>Kind<\/dt><dd>Automaton<\/dd>/);
  assert.match(html, /<dt>Status<\/dt><dd>Completed<\/dd>/);
  assert.match(html, /<dt>Outcome<\/dt><dd>completed<\/dd>/);
  assert.match(html, /<dt>Edge taken<\/dt><dd>On complete<\/dd>/);
  assert.match(html, /<dt>Started<\/dt><dd>(?!—)/);
  assert.match(html, /<dt>Finished<\/dt><dd>(?!—)/);
  assert.ok(!html.includes('Added by'), 'an original member has no insert fields');

  sb.openAutomationMemberDrawer('m2');
  html = drawer.innerHTML;
  assert.match(html, /<dt>Added by<\/dt><dd>agent:9<\/dd>/);
  assert.match(html, /<dt>Reason<\/dt><dd>needs a lint pass<\/dd>/);
  assert.match(html, /<dt>Added at<\/dt><dd>(?!—)/);
  assert.match(html, /<dt>Started<\/dt><dd>—<\/dd>/);
});

test('the nested toggle expands a child Automation taken from the loaded list', async () => {
  const child = { id: 'c1c1c1c1', name: 'Child chain', status: 'active', entry_member_id: 'k1', members: [member('k1', 'running')] };
  const a = automation({
    entry_member_id: 'n1', active_cursors: [],
    members: [{ id: 'n1', kind: 'automation', automation_id: 'c1c1c1c1', status: 'running', added_by: 'operator' }],
  });
  const { sb, strip, html } = await openDetail(a, { list: [a, child] });
  assert.match(html, /data-member-id="n1" data-kind="automation"[^>]*data-expanded="false"/);
  assert.ok(!html.includes('data-member-id="k1"'), 'the child strip is collapsed at first');

  sb.toggleAutomationNested('n1');
  assert.match(strip.innerHTML, /<div class="automation-nested" data-automation-id="c1c1c1c1" data-expanded="true">/);
  assert.match(strip.innerHTML, /data-member-id="k1"/);
  assert.match(strip.innerHTML, /Child chain/);

  sb.toggleAutomationNested('n1');
  assert.ok(!strip.innerHTML.includes('data-member-id="k1"'));
});

test('renderAutomationStrip redraws only the strip from the loaded Automation', async () => {
  const { sb, strip } = await openDetail(automation());
  strip.innerHTML = '';
  sb.renderAutomationStrip();
  assert.match(strip.innerHTML, /^<div class="automation-strip" data-automation-id="a1b2c3d4">/);
});

test('a failed load shows the load-failed text and a back control', async () => {
  const view = makeStubElement();
  const sb = loadApp({
    '/api/automations/a1b2c3d4': { status: 500, body: 'database is down' },
    '/api/autonomous/prds': { body: [] },
  }, { view });
  sb.openAutomationDetail('a1b2c3d4');
  await flushAsync();
  assert.match(view.innerHTML, /Could not load Automations: database is down/);
  assert.match(view.innerHTML, /Back to Automations/);
  assert.equal(stateOf(sb).detail, null);
});

test('a failed Automaton list leaves the page rendering with short member ids', async () => {
  const view = makeStubElement();
  const a = automation();
  const sb = loadApp({ ['/api/automations/' + a.id]: { body: a } }, { view });
  sb.openAutomationDetail(a.id);
  await flushAsync();
  assert.match(view.innerHTML, /<h2 class="automation-detail-title">Release train<\/h2>/);
  assert.match(view.innerHTML, /<span class="automation-member-title">m1aaaaaa<\/span>/);
});

test('hostile values are escaped everywhere on the page', async () => {
  const evil = '<img src=x onerror=alert(1)>';
  const a = automation({
    name: evil,
    description: evil,
    status: 'blocked',
    decisions: [{ at: '2026-10-22T10:00:00Z', kind: '"><script>1</script>', actor: evil, note: evil }],
    pending_inserts: [{ id: '"><b>', prd_id: 'x', name: evil, actor: evil, reason: evil }],
    members: [member('m"x\'y<z', 'running', { prd_id: "p'\"<q", added_by: evil, added_reason: evil })],
    entry_member_id: 'm"x\'y<z',
    active_cursors: [],
  });
  const { html, sb } = await openDetail(a, { prds: [{ id: "p'\"<q", title: evil, status: 'running' }] });
  assert.ok(!html.includes('<img'), 'no raw tag from a hostile value');
  assert.ok(!html.includes('<script>'));
  assert.ok(!html.includes('<b>'));
  assert.match(html, /&lt;img src=x onerror=alert\(1\)&gt;/);
  assert.ok(!/onclick="[^"]*\('p'"/.test(html), 'the quote in a prd_id cannot end the handler string');

  const drawer = makeStubElement();
  sb.document.createElement = () => drawer;
  sb.openAutomationMemberDrawer('m"x\'y<z');
  assert.ok(!drawer.innerHTML.includes('<img'));
  assert.match(drawer.innerHTML, /&lt;img src=x onerror=alert\(1\)&gt;/);
});

test('refreshAutomationView reloads the open detail without the loading block, or the list when none is open', async () => {
  const a = automation();
  const view = makeStubElement();
  const sb = loadApp({
    ['/api/automations/' + a.id]: { body: a },
    '/api/automations': { body: [a] },
    '/api/autonomous/prds': { body: [] },
  }, { view });
  sb.openAutomationDetail(a.id);
  await flushAsync();
  const before = sb.__calls.filter(c => c.url === '/api/automations/' + a.id).length;
  view.innerHTML = 'marker';
  sb.refreshAutomationView();
  assert.equal(view.innerHTML, 'marker', 'a quiet refresh does not blank the page');
  await flushAsync();
  assert.equal(sb.__calls.filter(c => c.url === '/api/automations/' + a.id).length, before + 1);
  assert.match(view.innerHTML, /automation-detail-title/);

  stateOf(sb).detailId = null;
  sb.__calls.length = 0;
  sb.refreshAutomationView();
  await flushAsync();
  assert.ok(sb.__calls.some(c => c.url === '/api/automations'), 'the list is reloaded');
  assert.ok(!sb.__calls.some(c => c.url.startsWith('/api/automations/')));
});

test('a quiet reload that fails keeps the page that is showing', async () => {
  const a = automation();
  const view = makeStubElement();
  const routes = { ['/api/automations/' + a.id]: { body: a }, '/api/autonomous/prds': { body: [] } };
  const sb = loadApp(routes, { view });
  sb.openAutomationDetail(a.id);
  await flushAsync();
  const shown = view.innerHTML;
  delete routes['/api/automations/' + a.id];
  sb.refreshAutomationView();
  await flushAsync();
  assert.equal(view.innerHTML, shown);
});

test('the back control returns to the list and clears the open detail', async () => {
  const { sb } = await openDetail(automation());
  let listed = 0;
  sb.renderAutomationView = () => { listed++; };
  sb.automationDetailBack();
  assert.equal(listed, 1);
  assert.equal(stateOf(sb).detailId, null);
  assert.equal(stateOf(sb).detail, null);
});

test('an answer for an Automation the user already left is dropped', async () => {
  const view = makeStubElement();
  const a = automation();
  const sb = loadApp({ ['/api/automations/' + a.id]: { body: a }, '/api/autonomous/prds': { body: [] } }, { view });
  sb.openAutomationDetail(a.id);
  stateOf(sb).detailId = null;
  await flushAsync();
  assert.ok(!view.innerHTML.includes('automation-detail-title'));
});
