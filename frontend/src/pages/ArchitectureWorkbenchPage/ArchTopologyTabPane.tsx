import React, { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Drawer } from '@code/common';
import { useToast } from '../../utils/toast';
import { useModuleUser } from '../../context/ModuleUserContext';
import * as archApi from '../../api/arch';
import * as trafficApi from '../../api/traffic';
import * as agentApi from '../../api/agent';
import type { Agent } from '../../types';
import type {
  TopologyGraphDTO,
  GraphNode,
  GraphEdge,
  RollupEdgeModel,
  ExpandStateMap,
  NeighborhoodDirection,
  SavedNodePosition,
  SavedViewport,
  TopologyLayoutListResponse,
  TopologyLayoutDetailDTO,
} from '../../types/topology_graph';
import type {
  ConsistencyReportDTO,
  RecordingSession,
  PacketAnatomyDrawerDTO,
  ConsistencyEdge,
} from '../../types/traffic';
import {
  estimateVisibleNodeCount,
  VISIBILITY_SOFT_THRESHOLD,
  VISIBILITY_HARD_CAP,
  SEARCH_AUTO_EXPAND_CAP,
  type ScopeAndFilterOptions,
} from '../../components/ArchCanvas/model/subgraph';
import { diffLayoutAgainstGraph } from '../../components/ArchCanvas/layout/layoutMerger';
import {
  resolveInitialLayout,
  restoreExpandStateFromSaved,
  serializeExpandState,
  extractPersistablePositions,
  saveTopologyDraft,
  loadTopologyDraft,
  clearTopologyDraft,
  DRAFT_DEBOUNCE_MS,
} from '../../components/ArchCanvas/layout/layoutPersistence';
import { ArchCanvas } from '../../components/ArchCanvas/ArchCanvas';
import {
  ArchCanvasToolbar,
  type FocusBreadcrumbItem,
  type OwnerFilterOption,
} from '../../components/ArchCanvas/panels/ArchCanvasToolbar';
import { ArchSummaryBar } from '../../components/ArchCanvas/panels/ArchSummaryBar';
import {
  NodeInspectDrawer,
  type NodeInspectTab,
} from '../../components/ArchCanvas/panels/NodeInspectDrawer';
import { computeNodeRelations } from '../../components/ArchCanvas/model/noiseReduction';
import { EdgeDetailDrawer } from '../../components/ArchCanvas/panels/EdgeDetailDrawer';
import { VisibilityGuardDialog } from '../../components/ArchCanvas/panels/VisibilityGuardDialog';
import { TopologyBookmarkDrawer } from '../../components/ArchCanvas/panels/TopologyBookmarkDrawer';
import { PacketAnatomyDrawer } from '../../components/PacketAnatomyDrawer/PacketAnatomyDrawer';
import { HelpCircle, AlertTriangle } from 'lucide-react';

export interface VisibilityGuardEvaluation {
  level: 'OK' | 'SOFT_WARNING' | 'HARD_CAP';
  estimatedCount: number;
  blocked: boolean;
}

/**
 * 统一可见规模守卫评估函数：
 * - <= 45: OK
 * - 46 .. 120: SOFT_WARNING (允许应用但弹出顶部黄色降噪横幅)
 * - > 120: HARD_CAP (拦截状态应用并弹出引导对话框)
 */
export function evaluateVisibilityGuard(
  nodes: GraphNode[],
  edges: GraphEdge[],
  candidateExpandState: ExpandStateMap,
  options: ScopeAndFilterOptions = {}
): VisibilityGuardEvaluation {
  const estimatedCount = estimateVisibleNodeCount(nodes, edges, candidateExpandState, options);
  if (estimatedCount > VISIBILITY_HARD_CAP) {
    return { level: 'HARD_CAP', estimatedCount, blocked: true };
  }
  if (estimatedCount > VISIBILITY_SOFT_THRESHOLD) {
    return { level: 'SOFT_WARNING', estimatedCount, blocked: false };
  }
  return { level: 'OK', estimatedCount, blocked: false };
}

export interface SearchExpandDecision {
  matchedNodeIds: Set<string>;
  shouldAutoExpand: boolean;
  ancestorsToExpand: ExpandStateMap;
  containerMatchCounts: Record<string, number>;
}

/**
 * 搜索命中数守卫评估函数 (SEARCH_AUTO_EXPAND_CAP = 30)：
 * - 命中数 <= 30: 自动沿父链生成待展开容器集合 ancestorsToExpand
 * - 命中数 > 30: 禁止全量展开容器，仅统计各祖先容器内部的命中数供容器角标展示
 */
export function computeSearchExpandDecision(
  nodes: GraphNode[],
  searchKeyword: string
): SearchExpandDecision {
  const kw = searchKeyword.trim().toLowerCase();
  const matchedNodeIds = new Set<string>();
  const ancestorsToExpand: ExpandStateMap = {};
  const containerMatchCounts: Record<string, number> = {};

  if (!kw || nodes.length === 0) {
    return {
      matchedNodeIds,
      shouldAutoExpand: false,
      ancestorsToExpand,
      containerMatchCounts,
    };
  }

  for (const n of nodes) {
    const match =
      n.name.toLowerCase().includes(kw) ||
      Boolean(n.code && n.code.toLowerCase().includes(kw)) ||
      n.id.toLowerCase().includes(kw);
    if (match) {
      matchedNodeIds.add(n.id);
      for (const ancId of n.ancestors) {
        containerMatchCounts[ancId] = (containerMatchCounts[ancId] || 0) + 1;
        ancestorsToExpand[ancId] = true;
      }
    }
  }

  const shouldAutoExpand =
    matchedNodeIds.size > 0 && matchedNodeIds.size <= SEARCH_AUTO_EXPAND_CAP;

  return {
    matchedNodeIds,
    shouldAutoExpand,
    ancestorsToExpand: shouldAutoExpand ? ancestorsToExpand : {},
    containerMatchCounts,
  };
}

export interface ParsedTopoUrlState {
  focusScope: string;
  neighborhoodHop: number;
  neighborhoodDirection: NeighborhoodDirection;
  ownerId: number | null;
  consistencyOnly: boolean;
  hideIsolated: boolean;
  layoutId: number | null;
}

export function parseTopoUrlParams(searchParams: URLSearchParams): ParsedTopoUrlState {
  const focusScope = searchParams.get('topo_focus') || 'GLOBAL';
  const rawHop = Number(searchParams.get('topo_hop') || 0);
  const neighborhoodHop = rawHop === 1 || rawHop === 2 ? rawHop : 0;
  const rawDir = (searchParams.get('topo_dir') || 'BOTH').toUpperCase();
  const neighborhoodDirection: NeighborhoodDirection =
    rawDir === 'UPSTREAM' || rawDir === 'DOWNSTREAM' ? rawDir : 'BOTH';
  const rawOwner = searchParams.get('topo_owner_id');
  const ownerId = rawOwner && !Number.isNaN(Number(rawOwner)) ? Number(rawOwner) : null;
  const consistencyOnly = searchParams.get('topo_consistency_only') === 'true';
  const hideIsolated = searchParams.get('topo_hide_isolated') === 'true';
  const rawLayout = searchParams.get('topo_layout_id');
  const layoutId = rawLayout && !Number.isNaN(Number(rawLayout)) ? Number(rawLayout) : null;

  return {
    focusScope,
    neighborhoodHop,
    neighborhoodDirection,
    ownerId,
    consistencyOnly,
    hideIsolated,
    layoutId,
  };
}

export function buildTopoUrlParams(
  state: Partial<ParsedTopoUrlState>
): Record<string, string | null> {
  const out: Record<string, string | null> = {};
  if (state.focusScope !== undefined) {
    out.topo_focus = state.focusScope && state.focusScope !== 'GLOBAL' ? state.focusScope : null;
  }
  if (state.neighborhoodHop !== undefined) {
    out.topo_hop = state.neighborhoodHop > 0 ? String(state.neighborhoodHop) : null;
  }
  if (state.neighborhoodDirection !== undefined) {
    out.topo_dir = state.neighborhoodDirection !== 'BOTH' ? state.neighborhoodDirection : null;
  }
  if (state.ownerId !== undefined) {
    out.topo_owner_id = state.ownerId != null ? String(state.ownerId) : null;
  }
  if (state.consistencyOnly !== undefined) {
    out.topo_consistency_only = state.consistencyOnly ? 'true' : null;
  }
  if (state.hideIsolated !== undefined) {
    out.topo_hide_isolated = state.hideIsolated ? 'true' : null;
  }
  if (state.layoutId !== undefined) {
    out.topo_layout_id = state.layoutId != null ? String(state.layoutId) : null;
  }
  return out;
}

export interface ArchTopologyTabPaneProps {
  initialView?: 'LOGICAL' | 'PHYSICAL';
  initialSessionId?: string | null;
  onViewChange?: (view: 'LOGICAL' | 'PHYSICAL') => void;
}

export const ArchTopologyTabPane: React.FC<ArchTopologyTabPaneProps> = ({
  initialView = 'LOGICAL',
  initialSessionId = null,
  onViewChange,
}) => {
  const [searchParams, setSearchParams] = useSearchParams();
  const toast = useToast();
  const { user } = useModuleUser();
  const currentUserId = user?.id ?? 'default';
  const isAdmin = Boolean(
    user?.is_super_admin ||
      user?.is_module_admin ||
      user?.roles?.includes('admin') ||
      user?.roles?.includes('traefik_admin')
  );

  // 1. 视角与 URL 同步状态
  const paramView = (searchParams.get('view')?.toUpperCase() as 'LOGICAL' | 'PHYSICAL') || initialView;
  const [view, setView] = useState<'LOGICAL' | 'PHYSICAL'>(
    paramView === 'PHYSICAL' ? 'PHYSICAL' : 'LOGICAL'
  );
  const initialUrlState = parseTopoUrlParams(searchParams);

  const [graphData, setGraphData] = useState<TopologyGraphDTO | null>(null);
  const [loadingGraph, setLoadingGraph] = useState(false);
  const [expandState, setExpandState] = useState<ExpandStateMap>({});
  const [depthLevel, setDepthLevel] = useState<number>(0);
  const [wheelAutoExpand, setWheelAutoExpand] = useState<boolean>(false);
  const [searchKeyword, setSearchKeyword] = useState<string>('');
  const [resetKey, setResetKey] = useState<number>(0);

  // M19 聚焦与降噪筛选状态
  const [focusScope, setFocusScope] = useState<string>(initialUrlState.focusScope);
  const [neighborhoodHop, setNeighborhoodHop] = useState<number>(initialUrlState.neighborhoodHop);
  const [neighborhoodDirection, setNeighborhoodDirection] = useState<NeighborhoodDirection>(
    initialUrlState.neighborhoodDirection
  );
  const [ownerId, setOwnerId] = useState<number | null>(initialUrlState.ownerId);
  const [consistencyOnly, setConsistencyOnly] = useState<boolean>(initialUrlState.consistencyOnly);
  const [hideIsolated, setHideIsolated] = useState<boolean>(initialUrlState.hideIsolated);
  const [foldHubEdges, setFoldHubEdges] = useState<boolean>(true);
  const [expandedHubNodeIds, setExpandedHubNodeIds] = useState<Set<string>>(new Set());

  // M19 混合布局位置记忆与双层持久化状态
  const [savedPositions, setSavedPositions] = useState<Record<string, SavedNodePosition> | null>(null);
  const [pinnedNodeIds, setPinnedNodeIds] = useState<Set<string>>(new Set());
  const [currentViewport, setCurrentViewport] = useState<SavedViewport>({ x: 0, y: 0, zoom: 1 });
  const [restoredViewport, setRestoredViewport] = useState<SavedViewport | null>(null);
  const [layoutList, setLayoutList] = useState<TopologyLayoutListResponse | null>(null);
  const [activeLayoutId, setActiveLayoutId] = useState<number | null>(initialUrlState.layoutId);
  const [hasUnsavedDraft, setHasUnsavedDraft] = useState<boolean>(false);
  const [isBookmarkDrawerOpen, setIsBookmarkDrawerOpen] = useState<boolean>(false);
  const [conflictBannerMessage, setConflictBannerMessage] = useState<string | null>(null);
  const [driftSummaryMessage, setDriftSummaryMessage] = useState<string | null>(null);

  // M19 统一可见规模守卫状态
  const [softGuardCount, setSoftGuardCount] = useState<number | null>(null);
  const [pendingGuardTransition, setPendingGuardTransition] = useState<{
    estimatedCount: number;
    nextExpandState: ExpandStateMap;
    nextFocusScope: string;
    nextDepthLevel?: number;
  } | null>(null);

  // 2. 物理拓扑环境叠加
  const paramEnvAgentId = searchParams.get('env_agent_id');
  const [envAgentId, setEnvAgentId] = useState<string | null>(paramEnvAgentId || null);
  const [agents, setAgents] = useState<Agent[]>([]);

  // 3. 一致性比对录制批次
  const paramSessionId = searchParams.get('session_id') || initialSessionId;
  const [sessions, setSessions] = useState<RecordingSession[]>([]);
  const [selectedSessionId, setSelectedSessionId] = useState<string | null>(paramSessionId || null);
  const [consistencyReport, setConsistencyReport] = useState<ConsistencyReportDTO | null>(null);
  const [loadingReport, setLoadingReport] = useState(false);

  // 4. 抽屉状态
  const [selectedNode, setSelectedNode] = useState<GraphNode | null>(null);
  const [isNodeDrawerOpen, setIsNodeDrawerOpen] = useState(false);
  const [nodeDrawerTab, setNodeDrawerTab] = useState<NodeInspectTab>('RELATIONS');
  const [visibleRollupEdges, setVisibleRollupEdges] = useState<RollupEdgeModel[]>([]);
  const [panToNodeRequest, setPanToNodeRequest] = useState<{ nodeId: string; seq: number } | null>(
    null
  );
  const [selectedEdge, setSelectedEdge] = useState<RollupEdgeModel | null>(null);
  const [isEdgeDrawerOpen, setIsEdgeDrawerOpen] = useState(false);
  const [isPendingDrawerOpen, setIsPendingDrawerOpen] = useState(false);
  const [anatomyData, setAnatomyData] = useState<PacketAnatomyDrawerDTO | null>(null);
  const [isAnatomyOpen, setIsAnatomyOpen] = useState(false);
  const [loadingAnatomy, setLoadingAnatomy] = useState(false);

  const draftTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // 同步 URL 参数助手
  const updateUrlParams = useCallback(
    (params: Record<string, string | null>) => {
      setSearchParams((prev) => {
        const next = new URLSearchParams(prev);
        Object.entries(params).forEach(([key, val]) => {
          if (val === null || val === undefined || val === '') {
            next.delete(key);
          } else {
            next.set(key, val);
          }
        });
        return next;
      });
    },
    [setSearchParams]
  );

  // 监听浏览器 URL 前进/后退/直达变化
  useEffect(() => {
    const pv = (searchParams.get('view')?.toUpperCase() as 'LOGICAL' | 'PHYSICAL') || initialView;
    const nextView = pv === 'PHYSICAL' ? 'PHYSICAL' : 'LOGICAL';
    setView((curr) => (curr !== nextView ? nextView : curr));

    const ps = searchParams.get('session_id') || initialSessionId || null;
    setSelectedSessionId((curr) => (curr !== ps ? ps : curr));

    const pe = searchParams.get('env_agent_id') || null;
    setEnvAgentId((curr) => (curr !== pe ? pe : curr));

    const parsed = parseTopoUrlParams(searchParams);
    setFocusScope((curr) => (curr !== parsed.focusScope ? parsed.focusScope : curr));
    setNeighborhoodHop((curr) => (curr !== parsed.neighborhoodHop ? parsed.neighborhoodHop : curr));
    setNeighborhoodDirection((curr) =>
      curr !== parsed.neighborhoodDirection ? parsed.neighborhoodDirection : curr
    );
    setOwnerId((curr) => (curr !== parsed.ownerId ? parsed.ownerId : curr));
    setConsistencyOnly((curr) => (curr !== parsed.consistencyOnly ? parsed.consistencyOnly : curr));
    setHideIsolated((curr) => (curr !== parsed.hideIsolated ? parsed.hideIsolated : curr));
    setActiveLayoutId((curr) => (curr !== parsed.layoutId ? parsed.layoutId : curr));
  }, [searchParams, initialView, initialSessionId]);

  // 统一可见规模守卫唯一入口 applyVisibilityChange()
  const applyVisibilityChange = useCallback(
    (
      nextExpandState: ExpandStateMap,
      nextFocusScope: string = focusScope,
      options?: { nextDepthLevel?: number; force?: boolean; nextHideIsolated?: boolean }
    ) => {
      const targetHideIsolated = options?.nextHideIsolated ?? hideIsolated;
      if (!graphData || !graphData.nodes) {
        setExpandState(nextExpandState);
        setFocusScope(nextFocusScope);
        if (options?.nextDepthLevel !== undefined) setDepthLevel(options.nextDepthLevel);
        return;
      }

      const guard = evaluateVisibilityGuard(
        graphData.nodes,
        graphData.edges || [],
        nextExpandState,
        {
          focusScope: nextFocusScope,
          seedNodeId: selectedNode?.id ?? null,
          neighborhoodHop,
          neighborhoodDirection,
          ownerId,
          consistencyOnly,
          hideIsolated: targetHideIsolated,
          searchKeyword,
        }
      );

      if (guard.blocked && !options?.force) {
        setPendingGuardTransition({
          estimatedCount: guard.estimatedCount,
          nextExpandState,
          nextFocusScope,
          nextDepthLevel: options?.nextDepthLevel,
        });
        return;
      }

      setPendingGuardTransition(null);
      setSoftGuardCount(guard.level === 'SOFT_WARNING' ? guard.estimatedCount : null);
      setExpandState(nextExpandState);
      setFocusScope(nextFocusScope);
      if (options?.nextDepthLevel !== undefined) {
        setDepthLevel(options.nextDepthLevel);
      }
      if (options?.nextHideIsolated !== undefined) {
        setHideIsolated(options.nextHideIsolated);
      }
      updateUrlParams(
        buildTopoUrlParams({
          focusScope: nextFocusScope,
          hideIsolated: targetHideIsolated,
        })
      );
    },
    [
      focusScope,
      hideIsolated,
      graphData,
      selectedNode,
      neighborhoodHop,
      neighborhoodDirection,
      ownerId,
      consistencyOnly,
      searchKeyword,
      updateUrlParams,
    ]
  );

  // 搜索命中计算与自动展开守卫 (SEARCH_AUTO_EXPAND_CAP = 30)
  const searchDecision = useMemo(
    () => computeSearchExpandDecision(graphData?.nodes ?? [], searchKeyword),
    [graphData, searchKeyword]
  );

  useEffect(() => {
    if (!searchDecision.shouldAutoExpand) return;
    if (Object.keys(searchDecision.ancestorsToExpand).length === 0) return;
    applyVisibilityChange({
      ...expandState,
      ...searchDecision.ancestorsToExpand,
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [searchKeyword, searchDecision.shouldAutoExpand]);

  // 加载全系统复合拓扑图
  const loadGraph = useCallback(
    async (targetView: 'LOGICAL' | 'PHYSICAL', targetEnvAgentId?: string | null) => {
      setLoadingGraph(true);
      try {
        const res = await archApi.getTopologyGraph(targetView, targetEnvAgentId);
        setGraphData(res);
      } catch (err) {
        toast.error(err instanceof Error ? err.message : '加载全系统架构拓扑图失败');
      } finally {
        setLoadingGraph(false);
      }
    },
    [toast]
  );

  useEffect(() => {
    void loadGraph(view, view === 'PHYSICAL' ? envAgentId : null);
  }, [view, envAgentId, loadGraph]);

  // 加载布局书签列表并按 5 级优先级恢复初始布局
  const applyLoadedSnapshot = useCallback(
    (
      snapshot: {
        focus_scope: string;
        node_positions: Record<string, SavedNodePosition>;
        expand_state: { depth_level: number; expanded_node_ids: string[]; collapsed_node_ids?: string[] };
        filter_state: {
          neighborhood_hop: number;
          neighborhood_direction: NeighborhoodDirection;
          owner_id?: number | null;
          consistency_only: boolean;
          hide_isolated: boolean;
          fold_hub_edges: boolean;
        };
        viewport: SavedViewport;
      },
      nodes: GraphNode[],
      edges: GraphEdge[]
    ) => {
      const diff = diffLayoutAgainstGraph(snapshot.node_positions || {}, nodes, edges);
      if (diff.driftCount > 0) {
        setDriftSummaryMessage(
          `拓扑结构演进提示：自上次保存后新增 ${diff.newIds.length} 个节点、迁移 ${diff.reparentedIds.length} 个、清理 ${diff.staleIds.length} 个失效节点`
        );
      } else {
        setDriftSummaryMessage(null);
      }
      setSavedPositions(diff.reconciledPositions);

      const pinned = new Set<string>();
      for (const [nid, pos] of Object.entries(diff.reconciledPositions)) {
        if (pos.pinned) pinned.add(nid);
      }
      setPinnedNodeIds(pinned);

      const restoredExpand = restoreExpandStateFromSaved(snapshot.expand_state, nodes);
      setNeighborhoodHop(snapshot.filter_state?.neighborhood_hop ?? 0);
      setNeighborhoodDirection(snapshot.filter_state?.neighborhood_direction ?? 'BOTH');
      setOwnerId(snapshot.filter_state?.owner_id ?? null);
      setConsistencyOnly(Boolean(snapshot.filter_state?.consistency_only));
      setHideIsolated(Boolean(snapshot.filter_state?.hide_isolated));
      setFoldHubEdges(snapshot.filter_state?.fold_hub_edges !== false);

      if (snapshot.viewport && Number.isFinite(snapshot.viewport.zoom)) {
        setRestoredViewport(snapshot.viewport);
        setCurrentViewport(snapshot.viewport);
      }

      applyVisibilityChange(restoredExpand, snapshot.focus_scope || 'GLOBAL', {
        nextDepthLevel: snapshot.expand_state?.depth_level ?? 0,
      });
    },
    [applyVisibilityChange]
  );

  const loadLayoutsAndResolveInitial = useCallback(
    async (targetView: 'LOGICAL' | 'PHYSICAL', graph: TopologyGraphDTO) => {
      try {
        const res = await archApi.listTopologyLayouts(targetView);
        setLayoutList(res);

        let urlBookmark: TopologyLayoutDetailDTO | null = null;
        if (activeLayoutId != null) {
          try {
            urlBookmark = await archApi.getPersonalTopologyLayout(activeLayoutId);
          } catch {
            urlBookmark = null;
          }
        }

        let defaultBookmark: TopologyLayoutDetailDTO | null = null;
        const defSummary = res.personal_items?.find((it) => it.is_default);
        if (!urlBookmark && defSummary) {
          try {
            defaultBookmark = await archApi.getPersonalTopologyLayout(defSummary.id);
          } catch {
            defaultBookmark = null;
          }
        }

        const draft = loadTopologyDraft(currentUserId, targetView);
        const resolved = resolveInitialLayout({
          urlBookmark,
          draft,
          defaultBookmark,
          canonical: res.canonical,
        });

        setHasUnsavedDraft(resolved.source === 'LOCAL_DRAFT');
        if (resolved.layout) {
          applyLoadedSnapshot(resolved.layout, graph.nodes || [], graph.edges || []);
        }
      } catch {
        // 布局接口非阻断，失败时自动回退冷启动 ELK L0
      }
    },
    [activeLayoutId, currentUserId, applyLoadedSnapshot]
  );

  useEffect(() => {
    if (!graphData || !graphData.nodes) return;
    void loadLayoutsAndResolveInitial(view, graphData);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [graphData, view]);

  // 自动将本地会话偏移防抖写入 localStorage 草稿
  const scheduleSaveDraft = useCallback(
    (nextPositions: Record<string, SavedNodePosition> | null) => {
      if (!graphData || !nextPositions) return;
      if (draftTimerRef.current) clearTimeout(draftTimerRef.current);
      draftTimerRef.current = setTimeout(() => {
        saveTopologyDraft(currentUserId, view, {
          view,
          structure_version: graphData.structure_version || '',
          focus_scope: focusScope,
          node_positions: extractPersistablePositions(nextPositions),
          expand_state: serializeExpandState(expandState, depthLevel),
          filter_state: {
            neighborhood_hop: neighborhoodHop,
            neighborhood_direction: neighborhoodDirection,
            owner_id: ownerId,
            consistency_only: consistencyOnly,
            hide_isolated: hideIsolated,
            fold_hub_edges: foldHubEdges,
          },
          viewport: currentViewport,
          updated_at: Date.now(),
        });
        setHasUnsavedDraft(true);
      }, DRAFT_DEBOUNCE_MS);
    },
    [
      graphData,
      currentUserId,
      view,
      focusScope,
      expandState,
      depthLevel,
      neighborhoodHop,
      neighborhoodDirection,
      ownerId,
      consistencyOnly,
      hideIsolated,
      foldHubEdges,
      currentViewport,
    ]
  );

  // 加载可用 Agent 宿主机列表
  useEffect(() => {
    let mounted = true;
    void (async () => {
      try {
        const res = await agentApi.listAgents({ page: 1, page_size: 100 });
        if (mounted) {
          setAgents(res.items ?? []);
        }
      } catch {
        // 静默处理
      }
    })();
    return () => {
      mounted = false;
    };
  }, []);

  // 加载协同录制批次列表
  useEffect(() => {
    let mounted = true;
    void (async () => {
      try {
        const res = await trafficApi.listRecordingSessions({ page: 1, page_size: 100 });
        if (mounted) {
          setSessions(res.items ?? []);
        }
      } catch {
        // 静默处理
      }
    })();
    return () => {
      mounted = false;
    };
  }, []);

  // 加载比对一致性报告
  useEffect(() => {
    if (!selectedSessionId) {
      setConsistencyReport(null);
      return;
    }
    let mounted = true;
    setLoadingReport(true);
    void (async () => {
      try {
        const res = await trafficApi.getConsistencyReport(selectedSessionId);
        if (mounted) {
          setConsistencyReport(res);
        }
      } catch (err) {
        if (mounted) {
          toast.error(err instanceof Error ? err.message : '加载一致性比对报告失败');
        }
      } finally {
        if (mounted) {
          setLoadingReport(false);
        }
      }
    })();
    return () => {
      mounted = false;
    };
  }, [selectedSessionId, toast]);

  const handleViewChange = useCallback(
    (nextView: 'LOGICAL' | 'PHYSICAL') => {
      setView(nextView);
      setFocusScope('GLOBAL');
      setExpandState({});
      setDepthLevel(0);
      setSavedPositions(null);
      updateUrlParams({ view: nextView, topo_focus: null, topo_layout_id: null });
      if (onViewChange) {
        onViewChange(nextView);
      }
    },
    [onViewChange, updateUrlParams]
  );

  const handleSelectEnvAgentId = useCallback(
    (agentId: string | null) => {
      setEnvAgentId(agentId);
      updateUrlParams({ env_agent_id: agentId });
    },
    [updateUrlParams]
  );

  const handleSelectSession = useCallback(
    (sessionId: string | null) => {
      setSelectedSessionId(sessionId);
      updateUrlParams({ session_id: sessionId });
    },
    [updateUrlParams]
  );

  const maxDepth = useMemo(() => {
    if (!graphData || !graphData.nodes || graphData.nodes.length === 0) return 0;
    return Math.max(0, ...graphData.nodes.map((n) => n.depth));
  }, [graphData]);

  // 层级深度滑杆调整（通过 applyVisibilityChange 统一守卫）
  const handleDepthLevelChange = useCallback(
    (level: number) => {
      if (!graphData) {
        setDepthLevel(level);
        return;
      }

      const nextExpand: ExpandStateMap = {};
      graphData.nodes.forEach((n) => {
        const isContainer =
          n.type === 'SUBSYSTEM' ||
          n.type === 'FUNCTIONAL_GROUP' ||
          n.type === 'PROCESS_GROUP' ||
          n.type === 'PROCESS' ||
          n.type === 'UNDEPLOYED_CONTAINER' ||
          n.type === 'EXTERNAL_CONTAINER';

        if (isContainer) {
          nextExpand[n.id] = n.depth < level;
        }
      });
      applyVisibilityChange(nextExpand, focusScope, { nextDepthLevel: level });
    },
    [graphData, focusScope, applyVisibilityChange]
  );

  // 单节点展开/折叠切换（通过 applyVisibilityChange 统一守卫）
  const handleToggleExpand = useCallback(
    (nodeId: string) => {
      const nextExpand: ExpandStateMap = {
        ...expandState,
        [nodeId]: !expandState[nodeId],
      };
      applyVisibilityChange(nextExpand, focusScope);
    },
    [expandState, focusScope, applyVisibilityChange]
  );

  // 下钻聚焦至指定容器或退出聚焦回 GLOBAL（通过 applyVisibilityChange 统一守卫）
  const handleFocusScopeChange = useCallback(
    (targetScope: string) => {
      if (!graphData) {
        setFocusScope(targetScope);
        return;
      }
      const nextExpand: ExpandStateMap = { ...expandState };
      if (targetScope && targetScope !== 'GLOBAL') {
        const targetNode = graphData.nodes.find((n) => n.id === targetScope);
        if (targetNode) {
          nextExpand[targetNode.id] = true;
          for (const ancId of targetNode.ancestors) {
            nextExpand[ancId] = true;
          }
        }
      }
      applyVisibilityChange(nextExpand, targetScope || 'GLOBAL');
    },
    [graphData, expandState, applyVisibilityChange]
  );

  // 固定/解锁节点位置
  const handleTogglePin = useCallback(
    (nodeId: string) => {
      setPinnedNodeIds((prev) => {
        const next = new Set(prev);
        if (next.has(nodeId)) next.delete(nodeId);
        else next.add(nodeId);
        return next;
      });
      setSavedPositions((prev) => {
        if (!prev || !prev[nodeId]) return prev;
        const next = {
          ...prev,
          [nodeId]: { ...prev[nodeId], pinned: !prev[nodeId].pinned },
        };
        scheduleSaveDraft(next);
        return next;
      });
    },
    [scheduleSaveDraft]
  );

  // 单节点拖拽停止后记录最新相对坐标并写入草稿（同时联动撑大父容器保存尺寸）
  const handleNodePositionChange = useCallback(
    (nodeId: string, pos: { x: number; y: number }) => {
      setSavedPositions((prev) => {
        const base = prev || {};
        const existing = base[nodeId];
        const gn = graphData?.nodes?.find((n) => n.id === nodeId);
        const parentId = existing?.parent_id ?? gn?.parent_id ?? null;
        const childW = existing?.width ?? (gn?.type === 'MODULE' ? 220 : 260);
        const childH = existing?.height ?? (gn?.type === 'MODULE' ? 74 : 80);

        const next: Record<string, SavedNodePosition> = {
          ...base,
          [nodeId]: {
            x: pos.x,
            y: pos.y,
            parent_id: parentId,
            is_expanded: Boolean(expandState[nodeId]),
            pinned: pinnedNodeIds.has(nodeId) || existing?.pinned,
            width: existing?.width,
            height: existing?.height,
          },
        };

        if (parentId && next[parentId]) {
          const parentPos = next[parentId];
          const reqW = pos.x + childW + 16;
          const reqH = pos.y + childH + 16;
          next[parentId] = {
            ...parentPos,
            width: Math.max(parentPos.width ?? 260, reqW),
            height: Math.max(parentPos.height ?? 140, reqH),
          };
        }

        scheduleSaveDraft(next);
        return next;
      });
    },
    [graphData, expandState, pinnedNodeIds, scheduleSaveDraft]
  );

  // 展开高扇出 Hub 节点折叠边
  const handleExpandHub = useCallback((hubNodeId: string) => {
    setExpandedHubNodeIds((prev) => {
      const next = new Set(prev);
      next.add(hubNodeId);
      return next;
    });
  }, []);

  // 重置布局并清除本地草稿
  const handleResetLayout = useCallback(() => {
    clearTopologyDraft(currentUserId, view);
    setHasUnsavedDraft(false);
    setSavedPositions(null);
    setPinnedNodeIds(new Set());
    setExpandedHubNodeIds(new Set());
    setResetKey((k) => k + 1);
    toast.success('已重置手动拖拽位移并恢复最优正交排版');
  }, [currentUserId, view, toast]);

  // 书签与团队基准 CRUD 处理
  const refreshLayoutList = useCallback(async () => {
    try {
      const res = await archApi.listTopologyLayouts(view);
      setLayoutList(res);
    } catch {
      // ignore
    }
  }, [view]);

  const buildCurrentSnapshotPayload = useCallback(() => {
    return {
      structure_version: graphData?.structure_version || '',
      node_positions: extractPersistablePositions(savedPositions || {}),
      expand_state: serializeExpandState(expandState, depthLevel),
      filter_state: {
        neighborhood_hop: neighborhoodHop,
        neighborhood_direction: neighborhoodDirection,
        owner_id: ownerId,
        consistency_only: consistencyOnly,
        hide_isolated: hideIsolated,
        fold_hub_edges: foldHubEdges,
      },
      viewport: currentViewport,
    };
  }, [
    graphData,
    savedPositions,
    expandState,
    depthLevel,
    neighborhoodHop,
    neighborhoodDirection,
    ownerId,
    consistencyOnly,
    hideIsolated,
    foldHubEdges,
    currentViewport,
  ]);

  const handlePublishCanonical = useCallback(async () => {
    setConflictBannerMessage(null);
    try {
      const snap = buildCurrentSnapshotPayload();
      const updated = await archApi.upsertCanonicalTopologyLayout({
        view,
        name: `${view === 'LOGICAL' ? '逻辑架构' : '物理架构'}团队基准视图`,
        expected_revision: layoutList?.canonical?.revision ?? null,
        ...snap,
      });
      clearTopologyDraft(currentUserId, view);
      setHasUnsavedDraft(false);
      await refreshLayoutList();
      toast.success(`已发布团队基准布局 (v${updated.revision})`);
    } catch (err) {
      const msg = err instanceof Error ? err.message : '发布团队基准失败';
      if (msg.includes('409') || msg.includes('版本冲突')) {
        setConflictBannerMessage(`检测到版本冲突 (409)：${msg}`);
      }
      toast.error(msg);
    }
  }, [buildCurrentSnapshotPayload, view, layoutList, currentUserId, refreshLayoutList, toast]);

  const handleResetCanonical = useCallback(async () => {
    try {
      await archApi.resetCanonicalTopologyLayout(view);
      await refreshLayoutList();
      toast.success('已重置团队共享基准视图');
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '重置团队基准失败');
    }
  }, [view, refreshLayoutList, toast]);

  const handleApplyCanonical = useCallback(() => {
    if (!layoutList?.canonical || !graphData) return;
    clearTopologyDraft(currentUserId, view);
    setHasUnsavedDraft(false);
    setActiveLayoutId(null);
    updateUrlParams({ topo_layout_id: null });
    applyLoadedSnapshot(layoutList.canonical, graphData.nodes || [], graphData.edges || []);
    toast.success('已恢复团队共享基准视图');
  }, [layoutList, graphData, currentUserId, view, updateUrlParams, applyLoadedSnapshot, toast]);

  const handleCreatePersonal = useCallback(
    async (data: { name: string; description: string; is_default: boolean }) => {
      try {
        const snap = buildCurrentSnapshotPayload();
        const created = await archApi.createPersonalTopologyLayout({
          view,
          name: data.name,
          description: data.description,
          focus_scope: focusScope,
          is_default: data.is_default,
          ...snap,
        });
        clearTopologyDraft(currentUserId, view);
        setHasUnsavedDraft(false);
        setActiveLayoutId(created.id);
        updateUrlParams({ topo_layout_id: String(created.id) });
        await refreshLayoutList();
        toast.success(`已保存个人视图书签「${created.name}」`);
      } catch (err) {
        toast.error(err instanceof Error ? err.message : '保存个人书签失败');
      }
    },
    [buildCurrentSnapshotPayload, view, focusScope, currentUserId, updateUrlParams, refreshLayoutList, toast]
  );

  const handleApplyPersonal = useCallback(
    async (id: number) => {
      if (!graphData) return;
      try {
        const detail = await archApi.getPersonalTopologyLayout(id);
        clearTopologyDraft(currentUserId, view);
        setHasUnsavedDraft(false);
        setActiveLayoutId(detail.id);
        updateUrlParams({ topo_layout_id: String(detail.id) });
        applyLoadedSnapshot(detail, graphData.nodes || [], graphData.edges || []);
        toast.success(`已加载书签「${detail.name}」`);
      } catch (err) {
        toast.error(err instanceof Error ? err.message : '加载个人书签失败');
      }
    },
    [graphData, currentUserId, view, updateUrlParams, applyLoadedSnapshot, toast]
  );

  const handleUpdatePersonal = useCallback(
    async (
      id: number,
      data: { name?: string; description?: string; is_default?: boolean; updateSnapshot?: boolean }
    ) => {
      setConflictBannerMessage(null);
      try {
        const existingItem = layoutList?.personal_items?.find((it) => it.id === id);
        const snap = data.updateSnapshot ? buildCurrentSnapshotPayload() : undefined;
        await archApi.updatePersonalTopologyLayout(id, {
          name: data.name,
          description: data.description,
          is_default: data.is_default,
          focus_scope: data.updateSnapshot ? focusScope : undefined,
          expected_revision: existingItem?.revision ?? null,
          ...(snap || {}),
        });
        if (data.updateSnapshot) {
          clearTopologyDraft(currentUserId, view);
          setHasUnsavedDraft(false);
        }
        await refreshLayoutList();
        toast.success('已更新个人书签');
      } catch (err) {
        const msg = err instanceof Error ? err.message : '更新个人书签失败';
        if (msg.includes('409') || msg.includes('版本冲突')) {
          setConflictBannerMessage(`检测到版本冲突 (409)：${msg}`);
        }
        toast.error(msg);
      }
    },
    [layoutList, buildCurrentSnapshotPayload, focusScope, currentUserId, view, refreshLayoutList, toast]
  );

  const handleDeletePersonal = useCallback(
    async (id: number) => {
      try {
        await archApi.deletePersonalTopologyLayout(id);
        if (activeLayoutId === id) {
          setActiveLayoutId(null);
          updateUrlParams({ topo_layout_id: null });
        }
        await refreshLayoutList();
        toast.success('已删除个人书签');
      } catch (err) {
        toast.error(err instanceof Error ? err.message : '删除个人书签失败');
      }
    },
    [activeLayoutId, updateUrlParams, refreshLayoutList, toast]
  );

  // 节点与边点击检视：单击仅选中锁定聚光灯（若抽屉已打开则自动跟随切换），通过操作条或双击显式打开抽屉
  const handleSelectNode = useCallback((node: GraphNode | null) => {
    setSelectedNode(node);
    if (!node) {
      setIsNodeDrawerOpen(false);
      setIsEdgeDrawerOpen(false);
    }
  }, []);

  const handleInspectNode = useCallback((node: GraphNode, tab: NodeInspectTab = 'RELATIONS') => {
    setSelectedNode(node);
    setNodeDrawerTab(tab);
    setIsNodeDrawerOpen(true);
    // 互斥关闭右侧其他侧边抽屉，防止 mask={false} 下 520px 的边画像与 480px 的节点画像层叠露边
    setIsEdgeDrawerOpen(false);
    setIsBookmarkDrawerOpen(false);
    setIsPendingDrawerOpen(false);
  }, []);

  const handleJumpToNode = useCallback((peerNode: GraphNode) => {
    setSelectedNode(peerNode);
    setPanToNodeRequest((prev) => ({
      nodeId: peerNode.id,
      seq: (prev?.seq ?? 0) + 1,
    }));
  }, []);

  const selectedNodeRelations = useMemo(
    () => computeNodeRelations(selectedNode?.id, graphData?.nodes ?? [], visibleRollupEdges),
    [selectedNode?.id, graphData?.nodes, visibleRollupEdges]
  );

  const handleSelectEdge = useCallback((edge: RollupEdgeModel) => {
    setSelectedEdge(edge);
    setIsEdgeDrawerOpen(true);
    // 互斥关闭右侧其他侧边抽屉
    setIsNodeDrawerOpen(false);
    setIsBookmarkDrawerOpen(false);
    setIsPendingDrawerOpen(false);
  }, []);

  const handleViewTrafficRecord = useCallback(
    async (recordId: string) => {
      setLoadingAnatomy(true);
      try {
        const anatomy = await trafficApi.getTrafficRecordAnatomy(recordId);
        setAnatomyData(anatomy);
        setIsAnatomyOpen(true);
      } catch (err) {
        toast.error(err instanceof Error ? err.message : '获取流量报文解剖失败');
      } finally {
        setLoadingAnatomy(false);
      }
    },
    [toast]
  );

  // 面包屑链路推导
  const focusBreadcrumbs: FocusBreadcrumbItem[] = useMemo(() => {
    if (!focusScope || focusScope === 'GLOBAL' || !graphData?.nodes) return [];
    const nodeMap = new Map(graphData.nodes.map((n) => [n.id, n]));
    const target = nodeMap.get(focusScope);
    if (!target) return [{ id: 'GLOBAL', name: '全局全景' }];
    const crumbs: FocusBreadcrumbItem[] = [{ id: 'GLOBAL', name: '全局全景' }];
    for (const ancId of target.ancestors) {
      const anc = nodeMap.get(ancId);
      if (anc) crumbs.push({ id: anc.id, name: anc.name });
    }
    crumbs.push({ id: target.id, name: target.name });
    return crumbs;
  }, [focusScope, graphData]);

  // 负责人筛选项推导
  const ownerOptions: OwnerFilterOption[] = useMemo(() => {
    if (!graphData?.nodes) return [];
    const map = new Map<number, string>();
    for (const n of graphData.nodes) {
      if (n.meta?.owner_id != null) {
        const oid = Number(n.meta.owner_id);
        if (Number.isFinite(oid) && oid > 0) {
          const oname = n.meta.owner_name ? String(n.meta.owner_name) : `负责人 #${oid}`;
          map.set(oid, oname);
        }
      }
    }
    return Array.from(map.entries()).map(([id, name]) => ({ id, name }));
  }, [graphData]);

  // 推荐下钻聚焦的最大顶层容器（供可见规模硬上限弹窗使用）
  const recommendedFocusNode = useMemo(() => {
    if (!graphData?.nodes) return null;
    const roots = graphData.nodes.filter((n) => n.depth === 0 && n.type !== 'EXTERNAL_CONTAINER');
    return roots.length > 0 ? { id: roots[0].id, name: roots[0].name } : null;
  }, [graphData]);

  const unattributedPendingList: ConsistencyEdge[] = useMemo(() => {
    if (!consistencyReport || !consistencyReport.edges) return [];
    return consistencyReport.edges.filter(
      (e) => e.status === 'UNATTRIBUTED' || e.category === 'UNATTRIBUTED'
    );
  }, [consistencyReport]);

  const graphStats = useMemo(() => {
    if (!graphData || !graphData.nodes) return null;
    let subsystems = 0;
    let functionalGroups = 0;
    let modules = 0;
    let processGroups = 0;
    let processes = 0;
    let externals = 0;
    let unreg = 0;
    graphData.nodes.forEach((n) => {
      if (n.type === 'SUBSYSTEM') subsystems++;
      else if (n.type === 'FUNCTIONAL_GROUP') functionalGroups++;
      else if (n.type === 'MODULE') modules++;
      else if (n.type === 'PROCESS_GROUP') processGroups++;
      else if (n.type === 'PROCESS') processes++;
      else if (
        n.type === 'EXTERNAL_CONTAINER' ||
        n.type === 'EXTERNAL_SYSTEM' ||
        n.type === 'EXTERNAL_ENDPOINT'
      )
        externals++;
      else if (n.type === 'UNREGISTERED_INTERFACE') unreg++;
    });
    return {
      subsystems,
      functionalGroups,
      modules,
      processGroups,
      processes,
      externals,
      unreg,
      edgeCount: graphData.edges?.length ?? 0,
    };
  }, [graphData]);

  return (
    <div className="code-arch-topology-tab-pane">
      {/* 顶部工具栏 */}
      <ArchCanvasToolbar
        view={view}
        onViewChange={handleViewChange}
        depthLevel={depthLevel}
        maxDepth={maxDepth}
        onDepthLevelChange={handleDepthLevelChange}
        wheelAutoExpand={wheelAutoExpand}
        onToggleWheelAutoExpand={setWheelAutoExpand}
        onResetLayout={handleResetLayout}
        searchKeyword={searchKeyword}
        onSearchChange={setSearchKeyword}
        sessions={sessions}
        selectedSessionId={selectedSessionId}
        onSelectSession={handleSelectSession}
        agents={agents}
        selectedEnvAgentId={envAgentId}
        onSelectEnvAgentId={handleSelectEnvAgentId}
        stats={graphStats}
        focusScope={focusScope}
        focusBreadcrumbs={focusBreadcrumbs}
        onFocusScopeChange={handleFocusScopeChange}
        neighborhoodHop={neighborhoodHop}
        onNeighborhoodHopChange={(hop) => {
          setNeighborhoodHop(hop);
          updateUrlParams(buildTopoUrlParams({ neighborhoodHop: hop }));
        }}
        neighborhoodDirection={neighborhoodDirection}
        onNeighborhoodDirectionChange={(dir) => {
          setNeighborhoodDirection(dir);
          updateUrlParams(buildTopoUrlParams({ neighborhoodDirection: dir }));
        }}
        ownerOptions={ownerOptions}
        ownerId={ownerId}
        onOwnerIdChange={(oid) => {
          setOwnerId(oid);
          updateUrlParams(buildTopoUrlParams({ ownerId: oid }));
        }}
        consistencyOnly={consistencyOnly}
        onToggleConsistencyOnly={(enabled) => {
          setConsistencyOnly(enabled);
          updateUrlParams(buildTopoUrlParams({ consistencyOnly: enabled }));
        }}
        hideIsolated={hideIsolated}
        onToggleHideIsolated={(enabled) => {
          setHideIsolated(enabled);
          updateUrlParams(buildTopoUrlParams({ hideIsolated: enabled }));
        }}
        foldHubEdges={foldHubEdges}
        onToggleFoldHubEdges={setFoldHubEdges}
        hasUnsavedDraft={hasUnsavedDraft}
        bookmarkQuotaText={
          layoutList?.quota ? `${layoutList.quota.used}/${layoutList.quota.limit}` : undefined
        }
        onOpenBookmarkDrawer={() => {
          setIsBookmarkDrawerOpen(true);
          setIsNodeDrawerOpen(false);
          setIsEdgeDrawerOpen(false);
          setIsPendingDrawerOpen(false);
        }}
      />

      {/* 软阈值 (46..120) 黄色降噪建议横幅 */}
      {softGuardCount != null && (
        <div className="code-arch-guard-banner" data-testid="soft-visibility-guard-banner">
          <AlertTriangle size={14} />
          <span>
            当前同屏可见节点已达 <strong>{softGuardCount}</strong> 个（建议阈值 ≤{' '}
            {VISIBILITY_SOFT_THRESHOLD}），可点击容器标题栏聚焦子系统或开启「隐藏孤立」提升可读性。
          </span>
          {!hideIsolated && (
            <button
              type="button"
              className="code-btn code-btn--secondary code-btn--xs"
              onClick={() => {
                setHideIsolated(true);
                updateUrlParams(buildTopoUrlParams({ hideIsolated: true }));
              }}
            >
              一键隐藏孤立
            </button>
          )}
        </div>
      )}

      {/* 搜索命中超过 30 个时的不自动全展提示横幅 */}
      {searchKeyword.trim() && searchDecision.matchedNodeIds.size > SEARCH_AUTO_EXPAND_CAP && (
        <div className="code-arch-guard-banner" data-testid="search-cap-guard-banner">
          <AlertTriangle size={14} />
          <span>
            搜索「{searchKeyword}」共命中 <strong>{searchDecision.matchedNodeIds.size}</strong>{' '}
            个节点（超过自动展开上限 {SEARCH_AUTO_EXPAND_CAP}
            ），已为您在各顶层容器标题栏高亮命中角标，请按需双击或聚焦目标容器下钻查看。
          </span>
        </div>
      )}

      {/* 一致性报告摘要栏 */}
      {loadingReport && (
        <div className="code-arch-loading-strip">比对一致性计算中...</div>
      )}
      {consistencyReport && !loadingReport && (
        <ArchSummaryBar
          report={consistencyReport}
          unattributedPendingList={unattributedPendingList}
          onOpenPendingList={() => {
            setIsPendingDrawerOpen(true);
            setIsNodeDrawerOpen(false);
            setIsEdgeDrawerOpen(false);
            setIsBookmarkDrawerOpen(false);
          }}
        />
      )}

      {/* 画布主体 */}
      <div className="code-arch-canvas-stage">
        {loadingGraph && (
          <div className="code-arch-loading-overlay">
            <div className="code-arch-loading-overlay__text">
              正在推导架构拓扑与层级关系...
            </div>
          </div>
        )}

        <ArchCanvas
          nodes={graphData?.nodes ?? []}
          edges={graphData?.edges ?? []}
          expandState={expandState}
          onToggleExpand={handleToggleExpand}
          onSelectNode={handleSelectNode}
          onInspectNode={handleInspectNode}
          onSelectEdge={handleSelectEdge}
          selectedNodeId={selectedNode?.id ?? null}
          consistencyReport={consistencyReport}
          view={view}
          resetKey={resetKey}
          searchKeyword={searchKeyword}
          wheelAutoExpand={wheelAutoExpand}
          depthLevel={depthLevel}
          maxDepth={maxDepth}
          onDepthLevelChange={handleDepthLevelChange}
          focusScope={focusScope}
          seedNodeId={selectedNode?.id ?? null}
          neighborhoodHop={neighborhoodHop}
          neighborhoodDirection={neighborhoodDirection}
          onNeighborhoodHopChange={(hop) => {
            setNeighborhoodHop(hop);
            updateUrlParams(buildTopoUrlParams({ neighborhoodHop: hop }));
          }}
          onNeighborhoodDirectionChange={(dir) => {
            setNeighborhoodDirection(dir);
            updateUrlParams(buildTopoUrlParams({ neighborhoodDirection: dir }));
          }}
          ownerId={ownerId}
          consistencyOnly={consistencyOnly}
          hideIsolated={hideIsolated}
          foldHubEdgesEnabled={foldHubEdges}
          expandedHubNodeIds={expandedHubNodeIds}
          onExpandHub={handleExpandHub}
          savedPositions={savedPositions}
          pinnedNodeIds={pinnedNodeIds}
          onFocusScope={handleFocusScopeChange}
          onTogglePin={handleTogglePin}
          onNodePositionChange={handleNodePositionChange}
          onPositionsComputed={(computed) => {
            setSavedPositions((prev) => (prev ? { ...prev, ...computed } : computed));
          }}
          initialViewport={restoredViewport}
          onViewportChange={setCurrentViewport}
          containerSearchMatchCounts={searchDecision.containerMatchCounts}
          onVisibleRollupEdgesChange={setVisibleRollupEdges}
          panToNodeRequest={panToNodeRequest}
          className="code-arch-canvas--full"
        />
      </div>

      {/* 可见规模硬上限 (>120) 拦截引导对话框 */}
      <VisibilityGuardDialog
        open={Boolean(pendingGuardTransition)}
        estimatedCount={pendingGuardTransition?.estimatedCount ?? 0}
        hardCap={VISIBILITY_HARD_CAP}
        recommendedFocusNode={recommendedFocusNode}
        onFocusRecommended={(nodeId) => {
          setPendingGuardTransition(null);
          handleFocusScopeChange(nodeId);
        }}
        onEnableHideIsolated={() => {
          if (!pendingGuardTransition) return;
          applyVisibilityChange(
            pendingGuardTransition.nextExpandState,
            pendingGuardTransition.nextFocusScope,
            {
              nextDepthLevel: pendingGuardTransition.nextDepthLevel,
              nextHideIsolated: true,
              force: true,
            }
          );
        }}
        onForceApply={() => {
          if (!pendingGuardTransition) return;
          applyVisibilityChange(
            pendingGuardTransition.nextExpandState,
            pendingGuardTransition.nextFocusScope,
            {
              nextDepthLevel: pendingGuardTransition.nextDepthLevel,
              force: true,
            }
          );
        }}
        onCancel={() => setPendingGuardTransition(null)}
      />

      {/* 个人视图书签与团队基准抽屉 */}
      <TopologyBookmarkDrawer
        open={isBookmarkDrawerOpen}
        onClose={() => setIsBookmarkDrawerOpen(false)}
        view={view}
        isAdmin={isAdmin}
        layoutList={layoutList}
        activeLayoutId={activeLayoutId}
        conflictBannerMessage={conflictBannerMessage}
        driftSummaryMessage={driftSummaryMessage}
        onPublishCanonical={handlePublishCanonical}
        onResetCanonical={handleResetCanonical}
        onApplyCanonical={handleApplyCanonical}
        onCreatePersonal={handleCreatePersonal}
        onApplyPersonal={handleApplyPersonal}
        onUpdatePersonal={handleUpdatePersonal}
        onDeletePersonal={handleDeletePersonal}
      />

      {/* 节点画像抽屉 */}
      <NodeInspectDrawer
        open={isNodeDrawerOpen}
        onClose={() => setIsNodeDrawerOpen(false)}
        node={selectedNode}
        activeTab={nodeDrawerTab}
        onTabChange={setNodeDrawerTab}
        relations={selectedNodeRelations}
        onJumpToNode={handleJumpToNode}
        onInspectEdge={handleSelectEdge}
        onFocusScope={handleFocusScopeChange}
      />

      {/* 边画像抽屉 */}
      <EdgeDetailDrawer
        open={isEdgeDrawerOpen}
        onClose={() => setIsEdgeDrawerOpen(false)}
        edge={selectedEdge}
        onViewTrafficRecord={handleViewTrafficRecord}
      />

      {/* 采样报文四段解剖抽屉 */}
      <PacketAnatomyDrawer
        open={isAnatomyOpen}
        onClose={() => setIsAnatomyOpen(false)}
        data={anatomyData}
        loading={loadingAnatomy}
      />

      {/* 待定归属待办抽屉 */}
      <Drawer
        open={isPendingDrawerOpen}
        onClose={() => setIsPendingDrawerOpen(false)}
        title={`待定归属流量待办清单 (${unattributedPendingList.length} 条)`}
        width={560}
      >
        <div className="code-arch-pending-drawer">
          <div className="code-arch-pending-drawer__notice">
            <HelpCircle size={16} className="code-arch-pending-drawer__notice-icon" />
            <span>
              因目标物理进程承载多个逻辑模块且缺失 TraceID 扩展头，导致无法唯一判定业务调用发起方或接收方。
            </span>
          </div>

          <table className="code-table">
            <thead>
              <tr>
                <th>序号</th>
                <th>调用链路 (源 → 宿)</th>
                <th>接口/服务</th>
                <th>实测次数</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {unattributedPendingList.length === 0 ? (
                <tr>
                  <td colSpan={5} className="code-arch-pending-drawer__empty">
                    无待定归属项
                  </td>
                </tr>
              ) : (
                unattributedPendingList.map((edge, idx) => {
                  const srcLabel =
                    edge.src_module_name ||
                    edge.source_module_name ||
                    (edge.src_process_id ? `进程 proc_${edge.src_process_id}` : '未归属发起方');
                  const dstLabel =
                    edge.dst_module_name ||
                    edge.target_module_name ||
                    edge.service_name ||
                    '未登记目标';
                  const recId = edge.sample_traffic_record_id;

                  return (
                    <tr
                      key={`${edge.src_module_id ?? edge.src_process_id ?? 'u'}-${
                        edge.dst_module_id ?? edge.service_name
                      }-${idx}`}
                    >
                      <td>{idx + 1}</td>
                      <td>
                        <div className="code-arch-pending-drawer__chain">
                          <div>{srcLabel}</div>
                          <div className="code-arch-pending-drawer__chain-sub">→ {dstLabel}</div>
                        </div>
                      </td>
                      <td>
                        <code>{edge.service_name || '-'}</code>
                      </td>
                      <td>{edge.call_count ?? edge.observed_call_count ?? 0}</td>
                      <td>
                        {recId && (
                          <button
                            type="button"
                            className="code-btn code-btn--secondary code-btn--xs"
                            onClick={() => {
                              void handleViewTrafficRecord(String(recId));
                            }}
                          >
                            查看解剖
                          </button>
                        )}
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
      </Drawer>
    </div>
  );
};

export default ArchTopologyTabPane;
