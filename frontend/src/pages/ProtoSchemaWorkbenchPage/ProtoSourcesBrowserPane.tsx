import React, { useCallback, useEffect, useState } from 'react';
import { Code2, GitCompare, Lock, Search } from 'lucide-react';
import { EmptyState, Pagination } from '@code/common';
import {
  getProtoFileContent,
  getProtoFileDiff,
  listProtoFileVersions,
  listProtoFiles,
} from '../../api/schema';
import { useToast } from '../../utils/toast';
import type {
  ProtoFileContentResponse,
  ProtoFileDTO,
  ProtoFileDiffResponse,
  ProtoFileVersionSummaryDTO,
} from '../../types';
import { ProtoReadOnlyCodeViewer } from './ProtoReadOnlyCodeViewer';
import { ProtoCommitDiffView } from './ProtoCommitDiffView';
import './ProtoDocsAndCodeViewer.css';

export interface ProtoSourcesBrowserPaneProps {
  initialSchemaName?: string;
}

export const ProtoSourcesBrowserPane: React.FC<ProtoSourcesBrowserPaneProps> = ({
  initialSchemaName = '',
}) => {
  const toast = useToast();
  const [files, setFiles] = useState<ProtoFileDTO[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(25);
  const [search, setSearch] = useState(initialSchemaName);
  const [loadingList, setLoadingList] = useState(false);

  const [selectedFileId, setSelectedFileId] = useState<number | null>(null);
  const [viewMode, setViewMode] = useState<'code' | 'diff'>('code');
  const [selectedVersion, setSelectedVersion] = useState<number | undefined>(undefined);
  const [versions, setVersions] = useState<ProtoFileVersionSummaryDTO[]>([]);
  const [fileContent, setFileContent] = useState<ProtoFileContentResponse | null>(null);
  const [diffData, setDiffData] = useState<ProtoFileDiffResponse | null>(null);
  const [loadingContent, setLoadingContent] = useState(false);

  const loadFiles = useCallback(async () => {
    if (typeof listProtoFiles !== 'function') return;
    setLoadingList(true);
    try {
      const res = await listProtoFiles({
        search: search.trim() || undefined,
        page,
        page_size: pageSize,
      });
      setFiles(res.items || []);
      setTotal(res.total || 0);
      if (!selectedFileId && res.items && res.items.length > 0) {
        setSelectedFileId(res.items[0].id);
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '加载只读源码列表失败');
    } finally {
      setLoadingList(false);
    }
  }, [page, pageSize, search, selectedFileId, toast]);

  useEffect(() => {
    void loadFiles();
  }, [loadFiles]);

  useEffect(() => {
    if (!selectedFileId || typeof getProtoFileContent !== 'function') return;
    let cancelled = false;
    setLoadingContent(true);

    Promise.all([
      getProtoFileContent(selectedFileId, selectedVersion),
      typeof listProtoFileVersions === 'function'
        ? listProtoFileVersions(selectedFileId, { page: 1, page_size: 25 })
        : Promise.resolve({ items: [], total: 0, page: 1, page_size: 25 }),
    ])
      .then(([contentRes, verRes]) => {
        if (cancelled) return;
        setFileContent(contentRes);
        setVersions(verRes.items || []);
        setDiffData(null);
      })
      .catch((err) => {
        if (!cancelled) {
          toast.error(err instanceof Error ? err.message : '读取只读源码内容失败');
        }
      })
      .finally(() => {
        if (!cancelled) setLoadingContent(false);
      });

    return () => {
      cancelled = true;
    };
  }, [selectedFileId, selectedVersion, toast]);

  const handleSwitchDiff = async () => {
    setViewMode('diff');
    if (!selectedFileId || versions.length < 2) {
      return;
    }
    try {
      const diffRes = await getProtoFileDiff(selectedFileId, 'prev', selectedVersion ?? 'latest');
      setDiffData(diffRes);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '获取版本 Diff 失败');
    }
  };

  return (
    <div className="pdoc-detail" data-testid="proto-sources-browser-pane">
      <div className="pdoc-banner pdoc-banner--info">
        <Lock size={15} />
        <span>
          <strong>Git-Only 只读无锁代码浏览器 (F-28)</strong>：M23 通过已编译/入池描述集虚拟重构只读源码与版本历史对比（M25 上线后将平滑切换至真实 Git 仓原文快照），平台严禁在线编辑保存或悲观加锁。
        </span>
      </div>

      <div className="pdoc-sources-shell">
        <aside className="pdoc-sources-list">
          <div className="code-search-box">
            <Search size={14} />
            <input
              type="text"
              className="code-input"
              placeholder="搜索 .proto 路径、制品或包名..."
              value={search}
              onChange={(e) => {
                setSearch(e.target.value);
                setPage(1);
              }}
            />
          </div>

          {loadingList ? (
            <p className="pdoc-card__subtitle">正在加载只读 .proto 源码列表...</p>
          ) : files.length === 0 ? (
            <EmptyState
              icon={<Code2 size={28} />}
              title="暂无可用源码视图"
              description="请先在台账 tab 上传或在线编译 .proto 描述集。"
            />
          ) : (
            <>
              {files.map((f) => (
                <button
                  key={f.id}
                  type="button"
                  className={`pdoc-file-item ${selectedFileId === f.id ? 'pdoc-file-item--active' : ''}`}
                  onClick={() => {
                    setSelectedFileId(f.id);
                    setSelectedVersion(undefined);
                    setViewMode('code');
                  }}
                >
                  <span className="pdoc-file-item__path">{f.file_path}</span>
                  <span className="pdoc-file-item__meta">
                    <span>{f.target_schema_name} v{f.latest_version}</span>
                    <span>{f.line_count} 行</span>
                  </span>
                </button>
              ))}
              <Pagination
                page={page}
                pageSize={pageSize}
                totalItems={total}
                onPageChange={setPage}
                onPageSizeChange={(ps) => {
                  setPageSize(ps);
                  setPage(1);
                }}
                pageSizeOptions={[15, 25, 50, 100]}
              />
            </>
          )}
        </aside>

        <section className="pdoc-detail">
          {fileContent && (
            <div className="pdoc-card">
              <div className="pdoc-card__header">
                <div className="code-tab-pills">
                  <button
                    type="button"
                    className={`code-pill-btn ${viewMode === 'code' ? 'code-pill-btn-active' : ''}`}
                    onClick={() => setViewMode('code')}
                  >
                    <Code2 size={13} /> 只读源码视图
                  </button>
                  <button
                    type="button"
                    className={`code-pill-btn ${viewMode === 'diff' ? 'code-pill-btn-active' : ''}`}
                    onClick={handleSwitchDiff}
                    disabled={versions.length < 2}
                    title={versions.length < 2 ? '需要至少 2 个历史版本方可对比 Diff' : '对比前一版本差异'}
                  >
                    <GitCompare size={13} /> 版本对比 Diff ({versions.length} 版本)
                  </button>
                </div>

                {versions.length > 0 && (
                  <select
                    className="code-select"
                    aria-label="切换版本快照"
                    value={fileContent.version.version}
                    onChange={(e) => setSelectedVersion(Number(e.target.value))}
                  >
                    {versions.map((v) => (
                      <option key={v.version} value={v.version}>
                        v{v.version} {v.version_tag ? `(${v.version_tag})` : ''} · {v.source_type}
                      </option>
                    ))}
                  </select>
                )}
              </div>
            </div>
          )}

          {loadingContent ? (
            <div className="pdoc-card">
              <p className="pdoc-card__subtitle">正在加载源码与行号锚点...</p>
            </div>
          ) : viewMode === 'diff' && diffData ? (
            <ProtoCommitDiffView
              filePath={diffData.file.file_path}
              fromVersionLabel={`v${diffData.from_version.version}`}
              toVersionLabel={`v${diffData.to_version.version}`}
              unifiedDiff={diffData.unified_diff}
              addedLines={diffData.added_lines}
              deletedLines={diffData.deleted_lines}
              commitSummary={diffData.to_version.summary}
              authorName={diffData.to_version.author_name}
            />
          ) : fileContent ? (
            <ProtoReadOnlyCodeViewer
              filePath={fileContent.file.file_path}
              content={fileContent.content}
              violations={fileContent.violations}
              symbolAnchors={fileContent.symbol_anchors}
              versionLabel={`v${fileContent.version.version} (${fileContent.version.source_type})`}
              hasSourceInfo={fileContent.file.has_source_info}
              sourceOrigin={fileContent.file.source_origin}
            />
          ) : (
            <EmptyState
              icon={<Code2 size={32} />}
              title="选择左侧只读 .proto 文件预览代码"
              description="支持词法着色、符号锚点跳转及跨版本 Diff。"
            />
          )}
        </section>
      </div>
    </div>
  );
};
