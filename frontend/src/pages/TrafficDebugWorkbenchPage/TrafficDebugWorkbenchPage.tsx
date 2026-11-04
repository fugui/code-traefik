import React, { useCallback, useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Pagination, usePaginationState } from '@code/common';
import { useToast } from '../../utils/toast';
import { useModuleUser } from '../../context/ModuleUserContext';
import * as trafficApi from '../../api/traffic';
import * as agentApi from '../../api/agent';
import type {
  TrafficRecordSummary,
  RecordingSession,
  Process,
  Agent,
  PacketAnatomyDrawerDTO,
  TransactionTreeGroupDTO,
  EntityLifecycleChainDTO,
} from '../../types';
import { DebugSessionContextBar } from './DebugSessionContextBar';
import { LiveTrafficFilterForm, type TrafficFilterValues } from './LiveTrafficFilterForm';
import { TrafficRecordTable } from './TrafficRecordTable';
import { TransactionCallTreeTable } from './TransactionCallTreeTable';
import { EntityLifecycleFilterStrip } from './EntityLifecycleFilterStrip';
import { TransactionSequenceDrawer } from './TransactionSequenceDrawer';
import { SinglePacketReplayDrawer } from './SinglePacketReplayDrawer';
import { PacketAnatomyDrawer } from '../../components/PacketAnatomyDrawer';
import './TrafficDebugWorkbenchPage.css';

export const TrafficDebugWorkbenchPage: React.FC = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const { user } = useModuleUser();
  const toast = useToast();

  const { page, pageSize, setPage, setPageSize } = usePaginationState({
    defaultPageSize: 25,
  });

  const urlSessionId = searchParams.get('session_id') || '';
  const urlLive = searchParams.get('live') === '1';
  const urlTraceId = searchParams.get('trace_id') || '';
  const urlView = (searchParams.get('view') as 'tree' | 'flat') || 'tree';
  const urlEntityKey = searchParams.get('entity_key') || undefined;
  const urlSeqRootId = searchParams.get('seq_root_id') || undefined;

  const [viewMode, setViewMode] = useState<'tree' | 'flat'>(urlView);
  const [selectedEntityKey, setSelectedEntityKey] = useState<string | undefined>(urlEntityKey);
  const [forceExpandAll, setForceExpandAll] = useState<boolean | undefined>(undefined);

  const [sessions, setSessions] = useState<RecordingSession[]>([]);
  const [selectedSessionId, setSelectedSessionId] = useState<string>(urlSessionId);
  const [liveMode, setLiveMode] = useState<boolean>(urlLive);
  const [isPaused, setIsPaused] = useState<boolean>(false);

  // 树表与平铺表数据
  const [treeGroups, setTreeGroups] = useState<TransactionTreeGroupDTO[]>([]);
  const [entityChains, setEntityChains] = useState<EntityLifecycleChainDTO[]>([]);
  const [records, setRecords] = useState<TrafficRecordSummary[]>([]);
  const [totalRecords, setTotalRecords] = useState<number>(0);
  const [loading, setLoading] = useState<boolean>(false);

  const [allProcesses, setAllProcesses] = useState<Process[]>([]);
  const [allAgents, setAllAgents] = useState<Agent[]>([]);

  // 筛选器状态
  const [filters, setFilters] = useState<TrafficFilterValues>({
    trace_id: urlTraceId || undefined,
  });

  // 时序泳道抽屉
  const [sequenceDrawerOpen, setSequenceDrawerOpen] = useState(false);
  const [sequenceRootRecordId, setSequenceRootRecordId] = useState<string | undefined>(urlSeqRootId);
  const [sequenceReplayTaskId, setSequenceReplayTaskId] = useState<string | undefined>(undefined);

  // 解剖与单包重放抽屉
  const [anatomyDrawerOpen, setAnatomyDrawerOpen] = useState(false);
  const [anatomyData, setAnatomyData] = useState<PacketAnatomyDrawerDTO | null>(null);
  const [loadingAnatomy, setLoadingAnatomy] = useState(false);
  const [selectedRecordForAnatomy, setSelectedRecordForAnatomy] = useState<TrafficRecordSummary | null>(null);

  const [singleReplayDrawerOpen, setSingleReplayDrawerOpen] = useState(false);
  const [recordForReplay, setRecordForReplay] = useState<TrafficRecordSummary | null>(null);

  // 1. 初始化拉取录制批次
  useEffect(() => {
    void (async () => {
      try {
        const res = await trafficApi.listRecordingSessions({ page_size: 100 });
        const items = res.items ?? [];
        setSessions(items);
        if (items.length > 0 && !urlSessionId) {
          setSelectedSessionId(items[0].id);
        }
      } catch (err) {
        toast.error(err instanceof Error ? err.message : '加载录制批次失败');
      }
    })();
  }, [urlSessionId, toast]);

  // 2. 初始化拉取进程与 Agent
  useEffect(() => {
    void (async () => {
      try {
        const [procRes, agentRes] = await Promise.all([
          agentApi.listProcesses({ page_size: 100 }),
          agentApi.listAgents({ page_size: 100 }),
        ]);
        setAllProcesses(procRes.items ?? []);
        setAllAgents(agentRes.items ?? []);
      } catch {
        // 静默
      }
    })();
  }, []);

  // 3. 核心数据加载 (按 viewMode 切换树表或平铺表)
  const loadData = useCallback(async () => {
    if (!selectedSessionId) return;
    setLoading(true);

    try {
      if (viewMode === 'tree') {
        const res = await trafficApi.getTransactionTrees(selectedSessionId, {
          direction: filters.direction,
          exchange_type: filters.exchange_type,
          service_name: filters.service_name,
          operation: filters.operation,
          sno: filters.sno,
          trace_id: filters.trace_id,
          entity_key: selectedEntityKey,
          keyword: filters.keyword,
          is_replayable: filters.is_replayable,
          has_error: filters.has_error,
          page,
          page_size: pageSize,
        });
        setTreeGroups(res.items ?? []);
        setTotalRecords(res.total ?? 0);
        setEntityChains(res.entity_chains ?? []);
      } else {
        const res = await trafficApi.listTrafficRecords({
          session_id: selectedSessionId,
          direction: filters.direction,
          exchange_type: filters.exchange_type,
          service_name: filters.service_name,
          operation: filters.operation,
          sno: filters.sno,
          trace_id: filters.trace_id,
          keyword: filters.keyword,
          sort_dir: 'asc',
          is_replayable: filters.is_replayable,
          has_error: filters.has_error,
          page,
          page_size: pageSize,
        });
        setRecords(res.items ?? []);
        setTotalRecords(res.total ?? 0);
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '检索流量数据失败');
    } finally {
      setLoading(false);
    }
  }, [selectedSessionId, viewMode, filters, selectedEntityKey, page, pageSize, toast]);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  // 4. 定时跟流
  useEffect(() => {
    if (!liveMode || isPaused) return;
    const timer = setInterval(() => {
      void loadData();
    }, 2000);
    return () => clearInterval(timer);
  }, [liveMode, isPaused, loadData]);

  // URL 参数同步
  const handleSelectSession = (id: string) => {
    setSelectedSessionId(id);
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      next.set('session_id', id);
      next.set('page', '1');
      return next;
    });
  };

  const handleViewModeChange = (mode: 'tree' | 'flat') => {
    setViewMode(mode);
    setPage(1);
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      next.set('view', mode);
      next.set('page', '1');
      return next;
    });
  };

  const handleSelectEntityKey = (key: string | undefined) => {
    setSelectedEntityKey(key);
    setPage(1);
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      if (key) {
        next.set('entity_key', key);
      } else {
        next.delete('entity_key');
      }
      next.set('page', '1');
      return next;
    });
  };

  const handleFilterChange = (updates: Partial<TrafficFilterValues>) => {
    setFilters((prev) => ({ ...prev, ...updates }));
    setPage(1);
  };

  const handleFilterReset = () => {
    setFilters({});
    setSelectedEntityKey(undefined);
    setPage(1);
  };

  const handleOpenAnatomy = async (record: TrafficRecordSummary) => {
    setSelectedRecordForAnatomy(record);
    setAnatomyDrawerOpen(true);
    setLoadingAnatomy(true);
    try {
      const data = await trafficApi.getTrafficRecordAnatomy(record.id, record.session_id);
      setAnatomyData(data);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '获取报文四段解剖失败');
    } finally {
      setLoadingAnatomy(false);
    }
  };

  const handleOpenSingleReplay = (record: TrafficRecordSummary) => {
    setRecordForReplay(record);
    setSingleReplayDrawerOpen(true);
  };

  const handleOpenSequenceDrawer = (rootRecordId: string) => {
    setSequenceRootRecordId(rootRecordId);
    setSequenceReplayTaskId(undefined);
    setSequenceDrawerOpen(true);
  };

  const handleOpenSequenceWithTask = (rootRecordId: string, replayTaskId: string) => {
    setSequenceRootRecordId(rootRecordId);
    setSequenceReplayTaskId(replayTaskId);
    setSequenceDrawerOpen(true);
  };

  return (
    <div className="code-workbench-page code-traffic-debug-workbench" data-testid="traffic-debug-workbench-page">
      <div className="code-workbench-header">
        <div className="code-workbench-title-group">
          <h2 className="code-workbench-title">独立流量调试工作台</h2>
          <span className="code-workbench-subtitle">
            基于 DOTR 权威因果构建报文时序树与甘特瀑布，贯通参数血缘与实体生命线，支持时序泳道透视与事务闭包重放
          </span>
        </div>
      </div>

      {/* 批次上下文导航条与视图切换 */}
      <DebugSessionContextBar
        currentSessionId={selectedSessionId}
        sessions={sessions}
        onSelectSession={handleSelectSession}
        liveStreamActive={liveMode}
        onToggleLiveStream={() => setLiveMode((prev) => !prev)}
        isPaused={isPaused}
        onTogglePause={() => setIsPaused((prev) => !prev)}
        viewMode={viewMode}
        onChangeViewMode={handleViewModeChange}
        onToggleExpandAll={(expand) => setForceExpandAll(expand)}
      />

      {/* 识别出的有状态业务实体胶囊筛选条 */}
      {viewMode === 'tree' && entityChains.length > 0 && (
        <EntityLifecycleFilterStrip
          chains={entityChains}
          selectedEntityKey={selectedEntityKey}
          onSelectEntityKey={handleSelectEntityKey}
        />
      )}

      {/* 报文检索多维过滤栏 (含 has_error) */}
      <div className="code-mt-sm">
        <LiveTrafficFilterForm
          filters={filters}
          onChange={handleFilterChange}
          onReset={handleFilterReset}
        />
      </div>

      {/* 主表格：根据 viewMode 默认展示事务调用树或时序平铺 */}
      <div className="code-mt-sm">
        {viewMode === 'tree' ? (
          <TransactionCallTreeTable
            groups={treeGroups}
            loading={loading}
            onSelectRecordForAnatomy={(r) => void handleOpenAnatomy(r)}
            onOpenSequenceDrawer={handleOpenSequenceDrawer}
            onSinglePacketReplay={handleOpenSingleReplay}
            forceExpandAll={forceExpandAll}
          />
        ) : (
          <TrafficRecordTable
            records={records}
            onSelectRecord={(r) => void handleOpenAnatomy(r)}
            selectedRecordId={selectedRecordForAnatomy?.id}
            onSinglePacketReplay={handleOpenSingleReplay}
            loading={loading}
          />
        )}

        <div className="code-mt-md">
          <Pagination
            totalItems={totalRecords}
            page={page}
            pageSize={pageSize}
            onPageChange={setPage}
            onPageSizeChange={setPageSize}
          />
        </div>
      </div>

      {/* 全链路交互式时序泳道抽屉 (双栏联动) */}
      <TransactionSequenceDrawer
        open={sequenceDrawerOpen}
        onClose={() => setSequenceDrawerOpen(false)}
        sessionId={selectedSessionId}
        rootRecordId={sequenceRootRecordId}
        replayTaskId={sequenceReplayTaskId}
      />

      {/* 四段深度报文解剖抽屉 */}
      <PacketAnatomyDrawer
        open={anatomyDrawerOpen}
        onClose={() => setAnatomyDrawerOpen(false)}
        data={anatomyData}
        loading={loadingAnatomy}
        onRedecode={async (recId) => {
          try {
            const redecoded = await trafficApi.redecodeTrafficRecord(recId);
            setAnatomyData(redecoded);
            toast.success('报文已按最新生效 Proto 重新解码');
          } catch (err) {
            toast.error(err instanceof Error ? err.message : '重新解码失败');
          }
        }}
        onFilterTrace={(tid) => {
          setFilters((prev) => ({ ...prev, trace_id: tid }));
          setAnatomyDrawerOpen(false);
          toast.info(`已应用 TraceID 过滤: ${tid}`);
        }}
        extraActions={
          selectedRecordForAnatomy?.direction === 'INGRESS' &&
          selectedRecordForAnatomy?.is_replayable ? (
            <button
              type="button"
              className="code-btn code-btn--primary code-btn--sm"
              onClick={() => {
                setAnatomyDrawerOpen(false);
                handleOpenSingleReplay(selectedRecordForAnatomy);
              }}
              data-testid="btn-drawer-single-replay"
            >
              事务闭包重放 &rarr;
            </button>
          ) : null
        }
      />

      {/* 事务闭包重放调试抽屉 */}
      <SinglePacketReplayDrawer
        open={singleReplayDrawerOpen}
        onClose={() => setSingleReplayDrawerOpen(false)}
        record={recordForReplay}
        allProcesses={allProcesses}
        allAgents={allAgents}
        currentUserId={user?.id}
        onOpenSequenceWithTask={handleOpenSequenceWithTask}
      />
    </div>
  );
};

export default TrafficDebugWorkbenchPage;
