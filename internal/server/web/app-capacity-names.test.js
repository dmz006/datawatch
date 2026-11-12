'use strict';
// Readable capacity names — PWA side.
//
// Capacity holders, waiting tasks and PRD cells must show the
// operator-readable name first ("<PRD title> / <task title>") with the short
// id in a smaller secondary span; long names middle-truncate inside a
// min-width:0 / overflow:hidden / text-overflow:ellipsis clamp so nothing
// overflows a 360px card, with the full text in an escaped title tooltip.
// Older daemon bodies without name resolution fall back to the raw id
// exactly as before, and every dynamic value is HTML-escaped.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-capacity-names.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const enBundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];

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
function wait(s, task) {
  s._task = task;
  return vm.runInContext('window.taskWaitHtml(_task)', s);
}

const NOW = Date.parse('2026-10-22T12:00:00Z');
const LONG_NAME = 'A'.repeat(20) + ' middle part ' + 'B'.repeat(25); // 50 chars

test('a long readable holder name is middle-truncated with the full text in an escaped tooltip', () => {
  const s = loadApp();
  const html = render(s, {
    pools: [{ name: 'host', limit: 2, held: 1, external: 0, holder: 'rawholder99', holder_names: [{ readable: LONG_NAME, short_id: 'abcd1234' }] }],
    leases: [], waiting: [], backoff: [], budgets: [],
  }, { now: NOW });
  // Middle truncation: 3/4 of the budget at the head, the rest at the tail, ellipsis between.
  const head = LONG_NAME.slice(0, 30);
  const tail = LONG_NAME.slice(-9);
  assert.ok(html.includes(head + '\u2026' + tail), 'expected middle-truncated label, got: ' + html);
  const body = html.replace(/ title="[^"]*"/g, ''); // tooltip may carry the full text
  assert.ok(!body.includes(LONG_NAME), 'un-truncated full name must not appear as body text: ' + body);
  // The full text stays available, escaped, in the title tooltip.
  assert.ok(html.includes(`title="${LONG_NAME}"`), 'full name must be in a title tooltip: ' + html);
  // Short id as a smaller secondary span, after the name.
  assert.ok(html.includes('abcd1234'), 'short id missing');
  assert.ok(html.indexOf(head) < html.indexOf('abcd1234'), 'readable name must come before the short id');
  assert.ok(html.includes('font-size:10px'), 'secondary short id must be the smaller span');
});

test('name cells carry min-width:0 / overflow clamp so they fit a 360px card', () => {
  const s = loadApp();
  const html = render(s, {
    pools: [{ name: 'host', limit: 1, held: 1, external: 0, holder: 'h1', holder_names: [{ readable: LONG_NAME, short_id: 'abcd1234' }] }],
    waiting: [{ holder: 'feedbeef1234', holder_name: { readable: LONG_NAME, short_id: 'abcd1234', prd_id: '1234567890abcdef', prd_title: LONG_NAME }, since: NOW - 5000, reason: 'pool full' }],
    leases: [], backoff: [], budgets: [],
  }, { now: NOW });
  const spans = [...html.matchAll(/<span class="cap-name"([^>]*)>/g)].map(m => m[1]);
  assert.ok(spans.length >= 2, 'no cap-name spans rendered: ' + html);
  for (const style of spans) {
    assert.ok(style.includes('min-width:0'), 'cap-name missing min-width:0: ' + style);
    assert.ok(style.includes('overflow:hidden'), 'cap-name missing overflow:hidden: ' + style);
    assert.ok(style.includes('text-overflow:ellipsis'), 'cap-name missing text-overflow:ellipsis: ' + style);
  }
  assert.ok(/<div class="prd-capacity-wait"[^>]*min-width:0/.test(html), 'waiting row must clamp for narrow cards: ' + html);
  assert.ok(/class="cap-holders"[^>]*min-width:0/.test(html), 'holder line must clamp for narrow cards: ' + html);
});

test('a resolved holder with no title renders as "(untitled <short id>)"', () => {
  const s = loadApp();
  const html = render(s, {
    pools: [{ name: 'host', limit: 1, held: 1, external: 0, holder: 'task-1234abcd', holder_names: [{ id: 'task-1234abcd', short_id: 'abcd1234' }] }],
    leases: [], waiting: [], backoff: [], budgets: [],
  });
  const label = enBundle.capacity_name_untitled.replace('{id}', 'abcd1234');
  assert.equal(label, '(untitled abcd1234)');
  assert.ok(html.includes(label), 'untitled label missing: ' + html);
  // And a waiting row whose PRD itself has no title falls back to 'PRD <short id>'.
  const w = render(s, {
    pools: [], leases: [], backoff: [], budgets: [],
    waiting: [{ holder: 'feedc0de1234', holder_name: { id: 'feedc0de1234', short_id: 'c0defeed' }, prd_id: '1234567890abcdef', since: NOW - 4000 }],
  }, { now: NOW });
  assert.ok(w.includes(enBundle.capacity_name_untitled.replace('{id}', 'c0defeed')), 'waiting row untitled label missing: ' + w);
  assert.ok(w.includes(enBundle.capacity_name_prd + ' '), 'PRD label missing: ' + w);
  assert.ok(w.includes('12345678'), 'PRD short id missing: ' + w);
});

test('an older body without name resolution falls back to the raw id exactly as before', () => {
  const s = loadApp();
  const w = render(s, {
    pools: [], leases: [], backoff: [], budgets: [],
    // Old shape: raw ids only, no holder_name / prd_title fields at all.
    waiting: [{ holder: 'abcdef1234567890', prd_id: '1234567890abcdef', since: NOW - 5000, reason: 'pool full' }],
  }, { now: NOW });
  assert.ok(w.includes('abcdef12'), 'raw holder short id missing: ' + w);
  assert.ok(w.includes(enBundle.capacity_name_prd + ' '), 'PRD label missing: ' + w);
  assert.ok(w.includes('12345678'), 'raw PRD short id missing: ' + w);
  assert.ok(w.includes('pool full'), 'reason missing: ' + w);
  assert.ok(!w.includes(untitledLabel()), 'old body must not show the untitled label: ' + w);
  assert.ok(!w.includes('class="cap-name"'), 'old body must render no cap-name spans: ' + w);
  // Pool holder lists keep their raw ids when the body sends no holder_names.
  const p = render(s, {
    pools: [{ name: 'node:gpu1', limit: 2, held: 2, external: 0, holder: 'abcdef1234567890,12345678ffff' }],
    leases: [], waiting: [], backoff: [], budgets: [],
  });
  assert.ok(p.includes('Holders: abcdef12, 12345678'), 'raw pool holder ids missing: ' + p);
  // An even older body with just a plain hold-name string still names it.
  const w2 = render(s, {
    pools: [], leases: [], backoff: [], budgets: [],
    waiting: [{ holder: 'h1234567890', hold_name: 'Old Daemon / some task', since: NOW - 5000 }],
  }, { now: NOW });
  assert.ok(w2.includes('Old Daemon / some task'), 'hold_name string missing: ' + w2);
});
function untitledLabel() { return enBundle.capacity_name_untitled.replace('{id}', 'abcdef12'); }

test('readable names, PRD titles and reasons are HTML-escaped', () => {
  const s = loadApp();
  const evilName = '<img src=x onerror=1>';
  const evilPrd = '<b>PRD</b>';
  const evilReason = '<script>alert(1)</script>';
  const html = render(s, {
    pools: [{ name: 'host', limit: 1, held: 1, external: 0, holder: 'h1', holder_names: [{ readable: evilName, short_id: 'abcd1234' }] }],
    waiting: [{ holder: 'w1', holder_name: { readable: evilName, short_id: 'c0defeed' }, prd_id: '<b>p</b>', prd_title: evilPrd, since: NOW - 5000, reason: evilReason }],
    leases: [], backoff: [], budgets: [],
  }, { now: NOW });
  for (const raw of ['<img', '<b>PRD', '<script>', 'onerror=1"']) {
    assert.ok(!html.includes(raw), 'unescaped ' + raw + ' leaked: ' + html);
  }
  assert.ok(html.includes('&lt;img src=x onerror=1&gt;'), 'holder name must be escaped: ' + html);
  assert.ok(html.includes(`title="${evilName.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')}"`), 'name tooltip must be escaped');
  assert.ok(html.includes('&lt;b&gt;PRD&lt;/b&gt;'), 'PRD title must be escaped: ' + html);
  assert.ok(html.includes('&lt;script&gt;alert(1)&lt;/script&gt;'), 'reason must be escaped: ' + html);
});

test('a waiting row shows the holder name, PRD title and readable reason before the raw text', () => {
  const s = loadApp();
  const w = render(s, {
    pools: [], leases: [], backoff: [], budgets: [],
    waiting: [{
      holder: 'feedbeef1234567890',
      holder_name: { readable: 'Ship billing / Fix tax rounding', short_id: 'c0defeed' },
      prd_id: 'ab12cd34ef56',
      prd_title: 'Ship billing',
      since: NOW - 65000,
      reason: 'held by 3f2a9c10 <raw>',
      read_reason: 'held by Ship billing / Refund edge cases (3f2a9c10)',
    }],
  }, { now: NOW });
  assert.ok(w.includes('Ship billing / Fix tax rounding'), 'holder name missing: ' + w);
  assert.ok(w.includes('held by Ship billing / Refund edge cases (3f2a9c10)'), 'readable reason missing: ' + w);
  assert.ok(!w.includes('held by 3f2a9c10 <raw>'), 'raw reason must not render when a readable one exists: ' + w);
  // Holder name first, PRD cell after it, full PRD title in its own tooltip.
  assert.ok(w.indexOf('Fix tax rounding') < w.indexOf('title="Ship billing"'), 'holder must precede PRD cell: ' + w);
  const w2 = render(s, {
    pools: [], leases: [], backoff: [], budgets: [],
    waiting: [{ holder: 'feedbeef1234567890', holder_name: { readable: 'Ship billing / Fix tax rounding' }, prd_id: '', since: NOW - 4000, reason: 'held by 3f2a9c10' }],
  }, { now: NOW });
  assert.ok(w2.includes('held by 3f2a9c10'), 'raw reason is the fallback: ' + w2);
});

test('taskWaitHtml shows a readable wait reason and the task name before the raw text', () => {
  const s = loadApp();
  const readable = 'pool node:gpu1 is full (2/2), held by Ship billing / Refund edge cases (3f2a9c10)';
  const raw = 'pool node:gpu1 is full (2/2), held by 3f2a9c10';
  const h = wait(s, { status: 'waiting_capacity', wait_cause: 'node_busy', wait_reason: raw, wait_reason_readable: readable });
  assert.ok(h.includes(readable), 'readable reason missing: ' + h);
  assert.ok(!h.includes('>held by 3f2a9c10</span>'), 'raw reason must not render beside the readable one: ' + h);
  // Task detail shape: PRD title / task title become the name, short id secondary.
  const d = wait(s, { status: 'waiting_capacity', id: 'task9f8e7d6c5b4a', prd_title: 'Ship billing', task_title: 'Fix tax rounding', wait_reason: raw });
  assert.ok(d.includes('Ship billing / Fix tax rounding'), 'task name missing: ' + d);
  assert.ok(d.indexOf('Ship billing / Fix tax rounding') < d.indexOf('task9f8e'), 'name must precede short id: ' + d);
  assert.ok(d.includes(raw), 'reason must still render: ' + d);
  // A body with neither name nor readable renders exactly the reason, as before.
  assert.equal(wait(s, { status: 'waiting_capacity', wait_reason: 'pool host is full' }), 'pool host is full');
});

test('all capacity_name_ keys exist in every locale bundle and the English fallback is coded', () => {
  const keys = ['capacity_name_untitled', 'capacity_name_prd'];
  for (const loc of LOCALES) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', loc + '.json'), 'utf8'));
    for (const k of keys) {
      assert.equal(typeof b[k], 'string', `${loc}.json is missing ${k}`);
      assert.ok(b[k].trim().length > 0, `${loc}.json has an empty ${k}`);
      assert.doesNotMatch(b[k], /\b(BL|B|F|S)\d+\b/, `${loc}.json ${k} names a tracker id`);
    }
  }
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  assert.ok(src.includes("t('capacity_name_untitled')"), 'app.js must use the capacity_name_untitled key');
  assert.ok(src.includes("t('capacity_name_prd')"), 'app.js must use the capacity_name_prd key');
  assert.ok(src.includes("'(untitled {id})'"), 'English fallback literal for the untitled label must be coded');
});
