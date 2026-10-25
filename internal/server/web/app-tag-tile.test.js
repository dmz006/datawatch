// Shared work tile for the Automata and Automation lists. Covers the pure
// helpers (progress maths, tag-chip overflow, relative age, row mode), the tile
// markup and its escaping, the proof that both lists draw through the one
// renderer, and locale parity for the tile keys.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const WEB = __dirname;
const NOW = Date.parse('2026-10-25T12:00:00Z');
const plain = v => JSON.parse(JSON.stringify(v));
const TAGS = {
  t1: { id: 't1', name: 'Work' },
  t2: { id: 't2', name: 'Mobile' },
  t3: { id: 't3', name: 'iOS' },
  t4: { id: 't4', name: 'Home' },
  t5: { id: 't5', name: 'Later' },
};

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(WEB, 'app.js'), sandbox);
  // t() returns the raw key until a bundle loads; install the English one so
  // the rendered strings are the real user-visible ones.
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

function setRegistry(sb, byId) {
  vm.runInContext(`_tagsState.byId = ${JSON.stringify(byId)}; _tagsState.loaded = true;`, sb);
}

// ── progress ────────────────────────────────────────────────────────────────

test('progress is done over total as a whole-number percentage', () => {
  const sb = loadApp();
  assert.deepEqual(plain(sb._tileProgress(4, 9)), { done: 4, total: 9, pct: 44 });
  assert.deepEqual(plain(sb._tileProgress(9, 9)), { done: 9, total: 9, pct: 100 });
});

test('zero tasks draw an empty bar and a 0 / 0 count, never NaN', () => {
  const sb = loadApp();
  assert.deepEqual(plain(sb._tileProgress(0, 0)), { done: 0, total: 0, pct: 0 });
  assert.deepEqual(plain(sb._tileProgress(undefined, undefined)), { done: 0, total: 0, pct: 0 });
  assert.deepEqual(plain(sb._tileAutomataCounts({ task_count: 0, task_status_counts: {} })), { done: 0, total: 0, pct: 0 });
  assert.deepEqual(plain(sb._tileAutomataCounts({})), { done: 0, total: 0, pct: 0 });
  const html = sb.renderWorkTile({
    id: 'z1', title: 'Empty', progress: { done: 0, total: 0 }, progressLabel: '0 / 0', nowMs: NOW,
  });
  assert.match(html, /style="width:0%;"/);
  assert.ok(!html.includes('NaN'));
});

test('a done count outside the range is clamped so the bar stops at full and never runs backwards', () => {
  const sb = loadApp();
  assert.deepEqual(plain(sb._tileProgress(12, 9)), { done: 9, total: 9, pct: 100 });
  assert.deepEqual(plain(sb._tileProgress(-3, 9)), { done: 0, total: 9, pct: 0 });
});

test('an Automaton list summary counts completed tasks against task_count', () => {
  const sb = loadApp();
  const summary = {
    task_count: 9,
    task_status_counts: { completed: 4, running: 1 },
    stories: [{ id: 's1', task_count: 9, task_status_counts: { completed: 4 } }],
  };
  assert.deepEqual(plain(sb._tileAutomataCounts(summary)), { done: 4, total: 9, pct: 44 });
});

test('a full record with tasks is counted task by task across its stories', () => {
  const sb = loadApp();
  const full = {
    task_count: 99,
    stories: [
      { tasks: [{ status: 'completed' }, { status: 'running' }] },
      { tasks: [{ status: 'completed' }, { status: 'completed' }] },
    ],
  };
  assert.deepEqual(plain(sb._tileAutomataCounts(full)), { done: 3, total: 4, pct: 75 });
});

// ── tags ────────────────────────────────────────────────────────────────────

test('the +N overflow counts the tags past three and shows the first three', () => {
  const sb = loadApp();
  const five = ['t1', 't2', 't3', 't4', 't5'].map(id => TAGS[id]);
  const out = plain(sb._tileTagOverflow(five, 3));
  assert.deepEqual(out.shown.map(tg => tg.name), ['Work', 'Mobile', 'iOS']);
  assert.equal(out.more, 2);
  assert.equal(sb._tileTagOverflow(five.slice(0, 3)).more, 0, 'three tags need no +N chip');
  assert.equal(sb._tileTagOverflow([]).more, 0);
  assert.equal(sb._tileTagOverflow(five).shown.length, 3, 'the default limit is three chips');
});

test('a tile with five tags shows three chips and a +2 control that holds the full list', () => {
  const sb = loadApp();
  setRegistry(sb, TAGS);
  const html = sb.renderWorkTile({
    id: 'p1', title: 'Tagged', tagIds: ['t1', 't2', 't3', 't4', 't5'], tagsById: TAGS,
    progress: { done: 0, total: 0 }, progressLabel: '0 / 0', nowMs: NOW,
  });
  assert.match(html, /<details class="work-tile-tags-more"/);
  assert.match(html, />\+2</);
  const row = html.split('class="work-tile-tags"')[1].split('work-tile-tags-more"')[0];
  assert.equal((row.match(/class="tag-chip"/g) || []).length, 3, 'three chips before the +N control');
  assert.equal((html.match(/class="tag-chip"/g) || []).length, 8, 'three shown plus the five in the full list');
});

test('a tile with three tags or fewer draws no +N control', () => {
  const sb = loadApp();
  setRegistry(sb, TAGS);
  const html = sb.renderWorkTile({
    id: 'p2', title: 'Few', tagIds: ['t1', 't2'], tagsById: TAGS,
    progress: { done: 0, total: 0 }, progressLabel: '0 / 0', nowMs: NOW,
  });
  assert.ok(!html.includes('work-tile-tags-more'));
  assert.equal((html.match(/class="tag-chip"/g) || []).length, 2);
});

test('tag IDs the registry does not hold are dropped, including inherited keys', () => {
  const sb = loadApp();
  const out = plain(sb._tileResolveTags(['t1', 'missing', 'constructor'], TAGS));
  assert.deepEqual(out.map(tg => tg.name), ['Work']);
});

test('an item reads tags from the list payload, falling back to tag_ids on a full record', () => {
  const sb = loadApp();
  assert.deepEqual(plain(sb._tileTagIds({ tags: ['t1'] })), ['t1']);
  assert.deepEqual(plain(sb._tileTagIds({ tag_ids: ['t2'] })), ['t2']);
  assert.deepEqual(plain(sb._tileTagIds({})), []);
  assert.deepEqual(plain(sb._tileTagIds(null)), []);
});

// ── relative age ────────────────────────────────────────────────────────────

test('the relative age reads now, minutes, hours and days, and skips a missing or bad time', () => {
  const sb = loadApp();
  const ago = ms => new Date(NOW - ms).toISOString();
  assert.equal(sb._tileAgeText(ago(30 * 1000), NOW), 'just now');
  assert.equal(sb._tileAgeText(ago(4 * 60 * 1000), NOW), '4m ago');
  assert.equal(sb._tileAgeText(ago(3 * 3600 * 1000), NOW), '3h ago');
  assert.equal(sb._tileAgeText(ago(2 * 86400 * 1000), NOW), '2d ago');
  assert.equal(sb._tileAgeText(new Date(NOW + 60000).toISOString(), NOW), 'just now', 'a future time reads as now');
  assert.equal(sb._tileAgeText('', NOW), '');
  assert.equal(sb._tileAgeText('not a time', NOW), '');
});

// ── row mode ────────────────────────────────────────────────────────────────

test('row mode switches at 720px and 420px, with the boundary on the wide side', () => {
  const sb = loadApp();
  assert.equal(sb._tileRowMode(719), 'compact');
  assert.equal(sb._tileRowMode(720), 'tile');
  assert.equal(sb._tileRowMode(419), 'line');
  assert.equal(sb._tileRowMode(420), 'compact');
  assert.equal(sb._tileRowMode(1280), 'tile');
  assert.equal(sb._tileRowMode(undefined), 'tile', 'an unknown width reads as wide');
});

test('the grid carries the row mode for the viewport at render time', () => {
  const sb = loadApp();
  sb.innerWidth = 419;
  assert.equal(sb._tileGridHTML('x'), '<div class="work-tile-grid" data-tile-mode="line">x</div>');
  sb.innerWidth = 720;
  assert.match(sb._tileGridHTML('x'), /data-tile-mode="tile"/);
});

test('a resize re-applies the mode to every grid on screen without a re-render', () => {
  const sb = loadApp();
  const grids = [{ dataset: { tileMode: 'tile' } }, { dataset: { tileMode: 'tile' } }];
  sb.document.querySelectorAll = sel => (sel === '.work-tile-grid' ? grids : []);
  sb.innerWidth = 500;
  sb._tileSyncModes();
  assert.deepEqual(grids.map(g => g.dataset.tileMode), ['compact', 'compact']);
});

test('the resize listener is bound once however often the script loads', () => {
  const sandbox = buildSandbox();
  const events = [];
  sandbox.addEventListener = ev => events.push(ev);
  vm.createContext(sandbox);
  loadScript(path.join(WEB, 'app.js'), sandbox);
  assert.equal(events.filter(ev => ev === 'resize').length, 1);
});

// ── markup and escaping ─────────────────────────────────────────────────────

test('a tile carries the card classes, short id, backend, progress, tags and activity', () => {
  const sb = loadApp();
  setRegistry(sb, TAGS);
  const html = sb.renderWorkTile({
    id: 'a1b2c3d4e5', rootId: 'prd-a1b2c3d4e5', title: 'Release pipeline',
    statusClass: 'prd-card-status-running', statusHtml: '<span>Running</span>',
    backend: 'claude-code · sonnet', progress: { done: 4, total: 9 }, progressLabel: '4 / 9',
    tagIds: ['t1'], tagsById: TAGS, updatedAt: new Date(NOW - 4 * 60000).toISOString(), nowMs: NOW,
    primaryHtml: '<button>Open</button>', menuHtml: '<button>Pin</button>', onOpen: 'openIt()',
  });
  assert.match(html, /^<div class="prd-row prd-card prd-card-status-running work-tile" id="prd-a1b2c3d4e5"/);
  assert.match(html, /onclick="openIt\(\)"/);
  assert.match(html, />a1b2c3d4</, 'the short id is the first eight characters');
  assert.match(html, /claude-code · sonnet/);
  assert.match(html, /style="width:44%;"/);
  assert.match(html, />4 \/ 9</);
  assert.match(html, /4m ago/);
  assert.match(html, /class="tag-chip"/);
});

test('actions split between the wide row and the overflow menu, and a tile with no other actions hides the menu on wide screens', () => {
  const sb = loadApp();
  const withMenu = sb.renderWorkTile({
    id: 'p', title: 'x', primaryHtml: '<button class="open">Open</button>', menuHtml: '<button class="pin">Pin</button>',
    progress: { done: 0, total: 0 }, nowMs: NOW,
  });
  assert.match(withMenu, /<details class="work-tile-more"/);
  assert.match(withMenu, /class="work-tile-primary work-tile-actions"[^>]*><button class="open">Open<\/button>/);
  assert.match(withMenu, /<div class="work-tile-primary-copy"><button class="open">Open<\/button><\/div>/);
  assert.match(withMenu, /<button class="pin">Pin<\/button>/);
  const openOnly = sb.renderWorkTile({
    id: 'a', title: 'y', primaryHtml: '<button class="open">Open</button>', progress: { done: 0, total: 0 }, nowMs: NOW,
  });
  assert.match(openOnly, /class="work-tile-more is-narrow-only"/);
});

test('a hostile title, tag name and id are escaped on the tile', () => {
  const sb = loadApp();
  const hostile = { bad: { id: 'bad', name: '<b>tag</b>' }, 'q"><i>': { id: 'q"><i>', name: 'ok' } };
  const html = sb.renderWorkTile({
    id: 'x"><script>', rootId: 'prd-x"><script>', title: '<img src=x onerror=alert(1)>',
    tagIds: ['bad', 'q"><i>'], tagsById: hostile, progress: { done: 0, total: 0 }, progressLabel: '0 / 0', nowMs: NOW,
  });
  assert.ok(!html.includes('<img'), 'title markup is escaped');
  assert.ok(!html.includes('<b>tag'), 'tag name markup is escaped');
  assert.ok(!html.includes('<script>'), 'id markup is escaped');
  assert.ok(!html.includes('<i>'), 'tag id markup is escaped');
  assert.ok(html.includes('&lt;img src=x onerror=alert(1)&gt;'));
  assert.ok(html.includes('&lt;b&gt;tag&lt;/b&gt;'));
});

// ── both lists use the one renderer ─────────────────────────────────────────

test('the Automata card and the Automation card both draw through renderWorkTile', () => {
  const sb = loadApp();
  const calls = [];
  sb.renderWorkTile = o => { calls.push(o); return '<div class="spy"></div>'; };
  assert.equal(sb.renderAutomataCard({ id: 'p1', title: 'Alpha', status: 'running' }), '<div class="spy"></div>');
  assert.equal(calls[0].rootId, 'prd-p1');
  assert.equal(sb.buildAutomationCardHTML({ id: 'a1', name: 'Chain', status: 'active' }, { now: NOW }), '<div class="spy"></div>');
  assert.equal(calls[1].extraClass, 'automation-card');
  assert.equal(calls.length, 2);
});

test('the source has one tile renderer, and both lists wrap their tiles in the grid', () => {
  const src = fs.readFileSync(path.join(WEB, 'app.js'), 'utf8');
  assert.equal((src.match(/^function renderWorkTile\(/gm) || []).length, 1);
  assert.equal((src.match(/return renderWorkTile\(\{/g) || []).length, 2);
  assert.equal((src.match(/_tileGridHTML\(filtered\.map\(renderAutomataCard\)/g) || []).length, 1);
  assert.equal((src.match(/_tileGridHTML\(items\.map\(/g) || []).length, 2);
});

test('an Automata tile shows the registry tags of its list item', () => {
  const sb = loadApp();
  setRegistry(sb, TAGS);
  const html = sb.renderAutomataCard({ id: 'p9', title: 'Tagged', status: 'draft', tags: ['t2'] });
  assert.match(html, /<span class="tag-chip" data-tag-id="t2"/);
});

test('an Automation tile shows the computed tags its list item carries, read-only', () => {
  const sb = loadApp();
  setRegistry(sb, TAGS);
  const html = sb.buildAutomationCardHTML({ id: 'a7', name: 'Chain', status: 'active', tags: ['t1', 't2'] }, { now: NOW });
  assert.match(html, /data-tag-id="t1"/);
  assert.match(html, /data-tag-id="t2"/);
  assert.ok(!html.includes('work-tile-tags-more'), 'two tags need no +N control');
});

test('an Automation tile keeps its member, idle and blocked lines in the body', () => {
  const sb = loadApp();
  const html = sb.buildAutomationCardHTML({
    id: 'a8', name: 'Busy', status: 'blocked',
    members: [{ id: 'm1', prd_id: 'p1', status: 'running' }],
    decisions: [{ at: '2026-10-25T10:00:00Z', kind: 'blocked', note: 'waiting' }],
  }, { prdTitles: { p1: 'Build it' }, now: NOW });
  assert.match(html, /class="work-tile-body"/);
  assert.match(html, />Running: Build it</);
  assert.match(html, />Blocked: waiting</);
});

// ── locale parity ───────────────────────────────────────────────────────────

test('every work_tile key is in all five bundles and reads without a tracker ID or the word Automation', () => {
  const dir = path.join(WEB, 'locales');
  const keys = Object.keys(JSON.parse(fs.readFileSync(path.join(dir, 'en.json'), 'utf8'))).filter(k => k.startsWith('work_tile_'));
  assert.ok(keys.length >= 9, 'the tile has its keys');
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(dir, lang + '.json'), 'utf8'));
    for (const k of keys) {
      assert.ok(typeof bundle[k] === 'string' && bundle[k].length > 0, `${lang} is missing ${k}`);
      assert.doesNotMatch(bundle[k], /\bBL\d+\b|\bPRD\b|Automation/, `${lang} ${k}`);
    }
  }
});
