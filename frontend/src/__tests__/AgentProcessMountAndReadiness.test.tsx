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
          <AgentManagementPage />
        </ModuleUserProvider>
      </MemoryRouter>
    );

    // Header actions
    expect(html).toContain('公私域 Agent 纳管与排他租约控制台');
    expect(html).toContain('团队公共 Agent');
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
});
