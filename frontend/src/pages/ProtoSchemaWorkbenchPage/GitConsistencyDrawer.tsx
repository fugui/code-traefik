import React, { useEffect, useState } from 'react';
import { CheckCircle2, FileDiff, GitCommitHorizontal, RefreshCw, ShieldAlert } from 'lucide-react';
import { Drawer } from '@code/common';
import * as schemaApi from '../../api/schema';
import { useToast } from '../../utils/toast';
import type { GitConsistencyReportDTO, GitRepoDTO } from '../../types';
import './ProtoDocsAndCodeViewer.css';

export interface GitConsistencyDrawerProps {
  open: boolean;
  isAdmin: boolean;
  repo: GitRepoDTO | null;
  selectedBundleId?: number | null;
  initialReport?: GitConsistencyReportDTO | null;
  onClose: () => void;
  onTriggerSync?: (branch: string, bundleId?: number | null) => Promise<void> | void;
}

function renderStatusBadge(status: string) {
  switch (status) {
    case 'in_sync':
      return <span className="code-badge code-badge-success">一致 (in_sync)</span>;
    case 'modified':
      return <span className="code-badge code-badge-warning">内容差异 (modified)</span>;
    case 'remote_only':
      return <span className="code-badge code-badge-info">仅远端新增 (remote_only)</span>;
    case 'db_only':
      return <span className="code-badge code-badge-danger">仅本地存在 (db_only)</span>;
    default:
      return <span className="pdoc-pill">{status}</span>;
  }
}

function shortHash(hash?: string): string {
  if (!hash) return '—';
  return hash.slice(0, 12);
}

export const GitConsistencyDrawer: React.FC<GitConsistencyDrawerProps> = ({
  open,
  isAdmin,
  repo,
  selectedBundleId,
  initialReport,
  onClose,
  onTriggerSync,
}) => {
  const toast = useToast();
  const [branch, setBranch] = useState<string>(
    initialReport?.branch || repo?.default_branch || 'main'
  );
  const [bundleId, setBundleId] = useState<number | null>(
    initialReport?.bundle_id ?? selectedBundleId ?? null
  );
  const [branches, setBranches] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const [report, setReport] = useState<GitConsistencyReportDTO | null>(initialReport ?? null);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  useEffect(() => {
    if (initialReport !== undefined) {
      setReport(initialReport);
    }
  }, [initialReport]);

  const repoId = repo?.id;
  const repoDefaultBranch = repo?.default_branch;

  useEffect(() => {
    if (!open || !repoId) return;
    setBranch(repoDefaultBranch || 'main');
    setBundleId(selectedBundleId ?? null);

    // 若 SSR/测试注入了 initialReport，跳过启动自动远程拉取
    if (initialReport) {
      return;
    }

    let cancelled = false;
    const runCheck = async () => {
      setLoading(true);
      setErrorMsg(null);
      try {
        const [branchResp, consistencyResp] = await Promise.allSettled([
          schemaApi.getGitRepoBranches(repoId),
          schemaApi.getGitRepoConsistency(repoId, {
            bundle_id: selectedBundleId ?? undefined,
            branch: repoDefaultBranch || undefined,
          }),
        ]);
        if (cancelled) return;
        if (branchResp.status === 'fulfilled') {
          setBranches(branchResp.value.branches || []);
        }
        if (consistencyResp.status === 'fulfilled') {
          setReport(consistencyResp.value);
        } else {
          const msg = (consistencyResp.reason as Error)?.message || '对账检查失败';
          setErrorMsg(msg);
          setReport(null);
        }
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    };
    void runCheck();
    return () => {
      cancelled = true;
    };
  }, [open, repoId, repoDefaultBranch, selectedBundleId, initialReport]);

  const handleRecheck = async () => {
    if (!repo) return;
    setLoading(true);
    setErrorMsg(null);
    try {
      const next = await schemaApi.getGitRepoConsistency(repo.id, {
        bundle_id: bundleId ?? undefined,
        branch: branch.trim() || undefined,
      });
      setReport(next);
    } catch (err) {
      const msg = (err as Error).message || 'SHA256 只读对账失败';
      setErrorMsg(msg);
      toast.error(msg);
    } finally {
      setLoading(false);
    }
  };

  const handleTriggerSync = async () => {
    if (!repo || !onTriggerSync) return;
    setSyncing(true);
    try {
      await onTriggerSync(branch.trim() || repo.default_branch || 'main', bundleId);
      toast.success('已发起异步同步任务，完成后列表与对账结果将自动刷新');
    } catch (err) {
      toast.error((err as Error).message || '触发同步失败');
    } finally {
      setSyncing(false);
    }
  };

  const headCommit = report?.head_commit_sha || report?.commit_sha || '';

  return (
    <Drawer
      open={open}
      onClose={onClose}
      title={repo ? `Git 远端 SHA256 只读一致性对账：${repo.name}` : 'Git 远端一致性对账'}
      subtitle="仅在内存计算远端树对象与数据库 traefik_proto_files 的 SHA256 指纹差异，零落盘、零写库"
      width={860}
    >
      <div className="pgit-drawer-stack">
        <div className="pdoc-card">
          <div className="pgit-toolbar-row">
            <label className="pgit-inline-field">
              <span>对账分支</span>
              {branches.length > 0 ? (
                <select
                  className="pgit-input"
                  value={branch}
                  onChange={(e) => setBranch(e.target.value)}
                >
                  {branches.map((b) => (
                    <option key={b} value={b}>
                      {b}
                    </option>
                  ))}
                </select>
              ) : (
                <input
                  type="text"
                  className="pgit-input"
                  value={branch}
                  onChange={(e) => setBranch(e.target.value)}
                  placeholder="main"
                />
              )}
            </label>

            <label className="pgit-inline-field">
              <span>Bundle 范围</span>
              <select
                className="pgit-input"
                value={bundleId ?? ''}
                onChange={(e) => setBundleId(e.target.value ? Number(e.target.value) : null)}
              >
                <option value="">全部 Bundles</option>
                {repo?.bundles?.map((b) => (
                  <option key={b.id} value={b.id}>
                    {b.bundle_name} ({b.target_schema_name})
                  </option>
                ))}
              </select>
            </label>

            <button
              type="button"
              className="btn btn-primary"
              disabled={loading || !repo}
              onClick={() => void handleRecheck()}
            >
              <RefreshCw size={14} className={loading ? 'pgit-spin' : undefined} />
              {loading ? '对账计算中...' : '重新对账'}
            </button>

            {isAdmin && onTriggerSync && report && !report.in_sync && (
              <button
                type="button"
                className="btn btn-secondary"
                disabled={syncing}
                onClick={() => void handleTriggerSync()}
              >
                <FileDiff size={14} />
                {syncing ? '已派发...' : '立即同步对齐远端'}
              </button>
            )}
          </div>

          {errorMsg && (
            <div className="pdoc-banner pdoc-banner--deprecated">
              <ShieldAlert size={15} />
              <span>{errorMsg}</span>
            </div>
          )}

          {report && (
            <>
              <div className="pdoc-meta-row">
                <span className="pdoc-pill">
                  <GitCommitHorizontal size={13} /> 远端 HEAD:{' '}
                  <code>{headCommit ? headCommit.slice(0, 7) : '—'}</code>
                </span>
                <span className="pdoc-pill">分支: {report.branch}</span>
                {report.in_sync ? (
                  <span className="code-badge code-badge-success">
                    <CheckCircle2 size={12} /> 完全一致 (In Sync)
                  </span>
                ) : (
                  <span className="code-badge code-badge-warning">存在版本差异</span>
                )}
              </div>

              <div className="pgit-stat-grid">
                <div className="pgit-stat-card">
                  <span className="pgit-stat-card__label">完全一致 (in_sync)</span>
                  <strong className="pgit-stat-card__value">{report.summary.in_sync}</strong>
                </div>
                <div className="pgit-stat-card">
                  <span className="pgit-stat-card__label">内容差异 (modified)</span>
                  <strong className="pgit-stat-card__value pgit-stat-card__value--warn">
                    {report.summary.modified}
                  </strong>
                </div>
                <div className="pgit-stat-card">
                  <span className="pgit-stat-card__label">仅远端新增 (remote_only)</span>
                  <strong className="pgit-stat-card__value pgit-stat-card__value--info">
                    {report.summary.remote_only}
                  </strong>
                </div>
                <div className="pgit-stat-card">
                  <span className="pgit-stat-card__label">仅本地存在 (db_only)</span>
                  <strong className="pgit-stat-card__value pgit-stat-card__value--danger">
                    {report.summary.db_only}
                  </strong>
                </div>
              </div>
            </>
          )}
        </div>

        {report && (
          <div className="pdoc-card">
            <h4 className="pdoc-card__title">逐文件 SHA256 只读明细 ({report.items.length})</h4>
            {report.items.length === 0 ? (
              <p className="pdoc-card__subtitle">当前筛选范围内无可比对的 .proto 文件。</p>
            ) : (
              <table className="code-table">
                <thead>
                  <tr>
                    <th>Bundle</th>
                    <th>文件相对路径</th>
                    <th>对账状态</th>
                    <th>远端 SHA256</th>
                    <th>DB 快照 SHA256</th>
                  </tr>
                </thead>
                <tbody>
                  {report.items.map((item) => (
                    <tr key={`${item.bundle_id}:${item.file_path}`}>
                      <td>
                        <span className="pdoc-pill">{item.bundle_name || item.target_schema_name}</span>
                      </td>
                      <td>
                        <code>{item.file_path}</code>
                      </td>
                      <td>{renderStatusBadge(item.status)}</td>
                      <td>
                        <code>{shortHash(item.remote_sha256)}</code>
                      </td>
                      <td>
                        <code>{shortHash(item.db_sha256)}</code>
                        {item.db_version ? (
                          <span className="pgit-muted"> (v{item.db_version})</span>
                        ) : null}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        )}
      </div>
    </Drawer>
  );
};

export default GitConsistencyDrawer;
