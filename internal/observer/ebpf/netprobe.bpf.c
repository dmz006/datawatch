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

// BL180 Phase 2 (v5.13.0) — per-connection attribution map. Keyed by
// the local socket pointer (kernel struct sock *) to avoid the
// 4-tuple gymnastics in BPF context (we read the tuple in userspace).
// Value is (pid, timestamp_ns) so userspace can prune stale entries.
//
// LRU_HASH means the kernel auto-evicts the oldest entry when the map
// fills, so we don't need a userspace pruner for memory bounds. The
// userspace TTL pruner still runs to keep the visible set fresh.
struct conn_attr_v {
    __u32 pid;
    __u32 _pad;
    __u64 ts_ns;
};

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, MAX_ENTRIES);
    __type(key, __u64);             // socket pointer cast to u64
    __type(value, struct conn_attr_v);
} conn_attribution SEC(".maps");

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

// BL180 Phase 2 (v5.13.0) — outbound conn attribution. tcp_v4_connect /
// tcp_v6_connect fire after __sys_connect's argument validation but
// before the kernel sends SYN, so the (sock, current pid) binding is
// captured at the point where the conn is irrevocably committed. We
// hook tcp_connect (a kernel internal both v4 and v6 call) and key on
// the sock pointer so v6 → v4 migration doesn't matter.
//
// inet_csk_accept fires on the server side returning the new accepted
// socket. The PID is the listener's accept-loop process — useful for
// associating inbound conns with the backend that accepted them.
SEC("kprobe/tcp_connect")
int BPF_KPROBE(kprobe_tcp_connect, struct sock *sk) {
    if (!sk) return 0;
    struct conn_attr_v v = {};
    v.pid = bpf_get_current_pid_tgid() >> 32;
    v.ts_ns = bpf_ktime_get_ns();
    __u64 key = (__u64)(unsigned long)sk;
    bpf_map_update_elem(&conn_attribution, &key, &v, BPF_ANY);
    return 0;
}

SEC("kretprobe/inet_csk_accept")
int BPF_KRETPROBE(kretprobe_inet_csk_accept, struct sock *new_sk) {
    if (!new_sk) return 0;
    struct conn_attr_v v = {};
    v.pid = bpf_get_current_pid_tgid() >> 32;
    v.ts_ns = bpf_ktime_get_ns();
    __u64 key = (__u64)(unsigned long)new_sk;
    bpf_map_update_elem(&conn_attribution, &key, &v, BPF_ANY);
    return 0;
}

char __license[] SEC("license") = "GPL";
