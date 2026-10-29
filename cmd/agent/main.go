package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"code-traefik/internal/agent/server"
)

var (
	Version   = "1.0.0"
	CommitID  = "unknown"
	BuildTime = "unknown"
)

func main() {
	var (
		listenAddr  = flag.String("listen", "0.0.0.0:9091", "Agent 监听地址 (IP:Port)")
		allowIPsStr = flag.String("allow-ips", "127.0.0.1", "允许访问的主控 IP/CIDR 白名单 (逗号分隔)")
		claimCode   = flag.String("claim-code", "", "预设认领码 (留空自动生成一次性认领码)")
		claimFile   = flag.String("claim-code-file", "", "认领码保存路径 (如 /var/run/code-traefik/claim.token)")
		maxBufferMB = flag.Int64("max-buffer-mb", 64, "单个活跃会话有界缓冲上限 (MB)")
		procRoot    = flag.String("proc-root", "/proc", "宿主机 procfs 挂载路径")
		showVersion = flag.Bool("version", false, "显示版本信息并退出")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("code-traefik-agent %s (commit: %s, built: %s)\n", Version, CommitID, BuildTime)
		os.Exit(0)
	}

	var allowIPs []string
	if *allowIPsStr != "" {
		parts := strings.Split(*allowIPsStr, ",")
		for _, p := range parts {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				allowIPs = append(allowIPs, trimmed)
			}
		}
	}

	cfg := server.AgentServerConfig{
		ListenAddr:            *listenAddr,
		AllowIPs:              allowIPs,
		ClaimCode:             *claimCode,
		ClaimCodeFile:         *claimFile,
		MaxSessionBufferBytes: *maxBufferMB * 1024 * 1024,
		AgentVersion:          Version,
		ProcRoot:              *procRoot,
	}

	srv, err := server.NewAgentServer(cfg)
	if err != nil {
		log.Fatalf("初始化 code-traefik-agent 失败: %v", err)
	}

	log.Printf("[Agent] 启动 code-traefik-agent 版本: %s", Version)
	log.Printf("[Agent] 监听地址: %s, 白名单: %v", *listenAddr, allowIPs)
	log.Printf("[Agent] 认领码 (ClaimCode): %s", srv.ClaimCode())
	log.Printf("[Agent] 实例 BootID: %s", srv.BootID())

	if err := srv.Start(); err != nil {
		log.Fatalf("启动监听失败: %v", err)
	}

	// 捕获操作系统退出信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	log.Printf("[Agent] 收到信号 %v，正在执行 PurgeAllMonitors 安全自洁与卸载...", sig)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("[Agent] 关闭服务异常: %v", err)
	} else {
		log.Printf("[Agent] 内核探针已全部卸载，服务安全退出。")
	}
}
