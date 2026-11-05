import React, { useEffect, useState } from 'react';
import { Sparkles, X, Plus, Trash2 } from 'lucide-react';
import type {
  ArchProcess,
  GoldenSuite,
  MaskRule,
  Process,
  TrafficRecordSummary,
} from '../../types';
import { curateGoldenSuite, fetchGoldenSuites } from '../../api/golden_suite';
import { useToast } from '../../utils/toast';

interface GoldenCurateDrawerProps {
  open: boolean;
  onClose: () => void;
  record: TrafficRecordSummary | null;
  sessionId: number;
  allProcesses?: Process[];
  archProcesses?: ArchProcess[];
  relatedRecordIds?: number[];
  onSuccess?: (suite: GoldenSuite) => void;
}

export const GoldenCurateDrawer: React.FC<GoldenCurateDrawerProps> = ({
  open,
  onClose,
  record,
  sessionId,
  allProcesses = [],
  archProcesses = [],
  relatedRecordIds = [],
  onSuccess,
}) => {
  const toast = useToast();
  const [curateMode, setCurateMode] = useState<'EXISTING' | 'NEW'>('NEW');
  const [existingSuites, setExistingSuites] = useState<GoldenSuite[]>([]);
  const [selectedSuiteId, setSelectedSuiteId] = useState<number | undefined>();

  // 新建套件表单
  const [newCode, setNewCode] = useState<string>('');
  const [newName, setNewName] = useState<string>('');
  const [newVersion, setNewVersion] = useState<string>('v1.0.0');
  const [newDescription, setNewDescription] = useState<string>('');
  const [newExtraIgnorePaths, setNewExtraIgnorePaths] = useState<string>('rsp.timestamp');

  // 依赖事务勾选与掩码规则
  const [includeChain, setIncludeChain] = useState<boolean>(false);
  const [maskRules, setMaskRules] = useState<MaskRule[]>([
    { path: 'rsp.token', strategy: 'IGNORE' },
  ]);
  const [newMaskPath, setNewMaskPath] = useState<string>('');

  const [submitting, setSubmitting] = useState<boolean>(false);

  // 识别当前记录归属的标准架构进程
  const currentProcess = allProcesses.find((p) => String(p.id) === String(record?.process_id));
  const currentArchProcess = archProcesses.find(
    (ap) => currentProcess?.arch_process_id != null && String(ap.id) === String(currentProcess.arch_process_id)
  );
  const archProcessId = currentArchProcess?.id || currentProcess?.arch_process_id;

  // 打开抽屉时初始化
  useEffect(() => {
    if (!open || !record) return;

    if (currentArchProcess) {
      setNewCode(`GS_${currentArchProcess.code.toUpperCase().replace(/[^A-Z0-9_]/g, '_')}_CORE`);
      setNewName(`${currentArchProcess.name} 黄金契约套件`);
    } else {
      setNewCode(`GS_SUITE_${Date.now()}`);
      setNewName(`黄金契约套件 ${new Date().toLocaleDateString()}`);
    }

    // 加载已有套件列表
    if (archProcessId) {
      fetchGoldenSuites({ arch_process_id: Number(archProcessId), page_size: 100 })
        .then((res) => {
          setExistingSuites(res.items);
          if (res.items.length > 0) {
            setSelectedSuiteId(res.items[0].id);
            setCurateMode('EXISTING');
          } else {
            setCurateMode('NEW');
          }
        })
        .catch(() => {
          setCurateMode('NEW');
        });
    }
  }, [open, record, currentArchProcess, archProcessId]);

  if (!open || !record) return null;

  const targetRecordIds: number[] = [Number(record.id)];
  if (includeChain && relatedRecordIds.length > 0) {
    relatedRecordIds.forEach((rid) => {
      if (!targetRecordIds.includes(rid)) {
        targetRecordIds.push(rid);
      }
    });
  }

  const handleAddMaskRule = () => {
    const trimmed = newMaskPath.trim();
    if (!trimmed) return;
    if (maskRules.some((m) => m.path === trimmed)) {
      toast.info('该掩码字段已在清单中');
      return;
    }
    setMaskRules([...maskRules, { path: trimmed, strategy: 'IGNORE' }]);
    setNewMaskPath('');
  };

  const handleRemoveMaskRule = (idx: number) => {
    setMaskRules(maskRules.filter((_, i) => i !== idx));
  };

  const handleSubmit = async () => {
    if (!sessionId) {
      toast.error('缺失源录制批次 ID');
      return;
    }

    if (curateMode === 'EXISTING' && !selectedSuiteId) {
      toast.error('请选择目标黄金套件');
      return;
    }

    if (curateMode === 'NEW') {
      if (!newCode.trim()) {
        toast.error('请输入全局唯一套件编码');
        return;
      }
      if (!newName.trim()) {
        toast.error('请输入套件名称');
        return;
      }
      if (!archProcessId) {
        toast.error('无法解析目标架构进程，请确保进程已完成蓝图绑定');
        return;
      }
    }

    setSubmitting(true);
    try {
      const extraIgnorePaths = newExtraIgnorePaths
        .split(',')
        .map((p) => p.trim())
        .filter(Boolean);

      const payload = {
        suite_id: curateMode === 'EXISTING' ? selectedSuiteId : undefined,
        new_suite:
          curateMode === 'NEW'
            ? {
                code: newCode.trim(),
                name: newName.trim(),
                arch_process_id: archProcessId as number,
                version: newVersion.trim() || 'v1.0.0',
                extra_ignore_paths: extraIgnorePaths,
                description: newDescription.trim(),
              }
            : undefined,
        session_id: sessionId,
        record_ids: targetRecordIds,
        mask_rules_json: maskRules,
      };

      const res = await curateGoldenSuite(payload);
      toast.success(
        `🌟 成功沉淀 ${res.added_items} 笔黄金契约用例至套件 [${res.suite.name}]！`
      );
      if (onSuccess) {
        onSuccess(res.suite);
      }
      onClose();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '沉淀黄金套件失败');
    } finally {
      setSubmitting(false);
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
          width: '680px',
          maxWidth: '92vw',
          height: '100%',
          backgroundColor: 'var(--color-bg-primary, #ffffff)',
          borderLeft: '1px solid var(--color-border, #e2e8f0)',
          boxShadow: '-8px 0 24px rgba(0, 0, 0, 0.12)',
          display: 'flex',
          flexDirection: 'column',
          zIndex: 1001,
        }}
        onClick={(e) => e.stopPropagation()}
        data-testid="golden-curate-drawer"
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
              沉淀为黄金契约用例快照 (完全自包含)
            </h3>
          </div>
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

        {/* 抽屉主体滚动区 */}
        <div style={{ padding: '20px', overflowY: 'auto', flex: 1 }}>
          {/* 自包含快照防腐提示 */}
          <div
            style={{
              padding: '12px 14px',
              backgroundColor: 'var(--color-info-bg, #ecfeff)',
              border: '1px solid var(--color-info-border, #a5f3fc)',
              borderRadius: '6px',
              marginBottom: '16px',
              fontSize: '12px',
              color: 'var(--color-info-text, #0e7490)',
              lineHeight: 1.5,
            }}
          >
            <strong>💡 完全自包含快照保障 (ADR-14)</strong>：系统将完整深拷贝入站请求帧、预期应答帧及下游专属
            ScopedMocks 规则。即使底层流量分区超过 30 天被物理清理或源录制批次被删除，套件资产依然 100% 独立可回放。
          </div>

          {/* 选定事务卡片 */}
          <div
            style={{
              padding: '14px',
              border: '1px solid var(--color-border, #e2e8f0)',
              borderRadius: '6px',
              backgroundColor: 'var(--color-bg-secondary, #f8fafc)',
              marginBottom: '16px',
            }}
          >
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                marginBottom: '8px',
              }}
            >
              <span
                style={{
                  fontSize: '11px',
                  fontWeight: 600,
                  color: 'var(--color-text-secondary, #64748b)',
                }}
              >
                选定根入站事务 #seq_{record.seq_no}
              </span>
              <span
                style={{
                  fontSize: '11px',
                  padding: '1px 6px',
                  borderRadius: '10px',
                  backgroundColor: 'var(--color-primary-bg, #eff6ff)',
                  color: 'var(--color-primary, #3b82f6)',
                  fontWeight: 600,
                }}
              >
                {record.exchange_type || 'UNARY'}
              </span>
            </div>
            <div style={{ fontSize: '13px', fontWeight: 600, marginBottom: '6px' }}>
              <code>{record.service_name}</code> / <code>{record.operation}</code>
            </div>
            <div
              style={{
                display: 'flex',
                gap: '16px',
                fontSize: '12px',
                color: 'var(--color-text-secondary, #64748b)',
              }}
            >
              <span>耗时: {record.latency_us ? `${record.latency_us}µs` : '-'}</span>
              <span>
                架构标准进程:{' '}
                <strong>{currentArchProcess ? currentArchProcess.name : `ID:${archProcessId || '未知'}`}</strong>
              </span>
            </div>
          </div>

          {/* 目标套件模式切换 */}
          <div style={{ marginBottom: '16px' }}>
            <label
              style={{
                display: 'block',
                fontSize: '13px',
                fontWeight: 600,
                marginBottom: '8px',
              }}
            >
              归属黄金套件
            </label>
            <div
              style={{
                display: 'flex',
                gap: '12px',
                backgroundColor: 'var(--color-bg-tertiary, #f1f5f9)',
                padding: '4px',
                borderRadius: '6px',
                width: 'fit-content',
              }}
            >
              <button
                type="button"
                onClick={() => setCurateMode('NEW')}
                style={{
                  padding: '6px 14px',
                  fontSize: '12px',
                  borderRadius: '4px',
                  border: 'none',
                  cursor: 'pointer',
                  fontWeight: curateMode === 'NEW' ? 600 : 400,
                  backgroundColor:
                    curateMode === 'NEW' ? 'var(--color-bg-primary, #ffffff)' : 'transparent',
                  color:
                    curateMode === 'NEW'
                      ? 'var(--color-primary, #3b82f6)'
                      : 'var(--color-text-secondary, #64748b)',
                  boxShadow: curateMode === 'NEW' ? '0 1px 3px rgba(0,0,0,0.1)' : 'none',
                }}
              >
                + 新建黄金契约套件
              </button>
              <button
                type="button"
                onClick={() => setCurateMode('EXISTING')}
                disabled={existingSuites.length === 0}
                style={{
                  padding: '6px 14px',
                  fontSize: '12px',
                  borderRadius: '4px',
                  border: 'none',
                  cursor: existingSuites.length === 0 ? 'not-allowed' : 'pointer',
                  fontWeight: curateMode === 'EXISTING' ? 600 : 400,
                  backgroundColor:
                    curateMode === 'EXISTING' ? 'var(--color-bg-primary, #ffffff)' : 'transparent',
                  color:
                    curateMode === 'EXISTING'
                      ? 'var(--color-primary, #3b82f6)'
                      : 'var(--color-text-secondary, #64748b)',
                  boxShadow: curateMode === 'EXISTING' ? '0 1px 3px rgba(0,0,0,0.1)' : 'none',
                  opacity: existingSuites.length === 0 ? 0.6 : 1,
                }}
              >
                加入既有套件 ({existingSuites.length})
              </button>
            </div>
          </div>

          {/* 新建套件表单 */}
          {curateMode === 'NEW' && (
            <div
              style={{
                display: 'flex',
                flexDirection: 'column',
                gap: '12px',
                marginBottom: '16px',
                padding: '14px',
                border: '1px solid var(--color-border, #e2e8f0)',
                borderRadius: '6px',
              }}
            >
              <div>
                <label style={{ display: 'block', fontSize: '12px', marginBottom: '4px' }}>
                  套件唯一编码 <span style={{ color: 'var(--color-danger, #ef4444)' }}>*</span>
                </label>
                <input
                  type="text"
                  value={newCode}
                  onChange={(e) => setNewCode(e.target.value)}
                  placeholder="如 GS_ORDER_CORE"
                  style={{
                    width: '100%',
                    padding: '6px 10px',
                    fontSize: '13px',
                    borderRadius: '4px',
                    border: '1px solid var(--color-border, #cbd5e1)',
                  }}
                  data-testid="input-suite-code"
                />
              </div>

              <div>
                <label style={{ display: 'block', fontSize: '12px', marginBottom: '4px' }}>
                  套件名称 <span style={{ color: 'var(--color-danger, #ef4444)' }}>*</span>
                </label>
                <input
                  type="text"
                  value={newName}
                  onChange={(e) => setNewName(e.target.value)}
                  placeholder="如 订单核心业务黄金契约套件"
                  style={{
                    width: '100%',
                    padding: '6px 10px',
                    fontSize: '13px',
                    borderRadius: '4px',
                    border: '1px solid var(--color-border, #cbd5e1)',
                  }}
                  data-testid="input-suite-name"
                />
              </div>

              <div style={{ display: 'flex', gap: '12px' }}>
                <div style={{ flex: 1 }}>
                  <label style={{ display: 'block', fontSize: '12px', marginBottom: '4px' }}>
                    基线版本号
                  </label>
                  <input
                    type="text"
                    value={newVersion}
                    onChange={(e) => setNewVersion(e.target.value)}
                    style={{
                      width: '100%',
                      padding: '6px 10px',
                      fontSize: '13px',
                      borderRadius: '4px',
                      border: '1px solid var(--color-border, #cbd5e1)',
                    }}
                  />
                </div>
                <div style={{ flex: 2 }}>
                  <label style={{ display: 'block', fontSize: '12px', marginBottom: '4px' }}>
                    套件级忽略字段路径 (逗号分隔)
                  </label>
                  <input
                    type="text"
                    value={newExtraIgnorePaths}
                    onChange={(e) => setNewExtraIgnorePaths(e.target.value)}
                    placeholder="如 rsp.timestamp, rsp.trace_id"
                    style={{
                      width: '100%',
                      padding: '6px 10px',
                      fontSize: '13px',
                      borderRadius: '4px',
                      border: '1px solid var(--color-border, #cbd5e1)',
                    }}
                  />
                </div>
              </div>

              <div>
                <label style={{ display: 'block', fontSize: '12px', marginBottom: '4px' }}>
                  套件描述说明
                </label>
                <textarea
                  rows={2}
                  value={newDescription}
                  onChange={(e) => setNewDescription(e.target.value)}
                  placeholder="说明该黄金套件守护的核心业务场景与契约假设..."
                  style={{
                    width: '100%',
                    padding: '6px 10px',
                    fontSize: '13px',
                    borderRadius: '4px',
                    border: '1px solid var(--color-border, #cbd5e1)',
                  }}
                />
              </div>
            </div>
          )}

          {/* 既有套件选择 */}
          {curateMode === 'EXISTING' && (
            <div style={{ marginBottom: '16px' }}>
              <select
                value={selectedSuiteId}
                onChange={(e) => setSelectedSuiteId(Number(e.target.value))}
                style={{
                  width: '100%',
                  padding: '8px 10px',
                  fontSize: '13px',
                  borderRadius: '4px',
                  border: '1px solid var(--color-border, #cbd5e1)',
                }}
                data-testid="select-existing-suite"
              >
                {existingSuites.map((s) => (
                  <option key={s.id} value={s.id}>
                    [{s.code}] {s.name} ({s.version}, {s.item_count} 用例)
                  </option>
                ))}
              </select>
            </div>
          )}

          {/* 连带事务链勾选项 */}
          <div
            style={{
              padding: '12px',
              border: '1px dashed var(--color-border, #cbd5e1)',
              borderRadius: '6px',
              marginBottom: '16px',
              backgroundColor: 'var(--color-bg-primary, #ffffff)',
            }}
          >
            <label
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: '8px',
                fontSize: '13px',
                cursor: 'pointer',
              }}
            >
              <input
                type="checkbox"
                checked={includeChain}
                onChange={(e) => setIncludeChain(e.target.checked)}
              />
              <span>连带前置依赖事务链一并固化 (打包同 TraceID 下游调用)</span>
            </label>
            <div
              style={{
                fontSize: '11px',
                color: 'var(--color-text-secondary, #64748b)',
                marginTop: '4px',
                paddingLeft: '22px',
              }}
            >
              当前共将固化 <strong>{targetRecordIds.length}</strong> 笔事务，并打包对应的 ScopedMocks 闭包。
            </div>
          </div>

          {/* 用例动态掩码规则清单 */}
          <div style={{ marginBottom: '16px' }}>
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                marginBottom: '8px',
              }}
            >
              <label style={{ fontSize: '13px', fontWeight: 600 }}>
                用例级动态掩码与忽略规则 (MaskRules)
              </label>
              <span
                style={{ fontSize: '11px', color: 'var(--color-text-secondary, #64748b)' }}
              >
                忽略动态流水号/Token，避免重放误报
              </span>
            </div>

            <div style={{ display: 'flex', gap: '8px', marginBottom: '8px' }}>
              <input
                type="text"
                value={newMaskPath}
                onChange={(e) => setNewMaskPath(e.target.value)}
                placeholder="字段路径，如 rsp.token 或 order_id"
                style={{
                  flex: 1,
                  padding: '5px 8px',
                  fontSize: '12px',
                  borderRadius: '4px',
                  border: '1px solid var(--color-border, #cbd5e1)',
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    handleAddMaskRule();
                  }
                }}
              />
              <button
                type="button"
                onClick={handleAddMaskRule}
                className="code-btn code-btn-secondary"
                style={{
                  padding: '5px 10px',
                  fontSize: '12px',
                  borderRadius: '4px',
                  display: 'flex',
                  alignItems: 'center',
                  gap: '4px',
                  cursor: 'pointer',
                }}
              >
                <Plus size={14} /> 添加
              </button>
            </div>

            <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px' }}>
              {maskRules.map((r, idx) => (
                <span
                  key={r.path}
                  style={{
                    display: 'inline-flex',
                    alignItems: 'center',
                    gap: '6px',
                    padding: '3px 8px',
                    borderRadius: '4px',
                    backgroundColor: 'var(--color-bg-secondary, #f1f5f9)',
                    border: '1px solid var(--color-border, #e2e8f0)',
                    fontSize: '12px',
                  }}
                >
                  <code>{r.path}</code> ({r.strategy})
                  <button
                    type="button"
                    onClick={() => handleRemoveMaskRule(idx)}
                    style={{
                      background: 'none',
                      border: 'none',
                      cursor: 'pointer',
                      padding: 0,
                      color: 'var(--color-danger, #ef4444)',
                    }}
                    title="移除"
                  >
                    <Trash2 size={12} />
                  </button>
                </span>
              ))}
            </div>
          </div>
        </div>

        {/* 抽屉底部操作栏 */}
        <div
          style={{
            padding: '14px 20px',
            borderTop: '1px solid var(--color-border, #e2e8f0)',
            display: 'flex',
            justifyContent: 'flex-end',
            gap: '12px',
            backgroundColor: 'var(--color-bg-secondary, #f8fafc)',
          }}
        >
          <button
            type="button"
            onClick={onClose}
            className="code-btn code-btn-secondary"
            style={{
              padding: '6px 16px',
              fontSize: '13px',
              borderRadius: '4px',
              cursor: 'pointer',
            }}
          >
            取消
          </button>
          <button
            type="button"
            onClick={handleSubmit}
            disabled={submitting}
            className="code-btn code-btn-primary"
            style={{
              padding: '6px 18px',
              fontSize: '13px',
              borderRadius: '4px',
              cursor: submitting ? 'not-allowed' : 'pointer',
              backgroundColor: 'var(--color-primary, #3b82f6)',
              color: '#ffffff',
              border: 'none',
              fontWeight: 600,
              display: 'flex',
              alignItems: 'center',
              gap: '6px',
            }}
            data-testid="btn-confirm-curate"
          >
            <Sparkles size={15} />
            {submitting ? '正在固化快照...' : '确认沉淀为黄金契约'}
          </button>
        </div>
      </aside>
    </div>
  );
};
