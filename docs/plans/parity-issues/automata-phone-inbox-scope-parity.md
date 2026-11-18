Parent release: datawatch v9.159.1

## What changed in datawatch

The Automata page fits a phone, and the Inbox follows the selected project. No setting, endpoint or command changed.

- On a narrow screen the Automata sub-tab bar and its action buttons, the second row under Automation (Inbox, Automations, Resources and the rest), the Projects bar and the Templates sub-tabs stay on one line and scroll sideways instead of wrapping or being cut off. The header title uses a shorter form. The Settings, Observer, Dashboard, session detail and Automaton detail tab strips use one shared scrolling strip style.
- With a project picked on the Projects sub-tab, the Inbox badge and every card under it (open questions, held questions, Triage, security failures, supervisor watch findings) show only that project's items; Overview counts everything waiting on you across all projects. The scope is a display rule of the web app: REST, MCP, the CLI and the chat channel still return every item.
- The Templates tab and its sub-tabs have their own labels (`automata_tab_templates`, `automata_templates_subtab_automaton`).

## What the mobile app needs

- Apply the same project scope to the Inbox badge and its cards, and keep the Overview total across all projects.
- Use the new Templates labels in all five locales (the web page changed six strings per locale).
- Keep the Automata tab strips on one scrolling line on a phone.
