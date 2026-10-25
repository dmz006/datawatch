'use strict';
// Holder nodes on the capacity card — PWA side.
//
// A pool whose holders sit on two or more nodes names the node beside each
// holder (a muted span after the short id). A one-node entry, a lease with no
// node and an older body without hold_nodes render exactly as before.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-capacity-holder-nodes.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const enBundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = enBundle;
  return sandbox;
}

function render(s, cap, opts) {
  s._cap = cap;
  s._opts = opts;
  return vm.runInContext('window.capacityCardHtml(_cap, _opts)', s);
}

const H1 = 'abcdef1234567890';
const H2 = '12345678ffff0000';
const H3 = 'feedc0de00001111';
const CHIP = 'class="cap-node" style="font-size:10px;color:var(--text2);"';

// A pool as the daemon sends it: the raw holder list in holder (comma string)
// and holders (array), with hold_nodes parallel to holders when given.
function pool(name, limit, held, ids, nodes) {
  const p = { name, limit, held, external: 0, holder: ids.join(','), holders: ids };
  if (nodes) p.hold_nodes = nodes;
  return p;
}
function card(s, pools) {
  return render(s, { pools, leases: [], waiting: [], backoff: [], budgets: [] });
}

test('holders on two nodes each show their node beside the short id', () => {
  const s = loadApp();
  const html = card(s, [pool('llm:pi', 4, 2, [H1, H2], ['gpu-a', 'gpu-b'])]);
  assert.ok(html.includes(`abcdef12 <span ${CHIP}>gpu-a</span>`), 'gpu-a beside first holder: ' + html);
  assert.ok(html.includes(`12345678 <span ${CHIP}>gpu-b</span>`), 'gpu-b beside second holder: ' + html);
});

test('a one-node entry renders exactly as it does without hold_nodes', () => {
  const s = loadApp();
  const withNodes = card(s, [pool('node:gpu-a', 2, 2, [H1, H2], ['gpu-a', 'gpu-a'])]);
  const without = card(s, [pool('node:gpu-a', 2, 2, [H1, H2])]);
  assert.ok(!withNodes.includes('cap-node'), 'one-node entry must not name the node: ' + withNodes);
  assert.equal(withNodes, without, 'one-node entry must render as before');
});

test('a lease with no node renders as before, and a mixed pool names only the holders that have one', () => {
  const s = loadApp();
  const bare = card(s, [pool('llm:pi', 4, 2, [H1, H2], ['', ''])]);
  assert.ok(!bare.includes('cap-node'), 'no-node leases must not name a node: ' + bare);
  assert.ok(bare.includes('Holders: abcdef12, 12345678'), 'raw holder list unchanged: ' + bare);

  const mixed = card(s, [pool('llm:pi', 4, 3, [H1, H2, H3], ['gpu-a', '', 'gpu-b'])]);
  assert.ok(mixed.includes(`abcdef12 <span ${CHIP}>gpu-a</span>`), 'gpu-a beside first: ' + mixed);
  assert.ok(mixed.includes(`feedc0de <span ${CHIP}>gpu-b</span>`), 'gpu-b beside third: ' + mixed);
  assert.equal(mixed.split('cap-node').length - 1, 2, 'only the two nodes are named: ' + mixed);
  assert.ok(mixed.includes('Holders: abcdef12 <span'), 'first holder carries its node: ' + mixed);
  assert.ok(mixed.includes(', 12345678, feedc0de <span'), 'bare holder stays bare: ' + mixed);
});

test('an older body without the holders array shows no node', () => {
  const s = loadApp();
  const html = card(s, [{ name: 'node:gpu-a', limit: 2, held: 2, external: 0, holder: H1 + ',' + H2, hold_nodes: ['gpu-a', 'gpu-b'] }]);
  assert.ok(!html.includes('cap-node'), 'without holders[] there is no safe pairing, so no node: ' + html);
  assert.ok(html.includes('Holders: abcdef12, 12345678'), 'raw holder ids still render: ' + html);
});

test('the node name is HTML-escaped', () => {
  const s = loadApp();
  const html = card(s, [pool('llm:pi', 4, 2, [H1, H2], ['<img src=x onerror=1>', 'gpu-b'])]);
  assert.ok(!html.includes('<img'), 'unescaped node name leaked: ' + html);
  assert.ok(html.includes('&lt;img src=x onerror=1&gt;'), 'node name must be escaped: ' + html);
});
