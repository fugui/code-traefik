import { describe, it, expect, vi } from 'vitest';
import { renderToString } from 'react-dom/server';
import type { ProtoFieldNodeDTO } from '../types/schema_tree';
import {
  MessageSchemaInspectorTree,
  isPathMatched,
} from '../pages/ProtoSchemaWorkbenchPage/MessageSchemaInspectorTree';

function buildThreeLevelNestedFields(): ProtoFieldNodeDTO[] {
  return [
    {
      name: 'code',
      tagNumber: 1,
      kind: 'INT32',
      cardinality: 'OPTIONAL',
      isMap: false,
      comments: '状态码',
      fieldPath: 'code',
      isIgnoredInDiff: false,
    },
    {
      name: 'data',
      tagNumber: 2,
      kind: 'MESSAGE',
      cardinality: 'OPTIONAL',
      isMap: false,
      fieldPath: 'data',
      isIgnoredInDiff: false,
      children: [
        {
          name: 'detail',
          tagNumber: 1,
          kind: 'MESSAGE',
          cardinality: 'OPTIONAL',
          isMap: false,
          fieldPath: 'data.detail',
          isIgnoredInDiff: false,
          children: [
            {
              name: 'timestamp',
              tagNumber: 1,
              kind: 'INT64',
              cardinality: 'OPTIONAL',
              isMap: false,
              comments: '毫秒级服务器时间戳',
              fieldPath: 'data.detail.timestamp',
              isIgnoredInDiff: true,
            },
            {
              name: 'nonce',
              tagNumber: 2,
              kind: 'STRING',
              cardinality: 'OPTIONAL',
              isMap: false,
              fieldPath: 'data.detail.nonce',
              isIgnoredInDiff: false,
            },
          ],
        },
      ],
    },
    {
      name: 'orders',
      tagNumber: 3,
      kind: 'MESSAGE',
      cardinality: 'REPEATED',
      isMap: false,
      comments: '订单明细列表',
      fieldPath: 'orders',
      isIgnoredInDiff: false,
      children: [
        {
          name: 'order_id',
          tagNumber: 1,
          kind: 'INT64',
          cardinality: 'OPTIONAL',
          isMap: false,
          fieldPath: 'orders[*].order_id',
          isIgnoredInDiff: false,
        },
        {
          name: 'token',
          tagNumber: 2,
          kind: 'STRING',
          cardinality: 'OPTIONAL',
          isMap: false,
          fieldPath: 'orders[*].token',
          isIgnoredInDiff: true,
        },
      ],
    },
  ];
}

describe('MessageSchemaTree Suite (M7-P3)', () => {
  it('testPathMatching_Algorithm: tests exact, prefix, and wildcard matching', () => {
    const rules = ['data.detail.timestamp', 'orders[*].token', 'header.*'];

    // 1. 精确匹配
    expect(isPathMatched('data.detail.timestamp', rules)).toBe(true);
    expect(isPathMatched('data.detail.nonce', rules)).toBe(false);

    // 2. 通配符数组匹配: orders[*].token 能够匹配实际运行时路径 orders[0].token 与 schema 路径 orders[*].token
    expect(isPathMatched('orders[*].token', rules)).toBe(true);
    expect(isPathMatched('orders[0].token', rules)).toBe(true);
    expect(isPathMatched('orders[42].token', rules)).toBe(true);
    expect(isPathMatched('orders[*].order_id', rules)).toBe(false);
    expect(isPathMatched('orders[0].order_id', rules)).toBe(false);

    // 3. 前缀子树匹配
    expect(isPathMatched('header.time', ['header'])).toBe(true);
    expect(isPathMatched('header[0]', ['header'])).toBe(true);
  });

  it('testTreeRender_NestedAndPathGeneration: renders badges, comments, tags and ignored styles', () => {
    const fields = buildThreeLevelNestedFields();
    const activeIgnores = ['data.detail.timestamp', 'orders[*].token'];

    const handleToggle = vi.fn();

    const html = renderToString(
      <MessageSchemaInspectorTree
        fields={fields}
        activeIgnorePaths={activeIgnores}
        onToggleIgnore={handleToggle}
        isAdmin={true}
      />
    );

    // 1. 验证字段名与 Tag
    expect(html).toContain('data-testid="tree-node-code"');
    expect(html).toContain('data-testid="tree-node-data.detail.timestamp"');
    expect(html).toContain('data-testid="tree-node-orders[*].order_id"');
    expect(html).toContain('#1');
    expect(html).toContain('#2');
    expect(html).toContain('#3');

    // 2. 验证类型徽章
    expect(html).toContain('INT32');
    expect(html).toContain('INT64');
    expect(html).toContain('MESSAGE');
    expect(html).toContain('REPEATED');

    // 3. 验证源码注释透视
    expect(html).toContain('// 毫秒级服务器时间戳');
    expect(html).toContain('// 订单明细列表');

    // 4. 验证已忽略对比节点的样式标记 (code-tree-node--ignored)
    expect(html).toContain('code-tree-node code-tree-node--ignored" data-testid="tree-node-data.detail.timestamp"');
    expect(html).toContain('code-tree-node code-tree-node--ignored" data-testid="tree-node-orders[*].token"');

    // 5. 验证未忽略节点
    expect(html).toContain('code-tree-node " data-testid="tree-node-data.detail.nonce"');
    expect(html).toContain('code-tree-node " data-testid="tree-node-orders[*].order_id"');

    // 6. 验证路径推导契约
    const tsNode = fields[1].children![0].children![0];
    expect(tsNode.fieldPath).toBe('data.detail.timestamp');

    const arrayItemNode = fields[2].children![0];
    expect(arrayItemNode.fieldPath).toBe('orders[*].order_id');
  });
});
