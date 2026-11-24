// Package ebpfkver supplies the running kernel's version to eBPF kprobe
// programs so the loader never has to detect it itself.
//
// The cilium/ebpf library detects the kernel version for a kprobe program
// whose spec carries none by reading the vDSO through /proc/self/mem. A
// daemon started from a binary with file capabilities (the way
// `datawatch setup ebpf` grants cap_bpf, cap_perfmon and cap_sys_resource
// to a non-root daemon) is not dumpable, and the kernel refuses that read,
// so the load fails with "open /proc/self/mem: permission denied". Setting
// the version on the specs avoids the read, keeps the process non-dumpable
// and needs no root.
package ebpfkver
