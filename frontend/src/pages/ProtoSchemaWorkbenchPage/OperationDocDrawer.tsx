import React, { useEffect, useState, useCallback } from 'react';
import { Drawer } from '@code/common';
import { useToast } from '../../utils/toast';
import * as schemaApi from '../../api/schema';
import type { ProtoOperation } from '../../types';
import type { OperationSchemaTreeDTO } from '../../types/schema_tree';
import { MessageSchemaInspectorTree } from './MessageSchemaInspectorTree';

export interface OperationDocDrawerProps {
  open: boolean;
  onClose: () => void;
  operation: ProtoOperation | null;
  isAdmin?: boolean;
  onOperationUpdated?: (updated: ProtoOperation) => void;
}

export const OperationDocDrawer: React.FC<OperationDocDrawerProps> = ({
  open,
  onClose,
  operation,
  isAdmin = false,
  onOperationUpdated,
}) => {
  const toast = useToast();
  const [activeTab, setActiveTab] = useState<'REQ' | 'RSP' | 'META'>('REQ');
  const [loading, setLoading] = useState(false);
  const [treeData, setTreeData] = useState<OperationSchemaTreeDTO | null>(null);
  const [ignorePaths, setIgnorePaths] = useState<string[]>([]);

  const loadTree = useCallback(async (opId: string) => {
    setLoading(true);
    try {
      const res = await schemaApi.getOperationSchemaTree(opId);
      setTreeData(res);
      setIgnorePaths(res.diffIgnorePaths || res.diff_ignore_paths || []);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '加载消息体字典树失败');
    } finally {
      setLoading(false);
    }
  }, [toast]);

  useEffect(() => {
    if (open && operation) {
      void loadTree(operation.id);
      setIgnorePaths(operation.diff_ignore_paths || []);
    } else {
      setTreeData(null);
    }
  }, [open, operation, loadTree]);

  const handleToggleIgnore = async (fieldPath: string, willIgnore: boolean) => {
    if (!operation) return;

    let nextPaths: string[];
    if (willIgnore) {
      if (ignorePaths.includes(fieldPath)) return;
      nextPaths = [...ignorePaths, fieldPath];
    } else {
      nextPaths = ignorePaths.filter((p) => p !== fieldPath);
    }

    setIgnorePaths(nextPaths);

    try {
      const updated = await schemaApi.updateOperationIgnorePaths(operation.id, nextPaths);
      toast.success(willIgnore ? `已添加忽略字段: ${fieldPath}` : `已移除忽略字段: ${fieldPath}`);
      if (onOperationUpdated) {
        onOperationUpdated(updated);
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '更新忽略规则失败');
      setIgnorePaths(ignorePaths); // 回滚
    }
  };

  const inheritedPaths = treeData?.inheritedIgnorePaths || treeData?.inherited_ignore_paths || [];
  const reqFields = treeData?.reqMessage?.fields || treeData?.req_message?.fields || [];
  const rspFields = treeData?.rspMessage?.fields || treeData?.rsp_message?.fields || [];

  return (
    <Drawer
      open={open}
      onClose={onClose}
      title={operation ? `${operation.service_name}.${operation.operation}` : '接口契约与字典树透视'}
      subtitle={operation ? `所属源文件: ${operation.proto_file}` : undefined}
      width="lg"
    >
      {operation ? (
        <div className="code-doc-drawer-body">
          <div className="code-meta-grid">
            <div className="code-meta-item">
              <span className="code-meta-label">Proto Package:</span>
              <span className="code-meta-val code-font-mono">{operation.proto_package || '(默认包)'}</span>
            </div>
            <div className="code-meta-item">
              <span className="code-meta-label">Proto Service:</span>
              <span className="code-meta-val code-font-mono">{operation.proto_service}</span>
            </div>
            <div className="code-meta-item">
              <span className="code-meta-label">请求 Message:</span>
              <span className="code-meta-val code-font-mono code-color-primary">{operation.req_message_full_name || '-'}</span>
            </div>
            <div className="code-meta-item">
              <span className="code-meta-label">应答 Message:</span>
              <span className="code-meta-val code-font-mono code-color-success">{operation.rsp_message_full_name || '-'}</span>
            </div>
          </div>

          <div className="code-type-selector-tabs code-mt-md">
            <button
              type="button"
              className={`code-type-selector-tab ${activeTab === 'REQ' ? 'code-type-selector-tab--active' : ''}`}
              onClick={() => setActiveTab('REQ')}
            >
              请求参数字典树 (Req Message)
            </button>
            <button
              type="button"
              className={`code-type-selector-tab ${activeTab === 'RSP' ? 'code-type-selector-tab--active' : ''}`}
              onClick={() => setActiveTab('RSP')}
            >
              应答参数字典树 (Rsp Message)
            </button>
            <button
              type="button"
              className={`code-type-selector-tab ${activeTab === 'META' ? 'code-type-selector-tab--active' : ''}`}
              onClick={() => setActiveTab('META')}
            >
              CROS 协议映射与忽略清单 ({ignorePaths.length + inheritedPaths.length})
            </button>
          </div>

          {loading ? (
            <div className="code-tpl-loading-screen code-p-xl">正在反射解析 Message 参数字典树...</div>
          ) : (
            <div className="code-doc-tab-content code-mt-sm">
              {activeTab === 'REQ' && (
                <div className="code-tree-pane" data-testid="req-tree-pane">
                  <div className="code-tree-pane-subhead">
                    <span className="code-font-bold">请求结构体全名:</span>{' '}
                    <code className="code-font-mono">{operation.req_message_full_name}</code>
                  </div>
                  <MessageSchemaInspectorTree
                    fields={reqFields}
                    activeIgnorePaths={ignorePaths}
                    inheritedIgnorePaths={inheritedPaths}
                    onToggleIgnore={isAdmin ? handleToggleIgnore : undefined}
                    isAdmin={isAdmin}
                  />
                </div>
              )}

              {activeTab === 'RSP' && (
                <div className="code-tree-pane" data-testid="rsp-tree-pane">
                  <div className="code-tree-pane-subhead">
                    <span className="code-font-bold">应答结构体全名:</span>{' '}
                    <code className="code-font-mono">{operation.rsp_message_full_name}</code>
                  </div>
                  <MessageSchemaInspectorTree
                    fields={rspFields}
                    activeIgnorePaths={ignorePaths}
                    inheritedIgnorePaths={inheritedPaths}
                    onToggleIgnore={isAdmin ? handleToggleIgnore : undefined}
                    isAdmin={isAdmin}
                  />
                </div>
              )}

              {activeTab === 'META' && (
                <div className="code-meta-pane">
                  <div className="code-info-box">
                    <h4 className="code-section-subhead">CROS RPC 映射规则规范</h4>
                    <p>
                      <strong>服务命名契约：</strong>
                      <code>service_name = &quot;{operation.service_name}&quot;</code>
                    </p>
                    <p>
                      <strong>方法名契约：</strong>
                      <code>operation = &quot;{operation.operation}&quot;</code>
                    </p>
                    <p className="code-color-muted code-text-xs">
                      第二段会话头（SessionHead）中的 64B <code>service_name</code> 与 32B <code>operation</code> 将自动匹配本条目进行动态反序列化与语义树 Diff 比对。
                    </p>
                  </div>

                  <div className="code-info-box code-mt-md">
                    <h4 className="code-section-subhead">三级生效对比忽略清单 (Diff Ignore Rules)</h4>
                    <div className="code-ignore-section">
                      <span className="code-font-bold">方法级专属忽略规则 ({ignorePaths.length} 项):</span>
                      {ignorePaths.length > 0 ? (
                        <div className="code-chip-group code-mt-xs">
                          {ignorePaths.map((p) => (
                            <span key={p} className="code-badge code-badge--warning code-chip-item">
                              <code>{p}</code>
                              {isAdmin && (
                                <button
                                  type="button"
                                  className="code-chip-remove"
                                  onClick={() => handleToggleIgnore(p, false)}
                                  title="移除此规则"
                                >
                                  ×
                                </button>
                              )}
                            </span>
                          ))}
                        </div>
                      ) : (
                        <div className="code-text-xs code-color-muted code-mt-xs">暂未配置方法级忽略规则（可在左侧 Req/Rsp 树中勾选）</div>
                      )}
                    </div>

                    <div className="code-ignore-section code-mt-md">
                      <span className="code-font-bold">架构服务级继承规则 ({inheritedPaths.length} 项):</span>
                      {inheritedPaths.length > 0 ? (
                        <div className="code-chip-group code-mt-xs">
                          {inheritedPaths.map((p) => (
                            <span key={p} className="code-badge code-badge--secondary code-chip-item">
                              <code>{p}</code>
                            </span>
                          ))}
                        </div>
                      ) : (
                        <div className="code-text-xs code-color-muted code-mt-xs">无服务级默认规则</div>
                      )}
                    </div>
                  </div>
                </div>
              )}
            </div>
          )}
        </div>
      ) : (
        <div className="code-empty-pane">未选择方法</div>
      )}
    </Drawer>
  );
};
