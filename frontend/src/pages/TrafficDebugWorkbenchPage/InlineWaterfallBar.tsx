import React from 'react';

interface InlineWaterfallBarProps {
  offsetPct: number;
  widthPct: number;
  durationUs?: number;
  relation?: string;
  hasError?: boolean;
}

export const InlineWaterfallBar: React.FC<InlineWaterfallBarProps> = ({
  offsetPct,
  widthPct,
  durationUs,
  relation,
  hasError,
}) => {
  const clampedOffset = Math.max(0, Math.min(100, offsetPct));
  const clampedWidth = Math.max(0.5, Math.min(100 - clampedOffset, widthPct));

  let barBg = 'var(--color-primary, #3b82f6)';
  if (hasError) {
    barBg = 'var(--color-danger, #ef4444)';
  } else if (relation === 'COMPENSATION') {
    barBg = 'var(--color-warning, #f59e0b)';
  } else if (relation === 'PARALLEL') {
    barBg = 'var(--color-info, #06b6d4)';
  }

  const formatDuration = (us?: number) => {
    if (us === undefined || us === null) return '';
    if (us < 1000) return `${us}µs`;
    if (us < 1000000) return `${(us / 1000).toFixed(1)}ms`;
    return `${(us / 1000000).toFixed(2)}s`;
  };

  return (
    <div
      className="code-waterfall-track"
      title={`耗时: ${formatDuration(durationUs)} (偏移: ${clampedOffset.toFixed(1)}%, 宽度: ${clampedWidth.toFixed(1)}%)`}
      style={{
        position: 'relative',
        width: '100%',
        minWidth: '100px',
        maxWidth: '140px',
        height: '14px',
        backgroundColor: 'var(--color-bg-secondary, #f1f5f9)',
        borderRadius: '3px',
        overflow: 'hidden',
        display: 'inline-block',
        verticalAlign: 'middle',
      }}
    >
      <div
        className="code-waterfall-bar"
        style={{
          position: 'absolute',
          left: `${clampedOffset}%`,
          width: `${clampedWidth}%`,
          top: 0,
          bottom: 0,
          backgroundColor: barBg,
          borderRadius: '2px',
          transition: 'all 0.2s ease',
        }}
      />
    </div>
  );
};
