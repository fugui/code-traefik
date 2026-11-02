import React from 'react';
import { Modal } from '@code/common';
import type { ConflictOrBrokenResponse } from '../../types';

export interface SchemaConflictContentProps {
  conflictData: ConflictOrBrokenResponse;
  onClose: () => void;
}

export const SchemaConflictContent: React.FC<SchemaConflictContentProps> = ({
  conflictData,
  onClose,
}) => {
  const hasConflicts = Boolean(conflictData.conflicts && conflictData.conflicts.length > 0);
  const hasBroken = Boolean(conflictData.broken_dependents && conflictData.broken_dependents.length > 0);

  return (
    <div style={{ padding: '8px 0', color: 'var(--color-text-primary)' }}>
      <p style={{ marginBottom: '16px', fontSize: '14px', color: 'var(--color-text-secondary)' }}>
        {conflictData.error || '当前操作未能通过联合编译门禁校验，请根据下方诊断信息调整描述集版本：'}
      </p>

      {hasConflicts && conflictData.conflicts && (
        <div style={{ marginBottom: '16px' }}>
          <div
            style={{
              fontSize: '13px',
              fontWeight: 600,
              marginBottom: '8px',
              color: 'var(--color-danger, #ef4444)',
            }}
          >
            冲突方法列表 ({conflictData.conflicts.length} 项):
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
                  <th style={{ padding: '8px 12px', borderBottom: '1px solid var(--color-border-primary)' }}>
                    服务名 (Service)
                  </th>
                  <th style={{ padding: '8px 12px', borderBottom: '1px solid var(--color-border-primary)' }}>
                    方法名 (RPC)
                  </th>
                  <th style={{ padding: '8px 12px', borderBottom: '1px solid var(--color-border-primary)' }}>
                    冲突描述集
                  </th>
                  <th style={{ padding: '8px 12px', borderBottom: '1px solid var(--color-border-primary)' }}>
                    冲突版本
                  </th>
                </tr>
              </thead>
              <tbody>
                {conflictData.conflicts.map((c, idx) => (
                  <tr
                    key={`${c.service_name}-${c.operation}-${idx}`}
                    style={{
                      borderBottom:
                        idx === conflictData.conflicts!.length - 1
                          ? 'none'
                          : '1px solid var(--color-border-primary)',
                    }}
                  >
                    <td style={{ padding: '8px 12px', fontFamily: 'monospace' }}>{c.service_name}</td>
                    <td style={{ padding: '8px 12px', fontWeight: 600 }}>{c.operation}</td>
                    <td style={{ padding: '8px 12px' }}>{c.conflict_schema_name}</td>
                    <td style={{ padding: '8px 12px' }}>{`v${c.conflict_version}`}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {hasBroken && conflictData.broken_dependents && (
        <div style={{ marginBottom: '16px' }}>
          <div
            style={{
              fontSize: '13px',
              fontWeight: 600,
              marginBottom: '8px',
              color: 'var(--color-danger, #ef4444)',
            }}
          >
            受影响破坏的下游描述集 ({conflictData.broken_dependents.length} 项):
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
                  <th style={{ padding: '8px 12px', borderBottom: '1px solid var(--color-border-primary)' }}>
                    描述集
                  </th>
                  <th style={{ padding: '8px 12px', borderBottom: '1px solid var(--color-border-primary)' }}>
                    版本
                  </th>
                  <th style={{ padding: '8px 12px', borderBottom: '1px solid var(--color-border-primary)' }}>
                    破坏原因
                  </th>
                </tr>
              </thead>
              <tbody>
                {conflictData.broken_dependents.map((b, idx) => (
                  <tr
                    key={`${b.schema_id}-${idx}`}
                    style={{
                      borderBottom:
                        idx === conflictData.broken_dependents!.length - 1
                          ? 'none'
                          : '1px solid var(--color-border-primary)',
                    }}
                  >
                    <td style={{ padding: '8px 12px', fontWeight: 600 }}>{b.schema_name}</td>
                    <td style={{ padding: '8px 12px' }}>{`v${b.version}`}</td>
                    <td style={{ padding: '8px 12px', color: 'var(--color-text-secondary)' }}>{b.reason}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: '20px' }}>
        <button type="button" className="btn btn-primary" onClick={onClose}>
          我知道了
        </button>
      </div>
    </div>
  );
};

export interface SchemaConflictModalProps {
  open: boolean;
  onClose: () => void;
  conflictData: ConflictOrBrokenResponse | null;
}

export const SchemaConflictModal: React.FC<SchemaConflictModalProps> = ({
  open,
  onClose,
  conflictData,
}) => {
  if (!open || !conflictData) return null;

  const hasConflicts = Boolean(conflictData.conflicts && conflictData.conflicts.length > 0);
  const hasBroken = Boolean(conflictData.broken_dependents && conflictData.broken_dependents.length > 0);
  const title = hasConflicts ? '⚠️ 描述集方法路由冲突' : hasBroken ? '⚠️ 依赖闭包破坏拦截' : '⚠️ 操作冲突提示';

  return (
    <Modal open={open} onClose={onClose} title={title}>
      <SchemaConflictContent conflictData={conflictData} onClose={onClose} />
    </Modal>
  );
};
