#!/usr/bin/env node
/**
 * Merge pipeline card story — the Automation Overview card shows the pipeline in
 * effect for the chosen or preselected project (source, steps, policy, model),
 * or, when the daemon refuses with project_required, the no-project empty state
 * with the project choice and a clickable worktree_base_repo help link
 * tags: surface:pwa feature:merge-pipeline group:automation
 *
 * Runs against the runner's sandbox daemon (never the production daemon). Flow:
 *   1. open the Automata tab, Automation sub-tab; the merge pipeline card appears;
 *   2. a bare GET /api/autonomous/merge-pipeline with no project either answers
 *      200 with the pipeline in effect, or 400 with code project_required;
 *   3. in the 200 case the card preselects a project and lists the steps;
 *   4. in the project_required case the card renders the empty state — never the
 *      raw error — with the project input and a help link on the
 *      worktree_base_repo line pointing at the how-to's "No project yet" section;
 *   5. the card never scrolls sideways at phone width (390x844).
 * The story reads only; it sets no pipeline, record or policy.
 */
import { screenshot } from './lib.mjs';
import { runPerfStory, openPWA, check, saveJson, skipStory, noHorizontalOverflow } from './perf-lib.mjs';

const PHONE = { width: 390, height: 844 };
const CARD = '.merge-pipeline-card';
const NO_PROJECT_LINK = 'howto/merge-pipeline.md#no-project-yet';

await runPerfStory(async (page) => {
  const browser = page.context().browser();
  const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport: PHONE, locale: 'en-US', isMobile: true, hasTouch: true });
  const p = await context.newPage();
  const errors = [];
  p.on('pageerror', (err) => errors.push(`pageerror: ${err.message}`));
  try {
    await openPWA(p, { timeout: 60000 });

    // What does the daemon answer for a bare read, no project?
    const probe = await p.evaluate(async () => {
      const r = await fetch('/api/autonomous/merge-pipeline', { credentials: 'same-origin' });
      let body = null;
      try { body = await r.json(); } catch {}
      return { status: r.status, body };
    });
    if (probe.status === 404) await skipStory('the daemon has no /api/autonomous/merge-pipeline; start a sandbox daemon built from this tree');
    const projectRequired = probe.status === 400 && probe.body && probe.body.code === 'project_required';
    check(projectRequired || probe.status === 200, `bare GET /api/autonomous/merge-pipeline -> ${probe.status}`);

    await p.evaluate(() => {
      window.navigate('autonomous');
      const btn = document.querySelector('.automata-tabs-bar .output-tab[data-tab="automation"]');
      if (btn && !btn.classList.contains('active')) btn.click();
    });
    await p.waitForSelector(CARD, { state: 'attached', timeout: 60000 });
    await p.locator(CARD).first().scrollIntoViewIfNeeded();

    const summary = { mode: '' };
    if (projectRequired) {
      // The empty state, not the raw refusal text.
      await p.waitForSelector(`${CARD}.merge-pipeline-no-project`, { state: 'attached', timeout: 30000 });
      const cardText = await (await p.locator(CARD).first()).innerText();
      check(cardText.includes('project'), `empty state has no project wording (got: ${cardText.slice(0, 200)})`);
      check(!/Choose a project with the project parameter/.test(cardText), 'raw daemon error text leaked into the card');
      const input = await p.locator(`${CARD} #mergePipelineProject`).first();
      check(await input.count() > 0, 'the project input is missing from the empty state');
      // The worktree_base_repo line is clickable: a help link to the how-to.
      const hrefs = await p.locator(`${CARD} .docs-link`).all();
      let linkOk = false;
      for (const h of hrefs) {
        const href = await h.getAttribute('href');
        if (href && href.includes(NO_PROJECT_LINK)) { linkOk = true; break; }
      }
      check(linkOk, `no help link to ${NO_PROJECT_LINK} on the empty state (got: ${hrefs.length} docs links)`);
      await screenshot(p, 'merge-pipeline-empty-state');
      summary.mode = 'project_required';
    } else {
      // A pipeline in effect: preselected project and the steps listed.
      await p.waitForSelector(`${CARD} .merge-pipeline-step`, { state: 'attached', timeout: 30000 });
      const steps = await p.locator(`${CARD} .merge-pipeline-step`).count();
      check(steps >= 1, `no steps listed (got ${steps})`);
      const value = await p.locator(`${CARD} #mergePipelineProject`).first().inputValue();
      check(typeof value === 'string' && value.length > 0, `no project preselected (value: "${value}")`);
      const cardText = await (await p.locator(CARD).first()).innerText();
      check(!/project_required/.test(cardText), 'refusal code leaked into the card');
      await screenshot(p, 'merge-pipeline-card');
      summary.mode = 'pipeline';
      summary.steps = steps;
      summary.project = value;
    }

    const fit = await noHorizontalOverflow(p);
    check(fit.ok, `merge pipeline card scrolls sideways at phone width (scrollWidth ${fit.scrollWidth}, viewport ${fit.innerWidth})`);
    check(!errors.length, `page errors: ${errors.join('; ')}`);
    await saveJson('merge-pipeline-card', summary);
  } finally {
    await context.close().catch(() => {});
  }
});
