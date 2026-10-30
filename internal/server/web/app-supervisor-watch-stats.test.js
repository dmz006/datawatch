// Supervisor watch Monitor card — the Monitor tab shows a Supervisor watch
// card driven by the supervisor_watch_* fields of the stats snapshot. The card
// is left out until the daemon reports a mode, shows the mode, the unresolved
// findings, the shadow would-alert and alerted counts, the poll age and the
// days of full agreement, marks alert counts above zero as a warning, and its
// labels are in all five locale bundles.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-supervisor-watch-stats.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const KEYS = [
  'stats_supervisor_watch',
  'stats_supervisor_watch_mode',
  'stats_supervisor_watch_open',
  'stats_supervisor_watch_would_alert',
  'stats_supervisor_watch_alerted',
  'stats_supervisor_watch_poll_age',
  'stats_supervisor_watch_agreed_days',
];

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

test('the Supervisor watch card is left out until the daemon reports a mode', () => {
  assert.ok(!renderStats({}).includes('Supervisor watch'));
});

test('the Supervisor watch card shows the mode and every count', () => {
  const html = renderStats({
    supervisor_watch_mode: 'shadow', supervisor_watch_findings_open: 3,
    supervisor_watch_findings_would_alert: 1, supervisor_watch_findings_alerted: 0,
    supervisor_watch_last_poll_age_seconds: 42, supervisor_watch_agreed_days: 2,
  });
  assert.ok(html.includes('Supervisor watch'), 'card title missing');
  for (const [label, v] of [['Mode', 'shadow'], ['Open findings', 3], ['Would alert', 1], ['Alerted', 0],
    ['Last poll \\(s ago\\)', 42], ['Days of full agreement', 2]]) {
    assert.match(html, new RegExp(label + '</span><span[^>]*>' + v + '</span>'), label + ' row missing or wrong');
  }
});

test('alert counts above zero are warnings, zeros are not, and a never-polled age is a dash', () => {
  const some = renderStats({ supervisor_watch_mode: 'on', supervisor_watch_findings_alerted: 2, supervisor_watch_last_poll_age_seconds: -1 });
  assert.match(some, /Alerted<\/span><span style="color:var\(--warning\);">2</);
  assert.match(some, /Last poll \(s ago\)<\/span><span[^>]*>-</);
  const none = renderStats({ supervisor_watch_mode: 'shadow' });
  assert.match(none, /Alerted<\/span><span style="color:var\(--text\);">0</);
});

test('the card labels exist in all five locale bundles', () => {
  for (const lang of LANGS) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    for (const k of KEYS) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, lang + ' lacks ' + k);
    }
    assert.ok(!/PRD/.test(KEYS.map(k => b[k]).join(' ')), lang + ' card labels must not say PRD');
  }
});
