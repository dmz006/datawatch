// The Automata and Dashboard nav buttons.
//
// Both start hidden. They show whenever the autonomous setting can be read, with
// automatic work on or off (the pages only view stored records until a button is
// pressed), and stay hidden when the setting cannot be read.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

function loadView() {
  const els = { navBtnAutonomous: makeStubElement(), navBtnDashboard: makeStubElement() };
  for (const el of Object.values(els)) el.style.display = 'none';
  const sandbox = buildSandbox({ byId: els });
  sandbox.fetch = () => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve([]), text: () => Promise.resolve('[]') });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return { sb: sandbox, els };
}

// The ways the setting cannot be read: daemon down, refused token, no engine
// to answer, and an answer that is not JSON.
const UNREADABLE = {
  'daemon down': () => Promise.reject(new Error('connection refused')),
  '401': () => Promise.resolve({ ok: false, status: 401, json: () => Promise.resolve({}) }),
  '503': () => Promise.resolve({ ok: false, status: 503, json: () => Promise.resolve({}) }),
  'bad answer': () => Promise.resolve({ ok: true, status: 200, json: () => Promise.reject(new Error('not JSON')) }),
};

// readSetting runs the boot read of /api/autonomous/config against one answer.
async function readSetting(sb, answer) {
  const rest = sb.fetch;
  sb.fetch = (url, opts) => (url === '/api/autonomous/config' ? answer() : rest(url, opts));
  await sb._loadAutomataPageGate();
  sb.fetch = rest;
}
const OFF = () => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ enabled: false }) });

const shown = els => [els.navBtnAutonomous.style.display, els.navBtnDashboard.style.display];

for (const enabled of [true, false]) {
  test('both nav buttons show when the setting reads enabled: ' + enabled, async () => {
    const { sb, els } = loadView();
    await readSetting(sb, () => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ enabled }) }));
    assert.deepEqual(shown(els), ['', '']);
  });
}

test('both nav buttons stay hidden when the setting cannot be read', async () => {
  for (const [name, answer] of Object.entries(UNREADABLE)) {
    const { sb, els } = loadView();
    await readSetting(sb, answer);
    assert.deepEqual(shown(els), ['none', 'none'], name);
  }
});

test('opening the Dashboard shows its button with automatic work off, and not when the setting cannot be read', async () => {
  for (const [name, answer] of Object.entries({ off: OFF, ...UNREADABLE })) {
    const { sb, els } = loadView();
    sb.fetch = () => answer();
    sb._dashCheckEnabled();
    await flushAsync();
    assert.equal(els.navBtnDashboard.style.display, name === 'off' ? '' : 'none', name);
  }
});
