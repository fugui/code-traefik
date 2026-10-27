BINARY          := code-traefik-server
FRONTEND_DIR    := frontend
DIST_DIR        := $(FRONTEND_DIR)/dist
NODE_MODULES    := $(FRONTEND_DIR)/node_modules

VERSION         ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT          ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILDTIME       ?= $(shell date -u '+%Y-%m-%d %H:%M:%S')
LDFLAGS         := -X 'main.Version=$(VERSION)' -X 'main.CommitID=$(COMMIT)' -X 'main.BuildTime=$(BUILDTIME)'

COMMON_DIR      := ../code-common/frontend/src
COMMON_SRCS     := $(shell find $(COMMON_DIR) -type f 2>/dev/null)

# 自动收集前端与后端源码依赖
FRONTEND_SRCS   := $(shell find $(FRONTEND_DIR) -type f -not -path "*/node_modules/*" -not -path "*/dist/*" 2>/dev/null) $(COMMON_SRCS)
BACKEND_SRCS    := $(shell find . -type f \( -name "*.go" -o -name "go.mod" -o -name "go.sum" \) -not -path "*/$(FRONTEND_DIR)/*" -not -path "*/.git/*")

.PHONY: all build install frontend backend clean run test lint

# 默认运行目标
all: build

# 完整打包构建（前端 dist + 内嵌单二进制）
build: $(BINARY)

# 安装前端依赖
install: $(NODE_MODULES)

$(NODE_MODULES): $(FRONTEND_DIR)/package.json
	cd $(FRONTEND_DIR) && ( [ -d node_modules ] || npm install --no-package-lock )
	@touch $(NODE_MODULES)

# 编译构建前端静态资产 (frontend/dist/)
frontend: $(DIST_DIR)

$(DIST_DIR): $(NODE_MODULES) $(FRONTEND_SRCS)
	cd $(FRONTEND_DIR) && npm run build
	@touch $(DIST_DIR)

# 编译后端可执行文件
backend: $(BINARY)

$(BINARY): $(BACKEND_SRCS) $(DIST_DIR)
	go mod tidy
	go build -ldflags "$(LDFLAGS)" -o $(BINARY)

# 清理构建产物
clean:
	rm -rf $(DIST_DIR) $(BINARY)

# 运行 Go 单元测试（支持在未构建 frontend/dist 前独立执行，自动过滤 node_modules 下的无关文件）
test:
	@clean_dist=0; \
	if [ ! -d "$(DIST_DIR)" ]; then \
		mkdir -p "$(DIST_DIR)" && touch "$(DIST_DIR)/.embed_placeholder"; \
		clean_dist=1; \
	fi; \
	cleanup() { if [ "$$clean_dist" = "1" ]; then rm -rf "$(DIST_DIR)"; fi; }; \
	trap cleanup EXIT; \
	pkgs=$$(go list ./... | grep -v '/$(FRONTEND_DIR)/'); \
	go fmt $$pkgs && go vet $$pkgs && go test -v $$pkgs

# 快捷启动命令
run: build
	./$(BINARY)

# 执行前端静态代码检查与类型校验
lint: $(NODE_MODULES)
	cd $(FRONTEND_DIR) && npm run lint
