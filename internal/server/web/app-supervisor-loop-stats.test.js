// Supervisor loop Monitor card — the Monitor tab shows a Supervisor loop card
// driven by the supervisor_loop_* fields of the stats snapshot. The card is
// left out until the daemon reports the loop's jobs, shows the state, jobs
// enabled, runs by outcome, retries and last-run age, marks escalated and
// error counts above zero as a warning, and its labels are in all five locale
// bundles.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-supervisor-loop-stats.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const KEYS = ['stats_supervisor_loop', 'stats_supervisor_loop_state', 'stats_supervisor_loop_on', 'stats_supervisor_loop_off',
  'stats_supervisor_loop_jobs', 'stats_supervisor_loop_runs', 'stats_supervisor_loop_ok', 'stats_supervisor_loop_validator_failed',
  'stats_supervisor_loop_escalated', 'stats_supervisor_loop_error', 'stats_supervisor_loop_retries', 'stats_supervisor_loop_last_run'];

function renderStats(extra) {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  sandbox._i18n.fallback = en;
  sandbox._i18n.bundle = en;
  sandbox._i18n.ready = true;
  const el = makeStubElement();
  el.closest = () => null;
  sandbox.scrollTo = () => {};
  sandbox.__el = el;
  sandbox.__data = Object.assign({
    timestamp: Date.now(), cpu_load_avg_1: 0.1, cpu_cores: 4,
    mem_used: 1, mem_total: 2, disk_used: 1, disk_total: 2,
    daemon_rss_bytes: 1, goroutines: 1, open_fds: 1, uptime_seconds: 1,
  }, extra);
  vm.runInContext('renderStatsData(__el, __data)', sandbox);
  return el.innerHTML;
}

test('the Supervisor loop card is left out until the daemon reports jobs', () => {
  assert.ok(!renderStats({}).includes('Supervisor loop'));
});

test('the Supervisor loop card shows the state and every count', () => {
  const html = renderStats({
    supervisor_loop_enabled: true, supervisor_loop_jobs: 11, supervisor_loop_jobs_enabled: 10,
    supervisor_loop_runs: 4, supervisor_loop_runs_ok: 2, supervisor_loop_runs_validator_failed: 1,
    supervisor_loop_runs_escalated: 1, supervisor_loop_runs_error: 0, supervisor_loop_retries: 3,
    supervisor_loop_last_run_age_seconds: 42,
  });
  assert.ok(html.includes('data-stat="supervisor-loop"'), 'card missing');
  for (const [label, v] of [['State', 'On'], ['Jobs enabled', '10 / 11'], ['Runs stored', 4], ['Ok', 2],
    ['Validator failed', 1], ['Escalated', 1], ['Error', 0], ['Retries used', 3], ['Last run \\(s ago\\)', 42]]) {
    assert.match(html, new RegExp(label + '</span><span[^>]*>' + v + '</span>'), label + ' row missing or wrong');
  }
});

test('escalated and error counts above zero are warnings, and a never-run age is a dash', () => {
  const some = renderStats({ supervisor_loop_jobs: 11, supervisor_loop_runs_error: 2, supervisor_loop_last_run_age_seconds: -1 });
  assert.match(some, /Error<\/span><span style="color:var\(--warning\);">2</);
  assert.match(some, /Last run \(s ago\)<\/span><span[^>]*>-</);
  assert.match(some, /State<\/span><span[^>]*>Off</);
  const none = renderStats({ supervisor_loop_jobs: 11 });
  assert.match(none, /Escalated<\/span><span style="color:var\(--text\);">0</);
});

test('the card labels exist in all five locale bundles', () => {
  for (const lang of LANGS) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    for (const k of KEYS) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, lang + ' lacks ' + k);
    }
  }
});
