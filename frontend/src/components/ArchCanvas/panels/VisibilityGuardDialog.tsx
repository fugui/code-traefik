import React from 'react';
import { ShieldAlert, Crosshair, EyeOff, AlertTriangle } from 'lucide-react';

export interface VisibilityGuardDialogProps {
  open: boolean;
  estimatedCount: number;
  hardCap?: number;
  recommendedFocusNode?: { id: string; name: string } | null;
  onFocusRecommended: (nodeId: string) => void;
  onEnableHideIsolated: () => void;
  onForceApply: () => void;
  onCancel: () => void;
}

export const VisibilityGuardDialog: React.FC<VisibilityGuardDialogProps> = ({
  open,
  estimatedCount,
  hardCap = 120,
  recommendedFocusNode,
  onFocusRecommended,
  onEnableHideIsolated,
  onForceApply,
  onCancel,
}) => {
  if (!open) return null;

  return (
    <div
      className="code-arch-guard-overlay"
      role="dialog"
      aria-modal="true"
      data-testid="visibility-guard-dialog"
    >
      <div className="code-arch-guard-modal">
        <div className="code-arch-guard-modal__header">
          <ShieldAlert size={18} className="code-arch-guard-modal__icon" />
          <span>可见规模过载拦截守卫</span>
        </div>

        <div className="code-arch-guard-modal__body">
          <p className="code-arch-guard-modal__desc">
            当前操作预计将同时展开渲染 <strong>{estimatedCount}</strong> 个节点，已超过单屏可读性与正交排版硬上限（
            <strong>{hardCap}</strong> 个可见节点）。建议采用分治下钻或降噪过滤以获得清晰拓扑视图：
          </p>

          <div className="code-arch-guard-modal__options">
            {recommendedFocusNode && (
              <button
                type="button"
                className="code-btn code-btn--primary code-arch-guard-modal__option-btn"
                onClick={() => onFocusRecommended(recommendedFocusNode.id)}
                data-testid="guard-focus-recommended-btn"
              >
                <Crosshair size={14} />
                <span>下钻聚焦至「{recommendedFocusNode.name}」</span>
              </button>
            )}

            <button
              type="button"
              className="code-btn code-btn--secondary code-arch-guard-modal__option-btn"
              onClick={onEnableHideIsolated}
              data-testid="guard-hide-isolated-btn"
            >
              <EyeOff size={14} />
              <span>开启隐藏孤立节点并重试</span>
            </button>

            <button
              type="button"
              className="code-btn code-btn--danger code-arch-guard-modal__option-btn"
              onClick={onForceApply}
              data-testid="guard-force-apply-btn"
            >
              <AlertTriangle size={14} />
              <span>仍强制展开 ({estimatedCount} 节点)</span>
            </button>
          </div>
        </div>

        <div className="code-arch-guard-modal__footer">
          <button
            type="button"
            className="code-btn code-btn--secondary code-btn--sm"
            onClick={onCancel}
            data-testid="guard-cancel-btn"
          >
            取消操作
          </button>
        </div>
      </div>
    </div>
  );
};
