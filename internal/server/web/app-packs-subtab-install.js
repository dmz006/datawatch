// app-packs-subtab-install.js - install, summary and acceptance pieces of the
// Packs sub-tab. Loaded after app-packs-subtab.js. It renders HTML strings and
// calls the packs REST routes; it keeps no pack list of its own, so every
// function takes the pack entry the caller read from the registry or from
// GET /api/packs.
//
//   install action  -> POST /api/packs/install {source_dir}
//   accept button   -> POST /api/packs/accept  {name, version}
//
// A pack entry is {name, version, source_dir?, accepted?, summary?, diff?}.
// summary is {task_titles, verification_scripts, tools, file_scopes}; diff is
// {new_tasks, new_scripts, new_tools, new_file_scopes}, the additions over the
// version the operator accepted before.

function _packsInstT(key, fallback) {
  try {
    const v = (typeof t === 'function') ? t(key) : key;
    return (v && v !== key) ? v : fallback;
  } catch (_) { return fallback; }
}

function _packsInstEsc(s) {
  return (typeof escHtml === 'function') ? escHtml(s) : String(s == null ? '' : s)
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

function _packsInstList(v) { return Array.isArray(v) ? v.filter(x => x != null && x !== '') : []; }

// packsInstallPayload builds the install request body from a registry entry.
// It returns null when the entry has no source folder to install from.
function packsInstallPayload(pack) {
  const dir = pack && typeof pack.source_dir === 'string' ? pack.source_dir.trim() : '';
  return dir ? { source_dir: dir } : null;
}

// packsAcceptPayload builds the accept request body, or null when the pack has
// no name or version.
function packsAcceptPayload(pack) {
  if (!pack || !pack.name || !pack.version) return null;
  return { name: String(pack.name), version: String(pack.version) };
}

// packsInstallButtonHTML is the install action for a registry pack. The pack is
// found again by index so no path is placed in an inline handler.
function packsInstallButtonHTML(pack, idx) {
  const e = _packsInstEsc;
  if (!packsInstallPayload(pack)) return '';
  return '<button class="btn-primary packs-install-btn" onclick="packsSubtabInstall(' + Number(idx)
    + ',event)">' + e(_packsInstT('packs_install', 'Install')) + '</button>';
}

// packsSummaryHTML lists what a pack adds: its tasks, verification scripts,
// tools and file scopes.
function packsSummaryHTML(summary) {
  const e = _packsInstEsc;
  summary = summary || {};
  const rows = [
    ['packs_summary_tasks', 'Tasks', summary.task_titles],
    ['packs_summary_scripts', 'Verification scripts', summary.verification_scripts],
    ['packs_summary_tools', 'Tools', summary.tools],
    ['packs_summary_scopes', 'File scopes', summary.file_scopes],
  ];
  let out = '<div class="packs-summary" style="font-size:12px;">'
    + '<div class="packs-summary-head" style="font-weight:600;">'
    + e(_packsInstT('packs_summary_head', 'This pack adds')) + '</div>';
  rows.forEach(r => {
    const items = _packsInstList(r[2]);
    out += '<div class="packs-summary-row"><span>' + e(_packsInstT(r[0], r[1])) + ':</span> '
      + (items.length ? items.map(e).join(', ')
        : '<em>' + e(_packsInstT('packs_summary_none', 'none')) + '</em>') + '</div>';
  });
  return out + '</div>';
}

// packsDiffHTML lists the changes a new version makes over the accepted one.
// It returns an empty string when the version adds nothing.
function packsDiffHTML(diff) {
  const e = _packsInstEsc;
  diff = diff || {};
  const rows = [
    ['packs_diff_tasks', 'New tasks', diff.new_tasks, false],
    ['packs_diff_scripts', 'New verification scripts', diff.new_scripts, true],
    ['packs_diff_tools', 'New tools', diff.new_tools, true],
    ['packs_diff_scopes', 'New file scopes', diff.new_file_scopes, true],
  ];
  let any = false;
  let out = '<div class="packs-diff" style="font-size:12px;">'
    + '<div class="packs-diff-head" style="font-weight:600;">'
    + e(_packsInstT('packs_diff_head', 'Changes in this version')) + '</div>';
  rows.forEach(r => {
    const items = _packsInstList(r[2]);
    if (!items.length) return;
    any = true;
    out += '<div class="packs-diff-row' + (r[3] ? ' packs-diff-widens' : '') + '"'
      + (r[3] ? ' style="color:var(--warning,#f59e0b);"' : '') + '><span>' + e(_packsInstT(r[0], r[1]))
      + ':</span> ' + items.map(e).join(', ') + '</div>';
  });
  return any ? out + '</div>' : '';
}

// packsAcceptButtonHTML is the accept button, shown only while the pack is not
// accepted.
function packsAcceptButtonHTML(pack, idx) {
  const e = _packsInstEsc;
  if (!packsAcceptPayload(pack) || pack.accepted) return '';
  return '<button class="btn-primary packs-accept-btn" onclick="packsSubtabAccept(' + Number(idx)
    + ',event)">' + e(_packsInstT('packs_accept', 'Accept')) + '</button>';
}

// packsPendingHTML renders one pack's summary, the version change list and the
// accept button for a pending acceptance.
function packsPendingHTML(pack, idx) {
  const e = _packsInstEsc;
  pack = pack || {};
  return '<div class="packs-pending" style="padding:8px 0;">'
    + '<div class="packs-pending-title" style="font-weight:600;">' + e(pack.name) + ' '
    + e(pack.version) + '</div>'
    + packsSummaryHTML(pack.summary) + packsDiffHTML(pack.diff)
    + '<p>' + packsAcceptButtonHTML(pack, idx) + '</p></div>';
}

const PACKS_INSTALL_LIST = [];

function _packsInstPost(path, body) {
  return apiFetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
}

// packsSubtabInstall installs the pack at idx of PACKS_INSTALL_LIST.
function packsSubtabInstall(idx, ev) {
  if (ev && ev.preventDefault) ev.preventDefault();
  const body = packsInstallPayload(PACKS_INSTALL_LIST[idx]);
  if (!body) return Promise.resolve(null);
  return _packsInstPost('/api/packs/install', body).then(r => {
    if (typeof showToast === 'function') showToast(_packsInstT('packs_installed', 'Pack installed'), 'success');
    if (typeof packsSubtabLoad === 'function') packsSubtabLoad();
    return r;
  }).catch(err => {
    if (typeof showToast === 'function') showToast((err && err.message) || String(err), 'error');
    return null;
  });
}

// packsSubtabAccept records acceptance of the pack at idx of PACKS_INSTALL_LIST.
function packsSubtabAccept(idx, ev) {
  if (ev && ev.preventDefault) ev.preventDefault();
  const body = packsAcceptPayload(PACKS_INSTALL_LIST[idx]);
  if (!body) return Promise.resolve(null);
  return _packsInstPost('/api/packs/accept', body).then(r => {
    if (typeof showToast === 'function') showToast(_packsInstT('packs_accepted', 'Pack accepted'), 'success');
    if (typeof packsSubtabLoad === 'function') packsSubtabLoad();
    return r;
  }).catch(err => {
    if (typeof showToast === 'function') showToast((err && err.message) || String(err), 'error');
    return null;
  });
}

window.PACKS_INSTALL_LIST = PACKS_INSTALL_LIST;
window.packsInstallPayload = packsInstallPayload;
window.packsAcceptPayload = packsAcceptPayload;
window.packsInstallButtonHTML = packsInstallButtonHTML;
window.packsSummaryHTML = packsSummaryHTML;
window.packsDiffHTML = packsDiffHTML;
window.packsAcceptButtonHTML = packsAcceptButtonHTML;
window.packsPendingHTML = packsPendingHTML;
window.packsSubtabInstall = packsSubtabInstall;
window.packsSubtabAccept = packsSubtabAccept;
