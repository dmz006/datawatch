// The session header's meta row keeps one height on a phone whatever the state
// badge says. A wrapping row grew by a line when WAITING_INPUT replaced RUNNING,
// which shrank the terminal by two rows and made tmux jump. On narrow screens the
// row must not wrap, the backend badge must be the part that shrinks, and the
// state badge, action buttons and icon group must keep their size.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');

const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');

function phoneBlock() {
  const start = css.indexOf('@media (max-width: 600px) {\n  .session-info-bar .meta { flex-wrap: nowrap');
  assert.ok(start >= 0, 'the narrow-screen rule for the session meta row is missing');
  let depth = 0, end = start;
  for (let i = css.indexOf('{', start); i < css.length; i++) {
    if (css[i] === '{') depth++;
    if (css[i] === '}') { depth--; if (depth === 0) { end = i + 1; break; } }
  }
  return css.slice(start, end);
}

test('the meta row does not wrap on a narrow screen', () => {
  const b = phoneBlock();
  assert.match(b, /\.session-info-bar \.meta \{[^}]*flex-wrap:\s*nowrap/);
  assert.match(b, /\.session-info-bar \.meta \{[^}]*overflow:\s*hidden/);
});

test('the backend badge is the part that shrinks, with an ellipsis', () => {
  const b = phoneBlock();
  const rule = b.match(/\.session-info-bar \.meta > \.backend-badge \{([^}]*)\}/);
  assert.ok(rule, 'backend badge rule missing');
  assert.match(rule[1], /min-width:\s*0/);
  assert.match(rule[1], /text-overflow:\s*ellipsis/);
  assert.match(rule[1], /white-space:\s*nowrap/);
  assert.match(rule[1], /flex:\s*0 1 auto/);
});

test('the state badge, action buttons and icon group keep their size', () => {
  const b = phoneBlock();
  assert.match(b, /\.session-info-bar \.meta > \.state,[\s\S]*\.session-info-bar \.meta > #actionBtns,[\s\S]*\.session-info-bar \.meta > span:last-child \{ flex-shrink: 0; \}/);
});

test('the default row still wraps on wide screens', () => {
  assert.match(css, /\.session-info-bar \.meta \{\n  display: flex;\n  gap: 6px;\n  align-items: center;\n  margin-top: 4px;\n  flex-wrap: wrap;\n\}/);
});
