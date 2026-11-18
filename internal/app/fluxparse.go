package app

import (
	"net"
	"regexp"
	"strings"
	"time"
)

// cnald/cnalw 组合根的小助手集：flux 表解析、锚点指纹、URL/源打印、时长取大。

// parseFluxTable 解析 userflux HTML 表格（正则替代 bs4，Python fetch_flux 同源）。
// 返回非空 map = 会话活着（L3 真值盖章）；空表 = 会话不存在（防抖后判死）。
// 行内单元格相邻成对取值：2 列（键/值）与 4 列（键/值/键/值）都吃——
// 参考校是 2 列，异校多列表不能被截成只剩第一对（面板显示用户名的根因）。
var (
	rowRe  = regexp.MustCompile(`(?s)<tr[^>]*>(.*?)</tr>`)
	cellRe = regexp.MustCompile(`(?s)<td[^>]*>(.*?)</td>`)
	tagRe  = regexp.MustCompile(`<[^>]+>`)
)

func parseFluxTable(body string) map[string]string {
	data := map[string]string{}
	for _, row := range rowRe.FindAllStringSubmatch(body, -1) {
		cells := cellRe.FindAllStringSubmatch(row[1], -1)
		for i := 0; i+1 < len(cells); i += 2 {
			k := strings.TrimSpace(tagRe.ReplaceAllString(cells[i][1], ""))
			k = strings.ReplaceAll(k, "：", "")
			v := strings.TrimSpace(tagRe.ReplaceAllString(cells[i+1][1], ""))
			if k != "" {
				data[k] = v
			}
		}
	}
	return data
}

// containsAny 报告 s 是否含 marks 中的任一指纹（Python _marks 探测判据同源）。
func containsAny(s string, marks []string) bool {
	for _, m := range marks {
		if m != "" && strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// peerURL 把裸 v6 字面量补成带 scheme 的完整 URL 前缀（路径由 peer_path 提供
// ——端点路径是校事实，不进代码；cmd/cnal 同款）。
func peerURL(host string) string {
	if strings.Contains(host, "://") {
		return host
	}
	return "http://[" + host + "]"
}

// srcStr 源打印（nil 显示"（无）"）。
func srcStr(ip net.IP) string {
	if ip == nil {
		return "（无）"
	}
	return ip.String()
}

// maxDuration 取较大时长（Python max(probe_timeout, 5) 同源语义）。
func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
