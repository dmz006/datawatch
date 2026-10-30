// Cleanup status card and settings fields — PWA side.
//
// window.cleanupCardHtml(status) draws GET /api/autonomous/cleanup: both passes
// (the Automaton worktrees pass and the Automata temp file sweep) with last run,
// counts, bytes freed and last error, the disk use readings and the disk alert
// badge. The card is read-only. The four cleanup settings fields and every
// cleanup_* locale key are pinned here as well.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const HOSTILE = '<img src=x onerror=alert(1)>';
const ENTITY_IMG = '&lt;img src=x onerror=alert(1)&gt;';

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
}

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  return sandbox;
}

function card(status) {
  const sandbox = loadApp();
  sandbox._s = status;
  return vm.runInContext('window.cleanupCardHtml(_s)', sandbox);
}

function pass(over) {
  return {
    mode: 'report',
    last_run: '2026-11-01T08:30:00Z',
    removed: 3, would_remove: 1, skipped: 2, failed: 1,
    bytes_freed: 123456789,
    last_error: 'permission denied',
    owner_removed: 2, net_removed: 1,
    owner_bytes_freed: 100000000, net_bytes_freed: 23456789,
    ...over,
  };
}

function status(over) {
  return {
    worktrees: pass(),
    temp: pass({ mode: 'on', removed: 7, would_remove: 0, skipped: 0, failed: 0, bytes_freed: 2048, last_error: '', owner_removed: 7, net_removed: 0 }),
    disk_alert_percent: 85,
    worktree_disk: { path: '/data/worktrees', percent: 91, alert: true },
    temp_disk: { path: '/tmp', percent: 40, alert: false },
    disk_alert: true,
    last_disk_alert_at: '2026-11-01T10:30:00Z',
    ...over,
  };
}

// ── the card ─────────────────────────────────────────────────────────────────

test('the card shows both passes with mode, counts, bytes freed and last error', () => {
  const html = card(status());
  assert.ok(html.includes('Automata cleanup status'), 'card title missing');
  assert.ok(html.includes('Automaton worktrees pass'), 'worktrees pass heading missing');
  assert.ok(html.includes('Automata temp file sweep'), 'temp pass heading missing');
  assert.ok(html.includes('Automata cleanup mode: report'), 'worktrees mode missing: ' + html);
  assert.ok(html.includes('Automata cleanup mode: on'), 'temp mode missing');
  assert.ok(html.includes('Automata cleanup: removed 3, would remove 1, skipped 2, failed 1'), 'worktrees counts missing: ' + html);
  assert.ok(html.includes('Automata cleanup: removed 7, would remove 0, skipped 0, failed 0'), 'temp counts missing');
  assert.ok(html.includes('Automata cleanup bytes freed: 117.7 MB'), 'worktrees bytes freed missing: ' + html);
  assert.ok(html.includes('Automata cleanup bytes freed: 2.0 KB'), 'temp bytes freed missing');
  assert.ok(html.includes('Last Automata cleanup error: permission denied'), 'last error missing');
  assert.ok(html.includes('No Automata cleanup error.'), 'a pass with no error says so');
  assert.ok(html.includes('removed by the owner: 2, by the safety net: 1'), 'owner and net split missing');
  assert.ok(html.includes('Last Automata cleanup run:'), 'last run line missing');
});

test('a pass that never ran says so and shows no counts', () => {
  const html = card(status({ worktrees: { mode: 'off' }, temp: { mode: 'report', last_run: '0001-01-01T00:00:00Z' } }));
  assert.equal(html.split('The Automata cleanup has not run yet.').length - 1, 2, 'both passes should read as not yet run: ' + html);
  assert.ok(!html.includes('would remove'), 'no counts line before a first run');
  assert.ok(!html.includes('bytes freed'), 'no bytes line before a first run');
});

test('the card shows disk use, the threshold and the disk alert badge', () => {
  const html = card(status());
  assert.ok(html.includes('Automata disk use'), 'disk heading missing');
  assert.ok(html.includes('Automaton worktree disk /data/worktrees: 91% used'), 'worktree disk line missing: ' + html);
  assert.ok(html.includes('Automata temp disk /tmp: 40% used'), 'temp disk line missing');
  assert.ok(html.includes('Automata disk alert threshold: 85%'), 'threshold missing');
  assert.ok(html.includes('cleanup-disk-badge-alert'), 'alert badge class missing');
  assert.ok(html.includes('>Automata disk alert<'), 'alert badge label missing');
  assert.ok(!html.includes('cleanup-disk-badge-ok'), 'ok badge must not show while alerting');
  assert.ok(html.includes('Last Automata disk alert:'), 'last disk alert time missing');
});

test('without a disk alert the badge reads ok', () => {
  const html = card(status({ disk_alert: false, last_disk_alert_at: undefined, worktree_disk: { path: '/data', percent: 10, alert: false } }));
  assert.ok(html.includes('cleanup-disk-badge-ok'), 'ok badge class missing: ' + html);
  assert.ok(html.includes('>Automata disk ok<'), 'ok badge label missing');
  assert.ok(!html.includes('cleanup-disk-badge-alert'), 'alert badge must not show');
  assert.ok(!html.includes('Last Automata disk alert:'), 'no last alert time when none fired');
});

test('a disk reading with no percent says it is not available, and no reading says none yet', () => {
  const unread = card(status({ worktree_disk: { path: '/data' }, temp_disk: undefined, disk_alert: false }));
  assert.ok(unread.includes('Automata disk use not available for /data'), 'unreadable line missing: ' + unread);
  const none = card(status({ worktree_disk: undefined, temp_disk: undefined, disk_alert: false }));
  assert.ok(none.includes('No Automata disk reading yet.'), 'no-reading line missing');
});

test('the card is read-only: no buttons, links or handlers, and it says so', () => {
  const html = card(status());
  assert.ok(html.includes('Read-only status'), 'read-only note missing');
  assert.ok(!/<button|<a[\s>]|<input|<select|<form|onclick=|href=/i.test(html), 'card must carry no controls: ' + html);
});

test('a missing or malformed status renders an empty card, not a throw', () => {
  for (const body of [null, undefined, {}, 'nope', 7, [], { worktrees: 'x', temp: 5, worktree_disk: 'y' }]) {
    const html = card(body);
    assert.ok(html.includes('Automata cleanup status'), 'title missing for ' + JSON.stringify(body));
    assert.ok(html.includes('cleanup-disk-badge-ok'), 'a status that is not alerting reads ok for ' + JSON.stringify(body));
    assert.equal(html.split('The Automata cleanup has not run yet.').length - 1, 2, 'both passes read as not yet run for ' + JSON.stringify(body));
  }
});

test('hostile text in every card field is escaped and never injectable', () => {
  const html = card(status({
    worktrees: pass({ mode: HOSTILE, last_error: HOSTILE }),
    temp: pass({ mode: HOSTILE, last_error: HOSTILE }),
    worktree_disk: { path: HOSTILE, percent: 90, alert: true },
    temp_disk: { path: HOSTILE },
  }));
  assert.ok(html.includes(ENTITY_IMG), 'hostile text should survive as an escaped entity');
  assert.ok(!html.includes('<img'), 'live tag must not survive: ' + html);
});

// ── the loader ───────────────────────────────────────────────────────────────

test('the Overview sub-tab carries the cleanup card slot and its loader reads the cleanup route', () => {
  const sandbox = loadApp();
  const body = vm.runInContext("_AUTOMATION_SUBTABS.find(s => s.id === 'overview').bodyHTML()", sandbox);
  assert.ok(body.includes('id="automationCleanupCard"'), 'cleanup slot missing from the Overview body');
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.ok(src.includes("url: '/api/autonomous/cleanup'"), 'loader must read GET /api/autonomous/cleanup');
  assert.ok(/'automation_safe_delete', 'automation_cleanup'\]/.test(src), 'stop-all list must include the cleanup loader');
});

// ── the settings fields ──────────────────────────────────────────────────────

function field(key) {
  const sandbox = loadApp();
  const fields = vm.runInContext('GENERAL_CONFIG_FIELDS', sandbox, { filename: 'app.js' });
  const autonomous = fields.find((sec) => sec.id === 'autonomous');
  return autonomous.fields.find((f) => f.key === key);
}

test('cleanup_mode is an off, report or on select defaulting to report', () => {
  const f = field('autonomous.automation.cleanup_mode');
  assert.ok(f, 'cleanup_mode field is declared');
  assert.equal(f.type, 'select');
  assert.deepEqual(Array.from(f.options), ['off', 'report', 'on']);
  assert.equal(f.default, 'report');
  assert.equal(f.labelKey, 'settings_automation_cleanup_mode');
  assert.ok(f.label && f.label.length > 0, 'hardcoded fallback label non-empty');
});

test('cleanup_retention_hours and cleanup_max_removals_per_pass are whole numbers of at least 1', () => {
  const cases = [
    ['autonomous.automation.cleanup_retention_hours', 'settings_automation_cleanup_retention_hours', '24'],
    ['autonomous.automation.cleanup_max_removals_per_pass', 'settings_automation_cleanup_max_removals_per_pass', '10'],
  ];
  for (const [key, labelKey, def] of cases) {
    const f = field(key);
    assert.ok(f, key + ' field is declared');
    assert.equal(f.type, 'number');
    assert.equal(f.min, 1, key + ' needs at least 1');
    assert.equal(f.step, 1, key + ' is a whole number');
    assert.equal(f.default, def);
    assert.equal(f.labelKey, labelKey);
    assert.ok(f.label && f.label.length > 0, 'hardcoded fallback label non-empty');
  }
});

test('disk_alert_percent is a whole percent from 1 to 100 with default 85', () => {
  const f = field('autonomous.automation.disk_alert_percent');
  assert.ok(f, 'disk_alert_percent field is declared');
  assert.equal(f.type, 'number');
  assert.equal(f.min, 1);
  assert.equal(f.max, 100);
  assert.equal(f.step, 1);
  assert.equal(f.default, '85');
  assert.equal(f.labelKey, 'settings_automation_disk_alert_percent');
  assert.ok(f.label && f.label.length > 0, 'hardcoded fallback label non-empty');
});

test('the settings fields use the same wording as their English locale text', () => {
  const en = bundle('en');
  for (const key of ['cleanup_mode', 'cleanup_retention_hours', 'cleanup_max_removals_per_pass', 'disk_alert_percent']) {
    const f = field('autonomous.automation.' + key);
    assert.equal(f.label, en[f.labelKey], key + ' fallback label should equal the English bundle text');
  }
});

// ── locale keys ──────────────────────────────────────────────────────────────

const KEYS = [
  'settings_automation_cleanup_mode', 'settings_automation_cleanup_retention_hours',
  'settings_automation_cleanup_max_removals_per_pass', 'settings_automation_disk_alert_percent',
  'cleanup_card_title', 'cleanup_read_only', 'cleanup_pass_worktrees', 'cleanup_pass_temp',
  'cleanup_mode_line', 'cleanup_last_run', 'cleanup_never_run', 'cleanup_counts',
  'cleanup_bytes_freed', 'cleanup_split', 'cleanup_last_error', 'cleanup_error_none',
  'cleanup_disk_heading', 'cleanup_disk_worktrees', 'cleanup_disk_temp', 'cleanup_disk_unreadable',
  'cleanup_disk_none', 'cleanup_disk_threshold', 'cleanup_disk_alert_badge', 'cleanup_disk_ok_badge',
  'cleanup_disk_alert_last', 'cleanup_failed',
];

test('every cleanup key exists in every locale bundle, says Automaton or Automata and has no tracker id', () => {
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of KEYS) {
      const v = b[k];
      assert.ok(typeof v === 'string' && v.length > 0, lang + ' missing or empty ' + k);
      assert.ok(/\bAutomat(?:on|a)\b/.test(v), lang + ' ' + k + ' must say Automaton or Automata: ' + v);
      assert.ok(!/\bPRDs?\b/.test(v), lang + ' ' + k + ' must say Automaton or Automata, never PRD');
      assert.ok(!/\b(?:BL|F|B|S)\d{1,6}\b/.test(v), lang + ' ' + k + ' carries a tracker id');
    }
  }
});

test('the non-English bundles carry the English text until translations arrive', () => {
  const en = bundle('en');
  for (const lang of LANGS.filter(l => l !== 'en')) {
    const b = bundle(lang);
    for (const k of KEYS) assert.equal(b[k], en[k], lang + ' ' + k + ' should match the English text for now');
  }
});

test('every t() key the card and loader use is in KEYS', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const start = src.indexOf('// ── Automata cleanup status card');
  const end = src.indexOf('// ── Automata progress card');
  const used = new Set((src.slice(start, end) + src.slice(src.indexOf('function _automationCleanupLoader'), src.indexOf('function _automationCleanupLoader') + 500))
    .match(/'cleanup_[a-z_]+'/g).map(s => s.slice(1, -1)));
  for (const k of used) assert.ok(KEYS.includes(k), 'key used by the card but not pinned: ' + k);
});
