// TS-821 — Automata tab tag browser. A three-level tag tree and four Automata are
// created through the API. The sections show subtree counts, an Automaton under two
// children is counted once under their parent, each tile carries its "also in:" line,
// a primary tag changes only the order inside a section, ANY and ALL matching work,
// a filtered list is one flat grid with the "No matches. Clear N filters" line, and at
// phone width the drill-down shows the breadcrumb, the back row and the URL hash.
//
// Runs against the sandbox daemon that scripts/e2e-pwa.sh starts (TEST_HTTP, TEST_TLS,
// TEST_TOKEN from lib.mjs). Every tag and Automaton is created inline and removed at
// the end; nothing is skipped for missing state.
import { runStory, connectToPWA, navigateTo, screenshot, saveLog, waitForSplash, env } from './lib.mjs';

// The sandbox serves a self-signed certificate; the API calls go to its TLS origin.
process.env.NODE_TLS_REJECT_UNAUTHORIZED = '0';

const RUN = Date.now().toString(36);
const nm = (base) => `${base} ${RUN}`;

function check(cond, msg) {
  if (!cond) throw new Error(msg);
}

async function api(method, path, body) {
  const res = await fetch(env.tls + path, {
    method,
    headers: { Authorization: `Bearer ${env.token}`, 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  if (!res.ok) throw new Error(`${method} ${path} -> HTTP ${res.status}: ${text.slice(0, 200)}`);
  return text ? JSON.parse(text) : null;
}

// ---- fixture ---------------------------------------------------------------
// work (root)
//   team
//     lane one
//     lane two
//   sales
// alpha: lane one + lane two (one Automaton under two children of team)
// beta:  lane one + sales, primary lane one
// gamma: sales
// delta: no tag (Untagged)
const fixture = { tags: {}, automata: {} };
const tagOrder = [];
const automataOrder = [];

async function createTags() {
  const make = async (key, name, parentId) => {
    const body = { name: nm(name) };
    if (parentId) body.parent_id = parentId;
    const t = await api('POST', '/api/autonomous/tags', body);
    check(t && t.id, `tag create ${name} returned no id`);
    fixture.tags[key] = t.id;
    tagOrder.push(t.id);
  };
  await make('work', 'Browse work');
  await make('team', 'Browse team', fixture.tags.work);
  await make('lane1', 'Browse lane one', fixture.tags.team);
  await make('lane2', 'Browse lane two', fixture.tags.team);
  await make('sales', 'Browse sales', fixture.tags.work);
}

async function createAutomata() {
  const make = async (key, title) => {
    const prd = await api('POST', '/api/autonomous/prds', { spec: `${nm(title)}: browse fixture`, project_dir: '/tmp', effort: 'low' });
    check(prd && prd.id, `automaton create ${title} returned no id`);
    fixture.automata[key] = { id: prd.id, title: nm(title) };
    automataOrder.push(prd.id);
  };
  await make('alpha', 'Browse item alpha');
  await make('beta', 'Browse item beta');
  await make('gamma', 'Browse item gamma');
  await make('delta', 'Browse item delta');
}

// setTags sends a tag set and an optional primary (undefined leaves it unchanged).
async function setTags(key, tagKeys, primaryKey) {
  const body = { tag_ids: tagKeys.map((k) => fixture.tags[k]) };
  if (primaryKey !== undefined) body.primary_tag_id = primaryKey === null ? '' : fixture.tags[primaryKey];
  return api('POST', `/api/autonomous/prds/${fixture.automata[key].id}/set_tags`, body);
}

async function applyBaseTags() {
  await setTags('alpha', ['lane1', 'lane2'], null);
  await setTags('beta', ['lane1', 'sales'], 'lane1');
  await setTags('gamma', ['sales'], null);
}

// aliasOf maps an Automaton id to its fixture name. A tile carries the full id in the
// title of its id code; the summary list has no title for a draft, so names are not used.
function aliasOf(id) {
  for (const [k, a] of Object.entries(fixture.automata)) if (a.id === id) return k;
  return id;
}

// ---- REST shapes the PWA depends on ----------------------------------------
async function assertRESTShapes() {
  const counts = await api('GET', '/api/autonomous/tags?counts=true');
  check(Array.isArray(counts.tags), 'counts=true reply has no tags array');
  check(counts.untagged && typeof counts.untagged.count === 'number', 'counts=true reply has no untagged.count');
  check(counts.registry && typeof counts.registry === 'object', 'counts=true reply has no registry object');
  const byId = Object.fromEntries(counts.tags.map((t) => [t.id, t]));
  for (const t of counts.tags) {
    for (const f of ['subtree_count', 'direct_count', 'session_subtree_count', 'session_direct_count']) {
      check(typeof t[f] === 'number', `tag ${t.name} has no numeric ${f}`);
    }
  }
  const want = {
    work: { subtree_count: 3, direct_count: 0 },
    team: { subtree_count: 2, direct_count: 0 },
    lane1: { subtree_count: 2, direct_count: 2 },
    lane2: { subtree_count: 1, direct_count: 1 },
    sales: { subtree_count: 2, direct_count: 2 },
  };
  for (const [key, exp] of Object.entries(want)) {
    const got = byId[fixture.tags[key]];
    check(got, `counts reply lacks the ${key} tag`);
    for (const [f, v] of Object.entries(exp)) {
      check(got[f] === v, `${key}.${f} = ${got[f]}, want ${v}`);
    }
  }
  // An Automaton under lane one and lane two is one item under team: the two child
  // counts add to three, team holds two.
  check(byId[fixture.tags.lane1].subtree_count + byId[fixture.tags.lane2].subtree_count > byId[fixture.tags.team].subtree_count,
    'an item under two children must be counted once under their parent');
  check(counts.untagged.count === 1, `untagged.count = ${counts.untagged.count}, want 1`);

  const list = await api('GET', '/api/autonomous/prds');
  const items = Array.isArray(list) ? list : (list.prds || []);
  const row = (id) => items.find((p) => p.id === id);
  const alpha = row(fixture.automata.alpha.id);
  const beta = row(fixture.automata.beta.id);
  check(alpha && Array.isArray(alpha.tags), 'automaton list items carry no tags array');
  check(alpha.tags.length === 2, `alpha lists ${alpha.tags.length} tags, want 2`);
  check(beta && beta.primary_tag_id === fixture.tags.lane1, `beta primary_tag_id = ${beta && beta.primary_tag_id}`);
  return { counts: counts.tags.length, items: items.length };
}

// ---- page helpers ----------------------------------------------------------
const PANEL = '#automataPanel';

async function openAutomataTags(page) {
  await navigateTo(page, 'autonomous');
  await page.evaluate(() => switchAutomataTab('automata'));
  await page.waitForFunction(() => {
    const b = document.querySelector('.automata-tabs-bar .output-tab[data-tab="automata"]');
    return !!b && b.classList.contains('active');
  }, null, { timeout: 10000 });
  // Draft Automata are not in the default active-only list, so the viewer turns on
  // the history toggle the way a user does before the fixture drafts are drawn.
  // The sandbox raises a one-off migration notice on connect; it sits over the tab
  // bar, so the viewer closes it first.
  for (const close of await page.locator('.toast-error-close').all()) {
    await close.click({ timeout: 2000 }).catch(() => {});
  }
  await page.waitForFunction(() => !document.querySelector('.toast-error-row'), null, { timeout: 5000 }).catch(() => {});
  await page.waitForSelector('#automataHistoryBtn', { state: 'attached', timeout: 15000 });
  const historyOn = await page.evaluate(() => !!document.getElementById('automataHistoryBtn')?.classList.contains('active'));
  if (!historyOn) await page.click('#automataHistoryBtn');
  await page.waitForFunction(() => document.getElementById('automataHistoryBtn')?.classList.contains('active'), null, { timeout: 5000 });
  await page.waitForSelector(`${PANEL} .tag-view-switch`, { state: 'attached', timeout: 15000 });
  await page.click(`button[onclick*="tagBrowseSetView('automata','tags')"]`);
  // The filter bar holds the tag facets; the viewer opens it with the header Filter button.
  const barOpen = await page.evaluate(() => document.getElementById('automataFilterBar')?.style.display === 'flex');
  if (!barOpen) await page.click('#automataFilterBtn');
  await page.waitForSelector(`${PANEL} .tag-sections, ${PANEL} .tag-drill-rows`, { state: 'attached', timeout: 10000 });
}

async function reloadAutomata(page) {
  await page.reload({ waitUntil: 'domcontentloaded' });
  await waitForSplash(page);
  await openAutomataTags(page);
}

// sectionKey: the tag key in the data-tag-section attribute, "automata:<id>" or
// "automata:_untagged".
// ownTiles lists the titles of the tiles drawn at one section's own level.
async function ownTiles(page, sectionId) {
  return page.evaluate(({ sel, id }) => {
    const d = document.querySelector(`${sel} details[data-tag-section="automata:${id}"]`);
    if (!d) return null;
    return [...d.querySelectorAll(':scope > .tag-section-body > .tag-section-items > .work-tile')]
      .map((t) => t.querySelector('.work-tile-id')?.getAttribute('title') || '');
  }, { sel: PANEL, id: sectionId }).then((ids) => ids.map(aliasOf));
}

async function sectionTitleCount(page, sectionId) {
  return page.evaluate(({ sel, id }) => {
    const d = document.querySelector(`${sel} details[data-tag-section="automata:${id}"]`);
    return d ? parseInt(d.querySelector(':scope > summary .tag-section-count').textContent, 10) : null;
  }, { sel: PANEL, id: sectionId });
}

async function alsoInLine(page, sectionId, alias) {
  return page.evaluate(({ sel, id, aid }) => {
    const d = document.querySelector(`${sel} details[data-tag-section="automata:${id}"]`);
    if (!d) return null;
    for (const t of d.querySelectorAll(':scope > .tag-section-body > .tag-section-items > .work-tile')) {
      if ((t.querySelector('.work-tile-id')?.getAttribute('title') || '') === aid) {
        const also = t.querySelector('.work-tile-also');
        return also ? also.textContent.replace(/\s+/g, ' ').trim() : '';
      }
    }
    return null;
  }, { sel: PANEL, id: sectionId, aid: fixture.automata[alias].id });
}

// flatTitles lists the tile titles of the flat grid (no sections drawn).
async function flatTitles(page) {
  const ids = await page.evaluate((sel) => [...document.querySelectorAll(`${sel} .work-tile-grid > .work-tile`)]
    .map((t) => t.querySelector('.work-tile-id')?.getAttribute('title') || ''), PANEL);
  return ids.map(aliasOf);
}

async function tagChecked(page, tagKey, on) {
  const open = await page.evaluate(() => !!document.querySelector('#tagFacets-automata details.tag-multi[open]'));
  if (!open) await page.click('#tagFacets-automata details.tag-multi > summary');
  const box = page.locator(`#tagFacets-automata input[type=checkbox][value="${fixture.tags[tagKey]}"]`);
  if ((await box.isChecked()) !== on) await box.click();
}

async function modeLabel(page) {
  return (await page.textContent('#tagFacets-automata .tag-mode-btn')).trim();
}

async function setMode(page, want) {
  if ((await modeLabel(page)) !== want) await page.click('#tagFacets-automata .tag-mode-btn');
  await page.waitForFunction((w) => {
    const b = document.querySelector('#tagFacets-automata .tag-mode-btn');
    return !!b && b.textContent.trim() === w;
  }, want, { timeout: 5000 });
}

async function showingCounts(page) {
  const text = await page.textContent(`${PANEL} .tag-showing`);
  const m = /Showing (\d+) of (\d+)/.exec(text || '');
  check(m, `no "Showing X of Y" line, got: ${JSON.stringify(text)}`);
  return { shown: Number(m[1]), of: Number(m[2]) };
}

function sameSet(a, b) {
  const x = [...a].sort();
  const y = [...b].sort();
  return x.length === y.length && x.every((v, i) => v === y[i]);
}

const SECTION_KEYS = ['work', 'team', 'lane1', 'lane2', 'sales'];

async function snapshotSections(page) {
  const out = {};
  for (const key of SECTION_KEYS) out[key] = await ownTiles(page, fixture.tags[key]);
  out._untagged = await ownTiles(page, '_untagged');
  return out;
}

// ---- story -----------------------------------------------------------------
await runStory(async (page) => {
  let restRes = null;
  try {
    await createTags();
    await createAutomata();
    await applyBaseTags();
    restRes = await assertRESTShapes();
    await saveLog('rest', JSON.stringify(restRes));

    await connectToPWA(page);
    await openAutomataTags(page);

    // --- sections and subtree counts -----------------------------------------
    const expectCounts = { work: 3, team: 2, lane1: 2, lane2: 1, sales: 2 };
    for (const [key, want] of Object.entries(expectCounts)) {
      const got = await sectionTitleCount(page, fixture.tags[key]);
      check(got === want, `section ${key} shows ${got}, want ${want}`);
    }
    check((await sectionTitleCount(page, '_untagged')) === 1, 'Untagged section does not show 1');
    await screenshot(page, '01-sections');

    // The sections draw each Automaton under every branch that holds it, so alpha
    // is drawn under lane one and lane two; alpha is not drawn under team itself.
    const snap = await snapshotSections(page);
    const want = {
      work: [], team: [],
      lane1: ['beta', 'alpha'].sort(),
      lane2: ['alpha'],
      sales: ['beta', 'gamma'].sort(),
      _untagged: ['delta'],
    };
    // beta carries primary lane one, so it leads lane one; the sorted compare below
    // checks membership, the order is checked separately.
    for (const key of Object.keys(want)) {
      check(sameSet(snap[key] || [], want[key]), `section ${key} holds ${JSON.stringify(snap[key])}, want ${JSON.stringify(want[key])}`);
    }
    check(snap.lane1[0] === 'beta', `primary lane one must lead the lane one section, got ${JSON.stringify(snap.lane1)}`);

    // --- also in: line --------------------------------------------------------
    const alsoAlpha1 = await alsoInLine(page, fixture.tags.lane1, 'alpha');
    check(alsoAlpha1 && alsoAlpha1.includes('also in:') && alsoAlpha1.includes(nm('Browse lane two')),
      `alpha under lane one shows "${alsoAlpha1}", want "also in:" naming lane two`);
    check(alsoAlpha1 && !alsoAlpha1.includes(nm('Browse lane one')) && !alsoAlpha1.includes(nm('Browse team')),
      `alpha's also-in line repeats the section it sits in: "${alsoAlpha1}"`);
    const alsoBetaSales = await alsoInLine(page, fixture.tags.sales, 'beta');
    check(alsoBetaSales && alsoBetaSales.includes(nm('Browse lane one')), `beta under sales shows "${alsoBetaSales}", want lane one named`);
    await saveLog('also-in', JSON.stringify({ alphaUnderLaneOne: alsoAlpha1, betaUnderSales: alsoBetaSales }));

    // --- primary tag changes only the order ----------------------------------
    const before = await snapshotSections(page);
    await setTags('beta', ['lane1', 'sales'], null);
    await setTags('alpha', ['lane1', 'lane2'], 'lane1');
    await reloadAutomata(page);
    const after = await snapshotSections(page);
    for (const key of Object.keys(before)) {
      check(sameSet(before[key] || [], after[key] || []), `primary change moved membership in ${key}: ${JSON.stringify(before[key])} -> ${JSON.stringify(after[key])}`);
    }
    check(after.lane1[0] === 'alpha', `after the primary moved to alpha, lane one leads with alpha, got ${JSON.stringify(after.lane1)}`);
    check(JSON.stringify(before.lane1) !== JSON.stringify(after.lane1), 'the primary tag change did not reorder lane one');
    await setTags('alpha', ['lane1', 'lane2'], null);
    await setTags('beta', ['lane1', 'sales'], 'lane1');
    await reloadAutomata(page);

    // --- ANY then ALL --------------------------------------------------------
    const total = restRes.items;
    await setMode(page, 'match ANY');
    await tagChecked(page, 'lane1', true);
    await tagChecked(page, 'lane2', true);
    await page.waitForSelector(`${PANEL} .tag-showing`, { state: 'attached', timeout: 10000 });
    const anyTitles = await flatTitles(page);
    check(sameSet(anyTitles, ['alpha', 'beta']),
      `ANY with lane one and lane two shows ${JSON.stringify(anyTitles)}, want alpha and beta`);
    const anyCounts = await showingCounts(page);
    check(anyCounts.shown === 2 && anyCounts.of === total, `ANY note reads ${anyCounts.shown} of ${anyCounts.of}, want 2 of ${total}`);
    check((await page.locator(`${PANEL} .tag-sections`).count()) === 0, 'a filtered list still draws the sections');
    await screenshot(page, '02-any-flat');

    await setMode(page, 'match ALL');
    await page.waitForFunction(() => /Showing 1 of/.test(document.querySelector('#automataPanel .tag-showing')?.textContent || ''), null, { timeout: 5000 });
    const allTitles = await flatTitles(page);
    check(sameSet(allTitles, ['alpha']), `ALL with lane one and lane two shows ${JSON.stringify(allTitles)}, want alpha only`);
    await screenshot(page, '03-all-flat');

    // --- no matches ----------------------------------------------------------
    await tagChecked(page, 'lane1', false);
    await tagChecked(page, 'sales', true);
    const noMatchBtn = page.locator(`${PANEL} .tag-no-matches-btn`);
    await noMatchBtn.waitFor({ state: 'visible', timeout: 10000 });
    const noMatchText = (await noMatchBtn.textContent()).trim();
    check(noMatchText === 'No matches. Clear 2 filters', `no-matches line reads "${noMatchText}", want "No matches. Clear 2 filters"`);
    await screenshot(page, '04-no-matches');
    await noMatchBtn.click();
    await page.waitForSelector(`${PANEL} .tag-sections`, { state: 'attached', timeout: 10000 });
    check((await page.locator(`${PANEL} .tag-no-matches-btn`).count()) === 0, 'Clear did not remove the no-matches line');
    await setMode(page, 'match ANY');

    // --- phone-width drill-down ----------------------------------------------
    await page.setViewportSize({ width: 390, height: 844 });
    await page.waitForSelector(`${PANEL} .tag-drill-rows`, { state: 'attached', timeout: 10000 });
    const row = (name) => page.locator(`${PANEL} .tag-drill-row`).filter({ hasText: name }).first();
    const crumbs = () => page.$$eval(`${PANEL} .tag-crumbs .tag-crumb`, (els) => els.map((e) => e.textContent.trim()));
    const hash = () => page.evaluate(() => location.hash);
    const waitHash = (h) => page.waitForFunction((want) => location.hash === want, h, { timeout: 5000 });

    check((await crumbs()).length === 1, 'drill-down root shows more than the root crumb');
    await row(nm('Browse work')).click();
    await waitHash(`#automata/tags/${encodeURIComponent(fixture.tags.work)}`);
    check(JSON.stringify(await crumbs()) === JSON.stringify(['Tags', nm('Browse work')]), `crumbs after entering work: ${JSON.stringify(await crumbs())}`);
    check((await page.locator(`${PANEL} .tag-drill-back`).count()) === 1, 'no back row under a tag');
    const teamCount = parseInt(await row(nm('Browse team')).locator('.tag-section-count').textContent(), 10);
    check(teamCount === 2, `team row count is ${teamCount}, want 2`);
    await screenshot(page, '05-drill-work');

    await row(nm('Browse team')).click();
    await waitHash(`#automata/tags/${encodeURIComponent(fixture.tags.work)}/${encodeURIComponent(fixture.tags.team)}`);
    check(JSON.stringify(await crumbs()) === JSON.stringify(['Tags', nm('Browse work'), nm('Browse team')]),
      `three-level crumbs: ${JSON.stringify(await crumbs())}`);
    const drillTitles = (await page.$$eval(`${PANEL} .work-tile-grid .work-tile-id`, (els) => els.map((e) => e.getAttribute('title') || ''))).map(aliasOf);
    check(sameSet(drillTitles, ['alpha', 'beta']),
      `team drill-down lists ${JSON.stringify(drillTitles)}, want alpha and beta once each`);
    check((await page.locator(`${PANEL} .tag-sections`).count()) === 0, 'phone width draws the sections');
    await screenshot(page, '06-drill-team');

    await page.click(`${PANEL} .tag-drill-back`);
    await waitHash(`#automata/tags/${encodeURIComponent(fixture.tags.work)}`);
    check(JSON.stringify(await crumbs()) === JSON.stringify(['Tags', nm('Browse work')]), 'back row did not return one level');

    await page.locator(`${PANEL} .tag-crumb`).first().click();
    await waitHash('#automata/tags');
    check(JSON.stringify(await crumbs()) === JSON.stringify(['Tags']), 'root crumb did not return to the top level');
    check((await hash()) === '#automata/tags', 'root crumb left a path in the hash');

    await page.setViewportSize({ width: 1280, height: 800 });

    await saveLog('result', [
      `counts: work 3, team 2, lane one 2, lane two 1, sales 2, untagged 1`,
      `rest shapes: ${JSON.stringify(restRes)}`,
      `ANY ${JSON.stringify(anyTitles)}, ALL ${JSON.stringify(allTitles)}`,
      `no-matches line: "${noMatchText}"`,
      `phone drill-down: breadcrumb, back row and hash checked`,
    ].join('\n'));
  } finally {
    // Remove what this story created: Automata first (clearing their tags), then tags leaves first.
    for (const a of Object.values(fixture.automata)) {
      await api('POST', `/api/autonomous/prds/${a.id}/set_tags`, { tag_ids: [] }).catch(() => {});
      await api('DELETE', `/api/autonomous/prds/${a.id}`).catch((e) => console.error('cleanup automaton:', e.message));
    }
    for (const id of [...tagOrder].reverse()) {
      await api('DELETE', `/api/autonomous/tags/${id}`).catch((e) => console.error('cleanup tag:', e.message));
    }
  }
});
