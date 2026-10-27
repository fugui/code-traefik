# 业务流量监控与重放 (`code-traefik`) HTTP 接口与权限契约规范

- **文档编号**：`01-http-api-contract.md`
- **文档版本**：v0.1.0
- **编制日期**：2026-10-27
- **归属阶段**：阶段三：接口与数据契约冻结

---

## 1. 路由前缀与鉴权约定

- **独立访问路径**：`/api/<endpoint>`
- **门户网关代理路径**：`/traefik/api/<endpoint>`（由后端 `commonServer.Run` 自动剥离 `/traefik` 前缀，Handler 内部统一注册为 `/api/*`）
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
| `PUT` | `/api/items/:id` | 创建人/负责人/`traefik_admin` | 更新业务条目并记录字段级 Diff 审计 | `UPDATE_ITEM` |
| `DELETE` | `/api/items/:id` | 创建人/负责人/`traefik_admin` | 删除业务条目 | `DELETE_ITEM` |
| `GET` | `/api/admin/settings` | 仅管理员 (`traefik_admin`) | 读取模块治理参数列表 | - |
| `PUT` | `/api/admin/settings` | 仅管理员 (`traefik_admin`) | 批量更新模块治理参数并记录审计 Diff | `UPDATE_SETTINGS` |

---

## 3. 标准分页与错误响应格式

### 3.1 分页列表响应 (`200 OK`)
```json
{
  "items": [],
  "total": 42,
  "page": 1,
  "page_size": 25
}
```

### 3.2 统一错误响应 (`400 / 401 / 403 / 404 / 409 / 500`)
```json
{
  "error": "具体的中文错误原因说明"
}
```
