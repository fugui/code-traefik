import { describe, it, expect, vi } from 'vitest';
import { renderToString } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import { SchemaPoolSummaryBar } from '../pages/ProtoSchemaWorkbenchPage/SchemaPoolSummaryBar';
import { SchemaAssetLedgerTable } from '../pages/ProtoSchemaWorkbenchPage/SchemaAssetLedgerTable';
import { SchemaConflictContent } from '../pages/ProtoSchemaWorkbenchPage/SchemaConflictModal';
import { SchemaAssetInspectContent } from '../pages/ProtoSchemaWorkbenchPage/SchemaAssetInspectDrawer';
import type {
  ProtoPoolSummaryDTO,
  ProtoSchemaLedgerItemDTO,
  ConflictOrBrokenResponse,
} from '../types';

vi.mock('../utils/toast', () => ({
  useToast: () => ({
    success: vi.fn(),
    error: vi.fn(),
    info: vi.fn(),
    warning: vi.fn(),
  }),
}));

vi.mock('../api/schema', () => ({
  getProtoSchemaSummary: vi.fn().mockResolvedValue({
    schema: { id: 1, name: 'trade-service', version: 2, is_active: true },
    proto_files: ['trade/order.proto'],
    services_summary: [
      {
        service_name: 'trade.OrderService',
        proto_file: 'trade/order.proto',
        proto_package: 'trade',
        proto_service: 'OrderService',
        is_claimed: true,
        claimed_module_name: '订单中心',
        operation_count: 3,
        unary_count: 3,
        stream_count: 0,
      },
    ],
    claimed_ratio: 1.0,
  }),
  listProtoSchemas: vi.fn().mockResolvedValue({
    items: [
      {
        id: 2,
        name: 'trade-service',
        version: 2,
        version_tag: 'v2.0.0',
        category: 'BUSINESS',
        file_name: 'trade_v2.pb',
        file_size: 40960,
        is_active: true,
        operation_count: 3,
      },
      {
        id: 1,
        name: 'trade-service',
        version: 1,
        version_tag: 'v1.0.0',
        category: 'BUSINESS',
        file_name: 'trade_v1.pb',
        file_size: 30720,
        is_active: false,
        operation_count: 2,
      },
    ],
  }),
  downloadProtoSchema: vi.fn(),
}));

describe('SchemaAssetLedgerWorkbench Suite (M11-P3)', () => {
  const mockPoolSummary: ProtoPoolSummaryDTO = {
    active_schema_count: 12,
    active_base_lib_count: 2,
    active_service_count: 28,
    active_operation_count: 156,
    claimed_service_count: 25,
    orphan_service_count: 3,
    claimed_ratio: 0.89,
    unresolved_dep_count: 1,
  };

  const mockLedgerItems: ProtoSchemaLedgerItemDTO[] = [
    {
      id: 10,
      name: 'common-base',
      version: 1,
      version_tag: 'v1.0.0',
      category: 'BASE_LIB',
      file_name: 'common_base_v1.pb',
      file_size: 15360,
      is_active: true,
      proto_file_count: 3,
      service_count: 0,
      operation_count: 0,
      proto_files: ['common/base.proto', 'common/money.proto'],
      external_imports: [],
      description: '全系统纯公共数据类型底座',
      created_by: 1,
      created_by_name: 'admin',
      created_at: '2026-11-01T10:00:00Z',
      updated_at: '2026-11-01T10:00:00Z',
      version_count: 1,
      latest_version: 1,
      active_version: 1,
      claimed_service_count: 0,
      claimed_ratio: 1.0,
      resolved_dependencies: [],
    },
    {
      id: 20,
      name: 'trade-service',
      version: 2,
      version_tag: 'v2.1.0',
      category: 'BUSINESS',
      file_name: 'trade_v2.pb',
      file_size: 61440,
      is_active: true,
      proto_file_count: 2,
      service_count: 2,
      operation_count: 8,
      proto_files: ['trade/order.proto', 'trade/pay.proto'],
      external_imports: ['common/base.proto'],
      description: '交易域核心微服务',
      created_by: 1,
      created_by_name: 'admin',
      created_at: '2026-11-02T12:00:00Z',
      updated_at: '2026-11-02T12:00:00Z',
      version_count: 2,
      latest_version: 2,
      active_version: 2,
      claimed_service_count: 2,
      claimed_ratio: 1.0,
      resolved_dependencies: [
        {
          schema_id: 10,
          schema_name: 'common-base',
          version: 1,
          category: 'BASE_LIB',
          matched_protos: ['common/base.proto'],
          status: 'READY',
        },
      ],
    },
    {
      id: 30,
      name: 'risk-gateway',
      version: 1,
      version_tag: 'v1.0-draft',
      category: 'GATEWAY',
      file_name: 'risk_gw_v1.pb',
      file_size: 24576,
      is_active: false,
      proto_file_count: 1,
      service_count: 1,
      operation_count: 4,
      proto_files: ['risk/gateway.proto'],
      external_imports: ['risk/rules.proto'],
      description: '风控网关描述集草稿',
      created_by: 2,
      created_by_name: 'developer',
      created_at: '2026-11-02T14:00:00Z',
      updated_at: '2026-11-02T14:00:00Z',
      version_count: 1,
      latest_version: 1,
      claimed_service_count: 0,
      claimed_ratio: 0.0,
      resolved_dependencies: [
        {
          schema_name: 'risk-rules',
          matched_protos: ['risk/rules.proto'],
          status: 'MISSING',
        },
      ],
    },
  ];

  it('renders SchemaPoolSummaryBar metrics and warning badges', () => {
    const html = renderToString(
      <SchemaPoolSummaryBar
        summary={mockPoolSummary}
        isAdmin={true}
        onUploadClick={vi.fn()}
      />
    );

    // 1. 验证全局描述集与基础库数量展示
    expect(html).toContain('12');
    expect(html).toContain('个 (.pb)');
    expect(html).toContain('2');
    expect(html).toContain('个基础库');

    // 2. 验证契约规模与认领率
    expect(html).toContain('28');
    expect(html).toContain('156');
    expect(html).toContain('89%');

    // 3. 验证未解析依赖与孤儿服务告警徽章
    expect(html).toContain('1 项缺失外部依赖');
    expect(html).toContain('3 个未认领服务');

    // 4. 验证管理员上传入口
    expect(html).toContain('上传描述集 (.pb)');
  });

  it('renders SchemaAssetLedgerTable category tabs, badges and operations', () => {
    const html = renderToString(
      <MemoryRouter>
        <SchemaAssetLedgerTable
          schemas={mockLedgerItems}
          total={3}
          page={1}
          pageSize={25}
          onPageChange={vi.fn()}
          onPageSizeChange={vi.fn()}
          selectedCategory=""
          onCategoryChange={vi.fn()}
          activeStatusFilter="ALL"
          onStatusFilterChange={vi.fn()}
          searchKeyword=""
          onSearchChange={vi.fn()}
          loading={false}
          isAdmin={true}
          onInspect={vi.fn()}
          onUploadNewVersion={vi.fn()}
          onActivate={vi.fn()}
          onDeactivate={vi.fn()}
          onBatchActivate={vi.fn()}
          onDelete={vi.fn()}
          onDownload={vi.fn()}
        />
      </MemoryRouter>
    );

    // 1. 验证分类胶囊
    expect(html).toContain('🧱 基础库');
    expect(html).toContain('📦 业务域');
    expect(html).toContain('🌐 网关');

    // 2. 验证名称与版本
    expect(html).toContain('common-base');
    expect(html).toContain('trade-service');
    expect(html).toContain('risk-gateway');
    expect(html).toContain('(共 2 版)');

    // 3. 验证跨包依赖胶囊
    expect(html).toContain('独立无外部依赖');
    expect(html).toContain('✅ 1 项就绪');
    expect(html).toContain('⚠️ 缺 1 项依赖');

    // 4. 验证操作按钮
    expect(html).toContain('🔍 透视');
    expect(html).toContain('⬆️ 新版');
  });

  it('renders SchemaConflictModal for 409 method overlap conflict', () => {
    const conflictData: ConflictOrBrokenResponse = {
      error: '描述集方法定义与当前生效池发生冲突',
      conflicts: [
        {
          service_name: 'trade.OrderService',
          operation: 'CreateOrder',
          conflict_schema_name: 'legacy-trade',
          conflict_version: 1,
        },
      ],
    };

    const html = renderToString(
      <SchemaConflictContent
        onClose={vi.fn()}
        conflictData={conflictData}
      />
    );

    expect(html).toContain('冲突方法列表');
    expect(html).toContain('trade.OrderService');
    expect(html).toContain('CreateOrder');
    expect(html).toContain('legacy-trade');
    expect(html).toContain('v1');
    expect(html).toContain('我知道了');
  });

  it('renders SchemaConflictModal for 409 broken dependent closure', () => {
    const brokenData: ConflictOrBrokenResponse = {
      error: '停用基础库导致下游依赖闭包破坏',
      broken_dependents: [
        {
          schema_id: 20,
          schema_name: 'trade-service',
          version: 2,
          reason: '无法解析基础消息 [common.BaseHeader]',
        },
      ],
    };

    const html = renderToString(
      <SchemaConflictContent
        onClose={vi.fn()}
        conflictData={brokenData}
      />
    );

    expect(html).toContain('受影响破坏的下游描述集');
    expect(html).toContain('trade-service');
    expect(html).toContain('v2');
    expect(html).toContain('无法解析基础消息 [common.BaseHeader]');
  });

  it('renders SchemaAssetInspectDrawer tabs and metadata', () => {
    const html = renderToString(
      <SchemaAssetInspectContent
        schema={mockLedgerItems[1]}
        isAdmin={true}
        onActivate={vi.fn()}
        onDeactivate={vi.fn()}
        onDelete={vi.fn()}
        onUploadNewVersion={vi.fn()}
        onNavigateToCatalog={vi.fn()}
      />
    );

    // 1. 验证抽屉标题与元信息
    expect(html).toContain('描述集资产透视: trade-service');
    expect(html).toContain('trade_v2.pb');

    // 2. 验证三个子面板 Tab
    expect(html).toContain('包含源文件与服务');
    expect(html).toContain('跨包依赖关系');
    expect(html).toContain('历史版本与回滚');

    // 3. 验证源文件列表
    expect(html).toContain('trade/order.proto');
    expect(html).toContain('trade/pay.proto');
    expect(html).toContain('在全局服务目录中查看 ➔');
  });
});
