window._splashStart = Date.now();

// ── i18n (BL214 / v5.28.0) ────────────────────────────────────────────────
// Lightweight zero-dep translation layer. Locale JSON files are served from
// /locales/{en,de,es,fr,ja}.json (see internal/server/web/locales). Strings
// originate from the Compose-Multiplatform Android client (datawatch-app
// composeApp/src/androidMain/res/values{,-de,-es,-fr,-ja}/strings.xml) so
// the PWA stays in lockstep with the mobile companion's vetted translations.
//
// Coverage is iterative — v5.28.0 wires the most-visible surfaces (bottom
// nav, common actions, settings tabs); subsequent v5.28.x patches extend
// `t()` calls into the rest of app.js as the Android string set grows.
window._i18n = {
  supported: ['en', 'de', 'es', 'fr', 'ja'],
  current: 'en',
  override: null,           // localStorage-persisted manual selection
  bundle: {},               // { key: translated string }
  fallback: {},             // English bundle for fallback when a key is missing
  ready: false,
};
function detectLocale() {
  const stored = localStorage.getItem('datawatch.locale') || '';
  if (stored && stored !== 'auto' && window._i18n.supported.includes(stored)) {
    window._i18n.override = stored;
    return stored;
  }
  // navigator.language is "en-US" / "de" / "ja-JP" — strip region.
  const browser = (navigator.language || 'en').toLowerCase().split('-')[0];
  if (window._i18n.supported.includes(browser)) return browser;
  return 'en';
}
async function loadLocale(lang) {
  try {
    const res = await fetch('/locales/' + lang + '.json', { cache: 'reload' });
    if (!res.ok) throw new Error('HTTP ' + res.status);
    return await res.json();
  } catch (e) {
    _dbg('I18N', 'failed to load ' + lang + ': ' + e.message);
    return null;
  }
}
async function initI18n() {
  // Always load EN as the fallback bundle; load active locale if non-EN.
  const en = await loadLocale('en') || {};
  window._i18n.fallback = en;
  const lang = detectLocale();
  window._i18n.current = lang;
  if (lang === 'en') {
    window._i18n.bundle = en;
  } else {
    const b = await loadLocale(lang);
    window._i18n.bundle = b || en;
  }
  window._i18n.ready = true;
  applyI18nDOM(document);
}
// applyI18nDOM walks elements with `data-i18n="<key>"` and replaces their
// textContent with t(key). Variants:
//   data-i18n-attr="title"   → updates the named attribute instead of text
//   data-i18n-html           → uses innerHTML (caller's responsibility to keep
//                              the locale bundle XSS-safe; only use for our own keys)
function applyI18nDOM(root) {
  if (!root) return;
  const els = root.querySelectorAll('[data-i18n]');
  els.forEach(el => {
    const key = el.getAttribute('data-i18n');
    if (!key) return;
    const val = t(key);
    const attr = el.getAttribute('data-i18n-attr');
    if (attr) {
      el.setAttribute(attr, val);
    } else if (el.hasAttribute('data-i18n-html')) {
      el.innerHTML = val;
    } else {
      el.textContent = val;
    }
  });
}
window.applyI18nDOM = applyI18nDOM;
function t(key, vars) {
  const b = window._i18n.bundle || {};
  let s = b[key];
  if (s === undefined) s = window._i18n.fallback[key];
  if (s === undefined) return key; // last-resort: show the key so misses are visible
  if (vars) {
    // Android-style %1$s / %1$d / %2$s placeholders → vars[0..n].
    s = s.replace(/%(\d+)\$[sd]/g, (_, n) => {
      const i = parseInt(n, 10) - 1;
      return (vars[i] !== undefined) ? String(vars[i]) : '';
    });
  }
  return s;
}
window.t = t; // expose for inline-onclick handlers + dev console
// Setting the locale override persists it and reloads the page so every
// rendered surface picks up the new bundle in one go.
//
// v5.28.3 — operator-asked: PWA UI language is the default app language;
// whisper transcription language follows it unless the operator has
// explicitly chosen a different code in the Whisper card. So when the
// picker chooses a concrete locale (en/de/es/fr/ja), we also push it to
// `whisper.language` via PUT /api/config. Picking 'Auto' (browser-detect)
// leaves whisper.language alone — that path is for "follow the browser"
// not "reset everything", and clobbering the whisper config from there
// would surprise an operator who set it independently.
async function setLocaleOverride(lang) {
  if (!lang || lang === 'auto') {
    localStorage.removeItem('datawatch.locale');
  } else if (window._i18n.supported.includes(lang)) {
    localStorage.setItem('datawatch.locale', lang);
    // Best-effort: also sync whisper transcription language. Failure is
    // non-fatal — the UI bundle change should still apply on reload even
    // if the daemon's PUT /api/config rejects (e.g. token mismatch on a
    // proxied PWA). Don't block the page reload on this.
    try {
      await fetch('/api/config', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', ...(typeof tokenHeader === 'function' ? tokenHeader() : {}) },
        body: JSON.stringify({ 'whisper.language': lang }),
      });
    } catch (e) {
      _dbg('I18N', 'whisper.language sync failed: ' + (e && e.message));
    }
  } else {
    return;
  }
  location.reload();
}
window.setLocaleOverride = setLocaleOverride;

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
  chatMessages: {},       // sessionId -> [{role, content, ts}] for chat-mode sessions
  chatStreaming: {},       // sessionId -> string (currently streaming assistant content)
  channelReady: {},       // sessionId -> bool (true once channel/ACP connection confirmed)
  // v4.0.2 — per-session dismiss flag for the "Input Required" yellow
  // banner. Set when: operator clicks the X, or operator sends input
  // (the banner is about to become stale anyway). Cleared when the
  // session transitions out of waiting_input AND back in, so a fresh
  // prompt round re-shows the banner even if a previous round was
  // dismissed.
  needsInputDismissed: {}, // sessionId -> bool
  needsInputLastShown: {}, // sessionId -> last prompt signature shown
  notifPermission: Notification.permission,
  sessionOrder: JSON.parse(localStorage.getItem('cs_session_order') || '[]'), // manual ordering
  servers: [],            // remote server list from /api/servers
  activeServer: null,     // selected server name (null = local)
  alertUnread: 0,         // unread alert count for badge
  alertSystemUnread: 0,   // BL226 — system-sourced unread count
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
  // Route through proxy when a remote server is selected
  const srv = state.activeServer;
  const wsPath = (srv && srv !== 'local') ? '/api/proxy/' + encodeURIComponent(srv) + '/ws' : '/ws';
  return `${proto}//${location.host}${wsPath}${q}`;
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
    // v4.0.6 — dismiss the self-update overlay once the daemon is
    // reachable again. We give it a moment so the "installed /
    // restarting" message is visible before the panel vanishes.
    const upd = document.getElementById('updateProgressOverlay');
    if (upd) setTimeout(() => upd.remove(), 1500);
    // Update comms server status indicator if visible
    const connInd = document.querySelector('.connection-indicator');
    if (connInd) {
      connInd.querySelector('.dot')?.classList.add('connected');
      const span = connInd.querySelector('span');
      if (span) span.textContent = 'Connected';
    }
    // Dismiss splash screen — only show once per 24h unless version changed
    const splash = document.getElementById('splash');
    if (splash) {
      const lastSplashTime = parseInt(localStorage.getItem('cs_splash_time') || '0', 10);
      const lastSplashVer = localStorage.getItem('cs_splash_version') || '';
      const serverVer = state._daemonVersion || '';
      const hoursSince = (Date.now() - lastSplashTime) / (1000 * 60 * 60);
      const isNewVersion = serverVer && lastSplashVer && serverVer !== lastSplashVer;

      if (isNewVersion) {
        // Show "Updated" badge on splash
        const badge = document.createElement('div');
        badge.style.cssText = 'position:absolute;top:8px;right:8px;background:var(--accent);color:#fff;font-size:10px;padding:2px 8px;border-radius:8px;font-weight:600;';
        badge.textContent = 'Updated to ' + serverVer;
        splash.style.position = 'relative';
        splash.appendChild(badge);
      }

      // Show splash if: first visit, new version, or >24h since last display
      const shouldShow = !lastSplashTime || isNewVersion || hoursSince >= 24;

      if (shouldShow) {
        localStorage.setItem('cs_splash_time', String(Date.now()));
        if (serverVer) localStorage.setItem('cs_splash_version', serverVer);
        const elapsed = Date.now() - (window._splashStart || 0);
        const remaining = Math.max(0, 3000 - elapsed);
        setTimeout(() => {
          splash.classList.add('fade-out');
          setTimeout(() => splash.remove(), 700);
        }, remaining);
      } else {
        // Skip splash — remove immediately
        splash.remove();
      }
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
      // v5.26.35 — operator-reported: "when service restarts, if i'm
      // in a session and it refreshes the tmux bar goes away and the
      // screen format is messed up, i have to exit the session and
      // go back in to reset". Cause: we were calling
      // renderSessionDetail() unconditionally on reconnect, which
      // rebuilds the toolbar HTML + detaches the xterm.js mount even
      // when the existing DOM was healthy. New rule: if we already
      // have a working terminal for this session, just re-subscribe
      // to the pane stream + force the next pane_capture frame to
      // redraw cleanly. Full re-render only when the terminal isn't
      // actually alive (first visit, navigation in from another
      // view, output_mode switch, etc.).
      const sid = state.activeSession;
      const sameSessionTermAlive = (
        state.terminal &&
        state._termSessionId === sid &&
        state._termHasContent
      );
      if (sameSessionTermAlive) {
        // Re-subscribe so the daemon pushes the next pane_capture
        // frame to us; xterm.js stays mounted, toolbar stays intact.
        send('subscribe', { session_id: sid });
        // Mark for a single fresh redraw on the next frame so any
        // dropped output during the disconnect catches up.
        state._pendingPaneCaptureRefresh = true;
        // v5.26.45 — operator-reported repeat: "when datawatch
        // daemon restarts and i'm in a session the screen gets
        // messed up, loses tmux chat and i have to exit and
        // reenter session to get view back again". v5.26.35 kept
        // the DOM healthy but missed this: while disconnected,
        // tmux on the daemon side is frozen at whatever size it
        // had at the moment the daemon stopped. The browser may
        // have resized in the meantime, OR tmux may have been re-
        // attached at a different default size after the daemon
        // restart. Either way the cached cols/rows on both sides
        // can drift. Force a resize_term immediately on reconnect
        // with the live xterm dimensions; tmux reshapes the pane,
        // pane_capture comes back at the right width, the next
        // frame redraws cleanly.
        const t = state.terminal;
        if (t && t.cols && t.rows) {
          send('resize_term', { session_id: sid, cols: t.cols, rows: t.rows });
        }
      } else {
        renderSessionDetail(sid);
      }
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
  // Route through proxy when a remote server is selected
  const srv = state.activeServer;
  const url = (srv && srv !== 'local') ? '/api/proxy/' + encodeURIComponent(srv) + path : path;
  return fetch(url, Object.assign({}, opts, { headers }))
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
          // Skip raw output for chat-mode sessions — only chat_message events render
          if (document.getElementById('chatArea')) break;
          // Check if this session uses log mode (no xterm)
          const logArea = document.querySelector('.log-viewer-mode');
          if (logArea && !state.terminal) {
            // v5.24.0 — capture scroll state BEFORE appending so we
            // can preserve scroll-back position. Pre-fix: every
            // raw_output frame yanked the operator back to the bottom.
            const wasAtBottom = logArea.scrollHeight - logArea.scrollTop <= logArea.clientHeight + 40;
            // Log mode — append formatted lines
            for (const chunk of rawLines) {
              const text = stripAnsi(chunk).trim();
              if (!text) continue;
              const div = document.createElement('div');
              div.className = 'log-line' + (text.includes('[opencode-acp]') ? ' log-acp-status' : '');
              div.textContent = text;
              logArea.appendChild(div);
            }
            if (wasAtBottom) {
              logArea.scrollTop = logArea.scrollHeight;
            }
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
      // Buffer if terminal not ready yet (subscribe fires before initXterm)
      if (msg.data && !state.terminal && state.activeView === 'session-detail' && state.activeSession === msg.data.session_id) {
        state._pendingPaneCapture = msg.data;
        break;
      }
      if (msg.data && state.terminal && state.activeView === 'session-detail' && state.activeSession === msg.data.session_id) {
        // Throttle: max ~30fps to prevent xterm.js buffer overload
        const now = performance.now();
        if (state._lastPaneWrite && (now - state._lastPaneWrite) < 33) break; // skip frame
        state._lastPaneWrite = now;
        // Freeze terminal display once session is complete/failed/killed —
        // prevents showing the shell prompt that appears after the LLM exits
        // but before the tmux session is cleaned up.
        const capSess = state.sessions.find(s => s.full_id === msg.data.session_id);
        const capState = capSess ? capSess.state : '';
        if (capState === 'complete' || capState === 'failed' || capState === 'killed') break;
        const capLines = msg.data.lines || [];
        // Skip frames that contain the completion marker — this is the
        // transitional frame where the echo fires before the backend updates
        // session state. Displaying it would briefly flash the shell prompt.
        if (capLines.some(l => l.includes('DATAWATCH_COMPLETE:'))) break;
        if (capLines.length > 0) {
          try {
            if (!state.terminal) break; // guard: terminal may have been destroyed
            if (!state._termHasContent || state._pendingPaneCaptureRefresh) {
              // First frame OR a post-reconnect forced refresh —
              // dismiss loading splash, reset for clean state. The
              // refresh path heals any drift accumulated during a
              // daemon-restart / WS-disconnect window without
              // tearing down the toolbar (v5.26.35).
              const splash = document.getElementById('termLoadingSplash');
              if (splash) splash.remove();
              if (state._termWatchdog) { clearTimeout(state._termWatchdog); state._termWatchdog = null; }
              state.terminal.reset();
              state.terminal.write(capLines.join('\r\n'));
              state._termHasContent = true;
              state._pendingPaneCaptureRefresh = false;
            } else {
              // v5.24.0 — when scrolled up in xterm to read earlier
              // output, every pane_capture redraw was yanking back to
              // the bottom (the redraw clears scrollback via \x1b[3J).
              // Detect scroll-back via xterm's buffer and skip until
              // operator returns to bottom.
              //
              // v5.26.14 — operator-reported (third iteration):
              // "scroll mode still getting live updates from running
              // session". v5.26.9 skipped redraws entirely in scroll
              // mode (broke scrolling). v5.26.10 added content-aware
              // dedupe (broke for claude-style TUIs whose status timer
              // updates every second, defeating the dedupe and
              // bleeding live updates into the scroll view). v5.26.14:
              // skip redraws while in scroll mode UNLESS
              // state._scrollPendingRefresh is true — the PageUp /
              // PageDown buttons set the flag right before sending
              // the scroll keystroke so exactly ONE redraw fires per
              // operator scroll action, picking up the new tmux
              // scroll position. Idle ticks (status timer, live
              // output etc.) skip silently.
              const buf = state.terminal.buffer && state.terminal.buffer.active;
              if (buf) {
                const atBottom = buf.viewportY >= buf.baseY;
                if (!atBottom) break; // skip redraw; preserve xterm scroll position
              }
              if (state._scrollMode && !state._scrollPendingRefresh) break;
              state._scrollPendingRefresh = false;
              const frameKey = capLines.join('\n');
              if (frameKey === state._lastPaneFrame) break; // identical frame; skip flash
              state._lastPaneFrame = frameKey;
              // Subsequent frames — clear screen + clear scrollback + home + redraw
              // \x1b[3J clears the scrollback buffer so repeated captures don't
              // accumulate duplicate content and cause scroll/display issues.
              state.terminal.write('\x1b[2J\x1b[3J\x1b[H' + capLines.join('\r\n'));
            }
          } catch (e) {
            console.error('[xterm] write failed, recovering:', e);
            // Terminal in bad state — destroy and let next navigation recreate
            destroyXterm();
          }
        }
      }
      break;
    case 'chat_message':
      if (msg.data) {
        // BL184: chat-mode opencode-acp can emit "ready"-flavoured
        // status messages through the chat channel before any output
        // line lands. Run the detection here too so the operator
        // doesn't have to back out + re-enter to see the transition.
        if (msg.data.session_id && msg.data.content) {
          markChannelReadyIfDetected(msg.data.session_id, [msg.data.content]);
        }
        handleChatMessage(msg.data);
      }
      break;
    case 'response':
      if (msg.data && msg.data.session_id) {
        state.lastResponse = state.lastResponse || {};
        state.lastResponse[msg.data.session_id] = msg.data.response;
        // Bound the cache so a long-lived browser tab handling many
        // sessions doesn't grow this map forever (BL291 leak audit).
        const keys = Object.keys(state.lastResponse);
        if (keys.length > 128) {
          // Drop the oldest 16 entries (insertion order ≈ session
          // creation order). Cheap, only fires once per overflow.
          for (let i = 0; i < 16; i++) delete state.lastResponse[keys[i]];
        }
      }
      break;
    case 'session_aware':
      if (msg.data && msg.data.summary) {
        // Show session awareness notification (don't suppress if viewing that session)
        showToast('Session update: ' + msg.data.summary.slice(0, 100), 'info', 5000);
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
    case 'update_progress':
      // v4.0.6 — self-update download progress bar. msg.data:
      // { version, phase, downloaded, total, error? }
      if (msg.data) handleUpdateProgress(msg.data);
      break;
    case 'prd_update':
      // v5.24.0 — operator-reported: Autonomous tab should auto-refresh
      // on PRD changes (create / decompose / approve / reject / run /
      // cancel / edit / delete). Daemon-side broadcasts MsgPRDUpdate
      // on every Manager.SavePRD via internal/autonomous.PRDUpdateFn.
      // Cheap reload here — the Autonomous tab panel re-fetches
      // /api/autonomous/prds and re-renders. Throttle so a Run that
      // mutates dozens of tasks per second doesn't reload the panel
      // dozens of times.
      if (state.activeView === 'autonomous') {
        clearTimeout(state._prdReloadTimer);
        state._prdReloadTimer = setTimeout(() => {
          if (typeof loadPRDPanel === 'function') loadPRDPanel();
        }, 250);
      }
      break;
  }
}

function handleAlert(a) {
  state.alertUnread++;
  if (a.source === 'system' || !a.session_id) state.alertSystemUnread++;
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

// v4.0.2 — dismiss the yellow "Input Required" banner. Keyed per
// session; re-appears automatically on the next distinct prompt.
//
// v5.26.44 — operator-reported: closing the yellow banner left the
// xterm.js viewport sized as if the banner were still present, and
// the terminal showed wrong dimensions until the operator navigated
// away and back. ResizeObserver on the container DID fire, but only
// after the 200ms debounce — long enough for the operator to notice
// the busted layout. Force an immediate fit + tmux resize sync after
// the next animation frame (DOM flushed).
function dismissNeedsInputBanner(sessionId) {
  state.needsInputDismissed[sessionId] = true;
  refreshNeedsInputBanner(sessionId);
  if (state.activeView === 'session-detail' &&
      state.activeSession === sessionId &&
      state.termFitAddon) {
    requestAnimationFrame(() => {
      try { state.termFitAddon.fit(); } catch(e) {}
      const t = state.terminal;
      if (t && t.cols && t.rows) {
        send('resize_term', { session_id: sessionId, cols: t.cols, rows: t.rows });
      }
    });
  }
}

// Build the inner HTML for the Input Required banner from the current
// session record. Returns '' when the banner shouldn't show. Called
// both from full renderSessionDetail and from refreshNeedsInputBanner
// (which patches the existing slot in place without re-rendering the
// whole session view).
function buildNeedsInputBannerHTML(sess, sessionId) {
  if (!sess) return '';
  const isWaiting = sess.state === 'waiting_input';
  if (!isWaiting) return '';
  if (state.needsInputDismissed[sessionId]) return '';
  if (!sess.prompt_context && !sess.last_prompt) return '';
  const ctxLines = sess.prompt_context
    ? sess.prompt_context.split('\n').map(l => stripAnsi(l).trim()).filter(l => l.length > 0)
    : [stripAnsi(sess.last_prompt).trim()];
  const trustPrompt = ctxLines.some(l => /local development|approved channels|trust this folder/i.test(l));
  const tip = trustPrompt
    ? '<div class="needs-input-tip">Tip: press <kbd>1</kbd> then <kbd>Enter</kbd> to accept.</div>'
    : '';
  const html = ctxLines.slice(-6).map(l => `<div>${escHtml(l)}</div>`).join('');
  return `<div class="needs-input-banner">
    <span class="needs-input-badge">Input Required</span>
    <div class="needs-input-body">${html}${tip}</div>
    <button class="btn-icon needs-input-dismiss" title="Dismiss (shows again next time the session waits for input)" onclick="dismissNeedsInputBanner('${escHtml(sessionId)}')">&#10005;</button>
  </div>`;
}

// v5.27.7 (BL208 / datawatch#26) — toggle the 3-dot generating
// indicator below the terminal output. Visible only while the session
// is in `running`; removed on every other state. Called from
// updateSession + renderSessionDetail so the dots appear/disappear in
// sync with the live state. CSS handles the actual fade animation.
function refreshGeneratingIndicator(sessionId) {
  if (state.activeView !== 'session-detail' || state.activeSession !== sessionId) return;
  const slot = document.getElementById('generatingSlot');
  if (!slot) return;
  const sess = state.sessions.find(s => s.full_id === sessionId);
  const isRunning = sess && sess.state === 'running';
  if (!isRunning) {
    if (slot.firstChild) {
      slot.innerHTML = '';
      // BL227 — the generating indicator occupies vertical space; clearing it
      // frees height for xterm but the terminal doesn't know until fit() fires.
      // Mirror the same rAF pattern used by dismissNeedsInputBanner (v5.26.44).
      if (state.termFitAddon) {
        requestAnimationFrame(() => {
          try { state.termFitAddon.fit(); } catch(e) {}
          const t = state.terminal;
          if (t && t.cols && t.rows) {
            send('resize_term', { session_id: sessionId, cols: t.cols, rows: t.rows });
          }
        });
      }
    }
    return;
  }
  // Idempotent: only inject once per running episode.
  if (slot.firstChild) return;
  slot.innerHTML = `<div class="generating-indicator" title="Session is generating">
    <span class="dw-dot"></span><span class="dw-dot"></span><span class="dw-dot"></span>
    <span style="opacity:0.6;">generating…</span>
  </div>`;
}
window.refreshGeneratingIndicator = refreshGeneratingIndicator;

// Patch the #needsInputSlot in place. Called from updateSession when
// the session-detail view is open and the active session changes
// state — this is what fixes the bug where the popup didn't appear
// while the operator was already inside the session.
function refreshNeedsInputBanner(sessionId) {
  if (state.activeView !== 'session-detail' || state.activeSession !== sessionId) return;
  const slot = document.getElementById('needsInputSlot');
  if (!slot) return;
  const sess = state.sessions.find(s => s.full_id === sessionId);
  // Reset dismissed flag on transition out of waiting_input so the
  // next prompt shows again — same logic that lived inline in
  // renderSessionDetail before the extract.
  if (sess && sess.state !== 'waiting_input' && state.needsInputDismissed[sessionId]) {
    state.needsInputDismissed[sessionId] = false;
  }
  // v5.27.1 — operator-reported: "after a prompt finishes and i submit
  // a new one, it refreshes the page, screen size refreshes wrong size,
  // tmux input goes away and i have to exit and reenter". Cause: the
  // state-driven banner refresh path patched the slot innerHTML but
  // didn't trigger an xterm fit() + resize_term sync the way the
  // explicit Dismiss-button path does (v5.26.44 fix). When the banner
  // toggled in or out, the container height changed but the terminal
  // stayed at the old dimensions until ResizeObserver's 200ms debounce
  // caught up — long enough for the operator to see a busted layout.
  // Compare current vs new banner HTML; on any change, force the same
  // immediate fit + tmux size sync that dismissNeedsInputBanner does.
  const before = slot.innerHTML;
  const next = buildNeedsInputBannerHTML(sess, sessionId);
  if (before === next) return;
  slot.innerHTML = next;
  if (state.termFitAddon) {
    requestAnimationFrame(() => {
      try { state.termFitAddon.fit(); } catch(e) {}
      const t = state.terminal;
      if (t && t.cols && t.rows) {
        send('resize_term', { session_id: sessionId, cols: t.cols, rows: t.rows });
      }
    });
  }
  // Belt-and-suspenders: rebind the Enter handler in case the input
  // element was reattached during a state transition. A duplicate
  // listener is harmless; a missing one means the operator has to exit
  // and re-enter the session to recover.
  const inputEl = document.getElementById('sessionInput');
  if (inputEl && !inputEl._dwEnterBound) {
    inputEl._dwEnterBound = true;
    inputEl.addEventListener('keydown', e => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        sendSessionInput();
      }
    });
  }
}

// v4.0.6 — self-update download progress overlay. Renders a fixed
// bottom-of-viewport card with a real progress bar while the daemon
// pulls the release asset, then flips to "Installed / Restarting…"
// before the WebSocket drops.
function handleUpdateProgress(data) {
  let el = document.getElementById('updateProgressOverlay');
  if (!el) {
    el = document.createElement('div');
    el.id = 'updateProgressOverlay';
    el.className = 'update-progress-overlay';
    el.innerHTML = `
      <div class="upd-head">
        <span class="upd-title">Updating to <span id="updVersion">…</span></span>
        <span class="upd-phase" id="updPhase">starting</span>
      </div>
      <div class="upd-bar-track"><div class="upd-bar-fill" id="updBarFill"></div></div>
      <div class="upd-meta" id="updMeta"></div>
    `;
    document.body.appendChild(el);
  }
  const version = data.version || '';
  const phase = data.phase || 'downloading';
  const downloaded = Number(data.downloaded || 0);
  const total = Number(data.total || 0);

  document.getElementById('updVersion').textContent = 'v' + version;
  const phaseEl = document.getElementById('updPhase');
  const fillEl = document.getElementById('updBarFill');
  const metaEl = document.getElementById('updMeta');

  phaseEl.textContent = phase;
  phaseEl.className = 'upd-phase upd-phase-' + phase;

  if (phase === 'failed') {
    fillEl.style.width = '100%';
    fillEl.classList.add('upd-fill-error');
    metaEl.textContent = data.error || 'update failed';
    // Leave overlay so the operator can read the error; auto-close after 15s.
    setTimeout(() => { if (el) el.remove(); }, 15000);
    return;
  }
  if (phase === 'installed') {
    fillEl.style.width = '100%';
    fillEl.classList.add('upd-fill-done');
    metaEl.textContent = 'Installed. Restarting…';
    return;
  }
  if (phase === 'restarting') {
    fillEl.style.width = '100%';
    fillEl.classList.add('upd-fill-done');
    metaEl.textContent = 'Daemon restarting — reconnect will happen automatically.';
    return;
  }
  // downloading / starting
  if (total > 0) {
    const pct = Math.min(100, Math.max(0, Math.round(downloaded * 100 / total)));
    fillEl.style.width = pct + '%';
    metaEl.textContent = `${pct}% — ${fmtBytes(downloaded)} of ${fmtBytes(total)}`;
  } else {
    // Unknown total (no Content-Length): indeterminate style.
    fillEl.style.width = '35%';
    fillEl.classList.add('upd-fill-indeterminate');
    metaEl.textContent = `${fmtBytes(downloaded)} downloaded…`;
  }
}

function fmtBytes(n) {
  if (n < 1024) return n + ' B';
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB';
  return (n / 1024 / 1024).toFixed(1) + ' MB';
}

function handleChannelReadyEvent(sessionId) {
  state.channelReady[sessionId] = true;
  // If viewing this session, dismiss the connection banner and enable input
  // without a full re-render (which would reset the terminal and cause scroll glitches)
  if (state.activeView === 'session-detail' && state.activeSession === sessionId) {
    const banner = document.getElementById('connBanner');
    if (banner) banner.remove();
    const inputBar = document.getElementById('inputBar');
    if (inputBar) inputBar.classList.remove('input-disabled');
    const inputField = document.getElementById('sessionInput');
    if (inputField) { inputField.disabled = false; inputField.placeholder = 'Send message…'; }
  }
}

function handleChatMessage(data) {
  const { session_id, role, content, streaming } = data;
  if (!session_id) return;

  if (!state.chatMessages[session_id]) state.chatMessages[session_id] = [];

  if (role === 'assistant' && streaming) {
    // Remove transient status indicator (Thinking.../Processing...)
    const statusInd = document.getElementById('chatStatusIndicator');
    if (statusInd) statusInd.remove();
    // Accumulate streaming content
    if (!state.chatStreaming[session_id]) state.chatStreaming[session_id] = '';
    state.chatStreaming[session_id] += content;
    // Update the live streaming bubble if viewing this session
    if (state.activeView === 'session-detail' && state.activeSession === session_id) {
      let bubble = document.getElementById('chatStreamBubble');
      const chatArea = document.getElementById('chatArea');
      if (!bubble && chatArea) {
        bubble = document.createElement('div');
        bubble.id = 'chatStreamBubble';
        bubble.className = 'chat-bubble chat-assistant chat-streaming';
        bubble.innerHTML = `<div class="chat-header">
          <span class="chat-avatar">AI</span>
          <span class="chat-role">Assistant</span>
          <span class="typing-indicator"></span>
        </div><div class="chat-content"></div>`;
        chatArea.appendChild(bubble);
      }
      if (bubble) {
        const ct = bubble.querySelector('.chat-content');
        if (ct) ct.innerHTML = renderChatMarkdown(state.chatStreaming[session_id]);
        if (chatArea) chatArea.scrollTop = chatArea.scrollHeight;
      }
    }
    return;
  }

  if (role === 'assistant' && !streaming) {
    // Streaming complete — finalize the message
    const fullContent = state.chatStreaming[session_id] || content;
    delete state.chatStreaming[session_id];
    if (fullContent) {
      state.chatMessages[session_id].push({ role, content: fullContent, ts: new Date().toISOString() });
    }
    // Replace streaming bubble with final bubble
    if (state.activeView === 'session-detail' && state.activeSession === session_id) {
      const streamBubble = document.getElementById('chatStreamBubble');
      if (streamBubble) streamBubble.remove();
      if (fullContent) appendChatBubble(session_id, role, fullContent);
    }
    return;
  }

  // System status messages — show as transient indicators, not permanent bubbles
  if (role === 'system') {
    const lc = (content || '').toLowerCase();
    // "Thinking... (<reason>)" → render the reason inline as a
    // visible "🧠 Thinking: <reason>" line so the operator sees the
    // thinking signal as it arrives (BL184 secondary). Empty
    // <details> wrappers were previously rendered which felt broken
    // (clicking the disclosure expanded to nothing). Today the ACP
    // protocol doesn't surface the actual chain-of-thought as a
    // separate stream; if/when it does, add a body to this bubble
    // and re-introduce the <details> wrapper.
    const thinkMatch = (content || '').match(/^Thinking\.\.\.\s*\(([^)]+)\)\s*$/);
    if (thinkMatch && state.activeView === 'session-detail' && state.activeSession === session_id) {
      const chatArea = document.getElementById('chatArea');
      const prev = document.getElementById('chatStatusIndicator');
      if (prev) prev.remove();
      if (chatArea) {
        const div = document.createElement('div');
        div.className = 'chat-bubble chat-system chat-thinking-bubble';
        div.innerHTML = `<div class="chat-header"><span class="chat-avatar">S</span><span class="chat-role">Thinking</span></div>
          <div class="chat-content"><span class="chat-thinking-line">&#129504; ${escHtml(thinkMatch[1])}</span></div>`;
        chatArea.appendChild(div);
        chatArea.scrollTop = chatArea.scrollHeight;
      }
      state.chatMessages[session_id].push({ role, content, ts: new Date().toISOString() });
      return;
    }
    const isTransient = lc === 'processing...' || lc === 'thinking...' ||
      lc.startsWith('ready') || lc === 'ready for next message' || lc === 'ready — send a message';
    if (isTransient && state.activeView === 'session-detail' && state.activeSession === session_id) {
      const chatArea = document.getElementById('chatArea');
      if (chatArea) {
        // Remove previous indicator
        const prev = document.getElementById('chatStatusIndicator');
        if (prev) prev.remove();
        // Show transient indicator (auto-removed when next assistant message arrives)
        if (!lc.startsWith('ready')) {
          const ind = document.createElement('div');
          ind.id = 'chatStatusIndicator';
          ind.className = 'chat-bubble chat-system chat-status-transient';
          ind.innerHTML = `<div class="chat-header"><span class="chat-avatar">S</span><span class="chat-role">System</span></div>
            <div class="chat-content"><span class="typing-indicator"></span> ${escHtml(content)}</div>`;
          chatArea.appendChild(ind);
          chatArea.scrollTop = chatArea.scrollHeight;
        }
      }
      // Don't persist transient messages in history
      return;
    }
  }

  // User or system message (persistent)
  state.chatMessages[session_id].push({ role, content, ts: new Date().toISOString() });
  // Keep last 200 messages
  if (state.chatMessages[session_id].length > 200) {
    state.chatMessages[session_id] = state.chatMessages[session_id].slice(-200);
  }
  if (state.activeView === 'session-detail' && state.activeSession === session_id) {
    appendChatBubble(session_id, role, content);
  }
}

function appendChatBubble(sessionId, role, content) {
  const chatArea = document.getElementById('chatArea');
  if (!chatArea) return;
  const wasAtBottom = chatArea.scrollHeight - chatArea.scrollTop <= chatArea.clientHeight + 40;
  const div = document.createElement('div');
  div.className = 'chat-bubble chat-' + role;
  const avatars = { user: 'U', assistant: 'AI', system: 'S' };
  const labels = { user: 'You', assistant: 'Assistant', system: 'System' };
  const now = new Date().toLocaleTimeString([], {hour:'2-digit',minute:'2-digit'});
  const rendered = role === 'assistant' ? renderChatMarkdown(content) : escHtml(content);
  let actions = '';
  if (role === 'assistant' && content.length > 10) {
    actions = `<div class="chat-actions">
      <button class="chat-action-btn" onclick="navigator.clipboard.writeText(this.closest('.chat-bubble').querySelector('.chat-content').innerText);showToast('Copied','success',1000)">Copy</button>
      <button class="chat-action-btn" onclick="chatRememberContent(this)">Remember</button>
    </div>`;
  }
  div.innerHTML = `<div class="chat-header">
    <span class="chat-avatar">${avatars[role] || '?'}</span>
    <span class="chat-role">${labels[role] || role}</span>
    <span class="chat-time">${now}</span>
  </div>
  <div class="chat-content">${rendered}</div>${actions}`;
  chatArea.appendChild(div);
  if (wasAtBottom) chatArea.scrollTop = chatArea.scrollHeight;
}

function chatRememberContent(btn) {
  const bubble = btn.closest('.chat-bubble');
  const text = bubble?.querySelector('.chat-content')?.innerText;
  if (!text || !state.activeSession) return;
  const truncated = text.length > 300 ? text.slice(0, 300) + '...' : text;
  send('send_input', { session_id: state.activeSession, text: 'remember: ' + truncated });
  showToast('Saving to memory...', 'info', 1500);
}

// renderChatMarkdown converts markdown to HTML for chat bubbles.
// Supports: code blocks, inline code, bold, italic, lists, images, mermaid, thinking sections.
function renderChatMarkdown(text) {
  if (!text) return '';
  let html = escHtml(text);

  // Thinking/reasoning blocks: <think>...</think> or <thinking>...</thinking>
  html = html.replace(/&lt;think(?:ing)?&gt;([\s\S]*?)&lt;\/think(?:ing)?&gt;/g,
    '<details class="chat-thinking"><summary>&#129504; Thinking...</summary><div class="chat-thinking-content">$1</div></details>');

  // Mermaid diagrams: ```mermaid\n...\n```
  html = html.replace(/```mermaid\n([\s\S]*?)```/g,
    '<div class="chat-mermaid" title="Mermaid diagram"><pre class="chat-code-block"><code>$1</code></pre><div style="font-size:9px;color:var(--text2);text-align:center;">Mermaid diagram (render in docs)</div></div>');

  // Code blocks: ```lang\n...\n```
  html = html.replace(/```(\w*)\n([\s\S]*?)```/g, '<pre class="chat-code-block"><code>$2</code></pre>');

  // Image URLs: ![alt](url) or bare image URLs
  html = html.replace(/!\[([^\]]*)\]\(([^)]+)\)/g,
    '<div class="chat-image"><img src="$2" alt="$1" style="max-width:100%;border-radius:6px;margin:4px 0;" onerror="this.style.display=\'none\'" /><div style="font-size:9px;color:var(--text2);">$1</div></div>');

  // Inline code
  html = html.replace(/`([^`]+)`/g, '<code class="chat-inline-code">$1</code>');
  // Bold
  html = html.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
  // Italic
  html = html.replace(/\*([^*]+)\*/g, '<em>$1</em>');
  // Headers: ### Header
  html = html.replace(/^### (.+)$/gm, '<div style="font-weight:700;font-size:14px;margin:6px 0 2px;">$1</div>');
  html = html.replace(/^## (.+)$/gm, '<div style="font-weight:700;font-size:15px;margin:8px 0 2px;">$1</div>');
  // Horizontal rule
  html = html.replace(/^---$/gm, '<hr style="border:none;border-top:1px solid var(--border);margin:8px 0;">');
  // Numbered lists
  html = html.replace(/\n(\d+)\. /g, '\n<span style="color:var(--accent);">$1.</span> ');
  // Bullet lists
  html = html.replace(/\n- /g, '\n&bull; ');
  // Links: [text](url)
  html = html.replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2" target="_blank" style="color:var(--accent2);">$1</a>');
  // Line breaks
  html = html.replace(/\n/g, '<br>');
  return html;
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
  // BL182 — when the active session-detail view is open and the
  // session state changed (especially into waiting_input), patch the
  // Input Required banner in place so the operator sees it without
  // having to back out and re-enter the session.
  if (state.activeView === 'session-detail' && state.activeSession === sess.full_id) {
    refreshNeedsInputBanner(sess.full_id);
    updateSessionDetailButtons(sess.full_id);
    refreshGeneratingIndicator(sess.full_id); // v5.27.7 BL208/#26
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
    // Skip raw output display for chat-mode sessions — only chat_message WS events render there
    const chatArea = document.getElementById('chatArea');
    if (chatArea) return; // chat mode — don't append raw tmux output

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
    // BL184: detect channel/ACP-ready in incoming output. Run the
    // detection unconditionally — previous code only ran when the
    // banner element was already in the DOM, which missed the case
    // where the operator opens the session AFTER the ACP became
    // ready (the renderSessionDetail-time scan only checks the
    // local outputBuffer, which can lag the server-side log).
    if (markChannelReadyIfDetected(sessionId, lines)) {
      // Banner / input-bar updates only matter when the operator
      // is currently viewing this session.
      if (state.activeView === 'session-detail' && state.activeSession === sessionId) {
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
              : `<button class="send-btn send-btn-tmux" onclick="sendSessionInputDirect()" title="Send via tmux">&#9654;</button>`;
          } else {
            btnSpan.innerHTML = `<button class="send-btn" onclick="sendSessionInput()">&#9658;</button>`;
          }
          inputBar.appendChild(btnSpan);
        }
      }
    }
  }
}

// BL184: detect channel/ACP ready signals in arbitrary text. Returns
// true when a transition was newly recognised. Caller hides the
// connection banner / enables input only on `true` return AND only
// when the operator is currently viewing the session — otherwise
// the cached state.channelReady[sessionId] is what the next render
// will pick up. Removes any in-DOM connection banner unconditionally
// so a stale one doesn't persist after the session goes ready.
function markChannelReadyIfDetected(sessionId, lines) {
  if (state.channelReady[sessionId]) return false;
  const text = (Array.isArray(lines) ? lines : [String(lines || '')])
    .map(l => stripAnsi(String(l || '')))
    .join('\n');
  const ready =
    text.includes('Listening for channel') ||
    text.includes('Channel: connected') ||
    text.includes('[opencode-acp] server ready') ||
    text.includes('[opencode-acp] session') ||
    text.includes('[opencode-acp] ready') ||
    text.includes('[opencode-acp] awaiting input');
  if (!ready) return false;
  state.channelReady[sessionId] = true;
  // Best-effort banner removal — works whether we're viewing the
  // session or not (no-op when the element doesn't exist).
  const banner = document.getElementById('connBanner');
  if (banner) banner.remove();
  return true;
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
  // Refresh schedule bar — removes executed schedules from the UI
  loadSessionSchedules(sessionId);
}

function onSessionsUpdated() {
  if (state.activeView === 'sessions') {
    // F14 — try in-place per-card diff first; fall back to full
    // re-render only when the visible session SET changes (filter/
    // history toggle, new card, removed card). Eliminates the
    // flicker + scroll-reset that the full innerHTML swap caused on
    // every WS state push.
    if (!tryUpdateSessionsInPlace()) {
      renderSessionsView();
    }
  } else if (state.activeView === 'session-detail' && state.activeSession) {
    updateSessionDetailButtons(state.activeSession);
    // v5.26.49 — operator-reported: "If I'm in a session and it
    // ends, the yellow box with prompt details doesn't show up, i
    // have to exit and re enter the session for it to display."
    // Cause: bulk `sessions` WS messages replaced state.sessions
    // wholesale and called onSessionsUpdated, but only the
    // single-session `session_state` path called
    // refreshNeedsInputBanner. So when a session entered
    // waiting_input via the bulk path (which is what fires when
    // prompt_context first becomes available), the banner stayed
    // hidden until the operator re-entered the view (which calls
    // renderSessionDetail → buildNeedsInputBannerHTML).
    refreshNeedsInputBanner(state.activeSession);
    refreshGeneratingIndicator(state.activeSession); // v5.27.7 BL208/#26
  }
}

// F14 — Live cell DOM diffing for the session list.
// Returns true when the in-place update is sufficient (caller skips
// full re-render). Returns false when the structural shape changed
// (cards added/removed) and a full render is needed.
function tryUpdateSessionsInPlace() {
  const list = document.querySelector('.session-list');
  if (!list) return false; // no list rendered yet
  if (state.selectMode) return false; // checkbox layout differs per render
  if (state.sessionFilter) return false; // filtered set is highly dynamic

  // Compute visible set the same way renderSessionsView does.
  const now = Date.now();
  const RECENT_MS = (state._recentMinutes || 5) * 60 * 1000;
  const active = state.sessions.filter(s => !DONE_STATES.has(s.state));
  const recent = state.sessions.filter(s =>
    DONE_STATES.has(s.state) && s.updated_at &&
    (now - new Date(s.updated_at).getTime()) < RECENT_MS);
  const pool = state.showHistory ? state.sessions : [...active, ...recent];
  const visible = sortSessionsByOrder(pool);
  const visibleIds = visible.map(s => s.full_id || s.id);

  // Compare against current DOM card order.
  const cards = Array.from(list.querySelectorAll('.session-card[data-full-id]'));
  const cardIds = cards.map(c => c.getAttribute('data-full-id'));
  if (cardIds.length !== visibleIds.length) return false;
  for (let i = 0; i < cardIds.length; i++) {
    if (cardIds[i] !== visibleIds[i]) return false;
  }

  // Same set, same order — update each card's mutable bits in place.
  for (let i = 0; i < visible.length; i++) {
    const sess = visible[i];
    const card = cards[i];
    const newHTML = sessionCard(sess, i, visible.length);
    // Diff at outerHTML granularity per card. Skip if unchanged.
    if (card.outerHTML !== newHTML) {
      const tmp = document.createElement('div');
      tmp.innerHTML = newHTML;
      const fresh = tmp.firstElementChild;
      if (fresh) card.replaceWith(fresh);
    }
  }
  return true;
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

  // Update nav button active states; scroll active tab into view for overflow nav
  document.querySelectorAll('.nav-btn').forEach(btn => {
    const isActive = btn.dataset.view === view;
    btn.classList.toggle('active', isActive);
    if (isActive) btn.scrollIntoView({ block: 'nearest', inline: 'nearest', behavior: 'smooth' });
  });

  // FAB (issue #22) — visible only on the sessions list. Original
  // logic also showed it on the alerts list, but the alerts page
  // doesn't have a "new alert" creation flow that this FAB invokes,
  // so the affordance was misleading. v5.26.37 — operator-asked:
  // "FAB is not necessary on alerts page".
  const fab = document.getElementById('newSessionFab');
  if (fab) {
    const showFab = view === 'sessions';
    fab.classList.toggle('hidden', !showFab);
  }

  // Header search-icon — toggles filter/sort rows on list views.
  // v5.26.46 — operator-asked: the autonomous tab's filter toggle
  // should match the sessions list's magnifying-glass + live in
  // the top header bar (not inside the panel). Sessions and
  // Autonomous both expose a filter row gated behind this button.
  const headerSearchBtn = document.getElementById('headerSearchBtn');
  if (headerSearchBtn) {
    headerSearchBtn.style.display =
      (view === 'sessions' || view === 'autonomous') ? 'inline-flex' : 'none';
    headerSearchBtn.title =
      view === 'autonomous' ? 'Toggle PRD filters' : 'Toggle search & filters';
  }

  const viewEl = document.getElementById('view');
  if (view === 'session-detail') {
    state.activeSession = sessionId;
    state.activeOutputTab = 'tmux';
    backBtn.style.display = 'inline';
    nav.style.display = 'none';
    if (viewEl) viewEl.classList.add('view-full');
    updateHeaderSessName(sessionId);
    renderSessionDetail(sessionId);
  } else {
    state.activeSession = null;
    state.selectMode = false;
    state.selectedSessions.clear();
    const selectBar = document.getElementById('selectBar');
    if (selectBar) selectBar.remove();
    backBtn.style.display = 'none';
    nav.style.display = 'flex';
    if (viewEl) viewEl.classList.remove('view-full');
    destroyXterm(); // clean up terminal when leaving session detail
    // Clean up stats polling interval
    const statsPanel = document.getElementById('statsPanel');
    if (statsPanel && statsPanel._statsInterval) {
      clearInterval(statsPanel._statsInterval);
      statsPanel._statsInterval = null;
    }

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
    } else if (view === 'autonomous') {
      headerTitle.textContent = 'Autonomous';
      renderAutonomousView();
    } else if (view === 'observer') {
      headerTitle.textContent = t('nav_observer') || 'Observer';
      renderObserverView();
    } else if (view === 'plugins') {
      // BL238 — redirect to Settings → Plugins sub-tab
      _settingsTab = 'plugins';
      localStorage.setItem('cs_settings_tab', 'plugins');
      state.activeView = 'settings';
      headerTitle.textContent = 'Settings';
      renderSettingsView();
    } else if (view === 'routing') {
      // BL238 — redirect to Settings → Routing sub-tab
      _settingsTab = 'routing';
      localStorage.setItem('cs_settings_tab', 'routing');
      state.activeView = 'settings';
      headerTitle.textContent = 'Settings';
      renderSettingsView();
    } else if (view === 'orchestrator') {
      // BL238 — redirect to Settings → Orchestrator sub-tab
      _settingsTab = 'orchestrator';
      localStorage.setItem('cs_settings_tab', 'orchestrator');
      state.activeView = 'settings';
      headerTitle.textContent = 'Settings';
      renderSettingsView();
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
  // B44 — search/filter icon toggle, parity with datawatch-app sessions
  // list. DEFAULT OFF (filters hidden, session list takes full window);
  // click the magnifying-glass to reveal. State persists in
  // localStorage. Replaces the "▴ filters" text pill from #23.
  if (state._filtersCollapsed === undefined) {
    // Mobile parity: default OFF means default-collapsed. Honor any
    // existing localStorage choice; new operators get the collapsed view.
    const stored = localStorage.getItem('cs_filters_collapsed');
    state._filtersCollapsed = stored === null ? true : stored === '1';
  }
  const collapsed = !!state._filtersCollapsed;
  // The search-icon toggle moved to the top header bar (next to the
  // daemon-status light) — wired in DOMContentLoaded. When collapsed,
  // the toolbar row is hidden entirely so the session list takes the
  // full window.
  const filterToggle = '';
  const toolbarBody = collapsed ? '' : `<div class="sessions-toolbar">
    <div class="session-filter-wrap">
      <input type="text" class="session-filter-input" id="sessionFilterInput"
        placeholder="Filter sessions…" value="${filterVal}"
        oninput="state.sessionFilter=this.value;renderSessionsView();document.getElementById('sessionFilterInput').focus()" />
      ${filterText ? `<button class="session-filter-clear" onclick="state.sessionFilter='';renderSessionsView()">&#10005;</button>` : ''}
    </div>
    ${backendTypes.length > 1 ? `<div class="backend-filter-badges">${backendBadges}</div>` : ''}
    ${state.activeServer && state.activeServer !== 'local' ? `<span class="server-indicator" style="font-size:10px;padding:2px 6px;border-radius:4px;background:var(--accent2);color:var(--bg);cursor:pointer;" onclick="selectServer(null)" title="Click to return to local">&#127760; ${escHtml(state.activeServer)}</span>` : ''}
    <span id="schedBadge" style="display:none;"></span>
    <button class="btn-toggle-history ${state.showHistory ? 'active' : ''}" onclick="toggleHistory()">
      History (${history.length})
    </button>
    ${state.showHistory && history.length > 0 ? `
      <button class="btn-icon" style="font-size:14px;padding:4px 6px;opacity:${state.selectMode ? '1' : '0.5'};" onclick="toggleSelectMode()" title="Select sessions">&#9745;</button>
    ` : ''}
  </div>`;
  const toggleBtn = `<div class="sessions-toolbar-row">${filterToggle}</div>${toolbarBody}`;

  if (visible.length === 0 && active.length === 0 && recent.length === 0) {
    view.innerHTML = `
      <div class="view-content" style="position:relative;">
        <div class="sessions-watermark"><img src="/favicon.svg" alt="" /></div>
        ${history.length > 0 ? toggleBtn : ''}
        <div class="empty-state">
          <span class="empty-state-icon">💬</span>
          <h3>No active sessions</h3>
          <p>Tap the <strong>+</strong> button to start a session,<br>or send commands via Signal.</p>
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

  // Show fixed bottom bar when in select mode
  let selectBar = document.getElementById('selectBar');
  if (state.selectMode) {
    if (!selectBar) {
      selectBar = document.createElement('div');
      selectBar.id = 'selectBar';
      selectBar.className = 'select-bar-fixed';
      document.body.appendChild(selectBar);
    }
    const inactive = state.sessions.filter(s => DONE_STATES.has(s.state));
    const allSelected = state.selectedSessions.size === inactive.length && inactive.length > 0;
    selectBar.innerHTML = `
      <button class="select-bar-btn" onclick="selectAllInactive()">&#9745; ${allSelected ? 'None' : 'All'} <span style="opacity:0.6;">(${inactive.length})</span></button>
      <button class="select-bar-btn select-bar-delete" onclick="deleteSelectedSessions()" ${state.selectedSessions.size === 0 ? 'disabled' : ''}>&#128465; Delete <span style="opacity:0.6;">(${state.selectedSessions.size})</span></button>
      <button class="select-bar-btn" onclick="toggleSelectMode()">Cancel</button>
    `;
  } else if (selectBar) {
    selectBar.remove();
  }
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
  showConfirmModal(t('dialog_delete_sessions_title', [count]), () => {
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
    // Prefer the full prompt_context (multi-line) over just the last line —
    // for trust prompts, the action ("press 1") lives on a different line
    // from the imperative ("Enter to confirm"), and showing only the last
    // line leaves the user with no idea what they're actually agreeing to.
    const ctxLines = sess.prompt_context
      ? sess.prompt_context.split('\n').map(l => stripAnsi(l).trim()).filter(l => l.length > 0)
      : (sess.last_prompt ? [stripAnsi(sess.last_prompt).trim()] : []);
    const promptHtml = ctxLines.length > 0
      ? ctxLines.slice(-4).map(l => `<div>${escHtml(l.length > 100 ? l.slice(0,100) + '…' : l)}</div>`).join('')
      : '<div>Input needed</div>';
    waitingRow = `<div class="card-waiting-row" onclick="event.stopPropagation()">
      <span class="card-waiting-label">${promptHtml}</span>
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
        ${sess.server && sess.server !== 'local' ? `<span class="server-badge" style="font-size:9px;padding:1px 4px;border-radius:3px;background:var(--accent2);color:var(--bg);margin-left:2px;" title="Server: ${escHtml(sess.server)}">${escHtml(sess.server)}</span>` : ''}
        ${sess.agent_id ? `<span class="agent-badge" style="font-size:9px;padding:1px 4px;border-radius:3px;background:rgba(124,58,237,0.18);color:var(--accent2);margin-left:2px;" title="Container worker (agent ${escHtml(sess.agent_id)}). v5.26.58 — full driver kind (docker/k8s/cf) + recursion depth land when the agent record is fetched.">⬡ worker</span>` : ''}
        <span class="time">${escHtml(ago)}</span>
        <span class="card-actions" onclick="event.stopPropagation()">${actions}</span>
        <span class="drag-handle" onclick="event.stopPropagation()" title="Drag to reorder">&#8942;&#8942;</span>
      </div>
      <div class="task">
        ${escHtml(taskText)}
        ${sess.last_response ? `<button class="btn-icon card-action response-icon" onclick="event.stopPropagation();showResponseViewer('${escHtml(fullId)}')" title="View last response">&#128196;</button>` : ''}
      </div>
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
      optHtml += `<option value="__esc__">ESC</option>`;
      optHtml += `<option value="__ctrlb__">tmux prefix (Ctrl-b)</option>`;
      optHtml += `<option value="/exit">quit</option>`;
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
  } else if (cmd === '__esc__') {
    send('command', { text: `sendkey ${fullId}: Escape` });
  } else if (cmd === '__ctrlb__') {
    send('command', { text: `sendkey ${fullId}: C-b` });
  } else if (cmd === '__scroll__') {
    toggleScrollMode();
    return;
  } else if (cmd === '__pageup__') {
    scrollPage('up');
    return;
  } else if (cmd === '__pagedown__') {
    scrollPage('down');
    return;
  } else if (cmd === '__quitscroll__') {
    exitScrollMode();
    return;
  } else {
    send('send_input', { session_id: fullId, text: cmd });
  }
  showToast('Sent', 'success', 1500);
}

// ── Session detail view ───────────────────────────────────────────────────────
function renderSessionDetail(sessionId) {
  // Reset scroll mode on re-render — prevents input bar stuck in display:none
  state._scrollMode = false;
  const staleScrollBar = document.getElementById('scrollBar');
  if (staleScrollBar) staleScrollBar.remove();

  // v5.27.0 — seed Channel tab from server-side ring buffer the first
  // time we open this session. Without this, the tab is empty until a
  // new message arrives over WS, even when datawatch-app shows full
  // activity from a long-running channel.
  if (!state._channelHistoryLoaded) state._channelHistoryLoaded = {};
  if (!state._channelHistoryLoaded[sessionId]) {
    state._channelHistoryLoaded[sessionId] = true;
    fetch('/api/channel/history?session_id=' + encodeURIComponent(sessionId), { credentials: 'same-origin' })
      .then(r => r.ok ? r.json() : null)
      .then(data => {
        if (!data || !Array.isArray(data.messages) || !data.messages.length) return;
        const seen = new Set((state.channelReplies[sessionId] || []).map(r => (r.ts || '') + '|' + r.text));
        const merged = (state.channelReplies[sessionId] || []).slice();
        for (const m of data.messages) {
          const ts = m.timestamp || new Date().toISOString();
          const key = ts + '|' + (m.text || '');
          if (seen.has(key)) continue;
          merged.push({ text: m.text || '', ts, direction: m.direction || 'incoming' });
        }
        merged.sort((a, b) => (a.ts || '').localeCompare(b.ts || ''));
        state.channelReplies[sessionId] = merged.slice(-50);
        if (state.activeView === 'session-detail' && state.activeSession === sessionId) {
          renderSessionDetail(sessionId);
        }
      })
      .catch(() => {});
  }
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

  // Banner is built by buildNeedsInputBannerHTML so renderSessionDetail
  // and the live updateSession patcher share one implementation.
  // v4.0.5: dismiss is sticky for the current waiting_input episode;
  // reset only when the session transitions out of waiting_input.
  if (!isWaiting && state.needsInputDismissed[sessionId]) {
    state.needsInputDismissed[sessionId] = false;
  }
  const needsBanner = buildNeedsInputBannerHTML(sess, sessionId);

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
      // B25: when the session is waiting on a prompt, the channel will never
      // connect until the user answers — say so explicitly so the spinner
      // doesn't look like a bug.
      const blockedNote = isWaiting
        ? ' <span style="opacity:0.85;">— answer the input prompt below first</span>'
        : '';
      connBanner = `<div class="conn-status-banner" id="connBanner">
        <span class="conn-spinner"></span> Waiting for ${modeLabel}…${blockedNote}
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
    <span style="color:var(--border);margin:0 4px;">|</span>
    <button class="term-tool-btn" id="scrollModeBtn" onclick="toggleScrollMode()" title="Enter tmux scroll mode (Ctrl-b [)">&#128220; Scroll</button>
  </div>`;
  const isChatMode = (sess?.output_mode === 'chat');
  const outputAreaHtml = showChannel
    ? `<div class="output-tabs">
        <button class="output-tab active" id="tabTmux" onclick="switchOutputTab('tmux')">${isChatMode ? 'Chat' : 'Tmux'}</button>
        <button class="output-tab" id="tabChannel" onclick="switchOutputTab('channel')">Channel</button>
        <button class="btn-icon" id="channelHelpBtn" style="font-size:12px;margin-left:auto;opacity:0.6;display:none;" onclick="showChannelHelp()" title="Channel commands">?</button>
        ${isChatMode ? '' : fontCtrl}
      </div>
      <div class="output-area ${isChatMode ? 'chat-mode' : 'output-area-tmux'}" id="${isChatMode ? 'chatArea' : 'outputAreaTmux'}"></div>
      <!-- v5.27.7 (BL208 / datawatch#26) — generating indicator slot.
           refreshGeneratingIndicator(sessionId) injects/removes the
           3-dot wave when the session enters/leaves running state. -->
      <div id="generatingSlot"></div>
      <div class="output-area output-area-channel" id="outputAreaChannel" style="display:none">${channelHtml}</div>`
    : (sess?.output_mode === 'chat'
       ? `<div class="output-area chat-mode" id="chatArea"></div>`
       : `<div style="display:flex;justify-content:flex-end;padding:2px 8px;">${fontCtrl}</div>
          <div class="output-area output-area-tmux" id="outputAreaTmux"></div>`);

  // For channel mode, pick the initial send button based on active tab (only when channel connected)
  const sendBtnHtml = isActive
    ? (showChannel && !isWaiting
      ? `<span id="sendBtnWrap">${state.activeOutputTab === 'channel'
          ? `<button class="send-btn send-btn-channel" onclick="sendChannelMessage()" title="Send via MCP channel">&#9654; ch</button>`
          : `<button class="send-btn send-btn-tmux" onclick="sendSessionInputDirect()" title="Send via tmux">&#9654;</button>`
        }</span>`
      : `<button class="send-btn" onclick="sendSessionInput()">&#9658;</button>`)
    + (isActive ? `<button class="btn-icon sched-input-btn" onclick="showScheduleInputPopup('${escHtml(sessionId)}')" title="Schedule input for later">&#128339;</button>` : '')
    + (isActive ? `<button class="btn-icon voice-input-btn" id="voiceInputBtn" onclick="toggleVoiceInput('${escHtml(sessionId)}')" title="Hold to record / click to start-stop voice input">&#127908;</button>` : '')
    : '';

  view.innerHTML = `
    <div class="session-detail">
      <div class="session-info-bar">
        <div class="meta">
          ${backendText ? `<span class="backend-badge">${escHtml(backendText)}</span>` : ''}
          ${/* v5.23.0 — operator-reported: drop the channel/acp mode
              badge here since the Channel/ACP tab below already conveys
              the mode. Keep tmux mode-badge so plain tmux sessions
              still show their mode (no tab system in tmux-only mode). */
            sessionMode === 'tmux' ? `<span class="mode-badge mode-${sessionMode}">${sessionMode}</span>` : ''}
          <span class="state detail-state-badge ${badgeClass}" onclick="showStateOverride('${escHtml(sessionId)}',this)" style="cursor:pointer;" title="Click to change state">${escHtml(stateText)}</span>
          <span id="actionBtns">${actionButtons}</span>
          <button class="detail-pill-btn" onclick="toggleSessionTimeline('${escHtml(sessionId)}')" title="Show event timeline">&#128336; Timeline</button>
        </div>
      </div>
      <div id="sessionSchedules" class="session-schedules" style="display:none;"></div>
      <!-- v5.28.5 (datawatch#34) — per-session process stats panel -->
      <div id="statsPanel" class="session-stats-panel" style="display:none;"></div>
      ${connBanner}
      <div id="needsInputSlot">${needsBanner}</div>
      ${outputAreaHtml}
      ${isActive && (sess?.input_mode || 'tmux') !== 'none' ? `<div id="savedCmdsQuick" class="saved-cmds-quick"><button class="btn-icon response-detail-btn" onclick="showResponseViewer('${escHtml(sessionId)}')" title="View last response">&#128196;</button>
        <span class="tmux-arrow-group" style="display:inline-flex;gap:2px;margin-left:6px;align-items:center;" title="Send arrow key to tmux">
          <button class="btn-icon tmux-arrow-btn" onclick="sendTmuxKey('${escHtml(sessionId)}','\\x1b[A')" title="Up">&uarr;</button>
          <button class="btn-icon tmux-arrow-btn" onclick="sendTmuxKey('${escHtml(sessionId)}','\\x1b[B')" title="Down">&darr;</button>
          <button class="btn-icon tmux-arrow-btn" onclick="sendTmuxKey('${escHtml(sessionId)}','\\x1b[D')" title="Left">&larr;</button>
          <button class="btn-icon tmux-arrow-btn" onclick="sendTmuxKey('${escHtml(sessionId)}','\\x1b[C')" title="Right">&rarr;</button>
        </span></div>` : ''}
      ${isActive && (sess?.input_mode || 'tmux') !== 'none' ? `<div class="input-bar${isWaiting ? ' needs-input' : ''}${!connReady ? ' input-disabled' : ''}" id="inputBar">
        <div class="input-field-wrap">
          <!-- v5.19.0 — operator-reported: "tmux input box doesn't need
               input required above it, there is a badge for that on top".
               The needsInputSlot at the page top renders the
               needs-input-badge yellow pill which already conveys the
               state; this inline label was a duplicate. -->
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

  // Show loading splash over the terminal area while waiting for first pane_capture.
  // Only reset terminal state when navigating to a *different* session — re-renders
  // of the same session (e.g. channel_ready, WS reconnect) should preserve the
  // existing terminal content to avoid scroll/display glitches.
  const isSameSession = state._termSessionId === sessionId && state._termHasContent;
  if (!isSameSession) {
    state._termHasContent = false;
    state._termSessionId = sessionId;
  }
  state._termConnectRetries = 0;
  const sessOutputMode = sess?.output_mode || 'terminal';
  const tmuxArea = document.getElementById('outputAreaTmux');
  if (tmuxArea && isActive && !isSameSession && sessOutputMode === 'terminal') {
    tmuxArea.innerHTML = `<div id="termLoadingSplash" style="display:flex;flex-direction:column;align-items:center;justify-content:center;height:200px;color:var(--text2);gap:12px;">
      <img src="/favicon.svg" alt="" style="width:64px;opacity:0.3;" />
      <div style="font-size:13px;" id="termLoadingText">Connecting to session…</div>
      <div style="font-size:10px;color:var(--text2);opacity:0.6;" id="termLoadingRetry"></div>
    </div>`;
    // Retry logic: if no pane_capture arrives within 5s, re-subscribe
    startTermConnectWatchdog(sessionId);
  }

  // Initialize output display — xterm.js for terminal mode, log viewer for log mode, chat for chat mode
  const outputMode = sess?.output_mode || 'terminal';
  if (outputMode === 'chat') {
    // Chat mode — structured message bubbles (OpenWebUI, Ollama, any output_mode=chat backend)
    const chatArea = document.getElementById('chatArea');
    if (chatArea) {
      // Render existing chat history
      const msgs = state.chatMessages[sessionId] || [];
      const avatars = { user: 'U', assistant: 'AI', system: 'S' };
      const labels = { user: 'You', assistant: 'Assistant', system: 'System' };

      // BL82: Group older messages into collapsible threads when >6 messages
      let renderedMsgs = '';
      if (msgs.length > 6) {
        const older = msgs.slice(0, -4);
        const recent = msgs.slice(-4);
        renderedMsgs += `<details class="chat-thread">
          <summary class="chat-thread-header">&#128172; ${older.length} earlier messages (click to expand)</summary>`;
        for (const m of older) {
          const rendered = m.role === 'assistant' ? renderChatMarkdown(m.content) : escHtml(m.content);
          const ts = m.ts ? new Date(m.ts).toLocaleTimeString([], {hour:'2-digit',minute:'2-digit'}) : '';
          renderedMsgs += `<div class="chat-bubble chat-${m.role}">
            <div class="chat-header"><span class="chat-avatar">${avatars[m.role]||'?'}</span><span class="chat-role">${labels[m.role]||m.role}</span><span class="chat-time">${ts}</span></div>
            <div class="chat-content">${rendered}</div></div>`;
        }
        renderedMsgs += `</details>`;
        for (const m of recent) {
          const rendered = m.role === 'assistant' ? renderChatMarkdown(m.content) : escHtml(m.content);
          const ts = m.ts ? new Date(m.ts).toLocaleTimeString([], {hour:'2-digit',minute:'2-digit'}) : '';
          renderedMsgs += `<div class="chat-bubble chat-${m.role}">
            <div class="chat-header"><span class="chat-avatar">${avatars[m.role]||'?'}</span><span class="chat-role">${labels[m.role]||m.role}</span><span class="chat-time">${ts}</span></div>
            <div class="chat-content">${rendered}</div></div>`;
        }
      } else {
        renderedMsgs = msgs.map(m => {
          const rendered = m.role === 'assistant' ? renderChatMarkdown(m.content) : escHtml(m.content);
          const ts = m.ts ? new Date(m.ts).toLocaleTimeString([], {hour:'2-digit',minute:'2-digit'}) : '';
          return `<div class="chat-bubble chat-${m.role}">
            <div class="chat-header"><span class="chat-avatar">${avatars[m.role]||'?'}</span><span class="chat-role">${labels[m.role]||m.role}</span><span class="chat-time">${ts}</span></div>
            <div class="chat-content">${rendered}</div></div>`;
        }).join('');
      }

      chatArea.innerHTML = (msgs.length ? renderedMsgs : '') + `<div class="chat-cmd-bar" id="chatCmdBar">
        <button class="chat-cmd-btn" onclick="chatQuickCmd('memories')">&#128218; memories</button>
        <button class="chat-cmd-btn" onclick="chatQuickCmd('recall: ')">&#128269; recall</button>
        <button class="chat-cmd-btn" onclick="chatQuickCmd('kg query ')">&#128279; kg query</button>
        <button class="chat-cmd-btn" onclick="chatQuickCmd('research: ')">&#128300; research</button>
      </div>`;
      if (!msgs.length) {
        chatArea.innerHTML = `<div class="chat-empty">
          <div style="font-size:36px;opacity:0.3;">&#128172;</div>
          <div style="font-size:13px;">Send a message to begin the conversation</div>
          <div style="font-size:11px;color:var(--text2);">Memory commands work here: remember, recall, kg, research</div>
        </div>` + chatArea.innerHTML;
      }
      chatArea.scrollTop = chatArea.scrollHeight;
    }
  } else if (outputMode === 'log') {
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
  loadSessionStats(sessionId);

  // Ensure input bar is visible (safety net against scroll mode or other display:none leaks)
  const renderedInputBar = document.getElementById('inputBar');
  if (renderedInputBar) renderedInputBar.style.display = '';

  // Allow Enter key to send (only when input bar is visible for active sessions).
  // v5.27.1 — track binding via a property on the element so the rebind path
  // in refreshNeedsInputBanner doesn't double-fire sendSessionInput.
  const inputEl = document.getElementById('sessionInput');
  if (inputEl && !inputEl._dwEnterBound) {
    inputEl._dwEnterBound = true;
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

function startTermConnectWatchdog(sessionId) {
  if (state._termWatchdog) clearTimeout(state._termWatchdog);
  const MAX_RETRIES = 3;
  const TIMEOUT_MS = 5000;
  state._termWatchdog = setTimeout(() => {
    // If content arrived, stop
    if (state._termHasContent) return;
    state._termConnectRetries = (state._termConnectRetries || 0) + 1;
    const retryEl = document.getElementById('termLoadingRetry');
    const textEl = document.getElementById('termLoadingText');
    if (state._termConnectRetries > MAX_RETRIES) {
      // Max retries exceeded — show error
      if (textEl) textEl.textContent = 'Unable to connect to session terminal';
      if (retryEl) retryEl.innerHTML = `<span style="color:var(--error);">Connection failed after ${MAX_RETRIES} retries.</span><br/>
        <button class="btn-secondary" style="margin-top:8px;font-size:11px;" onclick="retryTermConnect('${escHtml(sessionId)}')">Retry</button>
        <button class="btn-secondary" style="margin-top:8px;font-size:11px;margin-left:6px;" onclick="dismissTermSplash()">Use without terminal</button>`;
      return;
    }
    // Retry: re-subscribe
    if (textEl) textEl.textContent = 'Reconnecting to session…';
    if (retryEl) retryEl.textContent = `Attempt ${state._termConnectRetries} of ${MAX_RETRIES}`;
    send('subscribe', { session_id: sessionId });
    startTermConnectWatchdog(sessionId); // schedule next check
  }, TIMEOUT_MS);
}

function retryTermConnect(sessionId) {
  state._termConnectRetries = 0;
  const textEl = document.getElementById('termLoadingText');
  const retryEl = document.getElementById('termLoadingRetry');
  if (textEl) textEl.textContent = 'Connecting to session…';
  if (retryEl) retryEl.textContent = '';
  send('subscribe', { session_id: sessionId });
  startTermConnectWatchdog(sessionId);
}

function dismissTermSplash() {
  const splash = document.getElementById('termLoadingSplash');
  if (splash) splash.remove();
  state._termHasContent = true; // prevent watchdog from firing
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

// Tmux scroll mode — enter Ctrl-b [ to browse history, PageUp/Down to navigate, ESC to exit
function toggleScrollMode() {
  if (!state.activeSession) return;
  state._scrollMode = true;
  // v5.26.14 — request one immediate redraw so the operator sees
  // the scroll-back position the moment they enter scroll mode.
  // Subsequent live-update ticks are suppressed until they click
  // PageUp / PageDown.
  state._scrollPendingRefresh = true;
  send('command', { text: `tmux-copy-mode ${state.activeSession}` });
  // Hide input bar, show scroll controls bar at bottom
  const inputBar = document.getElementById('inputBar');
  if (inputBar) inputBar.style.display = 'none';
  // Create scroll control bar
  let scrollBar = document.getElementById('scrollBar');
  if (!scrollBar) {
    scrollBar = document.createElement('div');
    scrollBar.id = 'scrollBar';
    scrollBar.className = 'input-bar scroll-bar-active';
    scrollBar.innerHTML = `
      <button class="scroll-bar-btn" onclick="scrollPage('up')">&#9650; Page Up</button>
      <button class="scroll-bar-btn" onclick="scrollPage('down')">&#9660; Page Down</button>
      <button class="scroll-bar-btn scroll-bar-esc" onclick="exitScrollMode()">ESC — Exit Scroll</button>
    `;
    if (inputBar) inputBar.parentNode.insertBefore(scrollBar, inputBar.nextSibling);
    else document.querySelector('.session-detail')?.appendChild(scrollBar);
  }
  scrollBar.style.display = 'flex';
  // Update toolbar button
  const btn = document.getElementById('scrollModeBtn');
  if (btn) { btn.textContent = '⏹ Exit Scroll'; btn.onclick = exitScrollMode; }
}

function scrollPage(dir) {
  if (!state.activeSession) return;
  // v5.26.14 — request ONE redraw on the next pane_capture so the
  // operator sees the new scroll-back position. Without this flag,
  // pane_capture skips silently while in scroll mode.
  state._scrollPendingRefresh = true;
  const key = dir === 'up' ? 'PPage' : 'NPage';
  send('command', { text: `sendkey ${state.activeSession}: ${key}` });
}

function exitScrollMode() {
  if (!state.activeSession) return;
  state._scrollMode = false;
  // Use Escape to exit tmux copy-mode (q also works but Escape is universal)
  send('command', { text: `sendkey ${state.activeSession}: Escape` });
  // v5.26.14 — drop the dedupe cache so the first post-exit
  // pane_capture forces a fresh redraw of the live pane, not a
  // skipped-as-identical from the scroll view.
  state._lastPaneFrame = null;
  state._scrollPendingRefresh = false;
  restoreInputBar();
}

// restoreInputBar ensures input bar is visible and scroll bar is removed.
// Called from exitScrollMode and as a safety net from periodic checks.
function restoreInputBar() {
  const inputBar = document.getElementById('inputBar');
  if (inputBar) inputBar.style.display = '';
  const scrollBar = document.getElementById('scrollBar');
  if (scrollBar) scrollBar.remove();
  const btn = document.getElementById('scrollModeBtn');
  if (btn) { btn.innerHTML = '&#8597; Scroll'; btn.onclick = toggleScrollMode; }
  state._scrollMode = false;
}

// Periodic safety check: if input bar is hidden but no scroll bar exists, restore it.
// Catches edge cases where scroll mode exits abnormally (WS reconnect, DOM disruption).
setInterval(() => {
  if (state.activeView !== 'session-detail') return;
  const inputBar = document.getElementById('inputBar');
  const scrollBar = document.getElementById('scrollBar');
  if (inputBar && inputBar.style.display === 'none' && !scrollBar) {
    restoreInputBar();
  }
}, 3000);

function destroyXterm() {
  if (state._termWatchdog) { clearTimeout(state._termWatchdog); state._termWatchdog = null; }
  if (state._termResizeObserver) {
    state._termResizeObserver.disconnect();
    state._termResizeObserver = null;
  }
  if (state.terminal) {
    try { state.terminal.dispose(); } catch(e) { /* already disposed */ }
    state.terminal = null;
    state.termFitAddon = null;
    state._termHasContent = false;
    state._termSessionId = null;
    state._lastPaneFrame = null; // v5.26.10 — drop the dedupe cache; next session starts fresh
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
    state._termResizeObserver = resizeObs; // store for cleanup in destroyXterm
  }

  // Interactive keyboard mode — keystrokes sent to tmux via sendkey
  term.onData(data => {
    if (state.activeSession) {
      send('send_input', { session_id: state.activeSession, text: data, raw: true });
    }
  });

  state.terminal = term;
  state.termFitAddon = fitAddon;

  // Flush buffered pane capture that arrived before terminal was ready
  if (state._pendingPaneCapture) {
    const pending = state._pendingPaneCapture;
    state._pendingPaneCapture = null;
    const capLines = pending.lines || [];
    if (capLines.length > 0) {
      try {
        const splash = document.getElementById('termLoadingSplash');
        if (splash) splash.remove();
        if (state._termWatchdog) { clearTimeout(state._termWatchdog); state._termWatchdog = null; }
        term.reset();
        term.write(capLines.join('\r\n'));
        state._termHasContent = true;
      } catch(e) { console.error('[xterm] flush pending failed:', e); }
    }
  }
}

// v5.28.5 (datawatch#34) — load and display process stats for the active session.
// Fetches /api/stats, finds the envelope for this session_id, displays CPU/RSS/net/GPU metrics.
// Polls every 1 second to track live metrics.
function loadSessionStats(sessionId) {
  const el = document.getElementById('statsPanel');
  if (!el) return;

  // Attach interval ID to element so we can clear it later when switching sessions
  if (el._statsInterval) clearInterval(el._statsInterval);

  const fetchStats = () => {
    apiFetch('/api/stats')
      .then(data => {
        if (!data || !data.envelopes) {
          el.style.display = 'none';
          return;
        }
        const envelopes = data.envelopes || [];
        const sessEnv = envelopes.find(e => e.kind === 'session' && e.id && sessionId.startsWith(e.id));
        if (!sessEnv) {
          el.style.display = 'none';
          return;
        }
        el.style.display = 'block';
        const cpu = sessEnv.cpu_pct || 0;
        const rss = sessEnv.rss_bytes || 0;
        const threads = sessEnv.threads || 0;
        const fds = sessEnv.fds || 0;
        const netRx = sessEnv.net_rx_bps || 0;
        const netTx = sessEnv.net_tx_bps || 0;
        const gpu = sessEnv.gpu_pct || 0;
        const gpuMem = sessEnv.gpu_mem_bytes || 0;
        const cpuColor = cpu > 80 ? 'var(--error)' : cpu > 50 ? 'var(--warning)' : 'var(--success)';
        const rssDisplay = rss > 1000000000 ? (rss/1000000000).toFixed(1)+'GB' : rss > 1000000 ? (rss/1000000).toFixed(0)+'MB' : (rss/1000).toFixed(0)+'KB';
        const netRxDisplay = netRx > 1000000 ? (netRx/1000000).toFixed(1)+'MB/s' : (netRx/1000).toFixed(0)+'KB/s';
        const netTxDisplay = netTx > 1000000 ? (netTx/1000000).toFixed(1)+'MB/s' : (netTx/1000).toFixed(0)+'KB/s';
        el.innerHTML = `<div class="session-stats-bar">
          <div class="stat-group">
            <span class="stat-label">CPU</span>
            <span class="stat-value" style="color:${cpuColor};">${cpu.toFixed(1)}%</span>
          </div>
          <div class="stat-divider"></div>
          <div class="stat-group">
            <span class="stat-label">RAM</span>
            <span class="stat-value">${rssDisplay}</span>
          </div>
          <div class="stat-divider"></div>
          <div class="stat-group">
            <span class="stat-label">Threads</span>
            <span class="stat-value">${threads}</span>
          </div>
          <div class="stat-divider"></div>
          <div class="stat-group">
            <span class="stat-label">FDs</span>
            <span class="stat-value">${fds}</span>
          </div>
          ${netRx > 0 || netTx > 0 ? `<div class="stat-divider"></div>
          <div class="stat-group">
            <span class="stat-label">Net</span>
            <span class="stat-value" style="font-size:10px;">↓${netRxDisplay} ↑${netTxDisplay}</span>
          </div>` : ''}
          ${gpu > 0 ? `<div class="stat-divider"></div>
          <div class="stat-group">
            <span class="stat-label">GPU</span>
            <span class="stat-value">${gpu.toFixed(1)}%${gpuMem ? ' / '+(gpuMem/1000000000).toFixed(1)+'GB' : ''}</span>
          </div>` : ''}
        </div>`;
      })
      .catch(() => { el.style.display = 'none'; });
  };

  fetchStats(); // immediate fetch
  el._statsInterval = setInterval(fetchStats, 1000); // then poll every 1 second
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
    body: JSON.stringify(body),
  }).then(() => {
    showToast('Scheduled', 'success', 1500);
    document.getElementById('schedInputPopup')?.remove();
    loadSessionSchedules(sessionId);
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
  const helpBtn = document.getElementById('channelHelpBtn');
  if (tab === 'tmux') {
    tmuxArea.style.display = '';
    channelArea.style.display = 'none';
    if (tabTmux) tabTmux.classList.add('active');
    if (tabChannel) tabChannel.classList.remove('active');
    if (helpBtn) helpBtn.style.display = 'none';
    tmuxArea.scrollTop = tmuxArea.scrollHeight;
  } else {
    tmuxArea.style.display = 'none';
    channelArea.style.display = '';
    if (tabTmux) tabTmux.classList.remove('active');
    if (tabChannel) tabChannel.classList.add('active');
    if (helpBtn) helpBtn.style.display = '';
    channelArea.scrollTop = channelArea.scrollHeight;
  }
  // Update send button to match active tab
  const wrap = document.getElementById('sendBtnWrap');
  if (wrap) {
    if (tab === 'channel') {
      wrap.innerHTML = `<button class="send-btn send-btn-channel" onclick="sendChannelMessage()" title="Send via MCP channel">&#9654; ch</button>`;
    } else {
      wrap.innerHTML = `<button class="send-btn send-btn-tmux" onclick="sendSessionInputDirect()" title="Send via tmux">&#9654;</button>`;
    }
  }
}

function killSession(sessionId) {
  showConfirmModal(t('dialog_stop_session_title'), () => {
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
  // Restart in-place: reuse the same session ID and resume the LLM conversation
  apiFetch('/api/sessions/restart', { method: 'POST', body: JSON.stringify({ id: sess.full_id }) })
    .then(updated => {
      updateSession(updated);
      navigate('session-detail', updated.full_id);
      showToast('Session restarted', 'success', 2000);
    })
    .catch(err => {
      showToast('Restart failed: ' + err.message, 'error', 4000);
    });
}

// BL284 (v5.2.0) — operator-triggered tmux arrow keys for the saved-
// commands row. The PWA already sends raw keystrokes via the WS
// send_input event when the terminal is focused; these buttons let
// operators drive arrow-key navigation without focusing the term.
function sendTmuxKey(sessionId, escSeq) {
  if (!sessionId) sessionId = state.activeSession;
  if (!sessionId) return;
  send('send_input', { session_id: sessionId, text: escSeq, raw: true });
}
window.sendTmuxKey = sendTmuxKey;

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
    // v4.0.2 — the yellow "Input Required" banner is about to be
    // stale: the user just answered the prompt. Dismiss it now so
    // the UI doesn't wait on the server state-change round-trip.
    dismissNeedsInputBanner(state.activeSession);
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
  dismissNeedsInputBanner(state.activeSession);
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
  // v4.0.2 — any quick-input answers the current prompt.
  dismissNeedsInputBanner(state.activeSession);
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

// Voice input state — one recorder at a time. Click toggles record/stop.
state.voice = { recorder: null, chunks: [], sessionId: null };

// Terminal toolbar is always visible (operator 2026-04-26): the
// tmux/channel tab + font controls + scroll button row reads cleanly
// at all viewport sizes; the prior toggle button just got in the way.
// Mobile (datawatch-app) is being aligned to match — see issue
// dmz006/datawatch-app#term-toolbar-toggle-removed.

async function toggleVoiceInput(sessionId) {
  const btn = document.getElementById('voiceInputBtn');
  const inputEl = document.getElementById('sessionInput');
  // Stop if already recording.
  if (state.voice.recorder && state.voice.recorder.state === 'recording') {
    state.voice.recorder.stop();
    return;
  }
  // Need MediaRecorder + mic permission.
  if (!navigator.mediaDevices || typeof MediaRecorder === 'undefined') {
    showToast('Voice input not supported on this browser', 'error');
    return;
  }
  let stream;
  try {
    stream = await navigator.mediaDevices.getUserMedia({ audio: true });
  } catch (err) {
    showToast('Microphone permission denied', 'error');
    return;
  }
  // Pick a MIME type the browser actually supports — Safari prefers mp4, Chrome/Firefox webm.
  let mime = '';
  for (const cand of ['audio/webm;codecs=opus', 'audio/webm', 'audio/ogg;codecs=opus', 'audio/mp4']) {
    if (MediaRecorder.isTypeSupported && MediaRecorder.isTypeSupported(cand)) { mime = cand; break; }
  }
  const rec = new MediaRecorder(stream, mime ? { mimeType: mime } : undefined);
  state.voice = { recorder: rec, chunks: [], sessionId };
  rec.ondataavailable = (e) => { if (e.data && e.data.size > 0) state.voice.chunks.push(e.data); };
  rec.onstop = async () => {
    stream.getTracks().forEach(t => t.stop());
    if (btn) { btn.classList.remove('recording'); btn.innerHTML = '&#127908;'; }
    const blob = new Blob(state.voice.chunks, { type: mime || 'audio/webm' });
    state.voice = { recorder: null, chunks: [], sessionId: null };
    if (blob.size === 0) { showToast('No audio captured', 'warning'); return; }
    if (inputEl) { inputEl.disabled = true; inputEl.placeholder = 'Transcribing…'; }
    try {
      const ext = (mime.includes('mp4') ? '.m4a' : mime.includes('ogg') ? '.ogg' : '.webm');
      const fd = new FormData();
      fd.append('audio', blob, 'voice' + ext);
      if (sessionId) fd.append('session_id', sessionId);
      fd.append('ts_client', String(Date.now()));
      const token = localStorage.getItem('cs_token') || '';
      const headers = token ? { 'Authorization': 'Bearer ' + token } : {};
      const res = await fetch('/api/voice/transcribe', { method: 'POST', headers, body: fd });
      if (!res.ok) {
        const txt = await res.text();
        showToast('Transcribe failed: ' + (txt || res.status), 'error');
        return;
      }
      const data = await res.json();
      const transcript = (data && data.transcript) || '';
      if (inputEl && transcript) {
        inputEl.value = inputEl.value ? inputEl.value + ' ' + transcript : transcript;
        inputEl.focus();
      }
    } catch (err) {
      showToast('Voice transcribe error: ' + err.message, 'error');
    } finally {
      if (inputEl) { inputEl.disabled = false; inputEl.placeholder = ''; }
    }
  };
  if (btn) { btn.classList.add('recording'); btn.innerHTML = '&#9632;'; btn.title = 'Click to stop recording'; }
  rec.start();
}

// v5.26.8 — generic mic-button factory for any textarea / input the
// operator might want to dictate into. Operator-reported: large
// editing dialogs should have mic input. Gated on
// `state._whisperEnabled` (populated on boot from /api/config) — if
// whisper isn't configured the button isn't emitted at all.
//
// Usage:
//   <textarea id="myField" ...></textarea>
//   ${micButtonHTML('myField')}
//
// The click handler delegates to startGenericVoiceInput which records
// + transcribes + appends the transcript to the named field.
window.micButtonHTML = function(targetId) {
  if (!state._whisperEnabled) return '';
  const safeId = String(targetId).replace(/'/g, '&#39;');
  return `<button type="button" class="btn-icon" data-mic-for="${escHtml(safeId)}" onclick="startGenericVoiceInput(${JSON.stringify(targetId)},this)" title="Voice input — click to start / stop" style="margin-left:4px;">&#127908;</button>`;
};

// startGenericVoiceInput is the equivalent of toggleVoiceInput for any
// arbitrary text field, not just the session-detail input bar.
// Reuses the same recorder + /api/voice/transcribe path.
window.startGenericVoiceInput = async function(targetId, btn) {
  const target = document.getElementById(targetId);
  if (!target) { showToast('mic target not found', 'error'); return; }
  // Stop if already recording for this target.
  if (state.voice && state.voice.recorder && state.voice.recorder.state === 'recording' && state.voice._genericTarget === targetId) {
    state.voice.recorder.stop();
    return;
  }
  if (!navigator.mediaDevices || typeof MediaRecorder === 'undefined') {
    showToast('Voice input not supported on this browser', 'error'); return;
  }
  let stream;
  try { stream = await navigator.mediaDevices.getUserMedia({ audio: true }); }
  catch (err) { showToast('Microphone permission denied', 'error'); return; }
  let mime = '';
  for (const cand of ['audio/webm;codecs=opus', 'audio/webm', 'audio/ogg;codecs=opus', 'audio/mp4']) {
    if (MediaRecorder.isTypeSupported && MediaRecorder.isTypeSupported(cand)) { mime = cand; break; }
  }
  const rec = new MediaRecorder(stream, mime ? { mimeType: mime } : undefined);
  state.voice = { recorder: rec, chunks: [], _genericTarget: targetId };
  rec.ondataavailable = e => { if (e.data && e.data.size > 0) state.voice.chunks.push(e.data); };
  rec.onstop = async () => {
    stream.getTracks().forEach(t => t.stop());
    if (btn) { btn.classList.remove('recording'); btn.innerHTML = '&#127908;'; }
    const blob = new Blob(state.voice.chunks, { type: mime || 'audio/webm' });
    state.voice = { recorder: null, chunks: [], sessionId: null };
    if (blob.size === 0) { showToast('No audio captured', 'warning'); return; }
    const oldPlaceholder = target.placeholder || '';
    target.placeholder = 'Transcribing…';
    target.disabled = true;
    try {
      const ext = mime.includes('mp4') ? '.m4a' : mime.includes('ogg') ? '.ogg' : '.webm';
      const fd = new FormData();
      fd.append('audio', blob, 'voice' + ext);
      fd.append('ts_client', String(Date.now()));
      const tok = localStorage.getItem('cs_token') || '';
      const headers = tok ? { 'Authorization': 'Bearer ' + tok } : {};
      const res = await fetch('/api/voice/transcribe', { method: 'POST', headers, body: fd });
      if (!res.ok) { showToast('Transcribe failed: ' + (await res.text() || res.status), 'error'); return; }
      const data = await res.json();
      const transcript = (data && data.transcript) || '';
      if (transcript) {
        target.value = target.value ? target.value + ' ' + transcript : transcript;
        target.focus();
      }
    } catch (err) {
      showToast('Voice transcribe error: ' + err.message, 'error');
    } finally {
      target.disabled = false; target.placeholder = oldPlaceholder;
    }
  };
  if (btn) { btn.classList.add('recording'); btn.innerHTML = '&#9632;'; btn.title = 'Click to stop'; }
  rec.start();
};

// v5.26.8 — CSV expand-to-modal affordance. Operator-reported: comma-
// separated list inputs (per_task_guardrails, per_story_guardrails,
// fallback_chain, etc.) are awkward to edit inline, especially on
// mobile. This helper emits an [edit list] button next to such an
// input that opens a modal with a textarea (one item per line) plus
// the mic button.
//
// Usage from a config-field renderer:
//   <input id="myCsv" ...>${csvExpandButtonHTML('myCsv', 'Per-task guardrails')}
//
window.csvExpandButtonHTML = function(targetId, label) {
  const safeId = String(targetId).replace(/'/g, '&#39;');
  const safeLabel = (label || 'list').replace(/'/g, '&#39;');
  return `<button type="button" class="btn-icon" onclick="openCsvEditModal(${JSON.stringify(targetId)},${JSON.stringify(label || 'list')})" title="Edit list in a larger dialog" style="margin-left:4px;">&#9998;</button>`;
};

window.openCsvEditModal = function(targetId, label) {
  const target = document.getElementById(targetId);
  if (!target) { showToast('CSV target not found', 'error'); return; }
  const items = (target.value || '').split(',').map(s => s.trim()).filter(Boolean);
  const overlay = document.createElement('div');
  overlay.id = 'csvEditOverlay';
  overlay.style.cssText = 'position:fixed;inset:0;z-index:1000;background:rgba(0,0,0,0.5);display:flex;align-items:center;justify-content:center;padding:16px;';
  const taId = 'csvEditTextarea';
  overlay.innerHTML = `
    <div style="background:var(--bg);border:1px solid var(--border);border-radius:8px;max-width:520px;width:100%;padding:12px;display:flex;flex-direction:column;gap:8px;">
      <div style="display:flex;justify-content:space-between;align-items:center;">
        <strong>Edit ${escHtml(label)}</strong>
        <button class="btn-icon" onclick="document.getElementById('csvEditOverlay').remove();" title="Cancel">&#10005;</button>
      </div>
      <div style="font-size:11px;color:var(--text2);">One item per line. Empty lines + leading/trailing whitespace are ignored on Save.</div>
      <div style="display:flex;align-items:flex-start;">
        <textarea id="${taId}" class="form-input" rows="10" style="flex:1;resize:vertical;font-family:monospace;font-size:12px;">${escHtml(items.join('\n'))}</textarea>
        ${micButtonHTML(taId)}
      </div>
      <div style="display:flex;gap:6px;justify-content:flex-end;">
        <button type="button" class="btn-secondary" onclick="document.getElementById('csvEditOverlay').remove();">Cancel</button>
        <button type="button" class="btn-secondary" id="csvEditSave" style="background:var(--accent2);color:#fff;">Save</button>
      </div>
    </div>
  `;
  document.body.appendChild(overlay);
  document.getElementById('csvEditSave').onclick = () => {
    const lines = (document.getElementById(taId).value || '').split(/\r?\n/).map(s => s.trim()).filter(Boolean);
    target.value = lines.join(', ');
    target.dispatchEvent(new Event('change', { bubbles: true }));
    target.dispatchEvent(new Event('blur', { bubbles: true }));
    overlay.remove();
    showToast('List updated', 'success', 1200);
  };
};

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
        { name: 'ESC', command: '__esc__' },
        { name: 'tmux prefix (Ctrl-b)', command: '__ctrlb__' },
        { name: 'quit', command: '/exit' },
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
      // v5.19.0 — operator-reported regression: Response button + tmux
      // arrow group (shipped v5.2.0) were getting blown away when this
      // function overwrote panel.innerHTML. Preserve the Response +
      // arrow affordances.
      // v5.22.0 — operator follow-up: arrows must be right-justified
      // next to the dropdown (not above/below). Layout is now:
      //   [Response] [Commands… dropdown] [custom-cmd wrap] | spacer | [↑↓←→]
      // The arrow group uses margin-left:auto to push to the row's
      // right edge inside the flex container.
      const sid = sessionId || '';
      // v5.23.0 — operator-reported: Response button should be icon-only
      // (no text "Response") between commands + arrows. The 📄 glyph
      // alone with the title tooltip is enough.
      const responseBtn = sid ? `<button class="btn-icon response-detail-btn" onclick="showResponseViewer('${sid}')" title="View last response">&#128196;</button>` : '';
      const arrows = sid ? `<span class="tmux-arrow-group" style="display:inline-flex;gap:2px;margin-left:auto;align-items:center;flex-shrink:0;" title="Send arrow key to tmux">
        <button class="btn-icon tmux-arrow-btn" onclick="sendTmuxKey('${sid}','\\x1b[A')" title="Up">&uarr;</button>
        <button class="btn-icon tmux-arrow-btn" onclick="sendTmuxKey('${sid}','\\x1b[B')" title="Down">&darr;</button>
        <button class="btn-icon tmux-arrow-btn" onclick="sendTmuxKey('${sid}','\\x1b[D')" title="Left">&larr;</button>
        <button class="btn-icon tmux-arrow-btn" onclick="sendTmuxKey('${sid}','\\x1b[C')" title="Right">&rarr;</button>
      </span>` : '';
      panel.innerHTML = responseBtn +
        `<select class="quick-cmd-select" onchange="handleQuickCmd(this)"><option value="">Commands…</option>${optHtml}</select>` +
        `<div id="customCmdWrap" class="custom-cmd-wrap" style="display:none;">` +
        `<input type="text" class="custom-cmd-input" id="customCmdInput" placeholder="Type command…" onkeydown="if(event.key==='Enter'){sendCustomCmd();event.preventDefault();}">` +
        `<button class="quick-btn" onclick="sendCustomCmd()" title="Send">&#10148;</button>` +
        `<button class="quick-btn" onclick="hideCustomCmd()" title="Cancel">&#10005;</button></div>` +
        arrows;
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
  } else if (cmd === '__esc__') {
    send('command', { text: `sendkey ${state.activeSession}: Escape` });
  } else if (cmd === '__ctrlb__') {
    send('command', { text: `sendkey ${state.activeSession}: C-b` });
  } else if (cmd === '__scroll__') {
    toggleScrollMode();
    return;
  } else if (cmd === '__pageup__') {
    scrollPage('up');
    return;
  } else if (cmd === '__pagedown__') {
    scrollPage('down');
    return;
  } else if (cmd === '__quitscroll__') {
    exitScrollMode();
    return;
  } else {
    send('send_input', { session_id: state.activeSession, text: cmd });
  }
}

function deleteSession(sessionId) {
  showConfirmModal(t('dialog_delete_session_title'), () => {
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

// Open the new-session form (replaces the retired bottom-nav "New"
// tab — issue #22). Routes to the existing 'new' view so all field +
// dir-picker logic stays in one place. The styled modal-like
// presentation comes from .new-session-modal styling on body when
// activeView === 'new'.
function openNewSessionModal() {
  state._returnView = state.activeView === 'new' ? state._returnView : (state.activeView || 'sessions');
  navigate('new');
  document.body.classList.add('new-session-active');
}

function closeNewSessionModal() {
  document.body.classList.remove('new-session-active');
  navigate(state._returnView || 'sessions');
}

function renderNewSessionView() {
  const view = document.getElementById('view');
  view.innerHTML = `
    <div class="view-content">
      <div class="new-session-view">
        <div style="display:flex;align-items:flex-start;justify-content:space-between;gap:12px;">
          <div>
            <h2 style="margin-bottom:4px;">New Session</h2>
            <p style="margin:0;">Describe the coding task for the AI to work on.</p>
          </div>
          <button class="btn-icon" onclick="closeNewSessionModal()" title="Close" aria-label="Close" style="font-size:22px;line-height:1;padding:4px 10px;">&times;</button>
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
        <!-- v5.26.63 — operator-asked: New Session needs the same
             unified Profile dropdown + cluster picker the New PRD
             modal got in v5.26.30/34/46. First option is __dir__
             (project directory + LLM backend + session profile).
             Subsequent options are configured F10 project profiles.
             When a project profile is selected, the dir picker +
             LLM backend hide; a Cluster dropdown appears with
             "Local service instance" first. Spawn routes to
             /api/agents in profile mode, /api/sessions/start in
             dir mode. -->
        <div class="form-group">
          <label for="sessProfile">Profile</label>
          <select id="sessProfile" class="form-select" onchange="_sessProfileChanged()">
            <option value="__dir__">— project directory (local checkout) —</option>
          </select>
        </div>
        <div class="form-group" id="sessClusterRow" style="display:none;">
          <label for="sessClusterProfile">Cluster</label>
          <select id="sessClusterProfile" class="form-select">
            <option value="">— Local service instance (daemon-side) —</option>
          </select>
        </div>
        <div class="form-group" id="sessBackendRow">
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
        <!-- v5.27.5 — claude-code per-session overrides. Visible only
             when the selected backend is claude-code. Populated from
             /api/llm/claude/{models,efforts,permission_modes}. -->
        <div class="form-group" id="sessClaudeRow" style="display:none;">
          <label style="display:flex;justify-content:space-between;align-items:center;">
            <span>Claude options <span style="color:var(--text2);font-size:11px;font-weight:normal;">(optional — leave blank for config defaults)</span></span>
          </label>
          <select id="sessPermissionMode" class="form-select" style="margin-top:6px;" title="Permission mode (--permission-mode)">
            <option value="">Permission mode: (config default)</option>
          </select>
          <select id="sessClaudeModel" class="form-select" style="margin-top:6px;" title="Model (--model)">
            <option value="">Model: (config default)</option>
          </select>
          <select id="sessClaudeEffort" class="form-select" style="margin-top:6px;" title="Effort level (--effort)">
            <option value="">Effort: (config default)</option>
          </select>
        </div>
        <div class="form-group" id="sessDirRow">
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
  // v5.26.63 — populate the unified Profile + Cluster dropdowns
  // (mirrors the New PRD modal). Async — defaults to __dir__ until
  // the daemon's profile lists land.
  populateSessionProjectClusterDropdowns();

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

// v5.26.63 — populate the New Session "Profile" + "Cluster" drop-
// downs with the configured F10 project + cluster profiles. Mirrors
// the New PRD modal's openPRDCreateModal flow but writes into the
// session-modal element ids. Async; the dropdowns default to
// __dir__ until the daemon's profile lists land.
function populateSessionProjectClusterDropdowns() {
  Promise.all([
    fetch('/api/profiles/projects', { headers: tokenHeader() }).then(r => r.ok ? r.json() : null).catch(() => null),
    fetch('/api/profiles/clusters', { headers: tokenHeader() }).then(r => r.ok ? r.json() : null).catch(() => null),
  ]).then(([projs, clusters]) => {
    const projectsArr = Array.isArray(projs) ? projs : (projs && projs.profiles) || (projs && projs.projects) || [];
    const clustersArr = Array.isArray(clusters) ? clusters : (clusters && clusters.profiles) || (clusters && clusters.clusters) || [];
    const projNames = projectsArr.map(p => p && (p.name || p)).filter(Boolean);
    const clusNames = clustersArr.map(c => c && (c.name || c)).filter(Boolean);
    state._prdProjectProfiles = projNames; // shared with New PRD
    state._prdClusterProfiles = clusNames;
    const psel = document.getElementById('sessProfile');
    const csel = document.getElementById('sessClusterProfile');
    if (psel) {
      psel.innerHTML = '<option value="__dir__">— project directory (local checkout) —</option>'
        + projNames.map(n => `<option value="${escHtml(n)}">${escHtml(n)}</option>`).join('');
    }
    if (csel) {
      csel.innerHTML = '<option value="">— Local service instance (daemon-side) —</option>'
        + clusNames.map(n => `<option value="${escHtml(n)}">${escHtml(n)}</option>`).join('');
    }
  });
}
window.populateSessionProjectClusterDropdowns = populateSessionProjectClusterDropdowns;

// Show/hide helper toggled by the unified Profile dropdown's
// onchange. Mirrors _prdNewProfileChanged but operates on the
// New Session modal's element ids.
function _sessProfileChanged() {
  const sel = document.getElementById('sessProfile');
  if (!sel) return;
  const usingProfile = sel.value && sel.value !== '__dir__';
  const dirRow = document.getElementById('sessDirRow');
  const clusterRow = document.getElementById('sessClusterRow');
  const backendRow = document.getElementById('sessBackendRow');
  if (dirRow) dirRow.style.display = usingProfile ? 'none' : '';
  if (clusterRow) clusterRow.style.display = usingProfile ? '' : 'none';
  if (backendRow) backendRow.style.display = usingProfile ? 'none' : '';
}
window._sessProfileChanged = _sessProfileChanged;

// v5.27.5 — populate the new-session modal's claude-only dropdowns
// from the daemon-served alias / effort / permission-mode endpoints.
// Cached after first fetch so flipping the backend dropdown back and
// forth stays instant.
let _claudeOptionCache = null;
function populateClaudeOptionDropdowns() {
  const fill = (data) => {
    const pm = document.getElementById('sessPermissionMode');
    const md = document.getElementById('sessClaudeModel');
    const ef = document.getElementById('sessClaudeEffort');
    if (pm && data.modes) {
      pm.innerHTML = '<option value="">Permission mode: (config default)</option>' +
        data.modes.filter(m => m.value !== '').map(m =>
          `<option value="${escHtml(m.value)}" title="${escHtml(m.label)}">${escHtml(m.value)}</option>`
        ).join('');
    }
    if (md && (data.aliases || data.full_names)) {
      const aliasOpts = (data.aliases || []).map(a =>
        `<option value="${escHtml(a.value)}" title="${escHtml(a.description || '')}">${escHtml(a.label)}${a.description ? ' — ' + escHtml(a.description) : ''}</option>`
      ).join('');
      const fullOpts = (data.full_names || []).map(f =>
        `<option value="${escHtml(f.value)}">${escHtml(f.label)}</option>`
      ).join('');
      md.innerHTML = '<option value="">Model: (config default)</option>' +
        (aliasOpts ? '<optgroup label="Aliases">' + aliasOpts + '</optgroup>' : '') +
        (fullOpts ? '<optgroup label="Full names">' + fullOpts + '</optgroup>' : '');
    }
    if (ef && data.levels) {
      ef.innerHTML = '<option value="">Effort: (config default)</option>' +
        data.levels.map(l => `<option value="${escHtml(l.value)}" title="${escHtml(l.label)}">${escHtml(l.value)}</option>`).join('');
    }
  };
  if (_claudeOptionCache) { fill(_claudeOptionCache); return; }
  Promise.all([
    apiFetch('/api/llm/claude/permission_modes').catch(() => ({ modes: [] })),
    apiFetch('/api/llm/claude/models').catch(() => ({ aliases: [], full_names: [] })),
    apiFetch('/api/llm/claude/efforts').catch(() => ({ levels: [] })),
  ]).then(([pm, md, ef]) => {
    _claudeOptionCache = { modes: pm.modes, aliases: md.aliases, full_names: md.full_names, levels: ef.levels };
    fill(_claudeOptionCache);
  });
}
window.populateClaudeOptionDropdowns = populateClaudeOptionDropdowns;

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
        // v5.27.5 — show claude-specific dropdowns only for claude-code.
        const claudeRow = document.getElementById('sessClaudeRow');
        if (claudeRow) {
          const isClaudeCode = (sel.value === 'claude-code');
          claudeRow.style.display = isClaudeCode ? '' : 'none';
          if (isClaudeCode) populateClaudeOptionDropdowns();
        }
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
      // v5.26.46 — operator-asked: "need to be able to create a
      // directory while browsing". Add a "+ New folder" button that
      // prompts for a name and POSTs to /api/files {action:"mkdir"}
      // (the daemon-side endpoint already exists per handleFilesMkdir);
      // refresh the listing on success.
      content.innerHTML = `<div class="dir-current">${escHtml(currentPath)}</div>` +
        `<div style="display:flex;gap:6px;flex-wrap:wrap;margin-bottom:6px;">
           <button class="btn-secondary dir-select-btn" data-select="${escHtml(currentPath)}">&#10003; Use This Folder</button>
           <button class="btn-secondary" data-mkdir="${escHtml(currentPath)}" style="font-size:11px;">+ New folder</button>
         </div>` +
        (entries || '<div style="color:var(--text2);padding:8px;font-size:12px;">No subdirectories</div>');
      content.onclick = function(ev) {
        const entry = ev.target.closest('.dir-entry');
        const selBtn = ev.target.closest('[data-select]');
        const mkBtn = ev.target.closest('[data-mkdir]');
        if (entry && entry.dataset.path) {
          loadDirContents(entry.dataset.path);
        } else if (selBtn && selBtn.dataset.select) {
          selectDir(selBtn.dataset.select);
        } else if (mkBtn && mkBtn.dataset.mkdir) {
          mkdirInBrowser(mkBtn.dataset.mkdir);
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

// v5.26.46 — create a directory under `parent` from inside the dir
// browser. Uses the existing /api/files {action:"mkdir"} endpoint.
// On success, reloads the listing so the new folder appears.
function mkdirInBrowser(parent) {
  const name = prompt('New folder name (under ' + parent + '):');
  if (!name) return;
  const trimmed = name.trim();
  if (!trimmed) return;
  // Refuse path separators — server normalises but better fail
  // client-side with a clear message.
  if (trimmed.includes('/') || trimmed.includes('\\')) {
    showToast('Folder name must not contain slashes', 'error', 2500);
    return;
  }
  const target = parent.replace(/\/$/, '') + '/' + trimmed;
  apiFetch('/api/files', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path: target, action: 'mkdir' }),
  })
    .then(() => {
      showToast('Created ' + target, 'success', 2000);
      loadDirContents(parent);
    })
    .catch(err => showToast('Mkdir failed: ' + String(err), 'error', 3000));
}
window.mkdirInBrowser = mkdirInBrowser;

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

  // v5.26.63 — branch on the unified Profile dropdown.
  const sessProfileSel = document.getElementById('sessProfile');
  const usingProfile = sessProfileSel && sessProfileSel.value && sessProfileSel.value !== '__dir__';
  if (usingProfile) {
    // F10 project profile path → spawn through /api/agents.
    const projectProfile = sessProfileSel.value;
    const clusterProfile = document.getElementById('sessClusterProfile')?.value || '';
    const body = {
      project_profile: projectProfile,
      cluster_profile: clusterProfile,
      task,
      branch: 'main',
    };
    apiFetch('/api/agents', { method: 'POST', body: JSON.stringify(body) })
      .then(res => {
        const a = (res && res.agent) ? res.agent : res;
        if (!a || !a.id) {
          throw new Error('agent spawn returned no id');
        }
        showToast('Agent spawned (' + projectProfile + ' on ' + (clusterProfile || 'local') + ')', 'success', 3000);
        if (taskInput) taskInput.value = '';
        if (nameInput) nameInput.value = '';
        // Operator stays on sessions view; the spawned agent's session
        // shows up via the WS broadcast.
        navigate('sessions');
      })
      .catch(err => showToast('Spawn failed: ' + err.message, 'error', 4000))
      .finally(() => { if (btn) { btn.disabled = false; btn.textContent = 'Start Session'; } });
    return;
  }

  // __dir__ path → existing /api/sessions/start.
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
    // v5.27.5 — claude-code per-session overrides. Empty values fall
    // through to global cfg.Session defaults on the daemon side.
    permission_mode: document.getElementById('sessPermissionMode')?.value || '',
    model: document.getElementById('sessClaudeModel')?.value || '',
    claude_effort: document.getElementById('sessClaudeEffort')?.value || '',
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

function settingsSectionHeader(key, title, docsPath) {
  const collapsed = !!settingsCollapsed[key];
  const dl = docsPath ? docsLink(docsPath) : '';
  return `<div class="settings-section-title settings-section-toggle" onclick="toggleSettingsSection('${key}')">
    <span id="settings-chev-${key}" class="settings-chevron">${collapsed ? '▶' : '▼'}</span>${escHtml(title)}${dl}
  </div>`;
}

// docsLink — returns an inline "docs" pill linking to the embedded
// markdown viewer at /diagrams.html?path=…, or empty when the
// operator has hidden inline doc links via the General → "Show
// inline doc links" toggle (default ON; persisted in localStorage).
// Stop propagation so clicking the link doesn't collapse the section.
function docsLink(path, label) {
  if (localStorage.getItem('cs_show_docs_links') === '0') return '';
  const target = '/diagrams.html#' + encodeURIComponent('docs/' + path);
  const txt = label || 'docs';
  // Open in a new tab/window when running in a browser context so the
  // operator doesn't lose their place in Settings. stopPropagation
  // keeps the click from collapsing the section header.
  return ` <a href="${target}" target="_blank" rel="noopener noreferrer" onclick="event.stopPropagation()" class="docs-link" title="Open ${escHtml(path)} in a new tab">${escHtml(txt)}</a>`;
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
  // v5.28.0 (BL214) — tab labels translated through t(); keys mirror the
  // Android resource names (settings_tab_monitor etc.) so a single
  // datawatch-app translation update lands here on the next bundle pull.
  const tabBtns = [
    ['monitor',      t('settings_tab_monitor')],
    ['general',      t('settings_tab_general')],
    ['comms',        t('settings_tab_comms')],
    ['llm',          t('settings_tab_llm')],
    ['plugins',      'Plugins'],
    ['routing',      'Routing'],
    ['orchestrator', 'Orchestrator'],
    ['about',        t('settings_tab_about')],
  ].map(([id,label]) => `<button class="settings-tab-btn output-tab ${stab===id?'active':''}" data-tab="${id}" onclick="switchSettingsTab('${id}')">${escHtml(label)}</button>`).join('');

  view.innerHTML = `
    <div class="view-content">
      <div class="settings-tabs-bar">
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
              <div class="connection-indicator" title="Long-press to refresh server connection">
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

        ${COMMS_CONFIG_FIELDS.map(sec => `
        <div class="settings-section" data-group="comms" style="${stab!=='comms'?'display:none':''}">
          ${settingsSectionHeader('cc_'+sec.id, sec.section, sec.docs)}
          <div id="settings-sec-cc_${sec.id}" style="${secContent('cc_'+sec.id)}">
            <div id="ccfg_${sec.id}" style="color:var(--text2);font-size:13px;">Loading…</div>
          </div>
        </div>
        `).join('')}

        <div class="settings-section" data-group="comms" style="${stab!=='comms'?'display:none':''}">
          ${settingsSectionHeader('proxy', 'Proxy Resilience', 'flow/proxy-flow.md')}
          <div id="settings-sec-proxy" style="${secContent('proxy')}">
            <div id="proxySettings" style="color:var(--text2);font-size:13px;padding:4px 0;">Loading…</div>
          </div>
        </div>

        <div class="settings-section" data-group="comms" style="${stab!=='comms'?'display:none':''}">
          ${settingsSectionHeader('backends', 'Communication Configuration', 'messaging-backends.md')}
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
          ${settingsSectionHeader('llm', 'LLM Configuration', 'llm-backends.md')}
          <div id="settings-sec-llm" style="${secContent('llm')}">
            <div id="llmConfigList" style="color:var(--text2);font-size:13px;">Loading…</div>
          </div>
        </div>

        ${LLM_CONFIG_FIELDS.map(sec => `
        <div class="settings-section" data-group="llm" style="${stab!=='llm'?'display:none':''}">
          ${settingsSectionHeader('lc_'+sec.id, sec.section, sec.docs)}
          <div id="settings-sec-lc_${sec.id}" style="${secContent('lc_'+sec.id)}">
            <div id="llmCfg_${sec.id}" style="color:var(--text2);font-size:13px;">Loading…</div>
          </div>
        </div>
        `).join('')}

        <!-- BL220-G6 — Cost rates editor -->
        <div class="settings-section" data-group="llm" style="${stab!=='llm'?'display:none':''}">
          ${settingsSectionHeader('costrates', 'Cost Rates (USD / 1K tokens)', 'api/sessions.md')}
          <div id="settings-sec-costrates" style="${secContent('costrates')}">
            <div id="costRatesList"><div style="color:var(--text2);font-size:13px;">Loading…</div></div>
          </div>
        </div>

        ${GENERAL_CONFIG_FIELDS.map(sec => `
        <div class="settings-section" data-group="general" style="${stab!=='general'?'display:none':''}">
          ${settingsSectionHeader('gc_'+sec.id, sec.section, sec.docs)}
          <div id="settings-sec-gc_${sec.id}" style="${secContent('gc_'+sec.id)}">
            <div id="gcfg_${sec.id}" style="color:var(--text2);font-size:13px;">Loading…</div>
          </div>
        </div>`).join('')}

        <!-- F10 sprint 2: Project Profiles + Cluster Profiles cards -->
        <div class="settings-section" data-group="general" style="${stab!=='general'?'display:none':''}">
          ${settingsSectionHeader('gc_projectprofiles', 'Project Profiles')}
          <div id="settings-sec-gc_projectprofiles" style="${secContent('gc_projectprofiles')}">
            <div id="projectProfilesPanel" style="padding:4px 12px;">
              <div style="color:var(--text2);font-size:13px;">Loading…</div>
            </div>
          </div>
        </div>

        <div class="settings-section" data-group="general" style="${stab!=='general'?'display:none':''}">
          ${settingsSectionHeader('gc_clusterprofiles', 'Cluster Profiles')}
          <div id="settings-sec-gc_clusterprofiles" style="${secContent('gc_clusterprofiles')}">
            <div id="clusterProfilesPanel" style="padding:4px 12px;">
              <div style="color:var(--text2);font-size:13px;">Loading…</div>
            </div>
          </div>
        </div>

        <!-- v5.26.56 — Container Workers (F10) configuration. Operator-asked:
             "where in the pwa settings is the agent configuration." Exposes
             every cfg.Agents knob via the config-parity rule (REST → MCP →
             CLI → comm channels were already wired; the Web UI was missing). -->
        <div class="settings-section" data-group="general" style="${stab!=='general'?'display:none':''}">
          ${settingsSectionHeader('gc_agents', 'Container Workers', 'agents.md')}
          <div id="settings-sec-gc_agents" style="${secContent('gc_agents')}">
            <div style="padding:4px 12px;font-size:11px;color:var(--text2);">
              Config for ephemeral container workers spawned via Project Profile + Cluster Profile.
              Some keys (image_prefix, image_tag, callback_url, *_bin) require a daemon restart to take effect — the daemon's <code>/api/reload</code> response flags this.
            </div>
            <div id="agentsConfigPanel" style="padding:4px 12px;font-size:13px;color:var(--text2);">Loading…</div>
          </div>
        </div>

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

        <!-- BL234 — Language card removed from General; canonical location is Settings → About. -->

        <!-- BL220 Bundle F — General tab additions -->

        <div class="settings-section" data-group="general" style="${stab!=='general'?'display:none':''}">
          ${settingsSectionHeader('templates', 'Session Templates', 'api/sessions.md')}
          <div id="settings-sec-templates" style="${secContent('templates')}">
            <div id="templatesList"><div style="color:var(--text2);font-size:13px;">Loading…</div></div>
          </div>
        </div>

        <div class="settings-section" data-group="general" style="${stab!=='general'?'display:none':''}">
          ${settingsSectionHeader('device_aliases', 'Device Aliases', 'api/devices.md')}
          <div id="settings-sec-device_aliases" style="${secContent('device_aliases')}">
            <div id="deviceAliasesList"><div style="color:var(--text2);font-size:13px;">Loading…</div></div>
          </div>
        </div>

        <!-- BL235 — Branding/Splash moved to Settings → About (see below). -->

        <!-- BL219 — Tooling artifact lifecycle -->
        <div class="settings-section" data-group="general" style="${stab!=='general'?'display:none':''}">
          ${settingsSectionHeader('tooling', 'Backend Artifact Lifecycle')}
          <div id="settings-sec-tooling" style="${secContent('tooling')}">
            <div style="padding:8px 12px;font-size:13px;color:var(--text2);">
              Datawatch can manage LLM backend file artifacts (aider cache, goose sessions, etc.)
              in your project directories.
            </div>
            <div id="toolingStatusPanel"><div style="color:var(--text2);font-size:13px;padding:8px 12px;">Loading…</div></div>
            <div style="padding:8px 12px;display:flex;gap:8px;flex-wrap:wrap;">
              <button class="btn-secondary" onclick="loadToolingPanel()" style="font-size:12px;">↻ Refresh</button>
            </div>
          </div>
        </div>

        <!-- daemon log moved to monitor tab -->

        <div class="settings-section" data-group="monitor" style="${stab!=='monitor'?'display:none':''}">
          ${settingsSectionHeader('stats', 'System Statistics', 'flow/observer-flow.md')}
          <div id="settings-sec-stats" style="${secContent('stats')}">
            <div id="statsPanel"><div style="color:var(--text2);font-size:13px;padding:8px;">Loading…</div></div>
            <!-- v4.1.1 — eBPF status (above plugins). -->
            <div id="ebpfStatusBlock" style="border-top:1px solid var(--border);margin-top:8px;padding-top:10px;">
              <div style="font-size:11px;font-weight:600;color:var(--text2);text-transform:uppercase;letter-spacing:0.5px;padding:0 12px 6px;">eBPF (per-process net)</div>
              <div id="ebpfStatusLine" style="font-size:12px;padding:0 12px 4px;color:var(--text2);">Loading…</div>
            </div>
            <!-- v4.1.0 — installed plugins status strip. -->
            <div id="pluginsStatusBlock" style="border-top:1px solid var(--border);margin-top:8px;padding-top:10px;">
              <div style="font-size:11px;font-weight:600;color:var(--text2);text-transform:uppercase;letter-spacing:0.5px;padding:0 12px 6px;">Installed plugins</div>
              <div id="pluginsStatusList" style="font-size:12px;padding:0 12px 4px;color:var(--text2);">Loading…</div>
            </div>
            <!-- BL172 (S11) — federated observer peers (Shape B/C). -->
            <div id="observerPeersBlock" style="border-top:1px solid var(--border);margin-top:8px;padding-top:10px;">
              <div style="font-size:11px;font-weight:600;color:var(--text2);text-transform:uppercase;letter-spacing:0.5px;padding:0 12px 6px;display:flex;align-items:center;gap:8px;">
                <span>Federated peers</span>
                <span style="opacity:0.6;font-weight:400;text-transform:none;letter-spacing:0;">(datawatch-stats)</span>
              </div>
              <div id="observerPeersList" style="font-size:12px;padding:0 12px 4px;color:var(--text2);">Loading…</div>
            </div>
            <!-- BL173 (S12) — cluster.nodes from Shape C; hidden when payload empty. -->
            <div id="observerClusterBlock" style="border-top:1px solid var(--border);margin-top:8px;padding-top:10px;display:none;">
              <div style="font-size:11px;font-weight:600;color:var(--text2);text-transform:uppercase;letter-spacing:0.5px;padding:0 12px 6px;display:flex;align-items:center;gap:8px;">
                <span>Cluster nodes</span>
              </div>
              <div id="observerClusterList" style="font-size:12px;padding:0 12px 4px;color:var(--text2);"></div>
            </div>
            <!-- v5.27.10 (BL216) — MCP channel bridge introspection. -->
            <div id="channelBridgeBlock" style="border-top:1px solid var(--border);margin-top:8px;padding-top:10px;">
              <div style="font-size:11px;font-weight:600;color:var(--text2);text-transform:uppercase;letter-spacing:0.5px;padding:0 12px 6px;display:flex;align-items:center;gap:8px;">
                <span>MCP channel bridge</span>
                <a href="docs/howto/setup-and-install.md#mcp-channel-bridge" style="opacity:0.6;font-weight:400;text-transform:none;letter-spacing:0;">help</a>
              </div>
              <div id="channelBridgeStatus" style="font-size:12px;padding:0 12px 4px;color:var(--text2);">Loading…</div>
            </div>
          </div>
        </div>

        <div class="settings-section" data-group="llm" style="${stab!=='llm'?'display:none':''}">
          ${settingsSectionHeader('detection', 'Detection Filters')}
          <div id="settings-sec-detection" style="${secContent('detection')}">
            <div id="detectionFiltersList"><div style="color:var(--text2);font-size:13px;">Loading…</div></div>
          </div>
        </div>

        <div class="settings-section" data-group="monitor" style="${stab!=='monitor'?'display:none':''}">
          ${settingsSectionHeader('membrowser', 'Memory Browser')}
          <div id="settings-sec-membrowser" style="${secContent('membrowser')}">
            <div style="display:flex;gap:6px;padding:4px 12px;flex-wrap:wrap;">
              <input type="text" id="memorySearchInput" class="form-input" style="flex:1;min-width:120px;" placeholder="Search memories…" />
              <select id="memoryRoleFilter" class="form-select" style="font-size:11px;width:auto;">
                <option value="">All roles</option>
                <option value="manual">Manual</option>
                <option value="session">Session</option>
                <option value="learning">Learning</option>
                <option value="output_chunk">Chunks</option>
              </select>
              <select id="memorySinceFilter" class="form-select" style="font-size:11px;width:auto;">
                <option value="">All time</option>
                <option value="7">Last 7 days</option>
                <option value="30">Last 30 days</option>
                <option value="90">Last 90 days</option>
              </select>
              <button class="btn-secondary" style="font-size:11px;" onclick="searchMemories()">Search</button>
              <button class="btn-secondary" style="font-size:11px;" onclick="listMemories()">List</button>
              <button class="btn-secondary" style="font-size:11px;" onclick="exportMemories()" title="Download JSON backup">Export</button>
            </div>
            <div id="memoryBrowserList" style="padding:4px 12px;max-height:400px;overflow-y:auto;"></div>
          </div>
        </div>

        <div class="settings-section" data-group="monitor" style="${stab!=='monitor'?'display:none':''}">
          ${settingsSectionHeader('memmaint', 'Memory Maintenance', 'memory.md')}
          <div id="settings-sec-memmaint" style="${secContent('memmaint')}">
            <div style="display:grid;grid-template-columns:1fr 1fr;gap:8px;padding:6px 12px;">
              <div>
                <div style="font-size:11px;font-weight:600;margin-bottom:4px;">Similarity-stale eviction <span style="color:var(--text2);font-weight:normal;font-size:10px;">(sweeper.py)</span></div>
                <div style="font-size:10px;color:var(--text2);margin-bottom:4px;">Drops rows that never surface in any search and are older than the cutoff. Manual + pinned rows exempt.</div>
                <div style="display:flex;gap:4px;">
                  <input type="number" id="memSweepDays" class="form-input" style="width:80px;font-size:11px;" placeholder="days" value="90" min="1" />
                  <button class="btn-secondary" style="font-size:11px;" onclick="memorySweepStale(true)">Dry-run</button>
                  <button class="btn-secondary" style="font-size:11px;background:rgba(239,68,68,0.18);color:#ef4444;" onclick="memorySweepStale(false)" title="Actually delete eviction candidates">Apply</button>
                </div>
                <div id="memSweepResult" style="font-size:10px;color:var(--text2);margin-top:4px;"></div>
              </div>
              <div>
                <div style="font-size:11px;font-weight:600;margin-bottom:4px;">Spellcheck <span style="color:var(--text2);font-weight:normal;font-size:10px;">(spellcheck.py)</span></div>
                <div style="font-size:10px;color:var(--text2);margin-bottom:4px;">Conservative Levenshtein-based suggestions on text. Never rewrites — preview only.</div>
                <textarea id="memSpellInput" class="form-input" rows="2" style="font-size:11px;width:100%;" placeholder="Paste text to check…"></textarea>
                <button class="btn-secondary" style="font-size:11px;margin-top:4px;" onclick="memorySpellCheck()">Run spellcheck</button>
                <div id="memSpellResult" style="font-size:10px;color:var(--text2);margin-top:4px;"></div>
              </div>
              <div>
                <div style="font-size:11px;font-weight:600;margin-bottom:4px;">Extract facts <span style="color:var(--text2);font-weight:normal;font-size:10px;">(general_extractor.py)</span></div>
                <div style="font-size:10px;color:var(--text2);margin-bottom:4px;">Heuristic schema-free SVO triple extraction. Useful for KG pre-population.</div>
                <textarea id="memExtractInput" class="form-input" rows="2" style="font-size:11px;width:100%;" placeholder="Paste text to extract triples from…"></textarea>
                <button class="btn-secondary" style="font-size:11px;margin-top:4px;" onclick="memoryExtractFacts()">Extract triples</button>
                <div id="memExtractResult" style="font-size:10px;color:var(--text2);margin-top:4px;"></div>
              </div>
              <div>
                <div style="font-size:11px;font-weight:600;margin-bottom:4px;">Schema version <span style="color:var(--text2);font-weight:normal;font-size:10px;">(migrate.py)</span></div>
                <div style="font-size:10px;color:var(--text2);margin-bottom:4px;">Highest schema_version row applied to the active memory backend.</div>
                <button class="btn-secondary" style="font-size:11px;" onclick="memorySchemaVersion()">Check schema</button>
                <div id="memSchemaResult" style="font-size:10px;color:var(--text2);margin-top:4px;"></div>
              </div>
            </div>
            <div style="padding:6px 12px;font-size:10px;color:var(--text2);">
              Pin/unpin individual memories from the
              <a href="javascript:void(0)" onclick="document.getElementById('settings-sec-membrowser').scrollIntoView({behavior:'smooth'})" style="color:var(--accent);">Memory Browser</a> above —
              click the 📌 icon next to any row.
              Wake-up bundle preview: <a href="/api/memory/wakeup" target="_blank" style="color:var(--accent);">GET /api/memory/wakeup</a>.
            </div>
          </div>
        </div>

        <div class="settings-section" data-group="monitor" style="${stab!=='monitor'?'display:none':''}">
          ${settingsSectionHeader('schedules', 'Scheduled Events')}
          <div id="settings-sec-schedules" style="${secContent('schedules')}">
            <div id="schedulesList"><div style="color:var(--text2);font-size:13px;">Loading…</div></div>
          </div>
        </div>

        <!-- BL220-G10 — Global cooldown controls -->
        <div class="settings-section" data-group="monitor" style="${stab!=='monitor'?'display:none':''}">
          ${settingsSectionHeader('cooldown', 'Global Cooldown', 'api/sessions.md')}
          <div id="settings-sec-cooldown" style="${secContent('cooldown')}">
            <div id="cooldownStatus"><div style="color:var(--text2);font-size:13px;">Loading…</div></div>
          </div>
        </div>

        <!-- BL220 Bundle F — Monitor tab additions -->

        <div class="settings-section" data-group="monitor" style="${stab!=='monitor'?'display:none':''}">
          ${settingsSectionHeader('analytics', 'Session Analytics', 'api/sessions.md')}
          <div id="settings-sec-analytics" style="${secContent('analytics')}">
            <div id="analyticsPanel"><div style="color:var(--text2);font-size:13px;">Loading…</div></div>
          </div>
        </div>

        <div class="settings-section" data-group="monitor" style="${stab!=='monitor'?'display:none':''}">
          ${settingsSectionHeader('audit', 'Audit Log', 'architecture.md')}
          <div id="settings-sec-audit" style="${secContent('audit')}">
            <div id="auditPanel"><div style="color:var(--text2);font-size:13px;">Loading…</div></div>
          </div>
        </div>

        <div class="settings-section" data-group="monitor" style="${stab!=='monitor'?'display:none':''}">
          ${settingsSectionHeader('pipelines', 'Pipeline Manager', 'architecture.md')}
          <div id="settings-sec-pipelines" style="${secContent('pipelines')}">
            <div id="pipelinesPanel"><div style="color:var(--text2);font-size:13px;">Loading…</div></div>
          </div>
        </div>

        <div class="settings-section" data-group="monitor" style="${stab!=='monitor'?'display:none':''}">
          ${settingsSectionHeader('kg', 'Knowledge Graph', 'memory.md')}
          <div id="settings-sec-kg" style="${secContent('kg')}">
            <div id="kgPanel"><div style="color:var(--text2);font-size:13px;">Loading…</div></div>
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
              <div class="settings-label">System documentation &amp; diagrams</div>
              <div class="settings-value"><a href="/diagrams.html" target="_blank" style="color:var(--accent2);">/diagrams.html</a> <span style="color:var(--text2);font-size:11px;margin-left:6px;">— full markdown viewer with zoom + pan diagrams</span></div>
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
          <!-- v5.28.3 — operator-asked: PWA language picker belongs at the
               top of the datawatch identity card (Settings → About), not
               buried under General. Same dropdown options as Settings →
               General → Language (kept there too for discoverability). -->
          <div class="settings-row">
            <div class="settings-label">Language</div>
            <div class="settings-value">
              <select id="localePickerAbout" onchange="setLocaleOverride(this.value)" style="background:var(--bg2);color:var(--text);border:1px solid var(--border);border-radius:4px;padding:4px 8px;">
                <option value="auto">Auto (browser default)</option>
                <option value="en">English</option>
                <option value="de">Deutsch</option>
                <option value="es">Español</option>
                <option value="fr">Français</option>
                <option value="ja">日本語</option>
              </select>
            </div>
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
          <div class="settings-row">
            <div class="settings-label">Mobile app</div>
            <div class="settings-value" style="font-size:12px;">
              <a href="https://github.com/dmz006/datawatch-app" target="_blank" rel="noopener" style="color:var(--accent);">github.com/dmz006/datawatch-app</a>
              <div style="font-size:10px;color:var(--text2);margin-top:2px;">Play Store link will land here once the app is published.</div>
            </div>
          </div>
          <!-- BL235 — Branding/Splash config belongs in the app identity card. -->
          <div class="settings-row" style="flex-direction:column;align-items:flex-start;gap:6px;border-top:1px solid var(--border);margin-top:6px;padding-top:6px;">
            <div class="settings-label" style="width:100%;font-weight:600;">Branding / Splash</div>
            <div id="brandingPanel" style="width:100%;"><div style="color:var(--text2);font-size:12px;">Loading…</div></div>
          </div>
          <!-- Orphaned tmux sessions — operator/maintenance affordance,
               moved here from Settings → Monitor (operator 2026-04-26). -->
          <div class="settings-row" style="flex-direction:column;align-items:flex-start;gap:6px;">
            <div class="settings-label" style="width:100%;">Orphaned tmux sessions</div>
            <div class="settings-value" id="aboutOrphanedTmux" style="width:100%;font-size:11px;color:var(--text2);">checking…</div>
          </div>
        </div>

        <!-- BL238 — Plugins sub-tab -->
        <div class="settings-section" data-group="plugins" style="${stab!=='plugins'?'display:none':''}">
          ${settingsSectionHeader('plugins_list', 'Plugin Manager', 'plugins.md')}
          <div id="settings-sec-plugins_list" style="${secContent('plugins_list')}">
            <div id="pluginsPanelBody"><div style="text-align:center;padding:24px;color:var(--text2);font-size:13px;">Loading…</div></div>
          </div>
        </div>

        <!-- BL238 — Routing sub-tab -->
        <div class="settings-section" data-group="routing" style="${stab!=='routing'?'display:none':''}">
          ${settingsSectionHeader('routing_rules', 'Routing Rules', 'architecture.md')}
          <div id="settings-sec-routing_rules" style="${secContent('routing_rules')}">
            <div id="routingPanelBody"><div style="text-align:center;padding:24px;color:var(--text2);font-size:13px;">Loading…</div></div>
          </div>
        </div>

        <!-- BL238 — Orchestrator sub-tab -->
        <div class="settings-section" data-group="orchestrator" style="${stab!=='orchestrator'?'display:none':''}">
          ${settingsSectionHeader('orchestrator_graphs', 'PRD Orchestrator', 'architecture.md')}
          <div id="settings-sec-orchestrator_graphs" style="${secContent('orchestrator_graphs')}">
            <div id="orchestratorPanelBody"><div style="text-align:center;padding:24px;color:var(--text2);font-size:13px;">Loading…</div></div>
          </div>
        </div>

      </div>
    </div>`;

  loadLinkStatus();
  loadConfigStatus();
  loadServers();
  loadCommsConfig();
  loadProxySettings();
  listMemories();
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
  loadCostRatesConfig();
  loadCooldownStatus();
  loadDetectionFilters();
  loadTemplatesPanel();
  loadDeviceAliasesPanel();
  loadBrandingPanel();
  loadAnalyticsPanel();
  loadAuditPanel();
  loadPipelinesPanel();
  loadKgPanel();
  loadFilters();
  loadVersionInfo();
  loadLLMConfig();
  loadLLMTabConfig();
  loadGeneralConfig();
  loadDaemonLog(0);
  loadProjectProfiles();
  loadClusterProfiles();
  loadAgentsConfig();
  loadPluginsPanel();
  loadRoutingPanel();
  loadOrchestratorPanel();
  loadToolingPanel(); // BL219
  // v5.28.0 (BL214) — sync language picker to the active override
  // (or 'auto' when no localStorage value is set). Two pickers live
  // on the page: Settings → General → Language (legacy spot) AND
  // the prominent one at the top of Settings → About per v5.28.3
  // operator request — keep both in sync.
  const picker = document.getElementById('localePicker');
  if (picker) picker.value = window._i18n.override || 'auto';
  const pickerAbout = document.getElementById('localePickerAbout');
  if (pickerAbout) pickerAbout.value = window._i18n.override || 'auto';
}

// v5.26.56 — Container Workers (F10) config panel. Operator-asked:
// "where in the pwa settings is the agent configuration." Renders
// each cfg.Agents key as a labelled input, posts changes through
// the existing PUT /api/config dotted-key endpoint (same path the
// REST surface, MCP, CLI, and comm channels all use — full parity).
function loadAgentsConfig() {
  const panel = document.getElementById('agentsConfigPanel');
  if (!panel) return;
  fetch('/api/config', { headers: tokenHeader() })
    .then(r => r.ok ? r.json() : null)
    .then(cfg => {
      if (!cfg) { panel.innerHTML = '<em style="color:var(--error);">load failed</em>'; return; }
      const a = cfg.agents || {};
      const fields = [
        ['image_prefix', 'Image prefix', 'text', a.image_prefix || '', 'Registry path prepended to image names. Example: harbor.example.com/datawatch'],
        ['image_tag', 'Image tag', 'text', a.image_tag || '', 'Tag pulled. Empty falls back to "v" + daemon version. Use "latest" for cutting edge.'],
        ['docker_bin', 'Docker binary', 'text', a.docker_bin || '', 'Default "docker"; set to "podman" for rootless.'],
        ['kubectl_bin', 'kubectl binary', 'text', a.kubectl_bin || '', 'Default "kubectl"; set to "oc" for OpenShift or a vendored path.'],
        ['callback_url', 'Callback URL', 'text', a.callback_url || '', 'URL workers dial home to. Empty = derive from server bind. Required when daemon binds 0.0.0.0 (Pods can\'t reach 0.0.0.0).'],
        ['bootstrap_token_ttl_seconds', 'Bootstrap token TTL (s)', 'number', a.bootstrap_token_ttl_seconds || 0, 'How long bootstrap tokens stay valid. Default 300.'],
        ['worker_bootstrap_deadline_seconds', 'Worker bootstrap deadline (s)', 'number', a.worker_bootstrap_deadline_seconds || 0, 'Hard cap on time a worker has to call /api/agents/bootstrap. Default 60.'],
      ];
      const rows = fields.map(([key, label, type, val, hint]) =>
        `<div class="settings-row" style="flex-direction:column;align-items:stretch;gap:4px;margin-bottom:8px;">
          <label style="font-size:12px;font-weight:600;">${escHtml(label)}</label>
          <input type="${type}" class="form-input" id="agentsCfg_${escHtml(key)}" value="${escHtml(String(val))}" placeholder="${escHtml(hint)}" />
          <div style="font-size:10px;color:var(--text2);">${escHtml(hint)}</div>
        </div>`
      ).join('');
      panel.innerHTML = rows +
        `<div style="display:flex;gap:6px;justify-content:flex-end;margin-top:8px;">
          <button class="btn-secondary" onclick="saveAgentsConfig()">Save</button>
        </div>`;
    })
    .catch(err => { panel.innerHTML = '<em style="color:var(--error);">' + escHtml(String(err)) + '</em>'; });
}
window.loadAgentsConfig = loadAgentsConfig;

function saveAgentsConfig() {
  const keys = ['image_prefix','image_tag','docker_bin','kubectl_bin','callback_url','bootstrap_token_ttl_seconds','worker_bootstrap_deadline_seconds'];
  const body = {};
  for (const k of keys) {
    const el = document.getElementById('agentsCfg_' + k);
    if (!el) continue;
    let v = el.value;
    if (el.type === 'number') v = v ? Number(v) : 0;
    body['agents.' + k] = v;
  }
  apiFetch('/api/config', {
    method: 'PUT', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
    .then(() => showToast('Container Worker config saved (some keys need restart to apply)', 'success', 3000))
    .catch(err => showToast('Save failed: ' + String(err), 'error', 3000));
}
window.saveAgentsConfig = saveAgentsConfig;

function loadVersionInfo() {
  fetch('/api/health', { headers: tokenHeader() })
    .then(r => r.ok ? r.json() : null)
    .then(data => {
      if (!data) return;
      const el = document.getElementById('aboutVersion');
      if (el) el.textContent = 'v' + (data.version || '?');
    })
    .catch(() => {});
  // BL183 follow-up (v5.2.0) — orphaned-tmux affordance moved here
  // from Settings → Monitor per operator.
  loadAboutOrphanedTmux();
}

function loadAboutOrphanedTmux() {
  const el = document.getElementById('aboutOrphanedTmux');
  if (!el) return;
  fetch('/api/stats', { headers: tokenHeader() })
    .then(r => r.ok ? r.json() : null)
    .then(data => {
      const orphans = (data && data.orphaned_tmux) || [];
      const list = orphans.length === 0
        ? `<span style="color:var(--text2);">No orphan tmux sessions detected.</span>`
        : orphans.map(n => `<code style="color:var(--text2);">${escHtml(n)}</code>`).join(' &middot; ');
      const btn = `<button class="btn-secondary" style="font-size:11px;margin-top:4px;" ${orphans.length ? '' : 'disabled style="opacity:0.5;cursor:not-allowed;"'} onclick="killOrphanedTmux()">Kill all orphaned${orphans.length ? ` (${orphans.length})` : ''}</button>`;
      el.innerHTML = list + '<br>' + btn;
    })
    .catch(() => { el.textContent = 'unavailable'; });
}
window.loadAboutOrphanedTmux = loadAboutOrphanedTmux;

// BL191 / BL202 (v5.3.0) — Autonomous PRDs panel rendering. Backed by
// /api/autonomous/prds; each row exposes the action buttons that the
// PRD's current status allows.
// BL203 (v5.4.0+) — backends list cached for the PRD/task LLM dropdowns.
// Refreshed each time loadPRDPanel runs so newly-enabled backends show up
// without a page reload.
state._prdBackends = null;
state._prdEfforts = ['', 'low', 'medium', 'high', 'max', 'quick', 'normal', 'thorough'];

function loadPRDPanel() {
  const panel = document.getElementById('prdPanel');
  if (!panel) return;
  Promise.all([
    apiFetch('/api/autonomous/prds').catch(() => ({ prds: [] })),
    fetch('/api/backends', { headers: tokenHeader() }).then(r => r.ok ? r.json() : null).catch(() => null),
  ]).then(([data, backendsResp]) => {
    state._prdBackends = (backendsResp && backendsResp.llm) || [];
    const allPrds = (data && data.prds) || [];
    // v5.26.12 — operator-reported: children should load with
    // everything else, not lazy. Build a parent_id → [children] index
    // from the flat list so each row can render its children inline.
    // This is O(N) and avoids the N+1 GET /children fan-out the
    // lazy-load path used.
    const childIdx = {};
    for (const p of allPrds) {
      const pid = p.parent_prd_id || '';
      if (!pid) continue;
      (childIdx[pid] = childIdx[pid] || []).push(p);
    }
    state._prdChildIndex = childIdx;
    const filterStatus = (document.getElementById('prdFilterStatus') || {}).value || '';
    const includeTpl = (document.getElementById('prdIncludeTemplates') || {}).checked;
    let prds = allPrds.filter(p => includeTpl || !p.is_template);
    if (filterStatus) prds = prds.filter(p => p.status === filterStatus);
    if (prds.length === 0) {
      panel.innerHTML = '<em style="color:var(--text2);">No PRDs match.</em>';
      return;
    }
    panel.innerHTML = prds.map(renderPRDRow).join('');
  }).catch(err => { panel.innerHTML = '<span style="color:var(--error);">load failed: ' + escHtml(String(err)) + '</span>'; });
}
window.loadPRDPanel = loadPRDPanel;

// Render a backend <select>. Empty option = "inherit".
//
// v5.27.0 — operator-reported: dropdown showed every backend datawatch
// has ever known about, including ones the operator hadn't configured.
// Now filters to b.enabled === true so only configured backends appear.
// String-form backend entries (legacy shape) stay visible since we
// can't know their enabled state.
//
// v5.26.13 — operator-reported: "shell should be excluded from llm
// list for automation". The shell backend is a session backend (raw
// bash) but isn't an LLM — autonomous PRDs running through it have
// nothing to "decide" or "plan", so it doesn't belong in the
// per-PRD / per-task LLM-override dropdowns. Filter it out
// unconditionally; the existing-value escape hatch below still
// surfaces a previously-pinned `shell` so existing PRDs don't drop
// the assignment silently, but new picks can't choose it.
const NON_LLM_BACKENDS = new Set(['shell']);

function renderBackendSelect(id, current, onchange) {
  const opts = ['<option value="">(inherit)</option>'];
  (state._prdBackends || []).forEach(b => {
    if (typeof b === 'string') {
      const name = b;
      if (name && !NON_LLM_BACKENDS.has(name)) opts.push(`<option value="${escHtml(name)}" ${current === name ? 'selected' : ''}>${escHtml(name)}</option>`);
      return;
    }
    if (!b || !b.name) return;
    if (b.enabled === false) return; // skip non-configured backends
    if (NON_LLM_BACKENDS.has(b.name)) return; // skip non-LLM session backends (shell, …)
    opts.push(`<option value="${escHtml(b.name)}" ${current === b.name ? 'selected' : ''}>${escHtml(b.name)}</option>`);
  });
  // Always keep the current value visible even if it's not in the
  // enabled set (operator may have configured a backend then disabled
  // it; the existing PRD assignment shouldn't drop silently).
  if (current && !opts.some(o => o.includes(`value="${escHtml(current)}"`))) {
    opts.push(`<option value="${escHtml(current)}" selected>${escHtml(current)} (not configured)</option>`);
  }
  return `<select id="${id}" class="form-select" style="font-size:11px;padding:1px 4px;" ${onchange ? `onchange="${onchange}"` : ''}>${opts.join('')}</select>`;
}

function renderEffortSelect(id, current, onchange) {
  const opts = state._prdEfforts.map(e => `<option value="${escHtml(e)}" ${current === e ? 'selected' : ''}>${e ? escHtml(e) : '(inherit)'}</option>`);
  return `<select id="${id}" class="form-select" style="font-size:11px;padding:1px 4px;" ${onchange ? `onchange="${onchange}"` : ''}>${opts.join('')}</select>`;
}

function statusPill(status) {
  const colors = {
    draft: '#6b7280', decomposing: '#3b82f6', needs_review: '#f59e0b',
    approved: '#10b981', running: '#3b82f6', completed: '#10b981',
    revisions_asked: '#f59e0b', rejected: '#ef4444', cancelled: '#6b7280',
    archived: '#6b7280', active: '#3b82f6',
  };
  const c = colors[status] || '#6b7280';
  return `<span style="background:${c};color:#fff;font-size:10px;padding:1px 6px;border-radius:8px;">${escHtml(status || '?')}</span>`;
}

function renderPRDRow(prd) {
  const id = prd.id || '';
  const stories = prd.stories || [];
  // Phase 4 follow-up (v5.26.67) — file-conflict detection. Build a
  // map of file → first-story-that-plans-it. Subsequent stories
  // that plan the same file get a ⚠ marker on render. Operator-
  // visible signal that two stories will likely collide on save.
  const _conflictMap = {};
  for (const st of stories) {
    if ((st.status === 'completed' || st.status === 'blocked' || st.status === 'failed')) continue;
    for (const f of (st.files || [])) {
      if (_conflictMap[f] === undefined) {
        _conflictMap[f] = st.id;
      } else if (_conflictMap[f] !== st.id) {
        // Tag both stories' conflict set; the renderer reads
        // story._conflictSet to display the ⚠.
        st._conflictSet = st._conflictSet || {};
        st._conflictSet[f] = _conflictMap[f];
      }
    }
  }
  const taskCount = stories.reduce((n, s) => n + (s.tasks || []).length, 0);
  const tplBadge = prd.is_template ? '<span style="background:#7c3aed;color:#fff;font-size:10px;padding:1px 6px;border-radius:8px;margin-left:4px;">template</span>' : '';
  const tplOf = prd.template_of ? `<span style="font-size:10px;color:var(--text2);margin-left:4px;">from ${escHtml(prd.template_of)}</span>` : '';
  const llmBadge = (prd.backend || prd.effort || prd.model)
    ? `<span style="font-size:10px;color:var(--accent);margin-left:6px;background:rgba(96,165,250,0.1);padding:1px 6px;border-radius:6px;">LLM: ${escHtml(prd.backend || 'inherit')}${prd.effort ? ' / ' + escHtml(String(prd.effort)) : ''}${prd.model ? ' / ' + escHtml(prd.model) : ''}</span>`
    : '';
  // BL191 Q4 (v5.16.0) — genealogy badges. Parent link + depth indicator
  // when this PRD was spawned from a parent task's SpawnPRD shortcut.
  const parentBadge = prd.parent_prd_id
    ? `<span style="font-size:10px;color:var(--accent2);margin-left:6px;background:rgba(124,58,237,0.12);padding:1px 6px;border-radius:6px;" title="parent PRD ${escHtml(prd.parent_prd_id)} task ${escHtml(prd.parent_task_id || '')}">↗ parent ${escHtml(prd.parent_prd_id)}</span>`
    : '';
  const depthBadge = prd.depth
    ? `<span style="font-size:10px;color:var(--text2);margin-left:4px;background:rgba(255,255,255,0.05);padding:1px 6px;border-radius:6px;" title="recursion depth from a root PRD">depth ${prd.depth}</span>`
    : '';
  const actions = renderPRDActions(prd);
  // v5.27.8 — .prd-card replaces inline border/padding so the card
  // visual matches the Sessions card style (BL208 #30). Status drives
  // the left-border colour via .prd-card-status-<status>.
  // .prd-row class kept as alias for the v5.26.6 scrollToPRD selector.
  const statusClass = `prd-card-status-${(prd.status || 'draft').replace(/[^a-z_]/g, '')}`;
  return `<div class="prd-row prd-card ${statusClass}">
    <div style="display:flex;justify-content:space-between;align-items:center;gap:6px;flex-wrap:wrap;">
      <div style="flex:1;min-width:0;">
        <code style="font-size:11px;color:var(--text2);">${escHtml(id)}</code> ${statusPill(prd.status)}${tplBadge}${tplOf}${llmBadge}${parentBadge}${depthBadge}
        <div style="margin-top:2px;color:var(--text);font-weight:600;">${escHtml(prd.title || '(no title)')}</div>
        <div style="font-size:10px;color:var(--text2);">${stories.length} stories &middot; ${taskCount} tasks &middot; ${prd.decisions ? prd.decisions.length : 0} decisions</div>
      </div>
      <div style="display:flex;gap:4px;flex-wrap:wrap;">${actions}</div>
    </div>
    <details style="margin-top:6px;"><summary style="cursor:pointer;font-size:11px;color:var(--accent);">Stories &amp; tasks</summary>
      <div style="margin-top:6px;">${stories.map(st => renderStory(prd, st)).join('') || '<em style="color:var(--text2);">no stories yet</em>'}</div>
      ${prd.decisions && prd.decisions.length ? '<details style="margin-top:6px;"><summary style="cursor:pointer;font-size:11px;color:var(--accent);">Decisions log (' + prd.decisions.length + ')</summary>' + prd.decisions.map(d => '<div style="font-size:10px;color:var(--text2);padding:2px 0;border-top:1px solid var(--border);"><code>' + escHtml(d.kind) + '</code> ' + escHtml(d.actor || '') + ' ' + escHtml((d.note || '')) + '</div>').join('') + '</details>' : ''}
      ${(() => {
        // v5.26.12 — children load with the rest of the panel from the
        // flat-list parent index built in loadPRDPanel. No lazy fetch.
        const kids = (state._prdChildIndex || {})[id] || [];
        if (kids.length === 0) return '';
        const rows = kids.map(c => {
          const stories = c.stories || [];
          const taskCount = stories.reduce((n, s) => n + (s.tasks || []).length, 0);
          const verdictCount = stories.reduce((n, s) => n + ((s.verdicts || []).length) +
            (s.tasks || []).reduce((m, t) => m + ((t.verdicts || []).length), 0), 0);
          const blockedCount = stories.reduce((n, s) => n + (s.verdicts || []).filter(v => v.outcome === 'block').length +
            (s.tasks || []).reduce((m, t) => m + (t.verdicts || []).filter(v => v.outcome === 'block').length, 0), 0);
          const verdictBadge = verdictCount === 0 ? '' : (
            blockedCount > 0
              ? `<span style="font-size:9px;color:#fff;background:#ef4444;padding:1px 5px;border-radius:6px;margin-left:6px;">${blockedCount} block</span>`
              : `<span style="font-size:9px;color:var(--text2);background:rgba(255,255,255,0.06);padding:1px 5px;border-radius:6px;margin-left:6px;">${verdictCount} verdict${verdictCount===1?'':'s'}</span>`
          );
          const idClick = `scrollToPRD(${JSON.stringify(c.id)})`;
          return `<div style="padding:3px 0;border-top:1px solid var(--border);">↳ <code style="cursor:pointer;color:var(--accent);text-decoration:underline;" onclick="${escHtml(idClick)}" title="Scroll to this PRD's row">${escHtml(c.id)}</code> ${statusPill(c.status)} <strong>${escHtml(c.title || '(no title)')}</strong> <span style="opacity:0.7;">depth ${c.depth || 0} · ${stories.length}s/${taskCount}t</span>${verdictBadge}</div>`;
        }).join('');
        return `<details style="margin-top:6px;" open><summary style="cursor:pointer;font-size:11px;color:var(--accent2);">Children (${kids.length})</summary><div style="font-size:10px;color:var(--text2);padding:2px 0;">${rows}</div></details>`;
      })()}
    </details>
  </div>`;
}

// BL191 Q4 (v5.16.0) — fetch the child PRDs spawned by this parent's
// SpawnPRD tasks. Lazy: triggered by the "Load" button on the Children
// disclosure so empty PRDs don't pay the GET cost.
// v5.26.6 BL202 polish — child rows are now clickable: clicking the
// child ID scrolls the panel to that PRD's own row (same panel, since
// listPRDs returns the full forest), so the operator can drill down
// without leaving the Autonomous tab. Stories+tasks counts surface
// inline; PRD-level verdicts (if any) render directly under the row.
window.loadPRDChildren = function(prdID) {
  const target = document.getElementById('prd-children-' + prdID);
  if (!target) return;
  target.innerHTML = '<em>loading…</em>';
  apiFetch('/api/autonomous/prds/' + encodeURIComponent(prdID) + '/children').then(data => {
    const kids = (data && data.children) || [];
    if (kids.length === 0) {
      target.innerHTML = '<em style="opacity:0.7;">no children — none of this PRD\'s tasks spawned a child PRD yet</em>';
      return;
    }
    target.innerHTML = kids.map(c => {
      const stories = (c.stories || []);
      const taskCount = stories.reduce((n, s) => n + (s.tasks || []).length, 0);
      const verdictCount = stories.reduce((n, s) => n + ((s.verdicts || []).length) +
        (s.tasks || []).reduce((m, t) => m + ((t.verdicts || []).length), 0), 0);
      const blockedCount = stories.reduce((n, s) => n + (s.verdicts || []).filter(v => v.outcome === 'block').length +
        (s.tasks || []).reduce((m, t) => m + (t.verdicts || []).filter(v => v.outcome === 'block').length, 0), 0);
      const verdictBadge = verdictCount === 0 ? '' : (
        blockedCount > 0
          ? `<span style="font-size:9px;color:#fff;background:#ef4444;padding:1px 5px;border-radius:6px;margin-left:6px;">${blockedCount} block</span>`
          : `<span style="font-size:9px;color:var(--text2);background:rgba(255,255,255,0.06);padding:1px 5px;border-radius:6px;margin-left:6px;">${verdictCount} verdict${verdictCount===1?'':'s'}</span>`
      );
      const idClick = `scrollToPRD(${JSON.stringify(c.id)})`;
      return `<div style="padding:3px 0;border-top:1px solid var(--border);">↳ <code style="cursor:pointer;color:var(--accent);text-decoration:underline;" onclick="${escHtml(idClick)}" title="Scroll to this PRD's row">${escHtml(c.id)}</code> ${statusPill(c.status)} <strong>${escHtml(c.title || '(no title)')}</strong> <span style="opacity:0.7;">depth ${c.depth || 0} · ${stories.length}s/${taskCount}t</span>${verdictBadge}</div>`;
    }).join('');
  }).catch(err => {
    target.innerHTML = '<span style="color:var(--error,#ef4444);">load failed: ' + escHtml(String((err && err.message) || err)) + '</span>';
  });
};

// v5.26.6 — find the child's row in the rendered PRD panel and scroll
// it into view. Quick visual highlight so the operator sees where the
// row landed. Falls back to a toast when the child isn't currently
// rendered (status-filter active, or templates-only view).
window.scrollToPRD = function(id) {
  const rows = document.querySelectorAll('#prdPanel .prd-row');
  for (const row of rows) {
    const code = row.querySelector('code');
    if (code && code.textContent.trim() === id) {
      row.scrollIntoView({ behavior: 'smooth', block: 'center' });
      const orig = row.style.boxShadow;
      row.style.boxShadow = '0 0 0 2px var(--accent)';
      setTimeout(() => { row.style.boxShadow = orig; }, 1200);
      return;
    }
  }
  showToast('PRD ' + id + ' not in current filter', 'info', 2500);
};

function renderStory(prd, story) {
  const tasks = (story.tasks || []).map(t => renderTask(prd, story, t)).join('');
  // BL191 Q6 (v5.16.0) — story-level verdicts. One badge per guardrail
  // returned at the story level after every task in this story
  // completes. `block` paints the parent PRD blocked.
  const verdicts = renderVerdicts(story.verdicts);
  // v5.26.32 — story title + description edit (operator-asked: "i
  // don't see a story review or approval or story edit option").
  // Same gate as task edit: only in needs_review / revisions_asked.
  const editable = (prd.status === 'needs_review' || prd.status === 'revisions_asked');
  const editFn = `openPRDEditStoryModal(${JSON.stringify(prd.id)},${JSON.stringify(story.id)},${JSON.stringify(story.title || '')},${JSON.stringify(story.description || '')})`;
  const editBtn = editable ? `<button class="btn-icon" style="font-size:10px;margin-left:4px;" onclick="${escHtml(editFn)}" title="Edit story title + description">&#9998;</button>` : '';
  // Phase 3.C (v5.26.62) — per-story execution profile widget +
  // Approve / Reject buttons.
  // Profile-override is editable when PRD is in needs_review /
  // revisions_asked. Pill renders the current value (or "inherit"
  // pointer) when not editable.
  const profOverrideFn = `openPRDSetStoryProfileModal(${JSON.stringify(prd.id)},${JSON.stringify(story.id)},${JSON.stringify(story.execution_profile || '')})`;
  const profPill = editable
    ? `<button class="btn-icon" style="font-size:9px;margin-left:6px;background:rgba(96,165,250,0.1);padding:1px 6px;border-radius:6px;color:var(--accent);" onclick="${escHtml(profOverrideFn)}" title="Override execution profile for this story">prof: ${escHtml(story.execution_profile || '(inherit)')}</button>`
    : (story.execution_profile
        ? `<span style="font-size:9px;margin-left:6px;background:rgba(96,165,250,0.08);padding:1px 6px;border-radius:6px;color:var(--accent);" title="Per-story execution profile override">prof: ${escHtml(story.execution_profile)}</span>`
        : '');
  // Approve / Reject visible when story is awaiting_approval AND
  // PRD is in approved/active/running.
  const prdRunnable = (prd.status === 'approved' || prd.status === 'active' || prd.status === 'running');
  const showAR = prdRunnable && story.status === 'awaiting_approval';
  const approveFn = `_prdStoryApprove(${JSON.stringify(prd.id)},${JSON.stringify(story.id)})`;
  const rejectFn  = `_prdStoryReject(${JSON.stringify(prd.id)},${JSON.stringify(story.id)})`;
  const arBtns = showAR
    ? `<span style="margin-left:6px;">
         <button class="btn-icon" style="font-size:10px;background:rgba(16,185,129,0.18);color:#10b981;padding:1px 6px;border-radius:6px;" onclick="${escHtml(approveFn)}" title="Approve this story">&#10003; approve</button>
         <button class="btn-icon" style="font-size:10px;background:rgba(239,68,68,0.18);color:#ef4444;padding:1px 6px;border-radius:6px;margin-left:2px;" onclick="${escHtml(rejectFn)}" title="Reject this story">&#10005; reject</button>
       </span>`
    : '';
  const statusPill = story.status
    ? `<span style="font-size:9px;margin-left:6px;background:rgba(255,255,255,0.06);padding:1px 6px;border-radius:6px;color:var(--text2);">${escHtml(story.status)}</span>`
    : '';
  const desc = story.description ? `<div style="font-size:10px;color:var(--text2);margin:2px 0 4px 0;white-space:pre-wrap;">${escHtml(story.description)}</div>` : '';
  // Phase 4 (v5.26.64) — file association pills.
  // Phase 4 follow-up (v5.26.67) — ⚠ marker on files that conflict
  // with another pending story; click pill to edit list.
  const conflicts = story._conflictSet || {};
  const filesEditFn = `openPRDEditStoryFilesModal(${JSON.stringify(prd.id)},${JSON.stringify(story.id)},${JSON.stringify(story.files || [])})`;
  const hasFiles = story.files && story.files.length;
  // v5.26.70 — when no files yet, fold the edit affordance into the
  // title row so empty stories don't carry a blank "📝 ✎ files" line.
  const inlineFilesBtn = (editable && !hasFiles)
    ? `<button class="btn-icon" style="font-size:9px;margin-left:6px;color:var(--accent);background:rgba(96,165,250,0.1);padding:1px 6px;border-radius:6px;" onclick="${escHtml(filesEditFn)}" title="Add planned files">✎ files</button>`
    : '';
  const filesEditBtn = (editable && hasFiles)
    ? `<button class="btn-icon" style="font-size:9px;margin-left:6px;color:var(--accent);background:rgba(96,165,250,0.1);padding:1px 6px;border-radius:6px;" onclick="${escHtml(filesEditFn)}" title="Edit planned files">✎ files</button>`
    : '';
  const filesPlanned = hasFiles
    ? `<div style="font-size:10px;color:var(--text2);margin:2px 0;"><span style="color:var(--accent);">📝</span> ${story.files.map(f => `<code style="background:rgba(96,165,250,0.08);padding:1px 4px;border-radius:3px;margin-right:4px;" title="${conflicts[f] ? 'conflicts with story ' + escHtml(conflicts[f]) : ''}">${conflicts[f] ? '⚠ ' : ''}${escHtml(f)}</code>`).join('')}${filesEditBtn}</div>`
    : '';
  // v5.26.70 — collapse empty-segment whitespace so hidden lines
  // (no description, no files, no rejected_reason) don't carry
  // visible padding/margin in the rendered story card.
  const segments = [
    desc,
    filesPlanned,
    story.rejected_reason ? `<div style="font-size:10px;color:#ef4444;margin:2px 0;">rejected: ${escHtml(story.rejected_reason)}</div>` : '',
    tasks,
  ].filter(Boolean).join('');
  return `<div style="margin:4px 0;padding:4px;border-left:2px solid var(--accent2);"><div style="font-size:11px;font-weight:600;">${escHtml(story.title || story.id)}${statusPill}${profPill}${verdicts}${editBtn}${arBtns}${inlineFilesBtn}</div>${segments}</div>`;
}

// Phase 3.C (v5.26.62) — per-story Approve / Reject / set-profile
// modal helpers. Each posts to the corresponding REST endpoint
// added in v5.26.60 (Phase 3.A) and reloads the PRD panel on success.
function _prdStoryApprove(prdID, storyID) {
  apiFetch('/api/autonomous/prds/' + encodeURIComponent(prdID) + '/approve_story', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ story_id: storyID, actor: 'operator' }),
  })
    .then(() => { showToast('Story approved', 'success', 1500); loadPRDPanel(); })
    .catch(err => showToast('Approve failed: ' + String(err), 'error', 3000));
}
window._prdStoryApprove = _prdStoryApprove;

function _prdStoryReject(prdID, storyID) {
  const reason = prompt('Reject reason (required):');
  if (!reason || !reason.trim()) return;
  apiFetch('/api/autonomous/prds/' + encodeURIComponent(prdID) + '/reject_story', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ story_id: storyID, actor: 'operator', reason: reason.trim() }),
  })
    .then(() => { showToast('Story rejected', 'success', 1500); loadPRDPanel(); })
    .catch(err => showToast('Reject failed: ' + String(err), 'error', 3000));
}
window._prdStoryReject = _prdStoryReject;

// Phase 4 follow-up (v5.26.67) — operator-asked PWA file-edit modal.
// Mirrors the v5.26.8 CSV-edit pattern: textarea (one path per line)
// + mic input, save POSTs to .../set_story_files.
function openPRDEditStoryFilesModal(prdID, storyID, currentFiles) {
  const initial = (currentFiles || []).join('\n');
  _prdMountModal(`
    <div class="response-modal-header">
      <strong>Edit story files</strong>
      <button class="btn-icon" onclick="_prdCloseModal()" title="Close">&#10005;</button>
    </div>
    <form id="prdModalForm" class="response-modal-body" style="display:flex;flex-direction:column;gap:8px;">
      <label style="font-size:11px;color:var(--text2);display:flex;align-items:center;gap:4px;">
        Repo-relative paths, one per line. ${micButtonHTML('prdStoryFilesText')}
      </label>
      <textarea id="prdStoryFilesText" class="form-input" rows="8" style="resize:vertical;font-family:monospace;font-size:11px;">${escHtml(initial)}</textarea>
      <div style="font-size:10px;color:var(--text2);">Up to 50 paths. Empty lines + leading/trailing whitespace get stripped.</div>
      <div style="display:flex;gap:6px;justify-content:flex-end;">
        <button type="button" class="btn-secondary" onclick="_prdCloseModal()">Cancel</button>
        <button type="submit" class="btn-secondary" style="background:var(--accent2);color:#fff;">Save</button>
      </div>
    </form>
  `, () => {
    const txt = document.getElementById('prdStoryFilesText').value || '';
    const files = txt.split('\n').map(s => s.trim()).filter(Boolean).slice(0, 50);
    apiFetch('/api/autonomous/prds/' + encodeURIComponent(prdID) + '/set_story_files', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ story_id: storyID, files, actor: 'operator' }),
    })
      .then(() => { showToast('Story files saved', 'success', 1500); _prdCloseModal(); loadPRDPanel(); })
      .catch(err => showToast('Save failed: ' + String(err), 'error', 3000));
  });
}
window.openPRDEditStoryFilesModal = openPRDEditStoryFilesModal;

// Same shape for tasks.
function openPRDEditTaskFilesModal(prdID, taskID, currentFiles) {
  const initial = (currentFiles || []).join('\n');
  _prdMountModal(`
    <div class="response-modal-header">
      <strong>Edit task files</strong>
      <button class="btn-icon" onclick="_prdCloseModal()" title="Close">&#10005;</button>
    </div>
    <form id="prdModalForm" class="response-modal-body" style="display:flex;flex-direction:column;gap:8px;">
      <label style="font-size:11px;color:var(--text2);display:flex;align-items:center;gap:4px;">
        Repo-relative paths, one per line. ${micButtonHTML('prdTaskFilesText')}
      </label>
      <textarea id="prdTaskFilesText" class="form-input" rows="8" style="resize:vertical;font-family:monospace;font-size:11px;">${escHtml(initial)}</textarea>
      <div style="font-size:10px;color:var(--text2);">Up to 50 paths.</div>
      <div style="display:flex;gap:6px;justify-content:flex-end;">
        <button type="button" class="btn-secondary" onclick="_prdCloseModal()">Cancel</button>
        <button type="submit" class="btn-secondary" style="background:var(--accent2);color:#fff;">Save</button>
      </div>
    </form>
  `, () => {
    const txt = document.getElementById('prdTaskFilesText').value || '';
    const files = txt.split('\n').map(s => s.trim()).filter(Boolean).slice(0, 50);
    apiFetch('/api/autonomous/prds/' + encodeURIComponent(prdID) + '/set_task_files', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ task_id: taskID, files, actor: 'operator' }),
    })
      .then(() => { showToast('Task files saved', 'success', 1500); _prdCloseModal(); loadPRDPanel(); })
      .catch(err => showToast('Save failed: ' + String(err), 'error', 3000));
  });
}
window.openPRDEditTaskFilesModal = openPRDEditTaskFilesModal;

function openPRDSetStoryProfileModal(prdID, storyID, currentProfile) {
  // Profile dropdown: "(inherit PRD default)" + every configured
  // project profile by name.
  const opts = ['<option value="">(inherit PRD default)</option>']
    .concat((state._prdProjectProfiles || []).map(n =>
      `<option value="${escHtml(n)}" ${currentProfile === n ? 'selected' : ''}>${escHtml(n)}</option>`));
  _prdMountModal(`
    <div class="response-modal-header">
      <strong>Override execution profile</strong>
      <button class="btn-icon" onclick="_prdCloseModal()" title="Close">&#10005;</button>
    </div>
    <form id="prdModalForm" class="response-modal-body" style="display:flex;flex-direction:column;gap:8px;">
      <label style="font-size:11px;color:var(--text2);">Story ${escHtml(storyID)}</label>
      <select id="prdSetStoryProfile" class="form-select" style="font-size:11px;padding:1px 4px;">${opts.join('')}</select>
      <div style="font-size:10px;color:var(--text2);">Empty = inherit the PRD's default execution profile (PRD.project_profile). A name overrides for this story only.</div>
      <div style="display:flex;gap:6px;justify-content:flex-end;">
        <button type="button" class="btn-secondary" onclick="_prdCloseModal()">Cancel</button>
        <button type="submit" class="btn-secondary" style="background:var(--accent2);color:#fff;">Save</button>
      </div>
    </form>
  `, () => {
    const next = document.getElementById('prdSetStoryProfile').value;
    apiFetch('/api/autonomous/prds/' + encodeURIComponent(prdID) + '/set_story_profile', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ story_id: storyID, profile: next, actor: 'operator' }),
    })
      .then(() => { showToast('Story profile saved', 'success', 1500); _prdCloseModal(); loadPRDPanel(); })
      .catch(err => showToast('Save failed: ' + String(err), 'error', 3000));
  });
}
window.openPRDSetStoryProfileModal = openPRDSetStoryProfileModal;

function renderTask(prd, story, task) {
  const editable = (prd.status === 'needs_review' || prd.status === 'revisions_asked');
  // v5.26.3 — same attribute-quote escape applies as renderPRDActions.
  const editFn = `openPRDEditTaskModal(${JSON.stringify(prd.id)},${JSON.stringify(task.id)},${JSON.stringify(task.spec || '')},${JSON.stringify(task.backend || '')},${JSON.stringify(String(task.effort || ''))},${JSON.stringify(task.model || '')})`;
  const editBtn = editable ? `<button class="btn-icon" style="font-size:10px;" onclick="${escHtml(editFn)}" title="Edit spec + LLM">&#9998;</button>` : '';
  // BL203 — surface per-task LLM override when set; show "(inherit)" when empty.
  const llmBadge = (task.backend || task.effort || task.model)
    ? `<span style="font-size:9px;color:var(--accent);margin-left:4px;background:rgba(96,165,250,0.1);padding:1px 4px;border-radius:4px;">LLM: ${escHtml(task.backend || 'inherit')}${task.effort ? ' / ' + escHtml(String(task.effort)) : ''}${task.model ? ' / ' + escHtml(task.model) : ''}</span>`
    : '';
  // BL191 Q4 (v5.16.0) — SpawnPRD shortcut affordances. Indicator that
  // the task spec is treated as a child PRD spec; when the executor has
  // already spawned a child, show the link.
  const spawnBadge = task.spawn_prd
    ? `<span style="font-size:9px;color:var(--accent2);margin-left:4px;background:rgba(124,58,237,0.12);padding:1px 4px;border-radius:4px;" title="this task spec is a child PRD spec; executor will Decompose+(auto-)Approve+Run">↳ spawn</span>`
    : '';
  const childLink = task.child_prd_id
    ? `<span style="font-size:9px;color:var(--accent);margin-left:4px;">→ child <code>${escHtml(task.child_prd_id)}</code></span>`
    : '';
  // BL191 Q6 (v5.16.0) — task-level verdicts.
  const verdicts = renderVerdicts(task.verdicts);
  // Phase 4 (v5.26.64) — file association pills. Planned (📝
  // accent) shown when set; touched (✅ green) shown post-spawn.
  const taskFilesEditFn = `openPRDEditTaskFilesModal(${JSON.stringify(prd.id)},${JSON.stringify(task.id)},${JSON.stringify(task.files || [])})`;
  const hasTaskFiles = task.files && task.files.length;
  const hasTaskTouched = task.files_touched && task.files_touched.length;
  // v5.26.70 — only emit the file-edit ✎ when there are files to
  // edit. Empty editable tasks no longer carry a "📝 ✎" line.
  const taskFilesEditBtn = (editable && hasTaskFiles)
    ? `<button class="btn-icon" style="font-size:8px;margin-left:4px;color:var(--accent);background:rgba(96,165,250,0.08);padding:0 4px;border-radius:3px;" onclick="${escHtml(taskFilesEditFn)}" title="Edit planned files">✎</button>`
    : '';
  const inlineTaskFilesBtn = (editable && !hasTaskFiles)
    ? `<button class="btn-icon" style="font-size:8px;margin-left:4px;color:var(--accent);background:rgba(96,165,250,0.08);padding:0 4px;border-radius:3px;" onclick="${escHtml(taskFilesEditFn)}" title="Add planned files">✎ files</button>`
    : '';
  const filesP = hasTaskFiles
    ? `<div style="font-size:9px;margin-top:1px;"><span style="color:var(--accent);">📝</span> ${task.files.map(f => `<code style="background:rgba(96,165,250,0.08);padding:0 3px;margin-right:2px;border-radius:2px;">${escHtml(f)}</code>`).join('')}${taskFilesEditBtn}</div>`
    : '';
  const filesT = hasTaskTouched
    ? `<div style="font-size:9px;margin-top:1px;"><span style="color:#10b981;">✅</span> ${task.files_touched.map(f => `<code style="background:rgba(16,185,129,0.08);padding:0 3px;margin-right:2px;border-radius:2px;">${escHtml(f)}</code>`).join('')}</div>`
    : '';
  return `<div style="display:flex;justify-content:space-between;font-size:10px;color:var(--text2);padding:1px 0;"><span><code>${escHtml(task.id)}</code> ${escHtml(task.title || '')} ${llmBadge}${spawnBadge}${childLink}${verdicts} <span style="opacity:0.7;">— ${escHtml(task.spec || '')}</span>${inlineTaskFilesBtn}${filesP}${filesT}</span>${editBtn}</div>`;
}

// BL191 Q6 (v5.16.0) — per-guardrail verdict badges shown inline on
// stories + tasks. Color-coded by outcome with a tooltip showing
// severity + summary + first issue.
//
// v5.26.6 BL202 polish — badges are now clickable on touch devices
// (tooltips don't work on mobile / Wear OS). Click expands an inline
// drill-down panel with summary, severity, and the full issues list.
// Click again to collapse. Group ID lets us collocate the panel
// directly under the row of badges.
let _verdictDrilldownSeq = 0;
function renderVerdicts(verdicts) {
  if (!verdicts || verdicts.length === 0) return '';
  const colors = { pass: '#10b981', warn: '#f59e0b', block: '#ef4444' };
  const groupId = 'vd-' + (++_verdictDrilldownSeq);
  if (!state._verdictPayloads) state._verdictPayloads = {};
  state._verdictPayloads[groupId] = verdicts;
  const badges = verdicts.map((v, i) => {
    const c = colors[v.outcome] || '#6b7280';
    const tip = (v.summary || '') + (v.severity ? ' [' + v.severity + ']' : '') + (v.issues && v.issues.length ? '\n• ' + v.issues.slice(0, 3).join('\n• ') : '');
    return `<span title="${escHtml(tip)}" onclick="toggleVerdictDrilldown('${groupId}',${i})" style="background:${c};color:#fff;font-size:9px;padding:1px 5px;border-radius:6px;margin-left:2px;cursor:pointer;user-select:none;">${escHtml(v.guardrail || '?')}: ${escHtml(v.outcome || '?')}</span>`;
  }).join('');
  // Drill-down panel sits inline (initially hidden); toggleVerdictDrilldown fills it.
  return ' ' + badges + `<div id="${groupId}-panel" style="display:none;font-size:10px;color:var(--text);background:rgba(255,255,255,0.04);border-left:2px solid var(--border);padding:4px 6px;margin-top:3px;border-radius:3px;"></div>`;
}

window.toggleVerdictDrilldown = function(groupId, idx) {
  const panel = document.getElementById(groupId + '-panel');
  if (!panel) return;
  const verdicts = (state._verdictPayloads || {})[groupId] || [];
  const v = verdicts[idx];
  if (!v) return;
  // If already showing this verdict, collapse. Otherwise replace contents.
  if (panel.dataset.idx === String(idx) && panel.style.display !== 'none') {
    panel.style.display = 'none';
    panel.dataset.idx = '';
    return;
  }
  panel.dataset.idx = String(idx);
  const issues = (v.issues || []).map(s => '<li>' + escHtml(s) + '</li>').join('') || '<li><em style="opacity:0.7;">no issues</em></li>';
  panel.innerHTML =
    `<div style="font-weight:600;margin-bottom:2px;">${escHtml(v.guardrail || '?')} — ${escHtml(v.outcome || '?')}${v.severity ? ' <span style="opacity:0.7;">[' + escHtml(v.severity) + ']</span>' : ''}</div>` +
    (v.summary ? `<div style="margin-bottom:3px;">${escHtml(v.summary)}</div>` : '') +
    `<ul style="margin:0;padding-left:16px;">${issues}</ul>`;
  panel.style.display = 'block';
};

function renderPRDActions(prd) {
  const id = prd.id || '';
  const idJ = JSON.stringify(id);
  const status = prd.status || '';
  // v5.26.3 — operator-reported: every PRD button (Edit, Delete, Run,
  // Approve, …) silently no-op'd. Cause: `onclick="${fn}"` interpolated
  // JSON.stringify outputs that contain literal `"` chars, which closed
  // the outer onclick attribute mid-string. v5.22.0 fixed this for the
  // submitPRDEdit modal but missed renderPRDActions itself. escHtml
  // converts inner `"` → `&quot;`; the browser decodes back when parsing
  // the attribute, so the JS expression remains valid.
  const a = (label, fn, color) => `<button class="btn-secondary" style="font-size:10px;${color ? 'background:' + color + ';color:#fff;' : ''}" onclick="${escHtml(fn)}">${label}</button>`;
  if (prd.is_template) {
    return a('Instantiate', `openPRDInstantiateModal(${idJ})`);
  }
  const btns = [];
  if (status === 'draft' || status === 'revisions_asked') btns.push(a('Decompose', `prdAction(${idJ},'decompose','POST')`));
  // BL203 — Set LLM button is available pre-Run so the operator can pin a backend before approval.
  if (status !== 'running' && status !== 'completed') {
    const cur = JSON.stringify({ backend: prd.backend || '', effort: String(prd.effort || ''), model: prd.model || '' });
    btns.push(a('LLM', `openPRDSetLLMModal(${idJ},${cur})`, ''));
  }
  if (status === 'needs_review' || status === 'revisions_asked') {
    btns.push(a('Approve', `prdAction(${idJ},'approve','POST',{actor:'operator'})`, '#10b981'));
    btns.push(a('Reject', `prdActionPrompt(${idJ},'reject','reason','Rejection reason')`, '#ef4444'));
    btns.push(a('Revise', `prdActionPrompt(${idJ},'request_revision','note','What needs revision?')`, '#f59e0b'));
  }
  if (status === 'approved') btns.push(a('Run', `prdAction(${idJ},'run','POST')`, '#3b82f6'));
  if (status === 'running') btns.push(a('Cancel', `prdAction(${idJ},'','DELETE')`, '#ef4444'));
  // v5.19.0 — full CRUD finally. Edit (title + spec) on any non-running
  // status; Delete (hard remove + descendants) on every status. Both
  // confirm before firing because they're destructive.
  if (status !== 'running') {
    btns.push(a('Edit', `openPRDEditModal(${idJ},${JSON.stringify(prd.title || '')},${JSON.stringify(prd.spec || '')})`, ''));
  }
  btns.push(a('Delete', `confirmPRDDelete(${idJ})`, '#7c2d12'));
  return btns.join('');
}

// v5.19.0 — confirmation prompt + hard-delete REST call.
//
// v5.26.8 — operator-reported: "I get an error when trying to delete
// an autonomous task, and deleting a task with children should
// delete the children". Two improvements:
//  1. Pre-fetch /children so the confirm message names the count
//     of cascading deletions ("and 3 child PRD(s) under it").
//  2. Strip the "Error: " prefix from the apiFetch failure and
//     surface the daemon's actual message in the toast — pre-v5.26.8
//     toasts said "PRD delete failed: Error: prd ... is running ..."
//     which buried the actionable bit under double-prefix noise.
window.confirmPRDDelete = function(id) {
  apiFetch('/api/autonomous/prds/' + encodeURIComponent(id) + '/children')
    .catch(() => ({ children: [] }))
    .then(data => {
      const kids = (data && data.children) || [];
      const nKids = kids.length;
      const runningKid = kids.find(c => c.status === 'running');
      let msg = 'Delete PRD ' + id + '?';
      if (nKids > 0) {
        msg += ' This permanently removes it AND ' + nKids + ' child PRD' + (nKids === 1 ? '' : 's') +
               ' spawned via SpawnPRD' + (runningKid ? ' (one of which is running — daemon will refuse until you Cancel it first)' : '') + '.';
      } else {
        msg += ' This permanently removes it.';
      }
      msg += ' Cancelling first is reversible — deletion is not.';
      if (!window.confirm(msg)) return;
      apiFetch('/api/autonomous/prds/' + encodeURIComponent(id) + '?hard=true', { method: 'DELETE' })
        .then(() => { showToast('PRD ' + id + ' deleted' + (nKids > 0 ? ' (+' + nKids + ' child)' : ''), 'success', 1800); loadPRDPanel(); })
        .catch(err => {
          const raw = String((err && err.message) || err);
          const trimmed = raw.replace(/^Error:\s*/, '');
          showToast('Delete failed: ' + trimmed, 'error', 4500);
        });
    });
};

// v5.19.0 — modal for editing PRD-level title + spec via PATCH.
// v5.22.0 — fix: inline `onclick="submitPRDEdit(${JSON.stringify(id)})"`
// produced double-quotes inside a double-quoted attribute and broke the
// handler. Use HTML-attribute-escape so the embedded quotes survive.
window.openPRDEditModal = function(id, currentTitle, currentSpec) {
  const idAttr = escHtml(JSON.stringify(id)); // inner double-quotes → &quot;
  const html = `
    <div style="padding:14px;">
      <div style="font-weight:600;margin-bottom:8px;">Edit PRD <code>${escHtml(id)}</code></div>
      <label style="display:block;font-size:11px;margin-bottom:4px;">Title</label>
      <input id="prdEditTitle" type="text" class="form-input" style="width:100%;margin-bottom:10px;" value="${escHtml(currentTitle || '')}" placeholder="Short headline" />
      <label style="display:flex;align-items:center;gap:4px;font-size:11px;margin-bottom:4px;">Spec ${micButtonHTML('prdEditSpec')}</label>
      <textarea id="prdEditSpec" class="form-input" style="width:100%;height:140px;font-family:monospace;font-size:12px;" placeholder="Describe the feature in plain English">${escHtml(currentSpec || '')}</textarea>
      <div style="display:flex;justify-content:flex-end;gap:6px;margin-top:10px;">
        <button class="btn-secondary" onclick="document.getElementById('prdModal').remove();">Cancel</button>
        <button class="btn-primary" onclick="submitPRDEdit(${idAttr})">Save</button>
      </div>
    </div>
  `;
  _prdMountModal(html);
};

window.submitPRDEdit = function(id) {
  const title = document.getElementById('prdEditTitle')?.value || '';
  const spec = document.getElementById('prdEditSpec')?.value || '';
  apiFetch('/api/autonomous/prds/' + encodeURIComponent(id), {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ title, spec, actor: 'operator' }),
  }).then(() => {
    document.getElementById('prdModal')?.remove();
    showToast('PRD updated', 'success', 1500);
    loadPRDPanel();
  }).catch(err => showToast('PRD edit failed: ' + String(err), 'error', 3000));
};

function prdAction(id, action, method, body) {
  const url = '/api/autonomous/prds/' + encodeURIComponent(id) + (action ? '/' + action : '');
  const opts = { method, headers: { 'Content-Type': 'application/json' } };
  if (body) opts.body = JSON.stringify(body);
  apiFetch(url, opts).then(() => { showToast('PRD action ok', 'success', 1500); loadPRDPanel(); })
    .catch(err => showToast('PRD action failed: ' + String(err), 'error', 3000));
}
window.prdAction = prdAction;

function prdActionPrompt(id, action, key, prompt) {
  const val = window.prompt(prompt, '');
  if (val === null) return;
  prdAction(id, action, 'POST', { actor: 'operator', [key]: val });
}
window.prdActionPrompt = prdActionPrompt;

// BL203 (v5.4.x) — proper modal helpers replacing the v5.3.0 prompt()
// chains. Each modal opens with backend/effort/model dropdowns wired to
// the new SetPRDLLM / SetTaskLLM endpoints.
function _prdMountModal(html, onSubmit) {
  const existing = document.getElementById('prdModal');
  if (existing) existing.remove();
  const modal = document.createElement('div');
  modal.id = 'prdModal';
  modal.className = 'confirm-modal-overlay';
  modal.innerHTML = `<div class="response-modal" style="max-width:560px;width:92%;">${html}</div>`;
  modal.addEventListener('click', e => { if (e.target === modal) modal.remove(); });
  document.body.appendChild(modal);
  const form = document.getElementById('prdModalForm');
  if (form) form.addEventListener('submit', ev => {
    ev.preventDefault();
    onSubmit();
  });
}
function _prdCloseModal() { const m = document.getElementById('prdModal'); if (m) m.remove(); }

function openPRDCreateModal() {
  // v5.27.0 — operator-reported: backend should only show configured
  // backends; model should list available models for the selected
  // backend; if list isn't available, hide the model selector.
  // Pre-fetch backends + ollama + openwebui model lists in parallel.
  //
  // v5.26.20 — operator-reported: PRDs should be based on directory or
  // profile; PWA New PRD modal needs profile dropdowns. Fetch
  // /api/profiles/{projects,clusters} alongside the backend lists.
  const ensureBackends = state._prdBackends
    ? Promise.resolve()
    : fetch('/api/backends', { headers: tokenHeader() }).then(r => r.ok ? r.json() : null).then(d => { state._prdBackends = (d && d.llm) || []; }).catch(() => {});
  const ensureModels = (state._availableModels !== undefined)
    ? Promise.resolve()
    : Promise.all([
        fetch('/api/ollama/models', { headers: tokenHeader() }).then(r => r.ok ? r.json() : null).catch(() => null),
        fetch('/api/openwebui/models', { headers: tokenHeader() }).then(r => r.ok ? r.json() : null).catch(() => null),
      ]).then(([oll, owui]) => {
        state._availableModels = {};
        if (oll && Array.isArray(oll.models)) state._availableModels.ollama = oll.models.map(m => m.name || m).filter(Boolean);
        else if (Array.isArray(oll)) state._availableModels.ollama = oll.map(m => m.name || m).filter(Boolean);
        if (owui && Array.isArray(owui.data)) state._availableModels.openwebui = owui.data.map(m => m.id || m.name || m).filter(Boolean);
        else if (Array.isArray(owui)) state._availableModels.openwebui = owui.map(m => m.id || m.name || m).filter(Boolean);
      });
  const ensureProfiles = Promise.all([
    fetch('/api/profiles/projects', { headers: tokenHeader() }).then(r => r.ok ? r.json() : null).catch(() => null),
    fetch('/api/profiles/clusters', { headers: tokenHeader() }).then(r => r.ok ? r.json() : null).catch(() => null),
  ]).then(([projs, clusters]) => {
    const projectsArr = Array.isArray(projs) ? projs : (projs && projs.projects) || (projs && projs.profiles) || [];
    const clustersArr = Array.isArray(clusters) ? clusters : (clusters && clusters.clusters) || (clusters && clusters.profiles) || [];
    state._prdProjectProfiles = projectsArr.map(p => p && (p.name || p)).filter(Boolean);
    state._prdClusterProfiles = clustersArr.map(c => c && (c.name || c)).filter(Boolean);
  });
  Promise.all([ensureBackends, ensureModels, ensureProfiles]).then(() => {
    // v5.26.30 — operator-asked: collapse the three separate fields
    // (project_dir / project_profile / cluster_profile) into a single
    // "Profile" dropdown. The first option is the explicit
    // project_dir mode (operator types a path); subsequent options
    // are configured project profiles. When a profile is selected,
    // backend + effort are inherited from the profile's image_pair
    // so we hide those rows; cluster dropdown becomes required.
    const profileOpts = ['<option value="__dir__">— project directory (local checkout) —</option>']
      .concat((state._prdProjectProfiles || []).map(n => `<option value="${escHtml(n)}">${escHtml(n)}</option>`));
    // v5.26.34 — operator clarification: when a project profile is
    // selected, the cluster dropdown's first option is "Local service
    // instance" (empty value = daemon-side clone + local tmux
    // session). A real Cluster Profile is only required when the
    // operator specifically wants to dispatch to a remote cluster.
    const clusterProfileOpts = ['<option value="">— Local service instance (daemon-side) —</option>']
      .concat((state._prdClusterProfiles || []).map(n => `<option value="${escHtml(n)}">${escHtml(n)}</option>`));
    _prdMountModal(`
      <div class="response-modal-header">
        <strong>New PRD</strong>
        <button class="btn-icon" onclick="_prdCloseModal()" title="Close">&#10005;</button>
      </div>
      <form id="prdModalForm" class="response-modal-body" style="display:flex;flex-direction:column;gap:8px;">
        <label style="font-size:11px;color:var(--text2);">Title (optional)</label>
        <input id="prdNewTitle" type="text" class="form-input" placeholder="Short headline" />
        <label style="font-size:11px;color:var(--text2);display:flex;align-items:center;gap:4px;">Spec — describe the feature in plain English ${micButtonHTML('prdNewSpec')}</label>
        <textarea id="prdNewSpec" class="form-input" rows="6" placeholder="Add a CACHE column to /api/stats that surfaces RTK cache hit-rate alongside the existing token-savings card …" style="resize:vertical;font-family:inherit;"></textarea>
        <label style="font-size:11px;color:var(--text2);">Profile</label>
        <select id="prdNewProfile" class="form-select" style="font-size:11px;padding:1px 4px;" onchange="_prdNewProfileChanged()">${profileOpts.join('')}</select>
        <div id="prdNewDirRow">
          <label style="font-size:11px;color:var(--text2);">Project directory</label>
          <!-- v5.26.46 — operator-asked: directory selector like New
               Session, with "+ New folder" affordance. Reuses the
               existing dir-browser pattern; #selectedDirDisplay +
               #dirBrowser are singletons in the DOM and the modals
               (new-session vs. new-PRD) are mutually exclusive
               by view, so collision is fine. -->
          <div class="dir-picker">
            <span id="selectedDirDisplay" class="dir-display dir-display-clickable" onclick="openDirBrowser()" title="Click to browse">~/</span>
          </div>
          <div id="dirBrowser" class="dir-browser" style="display:none">
            <div id="dirBrowserContent"></div>
          </div>
        </div>
        <div id="prdNewClusterRow" style="display:none;">
          <label style="font-size:11px;color:var(--text2);">Cluster</label>
          <select id="prdNewClusterProfile" class="form-select" style="font-size:11px;padding:1px 4px;">${clusterProfileOpts.join('')}</select>
        </div>
        <div id="prdNewBackendRow" style="display:grid;grid-template-columns:1fr 1fr 1fr;gap:6px;">
          <div><label style="font-size:11px;color:var(--text2);">Backend</label>${renderBackendSelect('prdNewBackend', '', 'updatePRDNewModelField()')}</div>
          <div><label style="font-size:11px;color:var(--text2);">Effort</label>${renderEffortSelect('prdNewEffort', '', '')}</div>
          <div id="prdNewModelWrap" style="display:none;"><label style="font-size:11px;color:var(--text2);">Model (optional)</label><div id="prdNewModelInner"></div></div>
        </div>
        <div style="display:flex;gap:6px;justify-content:flex-end;">
          <button type="button" class="btn-secondary" onclick="_prdCloseModal()">Cancel</button>
          <button type="submit" class="btn-secondary" style="background:var(--accent2);color:#fff;">Create</button>
        </div>
      </form>
    `, () => {
      const spec = document.getElementById('prdNewSpec').value.trim();
      if (!spec) { showToast('Spec required', 'error', 2000); return; }
      const profileSel = document.getElementById('prdNewProfile').value;
      const usingProfile = profileSel && profileSel !== '__dir__';
      const projectProfile = usingProfile ? profileSel : '';
      const clusterProfile = usingProfile ? document.getElementById('prdNewClusterProfile').value : '';
      // v5.26.46 — pull from the dir-picker. newSessionState.selectedDir
      // is set by selectDir() (shared with New Session); when nothing has
      // been selected, fall back to the display span's textContent (the
      // "~/" placeholder, which we treat as empty).
      let projectDir = '';
      if (!usingProfile) {
        const sel = (typeof newSessionState !== 'undefined' && newSessionState.selectedDir) || '';
        const disp = document.getElementById('selectedDirDisplay');
        const dispTxt = disp ? disp.textContent.trim() : '';
        projectDir = sel || (dispTxt && dispTxt !== '~/' ? dispTxt : '');
      }
      // v5.26.34 — operator clarification: empty cluster_profile
      // means "local service instance" (daemon-side clone + local
      // tmux). Only validate that a cluster pick was made if the
      // operator is dispatching to a remote cluster — and that's
      // simply whatever the operator picks. So no required-field
      // check on cluster_profile when a project_profile is selected.
      // Dir mode still requires a path.
      if (!usingProfile) {
        if (!projectDir) {
          showToast('Enter a project directory', 'error', 2500);
          return;
        }
      }
      const body = {
        spec,
        title: document.getElementById('prdNewTitle').value.trim(),
        project_dir: projectDir,
        project_profile: projectProfile,
        cluster_profile: clusterProfile,
        // Backend/effort only set when running in dir mode — profile
        // mode inherits the worker LLM from the profile's image_pair.
        backend: usingProfile ? '' : document.getElementById('prdNewBackend').value,
        effort: usingProfile ? '' : document.getElementById('prdNewEffort').value,
      };
      // v5.27.0 — model field is dynamic (input or select inside
      // prdNewModelInner) and may be hidden entirely when no model
      // list is available for the selected backend.
      const modelEl = document.getElementById('prdNewModelInner')?.querySelector('input,select');
      const model = modelEl ? modelEl.value.trim() : '';
      apiFetch('/api/autonomous/prds', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      }).then(prd => {
        // PRD-level LLM (backend/effort already in create payload — model
        // and any consolidation goes through set_llm so the audit trail
        // gets the full triple).
        if (model || body.backend || body.effort) {
          return apiFetch('/api/autonomous/prds/' + encodeURIComponent(prd.id) + '/set_llm', {
            method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ backend: body.backend, effort: body.effort, model, actor: 'operator' }),
          });
        }
      }).then(() => { showToast('PRD created', 'success', 1500); _prdCloseModal(); loadPRDPanel(); })
        .catch(err => showToast('Create failed: ' + String(err), 'error', 3000));
    });
  });
}
window.openPRDCreateModal = openPRDCreateModal;

// v5.26.30 — toggle dir-vs-cluster row when the unified Profile
// dropdown changes. Profile mode hides the path input + the
// backend/effort row (profile carries the worker LLM); dir mode
// hides the cluster dropdown.
function _prdNewProfileChanged() {
  const sel = document.getElementById('prdNewProfile');
  if (!sel) return;
  const usingProfile = sel.value && sel.value !== '__dir__';
  const dirRow = document.getElementById('prdNewDirRow');
  const clusterRow = document.getElementById('prdNewClusterRow');
  const backendRow = document.getElementById('prdNewBackendRow');
  if (dirRow) dirRow.style.display = usingProfile ? 'none' : '';
  if (clusterRow) clusterRow.style.display = usingProfile ? '' : 'none';
  if (backendRow) backendRow.style.display = usingProfile ? 'none' : '';
}
window._prdNewProfileChanged = _prdNewProfileChanged;

// v5.27.0 — operator-reported: New PRD model field should list models
// available for the selected backend. Hide entirely when the backend
// has no known model list.
//
// v5.26.8 — generalized into refreshLLMModelField(wrapId, innerId,
// backendId, currentValue) so the same dynamic-dropdown pattern works
// in the per-PRD and per-task LLM modals too. updatePRDNewModelField
// stays as a thin wrapper for backwards compat with the existing
// `onchange` hook in renderBackendSelect.
window.refreshLLMModelField = function(wrapId, innerId, backendId, currentValue) {
  const wrap = document.getElementById(wrapId);
  const inner = document.getElementById(innerId);
  const backendEl = document.getElementById(backendId);
  if (!wrap || !inner || !backendEl) return;
  const backend = backendEl.value || '';
  const models = (state._availableModels || {})[backend] || [];
  if (!backend || models.length === 0) {
    wrap.style.display = 'none';
    inner.innerHTML = '';
    return;
  }
  wrap.style.display = '';
  // Preserve a model that the operator already had set when toggling
  // backends — surface as "(custom: <name>)" if it isn't in the list.
  let foundCurrent = false;
  const opts = ['<option value="">(backend default)</option>'];
  for (const m of models) {
    const sel = (currentValue && m === currentValue) ? ' selected' : '';
    if (sel) foundCurrent = true;
    opts.push(`<option value="${escHtml(m)}"${sel}>${escHtml(m)}</option>`);
  }
  if (currentValue && !foundCurrent) {
    opts.push(`<option value="${escHtml(currentValue)}" selected>(custom) ${escHtml(currentValue)}</option>`);
  }
  inner.innerHTML = `<select class="form-select" style="font-size:12px;width:100%;">${opts.join('')}</select>`;
};

window.updatePRDNewModelField = function() {
  refreshLLMModelField('prdNewModelWrap', 'prdNewModelInner', 'prdNewBackend', '');
};

// v5.26.8 — ensure /api/ollama/models + /api/openwebui/models are
// in state._availableModels. Used by the per-PRD + per-task LLM
// modals to populate the dropdown on open. openPRDCreateModal already
// pre-fetched these inline; the helper hoists the same logic so the
// edit-task and set-llm modals don't have to duplicate it.
window.ensureLLMModelLists = function() {
  if (state._availableModels !== undefined) return Promise.resolve();
  return Promise.all([
    fetch('/api/ollama/models', { headers: tokenHeader() }).then(r => r.ok ? r.json() : null).catch(() => null),
    fetch('/api/openwebui/models', { headers: tokenHeader() }).then(r => r.ok ? r.json() : null).catch(() => null),
  ]).then(([oll, owui]) => {
    state._availableModels = {};
    if (oll && Array.isArray(oll.models)) state._availableModels.ollama = oll.models.map(m => m.name || m).filter(Boolean);
    else if (Array.isArray(oll)) state._availableModels.ollama = oll.map(m => m.name || m).filter(Boolean);
    if (owui && Array.isArray(owui.data)) state._availableModels.openwebui = owui.data.map(m => m.id || m.name || m).filter(Boolean);
    else if (Array.isArray(owui)) state._availableModels.openwebui = owui.map(m => m.id || m.name || m).filter(Boolean);
  });
};

function openPRDEditTaskModal(prdID, taskID, currentSpec, currentBackend, currentEffort, currentModel) {
  // v5.26.8 — populate the model dropdown as soon as the modal mounts,
  // and refresh it every time the operator switches backends.
  ensureLLMModelLists().then(() => {
    _prdMountModal(`
    <div class="response-modal-header">
      <strong>Edit task ${escHtml(taskID)}</strong>
      <button class="btn-icon" onclick="_prdCloseModal()" title="Close">&#10005;</button>
    </div>
    <form id="prdModalForm" class="response-modal-body" style="display:flex;flex-direction:column;gap:8px;">
      <label style="font-size:11px;color:var(--text2);display:flex;align-items:center;gap:4px;">Spec ${micButtonHTML('prdEditSpec')}</label>
      <textarea id="prdEditSpec" class="form-input" rows="6" style="resize:vertical;font-family:inherit;">${escHtml(currentSpec || '')}</textarea>
      <div style="font-size:10px;color:var(--text2);">Per-task LLM override — empty inherits PRD then global.</div>
      <div style="display:grid;grid-template-columns:1fr 1fr 1fr;gap:6px;">
        <div><label style="font-size:11px;color:var(--text2);">Backend</label>${renderBackendSelect('prdEditBackend', currentBackend || '', `refreshLLMModelField('prdEditModelWrap','prdEditModelInner','prdEditBackend',${JSON.stringify(currentModel || '')})`)}</div>
        <div><label style="font-size:11px;color:var(--text2);">Effort</label>${renderEffortSelect('prdEditEffort', currentEffort || '', '')}</div>
        <div id="prdEditModelWrap" style="display:none;"><label style="font-size:11px;color:var(--text2);">Model</label><div id="prdEditModelInner"></div></div>
      </div>
      <div style="display:flex;gap:6px;justify-content:flex-end;">
        <button type="button" class="btn-secondary" onclick="_prdCloseModal()">Cancel</button>
        <button type="submit" class="btn-secondary" style="background:var(--accent2);color:#fff;">Save</button>
      </div>
    </form>
  `, () => {
    const newSpec = document.getElementById('prdEditSpec').value;
    const backend = document.getElementById('prdEditBackend').value;
    const effort = document.getElementById('prdEditEffort').value;
    const modelEl = document.getElementById('prdEditModelInner')?.querySelector('input,select');
    const model = modelEl ? modelEl.value.trim() : '';
    const calls = [];
    if (newSpec !== currentSpec) {
      calls.push(apiFetch('/api/autonomous/prds/' + encodeURIComponent(prdID) + '/edit_task', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ task_id: taskID, new_spec: newSpec, actor: 'operator' }),
      }));
    }
    if (backend !== (currentBackend || '') || effort !== (currentEffort || '') || model !== (currentModel || '')) {
      calls.push(apiFetch('/api/autonomous/prds/' + encodeURIComponent(prdID) + '/set_task_llm', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ task_id: taskID, backend, effort, model, actor: 'operator' }),
      }));
    }
    if (calls.length === 0) { _prdCloseModal(); return; }
    Promise.all(calls)
      .then(() => { showToast('Task updated', 'success', 1500); _prdCloseModal(); loadPRDPanel(); })
      .catch(err => showToast('Save failed: ' + String(err), 'error', 3000));
  });
  // Populate the model dropdown for the current backend now that
  // the modal is mounted in the DOM.
  refreshLLMModelField('prdEditModelWrap', 'prdEditModelInner', 'prdEditBackend', currentModel || '');
  });
}
window.openPRDEditTaskModal = openPRDEditTaskModal;

// v5.26.32 — operator-asked: "i don't see a story review or
// approval or story edit option." The story edit modal mirrors the
// task edit modal but only takes title + description (no LLM
// override at the story level — that's a future phase 3 item per
// docs/plans/2026-04-27-v6-prep-backlog.md).
function openPRDEditStoryModal(prdID, storyID, currentTitle, currentDescription) {
  _prdMountModal(`
    <div class="response-modal-header">
      <strong>Edit story ${escHtml(storyID)}</strong>
      <button class="btn-icon" onclick="_prdCloseModal()" title="Close">&#10005;</button>
    </div>
    <form id="prdModalForm" class="response-modal-body" style="display:flex;flex-direction:column;gap:8px;">
      <label style="font-size:11px;color:var(--text2);">Title</label>
      <input id="prdEditStoryTitle" type="text" class="form-input" value="${escHtml(currentTitle || '')}" />
      <label style="font-size:11px;color:var(--text2);display:flex;align-items:center;gap:4px;">Description ${micButtonHTML('prdEditStoryDesc')}</label>
      <textarea id="prdEditStoryDesc" class="form-input" rows="5" style="resize:vertical;font-family:inherit;">${escHtml(currentDescription || '')}</textarea>
      <div style="display:flex;gap:6px;justify-content:flex-end;">
        <button type="button" class="btn-secondary" onclick="_prdCloseModal()">Cancel</button>
        <button type="submit" class="btn-secondary" style="background:var(--accent2);color:#fff;">Save</button>
      </div>
    </form>
  `, () => {
    const newTitle = document.getElementById('prdEditStoryTitle').value.trim();
    const newDesc = document.getElementById('prdEditStoryDesc').value;
    if (newTitle === (currentTitle || '') && newDesc === (currentDescription || '')) {
      _prdCloseModal();
      return;
    }
    if (!newTitle && !newDesc) {
      showToast('Story needs at least a title or description', 'error', 2000);
      return;
    }
    apiFetch('/api/autonomous/prds/' + encodeURIComponent(prdID) + '/edit_story', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        story_id: storyID,
        new_title: newTitle === (currentTitle || '') ? '' : newTitle,
        new_description: newDesc === (currentDescription || '') ? '' : newDesc,
        actor: 'operator',
      }),
    })
      .then(() => { showToast('Story updated', 'success', 1500); _prdCloseModal(); loadPRDPanel(); })
      .catch(err => showToast('Save failed: ' + String(err), 'error', 3000));
  });
}
window.openPRDEditStoryModal = openPRDEditStoryModal;

function openPRDSetLLMModal(prdID, current) {
  // v5.26.8 — same dynamic model dropdown pattern as the New PRD and
  // Edit Task modals.
  ensureLLMModelLists().then(() => {
    _prdMountModal(`
    <div class="response-modal-header">
      <strong>PRD-level worker LLM</strong>
      <button class="btn-icon" onclick="_prdCloseModal()" title="Close">&#10005;</button>
    </div>
    <form id="prdModalForm" class="response-modal-body" style="display:flex;flex-direction:column;gap:8px;">
      <div style="font-size:11px;color:var(--text2);">Tasks without a per-task override inherit these values. Empty = fall back to the global session.llm_backend default.</div>
      <div style="display:grid;grid-template-columns:1fr 1fr 1fr;gap:6px;">
        <div><label style="font-size:11px;color:var(--text2);">Backend</label>${renderBackendSelect('prdSetBackend', current.backend || '', `refreshLLMModelField('prdSetModelWrap','prdSetModelInner','prdSetBackend',${JSON.stringify(current.model || '')})`)}</div>
        <div><label style="font-size:11px;color:var(--text2);">Effort</label>${renderEffortSelect('prdSetEffort', current.effort || '', '')}</div>
        <div id="prdSetModelWrap" style="display:none;"><label style="font-size:11px;color:var(--text2);">Model</label><div id="prdSetModelInner"></div></div>
      </div>
      <div style="display:flex;gap:6px;justify-content:flex-end;">
        <button type="button" class="btn-secondary" onclick="_prdCloseModal()">Cancel</button>
        <button type="submit" class="btn-secondary" style="background:var(--accent2);color:#fff;">Save</button>
      </div>
    </form>
  `, () => {
    const modelEl = document.getElementById('prdSetModelInner')?.querySelector('input,select');
    const body = {
      backend: document.getElementById('prdSetBackend').value,
      effort: document.getElementById('prdSetEffort').value,
      model: modelEl ? modelEl.value.trim() : '',
      actor: 'operator',
    };
    apiFetch('/api/autonomous/prds/' + encodeURIComponent(prdID) + '/set_llm', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }).then(() => { showToast('PRD LLM updated', 'success', 1500); _prdCloseModal(); loadPRDPanel(); })
      .catch(err => showToast('Save failed: ' + String(err), 'error', 3000));
  });
  // Populate the model dropdown for the current backend now that the
  // modal is mounted.
  refreshLLMModelField('prdSetModelWrap', 'prdSetModelInner', 'prdSetBackend', current.model || '');
  });
}
window.openPRDSetLLMModal = openPRDSetLLMModal;
window._prdCloseModal = _prdCloseModal;

function openPRDInstantiateModal(templateID) {
  const varsCSV = window.prompt('Template vars (k=v,k=v):', '') || '';
  const vars = {};
  varsCSV.split(',').forEach(kv => { const i = kv.indexOf('='); if (i > 0) vars[kv.slice(0,i).trim()] = kv.slice(i+1).trim(); });
  apiFetch('/api/autonomous/prds/' + encodeURIComponent(templateID) + '/instantiate', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ vars, actor: 'operator' }),
  }).then(() => { showToast('Template instantiated', 'success', 1500); loadPRDPanel(); })
    .catch(err => showToast('Instantiate failed: ' + String(err), 'error', 3000));
}
window.openPRDInstantiateModal = openPRDInstantiateModal;

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

// Comms tab config fields — web server, MCP server, proxy resilience
const COMMS_CONFIG_FIELDS = [
  { id: 'websrv', section: 'Web Server', docs: 'operations.md', fields: [
    { key: 'server.enabled', label: 'Enabled', type: 'toggle' },
    { key: 'server.host', label: 'Bind interface', type: 'interface_select' },
    { key: 'server.port', label: 'Port', type: 'number' },
    { key: 'server.tls', label: 'TLS enabled', type: 'toggle' },
    { key: 'server.tls_port', label: 'TLS port', type: 'number', placeholder: '8443' },
    { key: 'server.tls_auto_generate', label: 'TLS auto-generate cert', type: 'toggle' },
    { key: 'server.tls_cert', label: 'TLS cert path', type: 'text' },
    { key: 'server.tls_key', label: 'TLS key path', type: 'text' },
    { key: '_tls_install', label: 'Install cert on phone', type: 'html',
      html: `<div style="font-size:11px;padding:8px 0;">
        <a href="/api/cert?format=der" style="color:var(--accent2);text-decoration:underline;font-weight:600;" download="datawatch-ca.crt">&#128274; Download CA Certificate (.crt)</a>
        <span style="margin-left:8px;"><a href="/api/cert" style="color:var(--text2);text-decoration:underline;font-size:10px;" download="datawatch-ca.pem">PEM format</a></span>
        <details style="color:var(--text2);font-size:10px;margin-top:6px;">
          <summary style="cursor:pointer;color:var(--accent2);">Install instructions</summary>
          <div style="margin-top:4px;line-height:1.6;">
            <b>Android:</b><br/>
            1. Tap .crt link above to download<br/>
            2. Open downloaded file — or go to Settings &rarr; Security &amp; privacy &rarr; More security &amp; privacy &rarr; Encryption &amp; credentials &rarr; Install a certificate &rarr; CA certificate<br/>
            3. Select the downloaded datawatch-ca.crt<br/>
            4. Confirm install<br/><br/>
            <b>iPhone/iPad:</b><br/>
            1. Tap PEM format link above to download<br/>
            2. Go to Settings &rarr; General &rarr; VPN &amp; Device Management &rarr; tap the downloaded profile &rarr; Install<br/>
            3. Then Settings &rarr; General &rarr; About &rarr; Certificate Trust Settings &rarr; enable full trust for the datawatch certificate<br/><br/>
            <b>After installing:</b> Remove old home screen shortcut, visit https site, tap &#8942; &rarr; Install app
          </div>
        </details>
      </div>` },
    { key: 'server.channel_port', label: 'Channel port (0=random)', type: 'number' },
  ]},
  { id: 'mcpsrv', section: 'MCP Server', docs: 'mcp.md', fields: [
    { key: 'mcp.enabled', label: 'Enabled (stdio)', type: 'toggle' },
    { key: 'mcp.sse_enabled', label: 'SSE enabled (HTTP)', type: 'toggle' },
    { key: 'mcp.sse_host', label: 'SSE bind interface', type: 'interface_select' },
    { key: 'mcp.sse_port', label: 'SSE port', type: 'number' },
    { key: 'mcp.tls_enabled', label: 'TLS enabled', type: 'toggle' },
    { key: 'mcp.tls_auto_generate', label: 'TLS auto-generate cert', type: 'toggle' },
    { key: 'mcp.tls_cert', label: 'TLS cert path', type: 'text' },
    { key: 'mcp.tls_key', label: 'TLS key path', type: 'text' },
  ]},
];

const GENERAL_CONFIG_FIELDS = [
  { id: 'dw', section: 'Datawatch', docs: 'howto/setup-and-install.md', fields: [
    { key: 'session.log_level', label: 'Log level', type: 'select', options: ['info','debug','warn','error'] },
    { key: 'server.auto_restart_on_config', label: 'Auto-restart on config save', type: 'toggle' },
    { key: 'session.llm_backend', label: 'Default LLM backend', type: 'llm_select' },
  ]},
  { id: 'autoupdate', section: 'Auto-Update', docs: 'howto/daemon-operations.md', fields: [
    { key: 'update.enabled', label: 'Enabled', type: 'toggle' },
    { key: 'update.schedule', label: 'Schedule', type: 'select', options: ['hourly','daily','weekly'] },
    { key: 'update.time_of_day', label: 'Time of day (HH:MM)', type: 'text' },
  ]},
  { id: 'sess', section: 'Session', docs: 'howto/chat-and-llm-quickstart.md', fields: [
    { key: 'session.max_sessions', label: 'Max concurrent sessions', type: 'number' },
    { key: 'session.input_idle_timeout', label: 'Input idle timeout (sec)', type: 'number' },
    { key: 'session.tail_lines', label: 'Tail lines', type: 'number' },
    { key: 'session.alert_context_lines', label: 'Alert context lines', type: 'number', placeholder: '10' },
    { key: 'session.default_project_dir', label: 'Default project dir', type: 'dir_browse' },
    { key: 'session.root_path', label: 'File browser root path', type: 'dir_browse' },
    { key: 'session.console_cols', label: 'Default console width (cols)', type: 'number', placeholder: '80' },
    { key: 'session.console_rows', label: 'Default console height (rows)', type: 'number', placeholder: '24' },
    { key: 'server.recent_session_minutes', label: 'Recent session visibility (min)', type: 'number' },
    { key: 'session.auto_git_init', label: 'Auto git init', type: 'toggle' },
    { key: 'session.auto_git_commit', label: 'Auto git commit', type: 'toggle' },
    { key: 'session.kill_sessions_on_exit', label: 'Kill sessions on exit', type: 'toggle' },
    { key: 'session.mcp_max_retries', label: 'MCP auto-retry limit', type: 'number' },
    { key: 'session.schedule_settle_ms', label: 'Scheduled command settle (ms) — B30', type: 'number' },
    { key: 'server.suppress_active_toasts', label: 'Suppress toasts for active session', type: 'toggle' },
  ]},
  // v5.19.0 — RTK section moved out of General (operator: "should only
  // be in LLM"). The fuller version with auto_update + update_check_interval
  // lives in LLM_CONFIG_FIELDS at the same id='rtk'.
  { id: 'pipeline', section: 'Pipelines (Session Chaining)', docs: 'howto/pipeline-chaining.md', fields: [
    { key: 'pipeline.max_parallel', label: 'Max parallel tasks (0 = default 3)', type: 'number', placeholder: '3' },
    { key: 'pipeline.default_backend', label: 'Default backend (empty = session default)', type: 'text' },
  ]},
  // v4.0.1 — feature toggles for autonomous / plugins / orchestrator.
  // Each feature's full surface is REST + MCP + CLI per parity rule;
  // these Settings cards give the operator a one-click enable + links
  // to the operator docs. Field-level config stays YAML/REST/CLI.
  { id: 'autonomous', section: 'Autonomous PRD decomposition', docs: 'howto/autonomous-planning.md', fields: [
    { key: 'autonomous.enabled', label: 'Enable autonomous loop', type: 'toggle' },
    { key: 'autonomous.poll_interval_seconds', label: 'Poll interval (sec)', type: 'number', placeholder: '30' },
    { key: 'autonomous.max_parallel_tasks', label: 'Max parallel tasks', type: 'number', placeholder: '3' },
    // v5.26.16 — operator-reported: backend fields should be the
    // same dropdown as the New PRD modal (enabled+available, no
    // shell), with a paired model dropdown that refreshes on backend
    // change.
    { key: 'autonomous.decomposition_backend', label: 'Decomposition backend', type: 'llm_backend', pairedModelKey: 'autonomous.decomposition_model' },
    { key: 'autonomous.decomposition_model', label: 'Decomposition model', type: 'llm_model', backendKey: 'autonomous.decomposition_backend' },
    { key: 'autonomous.verification_backend', label: 'Verification backend', type: 'llm_backend', pairedModelKey: 'autonomous.verification_model' },
    { key: 'autonomous.verification_model', label: 'Verification model', type: 'llm_model', backendKey: 'autonomous.verification_backend' },
    { key: 'autonomous.auto_fix_retries', label: 'Auto-fix retries', type: 'number', placeholder: '1' },
    { key: 'autonomous.security_scan', label: 'Run security scan before commit', type: 'toggle' },
    // BL191 Q4 (v5.9.0) — recursive child PRDs.
    { key: 'autonomous.max_recursion_depth', label: 'Max recursion depth (0 disables SpawnPRD)', type: 'number', placeholder: '5' },
    { key: 'autonomous.auto_approve_children', label: 'Auto-approve spawned child PRDs', type: 'toggle' },
    // BL191 Q6 (v5.10.0) — guardrails-at-all-levels. Comma-separated
    // guardrail names (rules, security, release-readiness, docs-diagrams-architecture).
    { key: 'autonomous.per_task_guardrails', label: 'Per-task guardrails', type: 'text', placeholder: 'rules, security', csv: true },
    { key: 'autonomous.per_story_guardrails', label: 'Per-story guardrails', type: 'text', placeholder: 'release-readiness', csv: true },
    // Phase 3 (v5.26.62) — per-story approval gate. When ON, PRD
    // approval transitions every story to "awaiting_approval" and
    // the runner skips those until the operator approves each
    // individually via the per-story Approve button on the PRD card.
    { key: 'autonomous.per_story_approval', label: 'Per-story approval gate (each story needs explicit approve)', type: 'toggle' },
  ]},
  // v5.26.16 — operator-reported: PRD-DAG orchestrator section
  // belongs above Plugin framework. Orchestrator is a workflow-level
  // concern (PRD composition + guardrails) operators reach for next
  // after Autonomous; Plugin framework is a daemon-extensibility
  // concern operators set up rarely.
  { id: 'orchestrator', section: 'PRD-DAG orchestrator', docs: 'howto/prd-dag-orchestrator.md', fields: [
    { key: 'orchestrator.enabled', label: 'Enable PRD-DAG orchestrator', type: 'toggle' },
    { key: 'orchestrator.guardrail_backend', label: 'Guardrail backend', type: 'llm_backend', pairedModelKey: 'orchestrator.guardrail_model' },
    { key: 'orchestrator.guardrail_model', label: 'Guardrail model', type: 'llm_model', backendKey: 'orchestrator.guardrail_backend' },
    { key: 'orchestrator.guardrail_timeout_ms', label: 'Guardrail timeout (ms)', type: 'number', placeholder: '120000' },
    { key: 'orchestrator.max_parallel_prds', label: 'Max parallel PRDs', type: 'number', placeholder: '2' },
  ]},
  { id: 'plugins', section: 'Plugin framework', docs: 'agents.md', fields: [
    { key: 'plugins.enabled', label: 'Enable subprocess plugin framework', type: 'toggle' },
    { key: 'plugins.dir', label: 'Plugin discovery directory', type: 'text', placeholder: '~/.datawatch/plugins' },
    { key: 'plugins.timeout_ms', label: 'Invocation timeout (ms)', type: 'number', placeholder: '2000' },
  ]},
  { id: 'whisper', section: 'Voice Input (Whisper)', docs: 'howto/voice-input.md', fields: [
    { key: 'whisper.enabled', label: 'Enable voice transcription', type: 'toggle' },
    { key: 'whisper.backend', label: 'Backend — openai / ollama / openwebui reuse the endpoint + API key already configured for that LLM backend', type: 'select', options: ['whisper','openai','openai_compat','openwebui','ollama'] },
    { key: 'whisper.model', label: 'Model (tiny/base/small/medium/large; or remote model name)', type: 'text', placeholder: 'base' },
    // v5.28.3 — operator-asked: don't duplicate language between the
    // datawatch identity card and the Whisper card. Transcription
    // language tracks the PWA UI language (Settings → About → Language)
    // by default — `setLocaleOverride()` syncs `whisper.language` on
    // every PWA picker change. The field stays a YAML/REST/MCP/CLI
    // config key (parity rule), but the PWA Whisper card surfaces it
    // read-only with a pointer to the canonical control. Operators
    // who need a different transcription language than UI language
    // can still set it via `datawatch config set whisper.language <code>`
    // or directly in YAML.
    { key: 'whisper.language', label: 'Language — tracks PWA language (Settings → About). Override via CLI/YAML if needed.', type: 'readonly' },
    { key: 'whisper.venv_path', label: 'Python venv path (local whisper only)', type: 'text', placeholder: '.venv' },
    { key: 'whisper.test_button', label: 'Test transcription endpoint', type: 'button', action: 'testWhisperBackend' },
  ]},
];

// LLM tab config fields — memory and profiles rendered separately on the LLM tab
const LLM_CONFIG_FIELDS = [
  { id: 'memory', section: 'Episodic Memory', docs: 'howto/cross-agent-memory.md', fields: [
    { key: 'memory.enabled', label: 'Enable memory system', type: 'toggle' },
    { key: 'memory.backend', label: 'Storage backend', type: 'select', options: ['sqlite','postgres'] },
    { key: 'memory.embedder', label: 'Embedding provider', type: 'select', options: ['ollama','openai'] },
    { key: 'memory.embedder_model', label: 'Embedding model', type: 'text', placeholder: 'nomic-embed-text' },
    { key: 'memory.embedder_host', label: 'Embedder host', type: 'text' },
    { key: 'memory.top_k', label: 'Search results (top-K)', type: 'number' },
    { key: 'memory.auto_save', label: 'Auto-save session summaries', type: 'toggle' },
    { key: 'memory.learnings_enabled', label: 'Extract task learnings', type: 'toggle' },
    { key: 'memory.storage_mode', label: 'Storage mode', type: 'select', options: ['summary','verbatim'] },
    { key: 'memory.entity_detection', label: 'Auto entity detection', type: 'toggle' },
    { key: 'memory.session_awareness', label: 'Inject memory instructions into sessions', type: 'toggle' },
    { key: 'memory.session_broadcast', label: 'Broadcast session summaries to active sessions', type: 'toggle' },
    { key: 'memory.auto_hooks', label: 'Auto-install Claude Code hooks per session', type: 'toggle' },
    { key: 'memory.hook_save_interval', label: 'Hook save interval (messages)', type: 'number' },
    { key: 'memory.retention_days', label: 'Retention days (0 = forever)', type: 'number' },
    { key: 'memory.db_path', label: 'SQLite database path', type: 'text', placeholder: '~/.datawatch/memory.db' },
    { key: 'memory.postgres_url', label: 'PostgreSQL URL (enterprise)', type: 'text', placeholder: 'postgres://user:pass@host/db' },
  ]},
  { id: 'rtk', section: 'RTK (Token Savings)', docs: 'rtk-integration.md', fields: [
    { key: 'rtk.enabled', label: 'Enable RTK integration', type: 'toggle' },
    { key: 'rtk.binary', label: 'RTK binary path', type: 'text', placeholder: 'rtk' },
    { key: 'rtk.show_savings', label: 'Show token savings in stats', type: 'toggle' },
    { key: 'rtk.auto_init', label: 'Auto-init hooks if missing', type: 'toggle' },
    { key: 'rtk.auto_update', label: 'Auto-update RTK binary', type: 'toggle' },
    { key: 'rtk.update_check_interval', label: 'Update check interval (sec, 0=off)', type: 'number', placeholder: '86400' },
    { key: 'rtk.discover_interval', label: 'Discover interval (sec, 0=off)', type: 'number', placeholder: '0' },
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

function loadCommsConfig() {
  Promise.all([
    fetch('/api/config', { headers: tokenHeader() }).then(r => r.ok ? r.json() : null),
    fetch('/api/interfaces', { headers: tokenHeader() }).then(r => r.ok ? r.json() : [])
  ]).then(([cfg, interfaces]) => {
    if (!cfg) return;
    state._interfaces = interfaces || [];
    for (const sec of COMMS_CONFIG_FIELDS) {
      const el = document.getElementById('ccfg_' + sec.id);
      if (!el) continue;
      let html = '';
      for (const f of sec.fields) {
        const parts = f.key.split('.');
        const val = parts.reduce((o, k) => (o && o[k] !== undefined) ? o[k] : '', cfg);
        if (f.type === 'html') {
          html += f.html || '';
        } else if (f.type === 'button') {
          // BL191/BL189 (v5.2.0) — settings-side action button. Calls a
          // named function on the window object so handlers can live
          // alongside other PWA code.
          html += `<div class="settings-row" style="justify-content:space-between;align-items:center;">
            <div class="settings-label">${escHtml(f.label)}</div>
            <button class="btn-secondary" style="font-size:12px;" onclick="(window['${escHtml(f.action || '')}']||function(){showToast('Action ${escHtml(f.action || '')} not wired','error',2000);})()">Run</button>
          </div>`;
        } else if (f.type === 'select') {
          const opts = (f.options || []).map(o => `<option value="${escHtml(o)}" ${String(val) === o ? 'selected' : ''}>${escHtml(o)}</option>`).join('');
          html += `<div class="settings-row" style="justify-content:space-between;">
            <div class="settings-label">${escHtml(f.label)}</div>
            <select class="form-select general-cfg-input" onchange="saveGeneralField('${f.key}', this.value)">${opts}</select>
          </div>`;
        } else if (f.type === 'interface_select') {
          // v5.23.0 — operator-reported: comms interface_select wasn't
          // rendering items (treated state._interfaces strings but the
          // entries are {addr, label, name} objects), AND it was single-
          // select where operators want multi-select for binding to
          // multiple interfaces. Now mirrors the GENERAL_CONFIG_FIELDS
          // multi-select with the connected-iface protection.
          const currentVals = String(val || '0.0.0.0').split(',').map(s => s.trim());
          const ifaces = (interfaces && interfaces.length > 0) ? interfaces : [
            { addr: '0.0.0.0', label: '0.0.0.0 (all interfaces)' },
            { addr: '127.0.0.1', label: '127.0.0.1 (localhost)' },
          ];
          const connIf = typeof _resolveConnectedInterface === 'function' ? _resolveConnectedInterface() : null;
          const checkboxes = ifaces.map(iface => {
            const checked = currentVals.includes(iface.addr);
            const isConnected = connIf && iface.addr === connIf.addr;
            const badge = isConnected
              ? ' <span style="color:var(--success);font-size:10px;">(connected — auto-protected)</span>'
              : '';
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
        } else if (f.type === 'toggle') {
          const checked = !!val;
          html += `<div class="settings-row" style="justify-content:space-between;align-items:center;">
            <div class="settings-label">${escHtml(f.label)}</div>
            <label class="toggle-switch">
              <input type="checkbox" ${checked ? 'checked' : ''} onchange="saveGeneralField('${f.key}', this.checked)" />
              <span class="toggle-slider"></span>
            </label>
          </div>`;
        } else if (f.type === 'readonly') {
          // v5.28.3 — show the effective config value with no inline edit
          // control. Used to point operators at the canonical control
          // when a value is derived/synced from somewhere else (e.g.
          // whisper.language tracks the PWA UI language picker).
          html += `<div class="settings-row" style="justify-content:space-between;">
            <div class="settings-label">${escHtml(f.label)}</div>
            <div class="settings-value" style="font-family:monospace;color:var(--text2);">${escHtml(String(val || '—'))}</div>
          </div>`;
        } else {
          html += `<div class="settings-row" style="justify-content:space-between;">
            <div class="settings-label">${escHtml(f.label)}</div>
            <input type="${f.type === 'number' ? 'number' : 'text'}" class="form-input general-cfg-input" value="${escHtml(String(val || ''))}"
              placeholder="${escHtml(f.placeholder || '')}"
              onchange="saveGeneralField('${f.key}', this.value)" />
          </div>`;
        }
      }
      el.innerHTML = html;
    }
  }).catch(() => {});
}

function loadLLMTabConfig() {
  apiFetch('/api/config').then(cfg => {
    if (!cfg) return;
    // Resolve default for embedder_host from ollama.host
    const ollamaHost = cfg.ollama?.host || 'http://localhost:11434';
    for (const sec of LLM_CONFIG_FIELDS) {
      const el = document.getElementById('llmCfg_' + sec.id);
      if (!el) continue;
      let html = '';
      for (const f of sec.fields) {
        // Navigate config path, preserving false/0 values
        const parts = f.key.split('.');
        let val = cfg;
        for (const k of parts) {
          if (val && typeof val === 'object' && k in val) val = val[k];
          else { val = undefined; break; }
        }
        // For embedder_host, show the actual ollama.host as default
        let effectiveVal = val;
        let effectivePlaceholder = f.placeholder || '';
        if (f.key === 'memory.embedder_host') {
          effectivePlaceholder = ollamaHost;
          if (!val) effectiveVal = ollamaHost;
        }

        if (f.type === 'toggle') {
          const checked = !!val;
          const saveAction = f.key === 'memory.enabled'
            ? `testAndEnableMemory(this)`
            : `saveGeneralField('${f.key}', this.checked)`;
          html += `<div class="settings-row" style="justify-content:space-between;align-items:center;">
            <div class="settings-label">${escHtml(f.label)}</div>
            <div style="display:flex;align-items:center;gap:6px;">
              ${f.key === 'memory.enabled' ? `<button class="btn-secondary" style="font-size:10px;padding:2px 6px;" onclick="testMemoryConnection()">Test</button>` : ''}
              <label class="toggle-switch">
                <input type="checkbox" ${checked ? 'checked' : ''} onchange="${saveAction}" />
                <span class="toggle-slider"></span>
              </label>
            </div>
          </div>`;
        } else if (f.type === 'select') {
          const opts = f.options.map(o => `<option value="${escHtml(o)}" ${String(val || '') === o ? 'selected' : ''}>${escHtml(o)}</option>`).join('');
          html += `<div class="settings-row" style="justify-content:space-between;">
            <div class="settings-label">${escHtml(f.label)}</div>
            <select class="form-select general-cfg-input" onchange="saveGeneralField('${f.key}', this.value)">${opts}</select>
          </div>`;
        } else {
          const displayVal = effectiveVal !== undefined && effectiveVal !== null ? String(effectiveVal) : '';
          // v5.26.8 — CSV-list inputs get a [✎] button next to them
          // that opens a textarea-based modal (one item per line + mic
          // input when whisper is enabled). The auto-generated input
          // ID lets the modal find the live input on Save.
          const inputId = 'gcfg-input-' + f.key.replace(/[^a-z0-9_-]/gi, '-');
          const csvBtn = f.csv ? csvExpandButtonHTML(inputId, f.label) : '';
          html += `<div class="settings-row" style="justify-content:space-between;">
            <div class="settings-label">${escHtml(f.label)}</div>
            <span style="display:flex;align-items:center;flex:1;justify-content:flex-end;">
              <input id="${inputId}" type="${f.type === 'number' ? 'number' : 'text'}" class="form-input general-cfg-input" value="${escHtml(displayVal)}"
                placeholder="${escHtml(effectivePlaceholder)}"
                onchange="saveGeneralField('${f.key}', this.value)" />
              ${csvBtn}
            </span>
          </div>`;
        }
      }
      el.innerHTML = html;
    }
  }).catch(() => {});
}

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
          } else if (f.type === 'button') {
            // v5.19.0 — operator-reported regression: the whisper.test_button
            // (BL289 v5.4.0) rendered as <input type="button"> empty box
            // because loadGeneralConfig fell through to the generic else.
            // Now mirrors the loadCommsConfig button branch.
            html += `<div class="settings-row" style="justify-content:space-between;align-items:center;">
              <div class="settings-label">${escHtml(f.label)}</div>
              <button class="btn-secondary" style="font-size:12px;" onclick="(window['${escHtml(f.action || '')}']||function(){showToast('Action ${escHtml(f.action || '')} not wired','error',2000);})()">Run</button>
            </div>`;
          } else if (f.type === 'llm_backend') {
            // v5.26.16 — same dynamic-dropdown pattern as the New PRD
            // modal: enabled+available backends only, shell excluded.
            // The paired model field below subscribes to changes here.
            const inputId = 'gcfg-llmbk-' + f.key.replace(/\W+/g,'-');
            const modelSelector = f.pairedModelKey ? `'gcfg-llmbk-${f.pairedModelKey.replace(/\W+/g,'-')}-wrap'` : "''";
            const modelInner = f.pairedModelKey ? `'gcfg-llmbk-${f.pairedModelKey.replace(/\W+/g,'-')}-inner'` : "''";
            const opts = ['<option value="">(inherit)</option>'];
            (state._prdBackends || enabledBackends.map(n => ({name:n, enabled:true}))).forEach(b => {
              const name = (typeof b === 'string') ? b : (b && b.name);
              if (!name) return;
              if (NON_LLM_BACKENDS.has(name)) return;
              if (typeof b !== 'string' && b.enabled === false) return;
              opts.push(`<option value="${escHtml(name)}" ${String(val) === name ? 'selected' : ''}>${escHtml(name)}</option>`);
            });
            if (val && !opts.some(o => o.includes(`value="${escHtml(String(val))}"`))) {
              opts.push(`<option value="${escHtml(String(val))}" selected>${escHtml(String(val))} (not configured)</option>`);
            }
            const backendCurrent = JSON.stringify(String(val || ''));
            const onchange = `saveGeneralField('${f.key}', this.value); refreshLLMModelField(${modelSelector}, ${modelInner}, '${inputId}', '');`;
            html += `<div class="settings-row" style="justify-content:space-between;">
              <div class="settings-label">${escHtml(f.label)}</div>
              <select id="${inputId}" class="form-select general-cfg-input" onchange="${onchange}">${opts.join('')}</select>
            </div>`;
          } else if (f.type === 'llm_model') {
            // v5.26.16 — paired model dropdown. Reads from
            // state._availableModels keyed by the sibling backend's
            // value. Hidden when no models known for the backend.
            const wrapId = 'gcfg-llmbk-' + f.key.replace(/\W+/g,'-') + '-wrap';
            const innerId = 'gcfg-llmbk-' + f.key.replace(/\W+/g,'-') + '-inner';
            const backendInputId = f.backendKey ? ('gcfg-llmbk-' + f.backendKey.replace(/\W+/g,'-')) : '';
            const onchange = `saveGeneralField('${f.key}', (this.querySelector('select,input')||{value:''}).value)`;
            html += `<div id="${wrapId}" class="settings-row" style="justify-content:space-between;display:none;">
              <div class="settings-label">${escHtml(f.label)}</div>
              <div id="${innerId}" onchange="${onchange}" style="flex:0 0 200px;"></div>
            </div>`;
            // Defer the populate to after the DOM is in place; ensureLLMModelLists then refreshLLMModelField.
            setTimeout(() => {
              ensureLLMModelLists().then(() => refreshLLMModelField(wrapId, innerId, backendInputId, String(val || '')));
            }, 0);
          } else {
            html += `<div class="settings-row" style="justify-content:space-between;">
              <div class="settings-label">${escHtml(f.label)}</div>
              <input type="${f.type}" class="form-input general-cfg-input" value="${escHtml(String(val || ''))}"
                ${f.placeholder ? 'placeholder="' + escHtml(f.placeholder) + '"' : ''}
                onchange="saveGeneralField('${f.key}', ${f.type === 'number' ? 'Number(this.value)' : 'this.value'})" />
            </div>`;
          }
        }
        // PWA-only preferences appended to the dw section. Stored in
        // localStorage; not in the YAML config (per-browser choice).
        if (sec.id === 'dw') {
          const linksOn = localStorage.getItem('cs_show_docs_links') !== '0';
          html += `<div class="settings-row" style="justify-content:space-between;">
            <div class="settings-label">Show inline doc links</div>
            <label class="toggle-switch">
              <input type="checkbox" ${linksOn ? 'checked' : ''} onchange="toggleDocsLinksPref(this.checked)" />
              <span class="toggle-slider"></span>
            </label>
          </div>`;
        }
        el.innerHTML = html;
      }
    })
    .catch(() => {});
}

window.toggleDocsLinksPref = function(on) {
  localStorage.setItem('cs_show_docs_links', on ? '1' : '0');
  // Re-render Settings so the inline links appear/disappear immediately.
  if (state.activeView === 'settings') renderSettingsView();
};

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

// v5.26.56 — Operator-asked: "the test whisper i expected it would
// open a dialog with a mic button to test, not just backend test."
// Replaces the old silent-WAV health check (kept under the hood as
// a fallback) with an interactive recording dialog. Operator clicks
// 🎤 → speaks → transcript appears in the dialog. Fails closed
// (forces whisper.enabled=false) only when no transcript at all
// comes back from a non-empty recording.
function testWhisperBackend() {
  const overlay = document.createElement('div');
  overlay.id = 'whisperTestOverlay';
  overlay.style.cssText = 'position:fixed;inset:0;background:rgba(0,0,0,0.6);z-index:1000;display:flex;align-items:center;justify-content:center;padding:16px;';
  overlay.innerHTML = `
    <div style="background:var(--bg);border:1px solid var(--border);border-radius:10px;padding:18px;max-width:480px;width:100%;display:flex;flex-direction:column;gap:12px;">
      <div style="display:flex;align-items:center;justify-content:space-between;">
        <strong style="font-size:14px;">Test transcription</strong>
        <button class="btn-icon" onclick="document.getElementById('whisperTestOverlay').remove()" title="Close">&#10005;</button>
      </div>
      <div style="font-size:12px;color:var(--text2);">
        Click 🎤 to start recording, click again to stop. The transcribed text appears below.
        Verifies the configured Whisper backend end-to-end (mic → /api/voice/transcribe → text).
      </div>
      <div style="display:flex;gap:8px;align-items:center;">
        <button id="whisperTestMicBtn" class="btn-secondary" style="font-size:24px;width:64px;height:64px;border-radius:50%;" onclick="_whisperTestToggle(this)" title="Click to start / stop">&#127908;</button>
        <div id="whisperTestStatus" style="font-size:12px;color:var(--text2);">idle</div>
      </div>
      <textarea id="whisperTestTranscript" class="form-input" rows="4" placeholder="(transcript will appear here)" style="resize:vertical;font-family:inherit;" readonly></textarea>
      <div style="display:flex;gap:6px;justify-content:flex-end;">
        <button class="btn-secondary" onclick="testWhisperBackendQuick()">Run silent-WAV health check</button>
        <button class="btn-secondary" onclick="document.getElementById('whisperTestOverlay').remove()">Done</button>
      </div>
    </div>`;
  document.body.appendChild(overlay);
}
window.testWhisperBackend = testWhisperBackend;

// Reuses the same recorder + /api/voice/transcribe path as the
// inline mic buttons. Writes into the readonly textarea inside the
// modal.
window._whisperTestToggle = async function(btn) {
  const status = document.getElementById('whisperTestStatus');
  const target = document.getElementById('whisperTestTranscript');
  if (!target || !status) return;
  if (state.voice && state.voice.recorder && state.voice.recorder.state === 'recording' &&
      state.voice._genericTarget === 'whisperTestTranscript') {
    state.voice.recorder.stop();
    status.textContent = 'transcribing…';
    return;
  }
  if (!navigator.mediaDevices || typeof MediaRecorder === 'undefined') {
    status.textContent = 'browser does not support MediaRecorder';
    return;
  }
  let stream;
  try { stream = await navigator.mediaDevices.getUserMedia({ audio: true }); }
  catch (err) { status.textContent = 'mic permission denied'; return; }
  let mime = '';
  for (const cand of ['audio/webm;codecs=opus', 'audio/webm', 'audio/ogg;codecs=opus', 'audio/mp4']) {
    if (MediaRecorder.isTypeSupported && MediaRecorder.isTypeSupported(cand)) { mime = cand; break; }
  }
  const rec = new MediaRecorder(stream, mime ? { mimeType: mime } : undefined);
  state.voice = { recorder: rec, chunks: [], _genericTarget: 'whisperTestTranscript' };
  rec.ondataavailable = e => { if (e.data && e.data.size > 0) state.voice.chunks.push(e.data); };
  rec.onstop = async () => {
    stream.getTracks().forEach(t => t.stop());
    btn.classList.remove('recording');
    btn.innerHTML = '&#127908;';
    const blob = new Blob(state.voice.chunks, { type: mime || 'audio/webm' });
    state.voice = { recorder: null, chunks: [], sessionId: null };
    if (blob.size === 0) { status.textContent = 'no audio captured'; return; }
    try {
      const ext = mime.includes('mp4') ? '.m4a' : mime.includes('ogg') ? '.ogg' : '.webm';
      const fd = new FormData();
      fd.append('audio', blob, 'voice' + ext);
      fd.append('ts_client', String(Date.now()));
      const tok = localStorage.getItem('cs_token') || '';
      const headers = tok ? { 'Authorization': 'Bearer ' + tok } : {};
      const t0 = performance.now();
      const res = await fetch('/api/voice/transcribe', { method: 'POST', headers, body: fd });
      if (!res.ok) { status.textContent = 'transcribe failed: ' + (await res.text() || res.status); return; }
      const data = await res.json();
      const transcript = (data && data.transcript) || '';
      const ms = Math.round(performance.now() - t0);
      target.value = transcript || '(empty transcript — backend returned no text)';
      status.textContent = transcript
        ? `ok (${ms}ms, ${transcript.length} chars)`
        : 'transcribed empty — backend may be misconfigured';
      // Don't auto-disable whisper here — the operator opened this
      // dialog deliberately. They can disable manually if it really
      // is broken.
    } catch (err) {
      status.textContent = 'transcribe error: ' + err.message;
    }
  };
  btn.classList.add('recording');
  btn.innerHTML = '&#9632;';
  status.textContent = 'recording — click again to stop';
  rec.start();
};

// The pre-v5.26.56 silent-WAV health check, kept reachable for
// quick "does the backend respond at all" verification. Differs
// from the new flow: doesn't need a real microphone, doesn't
// produce a transcript, but does fail-disable on error.
window.testWhisperBackendQuick = function() {
  showToast('Testing transcription endpoint…', 'info', 1500);
  apiFetch('/api/voice/test', { method: 'POST' })
    .then(data => {
      if (data && data.ok) {
        showToast('Voice backend OK (' + (data.latency_ms || 0) + 'ms)', 'success', 3000);
      } else {
        showToast('Voice backend failed: ' + ((data && data.error) || 'unknown'), 'error', 6000);
        saveGeneralField('whisper.enabled', false);
      }
    })
    .catch(err => {
      showToast('Voice backend failed: ' + String(err), 'error', 6000);
      saveGeneralField('whisper.enabled', false);
    });
};

function checkForUpdate() {
  const el = document.getElementById('aboutUpdate');
  if (el) el.innerHTML = '<span style="color:var(--text2);font-size:12px;">Checking…</span>';
  // v5.27.4 (datawatch#25) — use the daemon's read-only check endpoint
  // instead of hitting api.github.com directly. One source of truth +
  // no CORS issues + goes through the daemon's auth.
  apiFetch('/api/update/check')
    .then(data => {
      if (!data) { if (el) el.innerHTML = '<span style="color:var(--error);">Check failed</span>'; return; }
      const current = data.current_version || '';
      const latest = data.latest_version || '';
      if (!el) return;
      if (data.status === 'update_available') {
        el.innerHTML = `<span style="color:var(--warning,#f59e0b);font-size:12px;">Update available: v${latest} (current: v${current})</span>` +
          ` <button class="btn-secondary" style="font-size:11px;margin-left:6px;" onclick="runUpdate()">Update</button>`;
      } else {
        el.innerHTML = '<span style="color:var(--success,#22c55e);font-size:12px;">Up to date (v' + current + ')</span>';
      }
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
  { key:'output_mode', label:'Output mode', type:'select_inline', options:['terminal','log','chat'], placeholder:'terminal' },
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
    { key:'claude_auto_accept_disclaimer', label:'Auto-accept startup disclaimer', type:'checkbox', section:'session' },
    { key:'permission_mode', label:'Default permission mode', type:'select', options:['','plan','acceptEdits','auto','bypassPermissions','dontAsk','default'], section:'session' },
    { key:'default_effort', label:'Default effort', type:'select', options:['','quick','normal','thorough'], section:'session' },
    { key:'fallback_chain', label:'Fallback chain (comma-separated profiles)', type:'text', placeholder:'claude-personal,gemini-backup', section:'session' },
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

// ── Proxy Resilience Settings ──────────────────────────────────────────────────

function loadProxySettings() {
  const el = document.getElementById('proxySettings');
  if (!el) return;
  apiFetch('/api/config').then(cfg => {
    const p = cfg?.proxy || {};
    const fields = [
      { key: 'proxy.enabled', label: 'Enabled', val: p.enabled || false, type: 'bool', desc: 'Enable proxy aggregation mode' },
      { key: 'proxy.health_interval', label: 'Health Interval', val: p.health_interval || 30, type: 'num', desc: 'Seconds between health checks' },
      { key: 'proxy.request_timeout', label: 'Request Timeout', val: p.request_timeout || 10, type: 'num', desc: 'Seconds per request' },
      { key: 'proxy.offline_queue_size', label: 'Queue Size', val: p.offline_queue_size || 100, type: 'num', desc: 'Max queued commands per server' },
      { key: 'proxy.circuit_breaker_threshold', label: 'CB Threshold', val: p.circuit_breaker_threshold || 3, type: 'num', desc: 'Failures before breaker trips' },
      { key: 'proxy.circuit_breaker_reset', label: 'CB Reset', val: p.circuit_breaker_reset || 30, type: 'num', desc: 'Seconds before retry' },
    ];
    let html = '<div style="font-size:10px;color:var(--text2);padding:0 0 6px;">Connection pooling, circuit breaker, and offline queuing for remote servers.</div>';
    for (const f of fields) {
      html += `<div class="settings-row" style="justify-content:space-between;align-items:center;gap:8px;">
        <div><span class="settings-label" style="font-size:12px;">${f.label}</span><br><span style="font-size:10px;color:var(--text2);">${f.desc}</span></div>
        <div style="display:flex;align-items:center;gap:4px;">`;
      if (f.type === 'bool') {
        html += `<label class="toggle-switch"><input type="checkbox" ${f.val ? 'checked' : ''} onchange="toggleProxySetting('${f.key}',this.checked)" /><span class="toggle-slider"></span></label>`;
      } else {
        html += `<input type="number" value="${f.val}" style="width:60px;font-size:12px;padding:2px 4px;background:var(--bg);border:1px solid var(--border);color:var(--text);border-radius:4px;"
          onchange="updateProxySetting('${f.key}',this.value)" />`;
      }
      html += `</div></div>`;
    }
    el.innerHTML = html;
  }).catch(() => { if (el) el.textContent = 'Config unavailable'; });
}

function toggleProxySetting(key, val) {
  apiFetch('/api/config', { method: 'PUT', body: JSON.stringify({ key, value: val }) })
    .then(() => { showToast('Saved', 'success', 1500); loadProxySettings(); })
    .catch(e => showToast('Save failed: ' + e.message, 'error'));
}

function updateProxySetting(key, val) {
  const num = parseInt(val, 10);
  if (isNaN(num) || num < 0) return;
  apiFetch('/api/config', { method: 'PUT', body: JSON.stringify({ key, value: num }) })
    .then(() => showToast('Saved', 'success', 1500))
    .catch(e => showToast('Save failed: ' + e.message, 'error'));
}

// ── Memory Stats & Browser ────────────────────────────────────────────────────

function loadMemoryStats() {
  const el = document.getElementById('memoryStatsPanel');
  if (!el) return;
  apiFetch('/api/memory/stats').then(data => {
    if (!data || !data.enabled) {
      el.innerHTML = '<div style="color:var(--text2);">Memory not enabled. Enable in Settings → General → Episodic Memory.</div>';
      return;
    }
    el.innerHTML = `
      <div style="display:grid;grid-template-columns:repeat(auto-fill,minmax(140px,1fr));gap:8px;">
        <div class="stat-card"><div class="stat-value">${data.total_count}</div><div class="stat-label">Total Memories</div></div>
        <div class="stat-card"><div class="stat-value">${data.manual_count}</div><div class="stat-label">Manual</div></div>
        <div class="stat-card"><div class="stat-value">${data.session_count}</div><div class="stat-label">Session</div></div>
        <div class="stat-card"><div class="stat-value">${data.learning_count}</div><div class="stat-label">Learnings</div></div>
        <div class="stat-card"><div class="stat-value">${data.chunk_count}</div><div class="stat-label">Chunks</div></div>
        <div class="stat-card"><div class="stat-value">${formatBytes(data.db_size_bytes || 0)}</div><div class="stat-label">DB Size</div></div>
      </div>`;
  }).catch(() => { if (el) el.textContent = 'Memory stats unavailable'; });
}

function exportMemories() {
  window.open('/api/memory/export', '_blank');
}

// v5.27.0 — mempalace alignment PWA actions. Each calls its REST
// endpoint and renders the result inline below the button. See
// docs/memory.md for the full surface table + behaviour notes.
function memorySweepStale(dryRun) {
  const days = parseInt(document.getElementById('memSweepDays').value, 10) || 90;
  const out = document.getElementById('memSweepResult');
  if (!dryRun && !confirm(`Apply eviction? This will delete every row that hasn't surfaced in any query and is older than ${days} days. Manual + pinned rows are exempt.`)) {
    return;
  }
  out.textContent = 'Running…';
  apiFetch('/api/memory/sweep_stale', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ older_than_days: days, dry_run: !!dryRun }),
  }).then(d => {
    out.textContent = `${dryRun ? 'Dry-run' : 'Applied'}: ${d.candidates} candidates, ${d.deleted || 0} deleted`;
  }).catch(e => { out.textContent = 'Failed: ' + e.message; });
}
window.memorySweepStale = memorySweepStale;

function memorySpellCheck() {
  const text = document.getElementById('memSpellInput').value.trim();
  const out = document.getElementById('memSpellResult');
  if (!text) { out.textContent = 'Enter text first'; return; }
  out.textContent = 'Running…';
  apiFetch('/api/memory/spellcheck', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ text }),
  }).then(d => {
    if (d.count === 0) { out.textContent = 'No suggestions'; return; }
    out.innerHTML = (d.suggestions || []).map(s =>
      `<code style="background:rgba(96,165,250,0.08);padding:1px 4px;margin-right:4px;border-radius:3px;">${escHtml(s.original)} → ${escHtml(s.proposed)}</code>`
    ).join('');
  }).catch(e => { out.textContent = 'Failed: ' + e.message; });
}
window.memorySpellCheck = memorySpellCheck;

function memoryExtractFacts() {
  const text = document.getElementById('memExtractInput').value.trim();
  const out = document.getElementById('memExtractResult');
  if (!text) { out.textContent = 'Enter text first'; return; }
  out.textContent = 'Running…';
  apiFetch('/api/memory/extract_facts', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ text }),
  }).then(d => {
    if (d.count === 0) { out.textContent = 'No triples'; return; }
    out.innerHTML = (d.triples || []).map(t =>
      `<code style="background:rgba(124,58,237,0.08);padding:1px 4px;margin-right:4px;border-radius:3px;">(${escHtml(t.subject)} ${escHtml(t.predicate)} ${escHtml(t.object)}) <span style="opacity:0.6;">${escHtml(t.source)} ${(t.confidence||0).toFixed(2)}</span></code>`
    ).join('');
  }).catch(e => { out.textContent = 'Failed: ' + e.message; });
}
window.memoryExtractFacts = memoryExtractFacts;

function memorySchemaVersion() {
  const out = document.getElementById('memSchemaResult');
  out.textContent = 'Loading…';
  // Schema version surfaces via /api/memory/stats.schema_version when
  // the backend reports it. Fall back to a dedicated probe path the
  // operator can read directly.
  apiFetch('/api/memory/stats').then(d => {
    out.textContent = 'schema_version: ' + (d.schema_version || '(not reported by this backend)');
  }).catch(e => { out.textContent = 'Failed: ' + e.message; });
}
window.memorySchemaVersion = memorySchemaVersion;

function listMemories() {
  const el = document.getElementById('memoryBrowserList');
  if (!el) return;
  el.innerHTML = '<div style="color:var(--text2);">Loading…</div>';
  const role = document.getElementById('memoryRoleFilter')?.value || '';
  const sinceDays = document.getElementById('memorySinceFilter')?.value || '';
  let url = '/api/memory/list?n=50';
  if (role) url += '&role=' + encodeURIComponent(role);
  if (sinceDays) {
    const d = new Date(); d.setDate(d.getDate() - parseInt(sinceDays));
    url += '&since=' + d.toISOString();
  }
  apiFetch(url).then(memories => {
    if (!memories || memories.length === 0) {
      el.innerHTML = '<div style="color:var(--text2);">No memories stored.</div>';
      return;
    }
    el.innerHTML = memories.map(m => {
      const date = m.created_at ? new Date(m.created_at).toLocaleDateString() : '';
      const content = (m.content || '').length > 200 ? m.content.slice(0, 200) + '…' : (m.content || '');
      const sim = m.similarity ? ` <span style="color:var(--accent2);">[${Math.round(m.similarity*100)}%]</span>` : '';
      return `<div class="settings-row" style="justify-content:space-between;align-items:flex-start;gap:8px;">
        <div style="flex:1;min-width:0;">
          <span style="font-size:10px;color:var(--text2);">#${m.id} ${m.role} ${date}${sim}</span>
          <div style="font-size:12px;white-space:pre-wrap;word-break:break-word;max-height:60px;overflow:hidden;">${escHtml(content)}</div>
        </div>
        <button class="btn-icon" style="font-size:10px;color:var(--error);" onclick="deleteMemory(${m.id})" title="Delete">&#128465;</button>
      </div>`;
    }).join('');
  }).catch(() => { if (el) el.textContent = 'Failed to load memories'; });
}

function searchMemories() {
  const input = document.getElementById('memorySearchInput');
  const el = document.getElementById('memoryBrowserList');
  if (!input || !el) return;
  const query = input.value.trim();
  if (!query) { listMemories(); return; }
  el.innerHTML = '<div style="color:var(--text2);">Searching…</div>';
  apiFetch(`/api/memory/search?q=${encodeURIComponent(query)}`).then(memories => {
    if (!memories || memories.length === 0) {
      el.innerHTML = '<div style="color:var(--text2);">No matches found.</div>';
      return;
    }
    el.innerHTML = memories.map(m => {
      const content = (m.content || '').length > 200 ? m.content.slice(0, 200) + '…' : (m.content || '');
      const sim = m.similarity ? ` [${Math.round(m.similarity*100)}%]` : '';
      return `<div class="settings-row" style="justify-content:space-between;align-items:flex-start;gap:8px;">
        <div style="flex:1;min-width:0;">
          <span style="font-size:10px;color:var(--text2);">#${m.id} ${m.role}${sim}</span>
          <div style="font-size:12px;white-space:pre-wrap;word-break:break-word;max-height:60px;overflow:hidden;">${escHtml(content)}</div>
        </div>
        <button class="btn-icon" style="font-size:10px;color:var(--error);" onclick="deleteMemory(${m.id})" title="Delete">&#128465;</button>
      </div>`;
    }).join('');
  }).catch(() => { if (el) el.textContent = 'Search failed'; });
}

function deleteMemory(id) {
  apiFetch('/api/memory/delete', { method: 'POST', body: JSON.stringify({ id }) })
    .then(() => { showToast('Deleted memory #' + id, 'success', 1500); listMemories(); loadMemoryStats(); })
    .catch(e => showToast('Delete failed: ' + e.message, 'error'));
}

function formatBytes(b) {
  if (b < 1024) return b + ' B';
  if (b < 1024*1024) return (b/1024).toFixed(1) + ' KB';
  return (b/(1024*1024)).toFixed(1) + ' MB';
}

// ── Remote Servers ─────────────────────────────────────────────────────────────

function loadServers() {
  const el = document.getElementById('serverStatus');
  if (!el) return;
  // Fetch server list and health in parallel
  Promise.all([
    fetch('/api/servers', { headers: tokenHeader() }).then(r => r.ok ? r.json() : null),
    fetch('/api/servers/health', { headers: tokenHeader() }).then(r => r.ok ? r.json() : []).catch(() => []),
  ]).then(([servers, health]) => {
    if (!servers) { el.textContent = 'Servers unavailable'; return; }
    state.servers = servers;
    if (servers.length === 0) { el.textContent = 'No servers available.'; return; }
    // Build health lookup: name → health info
    const healthMap = {};
    (health || []).forEach(h => { healthMap[h.name] = h; });
    // Default active server is 'local' when state.activeServer is null
    const effectiveActive = state.activeServer || 'local';
    const rows = servers.map(sv => {
      const auth = sv.has_auth ? '🔒' : '🔓';
      const isActive = effectiveActive === sv.name;
      const activeLabel = isActive ? ' <span style="color:var(--accent);font-size:11px;">(active)</span>' : '';
      // Health badge for remote servers
      const h = healthMap[sv.name];
      let healthBadge = '';
      if (h) {
        if (h.breaker_open) {
          healthBadge = ' <span style="color:#ef4444;font-size:11px;" title="Circuit breaker open: ' + escHtml(h.last_error || '') + '">&#9679; down</span>';
        } else if (h.healthy) {
          healthBadge = ' <span style="color:#10b981;font-size:11px;">&#9679; healthy</span>';
        } else {
          healthBadge = ' <span style="color:#f59e0b;font-size:11px;" title="' + escHtml(h.last_error || '') + '">&#9679; degraded</span>';
        }
        if (h.queued_cmds > 0) {
          healthBadge += ` <span style="color:var(--text2);font-size:10px;">(${h.queued_cmds} queued)</span>`;
        }
      }
      // Remote PWA link for non-local servers
      const pwaLink = sv.name !== 'local' && sv.enabled
        ? ` <a href="/remote/${encodeURIComponent(sv.name)}/" target="_blank" style="font-size:10px;color:var(--text2);text-decoration:underline;" title="Open remote PWA">PWA</a>`
        : '';
      return `<div class="settings-row" style="justify-content:space-between">
        <div><strong>${escHtml(sv.name)}</strong>${activeLabel}${healthBadge} ${auth}${pwaLink}<br><span style="font-size:12px;color:var(--text2)">${escHtml(sv.url)}</span></div>
        <button class="btn-secondary" style="font-size:12px;padding:4px 8px" onclick="selectServer('${escHtml(sv.name)}')">${isActive ? 'Connected' : 'Select'}</button>
      </div>`;
    }).join('');
    el.innerHTML = rows;
  }).catch(() => { if (el) el.textContent = 'Servers unavailable'; });
}

function selectServer(name) {
  const prev = state.activeServer;
  state.activeServer = (state.activeServer === name) ? null : name;
  loadServers();
  // Reconnect WS to the new server (or back to local)
  if (state.activeServer !== prev) {
    state.sessions = [];
    state.channelReady = {};
    state.outputBuffer = {};
    if (state.ws) { state.ws.close(); state.ws = null; }
    connect();
    showToast(state.activeServer ? `Connected to: ${state.activeServer}` : 'Connected to local server', 'info');
  }
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
      <button class="btn-secondary" style="font-size:12px;padding:4px 16px;" onclick="document.getElementById('confirmModal').remove()">${escHtml(t('action_no'))}</button>
      <button class="btn-stop" style="font-size:12px;padding:4px 16px;" id="confirmYesBtn">${escHtml(t('action_yes'))}</button>
    </div>
  </div>`;
  modal.addEventListener('click', e => { if (e.target === modal) modal.remove(); });
  document.body.appendChild(modal);
  const yesBtn = document.getElementById('confirmYesBtn');
  yesBtn.onclick = () => { modal.remove(); onConfirm(); };
  yesBtn.focus(); // Auto-select so Enter confirms immediately
}

function showResponseViewer(sessionId) {
  const existing = document.getElementById('responseViewer');
  if (existing) existing.remove();

  // BL178: always fetch the live response from the API. The cached
  // copy in state.lastResponse[sessionId] is populated from WS
  // `response` events on completion, but never invalidated — so on a
  // long-running browser tab it can be days stale (operator reproduced
  // on session 787e). We render the cached value first for instant
  // feedback, then overwrite with the fresh fetch when it returns.
  const cached = state.lastResponse && state.lastResponse[sessionId];
  if (cached) {
    renderResponseModal(sessionId, cached, /* loading= */ true);
  }
  apiFetch(`/api/sessions/response?id=${encodeURIComponent(sessionId)}`)
    .then(data => {
      const fresh = data.response || '(no response captured)';
      // Update the cache so the next click is also fresh.
      state.lastResponse = state.lastResponse || {};
      state.lastResponse[sessionId] = fresh;
      renderResponseModal(sessionId, fresh, /* loading= */ false);
    })
    .catch(() => {
      if (!cached) renderResponseModal(sessionId, '(failed to load response)', false);
    });
}

function renderResponseModal(sessionId, content, loading) {
  // BL178: when called with loading=true, we're showing a (possibly
  // stale) cached value while a fresh fetch is in flight. We patch
  // the body in place on the second call instead of tearing the
  // modal down — preserves scroll position and avoids flicker.
  const existing = document.getElementById('responseViewer');
  if (existing && !loading) {
    const body = existing.querySelector('#responseContent');
    if (body) {
      body.innerHTML = formatResponseContent(content);
      const stale = existing.querySelector('#responseStaleBadge');
      if (stale) stale.remove();
      return;
    }
  }
  if (existing) existing.remove();
  const sess = state.sessions.find(s => s.full_id === sessionId);
  const label = sess ? (sess.name || sess.id) : sessionId;
  const staleBadge = loading
    ? `<span id="responseStaleBadge" style="font-size:10px;color:var(--text2);margin-left:8px;">(updating…)</span>`
    : '';

  const modal = document.createElement('div');
  modal.id = 'responseViewer';
  modal.className = 'confirm-modal-overlay';
  modal.innerHTML = `<div class="response-modal">
    <div class="response-modal-header">
      <strong>Response — ${escHtml(label)}</strong>${staleBadge}
      <div style="display:flex;gap:6px;">
        <button class="btn-icon" onclick="copyResponseText()" title="Copy to clipboard" style="font-size:12px;">&#128203;</button>
        <button class="btn-icon" onclick="document.getElementById('responseViewer').remove()" title="Close">&#10005;</button>
      </div>
    </div>
    <div class="response-modal-body" id="responseContent">${formatResponseContent(content)}</div>
  </div>`;
  modal.addEventListener('click', e => { if (e.target === modal) modal.remove(); });
  document.body.appendChild(modal);
}

function formatResponseContent(text) {
  if (!text) return '<em style="color:var(--text2);">(no response captured)</em>';
  // Basic rich formatting: code blocks, bold, links
  let html = escHtml(text);
  // Code blocks: ```...```
  html = html.replace(/```(\w*)\n([\s\S]*?)```/g, '<pre class="response-code"><code>$2</code></pre>');
  // Inline code: `...`
  html = html.replace(/`([^`]+)`/g, '<code class="response-inline-code">$1</code>');
  // Bold: **...**
  html = html.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
  // Line breaks
  html = html.replace(/\n/g, '<br>');
  return html;
}

function copyResponseText() {
  const el = document.getElementById('responseContent');
  if (!el) return;
  const text = el.innerText || el.textContent;
  navigator.clipboard.writeText(text).then(() => showToast('Copied to clipboard', 'success', 1500));
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
  // v5.26.8 — auto badge removed entirely; header status dot is the
  // single source of WS-state truth.
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

  // Header search-icon toggle — moved from the sessions card to the
  // top header bar (next to the status light) per operator feedback
  // so it's reachable without scrolling.
  const searchBtn = document.getElementById('headerSearchBtn');
  if (searchBtn) searchBtn.addEventListener('click', () => {
    // v5.26.46 — sessions and autonomous tabs both gate their
    // filter rows behind this button. Sessions persist state via
    // _filtersCollapsed; autonomous toggles its own filter row
    // inline (no localStorage — short-lived view, less risk of
    // surprise on revisit).
    if (state.activeView === 'autonomous') {
      _toggleAutonomousFilters();
      return;
    }
    if (state._filtersCollapsed === undefined) state._filtersCollapsed = true;
    state._filtersCollapsed = !state._filtersCollapsed;
    localStorage.setItem('cs_filters_collapsed', state._filtersCollapsed ? '1' : '0');
    if (state.activeView === 'sessions') renderSessionsView();
  });

  // Long-press on any server-status indicator (the header dot, the
  // Settings → Comms → Servers indicator) force-refreshes the WS
  // connection. Use event delegation so it works for indicators added
  // later by re-renders. ~600 ms threshold and a small movement
  // tolerance to keep clicks/taps from triggering it accidentally.
  installStatusLongPressRefresh();
});

function installStatusLongPressRefresh() {
  const HOLD_MS = 600;
  const MOVE_TOLERANCE = 10;
  let timer = null;
  let startX = 0, startY = 0;
  let armedTarget = null;

  function isStatusIndicator(el) {
    if (!el) return false;
    return !!(el.closest && (el.closest('#statusDot') || el.closest('.connection-indicator')));
  }

  function clear() {
    if (timer) { clearTimeout(timer); timer = null; }
    armedTarget = null;
  }

  document.addEventListener('pointerdown', e => {
    if (!isStatusIndicator(e.target)) return;
    armedTarget = e.target;
    startX = e.clientX; startY = e.clientY;
    timer = setTimeout(() => {
      timer = null;
      if (!armedTarget) return;
      armedTarget = null;
      forceRefreshConnection();
    }, HOLD_MS);
  }, { passive: true });

  document.addEventListener('pointermove', e => {
    if (!timer) return;
    if (Math.abs(e.clientX - startX) > MOVE_TOLERANCE || Math.abs(e.clientY - startY) > MOVE_TOLERANCE) clear();
  }, { passive: true });

  document.addEventListener('pointerup', clear, { passive: true });
  document.addEventListener('pointercancel', clear, { passive: true });
}

function forceRefreshConnection() {
  showToast('Refreshing server connection…', 'info', 2000);
  if (state.reconnectTimer) {
    clearTimeout(state.reconnectTimer);
    state.reconnectTimer = null;
  }
  state.reconnectDelay = 1000;
  if (state.ws) {
    try { state.ws.close(); } catch (_) {}
    state.ws = null;
  }
  state.connected = false;
  updateStatusDot();
  // Tiny delay so the close event flushes and the UI redraws disconnected
  // state before we attempt a new socket. connect() is a no-op if a
  // CONNECTING/OPEN socket somehow lingered.
  setTimeout(() => connect(), 100);
}

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

// ── F10 sprint 2: Project + Cluster Profile UI ─────────────────────────
//
// Two cards on Settings → General. Each card lists existing profiles
// with Edit / Delete / Smoke buttons, and an "+ Add" button that opens
// a form. Form has a YAML-view toggle so power users can edit the
// raw body; validation is server-side (/api/profiles/*/{name}/smoke).

const _profileKnown = {
  agents: ['agent-claude', 'agent-opencode', 'agent-gemini', 'agent-aider'],
  sidecars: ['', 'lang-go', 'lang-node', 'lang-python', 'lang-rust', 'lang-kotlin', 'lang-ruby', 'tools-ops'],
  clusterKinds: ['docker', 'k8s', 'cf'],
  memoryModes: ['sync-back', 'shared', 'ephemeral'],
  gitProviders: ['github', 'gitlab', 'local'],
};

// Per-panel state: whether an editor form is open, plus any draft
// being composed. Lives on window so the inline onclicks can reach it.
const _profileUIState = {
  project: { editing: null /* name or '__new__' */, yamlMode: false },
  cluster: { editing: null, yamlMode: false },
};

function loadProjectProfiles() { loadProfiles('project'); }
function loadClusterProfiles() { loadProfiles('cluster'); }

// Core loader — fetches /api/profiles/<kind>s and renders into the panel.
function loadProfiles(kind) {
  const path = '/api/profiles/' + kind + 's';
  const panel = document.getElementById(kind + 'ProfilesPanel');
  if (!panel) return;
  fetch(path, { headers: tokenHeader() })
    .then(r => r.ok ? r.json() : Promise.reject(r.statusText))
    .then(data => {
      const profiles = (data && data.profiles) || [];
      panel.innerHTML = renderProfilesPanel(kind, profiles);
    })
    .catch(err => {
      panel.innerHTML = `<div style="color:var(--error);font-size:13px;">Error loading profiles: ${escHtml(String(err))}</div>`;
    });
}

function renderProfilesPanel(kind, profiles) {
  const editing = _profileUIState[kind].editing;
  // List section
  let rows = profiles.map(p => renderProfileRow(kind, p)).join('');
  if (rows === '') {
    rows = '<div style="color:var(--text2);font-size:12px;padding:4px 0;">No profiles yet. Click + Add to create one.</div>';
  }
  const listHtml = `
    <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:6px;">
      <span style="font-size:12px;color:var(--text2);">${profiles.length} profile${profiles.length===1?'':'s'}</span>
      ${editing ? '' :
        `<button class="btn-success" style="font-size:11px;" onclick="openProfileEditor('${kind}','__new__')">+ Add</button>`}
    </div>
    ${rows}
  `;
  // Optional editor below the list
  const editorHtml = editing ? renderProfileEditor(kind, editing, profiles) : '';
  return listHtml + editorHtml;
}

function renderProfileRow(kind, p) {
  const summary = (kind === 'project')
    ? `${escHtml(p.image_pair && p.image_pair.agent || '?')} + ${escHtml((p.image_pair && p.image_pair.sidecar) || '(solo)')}  —  ${escHtml(p.git && p.git.url || '')}`
    : `kind=${escHtml(p.kind || '?')}  ctx=${escHtml(p.context || '-')}  ns=${escHtml(p.namespace || 'default')}`;
  // F10 S6.6 — federation badge on project rows: surfaces memory mode
  // + namespace + shared_with at-a-glance so operators don't have to
  // open the editor to see the federation contract for each profile.
  let fedBadge = '';
  if (kind === 'project' && p.memory && p.memory.mode) {
    const ns = (p.memory.namespace || 'project-' + p.name);
    const shared = (p.memory.shared_with || []).length;
    const sharedTxt = shared > 0 ? `  ⇄ ${shared}` : '';
    const colour = p.memory.mode === 'shared' ? '#4a90e2'
                  : p.memory.mode === 'sync-back' ? '#7a4ae2'
                  : '#888';
    fedBadge = `<span title="memory federation" style="display:inline-block;font-size:10px;padding:1px 6px;margin-left:6px;border-radius:8px;background:${colour}33;color:${colour};border:1px solid ${colour};">${escHtml(p.memory.mode)} · ${escHtml(ns)}${sharedTxt}</span>`;
  }
  return `
    <div class="profile-row" style="display:flex;justify-content:space-between;gap:8px;align-items:center;padding:6px 0;border-bottom:1px solid var(--border);">
      <div style="flex:1;min-width:0;">
        <div style="font-weight:600;">${escHtml(p.name)}${fedBadge}</div>
        <div style="font-size:11px;color:var(--text2);overflow:hidden;text-overflow:ellipsis;white-space:nowrap;">${summary}</div>
      </div>
      <div style="display:flex;gap:4px;flex-shrink:0;">
        <button class="btn-secondary" style="font-size:10px;" onclick="smokeProfile('${kind}','${escHtml(p.name)}')" title="Run smoke test">Smoke</button>
        <button class="btn-secondary" style="font-size:10px;" onclick="openProfileEditor('${kind}','${escHtml(p.name)}')" title="Edit">Edit</button>
        <button class="btn-danger" style="font-size:10px;" onclick="deleteProfile('${kind}','${escHtml(p.name)}')" title="Delete">×</button>
      </div>
    </div>
  `;
}

// renderProfileEditor draws either the form-view or the YAML-view for
// a profile being created or edited. profileList contains the already-
// loaded profiles so we can pre-populate fields for edit.
function renderProfileEditor(kind, name, profileList) {
  const isNew = name === '__new__';
  const existing = isNew ? null : profileList.find(p => p.name === name);
  const yaml = _profileUIState[kind].yamlMode;
  const title = isNew ? 'New ' + kind + ' profile' : 'Edit ' + kind + ' profile: ' + name;
  const body = yaml
    ? renderProfileEditorYAML(kind, existing)
    : (kind === 'project' ? renderProjectEditorForm(existing) : renderClusterEditorForm(existing));
  return `
    <div class="profile-editor" style="margin-top:12px;padding:8px;border:1px solid var(--accent);border-radius:6px;background:var(--bg2);">
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:8px;">
        <strong style="font-size:13px;">${escHtml(title)}</strong>
        <div style="display:flex;gap:6px;">
          <button class="btn-secondary" style="font-size:11px;" onclick="toggleProfileYaml('${kind}')">${yaml ? 'Form view' : 'YAML view'}</button>
          <button class="btn-secondary" style="font-size:11px;" onclick="cancelProfileEditor('${kind}')">Cancel</button>
          <button class="btn-success" style="font-size:11px;" onclick="saveProfileEditor('${kind}',${isNew?'true':'false'},'${escHtml(isNew?'':name)}')">Save</button>
        </div>
      </div>
      ${body}
    </div>
  `;
}

// Form view — project profile. Fields mirror ProjectProfile struct.
function renderProjectEditorForm(existing) {
  const p = existing || { name: '', description: '', git: {}, image_pair: {}, memory: {} };
  const inp = (id, label, val, ph, type='text') => `
    <div class="settings-row" style="justify-content:space-between;">
      <div class="settings-label">${label}</div>
      <input type="${type}" class="form-input profile-field" id="pp_${id}"
             value="${escHtml(val||'')}" placeholder="${escHtml(ph||'')}" />
    </div>`;
  const sel = (id, label, val, options) => `
    <div class="settings-row" style="justify-content:space-between;">
      <div class="settings-label">${label}</div>
      <select class="form-select profile-field" id="pp_${id}">
        ${options.map(o => `<option value="${escHtml(o)}" ${o===val?'selected':''}>${escHtml(o||'(none)')}</option>`).join('')}
      </select>
    </div>`;
  const chk = (id, label, val) => `
    <div class="settings-row" style="justify-content:space-between;">
      <div class="settings-label">${label}</div>
      <input type="checkbox" class="profile-field" id="pp_${id}" ${val?'checked':''} />
    </div>`;
  return `
    ${inp('name','Name', p.name, 'dns-label like: my-proj')}
    ${inp('description','Description', p.description, 'optional')}
    ${inp('git_url','Git URL', p.git.url, 'https://github.com/user/repo')}
    ${inp('git_branch','Git branch', p.git.branch, 'defaults to repo default')}
    ${sel('git_provider','Git provider', p.git.provider || '', _profileKnown.gitProviders.concat(['']))}
    ${sel('agent','Agent image', (p.image_pair && p.image_pair.agent) || '', _profileKnown.agents)}
    ${sel('sidecar','Sidecar image', (p.image_pair && p.image_pair.sidecar) || '', _profileKnown.sidecars)}
    ${sel('memory_mode','Memory mode', (p.memory && p.memory.mode) || 'sync-back', _profileKnown.memoryModes)}
    ${inp('memory_namespace','Memory namespace', p.memory && p.memory.namespace, 'defaults to project-<name>')}
    ${inp('memory_shared_with','Memory shared_with (comma-separated)', (p.memory && p.memory.shared_with || []).join(', '), 'peer profiles must reciprocate')}
    ${chk('allow_spawn','Allow spawn children', !!p.allow_spawn_children)}
    ${inp('spawn_total','Spawn budget (total)', p.spawn_budget_total, 'e.g. 10', 'number')}
    ${inp('spawn_per_min','Spawn budget per minute', p.spawn_budget_per_minute, 'e.g. 2', 'number')}
  `;
}

// Form view — cluster profile.
function renderClusterEditorForm(existing) {
  const c = existing || { name: '', kind: 'k8s', default_resources: {}, creds_ref: {} };
  const inp = (id, label, val, ph, type='text') => `
    <div class="settings-row" style="justify-content:space-between;">
      <div class="settings-label">${label}</div>
      <input type="${type}" class="form-input profile-field" id="cp_${id}"
             value="${escHtml(val||'')}" placeholder="${escHtml(ph||'')}" />
    </div>`;
  const sel = (id, label, val, options) => `
    <div class="settings-row" style="justify-content:space-between;">
      <div class="settings-label">${label}</div>
      <select class="form-select profile-field" id="cp_${id}">
        ${options.map(o => `<option value="${escHtml(o)}" ${o===val?'selected':''}>${escHtml(o)}</option>`).join('')}
      </select>
    </div>`;
  return `
    ${inp('name','Name', c.name, 'dns-label like: test-k8s')}
    ${inp('description','Description', c.description, 'optional')}
    ${sel('kind','Kind', c.kind || 'k8s', _profileKnown.clusterKinds)}
    ${inp('context','Context', c.context, 'kubectl context name')}
    ${inp('endpoint','Endpoint (override)', c.endpoint, 'https://... (optional)')}
    ${inp('namespace','Namespace', c.namespace, 'default')}
    ${inp('registry','Image registry', c.image_registry, 'registry.example.com/datawatch')}
    ${inp('pull_secret','Pull secret', c.image_pull_secret, 'k8s secret name (optional)')}
    ${inp('parent_cb','Parent callback URL', c.parent_callback_url, 'auto-detect if empty')}
  `;
}

// YAML view — shared between kinds. Single big textarea.
function renderProfileEditorYAML(kind, existing) {
  // We POST JSON but show YAML; converted on save.
  let asYAML = '# Edit body here (YAML). Will be POSTed as JSON.\n';
  if (existing) {
    try {
      asYAML = yamlStringify(existing);
    } catch (e) {
      asYAML = '# (render error: ' + e.message + ')\n' + JSON.stringify(existing, null, 2);
    }
  } else {
    asYAML += kind === 'project'
      ? 'name: my-proj\ndescription: ""\ngit:\n  url: https://github.com/user/repo\n  branch: main\nimage_pair:\n  agent: agent-claude\n  sidecar: lang-go\nmemory:\n  mode: sync-back\n'
      : 'name: test-k8s\nkind: k8s\ncontext: testing\nnamespace: default\n';
  }
  return `
    <textarea id="profileYamlBody" class="form-input" rows="18" style="width:100%;font-family:monospace;font-size:11px;">${escHtml(asYAML)}</textarea>
  `;
}

// Tiny YAML serializer — just enough for profile bodies. Handles maps,
// arrays, scalars, skips keys whose value is empty string / null.
// Not general-purpose; kept small so we don't pull in a YAML lib.
function yamlStringify(obj, indent=0) {
  const pad = '  '.repeat(indent);
  if (obj === null || obj === undefined) return pad + 'null\n';
  if (typeof obj === 'string') return JSON.stringify(obj) + '\n';
  if (typeof obj === 'number' || typeof obj === 'boolean') return obj + '\n';
  if (Array.isArray(obj)) {
    if (obj.length === 0) return '[]\n';
    return '\n' + obj.map(v => pad + '- ' + yamlStringify(v, indent+1).trim()).join('\n') + '\n';
  }
  if (typeof obj === 'object') {
    const lines = [];
    Object.keys(obj).forEach(k => {
      const v = obj[k];
      if (v === '' || v === null || v === undefined) return;
      if (Array.isArray(v) && v.length === 0) return;
      if (typeof v === 'object' && !Array.isArray(v) && Object.keys(v).length === 0) return;
      const rendered = yamlStringify(v, indent+1);
      if (typeof v === 'object' && !Array.isArray(v)) {
        lines.push(pad + k + ':');
        lines.push(rendered.replace(/\n$/, ''));
      } else if (Array.isArray(v)) {
        lines.push(pad + k + ':' + rendered.replace(/\n$/, ''));
      } else {
        lines.push(pad + k + ': ' + rendered.trim());
      }
    });
    return lines.join('\n') + '\n';
  }
  return pad + String(obj) + '\n';
}

function openProfileEditor(kind, name) {
  _profileUIState[kind].editing = name;
  _profileUIState[kind].yamlMode = false;
  (kind === 'project' ? loadProjectProfiles : loadClusterProfiles)();
}

function cancelProfileEditor(kind) {
  _profileUIState[kind].editing = null;
  (kind === 'project' ? loadProjectProfiles : loadClusterProfiles)();
}

function toggleProfileYaml(kind) {
  // Switching view drops any in-form changes — warn via toast.
  _profileUIState[kind].yamlMode = !_profileUIState[kind].yamlMode;
  showToast('View switched; unsaved form inputs were lost', 'info', 2000);
  (kind === 'project' ? loadProjectProfiles : loadClusterProfiles)();
}

// saveProfileEditor collects form fields (or the YAML textarea) into a
// JSON body and POSTs/PUTs to the REST endpoint.
function saveProfileEditor(kind, isNew, name) {
  const body = _profileUIState[kind].yamlMode
    ? parseProfileYAML(kind)
    : (kind === 'project' ? collectProjectForm() : collectClusterForm());
  if (!body) return; // error already toasted
  const path = '/api/profiles/' + kind + 's' + (isNew ? '' : '/' + encodeURIComponent(name));
  const method = isNew ? 'POST' : 'PUT';
  fetch(path, {
    method,
    headers: Object.assign({'Content-Type':'application/json'}, tokenHeader()),
    body: JSON.stringify(body),
  })
  .then(r => r.text().then(t => ({ status: r.status, body: t })))
  .then(({status, body}) => {
    if (status >= 400) {
      showToast('Save failed: ' + body, 'error', 4000);
      return;
    }
    showToast('Saved ' + kind + ' profile ' + (isNew ? body.name||'' : name), 'success', 2000);
    _profileUIState[kind].editing = null;
    (kind === 'project' ? loadProjectProfiles : loadClusterProfiles)();
  })
  .catch(err => showToast('Save error: ' + err, 'error', 3000));
}

function collectProjectForm() {
  const val = id => (document.getElementById('pp_' + id) || {}).value || '';
  const chk = id => !!((document.getElementById('pp_' + id) || {}).checked);
  const num = id => {
    const v = val(id); if (v === '') return 0;
    const n = parseInt(v, 10); return Number.isNaN(n) ? 0 : n;
  };
  return {
    name: val('name'),
    description: val('description'),
    git: {
      url: val('git_url'),
      branch: val('git_branch'),
      provider: val('git_provider'),
    },
    image_pair: {
      agent: val('agent'),
      sidecar: val('sidecar'),
    },
    memory: {
      mode: val('memory_mode'),
      namespace: val('memory_namespace'),
      shared_with: (val('memory_shared_with') || '')
        .split(',').map(s => s.trim()).filter(s => s.length > 0),
    },
    allow_spawn_children: chk('allow_spawn'),
    spawn_budget_total: num('spawn_total'),
    spawn_budget_per_minute: num('spawn_per_min'),
  };
}

function collectClusterForm() {
  const val = id => (document.getElementById('cp_' + id) || {}).value || '';
  return {
    name: val('name'),
    description: val('description'),
    kind: val('kind'),
    context: val('context'),
    endpoint: val('endpoint'),
    namespace: val('namespace'),
    image_registry: val('registry'),
    image_pull_secret: val('pull_secret'),
    parent_callback_url: val('parent_cb'),
  };
}

// parseProfileYAML runs client-side: attempt JSON first (some users
// paste JSON in the YAML box), then tiny YAML parser for the common
// object-of-scalars/objects shape we expect. Anything more exotic
// gets flagged back to the user.
function parseProfileYAML() {
  const txt = (document.getElementById('profileYamlBody') || {}).value || '';
  const stripped = txt.split('\n').filter(l => !/^\s*#/.test(l)).join('\n').trim();
  if (!stripped) { showToast('YAML body is empty', 'error', 2000); return null; }
  // Try JSON
  try { return JSON.parse(stripped); } catch {}
  // Tiny YAML parser (object of scalars or object of object-of-scalars)
  try {
    return parseYAMLNaive(stripped);
  } catch (e) {
    showToast('YAML parse error: ' + e.message, 'error', 3000);
    return null;
  }
}

// parseYAMLNaive handles the shape produced by yamlStringify above:
//   top-level scalar keys, nested one level of maps, simple bool/number/
//   string/null scalars. Arrays and multi-line strings are not
//   supported — use JSON for those.
function parseYAMLNaive(text) {
  const out = {};
  let currentSub = null; // name of nested object being filled
  const lines = text.split('\n');
  for (const raw of lines) {
    if (!raw.trim()) continue;
    if (/^\s+/.test(raw)) {
      // indented line → belongs to currentSub
      if (!currentSub) throw new Error('unexpected indent: "' + raw + '"');
      const m = raw.trim().match(/^([A-Za-z0-9_-]+):\s*(.*)$/);
      if (!m) throw new Error('bad indented line: "' + raw + '"');
      out[currentSub][m[1]] = coerceYAMLScalar(m[2]);
    } else {
      const m = raw.match(/^([A-Za-z0-9_-]+):\s*(.*)$/);
      if (!m) throw new Error('bad top-level line: "' + raw + '"');
      if (m[2] === '') {
        currentSub = m[1]; out[currentSub] = {};
      } else {
        currentSub = null;
        out[m[1]] = coerceYAMLScalar(m[2]);
      }
    }
  }
  return out;
}
function coerceYAMLScalar(s) {
  s = s.trim();
  if (s === '' || s === 'null' || s === '~') return '';
  if (s === 'true') return true;
  if (s === 'false') return false;
  if (/^-?\d+$/.test(s)) return parseInt(s, 10);
  if ((s.startsWith('"') && s.endsWith('"')) || (s.startsWith("'") && s.endsWith("'"))) {
    return s.slice(1, -1);
  }
  return s;
}

function smokeProfile(kind, name) {
  fetch('/api/profiles/' + kind + 's/' + encodeURIComponent(name) + '/smoke',
        { method: 'POST', headers: tokenHeader() })
  .then(r => r.json().then(j => ({ status: r.status, body: j })))
  .then(({status, body}) => {
    const passed = status === 200 && (!body.errors || body.errors.length === 0);
    const lines = ['Smoke: ' + (passed ? 'PASS' : 'FAIL')];
    (body.checks || []).forEach(c => lines.push('  ✓ ' + c));
    (body.errors || []).forEach(e => lines.push('  ✗ ' + e));
    (body.warnings || []).forEach(w => lines.push('  ⚠ ' + w));
    showToast(lines.join('\n'), passed ? 'success' : 'error', 6000);
  })
  .catch(err => showToast('Smoke error: ' + err, 'error', 3000));
}

function deleteProfile(kind, name) {
  if (!confirm('Delete ' + kind + ' profile "' + name + '"?')) return;
  fetch('/api/profiles/' + kind + 's/' + encodeURIComponent(name),
        { method: 'DELETE', headers: tokenHeader() })
  .then(r => {
    if (r.status >= 400) { showToast('Delete failed: HTTP ' + r.status, 'error', 3000); return; }
    showToast('Deleted ' + kind + ' profile ' + name, 'success', 2000);
    (kind === 'project' ? loadProjectProfiles : loadClusterProfiles)();
  });
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

// BL172 — count of stale federated peers (last_push_at older than 60s
// or never). Polled independently of the Settings panel so the
// operator sees the badge without opening the Monitor card.
function updatePeerStaleBadge() {
  const badge = document.getElementById('peerStaleBadge');
  if (!badge) return;
  apiFetch('/api/observer/peers').then(data => {
    const peers = (data && data.peers) || [];
    if (peers.length === 0) {
      badge.style.display = 'none';
      return;
    }
    const now = Date.now();
    let stale = 0;
    for (const p of peers) {
      const lastPush = p.last_push_at ? new Date(p.last_push_at).getTime() : 0;
      if (!lastPush || (now - lastPush) > 60000) {
        stale++;
      }
    }
    if (stale > 0) {
      badge.textContent = stale > 99 ? '99+' : String(stale);
      badge.style.display = 'inline';
      badge.title = `${stale} federated peer(s) stale (>60s since last push)`;
    } else {
      badge.style.display = 'none';
    }
  }).catch(() => {
    // 503 (registry disabled) or network error — hide silently.
    badge.style.display = 'none';
  });
}

// Refresh the peer-stale badge every 30s. Cheap GET; cached on the
// daemon side and skipped when registry is disabled.
setInterval(updatePeerStaleBadge, 30000);
setTimeout(updatePeerStaleBadge, 1500);  // initial paint after auth settles

// BL191 / BL202 (v5.3.0) — top-level Autonomous tab. Operator
// directive 2026-04-26: PRDs are first-class workflow on par with
// Sessions, not buried inside Settings → General.
// v5.26.36 — operator-asked: "new prd should be a FAB (+) and not
// the new prd button at top. There should be a filter icon like
// sessions list to hide/show the filter and sort options, with it
// hidden by default."
//
// Header now carries just a filter-toggle icon (🔍-like funnel
// glyph). Filter row collapses by default; click the toggle to
// expose the status dropdown + templates checkbox. New PRD lives
// in a fixed Floating Action Button (+) at bottom-right.
function _toggleAutonomousFilters() {
  const row = document.getElementById('prdFilterRow');
  if (!row) return;
  const open = row.style.display !== 'none';
  row.style.display = open ? 'none' : 'flex';
  const btn = document.getElementById('prdFilterToggleBtn');
  if (btn) btn.classList.toggle('active', !open);
}
window._toggleAutonomousFilters = _toggleAutonomousFilters;

function renderAutonomousView() {
  const view = document.getElementById('view');
  if (!view) return;
  view.innerHTML = `
    <div class="view-content" style="position:relative;">
      <div style="padding:8px 4px;">
        <!-- v5.27.8 (BL208 #30) — operator-asked: drop the "PRDs"
             sub-header. The Autonomous tab label already makes the
             context clear; the redundant heading wasted vertical
             space and didn't match the Sessions tab's no-sub-header
             layout. Filter row stays — that's still functional. -->
        <div id="prdPanelToolbar" style="display:none;"></div>
        <div id="prdFilterRow" style="display:none;gap:6px;align-items:center;padding:4px 0 8px 0;flex-wrap:wrap;">
          <select id="prdFilterStatus" class="form-select" style="font-size:12px;padding:2px 6px;" onchange="loadPRDPanel()">
            <option value="">All statuses</option>
            <option value="draft">draft</option>
            <option value="needs_review">needs_review</option>
            <option value="approved">approved</option>
            <option value="running">running</option>
            <option value="completed">completed</option>
            <option value="rejected">rejected</option>
            <option value="cancelled">cancelled</option>
          </select>
          <label style="font-size:12px;display:inline-flex;gap:4px;align-items:center;"><input type="checkbox" id="prdIncludeTemplates" onchange="loadPRDPanel()" /> ${escHtml(t('autonomous_filter_templates'))}</label>
        </div>
        <div id="prdPanel" style="font-size:13px;color:var(--text);padding:6px 0;">loading…</div>
      </div>
      <!-- v5.26.37 — reuse the canonical .new-session-fab CSS class
           so size + bottom-nav clearance + safe-area inset match the
           sessions-tab FAB exactly. The element is removed from DOM
           when the operator leaves the autonomous view (view-content
           innerHTML gets replaced by the next renderXxxView()), so
           no separate visibility toggle is needed. -->
      <button id="prdNewFab" class="new-session-fab"
              onclick="openPRDCreateModal()" title="${escHtml(t('autonomous_fab_new'))}" aria-label="${escHtml(t('autonomous_fab_new'))}">+</button>
    </div>
  `;
  loadPRDPanel();
}
window.renderAutonomousView = renderAutonomousView;

function renderAlertsView() {
  const view = document.getElementById('view');
  if (!view) return;
  view.innerHTML = `<div class="view-content"><div id="alertsList" style="padding:12px;"><div class="spinner" style="text-align:center;padding:32px;">${escHtml(t('common_loading'))}</div></div></div>`;

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
      el.innerHTML = `<div style="text-align:center;color:var(--text2);padding:32px;">${escHtml(t('common_no_alerts'))}</div>`;
      return;
    }

    state.alertUnread = 0;
    state.alertSystemUnread = 0;
    updateAlertBadge();
    fetch('/api/alerts', { method: 'POST', headers: { 'Content-Type': 'application/json', ...tokenHeader() }, body: JSON.stringify({ all: true }) });

    // Group by session (BL226: source=system or no session_id → __system__)
    const groups = new Map();
    for (const a of data.alerts) {
      const key = (a.source === 'system' || !a.session_id) ? '__system__' : a.session_id;
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
      const sessLink = sess ? `<span style="cursor:pointer;text-decoration:underline;" onclick="${escHtml(`navigate('session',${JSON.stringify(sessID)})`)}">${escHtml(label)}</span>` : escHtml(label);
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
    const inactiveCount = inactiveTabs.length;
    const systemCount = systemAlerts.length;
    const defaultTab = activeCount > 0 ? 'active' : (inactiveCount > 0 ? 'inactive' : 'system');

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
    if (inactiveTabs.length === 0) {
      inactiveHtml = '<div style="text-align:center;color:var(--text2);padding:24px;">No inactive alerts.</div>';
    } else {
      for (const entry of inactiveTabs) {
        inactiveHtml += renderSessionSection(entry, true);
      }
    }

    // Build system tab content — BL226: pipeline/plugin/eBPF failures
    let systemHtml = '';
    if (systemAlerts.length === 0) {
      systemHtml = '<div style="text-align:center;color:var(--text2);padding:24px;">No system alerts.</div>';
    } else {
      systemHtml = systemAlerts.map((a, i) => renderAlert(a, '', i === 0)).join('');
    }
    const sysBadge = systemCount > 0
      ? `<span style="background:var(--error);color:#fff;border-radius:10px;font-size:10px;padding:1px 5px;margin-left:4px;">${systemCount > 99 ? '99+' : systemCount}</span>`
      : '';

    el.innerHTML = `
      <div style="display:flex;align-items:center;gap:0;margin-bottom:8px;">
        <button class="output-tab ${defaultTab === 'active' ? 'active' : ''}" id="alertTabActive" onclick="switchAlertTab('active')">
          Active${activeCount > 0 ? ' (' + activeCount + ')' : ''}
        </button>
        <button class="output-tab ${defaultTab === 'inactive' ? 'active' : ''}" id="alertTabInactive" onclick="switchAlertTab('inactive')">
          Inactive${inactiveCount > 0 ? ' (' + inactiveCount + ')' : ''}
        </button>
        <button class="output-tab ${defaultTab === 'system' ? 'active' : ''}" id="alertTabSystem" onclick="switchAlertTab('system')">
          System${sysBadge}
        </button>
        <div style="flex:1;"></div>
        <button class="btn-secondary" style="font-size:12px;" onclick="renderAlertsView()">Refresh</button>
      </div>
      <div id="alertPanelActive" style="${defaultTab === 'active' ? '' : 'display:none'}">${activeHtml}</div>
      <div id="alertPanelInactive" style="${defaultTab === 'inactive' ? '' : 'display:none'}">${inactiveHtml}</div>
      <div id="alertPanelSystem" style="${defaultTab === 'system' ? '' : 'display:none'}">${systemHtml}</div>
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
  const systemTab = document.getElementById('alertTabSystem');
  const systemPanel = document.getElementById('alertPanelSystem');
  if (!activeTab || !inactiveTab || !activePanel || !inactivePanel) return;
  [activeTab, inactiveTab, systemTab].forEach(t => t && t.classList.remove('active'));
  [activePanel, inactivePanel, systemPanel].forEach(p => p && (p.style.display = 'none'));
  if (tab === 'active') {
    activeTab.classList.add('active');
    activePanel.style.display = '';
  } else if (tab === 'system') {
    if (systemTab) systemTab.classList.add('active');
    if (systemPanel) systemPanel.style.display = '';
  } else {
    inactiveTab.classList.add('active');
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
  // v4.1.0 — load installed-plugins status strip into the card footer.
  loadPluginsStatus();
  // v4.1.1 — load eBPF status strip just above plugins.
  loadEBPFStatus();
  // BL172 (S11) — federated peers row.
  loadObserverPeers();
  // BL173 (S12) — cluster nodes (Shape C); shows itself only if non-empty.
  loadObserverClusterNodes();
  // v5.27.10 (BL216) — MCP channel bridge state.
  loadChannelBridge();
}

// v5.27.10 (BL216) — render /api/channel/info into the Monitor card so
// the operator can see at a glance which bridge sessions are using.
// Surfaces stale .mcp.json files with a one-click cleanup hint.
function loadChannelBridge() {
  const el = document.getElementById('channelBridgeStatus');
  if (!el) return;
  apiFetch('/api/channel/info').then(info => {
    if (!info || typeof info !== 'object') {
      el.textContent = 'unavailable';
      return;
    }
    const kindBadge = info.kind === 'go'
      ? '<span style="color:var(--success);font-weight:600;">Go</span>'
      : '<span style="color:var(--warning);font-weight:600;">JS (fallback)</span>';
    const ready = info.ready ? '✓' : '⚠';
    let html = '';
    html += `<div>Bridge: ${kindBadge} ${ready}</div>`;
    if (info.path) {
      html += `<div style="opacity:0.75;word-break:break-all;">${escHtml(info.path)}</div>`;
    }
    if (!info.ready && info.hint) {
      html += `<div style="color:var(--warning);margin-top:4px;">${escHtml(info.hint)}</div>`;
    }
    if (info.kind === 'js' && info.node_path) {
      html += `<div style="opacity:0.75;margin-top:2px;">node: ${escHtml(info.node_path)}</div>`;
    }
    // v5.28.7 — show MCP mode status (stdio/SSE) so operator can see
    // what's actually running without checking the config page separately.
    if (info.stdio_enabled || info.sse_enabled) {
      const modes = [];
      if (info.stdio_enabled) modes.push('<span style="color:var(--success);font-weight:500;">stdio</span>');
      if (info.sse_enabled) modes.push('<span style="color:var(--success);font-weight:500;">SSE</span>');
      html += `<div style="margin-top:4px;">MCP: ${modes.join(' + ')}</div>`;
    }
    if (Array.isArray(info.stale_mcp_json) && info.stale_mcp_json.length) {
      html += `<div style="margin-top:6px;color:var(--warning);">Stale .mcp.json files (point at missing channel.js):</div>`;
      info.stale_mcp_json.forEach(e => {
        html += `<div style="opacity:0.85;word-break:break-all;">• ${escHtml(e.path)} → ${escHtml(e.missing_channel_js)}</div>`;
      });
      html += `<div style="opacity:0.7;margin-top:4px;">Run <code>datawatch channel cleanup-stale-mcp-json</code> to remove.</div>`;
    }
    el.innerHTML = html;
  }).catch(() => {
    el.textContent = 'unavailable';
  });
}

// v4.1.1 — render the eBPF state from the observer's StatsResponse v2.
// Shows configured / capability / kprobe-loaded with honest messages
// so the operator knows whether a `datawatch setup ebpf` actually
// took effect.
function loadEBPFStatus() {
  const line = document.getElementById('ebpfStatusLine');
  if (!line) return;
  apiFetch('/api/stats?v=2').then(s => {
    const e = (s && s.host && s.host.ebpf) || null;
    if (!e) { line.innerHTML = '<span style="opacity:0.7;">observer disabled</span>'; return; }
    const dot = (color, label) => `<span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:${color};margin-right:6px;vertical-align:middle;"></span>${label}`;
    let head;
    if (e.kprobes_loaded) {
      head = dot('var(--success,#10b981)', 'live — per-process net wired');
    } else if (e.configured && e.capability) {
      head = dot('var(--accent2,#7c3aed)', 'configured + capability granted');
    } else if (e.configured) {
      head = dot('var(--warning,#f59e0b)', 'configured but capability missing');
    } else {
      head = dot('var(--text2)', 'off');
    }
    const msg = e.message ? `<div style="opacity:0.8;margin-top:3px;">${escHtml(e.message)}</div>` : '';
    line.innerHTML = head + msg;
  }).catch(() => {
    line.innerHTML = '<span style="opacity:0.7;">/api/stats?v=2 unavailable</span>';
  });
}

// v4.1.0 — populate the plugins-installed strip that sits at the
// bottom of the System Statistics card in Settings → Monitor.
function loadPluginsStatus() {
  const list = document.getElementById('pluginsStatusList');
  if (!list) return;
  apiFetch('/api/plugins').then(data => {
    const plugins = (data && data.plugins) || [];
    const native = (data && data.native) || [];
    const nativeRows = native.map(p => {
      const on = !!p.enabled;
      const dot = `<span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:${on?'var(--success,#10b981)':'var(--text2)'};margin-right:6px;"></span>`;
      const tag = ` <span style="opacity:0.55;font-size:11px;border:1px solid var(--text2);border-radius:3px;padding:0 4px;margin-left:4px;">native</span>`;
      const ver = p.version ? ` <span style="opacity:0.6;">v${escHtml(p.version)}</span>` : '';
      const desc = p.description ? ` &middot; <span style="opacity:0.7;">${escHtml(p.description)}</span>` : '';
      const msg = p.message ? ` &middot; <span style="opacity:0.6;font-size:12px;">${escHtml(p.message)}</span>` : '';
      return `<div style="padding:3px 0;">${dot}<strong>${escHtml(p.name)}</strong>${tag}${ver}${desc}${msg}</div>`;
    }).join('');
    const subRows = plugins.map(p => {
      const on = !!p.enabled;
      const dot = `<span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:${on?'var(--success,#10b981)':'var(--text2)'};margin-right:6px;"></span>`;
      const hooks = Array.isArray(p.hooks) && p.hooks.length
        ? ` &middot; <span style="opacity:0.7;">${p.hooks.join(', ')}</span>` : '';
      const invokes = (typeof p.invoke_count === 'number' && p.invoke_count > 0)
        ? ` &middot; <span style="opacity:0.7;">${p.invoke_count} invoke${p.invoke_count===1?'':'s'}</span>` : '';
      const err = p.last_error ? ` &middot; <span style="color:var(--error);" title="${escHtml(p.last_error)}">last-error</span>` : '';
      return `<div style="padding:3px 0;">${dot}<strong>${escHtml(p.name)}</strong>${p.version?` <span style="opacity:0.6;">v${escHtml(p.version)}</span>`:''} &middot; ${on?'enabled':'disabled'}${hooks}${invokes}${err}</div>`;
    }).join('');
    if (!nativeRows && !subRows) {
      list.innerHTML = '<span style="opacity:0.7;">none installed</span> &middot; <a href="/docs/api/plugins.md" style="color:var(--accent2);">plugin docs</a>';
      return;
    }
    list.innerHTML = nativeRows + subRows;
  }).catch(() => {
    // /api/plugins should always succeed now (native list is unconditional).
    list.innerHTML = '<span style="opacity:0.7;">plugin status unavailable</span>';
  });
}

// BL172 (S11) — federated observer peers (Shape B / C). Reads
// /api/observer/peers; renders a single line per peer with health
// dot, last-push age, and Snapshot / Remove actions. 503 (registry
// disabled) shows a calm "off" message rather than an error state.
// S13 — federated peers filter persists in localStorage.
// Values: "all" | "B" | "C" | "A" (A = agent / F10 worker).
function getPeerFilter() {
  return localStorage.getItem('cs_peer_filter') || 'all';
}
function setPeerFilter(v) {
  localStorage.setItem('cs_peer_filter', v);
  loadObserverPeers();
}

function loadObserverPeers() {
  const list = document.getElementById('observerPeersList');
  if (!list) return;
  apiFetch('/api/observer/peers').then(data => {
    const peers = (data && data.peers) || [];
    // Filter pill row — always rendered when there's at least one
    // peer of any kind, since the count distribution is what helps
    // the operator scope.
    const filter = getPeerFilter();
    const counts = { all: peers.length, A: 0, B: 0, C: 0 };
    for (const p of peers) {
      const s = (p.shape || '').toUpperCase();
      if (counts[s] !== undefined) counts[s]++;
    }
    const pillBtn = (val, label) => {
      const active = filter === val ? 'background:var(--accent2);color:var(--bg);' : '';
      return `<button class="filter-toggle-btn" style="${active}" onclick="setPeerFilter('${val}')">${label} (${counts[val]||0})</button>`;
    };
    const pills = peers.length > 0
      ? `<div style="display:flex;gap:4px;padding:0 0 6px;flex-wrap:wrap;align-items:center;">
          ${pillBtn('all','All')}
          ${pillBtn('A','Agents')}
          ${pillBtn('B','Standalone')}
          ${pillBtn('C','Cluster')}
          <button class="filter-toggle-btn" style="margin-left:auto;background:rgba(96,165,250,0.18);" onclick="showCrossHostView()" title="BL180 Phase 2 — local + every peer with cross-peer caller attribution">↔ Cross-host view</button>
        </div>` : '';

    if (!peers.length) {
      list.innerHTML = pills + '<span style="opacity:0.7;">no peers registered</span> &middot; '
        + '<span style="opacity:0.7;">deploy <code>datawatch-stats --datawatch &lt;url&gt; --name &lt;peer&gt;</code> on a remote host, or spawn an autonomous worker (it auto-peers). <a href="/docs/api/observer-peers.md" style="color:var(--accent);">docs</a></span>';
      return;
    }
    const visible = filter === 'all' ? peers : peers.filter(p => (p.shape || '').toUpperCase() === filter);
    if (visible.length === 0) {
      list.innerHTML = pills + `<span style="opacity:0.7;">no peers match the "${escHtml(filter)}" filter</span>`;
      return;
    }
    const now = Date.now();
    const rows = visible.map(p => {
      const lastPush = p.last_push_at ? new Date(p.last_push_at).getTime() : 0;
      const ageMs = lastPush ? (now - lastPush) : Infinity;
      let dotColor = 'var(--text2)';
      let ageLabel = 'never pushed';
      if (lastPush) {
        if (ageMs < 15000) dotColor = 'var(--success,#10b981)';
        else if (ageMs < 60000) dotColor = 'var(--warning,#f59e0b)';
        else dotColor = 'var(--error,#ef4444)';
        ageLabel = 'last push ' + observerPeerAgo(ageMs);
      }
      const dot = `<span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:${dotColor};margin-right:6px;"></span>`;
      // S13 — friendlier shape labels.
      const shapeLabels = { A: 'agent', B: 'standalone', C: 'cluster' };
      const shapeKey = (p.shape || '?').toUpperCase();
      const shapeText = shapeLabels[shapeKey] || ('shape ' + (p.shape||'?'));
      const shapeTag = `<span style="opacity:0.55;font-size:11px;border:1px solid var(--text2);border-radius:3px;padding:0 4px;margin-left:4px;">${escHtml(shapeText)}</span>`;
      const ver = p.version ? ` <span style="opacity:0.6;">v${escHtml(p.version)}</span>` : '';
      const safeName = JSON.stringify(p.name || '');
      const actions = `
        <button class="btn-icon" title="Last snapshot" style="font-size:11px;padding:1px 6px;margin-left:8px;" onclick='showObserverPeerSnapshot(${safeName})'>&#128202;</button>
        <button class="btn-icon" title="Remove peer (rotates token; peer auto-re-registers)" style="font-size:11px;padding:1px 6px;" onclick='removeObserverPeer(${safeName})'>&times;</button>`;
      return `<div style="padding:4px 0;display:flex;align-items:center;flex-wrap:wrap;">${dot}<strong>${escHtml(p.name)}</strong>${shapeTag}${ver} &middot; <span style="opacity:0.7;">${ageLabel}</span>${actions}</div>`;
    }).join('');
    list.innerHTML = pills + rows;
  }).catch(err => {
    const msg = (err && err.status === 503) ? 'off' : 'unavailable';
    list.innerHTML = `<span style="opacity:0.7;">peer registry ${msg}</span> &middot; `
      + `<span style="opacity:0.7;">enable with <code>observer.peers.allow_register: true</code></span>`;
  });
}

// BL180 Phase 2 cross-host (v5.16.0) — modal that fetches the
// federation-aware envelope view from /api/observer/envelopes/all-peers
// and renders one collapsible block per peer. CallerAttribution rows
// with `<peer>:<envelope-id>` prefixes (cross-host) get a 🔗 badge so
// operators can see at a glance which envelopes are reached across
// peers vs. only locally.
window.showCrossHostView = function() {
  const existing = document.getElementById('crossHostModal');
  if (existing) existing.remove();
  const modal = document.createElement('div');
  modal.id = 'crossHostModal';
  modal.className = 'confirm-modal-overlay';
  modal.innerHTML = `<div class="confirm-modal" style="max-width:900px;width:90vw;max-height:85vh;overflow:auto;padding:0;">
    <div style="display:flex;justify-content:space-between;align-items:center;padding:10px 14px;border-bottom:1px solid var(--border);">
      <strong>Cross-host envelope view</strong>
      <button class="btn-icon" style="font-size:16px;" onclick="document.getElementById('crossHostModal').remove();">&times;</button>
    </div>
    <div id="crossHostBody" style="padding:12px;font-size:12px;">
      <em style="color:var(--text2);">loading /api/observer/envelopes/all-peers …</em>
    </div>
  </div>`;
  document.body.appendChild(modal);
  apiFetch('/api/observer/envelopes/all-peers').then(data => {
    const body = document.getElementById('crossHostBody');
    if (!body) return;
    const byPeer = (data && data.by_peer) || {};
    const peerNames = Object.keys(byPeer);
    if (peerNames.length === 0) {
      body.innerHTML = '<em style="opacity:0.7;">no envelopes anywhere — local observer empty + no peers pushed yet</em>';
      return;
    }
    const sections = peerNames.map(peer => {
      const envs = byPeer[peer] || [];
      const envHtml = envs.map(e => {
        const callers = (e.callers || []).map(c => {
          const isCross = (c.caller || '').includes(':') && (c.caller || '').split(':').length >= 3;
          const tag = isCross
            ? `<span title="cross-host attribution" style="background:rgba(96,165,250,0.25);color:var(--accent);font-size:9px;padding:1px 4px;border-radius:4px;margin-right:3px;">🔗 cross</span>`
            : '';
          return `<div style="font-size:10px;color:var(--text2);padding-left:12px;">${tag}<code>${escHtml(c.caller || '?')}</code> <span style="opacity:0.7;">${escHtml(c.caller_kind || '')} · ${c.conns || 0} conns</span></div>`;
        }).join('');
        const listenSummary = (e.listen_addrs || []).map(la => `${la.ip}:${la.port}`).join(', ');
        const outboundSummary = (e.outbound_edges || []).map(oe => `${oe.target_ip}:${oe.target_port}`).join(', ');
        return `<div style="padding:4px 0;border-top:1px solid var(--border);">
          <div><code style="font-size:11px;">${escHtml(e.id || '?')}</code> <span style="opacity:0.6;">${escHtml(e.kind || '')}</span> <strong>${escHtml(e.label || '')}</strong></div>
          ${listenSummary ? `<div style="font-size:10px;color:var(--text2);">listen: ${escHtml(listenSummary)}</div>` : ''}
          ${outboundSummary ? `<div style="font-size:10px;color:var(--text2);">outbound: ${escHtml(outboundSummary)}</div>` : ''}
          ${callers}
        </div>`;
      }).join('') || '<em style="opacity:0.7;font-size:10px;">no envelopes</em>';
      return `<details open style="margin-bottom:8px;">
        <summary style="cursor:pointer;font-weight:600;">${escHtml(peer)} <span style="opacity:0.6;font-weight:normal;">(${envs.length} envelopes)</span></summary>
        <div style="margin-top:4px;padding-left:6px;">${envHtml}</div>
      </details>`;
    }).join('');
    body.innerHTML = sections;
  }).catch(err => {
    const body = document.getElementById('crossHostBody');
    if (body) body.innerHTML = `<span style="color:var(--error,#ef4444);">load failed: ${escHtml(String((err && err.message) || err))}</span>`;
  });
};

// observerPeerAgo formats a millisecond delta as "Xs / Xm / Xh ago".
function observerPeerAgo(ms) {
  if (ms < 1000) return 'just now';
  if (ms < 60000) return Math.floor(ms / 1000) + 's ago';
  if (ms < 3600000) return Math.floor(ms / 60000) + 'm ago';
  return Math.floor(ms / 3600000) + 'h ago';
}

// Drill-down — fetches /api/observer/peers/{name}/stats and shows
// the host + envelope summary in a transient toast/dialog.
// BL173 task 6 — full peer snapshot in a modal with the envelope tree
// + per-envelope drill-down to the process list (via /api/observer/envelope).
// Replaces the v4.5.1 toast (which showed only the top 6 envelopes).
function showObserverPeerSnapshot(name) {
  const existing = document.getElementById('observerSnapshotModal');
  if (existing) existing.remove();
  const modal = document.createElement('div');
  modal.id = 'observerSnapshotModal';
  modal.className = 'observer-snapshot-modal';
  modal.innerHTML = `
    <div class="observer-snapshot-card">
      <div class="observer-snapshot-header">
        <div>
          <h3 style="margin:0;">Peer snapshot</h3>
          <div style="opacity:0.7;font-size:12px;" id="observerSnapPeerLine">${escHtml(name)} — loading…</div>
        </div>
        <button class="btn-icon" onclick="closeObserverSnapshot()" title="Close" style="font-size:22px;">&times;</button>
      </div>
      <div id="observerSnapBody" class="observer-snapshot-body">
        <div style="color:var(--text2);padding:18px;">Fetching /api/observer/peers/${escHtml(name)}/stats …</div>
      </div>
    </div>`;
  document.body.appendChild(modal);
  document.body.style.overflow = 'hidden';

  apiFetch('/api/observer/peers/' + encodeURIComponent(name) + '/stats').then(snap => {
    renderObserverSnapshot(name, snap);
  }).catch(err => {
    const body = document.getElementById('observerSnapBody');
    if (body) body.innerHTML = `<div style="color:var(--error);padding:18px;">Snapshot unavailable: ${escHtml(String(err && err.message || err))}</div>`;
  });
}

function closeObserverSnapshot() {
  const m = document.getElementById('observerSnapshotModal');
  if (m) m.remove();
  document.body.style.overflow = '';
}

function renderObserverSnapshot(name, snap) {
  const head = document.getElementById('observerSnapPeerLine');
  const body = document.getElementById('observerSnapBody');
  if (!body) return;
  const host = (snap && snap.host) || {};
  const env = (snap && snap.envelopes) || [];
  if (head) {
    head.innerHTML = `${escHtml(name)} · ${escHtml(host.name || '?')} · ${escHtml(host.os || '?')} ${escHtml(host.arch || '')} · uptime ${host.uptime_seconds||0}s`;
  }
  // Sort by cpu desc.
  env.sort((a, b) => (b.cpu_pct || 0) - (a.cpu_pct || 0));
  const rows = env.map((e, idx) => {
    const safeID = JSON.stringify(e.id || '');
    const cpu = (e.cpu_pct || 0).toFixed(1);
    const rss = Math.round((e.rss_bytes || 0) / 1e6);
    const fds = e.open_fds || 0;
    const procs = e.process_count || 0;
    return `<div class="observer-env-row" id="observerEnvRow-${idx}">
      <div class="observer-env-summary" onclick='toggleObserverEnvelope(${idx}, ${safeID}, "${escHtml(name)}")' title="Click to drill into process tree">
        <span class="observer-env-toggle" id="observerEnvToggle-${idx}">&#9656;</span>
        <span class="observer-env-kind">${escHtml(e.kind || '?')}</span>
        <span class="observer-env-id">${escHtml(e.id || '?')}</span>
        <span class="observer-env-stats">cpu ${cpu}%</span>
        <span class="observer-env-stats">rss ${rss} MB</span>
        <span class="observer-env-stats">${procs} procs · ${fds} fds</span>
      </div>
      <div class="observer-env-detail" id="observerEnvDetail-${idx}" style="display:none;">
        <div style="color:var(--text2);padding:8px 24px;">Loading process tree…</div>
      </div>
    </div>`;
  }).join('');
  const headerLine = `<div style="padding:6px 18px;color:var(--text2);font-size:12px;border-bottom:1px solid var(--border);">${env.length} envelope${env.length===1?'':'s'} · click any row to expand its process tree</div>`;
  body.innerHTML = headerLine + (env.length ? rows : '<div style="padding:18px;color:var(--text2);">No envelopes in this snapshot.</div>');
}

// toggleObserverEnvelope — expand/collapse the process tree for one
// envelope. Lazy-loads via /api/observer/envelope?id=… when first opened.
function toggleObserverEnvelope(idx, envID, peerName) {
  const detail = document.getElementById('observerEnvDetail-' + idx);
  const toggle = document.getElementById('observerEnvToggle-' + idx);
  if (!detail) return;
  if (detail.style.display !== 'none') {
    detail.style.display = 'none';
    if (toggle) toggle.innerHTML = '&#9656;';
    return;
  }
  detail.style.display = 'block';
  if (toggle) toggle.innerHTML = '&#9662;';
  // Already loaded?
  if (detail.dataset.loaded === '1') return;
  apiFetch('/api/observer/envelope?id=' + encodeURIComponent(envID)).then(env => {
    detail.dataset.loaded = '1';
    const procs = (env && env.processes) || [];
    if (!procs.length) {
      detail.innerHTML = '<div style="padding:10px 24px;color:var(--text2);">No process detail for this envelope.</div>';
      return;
    }
    procs.sort((a, b) => (b.cpu_pct || 0) - (a.cpu_pct || 0));
    const top = procs.slice(0, 50).map(p => {
      const cpu = (p.cpu_pct || 0).toFixed(1);
      const rss = Math.round((p.rss_bytes || 0) / 1e6);
      const cmd = p.cmdline || p.comm || '?';
      return `<div class="observer-proc-row">
        <span class="observer-proc-pid">${p.pid || '?'}</span>
        <span class="observer-proc-cpu">${cpu}%</span>
        <span class="observer-proc-rss">${rss} MB</span>
        <span class="observer-proc-cmd" title="${escHtml(cmd)}">${escHtml(cmd.length > 80 ? cmd.slice(0,80)+'…' : cmd)}</span>
      </div>`;
    }).join('');
    const more = procs.length > 50 ? `<div style="padding:6px 24px;color:var(--text2);font-size:11px;">+${procs.length-50} more</div>` : '';
    detail.innerHTML = top + more;
  }).catch(err => {
    detail.innerHTML = `<div style="padding:10px 24px;color:var(--error);">Failed to load envelope: ${escHtml(String(err && err.message || err))}</div>`;
  });
}

// Remove — DELETE /api/observer/peers/{name}. Peer auto-re-registers
// on the next push, so this effectively rotates its token.
function removeObserverPeer(name) {
  if (!confirm(`Remove peer "${name}"?\nIt will auto-re-register on next push (token rotates).`)) return;
  const token = localStorage.getItem('cs_token') || '';
  const headers = {};
  if (token) headers['Authorization'] = 'Bearer ' + token;
  fetch('/api/observer/peers/' + encodeURIComponent(name), { method: 'DELETE', headers })
    .then(r => {
      if (r.ok) {
        showToast(`Removed peer ${name}`, 'info');
        loadObserverPeers();
      } else {
        showToast(`Remove failed: ${r.status}`, 'error');
      }
    })
    .catch(() => showToast('Remove failed', 'error'));
}

// BL173 (S12) — cluster nodes from /api/observer/stats. Renders only
// when the payload's cluster.nodes is non-empty (single-node setups
// see no card). Aggregates across all peers + the local snapshot.
function loadObserverClusterNodes() {
  const block = document.getElementById('observerClusterBlock');
  const list = document.getElementById('observerClusterList');
  if (!block || !list) return;
  apiFetch('/api/observer/stats').then(snap => {
    const nodes = (snap && snap.cluster && snap.cluster.nodes) || [];
    if (!nodes.length) {
      block.style.display = 'none';
      return;
    }
    block.style.display = '';
    list.innerHTML = nodes.map(n => {
      const cpuPct = Math.round(n.cpu_pct || 0);
      const memPct = Math.round(n.mem_pct || 0);
      const ready = n.ready !== false;
      const dot = `<span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:${ready?'var(--success,#10b981)':'var(--error,#ef4444)'};margin-right:6px;"></span>`;
      const pressure = (n.pressure && n.pressure.length)
        ? ` <span style="opacity:0.7;color:var(--warning,#f59e0b);font-size:11px;">[${(n.pressure||[]).join(',')}]</span>` : '';
      const pods = (typeof n.pod_count === 'number') ? ` &middot; <span style="opacity:0.7;">${n.pod_count} pods</span>` : '';
      const bar = (label, p, color) => `
        <span style="display:inline-flex;align-items:center;gap:4px;margin-left:8px;">
          <span style="font-size:11px;opacity:0.7;">${label}</span>
          <span style="display:inline-block;width:60px;height:5px;background:var(--bg);border-radius:3px;overflow:hidden;">
            <span style="display:block;height:100%;width:${p}%;background:${color};"></span>
          </span>
          <span style="font-size:11px;opacity:0.7;">${p}%</span>
        </span>`;
      return `<div style="padding:4px 0;display:flex;align-items:center;flex-wrap:wrap;">${dot}<strong>${escHtml(n.name)}</strong>${pressure}${pods}${bar('cpu', cpuPct, 'var(--accent)')}${bar('mem', memPct, 'var(--accent2)')}</div>`;
    }).join('');
  }).catch(() => {
    block.style.display = 'none';
  });
}

function renderStatsData(el, data) {
    if (!data || !data.timestamp) { el.innerHTML = '<div style="color:var(--text2);font-size:12px;padding:8px;">Stats not available.</div>'; return; }
    // Preserve scroll position to prevent visible jump on real-time updates
    const scrollParent = el.closest('.settings-section') || el.parentElement;
    const savedScroll = scrollParent ? scrollParent.scrollTop : 0;
    const pageScroll = window.scrollY;
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
    const host = data.bound_interfaces?.[0] || '0.0.0.0';
    const httpPort = data.web_port || 8080;
    const tlsPort = data.tls_port || 0;
    const hasTLS = data.tls_enabled && tlsPort > 0;
    html += `<div class="stat-card"><div class="stat-label">Infrastructure</div>
      <div style="font-size:10px;font-family:monospace;color:var(--text);line-height:1.6;">
        <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">HTTP</span><span>http://${host}:${httpPort}${hasTLS ? ' <span style="color:var(--text2);">(→ HTTPS)</span>' : ''}</span></div>
        ${hasTLS ? `<div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">HTTPS</span><span style="color:var(--success);">https://${host}:${tlsPort} <span style="color:var(--success);">🔒</span></span></div>` : ''}
        ${!hasTLS && data.tls_enabled ? `<div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">TLS</span><span style="color:var(--success);">https://${host}:${httpPort} 🔒</span></div>` : ''}
        ${data.mcp_sse_port ? `<div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">MCP SSE</span><span>${data.mcp_sse_host || '0.0.0.0'}:${data.mcp_sse_port}</span></div>` : ''}
        <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Tmux</span><span>${data.tmux_sessions || 0} sessions${data.orphaned_tmux?.length ? ' <span style="color:var(--warning);">(' + data.orphaned_tmux.length + ' orphan)</span>' : ''}</span></div>
      </div></div>`;
    // RTK Token Savings
    if (data.rtk_installed) {
      const savPct = data.rtk_avg_savings_pct ? data.rtk_avg_savings_pct.toFixed(1) + '%' : '—';
      const savTok = data.rtk_total_saved ? data.rtk_total_saved.toLocaleString() : '0';
      const savCmds = data.rtk_total_commands || 0;
      // RTK install/upgrade one-liner per upstream rtk-ai/rtk install.sh.
      const rtkInstallCmd = 'curl -fsSL https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh | sh';
      // BL223 — store cmd on DOM via data-cmd; avoid JSON.stringify inside
      // onclick="..." attributes (double quotes break the HTML attribute).
      const updateBadge = data.rtk_update_available
        ? ` <span class="rtk-update-badge" style="color:var(--error);font-weight:600;cursor:pointer;" title="Click to copy upgrade command" data-cmd="${escHtml(rtkInstallCmd)}">→ ${escHtml(data.rtk_latest_version)}</span>`
        : ' <span style="color:var(--success);">✓</span>';
      const upgradeRow = data.rtk_update_available
        ? `<div style="margin-top:2px;padding:4px 6px;background:var(--bg3);border-radius:4px;font-size:9px;color:var(--text2);">Upgrade: <code class="rtk-cmd-copy" style="cursor:pointer;color:var(--accent);user-select:all;word-break:break-all;" title="Click to copy" data-cmd="${escHtml(rtkInstallCmd)}">${escHtml(rtkInstallCmd)}</code></div>`
        : '';
      html += `<div class="stat-card"><div class="stat-label">RTK Token Savings</div>
        <div style="font-size:10px;font-family:monospace;color:var(--text);line-height:1.6;">
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Version</span><span>${escHtml(data.rtk_version || '?')}${updateBadge}</span></div>
          ${upgradeRow}
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Hooks</span><span style="color:${data.rtk_hooks_active ? 'var(--success)' : 'var(--warning)'};">${data.rtk_hooks_active ? 'active' : 'inactive'}</span></div>
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Tokens saved</span><span>${savTok}</span></div>
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Avg savings</span><span>${savPct}</span></div>
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Commands</span><span>${savCmds}</span></div>
        </div></div>`;
    }
    {
      const memEnabled = data.memory_enabled;
      const memStatus = memEnabled ? '<span style="color:var(--success);">enabled</span>' : '<span style="color:var(--text2);">disabled</span>';
      html += `<div class="stat-card"><div class="stat-label">Episodic Memory</div>
        <div style="font-size:10px;font-family:monospace;color:var(--text);line-height:1.6;">
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Status</span><span>${memStatus}</span></div>`;
      if (memEnabled) {
        const encStatus = data.memory_encrypted ? `<span style="color:var(--success);">encrypted</span> (${escHtml(data.memory_key_fingerprint || '?')})` : '<span style="color:var(--text2);">plaintext</span>';
        html += `
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Backend</span><span>${escHtml(data.memory_backend || 'sqlite')}</span></div>
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Embedder</span><span>${escHtml(data.memory_embedder || '—')}</span></div>
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Encryption</span><span>${encStatus}</span></div>
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Total</span><span>${data.memory_total_count || 0}</span></div>
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Manual</span><span>${data.memory_manual_count || 0}</span></div>
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Sessions</span><span>${data.memory_session_count || 0}</span></div>
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Learnings</span><span>${data.memory_learning_count || 0}</span></div>
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">DB Size</span><span>${fmt(data.memory_db_size_bytes || 0)}</span></div>`;
      }
      html += `</div></div>`;
    }
    if (data.ollama_stats && data.ollama_stats.available) {
      const os = data.ollama_stats;
      const running = os.running_models || [];
      const totalVRAM = running.reduce((a, m) => a + (m.size_vram || 0), 0);
      html += `<div class="stat-card"><div class="stat-label">Ollama Server</div>
        <div style="font-size:10px;font-family:monospace;color:var(--text);line-height:1.6;">
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Host</span><span>${escHtml(os.host || '—')}</span></div>
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Status</span><span style="color:var(--success);">online</span></div>
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Models</span><span>${os.model_count || 0}</span></div>
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Disk Used</span><span>${fmt(os.total_size_bytes || 0)}</span></div>
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">Running</span><span>${running.length}</span></div>
          <div style="display:flex;justify-content:space-between;"><span style="color:var(--text2);">VRAM Used</span><span>${fmt(totalVRAM)}</span></div>`;
      for (const m of running) {
        html += `<div style="display:flex;justify-content:space-between;padding-left:8px;"><span style="color:var(--accent2);">${escHtml(m.name)}</span><span>${fmt(m.size_vram || 0)}</span></div>`;
      }
      html += `</div></div>`;
    } else if (data.ollama_stats) {
      html += `<div class="stat-card"><div class="stat-label">Ollama Server</div>
        <div style="font-size:10px;color:var(--error);">${escHtml(data.ollama_stats.error || 'offline')}</div></div>`;
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
    // (Orphaned tmux affordance moved to Settings → About per operator
    //  2026-04-26 — see updateAboutOrphanedTmux below. Monitor stays
    //  metric-only.)
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
    // Restore scroll position after DOM update
    if (scrollParent) scrollParent.scrollTop = savedScroll;
    window.scrollTo(0, pageScroll);
    // BL223 — wire RTK copy buttons after innerHTML (avoids inline onclick+JSON.stringify)
    el.querySelectorAll('.rtk-update-badge, .rtk-cmd-copy').forEach(node => {
      node.addEventListener('click', function() {
        const cmd = this.dataset.cmd;
        if (!cmd) return;
        navigator.clipboard.writeText(cmd).then(() => {
          const orig = this.textContent;
          this.textContent = 'copied!';
          setTimeout(() => { this.textContent = orig; }, 1500);
        });
      });
    });
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
    // Debounce/cooldown numeric settings
    const debounce = d.prompt_debounce || 3;
    const cooldown = d.notify_cooldown || 15;
    let html = '<div style="font-size:10px;color:var(--text2);padding:4px 12px;">Global patterns applied to all backends without structured channels.</div>';
    html += `<div style="padding:6px 12px;border-bottom:1px solid var(--border);">
      <div style="font-size:11px;color:var(--text2);font-weight:600;margin-bottom:4px;">Timing</div>
      <div style="display:flex;gap:12px;align-items:center;flex-wrap:wrap;">
        <label style="font-size:10px;color:var(--text2);display:flex;align-items:center;gap:4px;">
          Prompt debounce (sec):
          <input type="number" min="0" max="60" value="${debounce}" id="det_prompt_debounce" class="form-input" style="width:50px;font-size:10px;padding:2px 4px;" onchange="saveDetTiming()" />
        </label>
        <label style="font-size:10px;color:var(--text2);display:flex;align-items:center;gap:4px;">
          Notify cooldown (sec):
          <input type="number" min="0" max="300" value="${cooldown}" id="det_notify_cooldown" class="form-input" style="width:50px;font-size:10px;padding:2px 4px;" onchange="saveDetTiming()" />
        </label>
      </div>
      <div style="font-size:9px;color:var(--text2);margin-top:2px;">Debounce: wait N sec after prompt detected before alerting. Cooldown: min sec between repeat alerts.</div>
    </div>`;
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

function saveDetTiming() {
  const debounce = parseInt(document.getElementById('det_prompt_debounce')?.value) || 3;
  const cooldown = parseInt(document.getElementById('det_notify_cooldown')?.value) || 15;
  apiFetch('/api/config', { method: 'PUT', body: JSON.stringify({
    'detection.prompt_debounce': debounce,
    'detection.notify_cooldown': cooldown,
  })}).then(() => showToast('Detection timing saved', 'success', 1500))
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
  // Load i18n bundle before rendering — non-blocking on the WS connect.
  // navigate() reads from t() so the bundle must be present before the
  // first render. Falls through to English on fetch failure.
  initI18n().finally(() => {
    document.documentElement.setAttribute('lang', window._i18n.current);
    connect();
    const _initView = localStorage.getItem('cs_active_view');
    const _initSession = localStorage.getItem('cs_active_session');
    navigate(_initView || 'sessions', _initSession || undefined);
  });

  // Load initial unread alert count
  fetch('/api/alerts', { headers: tokenHeader() })
    .then(r => r.ok ? r.json() : null)
    .then(data => { if (data) { state.alertUnread = data.unread_count || 0; updateAlertBadge(); } })
    .catch(() => {});

  // v5.26.8 — cache whisper-enabled so mic-button affordances on
  // textareas + CSV-expand modals only render when transcription is
  // actually wired. Operator-reported: mic should only show if
  // whisper is configured.
  fetch('/api/config', { headers: tokenHeader() })
    .then(r => r.ok ? r.json() : null)
    .then(cfg => {
      state._whisperEnabled = !!(cfg && cfg.whisper && cfg.whisper.enabled);
    })
    .catch(() => { state._whisperEnabled = false; });

  // v5.26.8 — hide the Autonomous tab when autonomous.enabled=false.
  // Operator-reported: tabs for disabled subsystems shouldn't render
  // at all. The button starts display:none in index.html; JS unhides
  // when /api/autonomous/config returns enabled:true. If the request
  // fails (auth issue, daemon down) we leave it hidden — better to
  // be invisible than to show a tab that 503s on click.
  fetch('/api/autonomous/config', { headers: tokenHeader() })
    .then(r => r.ok ? r.json() : null)
    .then(cfg => {
      const btn = document.getElementById('navBtnAutonomous');
      if (btn && cfg && cfg.enabled === true) btn.style.display = '';
    })
    .catch(() => {});

  // BL220-G1 — Observer panel: show when /api/observer/stats responds.
  fetch('/api/observer/stats', { headers: tokenHeader() })
    .then(r => { if (r.ok) { const b = document.getElementById('navBtnObserver'); if (b) b.style.display = ''; } })
    .catch(() => {});

  // BL238 — Plugins, Routing, Orchestrator moved to Settings sub-tabs; nav visibility checks removed.

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
window.toggleScrollMode = toggleScrollMode;
window.scrollPage = scrollPage;
window.exitScrollMode = exitScrollMode;
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
window.saveDetTiming = saveDetTiming;
window.loadStatsPanel = loadStatsPanel;
window.killOrphanedTmux = killOrphanedTmux;

// ── BL220-G1 Observer panel ───────────────────────────────────────────────────

function renderObserverView() {
  const view = document.getElementById('view');
  if (!view) return;
  view.innerHTML = `<div class="view-content"><div style="padding:12px;" id="observerPanelBody"><div style="text-align:center;padding:32px;">${escHtml(t('common_loading'))}</div></div></div>`;
  Promise.all([
    apiFetch('/api/observer/stats').catch(() => null),
    apiFetch('/api/observer/peers').catch(() => ({ peers: [] })),
    apiFetch('/api/observer/config').catch(() => null),
  ]).then(([stats, peersData, cfg]) => {
    const el = document.getElementById('observerPanelBody');
    if (!el) return;
    const peers = (peersData && peersData.peers) || [];
    const statsRow = stats ? `<div style="display:flex;gap:8px;flex-wrap:wrap;margin-bottom:12px;">${
      Object.entries(stats).filter(([,v]) => typeof v !== 'object').map(([k, v]) =>
        `<div style="background:var(--bg2);border:1px solid var(--border);border-radius:8px;padding:8px 12px;font-size:12px;min-width:80px;">
          <div style="opacity:0.6;font-size:10px;text-transform:uppercase;letter-spacing:0.5px;">${escHtml(k)}</div>
          <div style="font-weight:600;">${escHtml(String(v))}</div>
        </div>`).join('')
    }</div>` : '';
    const cfgRow = cfg && Object.keys(cfg).length ? `<div style="background:var(--bg2);border:1px solid var(--border);border-radius:8px;padding:10px 12px;margin-bottom:12px;font-size:12px;">
      <div style="font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:0.5px;opacity:0.6;margin-bottom:6px;">Config</div>
      ${Object.entries(cfg).map(([k, v]) => {
        const display = (v !== null && typeof v === 'object')
          ? `<span style="opacity:0.5;font-size:10px;">{${Object.keys(v).join(', ')}}</span>`
          : `<strong>${escHtml(String(v))}</strong>`;
        return `<div style="display:flex;justify-content:space-between;padding:2px 0;"><span style="opacity:0.7;">${escHtml(k)}</span>${display}</div>`;
      }).join('')}
    </div>` : '';
    const now = Date.now();
    const peerRows = peers.length === 0
      ? '<div style="opacity:0.7;padding:8px 0;">no peers registered</div>'
      : peers.map(p => {
          const lastPush = p.last_push_at ? new Date(p.last_push_at).getTime() : 0;
          const ageMs = lastPush ? (now - lastPush) : Infinity;
          const dotColor = lastPush ? (ageMs < 15000 ? 'var(--success,#10b981)' : ageMs < 60000 ? 'var(--warning,#f59e0b)' : 'var(--error,#ef4444)') : 'var(--text2)';
          const shapeText = ({A:'agent',B:'standalone',C:'cluster'}[(p.shape||'').toUpperCase()]) || (p.shape||'?');
          const ageLabel = lastPush ? observerPeerAgo(ageMs) : 'never';
          const safeName = JSON.stringify(p.name || '');
          return `<div style="padding:6px 0;border-top:1px solid var(--border);display:flex;align-items:center;flex-wrap:wrap;">
            <span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:${dotColor};margin-right:6px;flex-shrink:0;"></span>
            <strong>${escHtml(p.name)}</strong>
            <span style="opacity:0.55;font-size:10px;border:1px solid var(--text2);border-radius:3px;padding:0 4px;margin-left:4px;">${escHtml(shapeText)}</span>
            <span style="opacity:0.6;font-size:11px;margin-left:6px;">${escHtml(ageLabel)}</span>
            <div style="margin-left:auto;">
              <button class="btn-icon" style="font-size:11px;padding:1px 6px;" onclick='showObserverPeerSnapshot(${safeName})' title="Snapshot">&#128202;</button>
              <button class="btn-icon" style="font-size:11px;padding:1px 6px;" onclick='removeObserverPeer(${safeName})' title="Remove">&times;</button>
            </div>
          </div>`;
        }).join('');
    el.innerHTML = statsRow + cfgRow
      + `<div style="font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:0.5px;opacity:0.6;margin-bottom:6px;">Peers (${peers.length}) <button class="btn-icon" style="margin-left:6px;font-size:11px;padding:2px 8px;" onclick="renderObserverView()">↻</button></div>`
      + peerRows;
  }).catch(err => {
    const el = document.getElementById('observerPanelBody');
    if (el) el.innerHTML = `<div style="color:var(--error);padding:16px;">${escHtml(String(err.message||err))}</div>`;
  });
}
window.renderObserverView = renderObserverView;

// ── BL220-G2 Plugins panel ────────────────────────────────────────────────────

// BL219 — loadToolingPanel: shows per-backend artifact status in Settings → General.
function loadToolingPanel() {
  const el = document.getElementById('toolingStatusPanel');
  if (!el) return;
  apiFetch('/api/tooling/status').then(data => {
    const backends = Array.isArray(data.backends) ? data.backends : (data.status ? [data.status] : []);
    if (backends.length === 0) {
      el.innerHTML = '<div style="opacity:0.7;font-size:12px;padding:8px 12px;">No backends configured.</div>';
      return;
    }
    el.innerHTML = backends.map(b => {
      const present = (b.present||[]).join(', ') || 'none';
      const ignored = b.ignored ? '✓' : '⚠ not ignored';
      return `<div style="padding:6px 12px;border-top:1px solid var(--border);font-size:12px;display:flex;gap:8px;align-items:center;">
        <code style="width:90px;color:var(--accent);">${escHtml(b.backend)}</code>
        <span style="flex:1;color:var(--text2);">present: ${escHtml(present)}</span>
        <span style="font-size:11px;color:${b.ignored?'var(--ok, #4caf50)':'var(--warn, #ff9800)'};">${ignored}</span>
        <button class="btn-secondary" style="font-size:10px;padding:2px 6px;" onclick="toolingGitignore(${JSON.stringify(b.backend)})">gitignore</button>
        <button class="btn-secondary" style="font-size:10px;padding:2px 6px;color:var(--error);" onclick="toolingCleanup(${JSON.stringify(b.backend)})">cleanup</button>
      </div>`;
    }).join('');
  }).catch(() => { el.innerHTML = '<span style="color:var(--error);font-size:12px;padding:8px 12px;">Failed to load tooling status.</span>'; });
}
window.loadToolingPanel = loadToolingPanel;
window.toolingGitignore = function(backend) {
  apiFetch('/api/tooling/gitignore', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify({ backend }) })
    .then(d => { showToast(`gitignore: ${d.patterns_added||0} pattern(s) added`, 'success', 2500); loadToolingPanel(); })
    .catch(e => showToast(String(e.message||e), 'error'));
};
window.toolingCleanup = function(backend) {
  apiFetch('/api/tooling/cleanup', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify({ backend }) })
    .then(d => { showToast(`cleanup: ${(d.removed||[]).length} file(s) removed`, 'success', 2500); loadToolingPanel(); })
    .catch(e => showToast(String(e.message||e), 'error'));
};

// BL238 — loadPluginsPanel: populates #pluginsPanelBody inside Settings → Plugins sub-tab.
function loadPluginsPanel() {
  const el = document.getElementById('pluginsPanelBody');
  if (!el) return;
  el.innerHTML = '<div style="text-align:center;padding:24px;color:var(--text2);font-size:13px;">Loading…</div>';
  apiFetch('/api/plugins').then(data => {
    const panel = document.getElementById('pluginsPanelBody');
    if (!panel) return;
    const native = (data && data.native) || [];
    const subs = (data && data.plugins) || [];
    const renderPlugin = (p, isNative) => {
      const on = !!p.enabled;
      const dot = `<span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:${on?'var(--success,#10b981)':'var(--text2)'};margin-right:6px;flex-shrink:0;"></span>`;
      const tag = isNative ? `<span style="opacity:0.55;font-size:10px;border:1px solid var(--text2);border-radius:3px;padding:0 4px;margin-left:4px;">native</span>` : '';
      const ver = p.version ? ` <span style="opacity:0.6;font-size:11px;">v${escHtml(p.version)}</span>` : '';
      const desc = p.description ? `<div style="opacity:0.6;font-size:11px;margin-top:2px;">${escHtml(p.description)}</div>` : '';
      const lastErr = p.last_error ? `<div style="color:var(--error);font-size:11px;margin-top:2px;" title="${escHtml(p.last_error)}">⚠ last error</div>` : '';
      const acts = isNative ? '' : `<button class="btn-icon" style="font-size:11px;padding:2px 8px;white-space:nowrap;" onclick="pluginAction('${escHtml(p.name)}','${on?'disable':'enable'}')">${on?'Disable':'Enable'}</button>`;
      return `<div style="padding:8px 0;border-top:1px solid var(--border);display:flex;align-items:flex-start;gap:8px;">
        <div style="flex:1;">${dot}<strong>${escHtml(p.name)}</strong>${tag}${ver}${desc}${lastErr}</div>
        ${acts}
      </div>`;
    };
    panel.innerHTML = `<button class="btn-primary" style="font-size:12px;padding:6px 16px;margin-bottom:12px;" onclick="pluginReload()">Reload plugins</button>`
      + (native.length ? `<div style="font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:0.5px;opacity:0.6;margin-bottom:4px;">Native</div>${native.map(p=>renderPlugin(p,true)).join('')}` : '')
      + `<div style="font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:0.5px;opacity:0.6;margin-top:12px;margin-bottom:4px;">Subprocess</div>`
      + (subs.length ? subs.map(p=>renderPlugin(p,false)).join('') : '<div style="opacity:0.7;">no subprocess plugins installed</div>');
  }).catch(err => {
    const panel = document.getElementById('pluginsPanelBody');
    if (panel) panel.innerHTML = `<div style="color:var(--error);padding:16px;">${escHtml(String(err.message||err))}</div>`;
  });
}
window.loadPluginsPanel = loadPluginsPanel;

// BL238 — renderPluginsView redirects to Settings → Plugins sub-tab.
function renderPluginsView() {
  _settingsTab = 'plugins';
  localStorage.setItem('cs_settings_tab', 'plugins');
  navigate('settings');
}
window.renderPluginsView = renderPluginsView;
window.pluginAction = function(name, action) {
  apiFetch(`/api/plugins/${encodeURIComponent(name)}/${action}`, { method: 'POST' })
    .then(() => { showToast(`Plugin ${escHtml(name)} ${action}d`, 'success', 2000); loadPluginsPanel(); })
    .catch(e => showToast(String(e.message||e), 'error'));
};
window.pluginReload = function() {
  apiFetch('/api/plugins/reload', { method: 'POST' })
    .then(d => { showToast(`Reloaded: ${d.count||0} plugin(s)`, 'success', 2000); loadPluginsPanel(); })
    .catch(e => showToast(String(e.message||e), 'error'));
};

// ── BL220-G3 Routing rules editor ────────────────────────────────────────────

// BL238 — loadRoutingPanel: populates #routingPanelBody inside Settings → Routing sub-tab.
function loadRoutingPanel() {
  const el = document.getElementById('routingPanelBody');
  if (!el) return;
  el.innerHTML = '<div style="text-align:center;padding:24px;color:var(--text2);font-size:13px;">Loading…</div>';
  apiFetch('/api/routing-rules').then(data => {
    const panel = document.getElementById('routingPanelBody');
    if (!panel) return;
    const rules = (data && data.rules) || [];
    const inp = (id, ph) => `<input id="${id}" type="text" placeholder="${ph}" style="width:100%;box-sizing:border-box;margin-bottom:6px;font-size:13px;padding:6px 8px;background:var(--bg);border:1px solid var(--border);border-radius:6px;color:var(--text);">`;
    panel.innerHTML = `
      <div style="background:var(--bg2);border:1px solid var(--border);border-radius:8px;padding:10px 12px;margin-bottom:10px;">
        <div style="font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:0.5px;opacity:0.6;margin-bottom:8px;">Add rule</div>
        ${inp('routingPattern','Pattern (regex)')}${inp('routingBackend','Backend name')}${inp('routingDesc','Description (optional)')}
        <button class="btn-primary" style="font-size:12px;padding:6px 16px;" onclick="routingAddRule()">Add</button>
      </div>
      <div style="background:var(--bg2);border:1px solid var(--border);border-radius:8px;padding:10px 12px;margin-bottom:10px;">
        <div style="font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:0.5px;opacity:0.6;margin-bottom:8px;">Test routing</div>
        <div style="display:flex;gap:6px;">
          <input id="routingTestTask" type="text" placeholder="Task text…" style="flex:1;font-size:13px;padding:6px 8px;background:var(--bg);border:1px solid var(--border);border-radius:6px;color:var(--text);">
          <button class="btn-primary" style="font-size:12px;padding:6px 12px;" onclick="routingTest()">Test</button>
        </div>
        <div id="routingTestResult" style="margin-top:6px;font-size:12px;color:var(--text2);"></div>
      </div>
      <div style="font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:0.5px;opacity:0.6;margin-bottom:6px;">Rules (${rules.length})</div>
      ${rules.length === 0
        ? '<div style="opacity:0.7;padding:4px 0;">no rules — tasks route to the default backend</div>'
        : rules.map((r, i) => `<div style="padding:8px 0;border-top:1px solid var(--border);display:flex;align-items:flex-start;gap:8px;">
            <div style="flex:1;">
              <code style="font-size:12px;">${escHtml(r.pattern||'')}</code>
              <span style="margin-left:6px;font-size:11px;background:rgba(96,165,250,0.18);color:var(--accent);padding:1px 6px;border-radius:4px;">${escHtml(r.backend||'')}</span>
              ${r.description ? `<div style="opacity:0.6;font-size:11px;margin-top:2px;">${escHtml(r.description)}</div>` : ''}
            </div>
            <button class="btn-icon" style="font-size:13px;color:var(--error);" onclick="routingDeleteRule(${i})">&times;</button>
          </div>`).join('')}`;
    panel._rules = rules;
  }).catch(err => {
    const panel = document.getElementById('routingPanelBody');
    if (panel) panel.innerHTML = `<div style="color:var(--error);padding:16px;">${escHtml(String(err.message||err))}</div>`;
  });
}
window.loadRoutingPanel = loadRoutingPanel;

// BL238 — renderRoutingView redirects to Settings → Routing sub-tab.
function renderRoutingView() {
  _settingsTab = 'routing';
  localStorage.setItem('cs_settings_tab', 'routing');
  navigate('settings');
}
window.renderRoutingView = renderRoutingView;
window.routingAddRule = function() {
  const pattern = (document.getElementById('routingPattern')||{}).value||'';
  const backend = (document.getElementById('routingBackend')||{}).value||'';
  const desc = (document.getElementById('routingDesc')||{}).value||'';
  if (!pattern || !backend) { showToast('Pattern and backend are required', 'error'); return; }
  const el = document.getElementById('routingPanelBody');
  const rules = [...((el&&el._rules)||[]), { pattern, backend, description: desc }];
  apiFetch('/api/routing-rules', { method: 'POST', body: JSON.stringify({ rules }) })
    .then(() => { showToast('Rule added', 'success', 2000); loadRoutingPanel(); })
    .catch(e => showToast(String(e.message||e), 'error'));
};
window.routingDeleteRule = function(idx) {
  const el = document.getElementById('routingPanelBody');
  const rules = ((el&&el._rules)||[]).filter((_,i)=>i!==idx);
  apiFetch('/api/routing-rules', { method: 'POST', body: JSON.stringify({ rules }) })
    .then(() => { showToast('Rule deleted', 'success', 2000); loadRoutingPanel(); })
    .catch(e => showToast(String(e.message||e), 'error'));
};
window.routingTest = function() {
  const task = (document.getElementById('routingTestTask')||{}).value||'';
  if (!task) return;
  const res = document.getElementById('routingTestResult');
  if (res) res.textContent = 'testing…';
  apiFetch('/api/routing-rules/test', { method: 'POST', body: JSON.stringify({ task }) })
    .then(d => { if (res) res.innerHTML = d.matched
      ? `<span style="color:var(--success,#10b981);">✓ routes to <strong>${escHtml(d.backend)}</strong></span>`
      : '<span style="opacity:0.7;">no match — uses default backend</span>'; })
    .catch(e => { if (res) res.textContent = 'error: ' + String(e.message||e); });
};

// ── BL220-G15 Orchestrator panel ──────────────────────────────────────────────

// BL238 — loadOrchestratorPanel: populates #orchestratorPanelBody inside Settings → Orchestrator sub-tab.
function loadOrchestratorPanel() {
  const el = document.getElementById('orchestratorPanelBody');
  if (!el) return;
  el.innerHTML = '<div style="text-align:center;padding:24px;color:var(--text2);font-size:13px;">Loading…</div>';
  apiFetch('/api/orchestrator/graphs').then(data => {
    const panel = document.getElementById('orchestratorPanelBody');
    if (!panel) return;
    const graphs = (data && data.graphs) || [];
    const statusColor = { pending:'var(--text2)', running:'var(--accent,#6366f1)', done:'var(--success,#10b981)', failed:'var(--error,#ef4444)', cancelled:'var(--warning,#f59e0b)' };
    panel.innerHTML = `
      <div style="background:var(--bg2);border:1px solid var(--border);border-radius:8px;padding:10px 12px;margin-bottom:10px;">
        <div style="font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:0.5px;opacity:0.6;margin-bottom:8px;">New PRD graph</div>
        <input id="orchTitle" type="text" placeholder="Title (required)" style="width:100%;box-sizing:border-box;margin-bottom:6px;font-size:13px;padding:6px 8px;background:var(--bg);border:1px solid var(--border);border-radius:6px;color:var(--text);">
        <input id="orchDir" type="text" placeholder="Project directory (optional)" style="width:100%;box-sizing:border-box;margin-bottom:8px;font-size:13px;padding:6px 8px;background:var(--bg);border:1px solid var(--border);border-radius:6px;color:var(--text);">
        <button class="btn-primary" style="font-size:12px;padding:6px 16px;" onclick="orchCreateGraph()">Create</button>
      </div>
      <div style="font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:0.5px;opacity:0.6;margin-bottom:6px;">
        Graphs (${graphs.length}) <button class="btn-icon" style="margin-left:6px;font-size:11px;padding:2px 8px;" onclick="loadOrchestratorPanel()">↻</button>
      </div>
      ${graphs.length === 0
        ? '<div style="opacity:0.7;padding:4px 0;">no graphs — create one above</div>'
        : graphs.map(g => {
            const color = statusColor[g.status||'pending'] || 'var(--text2)';
            const prdCount = (g.prd_ids||g.nodes||[]).length;
            const safeId = JSON.stringify(g.id||'');
            return `<div style="padding:8px 0;border-top:1px solid var(--border);display:flex;align-items:flex-start;gap:8px;">
              <div style="flex:1;">
                <div style="display:flex;align-items:center;gap:6px;">
                  <span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:${color};flex-shrink:0;"></span>
                  <strong>${escHtml(g.title||g.id||'untitled')}</strong>
                  <span style="opacity:0.6;font-size:11px;">${escHtml(g.status||'pending')}</span>
                </div>
                ${prdCount ? `<div style="opacity:0.6;font-size:11px;margin-top:2px;">${prdCount} PRD${prdCount===1?'':'s'}</div>` : ''}
                <div style="opacity:0.5;font-size:10px;font-family:monospace;">${escHtml(g.id||'')}</div>
              </div>
              <div style="display:flex;gap:4px;align-items:center;">
                <button class="btn-icon" style="font-size:11px;padding:2px 8px;" onclick='orchRunGraph(${safeId})' title="Run">▶</button>
                <button class="btn-icon" style="font-size:11px;padding:2px 8px;color:var(--error);" onclick='orchDeleteGraph(${safeId})' title="Cancel">&times;</button>
              </div>
            </div>`;
          }).join('')}`;
  }).catch(err => {
    const panel = document.getElementById('orchestratorPanelBody');
    if (panel) panel.innerHTML = `<div style="color:var(--error);padding:16px;">${escHtml(String(err.message||'Orchestrator unavailable — set orchestrator.enabled: true'))}</div>`;
  });
}
window.loadOrchestratorPanel = loadOrchestratorPanel;

// BL238 — renderOrchestratorView redirects to Settings → Orchestrator sub-tab.
function renderOrchestratorView() {
  _settingsTab = 'orchestrator';
  localStorage.setItem('cs_settings_tab', 'orchestrator');
  navigate('settings');
}
window.renderOrchestratorView = renderOrchestratorView;
window.orchCreateGraph = function() {
  const title = (document.getElementById('orchTitle')||{}).value||'';
  const dir = (document.getElementById('orchDir')||{}).value||'';
  if (!title) { showToast('Title is required', 'error'); return; }
  const body = { title, prd_ids: [] };
  if (dir) body.project_dir = dir;
  apiFetch('/api/orchestrator/graphs', { method: 'POST', body: JSON.stringify(body) })
    .then(() => { showToast('Graph created', 'success', 2000); loadOrchestratorPanel(); })
    .catch(e => showToast(String(e.message||e), 'error'));
};
window.orchRunGraph = function(id) {
  apiFetch(`/api/orchestrator/graphs/${encodeURIComponent(id)}/run`, { method: 'POST' })
    .then(() => { showToast('Run started', 'success', 2000); loadOrchestratorPanel(); })
    .catch(e => showToast(String(e.message||e), 'error'));
};
window.orchDeleteGraph = function(id) {
  showConfirmModal('Cancel/delete this graph?', () => {
    apiFetch(`/api/orchestrator/graphs/${encodeURIComponent(id)}`, { method: 'DELETE' })
      .then(() => { showToast('Graph cancelled', 'success', 2000); loadOrchestratorPanel(); })
      .catch(e => showToast(String(e.message||e), 'error'));
  });
};

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
        .then(() => {
          showToast('Orphaned sessions killed', 'success', 2000);
          if (typeof loadStatsPanel === 'function') loadStatsPanel();
          if (typeof loadAboutOrphanedTmux === 'function') loadAboutOrphanedTmux();
        })
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
window.selectServer = selectServer;
window.toggleProxySetting = toggleProxySetting;
window.updateProxySetting = updateProxySetting;
window.showResponseViewer = showResponseViewer;
window.copyResponseText = copyResponseText;
window.chatRememberContent = chatRememberContent;
window.chatQuickCmd = chatQuickCmd;

function chatQuickCmd(prefix) {
  const input = document.getElementById('sessionInput');
  if (input) { input.value = prefix; input.focus(); }
}
window.loadMemoryStats = loadMemoryStats;
window.listMemories = listMemories;
window.searchMemories = searchMemories;
window.deleteMemory = deleteMemory;
window.exportMemories = exportMemories;
window.testMemoryConnection = testMemoryConnection;
window.testAndEnableMemory = testAndEnableMemory;

// F10 sprint 2 — Project + Cluster Profile UI handlers
window.loadProjectProfiles = loadProjectProfiles;
window.loadClusterProfiles = loadClusterProfiles;
window.openProfileEditor = openProfileEditor;
window.cancelProfileEditor = cancelProfileEditor;
window.toggleProfileYaml = toggleProfileYaml;
window.saveProfileEditor = saveProfileEditor;
window.smokeProfile = smokeProfile;
window.deleteProfile = deleteProfile;

function testMemoryConnection() {
  showToast('Testing Ollama embedding…', 'info', 2000);
  apiFetch('/api/memory/test')
    .then(data => {
      if (data.success) {
        showToast(`Ollama OK: ${data.model} (${data.dimensions}d vectors)`, 'success', 4000);
      } else {
        showToast(`Ollama test failed: ${data.error}`, 'error', 6000);
      }
    })
    .catch(e => showToast('Test failed: ' + e.message, 'error'));
}

function testAndEnableMemory(checkbox) {
  if (checkbox.checked) {
    // Enabling — test first
    showToast('Testing Ollama before enabling memory…', 'info', 2000);
    apiFetch('/api/memory/test')
      .then(data => {
        if (data.success) {
          saveGeneralField('memory.enabled', true);
          showToast(`Memory enabled (${data.model}, ${data.dimensions}d)`, 'success', 3000);
        } else {
          checkbox.checked = false;
          showToast(`Cannot enable memory: ${data.error}`, 'error', 6000);
        }
      })
      .catch(e => {
        checkbox.checked = false;
        showToast('Cannot enable memory: ' + e.message, 'error');
      });
  } else {
    // Disabling — no test needed
    saveGeneralField('memory.enabled', false);
  }
}

// ── BL220-G6 — Cost rates editor ──────────────────────────────────────────────

function loadCostRatesConfig() {
  const el = document.getElementById('costRatesList');
  if (!el) return;
  apiFetch('/api/cost/rates').then(data => {
    const rates = data?.rates || {};
    const backends = Object.keys(rates).sort();
    if (backends.length === 0) {
      el.innerHTML = '<div style="color:var(--text2);font-size:12px;padding:8px 0;">No rate data — daemon unavailable.</div>';
      return;
    }
    el.innerHTML = `
      <div style="font-size:11px;color:var(--text2);padding:0 0 8px;">USD per 1,000 tokens. Leave blank to keep current value.</div>
      <table style="width:100%;font-size:12px;border-collapse:collapse;">
        <thead>
          <tr>
            <th style="text-align:left;padding:4px 6px;color:var(--text2);font-weight:600;font-size:11px;text-transform:uppercase;">Backend</th>
            <th style="text-align:right;padding:4px 6px;color:var(--text2);font-weight:600;font-size:11px;text-transform:uppercase;">In / 1K</th>
            <th style="text-align:right;padding:4px 6px;color:var(--text2);font-weight:600;font-size:11px;text-transform:uppercase;">Out / 1K</th>
          </tr>
        </thead>
        <tbody>
          ${backends.map(name => {
            const r = rates[name] || {};
            return `<tr>
              <td style="padding:4px 6px;color:var(--text);">${escHtml(name)}</td>
              <td style="padding:4px 6px;text-align:right;">
                <input type="number" step="0.0001" min="0" class="form-input cost-rate-in" data-backend="${escHtml(name)}"
                  value="${r.in_per_k != null ? r.in_per_k : ''}" style="width:80px;font-size:11px;text-align:right;" placeholder="default" />
              </td>
              <td style="padding:4px 6px;text-align:right;">
                <input type="number" step="0.0001" min="0" class="form-input cost-rate-out" data-backend="${escHtml(name)}"
                  value="${r.out_per_k != null ? r.out_per_k : ''}" style="width:80px;font-size:11px;text-align:right;" placeholder="default" />
              </td>
            </tr>`;
          }).join('')}
        </tbody>
      </table>
      <div style="display:flex;gap:8px;padding:10px 0 4px;align-items:center;">
        <button class="btn-primary" style="font-size:12px;" onclick="saveCostRates()">Save rates</button>
        <button class="btn-secondary" style="font-size:12px;" onclick="resetCostRates()">Reset to defaults</button>
        <span id="costRatesSaveStatus" style="font-size:11px;color:var(--text2);"></span>
      </div>`;
  }).catch(() => { el.innerHTML = '<span style="color:var(--error);font-size:12px;">Failed to load cost rates.</span>'; });
}
window.loadCostRatesConfig = loadCostRatesConfig;

function saveCostRates() {
  const rates = {};
  document.querySelectorAll('.cost-rate-in').forEach(inp => {
    const name = inp.dataset.backend;
    const outInp = document.querySelector(`.cost-rate-out[data-backend="${CSS.escape(name)}"]`);
    const inVal = parseFloat(inp.value);
    const outVal = parseFloat(outInp?.value || '');
    rates[name] = {
      in_per_k:  isNaN(inVal)  ? 0 : inVal,
      out_per_k: isNaN(outVal) ? 0 : outVal,
    };
  });
  apiFetch('/api/cost/rates', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ rates }),
  }).then(() => {
    const s = document.getElementById('costRatesSaveStatus');
    if (s) { s.textContent = 'Saved.'; s.style.color = 'var(--success,#22c55e)'; setTimeout(() => { if (s) s.textContent = ''; }, 2500); }
  }).catch(() => showToast('Failed to save cost rates', 'error'));
}
window.saveCostRates = saveCostRates;

function resetCostRates() {
  apiFetch('/api/cost/rates', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ rates: {} }),
  }).then(() => { showToast('Cost rates reset to defaults', 'success', 2000); loadCostRatesConfig(); })
    .catch(() => showToast('Failed to reset cost rates', 'error'));
}
window.resetCostRates = resetCostRates;

// ── BL220-G10 — Global cooldown controls ──────────────────────────────────────

function loadCooldownStatus() {
  const el = document.getElementById('cooldownStatus');
  if (!el) return;
  apiFetch('/api/cooldown').then(data => {
    const active = !!data?.active;
    const untilMs = data?.until_unix_ms || 0;
    const reason = data?.reason || '';
    const remaining = untilMs ? Math.max(0, Math.ceil((untilMs - Date.now()) / 60000)) : 0;
    const statusHtml = active
      ? `<span style="color:var(--warning,#f59e0b);font-weight:600;">&#9888; Active — ${remaining}m remaining${reason ? ' — ' + escHtml(reason) : ''}</span>`
      : `<span style="color:var(--success,#22c55e);font-weight:600;">&#10003; No active cooldown</span>`;
    el.innerHTML = `
      <div style="padding:4px 0 10px;font-size:12px;">${statusHtml}</div>
      <div style="display:flex;gap:6px;flex-wrap:wrap;align-items:center;margin-bottom:8px;">
        <span style="font-size:11px;color:var(--text2);white-space:nowrap;">Set for:</span>
        ${[15, 30, 60, 240, 480, 1440].map(m =>
          `<button class="btn-secondary" style="font-size:11px;" onclick="setCooldown(${m})">${m >= 60 ? (m / 60) + 'h' : m + 'm'}</button>`
        ).join('')}
      </div>
      <div style="display:flex;gap:6px;align-items:center;">
        <input type="text" id="cooldownReason" class="form-input" style="flex:1;font-size:11px;" placeholder="Reason (optional)" />
        ${active ? `<button class="btn-secondary" style="font-size:12px;background:rgba(239,68,68,0.15);color:#ef4444;" onclick="clearCooldown()">Clear</button>` : ''}
      </div>`;
  }).catch(() => { el.innerHTML = '<span style="color:var(--error);font-size:12px;">Failed to load cooldown status.</span>'; });
}
window.loadCooldownStatus = loadCooldownStatus;

function setCooldown(minutes) {
  const reason = document.getElementById('cooldownReason')?.value || '';
  apiFetch('/api/cooldown', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ until_unix_ms: Date.now() + minutes * 60000, reason }),
  }).then(() => {
    showToast('Cooldown set for ' + (minutes >= 60 ? (minutes / 60) + 'h' : minutes + 'm'), 'success', 2000);
    loadCooldownStatus();
  }).catch(() => showToast('Failed to set cooldown', 'error'));
}
window.setCooldown = setCooldown;

function clearCooldown() {
  apiFetch('/api/cooldown', { method: 'DELETE' })
    .then(() => { showToast('Cooldown cleared', 'success', 2000); loadCooldownStatus(); })
    .catch(() => showToast('Failed to clear cooldown', 'error'));
}
window.clearCooldown = clearCooldown;

// ── BL220 Bundle F — Template management ──────────────────────────────────────

function loadTemplatesPanel() {
  const el = document.getElementById('templatesList');
  if (!el) return;
  apiFetch('/api/templates').then(data => {
    const templates = Array.isArray(data) ? data : [];
    const inp = (id, ph) => `<input id="${id}" type="text" placeholder="${escHtml(ph)}" style="flex:1;min-width:80px;font-size:11px;padding:4px 6px;background:var(--bg);border:1px solid var(--border);border-radius:4px;color:var(--text);" />`;
    el.innerHTML = `
      <details style="margin-bottom:8px;">
        <summary style="cursor:pointer;font-size:11px;font-weight:600;color:var(--accent);padding:4px 0;">+ Add template</summary>
        <div style="display:flex;flex-wrap:wrap;gap:4px;padding:6px 0;">
          ${inp('tmplName','Name (required)')}${inp('tmplBackend','Backend')}${inp('tmplDir','Project dir')}${inp('tmplEffort','Effort')}${inp('tmplDesc','Description')}
          <button class="btn-primary" style="font-size:11px;padding:4px 12px;" onclick="templateCreate()">Add</button>
        </div>
      </details>
      ${templates.length === 0
        ? '<div style="opacity:0.7;font-size:12px;">No templates — add one above or via YAML <code>session.templates</code>.</div>'
        : templates.map(t => {
            const safeName = JSON.stringify(t.name||'');
            const meta = [t.backend, t.profile, t.effort, t.project_dir].filter(Boolean).join(' · ');
            return `<div style="padding:6px 0;border-top:1px solid var(--border);display:flex;align-items:flex-start;gap:6px;">
              <div style="flex:1;">
                <strong style="font-size:12px;">${escHtml(t.name||'')}</strong>
                ${meta ? `<span style="opacity:0.6;font-size:10px;margin-left:4px;">${escHtml(meta)}</span>` : ''}
                ${t.description ? `<div style="opacity:0.6;font-size:10px;margin-top:1px;">${escHtml(t.description)}</div>` : ''}
              </div>
              <button class="btn-icon" style="font-size:12px;color:var(--error);" onclick='templateDelete(${safeName})'>&times;</button>
            </div>`;
          }).join('')}`;
  }).catch(() => { el.innerHTML = '<span style="color:var(--error);font-size:12px;">Failed to load templates.</span>'; });
}
window.loadTemplatesPanel = loadTemplatesPanel;
window.templateCreate = function() {
  const name = (document.getElementById('tmplName')||{}).value||'';
  if (!name) { showToast('Name is required', 'error'); return; }
  const body = { name };
  ['backend','dir','effort','desc'].forEach((k,i) => {
    const keys = ['backend','project_dir','effort','description'];
    const val = (document.getElementById('tmpl'+k.charAt(0).toUpperCase()+k.slice(1))||{}).value||'';
    if (val) body[keys[i]] = val;
  });
  apiFetch('/api/templates', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify(body) })
    .then(() => { showToast('Template created', 'success', 2000); loadTemplatesPanel(); })
    .catch(e => showToast(String(e.message||e), 'error'));
};
window.templateDelete = function(name) {
  showConfirmModal(`Delete template "${name}"?`, () => {
    apiFetch(`/api/templates/${encodeURIComponent(name)}`, { method: 'DELETE' })
      .then(() => { showToast('Template deleted', 'success', 2000); loadTemplatesPanel(); })
      .catch(e => showToast(String(e.message||e), 'error'));
  });
};

// ── BL220 Bundle F — Device alias manager ─────────────────────────────────────

function loadDeviceAliasesPanel() {
  const el = document.getElementById('deviceAliasesList');
  if (!el) return;
  apiFetch('/api/device-aliases').then(data => {
    const aliases = Array.isArray(data) ? data : [];
    el.innerHTML = `
      <details style="margin-bottom:8px;">
        <summary style="cursor:pointer;font-size:11px;font-weight:600;color:var(--accent);padding:4px 0;">+ Add alias</summary>
        <div style="display:flex;gap:4px;flex-wrap:wrap;padding:6px 0;">
          <input id="aliasName" type="text" placeholder="Alias (short name)" style="flex:1;min-width:80px;font-size:11px;padding:4px 6px;background:var(--bg);border:1px solid var(--border);border-radius:4px;color:var(--text);" />
          <input id="aliasServer" type="text" placeholder="Server name" style="flex:1;min-width:80px;font-size:11px;padding:4px 6px;background:var(--bg);border:1px solid var(--border);border-radius:4px;color:var(--text);" />
          <button class="btn-primary" style="font-size:11px;padding:4px 12px;" onclick="aliasCreate()">Add</button>
        </div>
      </details>
      ${aliases.length === 0
        ? '<div style="opacity:0.7;font-size:12px;">No aliases — add one above or via YAML <code>device_aliases</code>.</div>'
        : aliases.map(a => {
            const safeAlias = JSON.stringify(a.alias||'');
            return `<div style="padding:5px 0;border-top:1px solid var(--border);display:flex;align-items:center;gap:6px;">
              <code style="font-size:12px;flex:1;">${escHtml(a.alias||'')}</code>
              <span style="opacity:0.6;font-size:11px;">→ ${escHtml(a.server||'')}</span>
              <button class="btn-icon" style="font-size:12px;color:var(--error);" onclick='aliasDelete(${safeAlias})'>&times;</button>
            </div>`;
          }).join('')}`;
  }).catch(() => { el.innerHTML = '<span style="color:var(--error);font-size:12px;">Failed to load device aliases.</span>'; });
}
window.loadDeviceAliasesPanel = loadDeviceAliasesPanel;
window.aliasCreate = function() {
  const alias = (document.getElementById('aliasName')||{}).value||'';
  const server = (document.getElementById('aliasServer')||{}).value||'';
  if (!alias || !server) { showToast('Alias and server are required', 'error'); return; }
  apiFetch('/api/device-aliases', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify({ alias, server }) })
    .then(() => { showToast('Alias created', 'success', 2000); loadDeviceAliasesPanel(); })
    .catch(e => showToast(String(e.message||e), 'error'));
};
window.aliasDelete = function(alias) {
  apiFetch(`/api/device-aliases/${encodeURIComponent(alias)}`, { method: 'DELETE' })
    .then(() => { showToast('Alias deleted', 'success', 2000); loadDeviceAliasesPanel(); })
    .catch(e => showToast(String(e.message||e), 'error'));
};

// ── BL220 Bundle F — Branding / Splash config ─────────────────────────────────

function loadBrandingPanel() {
  const el = document.getElementById('brandingPanel');
  if (!el) return;
  apiFetch('/api/splash/info').then(data => {
    el.innerHTML = `
      <div style="font-size:11px;color:var(--text2);padding:0 0 8px;">
        Tagline and logo path are saved to <code>session.splash_tagline</code> and
        <code>session.splash_logo_path</code> in config.
        ${data.logo_url ? `<a href="${escHtml(data.logo_url)}" target="_blank" style="color:var(--accent);margin-left:4px;">View current logo</a>` : ''}
      </div>
      <div style="display:flex;flex-direction:column;gap:8px;">
        <div style="display:flex;gap:6px;align-items:center;">
          <label style="font-size:11px;width:90px;flex-shrink:0;">Tagline</label>
          <input id="splashTagline" type="text" class="form-input" style="flex:1;font-size:11px;" placeholder="e.g. AI-powered dev assistant"
            value="${escHtml(data.tagline||'')}" />
        </div>
        <div style="display:flex;gap:6px;align-items:center;">
          <label style="font-size:11px;width:90px;flex-shrink:0;">Logo path</label>
          <input id="splashLogoPath" type="text" class="form-input" style="flex:1;font-size:11px;" placeholder="Absolute path to PNG/SVG on server" value="" />
        </div>
        <div style="display:flex;gap:6px;padding-top:2px;">
          <button class="btn-primary" style="font-size:11px;" onclick="saveBranding()">Save</button>
          <span id="brandingSaveStatus" style="font-size:10px;color:var(--text2);align-self:center;"></span>
        </div>
      </div>`;
    apiFetch('/api/config').then(cfg => {
      const lp = document.getElementById('splashLogoPath');
      if (lp) lp.value = cfg?.session?.splash_logo_path || '';
    }).catch(() => {});
  }).catch(() => { el.innerHTML = '<span style="color:var(--error);font-size:12px;">Failed to load splash info.</span>'; });
}
window.loadBrandingPanel = loadBrandingPanel;
window.saveBranding = function() {
  const tagline = (document.getElementById('splashTagline')||{}).value||'';
  const logoPath = (document.getElementById('splashLogoPath')||{}).value||'';
  apiFetch('/api/config', {
    method: 'PUT',
    headers: {'Content-Type':'application/json'},
    body: JSON.stringify({ 'session.splash_tagline': tagline, 'session.splash_logo_path': logoPath }),
  }).then(() => {
    const s = document.getElementById('brandingSaveStatus');
    if (s) { s.textContent = 'Saved.'; setTimeout(() => { if(s) s.textContent=''; }, 2500); }
  }).catch(() => showToast('Failed to save branding', 'error'));
};

// ── BL220 Bundle F — Session Analytics ───────────────────────────────────────

function loadAnalyticsPanel() {
  const el = document.getElementById('analyticsPanel');
  if (!el) return;
  const ranges = [7, 14, 30, 90];
  const selId = 'analyticsRange';
  el.innerHTML = `<div style="display:flex;gap:6px;align-items:center;margin-bottom:8px;">
    <label style="font-size:11px;">Range:</label>
    <select id="${selId}" onchange="loadAnalyticsPanel()" style="font-size:11px;background:var(--bg2);border:1px solid var(--border);border-radius:4px;color:var(--text);padding:2px 4px;">
      ${ranges.map(d => `<option value="${d}"${d===7?' selected':''}>${d}d</option>`).join('')}
    </select>
    <span id="analyticsSuccessRate" style="font-size:11px;color:var(--text2);margin-left:4px;"></span>
  </div>
  <div id="analyticsBuckets" style="font-size:11px;color:var(--text2);">Loading…</div>`;
  const range = (document.getElementById(selId)||{}).value || '7';
  apiFetch(`/api/analytics?range=${range}d`).then(data => {
    const rate = document.getElementById('analyticsSuccessRate');
    if (rate && data.success_rate != null)
      rate.textContent = `Success rate: ${(data.success_rate * 100).toFixed(1)}%`;
    const buckets = data.buckets || [];
    const bucketsEl = document.getElementById('analyticsBuckets');
    if (!bucketsEl) return;
    if (buckets.length === 0) { bucketsEl.textContent = 'No sessions in range.'; return; }
    // API fields: session_count, completed, failed, killed (not total/errors)
    const maxTotal = Math.max(...buckets.map(b => b.session_count || 0), 1);
    bucketsEl.innerHTML = `<table style="width:100%;border-collapse:collapse;font-size:11px;">
      <thead><tr style="opacity:0.5;">
        <th style="text-align:left;padding:2px 4px;">Date</th>
        <th style="text-align:right;padding:2px 4px;">Total</th>
        <th style="text-align:right;padding:2px 4px;">OK</th>
        <th style="text-align:right;padding:2px 4px;">Err</th>
        <th style="text-align:left;padding:2px 4px 2px 8px;">Bar</th>
      </tr></thead><tbody>${
      buckets.map(b => {
        const total = b.session_count || 0;
        const errors = (b.failed || 0) + (b.killed || 0);
        const ok = total - errors;
        const pct = Math.round((total / maxTotal) * 100);
        const errPct = total ? Math.round((errors / total) * 100) : 0;
        const barColor = errPct > 20 ? 'var(--error,#ef4444)' : errPct > 5 ? 'var(--warning,#f59e0b)' : 'var(--success,#10b981)';
        return `<tr style="border-top:1px solid var(--border);">
          <td style="padding:3px 4px;">${escHtml(b.date||'')}</td>
          <td style="text-align:right;padding:3px 4px;">${total}</td>
          <td style="text-align:right;padding:3px 4px;color:var(--success,#10b981);">${ok}</td>
          <td style="text-align:right;padding:3px 4px;color:var(--error,#ef4444);">${errors}</td>
          <td style="padding:3px 4px 3px 8px;"><div style="height:8px;width:${pct}%;background:${barColor};border-radius:2px;max-width:120px;"></div></td>
        </tr>`;
      }).join('')}</tbody></table>`;
  }).catch(() => {
    const bucketsEl = document.getElementById('analyticsBuckets');
    if (bucketsEl) bucketsEl.innerHTML = '<span style="color:var(--error);">Failed to load analytics.</span>';
  });
}
window.loadAnalyticsPanel = loadAnalyticsPanel;

// ── BL220 Bundle F — Audit Log browser ───────────────────────────────────────

function loadAuditPanel() {
  const el = document.getElementById('auditPanel');
  if (!el) return;
  const actor = (document.getElementById('auditActorFilter')||{}).value||'';
  const action = (document.getElementById('auditActionFilter')||{}).value||'';
  const limit = (document.getElementById('auditLimitFilter')||{}).value||'50';
  const qp = new URLSearchParams({ limit });
  if (actor) qp.set('actor', actor);
  if (action) qp.set('action', action);
  const loadingHtml = el._filters ? '' : `
    <div style="display:flex;gap:4px;flex-wrap:wrap;margin-bottom:8px;align-items:center;">
      <input id="auditActorFilter" type="text" placeholder="Actor filter" style="flex:1;min-width:70px;font-size:11px;padding:3px 6px;background:var(--bg);border:1px solid var(--border);border-radius:4px;color:var(--text);" />
      <input id="auditActionFilter" type="text" placeholder="Action filter" style="flex:1;min-width:70px;font-size:11px;padding:3px 6px;background:var(--bg);border:1px solid var(--border);border-radius:4px;color:var(--text);" />
      <select id="auditLimitFilter" style="font-size:11px;background:var(--bg2);border:1px solid var(--border);border-radius:4px;color:var(--text);padding:3px 4px;">
        <option value="20">20</option><option value="50" selected>50</option><option value="100">100</option>
      </select>
      <button class="btn-secondary" style="font-size:11px;" onclick="loadAuditPanel()">Load</button>
    </div>
    <div id="auditEntries" style="font-size:11px;color:var(--text2);">Loading…</div>`;
  if (!el._filters) { el.innerHTML = loadingHtml; el._filters = true; }
  const entriesEl = document.getElementById('auditEntries');
  if (entriesEl) entriesEl.textContent = 'Loading…';
  apiFetch(`/api/audit?${qp}`).then(data => {
    const entries = (data && data.entries) || [];
    const target = document.getElementById('auditEntries');
    if (!target) return;
    if (entries.length === 0) { target.textContent = 'No audit entries in range.'; return; }
    target.innerHTML = entries.map(e => {
      const ts = e.ts ? new Date(e.ts).toLocaleString() : '';
      const details = e.details && Object.keys(e.details).length
        ? `<div style="opacity:0.6;margin-top:1px;font-family:monospace;">${escHtml(JSON.stringify(e.details))}</div>` : '';
      const sessLink = e.session_id
        ? `<span style="opacity:0.5;font-size:10px;font-family:monospace;margin-left:4px;">${escHtml(e.session_id.slice(-8))}</span>` : '';
      return `<div style="padding:4px 0;border-top:1px solid var(--border);">
        <div style="display:flex;gap:6px;align-items:baseline;flex-wrap:wrap;">
          <span style="opacity:0.5;white-space:nowrap;">${escHtml(ts)}</span>
          <strong>${escHtml(e.action||'')}</strong>
          <span style="opacity:0.7;">${escHtml(e.actor||'')}</span>${sessLink}
        </div>${details}
      </div>`;
    }).join('');
  }).catch(() => {
    const target = document.getElementById('auditEntries');
    if (target) target.innerHTML = '<span style="color:var(--error);">Failed to load audit log.</span>';
  });
}
window.loadAuditPanel = loadAuditPanel;

// ── BL220 Bundle F — Pipeline manager ────────────────────────────────────────

function loadPipelinesPanel() {
  const el = document.getElementById('pipelinesPanel');
  if (!el) return;
  el.innerHTML = '<div style="color:var(--text2);font-size:12px;">Loading…</div>';
  apiFetch('/api/pipelines').then(data => {
    const pipelines = Array.isArray(data) ? data : [];
    const stateColor = { pending:'var(--text2)', running:'var(--accent,#6366f1)', completed:'var(--success,#10b981)', failed:'var(--error,#ef4444)', cancelled:'var(--text2)' };
    el.innerHTML = `<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:8px;">
      <span style="font-size:11px;opacity:0.7;">${pipelines.length} pipeline${pipelines.length===1?'':'s'}</span>
      <button class="btn-icon" style="font-size:11px;padding:2px 8px;" onclick="loadPipelinesPanel()">↻</button>
    </div>` + (pipelines.length === 0
      ? '<div style="opacity:0.7;font-size:12px;">No pipelines — start one via REST or CLI.</div>'
      : pipelines.map(p => {
          const color = stateColor[p.state||'pending'] || 'var(--text2)';
          const tasksDone = (p.tasks||[]).filter(t=>t.state==='completed').length;
          const safeId = JSON.stringify(p.id||'');
          const canCancel = p.state === 'running' || p.state === 'pending';
          return `<div style="padding:8px 0;border-top:1px solid var(--border);">
            <div style="display:flex;align-items:center;gap:6px;">
              <span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:${color};flex-shrink:0;"></span>
              <strong style="font-size:12px;">${escHtml(p.name||p.id||'')}</strong>
              <span style="opacity:0.6;font-size:10px;">${escHtml(p.state||'')}</span>
              ${(p.tasks||[]).length ? `<span style="opacity:0.5;font-size:10px;margin-left:auto;">${tasksDone}/${p.tasks.length} tasks</span>` : ''}
              ${canCancel ? `<button class="btn-icon" style="font-size:11px;padding:2px 6px;color:var(--error);" onclick='pipelineCancel(${safeId})'>Cancel</button>` : ''}
            </div>
            <div style="opacity:0.5;font-size:10px;font-family:monospace;">${escHtml(p.id||'')}</div>
          </div>`;
        }).join(''));
  }).catch(() => { el.innerHTML = '<span style="color:var(--error);font-size:12px;">Failed to load pipelines.</span>'; });
}
window.loadPipelinesPanel = loadPipelinesPanel;
window.pipelineCancel = function(id) {
  apiFetch(`/api/pipeline?id=${encodeURIComponent(id)}&action=cancel`, { method: 'POST' })
    .then(() => { showToast('Pipeline cancelled', 'success', 2000); loadPipelinesPanel(); })
    .catch(e => showToast(String(e.message||e), 'error'));
};

// ── BL220 Bundle F — Knowledge Graph browser ──────────────────────────────────

function loadKgPanel() {
  const el = document.getElementById('kgPanel');
  if (!el) return;
  if (!el._init) {
    el._init = true;
    el.innerHTML = `
      <div style="display:flex;gap:4px;margin-bottom:8px;align-items:center;">
        <input id="kgEntityInput" type="text" placeholder="Entity to query…" class="form-input" style="flex:1;font-size:11px;" />
        <button class="btn-primary" style="font-size:11px;" onclick="kgQuery()">Query</button>
      </div>
      <div style="display:flex;gap:4px;margin-bottom:10px;align-items:center;">
        <input id="kgSubject" type="text" placeholder="Subject" class="form-input" style="flex:1;font-size:11px;" />
        <input id="kgPredicate" type="text" placeholder="Predicate" class="form-input" style="flex:1;font-size:11px;" />
        <input id="kgObject" type="text" placeholder="Object" class="form-input" style="flex:1;font-size:11px;" />
        <button class="btn-secondary" style="font-size:11px;" onclick="kgAddTriple()">Add triple</button>
      </div>
      <div id="kgResults" style="font-size:11px;color:var(--text2);"></div>`;
  }
}
window.loadKgPanel = loadKgPanel;
window.kgQuery = function() {
  const entity = (document.getElementById('kgEntityInput')||{}).value||'';
  if (!entity) return;
  const res = document.getElementById('kgResults');
  if (res) res.textContent = 'Querying…';
  apiFetch(`/api/memory/kg/query?entity=${encodeURIComponent(entity)}`).then(data => {
    if (!res) return;
    const triples = Array.isArray(data) ? data : (data?.triples || []);
    if (triples.length === 0) { res.textContent = 'No triples found for this entity.'; return; }
    res.innerHTML = `<div style="font-size:10px;opacity:0.6;margin-bottom:4px;">${triples.length} triple${triples.length===1?'':'s'}</div>`
      + triples.map(t => `<div style="padding:3px 0;border-top:1px solid var(--border);font-family:monospace;">
        <span style="color:var(--accent);">${escHtml(t.subject||'')}</span>
        <span style="opacity:0.6;margin:0 4px;">${escHtml(t.predicate||'')}</span>
        <span>${escHtml(t.object||'')}</span>
        ${t.valid_from ? `<span style="opacity:0.4;font-size:10px;margin-left:4px;">${escHtml(t.valid_from)}</span>` : ''}
      </div>`).join('');
  }).catch(e => { if (res) res.innerHTML = `<span style="color:var(--error);">${escHtml(String(e.message||e))}</span>`; });
};
window.kgAddTriple = function() {
  const subject = (document.getElementById('kgSubject')||{}).value||'';
  const predicate = (document.getElementById('kgPredicate')||{}).value||'';
  const object = (document.getElementById('kgObject')||{}).value||'';
  if (!subject || !predicate || !object) { showToast('Subject, predicate, object all required', 'error'); return; }
  apiFetch('/api/memory/kg/add', { method: 'POST', headers: {'Content-Type':'application/json'},
    body: JSON.stringify({ subject, predicate, object, source: 'pwa' }) })
    .then(() => { showToast('Triple added', 'success', 2000); kgQuery(); })
    .catch(e => showToast(String(e.message||e), 'error'));
};
