// Guard: style.css must have balanced braces and no block left open at the
// end of the file. An unclosed @media block (a merge once dropped one
// closing brace) makes every rule after it apply only inside that media
// query, so whole screens lose their styling with no error anywhere.
const { test } = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');

test('style.css braces are balanced and top-level rules are not nested', () => {
  const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8')
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/"(?:[^"\\\n]|\\.)*"|'(?:[^'\\\n]|\\.)*'/g, '""');
  let depth = 0;
  let line = 1;
  let firstOpenAtAt = -1;
  for (let i = 0; i < css.length; i++) {
    const ch = css[i];
    if (ch === '\n') line++;
    if (ch === '{') depth++;
    if (ch === '}') {
      depth--;
      assert.ok(depth >= 0, `extra closing brace near line ${line}`);
    }
  }
  assert.strictEqual(depth, 0, `style.css ends with ${depth} unclosed block(s)`);
  // A rule that starts at depth 0 right after an @media/@supports opener
  // without that opener closing would have been caught above; also make
  // sure no line-start '@media' appears while already inside a block.
  depth = 0;
  line = 1;
  const lines = css.split('\n');
  for (const l of lines) {
    if (/^@(media|supports)\b/.test(l)) {
      assert.strictEqual(depth, 0, `@media/@supports opened inside another block near line ${line}`);
    }
    for (const ch of l) {
      if (ch === '{') depth++;
      if (ch === '}') depth--;
    }
    line++;
  }
  void firstOpenAtAt;
});
