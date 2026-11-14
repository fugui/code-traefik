package decoder

import (
	"context"
	"errors"
	"strings"
	"testing"

	"code-traefik/models"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestProtoCompile_FourTierResolver_ExternalSkipAndStripSourceInfo(t *testing.T) {
	db := setupIsolatedDecoderDB(t)
	reg, err := NewRegistry(db)
	if err != nil {
		t.Fatalf("NewRegistry 失败: %v", err)
	}

	// 1. 准备 Tier 3 活跃池中的上游 BUSINESS Schema（提供 user.v1.UserService，供下游跨包引用）
	tier3Compiled, err := CompileProtoBundle(context.Background(), nil, CompileProtoBundleInput{
		Name:     "user-service-schema",
		Category: models.ProtoCategoryBusiness,
		Files: map[string]string{
			"user/v1/user.proto": `syntax = "proto3";
package user.v1;
// UserProfile 用户资料实体
message UserProfile {
  string user_id = 1; // 用户主键
  string nickname = 2;
}
// UserService 用户服务
service UserService {
  // GetUser 查询用户
  rpc GetUser(UserProfile) returns (UserProfile);
}
`,
		},
	})
	if err != nil {
		t.Fatalf("编译上游 user-service-schema 失败: %v", err)
	}

	_, _, _, err = reg.UploadSchema(UploadSchemaInput{
		Name:         "user-service-schema",
		Category:     models.ProtoCategoryBusiness,
		FileName:     "user.pb",
		RawBytes:     tier3Compiled.DescriptorBytes,
		AutoActivate: true,
		OperatorID:   1,
	})
	if err != nil {
		t.Fatalf("上传并激活上游 user-service-schema 失败: %v", err)
	}

	// 2. 准备 Tier 2 ProtoSourceProvider（模拟 BASE_LIB 优先与跨包 .proto 源文件提供，先试草稿直编再激活闭环）
	provider := NewInMemoryProtoSourceProvider()
	if err := provider.Put("common/base.proto", models.ProtoCategoryBusiness, []byte(`syntax = "proto3";
package common.v1;
message CommonHeader { string stale_business = 1; }
`)); err != nil {
		t.Fatalf("注入 Tier2 优先级对照片失败: %v", err)
	}
	if err := provider.Put("common/base.proto", models.ProtoCategoryBaseLib, []byte(`syntax = "proto3";
package common.v1;
import "google/protobuf/timestamp.proto";
// CommonHeader 公共请求头（不应泄漏注释与 SourceCodeInfo 至下游 .pb）
message CommonHeader {
  string trace_id = 1; // 追踪链路 ID
  google.protobuf.Timestamp sent_at = 2;
}
`)); err != nil {
		t.Fatalf("注入 Tier2 BASE_LIB 源码失败: %v", err)
	}

	// 先测试草稿模式仅依赖 Tier 1 + Tier 2(无需DB预存) + Tier 3(CompiledRegistry) + Tier 4(WKT)
	draftSchema, draftOps, _, err := reg.CompileAndSaveSchema(context.Background(), CompileProtoBundleInput{
		Name:             "order-service-schema",
		Category:         models.ProtoCategoryBusiness,
		VersionTag:       "v0.9.0-draft",
		Description:      "订单契约草稿编译",
		ProtoIncludeDirs: []string{".", "include"},
		Files: map[string]string{
			"include/order/types.proto": `syntax = "proto3";
package order.v1;
import "common/base.proto";
// OrderItem 订单明细注释
message OrderItem {
  common.v1.CommonHeader header = 1; // 请求头字段注释
  string sku = 2;
}
`,
			"order/order.proto": `syntax = "proto3";
package order.v1;
import "order/types.proto";
import "user/v1/user.proto";
import "google/protobuf/timestamp.proto";
// OrderReq 下单请求
message OrderReq {
  OrderItem item = 1;
  user.v1.UserProfile buyer = 2;
  google.protobuf.Timestamp created_at = 3;
}
// OrderService 订单服务注释
service OrderService {
  // CreateOrder 创建订单接口注释
  rpc CreateOrder(OrderReq) returns (OrderItem);
}
`,
		},
		AutoActivate:   false,
		SourceType:     models.ProtoSourceOnlineCompile,
		SourceProvider: provider,
		OperatorID:     1,
	})
	if err != nil {
		t.Fatalf("草稿态四级虚拟 Resolver CompileAndSaveSchema 失败: %v", err)
	}
	if len(draftOps) != 1 || draftSchema.ServiceCount != 1 {
		t.Fatalf("草稿编译应跳过 external_imports (含 BASE_LIB 与跨包 user.v1.UserService)，实测 service=%d ops=%+v",
			draftSchema.ServiceCount, draftOps)
	}

	// 将上游 common-base 正式入池激活，随后测试完整 AutoActivate 激活链路
	_, _, _, err = reg.CompileAndSaveSchema(context.Background(), CompileProtoBundleInput{
		Name:     "common-base-schema",
		Category: models.ProtoCategoryBaseLib,
		Files: map[string]string{
			"common/base.proto": `syntax = "proto3";
package common.v1;
import "google/protobuf/timestamp.proto";
// CommonHeader 公共请求头（不应泄漏注释与 SourceCodeInfo 至下游 .pb）
message CommonHeader {
  string trace_id = 1; // 追踪链路 ID
  google.protobuf.Timestamp sent_at = 2;
}
`,
		},
		AutoActivate: true,
		OperatorID:   1,
	})
	if err != nil {
		t.Fatalf("激活上游 BASE_LIB 失败: %v", err)
	}

	// 3. 正式编译并激活当前包（同时验证 AutoActivate=true）
	savedSchema, savedOps, _, err := reg.CompileAndSaveSchema(context.Background(), CompileProtoBundleInput{
		Name:             "order-service-schema",
		Category:         models.ProtoCategoryBusiness,
		VersionTag:       "v1.0.0",
		Description:      "订单契约在线直编",
		ProtoIncludeDirs: []string{".", "include"},
		Files: map[string]string{
			"include/order/types.proto": `syntax = "proto3";
package order.v1;
import "common/base.proto";
// OrderItem 订单明细注释
message OrderItem {
  common.v1.CommonHeader header = 1; // 请求头字段注释
  string sku = 2;
}
`,
			"order/order.proto": `syntax = "proto3";
package order.v1;
import "order/types.proto";
import "user/v1/user.proto";
import "google/protobuf/timestamp.proto";
// OrderReq 下单请求
message OrderReq {
  OrderItem item = 1;
  user.v1.UserProfile buyer = 2;
  google.protobuf.Timestamp created_at = 3;
}
// OrderService 订单服务注释
service OrderService {
  // CreateOrder 创建订单接口注释
  rpc CreateOrder(OrderReq) returns (OrderItem);
}
`,
		},
		AutoActivate:   true,
		SourceType:     models.ProtoSourceOnlineCompile,
		SourceProvider: provider,
		OperatorID:     1,
	})
	if err != nil {
		t.Fatalf("CompileAndSaveSchema 失败: %v", err)
	}

	if savedSchema.SourceType != models.ProtoSourceOnlineCompile {
		t.Errorf("期望 source_type 为 ONLINE_COMPILE，实测为 %s", savedSchema.SourceType)
	}
	if savedSchema.ProtoFileCount != 2 {
		t.Errorf("期望自有 proto_file_count = 2，实测为 %d", savedSchema.ProtoFileCount)
	}
	// 严禁把 Tier 3 外部导入的 user.v1.UserService 写入当前包的 Operation，避免激活时与上游归属冲突
	if savedSchema.ServiceCount != 1 || savedSchema.OperationCount != 1 {
		t.Fatalf("期望仅提取自有 1 Service / 1 Operation，实测 service=%d op=%d ops=%+v",
			savedSchema.ServiceCount, savedSchema.OperationCount, savedOps)
	}
	if savedOps[0].ServiceName != "order.v1.OrderService" || savedOps[0].Operation != "CreateOrder" {
		t.Errorf("提取的方法不符: %+v", savedOps[0])
	}

	// 4. 校验 DescriptorBytes 中的 SourceCodeInfo 保留与外部依赖剥离铁律
	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(savedSchema.DescriptorBytes, &fds); err != nil {
		t.Fatalf("解析生成 DescriptorBytes 失败: %v", err)
	}

	seenOwnSourceInfo := 0
	seenExtStripped := 0
	for _, f := range fds.File {
		switch f.GetName() {
		case "order/order.proto", "order/types.proto":
			if f.GetSourceCodeInfo() == nil || len(f.GetSourceCodeInfo().GetLocation()) == 0 {
				t.Errorf("自有文件 %s 必须通过 SourceInfoStandard 保留 SourceCodeInfo", f.GetName())
			}
			seenOwnSourceInfo++
		case "common/base.proto", "user/v1/user.proto":
			if f.GetSourceCodeInfo() != nil {
				t.Errorf("外部依赖文件 %s 必须剥离 SourceCodeInfo 防止 .pb 膨胀", f.GetName())
			}
			seenExtStripped++
		}
	}
	if seenOwnSourceInfo != 2 {
		t.Errorf("期望校验 2 个自有文件 SourceCodeInfo，实测 %d", seenOwnSourceInfo)
	}
	if seenExtStripped == 0 {
		t.Errorf("期望外部依赖文件被剥离 SourceCodeInfo 并被检验，实测 seenExtStripped=0")
	}

	// 确保注册表运行期成功重载新方法
	if op := reg.LookupOperation("order.v1.OrderService", "CreateOrder"); op == nil {
		t.Errorf("在线编译激活后 CompiledRegistry 应能解析 order.v1.OrderService:CreateOrder")
	}
}

func TestProtoCompile_PathTraversalAnd16MBLimitRejected(t *testing.T) {
	traversalCases := []string{
		"../secret.proto",
		"/etc/passwd.proto",
		"sub/../../escape.proto",
		"bad\x00file.proto",
		"",
	}
	for _, badPath := range traversalCases {
		_, err := CompileProtoBundle(context.Background(), nil, CompileProtoBundleInput{
			Name: "bad-path",
			Files: map[string]string{
				badPath: `syntax = "proto3"; package p; message M {}`,
			},
		})
		if err == nil {
			t.Fatalf("非法路径 %q 应当被拦截，但编译通过", badPath)
		}
		if !errors.Is(err, ErrPathTraversalDenied) {
			t.Errorf("非法路径 %q 应返回 ErrPathTraversalDenied，实测: %v", badPath, err)
		}
	}

	// include dir 路径穿越也要拦截
	_, err := CompileProtoBundle(context.Background(), nil, CompileProtoBundleInput{
		Name:             "bad-inc",
		ProtoIncludeDirs: []string{"../external"},
		Files: map[string]string{
			"a.proto": `syntax = "proto3"; package p; message M {}`,
		},
	})
	if err == nil || !errors.Is(err, ErrPathTraversalDenied) {
		t.Errorf("非法 include dir 应返回 ErrPathTraversalDenied，实测: %v", err)
	}

	// 超过 16MB 源码大小拦截
	huge := "syntax = \"proto3\";\npackage big;\nmessage M {\n  // " + strings.Repeat("A", models.MaxDescriptorFileSize) + "\n}\n"
	_, err = CompileProtoBundle(context.Background(), nil, CompileProtoBundleInput{
		Name: "huge-proto",
		Files: map[string]string{
			"big.proto": huge,
		},
	})
	if err == nil || !errors.Is(err, ErrDescriptorTooLarge) {
		t.Errorf("超过 16MB 源码应返回 ErrDescriptorTooLarge，实测: %v", err)
	}

	// 语法错误诊断信息行号/列号返回
	_, err = CompileProtoBundle(context.Background(), nil, CompileProtoBundleInput{
		Name: "syntax-err",
		Files: map[string]string{
			"broken.proto": "syntax = \"proto3\";\npackage p;\nmessage Bad {\n  unknown_type field = ;\n}\n",
		},
	})
	var cErr *CompileError
	if !errors.As(err, &cErr) {
		t.Fatalf("语法错误应返回 *CompileError，实测: %v", err)
	}
	if cErr.Code != "COMPILE_SYNTAX_ERROR" || len(cErr.Diagnostics) == 0 {
		t.Errorf("应返回 COMPILE_SYNTAX_ERROR 及行列诊断信息，实测: %+v", cErr)
	}
	if cErr.Diagnostics[0].File != "broken.proto" || cErr.Diagnostics[0].Line <= 0 {
		t.Errorf("诊断应包含文件与正行号，实测: %+v", cErr.Diagnostics[0])
	}
}

func TestBreaking_BaseLibMessageChangePropagatesViaReverseImports(t *testing.T) {
	db := setupIsolatedDecoderDB(t)
	reg, err := NewRegistry(db)
	if err != nil {
		t.Fatalf("NewRegistry 失败: %v", err)
	}

	// 1. 编译并激活 0-Service BASE_LIB (common/base.proto)
	baseRes, _, _, err := reg.CompileAndSaveSchema(context.Background(), CompileProtoBundleInput{
		Name:     "common-base-lib",
		Category: models.ProtoCategoryBaseLib,
		Files: map[string]string{
			"common/base.proto": `syntax = "proto3";
package common.v1;
message CommonHeader {
  string trace_id = 1;
}
`,
		},
		AutoActivate: true,
		OperatorID:   1,
	})
	if err != nil {
		t.Fatalf("激活 BASE_LIB v1 失败: %v", err)
	}
	if baseRes.ServiceCount != 0 {
		t.Fatalf("BASE_LIB 应为 0 Service，实测 %d", baseRes.ServiceCount)
	}

	// 2. 编译并激活中间无 Service 传递包 (common/mid.proto -> import common/base.proto)
	_, _, _, err = reg.CompileAndSaveSchema(context.Background(), CompileProtoBundleInput{
		Name:     "common-mid-lib",
		Category: models.ProtoCategoryBaseLib,
		Files: map[string]string{
			"common/mid.proto": `syntax = "proto3";
package common.mid;
import "common/base.proto";
message Envelope {
  common.v1.CommonHeader header = 1;
}
`,
		},
		AutoActivate: true,
		OperatorID:   1,
	})
	if err != nil {
		t.Fatalf("激活 mid lib 失败: %v", err)
	}

	// 3. 编译并激活业务包 (order/order.proto -> import common/mid.proto -> 导出 OrderService)
	_, _, _, err = reg.CompileAndSaveSchema(context.Background(), CompileProtoBundleInput{
		Name:     "order-biz",
		Category: models.ProtoCategoryBusiness,
		Files: map[string]string{
			"order/order.proto": `syntax = "proto3";
package order.v1;
import "common/mid.proto";
message CreateReq {
  common.mid.Envelope env = 1;
}
message CreateRsp {
  bool ok = 1;
}
service OrderService {
  rpc Create(CreateReq) returns (CreateRsp);
}
`,
		},
		AutoActivate: true,
		OperatorID:   1,
	})
	if err != nil {
		t.Fatalf("激活 order-biz 失败: %v", err)
	}

	// 4. 搭建架构元素：MOD_ORDER (PROVIDE order.v1.OrderService) <- MOD_GATEWAY (DEPEND) + MOD_UNRELATED
	sub := models.Subsystem{Code: "SUB_CORE", Name: "核心子系统", CreatedBy: 1}
	db.Create(&sub)
	grp := models.FunctionalGroup{SubsystemID: sub.ID, Code: "GRP_CORE", Name: "核心组", Path: "/GRP_CORE", CreatedBy: 1}
	db.Create(&grp)

	modOrder := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_ORDER", Name: "订单模块", ModuleKind: models.ModuleKindBusiness, CreatedBy: 1}
	modGw := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_GATEWAY", Name: "网关模块", ModuleKind: models.ModuleKindBusiness, CreatedBy: 1}
	modUnrel := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_UNRELATED", Name: "无关模块", ModuleKind: models.ModuleKindBusiness, CreatedBy: 1}
	db.Create(&modOrder)
	db.Create(&modGw)
	db.Create(&modUnrel)

	svcOrder := models.ModuleService{ModuleID: modOrder.ID, ServiceName: "order.v1.OrderService", CreatedBy: 1}
	db.Create(&svcOrder)
	depGw := models.ModuleDependency{ConsumerModuleID: modGw.ID, ServiceID: &svcOrder.ID, CreatedBy: 1}
	db.Create(&depGw)

	// 5. 变更 common/base.proto 字段类型 (string -> int64) 并尝试 auto_activate -> 应触发 0-Service 闭包穿透拦截
	_, _, _, err = reg.CompileAndSaveSchema(context.Background(), CompileProtoBundleInput{
		Name:     "common-base-lib",
		Category: models.ProtoCategoryBaseLib,
		Files: map[string]string{
			"common/base.proto": `syntax = "proto3";
package common.v1;
message CommonHeader {
  int64 trace_id = 1;
}
`,
		},
		AutoActivate: true,
		OperatorID:   1,
	})
	var breakingErr *SchemaBreakingError
	if !errors.As(err, &breakingErr) {
		t.Fatalf("BASE_LIB 破坏性变更应触发 *SchemaBreakingError，实测: %v", err)
	}
	if len(breakingErr.BreakingRules) == 0 {
		t.Fatalf("应检出 FIELD_TYPE_CHANGED 破坏性规则")
	}

	// 验证 Direct + Transitive Reverse Import 闭包解析出的受影响模块
	affectedMap := make(map[string]bool)
	for _, a := range breakingErr.AffectedDependents {
		affectedMap[a.ModuleCode] = true
	}
	if !affectedMap["MOD_ORDER"] {
		t.Errorf("应沿反向 import 闭包穿透影响服务提供模块 MOD_ORDER，实际: %+v", breakingErr.AffectedDependents)
	}
	if !affectedMap["MOD_GATEWAY"] {
		t.Errorf("应沿反向 import 闭包穿透影响下游消费模块 MOD_GATEWAY，实际: %+v", breakingErr.AffectedDependents)
	}
	if affectedMap["MOD_UNRELATED"] {
		t.Errorf("不应波及无关模块 MOD_UNRELATED，实际: %+v", breakingErr.AffectedDependents)
	}

	// 也直接验证 FindAffectedDependents 在传入 CompiledRegistry.Files 的行为
	var crFiles *protoregistry.Files
	if cr := reg.compiled.Load(); cr != nil {
		crFiles = cr.Files
	}
	if crFiles == nil {
		// 即使未传入 crFiles，从 DB 也可反推
		descBytes, _ := proto.Marshal(&descriptorpb.FileDescriptorSet{})
		_ = descBytes
		_, _ = protodesc.NewFiles(&descriptorpb.FileDescriptorSet{})
	}
	directAffected := FindAffectedDependents(db, []BreakingRuleViolation{
		{Rule: RuleTypeIncompatible, ProtoFile: "common/base.proto", Symbol: "common.v1.CommonHeader.trace_id", ServiceName: ""},
	}, crFiles)
	directMap := make(map[string]bool)
	for _, a := range directAffected {
		directMap[a.ModuleCode] = true
	}
	if !directMap["MOD_ORDER"] || !directMap["MOD_GATEWAY"] {
		t.Errorf("直接调用 FindAffectedDependents 应穿透 BASE_LIB 闭包命中 MOD_ORDER 与 MOD_GATEWAY，实测: %+v", directAffected)
	}
}

func TestDBProtoSourceProvider_ResolvesBaseLibAndIgnoresSoftDeleted(t *testing.T) {
	db := setupIsolatedDecoderDB(t)
	reg, err := NewRegistry(db)
	if err != nil {
		t.Fatalf("NewRegistry 失败: %v", err)
	}

	// 1. 构造 GitRepo 与 3 组 Bundle（同路径分别位于 BUSINESS 与 BASE_LIB，且另一条已被软删除）
	repo := models.GitRepo{
		Name:          "proto-monorepo",
		Provider:      models.GitProviderGitLab,
		RepoURL:       "http://127.0.0.1:8080/org/monorepo.git",
		DefaultBranch: "main",
		AuthType:      models.GitAuthNone,
		SyncMode:      models.GitSyncManual,
	}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("创建 GitRepo 失败: %v", err)
	}

	bBiz := models.GitProtoBundle{
		GitRepoID:        repo.ID,
		BundleName:       "biz-copy",
		TargetSchemaName: "biz-copy-schema",
		Category:         models.ProtoCategoryBusiness,
		SubdirPath:       "biz",
	}
	bBase := models.GitProtoBundle{
		GitRepoID:        repo.ID,
		BundleName:       "base-lib",
		TargetSchemaName: "common-base-schema",
		Category:         models.ProtoCategoryBaseLib,
		SubdirPath:       "common",
	}
	bDeleted := models.GitProtoBundle{
		GitRepoID:        repo.ID,
		BundleName:       "deleted-lib",
		TargetSchemaName: "deleted-schema",
		Category:         models.ProtoCategoryBaseLib,
		SubdirPath:       "deleted",
	}
	for _, b := range []*models.GitProtoBundle{&bBiz, &bBase, &bDeleted} {
		if err := db.Create(b).Error; err != nil {
			t.Fatalf("创建 Bundle %s 失败: %v", b.BundleName, err)
		}
	}

	insertProtoFile := func(b models.GitProtoBundle, relPath, content string, deleted bool) {
		pf := models.ProtoFile{
			GitRepoID:        repo.ID,
			BundleID:         b.ID,
			TargetSchemaName: b.TargetSchemaName,
			Category:         b.Category,
			FilePath:         relPath,
			RepoFilePath:     b.SubdirPath + "/" + relPath,
			ContentSHA256:    "sha256",
			LatestVersionNum: 1,
			IsDeleted:        deleted,
		}
		if err := db.Create(&pf).Error; err != nil {
			t.Fatalf("创建 ProtoFile 失败: %v", err)
		}
		ver := models.ProtoFileVersion{
			ProtoFileID:   pf.ID,
			Version:       1,
			SourceType:    models.ProtoSourceGitSync,
			CommitSHA:     "abcdef1",
			ContentText:   content,
			ContentSHA256: "sha256",
			LineCount:     3,
		}
		if err := db.Create(&ver).Error; err != nil {
			t.Fatalf("创建 ProtoFileVersion 失败: %v", err)
		}
		if err := db.Model(&pf).Update("latest_version_id", ver.ID).Error; err != nil {
			t.Fatalf("绑定 latest_version_id 失败: %v", err)
		}
	}

	// 同路径 BUSINESS 副本（不应被选中）
	insertProtoFile(bBiz, "common/types.proto", `syntax = "proto3";
package common.v1;
message BaseMeta { string from_biz_stale = 1; }
`, false)

	// 已被软删除的 BASE_LIB 独占文件（绝不能命中）
	insertProtoFile(bDeleted, "common/deleted_only.proto", `syntax = "proto3";
package common.v1;
message DeletedOnly { string gone = 1; }
`, true)

	// 活跃 BASE_LIB 优先文件（必须命中）
	insertProtoFile(bBase, "common/types.proto", `syntax = "proto3";
package common.v1;
// BaseMeta 公共元信息
message BaseMeta {
  string trace_id = 1;
  string tenant_code = 2;
}
`, false)

	provider := NewDBProtoSourceProvider(db)
	entry, err := provider.ResolveProtoSource(context.Background(), "common/types.proto")
	if err != nil || entry == nil {
		t.Fatalf("ResolveProtoSource(common/types.proto) 失败: entry=%v err=%v", entry, err)
	}
	if entry.Category != models.ProtoCategoryBaseLib || !strings.Contains(string(entry.Content), "tenant_code") {
		t.Fatalf("未优先命中 BASE_LIB 快照: category=%s content=%s", entry.Category, string(entry.Content))
	}

	deletedEntry, err := provider.ResolveProtoSource(context.Background(), "common/deleted_only.proto")
	if err != nil {
		t.Fatalf("ResolveProtoSource 对已软删除文件不应报错: %v", err)
	}
	if deletedEntry != nil {
		t.Fatalf("已软删除的 ProtoFile (is_deleted=true) 不应被 Tier 2 返回")
	}

	// 2. 通过 Registry.CompileAndSaveSchema 验证自动接驳 DBProtoSourceProvider（无需显式传 SourceProvider）
	draftSchema, _, _, err := reg.CompileAndSaveSchema(context.Background(), CompileProtoBundleInput{
		Name:     "db-tier2-consumer",
		Category: models.ProtoCategoryBusiness,
		Files: map[string]string{
			"biz/consumer.proto": `syntax = "proto3";
package biz.v1;
import "common/types.proto";
message PingReq { common.v1.BaseMeta meta = 1; }
message PingRsp { string ok = 1; }
service EchoService { rpc Ping(PingReq) returns (PingRsp); }
`,
		},
		AutoActivate: false,
		OperatorID:   1,
	})
	if err != nil {
		t.Fatalf("CompileAndSaveSchema 默认接驳 Tier-2 DBProtoSourceProvider 编译失败: %v", err)
	}
	if draftSchema == nil || draftSchema.OperationCount != 1 {
		t.Fatalf("预期编译产生 1 个 Operation，实际: %+v", draftSchema)
	}

	// 3. 若引入被软删除的文件，则应报 MISSING_IMPORTS
	_, _, _, err = reg.CompileAndSaveSchema(context.Background(), CompileProtoBundleInput{
		Name:     "db-tier2-deleted-consumer",
		Category: models.ProtoCategoryBusiness,
		Files: map[string]string{
			"biz/bad.proto": `syntax = "proto3";
package biz.v1;
import "common/deleted_only.proto";
message BadReq { common.v1.DeletedOnly d = 1; }
`,
		},
		AutoActivate: false,
		OperatorID:   1,
	})
	var ce *CompileError
	if !errors.As(err, &ce) || ce.Code != "MISSING_IMPORTS" {
		t.Fatalf("引入已软删除文件应返回 MISSING_IMPORTS，实测: %v", err)
	}
}
