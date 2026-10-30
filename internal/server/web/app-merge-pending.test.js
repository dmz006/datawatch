// Merge-pending badge and safe-to-delete report — PWA side.
//
// window.mergeBadgeHtml(entry) turns one entry of GET /api/autonomous/merge
// into the badge; only the pending and needs_look states draw one.
// window.mergeBadgeForAutomation(automation, byId) picks the entry an
// Automation card shows. window.safeDeleteCardHtml(report) draws the read-only
// report of GET /api/autonomous/safe-to-delete. The merge_* and safe_delete_*
// locale keys are pinned here as well.

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

function entry(over) {
  return {
    automaton_id: 'p1',
    name: 'Refresh indexes',
    branch: 'automaton/p1',
    state: 'pending',
    commits_ahead: 3,
    pending_since: '2026-10-24T10:00:00Z',
    alerted_at: null,
    ...over,
  };
}

function badge(e) {
  const sandbox = loadApp();
  sandbox._e = e;
  return vm.runInContext('window.mergeBadgeHtml(_e)', sandbox);
}

function pick(automation, byId) {
  const sandbox = loadApp();
  sandbox._a = automation;
  sandbox._idx = byId;
  return JSON.parse(vm.runInContext('JSON.stringify(window.mergeBadgeForAutomation(_a, _idx))', sandbox));
}

function report(body) {
  const sandbox = loadApp();
  sandbox._r = body;
  return vm.runInContext('window.safeDeleteCardHtml(_r)', sandbox);
}

function safeReport(over) {
  return {
    safe: [
      { branch: 'automaton/aaa', worktree: '/wt/aaa', reason: 'all commits on main', commits: 4 },
      { branch: 'automaton/bbb', worktree: '', reason: 'no commits ahead of main', commits: 0 },
    ],
    held: [
      { branch: 'automaton/ccc', worktree: '/wt/ccc', state: 'pending' },
      { branch: 'automaton/ddd', worktree: '/wt/ddd', state: 'merged', dirty: true, note: 'Automaton worktree has uncommitted changes' },
    ],
    held_dirty: 1,
    ...over,
  };
}

// ── the badge ────────────────────────────────────────────────────────────────

test('a pending entry draws the merge pending badge with its branch and commit count', () => {
  const html = badge(entry());
  assert.ok(html.includes('merge-badge-pending'), 'pending class missing: ' + html);
  assert.ok(html.includes('>Automaton merge pending<'), 'pending label missing');
  assert.ok(html.includes('automaton/p1'), 'branch missing from the title');
  assert.ok(html.includes('3 commits not on main'), 'commit count missing from the title: ' + html);
});

test('a needs_look entry draws the needs a look badge', () => {
  const html = badge(entry({ state: 'needs_look' }));
  assert.ok(html.includes('merge-badge-needs-look'), 'needs-look class missing: ' + html);
  assert.ok(html.includes('>Automaton needs a look<'), 'needs-look label missing');
  assert.ok(!html.includes('merge-badge-pending'), 'pending class must not show on a needs-look entry');
});

test('merged, empty and unknown entries draw no badge', () => {
  for (const state of ['merged', 'empty', 'unknown']) {
    assert.equal(badge(entry({ state })), '', state + ' must draw nothing');
  }
});

test('a missing, malformed or hostile entry draws no badge and never throws', () => {
  for (const bad of [null, undefined, 'pending', 7, []]) {
    assert.equal(badge(bad), '', 'no badge for ' + String(bad));
  }
  for (const state of ['constructor', 'hasOwnProperty', '__proto__', '', undefined, HOSTILE]) {
    assert.equal(badge(entry({ state })), '', 'no badge for state ' + String(state));
  }
});

test('hostile text in the branch is escaped in the badge title', () => {
  const html = badge(entry({ branch: HOSTILE }));
  assert.ok(html.includes('merge-badge-pending'), 'badge still drawn');
  assert.ok(html.includes(ENTITY_IMG), 'escaped branch missing: ' + html);
  assert.ok(!html.includes('<img'), 'live tag must not survive: ' + html);
});

// ── which entry an Automation card shows ─────────────────────────────────────

test('an Automation card shows its member that needs a look before one that is only pending', () => {
  const byId = {
    p1: entry({ automaton_id: 'p1', state: 'pending' }),
    p2: entry({ automaton_id: 'p2', state: 'needs_look', branch: 'automaton/p2' }),
    p3: entry({ automaton_id: 'p3', state: 'merged' }),
  };
  const got = pick({ members: [{ prd_id: 'p1' }, { prd_id: 'p2' }, { prd_id: 'p3' }] }, byId);
  assert.equal(got.automaton_id, 'p2');
  const only = pick({ members: [{ prd_id: 'p3' }, { prd_id: 'p1' }] }, byId);
  assert.equal(only.automaton_id, 'p1');
});

test('an Automation card with no badge-state member, no members or no index shows nothing', () => {
  const byId = { p3: entry({ automaton_id: 'p3', state: 'merged' }) };
  assert.equal(pick({ members: [{ prd_id: 'p3' }] }, byId), null);
  assert.equal(pick({ members: [] }, byId), null);
  assert.equal(pick({}, byId), null);
  assert.equal(pick(null, byId), null);
  assert.equal(pick({ members: [{ prd_id: 'p1' }] }, undefined), null);
  assert.equal(pick({ members: [{ prd_id: 'constructor' }, null, {}] }, byId), null);
});

test('the Automation card carries the badge only when a member has one', () => {
  const sandbox = loadApp();
  sandbox._a = { id: 'a1', name: 'Nightly', status: 'active', members: [{ id: 'm1', prd_id: 'p1' }] };
  sandbox._ctx = { prdTitles: {}, now: Date.now(), mergeById: { p1: entry() } };
  const withBadge = vm.runInContext('buildAutomationCardHTML(_a, _ctx)', sandbox);
  assert.ok(withBadge.includes('merge-badge-pending'), 'badge missing from the card: ' + withBadge);
  sandbox._ctx = { prdTitles: {}, now: Date.now(), mergeById: {} };
  const without = vm.runInContext('buildAutomationCardHTML(_a, _ctx)', sandbox);
  assert.ok(!without.includes('merge-badge'), 'no badge without a merge entry');
});

// ── the loader keeps its last good read ──────────────────────────────────────

test('loadMergeStatus indexes the automata by id and keeps the last read when a later one fails', async () => {
  const sandbox = loadApp();
  let fail = false;
  sandbox.apiFetch = () => (fail ? Promise.reject(new Error('boom')) : Promise.resolve({ automata: [entry(), entry({ automaton_id: 'p2', state: 'needs_look' })] }));
  const first = await vm.runInContext('window.loadMergeStatus()', sandbox);
  assert.deepEqual(Object.keys(first).sort(), ['p1', 'p2']);
  fail = true;
  vm.runInContext('window._mergeStatus.at = 0', sandbox);
  const second = await vm.runInContext('window.loadMergeStatus()', sandbox);
  assert.deepEqual(Object.keys(second).sort(), ['p1', 'p2'], 'a failed read must keep the last index');
});

// ── the safe-to-delete report ────────────────────────────────────────────────

test('the report lists the safe branches and the held ones with their details', () => {
  const html = report(safeReport());
  assert.ok(html.includes('Automaton safe-to-delete report'), 'card title missing');
  assert.ok(html.includes('Automaton branches safe to delete (2)'), 'safe heading missing: ' + html);
  assert.ok(html.includes('Automaton branches held (2)'), 'held heading missing');
  for (const b of ['automaton/aaa', 'automaton/bbb', 'automaton/ccc', 'automaton/ddd']) {
    assert.ok(html.includes(b), b + ' missing');
  }
  assert.ok(html.includes('Automaton worktree: /wt/aaa'), 'worktree line missing');
  assert.ok(html.includes('all commits on main'), 'safe reason missing');
  assert.ok(html.includes('Automaton commits not on main: 4'), 'commit count missing');
  assert.ok(html.includes('Automaton branch state: Automaton merge pending'), 'held state label missing: ' + html);
  assert.ok(html.includes('uncommitted changes'), 'dirty note missing');
});

test('the report is read-only: no buttons, links or handlers, and it says so', () => {
  const html = report(safeReport());
  assert.ok(html.includes('Read-only report'), 'read-only note missing');
  assert.ok(!/<button|<a[\s>]|<input|<select|<form|onclick=|href=/i.test(html), 'report must carry no controls: ' + html);
  assert.ok(!/>\s*(delete|push|merge|remove)\b/i.test(html.replace(/<[^>]*>/g, '>')), 'no delete, push, merge or remove action text');
});

test('an empty, missing or malformed report renders both empty states, not a throw', () => {
  for (const body of [null, undefined, {}, 'nope', { safe: 'x', held: 5 }, { safe: [], held: [] }]) {
    const html = report(body);
    assert.ok(html.includes('No Automaton branch is safe to delete.'), 'safe empty state missing for ' + JSON.stringify(body));
    assert.ok(html.includes('No Automaton branch is held.'), 'held empty state missing for ' + JSON.stringify(body));
    assert.ok(html.includes('Automaton branches safe to delete (0)') && html.includes('Automaton branches held (0)'), 'zero counts missing');
  }
});

test('a held entry with an unknown state shows the raw value escaped, and non-object entries are skipped', () => {
  const html = report({ safe: [null, 'x'], held: [{ branch: 'automaton/q', state: 'constructor' }, 7] });
  assert.ok(html.includes('Automaton branches safe to delete (0)'), 'non-object safe entries must be skipped');
  assert.ok(html.includes('Automaton branches held (1)'), 'non-object held entries must be skipped');
  assert.ok(html.includes('Automaton branch state: constructor'), 'inherited name must not map to a locale key: ' + html);
});

test('hostile text in every report field is escaped and never injectable', () => {
  const html = report({
    safe: [{ branch: HOSTILE, worktree: HOSTILE, reason: HOSTILE, commits: 1 }],
    held: [{ branch: HOSTILE, worktree: HOSTILE, state: HOSTILE, dirty: true, note: HOSTILE }],
  });
  assert.ok(html.includes(ENTITY_IMG), 'hostile text should survive as an escaped entity');
  assert.ok(!html.includes('<img'), 'live tag must not survive: ' + html);
});

// ── locale keys ──────────────────────────────────────────────────────────────

const KEYS = [
  'merge_state_pending', 'merge_state_needs_look', 'merge_state_merged', 'merge_state_empty',
  'merge_state_unknown', 'merge_badge_title', 'safe_delete_card_title', 'safe_delete_read_only',
  'safe_delete_safe_heading', 'safe_delete_held_heading', 'safe_delete_safe_empty',
  'safe_delete_held_empty', 'safe_delete_worktree', 'safe_delete_commits', 'safe_delete_state',
  'safe_delete_dirty', 'safe_delete_failed',
  'settings_automation_merge_pending_mode', 'settings_automation_merge_pending_alert_minutes',
];

test('every merge-pending and safe-to-delete key exists in every locale bundle, says Automaton or Automata and has no tracker id', () => {
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
