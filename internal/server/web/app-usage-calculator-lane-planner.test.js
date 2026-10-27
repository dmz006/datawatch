// Usage calculator lane-planner variant — PWA side.
//
// window.usageCalcRenderers['lane-planner'](body, opts) draws the table with one row
// for each lane count in the response, the advised and chosen badges, the finish line
// for Run flat out and the week timeline. The collapsible reading list in the verdict
// card and the burn-per-lane note under the inputs come through the frame's hooks.
// The renderer only reads the response; these tests check the drawing of it.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');
const { workedExampleBody } = require('./usage-calculator-fixture');

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

function render(sandbox, body) {
  sandbox._body = body;
  return vm.runInContext("window.usageCalcRenderers['lane-planner'](_body, {})", sandbox);
}

function frame(sandbox, body, opts) {
  sandbox._body = body;
  sandbox._opts = opts || {};
  return vm.runInContext('window.usageCalcFrameHtml(_body, _opts)', sandbox);
}

// rowsOf reads each table row: its lane count, its data marks and the text of its cells.
function rowsOf(html) {
  return [...html.matchAll(/<tr data-lanes="(\d+)"([^>]*)>([\s\S]*?)<\/tr>/g)].map(m => ({
    lanes: Number(m[1]),
    marks: m[2],
    cells: [...m[3].matchAll(/<td[^>]*>([\s\S]*?)<\/td>/g)]
      .map(c => c[1].replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ').trim()),
  }));
}

// finishBlock is a finish that fits before the allowance, as the worked example's
// Run flat out figures give it: 20 points at 3 lanes finish Sat 20:54 with 5.2 to spare.
const FINISH_WITHIN = {
  hours: 22.8, at: '2026-10-24T20:54:00-04:00', end_percent: 94.8, spare_points: 5.2, before_allowance: true, no_burn: false,
};

function flatOut(extra) {
  return workedExampleBody(Object.assign({
    policy: 'finish-early',
    advice: {
      policy: 'finish-early',
      advice: {
        policy: 'finish-early',
        lanes_suggested: 3,
        headline: 'Within budget: the work finishes in about 22.8 hours with 5.2 points of allowance to spare.',
        notes: ['All lanes run; the calculator does not slow them down under this policy.'],
        finish: FINISH_WITHIN,
        policy_target: null,
      },
    },
  }, extra || {}));
}

test('the lane-planner renderer and its two hooks are registered under the variant id', () => {
  const s = loadApp();
  assert.equal(vm.runInContext("typeof window.usageCalcRenderers['lane-planner']", s), 'function');
  assert.equal(vm.runInContext("typeof window.usageCalcVerdictDetails['lane-planner']", s), 'function');
  assert.equal(vm.runInContext("typeof window.usageCalcInputNotes['lane-planner']", s), 'function');
});

test('the worked example under Run until used up gives four rows, and the fit runs to 100 percent', () => {
  const s = loadApp();
  const body = workedExampleBody({ policy: 'use-all', advice: { advice: { policy: 'use-all', policy_target: 100, lanes_suggested: 2 } } });
  const rows = rowsOf(render(s, body));
  assert.deepEqual(rows.map(r => r.lanes), [0, 1, 2, 3]);
  assert.deepEqual(rows.map(r => [r.cells[1], r.cells[2]]), [
    ['3.0', '76.2%'], ['10.0', '86.1%'], ['17.1', '96.1%'], ['24.1', '106.0%'],
  ]);
  assert.deepEqual(rows.map(r => r.cells[5]), ['fits', 'fits', 'fits', 'does not fit']);
  assert.equal(rows[0].cells[3], 'none before the reset', 'a row with no run out');
  assert.equal(rows[3].cells[3], 'Sun 02:02', 'the run-out row names the run-out clock');
  assert.equal(rows[3].cells[4], '6.0', 'and its hours early');
});

test('under Pace to the week the target is 95 and 2 lanes and 3 lanes do not fit', () => {
  const s = loadApp();
  const html = render(s, workedExampleBody());
  const rows = rowsOf(html);
  assert.deepEqual(rows.map(r => r.cells[5]), ['fits', 'fits', 'does not fit', 'does not fit']);
  assert.ok(html.includes('Target for this policy: 95% at the reset, with the safety margin.'), 'pace target line');
  // The model advises 1 lane under pace (design section "The three usage policies"), so the
  // advised badge sits on row 1, and the chosen badge on row 3.
  assert.match(rows[1].marks, /data-advised="true"/);
  assert.doesNotMatch(rows[2].marks, /data-advised/);
  assert.match(rows[3].marks, /data-chosen="true"/);
});

test('the advised row and the chosen row carry badges under Run until used up', () => {
  const s = loadApp();
  const body = workedExampleBody({ policy: 'use-all', advice: { advice: { policy: 'use-all', policy_target: 100, lanes_suggested: 2 } } });
  const rows = rowsOf(render(s, body));
  assert.match(rows[2].marks, /data-advised="true"/);
  assert.match(rows[3].marks, /data-chosen="true"/);
  assert.ok(rows[2].cells[0].includes('Advised') && !rows[2].cells[0].includes('Your choice'), 'row 2 is advised only');
  assert.ok(rows[3].cells[0].includes('Your choice') && !rows[3].cells[0].includes('Advised'), 'row 3 is the choice only');
  assert.equal(rows[1].cells[0], '1', 'a plain row has its count and no badge');
});

test('the zero-lane row is labelled never advised and operator decision, and is never the advice', () => {
  const s = loadApp();
  const rows = rowsOf(render(s, workedExampleBody({ policy: 'use-all', advice: { advice: { policy: 'use-all', policy_target: 100, lanes_suggested: 2 } } })));
  assert.ok(rows[0].cells[0].includes('Never advised'), 'never advised label');
  assert.ok(rows[0].cells[0].includes('Operator') && rows[0].cells[0].includes('decision'), 'operator decision label');
  assert.doesNotMatch(rows[0].marks, /data-advised/);
});

test('zero chosen lanes adds the your-choice badge to the zero row as well', () => {
  const s = loadApp();
  const body = workedExampleBody({
    policy: 'use-all',
    advice: {
      lanes: 0,
      chosen: { lanes: 0, burn_per_24h: 3, projected_end: 76.23, run_out_at: null, hours_early: 0 },
      advice: { policy: 'use-all', policy_target: 100, lanes_suggested: 2 },
    },
  });
  const rows = rowsOf(render(s, body));
  assert.match(rows[0].marks, /data-chosen="true"/);
  assert.ok(rows[0].cells[0].includes('Your choice'));
  assert.ok(rows[0].cells[0].includes('Never advised'));
});

test('under Run flat out every row reads running, with a finish line and no target percent', () => {
  const s = loadApp();
  const html = render(s, flatOut());
  const rows = rowsOf(html);
  assert.deepEqual(rows.map(r => r.cells[5]), ['running', 'running', 'running', 'running']);
  assert.ok(!html.includes('does not fit') && !html.includes('>fits<'), 'no fit mark under Run flat out');
  assert.ok(html.includes('Run flat out has no end target'), 'no-target wording');
  assert.ok(html.includes('Work finishes Sat 20:54, 5.2 points to spare.'), 'finish line from advice.finish');
});

test('the no-burn finish shows the no-burn wording and never an infinite figure', () => {
  const s = loadApp();
  const html = render(s, flatOut({
    advice: {
      advice: {
        finish: { before_allowance: false, no_burn: true },
        headline: 'No lane is burning above the idle baseline, so no finish time can be given.',
      },
    },
  }));
  assert.ok(html.includes('No finish time: no lane is burning above the idle baseline.'), 'no-burn wording missing');
  assert.ok(!html.includes('Infinity'), 'no infinite figure');
  assert.ok(!html.includes('Work finishes'), 'no finish time is given');
});

test('a finish past the allowance reads as finishing after the allowance runs out', () => {
  const s = loadApp();
  const html = render(s, flatOut({
    advice: { advice: { finish: Object.assign({}, FINISH_WITHIN, { before_allowance: false, spare_points: -3.4 }) } },
  }));
  assert.ok(html.includes('Work finishes Sat 20:54, after the allowance runs out.'));
});

test('without remaining work the finish line asks for it', () => {
  const s = loadApp();
  const html = render(s, flatOut({ advice: { advice: { finish: null } } }));
  assert.ok(html.includes('Enter the remaining work to see the finish time.'));
});

test('the finish line appears only under Run flat out', () => {
  const s = loadApp();
  assert.ok(!render(s, workedExampleBody()).includes('usage-calc-lane-finish'), 'no finish line under Pace to the week');
  assert.ok(render(s, flatOut()).includes('usage-calc-lane-finish'), 'finish line under Run flat out');
});

test('bought extra credit moves the target the response names, and the fit reads that target', () => {
  const s = loadApp();
  const body = workedExampleBody({
    policy: 'use-all',
    advice: { extra_credit_points: 10, ceiling: 110, advice: { policy: 'use-all', policy_target: 110, lanes_suggested: 3 } },
  });
  const html = render(s, body);
  assert.ok(html.includes('Target for this policy: 110% at the reset, the full allowance.'), 'target from the response');
  assert.equal(rowsOf(html)[3].cells[5], 'fits', '3 lanes at 106.0 fit a 110 target');
});

test('the renderer reads policy_target, not the ceiling, for the fit', () => {
  const s = loadApp();
  const body = workedExampleBody({
    policy: 'use-all',
    advice: { ceiling: 110, advice: { policy: 'use-all', policy_target: 100, lanes_suggested: 2 } },
  });
  assert.equal(rowsOf(render(s, body))[3].cells[5], 'does not fit', 'a 100 target still rejects 106.0');
});

test('the table shows the burn and projection the response gives, not a recomputed figure', () => {
  const s = loadApp();
  const body = workedExampleBody();
  body.advice.by_lanes[1].burn_per_24h = 42.42;
  body.advice.by_lanes[1].projected_end = 99.9;
  const rows = rowsOf(render(s, body));
  assert.equal(rows[1].cells[1], '42.4');
  assert.equal(rows[1].cells[2], '99.9%');
});

test('the timeline draws one line per lane count from the newest reading and a run-out point for 3 lanes', () => {
  const s = loadApp();
  const html = render(s, workedExampleBody());
  const lines = [...html.matchAll(/data-lane-line="(\d)" x1="([\d.]+)" y1="([\d.]+)"/g)];
  assert.deepEqual(lines.map(m => m[1]), ['0', '1', '2', '3']);
  assert.ok(lines.every(m => m[2] === lines[0][2] && m[3] === lines[0][3]), 'every line starts at the newest reading');
  // Fri 22:08 is 134.13 of the 168 hours between the Sunday resets, on the 272 wide plot from x 36.
  assert.ok(Math.abs(Number(lines[0][2]) - 253.2) < 0.3, 'start x was ' + lines[0][2]);
  assert.ok(html.includes('data-run-out="3"'), 'run-out point for 3 lanes');
  assert.ok(html.includes('Lanes 3 run out Sun 02:02'), 'run-out label');
  assert.ok(html.includes('Now Fri 22:08'), 'now marker');
});

test('a tick falls every 24 hours across the week, both ends included', () => {
  const s = loadApp();
  const html = render(s, workedExampleBody());
  assert.equal((html.match(/data-tick-day="\d"/g) || []).length, 8, 'eight ticks from Sunday to Sunday');
  assert.ok(html.includes('>Sun</text>'), 'the week starts on the reset weekday');
});

test('the reading fields sit in a collapsible list inside the verdict card, above the lane body', () => {
  const s = loadApp();
  const html = frame(s, workedExampleBody({ variant: 'lane-planner' }), {});
  const verdictAt = html.indexOf('class="usage-calc-verdict"');
  const listAt = html.indexOf('<details class="usage-calc-lane-fields"');
  const bodyAt = html.indexOf('class="usage-calc-lane-planner"');
  assert.ok(verdictAt >= 0 && listAt > verdictAt, 'the list is drawn inside the verdict card');
  assert.ok(bodyAt > listAt, 'the lane body comes after the verdict card');
  assert.ok(!/<details class="usage-calc-lane-fields" open/.test(html), 'the list starts closed');
  assert.ok(html.slice(listAt, bodyAt).includes('data-field="0"'), 'the list holds the reading fields');
});

test('the burn-per-lane note sits under the input form and says it is inferred', () => {
  const s = loadApp();
  const html = frame(s, workedExampleBody({ variant: 'lane-planner' }), {});
  const form = html.indexOf('usageCalcSubmitReading()');
  const note = html.indexOf('usage-calc-lane-per-lane');
  assert.ok(note > form, 'the note comes after the reading form');
  assert.ok(html.includes('Burn per lane: 7.0 per 24 hours, inferred, recalibrate at each reading.'));
});

test('the stale marker is still drawn by the frame above the lane planner', () => {
  const s = loadApp();
  const html = frame(s, workedExampleBody({
    variant: 'lane-planner', stale: true, copied_at: '2026-10-23T20:00:00-04:00', keeper: 'peer-b',
  }), {});
  assert.ok(html.includes('Last copy from peer-b at'), 'stale marker missing');
  assert.ok(html.indexOf('Last copy from') < html.indexOf('class="usage-calc-lane-planner"'), 'marker sits in the verdict card');
});

test('the lane planner adds no tracker ID or PRD wording to the drawn text', () => {
  const s = loadApp();
  const html = render(s, workedExampleBody());
  assert.doesNotMatch(html, /\bPRD\b/, 'PRD is internal-only wording');
  assert.doesNotMatch(html, /\b(BL|GH|SEC)\d+/, 'no tracker id');
});

test('the table fits the phone width: full width, fixed columns, no pixel minimum', () => {
  const s = loadApp();
  const html = render(s, workedExampleBody());
  assert.ok(html.includes('width:100%;table-layout:fixed'), 'table takes the width of its card');
  assert.doesNotMatch(html, /min-width:\s*\d{3,}px/, 'no fixed minimum wider than a phone');
  assert.ok(html.includes('viewBox="0 0 320 196"') && html.includes('width="100%"'), 'the timeline scales to the width');
});

test('the renderer shows no NaN or undefined in the drawn text', () => {
  const s = loadApp();
  for (const body of [workedExampleBody(), flatOut()]) {
    const html = render(s, body);
    assert.ok(!html.includes('NaN'), 'NaN in the output');
    assert.ok(!html.includes('undefined'), 'undefined in the output');
  }
});

test('an empty lane list draws no table and no timeline lines, and does not fail', () => {
  const s = loadApp();
  const html = render(s, workedExampleBody({ advice: { by_lanes: [] } }));
  assert.ok(!html.includes('usage-calc-lane-table'), 'no table without rows');
  assert.ok(!html.includes('data-lane-line'), 'no timeline line without rows');
});
