// Automata phone layout — tab strips scroll sideways instead of wrapping.
//
// Reads style.css and app.js as source: inside the phone media query the
// Automata top strip, the Automation sub-tabs, the Projects strip and the
// other fixed strips must not wrap, must scroll horizontally and must carry an
// edge-fade rule. The shared reveal helper is checked in source and run in the
// sandbox, and the Automata header title must be short enough for a phone.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
const appSource = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));

const PHONE_QUERY = '@media (max-width: 600px), (pointer: coarse)';

// cssBlocks returns every top-level block of the stylesheet that starts with
// the given at-rule header, with its body (balanced braces).
function atRuleBodies(header) {
  const bodies = [];
  let from = 0;
  for (;;) {
    const i = css.indexOf(header, from);
    if (i < 0) break;
    const open = css.indexOf('{', i);
    let depth = 0, j = open;
    for (; j < css.length; j++) {
      if (css[j] === '{') depth++;
      else if (css[j] === '}' && --depth === 0) break;
    }
    bodies.push(css.slice(open + 1, j));
    from = j;
  }
  return bodies;
}

// rulesFor returns the declaration text of every rule in `scope` whose
// selector list contains exactly `selector`.
function rulesFor(scope, selector) {
  const out = [];
  const re = /([^{}]+)\{([^{}]*)\}/g;
  let m;
  while ((m = re.exec(scope))) {
    const sels = m[1].split(',').map(s => s.trim());
    if (sels.includes(selector)) out.push(m[2]);
  }
  return out;
}

const phoneScope = atRuleBodies(PHONE_QUERY).join('\n');
const wholeCss = css;

test('phone media query exists', () => {
  assert.ok(phoneScope.length > 0);
});

// The strips fixed for phone widths. Each one scrolls in the phone query or
// in its base rule, never wraps, and fades the side that still hides tabs.
const STRIPS = ['.automata-tabs-bar', '.automation-subtabs', '.projects-tabs-bar', '.tab-strip-scroll'];

for (const sel of STRIPS) {
  test(`${sel} does not wrap and scrolls sideways`, () => {
    const decls = rulesFor(wholeCss, sel).join('\n');
    assert.ok(decls.length > 0, `${sel} has a rule`);
    assert.doesNotMatch(decls, /flex-wrap:\s*wrap\b(?!-)/);
    assert.match(decls, /overflow-x:\s*auto/);
    assert.match(decls, /flex-wrap:\s*nowrap/);
  });

  test(`${sel} has an edge-fade rule`, () => {
    assert.match(wholeCss, new RegExp(sel.replace('.', '\\.') + '\\.tabs-more-right'));
    assert.match(wholeCss, new RegExp(sel.replace('.', '\\.') + '\\.tabs-more-left'));
    const fade = rulesFor(wholeCss, sel + '.tabs-more-right').join('\n');
    assert.match(fade, /mask-image:\s*linear-gradient/);
  });
}

test('Automata top strip and Projects strip are nowrap inside the phone query', () => {
  for (const sel of ['.automata-tabs-bar', '.projects-tabs-bar']) {
    const decls = rulesFor(phoneScope, sel).join('\n');
    assert.match(decls, /flex-wrap:\s*nowrap/, sel);
    assert.match(decls, /overflow-x:\s*auto/, sel);
    assert.doesNotMatch(decls, /flex-wrap:\s*wrap\b(?!-)/, sel);
  }
});

test('phone query keeps 40 px tap targets on the strips', () => {
  for (const sel of ['.automata-tabs-bar .output-tab', '.automation-subtabs .output-tab', '.prd-detail-tabs .output-tab']) {
    const decls = rulesFor(phoneScope, sel).concat(rulesFor(phoneScope, sel.replace(' .output-tab', ' .output-tab'))).join('\n');
    assert.match(decls, /min-height:\s*40px/, sel);
  }
});

test('shared reveal helper scrolls the active tab into view, with a scrollLeft fallback', () => {
  const start = appSource.indexOf('function _tabStripReveal(');
  assert.ok(start >= 0, '_tabStripReveal exists');
  const body = appSource.slice(start, appSource.indexOf('function _tabStripFade(', start));
  assert.match(body, /\.output-tab\.active/);
  assert.match(body, /typeof btn\.scrollIntoView === 'function'/);
  assert.match(body, /btn\.scrollIntoView\(/);
  assert.match(body, /\.scrollLeft\s*=/);
  assert.match(body, /_tabStripFade\(/);
});

function loadSandbox() {
  const sandbox = buildSandbox({ byId: {} });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

function fakeStrip(btn) {
  const strip = {
    scrollLeft: 0, clientWidth: 200, scrollWidth: 600,
    classes: new Set(),
    classList: { toggle(n, on) { on ? strip.classes.add(n) : strip.classes.delete(n); } },
    querySelector: () => btn,
    addEventListener() {},
  };
  return strip;
}

test('reveal helper calls scrollIntoView when the element has it', () => {
  const sandbox = loadSandbox();
  const calls = [];
  const btn = { scrollIntoView: (o) => calls.push(o), offsetLeft: 300, offsetWidth: 80 };
  const strip = fakeStrip(btn);
  sandbox.__strip = strip;
  vm.runInContext('_tabStripReveal(__strip)', sandbox);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].inline, 'center');
  assert.equal(strip.scrollLeft, 0, 'scrollLeft untouched when scrollIntoView is used');
  assert.ok(strip.classes.has('tabs-more-right'));
});

test('reveal helper falls back to scrollLeft without scrollIntoView', () => {
  const sandbox = loadSandbox();
  const btn = { offsetLeft: 300, offsetWidth: 80 };
  const strip = fakeStrip(btn);
  sandbox.__strip = strip;
  vm.runInContext('_tabStripReveal(__strip)', sandbox);
  assert.equal(strip.scrollLeft, 300 - (200 - 80) / 2);
});

test('Automata header title is short enough for a phone', () => {
  const title = en.nav_automata;
  assert.equal(typeof title, 'string');
  assert.ok(title.length > 0 && title.length <= 12, `"${title}" is ${title.length} characters`);
});
