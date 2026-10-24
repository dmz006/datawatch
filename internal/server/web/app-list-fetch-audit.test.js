// List-fetch audit for app.js: no list endpoint is requested in its full form
// on a tab open, a reconnect or a timer.
//
//   Part 1 (static)  every request URL literal for a list endpoint carries
//                    view=summary, a limit, or an equivalent narrowing marker,
//                    or sits in the explicit allowlist below with the reason
//                    recorded in docs/plans/web-list-fetch-audit.md. The
//                    string full=1 appears in no request at all.
//   Part 2 (runtime) app.js runs in the vm sandbox with a recording fetch and a
//                    fake clock; the Automata tab load, the Sessions tab load,
//                    the reconnect refresh (local and named server) and the
//                    periodic refresh timers never request a list in full form.
//   Part 3 (bytes)   a stub server shaped like the heavy seeded data set
//                    (internal/server/perf_seed_test.go: thousands of sessions,
//                    hundreds of Automata, a few of them 7-13 MB) answers the
//                    first load of each tab; bytes requested and returned stay
//                    under a constant budget, so a regression to the full form
//                    fails here.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const AUDIT_NOTE = path.join(__dirname, '..', '..', '..', 'docs', 'plans', 'web-list-fetch-audit.md');
const EN = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales/en.json'), 'utf8'));

// Same figure as perfBudgetBytes in internal/server/perf_budget_test.go and
// scripts/perf-budget.sh: a first-page list body stays under 256 KB.
const LIST_PAGE_BUDGET = 256 * 1024;
// Everything one tab's first load requests and receives, all calls summed.
const TAB_FIRST_LOAD_BUDGET = 256 * 1024;

// ── Endpoint table ───────────────────────────────────────────────────────────
// markers: query fragments that make a request of this endpoint the small form
// (a summary view, a limit, a server-side narrowing filter). bareIsSummary: the
// server answers the bare path with the summary form already. An endpoint with
// no markers has no small form on the server, so every use is allowlisted.
const ENDPOINTS = [
  { path: '/api/sessions', markers: ['view=summary', 'limit=', 'states='], bareIsSummary: false },
  { path: '/api/autonomous/prds', markers: ['view=summary', 'limit='], bareIsSummary: true },
  { path: '/api/alerts', markers: ['limit=', 'unread=1'], bareIsSummary: false },
  { path: '/api/evals/runs', markers: ['limit='], bareIsSummary: false },
  { path: '/api/council/runs', markers: ['limit='], bareIsSummary: false },
  { path: '/api/docs/search', markers: ['limit='], bareIsSummary: false },
  { path: '/api/memory/list', markers: ['n='], bareIsSummary: false },
  { path: '/api/sessions/aggregated', markers: [], aggregated: true },
  { path: '/api/alerts/aggregated', markers: [], aggregated: true },
  { path: '/api/autonomous/prds/aggregated', markers: [], aggregated: true },
  { path: '/api/cost/aggregated', markers: [], aggregated: true },
  { path: '/api/automations', markers: [] },
  { path: '/api/schedules', markers: [] },
  { path: '/api/observer/peers', markers: [] },
  { path: '/api/observer/envelopes', markers: [] },
  { path: '/api/observer/envelopes/all-peers', markers: [] },
  { path: '/api/compute/nodes', markers: [] },
  { path: '/api/plugins', markers: [] },
  { path: '/api/backends', markers: [] },
  { path: '/api/llms', markers: [] },
  { path: '/api/profiles', markers: [] },
  { path: '/api/profiles/projects', markers: [] },
  { path: '/api/profiles/clusters', markers: [] },
  { path: '/api/commands', markers: [] },
  { path: '/api/filters', markers: [] },
  { path: '/api/exit-hooks', markers: [] },
  { path: '/api/servers', markers: [] },
  { path: '/api/servers/health', markers: [] },
  { path: '/api/federation/peers', markers: [] },
  { path: '/api/federation/groups/builtins', markers: [] },
  { path: '/api/federation/meta-peers', markers: [] },
  { path: '/api/federation/fingerprint-matches', markers: [] },
  { path: '/api/skills', markers: [] },
  { path: '/api/skills/registries', markers: [] },
  { path: '/api/routing-rules', markers: [] },
  { path: '/api/orchestrator/graphs', markers: [] },
  { path: '/api/llm-roles', markers: [] },
  { path: '/api/templates', markers: [] },
  { path: '/api/pipelines', markers: [] },
  { path: '/api/algorithm', markers: [] },
  { path: '/api/cost/rates', markers: [] },
  { path: '/api/secrets', markers: [] },
  { path: '/api/docs/trust', markers: [] },
  { path: '/api/docs/trust/pending', markers: [] },
  { path: '/api/queue', markers: [] },
  { path: '/api/projects', markers: [] },
  { path: '/api/mcp/resources', markers: [] },
  { path: '/api/mcp/templates', markers: [] },
  { path: '/api/mcp/prompts', markers: [] },
  { path: '/api/ollama/models', markers: [] },
  { path: '/api/openwebui/models', markers: [] },
  { path: '/api/sessions/finished-oneshots', markers: [] },
  { path: '/api/files', markers: [] },
  { path: '/api/files/meta', markers: [] },
  { path: '/api/tooling/status', markers: [] },
  { path: '/api/memory/discussion', markers: [] },
  { path: '/api/memory/scopes/inventory', markers: [] },
  { path: '/api/memory/stats', markers: [] },
  { path: '/api/council/personas', markers: [] },
];
const ENDPOINT_BY_PATH = new Map(ENDPOINTS.map(e => [e.path, e]));

// ── Allowlist ────────────────────────────────────────────────────────────────
// A request literal that lacks a small-form marker passes only with an entry
// here. endpoint: the path. fn: the enclosing top-level function (omitted when
// the entry covers every call site of the endpoint). trigger and reason are the
// audit note's disposition for the call site.
const NO_SERVER_FORM = 'Left alone: the server has no summary, limit or conditional form for it.';
const BOUNDED_BY_CONFIG = 'Left alone: operator-curated and bounded by configuration (peers, nodes, plugins, profiles), not by history; the server has no summary, limit or conditional form.';
const FAN_OUT = 'Left alone: federation fan-out endpoint; it has no summary, limit or conditional form on the server, so paging it would need a server change.';
const ETAG_NO_CACHE = 'Left alone: no limit or summary form exists on the server, but the reply carries ETag with Cache-Control: no-cache, so a repeat request is already a conditional request that answers 304 when nothing changed.';

const ALLOWLIST = [
  { endpoint: '/api/sessions/aggregated', fn: '_loadAllServersSessions', trigger: 'tab open (All-servers view), reconnect', reason: FAN_OUT },
  { endpoint: '/api/alerts/aggregated', fn: '_alertsFetch', trigger: 'tab open, 30 s timer (All-servers branch)', reason: FAN_OUT },
  { endpoint: '/api/autonomous/prds/aggregated', fn: '_dashFetchPRDs', trigger: 'dashboard open, timer (All-servers branch)', reason: 'Left alone: no paged form on the aggregated route.' },
  { endpoint: '/api/autonomous/prds/aggregated', fn: '_automataFetchAll', trigger: 'tab open, 30 s timer (All-servers branch)', reason: 'Left alone: no paged form on the aggregated route.' },
  { endpoint: '/api/cost/aggregated', fn: '_dashFetchCost', trigger: 'dashboard timer', reason: 'Left alone: a single aggregate object, not a growing list.' },
  { endpoint: '/api/automations', trigger: 'tab open, 30 s timer; dialogs and forms are user action only', reason: ETAG_NO_CACHE },
  { endpoint: '/api/alerts', fn: 'openAlertDeepLink', trigger: 'user action only', reason: 'Left alone: a deep link has to find one alert by id wherever it sits in history, so it needs the unwindowed list; it runs once per link click, never on a timer.' },
  { endpoint: '/api/schedules', trigger: 'tab open, session open, Settings tab open', reason: 'Left alone: the server filters only by state and session_id; ' + BOUNDED_BY_CONFIG },
  { endpoint: '/api/observer/peers', trigger: '30 s timer, tab open', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/observer/envelopes', trigger: 'tab open, stats refresh', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/observer/envelopes/all-peers', trigger: 'tab open', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/compute/nodes', trigger: 'dashboard timer (about every 60 s), tab open', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/plugins', trigger: 'tab open', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/backends', trigger: 'tab open', reason: 'Left alone: a small fixed set, cached in _backendsCache where it is read repeatedly.' },
  { endpoint: '/api/llms', trigger: 'tab open or user action', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/profiles', trigger: 'tab open or user action', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/profiles/projects', trigger: 'tab open or user action', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/profiles/clusters', trigger: 'tab open or user action', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/commands', trigger: 'tab open or user action', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/filters', trigger: 'Settings tab open', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/exit-hooks', trigger: 'Settings tab open', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/servers', trigger: 'tab open or user action', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/servers/health', trigger: 'tab open or user action', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/federation/peers', trigger: 'tab open or user action', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/federation/groups/builtins', trigger: 'tab open or user action', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/federation/meta-peers', trigger: 'tab open or user action', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/federation/fingerprint-matches', trigger: 'tab open or user action', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/skills', trigger: 'tab open or user action', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/skills/registries', trigger: 'tab open or user action', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/routing-rules', trigger: 'Settings tab open', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/orchestrator/graphs', trigger: 'Settings tab open', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/llm-roles', trigger: 'Settings tab open', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/templates', trigger: 'Settings tab open', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/pipelines', trigger: 'Settings tab open', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/algorithm', trigger: 'Settings tab open', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/cost/rates', trigger: 'Settings tab open', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/secrets', trigger: 'Settings tab open', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/docs/trust', trigger: 'Settings tab open', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/docs/trust/pending', trigger: 'Settings tab open', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/queue', trigger: 'Settings tab open', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/projects', trigger: 'user action only', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/mcp/resources', trigger: 'user action only', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/mcp/templates', trigger: 'user action only', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/mcp/prompts', trigger: 'user action only', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/ollama/models', trigger: 'user action only', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/openwebui/models', trigger: 'user action only', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/sessions/finished-oneshots', trigger: 'user action only', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/files', trigger: 'user action only', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/files/meta', trigger: 'Settings tab open', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/tooling/status', trigger: 'Settings tab open', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/memory/discussion', trigger: 'Settings tab open', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/memory/scopes/inventory', trigger: 'user action only', reason: BOUNDED_BY_CONFIG },
  { endpoint: '/api/memory/stats', trigger: 'Settings tab open', reason: 'Left alone: a single stats object, not a growing list.' },
  { endpoint: '/api/council/personas', trigger: 'tab open or user action', reason: 'Left alone: a small fixed set.' },
];

// ── Source scanner ───────────────────────────────────────────────────────────
// A small JavaScript tokenizer: it finds every string and template literal
// (with the text between template expressions kept apart), blanks comments, and
// masks literal text so call structure can be read from the remaining code.

const REGEX_PRECEDERS = new Set(['(', ',', '=', ':', '[', '!', '&', '|', '?', '{', '}', ';', '+', '-', '*', '%', '<', '>', '~', '^']);
const REGEX_KEYWORDS = new Set(['return', 'typeof', 'case', 'in', 'of', 'delete', 'void', 'throw', 'new', 'else', 'do']);

function tokenize(src) {
  const literals = []; // { start, end, segments: [text between expressions] , parts: [[from,to]] }
  const blank = []; // [from, to) comment ranges
  const n = src.length;
  let prev = ''; // last significant token: a punctuation char, a keyword, or 'v' for a value

  function readRegex(i) {
    let j = i + 1;
    let inClass = false;
    while (j < n) {
      const c = src[j];
      if (c === '\\') { j += 2; continue; }
      if (c === '\n') return i + 1; // not a regex after all
      if (c === '[') inClass = true;
      else if (c === ']') inClass = false;
      else if (c === '/' && !inClass) { j++; break; }
      j++;
    }
    while (j < n && /[a-z]/i.test(src[j])) j++;
    return j;
  }

  // scan code from i; when untilBrace, stop after the brace that closes the
  // template expression this scan started inside.
  function scan(i, untilBrace) {
    let depth = 0;
    while (i < n) {
      const c = src[i];
      if (c === '\n' || c === ' ' || c === '\t' || c === '\r') { i++; continue; }
      if (c === '/' && src[i + 1] === '/') {
        let j = src.indexOf('\n', i); if (j < 0) j = n;
        blank.push([i, j]); i = j; continue;
      }
      if (c === '/' && src[i + 1] === '*') {
        let j = src.indexOf('*/', i + 2); j = j < 0 ? n : j + 2;
        blank.push([i, j]); i = j; continue;
      }
      if (c === '\'' || c === '"') {
        let j = i + 1;
        while (j < n && src[j] !== c && src[j] !== '\n') { if (src[j] === '\\') j++; j++; }
        literals.push({ start: i, end: j + 1, segments: [src.slice(i + 1, j)], parts: [[i + 1, j]] });
        i = j + 1; prev = 'v'; continue;
      }
      if (c === '`') {
        const lit = { start: i, end: 0, segments: [], parts: [] };
        let j = i + 1;
        let segStart = j;
        while (j < n && src[j] !== '`') {
          if (src[j] === '\\') { j += 2; continue; }
          if (src[j] === '$' && src[j + 1] === '{') {
            lit.segments.push(src.slice(segStart, j)); lit.parts.push([segStart, j]);
            j = scan(j + 2, true);
            segStart = j;
            continue;
          }
          j++;
        }
        lit.segments.push(src.slice(segStart, j)); lit.parts.push([segStart, j]);
        lit.end = j + 1;
        literals.push(lit);
        i = j + 1; prev = 'v'; continue;
      }
      if (c === '/') {
        const regexOk = prev === '' || REGEX_PRECEDERS.has(prev) || REGEX_KEYWORDS.has(prev);
        if (regexOk) { i = readRegex(i); prev = 'v'; continue; }
        prev = '/'; i++; continue;
      }
      if (/[A-Za-z_$]/.test(c)) {
        let j = i + 1;
        while (j < n && /[\w$]/.test(src[j])) j++;
        const word = src.slice(i, j);
        prev = REGEX_KEYWORDS.has(word) ? word : 'v';
        i = j; continue;
      }
      if (/[0-9]/.test(c)) {
        let j = i + 1;
        while (j < n && /[\w.]/.test(src[j])) j++;
        prev = 'v'; i = j; continue;
      }
      if (c === '{') depth++;
      if (c === '}') {
        if (untilBrace && depth === 0) return i + 1;
        depth--;
      }
      prev = (c === ')' || c === ']' || c === '}') ? 'v' : c;
      i++;
    }
    return i;
  }
  scan(0, false);
  return { literals, blank };
}

function lineOf(lineStarts, pos) {
  let lo = 0, hi = lineStarts.length - 1;
  while (lo < hi) { const mid = (lo + hi + 1) >> 1; if (lineStarts[mid] <= pos) lo = mid; else hi = mid - 1; }
  return lo + 1;
}

// matchClose returns the index of the paren closing the one opened at open.
function matchClose(masked, open) {
  let depth = 0;
  for (let i = open; i < masked.length; i++) {
    const c = masked[i];
    if (c === '(') depth++;
    else if (c === ')') { depth--; if (depth === 0) return i; }
  }
  return masked.length - 1;
}

// scanSource returns the list-endpoint request literals of src, and every
// literal segment that asks for a full form.
function scanSource(src) {
  const { literals, blank } = tokenize(src);
  const chars = src.split('');
  for (const [a, b] of blank) for (let i = a; i < b; i++) if (chars[i] !== '\n') chars[i] = ' ';
  const code = chars.join(''); // comments blanked, literals intact
  const maskedChars = chars.slice();
  for (const lit of literals) for (const [a, b] of lit.parts) for (let i = a; i < b; i++) if (maskedChars[i] !== '\n') maskedChars[i] = 'x';
  const masked = maskedChars.join('');

  const lineStarts = [0];
  for (let i = 0; i < src.length; i++) if (src[i] === '\n') lineStarts.push(i + 1);
  const lines = code.split('\n');
  // Top-level declarations (column 0) delimit functions.
  const declRe = /^(?:async\s+)?function\s*\*?\s*([\w$]+)|^window\.([\w$]+)\s*=|^(?:const|let|var)\s+([\w$]+)\s*=|^document\.addEventListener\('([\w-]+)'/;
  const decls = [];
  lines.forEach((ln, idx) => {
    const m = declRe.exec(ln);
    if (m) decls.push({ line: idx + 1, name: m[1] || m[2] || m[3] || ('on:' + m[4]) });
  });
  function enclosing(line) {
    let lo = 0, hi = decls.length - 1, best = -1;
    while (lo <= hi) { const mid = (lo + hi) >> 1; if (decls[mid].line <= line) { best = mid; lo = mid + 1; } else hi = mid - 1; }
    if (best < 0) return { name: '(top level)', text: '' };
    const from = decls[best].line;
    const to = best + 1 < decls.length ? decls[best + 1].line - 1 : lines.length;
    return { name: decls[best].name, text: lines.slice(from - 1, to).join('\n') };
  }

  const units = [];
  const fulls = [];
  for (const lit of literals) {
    const line = lineOf(lineStarts, lit.start);
    lit.segments.forEach((seg, idx) => {
      if (/(^|[?&])full=(1|true)\b/.test(seg)) fulls.push({ line, text: seg });
      const m = /^(\/api\/[A-Za-z0-9_\/-]+?)(\?|$)/.exec(seg);
      if (!m) return;
      const ep = ENDPOINT_BY_PATH.get(m[1]);
      if (!ep) return;
      // A template segment after an expression that continues the path (for
      // example `${id}/status`) is not this endpoint, but one starting with
      // "/api/" is a fresh path.
      const query = m[2] === '?' ? seg.slice(m[0].length) : null;
      // Enclosing call and whether it writes.
      let depth = 0, open = -1;
      for (let i = lit.start - 1; i >= 0; i--) {
        const c = masked[i];
        if (c === ';') break;
        if (c === ')') depth++;
        else if (c === '(') { if (depth === 0) { open = i; break; } depth--; }
      }
      let callee = '', args = '';
      if (open >= 0) {
        const mm = /([\w$.]+)\s*$/.exec(masked.slice(Math.max(0, open - 60), open));
        callee = mm ? mm[1] : '';
        args = src.slice(open, matchClose(masked, open) + 1);
      }
      const writes = /method\s*:\s*['"`](POST|PUT|PATCH|DELETE)/i.test(args) || /^\(\s*['"](POST|PUT|PATCH|DELETE)['"]/i.test(args);
      const fn = enclosing(line);
      units.push({ line, endpoint: ep, segment: seg, query, callee, writes, fn: fn.name, fnText: fn.text });
    });
  }
  return { units, fulls };
}

// A request passes when it narrows itself, or is a write, or is the bare path of
// an endpoint whose bare form is the summary.
function narrowed(u) {
  const ep = u.endpoint;
  if (u.writes) return true;
  if (u.query === null) return !!ep.bareIsSummary; // bare path
  if (!ep.markers.length) return false;
  const has = (text) => ep.markers.some(mk => text.includes(mk));
  if (u.query.length && !u.query.endsWith('&')) return has(u.query); // a complete query states its own form
  return has(u.fnText); // a query builder: the function adds the parameters
}

function allowed(u) {
  return ALLOWLIST.filter(a => a.endpoint === u.endpoint.path && (!a.fn || a.fn === u.fn));
}

const APP_SRC = fs.readFileSync(APP_JS, 'utf8');

// ── Part 1: static scan ──────────────────────────────────────────────────────

test('static: every list request in app.js is a small form or an allowlisted call site', () => {
  const { units } = scanSource(APP_SRC);
  assert.ok(units.length > 40, `scanner found only ${units.length} list request literals; the tokenizer lost the file`);
  const bad = units.filter(u => !narrowed(u) && !allowed(u).length);
  assert.deepEqual(bad.map(u => `app.js:${u.line} ${u.fn} ${u.endpoint.path}${u.query === null ? '' : '?' + u.query}`), [],
    'list request without view=summary, a limit or a conditional form, and not in the allowlist');
});

test('static: sessions, Automata and alerts list requests use the small form with no exception', () => {
  const { units } = scanSource(APP_SRC);
  const strict = units.filter(u => ['/api/sessions', '/api/autonomous/prds', '/api/alerts'].includes(u.endpoint.path));
  assert.ok(strict.length >= 6, `expected the sessions, Automata and alerts call sites, found ${strict.length}`);
  for (const u of strict) {
    assert.ok(narrowed(u), `app.js:${u.line} (${u.fn}) requests ${u.endpoint.path} without a small form`);
    assert.equal(allowed(u).length, 0, `${u.endpoint.path} must not need an allowlist entry (${u.fn})`);
  }
  // The shared paged fetchers exist and name the summary form.
  const fns = new Set(strict.map(u => u.fn));
  for (const name of ['_sessionsFetchPage', '_prdPanelQuery', '_automataQuery', '_alertsFetch']) {
    assert.ok(fns.has(name), `${name} no longer builds a list URL`);
  }
});

test('static: full=1 appears in no request built by app.js', () => {
  const { fulls } = scanSource(APP_SRC);
  assert.deepEqual(fulls.map(f => `app.js:${f.line} ${f.text}`), []);
  assert.ok(!/['"`][^'"`\n]*\bfull=(1|true)\b/.test(APP_SRC.replace(/\/\/.*$/gm, '')), 'full=1 inside a string literal');
});

test('static: every allowlist entry is used, has a reason, and is recorded in the audit note', () => {
  const { units } = scanSource(APP_SRC);
  const note = fs.readFileSync(AUDIT_NOTE, 'utf8');
  for (const a of ALLOWLIST) {
    assert.ok(a.reason && a.reason.length >= 30, `allowlist entry for ${a.endpoint} needs the audit note's reason`);
    assert.ok(a.trigger, `allowlist entry for ${a.endpoint} needs its trigger`);
    const hit = units.filter(u => u.endpoint.path === a.endpoint && (!a.fn || u.fn === a.fn));
    assert.ok(hit.length, `stale allowlist entry: ${a.endpoint}${a.fn ? ' in ' + a.fn : ''} no longer appears in app.js`);
    assert.ok(hit.some(u => !narrowed(u)), `allowlist entry for ${a.endpoint}${a.fn ? ' in ' + a.fn : ''} covers only requests that now pass; remove it`);
    assert.ok(note.includes(a.endpoint), `${a.endpoint} is not recorded in docs/plans/web-list-fetch-audit.md`);
    if (a.fn) assert.ok(note.includes('`' + a.fn + '`') || note.includes(a.fn), `${a.fn} is not recorded in docs/plans/web-list-fetch-audit.md`);
  }
});

test('static: the scanner flags a bare sessions list, a full=1 request, an unlisted fan-out and ignores comments and writes', () => {
  const src = [
    "function good() { return apiFetch('/api/sessions?view=summary&limit=50'); }",
    "function badBare() { return apiFetch('/api/sessions'); }",
    "function badFull() { return apiFetch('/api/autonomous/prds?full=1'); }",
    "function builder(c) { const q = ['limit=50']; if (c) q.push('cursor=' + c); return '/api/autonomous/prds?' + q.join('&'); }",
    "function badBuilder() { const q = ['status=x']; return '/api/alerts?' + q.join('&'); }",
    "function fanOut() { return apiFetch('/api/sessions/aggregated'); }",
    "function write() { return apiFetch('/api/alerts', { method: 'POST', body: '{}' }); }",
    "// apiFetch('/api/sessions') in a comment",
    "function tpl(p) { return fetch(`${p}/api/alerts?limit=${n}`); }",
    "function sub(id) { return apiFetch('/api/sessions/' + id + '/status'); }",
    "function rx() { return /\\/api\\/sessions/.test(x) ? fetch('/api/alerts') : 0; }",
  ].join('\n');
  const { units, fulls } = scanSource(src);
  const verdict = Object.fromEntries(units.map(u => [u.fn, narrowed(u)]));
  assert.equal(verdict.good, true);
  assert.equal(verdict.badBare, false);
  assert.equal(verdict.badFull, true, 'bare-summary endpoint with a query: caught by the full=1 rule instead');
  assert.equal(fulls.length, 1);
  assert.equal(verdict.builder, true);
  assert.equal(verdict.badBuilder, false);
  assert.equal(verdict.fanOut, false);
  assert.equal(verdict.write, true);
  assert.equal(verdict.tpl, true);
  assert.ok(!units.some(u => u.fn === 'sub'), 'a per-session sub-resource is not a list');
  assert.equal(units.filter(u => u.fn === 'rx').length, 1, 'the regex literal does not swallow the real call');
  assert.equal(units.length, 9 - 1 + 0, 'comment and sub-resource ignored');
});

// ── Runtime harness ──────────────────────────────────────────────────────────

function makeClock() {
  const c = { now: Date.UTC(2026, 9, 23, 12, 0, 0), seq: 0, timers: new Map() };
  c.setTimeout = (fn, ms) => { const id = ++c.seq; c.timers.set(id, { fn, at: c.now + Math.max(0, ms || 0), every: 0 }); return id; };
  c.setInterval = (fn, ms) => { const id = ++c.seq; const every = Math.max(1, ms || 1); c.timers.set(id, { fn, at: c.now + every, every }); return id; };
  c.clear = (id) => { c.timers.delete(id); };
  c.advance = async (ms) => {
    const end = c.now + ms;
    for (;;) {
      let next = null;
      for (const [id, t] of c.timers) if (t.at <= end && (!next || t.at < next.t.at)) next = { id, t };
      if (!next) break;
      c.now = Math.max(c.now, next.t.at);
      if (next.t.every) next.t.at += next.t.every; else c.timers.delete(next.id);
      try { next.t.fn(); } catch (e) { /* a stubbed DOM may reject a repaint; only the requests matter here */ }
      await flushAsync(2);
    }
    c.now = end;
    await flushAsync(10);
  };
  return c;
}

// The seeded data set, shaped like perfSeedSessions/perfSeedAutomata in
// internal/server/perf_seed_test.go. Text is generated lazily so a test that
// never asks for the full form never builds the multi-megabyte bodies.
const SEED = {
  sessions: 3000,
  bigTaskEvery: 20,
  bigTaskBytes: 128 * 1024,
  automata: 300,
  heavyBytes: [7, 8, 9, 10, 11, 13].map(m => m << 20),
  lightBytes: 60 * 1024,
  alerts: 500,
  summaryChars: 200,
};
const textOf = (n) => { const w = 'the task session output checkpoint verifier result error warning retry '; return w.repeat(Math.ceil(n / w.length)).slice(0, n); };
const SESSION_STATES = ['running', 'waiting_input', 'complete', 'complete', 'failed', 'killed'];
function sessionRow(i, full) {
  const id = (i + 1).toString(16).padStart(4, '0');
  const bigTask = i % SEED.bigTaskEvery === 0;
  const row = {
    id, full_id: 'perfhost-' + id, name: 'perf-session-' + String(i).padStart(4, '0'), project_dir: '/srv/perf/project',
    state: SESSION_STATES[i % SESSION_STATES.length], hostname: 'perfhost', backend_family: 'claude-code',
    created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T01:00:00Z', output_mode: 'log', input_mode: 'none',
  };
  const task = bigTask ? textOf(SEED.bigTaskBytes) : 'seeded task ' + i + ': ' + textOf(200);
  const lastResponse = textOf(1500), longSummary = textOf(3000);
  if (full) return Object.assign(row, { task, last_response: lastResponse, last_summary_long: longSummary });
  const cut = (s) => s.slice(0, SEED.summaryChars);
  return Object.assign(row, { task: cut(task), last_response: cut(lastResponse), last_summary_long: cut(longSummary), truncated_fields: ['task', 'last_response', 'last_summary_long'] });
}
const PRD_STATUSES = ['draft', 'approved', 'running', 'completed', 'failed', 'blocked'];
function prdBytes(i) { return i < SEED.heavyBytes.length ? SEED.heavyBytes[i] : SEED.lightBytes; }
function prdRow(i) {
  return {
    id: (0x10000000 + i).toString(16).padStart(8, '0'), title: 'perf automaton ' + String(i).padStart(3, '0'),
    status: PRD_STATUSES[i % PRD_STATUSES.length], rev: 1, project_dir: '/srv/perf/project',
    created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T01:00:00Z',
    story_count: i < SEED.heavyBytes.length ? 8 : 3, task_count: i < SEED.heavyBytes.length ? 96 : 12,
    task_status_counts: { completed: i < SEED.heavyBytes.length ? 96 : 12 },
  };
}
// The full form carries every story and task, the heavy ones 7-13 MB of text.
function prdFullJSON(i) {
  const row = prdRow(i);
  row.spec = textOf(2000);
  row.stories = [{ id: row.id + '-s00', title: 'story 0', tasks: [{ id: row.id + '-s00-t00', title: 'task 0.0', spec: textOf(prdBytes(i)) }] }];
  return JSON.stringify(row);
}

const ALERT_ROWS = Array.from({ length: SEED.alerts }, (_, i) => ({ id: 'a' + i, level: 'info', title: 'perf alert ' + i, body: textOf(300), created_at: '2026-10-23T01:00:00Z', source: 'system', read: i % 2 === 0 }));

// answer maps a request URL to a response body string, the way the server
// answers it for the seeded data set. Unknown endpoints answer an empty object.
function answer(urlIn) {
  const url = String(urlIn).replace(/^\/api\/proxy\/[^/]+/, '');
  const [p, qs = ''] = url.split('?');
  const q = new URLSearchParams(qs);
  const page = (rows, key, extra) => {
    const limit = Number(q.get('limit')) || rows.length;
    const start = q.get('cursor') ? Number(q.get('cursor')) : 0;
    const slice = rows.slice(start, start + limit);
    const next = start + limit < rows.length ? String(start + limit) : '';
    return JSON.stringify(Object.assign({ [key]: slice, total: rows.length, next_cursor: next }, extra || {}));
  };
  if (p === '/api/sessions') {
    const wantStates = q.get('states');
    let idx = Array.from({ length: SEED.sessions }, (_, i) => i);
    if (wantStates === 'active') idx = idx.filter(i => ['running', 'waiting_input'].includes(SESSION_STATES[i % SESSION_STATES.length]));
    if (q.get('view') === 'summary') return page(idx.map(i => sessionRow(i, false)), 'sessions');
    if (q.get('limit')) return page(idx.map(i => sessionRow(i, false)), 'sessions');
    return JSON.stringify(idx.map(i => sessionRow(i, true))); // the plain list: every record, whole
  }
  if (p === '/api/autonomous/prds') {
    if (q.get('full') === '1') return '{"prds":[' + Array.from({ length: SEED.automata }, (_, i) => prdFullJSON(i)).join(',') + '],"total":' + SEED.automata + '}';
    let idx = Array.from({ length: SEED.automata }, (_, i) => i);
    const statuses = q.get('status');
    if (statuses) idx = idx.filter(i => statuses.split(',').includes(PRD_STATUSES[i % PRD_STATUSES.length]));
    else if (q.get('active') === '1') idx = idx.filter(i => !['completed', 'failed'].includes(PRD_STATUSES[i % PRD_STATUSES.length]));
    const rows = idx.map(prdRow);
    // The bare path is the summary default: the first page plus every active row.
    return page(rows, 'prds');
  }
  if (p === '/api/alerts') {
    const limit = Number(q.get('limit')) || ALERT_ROWS.length;
    const rows = q.get('unread') === '1' ? ALERT_ROWS.filter(a => !a.read) : ALERT_ROWS.slice(0, limit);
    return JSON.stringify({ alerts: rows, unread_count: ALERT_ROWS.filter(a => !a.read).length, total: ALERT_ROWS.length, has_more: rows.length < ALERT_ROWS.length });
  }
  if (p === '/api/automations') return JSON.stringify(Array.from({ length: 20 }, (_, i) => ({ id: 'au' + i, name: 'perf-automation-' + i, status: 'active', version: 1, members: [] })));
  if (p === '/api/llms') return '{"llms":[]}';
  if (p === '/api/autonomous/tags') return '{"tags":[]}';
  if (p === '/api/commands') return '[]';
  return '{}';
}

// makePage loads app.js with a recording fetch and a fake clock.
function makePage({ server = '', panels = [], connected = true } = {}) {
  const clock = makeClock();
  const els = {};
  const byId = {};
  for (const id of panels) { els[id] = makeStubElement(); byId[id] = els[id]; }
  const sandbox = buildSandbox({ byId });
  sandbox.setTimeout = clock.setTimeout; sandbox.setInterval = clock.setInterval;
  sandbox.clearTimeout = clock.clear; sandbox.clearInterval = clock.clear;
  sandbox.AbortController = AbortController;
  sandbox.Date = class extends Date {
    constructor(...a) { if (a.length) super(...a); else super(clock.now); }
    static now() { return clock.now; }
  };
  const listeners = {};
  sandbox.WebSocket = function () {
    return { readyState: 1, addEventListener(t, fn) { listeners[t] = fn; }, send() {}, close() {} };
  };
  sandbox.WebSocket.OPEN = 1; sandbox.WebSocket.CONNECTING = 0; sandbox.WebSocket.CLOSING = 2; sandbox.WebSocket.CLOSED = 3;
  const calls = []; // { url, method, bytesOut, bytesIn }
  sandbox.fetch = (url, init) => {
    const method = String((init && init.method) || 'GET').toUpperCase();
    const text = answer(url);
    const rec = { url: String(url), method, bytesOut: Buffer.byteLength(String(url)) + Buffer.byteLength(String((init && init.body) || '')), bytesIn: Buffer.byteLength(text) };
    calls.push(rec);
    return Promise.resolve({
      ok: true, status: 200, headers: { get() { return null; } },
      text: () => Promise.resolve(text), json: () => Promise.resolve(JSON.parse(text)),
    });
  };
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n = sandbox._i18n || {};
  sandbox._i18n.bundle = EN;
  const run = (code) => vm.runInContext(code, sandbox);
  if (server) run(`state.activeServer = ${JSON.stringify(server)};`);
  run(`state.connected = ${connected ? 'true' : 'false'};`);
  return { sandbox, clock, els, calls, listeners, run };
}

// classify says whether a recorded request is a list request in its full form.
function fullFormReason(call, activeServer) {
  if (call.method !== 'GET') return '';
  const url = call.url.replace(/^\/api\/proxy\/[^/]+/, '');
  const [p, qs = ''] = url.split('?');
  const q = new URLSearchParams(qs);
  if (q.get('full') === '1' || q.get('full') === 'true') return 'full=1';
  const ep = ENDPOINT_BY_PATH.get(p);
  if (!ep) return '';
  if (ep.aggregated && activeServer !== 'all') return 'aggregated endpoint outside the All-servers view';
  if (!ep.markers.length) return '';
  const narrowedQuery = ep.markers.some(mk => qs.includes(mk));
  if (!narrowedQuery && !(ep.bareIsSummary && !qs)) return 'no small form';
  return '';
}
const fullForms = (page, activeServer) => page.calls.map(c => ({ c, r: fullFormReason(c, activeServer) })).filter(x => x.r).map(x => `${x.c.method} ${x.c.url}: ${x.r}`);

const bytesOf = (calls) => calls.reduce((n, c) => n + c.bytesOut + c.bytesIn, 0);
const listCalls = (calls) => calls.filter(c => c.method === 'GET' && /\/api\/(sessions|autonomous\/prds|alerts|automations)(\?|$)/.test(c.url.replace(/^\/api\/proxy\/[^/]+/, '')));

// ── Part 2: runtime ──────────────────────────────────────────────────────────

test('runtime: the Automata tab load requests the paged summary list', async () => {
  const page = makePage({ panels: ['automataPanel'] });
  await page.run('loadAutomataPanel()');
  const lists = page.calls.filter(c => c.url.startsWith('/api/autonomous/prds'));
  assert.ok(lists.length >= 1, 'no Automata list request');
  assert.deepEqual(fullForms(page, ''), []);
  assert.match(lists[0].url, /limit=\d+/);
});

test('runtime: the Sessions tab load requests the first summary page', async () => {
  const page = makePage({ panels: ['sessionsList'] });
  await page.run('loadSessionsList()');
  const lists = page.calls.filter(c => c.url.startsWith('/api/sessions'));
  assert.equal(lists.length, 1);
  assert.match(lists[0].url, /^\/api\/sessions\?view=summary&limit=\d+$/);
  assert.deepEqual(fullForms(page, ''), []);
});

test('runtime: navigating to each list tab requests no full form', async () => {
  for (const view of ['sessions', 'autonomous', 'alerts', 'automation']) {
    const page = makePage({ panels: ['sessionsList', 'automataPanel', 'alertsPanel', 'automationPanel', 'prdPanel'] });
    try { page.run(`navigate(${JSON.stringify(view)}, null, true)`); } catch (e) { /* a stub DOM may stop a repaint; requests already issued count */ }
    await flushAsync(40);
    assert.deepEqual(fullForms(page, ''), [], view);
  }
});

for (const [label, server] of [['local', ''], ['named server', 'peer one']]) {
  test(`runtime: the reconnect refresh (${label}) requests the first summary page and nothing in full form`, async () => {
    const page = makePage({ server, panels: ['sessionsList', 'automataPanel'] });
    page.run('connect()');
    page.listeners.open();
    await flushAsync(40);
    page.calls.length = 0;
    page.listeners.close({ code: 1006 });
    page.run('state.ws = null; state.connected = false; state.reconnectTimer = null;');
    page.run('connect()');
    page.listeners.open();
    await flushAsync(60);
    assert.ok(page.calls.some(c => /\/api\/sessions\?view=summary&limit=\d+/.test(c.url)), `no summary page: ${page.calls.map(c => c.url)}`);
    assert.deepEqual(fullForms(page, server), []);
  });
}

test('runtime: the periodic refresh timers request no list in full form', async () => {
  const page = makePage({ panels: ['sessionsList', 'automataPanel', 'prdPanel', 'automationPanel', 'alertsPanel'], connected: false });
  // Start every list loader, then let the fake clock run past several poll periods.
  for (const code of ['loadSessionsList()', 'loadAutomataPanel()', 'loadPRDPanel()', 'loadAutomationList()', 'renderAlertsView()', '_dashFetchPRDs()']) {
    try { await page.run(code); } catch (e) { /* stubbed DOM */ }
  }
  page.run(`state.activeView = 'autonomous'`);
  const before = page.calls.length;
  await page.clock.advance(125 * 1000);
  assert.ok(page.calls.length > before, 'the timers issued no refresh at all; the harness is not driving them');
  assert.deepEqual(fullForms(page, ''), []);
  // Each timer round repeats the same small request: the list loaders poll the summary list.
  const polled = page.calls.slice(before).filter(c => c.url.startsWith('/api/autonomous/prds?'));
  assert.ok(polled.length >= 1, 'the Automata 30 s poll did not run');
});

test('runtime: the dashboard refresh asks only for the live statuses with a limit', async () => {
  const page = makePage();
  await page.run('_dashFetchPRDs()');
  assert.deepEqual(page.calls.map(c => c.url), ['/api/autonomous/prds?status=running,blocked,planning&limit=500']);
  assert.deepEqual(fullForms(page, ''), []);
});

test('runtime: the classifier flags the full forms it exists to catch', () => {
  const call = (url, method = 'GET') => ({ url, method });
  assert.equal(fullFormReason(call('/api/sessions'), ''), 'no small form');
  assert.equal(fullFormReason(call('/api/proxy/p/api/sessions'), ''), 'no small form');
  assert.equal(fullFormReason(call('/api/autonomous/prds?full=1'), ''), 'full=1');
  assert.equal(fullFormReason(call('/api/alerts'), ''), 'no small form');
  assert.equal(fullFormReason(call('/api/sessions/aggregated'), ''), 'aggregated endpoint outside the All-servers view');
  assert.equal(fullFormReason(call('/api/sessions/aggregated'), 'all'), '');
  assert.equal(fullFormReason(call('/api/autonomous/prds'), ''), '');
  assert.equal(fullFormReason(call('/api/sessions?view=summary&limit=50'), ''), '');
  assert.equal(fullFormReason(call('/api/alerts', 'POST'), ''), '');
});

// ── Part 3: byte ratchet ─────────────────────────────────────────────────────

test('bytes: the seeded stub is as heavy as the live data set', () => {
  assert.ok(Buffer.byteLength(answer('/api/sessions')) > 20 * LIST_PAGE_BUDGET, 'plain session list is megabytes');
  assert.ok(Buffer.byteLength(answer('/api/autonomous/prds?full=1')) > 40 * LIST_PAGE_BUDGET, 'full Automata list is tens of megabytes');
  assert.ok(Buffer.byteLength(answer('/api/sessions?view=summary&limit=50')) < LIST_PAGE_BUDGET);
});

for (const [name, panels, code, view] of [
  ['Sessions', ['sessionsList'], 'loadSessionsList()', 'sessions'],
  ['Automata', ['automataPanel'], 'loadAutomataPanel()', 'autonomous'],
  ['Alerts', ['alertsPanel'], '_alertsFetch()', 'alerts'],
  ['Automation', ['automationPanel'], 'loadAutomationList()', 'automation'],
]) {
  test(`bytes: first load of the ${name} tab stays under budget`, async () => {
    const page = makePage({ panels });
    page.run(`state.activeView = ${JSON.stringify(view)}`);
    await page.run(code);
    await flushAsync(20);
    assert.deepEqual(fullForms(page, ''), []);
    for (const c of listCalls(page.calls)) {
      assert.ok(c.bytesIn <= LIST_PAGE_BUDGET, `${c.url} returned ${c.bytesIn} bytes, budget ${LIST_PAGE_BUDGET}`);
    }
    const total = bytesOf(page.calls);
    assert.ok(total > 0, 'the load requested nothing');
    assert.ok(total <= TAB_FIRST_LOAD_BUDGET, `${name} first load moved ${total} bytes over ${page.calls.length} requests, budget ${TAB_FIRST_LOAD_BUDGET}`);
  });
}

test('bytes: the reconnect refresh stays under budget', async () => {
  const page = makePage({ panels: ['sessionsList'] });
  page.run('connect()');
  page.listeners.open();
  await flushAsync(40);
  page.calls.length = 0;
  page.listeners.open();
  await flushAsync(60);
  const total = bytesOf(page.calls);
  assert.ok(total <= TAB_FIRST_LOAD_BUDGET, `reconnect refresh moved ${total} bytes, budget ${TAB_FIRST_LOAD_BUDGET}`);
});

test('bytes: a full-form request would blow the budget, so the ratchet bites', async () => {
  const page = makePage();
  const text = await (await page.sandbox.fetch('/api/sessions')).text();
  assert.ok(Buffer.byteLength(text) > TAB_FIRST_LOAD_BUDGET);
  assert.ok(bytesOf(page.calls) > TAB_FIRST_LOAD_BUDGET);
  assert.notDeepEqual(fullForms(page, ''), []);
});
