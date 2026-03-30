# bugs (open — ordered by priority)
- the alerts tab top menus were suppose to be cleaned up and look more professional. nothings was done, fix
- the menus on bottom of the page were suppose to be cleaned up and look more professional. nothing was done, fix
- the sessions page was suppose to have icon filters put between filter search and show history for each llm type (generate distinctive icon badges) only showing those that are in the active list (taking show history into account)
- on new session the auto gitinit and auto git commit were suppose to be moved to one line, nothign was done. fix
- on start a new session, the restart a previous session needs a filter and quick button LLM type filter like the sessions page
- alerts view should also display the commands sent, inline with the prompt response details.
- validate that all actions llm does that are discovered (thinking, processing, running, waiting on prompt, etc) all have badges that update the session in sessions list and inside the session. this was suppose to be done but i don't think it was. fix it.
- setting tab "Changes require a daemon restart to take efect. Restart now" should only be displayed if there were changes that need a restart.  there is a restart button at bottom of page, it is ok there; but a change should auto-restart and if it can't the prompt should display (and be highlighted) that a restart is required
- Suppress toasts was not documented; make sure all features are documented. do a full review and make sure nothing is missing.
- clicking on a host listen interface (bind host, sse host, should pop up a selector for interfaces; earlier instruction had full details but it should be multi-select; review prior plansand implement
- individual llm configs for auto git init and auto git commit are not there; make sure all llm and communication configuration options are available to edit in web ui and communication channels
- review all config options and make sure they are accessible in the settings page
- review all earlier plans and make sure all requested changes are done; many of these bugs were said to be fixed!
- bash shell recognizes the prompt after the first action; but not when session starts and screen does not show the prompt. this was an earlier bug, debug interactively
- i am not seeing the new ansi terminal, i tested a top and it is scrolling not doing the full ansi terminal. debug ansi terminal and fix

# planned

# backlog
- evaluate alternative covert/low-profile communication channels beyond DNS tunneling (see docs/covert-channels.md)
- **IPv6 listener support** — add `[::]` bind support for all listeners (HTTP, MCP SSE, DNS, webhooks); investigate dual-stack vs IPv6-only; update config defaults and documentation
