import { describe, it, expect, vi } from 'vitest';
import { renderToString } from 'react-dom/server';
import type { Agent, Process } from '../types';
import { RecordingStartContent } from '../pages/RecordingWorkbenchPage/RecordingStartDrawer';

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
});
