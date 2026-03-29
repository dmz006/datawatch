# updates
- each session log should capture (in auditable format) prompts and communication channel details of response.  there should be a way through api, mcp, cli, channels and web ui to browse session actions
# signal-go
- review ../signal-go/signal-cli/ and see if it is the code used for signal-cli we are using, also review against https://github.com/RadicalApp/libsignal-protocol-go and see if any modules or functionality is missing
- review the go modules and code created in ../signal-go/ - test and validate it works for our implementation of datawatch
- create a git project for it and integrate into datawatch and remove the signal-cli and java dependencies. the local datawatch installation is already linked to signal with signal-cli, see if link can be re-used and tested with new signal-go integration
- review docs, there was mention of this in a future planning; update future planning with all recent changes
# encrypted logs
- when `--secure` is used, session output logs should also be encrypted at rest (AES-256-GCM)
- add `datawatch export` CLI command with options:
  - `--all --folder /path/` — decrypt and export all logs to folder
  - `--log <session-id> --folder /path/` — decrypt and export specific session log
  - prompts for password to decrypt
- currently only config and data stores are encrypted; output.log files are plaintext

# config
- restructure config.yaml to group related fields by function (session, server, messaging, llm, etc.) with YAML comments documenting each field
- ensure the saved config file includes all fields with defaults and inline documentation
- the web UI General Configuration card should mirror the config file grouping

# backlog
- communication channel "DNS" — sets up a DNSSEC server that responds to specific DNS queries using secure DNS communications as a control channel. CLI interface extended: if configured remote service is of type DNS, commands are sent via DNS queries to the configured domain using the configurable resolver (host-configured or direct-connect). See `docs/covert-channels.md` for detailed design.
- evaluate alternative covert/low-profile communication channels beyond DNS tunneling (see `docs/covert-channels.md`)
