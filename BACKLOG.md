# bugs (open — ordered by priority)
- validate that all actions llm does that are discovered (thinking, processing, running, waiting on prompt, etc) all have badges that update the session in sessions list and inside the session.  it should best work in claude and opencode-acp but in watching a claude sessoin the actions never changed as it was working on things
- validate clicking on a host listen interface (bind host, sse host, should pop up a selector for interfaces; earlier instruction had full details but it should be multi-select; review prior plans and implement
- review all config options and make sure they are accessible in the settings page, for example the prompt filters are nto there and the recent console size
- when tls is enabled and set on another port the original 8080 stops working, so there is no service to redirect to tls. test all tls configurations work; disabling back to system initial config when done. after testing tls test interface configuration; should be easy to bind to a different interaface or two, save/restart (auto restart is enbled) and test those interfaces. resetting back to all when done
- llm prompt filtering should be editable for each llm
- make sure any config editing options are available to all communication channels to start
- why is openweb a series of script / curl connections. is it possible to connect and do an interactive session with it via api? can the experience be better?
- bash like openwebui is not rendering fine. it is like it is not seeing the ansi terminal.  the screen font is resizable but it scrolls to the bottom of the window. for opencode it was constrained to the defined window.  Is anything different? Is the bash llm still filtering ansi? same for any other llm?
- opencode is a little messed up also; it's not starting now, after it starts it just hangs; like ACP isn't conneting or something is causing a problem.  Start test sessions and debug
- when connecting to a session it is like it tries to play back the entire buffer causing the screen to scroll a bit vs just starting with screenshto of where it is and then getting terminal updates from there

# planned

# backlog
- evaluate alternative covert/low-profile communication channels beyond DNS tunneling (see docs/covert-channels.md)
- **IPv6 listener support** — add `[::]` bind support for all listeners (HTTP, MCP SSE, DNS, webhooks); investigate dual-stack vs IPv6-only; update config defaults and documentation
