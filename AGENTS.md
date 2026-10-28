# AGENTS.md - 业务流量监控与重放 (Code-Traefik)

> 💡 **上位准则**：工作区通用 SDD+TDD 四阶段研发流水线、`docs/` 双位编号规范、Go/React 编码风格、双模主题 Token、通用分页及自动交付规章，统一遵循 [`../AGENTS.md`](../AGENTS.md)（`code-common/rules/AGENTS.md`），本文档仅定义本模块专属契约与边界。

---

## 1. 模块专属契约 (Module Contract)

| 契约项 | 规范值 | 强约束说明 |
| :--- | :--- | :--- |
| **模块标识 (`ModuleKey`)** | `traefik` | 门户路由前缀 `/traefik`，Vite Federation 名称 `name: 'traefik'` |
| **服务与二进制名** | `Code-Traefik` / `code-traefik-server` + `code-traefik-agent` | Go Module: `code-traefik`；同仓双入口 `cmd/server`、`cmd/agent` |
| **端口分配** | 后端 `:8091` / 前端 `5181` | 验证完毕后必须主动关闭进程释放端口 |
| **数据库表前缀 (`TablePrefix`)** | `traefik_` | 本模块所有 GORM 模型 `TableName()` 必须以 `traefik_` 开头 |
| **管理员角色 (`AdminRole`)** | `traefik_admin` | 管理接口必须挂载 `commonAuth.RequireAdmin("traefik_admin")` |

---

## 2. 目录结构与业务边界

```text
code-traefik/
├── docs/                # SDD + TDD 规范文档库 (00-prompt ~ 06-ops)
├── cmd/server/          # (规划) 主控控制面入口 code-traefik-server
├── cmd/agent/           # (规划) 数据面 Agent 入口 code-traefik-agent（零数据库、零 .pb 依赖）
├── pkg/crosrpc/         # (规划) CROS RPC 分帧与头解析库（主控与 Agent 共用）
├── pkg/ebpf/            # (规划) Agent 嵌入式 cilium/ebpf 采集引擎
├── pkg/replay/          # (规划) Agent 二进制 Mock 匹配与 ullSNO 自愈
├── config/              # 配置加载与模块常量定义
├── models/              # GORM 数据模型（强制 traefik_ 表前缀隔离）
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
