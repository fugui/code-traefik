package hub

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"code-traefik/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 常量定义
const (
	DefaultLeaseTTL        = 30 * time.Second
	HeartbeatTimeout       = 2 * time.Second
	MaxMissedHeartbeats    = 3
	MinAgentPort           = 1024
	MaxAgentPort           = 65535
	AlibabaCloudMetadataIP = "100.100.100.200"
)

// 哨兵错误定义
var (
	ErrNotImplemented         = errors.New("hub: not implemented")
	ErrAgentNotFound          = errors.New("hub: agent not found")
	ErrAgentOffline           = errors.New("hub: agent is offline")
	ErrAgentForbidden         = errors.New("hub: permission denied for agent")
	ErrInvalidAgentEndpoint   = errors.New("hub: invalid agent endpoint")
	ErrLoopbackDisallowed     = errors.New("hub: loopback agent endpoint is disallowed in non-dev mode")
	ErrMetadataEndpoint       = errors.New("hub: cloud metadata endpoint is forbidden")
	ErrAgentPortOutOfRange    = errors.New("hub: agent port must be between 1024 and 65535")
	ErrAgentClaimCodeMismatch = errors.New("hub: agent claim code mismatch")
	ErrAgentVersionMismatch   = errors.New("hub: agent protocol version mismatch")
	ErrAgentHasProcesses      = errors.New("hub: cannot delete agent with existing processes")
	ErrAgentLeaseActive       = errors.New("hub: cannot delete or modify agent with active lease")
)

// LeaseConflictError 排他租约冲突错误 (409 Conflict)
type LeaseConflictError struct {
	AgentID       uint64
	AgentName     string
	LeaseType     string
	LeaseRefID    uint64
	LeaseHolderID uint64
	ExpireAt      time.Time
}

func (e *LeaseConflictError) Error() string {
	return fmt.Sprintf("hub: agent %d (%s) is already leased by user %d for %s ref %d until %s",
		e.AgentID, e.AgentName, e.LeaseHolderID, e.LeaseType, e.LeaseRefID, e.ExpireAt.Format(time.RFC3339))
}

// ValidateAgentEndpoint 校验 Agent 注册/更新地址并进行严格 SSRF 防护
func ValidateAgentEndpoint(rawEndpoint string, devAllowLoopback bool) (string, error) {
	trimmed := strings.TrimSpace(rawEndpoint)
	if trimmed == "" {
		return "", ErrInvalidAgentEndpoint
	}

	raw := trimmed
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "http://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", ErrInvalidAgentEndpoint
	}
	if u.Scheme != "http" {
		return "", ErrInvalidAgentEndpoint
	}

	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		return "", ErrInvalidAgentEndpoint
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port < MinAgentPort || port > MaxAgentPort {
		return "", ErrAgentPortOutOfRange
	}

	// 必须为合法 IPv4 地址，严禁域名与 IPv6
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil {
		return "", ErrInvalidAgentEndpoint
	}

	// 回环地址拦截 (127.0.0.0/8)
	if ip.IsLoopback() {
		if !devAllowLoopback {
			return "", ErrLoopbackDisallowed
		}
	}

	// 链路本地元数据地址 (169.254.0.0/16) 拦截
	if ip.IsLinkLocalUnicast() {
		return "", ErrMetadataEndpoint
	}

	// 阿里云内部元数据地址 (100.100.100.200) 拦截
	if host == AlibabaCloudMetadataIP {
		return "", ErrMetadataEndpoint
	}

	// 组播 (224.0.0.0/4)、广播与未指定全零 (0.0.0.0) 拦截
	if ip.IsMulticast() || ip.IsUnspecified() || host == "255.255.255.255" {
		return "", ErrInvalidAgentEndpoint
	}

	return fmt.Sprintf("http://%s:%d", ip.String(), port), nil
}

// NormalizeEndpoint 标准化 Agent Endpoint 为 http://ip:port
func NormalizeEndpoint(raw string) (string, error) {
	return ValidateAgentEndpoint(raw, true)
}

// AcquireAgentLeases 原子申请一组 Agent 的排他租约（严格按 AgentID 升序死锁规避）
func AcquireAgentLeases(ctx context.Context, db *gorm.DB, agentIDs []uint64, leaseType string, leaseRefID uint64, holderID uint, ttl time.Duration) error {
	if len(agentIDs) == 0 {
		return nil
	}

	// 严格按 ID 升序去重排序，彻底规避交叉死锁
	idMap := make(map[uint64]bool, len(agentIDs))
	uniqueIDs := make([]uint64, 0, len(agentIDs))
	for _, id := range agentIDs {
		if !idMap[id] {
			idMap[id] = true
			uniqueIDs = append(uniqueIDs, id)
		}
	}
	sort.Slice(uniqueIDs, func(i, j int) bool {
		return uniqueIDs[i] < uniqueIDs[j]
	})

	now := time.Now()

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var agents []models.Agent
		if err := tx.Where("id IN ?", uniqueIDs).
			Order("id ASC").
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Find(&agents).Error; err != nil {
			return err
		}

		if len(agents) != len(uniqueIDs) {
			return ErrAgentNotFound
		}

		for _, agt := range agents {
			// 防越权校验：若为个人私有 Agent，必须为本人持有
			if agt.Scope == models.ScopePrivate {
				if agt.OwnerID == nil || *agt.OwnerID != holderID {
					return ErrAgentForbidden
				}
			}

			// 离线节点禁止申请租约
			if agt.Status != models.AgentStatusOnline {
				return ErrAgentOffline
			}

			// 检查现有租约冲突
			if agt.LeaseType != models.AgentLeaseNone {
				// 检查是否已安全硬性到期（惰性自愈）
				if agt.LeaseExpiresAt != nil && agt.LeaseExpiresAt.Before(now) {
					// 租约已超时，自愈清空
				} else if agt.LeaseType == leaseType && agt.LeaseRefID != nil && *agt.LeaseRefID == leaseRefID &&
					agt.LeaseHolderID != nil && *agt.LeaseHolderID == holderID {
					// 重复续约持有，允许
				} else {
					holderIDVal := uint64(0)
					if agt.LeaseHolderID != nil {
						holderIDVal = uint64(*agt.LeaseHolderID)
					}
					refIDVal := uint64(0)
					if agt.LeaseRefID != nil {
						refIDVal = *agt.LeaseRefID
					}
					expVal := time.Time{}
					if agt.LeaseExpiresAt != nil {
						expVal = *agt.LeaseExpiresAt
					}
					return &LeaseConflictError{
						AgentID:       agt.ID,
						AgentName:     agt.Name,
						LeaseType:     agt.LeaseType,
						LeaseRefID:    refIDVal,
						LeaseHolderID: holderIDVal,
						ExpireAt:      expVal,
					}
				}
			}
		}

		// 全量通过，执行批量排他锁定
		exp := now.Add(ttl)
		for _, agt := range agents {
			if err := tx.Model(&models.Agent{}).Where("id = ?", agt.ID).Updates(map[string]interface{}{
				"lease_type":        leaseType,
				"lease_ref_id":      leaseRefID,
				"lease_holder_id":   holderID,
				"lease_acquired_at": &now,
				"lease_expires_at":  &exp,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// ReleaseAgentLeases 原子释放一组 Agent 的排他租约
func ReleaseAgentLeases(ctx context.Context, db *gorm.DB, agentIDs []uint64, leaseType string, leaseRefID uint64) error {
	if len(agentIDs) == 0 {
		return nil
	}

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, aid := range agentIDs {
			var agt models.Agent
			if err := tx.First(&agt, aid).Error; err == nil {
				if agt.LeaseType == leaseType && agt.LeaseRefID != nil && *agt.LeaseRefID == leaseRefID {
					_ = tx.Model(&models.Agent{}).Where("id = ?", aid).Updates(map[string]interface{}{
						"lease_type":        models.AgentLeaseNone,
						"lease_ref_id":      nil,
						"lease_holder_id":   nil,
						"lease_acquired_at": nil,
						"lease_expires_at":  nil,
					}).Error
				}
			}
		}
		return nil
	})
}

// ForceUnlockAgentLease 管理员强制解除 Agent 排他租约并触发 Agent 探针自洁
func ForceUnlockAgentLease(ctx context.Context, db *gorm.DB, agentID uint64, adminUserID uint, reason string) error {
	var agt models.Agent
	if err := db.First(&agt, agentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrAgentNotFound
		}
		return err
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		// 若当前租约关联了录制任务或重放任务，将其标记为失败终态
		if agt.LeaseType == models.AgentLeaseRecording && agt.LeaseRefID != nil {
			_ = tx.Model(&models.RecordingSession{}).Where("id = ? AND status = ?", *agt.LeaseRefID, models.SessionStatusRecording).
				Updates(map[string]interface{}{
					"status":      models.SessionStatusFailed,
					"stop_reason": "管理员强制释放租约: " + reason,
				}).Error
		} else if agt.LeaseType == models.AgentLeaseReplay && agt.LeaseRefID != nil {
			_ = tx.Model(&models.ReplayTask{}).Where("id = ? AND lifecycle_state = 'RUNNING'", *agt.LeaseRefID).
				Updates(map[string]interface{}{
					"lifecycle_state": models.SessionStatusFailed,
					"error_message":   "管理员强制释放租约: " + reason,
				}).Error
		}

		return tx.Model(&models.Agent{}).Where("id = ?", agentID).Updates(map[string]interface{}{
			"lease_type":        models.AgentLeaseNone,
			"lease_ref_id":      nil,
			"lease_holder_id":   nil,
			"lease_acquired_at": nil,
			"lease_expires_at":  nil,
		}).Error
	})
	if err != nil {
		return err
	}

	// 最佳努力调用 Agent /agent/v1/purge 卸载探针与清空 Maps
	go func(endpoint string) {
		client := &http.Client{
			Timeout: 2 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		req, err := http.NewRequest(http.MethodPost, endpoint+"/agent/v1/purge", nil)
		if err == nil {
			resp, err := client.Do(req)
			if err == nil {
				_ = resp.Body.Close()
			}
		}
	}(agt.Endpoint)

	return nil
}
