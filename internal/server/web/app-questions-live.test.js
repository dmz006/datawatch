// v1 question live-refresh tests — the question_raised WS frame.
//
// The question_raised frame is the return-path event for a successful
// POST /api/questions: { id, label?, text, options?, priority?,
// context? } (QuestionRaisedData on the Go side). While the Automation
// view is mounted — the questions card element exists — the frame must
// re-request the queue exactly once through
// window.refreshAutomationQuestions (no waiting for the next 5s poll
// tick) and show the PWA's standard live-event notice (the same alert
// dock / showToast path the needs_input case uses) naming the new
// question's label, or its id when the label is empty. The question
// text in that notice is escaped: a hostile label renders
// entity-encoded, never as live HTML. With the view unmounted the frame
// is a complete no-op — no request. A frame with missing fields never
// throws, whatever is (or isn't) in msg.data.
const test = require('node:test');
const path = require('path');
const assert = require('node:assert');
const fs = require('fs');
const vmmod = require('vm');
const { buildSandbox, loadScript, flushAsync } = require(path.join(__dirname, 'testutil_browser_stub.js'));

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const bundle = (lang) => JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
const en = bundle('en');
const HOSTILE = '<img src=x onerror=alert(1)>';
const ESC_HOSTILE = '&lt;img src=x onerror=alert(1)&gt;';

const loadApp = () => {
  const sandbox = buildSandbox();
  vmmod.createContext(sandbox); // buildSandbox returns a plain object; runInContext needs a context
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = en;
  return sandbox;
};

// Unmount the Automation view for frame-handler purposes: the questions
// card element no longer exists. Every other element keeps the stub.
const unmountCard = (s) => {
  const orig = s.document.getElementById;
  s.document.getElementById = (id) => (id === 'automationQuestionsCard') ? null : orig(id);
};

const okList = () => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ count: 0, questions: [] }) });

test('v1 question live-refresh', () => {
  test('mounted: question_raised refreshes the queue exactly once and shows a notice naming the label', async () => {
    const s = loadApp();
    const urls = [];
    s.fetch = (url) => { urls.push(String(url)); return okList(); };
    const ctl = vmmod.runInContext('_automationQuestionsLoader()', s);
    try {
      await ctl.load();
      const before = urls.length;
      s._frame = { type: 'question_raised', data: { id: 'q-live', label: 'Which GPU?', text: HOSTILE } };
      vmmod.runInContext('handleMessage(_frame)', s);
      await flushAsync(8);
      assert.equal(urls.length, before + 1, 'exactly one refresh request: ' + JSON.stringify(urls));
      assert.ok(urls[before].indexOf('/api/questions?state=open') !== -1,
        'the refresh re-requests the open queue: ' + urls[before]);
      // Notice: same alert-dock path as the other live events.
      const alerts = s._alertDock.alerts;
      assert.ok(alerts.length >= 1, 'a notice was queued in the alert dock: ' + JSON.stringify(alerts));
      const hit = alerts.find(a => a.message && a.message.indexOf('Which GPU?') !== -1);
      assert.ok(hit, 'the notice names the new question label: ' + JSON.stringify(alerts));
      assert.ok(!(s._alertDock.muted), 'dock is not muted');
    } finally {
      ctl.stop();
    }
  });

  test('mounted: no label, the notice names the question id instead', async () => {
    const s = loadApp();
    s.fetch = () => okList();
    const ctl = vmmod.runInContext('_automationQuestionsLoader()', s);
    try {
      await ctl.load();
      s._frame = { type: 'question_raised', data: { id: 'q-42', label: '', text: 'some long text' } };
      vmmod.runInContext('handleMessage(_frame)', s);
      await flushAsync(4);
      const hit = s._alertDock.alerts.find(a => a.message && a.message.indexOf('q-42') !== -1);
      assert.ok(hit, 'the notice falls back to the id: ' + JSON.stringify(s._alertDock.alerts));
    } finally {
      ctl.stop();
    }
  });

  test('mounted: a hostile label in the notice is escaped, never live HTML', async () => {
    const s = loadApp();
    s.fetch = () => okList();
    const ctl = vmmod.runInContext('_automationQuestionsLoader()', s);
    try {
      await ctl.load();
      s._frame = { type: 'question_raised', data: { id: 'q-xss', label: HOSTILE, text: 'x' } };
      vmmod.runInContext('handleMessage(_frame)', s);
      await flushAsync(4);
      const hit = s._alertDock.alerts.find(a => a.message && a.message.indexOf(HOSTILE) !== -1);
      assert.ok(hit, 'notice carrying the hostile label: ' + JSON.stringify(s._alertDock.alerts));
      // Render the dock the way the operator sees it.
      vmmod.runInContext('window._alertDock.expanded = true; renderAlertDock();', s);
      const el = s._alertDock.el;
      assert.ok(el, 'dock panel element rendered');
      assert.ok(el.innerHTML.indexOf(ESC_HOSTILE) !== -1,
        'hostile label is entity-encoded in the rendered dock: ' + el.innerHTML.slice(0, 800));
      assert.ok(el.innerHTML.indexOf(HOSTILE) === -1,
        'raw hostile HTML must never appear in the rendered dock');
    } finally {
      ctl.stop();
    }
  });

  test('unmounted: the frame is a no-op — no request, no notice', async () => {
    const s = loadApp();
    unmountCard(s);
    const urls = [];
    s.fetch = (url) => { urls.push(String(url)); return okList(); };
    // Realistic while unmounted: the Automation view never ran its
    // mount, so the automation_questions loader was never registered.
    assert.equal(vmmod.runInContext('window._listLoaders[\'automation_questions\']', s), undefined,
      'no automation_questions loader while the view is unmounted');
    s._frame = { type: 'question_raised', data: { id: 'q-m1', label: 'Which GPU?', text: 'x' } };
    vmmod.runInContext('handleMessage(_frame)', s); // must not throw
    await flushAsync(6);
    assert.equal(urls.length, 0, 'no request while the view is unmounted: ' + JSON.stringify(urls));
    assert.equal(s._alertDock.alerts.length, 0, 'no notice while the view is unmounted');
  });

  test('frames with missing fields never throw (mounted or unmounted)', async () => {
    const s = loadApp();
    s.fetch = () => okList();
    const frames = [
      { type: 'question_raised' },
      { type: 'question_raised', data: undefined },
      { type: 'question_raised', data: null },
      { type: 'question_raised', data: {} },
      { type: 'question_raised', data: 'not-an-object' },
      { type: 'question_raised', data: { id: 12345 } },
      { type: 'question_raised', data: { label: { evil: true } } },
      { type: 'question_raised', data: [1, 2, 3] },
    ];
    const before = s._alertDock.alerts.length;
    for (const f of frames) {
      s._frame = f;
      vmmod.runInContext('handleMessage(_frame)', s); // must not throw
    }
    await flushAsync(4);
    // The number-typed id is legitimate and notices; the rest carry no
    // usable name and add nothing.
    assert.ok(s._alertDock.alerts.length <= before + 1,
      'at most the numeric-id frame produced a notice: ' + JSON.stringify(s._alertDock.alerts));
    const s2 = loadApp();
    unmountCard(s2);
    for (const f of frames) {
      s2._frame = f;
      vmmod.runInContext('handleMessage(_frame)', s2); // must not throw
    }
    assert.equal(s2._alertDock.alerts.length, 0, 'unmounted: no notices at all');
  });
});

test('v1 question live-refresh: the notice key is in all five bundles', () => {
  test('en has the raised notice key with one %1$s slot', () => {
    assert.ok(en.automation_questions_raised, 'en missing automation_questions_raised');
    const ph = (en.automation_questions_raised.match(/%(\d+)\$[sd]/g) || []);
    assert.equal(ph.length, 1, 'exactly one placeholder: ' + en.automation_questions_raised);
  });

  for (const lang of ['de', 'es', 'fr', 'ja']) {
    const b = bundle(lang);
    test(lang + ' has the raised notice key, translated, same placeholder set', () => {
      assert.ok(b.automation_questions_raised, lang + ' missing automation_questions_raised');
      assert.notEqual(b.automation_questions_raised, en.automation_questions_raised, lang + ' copied the en value');
      const ph = (s) => JSON.stringify((s.match(/%(\d+)\$[sd]/g) || []).sort());
      assert.equal(ph(b.automation_questions_raised), ph(en.automation_questions_raised), lang + ' placeholder mismatch');
    });
  }

  test('app.js references the raised notice key through t()', () => {
    const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
    assert.ok(src.match(/\bt\(\s*['"]automation_questions_raised['"]\s*,/),
      'app.js must t(\'automation_questions_raised\') with the name argument');
  });
});
