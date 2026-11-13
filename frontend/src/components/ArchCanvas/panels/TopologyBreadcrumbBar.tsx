import React from 'react';
import { ArrowLeft, ChevronRight, Globe } from 'lucide-react';

export interface FocusBreadcrumbItem {
  id: string;
  name: string;
}

export interface TopologyBreadcrumbBarProps {
  focusScope: string;
  breadcrumbs: FocusBreadcrumbItem[];
  onNavigateScope: (scope: string) => void;
  containerCount?: number;
  moduleCount?: number;
  edgeCount?: number;
  externalCount?: number;
  className?: string;
}

/**
 * 拓扑画布顶置面包屑导航条 (M21-P4)：
 * - 左侧：[← 返回上一级] 按钮 + 可点击层级链 (L0 > L1 > L2)
 * - 右侧：当前单层视图节点/连线计数胶囊 + [🌐 返回全局]
 */
export const TopologyBreadcrumbBar: React.FC<TopologyBreadcrumbBarProps> = ({
  focusScope,
  breadcrumbs,
  onNavigateScope,
  containerCount,
  moduleCount,
  edgeCount,
  externalCount,
  className = '',
}) => {
  const isAtGlobal = !focusScope || focusScope === 'GLOBAL';
  const effectiveBreadcrumbs: FocusBreadcrumbItem[] =
    breadcrumbs.length > 0
      ? breadcrumbs
      : isAtGlobal
      ? [{ id: 'GLOBAL', name: '全局全景' }]
      : [
          { id: 'GLOBAL', name: '全局全景' },
          { id: focusScope, name: focusScope },
        ];

  const parentScope =
    effectiveBreadcrumbs.length >= 2
      ? effectiveBreadcrumbs[effectiveBreadcrumbs.length - 2].id
      : 'GLOBAL';

  const statsParts: string[] = [];
  if (typeof containerCount === 'number' && containerCount > 0) {
    statsParts.push(`${containerCount} 容器`);
  }
  if (typeof moduleCount === 'number' && moduleCount > 0) {
    statsParts.push(`${moduleCount} 模块`);
  }
  if (typeof externalCount === 'number' && externalCount > 0) {
    statsParts.push(`${externalCount} 外部摘要`);
  }
  if (typeof edgeCount === 'number') {
    statsParts.push(`${edgeCount} 依赖边`);
  }
  const statsSummary = statsParts.length > 0 ? `当前层：${statsParts.join(' · ')}` : '当前单层视图';

  return (
    <div
      className={`code-arch-breadcrumb ${className}`}
      data-testid="arch-breadcrumb-bar"
    >
      <div className="code-arch-breadcrumb__left">
        <button
          type="button"
          className="code-arch-breadcrumb__back-btn"
          disabled={isAtGlobal}
          onClick={() => onNavigateScope(parentScope)}
          title={isAtGlobal ? '已在全局顶层视图' : '返回上一级容器 (Esc / Backspace)'}
          data-testid="breadcrumb-back-btn"
        >
          <ArrowLeft size={12} />
          <span>返回上一级</span>
        </button>

        <nav
          className="code-arch-breadcrumb__trail"
          aria-label="拓扑下钻层级路径"
          data-testid="arch-focus-bar"
        >
          {effectiveBreadcrumbs.map((crumb, idx) => {
            const isLast = idx === effectiveBreadcrumbs.length - 1;
            const levelTag = `L${idx}`;
            return (
              <React.Fragment key={crumb.id}>
                <button
                  type="button"
                  className={`code-arch-breadcrumb__crumb ${
                    isLast ? 'code-arch-breadcrumb__crumb--active' : ''
                  }`}
                  onClick={() => onNavigateScope(crumb.id)}
                  data-testid={`focus-crumb-${crumb.id}`}
                >
                  <span className="code-arch-breadcrumb__crumb-tag">{levelTag}</span>
                  <span>{crumb.name}</span>
                </button>
                {!isLast && <ChevronRight size={12} className="code-arch-breadcrumb__sep" />}
              </React.Fragment>
            );
          })}
        </nav>
      </div>

      <div className="code-arch-breadcrumb__right">
        <span className="code-arch-breadcrumb__stats-pill" data-testid="breadcrumb-stats-pill">
          {statsSummary}
        </span>

        {!isAtGlobal && (
          <button
            type="button"
            className="code-btn code-btn--secondary code-btn--xs"
            onClick={() => onNavigateScope('GLOBAL')}
            data-testid="exit-focus-scope-btn"
            title="回到全局 L0 子系统/进程组视图"
          >
            <Globe size={12} />
            <span>返回全局</span>
          </button>
        )}
      </div>
    </div>
  );
};
