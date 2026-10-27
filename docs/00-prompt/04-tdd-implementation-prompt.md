# code-traefik: 阶段四 TDD 工程实施与全链路交付验证提示词指令

## 角色定位与工程原则
在阶段一（特性矩阵）、阶段二（系统架构与数据模型）与阶段三（API 契约与 Milestone 作战卡）全部定稿冻结后，现在正式进入：
**「阶段四：TDD 测试驱动开发、全栈实施与自动化验证」**。

---

## TDD 实施循环与步骤 (Red -> Green -> Refactor)

请严格按照 `docs/04-tasks/01-roadmap.md` 定义的里程碑顺序推进，每个 Milestone 内部执行以下闭环：

1. **Step 1 - 编写先行测试 (Red)**：
   - 在 `handlers/*_test.go`（或领域包 `*_test.go`）中，基于 `testdb.SetupIsolatedDB(t)` 编写本里程碑契约要求的单元测试与 HTTP 接口测试（涵盖正常 CRUD、分页筛选、参数非法 `400`、未登录 `401`、非管理员越权 `403`）。
2. **Step 2 - 编写最小完备业务实现 (Green)**：
   - 落地 `models/`（确保表前缀为 `traefik_`）、`handlers/`（显式处理所有 `error`，接入 `commonAudit` 审计）与 `main.go` 路由注册；
   - 落地 `frontend/src/` 页面与 API 对接（遵循 `--color-*` 亮暗双模主题 Token、Flat BEM 命名、URL 同步分页规范、零 `any` 类型）。
3. **Step 3 - 质量门禁验证与重构 (Refactor & Verify)**：
   - 运行 `make test`（确保 `go fmt`、`go vet` 与单元测试 100% 通过）；
   - 运行 `make lint`（确保 `tsc --noEmit` 与 `eslint` 零警告通过）；
   - 运行 `make build && make clean` 验证单二进制打包无误。

---

## 交付红线
1. 严禁使用 `_ = err` 忽略错误；严禁在前端写死 `#fff`、`#000`、`rgba(...)` 等硬编码颜色或滥用静态内联 `style`。
2. 单次回复前确保关闭并清理所有拉起的后台服务或测试进程，严禁占用 `:8091` 或 `:5181` 端口。
3. 更新 `README.md` 与 `docs/04-tasks/` 中的完成状态，确认无误后提交（遵循 Conventional Commits 中文规范）并 push。
