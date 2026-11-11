package decoder

import (
	"testing"

	"code-traefik/models"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// Helper to construct a FileDescriptorProto
func makeFile(name, pkg string, messages []*descriptorpb.DescriptorProto, enums []*descriptorpb.EnumDescriptorProto, services []*descriptorpb.ServiceDescriptorProto) *descriptorpb.FileDescriptorProto {
	return &descriptorpb.FileDescriptorProto{
		Syntax:      proto.String("proto3"),
		Name:        proto.String(name),
		Package:     proto.String(pkg),
		MessageType: messages,
		EnumType:    enums,
		Service:     services,
	}
}

// TestDetectBreakingChanges_ServiceMethodRemoved_AndSignatureChanged 验证服务/方法删除与签名篡改阻断 (M16 / ADR-14)
func TestDetectBreakingChanges_ServiceMethodRemoved_AndSignatureChanged(t *testing.T) {
	oldFile := makeFile("trade/v1/order.proto", "trade.v1",
		[]*descriptorpb.DescriptorProto{
			{Name: proto.String("CreateOrderReq")},
			{Name: proto.String("CreateOrderRsp")},
			{Name: proto.String("QueryOrderReq")},
			{Name: proto.String("QueryOrderRsp")},
			{Name: proto.String("NewReq")},
		},
		nil,
		[]*descriptorpb.ServiceDescriptorProto{
			{
				Name: proto.String("OrderService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:            proto.String("CreateOrder"),
						InputType:       proto.String(".trade.v1.CreateOrderReq"),
						OutputType:      proto.String(".trade.v1.CreateOrderRsp"),
						ClientStreaming: proto.Bool(false),
						ServerStreaming: proto.Bool(false),
					},
					{
						Name:            proto.String("QueryOrder"),
						InputType:       proto.String(".trade.v1.QueryOrderReq"),
						OutputType:      proto.String(".trade.v1.QueryOrderRsp"),
						ClientStreaming: proto.Bool(false),
						ServerStreaming: proto.Bool(false),
					},
				},
			},
			{
				Name: proto.String("AuditService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:       proto.String("AuditLog"),
						InputType:  proto.String(".trade.v1.CreateOrderReq"),
						OutputType: proto.String(".trade.v1.CreateOrderRsp"),
					},
				},
			},
		},
	)

	// 1. 删除服务 AuditService 与 方法 QueryOrder
	newFileRemoved := makeFile("trade/v1/order.proto", "trade.v1",
		[]*descriptorpb.DescriptorProto{
			{Name: proto.String("CreateOrderReq")},
			{Name: proto.String("CreateOrderRsp")},
		},
		nil,
		[]*descriptorpb.ServiceDescriptorProto{
			{
				Name: proto.String("OrderService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:            proto.String("CreateOrder"),
						InputType:       proto.String(".trade.v1.CreateOrderReq"),
						OutputType:      proto.String(".trade.v1.CreateOrderRsp"),
						ClientStreaming: proto.Bool(false),
						ServerStreaming: proto.Bool(false),
					},
				},
			},
		},
	)

	violations := DetectBreakingChanges([]*descriptorpb.FileDescriptorProto{oldFile}, []*descriptorpb.FileDescriptorProto{newFileRemoved})
	if len(violations) != 2 {
		t.Fatalf("期望检出 2 项删除违规，实测检出 %d 项: %+v", len(violations), violations)
	}

	foundMethodRemoved := false
	foundServiceRemoved := false
	for _, v := range violations {
		if v.Rule == RuleServiceOrMethodRemoved && v.Symbol == "trade.v1.OrderService.QueryOrder" {
			foundMethodRemoved = true
		}
		if v.Rule == RuleServiceOrMethodRemoved && v.Symbol == "trade.v1.AuditService" {
			foundServiceRemoved = true
		}
	}
	if !foundMethodRemoved {
		t.Errorf("未检出方法删除违规 trade.v1.OrderService.QueryOrder")
	}
	if !foundServiceRemoved {
		t.Errorf("未检出服务删除违规 trade.v1.AuditService")
	}

	// 2. 变更 RPC 方法入参/出参签名及 streaming 模式
	newFileSignatureChanged := makeFile("trade/v1/order.proto", "trade.v1",
		[]*descriptorpb.DescriptorProto{
			{Name: proto.String("CreateOrderReq")},
			{Name: proto.String("CreateOrderRsp")},
			{Name: proto.String("QueryOrderReq")},
			{Name: proto.String("QueryOrderRsp")},
			{Name: proto.String("NewReq")},
		},
		nil,
		[]*descriptorpb.ServiceDescriptorProto{
			{
				Name: proto.String("OrderService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:            proto.String("CreateOrder"),
						InputType:       proto.String(".trade.v1.NewReq"), // 入参变更为 NewReq
						OutputType:      proto.String(".trade.v1.CreateOrderRsp"),
						ClientStreaming: proto.Bool(true), // 客户端流式变更
						ServerStreaming: proto.Bool(false),
					},
					{
						Name:            proto.String("QueryOrder"),
						InputType:       proto.String(".trade.v1.QueryOrderReq"),
						OutputType:      proto.String(".trade.v1.CreateOrderRsp"), // 出参变更为 CreateOrderRsp
						ClientStreaming: proto.Bool(false),
						ServerStreaming: proto.Bool(true), // 服务端流式变更
					},
				},
			},
			{
				Name: proto.String("AuditService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:       proto.String("AuditLog"),
						InputType:  proto.String(".trade.v1.CreateOrderReq"),
						OutputType: proto.String(".trade.v1.CreateOrderRsp"),
					},
				},
			},
		},
	)

	sigViolations := DetectBreakingChanges([]*descriptorpb.FileDescriptorProto{oldFile}, []*descriptorpb.FileDescriptorProto{newFileSignatureChanged})
	if len(sigViolations) != 4 {
		t.Fatalf("期望检出 4 项签名与流式变更违规，实测检出 %d 项: %+v", len(sigViolations), sigViolations)
	}
	for _, v := range sigViolations {
		if v.Rule != RuleMethodSignatureChanged {
			t.Errorf("期望违规规则为 METHOD_SIGNATURE_CHANGED，实测为 %s", v.Rule)
		}
		if v.ServiceName != "trade.v1.OrderService" {
			t.Errorf("期望关联 ServiceName 为 trade.v1.OrderService，实测为 %s", v.ServiceName)
		}
	}
}

// TestDetectBreakingChanges_TagMutation_TypeIncompatible_AndMapOneof 验证 Tag 篡改、标量类型不兼容、Map与Oneof变异
func TestDetectBreakingChanges_TagMutation_TypeIncompatible_AndMapOneof(t *testing.T) {
	oldFile := makeFile("trade/v1/order.proto", "trade.v1",
		[]*descriptorpb.DescriptorProto{
			{
				Name: proto.String("AccountReq"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:   proto.String("user_id"),
						Number: proto.Int32(1),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
					},
					{
						Name:   proto.String("username"),
						Number: proto.Int32(2),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
					{
						Name:       proto.String("phone"),
						Number:     proto.Int32(3),
						Label:      descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:       descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						OneofIndex: proto.Int32(0),
					},
					{
						Name:       proto.String("email"),
						Number:     proto.Int32(4),
						Label:      descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:       descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						OneofIndex: proto.Int32(0),
					},
					{
						Name:     proto.String("attributes"),
						Number:   proto.Int32(5),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".trade.v1.AccountReq.AttributesEntry"),
					},
				},
				OneofDecl: []*descriptorpb.OneofDescriptorProto{
					{Name: proto.String("contact")},
				},
				NestedType: []*descriptorpb.DescriptorProto{
					{
						Name: proto.String("AttributesEntry"),
						Options: &descriptorpb.MessageOptions{
							MapEntry: proto.Bool(true),
						},
						Field: []*descriptorpb.FieldDescriptorProto{
							{
								Name:   proto.String("key"),
								Number: proto.Int32(1),
								Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
								Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
							},
							{
								Name:   proto.String("value"),
								Number: proto.Int32(2),
								Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
								Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
							},
						},
					},
				},
			},
		},
		nil,
		[]*descriptorpb.ServiceDescriptorProto{
			{
				Name: proto.String("AccountService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:       proto.String("GetAccount"),
						InputType:  proto.String(".trade.v1.AccountReq"),
						OutputType: proto.String(".trade.v1.AccountReq"),
					},
				},
			},
		},
	)

	// 1. Tag 变异测试：user_id Tag 1 -> 10，username 复用了 Tag 1
	newFileTagMutation := makeFile("trade/v1/order.proto", "trade.v1",
		[]*descriptorpb.DescriptorProto{
			{
				Name: proto.String("AccountReq"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:   proto.String("user_id"),
						Number: proto.Int32(10), // Tag 改变
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
					},
					{
						Name:   proto.String("username"),
						Number: proto.Int32(1), // 复用原 Tag 1
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
					{
						Name:       proto.String("phone"),
						Number:     proto.Int32(3),
						Label:      descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:       descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						OneofIndex: proto.Int32(0),
					},
					{
						Name:       proto.String("email"),
						Number:     proto.Int32(4),
						Label:      descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:       descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						OneofIndex: proto.Int32(0),
					},
					{
						Name:     proto.String("attributes"),
						Number:   proto.Int32(5),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".trade.v1.AccountReq.AttributesEntry"),
					},
				},
				OneofDecl: []*descriptorpb.OneofDescriptorProto{
					{Name: proto.String("contact")},
				},
				NestedType: []*descriptorpb.DescriptorProto{
					{
						Name: proto.String("AttributesEntry"),
						Options: &descriptorpb.MessageOptions{
							MapEntry: proto.Bool(true),
						},
						Field: []*descriptorpb.FieldDescriptorProto{
							{
								Name:   proto.String("key"),
								Number: proto.Int32(1),
								Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
								Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
							},
							{
								Name:   proto.String("value"),
								Number: proto.Int32(2),
								Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
								Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
							},
						},
					},
				},
			},
		},
		nil,
		[]*descriptorpb.ServiceDescriptorProto{
			{
				Name: proto.String("AccountService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:       proto.String("GetAccount"),
						InputType:  proto.String(".trade.v1.AccountReq"),
						OutputType: proto.String(".trade.v1.AccountReq"),
					},
				},
			},
		},
	)

	tagViolations := DetectBreakingChanges([]*descriptorpb.FileDescriptorProto{oldFile}, []*descriptorpb.FileDescriptorProto{newFileTagMutation})
	hasTagMutation := false
	for _, v := range tagViolations {
		if v.Rule == RuleTagMutation {
			hasTagMutation = true
			if v.ServiceName != "trade.v1.AccountService" {
				t.Errorf("Tag 变异应通过血缘推导归属至 trade.v1.AccountService，实测为 %s", v.ServiceName)
			}
		}
	}
	if !hasTagMutation {
		t.Fatalf("未能检出 TAG_MUTATION 违规: %+v", tagViolations)
	}

	// 2. 类型变异测试：
	// - username: string -> int32 (标量类型变更)
	// - phone: 从 oneof 中移出 (oneof 拆并)
	// - email: 单值变更为 repeated (基数标签变更)
	// - attributes map: value 类型从 string 变更为 int64 (map value 类型变更)
	newFileTypeIncompatible := makeFile("trade/v1/order.proto", "trade.v1",
		[]*descriptorpb.DescriptorProto{
			{
				Name: proto.String("AccountReq"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:   proto.String("user_id"),
						Number: proto.Int32(1),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
					},
					{
						Name:   proto.String("username"),
						Number: proto.Int32(2),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(), // string -> int32
					},
					{
						Name:   proto.String("phone"),
						Number: proto.Int32(3),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						// 移出 oneof
					},
					{
						Name:   proto.String("email"),
						Number: proto.Int32(4),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(), // optional -> repeated
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
					{
						Name:     proto.String("attributes"),
						Number:   proto.Int32(5),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".trade.v1.AccountReq.AttributesEntry"),
					},
				},
				NestedType: []*descriptorpb.DescriptorProto{
					{
						Name: proto.String("AttributesEntry"),
						Options: &descriptorpb.MessageOptions{
							MapEntry: proto.Bool(true),
						},
						Field: []*descriptorpb.FieldDescriptorProto{
							{
								Name:   proto.String("key"),
								Number: proto.Int32(1),
								Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
								Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
							},
							{
								Name:   proto.String("value"),
								Number: proto.Int32(2),
								Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
								Type:   descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(), // string -> int64
							},
						},
					},
				},
			},
		},
		nil,
		nil,
	)

	typeViolations := DetectBreakingChanges([]*descriptorpb.FileDescriptorProto{oldFile}, []*descriptorpb.FileDescriptorProto{newFileTypeIncompatible})
	incompatibleCount := 0
	for _, v := range typeViolations {
		if v.Rule == RuleTypeIncompatible {
			incompatibleCount++
		}
	}
	if incompatibleCount < 4 {
		t.Fatalf("期望检出至少 4 项 TYPE_INCOMPATIBLE 违规，实测检出 %d 项: %+v", incompatibleCount, typeViolations)
	}
}

// TestDetectBreakingChanges_RemovedField_RequiresNumericReservedRange 验证删除字段必须在 DescriptorProto.ReservedRange 数字区间内声明 (M16)
func TestDetectBreakingChanges_RemovedField_RequiresNumericReservedRange(t *testing.T) {
	oldFile := makeFile("trade/v1/order.proto", "trade.v1",
		[]*descriptorpb.DescriptorProto{
			{
				Name: proto.String("OrderHeader"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:   proto.String("trace_id"),
						Number: proto.Int32(1),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
					{
						Name:   proto.String("span_id"),
						Number: proto.Int32(2),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
					{
						Name:   proto.String("client_ip"),
						Number: proto.Int32(3),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
				},
			},
		},
		[]*descriptorpb.EnumDescriptorProto{
			{
				Name: proto.String("OrderStatus"),
				Value: []*descriptorpb.EnumValueDescriptorProto{
					{Name: proto.String("ORDER_STATUS_UNSPECIFIED"), Number: proto.Int32(0)},
					{Name: proto.String("ORDER_STATUS_CREATED"), Number: proto.Int32(1)},
					{Name: proto.String("ORDER_STATUS_PAID"), Number: proto.Int32(2)},
				},
			},
		},
		nil,
	)

	// 1. 删除 span_id 且未保留数字 Tag -> 阻断
	newWithoutReserve := makeFile("trade/v1/order.proto", "trade.v1",
		[]*descriptorpb.DescriptorProto{
			{
				Name: proto.String("OrderHeader"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:   proto.String("trace_id"),
						Number: proto.Int32(1),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
					{
						Name:   proto.String("client_ip"),
						Number: proto.Int32(3),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
				},
				// 仅保留了字段名字符串，未保留数字 Tag
				ReservedName: []string{"span_id"},
			},
		},
		[]*descriptorpb.EnumDescriptorProto{
			{
				Name: proto.String("OrderStatus"),
				Value: []*descriptorpb.EnumValueDescriptorProto{
					{Name: proto.String("ORDER_STATUS_UNSPECIFIED"), Number: proto.Int32(0)},
					{Name: proto.String("ORDER_STATUS_CREATED"), Number: proto.Int32(1)},
					{Name: proto.String("ORDER_STATUS_PAID"), Number: proto.Int32(2)},
				},
			},
		},
		nil,
	)

	v1 := DetectBreakingChanges([]*descriptorpb.FileDescriptorProto{oldFile}, []*descriptorpb.FileDescriptorProto{newWithoutReserve})
	foundRemovedWithoutReserve := false
	for _, v := range v1 {
		if v.Rule == RuleRemovedWithoutReserve && v.Symbol == "trade.v1.OrderHeader.span_id" {
			foundRemovedWithoutReserve = true
		}
	}
	if !foundRemovedWithoutReserve {
		t.Fatalf("仅保留 reserved string 必须被严格阻断为 REMOVED_WITHOUT_RESERVE: %+v", v1)
	}

	// 2. 删除 span_id 且声明了数字区间 [2, 3) (即 Tag 2) -> 允许通过
	newWithNumericReserve := makeFile("trade/v1/order.proto", "trade.v1",
		[]*descriptorpb.DescriptorProto{
			{
				Name: proto.String("OrderHeader"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:   proto.String("trace_id"),
						Number: proto.Int32(1),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
					{
						Name:   proto.String("client_ip"),
						Number: proto.Int32(3),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
					// 新增一个合法的 optional 字段
					{
						Name:   proto.String("device_id"),
						Number: proto.Int32(4),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
				},
				ReservedRange: []*descriptorpb.DescriptorProto_ReservedRange{
					{Start: proto.Int32(2), End: proto.Int32(3)}, // [2, 3) 覆盖 Tag 2
				},
			},
		},
		[]*descriptorpb.EnumDescriptorProto{
			{
				Name: proto.String("OrderStatus"),
				Value: []*descriptorpb.EnumValueDescriptorProto{
					{Name: proto.String("ORDER_STATUS_UNSPECIFIED"), Number: proto.Int32(0)},
					{Name: proto.String("ORDER_STATUS_CREATED"), Number: proto.Int32(1)},
					{Name: proto.String("ORDER_STATUS_PAID"), Number: proto.Int32(2)},
				},
			},
		},
		nil,
	)

	v2 := DetectBreakingChanges([]*descriptorpb.FileDescriptorProto{oldFile}, []*descriptorpb.FileDescriptorProto{newWithNumericReserve})
	if len(v2) != 0 {
		t.Fatalf("合法使用数字 reserved 废弃字段且新增字段应 PASS，但检出了违规: %+v", v2)
	}

	// 3. 枚举违规测试：
	// a. ORDER_STATUS_PAID 数值从 2 变更为 10 -> ENUM_BREAKING_CHANGE
	// b. 删除 ORDER_STATUS_CREATED (1) 且未声明 EnumReservedRange [1, 1] -> ENUM_BREAKING_CHANGE
	newEnumBreaking := makeFile("trade/v1/order.proto", "trade.v1",
		[]*descriptorpb.DescriptorProto{
			{
				Name: proto.String("OrderHeader"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:   proto.String("trace_id"),
						Number: proto.Int32(1),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
					{
						Name:   proto.String("span_id"),
						Number: proto.Int32(2),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
					{
						Name:   proto.String("client_ip"),
						Number: proto.Int32(3),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
				},
			},
		},
		[]*descriptorpb.EnumDescriptorProto{
			{
				Name: proto.String("OrderStatus"),
				Value: []*descriptorpb.EnumValueDescriptorProto{
					{Name: proto.String("ORDER_STATUS_UNSPECIFIED"), Number: proto.Int32(0)},
					// ORDER_STATUS_CREATED (1) 被直接删除，无 reserved
					{Name: proto.String("ORDER_STATUS_PAID"), Number: proto.Int32(10)}, // 2 变更为 10
				},
			},
		},
		nil,
	)

	v3 := DetectBreakingChanges([]*descriptorpb.FileDescriptorProto{oldFile}, []*descriptorpb.FileDescriptorProto{newEnumBreaking})
	if len(v3) != 2 {
		t.Fatalf("期望检出 2 项枚举破坏性变更违规，实测检出 %d 项: %+v", len(v3), v3)
	}
	for _, v := range v3 {
		if v.Rule != RuleEnumBreakingChange {
			t.Errorf("期望违规规则为 ENUM_BREAKING_CHANGE，实测为 %s", v.Rule)
		}
	}

	// 4. 枚举项删除且在 EnumReservedRange [1, 1] 声明保留 -> 允许通过
	newEnumReserved := makeFile("trade/v1/order.proto", "trade.v1",
		[]*descriptorpb.DescriptorProto{
			{
				Name: proto.String("OrderHeader"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:   proto.String("trace_id"),
						Number: proto.Int32(1),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
					{
						Name:   proto.String("span_id"),
						Number: proto.Int32(2),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
					{
						Name:   proto.String("client_ip"),
						Number: proto.Int32(3),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
				},
			},
		},
		[]*descriptorpb.EnumDescriptorProto{
			{
				Name: proto.String("OrderStatus"),
				Value: []*descriptorpb.EnumValueDescriptorProto{
					{Name: proto.String("ORDER_STATUS_UNSPECIFIED"), Number: proto.Int32(0)},
					{Name: proto.String("ORDER_STATUS_PAID"), Number: proto.Int32(2)},
				},
				ReservedRange: []*descriptorpb.EnumDescriptorProto_EnumReservedRange{
					{Start: proto.Int32(1), End: proto.Int32(1)}, // [1, 1] 闭区间保留原数值 1
				},
			},
		},
		nil,
	)

	v4 := DetectBreakingChanges([]*descriptorpb.FileDescriptorProto{oldFile}, []*descriptorpb.FileDescriptorProto{newEnumReserved})
	if len(v4) != 0 {
		t.Fatalf("合法保留枚举数值 reserved 后应 PASS，但检出了违规: %+v", v4)
	}
}

func TestFindAffectedDependents_MultiImplContract(t *testing.T) {
	db := setupIsolatedDecoderDB(t)

	// 1. 架构元素搭建
	sub := models.Subsystem{Code: "SUB_BREAKING", Name: "破坏性影响面子系统", CreatedBy: 1}
	db.Create(&sub)
	grp := models.FunctionalGroup{SubsystemID: sub.ID, Code: "GRP_BREAKING", Name: "功能组", Level: 1, CreatedBy: 1}
	db.Create(&grp)

	// FM A: CONTRACT_LIB 定义库
	modA := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_LIB_A", Name: "契约定义库A", ModuleKind: models.ModuleKindContractLib, CreatedBy: 1}
	// FM B & C: 实现模块
	modB := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_IMPL_B", Name: "实现模块B", ModuleKind: models.ModuleKindBusiness, CreatedBy: 1}
	modC := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_IMPL_C", Name: "实现模块C", ModuleKind: models.ModuleKindBusiness, CreatedBy: 1}
	// FM X: DISPATCH 消费模块
	modX := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_CONS_X", Name: "分发调用方X", ModuleKind: models.ModuleKindBusiness, CreatedBy: 1}
	// FM Y: 具体实现 B 消费模块
	modY := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_CONS_Y", Name: "直调调用方Y", ModuleKind: models.ModuleKindBusiness, CreatedBy: 1}
	// FM Z: 无关模块
	modZ := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_UNREL_Z", Name: "无关模块Z", ModuleKind: models.ModuleKindBusiness, CreatedBy: 1}

	db.Create(&modA)
	db.Create(&modB)
	db.Create(&modC)
	db.Create(&modX)
	db.Create(&modY)
	db.Create(&modZ)

	// 2. 契约登记
	contract := models.ServiceContract{
		ServiceName:     "cros.pay.PaymentGateway",
		DefinerModuleID: modA.ID,
		ProvideMode:     models.ContractProvideModeMultiImpl,
		CreatedBy:       1,
	}
	db.Create(&contract)

	// 实现服务
	msB := models.ModuleService{
		ModuleID:    modB.ID,
		ContractID:  &contract.ID,
		ServiceName: "cros.pay.PaymentGateway",
		ImplKey:     "alipay",
		CreatedBy:   1,
	}
	msC := models.ModuleService{
		ModuleID:    modC.ID,
		ContractID:  &contract.ID,
		ServiceName: "cros.pay.PaymentGateway",
		ImplKey:     "wechat",
		CreatedBy:   1,
	}
	db.Create(&msB)
	db.Create(&msC)

	// 依赖定义
	// X -> contract (DISPATCH)
	depX := models.ModuleDependency{
		ConsumerModuleID: modX.ID,
		ContractID:       &contract.ID,
		CreatedBy:        1,
	}
	// Y -> msB (具体实现)
	depY := models.ModuleDependency{
		ConsumerModuleID: modY.ID,
		ServiceID:        &msB.ID,
		CreatedBy:        1,
	}
	db.Create(&depX)
	db.Create(&depY)

	// 3. 执行影响面反查
	violations := []BreakingRuleViolation{
		{
			Rule:        RuleServiceOrMethodRemoved,
			ServiceName: "cros.pay.PaymentGateway",
		},
	}
	affected := FindAffectedDependents(db, violations)

	// 4. 验证: 必须包含 A (定义库), B (实现), C (实现), X (DISPATCH依赖), Y (具体实现依赖)
	// 且绝不能包含 Z (无关模块)
	affectedMap := make(map[uint64]string)
	for _, a := range affected {
		affectedMap[a.ModuleID] = a.ModuleCode
	}

	if _, ok := affectedMap[modA.ID]; !ok {
		t.Errorf("受影响模块应包含定义库 FM A (%s)", modA.Code)
	}
	if _, ok := affectedMap[modB.ID]; !ok {
		t.Errorf("受影响模块应包含实现模块 FM B (%s)", modB.Code)
	}
	if _, ok := affectedMap[modC.ID]; !ok {
		t.Errorf("受影响模块应包含实现模块 FM C (%s)", modC.Code)
	}
	if _, ok := affectedMap[modX.ID]; !ok {
		t.Errorf("受影响模块应包含 DISPATCH 消费方 FM X (%s)", modX.Code)
	}
	if _, ok := affectedMap[modY.ID]; !ok {
		t.Errorf("受影响模块应包含具体实现消费方 FM Y (%s)", modY.Code)
	}
	if _, ok := affectedMap[modZ.ID]; ok {
		t.Errorf("受影响模块不应包含无关模块 FM Z (%s)", modZ.Code)
	}
	if len(affected) != 5 {
		t.Fatalf("预期恰好 5 个受影响模块，实际得到 %d: %+v", len(affected), affected)
	}
}
