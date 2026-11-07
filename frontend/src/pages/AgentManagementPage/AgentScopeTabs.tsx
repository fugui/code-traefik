import React from 'react';

export type AgentScopeType = 'SHARED' | 'PRIVATE' | 'INSTALL';

export interface AgentScopeTabsProps {
  currentScope: AgentScopeType;
  onChangeScope: (scope: AgentScopeType) => void;
  sharedCount?: number;
  privateCount?: number;
}

export const AgentScopeTabs: React.FC<AgentScopeTabsProps> = ({
  currentScope,
  onChangeScope,
  sharedCount,
  privateCount,
}) => {
  return (
    <div className="code-scope-tabs" data-testid="agent-scope-tabs">
      <button
        type="button"
        className={`code-scope-tab ${currentScope === 'SHARED' ? 'code-scope-tab--active' : ''}`}
        onClick={() => onChangeScope('SHARED')}
        data-testid="tab-scope-shared"
      >
        <span className="code-scope-tab__title">团队公共 Agent (SHARED)</span>
        {sharedCount !== undefined && <span className="code-badge code-badge--secondary">{sharedCount}</span>}
      </button>

      <button
        type="button"
        className={`code-scope-tab ${currentScope === 'PRIVATE' ? 'code-scope-tab--active' : ''}`}
        onClick={() => onChangeScope('PRIVATE')}
        data-testid="tab-scope-private"
      >
        <span className="code-scope-tab__title">我的私有 Agent (PRIVATE)</span>
        {privateCount !== undefined && <span className="code-badge code-badge--info">{privateCount}</span>}
      </button>

      <button
        type="button"
        className={`code-scope-tab ${currentScope === 'INSTALL' ? 'code-scope-tab--active' : ''}`}
        onClick={() => onChangeScope('INSTALL')}
        data-testid="tab-scope-install"
      >
        <span className="code-scope-tab__title">📥 Agent 下载与接入说明 (INSTALL)</span>
      </button>
    </div>
  );
};
