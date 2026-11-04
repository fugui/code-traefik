import { describe, it, expect } from 'vitest';
import { renderToString } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import { AgentProcessMountContent } from '../pages/AgentManagementPage/AgentProcessMountDrawer';
import { AgentManagementPage } from '../pages/AgentManagementPage/AgentManagementPage';
import { RecordingStartContent } from '../pages/RecordingWorkbenchPage/RecordingStartDrawer';
import { ModuleUserProvider } from '../context/ModuleUserContext';
import type {
  Agent,
  Process,
  ArchProcessGroup,
  CurrentUserProfile,
} from '../types';

describe('AgentProcessMountAndReadiness Test Suite', () => {
  const dummyAgent: Agent = {
    id: 'agent-101',
    name: 'prod-gateway-node-01',
    endpoint: 'http://192.168.56.18:9091',
    scope: 'SHARED',
    status: 'ONLINE',
    mounts_revision: 3,
    fallback_agent_id: 99,
    lease_type: '',
  };

  const dummyProcesses: Process[] = [
    {
      id: 'p1',
      agent_id: 'agent-101',
      arch_process_id: 1,
      name: 'proc-account-srv',
      listen_port: 8080,
      effective_listen_port: 8080,
      probe_desired_state: 'ARMED',
      probe_status: 'READY',
      advertise_ip: '192.168.56.18',
    },
    {
      id: 'p2',
      agent_id: 'agent-101',
      arch_process_id: 2,
      name: 'proc-order-srv',
      listen_port: 8081,
      effective_listen_port: 8081,
      override_listen_port: 8081,
      probe_desired_state: 'ARMED',
      probe_status: 'NEED_RECONNECT',
      advertise_ip: '192.168.56.18',
    },
    {
      id: 'p3',
      agent_id: 'agent-101',
      arch_process_id: 3,
      name: 'proc-settle-srv',
      listen_port: 8082,
      effective_listen_port: 8082,
      probe_desired_state: 'ARMED',
      probe_status: 'WAITING_PROC',
      advertise_ip: '192.168.56.18',
    },
  ];

  const adminUser: CurrentUserProfile = {
    id: 1,
    username: 'admin',
    name: '系统管理员',
    roles: ['traefik_admin'],
    required_ad_role: 'traefik_admin',
    is_module_admin: true,
    is_super_admin: false,
  };

  it('renders AgentProcessMountContent with agent context and scan/match action', () => {
    const dummyArchGroups: ArchProcessGroup[] = [
      {
        id: 1,
        code: 'ORDER_GRP',
        name: '订单域进程组',
        sort_order: 1,
        processes: [
          {
            id: 1,
            group_id: 1,
            code: 'proc-account-srv',
            name: '账户服务进程',
            default_listen_port: 8080,
            default_cmd_pattern: 'proc-account-srv',
            description: '',
            sort_order: 1,
          },
          {
            id: 2,
            group_id: 1,
            code: 'proc-order-srv',
            name: '订单服务进程',
            default_listen_port: 8081,
            default_cmd_pattern: 'proc-order-srv',
            description: '',
            sort_order: 2,
          },
        ],
      },
    ];

    const html = renderToString(
      <AgentProcessMountContent
        agent={dummyAgent}
        existingProcesses={dummyProcesses}
        initialArchGroups={dummyArchGroups}
        onSuccess={() => {}}
      />
    );

    expect(html).toContain('data-testid="agent-process-mount-drawer"');
    expect(html).toContain('data-testid="auto-scan-procs-btn"');
    expect(html).toContain('⚡ 自动扫描宿主机进程并匹配架构树');
    expect(html).toContain('data-testid="save-mounts-btn"');
    expect(html).toContain('确认全量挂载并同步探针');
    expect(html).toContain('订单域进程组');
    expect(html).toContain('账户服务进程');
  });

  it('renders AgentManagementPage with mounts_revision, fallback_agent, batch mount button and 7-state badges', () => {
    const html = renderToString(
      <MemoryRouter initialEntries={['/agents?scope=SHARED&agent_id=agent-101']}>
        <ModuleUserProvider
          value={{
            user: adminUser,
            loadingUser: false,
            isEmbedded: false,
            reloadUser: async () => {},
          }}
        >
          <AgentManagementPage initialAgent={dummyAgent} initialProcesses={dummyProcesses} />
        </ModuleUserProvider>
      </MemoryRouter>
    );

    // Header actions & right pane details
    expect(html).toContain('公私域 Agent 纳管与排他租约控制台');
    expect(html).toContain('团队公共 Agent');
    expect(html).toContain('mounts-revision-badge');
    expect(html).toContain('fallback-agent-badge');
  });

  it('renders RecordingStartDrawer with 7-state badges, zero-restart banner, and need-reconnect warning', () => {
    // 1. All selected processes are READY -> zero-restart instant record
    const readyProcesses: Process[] = [
      {
        id: 'p-ready-1',
        agent_id: 'agent-101',
        name: 'svc-alpha',
        listen_port: 8080,
        probe_status: 'READY',
      },
      {
        id: 'p-ready-2',
        agent_id: 'agent-101',
        name: 'svc-beta',
        listen_port: 8081,
        probe_status: 'READY',
      },
    ];

    const htmlReady = renderToString(
      <MemoryRouter initialEntries={['/record?agent_id=agent-101&auto_start=1&session_no=REC-TEST-001']}>
        <RecordingStartContent
          allProcesses={readyProcesses}
          allAgents={[dummyAgent]}
          isAdmin={true}
          onClose={() => {}}
          onSuccess={() => {}}
        />
      </MemoryRouter>
    );

    expect(htmlReady).toContain('data-testid="zero-restart-ready-banner"');
    expect(htmlReady).toContain('⚡ 零重启即刻录制就绪');
    expect(htmlReady).toContain('probe-badge-ready');
    expect(htmlReady).toContain('🟢 READY');

    // 2. Contains NEED_RECONNECT process -> long connection warning banner
    const mixedProcesses: Process[] = [
      {
        id: 'p-ready-1',
        agent_id: 'agent-101',
        name: 'svc-alpha',
        listen_port: 8080,
        probe_status: 'READY',
      },
      {
        id: 'p-reconnect-2',
        agent_id: 'agent-101',
        name: 'svc-beta-reconnect',
        listen_port: 8081,
        probe_status: 'NEED_RECONNECT',
      },
    ];

    const htmlMixed = renderToString(
      <MemoryRouter initialEntries={['/record?agent_id=agent-101&auto_start=1&session_no=REC-TEST-002']}>
        <RecordingStartContent
          allProcesses={mixedProcesses}
          allAgents={[dummyAgent]}
          isAdmin={true}
          onClose={() => {}}
          onSuccess={() => {}}
        />
      </MemoryRouter>
    );

    expect(htmlMixed).toContain('data-testid="need-reconnect-warning"');
    expect(htmlMixed).toContain('⚠️ 存在历史长连接警示');
    expect(htmlMixed).toContain('probe-badge-need-reconnect');
    expect(htmlMixed).toContain('🟡 NEED_RECONNECT');
  });

  it('testTwoWorkbenches_SmartDefaultSelection_CascadeDownstream_AndBidirectionalDeepLinks', () => {
    // 1. /agents 页面渲染常驻待命消歧文案与 [🎬 发起录制] 按钮
    const agentHtml = renderToString(
      <MemoryRouter initialEntries={['/agents?scope=SHARED&agent_id=agent-101']}>
        <ModuleUserProvider
          value={{
            user: adminUser,
            loadingUser: false,
            isEmbedded: false,
            reloadUser: async () => {},
          }}
        >
          <AgentManagementPage initialAgent={dummyAgent} initialProcesses={dummyProcesses} />
        </ModuleUserProvider>
      </MemoryRouter>
    );

    expect(agentHtml).toContain('环境进程部署映射与常驻待命探针（配置一次 · 零损耗待命）');
    expect(agentHtml).toContain('data-testid="start-record-btn"');
    expect(agentHtml).toContain('🎬 发起录制');

    // 2. /record?auto_start=1&agent_id=agent-101 验证抽屉自动唤起、文案与单进程默认全选
    const singleProc: Process[] = [
      {
        id: 'p-order-single',
        agent_id: 'agent-101',
        name: 'sut-order',
        listen_port: 9001,
        probe_status: 'READY',
      },
    ];

    const drawerHtml = renderToString(
      <MemoryRouter initialEntries={['/record?agent_id=agent-101&auto_start=1&session_no=REC-M12-01']}>
        <RecordingStartContent
          allProcesses={singleProc}
          allAgents={[dummyAgent]}
          isAdmin={true}
          onClose={() => {}}
          onSuccess={() => {}}
        />
      </MemoryRouter>
    );

    expect(drawerHtml).toContain('本次采集目标进程（从已挂载进程中圈定范围 · 默认同时录制入口 INGRESS 与出口 EGRESS）');
    expect(drawerHtml).toContain('已选');
    expect(drawerHtml).toContain('sut-order');
    expect(drawerHtml).toContain('9001');

    // 3. 6 个进程大环境下验证智能默认预选（仅预选 READY 探针，DISARMED 禁选）
    const largeEnvProcs: Process[] = [
      { id: 'p1', agent_id: 'agent-101', name: 'p1', listen_port: 9001, probe_status: 'READY' },
      { id: 'p2', agent_id: 'agent-101', name: 'p2', listen_port: 9002, probe_status: 'WAITING_PROC' },
      { id: 'p3', agent_id: 'agent-101', name: 'p3', listen_port: 9003, probe_status: 'READY' },
      { id: 'p4', agent_id: 'agent-101', name: 'p4', listen_port: 9004, probe_status: 'NEED_RECONNECT' },
      { id: 'p5', agent_id: 'agent-101', name: 'p5', listen_port: 9005, probe_status: 'ERROR' },
      { id: 'p6', agent_id: 'agent-101', name: 'p6', listen_port: 9006, probe_status: 'READY' },
      { id: 'p7', agent_id: 'agent-101', name: 'p7', listen_port: 9007, probe_status: 'DISARMED', probe_desired_state: 'DISARMED' },
    ];

    const largeDrawerHtml = renderToString(
      <MemoryRouter initialEntries={['/record?agent_id=agent-101&auto_start=1']}>
        <RecordingStartContent
          allProcesses={largeEnvProcs}
          allAgents={[dummyAgent]}
          isAdmin={true}
          onClose={() => {}}
          onSuccess={() => {}}
        />
      </MemoryRouter>
    );

    // 默认仅选中 p1, p3, p6 (3 个 READY 探针)
    expect(largeDrawerHtml).toContain('data-testid="probe-badge-disarmed"');
    expect(largeDrawerHtml).toContain('⚪ DISARMED');
    expect(largeDrawerHtml).toContain('disabled=""');
    expect(largeDrawerHtml).toContain('data-testid="select-cascade-downstream-btn"');

    // 4. 空挂载态点击引导按钮验证跳转链接指示
    const emptyDrawerHtml = renderToString(
      <MemoryRouter initialEntries={['/record?agent_id=agent-101&auto_start=1']}>
        <RecordingStartContent
          allProcesses={[]}
          allAgents={[dummyAgent]}
          isAdmin={true}
          onClose={() => {}}
          onSuccess={() => {}}
        />
      </MemoryRouter>
    );

    expect(emptyDrawerHtml).toContain('data-testid="empty-processes-guide"');
    expect(emptyDrawerHtml).toContain('data-testid="goto-mount-process-btn"');
    expect(emptyDrawerHtml).toContain('前往挂载架构进程 →');
  });
});
