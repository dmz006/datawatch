# bugs (fixed in v0.6.35)
- ~~independent opencode/opencode-acp/opencode-prompt enabled flags~~ FIXED
- ~~textarea full width~~ FIXED
- ~~opencode-prompt status updates (--print-logs)~~ FIXED
- ~~hidden input for single-prompt sessions~~ FIXED
- ~~git init before commit ordering~~ FIXED
- ~~openwebui empty task error~~ FIXED (PromptRequired + validation)
- ~~testing tracker updated~~ FIXED

# bugs (remaining)
- claude MCP timeout should not kill session — dismiss banner, remove channel tab, let tmux work
- claude-code has no enabled flag (always returns true) — should be configurable like other LLMs
- ollama.ListModels() ignores config host when called from API (hardcodes localhost:11434 fallback)
- openwebui.ListModels() ignores config URL when called from API (hardcodes localhost:3000 fallback)
- channel port 7433 fallback in handleChannelReady should read from config
- opencode-acp startup timeout (30s), health check interval (5s), message timeout (30s) not configurable
- per-LLM auto_git_commit/auto_git_init overrides not yet in LLM config structs

# bugs (previously fixed, reference only)
- openwebui error below, debug and fix:
cd '/home/dmz' && curl -s -N -H 'Authorization: Bearer sk-11ef286387204367945339a85728622f' -H 'Content-Type: application/json' -d '{"model":"qwen3-coder-next:q4_K_M","messages":[{"role":"user","content":""}],"stream":true}' 'http://datawatch:3000/api/chat/completions' | python3 -c "import sys,json; [print(json.loads(l).get('choices',[{}])[0].get('delta',{}).get('content',''),end='',flush=True) for l in sys.stdin if l.startswith('data:') and l.strip() != 'data: [DONE]' for l in [l[5:].strip()]]"; echo; echo 'DATAWATCH_COMPLETE: openwebui done'
dmz@ralfthewise datawatch [main] (⎈ |infosecquote-prod:default)$ cd '/home/dmz' && curl -s -N -H 'Authorization: Bearer sk-11ef286387204367945339a85728622f' -H 'Content-Type: application/json' -d '{"model":"qwen3-coder-next:q4_K_M","messages":[{"role":"user","content":""}],"stream":true}' 'http://datawatch:3000/api/chat/completions' | python3 -c "import sys,json; [print(json.loads(l).get('choices',[{}])[0].get('delta',{}).get('content',''),end='',flush=True) for l in sys.stdin if l.startswith('data:') and l.strip() != 'data: [DONE]' for l in [l[5:].strip()]]"; echo; echo 'DATAWATCH_COMPLETE: openwebui done'
# updates
- review the go modules and code created in ../signal-go/ - test and validate it works for our implementation of datawatch
- create a git project for it and integrate into datawatch and remove the signal-cli and java dependencies. the local datawatch installation is already linked to signal with signal-cli, see if link can be re-used and tested with new signal-go integration
- review docs, there was mention of this in a future planning; update future planning with all recent changes
# toplan
- make a plan for changing the tmux web ui session to be a fully supported ANSI console so full ansi animated tools like claude and opencode do not need to escape codes and can display properly.  Make the default font size able to fit on normal cell phone width but allow changing of font and allowing user to scroll left and right and up and down to view the entire screen.  provide prompts on the right side to scroll or page back in history like console would
- make a plan for reviewing all built in detection filters for prompts and other hard coded settings and identify how they can be flexable by-llm or chat channel and extend the llm and chat configuration to include saving of them in the config file then make sure all channel and webui configuration options are available.  make a rule that other prompt or chat or other configuratoins are now in a by-llm or by-chat configuration in the config file and to not hard code those settings
- make a plan for datawatch capturing system details such as top, process details, cpu details, gpu details and make the settings tab have a sub menu whenthere with tabs for each menu, the first is t
he current settings, the 2nd will be statitics showing the details gathered in this plan.  it should be real time on web ui or query through channels or mcp and show as much detail and data as possib
le.  maybe even how much disk space or details about each session also. all "sections" in the statistics should be able to collapse so user can view whichever they prefer.  feel free to add monitorin
g for anything else that would be useful for someone managing datawatch
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
