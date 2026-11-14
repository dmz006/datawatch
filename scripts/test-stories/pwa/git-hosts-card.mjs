#!/usr/bin/env node
/**
 * Git hosts card story — Settings, General shows the Git hosts card, each host
 *                        renders its fields, and no secret is ever shown
 * tags: surface:pwa feature:git-hosts
 *
 * Route fulfilment (no daemon data): GET /api/config answers two git.hosts
 * entries (one vault-credentialed, one host-login) plus a planted token-shaped
 * value under a stray `token` key, and GET /api/git/hosts answers readiness for
 * both. The card must render one group per host with the name, kind, base URL
 * (self-hosted kinds only), credential source and vault secret name fields. The
 * vault secret field holds only the secret NAME; the planted token value must
 * never appear in the card's text, markup or any input value.
 */
import { screenshot } from './lib.mjs';
import {
  runPerfStory, coldPage, openPWA, fulfillEndpoint, check, saveJson,
} from './perf-lib.mjs';

const SECRET = 'ghp_ABCDEF0123456789abcdef0123456789abcd';

const config = {
  git: {
    hosts: [
      { name: 'work-github', kind: 'github', credential_source: 'vault', vault_secret: 'GH_TOKEN_OPERATOR', token: SECRET },
      { name: 'home-gitea', kind: 'gitea', base_url: 'https://git.example.com', credential_source: 'host_login' },
    ],
  },
};
const hostsReadiness = {
  hosts: [
    { name: 'work-github', kind: 'github', readiness: { state: 'ready', account: 'octo', scopes: ['repo'] } },
    { name: 'home-gitea', kind: 'gitea', readiness: { state: 'not_ready', problem: `login failed with ${SECRET}` } },
  ],
};

await runPerfStory(async (page) => {
  const { context, page: p } = await coldPage(page.context().browser());
  try {
    await openPWA(p);
    await fulfillEndpoint(p, '/api/config', config);
    await fulfillEndpoint(p, '/api/git/hosts', hostsReadiness);

    await p.evaluate(() => window.navigate('settings'));
    await p.locator('.settings-tab-btn[data-tab="general"]').click({ timeout: 30000 });
    await p.waitForSelector('#gitHostsCard .git-host-group', { state: 'attached', timeout: 30000 });
    await p.locator('#gitHostsCard').scrollIntoViewIfNeeded().catch(() => {});

    check((await p.$$eval('#gitHostsCard .git-host-group', (e) => e.length)) === 2, 'expected two git host groups');

    const fields = await p.$$eval('#gitHostsCard [data-gh-field]', (els) => els.map((e) => ({
      idx: e.getAttribute('data-gh-idx'), field: e.getAttribute('data-gh-field'), value: e.value,
    })));
    const val = (idx, f) => (fields.find((x) => x.idx === idx && x.field === f) || {}).value;
    for (const f of ['name', 'kind', 'credential_source', 'vault_secret']) {
      check(fields.some((x) => x.idx === '0' && x.field === f), `host 0 missing the ${f} field`);
    }
    check(val('0', 'name') === 'work-github', 'host 0 name wrong');
    check(val('0', 'kind') === 'github', 'host 0 kind wrong');
    check(val('0', 'credential_source') === 'vault', 'host 0 credential source wrong');
    check(val('0', 'vault_secret') === 'GH_TOKEN_OPERATOR', 'vault field must hold the secret name only');
    check(fields.some((x) => x.idx === '1' && x.field === 'base_url'), 'self-hosted host 1 missing the base URL field');
    check(val('1', 'base_url') === 'https://git.example.com', 'host 1 base URL wrong');
    check(!fields.some((x) => x.idx === '0' && x.field === 'base_url'), 'github host must not show a base URL field');

    // The redacted token field: no secret value anywhere in the card.
    const text = await p.$eval('#gitHostsCard', (e) => e.innerText);
    const html = await p.$eval('#gitHostsCard', (e) => e.innerHTML);
    check(text.includes('Git hosts') || text.length > 0, 'card rendered no text');
    for (const [where, s] of [['text', text], ['markup', html], ['field values', JSON.stringify(fields)]]) {
      check(!s.includes(SECRET), `secret token leaked into the card ${where}`);
      check(!/ghp_[A-Za-z0-9]{10,}/.test(s), `token-shaped string leaked into the card ${where}`);
    }
    check(!(await p.$('#gitHostsCard input[type="password"][value]')), 'a password input carries a value');

    await screenshot(p, 'git-hosts-card');
    await saveJson('git-hosts-card', { hosts: 2, fields: fields.length });
  } finally {
    await context.close().catch(() => {});
  }
});
