// Projects Overview sub-tabs — the bar uses the Projects sub-tab classes, each
// button switches the visible body, the choice survives a repaint, and the
// bar scrolls sideways on a phone instead of wrapping.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP = path.join(__dirname, 'app.js');
const KEY = 'cs_projects_subtab';

function makeStorage(initial = {}) {
  const m = new Map(Object.entries(initial));
  return {
    m,
    getItem(k) { return m.has(k) ? m.get(k) : null; },
    setItem(k, v) { m.set(k, String(v)); },
    removeItem(k) { m.delete(k); },
  };
}

function load({ hash = '', storage = makeStorage(), phone = false } = {}) {
  const out = { view: '' };
  const view = makeStubElement(h => { out.view = h; });
  view.insertAdjacentHTML = (pos, h) => { out.view = h + out.view; };
  const sandbox = buildSandbox({ byId: { automataPanel: view, view }, initialHash: hash });
  const ok = body => Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(body), text: () => Promise.resolve(JSON.stringify(body)) });
  sandbox.fetch = (url) => (String(url).includes('/api/projects') ? ok([{ name: 'acme', dir: '/work/acme' }]) : ok([]));
  sandbox.localStorage = storage;
  if (phone) {
    sandbox.innerWidth = 375;
    sandbox.innerHeight = 700;
    sandbox.matchMedia = (q) => ({ matches: /max-width/.test(String(q)), media: String(q), addEventListener() {}, addListener() {} });
  }
  vm.createContext(sandbox);
  loadScript(APP, sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  vm.runInContext("state.activeView = 'autonomous'; _automataState.tab = 'projects'", sandbox);
  return { sb: sandbox, out, storage };
}

const run = (sb, code) => vm.runInContext(code, sb);
const ids = html => [...html.matchAll(/data-projects-subtab="([a-z_]+)"/g)].map(m => m[1]);
// The part of the page after the bar: the visible sub-tab body.
const bodyOf = html => { const i = html.indexOf('</div>', html.indexOf('class="projects-subtabs"')); return html.slice(i); };
const selected = html => [...html.matchAll(/<button[^>]*data-projects-subtab="([a-z_]+)"[^>]*>/g)]
  .filter(m => /aria-selected="true"/.test(m[0])).map(m => m[1]);
const activeCls = html => [...html.matchAll(/<button[^>]*data-projects-subtab="([a-z_]+)"[^>]*>/g)]
  .filter(m => /class="projects-subtab active"/.test(m[0])).map(m => m[1]);

async function open(opts) {
  const l = load(opts);
  await l.sb.renderProjectsView();
  await flushAsync(30);
  return l;
}

test('clicking each Overview sub-tab shows its body and makes it the only selected button', async () => {
  const { sb, out } = await open();
  const all = ids(out.view);
  assert.ok(all.length >= 2, 'the Overview bar has several sub-tab buttons');
  const seen = new Set();
  for (const id of all) {
    sb.selectProjectsOverviewSubtab(id);
    await flushAsync(30);
    assert.deepEqual(ids(out.view), all, 'the bar keeps every button');
    assert.deepEqual(selected(out.view), [id], `only ${id} is aria-selected`);
    assert.deepEqual(activeCls(out.view), [id], `only ${id} has the active class`);
    assert.equal(run(sb, '_automationState.subtab'), id);
    seen.add(bodyOf(out.view));
  }
  assert.equal(seen.size, all.length, 'each sub-tab draws a different body');
});

test('the Overview bar uses the Projects sub-tab classes, not the Automation ones, and the Automation bar keeps its own', async () => {
  const { sb, out } = await open();
  assert.match(out.view, /class="projects-subtabs"/);
  assert.match(out.view, /class="projects-subtab( active)?"/);
  assert.doesNotMatch(out.view, /automation-subtabs/, 'no automation-subtabs class on the Projects Overview');
  assert.doesNotMatch(out.view, /class="output-tab/, 'no output-tab buttons on the Projects Overview');
  assert.doesNotMatch(out.view, /data-subtab=/, 'buttons carry data-projects-subtab only');
  assert.match(out.view, /onclick="selectProjectsOverviewSubtab\('/);

  const auto = run(sb, '_automationSubtabsBarHTML()');
  assert.match(auto, /class="output-tabs automation-subtabs"/);
  assert.match(auto, /class="output-tab/);
  assert.match(auto, /data-subtab="/);
  assert.doesNotMatch(auto, /projects-subtab/);
  assert.match(auto, /onclick="switchAutomationSubtab\('/);
  // Rendering the Projects style bar does not change the Automation bar.
  run(sb, '_automationSubtabsBarHTML({ projectsStyle: true })');
  assert.equal(run(sb, '_automationSubtabsBarHTML()'), auto);
});

test('the selected Overview sub-tab survives a repaint with the stored value and with a #projects hash', async () => {
  const first = await open();
  const all = ids(first.out.view);
  const pick = all[all.length - 1];
  first.sb.selectProjectsOverviewSubtab(pick);
  assert.equal(first.storage.getItem(KEY), pick, 'the choice is stored');

  // Repaint in place.
  await first.sb.renderProjectsView();
  await flushAsync(30);
  assert.deepEqual(selected(first.out.view), [pick]);

  // Fresh app, same storage, no address: restores the stored choice.
  const fresh = await open({ storage: makeStorage({ [KEY]: pick }) });
  assert.deepEqual(selected(fresh.out.view), [pick], 'stored value restored on load');

  // Fresh app, same storage, plain #projects address.
  const hashed = await open({ hash: '#projects', storage: makeStorage({ [KEY]: pick }) });
  assert.deepEqual(selected(hashed.out.view), [pick], 'a #projects hash keeps the stored sub-tab');
  assert.deepEqual(activeCls(hashed.out.view), [pick]);

  // An unknown stored value falls back to the default sub-tab.
  const bad = await open({ storage: makeStorage({ [KEY]: 'nope' }) });
  assert.equal(selected(bad.out.view).length, 1);
});

test('phone width: the bar has no wrap-forcing markup and the stylesheet scrolls it sideways without wrapping', async () => {
  const { out, sb } = await open({ phone: true });
  assert.equal(sb.innerWidth, 375);
  assert.ok(out.view.includes('class="projects-subtabs"'), 'the Overview bar carries the class the scrolling rule targets');
  const bar = out.view.slice(out.view.indexOf('class="projects-subtabs"'));
  const barHtml = bar.slice(0, bar.indexOf('</div>'));
  assert.doesNotMatch(barHtml, /flex-wrap|white-space\s*:\s*normal|<br|display\s*:\s*block/i, 'no wrap-forcing inline structure');
  assert.doesNotMatch(barHtml, /<div|<ul|<table/i, 'buttons sit directly in the bar, no nested rows');

  const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');
  const m = css.match(/(^|\n)\.projects-subtabs\s*\{([^}]*)\}/);
  assert.ok(m, 'a .projects-subtabs rule exists');
  assert.match(m[2], /overflow-x\s*:\s*auto/);
  assert.match(m[2], /white-space\s*:\s*nowrap/);
  assert.doesNotMatch(m[2], /flex-wrap\s*:\s*wrap/);
  const b = css.match(/\.projects-subtabs button\s*\{([^}]*)\}/);
  assert.ok(b && /flex-shrink\s*:\s*0/.test(b[1]), 'buttons do not shrink');
});
