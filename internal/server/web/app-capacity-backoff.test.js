'use strict';
// Real functional test for the capacity card's usage back-off line and its
// zone-preserving time formatter (added this release). The rules-review finding
// was that the new PWA rendering had no JS test; these tests execute the actual
// functions (not source strings) and pin down the behavior the reader relies on:
//   1. budgetTime shows the deadline in the zone it arrived in (the daemon's
//      zone), not the reader's local zone — the wall-clock and the offset travel
//      through unchanged; an invalid value falls back to the raw string.
//   2. capacityBackoffHtml draws the rung chip (localized label + rung value),
//      the reset time via budgetTime, and "reset time unknown" when no reset was
//      parsed; entries with no rung get no chip.
//
// Locale: `capacity_card_backoff_rung` (the "burn guard rung" label) is
// intentional and present in all five bundles (en/de/es/fr/ja). It renders via
// `t('capacity_card_backoff_rung')||'burn guard rung'`; the real-browser story
// scripts/test-stories/pwa/capacity-backoff-card.mjs asserts the chip too.
const test = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');

// Extract a top-level `function name(` definition up to its first column-0
// closing brace. The functions under test contain braces inside regex literals
// and template strings, so a brace-counter is unsafe; the codebase indents all
// body lines, so the first line that starts at column 0 with `}` is the close.
function extractFn(name) {
  const start = src.indexOf('function ' + name + '(');
  if (start < 0) throw new Error('function not found: ' + name);
  const lineStart = src.lastIndexOf('\n', start) + 1;
  let nl = src.indexOf('\n', start);
  while (nl !== -1) {
    if (src[nl + 1] === '}') break; // column-0 closing brace
    nl = src.indexOf('\n', nl + 1);
  }
  assert.ok(nl !== -1, 'no closing brace found for ' + name);
  return src.slice(lineStart, nl + 2); // nl+1 is the `}` char; include it
}

// Evaluate the extracted functions in a clean context, supplying just the app
// globals they depend on: t (locale) and escHtml.
function sandbox() {
  const locale = {
    capacity_card_backoff_resets: 'Resets',
    capacity_card_backoff_reset_unknown: 'reset time unknown',
    capacity_card_backoff_rung: 'burn guard rung',
    capacity_card_backoff_title: 'Usage back-off',
  };
  const t = (k) => (Object.prototype.hasOwnProperty.call(locale, k) ? locale[k] : k);
  const escHtml = (s) => String(s).replace(/[&<>"']/g, (c) => (
    { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]
  ));
  const code = [extractFn('budgetTime'), extractFn('capacityBackoffHtml')].join('\n\n');
  const ctx = vm.createContext({ t, escHtml });
  vm.runInContext(code + '\nthis.__budgetTime = budgetTime;\nthis.__capacityBackoffHtml = capacityBackoffHtml;', ctx);
  return { budgetTime: ctx.__budgetTime, capacityBackoffHtml: ctx.__capacityBackoffHtml };
}

test('budgetTime keeps the deadline in the zone it arrived in (daemon zone, not reader local)', () => {
  const { budgetTime } = sandbox();
  // UTC marker
  assert.strictEqual(budgetTime('2026-11-03T14:22:00Z'), '2026-11-03 14:22:00 UTC');
  // An explicit offset must be preserved verbatim, not shifted into the reader's local zone.
  assert.strictEqual(budgetTime('2026-11-03T14:22:00+05:30'), '2026-11-03 14:22:00 +05:30');
  assert.strictEqual(budgetTime('2026-11-03T14:22:00-08:00'), '2026-11-03 14:22:00 -08:00');
  // Wall-clock is never altered by formatting.
  const out = budgetTime('2026-11-03T09:05:07+09:45');
  assert.strictEqual(out, '2026-11-03 09:05:07 +09:45');
});

test('budgetTime falls back to the raw string for unparseable values', () => {
  const { budgetTime } = sandbox();
  assert.strictEqual(budgetTime(''), '');
  assert.strictEqual(budgetTime(undefined), '');
});

test('capacityBackoffHtml draws the rung chip with the localized label and the rung value', () => {
  const { capacityBackoffHtml } = sandbox();
  const html = capacityBackoffHtml([
    {
      backend: 'claude-code',
      until: '2026-11-03T14:22:00Z',
      reset_known: true,
      reason: 'hold_claude — weekly 128% vs 100%+25',
      rung: 'hold_claude',
    },
  ]);
  assert.ok(html.includes('claude-code'), 'renders the backend');
  assert.ok(html.includes('burn guard rung'), 'uses the localized rung label (capacity_card_backoff_rung)');
  assert.ok(html.includes('burn guard rung: hold_claude'), 'shows the localized label followed by the rung value');
  assert.ok(html.includes('2026-11-03 14:22:00 UTC'), 'renders the reset time via the zone-preserving budgetTime');
  assert.ok(html.includes('Resets'), 'uses the localized Resets label');
  assert.ok(html.includes('weekly 128% vs 100%+25'), 'shows the plain-language reason');
});

test('capacityBackoffHtml marks a no-reset entry as "reset time unknown" and omits the rung chip', () => {
  const { capacityBackoffHtml } = sandbox();
  const html = capacityBackoffHtml([
    { backend: 'pi', until: '2026-11-03T14:22:00Z', reset_known: false, reason: 'no reset time', rung: '' },
  ]);
  assert.ok(html.includes('reset time unknown'), 'shows the reset-unknown label');
  assert.ok(!html.includes('burn guard rung'), 'no rung chip for a blank rung');
});

test('capacityBackoffHtml returns empty string for no / non-array back-off', () => {
  const { capacityBackoffHtml } = sandbox();
  assert.strictEqual(capacityBackoffHtml(null), '');
  assert.strictEqual(capacityBackoffHtml('nope'), '');
});
