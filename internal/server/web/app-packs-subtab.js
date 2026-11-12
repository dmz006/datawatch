// app-packs-subtab.js - the Packs sub-tab under the Templates tab.
// Loaded before app.js. It renders a shell only: when packs are disabled it
// shows an explanation and an Enable button that opens the packs setting; in
// both states it links to the skills registry settings. It keeps no pack list
// of its own. Page helpers (t, escHtml, apiFetch, navigate, switchSettingsTab)
// are resolved when a function runs, so there is no load-order dependency.

const PACKS_SUBTAB_STATE = { status: 'loading', enabled: false, error: '' };

function _packsSubtabT(key, fallback) {
  try {
    const v = (typeof t === 'function') ? t(key) : key;
    return (v && v !== key) ? v : fallback;
  } catch (_) { return fallback; }
}

function _packsSubtabEsc(s) {
  return (typeof escHtml === 'function') ? escHtml(s) : String(s == null ? '' : s)
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

// packsSubtabHTML returns the sub-tab body for a state object.
function packsSubtabHTML(st) {
  st = st || PACKS_SUBTAB_STATE;
  const e = _packsSubtabEsc;
  if (st.status === 'loading') {
    return '<div class="packs-subtab packs-subtab-loading" style="text-align:center;padding:32px;color:var(--text2);">'
      + e(_packsSubtabT('packs_subtab_loading', 'Loading...')) + '</div>';
  }
  if (st.status === 'error') {
    return '<div class="packs-subtab packs-subtab-error"><span style="color:var(--error);">'
      + e(_packsSubtabT('packs_subtab_load_failed', 'Load failed:')) + ' ' + e(st.error) + '</span></div>';
  }
  const link = (cls, fn, label) => '<a href="#" class="' + cls + '" onclick="' + fn
    + ';return false;" style="color:var(--accent2,#60a5fa);">' + e(label) + '</a>';
  const registry = link('packs-registry-link', 'packsSubtabOpenRegistry(event)',
    _packsSubtabT('packs_subtab_registry_link', 'Skills registry settings'));
  let body;
  if (!st.enabled) {
    body = '<p class="packs-subtab-explain">' + e(_packsSubtabT('packs_subtab_disabled',
      'Standard task packs are turned off. Packs add installable standard tasks and Automaton templates from a connected skills registry. Turn them on in Settings; at least one skills registry source must be connected first.')) + '</p>'
      + '<p><button class="btn-primary packs-enable-btn" onclick="packsSubtabOpenSetting(event)">'
      + e(_packsSubtabT('packs_subtab_enable', 'Enable')) + '</button></p>';
  } else {
    body = '<p class="packs-subtab-explain">' + e(_packsSubtabT('packs_subtab_enabled',
      'Standard task packs are on. Pack contents come from the connected skills registry.')) + '</p>';
  }
  return '<div class="packs-subtab" style="padding:12px;font-size:13px;">' + body + '<p>' + registry + '</p></div>';
}

function packsSubtabRender() {
  const panel = (typeof document !== 'undefined') ? document.getElementById('automataPanel') : null;
  if (!panel) return;
  panel.innerHTML = packsSubtabHTML(PACKS_SUBTAB_STATE);
}

// packsSubtabLoad reads only the enabled flag from the packs config read.
function packsSubtabLoad() {
  const st = PACKS_SUBTAB_STATE;
  return apiFetch('/api/packs/config')
    .then(data => { st.enabled = !!(data && data.enabled); st.status = 'ok'; st.error = ''; })
    .catch(err => { st.status = 'error'; st.error = (err && err.message) || String(err); })
    .then(() => {
      if (typeof _automataTmplSubtab === 'undefined' || _automataTmplSubtab === 'packs') packsSubtabRender();
    });
}

// _packsSubtabGoto opens Settings on the given tab and scrolls to a section.
function _packsSubtabGoto(tab, sectionId) {
  if (typeof navigate === 'function') navigate('settings');
  setTimeout(() => {
    if (typeof switchSettingsTab === 'function') switchSettingsTab(tab);
    const el = document.getElementById(sectionId);
    if (el && el.scrollIntoView) el.scrollIntoView({ behavior: 'smooth' });
  }, 200);
}

function packsSubtabOpenSetting(ev) {
  if (ev && ev.preventDefault) ev.preventDefault();
  _packsSubtabGoto('general', 'settings-sec-gc_packs');
}

function packsSubtabOpenRegistry(ev) {
  if (ev && ev.preventDefault) ev.preventDefault();
  _packsSubtabGoto('automata', 'settings-sec-automata_skills');
}

window.packsSubtabHTML = packsSubtabHTML;
window.packsSubtabRender = packsSubtabRender;
window.packsSubtabLoad = packsSubtabLoad;
window.packsSubtabOpenSetting = packsSubtabOpenSetting;
window.packsSubtabOpenRegistry = packsSubtabOpenRegistry;
