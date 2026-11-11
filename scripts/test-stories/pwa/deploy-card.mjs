#!/usr/bin/env node
/**
 * Deploy card story — the Automation sub-tab's Deploy card states the configured
 *                     steps and the last result, and Run now posts once
 * tags: surface:pwa feature:deploy
 *
 * Route fulfilment (no daemon data, no network, never the real deploy): GET
 * /api/autonomous/deploy answers a configured view; POST
 * /api/autonomous/deploy/run is intercepted and counted. The card must list
 * every deploy setting, state the last phase and outcome, and the Run now
 * button must send exactly one request. The spec asked for a confirmation, but
 * app.js deployRunNow has none and may not change here; the story records that.
 */
import { readFile } from 'node:fs/promises';
import { screenshot } from './lib.mjs';
import {
  runPerfStory, coldPage, openPWA, fulfillEndpoint, measureTab,
  assertMeasure, check, saveJson,
} from './perf-lib.mjs';

const APP_JS = new URL('../../../internal/server/web/app.js', import.meta.url).pathname;

const view = {
  settings: {
    build_command: 'go build -o datawatch ./cmd/datawatch/',
    install_command: 'install -m 755 datawatch /usr/local/bin/datawatch',
    binary_path: '/usr/local/bin/datawatch',
    restart_command: 'systemctl --user restart datawatch',
    health_check: 'curl -fsS http://127.0.0.1:8080/healthz',
    drain_timeout: '10m',
    health_timeout: '2m',
  },
  phase: 'health_check',
  outcome: 'succeeded',
  running: false,
};

await runPerfStory(async (page) => {
  // The Settings page lists a Deploy group with every setting key.
  const src = await readFile(APP_JS, 'utf8');
  for (const k of ['build_command', 'install_command', 'binary_path', 'restart_command',
    'health_check', 'drain_timeout', 'health_timeout']) {
    check(src.includes(`key: 'autonomous.deploy.${k}'`), `Settings has no autonomous.deploy.${k} field`);
  }

  const { context, page: p } = await coldPage(page.context().browser());
  const dialogs = [];
  let posts = 0;
  let acceptDialog = false;
  try {
    p.on('dialog', async (d) => {
      dialogs.push({ type: d.type(), message: d.message() });
      if (acceptDialog) await d.accept().catch(() => {}); else await d.dismiss().catch(() => {});
    });
    await openPWA(p);
    await fulfillEndpoint(p, '/api/autonomous/deploy', view);
    await p.route('**/api/autonomous/deploy/run', async (route) => {
      posts++;
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status: 'succeeded' }) });
    });

    const r = await measureTab(p, 'autonomous', { subtab: 'automation', timeout: 30000, watchMs: 800 });
    assertMeasure('automation-deploy', r);

    await p.waitForFunction(() => {
      const c = document.getElementById('automationDeployCard');
      return c && c.querySelector('.deploy-card .deploy-setting');
    }, null, { timeout: 15000 });
    const text = await (await p.$('#automationDeployCard')).innerText();

    // The configured steps: label and value for each.
    for (const needle of [
      'Deploy', 'Settings in effect',
      'Build command', 'go build -o datawatch ./cmd/datawatch/',
      'Install command', 'install -m 755 datawatch /usr/local/bin/datawatch',
      'Installed binary path', '/usr/local/bin/datawatch',
      'Restart command', 'systemctl --user restart datawatch',
      'Health check command', 'curl -fsS http://127.0.0.1:8080/healthz',
      'Drain timeout', '10m', 'Health timeout', '2m',
    ]) check(text.includes(needle), `Deploy card missing "${needle}" (got: ${text.slice(0, 600)})`);

    // The last result.
    check(text.includes('Phase: health_check'), 'Deploy card does not state the last phase');
    check(text.includes('Last outcome: succeeded'), 'Deploy card does not state the last outcome');
    await screenshot(p, 'automation-deploy-card');

    // Run now: enabled with commands set, and it posts once.
    const btn = await p.$('#automationDeployCard button.deploy-run');
    check(btn, 'no Run now button on the Deploy card');
    check(!(await btn.isDisabled()), 'Run now is disabled although commands are configured');

    await btn.click();
    await p.waitForFunction(() => /Deploy result: succeeded/.test(
      (document.getElementById('deployMessage') || {}).textContent || ''), null, { timeout: 10000 });
    // app.js does not ask for confirmation today: one click posts once, no dialog.
    check(dialogs.length === 0, `Run now raised ${dialogs.length} dialog(s); app.js has no confirmation`);
    check(posts === 1, `Run now posted ${posts} request(s), expected exactly 1`);
    await screenshot(p, 'automation-deploy-run-result');
    await saveJson('deploy-card', { dialogs, posts });
  } finally {
    await context.close().catch(() => {});
  }
});
