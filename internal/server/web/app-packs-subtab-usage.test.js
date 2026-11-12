// Pins the Packs sub-tab usage pieces: the per-source auto-accept switch and
// the per-pack usage table read from the usage endpoint.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function fresh(respond) {
  const sandbox = buildSandbox({});
  const calls = [];
  sandbox.apiFetch = (url, opts) => { calls.push({ url, opts }); return respond ? respond(url) : Promise.resolve({ ok: true }); };
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app-packs-subtab-usage.js'), sandbox);
  return { sb: sandbox, calls };
}
const plain = v => JSON.parse(JSON.stringify(v));

test('auto-accept switch is off by default, checked when on, and escapes the name', () => {
  const { sb } = fresh();
  assert.equal(sb.packsAutoAcceptHTML({}, 0), '');
  const off = sb.packsAutoAcceptHTML({ name: 'src<b>' }, 3);
  assert.doesNotMatch(off, /switch" checked/);
  assert.match(off, /packsSubtabSetAutoAccept\(3,this\.checked\)/);
  assert.match(off, /src&lt;b&gt;/);
  assert.match(sb.packsAutoAcceptHTML({ name: 's', auto_accept_packs: true }, 0), /switch" checked/);
});

test('saving the switch PUTs auto_accept_packs to the source', async () => {
  const { sb, calls } = fresh();
  await sb.packsSetAutoAccept({ name: 'my src' }, true);
  assert.equal(calls[0].url, '/api/skills/registries/my%20src');
  assert.equal(calls[0].opts.method, 'PUT');
  assert.deepEqual(JSON.parse(calls[0].opts.body), { auto_accept_packs: true });
  assert.equal(await sb.packsSetAutoAccept(null, true), null);
  assert.equal(calls.length, 1);
});

test('usage rows accept an array or {usage}, drop unnamed rows, and sort', () => {
  const { sb } = fresh();
  const rows = [{ pack_name: 'b', pack_version: '1' }, { pack_version: '9' }, { pack_name: 'a', pack_version: '2' }, { pack_name: 'a', pack_version: '1' }];
  assert.deepEqual(plain(sb.packsUsageRows(rows)).map(r => r.pack_name + r.pack_version), ['a1', 'a2', 'b1']);
  assert.equal(sb.packsUsageRows({ usage: rows }).length, 3);
  assert.deepEqual(plain(sb.packsUsageRows(null)), []);
});

test('usage table shows runs, cost and last use, escaped; empty state otherwise', () => {
  const { sb } = fresh();
  assert.match(sb.packsUsageTableHTML([]), /No pack has been used yet/);
  const html = sb.packsUsageTableHTML([{ pack_name: 'al<i>', pack_version: '1.0.0', runs: 3, cost: 1.5, last_used: '2026-11-12T03:04:05Z' }]);
  assert.match(html, /al&lt;i&gt;/);
  assert.match(html, /<td>3<\/td><td>\$1\.50<\/td><td>2026-11-12 03:04<\/td>/);
  assert.match(sb.packsUsageTableHTML([{ pack_name: 'x', last_used: '0001-01-01T00:00:00Z' }]), /<td>\$0\.00<\/td><td><\/td>/);
});

test('usage load reads the endpoint and turns a failure into an error line', async () => {
  const ok = fresh(() => Promise.resolve({ usage: [{ pack_name: 'p', runs: 1 }] }));
  assert.match(await ok.sb.packsUsageLoad(), /packs-usage-table/);
  assert.equal(ok.calls[0].url, '/api/packs/usage');
  const bad = fresh(() => Promise.reject(new Error('boom')));
  assert.match(await bad.sb.packsUsageLoad(), /packs-usage-error[^>]*>boom/);
});
