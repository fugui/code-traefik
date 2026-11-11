package topology

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"code-traefik/models"
	"gorm.io/gorm"
)

// ExclusiveServiceInfo 独占服务元信息
type ExclusiveServiceInfo struct {
	ServiceID     uint64  `json:"service_id"`
	ModuleID      uint64  `json:"module_id"`
	ArchProcessID *uint64 `json:"arch_process_id"`
}

// ContractImplInfo 多实现契约的单模块实现元信息
type ContractImplInfo struct {
	ServiceID     uint64  `json:"service_id"`
	ModuleID      uint64  `json:"module_id"`
	ArchProcessID *uint64 `json:"arch_process_id"`
	ImplKey       string  `json:"impl_key"`
}

// MultiImplContractInfo 多实现契约元信息
type MultiImplContractInfo struct {
	ContractID      uint64             `json:"contract_id"`
	ServiceName     string             `json:"service_name"`
	DefinerModuleID uint64             `json:"definer_module_id"`
	Implementations []ContractImplInfo `json:"implementations"`
}

// ContractResolver 契约与目标模块多级解析器
type ContractResolver struct {
	exclusiveMap  map[string]ExclusiveServiceInfo  // serviceName -> ExclusiveServiceInfo
	contracts     map[string]MultiImplContractInfo // serviceName -> MultiImplContractInfo
	contractsByID map[uint64]MultiImplContractInfo // contractID -> MultiImplContractInfo
}

// LoadContractResolver 从数据库加载全部独占服务与多实现契约映射
func LoadContractResolver(ctx context.Context, db *gorm.DB) (*ContractResolver, error) {
	resolver := &ContractResolver{
		exclusiveMap:  make(map[string]ExclusiveServiceInfo),
		contracts:     make(map[string]MultiImplContractInfo),
		contractsByID: make(map[uint64]MultiImplContractInfo),
	}

	// 1. 加载所有契约
	var contractList []models.ServiceContract
	if err := db.WithContext(ctx).Find(&contractList).Error; err != nil {
		return nil, fmt.Errorf("load service contracts failed: %w", err)
	}
	for _, c := range contractList {
		if c.ProvideMode == models.ContractProvideModeMultiImpl {
			cKey := strings.TrimSpace(c.ServiceName)
			info := MultiImplContractInfo{
				ContractID:      c.ID,
				ServiceName:     cKey,
				DefinerModuleID: c.DefinerModuleID,
				Implementations: make([]ContractImplInfo, 0),
			}
			resolver.contracts[cKey] = info
			resolver.contractsByID[c.ID] = info
		}
	}

	// 2. 加载所有服务 (包含所挂载模块的 ArchProcessID)
	var services []models.ModuleService
	if err := db.WithContext(ctx).Preload("Module").Find(&services).Error; err != nil {
		return nil, fmt.Errorf("load module services failed: %w", err)
	}

	for _, svc := range services {
		svcName := strings.TrimSpace(svc.ServiceName)
		var archProcID *uint64
		if svc.Module != nil && svc.Module.ArchProcessID != nil {
			archProcID = svc.Module.ArchProcessID
		}

		if svc.ContractID == nil {
			// 独占服务
			resolver.exclusiveMap[svcName] = ExclusiveServiceInfo{
				ServiceID:     svc.ID,
				ModuleID:      svc.ModuleID,
				ArchProcessID: archProcID,
			}
		} else {
			// 多实现契约下的服务实现
			cID := *svc.ContractID
			impl := ContractImplInfo{
				ServiceID:     svc.ID,
				ModuleID:      svc.ModuleID,
				ArchProcessID: archProcID,
				ImplKey:       svc.ImplKey,
			}
			if cInfo, ok := resolver.contractsByID[cID]; ok {
				cInfo.Implementations = append(cInfo.Implementations, impl)
				resolver.contractsByID[cID] = cInfo
				resolver.contracts[cInfo.ServiceName] = cInfo
			}
		}
	}

	return resolver, nil
}

// ResolveDestinationFull 综合解析目标模块与契约 (三级分流降级)
// Tier 1: 独占服务 (PROVIDE) -> 确定性 dstModuleID, dstContractID = nil
// Tier 2: 多实现契约 (MULTI_IMPL) -> dstContractID = contract.ID
//
//	Fallback 2a: INGRESS 流量若 targetArchProcID 属于某实现模块 -> dstModuleID = impl.ModuleID
//	Fallback 2b: dstAddr 匹配会话 endpoint_snapshots -> 穿透对应 ArchProcessID -> dstModuleID
//	Fallback 2c: dstAddr 匹配宿主机环境进程表 envProcesses (IP:Port -> ArchProcessID) -> dstModuleID
//	Fallback 2d: 对端进程映射 sessionPeerProc 匹配实现所属 ArchProcessID -> dstModuleID
//	Fallback 2e: 无法裁决 -> dstModuleID = nil, dstContractID = contract.ID (严禁盲配 FM A 或任意实现)
//
// Tier 3: 未知契约/未登记服务 -> nil, nil
func (r *ContractResolver) ResolveDestinationFull(
	serviceName string,
	direction string,
	targetArchProcID uint64,
	dstAddr string,
	snapshots []models.EndpointSnapshotItem,
	envProcesses map[string]uint64,
	sessionPeerProc map[string]uint64,
) (dstModuleID *uint64, dstContractID *uint64) {
	key := strings.TrimSpace(serviceName)
	if key == "" {
		return nil, nil
	}

	// 1. Tier 1: 独占服务
	if excl, ok := r.exclusiveMap[key]; ok {
		mID := excl.ModuleID
		return &mID, nil
	}

	// 2. Tier 2: 多实现契约
	contract, ok := r.contracts[key]
	if !ok {
		return nil, nil
	}

	cID := contract.ContractID
	dstContractID = &cID

	// 2a. INGRESS 流量且已知接收进程 ArchProcessID
	if direction == models.TrafficDirIngress && targetArchProcID > 0 {
		for _, impl := range contract.Implementations {
			if impl.ArchProcessID != nil && *impl.ArchProcessID == targetArchProcID {
				mID := impl.ModuleID
				return &mID, dstContractID
			}
		}
	}

	cleanDst := strings.TrimSpace(dstAddr)
	dstHost, dstPort := parseHostAndPort(cleanDst)

	// 2b. 匹配会话级 endpoint_snapshots 快照
	if len(snapshots) > 0 && cleanDst != "" {
		for _, snap := range snapshots {
			if matchSnapshot(snap, cleanDst, dstHost, dstPort) {
				if snap.ArchProcessID > 0 {
					for _, impl := range contract.Implementations {
						if impl.ArchProcessID != nil && *impl.ArchProcessID == snap.ArchProcessID {
							mID := impl.ModuleID
							return &mID, dstContractID
						}
					}
				}
				for _, snapModID := range snap.ModuleIDs {
					for _, impl := range contract.Implementations {
						if impl.ModuleID == snapModID {
							mID := impl.ModuleID
							return &mID, dstContractID
						}
					}
				}
			}
		}
	}

	// 2c. 匹配宿主机环境进程表 envProcesses (IP:Port -> ArchProcessID)
	if len(envProcesses) > 0 && cleanDst != "" {
		if archPID, ok := envProcesses[cleanDst]; ok && archPID > 0 {
			for _, impl := range contract.Implementations {
				if impl.ArchProcessID != nil && *impl.ArchProcessID == archPID {
					mID := impl.ModuleID
					return &mID, dstContractID
				}
			}
		}
		// 尝试仅按 port 匹配 (若针对 127.0.0.1 或本机)
		if dstPort > 0 {
			for ep, archPID := range envProcesses {
				_, epPort := parseHostAndPort(ep)
				if epPort == dstPort && archPID > 0 {
					for _, impl := range contract.Implementations {
						if impl.ArchProcessID != nil && *impl.ArchProcessID == archPID {
							mID := impl.ModuleID
							return &mID, dstContractID
						}
					}
				}
			}
		}
	}

	// 2d. 匹配对端进程映射 sessionPeerProc
	if len(sessionPeerProc) > 0 {
		for _, archPID := range sessionPeerProc {
			if archPID > 0 {
				for _, impl := range contract.Implementations {
					if impl.ArchProcessID != nil && *impl.ArchProcessID == archPID {
						mID := impl.ModuleID
						return &mID, dstContractID
					}
				}
			}
		}
	}

	// 2e. 无法判定具体实现 -> 仅返回契约 ID，保留 dstModuleID = nil
	return nil, dstContractID
}

// ResolveDestination 实现 recorder.DestinationResolver 接口 (兼容单模块独占解析)
func (r *ContractResolver) ResolveDestination(serviceName, operation string) *uint64 {
	modID, _ := r.ResolveDestinationFull(serviceName, "", 0, "", nil, nil, nil)
	return modID
}

// IsMultiImplContract 检查是否为多实现契约
func (r *ContractResolver) IsMultiImplContract(serviceName string) bool {
	_, ok := r.contracts[strings.TrimSpace(serviceName)]
	return ok
}

// GetContractInfo 获取契约详情
func (r *ContractResolver) GetContractInfo(serviceName string) (*MultiImplContractInfo, bool) {
	info, ok := r.contracts[strings.TrimSpace(serviceName)]
	if !ok {
		return nil, false
	}
	return &info, true
}

func parseHostAndPort(addr string) (string, int) {
	if addr == "" {
		return "", 0
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, 0
	}
	p, _ := strconv.Atoi(portStr)
	return host, p
}

func matchSnapshot(snap models.EndpointSnapshotItem, rawAddr, host string, port int) bool {
	if port > 0 && snap.Port == port {
		if host == "" || host == "127.0.0.1" || host == "localhost" {
			return true
		}
		if snap.IP == host || snap.IP == "127.0.0.1" || snap.IP == "0.0.0.0" {
			return true
		}
	}
	snapAddr := fmt.Sprintf("%s:%d", snap.IP, snap.Port)
	return snapAddr == rawAddr
}
