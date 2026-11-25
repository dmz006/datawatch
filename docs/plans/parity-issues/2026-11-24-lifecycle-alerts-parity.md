# Parity draft: Automaton lifecycle alerts now reach the app (datawatch-app)

Draft only. No issue has been filed.

The daemon now stores every allowed Automaton lifecycle event as an alert and
publishes it to the push topics `alerts` and `automaton-<id>`. Before this fix
a daemon with the comm fan-out installed sent only the chat message. No API,
field or setting changed.

## What the app should check

- The alert list: lifecycle alerts have no `session_id` and no `source`. They
  belong with the system alerts. Title is the event kind, body is one line.
- Notifications: a registered push endpoint gets one push per event, with topic
  `alerts` and tag `lifecycle`. A stream subscribed to both `alerts` and an
  `automaton-<id>` topic receives the event on each, with the same event id.
- The daemon also sends every new alert to registered iOS devices through APNs.
  A device that has both an APNs and a push-endpoint registration may show the
  event twice; the app should decide how to handle that.
