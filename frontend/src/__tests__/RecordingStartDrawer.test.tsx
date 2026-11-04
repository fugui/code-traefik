import { describe, it, expect, vi } from 'vitest';
import { renderToString } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import type { Agent, Process } from '../types';
import {
  RecordingStartContent,
  computeSmartDefaultSelection,
} from '../pages/RecordingWorkbenchPage/RecordingStartDrawer';

describe('RecordingStartDrawer TDD Test Suite', () => {
  const mockAgents: Agent[] = [
    {
      id: 'agent-1',
      name: 'demo-local-agent',
      endpoint: '192.168.56.18:8091',
      scope: 'SHARED',
      status: 'ONLINE',
      lease_type: '',
    },
    {
      id: 'agent-2',
      name: 'staging-worker-02',
      endpoint: '192.168.56.22:8091',
      scope: 'SHARED',
      status: 'OFFLINE',
      lease_type: '',
    },
    {
      id: 'agent-3',
      name: 'busy-worker-03',
      endpoint: '192.168.56.23:8091',
      scope: 'SHARED',
      status: 'ONLINE',
      lease_type: 'RECORDING',
      lease_holder_name: '张三',
    },
  ];

  const mockProcesses: Process[] = [
    {
      id: 'p1',
      agent_id: 'agent-1',
      group_name: '核心业务组',
      name: 'sut-order',
      listen_port: 9001,
      process_modules: [
        {
          process_id: 'p1',
          module_id: 'm1',
          module: { id: 'm1', code: 'order', name: '订单结算服务' },
        },
      ],
    },
    {
      id: 'p2',
      agent_id: 'agent-1',
      group_name: '支撑服务组',
      name: 'backend-account',
      listen_port: 9002,
    },
    {
      id: 'p3',
      agent_id: 'agent-1',
      group_name: '支撑服务组',
      name: 'backend-inventory',
      listen_port: 9003,
    },
    {
      id: 'p4',
      agent_id: 'agent-1',
      group_name: '客户端',
      name: 'CLI',
      listen_port: 0, // 纯客户端
    },
    {
      id: 'p5',
      agent_id: 'agent-2',
      group_name: '远程网关组',
      name: 'gateway-proxy',
      listen_port: 8080,
    },
  ];

  it('TC-REC-DRAWER-01: 抽屉内容渲染、双卡片作用域与批次输入解挤', () => {
    const html = renderToString(
      <RecordingStartContent
        onClose={vi.fn()}
        onSuccess={vi.fn()}
        allProcesses={mockProcesses}
        allAgents={mockAgents}
        isAdmin={true}
      />
    );

    // 抽屉容器类
    expect(html).toContain('code-recording-start-drawer');
    // 双卡片单选解挤
    expect(html).toContain('个人私有素材');
    expect(html).toContain('PRIVATE');
    expect(html).toContain('团队公共基线');
    expect(html).toContain('SHARED');
    // 批次名称
    expect(html).toContain('批次名称');
    expect(html).toContain('自动生成名称');
  });

  it('TC-REC-DRAWER-02: 宿主 Agent 优先自动默认选中首个在线且空闲 Agent', () => {
    const html = renderToString(
      <RecordingStartContent
        onClose={vi.fn()}
        onSuccess={vi.fn()}
        allProcesses={mockProcesses}
        allAgents={mockAgents}
        isAdmin={false}
      />
    );

    // 应该包含 Agent 下拉选项
    expect(html).toContain('demo-local-agent');
    expect(html).toContain('192.168.56.18:8091');
    expect(html).toContain('🟢 在线 / 空闲');
  });

  it('TC-REC-DRAWER-03: 业务进程树按 GroupName 分组并展示组级复选与统计', () => {
    const html = renderToString(
      <RecordingStartContent
        onClose={vi.fn()}
        onSuccess={vi.fn()}
        allProcesses={mockProcesses}
        allAgents={mockAgents}
        isAdmin={true}
      />
    );

    // 进程组展示
    expect(html).toContain('核心业务组');
    expect(html).toContain('支撑服务组');
    expect(html).toContain('客户端');
    // 进程信息与端口 Badge
    expect(html).toContain('sut-order');
    expect(html).toContain('9001');
    expect(html).toContain('backend-account');
    expect(html).toContain('9002');
    expect(html).toContain('纯客户端');
    // 不应展示 agent-2 上的进程（因为默认选中 agent-1）
    expect(html).not.toContain('gateway-proxy');
  });

  it('TC-REC-DRAWER-04: 离线或租约占用 Agent 应具备防呆门禁提示', () => {
    const offlineAgents: Agent[] = [
      {
        id: 'agent-off',
        name: 'offline-agent',
        endpoint: '127.0.0.1:8091',
        scope: 'SHARED',
        status: 'OFFLINE',
        lease_type: '',
      },
    ];

    const html = renderToString(
      <RecordingStartContent
        onClose={vi.fn()}
        onSuccess={vi.fn()}
        allProcesses={mockProcesses}
        allAgents={offlineAgents}
        isAdmin={false}
      />
    );

    expect(html).toContain('offline-agent');
    expect(html).toContain('离线');
    expect(html).toContain('无法下发 eBPF 探针租约');
  });

  it('TC-REC-DRAWER-05: 底部 Sticky Footer 包含已选进程数与操作按钮', () => {
    const html = renderToString(
      <RecordingStartContent
        onClose={vi.fn()}
        onSuccess={vi.fn()}
        allProcesses={mockProcesses}
        allAgents={mockAgents}
        isAdmin={true}
      />
    );

    expect(html).toContain('已选');
    expect(html).toContain('立即启动录制');
    expect(html).toContain('btn-confirm-start-recording');
  });

  it('TC-REC-DRAWER-06: 租约占用 Agent 应展示占用人防呆警告', () => {
    const busyAgents: Agent[] = [
      {
        id: 'agent-busy',
        name: 'busy-worker',
        endpoint: '192.168.56.24:8091',
        scope: 'SHARED',
        status: 'ONLINE',
        lease_type: 'RECORDING',
        lease_holder_name: '李四',
      },
    ];

    const html = renderToString(
      <RecordingStartContent
        onClose={vi.fn()}
        onSuccess={vi.fn()}
        allProcesses={mockProcesses}
        allAgents={busyAgents}
        isAdmin={false}
      />
    );

    expect(html).toContain('busy-worker');
    expect(html).toContain('RECORDING');
    expect(html).toContain('租用人: 李四');
    expect(html).toContain('暂不可发起并发录制');
  });

  it('TC-REC-DRAWER-07: 单/少进程 (1..5) 智能默认全选，大于 5 进程仅默认勾选 READY 探针', () => {
    // 1. 少于等于 5 个进程时，默认全选可用进程
    const procs5: Process[] = [
      { id: 'p1', agent_id: 'agent-1', name: 'p1', listen_port: 8001, probe_status: 'WAITING_PROC' },
      { id: 'p2', agent_id: 'agent-1', name: 'p2', listen_port: 8002, probe_status: 'READY' },
      { id: 'p3', agent_id: 'agent-1', name: 'p3', listen_port: 8003, probe_status: 'NEED_RECONNECT' },
    ];
    const sel5 = computeSmartDefaultSelection(procs5);
    expect(sel5).toEqual(['p1', 'p2', 'p3']);

    // 2. 超过 5 个进程时，仅默认勾选 probe_status === 'READY'
    const procs7: Process[] = [
      { id: 'p1', agent_id: 'agent-1', name: 'p1', listen_port: 8001, probe_status: 'WAITING_PROC' },
      { id: 'p2', agent_id: 'agent-1', name: 'p2', listen_port: 8002, probe_status: 'READY' },
      { id: 'p3', agent_id: 'agent-1', name: 'p3', listen_port: 8003, probe_status: 'READY' },
      { id: 'p4', agent_id: 'agent-1', name: 'p4', listen_port: 8004, probe_status: 'NEED_RECONNECT' },
      { id: 'p5', agent_id: 'agent-1', name: 'p5', listen_port: 8005, probe_status: 'WAITING_PROC' },
      { id: 'p6', agent_id: 'agent-1', name: 'p6', listen_port: 8006, probe_status: 'READY' },
      { id: 'p7', agent_id: 'agent-1', name: 'p7', listen_port: 8007, probe_status: 'ERROR' },
    ];
    const sel7 = computeSmartDefaultSelection(procs7);
    expect(sel7).toEqual(['p2', 'p3', 'p6']);

    // 3. 抽屉内容渲染验证：1..5 个进程时底部直接显示已选 4 个（mockProcesses 中 agent-1 有 4 个进程）
    const html = renderToString(
      <MemoryRouter>
        <RecordingStartContent
          onClose={vi.fn()}
          onSuccess={vi.fn()}
          allProcesses={mockProcesses}
          allAgents={mockAgents}
          isAdmin={true}
        />
      </MemoryRouter>
    );
    expect(html).toContain('已选');
    expect(html).toContain('本次采集目标进程（从已挂载进程中圈定范围 · 默认同时录制入口 INGRESS 与出口 EGRESS）');
  });

  it('TC-REC-DRAWER-08: DISARMED 探针进程置灰禁选并展示专属提示 Badge', () => {
    const disarmedProcs: Process[] = [
      {
        id: 'p-disarmed',
        agent_id: 'agent-1',
        name: 'legacy-service',
        listen_port: 8089,
        probe_desired_state: 'DISARMED',
        probe_status: 'DISARMED',
      },
    ];

    const sel = computeSmartDefaultSelection(disarmedProcs);
    expect(sel).toEqual([]);

    const html = renderToString(
      <MemoryRouter>
        <RecordingStartContent
          onClose={vi.fn()}
          onSuccess={vi.fn()}
          allProcesses={disarmedProcs}
          allAgents={mockAgents}
          isAdmin={true}
        />
      </MemoryRouter>
    );

    expect(html).toContain('probe-badge-disarmed');
    expect(html).toContain('⚪ DISARMED');
    expect(html).toContain('disabled=""');
    expect(html).toContain('探针已被卸载，需在 Agent 管理中重新挂载');
  });

  it('TC-REC-DRAWER-09: 空挂载进程 Agent 渲染引导卡片与 [前往挂载架构进程 →] 深链按钮', () => {
    const html = renderToString(
      <MemoryRouter>
        <RecordingStartContent
          onClose={vi.fn()}
          onSuccess={vi.fn()}
          allProcesses={[]}
          allAgents={mockAgents}
          isAdmin={true}
        />
      </MemoryRouter>
    );

    expect(html).toContain('data-testid="empty-processes-guide"');
    expect(html).toContain('该 Agent 暂未挂载任何架构物理进程，无法圈定出入站采集范围');
    expect(html).toContain('data-testid="goto-mount-process-btn"');
    expect(html).toContain('前往挂载架构进程 →');
  });

  it('TC-REC-DRAWER-10: 工具栏 [全选可用]、[仅选就绪]、[🔗 勾选关联下游] 与 [清空]', () => {
    const html = renderToString(
      <MemoryRouter>
        <RecordingStartContent
          onClose={vi.fn()}
          onSuccess={vi.fn()}
          allProcesses={mockProcesses}
          allAgents={mockAgents}
          isAdmin={true}
        />
      </MemoryRouter>
    );

    expect(html).toContain('data-testid="select-all-available-btn"');
    expect(html).toContain('全选可用');
    expect(html).toContain('data-testid="select-only-ready-btn"');
    expect(html).toContain('仅选就绪');
    expect(html).toContain('data-testid="select-cascade-downstream-btn"');
    expect(html).toContain('🔗 勾选关联下游');
    expect(html).toContain('data-testid="clear-all-selection-btn"');
    expect(html).toContain('清空');
  });
});
