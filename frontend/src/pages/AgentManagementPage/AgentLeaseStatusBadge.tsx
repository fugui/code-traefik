import React from 'react';
import type { Agent } from '../../types';

export interface AgentLeaseStatusBadgeProps {
  agent: Agent;
  isAdmin: boolean;
  onForceUnlock?: (agentId: string) => void;
}

export const AgentLeaseStatusBadge: React.FC<AgentLeaseStatusBadgeProps> = ({
  agent,
  isAdmin,
  onForceUnlock,
}) => {
  if (!agent.lease_type) {
    return <span className="code-badge code-badge--success">空闲 (IDLE)</span>;
  }

  const isRecording = agent.lease_type === 'RECORDING';
  const badgeClass = isRecording ? 'code-badge--warning' : 'code-badge--primary';
  const typeText = isRecording ? '录制中' : '重放中';

  return (
    <div className="code-lease-badge-wrap">
      <span className={`code-badge ${badgeClass}`} title={`租约持有人: ${agent.lease_holder_name || '未知'}`}>
        {typeText} ({agent.lease_holder_name || '持锁'})
      </span>

      {isAdmin && onForceUnlock && (
        <button
          type="button"
          className="code-btn-text code-color-danger code-text-xs"
          onClick={() => onForceUnlock(agent.id)}
          title="管理员强制清空数据库排他租约并向 Agent 下发 purge 自洁"
        >
          强制解锁
        </button>
      )}
    </div>
  );
};
