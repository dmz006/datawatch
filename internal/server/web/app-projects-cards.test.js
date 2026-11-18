// Projects page — shared card renderer: Cards / Tiles / Compact.
//
// One shared renderer (renderProjectCard / renderProjectCards /
// renderProjectsCardToggle) draws any item {title, badge, badgeKind, age,
// details[], actions[], link} in three layouts. The mode is remembered in one
// localStorage key (every read/write in try/catch), phones default to compact
// (wider screens to cards), and shipped or merged work never gets a cancelled
// badge.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP = path.join(__dirname, 'app.js');
const EN = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
const NOW = Date.parse('2026-10-25T12:00:00Z');
const MODE_KEY = 'cs_projects_view_mode';

// loadApp builds the app fresh with the English bundle. innerWidth drives the
// first-visit (phone vs. wider) default.
function loadApp() {
  const sb = buildSandbox({});
  sb.innerWidth = 1280;
  vm.createContext(sb);
  loadScript(APP, sb);
  sb._i18n.bundle = EN;
  return sb;
}

function throwingStorage() {
  const store = {
    getItem() { throw new Error('storage read blocked'); },
    setItem() { throw new Error('storage write blocked'); },
  };
  return store;
}

// ── Mode: normalize + first-visit default ─────────────────────────────────

test('the three modes normalize, everything else does not', () => {
  const sb = loadApp();
  for (const m of ['cards', 'tiles', 'compact']) assert.equal(sb._projectsCardModeNormalize(m), m);
  for (const bad of ['list', 'LIST', '', ' ', 42, null, undefined, {}]) assert.equal(sb._projectsCardModeNormalize(bad), null);
  assert.deepEqual([...sb.PROJECTS_CARD_MODES], ['cards', 'tiles', 'compact']);
});

test('phones default to compact, wider screens to cards, unknown widths to cards', () => {
  const sb = loadApp();
  assert.equal(sb._projectsCardModeDefault(360), 'compact');
  assert.equal(sb._projectsCardModeDefault(599), 'compact');
  assert.equal(sb._projectsCardModeDefault(600), 'cards');
  assert.equal(sb._projectsCardModeDefault(1280), 'cards');
  assert.equal(sb._projectsCardModeDefault(undefined), 'cards', 'no window metrics: not a phone');
  assert.equal(sb._projectsCardModeDefault(-8), 'cards');
  assert.equal(sb._projectsCardModeDefault(0), 'cards');
});

test('first visit: phone width opens compact, desktop width opens cards', () => {
  const phone = loadApp();
  phone.innerWidth = 360;
  assert.equal(phone._projectsCardMode(), 'compact');

  const desk = loadApp();
  assert.equal(desk._projectsCardMode(), 'cards');
});

// ── Mode: storage (remembered mode, throwing storage) ──────────────────────

test('a stored mode beats the viewport default', () => {
  const phone = loadApp();
  phone.innerWidth = 360;
  phone.localStorage.setItem(MODE_KEY, 'tiles');
  assert.equal(phone._projectsCardMode(), 'tiles');

  const desk = loadApp();
  desk.localStorage.setItem(MODE_KEY, 'compact');
  assert.equal(desk._projectsCardMode(), 'compact');
});

test('a stored garbage value falls back to the viewport default', () => {
  const sb = loadApp();
  sb.localStorage.setItem(MODE_KEY, 'list');
  assert.equal(sb._projectsCardMode(), 'cards', 'desktop width → cards');
  const phone = loadApp();
  phone.innerWidth = 360;
  phone.localStorage.setItem(MODE_KEY, 'bogus');
  assert.equal(phone._projectsCardMode(), 'compact');
});

test('throwing storage is swallowed: read yields null, store does not throw', () => {
  const sb = loadApp();
  const bad = throwingStorage();
  assert.equal(sb._projectsCardModeRead(bad), null);
  assert.doesNotThrow(() => sb._projectsCardModeStore('tiles', bad));
  assert.doesNotThrow(() => sb._projectsCardModeStore('bogus', bad));
});

test('storing a valid mode lands in localStorage, invalid ones do not write', () => {
  const sb = loadApp();
  sb._projectsCardModeStore('compact');
  assert.equal(sb.localStorage.getItem(MODE_KEY), 'compact');
  sb._projectsCardModeStore('list');
  assert.equal(sb.localStorage.getItem(MODE_KEY), 'compact', 'invalid mode: nothing new written');
});

test('projectsSetViewMode remembers the choice and repaints only the active Projects view', () => {
  const sb = loadApp();
  vm.runInContext('__painted = 0; _projectsPaint = function () { __painted += 1; };', sb);
  assert.equal(sb.__painted, 0);
  vm.runInContext("state.activeView = 'autonomous'; _automataState.tab = 'projects'", sb);
  assert.equal(sb._projectsCardSetMode('tiles'), true);
  assert.equal(sb.localStorage.getItem(MODE_KEY), 'tiles');
  assert.equal(sb._projectsCardMode(), 'tiles');
  assert.equal(sb.__painted, 1);
  sb.__painted = 0;
  vm.runInContext("_automataState.tab = 'arcs'", sb);
  assert.equal(sb._projectsCardSetMode('compact'), true, 'still remembered…');
  assert.equal(sb.localStorage.getItem(MODE_KEY), 'compact');
  assert.equal(sb.__painted, 0, '…but no repaint while the Projects page is not shown');
  assert.equal(sb._projectsCardSetMode('list'), false, 'invalid mode: rejected');
  assert.equal(sb.localStorage.getItem(MODE_KEY), 'compact');
  assert.equal(sb.__painted, 0);
  assert.equal(sb.projectsSetViewMode, sb._projectsCardSetMode, 'public name for the inline onclick handlers');
});

// ── The three modes render the same item ───────────────────────────────────

const RICH_ITEM = {
  id: 'proj-1',
  title: 'Alpha',
  badge: 'projects_badge_shipped',
  badgeKind: 'ok',
  age: NOW - 2 * 3600e3,
  nowMs: NOW,
  details: [
    { label: 'Directory (absolute path)', value: '/work/alpha' },
    'main → main',
    'CI green',
  ],
  actions: [{ label: 'Open', onclick: "selectProjectsView('alpha')" }],
  link: 'https://example.com/alpha',
};

test('cards mode: badge, age, link, all details truncated-safe, actions', () => {
  const sb = loadApp();
  const html = sb.renderProjectCard(RICH_ITEM, 'cards');
  assert.match(html, /class="project-card project-card-card" data-project-id="proj-1"/);
  assert.match(html, /<header class="project-card-head">/);
  assert.ok(html.includes('href="https://example.com/alpha" target="_blank" rel="noopener noreferrer"'), 'title links out');
  assert.match(html, /class="project-card-badge project-card-badge-ok">shipped<\/span>/);
  assert.match(html, /class="project-card-age">2h ago<\/span>/);
  for (const d of ['/work/alpha', 'main → main', 'CI green']) assert.ok(html.includes(d), d + ' in the detail list');
  assert.equal((html.match(/class="project-card-detail"/g) || []).length, 3);
  assert.ok(html.includes('class="project-card-action" onclick="selectProjectsView(\'alpha\')"'), 'actions render with their onclick');
});

test('tiles mode: first two details inline, the rest behind +N', () => {
  const sb = loadApp();
  const html = sb.renderProjectCard(RICH_ITEM, 'tiles');
  assert.match(html, /class="project-card project-card-tile"/);
  assert.equal((html.match(/class="project-card-detail-inline"/g) || []).length, 2, 'at most two inline details');
  assert.match(html, /\+1<\/span>/, 'the third detail counts as +1');
  assert.ok(!html.includes('project-card-details'), 'no <ul> in a tile');
  assert.match(html, /class="project-card-badge project-card-badge-ok">shipped<\/span>/, 'same badge as the card');
});

test('compact mode: one line of badge, title and age, no lists, no actions row', () => {
  const sb = loadApp();
  const html = sb.renderProjectCard(RICH_ITEM, 'compact');
  assert.match(html, /class="project-card project-card-compact"/);
  assert.ok(!html.includes('project-card-details'), 'no detail <ul>');
  assert.ok(!html.includes('project-card-title" onclick'), 'no action buttons');
  assert.match(html, /class="project-card-age">2h ago<\/span>/);
});

test('an explicit mode is honored; a bad mode falls back to the remembered/first-visit one', () => {
  const sb = loadApp();
  assert.match(sb.renderProjectCard(RICH_ITEM, 'bogus'), /project-card-card/, 'desktop default');
  const phone = loadApp();
  phone.innerWidth = 360;
  assert.match(phone.renderProjectCard(RICH_ITEM, 'bogus'), /project-card-compact/, 'phone default');
});

test('renderProjectCards lays a list out per-mode with the mode on the container', () => {
  const sb = loadApp();
  const cards = sb.renderProjectCards([RICH_ITEM, { ...RICH_ITEM, id: 'proj-2' }], 'cards');
  assert.match(cards, /class="project-cards project-cards-cards" data-project-mode="cards"/);
  assert.equal((cards.match(/data-project-id=/g) || []).length, 2);
  const tiles = sb.renderProjectCards([RICH_ITEM], 'tiles');
  assert.match(tiles, /class="project-cards project-cards-tiles" data-project-mode="tiles"/);
  const compact = sb.renderProjectCards([RICH_ITEM], 'compact');
  assert.match(compact, /class="project-cards project-cards-compact" data-project-mode="compact"/);
  assert.equal(sb.renderProjectCards([], 'cards'), '<div class="project-cards project-cards-cards" data-project-mode="cards"></div>');
  assert.equal((sb.renderProjectCards(undefined, 'tiles').match(/project-card-tile/g) || []).length, 0);
});

test('the mode toggle is a role=group of the three Sessions-style buttons, active one on top', () => {
  const sb = loadApp();
  const html = sb.renderProjectsCardToggle('tiles');
  assert.match(html, /class="sessions-mode-toggle" role="group" aria-label="Projects view mode"/);
  assert.match(html, /class="sessions-mode-btn active" [^>]*aria-label="Tiles"/);
  assert.match(html, /class="sessions-mode-btn" [^>]*aria-label="Cards"/);
  assert.match(html, /class="sessions-mode-btn" [^>]*aria-label="Compact"/);
  for (const m of ['cards', 'tiles', 'compact']) assert.ok(html.includes(`onclick="projectsSetViewMode('${m}')"`, m));
  assert.equal((html.match(/class="sessions-mode-btn active"/g) || []).length, 1, 'exactly one active mode');
  // Default (no stored mode, desktop): cards is active.
  const fresh = loadApp();
  assert.match(fresh.renderProjectsCardToggle('bogus'), /class="sessions-mode-btn active"[^>]*aria-label="Cards"/);
});

// ── Truncation: full value survives in the title attribute ────────────────

test('long titles are cut with an ellipsis; the full value stays in title=""', () => {
  const sb = loadApp();
  const long = 'x'.repeat(120) + ' tail';
  const card = sb.renderProjectCard({ title: long }, 'cards');
  assert.equal(card.match(/<span class="project-card-title"[^>]*title="([^"]*)"/)[1], long, 'full title in the attribute');
  const shown = card.match(/<span class="project-card-title"[^>]*>([^<]*)<\/span>/)[1];
  assert.ok(shown.length <= 100, 'rendered text is short');
  assert.match(shown, /…$/);
  assert.notEqual(shown, long);
});

test('long detail values are cut; the full value stays in the row title=""', () => {
  const sb = loadApp();
  const long = 'a'.repeat(150);
  const card = sb.renderProjectCard({ title: 'T', details: [{ label: 'log', value: long }] }, 'cards');
  const row = card.match(/<li class="project-card-detail"[^>]*title="([^"]*)"[^>]*>/);
  assert.equal(row[1], 'log: ' + long, 'full row value in the attribute');
  assert.match(card, /class="project-card-detail"[^>]*>log: a{99}…<\/li>/);
});

test('short values pass through untouched', () => {
  const sb = loadApp();
  const t1 = sb._projectsCardText('short');
  assert.equal(t1.text, 'short');
  assert.equal(t1.truncated, false);
  assert.equal(sb._projectsCardText('').text, '');
  assert.equal(sb._projectsCardText(null).text, '');
  const at = sb._projectsCardText('y'.repeat(100));
  assert.equal(at.truncated, false, 'the limit is inclusive');
});

// ── Badge: shipped / merged never get cancelled ────────────────────────────

test('a shipped item never renders a cancelled badge, even when one is asked for', () => {
  const sb = loadApp();
  for (const m of ['cards', 'tiles', 'compact']) {
    const html = sb.renderProjectCard({ title: 'S', status: 'shipped', badge: 'projects_badge_cancelled', badgeKind: 'cancelled' }, m);
    assert.ok(!html.includes('project-card-badge'), `no badge at all in ${m}`);
  }
  // Same rule for an explicit cancelled badge on merged work.
  const merged = sb.renderProjectCard({ title: 'M', status: 'merged', badge: 'projects_badge_cancelled', badgeKind: 'cancelled' }, 'cards');
  assert.ok(!merged.includes('project-card-badge'));
  // And via the explicit flag form (shipped flag, status said cancelled).
  const flag = sb.renderProjectCard({ title: 'F', status: 'cancelled', shipped: true, badgeKind: 'cancelled' }, 'cards');
  assert.ok(!flag.includes('project-card-badge'));
});

test('a non-shipped item keeps its cancelled badge', () => {
  const sb = loadApp();
  const html = sb.renderProjectCard({ title: 'X', badge: 'projects_badge_cancelled', badgeKind: 'cancelled' }, 'cards');
  assert.match(html, /class="project-card-badge project-card-badge-cancelled">cancelled<\/span>/);
});

test('status maps to a badge: shipped/merged ok, failed error', () => {
  const sb = loadApp();
  const shipped = sb.renderProjectCard({ title: 'S', status: 'shipped' }, 'cards');
  assert.match(shipped, /class="project-card-badge project-card-badge-ok">shipped<\/span>/);
  const merged = sb.renderProjectCard({ title: 'M', status: 'merged' }, 'cards');
  assert.match(merged, /class="project-card-badge project-card-badge-ok">merged<\/span>/);
  const failed = sb.renderProjectCard({ title: 'X', status: 'failed' }, 'cards');
  assert.match(failed, /class="project-card-badge project-card-badge-error">failed<\/span>/);
  const planned = sb.renderProjectCard({ title: 'P', status: 'planned' }, 'tiles');
  assert.match(planned, /class="project-card-badge project-card-badge-neutral">planned<\/span>/);
  assert.equal(sb._projectsCardBadgeInfo({ title: 'none' }), null, 'no status, no badge → none');
});

test('an explicit badge without a kind defaults to neutral; an unknown kind also lands there', () => {
  const sb = loadApp();
  const info = sb._projectsCardBadgeInfo({ badge: 'projects_badge_active' });
  assert.equal(info.key, 'projects_badge_active');
  assert.equal(info.kind, 'neutral');
  assert.equal(sb._projectsCardBadgeInfo({ badge: 'b', badgeKind: 'glow' }).kind, 'neutral');
});

// ── Age: relative, tolerant input; missing ages read as none ───────────────

test('relative age: just now, minutes, hours, days (ms, seconds and ISO inputs)', () => {
  const sb = loadApp();
  const at = (n) => sb._projectsCardAgeInfo(n, NOW);
  assert.equal(at(NOW - 30e3).unit, 'now');
  const mins = at(NOW - 5 * 60e3);
  assert.equal(mins.unit, 'minutes');
  assert.equal(mins.count, 5);
  const hrs = at(NOW - 2 * 3600e3);
  assert.equal(hrs.unit, 'hours');
  assert.equal(hrs.count, 2);
  const dys = at(NOW - 3 * 24 * 3600e3);
  assert.equal(dys.unit, 'days');
  assert.equal(dys.count, 3);
  assert.equal(at(Math.floor(NOW / 1000) - 60).unit, 'minutes', 'a seconds epoch reads as seconds');
  assert.equal(at(new Date(NOW - 3 * 24 * 3600e3).toISOString()).unit, 'days', 'ISO strings parse');
  assert.equal(sb._projectsCardAgeText(NOW - 30e3, NOW), 'just now');
  assert.equal(sb._projectsCardAgeText(NOW - 5 * 60e3, NOW), '5m ago');
  assert.equal(sb._projectsCardAgeText(NOW - 2 * 3600e3, NOW), '2h ago');
  assert.equal(sb._projectsCardAgeText(NOW - 3 * 24 * 3600e3, NOW), '3d ago');
});

test('falsy or unparseable ages render as none', () => {
  const sb = loadApp();
  for (const age of ['', null, undefined, 'nonsense']) {
    assert.equal(sb._projectsCardAgeInfo(age, NOW), null, String(age));
    const html = sb.renderProjectCard({ title: 'T', age }, 'cards');
    assert.ok(!html.includes('project-card-age'), `no age element for ${String(age)}`);
  }
  // A future timestamp is not "in the past" and not an error: it reads as now.
  assert.equal(sb._projectsCardAgeInfo(NOW + 10 * 3600e3, NOW).unit, 'now');
});

// ── Title link: safe URLs only ─────────────────────────────────────────────

test('only scheme-relative, site-relative and anchor links render as <a>', () => {
  const sb = loadApp();
  for (const safe of ['https://example.com/p', 'https://example.com/p?x=1', '/rel/path', '#anchor', '  https://ex.com  ']) {
    assert.match(sb.renderProjectCard({ title: 'T', link: safe }, 'cards'), /<a class="project-card-title"/, safe);
  }
  for (const bad of ['javascript:alert(1)', 'data:text/html,x', 'bareword', '']) {
    const html = sb.renderProjectCard({ title: 'T', link: bad }, 'cards');
    assert.ok(!html.includes('<a class="project-card-title"'), `not a link: ${bad}`);
  }
});

// ── Project records → card items, and the Overview section ────────────────

test('project records map to card items: dir / backend / description as details', () => {
  const sb = loadApp();
  const acme = {
    name: 'acme', dir: '/work/acme', description: 'the old one', default_backend: 'claude-code',
    policy: { max_concurrent_tasks: 3 }, arcs: [{ id: 'arc-a', title: 'First arc' }],
  };
  const items = sb._projectCardItemsFromList([null, undefined, { dir: '/n' }, acme]);
  assert.equal(items.length, 1, 'incomplete records are skipped');
  assert.equal(items[0].id, 'acme');
  assert.equal(items[0].title, 'acme');
  assert.equal(items[0].details.length, 3);
  assert.equal(items[0].details[0].label, 'Directory (absolute path)');
  assert.equal(items[0].details[0].value, '/work/acme');
  assert.equal(items[0].details[1].label, 'Default backend');
  assert.equal(items[0].details[1].value, 'claude-code');
  assert.equal(items[0].details[2], 'the old one', 'a bare description stays a string detail');
  assert.equal(sb._projectCardItemsFromList(undefined).length, 0);
  assert.equal(sb._projectCardItemsFromList({}).length, 0);
});

test('the Overview card section shows the toggle + cards, or nothing for an empty list', () => {
  const sb = loadApp();
  assert.equal(sb._projectsCardSectionHTML([]), '', 'empty list: the section is absent');
  const list = [{ name: 'acme', dir: '/work/acme', default_backend: 'claude-code' }];
  const html = sb._projectsCardSectionHTML(list);
  assert.match(html, /<div id="projectsCards" data-projects-cards="block"/);
  assert.match(html, /class="sessions-mode-toggle" role="group"/);
  assert.match(html, /data-project-id="acme"/);
});

// ── Locale: the new keys ship translated in all five bundles ──────────────

test('the Overview page paints the card section inside its body, under the picker, and a mode switch repaints it', async () => {
  const ACME = { name: 'acme', dir: '/work/acme', description: 'old', default_backend: 'claude-code' };
  const view = makeStubElement(() => {});
  view.__ins = [];
  view.insertAdjacentHTML = (_pos, h) => { view.__ins.push(h); };
  Object.defineProperty(view, 'page', { get() { return (view.__ins.length ? view.__ins[view.__ins.length - 1] : '') + view.innerHTML; } });
  const sb = buildSandbox({ byId: { automataPanel: view, view } });
  sb.__projects = [ACME];
  sb.fetch = (u) => {
    const body = String(u) === '/api/projects' ? sb.__projects : (String(u).includes('/history') ? { items: [], gantt: [] } : {});
    return Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(body), text: () => Promise.resolve('[]') });
  };
  vm.createContext(sb);
  loadScript(APP, sb);
  sb._i18n.bundle = EN;
  vm.runInContext("state.activeView = 'autonomous'; _automataState.tab = 'projects'", sb);
  await sb._projectsLoad();
  sb._projectsPaint();
  await flushAsync();
  const page = view.page;
  assert.ok(page.includes('id="projectsPicker"'), 'picker on top');
  assert.ok(page.includes('id="projectsOverviewBody"'), 'overview body present');
  assert.ok(page.includes('id="projectsCards" data-projects-cards="block"'), 'card section painted');
  assert.ok(page.includes('data-project-id="acme"'), 'the project appears as a card item');
  assert.ok(page.indexOf('id="projectsPicker"') < page.indexOf('id="projectsOverviewBody"'), 'picker is above the body');
  assert.ok(page.indexOf('id="projectsOverviewBody"') < page.indexOf('id="projectsCards"'), 'the section sits inside the body');
  assert.ok(page.includes('class="project-cards project-cards-cards"'), 'desktop first visit: cards mode');
  // Switching the mode repaints the same page in the new mode.
  assert.equal(sb.projectsSetViewMode('tiles'), true);
  await flushAsync();
  const page2 = view.page;
  assert.ok(page2.includes('class="project-cards project-cards-tiles" data-project-mode="tiles"'), 'tiles layout after the switch');
  assert.ok(page2.includes('data-project-id="acme"'), 'same item in tiles mode');
  assert.match(page2, /class="sessions-mode-btn active" [^>]*aria-label="Tiles"/);
});

test('all five bundles carry the new projects_* keys, translated (not the bare key)', () => {
  for (const loc of ['en', 'de', 'es', 'fr', 'ja']) {
    const o = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', loc + '.json'), 'utf8'));
    for (const k of [
      'projects_view_cards', 'projects_view_tiles', 'projects_view_compact', 'projects_view_toggle',
      'projects_age_now', 'projects_age_minutes', 'projects_age_hours', 'projects_age_days',
      'projects_badge_shipped', 'projects_badge_merged', 'projects_badge_active',
      'projects_badge_planned', 'projects_badge_failed', 'projects_badge_cancelled',
    ]) {
      assert.ok(typeof o[k] === 'string' && o[k].length > 0, `${loc} ${k} exists`);
      assert.notEqual(o[k], k, `${loc} ${k} is translated, not the bare key`);
    }
  }
  // Spot-check actual translations rather than copies of the English text.
  const de = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'de.json'), 'utf8'));
  assert.equal(de.projects_view_cards, 'Karten');
  const ja = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'ja.json'), 'utf8'));
  assert.equal(ja.projects_view_tiles, 'タイル');
  const fr = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'fr.json'), 'utf8'));
  assert.equal(fr.projects_badge_cancelled, 'annulé');
});
