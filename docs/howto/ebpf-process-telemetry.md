---
docs:
  index: true
  topics: [ebpf, observer, telemetry, capabilities, sudoers, setcap, update, kprobes, cap_bpf]
---
# How-to: eBPF per-process telemetry

eBPF lets the observer count network bytes per process and per session, which
feeds per-session telemetry and capacity planning. It needs three Linux
capabilities on the daemon: `cap_bpf` (load programs), `cap_perfmon` (attach
kernel probes) and `cap_sys_resource` (lift the memory-lock limit). A daemon
that runs as your own user gets them from **file capabilities on the binary**.

This is a command-line and host change only. There is no REST, MCP, chat or
web surface for it, because it changes the host and needs `sudo`. You can read
the result in the observer stats (below).

## Base requirements

- Linux 5.8 or newer with kernel BTF (`/sys/kernel/btf/vmlinux`).
- `setcap` (`libcap2-bin` on Debian and Ubuntu) and `sudo`.
- The `datawatch` binary at a plain path: no spaces or wildcard characters.

## The sequence: install, `setup ebpf`, restart

1. **Install** the binary (`make install`, `datawatch update`, or your own copy).
2. **Grant the capabilities** with `datawatch setup ebpf`. It asks for your sudo
   password once and runs `sudo setcap cap_bpf,cap_perfmon,cap_sys_resource=ep <binary>`.
   It also turns eBPF on in the config.
3. **Restart the daemon.** A running process keeps the capabilities it started
   with, so the daemon must be restarted to pick them up.

Check it with `datawatch setup ebpf --status` (below), or read the observer
stats: `host.ebpf.capability` and `host.ebpf.kprobes_loaded` are both true when
it works. When the kernel probes could not load, `host.ebpf.message` says why.

## Why every release removes the capabilities

Capabilities are stored on the binary **file**. A release writes a new file,
so the new file has none, and eBPF stays off until `datawatch setup ebpf` is run
again. There are two ways to deal with that:

- Run `datawatch setup ebpf` with sudo after every release, then restart.
- Install the passwordless rule below once. After that, `datawatch update` and
  `make install` restore the capabilities by themselves.

## The sudoers rule (keep eBPF working across releases)

The rule lets **your user** run exactly one command as root without a password:
the `setcap` call that `setup ebpf` makes, on that one binary. It has no
wildcard: sudoers matches the whole argument list exactly.

Print it first. This changes nothing:

```
datawatch setup ebpf --sudoers
```

The output names the file, `/etc/sudoers.d/datawatch-ebpf-<user>` (a different
name for `--target stats`), and the two lines it holds. For user `alice` and a
binary at `/opt/dw/bin/datawatch` they are:

```
# datawatch-managed: ebpf-setcap (written by `datawatch setup ebpf --install-sudoers`; remove with `datawatch setup ebpf --remove-sudoers`)
alice ALL=(root) NOPASSWD: /usr/sbin/setcap cap_bpf\,cap_perfmon\,cap_sys_resource\=ep /opt/dw/bin/datawatch
```

The user name, the `setcap` path and the binary path come from your system. The
commas and the equals sign are escaped because sudoers reads them as
separators.

Install it yourself with `sudo visudo -f /etc/sudoers.d/datawatch-ebpf-<user>`
and paste the two lines, or let datawatch do it:

```
datawatch setup ebpf --install-sudoers
```

That writes the rule to a temporary file, checks it with `visudo -cf`, and only
if it is valid installs it root-owned with mode 0440 through `sudo` (one
password prompt in the terminal). If the check fails it prints visudo's error
and installs nothing. It also refuses to overwrite a file of the same name that
datawatch did not write.

### Remove it

```
datawatch setup ebpf --remove-sudoers
```

This removes the drop-in **only if it carries the marker comment** on its first
line. A file without the marker is left alone and the command says so.

### Status

```
datawatch setup ebpf --status
```

It reports three things: whether the binary file has the capabilities, whether
the **running** daemon holds them (and whether the kernel probes loaded), and
whether the passwordless rule is present (`sudo -n -l` with the exact command).
It ends with the next step: for example, restart the daemon when the file has
the capabilities but the daemon started before.

### Security note

The rule lets that user set those three capabilities on that one file without a
password. **Anyone who can write to that binary file can then run code with
them.** Keep the binary owned by you and not writable by anyone else (mode 0755
or tighter, in a directory only you can write to). Remove the rule if you stop
using eBPF.

## What an update does

After `datawatch update` (and the update button in the web app) replaces the
binary, and after `make install`:

- If the rule exists, the capabilities are re-applied with `sudo -n`, which
  never asks for a password. The message says to restart the daemon.
- If the rule does not exist, or sudo refuses, and the old file had the
  capabilities, the update prints the one command to run:
  `sudo <setcap> cap_bpf,cap_perfmon,cap_sys_resource=ep <binary>`.
- If neither applies, it prints nothing.

You can run the same step by hand with `datawatch setup ebpf --reapply`.
A custom deploy script should call it after it installs the binary.

## When the kernel probes do not load

A daemon with file capabilities is not dumpable, and an older loader failed with
`open /proc/self/mem: permission denied` while detecting the kernel version.
That is fixed: the loader sets the kernel version itself and needs neither root
nor a dumpable process. If `host.ebpf.message` still shows a permission error,
the message names the cause: the file lost its capabilities (run
`datawatch setup ebpf`) or the daemon was not restarted since.

## Reference

| Command | What it does | Changes the host |
|---|---|---|
| `datawatch setup ebpf` | Grants the capabilities (sudo password), enables eBPF in the config | yes |
| `datawatch setup ebpf --target stats` | The same for the standalone `datawatch-stats` binary | yes |
| `datawatch setup ebpf --sudoers` | Prints the rule and its file | no |
| `datawatch setup ebpf --install-sudoers` | Validates with `visudo -cf`, installs the drop-in | yes |
| `datawatch setup ebpf --remove-sudoers` | Removes the drop-in (marker required) | yes |
| `datawatch setup ebpf --status` | File, daemon and rule status | no |
| `datawatch setup ebpf --reapply` | Re-apply after a release (`sudo -n`), or print the command | only with the rule |
| `datawatch setup ebpf --disable` | Turns eBPF off in the config | config |
