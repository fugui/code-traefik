package decoder_test

import (
	"strings"
	"testing"

	"code-traefik/internal/server/decoder"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// buildComplexFieldTreeDescriptor 构建包含基本标量、嵌套子 Message、Repeated 数组、Map 及循环引用定义的描述集
func buildComplexFieldTreeDescriptor(t *testing.T) *descriptorpb.FileDescriptorSet {
	t.Helper()

	childMsgProto := &descriptorpb.DescriptorProto{
		Name: proto.String("ChildMessage"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:     proto.String("child"),
				Number:   proto.Int32(1),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				JsonName: proto.String("child"),
			},
		},
	}

	itemMsgProto := &descriptorpb.DescriptorProto{
		Name: proto.String("ItemMessage"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:     proto.String("id"),
				Number:   proto.Int32(1),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
				JsonName: proto.String("id"),
			},
		},
	}

	mapEntryProto := &descriptorpb.DescriptorProto{
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
				Name:     proto.String("value"),
				Number:   proto.Int32(2),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
				TypeName: proto.String(".dophi.test.ItemMessage"),
			},
		},
	}

	// 循环引用结构：ListNode 包含 next 指向自身
	listNodeProto := &descriptorpb.DescriptorProto{
		Name: proto.String("ListNode"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:     proto.String("val"),
				Number:   proto.Int32(1),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
				JsonName: proto.String("val"),
			},
			{
				Name:     proto.String("next"),
				Number:   proto.Int32(2),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
				TypeName: proto.String(".dophi.test.ListNode"),
				JsonName: proto.String("next"),
			},
		},
	}

	rootMsgProto := &descriptorpb.DescriptorProto{
		Name: proto.String("ComplexRootMessage"),
		NestedType: []*descriptorpb.DescriptorProto{
			mapEntryProto,
		},
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:     proto.String("order_id"),
				Number:   proto.Int32(1),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
				JsonName: proto.String("orderId"),
			},
			{
				Name:     proto.String("buyer_name"),
				Number:   proto.Int32(2),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				JsonName: proto.String("buyerName"),
			},
			{
				Name:     proto.String("parent"),
				Number:   proto.Int32(3),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
				TypeName: proto.String(".dophi.test.ChildMessage"),
				JsonName: proto.String("parent"),
			},
			{
				Name:     proto.String("items"),
				Number:   proto.Int32(4),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
				TypeName: proto.String(".dophi.test.ItemMessage"),
				JsonName: proto.String("items"),
			},
			{
				Name:     proto.String("attributes"),
				Number:   proto.Int32(5),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
				TypeName: proto.String(".dophi.test.ComplexRootMessage.AttributesEntry"),
				JsonName: proto.String("attributes"),
			},
		},
	}

	fileProto := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("dophi/test/complex.proto"),
		Package: proto.String("dophi.test"),
		MessageType: []*descriptorpb.DescriptorProto{
			childMsgProto,
			itemMsgProto,
			listNodeProto,
			rootMsgProto,
		},
		SourceCodeInfo: &descriptorpb.SourceCodeInfo{
			Location: []*descriptorpb.SourceCodeInfo_Location{
				// 对应 rootMsgProto (message index 3) 的 order_id (field index 0)
				// Path: 4 (MessageType), 3 (index 3), 2 (Field), 0 (index 0)
				{
					Path:            []int32{4, 3, 2, 0},
					Span:            []int32{10, 2, 30},
					LeadingComments: proto.String(" 订单全局唯一主键 ID\n"),
				},
				// 对应 rootMsgProto 的 buyer_name (field index 1)
				{
					Path:             []int32{4, 3, 2, 1},
					Span:             []int32{11, 2, 30},
					TrailingComments: proto.String(" 买家展示姓名\n"),
				},
			},
		},
	}

	return &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{fileProto},
	}
}

func TestExtractMessageFieldTree_Recursive(t *testing.T) {
	fds := buildComplexFieldTreeDescriptor(t)
	files, err := protodesc.NewFiles(fds)
	if err != nil {
		t.Fatalf("protodesc.NewFiles 失败: %v", err)
	}

	desc, err := files.FindDescriptorByName("dophi.test.ComplexRootMessage")
	if err != nil {
		t.Fatalf("查找 ComplexRootMessage 描述失败: %v", err)
	}
	msgDesc, ok := desc.(protoreflect.MessageDescriptor)
	if !ok {
		t.Fatalf("desc 不是 MessageDescriptor")
	}

	tree := decoder.ExtractMessageFieldTree(msgDesc)
	if len(tree) != 5 {
		t.Fatalf("期望顶级有 5 个字段，实际为 %d", len(tree))
	}

	// 1. 验证标量 order_id
	orderIDNode := tree[0]
	if orderIDNode.Name != "order_id" || orderIDNode.TagNumber != 1 || orderIDNode.Kind != "INT64" {
		t.Errorf("order_id 节点属性不符合预期: %+v", orderIDNode)
	}
	if orderIDNode.FieldPath != "order_id" {
		t.Errorf("order_id 路径期望 'order_id'，实际 '%s'", orderIDNode.FieldPath)
	}

	// 2. 验证 buyer_name
	buyerNode := tree[1]
	if buyerNode.Name != "buyer_name" || buyerNode.TagNumber != 2 || buyerNode.Kind != "STRING" {
		t.Errorf("buyer_name 节点属性不符合预期: %+v", buyerNode)
	}

	// 3. 验证嵌套子 Message parent -> child (路径 parent.child)
	parentNode := tree[2]
	if parentNode.Name != "parent" || parentNode.Kind != "MESSAGE" {
		t.Errorf("parent 节点属性不符合预期: %+v", parentNode)
	}
	if len(parentNode.Children) != 1 {
		t.Fatalf("parent 期望有 1 个子字段，实际为 %d", len(parentNode.Children))
	}
	childNode := parentNode.Children[0]
	if childNode.Name != "child" || childNode.FieldPath != "parent.child" {
		t.Errorf("child 路径期望 'parent.child'，实际 '%s'", childNode.FieldPath)
	}

	// 4. 验证 Repeated 数组 items -> id (路径 items[*].id)
	itemsNode := tree[3]
	if itemsNode.Name != "items" || itemsNode.Cardinality != "REPEATED" {
		t.Errorf("items 节点属性不符合预期: %+v", itemsNode)
	}
	if len(itemsNode.Children) != 1 {
		t.Fatalf("items 期望有 1 个子字段，实际为 %d", len(itemsNode.Children))
	}
	itemIDNode := itemsNode.Children[0]
	if itemIDNode.Name != "id" || itemIDNode.FieldPath != "items[*].id" || itemIDNode.Kind != "INT64" {
		t.Errorf("items[*].id 属性或路径不符合预期: %+v", itemIDNode)
	}

	// 5. 验证 Map 结构 attributes
	mapNode := tree[4]
	if mapNode.Name != "attributes" || !mapNode.IsMap {
		t.Errorf("attributes 期望 is_map=true，实际: %+v", mapNode)
	}
}

func TestExtractMessageFieldTree_SourceComments(t *testing.T) {
	fds := buildComplexFieldTreeDescriptor(t)
	files, err := protodesc.NewFiles(fds)
	if err != nil {
		t.Fatalf("protodesc.NewFiles 失败: %v", err)
	}

	desc, err := files.FindDescriptorByName("dophi.test.ComplexRootMessage")
	if err != nil {
		t.Fatalf("查找 ComplexRootMessage 失败: %v", err)
	}
	msgDesc := desc.(protoreflect.MessageDescriptor)
	tree := decoder.ExtractMessageFieldTree(msgDesc)

	// order_id 包含 leading_comments
	if !strings.Contains(tree[0].Comments, "订单全局唯一主键 ID") {
		t.Errorf("order_id 注释期望包含 '订单全局唯一主键 ID'，实际为 '%s'", tree[0].Comments)
	}

	// buyer_name 包含 trailing_comments
	if !strings.Contains(tree[1].Comments, "买家展示姓名") {
		t.Errorf("buyer_name 注释期望包含 '买家展示姓名'，实际为 '%s'", tree[1].Comments)
	}

	// parent 无注释，应为空字符串
	if tree[2].Comments != "" {
		t.Errorf("parent 期望空注释，实际为 '%s'", tree[2].Comments)
	}
}

func TestExtractMessageFieldTree_AntiCycleMaxDepth12(t *testing.T) {
	fds := buildComplexFieldTreeDescriptor(t)
	files, err := protodesc.NewFiles(fds)
	if err != nil {
		t.Fatalf("protodesc.NewFiles 失败: %v", err)
	}

	desc, err := files.FindDescriptorByName("dophi.test.ListNode")
	if err != nil {
		t.Fatalf("查找 ListNode 失败: %v", err)
	}
	msgDesc := desc.(protoreflect.MessageDescriptor)

	// 执行递归提取：面对 ListNode -> next -> ListNode 循环引用，必须平滑截断于 12 层
	tree := decoder.ExtractMessageFieldTree(msgDesc)
	if len(tree) == 0 {
		t.Fatalf("ListNode 树不能为空")
	}

	curr := tree
	depth := 0
	foundTruncated := false
	for depth < 20 {
		var nextNode *decoder.ProtoFieldNodeDTO
		for i := range curr {
			if curr[i].Name == "next" {
				nextNode = &curr[i]
				break
			}
		}
		if nextNode == nil {
			break
		}
		depth++
		if nextNode.IsTruncated {
			foundTruncated = true
			break
		}
		curr = nextNode.Children
	}

	if !foundTruncated {
		t.Fatalf("未检测到 is_truncated 标记截断，可能未正确防环截断")
	}
	if depth > 12 {
		t.Fatalf("递归深度超过 12 层截断上限: %d", depth)
	}
}
