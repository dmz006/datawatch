// Phone-width rollup of the session detail page: Tags and Scheduled collapse
// to one aria-expanded summary button at phone width; wide width keeps the
// legacy layout. Expanded state lives in memory only.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, makeStubElement, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const SID = 'host-aaaa';
const TAGS = [
  { id: 'own1', name: 'Alpha', color: '#112233' },
  { id: 'own2', name: 'Beta', color: '#223344' },
  { id: 'own3', name: 'Gamma', color: '#334455' },
];
const JOBS = [
  { id: 'sc1', command: 'continue', run_at: '2030-01-01T10:00:00Z' },
  { id: 'sc2', command: 'status', run_at: '2030-01-01T11:00:00Z' },
];

// setup builds a sandbox whose matchMedia reports `phone` (true = phone,
// false = wide), with recorders on localStorage/sessionStorage.
function setup(phone) {
  const sandbox = buildSandbox();
  const els = { sessionDetailTagRow: makeStubElement(), sessionSchedules: makeStubElement() };
  sandbox.document.getElementById = (id) => els[id] || makeStubElement();
  sandbox.matchMedia = (q) => ({ media: q, matches: !!phone, addListener() {}, addEventListener() {} });
  const writes = [];
  const rec = () => { const m = new Map(); return {
    getItem(k) { return m.has(k) ? m.get(k) : null; },
    setItem(k, v) { writes.push([k, v]); m.set(k, String(v)); },
    removeItem(k) { m.delete(k); },
  }; };
  sandbox.localStorage = rec();
  sandbox.sessionStorage = rec();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  const sess = { id: 'aaaa', full_id: SID, tags: ['own1', 'own2', 'own3'], inherited_tags: [] };
  vm.runInContext(
    `_tagsState.list = ${JSON.stringify(TAGS)}; _tagsState.byId = _tagsById(_tagsState.list); _tagsState.loaded = true; state.sessions = ${JSON.stringify([sess])};`,
    sandbox);
  sandbox.apiFetch = () => Promise.resolve(JOBS.map(j => ({ ...j })));
  return { sandbox, els, sess, writes };
}

async function paintAll(ctx) {
  ctx.sandbox._renderSessionDetailTagRow(ctx.sess);
  vm.runInContext(`loadSessionSchedules(${JSON.stringify(SID)})`, ctx.sandbox);
  await flushAsync(30);
}

function toggle(ctx, kind) {
  ctx.sandbox._sessionDetailRollupToggle(encodeURIComponent(SID), kind);
}

test('phone width, tags collapsed initially: aria-expanded=false, summary + count, no chips', async () => {
  const ctx = setup(true);
  await paintAll(ctx);
  const html = ctx.els.sessionDetailTagRow.innerHTML;
  assert.match(html, /class="session-detail-tags-rollup"/);
  assert.match(html, /aria-expanded="false"/);
  assert.match(html, /Tags \(3\)/); // session_detail_tags_summary fallback + count
  assert.ok(!html.includes('tag-chip'), 'full chip list must be absent while collapsed');
  assert.ok(!html.includes('Alpha'));
});

test('phone width, tags expanded then collapsed again', async () => {
  const ctx = setup(true);
  await paintAll(ctx);
  toggle(ctx, 'tags');
  let html = ctx.els.sessionDetailTagRow.innerHTML;
  assert.match(html, /aria-expanded="true"/);
  assert.ok(html.includes('tag-chip'));
  assert.ok(html.includes('Alpha') && html.includes('Gamma'));
  toggle(ctx, 'tags');
  html = ctx.els.sessionDetailTagRow.innerHTML;
  assert.match(html, /aria-expanded="false"/);
  assert.ok(!html.includes('tag-chip'));
});

test('phone width, scheduled: collapsed summary then per-job rows with cancel when expanded', async () => {
  const ctx = setup(true);
  await paintAll(ctx);
  let html = ctx.els.sessionSchedules.innerHTML;
  assert.match(html, /class="session-detail-scheduled-rollup"/);
  assert.match(html, /aria-expanded="false"/);
  assert.match(html, /Scheduled \(2\)/);
  assert.match(html, /Next run /);
  assert.ok(!html.includes('sched-item'), 'job rows hidden while collapsed');
  toggle(ctx, 'scheduled');
  html = ctx.els.sessionSchedules.innerHTML;
  assert.match(html, /aria-expanded="true"/);
  assert.equal((html.match(/class="sched-item"/g) || []).length, 2);
  assert.ok(html.includes("cancelSchedule('sc1'"));
  assert.ok(html.includes('continue') && html.includes('status'));
  toggle(ctx, 'scheduled');
  assert.match(ctx.els.sessionSchedules.innerHTML, /aria-expanded="false"/);
});

test('wide width: legacy layout, no rollup classes', async () => {
  const ctx = setup(false);
  await paintAll(ctx);
  const tagHtml = ctx.els.sessionDetailTagRow.innerHTML;
  assert.ok(!tagHtml.includes('session-detail-tags-rollup'));
  assert.ok(!tagHtml.includes('aria-expanded'));
  assert.ok(tagHtml.includes('tag-chip') && tagHtml.includes('Alpha'));
  const schedHtml = ctx.els.sessionSchedules.innerHTML;
  assert.ok(!schedHtml.includes('session-detail-scheduled-rollup'));
  assert.ok(!schedHtml.includes('aria-expanded'));
  assert.equal((schedHtml.match(/class="sched-item"/g) || []).length, 2);
  assert.ok(schedHtml.includes("cancelSchedule('sc2'"));
});

test('state: expanded state survives a repaint from the in-memory map, never touches web storage', async () => {
  const ctx = setup(true);
  await paintAll(ctx);
  toggle(ctx, 'tags');
  toggle(ctx, 'scheduled');
  // Re-run the paint paths.
  ctx.sandbox._renderSessionDetailTagRow(ctx.sess);
  vm.runInContext(`_renderSessionSchedules(${JSON.stringify(SID)})`, ctx.sandbox);
  assert.match(ctx.els.sessionDetailTagRow.innerHTML, /aria-expanded="true"/);
  assert.match(ctx.els.sessionSchedules.innerHTML, /aria-expanded="true"/);
  const bucket = vm.runInContext('JSON.stringify(state._sessionDetailRollup)', ctx.sandbox);
  assert.deepEqual(JSON.parse(bucket), { [SID]: { tags: true, scheduled: true } });
  const rollupWrites = ctx.writes.filter(([k, v]) => /rollup/i.test(k) || /rollup|aria-expanded/i.test(String(v)));
  assert.deepEqual(rollupWrites, [], 'rollup state must not be persisted to web storage');
});
