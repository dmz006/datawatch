// Automation tab — pending-insert parity with the CLI.
//
// The CLI prints each held insert's member id, PRD id, reason and approve
// command, and the daemon's loop status counts held inserts in pending_members.
// The PWA must agree: the live status row words "waiting: N" from that same
// count, and each pending insert row on the detail page shows the member id
// and the PRD id as visible (escaped) text. fetch is stubbed; rendered text
// is the real English bundle.

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

function automation(extra) {
  return Object.assign({
    id: 'a1b2c3d4', name: 'Release train', description: '', status: 'active', version: 7,
    entry_member_id: 'm1', active_cursors: [], members: [],
  }, extra || {});
}

// openDetail loads one Automation into a recording #view and resolves the html.
async function openDetail(a) {
  const view = makeStubElement();
  const routes = {
    ['/api/automations/' + encodeURIComponent(a.id)]: { body: a },
    '/api/autonomous/prds': { body: { prds: [] } },
  };
  const sb = loadApp(routes, { view, automationStrip: makeStubElement() });
  sb.openAutomationDetail(a.id);
  await flushAsync();
  return { sb, view, html: view.innerHTML };
}

test('the status row words "waiting: 3" when pending_members is 3 (held inserts are counted by the daemon)', () => {
  const sb = loadApp({}, {});
  const line = sb.automationStatusLine({ id: 'a1', current_member_id: 'm1', pending_members: 3, idle_seconds: 0 });
  assert.ok(line, 'the row is worded');
  assert.match(line, /waiting: 3/);
  assert.ok(!/waiting: 2/.test(line));
  // The daemon-side count includes held inserts, so the PWA renders whatever
  // pending_members says verbatim.
  assert.equal(
    sb.automationStatusLine({ current_prd_title: 'Ship it', pending_members: 3, idle_seconds: 600 }),
    'Now: Ship it · waiting: 3 · idle: 10m');
});

test('a pending insert row shows the member id, the PRD id, the reason and an Approve control', async () => {
  const a = automation({
    pending_inserts: [{ id: 'mem-77', prd_id: 'abcd1234abcd12', name: 'Hotfix', actor: 'agent:7', reason: 'tests found a gap', status: 'pending' }],
  });
  const { html } = await openDetail(a);
  assert.match(html, /<li class="automation-pending-insert" data-insert-id="mem-77">/);
  // Member id and PRD id are visible text, agreed with the CLI output.
  assert.match(html, /<span class="automation-pending-insert-ids">Member mem-77 · Automaton abcd1234abcd12<\/span>/);
  assert.match(html, /tests found a gap/);
  assert.match(html, /Requested by agent:7/);
  // The Approve control names the member id, like the CLI's approve command.
  assert.match(html, /<button type="button" class="btn-secondary automation-approve-insert" data-insert-id="mem-77" onclick="automationApproveInsert\('mem-77'\)">Approve insert<\/button>/);
});

test('hostile text in the pending insert ids, name, actor and reason is escaped', async () => {
  const evil = '<img src=x onerror=alert(1)>';
  const quote = '<b id="q">';
  const a = automation({
    pending_inserts: [{ id: evil, prd_id: quote, name: evil, actor: evil, reason: evil, status: 'pending' }],
  });
  const { html } = await openDetail(a);
  assert.ok(!html.includes('<img'), 'no raw tag from a hostile value');
  assert.ok(!html.includes('<b id='), 'the prd_id cannot open a raw element');
  assert.match(html, /Member &lt;img src=x onerror=alert\(1\)&gt; · Automaton &lt;b id=&quot;q&quot;&gt;/);
  assert.match(html, /&lt;img src=x onerror=alert\(1\)&gt;/);
});
