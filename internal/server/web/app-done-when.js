'use strict';
// "Done when" lists — PWA side.
//
// On an Automaton's overview this draws the request's "done when" table: each
// statement, how it is checked, where it must be true and how it reads (met,
// not met, cannot tell, unchecked), with the four actions view, edit, confirm
// and accept. "All lists" opens every list with open items grouped by reason.
// Nothing is drawn while acceptance.mode is off. The daemon decides how an
// item reads; this page only shows it and sends the request.
//
// Everything from the server is escaped, and buttons are wired with
// listeners, never with inline script.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-done-when.test.js

(function (root) {
  const SLOT_ID = 'prdDoneWhen';
  const MODAL_ID = 'doneWhenModal';

  function T(key, fallback) {
    if (typeof root.tOr === 'function') return root.tOr(key, fallback);
    const s = typeof root.t === 'function' ? root.t(key) : key;
    return s && s !== key ? s : fallback;
  }
  function E(v) {
    return String(v === undefined || v === null ? '' : v)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
  }

  const RESULTS = { unchecked: 'unchecked', met: 'met', not_met: 'not met', cannot_tell: 'cannot tell' };
  const STATUSES = {
    no_criteria: 'no criteria',
    draft: 'draft: not fixed yet',
    open: 'open',
    waiting: 'waiting',
    not_met: 'not met',
    ask_changed: 'the ask changed; the list has not been revised',
    failure_reported_still_broken: 'failure now reported, still broken',
    thin_list: 'the list is too thin for the ask',
    met_not_reviewed: 'met, but nobody has checked that the list covers the ask',
    met_on_changed_list: 'met on a changed list (not plain done)',
    answered_not_checked: 'answered, but the current versions have not been checked since (not done)',
    done_accepted: 'done, accepted without proof',
    done: 'done',
    delivered: 'delivered (not the same as done)',
    filed: 'filed (not the same as done)',
  };

  // An unknown value is shown as it is, never as met or done.
  function resultText(v) { return RESULTS[v] ? T('done_when_result_' + v, RESULTS[v]) : String(v || RESULTS.unchecked); }
  function statusText(v) { return STATUSES[v] ? T('done_when_status_' + v, STATUSES[v]) : String(v || ''); }

  // rows: one plain object per item, in list order.
  function rows(view) {
    const list = (view && view.list) || {};
    const read = {};
    ((view && view.rollup && view.rollup.items) || []).forEach(r => { read[r.id] = r; });
    return (list.items || []).map(it => {
      const r = read[it.id] || { result: {} };
      const result = r.result || {};
      let shown = resultText(result.value);
      if (r.accepted_without_proof) shown = T('done_when_accepted', 'accepted without proof');
      else if (result.waiting) shown = T('done_when_waiting', 'waiting');
      return {
        id: it.id, statement: it.statement, method: it.method, effect: it.effect || '', source: it.source || '',
        value: r.accepted_without_proof ? 'accepted' : (result.value || 'unchecked'),
        shown, reason: result.reason || '', claim: r.claim || '', changed: r.changed || '',
        alreadyTrue: !!result.already_true,
        canConfirm: it.method === 'confirm' && !!list.frozen_at && !/^0001-/.test(list.frozen_at),
      };
    });
  }

  function isFrozen(list) { return !!(list && list.frozen_at && !/^0001-/.test(list.frozen_at)); }

  function button(action, item, label) {
    return `<button type="button" class="btn-secondary" style="font-size:11px;padding:2px 8px;" data-dw-action="${E(action)}" data-dw-item="${E(item)}">${E(label)}</button>`;
  }

  function tableHtml(view) {
    const list = (view && view.list) || {};
    const frozen = isFrozen(list);
    const flags = (view && view.flags) || [];
    const changes = (view && view.changes) || {};
    const body = rows(view).map(r => {
      const notes = [];
      if (r.reason) notes.push(E(r.reason));
      if (r.claim) notes.push(E(r.claim));
      if (r.alreadyTrue) notes.push(E(T('done_when_already_true', 'already true at the start')));
      if (r.changed) notes.push(E(r.changed === 'added' ? T('done_when_added', 'added after the list was fixed') : T('done_when_altered', 'changed after the list was fixed')));
      const actions = [];
      if (r.canConfirm) {
        actions.push(button('yes', r.id, T('done_when_confirm_yes', 'Confirm met')));
        actions.push(button('no', r.id, T('done_when_confirm_no', 'Not met')));
      }
      if (frozen && r.value !== 'met' && r.value !== 'accepted') actions.push(button('accept', r.id, T('done_when_accept', 'Accept without proof')));
      return `<tr data-dw-row="${E(r.id)}"><td>${E(r.statement)}</td><td>${E(r.method)}</td><td>${E(r.effect)}</td>` +
        `<td data-dw-value="${E(r.value)}"><strong>${E(r.shown)}</strong><div style="color:var(--text2);font-size:11px;">${notes.join('<br>')}</div></td>` +
        `<td>${actions.join(' ')}</td></tr>`;
    }).join('');
    const removed = (changes.removed || []).map(it => `<li><s>${E(it.statement)}</s> (${E(T('done_when_removed', 'removed after the list was fixed'))})</li>`).join('');
    const flagRows = flags.map(f => `<li>${E(f.text)}${f.item ? ' <code>' + E(f.item) + '</code>' : ''}</li>`).join('');
    const top = [];
    if (!frozen) top.push(button('freeze', '', T('done_when_freeze', 'Fix this list')));
    if (frozen) top.push(button('cover', 'list', T('done_when_cover', 'Confirm the list covers the ask')));
    top.push(button('edit', '', T('done_when_edit', 'Edit')));
    top.push(button('all', '', T('done_when_all', 'All lists')));
    return `<div class="done-when">
      <h3 style="margin:0 0 4px;">${E(T('done_when_title', 'Done when'))}</h3>
      <div data-dw-status="${E((view.rollup || {}).status)}"><strong>${E(statusText((view.rollup || {}).status))}</strong></div>
      <div style="margin:6px 0;">${top.join(' ')}</div>
      ${flagRows ? `<ul style="color:var(--warning);font-size:12px;">${flagRows}</ul>` : ''}
      <div style="overflow-x:auto;"><table style="width:100%;font-size:12px;">
        <thead><tr><th>${E(T('done_when_col_statement', 'Statement'))}</th><th>${E(T('done_when_col_how', 'How'))}</th><th>${E(T('done_when_col_where', 'Where'))}</th><th>${E(T('done_when_col_result', 'Result'))}</th><th></th></tr></thead>
        <tbody>${body}</tbody></table></div>
      ${removed ? `<ul style="font-size:12px;">${removed}</ul>` : ''}
      <div data-dw-form></div>
      <div data-dw-message style="color:var(--error);font-size:12px;"></div>
    </div>`;
  }

  function allHtml(data) {
    const ov = (data && data.overview) || {};
    const lists = (ov.lists || []).map(l => `<tr><td><code>${E(l.request_id)}</code></td><td>${E(statusText(l.status))}</td><td>${E(l.open_items)}</td>` +
      `<td>${E(l.due === 'overdue' ? T('done_when_overdue', 'overdue') : l.due === 'due_soon' ? T('done_when_due_soon', 'due soon') : '')}</td><td>${E(l.yours || '')}</td></tr>`).join('');
    const groups = (ov.groups || []).map(g => `<li>${E(g.count)} · ${E(g.source)} · ${E(g.reason)}</li>`).join('');
    if (!lists) return `<p>${E(T('done_when_none', 'No "done when" lists yet.'))}</p>`;
    return `<table style="width:100%;font-size:12px;"><thead><tr><th>${E(T('done_when_col_request', 'Request'))}</th><th>${E(T('done_when_col_result', 'Result'))}</th><th>${E(T('done_when_col_open', 'Open'))}</th><th></th><th></th></tr></thead><tbody>${lists}</tbody></table>` +
      (groups ? `<h4>${E(T('done_when_groups', 'Open items by reason'))}</h4><ul style="font-size:12px;">${groups}</ul>` : '');
  }

  // The forms ask for the words that were said; the daemon keeps them.
  function formFields(action) {
    if (action === 'accept') return [['reason', T('done_when_reason', 'Why accept it without proof?')], ['words', T('done_when_words', 'Words that were said')]];
    if (action === 'edit') return [['changes', T('done_when_changes', 'Changes as JSON: items, remove, add_ask')], ['words', T('done_when_words', 'Words that were said')]];
    return [['words', T('done_when_words', 'Words that were said')]];
  }

  // request builds the write for one action; it returns { path, body } or
  // { error }. The caller name sent is only a label.
  function request(action, id, item, values, hash) {
    const body = { id, actor: 'web' };
    const words = (values.words || '').trim();
    if (words) body.words = words;
    if (action === 'freeze') return { path: '/api/acceptance/edit', body: Object.assign(body, { freeze: true, hash }) };
    if (action === 'yes' || action === 'no' || action === 'cover') {
      if (!words) return { error: T('done_when_need_words', 'The words that were said are required.') };
      return { path: '/api/acceptance/confirm', body: Object.assign(body, { item, answer: action === 'no' ? 'no' : 'yes', hash }) };
    }
    if (action === 'accept') {
      const reason = (values.reason || '').trim();
      if (!reason || !words) return { error: T('done_when_need_words', 'The words that were said are required.') };
      return { path: '/api/acceptance/accept', body: Object.assign(body, { item, reason, hash }) };
    }
    let changes;
    try { changes = JSON.parse(values.changes || ''); } catch (e) { return { error: String(e.message || e) }; }
    if (!changes || typeof changes !== 'object' || Array.isArray(changes)) return { error: T('done_when_bad_json', 'The changes must be a JSON object.') };
    const allowed = {};
    ['items', 'remove', 'add_ask', 'needed_by'].forEach(k => { if (changes[k] !== undefined) allowed[k] = changes[k]; });
    return { path: '/api/acceptance/edit', body: Object.assign(body, allowed, { hash }) };
  }

  function send(path, body) {
    return root.apiFetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
  }

  function showAll(doc) {
    const old = doc.getElementById(MODAL_ID);
    if (old) old.remove();
    const modal = doc.createElement('div');
    modal.id = MODAL_ID;
    modal.className = 'modal-overlay';
    modal.style.cssText = 'position:fixed;inset:0;background:rgba(0,0,0,.5);z-index:1000;overflow:auto;padding:24px;';
    modal.innerHTML = `<div style="background:var(--bg2);padding:16px;border-radius:8px;max-width:860px;margin:auto;"><button type="button" class="btn-secondary" data-dw-close>${E(T('done_when_close', 'Close'))}</button><div data-dw-all></div></div>`;
    modal.querySelector('[data-dw-close]').addEventListener('click', () => modal.remove());
    doc.body.appendChild(modal);
    root.apiFetch('/api/acceptance')
      .then(data => { modal.querySelector('[data-dw-all]').innerHTML = allHtml(data); })
      .catch(err => { modal.querySelector('[data-dw-all]').textContent = String((err && err.message) || err); });
  }

  function wire(slot, id, view) {
    const message = text => { const m = slot.querySelector('[data-dw-message]'); if (m) m.textContent = text; };
    const run = req => {
      if (req.error) { message(req.error); return; }
      send(req.path, req.body)
        .then(() => load(slot, id))
        .catch(err => message(String((err && err.message) || err)));
    };
    slot.querySelectorAll('[data-dw-action]').forEach(btn => btn.addEventListener('click', () => {
      const action = btn.getAttribute('data-dw-action');
      const item = btn.getAttribute('data-dw-item');
      if (action === 'all') { showAll(slot.ownerDocument); return; }
      if (action === 'freeze') { run(request(action, id, item, {}, view.hash)); return; }
      const holder = slot.querySelector('[data-dw-form]');
      holder.innerHTML = formFields(action).map(f => `<label style="display:block;font-size:12px;">${E(f[1])}<textarea data-dw-field="${E(f[0])}" rows="${f[0] === 'changes' ? 6 : 2}" style="width:100%;"></textarea></label>`).join('') +
        `<button type="button" class="btn-primary" data-dw-send>${E(T('done_when_send', 'Send'))}</button>`;
      holder.querySelector('[data-dw-send]').addEventListener('click', () => {
        const values = {};
        holder.querySelectorAll('[data-dw-field]').forEach(el => { values[el.getAttribute('data-dw-field')] = el.value; });
        run(request(action, id, item, values, view.hash));
      });
    }));
  }

  // load draws the slot. Off, not wired or no list: the slot stays hidden,
  // except that "no criteria" is said when the setting is on.
  function load(slot, id) {
    return root.apiFetch('/api/acceptance?id=' + encodeURIComponent(id))
      .then(view => {
        if (!view || view.mode !== 'report' || !view.list) { slot.innerHTML = ''; slot.style.display = 'none'; return; }
        slot.style.display = '';
        slot.innerHTML = tableHtml(view);
        wire(slot, id, view);
      })
      .catch(err => {
        const text = String((err && err.message) || err);
        if (/no done-when list/.test(text)) {
          slot.style.display = '';
          slot.innerHTML = `<div class="done-when"><strong>${E(T('done_when_title', 'Done when'))}</strong>: ${E(statusText('no_criteria'))}</div>`;
          return;
        }
        slot.style.display = 'none';
      });
  }

  // mount adds the slot to an Automaton's overview.
  function mount(body, id) {
    if (!body || !id || typeof root.apiFetch !== 'function') return null;
    const doc = body.ownerDocument;
    const slot = doc.createElement('div');
    slot.id = SLOT_ID;
    slot.className = 'prd-capacity-slot done-when-slot';
    slot.style.cssText = 'margin-top:10px;display:none;';
    (body.querySelector('.prd-detail-body') || body).appendChild(slot);
    load(slot, id);
    return slot;
  }

  const api = { rows, tableHtml, allHtml, request, resultText, statusText, mount, load, RESULTS, STATUSES };
  root.doneWhen = api;
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
})(typeof window !== 'undefined' ? window : globalThis);
