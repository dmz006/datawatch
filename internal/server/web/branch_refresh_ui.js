// Branch refresh UI — PWA side.
//
// Settings page: branchRefreshSettingsFields() gives the two daemon settings
// (autonomous.branch_refresh_enabled toggle, autonomous.branch_refresh_behind_limit
// number input) for the Autonomous settings list.
//
// Automaton page: branchRefreshPanelHtml(prd) shows the last refresh time, the
// commits behind main, the last result, whether refreshing is stopped by a
// conflict and the per-Automaton override select.
// The select posts to POST /api/autonomous/prds/{id}/branch-refresh with
// enabled true, false or null (null clears the override). Every value is escaped.

window.branchRefreshSettingsFields = function() {
  return [
    { key: 'autonomous.branch_refresh_enabled', label: 'Refresh Automaton branches from main between tasks', labelKey: 'settings_branch_refresh_enabled', helpKey: 'settings_branch_refresh_enabled_help', type: 'toggle' },
    { key: 'autonomous.branch_refresh_behind_limit', label: 'Branch refresh commits-behind limit (0 = default)', labelKey: 'settings_branch_refresh_behind_limit', helpKey: 'settings_branch_refresh_behind_limit_help', type: 'number', placeholder: '0', min: 0, step: 1 },
  ];
};

// branchRefreshOverrideValue maps the PRD override (true, false or null) to the
// select value ('on', 'off' or '' for the daemon setting).
window.branchRefreshOverrideValue = function(prd) {
  const o = prd && prd.branch_refresh_override;
  if (o === true) return 'on';
  if (o === false) return 'off';
  return '';
};

window.branchRefreshResultLabel = function(result) {
  switch (result) {
    case 'clean': return t('branch_refresh_result_clean');
    case 'conflict': return t('branch_refresh_result_conflict');
    case 'skipped': return t('branch_refresh_result_skipped');
    case 'error': return t('branch_refresh_result_error');
    default: return t('branch_refresh_result_none');
  }
};

window.branchRefreshPanelHtml = function(prd) {
  if (!prd || typeof prd !== 'object' || !prd.id) return '';
  const br = (prd.branch_refresh && typeof prd.branch_refresh === 'object') ? prd.branch_refresh : null;
  const when = br && br.last_refresh_at ? (typeof timeAgo === 'function' ? timeAgo(br.last_refresh_at) : String(br.last_refresh_at)) : t('branch_refresh_never');
  const behind = br ? String(Math.max(0, Number(br.commits_behind) || 0)) : '—';
  const result = br ? window.branchRefreshResultLabel(br.last_result) : t('branch_refresh_result_none');
  const stopped = !!(br && br.stopped);
  const state = t(stopped ? 'branch_refresh_state_stopped' : 'branch_refresh_state_active');
  const sel = window.branchRefreshOverrideValue(prd);
  const opt = (v, key) => `<option value="${v}"${sel === v ? ' selected' : ''}>${escHtml(t(key))}</option>`;
  const row = (k, v, cls) => `<div class="branch-refresh-${cls}" style="display:flex;gap:6px;font-size:12px;"><span style="color:var(--text2);">${escHtml(t(k))}</span><span>${escHtml(v)}</span></div>`;
  return `<div class="branch-refresh-panel prd-capacity-card" data-prd="${escHtml(prd.id)}" style="margin-top:8px;min-width:0;">` +
    `<div class="cap-title">${escHtml(t('branch_refresh_card_title'))}</div>` +
    row('branch_refresh_last_refresh', when, 'last') +
    row('branch_refresh_commits_behind', behind, 'behind') +
    row('branch_refresh_last_result', result, 'result') +
    row('branch_refresh_state_label', state, 'state') +
    `<div style="margin-top:4px;"><label class="wizard-label" for="branchRefreshOverride">${escHtml(t('branch_refresh_override_label'))}</label>` +
    `<select id="branchRefreshOverride" class="form-select" onchange="branchRefreshSetOverride('${escHtml(String(prd.id))}', this.value)">` +
    opt('', 'branch_refresh_override_default') + opt('on', 'branch_refresh_override_on') + opt('off', 'branch_refresh_override_off') +
    `</select></div></div>`;
};

window.branchRefreshSetOverride = function(id, value) {
  const enabled = value === 'on' ? true : (value === 'off' ? false : null);
  return apiFetch('/api/autonomous/prds/' + encodeURIComponent(id) + '/branch-refresh', {
    method: 'POST', headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({ enabled }),
  }).then(() => {
    if (typeof showToast === 'function') showToast(t('branch_refresh_override_saved'));
    if (typeof renderPRDDetailView === 'function') { try { renderPRDDetailView(id); } catch (e) { /* view not open */ } }
  }).catch(err => {
    if (typeof showToast === 'function') showToast(String((err && err.message) || err), 'error');
  });
};
