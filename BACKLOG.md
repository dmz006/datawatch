# bugs
- i don't see config options for all LLM.  every LLM should have config options, nothing should be hard-coded. i see when one opencode llm is disabled all are, they should be in configuration and independent and make sure all configurable options for LLM are in each llm config and available in web ui
- the prompt in starting new session is a good height but should be the same width as session name and be full width
- debug opencode-prompt, there should be more status updates on the screen showing it is thinking and details like other opencode llm.  
- when running a single prompt session, the saved commands and command input should be hidden
- allow per-llm config for override of auto git commit and auto git init and the order of the 2 options everywhere they are should have init before commit
- update testing script to document any newer components that have been tested
- openwebui error below, debug and fix:
cd '/home/dmz' && curl -s -N -H 'Authorization: Bearer sk-11ef286387204367945339a85728622f' -H 'Content-Type: application/json' -d '{"model":"qwen3-coder-next:q4_K_M","messages":[{"role":"user","content":""}],"stream":true}' 'http://datawatch:3000/api/chat/completions' | python3 -c "import sys,json; [print(json.loads(l).get('choices',[{}])[0].get('delta',{}).get('content',''),end='',flush=True) for l in sys.stdin if l.startswith('data:') and l.strip() != 'data: [DONE]' for l in [l[5:].strip()]]"; echo; echo 'DATAWATCH_COMPLETE: openwebui done'
dmz@ralfthewise datawatch [main] (⎈ |infosecquote-prod:default)$ cd '/home/dmz' && curl -s -N -H 'Authorization: Bearer sk-11ef286387204367945339a85728622f' -H 'Content-Type: application/json' -d '{"model":"qwen3-coder-next:q4_K_M","messages":[{"role":"user","content":""}],"stream":true}' 'http://datawatch:3000/api/chat/completions' | python3 -c "import sys,json; [print(json.loads(l).get('choices',[{}])[0].get('delta',{}).get('content',''),end='',flush=True) for l in sys.stdin if l.startswith('data:') and l.strip() != 'data: [DONE]' for l in [l[5:].strip()]]"; echo; echo 'DATAWATCH_COMPLETE: openwebui done'
# updates
- each session log should capture (in auditable format) prompts and communication channel details of response.  there should be a way through api, mcp, cli, channels and web ui to browse session actions
- for all connections that support TLS enablment there should be a configurable option to add TLS on unique port; if the normal services that use that port can use self signed certs (ie do not validate cert on connection) might want to keep TLS on the default port, some like web may want a 2nd port for TLS and keep the non-TLS port active.
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
