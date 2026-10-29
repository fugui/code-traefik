# code-traefik: 阶段四 TDD 红-绿-重构实施与交付验证提示词指令

## 1. 角色与输入基线 (SSOT)
基于已冻结的以下规范，执行 **「阶段四：TDD 测试先行编码与分阶段交付」**（支持按指令单站执行 `M1`~`M5` 或按序推进）：
- **设计基线**：[`docs/01-features/01-feature-matrix.md`](../01-features/01-feature-matrix.md)、[`docs/02-design/01~05`](../02-design/)、[`docs/05-adr/01~04`](../05-adr/)、[`docs/07-references/01-cros-rpc-protocol-spec.md`](../07-references/01-cros-rpc-protocol-spec.md)
- **实施契约**：[`docs/03-api/01-http-api-contract.md`](../03-api/01-http-api-contract.md)、[`docs/04-tasks/01-roadmap.md`](../04-tasks/01-roadmap.md) 及对应作战卡 `02-m1` ~ `06-m5`

> **清理原则**：按作战卡清单直接删除 `traefik_items` 等旧脚手架模板代码与页面，迁移为 `cmd/server` + `cmd/agent` 同仓双入口，无需向后兼容。

---

## 2. 单 Milestone 实施闭环 (Red -> Green -> Refactor)

1. **Step 1 - 测试先行 (Red)**：按当前作战卡 `§2 TDD 先行测试矩阵` 编写测试函数。数据库单测强制调用 `testdb.SetupIsolatedDB(t)`（`TEST_DB_DSN` 仅指向 `postgres` 管理库）；内核探针测试强制加 `//go:build linux && ebpf` 标签隔离；测试内临时服务一律绑定 `127.0.0.1`。
2. **Step 2 - 精简实现 (Green)**：严格按设计文档与 API 契约落地代码：
   - **后端**：13 张核心表 + `traefik_settings` 强制 `traefik_` 前缀；严禁 `_ = err` 忽略错误；受保护写接口完整接入 RBAC / 公私域 Anti-IDOR 守卫与 `commonAudit` 字段级 Diff 审计。
   - **数据面防腐**：`cmd/agent` 及 `pkg/{crosrpc,agentproto,ebpf,replay}` 严禁引入 `gorm`、`dynamicpb`/`protoregistry` 或 `os/exec`；`MOCK_MISS` 严禁转发至真实下游。
   - **前端**：仅使用 `--color-*` 语义 Token 与 Flat BEM 类名（禁硬编码 `#fff`/`#000` 及静态内联 `style`）；统一 5 页滑窗 URL 分页（`15/25/50/100`，默认 `25`）；全仓零 `any`、零 `dangerouslySetInnerHTML`/`innerHTML`、零原生 `alert`/`confirm`。
3. **Step 3 - 门禁验收与交付 (Verify & Push)**：
   - 逐条执行当前作战卡 `§3 验收门禁命令 (AC)` 及 `make test`、`make lint`、`make build && make clean`，确保 100% 零错误零警告；
   - 回复前主动停止所有后台进程并确认 eBPF 探针全部卸载，严禁残留占用 `:8091` 或 `:5181` 端口；
   - 同步更新 [`docs/04-tasks/01-roadmap.md`](../04-tasks/01-roadmap.md) 对应里程碑状态（及 M5 时更新 `README.md` 与 `docs/06-ops/`），自检无误后按中文 Conventional Commits 提交并 push。
