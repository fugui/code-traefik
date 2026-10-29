import React, { useCallback, useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Modal, Pagination, usePaginationState } from '@code/common';
import { useToast } from '../../utils/toast';
import { useModuleUser } from '../../context/ModuleUserContext';
import * as schemaApi from '../../api/schema';
import type { ProtoSchema, ProtoOperation, SchemaConflict } from '../../types';
import { SchemaUploadModal } from './SchemaUploadModal';
import { SchemaVersionList } from './SchemaVersionList';
import { OperationMappingTable } from './OperationMappingTable';
import { OperationDocDrawer } from './OperationDocDrawer';

export const ProtoSchemaWorkbenchPage: React.FC = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const { user } = useModuleUser();
  const toast = useToast();
  const isAdmin = Boolean(user?.is_module_admin || user?.is_super_admin);

  const { page, pageSize, setPage, setPageSize } = usePaginationState({
    defaultPageSize: 25,
  });

  const searchKeyword = searchParams.get('search') || '';
  const activeOnly = searchParams.get('active_only') === 'true';

  const [schemas, setSchemas] = useState<ProtoSchema[]>([]);
  const [selectedSchemaId, setSelectedSchemaId] = useState<string>('');
  const [operations, setOperations] = useState<ProtoOperation[]>([]);
  const [totalOperations, setTotalOperations] = useState(0);
  const [loading, setLoading] = useState(false);

  // 弹窗状态
  const [uploadModalOpen, setUploadModalOpen] = useState(false);
  const [docDrawerOpen, setDocDrawerOpen] = useState(false);
  const [selectedOp, setSelectedOp] = useState<ProtoOperation | null>(null);

  // 409 冲突弹窗
  const [conflictModalOpen, setConflictModalOpen] = useState(false);
  const [conflicts, setConflicts] = useState<SchemaConflict[]>([]);

  const loadSchemas = useCallback(async () => {
    try {
      const res = await schemaApi.listProtoSchemas({ page_size: 100 });
      const items = res.items ?? [];
      setSchemas(items);
      if (items.length > 0) {
        setSelectedSchemaId((prev) => prev || items[0].id);
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '加载描述集列表失败');
    }
  }, [toast]);

  useEffect(() => {
    void loadSchemas();
  }, [loadSchemas]);

  const loadOperations = useCallback(async () => {
    setLoading(true);
    try {
      const res = await schemaApi.listProtoOperations({
        schema_id: selectedSchemaId || undefined,
        active_only: activeOnly,
        search: searchKeyword || undefined,
        page,
        page_size: pageSize,
      });
      setOperations(res.items ?? []);
      setTotalOperations(res.total ?? 0);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '加载方法映射失败');
    } finally {
      setLoading(false);
    }
  }, [selectedSchemaId, activeOnly, searchKeyword, page, pageSize, toast]);

  useEffect(() => {
    void loadOperations();
  }, [loadOperations]);

  const handleActivate = async (schemaId: string) => {
    try {
      const res = await schemaApi.activateProtoSchema(schemaId);
      toast.success(`描述集已激活，共生效 ${res.activated_operations} 个服务契约`);
      void loadSchemas();
      void loadOperations();
    } catch (err: unknown) {
      const errorMsg = err instanceof Error ? err.message : '激活描述集失败';
      toast.error(errorMsg);
      // 如果后端带出冲突列表，弹窗展示
      setConflicts([]);
      setConflictModalOpen(true);
    }
  };

  const handleDelete = async (schemaId: string) => {
    try {
      await schemaApi.deleteProtoSchema(schemaId);
      toast.success('描述集已删除');
      setSelectedSchemaId('');
      void loadSchemas();
      void loadOperations();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '删除描述集失败');
    }
  };

  const handleSearchChange = (kw: string) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      if (kw) next.set('search', kw);
      else next.delete('search');
      next.set('page', '1');
      return next;
    });
  };

  const handleToggleActiveOnly = (val: boolean) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      if (val) next.set('active_only', 'true');
      else next.delete('active_only');
      next.set('page', '1');
      return next;
    });
  };

  return (
    <div className="code-workbench-page code-schema-workbench" data-testid="proto-schema-workbench">
      <div className="code-workbench-header">
        <div className="code-workbench-title-group">
          <h2 className="code-workbench-title">Protobuf 描述集与方法映射工作台</h2>
          <span className="code-workbench-subtitle">
            上传并解析 .pb 二进制描述集，自动维护 {'{package}-{serviceName}'} 服务契约并原子热切换注册表
          </span>
        </div>

        <div className="code-header-actions">
          {isAdmin && (
            <button
              type="button"
              className="code-btn code-btn--primary"
              onClick={() => setUploadModalOpen(true)}
            >
              + 上传 .pb 描述集
            </button>
          )}
        </div>
      </div>

      <div className="code-schema-layout code-mt-md">
        {/* 左侧：描述集版本时间线 */}
        <div className="code-schema-left-pane">
          <div className="code-pane-header">
            <span className="code-pane-title">已注册描述集版本 ({schemas.length})</span>
          </div>
          <SchemaVersionList
            schemas={schemas}
            selectedSchemaId={selectedSchemaId}
            onSelectSchema={(id) => {
              setSelectedSchemaId(id);
              setPage(1);
            }}
            isAdmin={isAdmin}
            onActivate={(id) => void handleActivate(id)}
            onDelete={(id) => void handleDelete(id)}
          />
        </div>

        {/* 右侧：契约方法映射表 */}
        <div className="code-schema-right-pane">
          <div className="code-pane-header">
            <div className="code-filter-row">
              <input
                type="text"
                className="code-input code-input--sm"
                placeholder="检索 service_name 或 operation..."
                value={searchKeyword}
                onChange={(e) => handleSearchChange(e.target.value)}
              />
              <label className="code-checkbox-chip">
                <input
                  type="checkbox"
                  checked={activeOnly}
                  onChange={(e) => handleToggleActiveOnly(e.target.checked)}
                />
                <span>仅看当前生效契约</span>
              </label>
            </div>
          </div>

          {loading ? (
            <div className="code-tpl-loading-screen">正在加载方法契约映射...</div>
          ) : (
            <OperationMappingTable
              operations={operations}
              onSelectOperation={(op) => {
                setSelectedOp(op);
                setDocDrawerOpen(true);
              }}
            />
          )}

          <div className="code-mt-md">
            <Pagination
              totalItems={totalOperations}
              page={page}
              pageSize={pageSize}
              onPageChange={setPage}
              onPageSizeChange={setPageSize}
            />
          </div>
        </div>
      </div>

      {/* 上传弹窗 */}
      <SchemaUploadModal
        open={uploadModalOpen}
        onClose={() => setUploadModalOpen(false)}
        onSuccess={() => {
          void loadSchemas();
          void loadOperations();
        }}
      />

      {/* 契约详情抽屉 */}
      <OperationDocDrawer
        open={docDrawerOpen}
        onClose={() => setDocDrawerOpen(false)}
        operation={selectedOp}
      />

      {/* 409 契约冲突提示弹窗 */}
      <Modal
        open={conflictModalOpen}
        onClose={() => setConflictModalOpen(false)}
        title="激活冲突拦截 (409 Conflict)"
        footer={
          <div className="code-modal-footer">
            <button
              type="button"
              className="code-btn code-btn--secondary"
              onClick={() => setConflictModalOpen(false)}
            >
              关闭
            </button>
          </div>
        }
      >
        <div className="code-conflict-content">
          <p className="code-color-danger">
            目标描述集中的服务契约与当前其他生效中的描述集发生重叠冲突，原子激活已被安全回滚：
          </p>
          {conflicts.length > 0 ? (
            <div className="code-table-wrapper code-mt-sm">
              <table className="code-table code-table--compact">
                <thead>
                  <tr>
                    <th>服务名称</th>
                    <th>方法名</th>
                    <th>冲突描述集</th>
                    <th>冲突版本</th>
                  </tr>
                </thead>
                <tbody>
                  {(conflicts ?? []).map((c, idx) => (
                    <tr key={idx}>
                      <td className="code-font-mono">{c.service_name}</td>
                      <td className="code-font-mono">{c.operation}</td>
                      <td>{c.conflict_schema_name}</td>
                      <td>v{c.conflict_version}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <div className="code-info-box code-mt-sm">
              请检查目标 .pb 文件是否包含与其他生效 IDL 重复的 <code>{'{package}-{serviceName}'}</code> 契约。
            </div>
          )}
        </div>
      </Modal>
    </div>
  );
};

export default ProtoSchemaWorkbenchPage;
