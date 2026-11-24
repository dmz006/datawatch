// scripts/test-stories/pwa/raw-key-scan-lib.mjs — the pure half of the raw-key scan
// (TS-856). No browser and no playwright import, so a node:test file can load it.
//
// A raw locale key on screen (for example pause_badge_paused) means a string was
// drawn before the locale bundle was ready, or with no bundle entry. This module
// holds the in-page collector (a function source string the story evaluates in the
// page), the classifier that decides which collected strings are raw keys, and the
// allow-list rules.

// A locale key: lowercase words joined by underscores.
export const KEY_PATTERN = /^[a-z][a-z0-9]*(?:_[a-z0-9]+)+$/;
// Characters that separate words inside a longer string.
const TOKEN_SPLIT = /[\s,;:()[\]"'|/<>=]+/;

// COLLECT_SRC runs in the page and returns every string a person can see: each
// visible text node, plus the document title and the title, aria-label, placeholder,
// alt and button value attributes of visible elements. `where` names the nearest
// element (tag, id, first classes) and the view/tab it sits in.
export const COLLECT_SRC = `(() => {
  const out = [];
  const SKIP = new Set(['SCRIPT', 'STYLE', 'NOSCRIPT', 'TEMPLATE', 'HEAD', 'TITLE', 'META', 'LINK']);
  const shown = (el) => {
    if (!el || el.nodeType !== 1) return false;
    if (typeof el.checkVisibility === 'function') return el.checkVisibility({ checkOpacity: false, checkVisibilityCSS: true });
    return !!(el.offsetWidth || el.offsetHeight || el.getClientRects().length);
  };
  // An element (or an ancestor) tagged data-raw-key-ok="<reason>" holds text that is shown on
  // purpose and looks like keys, such as API names listed verbatim: it is not scanned.
  const okZone = (el) => !!(el.closest && el.closest('[data-raw-key-ok]'));
  const label = (el) => {
    if (!el || el.nodeType !== 1) return '';
    let s = el.tagName.toLowerCase();
    if (el.id) s += '#' + el.id;
    const cls = (typeof el.className === 'string' ? el.className : '').split(/\\s+/).filter(Boolean).slice(0, 2);
    if (cls.length) s += '.' + cls.join('.');
    return s;
  };
  const where = (el) => {
    const ctx = [];
    const v = document.querySelector('.nav-btn.active');
    if (v) ctx.push('view=' + (v.getAttribute('data-view') || ''));
    const tabs = [...document.querySelectorAll('#view .output-tab.active, #view .settings-tab-btn.active')]
      .map((b) => b.getAttribute('data-tab') || (b.textContent || '').trim().slice(0, 24)).filter(Boolean);
    if (tabs.length) ctx.push('tab=' + tabs.join('>'));
    return label(el) + (ctx.length ? ' [' + ctx.join(' ') + ']' : '');
  };
  if (document.title) out.push({ kind: 'document-title', text: document.title, where: 'document.title' });
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  const seen = new Map();
  for (let n = walker.nextNode(); n; n = walker.nextNode()) {
    const text = (n.nodeValue || '').replace(/\\s+/g, ' ').trim();
    if (!text) continue;
    const el = n.parentElement;
    if (!el || SKIP.has(el.tagName) || okZone(el)) continue;
    let ok = seen.get(el);
    if (ok === undefined) { ok = shown(el); seen.set(el, ok); }
    if (ok) out.push({ kind: 'text', text, where: where(el) });
  }
  const ATTRS = ['title', 'aria-label', 'placeholder', 'alt'];
  for (const el of document.body.querySelectorAll('*')) {
    if (SKIP.has(el.tagName) || okZone(el)) continue;
    let vis;
    for (const a of ATTRS) {
      const v = el.getAttribute(a);
      if (!v || !v.trim()) continue;
      if (vis === undefined) vis = shown(el);
      if (vis) out.push({ kind: a, text: v.replace(/\\s+/g, ' ').trim(), where: where(el) });
    }
    if (el.tagName === 'INPUT' && /^(button|submit|reset)$/.test(el.type) && el.value && shown(el)) {
      out.push({ kind: 'value', text: el.value.trim(), where: where(el) });
    }
  }
  return out;
})()`;

// classify decides whether one collected string is a raw locale key. It returns
// null (fine) or { reason, key }:
//   pattern  the whole string is lowercase words joined by underscores
//   bundle   the whole string is exactly a key of the English bundle
//   token    a word inside longer text is a key of the English bundle that has an underscore
// enKeys is a Set of the English bundle's keys.
export function classify(text, enKeys) {
  const s = String(text || '').trim();
  if (!s) return null;
  if (KEY_PATTERN.test(s)) return { reason: 'pattern', key: s };
  if (enKeys.has(s)) return { reason: 'bundle', key: s };
  for (const tok of s.split(TOKEN_SPLIT)) {
    if (tok.includes('_') && KEY_PATTERN.test(tok) && enKeys.has(tok)) return { reason: 'token', key: tok };
  }
  return null;
}

// allowed reports whether a finding is on the allow-list. An entry names the
// string by `text` (exact), `texts` (any of) or `prefix`, or by none of them (any
// string at that place), and may limit it to a place with `where`, a substring of
// the element description the collector gives. The reason is required so an entry
// cannot be added silently.
export function allowed(text, where, allowList) {
  const s = String(text || '').trim();
  const w = String(where || '');
  return (allowList || []).some((e) => {
    if (!e || !e.reason) return false;
    if (e.where !== undefined && !w.includes(e.where)) return false;
    if (e.text !== undefined) return e.text === s;
    if (Array.isArray(e.texts)) return e.texts.includes(s);
    if (e.prefix !== undefined) return s.startsWith(e.prefix);
    return e.where !== undefined; // a place-only entry allows every string there
  });
}

// findingsOf turns the strings collected at one checkpoint into findings: those
// that are raw keys and not allowed. `at` describes the checkpoint (phase, time,
// view, tab) and `ready` is whether the bundle had loaded when it was collected.
export function findingsOf(items, enKeys, { at, ready, allowList = [] }) {
  const out = [];
  const seen = new Set();
  for (const it of items || []) {
    const c = classify(it.text, enKeys);
    if (!c || allowed(it.text, it.where, allowList)) continue;
    const id = `${it.kind}\u0000${it.text}\u0000${it.where}`;
    if (seen.has(id)) continue;
    seen.add(id);
    out.push({ at, ready: !!ready, kind: it.kind, text: it.text, key: c.key, reason: c.reason, where: it.where });
  }
  return out;
}

// formatFindings renders the first `limit` findings, one per line, for the failure message.
export function formatFindings(findings, limit = 25) {
  const lines = findings.slice(0, limit).map((f, i) =>
    `${String(i + 1).padStart(2)}. [${f.at}${f.ready ? '' : ', bundle not ready'}] ${f.kind} ${JSON.stringify(f.text)} (${f.reason}) in ${f.where}`);
  if (findings.length > limit) lines.push(`... and ${findings.length - limit} more (see raw-key-findings.json)`);
  return lines.join('\n');
}
