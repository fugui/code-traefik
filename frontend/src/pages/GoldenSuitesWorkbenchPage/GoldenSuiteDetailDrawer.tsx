import React, { useCallback, useEffect, useState } from 'react';
import {
  X,
  Sparkles,
  Play,
  Trash2,
  Edit2,
  Check,
  Plus,
  Shield,
  Layers,
} from 'lucide-react';
import { Modal } from '@code/common';
import type { GoldenSuite, GoldenSuiteItemSummary, MaskRule } from '../../types';
import {
  fetchGoldenSuite,
  updateGoldenSuite,
  updateGoldenSuiteItem,
  deleteGoldenSuiteItem,
} from '../../api/golden_suite';
import { useToast } from '../../utils/toast';

interface GoldenSuiteDetailDrawerProps {
  open: boolean;
  onClose: () => void;
  suiteId: number | null;
  onSuiteUpdated?: () => void;
  onExecuteSuite?: (suite: GoldenSuite) => void;
}

export const GoldenSuiteDetailDrawer: React.FC<GoldenSuiteDetailDrawerProps> = ({
  open,
  onClose,
  suiteId,
  onSuiteUpdated,
  onExecuteSuite,
}) => {
  const toast = useToast();
  const [suite, setSuite] = useState<GoldenSuite | null>(null);
  const [items, setItems] = useState<GoldenSuiteItemSummary[]>([]);
  const [loading, setLoading] = useState<boolean>(false);

  // 编辑套件元信息状态
  const [isEditingSuite, setIsEditingSuite] = useState<boolean>(false);
  const [editName, setEditName] = useState<string>('');
  const [editVersion, setEditVersion] = useState<string>('');
  const [editStatus, setEditStatus] = useState<'ACTIVE' | 'ARCHIVED'>('ACTIVE');
  const [editIgnorePaths, setEditIgnorePaths] = useState<string>('');
  const [editDescription, setEditDescription] = useState<string>('');
  const [savingSuite, setSavingSuite] = useState<boolean>(false);

  // 编辑单项掩码状态
  const [editingItemId, setEditingItemId] = useState<number | null>(null);
  const [itemMaskRules, setItemMaskRules] = useState<MaskRule[]>([]);
  const [newRulePath, setNewRulePath] = useState<string>('');
  const [newRuleStrategy, setNewRuleStrategy] = useState<'IGNORE' | 'REGEX' | 'FIXED'>('IGNORE');
  const [newRulePattern, setNewRulePattern] = useState<string>('');
  const [savingItem, setSavingItem] = useState<boolean>(false);

  // 删除确认模态窗状态（防误触，零原生 alert/confirm）
  const [deleteConfirmItem, setDeleteConfirmItem] = useState<GoldenSuiteItemSummary | null>(null);
  const [deletingItem, setDeletingItem] = useState<boolean>(false);

  const loadDetail = useCallback(async () => {
    if (!suiteId) return;
    setLoading(true);
    try {
      const res = await fetchGoldenSuite(suiteId);
      setSuite(res.suite);
      setItems(res.items || []);
      setEditName(res.suite.name);
      setEditVersion(res.suite.version);
      setEditStatus(res.suite.status);
      setEditIgnorePaths((res.suite.extra_ignore_paths || []).join(', '));
      setEditDescription(res.suite.description || '');
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '获取黄金套件详情失败');
    } finally {
      setLoading(false);
    }
  }, [suiteId, toast]);

  useEffect(() => {
    if (open && suiteId) {
      setIsEditingSuite(false);
      setEditingItemId(null);
      void loadDetail();
    } else {
      setSuite(null);
      setItems([]);
    }
  }, [open, suiteId, loadDetail]);

  if (!open) return null;

  const handleSaveSuite = async () => {
    if (!suite) return;
    if (!editName.trim()) {
      toast.error('套件名称不能为空');
      return;
    }

    setSavingSuite(true);
    try {
      const paths = editIgnorePaths
        .split(',')
        .map((p) => p.trim())
        .filter(Boolean);

      const updated = await updateGoldenSuite(suite.id, {
        name: editName.trim(),
        version: editVersion.trim(),
        status: editStatus,
        extra_ignore_paths: paths,
        description: editDescription.trim(),
      });

      setSuite(updated);
      setIsEditingSuite(false);
      toast.success('黄金套件元信息已更新');
      if (onSuiteUpdated) onSuiteUpdated();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '更新套件信息失败');
    } finally {
      setSavingSuite(false);
    }
  };

  const handleConfirmDeleteItem = async () => {
    if (!suite || !deleteConfirmItem) return;
    setDeletingItem(true);
    try {
      await deleteGoldenSuiteItem(suite.id, deleteConfirmItem.id);
      toast.success(`用例 #${deleteConfirmItem.order_no} 已从套件中移除`);
      setDeleteConfirmItem(null);
      void loadDetail();
      if (onSuiteUpdated) onSuiteUpdated();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '移除用例失败');
    } finally {
      setDeletingItem(false);
    }
  };

  const startEditItemMask = (item: GoldenSuiteItemSummary) => {
    setEditingItemId(item.id);
    setItemMaskRules(item.mask_rules_json ? [...item.mask_rules_json] : []);
    setNewRulePath('');
    setNewRulePattern('');
  };

  const handleAddMaskRule = () => {
    if (!newRulePath.trim()) return;
    setItemMaskRules((prev) => [
      ...prev,
      {
        path: newRulePath.trim(),
        strategy: newRuleStrategy,
        pattern: newRulePattern.trim() || undefined,
      },
    ]);
    setNewRulePath('');
    setNewRulePattern('');
  };

  const handleRemoveMaskRule = (idx: number) => {
    setItemMaskRules((prev) => prev.filter((_, i) => i !== idx));
  };

  const handleSaveItemMask = async (itemId: number) => {
    if (!suite) return;
    setSavingItem(true);
    try {
      await updateGoldenSuiteItem(suite.id, itemId, {
        mask_rules_json: itemMaskRules,
      });
      toast.success('用例字段级掩码已保存');
      setEditingItemId(null);
      void loadDetail();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '保存掩码规则失败');
    } finally {
      setSavingItem(false);
    }
  };

  return (
    <div
      className="code-drawer-mask"
      style={{
        position: 'fixed',
        top: 0,
        left: 0,
        right: 0,
        bottom: 0,
        backgroundColor: 'rgba(15, 23, 42, 0.45)',
        backdropFilter: 'blur(2px)',
        zIndex: 1000,
        display: 'flex',
        justifyContent: 'flex-end',
      }}
      onClick={onClose}
    >
      <aside
        className="code-drawer-panel"
        style={{
          width: '720px',
          maxWidth: '94vw',
          height: '100%',
          backgroundColor: 'var(--color-bg-primary, #ffffff)',
          borderLeft: '1px solid var(--color-border, #e2e8f0)',
          boxShadow: '-8px 0 24px rgba(0, 0, 0, 0.12)',
          display: 'flex',
          flexDirection: 'column',
          zIndex: 1001,
        }}
        onClick={(e) => e.stopPropagation()}
        data-testid="golden-suite-detail-drawer"
      >
        {/* 抽屉顶栏 */}
        <div
          style={{
            padding: '16px 20px',
            borderBottom: '1px solid var(--color-border, #e2e8f0)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            backgroundColor: 'var(--color-bg-secondary, #f8fafc)',
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            <Sparkles size={20} color="var(--color-warning, #d97706)" />
            <h3
              style={{
                margin: 0,
                fontSize: '16px',
                fontWeight: 600,
                color: 'var(--color-text-primary, #0f172a)',
              }}
            >
              黄金契约用例套件详情
            </h3>
            {suite && (
              <span
                style={{
                  fontSize: '12px',
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
            )}
          </div>

          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            {suite && onExecuteSuite && (
              <button
                type="button"
                className="code-btn code-btn--primary code-btn--sm"
                onClick={() => onExecuteSuite(suite)}
                style={{ display: 'inline-flex', alignItems: 'center', gap: '4px' }}
              >
                <Play size={14} />
                脱机回归
              </button>
            )}
            <button
              type="button"
              className="code-btn-icon"
              onClick={onClose}
              title="关闭"
              style={{
                background: 'none',
                border: 'none',
                cursor: 'pointer',
                color: 'var(--color-text-secondary, #64748b)',
              }}
            >
              <X size={18} />
            </button>
          </div>
        </div>

        {/* 抽屉主体 */}
        <div style={{ padding: '20px', overflowY: 'auto', flex: 1 }}>
          {loading ? (
            <div
              style={{
                padding: '40px',
                textAlign: 'center',
                color: 'var(--color-text-secondary, #64748b)',
              }}
            >
              正在读取套件自包含快照与用例账本...
            </div>
          ) : !suite ? (
            <div style={{ padding: '40px', textAlign: 'center' }}>未找到套件数据</div>
          ) : (
            <>
              {/* 套件元信息卡片 */}
              <div
                style={{
                  padding: '16px',
                  border: '1px solid var(--color-border, #e2e8f0)',
                  borderRadius: '8px',
                  backgroundColor: 'var(--color-bg-secondary, #f8fafc)',
                  marginBottom: '20px',
                }}
              >
                <div
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    marginBottom: '12px',
                  }}
                >
                  <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                    <span
                      style={{
                        fontSize: '11px',
                        fontFamily: 'monospace',
                        padding: '2px 6px',
                        borderRadius: '4px',
                        backgroundColor: 'var(--color-bg-tertiary, #e2e8f0)',
                        color: 'var(--color-text-secondary, #475569)',
                      }}
                    >
                      {suite.code}
                    </span>
                    <span
                      style={{
                        fontSize: '16px',
                        fontWeight: 600,
                        color: 'var(--color-text-primary, #0f172a)',
                      }}
                    >
                      {suite.name}
                    </span>
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
                  </div>

                  {!isEditingSuite ? (
                    <button
                      type="button"
                      className="code-btn code-btn--secondary code-btn--sm"
                      onClick={() => setIsEditingSuite(true)}
                      style={{ display: 'inline-flex', alignItems: 'center', gap: '4px' }}
                    >
                      <Edit2 size={13} />
                      编辑配置
                    </button>
                  ) : (
                    <div style={{ display: 'flex', gap: '6px' }}>
                      <button
                        type="button"
                        className="code-btn code-btn--primary code-btn--sm"
                        disabled={savingSuite}
                        onClick={handleSaveSuite}
                        style={{ display: 'inline-flex', alignItems: 'center', gap: '4px' }}
                      >
                        <Check size={13} />
                        保存
                      </button>
                      <button
                        type="button"
                        className="code-btn code-btn--secondary code-btn--sm"
                        onClick={() => setIsEditingSuite(false)}
                      >
                        取消
                      </button>
                    </div>
                  )}
                </div>

                {isEditingSuite ? (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '10px' }}>
                    <div>
                      <label style={{ fontSize: '12px', color: 'var(--color-text-secondary)' }}>
                        套件名称:
                      </label>
                      <input
                        type="text"
                        className="code-input"
                        value={editName}
                        onChange={(e) => setEditName(e.target.value)}
                        style={{ width: '100%', marginTop: '4px' }}
                      />
                    </div>
                    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px' }}>
                      <div>
                        <label style={{ fontSize: '12px', color: 'var(--color-text-secondary)' }}>
                          版本标识:
                        </label>
                        <input
                          type="text"
                          className="code-input"
                          value={editVersion}
                          onChange={(e) => setEditVersion(e.target.value)}
                          style={{ width: '100%', marginTop: '4px' }}
                        />
                      </div>
                      <div>
                        <label style={{ fontSize: '12px', color: 'var(--color-text-secondary)' }}>
                          生命周期状态:
                        </label>
                        <select
                          className="code-select"
                          value={editStatus}
                          onChange={(e) =>
                            setEditStatus(e.target.value as 'ACTIVE' | 'ARCHIVED')
                          }
                          style={{ width: '100%', marginTop: '4px' }}
                        >
                          <option value="ACTIVE">ACTIVE (生效中)</option>
                          <option value="ARCHIVED">ARCHIVED (已归档)</option>
                        </select>
                      </div>
                    </div>
                    <div>
                      <label style={{ fontSize: '12px', color: 'var(--color-text-secondary)' }}>
                        全局 Diff 忽略路径 (英文逗号分隔):
                      </label>
                      <input
                        type="text"
                        className="code-input"
                        value={editIgnorePaths}
                        onChange={(e) => setEditIgnorePaths(e.target.value)}
                        placeholder="例如: rsp.timestamp, rsp.trace_id"
                        style={{ width: '100%', marginTop: '4px' }}
                      />
                    </div>
                    <div>
                      <label style={{ fontSize: '12px', color: 'var(--color-text-secondary)' }}>
                        套件描述:
                      </label>
                      <textarea
                        className="code-textarea"
                        value={editDescription}
                        onChange={(e) => setEditDescription(e.target.value)}
                        rows={2}
                        style={{ width: '100%', marginTop: '4px', resize: 'vertical' }}
                      />
                    </div>
                  </div>
                ) : (
                  <>
                    <div
                      style={{
                        display: 'grid',
                        gridTemplateColumns: 'repeat(2, 1fr)',
                        gap: '10px',
                        fontSize: '12px',
                        color: 'var(--color-text-secondary, #64748b)',
                      }}
                    >
                      <div>
                        <strong>绑定标准架构进程: </strong>
                        <span style={{ color: 'var(--color-text-primary, #0f172a)' }}>
                          {suite.arch_process?.name || `进程 ID: ${suite.arch_process_id}`}
                          {suite.arch_process?.code && ` (${suite.arch_process.code})`}
                        </span>
                      </div>
                      <div>
                        <strong>固化用例总数: </strong>
                        <span style={{ color: 'var(--color-primary, #2563eb)', fontWeight: 600 }}>
                          {suite.item_count || items.length} 条
                        </span>
                      </div>
                    </div>

                    {suite.description && (
                      <div
                        style={{
                          marginTop: '8px',
                          fontSize: '12px',
                          color: 'var(--color-text-secondary, #475569)',
                        }}
                      >
                        <strong>套件说明: </strong>
                        {suite.description}
                      </div>
                    )}

                    {/* 全局忽略路径标签 */}
                    {suite.extra_ignore_paths && suite.extra_ignore_paths.length > 0 && (
                      <div style={{ marginTop: '10px', display: 'flex', alignItems: 'center', gap: '6px', flexWrap: 'wrap' }}>
                        <span style={{ fontSize: '11px', color: 'var(--color-text-secondary, #64748b)' }}>
                          全局忽略字段:
                        </span>
                        {suite.extra_ignore_paths.map((p) => (
                          <span
                            key={p}
                            style={{
                              fontSize: '11px',
                              fontFamily: 'monospace',
                              padding: '1px 6px',
                              borderRadius: '4px',
                              backgroundColor: 'var(--color-neutral-bg, #f1f5f9)',
                              border: '1px solid var(--color-border, #e2e8f0)',
                              color: 'var(--color-text-primary, #0f172a)',
                            }}
                          >
                            {p}
                          </span>
                        ))}
                      </div>
                    )}
                  </>
                )}
              </div>

              {/* 用例项账本 */}
              <div style={{ marginBottom: '16px' }}>
                <div
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    marginBottom: '10px',
                  }}
                >
                  <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                    <Layers size={16} color="var(--color-primary, #2563eb)" />
                    <span
                      style={{
                        fontSize: '14px',
                        fontWeight: 600,
                        color: 'var(--color-text-primary, #0f172a)',
                      }}
                    >
                      固化自包含契约用例清单 ({items.length})
                    </span>
                  </div>
                  <span style={{ fontSize: '12px', color: 'var(--color-text-secondary, #64748b)' }}>
                    脱机回归将严格按序号顺序回放
                  </span>
                </div>

                {items.length === 0 ? (
                  <div
                    style={{
                      padding: '30px',
                      textAlign: 'center',
                      border: '1px dashed var(--color-border, #cbd5e1)',
                      borderRadius: '8px',
                      color: 'var(--color-text-secondary, #94a3b8)',
                      fontSize: '13px',
                    }}
                  >
                    暂无固化用例。可在【流量全景与调用树】工作台右键或点击「🌟 沉淀为黄金契约」抽屉一键固化。
                  </div>
                ) : (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
                    {items.map((item) => {
                      const isEditingThis = editingItemId === item.id;
                      return (
                        <div
                          key={item.id}
                          style={{
                            border: '1px solid var(--color-border, #e2e8f0)',
                            borderRadius: '8px',
                            backgroundColor: 'var(--color-bg-primary, #ffffff)',
                            padding: '14px',
                            boxShadow: '0 1px 3px rgba(0,0,0,0.03)',
                          }}
                        >
                          <div
                            style={{
                              display: 'flex',
                              alignItems: 'flex-start',
                              justifyContent: 'space-between',
                              marginBottom: '8px',
                            }}
                          >
                            <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                              <span
                                style={{
                                  fontSize: '12px',
                                  fontWeight: 700,
                                  color: 'var(--color-primary, #2563eb)',
                                  backgroundColor: 'var(--color-primary-bg, #eff6ff)',
                                  padding: '2px 8px',
                                  borderRadius: '4px',
                                }}
                              >
                                #{item.order_no}
                              </span>
                              <span
                                style={{
                                  fontSize: '14px',
                                  fontWeight: 600,
                                  color: 'var(--color-text-primary, #0f172a)',
                                }}
                              >
                                {item.name || item.operation}
                              </span>
                              <span
                                style={{
                                  fontSize: '11px',
                                  padding: '2px 6px',
                                  borderRadius: '4px',
                                  backgroundColor:
                                    item.baseline_status === '0' || item.baseline_status === 'SUCCESS'
                                      ? 'var(--color-success-bg, #ecfdf5)'
                                      : 'var(--color-warning-bg, #fffbeb)',
                                  color:
                                    item.baseline_status === '0' || item.baseline_status === 'SUCCESS'
                                      ? 'var(--color-success-text, #059669)'
                                      : 'var(--color-warning-text, #b45309)',
                                  fontWeight: 600,
                                }}
                              >
                                基准: {item.baseline_status}
                              </span>
                            </div>

                            <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                              <button
                                type="button"
                                className="code-btn code-btn--secondary code-btn--sm"
                                onClick={() =>
                                  isEditingThis
                                    ? setEditingItemId(null)
                                    : startEditItemMask(item)
                                }
                                style={{ display: 'inline-flex', alignItems: 'center', gap: '4px' }}
                              >
                                <Shield size={12} />
                                {isEditingThis ? '关闭掩码' : '字段掩码'}
                              </button>
                              <button
                                type="button"
                                className="code-btn code-btn--danger code-btn--sm"
                                onClick={() => setDeleteConfirmItem(item)}
                                title="从套件中移除该用例"
                                style={{
                                  display: 'inline-flex',
                                  alignItems: 'center',
                                  gap: '4px',
                                  color: 'var(--color-danger, #ef4444)',
                                }}
                              >
                                <Trash2 size={13} />
                              </button>
                            </div>
                          </div>

                          {/* 详细指标与下游 Mock 标签 */}
                          <div
                            style={{
                              display: 'flex',
                              alignItems: 'center',
                              gap: '16px',
                              fontSize: '12px',
                              color: 'var(--color-text-secondary, #64748b)',
                              marginBottom: '8px',
                            }}
                          >
                            <div>
                              服务接口:{' '}
                              <span
                                style={{
                                  fontFamily: 'monospace',
                                  color: 'var(--color-text-primary, #0f172a)',
                                }}
                              >
                                {item.service_name}::{item.operation}
                              </span>
                            </div>
                            <div>
                              报文大小: {item.raw_req_len}B / {item.raw_rsp_len}B ({item.rsp_seg_count} 段)
                            </div>
                            <div>
                              专属 Mock:{' '}
                              <span
                                style={{
                                  color:
                                    item.scoped_mock_count > 0
                                      ? 'var(--color-primary, #2563eb)'
                                      : 'var(--color-text-secondary)',
                                  fontWeight: item.scoped_mock_count > 0 ? 600 : 400,
                                }}
                              >
                                {item.scoped_mock_count} 组规约
                              </span>
                            </div>
                          </div>

                          {/* 字段掩码规则预览 */}
                          {item.mask_rules_json && item.mask_rules_json.length > 0 && (
                            <div
                              style={{
                                display: 'flex',
                                alignItems: 'center',
                                gap: '6px',
                                flexWrap: 'wrap',
                                marginBottom: isEditingThis ? '12px' : '0',
                              }}
                            >
                              <span style={{ fontSize: '11px', color: 'var(--color-text-secondary)' }}>
                                动态掩码:
                              </span>
                              {item.mask_rules_json.map((r, rIdx) => (
                                <span
                                  key={rIdx}
                                  style={{
                                    fontSize: '11px',
                                    padding: '1px 6px',
                                    borderRadius: '4px',
                                    backgroundColor: 'var(--color-info-bg, #eff6ff)',
                                    border: '1px solid var(--color-info-border, #bfdbfe)',
                                    color: 'var(--color-primary, #1d4ed8)',
                                  }}
                                >
                                  {r.path} [{r.strategy}]
                                </span>
                              ))}
                            </div>
                          )}

                          {/* 掩码编辑面板 */}
                          {isEditingThis && (
                            <div
                              style={{
                                marginTop: '10px',
                                padding: '12px',
                                backgroundColor: 'var(--color-bg-secondary, #f8fafc)',
                                borderRadius: '6px',
                                border: '1px solid var(--color-border, #e2e8f0)',
                              }}
                            >
                              <div
                                style={{
                                  fontSize: '12px',
                                  fontWeight: 600,
                                  marginBottom: '8px',
                                  color: 'var(--color-text-primary, #0f172a)',
                                }}
                              >
                                编辑字段级对比掩码规则 (MaskRules)
                              </div>

                              {itemMaskRules.length > 0 ? (
                                <div
                                  style={{
                                    display: 'flex',
                                    flexDirection: 'column',
                                    gap: '6px',
                                    marginBottom: '10px',
                                  }}
                                >
                                  {itemMaskRules.map((mr, mIdx) => (
                                    <div
                                      key={mIdx}
                                      style={{
                                        display: 'flex',
                                        alignItems: 'center',
                                        justifyContent: 'space-between',
                                        padding: '4px 8px',
                                        backgroundColor: 'var(--color-bg-primary, #ffffff)',
                                        border: '1px solid var(--color-border, #e2e8f0)',
                                        borderRadius: '4px',
                                        fontSize: '12px',
                                      }}
                                    >
                                      <span>
                                        <code style={{ color: 'var(--color-primary, #2563eb)' }}>
                                          {mr.path}
                                        </code>{' '}
                                        <span style={{ color: 'var(--color-text-secondary)' }}>
                                          策略: {mr.strategy}
                                          {mr.pattern ? ` (${mr.pattern})` : ''}
                                        </span>
                                      </span>
                                      <button
                                        type="button"
                                        onClick={() => handleRemoveMaskRule(mIdx)}
                                        style={{
                                          background: 'none',
                                          border: 'none',
                                          cursor: 'pointer',
                                          color: 'var(--color-danger, #ef4444)',
                                        }}
                                      >
                                        <X size={14} />
                                      </button>
                                    </div>
                                  ))}
                                </div>
                              ) : (
                                <div
                                  style={{
                                    fontSize: '11px',
                                    color: 'var(--color-text-secondary, #64748b)',
                                    marginBottom: '8px',
                                  }}
                                >
                                  暂无特定掩码规则（将应用全局忽略配置）
                                </div>
                              )}

                              {/* 添加掩码输入条 */}
                              <div
                                style={{
                                  display: 'flex',
                                  alignItems: 'center',
                                  gap: '8px',
                                  marginBottom: '10px',
                                }}
                              >
                                <input
                                  type="text"
                                  className="code-input"
                                  placeholder="字段路径如 rsp.seq_id"
                                  value={newRulePath}
                                  onChange={(e) => setNewRulePath(e.target.value)}
                                  style={{ flex: 1, fontSize: '12px' }}
                                />
                                <select
                                  className="code-select"
                                  value={newRuleStrategy}
                                  onChange={(e) =>
                                    setNewRuleStrategy(
                                      e.target.value as 'IGNORE' | 'REGEX' | 'FIXED'
                                    )
                                  }
                                  style={{ width: '100px', fontSize: '12px' }}
                                >
                                  <option value="IGNORE">IGNORE</option>
                                  <option value="REGEX">REGEX</option>
                                  <option value="FIXED">FIXED</option>
                                </select>
                                <button
                                  type="button"
                                  className="code-btn code-btn--secondary code-btn--sm"
                                  onClick={handleAddMaskRule}
                                  style={{ display: 'inline-flex', alignItems: 'center', gap: '4px' }}
                                >
                                  <Plus size={12} />
                                  添加
                                </button>
                              </div>

                              <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '8px' }}>
                                <button
                                  type="button"
                                  className="code-btn code-btn--primary code-btn--sm"
                                  disabled={savingItem}
                                  onClick={() => handleSaveItemMask(item.id)}
                                >
                                  {savingItem ? '保存中...' : '保存掩码'}
                                </button>
                              </div>
                            </div>
                          )}
                        </div>
                      );
                    })}
                  </div>
                )}
              </div>
            </>
          )}
        </div>

        {/* 自定义删除确认模态框（防误触，零原生 alert/confirm） */}
        {deleteConfirmItem && (
          <Modal
            open={Boolean(deleteConfirmItem)}
            onClose={() => setDeleteConfirmItem(null)}
            title="移除黄金契约用例确认"
            width="sm"
            footer={
              <div className="code-modal-footer">
                <button
                  type="button"
                  className="code-btn code-btn--secondary"
                  onClick={() => setDeleteConfirmItem(null)}
                  disabled={deletingItem}
                >
                  取消
                </button>
                <button
                  type="button"
                  className="code-btn code-btn--danger"
                  disabled={deletingItem}
                  onClick={() => void handleConfirmDeleteItem()}
                  data-testid="btn-confirm-delete-suite-item"
                >
                  {deletingItem ? '移除中...' : '确认移除'}
                </button>
              </div>
            }
          >
            <div className="code-p-md">
              <p>
                确定要从套件中移除用例{' '}
                <strong>
                  #{deleteConfirmItem.order_no} [{deleteConfirmItem.name || deleteConfirmItem.operation}]
                </strong>{' '}
                吗？
              </p>
            </div>
          </Modal>
        )}
      </aside>
    </div>
  );
};
