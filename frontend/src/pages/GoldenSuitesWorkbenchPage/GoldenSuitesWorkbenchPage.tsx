import React, { useCallback, useEffect, useState } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import {
  Sparkles,
  Plus,
  Play,
  FileText,
  Trash2,
  Search,
  RotateCcw,
  CheckCircle2,
  XCircle,
  AlertTriangle,
} from 'lucide-react';
import { Modal, Pagination, usePaginationState } from '@code/common';
import type { Agent, ArchProcess, GoldenSuite, Process, ReplayTask } from '../../types';
import { fetchGoldenSuites, deleteGoldenSuite } from '../../api/golden_suite';
import { listArchProcesses } from '../../api/arch';
import { listAgents, listProcesses } from '../../api/agent';
import { useToast } from '../../utils/toast';
import { GoldenSuiteCreateModal } from './GoldenSuiteCreateModal';
import { GoldenSuiteExecuteModal } from './GoldenSuiteExecuteModal';
import { GoldenSuiteDetailDrawer } from './GoldenSuiteDetailDrawer';

export const GoldenSuitesWorkbenchPage: React.FC = () => {
  const toast = useToast();
  const [searchParams, setSearchParams] = useSearchParams();
  const navigate = useNavigate();

  const { page, pageSize, setPage, setPageSize } = usePaginationState({
    defaultPageSize: 25,
  });

  const urlSearch = searchParams.get('search') || '';
  const urlArchProcessId = searchParams.get('arch_process_id') || '';
  const urlStatus = searchParams.get('status') || '';

  const [suites, setSuites] = useState<GoldenSuite[]>([]);
  const [total, setTotal] = useState<number>(0);
  const [loading, setLoading] = useState<boolean>(false);

  // 依赖字典
  const [archProcesses, setArchProcesses] = useState<ArchProcess[]>([]);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [processes, setProcesses] = useState<Process[]>([]);

  // 弹窗与抽屉状态
  const [createModalOpen, setCreateModalOpen] = useState<boolean>(false);
  const [executeModalOpen, setExecuteModalOpen] = useState<boolean>(false);
  const [executingSuite, setExecutingSuite] = useState<GoldenSuite | null>(null);

  const [detailDrawerOpen, setDetailDrawerOpen] = useState<boolean>(false);
  const [selectedSuiteId, setSelectedSuiteId] = useState<number | null>(null);

  // 删除确认模态窗状态（防误触，零原生 alert/confirm）
  const [deleteConfirmSuite, setDeleteConfirmSuite] = useState<GoldenSuite | null>(null);
  const [deletingSuite, setDeletingSuite] = useState<boolean>(false);

  // 1. 加载依赖字典
  useEffect(() => {
    void (async () => {
      try {
        const [archRes, agentsRes, procRes] = await Promise.all([
          listArchProcesses({ page_size: 200 }),
          listAgents({ page_size: 200 }),
          listProcesses({ page_size: 200 }),
        ]);
        setArchProcesses(archRes.items || []);
        setAgents(agentsRes.items || []);
        setProcesses(procRes.items || []);
      } catch {
        // 静默
      }
    })();
  }, []);

  // 2. 加载套件列表
  const loadSuites = useCallback(async () => {
    setLoading(true);
    try {
      const res = await fetchGoldenSuites({
        search: urlSearch || undefined,
        arch_process_id: urlArchProcessId ? Number(urlArchProcessId) : undefined,
        status: urlStatus || undefined,
        page,
        page_size: pageSize,
      });
      setSuites(res.items || []);
      setTotal(res.total || 0);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '加载黄金用例库失败');
    } finally {
      setLoading(false);
    }
  }, [urlSearch, urlArchProcessId, urlStatus, page, pageSize, toast]);

  useEffect(() => {
    void loadSuites();
  }, [loadSuites]);

  // 3. 筛选变更
  const updateQueryParam = (key: string, val: string) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      if (val) {
        next.set(key, val);
      } else {
        next.delete(key);
      }
      next.set('page', '1');
      return next;
    });
  };

  const handleResetFilters = () => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      next.delete('search');
      next.delete('arch_process_id');
      next.delete('status');
      next.set('page', '1');
      return next;
    });
  };

  // 4. 操作处理
  const handleConfirmDeleteSuite = async () => {
    if (!deleteConfirmSuite) return;
    setDeletingSuite(true);
    try {
      await deleteGoldenSuite(deleteConfirmSuite.id);
      toast.success(`黄金套件 [${deleteConfirmSuite.name}] 已删除`);
      setDeleteConfirmSuite(null);
      void loadSuites();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '删除黄金套件失败');
    } finally {
      setDeletingSuite(false);
    }
  };

  const openExecuteModal = (suite: GoldenSuite) => {
    setExecutingSuite(suite);
    setExecuteModalOpen(true);
  };

  const openDetailDrawer = (suiteId: number) => {
    setSelectedSuiteId(suiteId);
    setDetailDrawerOpen(true);
  };

  const renderReplayStatusBadge = (status?: string) => {
    if (!status) {
      return (
        <span
          style={{
            fontSize: '11px',
            color: 'var(--color-text-tertiary, #94a3b8)',
          }}
        >
          未脱机回归
        </span>
      );
    }

    switch (status) {
      case 'PASS':
        return (
          <span
            style={{
              fontSize: '11px',
              padding: '2px 8px',
              borderRadius: '12px',
              backgroundColor: 'var(--color-success-bg, #ecfdf5)',
              color: 'var(--color-success-text, #059669)',
              fontWeight: 600,
              display: 'inline-flex',
              alignItems: 'center',
              gap: '4px',
            }}
          >
            <CheckCircle2 size={12} />
            PASS
          </span>
        );
      case 'FAILED':
        return (
          <span
            style={{
              fontSize: '11px',
              padding: '2px 8px',
              borderRadius: '12px',
              backgroundColor: 'var(--color-danger-bg, #fef2f2)',
              color: 'var(--color-danger-text, #dc2626)',
              fontWeight: 600,
              display: 'inline-flex',
              alignItems: 'center',
              gap: '4px',
            }}
          >
            <XCircle size={12} />
            FAILED
          </span>
        );
      case 'MOCK_MISS':
        return (
          <span
            style={{
              fontSize: '11px',
              padding: '2px 8px',
              borderRadius: '12px',
              backgroundColor: 'var(--color-warning-bg, #fffbeb)',
              color: 'var(--color-warning-text, #d97706)',
              fontWeight: 600,
              display: 'inline-flex',
              alignItems: 'center',
              gap: '4px',
            }}
          >
            <AlertTriangle size={12} />
            MOCK_MISS
          </span>
        );
      default:
        return (
          <span
            style={{
              fontSize: '11px',
              padding: '2px 8px',
              borderRadius: '12px',
              backgroundColor: 'var(--color-neutral-bg, #f1f5f9)',
              color: 'var(--color-text-secondary, #64748b)',
              fontWeight: 600,
            }}
          >
            {status}
          </span>
        );
    }
  };

  return (
    <div
      className="code-workbench-page code-golden-suites-workbench"
      data-testid="golden-suites-workbench"
    >
      {/* 头部标题与操作 */}
      <div className="code-workbench-header">
        <div className="code-workbench-title-group">
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            <Sparkles size={22} color="var(--color-warning, #d97706)" />
            <h2 className="code-workbench-title">黄金契约套件与脱机回归工作台</h2>
          </div>
          <span className="code-workbench-subtitle">
            将典型流量全景与调用树事务一键沉淀为自包含黄金用例快照，解耦原录制批次与物理分区，实现脱机全序回放与防劣化断言
          </span>
        </div>

        <div className="code-header-actions">
          <button
            type="button"
            className="code-btn code-btn--primary"
            onClick={() => setCreateModalOpen(true)}
            style={{ display: 'inline-flex', alignItems: 'center', gap: '6px' }}
          >
            <Plus size={16} />
            新建黄金契约套件
          </button>
        </div>
      </div>

      {/* 过滤筛选条 */}
      <div
        className="code-pane-header code-mt-sm"
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          flexWrap: 'wrap',
          gap: '12px',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '12px', flexWrap: 'wrap' }}>
          <div style={{ display: 'inline-flex', alignItems: 'center', gap: '6px' }}>
            <Search size={14} color="var(--color-text-secondary, #64748b)" />
            <input
              type="text"
              className="code-input code-input--compact"
              placeholder="搜索套件编码 / 名称..."
              value={urlSearch}
              onChange={(e) => updateQueryParam('search', e.target.value)}
              style={{ width: '200px' }}
            />
          </div>

          <div style={{ display: 'inline-flex', alignItems: 'center', gap: '6px' }}>
            <label className="code-field-label" style={{ fontSize: '12px', margin: 0 }}>
              归属架构进程:
            </label>
            <select
              className="code-select code-select--compact"
              value={urlArchProcessId}
              onChange={(e) => updateQueryParam('arch_process_id', e.target.value)}
              style={{ minWidth: '160px' }}
            >
              <option value="">全部架构进程 (ALL)</option>
              {archProcesses.map((ap) => (
                <option key={ap.id} value={ap.id}>
                  {ap.name} ({ap.code})
                </option>
              ))}
            </select>
          </div>

          <div style={{ display: 'inline-flex', alignItems: 'center', gap: '6px' }}>
            <label className="code-field-label" style={{ fontSize: '12px', margin: 0 }}>
              状态:
            </label>
            <select
              className="code-select code-select--compact"
              value={urlStatus}
              onChange={(e) => updateQueryParam('status', e.target.value)}
            >
              <option value="">全部状态 (ALL)</option>
              <option value="ACTIVE">ACTIVE (生效中)</option>
              <option value="ARCHIVED">ARCHIVED (已归档)</option>
            </select>
          </div>

          {(urlSearch || urlArchProcessId || urlStatus) && (
            <button
              type="button"
              className="code-btn code-btn--secondary code-btn--sm"
              onClick={handleResetFilters}
              style={{ display: 'inline-flex', alignItems: 'center', gap: '4px' }}
            >
              <RotateCcw size={12} />
              重置筛选
            </button>
          )}
        </div>

        <div style={{ fontSize: '12px', color: 'var(--color-text-secondary, #64748b)' }}>
          共检索到 <strong>{total}</strong> 组黄金套件
        </div>
      </div>

      {/* 套件主表 */}
      <div className="code-mt-sm">
        {loading ? (
          <div className="code-tpl-loading-screen" style={{ padding: '40px', textAlign: 'center' }}>
            正在加载黄金契约套件清单...
          </div>
        ) : suites.length === 0 ? (
          <div
            style={{
              padding: '60px 20px',
              textAlign: 'center',
              backgroundColor: 'var(--color-bg-primary, #ffffff)',
              borderRadius: '8px',
              border: '1px dashed var(--color-border, #cbd5e1)',
              color: 'var(--color-text-secondary, #64748b)',
            }}
          >
            <Sparkles size={32} color="var(--color-warning, #d97706)" style={{ margin: '0 auto 12px' }} />
            <h4 style={{ margin: '0 0 6px', fontSize: '16px', color: 'var(--color-text-primary)' }}>
              暂无匹配的黄金契约套件
            </h4>
            <p style={{ margin: '0 0 16px', fontSize: '13px' }}>
              您可以从顶部点击「新建黄金契约套件」，或者前往「流量全景与调用树」(/debug) 中点击「🌟 沉淀为黄金契约」抽屉一键固化。
            </p>
            <button
              type="button"
              className="code-btn code-btn--primary"
              onClick={() => setCreateModalOpen(true)}
            >
              + 立即创建套件
            </button>
          </div>
        ) : (
          <div className="code-table-container">
            <table className="code-table" data-testid="golden-suites-table">
              <thead>
                <tr>
                  <th style={{ width: '180px' }}>套件编码</th>
                  <th>套件名称 / 描述</th>
                  <th style={{ width: '180px' }}>绑定架构进程</th>
                  <th style={{ width: '80px', textAlign: 'center' }}>版本</th>
                  <th style={{ width: '90px', textAlign: 'center' }}>固化用例</th>
                  <th style={{ width: '130px', textAlign: 'center' }}>最新回归结论</th>
                  <th style={{ width: '90px', textAlign: 'center' }}>状态</th>
                  <th style={{ width: '140px' }}>更新时间</th>
                  <th style={{ width: '220px', textAlign: 'right' }}>操作</th>
                </tr>
              </thead>
              <tbody>
                {suites.map((suite) => {
                  const ap =
                    suite.arch_process ||
                    archProcesses.find((p) => p.id === suite.arch_process_id);
                  return (
                    <tr key={suite.id}>
                      <td>
                        <span
                          style={{
                            fontFamily: 'monospace',
                            fontSize: '12px',
                            fontWeight: 600,
                            color: 'var(--color-text-primary, #0f172a)',
                          }}
                        >
                          {suite.code}
                        </span>
                      </td>
                      <td>
                        <div style={{ display: 'flex', flexDirection: 'column' }}>
                          <span
                            style={{
                              fontWeight: 600,
                              color: 'var(--color-text-primary, #0f172a)',
                            }}
                          >
                            {suite.name}
                          </span>
                          {suite.description && (
                            <span
                              style={{
                                fontSize: '12px',
                                color: 'var(--color-text-secondary, #64748b)',
                                overflow: 'hidden',
                                textOverflow: 'ellipsis',
                                whiteSpace: 'nowrap',
                                maxWidth: '320px',
                              }}
                            >
                              {suite.description}
                            </span>
                          )}
                        </div>
                      </td>
                      <td>
                        <div style={{ display: 'flex', flexDirection: 'column' }}>
                          <span style={{ fontSize: '13px', color: 'var(--color-text-primary)' }}>
                            {ap?.name || `进程 #${suite.arch_process_id}`}
                          </span>
                          <span
                            style={{
                              fontSize: '11px',
                              fontFamily: 'monospace',
                              color: 'var(--color-text-secondary)',
                            }}
                          >
                            {ap?.default_cmd_pattern || ap?.code}
                          </span>
                        </div>
                      </td>
                      <td style={{ textAlign: 'center' }}>
                        <span
                          style={{
                            fontSize: '11px',
                            padding: '2px 6px',
                            borderRadius: '4px',
                            backgroundColor: 'var(--color-primary-bg, #eff6ff)',
                            color: 'var(--color-primary, #2563eb)',
                            fontWeight: 500,
                          }}
                        >
                          {suite.version}
                        </span>
                      </td>
                      <td style={{ textAlign: 'center' }}>
                        <span
                          style={{
                            fontSize: '12px',
                            fontWeight: 700,
                            color: 'var(--color-primary, #2563eb)',
                          }}
                        >
                          {suite.item_count} 条
                        </span>
                      </td>
                      <td style={{ textAlign: 'center' }}>
                        {renderReplayStatusBadge(suite.last_replay_status)}
                      </td>
                      <td style={{ textAlign: 'center' }}>
                        <span
                          style={{
                            fontSize: '11px',
                            padding: '2px 8px',
                            borderRadius: '12px',
                            backgroundColor:
                              suite.status === 'ACTIVE'
                                ? 'var(--color-success-bg, #ecfdf5)'
                                : 'var(--color-neutral-bg, #f1f5f9)',
                            color:
                              suite.status === 'ACTIVE'
                                ? 'var(--color-success-text, #059669)'
                                : 'var(--color-text-secondary, #64748b)',
                            fontWeight: 600,
                          }}
                        >
                          {suite.status === 'ACTIVE' ? '生效中' : '已归档'}
                        </span>
                      </td>
                      <td style={{ fontSize: '12px', color: 'var(--color-text-secondary, #64748b)' }}>
                        {suite.updated_at
                          ? new Date(suite.updated_at).toLocaleString()
                          : '-'}
                      </td>
                      <td style={{ textAlign: 'right' }}>
                        <div
                          style={{
                            display: 'inline-flex',
                            alignItems: 'center',
                            gap: '6px',
                            justifyContent: 'flex-end',
                          }}
                        >
                          <button
                            type="button"
                            className="code-btn code-btn--secondary code-btn--sm"
                            onClick={() => openDetailDrawer(suite.id)}
                            style={{ display: 'inline-flex', alignItems: 'center', gap: '4px' }}
                            data-testid={`btn-detail-${suite.id}`}
                          >
                            <FileText size={13} />
                            详情与用例
                          </button>
                          <button
                            type="button"
                            className="code-btn code-btn--primary code-btn--sm"
                            onClick={() => openExecuteModal(suite)}
                            style={{ display: 'inline-flex', alignItems: 'center', gap: '4px' }}
                            data-testid={`btn-execute-${suite.id}`}
                          >
                            <Play size={13} />
                            脱机回归
                          </button>
                          <button
                            type="button"
                            className="code-btn code-btn--danger code-btn--sm"
                            onClick={() => setDeleteConfirmSuite(suite)}
                            title="删除套件"
                            data-testid={`btn-delete-${suite.id}`}
                          >
                            <Trash2 size={13} />
                          </button>
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}

        {/* 分页组件 */}
        {total > 0 && (
          <div className="code-mt-sm code-flex-end">
            <Pagination
              page={page}
              pageSize={pageSize}
              totalItems={total}
              onPageChange={setPage}
              onPageSizeChange={setPageSize}
              pageSizeOptions={[15, 25, 50, 100]}
            />
          </div>
        )}
      </div>

      {/* 新建模态窗 */}
      <GoldenSuiteCreateModal
        open={createModalOpen}
        onClose={() => setCreateModalOpen(false)}
        archProcesses={archProcesses}
        onCreated={() => {
          void loadSuites();
        }}
      />

      {/* 脱机执行模态窗 */}
      <GoldenSuiteExecuteModal
        open={executeModalOpen}
        onClose={() => setExecuteModalOpen(false)}
        suite={executingSuite}
        allAgents={agents}
        allProcesses={processes}
        onExecuted={(task: ReplayTask) => {
          setExecuteModalOpen(false);
          void loadSuites();
          navigate(`/replay?task_id=${task.id}`);
        }}
      />

      {/* 套件详情抽屉 */}
      <GoldenSuiteDetailDrawer
        open={detailDrawerOpen}
        onClose={() => setDetailDrawerOpen(false)}
        suiteId={selectedSuiteId}
        onSuiteUpdated={() => {
          void loadSuites();
        }}
        onExecuteSuite={(suite) => {
          setDetailDrawerOpen(false);
          openExecuteModal(suite);
        }}
      />

      {/* 自定义删除确认模态框（防误触，零原生 alert/confirm） */}
      {deleteConfirmSuite && (
        <Modal
          open={Boolean(deleteConfirmSuite)}
          onClose={() => setDeleteConfirmSuite(null)}
          title="删除黄金契约套件确认"
          width="sm"
          footer={
            <div className="code-modal-footer">
              <button
                type="button"
                className="code-btn code-btn--secondary"
                onClick={() => setDeleteConfirmSuite(null)}
                disabled={deletingSuite}
              >
                取消
              </button>
              <button
                type="button"
                className="code-btn code-btn--danger"
                disabled={deletingSuite}
                onClick={() => void handleConfirmDeleteSuite()}
                data-testid="btn-confirm-delete-suite"
              >
                {deletingSuite ? '删除中...' : '确认彻底删除'}
              </button>
            </div>
          }
        >
          <div className="code-p-md" data-testid="delete-suite-confirm-dialog">
            <p>
              确定要永久删除黄金契约套件 <strong>{deleteConfirmSuite.name}</strong> (
              <code>{deleteConfirmSuite.code}</code>) 吗？
            </p>
            <p style={{ color: 'var(--color-danger, #ef4444)', fontSize: '12px', marginTop: '8px' }}>
              注意：套件下包含的全部自包含快照用例及 Mock 规则将一并物理删除，且不可恢复！
            </p>
          </div>
        </Modal>
      )}
    </div>
  );
};
