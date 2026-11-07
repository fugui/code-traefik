import React from 'react';
import { Search, RotateCcw, Eye, Layers, X, Server } from 'lucide-react';
import type { RecordingSession, Agent } from '../../../types';

export interface ArchStats {
  subsystems: number;
  functionalGroups: number;
  modules: number;
  processGroups: number;
  processes: number;
  externals: number;
  unreg: number;
  edgeCount: number;
}

export interface ArchCanvasToolbarProps {
  view: 'LOGICAL' | 'PHYSICAL';
  onViewChange: (view: 'LOGICAL' | 'PHYSICAL') => void;
  depthLevel: number;
  maxDepth: number;
  onDepthLevelChange: (level: number) => void;
  wheelAutoExpand: boolean;
  onToggleWheelAutoExpand: (enabled: boolean) => void;
  onResetLayout: () => void;
  searchKeyword: string;
  onSearchChange: (keyword: string) => void;
  sessions: RecordingSession[];
  selectedSessionId: string | null;
  onSelectSession: (sessionId: string | null) => void;
  agents?: Agent[];
  selectedEnvAgentId?: string | null;
  onSelectEnvAgentId?: (agentId: string | null) => void;
  stats?: ArchStats | null;
}

export const ArchCanvasToolbar: React.FC<ArchCanvasToolbarProps> = ({
  view,
  onViewChange,
  depthLevel,
  maxDepth,
  onDepthLevelChange,
  wheelAutoExpand,
  onToggleWheelAutoExpand,
  onResetLayout,
  searchKeyword,
  onSearchChange,
  sessions,
  selectedSessionId,
  onSelectSession,
  agents = [],
  selectedEnvAgentId = null,
  onSelectEnvAgentId,
  stats,
}) => {
  return (
    <div className="code-arch-toolbar">
      {/* 左侧控制区 */}
      <div className="code-arch-toolbar__left">
        {/* 视角切换 Segmented Control */}
        <div className="code-arch-segmented" role="tablist" data-testid="topology-view-segmented">
          <button
            type="button"
            className={`code-arch-segmented__item ${view === 'LOGICAL' ? 'code-arch-segmented__item--active' : ''}`}
            onClick={() => onViewChange('LOGICAL')}
            data-testid="toggle-topo-logical"
          >
            <Layers size={14} style={{ marginRight: 6, verticalAlign: 'text-bottom' }} />
            逻辑架构
          </button>
          <button
            type="button"
            className={`code-arch-segmented__item ${view === 'PHYSICAL' ? 'code-arch-segmented__item--active' : ''}`}
            onClick={() => onViewChange('PHYSICAL')}
            data-testid="toggle-topo-physical"
          >
            <Server size={14} style={{ marginRight: 6, verticalAlign: 'text-bottom' }} />
            物理架构
          </button>
        </div>

        <div className="code-arch-toolbar__divider" />

        {/* 物理部署视图下：多环境端点叠加选择器 (AC-M10-04) */}
        {view === 'PHYSICAL' && (
          <div className="code-arch-toolbar__session-group" data-testid="env-overlay-group">
            <Server size={14} className="code-arch-toolbar__icon" />
            <span className="code-arch-toolbar__label">环境叠加:</span>
            <select
              value={selectedEnvAgentId || ''}
              onChange={(e) => onSelectEnvAgentId?.(e.target.value || null)}
              className="code-arch-toolbar__select"
              data-testid="env-agent-selector"
              style={{ maxWidth: '200px' }}
            >
              <option value="">纯架构蓝图 (设计态)</option>
              {agents.map((ag) => (
                <option key={ag.id} value={ag.id}>
                  [{ag.scope}] #{ag.id} {ag.name}
                </option>
              ))}
            </select>
            {selectedEnvAgentId && (
              <button
                type="button"
                className="code-btn code-btn--secondary code-btn--xs code-arch-toolbar__detach-btn"
                onClick={() => onSelectEnvAgentId?.(null)}
                title="清除环境叠加，恢复纯设计态"
                data-testid="clear-env-overlay-btn"
              >
                清除
              </button>
            )}
          </div>
        )}

        {view === 'PHYSICAL' && <div className="code-arch-toolbar__divider" />}

        {/* 层级滑杆 (L0 ~ Ln) 与快捷展开 */}
        <div className="code-arch-toolbar__slider-group" title={`当前展开层级: L${depthLevel} (0 ~ ${maxDepth})`}>
          <Layers size={14} className="code-arch-toolbar__icon" />
          <span className="code-arch-toolbar__label">层级:</span>
          <span className="code-arch-toolbar__depth-pill">L{depthLevel}</span>
          <input
            type="range"
            min={0}
            max={Math.max(1, maxDepth)}
            value={depthLevel}
            onChange={(e) => onDepthLevelChange(Number(e.target.value))}
            className="code-arch-toolbar__slider"
          />
          {maxDepth > 0 && (
            <div className="code-arch-toolbar__quick-actions">
              <button
                type="button"
                className="code-arch-toolbar__quick-depth-btn"
                onClick={() => onDepthLevelChange(0)}
                title="一键折叠收起到顶层容器 (L0)"
                disabled={depthLevel === 0}
              >
                顶层
              </button>
              <button
                type="button"
                className="code-arch-toolbar__quick-depth-btn"
                onClick={() => onDepthLevelChange(maxDepth)}
                title={`一键平铺全展到深层微服务模块 (L${maxDepth})`}
                disabled={depthLevel === maxDepth}
              >
                全展
              </button>
            </div>
          )}
        </div>

        {/* 滚轮自动展开开关 */}
        <label className="code-arch-toolbar__checkbox-label" title="开启后根据缩放比例自动展开容器">
          <input
            type="checkbox"
            checked={wheelAutoExpand}
            onChange={(e) => onToggleWheelAutoExpand(e.target.checked)}
          />
          <span>滚轮展开</span>
        </label>

        {/* 重置布局 */}
        <button
          type="button"
          className="code-btn code-btn--secondary code-btn--sm code-arch-toolbar__reset-btn"
          onClick={onResetLayout}
          title="清除手动拖拽偏移并恢复全局最优排版"
        >
          <RotateCcw size={13} />
          <span>重置布局</span>
        </button>
      </div>

      {/* 中部全景架构资产摘要 (充分利用横向空白，直观展现系统全景规模) */}
      {stats && (
        <div className="code-arch-toolbar__center" data-testid="arch-toolbar-stats">
          {view === 'LOGICAL' ? (
            <>
              <span className="code-arch-stat-badge" title="逻辑子系统总数">
                子系统: <strong>{stats.subsystems}</strong>
              </span>
              <span className="code-arch-stat-badge" title="功能组总数">
                功能组: <strong>{stats.functionalGroups}</strong>
              </span>
              <span className="code-arch-stat-badge" title="微服务模块总数">
                模块: <strong>{stats.modules}</strong>
              </span>
              <span className="code-arch-stat-badge" title="声明契约边总数">
                契约依赖: <strong>{stats.edgeCount}</strong>
              </span>
              {stats.externals > 0 && (
                <span className="code-arch-stat-badge" title="外部依赖系统/端点数">
                  外部依赖: <strong>{stats.externals}</strong>
                </span>
              )}
            </>
          ) : (
            <>
              <span className="code-arch-stat-badge" title="物理进程组总数">
                进程组: <strong>{stats.processGroups}</strong>
              </span>
              <span className="code-arch-stat-badge" title="纳管物理进程总数">
                物理进程: <strong>{stats.processes}</strong>
              </span>
              <span className="code-arch-stat-badge" title="承载模块总数">
                挂载模块: <strong>{stats.modules}</strong>
              </span>
              <span className="code-arch-stat-badge" title="物理通信链路数">
                物理链路: <strong>{stats.edgeCount}</strong>
              </span>
              {stats.externals > 0 && (
                <span className="code-arch-stat-badge" title="外部依赖端点数">
                  外部系统: <strong>{stats.externals}</strong>
                </span>
              )}
            </>
          )}
        </div>
      )}

      {/* 右侧控制区 */}
      <div className="code-arch-toolbar__right">
        {/* 全图搜索 */}
        <div className="code-arch-toolbar__search-box">
          <Search size={13} className="code-arch-toolbar__search-icon" />
          <input
            type="text"
            placeholder="搜索模块 / 进程 / 子系统..."
            value={searchKeyword}
            onChange={(e) => onSearchChange(e.target.value)}
            className="code-arch-toolbar__search-input"
          />
          {searchKeyword && (
            <button
              type="button"
              className="code-arch-toolbar__search-clear"
              onClick={() => onSearchChange('')}
              title="清除搜索"
            >
              <X size={12} />
            </button>
          )}
        </div>

        <div className="code-arch-toolbar__divider" />

        {/* 录制批次与一致性叠加挂载 */}
        <div className="code-arch-toolbar__session-group">
          <Eye size={14} className="code-arch-toolbar__icon" />
          <span className="code-arch-toolbar__label">比对批次:</span>
          <select
            value={selectedSessionId || ''}
            onChange={(e) => onSelectSession(e.target.value || null)}
            className="code-arch-toolbar__select"
          >
            <option value="">(未挂载比对)</option>
            {sessions.map((s) => (
              <option key={s.id} value={s.id}>
                [{s.scope}] #{s.id} - {s.name} ({s.status})
              </option>
            ))}
          </select>
          {selectedSessionId && (
            <button
              type="button"
              className="code-btn code-btn--secondary code-btn--xs code-arch-toolbar__detach-btn"
              onClick={() => onSelectSession(null)}
              title="取消挂载该录制批次"
            >
              取消
            </button>
          )}
        </div>
      </div>
    </div>
  );
};
