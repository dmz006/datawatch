// Dashboard stat bar phone layout — the bar wraps so Edit and + Card stay visible.
//
// Reads style.css (comments stripped) and app.js as source. On a phone the
// .dboard-stat-bar must wrap onto several rows, and nothing may hide the Edit
// (#dashEditBtn) and + Card (#dashAddCardBtn) buttons through overflow.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');

const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
const appSource = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');

const PHONE_QUERY = '@media (max-width:600px)';

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
const baseScope = css.replace(/@media[^{]*\{(?:[^{}]*\{[^{}]*\})*[^{}]*\}/g, '');

test('phone media query exists', () => {
  assert.ok(phoneScope.length > 0);
});

test('stat bar wraps at phone width', () => {
  const decls = rulesFor(phoneScope, '.dboard-stat-bar').join('\n');
  assert.match(decls, /flex-wrap:\s*wrap\b(?!-)/);
  assert.match(decls, /row-gap:\s*\d+px/);
});

test('stat bar is not a nowrap or scrolling row on phone', () => {
  const decls = rulesFor(phoneScope, '.dboard-stat-bar').join('\n');
  assert.doesNotMatch(decls, /flex-wrap:\s*nowrap/);
  assert.doesNotMatch(decls, /overflow(-x)?:\s*(hidden|auto|scroll)/);
});

test('stat bar base rule is unchanged for desktop', () => {
  const decls = rulesFor(baseScope, '.dboard-stat-bar').join('\n');
  assert.match(decls, /display:\s*flex/);
  assert.doesNotMatch(decls, /flex-wrap/);
  assert.doesNotMatch(decls, /overflow/);
});

for (const id of ['#dashEditBtn', '#dashAddCardBtn']) {
  test(`${id} cannot shrink and is not clipped on phone`, () => {
    const decls = rulesFor(phoneScope, `.dboard-stat-bar ${id}`).join('\n');
    assert.match(decls, /flex-shrink:\s*0/);
    assert.doesNotMatch(decls, /display:\s*none/);
    assert.doesNotMatch(decls, /visibility:\s*hidden/);
  });

  test(`${id} is a direct child of the stat bar in the markup`, () => {
    const start = appSource.indexOf('<div class="dboard-stat-bar">');
    assert.ok(start >= 0, 'stat bar markup exists');
    const end = appSource.indexOf('id="dashFedErrorBanner"', start);
    const markup = appSource.slice(start, end);
    assert.ok(markup.includes(`id="${id.slice(1)}"`), `${id} is inside the stat bar`);
  });
}

test('no ancestor rule clips the stat bar horizontally on phone', () => {
  const decls = rulesFor(phoneScope, '.dboard-cards').join('\n');
  assert.doesNotMatch(decls, /overflow-x:\s*hidden/);
});
