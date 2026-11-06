#!/usr/bin/env bash
# ==============================================================================
# code-traefik-agent 智能启动包装脚本
# 自动探测 EUID，若非 root 用户则尝试使用 sudo 提权启动，保障具备 eBPF 内核特权
# ==============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_PATH="$SCRIPT_DIR/bin/code-traefik-agent"

if [ ! -f "$BIN_PATH" ]; then
    BIN_PATH="$SCRIPT_DIR/../code-traefik-agent"
fi

if [ ! -f "$BIN_PATH" ]; then
    echo -e "\033[31m[错误] 未找到 code-traefik-agent 二进制文件，请先执行 make build\033[0m" >&2
    exit 1
fi

if [ "$EUID" -ne 0 ]; then
    echo -e "\033[33m[Agent 启动器] 检测到当前以非 root (UID=$EUID) 运行，使用 sudo 提权以获取 eBPF 内核特权...\033[0m"
    exec sudo "$BIN_PATH" "$@"
else
    exec "$BIN_PATH" "$@"
fi
