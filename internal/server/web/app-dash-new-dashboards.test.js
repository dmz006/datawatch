// Six opt-in dashboard series cards — lane utilization, throughput forecast,
// verifier health, release train, review backlog and defects per day. Covers
// the card definitions (addable from Edit → Add Card, never in the default
// layout), the feature gates that keep each card off until its window holds
// data, the frame every card renders (what it is for, its state, the why when
// empty, and the next action), the locale keys in all five bundles, and that
// the operator-visible text names no tracker ID and never says "PRD".

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

const LOCALE_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];

// card id -> [element id, render function, active slot]
const CARDS = {
  'lane-utilization': { el: 'dashLaneUtilCard', render: '_dashRenderLaneUtil', slot: '_seriesLaneUtil', key: 'lane_util' },
  'throughput-forecast': { el: 'dashForecastCard', render: '_dashRenderForecast', slot: '_seriesForecast', key: 'forecast' },
  'verifier-health': { el: 'dashVerifierCard', render: '_dashRenderVerifier', slot: '_seriesVerifier', key: 'verifier' },
  'release-train': { el: 'dashReleaseTrainCard', render: '_dashRenderReleaseTrain', slot: '_seriesReleaseTrain', key: 'release_train' },
  'review-backlog': { el: 'dashReviewBacklogCard', render: '_dashRenderReviewBacklog', slot: '_seriesReviewBacklog', key: 'review_backlog' },
  'defects-per-day': { el: 'dashDefectsCard', render: '_dashRenderDefects', slot: '_seriesDefects', key: 'defects' },
};
const IDS = Object.keys(CARDS);

// Keys each card's frame reads, plus the card's own "why" reason key.
function frameKeys(id) {
  const k = CARDS[id].key;
  return ['dash_series_purpose_' + k, 'dash_series_next_' + k, 'dash_series_now_' + k];
}

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, lang + '.json'), 'utf8'));
}

function loadAppSandbox() {
  const els = {};
  for (const id of IDS) els[CARDS[id].el] = makeStubElement();
  const sandbox = buildSandbox({ byId: els });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const en = bundle('en');
  vm.runInContext('window._i18n = ' + JSON.stringify({ bundle: en, fallback: en }) + ';', sandbox);
  sandbox.__els = els;
  return sandbox;
}

function setSeries(sb, slot, payload) {
  vm.runInContext('_dash.' + slot + ' = ' + JSON.stringify(payload) + ';', sb);
}

function evalIn(sb, expr) {
  return vm.runInContext(expr, sb);
}

// Payloads that hold data for each card (the shape each renderer reads).
const DATA = {
  'lane-utilization': { lanes: [{ lane: 'lane-a', hours: [{ start: '2026-11-10T10:00:00Z', busy_fraction: 0.5 }] }] },
  'throughput-forecast': { points: [{ date: '2026-11-10', throughput_per_day: 1.5, completion_low_days: 2, completion_high_days: 4 }] },
  'verifier-health': { nodes: [{ node: 'local', runs: 3, waits: 1, wait_avg_seconds: 10, run_avg_seconds: 60, passes: 2, outcomes: 3, pass_rate: 0.66 }] },
  'release-train': { tags: [{ tag: 'v9.1.0', prd_id: 'p1' }], ci: [], ci_state: 'green', deploy: { state: 'ok' } },
  'review-backlog': { plans: [{ id: 'p1', title: 'Plan one', age_seconds: 7200 }], questions: [], plan_count: 1, question_count: 0 },
  'defects-per-day': { points: [{ date: '2026-11-10', found: 2, fixed: 1 }], total_found: 2, total_fixed: 1 },
};

// Payloads that hold nothing, so each card must stay off.
const EMPTY = {
  'lane-utilization': { lanes: [{ lane: 'lane-a', hours: [{ start: '2026-11-10T10:00:00Z', busy_fraction: 0 }] }] },
  'throughput-forecast': { points: [{ date: '2026-11-10', throughput_per_day: 0, completion_low_days: null, completion_high_days: null }] },
  'verifier-health': { nodes: [{ node: 'local', runs: 0, waits: 0 }] },
  'release-train': { tags: [], ci: [], ci_state: 'none', deploy: { state: 'none' } },
  'review-backlog': { plans: [], questions: [], plan_count: 0, question_count: 0 },
  'defects-per-day': { points: [], total_found: 0, total_fixed: 0 },
};

test('locale: every frame key exists in all five bundles', () => {
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const id of IDS) {
      for (const k of frameKeys(id)) {
        assert.ok(typeof b[k] === 'string' && b[k].length > 0, lang + ' is missing ' + k);
      }
    }
  }
});

test('locale: every card reason key exists in all five bundles', () => {
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const id of IDS) {
      const k = 'dash_card_off_' + CARDS[id].key + '_reason';
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, lang + ' is missing ' + k);
    }
  }
});

test('operator text: English frame text names no tracker ID and never says PRD', () => {
  const b = bundle('en');
  for (const id of IDS) {
    for (const k of frameKeys(id)) {
      assert.doesNotMatch(b[k], /\bPRD\b/, k + ' must say Automaton, not PRD');
      assert.doesNotMatch(b[k], /\b(BL|GH|SEC|HLLM)-?\d+\b|\bB\d{2,}\b/, k + ' must not carry a tracker ID');
    }
  }
});

test('card definitions: the six cards are addable and stay out of the default layout', () => {
  const sb = loadAppSandbox();
  const defs = evalIn(sb, 'DASH_CARD_DEFS.map(d => d.id)');
  const layout = evalIn(sb, 'DASH_DEFAULT_LAYOUT.map(d => d.id)');
  for (const id of IDS) {
    assert.ok(defs.includes(id), id + ' must be in DASH_CARD_DEFS (Edit → Add Card)');
    assert.ok(!layout.includes(id), id + ' must not be in DASH_DEFAULT_LAYOUT');
    const def = evalIn(sb, 'DASH_CARD_DEFS.find(d => d.id === ' + JSON.stringify(id) + ')');
    assert.equal(typeof def.body, 'function', id + ' needs a body');
    assert.ok(def.labelKey, id + ' needs a labelKey');
  }
});

test('feature gates: each card has a reason key and an active() gate', () => {
  const sb = loadAppSandbox();
  for (const id of IDS) {
    const feat = evalIn(sb, 'DASH_CARD_FEATURES[' + JSON.stringify(id) + ']');
    assert.ok(feat, id + ' needs a DASH_CARD_FEATURES entry');
    assert.equal(typeof feat.active, 'function', id + ' needs active()');
    assert.equal(feat.reasonKey, 'dash_card_off_' + CARDS[id].key + '_reason');
  }
});

test('feature gates: a card with data is active, one with an empty window is off', () => {
  const sb = loadAppSandbox();
  for (const id of IDS) {
    const slot = CARDS[id].slot;
    setSeries(sb, slot, DATA[id]);
    assert.equal(evalIn(sb, 'DASH_CARD_FEATURES[' + JSON.stringify(id) + '].active()'), true, id + ' with data should be active');
    setSeries(sb, slot, EMPTY[id]);
    assert.equal(evalIn(sb, 'DASH_CARD_FEATURES[' + JSON.stringify(id) + '].active()'), false, id + ' with an empty window should be off');
  }
});

test('frame: each card with data shows what it is for, its state and the next action', () => {
  const sb = loadAppSandbox();
  const en = bundle('en');
  for (const id of IDS) {
    setSeries(sb, CARDS[id].slot, DATA[id]);
    evalIn(sb, CARDS[id].render + '()');
    const html = sb.__els[CARDS[id].el].innerHTML;
    const k = CARDS[id].key;
    assert.ok(html.includes(escapeForHtml(en['dash_series_purpose_' + k])), id + ' should show its purpose');
    assert.ok(html.includes(escapeForHtml(en['dash_series_next_' + k])), id + ' should show the next action');
    assert.doesNotMatch(html, /%1\$d/, id + ' state line must have its count filled in');
  }
});

// A card whose window is empty is off: the grid shows one line with the reason
// (never the frame, never a blank card) via _dashCardBodySafe.
test('off: an empty card shows only its one-line reason, not the frame', () => {
  const sb = loadAppSandbox();
  const en = bundle('en');
  for (const id of IDS) {
    setSeries(sb, CARDS[id].slot, EMPTY[id]);
    const html = evalIn(sb, '_dashCardBodySafe(DASH_CARD_DEFS.find(d => d.id === ' + JSON.stringify(id) + '))');
    const k = CARDS[id].key;
    assert.ok(html.includes(escapeForHtml(en['dash_card_off_' + k + '_reason'])), id + ' should show its reason when off');
    assert.ok(!html.includes(escapeForHtml(en['dash_series_next_' + k])), id + ' should not show the frame when off');
  }
});

// The renderer's own empty branch (reached when the card element is drawn
// directly) keeps the purpose and next action, with the reason as the state.
test('frame: the empty branch keeps the purpose and next action around the reason', () => {
  const sb = loadAppSandbox();
  const en = bundle('en');
  for (const id of IDS) {
    setSeries(sb, CARDS[id].slot, { });
    evalIn(sb, CARDS[id].render + '()');
    const html = sb.__els[CARDS[id].el].innerHTML;
    const k = CARDS[id].key;
    assert.ok(html.includes(escapeForHtml(en['dash_series_purpose_' + k])), id + ' purpose on empty branch');
    assert.ok(html.includes(escapeForHtml(en['dash_series_next_' + k])), id + ' next action on empty branch');
    assert.ok(html.includes(escapeForHtml(en['dash_card_off_' + k + '_reason'])), id + ' reason on empty branch');
  }
});

test('frame: the state line carries the live count for the release train', () => {
  const sb = loadAppSandbox();
  setSeries(sb, '_seriesReleaseTrain', { tags: [{ tag: 'v1' }, { tag: 'v2' }], ci: [{ repo: 'r', workflow: 'w', status: 'green' }], ci_state: 'green', deploy: { state: 'ok' } });
  evalIn(sb, '_dashRenderReleaseTrain()');
  const html = sb.__els.dashReleaseTrainCard.innerHTML;
  assert.ok(html.includes('2 tags and 1 watched CI runs.'), 'state should read 2 tags and 1 watched CI runs, got: ' + html.slice(0, 200));
});

test('frame: the defects card state line reports found and fixed totals', () => {
  const sb = loadAppSandbox();
  setSeries(sb, '_seriesDefects', DATA['defects-per-day']);
  evalIn(sb, '_dashRenderDefects()');
  const html = sb.__els.dashDefectsCard.innerHTML;
  assert.ok(html.includes('2 found and 1 fixed in the last 14 days.'), 'state line should carry the totals');
});

// Mirrors escHtml for the English strings the frame prints (no quotes or
// ampersands appear in them, so only the apostrophe and angle brackets matter).
function escapeForHtml(s) {
  return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}
