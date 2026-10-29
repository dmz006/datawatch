// TS-834 — PWA Automation: the operator-question channel, off to on and back
//
// Drives the operator-question channel end to end against the sandbox daemon:
//   - switch off: autonomous.questions.enabled=false makes the Automation
//     view's question card show the disabled text with nothing listed, and
//     both POST and GET /api/questions refuse with 503 naming the switch;
//   - switch on: flipped back to true through PUT /api/config (same daemon,
//     no restart), one question is raised through POST /api/questions with
//     a run-unique text, two options, a recommendation and a default, label
//     "information";
//   - the queue card lists the question within the timeout; opening it in
//     headless Chromium shows the two options as radios, the recommendation
//     (option plus rationale) and the default;
//   - the question is then withdrawn through POST /api/questions/{id}/withdraw
//     (the store has no delete, and an answered question is a decision of
//     record a story must not make — the withdraw is the removal) and the
//     history shows it as withdrawn while the open queue no longer lists it.
// autonomous.questions.enabled is restored to its original value in the
// finally block. Runs against the sandbox daemon only (TEST_HTTP /
// TEST_TOKEN from the environment, via the story lib). Screenshot and log
// are saved the way TS-833 does. Exit 0 on pass, 1 on fail (skip comes
// through the .sh wrapper, as for TS-833).
import { runStory, connectToPWA, navigateTo, screenshot, saveLog, env } from './lib.mjs';

// The sandbox HTTP port redirects to TLS (307) and a redirect drops the
// Authorization header, so REST calls go straight to the self-signed TLS port.
// The relaxed certificate check lives on this dedicated Agent, not on the whole process.
import https from 'node:https';
const sandboxAgent = new https.Agent({ rejectUnauthorized: false });
const API = env.tls;

// api issues one REST call to the sandbox TLS port and answers { ok, status, json(), text() } like fetch.
function api(path, { method = 'GET', headers: h = {}, body } = {}) {
  return new Promise((resolve, reject) => {
    if (body) h = { ...h, 'Content-Length': Buffer.byteLength(body) };
    const req = https.request(new URL(path, API), { method, headers: h, agent: sandboxAgent }, (res) => {
      const chunks = [];
      res.on('data', (c) => chunks.push(c));
      res.on('end', () => {
        const text = Buffer.concat(chunks).toString('utf8');
        resolve({
          ok: res.statusCode >= 200 && res.statusCode < 300,
          status: res.statusCode,
          json: async () => JSON.parse(text || '{}'),
          text: async () => text,
        });
      });
    });
    req.on('error', reject);
    if (body) req.write(body);
    req.end();
  });
}

const headers = { 'Content-Type': 'application/json', Authorization: `Bearer ${env.token}` };
const SFX = Date.now().toString(36);
const TEXT = `TS-834 run ${SFX}: the demo Automaton dropped its connection mid-batch. How should it recover?`;
const OPTIONS = [
  { id: 'thirty-seconds', label: 'Reconnect every thirty seconds' },
  { id: 'five-minutes', label: 'Wait five minutes between attempts' },
];
const RECOMMENDATION = { option_id: 'thirty-seconds', rationale: 'keeps the queue bounded' };
const DEFAULT = 'five-minutes';

// questionBody is the one question the story raises: a run-unique text, two
// options, a recommendation and a default, label "information" per the story.
function questionBody() {
  return {
    label: 'information',
    context: 'none',
    text: TEXT,
    options: OPTIONS,
    recommendation: RECOMMENDATION,
    default: DEFAULT,
  };
}

// getEnabled reads the switch live: GET /api/config carries the effective value.
async function getEnabled() {
  const r = await api('/api/config', { headers });
  if (!r.ok) throw new Error(`GET /api/config -> ${r.status}`);
  const q = ((await r.json()).autonomous || {}).questions || {};
  return !!q.enabled;
}

// setEnabled flips the switch and checks the value landed, like TS-833.
async function setEnabled(value) {
  const r = await api('/api/config', {
    method: 'PUT',
    headers,
    body: JSON.stringify({ 'autonomous.questions.enabled': value }),
  });
  if (!r.ok) throw new Error(`PUT autonomous.questions.enabled=${value} -> ${r.status}`);
  const got = await getEnabled();
  if (got !== !!value) throw new Error(`switch did not land: wanted ${value}, read ${got}`);
}

await runStory(async (page) => {
  let original = null;
  try {
    original = await getEnabled();

    // ── Step 1: switch off — disabled card, 503s, nothing listed ─────────
    await setEnabled(false);
    await connectToPWA(page);
    await navigateTo(page, 'autonomous');
    await page.evaluate(() => switchAutomataTab && switchAutomataTab('automation'));
    await page.waitForSelector('#automationQuestionsCard', { timeout: 20000 });
    // Refresh once so the page paints the disabled state, then wait for it.
    await page.evaluate(() => window.refreshAutomationQuestions && window.refreshAutomationQuestions());
    await page.waitForFunction(
      () => {
        const b = document.getElementById('automationQuestionsBody');
        return b && (b.textContent || '').includes('question switch is off')
          && !b.querySelector('.questions-queue-row');
      },
      { timeout: 15000 },
    ).catch(async () => {
      const b = await page.evaluate(() => {
        const el = document.getElementById('automationQuestionsBody');
        return el ? el.textContent : '(no card body)';
      });
      throw new Error(`switch off: disabled card text not shown (got: "${b}")`);
    });
    await screenshot(page, '01-switch-off-disabled');

    // Both the raise and the read refuse service, naming the switch.
    const raiseOff = await api('/api/questions', { method: 'POST', headers, body: JSON.stringify(questionBody()) });
    if (raiseOff.status !== 503) throw new Error(`POST /api/questions (switch off): want 503, got ${raiseOff.status}`);
    if (!((await raiseOff.text()).includes('autonomous.questions.enabled')))
      throw new Error(`503 does not name autonomous.questions.enabled: ${(await raiseOff.text())}`);
    const listOff = await api('/api/questions?state=open', { headers });
    if (listOff.status !== 503) throw new Error(`GET /api/questions (switch off): want 503, got ${listOff.status}`);
    await saveLog('step1-switch-off', JSON.stringify({ raise: raiseOff.status, list: listOff.status }));

    // ── Step 2: switch on, raise one question ─────────────────────────────
    await setEnabled(true);
    const raised = await api('/api/questions', { method: 'POST', headers, body: JSON.stringify(questionBody()) });
    if (raised.status !== 201) {
      throw new Error(`POST /api/questions (switch on): want 201, got ${raised.status}: ${(await raised.text()).slice(0, 200)}`);
    }
    // The raise answers { question: {...} } — unwrap the envelope (both
    // shapes are tolerated so the story keeps reading an old daemon too).
    const raisedBody = await raised.json();
    const created = raisedBody.question || raisedBody;
    const questionId = String(created.id || '');
    if (!questionId) throw new Error(`raise returned no id: ${JSON.stringify(raisedBody)}`);
    if (created.state !== 'open') throw new Error(`raised question is not open: state=${created.state}`);
    if (created.text !== TEXT) throw new Error('raised question text does not match the run-unique text');
    await saveLog('step2-raised', JSON.stringify({ id: questionId, label: created.label, state: created.state }, null, 1));

    // ── Step 3: the queue card lists it; open it and check the dialog ────
    // The PWA live-refreshes on the question_raised frame while the card is
    // mounted; poll the card until the row lands within the window.
    await page.waitForFunction(
      (qid) => {
        const b = document.getElementById('automationQuestionsBody');
        return b && b.querySelector(`.questions-queue-row[data-question-id="${qid}"]`) !== null;
      },
      questionId,
      { timeout: 15000, polling: 500 },
    ).catch(async () => {
      // One forced refresh before the verdict (the 5-second poll alone
      // could have been mid-tick).
      await page.evaluate(() => window.refreshAutomationQuestions && window.refreshAutomationQuestions());
      await page.waitForFunction(
        (qid) => {
          const b = document.getElementById('automationQuestionsBody');
          return b && b.querySelector(`.questions-queue-row[data-question-id="${qid}"]`) !== null;
        },
        questionId,
        { timeout: 10000, polling: 500 },
      ).catch(async () => {
        const rows = await page.evaluate(() => {
          const b = document.getElementById('automationQuestionsBody');
          return b ? [...b.querySelectorAll('.questions-queue-row')].map((r) => r.getAttribute('data-question-id')) : [];
        });
        throw new Error(`queue card does not list ${questionId} (rows: ${rows.join(',') || 'none'})`);
      });
    });
    await screenshot(page, '02-queue-open');

    // Open the question: the answer dialog lists the options as radios,
    // marks the recommendation (option plus rationale) and the default.
    await page.click(`#automationQuestionsBody .questions-queue-row[data-question-id="${questionId}"] .questions-open-btn`, { timeout: 5000 });
    await page.waitForSelector('#genericModal', { state: 'visible', timeout: 10000 });
    const dialog = await page.evaluate(() => {
      const modal = document.getElementById('genericModal');
      return {
        radios: [...modal.querySelectorAll('input[name="questionAnswerChoice"]')].map((r) => r.value),
        rec: ((modal.querySelector('.question-answer-rec') || {}).textContent || '').trim(),
        def: ((modal.querySelector('.question-answer-default') || {}).textContent || '').trim(),
      };
    });
    if (dialog.radios.length !== 2) throw new Error(`expected two option radios, got: ${dialog.radios.join(',')}`);
    if (dialog.radios[0] !== OPTIONS[0].id || dialog.radios[1] !== OPTIONS[1].id)
      throw new Error(`option ids mismatch: ${dialog.radios.join(',')}`);
    if (!dialog.rec.includes(OPTIONS[0].label)) throw new Error(`recommendation line missing the recommended option: "${dialog.rec}"`);
    if (!dialog.rec.includes(RECOMMENDATION.rationale)) throw new Error(`recommendation line missing the rationale: "${dialog.rec}"`);
    if (!dialog.def.includes(OPTIONS[1].label)) throw new Error(`default line missing the default option: "${dialog.def}"`);
    await saveLog('step3-dialog', JSON.stringify(dialog, null, 1));
    await screenshot(page, '03-answer-dialog');
    // Leave the dialog without answering: the withdraw, not an answer, is
    // this story's removal, and an answer would be a decision of record.
    await page.evaluate(() => { const m = document.getElementById('genericModal'); if (m && m.remove) m.remove(); });

    // ── Steps 4–5: withdraw, then the history flags it withdrawn ──────────
    // The store has no delete; the withdraw is the removal (an answered
    // question cannot be removed either).
    const wd = await api(`/api/questions/${encodeURIComponent(questionId)}/withdraw`, {
      method: 'POST',
      headers,
      body: JSON.stringify({ actor: `TS-834-story-${SFX}`, reason: `TS-834 story cleanup run ${SFX}` }),
    });
    if (wd.status !== 200) {
      throw new Error(`POST /api/questions/${questionId}/withdraw: want 200, got ${wd.status}: ${(await wd.text()).slice(0, 200)}`);
    }
    const wdBody = await wd.json();
    if (((wdBody.question || {}).state) !== 'withdrawn') {
      throw new Error(`withdraw body state is ${((wdBody.question || {}).state)}, want withdrawn`);
    }
    const byId = await api(`/api/questions/${encodeURIComponent(questionId)}`, { headers });
    if (byId.status !== 200) throw new Error(`GET by id after withdraw -> ${byId.status}`);
    if (((await byId.json()).question || {}).state !== 'withdrawn') {
      throw new Error('GET by id after the withdraw does not read withdrawn');
    }
    const openList = await api('/api/questions?state=open', { headers });
    if (openList.ok && JSON.stringify(await openList.json()).includes(questionId)) {
      throw new Error('the open REST list still carries the withdrawn question');
    }

    // History panel: the question's row is flagged withdrawn.
    await page.evaluate(() => window.toggleAutomationQuestionsHistory && window.toggleAutomationQuestionsHistory());
    await page.evaluate(() => window.refreshAutomationQuestions && window.refreshAutomationQuestions());
    await page.waitForFunction(
      (qid) => {
        const row = document.querySelector(`.questions-history-row[data-question-id="${qid}"]`);
        return row && row.querySelector('.questions-history-state-withdrawn') !== null;
      },
      questionId,
      { timeout: 15000, polling: 500 },
    ).catch(async () => {
      const rows = await page.evaluate(() => [...document.querySelectorAll('.questions-history-row')].map((r) => r.getAttribute('data-question-id')));
      throw new Error(`history does not show ${questionId} as withdrawn (history rows: ${rows.join(',') || 'none'})`);
    });
    const history = await page.evaluate((qid) => {
      const row = document.querySelector(`.questions-history-row[data-question-id="${qid}"]`);
      return row ? { state: ((row.querySelector('.questions-history-state') || {}).textContent || '').trim(), withdrawn: row.querySelector('.questions-history-state-withdrawn') !== null } : null;
    }, questionId);
    if (!history || !history.withdrawn || !history.state.toLowerCase().includes('withdrawn')) {
      throw new Error(`history row is not flagged withdrawn: ${JSON.stringify(history)}`);
    }
    await saveLog('step5-history', JSON.stringify({ rest: 'withdrawn', history }, null, 1));
    await screenshot(page, '04-history-withdrawn');

    // Back to the open queue: the withdrawn question is no longer listed.
    await page.evaluate(() => window.toggleAutomationQuestionsHistory && window.toggleAutomationQuestionsHistory());
    await page.evaluate(() => window.refreshAutomationQuestions && window.refreshAutomationQuestions());
    await page.waitForFunction(
      (qid) => {
        const b = document.getElementById('automationQuestionsBody');
        return b && b.querySelector(`.questions-queue-row[data-question-id="${qid}"]`) === null;
      },
      questionId,
      { timeout: 15000, polling: 500 },
    ).catch(async () => {
      const rows = await page.evaluate(() => {
        const b = document.getElementById('automationQuestionsBody');
        return b ? [...b.querySelectorAll('.questions-queue-row')].map((r) => r.getAttribute('data-question-id')) : [];
      });
      throw new Error(`open queue still lists the withdrawn question (rows: ${rows.join(',')})`);
    });

    await saveLog('result', JSON.stringify({
      switch: { original, restoredTo: original },
      questionId,
      steps: [
        'switch off: disabled card text, nothing listed, POST and GET are 503 naming autonomous.questions.enabled',
        'switch on: one question raised (201, open) with run-unique text, label information, two options, recommendation, default',
        'the queue card lists it within the timeout; the dialog shows the options, the recommendation and the default',
        'withdrawn via POST /api/questions/{id}/withdraw; history flags it withdrawn and the open queue no longer lists it',
      ],
    }, null, 1));
  } finally {
    // Restore the switch to the value this run found it at, whichever
    // step failed (the original is the sandbox value read at the start).
    try {
      if (original !== null) await setEnabled(!!original);
    } catch (e) {
      console.error('TS-834: could not restore autonomous.questions.enabled:', e && e.message);
    }
  }
});
