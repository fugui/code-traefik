package models

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-traefik/config"
	"gorm.io/datatypes"
)

type tabler interface {
	TableName() string
}

func TestAll14ModelsTablePrefixAndMigration(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, config.ServiceName)

	if err := SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}

	modelsList := []tabler{
		Subsystem{},
		FunctionalGroup{},
		ArchProcessGroup{},
		ArchProcess{},
		Module{},
		ModuleService{},
		ModuleDependency{},
		Agent{},
		Process{},
		ProtoSchema{},
		ProtoOperation{},
		RecordingSession{},
		TrafficRecord{},
		ReplayTask{},
		ReplayItem{},
		TraefikSetting{},
	}

	if len(modelsList) != 16 {
		t.Fatalf("模型清单数量期望为 16 (15 张业务模型 + TraefikSetting)，实际为: %d", len(modelsList))
	}

	for _, m := range modelsList {
		tableName := m.TableName()
		typeName := reflect.TypeOf(m).Name()

		if !strings.HasPrefix(tableName, config.TablePrefix) {
			t.Fatalf("模型 [%s] 的表名 [%s] 未以模块专属前缀 %q 开头", typeName, tableName, config.TablePrefix)
		}

		if !db.Migrator().HasTable(tableName) {
			t.Fatalf("数据库中未能检测到已迁移的表 [%s] (模型 %s)", tableName, typeName)
		}
	}

	// 确认旧模板脚手架表 traefik_items 不存在
	if db.Migrator().HasTable("traefik_items") {
		t.Fatalf("检测到旧脚手架模板表 traefik_items 仍残留存在！")
	}
}

func TestU64NumericMaxUint64RoundTrip(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}

	now := time.Now().UTC()
	pm := &PartitionManager{}
	if _, err := pm.EnsureDailyPartition(db, now); err != nil {
		t.Fatalf("EnsureDailyPartition 失败: %v", err)
	}

	maxU64 := uint64(math.MaxUint64) // 18446744073709551615

	record := TrafficRecord{
		CreatedAt:        now,
		SessionID:        1001,
		SeqNo:            1,
		AgentID:          2001,
		ProcessID:        3001,
		CapturedPID:      1234,
		ConnKey:          "192.168.56.18:8091-192.168.56.19:9092",
		Direction:        TrafficDirIngress,
		ExchangeType:     ExchangeUnary,
		SrcAddr:          "192.168.56.18:8091",
		DstAddr:          "192.168.56.19:9092",
		ReqCapturedAtNs:  now.UnixNano(),
		TransportProto:   1,
		SNO:              U64Numeric(maxU64),
		StatusCode:       U64Numeric(maxU64),
		ServiceName:      "pkg-Service",
		Operation:        "OpMaxUint64",
		HeadersJSON:      datatypes.JSON([]byte(`{}`)),
		PayloadJSON:      datatypes.JSON([]byte(`{"test":"max_u64"}`)),
		ReqPayloadSHA256: "abc",
		DecodeStatus:     "SUCCESS",
	}

	if err := db.Create(&record).Error; err != nil {
		t.Fatalf("插入包含 MaxUint64 的 TrafficRecord 失败: %v", err)
	}

	var fetched TrafficRecord
	if err := db.Where("id = ? AND created_at = ?", record.ID, now).First(&fetched).Error; err != nil {
		t.Fatalf("查询已插入的 TrafficRecord 失败: %v", err)
	}

	if fetched.SNO.Uint64() != maxU64 {
		t.Fatalf("SNO 回读不一致: 期望 %d, 实际得到 %d", maxU64, fetched.SNO.Uint64())
	}
	if fetched.StatusCode.Uint64() != maxU64 {
		t.Fatalf("StatusCode 回读不一致: 期望 %d, 实际得到 %d", maxU64, fetched.StatusCode.Uint64())
	}

	// 验证 JSON 序列化无精度丢失为字符串
	jsonBytes, err := json.Marshal(fetched)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}

	var parsedMap map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &parsedMap); err != nil {
		t.Fatalf("反序列化 JSON 验证结构失败: %v", err)
	}

	expectedStr := "18446744073709551615"
	if parsedMap["sno"] != expectedStr {
		t.Fatalf("JSON sno 字段精度丢失: 期望 %q, 实际得到 %v", expectedStr, parsedMap["sno"])
	}
	if parsedMap["status_code"] != expectedStr {
		t.Fatalf("JSON status_code 字段精度丢失: 期望 %q, 实际得到 %v", expectedStr, parsedMap["status_code"])
	}
}

func TestPartialUniqueIndexesAndFKConstraints(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}

	// 准备三级架构基线数据
	sub := Subsystem{
		Code: "SUB_TEST",
		Name: "测试子系统",
	}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatalf("创建 Subsystem 失败: %v", err)
	}

	fg := FunctionalGroup{
		SubsystemID: sub.ID,
		Code:        "FG_TEST",
		Name:        "测试功能组",
		Level:       1,
		Path:        "/1/",
	}
	if err := db.Create(&fg).Error; err != nil {
		t.Fatalf("创建 FunctionalGroup 失败: %v", err)
	}

	mod1 := Module{
		SubsystemID: sub.ID,
		GroupID:     fg.ID,
		Code:        "MOD_TEST_1",
		Name:        "测试模块1",
	}
	if err := db.Create(&mod1).Error; err != nil {
		t.Fatalf("创建 Module1 失败: %v", err)
	}

	mod2 := Module{
		SubsystemID: sub.ID,
		GroupID:     fg.ID,
		Code:        "MOD_TEST_2",
		Name:        "测试模块2",
	}
	if err := db.Create(&mod2).Error; err != nil {
		t.Fatalf("创建 Module2 失败: %v", err)
	}

	// 1. 验证 ModuleService PROVIDE 相同 Service 全局唯一性拦截，以及 ModuleDependency 内部/外部依赖与唯一约束
	prov1 := ModuleService{
		ModuleID:    mod1.ID,
		ServiceName: "pkg.order.OrderService",
	}
	if err := db.Create(&prov1).Error; err != nil {
		t.Fatalf("Module1 声明 PROVIDE Service 失败: %v", err)
	}

	prov2 := ModuleService{
		ModuleID:    mod2.ID,
		ServiceName: "pkg.order.OrderService",
	}
	// 期望被 uk_traefik_mod_svc_name 唯一索引拦截
	errProv2 := db.Create(&prov2).Error
	if errProv2 == nil {
		t.Fatalf("期望不同模块声明相同 PROVIDE Service 被 uk_traefik_mod_svc_name 拦截，但成功插入了！")
	}

	depInternal := ModuleDependency{
		ConsumerModuleID: mod2.ID,
		ServiceID:        &prov1.ID,
		ProviderModuleID: &mod1.ID,
	}
	if err := db.Create(&depInternal).Error; err != nil {
		t.Fatalf("Module2 声明依赖 Module1 的 Service 失败: %v", err)
	}

	// 尝试重复依赖相同 service 期望拦截
	depInternalDup := ModuleDependency{
		ConsumerModuleID: mod2.ID,
		ServiceID:        &prov1.ID,
		ProviderModuleID: &mod1.ID,
	}
	if err := db.Create(&depInternalDup).Error; err == nil {
		t.Fatalf("期望重复依赖相同 Service 被 uk_traefik_dep_tuple 拦截，但成功插入了！")
	}

	// 验证外部系统依赖支持
	depExternal := ModuleDependency{
		ConsumerModuleID: mod1.ID,
		IsExternal:       true,
		ExternalSystem:   "WeChatPay",
		ExternalService:  "UnifiedOrder",
	}
	if err := db.Create(&depExternal).Error; err != nil {
		t.Fatalf("Module1 声明依赖外部三方系统失败: %v", err)
	}

	// 2. 验证不同 ProtoSchema 激活相同 (ServiceName, Operation) 被 uk_traefik_active_proto_op 拦截
	schema1 := ProtoSchema{
		Name:            "order_proto_v1",
		Version:         1,
		FileName:        "order.pb",
		FileSHA256:      "sha256_v1",
		FileSize:        1024,
		DescriptorBytes: []byte("mock-descriptor"),
		IsActive:        true,
	}
	if err := db.Create(&schema1).Error; err != nil {
		t.Fatalf("创建 Schema1 失败: %v", err)
	}

	op1 := ProtoOperation{
		SchemaID:           schema1.ID,
		ProtoFile:          "order.proto",
		ProtoPackage:       "order.v1",
		ProtoService:       "OrderService",
		ServiceName:        "order.v1.OrderService",
		Operation:          "SubmitOrder",
		ReqMessageFullName: "order.v1.SubmitReq",
		RspMessageFullName: "order.v1.SubmitRsp",
		IsActive:           true,
	}
	if err := db.Create(&op1).Error; err != nil {
		t.Fatalf("创建 Operation1 失败: %v", err)
	}

	schema2 := ProtoSchema{
		Name:            "order_proto_v2",
		Version:         1,
		FileName:        "order2.pb",
		FileSHA256:      "sha256_v2",
		FileSize:        1024,
		DescriptorBytes: []byte("mock-descriptor-2"),
		IsActive:        true,
	}
	if err := db.Create(&schema2).Error; err != nil {
		t.Fatalf("创建 Schema2 失败: %v", err)
	}

	op2 := ProtoOperation{
		SchemaID:           schema2.ID,
		ProtoFile:          "order2.proto",
		ProtoPackage:       "order.v2",
		ProtoService:       "OrderService",
		ServiceName:        "order.v1.OrderService", // 相同 ServiceName
		Operation:          "SubmitOrder",           // 相同 Operation
		ReqMessageFullName: "order.v2.SubmitReq",
		RspMessageFullName: "order.v2.SubmitRsp",
		IsActive:           true, // 同时激活
	}
	errOp2 := db.Create(&op2).Error
	if errOp2 == nil {
		t.Fatalf("期望两个生效 Schema 激活相同接口被 uk_traefik_active_proto_op 拦截，但成功插入了！")
	}

	// 3. 验证删除仍挂载 FunctionalGroup 的 Subsystem 被 ON DELETE RESTRICT 拦截
	errDelSub := db.Delete(&sub).Error
	if errDelSub == nil {
		t.Fatalf("期望删除仍挂载 FunctionalGroup 的 Subsystem 被 ON DELETE RESTRICT 拦截，但成功删除了！")
	}
}

func TestTrafficRecordsRangePartitionAndDropPurge(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}

	pm := &PartitionManager{}
	now := time.Now().UTC()

	// 1. 预建未来 7 天分区与 35 天前过期分区
	futureDate := now.AddDate(0, 0, 7)
	futureTable, err := pm.EnsureDailyPartition(db, futureDate)
	if err != nil {
		t.Fatalf("预建未来分区失败: %v", err)
	}

	oldDate := now.AddDate(0, 0, -35)
	oldTable, err := pm.EnsureDailyPartition(db, oldDate)
	if err != nil {
		t.Fatalf("预建过期分区失败: %v", err)
	}

	// 2. 检查 pg_inherits 确认已挂接为子分区
	var count int64
	checkInheritsSQL := `
SELECT COUNT(*)
FROM pg_inherits i
JOIN pg_class c ON c.oid = i.inhrelid
JOIN pg_class p ON p.oid = i.inhparent
WHERE p.relname = 'traefik_traffic_records' AND c.relname = ?;
`
	if err := db.Raw(checkInheritsSQL, oldTable).Scan(&count).Error; err != nil {
		t.Fatalf("查询 pg_inherits 失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("未能确认过期子分区 [%s] 挂接至母表", oldTable)
	}

	// 3. 往过期分区写入 10 条记录，往未来分区写入 10 条记录
	for i := 0; i < 10; i++ {
		recOld := TrafficRecord{
			CreatedAt:    oldDate.Add(time.Duration(i) * time.Minute),
			SessionID:    99,
			SeqNo:        int64(i + 1),
			AgentID:      1,
			ProcessID:    1,
			CapturedPID:  1000,
			ConnKey:      "1.1.1.1:1-2.2.2.2:2",
			Direction:    TrafficDirIngress,
			ExchangeType: ExchangeUnary,
			SrcAddr:      "1.1.1.1:1",
			DstAddr:      "2.2.2.2:2",
			HeadersJSON:  datatypes.JSON([]byte(`{}`)),
			PayloadJSON:  datatypes.JSON([]byte(`{}`)),
			DecodeStatus: "SUCCESS",
		}
		if err := db.Create(&recOld).Error; err != nil {
			t.Fatalf("写入过期分区数据失败: %v", err)
		}

		recFuture := TrafficRecord{
			CreatedAt:    futureDate.Add(time.Duration(i) * time.Minute),
			SessionID:    100,
			SeqNo:        int64(i + 1),
			AgentID:      1,
			ProcessID:    1,
			CapturedPID:  1000,
			ConnKey:      "1.1.1.1:1-2.2.2.2:2",
			Direction:    TrafficDirIngress,
			ExchangeType: ExchangeUnary,
			SrcAddr:      "1.1.1.1:1",
			DstAddr:      "2.2.2.2:2",
			HeadersJSON:  datatypes.JSON([]byte(`{}`)),
			PayloadJSON:  datatypes.JSON([]byte(`{}`)),
			DecodeStatus: "SUCCESS",
		}
		if err := db.Create(&recFuture).Error; err != nil {
			t.Fatalf("写入未来分区数据失败: %v", err)
		}
	}

	// 4. 执行 30 天 TTL 清理
	dropped, err := pm.PurgeExpiredPartitions(db, 30, now)
	if err != nil {
		t.Fatalf("PurgeExpiredPartitions 失败: %v", err)
	}

	// 确认 35 天前分区被 DROP
	foundOld := false
	for _, name := range dropped {
		if name == oldTable {
			foundOld = true
			break
		}
	}
	if !foundOld {
		t.Fatalf("期望 DROP TABLE 包含过期分区 [%s]，实际删除列表: %v", oldTable, dropped)
	}

	// 确认旧表已被物理删除
	if db.Migrator().HasTable(oldTable) {
		t.Fatalf("过期分区表 [%s] 应当已被物理销毁，但仍存在", oldTable)
	}

	// 确认未来分区完好无损，且 10 条数据依然可查
	if !db.Migrator().HasTable(futureTable) {
		t.Fatalf("未来分区表 [%s] 被误删！", futureTable)
	}
	var futureCount int64
	if err := db.Model(&TrafficRecord{}).Where("session_id = ?", 100).Count(&futureCount).Error; err != nil {
		t.Fatalf("查询未来分区记录失败: %v", err)
	}
	if futureCount != 10 {
		t.Fatalf("未来分区记录数期望为 10，实际为 %d", futureCount)
	}
}

func TestCleanRebuild15Tables_MultiSharedEnvAndPortPartialUnique(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, config.ServiceName)

	// 1. 模拟遗留旧 traefik_process_modules 表
	if err := db.Exec(`CREATE TABLE traefik_process_modules (id serial primary key, process_id bigint, module_id bigint)`).Error; err != nil {
		t.Fatalf("模拟创建旧表失败: %v", err)
	}

	// 2. 执行 SetupWithDB，触发清库直接重建
	if err := SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}

	// 验证旧表已被干净清理且 15 张核心业务表重建
	if db.Migrator().HasTable("traefik_process_modules") {
		t.Fatalf("旧表 traefik_process_modules 应当已被清理销毁，但依然存在！")
	}

	coreTables := []string{
		"traefik_subsystems",
		"traefik_functional_groups",
		"traefik_arch_process_groups",
		"traefik_arch_processes",
		"traefik_modules",
		"traefik_module_services",
		"traefik_module_dependencies",
		"traefik_agents",
		"traefik_processes",
		"traefik_proto_schemas",
		"traefik_proto_operations",
		"traefik_recording_sessions",
		"traefik_traffic_records",
		"traefik_replay_tasks",
		"traefik_replay_items",
	}
	for _, tbl := range coreTables {
		if !db.Migrator().HasTable(tbl) {
			t.Fatalf("核心表 [%s] 未成功创建", tbl)
		}
	}

	// 验证 TraefikSetting 默认预挂策略已初始化
	var sShared, sPrivate TraefikSetting
	if err := db.Where("setting_key = ?", "default_probe_desired_state_shared").First(&sShared).Error; err != nil || sShared.SettingVal != "ARMED" {
		t.Fatalf("默认预挂策略 default_probe_desired_state_shared 未初始化为 ARMED: %v, val: %s", err, sShared.SettingVal)
	}
	if err := db.Where("setting_key = ?", "default_probe_desired_state_private").First(&sPrivate).Error; err != nil || sPrivate.SettingVal != "ARMED" {
		t.Fatalf("默认预挂策略 default_probe_desired_state_private 未初始化为 ARMED: %v, val: %s", err, sPrivate.SettingVal)
	}

	// 3. 创建标准架构进程组与标准业务进程 P1(:18080), P2(:0), P3(:0)
	grp := ArchProcessGroup{
		Code: "GRP_CORE",
		Name: "核心服务组",
	}
	if err := db.Create(&grp).Error; err != nil {
		t.Fatalf("创建 ArchProcessGroup 失败: %v", err)
	}

	p1 := ArchProcess{
		GroupID:           grp.ID,
		Code:              "order-srv",
		Name:              "订单服务",
		DefaultListenPort: 18080,
	}
	p2 := ArchProcess{
		GroupID:           grp.ID,
		Code:              "client-a",
		Name:              "纯客户端A",
		DefaultListenPort: 0,
		DefaultCmdPattern: "./bin/client-a",
	}
	p3 := ArchProcess{
		GroupID:           grp.ID,
		Code:              "client-b",
		Name:              "纯客户端B",
		DefaultListenPort: 0,
		DefaultCmdPattern: "./bin/client-b",
	}
	if err := db.Create(&p1).Error; err != nil {
		t.Fatalf("创建 P1 失败: %v", err)
	}
	if err := db.Create(&p2).Error; err != nil {
		t.Fatalf("创建 P2 失败: %v", err)
	}
	if err := db.Create(&p3).Error; err != nil {
		t.Fatalf("创建 P3 失败: %v", err)
	}

	// 4. 创建两套公共测试环境 Agent-SIT 与 Agent-UAT
	agentSIT := Agent{
		Name:     "Agent-SIT",
		Endpoint: "192.168.1.101:8090",
		Scope:    ScopeShared,
		Status:   "ONLINE",
	}
	agentUAT := Agent{
		Name:     "Agent-UAT",
		Endpoint: "192.168.1.102:8090",
		Scope:    ScopeShared,
		Status:   "ONLINE",
	}
	if err := db.Create(&agentSIT).Error; err != nil {
		t.Fatalf("创建 Agent-SIT 失败: %v", err)
	}
	if err := db.Create(&agentUAT).Error; err != nil {
		t.Fatalf("创建 Agent-UAT 失败: %v", err)
	}

	// 5. 验证多套 SHARED 环境各自 1:1 挂载同一 ArchProcess P1 全部成功
	mountSIT1 := Process{
		AgentID:             agentSIT.ID,
		ArchProcessID:       p1.ID,
		EffectiveListenPort: 18080,
		ProbeDesiredState:   "ARMED",
	}
	if err := db.Create(&mountSIT1).Error; err != nil {
		t.Fatalf("Agent-SIT 挂载 P1 失败: %v", err)
	}

	mountUAT1 := Process{
		AgentID:             agentUAT.ID,
		ArchProcessID:       p1.ID,
		EffectiveListenPort: 18080,
		ProbeDesiredState:   "ARMED",
	}
	if err := db.Create(&mountUAT1).Error; err != nil {
		t.Fatalf("Agent-UAT 挂载 P1 失败（多套公共环境 1:1 挂载冲突）: %v", err)
	}

	// 6. 同一 Agent-SIT 重复挂载 P1 触发 uk_traefik_proc_agent_arch 唯一约束拦截
	mountSIT1Dup := Process{
		AgentID:             agentSIT.ID,
		ArchProcessID:       p1.ID,
		EffectiveListenPort: 18080,
		ProbeDesiredState:   "ARMED",
	}
	if err := db.Create(&mountSIT1Dup).Error; err == nil {
		t.Fatalf("同一 Agent 重复挂载同一 ArchProcess 期望被 uk_traefik_proc_agent_arch 拦截，但成功插入了！")
	}

	// 7. 同一 Agent 同时挂载 P2(port=0) 与 P3(port=0) 成功通过（允许多个纯客户端进程并存）
	mountSIT2 := Process{
		AgentID:             agentSIT.ID,
		ArchProcessID:       p2.ID,
		EffectiveListenPort: 0,
		ProbeDesiredState:   "ARMED",
	}
	if err := db.Create(&mountSIT2).Error; err != nil {
		t.Fatalf("Agent-SIT 挂载纯客户端 P2 失败: %v", err)
	}

	mountSIT3 := Process{
		AgentID:             agentSIT.ID,
		ArchProcessID:       p3.ID,
		EffectiveListenPort: 0,
		ProbeDesiredState:   "ARMED",
	}
	if err := db.Create(&mountSIT3).Error; err != nil {
		t.Fatalf("Agent-SIT 挂载纯客户端 P3 失败: %v", err)
	}

	// 8. 将 P2 的有效端口修改为 18080 与同机 P1 冲突，触发 uk_traefik_proc_agent_port 部分唯一索引拦截
	errConflict := db.Model(&mountSIT2).Update("effective_listen_port", 18080).Error
	if errConflict == nil {
		t.Fatalf("同一 Agent 端口冲突修改期望被 uk_traefik_proc_agent_port 拦截，但成功执行了！")
	}
}

func TestProtoSchemaConstraints_16MBSize_CategoryEnum_AndBackfillCounts(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := SetupWithDB(db); err != nil {
		t.Fatalf("初次 SetupWithDB 失败: %v", err)
	}

	// 模拟存量库状态：设置旧版 4MB CHECK 约束，并插入未回填计数的存量行
	_ = db.Exec("ALTER TABLE traefik_proto_schemas DROP CONSTRAINT IF EXISTS chk_traefik_proto_size").Error
	_ = db.Exec("ALTER TABLE traefik_proto_schemas ADD CONSTRAINT chk_traefik_proto_size CHECK (file_size > 0 AND file_size <= 4194304)").Error
	_ = db.Exec("ALTER TABLE traefik_proto_schemas DROP CONSTRAINT IF EXISTS chk_traefik_proto_category").Error

	legacySchema := ProtoSchema{
		Name:            "legacy_proto_schema",
		Version:         1,
		FileName:        "legacy.pb",
		FileSHA256:      "sha_legacy_test",
		FileSize:        1024,
		DescriptorBytes: []byte{0x08, 0x01},
		ProtoFiles:      datatypes.JSON(`["common/base.proto", "common/header.proto"]`),
		ProtoFileCount:  0,
		ServiceCount:    0,
		Category:        "BUSINESS",
	}
	if err := db.Create(&legacySchema).Error; err != nil {
		t.Fatalf("插入存量描述集记录失败: %v", err)
	}

	opLegacy := ProtoOperation{
		SchemaID:           legacySchema.ID,
		ProtoFile:          "common/header.proto",
		ProtoPackage:       "common",
		ProtoService:       "HeaderService",
		ServiceName:        "common.HeaderService",
		Operation:          "GetHeader",
		ReqMessageFullName: "common.HeaderReq",
		RspMessageFullName: "common.HeaderRsp",
	}
	if err := db.Create(&opLegacy).Error; err != nil {
		t.Fatalf("插入存量服务方法失败: %v", err)
	}

	// 执行升级迁移 SetupWithDB
	if err := SetupWithDB(db); err != nil {
		t.Fatalf("升级执行 SetupWithDB 失败: %v", err)
	}

	// 1. 验证存量行 proto_file_count 与 service_count 被幂等准确回填
	var updatedLegacy ProtoSchema
	if err := db.First(&updatedLegacy, legacySchema.ID).Error; err != nil {
		t.Fatalf("查询更新后存量描述集失败: %v", err)
	}
	if updatedLegacy.ProtoFileCount != 2 {
		t.Fatalf("存量 proto_file_count 回填期望为 2，实际为: %d", updatedLegacy.ProtoFileCount)
	}
	if updatedLegacy.ServiceCount != 1 {
		t.Fatalf("存量 service_count 回填期望为 1，实际为: %d", updatedLegacy.ServiceCount)
	}

	// 2. 验证 8MB (8,388,608 bytes) BASE_LIB 描述集成功落库（旧 4MB 约束已成功升级为 16MB）
	schema8MB := ProtoSchema{
		Name:            "base_lib_8mb",
		Version:         1,
		FileName:        "base_lib_8mb.pb",
		FileSHA256:      "sha_8mb_test",
		FileSize:        8 * 1024 * 1024,
		DescriptorBytes: []byte{0x08, 0x02},
		Category:        ProtoCategoryBaseLib,
		ProtoFiles:      datatypes.JSON(`["base.proto"]`),
		ProtoFileCount:  1,
		ServiceCount:    0,
	}
	if err := db.Create(&schema8MB).Error; err != nil {
		t.Fatalf("插入 8MB BASE_LIB 描述集失败（旧 4MB 约束未能自动升级）: %v", err)
	}

	// 3. 验证 > 16MB (16,777,217 bytes) 触发 chk_traefik_proto_size 拦截
	schemaTooLarge := ProtoSchema{
		Name:            "too_large_pb",
		Version:         1,
		FileName:        "large.pb",
		FileSHA256:      "sha_large_test",
		FileSize:        16777217,
		DescriptorBytes: []byte{0x08, 0x03},
		Category:        ProtoCategoryBusiness,
	}
	if err := db.Create(&schemaTooLarge).Error; err == nil {
		t.Fatalf("插入 > 16MB 记录期望被 chk_traefik_proto_size 拦截，但成功插入了！")
	}

	// 4. 验证非法 category = 'UNKNOWN' 触发 chk_traefik_proto_category 拦截
	schemaBadCategory := ProtoSchema{
		Name:            "bad_category_pb",
		Version:         1,
		FileName:        "bad.pb",
		FileSHA256:      "sha_bad_test",
		FileSize:        1024,
		DescriptorBytes: []byte{0x08, 0x04},
		Category:        "UNKNOWN",
	}
	if err := db.Create(&schemaBadCategory).Error; err == nil {
		t.Fatalf("插入非法 category='UNKNOWN' 期望被 chk_traefik_proto_category 拦截，但成功插入了！")
	}
}
