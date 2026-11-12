// Dashboard Live Events card — fed from the daemon event stream
// (GET /api/autonomous/event-log), merged with hook events.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function load() {
  const feed = { innerHTML: '' };
  const sandbox = buildSandbox({ byId: { dashEventFeed: feed } });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return { sandbox, feed };
}
const run = (sb, code) => vm.runInContext(code, sb);

test('daemon events render with kind, text and age, newest first', () => {
  const { sandbox, feed } = load();
  const now = Date.now();
  run(sandbox, `_dash._eventLog = []; _dash._daemonEvents = ${JSON.stringify([
    { kind: 'decision', summary: 'Decision recorded', time: new Date(now - 120000).toISOString() },
    { kind: 'watch', summary: 'Task started', time: new Date(now - 5000).toISOString() },
  ])};`);
  run(sandbox, '_dashRenderEventFeed()');
  assert.match(feed.innerHTML, /Task started[\s\S]*Decision recorded/);
  assert.match(feed.innerHTML, /2m ago/);
  assert.doesNotMatch(feed.innerHTML, /Waiting for hook events/);
});

test('hook events merge in with daemon events', () => {
  const { sandbox, feed } = load();
  const now = Date.now();
  run(sandbox, `_dash._daemonEvents = [{kind:'check', summary:'Checks passed', time:'${new Date(now - 60000).toISOString()}'}];
    _dash._eventLog = [{t:${now}, name:'sess-a', event:'Stop', tool:'', task:'', color:'#fff'}];`);
  run(sandbox, '_dashRenderEventFeed()');
  assert.match(feed.innerHTML, /hook/);
  assert.match(feed.innerHTML, /sess-a/);
  assert.match(feed.innerHTML, /Checks passed/);
});

test('empty stream shows only the one-line purpose', () => {
  const { sandbox, feed } = load();
  run(sandbox, '_dash._eventLog = []; _dash._daemonEvents = []; _dashRenderEventFeed()');
  assert.equal((feed.innerHTML.match(/<div/g) || []).length, 1);
  assert.match(feed.innerHTML, /dash_card_events_purpose|Newest Automata and session events/);
});

test('summaries are escaped', () => {
  const { sandbox, feed } = load();
  run(sandbox, `_dash._eventLog = []; _dash._daemonEvents = [{kind:'error', summary:'<img src=x>', time:new Date().toISOString()}]; _dashRenderEventFeed()`);
  assert.doesNotMatch(feed.innerHTML, /<img/);
});

test('card is inactive until an event exists, via the feature mechanism', () => {
  const { sandbox } = load();
  run(sandbox, '_dash._eventLog = []; _dash._daemonEvents = [];');
  assert.equal(run(sandbox, "_dashCardFeature('events').active()"), false);
  run(sandbox, "_dash._daemonEvents = [{kind:'watch', summary:'x', time:new Date().toISOString()}];");
  assert.equal(run(sandbox, "_dashCardFeature('events').active()"), true);
  run(sandbox, '_dash._daemonEvents = []; _dash._eventLog = [{t:1,name:"a",event:"Stop",color:"#fff"}];');
  assert.equal(run(sandbox, "_dashCardFeature('events').active()"), true);
});

test('poll uses a window handle and the new strings exist in all five locales', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  assert.match(src, /window\._dashEventLogInterval\s*=\s*setInterval/);
  for (const l of ['en', 'de', 'es', 'fr', 'ja']) {
    const j = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));
    for (const k of ['dash_card_events_purpose', 'dash_card_off_events_reason', 'dash_card_off_events_link']) {
      assert.ok(j[k], `${l} missing ${k}`);
    }
    assert.doesNotMatch(j.dash_card_events_purpose + j.dash_card_off_events_reason, /\bPRDs?\b/);
  }
});
