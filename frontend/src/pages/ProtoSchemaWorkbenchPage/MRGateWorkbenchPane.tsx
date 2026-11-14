import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import {
  AlertTriangle,
  CheckCircle2,
  ExternalLink,
  Eye,
  GitMerge,
  GitPullRequest,
  RefreshCw,
  Search,
  ShieldAlert,
  ShieldCheck,
  XCircle,
} from 'lucide-react';
import { Drawer, EmptyState, Modal, Pagination } from '@code/common';
import * as schemaApi from '../../api/schema';
import { subscribeServerEvents } from '../../api/sse';
import type {
  GitRepoDTO,
  MRCheckRunDTO,
  MRCheckRunDetailDTO,
} from '../../types';
import { useToast } from '../../utils/toast';
import SafeMarkdownText from '../../utils/safeMarkdown';

export interface MRGateWorkbenchPaneProps {
  isAdmin: boolean;
  onOpenSourcesTab?: (schemaName?: string) => void;
  onOpenLintCenter?: () => void;
  initialRuns?: MRCheckRunDTO[];
  initialTotal?: number;
  initialDetail?: MRCheckRunDetailDTO | null;
}

/**
 * 判定指定 MR 检查流水是否允许人工特批豁免：
 * 存在 .proto 编译语法或依赖错误（compile_passed=false）时系统绝对禁止人工豁免。
 */
export function canOverrideMRRun(
  run: Pick<MRCheckRunDTO, 'compile_passed' | 'status'>,
  isAdmin: boolean
): boolean {
  if (!isAdmin || !run.compile_passed) {
    return false;
  }
  return run.status === 'FAILED' || run.status === 'WARN';
}

const VALID_PAGE_SIZES = new Set([15, 25, 50, 100]);

function formatMRDate(iso?: string | null): string {
  if (!iso) return '-';
  const dt = new Date(iso);
  if (Number.isNaN(dt.getTime())) return iso;
  return dt.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
}

function getStatusMeta(status: string): { label: string; badgeClass: string } {
  switch (status) {
    case 'PASSED':
      return { label: '通过 (PASSED)', badgeClass: 'code-badge-success' };
    case 'WARN':
      return { label: '告警通过 (WARN)', badgeClass: 'code-badge-warning' };
    case 'FAILED':
      return { label: '门禁阻断 (FAILED)', badgeClass: 'code-badge-danger' };
    case 'OVERRIDDEN':
      return { label: '人工豁免 (OVERRIDDEN)', badgeClass: 'code-badge-info' };
    case 'RUNNING':
      return { label: '检查中 (RUNNING)', badgeClass: 'code-badge-neutral' };
    default:
      return { label: status || '排队中 (PENDING)', badgeClass: 'code-badge-neutral' };
  }
}

export const MRGateWorkbenchPane: React.FC<MRGateWorkbenchPaneProps> = ({
  isAdmin,
  onOpenSourcesTab,
  onOpenLintCenter,
  initialRuns,
  initialTotal,
  initialDetail = null,
}) => {
  const [searchParams, setSearchParams] = useSearchParams();
  const toast = useToast();

  // 1. 从 URL SearchParams 同步分页与筛选条件（保证前进/后退历史导航与通知深链直接可用）
  const rawPage = Number(searchParams.get('mr_page') || searchParams.get('page') || '1');
  const page = Number.isFinite(rawPage) && rawPage > 0 ? Math.floor(rawPage) : 1;

  const rawPageSize = Number(searchParams.get('mr_page_size') || searchParams.get('page_size') || '25');
  const pageSize = VALID_PAGE_SIZES.has(rawPageSize) ? rawPageSize : 25;

  const filterRepoId = searchParams.get('git_repo_id') || '';
  const filterStatus = searchParams.get('mr_status') || '';
  const filterPhase = searchParams.get('event_phase') || '';
  const filterKeyword = searchParams.get('q') || '';
  const runIdParam = searchParams.get('runId') || '';

  const [keywordDraft, setKeywordDraft] = useState(filterKeyword);
  useEffect(() => {
    setKeywordDraft(filterKeyword);
  }, [filterKeyword]);

  const [repos, setRepos] = useState<GitRepoDTO[]>([]);
  const [runs, setRuns] = useState<MRCheckRunDTO[]>(initialRuns ?? []);
  const [total, setTotal] = useState(initialTotal ?? initialRuns?.length ?? 0);
  const [loading, setLoading] = useState(false);
  const [rerunningId, setRerunningId] = useState<number | null>(null);

  // 2. 详情抽屉状态
  const [detailOpen, setDetailOpen] = useState(Boolean(initialDetail));
  const [detailLoading, setDetailLoading] = useState(false);
  const [selectedDetail, setSelectedDetail] = useState<MRCheckRunDetailDTO | null>(initialDetail);
  const activeRunIdRef = useRef<number | null>(initialDetail?.id ?? null);

  // 3. 人工豁免弹窗状态
  const [overrideTarget, setOverrideTarget] = useState<MRCheckRunDTO | null>(null);
  const [overrideReason, setOverrideReason] = useState('');
  const [overriding, setOverriding] = useState(false);

  const updateUrlParams = useCallback(
    (patch: Record<string, string | null>, resetPage = false) => {
      setSearchParams((prev) => {
        const next = new URLSearchParams(prev);
        next.set('tab', 'mr-gates');
        Object.entries(patch).forEach(([k, v]) => {
          if (v === null || v === '') {
            next.delete(k);
          } else {
            next.set(k, v);
          }
        });
        if (resetPage) {
          next.set('mr_page', '1');
        }
        return next;
      });
    },
    [setSearchParams]
  );

  // 加载仓库列表供筛选下拉使用
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const res = await schemaApi.listGitRepos({ page: 1, page_size: 100 });
        if (!cancelled) {
          setRepos(res.items || []);
        }
      } catch {
        // 仓库下拉筛选失败不阻塞 MR 门禁列表主渲染
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const fetchRuns = useCallback(async () => {
    setLoading(true);
    try {
      const parsedRepoId = filterRepoId ? Number(filterRepoId) : undefined;
      const res = await schemaApi.listMRCheckRuns({
        git_repo_id: parsedRepoId && Number.isFinite(parsedRepoId) ? parsedRepoId : undefined,
        status: filterStatus || undefined,
        event_phase: filterPhase || undefined,
        q: filterKeyword || undefined,
        page,
        page_size: pageSize,
      });
      setRuns(res.items || []);
      setTotal(res.total || 0);
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : '加载 MR 门禁流水失败');
    } finally {
      setLoading(false);
    }
  }, [filterRepoId, filterStatus, filterPhase, filterKeyword, page, pageSize, toast]);

  useEffect(() => {
    void fetchRuns();
  }, [fetchRuns]);

  const loadRunDetail = useCallback(
    async (runId: number) => {
      activeRunIdRef.current = runId;
      setDetailOpen(true);
      setDetailLoading(true);
      try {
        const detail = await schemaApi.getMRCheckRunDetail(runId);
        if (activeRunIdRef.current === runId) {
          setSelectedDetail(detail);
        }
      } catch (err: unknown) {
        toast.error(err instanceof Error ? err.message : '加载 MR 门禁详情失败');
      } finally {
        if (activeRunIdRef.current === runId) {
          setDetailLoading(false);
        }
      }
    },
    [toast]
  );

  // 深链参数 ?runId=<id> 自动打开详情抽屉
  useEffect(() => {
    if (!runIdParam) return;
    const numericId = Number(runIdParam);
    if (!Number.isFinite(numericId) || numericId <= 0) return;
    if (activeRunIdRef.current === numericId && detailOpen) return;
    void loadRunDetail(numericId);
  }, [runIdParam, detailOpen, loadRunDetail]);

  const handleOpenDetail = useCallback(
    (runId: number) => {
      updateUrlParams({ runId: String(runId) });
      void loadRunDetail(runId);
    },
    [loadRunDetail, updateUrlParams]
  );

  const handleCloseDetail = useCallback(() => {
    activeRunIdRef.current = null;
    setDetailOpen(false);
    setSelectedDetail(null);
    if (searchParams.get('runId')) {
      updateUrlParams({ runId: null });
    }
  }, [searchParams, updateUrlParams]);

  // 订阅 SSE 实时推送：mr_gate.updated / mr_gate.completed 自动刷新列表与当前详情
  useEffect(() => {
    const unsubscribe = subscribeServerEvents({
      onMessage: (evt) => {
        if (evt.event !== 'mr_gate.updated' && evt.event !== 'mr_gate.completed') {
          return;
        }
        void fetchRuns();
        const payload = evt.data as Partial<MRCheckRunDTO> | null;
        if (payload?.id && activeRunIdRef.current === payload.id && detailOpen) {
          void loadRunDetail(payload.id);
        }
      },
    });
    return () => {
      unsubscribe();
    };
  }, [fetchRuns, loadRunDetail, detailOpen]);

  const handleSearchSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    updateUrlParams({ q: keywordDraft.trim() || null }, true);
  };

  const handleRerun = async (run: MRCheckRunDTO) => {
    if (!isAdmin) return;
    setRerunningId(run.id);
    try {
      await schemaApi.rerunMRCheckRun(run.id);
      toast.success(`已重新触发 MR #${run.mr_iid} 四合一门禁检查`);
      await fetchRuns();
      if (detailOpen && selectedDetail?.id === run.id) {
        await loadRunDetail(run.id);
      }
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : '重跑门禁检查失败');
    } finally {
      setRerunningId(null);
    }
  };

  const handleOpenOverrideModal = (run: MRCheckRunDTO) => {
    if (!isAdmin) return;
    // 编译错误绝不允许人工豁免，前端同步强制拦截提示
    if (!run.compile_passed) {
      toast.error('存在 .proto 编译语法或依赖错误（compile_passed=false），系统禁止人工豁免放行！');
      return;
    }
    setOverrideTarget(run);
    setOverrideReason('');
  };

  const handleConfirmOverride = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!overrideTarget) return;
    if (!overrideTarget.compile_passed) {
      toast.error('存在编译错误，硬禁豁免放行');
      return;
    }
    const trimmed = overrideReason.trim();
    if (trimmed.length < 8) {
      toast.warning('请填写至少 8 个字符的业务变更评审依据或补偿方案说明');
      return;
    }

    setOverriding(true);
    try {
      await schemaApi.overrideMRCheckRun(overrideTarget.id, trimmed);
      toast.success('人工豁免成功，已记录 P1 级安全审计日志并尝试回写远端 MR 状态');
      const targetId = overrideTarget.id;
      setOverrideTarget(null);
      setOverrideReason('');
      await fetchRuns();
      if (detailOpen && selectedDetail?.id === targetId) {
        await loadRunDetail(targetId);
      }
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : '人工豁免提交失败');
    } finally {
      setOverriding(false);
    }
  };

  const summaryCounts = useMemo(() => {
    let blocked = 0;
    let passed = 0;
    let warnedOrOverridden = 0;
    for (const r of runs) {
      if (r.status === 'FAILED') blocked += 1;
      else if (r.status === 'PASSED') passed += 1;
      else if (r.status === 'WARN' || r.status === 'OVERRIDDEN') warnedOrOverridden += 1;
    }
    return { blocked, passed, warnedOrOverridden };
  }, [runs]);

  return (
    <div className="pdoc-card pmrg-workbench" data-testid="mr-gate-workbench">
      {/* 头部标题与跨页入口 */}
      <div className="pdoc-card__header pmrg-header">
        <div>
          <h3 className="pdoc-card__title">Git MR / PR 四合一契约门禁雷达</h3>
          <p className="pdoc-card__subtitle">
            针对合并请求执行「虚拟树增量编译 + 静态词汇合规 + AST Jaccard 防重 + 破坏性变更影响波及」四维审查，支持 Bot 原位评论与安全可控的 P1 审计豁免。
          </p>
        </div>
        <div className="pmrg-header__actions">
          {onOpenLintCenter && (
            <button type="button" className="btn btn-secondary" onClick={onOpenLintCenter}>
              <ShieldCheck size={15} />
              <span>静态规范与词典中心</span>
            </button>
          )}
          {onOpenSourcesTab && (
            <button type="button" className="btn btn-secondary" onClick={() => onOpenSourcesTab()}>
              <GitPullRequest size={15} />
              <span>Git 仓库与源码Diff</span>
            </button>
          )}
          <button
            type="button"
            className="btn btn-secondary"
            onClick={() => void fetchRuns()}
            disabled={loading}
          >
            <RefreshCw size={15} className={loading ? 'pgit-spin' : ''} />
            <span>刷新流水</span>
          </button>
        </div>
      </div>

      {/* 本页快速指标概览条 */}
      <div className="pmrg-summary-strip">
        <div className="pmrg-stat-chip">
          <span className="pmrg-stat-chip__label">累计流水记录</span>
          <strong className="pmrg-stat-chip__val">{total}</strong>
        </div>
        <div className="pmrg-stat-chip">
          <span className="pmrg-stat-chip__label">本页门禁通过</span>
          <strong className="pmrg-stat-chip__val pmrg-stat-chip__val--success">{summaryCounts.passed}</strong>
        </div>
        <div className="pmrg-stat-chip">
          <span className="pmrg-stat-chip__label">本页告警 / 豁免</span>
          <strong className="pmrg-stat-chip__val pmrg-stat-chip__val--warning">
            {summaryCounts.warnedOrOverridden}
          </strong>
        </div>
        <div className="pmrg-stat-chip">
          <span className="pmrg-stat-chip__label">本页门禁阻断</span>
          <strong className="pmrg-stat-chip__val pmrg-stat-chip__val--danger">{summaryCounts.blocked}</strong>
        </div>
      </div>

      {/* 筛选工具栏 */}
      <form className="pmrg-filters" onSubmit={handleSearchSubmit}>
        <select
          className="code-select"
          aria-label="按 Git 仓库筛选"
          value={filterRepoId}
          onChange={(e) => updateUrlParams({ git_repo_id: e.target.value || null }, true)}
        >
          <option value="">全部 Git 仓库</option>
          {repos.map((repo) => (
            <option key={repo.id} value={String(repo.id)}>
              {repo.name}
            </option>
          ))}
        </select>

        <select
          className="code-select"
          aria-label="按门禁状态筛选"
          value={filterStatus}
          onChange={(e) => updateUrlParams({ mr_status: e.target.value || null }, true)}
        >
          <option value="">全部状态</option>
          <option value="FAILED">阻断 (FAILED)</option>
          <option value="WARN">告警通过 (WARN)</option>
          <option value="PASSED">通过 (PASSED)</option>
          <option value="OVERRIDDEN">人工豁免 (OVERRIDDEN)</option>
          <option value="RUNNING">检查中 (RUNNING)</option>
        </select>

        <select
          className="code-select"
          aria-label="按触发阶段筛选"
          value={filterPhase}
          onChange={(e) => updateUrlParams({ event_phase: e.target.value || null }, true)}
        >
          <option value="">全部阶段</option>
          <option value="MR_GATE">MR 审查阶段 (MR_GATE)</option>
          <option value="MAIN_MERGE">主干合入阶段 (MAIN_MERGE)</option>
        </select>

        <div className="pmrg-search-box">
          <input
            type="text"
            className="code-input"
            aria-label="搜索 MR 标题、分支、作者或 Commit"
            placeholder="搜索 MR 标题 / 分支 / 作者 / Commit SHA..."
            value={keywordDraft}
            onChange={(e) => setKeywordDraft(e.target.value)}
          />
          <button type="submit" className="btn btn-secondary">
            <Search size={14} />
            <span>搜索</span>
          </button>
        </div>
      </form>

      {/* MR 四合一门禁列表 */}
      {loading && runs.length === 0 ? (
        <EmptyState compact title="正在加载 Git MR 四合一门禁流水..." />
      ) : runs.length === 0 ? (
        <EmptyState
          compact
          title="暂无 Git MR 门禁检查记录"
          description="配置 GitLab / GitHub / Gitea Webhook 指向 /api/webhooks/git-mr/:repoId 后，每次提交 MR/PR 与合入主干都将自动生成四合一合规检查流水。"
        />
      ) : (
        <div className="pmrg-table-wrap">
          <table className="pmrg-table">
            <thead>
              <tr>
                <th>MR / 合入流水与分支</th>
                <th>四合一质量维度诊断</th>
                <th>总门禁判定</th>
                <th>耗时与执行时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {runs.map((run) => {
                const st = getStatusMeta(run.status);
                const canOverride = canOverrideMRRun(run, isAdmin);
                const blockedByCompile = isAdmin && !run.compile_passed && run.status === 'FAILED';

                return (
                  <tr key={run.id} data-testid={`mr-run-row-${run.id}`}>
                    <td>
                      <div className="pmrg-mr-cell">
                        <div className="pmrg-mr-cell__top">
                          <span className="code-badge code-badge-neutral">
                            {run.event_phase === 'MAIN_MERGE' ? (
                              <>
                                <GitMerge size={12} />
                                <span>主干合入</span>
                              </>
                            ) : (
                              <>
                                <GitPullRequest size={12} />
                                <span>!{run.mr_iid}</span>
                              </>
                            )}
                          </span>
                          <button
                            type="button"
                            className="pmrg-title-link"
                            onClick={() => handleOpenDetail(run.id)}
                          >
                            {run.mr_title || `${run.target_schema_name || 'Schema'} 变更检查`}
                          </button>
                          {run.mr_url && (
                            <a
                              href={run.mr_url}
                              target="_blank"
                              rel="noreferrer"
                              className="pmrg-ext-link"
                              title="打开远端 MR/PR"
                            >
                              <ExternalLink size={12} />
                            </a>
                          )}
                        </div>
                        <div className="pmrg-mr-cell__meta">
                          <span>{run.git_repo_name || `Repo #${run.git_repo_id}`}</span>
                          {run.target_schema_name && (
                            <code className="code-tpl-code-inline">{run.target_schema_name}</code>
                          )}
                          <span>
                            {run.source_branch} → {run.target_branch}
                          </span>
                          <code className="pgit-commit-pill">{run.head_commit_sha.slice(0, 8)}</code>
                          <span>作者: {run.author_username || '-'}</span>
                        </div>
                      </div>
                    </td>

                    <td>
                      <div className="pmrg-matrix-pills">
                        <span
                          className={`pmrg-dim-pill ${
                            run.compile_passed ? 'pmrg-dim-pill--ok' : 'pmrg-dim-pill--fail'
                          }`}
                        >
                          {run.compile_passed ? '编译通过' : '编译失败'}
                        </span>
                        <span
                          className={`pmrg-dim-pill ${
                            run.lint_error_count > 0
                              ? 'pmrg-dim-pill--fail'
                              : run.lint_warning_count > 0
                                ? 'pmrg-dim-pill--warn'
                                : 'pmrg-dim-pill--ok'
                          }`}
                        >
                          规范 E:{run.lint_error_count} / W:{run.lint_warning_count}
                        </span>
                        <span
                          className={`pmrg-dim-pill ${
                            run.duplicate_group_count > 0 ? 'pmrg-dim-pill--warn' : 'pmrg-dim-pill--ok'
                          }`}
                        >
                          查重 {run.duplicate_group_count} 簇
                        </span>
                        <span
                          className={`pmrg-dim-pill ${
                            run.breaking_change_count > 0 ? 'pmrg-dim-pill--fail' : 'pmrg-dim-pill--ok'
                          }`}
                        >
                          破坏性 {run.breaking_change_count} · 波及 {run.affected_modules_count} 模块
                        </span>
                      </div>
                    </td>

                    <td>
                      <div className="pmrg-status-col">
                        <span className={`code-badge ${st.badgeClass}`}>{st.label}</span>
                        {run.remote_comment_id && (
                          <span className="pgit-muted" title={`远端 Bot 评论 ID: ${run.remote_comment_id}`}>
                            Bot评论 #{run.remote_comment_id}
                          </span>
                        )}
                        {run.status === 'OVERRIDDEN' && run.overridden_by_name && (
                          <span className="pgit-muted">豁免人: {run.overridden_by_name}</span>
                        )}
                      </div>
                    </td>

                    <td>
                      <div className="pmrg-time-col">
                        <span>{run.duration_ms > 0 ? `${run.duration_ms} ms` : '-'}</span>
                        <span className="pgit-muted">{formatMRDate(run.completed_at || run.created_at)}</span>
                      </div>
                    </td>

                    <td>
                      <div className="pmrg-row-actions">
                        <button
                          type="button"
                          className="btn btn-secondary btn-sm"
                          onClick={() => handleOpenDetail(run.id)}
                        >
                          <Eye size={13} />
                          <span>详情</span>
                        </button>

                        {isAdmin && (
                          <button
                            type="button"
                            className="btn btn-secondary btn-sm"
                            disabled={rerunningId === run.id}
                            onClick={() => void handleRerun(run)}
                          >
                            <RefreshCw size={13} className={rerunningId === run.id ? 'pgit-spin' : ''} />
                            <span>重跑</span>
                          </button>
                        )}

                        {canOverride && (
                          <button
                            type="button"
                            className="btn btn-primary btn-sm"
                            onClick={() => handleOpenOverrideModal(run)}
                          >
                            <ShieldAlert size={13} />
                            <span>豁免放行</span>
                          </button>
                        )}

                        {blockedByCompile && (
                          <button
                            type="button"
                            className="btn btn-secondary btn-sm"
                            disabled
                            title="存在 .proto 编译错误（compile_passed=false），系统硬禁人工豁免放行"
                            data-testid={`override-forbidden-${run.id}`}
                          >
                            <XCircle size={13} />
                            <span>不可豁免</span>
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {/* 统一分页栏 */}
      {total > 0 && (
        <div className="pmrg-pagination">
          <Pagination
            page={page}
            pageSize={pageSize}
            totalItems={total}
            onPageChange={(nextPage) => updateUrlParams({ mr_page: String(nextPage) })}
            onPageSizeChange={(nextSize) =>
              updateUrlParams({ mr_page_size: String(nextSize), mr_page: '1' })
            }
            pageSizeOptions={[15, 25, 50, 100]}
          />
        </div>
      )}

      {/* MR 门禁四合一诊断详情抽屉 */}
      <Drawer
        open={detailOpen}
        onClose={handleCloseDetail}
        title={
          selectedDetail
            ? `MR !${selectedDetail.mr_iid} 四合一契约体检报告 · ${selectedDetail.mr_title}`
            : 'MR 门禁检查详情'
        }
        width="xl"
      >
        {detailLoading ? (
          <EmptyState compact title="正在加载四合一检查报告明细..." />
        ) : !selectedDetail ? (
          <EmptyState compact title="未找到检查报告" />
        ) : (
          <div className="pmrg-detail-body" data-testid="mr-run-detail-drawer">
            {/* 顶部元信息与审批动作栏 */}
            <div className="pmrg-detail-summary">
              <div className="pmrg-detail-summary__badges">
                <span className={`code-badge ${getStatusMeta(selectedDetail.status).badgeClass}`}>
                  {getStatusMeta(selectedDetail.status).label}
                </span>
                <span className="code-badge code-badge-neutral">{selectedDetail.event_phase}</span>
                <code className="pgit-commit-pill">head: {selectedDetail.head_commit_sha.slice(0, 10)}</code>
                {selectedDetail.remote_comment_id && (
                  <span className="pgit-muted">远端 Bot 原位评论 ID: {selectedDetail.remote_comment_id}</span>
                )}
              </div>

              <div className="pmrg-detail-summary__actions">
                {isAdmin &&
                  selectedDetail.compile_passed &&
                  (selectedDetail.status === 'FAILED' || selectedDetail.status === 'WARN') && (
                    <button
                      type="button"
                      className="btn btn-primary btn-sm"
                      onClick={() => handleOpenOverrideModal(selectedDetail)}
                    >
                      <ShieldAlert size={14} />
                      <span>Approve with Override (人工豁免)</span>
                    </button>
                  )}
                {isAdmin && !selectedDetail.compile_passed && (
                  <span className="pmrg-compile-hard-lock" data-testid="detail-compile-override-lock">
                    <XCircle size={14} />
                    <span>编译错误硬禁豁免（ERR_MR_OVERRIDE_FORBIDDEN_COMPILE_FAILED）</span>
                  </span>
                )}
              </div>
            </div>

            {/* 人工豁免审计记录回显 */}
            {selectedDetail.status === 'OVERRIDDEN' && (
              <div className="pmrg-callout pmrg-callout--info">
                <ShieldCheck size={16} />
                <div>
                  <strong>已完成人工特批豁免放行（P1 审计已记录）</strong>
                  <p>
                    操作人：{selectedDetail.overridden_by_name || `User #${selectedDetail.overridden_by || '-'}`} · 理由：
                    {selectedDetail.override_reason}
                  </p>
                </div>
              </div>
            )}

            {/* 1. 虚拟树编译与语法检查结果 */}
            {!selectedDetail.compile_passed && (
              <div className="pmrg-callout pmrg-callout--danger">
                <XCircle size={16} />
                <div>
                  <strong>虚拟树补丁合并编译失败（硬阻断，禁止豁免）</strong>
                  <pre className="pmrg-compile-pre">
                    {selectedDetail.compile_error_text || '契约编译校验未通过，请查看下方细节并修复语法/导入错误'}
                  </pre>
                </div>
              </div>
            )}

            {/* 2. 破坏性变更与下游影响波及雷达 */}
            <section className="pmrg-detail-section">
              <h4 className="pmrg-detail-section__title">
                破坏性变更 ({selectedDetail.breaking_details?.length || 0}) 与下游受波及模块 (
                {selectedDetail.affected_modules?.length || 0})
              </h4>

              {(selectedDetail.breaking_details?.length || 0) === 0 ? (
                <div className="pmrg-ok-line">
                  <CheckCircle2 size={15} />
                  <span>未检测到 Wire 级向后不兼容或字段删除/类型破坏性变更。</span>
                </div>
              ) : (
                <ul className="pmrg-violation-list">
                  {selectedDetail.breaking_details.map((brk, idx) => (
                    <li key={`${brk.rule}-${brk.symbol}-${idx}`} className="pmrg-violation-item pmrg-violation-item--danger">
                      <div className="pmrg-violation-item__head">
                        <span className="code-badge code-badge-danger">{brk.rule}</span>
                        <code className="code-tpl-code-inline">{brk.proto_file}</code>
                        {brk.symbol && <code className="code-tpl-code-inline">{brk.symbol}</code>}
                      </div>
                      <p className="pmrg-violation-item__msg">{brk.message}</p>
                    </li>
                  ))}
                </ul>
              )}

              {(selectedDetail.affected_modules?.length || 0) > 0 && (
                <div className="pmrg-affected-modules" data-testid="mr-affected-modules">
                  <div className="pmrg-affected-modules__label">
                    <AlertTriangle size={14} />
                    <span>下游受波及架构模块（已自动触发站内通知与跨机提醒）：</span>
                  </div>
                  <div className="pmrg-affected-chips">
                    {selectedDetail.affected_modules.map((mod) => (
                      <div key={`${mod.module_id}-${mod.module_code}`} className="pmrg-module-chip">
                        <strong>{mod.module_name}</strong>
                        <span className="pgit-muted">({mod.module_code})</span>
                        <span>· 负责人: {mod.owner_name || '未指定'}</span>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </section>

            {/* 3. 静态词汇与注释规范检查明细 */}
            <section className="pmrg-detail-section">
              <h4 className="pmrg-detail-section__title">
                静态规范合规违规明细 ({selectedDetail.violations?.length || 0})
              </h4>
              {(selectedDetail.violations?.length || 0) === 0 ? (
                <div className="pmrg-ok-line">
                  <CheckCircle2 size={15} />
                  <span>所有静态规范与词表校验均已通过。</span>
                </div>
              ) : (
                <ul className="pmrg-violation-list">
                  {selectedDetail.violations.map((v) => (
                    <li
                      key={v.id}
                      className={`pmrg-violation-item ${
                        v.severity === 'ERROR' ? 'pmrg-violation-item--danger' : 'pmrg-violation-item--warn'
                      }`}
                    >
                      <div className="pmrg-violation-item__head">
                        <span
                          className={`code-badge ${
                            v.severity === 'ERROR' ? 'code-badge-danger' : 'code-badge-warning'
                          }`}
                        >
                          {v.severity} · {v.rule_code}
                        </span>
                        <code className="code-tpl-code-inline">
                          {v.proto_file}:{v.file_line}
                        </code>
                        {v.symbol_path && <span className="pgit-muted">{v.symbol_path}</span>}
                      </div>
                      <p className="pmrg-violation-item__msg">{v.message}</p>
                      {v.suggestion && (
                        <div className="pmrg-violation-item__suggestion">
                          <SafeMarkdownText content={`建议：${v.suggestion}`} />
                        </div>
                      )}
                    </li>
                  ))}
                </ul>
              )}
            </section>

            {/* 4. AST Jaccard 重复结构体检查 */}
            <section className="pmrg-detail-section">
              <h4 className="pmrg-detail-section__title">
                跨包 AST 结构体防重警告 ({selectedDetail.duplicate_groups?.length || 0})
              </h4>
              {(selectedDetail.duplicate_groups?.length || 0) === 0 ? (
                <div className="pmrg-ok-line">
                  <CheckCircle2 size={15} />
                  <span>未检测到高相似度的跨模块重复 Message 定义。</span>
                </div>
              ) : (
                <div className="pmrg-dup-groups">
                  {selectedDetail.duplicate_groups.map((grp) => (
                    <div key={grp.cluster_hash} className="pmrg-dup-card">
                      <div className="pmrg-dup-card__head">
                        <span className="code-badge code-badge-warning">
                          相似度 {(grp.similarity * 100).toFixed(1)}%
                        </span>
                        <span className="pgit-muted">{grp.suggestion}</span>
                      </div>
                      <ul className="pmrg-dup-members">
                        {grp.members.map((m, idx) => (
                          <li key={`${m.proto_file}-${m.message}-${idx}`}>
                            <code className="code-tpl-code-inline">
                              {m.proto_file}:{m.file_line}
                            </code>{' '}
                            <strong>{m.message}</strong> ({m.field_cnt} fields)
                          </li>
                        ))}
                      </ul>
                    </div>
                  ))}
                </div>
              )}
            </section>

            {/* 5. 变更 .proto 文件列表 */}
            {(selectedDetail.changed_proto_files?.length || 0) > 0 && (
              <section className="pmrg-detail-section">
                <h4 className="pmrg-detail-section__title">
                  本次 MR 变更的 .proto 契约文件 ({selectedDetail.changed_proto_files.length})
                </h4>
                <div className="pmrg-file-chips">
                  {selectedDetail.changed_proto_files.map((fp) => (
                    <code key={fp} className="code-tpl-code-inline">
                      {fp}
                    </code>
                  ))}
                </div>
              </section>
            )}
          </div>
        )}
      </Drawer>

      {/* Approve with Override 审批弹窗 */}
      <Modal
        open={Boolean(overrideTarget)}
        onClose={() => {
          setOverrideTarget(null);
          setOverrideReason('');
        }}
        title="Approve with Override · 人工特批豁免门禁"
        width="md"
        footer={null}
      >
        {overrideTarget && (
          <form onSubmit={(e) => void handleConfirmOverride(e)} className="pmrg-override-form">
            <div className="pmrg-callout pmrg-callout--warning">
              <ShieldAlert size={16} />
              <div>
                <strong>高危治理动作提示（P1 安全审计）</strong>
                <p>
                  本次豁免将同步更新 Git MR 状态为 SUCCESS 并允许继续合入，操作人、MR 信息与审批理由将永久写入平台 P1 级安全审计日志（禁止豁免编译语法错误）。
                </p>
              </div>
            </div>

            <div className="pmrg-override-meta">
              <span>
                目标流水：MR !{overrideTarget.mr_iid} · {overrideTarget.mr_title}
              </span>
              <span>
                破坏性变更：{overrideTarget.breaking_change_count} 项 · 规范错误：{overrideTarget.lint_error_count} 项
              </span>
            </div>

            <label className="pmrg-form-label" htmlFor="mr-override-reason-input">
              审批豁免依据与补偿兼容说明（必填，至少 8 个字符）
            </label>
            <textarea
              id="mr-override-reason-input"
              className="code-input pmrg-override-textarea"
              rows={4}
              required
              minLength={8}
              placeholder="例如：架构委员会已评审确认下游网关与消费者均已双写适配，随灰度发版切换..."
              value={overrideReason}
              onChange={(e) => setOverrideReason(e.target.value)}
            />

            <div className="pmrg-modal-footer">
              <button
                type="button"
                className="btn btn-secondary"
                onClick={() => {
                  setOverrideTarget(null);
                  setOverrideReason('');
                }}
              >
                取消
              </button>
              <button type="submit" className="btn btn-primary" disabled={overriding}>
                {overriding ? '提交豁免中...' : '确认特批豁免 (写 P1 审计)'}
              </button>
            </div>
          </form>
        )}
      </Modal>
    </div>
  );
};

export default MRGateWorkbenchPane;
