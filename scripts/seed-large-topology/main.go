package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"code-traefik/internal/server/topology"
	"code-traefik/models"

	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func extractDBName(dsn string) string {
	trimmed := strings.TrimSpace(dsn)
	if strings.HasPrefix(trimmed, "postgres://") || strings.HasPrefix(trimmed, "postgresql://") {
		u, err := url.Parse(trimmed)
		if err == nil {
			return strings.TrimPrefix(u.Path, "/")
		}
	}
	for _, part := range strings.Fields(trimmed) {
		if strings.HasPrefix(part, "dbname=") {
			return strings.TrimPrefix(part, "dbname=")
		}
	}
	return ""
}

func main() {
	dsn := flag.String("dsn", "", "PostgreSQL DSN (库名必须以 _test 或 _dev 结尾)")
	subsystemsCount := flag.Int("subsystems", 25, "子系统数量")
	groupsPerSub := flag.Int("groups", 10, "每个子系统的功能组数量")
	modulesPerGrp := flag.Int("modules", 10, "每个功能组的功能模块数量")
	hubCallers := flag.Int("hub-callers", 12, "依赖公共底座 sub_1 的不同调用方子系统数")
	withViolationSession := flag.Bool("with-violation-session", false, "是否写入含跨子系统违规调用的录制批次")
	flag.Parse()

	if *dsn == "" {
		log.Fatalf("必须指定 -dsn 参数")
	}

	dbName := extractDBName(*dsn)
	if !strings.HasSuffix(dbName, "_test") && !strings.HasSuffix(dbName, "_dev") {
		log.Fatalf("安全熔断拦截：拒绝向非测试/开发库写入大规模种子数据 (当前库名=%q，仅允许 *_test 或 *_dev)", dbName)
	}

	// 预初始化公共 departments 与 users 表（打破 Department <-> User 循环外键冷启动）
	bootstrapDB, err := gorm.Open(postgres.Open(*dsn), &gorm.Config{
		Logger:                                   logger.Default.LogMode(logger.Silent),
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		log.Fatalf("初始化基础连接失败: %v", err)
	}
	if err := bootstrapDB.AutoMigrate(&models.Department{}, &models.User{}); err != nil {
		log.Fatalf("预创建公共用户部门表失败: %v", err)
	}

	db, err := gorm.Open(postgres.Open(*dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}

	if err := models.SetupWithDB(db); err != nil {
		log.Fatalf("初始化表结构失败: %v", err)
	}

	// 清理目标测试/开发库既有架构与流量数据
	cleanupSQL := `
TRUNCATE TABLE
	traefik_topology_layouts,
	traefik_module_dependencies,
	traefik_module_services,
	traefik_modules,
	traefik_functional_groups,
	traefik_subsystems,
	traefik_arch_processes,
	traefik_arch_process_groups,
	traefik_traffic_records,
	traefik_recording_sessions
RESTART IDENTITY CASCADE;`
	if err := db.Exec(cleanupSQL).Error; err != nil {
		log.Fatalf("清理既有数据失败: %v", err)
	}

	var adminUser models.User
	if err := db.Where("username = ?", "admin").First(&adminUser).Error; err != nil {
		adminUser.ID = 1
	}
	ownerID := adminUser.ID

	// 1. 创建物理进程组与标准进程蓝图（便于物理视图同样可测）
	pgrps := make([]models.ArchProcessGroup, 0, 5)
	for i := 1; i <= 5; i++ {
		pgrps = append(pgrps, models.ArchProcessGroup{
			Code:        fmt.Sprintf("PGRP_%02d", i),
			Name:        fmt.Sprintf("核心集群进程域-%02d", i),
			SortOrder:   i,
			Description: "大规模拓扑验证进程组",
			CreatedBy:   ownerID,
			UpdatedBy:   ownerID,
		})
	}
	if err := db.CreateInBatches(&pgrps, 100).Error; err != nil {
		log.Fatalf("写入进程组失败: %v", err)
	}

	procs := make([]models.ArchProcess, 0, *subsystemsCount)
	for s := 1; s <= *subsystemsCount; s++ {
		pgrp := pgrps[(s-1)%len(pgrps)]
		procs = append(procs, models.ArchProcess{
			GroupID:                 pgrp.ID,
			Code:                    fmt.Sprintf("sut-sub-%02d", s),
			Name:                    fmt.Sprintf("标准服务进程-%02d", s),
			DefaultListenPort:       18000 + s,
			DefaultCmdPattern:       fmt.Sprintf("sut-sub-%02d", s),
			DefaultExtraDownstreams: datatypes.JSON([]byte("[]")),
			SortOrder:               s,
			CreatedBy:               ownerID,
			UpdatedBy:               ownerID,
		})
	}
	if err := db.CreateInBatches(&procs, 100).Error; err != nil {
		log.Fatalf("写入标准进程失败: %v", err)
	}

	// 2. 创建子系统
	subsystems := make([]models.Subsystem, 0, *subsystemsCount)
	for s := 1; s <= *subsystemsCount; s++ {
		name := fmt.Sprintf("核心业务子系统-%02d", s)
		if s == 1 {
			name = "公共底座子系统-01"
		}
		uid := ownerID
		subsystems = append(subsystems, models.Subsystem{
			Code:        fmt.Sprintf("SUB_%02d", s),
			Name:        name,
			OwnerID:     &uid,
			Description: "SC-11 大规模拓扑测试子系统",
			CreatedBy:   ownerID,
			UpdatedBy:   ownerID,
		})
	}
	if err := db.CreateInBatches(&subsystems, 100).Error; err != nil {
		log.Fatalf("写入子系统失败: %v", err)
	}

	// 3. 创建功能组
	totalGroups := (*subsystemsCount) * (*groupsPerSub)
	groups := make([]models.FunctionalGroup, 0, totalGroups)
	grpSeq := 0
	for sIdx, sub := range subsystems {
		for g := 1; g <= *groupsPerSub; g++ {
			grpSeq++
			groups = append(groups, models.FunctionalGroup{
				SubsystemID: sub.ID,
				Code:        fmt.Sprintf("GRP_%02d_%02d", sIdx+1, g),
				Name:        fmt.Sprintf("功能组-%02d-%02d", sIdx+1, g),
				Level:       1,
				Path:        fmt.Sprintf("/%d/", grpSeq),
				SortOrder:   g,
				CreatedBy:   ownerID,
				UpdatedBy:   ownerID,
			})
		}
	}
	if err := db.CreateInBatches(&groups, 200).Error; err != nil {
		log.Fatalf("写入功能组失败: %v", err)
	}

	// 4. 创建功能模块与模块服务
	totalModules := totalGroups * (*modulesPerGrp)
	modulesList := make([]models.Module, 0, totalModules)
	modSeq := 0
	for gIdx, grp := range groups {
		subIdx := gIdx / (*groupsPerSub)
		procID := procs[subIdx%len(procs)].ID
		uid := ownerID
		for m := 1; m <= *modulesPerGrp; m++ {
			modSeq++
			pID := procID
			modulesList = append(modulesList, models.Module{
				SubsystemID:   grp.SubsystemID,
				GroupID:       grp.ID,
				ArchProcessID: &pID,
				Code:          fmt.Sprintf("MOD_%04d", modSeq),
				Name:          fmt.Sprintf("微服务模块-%04d", modSeq),
				OwnerID:       &uid,
				Description:   "SC-11 种子模块",
				CreatedBy:     ownerID,
				UpdatedBy:     ownerID,
			})
		}
	}
	if err := db.CreateInBatches(&modulesList, 500).Error; err != nil {
		log.Fatalf("写入功能模块失败: %v", err)
	}

	servicesList := make([]models.ModuleService, 0, totalModules)
	for idx, mod := range modulesList {
		servicesList = append(servicesList, models.ModuleService{
			ModuleID:        mod.ID,
			ServiceName:     fmt.Sprintf("svc.v1.Mod%04dService", idx+1),
			DiffIgnorePaths: datatypes.JSON([]byte("[]")),
			Description:     fmt.Sprintf("模块 %s 标准对外服务", mod.Code),
			CreatedBy:       ownerID,
		})
	}
	if err := db.CreateInBatches(&servicesList, 500).Error; err != nil {
		log.Fatalf("写入模块服务失败: %v", err)
	}

	// 5. 创建模块依赖关系（含公共底座 Hub 依赖、外部第三方系统依赖及 ~4x 模块数的网格依赖）
	targetDeps := totalModules * 4
	if targetDeps < 600 {
		targetDeps = 600
	}
	deps := make([]models.ModuleDependency, 0, targetDeps+10)
	pairSeen := make(map[string]struct{}, targetDeps+10)

	modsPerSub := (*groupsPerSub) * (*modulesPerGrp)
	baseMod := modulesList[0]
	baseSvc := servicesList[0]

	maxCallerSub := *hubCallers + 1
	if maxCallerSub > *subsystemsCount {
		maxCallerSub = *subsystemsCount
	}
	for callerSub := 2; callerSub <= maxCallerSub; callerSub++ {
		callerModIdx := (callerSub - 1) * modsPerSub
		if callerModIdx < len(modulesList) {
			callerMod := modulesList[callerModIdx]
			key := fmt.Sprintf("%d->%d", callerMod.ID, baseSvc.ID)
			pairSeen[key] = struct{}{}
			svcID := baseSvc.ID
			provID := baseMod.ID
			deps = append(deps, models.ModuleDependency{
				ConsumerModuleID: callerMod.ID,
				IsExternal:       false,
				ServiceID:        &svcID,
				ProviderModuleID: &provID,
				Description:      "调用公共底座子系统核心服务",
				CreatedBy:        ownerID,
			})
		}
	}

	// 外部第三方系统依赖
	extSystems := []struct {
		sys string
		svc string
	}{
		{"UNIONPAY", "ClearSettlementService"},
		{"SMS_GATEWAY", "BatchSmsNotifyService"},
		{"OAUTH_CENTER", "TokenIntrospectService"},
	}
	for i, ext := range extSystems {
		if i < len(modulesList) {
			deps = append(deps, models.ModuleDependency{
				ConsumerModuleID: modulesList[i].ID,
				IsExternal:       true,
				ExternalSystem:   ext.sys,
				ExternalService:  ext.svc,
				Description:      "外部系统依赖",
				CreatedBy:        ownerID,
			})
		}
	}

	stride := 1
	for len(deps) < targetDeps && stride <= totalModules {
		for srcIdx := 0; srcIdx < totalModules && len(deps) < targetDeps; srcIdx++ {
			offset := stride
			if stride%3 == 0 {
				offset = stride * 7
			}
			dstIdx := (srcIdx + offset) % totalModules
			if srcIdx == dstIdx {
				continue
			}
			srcMod := modulesList[srcIdx]
			dstMod := modulesList[dstIdx]
			dstSvc := servicesList[dstIdx]
			key := fmt.Sprintf("%d->%d", srcMod.ID, dstSvc.ID)
			if _, exists := pairSeen[key]; exists {
				continue
			}
			pairSeen[key] = struct{}{}
			svcID := dstSvc.ID
			provID := dstMod.ID
			deps = append(deps, models.ModuleDependency{
				ConsumerModuleID: srcMod.ID,
				IsExternal:       false,
				ServiceID:        &svcID,
				ProviderModuleID: &provID,
				Description:      "模块间契约调用",
				CreatedBy:        ownerID,
			})
		}
		stride++
	}

	if err := db.CreateInBatches(&deps, 500).Error; err != nil {
		log.Fatalf("写入模块依赖边失败: %v", err)
	}

	// 6. 可选创建含跨子系统 VIOLATION 调用的录制批次
	if *withViolationSession && len(modulesList) > modsPerSub+2 {
		now := time.Now()
		session := models.RecordingSession{
			SessionNo:             fmt.Sprintf("REC-SC11-%d", now.Unix()),
			Name:                  "SC-11 大规模架构一致性验证批次",
			Scope:                 "SHARED",
			OwnerID:               ownerID,
			Status:                "STOPPED",
			LeasedAgentIDs:        datatypes.JSON([]byte("[1]")),
			TargetProcessIDs:      datatypes.JSON([]byte("[1,2]")),
			ProcessSnapshots:      datatypes.JSON([]byte("[]")),
			ProcessDiagnostics:    datatypes.JSON([]byte("[]")),
			CapturedFrames:        12,
			StartedAt:             &now,
			StoppedAt:             &now,
			StopReason:            "MANUAL",
			TopologyVerifySummary: datatypes.JSON([]byte(`{"verified_count":10,"violation_count":2}`)),
			Description:           "含未声明跨子系统越权调用的验证批次",
			CreatedBy:             ownerID,
		}
		if err := db.Create(&session).Error; err != nil {
			log.Fatalf("创建录制批次失败: %v", err)
		}

		srcMod := modulesList[0]
		violTargetMod := modulesList[modsPerSub+1]
		violTargetSvc := servicesList[modsPerSub+1]
		srcModID := srcMod.ID
		dstModID := violTargetMod.ID
		subID := srcMod.SubsystemID
		lat := int64(1250)

		tr := models.TrafficRecord{
			CreatedAt:          now,
			SessionID:          session.ID,
			SeqNo:              1,
			AgentID:            1,
			ProcessID:          1,
			CapturedPID:        10001,
			ConnKey:            "127.0.0.1:45000->127.0.0.1:18002",
			Direction:          "INBOUND",
			ExchangeType:       "UNARY",
			SrcAddr:            "127.0.0.1:45000",
			DstAddr:            "127.0.0.1:18002",
			ReqCapturedAtNs:    now.UnixNano(),
			LatencyUs:          &lat,
			TransportProto:     1,
			SNO:                models.U64Numeric(1001),
			ServiceName:        violTargetSvc.ServiceName,
			Operation:          "ExecuteIllegalCrossSubsystem",
			StatusCode:         models.U64Numeric(0),
			TraceID:            "sc11trace00000001",
			SpanID:             "sc11span0001",
			SubsystemID:        &subID,
			SrcModuleID:        &srcModID,
			SrcAttributionMode: "SINGLE_MODULE_PROCESS",
			DstModuleID:        &dstModID,
			HeadersJSON:        datatypes.JSON([]byte("{}")),
			DecodeStatus:       "DECODED_OK",
			PayloadJSON:        datatypes.JSON([]byte(`{"sc11":true}`)),
			IsReplayable:       true,
		}
		if err := db.Create(&tr).Error; err != nil {
			log.Fatalf("写入验证流量记录失败: %v", err)
		}
	}

	totalNodes := len(subsystems) + len(groups) + len(modulesList) + len(extSystems) + 1
	graphDTO, err := topology.BuildTopologyGraph(db, "LOGICAL", 0)
	rawBytes := 0
	gzipBytes := 0
	if err == nil {
		b, mErr := json.Marshal(map[string]any{"code": 200, "data": graphDTO})
		if mErr == nil {
			rawBytes = len(b)
			var buf bytes.Buffer
			gw := gzip.NewWriter(&buf)
			if _, wErr := gw.Write(b); wErr == nil {
				if cErr := gw.Close(); cErr == nil {
					gzipBytes = buf.Len()
				}
			}
		}
	}

	fmt.Printf(
		"SC-11 种子数据写入完成: db=%s, 子系统=%d, 功能组=%d, 模块=%d (图节点总计=%d), 依赖边=%d, Hub调用方子系统=%d, 违规录制批次=%v, GraphPayload=%dB (gzip=%dB)\n",
		dbName,
		len(subsystems),
		len(groups),
		len(modulesList),
		totalNodes,
		len(deps),
		maxCallerSub-1,
		*withViolationSession,
		rawBytes,
		gzipBytes,
	)
}
