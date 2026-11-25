# datawatch-app parity: MCP tools refuse to answer a waiting prompt

Draft only; no issue filed.

## What changed in the daemon

MCP tools that type into a session are refused while that session shows an
approval, permission, safety or confirmation prompt, or while its screen cannot
be read. There is no setting.

## Mobile impact

None expected. The Android, iOS and Wear clients answer prompts with
`send_input` frames on the session WebSocket, which is an operator surface and
is unchanged. No new field, endpoint, setting or string. (The WebSocket frame
shares its name with the MCP tool but is not that tool.)

## To check on the client

- Approve and deny a real permission prompt from the phone and the watch after
  the daemon is updated.
