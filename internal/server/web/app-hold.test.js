'use strict';
// app-hold.test.js — the "held" Automaton state in the web app app.js.
// node: holds a badge, a dialog action and a group view.
// Runs under `node --test` in this directory; no browser, no bundler. It
// uses the same stub-browser helpers the security tests do: the real
// app.js is loaded unmodified into a vm context and the public
// entry points (statusPill, renderAutomataCard, the hold dialog
// validator/submit, the Held Automata group renderer, the inbox badge
// count) are exercised against a stub fetch that records the routes.

const { test } = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');
const { buildSandbox, loadScript, flushAsync, vm, makeStubElement } = require('./testutil_browser_stub');

const appJsPath = path.join(__dirname, 'app.js');
const localesDir = path.join(__dirname, 'locales');

function bundle(locale) {
  return JSON.parse(fs.readFileSync(path.join(localesDir, locale + '.json'), 'utf8'));
}

// fetchCapture returns a fetch stub that records every call with its URL
// and options and answers 200 + empty JSON. The tests assert the route
// and body each action chose.
function fetchCapture() {
  const calls = [];
  const fetchFn = (url, init) => {
    calls.push({ url: String(url), init: init || {} });
    return Promise.resolve({
      ok: true, status: 200,
      json: () => Promise.resolve({}),
      text: () => Promise.resolve(''),
    });
  };
  return { calls, fetchFn };
}

// sandbox loads app.js once into a vm context with the en bundle bound so
// t() resolves real strings, an all-success stub fetch, and getElementById
// answers null for the elements the hold paths would repaint (so the
// reload paths no-op instead of walking the rest of the UI).
function sandbox() {
  const cap = fetchCapture();
  const yesBtn = makeStubElement();
  const sb = buildSandbox({
    byId: {
      confirmYesBtn: yesBtn,
      automataPanel: null,
    },
  });
  vm.createContext(sb);
  sb.fetch = cap.fetchFn;
  sb.document.getElementById = (id) => {
    if (id === 'confirmYesBtn') return yesBtn;
    if (id === 'automataPanel') return null; // loadAutomataPanel no-ops then
    return makeStubElement();
  };
  loadScript(appJsPath, sb);
  sb._i18n = sb._i18n || {};
  sb._i18n.bundle = bundle('en');
  sb._i18n.fallback = bundle('en');
  return { sb, cap, yesBtn };
}

test('statusPill renders the held badge class (a deliberate pause, not running)', () => {
  const { sb } = sandbox();
  const html = vm.runInContext('statusPill("held")', sb);
  assert.ok(html.includes('state-badge-held'), 'held badge class: ' + html);
  assert.ok(!html.includes('state-badge-running'), 'held is not styled as running');
  assert.ok(html.toLowerCase().includes('held'), 'shows the held label');
});

test('a paused card offers Hold but not Release/Extend', () => {
  const { sb } = sandbox();
  const html = vm.runInContext('renderAutomataCard({ id: "prd-paused", title: "T", status: "paused" }, "")', sb);
  assert.ok(html.includes('automaton_hold_action') || /Hold/.test(html), 'Hold button present');
  assert.ok(!html.includes('automataRelease('), 'no Release on a paused card');
  assert.ok(!html.includes('automataExtend('), 'no Extend on a paused card');
});

test('a held card offers Release and Extend, no new Hold, and shows the hold record', () => {
  const { sb } = sandbox();
  const code = `
    (function(){
      const prd = {
        id: "prd-held", title: "Held automaton", status: "held",
        hold: {
          reason: "waiting for operator",
          condition: "resume when the API is back",
          held_by: "operator", held_at: "2026-11-01T10:00:00Z",
          until: "2026-11-02T10:00:00Z",
        },
      };
      return renderAutomataCard(prd, "");
    })()
  `;
  const html = vm.runInContext(code, sb);
  assert.ok(html.includes('automataRelease('), 'Release button');
  assert.ok(html.includes('automataExtend('), 'Extend button');
  assert.ok(!html.includes('automataHold('), 'no fresh Hold button on a held card');
  assert.ok(html.includes('automaton-hold-record'), 'hold record line rendered');
  assert.ok(html.includes('waiting for operator'), 'reason shown');
  assert.ok(html.includes('resume when the API is back'), 'condition shown');
});

test('the hold dialog validation requires a reason', () => {
  const { sb, cap } = sandbox();
  const bad = vm.runInContext('window._automatonHoldValidate({ reason: "   " }, false)', sb);
  assert.strictEqual(bad.ok, false);
  assert.strictEqual(bad.error.field, 'reason');
  const good = vm.runInContext(
    'window._automatonHoldValidate({ reason: "r", condition: "c", until: "u" }, false)', sb);
  assert.strictEqual(good.ok, true);
  assert.strictEqual(good.payload.actor, 'operator');
  assert.strictEqual(good.payload.reason, 'r');
  // Submitting with an empty reason rejects and never touches the API.
  let rejected = null;
  vm.runInContext(`window.automatonHoldSubmit("prd-1", { reason: "" }, false).catch(e => { window.__hErr = e; })`, sb);
  return flushAsync().then(() => {
    rejected = vm.runInContext('window.__hErr', sb);
    assert.ok(rejected, 'rejects');
    assert.strictEqual(rejected.field, 'reason');
    assert.strictEqual(cap.calls.length === 0, true, 'no request without a reason');
  });
});

test('Hold POSTs /hold with reason, condition, until and actor=operator', () => {
  const { sb, cap } = sandbox();
  vm.runInContext(
    'window.automatonHoldSubmit("p/1", { reason: "r", condition: "c", until: "2026-12-31" }, false)', sb);
  return flushAsync().then(() => {
    assert.strictEqual(cap.calls.length, 1, 'one request: ' + JSON.stringify(cap.calls));
    assert.ok(cap.calls[0].url.endsWith('/api/autonomous/prds/p%2F1/hold'), cap.calls[0].url);
    assert.strictEqual((cap.calls[0].init.method || 'POST'), 'POST');
    const body = JSON.parse(cap.calls[0].init.body);
    assert.deepStrictEqual(body, { reason: 'r', condition: 'c', until: '2026-12-31', actor: 'operator' });
  });
});

test('Release asks once, then POSTs /release_hold', () => {
  const { sb, cap, yesBtn } = sandbox();
  vm.runInContext('window.automataRelease("prd-9")', sb);
  assert.strictEqual(typeof yesBtn.onclick, 'function', 'a confirmation was requested');
  yesBtn.onclick(); // confirm
  return flushAsync().then(() => {
    assert.strictEqual(cap.calls.length, 1, 'one request after confirming');
    assert.strictEqual(cap.calls[0].url, '/api/autonomous/prds/prd-9/release_hold');
    assert.strictEqual(cap.calls[0].init.method || 'POST', 'POST');
  });
});

test('the Held Automata group renders each row with reason, condition, expiry and a Release button', () => {
  const { sb } = sandbox();
  const code = `
    (function(){
      return window.heldAutomataListHtml({
        held: [
          {
            id: "prd-a", title: "First", reason: "needs operator",
            condition: "until the merge", held_by: "operator",
            held_at: "2026-11-01T10:00:00Z", until: "2026-11-02T10:00:00Z",
          },
          {
            id: "prd-b", title: "Second", reason: "expired hold",
            until: "2020-01-01T00:00:00Z", expired: true,
          },
        ],
        total: 2, expired: 1,
      });
    })()
  `;
  const html = vm.runInContext(code, sb);
  assert.ok(html.includes('prd-a') && html.includes('First'));
  assert.ok(html.includes('needs operator'), 'reason row');
  assert.ok(html.includes('until the merge'), 'condition row');
  assert.ok(html.includes('held-automata-expired'), 'expired row');
  assert.ok(html.includes(`automataRelease('prd-a')`), 'row Release button');
  assert.ok(html.includes(`automataRelease('prd-b')`), 'expired row still offers Release');
});

test('the Held Automata group shows the localized empty state', () => {
  const { sb } = sandbox();
  const html = vm.runInContext('window.heldAutomataListHtml({ held: [] })', sb);
  const empty = bundle('en')['held_automata_inbox_empty'];
  assert.ok(html.includes(empty), 'empty text: ' + html);
});

test('the inbox badge count excludes held Automata (they have their own card count)', () => {
  const { sb } = sandbox();
  const code = `
    (function(){
      _automationInboxCounts.automation_triage = 2;
      _automationInboxCounts.automation_security_failure = 1;
      _automationInboxCounts.automation_held_automata = 5;
      const n = _automationInboxBadgeCount();
      delete _automationInboxCounts.automation_triage;
      delete _automationInboxCounts.automation_security_failure;
      delete _automationInboxCounts.automation_held_automata;
      return n;
    })()
  `;
  const n = vm.runInContext(code, sb);
  assert.strictEqual(n, 3, '2 triage + 1 security, held excluded');
});

test('the Held group head help icon and the hold dialog help line link the "Held Automata" section of the hold how-to, via the docs link map', () => {
  const { sb } = sandbox();
  assert.strictEqual(vm.runInContext("docsLinkForKey('held_automata_inbox_title')", sb), 'howto/automaton-hold.md#held-automata');
  const icon = vm.runInContext('window.heldAutomataHelpLink()', sb);
  assert.ok(icon.includes('/diagrams.html#docs/howto/automaton-hold.md#held-automata'), 'group head icon: ' + icon);
  const line = vm.runInContext('window.heldHoldDialogHelpLine()', sb);
  assert.ok(line.includes('/diagrams.html#docs/howto/automaton-hold.md#held-automata'), 'dialog help line: ' + line);
  assert.ok(line.includes('Help:'), 'dialog line shows the label, not just a bare ?');
});

test('the held badge keeps a neutral color in style.css (never the error color)', () => {
  const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');
  const m = css.match(/\.state-badge-held\s*\{[^}]*\}/);
  assert.ok(m, '.state-badge-held rule exists');
  assert.ok(!m[0].includes('var(--error)'), 'not the error color: ' + m[0]);
  assert.ok(m[0].includes('var(--text2)'), 'neutral secondary-text color: ' + m[0]);
});
