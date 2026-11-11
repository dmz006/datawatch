// Decision records card in the Automata area — PWA side.
//
// window.decisionRecordCardHtml(view) renders the card body for one list
// view of GET /api/autonomous/decision-records: every record, newest first,
// with title, id, state, situation, question, options and tradeoffs,
// recommendation, default, the operator's answer verbatim, the document
// status line outcome, the follow-up outcome, and the folder watch status
// line. A null view is the not-fetched-yet loading line; an empty
// decision_records array is the empty line; a failed fetch paints the
// unavailable line, never the empty one. window.loadDecisionRecordCard()
// fetches the list and paints it into the #drCard slot. A record answered
// by an Automaton's daemon also shows its record id on the per-Automaton
// decision log line (window.decisionLineHtml). Every value is escaped; a
// hostile value stays inert markup.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-decision-records.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const HOSTILE = '<img src=x onerror=alert(1)>';
const ENTITY_IMG = '&lt;img src=x onerror=alert(1)&gt;';

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
}

function loadApp(lang) {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle(lang || 'en');
  return sandbox;
}

function record(over) {
  return {
    id: 'rec-main',
    title: 'Importer target',
    state: 'answered',
    situation: 'The importer writes to a bucket named in the template.',
    deciding: 'Use the staging bucket for the production importer?',
    options: [
      { id: 'opt-a', label: 'Staging', tradeoff: 'cheaper, but no SLA' },
      { id: 'opt-b', label: 'Production', tradeoff: 'costs, and the quota is shared' },
    ],
    recommendation: { label: 'Staging', reason: 'no consumer depends on the prod bucket yet' },
    default: 'opt-a',
    answer: {
      id: 'a-7',
      quote: 'Take staging for now.',
      actor: 'operator',
      surface: 'pwa',
      answered_at: '2026-10-29T11:30:00Z',
    },
    doc_change: {
      status: 'reported',
      path: 'docs/importer.md',
      before: 'Confirmed: staging',
      after: 'Confirmed: production, by operator, 2026-10-29',
    },
    follow_up: { name: 'start-ingest', kind: 'start_template', target: 'tpl-ingest', status: 'done', result: 'task t9 started' },
    ...over,
  };
}

function card(view) {
  const sandbox = loadApp();
  return vm.runInContext('window.decisionRecordCardHtml(' + JSON.stringify(view) + ')', sandbox);
}

// ── one record, every field ──────────────────────────────────────────────────

test('an answered record renders every field it carries, all escaped', () => {
  const r = record({ title: HOSTILE, situation: HOSTILE, answer: { quote: HOSTILE, actor: HOSTILE, surface: HOSTILE } });
  const html = card({ decision_records: [r], folder: null });
  for (const needle of [
    ENTITY_IMG, // title, situation, quote, actor each escaped exactly once-ish
    'rec-main',
    'Use the staging bucket for the production importer?',
    'decision-record-answer',
    'decision-record-row',
    'decision-record-card-title',
    'answered',
  ]) {
    assert.ok(html.includes(needle), 'missing ' + needle + ' in ' + html);
  }
  assert.ok(!html.includes(HOSTILE), 'unescaped hostile markup leaked: ' + html);
  assert.ok(!html.includes('<script'), html);
});

test('options carry their one-line tradeoff, and the recommendation its reason', () => {
  const html = card({ decision_records: [record()], folder: null });
  assert.ok(html.includes('decision-record-options'), html);
  assert.ok(html.includes('cheaper, but no SLA'), html);
  assert.ok(html.includes('costs, and the quota is shared'), html);
  assert.ok(html.includes('no consumer depends on the prod bucket yet'), html);
  assert.ok(html.includes('opt-a'), html); // the forced default
});

test('the document status line outcome and the follow-up outcome render', () => {
  const html = card({ decision_records: [record()], folder: null });
  assert.ok(html.includes('decision-record-doc-change'), html);
  assert.ok(html.includes('docs/importer.md'), html);
  assert.ok(html.includes('would be rewritten'), html); // reported mode wording
  assert.ok(html.includes('Confirmed: staging'), html);
  assert.ok(html.includes('→ Confirmed: production, by operator, 2026-10-29'), html);
  assert.ok(html.includes('decision-record-follow-up'), html);
  assert.ok(html.includes('start-ingest'), html);
  assert.ok(html.includes('start_template'), html);
  assert.ok(html.includes('task t9 started'), html);
});

test('an open record shows the open state and carries no answer block', () => {
  const html = card({ decision_records: [record({ state: 'open', answer: null, doc_change: null, follow_up: null })] });
  assert.ok(html.includes('decision-record-state-open'), html);
  assert.ok(!html.includes('decision-record-answer'), html);
});

test('an applied document change reads as a change, not a warning', () => {
  const html = card({ decision_records: [record({ doc_change: { status: 'applied', path: 'docs/x.md', before: 'a', after: 'b' } })] });
  assert.ok(html.includes('changed'), html);
  assert.ok(!html.includes('would be rewritten'), html);
});

// ── the three card states ────────────────────────────────────────────────────

test('an empty decision_records array renders nothing', () => {
  assert.equal(card({ decision_records: [] }), '');
});

test('a null view (fetched not yet) renders the loading line, not the empty one', () => {
  const html = card(null);
  assert.ok(html.includes('decision-record-loading'), html);
  assert.ok(!html.includes('decision-record-empty'), html);
  assert.ok(!html.includes('decision-record-row'), html);
});

test('a failed fetch renders the unavailable line, and it is not the empty line', () => {
  const sandbox = loadApp();
  const failed = vm.runInContext('window.decisionRecordCardFailedHtml()', sandbox);
  assert.ok(failed.includes('decision-record-error'), failed);
  assert.ok(failed.includes('not available'), failed);
  const empty = card({ decision_records: [] });
  assert.equal(empty, '');
  assert.notEqual(failed, empty);
});

// ── the folder watch status line ─────────────────────────────────────────────

test('an enabled folder line names the directory and the file count', () => {
  const html = card({ decision_records: [record()], folder: { enabled: true, path: '/srv/decisions', files_seen: 3 } });
  assert.ok(html.includes('decision-record-folder'), html);
  assert.ok(html.includes('/srv/decisions'), html);
  assert.ok(html.includes('3 files read'), html);
});

test('a disabled folder shows no folder line on the card', () => {
  const html = card({
    decision_records: [record()],
    folder: { enabled: false, path: '/srv/decisions', notes: [{ file: 'one.md', message: HOSTILE }] },
  });
  assert.ok(!html.includes('decision-record-folder'), html);
  assert.ok(!html.includes('Folder watch is off'), html);
});

// ── loading into the slot ─────────────────────────────────────────────────────

async function loadCard(reply, id) {
  const sandbox = loadApp();
  const urls = [];
  const slotEl = { innerHTML: 'unset' };
  sandbox.apiFetch = url => {
    urls.push(url);
    return reply instanceof Error ? Promise.reject(reply) : Promise.resolve(reply);
  };
  sandbox.document.getElementById = x => (x === (id || 'drCard') ? slotEl : null);
  vm.runInContext('window.loadDecisionRecordCard()', sandbox);
  await flushAsync();
  return { urls, slotEl };
}

test('the card fetches the decision records list and fills the #drCard slot', async () => {
  const { urls, slotEl } = await loadCard({ decision_records: [record()], folder: { enabled: true, path: '/srv/decisions', files_seen: 1 } });
  assert.deepEqual(urls, ['/api/autonomous/decision-records?limit=100']);
  assert.ok(slotEl.innerHTML.includes('rec-main'), slotEl.innerHTML);
  assert.ok(slotEl.innerHTML.includes('decision-record-folder'), slotEl.innerHTML);
});

test('a failed fetch fills the slot with the unavailable line, not the empty one', async () => {
  const { slotEl } = await loadCard(new Error('boom'));
  assert.ok(slotEl.innerHTML.includes('decision-record-error'), slotEl.innerHTML);
  assert.ok(!slotEl.innerHTML.includes('decision-record-empty'), slotEl.innerHTML);
});

test('a body that is not the expected shape renders nothing, never crashes', async () => {
  const { slotEl } = await loadCard(null);
  assert.equal(slotEl.innerHTML, '');
});

// ── record id on the per-Automaton decision log line ─────────────────────────

test('a decision log row with record_id shows that record id next to the question', () => {
  const sandbox = loadApp();
  const html = vm.runInContext(
    'window.decisionLineHtml([{ id: "q-1", state: "answered", question: "Go?", answer: { id: "a1", actor: "operator", quote: "Yes", answered_at: "2026-10-29T09:00:00Z" }, record_id: "rec-42" }])',
    sandbox,
  );
  assert.ok(html.includes('decision-record-ref-id'), html);
  assert.ok(html.includes('rec-42'), html);
});

test('a decision log row without record_id shows no record reference', () => {
  const sandbox = loadApp();
  const html = vm.runInContext(
    'window.decisionLineHtml([{ id: "q-2", state: "open", question: "Ship?", answer: null }])',
    sandbox,
  );
  assert.ok(!html.includes('decision-record-ref-id'), html);
});

// ── localization ──────────────────────────────────────────────────────────────

test('the card localizes in every supported language', () => {
  const want = {
    en: ['Decision records', 'issue a.md: x'],
    de: ['Entscheidungsprotokolle', 'Problem a.md: x'],
    es: ['Registros de decisiones', 'problema a.md: x'],
    fr: ['Registres de décisions', 'problème a.md: x'],
    ja: ['意思決定レコード', '問題 a.md: x'],
  };
  for (const lang of LANGS) {
    const html = vm.runInContext(
      'window.decisionRecordCardHtml({ decision_records: [{ id: "d1", status: "open", question: "q" }], folder: { enabled: true, path: "/srv/decisions", notes: [{ file: "a.md", message: "x" }] } })',
      loadApp(lang),
    );
    for (const needle of want[lang]) assert.ok(html.includes(needle), lang + ': missing ' + needle + ' in ' + html);
  }
});

// ── the settings rows behind the card ─────────────────────────────────────────

test('every autonomous.decisions.* key is a settings_automata_decisions row with a docs link', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const keys = [
    'enabled', 'folder', 'folder_glob', 'folder_withdraw_on_delete',
    'front_matter_delimiter', 'front_matter_separator', 'front_matter_field_separator', 'front_matter_keys',
    'clarity_mode', 'clarity_min_text_chars', 'clarity_require_tradeoffs', 'clarity_require_label', 'clarity_tracker_id_pattern',
    'doc_root', 'doc_status_mode', 'doc_status_pattern', 'doc_status_template', 'follow_ups',
  ];
  for (const k of keys) {
    assert.ok(src.includes("'autonomous.decisions." + k + "',"), 'row for autonomous.decisions.' + k + ' missing');
    assert.ok(src.includes('docsKey: \'settings_automata_decisions_' + k + '\''), 'docsKey for autonomous.decisions.' + k + ' missing');
    const link = k === 'folder' ? " 'howto/automaton-decision-records.md'" : ' DOCS_DEFINITIONS';
    assert.ok(src.includes('settings_automata_decisions_' + k + ':' + link), 'DOCS_LINKS_BY_KEY entry for settings_automata_decisions_' + k + ' missing');
  }
  assert.ok(src.includes('decision_record_card_title: DOCS_DEFINITIONS'), 'DOCS_LINKS_BY_KEY entry for the card title missing');
});

test('the new card locale keys exist in all five locale bundles', () => {
  const keys = [
    'decision_record_card_title', 'decision_record_empty', 'decision_record_loading', 'decision_record_unavailable',
    'decision_record_ref', 'decision_record_state_open', 'decision_record_state_answered', 'decision_record_state_withdrawn',
    'decision_record_situation', 'decision_record_deciding', 'decision_record_recommendation', 'decision_record_default',
    'decision_record_answer_actor', 'decision_record_answer_surface', 'decision_record_doc_change',
    'decision_record_doc_change_applied', 'decision_record_doc_change_reported', 'decision_record_follow_up',
    'decision_record_folder_off', 'decision_record_folder_issue', 'decision_record_folder_files',
  ];
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of keys) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, lang + ': missing ' + k);
    }
  }
});
