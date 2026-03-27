// ── State ──────────────────────────────────────────────────────────────────
const state = {
  connected: false,
  sessions: [],
  activeView: 'sessions', // sessions | new | settings | session-detail
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
  }
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
    }
  }
}

// ── Session list view ─────────────────────────────────────────────────────────
function renderSessionsView() {
  const view = document.getElementById('view');
  if (state.activeView !== 'sessions') return;

  // Sort by manual order first, then by updated_at for unordered sessions
  const ordered = sortSessionsByOrder(state.sessions);

  if (ordered.length === 0) {
    view.innerHTML = `
      <div class="view-content">
        <div class="empty-state">
          <span class="empty-state-icon">⚡</span>
          <h3>No sessions yet</h3>
          <p>Tap <strong>New</strong> below to start a claude-code session,<br>or send commands via Signal.</p>
        </div>
      </div>`;
    return;
  }

  const cards = ordered.map((sess, idx) => sessionCard(sess, idx, ordered.length)).join('');
  view.innerHTML = `<div class="view-content"><div class="session-list">${cards}</div></div>`;
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

  view.innerHTML = `
    <div class="session-detail">
      <div class="session-info-bar">
        <div class="task-text" title="${escHtml(taskText)}">${escHtml(displayTitle)}</div>
        <div class="meta">
          <span class="id">${escHtml(shortId)}</span>
          ${backendText ? `<span class="backend-badge">${escHtml(backendText)}</span>` : ''}
          <span class="state detail-state-badge ${badgeClass}">${escHtml(stateText)}</span>
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
      <div class="input-bar${isWaiting ? ' needs-input' : ''}">
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
        <button class="send-btn" onclick="sendSessionInput()">➤</button>
      </div>
    </div>`;

  // Scroll output to bottom
  const outputArea = document.getElementById('outputArea');
  if (outputArea) {
    outputArea.scrollTop = outputArea.scrollHeight;
  }

  // Allow Enter key to send
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
        <button class="btn-primary" onclick="submitNewSession()">Start Session</button>
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

  // Load backends
  fetchBackends();
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
  fetch('/api/files?path=' + encodeURIComponent(path || '~'), { headers })
    .then(r => r.json())
    .then(data => {
      const content = document.getElementById('dirBrowserContent');
      if (!content) return;
      content.innerHTML = `<div class="dir-current">${escHtml(data.path)}</div>` +
        (data.entries || []).filter(e => e.is_dir).map(e =>
          `<div class="dir-entry" onclick="dirEntryClick(${JSON.stringify(e.path)}, ${e.is_dir})">
            <span class="dir-icon">${e.is_dir ? '📁' : '📄'}</span>
            <span>${escHtml(e.name)}</span>
          </div>`
        ).join('');
    })
    .catch(err => {
      const content = document.getElementById('dirBrowserContent');
      if (content) content.innerHTML = '<div class="dir-error">Cannot read directory</div>';
    });
}

function dirEntryClick(path, isDir) {
  if (!isDir) return;
  if (path.endsWith('/..') || path === '..') {
    // navigate up - handled by server returning parent path
  }
  loadDirContents(path);
  // Double-click or explicit select: set as project dir
  newSessionState.selectedDir = path;
  const display = document.getElementById('selectedDirDisplay');
  if (display) display.textContent = path;
}

function submitNewSession() {
  const taskInput = document.getElementById('taskInput');
  const nameInput = document.getElementById('sessionNameInput');
  const backendSel = document.getElementById('backendSelect');
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
  };

  const ok = send('new_session', payload);
  if (ok) {
    taskInput.value = '';
    if (nameInput) nameInput.value = '';
    newSessionState.selectedDir = '';
    const browser = document.getElementById('dirBrowser');
    if (browser) browser.style.display = 'none';
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
          <div class="settings-section-title">About</div>
          <div class="settings-row">
            <div class="settings-label">datawatch PWA</div>
            <div class="settings-value">Real-time session management via WebSocket</div>
          </div>
          <div class="settings-row">
            <div class="settings-label">Sessions</div>
            <div class="settings-value">${state.sessions.length} in store</div>
          </div>
        </div>
      </div>
    </div>`;

  // Load link status, config status, and servers asynchronously
  loadLinkStatus();
  loadConfigStatus();
  loadServers();
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

// ── Back button ──────────────────────────────────────────────────────────────
document.addEventListener('DOMContentLoaded', () => {
  const backBtn = document.getElementById('backBtn');
  if (backBtn) {
    backBtn.addEventListener('click', () => navigate('sessions'));
  }

  registerServiceWorker();
  connect();
  navigate('sessions');

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
