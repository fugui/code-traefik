import React from 'react';
import {
  Search,
  RotateCcw,
  Eye,
  Layers,
  X,
  Server,
  Bookmark,
  ChevronRight,
  Compass,
  Filter,
} from 'lucide-react';
import type { RecordingSession, Agent } from '../../../types';
import type { NeighborhoodDirection } from '../../../types/topology_graph';

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

export interface FocusBreadcrumbItem {
  id: string;
  name: string;
}

export interface OwnerFilterOption {
  id: number;
  name: string;
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
  focusScope?: string;
  focusBreadcrumbs?: FocusBreadcrumbItem[];
  onFocusScopeChange?: (scope: string) => void;
  neighborhoodHop?: number;
  onNeighborhoodHopChange?: (hop: number) => void;
  neighborhoodDirection?: NeighborhoodDirection;
  onNeighborhoodDirectionChange?: (dir: NeighborhoodDirection) => void;
  ownerOptions?: OwnerFilterOption[];
  ownerId?: number | null;
  onOwnerIdChange?: (ownerId: number | null) => void;
  consistencyOnly?: boolean;
  onToggleConsistencyOnly?: (enabled: boolean) => void;
  hideIsolated?: boolean;
  onToggleHideIsolated?: (enabled: boolean) => void;
  foldHubEdges?: boolean;
  onToggleFoldHubEdges?: (enabled: boolean) => void;
  hasUnsavedDraft?: boolean;
  bookmarkQuotaText?: string;
  onOpenBookmarkDrawer?: () => void;
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
  focusScope = 'GLOBAL',
  focusBreadcrumbs = [],
  onFocusScopeChange,
  neighborhoodHop = 0,
  onNeighborhoodHopChange,
  neighborhoodDirection = 'BOTH',
  onNeighborhoodDirectionChange,
  ownerOptions = [],
  ownerId = null,
  onOwnerIdChange,
  consistencyOnly = false,
  onToggleConsistencyOnly,
  hideIsolated = false,
  onToggleHideIsolated,
  foldHubEdges = true,
  onToggleFoldHubEdges,
  hasUnsavedDraft = false,
  bookmarkQuotaText,
  onOpenBookmarkDrawer,
}) => {
  return (
    <div className="code-arch-toolbar-stack">
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
              <Layers size={14} className="code-arch-toolbar__btn-icon" />
              逻辑架构
            </button>
            <button
              type="button"
              className={`code-arch-segmented__item ${view === 'PHYSICAL' ? 'code-arch-segmented__item--active' : ''}`}
              onClick={() => onViewChange('PHYSICAL')}
              data-testid="toggle-topo-physical"
            >
              <Server size={14} className="code-arch-toolbar__btn-icon" />
              物理架构
            </button>
          </div>

          <div className="code-arch-toolbar__divider" />

          {/* 物理部署视图下：多环境端点叠加选择器 */}
          {view === 'PHYSICAL' && (
            <div className="code-arch-toolbar__session-group" data-testid="env-overlay-group">
              <Server size={14} className="code-arch-toolbar__icon" />
              <span className="code-arch-toolbar__label">环境叠加:</span>
              <select
                value={selectedEnvAgentId || ''}
                onChange={(e) => onSelectEnvAgentId?.(e.target.value || null)}
                className="code-arch-toolbar__select"
                data-testid="env-agent-selector"
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

          <label className="code-arch-toolbar__checkbox-label" title="开启后根据缩放比例自动展开容器">
            <input
              type="checkbox"
              checked={wheelAutoExpand}
              onChange={(e) => onToggleWheelAutoExpand(e.target.checked)}
            />
            <span>滚轮展开</span>
          </label>

          {/* 有向 N-Hop 邻域筛选器 */}
          {onNeighborhoodHopChange && (
            <div className="code-arch-toolbar__session-group">
              <Filter size={13} className="code-arch-toolbar__icon" />
              <select
                value={String(neighborhoodHop)}
                onChange={(e) => onNeighborhoodHopChange(Number(e.target.value))}
                className="code-arch-toolbar__select code-arch-toolbar__select--compact"
                data-testid="topo-hop-select"
                title="有向 N-Hop 邻域筛选"
              >
                <option value="0">全邻域 (0-Hop)</option>
                <option value="1">1-Hop 邻域</option>
                <option value="2">2-Hop 邻域</option>
              </select>
              {neighborhoodHop > 0 && onNeighborhoodDirectionChange && (
                <select
                  value={neighborhoodDirection}
                  onChange={(e) =>
                    onNeighborhoodDirectionChange(e.target.value as NeighborhoodDirection)
                  }
                  className="code-arch-toolbar__select code-arch-toolbar__select--compact"
                  data-testid="topo-dir-select"
                  title="邻域追踪方向"
                >
                  <option value="BOTH">双向关联</option>
                  <option value="UPSTREAM">仅看上游调用方</option>
                  <option value="DOWNSTREAM">仅看下游被调方</option>
                </select>
              )}
            </div>
          )}

          {/* 负责人过滤 */}
          {onOwnerIdChange && ownerOptions.length > 0 && (
            <select
              value={ownerId != null ? String(ownerId) : ''}
              onChange={(e) =>
                onOwnerIdChange(e.target.value ? Number(e.target.value) : null)
              }
              className="code-arch-toolbar__select code-arch-toolbar__select--compact"
              data-testid="topo-owner-select"
              title="按模块负责人筛选"
            >
              <option value="">全部负责人</option>
              {ownerOptions.map((o) => (
                <option key={o.id} value={String(o.id)}>
                  {o.name}
                </option>
              ))}
            </select>
          )}

          {/* 降噪快捷勾选 */}
          {onToggleConsistencyOnly && (
            <label className="code-arch-toolbar__checkbox-label" title="仅保留存在 VIOLATION / UNATTRIBUTED 异常调用的节点链路">
              <input
                type="checkbox"
                checked={consistencyOnly}
                onChange={(e) => onToggleConsistencyOnly(e.target.checked)}
                data-testid="toggle-consistency-only"
              />
              <span>仅看异常</span>
            </label>
          )}

          {onToggleHideIsolated && (
            <label className="code-arch-toolbar__checkbox-label" title="自动隐藏入度=0 且出度=0 的孤立模块与空容器">
              <input
                type="checkbox"
                checked={hideIsolated}
                onChange={(e) => onToggleHideIsolated(e.target.checked)}
                data-testid="toggle-hide-isolated"
              />
              <span>隐藏孤立</span>
            </label>
          )}

          {onToggleFoldHubEdges && (
            <label className="code-arch-toolbar__checkbox-label" title="对出/入边 > 8 的高扇出 Hub 节点自动折叠次要正常边">
              <input
                type="checkbox"
                checked={foldHubEdges}
                onChange={(e) => onToggleFoldHubEdges(e.target.checked)}
                data-testid="toggle-fold-hub"
              />
              <span>折叠Hub边</span>
            </label>
          )}
        </div>

        {/* 中部全景架构资产摘要 */}
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
              data-testid="topo-search-input"
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

          {/* 书签与基准管理抽屉入口 */}
          {onOpenBookmarkDrawer && (
            <button
              type="button"
              className="code-btn code-btn--secondary code-btn--sm code-arch-toolbar__reset-btn"
              onClick={onOpenBookmarkDrawer}
              title="管理个人视图书签与团队共享基准布局"
              data-testid="open-bookmark-drawer-btn"
            >
              <Bookmark size={13} />
              <span>书签{bookmarkQuotaText ? ` (${bookmarkQuotaText})` : ''}</span>
              {hasUnsavedDraft && (
                <span className="code-arch-badge code-arch-badge--warning">草稿</span>
              )}
            </button>
          )}

          {/* 重置布局 */}
          <button
            type="button"
            className="code-btn code-btn--secondary code-btn--sm code-arch-toolbar__reset-btn"
            onClick={onResetLayout}
            title="清除手动拖拽偏移并恢复全局最优排版"
            data-testid="reset-topo-layout-btn"
          >
            <RotateCcw size={13} />
            <span>重置布局</span>
          </button>
        </div>
      </div>

      {/* 下钻聚焦面包屑导航条 (Focus Scope Breadcrumb Bar) */}
      {focusScope && focusScope !== 'GLOBAL' && (
        <div className="code-arch-focus-bar" data-testid="arch-focus-bar">
          <div className="code-arch-focus-bar__crumbs">
            <Compass size={13} className="code-arch-focus-bar__icon" />
            <span className="code-arch-focus-bar__label">当前聚焦子图:</span>
            {(focusBreadcrumbs.length > 0
              ? focusBreadcrumbs
              : [
                  { id: 'GLOBAL', name: '全局全景' },
                  { id: focusScope, name: focusScope },
                ]
            ).map((crumb, idx, arr) => {
              const isLast = idx === arr.length - 1;
              return (
                <React.Fragment key={crumb.id}>
                  <button
                    type="button"
                    className={`code-arch-focus-bar__crumb ${
                      isLast ? 'code-arch-focus-bar__crumb--active' : ''
                    }`}
                    onClick={() => onFocusScopeChange?.(crumb.id)}
                    data-testid={`focus-crumb-${crumb.id}`}
                  >
                    {crumb.name}
                  </button>
                  {!isLast && <ChevronRight size={12} className="code-arch-focus-bar__sep" />}
                </React.Fragment>
              );
            })}
          </div>

          <button
            type="button"
            className="code-btn code-btn--secondary code-btn--xs"
            onClick={() => onFocusScopeChange?.('GLOBAL')}
            data-testid="exit-focus-scope-btn"
          >
            退出聚焦
          </button>
        </div>
      )}
    </div>
  );
};
