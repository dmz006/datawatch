# docs
- make sure docs highlight that for claude channel requires node (version??) and how to install node if it's not there.  Make sure if it's enabled and requirements are not there it provides appropriate warning and does not run until dependencies installed or it is disabled
# agent rules
- update agent rules to require any time a large plan is created to make sure there is a docs/plans created with details and timestamp and if possible reference to version it was implemented
# updates
- need a restart option on all channels: api, mcp, ui, cli
- each session log should capture (in auditable format) prompts and communication channel details of response.  there should be a way through api, mcp, cli, channels and web ui to browse session actions
# bugs
- make sure mcp provides 1:1 compatability with all features if possible
- web ui version says v0.5.8, cli version says v0.5.19 - fix
- when going into a session; the enter command should not be auto-selected, it causes keyboard to open up and makes reading screen hard; especially if auto-commands are generally entered
- update from web ui says downloading and wait; but doesn't seem to update, refreshed after 3 min and still old version; the command line version appears updated but the agent did not restart
- opencode ACP / MCP channels, like were done for claude, should have also been implemented
- there should be a status function to check on if it's running and details of any sessions, including if any are waiting on input
- should channel_enabled be true by default? If it's false then communication channels, even web should not show those services or features
- in web interface, if a backend is selected that is not installed/configured, should open a way to configure or edit it with proper instructions on how to setup (maybe link to github documentation if needed) currently shows a warning but no wizard or configuration or edit config option. the "start" or run button isnt' needed, it should be an edit with a form to edit the settings and links to instructions.  through channels it should be like cli a workflow asking for variables and setting them and when configured enable/disable. make sure that enable/disable does not require restart it should be able to just run or stop
- on upgrade, does downloading new version while it is running cause any permission errors? can the automated upgrade process restart the process? can the cli upgrade also restart the process?
- requests notification permission" on android chrome  "permission denied" 
- when creating a session the given name doesn't appear in the list of sessions after creating. the session starts and says (no task)
- the prompt detection should, if possible, use the filter system which should allow for notification and running of command(s)
- the config about if should have a link to the project
- configured ollama but it doesn't show active
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
