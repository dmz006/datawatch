// Hand-written response bodies for the usage calculator PWA tests. Each body has
// the shape of GET /api/usage/advice: usagecalc.AdviceResult in
// internal/usagecalc/service.go, with the Advice, PolicyAdvice, LaneRow, Finish,
// WorkResult and SeriesPoint types of internal/usagecalc/model.go.
//
// The worked example is the 22:08 reading of Friday 2026-10-23 at 72 percent,
// with the figures of docs/plans/2026-10-23-usage-calculator-design.md. A test
// that needs another reading patches a copy with workedExampleBody(patch).

// Readings since the Sunday 2026-10-18 08:00 reset: hours since the reset, the
// weekly percent and the reading time (Eastern, UTC-4 in October).
const SERIES_ROWS = [
  [2.33, 2, '2026-10-18T10:20:00-04:00'],
  [6.50, 4, '2026-10-18T14:30:00-04:00'],
  [9.00, 5, '2026-10-18T17:00:00-04:00'],
  [13.08, 7, '2026-10-18T21:05:00-04:00'],
  [25.92, 9, '2026-10-19T09:55:00-04:00'],
  [33.83, 13, '2026-10-19T17:50:00-04:00'],
  [36.25, 14, '2026-10-19T20:15:00-04:00'],
  [44.52, 16, '2026-10-20T04:31:00-04:00'],
  [56.67, 19, '2026-10-20T16:40:00-04:00'],
  [78.67, 30, '2026-10-21T14:40:00-04:00'],
  [82.50, 33, '2026-10-21T18:30:00-04:00'],
  [83.67, 34, '2026-10-21T19:40:00-04:00'],
  [100.60, 45, '2026-10-22T12:36:00-04:00'],
  [108.73, 55, '2026-10-22T20:44:00-04:00'],
  [114.67, 58, '2026-10-23T02:40:00-04:00'],
  [129.15, 67, '2026-10-23T17:09:00-04:00'],
  [134.13, 72, '2026-10-23T22:08:00-04:00'],
];

const WORKED_ADVICE = {
  read_at: '2026-10-23T22:08:00-04:00',
  percent: 72,
  latest_age_hours: 0.0,
  last_reset_at: '2026-10-18T08:00:00-04:00',
  next_reset_at: '2026-10-25T08:00:00-04:00',
  extra_credit_points: 0,
  ceiling: 100,
  lanes: 3,
  recent_lanes: 3,
  max_lanes: 3,
  safety_points: 5,
  days_since_reset: 5.59,
  days_left: 1.41,
  allowance_per_24h: 14.29,
  pace_line: 79.84,
  headroom: 7.84,
  recent_burn_per_24h: 24.08,
  week_avg_burn_per_24h: 12.88,
  recent_fallback: false,
  projected_end_recent: 105.98,
  projected_end_avg: 90.18,
  run_out_at_recent: '2026-10-25T02:02:00-04:00',
  run_out_at_avg: null,
  hours_early_recent: 5.96,
  hours_early_avg: 0,
  verdict: 'over',
  verdict_avg: 'within',
  ahead_of_pace: false,
  per_lane_per_24h: 7.03,
  baseline_per_24h: 3,
  by_lanes: [
    { lanes: 0, burn_per_24h: 3, planning_burn_per_24h: 3, projected_end: 76.23, run_out_at: null, hours_early: 0 },
    { lanes: 1, burn_per_24h: 10.03, planning_burn_per_24h: 7.73, projected_end: 86.14, run_out_at: null, hours_early: 0 },
    { lanes: 2, burn_per_24h: 17.05, planning_burn_per_24h: 12.46, projected_end: 96.07, run_out_at: null, hours_early: 0 },
    { lanes: 3, burn_per_24h: 24.08, planning_burn_per_24h: 17.18, projected_end: 105.98, run_out_at: '2026-10-25T02:02:00-04:00', hours_early: 5.96 },
  ],
  chosen: { lanes: 3, burn_per_24h: 24.08, projected_end: 105.98, run_out_at: '2026-10-25T02:02:00-04:00', hours_early: 5.96 },
  remaining_work: { in_use: 'estimate', points: 20, estimate_points: 20, queue_tasks: 6, cost_per_task: 3.33 },
  series: SERIES_ROWS.map(([hours, percent, at]) => ({ at, percent, hours_since_reset: hours })),
  // One entry per day from the reset (Sunday 08:00) to the reading (Friday 22:08). The
  // points are the straight-line interpolation between readings, so they are estimates and
  // sum to the newest percent (72).
  daily: [
    { start: '2026-10-18T08:00:00-04:00', end: '2026-10-19T08:00:00-04:00', points: 8.700935, estimated: true, partial: false },
    { start: '2026-10-19T08:00:00-04:00', end: '2026-10-20T08:00:00-04:00', points: 8.158325, estimated: true, partial: false },
    { start: '2026-10-20T08:00:00-04:00', end: '2026-10-21T08:00:00-04:00', points: 9.805741, estimated: true, partial: false },
    { start: '2026-10-21T08:00:00-04:00', end: '2026-10-22T08:00:00-04:00', points: 15.346223, estimated: true, partial: false },
    { start: '2026-10-22T08:00:00-04:00', end: '2026-10-23T08:00:00-04:00', points: 19.301623, estimated: true, partial: false },
    { start: '2026-10-23T08:00:00-04:00', end: '2026-10-23T22:08:00-04:00', points: 10.687155, estimated: true, partial: true },
  ],
  advice: {
    policy: 'pace',
    lanes_suggested: 1,
    headline: 'Over budget for the week at 3 lanes (106% at the reset). 1 lane ends at 86.1%.',
    notes: [
      'The operator chooses the lane count; this advice does not set it.',
      'Pace to the week aims 5 points under the pace line.',
    ],
    finish: null,
    policy_target: 95,
  },
};

const WORKED_BODY = {
  variant: 'all',
  policy: 'pace',
  stale: false,
  advice: WORKED_ADVICE,
  // The text forms the daemon builds from the same body (usagecalc.FormatCLI and FormatComm).
  text: {
    cli: 'usage: 72% read Fri 22:08, reset Sun 08:00 (1.41 days left)\n' +
      'pace line 79.8%, headroom 7.8, days since reset 5.59\n' +
      'burn 24.1 per 24h recent, 12.9 per 24h week average\n' +
      'projected end 106.0% recent (over), 90.2% week average (within)\n' +
      'runs out Sun 02:02, 6 hours before the reset (recent burn)\n' +
      'lanes 3 running, policy Pace to the week\n' +
      'advice: 1 lane ends at 86.1%',
    comm: 'Weekly 72% (read Fri 22:08)\n' +
      'Reset Sun 08:00, 1.41 days left\n' +
      'Pace line 79.8%, headroom 7.8\n' +
      'Burn 24.1 per 24h recent, 12.9 average\n' +
      'Ends at reset: 106.0% recent, 90.2% average\n' +
      'Over budget at the recent burn\n' +
      'Runs out Sun 02:02, about 6 h early\n' +
      'Lanes 3, policy Pace to the week\n' +
      'Advice: 1 lane ends at 86.1%\n' +
      'Reply: usage lanes 1 (example, not final)',
  },
};

function clone(value) {
  return JSON.parse(JSON.stringify(value));
}

// merge copies patch over base, recursing into plain objects. Arrays and scalars
// in the patch replace the base value.
function merge(base, patch) {
  for (const key of Object.keys(patch || {})) {
    const next = patch[key];
    const plain = next !== null && typeof next === 'object' && !Array.isArray(next);
    if (plain && base[key] !== null && typeof base[key] === 'object' && !Array.isArray(base[key])) {
      merge(base[key], next);
    } else {
      base[key] = clone(next);
    }
  }
  return base;
}

// workedExampleBody returns a fresh copy of the worked example, with patch applied.
function workedExampleBody(patch) {
  return merge(clone(WORKED_BODY), patch || {});
}

// emptyBody is the response when no reading has been saved yet: the advice needs
// a reading, and the numbers are left at zero.
function emptyBody(patch) {
  const body = {
    variant: 'all',
    policy: 'use-all',
    stale: false,
    advice: {
      needs: 'reading',
      percent: 0,
      ceiling: 100,
      lanes: 0,
      recent_lanes: 0,
      max_lanes: 3,
      safety_points: 5,
      extra_credit_points: 0,
      days_since_reset: 0,
      days_left: 0,
      pace_line: 0,
      headroom: 0,
      recent_burn_per_24h: 0,
      week_avg_burn_per_24h: 0,
      recent_fallback: false,
      projected_end_recent: 0,
      projected_end_avg: 0,
      run_out_at_recent: null,
      run_out_at_avg: null,
      hours_early_recent: 0,
      hours_early_avg: 0,
      ahead_of_pace: false,
      by_lanes: [],
      series: [],
      daily: [],
      remaining_work: { in_use: 'none', points: 0, queue_tasks: 0, cost_per_task: 0 },
      advice: {
        policy: 'use-all',
        lanes_suggested: 0,
        headline: 'Enter a weekly reading to see advice.',
        notes: [],
        finish: null,
        policy_target: 100,
      },
    },
  };
  return merge(body, patch || {});
}

module.exports = { workedExampleBody, emptyBody, SERIES_ROWS, merge };
