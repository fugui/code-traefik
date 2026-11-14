import React, { useCallback, useEffect, useState } from 'react';
import { Drawer, EmptyState, Pagination, usePaginationState } from '@code/common';
import { BookCheck, CheckCircle2, FileSearch, Play, Plus, ShieldAlert, Trash2 } from 'lucide-react';
import * as schemaApi from '../../api/schema';
import type {
  CheckReportDTO,
  CheckRuleDTO,
  RuleSeverity,
  VocabularyTermDTO,
} from '../../types/schema';

export type LintDrawerSubTab = 'rules' | 'vocab' | 'report';

interface LintRuleAndVocabDrawerProps {
  open: boolean;
  isAdmin: boolean;
  initialSubTab?: LintDrawerSubTab;
  initialRules?: CheckRuleDTO[];
  initialRulesTotal?: number;
  initialVocab?: VocabularyTermDTO[];
  initialVocabTotal?: number;
  initialReport?: CheckReportDTO | null;
  targetSchemaId?: number;
  onClose: () => void;
  onJumpToSource?: (protoFile: string, line: number) => void;
}

const SEVERITY_BADGE: Record<string, string> = {
  ERROR: 'code-badge code-badge-danger',
  WARNING: 'code-badge code-badge-warning',
  INFO: 'code-badge code-badge-info',
};

const REPORT_STATUS_BADGE: Record<string, string> = {
  PASSED: 'code-badge code-badge-success',
  WARN: 'code-badge code-badge-warning',
  FAILED: 'code-badge code-badge-danger',
};

const LintRuleAndVocabDrawer: React.FC<LintRuleAndVocabDrawerProps> = ({
  open,
  isAdmin,
  initialSubTab = 'rules',
  initialRules,
  initialRulesTotal,
  initialVocab,
  initialVocabTotal,
  initialReport,
  targetSchemaId,
  onClose,
  onJumpToSource,
}) => {
  const [subTab, setSubTab] = useState<LintDrawerSubTab>(initialSubTab);
  const { page, pageSize, setPage, setPageSize } = usePaginationState({ defaultPageSize: 25 });

  // 规则状态
  const [rules, setRules] = useState<CheckRuleDTO[]>(initialRules ?? []);
  const [rulesTotal, setRulesTotal] = useState<number>(initialRulesTotal ?? initialRules?.length ?? 0);
  const [rulesLoading, setRulesLoading] = useState(false);
  const [ruleCategoryFilter, setRuleCategoryFilter] = useState('');
  const [showRuleCreate, setShowRuleCreate] = useState(false);
  const [newRuleCode, setNewRuleCode] = useState('');
  const [newRuleName, setNewRuleName] = useState('');
  const [newRuleCategory, setNewRuleCategory] = useState('NAMING');
  const [newRuleScope, setNewRuleScope] = useState('MESSAGE');
  const [newRuleSeverity, setNewRuleSeverity] = useState<RuleSeverity>('WARNING');
  const [newRulePattern, setNewRulePattern] = useState('^[A-Z][A-Za-z0-9]*$');
  const [newRuleSuggestion, setNewRuleSuggestion] = useState('请遵循命名规范修正 {symbol}');
  const [confirmDeleteRuleId, setConfirmDeleteRuleId] = useState<number | null>(null);

  // 词库状态
  const [vocabTerms, setVocabTerms] = useState<VocabularyTermDTO[]>(initialVocab ?? []);
  const [vocabTotal, setVocabTotal] = useState<number>(initialVocabTotal ?? initialVocab?.length ?? 0);
  const [vocabLoading, setVocabLoading] = useState(false);
  const [vocabSourceFilter, setVocabSourceFilter] = useState('');
  const [vocabSearch, setVocabSearch] = useState('');
  const [showVocabCreate, setShowVocabCreate] = useState(false);
  const [newTerm, setNewTerm] = useState('');
  const [newTermEn, setNewTermEn] = useState('');
  const [newTermZh, setNewTermZh] = useState('');
  const [newTermAliases, setNewTermAliases] = useState('');
  const [newTermDomain, setNewTermDomain] = useState('GENERAL');
  const [confirmDeleteVocabId, setConfirmDeleteVocabId] = useState<number | null>(null);

  // 检查报告与重复探测状态
  const [activeReport, setActiveReport] = useState<CheckReportDTO | null>(initialReport ?? null);
  const [recentReports, setRecentReports] = useState<CheckReportDTO[]>(initialReport ? [initialReport] : []);
  const [scanRunning, setScanRunning] = useState(false);
  const [includeDuplicates, setIncludeDuplicates] = useState(true);
  const [includeLLM, setIncludeLLM] = useState(false);

  const [bannerMsg, setBannerMsg] = useState<string | null>(null);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  const loadRules = useCallback(async () => {
    setRulesLoading(true);
    try {
      const resp = await schemaApi.listCheckRules({
        page,
        page_size: pageSize,
        category: ruleCategoryFilter || undefined,
      });
      setRules(resp.items ?? []);
      setRulesTotal(resp.total ?? 0);
    } catch (err) {
      setErrorMsg(err instanceof Error ? err.message : '加载静态检查规则失败');
    } finally {
      setRulesLoading(false);
    }
  }, [page, pageSize, ruleCategoryFilter]);

  const loadVocab = useCallback(async () => {
    setVocabLoading(true);
    try {
      const resp = await schemaApi.listVocabularyTerms({
        page,
        page_size: pageSize,
        source: vocabSourceFilter || undefined,
        search: vocabSearch.trim() || undefined,
      });
      setVocabTerms(resp.items ?? []);
      setVocabTotal(resp.total ?? 0);
    } catch (err) {
      setErrorMsg(err instanceof Error ? err.message : '加载标准词库失败');
    } finally {
      setVocabLoading(false);
    }
  }, [page, pageSize, vocabSourceFilter, vocabSearch]);

  const loadReports = useCallback(async () => {
    try {
      const list = await schemaApi.listCheckReports({ page: 1, page_size: 15, schema_id: targetSchemaId });
      const rows = list.items ?? [];
      setRecentReports(rows);
      if (rows.length > 0 && !activeReport) {
        const detail = await schemaApi.getCheckReportDetail(rows[0].id);
        setActiveReport(detail);
      }
    } catch (err) {
      setErrorMsg(err instanceof Error ? err.message : '加载历史体检报告失败');
    }
  }, [targetSchemaId, activeReport]);

  useEffect(() => {
    if (!open) return;
    if (subTab === 'rules' && initialRules === undefined) {
      void loadRules();
    } else if (subTab === 'vocab' && initialVocab === undefined) {
      void loadVocab();
    } else if (subTab === 'report' && initialReport === undefined) {
      void loadReports();
    }
  }, [open, subTab, initialRules, initialVocab, initialReport, loadRules, loadVocab, loadReports]);

  const handleToggleRuleEnabled = async (rule: CheckRuleDTO) => {
    if (!isAdmin) return;
    setErrorMsg(null);
    try {
      const updated = await schemaApi.updateCheckRule(rule.id, { is_enabled: !rule.is_enabled });
      setRules((prev) => prev.map((r) => (r.id === rule.id ? updated : r)));
      setBannerMsg(`规则 ${rule.rule_code} 已${updated.is_enabled ? '启用' : '停用'}`);
    } catch (err) {
      setErrorMsg(err instanceof Error ? err.message : '更新规则状态失败');
    }
  };

  const handleChangeRuleSeverity = async (rule: CheckRuleDTO, severity: RuleSeverity) => {
    if (!isAdmin) return;
    setErrorMsg(null);
    try {
      const updated = await schemaApi.updateCheckRule(rule.id, { severity });
      setRules((prev) => prev.map((r) => (r.id === rule.id ? updated : r)));
      setBannerMsg(`规则 ${rule.rule_code} 级别已更新为 ${severity}`);
    } catch (err) {
      setErrorMsg(err instanceof Error ? err.message : '更新规则级别失败');
    }
  };

  const handleCreateRule = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!isAdmin) return;
    setErrorMsg(null);
    try {
      await schemaApi.createCheckRule({
        rule_code: newRuleCode.trim().toUpperCase(),
        name: newRuleName.trim(),
        category: newRuleCategory,
        target_scope: newRuleScope,
        severity: newRuleSeverity,
        check_mode: 'REGEX',
        pattern: newRulePattern.trim(),
        suggestion_template: newRuleSuggestion.trim(),
        is_enabled: true,
      });
      setShowRuleCreate(false);
      setNewRuleCode('');
      setNewRuleName('');
      setBannerMsg('自定义静态规则创建成功');
      await loadRules();
    } catch (err) {
      setErrorMsg(err instanceof Error ? err.message : '创建规则失败');
    }
  };

  const handleDeleteRule = async (id: number) => {
    if (!isAdmin) return;
    setErrorMsg(null);
    try {
      await schemaApi.deleteCheckRule(id);
      setConfirmDeleteRuleId(null);
      setBannerMsg('自定义规则已删除');
      await loadRules();
    } catch (err) {
      setErrorMsg(err instanceof Error ? err.message : '删除规则失败');
    }
  };

  const handleCreateVocab = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!isAdmin) return;
    setErrorMsg(null);
    try {
      const aliases = newTermAliases
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean);
      await schemaApi.createVocabularyTerm({
        term: newTerm.trim().toLowerCase(),
        description_en: newTermEn.trim(),
        description_zh: newTermZh.trim(),
        aliases,
        domain: newTermDomain,
        source: 'MANUAL',
        is_approved: true,
      });
      setShowVocabCreate(false);
      setNewTerm('');
      setNewTermEn('');
      setNewTermZh('');
      setNewTermAliases('');
      setBannerMsg('标准词根已加入企业词库');
      await loadVocab();
    } catch (err) {
      setErrorMsg(err instanceof Error ? err.message : '新增词根失败');
    }
  };

  const handleApproveVocab = async (item: VocabularyTermDTO) => {
    if (!isAdmin) return;
    setErrorMsg(null);
    try {
      const updated = await schemaApi.updateVocabularyTerm(item.id, { is_approved: true });
      setVocabTerms((prev) => prev.map((v) => (v.id === item.id ? updated : v)));
      setBannerMsg(`词根 "${item.term}" 已批准纳为标准`);
    } catch (err) {
      setErrorMsg(err instanceof Error ? err.message : '审批词根失败');
    }
  };

  const handleDeleteVocab = async (id: number) => {
    if (!isAdmin) return;
    setErrorMsg(null);
    try {
      await schemaApi.deleteVocabularyTerm(id);
      setConfirmDeleteVocabId(null);
      setBannerMsg('词根条目已移除');
      await loadVocab();
    } catch (err) {
      setErrorMsg(err instanceof Error ? err.message : '删除词根失败');
    }
  };

  const handleTriggerScan = async () => {
    setErrorMsg(null);
    setBannerMsg(null);
    setScanRunning(true);
    try {
      const report = await schemaApi.triggerCheckReportScan({
        schema_id: targetSchemaId,
        include_duplicates: includeDuplicates,
        include_llm: includeLLM,
      });
      setActiveReport(report);
      setRecentReports((prev) => [report, ...prev.filter((item) => item.id !== report.id)].slice(0, 15));
      setBannerMsg(
        `体检扫描 #${report.id} 完成：错误 ${report.error_count} / 警告 ${report.warning_count} / AST 重复簇 ${report.duplicate_group_count}`
      );
    } catch (err) {
      setErrorMsg(err instanceof Error ? err.message : '执行静态检查扫描失败');
    } finally {
      setScanRunning(false);
    }
  };

  const handleSelectReport = async (reportId: number) => {
    try {
      const detail = await schemaApi.getCheckReportDetail(reportId);
      setActiveReport(detail);
    } catch (err) {
      setErrorMsg(err instanceof Error ? err.message : '读取报告详情失败');
    }
  };

  return (
    <Drawer
      open={open}
      onClose={onClose}
      title="Protobuf 静态检查规则、标准词库与 AST 防重扫描"
      width={960}
    >
      <div className="plint-drawer">
        <div className="plint-tabs">
          <div className="plint-tabs__nav" role="tablist" aria-label="静态检查控制视图">
            <button
              type="button"
              role="tab"
              aria-selected={subTab === 'rules'}
              className={`plint-tab ${subTab === 'rules' ? 'plint-tab--active' : ''}`}
              onClick={() => {
                setPage(1);
                setSubTab('rules');
              }}
            >
              <ShieldAlert size={14} /> 规则矩阵 ({rulesTotal})
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={subTab === 'vocab'}
              className={`plint-tab ${subTab === 'vocab' ? 'plint-tab--active' : ''}`}
              onClick={() => {
                setPage(1);
                setSubTab('vocab');
              }}
            >
              <BookCheck size={14} /> 标准词汇词根库 ({vocabTotal})
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={subTab === 'report'}
              className={`plint-tab ${subTab === 'report' ? 'plint-tab--active' : ''}`}
              onClick={() => setSubTab('report')}
            >
              <FileSearch size={14} /> 体检报告与 AST 重复簇
            </button>
          </div>
          {!isAdmin && <span className="plint-hint">当前为只读/可发起扫描权限（修改规则与词库需管理员）</span>}
        </div>

        {errorMsg && <div className="plint-banner plint-banner--error">{errorMsg}</div>}
        {bannerMsg && <div className="plint-banner">{bannerMsg}</div>}

        {subTab === 'rules' && (
          <>
            <div className="plint-toolbar">
              <select
                className="code-input"
                aria-label="规则分类过滤"
                value={ruleCategoryFilter}
                onChange={(e) => {
                  setRuleCategoryFilter(e.target.value);
                  setPage(1);
                }}
              >
                <option value="">全部分类</option>
                <option value="PACKAGE">PACKAGE</option>
                <option value="NAMING">NAMING</option>
                <option value="COMMENT">COMMENT</option>
                <option value="STYLE">STYLE</option>
                <option value="VOCAB">VOCAB</option>
                <option value="DUPLICATE">DUPLICATE</option>
              </select>
              {isAdmin && (
                <button
                  type="button"
                  className="btn btn-primary"
                  onClick={() => setShowRuleCreate((prev) => !prev)}
                >
                  <Plus size={14} /> 新增规则
                </button>
              )}
            </div>

            {showRuleCreate && isAdmin && (
              <form className="plint-form" onSubmit={(e) => void handleCreateRule(e)}>
                <input
                  className="code-input"
                  required
                  placeholder="规则编码（如 CUSTOM_REQ_SUFFIX）"
                  value={newRuleCode}
                  onChange={(e) => setNewRuleCode(e.target.value)}
                />
                <input
                  className="code-input"
                  required
                  placeholder="规则名称"
                  value={newRuleName}
                  onChange={(e) => setNewRuleName(e.target.value)}
                />
                <select className="code-input" value={newRuleCategory} onChange={(e) => setNewRuleCategory(e.target.value)}>
                  <option value="NAMING">NAMING</option>
                  <option value="PACKAGE">PACKAGE</option>
                  <option value="STYLE">STYLE</option>
                  <option value="COMMENT">COMMENT</option>
                </select>
                <select className="code-input" value={newRuleScope} onChange={(e) => setNewRuleScope(e.target.value)}>
                  <option value="MESSAGE">MESSAGE</option>
                  <option value="FIELD">FIELD</option>
                  <option value="RPC">RPC</option>
                  <option value="SERVICE">SERVICE</option>
                  <option value="PACKAGE">PACKAGE</option>
                  <option value="ENUM_VALUE">ENUM_VALUE</option>
                </select>
                <select
                  className="code-input"
                  value={newRuleSeverity}
                  onChange={(e) => setNewRuleSeverity(e.target.value as RuleSeverity)}
                >
                  <option value="ERROR">ERROR</option>
                  <option value="WARNING">WARNING</option>
                  <option value="INFO">INFO</option>
                </select>
                <input
                  className="code-input"
                  required
                  placeholder="正则表达式 Pattern"
                  value={newRulePattern}
                  onChange={(e) => setNewRulePattern(e.target.value)}
                />
                <input
                  className="code-input"
                  placeholder="修复建议模板（支持 {symbol} {expected}）"
                  value={newRuleSuggestion}
                  onChange={(e) => setNewRuleSuggestion(e.target.value)}
                />
                <div className="plint-form__full">
                  <button type="button" className="btn btn-secondary" onClick={() => setShowRuleCreate(false)}>
                    取消
                  </button>
                  <button type="submit" className="btn btn-primary">
                    保存规则
                  </button>
                </div>
              </form>
            )}

            <div className="plint-table-wrap">
              <table className="plint-table">
                <thead>
                  <tr>
                    <th>规则编码与说明</th>
                    <th>作用范围</th>
                    <th>模式</th>
                    <th>告警级别</th>
                    <th>启用</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {rulesLoading ? (
                    <tr>
                      <td colSpan={6}>正在加载检查规则...</td>
                    </tr>
                  ) : rules.length === 0 ? (
                    <tr>
                      <td colSpan={6}>
                        <EmptyState title="暂无匹配的静态检查规则" />
                      </td>
                    </tr>
                  ) : (
                    rules.map((rule) => (
                      <tr key={rule.id}>
                        <td>
                          <div>
                            <strong>{rule.rule_code}</strong> · {rule.name}
                          </div>
                          <div className="plint-hint">{rule.description}</div>
                        </td>
                        <td>
                          <span className="code-badge">{rule.category}</span> / {rule.target_scope}
                        </td>
                        <td>{rule.check_mode}</td>
                        <td>
                          {isAdmin ? (
                            <select
                              className="code-input"
                              aria-label={`级别-${rule.rule_code}`}
                              value={rule.severity}
                              onChange={(e) => void handleChangeRuleSeverity(rule, e.target.value as RuleSeverity)}
                            >
                              <option value="ERROR">ERROR</option>
                              <option value="WARNING">WARNING</option>
                              <option value="INFO">INFO</option>
                            </select>
                          ) : (
                            <span className={SEVERITY_BADGE[rule.severity] ?? 'code-badge'}>{rule.severity}</span>
                          )}
                        </td>
                        <td>
                          <input
                            type="checkbox"
                            aria-label={`启用-${rule.rule_code}`}
                            checked={rule.is_enabled}
                            disabled={!isAdmin}
                            onChange={() => void handleToggleRuleEnabled(rule)}
                          />
                        </td>
                        <td>
                          {rule.is_builtin ? (
                            <span className="plint-hint">内置不可删</span>
                          ) : isAdmin ? (
                            confirmDeleteRuleId === rule.id ? (
                              <span className="plint-actions-cell">
                                <button
                                  type="button"
                                  className="btn btn-danger"
                                  onClick={() => void handleDeleteRule(rule.id)}
                                >
                                  确认
                                </button>
                                <button
                                  type="button"
                                  className="btn btn-secondary"
                                  onClick={() => setConfirmDeleteRuleId(null)}
                                >
                                  取消
                                </button>
                              </span>
                            ) : (
                              <button
                                type="button"
                                className="btn btn-secondary"
                                onClick={() => setConfirmDeleteRuleId(rule.id)}
                              >
                                <Trash2 size={13} /> 删除
                              </button>
                            )
                          ) : (
                            <span className="plint-hint">-</span>
                          )}
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>

            {rulesTotal > 0 && (
              <div className="plint-pagination">
                <Pagination
                  page={page}
                  pageSize={pageSize}
                  totalItems={rulesTotal}
                  onPageChange={setPage}
                  onPageSizeChange={setPageSize}
                />
              </div>
            )}
          </>
        )}

        {subTab === 'vocab' && (
          <>
            <div className="plint-toolbar">
              <input
                className="code-input"
                placeholder="检索词根 / 别名 / 中文释义"
                value={vocabSearch}
                onChange={(e) => {
                  setVocabSearch(e.target.value);
                  setPage(1);
                }}
              />
              <select
                className="code-input"
                aria-label="词库来源过滤"
                value={vocabSourceFilter}
                onChange={(e) => {
                  setVocabSourceFilter(e.target.value);
                  setPage(1);
                }}
              >
                <option value="">全部来源</option>
                <option value="BUILTIN_SEED">BUILTIN_SEED</option>
                <option value="MANUAL">MANUAL</option>
                <option value="LLM_SUGGESTED">LLM_SUGGESTED</option>
              </select>
              {isAdmin && (
                <button
                  type="button"
                  className="btn btn-primary"
                  onClick={() => setShowVocabCreate((prev) => !prev)}
                >
                  <Plus size={14} /> 录入标准词根
                </button>
              )}
            </div>

            {showVocabCreate && isAdmin && (
              <form className="plint-form" onSubmit={(e) => void handleCreateVocab(e)}>
                <input
                  className="code-input"
                  required
                  placeholder="标准词根（如 acct）"
                  value={newTerm}
                  onChange={(e) => setNewTerm(e.target.value)}
                />
                <input
                  className="code-input"
                  placeholder="英文全称（如 Account）"
                  value={newTermEn}
                  onChange={(e) => setNewTermEn(e.target.value)}
                />
                <input
                  className="code-input"
                  required
                  placeholder="中文业务释义"
                  value={newTermZh}
                  onChange={(e) => setNewTermZh(e.target.value)}
                />
                <input
                  className="code-input"
                  placeholder="常见非标别名（逗号分隔，如 acnt,acc_no）"
                  value={newTermAliases}
                  onChange={(e) => setNewTermAliases(e.target.value)}
                />
                <select className="code-input" value={newTermDomain} onChange={(e) => setNewTermDomain(e.target.value)}>
                  <option value="GENERAL">GENERAL</option>
                  <option value="FINANCE">FINANCE</option>
                  <option value="TRADE">TRADE</option>
                  <option value="RISK">RISK</option>
                </select>
                <div className="plint-form__full">
                  <button type="button" className="btn btn-secondary" onClick={() => setShowVocabCreate(false)}>
                    取消
                  </button>
                  <button type="submit" className="btn btn-primary">
                    确认录入
                  </button>
                </div>
              </form>
            )}

            <div className="plint-table-wrap">
              <table className="plint-table">
                <thead>
                  <tr>
                    <th>标准词根</th>
                    <th>语义释义</th>
                    <th>拦截非标别名</th>
                    <th>来源与审核状态</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {vocabLoading ? (
                    <tr>
                      <td colSpan={5}>正在加载标准词汇库...</td>
                    </tr>
                  ) : vocabTerms.length === 0 ? (
                    <tr>
                      <td colSpan={5}>
                        <EmptyState title="暂无符合条件标准词根" />
                      </td>
                    </tr>
                  ) : (
                    vocabTerms.map((term) => (
                      <tr key={term.id}>
                        <td>
                          <strong>{term.term}</strong>
                          <div className="plint-hint">{term.domain}</div>
                        </td>
                        <td>
                          <div>{term.description_zh}</div>
                          {term.description_en && <div className="plint-hint">{term.description_en}</div>}
                        </td>
                        <td>
                          {(term.aliases ?? []).length > 0 ? term.aliases.join(', ') : <span className="plint-hint">无</span>}
                        </td>
                        <td>
                          <div>
                            <span className="code-badge">{term.source}</span>
                          </div>
                          <div className="plint-hint">{term.is_approved ? '已审核标准' : '待管理员批准'}</div>
                        </td>
                        <td>
                          <div className="plint-actions-cell">
                            {isAdmin && !term.is_approved && (
                              <button
                                type="button"
                                className="btn btn-primary"
                                onClick={() => void handleApproveVocab(term)}
                              >
                                <CheckCircle2 size={13} /> 纳为标准
                              </button>
                            )}
                            {isAdmin &&
                              (confirmDeleteVocabId === term.id ? (
                                <>
                                  <button
                                    type="button"
                                    className="btn btn-danger"
                                    onClick={() => void handleDeleteVocab(term.id)}
                                  >
                                    确认
                                  </button>
                                  <button
                                    type="button"
                                    className="btn btn-secondary"
                                    onClick={() => setConfirmDeleteVocabId(null)}
                                  >
                                    取消
                                  </button>
                                </>
                              ) : (
                                <button
                                  type="button"
                                  className="btn btn-secondary"
                                  onClick={() => setConfirmDeleteVocabId(term.id)}
                                >
                                  删除
                                </button>
                              ))}
                          </div>
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>

            {vocabTotal > 0 && (
              <div className="plint-pagination">
                <Pagination
                  page={page}
                  pageSize={pageSize}
                  totalItems={vocabTotal}
                  onPageChange={setPage}
                  onPageSizeChange={setPageSize}
                />
              </div>
            )}
          </>
        )}

        {subTab === 'report' && (
          <>
            <div className="plint-toolbar">
              <button
                type="button"
                className="btn btn-primary"
                disabled={scanRunning}
                onClick={() => void handleTriggerScan()}
              >
                <Play size={14} /> {scanRunning ? '正在体检与 AST 去重比对...' : '发起全量体检扫描'}
              </button>
              <label className="plint-inline-check">
                <input
                  type="checkbox"
                  checked={includeDuplicates}
                  onChange={(e) => setIncludeDuplicates(e.target.checked)}
                />
                启用三护栏 AST 重复探测 (Jaccard &gt;= 0.85)
              </label>
              <label className="plint-inline-check">
                <input
                  type="checkbox"
                  checked={includeLLM}
                  onChange={(e) => setIncludeLLM(e.target.checked)}
                />
                启用 3s 熔断 LLM 辅助识别
              </label>
              {recentReports.length > 0 && (
                <select
                  className="code-input"
                  aria-label="切换历史检查报告"
                  value={activeReport?.id ?? ''}
                  onChange={(e) => {
                    const id = Number(e.target.value);
                    if (id > 0) void handleSelectReport(id);
                  }}
                >
                  {recentReports.map((rep) => (
                    <option key={rep.id} value={rep.id}>
                      报告 #{rep.id} · {rep.status} ({rep.error_count}E / {rep.warning_count}W)
                    </option>
                  ))}
                </select>
              )}
            </div>

            {!activeReport ? (
              <EmptyState title="尚未产生静态检查报告，请点击上方“发起全量体检扫描”" />
            ) : (
              <>
                <div className="plint-metrics">
                  <div className="plint-metric">
                    <div className="plint-metric__label">总体结论</div>
                    <div className="plint-metric__val">
                      <span className={REPORT_STATUS_BADGE[activeReport.status] ?? 'code-badge'}>
                        {activeReport.status}
                      </span>
                    </div>
                  </div>
                  <div className="plint-metric">
                    <div className="plint-metric__label">ERROR 阻断项</div>
                    <div className="plint-metric__val">{activeReport.error_count}</div>
                  </div>
                  <div className="plint-metric">
                    <div className="plint-metric__label">WARNING 警告项</div>
                    <div className="plint-metric__val">{activeReport.warning_count}</div>
                  </div>
                  <div className="plint-metric">
                    <div className="plint-metric__label">AST 重复结构体簇</div>
                    <div className="plint-metric__val">{activeReport.duplicate_group_count}</div>
                  </div>
                  <div className="plint-metric">
                    <div className="plint-metric__label">LLM 降级状态 / 耗时</div>
                    <div className="plint-metric__val">{activeReport.llm_status}</div>
                    <div className="plint-hint">{activeReport.duration_ms} ms</div>
                  </div>
                </div>

                {activeReport.llm_status === 'TIMEOUT_FALLBACK' && (
                  <div className="plint-banner plint-banner--warn">
                    LLM 辅助识别已触发 3 秒超时熔断（TIMEOUT_FALLBACK），当前已完整保留全部静态规则与 AST 重复探测结果。
                  </div>
                )}

                {(activeReport.duplicate_groups ?? []).length > 0 && (
                  <div className="plint-dup-grid">
                    <h4>跨服务高相似度重复结构体簇（Jaccard &gt;= 85%）</h4>
                    {(activeReport.duplicate_groups ?? []).map((group) => (
                      <div key={group.cluster_hash} className="plint-dup-card">
                        <div className="plint-dup-card__head">
                          <strong>
                            重复簇 #{group.cluster_hash} · 相似度 {(group.similarity * 100).toFixed(1)}%
                          </strong>
                          <span className="code-badge code-badge-warning">建议抽离至 BASE_LIB</span>
                        </div>
                        <div className="plint-hint">{group.suggestion}</div>
                        <div className="plint-dup-card__members">
                          {group.members.map((m) => (
                            <div key={`${m.proto_file}:${m.message}:${m.file_line}`} className="plint-dup-member">
                              <span>
                                <strong>{m.package}.{m.message}</strong> ({m.field_cnt} 个字段)
                              </span>
                              <button
                                type="button"
                                className="plint-link-btn"
                                onClick={() => onJumpToSource?.(m.proto_file, m.file_line)}
                              >
                                {m.proto_file}#L{m.file_line}
                              </button>
                            </div>
                          ))}
                        </div>
                      </div>
                    ))}
                  </div>
                )}

                <div className="plint-table-wrap">
                  <table className="plint-table">
                    <thead>
                      <tr>
                        <th>级别 / 规则</th>
                        <th>文件与精确行锚点</th>
                        <th>问题说明与修改建议</th>
                      </tr>
                    </thead>
                    <tbody>
                      {(activeReport.violations ?? []).length === 0 ? (
                        <tr>
                          <td colSpan={3}>未发现静态规范违规或重复定义。</td>
                        </tr>
                      ) : (
                        (activeReport.violations ?? []).map((v) => (
                          <tr key={`${v.rule_code}-${v.proto_file}-${v.symbol_path}-${v.file_line}`}>
                            <td>
                              <span className={SEVERITY_BADGE[v.severity] ?? 'code-badge'}>{v.severity}</span>
                              <div>
                                <strong>{v.rule_code}</strong>
                              </div>
                              <div className="plint-hint">{v.suggestion_source}</div>
                            </td>
                            <td>
                              <button
                                type="button"
                                className="plint-link-btn"
                                onClick={() => onJumpToSource?.(v.proto_file, v.file_line)}
                              >
                                {v.proto_file}#L{v.file_line}
                              </button>
                              <div className="plint-hint">{v.symbol_path}</div>
                            </td>
                            <td>
                              <div>{v.message}</div>
                              <div className="plint-hint">建议：{v.suggestion}</div>
                            </td>
                          </tr>
                        ))
                      )}
                    </tbody>
                  </table>
                </div>
              </>
            )}
          </>
        )}
      </div>
    </Drawer>
  );
};

export default LintRuleAndVocabDrawer;
