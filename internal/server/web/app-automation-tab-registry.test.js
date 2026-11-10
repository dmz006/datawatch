// The Automation view's six-tab registry, ordered strip, card ownership, card
// slots, Inbox badge, strip styling and switching behaviour.
//
// Counts and names are asserted, not just shapes: six entries in the order
// inbox, automations, resources, progress, setup, housekeeping; the English
// labels; the exact card ids each tab body owns; the pre-registered hidden
// placeholders; the Inbox badge sum; the strip rules in style.css.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const readLocale = l => JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));
const STYLE = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');

function jsonRes(body) {
  return { ok: true, status: 200, statusText: '', json: () => Promise.resolve(body), text: () => Promise.resolve(JSON.stringify(body)) };
}

function loadView(fetchImpl) {
  const els = {};
  const out = { view: '' };
  els.view = makeStubElement(h => { out.view = h; });
  const sandbox = buildSandbox({ byId: els });
  sandbox.URLSearchParams = URLSearchParams;
  sandbox.fetch = fetchImpl || (() => Promise.resolve(jsonRes([])));
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = readLocale('en');
  return { sb: sandbox, out, els };
}

const IDS = ['inbox', 'automations', 'resources', 'progress', 'setup', 'housekeeping'];
const LABELS = ['Inbox', 'Automations', 'Resources', 'Progress and cost', 'Setup and policy', 'Housekeeping'];
const registry = sb => vm.runInContext('_AUTOMATION_SUBTABS', sb);
const subtabOf = sb => vm.runInContext('_automationState.subtab', sb);

// Fixed cards per tab, in body order, and the hidden placeholders per tab in
// slot order. The union is the full set of card ids a body holds.
const FIXED = {
  inbox: ['automationTriageCard', 'automationQuestionsCard', 'automationHeldQuestionsCard', 'automationSecurityFailureCard'],
  automations: ['automationFilterBar', 'automationPanel'],
  resources: ['automationCapacityCard', 'automationLaneFillCard', 'automationUsageCard', 'automationShipCard'],
  progress: ['progressCard'],
  setup: ['automationOnboardingReportCard', 'automationMergePipelineCard', 'automationRequestWatchCard'],
  housekeeping: ['automationUncommittedWorkCard', 'automationProtectedPathsCard'],
};
const PLACEHOLDERS = {
  inbox: [],
  automations: ['automationReleaseArcCard', 'automationGanttCard'],
  resources: ['automationLaneUtilizationCard', 'automationVerifierSlotsCard'],
  progress: ['automationAllProjectsGanttCard', 'automationCostBillingCard', 'automationCompletionForecastCard'],
  setup: ['automationProjectPolicyCard', 'automationDecomposeLintCard', 'automationPriorityByKindCard', 'automationGroupingTagsCard', 'automationSupervisorLoopCard'],
  housekeeping: ['automationStaleWorktreesCard'],
};

// Every id="automation…Card"-style id and the filter bar/panel in a body, in
// document order. Inner ids (history button, search field, bodies) are not cards.
function cardIds(html) {
  return [...html.matchAll(/<div id="([A-Za-z]+)"/g)].map(m => m[1])
    .filter(id => /Card$|^automationFilterBar$|^automationPanel$/.test(id));
}
// The body of the selected tab: everything after the strip.
function bodyOf(html) {
  const i = html.indexOf('</div>', html.indexOf('automation-subtabs'));
  return html.slice(i);
}
function stripButtons(html) {
  return [...html.matchAll(/<button class="output-tab( active)? ?" role="tab" aria-selected="(true|false)" data-subtab="([a-z]+)"[^>]*>([\s\S]*?)<\/button>/g)]
    .map(m => ({ id: m[3], active: !!m[1], selected: m[2], inner: m[4] }));
}

test('registry: exactly six entries in order, each with labelKey in all five locales, bodyHTML, mount, stop', () => {
  const { sb } = loadView();
  const reg = registry(sb);
  assert.equal(reg.length, 6, 'six entries');
  assert.deepEqual(Array.from(reg, e => e.id), IDS, 'order');
  const bundles = Object.fromEntries(['en', 'de', 'es', 'fr', 'ja'].map(l => [l, readLocale(l)]));
  for (const e of reg) {
    assert.equal(typeof e.bodyHTML, 'function', e.id + ' bodyHTML');
    assert.equal(typeof e.mount, 'function', e.id + ' mount');
    assert.equal(typeof e.stop, 'function', e.id + ' stop');
    assert.ok(e.labelKey, e.id + ' labelKey');
    for (const [l, b] of Object.entries(bundles)) {
      assert.ok(typeof b[e.labelKey] === 'string' && b[e.labelKey].length > 0, l + ' has ' + e.labelKey);
    }
  }
  assert.equal(vm.runInContext('_AUTOMATION_DEFAULT_SUBTAB', sb), 'inbox', 'default constant');
  assert.equal(subtabOf(sb), 'inbox', 'initial state');
});

test('order and labels: six buttons with the English labels, each exactly t(labelKey); a locale override renames the tab', () => {
  const { sb, out } = loadView();
  sb.renderAutonomousView();
  const row = stripButtons(out.view);
  assert.equal(row.length, 6);
  assert.deepEqual(row.map(b => b.id), IDS);
  row.forEach((b, i) => {
    assert.equal(b.inner.replace(/<span[\s\S]*<\/span>/, ''), LABELS[i], b.id + ' English label');
    assert.equal(b.selected, i === 0 ? 'true' : 'false');
  });
  assert.match(out.view, /<div class="output-tabs automation-subtabs" role="tablist">/, 'wrapper class contract');
  assert.equal((out.view.match(/class="output-tab/g) || []).length >= 6, true);
  assert.equal((out.view.match(/role="tab"/g) || []).length, 6, 'six role=tab');
  assert.equal((out.view.match(/data-subtab="/g) || []).length, 6, 'six data-subtab');

  const reg = registry(sb);
  reg.forEach((e, i) => assert.equal(sb.t(e.labelKey), LABELS[i], e.id + ' label is t(labelKey)'));

  const key = reg[2].labelKey;
  sb._i18n.bundle[key] = 'Compute pool';
  sb.renderAutonomousView();
  const renamed = stripButtons(out.view);
  assert.equal(renamed[2].inner, 'Compute pool', 'overriding the value renames the tab with no code change');
  assert.deepEqual(renamed.map(b => b.id), IDS, 'ids unchanged');
  assert.equal(renamed[0].inner.replace(/<span[\s\S]*<\/span>/, ''), 'Inbox');
});

test('card ownership: each tab body holds exactly its fixed cards then its placeholders, and no card id is in two tabs', () => {
  const { sb, out } = loadView();
  const seen = new Map();
  for (const id of IDS) {
    sb.switchAutomationSubtab(id);
    assert.equal(subtabOf(sb), id);
    const ids = cardIds(bodyOf(out.view));
    assert.deepEqual(ids, [...FIXED[id], ...PLACEHOLDERS[id]], id + ' card ids in order');
    ids.forEach(c => {
      assert.ok(!seen.has(c), c + ' already owned by ' + seen.get(c) + ', also in ' + id);
      seen.set(c, id);
    });
  }
  assert.equal(seen.size, 4 + 2 + 4 + 1 + 3 + 2 + 13, 'total distinct card ids (fixed 16 + placeholders 13)');
  assert.equal(cardIds(bodyOf((sb.switchAutomationSubtab('inbox'), out.view)))[0], 'automationTriageCard', 'triage card is first on Inbox');
});

test('slots: a slot registered on resources appears only there, after the fixed cards, ordered; placeholders are hidden and empty', () => {
  const { sb, out } = loadView();
  assert.equal(typeof sb.registerAutomationCardSlot, 'function');
  assert.equal(sb.registerAutomationCardSlot('resources', { id: 'testSlotLate', order: 500, html: '<div id="testSlotLate"></div>' }), true);
  assert.equal(sb.registerAutomationCardSlot('resources', { id: 'testSlotEarly', order: 50, html: () => '<div id="testSlotEarly"></div>' }), true);
  assert.equal(sb.registerAutomationCardSlot('nosuchtab', { id: 'x', html: '' }), false, 'unknown tab refused');

  for (const id of IDS) {
    sb.switchAutomationSubtab(id);
    const body = bodyOf(out.view);
    if (id === 'resources') {
      const ids = [...body.matchAll(/<div id="([A-Za-z]+)"/g)].map(m => m[1]);
      const at = n => ids.indexOf(n);
      assert.ok(at('testSlotEarly') > at('automationUsageCard'), 'slot comes after the fixed cards');
      assert.ok(at('testSlotEarly') < at('automationLaneUtilizationCard'), 'order 50 sorts before the order-100 placeholders');
      assert.ok(at('automationVerifierSlotsCard') < at('testSlotLate'), 'order 500 sorts last');
      assert.ok(at('automationLaneUtilizationCard') < at('automationVerifierSlotsCard'));
    } else {
      assert.doesNotMatch(body, /testSlot/, id + ' does not hold the resources slot');
    }
  }

  for (const [tab, list] of Object.entries(PLACEHOLDERS)) {
    sb.switchAutomationSubtab(tab);
    const body = bodyOf(out.view);
    for (const p of list) {
      assert.match(body, new RegExp('<div id="' + p + '" class="prd-capacity-slot" style="display:none;[^"]*"></div>'), p + ' is a hidden empty placeholder on ' + tab);
    }
  }
  assert.equal(Object.values(PLACEHOLDERS).flat().length, 13, 'thirteen placeholders');
});

test('inbox badge: summed count from the existing routes, aria-label, hidden at zero, kept when another tab is shown', async () => {
  const data = {
    '/api/autonomous/triage': { records: [{ id: 'a' }, { id: 'b' }] },
    '/api/questions': [{ id: 'q1', text: 'x' }],
    '/api/autonomous/held-questions': { open: [{ id: 'h1' }] },
    '/api/autonomous/security-failures': [],
  };
  const calls = [];
  const fetchImpl = url => {
    const u = String(url);
    calls.push(u);
    const key = Object.keys(data).find(k => u.startsWith(k));
    return Promise.resolve(jsonRes(key ? data[key] : []));
  };
  const { sb, out } = loadView(fetchImpl);
  sb.navigate && sb.navigate('automation');
  sb.renderAutonomousView();
  await flushAsync(30);
  for (const route of ['/api/autonomous/triage', '/api/questions', '/api/autonomous/held-questions']) {
    assert.ok(calls.some(c => c.startsWith(route)), 'fetched ' + route);
  }

  sb.renderAutonomousView();
  let inbox = stripButtons(out.view).find(b => b.id === 'inbox');
  const m = inbox.inner.match(/<span class="automation-subtab-badge" aria-label="([^"]*)" style="([^"]*)">(\d+)<\/span>/);
  assert.ok(m, 'Inbox carries span.automation-subtab-badge: ' + inbox.inner);
  assert.equal(m[3], '4', 'badge is the summed count: 2 triage + 1 question + 1 held question');
  assert.equal(m[1], '4 waiting on you', 'aria-label');
  assert.doesNotMatch(m[2], /display:none/, 'visible when non-zero');

  sb.switchAutomationSubtab('setup');
  inbox = stripButtons(out.view).find(b => b.id === 'inbox');
  assert.equal(inbox.active, false, 'Inbox no longer active');
  assert.match(inbox.inner, /automation-subtab-badge" aria-label="4 waiting on you" style="[^"]*">4</, 'badge stays on Inbox behind Setup');
  for (const b of stripButtons(out.view).filter(b => b.id !== 'inbox')) assert.doesNotMatch(b.inner, /badge/, b.id + ' has no badge');

  // In-place update through the DOM: a stub badge gets the count text and aria-label.
  const attrs = {};
  const badge = { style: {}, textContent: '', setAttribute: (k, v) => { attrs[k] = v; } };
  sb.document.querySelector = sel => (/data-subtab="inbox"\] \.automation-subtab-badge/.test(sel) ? badge : null);
  vm.runInContext('_automationUpdateInboxBadge()', sb);
  assert.equal(badge.textContent, '4');
  assert.equal(attrs['aria-label'], '4 waiting on you');
  assert.equal(badge.style.display, '');
});

test('inbox badge: zero is hidden', () => {
  const { sb, out } = loadView();
  sb.renderAutonomousView();
  const inbox = stripButtons(out.view).find(b => b.id === 'inbox');
  assert.match(inbox.inner, /<span class="automation-subtab-badge" aria-label="0 waiting on you" style="display:none;">0<\/span>/, 'zero count is hidden but present');
});

test('strip behaviour: style.css rules for the sliding strip', () => {
  const block = STYLE.match(/\.automation-subtabs \{([^}]*)\}/);
  assert.ok(block, '.automation-subtabs rule exists');
  for (const decl of ['overflow-x: auto', '-webkit-overflow-scrolling: touch', 'scrollbar-width: none', 'position: sticky', 'flex-wrap: nowrap']) {
    assert.ok(block[1].includes(decl), '.automation-subtabs has ' + decl);
  }
  assert.match(STYLE, /\.automation-subtabs::-webkit-scrollbar\s*\{\s*display:\s*none;?\s*\}/, '::-webkit-scrollbar hidden');
  const tab = STYLE.match(/\.automation-subtabs \.output-tab \{([^}]*)\}/);
  assert.ok(tab, '.automation-subtabs .output-tab rule exists');
  assert.match(tab[1], /flex:\s*0 0 auto/);
  assert.match(tab[1], /white-space:\s*nowrap/);
  const phone = STYLE.match(/@media \(max-width: 600px\), \(pointer: coarse\) \{\s*\.automation-subtabs \.output-tab \{([^}]*)\}/);
  assert.ok(phone, 'phone block exists');
  assert.match(phone[1], /min-height:\s*40px/, 'phone keeps min-height 40px');
});

test('strip behaviour: switching tabs calls _automationSubtabsReveal on the active button', () => {
  const { sb } = loadView();
  const calls = [];
  const btn = makeStubElement();
  btn.scrollIntoView = opts => calls.push(opts);
  const queried = [];
  sb.document.querySelector = sel => { queried.push(sel); return sel === '.automation-subtabs .output-tab.active' ? btn : null; };
  sb.renderAutonomousView();
  assert.equal(calls.length, 1, 'the first render reveals the active button once');
  sb.switchAutomationSubtab('progress');
  assert.equal(calls.length, 2, 'switching reveals again');
  assert.deepEqual({ ...calls[1] }, { block: 'nearest', inline: 'center', behavior: 'smooth' });
  assert.ok(queried.includes('.automation-subtabs .output-tab.active'), 'the active button is the one queried');
  sb.switchAutomationSubtab('bogus');
  assert.equal(calls.length, 2, 'an unknown id does not reveal');
  assert.equal(typeof sb._automationSubtabsReveal, 'function');
});

test('switching: unknown id ignored, previous stop() runs, only the selected tab mounts', () => {
  const { sb } = loadView();
  const reg = registry(sb);
  const log = [];
  const orig = reg.map(e => ({ mount: e.mount, stop: e.stop }));
  reg.forEach(e => {
    e.mount = () => log.push('mount:' + e.id);
    e.stop = () => log.push('stop:' + e.id);
  });
  try {
    sb.renderAutonomousView();
    assert.deepEqual(log, ['mount:inbox'], 'first render mounts only Inbox');
    log.length = 0;

    sb.switchAutomationSubtab('no-such-tab');
    assert.deepEqual(log, [], 'unknown id: nothing runs');
    assert.equal(subtabOf(sb), 'inbox');

    sb.switchAutomationSubtab('resources');
    assert.deepEqual(log.slice(0, 2), ['stop:inbox', 'mount:resources'], 'previous stop runs, then the new tab mounts');
    assert.equal(log.filter(x => x.startsWith('mount:')).filter(x => x !== 'mount:resources').length, 0, 'no other tab mounts');
    assert.equal(subtabOf(sb), 'resources');
    log.length = 0;

    sb.switchAutomationSubtab('housekeeping');
    assert.equal(log[0], 'stop:resources');
    assert.ok(log.includes('mount:housekeeping'));
    assert.ok(!log.includes('mount:setup') && !log.includes('mount:progress'));
    log.length = 0;

    sb.switchAutomationSubtab('housekeeping');
    assert.ok(!log.includes('stop:housekeeping'), 'reselecting the same tab does not stop it');
  } finally {
    reg.forEach((e, i) => { e.mount = orig[i].mount; e.stop = orig[i].stop; });
  }
});
