// scripts/test-stories/pwa/mobile-fit-lib.mjs — browser-side mobile layout checks for datawatch PWA views.
//
// Takes a Playwright Page that is already showing the view under test and
// returns structured findings. This module does NOT import Playwright itself;
// the page object is passed in by the caller (see lib.mjs for the house style).
//
// Every finding is: { check, selector, detail, rect:{x,y,width,height}, text? }
// (Findings always carry a non-empty selector and detail; `text` is optional.)
//
// Checks and their exemptions:
//   page-scroll   The page overflows the viewport: documentElement or body
//                 scrollWidth > innerWidth + 1. Single finding with selector
//                 'html'; detail lists up to 5 widest offenders (elements
//                 whose right edge passes innerWidth + 1). When opts.scope is
//                 given, the check only fires if at least one in-scope element
//                 crosses the right viewport edge — document scrollWidth
//                 alone is not enough in that case.
//   edge-cross    An element extends past the viewport: rect.left < -1 or
//                 rect.right > innerWidth + 1. Exempt when the crossing is
//                 absorbed by an intentional horizontal scroll container
//                 (an ancestor with overflow-x auto|scroll whose own box fits
//                 inside the viewport) — but the container itself is still
//                 flagged if it crosses the viewport edge.
//   clipped-text  An element that owns a direct non-blank text node and clips
//                 it (scrollWidth > clientWidth + 1, or scrollHeight >
//                 clientHeight + 1 when overflow is hidden/clip or a line
//                 clamp is set). Accepted only when it has text-overflow:
//                 ellipsis AND a tooltip (non-empty title or aria-label on
//                 the element or an ancestor within 3 levels). Otherwise
//                 flagged, naming whichever of ellipsis/tooltip is missing.
//   tap-target    An interactive element (a[href], button, select, textarea,
//                 summary, visible-type inputs, [role=button], [onclick],
//                 non-negative [tabindex]) with width < tapMin or height <
//                 tapMin. Checkbox/radio inputs are measured as the larger of
//                 the input rect and their associated label rect. An inline
//                 link (display:inline) sitting in running text (parent has
//                 other non-blank text nodes) is exempt.
//   overlap       Siblings (children of one parent) that are both visible,
//                 neither positioned absolute/fixed/sticky, intersect by
//                 more than 4 px² AND more than 10% of the smaller rect.
//
// Only visible elements are examined: computed display not none, visibility
// not hidden, non-zero bounding rect, and no ancestor with display:none or
// aria-hidden="true".
//
// With opts.scope (a CSS selector list), edge-cross, clipped-text,
// tap-target and overlap only examine elements that match the scope or are
// inside a scope match. opts.ignore is a CSS selector list of elements to
// skip. opts.maxPerCheck caps findings per check (default 200).

export const CHECKS = ['page-scroll', 'edge-cross', 'clipped-text', 'tap-target', 'overlap'];
export const TAP_MIN = 40;

// Default per-check finding cap, applied when opts.maxPerCheck is not given.
const DEFAULT_MAX_PER_CHECK = 200;

// Detector body. Runs inside the page via page.evaluate, so it is fully
// self-contained (no closure/module references) and returns only
// JSON-serialisable data.
function runMobileFitChecks(opts) {
  const vw = window.innerWidth;
  const vh = window.innerHeight;
  const scopeSel = Array.isArray(opts.scope)
    ? opts.scope.filter((s) => typeof s === 'string' && s.trim())
    : [];
  const ignoreSel = Array.isArray(opts.ignore)
    ? opts.ignore.filter((s) => typeof s === 'string' && s.trim())
    : [];
  const tapMin = Number.isFinite(opts.tapMin) && opts.tapMin > 0 ? opts.tapMin : 40;
  const maxPerCheck = Number.isFinite(opts.maxPerCheck) && opts.maxPerCheck > 0
    ? Math.floor(opts.maxPerCheck)
    : 200;

  const counted = {};
  const findings = [];
  function add(check, selector, detail, rect, text) {
    selector = typeof selector === 'string' ? selector.trim() : '';
    detail = typeof detail === 'string' ? detail.trim() : '';
    if ((counted[check] || 0) >= maxPerCheck) return;
    if (!selector || !detail || !rect || typeof rect !== 'object') return;
    counted[check] = (counted[check] || 0) + 1;
    const out = { check, selector, detail, rect };
    if (text != null) out.text = text;
    findings.push(out);
  }

  const round = (v) => Math.round(v * 100) / 100;
  function rectOf(el) {
    const r = el.getBoundingClientRect();
    return { x: round(r.left), y: round(r.top), width: round(r.width), height: round(r.height) };
  }

  function matches(el, s) { try { return el.matches(s); } catch { return false; } }
  function selectorOk(s) { try { document.querySelectorAll(s); return true; } catch { return false; } }
  // Drop selectors the page cannot parse so they never abort the run.
  const validScope = scopeSel.filter(selectorOk);
  const validIgnore = ignoreSel.filter(selectorOk);
  const inScope = (el) =>
    validScope.length === 0 || validScope.some((s) => matches(el, s) || el.closest(s));
  const ignored = (el) => validIgnore.some((s) => matches(el, s));

  function cssEscape(name) {
    return (window.CSS && CSS.escape) ? CSS.escape(name) : String(name);
  }

  function isVisible(el) {
    if (!el || el.nodeType !== 1 || !el.isConnected) return false;
    const s = getComputedStyle(el);
    if (s.display === 'none' || s.visibility === 'hidden') return false;
    const r = el.getBoundingClientRect();
    if (!(r.width > 0) || !(r.height > 0)) return false;
    for (let a = el.parentElement; a; a = a.parentElement) {
      if (a.getAttribute && a.getAttribute('aria-hidden') === 'true') return false;
      const as = getComputedStyle(a);
      if (as.display === 'none' || as.visibility === 'hidden') return false;
    }
    return true;
  }

  // Stable selector: '#id' when the element has an id, otherwise tag plus up
  // to two class names (skip classes with a run of 4+ digits, which look
  // generated) with ':nth-child(N)' at each step, walking up to the nearest
  // ancestor with an id or to body, at most 6 levels.
  function tagStep(el) {
    const tag = el.tagName.toLowerCase();
    let classes = [];
    try { classes = Array.from(el.classList); } catch { classes = []; }
    classes = classes.filter((c) => !/\d{4}/.test(c)).slice(0, 2);
    const s = tag + (classes.length ? '.' + classes.map(cssEscape).join('.') : '');
    const parent = el.parentElement;
    const idx = parent ? Array.prototype.indexOf.call(parent.children, el) + 1 : 1;
    return s + ':nth-child(' + idx + ')';
  }
  function cssSelector(el) {
    if (el && el.id) return '#' + cssEscape(el.id);
    const parts = [];
    let node = el;
    while (node && node.nodeType === 1
      && node.tagName !== 'HTML' && node.tagName !== 'BODY'
      && parts.length < 6) {
      if (node.id) { parts.unshift('#' + cssEscape(node.id)); break; }
      parts.unshift(tagStep(node));
      node = node.parentElement;
    }
    return parts.join(' > ') || (el ? el.tagName.toLowerCase() : '');
  }

  const hasDirectText = (el) => {
    for (const n of el.childNodes) {
      if (n.nodeType === 3 && n.nodeValue && n.nodeValue.trim()) return true;
    }
    return false;
  };

  const textSnippet = (el, n) => {
    const t = (el.textContent || '').trim().slice(0, n);
    return t || null;
  };

  // Candidates: visible, non-ignored, in-scope elements of the document.
  const candidateEls = [];
  const all = document.body ? document.body.querySelectorAll('*') : document.querySelectorAll('*');
  for (const el of all) {
    if (!isVisible(el)) continue;
    if (ignored(el)) continue;
    if (!inScope(el)) continue;
    candidateEls.push(el);
  }
  const candidateSet = new Set(candidateEls);

  // --- a) page-scroll ------------------------------------------------------
  const docEl = document.documentElement;
  if (docEl) {
    const sw = Math.max(docEl.scrollWidth || 0, (document.body && document.body.scrollWidth) || 0);
    // scrollWidth grows past the viewport even when the overflow is clipped
    // (e.g. body { overflow-x: hidden }), in which case the page does NOT
    // scroll horizontally — that is the edge-cross situation instead. The
    // user agent propagates body's overflow to the viewport when the html
    // element keeps its default, so the document only scrolls horizontally
    // when neither one suppresses horizontal overflow.
    const htmlOX = getComputedStyle(docEl).overflowX;
    const bodyOX = document.body ? getComputedStyle(document.body).overflowX : 'visible';
    const suppressed = (htmlOX === 'hidden' || htmlOX === 'clip') ||
      (htmlOX === 'visible' && (bodyOX === 'hidden' || bodyOX === 'clip'));
    let show = sw > vw + 1 && !suppressed;
    if (validScope.length) {
      // Scoped: document scrollWidth alone is not enough — some element
      // inside the scope must cross the right viewport edge.
      show = candidateEls.some((el) => el.getBoundingClientRect().right > vw + 1);
    }
    if (show) {
      const offenders = candidateEls
        .map((el) => ({ el, r: el.getBoundingClientRect() }))
        .filter((c) => c.r.right > vw + 1)
        .sort((a, b) => b.r.width - a.r.width)
        .slice(0, 5)
        .map((c) => cssSelector(c.el) + ' (' + Math.round(c.r.width) + 'px wide)');
      let detail = 'scrollWidth ' + sw + ' > viewport ' + vw;
      if (validScope.length) detail += ' (element inside scope crosses right edge)';
      if (offenders.length) detail += '; widest: ' + offenders.join(', ');
      add('page-scroll', 'html', detail, { x: 0, y: 0, width: Math.round(vw), height: Math.round(vh) });
    }
  }

  // --- b) edge-cross -------------------------------------------------------
  function fitsViewport(r) { return r.left >= -1 && r.right <= vw + 1; }
  function absorbedByScrollContainer(el, r) {
    for (let a = el.parentElement; a; a = a.parentElement) {
      const s = getComputedStyle(a);
      if (s.overflowX === 'auto' || s.overflowX === 'scroll') {
        const ar = a.getBoundingClientRect();
        if (!fitsViewport(ar)) return false; // nearest scroll ancestor crosses the viewport itself
        return r.right > vw + 1
          ? r.right > ar.right - 1   // overflow absorbed by the container's scroll
          : r.left < ar.left + 1;
      }
    }
    return false;
  }
  for (const el of candidateEls) {
    const r = el.getBoundingClientRect();
    const left = r.left < -1;
    const right = r.right > vw + 1;
    if (!left && !right) continue;
    if (absorbedByScrollContainer(el, r)) continue;
    const bits = [];
    if (left) bits.push('left ' + Math.round(r.left) + ' < 0');
    if (right) bits.push('right ' + Math.round(r.right) + ' > viewport ' + vw);
    add('edge-cross', cssSelector(el), 'element crosses viewport edge: ' + bits.join('; '), rectOf(el));
  }

  // --- c) clipped-text -----------------------------------------------------
  function hasTooltipWithin(el, depth) {
    let node = el;
    for (let d = 0; node && d <= depth; node = node.parentElement, d += 1) {
      const t = node.getAttribute ? node.getAttribute('title') : null;
      const a = node.getAttribute ? node.getAttribute('aria-label') : null;
      if ((t != null && t.trim()) || (a != null && a.trim())) return true;
    }
    return false;
  }
  for (const el of candidateEls) {
    if (!hasDirectText(el)) continue;
    const s = getComputedStyle(el);
    const clamp = s.webkitLineClamp && s.webkitLineClamp !== 'none' && s.webkitLineClamp !== 'normal';
    const hiddenish = s.overflow === 'hidden' || s.overflow === 'clip' ||
                      s.overflowX === 'hidden' || s.overflowX === 'clip';
    let clip = null;
    if (el.scrollWidth > el.clientWidth + 1) {
      clip = 'scrollWidth ' + el.scrollWidth + ' > clientWidth ' + el.clientWidth;
    } else if ((hiddenish || clamp) && el.scrollHeight > el.clientHeight + 1) {
      clip = 'scrollHeight ' + el.scrollHeight + ' > clientHeight ' + el.clientHeight;
    }
    if (!clip) continue;
    const ellipsis = s.textOverflow === 'ellipsis';
    const tooltip = hasTooltipWithin(el, 3);
    if (ellipsis && tooltip) continue;
    const missing = [];
    if (!ellipsis) missing.push('text-overflow: ellipsis');
    if (!tooltip) missing.push('tooltip (title or aria-label)');
    add('clipped-text', cssSelector(el),
        clip + ' — missing ' + missing.join(' + '), rectOf(el), textSnippet(el, 80));
  }

  // --- d) tap-target -------------------------------------------------------
  const targets = new Set();
  const addTarget = (el) => { if (el && el.nodeType === 1) targets.add(el); };
  for (const el of document.querySelectorAll('a[href]')) addTarget(el);
  for (const el of document.querySelectorAll('button')) addTarget(el);
  for (const el of document.querySelectorAll('select')) addTarget(el);
  for (const el of document.querySelectorAll('textarea')) addTarget(el);
  for (const el of document.querySelectorAll('summary')) addTarget(el);
  for (const el of document.querySelectorAll('input')) {
    if ((el.getAttribute('type') || 'text').toLowerCase() !== 'hidden') addTarget(el);
  }
  for (const el of document.querySelectorAll('[role="button"]')) addTarget(el);
  for (const el of document.querySelectorAll('[onclick]')) addTarget(el);
  for (const el of document.querySelectorAll('[tabindex]')) {
    if (el.getAttribute('tabindex') !== '-1') addTarget(el);
  }
  for (const el of targets) {
    if (!isVisible(el)) continue;
    if (ignored(el)) continue;
    if (!inScope(el)) continue;
    let r = el.getBoundingClientRect();
    if (el.tagName === 'INPUT') {
      const type = (el.getAttribute('type') || 'text').toLowerCase();
      if (type === 'checkbox' || type === 'radio') {
        let label = null;
        const labels = el.labels;
        if (labels) for (const lb of labels) { if (isVisible(lb)) { label = lb; break; } }
        if (!label && el.id) {
          const l = document.querySelector('label[for="' + cssEscape(el.id) + '"]');
          if (l) label = l;
        }
        if (label) {
          const lr = label.getBoundingClientRect();
          if (lr.width > 0 || lr.height > 0) {
            const l2 = Math.min(r.left, lr.left), t2 = Math.min(r.top, lr.top);
            const r2 = Math.max(r.right, lr.right), b2 = Math.max(r.bottom, lr.bottom);
            r = { left: l2, top: t2, right: r2, bottom: b2, width: r2 - l2, height: b2 - t2 };
          }
        }
      }
    }
    if (r.width >= tapMin && r.height >= tapMin) continue;
    if (el.tagName === 'A' && getComputedStyle(el).display === 'inline' && el.parentElement) {
      // Inline link in running text: parent has other non-blank text nodes.
      let inRunningText = false;
      for (const n of el.parentElement.childNodes) {
        if (n.nodeType === 3 && n.nodeValue && n.nodeValue.trim()) { inRunningText = true; break; }
      }
      if (inRunningText) continue;
    }
    const bits = [];
    if (r.width < tapMin) bits.push('width ' + Math.round(r.width) + ' < ' + tapMin);
    if (r.height < tapMin) bits.push('height ' + Math.round(r.height) + ' < ' + tapMin);
    add('tap-target', cssSelector(el),
        'tap target ' + Math.round(r.width) + 'x' + Math.round(r.height) +
        ' below minimum ' + tapMin + 'px (' + bits.join(', ') + ')',
        rectOf(el), textSnippet(el, 60));
  }

  // --- e) overlap ----------------------------------------------------------
  const positioned = new Set(['absolute', 'fixed', 'sticky']);
  const parents = new Set();
  for (const el of candidateEls) {
    if (el.parentElement) parents.add(el.parentElement);
  }
  for (const parent of parents) {
    const kids = Array.from(parent.children).filter((el) => candidateSet.has(el));
    for (let i = 0; i < kids.length; i++) {
      for (let j = i + 1; j < kids.length; j++) {
        const a = kids[i], b = kids[j];
        const sa = getComputedStyle(a), sb = getComputedStyle(b);
        if (positioned.has(sa.position) || positioned.has(sb.position)) continue;
        const ra = a.getBoundingClientRect(), rb = b.getBoundingClientRect();
        const ix = Math.min(ra.right, rb.right) - Math.max(ra.left, rb.left);
        const iy = Math.min(ra.bottom, rb.bottom) - Math.max(ra.top, rb.top);
        if (ix <= 0 || iy <= 0) continue;
        const area = ix * iy;
        const smaller = Math.min(ra.width * ra.height, rb.width * rb.height);
        if (area <= 4 || area <= 0.1 * smaller) continue;
        add('overlap', cssSelector(b),
            'sibling overlap with ' + cssSelector(a) + ': ' + Math.round(area) + 'px² shared (' +
            Math.round(100 * area / smaller) + '% of smaller rect)',
            rectOf(b));
      }
    }
  }

  return findings;
}

// --- host-side API ----------------------------------------------------------

function toSelectorList(v) {
  if (v == null) return [];
  const arr = Array.isArray(v) ? v : [v];
  return arr.filter((s) => typeof s === 'string' && s.trim() !== '');
}

// Run every layout check on the page and return the findings array.
// opts: { scope?, ignore?, tapMin = TAP_MIN, maxPerCheck = 200 }
// scope/ignore are a single CSS selector or a list of them.
export async function collectFindings(page, opts = {}) {
  const config = {
    scope: toSelectorList(opts.scope),
    ignore: toSelectorList(opts.ignore),
    tapMin: (Number.isFinite(opts.tapMin) && opts.tapMin > 0) ? opts.tapMin : TAP_MIN,
    maxPerCheck: (Number.isFinite(opts.maxPerCheck) && opts.maxPerCheck > 0)
      ? Math.floor(opts.maxPerCheck) : DEFAULT_MAX_PER_CHECK,
  };
  let raw;
  try {
    raw = await page.evaluate(runMobileFitChecks, config);
  } catch (err) {
    throw new Error('collectFindings: page.evaluate failed: ' + (err && err.message));
  }
  const out = [];
  for (const f of Array.isArray(raw) ? raw : []) {
    if (!f || typeof f !== 'object') continue;
    const check = CHECKS.includes(f.check) ? f.check : null;
    const selector = typeof f.selector === 'string' && f.selector.trim() ? f.selector.trim() : null;
    const detail = typeof f.detail === 'string' && f.detail.trim() ? f.detail.trim() : null;
    if (!check || !selector || !detail) continue;
    const r = f.rect;
    const rect = (r && [r.x, r.y, r.width, r.height].every((v) => Number.isFinite(v)))
      ? { x: r.x, y: r.y, width: r.width, height: r.height }
      : { x: 0, y: 0, width: 0, height: 0 };
    const item = { check, selector, detail, rect };
    if (typeof f.text === 'string' && f.text) item.text = f.text;
    out.push(item);
  }
  return out;
}

// Report the page's rendering viewport in CSS pixels.
export async function describeViewport(page) {
  return page.evaluate(() => ({
    width: window.innerWidth,
    height: window.innerHeight,
    dpr: window.devicePixelRatio,
  }));
}
