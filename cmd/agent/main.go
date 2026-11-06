package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"code-traefik/internal/agent/server"
	"code-traefik/pkg/ebpf"
	"code-traefik/pkg/version"
)

var (
	Version   = version.Version
	CommitID  = version.CommitID
	BuildTime = version.BuildTime
)

// DefaultAllowIPs 默认受信任主控 IP 白名单（支持环回联调及现网主控服务器 10.1.64.178 直连挂载）
const DefaultAllowIPs = "127.0.0.1,10.1.64.178"

// resolveDefaultAllowIPs 支持环境变量 TRAEFIK_AGENT_ALLOW_IPS 覆盖默认白名单
func resolveDefaultAllowIPs() string {
	if env := os.Getenv("TRAEFIK_AGENT_ALLOW_IPS"); env != "" {
		return env
	}
	return DefaultAllowIPs
}

// parseAllowIPs 解析逗号分隔的 IP / CIDR 字符串
func parseAllowIPs(raw string) []string {
	var allowIPs []string
	if raw != "" {
		parts := strings.Split(raw, ",")
		for _, p := range parts {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				allowIPs = append(allowIPs, trimmed)
			}
		}
	}
	return allowIPs
}

// runDoctorCheck 执行宿主机环境与 eBPF 内核特权就绪诊断
func runDoctorCheck(procRoot string) int {
	fmt.Println("================================================================================")
	fmt.Printf("   code-traefik-agent 运行环境与内核特权就绪诊断 (Doctor) - %s\n", version.Full())
	fmt.Println("================================================================================")

	hasErrors := false

	// 1. 操作系统与系统架构检查
	fmt.Printf("[1/4] 操作系统与系统架构检查:\n")
	fmt.Printf("      - 操作系统: %s\n", runtime.GOOS)
	fmt.Printf("      - 系统架构: %s\n", runtime.GOARCH)
	if runtime.GOOS != "linux" {
		fmt.Printf("      - [✗] 错误: eBPF 探针仅支持 Linux 操作系统\n")
		hasErrors = true
	} else {
		fmt.Printf("      - [✓] 操作系统检查通过\n")
	}

	// 2. Linux 内核版本预检
	fmt.Printf("\n[2/4] Linux 内核版本预检:\n")
	releaseBytes, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		fmt.Printf("      - [?] 无法读取 /proc/sys/kernel/osrelease: %v\n", err)
	} else {
		releaseStr := strings.TrimSpace(string(releaseBytes))
		fmt.Printf("      - 内核版本: %s\n", releaseStr)
		var major, minor int
		if _, err := fmt.Sscanf(releaseStr, "%d.%d", &major, &minor); err == nil {
			if major > 5 || (major == 5 && minor >= 8) {
				fmt.Printf("      - [✓] 内核版本满足 eBPF RingBuf 与 SocketOps 推荐标准 (>= 5.8)\n")
			} else {
				fmt.Printf("      - [⚠️] 警告: 当前内核版本 %d.%d 低于 5.8，部分 eBPF 特性可能受限\n", major, minor)
			}
		}
	}

	// 3. 内核运行特权与 Linux Capabilities 探测
	fmt.Printf("\n[3/4] 内核运行特权与 Capabilities 探测:\n")
	checker := ebpf.NewLinuxCapabilityChecker()
	hasCaps, missing, detail := checker.CheckCapabilities()
	fmt.Printf("      - 运行 UID: EUID=%d\n", os.Geteuid())
	fmt.Printf("      - 特权详情: %s\n", detail)
	if !hasCaps {
		fmt.Printf("      - [✗] 权限不足: 缺少必要 Capabilities: %s\n", strings.Join(missing, ", "))
		fmt.Printf("      - [💡] 修复指引: 请使用 sudo 权限启动 Agent (sudo ./bin/code-traefik-agent)，或通过 setcap 授权\n")
		hasErrors = true
	} else {
		fmt.Printf("      - [✓] Linux Capabilities 具备状态完整\n")
	}

	// 4. 关键虚拟文件系统挂载探测 (/proc 与 /sys/fs/bpf)
	fmt.Printf("\n[4/4] 关键虚拟文件系统挂载探测:\n")
	if _, err := os.Stat(procRoot); err != nil {
		fmt.Printf("      - [✗] Procfs 无法访问 (%s): %v\n", procRoot, err)
		hasErrors = true
	} else {
		fmt.Printf("      - [✓] Procfs 正常可读 (%s)\n", procRoot)
	}
	if _, err := os.Stat("/sys/fs/bpf"); err != nil {
		fmt.Printf("      - [⚠️] 提示: /sys/fs/bpf 未挂载或不可直接访问: %v\n", err)
	} else {
		fmt.Printf("      - [✓] BPF 文件系统就绪 (/sys/fs/bpf)\n")
	}

	fmt.Println("================================================================================")
	if hasErrors {
		fmt.Printf("体检结论: 存在阻塞性环境或特权问题，无法正常挂载 eBPF 探针进行流量拦截！\n")
		return 1
	}
	fmt.Printf("体检结论: 环境与内核特权完全就绪，可随时承接管控端预挂载与流量捕获！\n")
	return 0
}

func main() {
	defaultAllowIPs := resolveDefaultAllowIPs()

	var (
		listenAddr  = flag.String("listen", "0.0.0.0:9091", "Agent 监听地址 (IP:Port)")
		allowIPsStr = flag.String("allow-ips", defaultAllowIPs, "允许访问的主控 IP/CIDR 白名单 (逗号分隔)")
		claimCode   = flag.String("claim-code", "", "预设认领码 (留空自动生成一次性认领码)")
		claimFile   = flag.String("claim-code-file", "", "认领码保存路径 (如 /var/run/code-traefik/claim.token)")
		maxBufferMB = flag.Int64("max-buffer-mb", 64, "单个活跃会话有界缓冲上限 (MB)")
		procRoot    = flag.String("proc-root", "/proc", "宿主机 procfs 挂载路径")
		showVersion = flag.Bool("version", false, "显示版本信息并退出")
		doctor      = flag.Bool("doctor", false, "执行环境与内核特权就绪体检诊断并退出")
		check       = flag.Bool("check", false, "执行环境与内核特权就绪体检诊断并退出 (等同于 --doctor)")
		requireCaps = flag.Bool("require-caps", false, "是否强制要求必须具备 eBPF 内核特权，若缺少则拒绝启动")
	)
	flag.Parse()

	// 保证向后兼容，双向同步版本元数据
	if Version != "1.0.0" && Version != version.Version {
		version.Version = Version
	}
	if CommitID != "unknown" && CommitID != version.CommitID {
		version.CommitID = CommitID
	}
	if BuildTime != "unknown" && BuildTime != version.BuildTime {
		version.BuildTime = BuildTime
	}

	if *showVersion {
		fmt.Printf("code-traefik-agent %s\n", version.Full())
		os.Exit(0)
	}

	if *doctor || *check {
		os.Exit(runDoctorCheck(*procRoot))
	}

	allowIPs := parseAllowIPs(*allowIPsStr)

	cfg := server.AgentServerConfig{
		ListenAddr:            *listenAddr,
		AllowIPs:              allowIPs,
		ClaimCode:             *claimCode,
		ClaimCodeFile:         *claimFile,
		MaxSessionBufferBytes: *maxBufferMB * 1024 * 1024,
		AgentVersion:          version.Short(),
		ProcRoot:              *procRoot,
	}

	srv, err := server.NewAgentServer(cfg)
	if err != nil {
		log.Fatalf("初始化 code-traefik-agent 失败: %v", err)
	}

	log.Printf("[Agent] 启动 code-traefik-agent 版本: %s", version.Full())
	log.Printf("[Agent] 监听地址: %s, 白名单: %v", *listenAddr, allowIPs)
	log.Printf("[Agent] 认领码 (ClaimCode): %s", srv.ClaimCode())
	log.Printf("[Agent] 实例 BootID: %s", srv.BootID())

	// 启动特权自检与显式横幅输出
	if srv.EbpfManager() != nil {
		hasCaps, missing, capDetail := srv.EbpfManager().CheckCapabilities()
		if hasCaps {
			log.Printf("[Agent] [✓] 内核特权检测通过: %s", capDetail)
		} else {
			log.Printf("[Agent] ======================================================================")
			log.Printf("[Agent] [⚠️  警告: 当前进程缺少 eBPF 内核特权]")
			log.Printf("[Agent] 运行 UID: EUID=%d, 缺少 Capabilities: %s", os.Geteuid(), strings.Join(missing, ", "))
			log.Printf("[Agent] 详情: %s", capDetail)
			log.Printf("[Agent] 影响: Agent 将无法向内核挂载探针，后续预挂载与流量捕获请求将被拒绝 (HTTP 503)!")
			log.Printf("[Agent] 建议: 请使用 sudo 权限运行 (sudo %s)，或通过 setcap 授权。", os.Args[0])
			log.Printf("[Agent] ======================================================================")
			if *requireCaps {
				log.Fatalf("[Agent] 错误: 已开启 --require-caps，由于缺少特权，启动中止。")
			}
		}
	}

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
