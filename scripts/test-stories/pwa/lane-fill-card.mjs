#!/usr/bin/env node
/**
 * Lane fill card story — the Automation Overview shows the lane fill card:
 *                        mode, per-lane-kind modes, effective versus
 *                        configured lane counts, and a refusal chip with its
 *                        reason text on each blocked decision
 * tags: surface:pwa feature:lane-fill
 *
 * Route fulfilment (no daemon data): GET /api/autonomous/lane-fill answers a
 * shadow-mode read view whose GPU kind runs "on", whose Claude lane count
 * follows an LLM cap (1 of 3 configured), and whose decision log holds one
 * blocked decision per refusal rule, one with a code the card does not know
 * (no chip) and one whose reason text and code are hostile HTML. The card
 * must show the mode in words, the modes line, the lane counts line, each
 * known code's chip and the reason text, with every value rendered as text
 * (the hostile record must never ship a live <img>/<script>), and the page
 * must not scroll sideways at phone width. Then an older daemon answers with
 * no lane_counts and no modes (the card renders as before, with neither
 * line), and finally the route fails — the card shows the failed state.
 */
import { screenshot } from './lib.mjs';
import {
  runPerfStory, coldPage, openPWA, fulfillEndpoint, failEndpoint, measureTab,
  assertMeasure, check, noHorizontalOverflow, saveJson,
} from './perf-lib.mjs';

const now = Date.now();
const iso = (msAgo) => new Date(now - msAgo).toISOString();

const CODES = [
  ['stale', 'Stale'],
  ['held', 'Held'],
  ['sequenced_after_unshipped', 'Sequenced after unshipped work'],
  ['proposal_not_confirmed', 'Proposal not confirmed'],
  ['proposal_missing', 'Proposal missing'],
  ['main_carries_work', 'Main already carries the work'],
  ['worktree_invalid', 'Worktree invalid'],
];

// Wire shape of internal/autonomous LaneFillStatus (lane_fill_coordinator.go).
const decisions = CODES.map(([code], i) => ({
  at: iso((i + 1) * 60 * 1000), lane_id: 'claude-1', action: 'blocked', title: `Automaton ${code}`,
  reason: `refusal text for ${code}`, eligibility_reason: code, executed: false,
}));
decisions.push({
  at: iso(9 * 60 * 1000), lane_id: 'claude-1', action: 'blocked', title: 'Automaton unknown-code',
  reason: 'a code from a newer daemon', eligibility_reason: 'brand_new_code', executed: false,
});
decisions.push({
  at: iso(10 * 60 * 1000), lane_id: 'gpu-1', action: 'blocked', title: 'Automaton hostile',
  reason: '<img src=x onerror=alert(1)> hostile reason', eligibility_reason: '"><script>alert(2)</script>', executed: false,
});

const populated = {
  mode: 'shadow',
  modes: { gpu: 'on', claude: 'shadow' },
  lane_counts: {
    gpu: { effective: 2, configured: 2, source: 'config' },
    claude: { effective: 1, configured: 3, source: 'llm_caps' },
  },
  lanes: [
    { id: 'gpu-1', type: 'gpu', occupied: false, eligible_waiting: 0 },
    { id: 'claude-1', type: 'claude', occupied: false, eligible_waiting: 0, blocker: 'nothing eligible' },
  ],
  queued_eligible: { gpu: 0, claude: 0 },
  evidence: { gpu_active_sessions: 0, loaded_models: [] },
  decisions,
  alerts: [],
};
const older = {
  mode: 'shadow',
  lanes: [{ id: 'gpu-1', type: 'gpu', occupied: false, eligible_waiting: 0 }],
  queued_eligible: { gpu: 0, claude: 0 },
  evidence: { gpu_active_sessions: 0, loaded_models: [] },
  decisions: [
    { at: iso(60 * 1000), lane_id: 'gpu-1', action: 'blocked', title: 'Automaton plain', reason: 'plain reason', executed: false },
  ],
  alerts: [],
};

const slotText = async (p) => (await (await p.$('#automationLaneFillCard')).innerText());
const reload = (p) => p.evaluate(() => window._listLoaders.automation_lane_fill.load());

await runPerfStory(async (page) => {
  const { context, page: p } = await coldPage(page.context().browser());
  const dialogs = [];
  try {
    p.on('dialog', async (d) => { dialogs.push(d.message()); await d.dismiss().catch(() => {}); });

    await openPWA(p);
    const ctl = await fulfillEndpoint(p, '/api/autonomous/lane-fill', populated);

    const r = await measureTab(p, 'autonomous', { subtab: 'automation', timeout: 30000, watchMs: 800 });
    assertMeasure('automation-lane-fill', r);
    await p.click('.automation-subtabs .output-tab[data-subtab="overview"]');
    await p.waitForFunction(() => {
      const c = document.getElementById('automationLaneFillCard');
      return c && c.querySelector('.lane-fill-card');
    }, null, { timeout: 15000 });
    await reload(p);

    const text = await slotText(p);
    const html = await (await p.$('#automationLaneFillCard')).innerHTML();
    for (const needle of [
      'Lane fill',
      'Lane counts: GPU lane 2 (configured 2, source: configuration); Claude lane 1 (configured 3, source: LLM caps)',
      'Modes by lane type: GPU lane on; Claude lane shadow',
      'refusal text for stale',
      'refusal text for worktree_invalid',
      'a code from a newer daemon',
    ]) check(text.includes(needle), `lane fill card missing "${needle}" (got: ${text.slice(0, 900)})`);

    // One chip per known code, worded from the bundle; none for the unknown one.
    for (const [code, label] of CODES) {
      const chip = await p.$(`#automationLaneFillCard .lane-fill-eligibility-${code}`);
      check(chip, `no chip for eligibility code ${code}`);
      const chipText = await chip.innerText();
      check(chipText.includes(label), `chip for ${code} reads "${chipText}", want "${label}"`);
    }
    check((await p.$$eval('#automationLaneFillCard .lane-fill-eligibility', els => els.length)) === CODES.length,
      'a decision with an unknown or hostile eligibility code showed a chip');

    // Hostile values are inert text, not markup; nothing ran.
    check(!html.includes('<img src=x'), 'hostile reason rendered as a live <img>');
    check(!html.includes('<script>alert(2)'), 'hostile eligibility code rendered as a live <script>');
    check(text.includes('hostile reason'), 'hostile reason text is not shown as text');
    check(!dialogs.length, `dialog(s) fired by a hostile record: ${dialogs.join('; ')}`);
    await screenshot(p, 'automation-lane-fill');

    // Phone width: the card (hostile and long text included) must never push
    // the page or the card wider than the viewport.
    await p.setViewportSize({ width: 360, height: 740 });
    await p.waitForTimeout(300);
    const fit = await noHorizontalOverflow(p);
    check(fit.ok, `lane fill card scrolls sideways at phone width (scrollWidth ${fit.scrollWidth}, viewport ${fit.innerWidth})`);
    await screenshot(p, 'automation-lane-fill-phone');
    await p.setViewportSize({ width: 1280, height: 800 });

    // An older daemon: no lane_counts, no modes, no codes. The card renders as
    // before, with neither line and no chip.
    ctl.stop();
    const olderCtl = await fulfillEndpoint(p, '/api/autonomous/lane-fill', older);
    await reload(p);
    await p.waitForFunction(() => {
      const c = document.getElementById('automationLaneFillCard');
      return c && c.innerText.includes('plain reason') && !c.innerText.includes('Lane counts:');
    }, null, { timeout: 15000 });
    const olderText = await slotText(p);
    check(!olderText.includes('Modes by lane type'), `older daemon card shows a modes line (got: ${olderText.slice(0, 400)})`);
    check((await p.$$eval('#automationLaneFillCard .lane-fill-eligibility', els => els.length)) === 0,
      'older daemon card shows an eligibility chip');
    await screenshot(p, 'automation-lane-fill-older');

    // Endpoint unavailable: the card says so, never a stale healthy state.
    olderCtl.stop();
    await failEndpoint(p, '/api/autonomous/lane-fill', { status: 503 });
    await reload(p);
    await p.waitForFunction(() => {
      const c = document.getElementById('automationLaneFillCard');
      return c && c.innerText.includes('Lane status failed');
    }, null, { timeout: 15000 });

    check(!dialogs.length, `dialog(s) fired somewhere in the story: ${dialogs.join('; ')}`);
    await saveJson('lane-fill', { states: r.states, indicatorMs: r.indicatorMs });
  } finally {
    await context.close().catch(() => {});
  }
});
