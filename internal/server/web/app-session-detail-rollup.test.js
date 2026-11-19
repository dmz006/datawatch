// Phone-width header icons of the session detail page: Tags and Scheduled are
// two aria-expanded buttons with count badges; wide width keeps the legacy
// layout. Open state lives in one module-level variable, never in web storage.

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
  const els = { sessionDetailTagRow: makeStubElement(), sessionSchedules: makeStubElement(), sessionDetailHeaderIcons: makeStubElement() };
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
    `state.activeSession = ${JSON.stringify(SID)}; _tagsState.list = ${JSON.stringify(TAGS)}; _tagsState.byId = _tagsById(_tagsState.list); _tagsState.loaded = true; state.sessions = ${JSON.stringify([sess])};`,
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
  ctx.sandbox._sessionDetailHeaderToggle(kind);
}
const open = (ctx) => vm.runInContext('_sessionDetailHeaderOpen', ctx.sandbox);

test('phone: header icons with count badges, accessible names, collapsed lists', async () => {
  const ctx = setup(true);
  await paintAll(ctx);
  const html = ctx.els.sessionDetailHeaderIcons.innerHTML;
  assert.equal((html.match(/<button type="button"/g) || []).length, 2);
  assert.match(html, /data-kind="tags"[^>]*aria-expanded="false" aria-label="Tags: 3"/);
  assert.match(html, /data-kind="scheduled"[^>]*aria-expanded="false" aria-label="Scheduled jobs: 2"/);
  assert.match(html, /<span class="session-detail-icon-badge">3<\/span>/);
  assert.match(html, /<span class="session-detail-icon-badge">2<\/span>/);
  assert.match(html, /title="next /); // next job time tooltip
  assert.equal(ctx.els.sessionDetailTagRow.innerHTML, '');
  assert.equal(ctx.els.sessionSchedules.innerHTML, '');
});

test('phone: tags popover opens in place, second tap closes', async () => {
  const ctx = setup(true);
  await paintAll(ctx);
  toggle(ctx, 'tags');
  assert.equal(open(ctx), 'tags');
  assert.match(ctx.els.sessionDetailHeaderIcons.innerHTML, /data-kind="tags"[^>]*aria-expanded="true"/);
  const html = ctx.els.sessionDetailTagRow.innerHTML;
  assert.ok(html.includes('session-detail-popover') && html.includes('Alpha') && html.includes('Gamma'));
  toggle(ctx, 'tags');
  assert.equal(open(ctx), '');
  assert.ok(!ctx.els.sessionDetailTagRow.innerHTML.includes('tag-chip'));
});

test('phone: scheduled popover lists jobs with cancel; opening one closes the other', async () => {
  const ctx = setup(true);
  await paintAll(ctx);
  toggle(ctx, 'scheduled');
  let html = ctx.els.sessionSchedules.innerHTML;
  assert.equal((html.match(/class="sched-item"/g) || []).length, 2);
  assert.ok(html.includes("cancelSchedule('sc1'"));
  toggle(ctx, 'tags');
  assert.equal(open(ctx), 'tags');
  assert.equal(ctx.els.sessionSchedules.innerHTML, '');
  assert.ok(ctx.els.sessionDetailTagRow.innerHTML.includes('Alpha'));
});

test('wide width: legacy layout, no header icons', async () => {
  const ctx = setup(false);
  await paintAll(ctx);
  assert.equal(ctx.els.sessionDetailHeaderIcons.innerHTML, '');
  const tagHtml = ctx.els.sessionDetailTagRow.innerHTML;
  assert.ok(!tagHtml.includes('aria-expanded'));
  assert.ok(tagHtml.includes('tag-chip') && tagHtml.includes('Alpha'));
  const schedHtml = ctx.els.sessionSchedules.innerHTML;
  assert.ok(!schedHtml.includes('aria-expanded') && !schedHtml.includes('session-detail-popover'));
  assert.equal((schedHtml.match(/class="sched-item"/g) || []).length, 2);
});

test('pure renderer: fakes in, markup out; zero counts omit the button', () => {
  const ctx = setup(true);
  const f = ctx.sandbox._sessionDetailHeaderIconsHTML;
  assert.equal(f({ tagCount: 0, jobs: [] }), '');
  const html = f({ tagCount: 1, jobs: [{ run_at: '2030-01-02T00:00:00Z' }, { run_at: '2030-01-01T00:00:00Z' }], open: 'scheduled', tr: (k, d) => '[' + k + ']' });
  assert.match(html, /data-kind="tags"[^>]*aria-expanded="false"/);
  assert.match(html, /data-kind="scheduled"[^>]*aria-expanded="true"/);
  assert.ok(html.includes('[session_detail_tags_button_label]'));
});

test('state: in memory only, never touches web storage', async () => {
  const ctx = setup(true);
  await paintAll(ctx);
  toggle(ctx, 'tags');
  toggle(ctx, 'scheduled');
  assert.equal(open(ctx), 'scheduled');
  assert.deepEqual(ctx.writes, [], 'header icon state must not be persisted to web storage');
});
