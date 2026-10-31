import React from 'react';
import { ChevronRight, Cpu, Server, FileCode, Edit3, Trash2 } from 'lucide-react';
import type { Process, Module } from '../../types';

export interface ProcessDetailPaneProps {
  process: Process;
  allModules: Module[];
  isAdmin: boolean;
  onEditProcess?: (proc: Process) => void;
  onDeleteProcess?: (procId: string) => void;
  onSelectModule: (moduleId: string) => void;
}

export const ProcessDetailPane: React.FC<ProcessDetailPaneProps> = ({
  process,
  isAdmin,
  onEditProcess,
  onDeleteProcess,
  onSelectModule,
}) => {
  const boundModules = process.process_modules ?? [];

  return (
    <div className="code-module-pane code-process-detail-pane">
      {/* 顶部微型面包屑 */}
      <div className="code-module-breadcrumb">
        <span className="code-module-breadcrumb__item">物理架构</span>
        <ChevronRight size={13} className="code-module-breadcrumb__sep" />
        <span className="code-module-breadcrumb__item">{process.group_name || '默认进程组'}</span>
        <ChevronRight size={13} className="code-module-breadcrumb__sep" />
        <span className="code-module-breadcrumb__item code-module-breadcrumb__item--current">
          {process.name} {process.listen_port > 0 ? `(Port: ${process.listen_port})` : '(纯客户端)'}
        </span>
      </div>

      {/* 进程基本信息概览卡片 */}
      <div className="code-module-meta-card">
        <div className="code-module-meta-header" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.6rem' }}>
            <Server size={22} style={{ color: 'var(--color-primary)' }} />
            <div>
              <h3 className="code-module-title" style={{ margin: 0 }}>
                {process.name}
                {process.listen_port > 0 ? (
                  <span className="code-badge code-badge--info" style={{ marginLeft: '0.6rem', fontSize: '0.75rem' }}>
                    Port: {process.listen_port}
                  </span>
                ) : (
                  <span className="code-badge code-badge--muted" style={{ marginLeft: '0.6rem', fontSize: '0.75rem' }}>
                    纯客户端 (无监听)
                  </span>
                )}
                <span className="code-badge code-badge--secondary" style={{ marginLeft: '0.4rem', fontSize: '0.75rem' }}>
                  {process.group_name || '默认进程组'}
                </span>
              </h3>
              <p className="code-module-desc" style={{ marginTop: '0.25rem', marginBottom: 0 }}>
                {process.description || '暂无进程说明'}
              </p>
            </div>
          </div>
          {isAdmin && (
            <div className="code-module-form-actions" style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              {onEditProcess && (
                <button
                  type="button"
                  className="code-btn code-btn--secondary code-btn--xs"
                  onClick={() => onEditProcess(process)}
                >
                  <Edit3 size={13} style={{ marginRight: '0.25rem' }} /> 编辑进程
                </button>
              )}
              {onDeleteProcess && (
                <button
                  type="button"
                  className="code-btn code-btn--danger-outline code-btn--xs"
                  onClick={() => onDeleteProcess(process.id)}
                >
                  <Trash2 size={13} style={{ marginRight: '0.25rem' }} /> 删除进程
                </button>
              )}
            </div>
          )}
        </div>

        <div className="code-meta-grid" style={{ marginTop: '0.875rem' }}>
          <div className="code-meta-item">
            <span className="code-meta-label">监听端口:</span>
            <span className="code-meta-val code-font-mono code-color-primary">
              {process.listen_port > 0 ? process.listen_port : '无 (纯客户端)'}
            </span>
          </div>
          <div className="code-meta-item">
            <span className="code-meta-label">所属 Agent 宿主:</span>
            <span className="code-meta-val">{process.agent?.name || `Agent #${process.agent_id}`}</span>
          </div>
          <div className="code-meta-item">
            <span className="code-meta-label">命令行特征:</span>
            <span className="code-meta-val code-font-mono code-text-xs">
              {process.cmd_pattern || `./bin/${process.name}`}
            </span>
          </div>
          <div className="code-meta-item">
            <span className="code-meta-label">装配功能模块数:</span>
            <span className="code-meta-val" style={{ fontWeight: 600 }}>
              {boundModules.length} 个功能模块
            </span>
          </div>
        </div>
      </div>

      {/* 装配承载的功能模块清单 */}
      <div className="code-interface-section">
        <div className="code-interface-header">
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <Cpu size={16} style={{ color: 'var(--color-primary)' }} />
            <span className="code-interface-title">装配承载的功能模块 (Functional Modules)</span>
          </div>
          <span className="code-color-muted code-text-xs">
            该进程编译并承载以下功能模块，模块间相互调用为同进程调用 (INTRA_PROCESS)
          </span>
        </div>

        <div className="code-table-wrapper">
          <table className="code-table">
            <thead>
              <tr>
                <th>模块代码 (Code)</th>
                <th>模块名称 (Name)</th>
                <th>模块说明</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {boundModules.map((pm) => (
                <tr key={pm.module_id}>
                  <td className="code-font-mono code-color-primary">
                    <span style={{ display: 'inline-flex', alignItems: 'center', gap: '0.35rem' }}>
                      <FileCode size={14} style={{ color: '#10b981' }} />
                      {pm.module?.code || `MOD_${pm.module_id}`}
                    </span>
                  </td>
                  <td>{pm.module?.name || `模块 #${pm.module_id}`}</td>
                  <td>-</td>
                  <td>
                    <button
                      type="button"
                      className="code-btn code-btn--secondary code-btn--xs"
                      onClick={() => onSelectModule(pm.module_id)}
                    >
                      查看接口契约
                    </button>
                  </td>
                </tr>
              ))}
              {boundModules.length === 0 && (
                <tr>
                  <td colSpan={4} className="code-table-empty">
                    当前进程尚未装配承载任何功能模块，可在创建或编辑模块时绑定至此进程
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
};

