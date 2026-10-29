import React from 'react';
import { Drawer } from '@code/common';
import type { ProtoOperation } from '../../types';

export interface OperationDocDrawerProps {
  open: boolean;
  onClose: () => void;
  operation: ProtoOperation | null;
}

export const OperationDocDrawer: React.FC<OperationDocDrawerProps> = ({ open, onClose, operation }) => {
  return (
    <Drawer
      open={open}
      onClose={onClose}
      title={operation ? `${operation.service_name}.${operation.operation}` : '接口契约文档'}
      subtitle={operation ? `所属源文件: ${operation.proto_file}` : undefined}
      width="md"
    >
      {operation ? (
        <div className="code-doc-drawer-body">
          <div className="code-meta-grid">
            <div className="code-meta-item">
              <span className="code-meta-label">Proto Package:</span>
              <span className="code-meta-val code-font-mono">{operation.proto_package || '(无)'}</span>
            </div>
            <div className="code-meta-item">
              <span className="code-meta-label">Proto Service:</span>
              <span className="code-meta-val code-font-mono">{operation.proto_service}</span>
            </div>
            <div className="code-meta-item">
              <span className="code-meta-label">请求 Message 类型:</span>
              <span className="code-meta-val code-font-mono code-color-primary">{operation.req_message_full_name}</span>
            </div>
            <div className="code-meta-item">
              <span className="code-meta-label">应答 Message 类型:</span>
              <span className="code-meta-val code-font-mono code-color-success">{operation.rsp_message_full_name}</span>
            </div>
          </div>

          <div className="code-mt-md">
            <h4 className="code-section-subhead">CROS RPC 映射规则规范</h4>
            <div className="code-info-box">
              <p>
                <strong>服务命名契约：</strong>
                <code>service_name = &quot;{operation.service_name}&quot;</code>
              </p>
              <p>
                <strong>方法名契约：</strong>
                <code>operation = &quot;{operation.operation}&quot;</code>
              </p>
              <p className="code-color-muted code-text-xs">
                报文第二段会话头（SessionHead）中的 64B <code>service_name</code> 与 32B <code>operation</code> 将与此契约自动双向匹配，实现无代码侵入的动态 Protobuf JSON 反序列化与语义比对。
              </p>
            </div>
          </div>
        </div>
      ) : (
        <div className="code-empty-pane">未选择方法</div>
      )}
    </Drawer>
  );
};
