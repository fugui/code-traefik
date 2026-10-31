import React from 'react';
import { Drawer } from '@code/common';
import type { GraphNode } from '../../../types/topology_graph';

export interface NodeInspectDrawerProps {
  open: boolean;
  onClose: () => void;
  node: GraphNode | null;
}

export const NodeInspectDrawer: React.FC<NodeInspectDrawerProps> = ({ open, onClose, node }) => {
  if (!node) return null;

  return (
    <Drawer open={open} onClose={onClose} title={`架构元素画像: ${node.name}`} width={460}>
      <div className="code-node-inspect">
        <div className="code-node-inspect__section">
          <h4 className="code-node-inspect__heading">基础标识</h4>
          <div className="code-node-inspect__row">
            <span className="code-node-inspect__label">节点 ID:</span>
            <code className="code-node-inspect__value">{node.id}</code>
          </div>
          <div className="code-node-inspect__row">
            <span className="code-node-inspect__label">节点类型:</span>
            <span className="code-node-inspect__value">{node.type}</span>
          </div>
          {node.code && (
            <div className="code-node-inspect__row">
              <span className="code-node-inspect__label">业务编码:</span>
              <code className="code-node-inspect__value">{node.code}</code>
            </div>
          )}
          <div className="code-node-inspect__row">
            <span className="code-node-inspect__label">层级深度 (Depth):</span>
            <span className="code-node-inspect__value">L{node.depth}</span>
          </div>
          {node.parent_id && (
            <div className="code-node-inspect__row">
              <span className="code-node-inspect__label">直接父节点:</span>
              <code className="code-node-inspect__value">{node.parent_id}</code>
            </div>
          )}
          {node.ancestors.length > 0 && (
            <div className="code-node-inspect__row">
              <span className="code-node-inspect__label">祖先链路:</span>
              <span className="code-node-inspect__value">
                {node.ancestors.join(' → ')}
              </span>
            </div>
          )}
        </div>

        {node.meta && Object.keys(node.meta).length > 0 && (
          <div className="code-node-inspect__section">
            <h4 className="code-node-inspect__heading">运行与视图元数据</h4>
            {node.meta.endpoint && (
              <div className="code-node-inspect__row">
                <span className="code-node-inspect__label">监听端点:</span>
                <code className="code-node-inspect__value">{String(node.meta.endpoint)}</code>
              </div>
            )}
            {node.meta.cmd_pattern && (
              <div className="code-node-inspect__row">
                <span className="code-node-inspect__label">命令行模式:</span>
                <code className="code-node-inspect__value">{String(node.meta.cmd_pattern)}</code>
              </div>
            )}
            {node.meta.provide_count != null && (
              <div className="code-node-inspect__row">
                <span className="code-node-inspect__label">对外供给服务:</span>
                <span className="code-node-inspect__value">{String(node.meta.provide_count)} 个</span>
              </div>
            )}
            {node.meta.description && (
              <div className="code-node-inspect__row">
                <span className="code-node-inspect__label">描述说明:</span>
                <span className="code-node-inspect__value">{String(node.meta.description)}</span>
              </div>
            )}
          </div>
        )}
      </div>
    </Drawer>
  );
};

