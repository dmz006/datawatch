// Package ebpfsetup holds the host-side steps behind `datawatch setup ebpf`:
// the exact capability list and argument list that grant a non-root daemon
// its eBPF capabilities, the sudoers rule that lets one user apply them to
// one binary without a password, installing and removing that rule, reading
// the status of all three pieces, and re-applying the capabilities after a
// release replaced the binary.
//
// Capabilities live on the binary file, and every release writes a new file,
// so each release removes them. The sudoers rule is how that is kept
// working without a root shell: it allows exactly one command line, with no
// wildcard. Everything that touches the host runs through a [Runner], so the
// tests use fakes and never call sudo.
package ebpfsetup
