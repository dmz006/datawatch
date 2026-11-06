// Capacity card — usage "resets at" line (zone-preserving) + burn-guard rung chip.
//
// Real-browser confirmation of the contract the daemon relies on: the "resets
// at" wall-clock is shown in the zone the deadline arrived in (the daemon's zone,
// which travels with the JSON string — UTC 'Z' or an explicit +HH:MM offset), NOT
// re-based to the phone's local zone. The deterministic unit proof lives in
// internal/server/web/app-capacity-backoff.test.js; this story drives the
// production render path (window.capacityCardHtml -> capacityBackoffHtml ->
// budgetTime) to prove the PWA renders it the same way on a phone viewport, and
// that the burn-guard rung chip + "reset time unknown" render too.
//
// It also renders the LIVE /api/capacity card (integration; non-fatal if nothing
// is in back-off) and checks for horizontal overflow at 390px.
//
// Exit 0 pass / 1 fail / 2 skip (runPerfStory + skipStory rules).
//   bash scripts/e2e-pwa.sh capacity-backoff-card
//
// Locale: `capacity_card_backoff_rung` (the burn-guard rung label) and
// `capacity_card_backoff_reset_unknown` (the no-reset line) are intentional and
// exist in all five bundles (en/de/es/fr/ja). This story asserts the rung chip
// (`cap-backoff-rung` element with the rung value) and the "reset time unknown"
// line render through `window.capacityCardHtml`.

import { runPerfStory, openPWA, check, saveJson } from './perf-lib.mjs';
import { storyEvidenceDir, screenshot } from './lib.mjs';
import path from 'node:path';

const PROBE = [
  { backend: 'claude-code', until: '2026-11-03T14:22:00Z',      reset_known: true,  rung: 'hold_claude', reason: 'hold_claude - weekly 128% vs 100%+25' },
  { backend: 'pi',          until: '2026-11-03T14:22:00+05:30', reset_known: true,  rung: '',            reason: 'rate-limit reset 14:22' },
  { backend: 'pi',          until: '',                            reset_known: false, rung: '',           reason: 'no reset time' },
];

runPerfStory(async (page) => {
  await openPWA(page);
  await page.setViewportSize({ width: 390, height: 844 });

  check(await page.evaluate(() => typeof window.capacityCardHtml === 'function'),
    'window.capacityCardHtml must exist in the PWA (feature present)');

  // Drive the REAL production renderer with each synthetic back-off entry and
  // report what it produced, measured inside the page (phone width, 360px cell).
  const reports = await page.evaluate((entries) => {
    const rows = [];
    for (const e of entries) {
      const html = window.capacityCardHtml({ backoff: [e] }, { showEmpty: true });
      const el = document.createElement('div');
      el.style.width = '360px';
      el.style.overflow = 'hidden';
      el.innerHTML = html;
      document.body.appendChild(el);
      const text = el.textContent || '';
      const overflow = el.scrollWidth - el.clientWidth;
      el.remove();

      const row = {
        backend: e.backend,
        hasBackend: text.includes(e.backend),
        reason: text.includes(e.reason),
        overflow,
      };
      if (e.reset_known && e.until) {
        // The zone of the input travels into the text: 'Z' -> UTC; an explicit
        // +HH:MM offset is preserved. The wall-clock stays what the daemon sent.
        const zone = /Z$/.test(e.until)
          ? 'UTC'
          : ((e.until.match(/([+-]\d{2}:\d{2})$/) || [])[1]);
        row.zone = zone;
        row.zonePreserved = zone ? text.includes(`14:22:00 ${zone}`) : false;
      }
      if (!e.reset_known) {
        const exp = (typeof t === 'function' ? t('capacity_card_backoff_reset_unknown') : '') || 'reset time unknown';
        row.resetUnknown = text.includes(exp);
      }
      if (e.rung) {
        row.rungChip = /cap-backoff-rung/.test(html) && text.includes(e.rung);
      }
      rows.push(row);
    }
    return rows;
  }, PROBE);

  for (const r of reports) {
    check(r.hasBackend === true, `${r.backend}: renders the backend`);
    check(r.reason === true, `${r.backend}: shows the reason`);
    check((r.overflow || 0) <= 2, `${r.backend}: no horizontal overflow at 360px (overflow=${r.overflow})`);
    if (r.zone !== undefined) check(r.zonePreserved === true,
      `${r.backend}: "resets at" is in the deadline's own zone (${r.zone}), not the phone's local zone`);
    if (r.resetUnknown !== undefined) check(r.resetUnknown === true, `${r.backend}: "reset time unknown" shown`);
    if (r.rungChip !== undefined) check(r.rungChip === true, `${r.backend}: burn-guard rung chip shows the rung`);
  }

  // Integration (non-fatal if nothing is in back-off): the LIVE endpoint must
  // render through the same path without overflow.
  const live = await page.evaluate(async () => {
    try {
      const r = await fetch('/api/capacity');
      if (!r.ok) return { active: 0, overflow: 0, http: r.status };
      const cap = await r.json();
      const active = Array.isArray(cap && cap.backoff) ? cap.backoff.length : 0;
      const html = window.capacityCardHtml(cap, { showEmpty: true });
      const el = document.createElement('div');
      el.style.width = '390px'; el.style.overflow = 'hidden';
      el.innerHTML = html;
      document.body.appendChild(el);
      const overflow = el.scrollWidth - el.clientWidth;
      el.remove();
      return { active, overflow, hasRung: /cap-backoff-rung/.test(html) };
    } catch (e) { return { active: 0, overflow: 0, error: String(e) }; }
  });
  check((live.overflow || 0) <= 2, `live /api/capacity card: no horizontal overflow at 390px (overflow=${live.overflow})`);

  // Evidence
  const dir = storyEvidenceDir();
  await page.screenshot({ path: path.join(dir, 'capacity-backoff-card.png'), fullPage: false });
  await saveJson('state.json', { probes: reports, live });

  console.log(`capacity-backoff-card: ${reports.length} probes OK, live_active=${live.active} → PASS`);
});
