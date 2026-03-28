# docs
- make sure documentation for opencode ACP documents what commands are available over ACP and what are over tmux
# updates
- need a restart option on all channels: api, mcp, ui, cli
- each session log should capture (in auditable format) prompts and communication channel details of response.  there should be a way through api, mcp, cli, channels and web ui to browse session actions
# bugs
- in web session, in a claude session, I can send a message to a channel but not to tmux; make sure it's clear and easy to change to which to use. also when sending to a new claude session i got an error sending to teh channel; most likely because claude was waiting for trust response but i couldn't send a message in web ui to the tmux
- when starting a session, not installed LLM shouldn't be in the list
- changelog is missing all updates from 0.5.13 - 0.5.2*, also make sure agent rules require changelog update with committed changes 
- can --secure be run after things are configured and running? if not enable it and make sure start/stop/restart provide proper prompts so it can still run as a daemon
- make sure mcp provides 1:1 compatability with all features if possible
- when going into a session; the enter command should not be auto-selected, it causes keyboard to open up and makes reading screen hard; especially if auto-commands are generally entered
- update from web ui says downloading and wait; but doesn't seem to update, refreshed after 3 min and still old version; the command line version appears updated but the agent did not restart
- should channel_enabled be true by default? If it's false then communication channels, even web should not show those services or features
- in web interface, if a backend is selected that is not installed/configured, should open a way to configure or edit it with proper instructions on how to setup (maybe link to github documentation if needed) currently shows a warning but no wizard or configuration or edit config option. the "start" or run button isnt' needed, it should be an edit with a form to edit the settings and links to instructions.  through channels it should be like cli a workflow asking for variables and setting them and when configured enable/disable. make sure that enable/disable does not require restart it should be able to just run or stop
- on upgrade, does downloading new version while it is running cause any permission errors? can the automated upgrade process restart the process? can the cli upgrade also restart the process?
- requests notification permission" on android chrome  "permission denied"
- the prompt detection should, if possible, use the filter system which should allow for notification and running of command(s)
- when configuring ollama it should just need the server, should connect and get a list of models available, same for claude, opencode, service integration should enable changing those and other features
- alerts should have the pre-configured commands able to be sent as a reply and they should be grouped by session with openable tabs or however to best organize it based on best practices
# signal-go
- review ../signal-go/signal-cli/ and see if it is the code used for signal-cli we are using, also review against https://github.com/RadicalApp/libsignal-protocol-go and see if any modules or functionality is missing
- review the go modules and code created in ../signal-go/ - test and validate it works for our implementation of datawatch
- create a git project for it and integrate into datawatch and remove the signal-cli and java dependencies. the local datawatch installation is already linked to signal with signal-cli, see if link can be re-used and tested with new signal-go integration
- review docs, there was mention of this in a future planning; update future planning with all recent changes
# backlog
- communication channel "DNS" — sets up a DNSSEC server that responds to specific DNS queries using secure DNS communications as a control channel. CLI interface extended: if configured remote service is of type DNS, commands are sent via DNS queries to the configured domain using the configurable resolver (host-configured or direct-connect). See `docs/covert-channels.md` for detailed design.
- evaluate alternative covert/low-profile communication channels beyond DNS tunneling (see `docs/covert-channels.md`)
