# Bug Test Plan — User Validation

Follow these steps to validate each fixed bug. Report PASS or FAIL for each.

---

## 1. Splash Screen (3 second minimum)
**Steps:**
1. Open browser, navigate to datawatch URL
2. Hard refresh (Ctrl+Shift+R)
3. Observe the splash screen with the eye logo, spinning ring, and bouncing dots
4. Time it — should display for at least 3 seconds before fading out

**Expected:** Purple eye logo centered, "datawatch" title, "AI Session Monitor" subtitle, 3+ seconds visible, smooth fade to sessions view.

---

## 2. Interface Binding Selector
**Steps:**
1. Go to Settings → Web Server section
2. Find "Bind interface" — should show checkboxes for 0.0.0.0, 127.0.0.1, and your network interfaces
3. Uncheck "0.0.0.0 (all interfaces)"
4. Check "127.0.0.1 (localhost)" — the "all" checkbox should automatically uncheck
5. Observe toast "Saved: 127.0.0.1. Restart required."
6. Check "0.0.0.0 (all)" again — the "127.0.0.1" checkbox should automatically uncheck
7. Observe toast confirming save

**Expected:** Mutual exclusion works (all vs specific), save confirmation shown, restart hint appears.

---

## 3. SSE Interface Binding
**Steps:**
1. Go to Settings → MCP Server section
2. Find "SSE bind interface" checkboxes
3. Repeat same checkbox tests as #2
4. Verify save confirmation appears

**Expected:** Same mutual exclusion behavior as web server interface.

---

## 4. TLS Dual-Port
**Steps:**
1. Go to Settings → Web Server → enable "TLS enabled"
2. Set "TLS port" to 8443
3. Save and restart (or let auto-restart handle it)
4. In browser, go to http://your-host:8080 — should redirect to https://your-host:8443
5. Verify HTTPS works on port 8443
6. Disable TLS and restart to reset

**Expected:** HTTP redirects to HTTPS with correct URL, HTTPS serves the app, disable resets to HTTP.

---

## 5. Stop/Delete Confirm Modal
**Steps:**
1. Go to any session in the session list
2. Open a session, click the Stop button
3. A dark overlay modal appears with "Stop session?" and Yes/No buttons
4. Verify "Yes" is focused (highlighted/outlined)
5. Press Enter — should stop the session without clicking
6. Try Delete on a completed session — same modal with "Delete session and data?"

**Expected:** No browser popup (no `confirm()` dialog). Inline modal, Yes auto-focused, Enter confirms.

---

## 6. Detection Filters Managed List
**Steps:**
1. Go to Settings → Detection Filters section
2. Should see 4 subsections: Prompt Patterns, Completion Patterns, Rate Limit Patterns, Input Needed
3. Each shows pattern count and a scrollable list of current patterns
4. Try adding a pattern: type "test-pattern" in the input field, click "Add"
5. Verify it appears in the list
6. Click the X next to "test-pattern" to remove it
7. Verify it disappears

**Expected:** Patterns display with counts, add/remove works, toasts confirm actions.

---

## 7. About Section Logo
**Steps:**
1. Go to Settings → scroll to "About" section
2. Should see the datawatch eye logo (same as favicon)
3. Below it: "datawatch" in large text, "AI Session Monitor & Bridge" subtitle
4. Below that: Version number, Update check, Restart button

**Expected:** Logo displayed centered above version info.

---

## 8. Terminal Font Size Controls
**Steps:**
1. Open any active session
2. Above the terminal, find the font toolbar: A- | 9px | A+ | Fit
3. Click A+ several times — font should increase, label should update
4. Click A- to decrease
5. Click "Fit" — font should auto-shrink until terminal fits screen width
6. Refresh the page — font size should be preserved

**Expected:** Font changes live, Fit auto-sizes, setting persists across page loads.

---

## 9. State Override (Manual)
**Steps:**
1. Open any session in session detail
2. Click on the state badge (e.g., "running", "waiting_input")
3. A dropdown menu should appear with state options
4. Select a different state (e.g., "complete")
5. Verify the badge updates and the session moves to the correct state

**Expected:** Clicking state badge shows dropdown, selecting a state changes it, toast confirms.

---

## 10. Schedule Input
**Steps:**
1. Open an active session
2. Find the clock icon (🕓) next to the send button
3. Click it — a popup should appear with:
   - "Command to send" input field
   - "When" input with natural language hint
   - Quick buttons: 5 min, 30 min, 1 hr, On input
   - Examples text: "in 30m, at 14:00, tomorrow at 9am"
4. Enter a command and click a quick button, then "Schedule"
5. Verify toast confirms schedule

**Expected:** Schedule popup appears, time hints visible, schedule saves successfully.

---

## 11. System Statistics Dashboard
**Steps:**
1. Go to Settings → System Statistics section
2. Should see grid cards: CPU Load, Memory, Disk, Daemon, Sessions
3. If GPU is available, GPU card should also show
4. Click "Refresh" to update
5. Verify numbers look reasonable (memory in GB, CPU load, session counts)

**Expected:** Stats display with formatted values, Refresh updates them.

---

## 12. Claude Session Exit Auto-Complete
**Steps:**
1. Start a new Claude session
2. Let it run for a bit
3. In the terminal, type `/exit` to quit Claude
4. Verify the session state changes to "complete" (not dropping to a shell prompt)

**Expected:** Session transitions to complete state after Claude exits.

---

## 13. Settings Tab Order
**Steps:**
1. Go to Settings
2. Verify tabs at top are: Monitor, General, Comms, LLM, About
3. Monitor should be the default/active tab

**Expected:** Monitor tab is first and selected by default.

---

## 14. View Persistence
**Steps:**
1. Navigate to Settings → LLM tab
2. Hard refresh the browser (Ctrl+Shift+R)
3. After splash screen, should return to Settings → LLM tab
4. Navigate to a session detail, refresh — should return to that session

**Expected:** View and tab persist across browser refresh.

---

## 15. Expandable Session Resources
**Steps:**
1. Go to Settings → Monitor tab
2. In the Resources section, click the ▶ chevron next to a session
3. Details should expand showing: Backend, PID, Memory, Network
4. Wait 5 seconds for live update — the expanded row should stay open
5. Click ▼ to collapse

**Expected:** Rows expand/collapse, stay open across live updates.

---

## 16. eBPF Status Notice
**Steps:**
1. Go to Settings → Monitor tab
2. If eBPF is enabled but not active, verify amber banner says "run: datawatch setup ebpf"
3. If eBPF is active, verify green "eBPF active" indicator
4. If eBPF is not enabled, no banner should appear

**Expected:** Correct status indicator based on eBPF state.

---

## 17. Progress Bars in Stats
**Steps:**
1. Go to Settings → Monitor tab
2. Verify CPU, Memory, Disk have progress bars with color coding
3. If GPU available, verify GPU utilization and VRAM bars
4. Bars should be green < 50%, yellow 50-80%, red > 80%

**Expected:** All major metrics have visual progress bars.

---

## 18. Detection Filters in LLM Tab
**Steps:**
1. Go to Settings → LLM tab
2. Verify Detection Filters, Saved Commands, and Output Filters sections are here
3. They should NOT appear in the Monitor tab

**Expected:** Filter/command management is under LLM tab.

---

## 19. eBPF Setup
**Steps:**
1. Run `datawatch setup ebpf` in terminal
2. Should check for CAP_BPF, prompt for sudo if missing
3. After success: `datawatch restart`
4. Check daemon log for "[ebpf] Attached 3 probes"
5. In Settings → Monitor, verify green "eBPF active" indicator

**Expected:** eBPF setup flow works interactively, probes attach after restart.
