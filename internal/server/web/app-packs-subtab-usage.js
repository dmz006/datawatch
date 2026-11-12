// app-packs-subtab-usage.js - per-source auto-accept switch and per-pack usage
// table for the Packs sub-tab. Loaded after app-packs-subtab.js. It renders
// HTML strings and calls REST routes; it keeps no list of its own.
//
//   auto-accept switch -> PUT /api/skills/registries/{name} {auto_accept_packs}
//   usage table        -> GET /api/packs/usage
//
// A source is a skills registry entry {name, auto_accept_packs?}. A usage row
// is {pack_name, pack_version, runs, cost, last_used}; the endpoint may return
// the rows as an array or as {usage: [...]}.

function _packsUseT(key, fallback) {
  try {
    const v = (typeof t === 'function') ? t(key) : key;
    return (v && v !== key) ? v : fallback;
  } catch (_) { return fallback; }
}

function _packsUseEsc(s) {
  return (typeof escHtml === 'function') ? escHtml(s) : String(s == null ? '' : s)
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

// packsAutoAcceptPayload builds the update body for a source switch.
function packsAutoAcceptPayload(on) { return { auto_accept_packs: !!on }; }

// packsAutoAcceptHTML renders the switch for one source. The source is found
// again by index so no name is placed in an inline handler. Off by default.
function packsAutoAcceptHTML(source, idx) {
  const e = _packsUseEsc;
  if (!source || !source.name) return '';
  return '<label class="packs-autoaccept" style="font-size:12px;display:inline-flex;gap:6px;align-items:center;">'
    + '<input type="checkbox" class="packs-autoaccept-switch"' + (source.auto_accept_packs ? ' checked' : '')
    + ' onchange="packsSubtabSetAutoAccept(' + Number(idx) + ',this.checked)">'
    + '<span>' + e(_packsUseT('packs_auto_accept', 'Auto-accept updates from')) + ' ' + e(source.name) + '</span></label>'
    + '<div class="packs-autoaccept-note" style="font-size:11px;opacity:.7;">'
    + e(_packsUseT('packs_auto_accept_note', 'A version that adds tools, scripts or file scopes still waits for you.'))
    + '</div>';
}

// packsSetAutoAccept saves the switch and returns the fetch promise.
function packsSetAutoAccept(source, on) {
  if (!source || !source.name) return Promise.resolve(null);
  return apiFetch('/api/skills/registries/' + encodeURIComponent(source.name), {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(packsAutoAcceptPayload(on)),
  });
}

// packsUsageRows normalises the usage response to a sorted array of rows.
function packsUsageRows(data) {
  const raw = Array.isArray(data) ? data : (data && Array.isArray(data.usage) ? data.usage : []);
  return raw.filter(r => r && r.pack_name).slice().sort((a, b) =>
    String(a.pack_name).localeCompare(String(b.pack_name))
    || String(a.pack_version || '').localeCompare(String(b.pack_version || '')));
}

function _packsUseCost(c) { return '$' + (Number(c) || 0).toFixed(2); }

function _packsUseWhen(ts) {
  if (!ts || String(ts).indexOf('0001-') === 0) return '';
  const d = new Date(ts);
  return isNaN(d.getTime()) ? '' : d.toISOString().replace('T', ' ').slice(0, 16);
}

// packsUsageTableHTML renders the per-pack usage table, or an empty-state line.
function packsUsageTableHTML(data) {
  const e = _packsUseEsc;
  const rows = packsUsageRows(data);
  const head = '<div class="packs-usage-head" style="font-weight:600;">'
    + e(_packsUseT('packs_usage_head', 'Pack usage')) + '</div>';
  if (!rows.length) {
    return '<div class="packs-usage">' + head + '<p style="font-size:12px;opacity:.7;">'
      + e(_packsUseT('packs_usage_empty', 'No pack has been used yet.')) + '</p></div>';
  }
  let out = '<div class="packs-usage">' + head + '<table class="packs-usage-table" style="font-size:12px;">'
    + '<thead><tr><th>' + e(_packsUseT('packs_usage_pack', 'Pack')) + '</th><th>'
    + e(_packsUseT('packs_usage_version', 'Version')) + '</th><th>'
    + e(_packsUseT('packs_usage_runs', 'Runs')) + '</th><th>'
    + e(_packsUseT('packs_usage_cost', 'Cost')) + '</th><th>'
    + e(_packsUseT('packs_usage_last', 'Last used')) + '</th></tr></thead><tbody>';
  rows.forEach(r => {
    out += '<tr><td>' + e(r.pack_name) + '</td><td>' + e(r.pack_version || '') + '</td><td>'
      + e(Number(r.runs) || 0) + '</td><td>' + e(_packsUseCost(r.cost)) + '</td><td>'
      + e(_packsUseWhen(r.last_used)) + '</td></tr>';
  });
  return out + '</tbody></table></div>';
}

// packsUsageLoad reads the usage endpoint and resolves to the rendered table.
// A failed read resolves to an error line rather than throwing.
function packsUsageLoad() {
  return apiFetch('/api/packs/usage')
    .then(packsUsageTableHTML)
    .catch(err => '<div class="packs-usage-error" style="font-size:12px;color:var(--error,#ef4444);">'
      + _packsUseEsc((err && err.message) || String(err)) + '</div>');
}
