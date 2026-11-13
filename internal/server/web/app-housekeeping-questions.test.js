// Housekeeping open-questions card: the Housekeeping sub-tab shows the same
// live open-question list as the Inbox card — same route (GET
// /api/questions?state=open&limit=50) through the shared fetch core
// (_automationQuestionsFetchCore), the same scope cut and the same row
// renderer (both cards paint _automationQuestionsOpenBody) — under its own
// unique element ids (automationHkQuestionsCard / automationHkQuestionsBody)
// and its own list loader (key automation_hk_questions), registered as a
// Housekeeping card slot below the repairs and Unlanded work cards. The Inbox
// search box lives only in the Inbox card; the mirror's fetch passes '' so its
// text never diverges from the Inbox card's. Entering the sub-tab starts the
// mirror loader, leaving it stops it (the slot stop path). The live
// question_raised refresh refreshes both cards.
//
// Like the sibling question tests this is a plain node:test script (no test
// framework, no package.json): app.js runs unmodified in a vm context with
// the shared browser stub, and fetch is replaced per test.
const test = require('node:test');
const path = require('path');
const fs = require('fs');
const assert = require('node:assert');
const vmmod = require('vm');
const { buildSandbox, loadScript, flushAsync } = require(path.join(__dirname, 'testutil_browser_stub.js'));

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES = path.join(__dirname, 'locales');
const readLocale = (l) => JSON.parse(fs.readFileSync(path.join(LOCALES, l + '.json'), 'utf8'));
const en = readLocale('en');

// loadApp: app.js in a fresh browser stub, with the English bundle so the
// rendered strings are the real user-visible ones.
const loadApp = () => {
  const sandbox = buildSandbox();
  vmmod.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = en;
  return sandbox;
};
const RUN = (sb, code) => vmmod.runInContext(code, sb);

// Two open questions from two different projects. Matching is by the project
// directory fallback of the shared inbox scope filter (no project: system
// tags), so the scope test does not depend on any Automata list state.
const qA = { id: 'q-a', label: 'Which GPU?', text: 'The GPU\nlong question text\nmore\nlines', asked_by: 'automaton:worker-7', created_at: new Date(Date.now() - 3 * 60 * 1000).toISOString() };
const qB = { id: 'q-b', label: 'Which dataset?', text: 'the dataset question', asked_by: 'automaton:worker-8', created_at: new Date(Date.now() - 25 * 60 * 1000).toISOString(), project_dir: '/w/beta' };
const LIST_TWO = { count: 2, questions: [Object.assign({ project_dir: '/w/alpha' }, qA), Object.assign({}, qB)] };
// paintedBody strips the mirror card's own chrome (the painted body div and
// its wrapper) so the byte comparison reaches the shared rows; bodies that
// have no body div (none) pass through unchanged.
const HK_BODY_TAG = '<div id="automationHkQuestionsBody" class="questions-body">';
// paintedBody strips the mirror card's own chrome (the painted body div and
// its wrapper) so the byte comparison reaches the shared rows; bodies that
// have no body div (none) pass through unchanged. The body div can contain
// nested divs, so the closing tag is matched by depth, not by occurrence.
const paintedBody = (rendered) => {
  const s = String(rendered);
  const i = s.indexOf(HK_BODY_TAG);
  if (i === -1) return s;
  let depth = 1, j = i + HK_BODY_TAG.length;
  while (j < s.length) {
    if (s.startsWith('<div', j)) { depth++; j += 4; }
    else if (s.startsWith('</div>', j)) { if (--depth === 0) break; j += 5; }
    else j++;
  }
  return j >= s.length ? s.slice(i + HK_BODY_TAG.length) : s.slice(i + HK_BODY_TAG.length, j);
};

const okList = (body) => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) });
// Fetch stub: records every requests questions list, answers it with 'body',
// and answers everything else with the generic empty ok body the renderers
// across the app tolerate (the subtab switch test relies on this too).
const recFetch = (rec, body) => (url) => {
  const u = String(url);
  rec.push(u);
  if (u.indexOf('/api/questions?state=open') === 0) return okList(body);
  return okList({});
};

test('housekeeping open-questions card', () => {
  test('the Housekeeping body holds the card shell with its unique id and none of the Inbox ids', () => {
    const sb = loadApp();
    RUN(sb, "_automationState.subtab = 'housekeeping'");
    const body = RUN(sb, '_automationBodyHTML()');
    // The card follows the other Housekeeping slot cards: it renders as the
    // hidden empty placeholder shell (a live body id appears only once its
    // loader paints the list in).
    assert.match(body, /id="automationHkQuestionsCard"/, 'the housekeeping body holds the card shell');
    assert.doesNotMatch(body, /housekeeping_questions_card_title|housekeeping_questions_help_link/, 'no raw keys');
    assert.doesNotMatch(body, /id="automationQuestionsCard"/, 'no Inbox card id');
    assert.doesNotMatch(body, /id="automationQuestionsBody"/, 'no Inbox body id');
    assert.doesNotMatch(body, /automationQuestionsSearch/, 'no search box in the mirror (the search is Inbox-only)');
    assert.doesNotMatch(body, /automationHkQuestionsBody/, 'the unique body id is painted by the loader, not in the shell');
    assert.ok(body.indexOf('automationHousekeepingRepairsCard') !== -1
      && body.indexOf('automationHkQuestionsCard') > body.indexOf('automationHousekeepingRepairsCard'), 'below the repairs card');
    assert.ok(body.indexOf('automationStaleWorktreesCard') !== -1
      && body.indexOf('automationHkQuestionsCard') > body.indexOf('automationStaleWorktreesCard'), 'below the Unlanded work card');
    for (const other of ['inbox', 'automations', 'resources', 'progress']) {
      RUN(sb, "_automationState.subtab = '" + other + "'");
      const b2 = RUN(sb, '_automationBodyHTML()');
      assert.doesNotMatch(b2, /automationHkQuestions/, other + ' body holds no mirror');
    }
    RUN(sb, "_automationState.subtab = 'inbox'");
    const inboxBody = RUN(sb, '_automationBodyHTML()');
    assert.ok(inboxBody.indexOf('automationQuestionsSearch') !== -1, 'the Inbox body carries the search box');
    assert.doesNotMatch(inboxBody, /automationHkQuestions/, 'no mirror on the Inbox body');
    RUN(sb, "switchAutomationSubtab('housekeeping')");
    assert.equal(RUN(sb, "_automationState.subtab"), 'housekeeping', 'the switch landed on housekeeping');
    const after = RUN(sb, '_automationBodyHTML()');
    assert.match(after, /id="automationHkQuestionsCard"/, 'the switched-in view renders the card shell');
    // Once the loader paints the list, the card carries its unique body id,
    // the translated title and the help link, and reveals itself.
    const paint = sb._automationHkQuestionsCardHtml({ count: 1, questions: [qA] });
    assert.match(paint, /id="automationHkQuestionsBody"/, 'painted card carries the unique body id');
    assert.match(paint, new RegExp(en.housekeeping_questions_card_title), 'painted card has the translated title, not the raw key');
    assert.ok(paint.indexOf('housekeeping_questions_card_title') === -1, 'painted card keeps no raw key');
    assert.ok(paint.indexOf('housekeeping-help') !== -1, 'painted card keeps the help link');
    assert.ok(paint.indexOf('automationQuestionsCard') === -1 && paint.indexOf('automationQuestionsBody') === -1 && paint.indexOf('automationQuestionsSearch') === -1, 'painted card reuses no Inbox id or search box');
  });

  test('the card slot is registered on housekeeping only, at order 110, with its own mount and stop', () => {
    const sb = loadApp();
    const ids = JSON.parse(RUN(sb, "JSON.stringify(_AUTOMATION_CARD_SLOTS.housekeeping.map(s => [s.id, s.order]))"));
    assert.ok(ids.some(x => x[0] === 'automationHkQuestionsCard' && x[1] === 110), 'housekeeping slot at order 110: ' + JSON.stringify(ids));
    const slot = RUN(sb, "_AUTOMATION_CARD_SLOTS.housekeeping.find(s => s.id === 'automationHkQuestionsCard')");
    assert.equal(typeof slot.mount, 'function', 'the slot starts its own loader on mount');
    assert.equal(typeof slot.stop, 'function', 'the slot stops its own loader on leave');
    // No other sub-tab registers the id.
    const others = JSON.parse(RUN(sb, "JSON.stringify(Object.keys(_AUTOMATION_CARD_SLOTS).filter(k => k !== 'housekeeping').map(k => _AUTOMATION_CARD_SLOTS[k].map(s => s.id)))"));
    for (const list of others) assert.ok(list.indexOf('automationHkQuestionsCard') === -1, 'no other tab uses the id: ' + JSON.stringify(others));
  });

  test('same API body: the Housekeeping body is byte-identical to the Inbox body in the overview and in a project scope', () => {
    const sb = loadApp();
    const inboxCtl = RUN(sb, '_automationQuestionsLoader()');
    const hkCtl = RUN(sb, '_automationHkQuestionsLoader()');
    try {
      assert.notEqual(hkCtl, inboxCtl, 'two distinct loader instances');
      assert.equal(hkCtl.state.opts.url, inboxCtl.state.opts.url, 'same list route and limit');
      assert.equal(hkCtl.state.opts.url, '/api/questions?state=open&limit=50', 'the open list route');
      // Both loaders paint through the one shared scope cut + renderer: the
      // Housekeeping render's painted body (its title row above and body id
      // around are the mirror's own chrome) is byte-identical to the Inbox
      // body for the same API body.
      const shared = (body) => sb._automationQuestionsOpenBody(body);
      const battery = [
        [LIST_TWO, 'the live list'],
        [{ count: 1, questions: [qA] }, 'a single question'],
        [{ count: 0, questions: [] }, 'the empty state'],
        [{ __automationQuestionsDisabled: true }, 'the 503 disabled sentinel'],
      ];
      for (const [body, label] of battery) {
        const want = shared(body);
        assert.equal(inboxCtl.state.opts.render(body), want, label + ': Inbox body is the shared cut + renderer');
        assert.equal(paintedBody(hkCtl.state.opts.render(body)), want, label + ': Housekeeping body is byte-identical');
      }
      // Sanity: the list battery really painted both rows (not an empty
      // fallback), and the sentinel battery really painted the disabled text.
      assert.ok(shared(LIST_TWO).indexOf('q-a') !== -1 && shared(LIST_TWO).indexOf('q-b') !== -1, 'both rows in the live list');
      assert.ok(shared({ count: 0, questions: [] }).indexOf(en.automation_questions_empty) !== -1, 'empty text');
      assert.ok(shared({ __automationQuestionsDisabled: true }).indexOf(en.automation_questions_disabled) !== -1, 'disabled text');
    } finally {
      inboxCtl.stop();
      hkCtl.stop();
    }
  });

  test('one project selected: the shared scope cut applies to both sub-tabs', () => {
    const sb = loadApp();
    const inboxCtl = RUN(sb, '_automationQuestionsLoader()');
    const hkCtl = RUN(sb, '_automationHkQuestionsLoader()');
    try {
      // Project scope active: the Projects tab is shown and alpha is selected.
      RUN(sb, "state.activeView = 'autonomous'");
      RUN(sb, "_automataState.tab = 'projects'");
      RUN(sb, "_projectsState.list = [{ name: 'alpha', dir: '/w/alpha' }]");
      RUN(sb, "_projectsState.selected = 'alpha'");
      try {
        const scoped = sb._automationQuestionsOpenBody(LIST_TWO);
        assert.ok(scoped.indexOf('q-a') !== -1, 'the selected project\x27s question is shown');
        assert.ok(scoped.indexOf('q-b') === -1, 'the other project\x27s question is not');
        assert.equal(inboxCtl.state.opts.render(LIST_TWO), scoped, 'Inbox scoped body identical to the shared cut');
        assert.equal(paintedBody(hkCtl.state.opts.render(LIST_TWO)), scoped, 'Housekeeping scoped body identical to the shared cut');
      } finally {
        RUN(sb, "state.activeView = 'sessions'");
        RUN(sb, "_automataState.tab = 'automation'");
        RUN(sb, "_projectsState.selected = ''");
      }
      // Overview again: both questions in both bodies.
      const all = sb._automationQuestionsOpenBody(LIST_TWO);
      assert.ok(all.indexOf('q-a') !== -1 && all.indexOf('q-b') !== -1, 'overview shows both');
      assert.equal(inboxCtl.state.opts.render(LIST_TWO), all, 'Inbox overview body identical');
      assert.equal(paintedBody(hkCtl.state.opts.render(LIST_TWO)), all, 'Housekeeping overview body identical');
    } finally {
      inboxCtl.stop();
      hkCtl.stop();
    }
  });

  test('the search is Inbox-only: the mirror\x27s fetch never carries the Inbox search text, the Inbox\x27s does', async () => {
    const sb = loadApp();
    const rec = [];
    sb.fetch = recFetch(rec, LIST_TWO);
    const inboxCtl = RUN(sb, '_automationQuestionsLoader()');
    const hkCtl = RUN(sb, '_automationHkQuestionsLoader()');
    try {
      // Housekeeping first, with no search text anywhere.
      rec.length = 0;
      await hkCtl.load();
      assert.equal(rec.filter(u => u.indexOf('/api/questions?state=open') === 0).length, 1, 'one queue request from the mirror');
      assert.ok(rec.indexOf('/api/questions?state=open&limit=50') !== -1, 'the mirror asks for the whole list, without any search text: ' + JSON.stringify(rec));
      // Now the Inbox card searches.
      sb._automationQuestionsState.searchText = 'gpu';
      rec.length = 0;
      await inboxCtl.load();
      assert.ok(rec.indexOf('/api/questions?state=open&limit=50&text=gpu') !== -1, 'the Inbox request rides the search text');
      // ...and the mirror still never does, even with the text set.
      rec.length = 0;
      await hkCtl.load();
      assert.ok(rec.every(u => u.indexOf('&text=') === -1), 'the mirror asks for the whole live list while the Inbox searches: ' + JSON.stringify(rec));
    } finally {
      sb._automationQuestionsState.searchText = '';
      inboxCtl.stop();
      hkCtl.stop();
    }
  });

  test('entering Housekeeping starts its own loader; leaving stops it, and it is a distinct instance from the Inbox loader', async () => {
    const sb = loadApp();
    const rec = [];
    sb.fetch = recFetch(rec, LIST_TWO);
    // A fresh app has not mounted either sub-tab's loaders yet: the mirror
    // loader does not exist (same unmounted property the live test asserts
    // for the Inbox loader).
    assert.equal(RUN(sb, 'window._listLoaders.automation_hk_questions'), undefined, 'no mirror loader before Housekeeping is mounted');
    // Enter the sub-tab the way a user does.
    RUN(sb, "switchAutomationSubtab('housekeeping')");
    await flushAsync(10);
    const hkCtl = RUN(sb, 'window._listLoaders.automation_hk_questions');
    assert.ok(hkCtl, 'the mirror loader is registered while Housekeeping is mounted');
    assert.equal(hkCtl.state.stopped, false, 'the mirror loader is live while its sub-tab is shown');
    assert.ok(rec.filter(u => u.indexOf('/api/questions?state=open') === 0).length >= 1, 'the mirror fetched the open list on entry');
    // Leave it again.
    RUN(sb, "switchAutomationSubtab('inbox')");
    await flushAsync(10);
    assert.equal(hkCtl.state.stopped, true, 'leaving the sub-tab stops the mirror loader');
    // The Inbox card got its own distinct loader instance on mount.
    const inCtl = RUN(sb, 'window._listLoaders.automation_questions');
    assert.ok(inCtl, 'the Inbox loader exists after mounting Inbox');
    assert.notEqual(inCtl, hkCtl, 'two distinct loader instances, one per card');
    assert.equal(inCtl.state.stopped, false, 'the Inbox loader is live while its sub-tab is shown');
  });

  test('the live question_raised refresh refreshes the Inbox card and the Housekeeping mirror', async () => {
    // One live event in a fresh sandbox whose fetch records every open-list
    // request; returns the count after the handler ran and settled.
    const trigger = async (sb, rec) => {
      rec.length = 0;
      sb._frame = { type: 'question_raised', data: { id: 'q-live', label: 'Which GPU?', text: 'x' } };
      vmmod.runInContext('handleMessage(_frame)', sb);
      await flushAsync(8);
      return rec.filter(u => u.indexOf('/api/questions?state=open') === 0).length;
    };
    // Both cards mounted: exactly one refresh per card, one toast.
    {
      const sb = loadApp();
      const rec = [];
      sb.fetch = recFetch(rec, LIST_TWO);
      RUN(sb, '_automationQuestionsLoader()');
      RUN(sb, '_automationHkQuestionsLoader()');
      const toasts = sb._alertDock.alerts.length;
      assert.equal(await trigger(sb, rec), 2, 'one refresh per mounted card: ' + JSON.stringify(rec));
      assert.equal(sb._alertDock.alerts.length, toasts + 1, 'one live toast');
    }
    // Inbox-only (the mirror is not mounted): exactly one refresh.
    {
      const sb = loadApp();
      const rec = [];
      sb.fetch = recFetch(rec, LIST_TWO);
      RUN(sb, '_automationQuestionsLoader()');
      assert.equal(await trigger(sb, rec), 1, 'the lone Inbox card gets exactly one refresh: ' + JSON.stringify(rec));
    }
    // Neither card mounted: no fetch at all, the toast still lands.
    {
      const sb = loadApp();
      const rec = [];
      sb.fetch = recFetch(rec, LIST_TWO);
      const toasts = sb._alertDock.alerts.length;
      assert.equal(await trigger(sb, rec), 0, 'no mounted card, no queue request');
      assert.equal(sb._alertDock.alerts.length, toasts + 1, 'the live toast still lands');
    }
  });

  test('locale keys exist in all five bundles, without PRD wording or tracker ids', () => {
    const KEYS = ['housekeeping_questions_card_title', 'housekeeping_questions_help_link'];
    for (const l of ['en', 'de', 'es', 'fr', 'ja']) {
      const b = readLocale(l);
      for (const k of KEYS) {
        assert.ok(typeof b[k] === 'string' && b[k].length > 0, l + ' has ' + k);
        assert.ok(!/PRD/i.test(b[k]), l + ': no PRD wording in ' + k);
        assert.ok(!/#\d{3,}/.test(b[k]), l + ': no tracker id in ' + k);
      }
      if (l !== 'en') for (const k of KEYS) assert.notEqual(b[k], en[k], l + ': ' + k + ' is a translation, not a copy of en');
    }
  });

  test('the help key resolves through the docs registry to the Automation tab page', () => {
    const sb = loadApp();
    const target = RUN(sb, "docsLinkForKey('housekeeping_questions_card')");
    assert.ok(target, 'the docs registry resolves the key: ' + JSON.stringify(target));
    assert.ok(target.indexOf('automation-tab.md') !== -1, 'it points at the Automation tab page: ' + target);
    // The card's help link renders through the same registry helper with the
    // housekeeping card's help sentence.
    const link = RUN(sb, "_housekeepingHelpLink('housekeeping_questions_card', 'housekeeping_questions_help_link')");
    assert.ok(link.indexOf('/diagrams.html#docs/') !== -1, 'the rendered link keeps the diagrams docs route: ' + link);
    assert.ok(link.indexOf('automation-tab.md') !== -1, 'the rendered link keeps the registry target: ' + link);
    for (const needle of ['docs-link', 'housekeeping-help', 'href=', 'target="_blank"', 'rel="noopener noreferrer"', 'onclick="event.stopPropagation()"', 'class=']) {
      assert.ok(link.indexOf(needle) !== -1, 'the rendered link keeps \u201c' + needle + '\u201d');
    }
    assert.ok(link.indexOf('title="' + en.housekeeping_questions_help_link + '"') !== -1, 'the help sentence is the link\u2019s title: ' + link);
  });
});
