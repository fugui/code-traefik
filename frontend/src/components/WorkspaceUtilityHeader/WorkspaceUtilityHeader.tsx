import React from 'react';
import { BellRing } from 'lucide-react';
import { NotificationBell } from '../NotificationBell';
import { MODULE_TITLE } from '../../constants';

export interface WorkspaceUtilityHeaderProps {
  className?: string;
}

/**
 * 嵌入门户模式（isEmbedded=true）下的轻量模块工具条：
 * 负责在宿主框架内容区顶部挂载双模对称的 NotificationBell 收件箱入口与 SSE 实时角标。
 */
export const WorkspaceUtilityHeader: React.FC<WorkspaceUtilityHeaderProps> = ({ className }) => {
  return (
    <div
      className={className ? `code-traefik-utility-header ${className}` : 'code-traefik-utility-header'}
      data-testid="workspace-utility-header"
    >
      <div className="code-traefik-utility-header__brand">
        <BellRing size={15} />
        <span>{MODULE_TITLE} · 契约变更通知与 MR 门禁雷达</span>
      </div>
      <div className="code-traefik-utility-header__actions">
        <NotificationBell />
      </div>
    </div>
  );
};

export default WorkspaceUtilityHeader;
