// v1 question-queue tests — the open-questions card in the Automation view.
//
// The card is its own listLoader on GET /api/questions?state=open&limit=50,
// polled like the capacity / lane fill / triage cards. Every row shows the
// question id, the label (when set), the visible high-priority marker,
// the escaped question text, the asker and the age; rows keep the route's
// order. The card is read-only (never POSTs): a 503 means the questions
// switch is off and the card paints only the translated disabled text. The
// row button hands off to window.openQuestion(id), which the answer form
// task writes — the inline call is guarded so the card still works while
// that function is absent. A search re-requests the list with the text
// parameter (on Enter or after a short input pause, never per keystroke).
//
// The Go-side guard (v8_73_25_appjs_locale_guard_test.go) already asserts
// that every t('...') key app.js uses exists in en.json; the per-key bundle
// tests below cover the cross-bundle properties that need the locale files
// read in — keys present in all five bundles, translations that are not
// English copies, and placeholder consistency.
const test = require('node:test');

test('v1 question-queue: card renders open questions with escaped hostile text', async () => {
  const path = require('path');
  const assert = require('node:assert');
  const { buildSandbox, loadScript, flushAsync } = require(path.join(__dirname, 'testutil_browser_stub.js'));
  const vmmod = require('vm');

  const APP_JS = path.join(__dirname, 'app.js');
  const LOCALES_DIR = path.join(__dirname, 'locales');
  const bundle = (lang) => JSON.parse(require('fs').readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
  const en = bundle('en');
  const de = bundle('de');

  const HOSTILE = '<img src=x onerror=alert(1)>';
  const q = (extra) => Object.assign({
    id: 'q1',
    label: 'Which GPU?',
    asked_by: 'automaton:worker-7',
    text: 'Which GPU should I use for the fine-tune run?',
    options: ['B200', 'H100'],
    created_at: new Date(Date.now() - 5 * 60 * 1000).toISOString(),
    state: 'open',
    priority: '',
    return_path: '/prd/1',
  }, extra);

  const loadApp = () => {
    const sandbox = buildSandbox();
    vmmod.createContext(sandbox); // buildSandbox returns a plain object; runInContext needs a context
    loadScript(APP_JS, sandbox);
    sandbox._i18n.bundle = en;
    return sandbox;
  };

  const okList = (qs) => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ count: qs.length, questions: qs }) });

  const sandbox = loadApp();
  // runInContext code cannot see host locals — expose the fixture as a
  // sandbox property (a global inside the context).
  sandbox._tqBody = { count: 2, questions: [q({ id: 'q2', text: HOSTILE }), q({ id: 'q1', asked_by: HOSTILE, label: HOSTILE })] };

  test('row markup: id, label, text, asked_by, age and hand-off button are all present', () => {
    const html = vmmod.runInContext('window.questionsCardHtml(_tqBody)', sandbox);
    assert.ok(html.includes('data-question-id="q1"'), 'row for q1: ' + html.slice(0, 400));
    assert.ok(html.includes('data-question-id="q2"'), 'row for q2: ' + html.slice(0, 400));
    assert.ok(html.includes('questions-text'), 'question text block rendered');
    assert.ok(html.includes('Which GPU?'), 'label rendered');
    assert.ok(html.includes('automaton:worker-7'), 'asked_by rendered');
    assert.ok(new RegExp('asked 5m ago').test(html), 'age from created_at rendered: ' + html.slice(0, 600));
    assert.ok(html.includes("if (typeof window.openQuestion === 'function') window.openQuestion('q1')"),
      'row button hands off to window.openQuestion(id)');
    assert.ok(html.includes("if (typeof window.openQuestion === 'function') window.openQuestion('q2')"),
      'second row button hands off to window.openQuestion(id)');
  });

  test('hostile values are escaped, never raw HTML', () => {
    const html = vmmod.runInContext('window.questionsCardHtml(_tqBody)', sandbox);
    assert.ok(!html.includes('<img src=x onerror=alert(1)>'), 'raw hostile HTML must never be spliced in: ' + html.slice(0, 600));
    assert.ok(html.includes('&lt;img src=x onerror=alert(1)&gt;'), 'hostile text shows up escaped');
  });

  test('rows keep the route order (no client-side reordering)', () => {
    const html = vmmod.runInContext('window.questionsCardHtml(_tqBody)', sandbox);
    const i2 = html.indexOf('data-question-id="q2"');
    const i1 = html.indexOf('data-question-id="q1"');
    assert.ok(i2 !== -1 && i1 !== -1, 'both rows present');
    assert.ok(i2 < i1, 'route order preserved (q2 before q1)');
  });

  test('high-priority marker is rendered only for priority "high"', () => {
    const high = vmmod.runInContext("window.questionsCardHtml({ count: 1, questions: [ Object.assign({}, _tqBody.questions[0], { priority: 'high' }) ] })", sandbox);
    assert.ok(high.includes('questions-priority-high'), 'high row carries the marker');
    assert.ok(high.includes(en.automation_questions_high), 'marker text is the translated high word');
    const normal = vmmod.runInContext("window.questionsCardHtml({ count: 1, questions: [ Object.assign({}, _tqBody.questions[0], { priority: 'normal' }) ] })", sandbox);
    assert.ok(!normal.includes('questions-priority-high'), 'normal row must not carry the marker');
  });

  test('empty result renders the translated empty text', () => {
    const html = vmmod.runInContext('window.questionsCardHtml({ count: 0, questions: [] })', sandbox);
    assert.ok(html, 'empty state html not empty');
    assert.ok(html.includes(en.automation_questions_empty), 'empty text rendered: ' + html);
    assert.ok(!html.includes('questions-queue-row'), 'no rows in the empty state');
  });

  test('503 renders only the translated disabled text and never POSTs', async () => {
    const s2 = loadApp();
    const calls = [];
    s2.fetch = (url, o) => {
      calls.push({ url: String(url), method: (o && o.method) || 'GET' });
      return Promise.resolve({ ok: false, status: 503, text: () => Promise.resolve('operator questions are disabled') });
    };
    const ctl = vmmod.runInContext('_automationQuestionsLoader()', s2);
    await ctl.load();
    const html = ctl.html();
    assert.equal(
      html,
      '<div style="color:var(--text2);">' + en.automation_questions_disabled + '</div>',
      'disabled state shows exactly the disabled text, nothing else: ' + html
    );
    assert.ok(calls.length >= 1, 'the disabled state came from a GET');
    assert.ok(calls.every(c => c.method === 'GET'), 'the card never POSTs: ' + JSON.stringify(calls));
    ctl.stop();
  });

  test('503 disabled text is translated (de bundle)', async () => {
    const s3 = buildSandbox();
    vmmod.createContext(s3);
    loadScript(APP_JS, s3);
    s3._i18n.bundle = de;
    s3.fetch = () => Promise.resolve({ ok: false, status: 503, text: () => Promise.resolve('off') });
    const ctl = vmmod.runInContext('_automationQuestionsLoader()', s3);
    await ctl.load();
    assert.ok(ctl.html().includes(de.automation_questions_disabled), 'de disabled text rendered: ' + ctl.html());
    ctl.stop();
  });

  test('loader: key, url and poll interval match the other Automation cards', () => {
    const s4 = loadApp();
    const ctl = vmmod.runInContext('_automationQuestionsLoader()', s4);
    assert.equal(ctl.state.key, 'automation_questions');
    assert.equal(ctl.state.opts.url, '/api/questions?state=open&limit=50');
    assert.equal(ctl.state.opts.pollMs, 5000);
    ctl.stop();
  });

  test('first request is exactly the open-questions list, no text param', async () => {
    const s5 = loadApp();
    const urls = [];
    s5.fetch = (url) => { urls.push(String(url)); return okList([q({})]); };
    const ctl = vmmod.runInContext('_automationQuestionsLoader()', s5);
    await ctl.load();
    assert.ok(ctl.html().includes('data-question-id="q1"'), 'row rendered from the live list');
    assert.ok(urls.includes('/api/questions?state=open&limit=50'), 'exact base url requested: ' + JSON.stringify(urls));
    ctl.stop();
  });

  test('Enter commits the search and re-requests the list with the text param', async () => {
    const s6 = buildSandbox({ byId: { automationQuestionsSearch: { value: 'gpu' } } });
    vmmod.createContext(s6);
    loadScript(APP_JS, s6);
    s6._i18n.bundle = en;
    const urls = [];
    s6.fetch = (url, o) => { urls.push(String(url)); return okList([q({})]); };
    const ctl = vmmod.runInContext('_automationQuestionsLoader()', s6);
    await ctl.load();
    const before = urls.length;
    vmmod.runInContext("onAutomationQuestionsSearchKeydown({ key: 'Enter' })", s6);
    await new Promise(r => setImmediate(r));
    await flushAsync(6);
    assert.ok(urls.length > before, 'Enter committed a new request');
    assert.ok(urls[before].includes('/api/questions?state=open&limit=50&text=gpu'),
      'search re-request carries the text param: ' + JSON.stringify(urls));
    ctl.stop();
  });

  test('input re-requests after the pause, not on every keystroke', async () => {
    const s7 = buildSandbox();
    vmmod.createContext(s7);
    loadScript(APP_JS, s7);
    s7._i18n.bundle = en;
    const urls = [];
    s7.fetch = (url, o) => { urls.push(String(url)); return okList([q({})]); };
    s7.setTimeout = (fn, ms) => setTimeout(fn, Math.min(ms, 300));
    s7.clearTimeout = (id) => clearTimeout(id);
    const ctl = vmmod.runInContext('_automationQuestionsLoader()', s7);
    await ctl.load();
    const before = urls.length;
    vmmod.runInContext("onAutomationQuestionsSearchInput('gpu'); onAutomationQuestionsSearchInput('gpu-q')", s7);
    await new Promise(r => setImmediate(r));
    await new Promise(r => setImmediate(r));
    assert.equal(urls.length, before, 'no request per keystroke: ' + JSON.stringify(urls));
    await new Promise(r => setTimeout(r, 450));
    await flushAsync(6);
    assert.equal(urls.length, before + 1, 'exactly one re-request after the pause: ' + JSON.stringify(urls));
    assert.ok(urls[before].includes('/api/questions?state=open&limit=50&text=gpu-q'),
      'the paused search committed the current value: ' + JSON.stringify(urls));
    ctl.stop();
  });

  test('window.refreshAutomationQuestions is exposed for the live refresh task', async () => {
    const s8 = loadApp();
    assert.equal(typeof s8.refreshAutomationQuestions, 'function', 'refreshAutomationQuestions exposed');
    let hits = 0;
    s8.fetch = () => { hits += 1; return okList([q({})]); };
    s8.refreshAutomationQuestions();
    await flushAsync(6);
    assert.equal(hits, 1, 'refresh re-requests the queue');
  });

  test('the card sits in the Automation body beside the other cards', () => {
    const s9 = loadApp();
    const body = vmmod.runInContext('_automationBodyHTML()', s9);
    assert.ok(body.includes('id="automationQuestionsCard"'), 'card wrapper present');
    assert.ok(body.includes('id="automationQuestionsSearch"'), 'search input present');
    const iTriage = body.indexOf('id="automationTriageCard"');
    const iQ = body.indexOf('id="automationQuestionsCard"');
    const iPanel = body.indexOf('id="automationPanel"');
    assert.ok(iTriage !== -1 && iQ !== -1 && iPanel !== -1, 'all anchors present');
    assert.ok(iTriage < iQ && iQ < iPanel, 'card sits after the triage card and before the list panel');
  });
});

// v1 question-queue: the automation_questions_* keys are in all five bundles
// (locale-guard step 3), the translations are not English copies (v5.45.1
// lesson), and the placeholder sets stay consistent between bundles (v5.45.1
// lesson).
test('v1 question-queue: automation_questions_* keys in all five bundles', () => {
  const path = require('path');
  const assert = require('node:assert');
  const fs = require('fs');
  const LOCALES_DIR = path.join(__dirname, 'locales');
  const read = (lang) => JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));

  const en = read('en');
  const keys = Object.keys(en).filter((k) => k.startsWith('automation_questions_'));

  test('en has the needed automation_questions_ keys', () => {
    for (const k of [
      'automation_questions_title',
      'automation_questions_search',
      'automation_questions_by',
      'automation_questions_high',
      'automation_questions_age',
      'automation_questions_open',
      'automation_questions_empty',
      'automation_questions_disabled',
      'automation_questions_failed',
    ]) assert.ok(en[k], 'missing en key: ' + k);
  });

  for (const lang of ['de', 'es', 'fr', 'ja']) {
    const bundle = read(lang);
    test(lang + ' has every automation_questions_ key app.js uses', () => {
      for (const k of keys) assert.ok(bundle[k] !== undefined, lang + ' missing key: ' + k);
    });
    test(lang + ' translations are not English copies', () => {
      const copied = keys.filter((k) => bundle[k] === en[k]);
      assert.ok(copied.length === 0, lang + ' copied en values: ' + JSON.stringify(copied));
    });
    test(lang + ' placeholders match the en key sets', () => {
      const ph = (s) => (s.match(/%(\d+)\$[sd]/g) || []).sort();
      const bad = keys.filter((k) => JSON.stringify(ph(bundle[k])) !== JSON.stringify(ph(en[k])));
      assert.ok(bad.length === 0, lang + ' placeholder mismatch: ' + JSON.stringify(bad));
    });
  }

  test('app.js t() calls for automation_questions_ all exist in en.json', () => {
    const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
    const used = [...new Set((src.matchAll(/\bt\('(\w+)'/g) || []).map((m) => m[1]).filter((k) => k.startsWith('automation_questions_')))];
    assert.ok(used.length >= 9, 'expected the new keys to be referenced: ' + JSON.stringify(used));
    for (const k of used) assert.ok(en[k], 'missing en key used by app.js: ' + k);
  });
});
