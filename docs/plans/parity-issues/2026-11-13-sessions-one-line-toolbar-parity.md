# Parity: Sessions one-line toolbar with view dropdown and Fold all (datawatch v9.168.0)

Parent release: datawatch v9.168.0

## What changed in datawatch

The main controls of the Sessions page now share one toolbar line that never wraps into several rows, at every width: a server select (the same shared picker as the header server dropdown, so choosing a server in one updates the other), the search box taking the free width, the Filters button with its badge, a Fold all icon that appears while a grouping is active, and a view mode select for List, Tiles and Compact. The chosen view is remembered on the device. The Tree and History buttons, and the back-to-local indicator while a federated server is active, move to a line directly below the toolbar. The same one-line rule was applied to the Alerts, Automata, Observer and Dashboard pages where it fits.

No route, tool, command or config key changed.

## What the mobile app needs

- Keep the Sessions controls on one line: server select, search, Filters with its badge, Fold all while a grouping is active, and a view mode select (List, Tiles, Compact) that remembers the choice.
- Put Tree, History and the back-to-local indicator on a line below the toolbar.
- Apply the one-line rule to the Alerts, Automata, Observer and Dashboard screens where it fits.
- One new web string, `sessions_view_dropdown` ("Sessions view mode"), in all five locales.
