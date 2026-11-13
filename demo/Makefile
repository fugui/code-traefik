export PATH := $(HOME)/go/bin:$(PATH)

BIN_DIR := bin

.PHONY: all build test clean start stop run-client proto tmux

all: build

# 重新生成 Protobuf 代码与二进制描述集
proto:
	@echo "Compiling Protobuf definitions and descriptor set..."
	cd .. && protoc --go_out=. --go_opt=module=code-traefik \
		--descriptor_set_out=demo/demo.pb --include_imports \
		demo/proto/order.proto demo/proto/account.proto demo/proto/inventory.proto demo/proto/payment.proto

# 编译微服务组件与 Agent 二进制产物
build: proto
	@echo "Building all CROS RPC binaries and Agent into $(BIN_DIR)..."
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/backend-account ./cmd/backend-account
	go build -o $(BIN_DIR)/backend-inventory ./cmd/backend-inventory
	go build -o $(BIN_DIR)/backend-payment ./cmd/backend-payment
	go build -o $(BIN_DIR)/sut-order ./cmd/sut-order
	go build -o $(BIN_DIR)/client ./cmd/client
	$(MAKE) -C .. agent
	cp --remove-destination ../code-traefik-agent $(BIN_DIR)/code-traefik-agent
	@echo "Build complete."

# 运行单元测试与集成测试
test:
	@echo "Running all demo unit and integration tests..."
	go test -v ./...

# 一键拉起后台服务集群 (纯后台静默运行)
start: build
	@./scripts/start_all.sh

# 一键启动 Tmux 田字格可视化集群 (推荐：4 进程 2x2 分屏实时监控与交互)
tmux:
	@./start.sh

# 一键停止服务集群
stop:
	@./scripts/stop_all.sh

# 启动 TUI 交互式客户端
run-client:
	@./bin/client --addr="127.0.0.1:9001"

# 清理构建产物
clean:
	@echo "Cleaning up $(BIN_DIR)..."
	@rm -rf $(BIN_DIR)
