// GH#189 — regression test for the ported DocsLinks table (app.js) and
// defsLink's key-based lookup. Before this fix, defsLink(title) always
// guessed an anchor from the card TITLE against a single hardcoded file
// (datawatch-definitions.md) -- wrong for the majority of cards, whose
// manual heading wording differs from the card title, or whose correct
// target is an entirely different file. Confirmed every settingsSectionHeader
// call site's `key` argument already matches a DocsLinks.kt key one-for-one
// before porting the table (ported verbatim, not re-derived).

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox.defsLink, 'function', 'defsLink is not defined -- did it get renamed?');
  assert.equal(typeof sandbox.docsLinkForKey, 'function', 'docsLinkForKey is not defined -- did it get renamed?');
  sandbox.localStorage = { getItem: () => null }; // "show docs links" default-on
  return sandbox;
}

function callStr(sandbox, fnName, ...args) {
  args.forEach((a, i) => { sandbox['__arg' + i] = a; });
  const argList = args.map((_, i) => '__arg' + i).join(', ');
  const result = vm.runInContext(`${fnName}(${argList})`, sandbox);
  args.forEach((_, i) => { delete sandbox['__arg' + i]; });
  return result;
}

test('docsLinkForKey resolves a card key to its real page, not datawatch-definitions.md', () => {
  const sandbox = loadAppJS();
  // work_queue's card title is "Work Queue" -- the OLD title-slug behavior
  // would have produced "datawatch-definitions.md#work-queue", which
  // happens to be correct here by coincidence; lc_goose is the case that
  // proves the fix: its card title is "Goose (Block)", whose old slug
  // ("goose-block") never existed as a heading anywhere.
  assert.equal(callStr(sandbox, 'docsLinkForKey', 'lc_goose'), 'llm-backends.md#goose');
  assert.equal(callStr(sandbox, 'docsLinkForKey', 'work_queue'), 'datawatch-definitions.md#work-queue');
});

test('docsLinkForKey resolves a key whose correct target is a howto page, not the manual', () => {
  const sandbox = loadAppJS();
  assert.equal(callStr(sandbox, 'docsLinkForKey', 'alert_rules'), 'howto/alert-rules.md');
  assert.equal(callStr(sandbox, 'docsLinkForKey', 'secrets_store'), 'howto/secrets-manager.md');
});

test('docsLinkForKey prefix fallbacks: stats_*, lc_backend_*, cc_global_*, dash_*', () => {
  const sandbox = loadAppJS();
  assert.equal(callStr(sandbox, 'docsLinkForKey', 'stats_anything_unlisted'), callStr(sandbox, 'docsLinkForKey', 'stats'));
  assert.equal(callStr(sandbox, 'docsLinkForKey', 'lc_backend_goose'), 'llm-backends.md#goose');
  assert.equal(callStr(sandbox, 'docsLinkForKey', 'cc_global_signal'), 'messaging-backends.md#signal');
  assert.equal(callStr(sandbox, 'docsLinkForKey', 'dash_anything_unlisted'), callStr(sandbox, 'docsLinkForKey', 'view_dashboard'));
});

test('docsLinkForKey returns null for an unknown key (defsLink falls back to the old behavior)', () => {
  const sandbox = loadAppJS();
  assert.equal(callStr(sandbox, 'docsLinkForKey', 'totally_unknown_key_xyz'), null);
});

test('defsLink uses the table target for a known key, ignoring the title entirely', () => {
  const sandbox = loadAppJS();
  const html = callStr(sandbox, 'defsLink', 'lc_goose', 'Goose (Block)');
  assert.match(html, /#docs\/llm-backends\.md#goose/);
  assert.doesNotMatch(html, /goose-block/);
});

test('defsLink falls back to a title-slug anchor on datawatch-definitions.md for an unknown key', () => {
  const sandbox = loadAppJS();
  const html = callStr(sandbox, 'defsLink', 'totally_unknown_key_xyz', 'Some Card Title');
  assert.match(html, /#docs\/datawatch-definitions\.md#some-card-title/);
});

test('defsLink returns "" when the operator hid inline doc links', () => {
  const sandbox = loadAppJS();
  sandbox.localStorage = { getItem: (k) => (k === 'cs_show_docs_links' ? '0' : null) };
  assert.equal(callStr(sandbox, 'defsLink', 'lc_goose', 'Goose (Block)'), '');
});
