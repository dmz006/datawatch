// The grouping dropdown keeps its element, its focus and its stored
// selection across list repaints. A list repaint replaces the panel's
// innerHTML, so without the reconcile step the group-by <select> in the
// shared list controls loses focus (and the open state) and can be
// re-drawn from stale inputs. These tests pin, for the 'automata' and
// 'automation' scopes, that:
//   1. choosing a group stores it and the re-drawn select shows it;
//   2. a repaint that lands while the select is focused leaves the SAME
//      element in place (no rebuild), with its selection restored;
//   3. a repaint with unchanged inputs does not replace the controls
//      element — the select is patched in place;
//   4. a simulated page reload (a fresh context, same localStorage) shows
//      the stored group in the select.
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

// load returns a fresh app.js context. Passing the same `store` object to
// two loads simulates two page loads sharing localStorage.
function load(store) {
  const sandbox = buildSandbox({ byId: {} });
  const m = store || {};
  sandbox.localStorage = {
    getItem: (k) => (k in m ? m[k] : null),
    setItem: (k, v) => { m[k] = String(v); },
    removeItem: (k) => { delete m[k]; },
  };
  sandbox.innerWidth = 1200; // a stable viewport so the mode default is fixed
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}
const run = (sb, code) => vm.runInContext(code, sb);

// ── Fake panel DOM ─────────────────────────────────────────────────────────
// makeRoot emulates the list panel just enough for the real app.js repaint
// path: assigning innerHTML "parses" the toolbar HTML that app.js itself
// produced (_listControlsHTML) into a FRESH .list-controls element — the
// same identity semantics a real browser's innerHTML replacement has.
function makeRoot(scope) {
  const root = { children: [] };
  const parse = (html) => {
    const selRe = /<select[^>]*class="list-group-select"[^>]*>([\s\S]*?)<\/select>/;
    const selHtml = (html.match(selRe) || [])[1];
    if (selHtml === undefined) return null;
    let selected = 'none';
    const selOpt = selHtml.match(/<option value="([^"]*)"[^>]*selected/);
    if (selOpt) selected = selOpt[1];
    const sel = {
      innerHTML: selHtml,
      _value: selected,
      _focusCalls: 0,
      focus() { this._focusCalls++; },
    };
    Object.defineProperty(sel, 'value', {
      get() { return sel._value; },
      set(v) { sel._value = v; },
    });
    const btnRe = /class="tag-view-btn( active)?" aria-pressed="([^"]*)"/g;
    const btns = [];
    let bm;
    while ((bm = btnRe.exec(html)) !== null) {
      btns.push({
        classList: {
          _active: !!bm[1],
          contains(c) { return c === 'active' ? !!this._active : false; },
          toggle(c, force) { if (c === 'active') this._active = force === true; },
        },
        setAttribute() {},
        getAttribute(a) { return a === 'aria-pressed' ? bm[2] : null; },
      });
    }
    const fresh = {
      _sel: sel,
      getAttribute(a) { return a === 'data-list-scope' ? scope : null; },
      querySelector(q) { return q === 'select.list-group-select' ? sel : null; },
      querySelectorAll(q) { return q === '.tag-view-btn' ? btns : []; },
      replaceWith(node) {
        const i = root.children.indexOf(fresh);
        if (i >= 0) root.children[i] = node;
      },
    };
    return fresh;
  };
  Object.defineProperty(root, 'innerHTML', {
    get() { return ''; },
    set(html) {
      root._lastHtml = html;
      const fresh = parse(html);
      root.children = fresh ? [fresh] : [];
      root._lastParsed = fresh;
    },
  });
  root.querySelectorAll = (q) => (q === '.list-controls[data-list-scope]' ? root.children : []);
  return root;
}

// makePage wires the fake panel into a sandbox and drives the real repaint
// sequence app.js uses: the panel innerHTML is replaced, then
// _listControlsReconcile runs on it (exactly what listLoader's paint does).
function makePage(sb, scope) {
  const root = makeRoot(scope);
  sb.__root = root;
  return {
    root,
    repaint() {
      const html = run(sb, `_listControlsHTML('${scope}')`);
      root.innerHTML = html;
      run(sb, '_listControlsReconcile(__root)');
    },
    standing() { return root.children[0]; },
    select(el) { return el && el._sel; },
  };
}

const SCOPES = ['automata', 'automation'];

for (const scope of SCOPES) {
  test(`${scope}: choosing a group stores it and the re-drawn select shows it`, () => {
    const sb = load();
    const page = makePage(sb, scope);
    page.repaint();
    // First paint: the default group, shown in the select.
    assert.equal(run(sb, `_listGroupGet('${scope}')`), 'none');
    assert.equal(page.select(page.standing()).value, 'none');
    // The user's choice goes through the real entry point; it also asks for
    // a repaint, which the stub DOM cannot do — the store is what sticks.
    run(sb, `listSetGroup('${scope}','state')`);
    assert.equal(run(sb, `_listGroupGet('${scope}')`), 'state');
    // The next repaint (a poll) must draw the select from the stored group.
    page.repaint();
    const sel = page.select(page.standing());
    assert.equal(sel.value, 'state');
    assert.match(sel.innerHTML, /<option value="state" selected>/);
  });

  test(`${scope}: a repaint while the select is focused keeps the same element`, () => {
    const sb = load();
    const page = makePage(sb, scope);
    page.repaint();
    const first = page.standing();
    const sel = page.select(first);
    // The user is in the dropdown: it has focus.
    sb.document.activeElement = sel;
    run(sb, `listSetGroup('${scope}','state')`);
    page.repaint(); // a poll repaint lands while the select is focused
    const standing = page.standing();
    assert.ok(standing, 'a controls element stands after the repaint');
    assert.notStrictEqual(standing, page.root._lastParsed, 'the fresh node does not stand');
    assert.strictEqual(standing, first, 'the focused element is kept in place');
    assert.equal(page.select(standing).value, 'state', 'the selection is restored to the stored group');
    assert.ok(page.select(standing)._focusCalls >= 1, 'focus is re-asserted on the kept select');
  });

  test(`${scope}: a repaint with unchanged inputs does not replace the controls`, () => {
    const sb = load();
    const page = makePage(sb, scope);
    page.repaint();
    const first = page.standing();
    page.repaint(); // unchanged scope/mode/group: a poll repaint
    const standing = page.standing();
    assert.ok(standing, 'a controls element stands after the repaint');
    assert.notStrictEqual(standing, page.root._lastParsed, 'the fresh node does not stand');
    assert.strictEqual(standing, first, 'the live element is patched in place, not replaced');
    assert.equal(page.select(standing).value, run(sb, `_listGroupGet('${scope}')`));
  });

  test(`${scope}: a changed group rebuilds the toolbar but restores the stored selection`, () => {
    const sb = load();
    const page = makePage(sb, scope);
    page.repaint();
    const first = page.standing();
    run(sb, `listSetGroup('${scope}','state')`); // input changed; nothing focused
    page.repaint();
    const standing = page.standing();
    assert.ok(standing, 'a controls element stands after the repaint');
    assert.strictEqual(standing, page.root._lastParsed, 'with changed inputs the fresh toolbar stands');
    assert.notStrictEqual(standing, first);
    assert.equal(page.select(standing).value, 'state', 'the standing select shows the stored group');
    assert.match(page.select(standing).innerHTML, /<option value="state" selected>/);
  });

  test(`${scope}: the stored group is shown after a simulated page reload`, () => {
    const store = {}; // shared localStorage backing across the two loads
    const sb = load(store);
    run(sb, `listSetGroup('${scope}','state')`);
    // Page reload: a brand-new app.js context (fresh _listControlsLive)
    // reading the same localStorage.
    const sb2 = load(store);
    const page = makePage(sb2, scope);
    page.repaint();
    const sel = page.select(page.standing());
    assert.equal(run(sb2, `_listGroupGet('${scope}')`), 'state');
    assert.equal(sel.value, 'state');
    assert.match(sel.innerHTML, /<option value="state" selected>/);
  });
}

// The reconcile step is wired into the repaint paths every scope goes
// through — the shared listLoader paint and the Inbox toolbar repaint — so
// no scope has a per-scope copy of the logic.
test('reconcile runs from the shared listLoader paint and the inbox toolbar repaint', () => {
  const src = require('fs').readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  assert.match(src, /el\.innerHTML = html\(\);\s*if \(opts\.afterPaint\)/);
  assert.match(src, /try \{ _listControlsReconcile\(el\); \} catch \(_e\) \{ \/\* ignore \*\/ \}/);
  assert.match(src, /_listControlsReconcile\(el\); \} catch \(_e\) \{ \/\* the toolbar repaint stands \*\/ \}/);
  assert.match(src, /function _listControlsReconcile\(root\)/);
  assert.ok(!/function _listControlsReconcile(automata|automation|inbox)/.test(src));
});
