import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useSearchParams } from 'react-router-dom';
import {
  CheckCircle2,
  Code2,
  FileDiff,
  FolderGit2,
  GitBranch,
  GitCommitHorizontal,
  Layers,
  Plus,
  RefreshCw,
  Search,
  Settings2,
  ShieldCheck,
} from 'lucide-react';
import { Pagination, usePaginationState } from '@code/common';
import * as schemaApi from '../../api/schema';
import { subscribeServerEvents } from '../../api/sse';
import { useToast } from '../../utils/toast';
import type {
  GitConsistencyReportDTO,
  GitProtoBundleDTO,
  GitRepoDTO,
  GitSyncCompletedEventPayload,
  ProtoFileContentResponse,
  ProtoFileDTO,
  ProtoFileDiffResponse,
  ProtoFileVersionSummaryDTO,
} from '../../types';
import { ProtoCommitDiffView } from './ProtoCommitDiffView';
import { ProtoReadOnlyCodeViewer } from './ProtoReadOnlyCodeViewer';
import { GitConsistencyDrawer } from './GitConsistencyDrawer';
import { GitRepoConfigDrawer } from './GitRepoConfigDrawer';
import './ProtoDocsAndCodeViewer.css';

export interface GitSourcesWorkbenchPaneProps {
  isAdmin?: boolean;
  initialSchemaName?: string;
  initialFileId?: number;
  initialHighlightLine?: number;
  initialGitRepos?: GitRepoDTO[];
  initialSelectedRepoId?: number | null;
  initialSelectedBundleId?: number | null;
  initialFiles?: ProtoFileDTO[];
  initialTotal?: number;
  initialSelectedFileId?: number | null;
  initialContent?: ProtoFileContentResponse | null;
  initialVersions?: ProtoFileVersionSummaryDTO[];
  initialManageOpen?: boolean;
  initialViewMode?: 'code' | 'diff';
  initialDiff?: ProtoFileDiffResponse | null;
  initialConsistencyOpen?: boolean;
  initialConsistencyReport?: GitConsistencyReportDTO | null;
}

function parseHashLine(hash: string): number {
  const matched = /^#L(\d+)$/i.exec(hash.trim());
  if (!matched) return 1;
  const parsed = Number(matched[1]);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 1;
}

function parsePositiveParam(raw: string | null): number | null {
  if (!raw) return null;
  const num = Number(raw);
  return Number.isFinite(num) && num > 0 ? num : null;
}

function renderRepoSyncBadge(status?: string) {
  switch (status) {
    case 'SUCCESS':
      return <span className="code-badge code-badge-success">同步成功</span>;
    case 'RUNNING':
      return <span className="code-badge code-badge-info">同步中...</span>;
    case 'FAILED':
      return <span className="code-badge code-badge-danger">同步失败</span>;
    default:
      return <span className="pdoc-pill">{status || 'IDLE'}</span>;
  }
}

export const GitSourcesWorkbenchPane: React.FC<GitSourcesWorkbenchPaneProps> = ({
  isAdmin = false,
  initialSchemaName = '',
  initialFileId,
  initialHighlightLine,
  initialGitRepos,
  initialSelectedRepoId,
  initialSelectedBundleId,
  initialFiles,
  initialTotal,
  initialSelectedFileId,
  initialContent,
  initialVersions,
  initialManageOpen = false,
  initialViewMode = 'code',
  initialDiff = null,
  initialConsistencyOpen = false,
  initialConsistencyReport = null,
}) => {
  const [searchParams, setSearchParams] = useSearchParams();
  const location = useLocation();
  const toast = useToast();

  const { page, pageSize, setPage, setPageSize } = usePaginationState({
    defaultPageSize: 25,
  });

  const urlRepoId = parsePositiveParam(searchParams.get('repo_id'));
  const urlBundleId = parsePositiveParam(searchParams.get('bundle_id'));
  const urlFileId = parsePositiveParam(searchParams.get('file_id'));
  const hashLine = parseHashLine(location.hash);

  // 1. 仓库与 Monorepo Bundle 状态
  const [gitRepos, setGitRepos] = useState<GitRepoDTO[]>(initialGitRepos ?? []);
  const [reposLoading, setReposLoading] = useState<boolean>(initialGitRepos === undefined);
  const [selectedRepoId, setSelectedRepoId] = useState<number | null>(
    urlRepoId ?? initialSelectedRepoId ?? (initialGitRepos?.[0]?.id ?? null)
  );
  const [selectedBundleId, setSelectedBundleId] = useState<number | null>(
    urlBundleId ?? initialSelectedBundleId ?? null
  );

  // 2. 文件列表、分页与筛选状态
  const [files, setFiles] = useState<ProtoFileDTO[]>(initialFiles ?? []);
  const [filesTotal, setFilesTotal] = useState<number>(initialTotal ?? (initialFiles?.length ?? 0));
  const [filesLoading, setFilesLoading] = useState<boolean>(initialFiles === undefined);
  const [searchText, setSearchText] = useState<string>(initialSchemaName);
  const [includeDeleted, setIncludeDeleted] = useState<boolean>(false);

  // 3. 选中文件、版本、高亮行与 Diff 视图状态
  const resolvedInitialFileId = urlFileId ?? initialSelectedFileId ?? initialFileId ?? (initialFiles?.[0]?.id ?? null);
  const resolvedInitialLine =
    hashLine > 1 ? hashLine : (initialHighlightLine ?? 1);

  const [selectedFileId, setSelectedFileId] = useState<number | null>(resolvedInitialFileId);
  const [highlightLine, setHighlightLine] = useState<number>(resolvedInitialLine);
  const [contentData, setContentData] = useState<ProtoFileContentResponse | null>(initialContent ?? null);
  const [contentLoading, setContentLoading] = useState<boolean>(false);
  const [versions, setVersions] = useState<ProtoFileVersionSummaryDTO[]>(initialVersions ?? []);
  const [selectedVersion, setSelectedVersion] = useState<number | undefined>(
    initialContent?.version?.version
  );

  const [viewMode, setViewMode] = useState<'code' | 'diff'>(initialViewMode);
  const [fromVersion, setFromVersion] = useState<number>(
    initialDiff?.from_version?.version ??
      (initialVersions && initialVersions.length > 1 ? initialVersions[1].version : 1)
  );
  const [toVersion, setToVersion] = useState<number>(
    initialDiff?.to_version?.version ?? (initialVersions?.[0]?.version ?? 1)
  );
  const [diffData, setDiffData] = useState<ProtoFileDiffResponse | null>(initialDiff);
  const [diffLoading, setDiffLoading] = useState<boolean>(false);

  // 4. 抽屉与同步运行状态
  const [manageDrawerOpen, setManageDrawerOpen] = useState<boolean>(initialManageOpen);
  const [manageCreatingNew, setManageCreatingNew] = useState<boolean>(false);
  const [consistencyDrawerOpen, setConsistencyDrawerOpen] = useState<boolean>(initialConsistencyOpen);
  const [syncDispatching, setSyncDispatching] = useState<boolean>(false);

  // 防止 SSR / 单测注入的初始数据在首次挂载时被无意义空请求覆盖
  const skipInitialReposFetchRef = useRef<boolean>(initialGitRepos !== undefined);
  const skipInitialFilesFetchRef = useRef<boolean>(initialFiles !== undefined);
  const skipInitialContentFetchRef = useRef<boolean>(initialContent !== undefined);

  const selectedRepo = useMemo(
    () => gitRepos.find((r) => r.id === selectedRepoId) ?? null,
    [gitRepos, selectedRepoId]
  );

  const selectedBundle: GitProtoBundleDTO | null = useMemo(() => {
    if (!selectedRepo || !selectedBundleId) return null;
    return selectedRepo.bundles?.find((b) => b.id === selectedBundleId) ?? null;
  }, [selectedRepo, selectedBundleId]);

  const normalizedViolations = useMemo(
    () =>
      (contentData?.violations ?? []).map((v) => ({
        ...v,
        line: v.line && v.line > 0 ? v.line : (v.file_line ?? 1),
      })),
    [contentData?.violations]
  );

  // URL 深链同步：当外部从 Docs 点击 ?tab=sources&file_id=xxx#L42 切入时响应变更
  const prevUrlFileIdRef = useRef<number | null>(urlFileId);
  useEffect(() => {
    if (urlFileId && urlFileId !== prevUrlFileIdRef.current) {
      prevUrlFileIdRef.current = urlFileId;
      skipInitialContentFetchRef.current = false;
      setSelectedFileId(urlFileId);
    }
    if (hashLine > 1 || location.hash === '#L1') {
      setHighlightLine(hashLine);
    }
  }, [urlFileId, hashLine, location.hash]);

  const loadGitRepos = useCallback(async () => {
    setReposLoading(true);
    try {
      const resp = await schemaApi.listGitRepos({ page: 1, page_size: 100 });
      const items = resp.items ?? [];
      setGitRepos(items);
      if (items.length > 0 && selectedRepoId === null && !urlFileId) {
        setSelectedRepoId(items[0].id);
      }
    } catch (err) {
      toast.error((err as Error).message || '加载 Git 仓库列表失败');
    } finally {
      setReposLoading(false);
    }
  }, [selectedRepoId, urlFileId, toast]);

  const loadFiles = useCallback(async () => {
    setFilesLoading(true);
    try {
      const resp = await schemaApi.listProtoFiles({
        git_repo_id: selectedRepoId ?? undefined,
        bundle_id: selectedBundleId ?? undefined,
        include_deleted: includeDeleted,
        search: searchText.trim() || undefined,
        page,
        page_size: pageSize,
      });
      const items = resp.items ?? [];
      setFiles(items);
      setFilesTotal(resp.total ?? items.length);

      if (items.length > 0) {
        setSelectedFileId((prev) => {
          if (prev && items.some((f) => f.id === prev)) {
            return prev;
          }
          if (urlFileId && prev === urlFileId) {
            return prev;
          }
          return items[0].id;
        });
      } else if (!urlFileId) {
        setSelectedFileId(null);
      }
    } catch (err) {
      toast.error((err as Error).message || '加载源码文件树失败');
    } finally {
      setFilesLoading(false);
    }
  }, [selectedRepoId, selectedBundleId, includeDeleted, searchText, page, pageSize, urlFileId, toast]);

  const loadFileDetail = useCallback(
    async (fileId: number, versionOverride?: number) => {
      setContentLoading(true);
      try {
        const [contentResp, versionsResp] = await Promise.all([
          schemaApi.getProtoFileContent(fileId, versionOverride),
          schemaApi.listProtoFileVersions(fileId),
        ]);
        setContentData(contentResp);
        const verItems = versionsResp.items ?? [];
        setVersions(verItems);
        setSelectedVersion(contentResp.version?.version ?? versionOverride);
        if (verItems.length >= 2) {
          setToVersion(verItems[0].version);
          setFromVersion(verItems[1].version);
        } else if (verItems.length === 1) {
          setToVersion(verItems[0].version);
          setFromVersion(verItems[0].version);
        }
      } catch (err) {
        toast.error((err as Error).message || '加载源码与版本明细失败');
      } finally {
        setContentLoading(false);
      }
    },
    [toast]
  );

  useEffect(() => {
    if (skipInitialReposFetchRef.current) {
      skipInitialReposFetchRef.current = false;
      return;
    }
    void loadGitRepos();
  }, [loadGitRepos]);

  useEffect(() => {
    if (skipInitialFilesFetchRef.current) {
      skipInitialFilesFetchRef.current = false;
      return;
    }
    void loadFiles();
  }, [loadFiles]);

  useEffect(() => {
    if (!selectedFileId) {
      return;
    }
    if (skipInitialContentFetchRef.current) {
      skipInitialContentFetchRef.current = false;
      return;
    }
    void loadFileDetail(selectedFileId, selectedVersion);
  }, [selectedFileId, selectedVersion, loadFileDetail]);

  useEffect(() => {
    if (viewMode !== 'diff' || !selectedFileId || fromVersion <= 0 || toVersion <= 0) {
      return;
    }
    if (initialDiff && fromVersion === initialDiff.from_version.version && toVersion === initialDiff.to_version.version && selectedFileId === initialDiff.file.id) {
      return;
    }
    let cancelled = false;
    const runDiff = async () => {
      setDiffLoading(true);
      try {
        const resp = await schemaApi.getProtoFileDiff(selectedFileId, fromVersion, toVersion);
        if (!cancelled) {
          setDiffData(resp);
        }
      } catch (err) {
        if (!cancelled) {
          toast.error((err as Error).message || '加载 Commit Diff 失败');
        }
      } finally {
        if (!cancelled) {
          setDiffLoading(false);
        }
      }
    };
    void runDiff();
    return () => {
      cancelled = true;
    };
  }, [viewMode, selectedFileId, fromVersion, toVersion, initialDiff, toast]);

  // 订阅 /api/events SSE git_sync.completed 事件：同步完成后无刷新更新列表、仓库状态与当前源码
  useEffect(() => {
    if (typeof window === 'undefined' || typeof fetch === 'undefined') {
      return;
    }
    const unsubscribe = subscribeServerEvents({
      onMessage: (evt) => {
        if (evt.event !== 'git_sync.completed') {
          return;
        }
        const payload = evt.data as GitSyncCompletedEventPayload | undefined;
        if (payload?.status === 'SUCCESS') {
          toast.success(
            `Git 同步完成 (${payload.repo_name || `#${payload.repo_id}`})：更新 ${payload.files_updated ?? 0} 个文件`
          );
        } else if (payload?.status === 'FAILED') {
          toast.error(`Git 同步失败：${payload.message || '未知错误'}`);
        }
        skipInitialReposFetchRef.current = false;
        skipInitialFilesFetchRef.current = false;
        skipInitialContentFetchRef.current = false;
        void loadGitRepos();
        void loadFiles();
        if (selectedFileId) {
          void loadFileDetail(selectedFileId, undefined);
        }
      },
    });
    return unsubscribe;
  }, [loadGitRepos, loadFiles, loadFileDetail, selectedFileId, toast]);

  const handleSelectRepo = (repoId: number | null) => {
    skipInitialFilesFetchRef.current = false;
    setSelectedRepoId(repoId);
    setSelectedBundleId(null);
    setPage(1);
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      next.set('tab', 'sources');
      if (repoId) next.set('repo_id', String(repoId));
      else next.delete('repo_id');
      next.delete('bundle_id');
      next.set('page', '1');
      return next;
    });
  };

  const handleSelectBundle = (bundleId: number | null) => {
    skipInitialFilesFetchRef.current = false;
    setSelectedBundleId(bundleId);
    setPage(1);
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      next.set('tab', 'sources');
      if (bundleId) next.set('bundle_id', String(bundleId));
      else next.delete('bundle_id');
      next.set('page', '1');
      return next;
    });
  };

  const handleSelectFile = (file: ProtoFileDTO) => {
    skipInitialContentFetchRef.current = false;
    setSelectedFileId(file.id);
    setSelectedVersion(undefined);
    setHighlightLine(1);
    setDiffData(null);
    setViewMode('code');
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      next.set('tab', 'sources');
      next.set('file_id', String(file.id));
      return next;
    });
  };

  const handleSelectLine = (line: number) => {
    setHighlightLine(line);
    if (typeof window !== 'undefined' && window.history?.replaceState) {
      const url = new URL(window.location.href);
      url.hash = `#L${line}`;
      window.history.replaceState(null, '', url.toString());
    }
  };

  const handleTriggerSync = async (branchOverride?: string, bundleOverride?: number | null) => {
    if (!selectedRepo || !isAdmin) return;
    setSyncDispatching(true);
    try {
      const res = await schemaApi.triggerGitRepoSync(selectedRepo.id, {
        bundle_id: bundleOverride !== undefined ? (bundleOverride ?? undefined) : (selectedBundleId ?? undefined),
        branch: branchOverride || selectedRepo.default_branch || 'main',
      });
      toast.info(`已发起分支 [${res.branch}] 异步契约同步，请稍候 SSE 自动刷新`);
      setGitRepos((prev) =>
        prev.map((r) => (r.id === selectedRepo.id ? { ...r, last_sync_status: 'RUNNING' } : r))
      );
    } catch (err) {
      toast.error((err as Error).message || '触发同步失败');
    } finally {
      setSyncDispatching(false);
    }
  };

  const activeFileMeta = contentData?.file ?? files.find((f) => f.id === selectedFileId) ?? null;

  return (
    <div className="pgit-workbench" data-testid="git-sources-workbench">
      {/* 顶部 Git 仓库 & Monorepo 1:N Bundles 切换控制栏 */}
      <div className="pdoc-card pgit-repo-bar">
        <div className="pgit-toolbar-row">
          <div className="pgit-toolbar-group">
            <label className="pgit-inline-field">
              <span>
                <FolderGit2 size={14} /> Git 仓库
              </span>
              <select
                className="pgit-input"
                aria-label="选择 Git 仓库"
                value={selectedRepoId ?? ''}
                disabled={reposLoading}
                onChange={(e) =>
                  handleSelectRepo(e.target.value ? Number(e.target.value) : null)
                }
              >
                <option value="">全部已纳管源码（含描述集重构兜底）</option>
                {gitRepos.map((repo) => (
                  <option key={repo.id} value={repo.id}>
                    {repo.name} ({repo.provider} · {repo.default_branch})
                  </option>
                ))}
              </select>
            </label>

            {selectedRepo && (
              <div className="pdoc-meta-row">
                <span className="pdoc-pill">
                  <GitBranch size={12} /> {selectedRepo.default_branch}
                </span>
                {renderRepoSyncBadge(selectedRepo.last_sync_status)}
                {selectedRepo.last_sync_commit && (
                  <code className="pgit-commit-pill" title={selectedRepo.last_sync_commit}>
                    <GitCommitHorizontal size={12} /> {selectedRepo.last_sync_commit.slice(0, 7)}
                  </code>
                )}
                {selectedRepo.has_credentials && selectedRepo.credential_hint && (
                  <span className="pdoc-pill" title="AES-256-GCM 脱敏凭据指纹">
                    凭据: <code>{selectedRepo.credential_hint}</code>
                  </span>
                )}
              </div>
            )}
          </div>

          <div className="pdoc-meta-row">
            {selectedRepo && (
              <button
                type="button"
                className="btn btn-secondary"
                onClick={() => setConsistencyDrawerOpen(true)}
              >
                <ShieldCheck size={14} /> 远端 SHA256 对账
              </button>
            )}

            {isAdmin && selectedRepo && (
              <button
                type="button"
                className="btn btn-secondary"
                disabled={syncDispatching}
                onClick={() => void handleTriggerSync()}
              >
                <RefreshCw size={14} className={syncDispatching ? 'pgit-spin' : undefined} />
                {syncDispatching ? '派发中...' : '立即同步'}
              </button>
            )}

            {isAdmin && selectedRepo && (
              <button
                type="button"
                className="btn btn-secondary"
                onClick={() => {
                  setManageCreatingNew(false);
                  setManageDrawerOpen(true);
                }}
              >
                <Settings2 size={14} /> 仓库与 Bundle 配置
              </button>
            )}

            {isAdmin && (
              <button
                type="button"
                className="btn btn-primary"
                onClick={() => {
                  setManageCreatingNew(true);
                  setManageDrawerOpen(true);
                }}
              >
                <Plus size={14} /> 接入 Git 仓库
              </button>
            )}
          </div>
        </div>

        {/* 1:N Monorepo Bundle 选择条 */}
        {selectedRepo && selectedRepo.bundles && selectedRepo.bundles.length > 0 && (
          <div className="pgit-bundle-strip">
            <span className="pgit-bundle-strip__label">
              <Layers size={13} /> Monorepo Bundles：
            </span>
            <button
              type="button"
              className={`code-pill-btn ${selectedBundleId === null ? 'code-pill-btn-active' : ''}`}
              onClick={() => handleSelectBundle(null)}
            >
              全部 ({selectedRepo.bundles.length})
            </button>
            {selectedRepo.bundles.map((b) => (
              <button
                key={b.id}
                type="button"
                className={`code-pill-btn ${selectedBundleId === b.id ? 'code-pill-btn-active' : ''}`}
                onClick={() => handleSelectBundle(b.id)}
                title={`${b.target_schema_name} (${b.subdir_path || '/'})`}
              >
                {b.bundle_name}
                <span className="pgit-bundle-chip-sub"> → {b.target_schema_name}</span>
              </button>
            ))}
            {selectedBundle && (
              <span className="pgit-muted">
                目录: <code>{selectedBundle.subdir_path || '/'}</code>
              </span>
            )}
          </div>
        )}
      </div>

      {/* 左右分栏：左侧文件树 + 标准分页 | 右侧只读源码 / Commit Diff */}
      <div className="pdoc-shell">
        <aside className="pdoc-tree-panel" aria-label="Git 契约源码文件树">
          <div className="pdoc-tree-panel__header">
            <div className="pgit-search-box">
              <Search size={14} />
              <input
                type="text"
                className="pdoc-tree-search"
                placeholder="检索 .proto 文件路径 / 包名 / 资产..."
                value={searchText}
                onChange={(e) => {
                  skipInitialFilesFetchRef.current = false;
                  setSearchText(e.target.value);
                  setPage(1);
                }}
              />
            </div>
            <label className="pgit-check">
              <input
                type="checkbox"
                checked={includeDeleted}
                onChange={(e) => {
                  skipInitialFilesFetchRef.current = false;
                  setIncludeDeleted(e.target.checked);
                  setPage(1);
                }}
              />
              包含仓库已删除归档文件
            </label>
          </div>

          <div className="pdoc-tree-scroll">
            {filesLoading ? (
              <div className="pdoc-empty">正在加载契约源码列表...</div>
            ) : files.length === 0 ? (
              <div className="pdoc-empty">
                当前条件下暂无已同步的 .proto 源码文件。若刚完成配置，请点击右上角“立即同步”。
              </div>
            ) : (
              files.map((file) => {
                const active = file.id === selectedFileId;
                return (
                  <button
                    key={file.id}
                    type="button"
                    className={`pdoc-tree-node ${active ? 'pdoc-tree-node--active' : ''}`}
                    onClick={() => handleSelectFile(file)}
                  >
                    <div className="pdoc-tree-node__main">
                      <Code2 size={14} />
                      <span className="pgit-file-path" title={file.repo_file_path || file.file_path}>
                        {file.file_path}
                      </span>
                    </div>
                    <div className="pgit-file-meta">
                      {file.bundle_name && <span className="pdoc-pill">{file.bundle_name}</span>}
                      {file.is_deleted && (
                        <span className="code-badge code-badge-danger">已删除</span>
                      )}
                      <span className="pdoc-tree-node__badge">v{file.latest_version}</span>
                    </div>
                  </button>
                );
              })
            )}
          </div>

          {filesTotal > 0 && (
            <div className="pgit-pagination-box">
              <Pagination
                page={page}
                pageSize={pageSize}
                totalItems={filesTotal}
                onPageChange={(nextPage) => {
                  skipInitialFilesFetchRef.current = false;
                  setPage(nextPage);
                }}
                onPageSizeChange={(nextSize) => {
                  skipInitialFilesFetchRef.current = false;
                  setPageSize(nextSize);
                }}
                pageSizeOptions={[15, 25, 50, 100]}
              />
            </div>
          )}
        </aside>

        <section className="pdoc-main-panel">
          {contentLoading ? (
            <div className="pdoc-card">
              <p className="pdoc-card__subtitle">正在读取不可变只读版本快照与规范诊断叠加层...</p>
            </div>
          ) : !contentData && !activeFileMeta ? (
            <div className="pdoc-card">
              <p className="pdoc-card__subtitle">
                请在左侧选择任意 .proto 文件以查看源码、行号锚点 (#L)、规则告警与历史 Commit Diff。
              </p>
            </div>
          ) : (
            <>
              {/* 顶部文件只读控制条：版本历史切换 & Code/Diff 切换（严禁在线修改与锁按钮） */}
              <div className="pdoc-card">
                <div className="pdoc-card__header">
                  <div>
                    <div className="pdoc-meta-row">
                      <h3 className="pdoc-card__title">
                        {activeFileMeta?.file_path || contentData?.file.file_path}
                      </h3>
                      <span className="code-badge code-badge-info">
                        {activeFileMeta?.target_schema_name}
                      </span>
                      {activeFileMeta?.bundle_name && (
                        <span className="pdoc-pill">{activeFileMeta.bundle_name}</span>
                      )}
                      <span className="code-badge code-badge-success">
                        <CheckCircle2 size={11} /> 只读权威快照
                      </span>
                    </div>
                    {activeFileMeta?.package_name && (
                      <p className="pdoc-card__subtitle">
                        package {activeFileMeta.package_name}
                        {activeFileMeta.repo_file_path
                          ? ` · 仓库全路径: ${activeFileMeta.repo_file_path}`
                          : ''}
                      </p>
                    )}
                  </div>

                  <div className="pdoc-meta-row">
                    {versions.length > 0 && (
                      <select
                        className="pgit-input"
                        aria-label="切换历史版本"
                        value={selectedVersion ?? versions[0].version}
                        onChange={(e) => {
                          skipInitialContentFetchRef.current = false;
                          setSelectedVersion(Number(e.target.value));
                        }}
                      >
                        {versions.map((ver) => (
                          <option key={ver.version} value={ver.version}>
                            v{ver.version} · {ver.version_tag || ver.commit_sha?.slice(0, 7) || 'snap'} (
                            {ver.author_name})
                          </option>
                        ))}
                      </select>
                    )}

                    <div className="code-tab-pills">
                      <button
                        type="button"
                        className={`code-pill-btn ${viewMode === 'code' ? 'code-pill-btn-active' : ''}`}
                        onClick={() => setViewMode('code')}
                      >
                        <Code2 size={13} /> 只读源码
                      </button>
                      <button
                        type="button"
                        className={`code-pill-btn ${viewMode === 'diff' ? 'code-pill-btn-active' : ''}`}
                        disabled={versions.length < 2}
                        onClick={() => setViewMode('diff')}
                      >
                        <FileDiff size={13} /> Commit Diff ({versions.length})
                      </button>
                    </div>
                  </div>
                </div>

                {contentData?.version && (
                  <div className="pdoc-meta-row">
                    {contentData.version.commit_sha && (
                      <code className="pgit-commit-pill">
                        Commit {contentData.version.commit_sha.slice(0, 7)}
                      </code>
                    )}
                    <span className="pdoc-pill">提交人: {contentData.version.author_name || '—'}</span>
                    <span className="pdoc-pill">
                      变更摘要: {contentData.version.commit_message || contentData.version.summary || '—'}
                    </span>
                  </div>
                )}
              </div>

              {viewMode === 'code' && contentData && (
                <ProtoReadOnlyCodeViewer
                  filePath={contentData.file.file_path}
                  content={contentData.content}
                  highlightLine={highlightLine}
                  violations={normalizedViolations}
                  symbolAnchors={contentData.symbol_anchors}
                  versionLabel={`v${contentData.version.version} (${contentData.version.version_tag || contentData.version.source_type})`}
                  hasSourceInfo={contentData.file.has_source_info}
                  sourceOrigin={contentData.file.source_origin}
                  onSelectLine={handleSelectLine}
                />
              )}

              {viewMode === 'diff' && (
                <div className="pgit-drawer-stack">
                  {versions.length >= 2 && (
                    <div className="pdoc-card pgit-toolbar-row">
                      <label className="pgit-inline-field">
                        <span>基准旧版本 (From)</span>
                        <select
                          className="pgit-input"
                          value={fromVersion}
                          onChange={(e) => setFromVersion(Number(e.target.value))}
                        >
                          {versions.map((v) => (
                            <option key={`from-${v.version}`} value={v.version}>
                              v{v.version} ({v.commit_sha?.slice(0, 7) || v.version_tag})
                            </option>
                          ))}
                        </select>
                      </label>
                      <label className="pgit-inline-field">
                        <span>目标新版本 (To)</span>
                        <select
                          className="pgit-input"
                          value={toVersion}
                          onChange={(e) => setToVersion(Number(e.target.value))}
                        >
                          {versions.map((v) => (
                            <option key={`to-${v.version}`} value={v.version}>
                              v{v.version} ({v.commit_sha?.slice(0, 7) || v.version_tag})
                            </option>
                          ))}
                        </select>
                      </label>
                    </div>
                  )}

                  {diffLoading ? (
                    <div className="pdoc-card">
                      <p className="pdoc-card__subtitle">正在计算版本间 Commit Diff...</p>
                    </div>
                  ) : diffData ? (
                    <ProtoCommitDiffView
                      filePath={diffData.file.file_path}
                      fromVersionLabel={`v${diffData.from_version.version} (${
                        diffData.from_version.commit_sha?.slice(0, 7) ||
                        diffData.from_version.version_tag ||
                        'snap'
                      })`}
                      toVersionLabel={`v${diffData.to_version.version} (${
                        diffData.to_version.commit_sha?.slice(0, 7) ||
                        diffData.to_version.version_tag ||
                        'snap'
                      })`}
                      unifiedDiff={diffData.unified_diff}
                      addedLines={diffData.added_lines}
                      deletedLines={diffData.deleted_lines}
                      commitSummary={
                        diffData.to_version.commit_message || diffData.to_version.summary
                      }
                      authorName={diffData.to_version.author_name}
                    />
                  ) : (
                    <div className="pdoc-card">
                      <p className="pdoc-card__subtitle">当前文件仅存在单个快照版本，暂无法对比历史 Diff。</p>
                    </div>
                  )}
                </div>
              )}
            </>
          )}
        </section>
      </div>

      {/* Git 仓库与 Monorepo 1:N Bundles 配置抽屉 */}
      <GitRepoConfigDrawer
        open={manageDrawerOpen}
        isAdmin={isAdmin}
        repo={manageCreatingNew ? null : selectedRepo}
        onClose={() => {
          setManageDrawerOpen(false);
          setManageCreatingNew(false);
        }}
        onSaved={(savedRepo, created) => {
          setGitRepos((prev) => {
            if (created) {
              return [savedRepo, ...prev];
            }
            return prev.map((r) => (r.id === savedRepo.id ? savedRepo : r));
          });
          setSelectedRepoId(savedRepo.id);
          if (created) {
            setManageCreatingNew(false);
          }
        }}
        onDeleted={(deletedId) => {
          setGitRepos((prev) => prev.filter((r) => r.id !== deletedId));
          if (selectedRepoId === deletedId) {
            setSelectedRepoId(null);
            setSelectedBundleId(null);
          }
        }}
      />

      {/* Git 远端 SHA256 只读一致性对账抽屉 */}
      <GitConsistencyDrawer
        open={consistencyDrawerOpen}
        isAdmin={isAdmin}
        repo={selectedRepo}
        selectedBundleId={selectedBundleId}
        initialReport={initialConsistencyReport}
        onClose={() => setConsistencyDrawerOpen(false)}
        onTriggerSync={handleTriggerSync}
      />
    </div>
  );
};

export default GitSourcesWorkbenchPane;
