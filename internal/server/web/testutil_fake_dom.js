'use strict';
// A tiny fake DOM for the web tests that need to see what really lands in the
// page: real innerHTML parsing into a tree, id lookup, attribute and class
// handling, focus tracking, document-level listeners and a small selector
// matcher. It covers only the markup app.js writes for the header button and
// the shared header modals -- double-quoted attributes, escaped values,
// nested div/span/button/a -- not arbitrary HTML.
//
//   const dom = installFakeDom(sandbox, { statics: ['serverPickerBtn', ...] });
//   dom.el('serverPickerBtn')            // a static element registered by id
//   dom.byId('serverPickerModal')        // an element created by app.js
//   dom.fire('keydown', { key: 'Tab' })  // dispatch to document listeners
//   dom.listenerCount('keydown')
//
// The base sandbox from testutil_browser_stub stays in place for every id the
// fake tree does not know: those still resolve to its generic stubs.

const VOID = new Set(['br', 'hr', 'img', 'input', 'path', 'circle', 'rect', 'line', 'polyline', 'polygon']);

function unescapeHtml(s) {
  return String(s).replace(/&quot;/g, '"').replace(/&#39;/g, "'").replace(/&#x27;/g, "'")
    .replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&');
}

function parseAttrs(src) {
  const attrs = {};
  const re = /([^\s=\/]+)(?:="([^"]*)")?/g;
  let m;
  while ((m = re.exec(src))) attrs[m[1]] = m[2] === undefined ? '' : unescapeHtml(m[2]);
  return attrs;
}

// Splits a selector list on top-level commas (not inside parentheses).
function splitList(sel) {
  const out = [];
  let depth = 0;
  let cur = '';
  for (const ch of sel) {
    if (ch === '(') depth++;
    if (ch === ')') depth--;
    if (ch === ',' && depth === 0) { out.push(cur.trim()); cur = ''; } else cur += ch;
  }
  if (cur.trim()) out.push(cur.trim());
  return out;
}

// One compound selector: optional tag, then .class / [attr] / [attr="v"] / :not(...) parts.
function matchCompound(el, sel) {
  let rest = sel.trim();
  const tag = rest.match(/^[a-zA-Z][a-zA-Z0-9-]*/);
  if (tag) {
    if (el.tagName !== tag[0].toUpperCase()) return false;
    rest = rest.slice(tag[0].length);
  }
  const attrRe = /^\[([^\]=]+)(?:="([^"]*)")?\]/;
  const notRe = /^:not\(((?:[^()]|\([^()]*\))*)\)/;
  while (rest) {
    let m = rest.match(/^\.([A-Za-z0-9_-]+)/);
    if (m) { if (!el.classList.contains(m[1])) return false; rest = rest.slice(m[0].length); continue; }
    m = rest.match(notRe);
    if (m) { if (matchCompound(el, m[1])) return false; rest = rest.slice(m[0].length); continue; }
    m = rest.match(attrRe);
    if (m) {
      const has = el.hasAttribute(m[1]);
      if (!has) return false;
      if (m[2] !== undefined && el.getAttribute(m[1]) !== m[2]) return false;
      rest = rest.slice(m[0].length);
      continue;
    }
    return false; // unsupported selector piece: never silently match
  }
  return true;
}

function matches(el, sel) { return splitList(sel).some(part => matchCompound(el, part)); }

function makeEl(tagName, doc) {
  const attrs = {};
  const el = {
    tagName: String(tagName).toUpperCase(),
    children: [],
    parentNode: null,
    className: '',
    style: {},
    dataset: {},
    removed: false,
    _text: '',
    _html: '',
    _listeners: {},
    classList: {
      add(c) { const s = new Set(el.className.split(/\s+/).filter(Boolean)); s.add(c); el.className = [...s].join(' '); },
      remove(c) { el.className = el.className.split(/\s+/).filter(x => x && x !== c).join(' '); },
      toggle(c, force) { const on = force === undefined ? !this.contains(c) : !!force; on ? this.add(c) : this.remove(c); },
      contains(c) { return el.className.split(/\s+/).includes(c); },
    },
    setAttribute(k, v) { attrs[k] = String(v); if (k === 'id') el.id = String(v); if (k === 'class') el.className = String(v); },
    getAttribute(k) {
      if (k === 'id') return el.id || (k in attrs ? attrs[k] : null);
      if (k === 'class') return el.className || null;
      return k in attrs ? attrs[k] : null;
    },
    hasAttribute(k) { return el.getAttribute(k) !== null; },
    removeAttribute(k) { delete attrs[k]; },
    addEventListener(t, fn) { (el._listeners[t] = el._listeners[t] || []).push(fn); },
    removeEventListener(t, fn) { el._listeners[t] = (el._listeners[t] || []).filter(f => f !== fn); },
    dispatch(t, ev) { (el._listeners[t] || []).slice().forEach(fn => fn(ev)); },
    focus() { doc.activeElement = el; },
    blur() { if (doc.activeElement === el) doc.activeElement = doc.body; },
    click() { el.dispatch('click', { target: el }); },
    remove() {
      el.removed = true;
      if (el.parentNode) el.parentNode.children = el.parentNode.children.filter(c => c !== el);
      el.parentNode = null;
      if (doc.activeElement && el.contains(doc.activeElement)) doc.activeElement = doc.body;
    },
    appendChild(c) { c.parentNode = el; c.removed = false; el.children.push(c); return c; },
    contains(n) { for (let x = n; x; x = x.parentNode) if (x === el) return true; return false; },
    querySelectorAll(sel) {
      const out = [];
      const walk = (n) => { n.children.forEach(c => { if (matches(c, sel)) out.push(c); walk(c); }); };
      walk(el);
      return out;
    },
    querySelector(sel) { return el.querySelectorAll(sel)[0] || null; },
    scrollIntoView() {},
    getBoundingClientRect() { return { width: 0, height: 0, top: 0, left: 0, right: 0, bottom: 0 }; },
  };
  Object.defineProperty(el, 'textContent', {
    get() { return el.children.length ? el.children.map(c => c.textContent).join('') + el._text : el._text; },
    set(v) { el._text = String(v); el.children = []; },
  });
  Object.defineProperty(el, 'innerHTML', {
    get() { return el._html; },
    set(v) {
      el._html = String(v);
      el._text = '';
      el.children = [];
      parseInto(el, el._html, doc);
    },
  });
  return el;
}

function parseInto(root, html, doc) {
  const stack = [root];
  const re = /<(\/?)([a-zA-Z][a-zA-Z0-9]*)((?:\s+[^>]*?)?)(\/?)>|([^<]+)/g;
  let m;
  while ((m = re.exec(html))) {
    const top = stack[stack.length - 1];
    if (m[5] !== undefined) { top._text += unescapeHtml(m[5]); continue; }
    const closing = m[1] === '/';
    const name = m[2].toLowerCase();
    if (closing) {
      for (let i = stack.length - 1; i > 0; i--) {
        if (stack[i].tagName === name.toUpperCase()) { stack.length = i; break; }
      }
      continue;
    }
    const child = makeEl(name, doc);
    const attrs = parseAttrs(m[3] || '');
    Object.keys(attrs).forEach(k => child.setAttribute(k, attrs[k]));
    child.parentNode = top;
    top.children.push(child);
    if (!(m[4] === '/' || VOID.has(name))) stack.push(child);
  }
}

function installFakeDom(sandbox, opts) {
  opts = opts || {};
  const baseDoc = sandbox.document;
  const doc = {
    activeElement: null,
    _listeners: {},
  };
  const body = makeEl('body', doc);
  doc.body = body;
  doc.activeElement = body;
  const statics = {};
  (opts.statics || []).forEach(id => { const e = makeEl('div', doc); e.setAttribute('id', id); statics[id] = e; });

  const findById = (id) => {
    const walk = (n) => {
      for (const c of n.children) {
        if (c.id === id) return c;
        const hit = walk(c);
        if (hit) return hit;
      }
      return null;
    };
    return walk(body);
  };

  baseDoc.body = body;
  Object.defineProperty(baseDoc, 'activeElement', { get() { return doc.activeElement; }, configurable: true });
  baseDoc.createElement = (tag) => makeEl(tag, doc);
  const realGet = baseDoc.getElementById;
  baseDoc.getElementById = (id) => findById(id) || statics[id] || realGet.call(baseDoc, id);
  baseDoc.addEventListener = (t, fn) => { (doc._listeners[t] = doc._listeners[t] || []).push(fn); };
  baseDoc.removeEventListener = (t, fn) => { doc._listeners[t] = (doc._listeners[t] || []).filter(f => f !== fn); };

  return {
    doc,
    body,
    el(id) { return statics[id]; },
    byId(id) { return findById(id) || statics[id] || null; },
    fire(type, ev) { (doc._listeners[type] || []).slice().forEach(fn => fn(ev)); },
    listenerCount(type) { return (doc._listeners[type] || []).length; },
    active() { return doc.activeElement; },
    makeEl: (tag) => makeEl(tag, doc),
  };
}

// pickerRows(sandbox) installs the fake DOM (once), opens the server picker
// modal through the real openServerPickerModal() and returns what it painted:
// { dom, list, rows, html, rowFor(name) }. rowFor matches each row's own
// data-server-name ('' = this server, 'all') -- never a selector built from a
// name.
function pickerRows(sandbox) {
  const dom = sandbox.__fakeDom || (sandbox.__fakeDom = installFakeDom(sandbox, {
    statics: ['serverPickerBtn', 'serverPickerDot', 'serverPickerName'],
  }));
  sandbox.__runInApp = sandbox.__runInApp || ((code) => require('vm').runInContext(code, sandbox));
  sandbox.__runInApp('openServerPickerModal()');
  const list = dom.byId('serverPickerModalList');
  const rows = list ? list.querySelectorAll('button') : [];
  return {
    dom,
    list,
    rows,
    html: list ? list.innerHTML : '',
    rowFor(name) { return rows.find(r => r.getAttribute('data-server-name') === name) || null; },
  };
}

module.exports = { installFakeDom, pickerRows, matches, parseInto, makeEl };
