#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEMO_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
BIN_DIR="${DEMO_DIR}/bin"
PID_FILE="/tmp/demo-cros-cluster.pid"

echo "=========================================================="
echo " Starting CROS RPC 4-Process Demo Microservice Cluster"
echo "=========================================================="

# 1. 编译全部可执行文件
echo "[1/4] Building binaries into ${BIN_DIR}..."
mkdir -p "${BIN_DIR}"
go build -o "${BIN_DIR}/backend-account" "${DEMO_DIR}/cmd/backend-account"
go build -o "${BIN_DIR}/backend-inventory" "${DEMO_DIR}/cmd/backend-inventory"
go build -o "${BIN_DIR}/sut-order" "${DEMO_DIR}/cmd/sut-order"
go build -o "${BIN_DIR}/client" "${DEMO_DIR}/cmd/client"

# 2. 清理已存在的进程
if [ -f "${PID_FILE}" ]; then
    echo "Stopping existing processes..."
    "${SCRIPT_DIR}/stop_all.sh" || true
fi

# 释放端口 (9001, 9002, 9003)
for port in 9001 9002 9003; do
    fuser -k -n tcp "${port}" 2>/dev/null || true
done
sleep 0.5

# 3. 依次拉起两个下游 Backend (使用 setsid nohup 脱离会话独立存活)
echo "[2/4] Starting Backend 1: Account Service (:9002)..."
setsid nohup "${BIN_DIR}/backend-account" --addr="127.0.0.1:9002" </dev/null >/tmp/demo-backend-account.log 2>&1 &
PID_ACC=$!

echo "[3/4] Starting Backend 2: Inventory Service (:9003)..."
setsid nohup "${BIN_DIR}/backend-inventory" --addr="127.0.0.1:9003" </dev/null >/tmp/demo-backend-inventory.log 2>&1 &
PID_INV=$!

sleep 0.5

# 4. 拉起被测核心服务 SUT
echo "[4/4] Starting SUT: Order Gateway (:9001)..."
setsid nohup "${BIN_DIR}/sut-order" --addr="127.0.0.1:9001" --account-addr="127.0.0.1:9002" --inventory-addr="127.0.0.1:9003" </dev/null >/tmp/demo-sut-order.log 2>&1 &
PID_SUT=$!

# 保存 PID
echo "${PID_ACC} ${PID_INV} ${PID_SUT}" > "${PID_FILE}"

sleep 0.5
echo ""
echo "=========================================================="
echo " Cluster Started Successfully!"
echo "   • Backend 1 (Account)   : PID ${PID_ACC} -> 127.0.0.1:9002 (log: /tmp/demo-backend-account.log)"
echo "   • Backend 2 (Inventory) : PID ${PID_INV} -> 127.0.0.1:9003 (log: /tmp/demo-backend-inventory.log)"
echo "   • SUT (Order Gateway)   : PID ${PID_SUT} -> 127.0.0.1:9001 (log: /tmp/demo-sut-order.log)"
echo "=========================================================="
echo ""
echo "To interact with the cluster, launch the TUI Client:"
echo "   ${BIN_DIR}/client"
echo "or run:"
echo "   make -C demo run-client"
echo ""
