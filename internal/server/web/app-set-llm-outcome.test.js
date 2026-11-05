'use strict';
// app-set-llm-outcome.test.js (task 6cb90009) — the web's set-llm outcome
// feedback helpers and the queued-task LLM-control gates:
//   * llmOutcomeSummary renders the structured outcome — how many targets
//     changed vs were left alone (started/finished, applies on their next run)
//     plus any planning requeue (requeued / deferred_to_next_run / unchanged)
//     with the pools it moved.
//   * llmOutcomeInlineToast fires the toast only when there is a real outcome.
//   * llmControlsEnabled keeps the worker LLM/effort affordances available for
//     a running Automaton (queued tasks can still be retargeted) and closes
//     them for completed/blocked PRDs.
//   * taskOverridable is true only for the unstarted (queued) family — started
//     and finished (failed / blocked / review_needed) tasks are not.
//   * renderPRDWaitReason surfaces a PRD's planner-queue wait reason, empty
//     when there is none.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');
const APP_JS = path.resolve(__dirname, 'app.js');

function loadApp(stubs = {}) {
  const s = buildSandbox({ byId: stubs });
  vm.createContext(s);
  loadScript(APP_JS, s);
  return s;
}

test('llmOutcomeSummary names changed + left-alone targets and the planning requeue with pools', () => {
  const s = loadApp();
  const out = s.window.llmOutcomeSummary({
    override: {
      changed: [{ kind: 'task', id: 't1' }, { kind: 'task', id: 't2' }],
      left_alone: [{ kind: 'task', id: 't3', reason: 'already started' }],
    },
    planning: { outcome: 'requeued', old_pools: ['fast'], new_pools: ['slow'] },
  });
  assert.match(out, /2 targets? updated/);
  assert.match(out, /1 left alone/);
  assert.match(out, /started\/finished/);
  assert.match(out, /planner requeued to slow/);
  assert.match(out, /was fast/i);
});

test('llmOutcomeSummary handles deferred planning and an unchanged planner', () => {
  const s = loadApp();
  const deferred = s.window.llmOutcomeSummary({
    override: { changed: [{}], left_alone: [] },
    planning: { outcome: 'deferred_to_next_run', reason: 'a planning run is already queued' },
  });
  assert.match(deferred, /deferred to next run/);
  assert.match(deferred, /a planning run is already queued/);

  const unchanged = s.window.llmOutcomeSummary({ planning: { outcome: 'unchanged' } });
  assert.match(unchanged, /planner unchanged/);
});

test('llmOutcomeSummary is empty when the response carries no outcome', () => {
  const s = loadApp();
  assert.equal(s.window.llmOutcomeSummary({}), '');
  assert.equal(s.window.llmOutcomeSummary(null), '');
});

test('llmControlsEnabled: open for running + structural-edit states, closed for completed/blocked', () => {
  const s = loadApp();
  for (const st of ['running', 'in_progress', 'planning', 'needs_review', 'revisions_asked', 'cancelled', 'blocked']) {
    assert.equal(s.window.llmControlsEnabled({ status: st }), true, st);
  }
  for (const st of ['completed', 'failed', 'shipped', 'rejected']) {
    assert.equal(s.window.llmControlsEnabled({ status: st }), false, st);
  }
});

test('taskOverridable is true only for the unstarted (queued) family', () => {
  const s = loadApp();
  for (const st of ['pending', 'queued', 'waiting_capacity', 'waiting_decision', 'awaiting_approval']) {
    assert.equal(s.window.taskOverridable({ status: st }), true, st);
  }
  // started
  for (const st of ['in_progress', 'running_tests', 'verifying']) {
    assert.equal(s.window.taskOverridable({ status: st }), false, st);
  }
  // finished — the verifier flagged that failed/blocked/review_needed must NOT
  // be treated as unstarted.
  for (const st of ['completed', 'failed', 'blocked', 'review_needed', 'cancelled']) {
    assert.equal(s.window.taskOverridable({ status: st }), false, st);
  }
});

test('storyHasOverridableTask is true only when the story has a queued task', () => {
  const s = loadApp();
  assert.equal(s.window.storyHasOverridableTask({ tasks: [{ status: 'completed' }, { status: 'in_progress' }] }), false);
  assert.equal(s.window.storyHasOverridableTask({ tasks: [{ status: 'completed' }, { status: 'queued' }] }), true);
  assert.equal(s.window.storyHasOverridableTask({ tasks: [] }), false);
});

test('renderPRDWaitReason shows the wait_reason when present and is empty when absent', () => {
  const s = loadApp();
  const chip = s.window.renderPRDWaitReason({ wait_reason: 'waiting for a planner slot in slow' });
  assert.match(chip, /waiting for a planner slot in slow/);
  assert.match(chip, /⏳/);
  assert.equal(s.window.renderPRDWaitReason({}), '');
  assert.equal(s.window.renderPRDWaitReason(null), '');
});

// 6cb90009 — web: PRD-level worker LLM/effort controls stay available for a
// running Automaton (its queued tasks can still be retargeted; the toast
// shows changed vs left-alone), and they close for completed PRDs; per-task
// controls stay per-task overridable.
function renderTop(s, expr) {
  return vm.runInContext(expr, s);
}

test('renderPRDActions keeps the PRD-level LLM button for running PRDs and removes it for completed (6cb90009)', () => {
  const prd = (status) => ({ id: 'p1', status });
  const html = (status) => renderTop(loadApp(), `renderPRDActions(${JSON.stringify(prd(status))})`);
  assert.match(html('running'), /openPRDSetLLMModal/);
  assert.match(html('cancelled'), /openPRDSetLLMModal/);
  assert.doesNotMatch(html('completed'), /openPRDSetLLMModal/);
});

test('renderStory keeps per-task LLM controls for queued tasks of a running PRD but disables them for started/finished tasks (6cb90009)', () => {
  const story = {
    id: 's1', title: 'S', status: 'running',
    tasks: [
      { id: 'tq', status: 'queued' },
      { id: 'tr', status: 'running' },
      { id: 'tf', status: 'failed' },
    ],
  };
  const prd = { id: 'p1', status: 'running' };
  const html = renderTop(loadApp(), `renderStory(${JSON.stringify(prd)}, ${JSON.stringify(story)})`);
  // the LLM/spec editor is one button whose onclick names the task id
  const editors = (html.match(/openPRDEditTaskModal/g) || []).length;
  assert.equal(editors, 1, 'exactly one task has the LLM/spec editor');
  assert.match(html, /openPRDEditTaskModal\(.{0,80}?tq/);
  assert.doesNotMatch(html, /openPRDEditTaskModal\(.{0,80}?tr/);
  assert.doesNotMatch(html, /openPRDEditTaskModal\(.{0,80}?tf/);
});
