package version

import "fmt"

var (
	// Version 语义化主版本号（默认 v0.1.0，构建期可通过 -ldflags 动态重写）
	Version = "v0.1.0"
	// CommitID Git 短提交散列
	CommitID = "unknown"
	// BuildTime 确定性构建时间戳 (UTC 或本地时间)
	BuildTime = "unknown"
)

// Info 版本元数据结构
type Info struct {
	Version   string `json:"version"`
	CommitID  string `json:"commit_id"`
	BuildTime string `json:"build_time"`
}

// Get 返回版本元数据对象
func Get() Info {
	return Info{
		Version:   Version,
		CommitID:  CommitID,
		BuildTime: BuildTime,
	}
}

// Short 返回用于 API 传输与元数据记录的语义主版本号 (如 "v0.1.0")
func Short() string {
	return Version
}

// Full 返回携带 Commit 与构建时间的完整指纹字符串
// 例如: "v0.1.0 (commit: 53fa4ac, built: 2026-11-03 10:08:52)"
func Full() string {
	return fmt.Sprintf("%s (commit: %s, built: %s)", Version, CommitID, BuildTime)
}

// String 实现 fmt.Stringer 接口，默认输出 Full 指纹
func String() string {
	return Full()
}
