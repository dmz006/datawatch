// Automata triage view model — node:test coverage.
//
// Covers the headline for each status, the reason fallbacks and the
// 240-character cap, the owner label, the waiting time (including the Go
// zero time), the inbox sort order, the stability of equal items and that
// sortInboxItems never mutates its input — plus the "how to unblock"
// hint, which the failed and blocked cards pick by reason kind.

const test = require('node:test');
const assert = require('node:assert/strict');
const M = require('./automata-triage-model.js');

test('headline: failed uses the task title', () => {
  assert.deepEqual(
    M.triageHeadline({ status: 'failed', task_title: 'Wire the parser' }),
    { key: 'triage_headline_failed', params: { task: 'Wire the parser' } },
  );
});

test('headline: blocked uses the first sentence of the blocked reason', () => {
  assert.deepEqual(
    M.triageHeadline({ status: 'blocked', blocked_reason: 'Needs an API key. Then retry.' }),
    { key: 'triage_headline_blocked', params: { reason: 'Needs an API key.' } },
  );
  assert.deepEqual(
    M.triageHeadline({ status: 'blocked', blocked_reason: 'Waiting on review\nmore detail' }),
    { key: 'triage_headline_blocked', params: { reason: 'Waiting on review' } },
  );
  assert.deepEqual(
    M.triageHeadline({ status: 'blocked', blocked_reason: '' }),
    { key: 'triage_headline_blocked', params: { reason: '' } },
  );
});

test('headline: merge_pending uses the branch name', () => {
  assert.deepEqual(
    M.triageHeadline({ status: 'merge_pending', branch: 'automaton/abc' }),
    { key: 'triage_headline_merge_pending', params: { branch: 'automaton/abc' } },
  );
});

test('headline: unknown or missing status falls back to none', () => {
  assert.deepEqual(M.triageHeadline({ status: 'weird' }), { key: 'triage_headline_none', params: {} });
  assert.deepEqual(M.triageHeadline(null), { key: 'triage_headline_none', params: {} });
});

test('reason: failed takes the first two lines of the error', () => {
  const error = 'line one\nline two\nline three';
  assert.deepEqual(M.triageReason({ status: 'failed', error }), { text: 'line one\nline two' });
});

test('reason: failed error is capped at 240 characters', () => {
  const error = 'x'.repeat(500);
  const out = M.triageReason({ status: 'failed', error });
  assert.equal(out.text.length, 240);
  assert.equal(out.text, 'x'.repeat(240));
});

test('reason: failed with no error falls back to session_end_reason', () => {
  assert.deepEqual(
    M.triageReason({ status: 'failed', error: '', session_end_reason: 'ended by operator' }),
    { text: 'ended by operator' },
  );
});

test('reason: blocked uses blocked_reason, not the error', () => {
  assert.deepEqual(
    M.triageReason({ status: 'blocked', blocked_reason: 'waiting', error: 'ignored' }),
    { text: 'waiting' },
  );
});

test('reason: blocked with no blocked_reason falls back to session_end_reason', () => {
  assert.deepEqual(
    M.triageReason({ status: 'blocked', blocked_reason: '', session_end_reason: 'session stopped' }),
    { text: 'session stopped' },
  );
});

test('reason: other statuses use session_end_reason', () => {
  assert.deepEqual(
    M.triageReason({ status: 'merge_pending', session_end_reason: 'done' }),
    { text: 'done' },
  );
});

test('reason: all empty returns the none key', () => {
  assert.deepEqual(M.triageReason({ status: 'failed' }), { key: 'triage_reason_none' });
  assert.deepEqual(M.triageReason({ status: 'blocked', blocked_reason: '   ' }), { key: 'triage_reason_none' });
  assert.deepEqual(M.triageReason(undefined), { key: 'triage_reason_none' });
});

// ── triageUnblockHint ───────────────────────────────────────────────

test('unblock: a failed record points at the task error', () => {
  assert.deepEqual(
    M.triageUnblockHint({ status: 'failed', error: 'boom', reason_source: 'task_error' }),
    { kind: 'error', key: 'triage_unblock_error' },
  );
});

test('unblock: a blocked record picks the hint of the source its record names', () => {
  assert.deepEqual(
    M.triageUnblockHint({ status: 'blocked', blocked_reason: 'held', reason_source: 'task_review' }),
    { kind: 'review', key: 'triage_unblock_review' },
  );
  assert.deepEqual(
    M.triageUnblockHint({ status: 'blocked', blocked_reason: 'e', reason_source: 'task_error' }),
    { kind: 'error', key: 'triage_unblock_error' },
  );
  assert.deepEqual(
    M.triageUnblockHint({ status: 'blocked', blocked_reason: 'v', reason_source: 'verdict' }),
    { kind: 'verdict', key: 'triage_unblock_verdict' },
  );
  assert.deepEqual(
    M.triageUnblockHint({ status: 'blocked', blocked_reason: 'b', reason_source: 'blocked_reason' }),
    { kind: 'blocked_reason', key: 'triage_unblock_blocked_reason' },
  );
});

test('unblock: a blocked record without a recognized source has no hint', () => {
  assert.equal(M.triageUnblockHint({ status: 'blocked', blocked_reason: 'held' }), null);
  assert.equal(M.triageUnblockHint({ status: 'blocked', blocked_reason: 'held', reason_source: 'mystery' }), null);
});

test('unblock: no reason text, other statuses or no record have no hint', () => {
  assert.equal(M.triageUnblockHint({ status: 'failed' }), null, 'failed with no error');
  assert.equal(M.triageUnblockHint({ status: 'blocked', blocked_reason: '  ' }), null, 'whitespace blocked reason');
  assert.equal(M.triageUnblockHint({ status: 'merge_pending', session_end_reason: 'x', reason_source: 'task_error' }), null, 'merge_pending');
  assert.equal(M.triageUnblockHint({ status: 'merge_blocked', session_end_reason: 'x' }), null, 'merge_blocked');
  assert.equal(M.triageUnblockHint(null), null, 'no record');
});

test('owner label: automation_name only when automation_id is set', () => {
  assert.equal(M.triageOwnerLabel({ automation_id: 'a1', automation_name: 'Nightly' }), 'Nightly');
  assert.equal(M.triageOwnerLabel({ automation_name: 'Nightly' }), '');
  assert.equal(M.triageOwnerLabel({}), '');
});

test('waiting since: returns first_seen_at as given', () => {
  const ts = '2026-11-08T10:00:00Z';
  assert.equal(M.triageWaitingSince({ first_seen_at: ts }), ts);
});

test('waiting since: empty when missing or the Go zero time', () => {
  assert.equal(M.triageWaitingSince({}), '');
  assert.equal(M.triageWaitingSince({ first_seen_at: '0001-01-01T00:00:00Z' }), '');
  assert.equal(M.triageWaitingSince(null), '');
});

test('sort: kinds in order, oldest first inside each kind', () => {
  const items = [
    { id: 'o1', kind: 'other', created_at: '2026-11-01T00:00:00Z' },
    { id: 's1', kind: 'security', created_at: '2026-11-01T00:00:00Z' },
    { id: 't2', kind: 'triage', first_seen_at: '2026-11-05T00:00:00Z' },
    { id: 'h1', kind: 'held_question', created_at: '2026-11-02T00:00:00Z' },
    { id: 'q2', kind: 'question', created_at: '2026-11-04T00:00:00Z' },
    { id: 't1', kind: 'triage', first_seen_at: '2026-11-03T00:00:00Z' },
    { id: 'q1', kind: 'question', created_at: '2026-11-03T00:00:00Z' },
  ];
  const ids = M.sortInboxItems(items).map((i) => i.id);
  assert.deepEqual(ids, ['q1', 'q2', 'h1', 't1', 't2', 's1', 'o1']);
});

test('sort: unknown kinds sort with other, undated items last in their kind', () => {
  const items = [
    { id: 'x', kind: 'mystery', created_at: '2026-11-01T00:00:00Z' },
    { id: 'nodate', kind: 'question' },
    { id: 'dated', kind: 'question', created_at: '2026-11-09T00:00:00Z' },
  ];
  assert.deepEqual(M.sortInboxItems(items).map((i) => i.id), ['dated', 'nodate', 'x']);
});

test('sort: equal items keep their input order', () => {
  const items = [
    { id: 'a', kind: 'triage', first_seen_at: '2026-11-01T00:00:00Z' },
    { id: 'b', kind: 'triage', first_seen_at: '2026-11-01T00:00:00Z' },
    { id: 'c', kind: 'triage', first_seen_at: '2026-11-01T00:00:00Z' },
  ];
  assert.deepEqual(M.sortInboxItems(items).map((i) => i.id), ['a', 'b', 'c']);
});

test('sort: returns a new array and never mutates the input', () => {
  const items = [
    { id: 'o', kind: 'other', created_at: '2026-11-02T00:00:00Z' },
    { id: 'q', kind: 'question', created_at: '2026-11-03T00:00:00Z' },
  ];
  const snapshot = JSON.parse(JSON.stringify(items));
  const originalOrder = items.slice();
  const out = M.sortInboxItems(items);
  assert.notEqual(out, items);
  assert.deepEqual(items, snapshot);
  assert.deepEqual(items, originalOrder);
  assert.equal(items[0], originalOrder[0]);
  assert.equal(items[1], originalOrder[1]);
  assert.deepEqual(M.sortInboxItems(undefined), []);
});
