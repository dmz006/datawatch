# bugs
- if user runs link and it's already setup it should say so and provide guideance on how to remove and reset if needed
- should be able to not only restart an old session but also delete, and delete should prompt and if yes then delete the data for that session
- in web interface can start/stop other backend, but if not configured should be a wizard to configure
- in config, remote servers should default to connected to localhost since that is what you are seeing
- sessions have started with claude and are in a prompt waiting for an answer; see any of the running session on local host daemon or session e156, there are no alerts of the prompt and there should be a shortcut to send saved commands (add if none or run from selected list).  Also when trying to send a response to the command I get "Error: session ralfthewise-e156 is not waiting for input (state: running)"
# backlog
- communication channel "DNS" — sets up a DNSSEC server that responds to specific DNS queries using secure DNS communications as a control channel. CLI interface extended: if configured remote service is of type DNS, commands are sent via DNS queries to the configured domain using the configurable resolver (host-configured or direct-connect). See `docs/covert-channels.md` for detailed design.
- evaluate alternative covert/low-profile communication channels beyond DNS tunneling (see `docs/covert-channels.md`)
