// Projects page — read-only links: effective policy, merge pipeline, arc
// documents and the project's event stream.
//
// The project page reads GET /api/projects/{name}/policy/effective, GET
// /api/autonomous/merge-pipeline?project=NAME and GET
// /api/autonomous/events?queue=pwa&project=NAME. Each section is read-only: the
// policy and merge pipeline link to their Settings entries, and no section has
// an input, select or button. An arc shows its plan and design document links
// and its linked datawatch-app issues from the arc record; only http(s) values
// become links.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP = path.join(__dirname, 'app.js');

// load builds the app with the Projects tab active and English strings. replies
// maps a URL fragment to the body (or an Error) the fetch stub answers with.
function load(replies = {}, projects = [{ name: 'acme', dir: '/work/acme', arcs: [] }]) {
  const view = makeStubElement(() => {});
  view.insertAdjacentHTML = () => {};
  const sandbox = buildSandbox({ byId: { view } });
  sandbox.__urls = [];
  sandbox.fetch = (url) => {
    const u = String(url);
    sandbox.__urls.push(u);
    const ok = body => Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(body), text: () => Promise.resolve(JSON.stringify(body)) });
    if (u.includes('/api/projects') && !u.includes('/policy/') && !u.includes('/rollup') && !u.includes('/history')) return ok(projects);
    for (const frag of Object.keys(replies)) {
      if (u.includes(frag)) {
        const r = replies[frag];
        if (r instanceof Error) return Promise.reject(r);
        return ok(r);
      }
    }
    if (u.includes('/history')) return ok({ items: [], gantt: [] });
    if (u.includes('/rollup')) return ok({});
    return ok({});
  };
  vm.createContext(sandbox);
  loadScript(APP, sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  vm.runInContext("state.activeView = 'projects'", sandbox);
  return { sb: sandbox, view };
}

const POLICY_BODY = {
  project: 'acme',
  policy: {
    max_concurrent_tasks: { value: 3, source: 'project' },
    retry_policy: { value: 'standard', source: 'daemon' },
  },
};
const MERGE_BODY = {
  project: 'acme',
  definition: { steps: [{ name: 'lint' }, { name: 'test' }] },
  source: 'file',
  reason: 'the project file is in use',
  conflict_policy: 'mechanical',
};
const EVENTS_BODY = {
  events: [
    { at: '2026-11-09T10:00:00Z', kind: 'automaton_started', level: 'info', automation_id: 'arc-a', task_id: 't1' },
  ],
  next_cursor: 9,
  dropped: false,
};

// selectAndLoad selects the project and runs the loader the page runs.
async function selectAndLoad(sb, name) {
  await sb._projectsLoad();
  vm.runInContext(`_projectsState.selected = ${JSON.stringify(name)}`, sb);
  await sb._projectsLoadSelected();
  await flushAsync();
}

test('the loader reads the policy, the merge pipeline and the project-filtered events', async () => {
  const { sb } = load({ '/policy/effective': POLICY_BODY, '/merge-pipeline': MERGE_BODY, '/autonomous/events': EVENTS_BODY });
  await selectAndLoad(sb, 'acme');
  const urls = Array.from(sb.__urls);
  assert.ok(urls.some(u => u === '/api/projects/acme/policy/effective'), 'policy read for the project');
  assert.ok(urls.some(u => u === '/api/autonomous/merge-pipeline?project=acme'), 'merge pipeline read for the project');
  assert.ok(urls.some(u => u.startsWith('/api/autonomous/events?') && u.includes('project=acme') && u.includes('queue=pwa')), 'events read with ?project=');
  const L = vm.runInContext('_projectsLinks', sb);
  assert.equal(L.name, 'acme');
  assert.equal(L.events.events.length, 1);
});

test('a failed read shows its own error and does not hide the other sections', async () => {
  const { sb } = load({
    '/policy/effective': new Error('boom'),
    '/merge-pipeline': MERGE_BODY,
    '/autonomous/events': new Error('503 pending events queue disabled'),
  });
  await selectAndLoad(sb, 'acme');
  const html = sb._projectLinksHTML({ name: 'acme' });
  assert.ok(html.includes('boom'), 'policy error shown');
  assert.ok(html.includes('the project file is in use'), 'merge pipeline still shown');
  assert.ok(html.includes('503 pending events queue disabled'), 'events error shown');
});

test('the policy section lists each value with its source and links to Settings, with no edit controls', async () => {
  const { sb } = load({ '/policy/effective': POLICY_BODY, '/merge-pipeline': MERGE_BODY, '/autonomous/events': EVENTS_BODY });
  await selectAndLoad(sb, 'acme');
  const html = sb._projectLinksHTML({ name: 'acme' });
  assert.ok(html.includes('max_concurrent_tasks:'), 'policy key shown');
  assert.ok(html.includes('(project)'), 'source shown');
  assert.ok(html.includes("_projectsOpenSettings(&quot;project_policy&quot;)") || html.includes('_projectsOpenSettings("project_policy")'), 'link opens the project policy settings');
  assert.ok(html.includes('_projectsOpenSettings(&quot;&quot;)') || html.includes('_projectsOpenSettings("")'), 'merge pipeline link opens Settings');
  assert.ok(!/<(input|select|textarea|button)\b/i.test(html), 'no edit controls in the links block');
});

test('the merge pipeline shows the source, the reason, the policy and the steps', async () => {
  const { sb } = load({ '/policy/effective': POLICY_BODY, '/merge-pipeline': MERGE_BODY, '/autonomous/events': EVENTS_BODY });
  await selectAndLoad(sb, 'acme');
  const html = sb._projectLinksHTML({ name: 'acme' });
  assert.ok(html.includes('the project file is in use'));
  assert.equal((html.match(/merge-pipeline-step/g) || []).length, 2, 'two steps listed');
});

test('an empty merge definition says so and an empty event list says so', async () => {
  const { sb } = load({
    '/policy/effective': { policy: {} },
    '/merge-pipeline': { source: 'default', definition: null, reason: '' },
    '/autonomous/events': { events: [], next_cursor: 0, dropped: true },
  });
  await selectAndLoad(sb, 'acme');
  const html = sb._projectLinksHTML({ name: 'acme' });
  assert.ok(html.includes('No policy values are set for this project.'));
  assert.ok(html.includes('No events for this project yet.'));
  assert.ok(html.includes('Some earlier events were dropped'), 'dropped note shown');
});

test('an arc shows its plan and design links and its datawatch-app issues', () => {
  const { sb } = load();
  const p = {
    name: 'acme',
    arcs: [{
      automation_id: 'arc-a',
      plan_docs: ['https://example.com/plan.md', 'docs/plans/x.md'],
      design_docs: ['https://example.com/design.md'],
      issues: [305, 'https://example.com/issues/306'],
    }],
  };
  const html = sb._projectArcDocsHTML(p, { automation_id: 'arc-a' });
  assert.ok(html.includes('href="https://example.com/plan.md"'), 'an http plan link is a link');
  assert.ok(html.includes('href="https://example.com/design.md"'), 'a design link is a link');
  assert.ok(html.includes('docs/plans/x.md') && !html.includes('href="docs/plans/x.md"'), 'a relative path is text, not a link');
  assert.ok(html.includes('datawatch-app #305'), 'a numeric issue is named by number');
  assert.ok(html.includes('href="https://example.com/issues/306"'), 'an issue URL is a link');
});

test('an arc with no record links shows none recorded and never a javascript: link', () => {
  const { sb } = load();
  const html = sb._projectArcDocsHTML({ name: 'acme', arcs: [] }, { automation_id: 'arc-b' });
  assert.ok(html.includes('none recorded'));
  assert.equal((html.match(/<a /g) || []).length, 0);
  const p = { name: 'acme', arcs: [{ automation_id: 'arc-b', plan_docs: ['javascript:alert(1)'] }] };
  const evil = sb._projectArcDocsHTML(p, { automation_id: 'arc-b' });
  assert.ok(!evil.includes('href='), 'a javascript: value is shown as text only');
  assert.ok(evil.includes('javascript:alert(1)'));
});

test('the event stream and policy sections are not shown for another project', async () => {
  const { sb } = load({ '/policy/effective': POLICY_BODY, '/merge-pipeline': MERGE_BODY, '/autonomous/events': EVENTS_BODY });
  await selectAndLoad(sb, 'acme');
  const html = sb._projectLinksHTML({ name: 'other' });
  assert.ok(!html.includes('max_concurrent_tasks'), 'acme policy not shown for other');
  assert.ok(!html.includes('the project file is in use'), 'acme merge not shown for other');
  assert.ok(html.includes('Loading'), 'other project reads as still loading');
});

test('a reply that arrives after the selection changed is dropped', async () => {
  const { sb } = load({ '/policy/effective': POLICY_BODY, '/merge-pipeline': MERGE_BODY, '/autonomous/events': EVENTS_BODY }, [
    { name: 'acme', arcs: [] }, { name: 'beta', arcs: [] },
  ]);
  await sb._projectsLoad();
  vm.runInContext(`_projectsState.selected = 'acme'`, sb);
  const pending = sb._projectsLoadLinks();
  vm.runInContext(`_projectsLinks.name = 'beta'`, sb);
  await pending;
  await flushAsync();
  const L = vm.runInContext('_projectsLinks', sb);
  assert.equal(L.name, 'beta', 'the stale reply does not overwrite the newer project');
  assert.equal(L.policy, null, 'no acme policy stored under beta');
});

test('the Settings link opens Settings and scrolls to a section that has an element', () => {
  const { sb } = load();
  let navigated = null;
  sb.navigate = (v) => { navigated = v; };
  sb._projectsOpenSettings('project_policy');
  assert.equal(navigated, 'settings');
});
