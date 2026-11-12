// listLoader: loading, cached, failed + retry, timeout, superseded, stale,
// parallel isolation.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function setup() {
  const sandbox = buildSandbox();
  // Controllable clock + timers.
  const clock = { t: 1000000, timers: [], id: 0 };
  sandbox.setTimeout = (fn, ms) => { const id = ++clock.id; clock.timers.push({ id, fn, at: clock.t + ms }); return id; };
  sandbox.clearTimeout = (id) => { clock.timers = clock.timers.filter(x => x.id !== id); };
  sandbox.setInterval = () => 0;
  sandbox.clearInterval = () => {};
  sandbox.AbortController = AbortController;
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n = sandbox._i18n || {};
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales/en.json'), 'utf8'));
  const advance = (ms) => {
    clock.t += ms;
    for (const x of clock.timers.filter(x => x.at <= clock.t)) { clock.timers = clock.timers.filter(y => y !== x); x.fn(); }
  };
  const el = () => ({ innerHTML: '' });
  const make = (o) => sandbox.listLoader(Object.assign({ render: d => '<ul>' + d.join(',') + '</ul>', now: () => clock.t }, o));
  return { sandbox, clock, advance, el, make };
}
const defer = () => { let res, rej; const p = new Promise((a, b) => { res = a; rej = b; }); return { p, res, rej }; };
const tick = () => new Promise(r => setImmediate(r));

test('loading visible immediately, before the request resolves', () => {
  const { el, make } = setup();
  const e = el(); const d = defer();
  make({ key: 'a1', el: e, url: '/x', fetch: () => d.p }).load();
  assert.match(e.innerHTML, /list-state-skeleton/);
  assert.match(e.innerHTML, /Loading/);
});

test('cached data renders at once with cached badge and age', async () => {
  const { sandbox, clock, el, make } = setup();
  sandbox.localStorage.setItem('cs_list_c1', JSON.stringify({ t: clock.t - 5000, data: ['old'] }));
  const e = el(); const d = defer();
  const l = make({ key: 'c1', el: e, url: '/x', pollMs: 1000, fetch: () => d.p });
  l.load();
  assert.match(e.innerHTML, /list-state-cached/);
  assert.match(e.innerHTML, /old/);
  assert.match(e.innerHTML, /5 s ago/);
  assert.doesNotMatch(e.innerHTML, /list-state-stale/, 'no stale while first load in flight');
  d.res(['new']); await tick(); await tick();
  assert.match(e.innerHTML, /new/);
  assert.doesNotMatch(e.innerHTML, /list-state-cached/);
  assert.ok(sandbox.localStorage.getItem('cs_list_c1').includes('new'));
});

test('oversized payload is never cached; broken storage does not throw', async () => {
  const { sandbox, el, make } = setup();
  const big = ['x'.repeat(600 * 1024)];
  await make({ key: 'big', el: el(), url: '/x', render: () => '', fetch: () => Promise.resolve(big) }).load();
  assert.equal(sandbox.localStorage.getItem('cs_list_big'), null);
  sandbox.localStorage.setItem = () => { throw new Error('quota'); };
  sandbox.localStorage.getItem = () => { throw new Error('denied'); };
  await make({ key: 'ok', el: el(), url: '/x', fetch: () => Promise.resolve(['a']) }).load();
});

test('failure shows failed banner + Retry, keeps cached data, retry recovers', async () => {
  const { sandbox, clock, el, make } = setup();
  sandbox.localStorage.setItem('cs_list_f1', JSON.stringify({ t: clock.t, data: ['kept'] }));
  const e = el(); let fail = true;
  const l = make({ key: 'f1', el: e, url: '/x', fetch: () => fail ? Promise.reject(new Error('boom')) : Promise.resolve(['fresh']) });
  await l.load();
  assert.match(e.innerHTML, /list-state-failed/);
  assert.match(e.innerHTML, /boom/);
  assert.match(e.innerHTML, /Retry/);
  assert.match(e.innerHTML, /kept/);
  fail = false;
  await l.retry();
  assert.doesNotMatch(e.innerHTML, /list-state-failed/);
  assert.match(e.innerHTML, /fresh/);
});

test('timeout aborts the request and shows failed state', async () => {
  const { sandbox, advance, el, make } = setup();
  const e = el(); let sig;
  const l = make({ key: 't1', el: e, url: '/x', timeoutMs: 15000, fetch: (u, o) => { sig = o.signal; return new Promise(() => {}); } });
  const p = l.load();
  advance(15001);
  await p;
  assert.match(e.innerHTML, /list-state-failed/);
  assert.match(e.innerHTML, /timeout/);
  assert.equal(sig.aborted, true);
});

test('superseded response is ignored and its request aborted', async () => {
  const { el, make } = setup();
  const e = el(); const d1 = defer(), d2 = defer(); const sigs = []; let n = 0;
  const l = make({ key: 's1', el: e, url: '/x', fetch: (u, o) => { sigs.push(o.signal); return (++n === 1 ? d1 : d2).p; } });
  const p1 = l.load(); const p2 = l.load();
  assert.equal(sigs[0].aborted, true);
  d2.res(['newer']); await p2;
  d1.res(['older']); await p1;
  assert.match(e.innerHTML, /newer/);
  assert.doesNotMatch(e.innerHTML, /older/);
});

test('stale warning only after the polling interval', async () => {
  const { clock, advance, el, make } = setup();
  const e = el();
  const l = make({ key: 'st1', el: e, url: '/x', pollMs: 30000, fetch: () => Promise.resolve(['a']) });
  await l.load();
  assert.equal(l.isStale(), false);
  clock.t += 29000; assert.equal(l.isStale(), false);
  clock.t += 2000; assert.equal(l.isStale(), true);
  // a new in-flight load suppresses the warning
  l.state.loading = true; assert.equal(l.isStale(), false);
});

test('parallel loaders are isolated: one failure does not blank the others', async () => {
  const { sandbox, el, make } = setup();
  const ea = el(), eb = el();
  const a = make({ key: 'pa', el: ea, url: '/a', fetch: () => Promise.resolve(['A']) });
  const b = make({ key: 'pb', el: eb, url: '/b', fetch: () => Promise.reject(new Error('down')) });
  await sandbox.listLoaderAll([a, b]);
  assert.match(ea.innerHTML, /A/);
  assert.doesNotMatch(ea.innerHTML, /list-state-failed/);
  assert.match(eb.innerHTML, /list-state-failed/);
});
