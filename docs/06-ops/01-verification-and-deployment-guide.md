# 业务流量监控与重放 (`code-traefik`) 联调验证与生产部署手册

- **文档编号**：`01-verification-and-deployment-guide.md`
- **文档版本**：v0.1.0
- **编制日期**：2026-10-27
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
# 终端 1：编译并启动后端（监听 :8091）
cp config.yaml.example config.yaml
make build && ./code-traefik-server

# 终端 2（可选，用于前端热更新调试）：启动 Vite 开发服务器（监听 :5181）
cd frontend && npm run dev
```
> 调试完成后请务必关闭启动的服务进程，释放 `:8091` 与 `:5181` 端口。

---

## 2. 挂载接入 CodeBench 宿主门户

在 `../code-bench/config.yaml` 的 `gateways:` 节点下添加本模块配置：

```yaml
gateways:
  traefik:
    url: "http://127.0.0.1:8091"
    title: "业务流量监控与重放"
    icon: "Activity"
    description: "业务流量监控与重放 业务子系统"
```

重启 `code-bench` 后，访问 `http://192.168.56.18:8000` 即可在工作台首页与左侧导航栏看到 **「业务流量监控与重放」** 模块。

---

## 3. 上线前必过自检清单 (Pre-Release Checklist)

- [ ] `config/config.go` 与所有新增 GORM 模型的 `TableName()` 均以 `traefik_` 开头；
- [ ] 所有写操作接口均已接入 `commonAudit` 操作审计，管理员接口均已挂载 `RequireAdmin("traefik_admin")`；
- [ ] 前端所有新增页面在亮色（Light）与暗色（Dark）主题下均无视觉黑块或文字融底，无硬编码色值，无 `any` 类型；
- [ ] `make test && make lint && make build` 全量零错误通过。
