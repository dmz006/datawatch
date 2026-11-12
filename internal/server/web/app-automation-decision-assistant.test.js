// Automation tab — decision assistant.
//
// Covers the setting block on the detail page, shadow and live labelling,
// assistant rows in the decision history, escaping, the set_decision_assistant
// request body and the five-locale key set. fetch is stubbed; rendered text is
// the real English bundle.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

function loadApp(routes, byId) {
  const sandbox = buildSandbox({ byId: byId || {} });
  sandbox.fetch = (url, opts) => {
    const hit = routes[url];
    if (!hit) return Promise.resolve({ ok: false, status: 404, statusText: 'Not Found', text: () => Promise.resolve('not found') });
    return Promise.resolve({
      ok: true, status: 200, statusText: '200',
      json: () => Promise.resolve(hit.body),
      text: () => Promise.resolve(JSON.stringify(hit.body)),
    });
  };
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

function automation(extra) {
  return Object.assign({
    id: 'a1b2c3d4', name: 'Release train', status: 'active', version: 3,
    entry_member_id: 'm1', active_cursors: [],
    members: [{ id: 'm1', prd_id: 'm1aaaaaaaaaaaa', status: 'running', added_by: 'operator' }],
  }, extra || {});
}

function render(a) {
  const sb = loadApp({});
  return sb.buildAutomationDetailHTML(a, {});
}

const SHADOW_NOTE = '[SHADOW — not acted on] outcome=would_decide option="skip_member" confidence=0.93 evidence: member=m2 failure="boom" reason: the failure is flaky';
const LIVE_NOTE = '[live] outcome=decided option="pause_automation" confidence=0.97 evidence: spend=high reason: over budget';

test('detail page says the decision assistant is off when no setting exists', () => {
  const html = render(automation());
  assert.match(html, /automation-assistant-off/);
  assert.match(html, />Off</);
});

test('detail page shows backend, model, mode, hooks and thresholds', () => {
  const html = render(automation({
    decision_assistant: { backend: 'pi-prompt', model: 'qwen3.8:27b', mode: 'live', hooks: ['empty_edge', 'budget_breach'], thresholds: { empty_edge: 0.9 } },
  }));
  assert.match(html, /pi-prompt \/ qwen3\.8:27b/);
  assert.match(html, /data-mode="live"/);
  assert.match(html, /Live \(acts\)/);
  assert.match(html, /Empty edge, Budget breach/);
  assert.match(html, /Empty edge 0\.9/);
});

test('mode defaults to shadow and no hooks / thresholds fall back to labels', () => {
  const html = render(automation({ decision_assistant: { backend: 'ollama', model: 'm' } }));
  assert.match(html, /data-mode="shadow"/);
  assert.match(html, /Shadow \(records only\)/);
  assert.match(html, /None enabled/);
  assert.match(html, /automation-assistant-thresholds">Default</);
});

test('shadow assistant decision reads "would have" with confidence and evidence', () => {
  const html = render(automation({ decisions: [{ kind: 'assistant_skip_member', actor: 'assistant:qwen3.8:27b', note: SHADOW_NOTE, at: '2026-10-22T10:00:00Z' }] }));
  assert.match(html, /automation-decision-assistant/);
  assert.match(html, /Would have: skip_member/);
  assert.match(html, /Confidence 0\.93/);
  assert.match(html, /Evidence: member=m2 failure=&quot;boom&quot;/);
  assert.match(html, /the failure is flaky/);
  assert.match(html, /Shadow \(records only\)/);
  assert.doesNotMatch(html, /Decided: /);
});

test('live assistant decision reads "decided" and is labelled live', () => {
  const html = render(automation({ decisions: [{ kind: 'assistant_budget_breach', actor: 'assistant:m', note: LIVE_NOTE }] }));
  assert.match(html, /Decided: pause_automation/);
  assert.match(html, /Confidence 0\.97/);
  assert.match(html, /automation-assistant-mode">Live \(acts\)</);
  assert.doesNotMatch(html, /Would have/);
});

test('non-assistant decisions keep the plain row', () => {
  const html = render(automation({ decisions: [{ kind: 'pause', actor: 'operator', note: 'by hand' }] }));
  assert.doesNotMatch(html, /automation-decision-assistant/);
  assert.match(html, /by hand/);
});

test('note parser returns empty fields for free text', () => {
  const sb = loadApp({});
  const p = sb.automationAssistantNoteParts('plain words');
  assert.equal(p.shadow, false);
  assert.equal(p.option, '');
  assert.equal(p.confidence, '');
});

test('assistant text is escaped everywhere', () => {
  const evil = '<img src=x onerror=alert(1)>';
  const html = render(automation({
    decision_assistant: { backend: evil, model: evil, hooks: [evil], thresholds: { [evil]: 1 } },
    decisions: [{
      kind: evil, actor: 'assistant:' + evil,
      note: `[SHADOW] outcome=x option="${evil}" confidence=0.5 evidence: ${evil} reason: ${evil}`,
    }],
  }));
  assert.ok(!html.includes('<img'), 'raw markup leaked');
  assert.match(html, /&lt;img src=x onerror=alert\(1\)&gt;/);
});

test('body builder clears on empty backend and keeps only chosen hooks', () => {
  const sb = loadApp({});
  assert.deepEqual(JSON.parse(JSON.stringify(sb.automationDecisionAssistantBody({ backend: '' }))), { clear: true });
  const body = JSON.parse(JSON.stringify(sb.automationDecisionAssistantBody({
    backend: 'pi-prompt', model: 'q', mode: 'bogus', hooks: ['skip_member', 'nope'],
    thresholds: { skip_member: '0.8', empty_edge: '0.5', budget_breach: '' },
  })));
  assert.deepEqual(body, { backend: 'pi-prompt', model: 'q', mode: 'shadow', hooks: ['skip_member'], thresholds: { skip_member: 0.8 } });
});

test('the edit control posts set_decision_assistant for the open Automation', async () => {
  const a = automation({ decision_assistant: { backend: 'ollama', model: 'm', mode: 'shadow', hooks: ['empty_edge'] } });
  const view = makeStubElement();
  const controls = makeStubElement();
  const sb = loadApp({
    ['/api/automations/' + a.id]: { body: a },
    '/api/autonomous/prds': { body: { prds: [] } },
  }, { view, automationControls: controls });
  sb.openAutomationDetail(a.id);
  await flushAsync();
  sb.renderAutomationControls(a);
  assert.match(controls.innerHTML, /data-op="set_decision_assistant"/);
  assert.match(controls.innerHTML, /openAutomationDecisionAssistantDialog\('a1b2c3d4'\)/);
});

test('every assistant key exists in all five locale bundles with the same placeholders', () => {
  const langs = ['en', 'de', 'es', 'fr', 'ja'];
  const bundles = {};
  for (const l of langs) bundles[l] = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));
  const keysOf = l => Object.keys(bundles[l]).filter(k => k.startsWith('automation_assistant_')).sort();
  const en = keysOf('en');
  assert.ok(en.length >= 20);
  for (const l of langs) {
    assert.deepEqual(keysOf(l), en, l + ' key set differs');
    for (const k of en) {
      assert.ok(String(bundles[l][k]).trim() !== '', l + ' ' + k + ' empty');
      assert.deepEqual(String(bundles[l][k]).match(/%\d+\$[sd]/g), String(bundles.en[k]).match(/%\d+\$[sd]/g), l + ' ' + k + ' placeholders');
    }
  }
});

test('every assistant key the app names literally exists in English', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  const used = new Set();
  const re = /['"`](automation_assistant_[a-z_]*[a-z])['"`]/g;
  let m;
  while ((m = re.exec(src)) !== null) used.add(m[1]);
  const missing = [...used].filter(k => !(k in en));
  assert.deepEqual(missing, []);
});
