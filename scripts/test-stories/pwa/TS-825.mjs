// TS-825 — the header Tags dropdown: one control holds the watched-only switch,
// the session tag filter and, on a session detail, that session's own watch and
// tag actions. The detail header itself carries no last-activity element and no
// watch button any more.
//
// Scenario: on a session detail there is no session-last-activity element and no
// watch button outside the dropdown; the Tags dropdown shows an empty count;
// Watched only narrows the session list and counts 1; a tag checkbox narrows the
// list to the tagged session; on the tagged session's detail the This session
// section offers Watch this session and Edit tags of this session; at a 360px
// viewport the open dropdown causes no horizontal overflow; an outside click and
// the Escape key both close it; every filter is reset at the end.
//
// Runs against the sandbox daemon that e2e-pwa.sh starts (TEST_HTTP / TEST_TLS
// from the environment), never the production daemon. Everything the story needs
// is created inline through the API: two tags and two shell sessions, one
// carrying a tag and one not. Nothing is skipped for missing state, and
// everything created is removed at the end (and again in the finally block if a
// step failed part way).
import { mkdtemp, rm } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { runStory, connectToPWA, navigateTo, screenshot, saveLog, env } from './lib.mjs';

// The daemon only starts a session in a directory under its allowed project roots,
// and the sandbox's own data dir is not one. The session therefore gets a scratch
// directory next to this file, removed with the other fixtures.
const here = path.dirname(fileURLToPath(import.meta.url));

const TAG = (n) => `TS825 ${n} ${Date.now().toString(36)}`;

// api(method, path, body) calls the sandbox over plain HTTP with the bearer
// token, which is how the sibling stories reach the daemon's REST surface.
async function api(method, p, body) {
  const r = await fetch(env.http + p, {
    method,
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${env.token}` },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await r.text();
  let parsed = text;
  try { parsed = text ? JSON.parse(text) : null; } catch { /* plain-text error body */ }
  return { status: r.status, body: parsed };
}

function mustOK(res, what) {
  if (res.status < 200 || res.status > 299) {
    throw new Error(`${what}: HTTP ${res.status} ${typeof res.body === 'string' ? res.body.slice(0, 200) : JSON.stringify(res.body).slice(0, 200)}`);
  }
  return res.body;
}

async function listSessions() {
  const list = mustOK(await api('GET', '/api/sessions'), 'list sessions');
  return Array.isArray(list) ? list : (list.sessions || []);
}

async function listTags() {
  const res = mustOK(await api('GET', '/api/autonomous/tags'), 'list tags');
  return Array.isArray(res) ? res : (res.tags || []);
}

// clickDom fires the click on the element itself. Toasts from earlier config
// migrations can sit over the header and would intercept a pointer click.
async function clickDom(page, selector) {
  await page.evaluate((sel) => document.querySelector(sel).click(), selector);
}

// shownCards lists the full IDs of the session cards currently in the list.
async function shownCards(page) {
  return page.$$eval('.session-card', (els) => els.map((e) => e.dataset.fullId));
}

// waitForCards waits until the list shows every id in `present` and none in `absent`.
async function waitForCards(page, present, absent, label) {
  try {
    await page.waitForFunction(
      ({ present, absent }) => {
        const ids = Array.from(document.querySelectorAll('.session-card')).map((e) => e.dataset.fullId);
        return present.every((i) => ids.includes(i)) && absent.every((i) => !ids.includes(i));
      },
      { present, absent },
      { timeout: 10000 },
    );
  } catch {
    throw new Error(`${label}: session list shows ${JSON.stringify(await shownCards(page))}, wanted ${JSON.stringify(present)} present and ${JSON.stringify(absent)} absent`);
  }
}

const countText = (page) => page.innerText('#headerTagsCount');
const menuOpen = (page) => page.evaluate(() => {
  const m = document.getElementById('headerTagsMenu');
  return !!m && !m.hidden;
});

async function openMenu(page) {
  if (!(await menuOpen(page))) await clickDom(page, '#headerTagsBtn');
  await page.waitForSelector('#headerTagsMenu', { state: 'visible', timeout: 10000 });
}

async function waitMenuClosed(page, label) {
  try {
    await page.waitForFunction(() => {
      const m = document.getElementById('headerTagsMenu');
      const b = document.getElementById('headerTagsBtn');
      return m.hidden && b.getAttribute('aria-expanded') === 'false';
    }, null, { timeout: 5000 });
  } catch {
    throw new Error(`${label}: the Tags dropdown is still open`);
  }
}

const created = { sessions: [], tags: [], dirs: [] };

// cleanup removes sessions before tags: a tag a session still carries cannot be deleted.
async function cleanup() {
  for (const id of created.sessions.splice(0).reverse()) {
    await api('POST', '/api/sessions/delete', { id }).catch(() => {});
  }
  for (const id of created.tags.splice(0).reverse()) {
    await api('DELETE', `/api/autonomous/tags/${encodeURIComponent(id)}`).catch(() => {});
  }
  for (const d of created.dirs.splice(0)) await rm(d, { recursive: true, force: true }).catch(() => {});
}

await runStory(async (page) => {
  const log = [];
  try {
    // --- (1) inline fixtures: two tags, a tagged session and an untagged one ---
    const tagOne = mustOK(await api('POST', '/api/autonomous/tags', { name: TAG('one') }), 'create tag one');
    created.tags.push(tagOne.id);
    const tagTwo = mustOK(await api('POST', '/api/autonomous/tags', { name: TAG('two') }), 'create tag two');
    created.tags.push(tagTwo.id);

    const projectDir = await mkdtemp(path.join(here, '.ts825-'));
    created.dirs.push(projectDir);
    const startBody = (name) => ({
      task: 'ts825-tags-menu-fixture', name, backend: 'shell', project_dir: projectDir, effort: 'quick',
    });
    const tagged = mustOK(await api('POST', '/api/sessions/start', startBody('ts825-tagged')), 'start tagged session');
    const idTagged = tagged.full_id || tagged.id;
    created.sessions.push(idTagged);
    const plain = mustOK(await api('POST', '/api/sessions/start', startBody('ts825-untagged')), 'start untagged session');
    const idPlain = plain.full_id || plain.id;
    created.sessions.push(idPlain);
    mustOK(await api('POST', '/api/sessions/tags', { id: idTagged, tag_ids: [tagOne.id] }), 'tag the tagged session');

    const sessions = await listSessions();
    const restTagged = sessions.find((s) => s.full_id === idTagged || s.id === idTagged);
    const restPlain = sessions.find((s) => s.full_id === idPlain || s.id === idPlain);
    if (!restTagged || !restPlain) throw new Error('a fixture session is missing from the sessions list');
    if (!(restTagged.tags || []).includes(tagOne.id)) throw new Error(`tagged session carries ${JSON.stringify(restTagged.tags)}, wanted ${tagOne.id}`);
    if ((restPlain.tags || []).length !== 0) throw new Error(`untagged session carries ${JSON.stringify(restPlain.tags)}`);
    log.push('REST: two tags, one tagged and one untagged session created');

    // --- (2) a session detail: no last-activity element, no header watch button ---
    await connectToPWA(page);
    await navigateTo(page, 'sessions');
    await waitForCards(page, [idTagged, idPlain], [], 'list before any filter');
    await page.evaluate((id) => window.navigate('session-detail', id), idPlain);
    await page.waitForSelector('.detail-state-badge', { state: 'visible', timeout: 20000 });
    const lastActivity = await page.locator('.session-last-activity, [id*="last-activity"], [data-last-activity]').count();
    if (lastActivity !== 0) throw new Error(`session detail has ${lastActivity} last-activity element(s), expected none`);
    // Any watch control in the page other than the ones inside the dropdown.
    const watchControls = await page.evaluate(() => Array.from(document.querySelectorAll('button, [role="button"], a'))
      .filter((el) => !el.closest('#headerTagsMenu'))
      .filter((el) => /toggleSessionWatch\b/.test(el.getAttribute('onclick') || '') || /watch/i.test(el.id || '') || (el.closest('header') && /watch/i.test(el.className || '')))
      .map((el) => el.outerHTML.slice(0, 120)));
    if (watchControls.length !== 0) throw new Error(`session detail still has a watch button outside the dropdown: ${watchControls.join(' | ')}`);
    await screenshot(page, 'session-detail-header');
    log.push('PWA: session detail has no session-last-activity element and no watch button outside the Tags dropdown');

    // --- (3) the Tags dropdown and its count ---
    await page.evaluate(() => window.navigate('sessions')); // the bottom nav is hidden on a detail view
    await waitForCards(page, [idTagged, idPlain], [], 'list after returning from the detail');
    await openMenu(page);
    if ((await countText(page)).trim() !== '') throw new Error(`Tags count with no filter should be empty, got "${await countText(page)}"`);
    const ariaIdle = await page.getAttribute('#headerTagsBtn', 'aria-label');
    if (ariaIdle !== 'Tags') throw new Error(`idle Tags button aria-label should be "Tags", got "${ariaIdle}"`);
    const rowsOne = await page.locator(`#headerTagsMenu .tag-multi-row input[value="${tagOne.id}"]`).count();
    const rowsTwo = await page.locator(`#headerTagsMenu .tag-multi-row input[value="${tagTwo.id}"]`).count();
    if (rowsOne !== 1 || rowsTwo !== 1) throw new Error(`the dropdown should list both tags once, got ${rowsOne} and ${rowsTwo}`);
    if ((await page.innerText('#headerTagsMenu')).includes('This session')) throw new Error('the This session section shows on the session list, where no session is open');
    log.push('PWA: Tags dropdown opens, count empty with no filter, both tags listed, no This session section on the list');

    // --- (4) Watched only narrows the list. The untagged session is watched first,
    //         so the filter has something to keep. ---
    await page.evaluate((id) => window.toggleSessionWatch(id), idPlain);
    await page.waitForSelector('#headerTagsMenu .tags-menu-row input', { state: 'visible', timeout: 5000 });
    await clickDom(page, '#headerTagsMenu .tags-menu-row input');
    await waitForCards(page, [idPlain], [idTagged], 'Watched only on');
    await page.waitForFunction(() => document.getElementById('headerTagsCount').textContent.trim() === '1', null, { timeout: 5000 });
    log.push('PWA: Watched only keeps just the watched session, count shows 1');
    await clickDom(page, '#headerTagsMenu .tags-menu-row input');
    await waitForCards(page, [idPlain, idTagged], [], 'Watched only off');
    await page.waitForFunction(() => document.getElementById('headerTagsCount').textContent.trim() === '', null, { timeout: 5000 });
    await page.evaluate((id) => window.toggleSessionWatch(id), idPlain);

    // --- (5) a tag checkbox narrows the list to the tagged session ---
    await clickDom(page, `#headerTagsMenu .tag-multi-row input[value="${tagOne.id}"]`);
    await waitForCards(page, [idTagged], [idPlain], 'tag one ticked');
    await page.waitForFunction(() => document.getElementById('headerTagsCount').textContent.trim() === '1', null, { timeout: 5000 });
    log.push('PWA: ticking the tag leaves only the tagged session in the list, count shows 1');

    // --- (6) the tagged session's detail: the This session section ---
    await clickDom(page, '#headerTagsBtn'); // close
    await waitMenuClosed(page, 'closing from the button');
    await page.evaluate((id) => window.navigate('session-detail', id), idTagged);
    await page.waitForSelector('.detail-state-badge', { state: 'visible', timeout: 20000 });
    await openMenu(page);
    await page.waitForFunction(() => /This session/.test(document.getElementById('headerTagsMenu').innerText), null, { timeout: 5000 });
    const buttons = await page.$$eval('#headerTagsMenu .tags-menu-btn', (els) => els.map((e) => e.textContent.trim()));
    for (const want of ['Watch this session', 'Edit tags of this session']) {
      if (!buttons.includes(want)) throw new Error(`This session section lacks "${want}" (buttons: ${JSON.stringify(buttons)})`);
    }
    await screenshot(page, 'session-tags-menu');
    log.push('PWA: the This session section offers Watch this session and Edit tags of this session');

    // --- (7) 360px wide, dropdown open: no horizontal overflow ---
    await page.setViewportSize({ width: 360, height: 800 });
    await page.waitForFunction(() => window.innerWidth === 360, null, { timeout: 5000 });
    await openMenu(page);
    const widths = await page.evaluate(() => ({
      scroll: document.documentElement.scrollWidth,
      client: document.documentElement.clientWidth,
    }));
    if (widths.scroll > widths.client) throw new Error(`horizontal overflow at 360px with the dropdown open: scrollWidth ${widths.scroll} > clientWidth ${widths.client}`);
    log.push(`PWA: at 360px with the dropdown open scrollWidth ${widths.scroll} <= clientWidth ${widths.client}`);
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.waitForFunction(() => window.innerWidth === 1280, null, { timeout: 5000 });

    // --- (8) closing: an outside click, then Escape ---
    await openMenu(page);
    await page.mouse.click(6, 790); // bottom-left corner: outside both the button and the panel
    await waitMenuClosed(page, 'outside click');
    log.push('PWA: an outside click closes the dropdown');
    await openMenu(page);
    await page.keyboard.press('Escape');
    await waitMenuClosed(page, 'Escape');
    log.push('PWA: Escape closes the dropdown');

    // --- (9) reset every filter, then remove what the story created ---
    await page.evaluate(() => window.navigate('sessions'));
    await page.waitForSelector('.session-card', { state: 'visible', timeout: 10000 });
    await openMenu(page);
    await page.waitForSelector('#headerTagsMenu .tags-menu-btn:has-text("Clear")', { state: 'visible', timeout: 5000 });
    await page.evaluate(() => window.tagBrowseClear('session'));
    await page.waitForFunction(() => document.getElementById('headerTagsCount').textContent.trim() === '', null, { timeout: 5000 });
    const leftover = await page.evaluate(() => ({
      watchFilter: !!state.sessionWatchFilter,
      selected: _tagBrowse.session.sel.length,
      watched: Array.from(state.watchedSessions),
    }));
    if (leftover.watchFilter || leftover.selected !== 0 || leftover.watched.length !== 0) {
      throw new Error(`filters not reset: ${JSON.stringify(leftover)}`);
    }
    await page.keyboard.press('Escape');
    await waitMenuClosed(page, 'closing after the reset');

    await cleanup();
    const sessionsAfter = (await listSessions()).map((s) => s.full_id || s.id);
    const tagsAfter = (await listTags()).map((t) => t.id);
    for (const id of [idTagged, idPlain]) {
      if (sessionsAfter.includes(id)) throw new Error(`session ${id} still exists after cleanup`);
    }
    for (const id of [tagOne.id, tagTwo.id]) {
      if (tagsAfter.includes(id)) throw new Error(`tag ${id} still exists after cleanup`);
    }
    log.push('Cleanup: filters reset, both sessions and both tags removed');

    await saveLog('result', log.join('\n'));
    console.log(log.map((l) => `  ${l}`).join('\n'));
  } catch (err) {
    await saveLog('result', `${log.join('\n')}\nFAILED: ${err.message}`).catch(() => {});
    throw err;
  } finally {
    await cleanup();
  }
});
