import React, { useCallback, useEffect, useState } from 'react';
import { BookOpen, FileWarning, FolderTree, Search } from 'lucide-react';
import { EmptyState } from '@code/common';
import { getProtoDocsTree, getProtoServiceDocDetail } from '../../api/schema';
import { useToast } from '../../utils/toast';
import type {
  ProtoDocTreeNodeDTO,
  ProtoDocTreeResponse,
  ProtoServiceDocDetailDTO,
} from '../../types';
import { ServiceReferenceDocView } from './ServiceReferenceDocView';
import './ProtoDocsAndCodeViewer.css';

export interface LivingProtoDocsPaneProps {
  initialTree?: ProtoDocTreeResponse;
  initialDetail?: ProtoServiceDocDetailDTO | null;
  initialServiceName?: string;
  initialSchemaId?: number | string;
  initialSearch?: string;
  onJumpToSource: (schemaId: number, filePath: string, line: number) => void;
}

function findFirstService(nodes: ProtoDocTreeNodeDTO[]): { serviceName: string; schemaId?: number } | null {
  for (const n of nodes) {
    if (n.node_type === 'SERVICE' && n.service_name) {
      return { serviceName: n.service_name, schemaId: n.schema_id };
    }
    if (n.children && n.children.length > 0) {
      const hit = findFirstService(n.children);
      if (hit) return hit;
    }
  }
  return null;
}

function normalizeInitialServiceQuery(rawSearch: string): string {
  const trimmed = rawSearch.trim();
  if (!trimmed) return '';
  if (trimmed.includes('/')) {
    return trimmed.split('/')[0];
  }
  return trimmed;
}

export const LivingProtoDocsPane: React.FC<LivingProtoDocsPaneProps> = ({
  initialTree,
  initialDetail,
  initialServiceName = '',
  initialSchemaId = '',
  initialSearch = '',
  onJumpToSource,
}) => {
  const toast = useToast();
  const [tree, setTree] = useState<ProtoDocTreeResponse | null>(initialTree ?? null);
  const [loadingTree, setLoadingTree] = useState(false);
  const [searchInput, setSearchInput] = useState(() => normalizeInitialServiceQuery(initialSearch));
  const [categoryFilter, setCategoryFilter] = useState('');
  const [includeDeprecated, setIncludeDeprecated] = useState(true);

  const [selectedService, setSelectedService] = useState<string>(
    initialServiceName || initialDetail?.service_name || ''
  );
  const [selectedSchemaId, setSelectedSchemaId] = useState<number | string>(
    initialSchemaId || initialDetail?.schema_id || ''
  );
  const [detail, setDetail] = useState<ProtoServiceDocDetailDTO | null>(initialDetail ?? null);
  const [loadingDetail, setLoadingDetail] = useState(false);

  const loadTree = useCallback(async () => {
    if (typeof getProtoDocsTree !== 'function') return;
    setLoadingTree(true);
    try {
      const data = await getProtoDocsTree({
        category: categoryFilter || undefined,
        schema_id: selectedSchemaId && !selectedService ? Number(selectedSchemaId) : undefined,
        include_deprecated: includeDeprecated,
        search: searchInput.trim() || undefined,
      });
      setTree(data);
      if (!selectedService && data.items.length > 0) {
        const first = findFirstService(data.items);
        if (first) {
          setSelectedService(first.serviceName);
          if (first.schemaId) setSelectedSchemaId(first.schemaId);
        }
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '加载接口参考文档树失败');
    } finally {
      setLoadingTree(false);
    }
  }, [categoryFilter, includeDeprecated, searchInput, selectedSchemaId, selectedService, toast]);

  useEffect(() => {
    if (initialTree) return;
    void loadTree();
  }, [initialTree, loadTree]);

  useEffect(() => {
    if (initialDetail && initialDetail.service_name === selectedService) {
      return;
    }
    if (!selectedService || typeof getProtoServiceDocDetail !== 'function') {
      return;
    }
    let cancelled = false;
    setLoadingDetail(true);
    getProtoServiceDocDetail(selectedService, selectedSchemaId || undefined)
      .then((res) => {
        if (!cancelled) {
          setDetail(res);
        }
      })
      .catch((err) => {
        if (!cancelled) {
          toast.error(err instanceof Error ? err.message : '加载服务接口详情失败');
        }
      })
      .finally(() => {
        if (!cancelled) setLoadingDetail(false);
      });
    return () => {
      cancelled = true;
    };
  }, [initialDetail, selectedSchemaId, selectedService, toast]);

  const handleSelectService = (svcName: string, schemaId?: number) => {
    setSelectedService(svcName);
    setSelectedSchemaId(schemaId ?? '');
  };

  const renderTreeNodes = (nodes: ProtoDocTreeNodeDTO[]): React.ReactNode => {
    return nodes.map((node) => {
      if (node.node_type === 'SUBSYSTEM') {
        return (
          <div key={node.node_key} className="pdoc-tree-group">
            <div className="pdoc-tree-group__label">
              <span>{node.label}</span>
              <span>{node.method_count ?? 0} RPC</span>
            </div>
            {node.children && renderTreeNodes(node.children)}
          </div>
        );
      }
      if (node.node_type === 'SCHEMA') {
        return (
          <div key={node.node_key} className="pdoc-tree-group">
            <div className="pdoc-tree-schema__label">
              <span className="code-badge code-badge-info">{node.category || 'PROTO'}</span>
              <span>{node.label}</span>
            </div>
            {node.children && renderTreeNodes(node.children)}
          </div>
        );
      }
      if (node.node_type === 'PACKAGE') {
        return (
          <div key={node.node_key} className="pdoc-tree-group">
            <div className="pdoc-tree-pkg__label">{node.label}</div>
            {node.children && renderTreeNodes(node.children)}
          </div>
        );
      }
      if (node.node_type === 'SERVICE' && node.service_name) {
        const isActive = selectedService === node.service_name;
        const btnCls = [
          'pdoc-tree-svc-btn',
          isActive ? 'pdoc-tree-svc-btn--active' : '',
          node.is_deprecated ? 'pdoc-tree-svc-btn--deprecated' : '',
        ]
          .filter(Boolean)
          .join(' ');
        return (
          <button
            key={node.node_key}
            type="button"
            className={btnCls}
            onClick={() => handleSelectService(node.service_name ?? '', node.schema_id)}
            title={node.service_name}
          >
            <span className="pdoc-tree-svc-btn__name">{node.label}</span>
            <span className="pdoc-pill">{node.method_count ?? 0}</span>
          </button>
        );
      }
      return null;
    });
  };

  return (
    <div className="pdoc-workbench" data-testid="living-proto-docs-pane">
      <aside className="pdoc-tree-panel" aria-label="活体文档服务树">
        <div className="pdoc-tree-panel__header">
          <h3 className="pdoc-tree-panel__title">
            <FolderTree size={14} /> 活体接口参考目录
          </h3>
          {tree && (
            <div className="pdoc-tree-panel__stats">
              <span>{tree.total_services} 服务</span>
              <span>{tree.total_methods} 方法</span>
            </div>
          )}
        </div>

        <div className="pdoc-tree-panel__filters">
          <div className="code-search-box">
            <Search size={14} />
            <input
              type="text"
              className="code-input"
              placeholder="搜索服务、方法、注释、@error_code..."
              value={searchInput}
              onChange={(e) => setSearchInput(e.target.value)}
            />
          </div>

          <div className="pdoc-tree-panel__filter-row">
            <select
              className="code-select"
              value={categoryFilter}
              onChange={(e) => setCategoryFilter(e.target.value)}
              aria-label="契约分类筛选"
            >
              <option value="">全部分类</option>
              <option value="BUSINESS">BUSINESS 业务契约</option>
              <option value="GATEWAY">GATEWAY 网关契约</option>
              <option value="BASE_LIB">BASE_LIB 公共依赖库</option>
            </select>

            <label className="pdoc-meta-row">
              <input
                type="checkbox"
                checked={includeDeprecated}
                onChange={(e) => setIncludeDeprecated(e.target.checked)}
              />
              <span>显示已废弃</span>
            </label>
          </div>
        </div>

        {loadingTree ? (
          <p className="pdoc-card__subtitle">正在聚合活体文档索引...</p>
        ) : tree && tree.items.length > 0 ? (
          <div className="pdoc-tree-list">{renderTreeNodes(tree.items)}</div>
        ) : (
          <EmptyState
            icon={<FileWarning size={28} />}
            title="暂无匹配文档符号"
            description="请上传/在线编译或激活描述集后自动生成全注释参考文档。"
          />
        )}
      </aside>

      <main className="pdoc-detail">
        {loadingDetail ? (
          <div className="pdoc-card">
            <p className="pdoc-card__subtitle">正在解码服务契约详情、Enum 字典与黄金样本...</p>
          </div>
        ) : detail ? (
          <ServiceReferenceDocView detail={detail} onJumpToSource={onJumpToSource} />
        ) : (
          <div className="pdoc-card">
            <EmptyState
              icon={<BookOpen size={36} />}
              title="请选择左侧 Service 查看活体接口参考文档"
              description="支持 @owner、@deprecated、@error_code、AST 缺省示例、ACTIVE 黄金样本与离线 Markdown 一键导出。"
            />
          </div>
        )}
      </main>
    </div>
  );
};
