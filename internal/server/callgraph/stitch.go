package callgraph

import (
	"math"
	"net"
	"strconv"
	"strings"

	"code-traefik/models"
)

// StitchedHopResult 跨跳缝合结果
type StitchedHopResult struct {
	EgressRecordID  uint64
	IngressRecordID uint64
	NetHopUs        int64
	Confidence      string
}

// StitchCrossHops 对会话内的一批记录进行跨进程跨跳缝合 (P1.EGRESS ⇔ P2.INGRESS)
func StitchCrossHops(
	records []models.TrafficRecord,
	hydratedMap map[uint64]*TrafficRecordSummary,
) (map[uint64]*TrafficRecordSummary, map[uint64]int64, map[uint64]string, map[uint64]bool) {
	stitchedIngressMap := make(map[uint64]*TrafficRecordSummary) // egressID -> stitched Ingress summary
	netHopMap := make(map[uint64]int64)                          // egressID -> net_hop_us
	confidenceMap := make(map[uint64]string)                     // egressID -> confidence
	consumedIngressIDs := make(map[uint64]bool)                  // ingressID -> true

	var egresses []models.TrafficRecord
	var ingresses []models.TrafficRecord

	for _, r := range records {
		if r.Direction == "EGRESS" {
			egresses = append(egresses, r)
		} else if r.Direction == "INGRESS" {
			ingresses = append(ingresses, r)
		}
	}

	// 1. 优先按 DOTR TraceID + SpanID 精确匹配
	for _, eg := range egresses {
		if eg.TraceID == "" || eg.TraceID == "0" {
			continue
		}
		for _, in := range ingresses {
			if consumedIngressIDs[in.ID] {
				continue
			}
			if in.TraceID != eg.TraceID {
				continue
			}
			if in.ServiceName != eg.ServiceName || in.Operation != eg.Operation {
				continue
			}
			// Trace 精确配对：SpanID 相同，或 Ingress 的 Span/FatherSpan 与 Egress 关联
			spanMatch := (eg.SpanID != "" && (eg.SpanID == in.SpanID || eg.SpanID == in.FatherSpanID)) ||
				(in.FatherSpanID != "" && in.FatherSpanID == eg.SpanID)
			if spanMatch {
				stitchedIngressMap[eg.ID] = hydratedMap[in.ID]
				consumedIngressIDs[in.ID] = true
				confidenceMap[eg.ID] = ConfidenceDOTRExact

				netHop := int64(0)
				if eg.LatencyUs != nil && in.LatencyUs != nil && *eg.LatencyUs > *in.LatencyUs {
					netHop = *eg.LatencyUs - *in.LatencyUs
				} else if in.ReqCapturedAtNs >= eg.ReqCapturedAtNs {
					netHop = (in.ReqCapturedAtNs - eg.ReqCapturedAtNs) / 1000
				}
				netHopMap[eg.ID] = netHop
				break
			}
		}
	}

	// 2. 指纹降级兜底: (ServiceName, Operation, ReqPayloadSHA256, DstPort, |ΔReqNs| <= 50ms)
	for _, eg := range egresses {
		if stitchedIngressMap[eg.ID] != nil {
			continue
		}
		egDstPort := extractPort(eg.DstAddr)

		for _, in := range ingresses {
			if consumedIngressIDs[in.ID] {
				continue
			}
			if in.ServiceName != eg.ServiceName || in.Operation != eg.Operation {
				continue
			}
			if eg.ReqPayloadSHA256 == "" || in.ReqPayloadSHA256 == "" || eg.ReqPayloadSHA256 != in.ReqPayloadSHA256 {
				continue
			}

			// 检查端口一致性 (Egress 的目的端口通常是 Ingress 绑定的服务端口)
			inDstPort := extractPort(in.DstAddr)
			if egDstPort > 0 && inDstPort > 0 && egDstPort != inDstPort {
				continue
			}

			// 时间窗检查 <= 50ms
			deltaNs := int64(math.Abs(float64(eg.ReqCapturedAtNs - in.ReqCapturedAtNs)))
			if deltaNs <= 50_000_000 {
				stitchedIngressMap[eg.ID] = hydratedMap[in.ID]
				consumedIngressIDs[in.ID] = true
				confidenceMap[eg.ID] = ConfidenceFingerprintInferred

				netHop := int64(0)
				if eg.LatencyUs != nil && in.LatencyUs != nil && *eg.LatencyUs > *in.LatencyUs {
					netHop = *eg.LatencyUs - *in.LatencyUs
				} else if in.ReqCapturedAtNs >= eg.ReqCapturedAtNs {
					netHop = (in.ReqCapturedAtNs - eg.ReqCapturedAtNs) / 1000
				}
				netHopMap[eg.ID] = netHop
				break
			}
		}
	}

	return stitchedIngressMap, netHopMap, confidenceMap, consumedIngressIDs
}

func extractPort(addr string) int {
	if addr == "" {
		return 0
	}
	_, portStr, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err == nil {
		p, _ := strconv.Atoi(portStr)
		return p
	}
	parts := strings.Split(addr, ":")
	if len(parts) == 2 {
		p, _ := strconv.Atoi(parts[1])
		return p
	}
	return 0
}
