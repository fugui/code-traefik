#!/bin/bash
set -euo pipefail

PID_FILE="/tmp/demo-cros-cluster.pid"

echo "Stopping CROS RPC demo cluster..."

if [ -f "${PID_FILE}" ]; then
    PIDS=$(cat "${PID_FILE}")
    for pid in ${PIDS}; do
        if kill -0 "${pid}" 2>/dev/null; then
            echo "Killing process PID ${pid}..."
            kill "${pid}" 2>/dev/null || true
        fi
    done
    rm -f "${PID_FILE}"
fi

# 确保端口彻底释放
for port in 9001 9002 9003; do
    fuser -k -n tcp "${port}" 2>/dev/null || true
done

echo "All demo processes stopped and ports released."
