export PATH := $(HOME)/go/bin:$(PATH)

BIN_DIR := bin

.PHONY: all build test clean start stop run-client proto

all: build

# 重新生成 Protobuf 代码
proto:
	@echo "Compiling Protobuf definitions..."
	cd .. && protoc --go_out=. --go_opt=module=code-traefik demo/proto/order.proto demo/proto/account.proto demo/proto/inventory.proto

# 编译四个进程的二进制产物
build: proto
	@echo "Building all 4 CROS RPC binaries into $(BIN_DIR)..."
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/backend-account ./cmd/backend-account
	go build -o $(BIN_DIR)/backend-inventory ./cmd/backend-inventory
	go build -o $(BIN_DIR)/sut-order ./cmd/sut-order
	go build -o $(BIN_DIR)/client ./cmd/client
	@echo "Build complete."

# 运行单元测试与集成测试
test:
	@echo "Running all demo unit and integration tests..."
	go test -v ./...

# 一键拉起后台服务集群
start: build
	@./scripts/start_all.sh

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
