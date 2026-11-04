import React, { useCallback, useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { usePaginationState } from '@code/common';
import { useToast } from '../../utils/toast';
import { useModuleUser } from '../../context/ModuleUserContext';
import * as trafficApi from '../../api/traffic';
import * as agentApi from '../../api/agent';
import type { RecordingSession, Process, Agent } from '../../types';
import { ActiveRecordingBanner } from './ActiveRecordingBanner';
import { RecordingStartDrawer } from './RecordingStartDrawer';
import { RecordingSessionTable } from './RecordingSessionTable';

export const RecordingWorkbenchPage: React.FC = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const { user } = useModuleUser();
  const toast = useToast();
  const isAdmin = Boolean(user?.is_module_admin || user?.is_super_admin);

  const { page, pageSize, setPage, setPageSize } = usePaginationState({
    defaultPageSize: 25,
  });

  const urlScope = searchParams.get('scope') || '';
  const urlSearch = searchParams.get('search') || '';

  const [sessions, setSessions] = useState<RecordingSession[]>([]);
  const [totalSessions, setTotalSessions] = useState(0);
  const [loading, setLoading] = useState(false);
  const [activeSession, setActiveSession] = useState<RecordingSession | null>(null);
  const [stopping, setStopping] = useState(false);

  const [allProcesses, setAllProcesses] = useState<Process[]>([]);
  const [allAgents, setAllAgents] = useState<Agent[]>([]);
  const [startModalOpen, setStartModalOpen] = useState(false);

  // 解析 URL ?auto_start=1&agent_id=<id> 自动唤起协同录制抽屉
  const urlAutoStart = searchParams.get('auto_start') === '1';

  const handleCloseStartModal = useCallback(() => {
    setStartModalOpen(false);
    if (searchParams.get('auto_start')) {
      setSearchParams((prev) => {
        const next = new URLSearchParams(prev);
        next.delete('auto_start');
        return next;
      }, { replace: true });
    }
  }, [searchParams, setSearchParams]);

  const loadSessions = useCallback(async () => {
    setLoading(true);
    try {
      const res = await trafficApi.listRecordingSessions({
        scope: urlScope ? (urlScope as 'SHARED' | 'PRIVATE') : undefined,
        search: urlSearch || undefined,
        page,
        page_size: pageSize,
      });
      setSessions(res.items ?? []);
      setTotalSessions(res.total ?? 0);

      // 检索当前是否有处于 RECORDING 态的活跃批次
      const rec = (res.items ?? []).find((s) => s.status === 'RECORDING');
      if (rec) {
        setActiveSession(rec);
      } else if (!urlScope && !urlSearch) {
        // 如果当前页没有，但在全量无过滤时查一次 RECORDING 批次
        const activeRes = await trafficApi.listRecordingSessions({
          status: 'RECORDING',
          page_size: 1,
        });
        if (activeRes.items && activeRes.items.length > 0) {
          setActiveSession(activeRes.items[0]);
        } else {
          setActiveSession(null);
        }
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '加载录制素材列表失败');
    } finally {
      setLoading(false);
    }
  }, [urlScope, urlSearch, page, pageSize, toast]);

  useEffect(() => {
    void loadSessions();
  }, [loadSessions]);

  // 加载进程与 Agent 基础元数据
  const loadMeta = useCallback(async () => {
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
  }, []);

  useEffect(() => {
    void loadMeta();
  }, [loadMeta]);

  useEffect(() => {
    if (urlAutoStart) {
      void loadMeta();
      setStartModalOpen(true);
    }
  }, [urlAutoStart, loadMeta]);

  const handleStopSession = async (sessionId: string) => {
    setStopping(true);
    try {
      await trafficApi.stopRecordingSession(sessionId);
      toast.success('协同录制已停止，正在排空聚合并释放排他租约');
      void loadSessions();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '停止录制失败');
    } finally {
      setStopping(false);
    }
  };

  const handlePublishSession = async (sessionId: string) => {
    try {
      await trafficApi.publishRecordingSession(sessionId);
      toast.success('私有素材已成功晋升为团队公共素材');
      void loadSessions();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '发布素材失败');
    }
  };

  const handleDeleteSession = async (sessionId: string) => {
    try {
      await trafficApi.deleteRecordingSession(sessionId);
      toast.success('录制批次已彻底删除');
      void loadSessions();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '删除录制批次失败');
    }
  };

  const handleScopeChange = (nextScope: string) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      if (nextScope) next.set('scope', nextScope);
      else next.delete('scope');
      next.set('page', '1');
      return next;
    });
  };

  const handleSearchChange = (nextSearch: string) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      if (nextSearch) next.set('search', nextSearch);
      else next.delete('search');
      next.set('page', '1');
      return next;
    });
  };

  return (
    <div className="code-workbench-page code-recording-workbench" data-testid="recording-workbench-page">
      <div className="code-workbench-header">
        <div className="code-workbench-title-group">
          <h2 className="code-workbench-title">流量录制素材工作台</h2>
          <span className="code-workbench-subtitle">
            管理全生命周期协同录制批次，下发 Agent 排他租约并沉淀公共/私有流量素材资产
          </span>
        </div>

        <div className="code-header-actions">
          <button
            type="button"
            className="code-btn code-btn--primary"
            onClick={() => {
              void loadMeta();
              setStartModalOpen(true);
            }}
            data-testid="btn-open-start-recording"
          >
            + 发起协同录制
          </button>
        </div>
      </div>

      {/* 活跃录制实时监控横幅 */}
      <ActiveRecordingBanner
        session={activeSession}
        onStop={handleStopSession}
        stopping={stopping}
      />

      {/* 录制素材台账表格 */}
      <div className="code-mt-md">
        <RecordingSessionTable
          sessions={sessions}
          total={totalSessions}
          page={page}
          pageSize={pageSize}
          onPageChange={setPage}
          onPageSizeChange={setPageSize}
          scopeFilter={urlScope}
          onScopeFilterChange={handleScopeChange}
          search={urlSearch}
          onSearchChange={handleSearchChange}
          currentUserId={user?.id}
          isAdmin={isAdmin}
          onPublish={handlePublishSession}
          onDelete={handleDeleteSession}
          loading={loading}
        />
      </div>

      {/* 发起多进程协同录制抽屉 */}
      <RecordingStartDrawer
        open={startModalOpen}
        onClose={handleCloseStartModal}
        onSuccess={() => {
          handleCloseStartModal();
          void loadSessions();
        }}
        allProcesses={allProcesses}
        allAgents={allAgents}
        isAdmin={isAdmin}
      />
    </div>
  );
};

export default RecordingWorkbenchPage;
