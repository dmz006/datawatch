#!/usr/bin/env node
/**
 * Security failure card story — the read-only report card renders on the Automation
 *                               sub-tab beside the triage card, one card per report
 * tags: surface:pwa feature:automata
 *
 * Route fulfilment (no daemon data): GET /api/autonomous/security-failures
 * answers one shadow-mode report, one on-mode report that stopped the
 * Automaton line, and one hostile report whose fields are markup. Each card
 * must show the dependency, the advisory identifier, the publication date, the
 * registry state, the recommendation, the mode and the action taken (or the
 * would-have-taken line in shadow mode), every value as text, with nothing
 * from the hostile report ever shipping a live <img> or <script>. The list
 * must settle on the empty state when the server reports no reports, and the
 * cards must not push the page wider than a phone viewport.
 *
 * The story only reads: it creates nothing, changes no config and never runs
 * against production (it drives the sandbox daemon the PWA story runner uses).
 */
import { screenshot } from './lib.mjs';
import {
  runPerfStory, coldPage, openPWA, fulfillEndpoint, check,
} from './perf-lib.mjs';

// Wire shape of autonomous.SecurityFailureListEntry (security_failure_render.go):
// the repo, ref, workflow and run identity, plus the stored SecurityFailureReport.
const shadowEntry = {
  repo: 'sandbox-throwaway/securityfailure',
  ref_kind: 'tag',
  ref_name: 'v0.0.1',
  workflow: 'CodeQL',
  run_id: '9000001',
  run_url: 'https://github.com/sandbox-throwaway/securityfailure/actions/runs/9000001',
  status: 'failing',
  report: {
    dependency: 'example.com/sandbox/vulnmod-a',
    advisory_ids: ['CVE-2026-90042'],
    workflow: 'CodeQL',
    run_id: 9000001,
    sha: 'e00e307597bea03949189be1e3333b91f3986818',
    published_at: '2026-10-27T09:00:00Z',
    fixed_version: '',
    registry_found: false,
    recommendation: 'keep_no_entry',
    mode: 'shadow',
    action: 'would_stop',
    assessed_at: '2026-10-30T02:22:34Z',
  },
};
const onEntry = {
  repo: 'sandbox-throwaway/securityfailure',
  ref_kind: 'tag',
  ref_name: 'v0.0.2',
  workflow: 'CodeQL',
  run_id: '9000002',
  run_url: 'https://github.com/sandbox-throwaway/securityfailure/actions/runs/9000002',
  status: 'failing',
  report: {
    dependency: 'example.com/sandbox/vulnmod-b',
    advisory_ids: ['CVE-2026-90077'],
    workflow: 'CodeQL',
    run_id: 9000002,
    sha: '30b92ffb394b9e15b9b11b4dc0c553a8df4e39e3',
    published_at: '2026-10-28T09:00:00Z',
    fixed_version: '1.4.2',
    registry_found: false,
    recommendation: 'upgrade',
    mode: 'on',
    action: 'stopped',
    assessed_at: '2026-10-30T02:31:30Z',
  },
};
// Hostile values: every field tries to inject markup.
const hostileEntry = {
  repo: '<img src=x onerror=alert(1)>',
  ref_kind: 'tag',
  ref_name: '<script>alert(1)</script>',
  workflow: 'onmouseover=alert(1)',
  run_id: '9000003',
  run_url: 'javascript:alert(1)',
  status: 'failing',
  report: {
    dependency: '<img src=x onerror=alert(1)>',
    advisory_ids: ['<script>alert(1)</script>'],
    published_at: 'not a date',
    recommendation: 'not-a-known-value',
    mode: 'shadow',
    action: '<img src=x onerror=alert(1)>',
    registry_error: '<script>alert(1)</script>',
  },
};

await runPerfStory(async (page) => {
  const { context, page: p } = await coldPage(page.context().browser());
  const dialogs = [];
  try {
    p.on('dialog', async (d) => { dialogs.push(d.message()); await d.dismiss().catch(() => {}); });

    await openPWA(p);
    const listCtl = await fulfillEndpoint(p, '/api/autonomous/security-failures', [onEntry, shadowEntry, hostileEntry]);

    // The Automation tab of the Automata view, which lands on its Chains
    // sub-tab: the card renders there beside the triage card.
    await p.evaluate(() => window.navigate('automation'));
    await p.waitForFunction(() => !!document.getElementById('automationSecurityFailureCard')
      && document.getElementById('automationSecurityFailureCard').querySelectorAll('.security-failure-card').length === 3,
    null, { timeout: 30000 });

    const slot = await p.$('#automationSecurityFailureCard');
    check(slot, 'no #automationSecurityFailureCard slot in the Automation sub-tab');
    const text = await slot.innerText();
    const html = await slot.innerHTML();

    // The shadow card: what it found, and that nothing was taken.
    for (const needle of [
      'Security failure',
      'sandbox-throwaway/securityfailure · tag/v0.0.1 · CodeQL · Run 9000001',
      'Dependency: example.com/sandbox/vulnmod-a',
      'Advisory: CVE-2026-90042',
      'Published: 2026-10-27T09:00:00Z',
      'Registry entry: not in the registry',
      'Recommendation: keep, but add a registry entry',
      'Mode: shadow',
      'Would have taken (shadow mode): stop the Automaton line',
    ]) check(text.includes(needle), `security failure card missing "${needle}" (got: ${text.slice(0, 800)})`);

    // The on card: the Automaton line was stopped and the fixed version is shown.
    for (const needle of [
      'Dependency: example.com/sandbox/vulnmod-b',
      'Advisory: CVE-2026-90077',
      'Fixed version: 1.4.2',
      'Recommendation: upgrade to the fixed version',
      'Mode: on',
      'Action taken: stopped the Automaton line',
    ]) check(text.includes(needle), `on-mode card missing "${needle}"`);

    // Hostile values are inert text: no live markup, no dialog.
    check(!html.includes('<img src=x'), 'hostile report value rendered as a live <img>');
    check(!html.includes('<script>alert'), 'hostile report value rendered as a live <script>');
    check(!/href="javascript:/i.test(html), 'hostile run_url rendered as a javascript: link');
    check(text.includes('<img src=x onerror=alert(1)>'), 'hostile value not shown as text');
    check(!dialogs.length, `dialog(s) fired by hostile report: ${dialogs.join('; ')}`);
    await screenshot(p, 'automation-security-failure-list');

    // Read-only: the card holds no control that acts.
    const controls = await p.$$eval('#automationSecurityFailureCard button, #automationSecurityFailureCard input, #automationSecurityFailureCard select', els => els.length);
    check(controls === 0, `security failure card holds ${controls} control(s); it must be read-only`);

    // Phone width: the cards must never push the page or a card wider than the viewport.
    await p.setViewportSize({ width: 360, height: 740 });
    await p.waitForTimeout(300);
    const fit = await p.evaluate(() => ({
      inner: window.innerWidth,
      doc: document.documentElement.scrollWidth,
      body: document.body.scrollWidth,
      wide: Array.from(document.querySelectorAll('#automationSecurityFailureCard .security-failure-card'))
        .filter(c => c.scrollWidth > c.clientWidth + 1 || c.getBoundingClientRect().right > window.innerWidth + 1).length,
    }));
    check(fit.doc <= fit.inner + 1 && fit.body <= fit.inner + 1,
      `security failure list scrolls sideways at phone width (doc ${fit.doc}, body ${fit.body}, viewport ${fit.inner})`);
    check(fit.wide === 0, `${fit.wide} security failure card(s) overflow at phone width`);
    await screenshot(p, 'automation-security-failure-phone');
    await p.setViewportSize({ width: 1280, height: 800 });

    // Empty state: a server that reports no reports draws the empty line, with
    // no card left in the slot. Rendered through the list renderer the loader
    // hands the body to, so the check does not depend on the poll timing.
    const empty = await p.evaluate(() => {
      const host = document.createElement('div');
      host.innerHTML = window.securityFailureListHtml([]);
      const out = { empty: !!host.querySelector('.security-failure-list-empty'), cards: host.querySelectorAll('.security-failure-card').length, text: host.innerText };
      host.remove();
      return out;
    });
    check(empty.empty && empty.cards === 0, `empty list did not render the empty state (cards ${empty.cards})`);
    check(empty.text.includes('No security failure reports.'), `empty state text unexpected: "${empty.text.slice(0, 200)}"`);
    listCtl.stop();
  } finally {
    await context.close().catch(() => {});
  }
});
