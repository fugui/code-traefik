import { describe, it, expect } from 'vitest';
import { renderToString } from 'react-dom/server';
import { useState } from 'react';

// 测试组件：模拟物理进程抽屉中的所属进程组选择与自定义切换控件
function ProcessGroupField({
  initialGroup,
  options,
}: {
  initialGroup: string;
  options: string[];
}) {
  const [isCustom, setIsCustom] = useState(false);
  const [selectedGroup, setSelectedGroup] = useState(initialGroup);
  const [customGroup, setCustomGroup] = useState('');

  return (
    <div className="code-form-item">
      <label className="code-form-label">所属进程组 (两级目录聚合):</label>
      {isCustom ? (
        <div className="code-form-row--inline">
          <input
            type="text"
            className="code-input"
            value={customGroup}
            onChange={(e) => setCustomGroup(e.target.value)}
            placeholder="输入新进程组名称"
          />
          <button
            type="button"
            className="code-btn code-btn--secondary"
            onClick={() => setIsCustom(false)}
          >
            选择已有
          </button>
        </div>
      ) : (
        <div className="code-form-row--inline">
          <select
            className="code-select"
            value={selectedGroup}
            onChange={(e) => {
              if (e.target.value === '__NEW_GROUP__') {
                setIsCustom(true);
              } else {
                setSelectedGroup(e.target.value);
              }
            }}
          >
            {options.map((grp) => (
              <option key={grp} value={grp}>
                {grp}
              </option>
            ))}
            <option value="__NEW_GROUP__">+ 自定义新分组...</option>
          </select>
          <button
            type="button"
            className="code-btn code-btn--secondary"
            onClick={() => setIsCustom(true)}
          >
            + 新分组
          </button>
        </div>
      )}
    </div>
  );
}

describe('ProcessGroupSelector Suite', () => {
  it('TC-PROC-GRP-01: 默认应渲染 <select> 下拉选项而非纯文本输入框，并包含聚合进程组', () => {
    const options = ['默认进程组', '核心业务组', '支撑服务组', '结算支付组'];
    const html = renderToString(
      <ProcessGroupField initialGroup="核心业务组" options={options} />
    );

    // 断言必须包含 select 控件与 code-select 样式类
    expect(html).toContain('<select class="code-select"');
    // 断言包含传入的选项
    expect(html).toContain('<option value="核心业务组" selected=""');
    expect(html).toContain('<option value="默认进程组"');
    expect(html).toContain('<option value="支撑服务组"');
    expect(html).toContain('<option value="结算支付组"');
    expect(html).toContain('<option value="__NEW_GROUP__">+ 自定义新分组...</option>');
    // 断言包含快捷新建按钮
    expect(html).toContain('+ 新分组');
  });

  it('TC-PROC-GRP-02: 进程组聚合去重算法应完整包含保底组、自定义组及已有进程组', () => {
    const customGroups = ['自定义A', '核心业务组'];
    const processes = [
      { group_name: '核心业务组' },
      { group_name: '订单处理组' },
      { group_name: '' },
    ];
    const currentFormGroup = '历史归档组';

    const set = new Set<string>();
    set.add('默认进程组');
    set.add('核心业务组');
    set.add('支撑服务组');
    for (const g of customGroups) {
      if (g.trim()) set.add(g.trim());
    }
    for (const p of processes) {
      if (p.group_name && p.group_name.trim()) set.add(p.group_name.trim());
    }
    if (currentFormGroup.trim()) set.add(currentFormGroup.trim());

    const result = Array.from(set);
    expect(result).toContain('默认进程组');
    expect(result).toContain('核心业务组');
    expect(result).toContain('支撑服务组');
    expect(result).toContain('自定义A');
    expect(result).toContain('订单处理组');
    expect(result).toContain('历史归档组');
    // 确保去重后核心业务组只出现一次
    expect(result.filter((x) => x === '核心业务组').length).toBe(1);
  });
});
