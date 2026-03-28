# bugs
- in web interface, if a backend is selected that is not installed/configured, should open a wizard to configure it (currently shows a warning but no wizard)
- description for a session should be optional
- in new session can't browse folders, only see and click once on any list but can't browse into anything


# backlog
- communication channel "DNS" — sets up a DNSSEC server that responds to specific DNS queries using secure DNS communications as a control channel. CLI interface extended: if configured remote service is of type DNS, commands are sent via DNS queries to the configured domain using the configurable resolver (host-configured or direct-connect). See `docs/covert-channels.md` for detailed design.
- evaluate alternative covert/low-profile communication channels beyond DNS tunneling (see `docs/covert-channels.md`)
