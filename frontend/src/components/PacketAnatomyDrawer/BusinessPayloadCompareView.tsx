import React, { useMemo, useState } from 'react';
import type { BusinessPayloadCompareProps } from './types';
import type { FrameSideAnatomyDTO } from '../../types';

export const BusinessPayloadCompareView: React.FC<BusinessPayloadCompareProps> = ({
  data,
}) => {
  const [copiedSide, setCopiedSide] = useState<'REQ' | 'RSP' | null>(null);

  const formatJson = (decoded: unknown): string => {
    if (!decoded) return '';
    if (typeof decoded === 'string') return decoded;
    try {
      return JSON.stringify(decoded, null, 2);
    } catch {
      return String(decoded);
    }
  };

  const reqJsonStr = useMemo(() => formatJson(data.reqSide?.payload.decodedJson), [data.reqSide?.payload.decodedJson]);
  const rspJsonStr = useMemo(() => formatJson(data.rspSide?.payload.decodedJson), [data.rspSide?.payload.decodedJson]);

  const handleCopy = (text: string, side: 'REQ' | 'RSP') => {
    try {
      if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
        void navigator.clipboard.writeText(text);
      }
    } catch {
      // 忽略 clipboard 异常
    }
    setCopiedSide(side);
    setTimeout(() => {
      setCopiedSide(null);
    }, 2000);
  };

  const renderPayloadContent = (sideData?: FrameSideAnatomyDTO, jsonStr?: string, side: 'REQ' | 'RSP' = 'REQ') => {
    if (!sideData) {
      if (side === 'REQ') {
        return (
          <div className="code-payload-empty-box">
            <span className="code-payload-empty-icon">⚪</span>
            <div className="code-payload-empty-text">本记录为无头孤立应答 (RSP_ORPHAN)，未采集到请求入参</div>
          </div>
        );
      }
      if (data.exchangeType === 'ONEWAY') {
        return (
          <div className="code-payload-empty-box">
            <span className="code-payload-empty-icon">🔵</span>
            <div className="code-payload-empty-text">单向通知调用 (ONEWAY)，服务端无需回传应答数据</div>
          </div>
        );
      }
      return (
        <div className="code-payload-empty-box">
          <span className="code-payload-empty-icon">⏳</span>
          <div className="code-payload-empty-text">尚未捕获到对应应答 (REQ_ONLY)，可能调用超时或连接中断</div>
        </div>
      );
    }

    const { payload } = sideData;

    if (jsonStr) {
      return (
        <pre className="code-json-viewer-content code-font-mono" data-testid={`json-content-${side.toLowerCase()}`}>
          {jsonStr}
        </pre>
      );
    }

    if (payload.decodeStatus === 'NO_SCHEMA_RAW_HEX') {
      return (
        <div className="code-payload-empty-box code-payload-empty-box--warning">
          <span className="code-payload-empty-icon">🧩</span>
          <div className="code-payload-empty-title">未匹配到 Proto 契约描述集</div>
          <div className="code-payload-empty-desc">
            当前载荷保留原始二进制（{payload.payloadLength} Bytes）。可展开下方「底层协议与原始字节流」查看 16 字节对齐 Hex，或上传对应 .pb 激活后点击重新解码。
          </div>
        </div>
      );
    }

    if (payload.decodeStatus === 'DECODE_ERR_RAW_HEX') {
      return (
        <div className="code-payload-empty-box code-payload-empty-box--danger">
          <span className="code-payload-empty-icon">⚠️</span>
          <div className="code-payload-empty-title">Protobuf 动态反序列化失败</div>
          <div className="code-payload-empty-desc">
            数据包可能存在字段损坏或版本不匹配，可展开下方底层专家模式查看原始 Hex。
          </div>
        </div>
      );
    }

    return (
      <div className="code-payload-empty-box">
        <span className="code-payload-empty-icon">📄</span>
        <div className="code-payload-empty-text">无业务载荷数据 (0 Bytes)</div>
      </div>
    );
  };

  const reqMessageName =
    data.reqSide?.payload.messageFullName ||
    data.reqSide?.payload.schemaName ||
    'Request Payload';

  const rspMessageName =
    data.rspSide?.payload.messageFullName ||
    data.rspSide?.payload.schemaName ||
    'Response Payload';

  const statusCode = data.rspSide?.sessionHead.uiStatusCode;
  const isBizError = statusCode !== undefined && statusCode !== 0;

  return (
    <div className="code-biz-compare-container" data-testid="business-payload-compare">
      {/* 左栏：业务请求入参 (Request) */}
      <div className="code-biz-compare-pane code-biz-compare-pane--req">
        <div className="code-biz-pane-header">
          <div className="code-biz-pane-title-group">
            <span className="code-biz-pane-icon">📤</span>
            <span className="code-biz-pane-title">业务请求参数 (Request)</span>
            {data.reqSide?.payload.payloadLength !== undefined && (
              <span className="code-badge code-badge--muted code-text-xs">
                {data.reqSide.payload.payloadLength}B
              </span>
            )}
          </div>

          <div className="code-biz-pane-actions">
            <span className="code-biz-schema-tag code-font-mono" title={reqMessageName}>
              {reqMessageName.split('.').pop()}
            </span>
            {reqJsonStr && (
              <button
                type="button"
                className="code-btn code-btn--secondary code-btn--xs"
                onClick={() => handleCopy(reqJsonStr, 'REQ')}
                title="复制请求 JSON 字符串"
              >
                {copiedSide === 'REQ' ? '已复制' : '复制 JSON'}
              </button>
            )}
          </div>
        </div>

        <div className="code-biz-pane-body">
          {renderPayloadContent(data.reqSide, reqJsonStr, 'REQ')}
        </div>
      </div>

      {/* 右栏：业务返回结果 (Response) */}
      <div className={`code-biz-compare-pane code-biz-compare-pane--rsp ${isBizError ? 'code-biz-compare-pane--error' : ''}`}>
        <div className="code-biz-pane-header">
          <div className="code-biz-pane-title-group">
            <span className="code-biz-pane-icon">📥</span>
            <span className="code-biz-pane-title">业务返回结果 (Response)</span>
            {statusCode !== undefined && (
              <span className={`code-badge ${statusCode === 0 ? 'code-badge--success' : 'code-badge--danger'} code-text-xs`}>
                Code: {statusCode}
              </span>
            )}
            {data.rspSide?.payload.payloadLength !== undefined && (
              <span className="code-badge code-badge--muted code-text-xs">
                {data.rspSide.payload.payloadLength}B
              </span>
            )}
          </div>

          <div className="code-biz-pane-actions">
            <span className="code-biz-schema-tag code-font-mono" title={rspMessageName}>
              {rspMessageName.split('.').pop()}
            </span>
            {rspJsonStr && (
              <button
                type="button"
                className="code-btn code-btn--secondary code-btn--xs"
                onClick={() => handleCopy(rspJsonStr, 'RSP')}
                title="复制响应 JSON 字符串"
              >
                {copiedSide === 'RSP' ? '已复制' : '复制 JSON'}
              </button>
            )}
          </div>
        </div>

        <div className="code-biz-pane-body">
          {renderPayloadContent(data.rspSide, rspJsonStr, 'RSP')}
        </div>
      </div>
    </div>
  );
};
