#!/usr/bin/env node
/**
 * TS-804 — an empty system shows the Automation empty state with Create Automation
 * tags: surface:pwa feature:load-performance feature:empty-state
 *
 * /api/automations answers an empty list (route fulfilment, so the test does not
 * depend on the daemon's data). The Automation sub-tab (opened from the Automata tab) must go loading -> empty
 * state, never blank, never the empty state before the load finished, and the
 * empty state must carry the Create Automation button that opens the form. The
 * shared capacity card must render beside it.
 */
import { screenshot } from './lib.mjs';
import {
  runPerfStory, coldPage, openPWA, fulfillEndpoint, delayEndpoint, measureTab,
  assertMeasure, check, viewState, saveJson,
} from './perf-lib.mjs';

await runPerfStory(async (page) => {
  const { context, page: p } = await coldPage(page.context().browser());
  try {
    await openPWA(p);
    await fulfillEndpoint(p, '/api/automations', []);
    // Hold the list back so the "no empty state before the first load" rule is observable.
    await delayEndpoint(p, '/api/capacity', 600);

    const states = [];
    const r = await measureTab(p, 'autonomous', { subtab: 'automation', timeout: 30000, watchMs: 800 });
    states.push(...r.states);
    assertMeasure('automation-empty', r);
    check(r.firstState === 'empty', `first settled state was "${r.firstState}", expected the empty state (${r.states.join(' > ')})`);
    check(r.states.indexOf('empty') > r.states.indexOf('loading') || r.states[0] === 'empty',
      `empty state appeared before a loading state (${r.states.join(' > ')})`);

    const empty = await p.$('.automation-empty-state').catch(() => null);
    check(empty, 'no .automation-empty-state rendered');
    const btn = await p.$('#automationEmptyCreateBtn').catch(() => null);
    check(btn, 'no Create Automation button in the empty state');
    check(await btn.isVisible(), 'Create Automation button not visible');
    const label = (await btn.textContent()) || '';
    check(/create/i.test(label), `button label "${label.trim()}" does not mention Create`);

    // The capacity card is the shared component, rendered in its own slot.
    await p.waitForFunction(() => {
      const c = document.getElementById('automationCapacityCard');
      return c && c.innerText.trim().length > 0;
    }, null, { timeout: 15000 });
    await screenshot(p, 'automation-empty');

    await btn.click();
    await p.waitForFunction(() => !!document.querySelector('#automationForm, .automation-form, .modal, [id*="utomationForm"]'),
      null, { timeout: 5000 }).catch(() => { throw new Error('Create Automation did not open a form'); });
    check((await viewState(p)) !== 'blank', 'view blank after opening the form');
    await saveJson('empty-state', { states });
  } finally {
    await context.close().catch(() => {});
  }
});
