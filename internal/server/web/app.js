// ── State ──────────────────────────────────────────────────────────────────
const state = {
  connected: false,
  sessions: [],
  activeView: 'sessions', // sessions | new | settings | session-detail | alerts
  activeSession: null,    // session FullID being viewed
  activeOutputTab: 'tmux', // which output tab is active: 'tmux' | 'channel'
  ws: null,
  reconnectDelay: 1000,
  reconnectTimer: null,
  token: localStorage.getItem('cs_token') || '',
  outputBuffer: {},       // sessionId -> string[]
  channelReplies: {},     // sessionId -> [{text, ts}]
  notifPermission: Notification.permission,
  sessionOrder: JSON.parse(localStorage.getItem('cs_session_order') || '[]'), // manual ordering
  servers: [],            // remote server list from /api/servers
  activeServer: null,     // selected server name (null = local)
  alertUnread: 0,         // unread alert count for badge
  showHistory: false,     // show completed/killed/failed sessions in main list
  backPressCount: 0,      // for double-back-press confirmation
  backPressTimer: null,
};

// Returns the communication mode for a session: 'acp' | 'channel' | 'tmux'
function getSessionMode(backend) {
  if (backend === 'opencode-acp') return 'acp';
  if (backend === 'claude' || backend === 'claude-code') return 'channel';
  return 'tmux';
}

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

// apiFetch wraps fetch() with auth header and JSON response parsing.
// Rejects with an Error whose message is the server error text on non-2xx.
function apiFetch(path, opts = {}) {
  const token = localStorage.getItem('cs_token') || '';
  const headers = Object.assign({ 'Content-Type': 'application/json' }, opts.headers || {});
  if (token) headers['Authorization'] = 'Bearer ' + token;
  return fetch(path, Object.assign({}, opts, { headers }))
    .then(r => r.ok ? r.json() : r.text().then(t => Promise.reject(new Error(t || r.statusText))));
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
    case 'channel_reply':
      if (msg.data) {
        handleChannelReply(msg.data);
      }
      break;
    case 'channel_notify':
      if (msg.data && msg.data.text) {
        showToast(`Channel: ${msg.data.text.slice(0, 80)}`, 'info', 4000);
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

function handleChannelReply(data) {
  const { text, session_id } = data;
  if (!session_id) return;
  if (!state.channelReplies[session_id]) state.channelReplies[session_id] = [];
  state.channelReplies[session_id].push({ text, ts: new Date().toISOString() });
  // Keep last 50 channel replies per session
  if (state.channelReplies[session_id].length > 50) {
    state.channelReplies[session_id] = state.channelReplies[session_id].slice(-50);
  }
  // If viewing this session's detail, append the channel reply to the channel output area
  if (state.activeView === 'session-detail' && state.activeSession === session_id) {
    const outputArea = document.getElementById('outputAreaChannel') || document.querySelector('.output-area');
    if (outputArea) {
      const wasAtBottom = outputArea.scrollHeight - outputArea.scrollTop <= outputArea.clientHeight + 40;
      const div = document.createElement('div');
      div.className = 'channel-reply-line new-line';
      div.textContent = text;
      outputArea.appendChild(div);
      if (wasAtBottom) outputArea.scrollTop = outputArea.scrollHeight;
    }
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

  // If currently viewing this session, append to tmux output area
  if (state.activeView === 'session-detail' && state.activeSession === sessionId) {
    const outputArea = document.getElementById('outputAreaTmux') || document.querySelector('.output-area');
    if (outputArea) {
      const wasAtBottom = outputArea.scrollHeight - outputArea.scrollTop <= outputArea.clientHeight + 40;
      lines.forEach(line => {
        const div = document.createElement('div');
        div.className = 'output-line new-line';
        div.textContent = stripAnsi(line);
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
      const outputArea = document.getElementById('outputAreaTmux') || document.querySelector('.output-area');
      if (outputArea && outputArea.parentNode) {
        outputArea.parentNode.insertBefore(banner, outputArea);
      }
    }
  }

  // Show toast notification
  const sessLabel = sess ? sess.id : sessionId;
  showToast(`[${sessLabel}] needs input`, 'info', 5000);
}

// updateSessionDetailButtons refreshes the state badge, action buttons, and header name
// without re-rendering the whole view (preserves scroll position / input).
function updateSessionDetailButtons(sessionId) {
  if (state.activeView !== 'session-detail' || state.activeSession !== sessionId) return;
  const sess = state.sessions.find(s => s.full_id === sessionId);
  if (!sess) return;
  // Keep header name in sync (name may have been updated via WS)
  updateHeaderSessName(sessionId);
  const stateText = sess.state || 'unknown';
  const isActive = stateText === 'running' || stateText === 'waiting_input' || stateText === 'rate_limited';
  const isDone = stateText === 'complete' || stateText === 'failed' || stateText === 'killed';
  const badge = document.querySelector('.detail-state-badge');
  if (badge) {
    badge.textContent = stateText;
    badge.className = `state detail-state-badge state-badge-${stateText}`;
  }
  const btnContainer = document.getElementById('actionBtns');
  if (btnContainer) {
    btnContainer.innerHTML = isActive
      ? `<button class="btn-stop" onclick="killSession('${escHtml(sessionId)}')" title="Stop session">&#9632; Stop</button>`
      : isDone
      ? `<button class="btn-restart" onclick="restartSession('${escHtml(sessionId)}')" title="Restart with same task">&#8635; Restart</button>
         <button class="btn-delete" onclick="deleteSession('${escHtml(sessionId)}')" title="Delete session">&#128465; Delete</button>`
      : '';
  }
}

function onSessionsUpdated() {
  if (state.activeView === 'sessions') {
    renderSessionsView();
  } else if (state.activeView === 'session-detail' && state.activeSession) {
    updateSessionDetailButtons(state.activeSession);
  }
}

// ── Navigation ───────────────────────────────────────────────────────────────
function navigate(view, sessionId, fromPopstate) {
  // Push a history entry so Android's back button fires popstate
  if (!fromPopstate) {
    history.pushState({ view, sessionId: sessionId || null }, '');
  }

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
    state.activeOutputTab = 'tmux';
    backBtn.style.display = 'inline';
    nav.style.display = 'none';
    updateHeaderSessName(sessionId);
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

// Handle Android/browser back button via popstate
window.addEventListener('popstate', function(e) {
  const st = e.state;
  if (!st) {
    // No state — navigated past app entry; go to sessions
    navigate('sessions', null, true);
    return;
  }
  const { view, sessionId } = st;
  // Intercept back from active session — require double-press
  if (state.activeView === 'session-detail' && state.activeSession) {
    const sess = state.sessions.find(s => s.full_id === state.activeSession);
    const isActive = sess && (sess.state === 'running' || sess.state === 'waiting_input' || sess.state === 'rate_limited');
    if (isActive) {
      if (state.backPressCount === 0) {
        state.backPressCount = 1;
        clearTimeout(state.backPressTimer);
        state.backPressTimer = setTimeout(() => { state.backPressCount = 0; }, 2500);
        showToast('Press back again to leave this active session', 'info', 2500);
        // Push state back so next back fires popstate again
        history.pushState({ view: state.activeView, sessionId: state.activeSession }, '');
        return;
      }
      state.backPressCount = 0;
      clearTimeout(state.backPressTimer);
    }
  }
  navigate(view || 'sessions', sessionId, true);
});

// ── Session header name helpers ───────────────────────────────────────────────
function updateHeaderSessName(sessionId) {
  const titleEl = document.getElementById('headerTitle');
  if (!titleEl) return;
  const sess = state.sessions.find(s => s.full_id === sessionId);
  const sessName = sess ? (sess.name || '') : '';
  const shortId = sess ? (sess.id || (sessionId || '').split('-').pop() || '') : (sessionId || '').split('-').pop();
  const taskSnip = sess ? (sess.task || '') : '';
  const displayName = sessName || (taskSnip.length > 28 ? taskSnip.slice(0, 28) + '…' : taskSnip) || shortId;
  titleEl.innerHTML = `<span class="header-sess-name" onclick="startHeaderRename('${escHtml(sessionId)}')" title="Click to rename">${escHtml(displayName)}</span><span class="header-id">#${escHtml(shortId)}</span><button class="btn-icon header-edit-btn" onclick="startHeaderRename('${escHtml(sessionId)}')" title="Rename">✎</button>`;
}

function startHeaderRename(sessionId) {
  const titleEl = document.getElementById('headerTitle');
  if (!titleEl) return;
  const sess = state.sessions.find(s => s.full_id === sessionId);
  const currentName = sess ? (sess.name || '') : '';
  titleEl.innerHTML = `<input type="text" id="headerRenameInput" class="header-rename-input" value="${escHtml(currentName)}" placeholder="Session name…" /><button class="btn-icon" onclick="confirmHeaderRename('${escHtml(sessionId)}')">✓</button><button class="btn-icon" onclick="cancelHeaderRename('${escHtml(sessionId)}')">✕</button>`;
  const input = document.getElementById('headerRenameInput');
  if (input) {
    input.focus();
    input.select();
    input.addEventListener('keydown', e => {
      if (e.key === 'Enter') confirmHeaderRename(sessionId);
      if (e.key === 'Escape') cancelHeaderRename(sessionId);
    });
  }
}

function confirmHeaderRename(sessionId) {
  const input = document.getElementById('headerRenameInput');
  if (!input) return;
  const name = input.value.trim();
  apiFetch('/api/sessions/rename', { method: 'POST', body: JSON.stringify({ id: sessionId, name }) })
    .then(() => {
      const sess = state.sessions.find(s => s.full_id === sessionId);
      if (sess) sess.name = name;
      updateHeaderSessName(sessionId);
      showToast('Session renamed', 'success', 2000);
    })
    .catch(e => showToast('Rename failed: ' + e.message, 'error'));
}

function cancelHeaderRename(sessionId) {
  updateHeaderSessName(sessionId);
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

let dragSrcId = null;

function sessionDragStart(ev, fullId) {
  dragSrcId = fullId;
  ev.dataTransfer.effectAllowed = 'move';
  ev.currentTarget.classList.add('dragging');
}

function sessionDragOver(ev) {
  ev.preventDefault();
  ev.dataTransfer.dropEffect = 'move';
  ev.currentTarget.classList.add('drag-over');
}

function sessionDrop(ev, targetId) {
  ev.preventDefault();
  ev.currentTarget.classList.remove('drag-over');
  if (!dragSrcId || dragSrcId === targetId) return;
  const order = sortSessionsByOrder(state.sessions).map(s => s.full_id || s.id);
  const srcIdx = order.indexOf(dragSrcId);
  const tgtIdx = order.indexOf(targetId);
  if (srcIdx < 0 || tgtIdx < 0) return;
  order.splice(srcIdx, 1);
  order.splice(tgtIdx, 0, dragSrcId);
  state.sessionOrder = order;
  localStorage.setItem('cs_session_order', JSON.stringify(order));
  renderSessionsView();
}

function sessionDragEnd(ev) {
  ev.currentTarget.classList.remove('dragging');
  document.querySelectorAll('.drag-over').forEach(el => el.classList.remove('drag-over'));
  dragSrcId = null;
}

function sessionCard(sess, idx, total) {
  const stateClass = `state-${sess.state}`;
  const badgeClass = `state-badge-${sess.state}`;
  const ago = timeAgo(sess.updated_at);
  const displayText = sess.name || sess.task || '';
  const taskText = displayText.length > 80 ? displayText.slice(0, 80) + '…' : (displayText || '(no task)');
  const shortId = sess.id || (sess.full_id || '').split('-').pop() || '????';
  const hostname = sess.hostname || '';
  const fullId = sess.full_id || sess.id || '';
  const backend = sess.llm_backend || '';
  const mode = getSessionMode(backend);
  const canUp = idx > 0;
  const canDown = idx < total - 1;

  return `
    <div class="session-card ${stateClass}" draggable="true" data-full-id="${escHtml(fullId)}"
         ondragstart="sessionDragStart(event,'${escHtml(fullId)}')"
         ondragover="sessionDragOver(event)"
         ondrop="sessionDrop(event,'${escHtml(fullId)}')"
         ondragend="sessionDragEnd(event)">
      <span class="drag-handle" title="Drag to reorder">⠿</span>
      <div class="session-card-header" onclick="navigate('session-detail', '${escHtml(fullId)}')">
        <span class="id">${escHtml(shortId)}</span>
        <span class="state ${badgeClass}">${escHtml(sess.state || 'unknown')}</span>
        ${backend ? `<span class="mode-badge mode-${mode}" title="${escHtml(backend)}">${mode}</span>` : ''}
        <span class="time">${escHtml(ago)}</span>
      </div>
      <div class="task" onclick="navigate('session-detail', '${escHtml(fullId)}')">${escHtml(taskText)}</div>
      ${hostname ? `<div class="hostname">${escHtml(hostname)}</div>` : ''}
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

  // Build output buffers — tmux and channel are kept separate
  const lines = state.outputBuffer[sessionId] || [];
  const replies = state.channelReplies[sessionId] || [];
  const tmuxHtml = lines.map(l => `<div class="output-line">${escHtml(stripAnsi(l))}</div>`).join('');
  const channelHtml = replies.map(r => `<div class="channel-reply-line">${escHtml(r.text)}</div>`).join('');

  const needsBanner = isWaiting
    ? `<div class="needs-input-banner">Waiting for input${sess && sess.last_prompt ? ': ' + escHtml(sess.last_prompt.slice(0, 100)) : ''}</div>`
    : '';

  const nameText = sess ? (sess.name || '') : '';
  const displayTitle = nameText || taskText || '(no task)';
  const backendText = sess ? (sess.llm_backend || '') : '';
  const projectDir = sess ? (sess.project_dir || '') : '';
  const sessionMode = getSessionMode(backendText);
  const isActive = stateText === 'running' || stateText === 'waiting_input' || stateText === 'rate_limited';
  const isDone = stateText === 'complete' || stateText === 'failed' || stateText === 'killed';

  const actionButtons = isActive
    ? `<button class="btn-stop" onclick="killSession('${escHtml(sessionId)}')" title="Stop session">&#9632; Stop</button>`
    : isDone
    ? `<button class="btn-restart" onclick="restartSession('${escHtml(sessionId)}')" title="Restart with same task">&#8635; Restart</button>
       <button class="btn-delete" onclick="deleteSession('${escHtml(sessionId)}')" title="Delete session">&#128465; Delete</button>`
    : '';

  // Dual output areas: tabs only shown when session uses a channel (claude-code)
  const hasChannel = sessionMode === 'channel';
  const outputAreaHtml = hasChannel
    ? `<div class="output-tabs">
        <button class="output-tab active" id="tabTmux" onclick="switchOutputTab('tmux')">Tmux</button>
        <button class="output-tab" id="tabChannel" onclick="switchOutputTab('channel')">Channel</button>
      </div>
      <div class="output-area output-area-tmux" id="outputAreaTmux">${tmuxHtml}</div>
      <div class="output-area output-area-channel" id="outputAreaChannel" style="display:none">${channelHtml}</div>`
    : `<div class="output-area output-area-tmux" id="outputAreaTmux">${tmuxHtml}</div>`;

  // For channel mode, pick the initial send button based on active tab
  const sendBtnHtml = isActive
    ? (sessionMode === 'channel' && !isWaiting
      ? `<span id="sendBtnWrap">${state.activeOutputTab === 'channel'
          ? `<button class="send-btn send-btn-channel" onclick="sendChannelMessage()" title="Send via MCP channel">&#9654; ch</button>`
          : `<button class="send-btn send-btn-tmux" onclick="sendSessionInputDirect()" title="Send via tmux">&#9654; tmux</button>`
        }</span>`
      : `<button class="send-btn" onclick="sendSessionInput()">&#9658;</button>`)
    : '';

  view.innerHTML = `
    <div class="session-detail">
      <div class="session-info-bar">
        <div class="meta">
          ${backendText ? `<span class="backend-badge">${escHtml(backendText)}</span>` : ''}
          <span class="mode-badge mode-${sessionMode}">${sessionMode}</span>
          <span class="state detail-state-badge ${badgeClass}">${escHtml(stateText)}</span>
          <span id="actionBtns">${actionButtons}</span>
          <button class="btn-icon" style="font-size:11px;margin-left:4px;" onclick="toggleSessionTimeline('${escHtml(sessionId)}')" title="Show event timeline">&#128336; Timeline</button>
        </div>
      </div>
      ${needsBanner}
      ${outputAreaHtml}
      ${isWaiting ? `<div class="quick-input-row">
        <button class="quick-btn" onclick="sendQuickInput('y')">y</button>
        <button class="quick-btn" onclick="sendQuickInput('n')">n</button>
        <button class="quick-btn" onclick="sendQuickInput('')">Enter</button>
        <button class="quick-btn quick-btn-danger" onclick="sendQuickInput('__ctrlc__')">Ctrl‑C</button>
      </div>` : ''}
      ${isActive ? `<div id="savedCmdsQuick" class="saved-cmds-quick"></div>` : ''}
      ${isActive ? `<div class="input-bar${isWaiting ? ' needs-input' : ''}">
        <div class="input-field-wrap">
          <div class="input-label" style="display:${isWaiting ? 'block' : 'none'}">Input Required</div>
          <input
            type="text"
            class="input-field"
            id="sessionInput"
            placeholder="${isWaiting ? 'Type your response…' : sessionMode === 'channel' ? 'Send message…' : 'Send command or input…'}"
            autocomplete="off"
            autocorrect="off"
            spellcheck="false"
          />
        </div>
        ${sendBtnHtml}
      </div>` : ''}
    </div>`;

  // Scroll tmux output to bottom (channel area starts empty)
  const outputArea = document.getElementById('outputAreaTmux');
  if (outputArea) {
    outputArea.scrollTop = outputArea.scrollHeight;
  }

  // Load saved commands quick panel
  if (isActive) {
    loadSavedCmdsQuick(sessionId);
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
    // Don't auto-focus on touch devices (would open soft keyboard unexpectedly)
    const isTouch = navigator.maxTouchPoints > 0 || window.matchMedia('(pointer:coarse)').matches;
    if (!isTouch) inputEl.focus();
  }
}

function toggleSessionTimeline(sessionId) {
  const existing = document.getElementById('timelinePanel');
  if (existing) { existing.remove(); return; }
  const outputArea = document.getElementById('outputAreaTmux') || document.querySelector('.output-area');
  if (!outputArea) return;
  const panel = document.createElement('div');
  panel.id = 'timelinePanel';
  panel.style.cssText = 'background:var(--surface2,#1e1e2e);border-top:1px solid var(--border);padding:12px;font-size:12px;font-family:monospace;max-height:260px;overflow-y:auto;color:var(--text2);';
  panel.innerHTML = '<div style="color:var(--text2);padding:8px 0;">Loading timeline…</div>';
  outputArea.insertAdjacentElement('afterend', panel);
  fetch('/api/sessions/timeline?id=' + encodeURIComponent(sessionId), { headers: tokenHeader() })
    .then(r => r.ok ? r.json() : null)
    .then(data => {
      if (!data || !data.lines || data.lines.length === 0) {
        panel.innerHTML = '<div style="color:var(--text2);padding:4px 0;">No timeline events recorded yet.</div>';
        return;
      }
      panel.innerHTML = '<div style="font-weight:600;margin-bottom:6px;color:var(--text1);">Timeline</div>' +
        data.lines.map(l => {
          const parts = l.split(' | ');
          const ts = parts[0] || '';
          const event = parts[1] || '';
          const detail = parts.slice(2).join(' | ');
          const eventColor = event.includes('state') ? 'var(--accent,#7c3aed)' :
            event.includes('input') ? 'var(--success,#22c55e)' :
            event.includes('rate') ? 'var(--warning,#f59e0b)' : 'var(--text2)';
          return `<div style="display:flex;gap:8px;padding:2px 0;border-bottom:1px solid var(--border,#333);">
            <span style="color:var(--text3,#666);flex-shrink:0;">${escHtml(ts.split('T')[1]?.replace('Z','') || ts)}</span>
            <span style="color:${eventColor};flex-shrink:0;width:100px;">${escHtml(event)}</span>
            <span>${escHtml(detail)}</span>
          </div>`;
        }).join('');
    })
    .catch(() => { panel.innerHTML = '<div style="color:var(--error);">Failed to load timeline.</div>'; });
}

function switchOutputTab(tab) {
  const tmuxArea = document.getElementById('outputAreaTmux');
  const channelArea = document.getElementById('outputAreaChannel');
  const tabTmux = document.getElementById('tabTmux');
  const tabChannel = document.getElementById('tabChannel');
  if (!tmuxArea || !channelArea) return;
  state.activeOutputTab = tab;
  if (tab === 'tmux') {
    tmuxArea.style.display = '';
    channelArea.style.display = 'none';
    if (tabTmux) tabTmux.classList.add('active');
    if (tabChannel) tabChannel.classList.remove('active');
    tmuxArea.scrollTop = tmuxArea.scrollHeight;
  } else {
    tmuxArea.style.display = 'none';
    channelArea.style.display = '';
    if (tabTmux) tabTmux.classList.remove('active');
    if (tabChannel) tabChannel.classList.add('active');
    channelArea.scrollTop = channelArea.scrollHeight;
  }
  // Update send button to match active tab
  const wrap = document.getElementById('sendBtnWrap');
  if (wrap) {
    if (tab === 'channel') {
      wrap.innerHTML = `<button class="send-btn send-btn-channel" onclick="sendChannelMessage()" title="Send via MCP channel">&#9654; ch</button>`;
    } else {
      wrap.innerHTML = `<button class="send-btn send-btn-tmux" onclick="sendSessionInputDirect()" title="Send via tmux">&#9654; tmux</button>`;
    }
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
    .then(r => {
      if (r.ok) {
        showToast('Session stopped', 'success', 2000);
        // Optimistic UI update — server will confirm via WebSocket state_change
        const sess = state.sessions.find(s => s.full_id === sessionId);
        if (sess) sess.state = 'killed';
        updateSessionDetailButtons(sessionId);
      } else {
        showToast('Stop failed', 'error');
      }
    })
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

// sendSessionInputDirect always routes via tmux, even in channel mode.
// Used when the user explicitly clicks the tmux send button.
function sendSessionInputDirect() {
  const inputEl = document.getElementById('sessionInput');
  if (!inputEl || !state.activeSession) return;
  const text = inputEl.value.trim();
  if (!text) return;
  send('command', { text: `send ${state.activeSession}: ${text}` });
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

function sendChannelMessage() {
  const inputEl = document.getElementById('sessionInput');
  if (!inputEl || !state.activeSession) return;
  const text = inputEl.value.trim();
  if (!text) return;
  const token = localStorage.getItem('cs_token') || '';
  const headers = { 'Content-Type': 'application/json' };
  if (token) headers['Authorization'] = 'Bearer ' + token;
  fetch('/api/channel/send', {
    method: 'POST',
    headers,
    body: JSON.stringify({ text, session_id: state.activeSession }),
  })
    .then(r => r.ok ? null : showToast('Channel send failed', 'error'))
    .catch(() => showToast('Channel send failed', 'error'));
  inputEl.value = '';
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

function loadSavedCmdsQuick(sessionId) {
  const token = localStorage.getItem('cs_token') || '';
  const headers = {};
  if (token) headers['Authorization'] = 'Bearer ' + token;
  fetch('/api/commands', { headers })
    .then(r => r.ok ? r.json() : [])
    .then(cmds => {
      const panel = document.getElementById('savedCmdsQuick');
      if (!panel || !cmds || !cmds.length) return;
      panel.innerHTML = '<span class="saved-cmds-label">Saved:</span>' +
        cmds.map(c => `<button class="quick-cmd-btn" onclick="sendSavedCmd(${JSON.stringify(c.command)})" title="${escHtml(c.command)}">${escHtml(c.name || c.command)}</button>`).join('');
    })
    .catch(() => {});
}

function sendSavedCmd(cmd) {
  if (!state.activeSession) return;
  send('command', { text: `send ${state.activeSession}: ${cmd}` });
}

function deleteSession(sessionId) {
  if (!confirm('Delete this session and its data? This cannot be undone.')) return;
  const token = localStorage.getItem('cs_token') || '';
  const headers = { 'Content-Type': 'application/json' };
  if (token) headers['Authorization'] = 'Bearer ' + token;
  fetch('/api/sessions/delete', {
    method: 'POST',
    headers,
    body: JSON.stringify({ id: sessionId, delete_data: true }),
  })
    .then(r => {
      if (r.ok) {
        showToast('Session deleted', 'success', 2000);
        navigate('sessions');
      } else {
        showToast('Delete failed', 'error');
      }
    })
    .catch(() => showToast('Delete failed', 'error'));
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
        <details class="create-form-details" style="margin-bottom:12px;">
          <summary class="create-form-summary" style="padding:4px 0;">+ Name / description (optional)</summary>
          <div style="margin-top:8px;">
            <input
              id="sessionNameInput"
              class="form-input"
              type="text"
              placeholder="Session name (e.g. Auth refactor)"
              style="margin-bottom:8px;"
            />
          </div>
        </details>
        <div class="form-group">
          <label for="taskInput">Task description <span style="color:var(--text2);font-size:11px;">(optional)</span></label>
          <textarea
            id="taskInput"
            class="form-textarea"
            placeholder="e.g. Add unit tests to internal/session/manager.go (leave empty for an interactive shell session)"
            rows="5"
          ></textarea>
        </div>
        <div class="form-group">
          <label for="backendSelect">LLM backend</label>
          <select id="backendSelect" class="form-select">
            <option value="">Loading backends…</option>
          </select>
          <div id="backendWarn" style="display:none;background:rgba(245,158,11,0.08);border:1px solid rgba(245,158,11,0.3);border-radius:6px;padding:8px 10px;font-size:12px;margin-top:6px;">
            <div style="color:var(--warning,#f59e0b);font-weight:600;margin-bottom:4px;">⚠ Backend not installed or configured</div>
            <div id="backendWarnDetail" style="color:var(--text2);line-height:1.5;"></div>
          </div>
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
    const displaySnip = s.name || s.task || '';
    const taskSnippet = displaySnip.length > 60 ? displaySnip.slice(0, 60) + '…' : (displaySnip || '(no task)');
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

// Returns HTML with setup instructions for a given backend name.
function backendSetupHint(name) {
  const docsBase = 'https://github.com/dmz006/datawatch/blob/main/docs';
  const hints = {
    'claude-code': `Install Claude Code: <code>npm install -g @anthropic-ai/claude-code</code><br>Then run: <code>datawatch setup llm claude-code</code>`,
    'ollama':      `Install Ollama: <a href="https://ollama.com/download" target="_blank" rel="noopener" style="color:var(--accent);">ollama.com/download</a><br>Then run: <code>datawatch setup llm ollama</code>`,
    'opencode':    `Install opencode: <a href="https://opencode.ai" target="_blank" rel="noopener" style="color:var(--accent);">opencode.ai</a><br>Then run: <code>datawatch setup llm opencode</code>`,
    'opencode-acp':  `Install opencode with ACP support. See <a href="${docsBase}/backends.md" target="_blank" rel="noopener" style="color:var(--accent);">docs/backends.md</a>`,
    'aider':       `Install aider: <code>pip install aider-install && aider-install</code><br>Then run: <code>datawatch setup llm aider</code>`,
    'gemini':      `Install Gemini CLI: <code>npm install -g @google/gemini-cli</code><br>Then run: <code>datawatch setup llm gemini</code>`,
    'goose':       `Install Goose: see <a href="https://block.github.io/goose/docs/getting-started/installation" target="_blank" rel="noopener" style="color:var(--accent);">Goose docs</a><br>Then run: <code>datawatch setup llm goose</code>`,
    'openwebui':   `Configure Open WebUI URL and API key: <code>datawatch setup llm openwebui</code>`,
    'shell':       `The shell backend requires no installation. Run: <code>datawatch setup llm shell</code> to configure`,
  };
  const hint = hints[name] || `Run <code>datawatch setup llm ${escHtml(name)}</code> or see <a href="${docsBase}/backends.md" target="_blank" rel="noopener" style="color:var(--accent);">docs/backends.md</a> for setup instructions.`;
  return hint;
}

function fetchBackends() {
  const token = localStorage.getItem('cs_token') || '';
  const headers = token ? { 'Authorization': 'Bearer ' + token } : {};
  fetch('/api/backends', { headers })
    .then(r => r.json())
    .then(data => {
      // data.llm is now [{name, available, version}]
      const backends = data.llm || [];
      newSessionState.backends = backends;
      // Only show available (enabled/installed) backends
      const available = backends.filter(b => typeof b === 'string' || b.available);
      const sel = document.getElementById('backendSelect');
      if (!sel) return;
      if (available.length === 0) {
        sel.innerHTML = '<option value="">No backends available — check setup</option>';
        return;
      }
      sel.innerHTML = available.map(b => {
        const name = typeof b === 'string' ? b : b.name;
        const selected = name === data.active ? ' selected' : '';
        return `<option value="${escHtml(name)}"${selected}>${escHtml(name)}</option>`;
      }).join('');
      // All options are available; hide the warning
      const warn = document.getElementById('backendWarn');
      if (warn) warn.style.display = 'none';
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
      const entries = (data.entries || []).filter(e => e.is_dir).map(e =>
        `<div class="dir-entry" data-path="${escHtml(e.path)}">
          <span class="dir-icon">${e.is_link ? '🔗' : (e.name === '..' ? '⬆' : '📁')}</span>
          <span>${escHtml(e.name)}</span>
        </div>`
      ).join('');
      content.innerHTML = `<div class="dir-current">${escHtml(currentPath)}</div>` +
        `<button class="btn-secondary dir-select-btn" data-select="${escHtml(currentPath)}">&#10003; Use This Folder</button>` +
        (entries || '<div style="color:var(--text2);padding:8px;font-size:12px;">No subdirectories</div>');
      // Attach click handlers via event delegation (avoids inline onclick/JSON escaping issues)
      content.onclick = function(ev) {
        const entry = ev.target.closest('.dir-entry');
        const selBtn = ev.target.closest('[data-select]');
        if (entry && entry.dataset.path) {
          loadDirContents(entry.dataset.path);
        } else if (selBtn && selBtn.dataset.select) {
          selectDir(selBtn.dataset.select);
        }
      };
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

  // Use REST so we get the full session object back and can navigate directly to it.
  apiFetch('/api/sessions/start', { method: 'POST', body: JSON.stringify(payload) })
    .then(sess => {
      taskInput.value = '';
      if (nameInput) nameInput.value = '';
      if (resumeInput) resumeInput.value = '';
      newSessionState.selectedDir = '';
      const browser = document.getElementById('dirBrowser');
      if (browser) browser.style.display = 'none';
      newSessionState.browsing = false;
      // Seed local state immediately so the detail view renders before the WS broadcast arrives.
      updateSession(sess);
      navigate('session-detail', sess.full_id);
    })
    .catch(err => {
      showToast('Failed to start session: ' + err.message, 'error', 4000);
    })
    .finally(() => {
      if (btn) { btn.disabled = false; btn.textContent = 'Start Session'; }
    });
}

// ── Settings collapsible state ─────────────────────────────────────────────────
const settingsCollapsed = JSON.parse(localStorage.getItem('cs_settings_collapsed') || '{}');
const settingsPagination = {}; // sectionKey -> currentPage

function toggleSettingsSection(key) {
  settingsCollapsed[key] = !settingsCollapsed[key];
  localStorage.setItem('cs_settings_collapsed', JSON.stringify(settingsCollapsed));
  const content = document.getElementById('settings-sec-' + key);
  const chevron = document.getElementById('settings-chev-' + key);
  if (content) content.style.display = settingsCollapsed[key] ? 'none' : '';
  if (chevron) chevron.textContent = settingsCollapsed[key] ? '▶' : '▼';
}

function settingsSectionHeader(key, title) {
  const collapsed = !!settingsCollapsed[key];
  return `<div class="settings-section-title settings-section-toggle" onclick="toggleSettingsSection('${key}')">
    <span id="settings-chev-${key}" class="settings-chevron">${collapsed ? '▶' : '▼'}</span>${escHtml(title)}
  </div>`;
}

function renderPageControls(key, page, total, pageSize, reloadFn) {
  const pages = Math.ceil(total / pageSize);
  if (pages <= 1) return '';
  const pageSizes = [5, 10, 25, 50];
  return `<div class="page-controls">
    <button class="btn-icon page-btn" ${page === 0 ? 'disabled' : ''} onclick="${reloadFn}(-1)">&#8592;</button>
    <span class="page-info">${page + 1} / ${pages}</span>
    <button class="btn-icon page-btn" ${page >= pages - 1 ? 'disabled' : ''} onclick="${reloadFn}(1)">&#8594;</button>
    <select class="page-size-sel" onchange="settingsPageSize('${key}',this.value)">
      ${pageSizes.map(n => `<option value="${n}"${n === pageSize ? ' selected' : ''}>${n}/page</option>`).join('')}
    </select>
  </div>`;
}

const settingsPageSize = {}; // sectionKey -> pageSize (default 10)
function getPageSize(key) { return settingsPageSize[key] || 10; }
window.settingsPageSize = function(key, size) {
  settingsPageSize[key] = parseInt(size, 10) || 10;
  settingsPagination[key] = 0;
  if (key === 'cmds') loadSavedCommands();
  else if (key === 'filters') loadFilters();
  else if (key === 'backends') loadConfigStatus();
  else if (key === 'servers') loadServers();
};

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

  const secContent = (key) => settingsCollapsed[key] ? 'display:none' : '';

  view.innerHTML = `
    <div class="view-content">
      <div class="settings-view">

        <div class="settings-section">
          <div class="settings-section-title">Authentication</div>
          <div class="settings-row">
            <div class="settings-label">Bearer Token</div>
            <input type="password" class="settings-input" id="tokenInput" value="${escHtml(state.token)}" placeholder="Leave empty if no token" autocomplete="off" />
          </div>
          <div class="settings-row">
            <button class="btn-secondary" onclick="saveToken()">Save Token &amp; Reconnect</button>
          </div>
        </div>

        <div class="settings-section">
          ${settingsSectionHeader('servers', 'Servers')}
          <div id="settings-sec-servers" style="${secContent('servers')}">
            <div class="settings-row">
              <div class="settings-label">Status</div>
              <div class="connection-indicator">
                <div class="dot ${connClass}"></div>
                <span>${escHtml(connText)}</span>
              </div>
            </div>
            <div class="settings-row">
              <div class="settings-label">This server</div>
              <div class="settings-value">${escHtml(location.host)}</div>
            </div>
            <div id="serverStatus" style="color:var(--text2);font-size:13px;padding:4px 0;">Loading…</div>
          </div>
        </div>

        <div class="settings-section">
          ${settingsSectionHeader('backends', 'Backend Status')}
          <div id="settings-sec-backends" style="${secContent('backends')}">
            <div id="configStatus" style="color:var(--text2);font-size:13px;padding:4px 0;">Loading…</div>
            <div class="settings-row" style="margin-top:4px;">
              <div class="settings-label">Signal Device</div>
              <div class="settings-value" id="linkStatusText">Checking…</div>
            </div>
            <div class="settings-row" id="linkActionRow">
              <button class="btn-secondary" onclick="startLinking()">Link Signal Device</button>
            </div>
            <div class="settings-row" id="linkQrRow" style="display:none">
              <div style="display:flex;flex-direction:column;align-items:center;gap:12px;width:100%;">
                <div id="linkQrCode" style="background:#fff;padding:12px;border-radius:8px;display:inline-block;"></div>
                <div style="font-size:12px;color:var(--text2);font-family:system-ui;text-align:center;line-height:1.5;">
                  Open Signal on your phone<br>Settings &rarr; Linked Devices &rarr; Link New Device
                </div>
              </div>
            </div>
          </div>
        </div>

        <div class="settings-section">
          ${settingsSectionHeader('llm', 'LLM Configuration')}
          <div id="settings-sec-llm" style="${secContent('llm')}">
            <div id="llmConfigList" style="color:var(--text2);font-size:13px;padding:4px 0;">Loading…</div>
          </div>
        </div>

        <div class="settings-section">
          ${settingsSectionHeader('notifs', 'Notifications')}
          <div id="settings-sec-notifs" style="${secContent('notifs')}">
            <div class="settings-row">
              <div class="settings-label">Status</div>
              <div class="settings-value">${escHtml(notifText)}</div>
            </div>
            <div class="settings-row">
              <button class="btn-success" onclick="requestNotificationPermission()">Request Permission</button>
            </div>
          </div>
        </div>

        <div class="settings-section">
          ${settingsSectionHeader('cmds', 'Saved Commands')}
          <div id="settings-sec-cmds" style="${secContent('cmds')}">
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
        </div>

        <div class="settings-section">
          ${settingsSectionHeader('filters', 'Output Filters')}
          <div id="settings-sec-filters" style="${secContent('filters')}">
            <div id="filtersList"><div style="color:var(--text2);font-size:13px;">Loading…</div></div>
            <details class="create-form-details">
              <summary class="create-form-summary">+ Add Filter</summary>
              <div class="create-form">
                <input id="newFilterPattern" class="form-input" type="text" placeholder="Regex pattern (e.g. DATAWATCH_RATE_LIMITED)" autocomplete="off" />
                <select id="newFilterAction" class="form-select">
                  <option value="send_input">send_input — send text to session</option>
                  <option value="alert">alert — create system alert</option>
                  <option value="schedule">schedule — queue command for next prompt</option>
                  <option value="detect_prompt">detect_prompt — mark session as waiting for input</option>
                </select>
                <input id="newFilterValue" class="form-input" type="text" placeholder="Value (optional, e.g. y)" autocomplete="off" />
                <button class="btn-primary" style="margin-top:6px;" onclick="createFilter()">Save Filter</button>
              </div>
            </details>
          </div>
        </div>

        <div class="settings-section">
          ${settingsSectionHeader('api', 'API')}
          <div id="settings-sec-api" style="${secContent('api')}">
            <div class="settings-row">
              <div class="settings-label">Swagger UI</div>
              <div class="settings-value"><a href="/api/docs" target="_blank" style="color:var(--accent2);">/api/docs</a></div>
            </div>
            <div class="settings-row">
              <div class="settings-label">OpenAPI Spec</div>
              <div class="settings-value"><a href="/api/openapi.yaml" target="_blank" style="color:var(--accent2);">/api/openapi.yaml</a></div>
            </div>
          </div>
        </div>

        <div class="settings-section">
          <div class="settings-section-title">About</div>
          <div class="settings-row">
            <div class="settings-label">Version</div>
            <div class="settings-value" id="aboutVersion">—</div>
          </div>
          <div class="settings-row">
            <div class="settings-label">Update</div>
            <div class="settings-value" id="aboutUpdate">
              <button class="btn-secondary" style="font-size:12px;" onclick="checkForUpdate()">Check now</button>
            </div>
          </div>
          <div class="settings-row">
            <div class="settings-label">Daemon</div>
            <div class="settings-value"><button class="btn-secondary" style="font-size:12px;" onclick="restartDaemon()">Restart</button></div>
          </div>
          <div class="settings-row">
            <div class="settings-label">Sessions</div>
            <div class="settings-value">
              <button class="btn-link" onclick="navigate('sessions');state.showHistory=true;renderSessionsView();">${state.sessions.length} in store</button>
            </div>
          </div>
          <div class="settings-row">
            <div class="settings-label">Project</div>
            <div class="settings-value"><a href="https://github.com/dmz006/datawatch" target="_blank" rel="noopener" style="color:var(--accent);">github.com/dmz006/datawatch</a></div>
          </div>
        </div>

      </div>
    </div>`;

  loadLinkStatus();
  loadConfigStatus();
  loadServers();
  loadSavedCommands();
  loadFilters();
  loadVersionInfo();
  loadLLMConfig();
}

function loadVersionInfo() {
  fetch('/api/health', { headers: tokenHeader() })
    .then(r => r.ok ? r.json() : null)
    .then(data => {
      if (!data) return;
      const el = document.getElementById('aboutVersion');
      if (el) el.textContent = 'v' + (data.version || '?');
    })
    .catch(() => {});
}

function loadLLMConfig() {
  const el = document.getElementById('llmConfigList');
  if (!el) return;
  fetch('/api/backends', { headers: tokenHeader() })
    .then(r => r.ok ? r.json() : null)
    .then(data => {
      if (!data) { el.textContent = 'Unavailable'; return; }
      const backends = data.llm || [];
      if (backends.length === 0) { el.textContent = 'No LLM backends registered.'; return; }
      el.innerHTML = backends.map(b => {
        const name = typeof b === 'string' ? b : b.name;
        const avail = typeof b === 'string' ? true : b.available;
        const ver = typeof b === 'object' && b.version ? ` <span style="color:var(--text2);font-size:11px;">v${escHtml(b.version)}</span>` : '';
        const statusColor = avail ? 'var(--success,#22c55e)' : 'var(--error,#ef4444)';
        const statusText = avail ? 'available' : 'not installed';
        const isActive = name === data.active;
        return `<div class="settings-row" style="justify-content:space-between;padding:6px 0;">
          <div>
            <strong>${escHtml(name)}</strong>${ver}
            ${isActive ? '<span style="color:var(--accent);font-size:11px;margin-left:6px;">(active)</span>' : ''}
          </div>
          <span style="font-size:11px;color:${statusColor};">${statusText}</span>
        </div>`;
      }).join('') + `<div style="font-size:11px;color:var(--text2);padding-top:6px;">
        Active backend set via <code>llm_backend</code> in config or per-session selection.
      </div>`;
    })
    .catch(() => { if (el) el.textContent = 'Failed to load'; });
}

function checkForUpdate() {
  const el = document.getElementById('aboutUpdate');
  if (el) el.innerHTML = '<span style="color:var(--text2);font-size:12px;">Checking…</span>';
  fetch('/api/health', { headers: tokenHeader() })
    .then(r => r.ok ? r.json() : null)
    .then(data => {
      if (!data) { if (el) el.innerHTML = '<span style="color:var(--error);">Check failed</span>'; return; }
      const current = data.version || '';
      // Ask GitHub API for latest release
      fetch('https://api.github.com/repos/dmz006/datawatch/releases/latest')
        .then(r => r.ok ? r.json() : null)
        .then(gh => {
          if (!gh || !gh.tag_name) { if (el) el.innerHTML = '<span style="color:var(--error);">Check failed</span>'; return; }
          const latest = gh.tag_name.replace(/^v/, '');
          if (!el) return;
          if (latest === current) {
            el.innerHTML = '<span style="color:var(--success,#22c55e);font-size:12px;">Up to date (v' + current + ')</span>';
          } else {
            el.innerHTML = `<span style="color:var(--warning,#f59e0b);font-size:12px;">Update available: v${latest} (current: v${current})</span>` +
              ` <button class="btn-secondary" style="font-size:11px;margin-left:6px;" onclick="runUpdate()">Update</button>`;
          }
        })
        .catch(() => { if (el) el.innerHTML = '<span style="color:var(--error);">Check failed</span>'; });
    })
    .catch(() => { if (el) el.innerHTML = '<span style="color:var(--error);">Check failed</span>'; });
}

function runUpdate() {
  const el = document.getElementById('aboutUpdate');
  if (el) el.innerHTML = '<span style="color:var(--text2);font-size:12px;">Downloading update… daemon will restart automatically.</span>';
  apiFetch('/api/update', { method: 'POST' })
    .then(data => {
      if (data.status === 'up_to_date') {
        if (el) el.innerHTML = '<span style="color:var(--success,#22c55e);font-size:12px;">Already up to date (v' + (data.version || '') + ')</span>';
      } else {
        showToast('Installing v' + (data.version || 'latest') + '… daemon will restart.', 'info', 8000);
      }
    })
    .catch(err => {
      if (el) el.innerHTML = '<span style="color:var(--error);font-size:12px;">Update failed: ' + err.message + '</span>';
    });
}

// ── Config / Backend Status ────────────────────────────────────────────────────

function tokenHeader() {
  const t = localStorage.getItem('cs_token') || '';
  return t ? { 'Authorization': 'Bearer ' + t } : {};
}

// Detect whether a backend has been configured (has any non-empty credential/url field)
function isBackendConfigured(svc, s) {
  const credFields = {
    telegram: ['token'], discord: ['token'], slack: ['token'],
    matrix: ['access_token'], ntfy: ['topic'], email: ['host', 'username'],
    twilio: ['account_sid', 'from_number'], github_webhook: ['secret'],
    webhook: ['addr'],
  };
  const fields = credFields[svc] || [];
  if (fields.length === 0) return true; // always considered configured
  return fields.some(f => s[f] && s[f] !== '' && s[f] !== '***');
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
        const configured = isBackendConfigured(svc, s);
        const label = svc.replace(/_/g, ' ');
        return `<div class="settings-row backend-row">
          <div class="settings-label backend-label" style="text-transform:capitalize;">${escHtml(label)}</div>
          <span class="state state-${on ? 'running' : configured ? 'complete' : 'failed'}" style="font-size:11px;">${on ? 'enabled' : configured ? 'disabled' : 'not configured'}</span>
          <div class="backend-actions">
            <button class="btn-secondary backend-btn" onclick="openBackendSetup('${svc}')" title="${configured ? 'Edit configuration' : 'Configure'}">
              ${configured ? '✎ Edit' : '⚙ Configure'}
            </button>
            ${configured ? `<button class="btn-secondary backend-btn ${on ? 'backend-btn-stop' : 'backend-btn-start'}" onclick="toggleBackend('${svc}',${!on})" title="${on ? 'Disable' : 'Enable'}">
              ${on ? '⏹ Disable' : '▶ Enable'}
            </button>` : ''}
          </div>
        </div>`;
      }).join('') + `<div style="font-size:11px;color:var(--text2);padding:8px 12px;">
        Changes require a daemon restart to take effect.
        <button class="btn-link" style="font-size:11px;" onclick="restartDaemon()">Restart now</button>
      </div>`;
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

// ── Backend config field definitions ──────────────────────────────────────────
const BACKEND_FIELDS = {
  telegram:       [{ key:'token', label:'Bot Token', type:'password' }, { key:'chat_id', label:'Chat ID', type:'text' }],
  discord:        [{ key:'token', label:'Bot Token', type:'password' }, { key:'channel_id', label:'Channel ID', type:'text' }],
  slack:          [{ key:'token', label:'OAuth Bot Token', type:'password' }, { key:'channel_id', label:'Channel ID', type:'text' }],
  matrix:         [{ key:'homeserver', label:'Homeserver URL', type:'text' }, { key:'user_id', label:'User ID (@bot:host)', type:'text' }, { key:'access_token', label:'Access Token', type:'password' }, { key:'room_id', label:'Room ID', type:'text' }],
  ntfy:           [{ key:'server_url', label:'Server URL', type:'text', placeholder:'https://ntfy.sh' }, { key:'topic', label:'Topic', type:'text' }, { key:'token', label:'Token (optional)', type:'password' }],
  email:          [{ key:'host', label:'SMTP Host', type:'text' }, { key:'port', label:'Port', type:'number', placeholder:'587' }, { key:'username', label:'Username', type:'text' }, { key:'password', label:'Password', type:'password' }, { key:'from', label:'From Address', type:'text' }, { key:'to', label:'To Address', type:'text' }],
  twilio:         [{ key:'account_sid', label:'Account SID', type:'text' }, { key:'auth_token', label:'Auth Token', type:'password' }, { key:'from_number', label:'From Number', type:'text' }, { key:'to_number', label:'To Number', type:'text' }, { key:'webhook_addr', label:'Webhook Addr', type:'text', placeholder:':9003' }],
  github_webhook: [{ key:'addr', label:'Listen Address', type:'text', placeholder:':9001' }, { key:'secret', label:'Webhook Secret', type:'password' }],
  webhook:        [{ key:'addr', label:'Listen Address', type:'text', placeholder:':9002' }, { key:'token', label:'Token (optional)', type:'password' }],
};

function openBackendSetup(service) {
  fetch('/api/config', { headers: tokenHeader() })
    .then(r => r.json())
    .then(cfg => showBackendConfigPopup(service, cfg[service] || {}))
    .catch(() => showToast('Failed to load config', 'error'));
}

function showBackendConfigPopup(service, currentValues) {
  const existing = document.getElementById('backendConfigPopup');
  if (existing) existing.remove();
  const fields = BACKEND_FIELDS[service] || [];
  const label = service.replace(/_/g, ' ');
  const fieldsHtml = fields.map(f => {
    const val = currentValues[f.key] && currentValues[f.key] !== '***' ? currentValues[f.key] : '';
    const ph = currentValues[f.key] === '***' ? '(configured — enter to change)' : (f.placeholder || '');
    return `<div class="popup-field">
      <label class="popup-field-label">${escHtml(f.label)}</label>
      <input type="${f.type||'text'}" id="bkf_${escHtml(f.key)}" class="form-input" value="${escHtml(val)}" placeholder="${escHtml(ph)}" autocomplete="off" />
    </div>`;
  }).join('');
  const popup = document.createElement('div');
  popup.id = 'backendConfigPopup';
  popup.className = 'backend-config-overlay';
  popup.innerHTML = `<div class="backend-config-popup">
    <div class="backend-config-header">
      <strong style="text-transform:capitalize;">Configure ${escHtml(label)}</strong>
      <button class="btn-icon" onclick="closeBackendConfigPopup()">✕</button>
    </div>
    <div class="backend-config-body">
      ${fields.length ? fieldsHtml : '<p style="color:var(--text2);font-size:13px;">No configurable fields.</p>'}
    </div>
    <div class="backend-config-footer">
      <button class="btn-primary" onclick="saveBackendConfig('${escHtml(service)}')">Save &amp; Enable</button>
      <button class="btn-secondary" onclick="closeBackendConfigPopup()">Cancel</button>
    </div>
  </div>`;
  popup.addEventListener('click', e => { if (e.target === popup) closeBackendConfigPopup(); });
  document.body.appendChild(popup);
}

function closeBackendConfigPopup() {
  const p = document.getElementById('backendConfigPopup');
  if (p) p.remove();
}

function saveBackendConfig(service) {
  const fields = BACKEND_FIELDS[service] || [];
  const updates = { [service + '.enabled']: true };
  for (const f of fields) {
    const el = document.getElementById('bkf_' + f.key);
    if (el && el.value.trim()) updates[service + '.' + f.key] = el.value.trim();
  }
  fetch('/api/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', ...tokenHeader() },
    body: JSON.stringify(updates),
  })
    .then(r => r.ok ? r.json() : r.text().then(t => Promise.reject(new Error(t))))
    .then(() => {
      closeBackendConfigPopup();
      showToast(service + ' configured. Restart daemon to apply.', 'success', 4000);
      loadConfigStatus();
    })
    .catch(err => showToast('Save failed: ' + err.message, 'error'));
}

function restartDaemon() {
  apiFetch('/api/restart', { method: 'POST' })
    .then(() => showToast('Daemon restarting… reconnecting in a moment.', 'info', 6000))
    .catch(err => showToast('Restart failed: ' + err.message, 'error'));
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
      if (servers.length === 0) { el.textContent = 'No servers available.'; return; }
      // Default active server is 'local' when state.activeServer is null
      const effectiveActive = state.activeServer || 'local';
      const rows = servers.map(sv => {
        const auth = sv.has_auth ? '🔒' : '🔓';
        const isActive = effectiveActive === sv.name;
        const activeLabel = isActive ? ' <span style="color:var(--accent);font-size:11px;">(active)</span>' : '';
        return `<div class="settings-row" style="justify-content:space-between">
          <div><strong>${escHtml(sv.name)}</strong>${activeLabel} ${auth}<br><span style="font-size:12px;color:var(--text2)">${escHtml(sv.url)}</span></div>
          <button class="btn-secondary" style="font-size:12px;padding:4px 8px" onclick="selectServer('${escHtml(sv.name)}')">${isActive ? 'Connected' : 'Select'}</button>
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
    } else if (permission === 'denied') {
      const isAndroid = /Android/i.test(navigator.userAgent);
      const hint = isAndroid
        ? 'On Android: tap the lock icon in the address bar → Site settings → Notifications → Allow.'
        : 'Check browser site settings to allow notifications for this site.';
      showToast('Notifications blocked. ' + hint, 'error', 8000);
    } else {
      showToast('Notification permission dismissed.', 'info');
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

// Strip ANSI terminal escape sequences for display
// eslint-disable-next-line no-control-regex
const ANSI_RE = /\x1b(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])/g;
function stripAnsi(s) { return s ? s.replace(ANSI_RE, '') : ''; }

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

  // Fetch alerts and saved commands in parallel
  Promise.all([
    fetch('/api/alerts', { headers: tokenHeader() }).then(r => r.ok ? r.json() : null),
    fetch('/api/commands', { headers: tokenHeader() }).then(r => r.ok ? r.json() : [])
  ]).then(([data, cmds]) => {
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

    // Group alerts by session_id; ungrouped alerts go into a null group
    const groups = new Map(); // sessionId|null -> [alerts]
    for (const a of data.alerts) {
      const key = a.session_id || null;
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key).push(a);
    }

    // Get live sessions for state lookup
    const liveSessions = state.sessions || [];

    const renderAlert = (a, sessState) => {
      const levelColor = a.level === 'error' ? 'var(--error)' : a.level === 'warn' ? 'var(--warning, #f59e0b)' : 'var(--text2)';
      const isWaiting = sessState === 'waiting_input';

      // Quick-reply buttons for waiting sessions (from saved commands)
      let replyBtns = '';
      if (isWaiting && cmds && cmds.length > 0 && a.session_id) {
        const shortID = a.session_id.split('-').pop();
        const btnHtml = cmds.map(c =>
          `<button class="quick-btn" onclick="alertSendCmd(${JSON.stringify(a.session_id)},${JSON.stringify(c.command)})">${escHtml(c.name)}</button>`
        ).join('');
        replyBtns = `<div class="quick-input-row" style="margin-top:8px;">${btnHtml}</div>`;
      }

      return `<div class="card" style="margin-bottom:6px;border-left:3px solid ${levelColor};${a.read ? 'opacity:0.6' : ''}">
        <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:4px;">
          <strong style="color:${levelColor};font-size:12px;">${escHtml(a.level.toUpperCase())}</strong>
          <span style="font-size:11px;color:var(--text2);">${timeAgo(a.created_at)}</span>
        </div>
        <div style="font-weight:500;font-size:13px;">${escHtml(a.title)}</div>
        <div style="font-size:12px;color:var(--text2);margin-top:2px;">${escHtml(a.body)}</div>
        ${replyBtns}
      </div>`;
    };

    let html = `<div style="display:flex;justify-content:flex-end;margin-bottom:8px;">
      <button class="btn-secondary" style="font-size:12px;" onclick="renderAlertsView()">Refresh</button>
    </div>`;

    // Render grouped sections
    for (const [sessID, alerts] of groups) {
      if (sessID) {
        const sess = liveSessions.find(s => s.full_id === sessID || s.id === sessID);
        const sessLabel = sess ? (sess.name || sess.id) : sessID.split('-').pop();
        const sessState = sess ? sess.state : '';
        const stateLabel = sessState === 'waiting_input' ? ' <span style="color:var(--warning,#f59e0b);font-size:11px;">⚠ waiting input</span>' : '';
        const sessLink = sess ? `<span style="cursor:pointer;text-decoration:underline;" onclick="navigate('session',${JSON.stringify(sessID)})">${escHtml(sessLabel)}</span>` : escHtml(sessLabel);
        html += `<div style="margin-bottom:16px;">
          <div style="font-size:12px;color:var(--text2);font-weight:600;margin-bottom:6px;padding:4px 0;border-bottom:1px solid var(--border);">
            Session: ${sessLink}${stateLabel}
          </div>`;
        for (const a of alerts) {
          html += renderAlert(a, sessState);
        }
        html += `</div>`;
      }
    }

    // Render ungrouped alerts last
    const ungrouped = groups.get(null);
    if (ungrouped && ungrouped.length > 0) {
      html += `<div style="margin-bottom:16px;">
        <div style="font-size:12px;color:var(--text2);font-weight:600;margin-bottom:6px;padding:4px 0;border-bottom:1px solid var(--border);">System</div>`;
      for (const a of ungrouped) {
        html += renderAlert(a, '');
      }
      html += `</div>`;
    }

    el.innerHTML = html;
  }).catch(() => {
    const el = document.getElementById('alertsList');
    if (el) el.innerHTML = '<div style="color:var(--error);padding:16px;">Failed to load alerts.</div>';
  });
}

function alertSendCmd(sessID, command) {
  apiFetch('/api/command', { method: 'POST', body: { command: 'send ' + sessID.split('-').pop() + ': ' + command } })
    .then(() => showToast('Sent: ' + command))
    .catch(e => showToast('Error: ' + e.message, true));
}

// ── Saved Commands (in Settings) ───────────────────────────────────────────────

function pageCmd(dir) {
  settingsPagination.cmds = Math.max(0, (settingsPagination.cmds || 0) + dir);
  loadSavedCommands();
}

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
      const ps = getPageSize('cmds');
      const page = Math.min(settingsPagination.cmds || 0, Math.max(0, Math.ceil(cmds.length / ps) - 1));
      settingsPagination.cmds = page;
      const pageCmds = cmds.slice(page * ps, page * ps + ps);
      el.innerHTML = renderPageControls('cmds', page, cmds.length, ps, 'pageCmd') +
        '<div>' + pageCmds.map(cmd => {
        const id = 'cmd-edit-' + cmd.name.replace(/[^a-z0-9]/gi, '_');
        return `<div class="settings-list-row">
          <div class="settings-list-view" id="${id}-view">
            <div class="settings-list-info">
              <strong>${escHtml(cmd.name)}</strong>
              <span class="settings-list-detail">${escHtml(cmd.command)}</span>
              ${cmd.seeded ? '<span class="settings-list-tag">(seeded)</span>' : ''}
            </div>
            <div class="settings-list-actions">
              <button class="btn-icon" title="Edit" onclick="showCmdEdit('${escHtml(cmd.name)}')">✎</button>
              ${!cmd.seeded ? `<button class="btn-icon btn-icon-del" title="Delete" onclick="deleteSavedCmd('${escHtml(cmd.name)}')">✕</button>` : ''}
            </div>
          </div>
          <div class="settings-list-edit" id="${id}-edit" style="display:none;">
            <input class="settings-input" id="${id}-name" value="${escHtml(cmd.name)}" placeholder="Name" style="width:120px;" />
            <input class="settings-input" id="${id}-val" value="${escHtml(cmd.command)}" placeholder="Command" style="flex:1;" />
            <button class="btn-secondary" style="font-size:12px;" onclick="saveCmdEdit('${escHtml(cmd.name)}','${id}')">Save</button>
            <button class="btn-icon" onclick="hideCmdEdit('${id}')">✕</button>
          </div>
        </div>`;
      }).join('') + '</div>';
    })
    .catch(() => { el.innerHTML = '<div style="color:var(--error);font-size:13px;">Failed to load commands.</div>'; });
}

function deleteSavedCmd(name) {
  fetch('/api/commands?name=' + encodeURIComponent(name), { method: 'DELETE', headers: tokenHeader() })
    .then(r => r.ok ? loadSavedCommands() : showToast('Delete failed', 'error'))
    .catch(() => showToast('Delete failed', 'error'));
}

function showCmdEdit(name) {
  const id = 'cmd-edit-' + name.replace(/[^a-z0-9]/gi, '_');
  const view = document.getElementById(id + '-view');
  const edit = document.getElementById(id + '-edit');
  if (view) view.style.display = 'none';
  if (edit) edit.style.display = 'flex';
}

function hideCmdEdit(id) {
  const view = document.getElementById(id + '-view');
  const edit = document.getElementById(id + '-edit');
  if (view) view.style.display = 'flex';
  if (edit) edit.style.display = 'none';
}

function saveCmdEdit(oldName, id) {
  const nameEl = document.getElementById(id + '-name');
  const valEl = document.getElementById(id + '-val');
  if (!nameEl || !valEl) return;
  apiFetch('/api/commands', {
    method: 'PUT',
    body: JSON.stringify({ old_name: oldName, name: nameEl.value.trim(), command: valEl.value.trim() }),
  })
    .then(() => { loadSavedCommands(); showToast('Command updated', 'success', 2000); })
    .catch(err => showToast('Update failed: ' + err.message, 'error'));
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

function pageFilter(dir) {
  settingsPagination.filters = Math.max(0, (settingsPagination.filters || 0) + dir);
  loadFilters();
}

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
      const ps = getPageSize('filters');
      const page = Math.min(settingsPagination.filters || 0, Math.max(0, Math.ceil(filters.length / ps) - 1));
      settingsPagination.filters = page;
      const pageFilters = filters.slice(page * ps, page * ps + ps);
      el.innerHTML = renderPageControls('filters', page, filters.length, ps, 'pageFilter') +
        '<div>' + pageFilters.map(f => {
        const fid = 'flt-' + f.id;
        const actions = ['alert','send_input','detect_prompt','schedule'];
        return `<div class="settings-list-row">
          <div class="settings-list-view" id="${fid}-view">
            <div class="settings-list-info">
              <span class="state state-${f.enabled ? 'running' : 'failed'}" style="font-size:10px;margin-right:6px;">${f.enabled ? 'on' : 'off'}</span>
              <code class="settings-list-detail">${escHtml(f.pattern)}</code>
              <span class="settings-list-tag">${escHtml(f.action)}${f.value ? ' → ' + escHtml(f.value) : ''}</span>
            </div>
            <div class="settings-list-actions">
              <button class="btn-icon" title="${f.enabled ? 'Disable' : 'Enable'}" onclick="toggleFilter('${escHtml(f.id)}',${!f.enabled})">${f.enabled ? '⏸' : '▶'}</button>
              <button class="btn-icon" title="Edit" onclick="showFilterEdit('${escHtml(f.id)}')">✎</button>
              <button class="btn-icon btn-icon-del" title="Delete" onclick="deleteFilter('${escHtml(f.id)}')">✕</button>
            </div>
          </div>
          <div class="settings-list-edit" id="${fid}-edit" style="display:none;">
            <input class="settings-input" id="${fid}-pat" value="${escHtml(f.pattern)}" placeholder="Pattern (regex)" style="flex:2;" />
            <select class="settings-input" id="${fid}-act" style="flex:1;">${actions.map(a => `<option value="${a}"${a===f.action?' selected':''}>${a}</option>`).join('')}</select>
            <input class="settings-input" id="${fid}-val" value="${escHtml(f.value||'')}" placeholder="Value (optional)" style="flex:1;" />
            <button class="btn-secondary" style="font-size:12px;" onclick="saveFilterEdit('${escHtml(f.id)}')">Save</button>
            <button class="btn-icon" onclick="hideFilterEdit('${escHtml(f.id)}')">✕</button>
          </div>
        </div>`;
      }).join('') + '</div>';
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

function showFilterEdit(id) {
  const fid = 'flt-' + id;
  const v = document.getElementById(fid + '-view');
  const e = document.getElementById(fid + '-edit');
  if (v) v.style.display = 'none';
  if (e) e.style.display = 'flex';
}

function hideFilterEdit(id) {
  const fid = 'flt-' + id;
  const v = document.getElementById(fid + '-view');
  const e = document.getElementById(fid + '-edit');
  if (v) v.style.display = 'flex';
  if (e) e.style.display = 'none';
}

function saveFilterEdit(id) {
  const fid = 'flt-' + id;
  const pattern = (document.getElementById(fid + '-pat') || {}).value || '';
  const action = (document.getElementById(fid + '-act') || {}).value || '';
  const value = (document.getElementById(fid + '-val') || {}).value || '';
  if (!pattern || !action) { showToast('Pattern and action required', 'error'); return; }
  apiFetch('/api/filters', {
    method: 'PATCH',
    body: JSON.stringify({ id, pattern, action, value, enabled: true }),
  })
    .then(() => { loadFilters(); showToast('Filter updated', 'success', 2000); })
    .catch(err => showToast('Update failed: ' + err.message, 'error'));
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
window.alertSendCmd = alertSendCmd;
window.toggleSessionTimeline = toggleSessionTimeline;
window.deleteSavedCmd = deleteSavedCmd;
window.toggleFilter = toggleFilter;
window.deleteFilter = deleteFilter;
window.killSession = killSession;
window.switchOutputTab = switchOutputTab;
window.restartSession = restartSession;
window.deleteSession = deleteSession;
window.sendSavedCmd = sendSavedCmd;
window.sessionDragStart = sessionDragStart;
window.sessionDragOver = sessionDragOver;
window.sessionDrop = sessionDrop;
window.sessionDragEnd = sessionDragEnd;
window.checkForUpdate = checkForUpdate;
window.runUpdate = runUpdate;
window.moveSession = moveSession;
window.sendQuickInput = sendQuickInput;
window.sendChannelMessage = sendChannelMessage;
window.sendSessionInputDirect = sendSessionInputDirect;
window.restartDaemon = restartDaemon;
window.openBackendSetup = openBackendSetup;
window.toggleBackend = toggleBackend;
window.showCmdEdit = showCmdEdit;
window.hideCmdEdit = hideCmdEdit;
window.saveCmdEdit = saveCmdEdit;
window.showFilterEdit = showFilterEdit;
window.hideFilterEdit = hideFilterEdit;
window.saveFilterEdit = saveFilterEdit;
window.renameSession = renameSession;
window.openDirBrowser = openDirBrowser;
window.dirEntryClick = dirEntryClick;
window.dirNavigate = dirNavigate;
window.selectDir = selectDir;
window.toggleHistory = toggleHistory;
window.createSavedCmd = createSavedCmd;
window.createFilter = createFilter;
window.toggleSettingsSection = toggleSettingsSection;
window.updateHeaderSessName = updateHeaderSessName;
window.startHeaderRename = startHeaderRename;
window.confirmHeaderRename = confirmHeaderRename;
window.cancelHeaderRename = cancelHeaderRename;
window.openBackendSetup = openBackendSetup;
window.closeBackendConfigPopup = closeBackendConfigPopup;
window.saveBackendConfig = saveBackendConfig;
window.pageCmd = pageCmd;
window.pageFilter = pageFilter;
window.loadLLMConfig = loadLLMConfig;
