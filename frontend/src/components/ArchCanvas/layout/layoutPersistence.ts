import {
  parseScopedPositionKey,
  type GraphNode,
  type ExpandStateMap,
  type SavedNodePosition,
  type SavedExpandState,
  type SavedFilterState,
  type SavedViewport,
  type TopologyLayoutDetailDTO,
  type TopologyStatusFilter,
} from '../../../types/topology_graph';

export const DRAFT_KEY_PREFIX = 'traefik_topo_draft_v2_';
export const DRAFT_DEBOUNCE_MS = 400;

export interface TopologyLocalDraft {
  view: 'LOGICAL' | 'PHYSICAL';
  structure_version: string;
  focus_scope: string;
  node_positions: Record<string, SavedNodePosition>;
  expand_state: SavedExpandState;
  filter_state: SavedFilterState;
  viewport: SavedViewport;
  updated_at: number;
}

export type InitialLayoutSource =
  | 'URL_BOOKMARK'
  | 'LOCAL_DRAFT'
  | 'PERSONAL_DEFAULT'
  | 'CANONICAL'
  | 'ELK_DEFAULT';

export interface ResolveInitialLayoutInput {
  urlBookmark?: TopologyLayoutDetailDTO | null;
  draft?: TopologyLocalDraft | null;
  defaultBookmark?: TopologyLayoutDetailDTO | null;
  canonical?: TopologyLayoutDetailDTO | null;
}

export interface ResolveInitialLayoutResult {
  source: InitialLayoutSource;
  layout: TopologyLayoutDetailDTO | TopologyLocalDraft | null;
}

/**
 * 5 级初始布局加载优先级链 (ADR-15 §5.3):
 * 1. URL 显式指定 (topo_layout_id) -> URL_BOOKMARK
 * 2. localStorage 本地未保存草稿 -> LOCAL_DRAFT
 * 3. 当前用户默认个人书签 (is_default = true) -> PERSONAL_DEFAULT
 * 4. 团队共享基准布局 (scope = SHARED) -> CANONICAL
 * 5. 冷启动默认 ELK L0 折叠视图 -> ELK_DEFAULT
 */
export function resolveInitialLayout(
  input: ResolveInitialLayoutInput
): ResolveInitialLayoutResult {
  if (input.urlBookmark) {
    return { source: 'URL_BOOKMARK', layout: input.urlBookmark };
  }
  if (input.draft) {
    return { source: 'LOCAL_DRAFT', layout: input.draft };
  }
  if (input.defaultBookmark) {
    return { source: 'PERSONAL_DEFAULT', layout: input.defaultBookmark };
  }
  if (input.canonical) {
    return { source: 'CANONICAL', layout: input.canonical };
  }
  return { source: 'ELK_DEFAULT', layout: null };
}

/**
 * 合成最终生效坐标：
 * effectivePositions = { ...canonicalPositions, ...bookmarkOverrides, ...sessionOverrides }
 */
export function composeEffectivePositions(
  canonicalPositions?: Record<string, SavedNodePosition> | null,
  bookmarkOverrides?: Record<string, SavedNodePosition> | null,
  sessionOverrides?: Record<string, SavedNodePosition> | null
): Record<string, SavedNodePosition> {
  return {
    ...(canonicalPositions || {}),
    ...(bookmarkOverrides || {}),
    ...(sessionOverrides || {}),
  };
}

/**
 * 计算当前坐标表相对于基准坐标表的稀疏覆盖差集 (Overrides)
 */
export function diffOverrides(
  basePositions: Record<string, SavedNodePosition> | null | undefined,
  currentPositions: Record<string, SavedNodePosition>
): Record<string, SavedNodePosition> {
  if (!basePositions) {
    return extractPersistablePositions(currentPositions);
  }
  const cleanedCurrent = extractPersistablePositions(currentPositions);
  const diff: Record<string, SavedNodePosition> = {};

  for (const [id, curr] of Object.entries(cleanedCurrent)) {
    const base = basePositions[id];
    if (
      !base ||
      Math.abs(base.x - curr.x) > 0.5 ||
      Math.abs(base.y - curr.y) > 0.5 ||
      Boolean(base.is_expanded) !== Boolean(curr.is_expanded) ||
      Boolean(base.pinned) !== Boolean(curr.pinned)
    ) {
      diff[id] = curr;
    }
  }

  return diff;
}

function isVolatileVirtualNodeId(rawId: string): boolean {
  return (
    rawId === 'VIRTUAL_UNREGISTERED_INTERFACE' ||
    rawId === 'UNATTRIBUTED_ROOT' ||
    rawId.startsWith('VIRTUAL_IMPL_UNRESOLVED') ||
    rawId.startsWith('dyn_viol_') ||
    rawId.startsWith('dyn_unattr_') ||
    rawId.startsWith('dyn_impl_unres_')
  );
}

/**
 * 判定裸节点 ID 是否为运行期动态合成或外部上下文临时节点（无作用域前缀时剥离）
 */
export function isEphemeralNodeId(nodeId: string): boolean {
  const { scope, rawId } = parseScopedPositionKey(nodeId);
  if (isVolatileVirtualNodeId(rawId)) {
    return true;
  }
  if (rawId.startsWith('ext_ctx_')) {
    return scope === null;
  }
  return false;
}

export interface ExtractPositionsOptions {
  validNodeIds?: ReadonlySet<string>;
  onlyPinned?: boolean;
}

/**
 * 剥离动态告警虚拟节点与裸外部上下文摘要卡，提取可持久化坐标字典：
 * - 允许 `${focusScope}::${rawId}` 及 `${focusScope}::ext_ctx_*` 作用域隔离复合键持久化；
 * - 若传入 validNodeIds，校验静态节点存在于当前架构图（带作用域的 ext_ctx_* 豁免）；
 * - 若传入 onlyPinned，则仅保留显式锁定 (pinned === true) 的坐标。
 */
export function extractPersistablePositions(
  positions: Record<string, SavedNodePosition>,
  options?: ExtractPositionsOptions
): Record<string, SavedNodePosition> {
  const validNodeIds = options?.validNodeIds;
  const onlyPinned = Boolean(options?.onlyPinned);
  const cleaned: Record<string, SavedNodePosition> = {};
  for (const [id, pos] of Object.entries(positions)) {
    if (isEphemeralNodeId(id)) continue;
    if (!Number.isFinite(pos.x) || !Number.isFinite(pos.y)) continue;
    if (onlyPinned && !pos.pinned) continue;
    if (validNodeIds) {
      const { scope, rawId } = parseScopedPositionKey(id);
      const isScopedExtCtx = Boolean(scope && rawId.startsWith('ext_ctx_'));
      if (!isScopedExtCtx && !validNodeIds.has(rawId)) {
        continue;
      }
    }
    cleaned[id] = { ...pos };
  }
  return cleaned;
}

/**
 * 从保存的 SavedExpandState 精确恢复展开字典（兼容 M21 level/overrides 与历史 depth_level/expanded_node_ids）
 */
export function restoreExpandStateFromSaved(
  saved: SavedExpandState | null | undefined,
  nodes: GraphNode[]
): ExpandStateMap {
  if (!saved) return {};
  const nodeIdSet = new Set(nodes.map((n) => n.id));
  const collapsedSet = new Set(saved.collapsed_node_ids || []);
  const expandState: ExpandStateMap = {};

  if (saved.overrides) {
    for (const [id, val] of Object.entries(saved.overrides)) {
      if (nodeIdSet.has(id) && val) {
        expandState[id] = true;
      }
    }
  }

  for (const id of saved.expanded_node_ids || []) {
    if (nodeIdSet.has(id) && !collapsedSet.has(id)) {
      expandState[id] = true;
    }
  }
  return expandState;
}

/**
 * 将当前运行期 ExpandStateMap 序列化为前后端兼容的持久化结构
 */
export function serializeExpandState(
  expandState: ExpandStateMap,
  depthLevel: number
): SavedExpandState {
  const expandedIds: string[] = [];
  const collapsedIds: string[] = [];
  const overrides: Record<string, boolean> = {};
  for (const [id, val] of Object.entries(expandState)) {
    if (isEphemeralNodeId(id)) continue;
    overrides[id] = Boolean(val);
    if (val) expandedIds.push(id);
    else collapsedIds.push(id);
  }
  expandedIds.sort();
  collapsedIds.sort();
  return {
    level: depthLevel,
    overrides,
    depth_level: depthLevel,
    expanded_node_ids: expandedIds,
    collapsed_node_ids: collapsedIds,
  };
}

/**
 * M21-P3 前后端 SavedFilterState 双向归一化：
 * 同时识别后端 Go 字段 (subsystem_ids, only_my_scope, status, ego_node_id, ego_hops, hide_isolated, hub_fold)
 * 与旧版书签字段 (neighborhood_hop, neighborhood_direction, owner_id, consistency_only, fold_hub_edges)
 */
export function normalizeSavedFilterState(
  raw?: Partial<SavedFilterState> | null
): {
  subsystemIds: string[];
  onlyMyScope: boolean;
  statusFilter: TopologyStatusFilter;
  egoNodeId?: string;
  neighborhoodHop: number;
  neighborhoodDirection: 'BOTH' | 'UPSTREAM' | 'DOWNSTREAM';
  consistencyOnly: boolean;
  hideIsolated: boolean;
  foldHubEdges: boolean;
} {
  const hop = raw?.ego_hops ?? raw?.neighborhood_hop ?? 0;
  const consistencyOnly = Boolean(
    raw?.consistency_only || raw?.status === 'ABNORMAL' || raw?.status === 'VIOLATION_ONLY'
  );
  const statusFilter: TopologyStatusFilter =
    raw?.status ?? (raw?.consistency_only ? 'ABNORMAL' : 'ALL');

  return {
    subsystemIds: Array.isArray(raw?.subsystem_ids) ? raw!.subsystem_ids : [],
    onlyMyScope: Boolean(raw?.only_my_scope || raw?.owner_id != null),
    statusFilter,
    egoNodeId: raw?.ego_node_id,
    neighborhoodHop: hop === 1 || hop === 2 ? hop : 0,
    neighborhoodDirection: raw?.neighborhood_direction ?? 'BOTH',
    consistencyOnly,
    hideIsolated: Boolean(raw?.hide_isolated),
    foldHubEdges: raw?.hub_fold ?? raw?.fold_hub_edges ?? true,
  };
}

/**
 * 序列化为严格符合 Go 后端 models.SavedFilterState 校验规则的 JSON 对象
 */
export function serializeBackendFilterState(input: {
  subsystemIds?: string[];
  onlyMyScope?: boolean;
  statusFilter?: TopologyStatusFilter;
  egoNodeId?: string | null;
  neighborhoodHop?: number;
  neighborhoodDirection?: 'BOTH' | 'UPSTREAM' | 'DOWNSTREAM';
  ownerId?: number | null;
  hideIsolated?: boolean;
  foldHubEdges?: boolean;
}): SavedFilterState {
  const status: TopologyStatusFilter = input.statusFilter ?? 'ALL';
  const hop = input.neighborhoodHop === 1 || input.neighborhoodHop === 2 ? input.neighborhoodHop : 0;
  return {
    subsystem_ids: input.subsystemIds && input.subsystemIds.length > 0 ? input.subsystemIds : undefined,
    only_my_scope: Boolean(input.onlyMyScope || input.ownerId != null),
    status,
    ego_node_id: input.egoNodeId || undefined,
    ego_hops: hop,
    hide_isolated: Boolean(input.hideIsolated),
    hub_fold: input.foldHubEdges ?? true,
    // 保留前端可选兼容字段
    neighborhood_hop: hop,
    neighborhood_direction: input.neighborhoodDirection ?? 'BOTH',
    owner_id: input.ownerId ?? null,
    consistency_only: status !== 'ALL',
    fold_hub_edges: input.foldHubEdges ?? true,
  };
}

const memoryDraftStore = new Map<string, string>();

function getStorageBackend(): {
  getItem: (key: string) => string | null;
  setItem: (key: string, val: string) => void;
  removeItem: (key: string) => void;
  keys: () => string[];
} {
  try {
    if (
      typeof window !== 'undefined' &&
      window.localStorage &&
      typeof window.localStorage.getItem === 'function' &&
      typeof window.localStorage.setItem === 'function'
    ) {
      const ls = window.localStorage;
      return {
        getItem: (key) => ls.getItem(key),
        setItem: (key, val) => ls.setItem(key, val),
        removeItem: (key) => ls.removeItem(key),
        keys: () => {
          const out: string[] = [];
          for (let i = 0; i < ls.length; i++) {
            const k = ls.key(i);
            if (k) out.push(k);
          }
          return out;
        },
      };
    }
  } catch {
    // fallback to memoryDraftStore
  }
  return {
    getItem: (key) => memoryDraftStore.get(key) ?? null,
    setItem: (key, val) => {
      memoryDraftStore.set(key, val);
    },
    removeItem: (key) => {
      memoryDraftStore.delete(key);
    },
    keys: () => Array.from(memoryDraftStore.keys()),
  };
}

/**
 * 构造按 userId 与 view 隔离的本地草稿键名
 */
export function getDraftStorageKey(
  userId: number | string,
  view: 'LOGICAL' | 'PHYSICAL'
): string {
  return `${DRAFT_KEY_PREFIX}${userId}_${view}`;
}

export function saveTopologyDraft(
  userId: number | string | null | undefined,
  view: 'LOGICAL' | 'PHYSICAL',
  draft: TopologyLocalDraft
): void {
  if (userId == null) return;
  try {
    const cleanedDraft: TopologyLocalDraft = {
      ...draft,
      node_positions: extractPersistablePositions(draft.node_positions),
    };
    getStorageBackend().setItem(getDraftStorageKey(userId, view), JSON.stringify(cleanedDraft));
  } catch {
    // 忽略浏览器配额异常
  }
}

export function loadTopologyDraft(
  userId: number | string | null | undefined,
  view: 'LOGICAL' | 'PHYSICAL'
): TopologyLocalDraft | null {
  if (userId == null) return null;
  try {
    const raw = getStorageBackend().getItem(getDraftStorageKey(userId, view));
    if (!raw) return null;
    const parsed = JSON.parse(raw) as TopologyLocalDraft;
    if (!parsed || typeof parsed !== 'object' || parsed.view !== view) {
      return null;
    }
    return parsed;
  } catch {
    return null;
  }
}

export function clearTopologyDraft(
  userId: number | string | null | undefined,
  view: 'LOGICAL' | 'PHYSICAL'
): void {
  if (userId == null) return;
  try {
    getStorageBackend().removeItem(getDraftStorageKey(userId, view));
  } catch {
    // ignore
  }
}

export function clearAllTopologyDrafts(): void {
  try {
    const store = getStorageBackend();
    for (const k of store.keys()) {
      if (k.startsWith(DRAFT_KEY_PREFIX)) {
        store.removeItem(k);
      }
    }
  } catch {
    // ignore
  }
}
