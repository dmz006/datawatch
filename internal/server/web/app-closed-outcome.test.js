// Closed work outcome — PWA side: the two Settings rows, the outcome and deploy
// chips of the Automaton detail, and the Closed work check card on the
// Housekeeping sub-tab. Every string goes through t() and exists in all five
// locales.
const test = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const src = fs.readFileSync(__dirname + '/app.js', 'utf8');
const locales = ['en', 'de', 'es', 'fr', 'ja'].map(l => [l, JSON.parse(fs.readFileSync(`${__dirname}/locales/${l}.json`, 'utf8'))]);

function esc(s) { return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;'); }
// t() echoes its key and arguments so the tests can read what was asked for.
function tt(k, v) { return v ? `${k}(${v.join('|')})` : k; }

function extract(startMarker, endMarker) {
  const a = src.indexOf(startMarker);
  const b = src.indexOf(endMarker, a);
  assert.ok(a >= 0 && b > a, startMarker);
  return src.slice(a, b);
}

test('Settings carry the two mode selects with on, shadow and off and a how-to link', () => {
  for (const [key, def] of [['autonomous.closed_outcome_archive', 'on'], ['autonomous.closed_outcome_check', 'shadow']]) {
    const m = src.match(new RegExp("\\{ key: '" + key.replace('.', '\\.') + "'[^\\n]*\\}"));
    assert.ok(m, key + ' row missing');
    assert.match(m[0], /type: 'select'/);
    assert.match(m[0], /options: \['on', 'shadow', 'off'\]/);
    assert.match(m[0], new RegExp("default: '" + def + "'"));
    assert.match(m[0], /helpKey: /);
    assert.match(m[0], /helpDocs: 'howto\/closed-work-outcome\.md'/);
  }
});

test('the outcome and deploy chips show the class, the twin, the version and the health', () => {
  const code = extract('const CLOSED_OUTCOMES', 'window.closedOutcomeBadgesHtml');
  const fn = new Function('t', 'escHtml', code + '; return closedOutcomeBadgesHtml;')(tt, esc);
  assert.strictEqual(fn(null), '');
  assert.strictEqual(fn({}), '');
  const html = fn({ closed_outcome: 'delivered', delivered_via: 'ab12cd34', closed_outcome_note: 'twin shipped it',
    deploy: { version: 'v9.1.0', host: 'h1', health: 'passed', at: '2026-11-24T10:00:00Z' } });
  assert.match(html, /data-outcome="delivered"/);
  assert.match(html, /closed_outcome_via\(ab12cd34\)/);
  assert.match(html, /twin shipped it/);
  assert.match(html, /data-health="passed"/);
  assert.match(html, /release_deploy_chip\(v9\.1\.0\|h1\)/);
  assert.strictEqual(fn({ closed_outcome: 'bogus' }), '');
  assert.match(fn({ deploy: { version: 'v1', health: 'weird' } }), /data-health="unchecked"/);
});

test('the check card groups findings by kind, shows the proof counts and a Run now button', () => {
  const code = extract('const OUTCOME_CHECK_KINDS', 'window.runOutcomeCheckNow = ');
  const fn = new Function('t', 'escHtml', '_housekeepingHelpLink', 'window', code + '; return window.outcomeCheckCardHtml;')(tt, esc, () => '', {});
  const html = fn({ mode: 'on', scanned: 7, counts: { closed_no_outcome: 1 }, proof: { checked: 3, proven: 1, partial: 1, unproven: 1 },
    findings: [{ automaton: 'ab12cd34', kind: 'closed_no_outcome', detail: 'archived with no outcome' }, { automaton: '', kind: 'daemon_older_than_shipped', detail: 'old' }] });
  assert.match(html, /data-mode="on"/);
  assert.match(html, /data-kind="closed_no_outcome"/);
  assert.match(html, /data-kind="daemon_older_than_shipped"/);
  assert.match(html, /outcome_check_proof\(3\|1\|1\|1\)/);
  assert.match(html, /runOutcomeCheckNow/);
  assert.match(html, /outcome_check_finding_line\(outcome_check_daemon\|old\)/);
  assert.match(fn({}), /outcome_check_none/);
  assert.doesNotMatch(fn({ mode: 'shadow', findings: [] }), /data-kind=/);
});

test('the card is wired to the Housekeeping sub-tab and polls the read route', () => {
  for (const s of ["registerAutomationCardSlot('housekeeping'", "id: 'automationOutcomeCheckCard'", "'/api/autonomous/outcome-check'",
    "key: 'automation_outcome_check'", '${closedOutcomeBadgesHtml(prd)}']) assert.ok(src.includes(s), s);
});

test('every key the feature uses exists in every locale and carries no tracker id', () => {
  const keys = new Set();
  for (const m of src.matchAll(/'((?:settings_closed_outcome_|closed_outcome_|release_deploy_chip|release_deploy_health_|outcome_check_)[a-z_]*)'/g)) { if (!m[1].endsWith('_')) keys.add(m[1]); }
  for (const o of ['delivered', 'superseded', 'dropped', 'held']) keys.add('closed_outcome_' + o);
  for (const h of ['passed', 'failed', 'unchecked']) keys.add('release_deploy_health_' + h);
  for (const m of ['off', 'shadow', 'on']) keys.add('outcome_check_mode_' + m);
  for (const k of ['closed_no_outcome', 'completed_no_shipped', 'shipped_no_deploy', 'deployed_older_than_shipped', 'daemon_older_than_shipped', 'intent_unproven']) keys.add('outcome_check_kind_' + k);
  assert.ok(keys.size >= 30, 'found ' + keys.size);
  for (const [l, j] of locales) for (const k of keys) {
    assert.ok(j[k], `${l}: ${k}`);
    assert.ok(!/\b(BL|GH|F)\d+\b/.test(j[k]), `${l}: ${k} has a tracker id`);
  }
});

test('non-English locales are translated, not copies of the English text', () => {
  const en = locales[0][1];
  for (const k of ['closed_outcome_delivered', 'outcome_check_card_title', 'outcome_check_run_now', 'settings_closed_outcome_archive_help']) {
    for (const [l, j] of locales.slice(1)) assert.notStrictEqual(j[k], en[k], `${l}: ${k}`);
  }
});
