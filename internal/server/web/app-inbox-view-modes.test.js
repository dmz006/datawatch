'use strict';
// app-inbox-view-modes.test.js
//
// The Inbox sub-tab's view-mode switch: the three Inbox lists (Triage, Open
// questions, Held questions) share one mode — List / Tiles / Compact — under
// the shared list-mode storage with scope 'inbox'.
//
// Pins, per mode:
//   * the toggle row renders three buttons (List / Tiles / Compact) with
//     exactly one marked active, no group-by select;
//   * the remembered mode is read back (List stays List, Tiles stays
//     Tiles, Compact stays Compact);
//   * each of the three Inbox lists renders in each of the three modes —
//     tile grids for tiles/compact, the plain rows/cards for list;
//   * compact pins the tile grid to data-tile-mode="tile" (the resize
//     handler skips .inbox-tile-grid-compact, like Sessions compact);
//   * a triage tile keeps the card's data attributes and action buttons
//     (the delegated click handler still dispatches);
//   * a merge_blocked triage record keeps its full card in every mode;
//   * switching modes does not load anything and does not alter the Inbox
//     badge count.
//
// The sandbox loads the real app.js plus the triage view model, sets the
// data on the loaders the way the list loaders do, and inspects the HTML the
// renderers produce for the stored mode. Timers are no-ops; no fetch is
// issued, which is also the point of the "no refetch" assertion.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub.js');

const APP_JS = path.join(__dirname, 'app.js');
const TRIAGE_MODEL_JS = path.join(__dirname, 'automata-triage-model.js');
const EN_JSON = path.join(__dirname, 'locales', 'en.json');

// capElement captures what the app writes to .innerHTML, the way the real
// toggle cell would hold it.
function capElement() {
  const cap = { html: '' };
  const el = { querySelector: () => null, querySelectorAll: () => [] };
  Object.defineProperty(el, 'innerHTML', { get() { return cap.html; }, set(v) { cap.html = v; } });
  return { el, cap };
}

function makeSandbox() {
  const { el, cap } = capElement();
  const sbx = buildSandbox({ byId: { inboxViewModeToggle: el } });
  sbx.innerWidth = 1280;
  vm.createContext(sbx);
  return { sbx, cap };
}

function loadApp(sbx) {
  loadScript(TRIAGE_MODEL_JS, sbx);
  loadScript(APP_JS, sbx);
  const en = require(EN_JSON);
  sbx._i18n.bundle = en;
  sbx._i18n.fallback = en;
}

function run(sbx, code) {
  return vm.runInContext(code, sbx, { filename: '<inbox-view-modes-test>' });
}

// ── fixtures ────────────────────────────────────────────────────────────────

const NOW = Date.now();
const iso = (minsAgo) => new Date(NOW - minsAgo * 60000).toISOString();

function triageBody() {
  return {
    records: [
      {
        prd_id: 'prd-1', task_id: 'task-1', status: 'failed',
        task_title: 'Run the suite', error: 'AssertionError: timed out',
        automation_id: 'auto-1', automation_name: 'Nightly',
        first_seen_at: iso(40),
      },
      {
        prd_id: 'prd-2', task_id: 'task-2', status: 'blocked',
        task_title: 'Write the docs', blocked_reason: 'Needs operator sign-off',
        reason_source: 'blocked_reason', first_seen_at: iso(120),
      },
      {
        prd_id: 'prd-mb', status: 'merge_blocked', prd_title: 'Blocked merge',
        merge_blocks: [{ task_title: 'T1', reason: 'conflict', action: 'rebase' }],
      },
    ],
  };
}

function questionBody() {
  return {
    count: 1,
    questions: [{
      id: 'q-42', label: 'Pick a lane', text: 'Which lane should this go to?',
      asked_by: 'auto-1', created_at: iso(40), priority: 'high',
      options: [{ id: 'lane-a', label: 'Lane A', tradeoffs: 'faster' }],
    }],
  };
}

function heldBody() {
  return {
    mode: 'ask',
    open: [{
      question_id: 'q-hold-1', title: 'Pick a held lane', text: 'Which lane is this one?',
      reason: 'two viable lanes', recommended_option: 'lane-a',
      prd_id: 'prd-9', task_id: 'task-9',
    }],
    shadow: [],
  };
}

// ── tests ───────────────────────────────────────────────────────────────────

test('Inbox toggle: three mode buttons, one active, no group select', () => {
  const { sbx, cap } = makeSandbox();
  loadApp(sbx);

  // Paint the toggle row the same way the Inbox sub-tab and a mode switch do.
  run(sbx, `_inboxViewRepaint()`);
  const html = cap.html;
  for (const mode of ['list', 'tiles', 'compact']) {
    assert.ok(html.includes(`listSetMode('inbox','${mode}')`),
      `toggle row lacks the ${mode} button: ${html}`);
  }
  assert.equal((html.match(/tag-view-btn active/g) || []).length, 1,
    'exactly one active button: ' + html);
  assert.equal(/<select/.test(html), false, 'no group-by select on the Inbox toggle');
  // 1280px default is tiles.
  assert.ok(html.includes(`tag-view-btn active" aria-pressed="true" onclick="listSetMode('inbox','tiles')`),
    'tiles is the active default at 1280px: ' + html);
});

test('Inbox mode: remembered choice is read back for every list', () => {
  const { sbx, cap } = makeSandbox();
  loadApp(sbx);

  for (const [mode, needle] of [
    ['tiles', 'work-tile-grid'],
    ['compact', 'inbox-tile-grid-compact'],
    ['list', 'triage-card'],
  ]) {
    run(sbx, `window.listSetMode('inbox', ${JSON.stringify(mode)})`);
    if (mode === 'list') {
      assert.ok(run(sbx, `window.triageListHtml(${JSON.stringify(triageBody())})`).includes('triage-card'),
        'list mode: plain triage cards');
      assert.ok(!run(sbx, `window.triageListHtml(${JSON.stringify(triageBody())})`).includes('work-tile-grid'),
        'list mode: no tile grid');
    } else {
      const h = run(sbx, `window.triageListHtml(${JSON.stringify(triageBody())})`);
      assert.ok(h.includes(needle), `tiles mode=${mode}: ${needle} missing: ${h}`);
      assert.ok(!h.includes('triage-btn-desc'), 'tile mode drops the button descriptions');
    }
  }

  // And the toggle row repaints with the active mark following the mode.
  const tHtml = cap.html;
  assert.ok(tHtml.includes(`listSetMode('inbox','list')`), 'toggle still present');
  assert.ok(/tag-view-btn active"/.test(tHtml), 'toggle marks one active: ' + tHtml);
});

test('Triage tile: keeps the card data attributes, buttons, and the merge_blocked card', () => {
  const { sbx } = makeSandbox();
  loadApp(sbx);
  run(sbx, `window.listSetMode('inbox', 'tiles')`);

  const h = run(sbx, `window.triageListHtml(${JSON.stringify(triageBody())})`);
  assert.ok(h.includes('data-tile-mode="tile"'), '1280px tiles grid: ' + h);
  // The failed record's tile.
  assert.ok(h.includes('work-tile triage-tile'), 'failed record renders a triage tile');
  assert.ok(h.includes('data-prd-id="prd-1"'), 'tile keeps data-prd-id');
  assert.ok(h.includes('data-action="open-detail"'), 'tile keeps the open-detail button');
  assert.ok(h.includes('data-action="retry"'), 'tile keeps the retry button (task record)');
  assert.ok(h.includes('data-action="cancel"'), 'tile keeps the cancel button');
  assert.ok(h.includes('Run the suite'), 'tile headline names the task');
  assert.ok(h.includes('AssertionError: timed out'), 'tile shows the failure reason');
  assert.ok(h.includes('Needs operator sign-off'), 'blocked tile shows the block reason');
  assert.ok(h.includes('waiting'), 'tile shows the wait line');
  // The merge_blocked record keeps its own full card inside the tile grid.
  assert.ok(h.includes('merge-blocked-row'), 'merge_blocked keeps its card rows');
  assert.ok(h.includes('Blocked merge'), 'merge_blocked card names the prd');
});

test('Triage compact: pins data-tile-mode to tile', () => {
  const { sbx } = makeSandbox();
  loadApp(sbx);
  run(sbx, `window.listSetMode('inbox', 'compact')`);

  const h = run(sbx, `window.triageListHtml(${JSON.stringify(triageBody())})`);
  assert.ok(h.includes('inbox-tile-grid-compact'), 'compact grid class: ' + h);
  assert.ok(h.includes('data-tile-mode="tile"'), 'compact pins the tile layout: ' + h);
});

test('Open questions: render in each mode, keep id/option/open attributes', () => {
  const { sbx } = makeSandbox();
  loadApp(sbx);
  const body = JSON.stringify(questionBody());

  run(sbx, `window.listSetMode('inbox', 'list')`);
  const listHtml = run(sbx, `window.questionsCardHtml(${body})`);
  assert.ok(listHtml.includes('questions-queue-list'), 'list mode keeps the queue rows');
  assert.ok(!listHtml.includes('work-tile-grid'), 'list mode: no tile grid');

  for (const mode of ['tiles', 'compact']) {
    run(sbx, `window.listSetMode('inbox', ${JSON.stringify(mode)})`);
    const h = run(sbx, `window.questionsCardHtml(${body})`);
    assert.ok(h.includes('work-tile-grid'), `${mode}: tile grid`);
    assert.ok(h.includes('work-tile questions-question-tile'), `${mode}: question tile`);
    assert.ok(h.includes('data-question-id="q-42"'), `${mode}: tile keeps the question id`);
    assert.ok(h.includes('data-option-id="lane-a"'), `${mode}: tile keeps the option button`);
    assert.ok(h.includes('questions-open-btn'), `${mode}: tile keeps the Open action`);
    assert.ok(h.includes('Lane A'), `${mode}: tile names the option`);
    if (mode === 'compact') {
      assert.ok(h.includes('inbox-tile-grid-compact'), 'compact grid class');
      assert.ok(h.includes('data-tile-mode="tile"'), 'compact pins the tile layout');
    }
  }
});

test('Held questions: render in each mode, keep id/answer attributes', () => {
  const { sbx } = makeSandbox();
  loadApp(sbx);
  const body = JSON.stringify(heldBody());

  run(sbx, `window.listSetMode('inbox', 'list')`);
  const listHtml = run(sbx, `window.heldQuestionsCardHtml(${body})`);
  assert.ok(listHtml.includes('held-question-list'), 'list mode keeps the held rows');
  assert.ok(!listHtml.includes('work-tile-grid'), 'list mode: no tile grid');

  for (const mode of ['tiles', 'compact']) {
    run(sbx, `window.listSetMode('inbox', ${JSON.stringify(mode)})`);
    const h = run(sbx, `window.heldQuestionsCardHtml(${body})`);
    assert.ok(h.includes('work-tile-grid'), `${mode}: tile grid`);
    assert.ok(h.includes('work-tile held-question-tile'), `${mode}: held tile`);
    assert.ok(h.includes('data-question-id="q-hold-1"'), `${mode}: tile keeps the question id`);
    assert.ok(h.includes('data-task-id="task-9"'), `${mode}: tile keeps the task id`);
    assert.ok(h.includes('held-question-answer'), `${mode}: tile keeps the Answer button`);
    assert.ok(h.includes('lane-a'), `${mode}: tile names the recommended option`);
    if (mode === 'compact') {
      assert.ok(h.includes('inbox-tile-grid-compact'), 'compact grid class');
      assert.ok(h.includes('data-tile-mode="tile"'), 'compact pins the tile layout');
    }
  }
});

test('Mode switch repaints from loaded data and never changes the Inbox badge', () => {
  const { sbx } = makeSandbox();
  loadApp(sbx);

  // The Inbox badge starts from the loaded counts.
  run(sbx, `
    _automationInboxCountSet('automation_triage', ${JSON.stringify(triageBody())});
    _automationInboxCountSet('automation_questions', ${JSON.stringify(questionBody())});
    _automationInboxCountSet('automation_held_questions', ${JSON.stringify(heldBody())});
  `);
  const before = run(sbx, `_automationInboxBadgeCount()`);
  assert.ok(before >= 2, 'badge counts the loaded actionable items: ' + before);

  // Fake the three Inbox loaders the way listLoader would paint them; a
  // paint re-renders the data already held (load is what would fetch), so
  // counting each key's paints shows the switch repaints without loading.
  const paints = {};
  for (const k of ['automation_triage', 'automation_questions', 'automation_held_questions']) {
    paints[k] = 0;
    sbx._listLoaders[k] = { state: { hasData: true }, paint: () => { paints[k] += 1; } };
  }

  for (const mode of ['list', 'tiles', 'compact']) {
    run(sbx, `window.listSetMode('inbox', ${JSON.stringify(mode)})`);
  }
  const after = run(sbx, `_automationInboxBadgeCount()`);
  assert.equal(after, before, 'the Inbox badge count is unchanged by a mode switch');
  for (const k of Object.keys(paints)) {
    assert.equal(paints[k], 3, `${k} repainted once per mode switch (no refetch): ${paints[k]}`);
  }
});
