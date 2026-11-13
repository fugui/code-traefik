import { describe, it, expect } from 'vitest';
import { renderToString } from 'react-dom/server';
import { ProtoReadOnlyCodeViewer, tokenizeProtoLine } from '../ProtoReadOnlyCodeViewer';
import { ProtoCommitDiffView } from '../ProtoCommitDiffView';

const sampleProtoSource = `syntax = "proto3";
package demo.order.v1;
// 恶意脚本注入测试: <script data-xss="1">console.log("xss")</script>
service OrderService {
  rpc CreateOrder(CreateOrderReq) returns (CreateOrderRsp);
}`;

describe('M23-P3 ProtoReadOnlyCodeViewer & ProtoCommitDiffView', () => {
  it('testReadOnlyCodeViewer_LineAnchorViolationWavyAndThemeTokens', () => {
    const html = renderToString(
      <ProtoReadOnlyCodeViewer
        filePath="order/v1/order.proto"
        content={sampleProtoSource}
        highlightLine={4}
        versionLabel="v2 (ONLINE_COMPILE)"
        hasSourceInfo={true}
        violations={[
          {
            rule_code: 'COMMENT_REQUIRED',
            severity: 'WARNING',
            message: '建议补充标准 中文注释 与 @error_code 标签',
            line: 3,
          },
        ]}
        symbolAnchors={[
          {
            symbol_kind: 'SERVICE',
            symbol_full_name: 'demo.order.v1.OrderService',
            start_line: 4,
            end_line: 6,
          },
        ]}
      />
    );

    // 1. #L4 高亮与行号锚点
    expect(html).toContain('id="L4"');
    expect(html).toContain('data-line="4"');
    expect(html).toContain('pdoc-code-line--highlighted');
    expect(html).toContain('#L4');

    // 2. 第 3 行波浪线告警与诊断展示
    expect(html).toContain('pdoc-code-line__wavy--warning');
    expect(html).toContain('COMMENT_REQUIRED');

    // 3. 严格只读：禁止 textarea / contenteditable，防止浏览器触发误编辑
    expect(html.toLowerCase()).not.toContain('<textarea');
    expect(html.toLowerCase()).not.toContain('contenteditable');

    // 4. React JSX 自动转义 XSS 脚本片段，严禁输出原生 <script>
    expect(html).not.toContain('<script');
    expect(html).toContain('&lt;script data-xss=&quot;1&quot;&gt;console.log(&quot;xss&quot;)&lt;/script&gt;');

    // 5. 纯函数词法着色切片验证
    const tokens = tokenizeProtoLine('  rpc CreateOrder(CreateOrderReq) returns (CreateOrderRsp);');
    expect(tokens.some((tk) => tk.kind === 'keyword' && tk.text === 'rpc')).toBe(true);
    expect(tokens.some((tk) => tk.kind === 'keyword' && tk.text === 'returns')).toBe(true);
  });

  it('renders unified diff cleanly in ProtoCommitDiffView using semantic classes', () => {
    const diffHtml = renderToString(
      <ProtoCommitDiffView
        filePath="order/v1/order.proto"
        fromVersionLabel="v1"
        toVersionLabel="v2"
        addedLines={1}
        deletedLines={1}
        commitSummary="新增订单幂等键字段"
        authorName="alice"
        unifiedDiff={`--- a/order/v1/order.proto@v1
+++ b/order/v1/order.proto@v2
@@ -1,3 +1,3 @@
 message CreateOrderReq {
-  string req_no = 1;
+  string idempotent_key = 1;
 }`}
      />
    );

    expect(diffHtml).toContain('pdoc-diff-row--del');
    expect(diffHtml).toContain('pdoc-diff-row--add');
    expect(diffHtml).toContain('idempotent_key');
    expect(diffHtml).toContain('alice');
    expect(diffHtml.toLowerCase()).not.toContain('<textarea');
  });
});
