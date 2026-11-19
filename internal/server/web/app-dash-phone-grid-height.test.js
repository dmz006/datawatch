// Dashboard phone layout — the card grid must not be a short nested scroll box.
//
// Reads style.css as source. At phone width (@media (max-width:600px)) the
// .dboard-cards wrapper must size to its content (no height:100% of the
// padded view, no overflow:hidden) and the .dboard-card-grid must not clip or
// scroll on its own, so the Dashboard scrolls as one page inside .view. The
// floating-button clearance (bottom padding on .view) must remain, and the
// desktop rules must stay unchanged.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');

const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');

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

function decl(rules, prop) {
  let val = null;
  for (const r of rules) {
    const m = new RegExp('(?:^|[;\\s])' + prop + '\\s*:\\s*([^;]+)').exec(r);
    if (m) val = m[1].trim();
  }
  return val;
}

const phone = atRuleBodies(PHONE_QUERY).join('\n');
const whole = css.replace(/@media[^{]*\{(?:[^{}]*\{[^{}]*\})*[^{}]*\}/g, '');

test('phone media query has dashboard rules', () => {
  assert.ok(phone.length > 0);
  assert.ok(rulesFor(phone, '.dboard-cards').length > 0, '.dboard-cards phone rule');
  assert.ok(rulesFor(phone, '.dboard-card-grid').length > 0, '.dboard-card-grid phone rule');
});

test('phone .dboard-cards is no longer a fixed-height clipped box', () => {
  const r = rulesFor(phone, '.dboard-cards');
  assert.equal(decl(r, 'height'), 'auto');
  assert.equal(decl(r, 'min-height'), '100%');
  assert.equal(decl(r, 'overflow'), 'visible');
});

test('phone .dboard-card-grid does not scroll or clip on its own', () => {
  const r = rulesFor(phone, '.dboard-card-grid');
  assert.equal(decl(r, 'overflow'), 'visible');
  assert.equal(decl(r, 'flex'), 'none');
});

test('phone grid keeps its columns and row sizing', () => {
  const r = rulesFor(phone, '.dboard-card-grid');
  assert.match(decl(r, 'grid-template-columns'), /repeat\(2,1fr\)/);
  assert.ok(decl(r, 'grid-auto-rows'));
});

test('floating-button bottom clearance is still present on .view', () => {
  const r = rulesFor(whole, '.view');
  const pb = decl(r, 'padding-bottom');
  assert.ok(pb, '.view padding-bottom present');
  assert.match(pb, /56px/);
  assert.match(pb, /var\(--nav-h\)/);
  assert.equal(decl(r, 'overflow-y'), 'auto');
});

test('phone rules do not zero the view bottom clearance', () => {
  for (const sel of ['.dboard-cards', '.dboard-card-grid']) {
    const r = rulesFor(phone, sel);
    const pb = decl(r, 'padding-bottom');
    assert.ok(pb === null || !/^0(px)?$/.test(pb), sel + ' must not zero padding-bottom');
  }
});

test('desktop dashboard rules are unchanged', () => {
  const cards = rulesFor(whole, '.dboard-cards');
  assert.equal(decl(cards, 'height'), '100%');
  assert.equal(decl(cards, 'overflow'), 'hidden');
  const grid = rulesFor(whole, '.dboard-card-grid');
  assert.equal(decl(grid, 'overflow-y'), 'auto');
  assert.equal(decl(grid, 'flex'), '1');
});
