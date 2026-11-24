# datawatch-app parity draft: eBPF probe load and the setup command (file only, no issue filed)

Date: 2026-11-24. Nothing in the app has to change for this work; this draft records why.

- **No new surface.** `datawatch setup ebpf --sudoers`, `--install-sudoers`, `--remove-sudoers`, `--status` and
  `--reapply` are host command-line modes: they run `setcap`, `visudo` and `sudo` on the machine the daemon runs on.
  There is deliberately no REST endpoint, MCP tool, chat verb, setting or web control, so the mobile client has
  nothing to mirror.
- **Existing read the app may already show.** `GET /api/observer/stats?v=2` carries `host.ebpf` with `configured`,
  `capability`, `kprobes_loaded` and `message`. A daemon with file capabilities now loads its kernel probes, so
  `kprobes_loaded` becomes true where it used to stay false with `open /proc/self/mem: permission denied`.
  The `message` text when the capability is missing now says that a release removes the capabilities and names
  `datawatch setup ebpf` and a restart. If the app shows `message` verbatim, no change is needed. If it maps the old
  text to a label, update the mapping to the new wording.
- **No locale keys, no config keys, no auth change.**
