import React from 'react';
import { Search, RotateCcw, Eye, Layers } from 'lucide-react';
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
      {/* 视角切换 */}
      <div className="code-arch-toolbar__group">
        <button
          type="button"
          className={`code-btn code-btn--sm ${view === 'LOGICAL' ? 'code-btn--primary' : 'code-btn--secondary'}`}
          onClick={() => onViewChange('LOGICAL')}
        >
          逻辑架构视图
        </button>
        <button
          type="button"
          className={`code-btn code-btn--sm ${view === 'PHYSICAL' ? 'code-btn--primary' : 'code-btn--secondary'}`}
          onClick={() => onViewChange('PHYSICAL')}
        >
          物理部署视图
        </button>
      </div>

      <div className="code-arch-toolbar__divider" />

      {/* 层级滑杆 (L0 ~ Ln) */}
      <div className="code-arch-toolbar__slider-group" title={`当前展开层级: L${depthLevel}`}>
        <Layers size={14} className="code-arch-toolbar__icon" />
        <span className="code-arch-toolbar__label">层级: L{depthLevel}</span>
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
        <span>滚轮自动展开</span>
      </label>

      {/* 重置布局 */}
      <button
        type="button"
        className="code-btn code-btn--secondary code-btn--sm"
        onClick={onResetLayout}
        title="清除手动拖拽偏移并恢复全局最优排版"
      >
        <RotateCcw size={13} />
        <span>重置布局</span>
      </button>

      <div className="code-arch-toolbar__divider" />

      {/* 全图搜索 */}
      <div className="code-arch-toolbar__search-box">
        <Search size={13} className="code-arch-toolbar__search-icon" />
        <input
          type="text"
          placeholder="搜索模块/进程/子系统..."
          value={searchKeyword}
          onChange={(e) => onSearchChange(e.target.value)}
          className="code-arch-toolbar__search-input"
        />
      </div>

      <div className="code-arch-toolbar__divider" />

      {/* 录制批次与一致性叠加挂载 */}
      <div className="code-arch-toolbar__session-group">
        <Eye size={14} className="code-arch-toolbar__icon" />
        <span className="code-arch-toolbar__label">叠加批次:</span>
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
            className="code-btn code-btn--secondary code-btn--xs"
            onClick={() => onSelectSession(null)}
          >
            取消挂载
          </button>
        )}
      </div>
    </div>
  );
};

