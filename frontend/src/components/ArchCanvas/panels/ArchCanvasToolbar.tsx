import React, { useState, useMemo, useRef, useEffect } from 'react';
import {
  Search,
  Sparkles,
  Eye,
  Layers,
  X,
  Server,
  Bookmark,
  SlidersHorizontal,
} from 'lucide-react';
import type { RecordingSession, Agent } from '../../../types';
import type {
  GraphNode,
  NeighborhoodDirection,
  TopologyStatusFilter,
} from '../../../types/topology_graph';
import {
  TopologyBreadcrumbBar,
  type FocusBreadcrumbItem,
} from './TopologyBreadcrumbBar';

export type { FocusBreadcrumbItem };

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

export interface OwnerFilterOption {
  id: number;
  name: string;
}

export interface SubsystemFilterOption {
  id: string;
  name: string;
}

export interface ArchCanvasToolbarProps {
  view: 'LOGICAL' | 'PHYSICAL';
  onViewChange: (view: 'LOGICAL' | 'PHYSICAL') => void;
  /** 保留向后兼容字段，M21 工具栏不再渲染全展滑杆与滚轮开关 */
  depthLevel?: number;
  maxDepth?: number;
  onDepthLevelChange?: (level: number) => void;
  wheelAutoExpand?: boolean;
  onToggleWheelAutoExpand?: (enabled: boolean) => void;
  onResetLayout?: () => void;
  onAutoRelayout?: () => void;
  searchKeyword: string;
  onSearchChange: (keyword: string) => void;
  searchCandidates?: GraphNode[];
  onSelectSearchCandidate?: (node: GraphNode) => void;
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
  renderBreadcrumbBar?: boolean;
  statusFilter?: TopologyStatusFilter;
  onStatusFilterChange?: (status: TopologyStatusFilter) => void;
  subsystemOptions?: SubsystemFilterOption[];
  subsystemIds?: string[];
  onSubsystemIdsChange?: (ids: string[]) => void;
  onlyMyScope?: boolean;
  onToggleOnlyMyScope?: (enabled: boolean) => void;
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
  showRealization?: boolean;
  onToggleShowRealization?: (show: boolean) => void;
  contractMode?: 'AGG' | 'EXPAND';
  onContractModeChange?: (mode: 'AGG' | 'EXPAND') => void;
  hasUnsavedDraft?: boolean;
  bookmarkQuotaText?: string;
  onOpenBookmarkDrawer?: () => void;
}

const NODE_TYPE_SHORT_LABEL: Record<string, string> = {
  SUBSYSTEM: '子系统',
  FUNCTIONAL_GROUP: '功能组',
  MODULE: '模块',
  PROCESS_GROUP: '进程组',
  PROCESS: '进程',
  EXTERNAL_CONTAINER: '外部域',
  EXTERNAL_SYSTEM: '外部系统',
  EXTERNAL_ENDPOINT: '外部端点',
};

export const ArchCanvasToolbar: React.FC<ArchCanvasToolbarProps> = ({
  view,
  onViewChange,
  onResetLayout,
  onAutoRelayout,
  searchKeyword,
  onSearchChange,
  searchCandidates = [],
  onSelectSearchCandidate,
  sessions,
  selectedSessionId,
  onSelectSession,
  agents = [],
  selectedEnvAgentId = null,
  onSelectEnvAgentId,
  focusScope = 'GLOBAL',
  focusBreadcrumbs = [],
  onFocusScopeChange,
  renderBreadcrumbBar = true,
  statusFilter,
  onStatusFilterChange,
  subsystemOptions = [],
  subsystemIds = [],
  onSubsystemIdsChange,
  onlyMyScope = false,
  onToggleOnlyMyScope,
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
  showRealization = true,
  onToggleShowRealization,
  contractMode = 'AGG',
  onContractModeChange,
  hasUnsavedDraft = false,
  bookmarkQuotaText,
  onOpenBookmarkDrawer,
}) => {
  const [isFilterPopoverOpen, setIsFilterPopoverOpen] = useState(false);
  const [isSearchFocused, setIsSearchFocused] = useState(false);
  const filterPopoverRef = useRef<HTMLDivElement | null>(null);
  const searchWrapRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    const handleOutsideClick = (event: MouseEvent) => {
      const target = event.target as Node | null;
      if (filterPopoverRef.current && target && !filterPopoverRef.current.contains(target)) {
        setIsFilterPopoverOpen(false);
      }
      if (searchWrapRef.current && target && !searchWrapRef.current.contains(target)) {
        setIsSearchFocused(false);
      }
    };
    document.addEventListener('mousedown', handleOutsideClick);
    return () => document.removeEventListener('mousedown', handleOutsideClick);
  }, []);

  const effectiveStatusFilter: TopologyStatusFilter =
    statusFilter ?? (consistencyOnly ? 'ABNORMAL' : 'ALL');

  const handleStatusChange = (next: TopologyStatusFilter) => {
    if (onStatusFilterChange) {
      onStatusFilterChange(next);
    }
    if (onToggleConsistencyOnly) {
      onToggleConsistencyOnly(next !== 'ALL');
    }
  };

  // 计算非默认激活过滤项数量（用于 Popover 计数角标）
  const activeFilterCount = useMemo(() => {
    let count = 0;
    if (subsystemIds.length > 0) count += 1;
    if (onlyMyScope) count += 1;
    if (neighborhoodHop > 0) count += 1;
    if (ownerId != null) count += 1;
    if (hideIsolated) count += 1;
    if (!foldHubEdges) count += 1;
    if (view === 'LOGICAL' && !showRealization) count += 1;
    if (view === 'LOGICAL' && contractMode === 'EXPAND') count += 1;
    return count;
  }, [
    subsystemIds.length,
    onlyMyScope,
    neighborhoodHop,
    ownerId,
    hideIsolated,
    foldHubEdges,
    view,
    showRealization,
    contractMode,
  ]);

  const handleToggleSubsystem = (id: string) => {
    if (!onSubsystemIdsChange) return;
    if (subsystemIds.includes(id)) {
      onSubsystemIdsChange(subsystemIds.filter((s) => s !== id));
    } else {
      onSubsystemIdsChange([...subsystemIds, id]);
    }
  };

  const handleRelayoutClick = () => {
    if (onAutoRelayout) {
      onAutoRelayout();
    } else if (onResetLayout) {
      onResetLayout();
    }
  };

  const limitedCandidates = useMemo(
    () => (searchKeyword.trim() ? searchCandidates.slice(0, 20) : []),
    [searchKeyword, searchCandidates]
  );

  // 构建可独立叉掉的 Filter Chips
  const filterChips = useMemo(() => {
    const chips: Array<{ key: string; label: string; onRemove: () => void }> = [];

    if (effectiveStatusFilter !== 'ALL') {
      chips.push({
        key: 'status',
        label: `状态: ${effectiveStatusFilter === 'VIOLATION_ONLY' ? '仅违规' : '仅异常'}`,
        onRemove: () => handleStatusChange('ALL'),
      });
    }

    if (subsystemIds.length > 0 && onSubsystemIdsChange) {
      const names = subsystemIds
        .map((id) => subsystemOptions.find((o) => o.id === id)?.name || id)
        .join('、');
      chips.push({
        key: 'subsystems',
        label: `关注域: ${names}`,
        onRemove: () => onSubsystemIdsChange([]),
      });
    }

    if (onlyMyScope && onToggleOnlyMyScope) {
      chips.push({
        key: 'only-my',
        label: '仅看与我相关',
        onRemove: () => onToggleOnlyMyScope(false),
      });
    }

    if (neighborhoodHop > 0 && onNeighborhoodHopChange) {
      const dirLabel =
        neighborhoodDirection === 'UPSTREAM'
          ? '上游'
          : neighborhoodDirection === 'DOWNSTREAM'
          ? '下游'
          : '双向';
      chips.push({
        key: 'hop',
        label: `邻域: ${neighborhoodHop}-Hop (${dirLabel})`,
        onRemove: () => onNeighborhoodHopChange(0),
      });
    }

    if (ownerId != null && onOwnerIdChange) {
      const ownerName = ownerOptions.find((o) => o.id === ownerId)?.name || `#${ownerId}`;
      chips.push({
        key: 'owner',
        label: `负责人: ${ownerName}`,
        onRemove: () => onOwnerIdChange(null),
      });
    }

    if (hideIsolated && onToggleHideIsolated) {
      chips.push({
        key: 'hide-isolated',
        label: '已隐藏孤立节点',
        onRemove: () => onToggleHideIsolated(false),
      });
    }

    if (view === 'LOGICAL' && !showRealization && onToggleShowRealization) {
      chips.push({
        key: 'hide-realization',
        label: '已隐藏实现虚线',
        onRemove: () => onToggleShowRealization(true),
      });
    }

    if (view === 'LOGICAL' && contractMode === 'EXPAND' && onContractModeChange) {
      chips.push({
        key: 'contract-expand',
        label: '契约: 展开到实现',
        onRemove: () => onContractModeChange('AGG'),
      });
    }

    return chips;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    effectiveStatusFilter,
    subsystemIds,
    subsystemOptions,
    onlyMyScope,
    neighborhoodHop,
    neighborhoodDirection,
    ownerId,
    ownerOptions,
    hideIsolated,
    view,
    showRealization,
    contractMode,
  ]);

  return (
    <div className="code-arch-toolbar-stack">
      <div className="code-arch-toolbar">
        {/* 组 1: 视图与数据源 */}
        <div className="code-arch-toolbar__group" data-testid="toolbar-group-view">
          <div className="code-arch-segmented" role="tablist" data-testid="topology-view-segmented">
            <button
              type="button"
              className={`code-arch-segmented__item ${
                view === 'LOGICAL' ? 'code-arch-segmented__item--active' : ''
              }`}
              onClick={() => onViewChange('LOGICAL')}
              data-testid="toggle-topo-logical"
            >
              <Layers size={14} className="code-arch-toolbar__btn-icon" />
              逻辑架构
            </button>
            <button
              type="button"
              className={`code-arch-segmented__item ${
                view === 'PHYSICAL' ? 'code-arch-segmented__item--active' : ''
              }`}
              onClick={() => onViewChange('PHYSICAL')}
              data-testid="toggle-topo-physical"
            >
              <Server size={14} className="code-arch-toolbar__btn-icon" />
              物理架构
            </button>
          </div>

          {view === 'PHYSICAL' && onSelectEnvAgentId && (
            <div className="code-arch-toolbar__session-group" data-testid="env-overlay-group">
              <select
                value={selectedEnvAgentId || ''}
                onChange={(e) => onSelectEnvAgentId(e.target.value || null)}
                className="code-arch-toolbar__select code-arch-toolbar__select--compact"
                data-testid="env-agent-selector"
                title="物理部署环境端点叠加"
              >
                <option value="">纯架构蓝图 (设计态)</option>
                {agents.map((ag) => (
                  <option key={ag.id} value={ag.id}>
                    [{ag.scope}] #{ag.id} {ag.name}
                  </option>
                ))}
              </select>
            </div>
          )}

          <div className="code-arch-toolbar__session-group">
            <Eye size={14} className="code-arch-toolbar__icon" />
            <select
              value={selectedSessionId || ''}
              onChange={(e) => onSelectSession(e.target.value || null)}
              className="code-arch-toolbar__select code-arch-toolbar__select--compact"
              data-testid="session-overlay-select"
              title="挂载流量比对录制批次"
            >
              <option value="">纯设计态 (未挂录制)</option>
              {sessions.map((s) => (
                <option key={s.id} value={s.id}>
                  [{s.scope}] #{s.id} - {s.name} ({s.status})
                </option>
              ))}
            </select>
          </div>
        </div>

        <div className="code-arch-toolbar__divider" />

        {/* 组 2: 全局定位与候选下钻搜索 */}
        <div
          className="code-arch-toolbar__group code-arch-toolbar__search-wrap"
          data-testid="toolbar-group-search"
          ref={searchWrapRef}
        >
          <div className="code-arch-toolbar__search-box">
            <Search size={13} className="code-arch-toolbar__search-icon" />
            <input
              type="text"
              placeholder="搜名称/编码，点选自动下钻定位..."
              value={searchKeyword}
              onFocus={() => setIsSearchFocused(true)}
              onChange={(e) => {
                onSearchChange(e.target.value);
                setIsSearchFocused(true);
              }}
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

          {isSearchFocused && limitedCandidates.length > 0 && (
            <div className="code-arch-search-dropdown" data-testid="topo-search-dropdown">
              {limitedCandidates.map((cand) => (
                <button
                  key={cand.id}
                  type="button"
                  className="code-arch-search-dropdown__item"
                  data-testid={`search-candidate-${cand.id}`}
                  onClick={() => {
                    onSelectSearchCandidate?.(cand);
                    setIsSearchFocused(false);
                  }}
                >
                  <span>{cand.name}</span>
                  <span className="code-arch-search-dropdown__meta">
                    {NODE_TYPE_SHORT_LABEL[cand.type] || cand.type}
                    {cand.code ? ` · ${cand.code}` : ''}
                  </span>
                </button>
              ))}
            </div>
          )}
        </div>

        <div className="code-arch-toolbar__divider" />

        {/* 组 3: 异常焦点分段切换 + 高级筛选图层 Popover */}
        <div
          className="code-arch-toolbar__group code-arch-filter-popover-wrap"
          data-testid="toolbar-group-filter"
          ref={filterPopoverRef}
        >
          <div className="code-arch-segmented" role="group" data-testid="topo-status-segmented">
            <button
              type="button"
              className={`code-arch-segmented__item ${
                effectiveStatusFilter === 'ALL' ? 'code-arch-segmented__item--active' : ''
              }`}
              onClick={() => handleStatusChange('ALL')}
              data-testid="status-filter-all"
            >
              全部
            </button>
            <button
              type="button"
              className={`code-arch-segmented__item ${
                effectiveStatusFilter === 'ABNORMAL' ? 'code-arch-segmented__item--active' : ''
              }`}
              onClick={() => handleStatusChange('ABNORMAL')}
              data-testid="status-filter-abnormal"
            >
              仅异常
            </button>
            <button
              type="button"
              className={`code-arch-segmented__item ${
                effectiveStatusFilter === 'VIOLATION_ONLY'
                  ? 'code-arch-segmented__item--active'
                  : ''
              }`}
              onClick={() => handleStatusChange('VIOLATION_ONLY')}
              data-testid="status-filter-violation"
            >
              仅违规
            </button>
          </div>

          <button
            type="button"
            className="code-btn code-btn--secondary code-btn--sm"
            onClick={() => setIsFilterPopoverOpen((prev) => !prev)}
            data-testid="topo-filter-popover-btn"
            aria-expanded={isFilterPopoverOpen}
          >
            <SlidersHorizontal size={13} />
            <span>
              {activeFilterCount > 0 ? `筛选与图层 (${activeFilterCount})` : '筛选与图层'}
            </span>
          </button>

          {/* 始终保留 DOM 挂载，通过 CSS 类显隐，保证 SSR 与交互测试均可稳定查询 */}
          <div
            className={`code-arch-filter-popover ${
              isFilterPopoverOpen ? '' : 'code-arch-filter-popover--hidden'
            }`}
            data-testid="topo-filter-popover"
          >
            {subsystemOptions.length > 0 && onSubsystemIdsChange && (
              <div className="code-arch-filter-popover__section">
                <span className="code-arch-filter-popover__title">关注域多选</span>
                <div className="code-arch-filter-popover__subsystem-list">
                  {subsystemOptions.map((sub) => (
                    <label key={sub.id} className="code-arch-toolbar__checkbox-label">
                      <input
                        type="checkbox"
                        checked={subsystemIds.includes(sub.id)}
                        onChange={() => handleToggleSubsystem(sub.id)}
                        data-testid={`filter-subsystem-${sub.id}`}
                      />
                      <span>{sub.name}</span>
                    </label>
                  ))}
                </div>
              </div>
            )}

            <div className="code-arch-filter-popover__section">
              <span className="code-arch-filter-popover__title">归属与降噪规则</span>
              {onToggleOnlyMyScope && (
                <label className="code-arch-toolbar__checkbox-label">
                  <input
                    type="checkbox"
                    checked={onlyMyScope}
                    onChange={(e) => onToggleOnlyMyScope(e.target.checked)}
                    data-testid="toggle-only-my-scope"
                  />
                  <span>仅看与我相关</span>
                </label>
              )}

              {onToggleHideIsolated && (
                <label className="code-arch-toolbar__checkbox-label">
                  <input
                    type="checkbox"
                    checked={hideIsolated}
                    onChange={(e) => onToggleHideIsolated(e.target.checked)}
                    data-testid="toggle-hide-isolated"
                  />
                  <span>隐藏无连线孤立节点</span>
                </label>
              )}

              {onToggleFoldHubEdges && (
                <label className="code-arch-toolbar__checkbox-label">
                  <input
                    type="checkbox"
                    checked={foldHubEdges}
                    onChange={(e) => onToggleFoldHubEdges(e.target.checked)}
                    data-testid="toggle-fold-hub"
                  />
                  <span>自动折叠高扇出 Hub 边 (&gt;8 条)</span>
                </label>
              )}

              {onNeighborhoodHopChange && (
                <div className="code-arch-toolbar__session-group">
                  <span className="code-arch-toolbar__label">邻域追踪:</span>
                  <select
                    value={String(neighborhoodHop)}
                    onChange={(e) => onNeighborhoodHopChange(Number(e.target.value))}
                    className="code-arch-toolbar__select code-arch-toolbar__select--compact"
                    data-testid="topo-hop-select"
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
                    >
                      <option value="BOTH">双向</option>
                      <option value="UPSTREAM">仅上游</option>
                      <option value="DOWNSTREAM">仅下游</option>
                    </select>
                  )}
                </div>
              )}

              {onOwnerIdChange && ownerOptions.length > 0 && (
                <div className="code-arch-toolbar__session-group">
                  <span className="code-arch-toolbar__label">负责人:</span>
                  <select
                    value={ownerId != null ? String(ownerId) : ''}
                    onChange={(e) =>
                      onOwnerIdChange(e.target.value ? Number(e.target.value) : null)
                    }
                    className="code-arch-toolbar__select code-arch-toolbar__select--compact"
                    data-testid="topo-owner-select"
                  >
                    <option value="">全部负责人</option>
                    {ownerOptions.map((o) => (
                      <option key={o.id} value={String(o.id)}>
                        {o.name}
                      </option>
                    ))}
                  </select>
                </div>
              )}
            </div>

            {view === 'LOGICAL' && (onToggleShowRealization || onContractModeChange) && (
              <div className="code-arch-filter-popover__section">
                <span className="code-arch-filter-popover__title">接口与实现图层</span>
                {onToggleShowRealization && (
                  <label className="code-arch-toolbar__checkbox-label">
                    <input
                      type="checkbox"
                      checked={showRealization}
                      onChange={(e) => onToggleShowRealization(e.target.checked)}
                      data-testid="toggle-realization"
                    />
                    <span>◁⋯ 显示接口实现关系</span>
                  </label>
                )}

                {onContractModeChange && (
                  <div className="code-segmented code-segmented--xs" data-testid="contract-mode-toggle">
                    <button
                      type="button"
                      className={`code-segmented-item ${
                        contractMode === 'AGG' ? 'code-segmented-item--active' : ''
                      }`}
                      onClick={() => onContractModeChange('AGG')}
                      data-testid="contract-mode-agg"
                    >
                      聚合到接口
                    </button>
                    <button
                      type="button"
                      className={`code-segmented-item ${
                        contractMode === 'EXPAND' ? 'code-segmented-item--active' : ''
                      }`}
                      onClick={() => onContractModeChange('EXPAND')}
                      data-testid="contract-mode-expand"
                    >
                      展开到实现
                    </button>
                  </div>
                )}
              </div>
            )}
          </div>
        </div>

        {/* 组 4: 布局整理与视图书签 */}
        <div
          className="code-arch-toolbar__group code-arch-toolbar__right"
          data-testid="toolbar-group-layout"
        >
          <button
            type="button"
            className="code-btn code-btn--secondary code-btn--sm code-arch-toolbar__reset-btn"
            onClick={handleRelayoutClick}
            title="清理当前作用域未钉住节点位移并重跑 ELK 正交排版"
            data-testid="reset-topo-layout-btn"
          >
            <Sparkles size={13} />
            <span>自动整理</span>
          </button>

          {onOpenBookmarkDrawer && (
            <button
              type="button"
              className="code-btn code-btn--secondary code-btn--sm code-arch-toolbar__reset-btn"
              onClick={onOpenBookmarkDrawer}
              title="管理个人视图书签与团队共享基准布局"
              data-testid="open-bookmark-drawer-btn"
            >
              <Bookmark size={13} />
              <span>视图书签{bookmarkQuotaText ? ` (${bookmarkQuotaText})` : ''}</span>
              {hasUnsavedDraft && (
                <span className="code-arch-badge code-arch-badge--warning">草稿</span>
              )}
            </button>
          )}
        </div>
      </div>

      {/* 激活态过滤标签栏 (Filter Chips) */}
      {filterChips.length > 0 && (
        <div className="code-arch-filter-chips" data-testid="topo-filter-chips">
          {filterChips.map((chip) => (
            <span key={chip.key} className="code-arch-filter-chip" data-testid={`filter-chip-${chip.key}`}>
              <span>{chip.label}</span>
              <button
                type="button"
                className="code-arch-filter-chip__close"
                onClick={chip.onRemove}
                aria-label={`清除过滤 ${chip.label}`}
                data-testid={`filter-chip-close-${chip.key}`}
              >
                ×
              </button>
            </span>
          ))}
        </div>
      )}

      {/* 独立工具栏渲染场景下（未禁用 renderBreadcrumbBar 且处于聚焦态），输出面包屑栏 */}
      {renderBreadcrumbBar && focusScope && focusScope !== 'GLOBAL' && (
        <TopologyBreadcrumbBar
          focusScope={focusScope}
          breadcrumbs={focusBreadcrumbs}
          onNavigateScope={(scope) => onFocusScopeChange?.(scope)}
        />
      )}
    </div>
  );
};
