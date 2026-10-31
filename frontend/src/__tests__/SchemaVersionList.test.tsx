import { describe, it, expect, vi } from 'vitest';
import { renderToString } from 'react-dom/server';
import { SchemaVersionList } from '../pages/ProtoSchemaWorkbenchPage/SchemaVersionList';
import type { ProtoSchema, ProtoSchemaSummaryDTO } from '../types';

// Mock toast
vi.mock('../utils/toast', () => ({
  useToast: () => ({
    success: vi.fn(),
    error: vi.fn(),
    info: vi.fn(),
    warning: vi.fn(),
  }),
}));

describe('SchemaVersionList Suite (M7-P4)', () => {
  const mockSchemas: ProtoSchema[] = [
    {
      id: '101',
      name: 'TradeGatewayIDL',
      version: 2,
      version_tag: 'v2.1.0',
      file_name: 'trade_v2.pb',
      file_sha256: 'abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890',
      is_active: true,
      operation_count: 5,
      proto_files: ['order.proto', 'account.proto', 'common.proto'],
    },
    {
      id: '100',
      name: 'TradeGatewayIDL',
      version: 1,
      version_tag: 'v1.0.0',
      file_name: 'trade_v1.pb',
      is_active: false,
      operation_count: 3,
      proto_files: ['order.proto'],
    },
  ];

  const mockSummary: ProtoSchemaSummaryDTO = {
    schema: mockSchemas[0],
    proto_files: ['account.proto', 'common.proto', 'order.proto'],
    claimed_ratio: 0.8,
    services_summary: [
      {
        service_name: 'dophi.order.v1.OrderService',
        proto_file: 'order.proto',
        proto_package: 'dophi.order.v1',
        proto_service: 'OrderService',
        claimed_module_id: 1,
        claimed_module_name: 'OrderCore',
        claimed_subsystem_name: 'TradeSystem',
        is_claimed: true,
        operation_count: 3,
        unary_count: 3,
        stream_count: 0,
      },
      {
        service_name: 'dophi.account.v1.AccountService',
        proto_file: 'account.proto',
        proto_package: 'dophi.account.v1',
        proto_service: 'AccountService',
        is_claimed: false, // 孤儿服务
        operation_count: 2,
        unary_count: 1,
        stream_count: 1,
      },
    ],
  };

  it('renders download button and sorted proto files', () => {
    const html = renderToString(
      <SchemaVersionList
        schemas={mockSchemas}
        selectedSchemaId="101"
        selectedSummary={mockSummary}
        onSelectSchema={vi.fn()}
        isAdmin={true}
        onActivate={vi.fn()}
        onDelete={vi.fn()}
      />
    );

    // 1. 验证导出 .pb 按钮
    expect(html).toContain('📥 导出 .pb');

    // 2. 验证 IDL 源文件展示 (按字母排序 account.proto, common.proto, order.proto)
    expect(html).toContain('IDL 源文件 (3):');
    expect(html).toContain('account.proto');
    expect(html).toContain('common.proto');
    expect(html).toContain('order.proto');
    const idxAccount = html.indexOf('account.proto');
    const idxCommon = html.indexOf('common.proto');
    const idxOrder = html.indexOf('order.proto');
    expect(idxAccount).toBeLessThan(idxCommon);
    expect(idxCommon).toBeLessThan(idxOrder);
  });

  it('renders architecture bloodline claim ratio and orphan service diagnosis', () => {
    const html = renderToString(
      <SchemaVersionList
        schemas={mockSchemas}
        selectedSchemaId="101"
        selectedSummary={mockSummary}
        onSelectSchema={vi.fn()}
        isAdmin={true}
        onActivate={vi.fn()}
        onDelete={vi.fn()}
      />
    );

    // 1. 验证架构认领覆盖百分比
    expect(html).toContain('80% 已认领');

    // 2. 验证未认领孤儿服务告警与未认领服务名提示
    expect(html).toContain('1 个未认领服务');
    expect(html).toContain('dophi.account.v1.AccountService');
    expect(html).toContain('建议前往拓扑图进行服务认领');
  });

  it('renders 100% all claimed when no orphan service exists', () => {
    const allClaimedSummary: ProtoSchemaSummaryDTO = {
      ...mockSummary,
      claimed_ratio: 1.0,
      services_summary: [
        {
          ...mockSummary.services_summary[0],
          is_claimed: true,
        },
      ],
    };

    const html = renderToString(
      <SchemaVersionList
        schemas={mockSchemas}
        selectedSchemaId="101"
        selectedSummary={allClaimedSummary}
        onSelectSchema={vi.fn()}
        isAdmin={true}
        onActivate={vi.fn()}
        onDelete={vi.fn()}
      />
    );

    expect(html).toContain('100% 已认领');
    expect(html).toContain('契约已全部血缘认领');
  });
});
