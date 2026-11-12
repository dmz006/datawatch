// Usage calculator parity — PWA side.
//
// The three variants (pace gauge, lane planner, burn ledger) are layouts over one response body.
// A reading must read the same in all of them: the same numbers, the same verdict wording, the
// same policy ids and labels and the same zero-lanes rule. These tests feed ONE fixed body to
// every renderer in window.usageCalcRenderers and compare what each one displays, then check
// those values against the body formatted by the page's own formatter. A variant is drawn the
// way the page draws it: the renderer's body together with the verdict-card and input-note hooks
// it registers, inside the shared frame, because the lane planner shows the reading fields in the
// verdict card and the others in their body.
//
// No network call and no wall clock: the frame gets a fixed time and the bodies are hand written.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');
const { workedExampleBody, merge } = require('./usage-calculator-fixture');

const EN = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
const FIXED_NOW = Date.UTC(2026, 9, 23, 22, 8);
const VARIANTS = ['pace-gauge', 'lane-planner', 'burn-ledger'];
const POLICIES = ['use-all', 'pace', 'finish-early'];
const POLICY_LABELS = { 'use-all': 'Run until used up', 'pace': 'Pace to the week', 'finish-early': 'Run flat out' };

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(JSON.stringify(EN));
  return sandbox;
}

// call runs a function of the page with plain arguments.
function call(sandbox, fn, ...args) {
  sandbox._args = args;
  return vm.runInContext(`${fn}(..._args)`, sandbox);
}

// view draws one variant as the page does: the shared frame around that variant's renderer.
function view(sandbox, variant, body) {
  return call(sandbox, 'window.usageCalcFrameHtml', body, { variant, now: FIXED_NOW });
}

// textOf strips the markup from a fragment, decodes the entities escHtml writes and collapses white space.
function textOf(fragment) {
  let out = fragment;
  for (let prev; out !== prev; prev = out) out = out.replace(/<[^>]+>/g, ' ');
  return out
    .replace(/&#39;/g, "'").replace(/&quot;/g, '"').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&')
    .replace(/\s+/g, ' ').trim();
}

// fieldRows reads the reading fields in the order they are drawn: one entry for each data-field row.
function fieldRows(html) {
  const rows = [];
  for (const m of html.matchAll(/<div data-field="(\d+)"[^>]*>([\s\S]*?)<\/div>/g)) rows[Number(m[1])] = textOf(m[2]);
  return rows;
}

// verdictCard reads the chips and the headline of the verdict card.
function verdictCard(html) {
  const chips = /<div class="usage-calc-chips"[^>]*>([\s\S]*?)<\/div>/.exec(html);
  const headline = /<div class="usage-calc-headline"[^>]*>([\s\S]*?)<\/div>/.exec(html);
  return { chips: chips ? textOf(chips[1]) : null, headline: headline ? textOf(headline[1]) : null };
}

// chipsIn lists every verdict chip anywhere on the page: its class and its label.
function chipsIn(html) {
  return [...html.matchAll(/<span class="usage-calc-chip (over|within)"[^>]*>([^<]*)<\/span>/g)].map(m => ({ cls: m[1], label: m[2] }));
}

// expectedRows is the body formatted by the page's own formatters and locale strings, in the
// shared field order of the design: reading, days since the reset, pace line and headroom, burns,
// projected ends, run out, verdicts, lanes and policy, advice.
function expectedRows(sandbox, body) {
  const a = body.advice;
  const num = (v, d) => call(sandbox, '_usageCalcNum', v, d);
  const clock = iso => call(sandbox, '_usageCalcClock', iso);
  const tr = (key, args) => call(sandbox, 't', key, args);
  const verdict = v => (v === 'over' ? tr('usage_calc_over') : tr('usage_calc_within'));
  const runOut = (at, early) => (at ? clock(at) + ' · ' + tr('usage_calc_hours_early', [num(early)]) : tr('usage_calc_runout_none'));
  const recent = tr('usage_calc_recent_burn');
  const average = tr('usage_calc_week_average');
  const rows = [
    `${tr('usage_calc_field_read')} ${num(a.percent, 0)}% · ${clock(a.read_at)}`,
    `${tr('usage_calc_field_days')} ${tr('usage_calc_days', [num(a.days_since_reset, 2)])}`,
    `${tr('usage_calc_field_pace')} ${num(a.pace_line)}% · ` +
      (a.ahead_of_pace ? tr('usage_calc_ahead_of_pace') : tr('usage_calc_headroom', [num(a.headroom)])),
    `${tr('usage_calc_field_burn')} ${recent} ${num(a.recent_burn_per_24h)}` +
      (a.recent_fallback ? ` (${tr('usage_calc_burn_fallback')})` : '') + ` · ${average} ${num(a.week_avg_burn_per_24h)}`,
    `${tr('usage_calc_field_projected')} ${recent} ${num(a.projected_end_recent)}% · ${average} ${num(a.projected_end_avg)}%`,
    `${tr('usage_calc_field_runout')} ${recent}: ${runOut(a.run_out_at_recent, a.hours_early_recent)} · ` +
      `${average}: ${runOut(a.run_out_at_avg, a.hours_early_avg)}`,
    `${tr('usage_calc_field_verdict')} ${verdict(a.verdict)} ${verdict(a.verdict_avg)}`,
    `${tr('usage_calc_field_lanes')} ${tr('usage_calc_lanes_policy', [String(a.lanes), tr('usage_calc_policy_' + body.policy.replace('-', '_'))])}`,
    `${tr('usage_calc_field_advice')} ${a.advice.headline}`,
  ];
  return rows.map(r => r.replace(/\s+/g, ' ').trim());
}

// The advice block of each policy for the worked reading (docs/plans/2026-10-23-usage-calculator-design.md).
const POLICY_ADVICE = {
  'use-all': {
    policy: 'use-all',
    lanes_suggested: 2,
    headline: 'Over budget at the recent burn: 3 lanes run out about 6 hours before the reset. 2 lanes end at 96.1%.',
    notes: ['The operator chooses the lane count; this advice does not set it.', 'Running out early is allowed under this policy.'],
    finish: null,
    policy_target: 100,
  },
  'pace': {
    policy: 'pace',
    lanes_suggested: 1,
    headline: 'Over budget for the week at 3 lanes (106% at the reset). 1 lane ends at 86.1%.',
    notes: ['The operator chooses the lane count; this advice does not set it.', 'Pace to the week aims 5 points under the pace line.'],
    finish: null,
    policy_target: 95,
  },
  'finish-early': {
    policy: 'finish-early',
    lanes_suggested: 3,
    headline: 'Within budget: the work finishes in about 22.8 hours with 5.2 points of allowance to spare.',
    notes: ['The operator chooses the lane count; this advice does not set it.', 'All lanes run; the calculator does not slow them down.'],
    finish: { hours: 22.8, at: '2026-10-24T20:54:00-04:00', end_percent: 94.8, spare_points: 5.2, before_allowance: true, no_burn: false },
    policy_target: null,
  },
};

// patched returns a copy of body with patch merged over it, the way workedExampleBody applies a patch.
function patched(body, patch) {
  return merge(JSON.parse(JSON.stringify(body)), patch);
}

function policyBody(id, patch) {
  const body = workedExampleBody({ policy: id, advice: { advice: POLICY_ADVICE[id] } });
  return patch ? patched(body, patch) : body;
}

// The five bodies the parity is checked on, besides the worked example.
const CASES = {
  'the worked example': () => workedExampleBody(),
  'a stale series': () => workedExampleBody({ stale: true, keeper: 'peer-b', copied_at: '2026-10-23T20:00:00-04:00' }),
  'bought extra credit': () => workedExampleBody({
    advice: {
      extra_credit_points: 15, ceiling: 115, projected_end_recent: 105.98, verdict: 'within',
      run_out_at_recent: null, hours_early_recent: 0,
      by_lanes: [
        { lanes: 0, burn_per_24h: 3, planning_burn_per_24h: 3, projected_end: 76.23, run_out_at: null, hours_early: 0 },
        { lanes: 1, burn_per_24h: 10.03, planning_burn_per_24h: 7.73, projected_end: 86.14, run_out_at: null, hours_early: 0 },
        { lanes: 2, burn_per_24h: 17.05, planning_burn_per_24h: 12.46, projected_end: 96.07, run_out_at: null, hours_early: 0 },
        { lanes: 3, burn_per_24h: 24.08, planning_burn_per_24h: 17.18, projected_end: 105.98, run_out_at: null, hours_early: 0 },
      ],
      chosen: { lanes: 3, burn_per_24h: 24.08, projected_end: 105.98, run_out_at: null, hours_early: 0 },
      advice: { lanes_suggested: 3, headline: 'Within budget for the week at 3 lanes (106% at the reset, ceiling 115%).', policy_target: 110 },
    },
  }),
  'a typed remaining-work figure': () => policyBody('finish-early', {
    advice: { remaining_work: { in_use: 'typed', points: 15.5, estimate_points: 20, typed_points: 15.5, queue_tasks: 6, cost_per_task: 3.33 } },
  }),
  'a recent burn that fell back to the week average': () => workedExampleBody({
    advice: {
      recent_fallback: true,
      fallback_notice: 'No earlier reading at least 2 hours back; the week average is used, so this figure is smoother than the recent burn.',
      recent_burn_per_24h: 12.88, projected_end_recent: 90.18, verdict: 'within', run_out_at_recent: null, hours_early_recent: 0,
      advice: { headline: 'Within budget for the week at 3 lanes (90% at the reset).' },
    },
  }),
};

test('the registry holds the three variants, and each one is a function', () => {
  const s = loadApp();
  const ids = vm.runInContext('Object.keys(window.usageCalcRenderers)', s);
  assert.deepEqual([...ids].sort(), [...VARIANTS].sort());
  for (const id of ids) assert.equal(vm.runInContext(`typeof window.usageCalcRenderers['${id}']`, s), 'function', id);
});

test('a stale body shows the last-copy marker in every variant, with the numbers unchanged', () => {
  const s = loadApp();
  const stale = CASES['a stale series']();
  for (const id of VARIANTS) {
    assert.ok(textOf(view(s, id, stale)).includes('Last copy from peer-b at'), `${id} shows the marker`);
    assert.deepEqual(fieldRows(view(s, id, stale)), fieldRows(view(s, id, workedExampleBody())), `${id}: the stale copy changes no number`);
  }
});

test('the settings card offers the same policy ids and labels as the page', () => {
  const s = loadApp();
  const field = vm.runInContext("GENERAL_CONFIG_FIELDS.flatMap(g => g.fields).find(f => f.key === 'usage.calculator.policy')", s);
  assert.deepEqual([...field.options], POLICIES);
  for (const p of POLICIES) {
    assert.equal(EN[field.optionLabelKeys[p]], POLICY_LABELS[p], `settings label of ${p}`);
    assert.equal(EN['usage_calc_policy_' + p.replace('-', '_')], POLICY_LABELS[p], `page label of ${p}`);
  }
});

test('a renderer draws only response fields: echoes of the inputs move nothing', () => {
  const s = loadApp();
  const base = workedExampleBody();
  // Fields that echo what was typed or set, with every output field held. None of them is drawn.
  const echoed = patched(base, {
    advice: { recent_lanes: 1, baseline_per_24h: 9.5, safety_points: 40, latest_age_hours: 30, allowance_per_24h: 99 },
    settings: { percent: 11, lanes: 0, policy: 'use-all', safety_points: 40, cost_per_task_points: 9 },
    input: { percent: 11, previous_percent: 3, previous_read_at: '2026-10-01T00:00:00-04:00', lanes: 0 },
  });
  echoed.advice.settings = { percent: 11, lanes: 0 };
  for (const id of VARIANTS) {
    assert.equal(view(s, id, echoed), view(s, id, base), `${id}: the page is identical`);
  }
});

test('drawing a variant does not change the response body', () => {
  const s = loadApp();
  for (const id of VARIANTS) {
    const body = workedExampleBody();
    const before = JSON.stringify(body);
    view(s, id, body);
    call(s, `window.usageCalcRenderers['${id}']`, body, {});
    assert.equal(JSON.stringify(body), before, `${id} left the body as it was`);
  }
});

test('the visible text of every variant names no tracker id', () => {
  const s = loadApp();
  for (const id of VARIANTS) {
    assert.ok(!/\b(BL|GH|SEC|B)\d+\b/.test(textOf(view(s, id, workedExampleBody()))), `${id}: no tracker id`);
  }
});
