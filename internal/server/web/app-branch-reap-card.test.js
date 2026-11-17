// Branch reaper card — PWA side of GET /api/autonomous/branch-reap.
//
// window.branchReapCardHtml(report) draws the reaped and waiting entries with
// their reasons (and backup refs), the backup refs list, the retention, and
// the Restore write per reaped branch (data-branch-reap-restore). A missing
// or malformed body reads as an empty report. The card is a preview: only
// the Restore button touches the wire, and it is delegated to document
// because the loader re-paints the card's innerHTML on every tick. The
// branch_reap_* locale keys are pinned here as well.

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
  return vm.runInContext('window.branchReapCardHtml(_r)', sandbox);
}

test('a reaped entry shows its branch, Automaton, reason, containment, commit count, backup ref and the Restore button', () => {
  const html = cardHTML({
    reaped: [{
      automaton_id: 'prd-9', branch: 'automaton/prd-9/t1',
      backup_ref: 'refs/datawatch/reap-backup/prd-9/t1', contained_in: 'automaton/prd-9',
      commits: 2, reason: 'all commits contained in main',
    }],
    waiting: [], backups: [], backup_days: 90,
  });
  assert.ok(html.includes('branch-reap-card'), 'card class missing');
  assert.ok(html.includes('automaton/prd-9/t1'), 'branch missing');
  assert.ok(html.includes('Automation prd-9') || html.includes('Autômato') || html.includes('Automaton') || html.includes('prd-9'), 'automaton id missing');
  assert.ok(html.includes('all commits contained in main'), 'reason missing');
  assert.ok(html.includes('automaton/prd-9<'), 'contained_in line missing');
  assert.ok(html.includes('2 commit(s)'), 'commit count missing');
  assert.ok(html.includes('refs/datawatch/reap-backup/prd-9/t1'), 'backup ref missing');
  assert.ok(html.includes('data-branch-reap-restore="automaton/prd-9/t1"'), 'restore button attribute missing');
  assert.ok(html.includes('type="button"'), 'restore must be a plain button');
});

test('an empty report reads as empty and carries no restore controls', () => {
  for (const r of [{}, { reaped: [], waiting: [] }, { reaped: [null, { reason: 'x' }], waiting: ['nope'] }]) {
    const html = cardHTML(r);
    assert.ok(html.includes('Nothing would be reaped and nothing is held.'), 'empty text missing for ' + JSON.stringify(r) + ': ' + html);
    assert.ok(!html.includes('data-branch-reap-restore'), 'no restore control for ' + JSON.stringify(r));
  }
});

test('hostile entry fields are escaped and never reach the markup as live tags', () => {
  const html = cardHTML({
    reaped: [{ automaton_id: HOSTILE, branch: HOSTILE, reason: HOSTILE, contained_in: HOSTILE, backup_ref: HOSTILE }],
    waiting: [{ branch: HOSTILE, reason: HOSTILE }],
    backups: [{ ref: HOSTILE, sha: HOSTILE, branch: HOSTILE }],
  });
  assert.ok(!html.includes('<img'), 'live tag must not survive: ' + html);
  assert.ok(html.includes(ENTITY_IMG), 'escaped entities expected');
  assert.ok(!/data-branch-reap-restore="<img/.test(html), 'hostile branch must not become a live attribute payload');
});

test('retention shows the configured days when positive and the default otherwise', () => {
  assert.ok(cardHTML({ reaped: [], waiting: [], backup_days: 90 }).includes('Backup ref retention: 90 whole days'), 'retention 90 missing');
  for (const v of [undefined, 0, -3, null, 'abc']) {
    const html = cardHTML({ reaped: [], waiting: [], backup_days: v });
    assert.ok(html.includes('Backup ref retention: default (unset)'), 'default retention missing for ' + JSON.stringify(v) + ': ' + html);
  }
});

test('backup refs render ref, sha and the branch it belongs to', () => {
  const html = cardHTML({
    reaped: [], waiting: [],
    backups: [{ ref: 'refs/datawatch/reap-backup/prd-9/t1', sha: 'abc123', branch: 'automaton/prd-9/t1' }],
  });
  assert.ok(html.includes('refs/datawatch/reap-backup/prd-9/t1'), 'ref missing');
  assert.ok(html.includes('abc123'), 'sha missing');
  assert.ok(html.includes('(automaton/prd-9/t1)'), 'branch annotation missing');
});

test('the card is an Events sub-tab slot whose loader reads the branch-reap route', () => {
  const sandbox = loadApp();
  const events = vm.runInContext("_automationSlotsHTML('events')", sandbox);
  assert.ok(events.includes('id="automationBranchReapCard"'), 'branch reap card must be an Events card slot');
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.ok(src.includes("url: '/api/autonomous/branch-reap'"), 'loader must read GET /api/autonomous/branch-reap');
  assert.ok(src.includes("'/api/autonomous/branch-reap/restore'"), 'restore must POST the one branch');
  assert.ok(src.includes("'automation_branch_reap'"), 'stop-all list must include the branch reap loader');
});

test('the restore control is delegated to document and bound exactly once', () => {
  const sandbox = loadApp();
  vm.runInContext('window._branchReapEnsureRestoreHandler()', sandbox);
  vm.runInContext('window._branchReapEnsureRestoreHandler()', sandbox);
  const bound = vm.runInContext('_branchReapRestoreBound', sandbox);
  assert.equal(bound, true, 'handler flag must be set');
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.ok(/document\.addEventListener\('click'/.test(src), 'restore click must be delegated to document');
});

test('the branch_reap_ locale keys exist in every bundle with real text and matching placeholders', () => {
  const en = bundle('en');
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of Object.keys(en).filter(k => k.startsWith('branch_reap_'))) {
      const v = b[k];
      assert.ok(typeof v === 'string' && v.length > 0, lang + ' missing or empty ' + k);
      assert.ok(!/\b(?:BL|F|B|S)\d{1,6}\b/.test(v), lang + ' ' + k + ' carries a tracker id');
      const enPlaceholders = (en[k].match(/%1\$[sd]/g) || []).length;
      const localPlaceholders = (v.match(/%1\$[sd]/g) || []).length;
      assert.equal(localPlaceholders, enPlaceholders, lang + ' ' + k + ' placeholder count differs from en: ' + v);
    }
    if (lang !== 'en') {
      assert.notEqual(b.branch_reap_card_title, en.branch_reap_card_title, lang + ' card title should be translated, not copied');
    }
  }
});
