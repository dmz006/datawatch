Parent release: datawatch v9.160.1

## What changed in datawatch

The web app's server selector is now one icon button (icon, status dot, short name on wide screens) in the fixed app header on every page, replacing the per-view row of server chips and the briefly shipped header dropdown; it opens the server picker window listing the choices. It lists All servers, this server (named by the daemon's hostname, never the word "Local") and one entry per enabled peer; an unreachable peer stays listed and selectable with an "(unreachable)" hint. Choosing an entry re-renders the page in place. The Sessions page now has a single labelled Group by control (the Filters sheet's dropdown); the second placement dropdown is gone and its placement axes (`compute:`, `peer:`, ...) appear as `Tag (prefix)` entries in the one control. The start-session button is hidden while a sheet or modal is open and long pages carry extra bottom padding so the button never covers the last chips or cards.

No setting, endpoint, command or tool changed.

## What the mobile app needs

- Replace any per-screen server chip row with one server icon button in the app bar that opens a picker window: All servers, this server, then each enabled peer, with an "(unreachable)" hint on a peer that does not answer.
- Keep one Group by control on the sessions list; offer placement axes seen on the loaded sessions alongside the tag prefixes.
- Keep the floating start-session button clear of open sheets and of the last rows of long lists.
- New strings in all five locales: `server_dropdown_all` ("All servers") and `server_dropdown_unreachable` ("(unreachable)"); removed: `server_chip_this_server_tip`, `server_chip_unreachable_tip`, `server_picker_label`, `sessions_group_by_label`.


Operator review: the project and plan selector icons are not covered by the docs yet; this pass documents only the shipped server button. Live verification of the button on a phone viewport is still pending.
