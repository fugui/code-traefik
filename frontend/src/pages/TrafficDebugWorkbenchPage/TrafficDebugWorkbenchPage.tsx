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
} from '../../types';
import { DebugSessionContextBar } from './DebugSessionContextBar';
import { LiveTrafficFilterForm, type TrafficFilterValues } from './LiveTrafficFilterForm';
import { TrafficRecordTable } from './TrafficRecordTable';
import { SinglePacketReplayDrawer } from './SinglePacketReplayDrawer';
import { PacketAnatomyDrawer } from '../../components/PacketAnatomyDrawer';

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

  const [sessions, setSessions] = useState<RecordingSession[]>([]);
  const [selectedSessionId, setSelectedSessionId] = useState<string>(urlSessionId);
  const [liveMode, setLiveMode] = useState<boolean>(urlLive);
  const [isPaused, setIsPaused] = useState<boolean>(false);

  const [records, setRecords] = useState<TrafficRecordSummary[]>([]);
  const [totalRecords, setTotalRecords] = useState<number>(0);
  const [loading, setLoading] = useState<boolean>(false);

  const [allProcesses, setAllProcesses] = useState<Process[]>([]);
  const [allAgents, setAllAgents] = useState<Agent[]>([]);

  // 筛选器状态
  const [filters, setFilters] = useState<TrafficFilterValues>({
    trace_id: urlTraceId || undefined,
  });

  // 抽屉状态
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

  // 3. 加载报文列表
  const loadRecords = useCallback(async () => {
    if (!selectedSessionId) return;
    setLoading(true);
    try {
      const res = await trafficApi.listTrafficRecords({
        session_id: selectedSessionId,
        direction: filters.direction,
        exchange_type: filters.exchange_type,
        service_name: filters.service_name,
        operation: filters.operation,
        sno: filters.sno,
        trace_id: filters.trace_id,
        keyword: filters.keyword,
        is_replayable: filters.is_replayable,
        has_error: filters.has_error,
        page,
        page_size: pageSize,
      });
      setRecords(res.items ?? []);
      setTotalRecords(res.total ?? 0);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '检索报文流水失败');
    } finally {
      setLoading(false);
    }
  }, [selectedSessionId, filters, page, pageSize, toast]);

  useEffect(() => {
    void loadRecords();
  }, [loadRecords]);

  // 4. 跟流定时轮询刷新
  useEffect(() => {
    if (!liveMode || isPaused) return;
    const timer = setInterval(() => {
      void loadRecords();
    }, 2000);
    return () => clearInterval(timer);
  }, [liveMode, isPaused, loadRecords]);

  const handleSelectSession = (id: string) => {
    setSelectedSessionId(id);
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      next.set('session_id', id);
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

  return (
    <div className="code-workbench-page code-traffic-debug-workbench" data-testid="traffic-debug-workbench-page">
      <div className="code-workbench-header">
        <div className="code-workbench-title-group">
          <h2 className="code-workbench-title">独立流量调试工作台</h2>
          <span className="code-workbench-subtitle">
            深度解剖 CROS RPC 往返报文，依托 Trace 串联与异常过滤定位缺陷，并对入站报文发起单包快速重放验证
          </span>
        </div>
      </div>

      {/* 批次上下文导航条 */}
      <DebugSessionContextBar
        currentSessionId={selectedSessionId}
        sessions={sessions}
        onSelectSession={handleSelectSession}
        liveStreamActive={liveMode}
        onToggleLiveStream={() => setLiveMode((prev) => !prev)}
        isPaused={isPaused}
        onTogglePause={() => setIsPaused((prev) => !prev)}
      />

      {/* 报文检索多维过滤栏 (含 has_error) */}
      <div className="code-mt-sm">
        <LiveTrafficFilterForm
          filters={filters}
          onChange={handleFilterChange}
          onReset={handleFilterReset}
        />
      </div>

      {/* 报文流水表 */}
      <div className="code-mt-sm">
        <TrafficRecordTable
          records={records}
          onSelectRecord={(r) => void handleOpenAnatomy(r)}
          selectedRecordId={selectedRecordForAnatomy?.id}
          onSinglePacketReplay={handleOpenSingleReplay}
          loading={loading}
        />

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
              单包重放调试 &rarr;
            </button>
          ) : null
        }
      />

      {/* 单包重放调试抽屉 */}
      <SinglePacketReplayDrawer
        open={singleReplayDrawerOpen}
        onClose={() => setSingleReplayDrawerOpen(false)}
        record={recordForReplay}
        allProcesses={allProcesses}
        allAgents={allAgents}
        currentUserId={user?.id}
      />
    </div>
  );
};

export default TrafficDebugWorkbenchPage;
