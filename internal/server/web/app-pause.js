// Operator pause — PWA side.
//
// A header button always shows whether new work is paused (a dim pause icon
// when nothing is, a highlighted "Paused" badge when something is). Clicking
// it opens a modal that lists the global pause and every project pause with
// who paused, why and when, and lets the operator pause (global or one
// project, with a required reason) or resume (one scope, or everything). The
// daemon decides who may resume; this page only sends the request and shows
// the daemon's refusal text. State comes from GET /api/pause and is polled.
// The pause is for automation work (Automaton starts, restarts and worker
// tasks); see pauseTargetOptions for which server a pause goes to.

const PAUSE_REFRESH_MS = 15000;
const PAUSE_MODAL_ID = 'pauseModal';
window._pause = window._pause || { state: null, error: '', message: '' };

// pauseSummary: { paused, count } from a state body; tolerant of a missing body.
window.pauseSummary = function (state) {
  const s = state && typeof state === 'object' ? state : {};
  const projects = Array.isArray(s.projects) ? s.projects : [];
  const count = projects.length + (s.global ? 1 : 0);
  return { paused: !!s.paused && count > 0, count };
};

window.pauseBadgeText = function (state) {
  const sum = window.pauseSummary(state);
  if (!sum.paused) return '';
  return _pauseT('pause_badge_paused', 'Paused') + (sum.count > 1 ? ' ' + sum.count : '');
};

function _pauseEsc(v) { return typeof escHtml === 'function' ? escHtml(v) : String(v); }
// _pauseT looks a string up with the English default. The poll starts at
// DOMContentLoaded, before the locale bundle has loaded, and t() returns the key
// itself for a key the bundle lacks, so `t(k) || d` would show the raw key: tOr
// treats a returned key as missing. The repaint on ready (below) redraws the
// text in the active locale.
function _pauseT(k, d) {
  if (typeof window.tOr === 'function') return window.tOr(k, d);
  const s = typeof t === 'function' ? t(k) : k;
  return s && s !== k ? s : d;
}

// Where a pause goes. The default is this server only. When the header server
// selector is on All, the window offers each server (the same enabled list the
// selector shows) or all servers; any other view pauses the server it is on, as
// before. A pause on this server never pauses another server by itself: each
// server keeps its own state and its own work.
window.pauseTargetOptions = function () {
  if (typeof state === 'undefined' || !state || state.activeServer !== 'all') return [];
  const servers = typeof _pickerServers === 'function' ? _pickerServers() : [];
  return [{ value: '', label: _pauseT('pause_target_local', 'This server only') }]
    .concat(servers.map(s => ({ value: s.name, label: s.label || s.name })))
    .concat([{ value: 'all', label: _pauseT('pause_target_all', 'All servers') }]);
};

function _pauseTarget() {
  const want = (window._pause && window._pause.target) || '';
  return window.pauseTargetOptions().some(o => o.value === want) ? want : '';
}

// A single server is reached through the proxy, the way connecting to it would.
function _pauseRoute(path, target) {
  return target && target !== 'all' ? '/api/proxy/' + encodeURIComponent(target) + path : path;
}

window.pauseSetTarget = function (value) {
  window._pause.target = value || '';
  return window.pauseRefresh();
};

// pausePeersHtml lists what each server answered (state or error).
window.pausePeersHtml = function (peers) {
  const names = peers && typeof peers === 'object' ? Object.keys(peers).sort() : [];
  if (!names.length) return '';
  const rows = names.map(n => {
    const p = peers[n] || {};
    const txt = p.ok ? (p.state && p.state.paused ? _pauseT('pause_peer_paused', 'paused') : _pauseT('pause_peer_clear', 'not paused')) : (p.error || '');
    return `<div>${_pauseEsc(n)}: ${_pauseEsc(txt)}</div>`;
  });
  return `<div id="pausePeers" style="font-size:12px;color:var(--text2);">${rows.join('')}</div>`;
};

window.pauseEntryHtml = function (e, isGlobal) {
  if (!e) return '';
  const where = isGlobal ? _pauseT('pause_scope_global', 'All work') : (_pauseT('pause_scope_project', 'Project') + ': ' + (e.project || ''));
  const when = e.at ? new Date(e.at).toLocaleString() : '';
  const resume = isGlobal
    ? `<button class="btn-secondary" onclick="pauseResume('global','')">${_pauseEsc(_pauseT('pause_action_resume', 'Resume'))}</button>`
    : `<button class="btn-secondary" onclick="pauseResume('project',${_pauseEsc(JSON.stringify(e.project || ''))})">${_pauseEsc(_pauseT('pause_action_resume', 'Resume'))}</button>`;
  return `<div class="pause-entry" style="border:1px solid var(--border,#333);border-radius:6px;padding:6px 8px;margin-bottom:6px;display:flex;justify-content:space-between;gap:8px;align-items:center;">`
    + `<div style="min-width:0;"><div style="font-weight:600;color:var(--warning,#f59e0b);">&#9208; ${_pauseEsc(where)}</div>`
    + `<div style="font-size:12px;">${_pauseEsc(e.reason || '')}</div>`
    + `<div style="font-size:11px;color:var(--text2);">${_pauseEsc(_pauseT('pause_by', 'by'))} ${_pauseEsc(e.by || '')} ${_pauseEsc(when)}</div></div>${resume}</div>`;
};

window.pauseBodyHtml = function () {
  const st = window._pause || {};
  const state = st.state || {};
  const entries = [];
  if (state.global) entries.push(window.pauseEntryHtml(state.global, true));
  (Array.isArray(state.projects) ? state.projects : []).forEach(p => entries.push(window.pauseEntryHtml(p, false)));
  const list = entries.length ? entries.join('') : `<div style="color:var(--text2);font-size:12px;">${_pauseEsc(_pauseT('pause_none', 'Nothing is paused'))}</div>`;
  const msg = st.error
    ? `<div class="pause-error" style="color:var(--error,#ef4444);font-size:12px;">${_pauseEsc(st.error)}</div>`
    : (st.message ? `<div class="pause-message" style="color:var(--success,#22c55e);font-size:12px;">${_pauseEsc(st.message)}</div>` : '');
  const inp = 'style="min-width:0;max-width:100%;padding:4px 6px;"';
  const opts = window.pauseTargetOptions();
  const cur = _pauseTarget();
  const picker = opts.length
    ? `<label style="display:flex;flex-direction:column;font-size:12px;">${_pauseEsc(_pauseT('pause_target_label', 'Server'))}<select id="pauseTarget" onchange="pauseSetTarget(this.value)" ${inp}>`
      + opts.map(o => `<option value="${_pauseEsc(o.value)}"${o.value === cur ? ' selected' : ''}>${_pauseEsc(o.label)}</option>`).join('') + `</select></label>`
    : '';
  return `<div id="pauseBody" style="display:flex;flex-direction:column;gap:8px;">`
    + `<div style="font-size:12px;color:var(--text2);">${_pauseEsc(_pauseT('pause_hint', 'A pause stops new work. Running tasks finish. Only the operator can resume.'))}</div>`
    + picker
    + `<div id="pauseList">${list}</div>`
    + window.pausePeersHtml(state.peers)
    + `<label style="display:flex;flex-direction:column;font-size:12px;">${_pauseEsc(_pauseT('pause_project_label', 'Project (optional, empty = all work)'))}<input id="pauseProject" type="text" ${inp}></label>`
    + `<label style="display:flex;flex-direction:column;font-size:12px;">${_pauseEsc(_pauseT('pause_reason_label', 'Reason (required)'))}<input id="pauseReason" type="text" ${inp}></label>`
    + `<div style="display:flex;gap:6px;flex-wrap:wrap;"><button class="btn-secondary" onclick="pauseSet()">${_pauseEsc(_pauseT('pause_action_pause', 'Pause'))}</button>`
    + `<button class="btn-secondary" onclick="pauseResume('all','')">${_pauseEsc(_pauseT('pause_action_resume_all', 'Resume all'))}</button></div>`
    + `<div id="pauseMsg">${msg}</div></div>`;
};

function _pausePaintBadge() {
  if (typeof document === 'undefined') return;
  const btn = document.getElementById('headerPauseBtn');
  if (!btn) return;
  const sum = window.pauseSummary(window._pause.state);
  const label = document.getElementById('headerPauseLabel');
  const text = window.pauseBadgeText(window._pause.state);
  if (label) label.textContent = text;
  btn.classList.toggle('is-paused', sum.paused);
  btn.style.opacity = sum.paused ? '1' : '0.5';
  btn.style.color = sum.paused ? 'var(--warning,#f59e0b)' : 'inherit';
  const title = sum.paused ? _pauseT('pause_badge_title_paused', 'New work is paused') : _pauseT('pause_badge_title_idle', 'Pause new work');
  btn.title = title;
  btn.setAttribute('aria-label', title);
}

function _pauseRepaintModal() {
  if (typeof document === 'undefined') return;
  const holder = document.getElementById('pauseBodyHolder');
  if (holder) {
    const keepP = (document.getElementById('pauseProject') || {}).value || '';
    const keepR = (document.getElementById('pauseReason') || {}).value || '';
    holder.innerHTML = window.pauseBodyHtml();
    const p = document.getElementById('pauseProject'); if (p) p.value = keepP;
    const r = document.getElementById('pauseReason'); if (r) r.value = keepR;
  }
}

window.pauseRefresh = function () {
  const target = _pauseTarget();
  return Promise.resolve(apiFetch(_pauseRoute('/api/pause', target) + (target === 'all' ? '?peers=1' : ''))).then(body => {
    window._pause.state = body; window._pause.error = '';
    _pausePaintBadge(); _pauseRepaintModal();
  }).catch(err => {
    window._pause.error = err && err.message ? err.message : String(err);
    _pauseRepaintModal();
  });
};

// pauseBuildSetBody: {error} for a missing reason, else {body}.
window.pauseBuildSetBody = function (vals) {
  vals = vals || {};
  const reason = String(vals.reason || '').trim();
  if (!reason) return { error: _pauseT('pause_reason_required', 'A reason is required') };
  const project = String(vals.project || '').trim();
  const body = { scope: project ? 'project' : 'global', reason, actor: 'web' };
  if (project) body.project = project;
  const target = _pauseTarget();
  if (target === 'all') body.all_peers = true;
  return { body, path: _pauseRoute('/api/pause', target) };
};

window.pauseSet = function () {
  const v = id => { const e = document.getElementById(id); return e ? e.value : ''; };
  const built = window.pauseBuildSetBody({ project: v('pauseProject'), reason: v('pauseReason') });
  if (built.error) { window._pause.error = built.error; window._pause.message = ''; _pauseRepaintModal(); return Promise.resolve(); }
  return Promise.resolve(apiFetch(built.path, { method: 'POST', body: JSON.stringify(built.body) })).then(body => {
    window._pause.state = body; window._pause.error = ''; window._pause.message = _pauseT('pause_saved', 'Paused');
    _pausePaintBadge(); _pauseRepaintModal();
  }).catch(err => {
    window._pause.error = err && err.message ? err.message : String(err); window._pause.message = '';
    _pauseRepaintModal();
  });
};

// pauseResume(scope, project): scope is global, project or all.
window.pauseResume = function (scope, project) {
  const body = scope === 'all' ? { all: true, actor: 'web' } : { scope, actor: 'web' };
  if (scope === 'project') body.project = project;
  const target = _pauseTarget();
  if (target === 'all') body.all_peers = true;
  return Promise.resolve(apiFetch(_pauseRoute('/api/pause/resume', target), { method: 'POST', body: JSON.stringify(body) })).then(res => {
    window._pause.state = res; window._pause.error = ''; window._pause.message = _pauseT('pause_resumed', 'Resumed');
    _pausePaintBadge(); _pauseRepaintModal();
  }).catch(err => {
    window._pause.error = err && err.message ? err.message : String(err); window._pause.message = '';
    _pauseRepaintModal();
  });
};

window.openPauseModal = function () {
  if (typeof _headerModalShow !== 'function') return;
  _headerModalShow({
    id: PAUSE_MODAL_ID, titleId: 'pauseModalTitle',
    titleText: _pauseT('pause_modal_title', 'Pause new work'),
    bodyHtml: `<div id="pauseBodyHolder">${window.pauseBodyHtml()}</div>`,
    openerId: 'headerPauseBtn', maxWidth: '420px',
  });
  window.pauseRefresh();
};

// pauseRepaintAll redraws the badge and an open pause window from the state
// already fetched, in the language now loaded.
window.pauseRepaintAll = function () { _pausePaintBadge(); _pauseRepaintModal(); };

window.pauseStartPolling = function () {
  if (window._pauseInterval) return;
  window.pauseRefresh();
  window._pauseInterval = setInterval(window.pauseRefresh, PAUSE_REFRESH_MS);
};

if (typeof document !== 'undefined' && document.addEventListener) {
  document.addEventListener('DOMContentLoaded', () => {
    if (!document.getElementById('headerPauseBtn')) return;
    // The first poll can land before the locale bundle has loaded: repaint when it is ready.
    if (typeof window.onI18nReady === 'function') window.onI18nReady(window.pauseRepaintAll);
    window.pauseStartPolling();
  });
}
