// SPDX-License-Identifier: GPL-2.0
// BPF program for per-PID TCP byte tracking
#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 8192);
    __type(key, __u32);    // PID
    __type(value, __u64);  // cumulative bytes
} tx_bytes SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 8192);
    __type(key, __u32);
    __type(value, __u64);
} rx_bytes SEC(".maps");

SEC("kprobe/tcp_sendmsg")
int trace_tcp_sendmsg(struct pt_regs *ctx) {
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    size_t size = PT_REGS_PARM3(ctx);
    __u64 *val = bpf_map_lookup_elem(&tx_bytes, &pid);
    if (val) {
        __sync_fetch_and_add(val, size);
    } else {
        __u64 init = size;
        bpf_map_update_elem(&tx_bytes, &pid, &init, BPF_ANY);
    }
    return 0;
}

SEC("kprobe/tcp_recvmsg")
int trace_tcp_recvmsg(struct pt_regs *ctx) {
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    // tcp_recvmsg return value is the number of bytes received
    // We can't get it from kprobe entry — use kretprobe instead
    return 0;
}

SEC("kretprobe/tcp_recvmsg")
int trace_tcp_recvmsg_ret(struct pt_regs *ctx) {
    __u32 pid = bpf_get_current_pid_tgid() >> 32;
    int ret = PT_REGS_RC(ctx);
    if (ret <= 0) return 0;
    __u64 size = (__u64)ret;
    __u64 *val = bpf_map_lookup_elem(&rx_bytes, &pid);
    if (val) {
        __sync_fetch_and_add(val, size);
    } else {
        bpf_map_update_elem(&rx_bytes, &pid, &size, BPF_ANY);
    }
    return 0;
}

char LICENSE[] SEC("license") = "GPL";
