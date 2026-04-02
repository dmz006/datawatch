window._splashStart = Date.now();

// ── Debug Console ──────────────────────────────────────────────────────────
// Captures JS errors, network failures, and WS events for debugging.
// Access via: triple-tap the status dot, or window._debugLog in browser console.
window._debugLog = [];
window._debugMax = 200;
function _dbg(type, msg) {
  const entry = { ts: new Date().toISOString().slice(11,23), type, msg };
  window._debugLog.push(entry);
  if (window._debugLog.length > window._debugMax) window._debugLog.shift();
}
window.addEventListener('error', e => _dbg('ERROR', `${e.message} at ${e.filename}:${e.lineno}`));
window.addEventListener('unhandledrejection', e => _dbg('REJECT', String(e.reason)));
// Wrap fetch to log failures
const _origFetch = window.fetch;
window.fetch = function(...args) {
  return _origFetch.apply(this, args).then(r => {
    if (!r.ok) _dbg('HTTP', `${r.status} ${args[0]}`);
    return r;
  }).catch(err => {
    _dbg('FETCH', `${args[0]} — ${err.message}`);
    throw err;
  });
};

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
  selectMode: false,      // multi-select mode for batch session deletion
  selectedSessions: new Set(), // full IDs of selected sessions
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
    // Update comms server status indicator if visible
    const connInd = document.querySelector('.connection-indicator');
    if (connInd) {
      connInd.querySelector('.dot')?.classList.add('connected');
      const span = connInd.querySelector('span');
      if (span) span.textContent = 'Connected';
    }
    // Dismiss splash screen — ensure minimum 5 seconds display
    const splash = document.getElementById('splash');
    if (splash) {
      const elapsed = Date.now() - (window._splashStart || 0);
      const remaining = Math.max(0, 3000 - elapsed);
      setTimeout(() => {
        splash.classList.add('fade-out');
        setTimeout(() => splash.remove(), 700);
      }, remaining);
    }
    showToast('Connected', 'success', 2000);
    // Load server-side UI preferences into state cache
    fetch('/api/config', { headers: tokenHeader() }).then(r => r.ok ? r.json() : null).then(cfg => {
      if (!cfg) return;
      state.suppressActiveToasts = cfg.server?.suppress_active_toasts !== false;
      state.autoRestartOnConfig = !!cfg.server?.auto_restart_on_config;
      state._recentMinutes = cfg.server?.recent_session_minutes || 5;
      state._maxSessions = cfg.session?.max_sessions || 10;
    }).catch(() => {});
    // After reconnect (e.g. after daemon restart), re-render the current view
    // so settings/LLM data reloads from the fresh daemon
    if (state.activeView === 'settings') {
      renderSettingsView();
    } else if (state.activeView === 'sessions') {
      renderSessionsView();
    } else if (state.activeView === 'session-detail' && state.activeSession) {
      // Re-render the session detail to restore output subscription, xterm.js,
      // and saved commands after WS reconnect (e.g. daemon restart).
      renderSessionDetail(state.activeSession);
    }
  });

  ws.addEventListener('message', e => {
    let msg;
    try { msg = JSON.parse(e.data); } catch { return; }
    handleMessage(msg);
  });

  ws.addEventListener('close', (e) => {
    _dbg('WS', `closed code=${e.code}`);
    state.connected = false;
    state.ws = null;
    updateStatusDot();
    // Update comms server status indicator if visible
    const connInd2 = document.querySelector('.connection-indicator');
    if (connInd2) {
      connInd2.querySelector('.dot')?.classList.remove('connected');
      const span2 = connInd2.querySelector('span');
      if (span2) span2.textContent = 'Disconnected';
    }
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
      // Initialize channel_ready state from session data
      for (const s of state.sessions) {
        if (s.channel_ready) state.channelReady[s.full_id] = true;
      }
      // Auto-reload browser if daemon version changed (new build deployed)
      if (msg.data && msg.data.version) {
        if (!state._daemonVersion) {
          state._daemonVersion = msg.data.version;
        } else if (state._daemonVersion !== msg.data.version) {
          console.log(`[datawatch] daemon version changed: ${state._daemonVersion} → ${msg.data.version}, reloading…`);
          location.reload();
          return;
        }
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
      // Raw output with ANSI preserved — route directly to xterm.js
      if (msg.data) {
        const sid = msg.data.session_id;
        const rawLines = msg.data.lines || [];
        if (!state.rawOutputBuffer[sid]) state.rawOutputBuffer[sid] = [];
        state.rawOutputBuffer[sid].push(...rawLines);
        if (state.rawOutputBuffer[sid].length > 500) {
          state.rawOutputBuffer[sid] = state.rawOutputBuffer[sid].slice(-500);
        }
        if (state.activeView === 'session-detail' && state.activeSession === sid && rawLines.length > 0) {
          // Check if this session uses log mode (no xterm)
          const logArea = document.querySelector('.log-viewer-mode');
          if (logArea && !state.terminal) {
            // Log mode — append formatted lines
            for (const chunk of rawLines) {
              const text = stripAnsi(chunk).trim();
              if (!text) continue;
              const div = document.createElement('div');
              div.className = 'log-line' + (text.includes('[opencode-acp]') ? ' log-acp-status' : '');
              div.textContent = text;
              logArea.appendChild(div);
            }
            logArea.scrollTop = logArea.scrollHeight;
          } else if (state.terminal) {
            // Terminal mode — raw_output not used for display.
            // Display handled by pane_capture messages (reset + clean lines).
          }
        }
      }
      break;
    case 'stats':
      // Real-time stats update — refresh the dashboard if on settings page
      if (msg.data && state.activeView === 'settings') {
        const el = document.getElementById('statsPanel');
        if (el) renderStatsData(el, msg.data);
      }
      break;
    case 'pane_capture':
      // Write capture-pane snapshot to xterm.js.
      // First frame uses reset() for a clean start.
      // Subsequent frames use ESC[2J ESC[H (clear + home) to avoid visible flash —
      // xterm.js batches these in a single render cycle so the clear and redraw
      // appear as one atomic update.
      if (msg.data && state.terminal && state.activeView === 'session-detail' && state.activeSession === msg.data.session_id) {
        const capLines = msg.data.lines || [];
        if (capLines.length > 0) {
          if (!state._termHasContent) {
            // First frame — reset for clean state
            state.terminal.reset();
            state.terminal.write(capLines.join('\r\n'));
            state._termHasContent = true;
          } else {
            // Subsequent frames — clear + home + redraw in one write
            state.terminal.write('\x1b[2J\x1b[H' + capLines.join('\r\n'));
          }
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
        // Also add to channel replies for the channel tab
        handleChannelReply({ text: `[notify] ${msg.data.text}`, session_id: msg.data.session_id || '', direction: 'notify' });
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
  // Show concise toast — title only; full body is in the Alerts view
  const toastMsg = a.title.length > 60 ? a.title.slice(0, 57) + '…' : a.title;
  showToast(toastMsg, level, 4000);
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
  const { text, session_id, direction } = data;
  if (!session_id) return;
  if (!state.channelReplies[session_id]) state.channelReplies[session_id] = [];
  state.channelReplies[session_id].push({ text, ts: new Date().toISOString(), direction: direction || 'incoming' });
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
      const dir = data.direction || 'incoming';
      const cls = dir === 'outgoing' ? 'channel-send-line' : dir === 'notify' ? 'channel-notify-line' : 'channel-reply-line';
      const prefix = dir === 'outgoing' ? '→ ' : dir === 'notify' ? '⚡ ' : '← ';
      div.className = `${cls} new-line`;
      div.textContent = prefix + text;
      outputArea.appendChild(div);
      if (wasAtBottom) outputArea.scrollTop = outputArea.scrollHeight;
    }
  }
}

function updateSession(sess) {
  if (!sess) return;
  // Sync channel_ready from session state updates
  if (sess.channel_ready) state.channelReady[sess.full_id] = true;
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
        const isLogMode = outputArea.classList.contains('log-viewer-mode');
        const wasAtBottom = outputArea.scrollHeight - outputArea.scrollTop <= outputArea.clientHeight + 40;
        lines.forEach(line => {
          const text = stripAnsi(line);
          if (!text.trim()) return;
          const div = document.createElement('div');
          if (isLogMode) {
            div.className = 'log-line' + (text.includes('[opencode-acp]') ? ' log-acp-status' : '')
              + (text.includes('processing') || text.includes('thinking') ? ' log-processing' : '')
              + (text.includes('ready') || text.includes('awaiting input') ? ' log-ready' : '')
              + (text.includes('error') || text.includes('failed') ? ' log-error' : '');
          } else {
            div.className = 'output-line new-line';
          }
          div.textContent = text;
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

  // If viewing this session, highlight the input bar (no banner — xterm.js shows the prompt)
  if (state.activeView === 'session-detail' && state.activeSession === sessionId) {
    const bar = document.querySelector('.input-bar');
    if (bar) bar.classList.add('needs-input');
    const label = document.querySelector('.input-label');
    if (label) label.style.display = 'block';
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
  // Persist view for refresh recovery
  localStorage.setItem('cs_active_view', view);
  if (sessionId) localStorage.setItem('cs_active_session', sessionId);
  else localStorage.removeItem('cs_active_session');

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
  const RECENT_MS = (state._recentMinutes || 5) * 60 * 1000;
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
  // Collect unique backend types from all sessions for compact filter badges
  const backendTypes = [...new Set(state.sessions.map(s => s.llm_backend).filter(Boolean))].sort();
  const backendShort = {
    'claude-code': 'claude', 'opencode': 'oc', 'opencode-acp': 'acp',
    'opencode-prompt': 'oc-p', 'openwebui': 'owui', 'ollama': 'olla',
    'aider': 'aider', 'goose': 'goose', 'gemini': 'gem', 'shell': 'sh',
  };
  const backendBadges = backendTypes.map(bt => {
    const isActive = filterText === bt.toLowerCase();
    const label = backendShort[bt] || bt;
    const count = state.sessions.filter(s => s.llm_backend === bt).length;
    return `<button class="backend-filter-badge ${isActive ? 'active' : ''}" onclick="setBackendFilter('${escHtml(bt)}')" title="${escHtml(bt)} (${count})">${escHtml(label)}<span class="badge-count">${count}</span></button>`;
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
    ${state.showHistory && history.length > 0 ? `<div style="position:relative;display:inline-block;">
      <button class="btn-icon" style="font-size:14px;padding:4px 6px;opacity:${state.selectMode ? '1' : '0.5'};" onclick="toggleSelectMode()" title="Select sessions">&#9745;</button>
      ${state.selectMode ? `<div class="select-popup">
        <button class="select-popup-btn" onclick="selectAllInactive()" title="Select all inactive">&#9745; ${state.selectedSessions.size === history.length ? 'None' : 'All'} <span style="opacity:0.6;">(${history.length})</span></button>
        <button class="select-popup-btn select-popup-delete" onclick="deleteSelectedSessions()" title="Delete selected" ${state.selectedSessions.size === 0 ? 'disabled' : ''}>&#128465; Delete <span style="opacity:0.6;">(${state.selectedSessions.size})</span></button>
      </div>` : ''}
    </div>` : ''}
  </div>`;

  if (visible.length === 0 && active.length === 0 && recent.length === 0) {
    view.innerHTML = `
      <div class="view-content" style="position:relative;">
        <div class="sessions-watermark"><img src="/favicon.svg" alt="" /></div>
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
  view.innerHTML = `<div class="view-content" style="position:relative;">
    <div class="sessions-watermark"><img src="/favicon.svg" alt="" /></div>
    ${toggleBtn}<div class="session-list">${cards}</div></div>`;

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
  apiFetch('/api/schedules?state=pending').then(items => {
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
  // Exit select mode when hiding history
  if (!state.showHistory) {
    state.selectMode = false;
    state.selectedSessions.clear();
  }
  renderSessionsView();
}

function toggleSelectMode() {
  state.selectMode = !state.selectMode;
  if (!state.selectMode) {
    state.selectedSessions.clear();
  }
  renderSessionsView();
}

function toggleSessionSelect(fullId) {
  if (state.selectedSessions.has(fullId)) {
    state.selectedSessions.delete(fullId);
  } else {
    state.selectedSessions.add(fullId);
  }
  renderSessionsView();
}

function selectAllInactive() {
  const inactive = state.sessions.filter(s => DONE_STATES.has(s.state));
  if (state.selectedSessions.size === inactive.length) {
    // Deselect all if all are selected
    state.selectedSessions.clear();
  } else {
    inactive.forEach(s => state.selectedSessions.add(s.full_id));
  }
  renderSessionsView();
}

function deleteSelectedSessions() {
  const count = state.selectedSessions.size;
  if (count === 0) return;
  showConfirmModal(`Delete ${count} session${count > 1 ? 's' : ''} and their data?`, () => {
    const ids = [...state.selectedSessions];
    let done = 0, failed = 0;
    const headers = { 'Content-Type': 'application/json', ...tokenHeader() };
    Promise.all(ids.map(id =>
      fetch('/api/sessions/delete', {
        method: 'POST', headers,
        body: JSON.stringify({ id, delete_data: true }),
      }).then(r => { if (r.ok) done++; else failed++; })
        .catch(() => failed++)
    )).then(() => {
      state.selectMode = false;
      state.selectedSessions.clear();
      showToast(`Deleted ${done} session${done !== 1 ? 's' : ''}${failed ? ', ' + failed + ' failed' : ''}`, done ? 'success' : 'error', 3000);
      renderSessionsView();
    });
  });
}

window.toggleSelectMode = toggleSelectMode;
window.toggleSessionSelect = toggleSessionSelect;
window.selectAllInactive = selectAllInactive;
window.deleteSelectedSessions = deleteSelectedSessions;

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

  const showCheckbox = state.selectMode && !isActive;
  const isSelected = state.selectedSessions.has(fullId);

  return `
    <div class="session-card ${stateClass}${isSelected ? ' selected' : ''}" draggable="${!showCheckbox}" data-full-id="${escHtml(fullId)}"
         onclick="${showCheckbox ? `event.preventDefault();toggleSessionSelect('${escHtml(fullId)}')` : `navigate('session-detail', '${escHtml(fullId)}')`}"
         ondragstart="sessionDragStart(event,'${escHtml(fullId)}')"
         ondragover="sessionDragOver(event)"
         ondrop="sessionDrop(event,'${escHtml(fullId)}')"
         ondragend="sessionDragEnd(event)">
      <div class="session-card-header">
        ${showCheckbox ? `<input type="checkbox" ${isSelected ? 'checked' : ''} onclick="event.stopPropagation();toggleSessionSelect('${escHtml(fullId)}')" style="margin-right:6px;" />` : ''}
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
  // Fetch saved commands and build grouped dropdown with custom option
  let html = '';
  fetch('/api/commands', { headers: tokenHeader() })
    .then(r => r.ok ? r.json() : [])
    .then(cmds => {
      const eid = escHtml(fullId);
      // System commands
      let optHtml = '<optgroup label="System">';
      optHtml += `<option value="yes">approve</option><option value="no">reject</option>`;
      optHtml += `<option value="continue">continue</option><option value="skip">skip</option>`;
      optHtml += '</optgroup>';
      if (cmds && cmds.length) {
        optHtml += '<optgroup label="Saved">';
        optHtml += cmds.map(c => `<option value="${escHtml(c.command)}">${escHtml(c.name)}</option>`).join('');
        optHtml += '</optgroup>';
      }
      optHtml += '<optgroup label=""><option value="__custom__">Custom…</option></optgroup>';
      html += `<select class="quick-cmd-select" onchange="cardHandleQuickCmd(this,'${eid}')"><option value="">Commands…</option>${optHtml}</select>`;
      html += `<div id="cardCustom-${escHtml(shortId)}" class="custom-cmd-wrap" style="display:none;" onclick="event.stopPropagation()">` +
        `<input type="text" class="custom-cmd-input" placeholder="Type…" onkeydown="if(event.key==='Enter'){cardSendCustom('${eid}','${escHtml(shortId)}');event.preventDefault();}">` +
        `<button class="quick-btn" onclick="event.stopPropagation();cardSendCustom('${eid}','${escHtml(shortId)}')" title="Send">&#10148;</button>` +
        `<button class="quick-btn" onclick="event.stopPropagation();document.getElementById('cardCustom-${escHtml(shortId)}').style.display='none'" title="Cancel">&#10005;</button></div>`;
      el.innerHTML = html;
      el.style.display = '';
    });
}

function cardHandleQuickCmd(sel, fullId) {
  const val = sel.value;
  sel.selectedIndex = 0;
  if (!val) return;
  if (val === '__custom__') {
    const shortId = fullId.split('-').pop();
    const wrap = document.getElementById('cardCustom-' + shortId);
    if (wrap) { wrap.style.display = 'flex'; wrap.querySelector('input')?.focus(); }
    return;
  }
  event.stopPropagation();
  cardSendCmd(fullId, val);
}

function cardSendCustom(fullId, shortId) {
  const wrap = document.getElementById('cardCustom-' + shortId);
  const input = wrap?.querySelector('input');
  if (!input || !input.value.trim()) return;
  cardSendCmd(fullId, input.value);
  input.value = '';
  wrap.style.display = 'none';
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
  const channelHtml = replies.map(r => {
    const dir = r.direction || 'incoming';
    const cls = dir === 'outgoing' ? 'channel-send-line' : dir === 'notify' ? 'channel-notify-line' : 'channel-reply-line';
    const prefix = dir === 'outgoing' ? '→ ' : dir === 'notify' ? '⚡ ' : '← ';
    return `<div class="${cls}">${prefix}${escHtml(r.text)}</div>`;
  }).join('');

  const nameText = sess ? (sess.name || '') : '';
  const displayTitle = nameText || taskText || '(no task)';
  const backendText = sess ? (sess.llm_backend || '') : '';
  const projectDir = sess ? (sess.project_dir || '') : '';
  const sessionMode = getSessionMode(backendText);
  const isActive = stateText === 'running' || stateText === 'waiting_input' || stateText === 'rate_limited';
  const isDone = stateText === 'complete' || stateText === 'failed' || stateText === 'killed';

  // Don't show "Waiting for input" banner when xterm is active — user sees the prompt directly
  const needsBanner = '';

  // Connection status banner for channel/ACP mode sessions.
  // Also determines whether input should be disabled until connection is established.
  let connBanner = '';
  let connReady = true;
  if (isActive && (sessionMode === 'channel' || sessionMode === 'acp')) {
    // Check cached ready state first (survives view navigation), then session data, then output scan
    const sessData = state.sessions.find(s => s.full_id === sessionId);
    if (state.channelReady[sessionId] || (sessData && sessData.channel_ready)) {
      connReady = true;
      state.channelReady[sessionId] = true;
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
        <span class="conn-spinner"></span> Waiting for ${modeLabel}…
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
  const curFontSize = parseInt(localStorage.getItem('cs_term_font_size')||'9');
  const fontCtrl = `<div class="term-toolbar">
    <button class="term-tool-btn" onclick="changeTermFontSize(-1)" title="Decrease font size">A&minus;</button>
    <span style="font-size:10px;color:var(--text2);min-width:28px;text-align:center;">${curFontSize}px</span>
    <button class="term-tool-btn" onclick="changeTermFontSize(1)" title="Increase font size">A+</button>
    <span style="color:var(--border);margin:0 4px;">|</span>
    <button class="term-tool-btn" onclick="termFitToWidth()" title="Fit terminal to screen width">Fit</button>
  </div>`;
  const outputAreaHtml = showChannel
    ? `<div class="output-tabs">
        <button class="output-tab active" id="tabTmux" onclick="switchOutputTab('tmux')">Tmux</button>
        <button class="output-tab" id="tabChannel" onclick="switchOutputTab('channel')">Channel</button>
        <button class="btn-icon" style="font-size:12px;margin-left:auto;opacity:0.6;" onclick="showChannelHelp()" title="Channel commands">?</button>
        ${fontCtrl}
      </div>
      <div class="output-area output-area-tmux" id="outputAreaTmux"></div>
      <div class="output-area output-area-channel" id="outputAreaChannel" style="display:none">${channelHtml}</div>`
    : `<div style="display:flex;justify-content:flex-end;padding:2px 8px;">${fontCtrl}</div>
       <div class="output-area output-area-tmux" id="outputAreaTmux"></div>`;

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
          <span class="state detail-state-badge ${badgeClass}" onclick="showStateOverride('${escHtml(sessionId)}',this)" style="cursor:pointer;" title="Click to change state">${escHtml(stateText)}</span>
          <span id="actionBtns">${actionButtons}</span>
          <button class="detail-pill-btn" onclick="toggleSessionTimeline('${escHtml(sessionId)}')" title="Show event timeline">&#128336; Timeline</button>
        </div>
      </div>
      <div id="sessionSchedules" class="session-schedules" style="display:none;"></div>
      ${connBanner}
      ${needsBanner}
      ${outputAreaHtml}
      ${isActive && (sess?.input_mode || 'tmux') !== 'none' ? `<div id="savedCmdsQuick" class="saved-cmds-quick"></div>` : ''}
      ${isActive && (sess?.input_mode || 'tmux') !== 'none' ? `<div class="input-bar${isWaiting ? ' needs-input' : ''}${!connReady ? ' input-disabled' : ''}" id="inputBar">
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

  // Initialize output display — xterm.js for terminal mode, log viewer for log mode
  const outputMode = sess?.output_mode || 'terminal';
  if (outputMode === 'log') {
    // Log viewer for ACP/headless sessions — show output.log content formatted
    const logArea = document.getElementById('outputAreaTmux');
    if (logArea) {
      logArea.classList.add('log-viewer-mode');
      const logLines = lines.map(l => stripAnsi(l)).filter(t => t.trim());
      logArea.innerHTML = logLines.map(line => {
        let cls = 'log-line';
        if (line.includes('[opencode-acp]')) cls += ' log-acp-status';
        if (line.includes('thinking') || line.includes('processing')) cls += ' log-processing';
        if (line.includes('ready') || line.includes('awaiting input')) cls += ' log-ready';
        if (line.includes('error') || line.includes('failed')) cls += ' log-error';
        return `<div class="${cls}">${escHtml(line)}</div>`;
      }).join('');
      logArea.scrollTop = logArea.scrollHeight;
    }
  } else {
    const rawLines = state.rawOutputBuffer[sessionId] || lines;
    const sessCols = sess ? (sess.console_cols || 0) : 0;
    const sessRows = sess ? (sess.console_rows || 0) : 0;
    initXterm(sessionId, rawLines, sessCols, sessRows);
  }

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

function changeTermFontSize(delta) {
  const current = parseInt(localStorage.getItem('cs_term_font_size') || '9', 10);
  const next = Math.max(5, Math.min(20, current + delta));
  localStorage.setItem('cs_term_font_size', String(next));
  // Update all labels (there may be one in tabs bar and one in standalone bar)
  document.querySelectorAll('.term-toolbar span').forEach(el => {
    if (el.textContent.includes('px')) el.textContent = next + 'px';
  });
  if (state.terminal) {
    state.terminal.options.fontSize = next;
    if (state.termFitAddon) {
      try { state.termFitAddon.fit(); } catch(e) {}
    }
  }
}

function termFitToWidth() {
  if (!state.terminal || !state.termFitAddon) return;
  // Auto-decrease font until terminal fits container width
  const container = document.getElementById('outputAreaTmux');
  if (!container) return;
  const maxWidth = container.clientWidth - 16; // padding
  let fs = parseInt(localStorage.getItem('cs_term_font_size') || '9', 10);
  while (fs > 5) {
    state.terminal.options.fontSize = fs;
    try { state.termFitAddon.fit(); } catch(e) {}
    // xterm viewport width is set by fit addon — check if scrollbar needed
    const viewport = container.querySelector('.xterm-viewport');
    if (viewport && viewport.scrollWidth <= viewport.clientWidth + 2) break;
    fs--;
  }
  localStorage.setItem('cs_term_font_size', String(fs));
  document.querySelectorAll('.term-toolbar span').forEach(el => {
    if (el.textContent.includes('px')) el.textContent = fs + 'px';
  });
}

function destroyXterm() {
  if (state.terminal) {
    state.terminal.dispose();
    state.terminal = null;
    state.termFitAddon = null;
    state._termHasContent = false;
  }
}

function initXterm(sessionId, bufferedLines, configCols, configRows) {
  destroyXterm();
  const container = document.getElementById('outputAreaTmux');
  if (!container || typeof Terminal === 'undefined') {
    if (container && bufferedLines) {
      container.innerHTML = bufferedLines.map(l => `<div class="output-line">${escHtml(stripAnsi(l))}</div>`).join('');
      container.scrollTop = container.scrollHeight;
    }
    return;
  }

  const savedFontSize = parseInt(localStorage.getItem('cs_term_font_size') || '9', 10);
  // Use the session's configured console size — DO NOT shrink below this
  const termOpts = {
    cursorBlink: true,
    fontSize: savedFontSize,
    fontFamily: "'JetBrains Mono', 'Fira Code', monospace",
    allowProposedApi: true,
  };
  // Set configured cols for wide terminals (e.g. claude 120 cols) — container scrolls horizontally
  if (configCols > 0) termOpts.cols = configCols;
  if (configRows > 0) termOpts.rows = configRows;
  termOpts.theme = {
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
  };
  termOpts.scrollback = 5000;
  const term = new Terminal(termOpts);

  let fitAddon = null;
  if (typeof FitAddon !== 'undefined') {
    fitAddon = new FitAddon.FitAddon();
    term.loadAddon(fitAddon);
  }

  term.open(container);

  // Sync tmux pane size with xterm.js terminal size.
  // After resize, the server sends a 'pane_capture' with fresh content at the correct width.
  function syncTmuxSize() {
    if (state.activeSession && term.cols && term.rows) {
      send('resize_term', { session_id: state.activeSession, cols: term.cols, rows: term.rows });
    }
  }

  // Fit terminal to container. For wide terminals (claude 120 cols), allow
  // horizontal scroll. For others, fit to container width naturally.
  const minCols = configCols || 80;
  const containerW = container.offsetWidth || 480;
  // Estimate if configCols fits: charWidth ~5px at small font, ~7px at normal
  const charEst = (savedFontSize || 9) * 0.6;
  const fitsInContainer = (minCols * charEst) <= containerW;

  if (fitAddon) {
    requestAnimationFrame(() => {
      try { fitAddon.fit(); } catch(e) {}
      // If configured cols DON'T fit, only enforce for backends that need it
      // (claude-code needs 120 — allow horizontal scroll)
      if (term.cols < minCols && fitsInContainer) {
        term.resize(minCols, term.rows);
      } else if (term.cols < minCols && configCols >= 120) {
        // Wide terminal required (claude) — force it, container scrolls horizontally
        term.resize(minCols, term.rows);
      }
      syncTmuxSize();
    });
  } else {
    syncTmuxSize();
  }

  // Don't write buffered file output to xterm — capture-pane will send a clean
  // snapshot shortly. Writing raw log data from output.log causes garbled display
  // because it contains ANSI escape sequences not intended for direct rendering.

  // Handle resize — debounced
  if (fitAddon) {
    let lastCols = term.cols, lastRows = term.rows;
    let resizeTimer = null;
    const resizeObs = new ResizeObserver(() => {
      if (resizeTimer) clearTimeout(resizeTimer);
      resizeTimer = setTimeout(() => {
        try { fitAddon.fit(); } catch(e) {}
        const nowFits = (minCols * charEst) <= (container.offsetWidth || 480);
        if (term.cols < minCols && (nowFits || configCols >= 120)) {
          term.resize(minCols, term.rows);
        }
        if (term.cols !== lastCols || term.rows !== lastRows) {
          lastCols = term.cols;
          lastRows = term.rows;
          syncTmuxSize();
        }
      }, 200);
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
      <div style="display:flex;gap:4px;flex-wrap:wrap;margin-top:6px;">
        <button class="btn-secondary" style="font-size:10px;padding:3px 8px;" onclick="document.getElementById('schedInputTime').value='on input'">On next prompt</button>
        <button class="btn-secondary" style="font-size:10px;padding:3px 8px;" onclick="document.getElementById('schedInputTime').value='in 5 minutes'">5 min</button>
        <button class="btn-secondary" style="font-size:10px;padding:3px 8px;" onclick="document.getElementById('schedInputTime').value='in 15 minutes'">15 min</button>
        <button class="btn-secondary" style="font-size:10px;padding:3px 8px;" onclick="document.getElementById('schedInputTime').value='in 30 minutes'">30 min</button>
        <button class="btn-secondary" style="font-size:10px;padding:3px 8px;" onclick="document.getElementById('schedInputTime').value='in 1 hour'">1 hr</button>
        <button class="btn-secondary" style="font-size:10px;padding:3px 8px;" onclick="document.getElementById('schedInputTime').value='in 2 hours'">2 hr</button>
      </div>
      <div style="font-size:9px;color:var(--text2);margin-top:4px;"><b>On next prompt</b> = fires when session next waits for input. Other options: tomorrow at 9am, next monday at 10:00</div>
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
  showConfirmModal('Stop session?', () => {
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
  });
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
    const resumeSel = document.getElementById('resumeSelect');
    const taskDetails = document.getElementById('taskDetailsSection');
    if (taskEl && sess.task) { taskEl.value = sess.task; if (taskDetails) taskDetails.open = true; }
    if (nameEl) nameEl.value = sess.name || '';
    if (sess.project_dir) {
      newSessionState.selectedDir = sess.project_dir;
      if (dirDisplay) dirDisplay.textContent = sess.project_dir;
    }
    // Pre-select the session in the resume dropdown
    if (resumeSel) {
      const fullId = sess.full_id || sess.id;
      for (const opt of resumeSel.options) {
        if (opt.value === fullId) { opt.selected = true; break; }
      }
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
  fetch('/api/commands', { headers: tokenHeader() })
    .then(r => r.ok ? r.json() : [])
    .then(cmds => {
      const panel = document.getElementById('savedCmdsQuick');
      if (!panel) return;
      // System commands (hardcoded)
      const systemOpts = [
        { name: 'approve', command: 'yes' },
        { name: 'reject', command: 'no' },
        { name: 'enter', command: '\n' },
        { name: 'continue', command: 'continue' },
        { name: 'skip', command: 'skip' },
        { name: 'abort', command: '\x03' },
      ];
      let optHtml = '<optgroup label="System">';
      optHtml += systemOpts.map(c => `<option value="${escHtml(c.command)}">${escHtml(c.name)}</option>`).join('');
      optHtml += '</optgroup>';
      if (cmds && cmds.length) {
        optHtml += '<optgroup label="Saved">';
        optHtml += cmds.map(c => `<option value="${escHtml(c.command)}">${escHtml(c.name || c.command)}</option>`).join('');
        optHtml += '</optgroup>';
      }
      optHtml += '<optgroup label=""><option value="__custom__">Custom…</option></optgroup>';
      panel.innerHTML = `<select class="quick-cmd-select" onchange="handleQuickCmd(this)"><option value="">Commands…</option>${optHtml}</select>` +
        `<div id="customCmdWrap" class="custom-cmd-wrap" style="display:none;">` +
        `<input type="text" class="custom-cmd-input" id="customCmdInput" placeholder="Type command…" onkeydown="if(event.key==='Enter'){sendCustomCmd();event.preventDefault();}">` +
        `<button class="quick-btn" onclick="sendCustomCmd()" title="Send">&#10148;</button>` +
        `<button class="quick-btn" onclick="hideCustomCmd()" title="Cancel">&#10005;</button></div>`;
    })
    .catch(() => {});
}

function handleQuickCmd(sel) {
  const val = sel.value;
  sel.selectedIndex = 0;
  if (!val) return;
  if (val === '__custom__') {
    const wrap = document.getElementById('customCmdWrap');
    if (wrap) { wrap.style.display = 'flex'; document.getElementById('customCmdInput')?.focus(); }
    return;
  }
  sendSavedCmd(val);
}

function sendCustomCmd() {
  const input = document.getElementById('customCmdInput');
  if (!input || !input.value.trim()) return;
  sendSavedCmd(input.value);
  input.value = '';
  hideCustomCmd();
}

function hideCustomCmd() {
  const wrap = document.getElementById('customCmdWrap');
  if (wrap) wrap.style.display = 'none';
  const input = document.getElementById('customCmdInput');
  if (input) input.value = '';
}

function sendSavedCmd(cmd) {
  if (!state.activeSession) return;
  if (cmd === '\n' || cmd === '') {
    send('send_input', { session_id: state.activeSession, text: '' });
  } else if (cmd === '\x03') {
    send('command', { text: `sendkey ${state.activeSession}: C-c` });
  } else {
    send('send_input', { session_id: state.activeSession, text: cmd });
  }
}

function deleteSession(sessionId) {
  showConfirmModal('Delete session and data?', () => {
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
  });
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
          <select id="profileSelect" class="form-select" style="margin-top:6px;">
            <option value="">Default (no profile)</option>
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
          <label for="resumeSelect">Resume previous session <span style="color:var(--text2);font-size:11px;">(optional)</span></label>
          <select id="resumeSelect" class="form-select" onchange="handleResumeSelect(this)">
            <option value="">Start fresh</option>
          </select>
          <div id="resumeCustomWrap" class="custom-cmd-wrap" style="display:none;margin-top:6px;">
            <input type="text" class="form-input" id="resumeCustomInput" placeholder="Session ID or name…" style="flex:1;" />
            <button class="quick-btn" onclick="document.getElementById('resumeCustomWrap').style.display='none';document.getElementById('resumeSelect').value=''" title="Cancel">&#10005;</button>
          </div>
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

  // Load backends, session backlog, and resume dropdown
  fetchBackends();
  renderSessionBacklog();
  populateResumeDropdown();
  populateProfileDropdown();

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

function populateProfileDropdown() {
  const sel = document.getElementById('profileSelect');
  if (!sel) return;
  apiFetch('/api/profiles').then(profiles => {
    let html = '<option value="">Default (no profile)</option>';
    if (profiles && typeof profiles === 'object') {
      const names = Object.keys(profiles).sort();
      if (names.length > 0) {
        html += '<optgroup label="Profiles">';
        html += names.map(name => {
          const p = profiles[name];
          return `<option value="${escHtml(name)}">${escHtml(name)} (${escHtml(p.backend || '?')})</option>`;
        }).join('');
        html += '</optgroup>';
      }
    }
    sel.innerHTML = html;
  }).catch(() => {});
}

function populateResumeDropdown() {
  const sel = document.getElementById('resumeSelect');
  if (!sel) return;
  const done = state.sessions.filter(s =>
    s.state === 'complete' || s.state === 'failed' || s.state === 'killed'
  ).sort((a, b) => new Date(b.updated_at) - new Date(a.updated_at)).slice(0, 30);

  let html = '<option value="">Start fresh</option>';
  if (done.length > 0) {
    html += '<optgroup label="Previous sessions">';
    html += done.map(s => {
      const label = s.name || s.task || s.id;
      const short = label.length > 50 ? label.slice(0, 47) + '…' : label;
      return `<option value="${escHtml(s.full_id || s.id)}" data-name="${escHtml(s.name || '')}" data-task="${escHtml(s.task || '')}" data-dir="${escHtml(s.project_dir || '')}" data-backend="${escHtml(s.llm_backend || '')}">${escHtml(s.id)} — ${escHtml(short)}</option>`;
    }).join('');
    html += '</optgroup>';
  }
  html += '<optgroup label=""><option value="__custom__">Custom session ID…</option></optgroup>';
  sel.innerHTML = html;
}

function handleResumeSelect(sel) {
  const val = sel.value;
  if (val === '__custom__') {
    document.getElementById('resumeCustomWrap').style.display = 'flex';
    document.getElementById('resumeCustomInput')?.focus();
    return;
  }
  // Hide custom input if visible
  const customWrap = document.getElementById('resumeCustomWrap');
  if (customWrap) customWrap.style.display = 'none';

  // Auto-fill form fields from selected session
  if (val) {
    const opt = sel.selectedOptions[0];
    if (opt) {
      const nameEl = document.getElementById('sessionNameInput');
      const taskEl = document.getElementById('taskInput');
      const dirDisplay = document.getElementById('selectedDirDisplay');
      const backendEl = document.getElementById('backendSelect');
      const taskDetails = document.getElementById('taskDetailsSection');

      if (nameEl && opt.dataset.name) nameEl.value = opt.dataset.name;
      if (opt.dataset.task) {
        if (taskEl) taskEl.value = opt.dataset.task;
        if (taskDetails) taskDetails.open = true;
      }
      if (opt.dataset.dir) {
        newSessionState.selectedDir = opt.dataset.dir;
        if (dirDisplay) dirDisplay.textContent = opt.dataset.dir;
      }
      if (backendEl && opt.dataset.backend) {
        for (const o of backendEl.options) {
          if (o.value === opt.dataset.backend) { o.selected = true; break; }
        }
      }
    }
  }
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
  const resumeSel = document.getElementById('resumeSelect');
  const resumeCustom = document.getElementById('resumeCustomInput');
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
    resume_id: (resumeSel && resumeSel.value === '__custom__' && resumeCustom)
      ? resumeCustom.value.trim()
      : (resumeSel && resumeSel.value && resumeSel.value !== '__custom__' ? resumeSel.value : ''),
    profile: document.getElementById('profileSelect')?.value || '',
    auto_git_commit: gitCommit ? gitCommit.checked : true,
    auto_git_init: gitInit ? gitInit.checked : false,
  };

  // Use REST so we get the full session object back and can navigate directly to it.
  apiFetch('/api/sessions/start', { method: 'POST', body: JSON.stringify(payload) })
    .then(sess => {
      taskInput.value = '';
      if (nameInput) nameInput.value = '';
      if (resumeSel) resumeSel.value = '';
      if (resumeCustom) resumeCustom.value = '';
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
let _settingsTab = localStorage.getItem('cs_settings_tab') || 'monitor';
const _expandedSessions = new Set(); // track expanded session rows across re-renders
const _expandedChannels = new Set(); // track expanded channel rows across re-renders
function switchSettingsTab(tab) {
  _settingsTab = tab;
  localStorage.setItem('cs_settings_tab', tab);
  document.querySelectorAll('.settings-section[data-group]').forEach(s => {
    s.style.display = s.dataset.group === tab ? '' : 'none';
  });
  document.querySelectorAll('.settings-tab-btn').forEach(b => {
    b.classList.toggle('active', b.dataset.tab === tab);
  });
}
window.switchSettingsTab = switchSettingsTab;

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

  const stab = _settingsTab;
  const tabBtns = [
    ['monitor','Monitor'],['general','General'],['comms','Comms'],['llm','LLM'],['about','About']
  ].map(([id,label]) => `<button class="settings-tab-btn output-tab ${stab===id?'active':''}" data-tab="${id}" onclick="switchSettingsTab('${id}')">${label}</button>`).join('');

  view.innerHTML = `
    <div class="view-content">
      <div class="settings-tabs-bar" style="display:flex;gap:2px;padding:4px 8px;border-bottom:1px solid var(--border);background:var(--bg2);position:sticky;top:0;z-index:10;">
        ${tabBtns}
      </div>
      <div class="settings-view">

        <div class="settings-section" data-group="comms" style="${stab!=='comms'?'display:none':''}">
          ${settingsSectionHeader('comms_auth', 'Authentication')}
          <div id="settings-sec-comms_auth" style="${secContent('comms_auth')}">
            <div class="settings-row" style="flex-direction:column;align-items:stretch;">
              <div class="settings-label">Browser token</div>
              <div style="display:flex;gap:6px;align-items:center;margin-top:4px;">
                <input type="password" class="form-input" id="tokenInput" value="${escHtml(state.token)}" placeholder="Token for this browser session" style="flex:1;" />
                <button class="btn-secondary" style="font-size:11px;white-space:nowrap;" onclick="saveToken()">Save &amp; Reconnect</button>
              </div>
            </div>
            <div class="settings-row" style="justify-content:space-between;">
              <div class="settings-label">Server bearer token</div>
              <input type="password" class="form-input general-cfg-input" id="cfgWebToken"
                onchange="saveGeneralField('server.token', this.value)" />
            </div>
            <div class="settings-row" style="justify-content:space-between;">
              <div class="settings-label">MCP SSE bearer token</div>
              <input type="password" class="form-input general-cfg-input" id="cfgMcpToken"
                onchange="saveGeneralField('mcp.token', this.value)" />
            </div>
          </div>
        </div>

        <div class="settings-section" data-group="comms" style="${stab!=='comms'?'display:none':''}">
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

        <div class="settings-section" data-group="comms" style="${stab!=='comms'?'display:none':''}">
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

        <div class="settings-section" data-group="llm" style="${stab!=='llm'?'display:none':''}">
          ${settingsSectionHeader('llm', 'LLM Configuration')}
          <div id="settings-sec-llm" style="${secContent('llm')}">
            <div id="llmConfigList" style="color:var(--text2);font-size:13px;">Loading…</div>
          </div>
        </div>

        ${GENERAL_CONFIG_FIELDS.map(sec => `
        <div class="settings-section" data-group="general" style="${stab!=='general'?'display:none':''}">
          ${settingsSectionHeader('gc_'+sec.id, sec.section)}
          <div id="settings-sec-gc_${sec.id}" style="${secContent('gc_'+sec.id)}">
            <div id="gcfg_${sec.id}" style="color:var(--text2);font-size:13px;">Loading…</div>
          </div>
        </div>`).join('')}

        <div class="settings-section" data-group="general" style="${stab!=='general'?'display:none':''}">
          ${settingsSectionHeader('gc_notifs', 'Notifications')}
          <div id="settings-sec-gc_notifs" style="${secContent('gc_notifs')}">
            <div class="settings-row">
              <div class="settings-label">Status</div>
              <div class="settings-value">${escHtml(notifText)}</div>
            </div>
            <div class="settings-row">
              <button class="btn-success" onclick="requestNotificationPermission()">Request Permission</button>
            </div>
            <!-- suppress_active_toasts and auto_restart moved to config cards -->
          </div>
        </div>

        <!-- daemon log moved to monitor tab -->

        <div class="settings-section" data-group="monitor" style="${stab!=='monitor'?'display:none':''}">
          ${settingsSectionHeader('stats', 'System Statistics')}
          <div id="settings-sec-stats" style="${secContent('stats')}">
            <div id="statsPanel"><div style="color:var(--text2);font-size:13px;padding:8px;">Loading…</div></div>
          </div>
        </div>

        <div class="settings-section" data-group="llm" style="${stab!=='llm'?'display:none':''}">
          ${settingsSectionHeader('detection', 'Detection Filters')}
          <div id="settings-sec-detection" style="${secContent('detection')}">
            <div id="detectionFiltersList"><div style="color:var(--text2);font-size:13px;">Loading…</div></div>
          </div>
        </div>

        <div class="settings-section" data-group="monitor" style="${stab!=='monitor'?'display:none':''}">
          ${settingsSectionHeader('schedules', 'Scheduled Events')}
          <div id="settings-sec-schedules" style="${secContent('schedules')}">
            <div id="schedulesList"><div style="color:var(--text2);font-size:13px;">Loading…</div></div>
          </div>
        </div>

        <div class="settings-section" data-group="monitor" style="${stab!=='monitor'?'display:none':''}">
          ${settingsSectionHeader('daemonlog', 'Daemon Log')}
          <div id="settings-sec-daemonlog" style="${secContent('daemonlog')}">
            <div id="daemonLogPanel" style="font-size:11px;font-family:monospace;color:var(--text2);max-height:300px;overflow-y:auto;background:var(--bg);border:1px solid var(--border);border-radius:6px;padding:6px;">Loading…</div>
            <div style="display:flex;gap:8px;padding:6px 0;align-items:center;">
              <button class="btn-secondary" style="font-size:11px;" onclick="loadDaemonLog(0)">Newest</button>
              <button class="btn-secondary" style="font-size:11px;" onclick="loadDaemonLog((state._logOffset||0)+50)">Older</button>
              <span id="daemonLogInfo" style="font-size:10px;color:var(--text2);"></span>
            </div>
          </div>
        </div>

        <div class="settings-section" data-group="llm" style="${stab!=='llm'?'display:none':''}">
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

        <div class="settings-section" data-group="llm" style="${stab!=='llm'?'display:none':''}">
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

        <div class="settings-section" data-group="about" style="${stab!=='about'?'display:none':''}">
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

        <div class="settings-section" data-group="about" style="${stab!=='about'?'display:none':''}">
          <div class="settings-section-title">About</div>
          <div style="text-align:center;padding:16px 0 8px;">
            <img src="/favicon.svg" alt="Datawatch" style="width:64px;height:64px;margin-bottom:8px;" />
            <div style="font-size:18px;font-weight:700;color:var(--text);letter-spacing:1px;">datawatch</div>
            <div style="font-size:11px;color:var(--text2);margin-top:2px;">AI Session Monitor & Bridge</div>
          </div>
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
  // Populate auth token fields in comms tab
  fetch('/api/config', { headers: tokenHeader() }).then(r => r.ok ? r.json() : null).then(cfg => {
    if (!cfg) return;
    const wt = document.getElementById('cfgWebToken');
    if (wt) wt.value = cfg.server?.token || '';
    const mt = document.getElementById('cfgMcpToken');
    if (mt) mt.value = cfg.mcp?.token || '';
  }).catch(() => {});
  loadSavedCommands();
  loadSchedulesList();
  loadStatsPanel();
  loadDetectionFilters();
  loadFilters();
  loadVersionInfo();
  loadLLMConfig();
  loadGeneralConfig();
  loadDaemonLog(0);
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
  { id: 'dw', section: 'Datawatch', fields: [
    { key: 'session.log_level', label: 'Log level', type: 'select', options: ['info','debug','warn','error'] },
    { key: 'server.auto_restart_on_config', label: 'Auto-restart on config save', type: 'toggle' },
    { key: 'session.llm_backend', label: 'Default LLM backend', type: 'llm_select' },
  ]},
  { id: 'autoupdate', section: 'Auto-Update', fields: [
    { key: 'update.enabled', label: 'Enabled', type: 'toggle' },
    { key: 'update.schedule', label: 'Schedule', type: 'select', options: ['hourly','daily','weekly'] },
    { key: 'update.time_of_day', label: 'Time of day (HH:MM)', type: 'text' },
  ]},
  { id: 'websrv', section: 'Web Server', fields: [
    { key: 'server.enabled', label: 'Enabled', type: 'toggle' },
    { key: 'server.host', label: 'Bind interface', type: 'interface_select' },
    { key: 'server.port', label: 'Port', type: 'number' },
    { key: 'server.tls', label: 'TLS enabled', type: 'toggle' },
    { key: 'server.tls_port', label: 'TLS port (0=replace main port)', type: 'number' },
    { key: 'server.tls_auto_generate', label: 'TLS auto-generate cert', type: 'toggle' },
    { key: 'server.tls_cert', label: 'TLS cert path', type: 'text' },
    { key: 'server.tls_key', label: 'TLS key path', type: 'text' },
    { key: 'server.channel_port', label: 'Channel port (0=random)', type: 'number' },
  ]},
  { id: 'mcpsrv', section: 'MCP Server', fields: [
    { key: 'mcp.enabled', label: 'Enabled (stdio)', type: 'toggle' },
    { key: 'mcp.sse_enabled', label: 'SSE enabled (HTTP)', type: 'toggle' },
    { key: 'mcp.sse_host', label: 'SSE bind interface', type: 'interface_select' },
    { key: 'mcp.sse_port', label: 'SSE port', type: 'number' },
    { key: 'mcp.tls_enabled', label: 'TLS enabled', type: 'toggle' },
    { key: 'mcp.tls_auto_generate', label: 'TLS auto-generate cert', type: 'toggle' },
    { key: 'mcp.tls_cert', label: 'TLS cert path', type: 'text' },
    { key: 'mcp.tls_key', label: 'TLS key path', type: 'text' },
  ]},
  { id: 'sess', section: 'Session', fields: [
    { key: 'session.max_sessions', label: 'Max concurrent sessions', type: 'number' },
    { key: 'session.input_idle_timeout', label: 'Input idle timeout (sec)', type: 'number' },
    { key: 'session.tail_lines', label: 'Tail lines', type: 'number' },
    { key: 'session.alert_context_lines', label: 'Alert context lines', type: 'number', placeholder: '10' },
    { key: 'session.default_project_dir', label: 'Default project dir', type: 'dir_browse' },
    { key: 'session.root_path', label: 'File browser root path', type: 'dir_browse' },
    { key: 'session.console_cols', label: 'Default console width (cols)', type: 'number', placeholder: '80' },
    { key: 'session.console_rows', label: 'Default console height (rows)', type: 'number', placeholder: '24' },
    { key: 'server.recent_session_minutes', label: 'Recent session visibility (min)', type: 'number' },
    { key: 'session.skip_permissions', label: 'Claude skip permissions', type: 'toggle' },
    { key: 'session.channel_enabled', label: 'Claude channel mode', type: 'toggle' },
    { key: 'session.auto_git_init', label: 'Auto git init', type: 'toggle' },
    { key: 'session.auto_git_commit', label: 'Auto git commit', type: 'toggle' },
    { key: 'session.kill_sessions_on_exit', label: 'Kill sessions on exit', type: 'toggle' },
    { key: 'session.mcp_max_retries', label: 'MCP auto-retry limit', type: 'number' },
    { key: 'server.suppress_active_toasts', label: 'Suppress toasts for active session', type: 'toggle' },
  ]},
  { id: 'profiles', section: 'Profiles & Fallback', fields: [
    { key: 'session.fallback_chain', label: 'Fallback chain (comma-separated profile names)', type: 'text', placeholder: 'e.g. claude-personal,gemini-backup' },
  ]},
  { id: 'rtk', section: 'RTK (Token Savings)', fields: [
    { key: 'rtk.enabled', label: 'Enable RTK integration', type: 'toggle' },
    { key: 'rtk.binary', label: 'RTK binary path', type: 'text', placeholder: 'rtk' },
    { key: 'rtk.show_savings', label: 'Show savings in stats', type: 'toggle' },
    { key: 'rtk.auto_init', label: 'Auto-init hooks if missing', type: 'toggle' },
    { key: 'rtk.discover_interval', label: 'Discover check interval (sec, 0=off)', type: 'number' },
  ]},
  { id: 'whisper', section: 'Voice Input (Whisper)', fields: [
    { key: 'whisper.enabled', label: 'Enable voice transcription', type: 'toggle' },
    { key: 'whisper.model', label: 'Whisper model', type: 'select', options: ['tiny','base','small','medium','large'] },
    { key: 'whisper.language', label: 'Language (ISO 639-1 code or "auto")', type: 'text', placeholder: 'en' },
    { key: 'whisper.venv_path', label: 'Python venv path', type: 'text', placeholder: '.venv' },
  ]},
];

function loadDaemonLog(offset) {
  const el = document.getElementById('daemonLogPanel');
  if (!el) return;
  state._logOffset = offset || 0;
  apiFetch(`/api/logs?lines=50&offset=${state._logOffset}`).then(data => {
    if (!data?.lines) { el.textContent = 'Log unavailable'; return; }
    el.innerHTML = data.lines.map(line => {
      // Color-code log lines
      let color = 'var(--text2)';
      if (line.includes('[warn]') || line.includes('WARNING')) color = 'var(--warning)';
      else if (line.includes('ERROR') || line.includes('[error]')) color = 'var(--error)';
      else if (line.includes('[ebpf]')) color = 'var(--success)';
      else if (line.includes('[debug]')) color = 'var(--accent2)';
      return `<div style="color:${color};padding:1px 0;white-space:pre-wrap;word-break:break-all;">${escHtml(line)}</div>`;
    }).join('');
    const info = document.getElementById('daemonLogInfo');
    if (info) info.textContent = `Showing ${data.lines.length} of ${data.total} lines (offset ${state._logOffset})`;
  }).catch(() => { el.textContent = 'Log unavailable'; });
}
window.loadDaemonLog = loadDaemonLog;

// Auto-refresh daemon log every 10s when visible on monitor tab
setInterval(() => {
  if (state.activeView === 'settings' && _settingsTab === 'monitor' && document.getElementById('daemonLogPanel')) {
    loadDaemonLog(state._logOffset || 0);
  }
}, 10000);

function loadGeneralConfig() {
  Promise.all([
    fetch('/api/config', { headers: tokenHeader() }).then(r => r.ok ? r.json() : null),
    fetch('/api/backends', { headers: tokenHeader() }).then(r => r.ok ? r.json() : null),
    fetch('/api/interfaces', { headers: tokenHeader() }).then(r => r.ok ? r.json() : [])
  ]).then(([cfg, backendsData, interfaces]) => {
      if (!cfg) return;
      state._interfaces = interfaces || [];
      const enabledBackends = (backendsData?.llm || []).filter(b => b.enabled).map(b => b.name);
      for (const sec of GENERAL_CONFIG_FIELDS) {
        const el = document.getElementById('gcfg_' + sec.id);
        if (!el) continue;
        let html = '';
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
            const ifaces = (interfaces && interfaces.length > 0) ? interfaces : [
              { addr: '0.0.0.0', label: '0.0.0.0 (all interfaces)' },
              { addr: '127.0.0.1', label: '127.0.0.1 (localhost)' },
            ];
            // Detect which interface the browser is connected through
            const connIf = typeof _resolveConnectedInterface === 'function' ? _resolveConnectedInterface() : null;
            const checkboxes = ifaces.map(iface => {
              const checked = currentVals.includes(iface.addr);
              const isConnected = connIf && iface.addr === connIf.addr;
              let badge = '';
              if (f.key === 'server.host' && isConnected) {
                badge = ' <span style="color:var(--success);font-size:10px;">(connected)</span>';
              }
              return `<label style="display:flex;align-items:center;gap:6px;font-size:12px;cursor:pointer;padding:3px 0;">
                <input type="checkbox" ${checked ? 'checked' : ''} value="${escHtml(iface.addr)}"
                  onchange="saveInterfaceField('${f.key}', this.closest('.iface-list'), this)" />
                <span style="font-family:monospace;color:var(--text);">${escHtml(iface.label)}${badge}</span>
              </label>`;
            }).join('');
            html += `<div class="settings-row" style="flex-direction:column;align-items:stretch;">
              <div class="settings-label">${escHtml(f.label)}</div>
              <div class="iface-list" style="display:flex;flex-direction:column;gap:2px;margin-top:4px;padding:8px 10px;background:var(--bg);border:1px solid var(--border);border-radius:6px;">
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
                ${f.placeholder ? 'placeholder="' + escHtml(f.placeholder) + '"' : ''}
                onchange="saveGeneralField('${f.key}', ${f.type === 'number' ? 'Number(this.value)' : 'this.value'})" />
            </div>`;
          }
        }
        el.innerHTML = html;
      }
    })
    .catch(() => {});
}

function _resolveConnectedInterface() {
  const browserHost = location.hostname;
  const ifaces = state._interfaces || [];
  // Match hostname to interface: direct IP match, label match, name match, or Tailscale MagicDNS
  return ifaces.find(i =>
    i.addr === browserHost ||
    i.label?.includes(browserHost) ||
    i.name === browserHost ||
    (browserHost.match(/^[a-zA-Z]/) && i.name?.includes('tailscale') && !['0.0.0.0','127.0.0.1'].includes(i.addr))
  ) || null;
}

function saveInterfaceField(key, listEl, changedEl) {
  const allBoxes = Array.from(listEl.querySelectorAll('input[type="checkbox"]'));
  const allBox = allBoxes.find(cb => cb.value === '0.0.0.0');
  const localhostBox = allBoxes.find(cb => cb.value === '127.0.0.1');
  const otherBoxes = allBoxes.filter(cb => cb.value !== '0.0.0.0');

  const connIface = _resolveConnectedInterface();
  const connectedIP = connIface?.addr || null;
  const browserHost = location.hostname;

  _dbg('IFACE', `save key=${key} changed=${changedEl?.value} checked=${changedEl?.checked} browser=${browserHost} connIP=${connectedIP}`);

  // Rule 1: Selecting "all" deselects everything else
  if (changedEl?.value === '0.0.0.0' && changedEl.checked) {
    otherBoxes.forEach(cb => { cb.checked = false; });
    _dbg('IFACE', 'All selected → unchecked others');
  }
  // Rule 2: Selecting any specific deselects "all", forces localhost on
  else if (changedEl?.value !== '0.0.0.0' && changedEl?.checked && allBox) {
    allBox.checked = false;
    // Force localhost always available when switching from all to specific
    if (localhostBox && !localhostBox.checked) {
      localhostBox.checked = true;
      _dbg('IFACE', 'Forced localhost on (always required for specific binding)');
    }
    _dbg('IFACE', `Specific ${changedEl.value} selected → unchecked all, ensured localhost`);
  }
  // Rule 3: Unchecking localhost when not on all — block it
  else if (changedEl?.value === '127.0.0.1' && !changedEl.checked && !(allBox?.checked)) {
    changedEl.checked = true;
    showToast('Localhost must remain enabled when binding to specific interfaces', 'warning', 3000);
    _dbg('IFACE', 'Blocked localhost uncheck — required for specific binding');
    return;
  }

  const finalChecked = allBoxes.filter(cb => cb.checked).map(cb => cb.value);
  _dbg('IFACE', `finalChecked=${JSON.stringify(finalChecked)}`);

  if (finalChecked.length === 0) {
    showToast('Select at least one interface', 'warning', 2000);
    if (allBox) allBox.checked = true;
    return;
  }

  // For web server: warn if connected interface is not covered
  if (key === 'server.host') {
    const isAllSelected = finalChecked.includes('0.0.0.0');
    const isConnectedSelected = connectedIP && finalChecked.includes(connectedIP);
    const isLocalhostSelected = finalChecked.includes('127.0.0.1');
    const isOnLocalhost = browserHost === 'localhost' || browserHost === '127.0.0.1';
    _dbg('IFACE', `safety: isAll=${isAllSelected} isConn=${isConnectedSelected} isLH=${isLocalhostSelected} isOnLH=${isOnLocalhost}`);

    if (!isAllSelected && !isConnectedSelected && !(isOnLocalhost && isLocalhostSelected)) {
      showToast(`Warning: your connection (${browserHost}${connectedIP ? ' → ' + connectedIP : ''}) is not selected. You may lose access after restart.`, 'warning', 5000);
    }
  }

  const val = finalChecked.join(',');
  fetch('/api/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', ...tokenHeader() },
    body: JSON.stringify({ [key]: val }),
  }).then(r => {
    if (r.ok) {
      showToast('Saved: ' + val + '. Restart required.', 'success', 3000);
      const hint = document.getElementById('restartHint');
      if (hint) hint.style.display = 'inline';
      if (state.autoRestartOnConfig) triggerAutoRestart();
      setTimeout(() => loadGeneralConfig(), 1000);
    } else {
      r.text().then(t => { _dbg('IFACE', `save failed: ${r.status} ${t}`); });
      showToast('Save failed', 'error');
    }
  }).catch(e => { _dbg('IFACE', `save error: ${e}`); showToast('Save failed', 'error'); });
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
  apiFetch('/api/health').then(data => {
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
// Console size fields shared by all LLM backends
const CONSOLE_SIZE_FIELDS = [
  { key:'console_cols', label:'Console width (cols)', type:'number', placeholder:'80' },
  { key:'console_rows', label:'Console height (rows)', type:'number', placeholder:'24' },
  { key:'output_mode', label:'Output mode', type:'select_inline', options:['terminal','log'], placeholder:'terminal' },
  { key:'input_mode', label:'Input mode', type:'select_inline', options:['tmux','none'], placeholder:'tmux' },
];
const GIT_FIELDS = [
  { key:'auto_git_init', label:'Auto git init', type:'checkbox', section:'session' },
  { key:'auto_git_commit', label:'Auto git commit', type:'checkbox', section:'session' },
];

const LLM_FIELDS = {
  'claude-code': [
    { key:'claude_code_bin', label:'Claude binary', type:'text', placeholder:'claude', section:'session' },
    { key:'claude_enabled', label:'Enabled', type:'checkbox', section:'session' },
    { key:'skip_permissions', label:'Skip permissions', type:'checkbox', section:'session' },
    { key:'channel_enabled', label:'Channel mode', type:'checkbox', section:'session' },
    ...GIT_FIELDS,
    { key:'console_cols', label:'Console width (cols)', type:'number', placeholder:'120', section:'session' },
    { key:'console_rows', label:'Console height (rows)', type:'number', placeholder:'40', section:'session' },
  ],
  'aider':          [{ key:'binary', label:'Binary path', type:'text', placeholder:'aider' }, ...GIT_FIELDS, ...CONSOLE_SIZE_FIELDS],
  'goose':          [{ key:'binary', label:'Binary path', type:'text', placeholder:'goose' }, ...GIT_FIELDS, ...CONSOLE_SIZE_FIELDS],
  'gemini':         [{ key:'binary', label:'Binary path', type:'text', placeholder:'gemini' }, ...GIT_FIELDS, ...CONSOLE_SIZE_FIELDS],
  'ollama':         [{ key:'model', label:'Model', type:'ollama_model_select' }, { key:'host', label:'Host URL', type:'text', placeholder:'http://localhost:11434' }, ...GIT_FIELDS, ...CONSOLE_SIZE_FIELDS],
  'opencode':       [{ key:'binary', label:'Binary path', type:'text', placeholder:'opencode' }, ...GIT_FIELDS, ...CONSOLE_SIZE_FIELDS],
  'opencode-acp':   [{ key:'binary', label:'Binary path', type:'text', placeholder:'opencode' }, { key:'acp_startup_timeout', label:'Startup timeout (sec)', type:'number', placeholder:'30' }, { key:'acp_health_interval', label:'Health interval (sec)', type:'number', placeholder:'5' }, { key:'acp_message_timeout', label:'Message timeout (sec)', type:'number', placeholder:'120' }, ...GIT_FIELDS, ...CONSOLE_SIZE_FIELDS],
  'opencode-prompt':[{ key:'binary', label:'Binary path', type:'text', placeholder:'opencode' }, ...GIT_FIELDS, ...CONSOLE_SIZE_FIELDS],
  'openwebui':      [{ key:'url', label:'Server URL', type:'text', placeholder:'http://localhost:3000' }, { key:'api_key', label:'API Key', type:'password' }, { key:'model', label:'Model', type:'openwebui_model_select' }, ...GIT_FIELDS, ...CONSOLE_SIZE_FIELDS],
  'shell':          [{ key:'script_path', label:'Script path (empty = interactive shell)', type:'text' }, ...GIT_FIELDS, ...CONSOLE_SIZE_FIELDS],
};

// Config section names in config.yaml for each LLM
const LLM_CFG_SECTION = {
  'claude-code':'session','aider':'aider','goose':'goose','gemini':'gemini','ollama':'ollama',
  'opencode':'opencode','opencode-acp':'opencode_acp','opencode-prompt':'opencode_prompt','openwebui':'openwebui','shell':'shell_backend'
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
    if (f.type === 'select_inline' && f.options) {
      const opts = f.options.map(o =>
        `<option value="${escHtml(o)}" ${String(val || f.placeholder) === o ? 'selected' : ''}>${escHtml(o)}</option>`
      ).join('');
      return `<div class="popup-field">
        <label class="popup-field-label">${escHtml(f.label)}</label>
        <select id="bkf_${escHtml(f.key)}" class="form-select" style="width:100%;">${opts}</select>
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
function showStateOverride(sessionId, el) {
  const existing = document.getElementById('stateOverrideMenu');
  if (existing) { existing.remove(); return; }
  const states = ['running', 'waiting_input', 'complete', 'killed', 'failed'];
  const menu = document.createElement('div');
  menu.id = 'stateOverrideMenu';
  menu.style.cssText = 'position:absolute;z-index:100;background:var(--bg2);border:1px solid var(--border);border-radius:8px;padding:4px;box-shadow:0 4px 12px rgba(0,0,0,0.3);';
  const rect = el.getBoundingClientRect();
  menu.style.top = (rect.bottom + 4) + 'px';
  menu.style.left = rect.left + 'px';
  menu.innerHTML = states.map(s =>
    `<div style="padding:4px 12px;font-size:11px;cursor:pointer;border-radius:4px;" onmouseover="this.style.background='var(--bg3)'" onmouseout="this.style.background=''" onclick="setSessionState('${sessionId}','${s}')">${s}</div>`
  ).join('');
  document.body.appendChild(menu);
  setTimeout(() => document.addEventListener('click', function rem() { menu.remove(); document.removeEventListener('click', rem); }, { once: true }), 10);
}

function setSessionState(sessionId, newState) {
  document.getElementById('stateOverrideMenu')?.remove();
  apiFetch('/api/sessions/state', {
    method: 'POST',
    body: JSON.stringify({ id: sessionId, state: newState }),
  }).then(() => {
    showToast('State set to ' + newState, 'success', 1500);
  }).catch(err => showToast('Failed: ' + err.message, 'error'));
}

function showConfirmModal(message, onConfirm) {
  const existing = document.getElementById('confirmModal');
  if (existing) existing.remove();
  const modal = document.createElement('div');
  modal.id = 'confirmModal';
  modal.className = 'confirm-modal-overlay';
  modal.innerHTML = `<div class="confirm-modal">
    <div style="font-size:13px;color:var(--text);margin-bottom:12px;">${escHtml(message)}</div>
    <div style="display:flex;gap:8px;justify-content:flex-end;">
      <button class="btn-secondary" style="font-size:12px;padding:4px 16px;" onclick="document.getElementById('confirmModal').remove()">No</button>
      <button class="btn-stop" style="font-size:12px;padding:4px 16px;" id="confirmYesBtn">Yes</button>
    </div>
  </div>`;
  modal.addEventListener('click', e => { if (e.target === modal) modal.remove(); });
  document.body.appendChild(modal);
  const yesBtn = document.getElementById('confirmYesBtn');
  yesBtn.onclick = () => { modal.remove(); onConfirm(); };
  yesBtn.focus(); // Auto-select so Enter confirms immediately
}

function showToast(message, type = 'info', duration = 3500) {
  let container = document.querySelector('.toast-container');
  if (!container) {
    container = document.createElement('div');
    container.className = 'toast-container';
    (document.querySelector('.app') || document.body).appendChild(container);
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

// Debug panel — triple-tap status dot to open
let _debugTapCount = 0, _debugTapTimer = null;
document.addEventListener('DOMContentLoaded', () => {
  const dot = document.getElementById('statusDot');
  if (dot) dot.addEventListener('click', () => {
    _debugTapCount++;
    if (_debugTapTimer) clearTimeout(_debugTapTimer);
    _debugTapTimer = setTimeout(() => { _debugTapCount = 0; }, 500);
    if (_debugTapCount >= 3) {
      _debugTapCount = 0;
      showDebugPanel();
    }
  });
});

function showDebugPanel() {
  const existing = document.getElementById('debugPanel');
  if (existing) { existing.remove(); return; }
  const panel = document.createElement('div');
  panel.id = 'debugPanel';
  panel.style.cssText = 'position:fixed;inset:0;z-index:999;background:rgba(0,0,0,0.8);display:flex;align-items:center;justify-content:center;';
  const entries = window._debugLog.slice(-50).reverse().map(e =>
    `<div style="font-size:10px;font-family:monospace;padding:1px 0;"><span style="color:var(--text2);">${e.ts}</span> <span style="color:${e.type==='ERROR'?'var(--error)':e.type==='WS'?'var(--accent2)':'var(--warning)'};font-weight:600;">${e.type}</span> ${escHtml(e.msg)}</div>`
  ).join('');
  panel.innerHTML = `<div style="background:var(--bg2);border:1px solid var(--border);border-radius:12px;padding:16px;max-width:500px;width:90%;max-height:80vh;overflow-y:auto;">
    <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:8px;">
      <strong style="color:var(--text);">Debug Console</strong>
      <button class="btn-icon" onclick="document.getElementById('debugPanel').remove()">&#10005;</button>
    </div>
    <div style="font-size:10px;color:var(--text2);margin-bottom:8px;">Last ${Math.min(50, window._debugLog.length)} events. Access full log: window._debugLog</div>
    ${entries || '<div style="color:var(--text2);font-size:11px;">No debug events captured.</div>'}
    <div style="margin-top:8px;display:flex;gap:6px;">
      <button class="btn-secondary" style="font-size:10px;" onclick="window._debugLog=[];showDebugPanel();">Clear</button>
      <button class="btn-secondary" style="font-size:10px;" onclick="navigator.clipboard.writeText(JSON.stringify(window._debugLog,null,2));showToast('Copied','success',1000);">Copy JSON</button>
    </div>
  </div>`;
  panel.addEventListener('click', e => { if (e.target === panel) panel.remove(); });
  document.body.appendChild(panel);
}
window.showDebugPanel = showDebugPanel;

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

      // Quick-reply dropdown only on the first (latest) alert for a waiting session
      let replyBtns = '';
      if (isFirst && isWaiting && cmds && cmds.length > 0 && a.session_id) {
        const sessId = JSON.stringify(a.session_id);
        const opts = cmds.map(c => {
          const safeVal = escHtml(c.command);
          return `<option value="${safeVal}">${escHtml(c.name)}</option>`;
        }).join('');
        replyBtns = `<div class="quick-input-row" style="margin-top:6px;"><select class="quick-cmd-select" onchange="if(this.value){alertSendCmd(${sessId},this.value);this.selectedIndex=0;}"><option value="">Quick reply…</option>${opts}</select></div>`;
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

function loadStatsPanel() {
  const el = document.getElementById('statsPanel');
  if (!el) return;
  apiFetch('/api/stats').then(data => {
    renderStatsData(el, data);
  }).catch(() => { el.innerHTML = '<div style="color:var(--text2);font-size:12px;padding:8px;">Stats unavailable.</div>'; });
}

function renderStatsData(el, data) {
    if (!data || !data.timestamp) { el.innerHTML = '<div style="color:var(--text2);font-size:12px;padding:8px;">Stats not available.</div>'; return; }
    const fmt = (bytes) => {
      if (bytes > 1e9) return (bytes/1e9).toFixed(1) + ' GB';
      if (bytes > 1e6) return (bytes/1e6).toFixed(1) + ' MB';
      if (bytes > 1e3) return (bytes/1e3).toFixed(1) + ' KB';
      return bytes + ' B';
    };
    const pct = (used, total) => total > 0 ? Math.round(100*used/total) : 0;
    const bar = (label, val, max, color, extra) => {
      const p = max > 0 ? Math.min(100, Math.round(100*val/max)) : 0;
      return `<div class="stat-card">
        <div style="display:flex;justify-content:space-between;"><span class="stat-label">${label}</span><span class="stat-value">${extra || p+'%'}</span></div>
        <div style="height:6px;background:var(--bg);border-radius:3px;margin-top:4px;overflow:hidden;">
          <div style="height:100%;width:${p}%;background:${color || 'var(--accent)'};border-radius:3px;transition:width 0.3s;"></div>
        </div>
      </div>`;
    };
    let html = '<div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:8px;padding:8px;">';
    // CPU (load as % of cores)
    const cpuPct = Math.min(100, Math.round(100 * data.cpu_load_avg_1 / data.cpu_cores));
    html += bar('CPU Load', data.cpu_load_avg_1, data.cpu_cores, cpuPct > 80 ? 'var(--error)' : cpuPct > 50 ? 'var(--warning)' : 'var(--success)', data.cpu_load_avg_1.toFixed(2) + ' / ' + data.cpu_cores + ' cores');
    // Memory
    html += bar('Memory', data.mem_used, data.mem_total, pct(data.mem_used,data.mem_total) > 85 ? 'var(--error)' : 'var(--accent)', fmt(data.mem_used) + ' / ' + fmt(data.mem_total));
    // Disk
    html += bar('Disk', data.disk_used, data.disk_total, pct(data.disk_used,data.disk_total) > 90 ? 'var(--error)' : 'var(--accent2)', fmt(data.disk_used) + ' / ' + fmt(data.disk_total));
    // Swap
    if (data.swap_total > 0) html += bar('Swap', data.swap_used, data.swap_total, 'var(--warning)', fmt(data.swap_used) + ' / ' + fmt(data.swap_total));
    // GPU
    if (data.gpu_name) {
      html += bar('GPU ' + escHtml(data.gpu_name), data.gpu_util_pct, 100, data.gpu_util_pct > 80 ? 'var(--error)' : 'var(--success)', data.gpu_util_pct + '% ' + data.gpu_temp + '°C');
      if (data.gpu_mem_total_mb > 0) html += bar('GPU VRAM', data.gpu_mem_used_mb, data.gpu_mem_total_mb, 'var(--accent2)', data.gpu_mem_used_mb + ' / ' + data.gpu_mem_total_mb + ' MB');
    }
    // Network — line-per-stat layout
    const netLabel = data.ebpf_active ? 'Network (datawatch)' : 'Network (system)';
    html += `<div class="stat-card"><div class="stat-label">${netLabel}</div>
      <div style="font-size:10px;font-family:monospace;color:var(--text);line-height:1.6;">
        <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">&#8595; Download</span><span>${fmt(data.net_rx_bytes || 0)}</span></div>
        <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">&#8593; Upload</span><span>${fmt(data.net_tx_bytes || 0)}</span></div>
      </div></div>`;
    // Daemon — line-per-stat layout
    const up = data.uptime_seconds || 0;
    const upStr = up > 3600 ? Math.floor(up/3600) + 'h ' + Math.floor((up%3600)/60) + 'm' : Math.floor(up/60) + 'm ' + (up%60) + 's';
    html += `<div class="stat-card"><div class="stat-label">Daemon</div>
      <div style="font-size:10px;font-family:monospace;color:var(--text);line-height:1.6;">
        <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Memory</span><span>${fmt(data.daemon_rss_bytes)} RSS</span></div>
        <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Goroutines</span><span>${data.goroutines}</span></div>
        <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">File descriptors</span><span>${data.open_fds || 0}</span></div>
        <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Uptime</span><span>${upStr}</span></div>
      </div></div>`;
    // Infrastructure
    const webProto = data.tls_enabled ? 'https' : 'http';
    const webPortStr = data.web_port ? ':' + data.web_port : ':8080';
    const tlsInfo = data.tls_enabled && data.tls_port ? ' · TLS :' + data.tls_port : '';
    html += `<div class="stat-card"><div class="stat-label">Infrastructure</div>
      <div style="font-size:10px;font-family:monospace;color:var(--text);line-height:1.6;">
        <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Web</span><span>${webProto}://${data.bound_interfaces?.[0] || '0.0.0.0'}${webPortStr}${tlsInfo}</span></div>
        ${data.mcp_sse_port ? `<div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">MCP SSE</span><span>${data.mcp_sse_host || '0.0.0.0'}:${data.mcp_sse_port}</span></div>` : ''}
        <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Tmux sessions</span><span>${data.tmux_sessions || 0}${data.orphaned_tmux?.length ? ' <span style="color:var(--warning);">(' + data.orphaned_tmux.length + ' orphan)</span>' : ''}</span></div>
      </div></div>`;
    // RTK Token Savings
    if (data.rtk_installed) {
      const savPct = data.rtk_avg_savings_pct ? data.rtk_avg_savings_pct.toFixed(1) + '%' : '—';
      const savTok = data.rtk_total_saved ? data.rtk_total_saved.toLocaleString() : '0';
      const savCmds = data.rtk_total_commands || 0;
      html += `<div class="stat-card"><div class="stat-label">RTK Token Savings</div>
        <div style="font-size:10px;font-family:monospace;color:var(--text);line-height:1.6;">
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Version</span><span>${escHtml(data.rtk_version || '?')}</span></div>
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Hooks</span><span style="color:${data.rtk_hooks_active ? 'var(--success)' : 'var(--warning)'};">${data.rtk_hooks_active ? 'active' : 'inactive'}</span></div>
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Tokens saved</span><span>${savTok}</span></div>
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Avg savings</span><span>${savPct}</span></div>
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Commands</span><span>${savCmds}</span></div>
        </div></div>`;
    }
    html += '</div>';

    // ── Session Statistics Card ──
    html += '<div style="font-size:11px;color:var(--text2);font-weight:600;padding:8px 8px 4px;border-top:1px solid var(--border);">Session Statistics</div>';
    // Mini donut: active sessions out of max concurrent
    const active = data.active_sessions || 0;
    const maxSess = state._maxSessions || 10; // loaded from config
    const activePct = Math.min(100, Math.round(100 * active / maxSess));
    html += `<div style="display:flex;align-items:center;gap:12px;padding:4px 8px;">
      <div style="width:48px;height:48px;border-radius:50%;background:conic-gradient(var(--success) 0% ${activePct}%, var(--border) ${activePct}% 100%);display:flex;align-items:center;justify-content:center;">
        <div style="width:32px;height:32px;border-radius:50%;background:var(--bg2);display:flex;align-items:center;justify-content:center;font-size:11px;font-weight:700;color:var(--text);">${active}</div>
      </div>
      <div style="font-size:11px;color:var(--text2);">
        <div><span style="color:var(--success);font-weight:600;">${active}</span> of ${maxSess} max</div>
      </div>
    </div>`;
    // Orphaned tmux section
    if (data.orphaned_tmux && data.orphaned_tmux.length > 0) {
      html += '<div style="padding:8px;border-top:1px solid var(--border);">';
      html += '<div style="font-size:11px;color:var(--warning);font-weight:600;margin-bottom:4px;">Orphaned Tmux Sessions</div>';
      html += data.orphaned_tmux.map(name => `<div style="display:flex;justify-content:space-between;align-items:center;font-size:11px;padding:2px 0;">
        <code style="color:var(--text2);">${escHtml(name)}</code>
        <span style="font-size:10px;color:var(--text2);">tmux attach -t ${escHtml(name)}</span>
      </div>`).join('');
      html += `<div style="display:flex;gap:6px;margin-top:6px;">
        <button class="btn-secondary" style="font-size:10px;" onclick="killOrphanedTmux()">Kill All Orphaned</button>
      </div></div>`;
    }
    // eBPF status notice
    if (data.ebpf_enabled && !data.ebpf_active) {
      html += `<div style="background:rgba(245,158,11,0.1);border:1px solid rgba(245,158,11,0.3);border-radius:8px;padding:8px 12px;margin:8px;font-size:11px;">
        <strong style="color:var(--warning);">eBPF Degraded</strong>
        <div style="color:var(--text2);margin-top:2px;">${escHtml(data.ebpf_message || 'eBPF enabled but not active')}</div>
      </div>`;
    } else if (data.ebpf_enabled && data.ebpf_active) {
      html += `<div style="font-size:10px;color:var(--success);padding:4px 12px;">● eBPF active — per-session network tracking</div>`;
    }

    // Per-session stats with expandable rows
    const upDaemon = data.uptime_seconds > 3600 ? Math.floor(data.uptime_seconds/3600)+'h'+Math.floor((data.uptime_seconds%3600)/60)+'m' : Math.floor(data.uptime_seconds/60)+'m';
    const allSessions = [
      { session_id: 'daemon', name: 'datawatch', backend: 'daemon', state: 'running',
        rss_bytes: data.daemon_rss_bytes, uptime: upDaemon, pane_pid: 0 },
      ...(data.session_stats || []).sort((a,b) => (a.name||a.session_id).localeCompare(b.name||b.session_id))
    ];
    if (allSessions.length > 0) {
      html += '<div style="padding:8px;border-top:1px solid var(--border);">';
      html += '<div style="font-size:11px;color:var(--text2);font-weight:600;margin-bottom:6px;">Sessions</div>';
      allSessions.forEach((s) => {
        const sid = s.session_id;
        const isDaemon = sid === 'daemon';
        const isOpen = _expandedSessions.has(sid);
        const memStr = s.rss_bytes > 1e6 ? (s.rss_bytes/1e6).toFixed(0) + ' MB' : Math.round(s.rss_bytes/1024) + ' KB';
        html += `<div style="border-bottom:1px solid var(--border);padding:4px 0;">
          <div style="display:flex;align-items:center;gap:6px;cursor:pointer;" onclick="_expandedSessions.has('${sid}')?_expandedSessions.delete('${sid}'):_expandedSessions.add('${sid}');loadStatsPanel()">
            <span style="font-size:8px;color:var(--text2);width:10px;">${isOpen ? '▼' : '▶'}</span>
            <span style="font-size:11px;font-weight:${isDaemon?'700':'500'};flex:1;">${escHtml(s.name || sid)}${!isDaemon ? ' <span style="color:var(--text2);font-weight:400;">(#' + escHtml(sid) + ')</span>' : ''}</span>
            <span class="state-badge-${s.state}" style="font-size:9px;padding:1px 5px;border-radius:4px;">${s.state}</span>
            <span style="font-size:10px;font-family:monospace;color:var(--text2);">${memStr}</span>
            <span style="font-size:10px;color:var(--text2);">${escHtml(s.uptime || '')}</span>
          </div>
          ${isOpen ? `<div style="padding:4px 0 4px 16px;font-size:10px;color:var(--text2);">
            <div>Backend: ${escHtml(s.backend)}${s.pane_pid ? ' · PID: ' + s.pane_pid : ''}</div>
            <div>Memory: ${memStr}${s.cpu_percent ? ' · CPU: ' + s.cpu_percent + '%' : ''}</div>
            ${(s.net_tx_bytes || s.net_rx_bytes) ?
              `<div>Network: ↓ ${fmt(s.net_rx_bytes||0)} ↑ ${fmt(s.net_tx_bytes||0)}</div>` :
              data.ebpf_enabled ? '<div>Network: eBPF tracking (no data yet)</div>' : '<div>Network: enable eBPF for per-session tracking</div>'}
          </div>` : ''}
        </div>`;
      });
      // Session count with link
      html += `<div style="font-size:10px;color:var(--text2);padding:4px 8px;text-align:center;">
        <a href="#" onclick="event.preventDefault();state.showHistory=true;navigate('sessions');setTimeout(renderSessionsView,100)" style="color:var(--accent2);">${data.total_sessions || 0} sessions in store</a>
      </div>`;
      html += '</div>';
    }
    // ── Communication Channels (expandable, split Chat / LLM) ──
    if (data.comm_stats && data.comm_stats.length > 0) {
      const fmtDur = (s) => s > 3600 ? (s/3600).toFixed(1) + 'h' : s > 60 ? Math.round(s/60) + 'm' : Math.round(s) + 's';
      const fmtAgo = (ts) => { if (!ts) return '—'; const d = Math.floor(Date.now()/1000 - ts); return d < 60 ? d + 's ago' : d < 3600 ? Math.floor(d/60) + 'm ago' : Math.floor(d/3600) + 'h ago'; };
      const chatChannels = data.comm_stats.filter(c => c.enabled && (c.type === 'messaging' || c.type === 'infra')).sort((a,b) => a.name.localeCompare(b.name));
      const llmChannels = data.comm_stats.filter(c => c.enabled && c.type === 'llm').sort((a,b) => a.name.localeCompare(b.name));
      const disabledChannels = data.comm_stats.filter(c => !c.enabled).sort((a,b) => a.name.localeCompare(b.name));

      const renderChanRow = (ch) => {
        const cid = ch.name;
        const isOpen = _expandedChannels.has(cid);
        return `<div style="border-bottom:1px solid var(--border);padding:4px 0;">
          <div style="display:flex;align-items:center;gap:6px;cursor:pointer;" onclick="_expandedChannels.has('${cid}')?_expandedChannels.delete('${cid}'):_expandedChannels.add('${cid}');loadStatsPanel()">
            <span style="font-size:8px;color:var(--text2);width:10px;">${isOpen ? '▼' : '▶'}</span>
            <span style="font-size:11px;flex:1;">${escHtml(ch.name)}</span>
            ${ch.type === 'llm' && ch.active_sessions ? `<span style="font-size:9px;font-weight:700;color:var(--bg2);background:var(--success);padding:1px 6px;border-radius:8px;min-width:16px;text-align:center;">${ch.active_sessions}</span>` : ''}
            ${ch.type === 'llm' ? `<span style="font-size:10px;font-family:monospace;color:var(--text2);">${ch.total_sessions || 0}</span>` : ''}
            ${ch.type === 'infra' && ch.connections ? `<span style="font-size:10px;font-family:monospace;color:var(--text2);">${ch.connections} conn</span>` : ''}
            ${ch.type === 'messaging' && (ch.msg_recv || ch.msg_sent) ? `<span style="font-size:10px;font-family:monospace;color:var(--text2);">${ch.msg_recv||0} in / ${ch.msg_sent||0} out</span>` : ''}
          </div>
          ${isOpen ? `<div style="padding:4px 0 4px 16px;font-size:10px;font-family:monospace;color:var(--text2);line-height:1.6;">` + (
            ch.type === 'llm' ? `
              <div style="display:flex;justify-content:space-between;"><span>Active sessions</span><span style="color:var(--text);">${ch.active_sessions || 0}</span></div>
              <div style="display:flex;justify-content:space-between;"><span>Total sessions</span><span style="color:var(--text);">${ch.total_sessions || 0}</span></div>
              <div style="display:flex;justify-content:space-between;"><span>Avg duration</span><span style="color:var(--text);">${ch.avg_duration_sec ? fmtDur(ch.avg_duration_sec) : '—'}</span></div>
              <div style="display:flex;justify-content:space-between;"><span>Avg prompts/session</span><span style="color:var(--text);">${ch.avg_prompts ? ch.avg_prompts.toFixed(1) : '—'}</span></div>
            ` : `
              <div style="display:flex;justify-content:space-between;"><span>Endpoint</span><span style="color:var(--text);">${escHtml(ch.endpoint || '—')}</span></div>
              ${ch.connections ? `<div style="display:flex;justify-content:space-between;"><span>Connections</span><span style="color:var(--text);">${ch.connections}</span></div>` : ''}
              <div style="display:flex;justify-content:space-between;"><span>Requests in</span><span style="color:var(--text);">${ch.msg_recv || 0}</span></div>
              <div style="display:flex;justify-content:space-between;"><span>Responses out</span><span style="color:var(--text);">${ch.msg_sent || 0}</span></div>
              <div style="display:flex;justify-content:space-between;"><span>Data in</span><span style="color:var(--text);">${fmt(ch.bytes_in || 0)}</span></div>
              <div style="display:flex;justify-content:space-between;"><span>Data out</span><span style="color:var(--text);">${fmt(ch.bytes_out || 0)}</span></div>
              ${ch.errors ? `<div style="display:flex;justify-content:space-between;"><span>Errors</span><span style="color:var(--error);">${ch.errors}</span></div>` : ''}
              ${ch.last_active ? `<div style="display:flex;justify-content:space-between;"><span>Last activity</span><span style="color:var(--text);">${fmtAgo(ch.last_active)}</span></div>` : ''}
            `
          ) + '</div>' : ''}
        </div>`;
      };

      // Chat channels section
      if (chatChannels.length > 0) {
        html += '<div style="font-size:11px;color:var(--text2);font-weight:600;padding:8px 8px 4px;border-top:1px solid var(--border);">Chat Channels</div>';
        html += '<div style="padding:0 8px;">';
        chatChannels.forEach(ch => { html += renderChanRow(ch); });
        html += '</div>';
      }
      // LLM backends section
      if (llmChannels.length > 0) {
        html += '<div style="font-size:11px;color:var(--text2);font-weight:600;padding:8px 8px 4px;border-top:1px solid var(--border);">LLM Backends</div>';
        html += '<div style="padding:0 8px;">';
        llmChannels.forEach(ch => { html += renderChanRow(ch); });
        html += '</div>';
      }
      // Disabled channels — compact summary
      if (disabledChannels.length > 0) {
        html += '<div style="padding:6px 8px 2px;font-size:10px;color:var(--text2);border-top:1px solid var(--border);">';
        html += '<span style="font-weight:600;">Inactive: </span>';
        html += disabledChannels.map(ch => escHtml(ch.name)).join(', ');
        html += '</div>';
      }
    }

    html += '<div style="text-align:center;padding:4px;font-size:10px;color:var(--text2);">● Live — updates every 5s</div>';
    el.innerHTML = html;
}

function loadDetectionFilters() {
  const el = document.getElementById('detectionFiltersList');
  if (!el) return;
  apiFetch('/api/config').then(cfg => {
    const d = cfg?.detection || {};
    const sections = [
      { key: 'prompt_patterns', label: 'Prompt Patterns', desc: 'Substrings that indicate waiting for input' },
      { key: 'completion_patterns', label: 'Completion Patterns', desc: 'Session completed markers' },
      { key: 'rate_limit_patterns', label: 'Rate Limit Patterns', desc: 'Rate limit hit markers' },
      { key: 'input_needed_patterns', label: 'Input Needed', desc: 'Explicit input-needed protocol markers' },
    ];
    // Built-in defaults for display when config is empty
    const builtinDefaults = {
      prompt_patterns: ['? ', '> ', '$ ', '# ', '[y/N]', '[Y/n]', 'Do you want to', 'Allow ', 'Trust ', '(y/n)', 'Would you like', 'Proceed?', 'Enter to confirm', '❯', 'Ask anything', '>>> '],
      completion_patterns: ['DATAWATCH_COMPLETE:'],
      rate_limit_patterns: ['DATAWATCH_RATE_LIMITED:', "You've hit your limit", 'rate limit exceeded', 'quota exceeded'],
      input_needed_patterns: ['DATAWATCH_NEEDS_INPUT:'],
    };
    let html = '<div style="font-size:10px;color:var(--text2);padding:4px 12px;">Global patterns applied to all backends without structured channels.</div>';
    for (const s of sections) {
      const patterns = d[s.key] || [];
      const defaults = builtinDefaults[s.key] || [];
      const isUsingDefaults = patterns.length === 0;
      const displayPatterns = isUsingDefaults ? defaults : patterns;
      const id = 'det_' + s.key;
      const items = displayPatterns.map((p, i) =>
        `<div class="det-item" style="display:flex;align-items:center;gap:4px;padding:2px 0;">
          <span style="flex:1;font-size:10px;font-family:monospace;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;${isUsingDefaults ? 'opacity:0.5;' : ''}" title="${escHtml(p)}">${escHtml(p)}</span>
          ${!isUsingDefaults ? `<button class="btn-icon" style="font-size:9px;color:var(--error);padding:1px 3px;" onclick="removeDetPattern('${s.key}',${i})">&#10005;</button>` : ''}
        </div>`
      ).join('');
      html += `<div style="padding:6px 12px;border-bottom:1px solid var(--border);">
        <div style="display:flex;justify-content:space-between;align-items:center;">
          <div style="font-size:11px;color:var(--text2);font-weight:600;">${s.label}</div>
          <span style="font-size:9px;color:var(--text2);">${isUsingDefaults ? defaults.length + ' defaults' : patterns.length + ' custom'}</span>
        </div>
        <div id="${id}" style="max-height:120px;overflow-y:auto;margin:4px 0;">${items}</div>
        <div style="display:flex;gap:4px;margin-top:4px;">
          <input type="text" class="form-input" id="${id}_add" placeholder="Add pattern..." style="flex:1;font-size:10px;padding:2px 6px;" />
          <button class="btn-secondary" style="font-size:10px;padding:2px 8px;" onclick="addDetPattern('${s.key}')">Add</button>
        </div>
      </div>`;
    }
    el.innerHTML = html;
  }).catch(() => { el.innerHTML = '<div style="color:var(--error);font-size:12px;padding:8px;">Failed to load.</div>'; });
}

function addDetPattern(key) {
  const input = document.getElementById('det_' + key + '_add');
  if (!input || !input.value.trim()) return;
  apiFetch('/api/config').then(cfg => {
    const patterns = (cfg?.detection?.[key] || []).slice();
    patterns.push(input.value.trim());
    return apiFetch('/api/config', { method: 'PUT', body: JSON.stringify({ ['detection.' + key]: patterns }) });
  }).then(() => { showToast('Pattern added', 'success', 1500); loadDetectionFilters(); })
    .catch(err => showToast('Failed: ' + err.message, 'error'));
}

function removeDetPattern(key, index) {
  apiFetch('/api/config').then(cfg => {
    const patterns = (cfg?.detection?.[key] || []).slice();
    patterns.splice(index, 1);
    return apiFetch('/api/config', { method: 'PUT', body: JSON.stringify({ ['detection.' + key]: patterns }) });
  }).then(() => { showToast('Pattern removed', 'success', 1500); loadDetectionFilters(); })
    .catch(err => showToast('Failed: ' + err.message, 'error'));
}

function loadSchedulesList() {
  const el = document.getElementById('schedulesList');
  if (!el) return;
  apiFetch('/api/schedules').then(items => {
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
    const hasMultiple = pageItems.length > 1;
    let html = hasMultiple ? `<div style="display:flex;justify-content:space-between;align-items:center;padding:4px 8px;border-bottom:1px solid var(--border);">
      <label style="font-size:10px;display:flex;align-items:center;gap:4px;color:var(--text2);"><input type="checkbox" id="schedSelectAll" onchange="toggleAllScheduleCheckboxes(this.checked)"> Select all</label>
      <button class="btn-secondary" style="font-size:10px;padding:2px 8px;" onclick="deleteSelectedSchedules()">Delete selected</button>
    </div>` : '';
    html += pageItems.map(sc => {
      const when = sc.run_at ? new Date(sc.run_at).toLocaleString() : 'on input';
      const stateClass = sc.state === 'pending' ? 'color:var(--warning)' : sc.state === 'done' ? 'color:var(--success)' : 'color:var(--text2)';
      const label = sc.type === 'new_session' && sc.deferred_session
        ? 'NEW: ' + escHtml(sc.deferred_session.name || sc.command)
        : escHtml(sc.session_id) + ': ' + escHtml(sc.command);
      const actions = [];
      if (sc.state === 'pending') {
        actions.push(`<button class="btn-icon" style="font-size:10px;" onclick="editSchedulePrompt('${sc.id}','${escHtml(sc.command).replace(/'/g,"\\'")}','${sc.run_at||''}')" title="Edit">&#9998;</button>`);
      }
      actions.push(`<button class="btn-icon" style="font-size:10px;color:var(--error);" onclick="deleteScheduleEntry('${sc.id}')" title="Delete">&#128465;</button>`);
      return `<div class="settings-row" style="justify-content:space-between;font-size:12px;">
        ${hasMultiple ? `<input type="checkbox" class="sched-checkbox" data-id="${sc.id}" style="margin-right:6px;">` : ''}
        <div style="flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;" title="${escHtml(sc.command)}">${label}</div>
        <div style="display:flex;align-items:center;gap:6px;">
          <span style="font-size:10px;color:var(--text2);">${when}</span>
          <span style="font-size:10px;${stateClass};font-weight:600;text-transform:uppercase;">${escHtml(sc.state)}</span>
          ${actions.join('')}
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

function editSchedulePrompt(id, currentCmd, currentRunAt) {
  const newCmd = prompt('Edit command:', currentCmd);
  if (newCmd === null) return; // cancelled
  const newTime = prompt('New time (ISO, or empty to keep):', currentRunAt || '');
  const body = { id, command: newCmd || currentCmd };
  if (newTime) body.run_at = newTime;
  apiFetch('/api/schedules', {
    method: 'PUT',
    body: JSON.stringify(body),
  }).then(() => { showToast('Schedule updated', 'success', 1500); loadSchedulesList(); })
    .catch(e => showToast('Update failed: ' + e.message, 'error'));
}

function deleteScheduleEntry(id) {
  apiFetch('/api/schedules?id=' + encodeURIComponent(id), { method: 'DELETE' })
    .then(() => { showToast('Deleted', 'success', 1500); loadSchedulesList(); })
    .catch(e => showToast('Delete failed: ' + e.message, 'error'));
}

function toggleAllScheduleCheckboxes(checked) {
  document.querySelectorAll('.sched-checkbox').forEach(cb => cb.checked = checked);
}

function deleteSelectedSchedules() {
  const ids = Array.from(document.querySelectorAll('.sched-checkbox:checked')).map(cb => cb.dataset.id);
  if (ids.length === 0) { showToast('No items selected', 'warning'); return; }
  showConfirmModal(`Delete ${ids.length} scheduled event(s)?`, () => {
    Promise.all(ids.map(id => apiFetch('/api/schedules?id=' + encodeURIComponent(id), { method: 'DELETE' })))
      .then(() => { showToast(`Deleted ${ids.length} events`, 'success', 1500); loadSchedulesList(); })
      .catch(e => showToast('Delete failed: ' + e.message, 'error'));
  });
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
  // Restore saved view or default to sessions
  const _initView = localStorage.getItem('cs_active_view');
  const _initSession = localStorage.getItem('cs_active_session');
  navigate(_initView || 'sessions', _initSession || undefined);

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
window.showStateOverride = showStateOverride;
window.setSessionState = setSessionState;
window.changeTermFontSize = changeTermFontSize;
window.termFitToWidth = termFitToWidth;
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
window.loadDetectionFilters = loadDetectionFilters;
window.addDetPattern = addDetPattern;
window.removeDetPattern = removeDetPattern;
window.loadStatsPanel = loadStatsPanel;
window.killOrphanedTmux = killOrphanedTmux;

function killOrphanedTmux() {
  showConfirmModal('Kill all orphaned tmux sessions?', () => {
    apiFetch('/api/stats').then(data => {
      if (!data.orphaned_tmux) return;
      const kills = data.orphaned_tmux.map(name =>
        fetch('/api/command', { method: 'POST', headers: { 'Content-Type': 'application/json', ...tokenHeader() },
          body: JSON.stringify({ text: 'tmux-kill:' + name }) })
      );
      // Use direct tmux kill via a simple API call
      apiFetch('/api/stats/kill-orphans', { method: 'POST' })
        .then(() => { showToast('Orphaned sessions killed', 'success', 2000); loadStatsPanel(); })
        .catch(() => showToast('Failed to kill orphans', 'error'));
    });
  });
}
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
