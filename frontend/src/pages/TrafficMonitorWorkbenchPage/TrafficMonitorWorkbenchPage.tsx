import React, { useCallback, useEffect, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Pagination, usePaginationState } from '@code/common';
import { useToast } from '../../utils/toast';
import { useModuleUser } from '../../context/ModuleUserContext';
import * as trafficApi from '../../api/traffic';
import * as agentApi from '../../api/agent';
import { subscribeServerEvents } from '../../api/sse';
import type {
  RecordingSession,
  TrafficRecordSummary,
  PacketAnatomyDrawerDTO,
  Process,
  Agent,
} from '../../types';
import { RecordingSessionBar } from './RecordingSessionBar';
import { LiveTrafficFilterForm, type TrafficFilterValues } from './LiveTrafficFilterForm';
import { TrafficRecordTable } from './TrafficRecordTable';
import { PacketAnatomyDrawer } from './PacketAnatomyDrawer';

export const TrafficMonitorWorkbenchPage: React.FC = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const { user } = useModuleUser();
  const toast = useToast();
  const isAdmin = Boolean(user?.is_module_admin || user?.is_super_admin);

  const { page, pageSize, setPage, setPageSize } = usePaginationState({
    defaultPageSize: 25,
  });

  const urlSessionId = searchParams.get('session_id') || '';

  // 筛选参数从 URL 解析
  const filters: TrafficFilterValues = {
    direction: (searchParams.get('direction') as 'INGRESS' | 'EGRESS') || undefined,
    exchange_type: searchParams.get('exchange_type') || undefined,
    service_name: searchParams.get('service_name') || undefined,
    operation: searchParams.get('operation') || undefined,
    sno: searchParams.get('sno') || undefined,
    trace_id: searchParams.get('trace_id') || undefined,
    keyword: searchParams.get('keyword') || undefined,
    is_replayable: searchParams.get('is_replayable') === 'true' ? true : undefined,
  };

  const [sessions, setSessions] = useState<RecordingSession[]>([]);
  const [currentSession, setCurrentSession] = useState<RecordingSession | null>(null);
  const [allProcesses, setAllProcesses] = useState<Process[]>([]);
  const [allAgents, setAllAgents] = useState<Agent[]>([]);

  const [records, setRecords] = useState<TrafficRecordSummary[]>([]);
  const [totalRecords, setTotalRecords] = useState(0);
  const [loading, setLoading] = useState(false);

  // SSE 实时流控制
  const [isSsePaused, setIsSsePaused] = useState(false);

  // 报文解剖抽屉状态
  const [anatomyDrawerOpen, setAnatomyDrawerOpen] = useState(false);
  const [selectedRecordId, setSelectedRecordId] = useState<string>('');
  const [anatomyData, setAnatomyData] = useState<PacketAnatomyDrawerDTO | null>(null);
  const [loadingAnatomy, setLoadingAnatomy] = useState(false);

  // 加载录制会话
  const loadSessions = useCallback(async () => {
    try {
      const res = await trafficApi.listRecordingSessions({ page_size: 50 });
      setSessions(res.items);
      if (res.items.length > 0) {
        const found = res.items.find((s) => s.id === urlSessionId) || res.items[0];
        setCurrentSession(found);
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '加载录制批次失败');
    }
  }, [urlSessionId, toast]);

  useEffect(() => {
    void loadSessions();
  }, [loadSessions]);

  // 加载进程与 Agent 资源
  useEffect(() => {
    void (async () => {
      try {
        const [procRes, agentRes] = await Promise.all([
          agentApi.listProcesses({ page_size: 100 }),
          agentApi.listAgents({ page_size: 100 }),
        ]);
        setAllProcesses(procRes.items);
        setAllAgents(agentRes.items);
      } catch {
        // 静默
      }
    })();
  }, []);

  const currentSessionId = currentSession?.id;

  // 加载流量记录列表
  const loadRecords = useCallback(async () => {
    if (!currentSessionId) return;
    setLoading(true);
    try {
      const res = await trafficApi.listTrafficRecords({
        session_id: currentSessionId,
        direction: filters.direction,
        exchange_type: filters.exchange_type,
        service_name: filters.service_name,
        operation: filters.operation,
        sno: filters.sno,
        trace_id: filters.trace_id,
        keyword: filters.keyword,
        is_replayable: filters.is_replayable,
        page,
        page_size: pageSize,
      });
      setRecords(res.items);
      setTotalRecords(res.total);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '加载流量记录失败');
    } finally {
      setLoading(false);
    }
  }, [
    currentSessionId,
    filters.direction,
    filters.exchange_type,
    filters.service_name,
    filters.operation,
    filters.sno,
    filters.trace_id,
    filters.keyword,
    filters.is_replayable,
    page,
    pageSize,
    toast,
  ]);

  useEffect(() => {
    void loadRecords();
  }, [loadRecords]);

  const loadRecordsRef = useRef(loadRecords);
  loadRecordsRef.current = loadRecords;

  // SSE 实时流订阅
  useEffect(() => {
    if (!currentSessionId || isSsePaused) return;

    const unsubscribe = subscribeServerEvents({
      sessionId: currentSessionId,
      onMessage: (evt) => {
        if (evt.event === 'traffic.persisted' || evt.event === 'session.lifecycle') {
          void loadRecordsRef.current();
        }
      },
    });

    return () => {
      unsubscribe();
    };
  }, [currentSessionId, isSsePaused]);

  // 打开单条报文解剖抽屉
  const handleOpenAnatomy = async (record: TrafficRecordSummary) => {
    setSelectedRecordId(record.id);
    setAnatomyDrawerOpen(true);
    setLoadingAnatomy(true);
    try {
      const data = await trafficApi.getTrafficRecordAnatomy(record.id, record.session_id);
      setAnatomyData(data);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '获取报文解剖失败');
    } finally {
      setLoadingAnatomy(false);
    }
  };

  const handleRedecode = async (recordId: string) => {
    try {
      const updated = await trafficApi.redecodeTrafficRecord(recordId);
      setAnatomyData(updated);
      toast.success('报文已按当前生效 Protobuf 描述集重新解码');
      void loadRecords();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '重解码失败');
    }
  };

  const handleFilterChange = (updates: Partial<TrafficFilterValues>) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      Object.entries(updates).forEach(([k, v]) => {
        if (v !== undefined && v !== '') {
          next.set(k, String(v));
        } else {
          next.delete(k);
        }
      });
      next.set('page', '1');
      return next;
    });
  };

  const handleResetFilters = () => {
    setSearchParams((prev) => {
      const next = new URLSearchParams();
      const currentSessionId = prev.get('session_id');
      if (currentSessionId) next.set('session_id', currentSessionId);
      return next;
    });
  };

  return (
    <div className="code-workbench-page code-traffic-workbench" data-testid="traffic-monitor-workbench">
      <div className="code-workbench-header">
        <div className="code-workbench-title-group">
          <h2 className="code-workbench-title">协同录制与流量解剖台</h2>
          <span className="code-workbench-subtitle">
            单 Agent 跨进程协同录制、快轨 SSE 实时帧流与无损+结构化四段深度解剖
          </span>
        </div>
      </div>

      {/* 录制批次控制栏 */}
      <RecordingSessionBar
        currentSession={currentSession}
        sessions={sessions}
        onSelectSession={(id) => {
          const s = sessions.find((item) => item.id === id) || null;
          setCurrentSession(s);
          setSearchParams((prev) => {
            const next = new URLSearchParams(prev);
            next.set('session_id', id);
            next.set('page', '1');
            return next;
          });
        }}
        onRefreshSessions={() => void loadSessions()}
        allProcesses={allProcesses}
        allAgents={allAgents}
        isAdmin={isAdmin}
        currentUserId={user?.id}
      />

      {/* 多维 URL 筛选栏 */}
      <div className="code-mt-sm">
        <LiveTrafficFilterForm
          filters={filters}
          onChange={handleFilterChange}
          onReset={handleResetFilters}
          isSsePaused={isSsePaused}
          onToggleSsePause={() => setIsSsePaused(!isSsePaused)}
        />
      </div>

      {/* 流量主表 */}
      <div className="code-mt-sm">
        {loading ? (
          <div className="code-tpl-loading-screen">正在加载流量报文列表...</div>
        ) : (
          <TrafficRecordTable
            records={records}
            onSelectRecord={(rec) => void handleOpenAnatomy(rec)}
            selectedRecordId={selectedRecordId}
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

      {/* 四段深度报文解剖抽屉 */}
      <PacketAnatomyDrawer
        open={anatomyDrawerOpen}
        onClose={() => setAnatomyDrawerOpen(false)}
        data={anatomyData}
        loading={loadingAnatomy}
        onRedecode={handleRedecode}
        onFilterTrace={(traceId) => {
          handleFilterChange({ trace_id: traceId });
          setAnatomyDrawerOpen(false);
        }}
      />
    </div>
  );
};

export default TrafficMonitorWorkbenchPage;
