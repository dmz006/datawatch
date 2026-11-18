// Model request progress view — PWA side.
//
// window.requestWatchCardHtml(status) turns the decoded GET
// /api/autonomous/request-watch body into the Automation Overview card, and
// window.requestWatchChipHtml(model_request) is the chip on an Automaton task
// row, on a session list row and on the session detail header. The chip words
// generating, the minutes without progress (one minute, under a minute) and an
// unreachable node, and uses the warning style past the threshold. The card
// polls through its list loader; no timer is stored on a DOM element.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const HOSTILE = '<img src=x onerror=alert(1)>';

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
}

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  return sandbox;
}

function card(status) {
  const sandbox = loadApp();
  sandbox._status = status;
  return vm.runInContext('window.requestWatchCardHtml(_status)', sandbox);
}

function chip(obs) {
  const sandbox = loadApp();
  sandbox._obs = obs;
  return vm.runInContext('window.requestWatchChipHtml(_obs)', sandbox);
}

function obs(over) {
  return {
    session_id: 'ab12', automaton_id: 'a1', task_id: 't1', role: 'worker', node: 'gpu-box', model: 'big-model',
    state: 'generating', last_change: '2026-10-31T10:00:00Z', no_progress_seconds: 0,
    last_poll: '2026-10-31T10:00:30Z', over_threshold: false,
    summary: 'waiting on a model request: generating', ...over,
  };
}

function status(list) {
  return { enabled: true, poll_seconds: 30, no_progress_minutes: 30, observations: list };
}

function stripAll(s, re) {
  let prev;
  do {
    prev = s;
    s = s.replace(re, '');
  } while (s !== prev);
  return s;
}

function text(html) {
  return stripAll(stripAll(html, /<[^>]*>/g), /&#\d+;|&#x[0-9a-f]+;/gi).trim();
}

// ── the card ─────────────────────────────────────────────────────────────────

test('the card lists a generating, an idle and an unreachable observation', () => {
  const html = card(status([
    obs({ session_id: 'gen1' }),
    obs({ session_id: 'idle1', role: 'verifier', task_id: 't2', state: 'idle', no_progress_seconds: 600 }),
    obs({ session_id: 'down1', state: 'unreachable', node: 'far-node', no_progress_seconds: 900, error: 'connection refused' }),
  ]));
  assert.ok(html.includes('Automata model request progress'), 'title missing: ' + html);
  assert.equal((html.match(/class="request-watch-row"/g) || []).length, 3, 'one row per observation');
  assert.ok(html.includes('Session gen1') && html.includes('Session idle1') && html.includes('Session down1'));
  assert.ok(html.includes('Worker') && html.includes('Verifier'), 'role missing');
  assert.ok(html.includes('Task t2'), 'task missing');
  assert.ok(html.includes('Node far-node'), 'node missing');
  assert.ok(html.includes('Waiting on a model request: generating'));
  assert.ok(html.includes('Waiting on a model request: no progress for 10 minutes'));
  assert.ok(html.includes('Waiting on a model request: node unreachable'));
  assert.ok(html.includes('Error: connection refused'), 'error line missing');
});

test('the card shows the empty state for no observations, a missing list or a malformed body', () => {
  for (const body of [status([]), { enabled: true }, null, undefined, 'x', []]) {
    const html = card(body);
    assert.ok(html.includes('No Automaton worker or verifier is waiting on an observed model request.'), 'empty text missing for ' + JSON.stringify(body));
    assert.ok(!html.includes('request-watch-row'), 'no rows for an empty list');
  }
});

test('the card has the help link to the definitions section, which follows the docs-link preference', () => {
  const html = card(status([]));
  assert.ok(html.includes('datawatch-definitions.md#model-request-progress'), 'help href missing: ' + html);
  assert.ok(html.includes('About model request progress'), 'help label missing');
  const sandbox = loadApp();
  sandbox.localStorage.getItem = k => (k === 'cs_show_docs_links' ? '0' : null);
  assert.ok(!vm.runInContext('window.requestWatchCardHtml({observations: []})', sandbox).includes('request-watch-help'), 'the link must hide with the preference');
});

test('the card escapes every value and caps a long list', () => {
  const html = card(status([obs({ session_id: HOSTILE, node: HOSTILE, task_id: HOSTILE, error: HOSTILE, model: HOSTILE })]));
  assert.ok(!html.includes(HOSTILE), 'hostile markup reached the page: ' + html);
  const many = Array.from({ length: 60 }, (_, i) => obs({ session_id: 's' + i }));
  const big = card(status(many));
  assert.equal((big.match(/class="request-watch-row"/g) || []).length, 50);
  assert.ok(big.includes('10 more not shown'), 'overflow note missing');
});

test('the card never says PRD and names no tracker ID', () => {
  const html = text(card(status([obs()])));
  assert.ok(!/\bPRDs?\b/.test(html));
  assert.ok(!/\b(BL|GH|B)\d+\b/.test(html));
});

// ── the chip ─────────────────────────────────────────────────────────────────

test('the chip words generating', () => {
  const html = chip(obs());
  assert.equal(text(html), 'Waiting on a model request: generating');
  assert.ok(html.includes('request-watch-chip'));
  assert.ok(!html.includes('prd-verdict-warn'), 'a request under the threshold is not a warning');
});

test('the chip words the minutes without progress, with the singular and under a minute', () => {
  const idle = secs => text(chip(obs({ state: 'idle', no_progress_seconds: secs })));
  assert.equal(idle(0), 'Waiting on a model request: no progress for under a minute');
  assert.equal(idle(59), 'Waiting on a model request: no progress for under a minute');
  assert.equal(idle(60), 'Waiting on a model request: no progress for 1 minute');
  assert.equal(idle(119), 'Waiting on a model request: no progress for 1 minute');
  assert.equal(idle(120), 'Waiting on a model request: no progress for 2 minutes');
  assert.equal(idle(1800), 'Waiting on a model request: no progress for 30 minutes');
  assert.equal(idle(undefined), 'Waiting on a model request: no progress for under a minute');
});

test('the chip words an unreachable node', () => {
  assert.equal(text(chip(obs({ state: 'unreachable', no_progress_seconds: 3000 }))), 'Waiting on a model request: node unreachable');
});

test('the chip before the first sample reads the plain waiting line', () => {
  assert.equal(text(chip(obs({ state: 'unknown' }))), 'Waiting on a model request');
});

test('a chip past the threshold uses the warning style', () => {
  const html = chip(obs({ state: 'idle', no_progress_seconds: 2400, over_threshold: true }));
  assert.ok(html.includes('prd-verdict-warn'), 'warning style missing: ' + html);
  assert.ok(html.includes('&#9888;'), 'warning mark missing');
  assert.ok(chip(obs({ state: 'unreachable', over_threshold: true })).includes('prd-verdict-warn'));
});

test('the chip tooltip names the node and the model and is escaped', () => {
  const html = chip(obs());
  assert.ok(html.includes('title="Model request on gpu-box · big-model"'), 'tooltip missing: ' + html);
  const hostile = chip(obs({ node: HOSTILE, model: HOSTILE }));
  assert.ok(!hostile.includes(HOSTILE), 'hostile markup reached the chip');
});

test('an absent or malformed model_request shows nothing', () => {
  for (const v of [undefined, null, '', 0, 'x', [], false]) {
    assert.equal(chip(v), '', 'expected no chip for ' + JSON.stringify(v));
  }
});

// ── the task row, the session row and the session header ────────────────────

function taskHtml(task) {
  const sandbox = loadApp();
  sandbox._p = {
    id: 'a1b2c3d4', title: 'Release train', status: 'running', project_dir: '.',
    story: [{ id: 'S1', title: 'Story', tasks: [task] }],
  };
  return vm.runInContext('renderTask(_p, _p.story[0], _p.story[0].tasks[0], false)', sandbox);
}

function task(over) {
  return { id: 't1', title: 'Add the index', status: 'verifying', ...over };
}

test('the task row shows the chip for each state', () => {
  const gen = taskHtml(task({ model_request: obs() }));
  assert.ok(gen.includes('Waiting on a model request: generating'), 'generating chip missing');
  const idle = taskHtml(task({ model_request: obs({ state: 'idle', no_progress_seconds: 300 }) }));
  assert.ok(idle.includes('Waiting on a model request: no progress for 5 minutes'));
  const one = taskHtml(task({ model_request: obs({ state: 'idle', no_progress_seconds: 61 }) }));
  assert.ok(one.includes('no progress for 1 minute<'), 'singular missing');
  const under = taskHtml(task({ model_request: obs({ state: 'idle', no_progress_seconds: 10 }) }));
  assert.ok(under.includes('no progress for under a minute'));
  const down = taskHtml(task({ model_request: obs({ state: 'unreachable' }) }));
  assert.ok(down.includes('Waiting on a model request: node unreachable'));
});

test('the task row chip uses the warning style past the threshold and replaces the bare waiting badge', () => {
  const warn = taskHtml(task({ model_request: obs({ state: 'idle', no_progress_seconds: 2400, over_threshold: true }) }));
  assert.ok(/request-watch-chip[^>]*>/.test(warn) && /prd-verdict-warn request-watch-chip/.test(warn), 'warning chip missing');
  const waiting = taskHtml(task({ status: 'waiting_capacity', model_request: obs() }));
  assert.ok(!waiting.includes('prd-task-wait-badge'), 'the bare waiting badge must give way to the chip');
  const plain = taskHtml(task({ status: 'waiting_capacity' }));
  assert.ok(plain.includes('prd-task-wait-badge'), 'a task without a request keeps the waiting badge');
});

test('a task without model_request shows no chip', () => {
  assert.ok(!taskHtml(task()).includes('request-watch-chip'));
});

function sess(extra) {
  return Object.assign({ id: 'ab12', full_id: 'host-ab12', state: 'running', task: 'build it', updated_at: '2026-10-31T12:00:00Z' }, extra || {});
}

test('the session list row shows the chip only when model_request is present', () => {
  const sandbox = loadApp();
  const withChip = sandbox.sessionCard(sess({ model_request: obs({ state: 'idle', no_progress_seconds: 180 }) }), 0, 1);
  assert.ok(withChip.includes('Waiting on a model request: no progress for 3 minutes'), 'session chip missing');
  const warn = sandbox.sessionCard(sess({ model_request: obs({ state: 'idle', no_progress_seconds: 3000, over_threshold: true }) }), 0, 1);
  assert.ok(warn.includes('prd-verdict-warn request-watch-chip'), 'session warning chip missing');
  assert.ok(!sandbox.sessionCard(sess(), 0, 1).includes('request-watch-chip'), 'no model_request, no chip');
});

test('the session detail header carries the chip', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.ok(src.includes('window.requestWatchChipHtml(sess && sess.model_request)'), 'detail header must render the chip from session.model_request');
});

// ── the loader and the timers ───────────────────────────────────────────────

test('the Events tab carries the slot and the loader reads the route', () => {
  const sandbox = loadApp();
  const body = vm.runInContext('automataEventsStatusHtml()', sandbox);
  assert.ok(body.includes('id="automationRequestWatchCard"'), 'slot missing from the Events status area');
  for (const other of ['inbox', 'automations', 'resources', 'progress', 'housekeeping']) {
    const o = vm.runInContext(`_AUTOMATION_SUBTABS.find(s => s.id === '${other}').bodyHTML()`, sandbox);
    assert.ok(!o.includes('id="automationRequestWatchCard"'), 'no request watch slot on ' + other);
  }
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.ok(src.includes("url: '/api/autonomous/request-watch'"), 'loader must read the route');
  assert.ok(/'automation_infra_retry', 'automation_request_watch'/.test(src), 'stop lists must include the loader');
  assert.ok(src.includes("'automation_release_readiness', 'automation_request_watch']"), 'the Events stop must end the loader');
  assert.ok(src.includes('_automationReleaseReadinessLoader(), _automationRequestWatchLoader()]'), 'the Events mount must start the loader');
});

test('mounting the loader twice leaves one loader, and stop ends its timers', () => {
  const sandbox = loadApp();
  const first = vm.runInContext('_automationRequestWatchLoader()', sandbox);
  const second = vm.runInContext('_automationRequestWatchLoader()', sandbox);
  assert.equal(first, second, 'a second mount must reuse the loader, not stack another');
  first.stop();
  assert.equal(first.state.pollTimer, null);
  assert.equal('tickTimer' in first.state, false, 'no tick timer exists on the loader state');
});

test('no interval is stored on a DOM element', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.ok(!/getElementById\([^)]*\)\.[A-Za-z_]*Interval\s*=/.test(src), 'an interval stored on an element is lost on re-render and stacks');
  const start = src.indexOf('// ── Automata model request progress');
  const end = src.indexOf('// ── Automata progress card');
  assert.ok(start > 0 && end > start, 'section markers missing');
  const section = src.slice(start, end);
  assert.ok(!/setInterval/.test(section) || /window\._requestWatchInterval/.test(section), 'a polled interval must live on window');
  assert.ok(!/\.\s*_?[A-Za-z]*Interval\s*=/.test(section.replace(/window\._requestWatchInterval/g, '')), 'no interval assignment on an element');
});

// ── locales ──────────────────────────────────────────────────────────────────

test('every request_watch_ key used by the view is in all five bundles, with no PRD and no tracker ID', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const keys = new Set(['request_watch_card_title', 'request_watch_generating', 'request_watch_no_progress', 'request_watch_unreachable', 'request_watch_empty', 'request_watch_help_link']);
  for (const m of src.matchAll(/'(request_watch_[a-z_]+)'/g)) keys.add(m[1]);
  for (const k of Object.values({ w: 'request_watch_role_worker', v: 'request_watch_role_verifier' })) keys.add(k);
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of keys) {
      assert.ok(typeof b[k] === 'string' && b[k] !== '', lang + ' bundle is missing ' + k);
      assert.ok(!/\bPRDs?\b/.test(b[k]), lang + ' ' + k + ' says PRD');
      assert.ok(!/\b(BL|GH|B)\d+\b/.test(b[k]), lang + ' ' + k + ' names a tracker ID');
    }
  }
  const en = bundle('en');
  assert.equal(en.request_watch_card_title, 'Automata model request progress');
  assert.equal(en.request_watch_generating, 'Waiting on a model request: generating');
});
