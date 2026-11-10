// Automata view — Events sub-tab row rendering and fetch behaviour.
//
// Loads the real app.js into the browser stub sandbox and checks that each
// event row shows time, kind chip, project, a labelled Automaton or Automation
// name, and summary; that a click expands detail and reason; and that the
// event-log fetch keeps one request in flight and retries once on failure.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function loadAppSandbox() {
  const sandbox = buildSandbox({ byId: {} });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

const AUTOMATON_EVENT = {
  id: 'ev-1',
  time: '2026-11-10T03:00:00Z',
  kind: 'watch',
  project: 'datawatch',
  automaton: 'prd-alpha',
  automation: '',
  summary: 'Watch finding on prd-alpha',
  detail: 'long detail text',
  reason: 'the reason text',
  next_action: 'check the lane',
};

const AUTOMATION_EVENT = {
  id: 'ev-2',
  time: '2026-11-10T04:00:00Z',
  kind: 'retry',
  project: 'datawatch',
  automaton: '',
  automation: 'auto-beta',
  summary: 'Retry scheduled',
  detail: '',
  reason: 'infra blip',
  next_action: '',
};

test('collapsed row shows time, kind chip, project, labelled owner and summary, but no detail', () => {
  const sandbox = loadAppSandbox();
  const html = vm.runInContext('automataEventRowHtml', sandbox)(AUTOMATON_EVENT, false);
  assert.match(html, /data-event-id="ev-1"/);
  assert.match(html, /automata-event-kind-watch/);
  assert.match(html, />watch</);
  assert.match(html, /Project: <strong>datawatch<\/strong>/);
  assert.match(html, /Automaton: <strong>prd-alpha<\/strong>/);
  assert.match(html, /Watch finding on prd-alpha/);
  assert.match(html, /aria-expanded="false"/);
  assert.doesNotMatch(html, /long detail text/);
  assert.doesNotMatch(html, /the reason text/);
});

test('expanded row shows detail, reason and next action', () => {
  const sandbox = loadAppSandbox();
  const html = vm.runInContext('automataEventRowHtml', sandbox)(AUTOMATON_EVENT, true);
  assert.match(html, /aria-expanded="true"/);
  assert.match(html, /long detail text/);
  assert.match(html, /the reason text/);
  assert.match(html, /check the lane/);
});

test('Automation rows are labelled Automation, never Automaton', () => {
  const sandbox = loadAppSandbox();
  const html = vm.runInContext('automataEventRowHtml', sandbox)(AUTOMATION_EVENT, false);
  assert.match(html, /Automation: <strong>auto-beta<\/strong>/);
  assert.doesNotMatch(html, /Automaton:/);
});

test('an event with both owners labels each one', () => {
  const sandbox = loadAppSandbox();
  const both = Object.assign({}, AUTOMATON_EVENT, { automation: 'auto-beta' });
  const owners = JSON.parse(JSON.stringify(vm.runInContext('automataEventOwners', sandbox)(both)));
  assert.deepEqual(owners.map(o => [o.label, o.name]), [['Automation', 'auto-beta'], ['Automaton', 'prd-alpha']]);
});

test('event text is HTML-escaped', () => {
  const sandbox = loadAppSandbox();
  const evil = Object.assign({}, AUTOMATON_EVENT, { summary: '<img src=x onerror=alert(1)>' });
  const html = vm.runInContext('automataEventRowHtml', sandbox)(evil, false);
  assert.doesNotMatch(html, /<img src=x/);
  assert.match(html, /&lt;img src=x/);
});

test('fetch keeps one request in flight and returns the same promise to a second caller', async () => {
  const sandbox = loadAppSandbox();
  let calls = 0;
  let release;
  sandbox.apiFetch = () => { calls++; return new Promise(r => { release = r; }); };
  const first = vm.runInContext('automataEventsFetch()', sandbox);
  const second = vm.runInContext('automataEventsFetch()', sandbox);
  assert.equal(first, second);
  release({ events: [AUTOMATON_EVENT] });
  const events = await first;
  assert.equal(calls, 1);
  assert.equal(events.length, 1);
  assert.equal(events[0].id, 'ev-1');
});

test('fetch retries once after a failure and then succeeds', async () => {
  const sandbox = loadAppSandbox();
  let calls = 0;
  sandbox.apiFetch = async () => {
    calls++;
    if (calls === 1) throw new Error('network down');
    return { events: [AUTOMATION_EVENT] };
  };
  const events = await vm.runInContext('automataEventsFetch()', sandbox);
  assert.equal(calls, 2);
  assert.equal(events[0].id, 'ev-2');
});

test('fetch rejects after two failed attempts and requests the event-log route', async () => {
  const sandbox = loadAppSandbox();
  const urls = [];
  sandbox.apiFetch = async (url) => { urls.push(url); throw new Error('boom'); };
  await assert.rejects(vm.runInContext('automataEventsFetch()', sandbox), /boom/);
  assert.equal(urls.length, 2);
  assert.ok(urls.every(u => u.startsWith('/api/autonomous/event-log?limit=')));
});

test('a later fetch is allowed once the previous one settled', async () => {
  const sandbox = loadAppSandbox();
  let calls = 0;
  sandbox.apiFetch = async () => { calls++; return { events: [] }; };
  await vm.runInContext('automataEventsFetch()', sandbox);
  await vm.runInContext('automataEventsFetch()', sandbox);
  assert.equal(calls, 2);
});
