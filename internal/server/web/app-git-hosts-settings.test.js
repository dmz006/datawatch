'use strict';
// Git hosts settings card — pure helpers the app.js wiring uses.
//
// Covers the editor's validation (buildHostsPatch), field visibility (base URL
// and vault secret), the config and readiness loaders, the test-connection
// path, the help link target, and that every git_hosts_ key the card asks for
// exists in all five locale bundles.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-git-hosts-settings.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const G = require('./app-git-hosts.js');

const WEB = __dirname;

test('base URL is shown for every kind except github', () => {
  assert.equal(G.baseUrlVisible('github'), false);
  for (const k of ['gitlab', 'gitea', 'bitbucket', 'git']) assert.equal(G.baseUrlVisible(k), true, k);
});

test('gitea, bitbucket and git require a base URL; gitlab and github do not', () => {
  assert.equal(G.baseUrlRequired('gitea'), true);
  assert.equal(G.baseUrlRequired('bitbucket'), true);
  assert.equal(G.baseUrlRequired('git'), true);
  assert.equal(G.baseUrlRequired('gitlab'), false);
  assert.equal(G.baseUrlRequired('github'), false);
});

test('vault secret name is shown only for the vault source', () => {
  assert.equal(G.vaultSecretVisible('vault'), true);
  assert.equal(G.vaultSecretVisible('host_login'), false);
  assert.equal(G.vaultSecretVisible('broker'), false);
  assert.equal(G.vaultSecretVisible(''), false);
});

test('help link points at the connect-git-host howto', () => {
  assert.equal(G.hostHelpUrl(), '/diagrams.html#docs/howto/connect-git-host.md');
  assert.ok(fs.existsSync(path.join(WEB, '..', '..', '..', 'docs', 'howto', 'connect-git-host.md')));
});

test('test path encodes the host name', () => {
  assert.equal(G.hostTestPath('gh-main'), '/api/git/hosts/gh-main/test');
  assert.equal(G.hostTestPath('a/b c'), '/api/git/hosts/a%2Fb%20c/test');
  assert.equal(G.hostTestPath(undefined), '/api/git/hosts//test');
});

test('hostsFromConfig reads git.hosts and defaults an empty source to host_login', () => {
  const cfg = { git: { hosts: [
    { name: 'gh', kind: 'github', credential_source: 'vault', vault_secret: 'GH_TOKEN', sign_tags: true, signing_key: 'K1' },
    { name: 'gl', kind: 'gitlab', base_url: 'https://gl.example' },
    null,
  ] } };
  const groups = G.hostsFromConfig(cfg);
  assert.equal(groups.length, 2);
  assert.deepEqual(groups[0], { name: 'gh', kind: 'github', base_url: '', credential_source: 'vault',
    vault_secret: 'GH_TOKEN', sign_tags: true, signing_key: 'K1' });
  assert.equal(groups[1].credential_source, 'host_login');
  assert.equal(groups[1].base_url, 'https://gl.example');
  assert.deepEqual(G.hostsFromConfig({}), []);
  assert.deepEqual(G.hostsFromConfig(null), []);
});

test('readinessRowsFromEntries flattens GET /api/git/hosts entries keyed by name', () => {
  const rows = G.readinessRowsFromEntries({ hosts: [
    { name: 'gh', kind: 'github', readiness: { logged_in: true, account: 'octocat', has_repo_scope: true } },
    { name: 'gl', kind: 'gitlab', readiness: { logged_in: false, problem: 'not logged in', fix_command: 'glab auth login' } },
  ] });
  assert.equal(rows.length, 2);
  assert.equal(rows[0].host, 'gh');
  assert.equal(rows[0].account, 'octocat');
  assert.equal(rows[1].fix_command, 'glab auth login');
  assert.deepEqual(G.readinessRowsFromEntries({}), []);
});

test('buildHostsPatch returns a clean git.hosts value for valid groups', () => {
  const res = G.buildHostsPatch([
    { name: ' gh ', kind: 'github', base_url: 'https://github.com', credential_source: 'host_login', vault_secret: 'ignored', sign_tags: false },
    { name: 'corp', kind: 'gitea', base_url: ' https://git.example ', credential_source: 'vault', vault_secret: ' CORP_TOKEN ', sign_tags: true, signing_key: 'KEY' },
  ]);
  assert.deepEqual(res.errors, []);
  assert.deepEqual(res.value, [
    { name: 'gh', kind: 'github', credential_source: 'host_login', sign_tags: false },
    { name: 'corp', kind: 'gitea', credential_source: 'vault', sign_tags: true,
      base_url: 'https://git.example', vault_secret: 'CORP_TOKEN', signing_key: 'KEY' },
  ]);
});

test('buildHostsPatch drops fields hidden for the kind or source', () => {
  const res = G.buildHostsPatch([
    { name: 'gh', kind: 'github', base_url: 'https://x', credential_source: 'broker', vault_secret: 'leftover' },
  ]);
  assert.deepEqual(res.errors, []);
  assert.equal(res.value[0].base_url, undefined);
  assert.equal(res.value[0].vault_secret, undefined);
});

test('buildHostsPatch reports each validation problem and returns no value', () => {
  const res = G.buildHostsPatch([
    { name: '', kind: 'github' },
    { name: 'dup', kind: 'gitea', base_url: '' },
    { name: 'dup', kind: 'github' },
    { name: 'v', kind: 'gitlab', credential_source: 'vault', vault_secret: '' },
    { name: 'k', kind: 'svn' },
    { name: 's', kind: 'github', credential_source: 'pigeon' },
  ]);
  assert.equal(res.value, null);
  const keys = res.errors.map((e) => e.key);
  assert.deepEqual(keys, [
    'git_hosts_err_name',
    'git_hosts_err_base_url',
    'git_hosts_err_duplicate',
    'git_hosts_err_vault',
    'git_hosts_err_kind',
    'git_hosts_err_source',
  ]);
  assert.equal(res.errors[0].index, 0);
  assert.equal(res.errors[2].index, 2);
});

test('buildHostsPatch never carries a token-shaped value into the patch', () => {
  const res = G.buildHostsPatch([
    { name: 'gh', kind: 'github', credential_source: 'vault', vault_secret: 'GH_TOKEN_OPERATOR' },
  ]);
  const text = JSON.stringify(res.value);
  assert.ok(text.includes('GH_TOKEN_OPERATOR'));
  assert.ok(!/gh[pousr]_[A-Za-z0-9]/.test(text));
});

test('testResultKey maps the connection result to a label key', () => {
  assert.equal(G.testResultKey({ ok: true }), 'git_hosts_test_ok');
  assert.equal(G.testResultKey({ ok: false }), 'git_hosts_test_failed');
  assert.equal(G.testResultKey(null), 'git_hosts_test_failed');
});

test('every git_hosts_ key the card uses exists in all five locale bundles', () => {
  const sources = ['app.js', 'app-git-hosts.js'].map((f) => fs.readFileSync(path.join(WEB, f), 'utf8')).join('\n');
  const found = new Set(sources.match(/git_hosts_[a-z_]+/g) || []);
  // Dynamic lookups are built from a prefix plus a value; check every value.
  const dynamic = ['git_hosts_kind_', 'git_hosts_state_', 'git_hosts_scopes_', 'git_hosts_source_'];
  const values = {
    git_hosts_kind_: G.GIT_HOST_KINDS.concat(['unknown']),
    git_hosts_state_: ['ready', 'not_logged_in', 'unknown'],
    git_hosts_scopes_: ['ok', 'missing', 'unknown'],
    git_hosts_source_: G.GIT_CREDENTIAL_SOURCES,
  };
  for (const p of dynamic) {
    found.delete(p);
    for (const v of values[p]) found.add(p + v);
  }
  assert.ok(found.size > 40, 'expected the card to use many git_hosts_ keys');
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', lang + '.json'), 'utf8'));
    for (const key of found) {
      assert.ok(typeof bundle[key] === 'string' && bundle[key] !== '', `${lang}.json missing ${key}`);
    }
  }
});
