// Automata tabs after the Events stream took over the watch and decision rows:
//
//   - the supervisor watch card is gone from the Inbox sub-tab, and the Inbox
//     no longer starts its poll (its data comes through the Events stream, kind watch)
//   - the Recent decisions block is gone from the lane fill card on Resources
//     (its data comes through the Events stream, kind decision); the current
//     lanes part stays where it was and a visible gap sits where it was
//
//   node --test app-automata-events-moved-decisions.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const HOSTILE = '<img src=x onerror=alert(1)>';

function loadApp() {
  const sandbox = buildSandbox({ byId: {}, localStorage: new Map() });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  const en = JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, 'en.json'), 'utf8'));
  sandbox._i18n.bundle = en;
  sandbox._i18n.fallback = en;
  return sandbox;
}

function bodyOfSubtab(sandbox, subtab) {
  vm.runInContext(`_automationState.subtab = '${subtab}'`, sandbox);
  return vm.runInContext('_automationBodyHTML()', sandbox);
}

function laneStatus(over) {
  return {
    mode: 'on', last_poll_at: '2026-11-10T10:00:00Z',
    lanes: [{ id: 'gpu-1', type: 'gpu', state: 'idle', occupant: null, blocker: 'no work', eligible_waiting: 0 }],
    queued_eligible: { gpu: 0, claude: 0 },
    evidence: { checked_at: '2026-11-10T10:00:00Z', gpu_active_sessions: 0, loaded_models: [], error: '' },
    decisions: [{
      at: '2026-11-10T10:00:00Z', mode: 'on', lane_id: 'gpu-1', lane_type: 'gpu', action: 'start_prd',
      prd_id: 'p1', title: 'Cache column', reason: 'would start', blocker: '', executed: true, error: '', repeat_count: 1,
    }],
    alerts: [{ at: '2026-11-10T10:05:00Z', lane_id: 'claude-1', work: 'Cache column', message: 'idle with work waiting' }],
    ...over,
  };
}

test('the Inbox sub-tab no longer holds the supervisor watch card', () => {
  const sandbox = loadApp();
  const inbox = bodyOfSubtab(sandbox, 'inbox');
  assert.ok(inbox.includes('id="automationTriageCard"'), 'inbox body must still render');
  assert.ok(!inbox.includes('automationSupervisorWatchCard'), 'supervisor watch card must be gone from Inbox');
  assert.ok(!inbox.includes('supervisor-watch-slot'), 'supervisor watch slot class must be gone from Inbox');
});

test('the Inbox mount does not start the supervisor watch poll', () => {
  const sandbox = loadApp();
  const started = [];
  sandbox.setInterval = () => { started.push(true); return 1; };
  sandbox.apiFetch = () => new Promise(() => {});
  const inbox = vm.runInContext('_AUTOMATION_SUBTABS', sandbox).find(s => s.id === 'inbox');
  inbox.mount();
  assert.equal(sandbox.window._supervisorWatchInterval, undefined, 'no supervisor watch timer on Inbox');
});

test('the Resources sub-tab keeps the lane card with its current lanes', () => {
  const sandbox = loadApp();
  const resources = bodyOfSubtab(sandbox, 'resources');
  assert.ok(resources.includes('id="automationLaneFillCard"'), 'lane card must stay on Resources');
  const html = vm.runInContext('window.laneFillCardHtml(_status)', Object.assign(sandbox, { _status: laneStatus() }));
  assert.ok(html.includes('gpu-1'), 'current lanes must still render');
  assert.ok(html.includes('Idle lane alerts'), 'idle lane alerts must still render');
});

test('the lane card no longer renders the Recent decisions block or its rows', () => {
  const sandbox = loadApp();
  const html = vm.runInContext('window.laneFillCardHtml(_status)', Object.assign(sandbox, { _status: laneStatus() }));
  assert.ok(!html.includes('Recent decisions'), 'Recent decisions heading must be gone: ' + html);
  assert.ok(!html.includes('lane-fill-decision"'), 'decision rows must be gone');
  assert.ok(!html.includes('Cache column · '), 'decision row text must not render');
  assert.equal(vm.runInContext('typeof _laneFillDecisionHtml', sandbox), 'undefined', 'decision row renderer must be removed');
});

test('a visible gap sits between the evidence line and the idle lane alerts', () => {
  const sandbox = loadApp();
  const html = vm.runInContext('window.laneFillCardHtml(_status)', Object.assign(sandbox, { _status: laneStatus() }));
  const gap = html.indexOf('class="lane-fill-gap"');
  const lane = html.indexOf('gpu-1');
  const alerts = html.indexOf('Idle lane alerts');
  assert.ok(gap >= 0, 'gap element missing');
  assert.ok(gap > lane, 'gap must follow the current lanes part');
  assert.ok(gap < alerts, 'gap must sit before the alerts section');
  assert.match(html, /class="lane-fill-gap"[^>]*height:14px/, 'gap must have a visible height');
});

test('the gap holds with an error evidence line and no decisions, and hostile lane text stays escaped', () => {
  const sandbox = loadApp();
  const html = vm.runInContext('window.laneFillCardHtml(_status)', Object.assign(sandbox, {
    _status: laneStatus({
      evidence: { error: HOSTILE, gpu_active_sessions: 0, loaded_models: [] },
      decisions: [],
      lanes: [{ id: HOSTILE, type: 'gpu', state: 'idle', occupant: null, blocker: '', eligible_waiting: 0 }],
    }),
  }));
  assert.ok(html.includes('class="lane-fill-gap"'), 'gap must render with no decisions');
  assert.ok(!html.includes('<img'), 'raw markup leaked: ' + html);
});
