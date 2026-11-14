import React, { useCallback, useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { BookOpen, Code2, ShieldCheck, FolderKanban, Network } from 'lucide-react';
import { Drawer, Modal, usePaginationState } from '@code/common';
import { useToast } from '../../utils/toast';
import { useModuleUser } from '../../context/ModuleUserContext';
import * as schemaApi from '../../api/schema';
import type {
  ProtoCategory,
  ProtoSchemaLedgerItemDTO,
  ProtoPoolSummaryDTO,
  ConflictOrBrokenResponse,
  ProtoOperation,
  ServiceContractGroupDTO,
  BreakingRuleViolation,
  AffectedDependent,
  ReconstructedProtoFileDTO,
} from '../../types';
import { SchemaPoolSummaryBar } from './SchemaPoolSummaryBar';
import { SchemaAssetLedgerTable } from './SchemaAssetLedgerTable';
import { SchemaAssetInspectDrawer } from './SchemaAssetInspectDrawer';
import { SchemaUploadModal, type SchemaUploadMode } from './SchemaUploadModal';
import { SchemaConflictModal } from './SchemaConflictModal';
import { BreakingChangeAlertDrawer } from '../ProtoSchemasPage/BreakingChangeAlertDrawer';
import { OperationMappingTable } from './OperationMappingTable';
import { OperationDocDrawer } from './OperationDocDrawer';
import { ProtoDependenciesGraphPane } from './ProtoDependenciesGraphPane';
import { LivingProtoDocsPane } from './LivingProtoDocsPane';
import { GitSourcesWorkbenchPane } from './GitSourcesWorkbenchPane';
import { MRGateWorkbenchPane } from './MRGateWorkbenchPane';
import { ProtoReadOnlyCodeViewer } from './ProtoReadOnlyCodeViewer';
import LintRuleAndVocabDrawer, { type LintDrawerSubTab } from './LintRuleAndVocabDrawer';
import './ProtoDocsAndCodeViewer.css';

export type SchemaWorkbenchTab = 'docs' | 'sources' | 'mr-gates' | 'assets' | 'graph';

export const ProtoSchemaWorkbenchPage: React.FC = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const { user } = useModuleUser();
  const toast = useToast();
  const isAdmin = Boolean(user?.is_module_admin || user?.is_super_admin);

  // 1. 顶层 5 视窗 Tab 判断（无损重映射旧版 ?tab=catalog -> docs）
  const rawTab = searchParams.get('tab');
  const serviceNameQuery = searchParams.get('service_name');
  const schemaIdQuery = searchParams.get('schema_id');
  const claimedStatusQuery = searchParams.get('claimed_status');
  const searchQueryParam = searchParams.get('search') || '';

  const resolveActiveTab = (): SchemaWorkbenchTab => {
    switch (rawTab) {
      case 'graph':
        return 'graph';
      case 'assets':
        return 'assets';
      case 'sources':
        return 'sources';
      case 'mr-gates':
        return 'mr-gates';
      case 'docs':
      case 'catalog':
      default:
        return 'docs';
    }
  };
  const activeTab: SchemaWorkbenchTab = resolveActiveTab();

  const setTab = (tab: SchemaWorkbenchTab) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      next.set('tab', tab);
      return next;
    });
  };

  // docs 视窗内部兼容模式：当由大盘跳入 ?claimed_status=UNCLAIMED 时默认进入契约认领列表，可一键切回参考文档
  const [docsSubMode, setDocsSubMode] = useState<'reference' | 'catalog-ops'>(() =>
    claimedStatusQuery === 'UNCLAIMED' ? 'catalog-ops' : 'reference'
  );

  // M23 只读源码跳转抽屉状态（由 ?tab=docs 点击 file.proto#L42 触发）
  const [sourceDrawerOpen, setSourceDrawerOpen] = useState(false);
  const [sourceDrawerLoading, setSourceDrawerLoading] = useState(false);
  const [sourceDrawerFile, setSourceDrawerFile] = useState<ReconstructedProtoFileDTO | null>(null);
  const [sourceDrawerLine, setSourceDrawerLine] = useState<number>(1);

  // M24 静态检查规则、标准词库与 AST 重复探测抽屉状态
  const [lintDrawerOpen, setLintDrawerOpen] = useState(false);
  const [lintDrawerSubTab, setLintDrawerSubTab] = useState<LintDrawerSubTab>('rules');

  // 2. 全局生效池摘要状态
  const [poolSummary, setPoolSummary] = useState<ProtoPoolSummaryDTO | null>(null);

  // 3. Tab 1: 文件资产台账状态
  const {
    page: assetPage,
    pageSize: assetPageSize,
    setPage: setAssetPage,
    setPageSize: setAssetPageSize,
  } = usePaginationState({ defaultPageSize: 25 });

  const selectedCategory = searchParams.get('category') || '';
  const activeStatusFilter = searchParams.get('status') || 'ALL';
  const sourceTypeFilter = searchParams.get('source_type') || '';
  const assetSearch = searchParams.get('search') || '';

  const [assetSchemas, setAssetSchemas] = useState<ProtoSchemaLedgerItemDTO[]>([]);
  const [totalAssetSchemas, setTotalAssetSchemas] = useState(0);
  const [assetLoading, setAssetLoading] = useState(false);

  // 4. Tab 2: 全局服务与接口目录状态
  const {
    page: catalogPage,
    pageSize: catalogPageSize,
    setPage: setCatalogPage,
    setPageSize: setCatalogPageSize,
  } = usePaginationState({ defaultPageSize: 25 });

  const [catalogGroups, setCatalogGroups] = useState<ServiceContractGroupDTO[]>([]);
  const [totalCatalogServices, setTotalCatalogServices] = useState(0);
  const [catalogLoading, setCatalogLoading] = useState(false);
  const [orphanOnly, setOrphanOnly] = useState(searchParams.get('claimed_status') === 'UNCLAIMED');

  // 5. 弹窗与抽屉受控状态
  const [uploadModalOpen, setUploadModalOpen] = useState(false);
  const [uploadModalMode, setUploadModalMode] = useState<SchemaUploadMode>('upload');
  const [targetSchemaForNewVer, setTargetSchemaForNewVer] = useState<ProtoSchemaLedgerItemDTO | null>(null);

  const [inspectDrawerOpen, setInspectDrawerOpen] = useState(false);
  const [inspectSchema, setInspectSchema] = useState<ProtoSchemaLedgerItemDTO | null>(null);

  const [conflictModalOpen, setConflictModalOpen] = useState(false);
  const [conflictData, setConflictData] = useState<ConflictOrBrokenResponse | null>(null);

  const [breakingDrawerOpen, setBreakingDrawerOpen] = useState(false);
  const [breakingDrawerData, setBreakingDrawerData] = useState<{
    schemaId: string | number | null;
    batchIds: (string | number)[] | null;
    breakingRules: BreakingRuleViolation[];
    affectedDependents: AffectedDependent[];
    onCustomOverride?: (reason: string) => Promise<void>;
  } | null>(null);
  const [isOverriding, setIsOverriding] = useState(false);

  const [docDrawerOpen, setDocDrawerOpen] = useState(false);
  const [selectedOp, setSelectedOp] = useState<ProtoOperation | null>(null);

  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);
  const [schemaToDelete, setSchemaToDelete] = useState<ProtoSchemaLedgerItemDTO | null>(null);

  // 加载描述集文件资产台账
  const loadAssetLedger = useCallback(async () => {
    setAssetLoading(true);
    try {
      const res = await schemaApi.listProtoSchemas({
        category: selectedCategory || undefined,
        active_only: activeStatusFilter === 'ACTIVE' ? true : activeStatusFilter === 'INACTIVE' ? false : undefined,
        source_type: sourceTypeFilter || undefined,
        search: assetSearch || undefined,
        latest_only: true,
        page: assetPage,
        page_size: assetPageSize,
      });
      setAssetSchemas(res.items ?? []);
      setTotalAssetSchemas(res.total ?? 0);
      if (res.pool_summary) {
        setPoolSummary(res.pool_summary);
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '加载描述集资产台账失败');
    } finally {
      setAssetLoading(false);
    }
  }, [selectedCategory, activeStatusFilter, sourceTypeFilter, assetSearch, assetPage, assetPageSize, toast]);

  useEffect(() => {
    if (activeTab === 'assets') {
      void loadAssetLedger();
    }
  }, [activeTab, loadAssetLedger]);

  // 加载全局服务目录
  const loadCatalogServices = useCallback(async () => {
    setCatalogLoading(true);
    try {
      const res = await schemaApi.listProtoServices({
        schema_id: schemaIdQuery || undefined,
        claimed_status: orphanOnly ? 'UNCLAIMED' : undefined,
        service_name: serviceNameQuery || undefined,
        search: assetSearch || undefined,
        page: catalogPage,
        page_size: catalogPageSize,
      });
      setCatalogGroups(res.items ?? []);
      setTotalCatalogServices(res.total ?? 0);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '加载全局服务目录失败');
    } finally {
      setCatalogLoading(false);
    }
  }, [schemaIdQuery, orphanOnly, serviceNameQuery, assetSearch, catalogPage, catalogPageSize, toast]);

  useEffect(() => {
    if (activeTab === 'docs' && docsSubMode === 'catalog-ops') {
      void loadCatalogServices();
    }
  }, [activeTab, docsSubMode, loadCatalogServices]);

  // 初始时若在 catalog 模式，补拉取一次 poolSummary
  useEffect(() => {
    if (!poolSummary) {
      void schemaApi
        .listProtoSchemas({ page_size: 1, latest_only: true })
        .then((res) => {
          if (res.pool_summary) setPoolSummary(res.pool_summary);
        })
        .catch(() => {});
    }
  }, [poolSummary]);

  // 快捷筛选过滤变更
  const handleCategoryChange = (cat: string) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      if (cat) next.set('category', cat);
      else next.delete('category');
      next.set('page', '1');
      return next;
    });
    setAssetPage(1);
  };

  const handleStatusFilterChange = (status: string) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      if (status !== 'ALL') next.set('status', status);
      else next.delete('status');
      next.set('page', '1');
      return next;
    });
    setAssetPage(1);
  };

  const handleSourceTypeFilterChange = (sourceType: string) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      if (sourceType) next.set('source_type', sourceType);
      else next.delete('source_type');
      next.set('page', '1');
      return next;
    });
    setAssetPage(1);
  };

  const handleSearchChange = (keyword: string) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      if (keyword.trim()) next.set('search', keyword.trim());
      else next.delete('search');
      next.set('page', '1');
      return next;
    });
    setAssetPage(1);
    setCatalogPage(1);
  };

  // 激活单个描述集
  const handleActivateSchema = async (s: ProtoSchemaLedgerItemDTO) => {
    try {
      const res = await schemaApi.activateProtoSchema(s.id);
      toast.success(`描述集 [${s.name} v${s.version}] 激活成功，生效 ${res.activated_operations} 个服务契约`);
      void loadAssetLedger();
      if (activeTab === 'docs' && docsSubMode === 'catalog-ops') void loadCatalogServices();
      if (inspectDrawerOpen && inspectSchema?.name === s.name) {
        setInspectSchema(res.schema);
      }
    } catch (err: unknown) {
      const respData = (err as unknown as { responseData?: ConflictOrBrokenResponse })?.responseData;
      if (respData && (respData.code === 'BREAKING_CHANGE_DETECTED' || (respData.breaking_rules && respData.breaking_rules.length > 0))) {
        setBreakingDrawerData({
          schemaId: s.id,
          batchIds: null,
          breakingRules: respData.breaking_rules || [],
          affectedDependents: respData.affected_dependents || [],
        });
        setBreakingDrawerOpen(true);
        return;
      }
      if (respData && (respData.conflicts || respData.broken_dependents)) {
        setConflictData(respData);
        setConflictModalOpen(true);
        return;
      }
      toast.error(err instanceof Error ? err.message : '激活描述集失败');
    }
  };

  // 停用描述集
  const handleDeactivateSchema = async (s: ProtoSchemaLedgerItemDTO) => {
    try {
      const res = await schemaApi.deactivateProtoSchema(s.id);
      toast.success(`描述集 [${s.name} v${s.version}] 已停用，卸载 ${res.deactivated_operations} 个服务契约`);
      void loadAssetLedger();
      if (activeTab === 'docs' && docsSubMode === 'catalog-ops') void loadCatalogServices();
      if (inspectDrawerOpen && inspectSchema?.name === s.name) {
        setInspectSchema(res.schema);
      }
    } catch (err: unknown) {
      const respData = (err as unknown as { responseData?: ConflictOrBrokenResponse })?.responseData;
      if (respData && (respData.conflicts || respData.broken_dependents)) {
        setConflictData(respData);
        setConflictModalOpen(true);
        return;
      }
      toast.error(err instanceof Error ? err.message : '停用描述集失败');
    }
  };

  // 批量激活选中项
  const handleBatchActivate = async (selectedIds: (string | number)[]) => {
    try {
      const res = await schemaApi.batchActivateProtoSchemas(selectedIds);
      toast.success(res.message || `成功批量激活 ${res.activated_count} 个描述集`);
      void loadAssetLedger();
      if (activeTab === 'docs' && docsSubMode === 'catalog-ops') void loadCatalogServices();
    } catch (err: unknown) {
      const respData = (err as unknown as { responseData?: ConflictOrBrokenResponse })?.responseData;
      if (respData && (respData.code === 'BREAKING_CHANGE_DETECTED' || (respData.breaking_rules && respData.breaking_rules.length > 0))) {
        setBreakingDrawerData({
          schemaId: null,
          batchIds: selectedIds,
          breakingRules: respData.breaking_rules || [],
          affectedDependents: respData.affected_dependents || [],
        });
        setBreakingDrawerOpen(true);
        return;
      }
      if (respData && (respData.conflicts || respData.broken_dependents)) {
        setConflictData(respData);
        setConflictModalOpen(true);
        return;
      }
      toast.error(err instanceof Error ? err.message : '批量激活失败');
    }
  };

  // 管理员强制放行破坏性变更
  const handleConfirmBreakingOverride = async (reason: string) => {
    if (!breakingDrawerData) return;
    setIsOverriding(true);
    try {
      if (breakingDrawerData.onCustomOverride) {
        await breakingDrawerData.onCustomOverride(reason);
      } else if (breakingDrawerData.schemaId) {
        const res = await schemaApi.activateProtoSchema(breakingDrawerData.schemaId, {
          allow_breaking: true,
          breaking_reason: reason,
        });
        toast.success(`已强制激活破坏性版本，生效 ${res.activated_operations} 个服务契约 (已记录 P1 审计)`);
      } else if (breakingDrawerData.batchIds) {
        const res = await schemaApi.batchActivateProtoSchemas(breakingDrawerData.batchIds, {
          allow_breaking: true,
          breaking_reason: reason,
        });
        toast.success(res.message || `已强制批量激活 ${res.activated_count} 个描述集 (已记录 P1 审计)`);
      }
      setBreakingDrawerOpen(false);
      setBreakingDrawerData(null);
      void loadAssetLedger();
      if (activeTab === 'docs' && docsSubMode === 'catalog-ops') void loadCatalogServices();
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : '强制激活操作失败');
    } finally {
      setIsOverriding(false);
    }
  };

  // 删除确认
  const handleDeleteSchema = (s: ProtoSchemaLedgerItemDTO) => {
    setSchemaToDelete(s);
    setDeleteConfirmOpen(true);
  };

  const confirmDeleteSchema = async () => {
    if (!schemaToDelete) return;
    try {
      await schemaApi.deleteProtoSchema(schemaToDelete.id);
      toast.success(`描述集 [${schemaToDelete.name} v${schemaToDelete.version}] 已彻底删除`);
      setDeleteConfirmOpen(false);
      setSchemaToDelete(null);
      void loadAssetLedger();
      if (activeTab === 'docs' && docsSubMode === 'catalog-ops') void loadCatalogServices();
      if (inspectDrawerOpen && inspectSchema?.id === schemaToDelete.id) {
        setInspectDrawerOpen(false);
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '删除描述集失败');
    }
  };

  // 透视指定描述集
  const handleInspectSchema = (s: ProtoSchemaLedgerItemDTO) => {
    setInspectSchema(s);
    setInspectDrawerOpen(true);
  };

  // 按 ID 溯源透视描述集
  const handleInspectSchemaById = async (schemaId: string | number) => {
    const found = assetSchemas.find((item) => String(item.id) === String(schemaId));
    if (found) {
      handleInspectSchema(found);
      return;
    }
    try {
      const summary = await schemaApi.getProtoSchemaSummary(schemaId);
      if (summary && summary.schema) {
        const item: ProtoSchemaLedgerItemDTO = {
          ...summary.schema,
          id: summary.schema.id,
          name: summary.schema.name,
          version: summary.schema.version,
          category: (summary.schema.category as ProtoCategory) || 'BUSINESS',
          file_name: summary.schema.file_name || `${summary.schema.name}.pb`,
          file_size: summary.schema.file_size || 0,
          is_active: summary.schema.is_active,
          proto_file_count: summary.proto_files.length,
          service_count: summary.services_summary.length,
          operation_count: summary.schema.operation_count || 0,
          proto_files: summary.proto_files,
          version_count: 1,
          latest_version: summary.schema.version,
          claimed_service_count: summary.services_summary.filter((s) => s.is_claimed).length,
          claimed_ratio: summary.claimed_ratio,
          resolved_dependencies: [],
        };
        handleInspectSchema(item);
      }
    } catch {
      toast.error('未找到对应描述集元数据');
    }
  };

  // 从抽屉跳至活体文档参考中心
  const handleNavigateToCatalog = (schemaId: string | number) => {
    setInspectDrawerOpen(false);
    setDocsSubMode('reference');
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      next.set('tab', 'docs');
      next.set('schema_id', String(schemaId));
      return next;
    });
  };

  // M23: 从文档页跳入只读源码抽屉定位至 file.proto#L<n>
  const handleJumpToSourceDrawer = async (schemaId: number, filePath: string, line: number) => {
    setSourceDrawerOpen(true);
    setSourceDrawerLine(line > 0 ? line : 1);
    setSourceDrawerLoading(true);
    try {
      const res = await schemaApi.getProtoSchemaSources(schemaId, filePath);
      const hit = res.files.find((f) => f.file_path === filePath) ?? res.files[0] ?? null;
      setSourceDrawerFile(hit);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '加载只读源码视图失败');
      setSourceDrawerFile(null);
    } finally {
      setSourceDrawerLoading(false);
    }
  };

  // 从依赖图节点跳回文件资产台账并透视该描述集
  const handleNavigateToSchemaAsset = (schemaId: string | number) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      next.set('tab', 'assets');
      next.set('schema_id', String(schemaId));
      return next;
    });
    void handleInspectSchemaById(schemaId);
  };

  return (
    <div className="code-proto-schema-workbench" style={{ padding: '20px' }}>
      {/* 顶部全局生效池健康摘要条 */}
      <SchemaPoolSummaryBar
        summary={poolSummary}
        isAdmin={isAdmin}
        onUploadClick={() => {
          setUploadModalMode('upload');
          setTargetSchemaForNewVer(null);
          setUploadModalOpen(true);
        }}
        onCompileClick={() => {
          setUploadModalMode('compile');
          setTargetSchemaForNewVer(null);
          setUploadModalOpen(true);
        }}
      />

      {/* 顶层无侧栏 5 视窗分段 Tab 切换导航 (M23 IA 收口) */}
      <div className="pdoc-top-tabs">
        <div className="pdoc-top-tabs__list" role="tablist" aria-label="契约治理视窗">
          <button
            type="button"
            role="tab"
            aria-selected={activeTab === 'docs'}
            onClick={() => setTab('docs')}
            className={`pdoc-top-tab ${activeTab === 'docs' ? 'pdoc-top-tab--active' : ''}`}
          >
            <BookOpen size={15} />
            <span>接口参考文档</span>
          </button>

          <button
            type="button"
            role="tab"
            aria-selected={activeTab === 'sources'}
            onClick={() => setTab('sources')}
            className={`pdoc-top-tab ${activeTab === 'sources' ? 'pdoc-top-tab--active' : ''}`}
          >
            <Code2 size={15} />
            <span>只读源码</span>
          </button>

          <button
            type="button"
            role="tab"
            aria-selected={activeTab === 'mr-gates'}
            onClick={() => setTab('mr-gates')}
            className={`pdoc-top-tab ${activeTab === 'mr-gates' ? 'pdoc-top-tab--active' : ''}`}
          >
            <ShieldCheck size={15} />
            <span>Git MR 门禁</span>
          </button>

          <button
            type="button"
            role="tab"
            aria-selected={activeTab === 'assets'}
            onClick={() => setTab('assets')}
            className={`pdoc-top-tab ${activeTab === 'assets' ? 'pdoc-top-tab--active' : ''}`}
          >
            <FolderKanban size={15} />
            <span>描述集资产台账</span>
          </button>

          <button
            type="button"
            role="tab"
            aria-selected={activeTab === 'graph'}
            onClick={() => setTab('graph')}
            className={`pdoc-top-tab ${activeTab === 'graph' ? 'pdoc-top-tab--active' : ''}`}
          >
            <Network size={15} />
            <span>跨包依赖关系图</span>
          </button>
        </div>

        <div className="code-tab-pills">
          <button
            type="button"
            className="btn btn-secondary"
            onClick={() => {
              setLintDrawerSubTab('rules');
              setLintDrawerOpen(true);
            }}
          >
            静态规则与词库体检
          </button>
          {activeTab === 'docs' && (
            <>
              <button
                type="button"
                className={`code-pill-btn ${docsSubMode === 'reference' ? 'code-pill-btn-active' : ''}`}
                onClick={() => setDocsSubMode('reference')}
              >
                全注释参考文档
              </button>
              <button
                type="button"
                className={`code-pill-btn ${docsSubMode === 'catalog-ops' ? 'code-pill-btn-active' : ''}`}
                onClick={() => setDocsSubMode('catalog-ops')}
              >
                契约认领与方法目录
              </button>
            </>
          )}
        </div>
      </div>

      {/* 主视图 1: 活体接口参考文档中心（兼容 ?tab=catalog 与 ?claimed_status=UNCLAIMED） */}
      {activeTab === 'docs' && docsSubMode === 'reference' && (
        <LivingProtoDocsPane
          initialServiceName={serviceNameQuery || ''}
          initialSchemaId={schemaIdQuery || ''}
          initialSearch={searchQueryParam}
          onJumpToSource={(sid, fp, line) => void handleJumpToSourceDrawer(sid, fp, line)}
        />
      )}

      {activeTab === 'docs' && docsSubMode === 'catalog-ops' && (
        <OperationMappingTable
          serviceGroups={catalogGroups}
          totalServices={totalCatalogServices}
          page={catalogPage}
          pageSize={catalogPageSize}
          onPageChange={setCatalogPage}
          onPageSizeChange={setCatalogPageSize}
          searchKeyword={assetSearch}
          loading={catalogLoading}
          onSelectOperation={(op) => {
            setSelectedOp(op);
            setDocDrawerOpen(true);
          }}
          onInspectSchema={(sid) => void handleInspectSchemaById(sid)}
          orphanOnly={orphanOnly}
          onOrphanOnlyChange={(val) => {
            setOrphanOnly(val);
            setSearchParams((prev) => {
              const next = new URLSearchParams(prev);
              if (val) next.set('claimed_status', 'UNCLAIMED');
              else next.delete('claimed_status');
              next.set('page', '1');
              return next;
            });
            setCatalogPage(1);
          }}
          selectedSchemaId={schemaIdQuery || ''}
          onSelectSchemaId={(sid) => {
            setSearchParams((prev) => {
              const next = new URLSearchParams(prev);
              if (sid) next.set('schema_id', sid);
              else next.delete('schema_id');
              next.set('page', '1');
              return next;
            });
            setCatalogPage(1);
          }}
          availableSchemas={assetSchemas}
        />
      )}

      {/* 主视图 2: Git 同步源码、Monorepo 多包与只读 Commit Diff 工作台 */}
      {activeTab === 'sources' && (
        <GitSourcesWorkbenchPane isAdmin={isAdmin} initialSchemaName={searchQueryParam} />
      )}

      {/* 主视图 3: Git MR 四合一门禁检查雷达（M26 全链路接入） */}
      {activeTab === 'mr-gates' && (
        <MRGateWorkbenchPane
          isAdmin={isAdmin}
          onOpenSourcesTab={() => setTab('sources')}
          onOpenLintCenter={() => {
            setLintDrawerSubTab('report');
            setLintDrawerOpen(true);
          }}
        />
      )}

      {/* 主视图 4: 描述集资产台账 */}
      {activeTab === 'assets' && (
        <SchemaAssetLedgerTable
          schemas={assetSchemas}
          total={totalAssetSchemas}
          page={assetPage}
          pageSize={assetPageSize}
          onPageChange={setAssetPage}
          onPageSizeChange={setAssetPageSize}
          selectedCategory={selectedCategory}
          onCategoryChange={handleCategoryChange}
          activeStatusFilter={activeStatusFilter}
          onStatusFilterChange={handleStatusFilterChange}
          sourceTypeFilter={sourceTypeFilter}
          onSourceTypeFilterChange={handleSourceTypeFilterChange}
          highlightedSchemaId={schemaIdQuery}
          searchKeyword={assetSearch}
          onSearchChange={handleSearchChange}
          loading={assetLoading}
          isAdmin={isAdmin}
          onInspect={handleInspectSchema}
          onUploadNewVersion={(s) => {
            setUploadModalMode('upload');
            setTargetSchemaForNewVer(s);
            setUploadModalOpen(true);
          }}
          onActivate={(s) => void handleActivateSchema(s)}
          onDeactivate={(s) => void handleDeactivateSchema(s)}
          onBatchActivate={(ids) => void handleBatchActivate(ids)}
          onDelete={handleDeleteSchema}
          onDownload={(s) => void schemaApi.downloadProtoSchema(s.id, s.file_name)}
        />
      )}

      {/* 主视图 5: 跨描述集依赖关系图 */}
      {activeTab === 'graph' && (
        <ProtoDependenciesGraphPane onNavigateToSchemaAsset={handleNavigateToSchemaAsset} />
      )}

      {/* 描述集上传、在线直编与新版本弹窗 */}
      <SchemaUploadModal
        open={uploadModalOpen}
        onClose={() => {
          setUploadModalOpen(false);
          setTargetSchemaForNewVer(null);
        }}
        initialName={targetSchemaForNewVer ? targetSchemaForNewVer.name : ''}
        initialCategory={targetSchemaForNewVer ? targetSchemaForNewVer.category : 'BUSINESS'}
        initialMode={uploadModalMode}
        onSuccess={() => {
          void loadAssetLedger();
          if (activeTab === 'docs' && docsSubMode === 'catalog-ops') void loadCatalogServices();
        }}
        onConflict={(cData) => {
          setConflictData(cData);
          setConflictModalOpen(true);
        }}
        onBreakingChange={(bData, onOverrideSubmit) => {
          setBreakingDrawerData({
            schemaId: null,
            batchIds: null,
            breakingRules: bData.breaking_rules || [],
            affectedDependents: bData.affected_dependents || [],
            onCustomOverride: onOverrideSubmit,
          });
          setBreakingDrawerOpen(true);
        }}
      />

      {/* 409 契约冲突与依赖破坏诊断弹窗 */}
      <SchemaConflictModal
        open={conflictModalOpen}
        onClose={() => {
          setConflictModalOpen(false);
          setConflictData(null);
        }}
        conflictData={conflictData}
      />

      {/* 破坏性契约变更拦截与影响面透视抽屉 */}
      <BreakingChangeAlertDrawer
        isOpen={breakingDrawerOpen}
        onClose={() => {
          setBreakingDrawerOpen(false);
          setBreakingDrawerData(null);
        }}
        breakingRules={breakingDrawerData?.breakingRules || []}
        affectedDependents={breakingDrawerData?.affectedDependents || []}
        isAdmin={isAdmin}
        onConfirmOverride={handleConfirmBreakingOverride}
        isSubmitting={isOverriding}
      />

      {/* 680px 描述集多版本透视抽屉 */}
      <SchemaAssetInspectDrawer
        open={inspectDrawerOpen}
        onClose={() => {
          setInspectDrawerOpen(false);
          setInspectSchema(null);
        }}
        schema={inspectSchema}
        isAdmin={isAdmin}
        onActivate={handleActivateSchema}
        onDeactivate={handleDeactivateSchema}
        onDelete={async (s) => {
          handleDeleteSchema(s);
        }}
        onUploadNewVersion={(s) => {
          setInspectDrawerOpen(false);
          setTargetSchemaForNewVer(s);
          setUploadModalOpen(true);
        }}
        onNavigateToCatalog={handleNavigateToCatalog}
      />

      {/* 接口字典树与 Diff 忽略规则抽屉 */}
      <OperationDocDrawer
        open={docDrawerOpen}
        onClose={() => {
          setDocDrawerOpen(false);
          setSelectedOp(null);
        }}
        operation={selectedOp}
        isAdmin={isAdmin}
        onOperationUpdated={(updated) => {
          setCatalogGroups((prev) =>
            prev.map((grp) => ({
              ...grp,
              operations: (grp.operations || []).map((op) =>
                op.id === updated.id ? { ...op, ...updated } : op
              ),
            }))
          );
        }}
      />

      {/* M23 只读源码 #L<n> 定位浏览抽屉 */}
      <Drawer
        open={sourceDrawerOpen}
        onClose={() => {
          setSourceDrawerOpen(false);
          setSourceDrawerFile(null);
        }}
        title={
          sourceDrawerFile
            ? `只读源码锚点浏览: ${sourceDrawerFile.file_path}#L${sourceDrawerLine}`
            : '只读源码锚点浏览'
        }
        width={880}
      >
        {sourceDrawerLoading ? (
          <p className="pdoc-card__subtitle">正在加载只读源码与精确行号锚点...</p>
        ) : sourceDrawerFile ? (
          <ProtoReadOnlyCodeViewer
            filePath={sourceDrawerFile.file_path}
            content={sourceDrawerFile.content}
            highlightLine={sourceDrawerLine}
            symbolAnchors={sourceDrawerFile.symbol_anchors}
            versionLabel={`${sourceDrawerFile.schema_name} v${sourceDrawerFile.schema_version}`}
            hasSourceInfo={sourceDrawerFile.has_source_info}
            sourceOrigin={sourceDrawerFile.source_origin}
            onSelectLine={(line) => setSourceDrawerLine(line)}
          />
        ) : (
          <p className="pdoc-card__subtitle">未能加载指定描述集的只读源码内容。</p>
        )}
      </Drawer>

      {/* M24 静态检查规则、标准词库与 AST 重复探测抽屉 */}
      <LintRuleAndVocabDrawer
        open={lintDrawerOpen}
        isAdmin={isAdmin}
        initialSubTab={lintDrawerSubTab}
        onClose={() => setLintDrawerOpen(false)}
        onJumpToSource={async (protoFile, line) => {
          try {
            const listResp = await schemaApi.listProtoFiles({ search: protoFile, page_size: 15 });
            const matched =
              listResp.items.find((item) => item.file_path === protoFile) ?? listResp.items[0];
            if (matched) {
              void handleJumpToSourceDrawer(matched.schema_id, matched.file_path, line);
              return;
            }
          } catch {
            // 找不到虚拟文件映射时退回只读源码页搜索
          }
          setLintDrawerOpen(false);
          setTab('sources');
        }}
      />

      {/* 受控删除确认弹窗 */}
      <Modal
        open={deleteConfirmOpen}
        onClose={() => {
          setDeleteConfirmOpen(false);
          setSchemaToDelete(null);
        }}
        title="确认删除描述集"
        footer={
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '8px' }}>
            <button
              type="button"
              className="btn btn-secondary"
              onClick={() => {
                setDeleteConfirmOpen(false);
                setSchemaToDelete(null);
              }}
            >
              取消
            </button>
            <button
              type="button"
              className="btn btn-danger"
              style={{ background: 'var(--color-danger, #ef4444)', color: '#fff', border: 'none' }}
              onClick={() => void confirmDeleteSchema()}
            >
              确认彻底删除
            </button>
          </div>
        }
      >
        <div style={{ padding: '8px 0', color: 'var(--color-text-primary)' }}>
          <p style={{ fontSize: '14px', marginBottom: '8px' }}>
            确定要删除描述集版本{' '}
            <strong>
              {schemaToDelete?.name} v{schemaToDelete?.version}
            </strong>{' '}
            吗？
          </p>
          <p style={{ fontSize: '12px', color: 'var(--color-text-secondary)' }}>
            删除后该版本对应的二进制产物与已解析的方法契约将被物理清除，且不可恢复。
          </p>
        </div>
      </Modal>
    </div>
  );
};

export default ProtoSchemaWorkbenchPage;
