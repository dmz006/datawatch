# Install

> **2026-04-29 — merged into the how-to.** Operator directive: keep
> one canonical install doc. Full guide at
> [`docs/howto/setup-and-install.md`](howto/setup-and-install.md) —
> covers all five install paths (one-liner, pre-built binary,
> `go install`, container, Helm/Kubernetes), systemd unit promotion,
> NFS-backed persistence, cluster paths reference, self-managing
> config, messaging-backend wizards, and the first-session smoke
> test.

This page stays as a redirect so existing bookmarks + cross-links
keep resolving. Pick your path:

| Path | Time to ready | Section in the how-to |
|------|---------------|-----------------------|
| One-liner (Linux/macOS) | ~30 s | [Option A](howto/setup-and-install.md#option-a--one-liner) |
| Pre-built binary | ~1 min | [Option B](howto/setup-and-install.md#option-b--pre-built-binary) |
| `go install` (build from tip) | ~1 min | [Option C](howto/setup-and-install.md#option-c--go-install) |
| Container (`ghcr.io/dmz006/datawatch-parent-full`) | ~2 min | [Option D](howto/setup-and-install.md#option-d--container) |
| Helm / Kubernetes | ~5 min | [Option E](howto/setup-and-install.md#option-e--helm-on-kubernetes) |

After install:

- [systemd unit promotion](howto/setup-and-install.md#promote-to-systemd-linux-single-host)
- [Cluster paths reference](howto/setup-and-install.md#cluster-paths-datawatch-holds-helm-install-only)
- [Self-managing config](howto/setup-and-install.md#self-managing-config-helm-install-only)
- [Messaging backend setup](howto/setup-and-install.md#messaging-backend-setup-signal--telegram--discord--matrix)
- [Initial config wizard](howto/setup-and-install.md#initial-config-wizard)
- [First session smoke test](howto/setup-and-install.md#smoke-test--start-your-first-session)

For day-two ops (start/stop/upgrade/diagnose), see
[`docs/howto/daemon-operations.md`](howto/daemon-operations.md).
For deep config reference, see
[`docs/operations.md`](operations.md).
