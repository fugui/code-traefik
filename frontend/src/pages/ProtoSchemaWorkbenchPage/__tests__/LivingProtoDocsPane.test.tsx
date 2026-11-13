import { describe, it, expect, vi } from 'vitest';
import { renderToString } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import { LivingProtoDocsPane } from '../LivingProtoDocsPane';
import { ProtoSchemaWorkbenchPage } from '../ProtoSchemaWorkbenchPage';
import type { ProtoDocTreeResponse, ProtoServiceDocDetailDTO } from '../../../types';

vi.mock('../../../api/schema', () => ({
  listProtoSchemas: vi.fn().mockResolvedValue({ items: [], total: 0, page: 1, page_size: 25 }),
  listProtoServices: vi.fn().mockResolvedValue({ items: [], total: 0, page: 1, page_size: 25 }),
  listProtoOperations: vi.fn().mockResolvedValue({ items: [], total: 0, page: 1, page_size: 25 }),
  getProtoDependenciesGraph: vi.fn().mockResolvedValue({ nodes: [], edges: [] }),
  getProtoDocsTree: vi.fn().mockResolvedValue({ items: [], total_services: 0, total_methods: 0, deprecated_count: 0 }),
  getProtoServiceDocDetail: vi.fn().mockResolvedValue(null),
  exportProtoDocsMarkdown: vi.fn(),
  getProtoSchemaSources: vi.fn().mockResolvedValue({ files: [] }),
  listProtoFiles: vi.fn().mockResolvedValue({ items: [], total: 0, page: 1, page_size: 25 }),
}));

vi.mock('../../../utils/toast', () => ({
  useToast: () => ({
    success: vi.fn(),
    error: vi.fn(),
    info: vi.fn(),
    warning: vi.fn(),
  }),
}));

const mockTree: ProtoDocTreeResponse = {
  total_services: 1,
  total_methods: 1,
  deprecated_count: 1,
  items: [
    {
      node_key: 'sub:10',
      node_type: 'SUBSYSTEM',
      label: '计费结算子系统',
      subsystem_id: 10,
      has_source_info: true,
      method_count: 1,
      deprecated_count: 1,
      children: [
        {
          node_key: 'schema:101',
          node_type: 'SCHEMA',
          label: 'billing-service-pb (v2)',
          schema_id: 101,
          schema_name: 'billing-service-pb',
          category: 'BUSINESS',
          has_source_info: true,
          method_count: 1,
          deprecated_count: 1,
          children: [
            {
              node_key: 'pkg:101:demo.billing.v1',
              node_type: 'PACKAGE',
              label: 'demo.billing.v1',
              schema_id: 101,
              schema_name: 'billing-service-pb',
              has_source_info: true,
              method_count: 1,
              deprecated_count: 1,
              children: [
                {
                  node_key: 'svc:101:demo.billing.v1.BillingService',
                  node_type: 'SERVICE',
                  label: 'BillingService',
                  schema_id: 101,
                  schema_name: 'billing-service-pb',
                  service_name: 'demo.billing.v1.BillingService',
                  summary: '统一实时计费服务',
                  is_deprecated: true,
                  method_count: 1,
                  deprecated_count: 1,
                  has_source_info: true,
                  children: [
                    {
                      node_key: 'method:101:demo.billing.v1.BillingService.Charge',
                      node_type: 'METHOD',
                      label: 'Charge',
                      schema_id: 101,
                      service_name: 'demo.billing.v1.BillingService',
                      operation: 'Charge',
                      summary: '执行实时扣费记账',
                      has_source_info: true,
                    },
                  ],
                },
              ],
            },
          ],
        },
      ],
    },
  ],
};

const mockDetail: ProtoServiceDocDetailDTO = {
  schema_id: 101,
  schema_name: 'billing-service-pb',
  schema_version: 2,
  schema_category: 'BUSINESS',
  source_type: 'ONLINE_COMPILE',
  has_source_info: true,
  package_name: 'demo.billing.v1',
  service_name: 'demo.billing.v1.BillingService',
  short_name: 'BillingService',
  proto_file: 'billing/v1/billing.proto',
  start_line: 42,
  end_line: 55,
  file_comments: '计费模块核心契约文件',
  summary: '统一实时计费服务',
  leading_comments: '统一实时计费服务，承担账户扣款与分账记账。',
  trailing_comments: '',
  is_deprecated: true,
  deprecation_reason: '请迁移至 BillingServiceV2 契约入口',
  owner_tag: 'team-fin',
  error_codes: [
    {
      code: 'INSUFFICIENT_FUNDS',
      http_or_cros_code: '402',
      description: '账户可用余额不足',
    },
  ],
  methods: [
    {
      operation: 'Charge',
      full_name: 'demo.billing.v1.BillingService.Charge',
      proto_file: 'billing/v1/billing.proto',
      start_line: 46,
      end_line: 46,
      summary: '执行实时扣费记账',
      leading_comments: '执行实时扣费记账，保障幂等重试一致。',
      trailing_comments: '',
      is_deprecated: false,
      deprecation_reason: '',
      owner_tag: 'alice.fin',
      error_codes: [
        {
          code: 'DUPLICATE_TX',
          http_or_cros_code: '409',
          description: '幂等流水号冲突',
        },
      ],
      req_message_full_name: 'demo.billing.v1.ChargeReq',
      rsp_message_full_name: 'demo.billing.v1.ChargeRsp',
      client_streaming: false,
      server_streaming: false,
      req_fields: [
        {
          name: 'account_id',
          number: 1,
          type: 'int64',
          label: 'optional',
          comment: '资金账户 ID',
          start_line: 12,
        },
      ],
      rsp_fields: [
        {
          name: 'tx_id',
          number: 1,
          type: 'string',
          label: 'optional',
          comment: '交易流水号',
          start_line: 20,
        },
      ],
      default_req_json: { account_id: '0' },
      default_rsp_json: { tx_id: '' },
      golden_samples: [
        {
          suite_id: 9,
          suite_code: 'GS-BILLING-PROD',
          suite_name: '计费黄金基准集',
          item_id: 33,
          order_no: 1,
          decode_status: 'DECODED_JSON',
          req_json: { account_id: '88001234' },
          rsp_json: { tx_id: 'TX-GOLDEN-991' },
          remark: '人民币标准扣费',
        },
      ],
    },
  ],
  enums: [
    {
      full_name: 'demo.billing.v1.Currency',
      short_name: 'Currency',
      proto_file: 'billing/v1/billing.proto',
      start_line: 4,
      end_line: 8,
      comment: '账单币种枚举',
      is_deprecated: false,
      values: [
        { name: 'CURRENCY_CNY', number: 1, comment: '人民币', is_deprecated: false, start_line: 6 },
      ],
    },
  ],
  referenced_types: [],
  lineage: {
    claim_status: 'CONTRACT',
    definer: {
      contract_id: 7,
      service_name: 'demo.billing.v1.BillingService',
      definer_module_id: 21,
      module_code: 'LIB_BILLING_CONTRACT',
      module_name: '计费公共契约库',
      subsystem_id: 10,
      subsystem_name: '计费结算子系统',
      provide_mode: 'MULTI_IMPL',
      impl_key_label: '地域分区',
    },
    implementations: [
      {
        service_id: 31,
        module_id: 22,
        module_code: 'MOD_BILLING_DOMESTIC',
        module_name: '国内计费实现',
        subsystem_id: 10,
        subsystem_name: '计费结算子系统',
        arch_process_code: 'billing-core-a',
        owner_name: 'alice',
        impl_key: 'cn-east',
        is_custom_service_name: false,
        service_name: 'demo.billing.v1.BillingService',
      },
    ],
    consumers: [
      {
        module_id: 25,
        module_code: 'MOD_ORDER_GW',
        module_name: '订单网关',
        subsystem_id: 10,
        subsystem_name: '计费结算子系统',
        arch_process_code: 'order-gateway',
        dep_type: 'DISPATCH',
      },
    ],
  },
};

describe('M23-P3 LivingProtoDocsPane (/schemas?tab=docs)', () => {
  it('testLivingDocsPane_SearchNav_ContractLineage_AndJumpToSourceLine', () => {
    const html = renderToString(
      <MemoryRouter initialEntries={['/schemas?tab=docs&service_name=demo.billing.v1.BillingService']}>
        <LivingProtoDocsPane
          initialTree={mockTree}
          initialDetail={mockDetail}
          onJumpToSource={vi.fn()}
        />
      </MemoryRouter>
    );

    expect(html).toContain('计费结算子系统');
    expect(html).toContain('BillingService');
    expect(html).toContain('请迁移至 BillingServiceV2 契约入口');
    expect(html).toContain('team-fin');
    expect(html).toContain('INSUFFICIENT_FUNDS');
    expect(html).toContain('DUPLICATE_TX');
    expect(html).toContain('CURRENCY_CNY');
    expect(html).toContain('人民币');
    expect(html).toContain('GS-BILLING-PROD');
    expect(html).toContain('TX-GOLDEN-991');
    expect(html).toContain('billing/v1/billing.proto#L42');
    expect(html).toContain('LIB_BILLING_CONTRACT');
    expect(html).toContain('/topology?focus=mod_22');
    expect(html).toContain('/architecture?tree=logical&amp;module_id=22');
    expect(html).toContain('导出 Markdown');
  });

  it('remaps legacy ?tab=catalog seamlessly to 5-tab IA docs view without broken links', () => {
    const html = renderToString(
      <MemoryRouter initialEntries={['/schemas?tab=catalog&service_name=demo.billing.v1.BillingService']}>
        <ProtoSchemaWorkbenchPage />
      </MemoryRouter>
    );

    expect(html).toContain('接口参考文档');
    expect(html).toContain('只读源码');
    expect(html).toContain('Git MR 门禁');
    expect(html).toContain('描述集资产台账');
    expect(html).toContain('跨包依赖关系图');
  });
});
