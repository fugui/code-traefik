import { describe, it, expect } from 'vitest';
import { renderToString } from 'react-dom/server';
import fs from 'fs';
import path from 'path';
import { SubsystemGroupTreePanel } from '../pages/ArchitectureWorkbenchPage/SubsystemGroupTreePanel';
import type { Subsystem, FunctionalGroupTreeNode, Module } from '../types';

describe('三级逻辑架构元素树 - 顶层子系统严格左对齐门禁', () => {
  const subsystems: Subsystem[] = [
    { id: '1', code: 'ECOMMERCE', name: '电商交易微服务系统' },
    { id: '2', code: 'CLIENT', name: '客户端交互子系统' },
  ];

  const dummyGroups: FunctionalGroupTreeNode[] = [
    {
      id: '1',
      subsystem_id: '1',
      code: 'TRADE_CORE',
      name: '交易中枢与网关',
      sort_order: 1,
      level: 1,
      path: '/1/',
      module_count: 1,
    },
  ];

  const dummyModules: Module[] = [
    {
      id: '1',
      subsystem_id: '1',
      group_id: '1',
      code: 'ORDER_CORE',
      name: '订单交易核心模块',
    },
  ];

  it('两个子系统在折叠状态下，顶层节点容器与按钮样式必须完全同构，严格保持左边距 0', () => {
    const html = renderToString(
      <SubsystemGroupTreePanel
        subsystems={subsystems}
        selectedSubsystemId="1"
        onSelectSubsystem={() => {}}
        groupTree={dummyGroups}
        selectedGroupId="1"
        onSelectGroup={() => {}}
        modules={dummyModules}
        isAdmin={true}
        onCreateSubsystem={() => {}}
        onEditSubsystem={() => {}}
        onDeleteSubsystem={() => {}}
        onCreateGroup={() => {}}
        onEditGroup={() => {}}
        onMoveGroup={() => {}}
        onDeleteGroup={() => {}}
      />
    );

    // 1. 两个子系统均渲染了顶层 wrapper 与 subsystem node
    expect(html).toContain('code-tree-node-wrapper--subsystem');
    expect(html).toContain('code-tree-node--subsystem');

    // 2. 检查两个子系统的 HTML 匹配
    const subsystemMatches = [...html.matchAll(/class="code-tree-node-wrapper code-tree-node-wrapper--subsystem"/g)];
    expect(subsystemMatches.length).toBe(2);

    // 3. 折叠状态下，两个子系统节点分别包含自身的 code-tree-node__left
    const leftContainerMatches = [...html.matchAll(/class="code-tree-node__left">/g)];
    expect(leftContainerMatches.length).toBe(2);

    // 4. 断言顶层子系统节点绝无多余占位符 code-tree-expand-placeholder
    expect(html).not.toContain('code-tree-expand-placeholder');
  });

  it('验证 index.css 中 code-tree-node--subsystem 与 wrapper 拥有严格的零左边距约束，且 code-tree-node 未被全局污染', () => {
    const cssContent = fs.readFileSync(path.resolve(__dirname, '../index.css'), 'utf-8');

    // 1. 验证 .code-tree-node-wrapper--subsystem 与 .code-tree-node--subsystem 明确声明 margin-left: 0
    expect(cssContent).toMatch(/\.code-tree-node-wrapper--subsystem\s*\{[^}]*margin-left:\s*0/);
    expect(cssContent).toMatch(/\.code-tree-node--subsystem\s*\{[^}]*margin-left:\s*0/);

    // 2. 验证 .code-tree-node--subsystem > .code-tree-node__left 与 button 明确声明 margin-left: 0
    expect(cssContent).toMatch(/\.code-tree-node--subsystem\s*>\s*\.code-tree-node__left\s*\{[^}]*margin-left:\s*0/);
    expect(cssContent).toMatch(/\.code-tree-node--subsystem\s+\.code-tree-expand-btn\s*\{[^}]*margin-left:\s*0/);

    // 3. 验证未限定前缀的全局顶层 .code-tree-node 没有被覆盖为 flex-direction: column
    // 确保独立的全局 .code-tree-node 声明方向为 row
    const isolatedGlobalTreeNodeMatches = [...cssContent.matchAll(/(?:^|\})\s*\.code-tree-node\s*\{([^}]+)\}/g)];
    expect(isolatedGlobalTreeNodeMatches.length).toBe(1);
    expect(isolatedGlobalTreeNodeMatches[0][1]).toContain('flex-direction: row');

    // 4. 验证原消息体字典树中的规则均严格收敛在 .code-schema-inspector-tree 作用域下
    expect(cssContent).toContain('.code-schema-inspector-tree .code-tree-node {');
    expect(cssContent).toContain('.code-schema-inspector-tree .code-tree-node__actions {');
  });
});
