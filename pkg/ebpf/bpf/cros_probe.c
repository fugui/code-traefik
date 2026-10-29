//go:build ignore

#include <linux/bpf.h>
#include <linux/types.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>

#define AF_INET 2
#define IPPROTO_TCP 6

#define MODE_RECORDING 0
#define MODE_REPLAY    1

#define STAT_BYPASSED_SELF 0
#define STAT_RINGBUF_DROPS 1
#define STAT_REDIRECTED    2

#define MAX_CHUNK_SIZE 4096

typedef struct {
    __u32 agent_pid;
    __u16 proxy_port;
    __u16 mock_port;
} agent_cfg;

typedef struct {
    __u64 session_id;
    __u64 process_id;
    __u16 listen_port;
    __u8 mode;
    __u8 pad;
} target_proc;

typedef struct {
    __u64 buf_ptr;
    __s32 fd;
    __u8 is_read;
    __u8 pad[3];
} rw_args;

typedef struct {
    __u32 pid;
    __s32 fd;
} pid_fd;

typedef struct {
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u32 conn_seq;
    __u64 stream_off_rx;
    __u64 stream_off_tx;
} conn_tuple;

typedef struct {
    __u64 process_id;
    __u32 dst_ip;
    __u16 dst_port;
    __u16 pad;
} ds_key;

typedef struct {
    __u16 redirect_port;
    __u8 is_replay;
    __u8 pad;
} ds_target;

typedef struct {
    __u64 process_id;
    __u32 pid;
    __u32 orig_ip;
    __u16 orig_port;
    __u16 pad;
} orig_dst;

typedef struct {
    __u32 ip;
    __u16 port;
    __u16 pad;
} client_addr;

typedef struct {
    __u64 session_id;
    __u64 process_id;
    __u32 pid;
    __u8 direction; // 1=INGRESS_REQ, 2=INGRESS_RSP
    __u8 pad[3];
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u32 conn_seq;
    __u64 stream_off;
    __u32 data_len;
    __u8 data[MAX_CHUNK_SIZE];
} ing_data_event;

typedef struct {
    __u64 process_id;
    __u32 pid;
    __u32 dst_ip;
    __u16 dst_port;
    __u16 pad;
} conn_observe_event;

const ing_data_event *__unused_ide __attribute__((unused));
const conn_observe_event *__unused_coe __attribute__((unused));

struct trace_event_raw_sys_enter {
    __u64 unused;
    long int id;
    unsigned long args[6];
};

struct trace_event_raw_sys_exit {
    __u64 unused;
    long int id;
    long int ret;
};

// --- BPF Maps ---

struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, agent_cfg);
} agent_cfg_map SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 1024);
    __type(key, __u32);
    __type(value, target_proc);
} target_pids_map SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 16384);
    __type(key, __u64); // tgid_pid
    __type(value, rw_args);
} active_rw_args_map SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 65536);
    __type(key, pid_fd);
    __type(value, conn_tuple);
} ing_conn_map SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 4096);
    __type(key, ds_key);
    __type(value, ds_target);
} downstream_rules_map SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 65536);
    __type(key, __u64); // socket cookie
    __type(value, orig_dst);
} cookie_orig_dst_map SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 65536);
    __type(key, client_addr);
    __type(value, orig_dst);
} nat_tuple_map SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 16 * 1024 * 1024); // 16MB
} ing_ringbuf SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1024 * 1024); // 1MB
} connect_observe_ringbuf SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 8);
    __type(key, __u32);
    __type(value, __u64);
} ebpf_stats_map SEC(".maps");

static __always_inline void incr_stat(__u32 idx) {
    __u64 *val = bpf_map_lookup_elem(&ebpf_stats_map, &idx);
    if (val) {
        (*val)++;
    }
}

// --- Programs ---

SEC("cgroup/connect4")
int cros_connect4(struct bpf_sock_addr *ctx) {
    if (ctx->family != AF_INET || (ctx->protocol != IPPROTO_TCP && ctx->protocol != 0)) {
        return 1;
    }

    __u32 zero = 0;
    agent_cfg *cfg = bpf_map_lookup_elem(&agent_cfg_map, &zero);
    if (!cfg) {
        return 1;
    }

    __u32 pid = (__u32)(bpf_get_current_pid_tgid() >> 32);

    // 核心红线 1：Agent 自身进程发起的一切出站连接直接放行，杜绝同机互调死锁
    if (pid == cfg->agent_pid) {
        incr_stat(STAT_BYPASSED_SELF);
        return 1;
    }

    target_proc *tp = bpf_map_lookup_elem(&target_pids_map, &pid);
    if (!tp) {
        return 1; // 非纳管目标进程，原样放行
    }

    ds_key key = {
        .process_id = tp->process_id,
        .dst_ip = ctx->user_ip4,
        .dst_port = ctx->user_port,
        .pad = 0,
    };
    ds_target *dst = bpf_map_lookup_elem(&downstream_rules_map, &key);
    if (!dst) {
        // 兼容同机通过 127.0.0.1 访问已登记宿主机下游端口
        key.dst_ip = bpf_htonl(0x7f000001);
        dst = bpf_map_lookup_elem(&downstream_rules_map, &key);
    }

    // 未命中下游重定向规则分支
    if (!dst) {
        // 重放模式下执行 Fail-Closed：严禁未声明连接外泄到真实网络，直接拒绝建连
        if (tp->mode == MODE_REPLAY) {
            return 0; // 返回 0 (EPERM)，内核强制阻断连接
        }
        // 录制模式下：投递观测事件以检出架构漂移 (VIOLATION)
        conn_observe_event obs = {
            .process_id = tp->process_id,
            .pid = pid,
            .dst_ip = ctx->user_ip4,
            .dst_port = ctx->user_port,
            .pad = 0,
        };
        bpf_ringbuf_output(&connect_observe_ringbuf, &obs, sizeof(obs), 0);
        return 1; // 原样放行
    }

    __u64 cookie = bpf_get_socket_cookie(ctx);
    orig_dst od = {
        .process_id = tp->process_id,
        .pid = pid,
        .orig_ip = ctx->user_ip4,
        .orig_port = ctx->user_port,
        .pad = 0,
    };
    bpf_map_update_elem(&cookie_orig_dst_map, &cookie, &od, BPF_ANY);

    ctx->user_ip4 = bpf_htonl(0x7f000001); // 重定向到 127.0.0.1 本地代理或 MockServer
    ctx->user_port = dst->redirect_port;
    incr_stat(STAT_REDIRECTED);
    return 1;
}

SEC("sockops")
int cros_sock_ops(struct bpf_sock_ops *skops) {
    if (skops->family != AF_INET) {
        return 1;
    }
    if (skops->op == BPF_SOCK_OPS_ACTIVE_ESTABLISHED_CB) {
        __u64 cookie = bpf_get_socket_cookie(skops);
        orig_dst *od = bpf_map_lookup_elem(&cookie_orig_dst_map, &cookie);
        if (od) {
            client_addr addr = {
                .ip = skops->local_ip4,
                .port = (__u16)bpf_ntohl(skops->local_port),
                .pad = 0,
            };
            bpf_map_update_elem(&nat_tuple_map, &addr, od, BPF_ANY);
            bpf_map_delete_elem(&cookie_orig_dst_map, &cookie);
        }
    }
    return 1;
}

// 系统调用读写参数暂存与事件发送辅助函数
static __always_inline void handle_rw_enter(void *buf, __s32 fd, __u8 is_read) {
    if (fd <= 2) {
        return;
    }
    __u64 tgid_pid = bpf_get_current_pid_tgid();
    __u32 pid = (__u32)(tgid_pid >> 32);

    target_proc *tp = bpf_map_lookup_elem(&target_pids_map, &pid);
    if (!tp) {
        return;
    }

    pid_fd pfd = { .pid = pid, .fd = fd };
    conn_tuple *conn = bpf_map_lookup_elem(&ing_conn_map, &pfd);
    if (!conn) {
        conn_tuple default_conn = {
            .src_ip = bpf_htonl(0x7f000001),
            .dst_ip = bpf_htonl(0x7f000001),
            .src_port = 0,
            .dst_port = bpf_htons(tp->listen_port),
            .conn_seq = (__u32)fd,
            .stream_off_rx = 0,
            .stream_off_tx = 0,
        };
        bpf_map_update_elem(&ing_conn_map, &pfd, &default_conn, BPF_NOEXIST);
        conn = bpf_map_lookup_elem(&ing_conn_map, &pfd);
        if (!conn) {
            return;
        }
    }

    rw_args args = {
        .buf_ptr = (__u64)buf,
        .fd = fd,
        .is_read = is_read,
        .pad = {0},
    };
    bpf_map_update_elem(&active_rw_args_map, &tgid_pid, &args, BPF_ANY);
}

static __always_inline void handle_rw_exit(long ret_bytes) {
    if (ret_bytes <= 0) {
        return;
    }

    __u64 tgid_pid = bpf_get_current_pid_tgid();
    rw_args *args = bpf_map_lookup_elem(&active_rw_args_map, &tgid_pid);
    if (!args) {
        return;
    }

    __u32 pid = (__u32)(tgid_pid >> 32);
    target_proc *tp = bpf_map_lookup_elem(&target_pids_map, &pid);
    if (!tp) {
        bpf_map_delete_elem(&active_rw_args_map, &tgid_pid);
        return;
    }

    pid_fd pfd = { .pid = pid, .fd = args->fd };
    conn_tuple *conn = bpf_map_lookup_elem(&ing_conn_map, &pfd);
    if (!conn) {
        bpf_map_delete_elem(&active_rw_args_map, &tgid_pid);
        return;
    }

    __u64 current_off;
    __u8 direction;
    if (args->is_read) {
        current_off = conn->stream_off_rx;
        conn->stream_off_rx += ret_bytes;
        direction = 1; // INGRESS_REQ
    } else {
        current_off = conn->stream_off_tx;
        conn->stream_off_tx += ret_bytes;
        direction = 2; // INGRESS_RSP
    }

    __u32 chunk_len = ret_bytes > MAX_CHUNK_SIZE ? MAX_CHUNK_SIZE : (__u32)ret_bytes;
    ing_data_event *evt = bpf_ringbuf_reserve(&ing_ringbuf, sizeof(*evt), 0);
    if (evt) {
        evt->session_id = tp->session_id;
        evt->process_id = tp->process_id;
        evt->pid = pid;
        evt->direction = direction;
        evt->pad[0] = 0; evt->pad[1] = 0; evt->pad[2] = 0;
        evt->src_ip = conn->src_ip;
        evt->dst_ip = conn->dst_ip;
        evt->src_port = conn->src_port;
        evt->dst_port = conn->dst_port;
        evt->conn_seq = conn->conn_seq;
        evt->stream_off = current_off;
        evt->data_len = chunk_len;
        bpf_probe_read_user(evt->data, chunk_len, (void *)args->buf_ptr);
        bpf_ringbuf_submit(evt, 0);
    } else {
        incr_stat(STAT_RINGBUF_DROPS);
    }

    bpf_map_delete_elem(&active_rw_args_map, &tgid_pid);
}

// Syscall Tracepoints
SEC("tracepoint/syscalls/sys_enter_read")
int trace_sys_enter_read(struct trace_event_raw_sys_enter *ctx) {
    handle_rw_enter((void *)ctx->args[1], (__s32)ctx->args[0], 1);
    return 0;
}

SEC("tracepoint/syscalls/sys_exit_read")
int trace_sys_exit_read(struct trace_event_raw_sys_exit *ctx) {
    handle_rw_exit(ctx->ret);
    return 0;
}

SEC("tracepoint/syscalls/sys_enter_write")
int trace_sys_enter_write(struct trace_event_raw_sys_enter *ctx) {
    handle_rw_enter((void *)ctx->args[1], (__s32)ctx->args[0], 0);
    return 0;
}

SEC("tracepoint/syscalls/sys_exit_write")
int trace_sys_exit_write(struct trace_event_raw_sys_exit *ctx) {
    handle_rw_exit(ctx->ret);
    return 0;
}

SEC("tracepoint/syscalls/sys_enter_recvfrom")
int trace_sys_enter_recvfrom(struct trace_event_raw_sys_enter *ctx) {
    handle_rw_enter((void *)ctx->args[1], (__s32)ctx->args[0], 1);
    return 0;
}

SEC("tracepoint/syscalls/sys_exit_recvfrom")
int trace_sys_exit_recvfrom(struct trace_event_raw_sys_exit *ctx) {
    handle_rw_exit(ctx->ret);
    return 0;
}

SEC("tracepoint/syscalls/sys_enter_sendto")
int trace_sys_enter_sendto(struct trace_event_raw_sys_enter *ctx) {
    handle_rw_enter((void *)ctx->args[1], (__s32)ctx->args[0], 0);
    return 0;
}

SEC("tracepoint/syscalls/sys_exit_sendto")
int trace_sys_exit_sendto(struct trace_event_raw_sys_exit *ctx) {
    handle_rw_exit(ctx->ret);
    return 0;
}

// 进程重启热更新 Tracepoint
SEC("tracepoint/sched/sched_process_exec")
int cros_sched_exec(void *ctx) {
    // 捕获新进程启动，配合用户态 2s 巡检
    return 0;
}

char _license[] SEC("license") = "GPL";
