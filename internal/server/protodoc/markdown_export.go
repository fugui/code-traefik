package protodoc

import (
	"encoding/json"
	"fmt"
	"strings"
)

func escapeMDCell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\x00", "")
	s = strings.ReplaceAll(s, "|", `\|`)
	return strings.TrimSpace(s)
}

func escapeMDProse(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	return strings.TrimSpace(s)
}

func markdownAnchor(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune('-')
		}
	}
	return b.String()
}

func formatPrettyJSON(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return "{}"
	}
	var anyVal any
	if err := json.Unmarshal([]byte(trimmed), &anyVal); err != nil {
		return trimmed
	}
	pretty, err := json.MarshalIndent(anyVal, "", "  ")
	if err != nil {
		return trimmed
	}
	return string(pretty)
}

func flattenFieldsForMarkdown(fields []FieldDocNode, prefix string, out *[]flatFieldRow) {
	for _, f := range fields {
		pathName := f.Name
		if prefix != "" {
			pathName = prefix + "." + f.Name
		}
		notes := f.Comment
		if f.OneofGroup != "" {
			if notes != "" {
				notes = fmt.Sprintf("[oneof:%s] %s", f.OneofGroup, notes)
			} else {
				notes = fmt.Sprintf("[oneof:%s]", f.OneofGroup)
			}
		}
		if f.IsDeprecated {
			notes = "[DEPRECATED] " + notes
		}
		if f.Truncated {
			notes = strings.TrimSpace(fmt.Sprintf("%s (递归/深度截断: %s)", notes, f.TruncatedReason))
		}
		*out = append(*out, flatFieldRow{
			Path:   pathName,
			Type:   f.Type,
			Label:  f.Label,
			Number: f.Number,
			Notes:  notes,
		})
		if len(f.Children) > 0 {
			flattenFieldsForMarkdown(f.Children, pathName, out)
		}
	}
}

type flatFieldRow struct {
	Path   string
	Type   string
	Label  string
	Number int32
	Notes  string
}

// RenderServicesMarkdown 将一个或多个 Service 的活体文档、血缘、Enum 字典与示例渲染为离线 Markdown 手册
func RenderServicesMarkdown(details []*ProtoServiceDocDetailDTO, title string) string {
	var sb strings.Builder
	if strings.TrimSpace(title) == "" {
		title = "CROS Protobuf 接口参考手册"
	}
	sb.WriteString("# " + escapeMDProse(title) + "\n\n")
	sb.WriteString("> 由 Code-Traefik 活体文档引擎 (`protodoc`) 自动生成，包含全层级注释、错误码矩阵、Enum 枚举字典、AST 缺省示例与 ACTIVE 黄金测试样本。\n\n")

	if len(details) > 1 {
		sb.WriteString("## 目录 (Table of Contents)\n\n")
		for idx, d := range details {
			sb.WriteString(fmt.Sprintf("%d. [%s](#%s)", idx+1, escapeMDProse(d.ServiceName), markdownAnchor(d.ServiceName)))
			if d.Summary != "" {
				sb.WriteString(" — " + escapeMDProse(d.Summary))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n---\n\n")
	}

	for svcIdx, svc := range details {
		if svcIdx > 0 {
			sb.WriteString("\n---\n\n")
		}
		sb.WriteString("## " + escapeMDProse(svc.ServiceName) + "\n\n")
		if svc.IsDeprecated {
			sb.WriteString("> **WARNING DEPRECATED**: " + escapeMDProse(svc.DeprecationReason) + "\n\n")
		}

		sb.WriteString(fmt.Sprintf("- **制品包**: `%s` (v%d / %s)\n", escapeMDCell(svc.SchemaName), svc.SchemaVersion, escapeMDCell(svc.SchemaCategory)))
		if svc.ProtoFile != "" {
			if svc.StartLine > 0 {
				sb.WriteString(fmt.Sprintf("- **源码位置**: `%s#L%d`\n", escapeMDCell(svc.ProtoFile), svc.StartLine))
			} else {
				sb.WriteString(fmt.Sprintf("- **源码位置**: `%s`\n", escapeMDCell(svc.ProtoFile)))
			}
		}
		if svc.OwnerTag != "" {
			sb.WriteString(fmt.Sprintf("- **负责人 (`@owner`)**: `%s`\n", escapeMDCell(svc.OwnerTag)))
		}
		if !svc.HasSourceInfo {
			sb.WriteString("- **说明**: 当前 `.pb` 描述集不含原始注释位置 (`SourceCodeInfo`)，以下由结构化反射自动生成。\n")
		}
		sb.WriteString("\n")

		if svc.LeadingComments != "" {
			sb.WriteString(escapeMDProse(svc.LeadingComments) + "\n\n")
		}

		if len(svc.ErrorCodes) > 0 {
			sb.WriteString("### 服务级错误码声明 (`@error_code`)\n\n")
			sb.WriteString("| 错误码标识 | HTTP/CROS 状态码 | 说明 |\n")
			sb.WriteString("| :--- | :---: | :--- |\n")
			for _, ec := range svc.ErrorCodes {
				sb.WriteString(fmt.Sprintf("| `%s` | `%s` | %s |\n",
					escapeMDCell(ec.Code), escapeMDCell(ec.HttpOrCrosCode), escapeMDCell(ec.Description)))
			}
			sb.WriteString("\n")
		}

		renderLineageMarkdown(&sb, svc.Lineage)

		sb.WriteString("### RPC 方法清单\n\n")
		sb.WriteString("| 方法名 | 请求消息 | 应答消息 | 负责人 | 状态 | 摘要 |\n")
		sb.WriteString("| :--- | :--- | :--- | :--- | :---: | :--- |\n")
		for _, m := range svc.Methods {
			status := "ACTIVE"
			if m.IsDeprecated {
				status = "DEPRECATED"
			}
			sb.WriteString(fmt.Sprintf("| `%s` | `%s` | `%s` | `%s` | %s | %s |\n",
				escapeMDCell(m.Operation),
				escapeMDCell(m.ReqMessageFullName),
				escapeMDCell(m.RspMessageFullName),
				escapeMDCell(m.OwnerTag),
				status,
				escapeMDCell(m.Summary),
			))
		}
		sb.WriteString("\n")

		for _, m := range svc.Methods {
			sb.WriteString(fmt.Sprintf("### 方法 `%s`\n\n", escapeMDProse(m.Operation)))
			if m.IsDeprecated {
				sb.WriteString("> **DEPRECATED**: " + escapeMDProse(m.DeprecationReason) + "\n\n")
			}
			if m.LeadingComments != "" {
				sb.WriteString(escapeMDProse(m.LeadingComments) + "\n\n")
			}
			if m.ProtoFile != "" && m.StartLine > 0 {
				sb.WriteString(fmt.Sprintf("- **声明位置**: `%s#L%d`\n", escapeMDCell(m.ProtoFile), m.StartLine))
			}
			if m.OwnerTag != "" {
				sb.WriteString(fmt.Sprintf("- **方法负责人**: `%s`\n", escapeMDCell(m.OwnerTag)))
			}
			sb.WriteString("\n")

			if len(m.ErrorCodes) > 0 {
				sb.WriteString("#### 方法错误码 (`@error_code`)\n\n")
				sb.WriteString("| 错误码标识 | 状态码 | 说明 |\n")
				sb.WriteString("| :--- | :---: | :--- |\n")
				for _, ec := range m.ErrorCodes {
					sb.WriteString(fmt.Sprintf("| `%s` | `%s` | %s |\n",
						escapeMDCell(ec.Code), escapeMDCell(ec.HttpOrCrosCode), escapeMDCell(ec.Description)))
				}
				sb.WriteString("\n")
			}

			renderFieldTableMarkdown(&sb, "请求参数字典 (`"+m.ReqMessageFullName+"`)", m.ReqFields)
			renderFieldTableMarkdown(&sb, "应答参数字典 (`"+m.RspMessageFullName+"`)", m.RspFields)

			sb.WriteString("#### 缺省 JSON 请求示例 (AST 自动推导)\n\n```json\n")
			sb.WriteString(formatPrettyJSON(m.DefaultReqJSON))
			sb.WriteString("\n```\n\n")

			sb.WriteString("#### 缺省 JSON 应答示例 (AST 自动推导)\n\n```json\n")
			sb.WriteString(formatPrettyJSON(m.DefaultRspJSON))
			sb.WriteString("\n```\n\n")

			if len(m.GoldenSamples) > 0 {
				sb.WriteString("#### ACTIVE 黄金测试样本\n\n")
				for _, gs := range m.GoldenSamples {
					sb.WriteString(fmt.Sprintf("- **套件**: `%s` (%s) · **解码状态**: `%s`\n\n",
						escapeMDCell(gs.SuiteCode), escapeMDCell(gs.SuiteName), escapeMDCell(gs.DecodeStatus)))
					if len(gs.ReqJSON) > 0 {
						sb.WriteString("**黄金请求 JSON**:\n\n```json\n")
						sb.WriteString(formatPrettyJSON(gs.ReqJSON))
						sb.WriteString("\n```\n\n")
					}
					if len(gs.RspJSON) > 0 {
						sb.WriteString("**黄金应答 JSON**:\n\n```json\n")
						sb.WriteString(formatPrettyJSON(gs.RspJSON))
						sb.WriteString("\n```\n\n")
					}
				}
			}
		}

		if len(svc.Enums) > 0 {
			sb.WriteString("### 关联 Enum 枚举字典\n\n")
			for _, ed := range svc.Enums {
				sb.WriteString(fmt.Sprintf("#### 枚举 `%s`\n\n", escapeMDProse(ed.FullName)))
				if ed.Comment != "" {
					sb.WriteString(escapeMDProse(ed.Comment) + "\n\n")
				}
				sb.WriteString("| 枚举值 | 数值 | 废弃 | 注释说明 |\n")
				sb.WriteString("| :--- | :---: | :---: | :--- |\n")
				for _, v := range ed.Values {
					dep := "否"
					if v.IsDeprecated {
						dep = "是"
					}
					sb.WriteString(fmt.Sprintf("| `%s` | %d | %s | %s |\n",
						escapeMDCell(v.Name), v.Number, dep, escapeMDCell(v.Comment)))
				}
				sb.WriteString("\n")
			}
		}
	}

	return sb.String()
}

func renderFieldTableMarkdown(sb *strings.Builder, title string, fields []FieldDocNode) {
	sb.WriteString("#### " + title + "\n\n")
	if len(fields) == 0 {
		sb.WriteString("_无字段（空消息体）_\n\n")
		return
	}
	var rows []flatFieldRow
	flattenFieldsForMarkdown(fields, "", &rows)
	sb.WriteString("| 字段路径 | 类型 | 修饰符 | Tag | 说明 |\n")
	sb.WriteString("| :--- | :--- | :---: | :---: | :--- |\n")
	for _, r := range rows {
		sb.WriteString(fmt.Sprintf("| `%s` | `%s` | `%s` | %d | %s |\n",
			escapeMDCell(r.Path), escapeMDCell(r.Type), escapeMDCell(r.Label), r.Number, escapeMDCell(r.Notes)))
	}
	sb.WriteString("\n")
}

func renderLineageMarkdown(sb *strings.Builder, lineage ServiceContractLineageDTO) {
	sb.WriteString("### 架构归属与多实现血缘 (ADR-16)\n\n")
	sb.WriteString(fmt.Sprintf("- **认领状态**: `%s`\n", escapeMDCell(lineage.ClaimStatus)))
	if lineage.Definer != nil {
		sb.WriteString(fmt.Sprintf("- **公共契约定义方 (`CONTRACT_LIB`)**: `%s` (%s / %s, 模式 `%s`)\n",
			escapeMDCell(lineage.Definer.ModuleCode),
			escapeMDCell(lineage.Definer.ModuleName),
			escapeMDCell(lineage.Definer.SubsystemName),
			escapeMDCell(lineage.Definer.ProvideMode)))
	}
	if len(lineage.Implementations) > 0 {
		sb.WriteString("\n| 实现Key | 模块编码 | 模块名称 | 所属子系统 | 架构进程 |\n")
		sb.WriteString("| :--- | :--- | :--- | :--- | :--- |\n")
		for _, impl := range lineage.Implementations {
			sb.WriteString(fmt.Sprintf("| `%s` | `%s` | %s | %s | `%s` |\n",
				escapeMDCell(impl.ImplKey),
				escapeMDCell(impl.ModuleCode),
				escapeMDCell(impl.ModuleName),
				escapeMDCell(impl.SubsystemName),
				escapeMDCell(impl.ArchProcessCode)))
		}
	}
	if len(lineage.Consumers) > 0 {
		sb.WriteString("\n**下游消费方**: ")
		names := make([]string, 0, len(lineage.Consumers))
		for _, c := range lineage.Consumers {
			names = append(names, fmt.Sprintf("`%s` (%s)", escapeMDCell(c.ModuleCode), escapeMDCell(c.DepType)))
		}
		sb.WriteString(strings.Join(names, ", "))
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
}
