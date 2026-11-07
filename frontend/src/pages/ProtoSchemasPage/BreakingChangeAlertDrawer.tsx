import React, { useState } from 'react';
import { Drawer } from '@code/common';
import type { BreakingRuleViolation, AffectedDependent } from '../../types';

export interface BreakingChangeAlertDrawerProps {
  isOpen: boolean;
  onClose: () => void;
  breakingRules: BreakingRuleViolation[];
  affectedDependents: AffectedDependent[];
  isAdmin?: boolean;
  onConfirmOverride?: (reason: string) => Promise<void> | void;
  isSubmitting?: boolean;
}

const getRuleBadgeStyle = (rule: string): React.CSSProperties => {
  let bg = 'rgba(239, 68, 68, 0.12)';
  let color = 'var(--color-danger, #ef4444)';
  let border = 'rgba(239, 68, 68, 0.3)';

  if (rule === 'METHOD_SIGNATURE_CHANGED' || rule === 'TAG_MUTATION') {
    bg = 'rgba(245, 158, 11, 0.12)';
    color = 'var(--color-warning, #f59e0b)';
    border = 'rgba(245, 158, 11, 0.3)';
  } else if (rule === 'TYPE_INCOMPATIBLE' || rule === 'ENUM_BREAKING_CHANGE') {
    bg = 'rgba(168, 85, 247, 0.12)';
    color = 'var(--color-accent, #a855f7)';
    border = 'rgba(168, 85, 247, 0.3)';
  }

  return {
    display: 'inline-block',
    padding: '2px 8px',
    borderRadius: '4px',
    fontSize: '11px',
    fontWeight: 600,
    fontFamily: 'monospace',
    backgroundColor: bg,
    color,
    border: `1px solid ${border}`,
  };
};

export const BreakingChangeAlertDrawer: React.FC<BreakingChangeAlertDrawerProps> = ({
  isOpen,
  onClose,
  breakingRules,
  affectedDependents,
  isAdmin = false,
  onConfirmOverride,
  isSubmitting = false,
}) => {
  const [overrideReason, setOverrideReason] = useState('');
  const [isOverrideOpen, setIsOverrideOpen] = useState(true);

  if (!isOpen) {
    return null;
  }

  const trimmedReason = overrideReason.trim();
  const isReasonValid = trimmedReason.length >= 8;

  const handleConfirm = async () => {
    if (!isReasonValid || isSubmitting || !onConfirmOverride) return;
    await onConfirmOverride(trimmedReason);
  };

  return (
    <Drawer
      open={isOpen}
      onClose={onClose}
      title="🛡️ 破坏性契约变更拦截 (Breaking Changes Detected)"
      width={680}
    >
      <div className="breaking-change-drawer" style={{ display: 'flex', flexDirection: 'column', gap: '20px' }}>
        {/* 顶部醒目警示条 */}
        <div
          style={{
            padding: '12px 16px',
            backgroundColor: 'rgba(239, 68, 68, 0.08)',
            border: '1px solid rgba(239, 68, 68, 0.25)',
            borderRadius: '6px',
            display: 'flex',
            alignItems: 'flex-start',
            gap: '12px',
          }}
        >
          <span style={{ fontSize: '20px', lineHeight: 1 }}>🚫</span>
          <div>
            <div style={{ fontWeight: 600, color: 'var(--color-danger, #ef4444)', fontSize: '14px', marginBottom: '4px' }}>
              检测到 Protobuf AST 破坏性改动，已硬性阻断激活 (HTTP 409 Conflict)
            </div>
            <div style={{ fontSize: '12px', color: 'var(--color-text-secondary)', lineHeight: '18px' }}>
              候选版本与当前生效契约存在向后不兼容变异。直接上线将导致既有客户端调用因反序列化失败或字段缺失而中断。
            </div>
          </div>
        </div>

        {/* 违规规则表格 */}
        <div className="breaking-change-drawer__section">
          <div
            style={{
              fontSize: '13px',
              fontWeight: 600,
              marginBottom: '10px',
              color: 'var(--color-text-primary)',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
            }}
          >
            <span>检出破坏性规则清单 ({breakingRules.length} 项)</span>
          </div>
          <div
            style={{
              border: '1px solid var(--color-border-primary)',
              borderRadius: '6px',
              overflow: 'hidden',
              maxHeight: '260px',
              overflowY: 'auto',
            }}
          >
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '12px' }}>
              <thead>
                <tr style={{ background: 'var(--color-bg-muted)', textAlign: 'left' }}>
                  <th style={{ padding: '8px 12px', borderBottom: '1px solid var(--color-border-primary)' }}>规则分类</th>
                  <th style={{ padding: '8px 12px', borderBottom: '1px solid var(--color-border-primary)' }}>文件与受损符号</th>
                  <th style={{ padding: '8px 12px', borderBottom: '1px solid var(--color-border-primary)' }}>受影响服务</th>
                  <th style={{ padding: '8px 12px', borderBottom: '1px solid var(--color-border-primary)' }}>违规详情</th>
                </tr>
              </thead>
              <tbody>
                {breakingRules.map((rule, idx) => (
                  <tr
                    key={`${rule.rule}-${rule.symbol}-${idx}`}
                    style={{
                      borderBottom:
                        idx === breakingRules.length - 1 ? 'none' : '1px solid var(--color-border-primary)',
                    }}
                  >
                    <td style={{ padding: '8px 12px', verticalAlign: 'top', whiteSpace: 'nowrap' }}>
                      <span style={getRuleBadgeStyle(rule.rule)}>{rule.rule}</span>
                    </td>
                    <td style={{ padding: '8px 12px', verticalAlign: 'top' }}>
                      <div style={{ fontFamily: 'monospace', fontWeight: 600, color: 'var(--color-text-primary)' }}>
                        {rule.symbol}
                      </div>
                      <div style={{ fontSize: '11px', color: 'var(--color-text-muted)' }}>{rule.proto_file}</div>
                    </td>
                    <td style={{ padding: '8px 12px', verticalAlign: 'top', fontFamily: 'monospace', color: 'var(--color-text-secondary)' }}>
                      {rule.service_name || '-'}
                    </td>
                    <td style={{ padding: '8px 12px', verticalAlign: 'top', color: 'var(--color-text-secondary)' }}>
                      {rule.message}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>

        {/* 波及消费方模块推导 */}
        <div className="breaking-change-drawer__section">
          <div
            style={{
              fontSize: '13px',
              fontWeight: 600,
              marginBottom: '10px',
              color: 'var(--color-text-primary)',
            }}
          >
            波及消费方模块推导 ({affectedDependents.length} 个功能模块)
          </div>
          {affectedDependents.length === 0 ? (
            <div
              style={{
                padding: '16px',
                textAlign: 'center',
                backgroundColor: 'var(--color-bg-secondary)',
                borderRadius: '6px',
                color: 'var(--color-text-muted)',
                fontSize: '12px',
                border: '1px dashed var(--color-border-primary)',
              }}
            >
              暂未检测到已登记 DEPEND 依赖该接口的消费方模块
            </div>
          ) : (
            <div
              style={{
                border: '1px solid var(--color-border-primary)',
                borderRadius: '6px',
                overflow: 'hidden',
                maxHeight: '220px',
                overflowY: 'auto',
              }}
            >
              <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '12px' }}>
                <thead>
                  <tr style={{ background: 'var(--color-bg-muted)', textAlign: 'left' }}>
                    <th style={{ padding: '8px 12px', borderBottom: '1px solid var(--color-border-primary)' }}>消费方模块</th>
                    <th style={{ padding: '8px 12px', borderBottom: '1px solid var(--color-border-primary)' }}>模块编码</th>
                    <th style={{ padding: '8px 12px', borderBottom: '1px solid var(--color-border-primary)' }}>标准进程</th>
                    <th style={{ padding: '8px 12px', borderBottom: '1px solid var(--color-border-primary)' }}>模块负责人</th>
                  </tr>
                </thead>
                <tbody>
                  {affectedDependents.map((dep, idx) => (
                    <tr
                      key={`${dep.module_id}-${idx}`}
                      style={{
                        borderBottom:
                          idx === affectedDependents.length - 1 ? 'none' : '1px solid var(--color-border-primary)',
                      }}
                    >
                      <td style={{ padding: '8px 12px', fontWeight: 600, color: 'var(--color-text-primary)' }}>
                        {dep.module_name}
                      </td>
                      <td style={{ padding: '8px 12px', fontFamily: 'monospace', color: 'var(--color-text-secondary)' }}>
                        {dep.module_code}
                      </td>
                      <td style={{ padding: '8px 12px' }}>
                        <span
                          style={{
                            padding: '2px 6px',
                            borderRadius: '4px',
                            fontSize: '11px',
                            backgroundColor: 'var(--color-bg-muted)',
                            color: 'var(--color-text-primary)',
                            fontFamily: 'monospace',
                            border: '1px solid var(--color-border-secondary)',
                          }}
                        >
                          {dep.arch_process_code || '-'}
                        </span>
                      </td>
                      <td style={{ padding: '8px 12px' }}>
                        <span
                          style={{
                            padding: '2px 8px',
                            borderRadius: '12px',
                            fontSize: '11px',
                            backgroundColor: 'rgba(59, 130, 246, 0.1)',
                            color: 'var(--color-primary, #3b82f6)',
                            border: '1px solid rgba(59, 130, 246, 0.25)',
                          }}
                        >
                          👤 {dep.owner_name || '未指定'}
                        </span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>

        {/* 管理员逃生舱折叠区 */}
        {isAdmin ? (
          <div
            style={{
              border: '1px solid rgba(245, 158, 11, 0.3)',
              borderRadius: '6px',
              backgroundColor: 'rgba(245, 158, 11, 0.04)',
              overflow: 'hidden',
            }}
          >
            <div
              style={{
                padding: '10px 16px',
                background: 'rgba(245, 158, 11, 0.08)',
                cursor: 'pointer',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                fontWeight: 600,
                fontSize: '13px',
                color: 'var(--color-warning, #f59e0b)',
              }}
              onClick={() => setIsOverrideOpen((prev) => !prev)}
            >
              <span>🚨 管理员强推 / 紧急回滚逃生舱 (Admin Override)</span>
              <span>{isOverrideOpen ? '▼' : '▶'}</span>
            </div>

            {isOverrideOpen && (
              <div style={{ padding: '14px 16px', display: 'flex', flexDirection: 'column', gap: '12px' }}>
                <div style={{ fontSize: '12px', color: 'var(--color-text-secondary)', lineHeight: '18px' }}>
                  ⚠️ 高危操作：破坏性变更可能导致线上服务不可用。仅限紧急版本回滚或经架构评审批准的大版本强推。本次操作将被记录至
                  <strong style={{ color: 'var(--color-danger, #ef4444)' }}> P1 级操作审计日志</strong>。
                </div>

                <div>
                  <textarea
                    style={{
                      width: '100%',
                      boxSizing: 'border-box',
                      minHeight: '80px',
                      padding: '8px 12px',
                      borderRadius: '6px',
                      border: '1px solid var(--color-border-primary)',
                      backgroundColor: 'var(--color-bg-primary)',
                      color: 'var(--color-text-primary)',
                      fontSize: '12px',
                      fontFamily: 'inherit',
                      resize: 'vertical',
                      outline: 'none',
                    }}
                    placeholder="请输入强推或紧急回滚原因，至少输入 8 个字符（例如：生产故障紧急回滚至上个稳定基线）"
                    value={overrideReason}
                    onChange={(e) => setOverrideReason(e.target.value)}
                    disabled={isSubmitting}
                  />
                  <div
                    style={{
                      display: 'flex',
                      justifyContent: 'space-between',
                      fontSize: '11px',
                      marginTop: '4px',
                      color: isReasonValid ? 'var(--color-text-secondary)' : 'var(--color-danger, #ef4444)',
                    }}
                  >
                    <span>
                      {isReasonValid
                        ? '✅ 原因长度合规'
                        : `❌ 至少输入 8 个字符 (当前已输入 ${trimmedReason.length} 个字符)`}
                    </span>
                    <span>{trimmedReason.length} / 500</span>
                  </div>
                </div>

                <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '10px' }}>
                  <button
                    type="button"
                    style={{
                      padding: '6px 16px',
                      borderRadius: '4px',
                      border: '1px solid var(--color-danger, #ef4444)',
                      backgroundColor: isReasonValid && !isSubmitting ? 'var(--color-danger, #ef4444)' : 'var(--color-bg-muted)',
                      color: isReasonValid && !isSubmitting ? '#ffffff' : 'var(--color-text-muted)',
                      cursor: isReasonValid && !isSubmitting ? 'pointer' : 'not-allowed',
                      fontSize: '12px',
                      fontWeight: 600,
                    }}
                    disabled={!isReasonValid || isSubmitting}
                    onClick={() => void handleConfirm()}
                  >
                    {isSubmitting ? '正在执行强制激活...' : '🚨 强制激活 / 回滚'}
                  </button>
                </div>
              </div>
            )}
          </div>
        ) : (
          <div
            style={{
              padding: '12px 16px',
              backgroundColor: 'var(--color-bg-muted)',
              borderRadius: '6px',
              border: '1px solid var(--color-border-primary)',
              fontSize: '12px',
              color: 'var(--color-text-muted)',
            }}
          >
            当前为只读视窗，如需紧急回滚或大版本强推，请联系具有 traefik_admin 权限的管理员操作。
          </div>
        )}

        {/* 底部操作栏 */}
        <div style={{ display: 'flex', justifyContent: 'flex-end', paddingTop: '10px', borderTop: '1px solid var(--color-border-primary)' }}>
          <button
            type="button"
            style={{
              padding: '6px 16px',
              borderRadius: '4px',
              border: '1px solid var(--color-border-primary)',
              backgroundColor: 'var(--color-bg-secondary)',
              color: 'var(--color-text-primary)',
              cursor: 'pointer',
              fontSize: '12px',
            }}
            onClick={onClose}
          >
            我知道了 / 关闭
          </button>
        </div>
      </div>
    </Drawer>
  );
};
