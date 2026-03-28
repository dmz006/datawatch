# updates
- need a restart option on all channels: api, mcp, ui, cli (daemon graceful drain + reconnect)
- each session log should capture (in auditable format) prompts and communication channel details of response.  there should be a way through api, mcp, cli, channels and web ui to browse session actions
- can --secure be run after things are configured and running? if not enable it and make sure start/stop/restart provide proper prompts so it can still run as a daemon
# bugs
- when configuring ollama it should just need the server, should connect and get a list of models available, same for claude, opencode, service integration should enable changing those and other features
- alerts should have the pre-configured commands able to be sent as a reply and they should be grouped by session with openable tabs or however to best organize it based on best practices
# signal-go
- review ../signal-go/signal-cli/ and see if it is the code used for signal-cli we are using, also review against https://github.com/RadicalApp/libsignal-protocol-go and see if any modules or functionality is missing
- review the go modules and code created in ../signal-go/ - test and validate it works for our implementation of datawatch
- create a git project for it and integrate into datawatch and remove the signal-cli and java dependencies. the local datawatch installation is already linked to signal with signal-cli, see if link can be re-used and tested with new signal-go integration
- review docs, there was mention of this in a future planning; update future planning with all recent changes
# backlog
- make sure mcp provides 1:1 compatability with all features if possible
- communication channel "DNS" — sets up a DNSSEC server that responds to specific DNS queries using secure DNS communications as a control channel. CLI interface extended: if configured remote service is of type DNS, commands are sent via DNS queries to the configured domain using the configurable resolver (host-configured or direct-connect). See `docs/covert-channels.md` for detailed design.
- evaluate alternative covert/low-profile communication channels beyond DNS tunneling (see `docs/covert-channels.md`)
