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
  outputBuffer: {},       // sessionId -> string[] (ANSI stripped for fallback)
  rawOutputBuffer: {},    // sessionId -> string[] (ANSI preserved for xterm.js)
  channelReplies: {},     // sessionId -> [{text, ts}]
  channelReady: {},       // sessionId -> bool (true once channel/ACP connection confirmed)
  notifPermission: Notification.permission,
  sessionOrder: JSON.parse(localStorage.getItem('cs_session_order') || '[]'), // manual ordering
  servers: [],            // remote server list from /api/servers
  activeServer: null,     // selected server name (null = local)
  alertUnread: 0,         // unread alert count for badge
  showHistory: false,     // show completed/killed/failed sessions in main list
  backPressCount: 0,      // for double-back-press confirmation
  backPressTimer: null,
  sessionFilter: '',      // dynamic filter for session list
  suppressActiveToasts: true, // cached from server config
  autoRestartOnConfig: false, // cached from server config
  terminal: null,          // xterm.js Terminal instance for active session
  termFitAddon: null,      // xterm.js FitAddon instance
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
    // Load server-side UI preferences into state cache
    fetch('/api/config', { headers: tokenHeader() }).then(r => r.ok ? r.json() : null).then(cfg => {
      if (!cfg) return;
      state.suppressActiveToasts = cfg.server?.suppress_active_toasts !== false;
      state.autoRestartOnConfig = !!cfg.server?.auto_restart_on_config;
    }).catch(() => {});
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
    case 'raw_output':
      // Raw output with ANSI preserved — buffer and route to xterm.js
      if (msg.data) {
        const sid = msg.data.session_id;
        const rawLines = msg.data.lines || [];
        if (!state.rawOutputBuffer[sid]) state.rawOutputBuffer[sid] = [];
        state.rawOutputBuffer[sid].push(...rawLines);
        if (state.rawOutputBuffer[sid].length > 500) {
          state.rawOutputBuffer[sid] = state.rawOutputBuffer[sid].slice(-500);
        }
        if (state.terminal && state.activeView === 'session-detail' && state.activeSession === sid && rawLines.length > 0) {
          state.terminal.write(rawLines.join('\r\n') + '\r\n');
        }
      }
      break;
    case 'needs_input':
      if (msg.data) {
        handleNeedsInput(msg.data.session_id, msg.data.prompt || '');
      }
      break;
    case 'notification':
      if (msg.data && msg.data.message) {
        // Suppress "Input sent" type notifications when viewing the session
        if (state.activeView === 'session-detail' && state.activeSession &&
            msg.data.message.includes(state.activeSession.split('-').pop())) {
          break;
        }
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
    case 'channel_ready':
      if (msg.data && msg.data.session_id) {
        handleChannelReadyEvent(msg.data.session_id);
      }
      break;
  }
}

function handleAlert(a) {
  state.alertUnread++;
  updateAlertBadge();
  // Suppress toast if user is actively viewing the session this alert belongs to
  // (configurable via Settings → suppress_active_toasts, default: true)
  const suppressActive = state.suppressActiveToasts;
  if (suppressActive && state.activeView === 'session-detail' && state.activeSession && a.session_id === state.activeSession) {
    return;
  }
  const level = a.level === 'error' ? 'error' : a.level === 'warn' ? 'error' : 'info';
  showToast(`⚠ ${a.title}: ${a.body}`, level, 5000);
}

function dismissConnBanner(sessionId) {
  // User dismissed the MCP connection banner — mark as ready so it doesn't reappear
  state.channelReady[sessionId] = true;
  const banner = document.getElementById('connBanner');
  if (banner) banner.remove();
  const inputBar = document.getElementById('inputBar');
  if (inputBar) inputBar.classList.remove('input-disabled');
  const inputField = document.getElementById('sessionInput');
  if (inputField) { inputField.disabled = false; inputField.placeholder = 'Send command or input…'; }
  showToast('MCP connection skipped — using tmux only', 'info', 3000);
}

function handleChannelReadyEvent(sessionId) {
  state.channelReady[sessionId] = true;
  // If viewing this session, re-render to show channel tab and dismiss banner
  if (state.activeView === 'session-detail' && state.activeSession === sessionId) {
    renderSessionDetail(sessionId);
  }
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
  const oldState = idx >= 0 ? state.sessions[idx].state : null;
  if (idx >= 0) {
    state.sessions[idx] = sess;
  } else {
    state.sessions.push(sess);
  }
  onSessionsUpdated();
  // If viewing alerts and session state changed, refresh to update quick-input buttons
  if (state.activeView === 'alerts' && oldState !== sess.state) {
    renderAlertsView();
  }
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

  // If currently viewing this session, append to fallback div (xterm.js uses raw_output channel)
  if (state.activeView === 'session-detail' && state.activeSession === sessionId) {
    if (state.terminal) {
      // xterm.js is active — raw_output handles rendering; skip div append
    } else {
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
    // Dismiss connection banner and enable input when ready message detected
    const connBannerEl = document.getElementById('connBanner');
    if (connBannerEl) {
      const text = lines.map(l => stripAnsi(l)).join('\n');
      if (text.includes('Listening for channel') || text.includes('Channel: connected') ||
          text.includes('[opencode-acp] server ready') || text.includes('[opencode-acp] session')) {
        state.channelReady[sessionId] = true;
        connBannerEl.remove();
        // Enable the input bar
        const inputBar = document.getElementById('inputBar');
        if (inputBar) inputBar.classList.remove('input-disabled');
        const inputField = document.getElementById('sessionInput');
        if (inputField) {
          inputField.disabled = false;
          inputField.placeholder = 'Send message…';
        }
        // Re-render send button
        const sess = state.sessions.find(s => s.full_id === sessionId);
        const mode = sess ? getSessionMode(sess.llm_backend || '') : 'tmux';
        const wrap = document.getElementById('sendBtnWrap');
        if (!wrap && inputBar) {
          // Add send button
          const btnSpan = document.createElement('span');
          btnSpan.id = 'sendBtnWrap';
          if (mode === 'channel') {
            btnSpan.innerHTML = state.activeOutputTab === 'channel'
              ? `<button class="send-btn send-btn-channel" onclick="sendChannelMessage()" title="Send via MCP channel">&#9654; ch</button>`
              : `<button class="send-btn send-btn-tmux" onclick="sendSessionInputDirect()" title="Send via tmux">&#9654; tmux</button>`;
          } else {
            btnSpan.innerHTML = `<button class="send-btn" onclick="sendSessionInput()">&#9658;</button>`;
          }
          inputBar.appendChild(btnSpan);
        }
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
    destroyXterm(); // clean up terminal when leaving session detail

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

  const now = Date.now();
  const RECENT_MS = 5 * 60 * 1000; // 5 minutes
  const active = state.sessions.filter(s => !DONE_STATES.has(s.state));
  const recent = state.sessions.filter(s =>
    DONE_STATES.has(s.state) && s.updated_at && (now - new Date(s.updated_at).getTime()) < RECENT_MS
  );
  const history = state.sessions.filter(s => DONE_STATES.has(s.state));
  const filterText = (state.sessionFilter || '').toLowerCase();
  // Show active + recently completed sessions by default; "Show history" shows all
  let pool = state.showHistory ? state.sessions : [...active, ...recent];
  if (filterText) {
    pool = pool.filter(s =>
      (s.name || '').toLowerCase().includes(filterText) ||
      (s.task || '').toLowerCase().includes(filterText) ||
      (s.id || '').toLowerCase().includes(filterText) ||
      (s.llm_backend || '').toLowerCase().includes(filterText)
    );
  }
  const visible = sortSessionsByOrder(pool);

  const filterVal = escHtml(state.sessionFilter || '');
  // Collect unique backend types from all sessions for quick-filter badges
  const backendTypes = [...new Set(state.sessions.map(s => s.llm_backend).filter(Boolean))].sort();
  const backendBadges = backendTypes.map(bt => {
    const isActive = filterText === bt.toLowerCase();
    return `<button class="backend-filter-badge ${isActive ? 'active' : ''}" onclick="setBackendFilter('${escHtml(bt)}')" title="Filter: ${escHtml(bt)}">${escHtml(bt)}</button>`;
  }).join('');
  const toggleBtn = `<div class="sessions-toolbar">
    <div class="session-filter-wrap">
      <input type="text" class="session-filter-input" id="sessionFilterInput"
        placeholder="Filter sessions…" value="${filterVal}"
        oninput="state.sessionFilter=this.value;renderSessionsView();document.getElementById('sessionFilterInput').focus()" />
      ${filterText ? `<button class="session-filter-clear" onclick="state.sessionFilter='';renderSessionsView()">&#10005;</button>` : ''}
    </div>
    ${backendTypes.length > 1 ? `<div class="backend-filter-badges">${backendBadges}</div>` : ''}
    <span id="schedBadge" style="display:none;"></span>
    <button class="btn-toggle-history ${state.showHistory ? 'active' : ''}" onclick="toggleHistory()">
      ${state.showHistory ? 'Hide' : 'Show'} history (${history.length})
    </button>
  </div>`;

  if (visible.length === 0 && active.length === 0 && recent.length === 0) {
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

  // Restore filter input focus and cursor position
  if (filterText) {
    const fi = document.getElementById('sessionFilterInput');
    if (fi) { fi.focus(); fi.setSelectionRange(fi.value.length, fi.value.length); }
  }
  // Load pending schedule badge
  loadGlobalScheduleBadge();
}

function loadGlobalScheduleBadge() {
  const badge = document.getElementById('schedBadge');
  if (!badge) return;
  apiFetch('/api/schedules?state=pending').then(r => r.json()).then(items => {
    if (!items || items.length === 0) {
      badge.style.display = 'none';
      return;
    }
    badge.style.display = 'inline';
    badge.innerHTML = `<button class="backend-filter-badge" onclick="toggleGlobalScheduleDropdown()" title="Pending schedules" style="position:relative;">
      &#128339; ${items.length}
    </button>
    <div id="globalSchedDropdown" style="display:none;position:absolute;right:0;top:100%;z-index:50;background:var(--bg2);border:1px solid var(--border);border-radius:8px;padding:8px;min-width:280px;max-height:200px;overflow-y:auto;box-shadow:0 4px 12px rgba(0,0,0,0.3);">
      ${items.map(sc => {
        const when = sc.run_at ? new Date(sc.run_at).toLocaleString() : 'on input';
        const label = sc.type === 'new_session' && sc.deferred_session ? 'NEW: ' + escHtml(sc.deferred_session.name || '') : escHtml(sc.session_id);
        return `<div style="display:flex;justify-content:space-between;align-items:center;padding:3px 0;font-size:11px;border-bottom:1px solid var(--border);">
          <span style="color:var(--accent2);">${label}</span>
          <span style="color:var(--text2);margin:0 6px;">${when}</span>
          <button class="btn-icon" style="font-size:9px;color:var(--error);" onclick="event.stopPropagation();cancelSchedule('${sc.id}','');loadGlobalScheduleBadge()">&#10005;</button>
        </div>`;
      }).join('')}
    </div>`;
  }).catch(() => { badge.style.display = 'none'; });
}

function toggleGlobalScheduleDropdown() {
  const dd = document.getElementById('globalSchedDropdown');
  if (dd) dd.style.display = dd.style.display === 'none' ? 'block' : 'none';
}

function setBackendFilter(backend) {
  // Toggle: click same badge to clear, click different to set
  if ((state.sessionFilter || '').toLowerCase() === backend.toLowerCase()) {
    state.sessionFilter = '';
  } else {
    state.sessionFilter = backend;
  }
  renderSessionsView();
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
  const isActive = !DONE_STATES.has(sess.state);
  const isWaiting = sess.state === 'waiting_input';

  // Action icons inline in header
  let actions = '';
  if (isActive) {
    actions += `<button class="btn-stop" style="font-size:10px;padding:2px 6px;" onclick="event.stopPropagation();killSession('${escHtml(fullId)}')" title="Stop">&#9632; Stop</button>`;
    if (isWaiting) {
      actions += `<button class="btn-icon card-action" onclick="event.stopPropagation();showCardCmds('${escHtml(fullId)}')" title="Quick commands">&#9654;</button>`;
    }
  } else if (DONE_STATES.has(sess.state)) {
    actions += `<button class="btn-icon card-action" onclick="event.stopPropagation();restartSession('${escHtml(fullId)}')" title="Restart">&#8635;</button>`;
    actions += `<button class="btn-icon card-action" onclick="event.stopPropagation();deleteSession('${escHtml(fullId)}')" title="Delete">&#128465;</button>`;
  }

  // Waiting-input prompt and expandable commands
  let waitingRow = '';
  if (isWaiting) {
    const prompt = sess.last_prompt ? escHtml(lastPromptLine(sess.last_prompt)) : 'Input needed';
    waitingRow = `<div class="card-waiting-row" onclick="event.stopPropagation()">
      <span class="card-waiting-label">${prompt}</span>
    </div>
    <div id="cardCmds-${escHtml(shortId)}" class="card-cmds-popup" style="display:none;" onclick="event.stopPropagation()"></div>`;
  }

  return `
    <div class="session-card ${stateClass}" draggable="true" data-full-id="${escHtml(fullId)}"
         onclick="navigate('session-detail', '${escHtml(fullId)}')"
         ondragstart="sessionDragStart(event,'${escHtml(fullId)}')"
         ondragover="sessionDragOver(event)"
         ondrop="sessionDrop(event,'${escHtml(fullId)}')"
         ondragend="sessionDragEnd(event)">
      <div class="session-card-header">
        <span class="id">${escHtml(shortId)}</span>
        <span class="state ${badgeClass}">${escHtml(sess.state || 'unknown')}</span>
        ${backend ? `<span class="backend-badge" style="font-size:10px;" title="${escHtml(backend)}">${escHtml(backend)}</span>` : ''}
        <span class="time">${escHtml(ago)}</span>
        <span class="card-actions" onclick="event.stopPropagation()">${actions}</span>
        <span class="drag-handle" onclick="event.stopPropagation()" title="Drag to reorder">&#8942;&#8942;</span>
      </div>
      <div class="task">${escHtml(taskText)}</div>
      ${waitingRow}
    </div>`;
}

function showCardCmds(fullId) {
  const sess = state.sessions.find(s => s.full_id === fullId);
  const shortId = sess ? sess.id : fullId.split('-').pop();
  const el = document.getElementById('cardCmds-' + shortId);
  if (!el) return;
  if (el.style.display !== 'none') { el.style.display = 'none'; return; }
  // Built-in quick keys
  let html = `<button class="quick-btn" onclick="event.stopPropagation();cardSendCmd('${escHtml(fullId)}','')">Enter</button>` +
    `<button class="quick-btn" onclick="event.stopPropagation();cardSendCmd('${escHtml(fullId)}','y')">y</button>` +
    `<button class="quick-btn" onclick="event.stopPropagation();cardSendCmd('${escHtml(fullId)}','n')">n</button>` +
    `<button class="quick-btn" onclick="event.stopPropagation();cardSendKey('${escHtml(fullId)}','Up')">&#9650;</button>` +
    `<button class="quick-btn" onclick="event.stopPropagation();cardSendKey('${escHtml(fullId)}','Down')">&#9660;</button>` +
    `<button class="quick-btn" onclick="event.stopPropagation();cardSendKey('${escHtml(fullId)}','Escape')">Esc</button>`;
  // Fetch saved commands and append
  fetch('/api/commands', { headers: tokenHeader() })
    .then(r => r.ok ? r.json() : [])
    .then(cmds => {
      if (cmds && cmds.length) {
        html += '<span style="color:var(--text2);font-size:10px;margin:0 4px;">|</span>';
        html += cmds.map(c => {
          const safeCmd = escHtml(JSON.stringify(c.command));
          return `<button class="quick-btn" onclick="event.stopPropagation();cardSendCmd('${escHtml(fullId)}',${safeCmd})">${escHtml(c.name)}</button>`;
        }).join('');
      }
      el.innerHTML = html;
      el.style.display = '';
    });
}

function cardSendKey(fullId, keyName) {
  send('command', { text: `sendkey ${fullId}: ${keyName}` });
  showToast('Sent: ' + keyName, 'success', 1500);
}

function cardSendCmd(fullId, cmd) {
  if (cmd === '\n' || cmd === '') {
    send('send_input', { session_id: fullId, text: '' });
  } else {
    send('send_input', { session_id: fullId, text: cmd });
  }
  showToast('Sent', 'success', 1500);
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

  const nameText = sess ? (sess.name || '') : '';
  const displayTitle = nameText || taskText || '(no task)';
  const backendText = sess ? (sess.llm_backend || '') : '';
  const projectDir = sess ? (sess.project_dir || '') : '';
  const sessionMode = getSessionMode(backendText);
  const isActive = stateText === 'running' || stateText === 'waiting_input' || stateText === 'rate_limited';
  const isDone = stateText === 'complete' || stateText === 'failed' || stateText === 'killed';

  const needsBanner = isWaiting
    ? `<div class="needs-input-banner">Waiting for input${sess && sess.last_prompt ? ': ' + escHtml(lastPromptLine(sess.last_prompt)) : ''}</div>`
    : '';

  // Connection status banner for channel/ACP mode sessions.
  // Also determines whether input should be disabled until connection is established.
  let connBanner = '';
  let connReady = true;
  if (isActive && (sessionMode === 'channel' || sessionMode === 'acp')) {
    // Check cached ready state first (survives view navigation)
    if (state.channelReady[sessionId]) {
      connReady = true;
    } else {
      const outputText = lines.map(l => stripAnsi(l)).join('\n');
      const channelOK = outputText.includes('Listening for channel') || outputText.includes('Channel: connected');
      const acpOK = outputText.includes('[opencode-acp] server ready') || outputText.includes('[opencode-acp] session');
      connReady = sessionMode === 'channel' ? channelOK : acpOK;
      if (connReady) state.channelReady[sessionId] = true;
    }
    if (!connReady) {
      // Show banner but do NOT disable input if session needs user input
      const modeLabel = sessionMode === 'channel' ? 'MCP channel' : 'ACP server';
      connBanner = `<div class="conn-status-banner" id="connBanner">
        <span class="conn-spinner"></span> Establishing ${modeLabel} connection…${isWaiting ? ' Accept prompt below to continue.' : ''}
        <button class="btn-icon" style="margin-left:auto;font-size:11px;opacity:0.7;" onclick="dismissConnBanner('${escHtml(sessionId)}')" title="Dismiss — use tmux only">✕</button>
      </div>`;
      if (isWaiting) connReady = true; // allow input for consent prompts
    }
  }

  const actionButtons = isActive
    ? `<button class="btn-stop" onclick="killSession('${escHtml(sessionId)}')" title="Stop session">&#9632; Stop</button>`
    : isDone
    ? `<button class="btn-restart" onclick="restartSession('${escHtml(sessionId)}')" title="Restart with same task">&#8635; Restart</button>
       <button class="btn-delete" onclick="deleteSession('${escHtml(sessionId)}')" title="Delete session">&#128465; Delete</button>`
    : '';

  // Dual output areas: channel tab only shown when channel is actually connected
  const showChannel = sessionMode === 'channel' && connReady;
  const outputAreaHtml = showChannel
    ? `<div class="output-tabs">
        <button class="output-tab active" id="tabTmux" onclick="switchOutputTab('tmux')">Tmux</button>
        <button class="output-tab" id="tabChannel" onclick="switchOutputTab('channel')">Channel</button>
        <button class="btn-icon" style="font-size:12px;margin-left:auto;opacity:0.6;" onclick="showChannelHelp()" title="Channel commands">?</button>
      </div>
      <div class="output-area output-area-tmux" id="outputAreaTmux"></div>
      <div class="output-area output-area-channel" id="outputAreaChannel" style="display:none">${channelHtml}</div>`
    : `<div class="output-area output-area-tmux" id="outputAreaTmux"></div>`;

  // For channel mode, pick the initial send button based on active tab (only when channel connected)
  const sendBtnHtml = isActive
    ? (showChannel && !isWaiting
      ? `<span id="sendBtnWrap">${state.activeOutputTab === 'channel'
          ? `<button class="send-btn send-btn-channel" onclick="sendChannelMessage()" title="Send via MCP channel">&#9654; ch</button>`
          : `<button class="send-btn send-btn-tmux" onclick="sendSessionInputDirect()" title="Send via tmux">&#9654; tmux</button>`
        }</span>`
      : `<button class="send-btn" onclick="sendSessionInput()">&#9658;</button>`)
    + (isActive ? `<button class="btn-icon sched-input-btn" onclick="showScheduleInputPopup('${escHtml(sessionId)}')" title="Schedule input for later">&#128339;</button>` : '')
    : '';

  view.innerHTML = `
    <div class="session-detail">
      <div class="session-info-bar">
        <div class="meta">
          ${backendText ? `<span class="backend-badge">${escHtml(backendText)}</span>` : ''}
          <span class="mode-badge mode-${sessionMode}">${sessionMode}</span>
          <span class="state detail-state-badge ${badgeClass}">${escHtml(stateText)}</span>
          <span id="actionBtns">${actionButtons}</span>
          <button class="detail-pill-btn" onclick="toggleSessionTimeline('${escHtml(sessionId)}')" title="Show event timeline">&#128336; Timeline</button>
        </div>
      </div>
      <div id="sessionSchedules" class="session-schedules" style="display:none;"></div>
      ${connBanner}
      ${needsBanner}
      ${outputAreaHtml}
      ${isWaiting ? `<div class="quick-input-row">
        <button class="quick-btn" onclick="sendQuickInput('y')">y</button>
        <button class="quick-btn" onclick="sendQuickInput('n')">n</button>
        <button class="quick-btn" onclick="sendQuickInput('')">Enter</button>
        <button class="quick-btn" onclick="sendQuickInput('__up__')" title="Arrow up">&#9650;</button>
        <button class="quick-btn" onclick="sendQuickInput('__down__')" title="Arrow down">&#9660;</button>
        <button class="quick-btn" onclick="sendQuickInput('__esc__')" title="Escape">Esc</button>
        <button class="quick-btn quick-btn-danger" onclick="sendQuickInput('__ctrlc__')">Ctrl‑C</button>
      </div>` : ''}
      ${isActive && backendText !== 'opencode-prompt' ? `<div id="savedCmdsQuick" class="saved-cmds-quick"></div>` : ''}
      ${isActive && backendText !== 'opencode-prompt' ? `<div class="input-bar${isWaiting ? ' needs-input' : ''}${!connReady ? ' input-disabled' : ''}" id="inputBar">
        <div class="input-field-wrap">
          <div class="input-label" style="display:${isWaiting ? 'block' : 'none'}">Input Required</div>
          <input
            type="text"
            class="input-field"
            id="sessionInput"
            placeholder="${!connReady ? 'Waiting for connection…' : isWaiting ? 'Type your response…' : sessionMode === 'channel' ? 'Send message…' : 'Send command or input…'}"
            autocomplete="off"
            autocorrect="off"
            spellcheck="false"
            ${!connReady ? 'disabled' : ''}
          />
        </div>
        ${connReady ? sendBtnHtml : ''}
      </div>` : ''}
    </div>`;

  // Initialize xterm.js terminal for tmux output — use raw buffer (ANSI preserved)
  const rawLines = state.rawOutputBuffer[sessionId] || lines;
  initXterm(sessionId, rawLines);

  // Load saved commands quick panel and pending schedules
  if (isActive) {
    loadSavedCmdsQuick(sessionId);
  }
  loadSessionSchedules(sessionId);

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

function destroyXterm() {
  if (state.terminal) {
    state.terminal.dispose();
    state.terminal = null;
    state.termFitAddon = null;
  }
}

function initXterm(sessionId, bufferedLines) {
  destroyXterm();
  const container = document.getElementById('outputAreaTmux');
  if (!container || typeof Terminal === 'undefined') {
    // Fallback: render as plain text if xterm.js not loaded
    if (container && bufferedLines) {
      container.innerHTML = bufferedLines.map(l => `<div class="output-line">${escHtml(stripAnsi(l))}</div>`).join('');
      container.scrollTop = container.scrollHeight;
    }
    return;
  }

  const term = new Terminal({
    cursorBlink: true,
    fontSize: 11,
    fontFamily: "'JetBrains Mono', 'Fira Code', monospace",
    cols: 120,
    rows: 30,
    allowProposedApi: true,
    theme: {
      background: '#0f1117',
      foreground: '#e2e8f0',
      cursor: '#a855f7',
      cursorAccent: '#0f1117',
      selectionBackground: 'rgba(168,85,247,0.3)',
      black: '#1a1d27',
      red: '#ef4444',
      green: '#10b981',
      yellow: '#f59e0b',
      blue: '#3b82f6',
      magenta: '#a855f7',
      cyan: '#06b6d4',
      white: '#e2e8f0',
      brightBlack: '#94a3b8',
      brightRed: '#f87171',
      brightGreen: '#34d399',
      brightYellow: '#fbbf24',
      brightBlue: '#60a5fa',
      brightMagenta: '#c084fc',
      brightCyan: '#22d3ee',
      brightWhite: '#f8fafc',
    },
    scrollback: 5000,
  });

  let fitAddon = null;
  if (typeof FitAddon !== 'undefined') {
    fitAddon = new FitAddon.FitAddon();
    term.loadAddon(fitAddon);
  }

  term.open(container);
  if (fitAddon) {
    try { fitAddon.fit(); } catch(e) {}
  }

  // Write buffered output
  if (bufferedLines && bufferedLines.length > 0) {
    term.write(bufferedLines.join('\r\n') + '\r\n');
  }

  // Handle resize
  if (fitAddon) {
    const resizeObs = new ResizeObserver(() => {
      try { fitAddon.fit(); } catch(e) {}
    });
    resizeObs.observe(container);
  }

  // Interactive keyboard mode — keystrokes sent to tmux via sendkey
  term.onData(data => {
    if (state.activeSession) {
      send('send_input', { session_id: state.activeSession, text: data, raw: true });
    }
  });

  state.terminal = term;
  state.termFitAddon = fitAddon;
}

function loadSessionSchedules(sessionId) {
  const el = document.getElementById('sessionSchedules');
  if (!el) return;
  apiFetch('/api/schedules?session_id=' + encodeURIComponent(sessionId) + '&state=pending')
    .then(r => r.json())
    .then(items => {
      if (!items || items.length === 0) {
        el.style.display = 'none';
        return;
      }
      el.style.display = 'block';
      const rows = items.map(sc => {
        const when = sc.run_at ? new Date(sc.run_at).toLocaleString() : 'on input';
        return `<div class="sched-item" style="display:flex;justify-content:space-between;align-items:center;padding:4px 8px;font-size:11px;">
          <span style="color:var(--text2);">${escHtml(when)}</span>
          <span style="flex:1;margin:0 8px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;">${escHtml(sc.command)}</span>
          <button class="btn-icon" style="font-size:10px;color:var(--error);" onclick="cancelSchedule('${sc.id}','${escHtml(sessionId)}')" title="Cancel">&#10005;</button>
        </div>`;
      }).join('');
      el.innerHTML = `<div style="font-size:10px;color:var(--text2);text-transform:uppercase;letter-spacing:.5px;padding:4px 10px;border-bottom:1px solid var(--border);">Scheduled (${items.length})</div>${rows}`;
    })
    .catch(() => { el.style.display = 'none'; });
}

function showScheduleInputPopup(sessionId) {
  const existing = document.getElementById('schedInputPopup');
  if (existing) { existing.remove(); return; }
  const inputEl = document.getElementById('sessionInput');
  const prefill = inputEl ? inputEl.value : '';
  const popup = document.createElement('div');
  popup.id = 'schedInputPopup';
  popup.className = 'backend-config-overlay';
  popup.innerHTML = `<div class="backend-config-popup" style="max-width:340px;">
    <div class="backend-config-header">
      <strong>Schedule Input</strong>
      <button class="btn-icon" onclick="document.getElementById('schedInputPopup').remove()">&#10005;</button>
    </div>
    <div class="backend-config-body" style="padding:12px;">
      <div class="form-group">
        <label style="font-size:11px;color:var(--text2);">Command to send</label>
        <input type="text" id="schedInputText" class="form-input" value="${escHtml(prefill)}" placeholder="e.g. continue" />
      </div>
      <div class="form-group" style="margin-top:8px;">
        <label style="font-size:11px;color:var(--text2);">When</label>
        <input type="text" id="schedInputTime" class="form-input" placeholder="in 30 minutes" />
        <div style="font-size:9px;color:var(--text2);margin-top:2px;">Examples: in 30m, at 14:00, tomorrow at 9am, next monday at 10:00</div>
      </div>
      <div style="display:flex;gap:6px;flex-wrap:wrap;margin-top:6px;">
        <button class="btn-secondary" style="font-size:11px;" onclick="document.getElementById('schedInputTime').value='in 5 minutes'">5 min</button>
        <button class="btn-secondary" style="font-size:11px;" onclick="document.getElementById('schedInputTime').value='in 30 minutes'">30 min</button>
        <button class="btn-secondary" style="font-size:11px;" onclick="document.getElementById('schedInputTime').value='in 1 hour'">1 hr</button>
        <button class="btn-secondary" style="font-size:11px;" onclick="document.getElementById('schedInputTime').value='on input'">On input</button>
      </div>
      <button class="btn-primary" style="margin-top:12px;width:100%;" onclick="submitScheduleInput('${escHtml(sessionId)}')">Schedule</button>
    </div>
  </div>`;
  popup.addEventListener('click', e => { if (e.target === popup) popup.remove(); });
  document.body.appendChild(popup);
  document.getElementById('schedInputText').focus();
}

function submitScheduleInput(sessionId) {
  const text = document.getElementById('schedInputText')?.value || '';
  const when = document.getElementById('schedInputTime')?.value || '';
  if (!text) { showToast('Enter a command to schedule', 'warning'); return; }
  const body = { session_id: sessionId, command: text, run_at: when || '' };
  apiFetch('/api/schedules', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  }).then(r => {
    if (r.ok) {
      showToast('Scheduled', 'success', 1500);
      document.getElementById('schedInputPopup')?.remove();
      loadSessionSchedules(sessionId);
    } else {
      r.text().then(t => showToast('Schedule failed: ' + t, 'error'));
    }
  }).catch(err => showToast('Schedule failed: ' + err.message, 'error'));
}

function cancelSchedule(schedId, sessionId) {
  apiFetch('/api/schedules?id=' + encodeURIComponent(schedId), { method: 'DELETE' })
    .then(() => {
      showToast('Schedule cancelled', 'success', 1500);
      if (sessionId) loadSessionSchedules(sessionId);
    })
    .catch(err => showToast('Cancel failed: ' + err.message, 'error'));
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
    if (sess.project_dir) {
      newSessionState.selectedDir = sess.project_dir;
      if (dirDisplay) dirDisplay.textContent = sess.project_dir;
    }
    if (resumeEl && sess.llm_session_id) {
      resumeEl.value = sess.llm_session_id;
    }
    // Select backend — retry after backends load since fetchBackends is async
    const selectBackend = () => {
      const sel = document.getElementById('backendSelect');
      if (!sel || !sess.llm_backend) return;
      for (const opt of sel.options) {
        if (opt.value === sess.llm_backend) {
          opt.selected = true;
          // Trigger onchange to update prompt-required state
          if (sel.onchange) sel.onchange();
          return;
        }
      }
      setTimeout(selectBackend, 200);
    };
    selectBackend();
    showToast('Pre-filled from previous session', 'success', 2000);
  }, 150);
}

function sendSessionInput() {
  const inputEl = document.getElementById('sessionInput');
  if (!inputEl) return;
  const text = inputEl.value; // Don't trim — empty string sends Enter
  const sendText = text || '\n'; // Empty input = send Enter key

  if (state.activeSession) {
    const sess = state.sessions.find(s => s.full_id === state.activeSession);
    if (sess && (sess.state === 'waiting_input' || sess.state === 'running' || sess.state === 'rate_limited')) {
      send('send_input', { session_id: state.activeSession, text: sendText });
    } else {
      send('command', { text: `send ${state.activeSession}: ${sendText}` });
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
    send('command', { text: `send ${state.activeSession}: \x03` });
  } else if (key === '__up__') {
    send('command', { text: `sendkey ${state.activeSession}: Up` });
  } else if (key === '__down__') {
    send('command', { text: `sendkey ${state.activeSession}: Down` });
  } else if (key === '__esc__') {
    send('command', { text: `sendkey ${state.activeSession}: Escape` });
  } else {
    send('send_input', { session_id: state.activeSession, text: key });
  }
}

function showChannelHelp() {
  const existing = document.getElementById('channelHelpPopup');
  if (existing) { existing.remove(); return; }
  const popup = document.createElement('div');
  popup.id = 'channelHelpPopup';
  popup.className = 'backend-config-overlay';
  popup.innerHTML = `<div class="backend-config-popup" style="max-width:380px;">
    <div class="backend-config-header">
      <strong>Channel Commands</strong>
      <button class="btn-icon" onclick="document.getElementById('channelHelpPopup').remove()">&#10005;</button>
    </div>
    <div class="backend-config-body" style="font-size:13px;line-height:1.6;">
      <p>The <b>Channel</b> tab communicates via MCP tool calls, bypassing tmux. Messages appear directly in the LLM's context.</p>
      <p style="margin-top:8px;"><b>You can send:</b></p>
      <ul style="padding-left:16px;margin:4px 0;">
        <li>Free-text instructions or follow-up questions</li>
        <li>Code review feedback or corrections</li>
        <li>Task reprioritization or scope changes</li>
      </ul>
      <p style="margin-top:8px;"><b>Claude slash commands (tmux tab):</b></p>
      <ul style="padding-left:16px;margin:4px 0;">
        <li><code>/mcp</code> — restart MCP servers</li>
        <li><code>/effort</code> — toggle effort level</li>
        <li><code>/help</code> — claude help</li>
        <li><code>/compact</code> — compact conversation</li>
        <li><code>/clear</code> — clear screen</li>
      </ul>
      <p style="margin-top:8px;"><b>LLM can send back:</b></p>
      <ul style="padding-left:16px;margin:4px 0;">
        <li>Progress updates and status messages</li>
        <li>Questions requiring your input</li>
        <li>Completion notifications</li>
      </ul>
      <p style="margin-top:8px;color:var(--text2);font-size:12px;">Channel replies appear as amber lines. Tmux tab shows raw terminal output. Use Channel for structured communication, Tmux for direct terminal access.</p>
    </div>
  </div>`;
  popup.addEventListener('click', e => { if (e.target === popup) popup.remove(); });
  document.body.appendChild(popup);
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
        cmds.map(c => {
          const safeCmd = escHtml(JSON.stringify(c.command));
          return `<button class="quick-cmd-btn" onclick="sendSavedCmd(${safeCmd})" title="${escHtml(c.name || c.command)}">${escHtml(c.name || c.command)}</button>`;
        }).join('');
    })
    .catch(() => {});
}

function sendSavedCmd(cmd) {
  if (!state.activeSession) return;
  // For newline/enter command, use send_input with empty string (sends Enter key only)
  if (cmd === '\n' || cmd === '') {
    send('send_input', { session_id: state.activeSession, text: '' });
  } else {
    send('send_input', { session_id: state.activeSession, text: cmd });
  }
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
        <div class="form-group">
          <label for="sessionNameInput">Session name</label>
          <input
            id="sessionNameInput"
            class="form-input"
            type="text"
            placeholder="e.g. Auth refactor"
          />
        </div>
        <details class="create-form-details" id="taskDetailsSection" style="margin-bottom:12px;">
          <summary class="create-form-summary" id="taskDetailsSummary" style="padding:4px 0;">+ Task description (optional)</summary>
          <div style="margin-top:8px;">
            <textarea
              id="taskInput"
              class="form-textarea"
              placeholder="e.g. Add unit tests to internal/session/manager.go (leave empty for an interactive shell session)"
              rows="5"
            ></textarea>
          </div>
        </details>
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
            <span id="selectedDirDisplay" class="dir-display dir-display-clickable" onclick="openDirBrowser()" title="Click to browse">~/</span>
          </div>
        </div>
        <div id="dirBrowser" class="dir-browser" style="display:none">
          <div id="dirBrowserContent"></div>
        </div>
        <div class="form-group" id="resumeIdGroup">
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
        <div class="form-group" style="display:flex;flex-direction:row;gap:16px;align-items:center;flex-wrap:wrap;">
          <label style="display:flex;align-items:center;gap:6px;font-size:12px;">
            <input type="checkbox" id="gitInitToggle" /> Auto git init
          </label>
          <label style="display:flex;align-items:center;gap:6px;font-size:12px;">
            <input type="checkbox" id="gitCommitToggle" checked /> Auto git commit
          </label>
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

  // Focus the name field by default; Cmd+Enter submits from task textarea when expanded
  const nameInput = document.getElementById('sessionNameInput');
  if (nameInput) {
    const isTouch = navigator.maxTouchPoints > 0 || window.matchMedia('(pointer:coarse)').matches;
    if (!isTouch) nameInput.focus();
    nameInput.addEventListener('keydown', e => {
      if (e.key === 'Enter') {
        e.preventDefault();
        submitNewSession();
      }
    });
  }
  const taskInput = document.getElementById('taskInput');
  if (taskInput) {
    taskInput.addEventListener('keydown', e => {
      if (e.key === 'Enter' && e.metaKey) {
        submitNewSession();
      }
    });
  }

  // Load backends and session backlog
  fetchBackends();
  renderSessionBacklog();

  // Set git toggles from config defaults
  fetch('/api/config', { headers: tokenHeader() })
    .then(r => r.ok ? r.json() : null)
    .then(cfg => {
      if (!cfg || !cfg.session) return;
      const gc = document.getElementById('gitCommitToggle');
      const gi = document.getElementById('gitInitToggle');
      if (gc) gc.checked = !!cfg.session.auto_git_commit;
      if (gi) gi.checked = !!cfg.session.auto_git_init;
    })
    .catch(() => {});
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
        const pr = typeof b === 'object' && b.prompt_required ? ' (prompt required)' : '';
        const sr = typeof b === 'object' && b.supports_resume ? 'true' : 'false';
        return `<option value="${escHtml(name)}"${selected} data-prompt-required="${typeof b === 'object' && b.prompt_required ? 'true' : 'false'}" data-supports-resume="${sr}">${escHtml(name)}${pr}</option>`;
      }).join('');
      // When backend changes, update prompt requirement
      sel.onchange = function() {
        const opt = sel.options[sel.selectedIndex];
        const pr = opt && opt.dataset.promptRequired === 'true';
        const sr = opt && opt.dataset.supportsResume === 'true';
        const taskDetails = document.getElementById('taskDetailsSection');
        const taskSummary = document.getElementById('taskDetailsSummary');
        const taskInput = document.getElementById('taskInput');
        const resumeGroup = document.getElementById('resumeIdGroup');
        if (pr) {
          if (taskDetails) taskDetails.open = true;
          if (taskSummary) taskSummary.textContent = 'Task / Prompt (required)';
          if (taskInput) { taskInput.required = true; taskInput.placeholder = 'Required — enter prompt for this backend'; }
        } else {
          if (taskSummary) taskSummary.textContent = '+ Task description (optional)';
          if (taskInput) { taskInput.required = false; taskInput.placeholder = 'e.g. Fix the auth bug in login.go'; }
        }
        // Show/hide resume field based on backend support
        if (resumeGroup) resumeGroup.style.display = sr ? '' : 'none';
      };
      sel.onchange(); // Set initial state
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

  // Validate prompt required
  if (backendSel) {
    const opt = backendSel.options[backendSel.selectedIndex];
    if (opt && opt.dataset.promptRequired === 'true' && !task) {
      showToast('This backend requires a prompt/task to start', 'error', 3000);
      taskInput.focus();
      return;
    }
  }

  const btn = document.querySelector('.btn-primary');
  if (btn) {
    btn.disabled = true;
    btn.textContent = 'Starting…';
  }

  const gitCommit = document.getElementById('gitCommitToggle');
  const gitInit = document.getElementById('gitInitToggle');
  const payload = {
    task,
    name: nameInput ? nameInput.value.trim() : '',
    backend: backendSel ? backendSel.value : '',
    project_dir: newSessionState.selectedDir || '',
    resume_id: resumeInput ? resumeInput.value.trim() : '',
    auto_git_commit: gitCommit ? gitCommit.checked : true,
    auto_git_init: gitInit ? gitInit.checked : false,
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
          ${settingsSectionHeader('backends', 'Communication Configuration')}
          <div id="settings-sec-backends" style="${secContent('backends')}">
            <div id="configStatus" style="color:var(--text2);font-size:13px;padding:4px 0;">Loading…</div>
            <div class="settings-row backend-row" style="margin-top:4px;justify-content:space-between;">
              <div class="settings-label backend-label" style="text-transform:capitalize;flex:1;">Signal Device</div>
              <div style="display:flex;align-items:center;gap:8px;">
                <span style="font-size:11px;" id="linkStatusText">Checking…</span>
                <span id="linkActionRow"><button class="btn-secondary backend-btn" style="font-size:11px;" onclick="startLinking()">Link Device</button></span>
              </div>
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
            <div id="llmConfigList" style="color:var(--text2);font-size:13px;">Loading…</div>
          </div>
        </div>

        <div class="settings-section">
          ${settingsSectionHeader('general', 'General Configuration')}
          <div id="settings-sec-general" style="${secContent('general')}">
            <div id="generalConfigList" style="color:var(--text2);font-size:13px;">Loading…</div>
          </div>
        </div>

        <div class="settings-section">
          ${settingsSectionHeader('detection', 'Detection Filters')}
          <div id="settings-sec-detection" style="${secContent('detection')}">
            <div id="detectionFiltersList"><div style="color:var(--text2);font-size:13px;">Loading…</div></div>
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
            <div class="settings-row" style="justify-content:space-between;">
              <div class="settings-label" title="When viewing a session, hide toast notifications about that session's state changes (reduces distraction while watching output)">Suppress toasts for active session</div>
              <label class="toggle-switch">
                <input type="checkbox" id="cfgSuppressToasts"
                  onchange="saveGeneralField('server.suppress_active_toasts', this.checked)" />
                <span class="toggle-slider"></span>
              </label>
            </div>
            <div class="settings-row" style="justify-content:space-between;">
              <div class="settings-label" title="Automatically restart the daemon after saving configuration changes that require a restart (host, port, TLS, binds). Skipped if config is encrypted without DATAWATCH_SECURE_PASSWORD.">Auto-restart daemon on config save</div>
              <label class="toggle-switch">
                <input type="checkbox" id="cfgAutoRestart"
                  onchange="saveGeneralField('server.auto_restart_on_config', this.checked)" />
                <span class="toggle-slider"></span>
              </label>
            </div>
          </div>
        </div>

        <div class="settings-section">
          ${settingsSectionHeader('schedules', 'Scheduled Events')}
          <div id="settings-sec-schedules" style="${secContent('schedules')}">
            <div id="schedulesList"><div style="color:var(--text2);font-size:13px;">Loading…</div></div>
          </div>
        </div>

        <div class="settings-section">
          ${settingsSectionHeader('cmds', 'Saved Commands')}
          <div id="settings-sec-cmds" style="${secContent('cmds')}">
            <div id="savedCmdsList"><div style="color:var(--text2);font-size:13px;">Loading…</div></div>
            <details class="create-form-details" style="padding:0 16px;">
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
            <details class="create-form-details" style="padding:0 16px;">
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
            <div class="settings-row">
              <div class="settings-label">MCP Tools</div>
              <div class="settings-value">
                <a href="/api/mcp/docs" target="_blank" style="color:var(--accent2);">/api/mcp/docs</a>
                <span style="color:var(--text2);font-size:11px;margin-left:6px;">(HTML) </span>
                <a href="/api/mcp/docs" target="_blank" style="color:var(--accent2);font-size:11px;" onclick="event.preventDefault();fetch('/api/mcp/docs',{headers:tokenHeader()}).then(r=>r.json()).then(d=>{const w=window.open('','_blank');w.document.write('<pre>'+JSON.stringify(d,null,2)+'</pre>')})">JSON</a>
              </div>
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
  loadSchedulesList();
  loadDetectionFilters();
  loadFilters();
  loadVersionInfo();
  loadLLMConfig();
  loadGeneralConfig();
  // Load notification toggle values from server config
  fetch('/api/config', { headers: tokenHeader() }).then(r => r.ok ? r.json() : null).then(cfg => {
    if (!cfg) return;
    const st = document.getElementById('cfgSuppressToasts');
    const ar = document.getElementById('cfgAutoRestart');
    if (st) st.checked = cfg.server?.suppress_active_toasts !== false;
    if (ar) ar.checked = !!cfg.server?.auto_restart_on_config;
  });
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
      // Map backend name to config key for enable/disable
      const cfgKeyMap = {
        'claude-code':'session','aider':'aider','goose':'goose','gemini':'gemini','ollama':'ollama',
        'opencode':'opencode','opencode-acp':'opencode-acp','opencode-prompt':'opencode-prompt','openwebui':'openwebui','shell':'shell'
      };
      el.innerHTML = backends.map(b => {
        const name = typeof b === 'string' ? b : b.name;
        const avail = typeof b === 'string' ? true : b.available;
        const enabled = typeof b === 'object' ? b.enabled : false;
        const ver = typeof b === 'object' && b.version ? ` <span style="color:var(--text2);font-size:11px;">${escHtml(b.version)}</span>` : '';
        const isDefault = name === data.active;
        const cfgKey = cfgKeyMap[name];

        if (!avail && !enabled) {
          return `<div class="settings-row backend-row" style="justify-content:space-between;">
            <div class="settings-label"><strong>${escHtml(name)}</strong></div>
            <span style="font-size:11px;color:var(--text2);">not configured</span>
            <button class="btn-secondary backend-btn" style="font-size:11px;" onclick="openLLMSetup('${escHtml(name)}')">Configure</button>
          </div>`;
        }

        const toggleKey = cfgKey ? cfgKey + '.enabled' : '';
        return `<div class="settings-row backend-row" style="justify-content:space-between;">
          <div class="settings-label" style="flex:1;">
            <strong>${escHtml(name)}</strong>${ver}
            ${isDefault ? ' <span style="color:var(--accent);font-size:10px;">(default)</span>' : ''}
          </div>
          ${cfgKey ? `<button class="btn-icon" style="font-size:12px;opacity:0.5;" onclick="openLLMSetup('${escHtml(name)}')" title="Edit configuration">✎</button>` : ''}
          <label class="toggle-switch" title="${enabled ? 'Enabled' : 'Disabled'}">
            <input type="checkbox" ${enabled ? 'checked' : ''} onchange="toggleLLM('${escHtml(toggleKey)}', this.checked, '${escHtml(name)}')" />
            <span class="toggle-slider"></span>
          </label>
        </div>`;
      }).join('') + `<div style="font-size:11px;color:var(--text2);padding:8px 12px;">
        Toggle enables/disables backends. The <strong>(default)</strong> backend is used for new sessions unless overridden.
        Change default via <code>session.llm_backend</code> in General Configuration.
      </div>`;
    })
    .catch(() => { if (el) el.textContent = 'Failed to load'; });
}

function toggleLLM(cfgKey, enabled, name) {
  if (!cfgKey) return;
  fetch('/api/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', ...tokenHeader() },
    body: JSON.stringify({ [cfgKey]: enabled }),
  })
    .then(r => {
      if (r.ok) {
        showToast(name + (enabled ? ' enabled' : ' disabled'), 'success', 2000);
        loadLLMConfig();
      } else showToast('Save failed', 'error');
    })
    .catch(() => showToast('Save failed', 'error'));
}

function openLLMSetup(name) {
  const section = LLM_CFG_SECTION[name];
  if (!section) { showToast('No config fields for ' + name, 'info'); return; }
  fetch('/api/config', { headers: tokenHeader() })
    .then(r => {
      if (!r.ok) throw new Error('HTTP ' + r.status);
      return r.json();
    })
    .then(cfg => {
      const fields = LLM_FIELDS[name] || [];
      const sectionCfg = cfg[section] || {};
      showBackendConfigPopup(section, sectionCfg, fields, name);
    })
    .catch(err => {
      console.error('openLLMSetup error:', err);
      showToast('Failed to load config: ' + err.message, 'error');
    });
}

function setActiveLLM(name) {
  fetch('/api/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', ...tokenHeader() },
    body: JSON.stringify({ 'session.llm_backend': name }),
  })
    .then(r => r.ok ? loadLLMConfig() : showToast('Save failed', 'error'))
    .catch(() => showToast('Save failed', 'error'));
}

// ── General Configuration ─────────────────────────────────────────────────────

const GENERAL_CONFIG_FIELDS = [
  { section: 'Session', fields: [
    { key: 'session.llm_backend', label: 'Default LLM backend', type: 'llm_select' },
    { key: 'session.max_sessions', label: 'Max concurrent sessions', type: 'number' },
    { key: 'session.input_idle_timeout', label: 'Input idle timeout (sec)', type: 'number' },
    { key: 'session.tail_lines', label: 'Tail lines', type: 'number' },
    { key: 'session.default_project_dir', label: 'Default project dir', type: 'dir_browse' },
    { key: 'session.root_path', label: 'File browser root path', type: 'dir_browse' },
    { key: 'session.skip_permissions', label: 'Claude skip permissions', type: 'toggle' },
    { key: 'session.channel_enabled', label: 'Claude channel mode', type: 'toggle' },
    { key: 'session.auto_git_init', label: 'Auto git init', type: 'toggle' },
    { key: 'session.auto_git_commit', label: 'Auto git commit', type: 'toggle' },
    { key: 'session.kill_sessions_on_exit', label: 'Kill sessions on exit', type: 'toggle' },
    { key: 'session.mcp_max_retries', label: 'MCP auto-retry limit', type: 'number' },
    { key: 'session.log_level', label: 'Log level', type: 'select', options: ['','info','debug','warn','error'] },
  ]},
  { section: 'Web Server', fields: [
    { key: 'server.enabled', label: 'Enabled', type: 'toggle' },
    { key: 'server.host', label: 'Bind interface', type: 'interface_select' },
    { key: 'server.port', label: 'Port', type: 'number' },
    { key: 'server.token', label: 'Bearer token', type: 'password' },
    { key: 'server.tls', label: 'TLS enabled', type: 'toggle' },
    { key: 'server.tls_port', label: 'TLS port (0=replace main port)', type: 'number' },
    { key: 'server.tls_auto_generate', label: 'TLS auto-generate cert', type: 'toggle' },
    { key: 'server.tls_cert', label: 'TLS cert path', type: 'text' },
    { key: 'server.tls_key', label: 'TLS key path', type: 'text' },
    { key: 'server.channel_port', label: 'Channel port (0=random)', type: 'number' },
  ]},
  { section: 'MCP Server', fields: [
    { key: 'mcp.enabled', label: 'Enabled (stdio)', type: 'toggle' },
    { key: 'mcp.sse_enabled', label: 'SSE enabled (HTTP)', type: 'toggle' },
    { key: 'mcp.sse_host', label: 'SSE bind interface', type: 'interface_select' },
    { key: 'mcp.sse_port', label: 'SSE port', type: 'number' },
    { key: 'mcp.token', label: 'SSE bearer token', type: 'password' },
    { key: 'mcp.tls_enabled', label: 'TLS enabled', type: 'toggle' },
    { key: 'mcp.tls_auto_generate', label: 'TLS auto-generate cert', type: 'toggle' },
    { key: 'mcp.tls_cert', label: 'TLS cert path', type: 'text' },
    { key: 'mcp.tls_key', label: 'TLS key path', type: 'text' },
  ]},
  { section: 'Auto-Update', fields: [
    { key: 'update.enabled', label: 'Enabled', type: 'toggle' },
    { key: 'update.schedule', label: 'Schedule', type: 'select', options: ['hourly','daily','weekly'] },
    { key: 'update.time_of_day', label: 'Time of day (HH:MM)', type: 'text' },
  ]},
];

function loadGeneralConfig() {
  const el = document.getElementById('generalConfigList');
  if (!el) return;
  Promise.all([
    fetch('/api/config', { headers: tokenHeader() }).then(r => r.ok ? r.json() : null),
    fetch('/api/backends', { headers: tokenHeader() }).then(r => r.ok ? r.json() : null),
    fetch('/api/interfaces', { headers: tokenHeader() }).then(r => r.ok ? r.json() : [])
  ]).then(([cfg, backendsData, interfaces]) => {
      if (!cfg) { el.textContent = 'Unavailable'; return; }
      const enabledBackends = (backendsData?.llm || []).filter(b => b.enabled).map(b => b.name);
      let html = '';
      for (const sec of GENERAL_CONFIG_FIELDS) {
        html += `<div style="font-size:11px;color:var(--text2);text-transform:uppercase;letter-spacing:.5px;padding:10px 16px 2px;">${escHtml(sec.section)}</div>`;
        for (const f of sec.fields) {
          const parts = f.key.split('.');
          const val = parts.reduce((o, k) => (o && o[k] !== undefined) ? o[k] : '', cfg);
          if (f.type === 'llm_select') {
            const opts = enabledBackends.map(n =>
              `<option value="${escHtml(n)}" ${String(val) === n ? 'selected' : ''}>${escHtml(n)}</option>`
            ).join('');
            html += `<div class="settings-row" style="justify-content:space-between;">
              <div class="settings-label">${escHtml(f.label)}</div>
              <select class="form-select general-cfg-input" onchange="saveGeneralField('${f.key}', this.value)">${opts}</select>
            </div>`;
          } else if (f.type === 'toggle') {
            const checked = !!val;
            html += `<div class="settings-row" style="justify-content:space-between;">
              <div class="settings-label">${escHtml(f.label)}</div>
              <label class="toggle-switch">
                <input type="checkbox" ${checked ? 'checked' : ''} onchange="saveGeneralField('${f.key}', this.checked)" />
                <span class="toggle-slider"></span>
              </label>
            </div>`;
          } else if (f.type === 'dir_browse') {
            const fid = 'cfg_dir_' + f.key.replace(/\./g, '_');
            const browserId = fid + '_browser';
            html += `<div class="settings-row" style="flex-direction:column;align-items:stretch;">
              <div class="settings-label">${escHtml(f.label)}</div>
              <div style="display:flex;gap:6px;align-items:center;margin-top:4px;">
                <input type="text" id="${fid}" class="form-input general-cfg-input" value="${escHtml(String(val || ''))}"
                  style="flex:1;" onchange="saveGeneralField('${f.key}', this.value)" />
                <button class="btn-secondary" style="font-size:11px;white-space:nowrap;" onclick="toggleSettingsDirBrowser('${fid}','${browserId}','${f.key}')">Browse</button>
              </div>
              <div id="${browserId}" class="dir-browser" style="display:none;margin-top:4px;"></div>
            </div>`;
          } else if (f.type === 'interface_select') {
            // Multi-select for network interfaces — current value may be comma-separated
            const currentVals = String(val || '0.0.0.0').split(',').map(s => s.trim());
            const ifaces = interfaces || [];
            const checkboxes = ifaces.map(iface => {
              const checked = currentVals.includes(iface.addr);
              return `<label class="iface-check" style="display:flex;align-items:center;gap:4px;font-size:11px;cursor:pointer;">
                <input type="checkbox" ${checked ? 'checked' : ''} value="${escHtml(iface.addr)}"
                  onchange="saveInterfaceField('${f.key}', this.closest('.iface-list'))" />
                <span style="font-family:monospace;">${escHtml(iface.label)}</span>
              </label>`;
            }).join('');
            html += `<div class="settings-row" style="flex-direction:column;align-items:stretch;">
              <div class="settings-label">${escHtml(f.label)}</div>
              <div class="iface-list" style="display:flex;flex-direction:column;gap:4px;margin-top:4px;padding:6px 8px;background:var(--bg);border:1px solid var(--border);border-radius:6px;">
                ${checkboxes}
              </div>
            </div>`;
          } else if (f.type === 'select') {
            const opts = (f.options || []).map(o =>
              `<option value="${escHtml(o)}" ${String(val) === o ? 'selected' : ''}>${escHtml(o)}</option>`
            ).join('');
            html += `<div class="settings-row" style="justify-content:space-between;">
              <div class="settings-label">${escHtml(f.label)}</div>
              <select class="form-select general-cfg-input" onchange="saveGeneralField('${f.key}', this.value)">${opts}</select>
            </div>`;
          } else {
            html += `<div class="settings-row" style="justify-content:space-between;">
              <div class="settings-label">${escHtml(f.label)}</div>
              <input type="${f.type}" class="form-input general-cfg-input" value="${escHtml(String(val || ''))}"
                onchange="saveGeneralField('${f.key}', ${f.type === 'number' ? 'Number(this.value)' : 'this.value'})" />
            </div>`;
          }
        }
      }
      html += `<div style="font-size:11px;color:var(--text2);padding:8px 12px;">
        Changes are saved immediately.
        <span id="restartHint" style="display:none;"> Restart required to apply changes.
          <button class="btn-link" style="font-size:11px;" onclick="restartDaemon()">Restart now</button>
        </span>
      </div>`;
      el.innerHTML = html;
    })
    .catch(() => { if (el) el.textContent = 'Config unavailable'; });
}

function saveInterfaceField(key, listEl) {
  const allBoxes = Array.from(listEl.querySelectorAll('input[type="checkbox"]'));
  const allBox = allBoxes.find(cb => cb.value === '0.0.0.0');
  const otherBoxes = allBoxes.filter(cb => cb.value !== '0.0.0.0');
  const checked = allBoxes.filter(cb => cb.checked);
  const values = checked.map(cb => cb.value);

  // Mutual exclusion: if "all" just got checked, uncheck others; if a specific one got checked, uncheck "all"
  if (allBox && allBox.checked && values.length > 1) {
    otherBoxes.forEach(cb => { cb.checked = false; });
  } else if (!allBox?.checked && values.some(v => v !== '0.0.0.0') && allBox) {
    allBox.checked = false;
  }

  const finalChecked = allBoxes.filter(cb => cb.checked).map(cb => cb.value);
  const val = finalChecked.join(',');
  if (!val) {
    showToast('Select at least one interface', 'warning', 2000);
    return;
  }
  saveGeneralField(key, val);
}

// Fields that require a daemon restart to take effect
const RESTART_FIELDS = new Set([
  'server.host', 'server.port', 'server.tls', 'server.tls_auto_generate', 'server.tls_cert', 'server.tls_key',
  'mcp.enabled', 'mcp.sse_enabled', 'mcp.sse_host', 'mcp.sse_port', 'mcp.tls_enabled',
  'dns_channel.enabled', 'dns_channel.listen', 'dns_channel.domain',
]);

function saveGeneralField(key, value) {
  fetch('/api/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', ...tokenHeader() },
    body: JSON.stringify({ [key]: value }),
  })
    .then(r => {
      if (r.ok) {
        showToast('Saved', 'success', 1500);
        // Update cached state for settings that affect UI behavior
        if (key === 'server.suppress_active_toasts') state.suppressActiveToasts = !!value;
        if (key === 'server.auto_restart_on_config') state.autoRestartOnConfig = !!value;
        // Show restart hint if this field requires a restart
        if (RESTART_FIELDS.has(key)) {
          const hint = document.getElementById('restartHint');
          if (hint) hint.style.display = 'inline';
        }
        // Auto-restart if configured
        if (state.autoRestartOnConfig && RESTART_FIELDS.has(key)) {
          triggerAutoRestart();
        }
      } else {
        showToast('Save failed', 'error');
      }
    })
    .catch(() => showToast('Save failed', 'error'));
}

function triggerAutoRestart() {
  // Check if encrypted without env password — warn instead of restarting
  apiFetch('/api/health').then(r => r.json()).then(data => {
    if (data.encrypted && !data.has_env_password) {
      showToast('Restart skipped: encrypted config requires DATAWATCH_SECURE_PASSWORD env variable for auto-restart', 'warning', 6000);
      return;
    }
    setTimeout(() => {
      showToast('Restarting daemon to apply changes…', 'info', 3000);
      restartDaemon();
    }, 500);
  }).catch(() => {
    // Can't check — restart anyway
    setTimeout(() => {
      showToast('Restarting daemon to apply changes…', 'info', 3000);
      restartDaemon();
    }, 500);
  });
}

function toggleSettingsDirBrowser(inputId, browserId, cfgKey) {
  const browser = document.getElementById(browserId);
  if (!browser) return;
  if (browser.style.display !== 'none') {
    browser.style.display = 'none';
    return;
  }
  browser.style.display = 'block';
  const input = document.getElementById(inputId);
  const startPath = (input && input.value) ? input.value : '~';
  loadSettingsDirContents(startPath, inputId, browserId, cfgKey);
}

function loadSettingsDirContents(path, inputId, browserId, cfgKey) {
  const browser = document.getElementById(browserId);
  if (!browser) return;
  browser.innerHTML = '<div style="color:var(--text2);padding:8px;font-size:12px;">Loading…</div>';
  fetch('/api/files?path=' + encodeURIComponent(path || '~'), { headers: tokenHeader() })
    .then(r => r.json())
    .then(data => {
      const currentPath = data.path || path;
      const entries = (data.entries || []).filter(e => e.is_dir).map(e =>
        `<div class="dir-entry" data-path="${escHtml(e.path)}">
          <span class="dir-icon">${e.is_link ? '🔗' : (e.name === '..' ? '⬆' : '📁')}</span>
          <span>${escHtml(e.name)}</span>
        </div>`
      ).join('');
      browser.innerHTML = `<div class="dir-current">${escHtml(currentPath)}</div>` +
        `<button class="btn-secondary dir-select-btn" data-select="${escHtml(currentPath)}">&#10003; Use This Folder</button>` +
        (entries || '<div style="color:var(--text2);padding:8px;font-size:12px;">No subdirectories</div>');
      browser.onclick = function(ev) {
        const entry = ev.target.closest('.dir-entry');
        const selBtn = ev.target.closest('[data-select]');
        if (entry && entry.dataset.path) {
          loadSettingsDirContents(entry.dataset.path, inputId, browserId, cfgKey);
        } else if (selBtn && selBtn.dataset.select) {
          const input = document.getElementById(inputId);
          if (input) input.value = selBtn.dataset.select;
          saveGeneralField(cfgKey, selBtn.dataset.select);
          browser.style.display = 'none';
          showToast('Directory set', 'success', 1500);
        }
      };
    })
    .catch(() => {
      browser.innerHTML = '<div class="dir-error">Cannot read directory</div>';
    });
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
    webhook: ['addr'], dns_channel: ['domain', 'secret'],
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
      const services = ['telegram', 'discord', 'slack', 'matrix', 'ntfy', 'email', 'twilio', 'github_webhook', 'webhook', 'dns_channel'];
      el.innerHTML = services.map(svc => {
        const s = cfg[svc] || {};
        const on = s.enabled;
        const configured = isBackendConfigured(svc, s);
        const label = svc.replace(/_/g, ' ');
        if (!configured) {
          return `<div class="settings-row backend-row" style="justify-content:space-between;">
            <div class="settings-label backend-label" style="text-transform:capitalize;">${escHtml(label)}</div>
            <span style="font-size:11px;color:var(--text2);">not configured</span>
            <button class="btn-secondary backend-btn" onclick="openBackendSetup('${svc}')" title="Configure">⚙ Configure</button>
          </div>`;
        }
        return `<div class="settings-row backend-row" style="justify-content:space-between;">
          <div class="settings-label backend-label" style="text-transform:capitalize;flex:1;">${escHtml(label)}</div>
          <div style="display:flex;align-items:center;gap:8px;">
            <button class="btn-icon" style="font-size:12px;opacity:0.6;" onclick="openBackendSetup('${svc}')" title="Edit configuration">✎</button>
            <label class="toggle-switch" title="${on ? 'Enabled — click to disable' : 'Disabled — click to enable'}">
              <input type="checkbox" ${on ? 'checked' : ''} onchange="toggleBackend('${svc}', this.checked)" />
              <span class="toggle-slider"></span>
            </label>
          </div>
        </div>`;
      }).join('') + `<div style="font-size:11px;color:var(--text2);padding:8px 12px;">
        <span id="backendRestartHint" style="display:none;color:var(--warning);">Restart required to apply changes.
          <button class="btn-link" style="font-size:11px;" onclick="restartDaemon()">Restart now</button>
        </span>
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
    .then(r => {
      if (r.ok) {
        const label = service.replace(/_/g, ' ');
        showToast(label + (enable ? ' enabled' : ' disabled'), 'success', 2000);
        loadConfigStatus();
        const hint = document.getElementById('backendRestartHint');
        if (hint) hint.style.display = 'inline';
        if (state.autoRestartOnConfig) triggerAutoRestart();
      } else showToast('Save failed', 'error');
    })
    .catch(() => showToast('Save failed', 'error'));
}

// ── Backend config field definitions ──────────────────────────────────────────
const LLM_FIELDS = {
  'claude-code': [
    { key:'claude_code_bin', label:'Claude binary', type:'text', placeholder:'claude', section:'session' },
    { key:'claude_enabled', label:'Enabled', type:'checkbox', section:'session' },
    { key:'skip_permissions', label:'Skip permissions', type:'checkbox', section:'session' },
    { key:'channel_enabled', label:'Channel mode', type:'checkbox', section:'session' },
    { key:'auto_git_init', label:'Auto git init', type:'checkbox', section:'session' },
    { key:'auto_git_commit', label:'Auto git commit', type:'checkbox', section:'session' },
  ],
  'aider':       [{ key:'binary', label:'Binary path', type:'text', placeholder:'aider' }, { key:'auto_git_init', label:'Auto git init', type:'checkbox', section:'session' }, { key:'auto_git_commit', label:'Auto git commit', type:'checkbox', section:'session' }],
  'goose':       [{ key:'binary', label:'Binary path', type:'text', placeholder:'goose' }, { key:'auto_git_init', label:'Auto git init', type:'checkbox', section:'session' }, { key:'auto_git_commit', label:'Auto git commit', type:'checkbox', section:'session' }],
  'gemini':      [{ key:'binary', label:'Binary path', type:'text', placeholder:'gemini' }, { key:'auto_git_init', label:'Auto git init', type:'checkbox', section:'session' }, { key:'auto_git_commit', label:'Auto git commit', type:'checkbox', section:'session' }],
  'ollama':      [{ key:'model', label:'Model', type:'ollama_model_select' }, { key:'host', label:'Host URL', type:'text', placeholder:'http://localhost:11434' }, { key:'auto_git_init', label:'Auto git init', type:'checkbox', section:'session' }, { key:'auto_git_commit', label:'Auto git commit', type:'checkbox', section:'session' }],
  'opencode':    [{ key:'binary', label:'Binary path', type:'text', placeholder:'opencode' }, { key:'auto_git_init', label:'Auto git init', type:'checkbox', section:'session' }, { key:'auto_git_commit', label:'Auto git commit', type:'checkbox', section:'session' }],
  'opencode-acp':[{ key:'binary', label:'Binary path', type:'text', placeholder:'opencode' }, { key:'acp_startup_timeout', label:'Startup timeout (sec)', type:'number', placeholder:'30' }, { key:'acp_health_interval', label:'Health interval (sec)', type:'number', placeholder:'5' }, { key:'acp_message_timeout', label:'Message timeout (sec)', type:'number', placeholder:'120' }, { key:'auto_git_init', label:'Auto git init', type:'checkbox', section:'session' }, { key:'auto_git_commit', label:'Auto git commit', type:'checkbox', section:'session' }],
  'opencode-prompt':[{ key:'binary', label:'Binary path', type:'text', placeholder:'opencode' }, { key:'auto_git_init', label:'Auto git init', type:'checkbox', section:'session' }, { key:'auto_git_commit', label:'Auto git commit', type:'checkbox', section:'session' }],
  'openwebui':   [{ key:'url', label:'Server URL', type:'text', placeholder:'http://localhost:3000' }, { key:'api_key', label:'API Key', type:'password' }, { key:'model', label:'Model', type:'openwebui_model_select' }, { key:'auto_git_init', label:'Auto git init', type:'checkbox', section:'session' }, { key:'auto_git_commit', label:'Auto git commit', type:'checkbox', section:'session' }],
  'shell':       [{ key:'script_path', label:'Script path (empty = interactive shell)', type:'text' }, { key:'auto_git_init', label:'Auto git init', type:'checkbox', section:'session' }, { key:'auto_git_commit', label:'Auto git commit', type:'checkbox', section:'session' }],
};

// Config section names in config.yaml for each LLM
const LLM_CFG_SECTION = {
  'claude-code':'session','aider':'aider','goose':'goose','gemini':'gemini','ollama':'ollama',
  'opencode':'opencode','opencode-acp':'opencode','opencode-prompt':'opencode','openwebui':'openwebui','shell':'shell_backend'
};

const BACKEND_FIELDS = {
  telegram:       [{ key:'token', label:'Bot Token', type:'password' }, { key:'chat_id', label:'Chat ID', type:'text' }, { key:'auto_manage_group', label:'Auto-manage group', type:'checkbox' }],
  discord:        [{ key:'token', label:'Bot Token', type:'password' }, { key:'channel_id', label:'Channel ID', type:'text' }, { key:'auto_manage_channel', label:'Auto-manage channel', type:'checkbox' }],
  slack:          [{ key:'token', label:'OAuth Bot Token', type:'password' }, { key:'channel_id', label:'Channel ID', type:'text' }, { key:'auto_manage_channel', label:'Auto-manage channel', type:'checkbox' }],
  matrix:         [{ key:'homeserver', label:'Homeserver URL', type:'text' }, { key:'user_id', label:'User ID (@bot:host)', type:'text' }, { key:'access_token', label:'Access Token', type:'password' }, { key:'room_id', label:'Room ID', type:'text' }, { key:'auto_manage_room', label:'Auto-manage room', type:'checkbox' }],
  ntfy:           [{ key:'server_url', label:'Server URL', type:'text', placeholder:'https://ntfy.sh' }, { key:'topic', label:'Topic', type:'text' }, { key:'token', label:'Token (optional)', type:'password' }],
  email:          [{ key:'host', label:'SMTP Host', type:'text' }, { key:'port', label:'Port', type:'number', placeholder:'587' }, { key:'username', label:'Username', type:'text' }, { key:'password', label:'Password', type:'password' }, { key:'from', label:'From Address', type:'text' }, { key:'to', label:'To Address', type:'text' }],
  twilio:         [{ key:'account_sid', label:'Account SID', type:'text' }, { key:'auth_token', label:'Auth Token', type:'password' }, { key:'from_number', label:'From Number', type:'text' }, { key:'to_number', label:'To Number', type:'text' }, { key:'webhook_addr', label:'Webhook Addr', type:'text', placeholder:'127.0.0.1:9003' }],
  github_webhook: [{ key:'addr', label:'Listen Address', type:'text', placeholder:'127.0.0.1:9001' }, { key:'secret', label:'Webhook Secret', type:'password' }],
  webhook:        [{ key:'addr', label:'Listen Address', type:'text', placeholder:'127.0.0.1:9002' }, { key:'token', label:'Token (optional)', type:'password' }],
  signal:         [{ key:'group_id', label:'Group ID (base64)', type:'text' }, { key:'config_dir', label:'signal-cli config dir', type:'text' }, { key:'device_name', label:'Device name', type:'text' }],
  dns_channel:    [{ key:'mode', label:'Mode', type:'text', placeholder:'server' }, { key:'domain', label:'Domain', type:'text', placeholder:'ctl.example.com' }, { key:'listen', label:'Listen (server)', type:'text', placeholder:':53' }, { key:'upstream', label:'Upstream (client)', type:'text', placeholder:'8.8.8.8:53' }, { key:'secret', label:'Shared Secret', type:'password' }, { key:'ttl', label:'TTL (seconds)', type:'number', placeholder:'0' }, { key:'max_response_size', label:'Max Response Size', type:'number', placeholder:'512' }, { key:'poll_interval', label:'Poll Interval', type:'text', placeholder:'5s' }, { key:'rate_limit', label:'Rate limit (per IP/min)', type:'number', placeholder:'30' }],
};

function openBackendSetup(service) {
  fetch('/api/config', { headers: tokenHeader() })
    .then(r => {
      if (!r.ok) throw new Error('HTTP ' + r.status);
      return r.json();
    })
    .then(cfg => showBackendConfigPopup(service, cfg[service] || {}))
    .catch(err => {
      console.error('openBackendSetup error:', err);
      showToast('Failed to load config: ' + err.message, 'error');
    });
}

function showBackendConfigPopup(service, currentValues, customFields, displayName) {
  const existing = document.getElementById('backendConfigPopup');
  if (existing) existing.remove();
  const fields = customFields || BACKEND_FIELDS[service] || [];
  const label = displayName || service.replace(/_/g, ' ');
  const fieldsHtml = fields.map(f => {
    const val = currentValues[f.key] && currentValues[f.key] !== '***' ? currentValues[f.key] : '';
    const ph = currentValues[f.key] === '***' ? '(configured — enter to change)' : (f.placeholder || '');
    if (f.type === 'ollama_model_select' || f.type === 'openwebui_model_select') {
      return `<div class="popup-field">
        <label class="popup-field-label">${escHtml(f.label)}</label>
        <select id="bkf_${escHtml(f.key)}" class="form-select" style="width:100%;" data-model-type="${f.type}">
          <option value="${escHtml(val)}">${escHtml(val || 'Loading…')}</option>
        </select>
      </div>`;
    }
    if (f.type === 'checkbox') {
      const checked = !!val;
      return `<div class="popup-field" style="display:flex;align-items:center;justify-content:space-between;">
        <label class="popup-field-label" style="margin-bottom:0;">${escHtml(f.label)}</label>
        <label class="toggle-switch">
          <input type="checkbox" id="bkf_${escHtml(f.key)}" ${checked ? 'checked' : ''} />
          <span class="toggle-slider"></span>
        </label>
      </div>`;
    }
    return `<div class="popup-field">
      <label class="popup-field-label">${escHtml(f.label)}</label>
      <input type="${f.type||'text'}" id="bkf_${escHtml(f.key)}" class="form-input" value="${escHtml(val)}" placeholder="${escHtml(ph)}" autocomplete="off" />
    </div>`;
  }).join('');
  const modelFields = fields.filter(f => f.type === 'ollama_model_select' || f.type === 'openwebui_model_select');
  const hasModelFields = modelFields.length > 0;

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
      ${hasModelFields ? `<button class="btn-secondary" style="font-size:11px;margin-right:auto;" onclick="testBackendConnection('${escHtml(service)}')">Test &amp; Load Models</button>` : ''}
      <button class="btn-primary" onclick="saveBackendConfig('${escHtml(service)}')">Save &amp; Enable</button>
      <button class="btn-secondary" onclick="closeBackendConfigPopup()">Cancel</button>
    </div>
  </div>`;
  popup.addEventListener('click', e => { if (e.target === popup) closeBackendConfigPopup(); });
  popup.addEventListener('keydown', e => {
    if (e.key === 'Escape') closeBackendConfigPopup();
    if (e.key === 'Enter' && e.target.tagName !== 'TEXTAREA') { e.preventDefault(); saveBackendConfig(service); }
  });
  document.body.appendChild(popup);
  // Focus first input for keyboard interaction
  const firstInput = popup.querySelector('input, select');
  if (firstInput) firstInput.focus();

  // Fetch models for dynamic model selects (only if service appears configured)
  for (const mf of modelFields) {
    // Skip auto-fetch if no connection details are configured
    const hasHost = currentValues.host || currentValues.url;
    if (!hasHost) {
      const sel = document.getElementById('bkf_' + mf.key);
      if (sel) sel.innerHTML = `<option value="${escHtml(currentValues[mf.key] || '')}">Use "Test &amp; Load Models" after setting host</option>`;
      continue;
    }
    const apiPath = mf.type === 'ollama_model_select' ? '/api/ollama/models' : '/api/openwebui/models';
    fetch(apiPath, { headers: tokenHeader() })
      .then(r => r.ok ? r.json() : [])
      .then(models => {
        const sel = document.getElementById('bkf_' + mf.key);
        if (!sel) return;
        if (!models || !models.length) {
          sel.innerHTML = `<option value="${escHtml(currentValues[mf.key] || '')}">No models — use "Test &amp; Load Models"</option>`;
          return;
        }
        const currentModel = currentValues[mf.key] || '';
        sel.innerHTML = models.map(m =>
          `<option value="${escHtml(m)}" ${m === currentModel ? 'selected' : ''}>${escHtml(m)}</option>`
        ).join('');
        if (!currentModel && models.length > 0) sel.value = models[0];
      })
      .catch(() => {
        const sel = document.getElementById('bkf_' + mf.key);
        if (sel) sel.innerHTML = `<option value="${escHtml(currentValues[mf.key] || '')}">Connection failed — use "Test &amp; Load Models"</option>`;
      });
  }
}

function testBackendConnection(service) {
  // Save connection fields first so the API can reach the service
  const updates = {};
  const allFields = BACKEND_FIELDS[service] || [];
  // Also check LLM fields
  const llmName = Object.entries(LLM_CFG_SECTION || {}).find(([, v]) => v === service);
  const lf = llmName ? LLM_FIELDS[llmName[0]] : [];
  const fields = allFields.length ? allFields : (lf || []);
  for (const f of fields) {
    const el = document.getElementById('bkf_' + f.key);
    if (el && el.value.trim()) updates[service + '.' + f.key] = el.value.trim();
  }

  showToast('Saving config and testing…', 'info', 2000);
  fetch('/api/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', ...tokenHeader() },
    body: JSON.stringify(updates),
  })
    .then(r => {
      if (!r.ok) throw new Error('Save failed');
      // Now fetch models
      let apiPath = '';
      if (service === 'ollama') apiPath = '/api/ollama/models';
      else if (service === 'openwebui') apiPath = '/api/openwebui/models';
      if (!apiPath) { showToast('No model list for this service', 'info'); return; }
      return fetch(apiPath, { headers: tokenHeader() });
    })
    .then(r => {
      if (!r) return;
      if (!r.ok) throw new Error('Connection failed (HTTP ' + r.status + ')');
      return r.json();
    })
    .then(models => {
      if (!models) return;
      if (!models.length) { showToast('Connected but no models found', 'info', 3000); return; }
      // Populate the model dropdown
      const sel = document.getElementById('bkf_model');
      if (sel) {
        const current = sel.value;
        sel.innerHTML = models.map(m =>
          `<option value="${escHtml(m)}" ${m === current ? 'selected' : ''}>${escHtml(m)}</option>`
        ).join('');
      }
      showToast('Connected! ' + models.length + ' models loaded', 'success', 3000);
    })
    .catch(err => showToast('Test failed: ' + err.message, 'error'));
}

function closeBackendConfigPopup() {
  const p = document.getElementById('backendConfigPopup');
  if (p) p.remove();
}

function saveBackendConfig(service) {
  // Look up fields from both communication and LLM field maps
  const llmName = Object.entries(LLM_CFG_SECTION || {}).find(([, v]) => v === service);
  const fields = BACKEND_FIELDS[service] || (llmName ? LLM_FIELDS[llmName[0]] : []) || [];
  const updates = { [service + '.enabled']: true };
  for (const f of fields) {
    const el = document.getElementById('bkf_' + f.key);
    if (!el) continue;
    const cfgPrefix = f.section ? f.section + '.' : service + '.';
    if (f.type === 'checkbox') {
      updates[cfgPrefix + f.key] = el.checked;
    } else if (el.value.trim()) {
      updates[cfgPrefix + f.key] = el.value.trim();
    }
  }
  fetch('/api/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', ...tokenHeader() },
    body: JSON.stringify(updates),
  })
    .then(r => r.ok ? r.json() : r.text().then(t => Promise.reject(new Error(t))))
    .then(() => {
      closeBackendConfigPopup();
      showToast(service.replace(/_/g, ' ') + ' saved.', 'success', 2000);
      // Delay reload to let server cache refresh
      setTimeout(() => {
        loadConfigStatus();
        loadLLMConfig();
      }, 500);
      // Auto-restart if configured
      if (state.autoRestartOnConfig) {
        triggerAutoRestart();
      }
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

// Strip ANSI terminal escape sequences for display (CSI, OSC, DCS, tmux passthrough)
// eslint-disable-next-line no-control-regex
const ANSI_RE = /\x1b\][^\x07]*(?:\x07|\x1b\\)|\x1bP[^\x1b]*\x1b\\|\x1b_[^\x1b]*\x1b\\|\x1b\^[^\x1b]*\x1b\\|\x1b(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])/g;
function stripAnsi(s) { return s ? s.replace(ANSI_RE, '') : ''; }

// Extract the last meaningful line from a multi-line prompt buffer
function lastPromptLine(prompt) {
  if (!prompt) return '';
  const lines = prompt.split('\n').map(l => stripAnsi(l).trim()).filter(l => l.length > 0);
  const last = lines.length > 0 ? lines[lines.length - 1] : '';
  return last.length > 100 ? last.slice(0, 100) + '…' : last;
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

  Promise.all([
    fetch('/api/alerts', { headers: tokenHeader() }).then(r => r.ok ? r.json() : null),
    fetch('/api/commands', { headers: tokenHeader() }).then(r => r.ok ? r.json() : []),
    fetch('/api/sessions', { headers: tokenHeader() }).then(r => r.ok ? r.json() : [])
  ]).then(([data, cmds, freshSessions]) => {
    // Update state.sessions with fresh data so active/inactive classification is accurate
    if (freshSessions && freshSessions.length > 0) {
      state.sessions = freshSessions;
    }
    const el = document.getElementById('alertsList');
    if (!el) return;
    if (!data || !data.alerts || data.alerts.length === 0) {
      el.innerHTML = '<div style="text-align:center;color:var(--text2);padding:32px;">No alerts.</div>';
      return;
    }

    state.alertUnread = 0;
    updateAlertBadge();
    fetch('/api/alerts', { method: 'POST', headers: { 'Content-Type': 'application/json', ...tokenHeader() }, body: JSON.stringify({ all: true }) });

    // Group by session
    const groups = new Map();
    for (const a of data.alerts) {
      const key = a.session_id || '__system__';
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key).push(a);
    }

    const liveSessions = state.sessions || [];
    const DONE = new Set(['complete', 'failed', 'killed']);

    // Separate active vs inactive session groups
    const activeTabs = [];
    const inactiveTabs = [];
    const systemAlerts = groups.get('__system__') || [];
    groups.delete('__system__');

    for (const [sessID, alerts] of groups) {
      const sess = liveSessions.find(s => s.full_id === sessID || s.id === sessID);
      const sessState = sess ? sess.state : 'unknown';
      // Sessions not found in live list or in terminal states are inactive
      const isActive = sess && !DONE.has(sessState);
      const entry = { sessID, alerts, sess, sessState, isActive };
      if (isActive) activeTabs.push(entry);
      else inactiveTabs.push(entry);
    }

    const renderAlert = (a, sessState, isFirst) => {
      const levelColor = a.level === 'error' ? 'var(--error)' : a.level === 'warn' ? 'var(--warning,#f59e0b)' : 'var(--text2)';
      const isWaiting = sessState === 'waiting_input';

      // Quick-reply buttons only on the first (latest) alert for a waiting session
      let replyBtns = '';
      if (isFirst && isWaiting && cmds && cmds.length > 0 && a.session_id) {
        const btnHtml = cmds.map(c => {
          const safeCmd = escHtml(JSON.stringify(c.command));
          return `<button class="quick-btn" style="font-size:10px;" onclick="alertSendCmd(${JSON.stringify(a.session_id)},${safeCmd})">${escHtml(c.name)}</button>`;
        }).join('');
        replyBtns = `<div class="quick-input-row" style="margin-top:6px;flex-wrap:wrap;">${btnHtml}</div>`;
      }

      return `<div class="card alert-card" style="margin-bottom:6px;border-left:3px solid ${levelColor};">
        <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:4px;">
          <strong style="color:${levelColor};font-size:12px;">${escHtml(a.level.toUpperCase())}</strong>
          <span style="font-size:11px;color:var(--text2);">${timeAgo(a.created_at)}</span>
        </div>
        <div style="font-weight:500;font-size:13px;">${escHtml(a.title)}</div>
        <div style="font-size:12px;color:var(--text2);margin-top:2px;">${escHtml(a.body)}</div>
        ${replyBtns}
      </div>`;
    };

    const renderSessionSection = (entry, collapsed) => {
      const { sessID, alerts, sess, sessState } = entry;
      const label = sess ? (sess.name || sess.id) : sessID.split('-').pop();
      const stateColor = sessState === 'waiting_input' ? 'var(--warning,#f59e0b)' : sessState === 'running' ? 'var(--success)' : 'var(--text2)';
      const stateText = sessState === 'waiting_input' ? 'waiting input' : sessState;
      const sessLink = sess ? `<span style="cursor:pointer;text-decoration:underline;" onclick="navigate('session',${JSON.stringify(sessID)})">${escHtml(label)}</span>` : escHtml(label);
      const badge = `<span class="state" style="font-size:10px;color:${stateColor};">${stateText}</span>`;
      const count = `<span style="font-size:11px;color:var(--text2);">${alerts.length} alert${alerts.length !== 1 ? 's' : ''}</span>`;
      const toggleId = 'alert-grp-' + sessID.replace(/[^a-z0-9]/gi, '-');

      if (collapsed) {
        return `<div class="alert-session-group" style="margin-bottom:8px;">
          <div class="settings-section-toggle" onclick="document.getElementById('${toggleId}').style.display=document.getElementById('${toggleId}').style.display==='none'?'':'none'" style="padding:8px 12px;background:var(--bg2);border-radius:var(--radius-sm);cursor:pointer;">
            <span class="settings-chevron" id="${toggleId}-chev">▶</span>
            ${sessLink} ${badge} ${count}
          </div>
          <div id="${toggleId}" style="display:none;">
            ${alerts.map((a, i) => renderAlert(a, sessState, i === 0)).join('')}
          </div>
        </div>`;
      }
      return `<div class="alert-session-group" style="margin-bottom:12px;">
        <div style="display:flex;align-items:center;gap:8px;padding:8px 12px;background:var(--bg2);border-radius:var(--radius-sm) var(--radius-sm) 0 0;border-bottom:1px solid var(--border);">
          ${sessLink} ${badge} ${count}
        </div>
        <div style="padding:4px 0;">
          ${alerts.map((a, i) => renderAlert(a, sessState, i === 0)).join('')}
        </div>
      </div>`;
    };

    const activeCount = activeTabs.length;
    const inactiveCount = inactiveTabs.length + (systemAlerts.length > 0 ? 1 : 0);
    const defaultTab = activeCount > 0 ? 'active' : 'inactive';

    // Build active content — sub-tabs per session, showing one at a time
    let activeHtml = '';
    if (activeTabs.length === 0) {
      activeHtml = '<div style="text-align:center;color:var(--text2);padding:24px;">No active session alerts.</div>';
    } else {
      // Sub-tabs row for each active session
      let subTabsHtml = '<div style="display:flex;gap:0;margin-bottom:8px;flex-wrap:wrap;">';
      for (let i = 0; i < activeTabs.length; i++) {
        const entry = activeTabs[i];
        const label = entry.sess ? (entry.sess.name || entry.sess.id) : entry.sessID.split('-').pop();
        const isFirst = i === 0;
        subTabsHtml += `<button class="output-tab${isFirst ? ' active' : ''}" id="alertSessTab-${i}" onclick="switchAlertSessionTab(${i}, ${activeTabs.length})">${escHtml(label)}</button>`;
      }
      subTabsHtml += '</div>';
      activeHtml += subTabsHtml;

      // One panel per active session, only first visible
      for (let i = 0; i < activeTabs.length; i++) {
        const entry = activeTabs[i];
        activeHtml += `<div id="alertSessPanel-${i}" style="${i === 0 ? '' : 'display:none'}">
          ${renderSessionSection(entry, false)}
        </div>`;
      }
    }

    // Build inactive content — all collapsed
    let inactiveHtml = '';
    if (inactiveTabs.length === 0 && systemAlerts.length === 0) {
      inactiveHtml = '<div style="text-align:center;color:var(--text2);padding:24px;">No inactive alerts.</div>';
    } else {
      for (const entry of inactiveTabs) {
        inactiveHtml += renderSessionSection(entry, true);
      }
      if (systemAlerts.length > 0) {
        const sysToggleId = 'alert-grp-system';
        inactiveHtml += `<div class="alert-session-group" style="margin-bottom:8px;">
          <div class="settings-section-toggle" onclick="document.getElementById('${sysToggleId}').style.display=document.getElementById('${sysToggleId}').style.display==='none'?'':'none'" style="padding:8px 12px;background:var(--bg2);border-radius:var(--radius-sm);cursor:pointer;">
            <span class="settings-chevron">▶</span>
            System <span style="font-size:11px;color:var(--text2);">${systemAlerts.length} alert${systemAlerts.length !== 1 ? 's' : ''}</span>
          </div>
          <div id="${sysToggleId}" style="display:none;">
            ${systemAlerts.map((a, i) => renderAlert(a, '', i === 0)).join('')}
          </div>
        </div>`;
      }
    }

    el.innerHTML = `
      <div style="display:flex;align-items:center;gap:0;margin-bottom:8px;">
        <button class="output-tab ${defaultTab === 'active' ? 'active' : ''}" id="alertTabActive" onclick="switchAlertTab('active')">
          Active${activeCount > 0 ? ' (' + activeCount + ')' : ''}
        </button>
        <button class="output-tab ${defaultTab === 'inactive' ? 'active' : ''}" id="alertTabInactive" onclick="switchAlertTab('inactive')">
          Inactive${inactiveCount > 0 ? ' (' + inactiveCount + ')' : ''}
        </button>
        <div style="flex:1;"></div>
        <button class="btn-secondary" style="font-size:12px;" onclick="renderAlertsView()">Refresh</button>
      </div>
      <div id="alertPanelActive" style="${defaultTab === 'active' ? '' : 'display:none'}">${activeHtml}</div>
      <div id="alertPanelInactive" style="${defaultTab === 'inactive' ? '' : 'display:none'}">${inactiveHtml}</div>
    `;
  }).catch(() => {
    const el = document.getElementById('alertsList');
    if (el) el.innerHTML = '<div style="color:var(--error);padding:16px;">Failed to load alerts.</div>';
  });
}

function switchAlertSessionTab(idx, total) {
  for (let i = 0; i < total; i++) {
    const tab = document.getElementById('alertSessTab-' + i);
    const panel = document.getElementById('alertSessPanel-' + i);
    if (tab) tab.classList.toggle('active', i === idx);
    if (panel) panel.style.display = i === idx ? '' : 'none';
  }
}

function switchAlertTab(tab) {
  const activeTab = document.getElementById('alertTabActive');
  const inactiveTab = document.getElementById('alertTabInactive');
  const activePanel = document.getElementById('alertPanelActive');
  const inactivePanel = document.getElementById('alertPanelInactive');
  if (!activeTab || !inactiveTab || !activePanel || !inactivePanel) return;
  if (tab === 'active') {
    activeTab.classList.add('active');
    inactiveTab.classList.remove('active');
    activePanel.style.display = '';
    inactivePanel.style.display = 'none';
  } else {
    activeTab.classList.remove('active');
    inactiveTab.classList.add('active');
    activePanel.style.display = 'none';
    inactivePanel.style.display = '';
  }
}

function alertSendCmd(sessID, command) {
  apiFetch('/api/command', { method: 'POST', body: { text: 'send ' + sessID.split('-').pop() + ': ' + command } })
    .then(() => { showToast('Sent: ' + command); renderAlertsView(); })
    .catch(e => showToast('Error: ' + e.message, 'error'));
}

// ── Saved Commands (in Settings) ───────────────────────────────────────────────

function pageCmd(dir) {
  settingsPagination.cmds = Math.max(0, (settingsPagination.cmds || 0) + dir);
  loadSavedCommands();
}

function loadDetectionFilters() {
  const el = document.getElementById('detectionFiltersList');
  if (!el) return;
  apiFetch('/api/config').then(r => r.ok ? r.json() : null).then(cfg => {
    if (!cfg || !cfg.detection) { el.innerHTML = '<div style="color:var(--text2);font-size:12px;padding:8px;">Using built-in defaults. Edit config.yaml to customize.</div>'; return; }
    const d = cfg.detection;
    const sections = [
      { key: 'prompt_patterns', label: 'Prompt Detection Patterns', desc: 'Substrings that indicate the LLM is waiting for input' },
      { key: 'completion_patterns', label: 'Completion Patterns', desc: 'Lines indicating a session has completed' },
      { key: 'rate_limit_patterns', label: 'Rate Limit Patterns', desc: 'Lines indicating a rate limit has been hit' },
      { key: 'input_needed_patterns', label: 'Input Needed Patterns', desc: 'Explicit protocol markers for input needed' },
    ];
    let html = '';
    for (const s of sections) {
      const patterns = d[s.key] || [];
      html += `<div style="padding:8px 12px;">
        <div style="font-size:11px;color:var(--text2);font-weight:600;margin-bottom:4px;">${s.label}</div>
        <div style="font-size:10px;color:var(--text2);margin-bottom:4px;">${s.desc}</div>
        <textarea class="form-input" style="font-size:11px;font-family:monospace;height:80px;width:100%;"
          onchange="saveDetectionPatterns('${s.key}', this.value)">${escHtml(patterns.join('\n'))}</textarea>
      </div>`;
    }
    html += '<div style="font-size:10px;color:var(--text2);padding:4px 12px;">One pattern per line. Empty = use built-in defaults. Requires restart.</div>';
    el.innerHTML = html;
  }).catch(() => { el.innerHTML = '<div style="color:var(--error);font-size:12px;padding:8px;">Failed to load.</div>'; });
}

function saveDetectionPatterns(key, value) {
  const patterns = value.split('\n').map(s => s.trim()).filter(Boolean);
  apiFetch('/api/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ['detection.' + key]: patterns }),
  }).then(r => {
    if (r.ok) showToast('Detection patterns saved', 'success', 1500);
    else showToast('Save failed', 'error');
  }).catch(() => showToast('Save failed', 'error'));
}

function loadSchedulesList() {
  const el = document.getElementById('schedulesList');
  if (!el) return;
  apiFetch('/api/schedules').then(r => r.json()).then(items => {
    if (!items || items.length === 0) {
      el.innerHTML = '<div style="color:var(--text2);font-size:12px;padding:8px;">No scheduled events.</div>';
      return;
    }
    // Show most recent first, paginated (10 per page)
    const page = settingsPagination.schedules || 0;
    const perPage = 10;
    const sorted = items.sort((a, b) => new Date(b.created_at) - new Date(a.created_at));
    const pageItems = sorted.slice(page * perPage, (page + 1) * perPage);
    const totalPages = Math.ceil(sorted.length / perPage);
    let html = pageItems.map(sc => {
      const when = sc.run_at ? new Date(sc.run_at).toLocaleString() : 'on input';
      const stateClass = sc.state === 'pending' ? 'color:var(--warning)' : sc.state === 'done' ? 'color:var(--success)' : 'color:var(--text2)';
      const label = sc.type === 'new_session' && sc.deferred_session
        ? 'NEW: ' + escHtml(sc.deferred_session.name || sc.command)
        : escHtml(sc.session_id) + ': ' + escHtml(sc.command);
      return `<div class="settings-row" style="justify-content:space-between;font-size:12px;">
        <div style="flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;" title="${escHtml(sc.command)}">${label}</div>
        <div style="display:flex;align-items:center;gap:6px;">
          <span style="font-size:10px;color:var(--text2);">${when}</span>
          <span style="font-size:10px;${stateClass};font-weight:600;text-transform:uppercase;">${escHtml(sc.state)}</span>
          ${sc.state === 'pending' ? `<button class="btn-icon" style="font-size:10px;color:var(--error);" onclick="cancelSchedule('${sc.id}','');loadSchedulesList()" title="Cancel">&#10005;</button>` : ''}
        </div>
      </div>`;
    }).join('');
    if (totalPages > 1) {
      html += `<div style="display:flex;justify-content:center;gap:8px;padding:6px;">
        ${page > 0 ? `<button class="btn-link" style="font-size:11px;" onclick="settingsPagination.schedules=${page - 1};loadSchedulesList()">&#9664; Prev</button>` : ''}
        <span style="font-size:11px;color:var(--text2);">Page ${page + 1}/${totalPages}</span>
        ${page < totalPages - 1 ? `<button class="btn-link" style="font-size:11px;" onclick="settingsPagination.schedules=${page + 1};loadSchedulesList()">Next &#9654;</button>` : ''}
      </div>`;
    }
    el.innerHTML = html;
  }).catch(() => { el.innerHTML = '<div style="color:var(--error);font-size:12px;padding:8px;">Failed to load schedules.</div>'; });
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
              <button class="btn-icon btn-icon-del" title="Delete" onclick="deleteSavedCmd('${escHtml(cmd.name)}')">✕</button>
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
window.switchAlertTab = switchAlertTab;
window.switchAlertSessionTab = switchAlertSessionTab;
window.toggleSessionTimeline = toggleSessionTimeline;
window.deleteSavedCmd = deleteSavedCmd;
window.toggleFilter = toggleFilter;
window.deleteFilter = deleteFilter;
window.killSession = killSession;
window.switchOutputTab = switchOutputTab;
window.restartSession = restartSession;
window.deleteSession = deleteSession;
window.sendSavedCmd = sendSavedCmd;
window.showCardCmds = showCardCmds;
window.cardSendCmd = cardSendCmd;
window.cardSendKey = cardSendKey;
window.sessionDragStart = sessionDragStart;
window.sessionDragOver = sessionDragOver;
window.sessionDrop = sessionDrop;
window.sessionDragEnd = sessionDragEnd;
window.checkForUpdate = checkForUpdate;
window.runUpdate = runUpdate;
window.moveSession = moveSession;
window.sendQuickInput = sendQuickInput;
window.sendChannelMessage = sendChannelMessage;
window.showChannelHelp = showChannelHelp;
window.dismissConnBanner = dismissConnBanner;
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
window.cancelSchedule = cancelSchedule;
window.showScheduleInputPopup = showScheduleInputPopup;
window.submitScheduleInput = submitScheduleInput;
window.saveDetectionPatterns = saveDetectionPatterns;
window.loadDetectionFilters = loadDetectionFilters;
window.loadGlobalScheduleBadge = loadGlobalScheduleBadge;
window.loadSchedulesList = loadSchedulesList;
window.loadSessionSchedules = loadSessionSchedules;
window.toggleGlobalScheduleDropdown = toggleGlobalScheduleDropdown;
window.setBackendFilter = setBackendFilter;
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
window.testBackendConnection = testBackendConnection;
window.pageCmd = pageCmd;
window.pageFilter = pageFilter;
window.loadLLMConfig = loadLLMConfig;
window.setActiveLLM = setActiveLLM;
window.toggleLLM = toggleLLM;
window.openLLMSetup = openLLMSetup;
window.loadGeneralConfig = loadGeneralConfig;
window.saveGeneralField = saveGeneralField;
window.saveInterfaceField = saveInterfaceField;
window.toggleSettingsDirBrowser = toggleSettingsDirBrowser;
