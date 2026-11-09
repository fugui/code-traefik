import type { GraphNode, GraphEdge, TopologyGraphDTO } from '../../types/topology_graph';
import type { ConsistencyReportDTO, ConsistencyEdge } from '../../types/traffic';

export interface LargeTopologyOptions {
  subsystems?: number;
  groupsPerSubsystem?: number;
  modulesPerGroup?: number;
  edgeCount?: number;
  hubCallers?: number;
  includeExternalSystems?: boolean;
}

/**
 * 生成确定性企业级大规模分层拓扑测试夹具（默认 25 子系统 × 10 功能组 × 10 模块 ≈ 2,775 节点 + 10,000 条边）：
 * - 25 个子系统 (sub_1 .. sub_25, depth=0)，其中 sub_1 为被 12 个子系统依赖的公共底座
 * - 250 个功能组 (grp_1 .. grp_250, depth=1)
 * - 2,500 个功能模块 (mod_1 .. mod_2500, depth=2)
 * - 外部系统容器 ext_container 与外部系统节点 ext_sys_1 / ext_sys_2
 * - 10,000 条跨模块调用契约边 + 一致性比对报告（含跨子系统 VIOLATION 动态违规边）
 */
export function generateLargeTopologyFixture(options: LargeTopologyOptions = {}): {
  graph: TopologyGraphDTO;
  report: ConsistencyReportDTO;
} {
  const subsystemCount = options.subsystems ?? 25;
  const groupsPerSubsystem = options.groupsPerSubsystem ?? 10;
  const modulesPerGroup = options.modulesPerGroup ?? 10;
  const targetEdgeCount = options.edgeCount ?? 10000;
  const hubCallers = options.hubCallers ?? 12;
  const includeExternalSystems = options.includeExternalSystems ?? true;

  const nodes: GraphNode[] = [];
  const edges: GraphEdge[] = [];

  let groupSeq = 0;
  let moduleSeq = 0;

  for (let s = 1; s <= subsystemCount; s++) {
    const subId = `sub_${s}`;
    nodes.push({
      id: subId,
      type: 'SUBSYSTEM',
      depth: 0,
      ancestors: [],
      entity_id: s,
      code: `SUB_${String(s).padStart(2, '0')}`,
      name: s === 1 ? '公共底座子系统-1' : `核心业务子系统-${s}`,
      sort_order: s,
      meta: { owner_id: (s % 4) + 1 },
    });

    for (let g = 1; g <= groupsPerSubsystem; g++) {
      groupSeq++;
      const grpId = `grp_${groupSeq}`;
      nodes.push({
        id: grpId,
        type: 'FUNCTIONAL_GROUP',
        parent_id: subId,
        depth: 1,
        ancestors: [subId],
        entity_id: groupSeq,
        code: `GRP_${String(groupSeq).padStart(3, '0')}`,
        name: `业务功能组-${s}-${g}`,
        sort_order: g,
        meta: { subsystem_id: s, level: 1 },
      });

      for (let m = 1; m <= modulesPerGroup; m++) {
        moduleSeq++;
        const modId = `mod_${moduleSeq}`;
        nodes.push({
          id: modId,
          type: 'MODULE',
          parent_id: grpId,
          depth: 2,
          ancestors: [subId, grpId],
          entity_id: moduleSeq,
          code: `MOD_${String(moduleSeq).padStart(4, '0')}`,
          name: `微服务模块-${s}-${g}-${m}`,
          sort_order: m,
          meta: {
            subsystem_id: s,
            group_id: groupSeq,
            owner_id: (moduleSeq % 5) + 1,
            owner_name: `架构师-${(moduleSeq % 5) + 1}`,
            is_deployed: true,
            provide_count: 2,
          },
        });
      }
    }
  }

  if (includeExternalSystems) {
    nodes.push({
      id: 'ext_container',
      type: 'EXTERNAL_CONTAINER',
      depth: 0,
      ancestors: [],
      code: 'EXT_ROOT',
      name: '外部第三方系统域',
      sort_order: 9000,
    });
    nodes.push({
      id: 'ext_sys_1',
      type: 'EXTERNAL_SYSTEM',
      parent_id: 'ext_container',
      depth: 1,
      ancestors: ['ext_container'],
      entity_id: 9001,
      code: 'UNIONPAY',
      name: '银联清算网关',
      sort_order: 1,
    });
    nodes.push({
      id: 'ext_sys_2',
      type: 'EXTERNAL_SYSTEM',
      parent_id: 'ext_container',
      depth: 1,
      ancestors: ['ext_container'],
      entity_id: 9002,
      code: 'SMS_GATEWAY',
      name: '统一短信网关',
      sort_order: 2,
    });
  }

  const totalModules = moduleSeq;
  const modulesPerSubsystem = groupsPerSubsystem * modulesPerGroup;
  const pairSeen = new Set<string>();
  let edgeSeq = 0;

  // 1. 构造被 hubCallers (默认 12) 个不同子系统依赖的公共底座 sub_1 (目标模块 mod_1)
  const maxCallerSub = Math.min(subsystemCount, hubCallers + 1);
  for (let callerSub = 2; callerSub <= maxCallerSub && edgeSeq < targetEdgeCount; callerSub++) {
    const srcModIdx = (callerSub - 1) * modulesPerSubsystem + 1;
    if (srcModIdx <= totalModules) {
      edgeSeq++;
      pairSeen.add(`${srcModIdx}->1`);
      edges.push({
        id: `edge_${edgeSeq}`,
        kind: 'CROSS_PROCESS',
        source_node_id: `mod_${srcModIdx}`,
        target_node_id: 'mod_1',
        src_module_id: srcModIdx,
        dst_module_id: 1,
        services: [
          {
            service_id: 1,
            service_name: 'svc.v1.CommonAuthService',
            description: `子系统 ${callerSub} 调用公共底座鉴权服务`,
          },
        ],
      });
    }
  }

  // 2. 构造 mod_1 对外调用 12 个下游模块，支撑正向邻域与出站验证
  for (let dst = 2; dst <= Math.min(totalModules, 14) && edgeSeq < targetEdgeCount; dst++) {
    const key = `1->${dst}`;
    if (pairSeen.has(key)) continue;
    pairSeen.add(key);
    edgeSeq++;
    edges.push({
      id: `edge_${edgeSeq}`,
      kind: 'CROSS_PROCESS',
      source_node_id: 'mod_1',
      target_node_id: `mod_${dst}`,
      src_module_id: 1,
      dst_module_id: dst,
      services: [
        {
          service_id: dst,
          service_name: `svc.v1.Module${dst}Service`,
          description: `公共底座分发至模块 ${dst}`,
        },
      ],
    });
  }

  // 3. 外部系统调用边
  if (includeExternalSystems && edgeSeq + 2 <= targetEdgeCount) {
    edgeSeq++;
    edges.push({
      id: `edge_${edgeSeq}`,
      kind: 'CROSS_PROCESS',
      source_node_id: 'mod_1',
      target_node_id: 'ext_sys_1',
      src_module_id: 1,
      services: [{ service_name: 'ext.unionpay.Clear', description: '调用银联清算' }],
    });
    edgeSeq++;
    edges.push({
      id: `edge_${edgeSeq}`,
      kind: 'CROSS_PROCESS',
      source_node_id: 'mod_2',
      target_node_id: 'ext_sys_2',
      src_module_id: 2,
      services: [{ service_name: 'ext.sms.Send', description: '调用短信网关' }],
    });
  }

  // 4. 按确定性步长填充其余子系统内与跨子系统契约边直至达到 targetEdgeCount
  let stride = 1;
  while (edgeSeq < targetEdgeCount) {
    for (let src = 1; src <= totalModules && edgeSeq < targetEdgeCount; src++) {
      // 优先产生同子系统 / 邻近功能组的局部高内聚调用，辅以跨子系统调用
      const offset = stride % 3 === 0 ? stride * 13 : stride;
      const dst = ((src - 1 + offset) % totalModules) + 1;
      if (src === dst) continue;
      const key = `${src}->${dst}`;
      if (pairSeen.has(key)) continue;
      pairSeen.add(key);
      edgeSeq++;
      edges.push({
        id: `edge_${edgeSeq}`,
        kind: src % 5 === 0 ? 'INTRA_PROCESS' : 'CROSS_PROCESS',
        source_node_id: `mod_${src}`,
        target_node_id: `mod_${dst}`,
        src_module_id: src,
        dst_module_id: dst,
        services: [
          {
            service_id: (dst % 6) + 1,
            service_name: `svc.v1.Domain${(dst % 6) + 1}Service`,
            description: `链路 ${src} -> ${dst}`,
          },
        ],
      });
    }
    stride++;
  }

  // 5. 构造一致性比对报告（300 条静态边状态命中 + 8 条跨子系统 VIOLATION 动态边）
  const reportSampleCount = Math.min(300, edges.length);
  const reportEdges: ConsistencyEdge[] = edges
    .slice(0, reportSampleCount)
    .filter((e) => e.src_module_id != null && e.dst_module_id != null)
    .map((e, idx) => ({
      src_module_id: e.src_module_id,
      dst_module_id: e.dst_module_id,
      service_name: e.services[0].service_name,
      operation: 'Execute',
      status: idx % 15 === 0 ? 'UNCOVERED' : 'VERIFIED',
      category: idx % 15 === 0 ? 'UNCOVERED' : 'VERIFIED',
      call_count: (idx + 1) * 5,
    }));

  for (let v = 1; v <= 8; v++) {
    const dstMod = Math.min(totalModules, modulesPerSubsystem * (v + 1) + v);
    reportEdges.push({
      src_module_id: v,
      dst_module_id: dstMod,
      service_name: `viol.v1.IllegalCrossSubSvc${v}`,
      operation: 'Bypass',
      status: 'VIOLATION',
      category: 'VIOLATION',
      call_count: v * 3,
    });
  }

  return {
    graph: {
      view: 'LOGICAL',
      model_version: `v1-logical-large-${nodes.length}`,
      structure_version: `s1-logical-large-${nodes.length}`,
      nodes,
      edges,
    },
    report: {
      session_id: '500',
      edges: reportEdges,
    },
  };
}
