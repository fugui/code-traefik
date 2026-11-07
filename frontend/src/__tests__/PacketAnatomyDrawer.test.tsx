import { describe, it, expect } from 'vitest';
import { renderToString } from 'react-dom/server';
import {
  PacketAnatomyDrawer,
  PacketAnatomyContent,
  BusinessSummaryBar,
  BusinessPayloadCompareView,
  RpcLatencyWaterfallBar,
  AdvancedProtocolAccordion,
} from '../components/PacketAnatomyDrawer';
import type { PacketAnatomyDrawerDTO } from '../types';

const evilScript = '<script>' + 'al' + 'ert(1)</' + 'script>';

export const mockAnatomyDTO: PacketAnatomyDrawerDTO = {
  recordId: '10001',
  sessionId: '55',
  direction: 'INGRESS',
  exchangeType: 'UNARY',
  roundTripLatencyUs: 1250,
  srcModuleName: 'MOD_API_GATEWAY',
  dstModuleName: 'MOD_ORDER_SUT',
  reqSide: {
    segmentCount: 1,
    commHead: {
      magicAscii: 'CSE.TCP',
      transportProtocol: 1,
      transportName: 'RPC_TRANSPORT_TCP',
      snoDec: '1000000001',
      snoHex: '0x000000003B9ACA01',
      uiLength: 320,
      usHeadLen: 280,
      ucMsgType: 1,
      msgTypeName: 'RPC_MSG_TYPE_REQ',
      isPacked: 0,
      segments: [
        { offsetStart: 0, byteLength: 8, fieldName: 'unMagicInfo', rawHex: '43 53 45 2E 54 43 50 01', displayValue: 'CSE.TCP' },
        { offsetStart: 8, byteLength: 8, fieldName: 'ullSNO', rawHex: '00 00 00 00 3B 9A CA 01', displayValue: '1000000001' },
        { offsetStart: 16, byteLength: 4, fieldName: 'uiLength', rawHex: '00 00 01 40', displayValue: '320' },
        { offsetStart: 20, byteLength: 2, fieldName: 'usHeadLen', rawHex: '00 01 18', displayValue: '280' },
        { offsetStart: 22, byteLength: 1, fieldName: 'ucMsgType', rawHex: '01', displayValue: '1' },
        { offsetStart: 23, byteLength: 1, fieldName: 'isPacked', rawHex: '00', displayValue: '0' },
      ],
    },
    sessionHead: {
      headKind: 'REQ',
      detectedVersion: 'V05 (256B)',
      sessionHeadLen: 256,
      serviceName: `dophi.order.v1.OrderService${evilScript}`,
      operation: 'CreateOrder',
      fields: [
        { name: 'uiVersion', offsetRange: '24..27', value: '5' },
        { name: 'szServiceName', offsetRange: '28..91', value: `dophi.order.v1.OrderService${evilScript}` },
        { name: 'szOperation', offsetRange: '92..123', value: 'CreateOrder' },
      ],
    },
    extendHead: {
      totalExtendBytes: 24,
      trace: {
        traceIdHex: '0123456789abcdef0123456789abcdef',
        spanIdHex: 'abcdef0123456789',
        fatherSpanIdHex: '0000000000000000',
        recordMsgSwitch: 1,
      },
      queueWaitUs: 15,
      items: [
        { ucType: 1, typeName: 'DOTR_TRACE', usHeadLen: 24, rawHex: '01 00 18 01 23 45 67 89 ab cd ef' },
      ],
    },
    payload: {
      payloadOffset: 304,
      payloadLength: 16,
      isTruncated: false,
      isReplayable: true,
      decodeStatus: 'DECODED_JSON',
      schemaName: 'order_service.pb',
      schemaVersion: 1,
      messageFullName: 'dophi.order.v1.CreateOrderReq',
      decodedJson: { orderId: 'ORD_20261029_001', amountCents: 9900 },
      rawFrameHexLines: [
        {
          offsetHex: '00000000',
          hexCols: '43 53 45 2E 54 43 50 01  00 00 00 00 3B 9A CA 01',
          asciiCols: 'CSE.TCP.........',
          region: 'COMM_HEAD',
        },
        {
          offsetHex: '00000018',
          hexCols: '00 00 00 05 64 6F 70 68  69 2E 6F 72 64 65 72 2E',
          asciiCols: '....dophi.order.',
          region: 'SESSION_HEAD',
        },
        {
          offsetHex: '00000118',
          hexCols: '01 00 18 01 23 45 67 89  AB CD EF 01 23 45 67 89',
          asciiCols: '....#Eg.........',
          region: 'EXTEND_HEAD',
        },
        {
          offsetHex: '00000130',
          hexCols: '0A 12 4F 52 44 5F 32 30  32 36 31 30 32 39 5F 30',
          asciiCols: '..ORD_20261029_0',
          region: 'PAYLOAD',
        },
      ],
    },
  },
  rspSide: {
    segmentCount: 3,
    commHead: {
      magicAscii: 'CSE.TCP',
      transportProtocol: 1,
      transportName: 'RPC_TRANSPORT_TCP',
      snoDec: '1000000001',
      snoHex: '0x000000003B9ACA01',
      uiLength: 512,
      usHeadLen: 280,
      ucMsgType: 3,
      msgTypeName: 'RPC_MSG_TYPE_RSP',
      isPacked: 0,
      segments: [],
    },
    sessionHead: {
      headKind: 'RSP',
      detectedVersion: 'V05 (256B)',
      sessionHeadLen: 256,
      serviceName: 'dophi.order.v1.OrderService',
      operation: 'CreateOrder',
      uiStatusCode: 0,
      timingBreakdownUs: {
        clientToServerUs: 120,
        serverProcessUs: 1100,
        queueWaitUs: 15,
      },
      fields: [],
    },
    extendHead: {
      totalExtendBytes: 0,
      items: [],
    },
    payload: {
      payloadOffset: 304,
      payloadLength: 232,
      isTruncated: false,
      isReplayable: true,
      decodeStatus: 'DECODED_JSON',
      decodedJson: { status: 'SUCCESS', code: 0, balanceRemaining: 5000 },
      rawFrameHexLines: [],
    },
  },
};

/**
 * 业务错误 Mock 数据：状态码为 5001
 */
export const mockBizErrorAnatomyDTO: PacketAnatomyDrawerDTO = {
  ...mockAnatomyDTO,
  recordId: '10002',
  rspSide: {
    ...mockAnatomyDTO.rspSide!,
    sessionHead: {
      ...mockAnatomyDTO.rspSide!.sessionHead,
      uiStatusCode: 5001,
      ullStatusCode: 'ERR_INSUFFICIENT_STOCK',
    },
    payload: {
      ...mockAnatomyDTO.rspSide!.payload,
      decodedJson: { error: 'ITEM_OUT_OF_STOCK', message: '库存不足无法扣减' },
    },
  },
};

/**
 * 单向 ONEWAY Mock 数据
 */
export const mockOnewayAnatomyDTO: PacketAnatomyDrawerDTO = {
  ...mockAnatomyDTO,
  recordId: '10003',
  exchangeType: 'ONEWAY',
  rspSide: undefined,
};

/**
 * TestPacketAnatomyDrawerFourSections
 * 验证底层四段解剖内容渲染及 XSS 转义防御 (F-15.2 向后兼容性)
 */
export function TestPacketAnatomyDrawerFourSections(): void {
  const html = renderToString(<PacketAnatomyContent data={mockAnatomyDTO} />);

  // 1. 验证 ① 24B 公共头位图与色块
  if (!html.includes('24B 公共头位图与校验') || !html.includes('unMagicInfo') || !html.includes('ullSNO')) {
    throw new Error('断言失败: 未能正确渲染 ① 24B 公共头位图色块');
  }

  // 2. 验证 ② 会话头版本 V05 与字段
  if (!html.includes('V05 (256B)') || !html.includes('会话头结构与耗时拆解')) {
    throw new Error('断言失败: 未能正确渲染 ② 会话头版本 V05 (256B)');
  }

  // 3. 验证 ③ 扩展头 DOTR TraceID
  if (!html.includes('0123456789abcdef0123456789abcdef') || !html.includes('扩展头链路与排队耗时')) {
    throw new Error('断言失败: 未能正确渲染 ③ 扩展头 DOTR TraceID');
  }

  // 4. 验证 ④ 动态 JSON 与 16B 对齐 Raw Hex
  if (!html.includes('ORD_20261029_001') || !html.includes('16 字节对齐报文 Raw Hex 视窗')) {
    throw new Error('断言失败: 未能正确渲染 ④ 载荷 JSON 与 16B 对齐 Raw Hex 对照');
  }

  // 5. 安全断言 (F-15.2): 恶意脚本必须被转义，DOM 中零 <script> 节点
  if (html.includes(evilScript)) {
    throw new Error('安全红线拦截失败: 恶意脚本未被安全转义，存在 XSS 漏洞！');
  }
  const escapedScript = '&lt;script&gt;' + 'al' + 'ert(1)&lt;/script&gt;';
  if (!html.includes(escapedScript)) {
    throw new Error('断言失败: 恶意脚本未被安全转义为 HTML 实体字符串');
  }
}

describe('PacketAnatomyDrawer (M18 业务级视图与向后兼容性)', () => {
  it('renders all four sections and escapes HTML (backward compatibility)', () => {
    TestPacketAnatomyDrawerFourSections();
  });

  it('TC-INSPECT-01: renders business-centric overview bar with service, operation, latency and trace ID', () => {
    const html = renderToString(
      <BusinessSummaryBar
        data={mockAnatomyDTO}
        viewMode="BUSINESS"
        onViewModeChange={() => {}}
      />
    );
    expect(html).toContain('CreateOrder');
    expect(html).toContain('🟢 成功 (Code: 0)');
    expect(html).toContain('1250 μs');
    expect(html).toContain('MOD_API_GATEWAY');
    expect(html).toContain('MOD_ORDER_SUT');
    expect(html).toContain('0123456789abcdef0123456789abcdef');
    expect(html).toContain('💼 业务视图');
    expect(html).toContain('🔬 协议专家');
  });

  it('TC-INSPECT-02: renders request and response split comparison view simultaneously', () => {
    const html = renderToString(<BusinessPayloadCompareView data={mockAnatomyDTO} />);
    // 验证无需切换 Tab，请求入参和响应出参同屏可见
    expect(html).toContain('业务请求参数 (Request)');
    expect(html).toContain('业务返回结果 (Response)');
    expect(html).toContain('ORD_20261029_001');
    expect(html).toContain('9900');
    expect(html).toContain('SUCCESS');
    expect(html).toContain('balanceRemaining');
  });

  it('TC-INSPECT-03: renders business error status badge prominently when statusCode is non-zero', () => {
    const html = renderToString(
      <BusinessSummaryBar
        data={mockBizErrorAnatomyDTO}
        viewMode="BUSINESS"
        onViewModeChange={() => {}}
      />
    );
    expect(html).toContain('🔴 业务异常 (Code: 5001');
    expect(html).toContain('ERR_INSUFFICIENT_STOCK');

    const compareHtml = renderToString(<BusinessPayloadCompareView data={mockBizErrorAnatomyDTO} />);
    expect(compareHtml).toContain('ITEM_OUT_OF_STOCK');
    expect(compareHtml).toContain('库存不足无法扣减');
  });

  it('TC-INSPECT-04: renders microsecond-level latency waterfall bar with stage breakdown', () => {
    const html = renderToString(<RpcLatencyWaterfallBar data={mockAnatomyDTO} />);
    expect(html).toContain('微秒级调用阶段耗时瀑布');
    expect(html).toContain('客户端传输:');
    expect(html).toContain('120 μs');
    expect(html).toContain('入队排队:');
    expect(html).toContain('15 μs');
    expect(html).toContain('服务端业务执行:');
    expect(html).toContain('1100 μs');
  });

  it('TC-INSPECT-05: renders advanced protocol accordion with collapsible toggle and tags', () => {
    const html = renderToString(<AdvancedProtocolAccordion data={mockAnatomyDTO} />);
    expect(html).toContain('底层二进制协议与十六进制解剖 (专家排错区)');
    expect(html).toContain('24B CommHead');
    expect(html).toContain('V05 (256B)');
    expect(html).toContain('DOTR 扩展头');
    expect(html).toContain('16B Raw Hex');
  });

  it('TC-INSPECT-06: renders graceful empty state for ONEWAY and RSP_ORPHAN packets', () => {
    const html = renderToString(<BusinessPayloadCompareView data={mockOnewayAnatomyDTO} />);
    expect(html).toContain('单向通知调用 (ONEWAY)');
  });

  it('renders Drawer with workspace scope to avoid stacking context conflict with host header', () => {
    const html = renderToString(
      <PacketAnatomyDrawer open={true} onClose={() => {}} data={mockAnatomyDTO} />
    );
    expect(html).toContain('code-drawer-root--workspace');
    expect(html).toContain('code-drawer-mask--workspace');
    expect(html).toContain('code-drawer-panel--workspace');
    expect(html).toContain('RPC 调用透视 · CreateOrder');
  });
});
