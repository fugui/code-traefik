import React, { useState, useEffect } from 'react';
import { Drawer } from '@code/common';
import type {
  TransactionSequenceDTO,
  SequenceStepDTO,
  PacketAnatomyDrawerDTO,
} from '../../types';
import { getTransactionSequence, getTrafficRecordAnatomy, redecodeTrafficRecord } from '../../api/traffic';
import { SequenceSwimlaneCanvas } from './SequenceSwimlaneCanvas';
import { PacketAnatomyContent } from '../../components/PacketAnatomyDrawer/PacketAnatomyDrawer';

export interface TransactionSequenceDrawerProps {
  open: boolean;
  onClose: () => void;
  sessionId: string;
  rootRecordId?: string;
  replayTaskId?: string;
}

export const TransactionSequenceDrawer: React.FC<TransactionSequenceDrawerProps> = ({
  open,
  onClose,
  sessionId,
  rootRecordId,
  replayTaskId,
}) => {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [sequence, setSequence] = useState<TransactionSequenceDTO | null>(null);
  const [selectedStep, setSelectedStep] = useState<SequenceStepDTO | null>(null);

  // 跨跳缝合视角切换 (EGRESS 客户端视角 vs INGRESS 服务端视角)
  const [stitchPerspective, setStitchPerspective] = useState<'EGRESS' | 'INGRESS'>('EGRESS');

  // 右侧报文解剖状态
  const [anatomyLoading, setAnatomyLoading] = useState(false);
  const [anatomyData, setAnatomyData] = useState<PacketAnatomyDrawerDTO | null>(null);

  useEffect(() => {
    if (!open || !sessionId || !rootRecordId) {
      setSequence(null);
      setSelectedStep(null);
      setAnatomyData(null);
      return;
    }

    let isMounted = true;
    setLoading(true);
    setError(null);

    getTransactionSequence(sessionId, rootRecordId, replayTaskId)
      .then((data) => {
        if (!isMounted) return;
        setSequence(data);
        if (data.steps && data.steps.length > 0) {
          setSelectedStep(data.steps[0]);
          setStitchPerspective('EGRESS');
        }
      })
      .catch((err) => {
        if (!isMounted) return;
        setError(err.message || '加载时序泳道失败');
      })
      .finally(() => {
        if (isMounted) setLoading(false);
      });

    return () => {
      isMounted = false;
    };
  }, [open, sessionId, rootRecordId, replayTaskId]);

  // 当选中的 step 或视角改变时，加载四段解剖
  useEffect(() => {
    if (!selectedStep) {
      setAnatomyData(null);
      return;
    }

    let targetRecordId = selectedStep.record.id;
    if (stitchPerspective === 'INGRESS' && selectedStep.stitched_ingress_record) {
      targetRecordId = selectedStep.stitched_ingress_record.id;
    }

    let isMounted = true;
    setAnatomyLoading(true);

    getTrafficRecordAnatomy(targetRecordId, sessionId)
      .then((dto) => {
        if (!isMounted) return;
        setAnatomyData(dto);
      })
      .catch(() => {
        if (!isMounted) return;
        setAnatomyData(null);
      })
      .finally(() => {
        if (isMounted) setAnatomyLoading(false);
      });

    return () => {
      isMounted = false;
    };
  }, [selectedStep, stitchPerspective, sessionId]);

  const handleSelectStep = (step: SequenceStepDTO) => {
    setSelectedStep(step);
    setStitchPerspective('EGRESS');
  };

  const handleRedecode = async (recordId: string) => {
    const updated = await redecodeTrafficRecord(recordId);
    setAnatomyData(updated);
  };

  return (
    <Drawer
      open={open}
      onClose={onClose}
      title={`全链路时序泳道与报文因果透视 (根事务 #${rootRecordId || ''})`}
      width={1280}
      extra={
        replayTaskId && (
          <span
            style={{
              fontSize: '12px',
              padding: '2px 8px',
              borderRadius: '4px',
              backgroundColor: 'var(--color-warning-bg, #fffbeb)',
              color: 'var(--color-warning, #d97706)',
              border: '1px solid var(--color-warning-border, #fef3c7)',
            }}
          >
            叠加重放任务 #{replayTaskId}
          </span>
        )
      }
    >
      {loading && (
        <div style={{ padding: '60px', textAlign: 'center', color: 'var(--color-text-secondary, #64748b)' }}>
          正在加载全链路时序图与参数血缘...
        </div>
      )}

      {error && (
        <div style={{ padding: '40px', textAlign: 'center', color: 'var(--color-danger, #ef4444)' }}>
          加载失败: {error}
        </div>
      )}

      {!loading && !error && sequence && (
        <div
          className="code-sequence-drawer-body"
          style={{
            display: 'flex',
            height: '100%',
            overflow: 'hidden',
          }}
        >
          {/* 左栏：SVG 时序泳道画布 (占 60%) */}
          <div
            className="code-sequence-drawer-left"
            style={{
              flex: '0 0 60%',
              height: '100%',
              borderRight: '1px solid var(--color-border, #e2e8f0)',
              overflow: 'hidden',
              display: 'flex',
              flexDirection: 'column',
            }}
          >
            <SequenceSwimlaneCanvas
              sequence={sequence}
              selectedStepIndex={selectedStep?.step_index}
              onSelectStep={handleSelectStep}
            />
          </div>

          {/* 右栏：选定报文的四段解剖 (占 40%) */}
          <div
            className="code-sequence-drawer-right"
            style={{
              flex: '0 0 40%',
              height: '100%',
              overflowY: 'auto',
              padding: '16px',
              backgroundColor: 'var(--color-bg-secondary, #f8fafc)',
            }}
          >
            {selectedStep ? (
              <div>
                {/* 跨跳缝合时的视角切换器 */}
                {selectedStep.stitched_ingress_record && (
                  <div
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: '8px',
                      padding: '8px 12px',
                      marginBottom: '12px',
                      backgroundColor: 'var(--color-primary-bg, #eff6ff)',
                      border: '1px solid var(--color-primary-border, #bfdbfe)',
                      borderRadius: '6px',
                      fontSize: '12px',
                    }}
                  >
                    <span style={{ fontWeight: 600, color: 'var(--color-primary, #3b82f6)' }}>
                      🔗 跨进程缝合视角:
                    </span>
                    <button
                      type="button"
                      onClick={() => setStitchPerspective('EGRESS')}
                      style={{
                        padding: '2px 8px',
                        borderRadius: '4px',
                        border: stitchPerspective === 'EGRESS' ? '1px solid var(--color-primary)' : '1px solid #cbd5e1',
                        backgroundColor: stitchPerspective === 'EGRESS' ? '#ffffff' : 'transparent',
                        fontWeight: stitchPerspective === 'EGRESS' ? 600 : 400,
                        cursor: 'pointer',
                      }}
                    >
                      客户端 (EGRESS)
                    </button>
                    <button
                      type="button"
                      onClick={() => setStitchPerspective('INGRESS')}
                      style={{
                        padding: '2px 8px',
                        borderRadius: '4px',
                        border: stitchPerspective === 'INGRESS' ? '1px solid var(--color-primary)' : '1px solid #cbd5e1',
                        backgroundColor: stitchPerspective === 'INGRESS' ? '#ffffff' : 'transparent',
                        fontWeight: stitchPerspective === 'INGRESS' ? 600 : 400,
                        cursor: 'pointer',
                      }}
                    >
                      服务端 (INGRESS)
                    </button>
                    <span style={{ marginLeft: 'auto', color: 'var(--color-text-secondary, #64748b)' }}>
                      网络跳耗时 ~{selectedStep.duration_us}µs
                    </span>
                  </div>
                )}

                {anatomyLoading ? (
                  <div style={{ padding: '40px', textAlign: 'center', color: 'var(--color-text-secondary, #64748b)' }}>
                    正在解析当前交互步骤的报文四段解剖...
                  </div>
                ) : anatomyData ? (
                  <PacketAnatomyContent
                    data={anatomyData}
                    onRedecode={handleRedecode}
                  />
                ) : (
                  <div style={{ padding: '40px', textAlign: 'center', color: 'var(--color-text-secondary, #64748b)' }}>
                    暂无该步骤的报文解剖数据
                  </div>
                )}
              </div>
            ) : (
              <div style={{ padding: '60px 20px', textAlign: 'center', color: 'var(--color-text-secondary, #64748b)' }}>
                👉 请在左侧时序泳道图中点击任意交互步骤，右侧将联动呈现该步骤的四段二进制与 JSON 报文解剖。
              </div>
            )}
          </div>
        </div>
      )}
    </Drawer>
  );
};
