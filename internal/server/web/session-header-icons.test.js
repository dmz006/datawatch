// Session detail header icons (Tags, Scheduled) — phone vs wide width.
// Fakes only: a tiny fake DOM, a fake matchMedia and fake session data; no
// daemon. Covers: at phone width the summary rows are absent and the header
// holds both icon buttons with the right badge counts, the scheduled tooltip
// carries the next-job time, aria-expanded is present; a tap toggles the list,
// a second tap, an outside tap and Escape close it; at wide width the rows are
// unchanged and no icons exist; nothing is written to web storage.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const vm = require('vm');
const { buildSandbox, loadScript } = require('./testutil_browser_stub');

const WEB = __dirname;
const SID = 'host-abcd';

function fakeEl() {
  return { innerHTML: '', style: {}, classList: { add() {}, remove() {}, toggle() {}, contains() { return false; } },
    setAttribute() {}, getAttribute() { return null; }, addEventListener() {}, removeEventListener() {},
    appendChild() {}, querySelector() { return null; }, querySelectorAll() { return []; } };
}

function load(phone) {
  const els = { sessionDetailHeaderIcons: fakeEl(), sessionDetailTagRow: fakeEl(), sessionSchedules: fakeEl() };
  const sb = buildSandbox({ byId: els });
  const listeners = {};
  const writes = [];
  sb.document.addEventListener = (type, fn) => { (listeners[type] = listeners[type] || []).push(fn); };
  sb.matchMedia = (q) => ({ matches: phone && /max-width:\s*640px/.test(q), addEventListener() {}, addListener() {} });
  sb.localStorage = { getItem() { return null; }, setItem(k, v) { writes.push(['local', k, v]); }, removeItem(k) { writes.push(['local-rm', k]); } };
  sb.sessionStorage = { getItem() { return null; }, setItem(k, v) { writes.push(['session', k, v]); }, removeItem(k) { writes.push(['session-rm', k]); } };
  sb.fetch = () => Promise.resolve({ ok: false, status: 404, json: () => Promise.resolve({}) });
  vm.createContext(sb);
  loadScript(path.join(WEB, 'app.js'), sb);
  sb._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  const run = (code) => vm.runInContext(code, sb);
  writes.length = 0; // ignore anything the load itself did
  run(`state.activeSession = ${JSON.stringify(SID)};
    state.sessions = [{ id: 'abcd', full_id: ${JSON.stringify(SID)}, inherited_tags: [], tags: ['t1', 't2'] }];
    _tagsState.byId = { t1: { id: 't1', name: 'alpha' }, t2: { id: 't2', name: 'beta' } };
    _schedLastItems.set(${JSON.stringify(SID)}, [
      { id: 's1', command: 'continue', run_at: '2031-05-06T10:00:00Z' },
      { id: 's2', command: 'status', run_at: '2030-02-03T04:05:00Z', cron_expr: '*/5 * * * *' },
      { id: 's3', command: 'later', run_at: '2032-01-01T00:00:00Z' }]);`);
  const sess = () => run(`_sessionFind(${JSON.stringify(SID)})`);
  const repaint = () => { run(`_renderSessionDetailTagRow(_sessionFind(${JSON.stringify(SID)}))`); run(`_renderSessionSchedules(${JSON.stringify(SID)})`); };
  const fire = (type, ev) => (listeners[type] || []).forEach(fn => fn(ev));
  const tap = (kind) => run(`_sessionDetailHeaderToggle(${JSON.stringify(kind)})`);
  const open = () => run('_sessionDetailHeaderOpen');
  return { sb, els, run, repaint, fire, tap, open, writes, sess, listeners };
}

const btnTag = (html, kind) => (html.match(new RegExp('<button[^>]*data-kind="' + kind + '"[^>]*>[\\s\\S]*?</button>')) || [''])[0];
const badge = (b) => (b.match(/session-detail-icon-badge">([^<]*)</) || [])[1];

test('phone: summary rows absent, header holds both icons with counts, tooltip and aria-expanded', () => {
  const h = load(true);
  h.repaint();
  assert.equal(h.els.sessionDetailTagRow.innerHTML, '');
  assert.equal(h.els.sessionDetailTagRow.style.display, 'none');
  assert.equal(h.els.sessionSchedules.innerHTML, '');
  assert.equal(h.els.sessionSchedules.style.display, 'none');
  const hdr = h.els.sessionDetailHeaderIcons.innerHTML;
  const tags = btnTag(hdr, 'tags'), sched = btnTag(hdr, 'scheduled');
  assert.ok(tags && sched, 'both icon buttons present');
  assert.equal(badge(tags), '2');
  assert.equal(badge(sched), '3');
  assert.match(tags, /aria-expanded="false"/);
  assert.match(sched, /aria-expanded="false"/);
  const next = new vm.Script('new Date("2030-02-03T04:05:00Z")').runInContext(h.sb);
  assert.match(sched, /title="next [^"]+"/);
  const title = sched.match(/title="([^"]*)"/)[1];
  assert.ok(title.includes(next.toLocaleString().replace(/&/g, '&amp;')) || title.includes(next.toLocaleString()), 'tooltip carries the earliest job time: ' + title);
});

test('phone: tap toggles the list; second tap, outside tap and Escape close it', () => {
  const h = load(true);
  h.repaint();
  h.tap('scheduled');
  assert.equal(h.open(), 'scheduled');
  assert.match(btnTag(h.els.sessionDetailHeaderIcons.innerHTML, 'scheduled'), /aria-expanded="true"/);
  assert.match(h.els.sessionSchedules.innerHTML, /session-detail-popover/);
  assert.equal(h.els.sessionSchedules.style.display, 'block');
  h.tap('scheduled');
  assert.equal(h.open(), '');
  assert.equal(h.els.sessionSchedules.innerHTML, '');

  h.tap('tags');
  assert.equal(h.open(), 'tags');
  assert.match(h.els.sessionDetailTagRow.innerHTML, /session-detail-popover/);
  assert.match(btnTag(h.els.sessionDetailHeaderIcons.innerHTML, 'tags'), /aria-expanded="true"/);

  // outside tap closes; a tap inside the popover/buttons does not
  h.fire('click', { target: { closest: () => ({}) } });
  assert.equal(h.open(), 'tags');
  h.fire('click', { target: { closest: () => null } });
  assert.equal(h.open(), '');
  assert.equal(h.els.sessionDetailTagRow.innerHTML, '');

  h.tap('scheduled');
  h.fire('keydown', { key: 'Enter' });
  assert.equal(h.open(), 'scheduled');
  h.fire('keydown', { key: 'Escape' });
  assert.equal(h.open(), '');
  assert.match(btnTag(h.els.sessionDetailHeaderIcons.innerHTML, 'scheduled'), /aria-expanded="false"/);
});

test('wide: rows render as before and no header icons exist', () => {
  const h = load(false);
  h.repaint();
  assert.equal(h.els.sessionDetailHeaderIcons.innerHTML, '');
  assert.equal(h.els.sessionDetailTagRow.style.display, 'flex');
  assert.match(h.els.sessionDetailTagRow.innerHTML, /tag-chip/);
  assert.doesNotMatch(h.els.sessionDetailTagRow.innerHTML, /session-detail-popover/);
  assert.equal(h.els.sessionSchedules.style.display, 'block');
  assert.match(h.els.sessionSchedules.innerHTML, /Scheduled/);
  assert.match(h.els.sessionSchedules.innerHTML, /\(3\)/);
  assert.equal((h.els.sessionSchedules.innerHTML.match(/class="sched-item"/g) || []).length, 3);
  assert.doesNotMatch(h.els.sessionSchedules.innerHTML, /session-detail-icon-btn/);
});

test('no web storage writes happen while opening and closing the lists', () => {
  for (const phone of [true, false]) {
    const h = load(phone);
    h.repaint();
    h.tap('tags'); h.tap('scheduled'); h.tap('scheduled');
    h.fire('keydown', { key: 'Escape' });
    h.fire('click', { target: { closest: () => null } });
    h.repaint();
    assert.deepEqual(h.writes, [], 'storage untouched (phone=' + phone + ')');
  }
});
