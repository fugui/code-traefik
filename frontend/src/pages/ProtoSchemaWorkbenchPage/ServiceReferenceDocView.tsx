import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import {
  AlertTriangle,
  BookOpen,
  Code2,
  Download,
  ExternalLink,
  GitBranch,
  Layers,
  ShieldAlert,
  UserCheck,
} from 'lucide-react';
import { MODULE_BASE_PATH } from '../../constants';
import { exportProtoDocsMarkdown } from '../../api/schema';
import { useToast } from '../../utils/toast';
import type {
  FieldDocNodeDTO,
  MethodDocDTO,
  ProtoServiceDocDetailDTO,
} from '../../types';
import './ProtoDocsAndCodeViewer.css';

function resolvePortalAwarePath(subPath: string): string {
  if (typeof window !== 'undefined' && window.__POWERED_BY_PORTAL__) {
    return `${MODULE_BASE_PATH}${subPath}`;
  }
  return subPath;
}

const SourceWorkbenchDeepLink: React.FC<{ fileId?: number; line: number }> = ({
  fileId,
  line,
}) => {
  if (!fileId || fileId <= 0) {
    return null;
  }
  const hash = line > 0 ? `#L${line}` : '';
  return (
    <Link
      to={resolvePortalAwarePath(`/schemas?tab=sources&file_id=${fileId}${hash}`)}
      className="pdoc-source-link pdoc-source-link--workbench"
      title="在源码工作台打开并定位行号"
    >
      <ExternalLink size={12} />
      工作台源码
    </Link>
  );
};

function formatJsonBlock(val: unknown): string {
  if (val === undefined || val === null) {
    return '{}';
  }
  try {
    return JSON.stringify(val, null, 2);
  } catch {
    return String(val);
  }
}

interface FlattenedFieldRow {
  path: string;
  depth: number;
  node: FieldDocNodeDTO;
}

function flattenDocFields(nodes: FieldDocNodeDTO[], depth = 0, prefix = ''): FlattenedFieldRow[] {
  const out: FlattenedFieldRow[] = [];
  for (const node of nodes) {
    const path = prefix ? `${prefix}.${node.name}` : node.name;
    out.push({ path, depth, node });
    if (node.children && node.children.length > 0) {
      out.push(...flattenDocFields(node.children, depth + 1, path));
    }
  }
  return out;
}

const FieldDictionaryTable: React.FC<{
  title: string;
  messageName: string;
  fields: FieldDocNodeDTO[];
}> = ({ title, messageName, fields }) => {
  const rows = flattenDocFields(fields);

  return (
    <div>
      <div className="pdoc-meta-row">
        <span className="pdoc-section-title">{title}</span>
        <span className="pdoc-pill">{messageName}</span>
      </div>
      {rows.length === 0 ? (
        <p className="pdoc-card__subtitle">无字段定义（空消息体）</p>
      ) : (
        <div className="pdoc-code-scroll">
          <table className="code-table">
            <thead>
              <tr>
                <th>字段路径</th>
                <th>类型 / 修饰符</th>
                <th>Tag</th>
                <th>注释与约束说明</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => {
                const { node } = row;
                const indentPrefix = row.depth > 0 ? `${'↳ '.repeat(Math.min(row.depth, 4))}` : '';
                return (
                  <tr key={`${row.path}:${node.number}`}>
                    <td>
                      <code className="pdoc-field-indent">
                        <span>{indentPrefix}{row.path}</span>
                      </code>
                      {node.oneof_group && (
                        <span className="code-badge code-badge-info">oneof:{node.oneof_group}</span>
                      )}
                      {node.is_deprecated && (
                        <span className="code-badge code-badge-danger">废弃</span>
                      )}
                      {node.truncated && (
                        <span className="code-badge code-badge-warning">
                          {node.truncated_reason === 'CYCLE_DETECTED' ? '循环引用截断' : '深度<=12截断'}
                        </span>
                      )}
                    </td>
                    <td>
                      <code>{node.type}</code>{' '}
                      <span className="pdoc-pill">{node.label}</span>
                    </td>
                    <td>#{node.number}</td>
                    <td>
                      <span>{node.comment || '—'}</span>
                      {node.enum_values && node.enum_values.length > 0 && (
                        <div className="pdoc-meta-row">
                          {node.enum_values.slice(0, 6).map((ev) => (
                            <span key={ev.name} className="pdoc-pill">
                              {ev.name}={ev.number}
                            </span>
                          ))}
                        </div>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
};

const MethodReferenceCard: React.FC<{
  method: MethodDocDTO;
  schemaId: number;
  onJumpToSource: (schemaId: number, filePath: string, line: number) => void;
}> = ({ method, schemaId, onJumpToSource }) => {
  const hasGolden = method.golden_samples && method.golden_samples.length > 0;
  const [sampleMode, setSampleMode] = useState<'golden' | 'default'>(hasGolden ? 'golden' : 'default');
  const activeGolden = hasGolden ? method.golden_samples[0] : undefined;

  const lineSuffix = method.start_line > 0 ? `#L${method.start_line}` : '';

  return (
    <section className="pdoc-method-card" aria-label={`RPC 方法 ${method.operation}`}>
      <div className="pdoc-method-card__top">
        <div>
          <div className="pdoc-meta-row">
            <h4 className="pdoc-card__title">rpc {method.operation}</h4>
            {method.is_deprecated && (
              <span className="code-badge code-badge-danger">DEPRECATED</span>
            )}
            {(method.client_streaming || method.server_streaming) && (
              <span className="code-badge code-badge-purple">
                {method.client_streaming ? 'ClientStream ' : ''}
                {method.server_streaming ? 'ServerStream' : ''}
              </span>
            )}
            {method.owner_tag && (
              <span className="pdoc-pill">
                <UserCheck size={12} /> @{method.owner_tag}
              </span>
            )}
          </div>
          <div className="pdoc-method-card__sig">
            ({method.req_message_full_name}) returns ({method.rsp_message_full_name})
          </div>
          {method.leading_comments && (
            <p className="pdoc-card__subtitle">{method.leading_comments}</p>
          )}
        </div>

        {method.proto_file && (
          <div className="pdoc-meta-row">
            <button
              type="button"
              className="pdoc-source-link"
              onClick={() => onJumpToSource(schemaId, method.proto_file, method.start_line || 1)}
            >
              <Code2 size={13} />
              {`${method.proto_file}${lineSuffix}`}
            </button>
            <SourceWorkbenchDeepLink
              fileId={method.source_file_id}
              line={method.start_line || 1}
            />
          </div>
        )}
      </div>

      {method.is_deprecated && (
        <div className="pdoc-banner pdoc-banner--deprecated">
          <AlertTriangle size={15} />
          <span>
            <strong>方法已废弃</strong>：{method.deprecation_reason || '请尽快升级至推荐替代接口'}
          </span>
        </div>
      )}

      {method.error_codes && method.error_codes.length > 0 && (
        <div>
          <span className="pdoc-section-title">
            <ShieldAlert size={14} /> 方法错误码契约 (@error_code)
          </span>
          <table className="code-table">
            <thead>
              <tr>
                <th>错误码标识</th>
                <th>HTTP / CROS 状态码</th>
                <th>说明</th>
              </tr>
            </thead>
            <tbody>
              {method.error_codes.map((ec) => (
                <tr key={`${ec.code}:${ec.http_or_cros_code}`}>
                  <td><code>{ec.code}</code></td>
                  <td><span className="code-badge code-badge-warning">{ec.http_or_cros_code}</span></td>
                  <td>{ec.description}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <FieldDictionaryTable
        title="请求消息字典"
        messageName={method.req_message_full_name}
        fields={method.req_fields}
      />

      <FieldDictionaryTable
        title="应答消息字典"
        messageName={method.rsp_message_full_name}
        fields={method.rsp_fields}
      />

      <div className="pdoc-json-panel">
        <div className="pdoc-json-panel__bar">
          <div className="code-tab-pills">
            {hasGolden && (
              <button
                type="button"
                className={`code-pill-btn ${sampleMode === 'golden' ? 'code-pill-btn-active' : ''}`}
                onClick={() => setSampleMode('golden')}
              >
                ACTIVE 黄金样本 ({activeGolden?.suite_code})
              </button>
            )}
            <button
              type="button"
              className={`code-pill-btn ${sampleMode === 'default' || !hasGolden ? 'code-pill-btn-active' : ''}`}
              onClick={() => setSampleMode('default')}
            >
              AST 缺省 JSON 样例
            </button>
          </div>
          {sampleMode === 'golden' && activeGolden && (
            <span className="pdoc-pill">
              套件：{activeGolden.suite_name} · 解码 {activeGolden.decode_status}
            </span>
          )}
        </div>

        <div className="pdoc-json-panel__grid">
          <div>
            <div className="pdoc-tree-panel__title">请求示例 (Request JSON)</div>
            <pre className="pdoc-json-pre">
              {sampleMode === 'golden' && activeGolden
                ? formatJsonBlock(activeGolden.req_json)
                : formatJsonBlock(method.default_req_json)}
            </pre>
          </div>
          <div>
            <div className="pdoc-tree-panel__title">应答示例 (Response JSON)</div>
            <pre className="pdoc-json-pre">
              {sampleMode === 'golden' && activeGolden
                ? formatJsonBlock(activeGolden.rsp_json)
                : formatJsonBlock(method.default_rsp_json)}
            </pre>
          </div>
        </div>
      </div>
    </section>
  );
};

export interface ServiceReferenceDocViewProps {
  detail: ProtoServiceDocDetailDTO;
  onJumpToSource: (schemaId: number, filePath: string, line: number) => void;
}

export const ServiceReferenceDocView: React.FC<ServiceReferenceDocViewProps> = ({
  detail,
  onJumpToSource,
}) => {
  const toast = useToast();
  const [exporting, setExporting] = useState(false);

  const handleExportMarkdown = async () => {
    setExporting(true);
    try {
      await exportProtoDocsMarkdown({
        service_name: detail.service_name,
        schema_id: detail.schema_id,
      });
      toast.success(`已导出 ${detail.short_name} 离线 Markdown 接口手册`);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '导出 Markdown 失败');
    } finally {
      setExporting(false);
    }
  };

  const serviceLineSuffix = detail.start_line > 0 ? `#L${detail.start_line}` : '';

  return (
    <div className="pdoc-detail" data-testid="service-reference-doc-view">
      {!detail.has_source_info && (
        <div className="pdoc-banner pdoc-banner--warning">
          <AlertTriangle size={16} />
          <span>
            降级提示：当前描述集来自未保留 <code>SourceCodeInfo</code> 的手工 <code>.pb</code> 二进制产物，系统已自动提取全量类型签名、字段编号、Enum 枚举及缺省 JSON；补充注释请使用在线编译或 Git 同步入池。
          </span>
        </div>
      )}

      {detail.is_deprecated && (
        <div className="pdoc-banner pdoc-banner--deprecated">
          <AlertTriangle size={16} />
          <div>
            <strong>服务已标记废弃 (@deprecated)</strong>：{detail.deprecation_reason || '请迁往新版推荐契约'}
          </div>
        </div>
      )}

      <div className="pdoc-card">
        <div className="pdoc-card__header">
          <div>
            <div className="pdoc-meta-row">
              <BookOpen size={18} />
              <h2 className="pdoc-card__title">{detail.service_name}</h2>
              <span className="code-badge code-badge-info">{detail.schema_category}</span>
              <span className="pdoc-pill">{detail.schema_name} v{detail.schema_version}</span>
            </div>
            {(detail.leading_comments || detail.summary) && (
              <p className="pdoc-card__subtitle">{detail.leading_comments || detail.summary}</p>
            )}
          </div>

          <div className="pdoc-meta-row">
            {detail.proto_file && (
              <>
                <button
                  type="button"
                  className="pdoc-source-link"
                  onClick={() => onJumpToSource(detail.schema_id, detail.proto_file, detail.start_line || 1)}
                >
                  <Code2 size={14} />
                  {`${detail.proto_file}${serviceLineSuffix}`}
                </button>
                <SourceWorkbenchDeepLink
                  fileId={detail.source_file_id}
                  line={detail.start_line || 1}
                />
              </>
            )}
            <button
              type="button"
              className="btn btn-secondary"
              disabled={exporting}
              onClick={handleExportMarkdown}
            >
              <Download size={14} /> {exporting ? '导出中...' : '导出 Markdown 手册'}
            </button>
          </div>
        </div>

        <div className="pdoc-meta-row">
          <span className="pdoc-pill">包路径：{detail.package_name || 'default'}</span>
          {detail.owner_tag && (
            <span className="pdoc-pill">
              <UserCheck size={12} /> 负责人：{detail.owner_tag}
            </span>
          )}
          <span className="pdoc-pill">来源：{detail.source_type || 'UPLOAD_PB'}</span>
          <span className="pdoc-pill">方法数：{detail.methods.length}</span>
        </div>

        {detail.error_codes && detail.error_codes.length > 0 && (
          <div>
            <span className="pdoc-section-title">
              <ShieldAlert size={14} /> 服务级错误码字典 (@error_code)
            </span>
            <table className="code-table">
              <thead>
                <tr>
                  <th>错误码标识</th>
                  <th>HTTP / CROS 状态码</th>
                  <th>描述说明</th>
                </tr>
              </thead>
              <tbody>
                {detail.error_codes.map((ec) => (
                  <tr key={`${ec.code}:${ec.http_or_cros_code}`}>
                    <td><code>{ec.code}</code></td>
                    <td><span className="code-badge code-badge-warning">{ec.http_or_cros_code}</span></td>
                    <td>{ec.description}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* ADR-16 架构归属、契约定义方、多实现与下游消费方 */}
      <div className="pdoc-card">
        <div className="pdoc-card__header">
          <span className="pdoc-section-title">
            <GitBranch size={15} /> ADR-16 架构血缘、公共契约定义与多实现矩阵
          </span>
          <span className="code-badge code-badge-info">状态：{detail.lineage.claim_status}</span>
        </div>

        {detail.lineage.definer && (
          <div className="pdoc-banner pdoc-banner--info">
            <Layers size={15} />
            <div>
              <strong>公共契约库定义方 (CONTRACT_LIB)</strong>：
              <code>{detail.lineage.definer.module_code}</code>（{detail.lineage.definer.module_name} / {detail.lineage.definer.subsystem_name}，模式 {detail.lineage.definer.provide_mode}）
            </div>
          </div>
        )}

        {detail.lineage.implementations.length > 0 ? (
          <table className="code-table">
            <thead>
              <tr>
                <th>实现 Key</th>
                <th>实现模块</th>
                <th>所属子系统 / 进程</th>
                <th>负责人</th>
                <th>工作台跳转</th>
              </tr>
            </thead>
            <tbody>
              {detail.lineage.implementations.map((impl) => (
                <tr key={impl.service_id}>
                  <td><span className="code-badge code-badge-purple">{impl.impl_key || 'DEFAULT'}</span></td>
                  <td>
                    <code>{impl.module_code}</code> · {impl.module_name}
                  </td>
                  <td>
                    {impl.subsystem_name || '—'} / <code>{impl.arch_process_code || '未绑进程'}</code>
                  </td>
                  <td>{impl.owner_name || '—'}</td>
                  <td>
                    <div className="pdoc-lineage-links">
                      <Link
                        to={resolvePortalAwarePath(`/topology?focus=mod_${impl.module_id}`)}
                        className="pdoc-deep-link"
                      >
                        拓扑聚焦 <ExternalLink size={12} />
                      </Link>
                      <Link
                        to={resolvePortalAwarePath(`/architecture?tree=logical&module_id=${impl.module_id}`)}
                        className="pdoc-deep-link"
                      >
                        架构台账 <ExternalLink size={12} />
                      </Link>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <p className="pdoc-card__subtitle">当前契约尚未被架构逻辑模块认领实现（UNCLAIMED）。</p>
        )}

        {detail.lineage.consumers.length > 0 && (
          <div className="pdoc-meta-row">
            <span className="pdoc-pill">下游调用消费方：</span>
            {detail.lineage.consumers.map((c) => (
              <Link
                key={c.module_id}
                to={resolvePortalAwarePath(`/topology?focus=mod_${c.module_id}`)}
                className="pdoc-deep-link"
              >
                {c.module_code} ({c.dep_type})
              </Link>
            ))}
          </div>
        )}
      </div>

      {/* 各 RPC 方法详细文档与示例 */}
      {detail.methods.map((method) => (
        <MethodReferenceCard
          key={method.full_name}
          method={method}
          schemaId={detail.schema_id}
          onJumpToSource={onJumpToSource}
        />
      ))}

      {/* Enum 枚举字典表 */}
      {detail.enums && detail.enums.length > 0 && (
        <div className="pdoc-card">
          <span className="pdoc-section-title">关联 Enum 枚举字典 ({detail.enums.length})</span>
          {detail.enums.map((ed) => (
            <div key={ed.full_name}>
              <div className="pdoc-meta-row">
                <strong>{ed.full_name}</strong>
                {ed.proto_file && (
                  <>
                    <button
                      type="button"
                      className="pdoc-source-link"
                      onClick={() => onJumpToSource(detail.schema_id, ed.proto_file, ed.start_line || 1)}
                    >
                      <Code2 size={12} />
                      {`${ed.proto_file}${ed.start_line > 0 ? `#L${ed.start_line}` : ''}`}
                    </button>
                    <SourceWorkbenchDeepLink
                      fileId={ed.source_file_id ?? detail.source_file_id}
                      line={ed.start_line || 1}
                    />
                  </>
                )}
                {ed.comment && <span className="pdoc-card__subtitle">{ed.comment}</span>}
              </div>
              <table className="code-table">
                <thead>
                  <tr>
                    <th>枚举常量</th>
                    <th>整数值</th>
                    <th>状态</th>
                    <th>注释说明</th>
                  </tr>
                </thead>
                <tbody>
                  {ed.values.map((ev) => (
                    <tr key={`${ed.full_name}:${ev.name}:${ev.number}`}>
                      <td><code>{ev.name}</code></td>
                      <td>{ev.number}</td>
                      <td>
                        {ev.is_deprecated ? (
                          <span className="code-badge code-badge-danger">DEPRECATED</span>
                        ) : (
                          <span className="code-badge code-badge-success">ACTIVE</span>
                        )}
                      </td>
                      <td>{ev.comment || '—'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ))}
        </div>
      )}
    </div>
  );
};
