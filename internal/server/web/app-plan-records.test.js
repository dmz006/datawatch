// Plan records in the PWA — the Records section of the Projects tab project
// page, the derived project label on Automaton and Automation cards and
// details, the project filter chip, and the three autonomous.plan_records
// settings rows.
//
// The Records section lists the registry rows and findings of the selected
// project (GET /api/plan-records/registry-rows and /findings), title first and
// id second, with a state chip, dates and the linked Automaton. It has a state
// filter, a search box that calls GET /api/plan-records/search, a detail panel
// with an edit form that shows the server's 409 message in place, a create
// form, and an Import button that shows the dry-run preview (counts and every
// conflict with its resolution) and writes only after the operator confirms.
// While the feature is off the server answers not found and the section draws
// nothing. Every record value is escaped.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-plan-records.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const HOSTILE = '<img src=x onerror=alert(1)>';
const ENTITY_IMG = '&lt;img src=x onerror=alert(1)&gt;';

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
}

const ROW = {
  title: 'Add the records view',
  id: 'records-view',
  uid: '11111111-2222-3333-4444-555555555555',
  kind: 'registry_row',
  project: 'acme',
  state: 'in_progress',
  body: 'Show the rows on the project page.',
  legacy_number: 412,
  legacy_source: 'docs/plans/README.md row 412',
  shipped_by: 'auto-77',
  links: [],
  aliases: ['Records view'],
  needs_review: false,
  created_at: '2026-11-01T10:00:00Z',
  updated_at: '2026-11-10T12:30:00Z',
};
const FINDING = {
  title: 'Search misses aliases',
  id: 'search-misses-aliases',
  uid: '99999999-2222-3333-4444-555555555555',
  kind: 'finding',
  project: 'acme',
  state: 'open',
  body: 'Typing an old title finds nothing.',
  raised_by: 'auto-12',
  links: [],
  aliases: [],
  needs_review: true,
  review_reason: 'imported with a suffix',
  created_at: '2026-11-02T10:00:00Z',
  updated_at: '2026-11-03T10:00:00Z',
};

// load builds the app with the Projects tab active. handler(url, opts) answers
// each plan-records request: it returns a body, or throws {status, text} for a
// failure. Everything else answers {}.
function load(handler, projects = [{ name: 'acme', dir: '/work/acme', arcs: [] }]) {
  const slot = { innerHTML: '' };
  const body = { innerHTML: '' };
  const view = makeStubElement(() => {});
  view.insertAdjacentHTML = () => {};
  const fields = {};
  const sandbox = buildSandbox({ byId: { automataPanel: view, view, planRecordsSlot: slot, planRecordsBody: body } });
  const realGet = sandbox.document.getElementById.bind(sandbox.document);
  sandbox.document.getElementById = id => (fields[id] || realGet(id));
  sandbox.__calls = [];
  sandbox.fetch = (url, opts) => {
    const u = String(url);
    const o = opts || {};
    const call = { url: u, method: o.method || 'GET', body: o.body ? JSON.parse(o.body) : null };
    const ok = b => Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(b), text: () => Promise.resolve(JSON.stringify(b)) });
    if (u.startsWith('/api/plan-records')) {
      sandbox.__calls.push(call);
      try {
        const r = handler ? handler(u, call) : { records: [], total: 0 };
        return ok(r);
      } catch (e) {
        const text = typeof e.text === 'string' ? e.text : JSON.stringify({ error: String(e.message || e) });
        return Promise.resolve({ ok: false, status: e.status || 500, statusText: 'err', json: () => Promise.resolve({}), text: () => Promise.resolve(text) });
      }
    }
    if (u.includes('/api/projects') && !u.includes('/policy/') && !u.includes('/rollup') && !u.includes('/history')) return ok(projects);
    if (u.includes('/history')) return ok({ items: [], gantt: [] });
    return ok({});
  };
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  vm.runInContext("state.activeView = 'autonomous'; _automataState.tab = 'projects'", sandbox);
  return { sb: sandbox, slot, body, fields };
}

async function selectAndLoad(sb, name) {
  await sb._projectsLoad();
  vm.runInContext(`_projectsState.selected = ${JSON.stringify(name)}`, sb);
  await sb._projectsLoadSelected();
  await flushAsync();
}

const calls = sb => Array.from(sb.__calls);
const pr = (sb, expr) => vm.runInContext(expr, sb);

function listHandler(u) {
  if (u.startsWith('/api/plan-records/registry-rows')) return { records: [ROW], total: 1, limit: 50, offset: 0 };
  if (u.startsWith('/api/plan-records/findings')) return { records: [FINDING], total: 1, limit: 50, offset: 0 };
  if (u.startsWith('/api/plan-records/search')) return { hits: [{ record: ROW, score: 90, matched: 'title' }], total: 1, limit: 50, offset: 0 };
  return {};
}

// ── list rendering ───────────────────────────────────────────────────────────

test('a record row shows the title first and the id second, with state, dates and the linked Automaton', () => {
  const { sb } = load();
  const html = sb.planRecordRowHtml(ROW);
  const iTitle = html.indexOf('>Add the records view<');
  const iId = html.indexOf('>records-view<');
  assert.ok(iTitle >= 0 && iId > iTitle, 'title appears before the id');
  assert.ok(html.includes('In progress'), 'state chip carries the localized state');
  assert.ok(html.includes('Updated'), 'update date shown');
  assert.ok(html.includes('Shipped by'), 'shipping Automaton labelled');
  assert.ok(html.includes('auto-77'), 'linked Automaton id shown');
  assert.ok(html.includes('row 412'), 'old row number shown so existing references resolve');
});

test('a finding row shows the raising Automaton and the needs-review flag', () => {
  const { sb } = load();
  const html = sb.planRecordRowHtml(FINDING);
  assert.ok(html.includes('Raised by') && html.includes('auto-12'));
  assert.ok(html.includes('Needs review'));
});

test('the section says what it is for, offers both views, the filters and the actions', async () => {
  const { sb } = load(listHandler);
  await selectAndLoad(sb, 'acme');
  const html = sb.planRecordsSectionHtml(pr(sb, '_planRecords'));
  assert.ok(html.includes('Plan records'), 'card title');
  assert.ok(html.includes('The registry rows and findings datawatch keeps for this project'), 'purpose line');
  assert.ok(html.includes('Registry rows') && html.includes('Findings'), 'two views');
  assert.ok(html.includes('planRecordsStateFilter'), 'state filter');
  assert.ok(html.includes('planRecordsSearch'), 'search box');
  assert.ok(html.includes('planRecordsNew()') && html.includes('planRecordsImportOpen()'), 'create and import actions');
  assert.ok(html.includes('This project has 1 registry rows; 1 shown.'), 'state of the list');
  assert.ok(html.includes('Add the records view'));
});

test('an empty view says so and what the operator can do next', async () => {
  const { sb } = load(() => ({ records: [], total: 0 }));
  await selectAndLoad(sb, 'acme');
  let html = sb.planRecordsSectionHtml(pr(sb, '_planRecords'));
  assert.ok(html.includes('No registry rows in this project yet. Import the plans archive, or create one.'));
  await sb.planRecordsSetKind('finding');
  html = sb.planRecordsSectionHtml(pr(sb, '_planRecords'));
  assert.ok(html.includes('No findings in this project yet. Import the findings files, or create one.'));
});

test('the project page carries a slot for the section', async () => {
  const { sb } = load(listHandler);
  await selectAndLoad(sb, 'acme');
  const page = sb._projectPageHTML({ name: 'acme' }, {}, '', null);
  assert.ok(page.includes('id="planRecordsSlot"'));
  assert.ok(page.includes('class="plan-records-card"'), 'section drawn once loaded');
});

// ── filters and search ───────────────────────────────────────────────────────

test('the loader reads the registry-row list for the selected project', async () => {
  const { sb } = load(listHandler);
  await selectAndLoad(sb, 'acme');
  const c = calls(sb).find(x => x.url.startsWith('/api/plan-records/registry-rows'));
  assert.ok(c, 'registry rows read');
  assert.ok(c.url.includes('project=acme'), 'scoped to the project');
  assert.ok(c.url.includes('limit=50'));
});

test('switching to Findings reads the findings route and clears the filters', async () => {
  const { sb } = load(listHandler);
  await selectAndLoad(sb, 'acme');
  await sb.planRecordsSetState('open');
  await sb.planRecordsSetKind('finding');
  const last = calls(sb).filter(x => x.url.startsWith('/api/plan-records/findings')).pop();
  assert.ok(last, 'findings read');
  assert.ok(!last.url.includes('state='), 'state filter reset on a kind switch');
  assert.equal(pr(sb, '_planRecords.kind'), 'finding');
});

test('the state filter is sent as ?state=', async () => {
  const { sb } = load(listHandler);
  await selectAndLoad(sb, 'acme');
  await sb.planRecordsSetState('blocked');
  const last = calls(sb).filter(x => x.url.startsWith('/api/plan-records/registry-rows')).pop();
  assert.ok(last.url.includes('state=blocked'));
});

test('the search box calls the search route with the text, the kind and the project', async () => {
  const { sb } = load(listHandler);
  await selectAndLoad(sb, 'acme');
  pr(sb, "_planRecords.q = 'records view'");
  await sb._planRecordsLoad(false);
  const c = calls(sb).filter(x => x.url.startsWith('/api/plan-records/search')).pop();
  assert.ok(c, 'search route called');
  assert.ok(c.url.includes('q=records%20view'));
  assert.ok(c.url.includes('kind=registry_row'));
  assert.ok(c.url.includes('project=acme'));
  assert.equal(pr(sb, '_planRecords.recs.length'), 1, 'the hit record is listed');
});

test('typing in the search box debounces on a window-level timer, not a DOM node', () => {
  const { sb } = load(listHandler);
  sb.planRecordsSearchInput('abc');
  assert.equal(pr(sb, '_planRecords.q'), 'abc');
  assert.ok(pr(sb, 'window._planRecordsSearchTimer') !== undefined, 'timer kept on window');
  vm.runInContext('clearTimeout(window._planRecordsSearchTimer)', sb);
});

test('a search with no hits says so', async () => {
  const { sb } = load(u => (u.startsWith('/api/plan-records/search') ? { hits: [], total: 0 } : { records: [], total: 0 }));
  await selectAndLoad(sb, 'acme');
  pr(sb, "_planRecords.q = 'nothing'");
  await sb._planRecordsLoad(false);
  const html = sb.planRecordsSectionHtml(pr(sb, '_planRecords'));
  assert.ok(html.includes('No record matches that search.'));
});

// ── edit form and the server's conflict messages ─────────────────────────────

async function openRow(sb, fields) {
  await selectAndLoad(sb, 'acme');
  sb.planRecordsOpen(ROW.uid);
  fields.planRecordTitle = { value: ROW.title };
  fields.planRecordState = { value: ROW.state };
  fields.planRecordBody = { value: ROW.body };
  fields.planRecordLinked = { value: ROW.shipped_by };
}

test('the detail panel carries the edit form with title, state, notes, project and the shipping Automaton', async () => {
  const { sb, fields } = load(listHandler);
  await openRow(sb, fields);
  const html = sb.planRecordDetailHtml(pr(sb, '_planRecords'));
  assert.ok(html.includes('id="planRecordTitle"') && html.includes('id="planRecordState"') && html.includes('id="planRecordBody"'));
  assert.ok(html.includes('id="planRecordLinked"') && html.includes('Shipped by (Automaton id)'));
  assert.ok(html.includes('id="planRecordProject"') && html.includes('readonly'), 'the project is shown but fixed');
  assert.ok(html.includes('Next: change the state'), 'says what the operator can do next');
  assert.ok(html.includes('Records view'), 'older titles listed');
});

test('a refused save shows the server message in place and keeps what was typed', async () => {
  const msg = 'the title "Taken title" is already used in project acme by "Other" (other-id)';
  const { sb, fields } = load((u, c) => {
    if (c.method === 'PUT') throw { status: 409, text: JSON.stringify({ error: msg }) };
    return listHandler(u);
  });
  await openRow(sb, fields);
  fields.planRecordTitle = { value: 'Taken title' };
  await sb.planRecordsSave();
  const put = calls(sb).find(x => x.method === 'PUT');
  assert.ok(put.url.endsWith('/api/plan-records/registry-rows/' + encodeURIComponent(ROW.uid)), 'saved by uid');
  assert.deepEqual(put.body, { title: 'Taken title' }, 'only the changed field is sent');
  const html = sb.planRecordDetailHtml(pr(sb, '_planRecords'));
  assert.ok(html.includes('id="planRecordFormError"'), 'error area drawn in the form');
  assert.ok(html.includes('is already used in project acme'), 'server message shown');
  assert.ok(html.includes('value="Taken title"'), 'the typed title is kept');
});

test('an ambiguous reference lists the candidates to pick from', async () => {
  const { sb, fields } = load((u, c) => {
    if (c.method === 'PUT') {
      throw { status: 409, text: JSON.stringify({ error: '"Add" matches more than one record; say which one (use its id)', candidates: [{ title: 'Add one', id: 'add-one' }, { title: 'Add two', id: 'add-two' }] }) };
    }
    return listHandler(u);
  });
  await openRow(sb, fields);
  fields.planRecordBody = { value: 'changed' };
  await sb.planRecordsSave();
  const html = sb.planRecordDetailHtml(pr(sb, '_planRecords'));
  assert.ok(html.includes('matches more than one record'));
  assert.ok(html.includes('Pick one by its id:'));
  assert.ok(html.includes('Add one') && html.includes('add-two'));
});

test('a good save updates the record in the list and says Saved', async () => {
  const updated = Object.assign({}, ROW, { state: 'shipped', updated_at: '2026-11-11T09:00:00Z' });
  const { sb, fields } = load((u, c) => (c.method === 'PUT' ? { record: updated } : listHandler(u)));
  await openRow(sb, fields);
  fields.planRecordState = { value: 'shipped' };
  await sb.planRecordsSave();
  assert.equal(pr(sb, '_planRecords.selected.state'), 'shipped');
  assert.equal(pr(sb, '_planRecords.recs[0].state'), 'shipped');
  assert.ok(sb.planRecordDetailHtml(pr(sb, '_planRecords')).includes('Saved.'));
});

test('a save with nothing changed sends nothing', async () => {
  const { sb, fields } = load(listHandler);
  await openRow(sb, fields);
  await sb.planRecordsSave();
  assert.ok(!calls(sb).some(x => x.method === 'PUT'));
  assert.ok(sb.planRecordDetailHtml(pr(sb, '_planRecords')).includes('Nothing changed.'));
});

test('an empty title is refused before anything is sent', async () => {
  const { sb, fields } = load(listHandler);
  await openRow(sb, fields);
  fields.planRecordTitle = { value: '   ' };
  await sb.planRecordsSave();
  assert.ok(!calls(sb).some(x => x.method === 'PUT'));
  assert.ok(sb.planRecordDetailHtml(pr(sb, '_planRecords')).includes('A title is required.'));
});

// ── create form ──────────────────────────────────────────────────────────────

test('the create form posts to the kind route with the project and shows a title conflict in place', async () => {
  const msg = 'the title "Dup" is already used in project acme by "Dup" (dup)';
  let first = true;
  const { sb, fields } = load((u, c) => {
    if (c.method === 'POST') {
      if (first) { first = false; throw { status: 409, text: JSON.stringify({ error: msg }) }; }
      return { record: Object.assign({}, ROW, { title: 'Fresh' }) };
    }
    return listHandler(u);
  });
  await selectAndLoad(sb, 'acme');
  sb.planRecordsNew();
  assert.ok(sb.planRecordCreateHtml(pr(sb, '_planRecords')).includes('New registry row'));
  fields.planRecordTitle = { value: 'Dup' };
  fields.planRecordState = { value: 'open' };
  fields.planRecordBody = { value: 'text' };
  fields.planRecordLinked = { value: 'auto-1' };
  await sb.planRecordsCreate();
  const post = calls(sb).find(x => x.method === 'POST');
  assert.equal(post.url, '/api/plan-records/registry-rows');
  assert.deepEqual(post.body, { project: 'acme', title: 'Dup', state: 'open', body: 'text', shipped_by: 'auto-1' });
  assert.ok(sb.planRecordCreateHtml(pr(sb, '_planRecords')).includes('is already used in project acme'));
  assert.ok(sb.planRecordCreateHtml(pr(sb, '_planRecords')).includes('value="Dup"'), 'typed title kept');
  fields.planRecordTitle = { value: 'Fresh' };
  await sb.planRecordsCreate();
  assert.equal(pr(sb, '_planRecords.view'), 'list', 'back to the list after a good create');
});

test('a new finding is posted to the findings route with raised_by', async () => {
  const { sb, fields } = load((u, c) => (c.method === 'POST' ? { record: FINDING } : listHandler(u)));
  await selectAndLoad(sb, 'acme');
  await sb.planRecordsSetKind('finding');
  sb.planRecordsNew();
  fields.planRecordTitle = { value: 'New finding' };
  fields.planRecordState = { value: 'open' };
  fields.planRecordBody = { value: '' };
  fields.planRecordLinked = { value: 'auto-9' };
  await sb.planRecordsCreate();
  const post = calls(sb).find(x => x.method === 'POST');
  assert.equal(post.url, '/api/plan-records/findings');
  assert.equal(post.body.raised_by, 'auto-9');
});

// ── import: preview first, write only after the operator confirms ────────────

const PREVIEW = {
  note: 'Preview only: nothing was written.',
  result: {
    project: 'acme', dry_run: true, policy: 'suffix',
    counts: { create: 40, unchanged: 3, changed: 1, conflicts: 2, skipped: 0 },
    conflicts: [
      { source: 'docs/plans/README.md row 12', title: 'Add the records view', reason: 'title', with: 'records-view', action: 'suffix', new_title: 'Add the records view (row 12)' },
      { source: 'docs/plans/README.md row 30', title: 'Other', reason: 'duplicate_row_number', with: 'row 30', action: 'skip', note: 'kept the first' },
    ],
    unparseable: [{ source: 'docs/plans/README.md row 99', reason: 'no title' }],
  },
};

test('Import shows the dry-run preview with counts and every conflict, and writes nothing until Apply', async () => {
  const { sb } = load((u, c) => {
    if (u.startsWith('/api/plan-records/import')) return u.includes('dry_run=true') ? PREVIEW : { note: 'Import finished.', result: Object.assign({}, PREVIEW.result, { dry_run: false }) };
    return listHandler(u);
  });
  await selectAndLoad(sb, 'acme');
  sb.planRecordsImportOpen();
  assert.ok(sb.planRecordImportHtml(pr(sb, '_planRecords')).includes('Preview import'));
  assert.ok(!sb.planRecordImportHtml(pr(sb, '_planRecords')).includes('Apply import'), 'no Apply before a preview');

  await sb.planRecordsImportPreview();
  const imports = () => calls(sb).filter(x => x.url.startsWith('/api/plan-records/import'));
  assert.equal(imports().length, 1, 'one request so far');
  assert.ok(imports()[0].url.includes('dry_run=true'), 'the first request is the dry run');
  assert.deepEqual(imports()[0].body, { project: 'acme' }, 'no policy sent: the configured one applies');

  const html = sb.planRecordImportHtml(pr(sb, '_planRecords'));
  assert.ok(html.includes('Preview only. Nothing has been written.'));
  assert.ok(html.includes('40 to create, 3 unchanged, 1 changed, 2 conflicts, 0 skipped'), 'counts');
  assert.ok(html.includes('Conflict policy: suffix'), 'policy named');
  assert.ok(html.includes('Add the records view (row 12)'), 'resolution of the first conflict');
  assert.ok(html.includes('suffix added') && html.includes('skipped'), 'each action named');
  assert.ok(html.includes('title already used') && html.includes('row number already used'), 'each reason named');
  assert.ok(html.includes('docs/plans/README.md row 99'), 'unreadable items listed');
  assert.ok(html.includes('Apply import'), 'Apply offered with the preview');

  await sb.planRecordsImportApply();
  assert.equal(imports().length, 2, 'Apply is the second request');
  assert.ok(!imports()[1].url.includes('dry_run'), 'the second request is a real run');
  assert.deepEqual(imports()[1].body, { project: 'acme' });
  const done = sb.planRecordImportHtml(pr(sb, '_planRecords'));
  assert.ok(done.includes('Import finished: 40 created, 3 unchanged, 2 conflicts resolved, 0 skipped.'));
});

test('Apply does nothing without a preview', async () => {
  const { sb } = load(listHandler);
  await selectAndLoad(sb, 'acme');
  sb.planRecordsImportOpen();
  await sb.planRecordsImportApply();
  assert.ok(!calls(sb).some(x => x.url.startsWith('/api/plan-records/import')));
});

test('a failed preview shows the error and offers a retry', async () => {
  const { sb } = load((u) => {
    if (u.startsWith('/api/plan-records/import')) throw { status: 400, text: JSON.stringify({ error: 'unknown project alias "acme"' }) };
    return listHandler(u);
  });
  await selectAndLoad(sb, 'acme');
  sb.planRecordsImportOpen();
  await sb.planRecordsImportPreview();
  const html = sb.planRecordImportHtml(pr(sb, '_planRecords'));
  assert.ok(html.includes('The import failed: unknown project alias'));
  assert.ok(html.includes('Preview import'));
  assert.ok(!html.includes('Apply import'));
});

// ── disabled state ───────────────────────────────────────────────────────────

test('while the feature is off the section draws nothing', async () => {
  const { sb } = load(() => { throw { status: 404, text: 'plan records are turned off (autonomous.plan_records.enabled)' }; });
  await selectAndLoad(sb, 'acme');
  assert.equal(pr(sb, '_planRecords.status'), 'disabled');
  assert.equal(sb.planRecordsSectionHtml(pr(sb, '_planRecords')), '');
  const page = sb._projectPageHTML({ name: 'acme' }, {}, '', null);
  assert.ok(page.includes('id="planRecordsSlot"></div>'), 'the slot stays empty');
  assert.ok(!page.includes('Plan records'));
});

test('a store that is not wired also draws nothing', async () => {
  const { sb } = load(() => { throw { status: 503, text: JSON.stringify({ error: 'plan records are not available' }) }; });
  await selectAndLoad(sb, 'acme');
  assert.equal(sb.planRecordsSectionHtml(pr(sb, '_planRecords')), '');
});

test('before the first answer, and for an unknown project, nothing is drawn', () => {
  const { sb } = load(listHandler);
  assert.equal(sb.planRecordsSectionHtml(null), '');
  assert.equal(sb.planRecordsSectionHtml(pr(sb, '_planRecords')), '');
});

test('a real failure shows the error line, not an empty list', async () => {
  const { sb } = load(() => { throw { status: 500, text: JSON.stringify({ error: 'disk full' }) }; });
  await selectAndLoad(sb, 'acme');
  const html = sb.planRecordsSectionHtml(pr(sb, '_planRecords'));
  assert.ok(html.includes('Could not load the plan records: disk full'));
});

// ── escaping ─────────────────────────────────────────────────────────────────

test('every record value is escaped, so hostile text stays inert', async () => {
  const bad = Object.assign({}, ROW, {
    title: HOSTILE, id: HOSTILE, body: HOSTILE, shipped_by: HOSTILE, legacy_source: HOSTILE,
    aliases: [HOSTILE], review_reason: HOSTILE, needs_review: true,
    links: [{ kind: HOSTILE, ref: HOSTILE }],
  });
  const { sb } = load();
  const row = sb.planRecordRowHtml(bad);
  assert.ok(!row.includes('<img'), 'row has no raw tag');
  assert.ok(row.includes(ENTITY_IMG));
  pr(sb, `_planRecords = Object.assign(_planRecordsFresh('acme'), { status: 'ready', selected: ${JSON.stringify(bad)}, view: 'detail', formError: ${JSON.stringify(HOSTILE)}, candidates: [{ title: ${JSON.stringify(HOSTILE)}, id: ${JSON.stringify(HOSTILE)} }] })`);
  const detail = sb.planRecordDetailHtml(pr(sb, '_planRecords'));
  assert.ok(!detail.includes('<img'), 'detail has no raw tag');
  assert.ok(detail.includes(ENTITY_IMG));
  const section = sb.planRecordsSectionHtml(pr(sb, '_planRecords'));
  assert.ok(!section.includes('<img'));
});

test('the import preview escapes every conflict value', () => {
  const { sb } = load();
  pr(sb, `_planRecords = Object.assign(_planRecordsFresh('acme'), { status: 'ready', view: 'import', importPhase: 'preview', preview: { result: { policy: ${JSON.stringify(HOSTILE)}, counts: {}, conflicts: [{ source: ${JSON.stringify(HOSTILE)}, title: ${JSON.stringify(HOSTILE)}, reason: ${JSON.stringify(HOSTILE)}, with: ${JSON.stringify(HOSTILE)}, action: ${JSON.stringify(HOSTILE)}, new_title: ${JSON.stringify(HOSTILE)}, note: ${JSON.stringify(HOSTILE)} }], unparseable: [{ source: ${JSON.stringify(HOSTILE)}, reason: ${JSON.stringify(HOSTILE)} }] } } })`);
  const html = sb.planRecordImportHtml(pr(sb, '_planRecords'));
  assert.ok(!html.includes('<img'), 'no raw tag in the preview');
  assert.ok(html.includes(ENTITY_IMG));
});

test('a hostile project name and a failed load message stay inert', async () => {
  const { sb } = load(() => { throw { status: 500, text: JSON.stringify({ error: HOSTILE }) }; }, [{ name: 'acme', dir: '/x', arcs: [] }]);
  await selectAndLoad(sb, 'acme');
  assert.ok(!sb.planRecordsSectionHtml(pr(sb, '_planRecords')).includes('<img'));
});

// ── project label and project filter chip ────────────────────────────────────

test('a card shows the derived project as a chip and shows nothing when there is none', () => {
  const { sb } = load();
  assert.ok(sb.planProjectChipHtml({ project: 'acme' }).includes('acme'));
  assert.equal(sb.planProjectChipHtml({}), '');
  assert.equal(sb.planProjectChipHtml({ project: '  ' }), '');
  assert.ok(!sb.planProjectChipHtml({ project: HOSTILE }).includes('<img'));
  const prd = { id: 'p1', title: 'Build it', status: 'draft', project: 'acme', stories: [] };
  assert.ok(sb.renderAutomataCard(prd, '').includes('plan-project-chip'), 'Automaton card carries the chip');
  const auto = { id: 'a1', name: 'Pipeline', status: 'draft', project: 'acme', members: [], version: 1 };
  assert.ok(sb.buildAutomationCardHTML(auto, { prdTitles: {}, now: Date.now() }).includes('plan-project-chip'), 'Automation card carries the chip');
  const none = { id: 'a2', name: 'Loose', status: 'draft', members: [], version: 1 };
  assert.ok(!sb.buildAutomationCardHTML(none, { prdTitles: {}, now: Date.now() }).includes('plan-project-chip'));
});

test('a detail panel shows the project, or the (none) wording when there is none', () => {
  const { sb } = load();
  assert.ok(sb.planProjectValueHtml({ project: 'acme' }).includes('acme'));
  assert.ok(sb.planProjectValueHtml({}).includes('(none)'));
  const auto = { id: 'a1', name: 'Pipeline', status: 'draft', project: 'acme', members: [], version: 1 };
  const detail = sb.buildAutomationDetailHTML(auto, {});
  assert.ok(detail.includes('automation-detail-project') && detail.includes('acme'));
  const loose = sb.buildAutomationDetailHTML({ id: 'a2', name: 'Loose', status: 'draft', members: [], version: 1 }, {});
  assert.ok(loose.includes('(none)'));
});

test('the project filter narrows a list by the derived project', () => {
  const { sb } = load();
  const items = [{ id: 'a', project: 'acme' }, { id: 'b', project: 'beta' }, { id: 'c' }];
  const tree = pr(sb, '_tagBrowseTree([])');
  const only = vm.runInContext('(items, tree) => _tagBrowseFilter(items, { projects: ["acme"] }, tree).map(x => x.id)', sb)(items, tree);
  assert.deepEqual(Array.from(only), ['a']);
  const both = vm.runInContext('(items, tree) => _tagBrowseFilter(items, { projects: ["acme", "beta"] }, tree).map(x => x.id)', sb)(items, tree);
  assert.deepEqual(Array.from(both), ['a', 'b']);
  const all = vm.runInContext('(items, tree) => _tagBrowseFilter(items, {}, tree).map(x => x.id)', sb)(items, tree);
  assert.equal(all.length, 3);
});

test('the project chips use the existing chip pattern and toggle the filter', () => {
  const { sb } = load();
  pr(sb, "_automationState.list = [{ id: 'a', name: 'A', status: 'draft', project: 'acme' }, { id: 'b', name: 'B', status: 'draft', project: 'beta' }]");
  let html = pr(sb, "_tagFacetsHTML('automation', false)");
  assert.ok(html.includes('project-chip') && html.includes('automata-filter-badge'), 'chip pattern reused');
  assert.ok(html.includes("tagBrowseToggleProject('automation','acme')"));
  assert.ok(html.includes('aria-pressed="false"'));
  sb.tagBrowseToggleProject('automation', 'acme');
  assert.equal(pr(sb, "_tagBrowse.automation.projects.has('acme')"), true);
  html = pr(sb, "_tagFacetsHTML('automation', false)");
  assert.ok(html.includes('project-chip active'), 'chip active after a toggle');
  assert.equal(pr(sb, "_tagBrowseActiveCount('automation')"), 1, 'counted as an active filter');
  sb.tagBrowseClear('automation');
  assert.equal(pr(sb, "_tagBrowse.automation.projects.size"), 0, 'Clear drops the project chips');
});

test('no project chips are drawn while no listed item has a project', () => {
  const { sb } = load();
  pr(sb, "_automationState.list = [{ id: 'a', name: 'A', status: 'draft' }]");
  assert.ok(!pr(sb, "_tagFacetsHTML('automation', false)").includes('project-chip'));
});

// ── settings rows ────────────────────────────────────────────────────────────

function settingsField(sb, key) {
  const json = vm.runInContext(`JSON.stringify(GENERAL_CONFIG_FIELDS.map(s => s.fields || []).reduce((a, b) => a.concat(b), []).find(f => f.key === ${JSON.stringify(key)}) || null)`, sb);
  return JSON.parse(json);
}

test('the Automata settings carry the three plan record rows, validated like the server', () => {
  const { sb } = load();
  const en = settingsField(sb, 'autonomous.plan_records.enabled');
  const wt = settingsField(sb, 'autonomous.plan_records.write_through');
  const pol = settingsField(sb, 'autonomous.plan_records.import_conflict_policy');
  assert.equal(en && en.type, 'toggle');
  assert.equal(wt && wt.type, 'toggle');
  assert.equal(pol && pol.type, 'select');
  assert.deepEqual(pol.options, ['suffix', 'rename', 'skip'], 'the same three values the server accepts');
  assert.equal(pol.default, 'suffix');
  for (const f of [en, wt, pol]) {
    assert.ok(f.helpKey, f.key + ' has help text');
    assert.ok(f.docsKey, f.key + ' has a how-to link');
  }
});

test('the settings rows link to the plan records how-to through the howto-link map', () => {
  const { sb } = load();
  for (const k of ['settings_automata_plan_records_enabled', 'settings_automata_plan_records_write_through', 'settings_automata_plan_records_import_conflict_policy']) {
    assert.equal(pr(sb, `DOCS_LINKS_BY_KEY[${JSON.stringify(k)}]`), 'howto/plan-records.md');
  }
});

// ── locales ──────────────────────────────────────────────────────────────────

test('every plan_records_ key app.js uses exists in all five bundles, with no tracker ids', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const used = new Set();
  for (const m of src.matchAll(/\bt\('((?:plan_records_|settings_automata_plan_records_)[a-z_]+)'/g)) used.add(m[1]);
  for (const k of ['plan_records_state_open', 'plan_records_import_reason_title', 'plan_records_import_action_suffix']) used.add(k);
  assert.ok(used.size > 40, 'the section uses many strings: ' + used.size);
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of used) assert.ok(typeof b[k] === 'string' && b[k] !== '', `${lang} is missing ${k}`);
    for (const [k, v] of Object.entries(b)) {
      if (!/^(plan_records_|settings_automata_plan_records_)/.test(k)) continue;
      assert.ok(!/\b(BL|GH|B)\d{2,}\b/.test(v), `${lang} ${k} carries a tracker id`);
      assert.ok(!/\bPRD\b/.test(v), `${lang} ${k} says PRD instead of Automaton`);
    }
  }
});

test('every state and import word the code builds a key from has a string', () => {
  const en = bundle('en');
  for (const s of ['open', 'planned', 'in_progress', 'blocked', 'shipped', 'done', 'deferred', 'cancelled', 'triaged', 'fixed', 'wontfix', 'duplicate']) {
    assert.ok(en['plan_records_state_' + s], 'state ' + s);
  }
  for (const a of ['suffix', 'rename', 'skip']) assert.ok(en['plan_records_import_action_' + a], 'action ' + a);
  for (const r of ['title', 'duplicate_row_number']) assert.ok(en['plan_records_import_reason_' + r], 'reason ' + r);
});

test('the Records code starts no interval and stores no timer on a DOM node', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const a = src.indexOf('// ── Plan records on the project page');
  const b = src.indexOf('function _projectsLoadSelected()');
  assert.ok(a > 0 && b > a);
  const block = src.slice(a, b);
  assert.ok(!/setInterval/.test(block), 'no setInterval');
  assert.ok(/window\._planRecordsSearchTimer/.test(block), 'the one timer is on window');
});
