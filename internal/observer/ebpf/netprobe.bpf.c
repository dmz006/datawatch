//go:build ignore

// netprobe.bpf.c — per-pid network byte counters via TCP/UDP kprobes.
// Compiled by bpf2go (`make ebpf-gen`) into netprobe_bpfel.{go,o}.
// Tagged `ignore` so `go build` never tries to feed it to cgo.
//
// Maps:
//   bytes_rx[pid] += incoming TCP/UDP payload bytes
//   bytes_tx[pid] += outgoing TCP/UDP payload bytes
//
// Hooks:
//   kprobe/tcp_sendmsg  — outbound TCP
//   kretprobe/tcp_recvmsg — inbound TCP (return value = bytes copied)
//   kprobe/udp_sendmsg  — outbound UDP
//   kretprobe/udp_recvmsg — inbound UDP
//
// Userspace iterates the maps every observer tick.

// SPDX-License-Identifier: GPL-2.0
#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

#define MAX_ENTRIES 16384

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_ENTRIES);
    __type(key, __u32);
    __type(value, __u64);
} bytes_rx SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_ENTRIES);
    __type(key, __u32);
    __type(value, __u64);
} bytes_tx SEC(".maps");

static __always_inline void add_bytes(void *map, __u64 delta) {
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    __u64 *cur = bpf_map_lookup_elem(map, &pid);
    if (cur) {
        *cur += delta;
    } else {
        bpf_map_update_elem(map, &pid, &delta, BPF_ANY);
    }
}

SEC("kprobe/tcp_sendmsg")
int BPF_KPROBE(kprobe_tcp_sendmsg, struct sock *sk, struct msghdr *msg, size_t size) {
    add_bytes(&bytes_tx, (__u64)size);
    return 0;
}

SEC("kretprobe/tcp_recvmsg")
int BPF_KRETPROBE(kretprobe_tcp_recvmsg, int ret) {
    if (ret > 0) add_bytes(&bytes_rx, (__u64)ret);
    return 0;
}

SEC("kprobe/udp_sendmsg")
int BPF_KPROBE(kprobe_udp_sendmsg, struct sock *sk, struct msghdr *msg, size_t len) {
    add_bytes(&bytes_tx, (__u64)len);
    return 0;
}

SEC("kretprobe/udp_recvmsg")
int BPF_KRETPROBE(kretprobe_udp_recvmsg, int ret) {
    if (ret > 0) add_bytes(&bytes_rx, (__u64)ret);
    return 0;
}

char __license[] SEC("license") = "GPL";
