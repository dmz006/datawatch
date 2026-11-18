#!/usr/bin/env node
/**
 * Projects cards story — the Projects view draws its project list with the
 * shared Cards / Tiles / Compact renderer, remembers the choice on this
 * device and defaults phone widths to compact; the selected project's
 * Unassigned board offers Assign / Dismiss / Undo per Automaton; and the
 * former Policy sub-tab is gone (its settings link sits in the header row).
 * tags: surface:pwa feature:projects-cards conflict:pwa-pwa
 *
 * Runs against the runner's sandbox daemon (never the production daemon).
 * The list and the roll-up of the project 'acme' are stubbed in the browser
 * so the story works on any fixture daemon; everything else (the project
 * links reads, the toasts) is the app's own code. Flow:
 *   1. open the Projects view on a phone width (390x844); the card section
 *      renders the project cards with the mode toggle;
 *   2. phone width defaults to the compact mode; switching to tiles and then
 *      to cards re-renders the same items and is remembered in localStorage
 *      (cs_projects_view_mode);
 *   3. select the project 'acme'; the sub-tab strip has exactly the live set
 *      (overview, plans, records, gantt, unassigned, history) — no Policy tab, and a
 *      stored 'policy' value falls back to Overview;
 *   4. the Unassigned tab lists the Automaton from the roll-up stub with
 *      Assign / Open / Dismiss; Dismiss hides it (remembered in
 *      cs_projects_unassigned_hidden) and the Undo note brings it back;
 *   5. Assign asks for a project name and saves it with POST
 *      /api/autonomous/prds/{id}/set_project;
 *   6. the view never scrolls sideways at phone width.
 */
import { screenshot } from './lib.mjs';
import {
  runPerfStory, openPWA, check, saveJson,
  noHorizontalOverflow, fulfillEndpoint,
} from './perf-lib.mjs';

const PHONE = { width: 390, height: 844 };
const VIEW_MODE_KEY = 'cs_projects_view_mode';
const UNASSIGNED_KEY = 'cs_projects_unassigned_hidden';
const SUBTAB_KEY = 'cs_projects_subtab';

const PROJECTS = [{ name: 'acme', dir: 'srv/web' }];
const ROLLUP = {
  arcs: [],
  unassigned: { automata: [{ id: 'prdx', title: 'Stray builder', status: 'active', project_dir: 'srv/web' }] },
  without_project: { automata: [] },
};

await runPerfStory(async (defaultPage) => {
  const browser = defaultPage.context().browser();
  const context = await browser.newContext({
    ignoreHTTPSErrors: true, viewport: PHONE, locale: 'en-US', isMobile: true, hasTouch: true,
  });
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', (err) => errors.push(`pageerror: ${err.message}`));

  let summary = { viewMode: '', subtab: '', dismissed: '', undo: '', assignCalls: 0 };
  try {
    await openPWA(page, { timeout: 60000 });

    // Stub the list and the roll-up so the story is fixture-independent.
    // GET /api/projects answers the project records as a bare array.
    const listCtl = await fulfillEndpoint(page, '/api/projects', PROJECTS, {});
    const rollupCtl = await fulfillEndpoint(page, '/api/projects/acme/rollup', ROLLUP, {});
    const probe = await page.evaluate(async () => {
        const r = await fetch('/api/projects', { credentials: 'same-origin' }).catch((e) => ({ status: 0, err: String(e) }));
        let body = null;
        try { body = r.status ? await r.json().catch(() => null) : { err: r.err }; } catch {}
        return { status: r.status || 0, body };
      });
      if (probe.status === 404 || probe.status === 0) {
        console.error('SKIP: the daemon has no /api/projects; start a sandbox daemon built from this tree');
        process.exitCode = 2;
        return;
      }
      check(Array.isArray(probe.body),
        `GET /api/projects answers a list (status ${probe.status}: ${JSON.stringify(probe.body).slice(0, 120)})`);
      const projectsOk = Array.isArray(probe.body) ? probe.body : (probe.body && probe.body.projects) || [];
      check(projectsOk.length >= 1 && projectsOk[0] && projectsOk[0].name,
        `GET /api/projects returns project records with a name (got: ${JSON.stringify(probe.body).slice(0, 120)})`);

    // 1. Open the Projects view; the card section appears with the toggle.
    await page.evaluate(() => { window.navigate('projects'); });    await page.waitForSelector('#projectsCards', { state: 'attached', timeout: 60000 });
    await page.waitForSelector('#projectsCards .sessions-mode-toggle .sessions-mode-btn', { state: 'attached', timeout: 10000 });
    const modeButtons = await page.$$eval('#projectsCards .sessions-mode-toggle .sessions-mode-btn', (bs) =>
      bs.map((b) => b.getAttribute('aria-label')));
    check(modeButtons.includes('Cards') && modeButtons.includes('Tiles') && modeButtons.includes('Compact'),
      `mode toggle has the three modes (got: ${modeButtons.join(', ')})`);
    check((await page.$$('#projectsCards .project-card')).length >= 1, 'at least one project card renders');

    // 2. Phone width defaults to compact (viewport-inferred, not stored); a
    //    deliberate switch remembers the choice in localStorage.
    const storedBefore = await page.evaluate((k) => localStorage.getItem(k), VIEW_MODE_KEY);
    check(storedBefore === null,
      `first visit stores no mode yet (got: ${JSON.stringify(storedBefore)})`);
    const activeModes = await page.$$eval('#projectsCards .sessions-mode-toggle .sessions-mode-btn.active', (bs) =>
      bs.map((b) => b.getAttribute('aria-label')));
    check(activeModes.join(', ') === 'Compact',
      `phone width (390) defaults to the compact mode (active: ${activeModes.join(', ')})`);
    check((await page.$$('#projectsCards .project-card-compact')).length >= 1,
      'compact mode draws compact cards');

    const clickMode = async (label) => {
      await page.evaluate((l) => {
        const b = [...document.querySelectorAll('#projectsCards .sessions-mode-toggle .sessions-mode-btn')]
          .find((x) => x.getAttribute('aria-label') === l);
        if (b) b.click();
      }, label);
    };
    await clickMode('Tiles');
    await page.waitForFunction((k) => localStorage.getItem(k) === 'tiles', VIEW_MODE_KEY, { timeout: 5000 });
    summary.viewMode = 'tiles';
    check((await page.$$('#projectsCards .project-card-tile')).length >= 1, 'tiles mode draws tile cards');

    await clickMode('Cards');
    await page.waitForFunction((k) => localStorage.getItem(k) === 'cards', VIEW_MODE_KEY, { timeout: 5000 });
    const cardCount = (await page.$$('#projectsCards .project-card')).length;
    check(cardCount >= 1, 'cards mode draws full cards');
    await screenshot(page, 'projects-modes');

    // 3. Select the project; the live sub-tab set has no Policy tab, and a
    //    stored 'policy' value falls back to Overview.
    await page.evaluate((k) => localStorage.setItem(k, 'policy'), SUBTAB_KEY);
    await page.evaluate(() => {
      const sel = document.getElementById('projectsPicker');
      if (sel) { sel.value = 'acme'; sel.dispatchEvent(new Event('change', { bubbles: true })); }
      else if (typeof window.selectProjectsView === 'function') window.selectProjectsView('acme');
    });
    let active = await page.waitForFunction(() => {
      const el = document.getElementById('projectsSubtabBody');
      return el ? el.getAttribute('data-projects-subtab-body') : '';
    }, null, { timeout: 60000, polling: 50 }).then((h) => h.jsonValue());
    check(active === 'overview', `stored 'policy' sub-tab falls back to overview (got: ${active})`);
    summary.subtab = active;
    await screenshot(page, 'projects-accents');

    const stripTabs = await page.$$eval('.project-detail-card .projects-subtab', (bs) =>
      bs.map((b) => b.getAttribute('data-projects-subtab')));
    check(!stripTabs.includes('policy'), `the Policy sub-tab is gone (got: ${stripTabs.join(', ')})`);
    for (const k of ['overview', 'plans', 'records', 'gantt', 'unassigned', 'history']) {
      check(stripTabs.includes(k), `the live sub-tab set has "${k}" (got: ${stripTabs.join(', ')})`);
    }

    // 4. Unassigned board: the Automaton from the roll-up with Assign / Open / Dismiss.
    const clickSubtab = async (k) => {
      await page.evaluate((kk) => {
        if (typeof window.selectProjectsSubtab === 'function') { window.selectProjectsSubtab(kk); return; }
        document.querySelector(`.projects-subtab[data-projects-subtab="${kk}"]`).click();
      }, k);
    };
    await clickSubtab('unassigned');
    await page.waitForSelector('#projectsSubtabBody .project-automaton[data-automaton="prdx"]', { state: 'attached', timeout: 30000 });
    const rowText = await (await page.locator('#projectsSubtabBody .project-automaton[data-automaton="prdx"]').first()).innerText();
    check(/Stray builder/.test(rowText), `the unassigned Automaton is listed (got: ${rowText.slice(0, 120)})`);
    check(/Assign to a project/.test(rowText) && /Dismiss/.test(rowText),
      `the row offers Assign and Dismiss (got: ${rowText.slice(0, 160)})`);
    await screenshot(page, 'projects-unassigned');

    // Dismiss hides it and remembers the choice on this device.
    await page.evaluate(() => window.projectsUnassignedDismiss('prdx'));
    await page.waitForFunction(() => !document.querySelector('#projectsSubtabBody .project-automaton[data-automaton="prdx"]'), null, { timeout: 5000, polling: 50 });
    const hiddenAfterDismiss = await page.evaluate((k) => localStorage.getItem(k), UNASSIGNED_KEY);
    check(JSON.parse(hiddenAfterDismiss || '[]').includes('prdx'),
      `dismiss is remembered in ${UNASSIGNED_KEY} (got: ${hiddenAfterDismiss})`);
    summary.dismissed = 'yes';
    check((await page.locator('#projectsSubtabBody').innerText()).includes('Undo'),
      'the Undo note appears while an Automaton is hidden');

    // Undo brings it back.
    await page.evaluate(() => window.projectsUnassignedUndo());
    await page.waitForSelector('#projectsSubtabBody .project-automaton[data-automaton="prdx"]', { state: 'attached', timeout: 5000 });
    summary.undo = 'yes';

    // 5. Assign saves the chosen project with the shared set_project call.
    const assignCtl = await fulfillEndpoint(page, '/api/autonomous/prds/prdx/set_project', {}, { method: 'POST' });
    await page.evaluate(() => {
      window.prompt = () => 'acme'; // the story chooses the stubbed project
    });
    try {
      await page.evaluate(async () => { await window.projectsUnassignedAssign('prdx'); });
    } catch (e) {
      console.error('assign threw:', e.message);
    }
    await page.waitForTimeout(500);
    const assignHits = assignCtl.hits;
    assignCtl.stop();
    check(assignHits >= 1, `assign saved the project with POST set_project (hits: ${assignHits})`);
    summary.assignCalls = assignHits;
    console.error(`assign: POST /api/autonomous/prds/prdx/set_project hits=${assignHits}`);

    // 6. No sideways scroll anywhere.
    const ov = await noHorizontalOverflow(page);
    check(ov.ok, `no horizontal overflow at 390px (scrollWidth ${ov.scrollWidth} > ${ov.innerWidth})`);

    check(errors.length === 0, `no page errors (got: ${errors.slice(0, 3).join(' | ')})`);
    await saveJson('story-summary', summary);
    await screenshot(page, 'end', `projects cards story done: ${JSON.stringify(summary)}`);
  } finally {
    if (typeof listCtl !== 'undefined') listCtl.stop();
    if (typeof rollupCtl !== 'undefined') rollupCtl.stop();
    await context.close().catch(() => {});
  }
});
