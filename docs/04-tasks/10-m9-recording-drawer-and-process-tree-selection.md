# M9 任务作战卡：协同录制抽屉重构与进程树选择器 (Recording Drawer & Process Tree Selection)

## 1. 任务概述与里程碑目标
将流量录制工作台（`RecordingWorkbenchPage`）中的「发起协同录制」弹窗从旧版居中 `Modal` 彻底重构为沉浸式右侧抽屉 `RecordingStartDrawer`。解决上部表单挤压、补充宿主 Agent 明确选择与状态防呆门禁、并将混乱平铺的微服务进程列表重构为具备组级全选/半选、折叠展开与搜索过滤的高质感业务进程树。

## 2. 验收门禁 (Acceptance Criteria)
- [x] **AC-1 (抽屉容器与操作吸底)**：点击「发起协同录制」滑出右侧 `Drawer`（宽度 `680px`），底部 Sticky 操作栏常驻屏幕右下角，左侧显示已选进程数与所属 Agent 汇总，右侧提供取消与立即启动按钮。
- [x] **AC-2 (作用域双卡片解挤)**：素材作用域改为并排两列卡片选择（`PRIVATE` / `SHARED`），包含明确标题、语义标签与说明文案，选中态具备主题色高亮，彻底消除原生单选框紧凑拥挤感。
- [x] **AC-3 (宿主 Agent 选择与状态防呆)**：顶部展示目标宿主机 Agent 下拉选择框，展示名称、IP、端口及在线/租约徽标；抽屉打开时自动默认选中首个在线且空闲的 Agent；若选中的 Agent 离线或被占用，下方进程树只读禁用并展示告警提示。
- [x] **AC-4 (业务进程树与组级联动)**：进程按 `group_name` 业务组分层归集，展示组展开/折叠、组级复选框（支持全选、清空、以及部分选中时的 `indeterminate` 半选状态）；组右侧显示 `(已选/总数)` 统计。
- [x] **AC-5 (进程项规范排版与搜索过滤)**：进程行展示加粗名称、端口 Badge（非零端口与纯客户端灰标区分）、宿主 Agent 与关联模块；支持输入关键字实时模糊匹配进程名、端口或组名，匹配时自动保持父节点展开。
- [x] **AC-6 (质量门禁与全绿通过)**：编写完整的 Vitest 单元测试用例，`make test` 与 `make lint` 100% 零警告通过。

## 3. TDD 先行测试计划 (`frontend/src/__tests__/RecordingStartDrawer.test.tsx`)
1. **TC-REC-DRAWER-01**：验证抽屉渲染、卡片式作用域切换与批次名称双向绑定。
2. **TC-REC-DRAWER-02**：验证 Agent 下拉选择器、自动默认选中在线空闲 Agent、以及离线 Agent 的只读禁用防呆。
3. **TC-REC-DRAWER-03**：验证业务进程树的组级全选、取消、部分勾选触发的组 `indeterminate` 状态。
4. **TC-REC-DRAWER-04**：验证树顶端关键字搜索过滤（输入 `9001` 或 `sut` 仅显示命中进程，且匹配父组自动展开）。
5. **TC-REC-DRAWER-05**：验证表单提交拦截（空名称、未勾选进程）以及成功启动时的入参组装。
6. **TC-REC-DRAWER-06**：验证暗色与亮色模式下的 Token 引用合规性，严禁写死纯色值。

## 4. 实施命令清单
```bash
# 1. 运行前端单测
npm --prefix frontend test src/__tests__/RecordingStartDrawer.test.tsx

# 2. 全量质量门禁验证
make test && make lint && make build
```
