package ebpf

import (
	"encoding/binary"
	"net"
)

// ipv4ToBPFBe32 将 Go net.IP 转换为内核 BPF __be32 内存四字节等价值。
// 在小端机器上，net.ParseIP("127.0.0.1").To4() 字节为 [127, 0, 0, 1]，
// NativeEndian 读取后为 0x0100007f，使得其在内存中的顺序为 7f 00 00 01，
// 与 Linux 内核中 bpf_htonl(0x7f000001) 内存布局 100% 对齐。
func ipv4ToBPFBe32(ip net.IP) uint32 {
	ip4 := ip.To4()
	if ip4 == nil {
		return 0
	}
	return binary.NativeEndian.Uint32(ip4)
}

// bpfBe32ToIPv4 将内核 BPF __be32 内存值还原为 Go net.IP
func bpfBe32ToIPv4(v uint32) net.IP {
	var b [4]byte
	binary.NativeEndian.PutUint32(b[:], v)
	return net.IPv4(b[0], b[1], b[2], b[3])
}

// htons 将主机序端口号 (16位) 转换为网络字节序在宿主架构下的 uint16 表现
func htons(port uint16) uint16 {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], port)
	return binary.NativeEndian.Uint16(b[:])
}

// ntohs 将网络字节序端口号还原为主机序 uint16
func ntohs(bePort uint16) uint16 {
	var b [2]byte
	binary.NativeEndian.PutUint16(b[:], bePort)
	return binary.BigEndian.Uint16(b[:])
}
