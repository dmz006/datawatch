# [parity] Projects: manage projects form and the Open settings fix (datawatch-app follow-up)

**Filed:** https://github.com/dmz006/datawatch-app/issues/464 (datawatch v9.154.3). This is the tracked draft; the issue lives on the `dmz006/datawatch-app` repository. Filing the issue does not change a line of this repo. Draft written 2026-11-12.

**Android and iPhone are tracked here, not changed here.**

## What changed in datawatch

On the web page's Automata, Projects sub-tab, the **Open settings** link now opens Settings, Automata, Project policy. A collapsible **Manage projects** block under the project picker adds, edits and removes a project (name, directory, description, default backend). It is open when there are no projects, while one is edited and while an error shows; name and directory are required. It uses the existing project endpoints, so there is no new setting, endpoint or tool.

## What the mobile app needs

- If the app has a Projects view, a way to add, edit and remove a project with the same four fields, and an Open settings entry that lands on the project policy section.
- The web page added about 20 strings in all five locales (`projects_manage_*` and the Open settings label); reuse the wording where the app has the same controls.
