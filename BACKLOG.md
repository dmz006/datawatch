# today
- the app has changed so much since it was first made it does not make sense to have the workflow default to signal. maybe more instructions to init the config if needed and an example of how to set up a communication channel; signal can be the example.
- when signal sets up, if the config file isn't there it should init on it's own and when setting up it should just save the config details on it's own
- there should be a similar wizard for all other service configuration.  Each wizard or instructions on how to get info needed should provide instructions, and gather variables needed to set the configuration details.  if config file isnt' there it should init first.  gather the least information possible to connect to the service, and if things like groups or other info is needed and can be gathered through connecting to the service and offering options for user to select then do so.
- bash complete should always be updated for any cli option change
- start should start a daemon service, and detach so user can exit the shell
- should still be able to use cli to communicate with service or any of the communication methods configured
- stop should default not touch the sessions, but have a cli option to close a session
- should be able to close sessions from all access methods
- if there are specific controls, make easy select option in web ui
- should be able to configure service through web ui
- should be able to communicate with local server or over net to api interface of another service
- there should be a --secure option that uses a user provided password to encrypt the config file.  
- if loading config file identifies it is encrypted prompt for a password to start
- do not save or cache the config file password
- warning in docs that if password is lost there is no recovery 
- should be able to edit configuration through web ui
- add a tracker document that shows what interfaces have been tested, validated and what details and conditions of tests were.  Nothign has been validated yet
# next
- did the recent daemon change break how MCP works? Does anything need to be updated? Both for local mode with claude and for network mode from other AI
- wizard through all interfaces to manage connections to other server. mostly for the cli to but also can be used by web ui.  by default the configuration is for the local server on localhost and initialization should add configuration for localhost for local cli communicatoins, other servers provide an option on to be able to connect and send communications to that server with the cli or service. the web interface should also be able to leverage the other server connection to start/manage sessions on that server using whatever connection methods that server is configured for. connection methods are web, API, if local client uses socket that
- cli, web, all communication ability to check version, see if there is a new version (checking git), and do an update
- add a feature to schedule a command for a session, allow stacking of commands (ie after command add this command) or just scheduling at a time, including "now"
- add a feature to allow web ui ordering of sessions
# future
- the configuration wizards and web config settings should support all config file configuration; even wizards for the llm setups
- a feature that can manage command "strings" so commands can be pre-built and then used together with the command scheduling option
- add a feature to create custom filters on sessions that watch for specific messages and run commands which can be entered or use any of the saved commands (with optoin to save it entered), even scheduled commands. allow for alerting to be enabled or not. allow for delays or chaining commands to run (a delay could be a command).  can submit multiple of these filters to any session and specify to continue or stop if filter matches after filter run is done.  allow re-ordering of filters. 
- seed filters with common known claude and opencode message and provide examples of claude token expiration message and expected prompts as well as known opencode and claude questions that commonly come up in development or system administration situations.
- seed commands with possible commands based on seeded filters
- there should be a message channel for system messages and alerts - all normal service communication details available. this allows for a history and ability for cli or web or communication channel to review entire alerting history and an easy way in cli or web to know alerts have happened in addition to the alerts going to browser if in web
# backlog
- communication channel "DNS" - sets up a DNS SEC server that responds to specific DNS queries that use secure DNS communications to provide control structure for service.  CLI interface extended, if configured remote service is of type DNS commands are sent via dns queries to the configured domain. use the configurable DNS server (host configured or direct connect)
- do a search for other options besides DNS tunneling for alternative communication 
