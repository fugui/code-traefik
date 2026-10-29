package models

import (
	"fmt"
	"regexp"
	"time"

	"gorm.io/gorm"
)

var partitionNameRegex = regexp.MustCompile(`^traefik_traffic_records_y(\d{4})m(\d{2})d(\d{2})$`)

// PartitionManager 负责 traefik_traffic_records 物理分区母表与按日分区的自动生命周期管理
type PartitionManager struct{}

// EnsureParentTable 幂等创建物理分区母表及自增序列
func (pm *PartitionManager) EnsureParentTable(db *gorm.DB) error {
	ddl := `
CREATE SEQUENCE IF NOT EXISTS traefik_traffic_records_id_seq;

CREATE TABLE IF NOT EXISTS traefik_traffic_records (
    id BIGINT NOT NULL DEFAULT nextval('traefik_traffic_records_id_seq'),
    created_at TIMESTAMPTZ NOT NULL,
    session_id BIGINT NOT NULL,
    seq_no BIGINT NOT NULL,
    agent_id BIGINT NOT NULL,
    process_id BIGINT NOT NULL,
    captured_pid INTEGER NOT NULL,
    conn_key VARCHAR(128) NOT NULL,
    direction VARCHAR(16) NOT NULL,
    exchange_type VARCHAR(24) NOT NULL DEFAULT 'UNARY',
    src_addr VARCHAR(64) NOT NULL,
    dst_addr VARCHAR(64) NOT NULL,
    req_captured_at_ns BIGINT NOT NULL,
    rsp_captured_at_ns BIGINT,
    latency_us BIGINT,
    transport_proto SMALLINT NOT NULL DEFAULT 1,
    sno NUMERIC(20,0) NOT NULL,
    req_type SMALLINT NOT NULL DEFAULT 0,
    rsp_seg_count INTEGER NOT NULL DEFAULT 0,
    is_packed SMALLINT NOT NULL DEFAULT 1,
    service_name VARCHAR(128) NOT NULL DEFAULT '',
    operation VARCHAR(128) NOT NULL DEFAULT '',
    status_code NUMERIC(20,0) NOT NULL DEFAULT 0,
    trace_id VARCHAR(32) NOT NULL DEFAULT '',
    span_id VARCHAR(32) NOT NULL DEFAULT '',
    father_span_id VARCHAR(32) NOT NULL DEFAULT '',
    subsystem_id BIGINT,
    src_module_id BIGINT,
    src_attribution_mode VARCHAR(32) NOT NULL DEFAULT '',
    src_candidate_module_ids JSONB,
    dst_module_id BIGINT,
    headers_json JSONB NOT NULL DEFAULT '{}',
    schema_id BIGINT,
    schema_version INTEGER,
    decode_status VARCHAR(32) NOT NULL DEFAULT '',
    payload_json JSONB NOT NULL DEFAULT '{}',
    req_payload_sha256 VARCHAR(64) NOT NULL DEFAULT '',
    raw_req_packet BYTEA,
    raw_rsp_packet BYTEA,
    is_truncated BOOLEAN NOT NULL DEFAULT FALSE,
    is_replayable BOOLEAN NOT NULL DEFAULT TRUE,
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

CREATE INDEX IF NOT EXISTS idx_traefik_tr_payload_gin ON traefik_traffic_records USING GIN (payload_json jsonb_path_ops);
CREATE INDEX IF NOT EXISTS idx_traefik_tr_session_seq ON traefik_traffic_records (session_id, seq_no);
CREATE INDEX IF NOT EXISTS idx_traefik_tr_session_svc ON traefik_traffic_records (session_id, service_name, operation);
CREATE INDEX IF NOT EXISTS idx_traefik_tr_conn_sno ON traefik_traffic_records (conn_key, sno);
`
	return db.Exec(ddl).Error
}

// FormatPartitionName 计算按日分区表名
func (pm *PartitionManager) FormatPartitionName(date time.Time) string {
	d := date.UTC()
	return fmt.Sprintf("traefik_traffic_records_y%04dm%02dd%02d", d.Year(), d.Month(), d.Day())
}

// EnsureDailyPartition 幂等预建单日物理子分区及其局部 GIN 索引
func (pm *PartitionManager) EnsureDailyPartition(db *gorm.DB, date time.Time) (string, error) {
	d := date.UTC()
	from := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 1)

	tableName := pm.FormatPartitionName(from)
	fromStr := from.Format("2006-01-02 15:04:05+00")
	toStr := to.Format("2006-01-02 15:04:05+00")

	createPartitionSQL := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s PARTITION OF traefik_traffic_records
FOR VALUES FROM ('%s') TO ('%s');

CREATE INDEX IF NOT EXISTS idx_%s_payload_gin ON %s USING GIN (payload_json jsonb_path_ops);
`, tableName, fromStr, toStr, tableName, tableName)

	if err := db.Exec(createPartitionSQL).Error; err != nil {
		return "", fmt.Errorf("创建子分区表 [%s] 失败: %w", tableName, err)
	}

	return tableName, nil
}

// EnsurePartitionsRange 批量预建时间范围内的日常子分区
func (pm *PartitionManager) EnsurePartitionsRange(db *gorm.DB, from, to time.Time) ([]string, error) {
	var created []string
	curr := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	end := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)

	for !curr.After(end) {
		name, err := pm.EnsureDailyPartition(db, curr)
		if err != nil {
			return created, err
		}
		created = append(created, name)
		curr = curr.AddDate(0, 0, 1)
	}
	return created, nil
}

// PurgeExpiredPartitions 通过轻量级 DROP TABLE 物理清理超过保留期 (retentionDays) 的历史子分区表
func (pm *PartitionManager) PurgeExpiredPartitions(db *gorm.DB, retentionDays int, now time.Time) ([]string, error) {
	cutoffDate := now.UTC().AddDate(0, 0, -retentionDays)
	cutoffDay := time.Date(cutoffDate.Year(), cutoffDate.Month(), cutoffDate.Day(), 0, 0, 0, 0, time.UTC)

	query := `
SELECT c.relname
FROM pg_inherits i
JOIN pg_class c ON c.oid = i.inhrelid
JOIN pg_class p ON p.oid = i.inhparent
WHERE p.relname = 'traefik_traffic_records';
`
	var partitionNames []string
	if err := db.Raw(query).Scan(&partitionNames).Error; err != nil {
		return nil, fmt.Errorf("查询分区子表列表失败: %w", err)
	}

	var dropped []string
	for _, name := range partitionNames {
		matches := partitionNameRegex.FindStringSubmatch(name)
		if len(matches) != 4 {
			continue
		}

		var year, month, day int
		_, _ = fmt.Sscanf(matches[1], "%d", &year)
		_, _ = fmt.Sscanf(matches[2], "%d", &month)
		_, _ = fmt.Sscanf(matches[3], "%d", &day)
		partDay := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)

		// 若分区归属日期严格早于安全清理阈值日
		if partDay.Before(cutoffDay) {
			dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s;", name)
			if err := db.Exec(dropSQL).Error; err != nil {
				return dropped, fmt.Errorf("DROP 分区表 [%s] 失败: %w", name, err)
			}
			dropped = append(dropped, name)
		}
	}

	return dropped, nil
}
