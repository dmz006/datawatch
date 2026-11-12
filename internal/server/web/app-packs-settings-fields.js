// app-packs-settings-fields.js - Settings > General > Standard task packs.
// Loaded before app.js. It holds the packs enabled switch, the active pack
// order list and the pack input fields, in the same field shape as the other
// GENERAL_CONFIG_FIELDS entries, plus the handlers those fields save through.
// Every handler resolves the page helpers (t, _apiPath, tokenHeader,
// showToast) when it runs, so this file has no load-order dependency on them.

const PACKS_HELP_DOCS = 'howto/automata-standard-tasks.md';

// The packs.* fields. The enabled switch needs at least one connected skills
// registry source; the daemon refuses to enable packs otherwise, and the
// refusal is shown inline next to the field.
const PACKS_SETTINGS_FIELDS = [
  { key: 'packs.enabled', label: 'Enable standard task packs (needs a connected skills registry)', labelKey: 'settings_packs_enabled', helpKey: 'settings_packs_enabled_help', helpDocs: PACKS_HELP_DOCS, type: 'toggle', onchange: "savePacksEnabled(this)" },
  { key: 'packs.active', label: 'Active packs, in order (comma separated)', labelKey: 'settings_packs_active', helpKey: 'settings_packs_active_help', helpDocs: PACKS_HELP_DOCS, type: 'text', placeholder: 'builtin', onchange: "savePacksActive(this)" },
  { key: 'packs.inputs.level', label: 'Pack inputs: level (global or project)', labelKey: 'settings_packs_inputs_level', helpKey: 'settings_packs_inputs_help', helpDocs: PACKS_HELP_DOCS, type: 'text', placeholder: 'global', onchange: "_packsInputChanged(this)" },
  { key: 'packs.inputs.pack', label: 'Pack inputs: pack name', labelKey: 'settings_packs_inputs_pack', helpKey: 'settings_packs_inputs_help', helpDocs: PACKS_HELP_DOCS, type: 'text', placeholder: 'builtin', onchange: "_packsInputChanged(this)" },
  { key: 'packs.inputs.key', label: 'Pack inputs: project key (project level only)', labelKey: 'settings_packs_inputs_key', helpKey: 'settings_packs_inputs_help', helpDocs: PACKS_HELP_DOCS, type: 'text', placeholder: '', onchange: "_packsInputChanged(this)" },
  { key: 'packs.inputs.values', label: 'Pack inputs: values (name=value, separated by semicolons)', labelKey: 'settings_packs_inputs_values', helpKey: 'settings_packs_inputs_help', helpDocs: PACKS_HELP_DOCS, type: 'text', placeholder: 'name=value; other=value', onchange: "savePacksInputs(this)" },
];

// parsePacksActive turns "builtin, my-pack" into ["builtin","my-pack"]:
// trimmed, empty entries dropped, order kept.
function parsePacksActive(raw) {
  return String(raw == null ? '' : raw).split(',').map(s => s.trim()).filter(s => s !== '');
}

// parsePacksInputs turns "a=1; b=two=2" into {a:"1", b:"two=2"}. It returns
// null when an entry has no name or no "=".
function parsePacksInputs(raw) {
  const out = {};
  const text = String(raw == null ? '' : raw).trim();
  if (text === '') return out;
  for (const part of text.split(';')) {
    const p = part.trim();
    if (p === '') continue;
    const i = p.indexOf('=');
    if (i <= 0) return null;
    out[p.slice(0, i).trim()] = p.slice(i + 1).trim();
  }
  return out;
}

// _packsError shows msg in a small alert under the field (or clears it).
function _packsError(el, msg) {
  const row = el && el.parentElement;
  if (!row || typeof row.querySelector !== 'function') return;
  let box = row.querySelector('.packs-field-error');
  if (!box) {
    if (!msg || typeof document === 'undefined' || !document.createElement) return;
    box = document.createElement('div');
    box.className = 'packs-field-error';
    box.setAttribute('role', 'alert');
    box.style.cssText = 'flex-basis:100%;font-size:11px;color:var(--error,#ef4444);';
    row.appendChild(box);
  }
  box.textContent = msg || '';
  box.style.display = msg ? '' : 'none';
}

// _packsRefusalText reads the daemon's refusal: JSON error or message, any
// other body as sent, an empty body as the generic line.
function _packsRefusalText(text) {
  const raw = String(text || '').trim();
  if (!raw) return t('settings_packs_error_refused');
  try {
    const body = JSON.parse(raw);
    const msg = body && (body.error || body.message);
    if (msg) return String(msg);
  } catch (_) { /* not JSON: show the body as sent */ }
  return raw;
}

function _packsSend(path, body, el, onRefused) {
  return fetch(_apiPath(path), {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', ...tokenHeader() },
    body: JSON.stringify(body),
  })
    .then(r => {
      if (r.ok) {
        _packsError(el, '');
        showToast('Saved', 'success', 1500);
        return null;
      }
      return r.text().then(text => {
        if (onRefused) onRefused();
        _packsError(el, _packsRefusalText(text));
      });
    })
    .catch(() => _packsError(el, t('settings_packs_error_network')));
}

// savePacksEnabled saves the switch through /api/packs/config. The daemon
// refuses to enable packs with no connected registry source; the switch is
// put back and the refusal shown.
function savePacksEnabled(el) {
  const want = !!(el && el.checked);
  return _packsSend('/api/packs/config', { enabled: want }, el, () => { if (el) el.checked = !want; });
}

// savePacksActive saves the ordered active list through /api/packs/config.
function savePacksActive(el) {
  return _packsSend('/api/packs/config', { active: parsePacksActive(el && el.value) }, el);
}

function _packsInputField(key) {
  const e = typeof document !== 'undefined' && document.querySelector
    ? document.querySelector('input[data-cfg-key="' + key + '"]') : null;
  return e ? String(e.value || '').trim() : '';
}

// _packsInputChanged: level, pack and key only choose the scope; the values
// box is what saves, so changing them sends nothing.
function _packsInputChanged(el) { _packsError(el, ''); }

// savePacksInputs saves the name=value list for the chosen level and pack.
// It replaces that scope, as the daemon does.
function savePacksInputs(el) {
  const level = _packsInputField('packs.inputs.level') || 'global';
  const pack = _packsInputField('packs.inputs.pack');
  const key = _packsInputField('packs.inputs.key');
  const inputs = parsePacksInputs(el && el.value);
  if (level !== 'global' && level !== 'project') { _packsError(el, t('settings_packs_error_level')); return null; }
  if (!pack) { _packsError(el, t('settings_packs_error_pack')); return null; }
  if (level === 'project' && !key) { _packsError(el, t('settings_packs_error_project_key')); return null; }
  if (inputs === null) { _packsError(el, t('settings_packs_error_pairs')); return null; }
  let q = '?level=' + encodeURIComponent(level) + '&pack=' + encodeURIComponent(pack);
  if (level === 'project') q += '&key=' + encodeURIComponent(key);
  return _packsSend('/api/packs/inputs' + q, { inputs }, el);
}
