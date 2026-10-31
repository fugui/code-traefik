package ebpf

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"code-traefik/pkg/agentproto"
)

// DefaultProcRoot 默认 /proc 根目录
const DefaultProcRoot = "/proc"

// ProcScanner 负责以只读方式解析 /proc 文件系统 (零 os/exec 依赖)
type ProcScanner struct {
	root string
}

// NewProcScanner 创建 procfs 扫描器，支持自定义根目录便于单测
func NewProcScanner(procRoot string) *ProcScanner {
	if procRoot == "" {
		procRoot = DefaultProcRoot
	}
	return &ProcScanner{root: procRoot}
}

// ScanListenProcesses 扫描当前宿主机所有处于 TCP_LISTEN 状态的进程
func (s *ProcScanner) ScanListenProcesses() ([]agentproto.DiscoveredProcess, error) {
	// 1. 读取 /proc/net/tcp
	tcpPath := filepath.Join(s.root, "net", "tcp")
	f, err := os.Open(tcpPath)
	if err != nil {
		return nil, fmt.Errorf("打开 %s 失败: %w", tcpPath, err)
	}
	defer f.Close()

	// inode -> listenPort
	listenInodes := make(map[string]uint16)
	scanner := bufio.NewScanner(f)
	// 跳过第一行标题
	if scanner.Scan() {
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			fields := strings.Fields(line)
			if len(fields) < 10 {
				continue
			}
			// fields[3] 为连接状态，0A 表示 TCP_LISTEN
			if fields[3] != "0A" {
				continue
			}
			// fields[1] 为 local_address, 格式为 HEX_IP:HEX_PORT
			port, err := parseHexPort(fields[1])
			if err != nil {
				continue
			}
			inode := fields[9]
			listenInodes[inode] = port
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取 %s 失败: %w", tcpPath, err)
	}

	if len(listenInodes) == 0 {
		return nil, nil
	}

	// 2. 扫描 /proc/[pid]/fd 反查所属 PID
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 目录失败: %w", s.root, err)
	}

	var results []agentproto.DiscoveredProcess
	seenPIDPort := make(map[string]bool)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pidNum, err := strconv.ParseUint(entry.Name(), 10, 32)
		if err != nil {
			continue // 非数字目录，跳过
		}
		pid := uint32(pidNum)

		fdDir := filepath.Join(s.root, entry.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue // 权限不足或进程已退出
		}

		for _, fd := range fds {
			linkTarget, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			// 匹配 socket:[12345]
			if strings.HasPrefix(linkTarget, "socket:[") && strings.HasSuffix(linkTarget, "]") {
				inode := linkTarget[8 : len(linkTarget)-1]
				if port, exists := listenInodes[inode]; exists {
					dedupKey := fmt.Sprintf("%d:%d", pid, port)
					if seenPIDPort[dedupKey] {
						continue
					}
					seenPIDPort[dedupKey] = true

					cmdline := s.readCmdline(pid)
					exeName := s.readExeName(pid)

					results = append(results, agentproto.DiscoveredProcess{
						PID:        pid,
						ListenPort: port,
						Cmdline:    cmdline,
						ExeName:    exeName,
					})
				}
			}
		}
	}

	return results, nil
}

// FindPIDByCmd 纯客户端进程无监听端口时，按命令行特征反查实时 PID
func (s *ProcScanner) FindPIDByCmd(cmdPattern string) (uint32, error) {
	cmdPattern = strings.TrimSpace(cmdPattern)
	if cmdPattern == "" {
		return 0, nil
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return 0, fmt.Errorf("读取 %s 目录失败: %w", s.root, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid64, err := strconv.ParseUint(entry.Name(), 10, 32)
		if err != nil {
			continue
		}
		pid := uint32(pid64)
		cmdlinePath := filepath.Join(s.root, entry.Name(), "cmdline")
		cmdBytes, err := os.ReadFile(cmdlinePath)
		if err != nil {
			continue
		}
		cmdline := string(bytes.ReplaceAll(cmdBytes, []byte{0}, []byte{' '}))
		exePath := filepath.Join(s.root, entry.Name(), "exe")
		exeName, _ := os.Readlink(exePath)
		if strings.Contains(cmdline, cmdPattern) || strings.Contains(exeName, cmdPattern) {
			return pid, nil
		}
	}
	return 0, nil
}

// FindPIDByPortAndCmd 按监听端口与命令行正则查找实时 PID (listenPort = 0 时按纯命令行特征查找客户端进程)
func (s *ProcScanner) FindPIDByPortAndCmd(listenPort uint16, cmdPattern string) (uint32, error) {
	if listenPort == 0 {
		return s.FindPIDByCmd(cmdPattern)
	}
	procs, err := s.ScanListenProcesses()
	if err != nil {
		return 0, err
	}

	for _, p := range procs {
		if p.ListenPort == listenPort {
			if cmdPattern == "" || strings.Contains(p.Cmdline, cmdPattern) || strings.Contains(p.ExeName, cmdPattern) {
				return p.PID, nil
			}
		}
	}

	return 0, nil
}

// DiagnoseEstablishedConns 诊断目标进程当前已与登记下游建立的 ESTABLISHED 长连接数 (A-01)
func (s *ProcScanner) DiagnoseEstablishedConns(pid uint32, downstreams []agentproto.DownstreamEndpoint) (int, bool, error) {
	if len(downstreams) == 0 {
		return 0, false, nil
	}

	// 目标进程网络空间连接
	tcpPath := filepath.Join(s.root, strconv.FormatUint(uint64(pid), 10), "net", "tcp")
	f, err := os.Open(tcpPath)
	if err != nil {
		// 回退读取宿主机全局 /proc/net/tcp
		tcpPath = filepath.Join(s.root, "net", "tcp")
		f, err = os.Open(tcpPath)
		if err != nil {
			return 0, false, fmt.Errorf("读取 tcp 连接失败: %w", err)
		}
	}
	defer f.Close()

	// 构造下游 endpoint 匹配集
	matchMap := make(map[string]bool)
	for _, ds := range downstreams {
		ip := ds.IP
		if ip == "127.0.0.1" || ip == "localhost" {
			matchMap[fmt.Sprintf("127.0.0.1:%d", ds.Port)] = true
		} else {
			matchMap[fmt.Sprintf("%s:%d", ds.IP, ds.Port)] = true
		}
	}

	count := 0
	scanner := bufio.NewScanner(f)
	if scanner.Scan() { // skip header
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			fields := strings.Fields(line)
			if len(fields) < 4 {
				continue
			}
			// 01 表示 TCP_ESTABLISHED
			if fields[3] != "01" {
				continue
			}
			remAddr := fields[2]
			remIP, remPort, err := parseHexAddr(remAddr)
			if err != nil {
				continue
			}
			key := fmt.Sprintf("%s:%d", remIP, remPort)
			if matchMap[key] {
				count++
			}
		}
	}

	return count, count > 0, nil
}

// FindPeerPIDForLocalPort 给定本地连接对端临时端口，反查宿主机上所属同机进程 PID
func (s *ProcScanner) FindPeerPIDForLocalPort(localPort uint16) (uint32, error) {
	tcpPath := filepath.Join(s.root, "net", "tcp")
	f, err := os.Open(tcpPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var targetInode string
	scanner := bufio.NewScanner(f)
	if scanner.Scan() {
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) < 10 {
				continue
			}
			port, err := parseHexPort(fields[1])
			if err == nil && port == localPort {
				targetInode = fields[9]
				break
			}
		}
	}

	if targetInode == "" || targetInode == "0" {
		return 0, nil
	}

	entries, err := os.ReadDir(s.root)
	if err != nil {
		return 0, err
	}

	targetPattern := fmt.Sprintf("socket:[%s]", targetInode)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pidNum, err := strconv.ParseUint(entry.Name(), 10, 32)
		if err != nil {
			continue
		}
		pid := uint32(pidNum)

		fdDir := filepath.Join(s.root, entry.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			linkTarget, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err == nil && linkTarget == targetPattern {
				return pid, nil
			}
		}
	}

	return 0, nil
}

func (s *ProcScanner) readCmdline(pid uint32) string {
	cmdPath := filepath.Join(s.root, strconv.FormatUint(uint64(pid), 10), "cmdline")
	data, err := os.ReadFile(cmdPath)
	if err != nil {
		return ""
	}
	// /proc/<pid>/cmdline 以 \0 分隔
	parts := bytes.Split(data, []byte{0})
	var strParts []string
	for _, p := range parts {
		if len(p) > 0 {
			strParts = append(strParts, string(p))
		}
	}
	return strings.Join(strParts, " ")
}

func (s *ProcScanner) readExeName(pid uint32) string {
	exePath := filepath.Join(s.root, strconv.FormatUint(uint64(pid), 10), "exe")
	target, err := os.Readlink(exePath)
	if err == nil && target != "" {
		return filepath.Base(target)
	}

	commPath := filepath.Join(s.root, strconv.FormatUint(uint64(pid), 10), "comm")
	data, err := os.ReadFile(commPath)
	if err == nil {
		return strings.TrimSpace(string(data))
	}
	return ""
}

// parseHexPort 从 "0100007F:0050" 格式提取端口 80
func parseHexPort(addrStr string) (uint16, error) {
	parts := strings.Split(addrStr, ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("非法地址格式: %s", addrStr)
	}
	p, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return 0, err
	}
	return uint16(p), nil
}

// parseHexAddr 从 "0100007F:1F90" 提取 IP 和 端口
func parseHexAddr(addrStr string) (string, uint16, error) {
	parts := strings.Split(addrStr, ":")
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("非法地址格式: %s", addrStr)
	}

	port, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return "", 0, err
	}

	ipBytes, err := hex.DecodeString(parts[0])
	if err != nil || len(ipBytes) != 4 {
		return "", 0, fmt.Errorf("解码 IP 失败: %s", parts[0])
	}

	// /proc/net/tcp 中 IPv4 为小端序 (Little-Endian)
	ip := net.IPv4(ipBytes[3], ipBytes[2], ipBytes[1], ipBytes[0]).String()
	return ip, uint16(port), nil
}

// Ensure interface check or helper exports
var _ io.Closer = (*os.File)(nil)
