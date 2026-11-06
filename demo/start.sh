#!/usr/bin/env bash
# ==============================================================================
# 脚本名称: start.sh
# 功能描述: 在 Tmux 中以田字格分屏布局启动运行 demo 微服务集群与交互客户端
# 布局结构 (2x2 田字格布局):
#   ┌───────────────────────────┬───────────────────────────┐
#   │       client (TUI)        │         sut-order         │
#   │   (左上格: 交互终端)      │   (右上格: 订单核心网关)   │
#   ├───────────────────────────┼───────────────────────────┤
#   │      backend-account      │     backend-inventory     │
#   │   (左下格: 资产服务)      │   (右下格: 库存服务)       │
#   └───────────────────────────┴───────────────────────────┘
# ==============================================================================

set -euo pipefail

# 获取 demo 项目根目录（自适应符号链接与脚本所在层级）
SOURCE="${BASH_SOURCE[0]}"
while [ -h "$SOURCE" ]; do
    DIR="$(cd "$(dirname "$SOURCE")" && pwd)"
    SOURCE="$(readlink "$SOURCE")"
    [[ $SOURCE != /* ]] && SOURCE="$DIR/$SOURCE"
done
DEMO_DIR="$(cd "$(dirname "$SOURCE")" && pwd)"
BIN_DIR="$DEMO_DIR/bin"
SESSION_NAME="demo-cros"

# 颜色高亮定义
GREEN="\033[0;32m"
BLUE="\033[0;34m"
YELLOW="\033[1;33m"
RED="\033[0;31m"
NC="\033[0m"

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

show_help() {
    echo "使用方式: $0 [选项]"
    echo ""
    echo "选项:"
    echo "  (无参数)      默认模式：按需编译并启动 Tmux 田字格分屏会话 (3 个微服务 + 1 个交互客户端)"
    echo "  --build, -b   强制重新编译 4 个可执行二进制产物后启动"
    echo "  --stop, -s    停止运行中的 Tmux 会话并彻底清理释放端口 (9001, 9002, 9003)"
    echo "  --no-client   仅启动 3 个微服务，左上格保留终端交互环境而不自动运行 client"
    echo "  --help, -h    显示帮助信息"
    echo ""
}

# 模式开关
FORCE_BUILD=false
DO_STOP=false
LAUNCH_CLIENT=true

while [[ $# -gt 0 ]]; do
    case "$1" in
        --build|-b)
            FORCE_BUILD=true
            shift
            ;;
        --stop|-s)
            DO_STOP=true
            shift
            ;;
        --no-client)
            LAUNCH_CLIENT=false
            shift
            ;;
        --help|-h)
            show_help
            exit 0
            ;;
        *)
            log_error "未知参数: $1"
            show_help
            exit 1
            ;;
    esac
done

cleanup_ports() {
    log_info "检查并释放占用端口 (9001, 9002, 9003)..."
    for port in 9001 9002 9003; do
        if command -v fuser &>/dev/null; then
            fuser -k -n tcp "${port}" 2>/dev/null || true
        elif command -v lsof &>/dev/null; then
            lsof -ti tcp:"${port}" | xargs -r kill -9 2>/dev/null || true
        fi
    done
    rm -f "/tmp/demo-cros-cluster.pid" 2>/dev/null || true
}

# 若指定停止模式
if [ "$DO_STOP" = true ]; then
    log_info "正在停止 Tmux 会话 '$SESSION_NAME' 与相关进程..."
    if tmux has-session -t "$SESSION_NAME" 2>/dev/null; then
        tmux kill-session -t "$SESSION_NAME" 2>/dev/null || true
    fi
    cleanup_ports
    log_success "Demo 集群与会话已全部停止并释放端口。"
    exit 0
fi

# 前置依赖检查
REQUIRED_CMDS=("tmux" "go" "make")
for cmd in "${REQUIRED_CMDS[@]}"; do
    if ! command -v "$cmd" &>/dev/null; then
        log_error "未检测到必要依赖工具: $cmd，请先安装后再运行本脚本。"
        exit 1
    fi
done

# ==============================================================================
# 第一步: 二进制编译 (支持按需编译与强制重新编译)
# ==============================================================================
BIN_TARGETS=("$BIN_DIR/backend-account" "$BIN_DIR/backend-inventory" "$BIN_DIR/sut-order" "$BIN_DIR/client")
NEED_BUILD=false

if [ "$FORCE_BUILD" = true ]; then
    NEED_BUILD=true
else
    for bin_path in "${BIN_TARGETS[@]}"; do
        if [ ! -f "$bin_path" ]; then
            NEED_BUILD=true
            break
        fi
    done
fi

if [ "$NEED_BUILD" = true ]; then
    log_info "正在编译 demo 4 进程二进制组件 (make -C $DEMO_DIR build)..."
    make -C "$DEMO_DIR" build
    log_success "二进制产物编译完成！"
else
    log_info "检测到现有二进制产物完备，跳过编译步骤（可使用 -b 强制重新编译）。"
fi

# ==============================================================================
# 第二步: 配置与启动 Tmux 田字格分屏会话
# ==============================================================================
log_info "正在配置 Tmux 田字格分屏会话: $SESSION_NAME ..."

# 若已存在同名会话，先清理旧会话
if tmux has-session -t "$SESSION_NAME" 2>/dev/null; then
    log_warn "检测到已存在的 Tmux 会话 '$SESSION_NAME'，正在终止重建..."
    tmux kill-session -t "$SESSION_NAME" 2>/dev/null || true
    wait_count=0
    while tmux has-session -t "$SESSION_NAME" 2>/dev/null && [ "$wait_count" -lt 20 ]; do
        sleep 0.05
        wait_count=$((wait_count + 1))
    done
    sleep 0.1
fi

# 清理残留端口
cleanup_ports

# 1. 创建会话，初始窗格作为【左上格】: client
PANE_TL=""
for retry in {1..5}; do
    if PANE_TL=$(tmux new-session -d -P -F '#{pane_id}' -s "$SESSION_NAME" -n "demo-cluster" -c "$DEMO_DIR" 2>/dev/null); then
        break
    fi
    sleep 0.15
done

if [ -z "$PANE_TL" ]; then
    log_error "创建 Tmux 会话 '$SESSION_NAME' 失败！"
    tmux new-session -d -P -F '#{pane_id}' -s "$SESSION_NAME" -n "demo-cluster" -c "$DEMO_DIR"
    exit 1
fi

# 2. 从左上格向下垂直切分，创建【左下格】: backend-account
PANE_BL=$(tmux split-window -v -P -F '#{pane_id}' -t "$PANE_TL" -c "$DEMO_DIR")

# 3. 从左上格向右水平切分，创建【右上格】: sut-order
PANE_TR=$(tmux split-window -h -P -F '#{pane_id}' -t "$PANE_TL" -c "$DEMO_DIR")

# 4. 从左下格向右水平切分，创建【右下格】: backend-inventory
PANE_BR=$(tmux split-window -h -P -F '#{pane_id}' -t "$PANE_BL" -c "$DEMO_DIR")

# 5. 格式化为标准田字格并开启鼠标与标题配置
tmux select-layout -t "$SESSION_NAME" tiled
tmux set-option -t "$SESSION_NAME" mouse on
tmux set-option -t "$SESSION_NAME" pane-border-status top
tmux set-option -t "$SESSION_NAME" pane-border-format " [ #{pane_title} ] "

tmux select-pane -t "$PANE_TL" -T "左上: client (TUI 交互控制台)"
tmux select-pane -t "$PANE_TR" -T "右上: sut-order (订单核心网关 :9001)"
tmux select-pane -t "$PANE_BL" -T "左下: backend-account (账户资产服务 :9002)"
tmux select-pane -t "$PANE_BR" -T "右下: backend-inventory (仓储库存服务 :9003)"

# ==============================================================================
# 第三步: 向各窗格分发启动指令
# ==============================================================================
log_info "正在向各窗格分发启动指令..."

# 1. 启动左下格: 资产服务 (:9002)
tmux send-keys -t "$PANE_BL" "./bin/backend-account --addr=127.0.0.1:9002" C-m

# 2. 启动右下格: 库存服务 (:9003)
tmux send-keys -t "$PANE_BR" "./bin/backend-inventory --addr=127.0.0.1:9003" C-m

# 3. 启动右上格: 订单网关核心 (:9001) - 稍候 0.2 秒确保下游端口已监听
tmux send-keys -t "$PANE_TR" "sleep 0.2 && ./bin/sut-order --addr=127.0.0.1:9001 --account-addr=127.0.0.1:9002 --inventory-addr=127.0.0.1:9003" C-m

# 4. 启动左上格: 交互客户端 (如果开启)
if [ "$LAUNCH_CLIENT" = true ]; then
    tmux send-keys -t "$PANE_TL" "sleep 0.6 && ./bin/client --addr=127.0.0.1:9001" C-m
fi

# 将焦点定位在左上格 (client 交互终端)
tmux select-pane -t "$PANE_TL"

log_success "Demo 集群田字格 Tmux 会话配置并启动完成！"

# ==============================================================================
# 第四步: 连接进入 Tmux 会话
# ==============================================================================
if [ -n "${TMUX:-}" ]; then
    tmux switch-client -t "$SESSION_NAME"
else
    # 检查是否处于具备完整交互能力的终端 (避免在非交互脚本或 dumb 终端中报错)
    if [ -t 0 ] && [ -t 1 ] && [ "${TERM:-}" != "dumb" ] && [ -n "${TERM:-}" ]; then
        tmux attach-session -t "$SESSION_NAME"
    else
        log_info "Tmux 会话已在后台就绪，可通过 'tmux attach -t $SESSION_NAME' 接入交互终端。"
    fi
fi
