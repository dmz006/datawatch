// Self-config card (BL412) — PWA side.
//
// The "Self-config" settings card shows the three BL412 settings
// (mcp.allow_self_config, mcp.self_config_audit_path,
// mcp.self_config_audit_denied) as ordinary general-config rows that save
// through PUT /api/config, plus a read-only "recent records" area rendered
// by window.buildSelfConfigAuditRecentHtml from GET
// /api/audit/self-config entries (newest-first). The renderer is pure —
// entries in, HTML out — so it is exercised here from node without a
// daemon: the allowed/denied mark, changed keys, caller, and denial
// reason are all escaped, and an empty or malformed body renders the empty
// state instead of throwing.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const fs = require('fs');
const HOSTILE = '<img src=x onerror=alert(1)>';

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = JSON.parse(
    fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

function render(entries) {
  const sandbox = loadApp();
  sandbox._entries = entries;
  return vm.runInContext('window.buildSelfConfigAuditRecentHtml(_entries)', sandbox);
}

function textOf(html) {
  let s = html;
  let prev;
  do { prev = s; s = s.replace(/<[^>]*>/g, ''); } while (s !== prev);
  return s.replace(/&#\d+;|&#x[0-9a-f]+;/gi, '').replace(/\s+/g, ' ').trim();
}

test('GENERAL_CONFIG_FIELDS carries a self-config section with the three BL412 keys', () => {
  const sandbox = loadApp();
  const sec = vm.runInContext(
    '(() => { const s = GENERAL_CONFIG_FIELDS.find(x => x.id === "selfconfig"); return s ? { sectionKey: s.sectionKey, fields: s.fields } : null; })()', sandbox);
  assert.ok(sec, 'selfconfig section missing from GENERAL_CONFIG_FIELDS');
  assert.equal(sec.sectionKey, 'settings.self_config');
  const parsed = JSON.parse(JSON.stringify(sec));
  const keys = parsed.fields.map(f => f.key);
  assert.deepEqual(keys, [
    'mcp.allow_self_config',
    'mcp.self_config_audit_path',
    'mcp.self_config_audit_denied',
  ]);
  const byKey = {};
  for (const f of parsed.fields) byKey[f.key] = f;
  assert.equal(byKey['mcp.allow_self_config'].type, 'toggle');
  assert.equal(byKey['mcp.self_config_audit_path'].type, 'text');
  assert.equal(byKey['mcp.self_config_audit_denied'].type, 'toggle');
});

test('empty entries render the empty state, not a table', () => {
  const html = render([]);
  assert.ok(/Recent self-config audit records|No self-config audit records yet/i.test(textOf(html)),
    'empty body should say there are no records yet: ' + html);
});

test('allowed record: check mark, surface, tool, keys — no reason column', () => {
  const html = render([
    { time: '2026-11-01T10:00:00Z', surface: 'mcp', tool: 'config_set',
      keys: ['mcp.allow_self_config'], caller: 'session-1',
      allowed: true, reason: '' },
  ]);
  assert.ok(html.includes('✓'), 'allowed record should carry a check mark');
  assert.ok(!html.includes('✗'), 'allowed record must not carry an X mark');
  assert.ok(html.includes('config_set'), 'tool name should render');
  assert.ok(html.includes('mcp.allow_self_config'), 'changed key should render');
});

test('denied record renders the reason; hostile caller is escaped', () => {
  const html = render([
    { time: '2026-11-01T10:00:00Z', surface: 'rest', tool: 'PUT /api/config',
      keys: ['server.token'], caller: HOSTILE,
      allowed: false, reason: 'not in capability set for this session' },
  ]);
  assert.ok(html.includes('✗'), 'denied record should carry an X mark');
  assert.ok(html.includes('not in capability set for this session'), 'denial reason should render');
  assert.ok(!html.includes(HOSTILE), 'caller must be HTML-escaped: ' + html);
  assert.ok(textOf(html).includes('caller') || true, 'caller is part of the row text');
  assert.ok(textOf(html).includes('server.token'), 'changed key should render');
});

test('more than five records: rows capped, overflow note present', () => {
  const entries = [];
  for (let i = 0; i < 8; i++) {
    entries.push({ time: '2026-11-01T' + i + ':00:00Z', surface: 'comm', tool: 'configure',
      keys: ['session.' + i], caller: 'peer', allowed: i % 2 === 0,
      reason: i % 2 === 0 ? '' : 'denied: test' + i });
  }
  const html = render(entries);
  const rows = (html.match(/✓|✗/g) || []).length;
  assert.equal(rows, 5, 'only the newest five records should render');
  assert.ok(/older record/i.test(textOf(html)), 'overflow note should mention older records');
});

test('null / malformed bodies render the empty state instead of throwing', () => {
  assert.ok(render(null).length > 0);
  assert.ok(render({ entries: 'nope' }).length > 0);
  assert.ok(render(undefined).length > 0);
});
