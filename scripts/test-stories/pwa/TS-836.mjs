// TS-836 — Automation detail page: pending-insert rows and the waiting count
//
// The CLI prints each held insert's member id, PRD id, reason and approve
// command, and the daemon's loop status counts held inserts in
// pending_members. The PWA must agree: the live status row words
// "waiting: N" from that same count, and each pending insert row on the
// detail page shows the member id and the PRD id as visible (escaped) text.
//
// This story drives the real PWA code in a real browser against the sandbox
// daemon: window._automationPendingInsertsHTML renders the pending-insert
// section and window.automationStatusLine words the loop-status row, so the
// assertions below cover the exact markup the operator sees, using the
// English bundle the sandbox ships by default (the same convention TS-826
// uses for rendered labels).
//
// Nothing is created or mutated on the daemon: only the app's pure render
// functions are exercised, so there is nothing to clean up.
import { runStory, connectToPWA, screenshot, saveLog } from './lib.mjs';

await runStory(async (page) => {
  await connectToPWA(page);

  const result = await page.evaluate(() => {
    const out = {};

    // The normal case: one held insert on an active Automation.
    const active = {
      id: 'ts836-automation', name: 'Release train', status: 'active', version: 7,
      pending_inserts: [{
        id: 'mem-77', prd_id: 'abcd1234abcd12', name: 'Hotfix',
        actor: 'agent:7', reason: 'tests found a gap', status: 'pending',
      }],
    };
    const host = document.createElement('div');
    document.body.appendChild(host);
    host.innerHTML = window._automationPendingInsertsHTML(active);

    out.sectionTitle = host.querySelector('h3') ? host.querySelector('h3').textContent : null;
    out.li = host.querySelector('li.automation-pending-insert');
    out.liAttrs = out.li ? { insertId: out.li.getAttribute('data-insert-id') } : null;
    out.idSpan = host.querySelector('span.automation-pending-insert-ids') ? host.querySelector('span.automation-pending-insert-ids').textContent : null;
    out.bySpan = host.querySelector('span.automation-pending-insert-by') ? host.querySelector('span.automation-pending-insert-by').textContent : null;
    out.reasonText = host.querySelector('span.automation-pending-insert-reason') ? host.querySelector('span.automation-pending-insert-reason').textContent : null;
    out.approve = host.querySelector('button.automation-approve-insert') || null;
    out.approveOnclick = out.approve ? out.approve.getAttribute('onclick') : null;

    // The hostile case: every value tries to inject markup.
    const evil = '<img src=x onerror=alert(1)>';
    const hostile = {
      id: 'ts836-hostile', name: '', status: 'active',
      pending_inserts: [{ id: evil, prd_id: evil, name: evil, actor: evil, reason: evil, status: 'pending' }],
    };
    host.innerHTML = window._automationPendingInsertsHTML(hostile);
    out.hostileRawImg = host.querySelectorAll('img').length;
    out.hostileText = host.textContent;
    out.hostileApproveOnclick = host.querySelector('button.automation-approve-insert')
      ? host.querySelector('button.automation-approve-insert').getAttribute('onclick') : null;
    out.hostileButtonCount = host.querySelectorAll('button').length;
    out.hostileId = evil;

    // A finished Automation: no Approve control, ids still visible.
    const done = {
      id: 'ts836-done', name: 'Done train', status: 'completed',
      pending_inserts: [{ id: 'mem-88', prd_id: 'ffff9999aaaa00', name: 'Fix', actor: 'agent:9', reason: '', status: 'pending' }],
    };
    host.innerHTML = window._automationPendingInsertsHTML(done);
    out.doneApprove = !!host.querySelector('button.automation-approve-insert');
    out.doneIdSpan = host.querySelector('span.automation-pending-insert-ids') ? host.querySelector('span.automation-pending-insert-ids').textContent : null;

    // No pending inserts: the section renders nothing at all.
    host.innerHTML = window._automationPendingInsertsHTML({ id: 'ts836-empty', status: 'active', pending_inserts: [] });
    out.emptyHtml = host.innerHTML;

    // The loop-status row words the daemon-side waiting count verbatim.
    out.statusLine = window.automationStatusLine({ current_prd_title: 'Ship it', pending_members: 3, idle_seconds: 600 });
    out.statusLineZero = window.automationStatusLine({ current_prd_title: 'Ship it', pending_members: 0, idle_seconds: 120 });

    host.remove();
    return out;
  });

  await screenshot(page, '01-pending-inserts');
  await saveLog('result', JSON.stringify(result, null, 2));

  const fail = (msg) => new Error(msg);

  // Normal case.
  if (result.sectionTitle !== 'Pending inserts') throw fail(`section title: expected "Pending inserts", got ${JSON.stringify(result.sectionTitle)}`);
  if (!result.liAttrs || result.liAttrs.insertId !== 'mem-77') throw fail(`row data-insert-id: got ${JSON.stringify(result.liAttrs)}`);
  if (result.idSpan !== 'Member mem-77 · Automaton abcd1234abcd12') {
    throw fail(`ids row: expected "Member mem-77 · Automaton abcd1234abcd12", got ${JSON.stringify(result.idSpan)}`);
  }
  if (result.bySpan !== 'Requested by agent:7') throw fail(`by row: expected "Requested by agent:7", got ${JSON.stringify(result.bySpan)}`);
  if (result.reasonText !== 'tests found a gap') throw fail(`reason row: got ${JSON.stringify(result.reasonText)}`);
  if (!result.approve) throw fail('no Approve control on an active Automation');
  if (result.approveOnclick !== "automationApproveInsert('mem-77')") {
    throw fail(`approve onclick: expected automationApproveInsert('mem-77'), got ${JSON.stringify(result.approveOnclick)}`);
  }

  // Hostile case: nothing parses as markup, still visible as text. The
  // approve handler must remain exactly one call whose argument, once
  // HTML-decoded, is the raw id itself — a breakout would leave extra
  // elements or an unbalanced call.
  if (result.hostileRawImg !== 0) throw fail('hostile insert value was parsed into an <img> element');
  if (result.hostileButtonCount !== 1) throw fail(`hostile row did not render exactly one control: ${result.hostileButtonCount}`);
  if (!result.hostileText.includes('<img src=x onerror=alert(1)>')) {
    throw fail(`hostile value not shown as text: ${JSON.stringify(result.hostileText)}`);
  }
  {
    const m = String(result.hostileApproveOnclick || '').match(/^automationApproveInsert\('(.*)'\)$/s);
    if (!m || m[1] !== result.hostileId) {
      throw fail(`approve onclick handler does not round-trip the raw id: ${JSON.stringify(result.hostileApproveOnclick)}`);
    }
  }

  // Finished Automation: ids stay visible, no editing controls.
  if (result.doneApprove) throw fail('a finished Automation still shows an Approve control');
  if (result.doneIdSpan !== 'Member mem-88 · Automaton ffff9999aaaa00') {
    throw fail(`finished Automation ids row: got ${JSON.stringify(result.doneIdSpan)}`);
  }

  // No pending inserts: nothing rendered.
  if (result.emptyHtml !== '') throw fail(`expected no section for zero pending inserts, got ${JSON.stringify(result.emptyHtml)}`);

  // Status row: the waiting count is the daemon-side pending_members figure.
  if (!/waiting: 3/.test(result.statusLine)) throw fail(`status line missing "waiting: 3": ${JSON.stringify(result.statusLine)}`);
  if (!/idle: 10m/.test(result.statusLine)) throw fail(`status line missing idle figure: ${JSON.stringify(result.statusLine)}`);
  if (/waiting: [1-9]/.test(result.statusLineZero)) throw fail(`zero pending_members must not word a pending count: ${JSON.stringify(result.statusLineZero)}`);
});
