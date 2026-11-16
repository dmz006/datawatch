// Landing state in the PWA: the landing record on the Automaton detail
// overview (state, commit, actor, at, reason) and the keep/drop decision
// buttons on a pending record. The server side (REST, MCP, CLI, comm) has
// its own Go tests. This file loads the real app.js into a vm sandbox and
// drives its functions with a recording fetch.

const nodeTest = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

// Every test title in this file starts with TestLandingState, so the
// feature's tests are found together in the node output.
const test = (title, fn) => nodeTest('TestLandingState: ' + title, fn);

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const BUNDLES = ['en', 'de', 'es', 'fr', 'ja'];
const bundle = lang => JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
const EN = bundle('en');

// The landing block keys the overview reads; all five locales carry them.
const LANDING_KEYS = [
  'automata_detail_landing',
  'automata_detail_landing_at',
  'automata_detail_landing_actor',
  'automata_detail_landing_commit',
  'automata_detail_landing_drop_btn',
  'automata_detail_landing_drop_prompt',
  'automata_detail_landing_keep_btn',
  'automata_detail_landing_keep_prompt',
  'automata_detail_landing_none',
  'automata_detail_landing_reason',
  'automata_detail_landing_state_dropped',
  'automata_detail_landing_state_kept',
  'automata_detail_landing_state_landed',
  'automata_detail_landing_state_pending',
];

const PRD = { id: 'PRD-1', title: 'Audit trail', status: 'running' };
const PENDING = {
  state: 'pending',
  commit: 'abc1234',
  actor: 'automaton 0a1b2c3d',
  at: '2026-01-01T09:00:00Z',
  reason: 'work complete, awaiting operator decision',
};

// boot loads app.js into a fresh sandbox with the English bundle in place, so
// t() returns English text. Toasts and the post-action refresh are stubbed.
function boot() {
  const sandbox = buildSandbox({});
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  vm.runInContext('Object.assign(window._i18n.bundle, ' + JSON.stringify(EN) + ')', sandbox);
  sandbox.confirm = () => true;
  sandbox.__toasts = [];
  sandbox.showToast = (message, type) => { sandbox.__toasts.push({ message, type }); };
  sandbox._refreshAutomataOrPRD = () => {};
  return sandbox;
}

// recordFetch replaces the sandbox fetch. Each call is recorded with its URL,
// method and parsed JSON body.
function recordFetch(sandbox, reply = () => ({ id: PRD.id })) {
  const calls = [];
  sandbox.fetch = (url, opts = {}) => {
    calls.push({ url: String(url), method: opts.method || 'GET', body: opts.body ? JSON.parse(opts.body) : null });
    const data = reply(String(url));
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(data), text: () => Promise.resolve(JSON.stringify(data)) });
  };
  return calls;
}

// onclickCodes returns the decoded onclick handlers in a rendered fragment.
function onclickCodes(html) {
  const decode = s => s.replace(/&quot;/g, '"').replace(/&#39;/g, "'").replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&');
  return [...html.matchAll(/onclick="([^"]*)"/g)].map(m => decode(m[1]));
}

function renderLanding(sandbox, prd) {
  return vm.runInContext('renderLandingState(' + JSON.stringify(prd) + ')', sandbox);
}

test('overview includes the landing block', () => {
  const sandbox = boot();
  const html = renderLanding(sandbox, { ...PRD, landing: PENDING });
  assert.ok(html.includes('Keep (branch not merged)'), 'keep button label (EN)');
  assert.ok(html.includes('Drop (work not merged)'), 'drop button label (EN)');
  assert.ok(html.includes('abc1234'), 'commit shown');
  assert.ok(html.includes('pending'), 'state word shown');
});

test('a pending record shows all fields and both decision buttons', () => {
  const sandbox = boot();
  const html = renderLanding(sandbox, { ...PRD, landing: PENDING });
  for (const field of ['Commit', 'Actor', 'Time', 'Reason']) {
    assert.ok(html.includes(field), 'field label ' + field + ' shown');
  }
  const handlers = onclickCodes(html);
  assert.equal(handlers.length, 2, 'exactly the keep and drop buttons');
  assert.ok(handlers.some(h => h.includes("'landing/keep'")), 'keep button POSTs landing/keep');
  assert.ok(handlers.some(h => h.includes("'landing/drop'")), 'drop button POSTs landing/drop');
  assert.ok(handlers.some(h => h.includes('reason')), 'the prompt asks for the reason');
});

test('a decided record shows its state and hides the decision buttons', () => {
  const sandbox = boot();
  for (const [state, word] of [['landed', 'landed'], ['kept', 'kept'], ['dropped', 'dropped']]) {
    const html = renderLanding(sandbox, { ...PRD, landing: { ...PENDING, state } });
    assert.ok(html.includes(word), state + ' state word shown');
    assert.ok(!html.includes('prdActionPrompt'), state + ' record has no decision buttons');
  }
});

test('no landing record renders the neutral line and no buttons', () => {
  const sandbox = boot();
  const html = renderLanding(sandbox, PRD);
  assert.ok(html.includes(EN.automata_detail_landing_none), 'neutral line shown');
  assert.ok(!html.includes('onclick='), 'no decision buttons');
});

test('keep: the prompt reason is required and POSTs to the keep endpoint with the operator actor', async () => {
  const sandbox = boot();
  const calls = recordFetch(sandbox);
  sandbox.prompt = (msg, default_) => 'keep it for the follow-up release';
  vm.runInContext("prdActionPrompt('PRD-1','landing/keep','reason','Reason for keeping this branch unmerged (required)')", sandbox);
  await new Promise(r => setTimeout(r, 0));
  assert.equal(calls.length, 1, 'one fetch');
  assert.equal(calls[0].method, 'POST');
  assert.equal(calls[0].url, '/api/autonomous/prds/PRD-1/landing/keep');
  assert.equal(calls[0].body.actor, 'operator');
  assert.equal(calls[0].body.reason, 'keep it for the follow-up release');
});

test('drop: the prompt reason POSTs to the drop endpoint', async () => {
  const sandbox = boot();
  const calls = recordFetch(sandbox);
  sandbox.prompt = (msg, default_) => 'work superseded by the redesign';
  vm.runInContext("prdActionPrompt('PRD-1','landing/drop','reason','Reason the work will not be merged (required)')", sandbox);
  await new Promise(r => setTimeout(r, 0));
  assert.equal(calls.length, 1, 'one fetch');
  assert.equal(calls[0].method, 'POST');
  assert.equal(calls[0].url, '/api/autonomous/prds/PRD-1/landing/drop');
  assert.equal(calls[0].body.reason, 'work superseded by the redesign');
});

test('locales: all five bundles carry the landing block keys', () => {
  for (const lang of BUNDLES) {
    const b = bundle(lang);
    for (const k of LANDING_KEYS) {
      assert.ok(typeof b[k] === 'string' && b[k].trim().length > 0, lang + ' has non-empty ' + k);
    }
  }
});

test('locales: every locale shows all four landing states', () => {
  for (const lang of BUNDLES) {
    const b = bundle(lang);
    for (const st of ['pending', 'landed', 'kept', 'dropped']) {
      assert.ok(b['automata_detail_landing_state_' + st], lang + ' has the ' + st + ' state word');
    }
  }
});
