import React, { useCallback, useEffect, useMemo, useState } from 'react';
import ELK from 'elkjs/lib/elk.bundled.js';
import {
  Background,
  BackgroundVariant,
  Controls,
  Handle,
  MarkerType,
  Position,
  ReactFlow,
  type Edge,
  type Node,
  type NodeProps,
} from '@xyflow/react';
import { RefreshCw } from 'lucide-react';
import * as schemaApi from '../../api/schema';
import { useToast } from '../../utils/toast';
import type { ProtoCategory, ProtoDependenciesGraphDTO, ProtoDependencyNodeDTO } from '../../types';

export interface ProtoGraphNodeData extends Record<string, unknown> {
  schemaNode: ProtoDependencyNodeDTO;
}

export type ProtoSchemaFlowNode = Node<ProtoGraphNodeData, 'protoSchemaNode'>;
export type ProtoSchemaFlowEdge = Edge;

export type ProtoCategoryFilter = 'ALL' | ProtoCategory;

export interface ProtoDependenciesGraphPaneProps {
  initialGraph?: ProtoDependenciesGraphDTO;
  onNavigateToSchemaAsset: (schemaId: string | number) => void;
}

const NODE_WIDTH = 240;
const NODE_HEIGHT = 104;

const elkInstance = new ELK();

const CATEGORY_META: Record<
  ProtoCategory,
  { label: string; icon: string; badgeBg: string; badgeColor: string; borderColor: string }
> = {
  BASE_LIB: {
    label: '基础公共库',
    icon: '🧱',
    badgeBg: 'var(--color-bg-muted)',
    badgeColor: 'var(--color-info, var(--color-primary))',
    borderColor: 'var(--color-info, var(--color-primary))',
  },
  BUSINESS: {
    label: '业务域服务',
    icon: '📦',
    badgeBg: 'var(--color-bg-muted)',
    badgeColor: 'var(--color-primary)',
    borderColor: 'var(--color-primary)',
  },
  GATEWAY: {
    label: '网关聚合层',
    icon: '🌐',
    badgeBg: 'var(--color-bg-muted)',
    badgeColor: 'var(--color-success)',
    borderColor: 'var(--color-success)',
  },
};

const SOURCE_TYPE_LABEL: Record<string, string> = {
  UPLOAD_PB: '二进制 .pb',
  ONLINE_COMPILE: '源码直编',
  GIT_SYNC: 'Git 同步',
  GIT_MR_AUTO: 'Git MR 门禁',
};

/**
 * 按描述集分类过滤节点，并仅保留两端均位于可见集合内的依赖有向边
 */
export function filterProtoGraphByCategory(
  graph: ProtoDependenciesGraphDTO,
  category: ProtoCategoryFilter
): ProtoDependenciesGraphDTO {
  const nodes = graph.nodes || [];
  const edges = graph.edges || [];
  const visibleNodes = category === 'ALL' ? nodes : nodes.filter((n) => n.category === category);
  const visibleIdSet = new Set(visibleNodes.map((n) => String(n.id)));
  const visibleEdges = edges.filter(
    (e) => visibleIdSet.has(String(e.from_schema_id)) && visibleIdSet.has(String(e.to_schema_id))
  );
  return {
    pool_summary: graph.pool_summary,
    nodes: visibleNodes,
    edges: visibleEdges,
  };
}

/**
 * 从图节点 ID (schema_123) 解析供 ?tab=assets&schema_id=X 使用的导航参数
 */
export function getSchemaNodeNavigationTarget(nodeId: string): { tab: 'assets'; schema_id: string } {
  const schemaId = nodeId.startsWith('schema_') ? nodeId.slice('schema_'.length) : nodeId;
  return { tab: 'assets', schema_id: schemaId };
}

/**
 * 使用既有 ELK 引擎对 Proto 描述集跨包 import 有向图执行分栏层次布局
 */
export async function layoutProtoDependenciesGraph(
  graph: ProtoDependenciesGraphDTO
): Promise<{ nodes: ProtoSchemaFlowNode[]; edges: ProtoSchemaFlowEdge[] }> {
  const rawNodes = graph.nodes || [];
  const rawEdges = graph.edges || [];

  if (rawNodes.length === 0) {
    return { nodes: [], edges: [] };
  }

  const validIds = new Set(rawNodes.map((n) => `schema_${n.id}`));
  const elkEdges = rawEdges
    .map((e, idx) => ({
      id: `p_edge_${e.from_schema_id}_${e.to_schema_id}_${idx}`,
      sources: [`schema_${e.from_schema_id}`],
      targets: [`schema_${e.to_schema_id}`],
    }))
    .filter((e) => validIds.has(e.sources[0]) && validIds.has(e.targets[0]));

  const positionMap = new Map<string, { x: number; y: number }>();

  try {
    const layouted = await elkInstance.layout({
      id: 'proto-graph-root',
      layoutOptions: {
        'elk.algorithm': 'layered',
        'elk.direction': 'RIGHT',
        'elk.spacing.nodeNode': '40',
        'elk.layered.spacing.nodeNodeBetweenLayers': '90',
        'elk.edgeRouting': 'SPLINES',
      },
      children: rawNodes.map((n) => ({
        id: `schema_${n.id}`,
        width: NODE_WIDTH,
        height: NODE_HEIGHT,
      })),
      edges: elkEdges,
    });

    for (const child of layouted.children || []) {
      positionMap.set(child.id, {
        x: Number.isFinite(child.x) ? Number(child.x) : 0,
        y: Number.isFinite(child.y) ? Number(child.y) : 0,
      });
    }
  } catch {
    rawNodes.forEach((n, index) => {
      positionMap.set(`schema_${n.id}`, {
        x: (index % 3) * (NODE_WIDTH + 60),
        y: Math.floor(index / 3) * (NODE_HEIGHT + 40),
      });
    });
  }

  const flowNodes: ProtoSchemaFlowNode[] = rawNodes.map((n, index) => {
    const id = `schema_${n.id}`;
    const pos = positionMap.get(id) ?? {
      x: (index % 3) * (NODE_WIDTH + 60),
      y: Math.floor(index / 3) * (NODE_HEIGHT + 40),
    };
    return {
      id,
      type: 'protoSchemaNode',
      position: pos,
      width: NODE_WIDTH,
      height: NODE_HEIGHT,
      data: { schemaNode: n },
    };
  });

  const flowEdges: ProtoSchemaFlowEdge[] = rawEdges.map((e, idx) => {
    const labelText =
      e.matched_protos && e.matched_protos.length > 0
        ? e.matched_protos.length === 1
          ? e.matched_protos[0]
          : `${e.matched_protos[0]} (+${e.matched_protos.length - 1})`
        : 'import';

    return {
      id: `edge_${e.from_schema_id}_${e.to_schema_id}_${idx}`,
      source: `schema_${e.from_schema_id}`,
      target: `schema_${e.to_schema_id}`,
      label: labelText,
      animated: false,
      markerEnd: {
        type: MarkerType.ArrowClosed,
        color: 'var(--color-primary)',
      },
      style: {
        stroke: 'var(--color-primary)',
        strokeWidth: 1.6,
      },
      labelStyle: {
        fontSize: 11,
        fill: 'var(--color-text-secondary)',
      },
      labelBgStyle: {
        fill: 'var(--color-bg-surface)',
      },
    };
  });

  return { nodes: flowNodes, edges: flowEdges };
}

const ProtoSchemaGraphNodeCard: React.FC<NodeProps<ProtoSchemaFlowNode>> = ({ data }) => {
  const { schemaNode } = data;
  const catMeta = CATEGORY_META[schemaNode.category] || CATEGORY_META.BUSINESS;
  const srcLabel = schemaNode.source_type ? SOURCE_TYPE_LABEL[schemaNode.source_type] || schemaNode.source_type : '';

  return (
    <div
      className="code-proto-graph-node"
      data-testid={`proto-graph-node-${schemaNode.id}`}
      style={{
        width: `${NODE_WIDTH}px`,
        minHeight: `${NODE_HEIGHT}px`,
        padding: '10px 12px',
        borderRadius: '8px',
        background: 'var(--color-bg-surface)',
        border: `1.5px solid ${catMeta.borderColor}`,
        boxShadow: 'var(--shadow-sm)',
        color: 'var(--color-text-primary)',
        cursor: 'pointer',
        display: 'flex',
        flexDirection: 'column',
        justifyContent: 'space-between',
        gap: '6px',
      }}
    >
      <Handle type="target" position={Position.Left} />
      <Handle type="source" position={Position.Right} />

      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '6px' }}>
        <span
          style={{
            fontSize: '13px',
            fontWeight: 600,
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
          title={schemaNode.name}
        >
          {schemaNode.name}
        </span>
        <span
          style={{
            fontSize: '11px',
            padding: '1px 6px',
            borderRadius: '4px',
            background: 'var(--color-bg-muted)',
            color: 'var(--color-text-secondary)',
            fontWeight: 600,
            flexShrink: 0,
          }}
        >
          v{schemaNode.version}
        </span>
      </div>

      <div style={{ display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: '6px' }}>
        <span
          style={{
            fontSize: '11px',
            padding: '2px 6px',
            borderRadius: '4px',
            background: catMeta.badgeBg,
            color: catMeta.badgeColor,
            fontWeight: 600,
          }}
        >
          {catMeta.icon} {catMeta.label}
        </span>
        {srcLabel && (
          <span
            style={{
              fontSize: '11px',
              padding: '2px 6px',
              borderRadius: '4px',
              background: 'var(--color-bg-muted)',
              color: 'var(--color-text-muted)',
            }}
          >
            {srcLabel}
          </span>
        )}
      </div>

      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          fontSize: '11px',
          color: 'var(--color-text-secondary)',
        }}
      >
        <span>
          {schemaNode.proto_file_count} 文件 / {schemaNode.service_count} 服务 / {schemaNode.operation_count} 方法
        </span>
        <span>{schemaNode.is_active ? '🟢 生效' : '⚪ 草稿'}</span>
      </div>
    </div>
  );
};

const protoNodeTypes = {
  protoSchemaNode: ProtoSchemaGraphNodeCard,
};

export const ProtoDependenciesGraphPane: React.FC<ProtoDependenciesGraphPaneProps> = ({
  initialGraph,
  onNavigateToSchemaAsset,
}) => {
  const toast = useToast();
  const [graph, setGraph] = useState<ProtoDependenciesGraphDTO>(
    initialGraph || { nodes: [], edges: [] }
  );
  const [loading, setLoading] = useState(!initialGraph);
  const [categoryFilter, setCategoryFilter] = useState<ProtoCategoryFilter>('ALL');
  const [activeOnly, setActiveOnly] = useState(true);
  const [flowNodes, setFlowNodes] = useState<ProtoSchemaFlowNode[]>([]);
  const [flowEdges, setFlowEdges] = useState<ProtoSchemaFlowEdge[]>([]);

  const loadGraph = useCallback(async () => {
    setLoading(true);
    try {
      const data = await schemaApi.getProtoDependenciesGraph({ active_only: activeOnly });
      setGraph(data);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '加载跨包依赖图谱失败');
    } finally {
      setLoading(false);
    }
  }, [activeOnly, toast]);

  useEffect(() => {
    if (!initialGraph) {
      void loadGraph();
    }
  }, [initialGraph, loadGraph]);

  const filteredGraph = useMemo(
    () => filterProtoGraphByCategory(graph, categoryFilter),
    [graph, categoryFilter]
  );

  useEffect(() => {
    let cancelled = false;
    void layoutProtoDependenciesGraph(filteredGraph).then((res) => {
      if (!cancelled) {
        setFlowNodes(res.nodes);
        setFlowEdges(res.edges);
      }
    });
    return () => {
      cancelled = true;
    };
  }, [filteredGraph]);

  const filterButtons: Array<{ value: ProtoCategoryFilter; label: string }> = [
    { value: 'ALL', label: `全部 (${graph.nodes.length})` },
    {
      value: 'BASE_LIB',
      label: `🧱 基础公共库 (${graph.nodes.filter((n) => n.category === 'BASE_LIB').length})`,
    },
    {
      value: 'BUSINESS',
      label: `📦 业务域服务 (${graph.nodes.filter((n) => n.category === 'BUSINESS').length})`,
    },
    {
      value: 'GATEWAY',
      label: `🌐 网关聚合层 (${graph.nodes.filter((n) => n.category === 'GATEWAY').length})`,
    },
  ];

  return (
    <div className="proto-graph-pane" data-testid="proto-dependencies-graph-pane">
      {/* 顶部过滤工具条 */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          flexWrap: 'wrap',
          gap: '12px',
          marginBottom: '12px',
          padding: '10px 14px',
          borderRadius: '8px',
          background: 'var(--color-bg-surface)',
          border: '1px solid var(--color-border-primary)',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' }}>
          {filterButtons.map((btn) => {
            const selected = categoryFilter === btn.value;
            return (
              <button
                key={btn.value}
                type="button"
                onClick={() => setCategoryFilter(btn.value)}
                style={{
                  padding: '5px 12px',
                  borderRadius: '6px',
                  fontSize: '12px',
                  fontWeight: selected ? 600 : 400,
                  cursor: 'pointer',
                  border: `1px solid ${selected ? 'var(--color-primary)' : 'var(--color-border-primary)'}`,
                  background: selected ? 'var(--color-bg-muted)' : 'var(--color-bg-surface)',
                  color: selected ? 'var(--color-primary)' : 'var(--color-text-secondary)',
                }}
              >
                {btn.label}
              </button>
            );
          })}
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
          <label
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: '6px',
              fontSize: '12px',
              color: 'var(--color-text-secondary)',
              cursor: 'pointer',
            }}
          >
            <input
              type="checkbox"
              checked={activeOnly}
              onChange={(e) => setActiveOnly(e.target.checked)}
            />
            <span>仅展示生效池版本</span>
          </label>

          <button
            type="button"
            className="btn btn-secondary"
            onClick={() => void loadGraph()}
            disabled={loading}
            style={{ display: 'inline-flex', alignItems: 'center', gap: '6px', fontSize: '12px' }}
          >
            <RefreshCw size={13} />
            <span>{loading ? '刷新中...' : '刷新依赖图'}</span>
          </button>
        </div>
      </div>

      {/* 图谱摘要提示 & 空态 / ReactFlow 舞台 */}
      {filteredGraph.nodes.length === 0 ? (
        <div
          className="glass-card"
          style={{
            padding: '56px 24px',
            textAlign: 'center',
            background: 'var(--color-bg-surface)',
            border: '1px solid var(--color-border-primary)',
            borderRadius: '8px',
            color: 'var(--color-text-secondary)',
          }}
        >
          <p style={{ fontSize: '14px', fontWeight: 600, color: 'var(--color-text-primary)', marginBottom: '6px' }}>
            当前筛选条件下暂无描述集依赖关系节点
          </p>
          <p style={{ fontSize: '12px' }}>
            点击任意节点将自动下钻至对应描述集资产台账；可切换分类或上传带外部 import 的描述集。
          </p>
          <div style={{ display: 'flex', justifyContent: 'center', gap: '8px', marginTop: '12px', flexWrap: 'wrap' }}>
            {graph.nodes.map((n) => (
              <span key={n.id} style={{ fontSize: '12px', color: 'var(--color-text-muted)' }}>
                {n.name}
              </span>
            ))}
          </div>
        </div>
      ) : (
        <div
          className="glass-card"
          style={{
            height: '580px',
            borderRadius: '8px',
            overflow: 'hidden',
            background: 'var(--color-bg-canvas, var(--color-bg-surface))',
            border: '1px solid var(--color-border-primary)',
            position: 'relative',
          }}
        >
          {/* 供无头 SSR 或无障碍列表快速索引当前节点名 */}
          <div style={{ display: 'none' }} aria-hidden="true">
            {filteredGraph.nodes.map((n) => (
              <span key={n.id}>{n.name}</span>
            ))}
          </div>

          <ReactFlow
            nodes={flowNodes}
            edges={flowEdges}
            nodeTypes={protoNodeTypes}
            fitView
            nodesDraggable={false}
            nodesConnectable={false}
            proOptions={{ hideAttribution: true }}
            onNodeClick={(_, node) => {
              const target = getSchemaNodeNavigationTarget(node.id);
              onNavigateToSchemaAsset(target.schema_id);
            }}
          >
            <Controls showInteractive={false} />
            <Background variant={BackgroundVariant.Dots} gap={18} size={1} />
          </ReactFlow>
        </div>
      )}
    </div>
  );
};
