
í
demo/proto/order.proto
demo.order"C
CompositeQuoteReq
user_id (	RuserId
sku_id (	RskuId"„
CompositeQuoteRsp
code (Rcode
message (	Rmessage
user_id (	RuserId#
balance_cents (RbalanceCents
sku_id (	RskuId
sku_name (	RskuName(
unit_price_cents (RunitPriceCents'
available_stock (RavailableStock"†
CreateOrderReq
user_id (	RuserId
sku_id (	RskuId
quantity (Rquantity(
client_order_ref (	RclientOrderRef"Ç
CreateOrderRsp
code (Rcode
message (	Rmessage
order_id (	RorderId,
total_amount_cents (RtotalAmountCents
status (	Rstatus&
created_at_unix (RcreatedAtUnix"+
OrderDetailReq
order_id (	RorderId"“
OrderDetailRsp
code (Rcode
message (	Rmessage
order_id (	RorderId
user_id (	RuserId
sku_id (	RskuId
quantity (Rquantity,
total_amount_cents (RtotalAmountCents
status (	Rstatus&
created_at_unix	 (RcreatedAtUnix"C
CancelOrderReq
order_id (	RorderId
reason (	Rreason"´
CancelOrderRsp
code (Rcode
message (	Rmessage
order_id (	RorderId2
refunded_amount_cents (RrefundedAmountCents%
released_stock (RreleasedStock2¹
OrderServiceQ
GetCompositeQuote.demo.order.CompositeQuoteReq.demo.order.CompositeQuoteRspE
CreateOrder.demo.order.CreateOrderReq.demo.order.CreateOrderRspH
GetOrderDetail.demo.order.OrderDetailReq.demo.order.OrderDetailRspE
CancelOrder.demo.order.CancelOrderReq.demo.order.CancelOrderRspB%Z#code-traefik/demo/pkg/proto/orderpbbproto3
ƒ
demo/proto/account.protodemo.account"(
GetBalanceReq
user_id (	RuserId"˜
GetBalanceRsp
code (Rcode
message (	Rmessage
user_id (	RuserId
	user_name (	RuserName#
balance_cents (RbalanceCents"p
DeductBalanceReq
user_id (	RuserId!
amount_cents (RamountCents 
biz_order_id (	R
bizOrderId"¨
DeductBalanceRsp
code (Rcode
message (	Rmessage
user_id (	RuserId+
remaining_balance (RremainingBalance 
tx_serial_no (	R
txSerialNo"ˆ
RefundBalanceReq
user_id (	RuserId!
amount_cents (RamountCents 
biz_order_id (	R
bizOrderId
reason (	Rreason"¬
RefundBalanceRsp
code (Rcode
message (	Rmessage
user_id (	RuserId'
current_balance (RcurrentBalance(
refund_serial_no (	RrefundSerialNo"O
DepositBalanceReq
user_id (	RuserId!
amount_cents (RamountCents"j
DepositBalanceRsp
code (Rcode
message (	Rmessage'
current_balance (RcurrentBalance2Î
AccountServiceF

GetBalance.demo.account.GetBalanceReq.demo.account.GetBalanceRspO
DeductBalance.demo.account.DeductBalanceReq.demo.account.DeductBalanceRspO
RefundBalance.demo.account.RefundBalanceReq.demo.account.RefundBalanceRspR
DepositBalance.demo.account.DepositBalanceReq.demo.account.DepositBalanceRspB'Z%code-traefik/demo/pkg/proto/accountpbbproto3
±

demo/proto/inventory.protodemo.inventory"(
GetItemStockReq
sku_id (	RskuId"ˆ
GetItemStockRsp
code (Rcode
message (	Rmessage
sku_id (	RskuId
sku_name (	RskuName(
unit_price_cents (RunitPriceCents
total_stock (R
totalStock!
locked_stock (RlockedStock'
available_stock (RavailableStock"c
LockStockReq
sku_id (	RskuId
quantity (Rquantity 
biz_order_id (	R
bizOrderId"~
LockStockRsp
code (Rcode
message (	Rmessage
lock_id (	RlockId'
locked_quantity (RlockedQuantity"L
ConfirmStockReq
lock_id (	RlockId 
biz_order_id (	R
bizOrderId"h
ConfirmStockRsp
code (Rcode
message (	Rmessage'
remaining_stock (RremainingStock"B
ReleaseStockReq
lock_id (	RlockId
reason (	Rreason"f
ReleaseStockRsp
code (Rcode
message (	Rmessage%
restored_stock (RrestoredStock2Ñ
InventoryServiceP
GetItemStock.demo.inventory.GetItemStockReq.demo.inventory.GetItemStockRspG
	LockStock.demo.inventory.LockStockReq.demo.inventory.LockStockRspP
ConfirmStock.demo.inventory.ConfirmStockReq.demo.inventory.ConfirmStockRspP
ReleaseStock.demo.inventory.ReleaseStockReq.demo.inventory.ReleaseStockRspB)Z'code-traefik/demo/pkg/proto/inventorypbbproto3