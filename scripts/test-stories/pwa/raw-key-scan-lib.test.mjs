// scripts/test-stories/pwa/raw-key-scan-lib.test.mjs — node:test for the pure half of
// the raw-key scan (TS-856). No browser needed.
import test from 'node:test';
import assert from 'node:assert/strict';
import { classify, allowed, findingsOf, formatFindings, KEY_PATTERN, COLLECT_SRC } from './raw-key-scan-lib.mjs';

const KEYS = new Set(['pause_badge_paused', 'nav_sessions', 'save', 'settings_attempt_cap_group']);

test('a string of lowercase words joined by underscores is a raw key, in the bundle or not', () => {
  assert.deepEqual(classify('pause_badge_paused', KEYS), { reason: 'pattern', key: 'pause_badge_paused' });
  assert.deepEqual(classify('  brand_new_key ', KEYS), { reason: 'pattern', key: 'brand_new_key' });
});

test('a string that is exactly an English bundle key is a raw key', () => {
  assert.deepEqual(classify('save', KEYS), { reason: 'bundle', key: 'save' });
  assert.equal(classify('Save', KEYS), null, 'a capitalised label is not the key');
});

test('a bundle key with an underscore inside longer text is a raw key', () => {
  assert.deepEqual(classify('pause_badge_paused 2', KEYS), { reason: 'token', key: 'pause_badge_paused' });
  assert.deepEqual(classify('Attempts: nav_sessions (3)', KEYS), { reason: 'token', key: 'nav_sessions' });
});

test('ordinary text, ids with other characters and unknown snake_case inside text are fine', () => {
  for (const s of ['Paused', 'Paused 2', 'New work is paused', 'max_total is 6', 'autonomous.attempt_cap.max_total', 'abc-def', 'Sessions', '12:30', '']) {
    assert.equal(classify(s, KEYS), null, s);
  }
});

test('KEY_PATTERN needs at least two words and starts with a letter', () => {
  assert.ok(KEY_PATTERN.test('a_b'));
  assert.ok(!KEY_PATTERN.test('word'));
  assert.ok(!KEY_PATTERN.test('_a_b'));
  assert.ok(!KEY_PATTERN.test('A_b'));
});

test('an allow-list entry needs a reason and matches an exact string, one of several or a prefix', () => {
  assert.equal(allowed('review_needed', 'span', [{ text: 'review_needed', reason: 'status id shown verbatim' }]), true);
  assert.equal(allowed('review_needed', 'span', [{ text: 'review_needed' }]), false, 'no reason, no allowance');
  assert.equal(allowed('queue_depth: 3', 'span', [{ prefix: 'queue_depth', reason: 'metric name' }]), true);
  assert.equal(allowed('other_thing', 'span', [{ prefix: 'queue_depth', reason: 'metric name' }]), false);
  assert.equal(allowed('b_two', 'span', [{ texts: ['a_one', 'b_two'], reason: 'ids' }]), true);
  assert.equal(allowed('c_three', 'span', [{ texts: ['a_one', 'b_two'], reason: 'ids' }]), false);
});

test('an allow-list entry limited by where allows the string only at that place', () => {
  const list = [{ text: 'cpu_pct', where: 'view=observer', reason: 'API field name' }, { where: 'th.task-routing-type', reason: 'row keys' }];
  assert.equal(allowed('cpu_pct', 'div [view=observer]', list), true);
  assert.equal(allowed('cpu_pct', 'div [view=sessions]', list), false);
  assert.equal(allowed('docs_set', 'th.task-routing-type [view=settings]', list), true, 'a place-only entry allows every string there');
  assert.equal(allowed('docs_set', 'span [view=settings]', list), false);
});

test('findingsOf keeps the raw keys, drops allowed ones and duplicates, and records the checkpoint', () => {
  const items = [
    { kind: 'text', text: 'pause_badge_paused', where: 'span#headerPauseLabel' },
    { kind: 'text', text: 'pause_badge_paused', where: 'span#headerPauseLabel' },
    { kind: 'aria-label', text: 'nav_sessions', where: 'button.nav-btn' },
    { kind: 'text', text: 'Sessions', where: 'button.nav-btn' },
    { kind: 'text', text: 'review_needed', where: 'span.chip' },
  ];
  const f = findingsOf(items, KEYS, { at: 'delayed/0.5s', ready: false, allowList: [{ text: 'review_needed', reason: 'status id' }] });
  assert.equal(f.length, 2);
  assert.deepEqual(f.map((x) => [x.kind, x.text, x.ready, x.at]), [
    ['text', 'pause_badge_paused', false, 'delayed/0.5s'],
    ['aria-label', 'nav_sessions', false, 'delayed/0.5s']]);
  assert.match(formatFindings(f), /1\. \[delayed\/0\.5s, bundle not ready\] text "pause_badge_paused" \(pattern\) in span#headerPauseLabel/);
});

test('formatFindings caps the list and says how many more there are', () => {
  const many = Array.from({ length: 30 }, (_, i) => ({ at: 'x', ready: true, kind: 'text', text: `a_b_${i}`, reason: 'pattern', where: 'p' }));
  const out = formatFindings(many, 5).split('\n');
  assert.equal(out.length, 6);
  assert.match(out[5], /and 25 more/);
});

test('the in-page collector source is one self-contained expression', () => {
  assert.doesNotThrow(() => new Function(`return ${COLLECT_SRC}`));
  assert.match(COLLECT_SRC, /aria-label/);
  assert.match(COLLECT_SRC, /placeholder/);
  assert.match(COLLECT_SRC, /document\.title/);
  assert.match(COLLECT_SRC, /data-raw-key-ok/, 'the collector skips tagged zones');
});
