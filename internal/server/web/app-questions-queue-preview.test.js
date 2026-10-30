// Question-queue row preview tests — the open-questions card shows the label
// first (the id is smaller secondary text) and only the first N lines of the
// question text, where N is autonomous.questions.queue_preview_lines (default
// 3). A clipped row carries a Show more control that expands it to the full
// text and flips to Show less; the expanded state is keyed by question id so
// the card's periodic refresh keeps it.
const test = require('node:test');
const path = require('path');
const assert = require('node:assert');
const vmmod = require('vm');
const fs = require('fs');
const { buildSandbox, loadScript, flushAsync } = require(path.join(__dirname, 'testutil_browser_stub.js'));

const APP_JS = path.join(__dirname, 'app.js');
const bundle = (lang) => JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
const en = bundle('en');

const loadApp = () => {
  const sandbox = buildSandbox();
  vmmod.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = en;
  return sandbox;
};

const lines = (n) => Array.from({ length: n }, (_, i) => 'line ' + (i + 1)).join('\n');
const q = (extra) => Object.assign({
  id: 'q1', label: 'Which GPU?', asked_by: 'automaton:worker-7', text: 'short question',
  created_at: new Date(Date.now() - 5 * 60 * 1000).toISOString(), state: 'open', priority: '',
}, extra);

// render paints one question through the card builder; the fixture goes in
// as a sandbox global because runInContext cannot see host locals.
const render = (sandbox, question) => {
  sandbox._pq = { count: 1, questions: [question] };
  return vmmod.runInContext('window.questionsCardHtml(_pq)', sandbox);
};
const textBlock = (html) => {
  const m = html.match(/<div class="questions-text"[^>]*>([\s\S]*?)<\/div>/);
  return m ? m[1] : '';
};

test('short text shows no Show more control', () => {
  const s = loadApp();
  const html = render(s, q({ text: 'one\ntwo' }));
  assert.ok(html.includes('one\ntwo'), 'full short text shown');
  assert.ok(!html.includes('questions-show-more'), 'no control for short text');
  assert.ok(!html.includes(en.automation_questions_show_more));
});

test('long text is clipped to the first 3 lines with the control', () => {
  const s = loadApp();
  const html = render(s, q({ text: lines(6) }));
  const shown = textBlock(html);
  assert.equal(shown, 'line 1\nline 2\nline 3', 'only the first three lines: ' + shown);
  assert.ok(html.includes('questions-show-more'), 'control present');
  assert.ok(html.includes('>' + en.automation_questions_show_more + '<'), 'control reads Show more');
});

test('a single very long line is clipped and gets the control', () => {
  const s = loadApp();
  const html = render(s, q({ text: 'word '.repeat(200) }));
  assert.ok(html.includes('-webkit-line-clamp:3'), 'collapsed block carries a 3-line clamp');
  assert.ok(html.includes('questions-show-more'), 'control present for a long paragraph');
});

test('expanding shows everything and flips to Show less, surviving a refresh', async () => {
  const s = loadApp();
  s.fetch = () => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ count: 1, questions: [q({ text: lines(6) })] }) });
  const ctl = vmmod.runInContext('_automationQuestionsLoader()', s);
  await ctl.load();
  await flushAsync(4);
  assert.ok(ctl.html().includes('>' + en.automation_questions_show_more + '<'));
  assert.ok(!ctl.html().includes('line 6'), 'collapsed hides the tail');

  vmmod.runInContext("window.toggleAutomationQuestionPreview('q1')", s);
  let html = ctl.html();
  assert.ok(html.includes('line 6'), 'expanded shows the full text');
  assert.ok(html.includes('>' + en.automation_questions_show_less + '<'), 'control flips to Show less');
  assert.ok(!html.includes('>' + en.automation_questions_show_more + '<'));

  await ctl.load(); // the periodic refresh
  await flushAsync(4);
  html = ctl.html();
  assert.ok(html.includes('line 6'), 'still expanded after the refresh');

  vmmod.runInContext("window.toggleAutomationQuestionPreview('q1')", s);
  assert.ok(!ctl.html().includes('line 6'), 'Show less collapses again');
  ctl.stop();
});

test('expanded state is per question id', () => {
  const s = loadApp();
  vmmod.runInContext("window.toggleAutomationQuestionPreview('q1')", s);
  s._pq = { count: 2, questions: [q({ id: 'q1', text: lines(5) }), q({ id: 'q2', text: lines(5) })] };
  const html = vmmod.runInContext('window.questionsCardHtml(_pq)', s);
  const rows = html.split('<div class="questions-queue-row"').slice(1);
  assert.ok(rows[0].includes('line 5'), 'q1 expanded');
  assert.ok(!rows[1].includes('line 5'), 'q2 still clipped');
});

test('the configured N from the quality policy is honoured', async () => {
  const s = loadApp();
  const urls = [];
  s.fetch = (url) => {
    urls.push(String(url));
    if (String(url).indexOf('/api/questions/quality') === 0) {
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ mode: 'warn', queue_preview_lines: 5 }) });
    }
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ count: 1, questions: [q({ text: lines(8) })] }) });
  };
  vmmod.runInContext('_automationQuestionsLoadQuality()', s);
  await flushAsync(6);
  assert.ok(urls.includes('/api/questions/quality'), 'quality policy read: ' + JSON.stringify(urls));
  const ctl = vmmod.runInContext('_automationQuestionsLoader()', s);
  await ctl.load();
  await flushAsync(6);
  assert.equal(textBlock(ctl.html()), lines(5), 'five lines shown');
  ctl.stop();
});

test('N of 2 clips at two lines; an unusable value falls back to 3', () => {
  const s = loadApp();
  vmmod.runInContext('_automationQuestionsState.previewLines = 2', s);
  assert.equal(textBlock(render(s, q({ text: lines(4) }))), 'line 1\nline 2');
  vmmod.runInContext('_automationQuestionsState.previewLines = 0', s);
  assert.equal(textBlock(render(s, q({ text: lines(4) }))), 'line 1\nline 2\nline 3');
});

test('a failed quality read keeps the default of 3', async () => {
  const s = loadApp();
  s.fetch = (url) => String(url).indexOf('/api/questions/quality') === 0
    ? Promise.resolve({ ok: false, status: 503, text: () => Promise.resolve('off') })
    : Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ count: 1, questions: [q({ text: lines(6) })] }) });
  vmmod.runInContext('_automationQuestionsLoadQuality()', s);
  await flushAsync(6);
  const ctl = vmmod.runInContext('_automationQuestionsLoader()', s);
  await ctl.load();
  await flushAsync(6);
  assert.equal(textBlock(ctl.html()), lines(3));
  ctl.stop();
});

test('the label leads the row and the id is secondary; Open and the high marker stay', () => {
  const s = loadApp();
  const html = render(s, q({ priority: 'high', text: lines(6) }));
  const iLabel = html.indexOf('questions-label');
  const iId = html.indexOf('questions-id');
  assert.ok(iLabel !== -1 && iId !== -1 && iLabel < iId, 'label before id');
  assert.ok(/class="questions-id" style="font-size:11px;color:var\(--text2\);"/.test(html), 'id is the smaller secondary text');
  assert.ok(html.includes('questions-priority-high'), 'high marker kept');
  assert.ok(html.includes("window.openQuestion('q1')"), 'Open button kept');
});

test('a row with no label shows the id as the primary text', () => {
  const s = loadApp();
  const html = render(s, q({ label: '' }));
  assert.ok(!html.includes('questions-label'));
  assert.ok(/class="questions-id" style="font-weight:600/.test(html));
});

test('the two new keys exist in all five bundles', () => {
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = bundle(lang);
    assert.equal(b.automation_questions_show_more, 'Show more', lang);
    assert.equal(b.automation_questions_show_less, 'Show less', lang);
  }
});
