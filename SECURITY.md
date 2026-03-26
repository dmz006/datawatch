# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| 0.1.x   | Yes       |

## Reporting a Vulnerability

Please report security vulnerabilities via GitHub's private security advisory feature:
https://github.com/dmz006/claude-signal/security/advisories/new

Do NOT open a public GitHub issue for security vulnerabilities.

We aim to respond within 72 hours and release a fix within 14 days for
critical vulnerabilities.

## Security Considerations

### Signal Account Access
claude-signal has full access to send and receive messages on your Signal account
via the linked device. It only processes messages from the configured group ID.
Guard your signal-cli config directory (~/.local/share/signal-cli) carefully.

### Network Access (PWA)
The HTTP/WebSocket server binds to 0.0.0.0 by default. Access is controlled by:
1. **Tailscale** (recommended): restricts access to your Tailscale network
2. **Bearer token**: optional additional layer (`server.token` in config)
3. **TLS**: optional; Tailscale provides transport encryption at the network layer

For public-facing deployments, always enable both the token and TLS.

### File System Access
claude-code sessions run with access to the configured project directory.
The --add-dir flag limits claude-code's file system scope to that directory tree.
Sessions run as the user who started the claude-signal daemon.

### Secrets in Tasks
Do not include API keys, passwords, or secrets in task descriptions sent via Signal,
as they will be stored in session logs (~/.claude-signal/logs/).

### Data Storage
All data is stored locally. No telemetry, no external API calls except to Signal
infrastructure (via signal-cli). Session logs, config, and Signal keys stay on your machine.

### Running as a System Service
When installed system-wide, the daemon runs as the `claude-signal` system user
with no home directory and restricted file system access (systemd hardening).
The claude binary and tmux must be accessible to this user.
