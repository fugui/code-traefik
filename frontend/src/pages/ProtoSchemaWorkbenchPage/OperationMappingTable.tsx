import React from 'react';
import type { ProtoOperation } from '../../types';

export interface OperationMappingTableProps {
  operations: ProtoOperation[];
  onSelectOperation: (op: ProtoOperation) => void;
}

export const OperationMappingTable: React.FC<OperationMappingTableProps> = ({
  operations,
  onSelectOperation,
}) => {
  return (
    <div className="code-table-wrapper">
      <table className="code-table">
        <thead>
          <tr>
            <th>服务契约 (service_name)</th>
            <th>调用方法 (operation)</th>
            <th>请求消息体 (req_message)</th>
            <th>应答消息体 (rsp_message)</th>
            <th>流式模式</th>
            <th>生效态</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          {operations.map((op) => (
            <tr key={op.id}>
              <td className="code-font-mono code-color-primary">{op.service_name}</td>
              <td className="code-font-mono">{op.operation}</td>
              <td className="code-font-mono code-text-xs code-color-muted">{op.req_message_full_name || '-'}</td>
              <td className="code-font-mono code-text-xs code-color-muted">{op.rsp_message_full_name || '-'}</td>
              <td>
                {op.client_streaming || op.server_streaming ? (
                  <span className="code-badge code-badge--info">
                    {op.client_streaming ? 'ClientStream ' : ''}
                    {op.server_streaming ? 'ServerStream' : ''}
                  </span>
                ) : (
                  <span className="code-badge code-badge--secondary">Unary</span>
                )}
              </td>
              <td>
                <span className={`code-badge ${op.is_active ? 'code-badge--success' : 'code-badge--secondary'}`}>
                  {op.is_active ? '生效中' : '未激活'}
                </span>
              </td>
              <td>
                <button
                  type="button"
                  className="code-btn code-btn--secondary code-btn--xs"
                  onClick={() => onSelectOperation(op)}
                >
                  查看契约
                </button>
              </td>
            </tr>
          ))}
          {operations.length === 0 && (
            <tr>
              <td colSpan={7} className="code-table-empty">
                当前描述集下未检索到契约映射
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
};
