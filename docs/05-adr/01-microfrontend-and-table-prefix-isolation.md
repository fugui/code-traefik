# ADR-01: 采用 Vite Module Federation 双模容器与共享库 `traefik_` 表前缀隔离

- **文档编号**：`01-microfrontend-and-table-prefix-isolation.md`
- **日期**：2026-10-27
- **状态**：已采纳 (Accepted)

---

## 1. 背景与问题陈述 (Context & Problem Statement)

`code-traefik`（业务流量监控与重放）作为 CodeBench 研发工作台生态的子系统，需要解决三个核心架构问题：
1. **如何在不重新编译宿主门户 `code-bench` 的前提下实现前端即插即用与独立开发兼顾？**
2. **如何在与平台共享用户（`users`）、部门（`departments`）和审计日志（`audit_logs`）的同时，避免业务表命名冲突与测试污染？**
3. **如何在前后端分离开发与生产极简交付之间取得平衡？**

---

## 2. 决策方案 (Decisions)

### 决策 1：基于 `@originjs/vite-plugin-federation` 暴露 `./App` 与 `./menu` 双模契约
- 前端构建固定设置 `base: '/traefik/'`，向外暴露 `./App` 与 `./menu` 两个标准入口，共享 `react`、`react-dom`、`react-router-dom`。
- 根据 `isEmbedded` 属性切换渲染轻量路由视图（`EmbeddedRoutes`）或独立调试外壳（`StandaloneShell`）。

### 决策 2：共享 PostgreSQL 实例 + 强制 `traefik_` 表名前缀 + `testdb` 物理隔离单测
- 生产与联调环境下复用平台统一 PostgreSQL 库，通过 `config.TablePrefix = "traefik_"` 强制约束所有模块自有表名。
- 单元测试严禁连接共享库，统一通过 `code-common/backend/testdb.SetupIsolatedDB(t)` 动态创建独立临时库并在测试结束后自动 `DROP DATABASE`。

### 决策 3：基于 Go 1.16+ `//go:embed all:frontend/dist` 实现单二进制交付
- 编译期将前端构建产物完整内嵌至 `code-traefik-server` 单一可执行文件中，由 `commonServer.Run` 统一处理 API 路由、网关前缀剥离与 SPA History Fallback。
