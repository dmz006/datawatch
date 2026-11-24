// Finished work — PWA side: the Settings row for autonomous.finished_work, the
// finished-work section of the Housekeeping repairs card (owner and open
// question per row, run buttons) and the keep or drop decision prompt on a
// refused cancel or archive.
'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const APP = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const KEYS = ['settings_finished_work', 'finished_work_title', 'finished_work_purpose', 'finished_work_none', 'finished_work_row',
  'finished_work_question', 'finished_work_run_sweep', 'finished_work_run_audit', 'finished_work_run_failed', 'finished_work_run_done',
  'finished_work_decision_prompt', 'finished_work_reason_prompt',
  'finished_work_cause_finished_not_merged', 'finished_work_cause_code_done_rest_pending', 'finished_work_cause_reask_due',
  'finished_work_cause_branch_gone_unlanded', 'finished_work_cause_stuck_no_activity'];

test('the Settings page has a select row for autonomous.finished_work with on, shadow and off', () => {
  const m = APP.match(/\{ key: 'autonomous\.finished_work'[^\n]*\}/);
  assert.ok(m, 'autonomous.finished_work row missing from the settings fields');
  assert.match(m[0], /labelKey: 'settings_finished_work'/);
  assert.match(m[0], /options: \['on', 'shadow', 'off'\]/);
  assert.match(m[0], /default: 'on'/);
});

test('every finished-work string exists in all five locales', () => {
  for (const l of LOCALES) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));
    for (const k of KEYS) assert.ok(typeof b[k] === 'string' && b[k].length > 0, l + ' lacks ' + k);
  }
});

test('the Housekeeping repairs card draws the finished-work rows with run buttons', () => {
  assert.ok(APP.includes('_housekeepingFinishedWorkHtml(d.finished_work)'), 'card does not draw finished_work');
  assert.ok(APP.includes("window.runFinishedWork('sweep', this)") && APP.includes("window.runFinishedWork('audit', this)"));
  assert.ok(APP.includes("'/api/autonomous/finished-work/run'"));
  for (const c of ['finished_not_merged', 'code_done_rest_pending', 'reask_due', 'branch_gone_unlanded', 'stuck_no_activity']) {
    assert.ok(APP.includes("'finished_work_cause_" + c + "'"), 'cause ' + c + ' not handled');
  }
});

test('a refused cancel or archive asks for a keep or drop decision and a reason', () => {
  assert.ok(APP.includes('function _automataPostWithDecision(url)'));
  assert.match(APP, /keep or drop decision/);
  assert.ok(APP.includes("_automataPostWithDecision('/api/autonomous/prds/' + encodeURIComponent(id) + '/cancel')"));
  assert.ok(APP.includes("_automataPostWithDecision('/api/autonomous/prds/' + encodeURIComponent(id) + '/archive')"));
  assert.ok(APP.includes('JSON.stringify({ decision: d, reason })'));
});
