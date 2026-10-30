import React, { useState } from 'react';
import { ChevronRight } from 'lucide-react';
import type { Module, ModuleInterface, Subsystem, FunctionalGroupTreeNode } from '../../types';

export interface ModuleContractDetailPaneProps {
  modules: Module[];
  selectedModuleId: string;
  onSelectModule: (id: string) => void;
  selectedModule: Module | null;
  interfaces: ModuleInterface[];
  isAdmin: boolean;
  onCreateModule: () => void;
  onEditModule: (mod: Module) => void;
  onDeleteModule: (id: string) => void;
  onCreateInterface: () => void;
  onDeleteInterface: (ifaceId: string) => void;
  onUpdateIgnorePaths: (ifaceId: string, paths: string[]) => void;
  currentSubsystem?: Subsystem | null;
  currentGroup?: FunctionalGroupTreeNode | null;
}

export const ModuleContractDetailPane: React.FC<ModuleContractDetailPaneProps> = ({
  modules,
  selectedModuleId,
  onSelectModule,
  selectedModule,
  interfaces,
  isAdmin,
  onCreateModule,
  onEditModule,
  onDeleteModule,
  onCreateInterface,
  onDeleteInterface,
  onUpdateIgnorePaths,
  currentSubsystem,
  currentGroup,
}) => {
  const [editingIfaceId, setEditingIfaceId] = useState<string | null>(null);
  const [ignorePathInput, setIgnorePathInput] = useState<string>('');

  const handleStartEditPaths = (iface: ModuleInterface) => {
    setEditingIfaceId(iface.id);
    setIgnorePathInput((iface.diff_ignore_paths ?? []).join(', '));
  };

  const handleSavePaths = (ifaceId: string) => {
    const paths = ignorePathInput
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean);
    onUpdateIgnorePaths(ifaceId, paths);
    setEditingIfaceId(null);
  };

  return (
    <div className="code-module-pane">
      {/* 顶部面包屑路径 */}
      <div className="code-module-breadcrumb">
        <span className="code-module-breadcrumb__item">架构根节点</span>
        {currentSubsystem && (
          <>
            <ChevronRight size={13} className="code-module-breadcrumb__sep" />
            <span className="code-module-breadcrumb__item">{currentSubsystem.name}</span>
          </>
        )}
        {currentGroup && (
          <>
            <ChevronRight size={13} className="code-module-breadcrumb__sep" />
            <span className="code-module-breadcrumb__item code-module-breadcrumb__item--current">
              {currentGroup.name}
            </span>
          </>
        )}
      </div>

      {/* 模块选择与概览卡片 */}
      <div className="code-module-pane__header">
        <div className="code-module-pane__title-row">
          <span className="code-module-pane__title">功能模块契约与进程绑定</span>
          {isAdmin && (
            <button
              type="button"
              className="code-btn code-btn--primary code-btn--xs"
              onClick={onCreateModule}
            >
              + 新建模块
            </button>
          )}
        </div>

        <div className="code-module-selector-tabs">
          {(modules ?? []).map((m) => (
            <button
              key={m.id}
              type="button"
              className={`code-module-tab-chip ${selectedModuleId === m.id ? 'code-module-tab-chip--active' : ''}`}
              onClick={() => onSelectModule(m.id)}
            >
              <span className="code-module-chip__code">{m.code}</span>
              <span className="code-module-chip__name">{m.name}</span>
            </button>
          ))}
          {(modules ?? []).length === 0 && (
            <div className="code-empty-pane code-p-sm">当前功能组下暂无功能模块</div>
          )}
        </div>
      </div>

      {/* 模块基本信息 */}
      {selectedModule ? (
        <div className="code-module-pane__body">
          <div className="code-module-meta-card">
            <div className="code-module-meta-header">
              <div>
                <h3 className="code-module-title">{selectedModule.name} ({selectedModule.code})</h3>
                <p className="code-module-desc">{selectedModule.description || '暂无描述'}</p>
              </div>
              {isAdmin && (
                <div className="code-module-card-actions">
                  <button
                    type="button"
                    className="code-btn code-btn--secondary code-btn--xs"
                    onClick={() => onEditModule(selectedModule)}
                  >
                    编辑模块
                  </button>
                  <button
                    type="button"
                    className="code-btn code-btn--danger code-btn--xs"
                    onClick={() => onDeleteModule(selectedModule.id)}
                  >
                    删除模块
                  </button>
                </div>
              )}
            </div>

            <div className="code-meta-grid">
              <div className="code-meta-item">
                <span className="code-meta-label">公共基线进程:</span>
                <span className="code-meta-val code-font-mono">
                  {selectedModule.process
                    ? `${selectedModule.process.name} (Port: ${selectedModule.process.listen_port})`
                    : '未绑定公共基线进程'}
                </span>
              </div>
              <div className="code-meta-item">
                <span className="code-meta-label">所属子系统:</span>
                <span className="code-meta-val">{selectedModule.subsystem?.name || '-'}</span>
              </div>
              <div className="code-meta-item">
                <span className="code-meta-label">声明接口契约:</span>
                <span className="code-meta-val">{interfaces.length} 个</span>
              </div>
            </div>
          </div>

          {/* 接口契约明细表 */}
          <div className="code-interface-section">
            <div className="code-interface-header">
              <span className="code-interface-title">PROVIDE (暴露) / DEPEND (依赖) 接口列表</span>
              {isAdmin && (
                <button
                  type="button"
                  className="code-btn code-btn--secondary code-btn--xs"
                  onClick={onCreateInterface}
                >
                  + 添加接口契约
                </button>
              )}
            </div>

            <div className="code-table-wrapper">
              <table className="code-table">
                <thead>
                  <tr>
                    <th>关系类型</th>
                    <th>服务名称 (service_name)</th>
                    <th>方法名 (operation)</th>
                    <th>默认 Diff 忽略字段路径 (diff_ignore_paths)</th>
                    <th>说明</th>
                    {isAdmin && <th>操作</th>}
                  </tr>
                </thead>
                <tbody>
                  {(interfaces ?? []).map((iface) => (
                    <tr key={iface.id}>
                      <td>
                        <span className={`code-badge ${iface.relation_type === 'PROVIDE' ? 'code-badge--success' : 'code-badge--info'}`}>
                          {iface.relation_type}
                        </span>
                      </td>
                      <td className="code-font-mono code-color-primary">{iface.service_name}</td>
                      <td className="code-font-mono">{iface.operation}</td>
                      <td>
                        {editingIfaceId === iface.id ? (
                          <div className="code-ignore-path-edit-inline">
                            <input
                              type="text"
                              className="code-input code-input--sm"
                              value={ignorePathInput}
                              onChange={(e) => setIgnorePathInput(e.target.value)}
                              placeholder="逗号分隔，如 header.timestamp, items[*].id"
                            />
                            <button
                              type="button"
                              className="code-btn code-btn--primary code-btn--xs"
                              onClick={() => handleSavePaths(iface.id)}
                            >
                              保存
                            </button>
                            <button
                              type="button"
                              className="code-btn code-btn--secondary code-btn--xs"
                              onClick={() => setEditingIfaceId(null)}
                            >
                              取消
                            </button>
                          </div>
                        ) : (
                          <div className="code-ignore-path-chips">
                            {(iface.diff_ignore_paths ?? []).map((path, idx) => (
                              <span key={idx} className="code-tag code-tag--muted code-font-mono">
                                {path}
                              </span>
                            ))}
                            {(!iface.diff_ignore_paths || iface.diff_ignore_paths.length === 0) && (
                              <span className="code-color-muted code-text-xs">无</span>
                            )}
                            {isAdmin && (
                              <button
                                type="button"
                                className="code-btn-text code-text-xs"
                                onClick={() => handleStartEditPaths(iface)}
                              >
                                ✎
                              </button>
                            )}
                          </div>
                        )}
                      </td>
                      <td>{iface.description || '-'}</td>
                      {isAdmin && (
                        <td>
                          <button
                            type="button"
                            className="code-btn code-btn--danger code-btn--xs"
                            onClick={() => onDeleteInterface(iface.id)}
                          >
                            删除
                          </button>
                        </td>
                      )}
                    </tr>
                  ))}
                  {interfaces.length === 0 && (
                    <tr>
                      <td colSpan={isAdmin ? 6 : 5} className="code-table-empty">
                        暂无接口契约声明
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      ) : (
        <div className="code-empty-pane code-p-lg">请先在左侧选择或新建一个功能模块</div>
      )}
    </div>
  );
};
