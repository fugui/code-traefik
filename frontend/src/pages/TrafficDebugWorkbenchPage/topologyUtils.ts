import { TrafficRecordSummary } from '../../types/traffic';

export interface TopologyEndpoints {
  srcName: string;
  dstName: string;
  tooltip: string;
}

/**
 * 提取 IP 地址并剔除瞬态端口号
 */
export function extractPureIP(addr?: string): string {
  if (!addr) return '';
  const trimmed = addr.trim();
  // IPv6 bracket format: [::1]:8080
  if (trimmed.startsWith('[')) {
    const endBracket = trimmed.indexOf(']');
    if (endBracket !== -1) {
      return trimmed.substring(1, endBracket);
    }
  }
  // IPv4 format: 127.0.0.1:8080
  const colonIndex = trimmed.lastIndexOf(':');
  if (colonIndex > 0 && !trimmed.includes('::')) {
    return trimmed.substring(0, colonIndex);
  }
  return trimmed;
}

/**
 * 根据流量报文方向与可用元数据，执行自适应双向降级
 * 杜绝 `- ➔ -`，并保障出站 EGRESS 调用时主调方与被调方方向不颠倒
 */
export function resolveTopologyEndpoints(record: Partial<TrafficRecordSummary>): TopologyEndpoints {
  const isIngress = record.direction === 'INGRESS';
  const pureSrcIP = extractPureIP(record.src_addr);
  const pureDstIP = extractPureIP(record.dst_addr);

  let srcName = record.src_module_name?.trim() || '';
  let dstName = record.dst_module_name?.trim() || '';

  if (isIngress) {
    if (!srcName) {
      if (record.peer_proc_name?.trim()) {
        srcName = record.peer_proc_name.trim();
      } else if (pureSrcIP) {
        srcName = `外部客户端 (${pureSrcIP})`;
      } else {
        srcName = '外部客户端';
      }
    }

    if (!dstName) {
      dstName =
        record.process_name?.trim() ||
        record.arch_process_name?.trim() ||
        pureDstIP ||
        '本地服务';
    }
  } else {
    // EGRESS 出站报文
    if (!srcName) {
      srcName =
        record.process_name?.trim() ||
        record.arch_process_name?.trim() ||
        pureSrcIP ||
        '本地服务';
    }

    if (!dstName) {
      if (record.peer_proc_name?.trim()) {
        dstName = record.peer_proc_name.trim();
      } else if (record.service_name?.trim()) {
        dstName = record.service_name.trim();
      } else if (pureDstIP) {
        dstName = pureDstIP;
      } else {
        dstName = '下游服务';
      }
    }
  }

  const tooltip = `源端: ${record.src_addr || srcName} ➔ 目标端: ${record.dst_addr || dstName}`;

  return { srcName, dstName, tooltip };
}
