'use strict';
// Git hosts card — pure render functions.
//
// Covers card visibility, the row data for ready / not-logged-in / unknown
// hosts, fix-command passthrough, token redaction (no token, vault secret or
// base URL ever leaves the row), and that every label goes through t() by key.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-git-hosts.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const { shouldShowGitHostsCard, renderHostRow } = require('./app-git-hosts.js');

const HOST = {
  name: 'github.com',
  kind: 'github',
  base_url: 'https://github.com',
  credential_source: 'vault',
  vault_secret: 'GH_TOKEN_OPERATOR',
};

const READY = {
  host: 'github.com',
  kind: 'github',
  logged_in: true,
  account: 'octocat',
  scopes: ['repo', 'workflow'],
  has_repo_scope: true,
  has_workflow_scope: true,
};

// withT installs a fake global t() that records every key it is asked for.
function withT(fn) {
  const seen = [];
  const prev = globalThis.t;
  globalThis.t = (key, vars) => { seen.push({ key, vars }); return `<${key}>`; };
  try {
    return { result: fn(), seen };
  } finally {
    if (prev === undefined) delete globalThis.t;
    else globalThis.t = prev;
  }
}

test('card is hidden with no hosts and no readiness problem', () => {
  assert.equal(shouldShowGitHostsCard([], []), false);
  assert.equal(shouldShowGitHostsCard(undefined, undefined), false);
  assert.equal(shouldShowGitHostsCard(null, [READY]), false);
});

test('card shows when at least one host is configured', () => {
  assert.equal(shouldShowGitHostsCard([HOST], []), true);
  assert.equal(shouldShowGitHostsCard([HOST], null), true);
});

test('card shows with no host when a readiness row reports a problem', () => {
  const problem = { host: 'gitlab.example', kind: 'gitlab', logged_in: false, problem: 'not logged in' };
  assert.equal(shouldShowGitHostsCard([], [problem]), true);
});

test('a readiness row without a problem does not show the card on its own', () => {
  assert.equal(shouldShowGitHostsCard([], [READY]), false);
});

test('an off or empty readiness status is never rendered', () => {
  const problem = { host: 'x', problem: 'broken' };
  assert.equal(shouldShowGitHostsCard([HOST], { status: 'off', rows: [problem] }), false);
  assert.equal(shouldShowGitHostsCard([HOST], { status: '' }), false);
  assert.equal(shouldShowGitHostsCard([], { status: 'off', rows: [problem] }), false);
});

test('ready host row carries name, kind, account and scope status', () => {
  const { result, seen } = withT(() => renderHostRow(HOST, [READY]));
  assert.equal(result.name, 'github.com');
  assert.equal(result.kind, 'github');
  assert.equal(result.state, 'ready');
  assert.equal(result.account, 'octocat');
  assert.equal(result.scopes, 'ok');
  assert.deepEqual(result.scopeList, ['repo', 'workflow']);
  assert.equal(result.fixCommand, '');
  assert.equal(result.fixLabel, '');
  const keys = seen.map((s) => s.key);
  assert.ok(keys.includes('git_hosts_kind_github'));
  assert.ok(keys.includes('git_hosts_state_ready'));
  assert.ok(keys.includes('git_hosts_scopes_ok'));
});

test('labels are looked up by key through t() with their variables', () => {
  const { result, seen } = withT(() => renderHostRow(HOST, [READY]));
  const account = seen.find((s) => s.key === 'git_hosts_account');
  assert.deepEqual(account.vars, { account: 'octocat' });
  assert.equal(result.accountLabel, '<git_hosts_account>');
  assert.equal(result.titleLabel, '<git_hosts_row_title>');
});

test('without t() the key is returned, so the module still runs under node', () => {
  const prev = globalThis.t;
  delete globalThis.t;
  try {
    const row = renderHostRow(HOST, [READY]);
    assert.equal(row.stateLabel, 'git_hosts_state_ready');
    assert.equal(row.kindLabel, 'git_hosts_kind_github');
  } finally {
    if (prev !== undefined) globalThis.t = prev;
  }
});

test('not-logged-in host shows the fix command and its label', () => {
  const row = {
    host: 'github.com', kind: 'github', logged_in: false,
    fix_command: 'gh auth login --hostname github.com',
    problem: 'not logged in',
  };
  const { result } = withT(() => renderHostRow(HOST, [row]));
  assert.equal(result.state, 'not_logged_in');
  assert.equal(result.fixCommand, 'gh auth login --hostname github.com');
  assert.equal(result.fixLabel, '<git_hosts_fix_label>');
  assert.equal(result.problem, 'not logged in');
});

test('host without a readiness row is unknown with no account or fix', () => {
  const { result } = withT(() => renderHostRow(HOST, []));
  assert.equal(result.state, 'unknown');
  assert.equal(result.account, '');
  assert.equal(result.scopes, 'unknown');
  assert.equal(result.fixCommand, '');
});

test('scope status is missing when scope data exists but repo scope is absent', () => {
  const row = { host: 'github.com', logged_in: true, account: 'octocat', scopes: ['gist'] };
  const { result } = withT(() => renderHostRow(HOST, [row]));
  assert.equal(result.scopes, 'missing');
  assert.equal(result.scopesLabel, '<git_hosts_scopes_missing>');
});

test('row matches by hostname of the base URL when the name differs', () => {
  const host = { name: 'work-gh', kind: 'github', base_url: 'https://github.com/' };
  const { result } = withT(() => renderHostRow(host, [READY]));
  assert.equal(result.state, 'ready');
  assert.equal(result.account, 'octocat');
});

test('a token in a problem string is redacted', () => {
  const row = {
    host: 'github.com', logged_in: false,
    problem: 'token ghp_abcdef1234567890 rejected for https://user:hunter2@example.invalid',
  };
  const { result } = withT(() => renderHostRow(HOST, [row]));
  assert.ok(!result.problem.includes('ghp_abcdef1234567890'));
  assert.ok(!result.problem.includes('hunter2'));
  assert.ok(result.problem.includes('[redacted]'));
});

test('row never exposes the token source, vault secret or base URL', () => {
  const { result } = withT(() => renderHostRow(HOST, [READY]));
  const dump = JSON.stringify(result);
  assert.ok(!dump.includes('GH_TOKEN_OPERATOR'), 'vault secret leaked');
  assert.ok(!dump.includes('https://github.com'), 'base URL leaked');
  assert.ok(!('vault_secret' in result));
  assert.ok(!('base_url' in result));
  assert.ok(!('credential_source' in result));
  assert.ok(!('token' in result));
});

test('missing host argument does not throw', () => {
  const { result } = withT(() => renderHostRow(undefined, undefined));
  assert.equal(result.name, '');
  assert.equal(result.state, 'unknown');
});
