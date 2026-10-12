# Checkpoint — 2026-10-12 07:06

Task: integration tests for PWA plugin widget wiring (app-plugin-widgets.test.js)

Done:
- Read real code: _dashCardHTML, _dashAddCard, _dashShowAddPanel, _dashRenderPluginWidgets, refreshSessionPluginWidgets, _pwRenderInto, _pwEmptyState
- Appended 15 integration tests (card shell / DASH_CARD_DEFS bypass, dispatch spy, fetch failures incl. 404/503 with unhandledRejection guard, disabled plugin, malicious ids, session container)
- Full suite: node --test internal/server/web/*.test.js → 249/249 pass

Current: committing
Remaining: none
