// Remote branch reaper card — PWA side of GET /api/autonomous/remote-reap.
//
// window.remoteReapCardHtml(report) draws the reaped and held entries with
// their reasons (backup refs, base, tip, age), the origin backup refs with
// their SHAs, the retention window, and the mode note (report-only vs
// enabled). A missing or malformed body reads as an empty report. The report
// itself deletes nothing on origin; only the Restore button on a reaped
// entry touches the wire, and it is delegated to document because the loader
// re-paints the card's innerHTML on every tick. The remote_reap_* locale
// keys are pinned here as well.

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

function cardHTML(r) {
  const sandbox = loadApp();
  sandbox._r = r;
  return vm.runInContext('window.remoteReapCardHtml(_r)', sandbox);
}

test('a reaped entry shows its branch, Automaton, base, tip, age, reason, backup ref and the Restore button', () => {
  const html = cardHTML({
    report_only: false,
    reaped: [{
      automaton_id: 'prd-9', branch: 'automaton/prd-9', base: 'main',
      tip: 'abc12', age_days: 42, backup_ref: 'refs/datawatch/remote-reap-backup/prd-9/123',
      reason: 'merged into main 42 days ago',
    }],
    held: [], backups: [], retention_days: 30,
  });
  assert.ok(html.includes('remote-reap-card'), 'card class missing');
  assert.ok(html.includes('Remote branch reaper'), 'card title missing');
  assert.ok(html.includes('automaton/prd-9'), 'branch missing');
  assert.ok(html.includes('Automaton prd-9'), 'automaton id missing');
  assert.ok(html.includes('Merged into main'), 'base missing');
  assert.ok(html.includes('Tip abc12'), 'tip missing');
  assert.ok(html.includes('42 days old'), 'age missing');
  assert.ok(html.includes('merged into main 42 days ago'), 'reason missing');
  assert.ok(html.includes('refs/datawatch/remote-reap-backup/prd-9/123'), 'backup ref missing');
  assert.ok(html.includes('data-remote-reap-restore="automaton/prd-9"'), 'restore button attribute missing');
  assert.ok(html.includes('type="button"'), 'restore must be a plain button');
});

test('the mode note is enabled vs report-only depending on report_only', () => {
  assert.ok(
    cardHTML({ reaped: [{ branch: 'b' }], held: [], report_only: false })
      .includes('Enabled — a pass deletes the listed branches from origin after backing up their tips.'),
    'enabled note missing',
  );
  for (const r of [{ reaped: [{ branch: 'b' }] }, { reaped: [{ branch: 'b' }], report_only: true }]) {
    assert.ok(
      cardHTML(r).includes('Report-only — nothing is deleted on origin until remote reap is enabled in the Automata settings.'),
      'report-only note missing for ' + JSON.stringify(r),
    );
  }
});

test('an empty report reads as empty and carries no restore controls', () => {
  for (const r of [{}, { reaped: [], held: [] }, { reaped: [null, { reason: 'x' }], held: ['nope'] }]) {
    const html = cardHTML(r);
    assert.ok(html.includes('Nothing would be reaped and nothing is held.'), 'empty text missing for ' + JSON.stringify(r) + ': ' + html);
    assert.ok(!html.includes('data-remote-reap-restore'), 'no restore control for ' + JSON.stringify(r));
  }
});

test('a held entry shows its reason but no Restore button', () => {
  const html = cardHTML({
    reaped: [],
    held: [{ branch: 'automaton/prd-7', reason: 'not fully merged into main' }],
  });
  assert.ok(html.includes('automaton/prd-7'), 'held branch missing');
  assert.ok(html.includes('not fully merged into main'), 'held reason missing');
  assert.ok(html.includes('Held — left on origin'), 'held heading missing');
  assert.ok(!html.includes('data-remote-reap-restore'), 'held entry must not offer restore');
});

test('hostile entry fields are escaped and never reach the markup as live tags', () => {
  const html = cardHTML({
    reaped: [{ automaton_id: HOSTILE, branch: HOSTILE, base: HOSTILE, tip: HOSTILE, reason: HOSTILE, backup_ref: HOSTILE }],
    held: [{ branch: HOSTILE, reason: HOSTILE }],
    backups: [{ ref: HOSTILE, sha: HOSTILE, branch: HOSTILE }],
  });
  assert.ok(!html.includes('<img'), 'live tag must not survive: ' + html);
  assert.ok(html.includes(ENTITY_IMG), 'escaped entities expected');
  assert.ok(!/data-remote-reap-restore="<img/.test(html), 'hostile branch must not become a live attribute payload');
});

test('retention shows the configured days when positive and the default otherwise', () => {
  assert.ok(cardHTML({ reaped: [], held: [], retention_days: 90 }).includes('Retention window: 90 whole days'), 'retention 90 missing');
  for (const v of [undefined, 0, -3, null, 'abc']) {
    const html = cardHTML({ reaped: [], held: [], retention_days: v });
    assert.ok(html.includes('Retention window: default (unset)'), 'default retention missing for ' + JSON.stringify(v) + ': ' + html);
  }
});

test('backup refs render ref, sha and the branch it belongs to', () => {
  const html = cardHTML({
    reaped: [], held: [],
    backups: [{ ref: 'refs/datawatch/remote-reap-backup/prd-9/123', sha: 'abc123', branch: 'automaton/prd-9' }],
  });
  assert.ok(html.includes('refs/datawatch/remote-reap-backup/prd-9/123'), 'ref missing');
  assert.ok(html.includes('abc123'), 'sha missing');
  assert.ok(html.includes('(automaton/prd-9)'), 'branch annotation missing');
});

test('the card is an Events sub-tab slot whose loader reads the remote-reap route', () => {
  const sandbox = loadApp();
  const events = vm.runInContext("_automationSlotsHTML('events')", sandbox);
  assert.ok(events.includes('id="automationRemoteReapCard"'), 'remote reap card must be an Events card slot');
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.ok(src.includes("url: '/api/autonomous/remote-reap'"), 'loader must read GET /api/autonomous/remote-reap');
  assert.ok(src.includes("'/api/autonomous/remote-reap/restore'"), 'restore must POST the one branch');
  assert.ok(src.includes("'automation_remote_reap'"), 'stop-all list must include the remote reap loader');
});

test('the restore control is delegated to document and bound exactly once', () => {
  const sandbox = loadApp();
  vm.runInContext('window._remoteReapEnsureRestoreHandler()', sandbox);
  vm.runInContext('window._remoteReapEnsureRestoreHandler()', sandbox);
  const bound = vm.runInContext('_remoteReapRestoreBound', sandbox);
  assert.equal(bound, true, 'handler flag must be set');
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.ok(/document\.addEventListener\('click'/.test(src), 'restore click must be delegated to document');
});

test('the remote_reap_ locale keys exist in every bundle with real text and matching placeholders', () => {
  const en = bundle('en');
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of Object.keys(en).filter(k => k.startsWith('remote_reap_'))) {
      const v = b[k];
      assert.ok(typeof v === 'string' && v.length > 0, lang + ' missing or empty ' + k);
      assert.ok(!/\b(?:BL|F|B|S)\d{1,6}\b/.test(v), lang + ' ' + k + ' carries a tracker id');
      const enPlaceholders = (en[k].match(/%1\$[sd]/g) || []).length;
      const localPlaceholders = (v.match(/%1\$[sd]/g) || []).length;
      assert.equal(localPlaceholders, enPlaceholders, lang + ' ' + k + ' placeholder count differs from en: ' + v);
    }
    if (lang !== 'en') {
      assert.notEqual(b.remote_reap_card_title, en.remote_reap_card_title, lang + ' card title should be translated, not copied');
    }
  }
});
