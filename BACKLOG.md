# future
- the configuration wizards and web config settings should support all config file configuration; even wizards for the llm setups (`setup llm <backend>`, `setup session`, `setup mcp` CLI wizards; full config form in Web UI settings)
- alerts command in messaging backends: `alerts` command shows recent alert history; triggered alerts should also be sent to all active messaging backends

# backlog
- communication channel "DNS" — sets up a DNSSEC server that responds to specific DNS queries using secure DNS communications as a control channel. CLI interface extended: if configured remote service is of type DNS, commands are sent via DNS queries to the configured domain using the configurable resolver (host-configured or direct-connect)
- evaluate alternative covert/low-profile communication channels beyond DNS tunneling (see `docs/covert-channels.md`)
