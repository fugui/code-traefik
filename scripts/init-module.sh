#!/usr/bin/env bash
# ==============================================================================
# 脚本名称: scripts/init-module.sh
# 功能描述: 将 code-template 一键初始化派生为新的 CodeBench 微前端业务模块
# ==============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEMPLATE_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

GREEN="\033[0;32m"
BLUE="\033[0;34m"
YELLOW="\033[1;33m"
RED="\033[0;31m"
NC="\033[0m"

log_info()    { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[OK]${NC} $1"; }
log_warn()    { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error()   { echo -e "${RED}[ERROR]${NC} $1" >&2; }

show_help() {
  cat <<EOF
用法:
  bash scripts/init-module.sh [选项]

选项:
  --key <module_key>         模块唯一英文标识（必填，如 monitor、asset_mgr；仅限小写字母开头 + 小写字母/数字/下划线）
  --title <模块中文名称>      门户侧边栏与页面显示标题（默认根据 key 生成）
  --target-dir <目标目录>    新模块工程输出目录（默认: 在 code-template 同级目录生成 code-<key>）
  --in-place                 在当前目录就地替换（不推荐在模板仓库使用）
  --port <后端端口>           Go 后端监听端口，范围 1024-65535（默认: 8091）
  --dev-port <前端调试端口>   Vite 独立开发服务器端口，范围 1024-65535（默认: 5181）
  --table-prefix <表前缀>     共享 PostgreSQL 业务表名前缀，以 _ 结尾（默认: <key>_）
  --icon <Lucide图标名>       CodeBench 门户卡片与菜单图标（默认: Activity）
  --dry-run                  仅预览将要执行的变更，不实际修改任何文件
  --help, -h                 查看帮助说明

示例:
  # 在父级 codes 目录下直接派生新模块 code-monitor
  code-template/scripts/init-module.sh --key monitor --title "集群监控诊断" --port 8091 --dev-port 5181 --table-prefix mon_
EOF
}

MODULE_KEY=""
MODULE_TITLE=""
TARGET_DIR=""
IN_PLACE="false"
BACKEND_PORT="8091"
FRONTEND_PORT="5181"
TABLE_PREFIX=""
MODULE_ICON="Activity"
DRY_RUN="false"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --key)
      MODULE_KEY="${2:-}"
      shift 2
      ;;
    --title)
      MODULE_TITLE="${2:-}"
      shift 2
      ;;
    --target-dir)
      TARGET_DIR="${2:-}"
      shift 2
      ;;
    --in-place)
      IN_PLACE="true"
      shift
      ;;
    --port)
      BACKEND_PORT="${2:-}"
      shift 2
      ;;
    --dev-port)
      FRONTEND_PORT="${2:-}"
      shift 2
      ;;
    --table-prefix)
      TABLE_PREFIX="${2:-}"
      shift 2
      ;;
    --icon)
      MODULE_ICON="${2:-}"
      shift 2
      ;;
    --dry-run)
      DRY_RUN="true"
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

# 若未通过命令行传入 --key，进入交互式引导模式
if [[ -z "$MODULE_KEY" ]]; then
  if [[ -t 0 ]]; then
    echo "=================================================================="
    echo "        CodeBench 新业务微前端模块初始化向导 (code-template)       "
    echo "=================================================================="
    read -r -p "1. 请输入模块唯一标识 (小写字母开头，如 monitor): " MODULE_KEY
    read -r -p "2. 请输入模块中文名称 [默认: 新业务模块]: " INPUT_TITLE
    [[ -n "$INPUT_TITLE" ]] && MODULE_TITLE="$INPUT_TITLE"
    read -r -p "3. 请输入后端监听端口 [默认: 8091]: " INPUT_PORT
    [[ -n "$INPUT_PORT" ]] && BACKEND_PORT="$INPUT_PORT"
    read -r -p "4. 请输入前端 Vite 调试端口 [默认: 5181]: " INPUT_DEV_PORT
    [[ -n "$INPUT_DEV_PORT" ]] && FRONTEND_PORT="$INPUT_DEV_PORT"
    read -r -p "5. 请输入数据库表前缀 [默认: ${MODULE_KEY}_]: " INPUT_PREFIX
    [[ -n "$INPUT_PREFIX" ]] && TABLE_PREFIX="$INPUT_PREFIX"
    if [[ -z "$TARGET_DIR" && "$IN_PLACE" != "true" ]]; then
      DEFAULT_DERIVED_DIR="$(cd "$TEMPLATE_ROOT/.." && pwd)/code-${MODULE_KEY}"
      read -r -p "6. 请输入新模块输出目录 [默认: $DEFAULT_DERIVED_DIR]: " INPUT_DIR
      [[ -n "$INPUT_DIR" ]] && TARGET_DIR="$INPUT_DIR"
    fi
  else
    log_error "非交互式终端必须通过 --key 指定模块标识！"
    show_help
    exit 1
  fi
fi

# 1. 严格校验 MODULE_KEY 格式
if [[ ! "$MODULE_KEY" =~ ^[a-z][a-z0-9_]{1,23}$ ]]; then
  log_error "模块标识 '$MODULE_KEY' 不合法！必须以小写字母开头，仅含小写字母、数字或下划线，长度 2-24 位。"
  exit 1
fi

# 2. 检查平台保留路由与核心模块命名冲突
RESERVED_KEYS=(
  "template" "bench" "common" "shield" "pipeline" "pdm" "gate" "proto" "notifier"
  "api" "v1" "admin" "docs" "feedback" "oauth2" "uploads" "login" "assets" "static" "public" "modelgate"
)
for reserved in "${RESERVED_KEYS[@]}"; do
  if [[ "$MODULE_KEY" == "$reserved" ]]; then
    log_error "模块标识 '$MODULE_KEY' 为平台保留关键字或已存在的核心模块，请更换其他名称！"
    exit 1
  fi
done

# 3. 校验端口合法性
validate_port() {
  local p="$1"
  local label="$2"
  if [[ ! "$p" =~ ^[0-9]+$ ]] || (( p < 1024 || p > 65535 )); then
    log_error "${label} '$p' 不合法！请输入 1024-65535 之间的整数端口。"
    exit 1
  fi
}
validate_port "$BACKEND_PORT" "后端端口"
validate_port "$FRONTEND_PORT" "前端调试端口"

if [[ "$BACKEND_PORT" == "$FRONTEND_PORT" ]]; then
  log_error "后端端口与前端调试端口不能相同 ($BACKEND_PORT)！"
  exit 1
fi

# 4. 补全默认表名前缀与模块中文标题
if [[ -z "$TABLE_PREFIX" ]]; then
  TABLE_PREFIX="${MODULE_KEY}_"
fi
if [[ "$TABLE_PREFIX" != *_ ]]; then
  TABLE_PREFIX="${TABLE_PREFIX}_"
fi
if [[ ! "$TABLE_PREFIX" =~ ^[a-z][a-z0-9_]{1,19}_$ ]]; then
  log_error "数据库表前缀 '$TABLE_PREFIX' 不合法！必须以小写字母开头并以 '_' 结尾（例如 mon_）。"
  exit 1
fi

if [[ -z "$MODULE_TITLE" ]]; then
  MODULE_TITLE="新业务模块 (${MODULE_KEY})"
fi

# 5. 计算目标输出目录
TEMPLATE_NAME="$(basename "$TEMPLATE_ROOT")"
IS_TEMPLATE_DIR="false"
if [[ "$TEMPLATE_NAME" == "code-template" ]]; then
  IS_TEMPLATE_DIR="true"
fi

if [[ -z "$TARGET_DIR" ]]; then
  if [[ "$IS_TEMPLATE_DIR" == "true" && "$IN_PLACE" != "true" ]]; then
    TARGET_DIR="$(cd "$TEMPLATE_ROOT/.." && pwd)/code-${MODULE_KEY}"
  else
    TARGET_DIR="$TEMPLATE_ROOT"
  fi
fi

# 规范化 TARGET_DIR 路径
TARGET_DIR="$(python3 -c "import os, sys; print(os.path.abspath(sys.argv[1]))" "$TARGET_DIR" 2>/dev/null || readlink -f "$TARGET_DIR" 2>/dev/null || echo "$TARGET_DIR")"

if ! command -v python3 &>/dev/null; then
  log_error "未检测到 python3，请先安装 python3 以执行安全精准替换。"
  exit 1
fi

# 检查模板根目录状态
if ! grep -q "module code-template" "$TEMPLATE_ROOT/go.mod" 2>/dev/null; then
  log_error "模板根目录 $TEMPLATE_ROOT 的 go.mod 已不是初始 code-template 状态，为防止重复替换破坏源码，脚本已中止。"
  exit 1
fi

# 检查目标目录冲突
if [[ "$TARGET_DIR" != "$TEMPLATE_ROOT" ]]; then
  if [[ -d "$TARGET_DIR" && -n "$(ls -A "$TARGET_DIR" 2>/dev/null)" ]]; then
    log_error "目标目录 '$TARGET_DIR' 已存在且非空！请指定其他目录或先清理该目录。"
    exit 1
  fi
fi

log_info "即将按以下参数初始化新模块："
echo "  - 模块标识 (Key)      : $MODULE_KEY"
echo "  - 模块名称 (Title)    : $MODULE_TITLE"
echo "  - 目标工程目录        : $TARGET_DIR"
echo "  - Go 模块名           : code-$MODULE_KEY"
echo "  - 管理员角色          : ${MODULE_KEY}_admin"
echo "  - 数据库表名前缀      : $TABLE_PREFIX"
echo "  - 后端监听端口        : :$BACKEND_PORT"
echo "  - 前端开发端口        : $FRONTEND_PORT"

if [[ "$DRY_RUN" == "true" ]]; then
  log_warn "当前为 --dry-run 预览模式，未修改任何文件。"
  exit 0
fi

# 若目标目录不等于模板根目录，执行干净拷贝（排除模板自身的 docs 教程，后续将为新工程生成专属 SDD+TDD docs 体系）
if [[ "$TARGET_DIR" != "$TEMPLATE_ROOT" ]]; then
  log_info "正在将模板源码干净复制到目标目录: $TARGET_DIR ..."
  mkdir -p "$TARGET_DIR"
  if command -v rsync &>/dev/null; then
    rsync -a \
      --exclude='.git' \
      --exclude='docs' \
      --exclude='node_modules' \
      --exclude='dist' \
      --exclude='frontend/node_modules' \
      --exclude='frontend/dist' \
      --exclude='code-template-server' \
      --exclude='*-server' \
      --exclude='config.yaml' \
      --exclude='*.log' \
      --exclude='*.db' \
      --exclude='*.sqlite' \
      --exclude='.DS_Store' \
      --exclude='.idea' \
      --exclude='coverage.out' \
      "$TEMPLATE_ROOT/" "$TARGET_DIR/"
  else
    cp -r "$TEMPLATE_ROOT/." "$TARGET_DIR/"
    rm -rf "$TARGET_DIR/.git" "$TARGET_DIR/docs" "$TARGET_DIR/frontend/node_modules" "$TARGET_DIR/frontend/dist" \
           "$TARGET_DIR/code-template-server" "$TARGET_DIR"/*-server "$TARGET_DIR/config.yaml" 2>/dev/null || true
  fi

  # 为新模块目录初始化独立干净的 Git 仓库（若尚未初始化）
  if [[ ! -d "$TARGET_DIR/.git" ]]; then
    (cd "$TARGET_DIR" && git init -b main 2>/dev/null || git init) &>/dev/null || true
  fi
fi

PROJECT_ROOT="$TARGET_DIR"
export PROJECT_ROOT MODULE_KEY MODULE_TITLE BACKEND_PORT FRONTEND_PORT TABLE_PREFIX MODULE_ICON

python3 - <<'PYEOF'
import os
import shutil
from datetime import date
from pathlib import Path

root = Path(os.environ["PROJECT_ROOT"])
key = os.environ["MODULE_KEY"]
title = os.environ["MODULE_TITLE"]
port = os.environ["BACKEND_PORT"]
dev_port = os.environ["FRONTEND_PORT"]
table_prefix = os.environ["TABLE_PREFIX"]
icon = os.environ.get("MODULE_ICON", "Activity")
today = date.today().isoformat()

pascal_key = "".join(part.capitalize() for part in key.split("_"))
camel_Export = "".join(part.capitalize() if idx > 0 else part for idx, part in enumerate(key.split("_")))
service_name = f"Code-{pascal_key}"
admin_role = f"{key}_admin"

def patch_file(rel_path: str, replacements: list[tuple[str, str]]) -> None:
    p = root / rel_path
    if not p.exists():
        return
    content = p.read_text(encoding="utf-8")
    for old, new in replacements:
        content = content.replace(old, new)
    p.write_text(content, encoding="utf-8")

def write_doc(rel_path: str, content: str) -> None:
    p = root / rel_path
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(content.lstrip("\n"), encoding="utf-8")

# 1. 构建与后端配置替换
patch_file("Makefile", [
    ("BINARY          := code-template-server", f"BINARY          := code-{key}-server"),
])

patch_file("go.mod", [
    ("module code-template", f"module code-{key}"),
])

patch_file("config/config.go", [
    ('ModuleKey   = "template"', f'ModuleKey   = "{key}"'),
    ('ServiceName = "Code-Template"', f'ServiceName = "{service_name}"'),
    ('AdminRole   = "template_admin"', f'AdminRole   = "{admin_role}"'),
    ('TablePrefix = "tpl_"', f'TablePrefix = "{table_prefix}"'),
    ('DefaultPort = ":8090"', f'DefaultPort = ":{port}"'),
])

patch_file("config.yaml.example", [
    ("Code-Template", service_name),
    ('port: ":8090"', f'port: ":{port}"'),
    ('external_url: "http://127.0.0.1:8090"', f'external_url: "http://127.0.0.1:{port}"'),
    ('client_id: "code-template"', f'client_id: "code-{key}"'),
    ("tpl_", table_prefix),
])

# 替换所有 Go 源码中的内部包导入路径
for go_file in root.rglob("*.go"):
    if "frontend" in go_file.parts or ".git" in go_file.parts:
        continue
    text = go_file.read_text(encoding="utf-8")
    if '"code-template/' in text:
        go_file.write_text(text.replace('"code-template/', f'"code-{key}/'), encoding="utf-8")

# 2. 前端模块联邦与常量替换
patch_file("frontend/package.json", [
    ('"name": "code-template-frontend"', f'"name": "code-{key}-frontend"'),
    ('"dev": "vite --port 5180"', f'"dev": "vite --port {dev_port}"'),
])

patch_file("frontend/index.html", [
    ("Code-Template 独立开发调试控制台", f"{title} ({service_name})"),
])

patch_file("frontend/vite.config.ts", [
    ("appName: 'code-template'", f"appName: 'code-{key}'"),
    ("name: 'template'", f"name: '{key}'"),
    ("base: process.env.VITE_BASE_PATH || '/template/'", f"base: process.env.VITE_BASE_PATH || '/{key}/'"),
    ("port: 5180", f"port: {dev_port}"),
    ("'/template/api'", f"'/{key}/api'"),
    ("http://127.0.0.1:8090", f"http://127.0.0.1:{port}"),
])

patch_file("frontend/src/constants.ts", [
    ("export const MODULE_KEY = 'template';", f"export const MODULE_KEY = '{key}';"),
    ("export const MODULE_TITLE = '示例模板模块';", f"export const MODULE_TITLE = '{title}';"),
    ("export const ADMIN_ROLE = 'template_admin';", f"export const ADMIN_ROLE = '{admin_role}';"),
])

patch_file("frontend/src/menu.ts", [
    ("templateMenuConfig", f"{camel_Export}MenuConfig"),
])

# 3. 清理旧 docs（若为就地替换）并生成 SDD + TDD 标准文档体系与 AGENTS.md / README.md
docs_dir = root / "docs"
if docs_dir.exists():
    shutil.rmtree(docs_dir)

write_doc("AGENTS.md", f"""
# AGENTS.md - {title} ({service_name})

> 💡 **上位准则**：工作区通用 SDD+TDD 四阶段研发流水线、`docs/` 双位编号规范、Go/React 编码风格、双模主题 Token、通用分页及自动交付规章，统一遵循 [`../AGENTS.md`](../AGENTS.md)（`code-common/rules/AGENTS.md`），本文档仅定义本模块专属契约与边界。

---

## 1. 模块专属契约 (Module Contract)

| 契约项 | 规范值 | 强约束说明 |
| :--- | :--- | :--- |
| **模块标识 (`ModuleKey`)** | `{key}` | 门户路由前缀 `/{key}`，Vite Federation 名称 `name: '{key}'` |
| **服务与二进制名** | `{service_name}` / `code-{key}-server` | Go Module: `code-{key}` |
| **端口分配** | 后端 `:{port}` / 前端 `{dev_port}` | 验证完毕后必须主动关闭进程释放端口 |
| **数据库表前缀 (`TablePrefix`)** | `{table_prefix}` | 本模块所有 GORM 模型 `TableName()` 必须以 `{table_prefix}` 开头 |
| **管理员角色 (`AdminRole`)** | `{admin_role}` | 管理接口必须挂载 `commonAuth.RequireAdmin("{admin_role}")` |

---

## 2. 目录结构与业务边界

```text
code-{key}/
├── docs/                # SDD + TDD 规范文档库 (00-prompt ~ 06-ops)
├── config/              # 配置加载与模块常量定义
├── models/              # GORM 数据模型（强制 {table_prefix} 表前缀隔离）
├── handlers/            # HTTP 控制器、RBAC 守卫、审计 Diff 与 *_test.go 隔离单测
└── frontend/            # React 18 微前端工程（暴露 ./App 与 ./menu）
```

* **本模块专有架构红线**：*(随阶段二 `docs/02-design/` 演进在此补充领域防腐约束)*

---

## 3. 常用验证命令

```bash
make test                                                                                      # 后端 fmt + vet + 单元测试
TEST_DB_DSN="host=127.0.0.1 port=5432 user=postgres password=PWD dbname=postgres sslmode=disable" make test  # 物理隔离库全量单测
make lint                                                                                      # 前端 tsc + eslint 检查
make build && make clean                                                                       # 单二进制打包验证并清理产物
```
""")

write_doc("README.md", f"""
# {title} (`code-{key}`)

> 基于 CodeBench 微前端架构与 `code-common` 公共底座构建的 **{title}** 业务子系统。

本项目严格遵循 **SDD (Spec-Driven Development，规范驱动开发) + TDD (Test-Driven Development，测试驱动开发)** 工程模式：
**「特性边界明确 (`01-features`) -> 架构与模型定稿 (`02-design`) -> 接口契约与任务拆解 (`03-api` & `04-tasks`) -> TDD 测试先行与分阶段交付」**

---

## 1. 系统架构与微前端拓扑 (System Topology)

```mermaid
flowchart LR
    subgraph Host["CodeBench 宿主门户 (:8000)"]
        PortalUI["门户壳容器<br/>动态加载 /{key}/assets/remoteEntry.js"]
        Gateway["透明反向代理网关<br/>/{key}/api/* -> 127.0.0.1:{port}/api/*"]
    end

    subgraph SubModule["{title} (code-{key} :{port})"]
        FE["React 18 微前端视图<br/>(./App & ./menu 双模容器)"]
        BE["Go Gin 后端服务<br/>(server.Run + JWT/RBAC + Audit Diff)"]
    end

    subgraph Storage["共享基础设施 (PostgreSQL)"]
        SharedTbl[("公共表: users / departments / audit_logs")]
        ModTbl[("模块专属表: {table_prefix}*")]
    end

    PortalUI -- "Module Federation" --> FE
    FE -- "HTTP Fetch (Bearer JWT)" --> Gateway
    Gateway -- "StripPrefix /{key}" --> BE
    BE --> SharedTbl
    BE --> ModTbl
```

---

## 2. 目录结构 (Repository Layout)

```text
code-{key}/
├── docs/                # SDD + TDD 规范文档库 (00-prompt ~ 06-ops)
├── config/              # 配置加载与模块级元数据常量 (ModuleKey="{key}", TablePrefix="{table_prefix}")
├── models/              # GORM 数据模型（统一使用 {table_prefix} 表前缀隔离）
├── handlers/            # HTTP 控制器、RBAC 权限拦截、字段级审计与物理隔离单元测试
├── frontend/            # React 18 + Vite 5 + Module Federation 前端工程
├── scripts/             # 自动化构建与辅助脚本
├── Makefile             # 标准构建、测试与代码检查入口
├── AGENTS.md            # AI Agent 协作准则与架构红线
└── README.md            # 项目概览与 SDD+TDD 路线图
```

完整文档规范与分层索引请查阅 [`docs/README.md`](./docs/README.md)。

---

## 3. SDD + TDD 研发路线图 (Lifecycle Checklist)

- [ ] **阶段一：特性清单与边界明确** (`docs/01-features/`)
  - [ ] [`01-feature-matrix.md`](./docs/01-features/01-feature-matrix.md)（业务愿景、`P0~P2` 功能矩阵、验收场景与 `Non-Goals`）
- [ ] **阶段二：系统架构与模型设计** (`docs/02-design/`)
  - [ ] [`01-system-architecture.md`](./docs/02-design/01-system-architecture.md)（双模运行架构、`{table_prefix}*` 数据表结构设计与前后端分层）
- [ ] **阶段三：接口契约与 TDD 任务分解** (`docs/03-api/`, `docs/04-tasks/`)
  - [ ] [`01-http-api-contract.md`](./docs/03-api/01-http-api-contract.md)（RESTful API 契约、RBAC 权限矩阵与审计规范）
  - [ ] [`01-roadmap.md`](./docs/04-tasks/01-roadmap.md)（Milestone 1~4 依赖拓扑与 TDD 质量门禁）
  - [ ] [`02-m1-domain-model-and-api.md`](./docs/04-tasks/02-m1-domain-model-and-api.md)（Milestone 1 作战卡与 TDD 先行用例清单）
- [ ] **阶段四：工程实施、架构决策与运维验证** (`docs/05-adr/`, `docs/06-ops/`)
  - [ ] [`01-microfrontend-and-table-prefix-isolation.md`](./docs/05-adr/01-microfrontend-and-table-prefix-isolation.md)（ADR-01 架构决策记录）
  - [ ] [`01-verification-and-deployment-guide.md`](./docs/06-ops/01-verification-and-deployment-guide.md)（联调测试与生产部署手册）

---

## 4. 快速开始与常用命令

```bash
# 1. 生成本地配置文件（确保 database 与 auth.jwt_secret 与 code-bench 保持一致）
cp config.yaml.example config.yaml

# 2. 安装前端依赖、执行静态检查、运行单测并打包单二进制
make install && make lint && make test && make build

# 3. 启动服务（默认监听 :{port}）
./code-{key}-server
```
""")

write_doc("docs/README.md", f"""
# {title} (`code-{key}`) 文档知识库 (Docs Hub)

本项目严格采用 **SDD (Spec-Driven Development，规范驱动开发) + TDD (Test-Driven Development，测试驱动开发)** 模式，所有提示词资产、需求边界、系统架构、数据表模型、API 契约、TDD 任务拆解与运维验证手册均在此目录中进行统一版本化管理。

---

## 目录分层架构

```text
docs/
├── 00-prompt/       # 提示词库：记录各阶段 SDD/TDD Prompt 资产与上下文演进指令
│   ├── 01-init-features-prompt.md         # 阶段一：特性矩阵 (Feature Matrix) 与需求边界生成提示词
│   ├── 02-design-prompt.md                # 阶段二：系统架构、GORM 模型与微前端交互设计提示词
│   ├── 03-api-and-tasks-prompt.md         # 阶段三：API 契约冻结与 TDD Milestone 任务拆解提示词
│   └── 04-tdd-implementation-prompt.md    # 阶段四：TDD 红-绿-重构编码实施与全链路交付验证提示词
├── 01-features/     # 特性与需求边界：特性清单矩阵 (Feature Matrix)、P0~P2 优先级与 Non-Goals
│   └── 01-feature-matrix.md               # 阶段一：需求范围、核心特性矩阵与验收场景定义
├── 02-design/       # 系统技术架构 (SDD)：微前端双模拓扑、{table_prefix}* 数据表模型与核心业务流程
│   └── 01-system-architecture.md          # 【总览篇】系统总体架构、数据模型与前端组件分层设计
├── 03-api/          # 接口与数据契约：RESTful API 签名、请求/响应 Schema、RBAC 权限与审计规范
│   └── 01-http-api-contract.md            # 【API 契约】HTTP 接口字典、角色守卫 ({admin_role}) 与状态码规范
├── 04-tasks/        # TDD 实施规划与验收：Milestone 拆解、任务作战卡与自动化验收测试标准 (AC)
│   ├── 01-roadmap.md                      # 【工程路线图】Milestone 1~4 依赖拓扑图与 TDD 质量门禁
│   └── 02-m1-domain-model-and-api.md      # 【M1 作战卡】核心领域模型、API 与物理隔离单元测试
├── 05-adr/          # 架构决策记录 (ADR)：重大技术选型与权衡记录 (Architecture Decision Records)
│   └── 01-microfrontend-and-table-prefix-isolation.md # ADR-01：微前端集成、共享库表前缀隔离与 TDD 测试策略
└── 06-ops/          # 交付与验证：双模联调、物理隔离数据库测试与生产环境单二进制部署手册
    └── 01-verification-and-deployment-guide.md # 联调验证、自动化回归测试与生产部署操作指南
```

---

## 目录治理规则

1. **全链路双位编号递进**：
   - 一级子目录采用阶段双位数字前缀（`00-` ~ `06-`）；
   - 各阶段目录下的**所有 Markdown 文档必须带双位数字编号前缀**（如 `01-feature-matrix.md`、`01-system-architecture.md`、`01-http-api-contract.md`、`01-roadmap.md`），保证在文件树、IDE 及文档中心中保持严格时序与逻辑有序性。
2. **规范先行与测试驱动 (SDD + TDD)**：
   - 任何业务代码实施前，必须先在 `01-features/`、`02-design/`、`03-api/` 与 `04-tasks/` 中冻结设计契约与验收标准 (AC)；
   - 实施阶段遵循 **TDD（先写测试 -> 运行失败 Red -> 编写实现通过 Green -> 重构优化 Refactor）**，所有数据库单测必须通过 `testdb.SetupIsolatedDB` 实现物理隔离。
3. **决策可溯**：凡涉及关键架构选型、数据模型拆分、跨模块交互或性能权衡，须在 `05-adr/` 中以标准 ADR 模板记录背景与决策理由。
""")

write_doc("docs/00-prompt/01-init-features-prompt.md", f"""
# code-{key}: 阶段一特性清单与需求边界 (SDD) 提示词指令

## 角色定位与工程原则
你是一名资深 Go 全栈系统架构师与产品领域专家。我们将采用 **SDD (规范驱动开发) + TDD (测试驱动开发)** 的工程方法，在 CodeBench 微前端体系下开发 **code-{key}（{title}）** 业务子系统。

开发过程**严禁直接跳过设计堆砌业务代码**，必须严格遵循：
**「阶段一：特性与边界明确 (`01-features`) -> 阶段二：架构与模型定稿 (`02-design`) -> 阶段三：契约冻结与 TDD 任务拆解 (`03-api` & `04-tasks`) -> 阶段四：TDD 测试先行与分阶段实施」**。

---

## 1. 模块基础元数据契约
- **模块唯一标识 (`ModuleKey`)**：`{key}`（网关与前端路由前缀 `/{key}`）
- **模块中文名称**：`{title}`（服务名 `{service_name}`）
- **端口分配**：Go 后端监听 `:{port}`，前端 Vite 独立调试端口 `{dev_port}`
- **数据库隔离策略**：与 CodeBench 共享 PostgreSQL 实例（复用 `users`, `departments`, `audit_logs`），本模块所有业务表强制使用前缀 `{table_prefix}`
- **RBAC 权限角色**：模块管理员角色为 `{admin_role}`（`super_admin` 拥有同等最高权限）

---

## 2. 阶段一执行指令（生成或细化 `docs/01-features/01-feature-matrix.md`）
请结合具体业务诉求，在 `docs/01-features/01-feature-matrix.md` 中输出详尽、结构化、可直接指导后续架构设计与 TDD 验收的**特性清单矩阵与范围界定规范**：

1. **系统愿景与核心价值主张**：明确 `{title}` 解决的核心业务痛点与目标用户角色。
2. **结构化功能特性矩阵表格**：
   - 按核心业务子域划分（列包含：特性编号 `F-XX`、特性名称、优先级 `P0/P1/P2`、功能描述与技术规范、TDD 验收衡量标准）。
3. **核心业务验收场景矩阵 (Verification Scenarios)**：设计 `SC-01` ~ `SC-05` 端到端核心业务场景（含正常主链路、参数校验拦截、RBAC 越权拦截、并发/边界场景）。
4. **明确定义 Non-Goals（非目标边界）**：界定当前版本明确不做的功能，防止需求蔓延。
5. **非功能性与安全红线 (NFR & Guardrails)**：明确响应延迟、数据隔离、字段级审计与亮/暗双模主题适配要求。
6. **在本阶段定稿确认前，请勿修改或新增任何 `.go` / `.tsx` 业务代码。**
""")

write_doc("docs/00-prompt/02-design-prompt.md", f"""
# code-{key}: 阶段二系统架构与数据模型设计 (SDD) 提示词指令

## 角色定位与工程原则
在阶段一特性清单与边界界定规范（[`docs/01-features/01-feature-matrix.md`](../01-features/01-feature-matrix.md)）已定稿的基础上，现在正式进入：
**「阶段二：系统架构与数据模型设计文档编制」**。

---

## 任务目标与设计产出 (`docs/02-design/`)

请在 `docs/02-design/` 下编制或细化系统架构设计文档（默认主文档为 `01-system-architecture.md`，复杂业务可按双位编号拆分为多篇专篇）：

| 文档路径 | 规范类型 | 核心职责与设计边界 |
| :--- | :--- | :--- |
| `docs/02-design/01-system-architecture.md` | **【总览篇】系统总体架构与数据模型** | 微前端嵌入/独立双模拓扑、端到端请求序列图、GORM 实体关系图 (ER Diagram) 与 `{table_prefix}*` 表结构字典、前端页面与公共组件复用设计 |
| `docs/02-design/02-core-domain-engine.md` *(按需新增)* | **【业务篇】核心领域引擎与状态机** | 核心业务状态流转图、关键算法/规则编排、异步任务或外部系统交互容错设计 |

## 核心设计约束
1. **表名前缀强制隔离**：所有新增 GORM 模型必须显式定义 `TableName() string` 返回 `"{table_prefix}<name>"`，严禁污染公共表空间。
2. **公共能力最大化复用**：
   - 后端复用 `code-common/backend/auth`（JWT + `{admin_role}` 角色守卫）与 `code-common/backend/audit`（字段级 Diff 操作审计）；
   - 前端复用 `@code/common`（`Pagination` + `usePagination`、`Drawer`、`ConfirmProvider`、`ToastProvider`、`MemberSearchSelect`、`StatusTag`），样式 100% 采用 `--color-*` 语义 Token 与 `.code-{key}-*` Flat BEM 命名。
3. **红线提示**：本阶段只产出架构与模型设计文档，**暂不编写具体业务实现代码**。
""")

write_doc("docs/00-prompt/03-api-and-tasks-prompt.md", f"""
# code-{key}: 阶段三接口契约冻结与 TDD 任务拆解 (SDD + TDD) 提示词指令

## 角色定位与工程原则
在阶段一特性矩阵（[`docs/01-features/01-feature-matrix.md`](../01-features/01-feature-matrix.md)）与阶段二系统架构设计（[`docs/02-design/01-system-architecture.md`](../02-design/01-system-architecture.md)）定稿的基础上，现在正式进入：
**「阶段三：API 契约冻结与 TDD 里程碑任务分解」**。

---

## 任务目标与交付文档矩阵

| 文档路径 | 规范类型 | 核心职责与交付要求 |
| :--- | :--- | :--- |
| `docs/03-api/01-http-api-contract.md` | **【API 契约】** | 冻结所有 RESTful 接口路径、Query/Body 参数、响应 JSON Schema、HTTP 状态码、RBAC 角色权限要求（普通登录用户 vs `{admin_role}`）及审计动作标识 |
| `docs/04-tasks/01-roadmap.md` | **【工程路线图】** | 拆解正交递进的 Milestone 1 ~ N 依赖拓扑图（DAG）、各里程碑交付物清单与强制质量门禁 |
| `docs/04-tasks/02-m1-domain-model-and-api.md` | **【M1 作战卡】** | 明确 Milestone 1 的具体编码任务、**TDD 先行单元测试用例清单**（正常/边界/异常/越权）及自动化验收命令 (Acceptance Criteria) |

> **红线提示**：每个 Milestone 作战卡必须明确列出对应的 `*_test.go` 测试用例设计，确保阶段四能够严格按 **TDD（测试先行）** 模式推进。
""")

write_doc("docs/00-prompt/04-tdd-implementation-prompt.md", f"""
# code-{key}: 阶段四 TDD 工程实施与全链路交付验证提示词指令

## 角色定位与工程原则
在阶段一（特性矩阵）、阶段二（系统架构与数据模型）与阶段三（API 契约与 Milestone 作战卡）全部定稿冻结后，现在正式进入：
**「阶段四：TDD 测试驱动开发、全栈实施与自动化验证」**。

---

## TDD 实施循环与步骤 (Red -> Green -> Refactor)

请严格按照 `docs/04-tasks/01-roadmap.md` 定义的里程碑顺序推进，每个 Milestone 内部执行以下闭环：

1. **Step 1 - 编写先行测试 (Red)**：
   - 在 `handlers/*_test.go`（或领域包 `*_test.go`）中，基于 `testdb.SetupIsolatedDB(t)` 编写本里程碑契约要求的单元测试与 HTTP 接口测试（涵盖正常 CRUD、分页筛选、参数非法 `400`、未登录 `401`、非管理员越权 `403`）。
2. **Step 2 - 编写最小完备业务实现 (Green)**：
   - 落地 `models/`（确保表前缀为 `{table_prefix}`）、`handlers/`（显式处理所有 `error`，接入 `commonAudit` 审计）与 `main.go` 路由注册；
   - 落地 `frontend/src/` 页面与 API 对接（遵循 `--color-*` 亮暗双模主题 Token、Flat BEM 命名、URL 同步分页规范、零 `any` 类型）。
3. **Step 3 - 质量门禁验证与重构 (Refactor & Verify)**：
   - 运行 `make test`（确保 `go fmt`、`go vet` 与单元测试 100% 通过）；
   - 运行 `make lint`（确保 `tsc --noEmit` 与 `eslint` 零警告通过）；
   - 运行 `make build && make clean` 验证单二进制打包无误。

---

## 交付红线
1. 严禁使用 `_ = err` 忽略错误；严禁在前端写死 `#fff`、`#000`、`rgba(...)` 等硬编码颜色或滥用静态内联 `style`。
2. 单次回复前确保关闭并清理所有拉起的后台服务或测试进程，严禁占用 `:{port}` 或 `:{dev_port}` 端口。
3. 更新 `README.md` 与 `docs/04-tasks/` 中的完成状态，确认无误后提交（遵循 Conventional Commits 中文规范）并 push。
""")

write_doc("docs/01-features/01-feature-matrix.md", f"""
# {title} (`code-{key}`) 特性清单与需求范围界定规范 (Feature Matrix & Scope)

- **文档编号**：`01-feature-matrix.md`
- **文档版本**：v0.1.0
- **编制日期**：{today}
- **当前阶段**：阶段一：特性清单与边界明确
- **状态**：初始基线（请结合具体业务需求演进完善）

---

## 1. 系统愿景与产品定位 (Vision & Positioning)

### 1.1 项目愿景
**{title} (`code-{key}`)** 是挂载于 CodeBench 开发者综合工作台的核心业务子系统，旨在提供高内聚、可观测、权限严密且完全符合双模主题体验的 {title} 能力闭环。

### 1.2 核心价值主张
1. **即插即用微前端架构**：既可作为远程联邦模块（`/{key}`）无缝嵌入 CodeBench 门户共享单点登录与导航，亦可独立启动（`:{port}` / Vite `:{dev_port}`）进行全功能研发调试。
2. **共享库安全隔离与全量审计**：与平台共享核心用户与部门底座，模块自有表统一通过 `{table_prefix}` 前缀物理隔离，所有业务变更自动记录字段级 Diff 审计日志。
3. **SDD + TDD 质量内建**：所有特性具备明确的接口契约与基于 `testdb.SetupIsolatedDB` 的自动化回归测试保护。

---

## 2. 需求功能矩阵 (Feature Matrix)

特性优先级定义：
- **P0 (Must-Have)**：核心基石特性，首期必须完整落地并具备 100% 自动化测试覆盖。
- **P1 (Should-Have)**：重要业务增强与体验优化特性。
- **P2 (Nice-to-Have)**：远期演进与高级分析扩展特性。

| 编号 | 特性名称 | 涉及层级 | 优先级 | 功能描述与技术规范 | TDD 验收指标 / 衡量标准 |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **F-01** | **核心业务看板与健康观测** | `Frontend`<br>`Backend` | **P0** | 提供 `/api/health` 免密探针与 `/api/items/stats` 业务核心指标聚合看板，实时展示各状态条目分布与运行状态。 | `GET /api/health` 返回 `200 OK`；看板卡片在亮/暗双模主题下无硬编码色值且数据准确。 |
| **F-02** | **业务实体全生命周期 CRUD 与 URL 分页** | `Frontend`<br>`Backend`<br>`Models` | **P0** | 支持业务核心实体的分页列表查询（关键词搜索、状态/分类过滤）、新建、编辑与二次确认删除；前端采用 5 页滑动窗口且与 URL Search Params 双向同步（默认 25 条/页）。 | 1. 隔离库单测验证创建、唯一性冲突(`409`)、修改、删除及分页筛选全部通过。<br>2. 浏览器前进/后退保持分页与筛选状态。 |
| **F-03** | **多级 RBAC 权限守卫与负责人关联** | `Backend`<br>`Frontend` | **P0** | 集成 `commonAuth.AuthMiddleware` 与 `RequireAdmin("{admin_role}")`；普通成员仅可管理本人创建或负责的实体，模块管理员（`{admin_role}`）与超管拥有全量治理权限。 | 未携带 Token 返回 `401`；普通用户调用 `/api/admin/*` 或修改他人条目返回 `403 Forbidden`。 |
| **F-04** | **字段级 Diff 操作审计追踪** | `Backend` | **P0** | 所有写操作（新建/更新/删除/配置变更）接入 `commonAudit.RecordDiff`，自动比对并记录变更前后的字段差异至平台审计中心。 | 执行更新接口后，审计表准确落盘操作人、目标对象及字段级 Old/New Diff。 |
| **F-05** | **模块动态参数治理中心** | `Frontend`<br>`Backend` | **P1** | 为 `{admin_role}` 提供专属模块参数配置页（`/admin/settings`），支持运行时调整模块级开关与阈值。 | 仅管理员可见侧边栏入口并可成功保存配置；普通用户访问被拦截。 |

> 💡 **待扩展业务特性**：请在此处继续追加 `{title}` 专属的领域特性（如 `F-06`、`F-07`...）。

---

## 3. 核心验收场景矩阵 (Verification Scenarios)

| 场景编号 | 场景名称 | 触发条件与输入特征 | 预期行为与 TDD 断言 |
| :--- | :--- | :--- | :--- |
| **SC-01** | **免密探针与未认证拦截** | 1. 请求 `GET /api/health`<br>2. 无 Token 请求 `GET /api/me` | 1. 返回 `200 OK` 且包含 `service: "{service_name}"`<br>2. 返回 `401 Unauthorized` |
| **SC-02** | **业务实体生命周期与唯一性约束** | 认证用户依次执行 `POST /api/items` -> 重复编码 `POST` -> `PUT` 更新 -> `GET` 分页查询 -> `DELETE` | 首次创建 `201`；重复编码拦截返回 `409`；更新与删除返回 `200` 且数据一致 |
| **SC-03** | **RBAC 管理员越权防护** | 普通角色用户（无 `{admin_role}`）请求 `GET /api/admin/settings` 或 `PUT /api/admin/settings` | 鉴权中间件直接短路拦截，返回 `403 Forbidden` |

---

## 4. 非目标边界界定 (Non-Goals)

- **Non-Goal 1**：不重复自建独立的用户管理、部门树维护或登录认证中心，统一复用 CodeBench 与 `code-common` 底座。
- **Non-Goal 2**：不在共享 PostgreSQL 数据库中创建任何不带 `{table_prefix}` 前缀的业务表。
- **Non-Goal 3**：不引入 Ant Design、Material UI 等重型第三方 UI 组件库，保持 `@code/common` + 原生 Vanilla CSS 的轻量一致性。
""")

write_doc("docs/02-design/01-system-architecture.md", f"""
# {title} (`code-{key}`) 系统总体架构与数据模型设计

- **文档编号**：`01-system-architecture.md`
- **文档版本**：v0.1.0
- **编制日期**：{today}
- **归属阶段**：阶段二：系统架构与模型设计

---

## 1. 微前端双模运行架构 (Dual-Mode Architecture)

`code-{key}` 采用前后端一体化单二进制内嵌（`go:embed`）+ Vite Module Federation 微前端联邦架构，无缝支持两种运行形态：

1. **门户嵌入模式 (`isEmbedded = true`)**：
   - CodeBench 宿主门户（`:8000`）通过网关配置挂载 `/{key}`，在运行时动态拉取 `/{key}/assets/remoteEntry.js` 的 `./menu` 与 `./App`。
   - 前端 `setupFetchInterceptor({{{{ appPrefix: '/{key}' }}}})` 自动将模块内的 `/api/*` 请求重写为 `/{key}/api/*` 并携带门户签发的 `Bearer JWT`。
   - 后端 `commonServer.Run` 自动剥离 `/{key}` 前缀，由同一套 Gin 路由处理器响应。
2. **独立开发调试模式 (`isEmbedded = false`)**：
   - 开发者直接访问 Vite 调试端口（`http://192.168.56.18:{dev_port}/{key}/`）或后端端口（`http://192.168.56.18:{port}`）。
   - 自动渲染内置的 `StandaloneShell`（含独立调试侧边栏、亮/暗主题切换器与 `UnifiedLogin` 登录页）。

---

## 2. 数据库模型与表前缀隔离设计 (Database Schema)

本模块与 CodeBench 共享 PostgreSQL 数据库，所有模块自有表均强制以 **`{table_prefix}`** 开头：

```mermaid
erDiagram
    USERS ||--o{{ {table_prefix.upper()}ITEMS : "owner_id / creator_id"
    {table_prefix.upper()}ITEMS {{
        uint id PK
        string code UK "唯一业务编码 (大写标准化)"
        string name "名称"
        string category "业务分类"
        string status "状态: active | draft | archived"
        string priority "优先级: high | medium | low"
        uint owner_id FK "负责人 ID (关联公共 users.id)"
        uint creator_id FK "创建人 ID"
        text description "详细说明"
        datetime created_at
        datetime updated_at
    }}
    {table_prefix.upper()}SETTINGS {{
        uint id PK
        string setting_key UK "参数键名"
        string setting_val "参数值"
        string description "参数说明"
        uint updated_by "最后修改人 ID"
        datetime updated_at
    }}
```

---

## 3. 代码分层与职责边界 (Package Layering)

| 层级 / 目录 | 核心文件 | 职责说明 |
| :--- | :--- | :--- |
| **配置层 (`config/`)** | `config.go` | 集中声明 `ModuleKey="{key}"`、`TablePrefix="{table_prefix}"`、`AdminRole="{admin_role}"`、`DefaultPort=":{port}"` 及 YAML 加载逻辑 |
| **模型层 (`models/`)** | `db.go`, `item.go` | GORM 连接池初始化、`AutoMigrate`、幂等种子数据初始化及业务结构体/白名单校验函数 |
| **控制层 (`handlers/`)** | `health.go`, `auth.go`, `item.go`, `settings.go` | HTTP 参数绑定、RBAC 权限校验、GORM 事务/查询编排、`commonAudit` 审计记录 |
| **前端视图 (`frontend/src/`)** | `App.tsx`, `menu.ts`, `pages/*` | 路由分发、侧边栏契约导出、基于 `--color-*` Token 的业务页面组件 |
""")

write_doc("docs/03-api/01-http-api-contract.md", f"""
# {title} (`code-{key}`) HTTP 接口与权限契约规范

- **文档编号**：`01-http-api-contract.md`
- **文档版本**：v0.1.0
- **编制日期**：{today}
- **归属阶段**：阶段三：接口与数据契约冻结

---

## 1. 路由前缀与鉴权约定

- **独立访问路径**：`/api/<endpoint>`
- **门户网关代理路径**：`/{key}/api/<endpoint>`（由后端 `commonServer.Run` 自动剥离 `/{key}` 前缀，Handler 内部统一注册为 `/api/*`）
- **认证方式**：HTTP Header `Authorization: Bearer <JWT_TOKEN>`

---

## 2. 核心接口契约矩阵 (API Matrix)

| 方法 | 路由路径 | 鉴权级别 | 功能描述 | 审计动作 (`AuditAction`) |
| :---: | :--- | :---: | :--- | :--- |
| `GET` | `/api/health` | 免密 (Public) | 服务存活与版本健康检查探针 | - |
| `GET` | `/api/auth/config` | 免密 (Public) | 获取独立调试模式下的登录方式配置 | - |
| `POST` | `/api/auth/login` | 免密 (Public) | 独立调试模式本地账密登录 | `LOGIN` |
| `GET` | `/api/me` | 登录用户 (JWT) | 获取当前登录用户信息及 `is_module_admin` 权限标识 | - |
| `GET` | `/api/items/stats` | 登录用户 (JWT) | 获取看板各状态条目统计汇总 | - |
| `GET` | `/api/items` | 登录用户 (JWT) | 分页查询业务条目（支持 `page`, `page_size`, `search`, `status`, `category`） | - |
| `POST` | `/api/items` | 登录用户 (JWT) | 新建业务条目（自动将 `code` 转为大写并校验唯一性） | `CREATE_ITEM` |
| `PUT` | `/api/items/:id` | 创建人/负责人/`{admin_role}` | 更新业务条目并记录字段级 Diff 审计 | `UPDATE_ITEM` |
| `DELETE` | `/api/items/:id` | 创建人/负责人/`{admin_role}` | 删除业务条目 | `DELETE_ITEM` |
| `GET` | `/api/admin/settings` | 仅管理员 (`{admin_role}`) | 读取模块治理参数列表 | - |
| `PUT` | `/api/admin/settings` | 仅管理员 (`{admin_role}`) | 批量更新模块治理参数并记录审计 Diff | `UPDATE_SETTINGS` |

---

## 3. 标准分页与错误响应格式

### 3.1 分页列表响应 (`200 OK`)
```json
{{
  "items": [],
  "total": 42,
  "page": 1,
  "page_size": 25
}}
```

### 3.2 统一错误响应 (`400 / 401 / 403 / 404 / 409 / 500`)
```json
{{
  "error": "具体的中文错误原因说明"
}}
```
""")

write_doc("docs/04-tasks/01-roadmap.md", f"""
# {title} (`code-{key}`) 工程实施路线图与 TDD 验收总则 (Roadmap)

- **文档编号**：`01-roadmap.md`
- **文档版本**：v0.1.0
- **编制日期**：{today}
- **归属阶段**：阶段三/四：TDD 实施规划与质量门禁

---

## 1. 工程递进策略 (TDD Execution Strategy)

基于已冻结的架构设计（[`docs/02-design/01-system-architecture.md`](../02-design/01-system-architecture.md)）与接口契约（[`docs/03-api/01-http-api-contract.md`](../03-api/01-http-api-contract.md)），项目划分为正交递进的里程碑。每个里程碑均遵循 **TDD（测试先行 -> 编码实现 -> 重构验收）** 原则。

```mermaid
flowchart LR
    M1["Milestone 1: 脚手架基线与核心域模型<br/>({table_prefix}* 表结构 + 基础 CRUD + 隔离单测)"]
    M2["Milestone 2: 核心业务逻辑与外部协同<br/>(领域专有服务 + 规则引擎 + 边界单测)"]
    M3["Milestone 3: 微前端交互与双模主题体验<br/>(业务视图 + URL 分页 + 亮/暗主题验收)"]
    M4["Milestone 4: CodeBench 门户集成与生产交付<br/>(网关挂载 + 全链路回归 + 单二进制构建)"]

    M1 ==> M2 ==> M3 ==> M4
```

---

## 2. 里程碑全景矩阵

| 里程碑 | 作战卡文档 | 核心交付物 | 自动化验收门禁 (Quality Gate) | 状态 |
| :--- | :--- | :--- | :--- | :---: |
| **M1：基础底座与核心模型** | [`02-m1-domain-model-and-api.md`](./02-m1-domain-model-and-api.md) | `config/`, `models/`, `handlers/`, `handlers/item_test.go`, `frontend/` 基础脚手架 | `make test` 与 `make lint` 100% 通过 | ✅ 已就绪 |
| **M2：定制领域模型与 API 扩展** | *(待按需新建 `03-m2-*.md`)* | 新增 `{table_prefix}*` 领域模型、业务 Handler 与 `testdb` 隔离单测 | `TEST_DB_DSN=... make test` 全绿 | ⏳ 待规划 |
| **M3：前端领域视图与交互深耕** | *(待按需新建 `04-m3-*.md`)* | 领域专属 React 页面、抽屉表单、图表看板与样式 | `make lint && make build` 零报错 | ⏳ 待规划 |
| **M4：门户联调与上线交付** | [`docs/06-ops/01-verification-and-deployment-guide.md`](../06-ops/01-verification-and-deployment-guide.md) | `code-bench/config.yaml` 网关挂载与生产二进制包 | 上线前自检清单 100% 达成 | ⏳ 待执行 |

---

## 3. 全局强制质量门禁 (Mandatory Quality Gates)

1. **表前缀合规**：所有 GORM 模型的 `TableName()` 必须以 `{table_prefix}` 开头。
2. **静态与类型检查零容忍**：每次提交前必须通过 `make test`（`go fmt` + `go vet` + `go test`）与 `make lint`（`tsc --noEmit` + `eslint`）。
3. **进程与端口卫生**：任何验证结束后，后台不得残留占用 `:{port}` 或 `:{dev_port}` 的进程。
""")

write_doc("docs/04-tasks/02-m1-domain-model-and-api.md", f"""
# Milestone 1 作战卡：基础领域模型、RBAC 鉴权与 CRUD 闭环

- **文档编号**：`02-m1-domain-model-and-api.md`
- **归属阶段**：阶段三/四：M1 任务拆解与 TDD 验收标准
- **前置依赖**：`docs/02-design/01-system-architecture.md`、`docs/03-api/01-http-api-contract.md`

---

## 1. M1 实施目标

确立 `code-{key}` 的工程底座：完成 `{table_prefix}items` 与 `{table_prefix}settings` 数据模型隔离、JWT + `{admin_role}` 角色鉴权守卫、带字段级 Diff 审计的 CRUD 接口，以及前端双模容器与 URL 分页列表页。

---

## 2. TDD 单元测试用例清单 (`handlers/item_test.go`)

在扩展或修改 M1 核心逻辑时，必须确保以下 TDD 用例 100% 通过：
1. **`TestModelTablePrefixAndValidators`**：
   - 断言 `TemplateItem{{}}.TableName() == "{table_prefix}items"`；
   - 断言 `TemplateSetting{{}}.TableName() == "{table_prefix}settings"`；
   - 断言状态与优先级白名单校验函数准确拦截非法枚举值。
2. **`TestHealthAndAuthRBAC`**：
   - 免密访问 `GET /api/health` 返回 `200 OK`；
   - 未携带 Token 访问 `GET /api/me` 返回 `401 Unauthorized`；
   - 普通角色用户访问 `GET /api/me` 返回 `is_module_admin: false`，且访问 `GET /api/admin/settings` 被 `403 Forbidden` 拦截。
3. **`TestItemCRUDWithIsolatedDB`**（基于 `testdb.SetupIsolatedDB`）：
   - 自动创建临时 PostgreSQL 隔离库并完成 `InitDB` 迁移与种子数据写入；
   - 验证新建条目 (`201`) -> 重复编码冲突拦截 (`409`) -> 更新条目与状态 (`200`) -> 关键词分页检索 (`200`) -> 删除条目 (`200`) 全链路闭环。

---

## 3. 验收标准 (Acceptance Criteria)

- [x] **AC-M1-01**：`make test`（含 `go fmt`、`go vet`、表前缀与 RBAC 单元测试）零警告通过。
- [x] **AC-M1-02**：在配置 `TEST_DB_DSN`（指向 `postgres` 管理库）时，`TestItemCRUDWithIsolatedDB` 物理隔离数据库测试 100% 通过。
- [x] **AC-M1-03**：`make lint`（前端 TypeScript 类型检查与 ESLint 检查）零错误通过。
- [x] **AC-M1-04**：`make build` 成功生成内嵌前端静态资源的单二进制文件 `code-{key}-server`。
""")

write_doc("docs/05-adr/01-microfrontend-and-table-prefix-isolation.md", f"""
# ADR-01: 采用 Vite Module Federation 双模容器与共享库 `{table_prefix}` 表前缀隔离

- **文档编号**：`01-microfrontend-and-table-prefix-isolation.md`
- **日期**：{today}
- **状态**：已采纳 (Accepted)

---

## 1. 背景与问题陈述 (Context & Problem Statement)

`code-{key}`（{title}）作为 CodeBench 研发工作台生态的子系统，需要解决三个核心架构问题：
1. **如何在不重新编译宿主门户 `code-bench` 的前提下实现前端即插即用与独立开发兼顾？**
2. **如何在与平台共享用户（`users`）、部门（`departments`）和审计日志（`audit_logs`）的同时，避免业务表命名冲突与测试污染？**
3. **如何在前后端分离开发与生产极简交付之间取得平衡？**

---

## 2. 决策方案 (Decisions)

### 决策 1：基于 `@originjs/vite-plugin-federation` 暴露 `./App` 与 `./menu` 双模契约
- 前端构建固定设置 `base: '/{key}/'`，向外暴露 `./App` 与 `./menu` 两个标准入口，共享 `react`、`react-dom`、`react-router-dom`。
- 根据 `isEmbedded` 属性切换渲染轻量路由视图（`EmbeddedRoutes`）或独立调试外壳（`StandaloneShell`）。

### 决策 2：共享 PostgreSQL 实例 + 强制 `{table_prefix}` 表名前缀 + `testdb` 物理隔离单测
- 生产与联调环境下复用平台统一 PostgreSQL 库，通过 `config.TablePrefix = "{table_prefix}"` 强制约束所有模块自有表名。
- 单元测试严禁连接共享库，统一通过 `code-common/backend/testdb.SetupIsolatedDB(t)` 动态创建独立临时库并在测试结束后自动 `DROP DATABASE`。

### 决策 3：基于 Go 1.16+ `//go:embed all:frontend/dist` 实现单二进制交付
- 编译期将前端构建产物完整内嵌至 `code-{key}-server` 单一可执行文件中，由 `commonServer.Run` 统一处理 API 路由、网关前缀剥离与 SPA History Fallback。
""")

write_doc("docs/06-ops/01-verification-and-deployment-guide.md", f"""
# {title} (`code-{key}`) 联调验证与生产部署手册

- **文档编号**：`01-verification-and-deployment-guide.md`
- **文档版本**：v0.1.0
- **编制日期**：{today}
- **归属阶段**：阶段四：交付验证与运维指南

---

## 1. 本地开发与物理隔离测试验证

### 1.1 执行全套静态检查与单元测试
```bash
# 后端格式化、静态检查与基础单测
make test

# 携带物理隔离临时数据库的完整 CRUD 集成单测（注意：dbname 必须为 postgres 或 *_test）
TEST_DB_DSN="host=127.0.0.1 port=5432 user=postgres password=YOUR_PASSWORD dbname=postgres sslmode=disable" make test

# 前端 TypeScript 与 ESLint 检查
make lint
```

### 1.2 独立调试模式启动
```bash
# 终端 1：编译并启动后端（监听 :{port}）
cp config.yaml.example config.yaml
make build && ./code-{key}-server

# 终端 2（可选，用于前端热更新调试）：启动 Vite 开发服务器（监听 :{dev_port}）
cd frontend && npm run dev
```
> 调试完成后请务必关闭启动的服务进程，释放 `:{port}` 与 `:{dev_port}` 端口。

---

## 2. 挂载接入 CodeBench 宿主门户

在 `../code-bench/config.yaml` 的 `gateways:` 节点下添加本模块配置：

```yaml
gateways:
  {key}:
    url: "http://127.0.0.1:{port}"
    title: "{title}"
    icon: "{icon}"
    description: "{title} 业务子系统"
```

重启 `code-bench` 后，访问 `http://192.168.56.18:8000` 即可在工作台首页与左侧导航栏看到 **「{title}」** 模块。

---

## 3. 上线前必过自检清单 (Pre-Release Checklist)

- [ ] `config/config.go` 与所有新增 GORM 模型的 `TableName()` 均以 `{table_prefix}` 开头；
- [ ] 所有写操作接口均已接入 `commonAudit` 操作审计，管理员接口均已挂载 `RequireAdmin("{admin_role}")`；
- [ ] 前端所有新增页面在亮色（Light）与暗色（Dark）主题下均无视觉黑块或文字融底，无硬编码色值，无 `any` 类型；
- [ ] `make test && make lint && make build` 全量零错误通过。
""")
PYEOF

if [[ "$TARGET_DIR" != "$TEMPLATE_ROOT" ]]; then
  log_success "新业务模块工程已成功派生至: $TARGET_DIR"
else
  log_success "当前工程代码与微前端契约已就地完成全链路替换！"
fi
echo ""
echo "=================================================================="
echo "下一步接入 CodeBench 操作指引："
echo "------------------------------------------------------------------"
if [[ "$TARGET_DIR" != "$TEMPLATE_ROOT" ]]; then
  echo "1. 进入新模块目录："
  echo "   cd $TARGET_DIR"
  echo ""
  echo "2. 生成本地配置并编译验证："
else
  echo "1. 生成本地配置并编译验证："
fi
echo "   cp config.yaml.example config.yaml"
echo "   # 请确保 config.yaml 中的 database 与 auth.jwt_secret 与 code-bench 一致"
echo "   make install && make lint && make test && make build"
echo ""
if [[ "$TARGET_DIR" != "$TEMPLATE_ROOT" ]]; then
  echo "3. 在 ../code-bench/config.yaml 的 gateways 节点下追加以下配置："
else
  echo "2. 在 ../code-bench/config.yaml 的 gateways 节点下追加以下配置："
fi
echo ""
cat <<YAML
gateways:
  ${MODULE_KEY}:
    url: "http://127.0.0.1:${BACKEND_PORT}"
    title: "${MODULE_TITLE}"
    icon: "${MODULE_ICON}"
    description: "${MODULE_TITLE} 业务子系统"
YAML
echo ""
if [[ "$TARGET_DIR" != "$TEMPLATE_ROOT" ]]; then
  echo "4. 启动本服务 (./code-${MODULE_KEY}-server) 并重启 code-bench 即可生效！"
else
  echo "3. 启动本服务 (./code-${MODULE_KEY}-server) 并重启 code-bench 即可生效！"
fi
echo "=================================================================="
