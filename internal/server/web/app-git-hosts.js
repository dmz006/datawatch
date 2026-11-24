'use strict';
// Git hosts card — PWA render logic (pure, no DOM access).
//
// Turns the configured git hosts (GET /api/git/hosts entries) and their
// readiness (internal/git HostReadiness rows) into plain data the card can
// draw. Nothing here touches the document: callers take the returned objects
// and build markup with their own escaping.
//
//   shouldShowGitHostsCard(hosts, readiness) -> boolean
//   renderHostRow(host, readiness)           -> row data object
//
// Rules:
//   - The card is hidden when no host is configured and no readiness row
//     reports a problem. A readiness status of "off" or "" hides it outright.
//   - A row carries the host name, kind, account, scope status and, when the
//     daemon supplied one, the fix command. It never carries a token, the
//     vault secret reference or the base URL (which may embed credentials).
//   - Every label is looked up by key through the global t() helper. In the
//     browser that is app.js's t(); under node the key itself is returned.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-git-hosts.test.js

// tr looks a label up through t() by key. Outside the browser (node tests)
// there is no t(), so the key comes back unchanged.
function tr(key, vars) {
  if (typeof t === 'function') return t(key, vars);
  return key;
}

// Strips anything token-shaped from free text before it reaches a row.
const TOKEN_PATTERNS = [
  /gh[pousr]_[A-Za-z0-9_]+/g,
  /github_pat_[A-Za-z0-9_]+/g,
  /glpat-[A-Za-z0-9_-]+/g,
  /:\/\/[^/@\s]+:[^/@\s]+@/g,
];
function redactText(s) {
  let out = String(s || '');
  for (const re of TOKEN_PATTERNS) out = out.replace(re, '[redacted]');
  return out;
}

// readinessStatus returns the status a readiness payload carries, if any.
// readiness may be an array of rows or null; a bare object with a status
// field is accepted as well.
function readinessStatus(readiness) {
  if (readiness && !Array.isArray(readiness) && typeof readiness === 'object' && 'status' in readiness) {
    return String(readiness.status || '');
  }
  return null;
}

function readinessRows(readiness) {
  if (Array.isArray(readiness)) return readiness.filter((r) => r && typeof r === 'object');
  if (readiness && typeof readiness === 'object' && Array.isArray(readiness.rows)) {
    return readiness.rows.filter((r) => r && typeof r === 'object');
  }
  return [];
}

// hostnameOf extracts the host part of a base URL without the URL global.
function hostnameOf(base) {
  const s = String(base || '').trim();
  if (!s) return '';
  const m = s.replace(/^[a-z]+:\/\//i, '').split(/[/?#]/)[0];
  return m.replace(/^[^@]*@/, '');
}

// rowFor returns the readiness row that belongs to a host entry: matched by
// the entry name first, then by the hostname of its base URL.
function rowFor(host, readiness) {
  const rows = readinessRows(readiness);
  const name = String(host && host.name || '');
  const hn = hostnameOf(host && host.base_url);
  return rows.find((r) => r.host === name)
    || (hn ? rows.find((r) => r.host === hn) : undefined)
    || null;
}

// shouldShowGitHostsCard reports whether the git hosts card has anything to say.
function shouldShowGitHostsCard(hosts, readiness) {
  const status = readinessStatus(readiness);
  if (status === 'off' || status === '') return false;
  const hostList = Array.isArray(hosts) ? hosts.filter(Boolean) : [];
  if (hostList.length > 0) return true;
  return readinessRows(readiness).some((r) => String(r.problem || '').trim() !== '');
}

// scopeState: 'ok' when the repo scope is present, 'missing' when scope data
// exists but repo is absent, 'unknown' when the daemon reported no scope data.
function scopeState(r) {
  const hasScopeData = (Array.isArray(r.scopes) && r.scopes.length > 0)
    || r.has_repo_scope === true || r.has_workflow_scope === true || r.has_api_scope === true;
  if (!hasScopeData) return 'unknown';
  return r.has_repo_scope === true ? 'ok' : 'missing';
}

// renderHostRow returns the data for one host row. readiness is the
// HostReadiness array for all hosts; the matching row is picked by name.
function renderHostRow(host, readiness) {
  const h = host || {};
  const r = rowFor(h, readiness);
  const name = String(h.name || '');
  const kind = String(h.kind || '');

  let state;
  if (!r) state = 'unknown';
  else state = r.logged_in === true ? 'ready' : 'not_logged_in';

  const account = r && r.account ? String(r.account) : '';
  const scopes = r ? scopeState(r) : 'unknown';
  const scopeList = r && Array.isArray(r.scopes) ? r.scopes.map((s) => String(s)) : [];
  const fixCommand = r && r.fix_command ? String(r.fix_command) : '';
  const problem = r && r.problem ? redactText(r.problem) : '';

  return {
    name,
    kind,
    kindLabel: tr('git_hosts_kind_' + (kind || 'unknown')),
    state,
    stateLabel: tr('git_hosts_state_' + state),
    account,
    accountLabel: account ? tr('git_hosts_account', { account }) : tr('git_hosts_account_none'),
    scopes,
    scopesLabel: tr('git_hosts_scopes_' + scopes),
    scopeList,
    problem,
    fixCommand,
    fixLabel: fixCommand ? tr('git_hosts_fix_label') : '',
    titleLabel: tr('git_hosts_row_title', { name }),
  };
}

// ---- Settings editor helpers (git.hosts editing card) --------------------

const GIT_HOST_KINDS = ['github', 'gitlab', 'gitea', 'bitbucket', 'git'];
const GIT_CREDENTIAL_SOURCES = ['host_login', 'vault', 'broker'];
const GIT_HOST_HELP_URL = '/diagrams.html#docs/howto/connect-git-host.md';

// baseUrlVisible: the base URL field is shown for every kind except github,
// whose base URL is always github.com.
function baseUrlVisible(kind) {
  return String(kind || '') !== 'github';
}

// baseUrlRequired: gitea, bitbucket and git have no public default, so the
// base URL must be set. gitlab only needs one when self-hosted.
function baseUrlRequired(kind) {
  return ['gitea', 'bitbucket', 'git'].includes(String(kind || ''));
}

// vaultSecretVisible: the vault secret name is shown only for the vault source.
function vaultSecretVisible(source) {
  return String(source || '') === 'vault';
}

// hostHelpUrl is the help link target for the card.
function hostHelpUrl() {
  return GIT_HOST_HELP_URL;
}

// hostTestPath is the POST path for one host's connection test.
function hostTestPath(name) {
  return '/api/git/hosts/' + encodeURIComponent(String(name || '')) + '/test';
}

// hostsFromConfig turns GET /api/config's git.hosts into editable groups.
// Only the editable fields and the signing fields the save must carry back
// are kept; an empty credential source reads as host_login.
function hostsFromConfig(cfg) {
  const list = cfg && cfg.git && Array.isArray(cfg.git.hosts) ? cfg.git.hosts : [];
  return list.filter((h) => h && typeof h === 'object').map((h) => ({
    name: String(h.name || ''),
    kind: String(h.kind || 'github'),
    base_url: String(h.base_url || ''),
    credential_source: String(h.credential_source || 'host_login'),
    vault_secret: String(h.vault_secret || ''),
    sign_tags: h.sign_tags === true,
    signing_key: String(h.signing_key || ''),
  }));
}

// readinessRowsFromEntries flattens GET /api/git/hosts entries
// ({name, kind, readiness:{...}}) into the HostReadiness-shaped rows
// renderHostRow reads, each keyed by the entry name.
function readinessRowsFromEntries(payload) {
  const entries = payload && Array.isArray(payload.hosts) ? payload.hosts : [];
  return entries.filter((e) => e && typeof e === 'object').map((e) => {
    const r = e.readiness && typeof e.readiness === 'object' ? e.readiness : {};
    return Object.assign({}, r, { host: String(e.name || r.host || '') });
  });
}

// buildHostsPatch validates the edited groups and returns the git.hosts value
// for PUT /api/config. It returns {value, errors}: errors is a list of
// {index, key} where key is a git_hosts_ i18n key; value is null when any
// error is present. Credentials never enter this object: only the vault
// secret NAME is carried.
function buildHostsPatch(groups) {
  const list = Array.isArray(groups) ? groups : [];
  const errors = [];
  const seen = new Set();
  const value = [];
  list.forEach((g, index) => {
    const row = g || {};
    const name = String(row.name || '').trim();
    const kind = String(row.kind || '').trim();
    const source = String(row.credential_source || 'host_login').trim() || 'host_login';
    const base = String(row.base_url || '').trim();
    const vault = String(row.vault_secret || '').trim();
    if (!name) errors.push({ index, key: 'git_hosts_err_name' });
    else if (seen.has(name)) errors.push({ index, key: 'git_hosts_err_duplicate' });
    seen.add(name);
    if (!GIT_HOST_KINDS.includes(kind)) errors.push({ index, key: 'git_hosts_err_kind' });
    if (!GIT_CREDENTIAL_SOURCES.includes(source)) errors.push({ index, key: 'git_hosts_err_source' });
    if (baseUrlRequired(kind) && !base) errors.push({ index, key: 'git_hosts_err_base_url' });
    if (vaultSecretVisible(source) && !vault) errors.push({ index, key: 'git_hosts_err_vault' });
    const entry = {
      name,
      kind,
      credential_source: source,
      sign_tags: row.sign_tags === true,
    };
    if (baseUrlVisible(kind) && base) entry.base_url = base;
    if (vaultSecretVisible(source)) entry.vault_secret = vault;
    const signingKey = String(row.signing_key || '').trim();
    if (signingKey) entry.signing_key = signingKey;
    value.push(entry);
  });
  return { value: errors.length ? null : value, errors };
}

// testResultKey picks the i18n key for a connection-test result.
function testResultKey(result) {
  return result && result.ok === true ? 'git_hosts_test_ok' : 'git_hosts_test_failed';
}

const api = {
  shouldShowGitHostsCard, renderHostRow,
  GIT_HOST_KINDS, GIT_CREDENTIAL_SOURCES,
  baseUrlVisible, baseUrlRequired, vaultSecretVisible, hostHelpUrl, hostTestPath,
  hostsFromConfig, readinessRowsFromEntries, buildHostsPatch, testResultKey,
};

if (typeof module !== 'undefined' && module.exports) module.exports = api;
if (typeof window !== 'undefined') window.gitHostsCard = api;
