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

func TestAll13ModelsTablePrefixAndMigration(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, config.ServiceName)

	if err := SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}

	modelsList := []tabler{
		Subsystem{},
		FunctionalGroup{},
		Module{},
		ModuleInterface{},
		Agent{},
		Process{},
		ProcessModule{},
		ProtoSchema{},
		ProtoOperation{},
		RecordingSession{},
		TrafficRecord{},
		ReplayTask{},
		ReplayItem{},
		TraefikSetting{},
	}

	if len(modelsList) != 14 {
		t.Fatalf("模型清单数量期望为 14 (13 张业务模型 + TraefikSetting)，实际为: %d", len(modelsList))
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

	// 1. 验证 DEPEND 相同接口允许共存 vs PROVIDE 相同接口全局拦截
	dep1 := ModuleInterface{
		ModuleID:     mod1.ID,
		RelationType: InterfaceRelationDepend,
		ServiceName:  "pkg.order-OrderService",
		Operation:    "GetOrder",
	}
	if err := db.Create(&dep1).Error; err != nil {
		t.Fatalf("Module1 声明 DEPEND 失败: %v", err)
	}

	dep2 := ModuleInterface{
		ModuleID:     mod2.ID,
		RelationType: InterfaceRelationDepend,
		ServiceName:  "pkg.order-OrderService",
		Operation:    "GetOrder",
	}
	if err := db.Create(&dep2).Error; err != nil {
		t.Fatalf("Module2 声明相同 DEPEND 应当成功，但报错: %v", err)
	}

	prov1 := ModuleInterface{
		ModuleID:     mod1.ID,
		RelationType: InterfaceRelationProvide,
		ServiceName:  "pkg.order-OrderService",
		Operation:    "CreateOrder",
	}
	if err := db.Create(&prov1).Error; err != nil {
		t.Fatalf("Module1 声明 PROVIDE 失败: %v", err)
	}

	prov2 := ModuleInterface{
		ModuleID:     mod2.ID,
		RelationType: InterfaceRelationProvide,
		ServiceName:  "pkg.order-OrderService",
		Operation:    "CreateOrder",
	}
	// 期望被 uk_traefik_provide_service_op 部分唯一索引拦截
	errProv2 := db.Create(&prov2).Error
	if errProv2 == nil {
		t.Fatalf("期望不同模块声明相同 PROVIDE 接口被 uk_traefik_provide_service_op 拦截，但成功插入了！")
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
		ServiceName:        "order.v1-OrderService",
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
		ServiceName:        "order.v1-OrderService", // 相同 ServiceName
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
