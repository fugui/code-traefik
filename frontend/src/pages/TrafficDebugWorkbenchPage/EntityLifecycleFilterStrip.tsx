import React from 'react';
import type { EntityLifecycleChainDTO } from '../../types';

interface EntityLifecycleFilterStripProps {
  chains: EntityLifecycleChainDTO[];
  selectedEntityKey?: string;
  onSelectEntityKey: (key: string | undefined) => void;
}

export const EntityLifecycleFilterStrip: React.FC<EntityLifecycleFilterStripProps> = ({
  chains,
  selectedEntityKey,
  onSelectEntityKey,
}) => {
  if (!chains || chains.length === 0) {
    return null;
  }

  return (
    <div
      className="code-debug-entity-strip"
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: '8px',
        flexWrap: 'wrap',
        padding: '6px 12px',
        backgroundColor: 'var(--color-bg-secondary, #f8fafc)',
        borderRadius: '6px',
        marginBottom: '12px',
        fontSize: '12px',
        border: '1px solid var(--color-border, #e2e8f0)',
      }}
    >
      <span style={{ fontWeight: 600, color: 'var(--color-text-secondary, #64748b)' }}>
        🏷️ 识别到有状态业务实体:
      </span>
      {selectedEntityKey && (
        <button
          type="button"
          onClick={() => onSelectEntityKey(undefined)}
          className="code-debug-entity-clear-btn"
          style={{
            padding: '2px 8px',
            fontSize: '11px',
            borderRadius: '12px',
            border: '1px dashed var(--color-border, #cbd5e1)',
            backgroundColor: 'transparent',
            color: 'var(--color-text-secondary, #64748b)',
            cursor: 'pointer',
          }}
        >
          ✕ 清除筛选
        </button>
      )}
      {chains.map((chain) => {
        const isSelected = selectedEntityKey === chain.entity_key;
        const stepCount = chain.steps ? chain.steps.length : 0;

        return (
          <button
            key={chain.entity_key}
            type="button"
            onClick={() => onSelectEntityKey(isSelected ? undefined : chain.entity_key)}
            className={`code-debug-entity-capsule ${isSelected ? 'active' : ''}`}
            title={`点击仅查看该实体全生命周期 (${chain.entity_key})`}
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: '4px',
              padding: '3px 10px',
              borderRadius: '14px',
              border: isSelected
                ? '1px solid var(--color-primary, #3b82f6)'
                : '1px solid var(--color-border, #cbd5e1)',
              backgroundColor: isSelected
                ? 'var(--color-primary-bg, #eff6ff)'
                : 'var(--color-bg-primary, #ffffff)',
              color: isSelected
                ? 'var(--color-primary, #3b82f6)'
                : 'var(--color-text-primary, #1e293b)',
              cursor: 'pointer',
              fontWeight: isSelected ? 600 : 400,
              transition: 'all 0.15s ease',
            }}
          >
            <span>{`${chain.field_name}=${chain.entity_value}`}</span>
            <span
              style={{
                fontSize: '10px',
                padding: '1px 5px',
                borderRadius: '8px',
                backgroundColor: isSelected
                  ? 'var(--color-primary, #3b82f6)'
                  : 'var(--color-bg-secondary, #e2e8f0)',
                color: isSelected ? '#ffffff' : 'var(--color-text-secondary, #64748b)',
              }}
            >
              {`${stepCount} 笔`}
            </span>
          </button>
        );
      })}
    </div>
  );
};
