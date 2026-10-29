import React from 'react';

export interface AgentScopeTabsProps {
  currentScope: 'SHARED' | 'PRIVATE';
  onChangeScope: (scope: 'SHARED' | 'PRIVATE') => void;
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
    <div className="code-scope-tabs">
      <button
        type="button"
        className={`code-scope-tab ${currentScope === 'SHARED' ? 'code-scope-tab--active' : ''}`}
        onClick={() => onChangeScope('SHARED')}
      >
        <span className="code-scope-tab__title">团队公共 Agent (SHARED)</span>
        {sharedCount !== undefined && <span className="code-badge code-badge--secondary">{sharedCount}</span>}
      </button>

      <button
        type="button"
        className={`code-scope-tab ${currentScope === 'PRIVATE' ? 'code-scope-tab--active' : ''}`}
        onClick={() => onChangeScope('PRIVATE')}
      >
        <span className="code-scope-tab__title">我的私有 Agent (PRIVATE)</span>
        {privateCount !== undefined && <span className="code-badge code-badge--info">{privateCount}</span>}
      </button>
    </div>
  );
};
