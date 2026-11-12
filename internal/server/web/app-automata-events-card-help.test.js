// Automata view — Events sub-tab moved-card help anchors.
//
// Every moved card (supervisor watch, recent decisions, infrastructure
// retries, Automata checks, safe-to-delete, cleanup status) appears in the
// event stream as its kind, and the spec requires one '?' help entry per
// moved card. This test checks that each card's row carries a help anchor
// wired to docs/howto/automata-events.md, that safe-to-delete and cleanup
// status (both kind cleanup) pick their own key from the event id, and that
// non-card rows (alerts, errors) carry no card help anchor.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function loadAppSandbox() {
  const sandbox = buildSandbox({ byId: {} });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

function event(kind, id) {
  return { id, time: '2026-11-10T03:00:00Z', kind, project: 'datawatch', summary: 's' };
}

test('each moved card kind maps to its own help key', () => {
  const sandbox = loadAppSandbox();
  const f = vm.runInContext('automataEventsCardHelpKey', sandbox);
  assert.equal(f(event('watch', 'watch:1')), 'automata_events_help_card_supervisor_watch');
  assert.equal(f(event('decision', 'decision:1')), 'automata_events_help_card_recent_decisions');
  assert.equal(f(event('retry', 'retry:1')), 'automata_events_help_card_infra_retries');
  assert.equal(f(event('check', 'check:1')), 'automata_events_help_card_automata_checks');
});

test('safe-to-delete and cleanup status share kind cleanup but own keys', () => {
  const sandbox = loadAppSandbox();
  const f = vm.runInContext('automataEventsCardHelpKey', sandbox);
  assert.equal(f(event('cleanup', 'cleanup:safe:main:1')), 'automata_events_help_card_safe_to_delete');
  assert.equal(f(event('cleanup', 'cleanup:held:main:1')), 'automata_events_help_card_safe_to_delete');
  assert.equal(f(event('cleanup', 'cleanup:status:1')), 'automata_events_help_card_cleanup_status');
});

test('non-card kinds carry no card help anchor', () => {
  const sandbox = loadAppSandbox();
  const f = vm.runInContext('automataEventsCardHelpKey', sandbox);
  assert.equal(f(event('alert', 'alert:1')), '');
  assert.equal(f(event('error', 'error:1')), '');
  assert.equal(f(event('', 'x:1')), '');
  assert.equal(f(null), '');
});

test('every moved card row renders a ? help anchor linking the events howto', () => {
  const sandbox = loadAppSandbox();
  const row = vm.runInContext('automataEventRowHtml', sandbox);
  const cases = [
    ['watch', 'watch:1'],
    ['decision', 'decision:1'],
    ['retry', 'retry:1'],
    ['check', 'check:1'],
    ['cleanup', 'cleanup:safe:main:1'],
    ['cleanup', 'cleanup:status:1'],
  ];
  for (const [kind, id] of cases) {
    const html = row(event(kind, id), false);
    assert.match(html, /href="\/diagrams\.html#docs\/howto\/automata-events\.md"/, `no help link for ${kind} ${id}`);
    assert.ok(html.indexOf('>?</a>') !== -1, `no ? anchor for ${kind} ${id}`);
  }
});

test('alert rows render without a moved-card help anchor', () => {
  const sandbox = loadAppSandbox();
  const html = vm.runInContext('automataEventRowHtml', sandbox)(event('alert', 'alert:1'), false);
  assert.doesNotMatch(html, /href="\/diagrams\.html#docs\/howto\/automata-events\.md"/);
});
