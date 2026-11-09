import type {
  GraphNode,
  ExpandStateMap,
  SavedNodePosition,
  SavedExpandState,
  SavedFilterState,
  SavedViewport,
  TopologyLayoutDetailDTO,
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

/**
 * 判定节点 ID 是否为运行期动态合成或外部上下文临时节点（严禁写入持久化）
 */
export function isEphemeralNodeId(nodeId: string): boolean {
  return (
    nodeId === 'VIRTUAL_UNREGISTERED_INTERFACE' ||
    nodeId === 'UNATTRIBUTED_ROOT' ||
    nodeId.startsWith('ext_ctx_') ||
    nodeId.startsWith('dyn_viol_') ||
    nodeId.startsWith('dyn_unattr_')
  );
}

/**
 * 剥离动态告警虚拟节点与外部上下文摘要卡，提取纯静态可持久化坐标字典
 */
export function extractPersistablePositions(
  positions: Record<string, SavedNodePosition>
): Record<string, SavedNodePosition> {
  const cleaned: Record<string, SavedNodePosition> = {};
  for (const [id, pos] of Object.entries(positions)) {
    if (isEphemeralNodeId(id)) continue;
    if (!Number.isFinite(pos.x) || !Number.isFinite(pos.y)) continue;
    cleaned[id] = { ...pos };
  }
  return cleaned;
}

/**
 * 从保存的 SavedExpandState 精确恢复展开字典（仅触碰保存的可见展开集，严禁全量展开）
 */
export function restoreExpandStateFromSaved(
  saved: SavedExpandState | null | undefined,
  nodes: GraphNode[]
): ExpandStateMap {
  if (!saved) return {};
  const nodeIdSet = new Set(nodes.map((n) => n.id));
  const collapsedSet = new Set(saved.collapsed_node_ids || []);
  const expandState: ExpandStateMap = {};

  for (const id of saved.expanded_node_ids || []) {
    if (nodeIdSet.has(id) && !collapsedSet.has(id)) {
      expandState[id] = true;
    }
  }
  return expandState;
}

/**
 * 将当前运行期 ExpandStateMap 序列化为可持久化结构
 */
export function serializeExpandState(
  expandState: ExpandStateMap,
  depthLevel: number
): SavedExpandState {
  const expandedIds: string[] = [];
  const collapsedIds: string[] = [];
  for (const [id, val] of Object.entries(expandState)) {
    if (isEphemeralNodeId(id)) continue;
    if (val) expandedIds.push(id);
    else collapsedIds.push(id);
  }
  expandedIds.sort();
  collapsedIds.sort();
  return {
    depth_level: depthLevel,
    expanded_node_ids: expandedIds,
    collapsed_node_ids: collapsedIds,
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
