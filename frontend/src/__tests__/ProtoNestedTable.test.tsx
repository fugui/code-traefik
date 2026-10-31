import { describe, it, expect } from 'vitest';
import { renderToString } from 'react-dom/server';
import type { ProtoOperation } from '../types';
import { rollupServices, OperationMappingTable } from '../pages/ProtoSchemaWorkbenchPage/OperationMappingTable';

function buildMockOperations(): ProtoOperation[] {
  const ops: ProtoOperation[] = [];

  // Service 1: dophi.account-AccountService (4 operations: 3 Unary, 1 Stream)
  ops.push(
    {
      id: '101',
      schema_id: '1',
      proto_file: 'account.proto',
      proto_package: 'dophi.account',
      proto_service: 'AccountService',
      service_name: 'dophi.account-AccountService',
      operation: 'GetBalance',
      req_message_full_name: 'dophi.account.GetBalanceReq',
      rsp_message_full_name: 'dophi.account.GetBalanceRsp',
      client_streaming: false,
      server_streaming: false,
      is_active: true,
    },
    {
      id: '102',
      schema_id: '1',
      proto_file: 'account.proto',
      proto_package: 'dophi.account',
      proto_service: 'AccountService',
      service_name: 'dophi.account-AccountService',
      operation: 'DeductBalance',
      req_message_full_name: 'dophi.account.DeductBalanceReq',
      rsp_message_full_name: 'dophi.account.DeductBalanceRsp',
      client_streaming: false,
      server_streaming: false,
      is_active: true,
    },
    {
      id: '103',
      schema_id: '1',
      proto_file: 'account.proto',
      proto_package: 'dophi.account',
      proto_service: 'AccountService',
      service_name: 'dophi.account-AccountService',
      operation: 'RefundBalance',
      req_message_full_name: 'dophi.account.RefundBalanceReq',
      rsp_message_full_name: 'dophi.account.RefundBalanceRsp',
      client_streaming: false,
      server_streaming: false,
      is_active: false,
    },
    {
      id: '104',
      schema_id: '1',
      proto_file: 'account.proto',
      proto_package: 'dophi.account',
      proto_service: 'AccountService',
      service_name: 'dophi.account-AccountService',
      operation: 'StreamAuditLogs',
      req_message_full_name: 'dophi.account.StreamAuditLogsReq',
      rsp_message_full_name: 'dophi.account.StreamAuditLogsRsp',
      client_streaming: false,
      server_streaming: true,
      is_active: true,
    }
  );

  // Service 2: dophi.order-OrderService (5 operations: 4 Unary, 1 Stream)
  ops.push(
    {
      id: '201',
      schema_id: '1',
      proto_file: 'order.proto',
      proto_package: 'dophi.order',
      proto_service: 'OrderService',
      service_name: 'dophi.order-OrderService',
      operation: 'CreateOrder',
      req_message_full_name: 'dophi.order.CreateOrderReq',
      rsp_message_full_name: 'dophi.order.CreateOrderRsp',
      is_active: true,
    },
    {
      id: '202',
      schema_id: '1',
      proto_file: 'order.proto',
      proto_package: 'dophi.order',
      proto_service: 'OrderService',
      service_name: 'dophi.order-OrderService',
      operation: 'CancelOrder',
      req_message_full_name: 'dophi.order.CancelOrderReq',
      rsp_message_full_name: 'dophi.order.CancelOrderRsp',
      is_active: true,
    },
    {
      id: '203',
      schema_id: '1',
      proto_file: 'order.proto',
      proto_package: 'dophi.order',
      proto_service: 'OrderService',
      service_name: 'dophi.order-OrderService',
      operation: 'QueryOrder',
      req_message_full_name: 'dophi.order.QueryOrderReq',
      rsp_message_full_name: 'dophi.order.QueryOrderRsp',
      is_active: false,
    },
    {
      id: '204',
      schema_id: '1',
      proto_file: 'order.proto',
      proto_package: 'dophi.order',
      proto_service: 'OrderService',
      service_name: 'dophi.order-OrderService',
      operation: 'ListOrders',
      req_message_full_name: 'dophi.order.ListOrdersReq',
      rsp_message_full_name: 'dophi.order.ListOrdersRsp',
      is_active: false,
    },
    {
      id: '205',
      schema_id: '1',
      proto_file: 'order.proto',
      proto_package: 'dophi.order',
      proto_service: 'OrderService',
      service_name: 'dophi.order-OrderService',
      operation: 'SyncOrdersStream',
      req_message_full_name: 'dophi.order.SyncOrdersReq',
      rsp_message_full_name: 'dophi.order.SyncOrdersRsp',
      client_streaming: true,
      server_streaming: false,
      is_active: false,
    }
  );

  // Service 3: dophi.inventory-InventoryService (3 operations: 3 Unary, 0 Stream)
  ops.push(
    {
      id: '301',
      schema_id: '1',
      proto_file: 'inventory.proto',
      proto_package: 'dophi.inventory',
      proto_service: 'InventoryService',
      service_name: 'dophi.inventory-InventoryService',
      operation: 'LockStock',
      req_message_full_name: 'dophi.inventory.LockStockReq',
      rsp_message_full_name: 'dophi.inventory.LockStockRsp',
      is_active: true,
    },
    {
      id: '302',
      schema_id: '1',
      proto_file: 'inventory.proto',
      proto_package: 'dophi.inventory',
      proto_service: 'InventoryService',
      service_name: 'dophi.inventory-InventoryService',
      operation: 'UnlockStock',
      req_message_full_name: 'dophi.inventory.UnlockStockReq',
      rsp_message_full_name: 'dophi.inventory.UnlockStockRsp',
      is_active: true,
    },
    {
      id: '303',
      schema_id: '1',
      proto_file: 'inventory.proto',
      proto_package: 'dophi.inventory',
      proto_service: 'InventoryService',
      service_name: 'dophi.inventory-InventoryService',
      operation: 'QueryStock',
      req_message_full_name: 'dophi.inventory.QueryStockReq',
      rsp_message_full_name: 'dophi.inventory.QueryStockRsp',
      is_active: true,
    }
  );

  return ops;
}

describe('ProtoNestedTable Suite (M7-P2)', () => {
  it('testServiceGroupRollup_Aggregation: 12 operations correctly rollup into 3 services', () => {
    const ops = buildMockOperations();
    expect(ops.length).toBe(12);

    const claimMap = {
      'dophi.account-AccountService': {
        id: '1',
        code: 'MOD_ACCOUNT',
        name: '账户模块',
        subsystemName: '用户子系统',
      },
    };

    const groups = rollupServices(ops, claimMap);
    // 1. 验证聚合为 3 个服务
    expect(groups.length).toBe(3);

    const accountSvc = groups.find((g) => g.serviceName === 'dophi.account-AccountService');
    const orderSvc = groups.find((g) => g.serviceName === 'dophi.order-OrderService');
    const invSvc = groups.find((g) => g.serviceName === 'dophi.inventory-InventoryService');

    expect(accountSvc).toBeDefined();
    expect(orderSvc).toBeDefined();
    expect(invSvc).toBeDefined();

    // 2. 验证 AccountService RPC 统计与认领
    expect(accountSvc?.totalOperations).toBe(4);
    expect(accountSvc?.unaryCount).toBe(3);
    expect(accountSvc?.streamCount).toBe(1);
    expect(accountSvc?.isActive).toBe(true);
    expect(accountSvc?.claimedByModule?.code).toBe('MOD_ACCOUNT');

    // 3. 验证 OrderService
    expect(orderSvc?.totalOperations).toBe(5);
    expect(orderSvc?.unaryCount).toBe(4);
    expect(orderSvc?.streamCount).toBe(1);
    expect(orderSvc?.claimedByModule).toBeUndefined(); // 孤儿未认领契约

    // 4. 验证 InventoryService
    expect(invSvc?.totalOperations).toBe(3);
    expect(invSvc?.unaryCount).toBe(3);
    expect(invSvc?.streamCount).toBe(0);

    // 5. 渲染组件默认处于折叠态 (不包含子方法详细行)
    const html = renderToString(
      <OperationMappingTable
        operations={ops}
        onSelectOperation={() => {}}
        claimMap={claimMap}
      />
    );

    // 包含 3 个服务名
    expect(html).toContain('dophi.account-AccountService');
    expect(html).toContain('dophi.order-OrderService');
    expect(html).toContain('dophi.inventory-InventoryService');
    // 包含认领徽章与孤儿徽章
    expect(html).toContain('MOD_ACCOUNT');
    expect(html).toContain('未认领孤儿契约');
  });

  it('testNestedTable_SearchPenetration: search keyword auto-expands matching service and highlights', () => {
    const ops = buildMockOperations();
    const noop = () => {};

    // 1. 搜索 DeductBalance (属于 dophi.account-AccountService)
    const htmlWithSearch = renderToString(
      <OperationMappingTable
        operations={ops}
        onSelectOperation={noop}
        searchKeyword="DeductBalance"
      />
    );

    // 包含高亮标记
    expect(htmlWithSearch).toContain('<mark class="code-search-highlight">DeductBalance</mark>');
    // 命中的服务行展开，渲染了子方法行
    expect(htmlWithSearch).toContain('data-testid="op-row-DeductBalance"');
    expect(htmlWithSearch).toContain('dophi.account.DeductBalanceReq');

    // 未命中的服务方法不应展示
    expect(htmlWithSearch).not.toContain('data-testid="op-row-CreateOrder"');
    expect(htmlWithSearch).not.toContain('data-testid="op-row-LockStock"');
  });
});

