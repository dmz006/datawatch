'use strict';
// app-timeline-card.test.js — node:test coverage for the new Automaton
// Timeline dashboard card + the per-Automaton "Timeline" detail tab in
// app.js:
//   - window.timelineCardHtml(state)      — pure (state, filters) → HTML
//   - window.loadTimelineCard(force)      — fetch/cache, single inflight
//   - window._timelineCardFilter(which, v)
//   - window.timelineGanttSvg(automaton)  — pure SVG render
//   - window._renderDetailTimelineTab(prd)
//   - locale key parity across en/de/es/fr/ja (existing + new keys)
//   - the card section must be timing-free (the dashboard rAF loop is the
//     only fetch driver); the detail tab is the one place a poll lives.
// No jsdom/jest: loads the real app.js into a stubbed browser (see
// testutil_browser_stub.js, BL394 §3h) and exercises the public surface.

const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { buildSandbox, loadScript, flushAsync, vm } = require('./testutil_browser_stub.js');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALE_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];

// Two projects, four active automata — enough to exercise every filter.
const FIXTURE = {
  today: '2026-11-09T12:00:00Z',
  projects: [
    {
      project: 'alpha',
      automata: [
        {
          id: 'prd-a', title: 'Ship the Gantt', project: 'alpha',
          kind: 'automaton', tags: ['ui'], status: 'running',
          start: '2026-11-08T09:00:00Z', end: null,
          projected_finish: '2026-11-10T09:00:00Z',
          stages: [
            { id: 'st-1', title: 'Build', status: 'running', start: '2026-11-08T09:00:00Z', end: null,
              members: [
                { id: 'm-1', title: 'Wire the card', status: 'done',
                  start: '2026-11-08T09:00:00Z', end: '2026-11-08T15:00:00Z', intervals: [] },
                { id: 'm-2', title: 'Draw the Gantt', status: 'running',
                  start: '2026-11-08T15:00:00Z', end: null,
                  intervals: [{ kind: 'blocked', start: '2026-11-09T06:00:00Z', end: '2026-11-09T09:00:00Z', reason: 'waiting on review' }] },
              ] },
            { id: 'st-2', title: 'Ship', status: 'pending', start: '2026-11-10T08:00:00Z', end: null,
              members: [{ id: 'm-3', title: 'Tag it', status: 'pending', start: '2026-11-10T08:00:00Z', end: null, intervals: [] }] },
          ],
          intervals: [],
        },
        {
          id: 'prd-b', title: 'Refresh nightly', project: 'alpha',
          kind: 'automation', tags: [], status: 'paused',
          start: '2026-11-07T00:00:00Z', end: null,
          stages: [
            { id: 'st-3', title: 'Poll', status: 'running', start: '2026-11-07T00:00:00Z', end: null,
              members: [{ id: 'm-4', title: 'Fetch loop', status: 'running', start: '2026-11-07T00:00:00Z', end: null,
                intervals: [{ kind: 'waiting', start: '2026-11-09T00:00:00Z', end: null, reason: 'upstream rate limit' }] }] },
          ],
          intervals: [],
        },
      ],
    },
    {
      project: 'beta',
      automata: [
        {
          id: 'prd-c', title: 'Migrate <db>', project: 'beta',
          kind: 'automaton', tags: ['core'], status: 'blocked',
          start: '2026-11-06T00:00:00Z', end: null,
          stages: [
            { id: 'st-4', title: 'Plan', status: 'done', start: '2026-11-06T00:00:00Z', end: '2026-11-06T12:00:00Z',
              members: [{ id: 'm-5', title: 'Scope', status: 'done', start: '2026-11-06T00:00:00Z', end: '2026-11-06T06:00:00Z', intervals: [] }] },
          ],
          intervals: [],
        },
        {
          id: 'prd-d', title: 'Lint sweep', project: 'beta',
          kind: 'automation', tags: ['ui'], status: 'planning',
          start: '2026-11-09T08:00:00Z', end: null,
          stages: [],
          intervals: [],
        },
      ],
    },
  ],
  counts: { projects: 2, automata: 4, stages: 5, members: 5, blocked: 1 },
};

function okState(over) {
  return Object.assign({ status: 'ok', data: FIXTURE, error: '', fetchedAt: 1, attemptedAt: 1, inflight: null,
    filters: { project: '', automation: '', stage: '', kind: '', tag: '' } }, over || {});
}

function sandboxWith(localeFile, sandboxOpts) {
  const sandbox = buildSandbox(sandboxOpts);
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  // app.js re-initialises _i18n on load, so the real locale bundle is set
  // AFTER loading. t() then resolves the timeline keys instead of falling
  // back to the raw key (which would hide a missing-key bug).
  const bundle = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, localeFile), 'utf8'));
  sandbox._i18n.bundle = bundle;
  return sandbox;
}

// ── render: happy path ──────────────────────────────────────────────────────

test('card renders the stacked automata, filter bar, and status', () => {
  const sandbox = sandboxWith('en.json');
  const html = vm.runInContext('window.timelineCardHtml(okState())', Object.assign(sandbox, { okState }));
  assert.equal(typeof html, 'string');
  assert.ok(html.includes('timeline-card'), 'card root');
  assert.ok(html.includes('data-timeline-status="ok"'), 'status attr');
  // Every automaton appears (plain titles), grouped by project.
  for (const id of ['prd-a', 'prd-b', 'prd-c', 'prd-d']) {
    assert.ok(html.includes('data-automaton-id="' + id + '"'), 'row for ' + id);
  }
  assert.ok(html.includes('data-project="alpha"') && html.includes('data-project="beta"'), 'project groups');
  // Filter bar: exactly the five documented selects.
  const selects = html.match(/class="timeline-card-filter"/g) || [];
  assert.equal(selects.length, 5, 'five filters');
  for (const which of ['project', 'automation', 'stage', 'kind', 'tag']) {
    assert.ok(html.includes('data-tl-filter="' + which + '"'), which + ' filter');
  }
  // Each select is named (not five bare "All" boxes).
  for (const label of ['Project', 'Automation', 'Stage', 'Kind', 'Tag']) {
    assert.ok(html.includes('aria-label="' + label + '"'), label + ' filter is labelled');
  }
  // Gantt SVG per automaton that has stages.
  assert.ok(html.includes('class="timeline-gantt"'), 'gantt svg');
  // Summary counts both projects (t() substitutes %1$d/%2$d).
  assert.ok(html.includes('4 active automata across 2 projects'), 'summary line');
});

test('card empty state when no automata match', () => {
  const sandbox = sandboxWith('en.json');
  const empty = { today: '2026-11-09T12:00:00Z', projects: [], counts: { projects: 0, automata: 0, stages: 0, members: 0, blocked: 0 } };
  const st = okState({ data: empty });
  const html = vm.runInContext('window.timelineCardHtml(st)', Object.assign(sandbox, { st }));
  assert.ok(html.includes('No active automata to chart right now.'), 'empty message');
  assert.ok(!html.includes('data-automaton-id="'), 'no rows');
});

test('card failed state shows the error message', () => {
  const sandbox = sandboxWith('en.json');
  const st = { status: 'failed', data: null, error: 'boom', fetchedAt: 0, attemptedAt: 1, inflight: null,
    filters: { project: '', automation: '', stage: '', kind: '', tag: '' } };
  const html = vm.runInContext('window.timelineCardHtml(st)', Object.assign(sandbox, { st }));
  assert.ok(html.includes('Failed to load the automaton timeline.'), 'error message');
  assert.ok(html.includes('boom'), 'underlying error surfaced');
  assert.ok(html.includes('data-timeline-status="failed"'), 'failed status attr');
});

// ── filters: each select narrows the stack ─────────────────────────────────

function filteredHtml(localeFile, filters) {
  const sandbox = sandboxWith(localeFile);
  const st = okState({ filters: Object.assign({ project: '', automation: '', stage: '', kind: '', tag: '' }, filters) });
  return vm.runInContext('window.timelineCardHtml(st)', Object.assign(sandbox, { st }));
}

test('project filter keeps only that project', () => {
  const html = filteredHtml('en.json', { project: 'beta' });
  assert.ok(html.includes('data-automaton-id="prd-c"') && html.includes('data-automaton-id="prd-d"'), 'beta rows kept');
  assert.ok(!html.includes('data-automaton-id="prd-a"'), 'alpha row hidden');
});

test('automation filter keeps only the named automaton', () => {
  const html = filteredHtml('en.json', { automation: 'Ship the Gantt' });
  assert.ok(html.includes('data-automaton-id="prd-a"'), 'named automaton kept');
  assert.ok(!html.includes('data-automaton-id="prd-b"'), 'others hidden');
});

test('stage filter keeps only automata that have that stage', () => {
  const html = filteredHtml('en.json', { stage: 'Poll' });
  assert.ok(html.includes('data-automaton-id="prd-b"'), 'stage owner kept');
  assert.ok(!html.includes('data-automaton-id="prd-a"'), 'non-owner hidden');
  assert.ok(!html.includes('data-automaton-id="prd-d"'), 'stage-less automaton hidden');
});

test('kind filter keeps only that kind', () => {
  const html = filteredHtml('en.json', { kind: 'automation' });
  assert.ok(html.includes('data-automaton-id="prd-b"') && html.includes('data-automaton-id="prd-d"'), 'automations kept');
  assert.ok(!html.includes('data-automaton-id="prd-a"'), 'automaton hidden');
  assert.ok(!html.includes('data-automaton-id="prd-c"'), 'automaton hidden');
});

test('tag filter keeps only automata tagged with it', () => {
  const html = filteredHtml('en.json', { tag: 'ui' });
  assert.ok(html.includes('data-automaton-id="prd-a"') && html.includes('data-automaton-id="prd-d"'), 'tagged kept');
  assert.ok(!html.includes('data-automaton-id="prd-b"'), 'untagged hidden');
  assert.ok(!html.includes('data-automaton-id="prd-c"'), 'other tag hidden');
});

// ── loader: fetch/cache/single-inflight, URL, error state ──────────────────

test('loadTimelineCard fetches once, caches, and honors filters in the URL', async () => {
  const sandbox = sandboxWith('en.json');
  const calls = [];
  sandbox.fetch = (url) => {
    calls.push(String(url));
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(FIXTURE) });
  };
  await vm.runInContext('window.loadTimelineCard(false)', sandbox);
  await flushAsync();
  assert.equal(calls.length, 1, 'one fetch');
  assert.equal(calls[0], '/api/autonomous/timeline', 'plain URL with no filters');
  const st = sandbox.window._timelineCardState;
  assert.equal(st.status, 'ok');
  assert.equal(st.data, FIXTURE, 'data cached');
  assert.equal(st.inflight, null, 'inflight cleared');

  // A second immediate call is served from the cache (no new fetch)…
  await vm.runInContext('window.loadTimelineCard(false)', sandbox);
  await flushAsync();
  assert.equal(calls.length, 1, 'cached — no second fetch');

  // …but a filter change forces a fetch with the query string.
  vm.runInContext('window._timelineCardFilter("kind", "automation")', sandbox);
  await flushAsync();
  assert.equal(calls.length, 2, 'filter change refetches');
  assert.equal(calls[1], '/api/autonomous/timeline?kind=automation', 'filter in URL');
});

test('loadTimelineCard failure → failed state with the message', async () => {
  const sandbox = sandboxWith('en.json');
  sandbox.fetch = () => Promise.reject(new Error('nope'));
  await vm.runInContext('window.loadTimelineCard(true)', sandbox);
  await flushAsync();
  const st = sandbox.window._timelineCardState;
  assert.equal(st.status, 'failed');
  assert.equal(st.error, 'nope');
  assert.equal(st.inflight, null, 'inflight cleared on failure');
});

// ── Gantt render: markers, hatching, plain titles, escaping ────────────────

function ganttFor(localeFile, automaton) {
  const sandbox = sandboxWith(localeFile);
  // `today` is injected (the server's clock in the response), never Date.now().
  return vm.runInContext('window.timelineGanttSvg(a, { uid: "x", today: T })',
    Object.assign(sandbox, { a: automaton, T: FIXTURE.today }));
}

test('Gantt has today + projected markers, hatched blocked, and labels', () => {
  const a = FIXTURE.projects[0].automata[0];
  const svg = ganttFor('en.json', a);
  assert.ok(svg.startsWith('<svg'), 'svg root');
  assert.ok(svg.includes('class="timeline-gantt"'), 'gantt class');
  assert.ok(svg.includes('class="timeline-today"'), 'today marker');
  assert.ok(svg.includes('class="timeline-projected"'), 'projected-finish marker');
  assert.ok(svg.includes('data-timeline-interval="blocked"'), 'blocked hatched span');
  assert.ok(svg.includes('pattern id="tlh-x-blocked"'), 'blocked hatch pattern');
  assert.ok(svg.includes('Ship the Gantt'), 'plain title, no ids in labels');
  assert.ok(svg.includes('Draw the Gantt'), 'member label');
  assert.ok(!svg.includes('prd-a') && !svg.includes('st-1') && !svg.includes('m-2'), 'raw ids not shown');
});

test('Gantt waiting interval gets the waiting hatch', () => {
  const a = FIXTURE.projects[0].automata[1];
  const svg = ganttFor('en.json', a);
  assert.ok(svg.includes('data-timeline-interval="waiting"'), 'waiting span');
  assert.ok(svg.includes('pattern id="tlh-x-waiting"'), 'waiting hatch pattern');
});

test('Gantt with no stages degrades to a plain status line', () => {
  const out = ganttFor('en.json', { id: 'x', title: 'T', stages: [] });
  assert.ok(out.includes('timeline-no-stages'), 'no-stages class');
  assert.ok(!out.startsWith('<svg'), 'no svg');
});

test('user data is escaped in labels, intervals, and tooltips', () => {
  const a = {
    id: 'e1', title: 'A"onmouseover="x', stages: [{
      id: 's", onload="y', title: 'S", onload="y', status: 'running', start: '2026-11-08T09:00:00Z', end: null,
      members: [{
        id: 'm", onload="z', title: 'M", onload="z', status: 'running', start: '2026-11-08T09:00:00Z', end: null,
        intervals: [{ kind: 'blocked', start: '2026-11-08T10:00:00Z', end: '2026-11-08T11:00:00Z',
          reason: 'evil"><script>window.__pwned=1</script>' }],
      }],
    }],
  };
  const sandbox = sandboxWith('en.json');
  const svg = vm.runInContext('window.timelineGanttSvg(a, { uid: "esc" })', Object.assign(sandbox, { a }));
  assert.ok(!svg.includes('<script>'), 'no raw script tag');
  assert.ok(!svg.includes('onmouseover="x'), 'no raw injected attribute');
  assert.ok(svg.includes('&quot;') || svg.includes('&amp;'), 'escaped entity present');
  assert.ok(!sandbox.window.__pwned, 'no script executed');
});

// ── detail tab ──────────────────────────────────────────────────────────────

test('detail Timeline tab renders into #timelineTabBody via the shared renderers', async () => {
  // Pre-register the body element: the stub otherwise hands out a fresh
  // throwaway element per getElementById call, so nothing could be read back.
  const bodyEl = { innerHTML: '', textContent: '' };
  const sandbox = sandboxWith('en.json', { byId: { timelineTabBody: bodyEl } });
  sandbox.fetch = () => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(FIXTURE) });
  vm.runInContext('window._loadPRDTimelineTab({ id: "prd-a" })', sandbox);
  await flushAsync();
  const body = bodyEl.innerHTML;
  assert.ok(body.includes('class="timeline-gantt"'), 'gantt rendered');
  assert.ok(body.includes('Ship the Gantt'), 'automaton title');
  // The tab HTML itself carries the tab marker + body anchor.
  const tab = vm.runInContext('window._renderDetailTimelineTab({ id: "prd-a" })', sandbox);
  assert.ok(tab.includes('data-timeline-tab="1"'), 'tab marker');
  assert.ok(tab.includes('id="timelineTabBody"'), 'body anchor');
});

// ── locale parity: every key in en exists (non-empty) in de/es/fr/ja ───────

const NEW_KEYS = [
  'prd_tab_timeline',
  'timeline_card_title', 'timeline_card_loading', 'timeline_card_error',
  'timeline_card_empty', 'timeline_card_stale', 'timeline_card_summary',
  'timeline_card_filter_project', 'timeline_card_filter_automation',
  'timeline_card_filter_stage', 'timeline_card_filter_kind',
  'timeline_card_filter_tag', 'timeline_card_kind_all',
  'timeline_card_kind_automaton', 'timeline_card_kind_automation',
  'timeline_card_status_planning', 'timeline_card_status_decomposing',
  'timeline_card_status_running', 'timeline_card_status_paused',
  'timeline_card_status_blocked',
  'timeline_gantt_title', 'timeline_gantt_no_stages', 'timeline_gantt_today',
  'timeline_gantt_projected', 'timeline_gantt_blocked', 'timeline_gantt_waiting',
];
const OLD_KEYS = ['timeline_empty', 'timeline_error', 'timeline_loading', 'timeline_title'];

test('new + old timeline keys exist in all five locales', () => {
  const bundles = {};
  for (const lang of LANGS) bundles[lang] = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, lang + '.json'), 'utf8'));
  for (const key of [...NEW_KEYS, ...OLD_KEYS]) {
    for (const lang of LANGS) {
      assert.equal(typeof bundles[lang][key], 'string', `${lang}.json has ${key}`);
      assert.ok(bundles[lang][key].length > 0, `${lang}.json ${key} non-empty`);
    }
  }
});

test('translated values are not English (de/es/fr/ja differ from en)', () => {
  const bundles = {};
  for (const lang of LANGS) bundles[lang] = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, lang + '.json'), 'utf8'));
  for (const key of ['timeline_card_empty', 'timeline_gantt_today', 'timeline_gantt_no_stages']) {
    for (const lang of ['de', 'es', 'fr', 'ja']) {
      assert.notEqual(bundles[lang][key], bundles.en[key], `${lang} ${key} translated`);
    }
  }
});

// ── structural: card section is timing-free; the tab is the only poller ───

test('card implementation section owns no timers; the detail tab is the sole poller', () => {
  const app = fs.readFileSync(APP_JS, 'utf8');
  const s = app.indexOf('// ── Automaton Timeline card (Gantt)');
  const e = app.indexOf('// ── Timeline tab (Automaton detail view)');
  const p = app.indexOf('// ── Plan-coverage gate (PRD view)');
  assert.ok(s > -1 && e > s, 'markers found in order');
  const cardSection = app.slice(s, e);
  assert.ok(!cardSection.includes('setInterval'), 'card section: no setInterval');
  assert.ok(!cardSection.includes('setTimeout'), 'card section: no setTimeout');
  const tabSection = app.slice(e, p > -1 ? p : app.length);
  assert.ok(tabSection.includes('setInterval'), 'detail tab owns its poll');
  // The dashboard loop drives the card render.
  assert.ok(app.includes('_dashRenderTimeline()'), 'loop hook wired');
});

test('card filters are event-attribute driven, never inline user data (BL394)', () => {
  const sandbox = sandboxWith('en.json');
  const html = vm.runInContext('window.timelineCardHtml(okState())', Object.assign(sandbox, { okState }));
  assert.ok(html.includes('onchange="window._timelineCardFilter(this.dataset.tlFilter, this.value)"'),
    'filter selects dispatch via data attribute + named function');
  // No inline handler carries an automaton/project/stage title.
  for (const title of ['Ship the Gantt', 'Refresh nightly', 'Poll']) {
    assert.ok(!new RegExp('onclick="[^"]*' + title.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '[^"]*"').test(html),
      'no inline onclick with ' + title);
  }
});

test('user data lands escaped, never raw', () => {
  const sandbox = sandboxWith('en.json');
  const html = vm.runInContext('window.timelineCardHtml(okState())', Object.assign(sandbox, { okState }));
  assert.ok(html.includes('Migrate &lt;db&gt;'), 'title escaped in card');
});
