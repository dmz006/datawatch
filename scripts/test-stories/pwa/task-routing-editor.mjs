#!/usr/bin/env node
/**
 * Task routing editor story — Settings → Automata → Task routing: edit one row,
 *                             save a profile, switch the active profile, dry run
 * tags: surface:pwa feature:task-routing
 *
 * Route fulfilment (no daemon data, no live LLM): GET /api/autonomous/task-routing,
 * /api/llms, the model lists and /api/autonomous/prds are answered by the story,
 * and so are the writes. A hostile value sits in a backend name and in a draft
 * Automaton title, and comes back in the dry run; every one must render as text
 * (no live <img>/<script>, no dialog). The story asserts the posted bodies:
 *   - editing the effort of the "code" row and pressing "Save table" PUTs the
 *     whole table with that row changed,
 *   - "Save table as profile" POSTs {name, rows} to /task-routing/profiles,
 *   - picking another profile PUTs {name} to /task-routing/active-profile,
 *   - the preview POSTs {profile: <the newly active profile>} to the dry-run
 *     route of the picked draft and renders one row per entry.
 * The editor must not scroll sideways at 360 px.
 */
import { screenshot } from './lib.mjs';
import {
  runPerfStory, coldPage, openPWA, fulfillEndpoint, check, noHorizontalOverflow, saveJson,
} from './perf-lib.mjs';

const HOSTILE = '<img src=x onerror=alert(1)>';
const DRAFT_ID = 'tr-draft-1';

const rowsBalanced = {
  code: { worker_backend: 'claude-code', worker_model: 'sonnet', effort: 'medium' },
  docs: { worker_backend: 'ollama-small' },
};
const rowsThrifty = {
  code: { worker_backend: 'ollama-small' },
  docs: { worker_backend: 'ollama-small' },
};
const view = {
  enabled: true,
  active_profile: 'balanced',
  task_types: ['code', 'docs'],
  profiles: { balanced: rowsBalanced, thrifty: rowsThrifty },
  validation: { valid: true, problems: [] },
};

await runPerfStory(async (page) => {
  const { context, page: p } = await coldPage(page.context().browser());
  const dialogs = [];
  const puts = [];
  const profilePosts = [];
  const activations = [];
  const dryRuns = [];
  try {
    p.on('dialog', async (d) => { dialogs.push(d.message()); await d.dismiss().catch(() => {}); });
    await openPWA(p);

    const json = (route, body) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) });
    await p.route('**/api/autonomous/**', (route) => {
      const req = route.request();
      const path = new URL(req.url()).pathname;
      const method = req.method();
      if (path === '/api/autonomous/task-routing' && method === 'PUT') {
        const body = req.postDataJSON();
        puts.push(body);
        Object.assign(view, { enabled: body.enabled, active_profile: body.active_profile, profiles: body.profiles });
        return json(route, view);
      }
      if (path === '/api/autonomous/task-routing/active-profile' && method === 'PUT') {
        const body = req.postDataJSON();
        activations.push(body);
        view.active_profile = body.name;
        return json(route, view);
      }
      if (path === '/api/autonomous/task-routing/profiles' && method === 'POST') {
        const body = req.postDataJSON();
        profilePosts.push(body);
        view.profiles = { ...view.profiles, [body.name]: body.rows };
        return json(route, view);
      }
      if (path === `/api/autonomous/prds/${DRAFT_ID}/task-routing/dry-run` && method === 'POST') {
        dryRuns.push(req.postDataJSON());
        return json(route, {
          entries: [
            { task_id: 't1', title: HOSTILE, task_type: 'code', row: 'code', would_apply: true, result: { worker_backend: 'ollama-small', worker_model: 'qwen' } },
            { task_id: 't2', title: 'Write the docs', task_type: 'docs', would_apply: false, reason: 'no row for this type' },
          ],
        });
      }
      return route.fallback();
    });
    await fulfillEndpoint(p, '/api/autonomous/task-routing', view);
    await fulfillEndpoint(p, '/api/llms', { llms: [
      { name: 'claude-code', kind: 'claude-code', model: 'sonnet' },
      { name: 'ollama-small', kind: 'ollama' },
      { name: HOSTILE, kind: 'opencode' },
    ] });
    await fulfillEndpoint(p, '/api/llm/claude/models', { aliases: [{ value: 'sonnet' }, { value: 'opus' }], full_names: [] });
    await fulfillEndpoint(p, '/api/ollama/models', ['qwen']);
    await fulfillEndpoint(p, '/api/autonomous/prds', { prds: [
      { id: DRAFT_ID, title: HOSTILE, status: 'draft' },
      { id: 'tr-running', title: 'Running one', status: 'running' },
    ] });

    // Open Settings → Automata and the Task routing card.
    await p.evaluate(() => window.navigate('settings'));
    await p.locator('.settings-tab-btn[data-tab="automata"]').click({ timeout: 30000 });
    await p.waitForSelector('#automataTaskRoutingPanel .task-routing-table', { state: 'attached', timeout: 30000 });
    if (!(await p.locator('#automataTaskRoutingPanel').isVisible())) {
      await p.locator('#settings-sec-automata_task_routing').evaluate((el) => {
        const head = el.previousElementSibling;
        if (head) head.click();
      });
    }
    await p.locator('#automataTaskRoutingPanel').scrollIntoViewIfNeeded();
    const panel = p.locator('#automataTaskRoutingPanel');
    check((await panel.locator('tr.task-routing-row').count()) === 2, 'the editor must show one row per task type');

    let html = await panel.innerHTML();
    check(!html.includes('<img src=x'), 'hostile backend name rendered as a live <img>');
    await screenshot(p, 'task-routing-editor-open');

    // Edit one row, then save the table.
    await panel.locator('select[data-type="code"][data-field="effort"]').selectOption('high');
    await p.waitForTimeout(200);
    await panel.locator('.task-routing-save').click();
    await p.waitForFunction(() => document.querySelector('#automataTaskRoutingPanel .task-routing-table'), null, { timeout: 15000 });
    await p.waitForTimeout(400);
    check(puts.length === 1, `expected one table save, got ${puts.length}`);
    const put = puts[0] || {};
    check(put.active_profile === 'balanced' && put.enabled === true, `table save must keep the active profile and flag, got ${JSON.stringify(put).slice(0, 300)}`);
    check(put.profiles && put.profiles.balanced.code.effort === 'high', `edited effort missing from saved table: ${JSON.stringify(put.profiles && put.profiles.balanced).slice(0, 300)}`);
    check(put.profiles.balanced.code.worker_backend === 'claude-code' && put.profiles.thrifty && put.profiles.thrifty.code.worker_backend === 'ollama-small',
      'saving the table must carry the untouched fields and the other profile');

    // Save the table as a new profile.
    await panel.locator('#trProfileName').fill('fast');
    await panel.locator('.task-routing-save-profile').click();
    await p.waitForTimeout(500);
    check(profilePosts.length === 1, `expected one profile save, got ${profilePosts.length}`);
    check(profilePosts[0].name === 'fast' && profilePosts[0].rows && profilePosts[0].rows.code.effort === 'high',
      `profile save body wrong: ${JSON.stringify(profilePosts[0]).slice(0, 300)}`);
    await p.waitForFunction(() => [...document.querySelectorAll('#trProfile option')].some((o) => o.value === 'fast'), null, { timeout: 15000 });

    // Switch the active profile.
    await panel.locator('#trProfile').selectOption('thrifty');
    await p.waitForTimeout(500);
    check(activations.length === 1 && activations[0].name === 'thrifty', `active-profile body wrong: ${JSON.stringify(activations)}`);
    await p.waitForFunction(() => document.querySelector('#trProfile') && document.querySelector('#trProfile').value === 'thrifty', null, { timeout: 15000 });

    // Dry run against the draft Automaton.
    const opts = await panel.locator('#trPreviewAutomaton option').allInnerTexts();
    check(opts.some((o) => o.includes(HOSTILE)), `draft title must appear as text in the picker (got ${JSON.stringify(opts)})`);
    check(!opts.some((o) => o.includes('Running one')), 'only draft Automata may be offered for the preview');
    await panel.locator('#trPreviewAutomaton').selectOption(DRAFT_ID);
    await panel.locator('.task-routing-preview-run').click();
    await p.waitForSelector('#trPreviewResult .task-routing-preview-row', { timeout: 15000 });
    check(dryRuns.length === 1 && dryRuns[0].profile === 'thrifty', `dry-run body wrong: ${JSON.stringify(dryRuns)}`);
    check((await p.locator('#trPreviewResult .task-routing-preview-row').count()) === 2, 'dry run must render one row per entry');
    const previewText = await p.locator('#trPreviewResult').innerText();
    check(previewText.includes(HOSTILE), 'hostile task title must show as text in the preview');
    check(previewText.includes('ollama-small:qwen') && previewText.includes('no row for this type'), `preview verdicts missing (got ${previewText.slice(0, 300)})`);
    html = await panel.innerHTML();
    check(!html.includes('<img src=x'), 'hostile value rendered as a live <img> after the dry run');
    check(!dialogs.length, `dialog(s) fired by a hostile value: ${dialogs.join('; ')}`);
    await screenshot(p, 'task-routing-editor-preview');

    // Phone width: no sideways scroll.
    await p.setViewportSize({ width: 360, height: 740 });
    await p.waitForTimeout(300);
    await panel.scrollIntoViewIfNeeded();
    const fit = await noHorizontalOverflow(p);
    check(fit.ok, `task routing editor scrolls sideways at phone width (scrollWidth ${fit.scrollWidth}, viewport ${fit.innerWidth})`);
    await screenshot(p, 'task-routing-editor-phone');

    check(!dialogs.length, `dialog(s) fired somewhere in the story: ${dialogs.join('; ')}`);
    await saveJson('task-routing-editor', { puts, profilePosts, activations, dryRuns });
  } finally {
    await context.close().catch(() => {});
  }
});
