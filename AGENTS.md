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

* **本模块专有架构红线（阶段二已冻结，详见 [`docs/02-design/`](docs/02-design/)）**：
  1. **主从物理防腐**：`cmd/agent` 及 `pkg/{crosrpc,agentproto,ebpf,replay}` 严禁引入 `gorm`、数据库连接或 `dynamicpb`/`protoregistry`；主控 `cmd/server` 以非特权运行，严禁直接挂载 eBPF 或跨网发重放包。
  2. **协议与契约唯一性**：仅处理 IPv4 TCP 明文 DOPHI `CROS RPC` 报文；`ascServiceName` 强制采用点分规范 `{package}.{serviceName}`（若无 package 则为 `{serviceName}`），`PROVIDE` 接口全局唯一。
  3. **公私域隔离与 Agent 动态排他租约**：三级架构元素与 `.pb` 为全局唯一公共 SSOT；`Agent` 与 `RecordingSession` 严格区分 `SHARED`（团队共享）与 `PRIVATE`（仅 `owner_id` 本人可见与操作，横向越权返 `403`）；同一台 `Agent` 同一时刻仅允许 1 名用户持有 1 项录制或重放排他租约（并发争抢返 `409 Conflict`）；`traefik_traffic_records` 强制采用单次往返一行双模持久化（`raw_req_packet` + `raw_rsp_packet` `bytea` 与 `payload_json` `jsonb`）。
  4. **重放零外泄与探针卫生**：重放 `MOCK_MISS` 严禁转发至真实下游；主控失联超宽限期（5 分钟）或收到退出信号时，Agent 必须自动卸载全部 eBPF 探针与监听端口。

---

## 3. 常用验证命令

```bash
make test                                                                                      # 后端 fmt + vet + 单元测试
TEST_DB_DSN="host=127.0.0.1 port=5432 user=postgres password=PWD dbname=postgres sslmode=disable" make test  # 物理隔离库全量单测
make lint                                                                                      # 前端 tsc + eslint 检查
make build && make clean                                                                       # 单二进制打包验证并清理产物
```
