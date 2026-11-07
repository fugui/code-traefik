package version

import (
	"fmt"
	"os/exec"
	"runtime/debug"
	"strings"
	"time"
)

var (
	// Version 语义化主版本号（默认 v0.2.0，构建期可通过 -ldflags 动态重写）
	Version = "v0.2.0"
	// CommitID Git 短提交散列
	CommitID = "unknown"
	// BuildTime 确定性构建时间戳 (UTC 或本地时间)
	BuildTime = "unknown"
)

func init() {
	initFromBuildInfo()
}

// initFromBuildInfo 在未通过 -ldflags 显式注入元数据时，尝试通过 runtime/debug.ReadBuildInfo() 读取 Go 编译器内置的 VCS 信息进行优雅降级
func initFromBuildInfo() {
	info, ok := debug.ReadBuildInfo()
	var vcsRevision, vcsTime string
	var vcsModified bool

	if ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				vcsRevision = setting.Value
			case "vcs.time":
				vcsTime = setting.Value
			case "vcs.modified":
				vcsModified = (setting.Value == "true")
			}
		}
	}

	if (CommitID == "unknown" || CommitID == "") && vcsRevision != "" {
		short := vcsRevision
		if len(short) > 7 {
			short = short[:7]
		}
		if vcsModified {
			short += "-dirty"
		}
		CommitID = short
	}

	if (BuildTime == "unknown" || BuildTime == "") && vcsTime != "" {
		if t, err := time.Parse(time.RFC3339, vcsTime); err == nil {
			BuildTime = t.Local().Format("2006-01-02 15:04:05")
		} else {
			BuildTime = vcsTime
		}
	}

	// 若处于本地开发调试环境 (如 go run)，Go 默认关闭 VCS 打包，此时通过 git 命令尝试补齐
	if (CommitID == "unknown" || CommitID == "") || (BuildTime == "unknown" || BuildTime == "") {
		fillFromGitCLI()
	}
}

// fillFromGitCLI 仅作为本地调试开发阶段的尽力而为兜底，不阻塞程序启动
func fillFromGitCLI() {
	if CommitID == "unknown" || CommitID == "" {
		cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
		if out, err := cmd.Output(); err == nil {
			if trimmed := strings.TrimSpace(string(out)); trimmed != "" {
				CommitID = trimmed
			}
		}
	}

	if BuildTime == "unknown" || BuildTime == "" {
		cmd := exec.Command("git", "log", "-1", "--format=%cd", "--date=format:%Y-%m-%d %H:%M:%S")
		if out, err := cmd.Output(); err == nil {
			if trimmed := strings.TrimSpace(string(out)); trimmed != "" {
				BuildTime = trimmed
			}
		}
	}
}

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
