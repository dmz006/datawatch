// Automata view — Events sub-tab filters: kind chips, project dropdown, debounced
// search and Load more.
//
// Loads the real app.js into the browser stub sandbox. Checks that the chips
// send kind= as a comma list in canonical order, the dropdown sends project=,
// the search box sends q= once typing pauses, and Load more follows next_cursor
// with before=. apiFetch and setTimeout are replaced so every request is seen.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const KINDS = ['watch', 'decision', 'retry', 'check', 'cleanup', 'alert', 'error'];

// loadAppSandbox loads app.js with apiFetch recording every URL. Each response
// is taken from the queue in `pages`; an empty queue returns an empty page.
function loadAppSandbox(pages = []) {
  const sandbox = buildSandbox({ byId: {} });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const urls = [];
  sandbox.apiFetch = async (url) => {
    urls.push(url);
    return pages.length ? pages.shift() : { events: [], next_cursor: '', total: 0 };
  };
  // Capture timers so the debounce can be driven by hand.
  const timers = [];
  sandbox.setTimeout = (fn, ms) => { timers.push({ fn, ms }); return timers.length; };
  sandbox.clearTimeout = (id) => { if (timers[id - 1]) timers[id - 1].cleared = true; };
  return { sandbox, urls, timers };
}

function stateOf(sandbox) {
  return vm.runInContext("_automataEventsState", sandbox);
}

function run(sandbox, code, ...args) {
  return vm.runInContext(code, sandbox)(...args);
}

const EVENT = (id, project, kind = 'watch') => ({
  id, time: '2026-11-10T03:00:00Z', kind, project,
  automaton: 'prd-alpha', automation: '', summary: 'summary ' + id,
  detail: '', reason: '', next_action: '',
});

test('query carries limit first and no filter params by default', () => {
  const { sandbox } = loadAppSandbox();
  assert.equal(run(sandbox, 'automataEventsQuery'), '/api/autonomous/event-log?limit=50');
});

test('kind chips are sent as one comma list in canonical chip order', () => {
  const { sandbox } = loadAppSandbox();
  stateOf(sandbox).kinds.add('error');
  stateOf(sandbox).kinds.add('watch');
  assert.equal(run(sandbox, 'automataEventsQuery'), '/api/autonomous/event-log?limit=50&kind=watch%2Cerror');
});

test('project, search and cursor are encoded into their own params', () => {
  const { sandbox } = loadAppSandbox();
  const st = stateOf(sandbox);
  st.project = 'data watch';
  st.q = 'a&b';
  const url = run(sandbox, 'automataEventsQuery', 'cur+1');
  assert.equal(url, '/api/autonomous/event-log?limit=50&project=data%20watch&q=a%26b&before=cur%2B1');
});

test('toggling a kind chip on then off reloads page one with and without kind=', async () => {
  const { sandbox, urls } = loadAppSandbox();
  run(sandbox, 'toggleAutomataEventKind', 'retry');
  await flushAsync();
  assert.ok(urls[urls.length - 1].includes('kind=retry'), urls[urls.length - 1]);
  run(sandbox, 'toggleAutomataEventKind', 'retry');
  await flushAsync();
  assert.ok(!urls[urls.length - 1].includes('kind='), urls[urls.length - 1]);
  assert.equal(urls.length, 2);
});

test('an unknown kind is ignored and sends no request', async () => {
  const { sandbox, urls } = loadAppSandbox();
  run(sandbox, 'toggleAutomataEventKind', 'bogus');
  await flushAsync();
  assert.equal(urls.length, 0);
  assert.equal(stateOf(sandbox).kinds.size, 0);
});

test('the project dropdown sends project= and selecting All projects clears it', async () => {
  const { sandbox, urls } = loadAppSandbox();
  run(sandbox, 'setAutomataEventsProject', 'datawatch');
  await flushAsync();
  assert.ok(urls[urls.length - 1].includes('project=datawatch'), urls[urls.length - 1]);
  run(sandbox, 'setAutomataEventsProject', '');
  await flushAsync();
  assert.ok(!urls[urls.length - 1].includes('project='), urls[urls.length - 1]);
});

test('the project dropdown offers every project seen in loaded events', async () => {
  const { sandbox } = loadAppSandbox([{ events: [EVENT('1', 'zeta'), EVENT('2', 'alpha')], next_cursor: '' }]);
  run(sandbox, 'automataEventsReload');
  await flushAsync();
  const opts = run(sandbox, 'automataEventsProjectOptions');
  assert.match(opts, /<option value=""[^>]*>All projects<\/option>/);
  assert.ok(opts.indexOf('value="alpha"') < opts.indexOf('value="zeta"'));
});

test('search input is debounced: several keystrokes send one q= request', async () => {
  const { sandbox, urls, timers } = loadAppSandbox();
  run(sandbox, 'scheduleAutomataEventsSearch', 'b');
  run(sandbox, 'scheduleAutomataEventsSearch', 'bu');
  run(sandbox, 'scheduleAutomataEventsSearch', 'bug');
  const live = timers.filter(t => !t.cleared);
  assert.equal(live.length, 1, 'only the last timer is live');
  assert.equal(live[0].ms, 300);
  assert.equal(urls.length, 0, 'nothing sent while typing');
  live[0].fn();
  await flushAsync();
  assert.equal(urls.length, 1);
  assert.ok(urls[0].includes('q=bug'), urls[0]);
});

test('search with unchanged text after trimming sends nothing new', async () => {
  const { sandbox, urls, timers } = loadAppSandbox();
  run(sandbox, 'scheduleAutomataEventsSearch', '  ');
  timers.filter(t => !t.cleared).forEach(t => t.fn());
  await flushAsync();
  assert.equal(urls.length, 0);
});

test('Load more sends before= from next_cursor and appends the next page', async () => {
  const { sandbox, urls } = loadAppSandbox([
    { events: [EVENT('1', 'datawatch')], next_cursor: 'cursor-A', total: 2 },
    { events: [EVENT('2', 'datawatch')], next_cursor: '', total: 2 },
  ]);
  run(sandbox, 'automataEventsReload');
  await flushAsync();
  assert.equal(stateOf(sandbox).nextCursor, 'cursor-A');
  run(sandbox, 'automataEventsLoadMore');
  await flushAsync();
  assert.ok(urls[1].includes('before=cursor-A'), urls[1]);
  assert.deepEqual(stateOf(sandbox).events.map(e => e.id), ['1', '2']);
  assert.equal(stateOf(sandbox).nextCursor, '');
});

test('Load more sends nothing when there is no next_cursor', async () => {
  const { sandbox, urls } = loadAppSandbox();
  run(sandbox, 'automataEventsLoadMore');
  await flushAsync();
  assert.equal(urls.length, 0);
});

test('a filter change during Load more drops the older page', async () => {
  const { sandbox, urls } = loadAppSandbox([
    { events: [EVENT('1', 'datawatch')], next_cursor: 'cursor-A', total: 2 },
  ]);
  run(sandbox, 'automataEventsReload');
  await flushAsync();
  // Hold every request open so the two responses can be settled by hand.
  const pending = [];
  sandbox.apiFetch = (url) => { urls.push(url); return new Promise(r => pending.push(r)); };
  run(sandbox, 'automataEventsLoadMore');   // pending[0]: page after cursor-A
  run(sandbox, 'toggleAutomataEventKind', 'alert'); // pending[1]: fresh page one
  pending[1]({ events: [EVENT('fresh', 'datawatch', 'alert')], next_cursor: '', total: 1 });
  await flushAsync();
  pending[0]({ events: [EVENT('stale', 'datawatch')], next_cursor: 'stale-cursor', total: 1 });
  await flushAsync();
  assert.deepEqual(stateOf(sandbox).events.map(e => e.id), ['fresh']);
  assert.equal(stateOf(sandbox).nextCursor, '');
});

test('filter bar renders the seven kind chips in order with aria-pressed state', () => {
  const { sandbox } = loadAppSandbox();
  stateOf(sandbox).kinds.add('check');
  const html = run(sandbox, 'automataEventsFilterHtml');
  const chips = [...html.matchAll(/data-events-kind="([a-z]+)" aria-pressed="(true|false)"/g)];
  assert.deepEqual(chips.map(m => m[1]), KINDS);
  assert.deepEqual(chips.filter(m => m[2] === 'true').map(m => m[1]), ['check']);
});

test('filter bar escapes the search value and labels the controls', () => {
  const { sandbox } = loadAppSandbox();
  stateOf(sandbox).q = '<img src=x onerror=1>';
  const html = run(sandbox, 'automataEventsFilterHtml');
  assert.doesNotMatch(html, /<img src=x/);
  assert.match(html, /id="automataEventsSearch"/);
  assert.match(html, /id="automataEventsProject"/);
});
