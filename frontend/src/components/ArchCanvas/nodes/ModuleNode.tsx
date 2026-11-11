import React from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import type { ModuleNodeType } from '../layout/layoutProcessor';
import { Cpu, AlertTriangle, Pin } from 'lucide-react';

export const ModuleNode: React.FC<NodeProps<ModuleNodeType>> = ({ id, data }) => {
  const { graphNode, isPinned, onTogglePin, lodMode = 'STANDARD' } = data;
  const isContractLib = graphNode.meta?.module_kind === 'CONTRACT_LIB';
  const isDeployed = isContractLib || graphNode.meta?.is_deployed !== false;
  const provideCount = Number(graphNode.meta?.provide_count || 0);
  const contractCount = Number(graphNode.meta?.contract_count || 0);
  const implCount = Number(graphNode.meta?.impl_count || 0);
  const ownerName = graphNode.meta?.owner_name ? String(graphNode.meta.owner_name) : undefined;

  const handlePinClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (onTogglePin) {
      onTogglePin(id);
    }
  };

  return (
    <div
      className={`code-arch-module ${isContractLib ? 'code-arch-module--contract-lib' : ''} ${
        !isDeployed ? 'code-arch-module--undeployed' : ''
      } ${isPinned ? 'code-arch-node--pinned' : ''} ${
        lodMode === 'COMPACT' ? 'code-arch-node--lod-compact' : ''
      }`}
      style={isContractLib ? { borderStyle: 'dashed' } : undefined}
      title={graphNode.name}
    >
      <Handle type="target" position={Position.Left} className="code-arch-handle" />

      <div className="code-arch-module__header">
        <Cpu size={13} className="code-arch-module__icon" />
        <span className="code-arch-module__code">{graphNode.code || graphNode.id}</span>
        {isContractLib && (
          <span className="code-arch-badge code-arch-badge--interface" data-testid="interface-badge">
            «interface»
          </span>
        )}
        {!isDeployed && !isContractLib && lodMode !== 'COMPACT' && (
          <span className="code-arch-module__warn-badge" title="未在公共环境绑定物理进程">
            <AlertTriangle size={11} /> 未部署
          </span>
        )}
        {onTogglePin && (
          <button
            type="button"
            className={`code-arch-container__action-btn ${isPinned ? 'code-arch-container__action-btn--active' : ''}`}
            onClick={handlePinClick}
            title={isPinned ? '取消固定位置' : '固定模块位置'}
            data-testid={`pin-node-btn-${id}`}
          >
            <Pin size={11} />
          </button>
        )}
      </div>

      <div className="code-arch-module__body">
        <span className="code-arch-module__name">{graphNode.name}</span>
      </div>

      {lodMode !== 'COMPACT' && (
        <div className="code-arch-module__footer">
          {lodMode === 'DETAIL' && ownerName && (
            <span className="code-arch-badge code-arch-badge--counter" title={`负责人: ${ownerName}`}>
              {ownerName}
            </span>
          )}
          {isContractLib ? (
            <span
              className="code-arch-badge code-arch-badge--contract-count"
              title={`${contractCount} 契约 / ${implCount} 实现`}
            >
              {contractCount} 契约 / {implCount} 实现
            </span>
          ) : (
            provideCount > 0 && (
              <span className="code-arch-badge code-arch-badge--provide" title={`对外提供 ${provideCount} 个服务契约`}>
                供给: {provideCount}
              </span>
            )
          )}
        </div>
      )}

      <Handle type="source" position={Position.Right} className="code-arch-handle" />
    </div>
  );
};
