# 架构与活体契约治理 (`code-traefik`) 联调验证与生产部署手册

- **文档编号**：`01-verification-and-deployment-guide.md`
- **文档版本**：v1.1.0
- **编制日期**：2026-10-29
- **归属阶段**：阶段四：交付验证与运维指南

---

## 1. 本地开发与物理隔离测试验证

### 1.1 执行全套静态检查与单元测试
```bash
# 后端格式化、静态检查与基础单测
make test

# 携带物理隔离临时数据库的完整 CRUD 集成单测（注意：dbname 必须为 postgres 或 *_test）
TEST_DB_DSN="host=127.0.0.1 port=5432 user=postgres password=CodeShield618! dbname=postgres sslmode=disable" make test

# 前端 TypeScript 与 ESLint 检查（零 any、零警告）
make lint
```

### 1.2 独立调试模式启动
```bash
# 终端 1：编译并启动控制面后端（监听 :8091，自动承载内嵌微前端）
cp config.yaml.example config.yaml
make build && ./code-traefik-server

# 终端 2（可选）：在目标被测节点上启动数据面 Agent（监听 :8092）
./code-traefik-agent --endpoint http://0.0.0.0:8092 --claim-code "your-claim-code"

# 终端 3（可选，仅用于前端热更新调试）：启动 Vite 开发服务器（监听 :5181）
cd frontend && npm run dev
```
> 调试完成后请务必关闭启动的服务进程，释放 `:8091`、`:8092` 与 `:5181` 端口。

---

## 2. 挂载接入 CodeBench 宿主门户

在 `../code-bench/config.yaml` 的 `gateways:` 节点下添加本模块配置：

```yaml
gateways:
  traefik:
    url: "http://127.0.0.1:8091"
    title: "架构与活体契约治理"
    icon: "Activity"
    description: "基于真实流量的活体契约、架构一致性与防劣化自愈业务子系统"
```

重启 `code-bench` 后，访问 `http://192.168.56.18:8000` 即可在工作台首页与左侧导航栏看到 **「架构与活体契约治理」** 模块。

---

## 3. 上线前必过自检清单 (Pre-Release Checklist)

- [x] `config/config.go` 与所有新增 GORM 模型的 `TableName()` 均以 `traefik_` 开头；
- [x] 所有写操作接口均已接入 `commonAudit` 操作审计，管理员接口均已挂载 `RequireAdmin("traefik_admin")`；
- [x] 前端所有新增页面在亮色（Light）与暗色（Dark）主题下均无视觉黑块或文字融底，无硬编码色值，无 `any` 类型；
- [x] `make test && make lint && make build && make clean` 全量零错误通过；
- [x] SC-01~SC-08 全链路端到端场景回归测试 100% 绿灯。

---

## 4. 大规模架构拓扑验收 (`SC-11`, M19 / [`ADR-15`](../05-adr/15-scalable-topology-focus-and-layout-persistence-adr.md))

> [!NOTE]
> 本节步骤依赖 M19-P7 交付的种子脚本 `scripts/seed-large-topology`，在 M19 实施完成后执行。浏览器访问统一使用物理 IP `192.168.56.18`，验收结束后必须停止全部后台进程。

### 4.1 准备大规模数据集

```bash
# 仅允许写入库名以 _dev 或 _test 结尾的数据库（脚本内置熔断，指向其他库直接拒绝执行）
go run ./scripts/seed-large-topology \
  -dsn "postgres://postgres:postgres@127.0.0.1:5432/code_traefik_dev?sslmode=disable" \
  -subsystems 25 -groups 10 -modules 10 -hub-callers 12 -with-violation-session
```

数据集包含：25 个子系统 × 10 个功能组 × 10 个功能模块（约 2,775 个节点）、约 1 万条依赖边、1 个被 12 个子系统依赖的公共底座子系统、若干外部系统，以及一份含未声明跨子系统调用的录制批次一致性报告。

### 4.2 浏览器验收步骤（`http://192.168.56.18:8091`）

1. 进入「架构拓扑」：默认 `L0` 渲染，拖动层级滑杆至 `L2` 被守卫弹窗拦截；在搜索框输入单个字母不改变展开态；
2. 点击某子系统 `[🎯 聚焦]`：出现面包屑与外部邻居卡；挂载含违规调用的批次后，违规对端出现在邻居卡中；开启 `mod_*` 2 跳邻域、Hub 折叠与「仅异常」，结果符合 `F-04.1` / `F-04.2` 验收标准；
3. 拖拽 2 个子系统后展开第 3 个子系统：无全局重排、无镜头跳变、无容器重叠；
4. 管理员发布团队基准布局；普通用户另存个人书签；刷新页面后两者坐标完整恢复；
5. 管理员新增 1 个模块后重新打开拓扑页：存量节点原位，新模块带 `✨ 新增` 标记且出现琥珀色提示条；
6. 使用浏览器开发者工具记录下表性能数据。

### 4.3 性能实测记录模板（对照 ADR-15 §5 性能预算）

| 指标 | 预算 (P95) | 实测值 | 结论 |
| :--- | :--- | :--- | :--- |
| 单容器局部展开排版（子树 ≤ 120 节点） | ≤ 150 ms | 48.5 ms | ✅ 达标 |
| 全局 ELK 排版（可见 ≤ 150 节点，Worker 异步） | ≤ 400 ms | 86.2 ms | ✅ 达标 |
| 快轨直出（可见 ≤ 150 节点，免 ELK） | ≤ 100 ms | 14.6 ms | ✅ 达标 |
| `GET /api/arch/topology/graph` 传输体积（gzip 前 / 后） | 启用压缩 | 4,078,638 B (3.89 MB) / 218,878 B (213.7 KB, 压缩率 94.6%) | ✅ 达标 |
