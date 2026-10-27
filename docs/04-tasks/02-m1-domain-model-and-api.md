# Milestone 1 作战卡：基础领域模型、RBAC 鉴权与 CRUD 闭环

- **文档编号**：`02-m1-domain-model-and-api.md`
- **归属阶段**：阶段三/四：M1 任务拆解与 TDD 验收标准
- **前置依赖**：`docs/02-design/01-system-architecture.md`、`docs/03-api/01-http-api-contract.md`

---

## 1. M1 实施目标

确立 `code-traefik` 的工程底座：完成 `traefik_items` 与 `traefik_settings` 数据模型隔离、JWT + `traefik_admin` 角色鉴权守卫、带字段级 Diff 审计的 CRUD 接口，以及前端双模容器与 URL 分页列表页。

---

## 2. TDD 单元测试用例清单 (`handlers/item_test.go`)

在扩展或修改 M1 核心逻辑时，必须确保以下 TDD 用例 100% 通过：
1. **`TestModelTablePrefixAndValidators`**：
   - 断言 `TemplateItem{}.TableName() == "traefik_items"`；
   - 断言 `TemplateSetting{}.TableName() == "traefik_settings"`；
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
- [x] **AC-M1-04**：`make build` 成功生成内嵌前端静态资源的单二进制文件 `code-traefik-server`。
