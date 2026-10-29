import React from 'react';
import type { FieldDiff } from '../../../types';

export interface ProtoFieldDiffTableProps {
  fieldDiffs?: FieldDiff[];
}

export const ProtoFieldDiffTable: React.FC<ProtoFieldDiffTableProps> = ({ fieldDiffs }) => {
  const diffs = fieldDiffs ?? [];

  return (
    <div className="code-anatomy-card">
      <div className="code-anatomy-card__header">
        <div className="code-anatomy-card__title">第二层：Protobuf 语义字段级比对 (Layer 2)</div>
        <span className={`code-badge ${diffs.length === 0 ? 'code-badge--success' : 'code-badge--danger'}`}>
          {diffs.length === 0 ? '语义树完全一致 (PASS)' : `检出 ${diffs.length} 处字段突变 (FAILED)`}
        </span>
      </div>

      <div className="code-anatomy-card__body">
        {diffs.length > 0 ? (
          <div className="code-table-wrapper">
            <table className="code-table code-table--compact">
              <thead>
                <tr>
                  <th>字段路径 (FieldPath)</th>
                  <th>变动类型 (Kind)</th>
                  <th>基线期望值 (Expected)</th>
                  <th>实际回收值 (Actual)</th>
                </tr>
              </thead>
              <tbody>
                {diffs.map((d, idx) => (
                  <tr key={`${d.field_path}-${idx}`}>
                    <td className="code-font-mono code-color-primary">{d.field_path}</td>
                    <td>
                      <span className={`code-badge ${d.diff_kind === 'MODIFIED' ? 'code-badge--warning' : d.diff_kind === 'ADDED' ? 'code-badge--info' : 'code-badge--danger'}`}>
                        {d.diff_kind}
                      </span>
                    </td>
                    <td className="code-font-mono code-color-muted">
                      {typeof d.expected === 'object' ? JSON.stringify(d.expected) : String(d.expected ?? 'null')}
                    </td>
                    <td className="code-font-mono code-color-danger">
                      {typeof d.actual === 'object' ? JSON.stringify(d.actual) : String(d.actual ?? 'null')}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="code-info-box code-info-box--success">
            经过忽略路径过滤与 Map 无序键值对语义归一化比对，未检出任何非预期的字段级数值差异。
          </div>
        )}
      </div>
    </div>
  );
};
