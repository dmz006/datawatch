// Closing an Automaton — PWA side: when the reply of a cancel or an archive
// says task worktrees or branches were kept, the page says how many and why.
'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const APP = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
const GO = fs.readFileSync(path.join(__dirname, '..', '..', 'autonomous', 'task_worktree_keep.go'), 'utf8');
const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const REASONS = [...GO.matchAll(/^\ttaskKeep\w+\s*=\s*"(\w+)"/gm)].map(m => m[1]);

test('every kept reason and the notice exist in all five locales', () => {
  assert.ok(REASONS.length >= 13, 'reason constants not found: ' + REASONS);
  for (const l of LOCALES) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));
    for (const k of ['automata_task_worktrees_kept', ...REASONS.map(r => 'task_keep_reason_' + r)]) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, l + ' lacks ' + k);
    }
  }
});

function notice(reply) {
  const src = APP.match(/function _automataKeptNotice\(res\) \{[\s\S]*?\n\}/);
  assert.ok(src, '_automataKeptNotice missing');
  const shown = [];
  const fn = new Function('t', 'tOr', 'showToast', src[0] + '\nreturn _automataKeptNotice;')(
    (k, v) => k + '|' + v.join('|'), k => k, msg => shown.push(msg));
  assert.equal(fn(reply), reply, 'the reply must pass through');
  return shown;
}

test('the notice names the counts and the reasons, and is silent when nothing was kept', () => {
  assert.deepEqual(notice({ task_worktree_cleanup: { worktrees_kept: 2, branches_kept: 3, reasons: { unmerged_branch: 1, uncommitted_work: 2 } } }),
    ['automata_task_worktrees_kept|2|3|task_keep_reason_uncommitted_work (2), task_keep_reason_unmerged_branch (1)']);
  assert.deepEqual(notice({ task_worktree_cleanup: { worktrees_kept: 0, branches_kept: 0 } }), []);
  assert.deepEqual(notice({ status: 'cancelled' }), []);
});

test('cancel and archive replies go through the notice', () => {
  assert.match(APP, /\}\)\.then\(_automataKeptNotice\);\n\}\nwindow\.batchAutomataAction/);
});
