import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import LintRuleAndVocabDrawer from '../LintRuleAndVocabDrawer';
import type { CheckReportDTO, CheckRuleDTO, VocabularyTermDTO } from '../../../types/schema';

const SAMPLE_RULES: CheckRuleDTO[] = [
  {
    id: 1,
    rule_code: 'PKG_NAMING',
    name: '多段小写包名规范',
    category: 'PACKAGE',
    target_scope: 'PACKAGE',
    severity: 'ERROR',
    check_mode: 'REGEX',
    pattern: '^[a-z].*$',
    description: '支持多段包名 dophi.order.v1',
    suggestion_template: '建议使用 {example}',
    exemption_rule: {},
    is_builtin: true,
    is_enabled: true,
    created_at: '2026-11-14T08:00:00Z',
    updated_at: '2026-11-14T08:00:00Z',
  },
  {
    id: 2,
    rule_code: 'COMMENT_REQUIRED',
    name: 'Service/RPC 前导注释要求',
    category: 'COMMENT',
    target_scope: 'RPC',
    severity: 'WARNING',
    check_mode: 'COMMENT',
    pattern: '',
    description: '默认 WARNING 不阻塞历史基线',
    suggestion_template: '请为 {symbol} 补充注释',
    exemption_rule: {},
    is_builtin: true,
    is_enabled: true,
    created_at: '2026-11-14T08:00:00Z',
    updated_at: '2026-11-14T08:00:00Z',
  },
  {
    id: 10,
    rule_code: 'CUSTOM_SUFFIX_CHK',
    name: '自定义命名尾缀检查',
    category: 'STYLE',
    target_scope: 'MESSAGE',
    severity: 'INFO',
    check_mode: 'REGEX',
    pattern: '^[A-Z].*$',
    description: '团队可选扩展规则',
    suggestion_template: '调整 {symbol}',
    exemption_rule: {},
    is_builtin: false,
    is_enabled: false,
    created_at: '2026-11-14T08:00:00Z',
    updated_at: '2026-11-14T08:00:00Z',
  },
];

const SAMPLE_VOCAB: VocabularyTermDTO[] = [
  {
    id: 1,
    term: 'amt',
    description_en: 'Amount',
    description_zh: '金额/数额',
    aliases: ['amnt', 'amt_val'],
    similar_terms: ['amount'],
    domain: 'FINANCE',
    source: 'BUILTIN_SEED',
    is_approved: true,
    created_at: '2026-11-14T08:00:00Z',
    updated_at: '2026-11-14T08:00:00Z',
  },
  {
    id: 99,
    term: 'xkz',
    description_en: 'Risk Key Zone',
    description_zh: '风控识别区码',
    aliases: ['xkz_cd'],
    similar_terms: [],
    domain: 'GENERAL',
    source: 'LLM_SUGGESTED',
    is_approved: false,
    created_at: '2026-11-14T08:00:00Z',
    updated_at: '2026-11-14T08:00:00Z',
  },
];

const SAMPLE_REPORT: CheckReportDTO = {
  id: 7,
  trigger_source: 'MANUAL',
  status: 'WARN',
  error_count: 0,
  warning_count: 2,
  info_count: 1,
  duplicate_group_count: 1,
  llm_status: 'TIMEOUT_FALLBACK',
  duration_ms: 3012,
  created_at: '2026-11-14T08:00:00Z',
  violations: [
    {
      id: 101,
      report_id: 7,
      rule_code: 'VOCAB_STANDARD_TERM',
      severity: 'WARNING',
      proto_file: 'dophi/trade/v1/trade.proto',
      symbol_path: 'dophi.trade.v1.TradeSummary.cust_ord_amnt',
      file_line: 14,
      file_column: 3,
      message: '字段 cust_ord_amnt 中的词根 "amnt" 为非标准别名',
      suggestion: '建议替换为标准词根 amt',
      suggestion_source: 'VOCAB_MATCH',
    },
  ],
  duplicate_groups: [
    {
      cluster_hash: 'deadbeef1234',
      similarity: 0.923,
      suggestion: '建议抽取公共结构体沉淀至 BASE_LIB',
      members: [
        { proto_file: 'dophi/trade/v1/trade.proto', package: 'dophi.trade.v1', message: 'TradeSummary', file_line: 10, field_cnt: 6 },
        { proto_file: 'dophi/clearing/v1/clearing.proto', package: 'dophi.clearing.v1', message: 'DealSummary', file_line: 8, field_cnt: 6 },
      ],
    },
  ],
};

describe('LintRuleAndVocabDrawer M24 UI', () => {
  it('应渲染规则矩阵与内置保护标识（COMMENT_REQUIRED 默认 WARNING）', () => {
    const html = renderToStaticMarkup(
      <MemoryRouter>
        <LintRuleAndVocabDrawer
          open
          isAdmin
          initialSubTab="rules"
          initialRules={SAMPLE_RULES}
          initialRulesTotal={3}
          onClose={() => {}}
        />
      </MemoryRouter>
    );

    expect(html).toContain('PKG_NAMING');
    expect(html).toContain('COMMENT_REQUIRED');
    expect(html).toContain('WARNING');
    expect(html).toContain('内置不可删');
    expect(html).toContain('CUSTOM_SUFFIX_CHK');
    expect(html).toContain('新增规则');
  });

  it('应渲染标准词库并展示 LLM_SUGGESTED 一键纳为标准操作按钮', () => {
    const html = renderToStaticMarkup(
      <MemoryRouter>
        <LintRuleAndVocabDrawer
          open
          isAdmin
          initialSubTab="vocab"
          initialVocab={SAMPLE_VOCAB}
          initialVocabTotal={2}
          onClose={() => {}}
        />
      </MemoryRouter>
    );

    expect(html).toContain('amt');
    expect(html).toContain('BUILTIN_SEED');
    expect(html).toContain('xkz');
    expect(html).toContain('LLM_SUGGESTED');
    expect(html).toContain('纳为标准');
  });

  it('应渲染体检报告、TIMEOUT_FALLBACK 提示与 AST 重复簇对比卡片', () => {
    const html = renderToStaticMarkup(
      <MemoryRouter>
        <LintRuleAndVocabDrawer
          open
          isAdmin={false}
          initialSubTab="report"
          initialReport={SAMPLE_REPORT}
          onClose={() => {}}
        />
      </MemoryRouter>
    );

    expect(html).toContain('TIMEOUT_FALLBACK');
    expect(html).toContain('TradeSummary');
    expect(html).toContain('DealSummary');
    expect(html).toContain('92.3%');
    expect(html).toContain('dophi/trade/v1/trade.proto#L14');
    expect(html).toContain('发起全量体检扫描');
    expect(html).not.toContain('新增规则');
  });
});
