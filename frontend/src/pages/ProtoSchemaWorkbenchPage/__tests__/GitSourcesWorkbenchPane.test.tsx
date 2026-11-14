import { describe, expect, it, vi } from 'vitest';
import { renderToString } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import { GitSourcesWorkbenchPane } from '../GitSourcesWorkbenchPane';
import { SchemaAssetLedgerTable } from '../SchemaAssetLedgerTable';
import { ServiceReferenceDocView } from '../ServiceReferenceDocView';
import type {
  GitConsistencyReportDTO,
  GitRepoDTO,
  ProtoFileContentResponse,
  ProtoFileDTO,
  ProtoFileDiffResponse,
  ProtoFileVersionSummaryDTO,
  ProtoSchemaLedgerItemDTO,
  ProtoServiceDocDetailDTO,
} from '../../../types';

const sampleGitRepo: GitRepoDTO = {
  id: 11,
  name: 'dophi-monorepo',
  provider: 'GITLAB',
  repo_url: 'https://git.example.com/dophi/contracts.git',
  api_base_url: 'https://git.example.com',
  default_branch: 'main',
  auth_type: 'TOKEN',
  has_credentials: true,
  credential_hint: 'glpa****9876',
  has_webhook_secret: true,
  sync_mode: 'WEBHOOK',
  poll_interval_sec: 300,
  auto_compile: true,
  auto_activate: false,
  lint_on_sync: true,
  last_sync_commit: '1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b',
  last_sync_status: 'SUCCESS',
  last_sync_error: '',
  description: '金融核心 Monorepo 契约仓',
  created_by: 1,
  created_at: '2026-11-14T01:00:00Z',
  updated_at: '2026-11-14T08:00:00Z',
  bundles: [
    {
      id: 101,
      git_repo_id: 11,
      bundle_name: 'common-base',
      target_schema_name: 'common-proto',
      category: 'BASE_LIB',
      subdir_path: 'common/proto',
      include_dirs: ['.'],
      entry_files: [],
      file_globs: ['*.proto'],
      exclude_globs: [],
      auto_compile: true,
      auto_activate: true,
      mr_auto_publish: 'AUTO_ACTIVATE',
      lint_on_sync: true,
      last_sync_commit: '1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b',
      description: '公共基础模型库',
      created_at: '2026-11-14T01:00:00Z',
      updated_at: '2026-11-14T08:00:00Z',
    },
    {
      id: 102,
      git_repo_id: 11,
      bundle_name: 'order-svc',
      target_schema_name: 'order-proto',
      category: 'BUSINESS',
      subdir_path: 'services/order',
      include_dirs: ['.', '../common/proto'],
      entry_files: ['order.proto'],
      file_globs: ['**/*.proto'],
      exclude_globs: ['**/testdata/**'],
      auto_compile: true,
      auto_activate: false,
      mr_auto_publish: 'DRAFT_ONLY',
      lint_on_sync: true,
      last_sync_commit: '1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b',
      description: '订单交易领域服务契约',
      created_at: '2026-11-14T01:00:00Z',
      updated_at: '2026-11-14T08:00:00Z',
    },
  ],
};

const sampleFile: ProtoFileDTO = {
  id: 501,
  schema_id: 88,
  git_repo_id: 11,
  git_repo_name: 'dophi-monorepo',
  bundle_id: 102,
  bundle_name: 'order-svc',
  target_schema_name: 'order-proto',
  category: 'BUSINESS',
  file_path: 'v1/order.proto',
  repo_file_path: 'services/order/v1/order.proto',
  package_name: 'dophi.order.v1',
  latest_version: 2,
  latest_commit_sha: '1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b',
  content_sha256: 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
  is_active_schema: true,
  has_source_info: true,
  is_deleted: false,
  source_origin: 'GIT_SYNC',
  line_count: 6,
  updated_at: '2026-11-14T08:00:00Z',
};

const paddedLines = Array.from({ length: 40 }, (_, i) => `// line ${i + 1}`).join('\n');
const sampleProtoContent =
  paddedLines +
  '\n' +
  [
    'service OrderService {',
    '  rpc CreateOrder (CreateOrderReq) returns (CreateOrderRsp);',
    '  rpc CancelOrder (CancelOrderReq) returns (CancelOrderRsp);',
    '}',
  ].join('\n') +
  '\n';

const sampleContentResp: ProtoFileContentResponse = {
  file: sampleFile,
  version: {
    id: 902,
    proto_file_id: 501,
    schema_id: 88,
    version: 2,
    version_tag: 'git-1a2b3c4',
    commit_sha: '1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b',
    commit_message: 'feat: add CancelOrder method',
    content_sha256: 'e3b0c44298fc1c149afbf4c8996fb924',
    source_type: 'GIT_SYNC',
    author_name: 'Alice',
    summary: 'feat: add CancelOrder method',
    created_at: '2026-11-14T08:00:00Z',
    line_count: 44,
    is_active: true,
  },
  content: sampleProtoContent,
  violations: [
    {
      rule_code: 'COMMENT_REQUIRED',
      severity: 'WARNING',
      message: 'RPC CancelOrder 缺少契约说明注释',
      line: 43,
      file_line: 43,
      column: 3,
    },
  ],
  symbol_anchors: [
    {
      symbol_kind: 'SERVICE',
      symbol_full_name: 'dophi.order.v1.OrderService',
      start_line: 42,
      end_line: 44,
    },
  ],
};

const sampleVersions: ProtoFileVersionSummaryDTO[] = [
  {
    id: 902,
    proto_file_id: 501,
    schema_id: 88,
    version: 2,
    version_tag: 'git-1a2b3c4',
    commit_sha: '1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b',
    commit_message: 'feat: add CancelOrder method',
    content_sha256: 'sha2',
    source_type: 'GIT_SYNC',
    author_name: 'Alice',
    summary: 'feat: add CancelOrder method',
    is_active: true,
    created_at: '2026-11-14T08:00:00Z',
  },
  {
    id: 901,
    proto_file_id: 501,
    schema_id: 87,
    version: 1,
    version_tag: 'git-0011223',
    commit_sha: '00112233445566778899aabbccddeeff00112233',
    commit_message: 'feat: init OrderService',
    content_sha256: 'sha1',
    source_type: 'GIT_SYNC',
    author_name: 'Bob',
    summary: 'feat: init OrderService',
    is_active: false,
    created_at: '2026-11-13T08:00:00Z',
  },
];

const sampleDiff: ProtoFileDiffResponse = {
  file: sampleFile,
  from_version: {
    ...sampleContentResp.version,
    id: 901,
    version: 1,
    version_tag: 'git-0011223',
    commit_sha: '00112233445566778899aabbccddeeff00112233',
    author_name: 'Bob',
  },
  to_version: sampleContentResp.version,
  unified_diff: [
    '--- a/v1/order.proto@v1',
    '+++ b/v1/order.proto@v2',
    '@@ -41,3 +41,4 @@',
    ' service OrderService {',
    '   rpc CreateOrder (CreateOrderReq) returns (CreateOrderRsp);',
    '+  rpc CancelOrder (CancelOrderReq) returns (CancelOrderRsp);',
    ' }',
  ].join('\n'),
  added_lines: 1,
  deleted_lines: 0,
};

const sampleConsistency: GitConsistencyReportDTO = {
  repo_id: 11,
  repo_name: 'dophi-monorepo',
  branch: 'main',
  commit_sha: '99887766554433221100ffeeddccbbaa99887766',
  head_commit_sha: '99887766554433221100ffeeddccbbaa99887766',
  in_sync: false,
  remote_only_files: ['order-svc:v1/refund.proto'],
  db_only_files: [],
  modified_files: ['order-svc:v1/order.proto'],
  summary: {
    in_sync: 1,
    modified: 1,
    remote_only: 1,
    db_only: 0,
  },
  items: [
    {
      bundle_id: 101,
      bundle_name: 'common-base',
      target_schema_name: 'common-proto',
      file_path: 'common.proto',
      status: 'in_sync',
      remote_sha256: 'aaaa1111bbbb2222',
      db_sha256: 'aaaa1111bbbb2222',
      db_version: 1,
    },
    {
      bundle_id: 102,
      bundle_name: 'order-svc',
      target_schema_name: 'order-proto',
      file_path: 'v1/order.proto',
      status: 'modified',
      remote_sha256: 'cccc3333dddd4444',
      db_sha256: 'eeee5555ffff6666',
      db_version: 2,
    },
    {
      bundle_id: 102,
      bundle_name: 'order-svc',
      target_schema_name: 'order-proto',
      file_path: 'v1/refund.proto',
      status: 'remote_only',
      remote_sha256: '7777888899990000',
    },
  ],
  checked_at: '2026-11-14T08:30:00Z',
};

describe('M25 GitSourcesWorkbenchPane & DeepLink & Source Badges', () => {
  it('testGitSourcesPane_MonorepoBundles_ConsistencyAndCommitDiff: 只读源码、#L42高亮、Violation叠加与脱敏仓库抽屉', () => {
    const htmlCode = renderToString(
      <MemoryRouter initialEntries={['/schemas?tab=sources&file_id=501#L42']}>
        <GitSourcesWorkbenchPane
          isAdmin
          initialGitRepos={[sampleGitRepo]}
          initialSelectedRepoId={11}
          initialSelectedBundleId={102}
          initialFiles={[sampleFile]}
          initialTotal={1}
          initialSelectedFileId={501}
          initialHighlightLine={42}
          initialContent={sampleContentResp}
          initialVersions={sampleVersions}
          initialManageOpen
        />
      </MemoryRouter>
    );

    // Monorepo 仓库与 1:N Bundles 渲染及凭据脱敏检查
    expect(htmlCode).toContain('dophi-monorepo');
    expect(htmlCode).toContain('common-base');
    expect(htmlCode).toContain('order-svc');
    expect(htmlCode).toContain('glpa****9876');
    expect(htmlCode).not.toContain('glpat-secret-token');

    // 只读代码浏览器、#L42 行高亮、静态检查违规与零可编辑框
    expect(htmlCode).toContain('v1/order.proto');
    expect(htmlCode).toContain('COMMENT_REQUIRED');
    expect(htmlCode).toContain('pdoc-code-line--highlighted');
    expect(htmlCode).toContain('id="L42"');
    expect(htmlCode).not.toContain('<textarea');
    expect(htmlCode).not.toContain('锁定文件');
    expect(htmlCode).not.toContain('解锁文件');

    // Commit Diff 模式与只读 SHA256 一致性对账抽屉
    const htmlDiffAndConsistency = renderToString(
      <MemoryRouter initialEntries={['/schemas?tab=sources&file_id=501']}>
        <GitSourcesWorkbenchPane
          isAdmin
          initialGitRepos={[sampleGitRepo]}
          initialSelectedRepoId={11}
          initialFiles={[sampleFile]}
          initialTotal={1}
          initialSelectedFileId={501}
          initialContent={sampleContentResp}
          initialVersions={sampleVersions}
          initialViewMode="diff"
          initialDiff={sampleDiff}
          initialConsistencyOpen
          initialConsistencyReport={sampleConsistency}
        />
      </MemoryRouter>
    );

    expect(htmlDiffAndConsistency).toContain('pdoc-diff-row--add');
    expect(htmlDiffAndConsistency).toContain('rpc CancelOrder (CancelOrderReq) returns (CancelOrderRsp);');
    expect(htmlDiffAndConsistency).toContain('v1/refund.proto');
    expect(htmlDiffAndConsistency).toContain('9988776');
    expect(htmlDiffAndConsistency).toContain('内容差异 (modified)');
    expect(htmlDiffAndConsistency).toContain('仅远端新增 (remote_only)');
  });

  it('资产台账渲染 UPLOAD_PB / GIT_SYNC 来源徽章与短 Commit SHA，文档详情渲染 ?tab=sources 深链', () => {
    const ledgerItems: ProtoSchemaLedgerItemDTO[] = [
      {
        id: 1,
        name: 'upload-schema',
        version: 1,
        version_tag: 'v1.0',
        category: 'BASE_LIB',
        source_type: 'UPLOAD_PB',
        file_name: 'base.pb',
        file_sha256: 'abc',
        file_size: 1024,
        is_active: true,
        proto_file_count: 1,
        service_count: 0,
        operation_count: 0,
        proto_files: ['common.proto'],
        external_imports: [],
        description: '',
        created_by: 1,
        created_at: '2026-11-14T00:00:00Z',
        updated_at: '2026-11-14T00:00:00Z',
        version_count: 1,
        latest_version: 1,
        claimed_service_count: 0,
        claimed_ratio: 0,
        resolved_dependencies: [],
      },
      {
        id: 2,
        name: 'order-proto',
        version: 2,
        version_tag: 'git-1a2b3c4',
        category: 'BUSINESS',
        source_type: 'GIT_SYNC',
        source_commit_sha: '1a2b3c4d5e6f7a8b9c0d',
        file_name: 'order.pb',
        file_sha256: 'def',
        file_size: 2048,
        is_active: true,
        proto_file_count: 1,
        service_count: 1,
        operation_count: 2,
        proto_files: ['v1/order.proto'],
        external_imports: ['common.proto'],
        description: '',
        created_by: 1,
        created_at: '2026-11-14T00:00:00Z',
        updated_at: '2026-11-14T00:00:00Z',
        version_count: 2,
        latest_version: 2,
        claimed_service_count: 1,
        claimed_ratio: 1,
        resolved_dependencies: [],
      },
    ];

    const tableHtml = renderToString(
      <MemoryRouter>
        <SchemaAssetLedgerTable
          schemas={ledgerItems}
          loading={false}
          total={2}
          page={1}
          pageSize={25}
          isAdmin={false}
          selectedCategory=""
          activeStatusFilter="ALL"
          searchKeyword=""
          onCategoryChange={vi.fn()}
          onStatusFilterChange={vi.fn()}
          onSearchChange={vi.fn()}
          onPageChange={vi.fn()}
          onPageSizeChange={vi.fn()}
          onActivate={vi.fn()}
          onDeactivate={vi.fn()}
          onBatchActivate={vi.fn()}
          onDelete={vi.fn()}
          onDownload={vi.fn()}
          onInspect={vi.fn()}
          onUploadNewVersion={vi.fn()}
        />
      </MemoryRouter>
    );

    expect(tableHtml).toContain('手动上传 .pb');
    expect(tableHtml).toContain('Git 同步');
    expect(tableHtml).toContain('1a2b3c4');

    const detail: ProtoServiceDocDetailDTO = {
      schema_id: 88,
      schema_name: 'order-proto',
      schema_version: 2,
      schema_category: 'BUSINESS',
      source_type: 'GIT_SYNC',
      has_source_info: true,
      package_name: 'dophi.order.v1',
      service_name: 'dophi.order.v1.OrderService',
      short_name: 'OrderService',
      proto_file: 'v1/order.proto',
      source_file_id: 501,
      start_line: 42,
      end_line: 44,
      file_comments: '',
      summary: '订单服务',
      leading_comments: '',
      trailing_comments: '',
      is_deprecated: false,
      deprecation_reason: '',
      owner_tag: 'team-order',
      error_codes: [],
      methods: [],
      enums: [],
      referenced_types: [],
      lineage: {
        claim_status: 'CLAIMED_SINGLE',
        implementations: [],
        consumers: [],
      },
    };

    const docHtml = renderToString(
      <MemoryRouter>
        <ServiceReferenceDocView detail={detail} onJumpToSource={vi.fn()} />
      </MemoryRouter>
    );

    expect(docHtml).toContain('tab=sources&amp;file_id=501#L42');
  });
});
