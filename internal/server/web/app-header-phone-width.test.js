'use strict';
// Header at phone width — the server button and the scope button must leave the
// header a single non-wrapping line, with the title taking the spare room.
//
// This file reads index.html and style.css as text (no browser, no daemon, no
// tmux): it strips the comments, parses the CSS rules and their @media blocks,
// and works out what each header control gets at a 320 px phone and at a wider
// screen. It pins:
//   (a) the header is one flex line that never wraps;
//   (b) the title grows, has min-width 0 and truncates with an ellipsis;
//   (c) the server and scope buttons are icon-only below the phone breakpoint
//       and the server short name shows above it;
//   (d) no control carries a fixed width that can push the others off a 320 px
//       screen: the fixed widths of the controls visible together on the
//       Automata view stay under 320 minus the title's minimum;
//   (e) both icon buttons are real <button type="button"> elements with an
//       aria-label, and the header holds each of them exactly once.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-header-phone-width.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');

const WEB = __dirname;
const INDEX_HTML = fs.readFileSync(path.join(WEB, 'index.html'), 'utf8');
const STYLE_CSS = fs.readFileSync(path.join(WEB, 'style.css'), 'utf8');
const APP_JS = fs.readFileSync(path.join(WEB, 'app.js'), 'utf8');

const PHONE = { width: 320, height: 640, coarse: true };
const WIDE = { width: 700, height: 900, coarse: false };
// The title may shrink to nothing (min-width 0), so the budget keeps a small
// floor for it: room for a few characters and the ellipsis.
const TITLE_MIN = 40;
const SCREEN = 320;

// ── CSS reading ────────────────────────────────────────────────────────────

function stripCssComments(css) { return css.replace(/\/\*[\s\S]*?\*\//g, ''); }

function splitTop(s, sep) {
  const out = [];
  let depth = 0;
  let cur = '';
  for (const ch of s) {
    if (ch === '(') depth++;
    if (ch === ')') depth--;
    if (ch === sep && depth === 0) { out.push(cur); cur = ''; } else cur += ch;
  }
  out.push(cur);
  return out;
}

function parseDecls(body) {
  const decls = {};
  splitTop(body, ';').forEach(d => {
    const i = d.indexOf(':');
    if (i < 0) return;
    const prop = d.slice(0, i).trim().toLowerCase();
    let value = d.slice(i + 1).trim();
    const important = /!\s*important\s*$/i.test(value);
    value = value.replace(/!\s*important\s*$/i, '').trim();
    if (prop) decls[prop] = { value, important };
  });
  return decls;
}

// parseRules returns [{ media, selectors, decls, order }] for every style rule,
// flattening @media blocks (media is the query text, '' at the top level).
// Other at-rules (@keyframes, @font-face, @supports) are skipped.
function parseRules(src, media, acc) {
  acc = acc || [];
  let i = 0;
  while (i < src.length) {
    const open = src.indexOf('{', i);
    if (open < 0) break;
    const head = src.slice(i, open).trim();
    let depth = 1;
    let j = open + 1;
    while (j < src.length && depth) {
      if (src[j] === '{') depth++;
      else if (src[j] === '}') depth--;
      j++;
    }
    const body = src.slice(open + 1, j - 1);
    if (/^@media\b/i.test(head)) parseRules(body, head.replace(/^@media\s*/i, '').trim(), acc);
    else if (!head.startsWith('@')) {
      acc.push({ media, selectors: splitTop(head, ',').map(s => s.trim()).filter(Boolean), decls: parseDecls(body), order: acc.length });
    }
    i = j;
  }
  return acc;
}

function evalFeature(f, env) {
  let m = f.match(/^max-width\s*:\s*(\d+)px$/);
  if (m) return env.width <= Number(m[1]);
  m = f.match(/^min-width\s*:\s*(\d+)px$/);
  if (m) return env.width >= Number(m[1]);
  m = f.match(/^max-height\s*:\s*(\d+)px$/);
  if (m) return env.height <= Number(m[1]);
  m = f.match(/^min-height\s*:\s*(\d+)px$/);
  if (m) return env.height >= Number(m[1]);
  if (/^pointer\s*:\s*coarse$/.test(f)) return env.coarse;
  if (/^hover\s*:\s*hover$/.test(f)) return !env.coarse;
  return false; // reduced-motion, print, dark scheme ...: not the phone/desktop cases here
}

function mediaMatches(query, env) {
  if (!query) return true;
  return splitTop(query, ',').some(part => {
    let p = part.trim().toLowerCase();
    let negate = false;
    if (/^not\s/.test(p)) { negate = true; p = p.replace(/^not\s+/, ''); }
    const feats = [...p.matchAll(/\(([^)]*)\)/g)].map(m => m[1].trim());
    const ok = feats.length > 0 && feats.every(f => evalFeature(f, env));
    return negate ? !ok : ok;
  });
}

const RULES = parseRules(stripCssComments(STYLE_CSS), '');

// matchSpec returns the specificity of selector sel for element el, or -1. Only
// selectors whose last compound names the element's id or class, with no
// pseudo-class or attribute part, are considered; an ancestor part may only be
// the header itself or the app shell.
function matchSpec(sel, el) {
  const parts = sel.trim().split(/\s*[>+~]\s*|\s+/).filter(Boolean);
  const last = parts.pop();
  if (!last || /[:\[]/.test(last)) return -1;
  const ids = [...last.matchAll(/#([\w-]+)/g)].map(m => m[1]);
  const cls = [...last.matchAll(/\.([\w-]+)/g)].map(m => m[1]);
  const tag = (last.match(/^[a-zA-Z][\w-]*/) || [''])[0].toLowerCase();
  if (!ids.length && !cls.length && !tag) return -1;
  if (ids.some(x => x !== el.id)) return -1;
  if (cls.some(c => el.classes.indexOf(c) < 0)) return -1;
  if (tag && tag !== el.tag) return -1;
  let spec = ids.length * 100 + cls.length * 10 + (tag ? 1 : 0);
  for (const a of parts) {
    if (a === '.header' || a === 'header') spec += (a === '.header' ? 10 : 1);
    else if (a === '.app') spec += 10;
    else if (a === 'body' || a === 'html') spec += 1;
    else return -1;
  }
  return spec;
}

// computed(el, env) -> { prop: value } for the rules that apply at env.
function computed(el, env) {
  const hits = [];
  RULES.forEach(r => {
    if (!mediaMatches(r.media, env)) return;
    let best = -1;
    r.selectors.forEach(s => { best = Math.max(best, matchSpec(s, el)); });
    if (best >= 0) hits.push({ r, spec: best });
  });
  const out = {};
  const rank = {};
  hits.forEach(({ r, spec }) => {
    Object.keys(r.decls).forEach(p => {
      const d = r.decls[p];
      const key = [d.important ? 1 : 0, spec, r.order];
      const prev = rank[p];
      if (!prev || key[0] > prev[0] || (key[0] === prev[0] && (key[1] > prev[1] || (key[1] === prev[1] && key[2] >= prev[2])))) {
        rank[p] = key;
        out[p] = d.value;
      }
    });
  });
  return out;
}

function px(v) {
  if (String(v || '').trim() === '0') return 0;
  const m = /^(-?\d+(?:\.\d+)?)px$/.exec(String(v || '').trim());
  return m ? Number(m[1]) : null;
}
function hPadding(v) {
  if (v === undefined) return 0;
  const p = String(v).trim().split(/\s+/);
  const right = p.length === 1 ? p[0] : p[1];
  const left = p.length === 4 ? p[3] : right;
  const r = px(right) !== null ? px(right) : (right === '0' ? 0 : null);
  const l = px(left) !== null ? px(left) : (left === '0' ? 0 : null);
  return r === null || l === null ? 0 : r + l;
}

// ── HTML reading ───────────────────────────────────────────────────────────

const VOID = new Set(['br', 'hr', 'img', 'input', 'meta', 'link']);

function parseAttrs(src) {
  const a = {};
  const re = /([^\s=\/]+)(?:="([^"]*)")?/g;
  let m;
  while ((m = re.exec(src))) a[m[1]] = m[2] === undefined ? '' : m[2];
  return a;
}

// withoutHtmlComments returns the markup with every comment cut out. A comment
// that is never closed stays.
function withoutHtmlComments(html) {
  let out = '';
  let from = 0;
  for (;;) {
    const open = html.indexOf('<!--', from);
    const close = open < 0 ? -1 : html.indexOf('-->', open + 4);
    if (close < 0) return out + html.slice(from);
    out += html.slice(from, open);
    from = close + 3;
  }
}

// topLevel returns the direct children of the markup: { tag, attrs, outer, inner }.
function topLevel(html) {
  const out = [];
  const re = /<(\/?)([a-zA-Z][\w-]*)([^>]*)>/g;
  let depth = 0;
  let cur = null;
  let m;
  while ((m = re.exec(html))) {
    const closing = m[1] === '/';
    const tag = m[2].toLowerCase();
    const selfClose = /\/\s*$/.test(m[3]) || VOID.has(tag);
    if (!closing) {
      if (depth === 0) cur = { tag, attrs: parseAttrs(m[3].replace(/\/\s*$/, '')), start: m.index, innerStart: re.lastIndex };
      if (selfClose) { if (depth === 0) { cur.outer = html.slice(cur.start, re.lastIndex); cur.inner = ''; out.push(cur); cur = null; } } else depth++;
    } else {
      depth--;
      if (depth === 0 && cur) {
        cur.outer = html.slice(cur.start, re.lastIndex);
        cur.inner = html.slice(cur.innerStart, m.index);
        out.push(cur);
        cur = null;
      }
    }
  }
  return out;
}

function headerHtml() {
  const src = withoutHtmlComments(INDEX_HTML);
  const a = src.indexOf('<header class="header">');
  assert.ok(a >= 0, 'index.html has the shared header');
  return src.slice(a + '<header class="header">'.length, src.indexOf('</header>', a));
}

const HEADER = headerHtml();
const CHILDREN = topLevel(HEADER).map(c => Object.assign(c, {
  id: c.attrs.id || '',
  classes: (c.attrs.class || '').split(/\s+/).filter(Boolean),
  inline: c.attrs.style || '',
}));
const child = id => CHILDREN.find(c => c.id === id);
const hiddenInline = c => /display\s*:\s*none/i.test(c.inline);

// shownOnAutomata: the header children without an inline display:none, plus the
// ones navigate() switches on for the Automata view, minus any the stylesheet
// hides at env (the expand toggle is hidden on phones).
function shownOnAutomata(env) {
  const nav = APP_JS.slice(APP_JS.indexOf('function navigate('));
  return CHILDREN.filter(c => {
    if (env && /^none$/.test(computed(asEl(c), env).display || '')) return false;
    if (!hiddenInline(c)) return true;
    const at = nav.indexOf(`getElementById('${c.id}')`);
    if (at < 0 || at > 40000) return false;
    const win = nav.slice(at, at + 420);
    return /'autonomous'/.test(win) && /inline-flex/.test(win);
  });
}

function asEl(c) { return { id: c.id, classes: c.classes, tag: c.tag }; }

// ── (a) one non-wrapping line ──────────────────────────────────────────────

test('(a) the header is one flex row that never wraps, at phone width or wider', () => {
  const header = { id: '', classes: ['header'], tag: 'header' };
  [PHONE, WIDE].forEach(env => {
    const cs = computed(header, env);
    assert.equal(cs.display, 'flex', `display at ${env.width}px`);
    assert.equal(cs['flex-wrap'] || 'nowrap', 'nowrap', `flex-wrap at ${env.width}px`);
    assert.ok(px(cs.height) !== null || /var\(--header-h\)/.test(cs.height), 'one fixed height: the row has no room to grow a second line');
    assert.equal(cs['align-items'], 'center');
  });
  const wrapRules = RULES.filter(r => r.selectors.some(s => /(^|[\s>])\.header$/.test(s.trim()) || /(^|[\s>])header$/.test(s.trim())) && r.decls['flex-wrap'] && /wrap/.test(r.decls['flex-wrap'].value) && !/^nowrap$/.test(r.decls['flex-wrap'].value));
  assert.deepEqual(wrapRules.map(r => r.selectors.join(',')), [], 'no rule turns header wrapping on');
});

test('(a) every header control that holds text keeps it on one line', () => {
  ['serverPickerBtn', 'headerScopeBtn'].forEach(id => {
    const c = child(id);
    assert.equal(computed(asEl(c), PHONE)['white-space'], 'nowrap', id + ' does not wrap');
    assert.equal(computed(asEl(c), PHONE).flex, '0 0 auto', id + ' never grows or shrinks below its icon');
  });
  assert.equal(computed(asEl(child('headerTitle')), PHONE)['white-space'], 'nowrap', 'the title does not wrap');
});

// ── (b) the title ──────────────────────────────────────────────────────────

test('(b) the title takes the spare room, may shrink to zero and ends in an ellipsis', () => {
  const t = child('headerTitle');
  assert.ok(t, '#headerTitle is in the header');
  assert.ok(t.classes.includes('header-title'));
  [PHONE, WIDE].forEach(env => {
    const cs = computed(asEl(t), env);
    const grow = Number((cs.flex || '').trim().split(/\s+/)[0]);
    assert.ok(grow >= 1, `flex-grow >= 1 at ${env.width}px (flex: ${cs.flex})`);
    assert.equal(px(cs['min-width']), 0, `min-width: 0 at ${env.width}px, so it can shrink below its text`);
    assert.equal(cs.overflow, 'hidden', 'overflow hidden');
    assert.equal(cs['text-overflow'], 'ellipsis', 'text-overflow ellipsis');
    assert.equal(cs['white-space'], 'nowrap');
  });
  assert.ok(!/width\s*:/i.test(t.inline.replace(/min-width|max-width/gi, '')), 'no fixed width on the title');
});

test('(b) no other control grows: the title is the only flex item with flex-grow', () => {
  const growers = shownOnAutomata(PHONE).filter(c => {
    const g = Number((computed(asEl(c), PHONE).flex || '0').trim().split(/\s+/)[0]);
    return g >= 1;
  }).map(c => c.id);
  assert.deepEqual(growers, ['headerTitle']);
});

// ── (c) icon-only below the breakpoint, short name above ───────────────────

function phoneBreakpoint() {
  // the @media max-width rule that hides the server short name
  const rule = RULES.find(r => /max-width/.test(r.media) && r.selectors.includes('.header-server-name') && r.decls.display && r.decls.display.value === 'none');
  assert.ok(rule, 'a max-width media rule hides .header-server-name');
  return Number(/max-width\s*:\s*(\d+)px/.exec(rule.media)[1]);
}

test('(c) the phone breakpoint hides the server short name; above it the name shows, capped with an ellipsis', () => {
  const bp = phoneBreakpoint();
  assert.ok(bp >= 480 && bp <= 700, 'a phone breakpoint, got ' + bp);
  const name = { id: 'serverPickerName', classes: ['header-server-name'], tag: 'span' };
  assert.equal(computed(name, PHONE).display, 'none', 'icon-only at 320px');
  assert.equal(computed(name, { width: bp, height: 640, coarse: true }).display, 'none', 'still hidden at the breakpoint');
  const above = computed(name, { width: bp + 1, height: 900, coarse: false });
  assert.notEqual(above.display, 'none', 'the short name shows above the breakpoint');
  assert.equal(above['text-overflow'], 'ellipsis');
  assert.equal(above['white-space'], 'nowrap');
  assert.equal(above.overflow, 'hidden');
  assert.ok(/^\d+ch$/.test(above['max-width']), 'the name is capped (' + above['max-width'] + ')');
  assert.ok(parseInt(above['max-width'], 10) <= 16, 'the cap is short');
});

test('(c) the server button still carries icon, status dot and name in the markup, and the full name travels in aria-label and title', () => {
  const b = child('serverPickerBtn');
  assert.match(b.inner, /<svg[^>]*class="header-server-icon"[^>]*aria-hidden="true"/);
  assert.match(b.inner, /id="serverPickerDot"[^>]*aria-hidden="true"/);
  assert.match(b.inner, /<span id="serverPickerName" class="header-server-name">[^<]+<\/span>/);
  assert.ok(b.attrs['aria-label'] && b.attrs.title, 'label and title hold the name that the narrow button hides');
});

test('(c) the scope button is icon-only at every width: an aria-hidden icon and a badge with no visible text', () => {
  const b = child('headerScopeBtn');
  assert.match(b.inner, /<svg[^>]*class="header-scope-icon"[^>]*aria-hidden="true"/);
  const rest = b.inner.replace(/<svg[\s\S]*?<\/svg>/, '').replace(/<span id="headerScopeBadge"[^>]*><\/span>/, '');
  assert.match(rest, /^(?:\s|<[^>]*>)*$/, 'no label text inside the button');
  const hasLabelSpan = /class="[^"]*header-scope-(name|label|text)[^"]*"/.test(b.inner);
  assert.ok(!hasLabelSpan, 'no scope name span');
  ['phone', 'wide'].forEach(k => {
    const env = k === 'phone' ? PHONE : WIDE;
    const cs = computed(asEl(b), env);
    assert.equal(cs['white-space'], 'nowrap');
    assert.ok(px(cs['min-width']) >= 44, 'a 44px hit area at ' + env.width + 'px');
    assert.ok(px(cs['min-height']) >= 44);
  });
  // the badge is a dot, its text is visually hidden (never takes row width)
  const badgeText = computed({ id: '', classes: ['header-scope-badge-text'], tag: 'span' }, PHONE);
  assert.equal(badgeText.position, 'absolute');
  assert.equal(badgeText.width, '1px');
  const badge = computed({ id: 'headerScopeBadge', classes: ['header-scope-badge'], tag: 'span' }, PHONE);
  assert.equal(badge.position, 'absolute', 'the badge is out of flow');
});

// ── (d) the width budget ───────────────────────────────────────────────────

// fixedWidth is the width a control carries whatever the room: the larger of its
// declared width and min-width in px, plus its horizontal padding when the box
// is content-sized.
function fixedWidth(c, env) {
  const cs = computed(asEl(c), env);
  const inlineWidth = px((/(?:^|;)\s*width\s*:\s*([^;]+)/i.exec(c.inline) || [])[1]);
  const w = Math.max(px(cs.width) || 0, px(cs['min-width']) || 0, inlineWidth || 0);
  if (!w) return 0;
  const boxSizing = cs['box-sizing'] || 'border-box'; // the global reset
  return boxSizing === 'content-box' ? w + hPadding((cs.padding || '0')) : w;
}

test('(d) the header shows both icon buttons on the Automata view and hides the scope button elsewhere', () => {
  const ids = shownOnAutomata(PHONE).map(c => c.id);
  assert.ok(ids.includes('serverPickerBtn') && ids.includes('headerScopeBtn'), 'both buttons are visible together: ' + ids.join(','));
  assert.ok(hiddenInline(child('headerScopeBtn')), 'the scope button starts hidden and navigate() shows it');
  assert.match(APP_JS, /headerScopeBtn\.style\.display = view === 'autonomous' \? 'inline-flex' : 'none';/);
});

test('(d) no header control has a fixed width that can push another off a 320px screen', () => {
  const visible = shownOnAutomata(PHONE);
  const rows = visible.map(c => ({ id: c.id || c.tag, width: fixedWidth(c, PHONE) }));
  const sum = rows.reduce((n, r) => n + r.width, 0);
  const budget = SCREEN - TITLE_MIN;
  assert.ok(sum < budget, `fixed widths on the Automata view add up to ${sum}px; the budget is ${budget}px (${SCREEN} minus the ${TITLE_MIN}px title floor): ` + JSON.stringify(rows.filter(r => r.width)));
  // the controls the shared header budgets for: a single control stays near a
  // 44px hit target; nothing like a 120px fixed block
  rows.forEach(r => assert.ok(r.width <= 60, `${r.id} has a ${r.width}px fixed width`));
  // the two new icon buttons cost the same as each other and are not wider than a hit target plus padding
  const server = fixedWidth(child('serverPickerBtn'), PHONE);
  const scope = fixedWidth(child('headerScopeBtn'), PHONE);
  assert.ok(server >= 44 && server <= 56, 'server button ' + server);
  assert.equal(scope, server, 'the scope button costs what the server button costs');
});

test('(d) the title is not counted in the fixed widths and no inline style gives a visible control a pixel width', () => {
  assert.equal(fixedWidth(child('headerTitle'), PHONE), 0);
  shownOnAutomata(PHONE).forEach(c => {
    const m = /(?:^|;)\s*(?:min-)?width\s*:\s*(\d+)px/i.exec(c.inline);
    assert.equal(m, null, `${c.id || c.tag} has an inline pixel width`);
  });
});

test('(d) the extreme-zoom rules still drop the least essential controls so the row fits', () => {
  ['headerHelpLink', 'headerTagsBtn'].forEach(id => {
    const c = child(id);
    assert.equal(computed(asEl(c), { width: 200, height: 640, coarse: true }).display, 'none', id + ' is hidden at 200px');
    assert.notEqual(computed(asEl(c), PHONE).display, 'none', id + ' shows at 320px');
  });
  const expand = child('headerFullscreenBtn');
  assert.equal(computed(asEl(expand), PHONE).display, 'none', 'the expand toggle is hidden on phones, so it takes no row width');
  assert.notEqual(computed(asEl(expand), WIDE).display, 'none', 'and shows on a wide screen');
  const scope = child('headerScopeBtn');
  assert.notEqual(computed(asEl(scope), { width: 200, height: 640, coarse: true }).display, 'none', 'the scope button is never hidden by CSS: JS owns its visibility');
});

// ── (e) real buttons, labelled, once ───────────────────────────────────────

test('(e) both icon buttons are real <button type="button"> elements with an aria-label', () => {
  ['serverPickerBtn', 'headerScopeBtn'].forEach(id => {
    const b = child(id);
    assert.ok(b, id + ' is a direct child of the header');
    assert.equal(b.tag, 'button', id + ' is a <button>, not a div or span');
    assert.equal(b.attrs.type, 'button', id + ' has type="button"');
    assert.ok(typeof b.attrs['aria-label'] === 'string' && b.attrs['aria-label'].trim().length > 2, id + ' has an aria-label');
    assert.equal(b.attrs['aria-haspopup'], 'dialog');
    assert.equal(b.attrs['aria-expanded'], 'false');
    assert.ok(b.attrs.onclick, id + ' opens its modal on click');
    assert.ok(!/\btabindex="-1"/.test(b.outer), id + ' stays in the tab order');
  });
  assert.equal(child('serverPickerBtn').attrs.onclick, 'openServerPickerModal()');
  assert.equal(child('headerScopeBtn').attrs.onclick, 'openScopeModal()');
});

test('(e) the header contains the server button and the scope button exactly once each', () => {
  const src = withoutHtmlComments(INDEX_HTML);
  const count = (re, s) => (s.match(re) || []).length;
  ['serverPickerBtn', 'headerScopeBtn', 'headerScopeBadge', 'serverPickerDot', 'serverPickerName'].forEach(id => {
    assert.equal(count(new RegExp(`id="${id}"`, 'g'), src), 1, `${id} appears once in index.html`);
  });
  assert.equal(count(/id="serverPickerBtn"/g, HEADER), 1, 'the server button is inside the header');
  assert.equal(count(/id="headerScopeBtn"/g, HEADER), 1, 'the scope button is inside the header');
  assert.equal(count(/class="header-server-btn"/g, src), 1);
  assert.equal(count(/class="header-scope-btn"/g, src), 1);
  assert.equal(CHILDREN.filter(c => c.id === 'serverPickerBtn').length, 1);
  assert.equal(CHILDREN.filter(c => c.id === 'headerScopeBtn').length, 1);
  const at = id => CHILDREN.findIndex(c => c.id === id);
  assert.ok(at('headerTitle') < at('serverPickerBtn') && at('serverPickerBtn') < at('headerScopeBtn') && at('headerScopeBtn') < at('headerIdentityBtn'),
    'title, server, scope, identity in that order');
});

test('(e) the old server chip row and dropdown are gone: nothing else in the header is a server picker', () => {
  assert.ok(!/id="serverDropdown"/.test(HEADER), 'no second server control');
  assert.ok(!/server-picker-bar/.test(HEADER));
});

test('(e) the stylesheet parsed into the rules this file relies on', () => {
  // a guard against the parser silently reading nothing
  const has = sel => RULES.some(r => r.selectors.includes(sel));
  ['.header', '.header-title', '.header > .header-server-btn', '.header > .header-scope-btn', '.header-server-name', '.status-dot'].forEach(s => assert.ok(has(s), 'rule for ' + s));
  assert.ok(RULES.length > 500, 'many rules parsed: ' + RULES.length);
});
