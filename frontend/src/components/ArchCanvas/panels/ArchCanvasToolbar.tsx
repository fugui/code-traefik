import React from 'react';
import { Search, RotateCcw, Eye, Layers, X } from 'lucide-react';
import type { RecordingSession } from '../../../types';

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
}) => {
  return (
    <div className="code-arch-toolbar">
      {/* 左侧控制区 */}
      <div className="code-arch-toolbar__left">
        {/* 视角切换 Segmented Control */}
        <div className="code-arch-segmented" role="tablist">
          <button
            type="button"
            className={`code-arch-segmented__item ${view === 'LOGICAL' ? 'code-arch-segmented__item--active' : ''}`}
            onClick={() => onViewChange('LOGICAL')}
          >
            逻辑架构视图
          </button>
          <button
            type="button"
            className={`code-arch-segmented__item ${view === 'PHYSICAL' ? 'code-arch-segmented__item--active' : ''}`}
            onClick={() => onViewChange('PHYSICAL')}
          >
            物理部署视图
          </button>
        </div>

        <div className="code-arch-toolbar__divider" />

        {/* 层级滑杆 (L0 ~ Ln) */}
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
