// ── State ──────────────────────────────────────────────────────────────────
const state = {
  connected: false,
  sessions: [],
  activeView: 'sessions', // sessions | new | settings | session-detail | alerts
  activeSession: null,    // session FullID being viewed
  ws: null,
  reconnectDelay: 1000,
  reconnectTimer: null,
  token: localStorage.getItem('cs_token') || '',
  outputBuffer: {},       // sessionId -> string[]
  notifPermission: Notification.permission,
  sessionOrder: JSON.parse(localStorage.getItem('cs_session_order') || '[]'), // manual ordering
  servers: [],            // remote server list from /api/servers
  activeServer: null,     // selected server name (null = local)
  alertUnread: 0,         // unread alert count for badge
  showHistory: false,     // show completed/killed/failed sessions in main list
  backPressCount: 0,      // for double-back-press confirmation
  backPressTimer: null,
};

function buildWsUrl() {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const token = localStorage.getItem('cs_token') || '';
  const q = token ? `?token=${encodeURIComponent(token)}` : '';
  return `${proto}//${location.host}/ws${q}`;
}

// ── WebSocket ───────────────────────────────────────────────────────────────
function connect() {
  if (state.ws && (state.ws.readyState === WebSocket.OPEN || state.ws.readyState === WebSocket.CONNECTING)) {
    return;
  }

  const url = buildWsUrl();
  let ws;
  try {
    ws = new WebSocket(url);
  } catch (e) {
    scheduleReconnect();
    return;
  }
  state.ws = ws;

  ws.addEventListener('open', () => {
    state.connected = true;
    state.reconnectDelay = 1000;
    updateStatusDot();
    showToast('Connected', 'success', 2000);
  });

  ws.addEventListener('message', e => {
    let msg;
    try { msg = JSON.parse(e.data); } catch { return; }
    handleMessage(msg);
  });

  ws.addEventListener('close', () => {
    state.connected = false;
    state.ws = null;
    updateStatusDot();
    scheduleReconnect();
  });

  ws.addEventListener('error', () => {
    // close event will fire after error and trigger reconnect
  });
}

function disconnect() {
  if (state.reconnectTimer) {
    clearTimeout(state.reconnectTimer);
    state.reconnectTimer = null;
  }
  if (state.ws) {
    state.ws.close();
    state.ws = null;
  }
}

function scheduleReconnect() {
  if (state.reconnectTimer) return;
  state.reconnectTimer = setTimeout(() => {
    state.reconnectTimer = null;
    connect();
  }, state.reconnectDelay);
  state.reconnectDelay = Math.min(state.reconnectDelay * 2, 30000);
}

function send(type, data) {
  if (!state.ws || state.ws.readyState !== WebSocket.OPEN) {
    showToast('Not connected', 'error');
    return false;
  }
  state.ws.send(JSON.stringify({ type, data, ts: new Date().toISOString() }));
  return true;
}

// ── Message handlers ─────────────────────────────────────────────────────────
function handleMessage(msg) {
  switch (msg.type) {
    case 'sessions':
      if (msg.data && msg.data.sessions) {
        state.sessions = msg.data.sessions || [];
      } else {
        state.sessions = msg.data || [];
      }
      onSessionsUpdated();
      break;
    case 'session_state':
      updateSession(msg.data);
      break;
    case 'output':
      if (msg.data) {
        appendOutput(msg.data.session_id, msg.data.lines || []);
      }
      break;
    case 'needs_input':
      if (msg.data) {
        handleNeedsInput(msg.data.session_id, msg.data.prompt || '');
      }
      break;
    case 'notification':
      if (msg.data && msg.data.message) {
        showToast(msg.data.message);
      }
      break;
    case 'error':
      if (msg.data && msg.data.message) {
        showToast(msg.data.message, 'error');
      }
      break;
    case 'alert':
      if (msg.data) {
        handleAlert(msg.data);
      }
      break;
  }
}

function handleAlert(a) {
  state.alertUnread++;
  updateAlertBadge();
  const level = a.level === 'error' ? 'error' : a.level === 'warn' ? 'error' : 'info';
  showToast(`⚠ ${a.title}: ${a.body}`, level, 5000);
}

function updateSession(sess) {
  if (!sess) return;
  const idx = state.sessions.findIndex(s => s.full_id === sess.full_id);
  if (idx >= 0) {
    state.sessions[idx] = sess;
  } else {
    state.sessions.push(sess);
  }
  onSessionsUpdated();
}

function appendOutput(sessionId, lines) {
  if (!state.outputBuffer[sessionId]) {
    state.outputBuffer[sessionId] = [];
  }
  state.outputBuffer[sessionId].push(...lines);
  // Keep last 500 lines in buffer
  if (state.outputBuffer[sessionId].length > 500) {
    state.outputBuffer[sessionId] = state.outputBuffer[sessionId].slice(-500);
  }

  // If currently viewing this session, append to display
  if (state.activeView === 'session-detail' && state.activeSession === sessionId) {
    const outputArea = document.querySelector('.output-area');
    if (outputArea) {
      const wasAtBottom = outputArea.scrollHeight - outputArea.scrollTop <= outputArea.clientHeight + 40;
      lines.forEach(line => {
        const div = document.createElement('div');
        div.className = 'output-line new-line';
        div.textContent = line;
        outputArea.appendChild(div);
      });
      if (wasAtBottom) {
        outputArea.scrollTop = outputArea.scrollHeight;
      }
    }
  }
}

function handleNeedsInput(sessionId, prompt) {
  // Update session state in memory
  const sess = state.sessions.find(s => s.full_id === sessionId || s.id === sessionId);

  // Show browser notification
  if (state.notifPermission === 'granted') {
    const sessLabel = sess ? sess.id : sessionId;
    new Notification('Datawatch — Input Needed', {
      body: `Session [${sessLabel}] is waiting for your input.\n${prompt.slice(0, 80)}`,
      icon: '/icon-192.svg',
      tag: 'needs-input-' + sessionId,
      renotify: true,
    });
  }

  // If viewing this session, highlight the input bar
  if (state.activeView === 'session-detail' && state.activeSession === sessionId) {
    const bar = document.querySelector('.input-bar');
    if (bar) bar.classList.add('needs-input');
    const label = document.querySelector('.input-label');
    if (label) label.style.display = 'block';
    // Show banner
    const existing = document.querySelector('.needs-input-banner');
    if (!existing) {
      const banner = document.createElement('div');
      banner.className = 'needs-input-banner';
      banner.textContent = 'Waiting for input: ' + (prompt.slice(0, 100) || 'response required');
      const outputArea = document.querySelector('.output-area');
      if (outputArea && outputArea.parentNode) {
        outputArea.parentNode.insertBefore(banner, outputArea);
      }
    }
  }

  // Show toast notification
  const sessLabel = sess ? sess.id : sessionId;
  showToast(`[${sessLabel}] needs input`, 'info', 5000);
}

function onSessionsUpdated() {
  if (state.activeView === 'sessions') {
    renderSessionsView();
  } else if (state.activeView === 'session-detail' && state.activeSession) {
    // Update header state badge
    const sess = state.sessions.find(s => s.full_id === state.activeSession);
    if (sess) {
      const badge = document.querySelector('.detail-state-badge');
      if (badge) {
        badge.textContent = sess.state;
        badge.className = `state state-badge-${sess.state}`;
      }
    }
  }
}

// ── Navigation ───────────────────────────────────────────────────────────────
function navigate(view, sessionId) {
  state.activeView = view;

  const backBtn = document.getElementById('backBtn');
  const nav = document.getElementById('nav');
  const headerTitle = document.getElementById('headerTitle');

  // Update nav button active states
  document.querySelectorAll('.nav-btn').forEach(btn => {
    btn.classList.toggle('active', btn.dataset.view === view);
  });

  if (view === 'session-detail') {
    state.activeSession = sessionId;
    backBtn.style.display = 'inline';
    nav.style.display = 'none';
    headerTitle.textContent = 'Session ' + (sessionId ? sessionId.split('-').pop() : '');
    renderSessionDetail(sessionId);
  } else {
    state.activeSession = null;
    backBtn.style.display = 'none';
    nav.style.display = 'flex';

    if (view === 'sessions') {
      headerTitle.textContent = 'Datawatch';
      renderSessionsView();
    } else if (view === 'new') {
      headerTitle.textContent = 'New Session';
      renderNewSessionView();
    } else if (view === 'settings') {
      headerTitle.textContent = 'Settings';
      renderSettingsView();
    } else if (view === 'alerts') {
      headerTitle.textContent = 'Alerts';
      renderAlertsView();
    }
  }
}

// ── Session list view ─────────────────────────────────────────────────────────
const DONE_STATES = new Set(['complete', 'failed', 'killed']);

function renderSessionsView() {
  const view = document.getElementById('view');
  if (state.activeView !== 'sessions') return;

  const active = state.sessions.filter(s => !DONE_STATES.has(s.state));
  const history = state.sessions.filter(s => DONE_STATES.has(s.state));
  const visible = state.showHistory
    ? sortSessionsByOrder(state.sessions)
    : sortSessionsByOrder(active);

  const toggleBtn = `<div class="sessions-toolbar">
    <button class="btn-toggle-history ${state.showHistory ? 'active' : ''}" onclick="toggleHistory()">
      ${state.showHistory ? 'Hide' : 'Show'} history (${history.length})
    </button>
  </div>`;

  if (visible.length === 0 && active.length === 0) {
    view.innerHTML = `
      <div class="view-content">
        ${history.length > 0 ? toggleBtn : ''}
        <div class="empty-state">
          <span class="empty-state-icon">⚡</span>
          <h3>No active sessions</h3>
          <p>Tap <strong>New</strong> below to start a session,<br>or send commands via Signal.</p>
        </div>
      </div>`;
    return;
  }

  const cards = visible.map((sess, idx) => sessionCard(sess, idx, visible.length)).join('');
  view.innerHTML = `<div class="view-content">${toggleBtn}<div class="session-list">${cards}</div></div>`;
}

function toggleHistory() {
  state.showHistory = !state.showHistory;
  renderSessionsView();
}

function sortSessionsByOrder(sessions) {
  const order = state.sessionOrder;
  const inOrder = [], rest = [];
  const seen = new Set();
  for (const id of order) {
    const s = sessions.find(x => (x.full_id || x.id) === id);
    if (s) { inOrder.push(s); seen.add(id); }
  }
  for (const s of sessions) {
    const id = s.full_id || s.id;
    if (!seen.has(id)) rest.push(s);
  }
  rest.sort((a, b) => new Date(b.updated_at) - new Date(a.updated_at));
  return [...inOrder, ...rest];
}

function moveSession(fullId, dir) {
  const order = sortSessionsByOrder(state.sessions).map(s => s.full_id || s.id);
  const idx = order.indexOf(fullId);
  if (idx < 0) return;
  const newIdx = idx + dir;
  if (newIdx < 0 || newIdx >= order.length) return;
  [order[idx], order[newIdx]] = [order[newIdx], order[idx]];
  state.sessionOrder = order;
  localStorage.setItem('cs_session_order', JSON.stringify(order));
  renderSessionsView();
}

function sessionCard(sess, idx, total) {
  const stateClass = `state-${sess.state}`;
  const badgeClass = `state-badge-${sess.state}`;
  const ago = timeAgo(sess.updated_at);
  const taskText = (sess.task || '').length > 80 ? sess.task.slice(0, 80) + '…' : (sess.task || '(no task)');
  const shortId = sess.id || (sess.full_id || '').split('-').pop() || '????';
  const hostname = sess.hostname || '';
  const fullId = sess.full_id || sess.id || '';
  const canUp = idx > 0;
  const canDown = idx < total - 1;

  return `
    <div class="session-card ${stateClass}">
      <div class="session-card-header" onclick="navigate('session-detail', '${escHtml(fullId)}')">
        <span class="id">${escHtml(shortId)}</span>
        <span class="state ${badgeClass}">${escHtml(sess.state || 'unknown')}</span>
        <span class="time">${escHtml(ago)}</span>
      </div>
      <div class="task" onclick="navigate('session-detail', '${escHtml(fullId)}')">${escHtml(taskText)}</div>
      ${hostname ? `<div class="hostname">${escHtml(hostname)}</div>` : ''}
      <div class="session-order-btns">
        <button class="order-btn" ${canUp ? '' : 'disabled'} onclick="event.stopPropagation();moveSession('${escHtml(fullId)}',-1)" title="Move up">↑</button>
        <button class="order-btn" ${canDown ? '' : 'disabled'} onclick="event.stopPropagation();moveSession('${escHtml(fullId)}',1)" title="Move down">↓</button>
      </div>
    </div>`;
}

// ── Session detail view ───────────────────────────────────────────────────────
function renderSessionDetail(sessionId) {
  const view = document.getElementById('view');
  const sess = state.sessions.find(s => s.full_id === sessionId);

  const taskText = sess ? (sess.task || '') : '';
  const stateText = sess ? (sess.state || 'unknown') : 'unknown';
  const badgeClass = `state-badge-${stateText}`;
  const shortId = sess ? (sess.id || '') : '';
  const isWaiting = stateText === 'waiting_input';

  // Subscribe to output for this session
  send('subscribe', { session_id: sessionId });

  // Build output from buffer
  const lines = state.outputBuffer[sessionId] || [];
  const outputHtml = lines.map(l => `<div class="output-line">${escHtml(l)}</div>`).join('');

  const needsBanner = isWaiting
    ? `<div class="needs-input-banner">Waiting for input${sess && sess.last_prompt ? ': ' + escHtml(sess.last_prompt.slice(0, 100)) : ''}</div>`
    : '';

  const nameText = sess ? (sess.name || '') : '';
  const displayTitle = nameText || taskText || '(no task)';
  const backendText = sess ? (sess.llm_backend || '') : '';
  const projectDir = sess ? (sess.project_dir || '') : '';
  const isActive = stateText === 'running' || stateText === 'waiting_input' || stateText === 'rate_limited';
  const isDone = stateText === 'complete' || stateText === 'failed' || stateText === 'killed';

  const actionButtons = isActive
    ? `<button class="btn-stop" onclick="killSession('${escHtml(sessionId)}')" title="Stop session">&#9632; Stop</button>`
    : isDone
    ? `<button class="btn-restart" onclick="restartSession('${escHtml(sessionId)}')" title="Restart with same task">&#8635; Restart</button>`
    : '';

  view.innerHTML = `
    <div class="session-detail">
      <div class="session-info-bar">
        <div class="task-text" title="${escHtml(taskText)}">${escHtml(displayTitle)}</div>
        <div class="meta">
          <span class="id">${escHtml(shortId)}</span>
          ${backendText ? `<span class="backend-badge">${escHtml(backendText)}</span>` : ''}
          <span class="state detail-state-badge ${badgeClass}">${escHtml(stateText)}</span>
          ${actionButtons}
        </div>
        <div class="session-rename-row">
          <input type="text" class="rename-input" id="renameInput"
            value="${escHtml(nameText)}" placeholder="Name this session…" />
          <button class="btn-icon" onclick="renameSession('${escHtml(sessionId)}')" title="Rename">✎</button>
        </div>
      </div>
      ${needsBanner}
      <div class="output-area" id="outputArea">${outputHtml}</div>
      ${isWaiting ? `<div class="quick-input-row">
        <button class="quick-btn" onclick="sendQuickInput('y')">y</button>
        <button class="quick-btn" onclick="sendQuickInput('n')">n</button>
        <button class="quick-btn" onclick="sendQuickInput('')">Enter</button>
        <button class="quick-btn quick-btn-danger" onclick="sendQuickInput('__ctrlc__')">Ctrl‑C</button>
      </div>` : ''}
      ${isActive ? `<div class="input-bar${isWaiting ? ' needs-input' : ''}">
        <div class="input-field-wrap">
          <div class="input-label" style="display:${isWaiting ? 'block' : 'none'}">Input Required</div>
          <input
            type="text"
            class="input-field"
            id="sessionInput"
            placeholder="${isWaiting ? 'Type your response…' : 'Send command or input…'}"
            autocomplete="off"
            autocorrect="off"
            spellcheck="false"
          />
        </div>
        <button class="send-btn" onclick="sendSessionInput()">&#9658;</button>
      </div>` : ''}
    </div>`;

  // Scroll output to bottom
  const outputArea = document.getElementById('outputArea');
  if (outputArea) {
    outputArea.scrollTop = outputArea.scrollHeight;
  }

  // Allow Enter key to send (only when input bar is visible for active sessions)
  const inputEl = document.getElementById('sessionInput');
  if (inputEl) {
    inputEl.addEventListener('keydown', e => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        sendSessionInput();
      }
    });
    inputEl.focus();
  }
}

function killSession(sessionId) {
  if (!confirm('Stop this session?')) return;
  const token = localStorage.getItem('cs_token') || '';
  const headers = { 'Content-Type': 'application/json' };
  if (token) headers['Authorization'] = 'Bearer ' + token;
  fetch('/api/sessions/kill', {
    method: 'POST',
    headers,
    body: JSON.stringify({ id: sessionId }),
  })
    .then(r => r.ok ? showToast('Session stopped', 'success', 2000) : showToast('Stop failed', 'error'))
    .catch(() => showToast('Stop failed', 'error'));
}

function restartSession(sessionId) {
  const sess = state.sessions.find(s => s.full_id === sessionId);
  if (!sess) return;
  // Pre-fill the new session form and navigate to it
  navigate('new');
  // Wait for the view to render then populate fields
  setTimeout(() => {
    const taskEl = document.getElementById('taskInput');
    const nameEl = document.getElementById('sessionNameInput');
    const backendEl = document.getElementById('backendSelect');
    const dirDisplay = document.getElementById('selectedDirDisplay');
    const resumeEl = document.getElementById('resumeIdInput');
    if (taskEl) taskEl.value = sess.task || '';
    if (nameEl) nameEl.value = sess.name ? sess.name + ' (restart)' : '';
    if (backendEl && sess.llm_backend) {
      for (const opt of backendEl.options) {
        if (opt.value === sess.llm_backend) { opt.selected = true; break; }
      }
    }
    if (sess.project_dir) {
      newSessionState.selectedDir = sess.project_dir;
      if (dirDisplay) dirDisplay.textContent = sess.project_dir;
    }
    // Pre-fill resume ID if available (stored on the session as llm_session_id)
    if (resumeEl && sess.llm_session_id) {
      resumeEl.value = sess.llm_session_id;
    }
    showToast('Pre-filled from previous session', 'success', 2000);
  }, 150);
}

function sendSessionInput() {
  const inputEl = document.getElementById('sessionInput');
  if (!inputEl) return;
  const text = inputEl.value.trim();
  if (!text) return;

  if (state.activeSession) {
    const sess = state.sessions.find(s => s.full_id === state.activeSession);
    if (sess && sess.state === 'waiting_input') {
      // Send as input to session
      send('send_input', { session_id: state.activeSession, text });
    } else {
      // Send as generic command
      send('command', { text: `send ${state.activeSession}: ${text}` });
    }
  }

  inputEl.value = '';
}

function sendQuickInput(key) {
  if (!state.activeSession) return;
  if (key === '__ctrlc__') {
    // Send Ctrl-C as a special interrupt command
    send('command', { text: `send ${state.activeSession}: \x03` });
  } else {
    send('send_input', { session_id: state.activeSession, text: key });
  }
}

function renameSession(sessionId) {
  const input = document.getElementById('renameInput');
  if (!input) return;
  const name = input.value.trim();
  const token = localStorage.getItem('cs_token') || '';
  const headers = { 'Content-Type': 'application/json' };
  if (token) headers['Authorization'] = 'Bearer ' + token;
  fetch('/api/sessions/rename', {
    method: 'POST',
    headers,
    body: JSON.stringify({ id: sessionId, name }),
  })
    .then(r => r.ok ? showToast('Session renamed', 'success', 2000) : showToast('Rename failed', 'error'))
    .catch(() => showToast('Rename failed', 'error'));
}

// ── New session view ──────────────────────────────────────────────────────────
// State for new session form
const newSessionState = {
  backends: [],
  selectedDir: '',
  browsing: false,
};

function renderNewSessionView() {
  const view = document.getElementById('view');
  view.innerHTML = `
    <div class="view-content">
      <div class="new-session-view">
        <div>
          <h2>New Session</h2>
          <p>Describe the coding task for the AI to work on.</p>
        </div>
        <div class="form-group">
          <label for="sessionNameInput">Session name (optional)</label>
          <input
            id="sessionNameInput"
            class="form-input"
            type="text"
            placeholder="e.g. Auth refactor"
          />
        </div>
        <div class="form-group">
          <label for="taskInput">Task description</label>
          <textarea
            id="taskInput"
            class="form-textarea"
            placeholder="e.g. Add unit tests to internal/session/manager.go covering the Start and Kill methods"
            rows="5"
          ></textarea>
        </div>
        <div class="form-group">
          <label for="backendSelect">LLM backend</label>
          <select id="backendSelect" class="form-select">
            <option value="">Loading backends…</option>
          </select>
        </div>
        <div class="form-group">
          <label>Project directory</label>
          <div class="dir-picker">
            <span id="selectedDirDisplay" class="dir-display">~/</span>
            <button class="btn-secondary" onclick="openDirBrowser()">Browse</button>
          </div>
        </div>
        <div id="dirBrowser" class="dir-browser" style="display:none">
          <div id="dirBrowserContent"></div>
        </div>
        <div class="form-group">
          <label for="resumeIdInput">Resume session ID <span style="color:var(--text2);font-size:11px;">(optional — claude: conversation ID, opencode: -s SESSION_ID)</span></label>
          <input
            id="resumeIdInput"
            class="form-input"
            type="text"
            placeholder="Leave empty to start fresh"
            autocomplete="off"
            spellcheck="false"
          />
        </div>
        <button class="btn-primary" onclick="submitNewSession()">Start Session</button>

        <div class="session-backlog-section">
          <div class="session-backlog-title">Restart a previous session</div>
          <div id="sessionBacklog" class="session-backlog-list">
            <div style="color:var(--text2);font-size:13px;">Loading…</div>
          </div>
        </div>
      </div>
    </div>`;

  const taskInput = document.getElementById('taskInput');
  if (taskInput) {
    taskInput.focus();
    taskInput.addEventListener('keydown', e => {
      if (e.key === 'Enter' && e.metaKey) {
        submitNewSession();
      }
    });
  }

  // Load backends and session backlog
  fetchBackends();
  renderSessionBacklog();
}

function renderSessionBacklog() {
  const el = document.getElementById('sessionBacklog');
  if (!el) return;
  // Use sessions already in state
  const done = state.sessions.filter(s =>
    s.state === 'complete' || s.state === 'failed' || s.state === 'killed'
  ).sort((a, b) => new Date(b.updated_at) - new Date(a.updated_at)).slice(0, 20);

  if (done.length === 0) {
    el.innerHTML = '<div style="color:var(--text2);font-size:13px;">No previous sessions.</div>';
    return;
  }
  el.innerHTML = done.map(s => {
    const taskSnippet = (s.task || '').length > 60 ? s.task.slice(0, 60) + '…' : (s.task || '(no task)');
    const backend = s.llm_backend || '';
    const badgeClass = `state-badge-${s.state}`;
    const ago = timeAgo(s.updated_at);
    return `<div class="backlog-entry">
      <div class="backlog-entry-main">
        <span class="backlog-task" title="${escHtml(s.task || '')}">${escHtml(taskSnippet)}</span>
        <span class="state ${badgeClass}" style="font-size:10px;">${escHtml(s.state)}</span>
      </div>
      <div class="backlog-entry-meta">
        ${backend ? `<span class="backend-badge" style="font-size:10px;">${escHtml(backend)}</span>` : ''}
        <span style="color:var(--text2);font-size:11px;">${escHtml(ago)}</span>
      </div>
      <button class="btn-secondary backlog-restart-btn" onclick="restartSession('${escHtml(s.full_id || s.id)}')">&#8635; Restart</button>
    </div>`;
  }).join('');
}

function fetchBackends() {
  const token = localStorage.getItem('cs_token') || '';
  const headers = token ? { 'Authorization': 'Bearer ' + token } : {};
  fetch('/api/backends', { headers })
    .then(r => r.json())
    .then(data => {
      newSessionState.backends = data.llm || [];
      const sel = document.getElementById('backendSelect');
      if (!sel) return;
      sel.innerHTML = newSessionState.backends.map(b =>
        `<option value="${escHtml(b)}"${b === data.active ? ' selected' : ''}>${escHtml(b)}</option>`
      ).join('');
      if (newSessionState.backends.length === 0) {
        sel.innerHTML = '<option value="">No backends registered</option>';
      }
    })
    .catch(() => {
      const sel = document.getElementById('backendSelect');
      if (sel) sel.innerHTML = '<option value="">claude-code</option>';
    });
}

function openDirBrowser() {
  const browser = document.getElementById('dirBrowser');
  if (!browser) return;
  if (newSessionState.browsing) {
    browser.style.display = 'none';
    newSessionState.browsing = false;
    return;
  }
  newSessionState.browsing = true;
  browser.style.display = 'block';
  loadDirContents(newSessionState.selectedDir || '~');
}

function loadDirContents(path) {
  const token = localStorage.getItem('cs_token') || '';
  const headers = token ? { 'Authorization': 'Bearer ' + token } : {};
  const content = document.getElementById('dirBrowserContent');
  if (content) content.innerHTML = '<div style="color:var(--text2);padding:8px;">Loading…</div>';
  fetch('/api/files?path=' + encodeURIComponent(path || '~'), { headers })
    .then(r => r.json())
    .then(data => {
      const content = document.getElementById('dirBrowserContent');
      if (!content) return;
      const currentPath = data.path || path;
      const selectBtn = `<button class="btn-secondary dir-select-btn" onclick="selectDir(${JSON.stringify(currentPath)})">&#10003; Use This Folder</button>`;
      const entries = (data.entries || []).filter(e => e.is_dir).map(e =>
        `<div class="dir-entry" onclick="dirNavigate(${JSON.stringify(e.path)})">
          <span class="dir-icon">${e.is_link ? '🔗' : (e.name === '..' ? '⬆' : '📁')}</span>
          <span>${escHtml(e.name)}</span>
        </div>`
      ).join('');
      content.innerHTML = `<div class="dir-current">${escHtml(currentPath)}</div>${selectBtn}${entries || '<div style="color:var(--text2);padding:8px;font-size:12px;">No subdirectories</div>'}`;
    })
    .catch(() => {
      const content = document.getElementById('dirBrowserContent');
      if (content) content.innerHTML = '<div class="dir-error">Cannot read directory</div>';
    });
}

function dirNavigate(path) {
  loadDirContents(path);
}

function selectDir(path) {
  newSessionState.selectedDir = path;
  const display = document.getElementById('selectedDirDisplay');
  if (display) display.textContent = path;
  // Close browser
  const browser = document.getElementById('dirBrowser');
  if (browser) browser.style.display = 'none';
  newSessionState.browsing = false;
  showToast('Project directory set', 'success', 1500);
}

function dirEntryClick(path, isDir) {
  // Legacy — kept for any inline onclick calls; new code uses dirNavigate/selectDir
  if (!isDir) return;
  dirNavigate(path);
}

function submitNewSession() {
  const taskInput = document.getElementById('taskInput');
  const nameInput = document.getElementById('sessionNameInput');
  const backendSel = document.getElementById('backendSelect');
  const resumeInput = document.getElementById('resumeIdInput');
  if (!taskInput) return;
  const task = taskInput.value.trim();
  if (!task) {
    showToast('Task description is required', 'error');
    return;
  }

  const btn = document.querySelector('.btn-primary');
  if (btn) {
    btn.disabled = true;
    btn.textContent = 'Starting…';
  }

  const payload = {
    task,
    name: nameInput ? nameInput.value.trim() : '',
    backend: backendSel ? backendSel.value : '',
    project_dir: newSessionState.selectedDir || '',
    resume_id: resumeInput ? resumeInput.value.trim() : '',
  };

  const ok = send('new_session', payload);
  if (ok) {
    taskInput.value = '';
    if (nameInput) nameInput.value = '';
    if (resumeInput) resumeInput.value = '';
    newSessionState.selectedDir = '';
    const browser = document.getElementById('dirBrowser');
    if (browser) browser.style.display = 'none';
    newSessionState.browsing = false;
    showToast('Session starting…', 'success', 2000);
    setTimeout(() => navigate('sessions'), 500);
  }

  if (btn) {
    btn.disabled = false;
    btn.textContent = 'Start Session';
  }
}

// ── Settings view ─────────────────────────────────────────────────────────────
function renderSettingsView() {
  const view = document.getElementById('view');
  const connClass = state.connected ? 'connected' : '';
  const connText = state.connected ? 'Connected' : 'Disconnected';
  const notifText = state.notifPermission === 'granted'
    ? 'Notifications enabled'
    : state.notifPermission === 'denied'
    ? 'Notifications blocked (check browser settings)'
    : 'Notifications not yet requested';

  view.innerHTML = `
    <div class="view-content">
      <div class="settings-view">
        <div class="settings-section">
          <div class="settings-section-title">Connection</div>
          <div class="settings-row">
            <div class="settings-label">Status</div>
            <div class="connection-indicator">
              <div class="dot ${connClass}"></div>
              <span>${escHtml(connText)}</span>
            </div>
          </div>
          <div class="settings-row">
            <div class="settings-label">Server</div>
            <div class="settings-value">${escHtml(location.host)}</div>
          </div>
        </div>

        <div class="settings-section">
          <div class="settings-section-title">Authentication</div>
          <div class="settings-row">
            <div class="settings-label">Bearer Token</div>
            <input
              type="password"
              class="settings-input"
              id="tokenInput"
              value="${escHtml(state.token)}"
              placeholder="Leave empty if no token configured"
              autocomplete="off"
            />
          </div>
          <div class="settings-row">
            <button class="btn-secondary" onclick="saveToken()">Save Token &amp; Reconnect</button>
          </div>
        </div>

        <div class="settings-section">
          <div class="settings-section-title">Signal Device Linking</div>
          <div class="settings-row">
            <div class="settings-label">Link Status</div>
            <div class="settings-value" id="linkStatusText">Checking…</div>
          </div>
          <div class="settings-row" id="linkActionRow">
            <button class="btn-secondary" onclick="startLinking()">Start Linking</button>
          </div>
          <div class="settings-row" id="linkQrRow" style="display:none">
            <div style="display:flex;flex-direction:column;align-items:center;gap:12px;width:100%;">
              <div id="linkQrCode" style="background:#fff;padding:12px;border-radius:8px;display:inline-block;"></div>
              <div style="font-size:12px;color:var(--text2);font-family:system-ui;text-align:center;line-height:1.5;">
                Open Signal on your phone<br>
                Settings &rarr; Linked Devices &rarr; Link New Device<br>
                then scan the QR code above.
              </div>
            </div>
          </div>
        </div>

        <div class="settings-section">
          <div class="settings-section-title">Notifications</div>
          <div class="settings-row">
            <div class="settings-label">Status</div>
            <div class="settings-value">${escHtml(notifText)}</div>
          </div>
          <div class="settings-row">
            <button class="btn-success" onclick="requestNotificationPermission()">
              Request Notification Permission
            </button>
          </div>
        </div>

        <div class="settings-section">
          <div class="settings-section-title">API</div>
          <div class="settings-row">
            <div class="settings-label">Swagger UI</div>
            <div class="settings-value">
              <a href="/api/docs" target="_blank" style="color:var(--accent2);">/api/docs</a>
            </div>
          </div>
          <div class="settings-row">
            <div class="settings-label">OpenAPI Spec</div>
            <div class="settings-value">
              <a href="/api/openapi.yaml" target="_blank" style="color:var(--accent2);">/api/openapi.yaml</a>
            </div>
          </div>
        </div>

        <div class="settings-section">
          <div class="settings-section-title">Backend Status</div>
          <div id="configStatus" style="color:var(--text2);font-size:13px;padding:8px 0;">Loading…</div>
          <div class="settings-row">
            <button class="btn-secondary" onclick="loadConfigStatus()">Refresh</button>
          </div>
        </div>

        <div class="settings-section">
          <div class="settings-section-title">Remote Servers</div>
          <div id="serverStatus" style="color:var(--text2);font-size:13px;padding:8px 0;">Loading…</div>
          <div class="settings-row">
            <button class="btn-secondary" onclick="loadServers()">Refresh</button>
          </div>
        </div>

        <div class="settings-section">
          <div class="settings-section-title">Saved Commands</div>
          <div id="savedCmdsList"><div style="color:var(--text2);font-size:13px;">Loading…</div></div>
          <details class="create-form-details">
            <summary class="create-form-summary">+ Add Command</summary>
            <div class="create-form">
              <input id="newCmdName" class="form-input" type="text" placeholder="Name (e.g. approve)" autocomplete="off" />
              <input id="newCmdValue" class="form-input" type="text" placeholder="Command text (e.g. y)" autocomplete="off" />
              <button class="btn-primary" style="margin-top:6px;" onclick="createSavedCmd()">Save Command</button>
            </div>
          </details>
        </div>

        <div class="settings-section">
          <div class="settings-section-title">Output Filters</div>
          <div id="filtersList"><div style="color:var(--text2);font-size:13px;">Loading…</div></div>
          <details class="create-form-details">
            <summary class="create-form-summary">+ Add Filter</summary>
            <div class="create-form">
              <input id="newFilterPattern" class="form-input" type="text" placeholder="Regex pattern (e.g. DATAWATCH_RATE_LIMITED)" autocomplete="off" />
              <select id="newFilterAction" class="form-select">
                <option value="send_input">send_input — send text to session</option>
                <option value="kill">kill — terminate session</option>
                <option value="notify">notify — send notification</option>
                <option value="log">log — log line only</option>
              </select>
              <input id="newFilterValue" class="form-input" type="text" placeholder="Value (optional, e.g. y)" autocomplete="off" />
              <button class="btn-primary" style="margin-top:6px;" onclick="createFilter()">Save Filter</button>
            </div>
          </details>
        </div>

        <div class="settings-section">
          <div class="settings-section-title">About</div>
          <div class="settings-row">
            <div class="settings-label">datawatch PWA</div>
            <div class="settings-value">Real-time session management via WebSocket</div>
          </div>
          <div class="settings-row">
            <div class="settings-label">Sessions</div>
            <div class="settings-value">
              <button class="btn-link" onclick="navigate('sessions');state.showHistory=true;renderSessionsView();">
                ${state.sessions.length} in store
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>`;

  // Load link status, config status, servers, saved commands, and filters asynchronously
  loadLinkStatus();
  loadConfigStatus();
  loadServers();
  loadSavedCommands();
  loadFilters();
}

// ── Config / Backend Status ────────────────────────────────────────────────────

function tokenHeader() {
  const t = localStorage.getItem('cs_token') || '';
  return t ? { 'Authorization': 'Bearer ' + t } : {};
}

function loadConfigStatus() {
  const el = document.getElementById('configStatus');
  if (!el) return;
  fetch('/api/config', { headers: tokenHeader() })
    .then(r => r.json())
    .then(cfg => {
      const services = ['telegram', 'discord', 'slack', 'matrix', 'ntfy', 'email', 'twilio', 'github_webhook', 'webhook'];
      el.innerHTML = services.map(svc => {
        const s = cfg[svc] || {};
        const on = s.enabled;
        return `<div class="settings-row" style="padding:4px 0;">
          <div class="settings-label" style="text-transform:capitalize;">${escHtml(svc.replace('_', ' '))}</div>
          <span class="state state-${on ? 'running' : 'failed'}" style="font-size:11px;">${on ? 'enabled' : 'disabled'}</span>
          <button class="btn-icon" title="${on ? 'Disable' : 'Enable'}" onclick="toggleBackend('${svc}',${!on})" style="margin-left:8px;">${on ? '⏸' : '▶'}</button>
        </div>`;
      }).join('');
    })
    .catch(() => { const el2 = document.getElementById('configStatus'); if (el2) el2.textContent = 'Config unavailable'; });
}

function toggleBackend(service, enable) {
  fetch('/api/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', ...tokenHeader() },
    body: JSON.stringify({ [service + '.enabled']: enable }),
  })
    .then(r => r.ok ? loadConfigStatus() : showToast('Save failed', 'error'))
    .catch(() => showToast('Save failed', 'error'));
}

// ── Remote Servers ─────────────────────────────────────────────────────────────

function loadServers() {
  const el = document.getElementById('serverStatus');
  if (!el) return;
  fetch('/api/servers', { headers: tokenHeader() })
    .then(r => r.ok ? r.json() : null)
    .then(servers => {
      if (!servers) { el.textContent = 'Servers unavailable'; return; }
      state.servers = servers;
      if (servers.length === 0) { el.textContent = 'No remote servers configured.'; return; }
      const rows = servers.map(sv => {
        const auth = sv.has_auth ? '🔒' : '🔓';
        const active = state.activeServer === sv.name ? ' (active)' : '';
        return `<div class="settings-row" style="justify-content:space-between">
          <div><strong>${escHtml(sv.name)}</strong>${escHtml(active)} ${auth}<br><span style="font-size:12px;color:var(--text2)">${escHtml(sv.url)}</span></div>
          <button class="btn-secondary" style="font-size:12px;padding:4px 8px" onclick="selectServer('${escHtml(sv.name)}')">${state.activeServer === sv.name ? 'Selected' : 'Select'}</button>
        </div>`;
      }).join('');
      el.innerHTML = rows;
    })
    .catch(() => { if (el) el.textContent = 'Servers unavailable'; });
}

function selectServer(name) {
  state.activeServer = (state.activeServer === name) ? null : name;
  loadServers();
  showToast(state.activeServer ? `Viewing server: ${state.activeServer}` : 'Viewing local server', 'info');
}

// ── Signal Device Linking ──────────────────────────────────────────────────────

function loadLinkStatus() {
  const el = document.getElementById('linkStatusText');
  if (!el) return;
  fetch('/api/link/status', { headers: tokenHeader() })
    .then(r => r.json())
    .then(data => {
      if (!el) return;
      if (data.linked) {
        el.textContent = 'Linked' + (data.account_number ? ' (' + data.account_number + ')' : '');
        const row = document.getElementById('linkActionRow');
        if (row) row.style.display = 'none';
      } else {
        el.textContent = 'Not linked';
      }
    })
    .catch(() => {
      if (el) el.textContent = 'Unknown';
    });
}

function startLinking() {
  const btn = document.querySelector('#linkActionRow button');
  if (btn) { btn.disabled = true; btn.textContent = 'Starting…'; }

  fetch('/api/link/start', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...tokenHeader() },
    body: JSON.stringify({ device_name: '' }),
  })
    .then(r => r.json())
    .then(data => {
      if (!data.stream_id) {
        showToast('Failed to start linking', 'error');
        if (btn) { btn.disabled = false; btn.textContent = 'Start Linking'; }
        return;
      }
      showToast('Linking started — waiting for QR code…', 'info', 5000);
      streamLinkEvents(data.stream_id);
    })
    .catch(err => {
      showToast('Error: ' + err.message, 'error');
      if (btn) { btn.disabled = false; btn.textContent = 'Start Linking'; }
    });
}

function streamLinkEvents(streamId) {
  const evtSource = new EventSource('/api/link/stream?id=' + encodeURIComponent(streamId) + (state.token ? '&token=' + encodeURIComponent(state.token) : ''));

  evtSource.addEventListener('qr', function(e) {
    const qrRow = document.getElementById('linkQrRow');
    const qrDiv = document.getElementById('linkQrCode');
    if (!qrRow || !qrDiv) return;

    qrRow.style.display = 'block';
    qrDiv.innerHTML = '';

    // Render QR code using qrcode.js library
    if (typeof QRCode !== 'undefined') {
      new QRCode(qrDiv, {
        text: e.data,
        width: 220,
        height: 220,
        colorDark: '#000000',
        colorLight: '#ffffff',
        correctLevel: QRCode.CorrectLevel.M,
      });
    } else {
      // Fallback: show the URI as text
      qrDiv.style.background = '#fff';
      qrDiv.style.color = '#000';
      qrDiv.style.fontSize = '10px';
      qrDiv.style.wordBreak = 'break-all';
      qrDiv.style.padding = '8px';
      qrDiv.textContent = e.data;
    }
  });

  evtSource.addEventListener('linked', function() {
    evtSource.close();
    const qrRow = document.getElementById('linkQrRow');
    if (qrRow) qrRow.style.display = 'none';
    showToast('Device linked successfully!', 'success', 5000);
    loadLinkStatus();
  });

  evtSource.addEventListener('error', function(e) {
    evtSource.close();
    const qrRow = document.getElementById('linkQrRow');
    if (qrRow) qrRow.style.display = 'none';
    showToast('Linking error: ' + (e.data || 'unknown error'), 'error', 5000);
    const btn = document.querySelector('#linkActionRow button');
    if (btn) { btn.disabled = false; btn.textContent = 'Retry Linking'; }
  });

  evtSource.onerror = function() {
    evtSource.close();
  };
}

function saveToken() {
  const input = document.getElementById('tokenInput');
  if (!input) return;
  const token = input.value.trim();
  state.token = token;
  localStorage.setItem('cs_token', token);
  showToast('Token saved. Reconnecting…', 'success', 2000);
  disconnect();
  setTimeout(connect, 500);
}

// ── Notifications ─────────────────────────────────────────────────────────────
function requestNotificationPermission() {
  if (!('Notification' in window)) {
    showToast('Notifications not supported in this browser', 'error');
    return;
  }
  Notification.requestPermission().then(permission => {
    state.notifPermission = permission;
    if (permission === 'granted') {
      showToast('Notifications enabled!', 'success');
    } else {
      showToast('Notification permission denied', 'error');
    }
    if (state.activeView === 'settings') {
      renderSettingsView();
    }
  });
}

// ── Toast notifications ───────────────────────────────────────────────────────
function showToast(message, type = 'info', duration = 3500) {
  let container = document.querySelector('.toast-container');
  if (!container) {
    container = document.createElement('div');
    container.className = 'toast-container';
    document.body.appendChild(container);
  }

  const toast = document.createElement('div');
  toast.className = `toast${type === 'error' ? ' toast-error' : type === 'success' ? ' toast-success' : ''}`;
  toast.textContent = message;
  container.appendChild(toast);

  setTimeout(() => {
    toast.style.opacity = '0';
    toast.style.transition = 'opacity 0.3s ease';
    setTimeout(() => toast.remove(), 300);
  }, duration);
}

// ── Status dot ────────────────────────────────────────────────────────────────
function updateStatusDot() {
  const dot = document.getElementById('statusDot');
  if (dot) {
    dot.classList.toggle('connected', state.connected);
  }
}

// ── Utility functions ─────────────────────────────────────────────────────────
function timeAgo(ts) {
  if (!ts) return '';
  const d = new Date(ts);
  const now = new Date();
  const secs = Math.floor((now - d) / 1000);
  if (secs < 5) return 'just now';
  if (secs < 60) return `${secs}s ago`;
  const mins = Math.floor(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return `${Math.floor(hrs / 24)}d ago`;
}

function escHtml(str) {
  if (str === null || str === undefined) return '';
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

// ── Service Worker ────────────────────────────────────────────────────────────
function registerServiceWorker() {
  if ('serviceWorker' in navigator) {
    navigator.serviceWorker.register('/sw.js').then(reg => {
      console.log('SW registered:', reg.scope);
    }).catch(err => {
      console.warn('SW registration failed:', err);
    });
  }
}

// ── Alerts view ───────────────────────────────────────────────────────────────

function updateAlertBadge() {
  const badge = document.getElementById('alertBadge');
  if (!badge) return;
  if (state.alertUnread > 0) {
    badge.textContent = state.alertUnread > 99 ? '99+' : String(state.alertUnread);
    badge.style.display = 'inline';
  } else {
    badge.style.display = 'none';
  }
}

function renderAlertsView() {
  const view = document.getElementById('view');
  if (!view) return;
  view.innerHTML = `<div class="view-content"><div id="alertsList" style="padding:12px;"><div class="spinner" style="text-align:center;padding:32px;">Loading…</div></div></div>`;
  fetch('/api/alerts', { headers: tokenHeader() })
    .then(r => r.ok ? r.json() : null)
    .then(data => {
      const el = document.getElementById('alertsList');
      if (!el) return;
      if (!data || !data.alerts || data.alerts.length === 0) {
        el.innerHTML = '<div style="text-align:center;color:var(--text2);padding:32px;">No alerts.</div>';
        return;
      }
      // Mark all read
      state.alertUnread = 0;
      updateAlertBadge();
      fetch('/api/alerts', { method: 'POST', headers: { 'Content-Type': 'application/json', ...tokenHeader() }, body: JSON.stringify({ all: true }) });

      el.innerHTML = `<div style="display:flex;justify-content:flex-end;margin-bottom:8px;">
        <button class="btn-secondary" style="font-size:12px;" onclick="renderAlertsView()">Refresh</button>
      </div>` + data.alerts.map(a => {
        const levelColor = a.level === 'error' ? 'var(--error)' : a.level === 'warn' ? 'var(--warning, #f59e0b)' : 'var(--text2)';
        const sessLink = a.session_id ? `<span style="font-size:11px;color:var(--text2);margin-left:8px;">[${escHtml(a.session_id)}]</span>` : '';
        return `<div class="card" style="margin-bottom:8px;border-left:3px solid ${levelColor};${a.read ? 'opacity:0.6' : ''}">
          <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:4px;">
            <strong style="color:${levelColor};">${escHtml(a.level.toUpperCase())}</strong>${sessLink}
            <span style="font-size:11px;color:var(--text2);">${timeAgo(a.created_at)}</span>
          </div>
          <div style="font-weight:500;">${escHtml(a.title)}</div>
          <div style="font-size:13px;color:var(--text2);margin-top:4px;">${escHtml(a.body)}</div>
        </div>`;
      }).join('');
    })
    .catch(() => {
      const el = document.getElementById('alertsList');
      if (el) el.innerHTML = '<div style="color:var(--error);padding:16px;">Failed to load alerts.</div>';
    });
}

// ── Saved Commands (in Settings) ───────────────────────────────────────────────

function loadSavedCommands() {
  const el = document.getElementById('savedCmdsList');
  if (!el) return;
  fetch('/api/commands', { headers: tokenHeader() })
    .then(r => r.ok ? r.json() : [])
    .then(cmds => {
      if (!cmds || cmds.length === 0) {
        el.innerHTML = '<div style="color:var(--text2);font-size:13px;">No saved commands. Run <code>datawatch seed</code> to populate defaults.</div>';
        return;
      }
      el.innerHTML = cmds.map(c => `<div class="settings-row" style="justify-content:space-between;padding:4px 0;">
        <div>
          <strong>${escHtml(c.name)}</strong>
          <span style="font-size:12px;color:var(--text2);margin-left:8px;">${escHtml(c.command)}</span>
          ${c.seeded ? '<span style="font-size:10px;color:var(--text2);margin-left:4px;">(seeded)</span>' : ''}
        </div>
        ${!c.seeded ? `<button class="btn-icon" title="Delete" onclick="deleteSavedCmd('${escHtml(c.name)}')">✕</button>` : ''}
      </div>`).join('');
    })
    .catch(() => { el.innerHTML = '<div style="color:var(--error);font-size:13px;">Failed to load commands.</div>'; });
}

function deleteSavedCmd(name) {
  fetch('/api/commands?name=' + encodeURIComponent(name), { method: 'DELETE', headers: tokenHeader() })
    .then(r => r.ok ? loadSavedCommands() : showToast('Delete failed', 'error'))
    .catch(() => showToast('Delete failed', 'error'));
}

function createSavedCmd() {
  const name = (document.getElementById('newCmdName') || {}).value || '';
  const command = (document.getElementById('newCmdValue') || {}).value || '';
  if (!name || !command) { showToast('Name and command required', 'error'); return; }
  fetch('/api/commands', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...tokenHeader() },
    body: JSON.stringify({ name, command }),
  })
    .then(r => {
      if (r.ok) {
        document.getElementById('newCmdName').value = '';
        document.getElementById('newCmdValue').value = '';
        loadSavedCommands();
        showToast('Command saved', 'success', 2000);
      } else {
        r.text().then(t => showToast(t || 'Save failed', 'error'));
      }
    })
    .catch(() => showToast('Save failed', 'error'));
}

// ── Filters (in Settings) ─────────────────────────────────────────────────────

function loadFilters() {
  const el = document.getElementById('filtersList');
  if (!el) return;
  fetch('/api/filters', { headers: tokenHeader() })
    .then(r => r.ok ? r.json() : [])
    .then(filters => {
      if (!filters || filters.length === 0) {
        el.innerHTML = '<div style="color:var(--text2);font-size:13px;">No filters. Run <code>datawatch seed</code> to populate defaults.</div>';
        return;
      }
      el.innerHTML = filters.map(f => `<div class="settings-row" style="flex-direction:column;align-items:flex-start;padding:6px 0;border-bottom:1px solid var(--border,rgba(255,255,255,0.07));">
        <div style="display:flex;align-items:center;justify-content:space-between;width:100%;">
          <span class="state state-${f.enabled ? 'running' : 'failed'}" style="font-size:10px;">${f.enabled ? 'on' : 'off'}</span>
          <span style="font-size:12px;color:var(--text2);margin-left:8px;flex:1;">${escHtml(f.action)}</span>
          <button class="btn-icon" title="${f.enabled ? 'Disable' : 'Enable'}" onclick="toggleFilter('${escHtml(f.id)}',${!f.enabled})">${f.enabled ? '⏸' : '▶'}</button>
          <button class="btn-icon" title="Delete" onclick="deleteFilter('${escHtml(f.id)}')">✕</button>
        </div>
        <code style="font-size:11px;color:var(--text2);margin-top:2px;">${escHtml(f.pattern)}</code>
        ${f.value ? `<div style="font-size:11px;color:var(--text2);">→ ${escHtml(f.value)}</div>` : ''}
      </div>`).join('');
    })
    .catch(() => { el.innerHTML = '<div style="color:var(--error);font-size:13px;">Failed to load filters.</div>'; });
}

function toggleFilter(id, enable) {
  fetch('/api/filters', { method: 'PATCH', headers: { 'Content-Type': 'application/json', ...tokenHeader() }, body: JSON.stringify({ id, enabled: enable }) })
    .then(r => r.ok ? loadFilters() : showToast('Update failed', 'error'))
    .catch(() => showToast('Update failed', 'error'));
}

function deleteFilter(id) {
  fetch('/api/filters?id=' + encodeURIComponent(id), { method: 'DELETE', headers: tokenHeader() })
    .then(r => r.ok ? loadFilters() : showToast('Delete failed', 'error'))
    .catch(() => showToast('Delete failed', 'error'));
}

function createFilter() {
  const pattern = (document.getElementById('newFilterPattern') || {}).value || '';
  const action = (document.getElementById('newFilterAction') || {}).value || '';
  const value = (document.getElementById('newFilterValue') || {}).value || '';
  if (!pattern || !action) { showToast('Pattern and action required', 'error'); return; }
  fetch('/api/filters', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...tokenHeader() },
    body: JSON.stringify({ pattern, action, value }),
  })
    .then(r => {
      if (r.ok) {
        document.getElementById('newFilterPattern').value = '';
        document.getElementById('newFilterValue').value = '';
        loadFilters();
        showToast('Filter saved', 'success', 2000);
      } else {
        r.text().then(t => showToast(t || 'Save failed', 'error'));
      }
    })
    .catch(() => showToast('Save failed', 'error'));
}

// ── Back button ──────────────────────────────────────────────────────────────
document.addEventListener('DOMContentLoaded', () => {
  const backBtn = document.getElementById('backBtn');
  if (backBtn) {
    backBtn.addEventListener('click', () => {
      // Require two presses within 2s to leave a session detail view
      if (state.activeView === 'session-detail') {
        const sess = state.sessions.find(s => s.full_id === state.activeSession);
        const isActive = sess && !DONE_STATES.has(sess.state);
        if (isActive) {
          if (state.backPressCount === 0) {
            state.backPressCount = 1;
            showToast('Session is running. Press Back again to leave.', 'warn', 2500);
            clearTimeout(state.backPressTimer);
            state.backPressTimer = setTimeout(() => { state.backPressCount = 0; }, 2500);
            return;
          }
          state.backPressCount = 0;
          clearTimeout(state.backPressTimer);
        }
      }
      navigate('sessions');
    });
  }

  registerServiceWorker();
  connect();
  navigate('sessions');

  // Load initial unread alert count
  fetch('/api/alerts', { headers: tokenHeader() })
    .then(r => r.ok ? r.json() : null)
    .then(data => { if (data) { state.alertUnread = data.unread_count || 0; updateAlertBadge(); } })
    .catch(() => {});

  // Periodically refresh time-ago labels while on sessions view
  setInterval(() => {
    if (state.activeView === 'sessions') {
      renderSessionsView();
    }
  }, 30000);
});

// Make navigate global so onclick attributes work
window.navigate = navigate;
window.submitNewSession = submitNewSession;
window.sendSessionInput = sendSessionInput;
window.saveToken = saveToken;
window.requestNotificationPermission = requestNotificationPermission;
window.startLinking = startLinking;
window.renderAlertsView = renderAlertsView;
window.deleteSavedCmd = deleteSavedCmd;
window.toggleFilter = toggleFilter;
window.deleteFilter = deleteFilter;
window.killSession = killSession;
window.restartSession = restartSession;
window.moveSession = moveSession;
window.sendQuickInput = sendQuickInput;
window.renameSession = renameSession;
window.openDirBrowser = openDirBrowser;
window.dirEntryClick = dirEntryClick;
window.dirNavigate = dirNavigate;
window.selectDir = selectDir;
window.toggleHistory = toggleHistory;
window.createSavedCmd = createSavedCmd;
window.createFilter = createFilter;
