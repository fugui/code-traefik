import React, { useState } from 'react';
import type {
  TransactionSequenceDTO,
  SequenceStepDTO,
} from '../../types';

interface SequenceSwimlaneCanvasProps {
  sequence: TransactionSequenceDTO;
  selectedStepIndex?: number;
  onSelectStep: (step: SequenceStepDTO) => void;
}

export const SequenceSwimlaneCanvas: React.FC<SequenceSwimlaneCanvasProps> = ({
  sequence,
  selectedStepIndex,
  onSelectStep,
}) => {
  const [showLineage, setShowLineage] = useState(true);

  const participants = sequence?.participants || [];
  const steps = sequence?.steps || [];
  const parameter_lineages = sequence?.parameter_lineages || [];

  if (participants.length === 0 || steps.length === 0) {
    return (
      <div style={{ padding: '40px', textAlign: 'center', color: 'var(--color-text-secondary, #64748b)' }}>
        暂无可绘制的时序泳道数据
      </div>
    );
  }

  const colWidth = 190;
  const colMargin = 40;
  const headerHeight = 70;
  const stepRowHeight = 85;
  const paddingBottom = 60;

  const totalWidth = Math.max(800, colMargin * 2 + participants.length * colWidth);
  const totalHeight = headerHeight + steps.length * stepRowHeight + paddingBottom;

  const participantXMap = new Map<string, number>();
  participants.forEach((p, idx) => {
    participantXMap.set(p.participant_key, colMargin + idx * colWidth + colWidth / 2);
  });

  // 映射 recordID 到 stepRow 坐标
  const recordStepYMap = new Map<string, number>();
  steps.forEach((s, idx) => {
    const y = headerHeight + idx * stepRowHeight + stepRowHeight / 2;
    recordStepYMap.set(s.record.id, y);
    if (s.stitched_ingress_record) {
      recordStepYMap.set(s.stitched_ingress_record.id, y);
    }
  });

  return (
    <div className="code-sequence-swimlane-container" style={{ width: '100%', height: '100%', display: 'flex', flexDirection: 'column' }}>
      {/* 顶部工具栏 */}
      <div
        className="code-sequence-toolbar"
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '8px 16px',
          backgroundColor: 'var(--color-bg-secondary, #f8fafc)',
          borderBottom: '1px solid var(--color-border, #e2e8f0)',
          fontSize: '12px',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '16px' }}>
          <span style={{ fontWeight: 600 }}>全链路时序泳道图 (DOTR 权威时序)</span>
          <span style={{ color: 'var(--color-text-secondary, #64748b)' }}>
            {`总耗时: ${sequence.total_duration_us ?? 0}µs · 步骤: ${steps.length}步`}
          </span>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
          <label style={{ display: 'inline-flex', alignItems: 'center', gap: '6px', cursor: 'pointer' }}>
            <input
              type="checkbox"
              checked={showLineage}
              onChange={(e) => setShowLineage(e.target.checked)}
              data-testid="toggle-show-lineage"
            />
            <span>{`显示参数传递血缘 (${parameter_lineages.length} 处)`}</span>
          </label>
        </div>
      </div>

      {/* SVG 画布 */}
      <div
        className="code-sequence-svg-scroll-area"
        style={{
          flex: 1,
          overflow: 'auto',
          backgroundColor: 'var(--color-bg-primary, #ffffff)',
          padding: '16px',
        }}
      >
        <svg
          width={totalWidth}
          height={totalHeight}
          viewBox={`0 0 ${totalWidth} ${totalHeight}`}
          style={{ display: 'block', margin: '0 auto', fontFamily: 'system-ui, sans-serif' }}
          data-testid="sequence-svg-canvas"
        >
          <defs>
            <marker
              id="arrow-solid"
              viewBox="0 0 10 10"
              refX="8"
              refY="5"
              markerWidth="6"
              markerHeight="6"
              orient="auto-start-reverse"
            >
              <path d="M 0 1 L 8 5 L 0 9 z" fill="var(--color-primary, #3b82f6)" />
            </marker>
            <marker
              id="arrow-dashed"
              viewBox="0 0 10 10"
              refX="8"
              refY="5"
              markerWidth="6"
              markerHeight="6"
              orient="auto-start-reverse"
            >
              <path d="M 0 1 L 8 5 L 0 9 z" fill="var(--color-text-secondary, #64748b)" />
            </marker>
            <marker
              id="arrow-error"
              viewBox="0 0 10 10"
              refX="8"
              refY="5"
              markerWidth="6"
              markerHeight="6"
              orient="auto-start-reverse"
            >
              <path d="M 0 1 L 8 5 L 0 9 z" fill="var(--color-danger, #ef4444)" />
            </marker>
            <marker
              id="arrow-lineage"
              viewBox="0 0 10 10"
              refX="8"
              refY="5"
              markerWidth="6"
              markerHeight="6"
              orient="auto-start-reverse"
            >
              <path d="M 0 2 L 7 5 L 0 8 z" fill="var(--color-warning, #f59e0b)" />
            </marker>
          </defs>

          {/* 1. 参与者泳道标题框与生命线 */}
          {participants.map((p, idx) => {
            const centerX = colMargin + idx * colWidth + colWidth / 2;
            const boxWidth = colWidth - 24;
            const boxX = centerX - boxWidth / 2;

            return (
              <g key={p.participant_key} className="code-seq-participant-group">
                {/* 垂直生命虚线 */}
                <line
                  x1={centerX}
                  y1={headerHeight}
                  x2={centerX}
                  y2={totalHeight - 20}
                  stroke="var(--color-border, #cbd5e1)"
                  strokeWidth="1.5"
                  strokeDasharray="4 4"
                />

                {/* 顶部标题框 */}
                <rect
                  x={boxX}
                  y={10}
                  width={boxWidth}
                  height={headerHeight - 20}
                  rx={6}
                  fill="var(--color-bg-secondary, #f8fafc)"
                  stroke="var(--color-border, #cbd5e1)"
                  strokeWidth="1.5"
                />
                <text
                  x={centerX}
                  y={30}
                  textAnchor="middle"
                  fontSize="12"
                  fontWeight="600"
                  fill="var(--color-text-primary, #1e293b)"
                >
                  {p.title}
                </text>
                <text
                  x={centerX}
                  y={46}
                  textAnchor="middle"
                  fontSize="10"
                  fill="var(--color-text-secondary, #64748b)"
                >
                  {p.subtitle || p.kind}
                </text>
              </g>
            );
          })}

          {/* 2. 交互步连线与三段耗时色柱 */}
          {steps.map((step, idx) => {
            const isSelected = selectedStepIndex === step.step_index;
            const fromX = participantXMap.get(step.from_participant) ?? (colMargin + colWidth / 2);
            const toX = participantXMap.get(step.to_participant) ?? (colMargin + 1.5 * colWidth);
            const y = headerHeight + idx * stepRowHeight + stepRowHeight / 2;

            const isReqErr = step.record.status_code !== '0' && step.record.status_code !== 0 || step.record.biz_error;
            const isMockMiss = step.replay_overlay?.status === 'MOCK_MISS';
            const isScopedHit = step.replay_overlay?.status === 'SCOPED_HIT';
            const hasDiff = step.replay_overlay && (step.replay_overlay.diff_count ?? 0) > 0;

            let arrowColor = 'var(--color-primary, #3b82f6)';
            let markerId = 'url(#arrow-solid)';
            if (isMockMiss || isReqErr) {
              arrowColor = 'var(--color-danger, #ef4444)';
              markerId = 'url(#arrow-error)';
            }

            const midX = (fromX + toX) / 2;

            return (
              <g
                key={step.step_index}
                className={`code-seq-step-group ${isSelected ? 'selected' : ''}`}
                onClick={() => onSelectStep(step)}
                style={{ cursor: 'pointer' }}
                data-testid={`sequence-step-${step.step_index}`}
              >
                {/* 选中背景高亮 */}
                {isSelected && (
                  <rect
                    x={Math.min(fromX, toX) - 20}
                    y={y - 25}
                    width={Math.abs(toX - fromX) + 40}
                    height={50}
                    rx={6}
                    fill="var(--color-primary-bg, rgba(59, 130, 246, 0.08))"
                    stroke="var(--color-primary, #3b82f6)"
                    strokeWidth="1"
                    strokeDasharray="2 2"
                  />
                )}

                {/* 请求箭头实线 */}
                <line
                  x1={fromX}
                  y1={y}
                  x2={toX > fromX ? toX - 6 : toX + 6}
                  y2={y}
                  stroke={arrowColor}
                  strokeWidth={isSelected ? '2.5' : '1.8'}
                  markerEnd={markerId}
                />

                {/* 箭头文字标注 */}
                <text
                  x={midX}
                  y={y - 8}
                  textAnchor="middle"
                  fontSize="11"
                  fontWeight="600"
                  fill={arrowColor}
                >
                  #{step.step_index} {step.record.operation} ({step.duration_us}µs)
                </text>

                {/* 重放覆盖提示 */}
                {step.replay_overlay && (
                  <text
                    x={midX}
                    y={y + 16}
                    textAnchor="middle"
                    fontSize="10"
                    fontWeight="600"
                    fill={isMockMiss || hasDiff ? 'var(--color-danger, #ef4444)' : 'var(--color-success, #10b981)'}
                  >
                    {isMockMiss && '[⚠️ MOCK_MISS 阻断]'}
                    {isScopedHit && '[✅ SCOPED_HIT 精确匹配]'}
                    {hasDiff && `[⚠️ RSP_DIFF ${step.replay_overlay.diff_count}处差异]`}
                    {!isMockMiss && !isScopedHit && !hasDiff && `[重放: ${step.replay_overlay.status}]`}
                  </text>
                )}

                {/* 目标端生命线上的三段阶段耗时色柱 */}
                {step.stage_timing && (
                  <g className="code-seq-stage-timing-pillar">
                    {/* 执行色柱 */}
                    <rect
                      x={toX - 4}
                      y={y - 12}
                      width={8}
                      height={24}
                      rx={2}
                      fill="var(--color-primary, #3b82f6)"
                    >
                      <title>{`执行耗时: ${step.stage_timing.server_exec_us ?? 0}µs`}</title>
                    </rect>
                  </g>
                )}
              </g>
            );
          })}

          {/* 3. 参数传递血缘贝塞尔曲线 */}
          {showLineage &&
            parameter_lineages.map((lineage, idx) => {
              const y1 = recordStepYMap.get(lineage.from_record_id);
              const y2 = recordStepYMap.get(lineage.to_record_id);
              if (y1 === undefined || y2 === undefined) {
                return null;
              }

              // 绘制向左弯曲的贝塞尔弧线
              const curveControlX = Math.max(30, colMargin - 20 - (idx % 3) * 15);
              const pathD = `M ${colMargin + 10} ${y1} C ${curveControlX} ${y1}, ${curveControlX} ${y2}, ${colMargin + 10} ${y2}`;

              return (
                <g key={`lineage-${idx}`} className="code-seq-lineage-arc">
                  <path
                    d={pathD}
                    fill="none"
                    stroke="var(--color-warning, #f59e0b)"
                    strokeWidth="1.5"
                    strokeDasharray="4 3"
                    markerEnd="url(#arrow-lineage)"
                  />
                  <text
                    x={curveControlX - 4}
                    y={(y1 + y2) / 2}
                    textAnchor="end"
                    fontSize="9"
                    fill="var(--color-warning, #d97706)"
                  >
                    <title>{`${lineage.from_field_path} ➔ ${lineage.to_field_path} (${lineage.sample_value})`}</title>
                    {lineage.sample_value}
                  </text>
                </g>
              );
            })}
        </svg>
      </div>
    </div>
  );
};
