// Dashboard series cards — release train (tags, CI and deploy state), review
// backlog (plans and held questions with their age) and defects per day
// (found and fixed per day). Covers the DASH_CARD_FEATURES gates that keep each
// card off until its window holds something, the render output and escaping,
// the card definitions staying out of the default layout, and the locale keys
// in all five bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

const LOCALE_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const CARD_IDS = ['release-train', 'review-backlog', 'defects-per-day'];
const SERIES_MORE_KEYS = [
  'dash_release_train', 'dash_review_backlog', 'dash_defects_per_day',
  'dash_card_off_release_train_reason', 'dash_card_off_review_backlog_reason', 'dash_card_off_defects_reason',
  'dash_series_tags', 'dash_series_ci_state', 'dash_series_deploy_state', 'dash_series_ci_runs',
  'dash_series_repo', 'dash_series_plans', 'dash_series_questions', 'dash_series_title',
  'dash_series_age', 'dash_series_found', 'dash_series_fixed', 'dash_series_total',
  'dash_series_status', 'dash_series_age_hours',
  'dash_series_state_failing', 'dash_series_state_pending', 'dash_series_state_green',
  'dash_series_state_ok', 'dash_series_state_failed', 'dash_series_state_none',
  'dash_series_date', 'dash_series_none',
];

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, lang + '.json'), 'utf8'));
}

function loadAppSandbox() {
  const els = {
    dashReleaseTrainCard: makeStubElement(),
    dashReviewBacklogCard: makeStubElement(),
    dashDefectsCard: makeStubElement(),
  };
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

const EMPTY_RELEASE = { tags: [], ci: [], ci_state: 'none', deploy: { state: 'none' } };
const EMPTY_BACKLOG = { plan_count: 0, question_count: 0, plans: [], questions: [] };
const EMPTY_DEFECTS = { points: [], total_found: 0, total_fixed: 0 };

test('locale: every new series key exists in all five bundles', () => {
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of SERIES_MORE_KEYS) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, lang + ' is missing ' + k);
    }
  }
});

test('card definitions: the three cards are addable but not in the default layout', () => {
  const sb = loadAppSandbox();
  const defs = vm.runInContext('DASH_CARD_DEFS', sb).map(d => d.id);
  const layout = vm.runInContext('DASH_DEFAULT_LAYOUT', sb).map(d => d.id);
  for (const id of CARD_IDS) {
    assert.ok(defs.includes(id), id + ' should be in the card picker');
    assert.ok(!layout.includes(id), id + ' must not be in the default layout');
  }
});

test('feature gate: release train is off with nothing recorded and on once a tag, run or deploy exists', () => {
  const sb = loadAppSandbox();
  const feat = vm.runInContext('DASH_CARD_FEATURES', sb)['release-train'];
  assert.equal(feat.active(), false, 'no payload yet');
  setSeries(sb, '_seriesReleaseTrain', EMPTY_RELEASE);
  assert.equal(feat.active(), false, 'an empty window is not shown');
  setSeries(sb, '_seriesReleaseTrain', { ...EMPTY_RELEASE, deploy: { state: 'failed' } });
  assert.equal(feat.active(), true, 'a failed deploy shows the card');
  setSeries(sb, '_seriesReleaseTrain', { ...EMPTY_RELEASE, tags: [{ tag: 'v1.2.3', ok: true }] });
  assert.equal(feat.active(), true, 'a cut tag shows the card');
});

test('feature gate: review backlog is off with nothing waiting and on once a plan or question waits', () => {
  const sb = loadAppSandbox();
  const feat = vm.runInContext('DASH_CARD_FEATURES', sb)['review-backlog'];
  assert.equal(feat.active(), false, 'no payload yet');
  setSeries(sb, '_seriesReviewBacklog', EMPTY_BACKLOG);
  assert.equal(feat.active(), false, 'nothing waiting is not shown');
  setSeries(sb, '_seriesReviewBacklog', { ...EMPTY_BACKLOG, question_count: 1, questions: [{ question_id: 'q1', age_seconds: 7200 }] });
  assert.equal(feat.active(), true, 'a held question shows the card');
});

test('feature gate: defects per day is off with no defects and on once one is found or fixed', () => {
  const sb = loadAppSandbox();
  const feat = vm.runInContext('DASH_CARD_FEATURES', sb)['defects-per-day'];
  assert.equal(feat.active(), false, 'no payload yet');
  setSeries(sb, '_seriesDefects', EMPTY_DEFECTS);
  assert.equal(feat.active(), false, 'no defects is not shown');
  setSeries(sb, '_seriesDefects', { points: [{ date: '2026-11-10', found: 2, fixed: 0 }], total_found: 2, total_fixed: 0 });
  assert.equal(feat.active(), true, 'a found defect shows the card');
});

test('feature gates: each new gate names locale keys that resolve in English', () => {
  const sb = loadAppSandbox();
  const en = bundle('en');
  const feats = vm.runInContext('DASH_CARD_FEATURES', sb);
  for (const id of CARD_IDS) {
    assert.ok(en[feats[id].reasonKey], id + ' reason key ' + feats[id].reasonKey + ' missing in en');
    assert.ok(en[feats[id].linkKey], id + ' link key missing in en');
  }
});

test('render: release train shows the CI and deploy states and escapes the repo name', () => {
  const sb = loadAppSandbox();
  setSeries(sb, '_seriesReleaseTrain', {
    tags: [{ tag: 'v1.2.3', prd_id: '<p1>', ok: true }],
    ci: [{ repo: 'acme/<app>', workflow: 'ci.yml', status: 'failing' }],
    ci_state: 'failing',
    deploy: { state: 'ok' },
  });
  vm.runInContext('_dashRenderReleaseTrain()', sb);
  const html = sb.__els.dashReleaseTrainCard.innerHTML;
  assert.ok(html.includes('v1.2.3'), 'the tag is shown');
  assert.ok(html.includes('failing'), 'the CI state is shown in English');
  assert.ok(html.includes('ok'), 'the deploy state is shown');
  assert.ok(html.includes('acme/&lt;app&gt;'), 'the repo name is escaped');
  assert.ok(!html.includes('<app>'), 'no raw markup from the repo name');
  assert.ok(!html.includes('<p1>'), 'the tag tooltip is escaped');
});

test('render: review backlog lists plans and questions with their age in hours', () => {
  const sb = loadAppSandbox();
  setSeries(sb, '_seriesReviewBacklog', {
    plan_count: 1, question_count: 1,
    plans: [{ id: 'pl1', title: 'Ship <beta>', age_seconds: 5400 }],
    questions: [{ question_id: 'q1', title: 'Pick a lane', age_seconds: 7300 }],
  });
  vm.runInContext('_dashRenderReviewBacklog()', sb);
  const html = sb.__els.dashReviewBacklogCard.innerHTML;
  assert.ok(html.includes('Ship &lt;beta&gt;'), 'the plan title is escaped');
  assert.ok(html.includes('Pick a lane'), 'the question title is shown');
  assert.ok(html.includes('1 h'), 'the age is shown in whole hours');
  assert.ok(html.includes('Held questions') && html.includes('Plans waiting'), 'both sections are labelled');
});

test('render: defects per day shows the totals and one row per day', () => {
  const sb = loadAppSandbox();
  setSeries(sb, '_seriesDefects', {
    points: [{ date: '2026-11-09', found: 3, fixed: 1 }, { date: '2026-11-10', found: 0, fixed: 2 }],
    total_found: 3, total_fixed: 3,
  });
  vm.runInContext('_dashRenderDefects()', sb);
  const html = sb.__els.dashDefectsCard.innerHTML;
  assert.ok(html.includes('3 / 3'), 'the totals are shown');
  assert.ok(html.includes('2026-11-09') && html.includes('2026-11-10'), 'each day is a row');
});

test('render: an inactive series card renders its reason, never the table', () => {
  const sb = loadAppSandbox();
  setSeries(sb, '_seriesDefects', EMPTY_DEFECTS);
  vm.runInContext('_dashRenderDefects()', sb);
  const html = sb.__els.dashDefectsCard.innerHTML;
  assert.ok(html.includes(bundle('en').dash_card_off_defects_reason), 'the English reason is rendered');
  assert.ok(!html.includes('<table'), 'no table for an empty window');
});
