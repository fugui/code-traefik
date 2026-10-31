import React, { useState, useEffect, useCallback, useMemo } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Drawer } from '@code/common';
import { useToast } from '../../utils/toast';
import * as archApi from '../../api/arch';
import * as trafficApi from '../../api/traffic';
import type {
  TopologyGraphDTO,
  GraphNode,
  RollupEdgeModel,
  ExpandStateMap,
} from '../../types/topology_graph';
import type {
  ConsistencyReportDTO,
  RecordingSession,
  PacketAnatomyDrawerDTO,
  ConsistencyEdge,
} from '../../types/traffic';
import { ArchCanvas } from '../../components/ArchCanvas/ArchCanvas';
import { ArchCanvasToolbar } from '../../components/ArchCanvas/panels/ArchCanvasToolbar';
import { ArchSummaryBar } from '../../components/ArchCanvas/panels/ArchSummaryBar';
import { NodeInspectDrawer } from '../../components/ArchCanvas/panels/NodeInspectDrawer';
import { EdgeDetailDrawer } from '../../components/ArchCanvas/panels/EdgeDetailDrawer';
import { PacketAnatomyDrawer } from '../../components/PacketAnatomyDrawer/PacketAnatomyDrawer';
import { HelpCircle } from 'lucide-react';

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

  // 1. 视角与状态控制
  const paramView = (searchParams.get('view')?.toUpperCase() as 'LOGICAL' | 'PHYSICAL') || initialView;
  const [view, setView] = useState<'LOGICAL' | 'PHYSICAL'>(
    paramView === 'PHYSICAL' ? 'PHYSICAL' : 'LOGICAL'
  );

  const [graphData, setGraphData] = useState<TopologyGraphDTO | null>(null);
  const [loadingGraph, setLoadingGraph] = useState(false);
  const [expandState, setExpandState] = useState<ExpandStateMap>({});
  const [depthLevel, setDepthLevel] = useState<number>(0);
  const [wheelAutoExpand, setWheelAutoExpand] = useState<boolean>(false);
  const [searchKeyword, setSearchKeyword] = useState<string>('');
  const [resetKey, setResetKey] = useState<number>(0);

  // 2. 一致性比对录制批次
  const paramSessionId = searchParams.get('session_id') || initialSessionId;
  const [sessions, setSessions] = useState<RecordingSession[]>([]);
  const [selectedSessionId, setSelectedSessionId] = useState<string | null>(paramSessionId || null);
  const [consistencyReport, setConsistencyReport] = useState<ConsistencyReportDTO | null>(null);
  const [loadingReport, setLoadingReport] = useState(false);

  // 监听浏览器 URL 前进/后退/直达变化
  useEffect(() => {
    const pv = (searchParams.get('view')?.toUpperCase() as 'LOGICAL' | 'PHYSICAL') || initialView;
    const nextView = pv === 'PHYSICAL' ? 'PHYSICAL' : 'LOGICAL';
    setView((curr) => (curr !== nextView ? nextView : curr));

    const ps = searchParams.get('session_id') || initialSessionId || null;
    setSelectedSessionId((curr) => (curr !== ps ? ps : curr));
  }, [searchParams, initialView, initialSessionId]);

  // 搜索关键字联动：当输入 searchKeyword 时，自动沿父链展开命中节点的容器
  useEffect(() => {
    const kw = searchKeyword.trim().toLowerCase();
    if (!kw || !graphData || !graphData.nodes) return;

    const parentMap = new Map<string, string | null>();
    graphData.nodes.forEach((n) => {
      parentMap.set(n.id, n.parent_id ?? null);
    });

    const toExpand: ExpandStateMap = {};
    let hasMatch = false;

    graphData.nodes.forEach((n) => {
      const match =
        n.name.toLowerCase().includes(kw) ||
        (n.code && n.code.toLowerCase().includes(kw)) ||
        n.id.toLowerCase().includes(kw);
      if (match) {
        hasMatch = true;
        let curr = parentMap.get(n.id);
        while (curr) {
          toExpand[curr] = true;
          curr = parentMap.get(curr);
        }
      }
    });

    if (hasMatch && Object.keys(toExpand).length > 0) {
      setExpandState((prev) => ({
        ...prev,
        ...toExpand,
      }));
    }
  }, [searchKeyword, graphData]);

  // 3. 抽屉状态
  const [selectedNode, setSelectedNode] = useState<GraphNode | null>(null);
  const [isNodeDrawerOpen, setIsNodeDrawerOpen] = useState(false);
  const [selectedEdge, setSelectedEdge] = useState<RollupEdgeModel | null>(null);
  const [isEdgeDrawerOpen, setIsEdgeDrawerOpen] = useState(false);
  const [isPendingDrawerOpen, setIsPendingDrawerOpen] = useState(false);

  // 4. 报文四段解剖抽屉状态
  const [anatomyData, setAnatomyData] = useState<PacketAnatomyDrawerDTO | null>(null);
  const [isAnatomyOpen, setIsAnatomyOpen] = useState(false);
  const [loadingAnatomy, setLoadingAnatomy] = useState(false);

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

  // 切换视角
  const handleViewChange = useCallback(
    (nextView: 'LOGICAL' | 'PHYSICAL') => {
      setView(nextView);
      updateUrlParams({ view: nextView });
      if (onViewChange) {
        onViewChange(nextView);
      }
    },
    [onViewChange, updateUrlParams]
  );

  // 加载全系统复合拓扑图
  const loadGraph = useCallback(
    async (targetView: 'LOGICAL' | 'PHYSICAL') => {
      setLoadingGraph(true);
      try {
        const res = await archApi.getTopologyGraph(targetView);
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
    void loadGraph(view);
  }, [view, loadGraph]);

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
        // 批次列表非强依赖，静默处理
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

  // 选择批次变更
  const handleSelectSession = useCallback(
    (sessionId: string | null) => {
      setSelectedSessionId(sessionId);
      updateUrlParams({ session_id: sessionId });
    },
    [updateUrlParams]
  );

  // 计算最大深度
  const maxDepth = useMemo(() => {
    if (!graphData || !graphData.nodes || graphData.nodes.length === 0) return 0;
    return Math.max(0, ...graphData.nodes.map((n) => n.depth));
  }, [graphData]);

  // 层级深度滑杆调整（自动联动展开/折叠）
  const handleDepthLevelChange = useCallback(
    (level: number) => {
      setDepthLevel(level);
      if (!graphData) return;

      const nextExpand: ExpandStateMap = {};
      graphData.nodes.forEach((n) => {
        // 容器类节点具有展开能力
        const isContainer =
          n.type === 'SUBSYSTEM' ||
          n.type === 'FUNCTIONAL_GROUP' ||
          n.type === 'PROCESS_GROUP' ||
          n.type === 'PROCESS' ||
          n.type === 'UNDEPLOYED_CONTAINER' ||
          n.type === 'EXTERNAL_CONTAINER';

        if (isContainer) {
          // 深度小于滑杆目标值的容器自动展开，大于等于目标值的折叠
          nextExpand[n.id] = n.depth < level;
        }
      });
      setExpandState(nextExpand);
    },
    [graphData]
  );

  // 单节点展开/折叠切换
  const handleToggleExpand = useCallback((nodeId: string) => {
    setExpandState((prev) => ({
      ...prev,
      [nodeId]: !prev[nodeId],
    }));
  }, []);

  // 重置布局
  const handleResetLayout = useCallback(() => {
    setResetKey((k) => k + 1);
    toast.success('已重置手动拖拽位移并恢复最优正交排版');
  }, [toast]);

  // 节点点击检视
  const handleSelectNode = useCallback((node: GraphNode) => {
    setSelectedNode(node);
    setIsNodeDrawerOpen(true);
  }, []);

  // 边点击检视
  const handleSelectEdge = useCallback((edge: RollupEdgeModel) => {
    setSelectedEdge(edge);
    setIsEdgeDrawerOpen(true);
  }, []);

  // 穿透打开报文四段解剖
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

  // 待定边待办列表
  const unattributedPendingList: ConsistencyEdge[] = useMemo(() => {
    if (!consistencyReport || !consistencyReport.edges) return [];
    return consistencyReport.edges.filter(
      (e) => e.status === 'UNATTRIBUTED' || e.category === 'UNATTRIBUTED'
    );
  }, [consistencyReport]);

  return (
    <div className="code-arch-topology-tab-pane" style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
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
      />

      {/* 一致性报告摘要栏 */}
      {loadingReport && (
        <div style={{ padding: '4px 12px', fontSize: 12, color: 'var(--color-text-secondary)', backgroundColor: 'var(--color-bg-muted)' }}>
          比对一致性计算中...
        </div>
      )}
      {consistencyReport && !loadingReport && (
        <ArchSummaryBar
          report={consistencyReport}
          unattributedPendingList={unattributedPendingList}
          onOpenPendingList={() => setIsPendingDrawerOpen(true)}
        />
      )}

      {/* 画布主体 */}
      <div style={{ flex: 1, position: 'relative', overflow: 'hidden' }}>
        {loadingGraph && (
          <div
            style={{
              position: 'absolute',
              inset: 0,
              zIndex: 20,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              backgroundColor: 'rgba(var(--color-bg-surface), 0.65)',
              backdropFilter: 'blur(2px)',
            }}
          >
            <div style={{ textAlign: 'center', color: 'var(--color-text-secondary)', fontSize: 13 }}>
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
          onSelectEdge={handleSelectEdge}
          consistencyReport={consistencyReport}
          view={view}
          resetKey={resetKey}
          searchKeyword={searchKeyword}
          wheelAutoExpand={wheelAutoExpand}
          depthLevel={depthLevel}
          maxDepth={maxDepth}
          onDepthLevelChange={handleDepthLevelChange}
          className="code-arch-canvas--full"
        />
      </div>

      {/* 节点画像抽屉 */}
      <NodeInspectDrawer
        open={isNodeDrawerOpen}
        onClose={() => setIsNodeDrawerOpen(false)}
        node={selectedNode}
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
        <div style={{ padding: '8px 4px' }}>
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 8,
              padding: '10px 12px',
              backgroundColor: 'var(--color-bg-muted)',
              borderRadius: 6,
              marginBottom: 16,
              fontSize: 12,
              color: 'var(--color-text-secondary)',
            }}
          >
            <HelpCircle size={16} style={{ color: 'var(--color-warning)' }} />
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
                  <td colSpan={5} style={{ textAlign: 'center', color: 'var(--color-text-muted)', padding: '24px 0' }}>
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
                    <tr key={`${edge.src_module_id ?? edge.src_process_id ?? 'u'}-${edge.dst_module_id ?? edge.service_name}-${idx}`}>
                      <td>{idx + 1}</td>
                      <td>
                        <div style={{ fontSize: 12 }}>
                          <div>{srcLabel}</div>
                          <div style={{ color: 'var(--color-text-muted)', fontSize: 11 }}>
                            → {dstLabel}
                          </div>
                        </div>
                      </td>
                      <td>
                        <code style={{ fontSize: 11 }}>{edge.service_name || '-'}</code>
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
