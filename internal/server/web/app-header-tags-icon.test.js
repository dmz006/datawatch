// Header Tags icon — the button's svg is a filled drawing whose colour comes
// only from theme tokens (no hex or rgb/rgba literals in index.html or the
// .header-tags-icon* rules of style.css). Covers: the button markup, token-only
// colours, WCAG contrast of the body fill against the header background in the
// dark and the light theme, the lighter hole + darker edge stroke, hover /
// :focus-visible / is-active states, the _tagsMenuRefresh is-active + label
// behaviour, and the phone tap target + empty badge rules.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const vm = require('vm');
const { buildSandbox, loadScript } = require('./testutil_browser_stub');

const WEB = __dirname;
const html = fs.readFileSync(path.join(WEB, 'index.html'), 'utf8');
const css = fs.readFileSync(path.join(WEB, 'style.css'), 'utf8');

// --- markup helpers ---------------------------------------------------------

function buttonTag() {
  const i = html.indexOf('id="headerTagsBtn"');
  assert.ok(i > 0, 'index.html has the Tags button');
  const start = html.lastIndexOf('<button', i);
  const end = html.indexOf('</button>', i);
  return html.slice(start, end + '</button>'.length);
}

// ruleFor returns the declaration body (text between the braces) of the first
// rule whose selector starts at an occurrence of `sel`; skip is an optional
// regex tested against the text right after `sel` to skip variants like
// ":empty". Only the matched rule's own body is returned.
function ruleFor(sel, skip = null) {
  let i = css.indexOf(sel);
  while (i >= 0) {
    if (!skip || !skip.test(css[i + sel.length])) break;
    i = css.indexOf(sel, i + sel.length);
  }
  assert.ok(i >= 0, 'style.css has a rule for ' + sel);
  const open = css.indexOf('{', i);
  const close = css.indexOf('}', open);
  const body = css.slice(open + 1, close).trim();
  assert.ok(body && !body.includes('{'), 'a real declaration body for ' + sel);
  return body;
}

// allBodiesFor returns the declaration bodies of every rule whose selector
// mentions the class (base + hover + is-active + transition rules).
function allBodiesFor(needle) {
  const out = [];
  let i = css.indexOf(needle);
  while (i >= 0) {
    const open = css.indexOf('{', i);
    if (open < 0) break;
    const prevClose = css.lastIndexOf('}', i);
    if (i > prevClose) {
      const close = css.indexOf('}', open);
      const body = css.slice(open + 1, close).trim();
      if (body && !body.includes('{')) out.push(body);
    }
    i = css.indexOf(needle, open + 1);
  }
  return out;
}

function tokensOf(blockSel) {
  // the theme blocks are quoted selectors, so make sure the selector is
  // followed by whitespace before the opening brace (skips comment mentions)
  const body = blockSel.startsWith('[') ? ruleFor(blockSel, /[^\s]/) : ruleFor(blockSel);
  const t = {};
  for (const m of body.matchAll(/--([a-zA-Z0-9]+)\s*:\s*([^;]+)/g)) {
    const v = m[2].trim().split('/*')[0].trim();
    t[m[1]] = v;
  }
  return t;
}

// --- WCAG colour math (linearized sRGB relative luminance) ------------------

function hexToRgb(hex) {
  let h = hex.trim().replace(/^#/, '');
  if (h.length === 3) h = h.split('').map(c => c + c).join('');
  assert.match(h, /^[0-9a-fA-F]{6}$/, 'expected a hex colour, got ' + hex);
  return [parseInt(h.slice(0, 2), 16), parseInt(h.slice(2, 4), 16), parseInt(h.slice(4, 6), 16)];
}

function channelLum(c8) {
  const c = c8 / 255;
  return c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
}

function luminance(rgb) {
  return 0.2126 * channelLum(rgb[0]) + 0.7152 * channelLum(rgb[1]) + 0.0722 * channelLum(rgb[2]);
}

function contrast(a, b) {
  const la = luminance(a), lb = luminance(b);
  const [hi, lo] = la >= lb ? [la, lb] : [lb, la];
  return (hi + 0.05) / (lo + 0.05);
}

const HEX_RE = /#[0-9a-fA-F]{3,8}\b/;

// --- app.js helper ----------------------------------------------------------

function makeBtn() {
  const classes = new Set();
  const attrs = {};
  return {
    classes,
    classList: {
      contains: c => classes.has(c),
      add: c => classes.add(c),
      remove: c => classes.delete(c),
      toggle(c, force) {
        if (force === undefined) { classes.has(c) ? classes.delete(c) : classes.add(c); }
        else if (force) classes.add(c); else classes.delete(c);
        return classes.has(c);
      },
    },
    setAttribute(k, v) { attrs[k] = String(v); },
    getAttribute(k) { return k in attrs ? attrs[k] : null; },
  };
}

function loadApp() {
  const btn = makeBtn();
  const chip = { textContent: '' };
  const sandbox = buildSandbox({ byId: { headerTagsBtn: btn, headerTagsCount: chip } });
  sandbox.fetch = () => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ tags: [] }) });
  vm.createContext(sandbox);
  loadScript(path.join(WEB, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  // Keep the redraw path inert so refresh only exercises button/chip state.
  vm.runInContext('globalThis._tagsMenuHTML = () => "<p>menu</p>";', sandbox);
  const run = code => vm.runInContext(code, sandbox);
  return { sb: sandbox, btn, chip, run };
}

// --- tests ------------------------------------------------------------------

test('the Tags button line holds one header-tags-icon svg plus the count badge and full wiring', () => {
  const tag = buttonTag();
  assert.equal((tag.match(/<svg/g) || []).length, 1, 'exactly one svg in the button');
  const icon = tag.slice(tag.indexOf('<svg'), tag.indexOf('</svg>') + 6);
  assert.match(icon, /class="header-tags-icon"/);
  assert.match(icon, /aria-hidden="true"/);
  assert.match(icon, /class="header-tags-icon-body"/, 'the filled body path is present');
  assert.match(icon, /class="header-tags-icon-hole"/, 'the hole is present');
  for (const needle of [
    'id="headerTagsBtn"',
    'onclick="toggleTagsMenu()"',
    'aria-haspopup="dialog"',
    'aria-expanded="false"',
    'aria-controls="headerTagsMenu"',
    'id="headerTagsCount"',
    'class="header-tags-count"',
  ]) assert.ok(tag.includes(needle), 'button keeps ' + needle);
});

test('the icon svg and every .header-tags-icon* rule use var(--...) tokens with no hex or rgb()/rgba() literals', () => {
  const tag = buttonTag();
  const icon = tag.slice(tag.indexOf('<svg'), tag.indexOf('</svg>') + 6);
  assert.ok(!HEX_RE.test(icon), 'svg markup has no hex literals');
  assert.ok(!/rgba?\(/.test(icon), 'svg markup has no rgb()/rgba()');
  assert.ok(!icon.includes('currentColor'), 'svg markup is not currentColor-outlined');
  const bodies = allBodiesFor('.header-tags-icon');
  assert.ok(bodies.length >= 4, 'base, body, hole and shine rules exist in style.css');
  for (const decl of bodies) {
    assert.ok(!HEX_RE.test(decl), 'no hex literal in: ' + decl);
    assert.ok(!decl.match(/^rgba?\(/), 'no rgb()/rgba() literal in: ' + decl);
    assert.ok(!/currentColor/.test(decl), 'no currentColor in: ' + decl);
  }
  for (const sel of ['.header-tags-icon-body', '.header-tags-icon-hole', '.header-tags-icon-shine']) {
    assert.match(ruleFor(sel), /var\(--[a-z0-9]+\)/i, sel + ' paints with a var(--...) token');
  }
  assert.match(ruleFor('.header-tags-icon'), /display:\s*block/, 'inline icon svg stays block-level');
});

test('the body fill clears the 3:1 UI contrast floor against the header background in BOTH the dark and the light theme', () => {
  const bodyRule = ruleFor('.header-tags-icon-body');
  const fillM = bodyRule.match(/fill:\s*var\((--[a-z0-9]+)\)/i);
  assert.ok(fillM, 'the first .header-tags-icon-body rule names a fill token');
  const fillTok = fillM[1];
  const names = [];
  const ratios = {};
  for (const [name, sel] of [['dark (:root)', ':root'], ['light ([data-theme="light"])', '[data-theme="light"]']]) {
    const toks = tokensOf(sel);
    assert.ok(toks[fillTok.slice(2)], name + ' defines ' + fillTok);
    assert.ok(toks.bg2, name + ' defines --bg2 (the .header background)');
    const r = contrast(hexToRgb(toks[fillTok.slice(2)]), hexToRgb(toks.bg2));
    ratios[name] = r;
    names.push(name + ' ' + toks[fillTok.slice(2)] + ' on ' + toks.bg2 + ' = ' + r.toFixed(2) + ':1');
    assert.ok(r >= 3, name + ' contrast ' + r.toFixed(2) + ':1 is below 3:1');
  }
  console.log('tags-icon contrast (dark & light):', names.join('; '), JSON.stringify(ratios));
});

test('the hole fill is lighter than the body fill and the body carries a darker edge stroke', () => {
  const bodyRule = ruleFor('.header-tags-icon-body');
  assert.match(bodyRule, /stroke:\s*color-mix\(in srgb,\s*var\(--accent2\)\s+70%?,\s*var\(--bg\)/,
    'the body stroke mixes the accent toward the (dark/light) --bg base for a darker edge');
  assert.match(bodyRule, /stroke-width:\s*1/, 'the body edge stroke is visible');
  const holeRule = ruleFor('.header-tags-icon-hole');
  const m = holeRule.match(/color-mix\(in srgb,\s*var\((--[a-z0-9]+)\)\s+(\d+)%\s*,\s*var\((--[a-z0-9]+)\)/);
  assert.ok(m, 'the hole mixes the accent toward a second token');
  const pct = Number(m[2]) / 100; // the listed mix weight of the accent
  // In the base (dark) theme the --text mixing base is the light colour, so
  // the hole must resolve lighter than the body there.
  const toks = tokensOf(':root');
  const accent = hexToRgb(toks[m[1].slice(2)]);
  const base = hexToRgb(toks[m[3].slice(2)]);
  const hole = [0, 1, 2].map(i => Math.round(accent[i] * pct + base[i] * (1 - pct)));
  const body = hexToRgb(toks.accent2);
  assert.ok(luminance(hole) > luminance(body),
    'dark-theme hole ' + JSON.stringify(hole) + ' is not lighter than the body ' + JSON.stringify(body));
  assert.match(holeRule, /var\(--text\)/, 'the hole blends toward the --text base (the lighter mixing color)');
});

test('hover and :focus-visible give #headerTagsBtn a visible ring and an is-active rule exists', () => {
  const hover = ruleFor('#headerTagsBtn:hover');
  assert.equal(hover.includes('color-mix(in srgb, var(--accent2) 12%, transparent)'), true, 'hover paints a visible background');
  const focus = ruleFor('#headerTagsBtn:focus-visible');
  const out = focus.match(/outline:\s*(\d+(?:\.\d+)?)px/);
  assert.ok(out, ':focus-visible carries an outline');
  assert.ok(Number(out[1]) >= 2, 'the focus outline is at least 2px wide');
  assert.match(focus, /var\(--accent2\)/, 'the focus ring is token-coloured');
  const active = ruleFor('#headerTagsBtn.is-active');
  assert.match(active, /background:\s*color-mix\(in srgb,\s*var\(--accent2\)\s+18%?,\s*transparent\)/, 'is-active has its own visible fill');
});

test('_tagsMenuRefresh toggles is-active with the menu or an active filter, and keeps aria-expanded / count / label in sync', () => {
  const h = loadApp();
  const label = h.sb.t('tags_menu_button');
  // Neither menu open nor a filter: is-active off, aria-expanded false, empty badge.
  h.sb._tagsMenuRefresh();
  assert.equal(h.btn.classList.contains('is-active'), false, 'no is-active with menu closed and count 0');
  assert.equal(h.btn.getAttribute('aria-expanded'), 'false');
  assert.equal(h.chip.textContent, '');
  assert.equal(h.btn.getAttribute('aria-label'), label, 'aria-label comes from t(\'tags_menu_button\')');
  assert.equal(h.btn.getAttribute('title'), label, 'title comes from t(\'tags_menu_button\')');
  // Menu open with an empty count: is-active on, aria-expanded true.
  h.run('_tagsMenuOpen = true;');
  h.sb._tagsMenuRefresh();
  assert.equal(h.btn.classList.contains('is-active'), true, 'is-active on while the menu is open');
  assert.equal(h.btn.getAttribute('aria-expanded'), 'true');
  h.run('_tagsMenuOpen = false;');
  h.sb._tagsMenuRefresh();
  assert.equal(h.btn.classList.contains('is-active'), false, 'is-active off once the menu closes with no filter');
  // A watch filter on (count 1) with the menu closed: is-active on again, badge shows 1.
  h.run('state.sessionWatchFilter = true;');
  h.sb._tagsMenuRefresh();
  assert.equal(h.btn.classList.contains('is-active'), true, 'is-active on while a filter is active');
  assert.equal(h.chip.textContent, '1');
  assert.equal(h.btn.getAttribute('aria-label'), label + ' (1)');
  h.run('state.sessionWatchFilter = false;');
  h.sb._tagsMenuRefresh();
  assert.equal(h.btn.classList.contains('is-active'), false, 'is-active off when the last filter is cleared');
  assert.equal(h.chip.textContent, '');
});

test('the phone tap target selector group keeps #headerTagsBtn at >=40px (44px) and the badge rule still hides an empty badge', () => {
  const groupM = css.match(/\.header > #headerTagsBtn,[\s\S]*?min-width:\s*(\d+)px;[\s\S]*?min-height:\s*(\d+)px;/);
  assert.ok(groupM, 'the shared tap-target selector group still lists #headerTagsBtn');
  assert.ok(Number(groupM[1]) >= 40, 'min-width ' + groupM[1] + 'px is below the 40px floor');
  assert.ok(Number(groupM[2]) >= 40, 'min-height ' + groupM[2] + 'px is below the 40px floor');
  assert.match(css, /\.header-tags-count:empty\s*\{\s*display:\s*none\s*;?\s*\}/, 'the empty badge is still hidden');
  const badge = ruleFor('.header-tags-count', /:/);
  assert.match(badge, /position:\s*absolute/);
  assert.match(badge, /background:\s*var\(--accent2\)/);
});
