import React from 'react';

export interface DiffIgnoreFieldToggleProps {
  fieldPath: string;
  isIgnored: boolean;
  onToggle?: (fieldPath: string, willIgnore: boolean) => void;
  disabled?: boolean;
}

export const DiffIgnoreFieldToggle: React.FC<DiffIgnoreFieldToggleProps> = ({
  fieldPath,
  isIgnored,
  onToggle,
  disabled = false,
}) => {
  const getTitle = () => {
    if (disabled) {
      return isIgnored
        ? `已配置或继承忽略规则: ${fieldPath} (只读)`
        : `无权修改忽略规则（需模块管理员权限）: ${fieldPath}`;
    }
    return isIgnored
      ? `已忽略对比规则: ${fieldPath} (点击取消)`
      : `点击忽略该字段对比: ${fieldPath}`;
  };

  return (
    <label
      className={`code-diff-ignore-toggle ${isIgnored ? 'code-diff-ignore-toggle--active' : ''} ${disabled ? 'code-diff-ignore-toggle--disabled' : ''}`}
      title={getTitle()}
      onClick={(e) => e.stopPropagation()}
    >
      <input
        type="checkbox"
        checked={isIgnored}
        disabled={disabled}
        onChange={(e) => onToggle && onToggle(fieldPath, e.target.checked)}
        className="code-checkbox-input"
        data-testid={`diff-toggle-${fieldPath}`}
      />
      <span className="code-diff-ignore-label">
        {isIgnored ? '忽略对比' : '对比校验'}
      </span>
    </label>
  );
};

