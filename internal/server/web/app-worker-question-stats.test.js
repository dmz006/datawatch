// Worker question marker stats — the Monitor tab shows a Worker Questions card
// driven by the worker_questions_* counters in the stats snapshot. The card is
// left out until a counter is above zero, shows all five counters, marks
// refusals as a warning, and its labels are in all five locale bundles.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-worker-question-stats.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const KEYS = [
  'stats_worker_questions',
  'stats_worker_questions_raised',
  'stats_worker_questions_duplicate',
  'stats_worker_questions_refused',
  'stats_worker_questions_parked',
  'stats_worker_questions_resumed',
];

function loadAppJS() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  sandbox._i18n.fallback = en;
  sandbox._i18n.bundle = en;
  sandbox._i18n.ready = true;
  return sandbox;
}

function renderStats(extra) {
  const sandbox = loadAppJS();
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

test('the Worker Questions card is left out while every counter is zero', () => {
  assert.ok(!renderStats({}).includes('Worker Questions'));
  assert.ok(!renderStats({ worker_questions_raised: 0 }).includes('Worker Questions'));
});

test('the Worker Questions card shows all five counters', () => {
  const html = renderStats({
    worker_questions_raised: 4, worker_questions_duplicate: 2, worker_questions_refused: 0,
    worker_questions_parked: 3, worker_questions_resumed: 1,
  });
  assert.ok(html.includes('Worker Questions'), 'card title missing');
  for (const [label, n] of [['Raised', 4], ['Repeated', 2], ['Refused', 0], ['Parked', 3], ['Resumed', 1]]) {
    assert.match(html, new RegExp(label + '</span><span[^>]*>' + n + '</span>'), label + ' row missing or wrong');
  }
});

test('a refusal count is shown in the warning colour, a zero is not', () => {
  const some = renderStats({ worker_questions_raised: 1, worker_questions_refused: 2 });
  assert.match(some, /Refused<\/span><span style="color:var\(--warning\);">2</);
  const none = renderStats({ worker_questions_raised: 1 });
  assert.match(none, /Refused<\/span><span style="color:var\(--text\);">0</);
});

test('the card labels exist in all five locale bundles', () => {
  for (const lang of LANGS) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    for (const k of KEYS) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, lang + ' lacks ' + k);
    }
  }
});
