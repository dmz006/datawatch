// The Templates tab's sub-tabs: the registry drives the sub-tab bar
// (one button per entry), the currently selected key persists under
// `automata_templates_subtab` and both the Templates sub-tab
// (default) and the standard tasks sub-tab show their own content.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

// freshSandbox loads the real app.js into a shared stub browser, same
// pattern as app-automation-subtabs.test.js. #automataPanel is
// pre-registered in byId so we can see what lands there. opts.preStore
// seeds localStorage BEFORE app.js runs, so the registry's initial
// stored-value read sees it.
function freshSandbox(opts = {}) {
  const els = { automataPanel: makeStubElement() };
  const sandbox = buildSandbox({ byId: els });
  sandbox.URLSearchParams = URLSearchParams;
  if (opts.preStore) {
    for (const [k, v] of Object.entries(opts.preStore)) sandbox.localStorage.setItem(k, v);
  }
  sandbox.fetch = () => Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve({}), text: () => Promise.resolve('{}') });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  // t() falls back to the raw key until a bundle loads; install the
  // English one so the rendered strings are the real user-visible ones.
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return { sb: sandbox, panel: els.automataPanel, els };
}

// tmplSubtabButtons lists the Templates sub-tab bar buttons in render
// order. The first-row tab bar uses data-tab and the Automation second
// row uses data-subtab, so data-tmplsubtab cannot pick up either.
function tmplSubtabButtons(html) {
  return [...html.matchAll(/class="output-tab( active)? ?" role="tab" aria-selected="(true|false)" data-tmplsubtab="([a-z]+)"/g)]
    .map(m => ({ key: m[3], active: !!m[1], selected: m[2] === 'true' }));
}

test('both sub-tabs render in order: Templates first (active by default), standard tasks second', () => {
  const { sb } = freshSandbox();
  const row = tmplSubtabButtons(sb._templatesSubtabsBarHTML());
  assert.deepEqual(row.map(b => b.key), ['automaton', 'standard'], 'the bar has exactly two buttons in registry order');
  row.forEach((b, i) => {
    assert.equal(b.active, i === 0, b.key + (i === 0 ? ' is active by default' : ' is not active by default'));
    assert.equal(b.selected, i === 0, b.key + ' aria-selected matches');
  });
});

test('switching changes the body and remembers the choice', async () => {
  const { sb, panel } = freshSandbox();
  vm.runInContext('_automataState.tab = "templates"', sb);
  const before = panel.innerHTML;
  sb.switchTemplatesSubtab('standard');
  assert.notEqual(panel.innerHTML, before, 'body changed after switching to standard tasks');
  assert.match(panel.innerHTML, /loading-eye-block/, 'standard tasks renderer shows the loading state');
  assert.equal(sb.localStorage.getItem('automata_templates_subtab'), 'standard', 'the standard tasks choice is persisted');
  sb.switchTemplatesSubtab('automaton');
  assert.equal(sb.localStorage.getItem('automata_templates_subtab'), 'automaton', 'switching back to Templates persists again');
  // The automaton entry's loader fetches, then re-renders; let the
  // stubbed fetch settle.
  await flushAsync();
  assert.match(panel.innerHTML, /oninput="_automataOnTmplSearch\(this\.value\)"/, 'Templates body rendered by its real renderer');
  assert.notEqual(panel.innerHTML, before, 'body changed after switching back');
});

test('a fresh load (nothing stored) defaults to Templates', () => {
  const { sb } = freshSandbox();
  assert.equal(sb.localStorage.getItem('automata_templates_subtab'), null, 'nothing has been stored yet');
  const row = tmplSubtabButtons(sb._templatesSubtabsBarHTML());
  assert.equal(row[0].active, true, 'Templates is active');
  assert.equal(row[1].active, false, 'standard tasks is not active');

  // A corrupt stored value falls back to the default instead of
  // sticking: a key no entry knows must land on the default sub-tab.
  const { sb: sbBad } = freshSandbox({ preStore: { automata_templates_subtab: 'bogus-key' } });
  const rowBad = tmplSubtabButtons(sbBad._templatesSubtabsBarHTML());
  assert.equal(rowBad[0].active, true, 'a corrupt stored value still lands on the default sub-tab');

  // A stored choice the registry does know is honored on load.
  const { sb: sbStored } = freshSandbox({ preStore: { automata_templates_subtab: 'standard' } });
  const rowStored = tmplSubtabButtons(sbStored._templatesSubtabsBarHTML());
  assert.equal(rowStored[1].active, true, 'a stored standard tasks choice is honored on load');
});

test('a fake third registry entry renders as a third button after the existing two, and its renderer runs when selected', () => {
  const { sb, panel } = freshSandbox();
  vm.runInContext(`
    _automataState.tab = "templates";
    _AUTOMATA_TEMPLATE_SUBTABS.push({
      key: 'third',
      titleKey: 'third_subtab_title',
      load() {},
      render() {
        const p = document.getElementById('automataPanel');
        if (p) p.innerHTML = 'third-entry-renderer';
      },
    });
  `, sb);
  const row = tmplSubtabButtons(sb._templatesSubtabsBarHTML());
  assert.deepEqual(row.map(b => b.key), ['automaton', 'standard', 'third'], 'the third entry renders as a third button in order');
  row.forEach(b => assert.equal(b.active, b.key === 'automaton', 'existing default unchanged: only ' + (b.active ? b.key : '') + ' is active'));
  sb.switchTemplatesSubtab('third');
  assert.equal(panel.innerHTML, 'third-entry-renderer', 'selecting the third entry runs its renderer');
  assert.equal(sb.localStorage.getItem('automata_templates_subtab'), 'third', 'the third choice persists like the others');
});

test('each sub-tab shows its empty text when its list is empty', async () => {
  const { sb, panel } = freshSandbox();
  vm.runInContext('_automataState.tab = "templates"', sb);

  // Templates: the default sub-tab, with an empty list from the stubbed fetch.
  sb.switchTemplatesSubtab('automaton');
  await flushAsync();
  assert.match(panel.innerHTML, /No Automaton templates yet\. An Automaton template is a reusable Automaton definition you can instantiate/, 'the Templates empty text says what a template is');
  assert.match(panel.innerHTML, /save an Automaton as a template, or create one from the template store/, 'the Templates empty text says how to add one');

  // Standard tasks: an empty list shows the explanation instead of a bare table.
  sb.switchTemplatesSubtab('standard');
  await flushAsync();
  assert.match(panel.innerHTML, /No standard tasks yet\. Standard tasks are the tasks the decomposer adds to every story/, 'the standard tasks empty text says what they are');
  assert.match(panel.innerHTML, /When autonomous\.task_templates\.enabled is on, the decomposer adds each standard task to every story/, 'the standard tasks empty text names the enabling setting');
  assert.ok(!panel.innerHTML.includes('class="automata-standard-tasks"'), 'no table is drawn over an empty list');
});

test('the first sub-tab is labelled "Templates" on every locale, and the key and deep link are unchanged', () => {
  const { sb } = freshSandbox();
  const html = sb._templatesSubtabsBarHTML();
  assert.match(html, /data-tmplsubtab="automaton"[^>]*>\s*Templates\s*</, 'the English sub-tab button reads Templates');
  assert.doesNotMatch(html, /Automaton templates/, 'the old label is gone from the strip');
  for (const l of ['en', 'fr', 'es', 'de', 'ja']) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));
    assert.equal(b.automata_templates_subtab_automaton, 'Templates', l + ' sub-tab label');
    assert.equal(b.automata_tab_templates, 'Templates', l + ' tab label');
  }
  // The route stays: the stored key is still "automaton" and still selects it.
  const s2 = freshSandbox({ preStore: { automata_templates_subtab: 'automaton' } });
  assert.equal(tmplSubtabButtons(s2.sb._templatesSubtabsBarHTML())[0].active, true, 'stored key "automaton" still selects the sub-tab');
});
